package agenttools

import (
	"context"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// handlers.go is the implementations the table binds.
//
// Each one translates a tool's arguments into an application service's request. That
// is the whole job: the service owns the rules, the repository owns the writes, and a
// handler that enforced a domain rule here would be a second place for it to be wrong.
//
// THE SCOPE COMES FROM ToolRequest, NEVER FROM THE ARGUMENTS. Section 7.1 forbids
// taking a project or episode from a model — a model that could name its own project
// could read another project's story — so no schema has a project field and no handler
// reads one. Where a tool takes an artifact id, the handler walks that artifact to its
// episode and compares the project BEFORE returning or writing anything.

// ---------------------------------------------------------------------------
// Workflow
// ---------------------------------------------------------------------------

func bindReadState(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			WorkflowRunID string `json:"workflowRunId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		runID := strings.TrimSpace(arguments.WorkflowRunID)
		if runID == "" {
			runID = request.WorkflowRunID
		}
		if runID == "" {
			return nil, agent.InvalidError("No workflow run is in scope, so there is no state to read.")
		}
		run, err := deps.Workflow.GetRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		// Checked before the stages are read, so a run in another project is refused
		// rather than summarized.
		if run.ProjectID != request.ProjectID {
			return nil, agent.SecurityError("That workflow run belongs to another project.")
		}
		stages, err := deps.Workflow.ListStages(ctx, runID)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"workflowRunId":    run.ID,
			"workflowType":     run.WorkflowType,
			"status":           string(run.Status),
			"currentStage":     string(run.CurrentStage),
			"activeStageRunId": run.ActiveStageRunID,
			"retryCount":       run.RetryCount,
			"stages":           stageViews(stages),
		}, nil
	}
}

// stageView is one stage attempt as a prompt sees it.
//
// It reports what section 9 says a Decision agent needs — the attempt, its status, its
// agent and whether it is active — and NOT the input or output JSON, which would carry
// a whole artifact into a prompt. A model that needs the artifact calls the read tool
// for it, which is section 6.4's "返回结构化、限长数据".
type stageView struct {
	Stage     string `json:"stage"`
	Attempt   int    `json:"attempt"`
	Status    string `json:"status"`
	Active    bool   `json:"active"`
	AgentKey  string `json:"agentKey,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
}

func stageViews(stages []workflow.StageRun) []stageView {
	views := make([]stageView, 0, len(stages))
	for _, stage := range stages {
		views = append(views, stageView{
			Stage: string(stage.Stage), Attempt: stage.Attempt,
			Status: string(stage.Status), Active: stage.IsActive(),
			AgentKey: stage.ExecutionAgentKey, ErrorCode: stage.ErrorCode,
		})
	}
	return views
}

// requestUserGate parks a stage at a user decision.
//
// It is a REAL state change rather than a note: section 12.2's gate happens in
// `waiting_user`, and the domain's machine permits `running → waiting_user`,
// `reviewing → waiting_user` and `execution_succeeded → waiting_user` — the edges a
// request for a person arrives on. The transition goes through the workflow service, so
// it writes its audit event in the same transaction as the change (ADR-0009).
//
// What it does NOT do is decide anything. It moves the stage to where a person is
// asked; SubmitGateDecision is a user's command and is not reachable from a tool, which
// is what section 9's "Decision 不能提出非法动作" means at this layer.
func bindRequestUserGate(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			StageRunID string `json:"stageRunId"`
			Reason     string `json:"reason"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		stageRunID := strings.TrimSpace(arguments.StageRunID)
		if stageRunID == "" {
			stageRunID = request.StageRunID
		}
		if stageRunID == "" {
			return nil, agent.InvalidError("No stage is in scope to park.")
		}
		reason, err := required(arguments.Reason, "reason")
		if err != nil {
			return nil, err
		}
		stage, err := deps.Workflow.GetStage(ctx, stageRunID)
		if err != nil {
			return nil, err
		}
		// The stage's run is what names its project, and that check runs before the
		// transition so a model cannot park another project's stage.
		run, err := deps.Workflow.GetRun(ctx, stage.WorkflowRunID)
		if err != nil {
			return nil, err
		}
		if run.ProjectID != request.ProjectID {
			return nil, agent.SecurityError("That stage belongs to another project.")
		}
		// The edge is checked against the domain rather than assumed, so this cannot
		// produce a transition SQLite would refuse.
		if !workflow.CanStageTransition(stage.Status, workflow.StageWaitingUser) {
			return nil, agent.ConflictError("That stage cannot be parked for a decision from where it is.")
		}
		updated, err := deps.Workflow.TransitionStage(ctx, appworkflow.TransitionStageRequest{
			StageRunID: stage.ID,
			Status:     workflow.StageWaitingUser,
			Revision:   stage.Revision,
			Actor:      appworkflow.Actor{Type: createdByAgent, ID: request.StageRunID},
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"stageRunId": updated.ID,
			"stage":      string(updated.Stage),
			"status":     string(updated.Status),
			"reason":     reason,
			"note":       "The stage is waiting for a person. The runtime does not decide for them.",
		}, nil
	}
}

// ---------------------------------------------------------------------------
// Story reads
// ---------------------------------------------------------------------------

func bindReadEvents(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			ChapterID string `json:"chapterId"`
			Status    string `json:"status"`
			Limit     int    `json:"limit"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		status, err := storyStatusFilter(arguments.Status)
		if err != nil {
			return nil, err
		}
		events, err := deps.Story.ListStoryEvents(ctx, request.ProjectID,
			strings.TrimSpace(arguments.ChapterID), status)
		if err != nil {
			return nil, err
		}
		limit := limitOf(arguments.Limit, 50, 200)
		total := len(events)
		if len(events) > limit {
			events = events[:limit]
		}
		return map[string]any{"events": eventViews(events), "returned": len(events), "total": total}, nil
	}
}

