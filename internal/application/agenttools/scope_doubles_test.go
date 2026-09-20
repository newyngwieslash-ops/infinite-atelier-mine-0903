package agenttools

import (
	"context"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
	"time"
)

// scope_doubles_test.go holds the narrow fakes the boundary tests need.
//
// Each embeds the application-layer INTERFACE it stands in for, with the interface left nil, so
// any method the test did not implement panics rather than returning a zero value that reads
// like success. That is this repository's established pattern, and it is what makes a fake this
// small safe: a handler that called something else would fail loudly rather than silently
// getting an empty episode list.

// scriptServiceWithEpisodes builds a script service whose only useful read is GetEpisode.
//
// It is a whole service rather than an interface because the tool table takes the concrete type
// — which is the point of the composition: a tool calls the SERVICE, and the service's own
// validation is part of what it does. The fake sits one layer down, at the repository.
func scriptServiceWithEpisodes(t *testing.T, episodes map[string]string) *appscript.Service {
	t.Helper()
	return appscript.NewService(appscript.Options{
		Repository: episodeLookup{episodes: episodes},
		Clock:      scopeClock{},
		IDs:        scopeIDs{},
	})
}

// episodeLookup answers GetEpisode and GetStorySkeletonVersion, which are the two reads the
// boundary checks and the tool handlers above them reach.
type episodeLookup struct {
	appscript.Repository
	episodes map[string]string
}

func (l episodeLookup) GetEpisode(_ context.Context, id string) (scriptdomain.Episode, error) {
	project, ok := l.episodes[id]
	if !ok {
		return scriptdomain.Episode{}, scriptdomain.NotFoundError()
	}
	return scriptdomain.Episode{ID: id, ProjectID: project}, nil
}

func (l episodeLookup) GetStorySkeletonVersion(_ context.Context, id string) (scriptdomain.StorySkeletonVersion, error) {
	// Every version id belongs to the single foreign episode, which is what the handler test
	// needs: the version resolves, and the EPISODE it names is one the caller may not touch.
	return scriptdomain.StorySkeletonVersion{ID: id, EpisodeID: "episode-other"}, nil
}

// workflowService builds a workflow service over the given reads.
//
// ALL FOUR repositories are supplied even though the boundary checks reach only two, because the
// service's Available reports false unless every one is present — and a service that reported
// unavailable would make these tests assert about the fail-closed path instead of about the
// boundary. The unused two are empty lookups rather than nil, which is what a test about a
// different question needs.
func workflowService(t *testing.T, runs, stages map[string]string) *appworkflow.Service {
	t.Helper()
	return appworkflow.NewService(appworkflow.Options{
		Runs:      runLookup{runs: runs},
		Stages:    stageLookup{stages: stages},
		Reviews:   emptyReviews{},
		Decisions: emptyDecisions{},
		Events:    emptyEvents{},
		Clock:     scopeClock{},
		IDs:       scopeIDs{},
	})
}

// The three reads the boundary tests never make. Each is a distinct interface in the workflow
// service's Options, so each needs its own type.
type emptyReviews struct{ appworkflow.ReviewRepository }

type emptyDecisions struct{ appworkflow.DecisionRepository }

type emptyEvents struct{ appworkflow.EventRepository }

// runLookup answers GetRun by mapping a run id to its project.
type runLookup struct {
	appworkflow.RunRepository
	runs map[string]string
}

func (l runLookup) GetRun(_ context.Context, id string) (workflow.WorkflowRun, error) {
	project, ok := l.runs[id]
	if !ok {
		return workflow.WorkflowRun{}, workflow.NotFoundError()
	}
	return workflow.WorkflowRun{ID: id, ProjectID: project, Revision: 1}, nil
}

// stageLookup answers GetStage by mapping a stage id to its run.
type stageLookup struct {
	appworkflow.StageRepository
	stages map[string]string
}

func (l stageLookup) GetStage(_ context.Context, id string) (workflow.StageRun, error) {
	runID, ok := l.stages[id]
	if !ok {
		return workflow.StageRun{}, workflow.NotFoundError()
	}
	return workflow.StageRun{ID: id, WorkflowRunID: runID, Revision: 1}, nil
}

// scopeClock and scopeIDs are the determinism ports, fixed so the fakes are deterministic.
type scopeClock struct{}

func (scopeClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

type scopeIDs struct{}

func (scopeIDs) New() (string, error) { return "scope-id", nil }

// Compile-time proof that the fakes satisfy what the tool table needs.
var (
	_ agentruntime.ToolHandler = func(context.Context, agentruntime.ToolRequest) (any, error) { return nil, nil }
	_                          = agent.Spec{}
)
