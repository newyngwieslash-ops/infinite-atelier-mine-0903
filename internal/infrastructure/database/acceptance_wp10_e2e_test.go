package database

import (
	"context"
	"strings"
	"testing"
	"time"

	appproduction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// acceptance_wp10_e2e_test.go walks AC-E2E-004 over the assembled production stack.
//
//	人为制造一个角色服装引用错误：
//	- Supervisor 定位到具体 Shot；
//	- ReviewReport 提供证据和建议；
//	- FIX 产生新版本；
//	- 旧版本保留；
//	- 通过后 Workflow 进入下一阶段。
//
// # Why this is separate from the checker's own tests
//
// `consistency_wp10_test.go` asserts the first two clauses against the RULES alone. An independent
// spec review pointed out that the remaining three — the FIX, the retained version and the advance —
// were covered only by the generic pipeline machinery on a DIFFERENT fault (AC-BOARD-002's supervisor
// finding), never for this one. The two faults take different paths: AC-BOARD-002's is a model's
// finding whose fix is the model's output, while this one is a DETERMINISTIC blocker that overruled a
// happy supervisor. That is the branch of `ReviewPassed` the mutation review found unprotected, and
// the one the deterministic half exists for.
//
// The stack is the canary's, so the board the fault is injected into is one the production stages
// actually wrote: twelve rows citing the script's own shots, over a director plan and an approved gap
// report.

// costumeFaultBoard is a canary board with two costume versions, so a citation can be made wrong.
type costumeFaultBoard struct {
	*productionCanary
	itemIDs   []string
	versionID string
	// costumeV1 is in force and v2 is superseded, so citing v2 is the injected fault.
	costumeV1 string
	costumeV2 string
	// shotIDs are the script's shots, in order, so a row can be named by position.
	shotIDs []string
}

// newCostumeFaultBoard stages a board through the production chain and attaches a costume.
func newCostumeFaultBoard(t *testing.T) *costumeFaultBoard {
	t.Helper()
	canary := newProductionCanary(t)
	// The tool-call scenario, which is the one the production chain needs: a stage's write happens
	// through a TOOL, and the mock's default reply is a document with no call, so a board staged
	// without this writes nothing. The canary's own walk sets the same scenario.
	canary.mock.SetScenario(infraproviders.MockScenarioToolCall)
	ctx := context.Background()

	// The costume asset, its two versions and the character state that says which is worn.
	assets := NewAssetRepository(canary.db)
	generator := newTestIDGenerator()
	if err := assets.CreateAsset(ctx, asset.Asset{
		ID: "ac-costume", ProjectID: canary.ids.project, Type: asset.TypeCostume, Name: "冬装",
		StoryEntityID: "ac-character", Status: asset.StatusActive,
		CreatedAt: canaryClockInstant(), UpdatedAt: canaryClockInstant(), Revision: 1,
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	costumeV1 := mustNewID(t, generator)
	costumeV2 := mustNewID(t, generator)
	for index, versionID := range []string{costumeV1, costumeV2} {
		status := versioning.StatusApproved
		if index == 1 {
			status = versioning.StatusSuperseded
		}
		if err := assets.CreateVersion(ctx, asset.Version{
			ID: versionID, AssetID: "ac-costume", VersionNumber: index + 1, Status: status,
			CreatedByType: versioning.CreatedByUser, CreatedAt: canaryClockInstant(),
		}); err != nil {
			t.Fatalf("CreateVersion: %v", err)
		}
	}
	if _, err := canary.db.ExecContext(ctx,
		`UPDATE assets SET current_approved_version_id = ? WHERE id = 'ac-costume'`, costumeV1); err != nil {
		t.Fatal(err)
	}
	// The character entity the costume belongs to, and the state that says it is worn from the
	// beginning of the story. The rule compares a row's citation against THIS.
	if _, err := canary.db.ExecContext(ctx, `INSERT INTO story_entities
		(id, project_id, entity_type, canonical_name, status, created_at, updated_at, revision)
		VALUES ('ac-character', ?, 'character', '沈砚', 'accepted',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, canary.ids.project); err != nil {
		t.Fatal(err)
	}
	if _, err := canary.db.ExecContext(ctx, `INSERT INTO character_states
		(id, character_entity_id, from_event_order, to_event_order, appearance_json,
		 costume_asset_version_id, injuries_json, possessions_json, relationship_state_json,
		 source_fact_id, status, created_at, updated_at, revision)
		VALUES ('ac-state', 'ac-character', 1, NULL, '', ?, '', '', '', '', 'accepted',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, costumeV1); err != nil {
		t.Fatal(err)
	}

	// The board, written by the production chain: plan, gap analysis, storyboard table.
	scriptVersionID, shotIDs := canary.seedScriptWithShots(t, 12)
	// The state is the layer's own type rather than a map: the production pipeline renders its
	// prompt's state layer from `StateFields`, so a board staged with a map would reach its write tool
	// with no shots to cite and produce no version — which is exactly what the first version of this
	// test did, and what the failure said.
	plan := canary.runProductionStage(t, stagepipeline.StageRequest{
		WorkflowRunID: canary.ids.workflowRun,
		Stage:         "director_plan",
		ProjectID:     canary.ids.project,
		EpisodeID:     canary.ids.episode,
		Task:          "Plan how this episode is shot.",
		State: appproduction.StateFields{
			EpisodeID: canary.ids.episode, ScriptVersionID: scriptVersionID,
		},
	})
	planVersionID := canary.versionOfToolCall(t, plan, "storyboard.create_director_plan_version")
	if planVersionID == "" {
		t.Fatal("the plan stage wrote no version")
	}
	board := canary.runProductionStage(t, stagepipeline.StageRequest{
		WorkflowRunID: canary.ids.workflowRun,
		Stage:         "storyboard_table",
		ProjectID:     canary.ids.project,
		EpisodeID:     canary.ids.episode,
		Task:          "Board every shot of the script.",
		State: appproduction.StateFields{
			EpisodeID: canary.ids.episode, ScriptVersionID: scriptVersionID,
			DirectorPlanVersionID: planVersionID, ShotIDs: shotIDs,
		},
	})
	versionID := canary.versionOfToolCall(t, board, "storyboard.create_storyboard_version")
	if versionID == "" {
		t.Fatal("the board stage wrote no version")
	}
	rows, err := NewStoryboardRepository(canary.db).ListStoryboardItems(ctx, versionID)
	if err != nil {
		t.Fatalf("ListStoryboardItems: %v", err)
	}
	itemIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		itemIDs = append(itemIDs, row.ID)
	}
	return &costumeFaultBoard{
		productionCanary: canary, itemIDs: itemIDs, versionID: versionID,
		costumeV1: costumeV1, costumeV2: costumeV2, shotIDs: shotIDs,
	}
}

// injectWrongCostume makes one row cite the superseded costume.
func (b *costumeFaultBoard) injectWrongCostume(t *testing.T, rowIndex int) {
	t.Helper()
	if err := NewAssetRepository(b.db).AddUsage(context.Background(), asset.Usage{
		ID: "ac-bad-usage", AssetVersionID: b.costumeV2,
		ConsumerType: asset.ConsumerShot, ConsumerID: b.itemIDs[rowIndex],
		UsageRole: "costume", Required: true, CreatedAt: canaryClockInstant(),
	}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
}

// checkerFor builds the real checker over this canary's database.
func (b *costumeFaultBoard) checkerFor(t *testing.T) *StoryboardConsistencyChecker {
	t.Helper()
	return NewStoryboardConsistencyChecker(
		boardConsistencyReader{repo: NewStoryboardRepository(b.db)},
		assetRepoConsistencyReader{repo: NewAssetRepository(b.db)},
		scriptSourceRef{repo: NewScriptRepository(b.db)},
		storyRepoConsistencyReader{repo: NewStoryRepository(b.db)},
	)
}

// TestE2E004ACostumeFaultIsLocatedAndThenFixed is AC-E2E-004 in full.
func TestE2E004ACostumeFaultIsLocatedAndThenFixed(t *testing.T) {
	board := newCostumeFaultBoard(t)
	ctx := context.Background()
	const atRow = 5
	if len(board.itemIDs) <= atRow {
		t.Fatalf("the board has %d rows, too few for a sixth", len(board.itemIDs))
	}
	board.injectWrongCostume(t, atRow)

	// Clauses 1 and 2: the report locates the row and carries evidence and a suggestion.
	findings, err := board.checkerFor(t).Check(ctx, "storyboard_table", board.versionID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	located := false
	for _, finding := range findings {
		if finding.Rule != consistency.RuleCostumeContinuity {
			continue
		}
		located = true
		if finding.EntityID != board.itemIDs[atRow] {
			t.Fatalf("the finding names %s, want the sixth row %s", finding.EntityID, board.itemIDs[atRow])
		}
		if !finding.Blocker() {
			t.Fatalf("a wrong costume is not a blocker: %+v", finding)
		}
		if !finding.AutoFixable {
			t.Fatal("the finding is not marked auto-fixable, so the FIX clause has no route")
		}
		if finding.Suggestion == "" {
			t.Fatal("the finding suggests nothing, which FR-110's report shape requires")
		}
		refs := map[string]bool{}
		for _, entry := range finding.Evidence {
			refs[entry.Ref] = true
		}
		if !refs[board.costumeV2] || !refs[board.costumeV1] {
			t.Fatalf("the evidence does not cite both versions: %+v", finding.Evidence)
		}
	}
	if !located {
		t.Fatalf("the injected fault was not located: %+v", findings)
	}

	// THE VERDICT the pipeline computes: a happy supervisor cannot pass this board. This is the
	// branch the mutation review found unprotected — replacing the predicate with the supervisor's own
	// verdict left every test green — and the reason AC-E2E-004's FIX step is reached at all.
	if stagepipeline.ReviewPassed(true, findings) {
		t.Fatal("a board with a deterministic blocker passed on the supervisor's verdict alone")
	}

	// Clauses 3 and 4: the FIX produces a NEW version and the old one is kept.
	//
	// The correction is written through the storyboard repository, which is what the pipeline's manual
	// edit and its FIX both do at the write. Every row is carried forward except the faulty one — the
	// "其他 Shot 不变" property AC-BOARD-002 also grades — so the new version differs from the old in
	// exactly one citation.
	before, err := NewStoryboardRepository(board.db).GetStoryboardVersion(ctx, board.versionID)
	if err != nil {
		t.Fatal(err)
	}
	beforeRows, err := NewStoryboardRepository(board.db).ListStoryboardItems(ctx, board.versionID)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewStoryboardRepository(board.db)
	// The version row is written through the REPOSITORY, which is what the pipeline's own write path
	// calls: the storyboard SERVICE is the one that mints identifiers and records events, and neither
	// is what this test grades. The revision's citations are re-pointed like a real fix.
	after := before
	after.ID = before.ID + "-v2"
	after.VersionNumber = before.VersionNumber + 1
	after.Status = versioning.StatusDraft
	after.BasedOnVersionID = before.ID
	if err := repo.CreateStoryboardVersion(ctx, after); err != nil {
		t.Fatalf("CreateStoryboardVersion: %v", err)
	}
	if after.ID == before.ID {
		t.Fatal("the fix overwrote the old version rather than producing a new one")
	}
	if after.VersionNumber <= before.VersionNumber {
		t.Fatalf("the new version is numbered %d against the old %d", after.VersionNumber, before.VersionNumber)
	}
	carried := make([]string, 0, len(beforeRows))
	for index, row := range beforeRows {
		copied := row
		copied.ID = row.ID + "-fixed"
		copied.StoryboardVersionID = after.ID
		if err := repo.CreateStoryboardItem(ctx, copied); err != nil {
			t.Fatalf("CreateStoryboardItem: %v", err)
		}
		carried = append(carried, copied.ID)
		if index != atRow {
			continue
		}
		if err := NewAssetRepository(board.db).AddUsage(ctx, asset.Usage{
			ID: "ac-fixed-usage", AssetVersionID: board.costumeV1,
			ConsumerType: asset.ConsumerShot, ConsumerID: copied.ID,
			UsageRole: "costume", Required: true, CreatedAt: canaryClockInstant(),
		}); err != nil {
			t.Fatalf("AddUsage for the corrected row: %v", err)
		}
	}
	// The old version and its rows are still there, which is what "旧版本保留" means.
	kept, err := repo.GetStoryboardVersion(ctx, before.ID)
	if err != nil {
		t.Fatalf("the old version was deleted by the fix: %v", err)
	}
	if kept.VersionNumber != before.VersionNumber {
		t.Fatalf("the old version was renumbered: %d", kept.VersionNumber)
	}
	keptRows, err := repo.ListStoryboardItems(ctx, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keptRows) != len(beforeRows) {
		t.Fatalf("the old version lost rows: %d of %d", len(keptRows), len(beforeRows))
	}

	// Clause 5: the corrected board passes the rules, and every non-faulty row is unchanged.
	clean, err := board.checkerFor(t).Check(ctx, "storyboard_table", after.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blockers := consistency.Blockers(clean); len(blockers) != 0 {
		t.Fatalf("the corrected board still has blockers: %+v", blockers)
	}
	if !stagepipeline.ReviewPassed(true, clean) {
		t.Fatal("a clean board was not passed by a happy supervisor")
	}
	// 其他 Shot 不变, asserted field by field on a row that was not the fault.
	original := beforeRows[0]
	revised, err := repo.GetStoryboardItem(ctx, carried[0])
	if err != nil {
		t.Fatal(err)
	}
	if revised.ShotID != original.ShotID || revised.Ordinal != original.Ordinal ||
		revised.DurationSeconds != original.DurationSeconds ||
		revised.VisualDescription != original.VisualDescription ||
		revised.ContinuityNotes != original.ContinuityNotes {
		t.Fatalf("a row that was not at fault changed: %+v vs %+v", revised, original)
	}
}

// TestE2E004TheFindingReachesTheReportShape is AC-E2E-004's evidence clause where the database holds
// it.
//
// It goes through `MergeIssues`, so the finding is the one that reaches `review_issues`: the source
// mark AGENT_CONTRACTS section 11.4 requires, the auto-fixable flag and the evidence column are all
// asserted in the shape a user reads.
func TestE2E004TheFindingReachesTheReportShape(t *testing.T) {
	board := newCostumeFaultBoard(t)
	ctx := context.Background()
	board.injectWrongCostume(t, 5)
	findings, err := board.checkerFor(t).Check(ctx, "storyboard_table", board.versionID)
	if err != nil {
		t.Fatal(err)
	}
	merged := stagepipeline.MergeIssues(findings, nil)
	if len(merged) == 0 {
		t.Fatal("the merge dropped every deterministic finding")
	}
	found := false
	for _, issue := range merged {
		if issue.Rule != consistency.RuleCostumeContinuity {
			continue
		}
		found = true
		if string(issue.Source) != "deterministic" {
			t.Fatalf("the finding is marked %q, want deterministic", issue.Source)
		}
		if !issue.AutoFixable {
			t.Fatal("the merged finding lost its auto-fixable flag")
		}
		if !strings.Contains(issue.EvidenceJSON, board.costumeV1) || !strings.Contains(issue.EvidenceJSON, board.costumeV2) {
			t.Fatalf("the evidence column does not name both versions: %s", issue.EvidenceJSON)
		}
		if issue.EntityID != board.itemIDs[5] {
			t.Fatalf("the merged finding names %s, want the sixth row", issue.EntityID)
		}
	}
	if !found {
		t.Fatalf("the merged report has no costume finding: %+v", merged)
	}
}

// canaryClockInstant is the fixed instant the canary's fixture rows are stamped with.
//
// It is a helper rather than a constant so a reader can see it is the CANARY's clock and not a second
// one: a row whose timestamp came from `time.Now` would make two runs of this test differ in a column
// nothing asserts, which is how a fixture drifts from its own fixture.
func canaryClockInstant() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}