// eventView is one event as a prompt sees it: the fields a later stage needs, and no
// unbounded free text.
type eventView struct {
	ID             string  `json:"id"`
	ChapterID      string  `json:"chapterId,omitempty"`
	Ordinal        int     `json:"ordinal"`
	Name           string  `json:"name"`
	EventType      string  `json:"eventType,omitempty"`
	Status         string  `json:"status"`
	Importance     string  `json:"importance,omitempty"`
	Confidence     float64 `json:"confidence"`
	StoryTimeOrder *int    `json:"storyTimeOrder,omitempty"`
	LocationID     string  `json:"locationEntityId,omitempty"`
}

func eventViews(events []story.StoryEvent) []eventView {
	views := make([]eventView, 0, len(events))
	for _, event := range events {
		views = append(views, eventView{
			ID: event.ID, ChapterID: event.ChapterID, Ordinal: event.Ordinal,
			Name: event.Name, EventType: event.EventType, Status: string(event.Status),
			Importance: event.Importance, Confidence: event.Confidence,
			StoryTimeOrder: event.StoryTimeOrder, LocationID: event.LocationEntityID,
		})
	}
	return views
}

// readChapterText returns one chapter's text, marked untrusted.
//
// The text is read through the reader WP-06 declared for this walk, which is the one
// place that knows the rune-slicing rule and the clamping a chapter's offsets need.
// The adapter the composition root supplies delegates to the import service, so there
// is ONE implementation of "what text is this chapter" rather than two that could
// disagree about the byte-versus-character rule.
func bindReadChapterText(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			ChapterID string `json:"chapterId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		chapterID, err := required(arguments.ChapterID, "chapter")
		if err != nil {
			return nil, err
		}
		chapter, err := deps.Chapters.ChapterWithText(ctx, chapterID)
		if err != nil {
			return nil, err
		}
		// The reader walks chapter → version → document and reports the project it
		// found, so the boundary check needs no second lookup and cannot observe a
		// different state than the text came from.
		if chapter.ProjectID != request.ProjectID {
			return nil, agent.SecurityError("That chapter belongs to another project.")
		}
		return map[string]any{
			"chapterId":               chapter.Chapter.ID,
			"title":                   chapter.Chapter.Title,
			"sourceDocumentVersionId": chapter.SourceDocumentVersionID,
			"text":                    chapter.Text,
			"language":                chapter.Language,
			// The caller is told what this is, because section 5.2's untrusted list
			// begins with imported novel text and the prompt layer is what marks it.
			"untrusted": true,
		}, nil
	}
}

func bindReadRules(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			Category string `json:"category"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		rules, err := deps.Projects.ListRules(ctx, request.ProjectID, false)
		if err != nil {
			return nil, err
		}
		wanted := strings.TrimSpace(arguments.Category)
		views := make([]ruleView, 0, len(rules))
		for _, rule := range rules {
			if wanted != "" && !strings.EqualFold(string(rule.Category), wanted) {
				continue
			}
			views = append(views, ruleView{
				ID: rule.ID, Category: string(rule.Category), Name: rule.Name,
				Content: rule.Content, Strength: string(rule.Strength),
				Status: string(rule.Status), LockedByUser: rule.LockedByUser,
			})
		}
		return map[string]any{"rules": views, "total": len(views)}, nil
	}
}

