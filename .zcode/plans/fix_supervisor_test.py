import io

p = 'internal/infrastructure/database/supervisor_location_test.go'
s = io.open(p, encoding='utf-8').read()

old = s[s.index('// superviseBoard runs the storyboard supervisor over one attempt.'):]
new = '''// superviseBoard runs the storyboard supervisor over one attempt.
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
func (c *productionCanary) superviseBoard(t *testing.T, boardVersionID, itemID string, fail bool) stagepipeline.SupervisionResult {
	t.Helper()
	ctx := context.Background()
	attempt := c.startBoardAttempt(t, boardVersionID)
	if fail {
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
'''
s = s.replace(old, new)

header = '''package database

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

'''
s = header + s.lstrip('\n')
io.open(p, 'w', encoding='utf-8').write(s)
print("test rewritten with the real helpers")
