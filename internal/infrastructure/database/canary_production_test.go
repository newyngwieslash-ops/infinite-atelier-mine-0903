package database

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appproduction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// canary_production_test.go drives WP-09's production stages to an APPROVED STORYBOARD.
//
// It is the ROADMAP's scope item 14, and the chain is FR-050/060/070's order:
//
//	director plan → supervision → PASS →
//	asset gap analysis (NO supervision, section 10.1's own setting) → user PASS →
//	storyboard table (one row per shot, citing the script's shots) → supervision → PASS
//
// Everything but the model is production code, for the reason the script canary records:
// section 18.3 forbids CI calling a paid provider. The MOCK's production branches are what
// make the chain possible at all — `providers/mock_production.go` implements them — and a
// generic tool-call reply would ask for the wrong write at every stage.

// productionCanary is the canary plus the production pipeline under test.
type productionCanary struct {
	*canary
	pipeline *appproduction.Service
	gaps     *appassets.GapService
}

// newProductionCanary composes the production pipeline over the canary's stack.
func newProductionCanary(t *testing.T) *productionCanary {
	t.Helper()
	base := newCanary(t)
	gapService := appassets.NewGapService(appassets.GapOptions{
		Gaps: NewAssetRepository(base.db), Clock: canaryClock{}, IDs: newTestIDGenerator(),
	})
	pipeline := appproduction.New(appproduction.Options{
		Engine: base.engine, Runtime: base.runtime, Workflow: base.workflow,
		Assembly: base.assembly, Runs: base.repo,
		Storyboard: base.storyboard, Assets: base.assets, Gaps: gapService,
	})
	return &productionCanary{canary: base, pipeline: pipeline, gaps: gapService}
}

