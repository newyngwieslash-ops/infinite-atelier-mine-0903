package stagepipeline

import (
	"context"
	"encoding/json"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// Service drives one agent layer's stages.
//
// Every dependency is required, and their absence is a REFUSAL for every command rather
// than a degraded mode: a pipeline that ran a stage without a supervisor would be
// skipping the review its policy requires, which is not something a caller can see in the
// result.
type Service struct {
	engine   *agentruntime.Engine
	runtime  *agentruntime.Runtime
	workflow *appworkflow.Service
	assembly SkillSource
	runs     RunReader
	layer    Layer
	episodes EpisodeProjectLookup
}

// SkillSource is what the pipeline reads a skill from.
//
// It is an interface rather than the concrete assembly so a test can supply documents
// directly, which is what lets this package be exercised without a pack directory, a
// database or a provider. The assembly satisfies it; nothing else in the build does.
type SkillSource interface {
	// SkillDocument returns the document text for one agent key.
	SkillDocument(agentKey string) (string, bool)
	// SkillVersionOf returns the version a run should cite for that agent.
	SkillVersionOf(agentKey string) (string, bool)
}

// RunReader is the run record's read side, declared as the runtime's own port rather than
// as the Inspector: the Inspector is a presentation surface for the Agent Center, and this
// package needs rows and tool calls rather than a summarised view.
type RunReader = agentruntime.RunReader

// Options configures a Service.
//
// `Layer` and `Episodes` are separated from the rest because they are what make one
// Service a script pipeline and another a production pipeline: the mechanism is the same
// and the knowledge is the caller's.
type Options struct {
	Engine   *agentruntime.Engine
	Runtime  *agentruntime.Runtime
	Workflow *appworkflow.Service
	Assembly SkillSource
	Runs     RunReader
	Layer    Layer
	// Episodes answers which project an episode belongs to, for the manual edit's scope
	// check. It is required: a manual edit without it could write into another project's
	// episode, which is the one boundary a user-triggered write must not cross.
	Episodes EpisodeProjectLookup
}

// New builds a Service.
func New(options Options) *Service {
	return &Service{
		engine:   options.Engine,
		runtime:  options.Runtime,
		workflow: options.Workflow,
		assembly: options.Assembly,
		runs:     options.Runs,
		layer:    options.Layer,
		episodes: options.Episodes,
	}
}

// Available reports whether the pipeline can drive anything.
func (s *Service) Available() bool {
	return s != nil && s.engine != nil && s.runtime != nil && s.workflow != nil &&
		s.assembly != nil && s.runs != nil && s.layer != nil
}

// LayerName names the layer this service drives, for a caller's messages and records.
func (s *Service) LayerName() string {
	if s == nil || s.layer == nil {
		return ""
	}
	return s.layer.Name()
}

// Episodes returns the episode lookup the manual edit's scope check runs against.
//
// It is exported because a LAYER's own test has to assert that the check is WIRED: WP-09's
// extraction moved the script layer's direct `GetEpisode` call behind this port, and a port
// nothing satisfies would have disabled a project-scope check whose failure mode is silent
// — it refuses only when a caller states a project, which no existing test did.
//
// A build with no lookup is reported as nil rather than as an error, so a test can tell the
// two apart: the missing value is the defect, and an "unavailable" sentinel would read like
// one.
func (s *Service) Episodes() EpisodeProjectLookup {
	if s == nil {
		return nil
	}
	return s.episodes
}

// stageAgents resolves a stage, refusing one this layer does not drive.
func (s *Service) stageAgents(stage Stage) (StageAgents, error) {
	agents, ok := s.layer.AgentsFor(stage)
	if !ok {
		return StageAgents{}, agent.InvalidError(
			"This pipeline does not drive the stage " + string(stage) + ".")
	}
	return agents, nil
}

// RunStage starts one attempt, runs it, and parks the stage for review.
//
// A REFUSAL LEAVES THE ATTEMPT WHERE THE RUNTIME PUT IT. The attempt was created and
// moved to `running`, and a model failure does not undo that: the stage's record has to
// say an attempt happened, or a reader would see a stage that silently did nothing. The
// runtime has already recorded the run's own status; what this method does not do is move
// the stage onward, because a failed run has no review.
func (s *Service) RunStage(ctx context.Context, request StageRequest) (StageResult, error) {
	if !s.Available() {
		return StageResult{}, unavailable()
	}
	// The stage is resolved FIRST, so a caller that used the wrong pipeline learns that
	// before anything is read, written or run.
	agents, err := s.stageAgents(request.Stage)
	if err != nil {
		return StageResult{}, err
	}
	workflowRunID := trimOrEmpty(request.WorkflowRunID)
	if workflowRunID == "" {
		return StageResult{}, agent.InvalidError("A stage run must name the workflow run it belongs to.")
	}
	// The revision's findings and pins are read BEFORE the attempt exists, because an
	// attempt created and then refused for a missing decision would be a stage in play with
	// nothing to run — and a caller could not tell that from a slow model.
	lockedRefs, fixIssueIDs, err := s.revisionContext(ctx, request.Stage, request.FixFromStageRunID)
	if err != nil {
		return StageResult{}, err
	}
	skill, skillVersion, err := s.skillFor(agents.Execution)
	if err != nil {
		return StageResult{}, err
	}
	attempt, err := s.engine.StartStage(ctx, agentruntime.StartStageRequest{
		WorkflowRunID: workflowRunID,
		Stage:         request.Stage,
		ExecutionKey:  agents.Execution,
		InputJSON:     stageInput(request),
		Actor:         s.actor(),
	})
	if err != nil {
		return StageResult{}, err
	}
	outcome, err := s.runtime.Run(ctx, agentruntime.Invocation{
		AgentKey:        agents.Execution,
		ProjectID:       trimOrEmpty(request.ProjectID),
		EpisodeID:       trimOrEmpty(request.EpisodeID),
		WorkflowRunID:   workflowRunID,
		StageRunID:      attempt.ID,
		Skill:           skill,
		SkillVersion:    skillVersion,
		WorkflowState:   s.layer.StateFor(attempt, request),
		Task:            request.Task,
		TaskIsUntrusted: request.TaskIsUntrusted,
		UserMessage:     request.UserMessage,
		LockedRefs:      lockedRefs,
		FixIssueIDs:     fixIssueIDs,
		ModelID:         trimOrEmpty(request.ModelID),
		ProviderID:      trimOrEmpty(request.ProviderID),
	})
	if err != nil {
		// The attempt stays where the runtime left it. A caller retrying finds it in play
		// and the engine refuses a second attempt, which is the right answer: the first has
		// a record, and a re-run is a REVISION rather than a fresh attempt.
		return StageResult{StageRun: attempt, Outcome: outcome}, err
	}
	// THE STAGE PARKS WHERE ITS NEXT STEP IS, and for a stage with no supervisor that is
	// the user's gate rather than a review.
	//
	// This is a real case rather than a defensive branch: AGENT_CONTRACTS section 10.1
	// marks `asset_analysis` `supervision: false`, and its output is a list of facts about
	// the asset library — each line either names an asset that exists or says one does not
	// — so there is nothing for a ruleset to judge that the database does not already
	// answer. A stage parked at `reviewing` with nobody to review it can never reach
	// `waiting_user`, and `ApplyGate` refuses a stage that is not waiting — so the whole
	// pipeline would stop at the first unsupervised stage, which is what the production
	// canary found when it asserted the status.
	//
	// It is decided from the LAYER's own map rather than from the engine's policy table,
	// because the map is what states which agents serve a stage and the two could disagree:
	// a policy that said "supervised" for a stage whose supervisor is unnamed would send
	// this method looking for an agent that does not exist.
	parked := workflow.StageReviewing
	if strings.TrimSpace(agents.Supervision) == "" {
		parked = workflow.StageWaitingUser
	}
	transitioned, err := s.engine.Transition(ctx, agentruntime.StageTransitionRequest{
		StageRunID: attempt.ID,
		Status:     parked,
		Actor:      s.actor(),
	})
	if err != nil {
		return StageResult{StageRun: attempt, Outcome: outcome}, err
	}
	return StageResult{
		StageRun:    transitioned,
		Outcome:     outcome,
		ArtifactIDs: ArtifactIDsOf(outcome),
	}, nil
}

// RunSupervision reviews one attempt and applies the report to the stage.
//
// The supervisor is chosen by the layer's map rather than by a heuristic: a stage whose
// supervisor could not be resolved is refused, because the alternative is a stage that
// runs unsupervised while its policy says it must be reviewed.
//
// The order is the one AGENT_CONTRACTS section 8 gives: run the supervisor, store its
// report, then move the stage. Storing BEFORE moving matters, because the engine's
// `ApplySupervision` decides the next status from the report's verdict — and a stage moved
// on a report that was not stored would be a transition no reader could reconstruct.
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
	agents, err := s.stageAgents(attempt.Stage)
	if err != nil {
		return SupervisionResult{}, err
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
		WorkflowState: s.layer.StateFor(attempt, StageRequest{
			WorkflowRunID: attempt.WorkflowRunID,
			Stage:         attempt.Stage,
			ProjectID:     request.ProjectID,
			EpisodeID:     request.EpisodeID,
			State:         request.State,
		}),
		// The reviewer is TOLD what to read rather than handed the content, which is
		// §6.4's rule that large text travels as a reference: a supervisor that loaded the
		// artifact itself is the property §10.1 asks for, and a prompt carrying the
		// artifact would make that impossible to distinguish from the executor's summary.
		Task:            supervisionTask(attempt, request),
		TaskIsUntrusted: false,
		ModelID:         trimOrEmpty(request.ModelID),
		ProviderID:      trimOrEmpty(request.ProviderID),
	})
	if err != nil {
		return SupervisionResult{StageRun: attempt, Outcome: outcome}, err
	}
	report, err := ReviewFromOutcome(outcome, agents.Supervision)
	if err != nil {
		return SupervisionResult{StageRun: attempt, Outcome: outcome}, err
	}
	// The findings are read from the model's VALIDATED output, which is the shape the
	// review-report contract pins. Reading them here rather than asking the supervisor to
	// call a tool for them is what makes the report and its issues one atomic write:
	// section 7.6 puts the issues INSIDE the report, so a second call could store one
	// without the other.
	issues, err := IssuesFromOutcome(outcome)
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
		Actor:             s.actor(),
	})
	if err != nil {
		return SupervisionResult{StageRun: attempt, Outcome: outcome, Report: stored, Issues: storedIssues}, err
	}
	return SupervisionResult{StageRun: moved, Outcome: outcome, Report: stored, Issues: storedIssues}, nil
}

