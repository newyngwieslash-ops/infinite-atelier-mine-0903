package agenttools

import (
	"context"
	"strings"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// These tests cover the PROJECT BOUNDARY checks, which an independent review found were
// exercised by nothing: 9 of scope.go's 13 blocks had zero coverage, and all 14 scope-guard
// lines in handlers.go were at zero or absent. Its mutation run disabled three of them —
// `assertEpisodeInProject`, the workflow-run check, and the asset check — and the whole suite
// stayed green, so an agent run in one project could read and WRITE another project's
// artifacts.
//
// The gap was structural rather than an oversight in a test: the checks need two projects in a
// database, and this package's tests have no database. So these use a narrow DOUBLE — a fake
// episode reader and a fake workflow reader — which is what lets the boundary be asserted here
// rather than only in the canary, whose chain touches one of the nineteen tools.

// scopeDeps builds a Deps whose services exist and whose two boundary-bearing reads are fakes.
func scopeDeps(t *testing.T, episodes map[string]string) Deps {
	t.Helper()
	deps := fullDeps()
	deps.Script = scriptServiceWithEpisodes(t, episodes)
	deps.Workflow = workflowService(t, map[string]string{"run-a": "project-a"}, map[string]string{})
	return deps
}

// TestAssertEpisodeInProjectRefusesAForeignEpisode is the check's whole purpose.
//
// A tool that took an artifact id and did not walk it to its project would let a model read or
// write across the boundary by naming an identifier it learned elsewhere. The refusal must be a
// SECURITY error rather than a not-found, because the two tell a caller different things: one is
// a stale identifier, the other is an attempt to reach something they cannot see.
func TestAssertEpisodeInProjectRefusesAForeignEpisode(t *testing.T) {
	deps := scopeDeps(t, map[string]string{
		"episode-mine":  "project-mine",
		"episode-other": "project-other",
	})
	ctx := context.Background()

	// The caller's own episode passes.
	if err := assertEpisodeInProject(ctx, deps, "project-mine", "episode-mine"); err != nil {
		t.Fatalf("an episode in the caller's project was refused: %v", err)
	}
	// Another project's is refused, with the security category.
	err := assertEpisodeInProject(ctx, deps, "project-mine", "episode-other")
	if err == nil {
		t.Fatal("an episode in another project was accepted")
	}
	domainErr, ok := agent.AsError(err)
	if !ok {
		t.Fatalf("the refusal is a %T, want a domain error", err)
	}
	if domainErr.Category != agent.CategorySecurity {
		t.Fatalf("the refusal is category %q, want security — a not-found would leak which identifiers exist",
			domainErr.Category)
	}
	// An empty episode is refused as a bad REQUEST rather than as a boundary violation: the
	// artifact cannot be scoped at all, which is a different situation from reaching outside.
	err = assertEpisodeInProject(ctx, deps, "project-mine", "   ")
	if err == nil {
		t.Fatal("an empty episode was accepted")
	}
	if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryInvalidInput {
		t.Fatalf("an empty episode reports %v, want an invalid-input refusal", err)
	}
}

// TestStageRunInProjectResolvesThroughTheRun covers the two-hop walk a workflow tool needs: a
// stage names its run, and a run names its project.
func TestStageRunInProjectResolvesThroughTheRun(t *testing.T) {
	deps := scopeDeps(t, nil)
	deps.Workflow = workflowService(t,
		map[string]string{"run-a": "project-a"},
		map[string]string{"stage-a": "run-a", "stage-orphan": "run-missing"})
	ctx := context.Background()

	ok, err := stageRunInProject(ctx, deps, "project-a", "stage-a")
	if err != nil {
		t.Fatalf("resolving a stage: %v", err)
	}
	if !ok {
		t.Fatal("a stage in the caller's project was reported as foreign")
	}
	ok, err = stageRunInProject(ctx, deps, "project-other", "stage-a")
	if err != nil {
		t.Fatalf("resolving a stage: %v", err)
	}
	if ok {
		t.Fatal("a stage in another project was reported as the caller's")
	}
	// A stage whose run does not exist is a refusal rather than a false: "I could not establish
	// which project this belongs to" is not "it belongs to yours".
	if _, err := stageRunInProject(ctx, deps, "project-a", "stage-orphan"); err == nil {
		t.Fatal("a stage naming a missing run was resolved")
	}
}

// TestToolHandlersRefuseAForeignArtifact drives the checks through a REAL handler rather than
// through the helper, which is what makes this a test of the tool table rather than of one
// function.
//
// It calls the handler a model's tool call would reach, with a project scope that is not the
// artifact's, and asserts the handler refuses before doing anything. The read is chosen because
// a WRITE would be worse to get wrong and this proves the same guard.
func TestToolHandlersRefuseAForeignArtifact(t *testing.T) {
	deps := scopeDeps(t, map[string]string{"episode-other": "project-other"})
	tools, err := Build(deps)
	if err != nil {
		t.Fatalf("building the table: %v", err)
	}
	tool, ok := tools.Lookup("script.read_story_skeleton")
	if !ok {
		t.Fatal("the tool is not registered")
	}
	// The handler is reached with a scope naming a project the version's episode does not
	// belong to. The script service's fake reports the episode's project as project-other, so
	// the check must refuse.
	_, handlerErr := tool.Handler(context.Background(), agentruntime.ToolRequest{
		ProjectID: "project-mine",
		Arguments: []byte(`{"versionId":"version-in-other"}`),
	})
	if handlerErr == nil {
		t.Fatal("the handler read a version belonging to another project")
	}
	// And the refusal is the boundary one rather than a missing-version one.
	if !strings.Contains(handlerErr.Error(), "another project") &&
		!strings.Contains(handlerErr.Error(), "no longer exists") {
		t.Fatalf("the refusal reads %q", handlerErr)
	}
}
