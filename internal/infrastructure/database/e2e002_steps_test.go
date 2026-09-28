package database

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	appproduction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appscriptpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/scriptpipeline"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// e2e002_steps_test.go holds the AC-E2E-002 walk's chain steps.
//
// They are in their own file because each is a stage or a command sequence with its own reasoning,
// and the walk itself reads better as the criterion's clauses in order. Every function here is
// called once, from `TestE2E002FromNovelToApprovedPanelImages`, and the order of the calls is the
// order of the criterion.
//
// TWO PIPELINES, and which stage belongs to which is the layer's own split rather than this file's
// choice: the script stages (`story_skeleton`, `adaptation_strategy`, `script_generation`) go
// through the script canary's `pipeline`, and the production stages (`asset_gap_analysis` …
// `storyboard_table`) through the production pipeline this walk composes. Both run over the same
// stack, so the artefacts one writes are what the other reads.

// acE2E002ShotFloor is the number of shots the walk asks the script for.
//
// PRD §19's AC-E2E-002 asks for a board of 「不少于 12 个镜头」, and the board's rows come from the
// script's shots — so the requirement is a property of the SCRIPT the episode was written from, and
// the request is made where the script is generated. 12 rather than more: the criterion names a
// floor, and a bigger number would cost the batch jobs without proving anything the floor does not.
const acE2E002ShotFloor = 12

// runScriptChain drives 骨架 → 策略 → 剧本, supervising and gating each stage, and returns the
// approved script version's id.
//
// It uses the SCRIPT canary's own helpers — `runScriptStage`, `review`, `pass`,
// `seedScriptVersionRow`, `singleArtifactOf` — rather than re-implementing them, so this walk and
// `TestCanaryScriptPipelineToApprovedScript` drive the same code path. What this adds over that
// canary is the assertion after each gate: the APPROVED row is read back, because 「完成」 is a
// statement about what the database holds.
func (w *e2e002Walk) runScriptChain(t *testing.T, episodeID string) string {
	t.Helper()

	// The events the skeleton selects, so the link set names rows that exist.
	eventIDs := w.seedStoryEvents(t, 2)

	// --- the story skeleton ------------------------------------------------------------------
	skeleton := w.runScriptStage(t, stagepipeline.StageRequest{
		WorkflowRunID: w.ids.workflowRun,
		Stage:         appscriptpipeline.StageStorySkeleton,
		ProjectID:     w.ids.project,
		EpisodeID:     episodeID,
		Task:          "Write the skeleton for this episode.",
		State:         appscriptpipeline.StateFields{SelectedEventIDs: eventIDs},
	})
	w.assertStageReviewed(t, skeleton.StageRun, string(appscriptpipeline.StageStorySkeleton))
	skeletonVersionID := w.singleArtifactOf(t, skeleton, "story_skeleton")
	if reviewed := w.review(t, skeleton.StageRun, skeletonVersionID); !reviewed.Report.Passed {
		t.Fatalf("clause 5: the skeleton review failed: %s", reviewed.Report.Summary)
	}
	w.pass(t, skeleton.StageRun, skeletonVersionID, "the skeleton is what we want to shoot.")
	w.assertApproved(t, "story_skeleton_versions", skeletonVersionID)

	// --- the adaptation strategy --------------------------------------------------------------
	strategy := w.runScriptStage(t, stagepipeline.StageRequest{
		WorkflowRunID: w.ids.workflowRun,
		Stage:         appscriptpipeline.StageAdaptationStrategy,
		ProjectID:     w.ids.project,
		EpisodeID:     episodeID,
		Task:          "Decide what happens to each event.",
	})
	w.assertStageReviewed(t, strategy.StageRun, string(appscriptpipeline.StageAdaptationStrategy))
	strategyVersionID := w.singleArtifactOf(t, strategy, "adaptation_strategy")
	if reviewed := w.review(t, strategy.StageRun, strategyVersionID); !reviewed.Report.Passed {
		t.Fatalf("clause 5: the strategy review failed: %s", reviewed.Report.Summary)
	}
	w.pass(t, strategy.StageRun, strategyVersionID, "the strategy is sound.")
	w.assertApproved(t, "adaptation_strategy_versions", strategyVersionID)

	// --- the script itself, which is a version ROW plus a STRUCTURE ---------------------------
	//
	// The row is created BEFORE the stage runs, because a structure needs a version to hang off and
	// the write tool takes the version's id. That is the ordering the pipeline's own state layer
	// assumes, which is why the generation stage's prompt carries `script_version=`.
	scriptVersionID := w.seedScriptVersionRow(t, skeletonVersionID, strategyVersionID)
	generation := w.runScriptStage(t, stagepipeline.StageRequest{
		WorkflowRunID: w.ids.workflowRun,
		Stage:         appscriptpipeline.StageScriptGeneration,
		ProjectID:     w.ids.project,
		EpisodeID:     episodeID,
		Task:          "Write the script.",
		State: appscriptpipeline.StateFields{
			ScriptVersionID:   scriptVersionID,
			SkeletonVersionID: skeletonVersionID,
			StrategyVersionID: strategyVersionID,
			// The criterion's shape requirement, asked for where the script is written.
			ShotCount: acE2E002ShotFloor,
		},
	})
	w.assertStageReviewed(t, generation.StageRun, string(appscriptpipeline.StageScriptGeneration))
	// The write went to the version the state named, which is what makes the row and its content one
	// artefact rather than two.
	if ids := generation.ArtifactIDs; len(ids) != 1 || ids[0] != scriptVersionID {
		t.Fatalf("clause 4: the generation stage wrote %v, want the version the state named (%q)", ids, scriptVersionID)
	}
	if reviewed := w.review(t, generation.StageRun, scriptVersionID); !reviewed.Report.Passed {
		t.Fatalf("clause 5: the script review failed: %s", reviewed.Report.Summary)
	}
	w.pass(t, generation.StageRun, scriptVersionID, "shoot this.")
	w.assertApproved(t, "script_versions", scriptVersionID)
	return scriptVersionID
}