// ApplyUserGate records the user's decision and moves the stage.
//
// THE DECISION IS WRITTEN BEFORE THE STAGE MOVES, and the order is what makes a FIX
// recoverable: the findings a user names live on the decision row, and the pipeline that
// runs the next attempt reads them from there. A stage moved to `needs_fix` with no
// decision row would be a re-run with nothing to fix.
//
// It does NOT start the next attempt. Section 10.2's FIX is "re-runs against specific
// findings", and starting a revision is a separate command with its own budget check — so
// a caller that wants the re-run asks for it explicitly, and a caller that only wants to
// record the decision (a user closing a window) is not forced into running a model.
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
	if _, err := s.stageAgents(attempt.Stage); err != nil {
		return workflow.StageRun{}, err
	}
	// The findings a FIX names are stated by the caller as JSON, and the pipeline does not
	// read them here: the decision row is where they are durable, and parsing them twice
	// would be two chances to disagree. What is checked is that they PARSE, so a malformed
	// list is refused before the row is written rather than discovered by the re-run.
	if _, err := IssueIDsOf(request.IssueIDsJSON); err != nil {
		return workflow.StageRun{}, err
	}
	// The decision is written with a USER actor. §11.5's "用户决策不可由 Agent 伪造" is
	// enforced by the domain, so a decision whose author is an agent is refused rather than
	// flagged — and this command is the only route that produces one.
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
	// The artifact is approved BEFORE the stage moves, and the order is what makes a
	// failure recoverable: an approved version with a stage still waiting for its gate is a
	// state a user can retry from, while a passed stage whose version was never approved is
	// a workflow that claims success and a project with nothing to build from.
	if IsApprovingDecision(request.Decision) {
		versionID := trimOrEmpty(request.ArtifactVersionID)
		if versionID == "" {
			// Refused rather than skipped, and the refusal says what is missing: a decision
			// that approves nothing is a gate that did not gate, and the caller's next step
			// is to name the version it reviewed.
			return workflow.StageRun{}, agent.InvalidError("An approving decision must name the artifact version it approves.")
		}
		if err := s.layer.Approve(ctx, attempt.Stage, versionID, trimOrEmpty(request.CreatedByID)); err != nil {
			return workflow.StageRun{}, err
		}
	}
	return s.engine.ApplyGate(ctx, agentruntime.ApplyGateRequest{
		StageRunID: attempt.ID,
		Decision:   request.Decision,
		Actor:      userActor(request.CreatedByID),
	})
}