type ruleView struct {
	ID           string `json:"id"`
	Category     string `json:"category,omitempty"`
	Name         string `json:"name"`
	Content      string `json:"content,omitempty"`
	Strength     string `json:"strength,omitempty"`
	Status       string `json:"status"`
	LockedByUser bool   `json:"lockedByUser"`
}

// ---------------------------------------------------------------------------
// Script reads
// ---------------------------------------------------------------------------

func bindReadStorySkeleton(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			VersionID string `json:"versionId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		versionID, err := required(arguments.VersionID, "version")
		if err != nil {
			return nil, err
		}
		version, err := deps.Script.GetStorySkeletonVersion(ctx, versionID)
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, version.EpisodeID); err != nil {
			return nil, err
		}
		return map[string]any{
			"versionId":                version.ID,
			"episodeId":                version.EpisodeID,
			"versionNumber":            version.VersionNumber,
			"status":                   string(version.Status),
			"basedOnVersionId":         version.BasedOnVersionID,
			"openingHook":              version.OpeningHook,
			"coreConflict":             version.CoreConflict,
			"turningPointsJson":        version.TurningPointsJSON,
			"climax":                   version.Climax,
			"endingHook":               version.EndingHook,
			"estimatedDurationSeconds": version.EstimatedDurationSeconds,
		}, nil
	}
}

func bindReadAdaptationStrategy(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			VersionID string `json:"versionId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		versionID, err := required(arguments.VersionID, "version")
		if err != nil {
			return nil, err
		}
		version, err := deps.Script.GetAdaptationStrategyVersion(ctx, versionID)
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, version.EpisodeID); err != nil {
			return nil, err
		}
		return map[string]any{
			"versionId":             version.ID,
			"episodeId":             version.EpisodeID,
			"versionNumber":         version.VersionNumber,
			"status":                string(version.Status),
			"strategySummary":       version.StrategySummary,
			"adaptationMode":        string(version.AdaptationMode),
			"mergedEventGroupsJson": version.MergedEventGroupsJSON,
			"originalAdditions":     version.OriginalAdditions,
			"rationale":             version.Rationale,
			"risks":                 version.Risks,
		}, nil
	}
}

