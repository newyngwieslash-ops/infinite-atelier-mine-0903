package agenttools

import (
	"context"
	"strings"
	"time"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
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

// bindReadScriptStructure returns a version's whole content: its scenes, lines and shots.
//
// It is separate from `script.read_script_version` because the two answer different questions and
// have different weights. The version's own row is small and a supervisor reads it to decide what to
// load; the structure is the artifact, and section 6.4's "返回结构化、限长数据" is why it is bounded
// and paged rather than returned with the row. A stage that needs the content asks for it; a stage
// that needs to know WHICH version exists does not pay for it.
//
// The bound is the SCENE count, and over it the call is REFUSED rather than truncated — the rule the
// whole-content write follows, and the reason is the same: half a script is not a smaller script, it
// is a document whose duration, ordinals and scene count are all wrong in ways a reader cannot see.
func bindReadScriptStructure(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			VersionID string `json:"versionId"`
			SceneFrom int    `json:"sceneFrom"`
			SceneTo   int    `json:"sceneTo"`
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
		script, err := deps.Script.GetScript(ctx, version.ScriptID)
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, script.EpisodeID); err != nil {
			return nil, err
		}
		structure, err := deps.Script.GetScriptStructure(ctx, version.ID)
		if err != nil {
			return nil, err
		}
		from, to := structurePage(arguments.SceneFrom, arguments.SceneTo, len(structure.Scenes))
		if from > to {
			// An empty page is a legal answer — a caller asking for scenes beyond the end gets "there
			// are none there" rather than an error — but a page whose range is INVERTED is a malformed
			// request, and returning an empty list would read as "this version ends sooner than you
			// thought".
			return nil, agent.InvalidError("The requested scene range runs backwards.")
		}
		views := make([]map[string]any, 0, to-from+1)
		for _, scene := range structure.Scenes[from-1 : to] {
			views = append(views, sceneView(scene))
		}
		return map[string]any{
			"versionId": version.ID,
			"episodeId": script.EpisodeID,
			"status":    string(version.Status),
			// The version's own summed duration travels with every page, because it is a property of
			// the whole version and a reader comparing it against the episode's target needs it even
			// when it is looking at one scene.
			"estimatedDurationSeconds": structure.TotalDurationSeconds(),
			"sceneCount":               len(structure.Scenes),
			"sceneFrom":                from,
			"sceneTo":                  to,
			"scenes":                   views,
		}, nil
	}
}

// sceneView renders one scene and its children.
func sceneView(scene scriptdomain.SceneStructure) map[string]any {
	lines := make([]map[string]any, 0, len(scene.DialogueLines))
	for _, line := range scene.DialogueLines {
		lines = append(lines, map[string]any{
			"ordinal":            line.Ordinal,
			"type":               string(line.Type),
			"characterEntityId":  line.CharacterEntityID,
			"text":               line.Text,
			"emotion":            line.Emotion,
			"sourceStoryEventId": line.SourceStoryEventID,
			"locked":             line.Locked,
		})
	}
	shots := make([]map[string]any, 0, len(scene.Shots))
	for _, shot := range scene.Shots {
		shots = append(shots, map[string]any{
			"ordinal":           shot.Ordinal,
			"shotNumber":        shot.ShotNumber,
			"visualDescription": shot.VisualDescription,
			"status":            string(shot.Status),
		})
	}
	return map[string]any{
		"sceneId":              scene.ID,
		"ordinal":              scene.Ordinal,
		"sceneNumber":          scene.SceneNumber,
		"slugline":             scene.Slugline,
		"interiorExterior":     string(scene.InteriorExterior),
		"locationEntityId":     scene.LocationEntityID,
		"timeOfDay":            scene.TimeOfDay,
		"summary":              scene.Summary,
		"dramaticGoal":         scene.DramaticGoal,
		"durationSeconds":      scene.EstimatedDurationSeconds,
		"sourceStoryEventId":   scene.SourceStoryEventID,
		"isOriginalAdaptation": scene.IsOriginalAdaptation,
		"dialogueLines":        lines,
		"shots":                shots,
	}
}

