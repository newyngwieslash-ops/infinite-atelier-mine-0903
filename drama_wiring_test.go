package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	"time"
)

// drama_wiring_test.go covers what the composition root SUPPLIES, which is the one thing no unit test in
// the packages below can check.
//
// The gap it exists for: WP-08's `ProjectScriptVersion` takes an optional projector and REFUSES when it has
// none, and `composeDrama` supplied nothing — so every projection from a real build failed, while the
// canary's own assertion passed because it used a double. A double that satisfies a port cannot notice
// that production does not compose one, and a unit test of the service cannot notice either: from inside
// the service, "no projector" and "a build with no canvas" are the same state.
//
// So these tests drive the WIRING: they compose the stack the way `app.go` does and assert that the
// dependencies a user's commands need are actually there.

// composedDrama builds the drama stack over a fresh migrated database, the way the application does.
func composedDrama(t *testing.T) (*dramaWiring, *appprojects.Service, *database.Handle) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	handle, err := database.Open(ctx, filepath.Join(dir, "studio.db"), filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	if err := handle.Err(); err != nil {
		t.Fatalf("the database is unusable: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })
	store, err := filestore.New(filepath.Join(dir, "files"), filepath.Join(dir, "temp"))
	if err != nil {
		t.Fatalf("opening the file store: %v", err)
	}
	// The canvas writer, as `composeProjects` builds it. Only the parts the projection needs are
	// supplied, because this test is about which service is PLUMBED, not about what a project can do.
	canvasWriter := appprojects.NewService(appprojects.Options{
		Projects: database.NewProjectRepository(handle.SQL()),
		Canvas:   database.NewCanvasRepository(handle.SQL()),
		Settings: database.NewDramaSettingsRepository(handle.SQL()),
		Clock:    appprojectsClock{},
		IDs:      &testIDs{},
	})
	stack := composeDrama(handle, store, canvasWriter)
	if stack == nil {
		t.Fatal("composeDrama returned nil over a writable database")
	}
	return stack, canvasWriter, handle
}

// testIDs mints deterministic identifiers.
type testIDs struct{ next int }

func (g *testIDs) New() (string, error) {
	g.next++
	return "wiring-id-" + string(rune('a'+g.next%26)), nil
}

// TestComposeDramaSuppliesTheCanvasProjector is the assertion that would have caught the gap.
//
// The service exposes no way to ask "do you have a projector", so the assertion is on the BEHAVIOUR a
// caller sees: a projection of a version that exists either writes nodes or refuses. A build with no
// projector refuses with "this build cannot project onto a canvas", which is exactly what every real
// build was doing.
func TestComposeDramaSuppliesTheCanvasProjector(t *testing.T) {
	ctx := context.Background()
	stack, canvasWriter, handle := composedDrama(t)
	// A project and an episode to hang a version off, written through the real repositories so the
	// projection's own validation has rows to check.
	projectID := seedWiringProject(t, canvasWriter)
	episode, err := stack.script.CreateEpisode(ctx, appscript.CreateEpisodeRequest{
		ProjectID: projectID, SeasonNumber: 1, EpisodeNumber: 1, Title: "Wiring",
	})
	if err != nil {
		t.Fatalf("creating an episode: %v", err)
	}
	scriptRecord, err := stack.script.EnsureScript(ctx, episode.ID)
	if err != nil {
		t.Fatalf("ensuring the script: %v", err)
	}
	skeleton, err := stack.script.CreateStorySkeletonVersion(ctx, appscript.CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", EndingHook: "ending",
	})
	if err != nil {
		t.Fatalf("creating a skeleton: %v", err)
	}
	strategy, err := stack.script.CreateAdaptationStrategyVersion(ctx, appscript.CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "summary",
	})
	if err != nil {
		t.Fatalf("creating a strategy: %v", err)
	}
	version, err := stack.script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
		ScriptID: scriptRecord.ID, StorySkeletonVersionID: skeleton.ID,
		AdaptationStrategyVersionID: strategy.ID,
		CreatedByType:               versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("creating a script version: %v", err)
	}
	if _, err := stack.script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
		ScriptID: scriptRecord.ID, ScriptVersionID: version.ID,
		Structure: wiringStructure(version.ID),
	}); err != nil {
		t.Fatalf("writing the structure: %v", err)
	}

	// THE ASSERTION. A refusal here means the composition root forgot the projector, which is what it
	// did until this test existed.
	result, err := stack.script.ProjectScriptVersion(ctx, appscript.ProjectScriptVersionRequest{
		ProjectID: projectID, ScriptVersionID: version.ID,
	})
	if err != nil {
		t.Fatalf("a composed build refused to project, so the projector is not wired: %v", err)
	}
	if len(result.SceneNodeIDs) != 2 {
		t.Fatalf("the projection wrote %d nodes for a two-scene version", len(result.SceneNodeIDs))
	}
	// The nodes are REAL ROWS, read back through the repository rather than through the service that
	// wrote them: the projection's whole point is that a user sees the scenes on their board, and a
	// service's return value is not the board.
	documents, err := database.NewCanvasRepository(handle.SQL()).ListDocuments(ctx, projectID)
	if err != nil {
		t.Fatalf("reading the canvas documents: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("the project has %d canvas documents", len(documents))
	}
	nodes, err := database.NewCanvasRepository(handle.SQL()).ListNodes(ctx, documents[0].ID)
	if err != nil {
		t.Fatalf("reading the canvas nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("the canvas holds %d nodes after projecting two scenes", len(nodes))
	}
	// Each node references the SCENE it was projected from, so a click on the board can find the thing
	// it draws.
	for _, node := range nodes {
		if node.EntityType != "scene" {
			t.Fatalf("a node draws entity type %q", node.EntityType)
		}
	}
	// The projection is idempotent over the same version: a second call MOVES the existing nodes rather
	// than piling up duplicates, which DOMAIN_MODEL §10.2 requires and which the projects service
	// implements. Asserting it here is what keeps a re-run of a workflow from doubling a board.
	if _, err := stack.script.ProjectScriptVersion(ctx, appscript.ProjectScriptVersionRequest{
		ProjectID: projectID, ScriptVersionID: version.ID,
	}); err != nil {
		t.Fatalf("re-projecting: %v", err)
	}
	after, err := database.NewCanvasRepository(handle.SQL()).ListNodes(ctx, documents[0].ID)
	if err != nil {
		t.Fatalf("reading the canvas nodes after re-projecting: %v", err)
	}
	if len(after) != len(nodes) {
		t.Fatalf("re-projecting changed the node count from %d to %d", len(nodes), len(after))
	}
}

// TestComposeDramaRefusesProjectionWithoutACanvas covers the other branch, so the test above is a
// comparison rather than a single observation.
//
// A build whose project stack failed to compose has no canvas writer, and the script service must then
// REFUSE rather than report a silent success — a caller asking for a projection and getting success would
// have no way to tell it did not happen.
func TestComposeDramaRefusesProjectionWithoutACanvas(t *testing.T) {
	ctx := context.Background()
	stack, canvasWriter, handle := composedDrama(t)
	if stack == nil {
		t.Fatal("composeDrama returned nil")
	}
	projectID := seedWiringProject(t, canvasWriter)
	// A stack composed with NO canvas writer, which is what a failed project stack produces.
	dir := t.TempDir()
	store, err := filestore.New(filepath.Join(dir, "files"), filepath.Join(dir, "temp"))
	if err != nil {
		t.Fatalf("opening the file store: %v", err)
	}
	withoutCanvas := composeDrama(handle, store, nil)
	if withoutCanvas == nil {
		t.Fatal("composeDrama returned nil for a build without a canvas")
	}
	episode, err := withoutCanvas.script.CreateEpisode(ctx, appscript.CreateEpisodeRequest{
		ProjectID: projectID, SeasonNumber: 2, EpisodeNumber: 1, Title: "NoCanvas",
	})
	if err != nil {
		t.Fatalf("creating an episode: %v", err)
	}
	scriptRecord, err := withoutCanvas.script.EnsureScript(ctx, episode.ID)
	if err != nil {
		t.Fatalf("ensuring the script: %v", err)
	}
	skeleton, err := withoutCanvas.script.CreateStorySkeletonVersion(ctx, appscript.CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
	})
	if err != nil {
		t.Fatalf("creating a skeleton: %v", err)
	}
	strategy, err := withoutCanvas.script.CreateAdaptationStrategyVersion(ctx, appscript.CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "summary",
	})
	if err != nil {
		t.Fatalf("creating a strategy: %v", err)
	}
	version, err := withoutCanvas.script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
		ScriptID: scriptRecord.ID, StorySkeletonVersionID: skeleton.ID,
		AdaptationStrategyVersionID: strategy.ID, CreatedByType: versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("creating a script version: %v", err)
	}
	if _, err := withoutCanvas.script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
		ScriptID: scriptRecord.ID, ScriptVersionID: version.ID,
		Structure: wiringStructure(version.ID),
	}); err != nil {
		t.Fatalf("writing the structure: %v", err)
	}
	if _, err := withoutCanvas.script.ProjectScriptVersion(ctx, appscript.ProjectScriptVersionRequest{
		ProjectID: projectID, ScriptVersionID: version.ID,
	}); err == nil {
		t.Fatal("a build with no canvas reported a successful projection")
	}
	// And the stack without a canvas is otherwise USABLE: the refusal is about the projection, not about
	// the build. Without this half, a `composeDrama` that refused everything would pass the assertion above.
	if _, err := withoutCanvas.script.GetScriptStructure(ctx, version.ID); err != nil {
		t.Fatalf("a build without a canvas cannot read a structure: %v", err)
	}
}