func bindReadScriptVersion(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			VersionID string `json:"versionId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		versionID, err := required(arguments.VersionID, "version")
		if err != nil {
			return nil, err
		}
		version, err := deps.Script.GetScriptVersion(ctx, versionID)
		if err != nil {
			return nil, err
		}
		// A script version carries its script, and the script carries its episode.
		script, err := deps.Script.GetScript(ctx, version.ScriptID)
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, script.EpisodeID); err != nil {
			return nil, err
		}
		scenes, err := deps.Script.ListScenes(ctx, version.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"versionId":                   version.ID,
			"episodeId":                   script.EpisodeID,
			"versionNumber":               version.VersionNumber,
			"status":                      string(version.Status),
			"summary":                     version.Summary,
			"estimatedDurationSeconds":    version.EstimatedDurationSeconds,
			"storySkeletonVersionId":      version.StorySkeletonVersionID,
			"adaptationStrategyVersionId": version.AdaptationStrategyVersionID,
			"sceneCount":                  len(scenes),
		}, nil
	}
}

// ---------------------------------------------------------------------------
// Script writes
// ---------------------------------------------------------------------------

func bindCreateStorySkeletonVersion(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			EpisodeID                string `json:"episodeId"`
			BasedOnVersionID         string `json:"basedOnVersionId"`
			OpeningHook              string `json:"openingHook"`
			CoreConflict             string `json:"coreConflict"`
			TurningPointsJSON        string `json:"turningPointsJson"`
			Climax                   string `json:"climax"`
			EndingHook               string `json:"endingHook"`
			EstimatedDurationSeconds int    `json:"estimatedDurationSeconds"`
			ChangeReason             string `json:"changeReason"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		episodeID, err := required(arguments.EpisodeID, "episode")
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, episodeID); err != nil {
			return nil, err
		}
		createdBy, createdByID := agentActor(request)
		version, err := deps.Script.CreateStorySkeletonVersion(ctx, appscript.CreateStorySkeletonVersionRequest{
			EpisodeID:                episodeID,
			BasedOnVersionID:         strings.TrimSpace(arguments.BasedOnVersionID),
			OpeningHook:              arguments.OpeningHook,
			CoreConflict:             arguments.CoreConflict,
			TurningPointsJSON:        arguments.TurningPointsJSON,
			Climax:                   arguments.Climax,
			EndingHook:               arguments.EndingHook,
			EstimatedDurationSeconds: arguments.EstimatedDurationSeconds,
			SourceAgentRunID:         request.AgentRunID,
			CreatedByType:            createdBy,
			CreatedByID:              createdByID,
			ChangeReason:             arguments.ChangeReason,
		})
		if err != nil {
			return nil, err
		}
		return artifactResult("story_skeleton", "story_skeleton_version", version.ID,
			version.VersionNumber, string(version.Status), "story skeleton"), nil
	}
}

func bindCreateAdaptationStrategyVersion(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			EpisodeID             string `json:"episodeId"`
			BasedOnVersionID      string `json:"basedOnVersionId"`
			StrategySummary       string `json:"strategySummary"`
			AdaptationMode        string `json:"adaptationMode"`
			MergedEventGroupsJSON string `json:"mergedEventGroupsJson"`
			OriginalAdditions     string `json:"originalAdditions"`
			Rationale             string `json:"rationale"`
			Risks                 string `json:"risks"`
			ChangeReason          string `json:"changeReason"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		episodeID, err := required(arguments.EpisodeID, "episode")
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, episodeID); err != nil {
			return nil, err
		}
		createdBy, createdByID := agentActor(request)
		version, err := deps.Script.CreateAdaptationStrategyVersion(ctx, appscript.CreateAdaptationStrategyVersionRequest{
			EpisodeID:             episodeID,
			BasedOnVersionID:      strings.TrimSpace(arguments.BasedOnVersionID),
			StrategySummary:       arguments.StrategySummary,
			AdaptationMode:        scriptdomain.AdaptationMode(strings.TrimSpace(arguments.AdaptationMode)),
			MergedEventGroupsJSON: arguments.MergedEventGroupsJSON,
			OriginalAdditions:     arguments.OriginalAdditions,
			Rationale:             arguments.Rationale,
			Risks:                 arguments.Risks,
			SourceAgentRunID:      request.AgentRunID,
			CreatedByType:         createdBy,
			CreatedByID:           createdByID,
			ChangeReason:          arguments.ChangeReason,
		})
		if err != nil {
			return nil, err
		}
		return artifactResult("adaptation_strategy", "adaptation_strategy_version", version.ID,
			version.VersionNumber, string(version.Status), "adaptation strategy"), nil
	}
}

func bindCreateScriptVersion(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			EpisodeID                   string `json:"episodeId"`
			BasedOnVersionID            string `json:"basedOnVersionId"`
			StorySkeletonVersionID      string `json:"storySkeletonVersionId"`
			AdaptationStrategyVersionID string `json:"adaptationStrategyVersionId"`
			Summary                     string `json:"summary"`
			EstimatedDurationSeconds    int    `json:"estimatedDurationSeconds"`
			ChangeReason                string `json:"changeReason"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		episodeID, err := required(arguments.EpisodeID, "episode")
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, episodeID); err != nil {
			return nil, err
		}
		// The two upstream versions must belong to the SAME episode, checked before the
		// write: a script assembled from another episode's skeleton would be a version
		// whose citations point at a different story, and the citation would look valid.
		for _, reference := range []struct {
			id     string
			kind   string
			lookup func(context.Context, string) (string, error)
		}{
			{arguments.StorySkeletonVersionID, "story skeleton", func(ctx context.Context, id string) (string, error) {
				version, err := deps.Script.GetStorySkeletonVersion(ctx, id)
				return version.EpisodeID, err
			}},
			{arguments.AdaptationStrategyVersionID, "adaptation strategy", func(ctx context.Context, id string) (string, error) {
				version, err := deps.Script.GetAdaptationStrategyVersion(ctx, id)
				return version.EpisodeID, err
			}},
		} {
			trimmed := strings.TrimSpace(reference.id)
			if trimmed == "" {
				continue
			}
			episodeOfReference, err := reference.lookup(ctx, trimmed)
			if err != nil {
				return nil, err
			}
			if episodeOfReference != episodeID {
				return nil, agent.InvalidError("The " + reference.kind + " version belongs to a different episode.")
			}
		}
		script, err := deps.Script.EnsureScript(ctx, episodeID)
		if err != nil {
			return nil, err
		}
		createdBy, createdByID := agentActor(request)
		version, err := deps.Script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
			ScriptID:                    script.ID,
			BasedOnVersionID:            strings.TrimSpace(arguments.BasedOnVersionID),
			StorySkeletonVersionID:      strings.TrimSpace(arguments.StorySkeletonVersionID),
			AdaptationStrategyVersionID: strings.TrimSpace(arguments.AdaptationStrategyVersionID),
			EstimatedDurationSeconds:    arguments.EstimatedDurationSeconds,
			Summary:                     arguments.Summary,
			SourceAgentRunID:            request.AgentRunID,
			CreatedByType:               createdBy,
			CreatedByID:                 createdByID,
			ChangeReason:                arguments.ChangeReason,
		})
		if err != nil {
			return nil, err
		}
		return artifactResult("script_generation", "script_version", version.ID,
			version.VersionNumber, string(version.Status), "script"), nil
	}
}

