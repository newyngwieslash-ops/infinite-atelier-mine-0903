package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	infrajobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/jobs"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// batch_wiring_test.go covers AC-BOARD-001 and AC-BOARD-003 through the REAL stack:
// the production pipeline's gate and its batch, over a migrated database.
//
// It is here rather than in the package's own test files because the two commands need a
// database, a job store and four services composed together — which is what this file's
// helpers already build. A unit test with doubles could not answer the question the
// acceptance criteria ask, because both are about what a SECOND call sees: the gate reads
// the four approvals as rows, and the batch's idempotency is a uniqueness constraint.

// batchHarness is the production stack over a fresh database.
type batchHarness struct {
	drama      *dramaWiring
	production *productionpipeline.Service
	jobs       *appjobs.Service
	db         *sql.DB
	projectID  string
	// images is the deterministic adapter this harness registered, so a test can assert what
	// the batch asked a provider for.
	images *infraproviders.MockImageAdapter
	// imageProviderID is the config row whose kind resolves to that adapter.
	imageProviderID string
	// lastGapReportID is the id of the most recent report `writeGapReport` recorded, so a
	// test can assert on the approval of a report it deliberately did NOT approve.
	lastGapReportID string
}

// newBatchHarness composes the production pipeline over the drama stack.
func newBatchHarness(t *testing.T) *batchHarness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	handle, err := database.Open(ctx, filepath.Join(dir, "studio.db"), filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })
	store, err := filestore.New(filepath.Join(dir, "files"), filepath.Join(dir, "temp"))
	if err != nil {
		t.Fatalf("opening the file store: %v", err)
	}
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
	// THE JOB STACK IS COMPLETE HERE rather than a bare service, because these tests are about
	// a batch's jobs EXECUTING: a service with no runner accepts submissions and settles
	// nothing, so every collection assertion would be about a job that never ran.
	//
	// The image adapter is supplied DIRECTLY to the runner, through the `AdapterSource` port
	// the runner declares for exactly this. Going through the registry could not work: the
	// registry resolves a provider by reading its CONFIG and dispatching on kind, so the mock
	// needs a `provider_configs` row carrying `mock_image` — and the schema's CHECK does not
	// admit that kind, which is the guardrail that stops a real configuration selecting it.
	// A harness that wanted one would have to change the schema, and the guardrail is worth
	// more than the fixture's convenience.
	//
	// What matters for these tests is that an image job RUNS and produces a file the
	// collection can turn into a candidate; which adapter answered is not their subject.
	images := infraproviders.NewMockImageAdapter()
	resultStore := infrajobs.NewResultStore(store, database.NewFileRepository(handle.SQL()), database.NewFileReferenceRepository(handle.SQL()))
	runner := infrajobs.NewRunner(batchAdapterSource{images: images}, resultStore, nil, maxJobResultBytes)
	jobService := appjobs.NewService(appjobs.Options{
		Repository: database.NewJobRepository(handle.SQL()),
		Clock:      appjobs.NewClockFunc(func() time.Time { return time.Now().UTC() }),
		IDs:        appjobsPrefixIDs{inner: id.NewGenerator()},
		Publisher:  discardPublisher{},
		Runner:     runner,
		Policy:     job.DefaultRetryPolicy(),
	})
	production := productionpipeline.New(productionpipeline.Options{
		Storyboard: stack.storyboard,
		Gaps:       stack.gaps,
		Jobs:       jobService,
		Assets:     stack.assets,
		Workflow:   stack.workflow,
	})
	// The provider id the batch is asked to submit to. It needs no config row, because the
	// adapter source below answers for every id: what a real build resolves through a
	// configured provider is resolved here directly.
	const imageProviderID = "canary-image-provider"
	return &batchHarness{
		drama: stack, production: production, jobs: jobService,
		db: handle.SQL(), images: images, imageProviderID: imageProviderID,
		projectID: seedWiringProject(t, canvasWriter),
	}
}