// StartRevision begins the attempt a FIX or REDO asked for.
//
// It is a separate command from the gate because it runs a MODEL: a user who decided to
// fix something has not thereby decided to spend a run, and a UI that submitted a decision
// should not silently start one.
//
// The findings are NOT passed here. They are read from the decision row by `RunStage` when
// the caller names the attempt being revised, which is what makes the FIX survive a
// restart between the two commands — and what makes a FIX that read no findings impossible
// rather than merely unlikely.
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
		Actor:      s.actor(),
	})
}

// ManualEdit writes the user's own version and passes the stage.
//
// AGENT_CONTRACTS section 10.2 makes `manual_edit` end at `passed` rather than at a
// review: "用户的版本即产物". So this command does two things in one call — write the
// version, then move the stage — and the ORDER is what makes the pair recoverable. A stage
// passed with no version would be an approval of nothing; a version written and then a
// failed transition leaves a version the user can retry the gate on.
//
// THE VERSION IS ATTRIBUTED TO THE USER, not to the run that happened to be in play.
// DOMAIN_MODEL §13.4 distinguishes the two, and PRD FR-100's audit is what the distinction
// is for: a reviewer has to be able to tell which versions came from a model. A manual
// edit recorded as an agent's would put the user's own work in the model's column — which
// is the layer's `WriteVersion` to honour, and the reason that method exists.
func (s *Service) ManualEdit(ctx context.Context, request ManualEditRequest) (StageResult, error) {
	if !s.Available() {
		return StageResult{}, unavailable()
	}
	attempt, err := s.workflow.GetStage(ctx, trimOrEmpty(request.StageRunID))
	if err != nil {
		return StageResult{}, err
	}
	// The stage in the request must be the stage the attempt is, because the attempt is what
	// the version is written against: a caller that named the wrong stage would have its
	// content written into another stage's artifact and approved under this one's name.
	if request.Stage != "" && request.Stage != attempt.Stage {
		return StageResult{}, agent.InvalidError("That attempt belongs to a different stage than the one named.")
	}
	agents, err := s.stageAgents(attempt.Stage)
	if err != nil {
		return StageResult{}, err
	}
	if err := s.assertEpisodeInProject(ctx, request.ProjectID, request.EpisodeID); err != nil {
		return StageResult{}, err
	}
	versionID, err := s.layer.WriteVersion(ctx, attempt.Stage, agents, request)
	if err != nil {
		return StageResult{}, err
	}
	moved, err := s.ApplyUserGate(ctx, GateRequest{
		StageRunID: attempt.ID,
		Decision:   workflow.GateManualEdit,
		// The user's own version is what the decision approves, which is §10.2's
		// "用户的版本即产物".
		ArtifactVersionID: versionID,
		Instruction:       request.ChangeReason,
		CreatedByID:       request.CreatedByID,
	})
	if err != nil {
		// The version IS written, and the refusal says so by returning the identifier: a
		// caller whose transition failed has a version to look at and a gate to retry, and
		// hiding the write would leave an orphan row nobody knew about.
		return StageResult{StageRun: attempt, ArtifactIDs: []string{versionID}}, err
	}
	return StageResult{StageRun: moved, ArtifactIDs: []string{versionID}}, nil
}

