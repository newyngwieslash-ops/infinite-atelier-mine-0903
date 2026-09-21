package scriptpipeline

import (
	"context"
	"encoding/json"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// supervise.go reviews a stage and applies the result, and holds the two commands a user's decision
// turns into.
//
// The three belong together because they are one conversation: the execution writes an artifact, the
// supervisor reads it and reports, and the user decides. Splitting them across files would put the
// policy lookup, the review write and the gate in three places, and the thing that must not drift —
// which stage's review is being applied to which attempt — is exactly what is shared.

// SupervisionRequest asks for one review of one attempt.
type SupervisionRequest struct {
	StageRunID string
	ProjectID  string
	EpisodeID  string
	// ArtifactVersionID is the version the review is about, which the supervisor reads with its own
	// tools. The pipeline does not infer it: a review of the wrong version is a report about an
	// artifact nobody is judging, and the version a stage produced is knowable from the attempt.
	ArtifactVersionID string
	ModelID           string
	ProviderID        string
}

// SupervisionResult is what one review produced.
type SupervisionResult struct {
	StageRun workflow.StageRun
	Outcome  agentruntime.Outcome
	Report   workflow.ReviewReport
	Issues   []workflow.ReviewIssue
}

// RunSupervision reviews one attempt and applies the report to the stage.
//
// The supervisor is chosen by the STATED map rather than by a heuristic (see the package comment): a
// stage whose supervisor could not be resolved is refused, because the alternative is a stage that runs
// unsupervised while its policy says it must be reviewed.
//
// The order is the one AGENT_CONTRACTS section 8 gives: run the supervisor, store its report, then move
// the stage. Storing BEFORE moving matters, because the engine's `ApplySupervision` decides the next
// status from the report's verdict — and a stage moved on a report that was not stored would be a
// transition no reader could reconstruct.
func (s *Service) RunSupervision(ctx context.Context, request SupervisionRequest) (SupervisionResult, error) {
	if !s.Available() {
		return SupervisionResult{}, unavailable()
	}
	stageRunID := trimOrEmpty(request.StageRunID)
	if stageRunID == "" {
		return SupervisionResult{}, agent.InvalidError("A supervision run must name the stage attempt it reviews.")
	}
	attempt, err := s.workflow.GetStage(ctx, stageRunID)
	if err != nil {
		return SupervisionResult{}, err
	}
	agents, ok := AgentsForStage(attempt.Stage)
	if !ok {
		return SupervisionResult{}, agentRefusal(attempt.Stage)
	}
	skill, skillVersion, err := s.skillFor(agents.Supervision)
	if err != nil {
		return SupervisionResult{}, err
	}
	outcome, err := s.runtime.Run(ctx, agentruntime.Invocation{
		AgentKey:      agents.Supervision,
		ProjectID:     trimOrEmpty(request.ProjectID),
		EpisodeID:     trimOrEmpty(request.EpisodeID),
		WorkflowRunID: attempt.WorkflowRunID,
		StageRunID:    attempt.ID,
		Skill:         skill,
		SkillVersion:  skillVersion,
		WorkflowState: s.stateFor(attempt, StageRequest{
			WorkflowRunID: attempt.WorkflowRunID,
			Stage:         attempt.Stage,
			ProjectID:     request.ProjectID,
			EpisodeID:     request.EpisodeID,
		}),
		// The reviewer is TOLD what to read rather than handed the content, which is §6.4's rule that
		// large text travels as a reference: a supervisor that loaded the artifact itself is the
		// property §10.1 asks for, and a prompt carrying the artifact would make that impossible to
		// distinguish from the executor's own summary.
		Task:            supervisionTask(attempt, request),
		TaskIsUntrusted: false,
		ModelID:         trimOrEmpty(request.ModelID),
		ProviderID:      trimOrEmpty(request.ProviderID),
	})
	if err != nil {
		return SupervisionResult{StageRun: attempt, Outcome: outcome}, err
	}
	report, err := reviewFromOutcome(outcome, agents.Supervision)
	if err != nil {
		return SupervisionResult{StageRun: attempt, Outcome: outcome}, err
	}
	// The findings are read from the model's VALIDATED output, which is the shape the review-report
	// contract pins. Reading them here rather than asking the supervisor to call a tool for them is
	// what makes the report and its issues one atomic write: section 7.6 puts the issues INSIDE the
	// report, so a second call could store one without the other.
	issues, err := issuesFromOutcome(outcome)
	if err != nil {
		return SupervisionResult{StageRun: attempt, Outcome: outcome}, err
	}
	stored, storedIssues, err := s.workflow.RecordReview(ctx, appworkflow.RecordReviewRequest{
		StageRunID:        attempt.ID,
		SupervisorKey:     agents.Supervision,
		RulesetVersion:    report.RulesetVersion,
		Passed:            report.Passed,
		Severity:          report.Severity,
		RecommendedAction: report.RecommendedAction,
		Summary:           report.Summary,
		Issues:            issues,
	})
	if err != nil {
		return SupervisionResult{StageRun: attempt, Outcome: outcome}, err
	}
	moved, err := s.engine.ApplySupervision(ctx, agentruntime.RecordSupervisionRequest{
		StageRunID:        attempt.ID,
		Passed:            report.Passed,
		Severity:          report.Severity,
		RecommendedAction: report.RecommendedAction,
		Actor:             stageActor(),
	})
	if err != nil {
		return SupervisionResult{StageRun: attempt, Outcome: outcome, Report: stored, Issues: storedIssues}, err
	}
	return SupervisionResult{StageRun: moved, Outcome: outcome, Report: stored, Issues: storedIssues}, nil
}

// supervisionTask states what the reviewer is asked to review.
//
// It names the artifact by REFERENCE and states the ruleset the report must cite, which are the two
// things the supervision contract requires of the request. It carries no content of the artifact: a
// supervisor loads what it reviews with its own read tools, which is what makes "Supervisor 读取 DB"
// a property rather than a claim.
func supervisionTask(attempt workflow.StageRun, request SupervisionRequest) string {
	parts := []string{
		"Review the " + string(attempt.Stage) + " attempt " + attempt.ID + ".",
	}
	if version := trimOrEmpty(request.ArtifactVersionID); version != "" {
		parts = append(parts, "The artifact under review is version "+version+"; load it with your own read tools.")
	} else {
		parts = append(parts, "Load the artifact this attempt produced with your own read tools.")
	}
	// The ruleset is what the findings are checked against, and §7.6 requires the report to name it.
	// It is stated as a source the reviewer reads rather than as a version to repeat, because the
	// version a PROJECT has is a fact about the project and this pipeline does not read it.
	parts = append(parts, "Cite the ruleset version you reviewed against, and read the project's rules before judging.")
	return strings.Join(parts, " ")
}

// reviewFromOutcome reads the verdict out of a supervisor's validated output.
//
// The output's SHAPE is the review-report contract, which the runtime has already validated, so this
// reads fields the schema guarantees rather than defending against a malformed document. What it does
// defend against is a document that is valid and says nothing usable — an empty stage, for instance —
// which is a defect in the model's answer rather than in the contract.
func reviewFromOutcome(outcome agentruntime.Outcome, supervisorKey string) (workflow.ReviewReport, error) {
	var document struct {
		Passed            bool   `json:"passed"`
		Severity          string `json:"severity"`
		RulesetVersion    string `json:"rulesetVersion"`
		RecommendedAction string `json:"recommendedAction"`
		Summary           string `json:"summary"`
	}
	if err := json.Unmarshal(outcome.Output, &document); err != nil {
		return workflow.ReviewReport{}, agent.InvalidError("The review report could not be read.")
	}
	report := workflow.ReviewReport{
		Passed:            document.Passed,
		Severity:          workflow.Severity(document.Severity),
		RulesetVersion:    strings.TrimSpace(document.RulesetVersion),
		RecommendedAction: strings.TrimSpace(document.RecommendedAction),
		Summary:           document.Summary,
		SupervisorKey:     supervisorKey,
	}
	if report.RulesetVersion == "" {
		// §7.6 requires a ruleset version: a report that named none could not be reproduced, because
		// nothing would say which rules the findings were checked against.
		return workflow.ReviewReport{}, agent.InvalidError("The review report names no ruleset version.")
	}
	return report, nil
}

// issuesFromOutcome reads a report's findings out of the same validated output.
//
// The identifiers are MINTED by the workflow service, not here, so each finding carries the fields the
// report states and nothing that identifies it — the same division the script tools use, and for the
// same reason: an identifier a model supplies is one it can get wrong.
func issuesFromOutcome(outcome agentruntime.Outcome) ([]appworkflow.ReviewIssueInput, error) {
	var document struct {
		Issues []struct {
			Rule         string `json:"rule"`
			Severity     string `json:"severity"`
			EntityType   string `json:"entityType"`
			EntityID     string `json:"entityId"`
			Location     string `json:"location"`
			Field        string `json:"field"`
			Problem      string `json:"problem"`
			Suggestion   string `json:"suggestion"`
			AutoFixable  bool   `json:"autoFixable"`
			EvidenceJSON string `json:"evidenceJson"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(outcome.Output, &document); err != nil {
		return nil, agent.InvalidError("The review report's findings could not be read.")
	}
	out := make([]appworkflow.ReviewIssueInput, 0, len(document.Issues))
	for _, issue := range document.Issues {
		severity := workflow.Severity(strings.TrimSpace(issue.Severity))
		if !workflow.IsValidSeverity(severity) {
			// A finding whose severity is unknown cannot be routed — the engine's "critical 强制人工门"
			// reads it — so it is refused rather than defaulted. Defaulting would silently decide how
			// urgent somebody else's problem is.
			return nil, agent.InvalidError("A finding states a severity that is not recognised.")
		}
		out = append(out, appworkflow.ReviewIssueInput{
			Rule:         strings.TrimSpace(issue.Rule),
			Severity:     severity,
			EntityType:   strings.TrimSpace(issue.EntityType),
			EntityID:     strings.TrimSpace(issue.EntityID),
			Location:     issue.Location,
			Field:        strings.TrimSpace(issue.Field),
			Problem:      issue.Problem,
			Suggestion:   issue.Suggestion,
			EvidenceJSON: issue.EvidenceJSON,
			AutoFixable:  issue.AutoFixable,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// The user's decisions
// ---------------------------------------------------------------------------

// GateRequest carries a user's decision about one attempt.
type GateRequest struct {
	StageRunID  string
	Decision    workflow.GateDecision
	Instruction string
	Reason      string
	// LockedEntityRefsJSON is what the user pinned while deciding, in section 7.3's shape. It is stored
	// on the decision and read back by the next attempt's FIX, which is why it travels as JSON rather
	// than as a decoded value: the pipeline stores what the user's command produced.
	LockedEntityRefsJSON string
	// IssueIDsJSON are the findings a FIX must address, as the JSON array section 12.2 stores on the
	// decision. It is a string rather than a slice because the column is JSON and the pipeline must not
	// re-encode what the user's command stated.
	IssueIDsJSON string
	// ArtifactVersionID names the version an approving decision puts in force, and it is REQUIRED for
	// one that does.
	//
	// It is here because moving a stage and approving an artifact are TWO acts, and AC-SCRIPT-001's
	// "approved 唯一" is about the second: an episode has one approved skeleton version, and the schema
	// enforces that with a partial unique index. A gate that only moved the stage would leave the episode
	// with no approved version while the workflow reported the stage passed — which is what the canary
	// found, and it is not a small thing: every later stage reads the APPROVED version.
	ArtifactVersionID string
	CreatedByID       string
}

// ApplyUserGate records the user's decision and moves the stage.
//
// THE DECISION IS WRITTEN BEFORE THE STAGE MOVES, and the order is what makes a FIX recoverable: the
// findings a user names live on the decision row, and the pipeline that runs the next attempt reads them
// from there. A stage moved to `needs_fix` with no decision row would be a re-run with nothing to fix.
//
// It does NOT start the next attempt. Section 10.2's FIX is "re-runs against specific findings", and
// starting a revision is a separate command with its own budget check — so a caller that wants the
// re-run asks for it explicitly, and a caller that only wants to record the decision (a user closing a
// window) is not forced into running a model.
func (s *Service) ApplyUserGate(ctx context.Context, request GateRequest) (workflow.StageRun, error) {
	if !s.Available() {
		return workflow.StageRun{}, unavailable()
	}
	stageRunID := trimOrEmpty(request.StageRunID)
	if stageRunID == "" {
		return workflow.StageRun{}, agent.InvalidError("A gate decision must name the stage attempt it is about.")
	}
	if !workflow.IsValidGateDecision(request.Decision) {
		return workflow.StageRun{}, agent.InvalidError("That gate decision is not recognised.")
	}
	attempt, err := s.workflow.GetStage(ctx, stageRunID)
	if err != nil {
		return workflow.StageRun{}, err
	}
	// The findings a FIX names are stated by the caller as JSON, and the pipeline does not read them
	// here: the decision row is where they are durable, and parsing them twice would be two chances to
	// disagree. What is checked is that they PARSE, so a malformed list is refused before the row is
	// written rather than discovered by the re-run.
	if _, err := issueIDsOf(request.IssueIDsJSON); err != nil {
		return workflow.StageRun{}, err
	}
	// The decision is written with a USER actor. §11.5's "用户决策不可由 Agent 伪造" is enforced by the
	// domain, so a decision whose author is an agent is refused rather than flagged — and this command is
	// the only route that produces one.
	if _, err := s.workflow.SubmitGateDecision(ctx, appworkflow.SubmitGateDecisionRequest{
		WorkflowRunID:        attempt.WorkflowRunID,
		StageRunID:           attempt.ID,
		Decision:             request.Decision,
		IssueIDsJSON:         request.IssueIDsJSON,
		Instruction:          request.Instruction,
		Reason:               request.Reason,
		LockedEntityRefsJSON: request.LockedEntityRefsJSON,
		CreatedBy:            versioning.CreatedByUser,
		CreatedByID:          trimOrEmpty(request.CreatedByID),
	}); err != nil {
		return workflow.StageRun{}, err
	}
	// The artifact is approved BEFORE the stage moves, and the order is what makes a failure recoverable:
	// an approved version with a stage still waiting for its gate is a state a user can retry from, while
	// a passed stage whose version was never approved is a workflow that claims success and a project with
	// nothing to build from.
	if isApprovingDecision(request.Decision) {
		if err := s.approveArtifact(ctx, attempt, request); err != nil {
			return workflow.StageRun{}, err
		}
	}
	return s.engine.ApplyGate(ctx, agentruntime.ApplyGateRequest{
		StageRunID: attempt.ID,
		Decision:   request.Decision,
		Actor:      userActor(request.CreatedByID),
	})
}

// isApprovingDecision reports whether a decision makes the artifact the one in force.
//
// Three of §10.2's decisions do, and they do it for different reasons: `approve` accepts the artifact,
// `manual_edit` makes the user's own version the artifact, and `skip` moves on without judging it. A
// WAIVER is not in the list because §15.3's waiver accepts a STALE artifact rather than approving a new
// one — the version in force is the one already approved. `fix` and `redo` do not, and `cancel` ends the
// run.
func isApprovingDecision(decision workflow.GateDecision) bool {
	switch decision {
	case workflow.GateApprove, workflow.GateManualEdit, workflow.GateSkip:
		return true
	default:
		return false
	}
}

// approveArtifact makes one version the artifact in force for its family.
//
// The family comes from the STAGE, so a caller cannot approve a version of another kind by naming it: a
// skeleton version approved against the script stage would make "the approved artifact of this stage" a
// statement the database recorded and no reader could reconcile with what the stage produced.
func (s *Service) approveArtifact(ctx context.Context, attempt workflow.StageRun, request GateRequest) error {
	agents, ok := AgentsForStage(attempt.Stage)
	if !ok {
		return agentRefusal(attempt.Stage)
	}
	versionID := trimOrEmpty(request.ArtifactVersionID)
	if versionID == "" {
		// Refused rather than skipped, and the refusal says what is missing: a decision that approves
		// nothing is a gate that did not gate, and the caller's next step is to name the version it
		// reviewed.
		return agent.InvalidError("An approving decision must name the artifact version it approves.")
	}
	traceID := trimOrEmpty(request.CreatedByID)
	switch agents.Family {
	case scriptdomain.FamilyStorySkeleton:
		if _, err := s.script.ApproveStorySkeletonVersion(ctx, appscript.ApproveStorySkeletonVersionRequest{
			VersionID: versionID, TraceID: traceID,
		}); err != nil {
			return err
		}
	case scriptdomain.FamilyAdaptationStrategy:
		if _, err := s.script.ApproveAdaptationStrategyVersion(ctx, appscript.ApproveAdaptationStrategyVersionRequest{
			VersionID: versionID, TraceID: traceID,
		}); err != nil {
			return err
		}
	default:
		if _, err := s.script.ApproveScriptVersion(ctx, appscript.ApproveScriptVersionRequest{
			ScriptVersionID: versionID, TraceID: traceID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// StartRevision begins the attempt a FIX or REDO asked for.
//
// It is a separate command from the gate because it runs a MODEL: a user who decided to fix something has
// not thereby decided to spend a run, and a UI that submitted a decision should not silently start one.
//
// The findings are NOT passed here. They are read from the decision row by `RunStage` when the caller
// names the attempt being revised, which is what makes the FIX survive a restart between the two
// commands — and what makes a FIX that read no findings impossible rather than merely unlikely.
func (s *Service) StartRevision(ctx context.Context, stageRunID string) (workflow.StageRun, error) {
	if !s.Available() {
		return workflow.StageRun{}, unavailable()
	}
	trimmed := trimOrEmpty(stageRunID)
	if trimmed == "" {
		return workflow.StageRun{}, agent.InvalidError("A revision must name the stage attempt it revises.")
	}
	return s.engine.StartRevision(ctx, agentruntime.StageTransitionRequest{
		StageRunID: trimmed,
		Actor:      stageActor(),
	})
}

// ---------------------------------------------------------------------------
// The user's own edit
// ---------------------------------------------------------------------------

// ManualEditRequest asks to write a user's own version of a stage's artifact.
type ManualEditRequest struct {
	StageRunID string
	Stage      Stage
	ProjectID  string
	EpisodeID  string
	// Structure is the user's content for a script version's scenes, lines and shots. It carries no
	// identifiers and no ordinals, exactly as the model's own write does: §17's "ID、顺序和唯一性" is the
	// code's job whoever is writing, and a user editing a script through a form is not stating row ids.
	Structure scriptdomain.ScriptStructureDraft
	// Skeleton and Strategy are the two upstream artifacts, for the stages whose content is a row
	// rather than a structure. Exactly one of the three is used, chosen by the stage.
	Skeleton *appscript.CreateStorySkeletonVersionRequest
	Strategy *appscript.CreateAdaptationStrategyVersionRequest
	Summary  string
	// BasedOnVersionID links the user's version to the one it edited, which is what makes AC-SCRIPT-002's
	// "原版本保留" auditable for a manual edit as well as for a FIX.
	BasedOnVersionID string
	// SkeletonVersionID and StrategyVersionID are what the script stage's version cites. §7.5 makes
	// them FIELDS of the version rather than provenance, so the domain refuses a script version
	// without them — and a user's edit through a form has the same obligation a model's write does.
	SkeletonVersionID string
	StrategyVersionID string
	CreatedByID       string
	ChangeReason      string
}

// ManualEdit writes the user's own version and passes the stage.
//
// AGENT_CONTRACTS section 10.2 makes `manual_edit` end at `passed` rather than at a review: "用户的版本
// 即产物". So this command does two things in one call — write the version, then move the stage — and the
// ORDER is what makes the pair recoverable. A stage passed with no version would be an approval of
// nothing; a version written and then a failed transition leaves a version the user can retry the gate on.
//
// THE VERSION IS ATTRIBUTED TO THE USER, not to the run that happened to be in play. DOMAIN_MODEL §13.4
// distinguishes the two, and PRD FR-100's audit is what the distinction is for: a reviewer has to be able
// to tell which versions came from a model. A manual edit recorded as an agent's would put the user's own
// work in the model's column.
func (s *Service) ManualEdit(ctx context.Context, request ManualEditRequest) (StageResult, error) {
	if !s.Available() {
		return StageResult{}, unavailable()
	}
	attempt, err := s.workflow.GetStage(ctx, trimOrEmpty(request.StageRunID))
	if err != nil {
		return StageResult{}, err
	}
	// The stage in the request must be the stage the attempt is, because the attempt is what the version
	// is written against: a caller that named the wrong stage would have its content written into another
	// stage's artifact and approved under this one's name.
	if request.Stage != "" && request.Stage != attempt.Stage {
		return StageResult{}, agent.InvalidError("That attempt belongs to a different stage than the one named.")
	}
	agents, ok := AgentsForStage(attempt.Stage)
	if !ok {
		return StageResult{}, agentRefusal(attempt.Stage)
	}
	if err := s.assertEpisodeInProject(ctx, request.ProjectID, request.EpisodeID); err != nil {
		return StageResult{}, err
	}
	versionID, err := s.writeUserVersion(ctx, attempt.Stage, agents, request)
	if err != nil {
		return StageResult{}, err
	}
	moved, err := s.ApplyUserGate(ctx, GateRequest{
		StageRunID: attempt.ID,
		Decision:   workflow.GateManualEdit,
		// The user's own version is what the decision approves, which is §10.2's "用户的版本即产物".
		ArtifactVersionID: versionID,
		Instruction:       request.ChangeReason,
		CreatedByID:       request.CreatedByID,
	})
	if err != nil {
		// The version IS written, and the refusal says so by returning the identifier: a caller whose
		// transition failed has a version to look at and a gate to retry, and hiding the write would
		// leave an orphan row nobody knew about.
		return StageResult{StageRun: attempt, ArtifactIDs: []string{versionID}}, err
	}
	return StageResult{StageRun: moved, ArtifactIDs: []string{versionID}}, nil
}

// writeUserVersion writes the version one stage's manual edit produces.
//
// The three stages produce three kinds of row, so the switch is by STAGE rather than by whether a field
// was populated: a caller that supplied a skeleton request for the script stage has used the wrong
// request, and inferring from what was filled in would write a skeleton version into a script stage.
func (s *Service) writeUserVersion(ctx context.Context, stage Stage, agents StageAgents, request ManualEditRequest) (string, error) {
	switch stage {
	case StageStorySkeleton:
		if request.Skeleton == nil {
			return "", agent.InvalidError("A manual edit of the skeleton stage needs a skeleton version.")
		}
		body := *request.Skeleton
		body.EpisodeID = trimOrEmpty(request.EpisodeID)
		body.BasedOnVersionID = trimOrEmpty(request.BasedOnVersionID)
		body.ChangeReason = request.ChangeReason
		body.CreatedByType = versioning.CreatedByUser
		body.CreatedByID = trimOrEmpty(request.CreatedByID)
		version, err := s.script.CreateStorySkeletonVersion(ctx, body)
		if err != nil {
			return "", err
		}
		return version.ID, nil
	case StageAdaptationStrategy:
		if request.Strategy == nil {
			return "", agent.InvalidError("A manual edit of the strategy stage needs a strategy version.")
		}
		body := *request.Strategy
		body.EpisodeID = trimOrEmpty(request.EpisodeID)
		body.BasedOnVersionID = trimOrEmpty(request.BasedOnVersionID)
		body.ChangeReason = request.ChangeReason
		body.CreatedByType = versioning.CreatedByUser
		body.CreatedByID = trimOrEmpty(request.CreatedByID)
		version, err := s.script.CreateAdaptationStrategyVersion(ctx, body)
		if err != nil {
			return "", err
		}
		return version.ID, nil
	default:
		// The script stage: the user's content is a STRUCTURE, and it needs a version row to hang off.
		// The row is created here with the user as its author, because a manual edit is the user writing
		// rather than a model that happens to be in play.
		scriptRecord, err := s.script.EnsureScript(ctx, trimOrEmpty(request.EpisodeID))
		if err != nil {
			return "", err
		}
		version, err := s.script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
			ScriptID:                    scriptRecord.ID,
			BasedOnVersionID:            trimOrEmpty(request.BasedOnVersionID),
			StorySkeletonVersionID:      trimOrEmpty(request.SkeletonVersionID),
			AdaptationStrategyVersionID: trimOrEmpty(request.StrategyVersionID),
			Summary:                     request.Summary,
			CreatedByType:               versioning.CreatedByUser,
			CreatedByID:                 trimOrEmpty(request.CreatedByID),
			ChangeReason:                request.ChangeReason,
		})
		if err != nil {
			return "", err
		}
		if len(request.Structure.Scenes) == 0 {
			// A version row with no content is a manual edit that edited nothing. It is refused HERE
			// rather than left for the structure write, because the version row is already committed by
			// this point and the caller's next step is to supply the scenes — an empty version would
			// read to a reviewer as a script whose content is missing.
			return "", agent.InvalidError("A manual edit of a script needs at least one scene.")
		}
		if _, err := s.script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
			ScriptID:        scriptRecord.ID,
			ScriptVersionID: version.ID,
			// The project travels so the reference check runs: a user's edit cites story events and
			// entities through the form, and those citations are checked like any other.
			ProjectID:     trimOrEmpty(request.ProjectID),
			Draft:         request.Structure,
			Summary:       request.Summary,
			CreatedByType: versioning.CreatedByUser,
			CreatedByID:   trimOrEmpty(request.CreatedByID),
			ChangeReason:  request.ChangeReason,
		}); err != nil {
			return "", err
		}
		return version.ID, nil
	}
}

// assertEpisodeInProject refuses an episode that belongs to another project.
//
// It is the same walk every tool handler makes, and it is here because a manual edit is a WRITE the user
// asked for from a UI that could be showing another project's window. A missing episode is a not-found
// rather than a refusal: one is a stale identifier, the other is an attempt to reach across a boundary.
func (s *Service) assertEpisodeInProject(ctx context.Context, projectID, episodeID string) error {
	project := trimOrEmpty(projectID)
	if project == "" {
		// No project means the caller's own edit through a route that does not state one, which is the
		// same boundary the script service's reference check documents: the check is not broken, it is
		// not being asked.
		return nil
	}
	episode := trimOrEmpty(episodeID)
	if episode == "" {
		return agent.InvalidError("A manual edit must name the episode it writes for.")
	}
	record, err := s.script.GetEpisode(ctx, episode)
	if err != nil {
		return err
	}
	if record.ProjectID != project {
		return agent.SecurityError("That episode belongs to another project.")
	}
	return nil
}

// userActor is the attribution a user's own command records.
func userActor(createdByID string) agentruntime.Actor {
	return agentruntime.Actor{Type: "user", ID: trimOrEmpty(createdByID)}
}