// seedStoryboardWithShots writes an approved board with the requested number of rows.
//
// It goes through the production services rather than inserting rows, so what the batch
// reads is what a workflow would have produced.
func (h *batchHarness) seedStoryboardWithShots(t *testing.T, shots int) (versionID, episodeID string) {
	t.Helper()
	ctx := context.Background()
	episode, err := h.drama.script.CreateEpisode(ctx, appscript.CreateEpisodeRequest{
		ProjectID: h.projectID, SeasonNumber: 1, EpisodeNumber: 1, Title: "Batch",
	})
	if err != nil {
		t.Fatalf("creating an episode: %v", err)
	}
	scriptRecord, err := h.drama.script.EnsureScript(ctx, episode.ID)
	if err != nil {
		t.Fatalf("ensuring the script: %v", err)
	}
	skeleton, err := h.drama.script.CreateStorySkeletonVersion(ctx, appscript.CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", EndingHook: "ending",
	})
	if err != nil {
		t.Fatalf("creating a skeleton: %v", err)
	}
	strategy, err := h.drama.script.CreateAdaptationStrategyVersion(ctx, appscript.CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "summary",
	})
	if err != nil {
		t.Fatalf("creating a strategy: %v", err)
	}
	scriptVersion, err := h.drama.script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
		ScriptID: scriptRecord.ID, StorySkeletonVersionID: skeleton.ID,
		AdaptationStrategyVersionID: strategy.ID, CreatedByType: versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("creating a script version: %v", err)
	}
	// The SHOTS are written directly, because the script stage's structure write is the
	// path the canary covers and this fixture is about what the board does with them. The
	// ids are what the board's rows cite, which is the fact under test.
	structure := script.ScriptStructure{ScriptVersionID: scriptVersion.ID}
	for index := 1; index <= shots; index++ {
		scene := script.Scene{
			ID: "batch-scene-" + itoaTest(index), ScriptVersionID: scriptVersion.ID, Ordinal: index,
			SceneNumber: itoaTest(index), Slugline: "INT. scene " + itoaTest(index) + " - day",
			InteriorExterior: script.InteriorINT, CreatedAt: fixedWiringTime(), UpdatedAt: fixedWiringTime(), Revision: 1,
		}
		structure.Scenes = append(structure.Scenes, script.SceneStructure{
			Scene: scene,
			Shots: []script.Shot{{
				ID: "batch-shot-" + itoaTest(index), SceneID: scene.ID, Ordinal: 1,
				ShotNumber: itoaTest(index), VisualDescription: "a corridor",
				Status: versioning.StatusDraft, CreatedAt: fixedWiringTime(), UpdatedAt: fixedWiringTime(), Revision: 1,
			}},
		})
	}
	if _, err := h.drama.script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
		ScriptID: scriptRecord.ID, ScriptVersionID: scriptVersion.ID, Structure: structure,
	}); err != nil {
		t.Fatalf("writing the structure: %v", err)
	}
	plan, err := h.drama.storyboard.CreateDirectorPlanVersion(ctx, appstoryboard.CreateDirectorPlanVersionRequest{
		EpisodeID: episode.ID, ScriptVersionID: scriptVersion.ID,
	})
	if err != nil {
		t.Fatalf("creating the director plan: %v", err)
	}
	board, err := h.drama.storyboard.EnsureStoryboard(ctx, episode.ID)
	if err != nil {
		t.Fatalf("ensuring the storyboard: %v", err)
	}
	version, err := h.drama.storyboard.CreateStoryboardVersion(ctx, appstoryboard.CreateStoryboardVersionRequest{
		StoryboardID: board.ID, ScriptVersionID: scriptVersion.ID,
		DirectorPlanVersionID: plan.ID, CreatedByType: versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("creating the storyboard version: %v", err)
	}
	for index := 1; index <= shots; index++ {
		if _, err := h.drama.storyboard.CreateStoryboardItem(ctx, appstoryboard.CreateStoryboardItemRequest{
			StoryboardVersionID: version.ID, ShotID: "batch-shot-" + itoaTest(index), Ordinal: index,
			ShotSize: "MS", DurationSeconds: 4, VisualDescription: "a corridor at dusk",
			FirstFrameDescription: "the door ajar",
		}); err != nil {
			t.Fatalf("creating storyboard item %d: %v", index, err)
		}
	}
	// The board is APPROVED, because a batch refuses one that is not.
	if _, err := h.drama.storyboard.ApproveStoryboardVersion(ctx, appstoryboard.ApproveStoryboardVersionRequest{
		VersionID: version.ID,
	}); err != nil {
		t.Fatalf("approving the storyboard version: %v", err)
	}
	return version.ID, episode.ID
}

