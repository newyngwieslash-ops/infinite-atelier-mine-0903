package stagepipeline

import (
	"context"
	"strings"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// dependency_gate_test.go covers FR-100's 「状态迁移不允许跳过未满足依赖的阶段」.
//
// # What the four tests are for
//
// The criterion has four ways to be wrong, and each one is a test below:
//
//  1. A stage whose prerequisite has NOT passed is admitted. `TestTheGateRefusesAStageWhoseDependencyHasNotPassed`
//     catches that, and it asserts the refusal NAMES the missing stage — a message that said only
//     "dependencies unmet" would leave a caller to guess which.
//  2. A stage whose prerequisite HAS passed is refused anyway. That is the failure mode of a gate
//     written too strictly, and it is the one that would break real work rather than merely let a
//     mistake through. `TestTheGateAdmitsAStageWhoseDependencyHasPassed` catches it.
//  3. A stage with two prerequisites is admitted when only ONE has passed.
//     `TestTheGateRequiresEveryDeclaredDependency` catches it.
//  4. A stage that declares nothing is refused. `TestAStageWithNoDeclaredDependencyIsAdmitted`
//     catches it, and it matters because every layer's first stage declares nothing.
//
// # Why a stub run rather than the real services
//
// The gate's input is a `RunState` and a declaration; the real services supply both but also a
// model, a database and a file store. A stub states the two inputs directly, so a failure here
// names the gate rather than the stack around it. The end-to-end evidence is elsewhere: the
// AC-E2E-002 walk drives the real chain, and it is what caught the first version of the graph being
// too strict.

// gatedLayer declares one dependency so the gate has something to check.
type gatedLayer struct {
	depends []Stage
}

func (gatedLayer) Name() string                    { return "gatedpipeline" }
func (gatedLayer) Stages() []Stage                 { return []Stage{"first_stage", "second_stage"} }
func (g gatedLayer) DependsOn(Stage) []Stage       { return g.depends }
func (gatedLayer) AgentsFor(Stage) (StageAgents, bool) {
	return StageAgents{ArtifactType: "x"}, true
}
func (gatedLayer) StateFor(workflow.StageRun, StageRequest) string      { return "" }
func (gatedLayer) Approve(context.Context, Stage, string, string) error { return nil }
func (gatedLayer) Locks(context.Context, Stage, string) ([]agentruntime.LockedRef, error) {
	return nil, nil
}
func (gatedLayer) WriteVersion(context.Context, Stage, StageAgents, ManualEditRequest) (string, error) {
	return "version-1", nil
}

var _ Layer = gatedLayer{}

// gateStateLoader supplies the run's state to the gate.
//
// It implements the small port `requireDependencies` reads through, so a test states the state
// directly. That is also the honest seam: the gate needs a run's stages and a declaration, and every
// other part of the engine — the runtime, the model, the tool table — is irrelevant to the question
// it asks. A test wired through the whole engine would fail for reasons that have nothing to do with
// the gate, which is what the first version of this file did.
type gateStateLoader struct {
	stages map[workflow.StageName]workflow.StageRun
	err    error
}

func (l gateStateLoader) Load(context.Context, string) (agentruntime.RunState, error) {
	if l.err != nil {
		return agentruntime.RunState{}, l.err
	}
	state := agentruntime.RunState{Stages: map[workflow.StageName]agentruntime.StageState{}}
	for name, stage := range l.stages {
		state.Stages[name] = agentruntime.StageState{Stage: name, Attempts: []workflow.StageRun{stage}}
	}
	return state, nil
}

// serviceWith builds a Service whose gate can be exercised: the loader supplies the run's state and
// the layer supplies the declaration.
func serviceWith(layer Layer, statuses map[workflow.StageName]workflow.StageStatus) *Service {
	stages := map[workflow.StageName]workflow.StageRun{}
	for name, status := range statuses {
		stages[name] = workflow.StageRun{
			ID: "attempt-" + string(name), WorkflowRunID: "run-1", Stage: name,
			Attempt: 1, Status: status, Revision: 1,
		}
	}
	return &Service{state: gateStateLoader{stages: stages}, layer: layer}
}

// TestTheGateRefusesAStageWhoseDependencyHasNotPassed is the criterion, and it is the first case
// above.
func TestTheGateRefusesAStageWhoseDependencyHasNotPassed(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		status workflow.StageStatus
	}{
		{"the dependency never ran", ""},
		{"the dependency is still running", workflow.StageRunning},
		{"the dependency is queued", workflow.StagePending},
		{"the dependency is waiting for a person", workflow.StageWaitingUser},
		{"the dependency failed", workflow.StageFailed},
		{"the dependency was cancelled", workflow.StageCancelled},
		{"the dependency was superseded", workflow.StageSuperseded},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := serviceWith(
				gatedLayer{depends: []Stage{"first_stage"}},
				map[workflow.StageName]workflow.StageStatus{"first_stage": testCase.status},
			)
			err := service.requireDependencies(ctx, "run-1", "second_stage")
			if err == nil {
				t.Fatalf("a stage was admitted while its dependency was %q", testCase.status)
			}
			// The refusal NAMES the stage, because a caller's next step is either to run it or to
			// understand why the pipeline stopped.
			if !strings.Contains(err.Error(), "first_stage") {
				t.Fatalf("the refusal does not name the missing stage: %v", err)
			}
		})
	}
}

