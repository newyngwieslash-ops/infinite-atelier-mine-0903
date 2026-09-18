package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// dramaRepoHandle opens a temporary database migrated through the WP-05 head
// (version 12) and returns its raw pool. It is the fixture the storyboard,
// workflow and staleness repository tests share: the tables they write arrive
// in migrations 000007 through 000012, so the WP-04 set the existing helpers
// build is not enough.
//
// The helper names in this file are prefixed with "drama" so they cannot
// collide with fixtures another writer adds to this package for the story and
// script families.
func dramaRepoHandle(t *testing.T) *sql.DB {
	t.Helper()
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"),
		filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if handle.Mode() != ModeReady {
		t.Fatalf("database not ready: %v", handle.Err())
	}
	return handle.SQL()
}

// dramaSeedParents writes the rows every WP-05 table hangs off: one workspace,
// one project, one episode, one script with a version, and one workflow run.
func dramaSeedParents(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO workspaces (id, name, kind, created_at, updated_at, revision)
		 VALUES ('drama-ws', 'Local', 'local', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO projects (id, workspace_id, project_type, name, language, status, created_at, updated_at, revision)
		 VALUES ('drama-project', 'drama-ws', 'drama', 'Drama', 'zh-CN', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO episodes (id, project_id, season_number, episode_number, title, created_at, updated_at)
		 VALUES ('drama-episode', 'drama-project', 1, 1, 'Pilot', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO scripts (id, episode_id, created_at, updated_at)
		 VALUES ('drama-script', 'drama-episode', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO script_versions (id, script_id, version_number, status, created_at)
		 VALUES ('drama-script-version', 'drama-script', 1, 'draft', '2026-01-01T00:00:00Z')`,
		`INSERT INTO workflow_runs (id, project_id, workflow_type, status, created_at, updated_at)
		 VALUES ('drama-run', 'drama-project', 'episode_production', 'pending', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("seeding the drama fixture failed: %v\n%s", err, statement)
		}
	}
}

// dramaIDGenerator is a UUIDv7 generator over the package's fixed clock, so two
// fixtures in one test never mint the same identifier.
func dramaIDGenerator() *id.Generator {
	return newTestIDGenerator()
}

// dramaTime is the timestamp the fixtures stamp their rows with.
func dramaTime() time.Time {
	return fixedClock()()
}

// dramaStoryboardFixture is one director plan, one storyboard identity and one
// storyboard version, ready for an item or a panel to hang off.
type dramaStoryboardFixture struct {
	PlanVersionID string
	StoryboardID  string
	VersionID     string
}

// dramaSeedStoryboard writes a plan, a storyboard identity and a version
// through the repository, so the rows the storyboard and staleness tests reuse
// are the ones the production write path produces.
func dramaSeedStoryboard(t *testing.T, repo *StoryboardRepository) dramaStoryboardFixture {
	t.Helper()
	ctx := context.Background()
	generator := dramaIDGenerator()
	planID := mustNewID(t, generator)
	storyboardID := mustNewID(t, generator)
	versionID := mustNewID(t, generator)
	now := dramaTime()

	if err := repo.CreateDirectorPlanVersion(ctx, storyboard.DirectorPlanVersion{
		ID: planID, EpisodeID: "drama-episode", VersionNumber: 1, Status: versioning.StatusDraft,
		ScriptVersionID: "drama-script-version", CreatedByType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateDirectorPlanVersion: %v", err)
	}
	if err := repo.CreateStoryboard(ctx, storyboard.Storyboard{
		ID: storyboardID, EpisodeID: "drama-episode", CreatedAt: now, UpdatedAt: now, Revision: 1,
	}); err != nil {
		t.Fatalf("CreateStoryboard: %v", err)
	}
	if err := repo.CreateStoryboardVersion(ctx, storyboard.StoryboardVersion{
		ID: versionID, StoryboardID: storyboardID, VersionNumber: 1, Status: versioning.StatusDraft,
		ScriptVersionID: "drama-script-version", DirectorPlanVersionID: planID,
		CreatedByType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateStoryboardVersion: %v", err)
	}
	return dramaStoryboardFixture{PlanVersionID: planID, StoryboardID: storyboardID, VersionID: versionID}
}

// mustNewID mints one identifier or fails the test.
func mustNewID(t *testing.T, generator *id.Generator) string {
	t.Helper()
	value, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// TestStoryboardRepositoryRoundTrip covers create-and-read for all five
// families and checks the values that came back are the ones that went in.
func TestStoryboardRepositoryRoundTrip(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewStoryboardRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	fixture := dramaSeedStoryboard(t, repo)
	now := dramaTime()

	plan, err := repo.GetDirectorPlanVersion(ctx, fixture.PlanVersionID)
	if err != nil {
		t.Fatalf("GetDirectorPlanVersion: %v", err)
	}
	if plan.EpisodeID != "drama-episode" || plan.VersionNumber != 1 || plan.ScriptVersionID != "drama-script-version" {
		t.Fatalf("director plan round trip changed the row: %+v", plan)
	}
	if plan.Status != versioning.StatusDraft || plan.CreatedByType != versioning.CreatedByUser {
		t.Fatalf("director plan vocabulary lost: %+v", plan)
	}
	if !plan.CreatedAt.Equal(now) {
		t.Fatalf("created_at = %v, want %v", plan.CreatedAt, now)
	}
	highest, err := repo.MaxDirectorPlanVersionNumber(ctx, "drama-episode")
	if err != nil {
		t.Fatal(err)
	}
	if highest != 1 {
		t.Fatalf("MaxDirectorPlanVersionNumber = %d, want 1", highest)
	}
	empty, err := repo.MaxDirectorPlanVersionNumber(ctx, "no-such-episode")
	if err != nil {
		t.Fatal(err)
	}
	if empty != 0 {
		t.Fatalf("an episode with no plans reports %d, want 0", empty)
	}

	identity, err := repo.GetStoryboardByEpisode(ctx, "drama-episode")
	if err != nil {
		t.Fatalf("GetStoryboardByEpisode: %v", err)
	}
	if identity.ID != fixture.StoryboardID || identity.Revision != 1 {
		t.Fatalf("storyboard round trip: %+v", identity)
	}
	if _, err := repo.GetStoryboard(ctx, fixture.StoryboardID); err != nil {
		t.Fatalf("GetStoryboard: %v", err)
	}

	version, err := repo.GetStoryboardVersion(ctx, fixture.VersionID)
	if err != nil {
		t.Fatalf("GetStoryboardVersion: %v", err)
	}
	if version.StoryboardID != fixture.StoryboardID || version.DirectorPlanVersionID != fixture.PlanVersionID {
		t.Fatalf("storyboard version round trip: %+v", version)
	}
	// The stored row is the one the stale chain walks, so the domain must be
	// able to answer its question from it.
	if err := version.ValidateAgainst("drama-script-version", fixture.PlanVersionID); err != nil {
		t.Fatalf("the stored version does not validate against its own inputs: %v", err)
	}
	versions, err := repo.MaxStoryboardVersionNumber(ctx, fixture.StoryboardID)
	if err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Fatalf("MaxStoryboardVersionNumber = %d, want 1", versions)
	}

	itemID := mustNewID(t, generator)
	if err := repo.CreateStoryboardItem(ctx, storyboard.StoryboardItem{
		ID: itemID, StoryboardVersionID: fixture.VersionID, ShotID: "drama-shot-1", Ordinal: 1,
		ShotSize: "MCU", DurationSeconds: 4, VisualDescription: "a corridor",
		Status: versioning.StatusDraft, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}); err != nil {
		t.Fatalf("CreateStoryboardItem: %v", err)
	}
	item, err := repo.GetStoryboardItem(ctx, itemID)
	if err != nil {
		t.Fatalf("GetStoryboardItem: %v", err)
	}
	if item.ShotID != "drama-shot-1" || item.Ordinal != 1 || item.DurationSeconds != 4 || item.ShotSize != "MCU" {
		t.Fatalf("storyboard item round trip: %+v", item)
	}
	found, ok, err := repo.FindStoryboardItemByShot(ctx, fixture.VersionID, "drama-shot-1")
	if err != nil || !ok || found.ID != itemID {
		t.Fatalf("FindStoryboardItemByShot = %+v ok=%v err=%v", found, ok, err)
	}
	if _, ok, err := repo.FindStoryboardItemByShot(ctx, fixture.VersionID, "no-such-shot"); err != nil || ok {
		t.Fatalf("a missing shot reported found=%v err=%v", ok, err)
	}
	byOrdinal, ok, err := repo.FindStoryboardItemByOrdinal(ctx, fixture.VersionID, 1)
	if err != nil || !ok || byOrdinal.ID != itemID {
		t.Fatalf("FindStoryboardItemByOrdinal = %+v ok=%v err=%v", byOrdinal, ok, err)
	}

	panelID := mustNewID(t, generator)
	if err := repo.CreatePanelVersion(ctx, storyboard.StoryboardPanelVersion{
		ID: panelID, StoryboardItemID: itemID, VersionNumber: 1, Status: versioning.StatusDraft,
		VisualPrompt: "wide shot", CreatedByType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreatePanelVersion: %v", err)
	}
	panel, err := repo.GetPanelVersion(ctx, panelID)
	if err != nil {
		t.Fatalf("GetPanelVersion: %v", err)
	}
	if panel.StoryboardItemID != itemID || panel.VisualPrompt != "wide shot" || panel.ApprovedImageAssetVersionID != "" {
		t.Fatalf("panel round trip: %+v", panel)
	}
	panels, err := repo.ListPanelVersions(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if len(panels) != 1 || panels[0].ID != panelID {
		t.Fatalf("ListPanelVersions = %+v", panels)
	}
	maxPanels, err := repo.MaxPanelVersionNumber(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if maxPanels != 1 {
		t.Fatalf("MaxPanelVersionNumber = %d, want 1", maxPanels)
	}
	foreignKeysClean(t, db)
}

// TestStoryboardRepositoryListItemsOrdersByOrdinal proves the read path returns
// the shootable order, not insertion order.
func TestStoryboardRepositoryListItemsOrdersByOrdinal(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewStoryboardRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	fixture := dramaSeedStoryboard(t, repo)
	now := dramaTime()

	// Inserted out of order on purpose: the query orders, the writer must not
	// have to.
	for _, ordinal := range []int{3, 1, 2} {
		if err := repo.CreateStoryboardItem(ctx, storyboard.StoryboardItem{
			ID: mustNewID(t, generator), StoryboardVersionID: fixture.VersionID,
			ShotID: "drama-shot-" + strconv.Itoa(ordinal), Ordinal: ordinal,
			Status: versioning.StatusDraft, CreatedAt: now, UpdatedAt: now, Revision: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := repo.ListStoryboardItems(ctx, fixture.VersionID)
	if err != nil {
		t.Fatalf("ListStoryboardItems: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("ListStoryboardItems returned %d rows, want 3", len(items))
	}
	for index, item := range items {
		if item.Ordinal != index+1 {
			t.Fatalf("item %d has ordinal %d, want %d", index, item.Ordinal, index+1)
		}
	}
	foreignKeysClean(t, db)
}

// TestStoryboardRepositoryEnforcesOneItemPerShotAndPosition proves the two
// unique constraints migration 000010 declares are actually enforced, so the
// application's pre-check is a courtesy rather than the guarantee.
func TestStoryboardRepositoryEnforcesOneItemPerShotAndPosition(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewStoryboardRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	fixture := dramaSeedStoryboard(t, repo)
	now := dramaTime()

	if err := repo.CreateStoryboardItem(ctx, storyboard.StoryboardItem{
		ID: mustNewID(t, generator), StoryboardVersionID: fixture.VersionID, ShotID: "drama-shot-1",
		Ordinal: 1, Status: versioning.StatusDraft, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	// The same shot at a free position.
	err := repo.CreateStoryboardItem(ctx, storyboard.StoryboardItem{
		ID: mustNewID(t, generator), StoryboardVersionID: fixture.VersionID, ShotID: "drama-shot-1",
		Ordinal: 2, Status: versioning.StatusDraft, CreatedAt: now, UpdatedAt: now, Revision: 1,
	})
	if err == nil {
		t.Fatal("a shot was stored twice in one storyboard version")
	}
	if _, ok := storyboard.AsError(err); !ok {
		t.Fatalf("the constraint failure is not a domain error: %v", err)
	}
	// A different shot at the taken position.
	err = repo.CreateStoryboardItem(ctx, storyboard.StoryboardItem{
		ID: mustNewID(t, generator), StoryboardVersionID: fixture.VersionID, ShotID: "drama-shot-2",
		Ordinal: 1, Status: versioning.StatusDraft, CreatedAt: now, UpdatedAt: now, Revision: 1,
	})
	if err == nil {
		t.Fatal("two shots were stored at one position")
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM storyboard_items") != 1 {
		t.Fatal("a rejected insert wrote a row")
	}
	foreignKeysClean(t, db)
}

// TestStoryboardRepositoryRejectsAnUnknownParent proves the foreign keys are
// reported as domain errors, so a caller is told about the missing parent
// rather than about a constraint.
func TestStoryboardRepositoryRejectsAnUnknownParent(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewStoryboardRepository(db)
	ctx := context.Background()
	now := dramaTime()

	missing := "00000000-0000-7000-8000-000000000001"
	err := repo.CreateStoryboardItem(ctx, storyboard.StoryboardItem{
		ID: mustNewID(t, dramaIDGenerator()), StoryboardVersionID: missing, ShotID: "drama-shot-1",
		Ordinal: 1, Status: versioning.StatusDraft, CreatedAt: now, UpdatedAt: now, Revision: 1,
	})
	if err == nil {
		t.Fatal("a storyboard item was stored under a version that does not exist")
	}
	if domainErr, ok := storyboard.AsError(err); !ok || domainErr.Category != storyboard.CategoryInvalidInput {
		t.Fatalf("expected the domain's invalid_input, got %v", err)
	}
	// A version that names a script version which does not exist is refused too.
	if err := repo.CreateStoryboardVersion(ctx, storyboard.StoryboardVersion{
		ID: mustNewID(t, dramaIDGenerator()), StoryboardID: missing, VersionNumber: 1,
		Status: versioning.StatusDraft, ScriptVersionID: "drama-script-version",
		DirectorPlanVersionID: missing, CreatedByType: versioning.CreatedByUser, CreatedAt: now,
	}); err == nil {
		t.Fatal("a storyboard version was stored under a storyboard that does not exist")
	}
	if _, err := repo.GetStoryboard(ctx, missing); err == nil {
		t.Fatal("reading a missing storyboard succeeded")
	} else if domainErr, ok := storyboard.AsError(err); !ok || domainErr.Category != storyboard.CategoryNotFound {
		t.Fatalf("expected not_found, got %v", err)
	}
}

// TestStoryboardRepositoryApprovePanelImageGuardsTheItemRevision is the
// compare-and-swap the panel table has no revision column for: the guard is
// taken on the panel's parent item, and a stale revision writes neither the
// item nor the approval.
func TestStoryboardRepositoryApprovePanelImageGuardsTheItemRevision(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewStoryboardRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	fixture := dramaSeedStoryboard(t, repo)
	now := dramaTime()

	itemID := mustNewID(t, generator)
	if err := repo.CreateStoryboardItem(ctx, storyboard.StoryboardItem{
		ID: itemID, StoryboardVersionID: fixture.VersionID, ShotID: "drama-shot-1", Ordinal: 1,
		Status: versioning.StatusDraft, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	panelID := mustNewID(t, generator)
	if err := repo.CreatePanelVersion(ctx, storyboard.StoryboardPanelVersion{
		ID: panelID, StoryboardItemID: itemID, VersionNumber: 1, Status: versioning.StatusDraft,
		CreatedByType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// A stale expected revision is a conflict, and the approval is not written.
	if err := repo.ApprovePanelImage(ctx, panelID, "drama-asset-version", 7); err == nil {
		t.Fatal("a stale revision was accepted")
	} else if domainErr, ok := storyboard.AsError(err); !ok || domainErr.Category != storyboard.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	stored, err := repo.GetPanelVersion(ctx, panelID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ApprovedImageAssetVersionID != "" {
		t.Fatalf("a rejected approval wrote %q", stored.ApprovedImageAssetVersionID)
	}

	// The matching revision is accepted and advances the item's revision.
	if err := repo.ApprovePanelImage(ctx, panelID, "drama-asset-version", 1); err != nil {
		t.Fatalf("ApprovePanelImage: %v", err)
	}
	stored, err = repo.GetPanelVersion(ctx, panelID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ApprovedImageAssetVersionID != "drama-asset-version" {
		t.Fatalf("stored approval = %q", stored.ApprovedImageAssetVersionID)
	}
	item, err := repo.GetStoryboardItem(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Revision != 2 {
		t.Fatalf("item revision = %d, want 2 after one approval", item.Revision)
	}
	// Approving a panel that does not exist is a not-found rather than a write.
	if err := repo.ApprovePanelImage(ctx, mustNewID(t, generator), "drama-asset-version", 2); err == nil {
		t.Fatal("approving a missing panel succeeded")
	}
	foreignKeysClean(t, db)
}