// writeGapReport records an analysis and remembers its id, without approving it.
func (h *batchHarness) writeGapReport(t *testing.T, episodeID string, satisfied bool) {
	t.Helper()
	ctx := context.Background()
	// The script version the report is ABOUT is read from the table, because a report
	// names a VERSION rather than an approval: the fixture approves none, and the schema's
	// foreign key would refuse one that does not exist.
	var versionID string
	if err := h.db.QueryRowContext(ctx,
		`SELECT v.id FROM script_versions v
		 JOIN scripts s ON s.id = v.script_id
		 WHERE s.episode_id = ? ORDER BY v.version_number DESC LIMIT 1`, episodeID).Scan(&versionID); err != nil {
		t.Fatalf("reading the episode's script version: %v", err)
	}
	status := asset.GapMissing
	assetID := ""
	if satisfied {
		status = asset.GapSatisfied
		assetID = "batch-asset-version"
	}
	report, _, err := h.drama.gaps.CreateGapReport(ctx, appassets.CreateGapReportRequest{
		EpisodeID: episodeID, ScriptVersionID: versionID,
		Items: []appassets.GapItemInput{{
			AssetType: asset.TypeCharacter, StoryEntityID: "entity-1",
			AssetID: assetID, Status: status, Required: true,
		}},
	})
	if err != nil {
		t.Fatalf("creating the gap report: %v", err)
	}
	h.lastGapReportID = report.ID
	if !satisfied {
		return
	}
	if _, err := h.drama.gaps.ApproveGapReport(ctx, appassets.ApproveGapReportRequest{ReportID: report.ID}); err != nil {
		t.Fatalf("approving the gap report: %v", err)
	}
}

// approveGapReport writes and approves an episode's analysis, which only a report whose
// required assets are satisfied can be.
func (h *batchHarness) approveGapReport(t *testing.T, episodeID string, satisfied bool) {
	t.Helper()
	h.writeGapReport(t, episodeID, satisfied)
}

// TestTheGateBlocksABatchWhileTheEpisodeHasNoAnalysedAssets covers AC-BOARD-001's block
// and the roadmap's "必需资产缺失时阻止批量", which are the SAME gate read two ways.
//
// THE MECHANISM IS WORTH STATING, because the obvious fixture does not work: a report with
// a missing required asset CANNOT BE APPROVED — `CanApproveGapReport` refuses it, which is
// the domain's whole point — so "an approved report that still has a required gap" is not a
// state this build can reach. The block is therefore enforced in two places that compose:
//
//   - at the APPROVAL, where a report with a missing required asset is refused, so a user
//     cannot put one in force;
//   - at the BATCH, where an episode with no approved report is refused, so a production
//     cannot run on an analysis nobody accepted.
//
// The second is what this asserts, because it is the gate's own half. A gate that treated
// "no approved report" as "nothing missing" would run a batch against shots whose required
// characters have no approved design, and the images would be generated anyway.
func TestTheGateBlocksABatchWhileTheEpisodeHasNoAnalysedAssets(t *testing.T) {
	ctx := context.Background()
	h := newBatchHarness(t)
	versionID, episodeID := h.seedStoryboardWithShots(t, 2)
	// The first half: a report is written but NOT approved, so nothing is in force.
	h.writeGapReport(t, episodeID, false)
	if _, err := h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: 1, ProviderID: "provider-1", ModelName: "model-1",
	}); err == nil {
		t.Fatal("a batch ran while the episode had no approved gap analysis")
	}
	// NOTHING was submitted, which is the part that matters: AC-BOARD-001 says the batch
	// is BLOCKED, and a batch that submitted some jobs and then refused would have spent
	// the user's provider budget on work it decided not to do.
	if count := jobCount(t, h.db, h.projectID); count != 0 {
		t.Fatalf("the blocked batch submitted %d jobs", count)
	}
	// And the report itself cannot be approved while a required asset is missing, which
	// is the other half of the same rule.
	if _, err := h.drama.gaps.ApproveGapReport(ctx, appassets.ApproveGapReportRequest{
		ReportID: h.lastGapReportID,
	}); err == nil {
		t.Fatal("a report with a missing required asset was approved")
	}
	// With the report approved, the same batch runs: the refusal above is about the
	// analysis and not about the fixtures.
	h.approveGapReport(t, episodeID, true)
	if _, err := h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: 1, ProviderID: "provider-1", ModelName: "model-1",
	}); err != nil {
		t.Fatalf("a batch was refused with the analysis approved: %v", err)
	}
}