// TestTheGateAdmitsAStageWhoseDependencyHasPassed is the second case: a gate written too strictly
// would refuse work that is fine, and that is the failure that breaks a product rather than letting
// a mistake through.
func TestTheGateAdmitsAStageWhoseDependencyHasPassed(t *testing.T) {
	service := serviceWith(
		gatedLayer{depends: []Stage{"first_stage"}},
		map[workflow.StageName]workflow.StageStatus{"first_stage": workflow.StagePassed},
	)
	if err := service.requireDependencies(context.Background(), "run-1", "second_stage"); err != nil {
		t.Fatalf("a stage was refused while its dependency had passed: %v", err)
	}
}

// TestTheGateRequiresEveryDeclaredDependency is the third case: a stage with two prerequisites must
// wait for BOTH.
func TestTheGateRequiresEveryDeclaredDependency(t *testing.T) {
	service := serviceWith(
		gatedLayer{depends: []Stage{"first_stage", "third_stage"}},
		map[workflow.StageName]workflow.StageStatus{"first_stage": workflow.StagePassed},
	)
	err := service.requireDependencies(context.Background(), "run-1", "second_stage")
	if err == nil {
		t.Fatal("a stage was admitted while one of its two dependencies had not passed")
	}
	if !strings.Contains(err.Error(), "third_stage") {
		t.Fatalf("the refusal does not name the dependency that is missing: %v", err)
	}
	// And the one that HAD passed is not named, so the message points at the work left to do.
	if strings.Contains(err.Error(), "first_stage") {
		t.Fatalf("the refusal names a dependency that has passed: %v", err)
	}
}

// TestAStageWithNoDeclaredDependencyIsAdmitted is the fourth case, and it is what keeps every
// layer's first stage runnable.
func TestAStageWithNoDeclaredDependencyIsAdmitted(t *testing.T) {
	service := serviceWith(gatedLayer{}, map[workflow.StageName]workflow.StageStatus{})
	if err := service.requireDependencies(context.Background(), "run-1", "first_stage"); err != nil {
		t.Fatalf("a stage that declares no dependency was refused: %v", err)
	}
}

// TestAPassedAttemptCountsEvenWhenSuperseded is the reading `Passed` implements, stated as its own
// test because it is the subtle half: a stage that passed and was later superseded still produced the
// approved artifact its successor reads, so a gate that insisted on the NEWEST attempt being passed
// would refuse a stage whose input is sitting there approved.
func TestAPassedAttemptCountsEvenWhenSuperseded(t *testing.T) {
	state := agentruntime.RunState{Stages: map[workflow.StageName]agentruntime.StageState{
		"first_stage": {Stage: "first_stage", Attempts: []workflow.StageRun{
			{ID: "attempt-2", Stage: "first_stage", Attempt: 2, Status: workflow.StageSuperseded},
			{ID: "attempt-1", Stage: "first_stage", Attempt: 1, Status: workflow.StagePassed},
		}},
	}}
	if unmet := state.UnmetDependencies([]workflow.StageName{"first_stage"}); len(unmet) != 0 {
		t.Fatalf("a superseded revision of a passed stage was treated as unmet: %v", unmet)
	}
}