// revisionContext reads what a revision must know: the pins to respect and the findings to
// address.
//
// The pins come from the base version's lock rows and from the decision's own
// `locked_entity_refs_json`; the findings come from the decision. All three are read HERE,
// from the database, and none is accepted as an argument: section 7.3 says lockedRefs and
// fixIssueIDs come "from the database; the agent cannot add to either", and a value the
// caller supplied could.
//
// A first attempt has neither, and that is not a gap: there is no base version to pin a
// field on and no decision to read findings from.
func (s *Service) revisionContext(ctx context.Context, stage Stage, fixFromStageRunID string) ([]agentruntime.LockedRef, []string, error) {
	base := trimOrEmpty(fixFromStageRunID)
	if base == "" {
		return nil, nil, nil
	}
	decision, ok, err := s.workflow.LatestDecisionForStage(ctx, base)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		// A revision with no decision is a caller error rather than an empty revision: the
		// whole point of naming the attempt is that a user decided something about it.
		return nil, nil, agent.InvalidError("That stage attempt has no user decision to revise against.")
	}
	findings, err := IssueIDsOf(decision.IssueIDsJSON)
	if err != nil {
		return nil, nil, err
	}
	locks, err := s.locksFor(ctx, stage, decision)
	if err != nil {
		return nil, nil, err
	}
	return locks, findings, nil
}