// structurePage resolves a caller's scene range into an inclusive window of 1-based ordinals.
//
// A page is CLAMPED rather than refused when it runs past the end, because a caller that asked for
// scenes 1..50 of a 12-scene version has asked a well-formed question whose answer is "twelve": the
// bound is the runtime's, and section 6.4's paging exists so a caller does not have to know the size
// in advance. An INVERTED range is the caller's own error and is refused by the caller.
func structurePage(from, to, total int) (int, int) {
	if total == 0 {
		// A version whose content was never written. There is no page to return, and 1..0 is the
		// empty range the caller's own check turns into a refusal — the honest answer, because
		// "this version has no structure" is not something a page of it can express.
		return 1, 0
	}
	if from <= 0 {
		from = 1
	}
	if from > total {
		from = total
	}
	if to <= 0 || to > total {
		to = total
	}
	return from, to
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
			EpisodeID                string   `json:"episodeId"`
			BasedOnVersionID         string   `json:"basedOnVersionId"`
			OpeningHook              string   `json:"openingHook"`
			CoreConflict             string   `json:"coreConflict"`
			TurningPointsJSON        string   `json:"turningPointsJson"`
			Climax                   string   `json:"climax"`
			EndingHook               string   `json:"endingHook"`
			EstimatedDurationSeconds int      `json:"estimatedDurationSeconds"`
			SelectedEventIDs         []string `json:"selectedEventIds"`
			ChangeReason             string   `json:"changeReason"`
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
			// The selected events are written as §7.4's LINK TABLE rather than as part of the JSON
			// turning points: which events an episode contains is a queryable relation, and §2.6
			// forbids carrying queryable state in Markdown or JSON. The service checks each id against
			// the project and refuses a set that names an event which does not exist.
			SelectedEventIDs: arguments.SelectedEventIDs,
			SourceAgentRunID: request.AgentRunID,
			CreatedByType:    createdBy,
			CreatedByID:      createdByID,
			ChangeReason:     arguments.ChangeReason,
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
			EpisodeID             string               `json:"episodeId"`
			BasedOnVersionID      string               `json:"basedOnVersionId"`
			StrategySummary       string               `json:"strategySummary"`
			AdaptationMode        string               `json:"adaptationMode"`
			MergedEventGroupsJSON string               `json:"mergedEventGroupsJson"`
			OriginalAdditions     string               `json:"originalAdditions"`
			Rationale             string               `json:"rationale"`
			Risks                 string               `json:"risks"`
			EventLinks            []scriptEventLinkArg `json:"eventLinks"`
			ChangeReason          string               `json:"changeReason"`
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
		// The treatments are validated HERE rather than left to the database, because the treatment
		// column has a CHECK and a value that reached it would come back as a constraint failure
		// instead of a refusal a model can act on. The ORDINAL is not taken from the arguments: the
		// array's own order is the adaptation's order, which is the only thing §7.5's "reordered" can
		// mean, and a payload that stated both could state two different ones.
		links := make([]scriptdomain.StrategyEventLink, 0, len(arguments.EventLinks))
		for _, link := range arguments.EventLinks {
			eventID := strings.TrimSpace(link.StoryEventID)
			if eventID == "" {
				continue
			}
			treatment := scriptdomain.EventTreatment(strings.TrimSpace(link.Treatment))
			if !scriptdomain.IsValidEventTreatment(treatment) {
				return nil, agent.InvalidError("That story event treatment is not recognised: " + link.Treatment + ".")
			}
			links = append(links, scriptdomain.StrategyEventLink{
				StoryEventID: eventID,
				Treatment:    treatment,
			})
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
			EventLinks:            links,
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

// scriptEventLinkArg is one strategy treatment as a model states it.
//
// It has no ordinal, for the reason the handler comment gives: the array's position IS the order.
type scriptEventLinkArg struct {
	StoryEventID string `json:"storyEventId"`
	Treatment    string `json:"treatment"`
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

// bindCreateScriptStructure writes a version's whole content: its scenes, lines and shots.
//
// IT TAKES NO IDENTIFIERS AND NO ORDINALS, and that is AGENT_CONTRACTS §17 rather than a schema
// shortcut. "ID、顺序和唯一性" is code's business, so the payload states the shape and the service
// mints every id, numbers every ordinal from its position, and attaches every line and shot to the
// scene it was nested under. A model cannot invent a scene id, and it cannot produce a gap in the
// ordinals, because it never states either.
//
// It also takes no DURATION, for the same reason: §17 puts 时长求和 in the code's column, and the
// version's total is derived by summing its scenes. A declared total and a computed one can disagree
// while only one of them is checkable.
//
// The version must already exist — `script.create_script_version` writes the row and this fills it
// in — because a version IS its identity and its status, and a content write that created the row
// would have to decide both.
func bindCreateScriptStructure(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			EpisodeID        string                    `json:"episodeId"`
			VersionID        string                    `json:"versionId"`
			BasedOnVersionID string                    `json:"basedOnVersionId"`
			Summary          string                    `json:"summary"`
			ChangeReason     string                    `json:"changeReason"`
			Scenes           []scriptStructureSceneArg `json:"scenes"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		versionID, err := required(arguments.VersionID, "script version")
		if err != nil {
			return nil, err
		}
		// The version is read FIRST so its episode can be checked against the run's project. A content
		// write is the largest write a stage makes, so a scope error here would be the worst one: a
		// hundred rows of another project's script.
		version, err := deps.Script.GetScriptVersion(ctx, versionID)
		if err != nil {
			return nil, err
		}
		script, err := deps.Script.GetScript(ctx, version.ScriptID)
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, script.EpisodeID); err != nil {
			return nil, err
		}
		// A caller that ALSO named an episode must be naming this one, otherwise the argument is a
		// second, unchecked statement of where the write goes.
		if stated := strings.TrimSpace(arguments.EpisodeID); stated != "" && stated != script.EpisodeID {
			return nil, agent.InvalidError("That version belongs to a different episode than the one named.")
		}
		draft, err := scriptDraftFromArguments(arguments.Scenes)
		if err != nil {
			return nil, err
		}
		updated, err := deps.Script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
			ScriptID:        script.ID,
			ScriptVersionID: version.ID,
			// The BASE is not taken from the arguments: the service reads the version row's own
			// `based_on_version_id`, which a caller cannot restate. The field is passed through only
			// so a caller's intent is visible in the record.
			BasedOnVersionID: strings.TrimSpace(arguments.BasedOnVersionID),
			Draft:            draft,
			// The project is what makes the reference check possible: a scene citing a story event or
			// entity is checked against THIS project, and the run's own project is the only one it can
			// legitimately be.
			ProjectID:        request.ProjectID,
			SourceAgentRunID: request.AgentRunID,
			Summary:          arguments.Summary,
			CreatedByType:    createdByAgent,
			CreatedByID:      request.AgentRunID,
			ChangeReason:     arguments.ChangeReason,
		})
		if err != nil {
			return nil, err
		}
		result := artifactResult("script_generation", "script_version", updated.ID,
			updated.VersionNumber, string(updated.Status), "script")
		// The DERIVED duration is reported, because it is the number a supervisor compares against the
		// episode's target and the number a reviewer sees: reporting a count of scenes the model wrote
		// would be a second statement of the same fact, and the model's count is not the one stored.
		result["estimatedDurationSeconds"] = updated.EstimatedDurationSeconds
		result["sceneCount"] = len(arguments.Scenes)
		return result, nil
	}
}

// scriptStructureSceneArg is one scene as a model states it: the shape, with no ids and no ordinals.
type scriptStructureSceneArg struct {
	SceneNumber              string                   `json:"sceneNumber"`
	Slugline                 string                   `json:"slugline"`
	InteriorExterior         string                   `json:"interiorExterior"`
	LocationEntityID         string                   `json:"locationEntityId"`
	TimeOfDay                string                   `json:"timeOfDay"`
	Summary                  string                   `json:"summary"`
	DramaticGoal             string                   `json:"dramaticGoal"`
	EstimatedDurationSeconds int                      `json:"estimatedDurationSeconds"`
	SourceStoryEventID       string                   `json:"sourceStoryEventId"`
	IsOriginalAdaptation     bool                     `json:"isOriginalAdaptation"`
	DialogueLines            []scriptStructureLineArg `json:"dialogueLines"`
	Shots                    []scriptStructureShotArg `json:"shots"`
}

// scriptStructureLineArg is one dialogue line as a model states it.
type scriptStructureLineArg struct {
	Type               string `json:"type"`
	CharacterEntityID  string `json:"characterEntityId"`
	Text               string `json:"text"`
	Emotion            string `json:"emotion"`
	PerformanceNote    string `json:"performanceNote"`
	SourceStoryEventID string `json:"sourceStoryEventId"`
}

// scriptStructureShotArg is one shot as a model states it.
type scriptStructureShotArg struct {
	ShotNumber               string `json:"shotNumber"`
	ShotSize                 string `json:"shotSize"`
	CameraAngle              string `json:"cameraAngle"`
	CameraMovement           string `json:"cameraMovement"`
	EstimatedDurationSeconds int    `json:"estimatedDurationSeconds"`
	VisualDescription        string `json:"visualDescription"`
	ActionDescription        string `json:"actionDescription"`
	AudioIntent              string `json:"audioIntent"`
	ContinuityNotes          string `json:"continuityNotes"`
}

// scriptDraftFromArguments converts the arguments into the domain's draft and validates the two
// closed vocabularies the schema cannot express as an enum over an optional field.
//
// The conversion is not a formality: the interior marking and the line type are CHECK constraints in
// SQL, so a value this function let through would reach the database and come back as a constraint
// failure rather than as a refusal naming the field. Both are checked HERE so the model is told what
// it got wrong.
func scriptDraftFromArguments(scenes []scriptStructureSceneArg) (scriptdomain.ScriptStructureDraft, error) {
	if len(scenes) == 0 {
		return scriptdomain.ScriptStructureDraft{}, agent.InvalidError("The script version needs at least one scene.")
	}
	if len(scenes) > scriptdomain.MaxScenesPerVersion {
		return scriptdomain.ScriptStructureDraft{}, agent.InvalidError("That script version has more scenes than one call may write.")
	}
	draft := scriptdomain.ScriptStructureDraft{
		Scenes: make([]scriptdomain.SceneDraft, 0, len(scenes)),
	}
	for _, scene := range scenes {
		interior := strings.TrimSpace(scene.InteriorExterior)
		marking := scriptdomain.InteriorExterior(interior)
		if interior == "" {
			marking = scriptdomain.InteriorOTHER
		} else if !scriptdomain.IsValidInteriorExterior(marking) {
			return scriptdomain.ScriptStructureDraft{}, agent.InvalidError(
				"That interior/exterior marking is not recognised: " + interior + ".")
		}
		if len(scene.DialogueLines) > scriptdomain.MaxLinesPerScene {
			return scriptdomain.ScriptStructureDraft{}, agent.InvalidError("A scene has more dialogue lines than one call may write.")
		}
		if len(scene.Shots) > scriptdomain.MaxShotsPerScene {
			return scriptdomain.ScriptStructureDraft{}, agent.InvalidError("A scene has more shots than one call may write.")
		}
		entry := scriptdomain.SceneDraft{
			SceneNumber:              strings.TrimSpace(scene.SceneNumber),
			Slugline:                 scene.Slugline,
			InteriorExterior:         marking,
			LocationEntityID:         strings.TrimSpace(scene.LocationEntityID),
			TimeOfDay:                scene.TimeOfDay,
			Summary:                  scene.Summary,
			DramaticGoal:             scene.DramaticGoal,
			EstimatedDurationSeconds: scene.EstimatedDurationSeconds,
			SourceStoryEventID:       strings.TrimSpace(scene.SourceStoryEventID),
			IsOriginalAdaptation:     scene.IsOriginalAdaptation,
			DialogueLines:            make([]scriptdomain.DialogueLineDraft, 0, len(scene.DialogueLines)),
			Shots:                    make([]scriptdomain.ShotDraft, 0, len(scene.Shots)),
		}
		if entry.EstimatedDurationSeconds < 0 {
			return scriptdomain.ScriptStructureDraft{}, agent.InvalidError("A scene's duration cannot be negative.")
		}
		for _, line := range scene.DialogueLines {
			stated := strings.TrimSpace(line.Type)
			lineType := scriptdomain.LineType(stated)
			if stated == "" {
				lineType = scriptdomain.LineDialogue
			} else if !scriptdomain.IsValidLineType(lineType) {
				return scriptdomain.ScriptStructureDraft{}, agent.InvalidError("That dialogue line type is not recognised: " + stated + ".")
			}
			entry.DialogueLines = append(entry.DialogueLines, scriptdomain.DialogueLineDraft{
				Type:               lineType,
				CharacterEntityID:  strings.TrimSpace(line.CharacterEntityID),
				Text:               line.Text,
				Emotion:            line.Emotion,
				PerformanceNote:    line.PerformanceNote,
				SourceStoryEventID: strings.TrimSpace(line.SourceStoryEventID),
			})
		}
		for _, shot := range scene.Shots {
			if shot.EstimatedDurationSeconds < 0 {
				return scriptdomain.ScriptStructureDraft{}, agent.InvalidError("A shot's duration cannot be negative.")
			}
			entry.Shots = append(entry.Shots, scriptdomain.ShotDraft{
				ShotNumber:               strings.TrimSpace(shot.ShotNumber),
				ShotSize:                 shot.ShotSize,
				CameraAngle:              shot.CameraAngle,
				CameraMovement:           shot.CameraMovement,
				EstimatedDurationSeconds: shot.EstimatedDurationSeconds,
				VisualDescription:        shot.VisualDescription,
				ActionDescription:        shot.ActionDescription,
				AudioIntent:              shot.AudioIntent,
				ContinuityNotes:          shot.ContinuityNotes,
			})
		}
		draft.Scenes = append(draft.Scenes, entry)
	}
	return draft, nil
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

// artifactResultWithItems is `artifactResult` plus the rows a write produced.
//
// The storyboard table's write is the one tool whose result is a SET rather than a single
// artifact: FR-070 makes the rows the board's content, and a result naming only the
// version would leave a reader unable to tell a board of twelve shots from an empty one.
// The version is still the FIRST artifact, because that is the row a gate approves.
func artifactResultWithItems(stage, entityType, entityID string, versionNumber int, status, label string, itemIDs []string) map[string]any {
	result := artifactResult(stage, entityType, entityID, versionNumber, status, label)
	result["itemIds"] = itemIDs
	result["itemCount"] = len(itemIDs)
	if len(itemIDs) == 0 {
		// An empty board is stated as such rather than left to be inferred from a zero:
		// a caller reading "itemCount: 0" and one reading a missing field make different
		// decisions, and only one of them is right.
		result["summary"] = "Created " + label + " version " + itoaSmall(versionNumber) + " with no rows."
		return result
	}
	result["summary"] = "Created " + label + " version " + itoaSmall(versionNumber) +
		" with " + itoaSmall(len(itemIDs)) + " rows."
	return result
}

// recordShotAssetUsage records that a storyboard row uses an asset.
//
// §7.8 normalises the reference ("Shot 与资产引用通过 AssetUsage/ShotAssetReference 正规化"),
// so a row's asset refs are USAGES rather than a column — and a usage names an asset
// VERSION rather than an asset, because §8.6's consumer consumes a version. The version is
// the asset's APPROVED one, which is the only kind §9.5 lets a panel's image derive from:
// a reference to a candidate would make a board depend on an image no user has accepted.
//
// It is refused rather than skipped when nothing is approved, and the refusal names the
// asset: a row citing an asset with no approved version looks complete and would render
// nothing, which is exactly the defect a supervisor would report later and more expensively.
func recordShotAssetUsage(ctx context.Context, deps Deps, projectID, itemID, assetID, usageRole string) error {
	if err := contextDone(ctx); err != nil {
		return err
	}
	id, err := required(assetID, "asset")
	if err != nil {
		return err
	}
	record, err := deps.Assets.GetAsset(ctx, id)
	if err != nil {
		return err
	}
	if record.ProjectID != projectID {
		return agent.SecurityError("That asset belongs to another project.")
	}
	approved := strings.TrimSpace(record.CurrentApprovedVersionID)
	if approved == "" {
		return agent.InvalidError("The asset " + record.Name + " has no approved version, so a storyboard row cannot cite it yet.")
	}
	role := strings.TrimSpace(usageRole)
	if role == "" {
		role = "reference"
	}
	// The consumer is the SHOT rather than the item: §8.6's vocabulary names a shot, and
	// the item is a board's row about that shot — the asset is what the picture uses, and
	// the picture is the shot.
	if _, err := deps.Assets.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: approved,
		ConsumerType:   asset.ConsumerShot,
		ConsumerID:     itemID,
		UsageRole:      role,
		Required:       true,
	}); err != nil {
		return err
	}
	return nil
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
			// Items are the board's rows, one per shot. WP-09 added them, because FR-070
			// makes every one of these fields a MUST per shot and the tool previously wrote
			// only the version row — so a model asked to board a script produced a VERSION
			// with no rows and the board was empty.
			Items []struct {
				ShotID                 string `json:"shotId"`
				ShotSize               string `json:"shotSize"`
				CameraAngle            string `json:"cameraAngle"`
				CameraMovement         string `json:"cameraMovement"`
				DurationSeconds        int    `json:"durationSeconds"`
				VisualDescription      string `json:"visualDescription"`
				ActionDescription      string `json:"actionDescription"`
				DialogueAudioSummary   string `json:"dialogueAudioSummary"`
				ContinuityNotes        string `json:"continuityNotes"`
				FirstFrameDescription  string `json:"firstFrameDescription"`
				LastFrameDescription   string `json:"lastFrameDescription"`
				VideoMotionDescription string `json:"videoMotionDescription"`
				AssetRefs              []struct {
					AssetID   string `json:"assetId"`
					UsageRole string `json:"usageRole"`
				} `json:"assetRefs"`
			} `json:"items"`
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
		// THE CITED VERSIONS MUST BE IN FORCE (T14, 2026-09-26 audit): a board
		// drawn from a draft or rejected script is a board of nothing — the
		// stage's own approval is not the same fact as the cited version's,
		// and reading only the episode passed both before. `approved` is the
		// exact requirement: `IsContentFrozen` would also admit a superseded
		// version, which a board may cite for HISTORY but must not build on.
		if scriptVersion.Status != versioning.StatusApproved {
			return nil, agent.InvalidError("The cited script version is not approved, so a storyboard cannot be drawn from it.")
		}
		plan, err := deps.Storyboard.GetDirectorPlanVersion(ctx, planVersionID)
		if err != nil {
			return nil, err
		}
		if plan.EpisodeID != episodeID {
			return nil, agent.InvalidError("The director plan version belongs to a different episode.")
		}
		if plan.Status != versioning.StatusApproved {
			return nil, agent.InvalidError("The cited director plan version is not approved, so a storyboard cannot be drawn from it.")
		}
		// The shots the rows must cite are read BEFORE the version is written, so a row
		// naming a shot that does not belong to this script is refused while the write is
		// still one call — `storyboard_items.shot_id` has NO foreign key (section 9.4
		// normalises the reference away), so the database would accept an invented one.
		shotsInScript := map[string]bool{}
		if len(arguments.Items) > 0 {
			structure, err := deps.Script.GetScriptStructure(ctx, scriptVersion.ID)
			if err != nil {
				return nil, err
			}
			for _, scene := range structure.Scenes {
				for _, shot := range scene.Shots {
					shotsInScript[shot.ID] = true
				}
			}
		}
		// The ordinals come from the ARRAY'S ORDER, and a repeated shot is refused here
		// rather than by the schema: `UNIQUE (storyboard_version_id, shot_id)` would
		// reject it, but the refusal would name a constraint rather than the row.
		seenShots := map[string]bool{}
		for index, item := range arguments.Items {
			shotID := strings.TrimSpace(item.ShotID)
			if shotID == "" {
				return nil, agent.InvalidError("Every storyboard row must name the shot it boards.")
			}
			if seenShots[shotID] {
				return nil, agent.InvalidError("A shot appears twice in this board, so its duration and position would be ambiguous.")
			}
			seenShots[shotID] = true
			if !shotsInScript[shotID] {
				return nil, agent.InvalidError("Row " + itoaSmall(index+1) + " names a shot that does not belong to this script version.")
			}
			if item.DurationSeconds < 0 {
				return nil, agent.InvalidError("A storyboard row cannot have a negative duration.")
			}
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
		itemIDs := make([]string, 0, len(arguments.Items))
		for index, item := range arguments.Items {
			row, err := deps.Storyboard.CreateStoryboardItem(ctx, appstoryboard.CreateStoryboardItemRequest{
				StoryboardVersionID:  version.ID,
				ShotID:               strings.TrimSpace(item.ShotID),
				Ordinal:              index + 1,
				ShotSize:             item.ShotSize,
				CameraAngle:          item.CameraAngle,
				CameraMovement:       item.CameraMovement,
				DurationSeconds:      item.DurationSeconds,
				VisualDescription:    item.VisualDescription,
				ActionDescription:    item.ActionDescription,
				DialogueAudioSummary: item.DialogueAudioSummary,
				ContinuityNotes:      item.ContinuityNotes,
				// FR-070's three, added by migration 000018. They are the SHOOTING
				// decisions a video model is given, which is why they are the item's
				// rather than the script shot's.
				FirstFrameDescription:  item.FirstFrameDescription,
				LastFrameDescription:   item.LastFrameDescription,
				VideoMotionDescription: item.VideoMotionDescription,
			})
			if err != nil {
				return nil, err
			}
			itemIDs = append(itemIDs, row.ID)
			// The asset references are recorded as USAGES rather than as a column, because
			// section 7.8 normalises them: "Shot 与资产引用通过 AssetUsage/ShotAssetReference
			// 正规化". A row naming an asset that has no approved version is refused by the
			// resolution below rather than stored as a dangling reference.
			for _, ref := range item.AssetRefs {
				if err := recordShotAssetUsage(ctx, deps, request.ProjectID, row.ID, ref.AssetID, ref.UsageRole); err != nil {
					return nil, err
				}
			}
		}
		return artifactResultWithItems("storyboard_table", "storyboard_version", version.ID,
			version.VersionNumber, string(version.Status), "storyboard", itemIDs), nil
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

// deepRecall walks from a question to the original messages, which is AGENT_CONTRACTS
// section 12.3's Deep Recall Tool.
//
// # What it was, and why that was a defect
//
// WP-07 registered this key with the RECENT window and a comment promising WP-10 would deepen
// it: "the name keeps the section's spelling so a skill does not change when WP-10 deepens it".
// WP-10 built the deep recall — the summary search, the threshold, the rerank, the walk back to
// the sources — as `memory.Service.DeepRecall`, and then did not connect it here. So the tool
// whose whole purpose is history returned the last twenty turns, the two skills that grant it
// documented the recent window, and section 12.3's flow had no caller that a model could reach.
// An independent review found it; this is the correction.
//
// # The two shapes it answers
//
// A call WITH a query runs section 12.3's flow: summary candidates, filtered by the threshold,
// reranked, their sources loaded, with provenance. A call with NO query keeps the recent window,
// because that is a legitimate thing to ask a memory for and it is what every caller before this
// change got. The `window` field says which one answered, so a model reading the result can tell
// whether it got history or the tail of the conversation.
//
// # The two invariants that were already here, and stay
//
// DOMAIN_MODEL section 14.5's rules, which a recall that broke either of would leak:
//
//   - the scope comes from the RUN, so a model cannot widen it: there is no project, episode or
//     agent argument, and the episode and agent are the run's own;
//   - the current message is excluded, so a turn cannot recall itself.
func bindDeepRecall(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			Query            string `json:"query"`
			MaxSummaries     int    `json:"maxSummaries"`
			MaxRawMessages   int    `json:"maxRawMessages"`
			Limit            int    `json:"limit"`
			ExcludeMessageID string `json:"excludeMessageId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		// The run's scope, with the agent key this time: section 12.3's walk loads a summary's
		// sources, and a summary belongs to the conversation that produced it. The recalled
		// layer of WP-07 passed an empty agent key, which made one agent's recall span every
		// agent's turns in the episode.
		scope := appmemory.ScopeFor(request.ProjectID, request.EpisodeID, request.AgentKey)
		if query := strings.TrimSpace(arguments.Query); query != "" {
			result, err := deps.Memory.DeepRecall(ctx, appmemory.DeepRecallRequest{
				Scope:          scope,
				Query:          query,
				MaxSummaries:   arguments.MaxSummaries,
				MaxRawMessages: arguments.MaxRawMessages,
			})
			if err != nil {
				return nil, err
			}
			summaries := make([]memorySummaryView, 0, len(result.Summaries))
			for _, candidate := range result.Summaries {
				summaries = append(summaries, memorySummaryView{
					MemoryID:   candidate.Item.ID,
					Content:    candidate.Item.Content,
					Score:      candidate.Score,
					Similarity: candidate.Similarity,
					Pinned:     candidate.Pinned,
				})
			}
			views := make([]memoryView, 0, len(result.Messages))
			provenance := make([]memoryProvenanceView, 0, len(result.Provenance))
			for index, message := range result.Messages {
				views = append(views, memoryView{
					Role:     string(message.Role),
					FromRun:  message.AgentKey,
					Content:  message.Content,
					Recalled: true,
				})
				if index < len(result.Provenance) {
					entry := result.Provenance[index]
					provenance = append(provenance, memoryProvenanceView{
						// The two hops section 12.3 asks to be visible: the message it came from and
						// the summary the walk selected it through.
						MessageID:    entry.MessageID,
						SummarizedBy: entry.SummarizedBy,
						Role:         entry.Role,
						AgentKey:     entry.AgentKey,
						CreatedAt:    entry.CreatedAt.UTC().Format(time.RFC3339),
					})
				}
			}
			return map[string]any{
				"query":      query,
				"summaries":  summaries,
				"messages":   views,
				"provenance": provenance,
				"total":      len(views),
				"usedTokens": result.UsedTokens,
				"truncated":  result.Truncated,
				"window":     "deep_recall",
			}, nil
		}
		// No query: the recent window, which is what a caller that just wants the conversation
		// so far is asking for.
		items, err := deps.Memory.BuildContext(ctx, appmemory.RecallRequest{
			Scope: scope,
			// The exclusion is the caller's to state, and the runtime supplies the message being
			// answered. An empty one recalls everything in scope, which is what a first turn asks.
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
				// The provenance section 12.2 requires recalled context to carry, so a reader can
				// tell which run a remembered message came from.
				FromRun:  item.Provenance,
				Content:  item.Content,
				Recalled: true,
			})
		}
		return map[string]any{"messages": views, "total": len(views), "window": "recent"}, nil
	}
}

// memorySummaryView is one summary the walk selected.
//
// The two numbers are separate here for the reason the preview separates them: the threshold
// applies to the similarity, and a model reasoning about why a summary was selected needs the
// number the threshold was compared against rather than only the fused score.
type memorySummaryView struct {
	MemoryID   string  `json:"memoryId"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
	Similarity float64 `json:"similarity"`
	Pinned     bool    `json:"pinned,omitempty"`
}

// memoryProvenanceView is where one restored message came from.
type memoryProvenanceView struct {
	MessageID    string `json:"messageId,omitempty"`
	SummarizedBy string `json:"summarizedBy,omitempty"`
	Role         string `json:"role,omitempty"`
	AgentKey     string `json:"agentKey,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
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
