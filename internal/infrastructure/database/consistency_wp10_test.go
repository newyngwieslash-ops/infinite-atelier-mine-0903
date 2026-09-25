package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// wp10ConsistencyFixture is a board whose rows cite real assets, plus the story state those citations
// are compared against.
//
// It is built from the DRAMA fixtures the other repository tests share, so the checks run over the
// real schema with the real foreign keys rather than over hand-built structs.
type wp10ConsistencyFixture struct {
	db          *sql.DB
	versionID   string
	itemIDs     []string
	costumeV1   string
	costumeV2   string
	characterID string
}

// seedConsistencyFixture writes two shots, one scene, three assets and a character state.
func seedConsistencyFixture(t *testing.T) wp10ConsistencyFixture {
	t.Helper()
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	repo := NewStoryboardRepository(db)
	fixture := dramaSeedStoryboard(t, repo)
	now := dramaTime()

	// The script structure the coverage and duration rules read: one scene with two shots.
	statements := []string{
		`INSERT INTO scenes (id, script_version_id, ordinal, scene_number, slugline, interior_exterior,
			estimated_duration_seconds, created_at, updated_at, revision)
		 VALUES ('cs-scene', 'drama-script-version', 1, '1', 'INT. 渡口 - 夜', 'INT',
			10, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO shots (id, scene_id, ordinal, shot_number, estimated_duration_seconds,
			created_at, updated_at, revision)
		 VALUES ('cs-shot-1', 'cs-scene', 1, '1', 5, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO shots (id, scene_id, ordinal, shot_number, estimated_duration_seconds,
			created_at, updated_at, revision)
		 VALUES ('cs-shot-2', 'cs-scene', 2, '2', 5, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		// The script version's own estimate, which the duration rule compares against. Ten seconds for
		// two five-second shots, so a correct board is inside the tolerance.
		`UPDATE script_versions SET estimated_duration_seconds = 10 WHERE id = 'drama-script-version'`,
		// The character and the two costume versions.
		`INSERT INTO story_entities (id, project_id, entity_type, canonical_name, status, created_at, updated_at, revision)
		 VALUES ('cs-char', 'drama-project', 'character', '沈砚', 'accepted',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the consistency fixture failed: %v\n%s", err, statement)
		}
	}

	// Two costume assets: version 1 is approved, version 2 is superseded. The story says the character
	// wears v1 at this point, so a row citing v2 is the AC-E2E-004 fault.
	if _, err := db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, story_entity_id, current_approved_version_id, status,
		 created_at, updated_at, revision)
		VALUES ('cs-costume', 'drama-project', 'costume', '冬装', 'cs-char', '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	assets := NewAssetRepository(db)
	costumeV1 := mustNewID(t, generator)
	costumeV2 := mustNewID(t, generator)
	for index, versionID := range []string{costumeV1, costumeV2} {
		status := versioning.StatusApproved
		if index == 1 {
			status = versioning.StatusSuperseded
		}
		// CreatedByType is stated rather than left empty: the schema's CHECK refuses an empty value and
		// the domain's Validate does not, so a fixture that omitted it would fail on a constraint the
		// caller could not see. Every real writer states one; this is not a defect in the repository but
		// in a fixture that left a required column at its zero value.
		if err := assets.CreateVersion(ctx, asset.Version{
			ID: versionID, AssetID: "cs-costume", VersionNumber: index + 1, Status: status,
			CreatedByType: versioning.CreatedByUser, CreatedAt: now,
		}); err != nil {
			t.Fatalf("CreateVersion: %v", err)
		}
	}
	// v1 is the version in force, which is what "approved asset version" means (section 8.4).
	if _, err := db.ExecContext(ctx, `UPDATE assets SET current_approved_version_id = ? WHERE id = 'cs-costume'`, costumeV1); err != nil {
		t.Fatal(err)
	}
	// And the story says the character wears it from the beginning of the story onward.
	if _, err := db.ExecContext(ctx, `INSERT INTO character_states
		(id, character_entity_id, from_event_order, to_event_order, appearance_json,
		 costume_asset_version_id, injuries_json, possessions_json, relationship_state_json,
		 source_fact_id, status, created_at, updated_at, revision)
		VALUES ('cs-state', 'cs-char', 1, NULL, '', ?, '', '', '', '', 'accepted',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, costumeV1); err != nil {
		t.Fatal(err)
	}

	// Two board rows, one per shot, ordinals 1 and 2.
	itemIDs := []string{}
	for index, shotID := range []string{"cs-shot-1", "cs-shot-2"} {
		itemID := mustNewID(t, generator)
		if err := repo.CreateStoryboardItem(ctx, storyboard.StoryboardItem{
			ID: itemID, StoryboardVersionID: fixture.VersionID, ShotID: shotID, Ordinal: index + 1,
			ShotSize: "MS", DurationSeconds: 5, Status: versioning.StatusDraft,
			CreatedAt: now, UpdatedAt: now, Revision: 1,
		}); err != nil {
			t.Fatalf("CreateStoryboardItem: %v", err)
		}
		itemIDs = append(itemIDs, itemID)
	}
	return wp10ConsistencyFixture{
		db: db, versionID: fixture.VersionID, itemIDs: itemIDs,
		costumeV1: costumeV1, costumeV2: costumeV2, characterID: "cs-char",
	}
}

// checkStoryboard runs the rules through the composition root's own entry point, which takes a stage.
func (f wp10ConsistencyFixture) checkStoryboard(ctx context.Context, versionID string) ([]consistency.Finding, error) {
	return f.checkerFor(nil).Check(ctx, "storyboard_table", versionID)
}

// checkerFor builds the checker over the fixture's database.
//
// The adapters are LOCAL to this test rather than the composition root's, because they are the same
// translation and a test that used the root's would be testing the root's file as well — and because
// the root package imports this one, so a test here cannot name its types. The shapes are identical,
// which is what the compile-time assertions at the foot of each adapter prove.
func (f wp10ConsistencyFixture) checkerFor(t *testing.T) *StoryboardConsistencyChecker {
	board := NewStoryboardRepository(f.db)
	assets := NewAssetRepository(f.db)
	story := NewStoryRepository(f.db)
	scriptRepo := NewScriptRepository(f.db)
	return NewStoryboardConsistencyChecker(
		boardConsistencyReader{repo: board},
		assetRepoConsistencyReader{repo: assets},
		scriptSourceRef{repo: scriptRepo},
		storyRepoConsistencyReader{repo: story},
	)
}

// boardConsistencyReader adapts the storyboard repository to the checker's board port.
type boardConsistencyReader struct{ repo *StoryboardRepository }

func (r boardConsistencyReader) GetStoryboardVersion(ctx context.Context, id string) (storyboard.StoryboardVersion, error) {
	return r.repo.GetStoryboardVersion(ctx, id)
}

func (r boardConsistencyReader) ListStoryboardItems(ctx context.Context, id string) ([]storyboard.StoryboardItem, error) {
	return r.repo.ListStoryboardItems(ctx, id)
}

var _ appconsistency.StoryboardReader = boardConsistencyReader{}

// assetRepoConsistencyReader adapts the asset repository to the checker's asset port.
type assetRepoConsistencyReader struct{ repo *AssetRepository }

func (r assetRepoConsistencyReader) UsagesForConsumer(ctx context.Context, consumerType asset.ConsumerType, consumerID string) ([]asset.Usage, error) {
	return r.repo.UsagesForConsumer(ctx, consumerType, consumerID)
}

func (r assetRepoConsistencyReader) GetVersion(ctx context.Context, id string) (asset.Version, error) {
	return r.repo.GetVersion(ctx, id)
}

func (r assetRepoConsistencyReader) GetAsset(ctx context.Context, id string) (asset.Asset, error) {
	return r.repo.GetAsset(ctx, id)
}

func (r assetRepoConsistencyReader) ListFilesWithTypes(ctx context.Context, versionID string) ([]appconsistency.AssetFile, error) {
	return r.repo.ListFilesWithTypes(ctx, versionID)
}

var _ appconsistency.AssetReader = assetRepoConsistencyReader{}

// storyRepoConsistencyReader adapts the story repository to the checker's story port.
type storyRepoConsistencyReader struct{ repo *StoryRepository }

func (r storyRepoConsistencyReader) CostumeStateAt(ctx context.Context, characterEntityID string, eventOrder int) (string, bool, error) {
	return r.repo.CostumeStateAt(ctx, characterEntityID, eventOrder)
}

func (r storyRepoConsistencyReader) StoryEventParticipantsFor(ctx context.Context, storyEventID string) ([]string, error) {
	return r.repo.StoryEventParticipantsFor(ctx, storyEventID)
}

var _ appconsistency.StoryStateReader = storyRepoConsistencyReader{}

// scriptSourceRef reads a script version's structure directly, which is what the composition root's
// adapter does through the script service.
type scriptSourceRef struct{ repo *ScriptRepository }

func (s scriptSourceRef) ScriptReaderFor(ctx context.Context, scriptVersionID string) appconsistency.ScriptReader {
	return &scriptReaderRef{repo: s.repo, version: scriptVersionID}
}

type scriptReaderRef struct {
	repo    *ScriptRepository
	version string
	// structure is read once, because the rules ask four questions of it per check.
	structure *script.ScriptStructure
	loaded    bool
}

func (r *scriptReaderRef) load(ctx context.Context) *script.ScriptStructure {
	if r.loaded {
		return r.structure
	}
	r.loaded = true
	if r.repo == nil || r.version == "" {
		return nil
	}
	structure, err := r.repo.GetScriptStructure(ctx, r.version)
	if err != nil {
		return nil
	}
	r.structure = &structure
	return r.structure
}

func (r *scriptReaderRef) ShotIDs(ctx context.Context) ([]string, error) {
	structure := r.load(ctx)
	if structure == nil {
		return nil, nil
	}
	ids := []string{}
	for _, scene := range structure.Scenes {
		for _, shot := range scene.Shots {
			ids = append(ids, shot.ID)
		}
	}
	return ids, nil
}

func (r *scriptReaderRef) Duration(ctx context.Context) (int, error) {
	_ = r.load(ctx)
	if r.repo == nil {
		return 0, nil
	}
	version, err := r.repo.GetScriptVersion(ctx, r.version)
	if err != nil {
		return 0, err
	}
	return version.EstimatedDurationSeconds, nil
}

func (r *scriptReaderRef) SceneEventOf(ctx context.Context, sceneID string) (string, error) {
	structure := r.load(ctx)
	if structure == nil {
		return "", nil
	}
	for _, scene := range structure.Scenes {
		if scene.ID == sceneID {
			return scene.SourceStoryEventID, nil
		}
	}
	return "", nil
}

func (r *scriptReaderRef) ShotScene(ctx context.Context, shotID string) (string, error) {
	structure := r.load(ctx)
	if structure == nil {
		return "", nil
	}
	for _, scene := range structure.Scenes {
		for _, shot := range scene.Shots {
			if shot.ID == shotID {
				return scene.ID, nil
			}
		}
	}
	return "", nil
}

var _ appconsistency.ScriptReader = (*scriptReaderRef)(nil)
var _ = id.Generator{}
var _ = time.Now

// TestConsistencyACleanBoardReportsNothing is the rule that keeps the checks usable.
//
// A board that covers its shots, cites the approved costume and totals the script's own duration must
// produce NO findings. A check that fired on a correct artifact would be one a user learns to
// ignore, and the six rules are all capable of that failure in their own way: a coverage rule that
// mis-reads the script, a continuity rule that cannot tell "no state recorded" from "wrong costume",
// a duration rule with no tolerance.
func TestConsistencyACleanBoardReportsNothing(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	// The board cites the costume version that is both approved and in force.
	if err := NewAssetRepository(fixture.db).AddUsage(ctx, asset.Usage{
		ID: "clean-usage", AssetVersionID: fixture.costumeV1,
		ConsumerType: asset.ConsumerShot, ConsumerID: fixture.itemIDs[0],
		UsageRole: "costume", Required: true, CreatedAt: dramaTime(),
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatalf("CheckStoryboard: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("a clean board produced %d findings, want none: %+v", len(findings), findings)
	}
}

// TestConsistencyLocatesTheWrongCostume is AC-E2E-004 and AC-BOARD-002's own defect.
//
// The criterion: "人为制造一个角色服装引用错误 — Supervisor 定位到具体 Shot；ReviewReport 提供证据和
// 建议". The injection is a row citing a costume version the story does not have in force at that
// point, and the assertion is that the CODE finds it — with the row identified and the two versions
// named as evidence — rather than that a model happened to notice.
func TestConsistencyLocatesTheWrongCostume(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	// Row TWO cites the superseded costume, which is the fault.
	if err := NewAssetRepository(fixture.db).AddUsage(ctx, asset.Usage{
		ID: "wrong-usage", AssetVersionID: fixture.costumeV2,
		ConsumerType: asset.ConsumerShot, ConsumerID: fixture.itemIDs[1],
		UsageRole: "costume", Required: true, CreatedAt: dramaTime(),
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatalf("CheckStoryboard: %v", err)
	}
	located := false
	for _, finding := range findings {
		if finding.Rule != consistency.RuleCostumeContinuity {
			continue
		}
		located = true
		// It points at the ROW that is wrong, not at the board.
		if finding.EntityID != fixture.itemIDs[1] {
			t.Fatalf("the finding names %s, want the second row", finding.EntityID)
		}
		if finding.Field != "costumeVersionId" {
			t.Fatalf("the finding is about field %q", finding.Field)
		}
		// The evidence names both versions, so a reader can compare them (FR-110's own example cites
		// two references for exactly this reason).
		refs := map[string]bool{}
		for _, entry := range finding.Evidence {
			refs[entry.Ref] = true
		}
		if !refs[fixture.costumeV2] {
			t.Fatalf("the finding does not cite the version the row uses: %+v", finding.Evidence)
		}
		if !refs[fixture.costumeV1] {
			t.Fatalf("the finding does not cite the version the story has in force: %+v", finding.Evidence)
		}
		// It is a blocker, and it is auto-fixable — which is what makes the FIX step of the criterion
		// mean "repoint the citation" rather than "rewrite the row".
		if !finding.Blocker() {
			t.Fatalf("a wrong costume is not a blocker: %+v", finding)
		}
		if !finding.AutoFixable {
			t.Fatal("a wrong costume is not marked auto-fixable")
		}
	}
	if !located {
		t.Fatalf("the wrong costume was not located: %+v", findings)
	}
}

// TestConsistencyDoesNotFireWithoutAStoryState is the false-positive guard.
//
// A character whose costume nobody has recorded is not a character in the wrong costume. The rule must
// stay silent, because the alternative — reporting every row of a project that has not filled in its
// character states — would make the check unusable from the day it shipped.
func TestConsistencyDoesNotFireWithoutAStoryState(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	// Remove the state that covers the board's position, leaving the character with nothing recorded.
	if _, err := fixture.db.ExecContext(ctx, `DELETE FROM character_states WHERE id = 'cs-state'`); err != nil {
		t.Fatal(err)
	}
	if err := NewAssetRepository(fixture.db).AddUsage(ctx, asset.Usage{
		ID: "orphan-usage", AssetVersionID: fixture.costumeV1,
		ConsumerType: asset.ConsumerShot, ConsumerID: fixture.itemIDs[0],
		UsageRole: "costume", Required: true, CreatedAt: dramaTime(),
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Rule == consistency.RuleCostumeContinuity {
			t.Fatalf("the continuity rule fired with no story state to compare against: %+v", finding)
		}
	}
}

// TestConsistencyFindsAMissingShot is section 11.3's "缺失镜头".
func TestConsistencyFindsAMissingShot(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	// Delete the second row, leaving the script's second shot unboarded.
	if _, err := fixture.db.ExecContext(ctx, `DELETE FROM storyboard_items WHERE id = ?`, fixture.itemIDs[1]); err != nil {
		t.Fatal(err)
	}
	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range findings {
		if finding.Rule == consistency.RuleShotCoverage && finding.Field == "shotId" {
			found = true
			if !finding.Blocker() {
				t.Fatalf("an unboarded shot is not a blocker: %+v", finding)
			}
			if finding.AutoFixable {
				t.Fatal("a missing row is marked auto-fixable, but what it should say is a decision")
			}
		}
	}
	if !found {
		t.Fatalf("the missing shot was not reported: %+v", findings)
	}
}

// TestConsistencyFindsADurationDrift is section 11.3's "时长总和", and its tolerance.
func TestConsistencyFindsADurationDrift(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	// Ten seconds of rows against a ten-second estimate is clean; doubling one row is not.
	if _, err := fixture.db.ExecContext(ctx,
		`UPDATE storyboard_items SET duration_seconds = 40 WHERE id = ?`, fixture.itemIDs[0]); err != nil {
		t.Fatal(err)
	}
	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range findings {
		if finding.Rule == consistency.RuleDuration {
			found = true
			// A drift is a remark rather than a blocker: the board is longer than planned, which a
			// person decides about.
			if finding.Blocker() {
				t.Fatalf("a duration drift blocks the stage: %+v", finding)
			}
		}
	}
	if !found {
		t.Fatalf("the duration drift was not reported: %+v", findings)
	}
	// And a board inside the tolerance is silent, which is what makes the rule's number meaningful.
	fixture2 := seedConsistencyFixture(t)
	if _, err := fixture2.db.ExecContext(ctx,
		`UPDATE storyboard_items SET duration_seconds = 6 WHERE id = ?`, fixture2.itemIDs[0]); err != nil {
		t.Fatal(err)
	}
	findings, err = fixture2.checkStoryboard(ctx, fixture2.versionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Rule == consistency.RuleDuration {
			t.Fatalf("a one-second drift on a ten-second estimate was reported: %+v", finding)
		}
	}
}

// TestConsistencyFindsASupersededAsset checks the rule that is separate from continuity.
//
// A row may cite a costume the story agrees with and that the project has nonetheless replaced. That
// is the "Approved Version" half of section 11.2, and it is a Critical rather than a Major finding: the
// image would be generated from a version the project no longer stands behind.
func TestConsistencyFindsASupersededAsset(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	// Approve v2, so v1 becomes the superseded one while the story still names it.
	if _, err := fixture.db.ExecContext(ctx,
		`UPDATE assets SET current_approved_version_id = ? WHERE id = 'cs-costume'`, fixture.costumeV2); err != nil {
		t.Fatal(err)
	}
	if err := NewAssetRepository(fixture.db).AddUsage(ctx, asset.Usage{
		ID: "stale-usage", AssetVersionID: fixture.costumeV1,
		ConsumerType: asset.ConsumerShot, ConsumerID: fixture.itemIDs[0],
		UsageRole: "reference", Required: true, CreatedAt: dramaTime(),
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range findings {
		if finding.Rule == consistency.RuleAssetApproved {
			found = true
			if finding.Severity != consistency.SeverityCritical {
				t.Fatalf("a superseded citation is %q, want critical", finding.Severity)
			}
			// The suggestion names the version to use, so the fix is a value rather than a search.
			if finding.Suggestion == "" {
				t.Fatal("the finding suggests nothing")
			}
		}
	}
	if !found {
		t.Fatalf("the superseded citation was not reported: %+v", findings)
	}
}

// TestTheCheckerCoversOnlyTheStagesItHasRulesFor keeps the stage map honest.
//
// A checker that silently ran the storyboard rules for the asset stage would report findings about
// another artifact's version id, and the pipeline would attribute them to the wrong stage.
func TestTheCheckerCoversOnlyTheStagesItHasRulesFor(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	checker := fixture.checkerFor(t)
	for _, stage := range []string{"asset_analysis", "asset_generation", "director_plan", "script_generation", ""} {
		findings, err := checker.Check(context.Background(), stage, fixture.versionID)
		if err != nil {
			t.Fatalf("Check(%q): %v", stage, err)
		}
		if len(findings) != 0 {
			t.Fatalf("stage %q has no ruleset but reported %d findings", stage, len(findings))
		}
	}
	// The covered stage does run, which is the other half of the assertion.
	findings, err := checker.Check(context.Background(), "storyboard_table", fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		// The clean fixture has nothing wrong, so this asserts the call RAN rather than that it found
		// something: a version id the checker could not read would have produced an error, and an empty
		// result from a clean board is the correct answer.
		t.Fatalf("the covered stage reported %d findings on a clean board: %+v", len(findings), findings)
	}
}
