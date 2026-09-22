package desktop

import (
	"context"
	"testing"

	appproductionpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appscriptpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/scriptpipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// routing_wp09_test.go covers WHICH LAYER A USER'S CLICK REACHES.
//
// A mutation run over WP-09 found this untested in four ways at once. `AttachProductionPipeline`
// appears in NO test in this repository, so every assertion about the production layer drove
// `productionpipeline.Service` directly — and the binding, which is the only code that decides
// which layer a stage or an attempt is dispatched to, was dead to the suite. Four mutations
// survived: routing every stage to the script pipeline, building the SCRIPT state for a
// production stage, routing every attempt to the script pipeline, and swapping the two slots.
//
// The second of those is the one that matters most, because it is a defect the code's own
// comment records as having ALREADY SHIPPED once: a production stage whose state is the script
// layer's carries no `shot_ids`, and the board tool then refuses every row it writes for citing
// a shot that "does not belong to this script version".

// routingPipeline is a StagePipeline double that records what it was asked for.
//
// It embeds the interface so a method this test did not expect panics rather than returning a
// zero value — the pattern the repository's other doubles use. The `ManualEdit` signature is
// the mechanism's, which is what makes one type serve both layers.
type routingPipeline struct {
	StagePipeline
	name string
	// runStages records every stage `RunStage` was asked to run.
	runStages []string
	// states records the state each call carried, so the two layers' structs can be told apart.
	states []any
	// attempts records every stage-run id the attempt-scoped commands named.
	attempts []string
}

func (p *routingPipeline) RunStage(_ context.Context, request stagepipeline.StageRequest) (stagepipeline.StageResult, error) {
	p.runStages = append(p.runStages, string(request.Stage))
	p.states = append(p.states, request.State)
	// The returned attempt carries an id THE STORE KNOWS, because the binding reads the
	// attempt's review report after the pipeline returns: a double that answered with an id of
	// its own would make every call fail one step later, for a reason that has nothing to do
	// with routing. That is what the first version of this double did.
	return stagepipeline.StageResult{StageRun: workflow.StageRun{ID: routingAttemptID}}, nil
}

func (p *routingPipeline) RunSupervision(_ context.Context, request stagepipeline.SupervisionRequest) (stagepipeline.SupervisionResult, error) {
	p.attempts = append(p.attempts, request.StageRunID)
	p.states = append(p.states, request.State)
	return stagepipeline.SupervisionResult{StageRun: workflow.StageRun{ID: request.StageRunID}}, nil
}

func (p *routingPipeline) ApplyUserGate(_ context.Context, request stagepipeline.GateRequest) (workflow.StageRun, error) {
	p.attempts = append(p.attempts, request.StageRunID)
	return workflow.StageRun{ID: request.StageRunID, Status: workflow.StagePassed}, nil
}

func (p *routingPipeline) StartRevision(_ context.Context, stageRunID string) (workflow.StageRun, error) {
	p.attempts = append(p.attempts, stageRunID)
	return workflow.StageRun{ID: stageRunID, Status: workflow.StageRunning}, nil
}

func (p *routingPipeline) ManualEdit(_ context.Context, request stagepipeline.ManualEditRequest) (stagepipeline.StageResult, error) {
	p.attempts = append(p.attempts, request.StageRunID)
	return stagepipeline.StageResult{StageRun: workflow.StageRun{ID: request.StageRunID}}, nil
}

// routingAttemptID is the attempt every routing double answers about, and the id the store
// holds it under.
const routingAttemptID = "attempt-1"

// routingBinding builds a binding with both pipelines attached and the drama fixture's
// workflow service, whose stage store answers with an attempt at the named stage.
//
// The store is the EXISTING `dramaStore`, which already satisfies all five workflow
// repository ports: a second double would be a second answer to "what does a stage run look
// like", and the one that exists is the one the other desktop tests assert against.
func routingBinding(t *testing.T, attemptStage string) (*DramaBinding, *routingPipeline, *routingPipeline) {
	t.Helper()
	script := &routingPipeline{name: "script"}
	production := &routingPipeline{name: "production"}
	store := newDramaStore()
	store.stages[routingAttemptID] = workflow.StageRun{
		ID: routingAttemptID, WorkflowRunID: "run-1",
		Stage: workflow.StageName(attemptStage), Status: workflow.StageWaitingUser,
		Attempt: 1, Revision: 1,
	}
	// The attempt NEEDS A REVIEW REPORT, because `RunSupervision` reads it back after the
	// pipeline returns: the command's job is to run the review and hand the caller the stored
	// report, and a store with no report would fail one step after the routing this test is
	// about. That is what the first version of this fixture did.
	store.reports["report-1"] = workflow.ReviewReport{
		ID: "report-1", StageRunID: routingAttemptID, SupervisorKey: "test.supervisor",
		RulesetVersion: "test.v1", Severity: workflow.SeverityNone,
		CreatedAt: fixedDramaClock{}.Now(),
	}
	binding := &DramaBinding{}
	ctx := context.Background()
	AttachPipeline(binding, ctx, script)
	AttachProductionPipeline(binding, ctx, production)
	AttachWorkflow(binding, ctx, newRoutingWorkflow(t, store))
	return binding, script, production
}

// newRoutingWorkflow builds the workflow service over a store.
func newRoutingWorkflow(t *testing.T, store *dramaStore) *appworkflow.Service {
	t.Helper()
	return appworkflow.NewService(appworkflow.Options{
		Runs: store, Stages: store, Reviews: store, Decisions: store, Events: store,
		Clock: fixedDramaClock{}, IDs: fixedIDs(),
	})
}

// TestTheBindingRoutesAStageToTheLayerThatDrivesIt is the assertion the mutation run asked for.
//
// It asserts BOTH directions for every stage: the right slot received the call AND the other
// received none. One without the other would pass against a binding that called both.
func TestTheBindingRoutesAStageToTheLayerThatDrivesIt(t *testing.T) {
	scriptStages := []string{"story_skeleton", "adaptation_strategy", "script_generation"}
	productionStages := []string{
		string(appproductionpipeline.StageDirectorPlan),
		string(appproductionpipeline.StageAssetGapAnalysis),
		string(appproductionpipeline.StageAssetGeneration),
		string(appproductionpipeline.StageStoryboardTable),
		string(appproductionpipeline.StageStoryboardPanel),
	}
	for _, stage := range scriptStages {
		binding, script, production := routingBinding(t, stage)
		if _, err := binding.RunScriptStage(RunScriptStageRequest{Stage: stage, WorkflowRunID: "run-1"}); err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		if len(script.runStages) != 1 || script.runStages[0] != stage {
			t.Errorf("%s reached the script slot as %v", stage, script.runStages)
		}
		if len(production.runStages) != 0 {
			t.Errorf("%s also reached the production slot", stage)
		}
	}
	for _, stage := range productionStages {
		binding, script, production := routingBinding(t, stage)
		if _, err := binding.RunScriptStage(RunScriptStageRequest{
			Stage: stage, WorkflowRunID: "run-1", EpisodeID: "episode-1",
		}); err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		if len(production.runStages) != 1 || production.runStages[0] != stage {
			t.Errorf("%s reached the production slot as %v", stage, production.runStages)
		}
		if len(script.runStages) != 0 {
			// THE DEFECT THIS CATCHES: a production stage run by the script layer is refused
			// by that layer's stage map, so the caller sees a refusal naming the wrong
			// pipeline — and if the layers' stage sets ever overlapped, it would run with the
			// wrong agents and report success.
			t.Errorf("%s also reached the script slot, which does not drive it", stage)
		}
	}
}

// TestTheBindingBuildsTheStateOfTheLayerThatDrivesTheStage covers the regression the code's own
// comment records as having shipped once.
//
// A production stage whose state is the script layer's carries no `shot_ids`, and the board
// tool then refuses every row for citing a shot that "does not belong to this script version" —
// a refusal that names a citation problem and not the missing field.
func TestTheBindingBuildsTheStateOfTheLayerThatDrivesTheStage(t *testing.T) {
	binding, _, production := routingBinding(t, "storyboard_table")
	if _, err := binding.RunScriptStage(RunScriptStageRequest{
		Stage:               "storyboard_table",
		WorkflowRunID:       "run-1",
		EpisodeID:           "episode-1",
		ScriptVersionID:     "script-1",
		StoryboardVersionID: "board-1",
		ShotIDs:             []string{"shot-1", "shot-2"},
	}); err != nil {
		t.Fatalf("running the board stage: %v", err)
	}
	if len(production.states) != 1 {
		t.Fatalf("the production slot recorded %d states", len(production.states))
	}
	fields, ok := production.states[0].(appproductionpipeline.StateFields)
	if !ok {
		// The script layer's struct is what a stage with no `shot_ids` would have carried.
		t.Fatalf("a production stage was given %T, which is not the production layer's state", production.states[0])
	}
	if len(fields.ShotIDs) != 2 || fields.ShotIDs[0] != "shot-1" {
		t.Fatalf("the state's shot ids are %v, and the board's rows cite them", fields.ShotIDs)
	}
	if fields.StoryboardVersionID != "board-1" || fields.ScriptVersionID != "script-1" {
		t.Fatalf("the state names board %q and script %q", fields.StoryboardVersionID, fields.ScriptVersionID)
	}

	// And the other direction: a SCRIPT stage gets the script layer's state, not the
	// production layer's.
	scriptBinding, script, _ := routingBinding(t, "script_generation")
	if _, err := scriptBinding.RunScriptStage(RunScriptStageRequest{
		Stage: "script_generation", WorkflowRunID: "run-1",
		SkeletonVersionID: "skeleton-1", SelectedEventIDs: []string{"event-1"},
	}); err != nil {
		t.Fatalf("running the script stage: %v", err)
	}
	if len(script.states) != 1 {
		t.Fatalf("the script slot recorded %d states", len(script.states))
	}
	scriptFields, ok := script.states[0].(appscriptpipeline.StateFields)
	if !ok {
		t.Fatalf("a script stage was given %T", script.states[0])
	}
	if scriptFields.SkeletonVersionID != "skeleton-1" || len(scriptFields.SelectedEventIDs) != 1 {
		t.Fatalf("the script state is %+v", scriptFields)
	}
}

// TestTheAttemptScopedCommandsFindTheirLayer covers the other half of the routing.
//
// `RunSupervision`, `ApplyUserGate` and `StartRevision` name a stage RUN rather than a stage, so
// the layer is discovered by reading the attempt. A binding that always chose the script slot
// would send a production attempt's decision to a pipeline that does not drive its stage.
func TestTheAttemptScopedCommandsFindTheirLayer(t *testing.T) {
	for _, stage := range []string{"story_skeleton", "storyboard_table", "director_plan"} {
		binding, script, production := routingBinding(t, stage)
		_, err := binding.RunScriptSupervision(RunScriptSupervisionRequest{
			StageRunID: routingAttemptID, ProjectID: "project-1",
		})
		if err != nil {
			t.Fatalf("%s: supervising: %v", stage, err)
		}
		_, err = binding.ApplyScriptGate(ApplyScriptGateRequest{
			StageRunID: routingAttemptID, Decision: string(workflow.GateApprove),
		})
		if err != nil {
			t.Fatalf("%s: applying a gate: %v", stage, err)
		}
		if _, err := binding.StartScriptRevision(routingAttemptID); err != nil {
			t.Fatalf("%s: starting a revision: %v", stage, err)
		}
		expected, other := script, production
		if appproductionpipeline.IsProductionStage(stage) {
			expected, other = production, script
		}
		if len(expected.attempts) != 3 {
			t.Errorf("%s: the right layer received %d of three commands", stage, len(expected.attempts))
		}
		if len(other.attempts) != 0 {
			t.Errorf("%s: the other layer received %v", stage, other.attempts)
		}
	}
}

// TestAnUnknownStageReachesTheScriptSlot covers the fallback.
//
// A stage neither layer drives has to reach SOME pipeline, and the script slot is the one whose
// stage map REFUSES it — the refusal names the stage, so the caller's next step is to correct
// the name rather than to wonder which build they have. What this asserts is the ROUTING: which
// slot received the call. The refusal itself is the layer's, and `layer_wp08_test.go` covers it.
//
// The first version of this test asserted a refusal from the BINDING, and the double accepts any
// stage — so it failed for a reason that had nothing to do with routing.
func TestAnUnknownStageReachesTheScriptSlot(t *testing.T) {
	binding, script, production := routingBinding(t, "a_stage_neither_layer_drives")
	if _, err := binding.RunScriptStage(RunScriptStageRequest{
		Stage: "a_stage_neither_layer_drives", WorkflowRunID: "run-1",
	}); err != nil {
		t.Fatalf("running an unknown stage: %v", err)
	}
	if len(script.runStages) != 1 {
		t.Fatalf("an unknown stage reached the script slot %v", script.runStages)
	}
	if len(production.runStages) != 0 {
		t.Errorf("an unknown stage reached the production slot")
	}
}
