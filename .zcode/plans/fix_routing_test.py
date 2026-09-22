import io

p = 'internal/desktop/routing_wp09_test.go'
s = io.open(p, encoding='utf-8').read()

old = s[s.index('// routingBinding builds a binding with both pipelines attached'):s.index('// TestTheBindingRoutesAStageToTheLayerThatDrivesIt is the assertion')]
new = '''// routingBinding builds a binding with both pipelines attached and the drama fixture's
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
	store.stages["attempt-1"] = workflow.StageRun{
		ID: "attempt-1", WorkflowRunID: "run-1",
		Stage: workflow.StageName(attemptStage), Status: workflow.StageWaitingUser,
		Attempt: 1, Revision: 1,
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

'''
s = s.replace(old, new, 1)

# The imports the file actually needs.
header = '''package desktop

import (
	"context"
	"strings"
	"testing"

	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	appproductionpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appscriptpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/scriptpipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

'''
start = s.index('package desktop')
end = s.index('// routing_wp09_test.go covers')
s = header + s[end:]
io.open(p, 'w', encoding='utf-8').write(s)
print("rewritten")