// artifactResult is the shape every write tool returns.
//
// It names the artifact by REFERENCE, which is what section 7.4's ExecutionResult
// carries and what the runtime's ArtifactVerifier reads back. The identifier is the row
// the service just wrote, so a stage reporting a version cannot be reporting one that
// does not exist: the write is what produced the id.
func artifactResult(stage, entityType, entityID string, versionNumber int, status, label string) map[string]any {
	return map[string]any{
		"stage": stage,
		"artifacts": []map[string]any{{
			"entityType": entityType,
			"entityId":   entityID,
			"operation":  "created",
		}},
		"versionNumber": versionNumber,
		"status":        status,
		"summary":       "Created " + label + " version " + itoaSmall(versionNumber) + ".",
		"nextAction":    "review",
	}
}

// ---------------------------------------------------------------------------
// Storyboard
// ---------------------------------------------------------------------------

func bindReadDirectorPlan(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			VersionID string `json:"versionId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		versionID, err := required(arguments.VersionID, "version")
		if err != nil {
			return nil, err
		}
		version, err := deps.Storyboard.GetDirectorPlanVersion(ctx, versionID)
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, version.EpisodeID); err != nil {
			return nil, err
		}
		return map[string]any{
			"versionId":       version.ID,
			"episodeId":       version.EpisodeID,
			"versionNumber":   version.VersionNumber,
			"status":          string(version.Status),
			"scriptVersionId": version.ScriptVersionID,
			"visualRhythm":    version.VisualRhythm,
			"cameraLanguage":  version.CameraLanguage,
			"colorLighting":   version.ColorLighting,
			"staging":         version.Staging,
			"continuityRules": version.ContinuityRules,
			"audioDirection":  version.AudioDirection,
		}, nil
	}
}

func bindReadStoryboard(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			VersionID string `json:"versionId"`
			Limit     int    `json:"limit"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		versionID, err := required(arguments.VersionID, "version")
		if err != nil {
			return nil, err
		}
		version, err := deps.Storyboard.GetStoryboardVersion(ctx, versionID)
		if err != nil {
			return nil, err
		}
		board, err := deps.Storyboard.GetStoryboard(ctx, version.StoryboardID)
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, board.EpisodeID); err != nil {
			return nil, err
		}
		items, err := deps.Storyboard.ListStoryboardItems(ctx, version.ID)
		if err != nil {
			return nil, err
		}
		limit := limitOf(arguments.Limit, 100, 200)
		total := len(items)
		if len(items) > limit {
			items = items[:limit]
		}
		return map[string]any{
			"versionId":     version.ID,
			"episodeId":     board.EpisodeID,
			"versionNumber": version.VersionNumber,
			"status":        string(version.Status),
			"items":         itemViews(items),
			"returned":      len(items),
			"total":         total,
		}, nil
	}
}

