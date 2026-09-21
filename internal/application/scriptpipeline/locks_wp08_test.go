package scriptpipeline

import (
	"context"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// locks_wp08_test.go covers the version walk a FIX makes: which stage attempt produced which version,
// and which fields of that version a user pinned.
//
// The quality review measured `locksFor`, `versionOfDecision` and `runsForStage` at 0% coverage, and
// every one of them carries a comment stating a rule. The rules are the ones AC-SCRIPT-002 turns on: a
// revision must respect what the user pinned, and the pin must be found from the ATTEMPT's own write
// rather than from a claim.

// runReaderFake answers the run record's read side.
//
// It embeds the interface so a call this test did not expect panics rather than returning a zero value
// that reads like a stage with no runs — the pattern the repository's other fakes use.
type runReaderFake struct {
	agentruntime.RunReader
	runs  []agent.AgentRun
	calls map[string][]agent.AgentToolCall
}

func (f *runReaderFake) ListRuns(_ context.Context, _ string, _ int) ([]agent.AgentRun, error) {
	return f.runs, nil
}

func (f *runReaderFake) ListToolCalls(_ context.Context, runID string) ([]agent.AgentToolCall, error) {
	return f.calls[runID], nil
}

// TestTheVersionAWalkFindsWhatTheAttemptWrote covers `versionOfDecision` and `runsForStage`.
//
// The version is found from the run's recorded TOOL CALLS and from the key that names the WRITE. Both
// halves are asserted because each is a rule: a call the stage made to READ is not evidence of a version,
// and a call of another family's write is evidence of another artifact.
func TestTheVersionAWalkFindsWhatTheAttemptWrote(t *testing.T) {
	ctx := context.Background()
	agents, _ := AgentsForStage(StageScriptGeneration)
	// The stage attempt owns one run. Its FIRST call is a read whose result happens to carry an
	// `entityId`, and its second is the write. A walk that took any call's result would stop at the first
	// — which is the mutation the review found surviving.
	reads := `{"artifacts":[{"entityType":"scene","entityId":"read-not-written"}]}`
	writes := `{"artifacts":[{"entityType":"script_version","entityId":"version-from-the-write"}]}`
	runs := &runReaderFake{
		runs: []agent.AgentRun{{ID: "run-1", StageRunID: "stage-1"}},
		calls: map[string][]agent.AgentToolCall{
			"run-1": {
				{ToolKey: "script.read_story_skeleton", OutputJSON: reads},
				// The key the walk looks for is `create_<artifact type>`: `script_version` for this
				// stage, which is the ROW the stage's write creates. The structure tool fills that row
				// rather than creating it, so its key is not the one that names a version — and a walk
				// keyed on the wrong one finds nothing. This test found that out by using the wrong key
				// first.
				{ToolKey: "script.create_script_version", OutputJSON: writes},
			},
		},
	}
	service := &Service{runs: runs}
	decision := workflow.UserGateDecision{StageRunID: "stage-1", WorkflowRunID: "run-1"}
	versionID, err := service.versionOfDecision(ctx, decision, agents)
	if err != nil {
		t.Fatalf("walking to the version: %v", err)
	}
	if versionID != "version-from-the-write" {
		t.Fatalf("the walk found %q, which did not come from the stage's write", versionID)
	}

	// A run whose call is a WRITE OF ANOTHER FAMILY yields nothing: the strategy's write is not evidence
	// that this stage produced a script version.
	otherFamily := &runReaderFake{
		runs: []agent.AgentRun{{ID: "run-1", StageRunID: "stage-1"}},
		calls: map[string][]agent.AgentToolCall{
			"run-1": {{ToolKey: "script.create_adaptation_strategy_version", OutputJSON: writes}},
		},
	}
	service = &Service{runs: otherFamily}
	if versionID, err := service.versionOfDecision(ctx, decision, agents); err != nil || versionID != "" {
		t.Fatalf("another family's write yielded %q, %v", versionID, err)
	}

	// A run with no writes yields nothing rather than an error: an attempt whose output failed
	// validation is a real state, and the caller's answer is "there are no locks to state".
	noWrites := &runReaderFake{runs: []agent.AgentRun{{ID: "run-1", StageRunID: "stage-1"}}}
	service = &Service{runs: noWrites}
	if versionID, err := service.versionOfDecision(ctx, decision, agents); err != nil || versionID != "" {
		t.Fatalf("a run with no writes yielded %q, %v", versionID, err)
	}

	// A decision naming no attempt yields nothing: there is no run to walk.
	service = &Service{runs: runs}
	if versionID, err := service.versionOfDecision(ctx, workflow.UserGateDecision{}, agents); err != nil || versionID != "" {
		t.Fatalf("a decision with no attempt yielded %q, %v", versionID, err)
	}

	// And a build with no runs reader REFUSES rather than reporting "no version", because the two send a
	// caller to different places.
	service = &Service{}
	if _, err := service.versionOfDecision(ctx, decision, agents); err == nil {
		t.Fatal("a walk with no run reader reported no version instead of refusing")
	}
}

// TestTheRunsWalkNarrowsToTheAttempt covers the filter `runsForStage` applies.
//
// The read side lists a PROJECT's runs, so this function's whole job is to narrow them to one attempt.
// A walk that returned every run would let another stage's write supply this stage's version.
func TestTheRunsWalkNarrowsToTheAttempt(t *testing.T) {
	ctx := context.Background()
	runs := &runReaderFake{runs: []agent.AgentRun{
		{ID: "run-other", StageRunID: "another-attempt"},
		{ID: "run-1", StageRunID: "stage-1"},
		{ID: "run-2", StageRunID: "stage-1"},
	}}
	service := &Service{runs: runs}
	owned, err := service.runsForStage(ctx, "project-1", "stage-1")
	if err != nil {
		t.Fatalf("walking the runs: %v", err)
	}
	if len(owned) != 2 || owned[0].ID != "run-1" || owned[1].ID != "run-2" {
		t.Fatalf("the attempt's runs are %+v", owned)
	}
	// An attempt with no runs returns an empty slice rather than refusing: a stage that has not run yet
	// is a state, not an error.
	if owned, err := service.runsForStage(ctx, "project-1", "never-ran"); err != nil || len(owned) != 0 {
		t.Fatalf("an attempt with no runs returned %+v, %v", owned, err)
	}
}
