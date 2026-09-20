package agentruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// These tests cover the engine, which is where section 10.2's gate behaviour and
// AC-AGENT-005's "FIX 超过 2 次转人工" are decided.
//
// The engine is driven over an in-memory transitioner that mirrors the domain's
// state machine and the schema's constraints, so a test that passes here is a test
// about the engine's decisions rather than about a permissive double.

// memoryTransitioner is a workflow run's stages, in memory.
type memoryTransitioner struct {
	runs   map[string]workflow.WorkflowRun
	stages map[string]workflow.StageRun
	// byStage indexes a stage id to its run, so the locator works.
	order []string
	// revisions counts the transitions INTO needs_fix or needs_redo per attempt,
	// which is what the audit trail records and what the FIX budget is keyed on.
	revisions map[string]int
}

func newMemoryTransitioner() *memoryTransitioner {
	return &memoryTransitioner{
		runs:      map[string]workflow.WorkflowRun{},
		stages:    map[string]workflow.StageRun{},
		revisions: map[string]int{},
	}
}

func (m *memoryTransitioner) CreateStage(_ context.Context, request StageCreationRequest) (workflow.StageRun, error) {
	// The (run, stage) uniqueness the schema enforces.
	for _, stage := range m.stages {
		if stage.WorkflowRunID == request.WorkflowRunID && stage.Stage == request.Stage && stage.Attempt == request.Attempt {
			return workflow.StageRun{}, workflow.ConflictError("That stage attempt already exists for this run.")
		}
	}
	stage := workflow.StageRun{
		ID: "stage-" + itoa(len(m.stages)+1), WorkflowRunID: request.WorkflowRunID,
		Stage: request.Stage, Attempt: request.Attempt, ExecutionAgentKey: request.ExecutionKey,
		Status: workflow.StagePending, InputJSON: request.InputJSON, Revision: 1,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	m.stages[stage.ID] = stage
	m.order = append(m.order, stage.ID)
	return stage, nil
}

func (m *memoryTransitioner) TransitionStage(_ context.Context, request StageTransitionRequest) (workflow.StageRun, error) {
	stage, ok := m.stages[request.StageRunID]
	if !ok {
		return workflow.StageRun{}, workflow.NotFoundError()
	}
	if stage.Revision != request.Revision {
		return workflow.StageRun{}, workflow.ConflictError("This stage changed in another window. Reload it and try again.")
	}
	// The domain's machine is the authority, exactly as the real service has it.
	if !workflow.CanStageTransition(stage.Status, request.Status) {
		return workflow.StageRun{}, workflow.ConflictError("That transition is not permitted.")
	}
	// A revision is recorded the way the audit trail records it: the transition INTO
	// needs_fix or needs_redo. Without this the double could not answer the budget
	// question, and a test using it would be testing a permissive fake.
	if request.Status == workflow.StageNeedsFix || request.Status == workflow.StageNeedsRedo {
		m.revisions[stage.ID]++
	}
	stage.Status = request.Status
	stage.Revision++
	if request.Status == workflow.StageRunning && stage.StartedAt.IsZero() {
		stage.StartedAt = time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
	}
	m.stages[stage.ID] = stage
	return stage, nil
}

func (m *memoryTransitioner) ListStages(_ context.Context, workflowRunID string) ([]workflow.StageRun, error) {
	out := []workflow.StageRun{}
	for _, id := range m.order {
		if stage := m.stages[id]; stage.WorkflowRunID == workflowRunID {
			out = append(out, stage)
		}
	}
	return out, nil
}

func (m *memoryTransitioner) GetRun(_ context.Context, runID string) (workflow.WorkflowRun, error) {
	run, ok := m.runs[runID]
	if !ok {
		return workflow.WorkflowRun{}, workflow.NotFoundError()
	}
	return run, nil
}

// RevisionCount is the RevisionCounter the engine needs.
func (m *memoryTransitioner) RevisionCount(_ context.Context, stageRunID string) (int, error) {
	if _, ok := m.stages[stageRunID]; !ok {
		return 0, workflow.NotFoundError()
	}
	return m.revisions[stageRunID], nil
}

// WorkflowRunOfStage is the locator the engine needs.
func (m *memoryTransitioner) WorkflowRunOfStage(_ context.Context, stageRunID string) (string, error) {
	stage, ok := m.stages[stageRunID]
	if !ok {
		return "", workflow.NotFoundError()
	}
	return stage.WorkflowRunID, nil
}

// engineHarness is an engine over the in-memory transitioner.
type engineHarness struct {
	engine       *Engine
	transitioner *memoryTransitioner
}

func newEngineHarness(t *testing.T) *engineHarness {
	t.Helper()
	table := testTools(t)
	transitioner := newMemoryTransitioner()
	transitioner.runs["run-1"] = workflow.WorkflowRun{
		ID: "run-1", ProjectID: "project-1", WorkflowType: "episode_production",
		Status: workflow.RunRunning, Revision: 1,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	runtime := New(Options{
		Registry: mustRegistry(t, table), Tools: table,
		Models: &scriptedModel{replies: []string{`{"summary":"x"}`}},
		Runs:   &memoryRunStore{}, Validate: jsonValidator,
		Clock: fixedRuntimeClock{}, IDs: &runtimeIDs{},
	})
	return &engineHarness{
		engine: NewEngine(EngineOptions{
			Runtime: runtime, Transitioner: transitioner,
			Clock: fixedRuntimeClock{}, IDs: &runtimeIDs{},
		}),
		transitioner: transitioner,
	}
}

// toReviewing walks a stage from running to reviewing through the legal edge, so a
// test that needs a reviewable stage states the walk rather than repeating it.
//
// It goes through `execution_succeeded` first, which is the path section 8's call
// chain describes: the runner records output, the stage reports it succeeded, and
// then a review begins. `running → reviewing` is also legal, but the longer path is
// the one a real run takes.
func toReviewing(t *testing.T, harness *engineHarness, stageID string) workflow.StageRun {
	t.Helper()
	ctx := context.Background()
	current := harness.transitioner.stages[stageID]
	for _, status := range []workflow.StageStatus{workflow.StageExecutionSucceeded, workflow.StageReviewing} {
		next, err := harness.transitioner.TransitionStage(ctx, StageTransitionRequest{
			StageRunID: stageID, Status: status, Revision: current.Revision,
		})
		if err != nil {
			t.Fatalf("walking to %s: %v", status, err)
		}
		current = next
	}
	return current
}

// toWaitingUser walks a stage into the state a gate decision applies to.
func toWaitingUser(t *testing.T, harness *engineHarness, stageID string) workflow.StageRun {
	t.Helper()
	current := toReviewing(t, harness, stageID)
	next, err := harness.transitioner.TransitionStage(context.Background(), StageTransitionRequest{
		StageRunID: stageID, Status: workflow.StageWaitingUser, Revision: current.Revision,
	})
	if err != nil {
		t.Fatalf("walking to waiting_user: %v", err)
	}
	return next
}

// TestStartStageRefusesASecondActiveAttempt covers the invariant migration 000011
// left to the WP-07 writer and migration 000015 turned into an index.
func TestStartStageRefusesASecondActiveAttempt(t *testing.T) {
	harness := newEngineHarness(t)
	ctx := context.Background()
	first, err := harness.engine.StartStage(ctx, StartStageRequest{
		WorkflowRunID: "run-1", Stage: "story_skeleton", ExecutionKey: "script.execution.story_skeleton",
	})
	if err != nil {
		t.Fatalf("StartStage: %v", err)
	}
	if first.Status != workflow.StageRunning {
		t.Fatalf("the created stage is %q, want running", first.Status)
	}
	if first.Attempt != 1 {
		t.Fatalf("the first attempt is numbered %d", first.Attempt)
	}
	// A second attempt while the first is in play is refused, and the refusal names
	// the attempt rather than the rule.
	_, err = harness.engine.StartStage(ctx, StartStageRequest{
		WorkflowRunID: "run-1", Stage: "story_skeleton", ExecutionKey: "script.execution.story_skeleton",
	})
	if err == nil {
		t.Fatal("a second active attempt was started")
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("the refusal is a %T, want a conflict", err)
	}
	if conflict.StageRun != first.ID {
		t.Fatalf("the refusal names %q, want the attempt in play %q", conflict.StageRun, first.ID)
	}
	if !IsRetriable(err) {
		t.Fatal("a state conflict is reported as not retriable, but reloading and retrying is the remedy")
	}
	// Once the first attempt is terminal, a new one is allowed and numbered next.
	if _, err := harness.transitioner.TransitionStage(ctx, StageTransitionRequest{
		StageRunID: first.ID, Status: workflow.StageFailed, Revision: first.Revision,
	}); err != nil {
		t.Fatal(err)
	}
	second, err := harness.engine.StartStage(ctx, StartStageRequest{
		WorkflowRunID: "run-1", Stage: "story_skeleton", ExecutionKey: "script.execution.story_skeleton",
	})
	if err != nil {
		t.Fatalf("a new attempt after a failed one: %v", err)
	}
	if second.Attempt != 2 {
		t.Fatalf("the second attempt is numbered %d", second.Attempt)
	}
	// A DIFFERENT stage is unaffected.
	if _, err := harness.engine.StartStage(ctx, StartStageRequest{
		WorkflowRunID: "run-1", Stage: "adaptation_strategy", ExecutionKey: "script.execution.adaptation_strategy",
	}); err != nil {
		t.Fatalf("another stage: %v", err)
	}
}

// TestStartStageRefusesRestartingAPassedStage covers the one edge `passed` has.
func TestStartStageRefusesRestartingAPassedStage(t *testing.T) {
	harness := newEngineHarness(t)
	ctx := context.Background()
	stage, err := harness.engine.StartStage(ctx, StartStageRequest{
		WorkflowRunID: "run-1", Stage: "story_skeleton",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Walk it to passed the way the engine would: running → waiting_user → passed.
	for _, status := range []workflow.StageStatus{workflow.StageWaitingUser, workflow.StagePassed} {
		current, ok := harness.transitioner.stages[stage.ID]
		if !ok {
			t.Fatal("the stage disappeared")
		}
		if _, err := harness.transitioner.TransitionStage(ctx, StageTransitionRequest{
			StageRunID: stage.ID, Status: status, Revision: current.Revision,
		}); err != nil {
			t.Fatalf("moving to %s: %v", status, err)
		}
	}
	_, err = harness.engine.StartStage(ctx, StartStageRequest{WorkflowRunID: "run-1", Stage: "story_skeleton"})
	if err == nil {
		t.Fatal("a passed stage was restarted")
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("the refusal is a %T, want a conflict", err)
	}
	// The message says what to do instead, because superseding is a different command.
	if conflict.Message == "" {
		t.Fatal("the refusal carries no message")
	}
}

// TestApplySupervisionSendsAFailedReviewToFix is section 10.2's FIX path: a failing
// review while the budget lasts moves the stage to needs_fix, and the revision
// reuses the SAME attempt row rather than creating a second one.
//
// The reuse is the property worth pinning here, because it is what makes the
// budget's counting subtle: a revision does not grow the attempt count, so the
// budget has to be counted from the audit trail rather than from the attempts. The
// exhaustion of the budget is the next test's subject.
func TestApplySupervisionSendsAFailedReviewToFix(t *testing.T) {
	harness := newEngineHarness(t)
	ctx := context.Background()
	stage, err := harness.engine.StartStage(ctx, StartStageRequest{WorkflowRunID: "run-1", Stage: "story_skeleton"})
	if err != nil {
		t.Fatal(err)
	}
	policy := StagePolicyFor("story_skeleton")
	if policy.MaxAutoFix != 2 {
		t.Fatalf("the fixture assumes a budget of 2 and found %d", policy.MaxAutoFix)
	}
	// The first failing review, with no revisions yet, requests a fix.
	current := toReviewing(t, harness, stage.ID)
	afterFix, err := harness.engine.ApplySupervision(ctx, RecordSupervisionRequest{
		StageRunID: stage.ID, Revision: current.Revision, Passed: false,
	})
	if err != nil {
		t.Fatalf("ApplySupervision: %v", err)
	}
	if afterFix.Status != workflow.StageNeedsFix {
		t.Fatalf("a failing review on attempt 1 gave %q, want needs_fix", afterFix.Status)
	}
	// The revision reuses the row, which the domain's IsActive and its only edge out
	// of needs_fix require.
	revised, err := harness.engine.StartRevision(ctx, StageTransitionRequest{
		StageRunID: stage.ID, Revision: afterFix.Revision,
	})
	if err != nil {
		t.Fatalf("StartRevision: %v", err)
	}
	if revised.ID != stage.ID {
		t.Fatalf("the revision created a new attempt %q, want the same row", revised.ID)
	}
	if revised.Status != workflow.StageRunning {
		t.Fatalf("the revision is %q, want running", revised.Status)
	}
	if len(harness.transitioner.stages) != 1 {
		t.Fatalf("the revision left %d stage rows, want one", len(harness.transitioner.stages))
	}
	if got := harness.transitioner.revisions[stage.ID]; got != 1 {
		t.Fatalf("the attempt records %d revisions, want 1", got)
	}
	// A stage that is not waiting for a revision refuses one, so the method cannot be
	// used to restart an arbitrary stage.
	if _, err := harness.engine.StartRevision(ctx, StageTransitionRequest{
		StageRunID: stage.ID, Revision: revised.Revision,
	}); err == nil {
		t.Fatal("a revision was started on a running stage")
	}
}

// TestApplySupervisionOverBudgetWaitsForAPerson covers the route the budget is
// measured on: once a stage has been revised as often as the gate allows, a further
// failing review sends it to a person instead of to another fix.
//
// The count is of REVISIONS rather than of attempts, which is the distinction that
// matters here and that the first version of this engine got wrong: a revision
// reuses the attempt row, so counting attempts could never trip the budget. This
// test drives the loop the way a real run does — revise, review, revise — and
// asserts that the third failing review stops.
func TestApplySupervisionOverBudgetWaitsForAPerson(t *testing.T) {
	harness := newEngineHarness(t)
	ctx := context.Background()
	stage, err := harness.engine.StartStage(ctx, StartStageRequest{WorkflowRunID: "run-1", Stage: "story_skeleton"})
	if err != nil {
		t.Fatal(err)
	}
	// Two revisions, which is the whole budget.
	for revision := 1; revision <= 2; revision++ {
		current := toReviewing(t, harness, stage.ID)
		afterFix, err := harness.engine.ApplySupervision(ctx, RecordSupervisionRequest{
			StageRunID: stage.ID, Revision: current.Revision, Passed: false,
		})
		if err != nil {
			t.Fatalf("revision %d: %v", revision, err)
		}
		if afterFix.Status != workflow.StageNeedsFix {
			t.Fatalf("revision %d gave %q, want needs_fix — the budget is not spent yet", revision, afterFix.Status)
		}
		if _, err := harness.engine.StartRevision(ctx, StageTransitionRequest{
			StageRunID: stage.ID, Revision: afterFix.Revision,
		}); err != nil {
			t.Fatalf("starting revision %d: %v", revision, err)
		}
	}
	if got := harness.transitioner.revisions[stage.ID]; got != 2 {
		t.Fatalf("the attempt records %d revisions, want 2", got)
	}
	// The third failing review finds the budget spent and waits for a person.
	current := toReviewing(t, harness, stage.ID)
	after, err := harness.engine.ApplySupervision(ctx, RecordSupervisionRequest{
		StageRunID: stage.ID, Revision: current.Revision, Passed: false,
	})
	if err != nil {
		t.Fatalf("the third review: %v", err)
	}
	if after.Status != workflow.StageWaitingUser {
		t.Fatalf("a failing review past the budget gave %q, want waiting_user", after.Status)
	}
	// And there is exactly one attempt row throughout, because every revision reused
	// it — which is why the count had to come from somewhere other than the attempts.
	if len(harness.transitioner.stages) != 1 {
		t.Fatalf("the run holds %d stage rows, want one reused attempt", len(harness.transitioner.stages))
	}
}

// TestStartRevisionRefusesWhenTheBudgetIsAlreadySpent covers the guard on the
// revision command itself.
//
// The engine's own path cannot reach this state — a stage arrives in needs_fix only
// because a review requested it while the budget allowed — so this is defence in
// depth for a caller that reaches StartRevision directly. A mutation removing the
// check left the suite green, which is what an untested guard looks like, so the
// state is constructed directly and the guard is asserted.
func TestStartRevisionRefusesWhenTheBudgetIsAlreadySpent(t *testing.T) {
	harness := newEngineHarness(t)
	ctx := context.Background()
	stage, err := harness.engine.StartStage(ctx, StartStageRequest{WorkflowRunID: "run-1", Stage: "story_skeleton"})
	if err != nil {
		t.Fatal(err)
	}
	// Drive the stage to needs_fix with the revisions ledger already past the budget,
	// which is the state a direct caller could otherwise exploit.
	//
	// The transition INTO needs_fix is itself a revision as far as the audit trail is
	// concerned, so the ledger is set one BELOW the target: the double increments it
	// on the way in, and the assertion below is about what the command then reads.
	current := toReviewing(t, harness, stage.ID)
	policy := StagePolicyFor("story_skeleton")
	harness.transitioner.revisions[stage.ID] = policy.MaxAutoFix
	waiting, err := harness.transitioner.TransitionStage(ctx, StageTransitionRequest{
		StageRunID: stage.ID, Status: workflow.StageNeedsFix, Revision: current.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	spent := policy.MaxAutoFix + 1
	if got := harness.transitioner.revisions[stage.ID]; got != spent {
		t.Fatalf("the fixture's ledger reads %d, want %d", got, spent)
	}
	_, err = harness.engine.StartRevision(ctx, StageTransitionRequest{
		StageRunID: stage.ID, Revision: waiting.Revision,
	})
	if err == nil {
		t.Fatal("a revision was started with the budget already spent")
	}
	var quota *QuotaError
	if !errors.As(err, &quota) || quota.Limit != "auto_fix" {
		t.Fatalf("the refusal is %v, want the auto-fix quota", err)
	}
	if quota.Used != spent {
		t.Fatalf("the quota reports %d used, want %d", quota.Used, spent)
	}
}

// TestApplySupervisionPassingRespectsTheGate covers the gate: a passing review
// parks the stage for a person when the gate needs one, and passes it when not.
func TestApplySupervisionPassingRespectsTheGate(t *testing.T) {
	harness := newEngineHarness(t)
	ctx := context.Background()
	stage, err := harness.engine.StartStage(ctx, StartStageRequest{WorkflowRunID: "run-1", Stage: "story_skeleton"})
	if err != nil {
		t.Fatal(err)
	}
	// story_skeleton's gate requires a person, so a passing review waits.
	current := toReviewing(t, harness, stage.ID)
	after, err := harness.engine.ApplySupervision(ctx, RecordSupervisionRequest{
		StageRunID: stage.ID, Revision: current.Revision, Passed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != workflow.StageWaitingUser {
		t.Fatalf("a passing review under a required gate gave %q, want waiting_user", after.Status)
	}
	// The gate table is the PRD's, and every stage it names requires a person.
	for _, name := range workflow.DocumentedStageNames {
		policy := StagePolicyFor(workflow.StageName(name))
		if policy.UserGate != UserGateRequired {
			t.Fatalf("the stage %s does not require a gate", name)
		}
	}
	// A stage nobody configured stops for a person rather than passing silently.
	unknown := StagePolicyFor("a_stage_nobody_configured")
	if unknown.UserGate != UserGateRequired || unknown.Supervision != SupervisionNone {
		t.Fatalf("an unconfigured stage reads as %+v, want a required gate and no supervision", unknown)
	}
}

// TestApplyGateMovesTheStagePerDecision covers section 10.2's decision table.
func TestApplyGateMovesTheStagePerDecision(t *testing.T) {
	cases := []struct {
		decision workflow.GateDecision
		want     workflow.StageStatus
	}{
		{workflow.GateApprove, workflow.StagePassed},
		{workflow.GateManualEdit, workflow.StagePassed},
		{workflow.GateSkip, workflow.StagePassed},
		{workflow.GateWaive, workflow.StagePassed},
		{workflow.GateFix, workflow.StageNeedsFix},
		{workflow.GateRedo, workflow.StageNeedsRedo},
		{workflow.GateCancel, workflow.StageCancelled},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.decision), func(t *testing.T) {
			harness := newEngineHarness(t)
			ctx := context.Background()
			stage, err := harness.engine.StartStage(ctx, StartStageRequest{WorkflowRunID: "run-1", Stage: "story_skeleton"})
			if err != nil {
				t.Fatal(err)
			}
			current := toWaitingUser(t, harness, stage.ID)
			after, err := harness.engine.ApplyGate(ctx, ApplyGateRequest{
				StageRunID: stage.ID, Revision: current.Revision, Decision: testCase.decision,
			})
			if err != nil {
				t.Fatalf("ApplyGate(%s): %v", testCase.decision, err)
			}
			if after.Status != testCase.want {
				t.Fatalf("the decision %s gave %q, want %q", testCase.decision, after.Status, testCase.want)
			}
			// Every resulting status must be one the domain permits from the state the
			// gate applies to, which is waiting_user.
			if !workflow.CanStageTransition(workflow.StageWaitingUser, after.Status) {
				t.Fatalf("the decision %s produced %q, which the domain does not permit from waiting_user", testCase.decision, after.Status)
			}
		})
	}
}

// TestApplyGateRefusesAStageThatIsNotWaiting covers the state check: a decision
// applies to a stage waiting for one, not to a stage already decided or in flight.
func TestApplyGateRefusesAStageThatIsNotWaiting(t *testing.T) {
	harness := newEngineHarness(t)
	ctx := context.Background()
	stage, err := harness.engine.StartStage(ctx, StartStageRequest{WorkflowRunID: "run-1", Stage: "story_skeleton"})
	if err != nil {
		t.Fatal(err)
	}
	// The stage is running, so a decision does not apply.
	_, err = harness.engine.ApplyGate(ctx, ApplyGateRequest{
		StageRunID: stage.ID, Revision: stage.Revision, Decision: workflow.GateApprove,
	})
	if err == nil {
		t.Fatal("a gate decision applied to a running stage")
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("the refusal is a %T, want a conflict", err)
	}
	// An unknown decision is an invalid input rather than a conflict. The stage is
	// walked to the gate first, so the refusal is about the decision and not about
	// the state.
	waiting := toWaitingUser(t, harness, stage.ID)
	_, err = harness.engine.ApplyGate(ctx, ApplyGateRequest{
		StageRunID: waiting.ID, Revision: waiting.Revision, Decision: "maybe",
	})
	if err == nil {
		t.Fatal("an unknown decision was applied")
	}
	var domainErr *agent.Error
	if !errors.As(err, &domainErr) || domainErr.Category != agent.CategoryInvalidInput {
		t.Fatalf("the refusal is %v, want an invalid-input error", err)
	}
}

// TestEngineFailsClosedWhenUnattached covers the composition case.
func TestEngineFailsClosedWhenUnattached(t *testing.T) {
	engine := NewEngine(EngineOptions{})
	if engine.Available() {
		t.Fatal("an engine with no runtime reports itself available")
	}
	if _, err := engine.StartStage(context.Background(), StartStageRequest{WorkflowRunID: "run-1", Stage: "x"}); err == nil {
		t.Fatal("an incomplete engine started a stage")
	}
	if _, err := engine.ApplyGate(context.Background(), ApplyGateRequest{StageRunID: "s", Decision: workflow.GateApprove}); err == nil {
		t.Fatal("an incomplete engine applied a gate")
	}
	if _, err := engine.Load(context.Background(), "run-1"); err == nil {
		t.Fatal("an incomplete engine loaded a run")
	}
}

// TestStageStateActiveUsesTheDomainsOwnPredicate covers the tie between the
// engine's notion of "in play" and the schema's partial index: both are built from
// the seven active statuses, so a test pins them together.
func TestStageStateActiveUsesTheDomainsOwnPredicate(t *testing.T) {
	// Every status the domain calls active must be one migration 000015's index
	// includes, and every status it calls finished must be one the index excludes.
	// The list here is the index's WHERE clause, transcribed.
	indexed := map[workflow.StageStatus]bool{
		workflow.StagePending: true, workflow.StageRunning: true,
		workflow.StageExecutionSucceeded: true, workflow.StageReviewing: true,
		workflow.StageWaitingUser: true, workflow.StageNeedsFix: true, workflow.StageNeedsRedo: true,
	}
	for _, status := range workflow.StageStatuses {
		attempt := workflow.StageRun{Stage: "s", Attempt: 1, Status: status}
		if attempt.IsActive() != indexed[status] {
			t.Fatalf("the domain and the partial index disagree about whether %q is active", status)
		}
	}
	// The state helper reads the same predicate.
	state := StageState{Stage: "s", Attempts: []workflow.StageRun{
		{ID: "b", Attempt: 2, Status: workflow.StagePassed},
		{ID: "a", Attempt: 1, Status: workflow.StageRunning},
	}}
	active, ok := state.Active()
	if !ok || active.ID != "a" {
		t.Fatalf("Active returned %+v, %t", active, ok)
	}
	if latest, ok := state.Latest(); !ok || latest.ID != "b" {
		t.Fatalf("Latest returned %+v, %t", latest, ok)
	}
	if state.AttemptCount() != 2 {
		t.Fatalf("AttemptCount is %d", state.AttemptCount())
	}
}