// TestTheBatchSubmitsTwoCandidatesPerShotAndDoesNotRepeatThem covers AC-BOARD-003's first
// and last clauses in one case, because they are the same fact seen twice: two candidates
// are two jobs, and a re-run of the same request finds them rather than making more.
func TestTheBatchSubmitsTwoCandidatesPerShotAndDoesNotRepeatThem(t *testing.T) {
	ctx := context.Background()
	h := newBatchHarness(t)
	versionID, episodeID := h.seedStoryboardWithShots(t, 3)
	h.approveGapReport(t, episodeID, true)

	request := productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: 2, ProviderID: "provider-1", ModelName: "model-1",
	}
	first, err := h.production.RunImageBatch(ctx, request)
	if err != nil {
		t.Fatalf("running the batch: %v", err)
	}
	if len(first.Submitted) != 6 {
		t.Fatalf("the batch submitted %d jobs for 3 shots at 2 candidates", len(first.Submitted))
	}
	if first.Duplicate != 0 {
		t.Fatalf("the first batch reported %d duplicates", first.Duplicate)
	}
	// Both candidates of a shot are jobs, and they are DIFFERENT jobs: an input that did
	// not carry the candidate index would have collided with itself and the second would
	// have been a replay of the first.
	perShot := map[string]map[string]bool{}
	for _, submission := range first.Submitted {
		if perShot[submission.ShotID] == nil {
			perShot[submission.ShotID] = map[string]bool{}
		}
		perShot[submission.ShotID][submission.JobID] = true
	}
	for shotID, jobs := range perShot {
		if len(jobs) != 2 {
			t.Fatalf("shot %s has %d distinct jobs, want two candidates", shotID, len(jobs))
		}
	}

	// THE RESTART. The same request again — which is what a restarted batch sends — must
	// find the existing jobs rather than submitting duplicates, and the idempotency key
	// is what makes that true without a ledger of what was sent.
	second, err := h.production.RunImageBatch(ctx, request)
	if err != nil {
		t.Fatalf("re-running the batch: %v", err)
	}
	if len(second.Submitted) != 6 || second.Duplicate != 6 {
		t.Fatalf("the re-run submitted %d jobs of which %d were duplicates", len(second.Submitted), second.Duplicate)
	}
	for index, submission := range second.Submitted {
		if submission.JobID != first.Submitted[index].JobID {
			t.Fatalf("submission %d is job %s on the re-run and %s on the first",
				index, submission.JobID, first.Submitted[index].JobID)
		}
	}
	// And the STORE agrees: six rows, not twelve.
	if count := jobCount(t, h.db, h.projectID); count != 6 {
		t.Fatalf("the project has %d jobs after two identical batches", count)
	}
	// Every row is an image job filed under a storyboard ITEM: the entity is the item
	// rather than the shot, because an image belongs to one row of one board.
	for _, subject := range jobSubjectsFor(t, h.db, h.projectID) {
		if subject != string(job.JobTypeImageGeneration)+" storyboard_item" {
			t.Errorf("a job row reads %q", subject)
		}
	}
}