// locksFor renders the pins a revision must respect.
//
// Two sources, because there are two kinds of pin. The decision's
// `locked_entity_refs_json` is what a user pinned while reviewing — section 7.3's
// lockedRefs, which the gate command stores. And the base VERSION's own lock rows are the
// layer's, which is what makes "锁定字段不变" enforceable for a family that has them.
//
// The version is found through the ATTEMPT the decision names, and through the run's
// recorded tool calls rather than through a model's answer: the write is what proves the
// row exists.
func (s *Service) locksFor(ctx context.Context, stage Stage, decision workflow.UserGateDecision) ([]agentruntime.LockedRef, error) {
	refs, err := LockedRefsOf(decision.LockedEntityRefsJSON)
	if err != nil {
		return nil, err
	}
	agents, err := s.stageAgents(stage)
	if err != nil {
		return nil, err
	}
	versionID, err := s.versionOfDecision(ctx, decision, agents)
	if err != nil {
		return nil, err
	}
	if versionID == "" {
		// The attempt produced no version — a failed or superseded attempt. There are no
		// field locks to state, and the decision's own pins still travel.
		return refs, nil
	}
	locks, err := s.layer.Locks(ctx, stage, versionID)
	if err != nil {
		return nil, err
	}
	return append(refs, locks...), nil
}

// versionOfDecision finds the version one attempt produced.
//
// It walks the decision's stage attempt to the RUNS that attempt owns, and then through
// their recorded TOOL CALLS. That is the long way round, and it is the honest one: the
// write is what proves a version exists, and the alternative — a version id stored on the
// decision or parsed out of a model's answer — would be a claim. A run whose call was
// recorded but whose output could not be read yields no id, and the caller then has no
// field locks to state rather than a lock on the wrong row.
func (s *Service) versionOfDecision(ctx context.Context, decision workflow.UserGateDecision, agents StageAgents) (string, error) {
	stageRunID := trimOrEmpty(decision.StageRunID)
	if stageRunID == "" {
		return "", nil
	}
	runs, err := s.runsForStage(ctx, decision.WorkflowRunID, stageRunID)
	if err != nil {
		return "", err
	}
	// The write tool's key is what identifies the artifact, and matching on it rather than
	// on the result's shape keeps this honest: a call whose result could not be parsed
	// still names what it tried to write.
	writeKey := strings.TrimSpace(agents.WriteToolPrefix) + agents.ArtifactType
	if strings.TrimSpace(agents.WriteToolPrefix) == "" {
		// A layer that named no prefix cannot be walked, and guessing one would match the
		// wrong call. Refusing is the fail-closed direction: the caller gets no locks
		// rather than locks on another stage's version.
		return "", nil
	}
	for _, run := range runs {
		calls, err := s.runs.ListToolCalls(ctx, run.ID)
		if err != nil {
			return "", err
		}
		for _, call := range calls {
			if call.ToolKey != writeKey {
				continue
			}
			if id := EntityIDOf(call.OutputJSON); id != "" {
				return id, nil
			}
		}
	}
	return "", nil
}