type itemView struct {
	ID                string `json:"id"`
	Ordinal           int    `json:"ordinal"`
	ShotSize          string `json:"shotSize,omitempty"`
	CameraAngle       string `json:"cameraAngle,omitempty"`
	CameraMovement    string `json:"cameraMovement,omitempty"`
	DurationSeconds   int    `json:"durationSeconds,omitempty"`
	VisualDescription string `json:"visualDescription,omitempty"`
	Status            string `json:"status"`
}

func itemViews(items []storyboard.StoryboardItem) []itemView {
	views := make([]itemView, 0, len(items))
	for _, item := range items {
		views = append(views, itemView{
			ID: item.ID, Ordinal: item.Ordinal, ShotSize: item.ShotSize,
			CameraAngle: item.CameraAngle, CameraMovement: item.CameraMovement,
			DurationSeconds: item.DurationSeconds, VisualDescription: item.VisualDescription,
			Status: string(item.Status),
		})
	}
	return views
}

func bindCreateDirectorPlanVersion(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			EpisodeID        string `json:"episodeId"`
			ScriptVersionID  string `json:"scriptVersionId"`
			BasedOnVersionID string `json:"basedOnVersionId"`
			VisualRhythm     string `json:"visualRhythm"`
			CameraLanguage   string `json:"cameraLanguage"`
			ColorLighting    string `json:"colorLighting"`
			Staging          string `json:"staging"`
			ContinuityRules  string `json:"continuityRules"`
			AudioDirection   string `json:"audioDirection"`
			ChangeReason     string `json:"changeReason"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		episodeID, err := required(arguments.EpisodeID, "episode")
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, episodeID); err != nil {
			return nil, err
		}
		createdBy, createdByID := agentActor(request)
		version, err := deps.Storyboard.CreateDirectorPlanVersion(ctx, appstoryboard.CreateDirectorPlanVersionRequest{
			EpisodeID:        episodeID,
			ScriptVersionID:  strings.TrimSpace(arguments.ScriptVersionID),
			BasedOnVersionID: strings.TrimSpace(arguments.BasedOnVersionID),
			VisualRhythm:     arguments.VisualRhythm,
			CameraLanguage:   arguments.CameraLanguage,
			ColorLighting:    arguments.ColorLighting,
			Staging:          arguments.Staging,
			ContinuityRules:  arguments.ContinuityRules,
			AudioDirection:   arguments.AudioDirection,
			SourceAgentRunID: request.AgentRunID,
			CreatedByType:    createdBy,
			CreatedByID:      createdByID,
			ChangeReason:     arguments.ChangeReason,
		})
		if err != nil {
			return nil, err
		}
		return artifactResult("director_plan", "director_plan_version", version.ID,
			version.VersionNumber, string(version.Status), "director plan"), nil
	}
}

func bindCreateStoryboardVersion(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			EpisodeID             string `json:"episodeId"`
			ScriptVersionID       string `json:"scriptVersionId"`
			DirectorPlanVersionID string `json:"directorPlanVersionId"`
			BasedOnVersionID      string `json:"basedOnVersionId"`
			ChangeReason          string `json:"changeReason"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		episodeID, err := required(arguments.EpisodeID, "episode")
		if err != nil {
			return nil, err
		}
		// Section 9.3 makes the script and the plan FIELDS of a storyboard version
		// rather than provenance: a board that names neither is a board of nothing.
		scriptVersionID, err := required(arguments.ScriptVersionID, "script version")
		if err != nil {
			return nil, err
		}
		planVersionID, err := required(arguments.DirectorPlanVersionID, "director plan version")
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, episodeID); err != nil {
			return nil, err
		}
		// Both upstream versions must belong to this episode, checked before the write
		// for the same reason the script stage checks its own: a citation pointing at
		// another episode's artifact looks valid and is not.
		scriptVersion, err := deps.Script.GetScriptVersion(ctx, scriptVersionID)
		if err != nil {
			return nil, err
		}
		script, err := deps.Script.GetScript(ctx, scriptVersion.ScriptID)
		if err != nil {
			return nil, err
		}
		if script.EpisodeID != episodeID {
			return nil, agent.InvalidError("The script version belongs to a different episode.")
		}
		plan, err := deps.Storyboard.GetDirectorPlanVersion(ctx, planVersionID)
		if err != nil {
			return nil, err
		}
		if plan.EpisodeID != episodeID {
			return nil, agent.InvalidError("The director plan version belongs to a different episode.")
		}
		board, err := deps.Storyboard.EnsureStoryboard(ctx, episodeID)
		if err != nil {
			return nil, err
		}
		createdBy, createdByID := agentActor(request)
		version, err := deps.Storyboard.CreateStoryboardVersion(ctx, appstoryboard.CreateStoryboardVersionRequest{
			StoryboardID:          board.ID,
			ScriptVersionID:       scriptVersion.ID,
			DirectorPlanVersionID: plan.ID,
			BasedOnVersionID:      strings.TrimSpace(arguments.BasedOnVersionID),
			SourceAgentRunID:      request.AgentRunID,
			CreatedByType:         createdBy,
			CreatedByID:           createdByID,
			ChangeReason:          arguments.ChangeReason,
		})
		if err != nil {
			return nil, err
		}
		return artifactResult("storyboard_table", "storyboard_version", version.ID,
			version.VersionNumber, string(version.Status), "storyboard"), nil
	}
}