// TestCanaryProductionStagesToAnApprovedBoard is the acceptance walk for the agent half of
// the production pipeline.
//
// The batch and the panel approval are NOT here: they are `batch_wiring_test.go`'s
// subject, which drives them over the composed stack. Both walks are needed, because a
// stage that writes a board with no rows and a batch that images an empty board are
// different failures with the same symptom.
func TestCanaryProductionStagesToAnApprovedBoard(t *testing.T) {
	canary := newProductionCanary(t)
	canary.mock.SetScenario(infraproviders.MockScenarioToolCall)

	// The script the board is built from, written through the SERVICE so the rows the
	// board's citations are checked against are the ones the production path makes.
	scriptVersionID, shotIDs := canary.seedScriptWithShots(t, 3)

	// --- The director plan (FR-060) ---
	plan := canary.runProductionStage(t, stagepipeline.StageRequest{
		WorkflowRunID: canary.ids.workflowRun,
		Stage:         appproduction.StageDirectorPlan,
		ProjectID:     canary.ids.project,
		EpisodeID:     canary.ids.episode,
		Task:          "Plan how this episode is shot.",
		State: appproduction.StateFields{
			EpisodeID: canary.ids.episode, ScriptVersionID: scriptVersionID,
		},
	})
	canary.canary.assertStageReviewed(t, plan.StageRun, "director_plan")
	planVersionID := canary.versionOfToolCall(t, plan, "storyboard.create_director_plan_version")
	if planVersionID == "" {
		t.Fatal("the plan stage wrote no version")
	}
	if reviewed := canary.reviewProduction(t, plan.StageRun, planVersionID); reviewed.Report.Passed != true {
		t.Fatalf("the plan review failed: %s", reviewed.Report.Summary)
	}
	canary.canary.passThrough(t, canary.pipeline, plan.StageRun, planVersionID, "the plan is what we want.")
	// The version is APPROVED, read back from the table: a gate that moved the stage
	// without approving the version would leave a passed stage and nothing to build from.
	if approved := canary.approvedRow(t, "director_plan_versions", planVersionID); !approved {
		t.Fatal("the plan stage passed with no approved version")
	}

	// --- The asset gap analysis (FR-050) ---
	//
	// Its stage policy has NO SUPERVISION — section 10.1's own setting — so the attempt
	// goes straight to the user's gate. That is asserted rather than skipped: a stage that
	// ran a review its policy does not require would spend the user's provider budget on a
	// run nothing asked for.
	gap := canary.runProductionStage(t, stagepipeline.StageRequest{
		WorkflowRunID: canary.ids.workflowRun,
		Stage:         appproduction.StageAssetGapAnalysis,
		ProjectID:     canary.ids.project,
		EpisodeID:     canary.ids.episode,
		Task:          "Say which story facts this episode needs assets for.",
		State: appproduction.StateFields{
			EpisodeID: canary.ids.episode, ScriptVersionID: scriptVersionID,
		},
	})
	if gap.StageRun.Status != workflow.StageWaitingUser {
		t.Fatalf("the analysis stage is at %q, want waiting_user without a review", gap.StageRun.Status)
	}
	reportID := canary.reportIDOf(t, gap)
	if reportID == "" {
		t.Fatal("the analysis wrote no report")
	}
	// The report is a real row with real lines, and its content is what lets the user
	// approve it: the mock writes a SATISFIED line, and a report with a missing required
	// asset could not be approved at all.
	report, items, err := canary.gaps.GetGapReport(context.Background(), reportID)
	if err != nil {
		t.Fatalf("reading the report the stage wrote: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("the report the stage wrote has no lines")
	}
	if report.Status != versioning.StatusDraft {
		t.Fatalf("the report is %q, want draft: a write tool cannot approve", report.Status)
	}
	// THE GATE IS THE APPROVAL. `passThrough` sends the decision AND names the version,
	// and the layer delegates to the gap service's own approval — so calling the service
	// directly here as well would approve the report twice, which the second call refuses.
	// The assertion below is therefore on the RESULT of the gate rather than on a separate
	// command: a report the gate approved is the one in force.
	canary.canary.passThrough(t, canary.pipeline, gap.StageRun, reportID, "the analysis is right.")
	approved, found, err := canary.gaps.CurrentApprovedGapReport(context.Background(), canary.ids.episode)
	if err != nil || !found {
		t.Fatalf("the gate passed the analysis with nothing in force: found=%v err=%v", found, err)
	}
	if approved.ID != reportID {
		t.Fatalf("the report in force is %q, want the one the stage wrote (%q)", approved.ID, reportID)
	}

	// --- The storyboard table (FR-070) ---
	board := canary.runProductionStage(t, stagepipeline.StageRequest{
		WorkflowRunID: canary.ids.workflowRun,
		Stage:         appproduction.StageStoryboardTable,
		ProjectID:     canary.ids.project,
		EpisodeID:     canary.ids.episode,
		Task:          "Board every shot of this episode.",
		State: appproduction.StateFields{
			EpisodeID: canary.ids.episode, ScriptVersionID: scriptVersionID,
			DirectorPlanVersionID: planVersionID, ShotIDs: shotIDs,
		},
	})
	canary.canary.assertStageReviewed(t, board.StageRun, "storyboard_table")
	boardVersionID := canary.versionOfToolCall(t, board, "storyboard.create_storyboard_version")
	if boardVersionID == "" {
		t.Fatal("the board stage wrote no version")
	}
	// THE ROWS EXIST, one per shot, and each cites a real shot of the script. This is what
	// FR-070's "每个镜头至少包含" turns on and what the stage's old tool list could not
	// produce: it had no read that listed the shots, so a model could only guess at ids.
	boardItems, err := canary.storyboard.ListStoryboardItems(context.Background(), boardVersionID)
	if err != nil {
		t.Fatalf("reading the board's rows: %v", err)
	}
	if len(boardItems) != len(shotIDs) {
		t.Fatalf("the board has %d rows for %d shots", len(boardItems), len(shotIDs))
	}
	for index, item := range boardItems {
		if item.ShotID != shotIDs[index] {
			t.Errorf("row %d cites shot %q, want %q", index, item.ShotID, shotIDs[index])
		}
		if item.Ordinal != index+1 {
			t.Errorf("row %d has ordinal %d", index, item.Ordinal)
		}
		// FR-070's fields are the row's, and the three migration 000018 added are the
		// ones that had nowhere to live before WP-09.
		if item.FirstFrameDescription == "" || item.LastFrameDescription == "" || item.VideoMotionDescription == "" {
			t.Errorf("row %d is missing FR-070's frame or motion descriptions: %+v", index, item)
		}
	}
	if reviewed := canary.reviewProduction(t, board.StageRun, boardVersionID); !reviewed.Report.Passed {
		t.Fatalf("the board review failed: %s", reviewed.Report.Summary)
	}
	canary.canary.passThrough(t, canary.pipeline, board.StageRun, boardVersionID, "the board is what we want to shoot.")
	if approved := canary.approvedRow(t, "storyboard_versions", boardVersionID); !approved {
		t.Fatal("the board stage passed with no approved version")
	}
}

// runProductionStage runs one stage through the pipeline.
func (c *productionCanary) runProductionStage(t *testing.T, request stagepipeline.StageRequest) stagepipeline.StageResult {
	t.Helper()
	result, err := c.pipeline.RunStage(context.Background(), request)
	if err != nil {
		t.Fatalf("running the %s stage: %v", request.Stage, err)
	}
	return result
}

// reviewProduction supervises a production stage's attempt.
//
// It is a copy of the script canary's `review` with this pipeline in place of that one,
// and the duplication is the two pipelines' rather than the test's: each canary drives the
// pipeline it composes, and a shared helper taking an interface would hide which one a
// given walk exercises.
func (c *productionCanary) reviewProduction(t *testing.T, stage workflow.StageRun, artifactVersionID string) stagepipeline.SupervisionResult {
	t.Helper()
	c.mock.SetScenario(infraproviders.MockScenarioNormal)
	defer c.mock.SetScenario(infraproviders.MockScenarioToolCall)
	result, err := c.pipeline.RunSupervision(context.Background(), stagepipeline.SupervisionRequest{
		StageRunID:        stage.ID,
		ProjectID:         c.ids.project,
		EpisodeID:         c.ids.episode,
		ArtifactVersionID: artifactVersionID,
	})
	if err != nil {
		t.Fatalf("supervising %s: %v", stage.Stage, err)
	}
	if strings.TrimSpace(result.Report.RulesetVersion) == "" {
		t.Fatal("the stored review report names no ruleset version")
	}
	stored, issues, err := c.workflow.GetReviewReport(context.Background(), stage.ID)
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

// approvedRow reports whether a version row is the approved one for its parent.
func (c *productionCanary) approvedRow(t *testing.T, table, versionID string) bool {
	t.Helper()
	// The table name is a literal from this file's own call sites rather than a caller's
	// string, which is what makes it safe to interpolate: a parameterised table name is
	// not a thing SQL supports, and the alternative — a query per table — would be three
	// copies of one question.
	query := ""
	switch table {
	case "director_plan_versions", "storyboard_versions", "asset_gap_reports":
		query = `SELECT COUNT(*) FROM ` + table + ` WHERE id = ? AND status = 'approved'`
	default:
		t.Fatalf("no approval query is defined for %q", table)
	}
	var count int
	if err := c.db.QueryRowContext(context.Background(), query, versionID).Scan(&count); err != nil {
		t.Fatalf("reading the approval of %s: %v", versionID, err)
	}
	return count == 1
}

// versionOfToolCall reads the row a stage's write tool reported.
//
// It walks the recorded TOOL CALLS rather than the model's answer, which is §AC-AGENT-003's
// rule: an answer is a claim about what was produced, and a call's result is the row the
// write returned.
func (c *productionCanary) versionOfToolCall(t *testing.T, result stagepipeline.StageResult, toolKey string) string {
	t.Helper()
	for _, call := range result.Outcome.ToolCalls {
		if call.ToolKey != toolKey {
			continue
		}
		var document struct {
			Artifacts []struct {
				EntityID string `json:"entityId"`
			} `json:"artifacts"`
		}
		if err := json.Unmarshal([]byte(call.OutputJSON), &document); err == nil {
			for _, artifact := range document.Artifacts {
				if id := strings.TrimSpace(artifact.EntityID); id != "" {
					return id
				}
			}
		}
	}
	return ""
}

// reportIDOf reads the gap report id out of the analysis stage's tool call.
//
// It is separate from `versionOfToolCall` because the report tool's result carries the id
// as `reportId` rather than in an artifact list: a write tool's result is its own contract,
// and this reads the one that tool actually returns rather than a shape invented for it.
func (c *productionCanary) reportIDOf(t *testing.T, result stagepipeline.StageResult) string {
	t.Helper()
	for _, call := range result.Outcome.ToolCalls {
		if call.ToolKey != "asset.create_gap_report" {
			continue
		}
		var document struct {
			ReportID string `json:"reportId"`
		}
		if err := json.Unmarshal([]byte(call.OutputJSON), &document); err != nil {
			continue
		}
		if id := strings.TrimSpace(document.ReportID); id != "" {
			return id
		}
	}
	return ""
}

// seedScriptWithShots writes a script version with N shots, and returns its id and the
// shots' ids so the board's citations can be checked against rows that exist.
func (c *productionCanary) seedScriptWithShots(t *testing.T, shots int) (string, []string) {
	t.Helper()
	ctx := context.Background()
	scriptRecord, err := c.script.EnsureScript(ctx, c.ids.episode)
	if err != nil {
		t.Fatalf("ensuring the script: %v", err)
	}
	skeleton, err := c.script.CreateStorySkeletonVersion(ctx, appscript.CreateStorySkeletonVersionRequest{
		EpisodeID: c.ids.episode, OpeningHook: "the canary's hook", EndingHook: "the canary's ending",
		CreatedByType: versioning.CreatedByAgent, CreatedByID: "canary",
	})
	if err != nil {
		t.Fatalf("creating the skeleton: %v", err)
	}
	strategy, err := c.script.CreateAdaptationStrategyVersion(ctx, appscript.CreateAdaptationStrategyVersionRequest{
		EpisodeID: c.ids.episode, StrategySummary: "the canary's strategy",
		CreatedByType: versioning.CreatedByAgent, CreatedByID: "canary",
	})
	if err != nil {
		t.Fatalf("creating the strategy: %v", err)
	}
	version, err := c.script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
		ScriptID: scriptRecord.ID, StorySkeletonVersionID: skeleton.ID,
		AdaptationStrategyVersionID: strategy.ID, CreatedByType: versioning.CreatedByAgent,
		CreatedByID: "canary",
	})
	if err != nil {
		t.Fatalf("creating the script version: %v", err)
	}
	structure := script.ScriptStructure{ScriptVersionID: version.ID}
	shotIDs := make([]string, 0, shots)
	for index := 1; index <= shots; index++ {
		number := canaryInt(index)
		scene := script.Scene{
			ID: "canary-scene-" + number, ScriptVersionID: version.ID, Ordinal: index,
			SceneNumber: number, Slugline: "INT. scene " + number + " - day",
			InteriorExterior: script.InteriorINT,
			CreatedAt:        canaryClockTime(), UpdatedAt: canaryClockTime(), Revision: 1,
		}
		shotID := "canary-shot-" + number
		shotIDs = append(shotIDs, shotID)
		structure.Scenes = append(structure.Scenes, script.SceneStructure{
			Scene: scene,
			Shots: []script.Shot{{
				ID: shotID, SceneID: scene.ID, Ordinal: 1, ShotNumber: number,
				VisualDescription: "the canary's shot " + number,
				Status:            versioning.StatusDraft,
				CreatedAt:         canaryClockTime(), UpdatedAt: canaryClockTime(), Revision: 1,
			}},
		})
	}
	if _, err := c.script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
		ScriptID: scriptRecord.ID, ScriptVersionID: version.ID, Structure: structure,
	}); err != nil {
		t.Fatalf("writing the structure: %v", err)
	}
	return version.ID, shotIDs
}

// canaryInt renders a small positive integer, since this file does not import strconv for
// one call site.
func canaryInt(value int) string {
	if value <= 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// canaryClockTime is the canary's fixed timestamp.
func canaryClockTime() time.Time {
	return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
}