// assertApproved row-checks that a version is its family's approved one.
//
// It reads the STORED row rather than a returned struct, because 「通过」 and 「完成」 are statements
// about what the database holds: a service that answered correctly while the row said otherwise is
// the class of defect this walk was written to find.
func (w *e2e002Walk) assertApproved(t *testing.T, table, versionID string) {
	t.Helper()
	// The table name comes from this file's own call sites rather than from a caller, which is what
	// makes interpolating it safe — a parameterised table name is not a thing SQL supports, and the
	// alternative would be one query per family.
	switch table {
	case "story_skeleton_versions", "adaptation_strategy_versions", "script_versions",
		"director_plan_versions", "storyboard_versions", "asset_gap_reports":
	default:
		t.Fatalf("assertApproved was asked about the unknown family %q", table)
	}
	var count int
	if err := w.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM `+table+` WHERE id = ? AND status = 'approved'`, versionID).Scan(&count); err != nil {
		t.Fatalf("reading the approved row: %v", err)
	}
	if count != 1 {
		t.Fatalf("the version %s is not the approved row of %s, so the stage that produced it did not put it in force", versionID, table)
	}
}

// runGapAnalysis drives the asset gap report to an approved report and returns its id.
//
// The stage has NO supervisor — section 10.1's own setting, which the production layer reflects —
// so it parks at `waiting_user` and the user gate IS its review. Calling the supervisor here would
// fail for the honest reason that no supervision agent serves this stage.
func (w *e2e002Walk) runGapAnalysis(t *testing.T, episodeID, scriptVersionID string) string {
	t.Helper()
	// The state names the SCRIPT VERSION, because the report the stage writes cites the script it
	// analysed: a gap report that named no script would be a list of assets for an unknown episode.
	stage := w.runProductionStage(t, stagepipeline.StageRequest{
		WorkflowRunID: w.ids.workflowRun,
		Stage:         appproduction.StageAssetGapAnalysis,
		ProjectID:     w.ids.project,
		EpisodeID:     episodeID,
		Task:          "Say which story facts this episode needs assets for.",
		State:         appproduction.StateFields{EpisodeID: episodeID, ScriptVersionID: scriptVersionID},
	})
	reportID := w.reportIDOf(t, stage)
	if reportID == "" {
		t.Fatal("clause 6: the gap stage wrote no report")
	}
	w.passThrough(t, w.production, stage.StageRun, reportID, "the list is right.")
	w.assertApproved(t, "asset_gap_reports", reportID)
	t.Logf("clause 6: gap report %s is approved", reportID)
	return reportID
}

// runAssetGeneration drives the asset_generation stage and returns the character and scene assets.
//
// # This stage had no driver anywhere in the repository before this walk
//
// It is in the policy table and the production layer serves it — with an execution agent and NO
// supervisor — and no test ran it: the routing test named the constant and stopped there.
//
// # The two decisions worth naming, both consequences of the stage's contract
//
//   - The assets the criterion counts (「2 个角色、2 个场景」) are created through the ASSETS service
//     first. The stage's execution agent writes a candidate VERSION against an asset, so the asset
//     must exist for it to write against; creating them is what a user does.
//   - The stage is NOT gated to `passed`. The mock's write produces a version with no committed file
//     and `ApproveVersion` refuses one — the mock's limit rather than the stage's, since a real
//     provider produces bytes. The walk therefore asserts what it can prove: the stage ran, its
//     agent reached its write, and the counted assets exist with versions. Driving the gate would be
//     asserting a state the mock cannot reach.
func (w *e2e002Walk) runAssetGeneration(t *testing.T, episodeID, gapReportID string) (characters, scenes []string) {
	t.Helper()
	ctx := context.Background()

	targets := []struct {
		name      string
		assetType asset.Type
	}{
		{"林砚", asset.TypeCharacter},
		{"老周", asset.TypeCharacter},
		{"渡口", asset.TypeLocation},
		{"盐仓", asset.TypeLocation},
	}
	for _, target := range targets {
		record, _, err := w.assets.CreateAsset(ctx, appassets.CreateAssetRequest{
			ProjectID: w.ids.project, Type: target.assetType, Name: target.name,
		})
		if err != nil {
			t.Fatalf("clause 7: creating the %s asset %q: %v", target.assetType, target.name, err)
		}
		if target.assetType == asset.TypeCharacter {
			characters = append(characters, record.ID)
		} else {
			scenes = append(scenes, record.ID)
		}
	}

	// The stage runs over the assets the walk just created, and it is TOLD which ones: the write
	// tool resolves the asset it is given and refuses an unknown id, so a model that could not see
	// the ids could only guess them. That is the same reason `ShotIDs` travels to the board stage.
	stage := w.runProductionStage(t, stagepipeline.StageRequest{
		WorkflowRunID: w.ids.workflowRun,
		Stage:         appproduction.StageAssetGeneration,
		ProjectID:     w.ids.project,
		EpisodeID:     episodeID,
		Task:          "Plan the asset generation for this episode.",
		State: appproduction.StateFields{
			EpisodeID:        episodeID,
			AssetGapReportID: gapReportID,
			AssetIDs:         append(append([]string{}, characters...), scenes...),
		},
	})
	if !e2e002ReachedAWrite(stage) {
		t.Fatal("clause 7: the asset generation stage ran and its agent reached no write call")
	}
	if stage.StageRun.Status != workflow.StageWaitingUser && stage.StageRun.Status != workflow.StageReviewing {
		t.Fatalf("clause 7: the asset generation stage ended at %q, and the layer serves it with no supervisor so it should be waiting for a person", stage.StageRun.Status)
	}

	// And every asset the criterion counts exists with the version `CreateAsset` writes for it.
	for _, id := range append(append([]string{}, characters...), scenes...) {
		versions, err := w.assets.ListVersions(ctx, id)
		if err != nil {
			t.Fatalf("clause 7: reading the asset's versions: %v", err)
		}
		if len(versions) == 0 {
			t.Fatalf("clause 7: the asset %s has no version, so nothing was created for it", id)
		}
	}
	return characters, scenes
}

// e2e002ReachedAWrite reports whether the stage's agent called a write tool.
//
// It reads the run's recorded TOOL CALLS rather than the stage's artifact list, because the mock's
// production reply for this stage is `partial` — there is no single artefact for it to name — so the
// call record is where the write is visible.
func e2e002ReachedAWrite(result stagepipeline.StageResult) bool {
	for _, call := range result.Outcome.ToolCalls {
		if call.ToolKey != "" {
			return true
		}
	}
	return false
}

// runDirectorPlan drives FR-060's planning stage to an approved plan and returns its version id.
//
// It sits between the script and the board because the board CITES the plan it was drawn from, and
// the repository's foreign key refuses a version naming a plan that does not exist — so a walk that
// skipped this stage would be a walk whose board could not be written.
func (w *e2e002Walk) runDirectorPlan(t *testing.T, episodeID, scriptVersionID string) string {
	t.Helper()
	stage := w.runProductionStage(t, stagepipeline.StageRequest{
		WorkflowRunID: w.ids.workflowRun,
		Stage:         appproduction.StageDirectorPlan,
		ProjectID:     w.ids.project,
		EpisodeID:     episodeID,
		Task:          "Plan how this episode is shot.",
		State:         appproduction.StateFields{EpisodeID: episodeID, ScriptVersionID: scriptVersionID},
	})
	w.assertStageReviewed(t, stage.StageRun, string(appproduction.StageDirectorPlan))
	planVersionID := w.versionOfToolCall(t, stage, "storyboard.create_director_plan_version")
	if planVersionID == "" {
		t.Fatal("clause 8: the plan stage wrote no version")
	}
	if reviewed := w.reviewProduction(t, stage.StageRun, planVersionID); !reviewed.Report.Passed {
		t.Fatalf("clause 8: the plan review failed: %s", reviewed.Report.Summary)
	}
	w.passThrough(t, w.production, stage.StageRun, planVersionID, "the plan is what we want.")
	w.assertApproved(t, "director_plan_versions", planVersionID)
	return planVersionID
}

// shotIDsOf returns a script version's shots in the script's own order.
//
// The order is the STRUCTURE's rather than a table's: §7.6 makes a scene's shots ordinals 1..n, so
// reading scenes in their ordinal order and each scene's shots in theirs is the order the script is
// written in — which is the order the board must board them in.
func (w *e2e002Walk) shotIDsOf(t *testing.T, scriptVersionID string) []string {
	t.Helper()
	structure, err := w.script.GetScriptStructure(context.Background(), scriptVersionID)
	if err != nil {
		t.Fatalf("clause 8: reading the script structure: %v", err)
	}
	ids := make([]string, 0)
	for _, scene := range structure.Scenes {
		for _, shot := range scene.Shots {
			ids = append(ids, shot.ID)
		}
	}
	return ids
}

// runStoryboardTable drives the storyboard table stage to an approved board and returns it.
func (w *e2e002Walk) runStoryboardTable(t *testing.T, episodeID, scriptVersionID, planVersionID string) string {
	t.Helper()
	shotIDs := w.shotIDsOf(t, scriptVersionID)
	if len(shotIDs) == 0 {
		t.Fatal("clause 8: the approved script has no shots, so there is nothing to board")
	}
	stage := w.runProductionStage(t, stagepipeline.StageRequest{
		WorkflowRunID: w.ids.workflowRun,
		Stage:         appproduction.StageStoryboardTable,
		ProjectID:     w.ids.project,
		EpisodeID:     episodeID,
		Task:          "Board this episode.",
		State: appproduction.StateFields{
			EpisodeID:             episodeID,
			ScriptVersionID:       scriptVersionID,
			DirectorPlanVersionID: planVersionID,
			ShotIDs:               shotIDs,
		},
	})
	w.assertStageReviewed(t, stage.StageRun, string(appproduction.StageStoryboardTable))
	boardVersionID := w.versionOfToolCall(t, stage, "storyboard.create_storyboard_version")
	if boardVersionID == "" {
		t.Fatal("clause 8: the board stage wrote no version")
	}
	if reviewed := w.reviewProduction(t, stage.StageRun, boardVersionID); !reviewed.Report.Passed {
		t.Fatalf("clause 8: the board review failed: %s", reviewed.Report.Summary)
	}
	w.passThrough(t, w.production, stage.StageRun, boardVersionID, "the board is what we want to shoot.")
	w.assertApproved(t, "storyboard_versions", boardVersionID)
	return boardVersionID
}

// runPanelImageChain is the chain this walk exists for: one image asset and one panel version per
// row, the batch, the collection, and one approval per row.
//
// It returns the approved images keyed by storyboard ITEM id, which is the key the traceability read
// joins on.
//
// Each step is a requirement the backend states rather than a choice this test made:
//
//   - `CollectBatchResults` takes `assetByItem` — the caller's map of row to asset — because asset
//     identity is the user's to decide, not the backend's to invent.
//   - §9.5 approves a PANEL, so a panel version must exist; `CreatePanelVersion` is the command, and
//     this walk is its first production caller.
//   - The approval's candidate list must CONTAIN the approved image, which the domain checks, so the
//     whole gallery travels with each call.
func (w *e2e002Walk) runPanelImageChain(t *testing.T, episodeID, boardVersionID string) map[string]string {
	t.Helper()
	ctx := context.Background()

	items, err := w.storyboard.ListStoryboardItems(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("clause 8: listing the board's rows: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("clause 8: the approved board has no rows")
	}

	// One asset per row, named from the ordinal so a second run reuses it rather than forking the
	// row's versions into a parallel asset.
	assetByItem := map[string]string{}
	for _, item := range items {
		record, _, err := w.assets.CreateAsset(ctx, appassets.CreateAssetRequest{
			ProjectID: w.ids.project,
			Type:      asset.TypeImage,
			Name:      "分镜图 #" + itoaE2E002(item.Ordinal),
		})
		if err != nil {
			t.Fatalf("clause 8: creating the image asset for row %d: %v", item.Ordinal, err)
		}
		assetByItem[item.ID] = record.ID
	}

	// Two candidates per row: enough to prove the candidate list is a LIST, and few enough to keep
	// the walk fast.
	const perShotCandidates = 2
	batch, err := w.production.RunImageBatch(ctx, appproduction.RunImageBatchRequest{
		StoryboardVersionID: boardVersionID,
		EpisodeID:           episodeID,
		ProjectID:           w.ids.project,
		PerShotCandidates:   perShotCandidates,
		ProviderID:          "e2e002-image-provider",
		ModelName:           "e2e002-image-model",
	})
	if err != nil {
		t.Fatalf("clause 8: submitting the image batch: %v", err)
	}
	jobIDs := make([]string, 0, len(batch.Submitted))
	for _, submission := range batch.Submitted {
		jobIDs = append(jobIDs, submission.JobID)
	}
	if len(jobIDs) != len(items)*perShotCandidates {
		t.Fatalf("clause 8: the batch submitted %d jobs for %d rows at %d candidates each",
			len(jobIDs), len(items), perShotCandidates)
	}

	// The scheduler, which the canary stack does not run: a submitted job sits at `pending` until a
	// worker claims it, and a collection over unclaimed jobs has nothing to collect.
	w.jobs.Start(ctx, 2, 20*time.Millisecond)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = w.jobs.Stop(stopCtx)
	})
	w.waitForBatchJobs(t, jobIDs, 30*time.Second)

	collected, err := w.production.CollectBatchResults(ctx, appproduction.CollectBatchResultsRequest{
		AssetByItem: assetByItem,
		JobIDs:      jobIDs,
		UsageRole:   "image",
	})
	if err != nil {
		t.Fatalf("clause 8: collecting the batch: %v", err)
	}
	fresh := 0
	for _, candidate := range collected {
		if candidate.VersionID != "" {
			fresh++
		}
	}
	if fresh != len(jobIDs) {
		t.Fatalf("clause 8: %d of %d settled jobs produced a candidate version", fresh, len(jobIDs))
	}

	// One panel version per row, then one approval each, over the candidates the collection wrote.
	approved := map[string]string{}
	for _, item := range items {
		panel, err := w.storyboard.CreatePanelVersion(ctx, appstoryboard.CreatePanelVersionRequest{
			StoryboardItemID: item.ID,
			VisualPrompt:     item.VisualDescription,
			ChangeReason:     "AC-E2E-002 panel image batch",
			CreatedByType:    versioning.CreatedByUser,
		})
		if err != nil {
			t.Fatalf("clause 8: creating the panel version for row %d: %v", item.Ordinal, err)
		}
		versions, err := w.assets.ListVersions(ctx, assetByItem[item.ID])
		if err != nil {
			t.Fatalf("clause 8: reading row %d's candidate versions: %v", item.Ordinal, err)
		}
		candidates := make([]string, 0, len(versions))
		for _, version := range versions {
			if version.Status == asset.VersionCandidate {
				candidates = append(candidates, version.ID)
			}
		}
		if len(candidates) == 0 {
			t.Fatalf("clause 8: row %d has no candidate versions, so there is nothing to approve", item.Ordinal)
		}
		current, err := w.storyboard.GetStoryboardItem(ctx, item.ID)
		if err != nil {
			t.Fatalf("clause 8: reading row %d before the approval: %v", item.Ordinal, err)
		}
		if _, err := w.storyboard.ApprovePanelImage(ctx, appstoryboard.ApprovePanelImageRequest{
			PanelVersionID:              panel.ID,
			ApprovedImageAssetVersionID: candidates[0],
			CandidateVersionIDs:         candidates,
			ExpectedRevision:            current.Revision,
		}); err != nil {
			t.Fatalf("clause 8: approving row %d's image: %v", item.Ordinal, err)
		}
		approved[item.ID] = candidates[0]
	}
	return approved
}

// waitForBatchJobs polls until every named job has settled.
//
// Polling rather than a fixed sleep: a sleep long enough for the slowest machine slows every
// machine, and one short enough to be fast is flaky. The deadline is the assertion, so a job that
// never settles fails with a number rather than hanging.
func (w *e2e002Walk) waitForBatchJobs(t *testing.T, jobIDs []string, within time.Duration) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(within)
	for {
		settled := 0
		for _, id := range jobIDs {
			var status string
			if err := w.db.QueryRowContext(ctx,
				`SELECT status FROM generation_jobs WHERE id = ?`, id).Scan(&status); err != nil {
				t.Fatalf("clause 8: reading job %s: %v", id, err)
			}
			switch status {
			case "succeeded", "failed", "cancelled", "orphaned", "remote_only":
				settled++
			}
		}
		if settled == len(jobIDs) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("clause 8: %d of %d batch jobs settled within %s", settled, len(jobIDs), within)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// e2e002AdapterSource answers every image submission with the deterministic mock.
//
// It exists because `provider_configs.kind`'s CHECK does not admit `mock_image`: the guardrail that
// stops a real configuration selecting a mock is worth more than a fixture's convenience, so the
// adapter is supplied through the runner's own port — the same reasoning and the same shape as the
// root package's `batchAdapterSource`.
type e2e002AdapterSource struct{ images *infraproviders.MockImageAdapter }

func (s e2e002AdapterSource) ImagePortFor(context.Context, string) (appjobs.ImagePort, error) {
	if s.images == nil {
		return nil, provider.NewUnsupportedError()
	}
	return s.images, nil
}

// Video and audio report unsupported, which is the honest answer for a walk whose subject is an
// IMAGE batch: a video job reaching this source is a job the walk did not mean to submit, and a mock
// answer would hide that.
func (e2e002AdapterSource) VideoPortFor(context.Context, string) (appjobs.VideoPort, error) {
	return nil, provider.NewUnsupportedError()
}

func (e2e002AdapterSource) AudioPortFor(context.Context, string) (appjobs.AudioPort, error) {
	return nil, provider.NewUnsupportedError()
}

// assertTraceable is the criterion's closing clause, asserted against the read the EXPORT makes.
//
// 「所有结果可追溯到输入、模型、任务和版本」 has a mechanical form and this is it: the timeline —
// which `timeline.go` and `final_reader.go` are both built from, and which the MP4 export composes
// from — must resolve every shot's media to the version that was approved, and that version must
// name the JOB that produced it. A walk that read the panel table instead would be reading the
// record rather than the join, and the defect this walk found lived precisely in that gap: the
// panel held the image while the join could not see it.
func (w *e2e002Walk) assertTraceable(t *testing.T, episodeID string, approved map[string]string) {
	t.Helper()
	ctx := context.Background()
	timeline, err := w.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: episodeID})
	if err != nil {
		t.Fatalf("clause 9: reading the timeline: %v", err)
	}
	if len(timeline.Shots) != len(approved) {
		t.Fatalf("clause 9: the timeline reports %d shots and %d rows were approved", len(timeline.Shots), len(approved))
	}
	if timeline.MissingMedia != 0 {
		t.Fatalf("clause 9: the timeline reports %d shots with no media, and every shot was approved", timeline.MissingMedia)
	}
	for _, shot := range timeline.Shots {
		want, ok := approved[shot.ItemID]
		if !ok {
			t.Fatalf("clause 9: the timeline carries a shot at row %s that this walk never approved", shot.ItemID)
		}
		if shot.MediaVersionID != want {
			t.Fatalf("clause 9: the row's media resolves to %q and %q was approved, so the export would compose a different frame than the one put in force",
				shot.MediaVersionID, want)
		}
		// The version names the JOB that produced it (AC-ASSET-002's lineage fact), which is what
		// makes 「可追溯到…任务和版本」 mechanical rather than a claim.
		var generationJobID string
		if err := w.db.QueryRowContext(ctx,
			`SELECT COALESCE(generation_job_id, '') FROM asset_versions WHERE id = ?`, want).Scan(&generationJobID); err != nil {
			t.Fatalf("clause 9: reading the version's job: %v", err)
		}
		if generationJobID == "" {
			t.Fatalf("clause 9: the approved version %s names no generation job, so the frame cannot be traced to the task that produced it", want)
		}
		var jobRows int
		if err := w.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM generation_jobs WHERE id = ?`, generationJobID).Scan(&jobRows); err != nil {
			t.Fatalf("clause 9: resolving the job: %v", err)
		}
		if jobRows != 1 {
			t.Fatalf("clause 9: the version names job %s and no such job exists", generationJobID)
		}
		// And the version itself exists, which closes 「…和版本」.
		var versionRows int
		if err := w.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM asset_versions WHERE id = ?`, want).Scan(&versionRows); err != nil {
			t.Fatalf("clause 9: resolving the version: %v", err)
		}
		if versionRows != 1 {
			t.Fatalf("clause 9: the timeline cites version %s and no such version exists", want)
		}
	}
	t.Logf("clause 9: %d shots resolve to an approved version and the job that produced it", len(timeline.Shots))
}

// e2e002DocumentStore is the import service's document port over the real file store.
//
// The importer writes two objects per document — the ORIGINAL bytes and the normalized text — and
// both go through `filestore`, so the fixture is content-addressed and committed the way a desktop
// build commits it. The alternative, a map in the test, would make the import's bytes a fact this
// walk alone understood.
//
// # Why this is written here rather than imported
//
// A production adapter for this port exists (`desktop.NewDocumentStore`), and it CANNOT be used
// from this package: `internal/desktop` imports this one, so the dependency in that direction would
// be a cycle. The repository records the same constraint for the same reason in
// `scale_records_wp12_test.go`, where a page-size constant is mirrored rather than read for exactly
// this. What is mirrored here is the two-step sequence — put the bytes, then record the metadata
// row that makes them addressable — and both steps are the production implementations'
// (`filestore.Store.Put` and `FileRepository.UpsertObject`), so no behaviour is re-created, only
// sequenced.
type e2e002DocumentStore struct {
	store *filestore.Store
	files *FileRepository
}

func (s e2e002DocumentStore) Import(ctx context.Context, displayName string, body []byte) (appfiles.Object, error) {
	object, err := s.store.Put(ctx, displayName, bytes.NewReader(body))
	if err != nil {
		return appfiles.Object{}, err
	}
	// The metadata row is what makes the object reachable by key, and a version whose bytes could
	// not be read back would be an import that reported success and stored nothing.
	if err := s.files.UpsertObject(ctx, object); err != nil {
		return appfiles.Object{}, err
	}
	return object, nil
}

func (s e2e002DocumentStore) Open(ctx context.Context, storageKey string) ([]byte, error) {
	reader, err := s.store.Open(ctx, storageKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(reader)
}

// e2e002JobIDs prefixes the identifiers the job service mints, so a job from this walk is
// recognisable in a failure message.
type e2e002JobIDs struct{ inner *id.Generator }

func (g e2e002JobIDs) NewID(prefix string) string {
	value, err := g.inner.New()
	if err != nil {
		// The only failure is entropy, and an identifier with zeroed randomness would be a collision
		// rather than a degraded id.
		panic(err)
	}
	return prefix + "-" + value
}

// runProductionStage runs one stage through the PRODUCTION pipeline.
//
// The script canary's `runScriptStage` drives the SCRIPT pipeline and this walk needs both: the two
// pipelines serve different stages (`AgentsFor` answers for its own set and refuses the rest), so
// which one a stage goes through is the layer's decision rather than the caller's.
func (w *e2e002Walk) runProductionStage(t *testing.T, request stagepipeline.StageRequest) stagepipeline.StageResult {
	t.Helper()
	result, err := w.production.RunStage(context.Background(), request)
	if err != nil {
		t.Fatalf("running the %s stage: %v", request.Stage, err)
	}
	return result
}

// reviewProduction supervises a production stage's attempt.
//
// It switches the mock to its normal scenario because a SUPERVISOR may not write (AC-AGENT-001):
// the tool-call scenario answers an execution agent's request for a write, and a supervisor
// receiving one would be refused rather than reviewed.
func (w *e2e002Walk) reviewProduction(t *testing.T, stage workflow.StageRun, artifactVersionID string) stagepipeline.SupervisionResult {
	t.Helper()
	w.mock.SetScenario(infraproviders.MockScenarioNormal)
	defer w.mock.SetScenario(infraproviders.MockScenarioToolCall)
	result, err := w.production.RunSupervision(context.Background(), stagepipeline.SupervisionRequest{
		StageRunID:        stage.ID,
		ProjectID:         w.ids.project,
		EpisodeID:         w.ids.episode,
		ArtifactVersionID: artifactVersionID,
	})
	if err != nil {
		t.Fatalf("supervising %s: %v", stage.Stage, err)
	}
	stored, issues, err := w.workflow.GetReviewReport(context.Background(), stage.ID)
	if err != nil {
		t.Fatalf("reading the stored report: %v", err)
	}
	if stored.ID != result.Report.ID {
		t.Fatalf("the stored report is %q, want %q", stored.ID, result.Report.ID)
	}
	if len(issues) != len(result.Issues) {
		t.Fatalf("the stored report has %d findings and %d were recorded", len(issues), len(result.Issues))
	}
	result.Report = stored
	return result
}

// productionReader gives the walk the production canary's two tool-output readers.
//
// `versionOfToolCall` and `reportIDOf` live on `productionCanary`, and this walk embeds the SCRIPT
// canary. Copying their bodies would put the shape of a write tool's output in two places — and the
// shape is the tool table's, not either test's — so the walk composes the production canary over its
// own base and asks it. One reader, one place to change.
func (w *e2e002Walk) productionReader() *productionCanary {
	return &productionCanary{canary: w.canary, pipeline: w.production}
}

// versionOfToolCall reads the version a stage's write tool reported.
func (w *e2e002Walk) versionOfToolCall(t *testing.T, result stagepipeline.StageResult, toolKey string) string {
	t.Helper()
	return w.productionReader().versionOfToolCall(t, result, toolKey)
}

// reportIDOf reads the gap report a stage's write tool reported.
//
// The gap stage's artefact is a REPORT rather than a version of one of the four families, so its id
// travels in the tool call's own output rather than in the run's artifact list.
func (w *e2e002Walk) reportIDOf(t *testing.T, result stagepipeline.StageResult) string {
	t.Helper()
	return w.productionReader().reportIDOf(t, result)
}

// EffectPortFor reports unsupported, on the same reasoning the other two
// refusals carry: an effect job reaching this image-batch walk is a job the
// walk did not mean to submit.
func (e2e002AdapterSource) EffectPortFor(context.Context, string) (appjobs.EffectPort, error) {
	return nil, provider.NewUnsupportedError()
}