// runsForStage returns the agent runs one stage attempt owns.
//
// The engine has no read for this and the workflow service's own reads are about stages
// rather than runs, so the walk goes through the inspector's project-scoped list: the runs
// of a project, narrowed to the attempt. The limit is the inspector's ceiling, and it is
// the RIGHT ceiling for this question because a single stage attempt owns a handful of
// runs even when a stage has been revised many times.
func (s *Service) runsForStage(ctx context.Context, projectID, stageRunID string) ([]agent.AgentRun, error) {
	if s.runs == nil {
		return nil, agent.UnavailableError()
	}
	runs, err := s.runs.ListRuns(ctx, projectID, agentruntime.MaxRunLimit)
	if err != nil {
		return nil, err
	}
	owned := make([]agent.AgentRun, 0, 4)
	for _, run := range runs {
		if run.StageRunID == stageRunID {
			owned = append(owned, run)
		}
	}
	return owned, nil
}

// skillFor reads an agent's skill and the version its run must cite.
//
// A missing document is a REFUSAL rather than an empty prompt: a run with no skill would
// be a run this build's pack never described, and the model would answer from the policy
// layer alone — exactly the "undescribed agent" the pack format exists to prevent.
func (s *Service) skillFor(agentKey string) (string, string, error) {
	document, ok := s.assembly.SkillDocument(agentKey)
	if !ok || strings.TrimSpace(document) == "" {
		return "", "", agent.NotFoundError()
	}
	version, ok := s.assembly.SkillVersionOf(agentKey)
	if !ok {
		// §4.2 requires a run to name the skill version it ran. A run whose version could
		// not be resolved would cite nothing, which is worse than not running: the record
		// would say a skill was used and not which one.
		return "", "", agent.NotFoundError()
	}
	return document, version, nil
}

// assertEpisodeInProject refuses an episode that belongs to another project.
//
// It is the same walk every tool handler makes, and it is here because a manual edit is a
// WRITE the user asked for from a UI that could be showing another project's window. A
// missing episode is a not-found rather than a refusal: one is a stale identifier, the
// other is an attempt to reach across a boundary.
func (s *Service) assertEpisodeInProject(ctx context.Context, projectID, episodeID string) error {
	project := trimOrEmpty(projectID)
	if project == "" {
		// No project means the caller's own edit through a route that does not state one,
		// which is the same boundary the script service's reference check documents: the
		// check is not broken, it is not being asked.
		return nil
	}
	if s.episodes == nil {
		// A project was stated and this build cannot check it, so the write is refused
		// rather than let through: the check exists to stop one project's window writing
		// into another's episode, and a build that cannot make it must fail closed.
		return agent.UnavailableError()
	}
	episode := trimOrEmpty(episodeID)
	if episode == "" {
		return agent.InvalidError("A manual edit must name the episode it writes for.")
	}
	owner, err := s.episodes.ProjectOfEpisode(ctx, episode)
	if err != nil {
		return err
	}
	if owner != project {
		return agent.SecurityError("That episode belongs to another project.")
	}
	return nil
}

// actor is the attribution every stage transition this pipeline makes records.
//
// A SYSTEM actor rather than an agent one, and the distinction is what keeps the audit
// readable: a status the stage machine moved is not a status a model chose. The
// identifier names the LAYER, so a reader of the trail can tell which pipeline moved the
// stage.
func (s *Service) actor() agentruntime.Actor {
	name := "stagepipeline"
	if s != nil && s.layer != nil {
		name = s.layer.Name()
	}
	return agentruntime.Actor{Type: "system", ID: name}
}

// stageInput renders what a caller asked for, for the attempt's own record.
//
// It carries identifiers and the task, and NO model output: the run has its own record
// with the transcript, and a second copy here could disagree with it.
func stageInput(request StageRequest) string {
	document := map[string]any{
		"projectId": trimOrEmpty(request.ProjectID),
		"episodeId": trimOrEmpty(request.EpisodeID),
	}
	if task := strings.TrimSpace(request.Task); task != "" {
		document["task"] = task
	}
	if base := trimOrEmpty(request.FixFromStageRunID); base != "" {
		document["fixFromStageRunId"] = base
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		// An input document that will not marshal is this file's bug. An empty object is
		// recorded rather than failing the attempt: the record's job is to say an attempt
		// happened.
		return "{}"
	}
	return string(encoded)
}