// TestTheStageCommandsRefuseWithoutAPipeline covers the fail-closed direction of the stage surface.
//
// The pipeline is attached by the agent stack, and a build whose agent stack did not compose has none. The
// binding must then REFUSE rather than report success — a stage that "ran" with no runtime behind it would
// be a workflow advancing on nothing, which is worse than an error a user can read.
//
// This test exists because the OPPOSITE state was a real defect: the pipeline was composed and attached
// nowhere, so every stage command was unreachable while everything else worked. A refusal test is the cheap
// half of the pair; the expensive half is `TestComposeAgentsSuppliesTheScriptPipeline` below, which asserts
// that a composed build actually has one.
func TestTheStageCommandsRefuseWithoutAPipeline(t *testing.T) {
	binding := &desktop.DramaBinding{}
	if _, err := binding.RunScriptStage(desktop.RunScriptStageRequest{Stage: "story_skeleton"}); err == nil {
		t.Fatal("a binding with no pipeline ran a stage")
	}
	if _, err := binding.ApplyScriptGate(desktop.ApplyScriptGateRequest{Decision: "approve"}); err == nil {
		t.Fatal("a binding with no pipeline applied a gate decision")
	}
	if _, err := binding.StartScriptRevision("stage-1"); err == nil {
		t.Fatal("a binding with no pipeline started a revision")
	}
	if _, err := binding.ManualEditScript(desktop.ManualEditScriptRequest{Stage: "story_skeleton"}); err == nil {
		t.Fatal("a binding with no pipeline accepted a manual edit")
	}
	if _, err := binding.RunScriptSupervision(desktop.RunScriptSupervisionRequest{StageRunID: "s"}); err == nil {
		t.Fatal("a binding with no pipeline ran a supervision")
	}
}