func bindCreatePanelVersion(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			ItemID           string `json:"itemId"`
			Prompt           string `json:"prompt"`
			NegativePrompt   string `json:"negativePrompt"`
			ChangeReason     string `json:"changeReason"`
			BasedOnVersionID string `json:"basedOnVersionId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		itemID, err := required(arguments.ItemID, "storyboard item")
		if err != nil {
			return nil, err
		}
		item, err := deps.Storyboard.GetStoryboardItem(ctx, itemID)
		if err != nil {
			return nil, err
		}
		version, err := deps.Storyboard.GetStoryboardVersion(ctx, item.StoryboardVersionID)
		if err != nil {
			return nil, err
		}
		board, err := deps.Storyboard.GetStoryboard(ctx, version.StoryboardID)
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, board.EpisodeID); err != nil {
			return nil, err
		}
		createdBy, createdByID := agentActor(request)
		panel, err := deps.Storyboard.CreatePanelVersion(ctx, appstoryboard.CreatePanelVersionRequest{
			StoryboardItemID: itemID,
			BasedOnVersionID: strings.TrimSpace(arguments.BasedOnVersionID),
			VisualPrompt:     arguments.Prompt,
			NegativePrompt:   arguments.NegativePrompt,
			ChangeReason:     arguments.ChangeReason,
			SourceAgentRunID: request.AgentRunID,
			CreatedByType:    createdBy,
			CreatedByID:      createdByID,
		})
		if err != nil {
			return nil, err
		}
		return artifactResult("storyboard_panel_generation", "storyboard_panel_version", panel.ID,
			panel.VersionNumber, string(panel.Status), "storyboard panel"), nil
	}
}

// ---------------------------------------------------------------------------
// Assets
// ---------------------------------------------------------------------------

func bindReadApprovedAssets(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			Types []string `json:"types"`
			Limit int      `json:"limit"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		types, err := assetTypes(arguments.Types)
		if err != nil {
			return nil, err
		}
		records, err := deps.Assets.ListAssets(ctx, appassets.ListFilter{
			ProjectID: request.ProjectID, Types: types,
		})
		if err != nil {
			return nil, err
		}
		limit := limitOf(arguments.Limit, 50, 200)
		views := make([]assetView, 0, len(records))
		for _, record := range records {
			// Only an asset with an APPROVED version is reported. A candidate is not a
			// fact about the project (PRD FR-030), and a downstream stage that built on
			// one would be building on something nobody agreed to.
			if strings.TrimSpace(record.CurrentApprovedVersionID) == "" {
				continue
			}
			views = append(views, assetView{
				ID: record.ID, Type: string(record.Type), Name: record.Name,
				Status: string(record.Status), ApprovedVersionID: record.CurrentApprovedVersionID,
			})
			if len(views) >= limit {
				break
			}
		}
		return map[string]any{"assets": views, "total": len(views)}, nil
	}
}