// TestTheBatchNarrowsToOneShotForASingleShotRedo covers AC-BOARD-002's scope at the batch
// level: a redo names ONE shot, and the batch must submit for that shot alone.
func TestTheBatchNarrowsToOneShotForASingleShotRedo(t *testing.T) {
	ctx := context.Background()
	h := newBatchHarness(t)
	versionID, episodeID := h.seedStoryboardWithShots(t, 4)
	h.approveGapReport(t, episodeID, true)

	result, err := h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: 1, ProviderID: "provider-1", ModelName: "model-1",
		ShotIDs: []string{"batch-shot-2"},
	})
	if err != nil {
		t.Fatalf("the single-shot redo: %v", err)
	}
	if len(result.Submitted) != 1 || result.Submitted[0].ShotID != "batch-shot-2" {
		t.Fatalf("the narrowed batch submitted %+v", result.Submitted)
	}
	// A shot NOT in the board is refused rather than silently ignored, so a caller with a
	// stale identifier learns it rather than getting an empty success.
	if _, err := h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: 1, ProviderID: "provider-1", ModelName: "model-1",
		ShotIDs: []string{"no-such-shot"},
	}); err == nil {
		t.Fatal("a batch naming a shot outside the board succeeded")
	}
}

// TestTheBatchRefusesAnUnapprovedBoard covers the other gate the batch makes.
//
// Generating images for a board under review would spend the user's budget on rows a FIX
// may replace, and the approval is the fact that says the rows are settled.
func TestTheBatchRefusesAnUnapprovedBoard(t *testing.T) {
	ctx := context.Background()
	h := newBatchHarness(t)
	// The board is written but NOT approved: `seedStoryboardWithShots` approves, so this
	// case builds its own draft by approving and then superseding with a new draft.
	versionID, episodeID := h.seedStoryboardWithShots(t, 2)
	h.approveGapReport(t, episodeID, true)
	version, err := h.drama.storyboard.GetStoryboardVersion(ctx, versionID)
	if err != nil {
		t.Fatalf("reading the version: %v", err)
	}
	draft, err := h.drama.storyboard.CreateStoryboardVersion(ctx, appstoryboard.CreateStoryboardVersionRequest{
		StoryboardID: version.StoryboardID, ScriptVersionID: version.ScriptVersionID,
		DirectorPlanVersionID: version.DirectorPlanVersionID,
		BasedOnVersionID:      version.ID, CreatedByType: versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("creating the draft version: %v", err)
	}
	// The draft NEEDS ROWS, and this is the difference between a test of the approval and
	// one that passes for the wrong reason: without them the batch refuses because the
	// board is EMPTY — a different refusal with the same shape — and a mutation that
	// dropped the approval check survived it. The rows are the same shots the approved
	// version holds, which is what a revision is.
	for index := 1; index <= 2; index++ {
		if _, err := h.drama.storyboard.CreateStoryboardItem(ctx, appstoryboard.CreateStoryboardItemRequest{
			StoryboardVersionID: draft.ID, ShotID: "batch-shot-" + itoaTest(index), Ordinal: index,
			ShotSize: "MS", DurationSeconds: 4,
		}); err != nil {
			t.Fatalf("creating draft row %d: %v", index, err)
		}
	}
	_, err = h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: draft.ID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: 1, ProviderID: "provider-1", ModelName: "model-1",
	})
	if err == nil {
		t.Fatal("a batch ran against an unapproved board")
	}
	// The refusal NAMES the approval, so a caller's next step is to approve the board
	// rather than to hunt for a missing row.
	if !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("the refusal reads %q, which does not say the board is unapproved", err)
	}
	// And nothing was submitted, which is the part that matters: the batch is blocked
	// rather than partially run.
	if count := jobCount(t, h.db, h.projectID); count != 0 {
		t.Fatalf("the blocked batch submitted %d jobs", count)
	}
}