// TestComposeDramaSuppliesTheGapService is the assertion that keeps AC-BOARD-001's gate
// from refusing in every real build.
//
// The batch gate reads an episode's approved gap report through this service, and its
// unresolved-required-items query is what decides whether a batch may run. A build that
// composed no gap service would have that query refuse for a missing dependency rather
// than answer — a refusal that looks like "no analysis", which is the "interface with no
// real path" shape this repository's reviews have found twice.
//
// The assertion is on the BEHAVIOUR a caller sees, because the service exposes no way to
// ask "are you composed": an episode with no report must answer with the report-specific
// refusal, not with a storage failure.
func TestComposeDramaSuppliesTheGapService(t *testing.T) {
	ctx := context.Background()
	stack, canvasWriter, _ := composedDrama(t)
	projectID := seedWiringProject(t, canvasWriter)
	episode, err := stack.script.CreateEpisode(ctx, appscript.CreateEpisodeRequest{
		ProjectID: projectID, SeasonNumber: 1, EpisodeNumber: 1, Title: "Gaps",
	})
	if err != nil {
		t.Fatalf("creating an episode: %v", err)
	}
	// No report is approved yet, so the query refuses — and it refuses with the message
	// that says WHICH situation this is.
	_, err = stack.gaps.UnresolvedRequiredItems(ctx, episode.ID)
	if err == nil {
		t.Fatal("an episode with no approved gap report reported no missing assets")
	}
	domainErr, ok := asset.AsError(err)
	if !ok || domainErr.Category != asset.CategoryConflict {
		t.Fatalf("the refusal is %v, want the conflict that says no report is approved", err)
	}
	if !strings.Contains(domainErr.SafeMessage, "approved gap report") {
		t.Fatalf("the refusal reads %q, which does not say what is missing", domainErr.SafeMessage)
	}
	if !stack.gaps.Available() {
		t.Fatal("the composed stack carries no gap service")
	}
}