type assetView struct {
	ID                string `json:"id"`
	Type              string `json:"type,omitempty"`
	Name              string `json:"name"`
	Status            string `json:"status"`
	ApprovedVersionID string `json:"approvedVersionId,omitempty"`
}

func bindCreateCandidateVersion(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			AssetID        string `json:"assetId"`
			Prompt         string `json:"prompt"`
			NegativePrompt string `json:"negativePrompt"`
			MetadataJSON   string `json:"metadataJson"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		assetID, err := required(arguments.AssetID, "asset")
		if err != nil {
			return nil, err
		}
		existing, err := deps.Assets.GetAsset(ctx, assetID)
		if err != nil {
			return nil, err
		}
		// An asset's project is its own field, and it is checked before the write so a
		// model cannot add a version to another project's asset.
		if existing.ProjectID != request.ProjectID {
			return nil, agent.SecurityError("That asset belongs to another project.")
		}
		createdBy, _ := agentActor(request)
		version, err := deps.Assets.AddVersion(ctx, appassets.AddVersionRequest{
			AssetID:        assetID,
			Prompt:         arguments.Prompt,
			NegativePrompt: arguments.NegativePrompt,
			Metadata:       arguments.MetadataJSON,
			CreatedByType:  createdBy,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"stage": "asset_generation",
			"artifacts": []map[string]any{{
				"entityType": "asset_version",
				"entityId":   version.ID,
				"operation":  "created",
			}},
			"versionNumber": version.VersionNumber,
			"status":        string(version.Status),
			"summary":       "Created candidate asset version " + itoaSmall(version.VersionNumber) + ".",
			"note":          "The version is a candidate. Approving it is a user's decision.",
			"nextAction":    "review",
		}, nil
	}
}

// ---------------------------------------------------------------------------
// Memory
// ---------------------------------------------------------------------------

// deepRecall returns the recent messages for the run's own scope.
//
// WP-07's scope item 15 allows the RECENT window and no more: the semantic recall with
// a threshold and a rerank is WP-10's. What this DOES have is the two invariants
// DOMAIN_MODEL section 14.5 states, because a recall that broke one would leak and a
// leak is not something a later package should have to find:
//
//   - the scope comes from the RUN, so a model cannot widen it: there is no project,
//     episode or agent argument;
//   - the current message is excluded, so a turn cannot recall itself.
func bindDeepRecall(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			Limit            int    `json:"limit"`
			ExcludeMessageID string `json:"excludeMessageId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		items, err := deps.Memory.BuildContext(ctx, appmemory.RecallRequest{
			Scope: appmemory.ScopeFor(request.ProjectID, request.EpisodeID, ""),
			// The exclusion is the caller's to state, and the runtime supplies the
			// message being answered. An empty one recalls everything in scope, which is
			// what a first turn in a conversation asks for.
			ExcludeMessageID: strings.TrimSpace(arguments.ExcludeMessageID),
			Limit:            limitOf(arguments.Limit, appmemory.DefaultLimit, appmemory.MaxLimit),
		})
		if err != nil {
			return nil, err
		}
		views := make([]memoryView, 0, len(items))
		for _, item := range items {
			views = append(views, memoryView{
				Role: string(item.Role),
				// The provenance section 12.2 requires recalled context to carry, so a
				// reader can tell which run a remembered message came from.
				FromRun:  item.Provenance,
				Content:  item.Content,
				Recalled: true,
			})
		}
		return map[string]any{"messages": views, "total": len(views), "window": "recent"}, nil
	}
}

type memoryView struct {
	Role    string `json:"role"`
	FromRun string `json:"fromRun,omitempty"`
	Content string `json:"content"`
	// Recalled marks the content as memory rather than as the current turn. Section
	// 5.2 puts history's instructions in the untrusted list, and this is the field the
	// prompt layer reads to know which messages those are.
	Recalled bool `json:"recalled"`
}
