package agentruntime

import (
	"context"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// engine.go decides when a stage advances.
//
// WP-05 built the state machines and said outright that the timing was not its
// question: domain/workflow's doc comment reads "Nothing here decides when a stage
// advances", and application/workflow's says "Whether a stage *should* advance is
// WP-07's decision; this method only refuses an edge the domain's machine does not
// allow." This file is that decision.
//
// What it decides, from AGENT_CONTRACTS sections 9 and 10.2:
//
//   - WHICH STAGES MAY RUN NOW. A stage's dependencies must be satisfied. The
//     engine reads the run's stage rows rather than trusting the model, which is
//     section 9's "Decision 不能提出非法动作。Runtime 必须再次校验".
//   - WHAT A GATE DECISION DOES. APPROVE passes and moves on, FIX re-runs against
//     named issues, REDO re-runs from the same upstream, MANUAL_EDIT takes the
//     user's version. Each maps onto edges the domain already permits.
//   - WHEN AUTOMATIC REVISION STOPS. Section 10.1's maxAutoFix is 2, and section
//     10.2's "FIX 超过 2 次转人工" is what AC-AGENT-005's second half tests.
//   - WHAT THE GATE REQUIRES. Section 10.1's per-stage supervision and userGate
//     settings decide whether a stage needs a review, a person, both or neither.
//
// The engine drives transitions through the workflow service rather than writing
// stage_runs itself, because that service is what records the audit event in the
// same transaction as the change (ADR-0009). A stage transition written around it
// would be a state change with no record.

// StageTransitioner is the workflow service's write surface, as the engine uses it.
//
// It is an interface so the engine can be tested without a database, and it is
// narrow — four methods — because that is the whole of what the engine is allowed
// to do to a run.
type StageTransitioner interface {
	TransitionStage(ctx context.Context, request StageTransitionRequest) (workflow.StageRun, error)
	CreateStage(ctx context.Context, request StageCreationRequest) (workflow.StageRun, error)
	ListStages(ctx context.Context, workflowRunID string) ([]workflow.StageRun, error)
	GetRun(ctx context.Context, runID string) (workflow.WorkflowRun, error)
}

// StageTransitionRequest is one stage status change.
type StageTransitionRequest struct {
	StageRunID string
	Status     workflow.StageStatus
	Revision   int64
	Actor      Actor
}

// StageCreationRequest is one new attempt.
type StageCreationRequest struct {
	WorkflowRunID string
	Stage         workflow.StageName
	Attempt       int
	ExecutionKey  string
	InputJSON     string
	Actor         Actor
}

// Actor is who caused a change.
type Actor struct {
	Type string
	ID   string
}

// GateConfig is one stage's quality-gate settings (AGENT_CONTRACTS section 10.1).
//
// The three-valued Supervision is the section's own: "conditional" means the gate
// needs a review when the stage's output is the kind that can be wrong in a way a
// ruleset sees, which the stage's own configuration decides. It is not "maybe" —
// it is resolved by StagePolicyFor below into a yes or a no before the engine acts,
// because a gate that is undecided is a gate that does nothing.
type GateConfig struct {
	Stage          workflow.StageName
	Supervision    SupervisionSetting
	UserGate       UserGateSetting
	MaxAutoFix     int
	RequiredChecks []string
}

// SupervisionSetting is whether a stage needs a review.
type SupervisionSetting string

const (
	SupervisionRequired    SupervisionSetting = "required"
	SupervisionOptional    SupervisionSetting = "optional"
	SupervisionNone        SupervisionSetting = "none"
	SupervisionConditional SupervisionSetting = "conditional"
)

// UserGateSetting is whether a stage needs a person.
type UserGateSetting string

const (
	UserGateRequired UserGateSetting = "required"
	UserGateOptional UserGateSetting = "optional"
)

// MaxAutoFixCeiling bounds a stage's automatic revision budget.
//
// Section 10.1 gives every stage that has one the value 2, and AC-AGENT-005 says
// "FIX 超过 2 次转人工". The ceiling exists so a configuration cannot ask for more
// than the specification allows: a stage that revised itself twenty times before
// asking a person is not the behaviour the gate is for.
const MaxAutoFixCeiling = 2

// DefaultMaxAutoFix is the budget a stage uses when its configuration does not
// state one, which is section 10.1's figure.
const DefaultMaxAutoFix = 2

// StagePolicyFor returns a stage's gate settings.
//
// The table is PRD FR-100's, whose stage keys ADR-0011 rules are the ones this
// build uses. AGENT_CONTRACTS section 10.1 gives the same gate settings against a
// different key list; the two agree on every stage they share apart from
// supervision on the extraction stage, and ADR-0011 records that the PRD's
// "conditional" is the one taken because the extraction stage's own ruleset is
// what decides.
//
// A stage not in the table gets no supervision and a required gate, which is the
// conservative pair: nothing is auto-approved, and nothing is auto-reviewed either,
// so a stage someone forgot to configure stops for a person rather than passing
// silently.
func StagePolicyFor(stage workflow.StageName) GateConfig {
	for _, candidate := range defaultStagePolicies {
		if candidate.Stage == stage {
			return candidate
		}
	}
	return GateConfig{Stage: stage, Supervision: SupervisionNone, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix}
}

// defaultStagePolicies is PRD FR-100's default quality gate, in its order.
//
// The supervision values are the PRD's; the userGate values are the PRD's too,
// which does not restate AGENT_CONTRACTS section 10.1's list but does say at
// section 12.2 that a stage's candidate needs the user's decision. Every stage
// therefore requires a gate, which is the stricter of the two documents and the
// one the acceptance criteria are written against.
var defaultStagePolicies = []GateConfig{
	{Stage: "chapter_event_extraction", Supervision: SupervisionConditional, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
	{Stage: "story_skeleton", Supervision: SupervisionRequired, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
	{Stage: "adaptation_strategy", Supervision: SupervisionRequired, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
	{Stage: "script_generation", Supervision: SupervisionRequired, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
	{Stage: "asset_gap_analysis", Supervision: SupervisionNone, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
	{Stage: "asset_generation", Supervision: SupervisionConditional, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
	{Stage: "storyboard_table", Supervision: SupervisionRequired, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
	{Stage: "storyboard_panel_generation", Supervision: SupervisionConditional, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
	{Stage: "video_generation", Supervision: SupervisionConditional, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
	{Stage: "final_episode", Supervision: SupervisionRequired, UserGate: UserGateRequired, MaxAutoFix: DefaultMaxAutoFix},
}

// DocumentsStage returns whether a stage's output is the kind a ruleset can review.
//
// It resolves the "conditional" setting. Section 10.1 marks the extraction stage
// and the generation stages conditional because their output's acceptability
// depends on things a ruleset cannot see — an extraction is judged against the
// source text, and a generated image against a style guide a person reads. This
// build resolves the condition the only way it can without inventing a ruleset:
// a conditional stage IS supervised, because the review is read-only and cheap and
// the alternative is an unreviewed artifact. ADR-0011 records the ruling.
func DocumentsStage(setting SupervisionSetting) bool {
	return setting == SupervisionRequired || setting == SupervisionConditional
}

// Engine drives stages.
type Engine struct {
	runtime      *Runtime
	transitioner StageTransitioner
	clock        Clock
	ids          IDGenerator
}

// EngineOptions configures an Engine.
type EngineOptions struct {
	Runtime      *Runtime
	Transitioner StageTransitioner
	Clock        Clock
	IDs          IDGenerator
}

// NewEngine builds an Engine.
func NewEngine(options EngineOptions) *Engine {
	return &Engine{
		runtime:      options.Runtime,
		transitioner: options.Transitioner,
		clock:        options.Clock,
		ids:          options.IDs,
	}
}

// Available reports whether the engine can drive anything.
func (e *Engine) Available() bool {
	return e != nil && e.runtime != nil && e.transitioner != nil
}

// StageState is what the engine knows about one stage of a run.
type StageState struct {
	Stage workflow.StageName
	// Attempts is every attempt, newest first.
	Attempts []workflow.StageRun
}

// Active returns the attempt in play, if there is one.
//
// It uses the domain's own IsActive rather than restating the active set, so the
// engine and the schema's partial unique index cannot disagree about which
// attempts are in play — the index is built from the same seven statuses.
func (s StageState) Active() (workflow.StageRun, bool) {
	for _, attempt := range s.Attempts {
		if attempt.IsActive() {
			return attempt, true
		}
	}
	return workflow.StageRun{}, false
}

// Latest returns the newest attempt, active or not.
func (s StageState) Latest() (workflow.StageRun, bool) {
	if len(s.Attempts) == 0 {
		return workflow.StageRun{}, false
	}
	return s.Attempts[0], true
}

// AttemptCount is how many attempts the stage has had.
func (s StageState) AttemptCount() int { return len(s.Attempts) }

// RunState is a run and its stages.
type RunState struct {
	Run    workflow.WorkflowRun
	Stages map[workflow.StageName]StageState
}

// Load reads a run's state, which is the database fact every decision is checked
// against.
func (e *Engine) Load(ctx context.Context, workflowRunID string) (RunState, error) {
	if !e.Available() {
		return RunState{}, agent.UnavailableError()
	}
	run, err := e.transitioner.GetRun(ctx, workflowRunID)
	if err != nil {
		return RunState{}, err
	}
	stages, err := e.transitioner.ListStages(ctx, workflowRunID)
	if err != nil {
		return RunState{}, err
	}
	state := RunState{Run: run, Stages: map[workflow.StageName]StageState{}}
	for _, stage := range stages {
		current := state.Stages[stage.Stage]
		current.Stage = stage.Stage
		current.Attempts = append(current.Attempts, stage)
		state.Stages[stage.Stage] = current
	}
	// Newest attempt first, so Active and Latest are the first match.
	for name, stage := range state.Stages {
		attempts := stage.Attempts
		sortStagesByAttemptDescending(attempts)
		state.Stages[name] = StageState{Stage: name, Attempts: attempts}
	}
	return state, nil
}

// StartStageRequest asks the engine to begin a stage.
type StartStageRequest struct {
	WorkflowRunID string
	Stage         workflow.StageName
	ExecutionKey  string
	InputJSON     string
	Actor         Actor
}

// StartStage creates an attempt and moves it to running.
//
// Two rules decide whether it may:
//
//   - At most one attempt is active for a stage. The database enforces this with
//     the partial unique index migration 000015 added, and the engine checks it
//     first so the caller gets a domain conflict rather than a constraint failure.
//     Migration 000011's comment said this invariant was "enforced by the WP-07
//     writer"; that writer is here, and since 000015 it is also enforced by the
//     schema, which is the stronger of the two.
//   - A stage already passed may not be restarted. Its edge is `passed →
//     superseded` and nothing else, so a second attempt would need the first
//     superseded explicitly rather than silently.
func (e *Engine) StartStage(ctx context.Context, request StartStageRequest) (workflow.StageRun, error) {
	if !e.Available() {
		return workflow.StageRun{}, agent.UnavailableError()
	}
	if err := workflow.ValidateStageName(request.Stage); err != nil {
		return workflow.StageRun{}, agent.InvalidError("The stage name is not usable.")
	}
	state, err := e.Load(ctx, request.WorkflowRunID)
	if err != nil {
		return workflow.StageRun{}, err
	}
	stage := state.Stages[request.Stage]
	if active, ok := stage.Active(); ok {
		// The refusal names the attempt rather than the rule, because the caller's next
		// step is to look at that attempt.
		return workflow.StageRun{}, &ConflictError{
			Message:  "That stage already has an attempt in play.",
			StageRun: active.ID,
		}
	}
	if latest, ok := stage.Latest(); ok && latest.Status == workflow.StagePassed {
		return workflow.StageRun{}, &ConflictError{
			Message:  "That stage has passed, so it must be superseded before another attempt runs.",
			StageRun: latest.ID,
		}
	}
	attempt := stage.AttemptCount() + 1
	created, err := e.transitioner.CreateStage(ctx, StageCreationRequest{
		WorkflowRunID: request.WorkflowRunID,
		Stage:         request.Stage,
		Attempt:       attempt,
		ExecutionKey:  request.ExecutionKey,
		InputJSON:     request.InputJSON,
		Actor:         request.Actor,
	})
	if err != nil {
		return workflow.StageRun{}, err
	}
	// CreateStage writes 'pending'; the engine moves it to 'running' because that is
	// the edge the domain permits and because a stage that was created but never
	// started would look like a stage waiting for something.
	running, err := e.transitioner.TransitionStage(ctx, StageTransitionRequest{
		StageRunID: created.ID,
		Status:     workflow.StageRunning,
		Revision:   created.Revision,
		Actor:      request.Actor,
	})
	if err != nil {
		return workflow.StageRun{}, err
	}
	return running, nil
}

// ConflictError reports a state the caller must resolve before continuing.
//
// It is its own type because the engine's conflicts carry the row that caused them,
// and a caller resolving one needs to name it. It reports itself as retriable,
// because section 14.2 does not list a state conflict among the failures not to
// retry: reloading and trying again is exactly the remedy.
type ConflictError struct {
	Message  string
	StageRun string
}

func (e *ConflictError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ConflictError) Category() agent.ErrorCategory { return agent.CategoryConflict }

// Retriable is true: the caller resolves the conflict by reloading, and the same
// command then succeeds.
func (e *ConflictError) Retriable() bool { return true }

// RecordSupervisionRequest records a review's outcome and moves the stage
// accordingly.
type RecordSupervisionRequest struct {
	StageRunID        string
	Revision          int64
	Passed            bool
	RecommendedAction string
	// Severity is the report's own severity, which section 7.6 gives a rule of its own:
	// "critical 强制人工门". It is separate from Passed because the two answer different
	// questions — whether the report passed, and how bad what it found is — and a critical
	// finding can accompany either.
	//
	// An empty value means the caller did not state one, and the ordinary policy applies. That is
	// not the same as "none": a caller who read a report with no severity and passed it through
	// unchanged gets the policy's answer, which is right, because the absence of a statement is
	// not a statement of absence.
	Severity workflow.Severity
	Actor    Actor
}

// ApplySupervision moves a stage after its review.
//
// A passing review moves the stage to `waiting_user` when the gate needs a person
// and to `passed` when it does not. A failing review moves it to `needs_fix` when
// the budget allows another automatic attempt and to `waiting_user` when it does
// not — which is section 10.2's "FIX 超过 2 次转人工" and AC-AGENT-005's second half.
//
// The stage must already be REVIEWING or WAITING_USER, because those are the only
// two statuses the domain's machine lets reach `needs_fix`, `passed` or back to
// `waiting_user`. A stage that is still `running` has no review to apply: the
// runner records its output and the caller moves it to `reviewing` first, which is
// the order section 8's call chain describes ("ExecutionResult validation → reload
// actual workspace → ReviewReport validation").
func (e *Engine) ApplySupervision(ctx context.Context, request RecordSupervisionRequest) (workflow.StageRun, error) {
	if !e.Available() {
		return workflow.StageRun{}, agent.UnavailableError()
	}
	stage, state, err := e.stageFor(ctx, request.StageRunID)
	if err != nil {
		return workflow.StageRun{}, err
	}
	if stage.Status != workflow.StageReviewing && stage.Status != workflow.StageWaitingUser {
		return workflow.StageRun{}, &ConflictError{
			Message:  "That stage is not under review.",
			StageRun: stage.ID,
		}
	}
	policy := StagePolicyFor(stage.Stage)
	status := workflow.StageWaitingUser
	switch {
	case request.Severity == workflow.SeverityCritical:
		// Section 7.6's "critical 强制人工门", which the domain names as this package's to
		// enforce. A critical finding stops for a person whatever the stage's policy says and
		// whatever the report's verdict was: the policy's userGate setting is about the ordinary
		// case, and a critical finding is not the ordinary case.
		//
		// It is checked BEFORE the passed/failed branch because it overrides both: a critical
		// finding on a stage that would otherwise pass still needs a person, and so does one on a
		// stage that would otherwise revise automatically.
		status = workflow.StageWaitingUser
	case request.Passed:
		if policy.UserGate == UserGateRequired {
			// The gate is a person's; the stage waits rather than passing.
			status = workflow.StageWaitingUser
		} else {
			status = workflow.StagePassed
		}
	default:
		// A failing review can be revised automatically only while the budget lasts.
		// The count is of REVISIONS rather than of attempts, because a revision
		// reuses the attempt row: see RevisionCounter.
		revisions, err := e.revisionCount(ctx, stage.ID)
		if err != nil {
			return workflow.StageRun{}, err
		}
		status = reviseOrWait(revisions, policy.MaxAutoFix)
	}
	_ = state
	// The edge is checked against the domain rather than assumed, so a change to the
	// machine cannot leave this method producing a transition SQLite refuses.
	if !workflow.CanStageTransition(stage.Status, status) {
		return workflow.StageRun{}, &ConflictError{
			Message:  "That review outcome does not apply to the stage's current state.",
			StageRun: stage.ID,
		}
	}
	return e.transitioner.TransitionStage(ctx, StageTransitionRequest{
		StageRunID: stage.ID, Status: status, Revision: request.Revision, Actor: request.Actor,
	})
}

// ApplyGateRequest applies a user's decision to a stage.
type ApplyGateRequest struct {
	StageRunID string
	Revision   int64
	Decision   workflow.GateDecision
	// The findings a FIX decision names are NOT here, and their absence is deliberate.
	//
	// Section 12.2's gate stores issue_ids_json and instruction on the DECISION row, which the
	// workflow service writes when the user's decision is submitted. A copy on this request would be
	// a second source of truth for the same fact, and it would be the one nothing read: the engine
	// moves the stage, and the pipeline that builds the next attempt reads the decision back from
	// the database — which is what makes "FIX re-runs against specific findings" survive a restart,
	// where a value threaded through two calls would not.
	Actor Actor
}

// ApplyGate moves a stage after a user decision (AGENT_CONTRACTS section 10.2).
//
// Each decision maps onto edges the domain already permits, and the mapping is
// stated rather than derived so it can be read against the specification:
//
//	approve      waiting_user → passed
//	fix          waiting_user → needs_fix   (re-run against the named issues)
//	redo         waiting_user → needs_redo  (re-run from the same upstream)
//	manual_edit  waiting_user → passed      (the user's version IS the artifact)
//	cancel       waiting_user → cancelled
//	skip         waiting_user → passed      (the domain has no skipped status;
//	                                         section 12.2's skip ends at passed,
//	                                         which the domain records as a ruling)
//	waive        waiting_user → passed      (a waiver accepts the artifact despite
//	                                         its stale mark, so the stage passes)
func (e *Engine) ApplyGate(ctx context.Context, request ApplyGateRequest) (workflow.StageRun, error) {
	if !e.Available() {
		return workflow.StageRun{}, agent.UnavailableError()
	}
	if !workflow.IsValidGateDecision(request.Decision) {
		return workflow.StageRun{}, agent.InvalidError("That gate decision is not recognised.")
	}
	stage, _, err := e.stageFor(ctx, request.StageRunID)
	if err != nil {
		return workflow.StageRun{}, err
	}
	if stage.Status != workflow.StageWaitingUser {
		// A gate decision applies to a stage that is waiting for one. Applying it to a
		// stage in any other state would be a second decision about something already
		// decided.
		return workflow.StageRun{}, &ConflictError{
			Message:  "That stage is not waiting for a decision.",
			StageRun: stage.ID,
		}
	}
	var status workflow.StageStatus
	switch request.Decision {
	case workflow.GateApprove, workflow.GateManualEdit, workflow.GateSkip, workflow.GateWaive:
		status = workflow.StagePassed
	case workflow.GateFix:
		status = workflow.StageNeedsFix
	case workflow.GateRedo:
		status = workflow.StageNeedsRedo
	case workflow.GateCancel:
		status = workflow.StageCancelled
	default:
		return workflow.StageRun{}, agent.InvalidError("That gate decision does not apply to a stage.")
	}
	// The edge must be one the domain permits. It is checked rather than assumed so a
	// future decision added to the vocabulary cannot reach SQLite untested.
	if !workflow.CanStageTransition(stage.Status, status) {
		return workflow.StageRun{}, &ConflictError{
			Message:  "That decision does not apply to the stage's current state.",
			StageRun: stage.ID,
		}
	}
	return e.transitioner.TransitionStage(ctx, StageTransitionRequest{
		StageRunID: stage.ID, Status: status, Revision: request.Revision, Actor: request.Actor,
	})
}

// StartRevision begins the attempt a FIX or REDO decision asked for.
//
// It is the second half of section 10.2's FIX/REDO: a decision moves the stage to
// `needs_fix` or `needs_redo`, and this re-runs it. The two are separate commands
// because the decision is a user's and the re-run is the engine's.
//
// The attempt to re-run is the SAME ROW, not a new one. That follows from the
// domain WP-05 already shipped: IsActive counts `needs_fix` and `needs_redo` as
// active, and the only edge out of them is `→ running`. So a revision reuses the
// attempt, and a new attempt is created only once the previous one is terminal.
// ADR-0011 records this, because the alternative reading — "创建新 Attempt" in
// section 10.2 — would need both IsActive and migration 000015's index changed.
func (e *Engine) StartRevision(ctx context.Context, request StageTransitionRequest) (workflow.StageRun, error) {
	if !e.Available() {
		return workflow.StageRun{}, agent.UnavailableError()
	}
	stage, state, err := e.stageFor(ctx, request.StageRunID)
	if err != nil {
		return workflow.StageRun{}, err
	}
	if stage.Status != workflow.StageNeedsFix && stage.Status != workflow.StageNeedsRedo {
		return workflow.StageRun{}, &ConflictError{
			Message:  "That stage is not waiting for a revision.",
			StageRun: stage.ID,
		}
	}
	// The budget is checked here as well as at supervision, because a caller could
	// otherwise start a revision the review already decided against. Over budget, the
	// revision is refused rather than run: section 10.2 sends it to a person, and a
	// person is not a thing this command can produce.
	policy := StagePolicyFor(stage.Stage)
	revisions, err := e.revisionCount(ctx, stage.ID)
	if err != nil {
		return workflow.StageRun{}, err
	}
	// The budget was already checked when the review decided this stage needed a fix,
	// so this command honours that decision rather than re-litigating it. What it
	// refuses is a revision the review did NOT request — a caller reaching this
	// method directly on a stage whose budget is spent.
	//
	// The two checks therefore differ in what they are for, and the difference is
	// deliberate: ApplySupervision decides whether a fix may be REQUESTED, which is true
	// while FEWER than the budget have been; StartRevision decides whether the request may
	// be honoured, which is true UP TO AND INCLUDING the last one.
	//
	// That difference shows up in the comparison: `revisions > budget` here, where
	// ApplySupervision uses `revisions < budget`. A revision is counted by the transition
	// INTO needs_fix, so at the moment StartRevision runs the count already INCLUDES the
	// revision being honoured — which is why the refusal is strictly greater than the
	// budget rather than greater-or-equal. An earlier version of this comment said "hence
	// the `+ 1`", describing an expression the code does not contain: a reader who followed
	// it would write `revisions+1 > MaxAutoFix`, which permits one EXTRA automatic revision
	// and breaks AC-AGENT-005's "FIX 超过 2 次转人工". The two predicates are asserted to be
	// complementary by test, and the comments now agree with them.
	if revisions > policy.MaxAutoFix {
		return workflow.StageRun{}, &QuotaError{Limit: "auto_fix", Allowed: policy.MaxAutoFix, Used: revisions}
	}
	_ = state
	return e.transitioner.TransitionStage(ctx, StageTransitionRequest{
		StageRunID: stage.ID, Status: workflow.StageRunning, Revision: request.Revision, Actor: request.Actor,
	})
}

// mayRevise reports whether a stage may begin another automatic revision.
//
// It is one predicate for both call sites rather than a comparison written twice,
// because the two must agree about the same budget: the first version had
// ApplySupervision using `<` and StartRevision using `>=`, which are not
// complements, so a review would request a fix that the revision then refused. The
// budget is spent once MaxAutoFix fixes have been REQUESTED, which is what a
// transition into needs_fix records.
//
//	revisions = 0  no fix requested yet        → a fix is allowed
//	revisions = N  N fixes requested           → allowed while N < MaxAutoFix
//	revisions = MaxAutoFix                      → a person decides next
func mayRevise(revisions, budget int) bool {
	return revisions < budget
}

// reviseOrWait returns the status a failing review produces.
//
// It exists so the rule reads in one place: while a fix may still be requested the
// stage goes to needs_fix, and once it may not the stage waits for a person.
func reviseOrWait(revisions, budget int) workflow.StageStatus {
	if mayRevise(revisions, budget) {
		return workflow.StageNeedsFix
	}
	return workflow.StageWaitingUser
}

// revisionCount reports how many automatic revisions an attempt has had.
//
// A missing counter is a refusal rather than a zero: an engine that cannot count
// revisions would run an unbounded loop, and reporting "no revisions yet" for a
// capability that is absent is exactly the fail-open mistake this package's other
// optional ports avoid by saying so.
func (e *Engine) revisionCount(ctx context.Context, stageRunID string) (int, error) {
	counter, ok := e.transitioner.(RevisionCounter)
	if !ok {
		return 0, agent.UnavailableError()
	}
	return counter.RevisionCount(ctx, stageRunID)
}

// Transition moves one attempt to a new status.
//
// It is the STAGE MACHINE's single entry point for a move that is not a review, a gate or a revision —
// the "the run finished, now review it" step, which is the one a driving pipeline makes. The domain's
// machine is what decides whether the edge is legal: this method forwards to the transitioner, which
// validates against `workflow`'s table, so a caller cannot skip a status by using it.
//
// It exists so the pipeline that drives a stage does not have to reach through the engine to the
// transitioner and reimplement the revision bookkeeping. That is not a convenience: the revision
// number a compare-and-swap needs belongs to the ROW, and a caller that read the row and then wrote
// it would be racing every other writer of the same stage.
func (e *Engine) Transition(ctx context.Context, request StageTransitionRequest) (workflow.StageRun, error) {
	if !e.Available() {
		return workflow.StageRun{}, agent.UnavailableError()
	}
	if strings.TrimSpace(request.StageRunID) == "" {
		return workflow.StageRun{}, agent.InvalidError("A stage run is required.")
	}
	if !workflow.IsValidStageStatus(request.Status) {
		return workflow.StageRun{}, agent.InvalidError("That stage status is not recognised.")
	}
	// The revision is READ rather than taken from the caller, so a stale one produces a conflict at
	// the transitioner instead of a silent overwrite — the same reason every other write in this
	// repository carries an expected revision.
	stage, _, err := e.stageFor(ctx, request.StageRunID)
	if err != nil {
		return workflow.StageRun{}, err
	}
	request.Revision = stage.Revision
	return e.transitioner.TransitionStage(ctx, request)
}

// stageFor loads one stage's row and its siblings' state.
//
// The stage's own row does not carry enough to decide anything — a decision needs
// the attempt COUNT and the statuses of the other attempts — so the run is found
// first and its whole stage set loaded. One query path rather than two, because two
// would be two chances to read a different state than the one acted on.
func (e *Engine) stageFor(ctx context.Context, stageRunID string) (workflow.StageRun, StageState, error) {
	trimmed := strings.TrimSpace(stageRunID)
	if trimmed == "" {
		return workflow.StageRun{}, StageState{}, agent.InvalidError("A stage run is required.")
	}
	locator, ok := e.transitioner.(StageRunLocator)
	if !ok {
		// An adapter that cannot say which run a stage belongs to cannot drive the
		// engine at all, so the refusal names the missing capability rather than
		// pretending the stage does not exist.
		return workflow.StageRun{}, StageState{}, agent.UnavailableError()
	}
	runID, err := locator.WorkflowRunOfStage(ctx, trimmed)
	if err != nil {
		return workflow.StageRun{}, StageState{}, err
	}
	state, err := e.Load(ctx, runID)
	if err != nil {
		return workflow.StageRun{}, StageState{}, err
	}
	for _, stage := range state.Stages {
		for _, attempt := range stage.Attempts {
			if attempt.ID == trimmed {
				return attempt, stage, nil
			}
		}
	}
	// The run exists but holds no such stage, which is a not-found rather than a
	// storage failure: the caller's stage id is stale.
	return workflow.StageRun{}, StageState{}, agent.NotFoundError()
}

// RevisionCounter reports how many automatic revisions a stage attempt has had.
//
// It exists because the attempt COUNT cannot answer that question. A FIX or REDO
// reuses the attempt row — the domain's IsActive counts needs_fix and needs_redo as
// active, and the only edge out of them is back to running — so the number of
// attempts stays where it was and a budget keyed on it could never trip. The first
// version of this engine did exactly that, and a test caught it: three revisions
// into a budget of two, a fourth was still allowed.
//
// The count comes from the audit trail rather than a new column: every revision is
// a `needs_fix`/`needs_redo` transition, and the workflow_events table already
// records each one with its stage run. That is a durable fact this engine can read
// without a schema change, and it is the same fact section 12.2's user-visible
// history shows.
type RevisionCounter interface {
	// RevisionCount returns how many times a stage attempt has begun a revision,
	// counting the transitions INTO needs_fix and needs_redo.
	RevisionCount(ctx context.Context, stageRunID string) (int, error)
}

// StageRunLocator is the optional lookup the engine needs to find a stage's run.
//
// The transitioner's four methods are the engine's writes and reads of a run's
// stages; finding which run a stage belongs to is a fifth capability, and it is
// separate so a minimal adapter can omit it and the engine can say so rather than
// panicking on a missing method.
type StageRunLocator interface {
	WorkflowRunOfStage(ctx context.Context, stageRunID string) (string, error)
}

// sortStagesByAttemptDescending orders attempts newest first, by attempt number
// then by id so two rows with one number cannot swap between reads.
func sortStagesByAttemptDescending(attempts []workflow.StageRun) {
	for i := 1; i < len(attempts); i++ {
		for j := i; j > 0; j-- {
			left, right := attempts[j-1], attempts[j]
			if left.Attempt > right.Attempt || (left.Attempt == right.Attempt && left.ID >= right.ID) {
				break
			}
			attempts[j-1], attempts[j] = attempts[j], attempts[j-1]
		}
	}
}
