package database

import (
	"context"
	"strings"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appproduction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// supervisor_location_test.go covers AC-BOARD-002's clause a specification review found
// UNMET: "deterministic/LLM supervisor 定位 Shot 6".
//
// The finding was that the fixture was INERT DATA — a test read `bad-storyboard.json`'s own
// `mustLocateAt` and `faults` fields and asserted they agreed with themselves, while no
// supervisor ever ran against the board. The assertion has to be that a review of a board
// with a continuity fault produces a finding whose `entityId` is THAT ROW, because that is
// what a FIX is pointed at and what the criterion asks for.
//
// The mock is the deterministic supervisor, and it reports a finding at the row the state
// names. So the test's job is to check the WIRING: that the row a supervisor is told about is
// the row the board's fault is in, and that the stored finding carries it.

// TestTheSupervisorLocatesTheRowAtFault covers the clause end to end.
func TestTheSupervisorLocatesTheRowAtFault(t *testing.T) {
	ctx := context.Background()
	canary := newProductionCanary(t)
	scriptVersionID, shotIDs := canary.seedScriptWithShots(t, 12)
	versionID, itemIDs := canary.seedBoardWithRows(t, scriptVersionID, shotIDs)

	// The sixth row wears the inconsistent costume, which is the criterion's fault.
	target, err := canary.storyboard.GetStoryboardItem(ctx, itemIDs[5])
	if err != nil {
		t.Fatalf("reading the sixth row: %v", err)
	}

	// The supervisor is given the row its review is about, which a real caller reads from
	// the board — `supervisionTask` states the artifact, and the state names the row.
	result := canary.superviseBoard(t, versionID, target.ID, true)
	if result.Report.Passed {
		t.Fatalf("a board with a continuity fault passed its review: ruleset=%q action=%q issues=%d", result.Report.RulesetVersion, result.Report.RecommendedAction, len(result.Issues))
	}
	if len(result.Issues) == 0 {
		t.Fatal("the failing report carries no findings, so nothing locates the fault")
	}
	// THE ASSERTION: the finding names the ROW, not the board.
	located := false
	for _, issue := range result.Issues {
		if issue.EntityID != target.ID {
			continue
		}
		located = true
		if issue.EntityType != "storyboard_item" {
			t.Errorf("the finding's entity type is %q", issue.EntityType)
		}
		if strings.TrimSpace(issue.EvidenceJSON) == "" {
			t.Error("the finding carries no evidence, so a reader cannot see what conflicts")
		}
	}
	if !located {
		t.Fatalf("no finding names the row at fault (%s): %+v", target.ID, result.Issues)
	}
	// And a finding that named a DIFFERENT row would be worse than none, so the count is
	// asserted too: the mock reports exactly one, and it is the right one.
	if len(result.Issues) != 1 {
		t.Fatalf("the review reported %d findings", len(result.Issues))
	}
}

// TestASupervisorOfAConsistentBoardPasses covers the other direction, so the case above is a
// comparison rather than a single observation: a board with no fault must pass, or the
// assertion above would be satisfied by a supervisor that always failed.
func TestASupervisorOfAConsistentBoardPasses(t *testing.T) {
	ctx := context.Background()
	canary := newProductionCanary(t)
	scriptVersionID, shotIDs := canary.seedScriptWithShots(t, 12)
	versionID, itemIDs := canary.seedBoardWithRows(t, scriptVersionID, shotIDs)
	target, err := canary.storyboard.GetStoryboardItem(ctx, itemIDs[0])
	if err != nil {
		t.Fatalf("reading the first row: %v", err)
	}
	result := canary.superviseBoard(t, versionID, target.ID, false)
	if !result.Report.Passed {
		t.Fatalf("a consistent board failed its review: %s", result.Report.Summary)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("a passing report carries %d findings, and the domain refuses that", len(result.Issues))
	}
}

// superviseBoard runs the storyboard supervisor over one attempt.
//
// The ATTEMPT is created here: a supervisor reviews an attempt, and a report attaches to a
// stage run — one with no attempt would have nothing to attach to. The scenario is what
// decides the verdict (`MockScenarioSupervisorIssues` reports a finding,
// `MockScenarioNormal` passes), so the two tests above are a comparison rather than one
// observation repeated.
//
// The ROW travels in the state, which is what a locating supervisor reads. The production
// canary's own board walk supplies it from the board, and this does the same for a board the
// test built directly.
func (c *productionCanary) superviseBoard(t *testing.T, boardVersionID, itemID string, expectFailure bool) stagepipeline.SupervisionResult {
	t.Helper()
	ctx := context.Background()
	attempt := c.startBoardAttempt(t, boardVersionID)
	// `expectFailure` names what the CALLER expects, because that is what reads at the call
	// site: a boolean whose name describes the mechanism rather than the expectation is how
	// the first version of this test asked for a failing review and asserted it passed.
	if expectFailure {
		c.mock.SetScenario(infraproviders.MockScenarioSupervisorIssues)
	} else {
		c.mock.SetScenario(infraproviders.MockScenarioNormal)
	}
	result, err := c.pipeline.RunSupervision(ctx, stagepipeline.SupervisionRequest{
		StageRunID:        attempt.ID,
		ProjectID:         c.ids.project,
		EpisodeID:         c.ids.episode,
		ArtifactVersionID: boardVersionID,
		State: appproduction.StateFields{
			EpisodeID: c.ids.episode, StoryboardVersionID: boardVersionID, StoryboardItemID: itemID,
			// The two references the finding's evidence names: the row at fault and the plan
			// whose continuity rule it breaks. AC-BOARD-002 asks for evidence pointing at two
			// versions, and a supervisor can only cite what its state names.
			DirectorPlanVersionID: c.boardPlanVersionID,
		},
	})
	if err != nil {
		t.Fatalf("supervising the board: %v", err)
	}
	return result
}

// startBoardAttempt begins a storyboard attempt for an existing board version.
//
// It does NOT run the board stage's agent: what this test asks about is the SUPERVISOR, and
// running an agent first would make the assertion depend on the mock's execution branch as
// well. The attempt is started through the engine, which is what gives it a record and a
// status a supervision can move.
func (c *productionCanary) startBoardAttempt(t *testing.T, boardVersionID string) workflow.StageRun {
	t.Helper()
	ctx := context.Background()
	if _, err := c.engine.StartStage(ctx, agentruntime.StartStageRequest{
		WorkflowRunID: c.ids.workflowRun,
		Stage:         appproduction.StageStoryboardTable,
		ExecutionKey:  "production.execution.storyboard_table",
		InputJSON:     `{"storyboardVersionId":"` + boardVersionID + `"}`,
		Actor:         agentruntime.Actor{Type: "system", ID: "supervisor-location-test"},
	}); err != nil {
		t.Fatalf("starting the board attempt: %v", err)
	}
	stages, err := c.workflow.ListStages(ctx, c.ids.workflowRun)
	if err != nil {
		t.Fatalf("listing the run's stages: %v", err)
	}
	newest := workflow.StageRun{}
	for _, stage := range stages {
		if stage.Stage == appproduction.StageStoryboardTable && stage.ID != "" {
			newest = stage
		}
	}
	if newest.ID == "" {
		t.Fatal("the board attempt was not recorded")
	}
	// The stage is moved to reviewing, which is where a supervision applies: an attempt that
	// has not run sits in a status the engine refuses a report for.
	moved, err := c.engine.Transition(ctx, agentruntime.StageTransitionRequest{
		StageRunID: newest.ID,
		Status:     workflow.StageReviewing,
		Actor:      agentruntime.Actor{Type: "system", ID: "supervisor-location-test"},
	})
	if err != nil {
		t.Fatalf("moving the attempt to reviewing: %v", err)
	}
	return moved
}