// seedWiringProject creates a drama project through the real service.
//
// IT GOES THROUGH THE SERVICE RATHER THAN INSERTING A ROW, and the first version of this fixture did
// insert one — which failed with "the requested item no longer exists" when the projection ran, because a
// canvas document is created BY `CreateDramaProject` and a hand-written row has none. The failure was the
// fixture's fault and it was worth keeping: it is exactly the difference between "a project row exists" and
// "a project exists", and the projection needs the second.
func seedWiringProject(t *testing.T, canvasWriter *appprojects.Service) string {
	t.Helper()
	ctx := context.Background()
	// No workspace is created here: migration 000004 seeds the local one, and `CreateDramaProject` looks it
	// up by its fixed identifier. A fixture that inserted one would be inserting a row the schema already
	// owns.
	record, err := canvasWriter.CreateDramaProject(ctx, appprojects.CreateDramaProjectRequest{
		Name: "Wiring", Language: "zh-CN",
	})
	if err != nil {
		t.Fatalf("creating the project: %v", err)
	}
	return record.ID
}

// wiringStructure builds a two-scene structure for the projection assertions.
func wiringStructure(versionID string) scriptdomain.ScriptStructure {
	return scriptdomain.ScriptStructure{ScriptVersionID: versionID, Scenes: []scriptdomain.SceneStructure{
		{Scene: sceneAtWiring(versionID, 1, "INT. one - day", 90)},
		{Scene: sceneAtWiring(versionID, 2, "EXT. two - night", 60)},
	}}
}

// sceneAtWiring builds one scene at an ordinal.
//
// The timestamps and the revision are stated because the REAL repository writes them: an in-memory double
// fills them from its clock, and a fixture through the production path has to supply what the columns
// require. The `revision >= 1` CHECK is what makes a zero fail, which the first version of this fixture
// did.
func sceneAtWiring(versionID string, ordinal int, slugline string, seconds int) scriptdomain.Scene {
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return scriptdomain.Scene{
		ID: "wiring-scene-" + string(rune('0'+ordinal)), ScriptVersionID: versionID, Ordinal: ordinal,
		SceneNumber: string(rune('0' + ordinal)), Slugline: slugline,
		InteriorExterior:         scriptdomain.InteriorINT,
		EstimatedDurationSeconds: seconds,
		CreatedAt:                stamp, UpdatedAt: stamp, Revision: 1,
	}
}