// TestTheGateRefusesAnEpisodeWithNoGapAnalysis covers the "unknown is not ready" rule.
//
// An episode nobody analysed has no report, and a gate that treated "no report" as
// "nothing missing" would run a batch against shots whose required characters have no
// approved design.
func TestTheGateRefusesAnEpisodeWithNoGapAnalysis(t *testing.T) {
	ctx := context.Background()
	h := newBatchHarness(t)
	versionID, episodeID := h.seedStoryboardWithShots(t, 2)
	// No gap report is written at all.
	if _, err := h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: 1, ProviderID: "provider-1", ModelName: "model-1",
	}); err == nil {
		t.Fatal("a batch ran for an episode with no gap analysis")
	}
}

// fixedWiringTime is the timestamp these fixtures stamp their rows with.
func fixedWiringTime() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

// itoaTest renders a small positive integer without importing strconv for a fixture.
func itoaTest(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// discardPublisher satisfies the job service's publisher port without publishing.
type discardPublisher struct{}

func (discardPublisher) PublishJobChanged(context.Context, appjobs.JobEvent) {}

// appjobsPrefixIDs adapts the platform generator to the job service's port.
type appjobsPrefixIDs struct{ inner *id.Generator }

func (g appjobsPrefixIDs) NewID(prefix string) string {
	value, err := g.inner.New()
	if err != nil {
		// The generator's only failure is entropy, which is not something a test can
		// recover from; a panic here would be reported as the test's failure rather than
		// as a silent zero id.
		panic(err)
	}
	return prefix + "-" + value
}

// jobCount reports how many jobs a project has, read from the table rather than through
// the service: the service's own list filter carries no project field, and a fixture that
// filtered in Go would be answering a different question than "what is in the database".
func jobCount(t *testing.T, db *sql.DB, projectID string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM generation_jobs WHERE project_id = ?`, projectID).Scan(&count); err != nil {
		t.Fatalf("counting the project's jobs: %v", err)
	}
	return count
}

// jobSubjectsFor reports each of a project's jobs as "type entityType", read from the
// table rather than through the service for the same reason as `jobCount`.
func jobSubjectsFor(t *testing.T, db *sql.DB, projectID string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT job_type, entity_type FROM generation_jobs WHERE project_id = ? ORDER BY created_at, id`, projectID)
	if err != nil {
		t.Fatalf("reading the project's jobs: %v", err)
	}
	defer rows.Close()
	subjects := []string{}
	for rows.Next() {
		var jobType, entityType string
		if err := rows.Scan(&jobType, &entityType); err != nil {
			t.Fatalf("reading a job row: %v", err)
		}
		subjects = append(subjects, jobType+" "+entityType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the project's jobs: %v", err)
	}
	return subjects
}

// batchAdapterSource answers the runner's adapter lookup with the deterministic image mock.
//
// It is the seam `infrastructure/jobs` declares for a caller that supplies its own adapters —
// the same port the registry implements in a real build — and it exists here because the
// registry CANNOT answer for the mock: the schema's kind CHECK does not admit `mock_image`,
// which is the guardrail that keeps a real configuration from selecting it.
type batchAdapterSource struct {
	images *infraproviders.MockImageAdapter
}

func (s batchAdapterSource) ImagePortFor(context.Context, string) (appjobs.ImagePort, error) {
	if s.images == nil {
		return nil, provider.NewUnsupportedError()
	}
	return s.images, nil
}

// VideoPortFor and AudioPortFor report unsupported, which is the honest answer for a harness
// whose subject is an IMAGE batch: a video job reaching this source is a job the harness did
// not mean to submit, and a mock answer would hide that.
func (batchAdapterSource) VideoPortFor(context.Context, string) (appjobs.VideoPort, error) {
	return nil, provider.NewUnsupportedError()
}

func (batchAdapterSource) AudioPortFor(context.Context, string) (appjobs.AudioPort, error) {
	return nil, provider.NewUnsupportedError()
}
