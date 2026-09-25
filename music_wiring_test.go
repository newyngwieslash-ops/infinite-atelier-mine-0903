package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
)

// music_wiring_test.go drives the PRODUCTION musicImporterAdapter over a real database.
//
// # Why this file exists, and what a mutation proved about its absence
//
// The music import binding has a suite over a double that grades the ORDER of its five steps, and the
// database suite has a walk that builds the same rows by hand. Neither touches `musicImporterAdapter` —
// the production adapter that decides WHICH usage role a bed is recorded with — so a mutation that
// changed `asset.ConsumerShot` to `asset.ConsumerJob` there SURVIVED a full mutation run. The mutation
// harness is what found it: its anchor matched nothing in the file I had pointed it at, and following
// that anchor led here.
//
// The consequence that mutation would have had is the defect this repository keeps finding, in its data
// form: the mix joins `asset_usages` on the SHOT, so a bed recorded against a job is a perfectly valid
// row that NO READ FINDS. The import would report success, the library would show the track, and the
// music would never play.
//
// So this test asserts the two things the adapter alone decides: the role a bed is recorded with, and
// the file role its bytes are attached under. Both are strings, and both are invisible to every test
// that supplies its own equivalent.

// musicHarness composes the drama stack and the music importer over a fresh database.
type musicHarness struct {
	drama     *dramaWiring
	db        *sql.DB
	projectID string
	shotID    string
}

func newMusicHarness(t *testing.T) *musicHarness {
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
	projectID := seedWiringProject(t, canvasWriter)
	// One episode and one shot: the bed is attached to a SHOT, and the shot has to exist.
	shotID := seedShotForMusic(t, handle, projectID)
	return &musicHarness{drama: stack, db: handle.SQL(), projectID: projectID, shotID: shotID}
}

// seedShotForMusic writes the episode, script and storyboard rows a shot needs.
//
// It inserts them directly rather than driving five services to produce one shot: the subject of this
// file is the adapter's two role strings, and a fixture that went through the whole production pipeline
// would take a hundred lines to reach the same row.
func seedShotForMusic(t *testing.T, handle *database.Handle, projectID string) string {
	t.Helper()
	ctx := context.Background()
	// The column lists are the schema's own — read from migration 000010 — because the version tables
	// carry NOT NULL columns with no default (a script version to cite, a status), and a fixture that
	// guessed at them would fail on the first insert rather than on the assertion.
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO episodes (id, project_id, season_number, episode_number, title, created_at, updated_at)
			VALUES ('music-episode', ?, 1, 1, 'Pilot', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			[]any{projectID}},
		{`INSERT INTO scripts (id, episode_id, created_at, updated_at)
			VALUES ('music-script', 'music-episode', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, nil},
		{`INSERT INTO script_versions (id, script_id, version_number, status, created_at)
			VALUES ('music-script-version', 'music-script', 1, 'approved', '2026-01-01T00:00:00Z')`, nil},
		// A director plan version cites the EPISODE and the script version; there is no separate plan
		// identity table, which is what the first version of this fixture got wrong.
		{`INSERT INTO director_plan_versions
			(id, episode_id, version_number, status, script_version_id, created_at)
			VALUES ('music-plan-version', 'music-episode', 1, 'approved', 'music-script-version',
				'2026-01-01T00:00:00Z')`, nil},
		{`INSERT INTO storyboards (id, episode_id, created_at, updated_at)
			VALUES ('music-board', 'music-episode', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, nil},
		{`INSERT INTO storyboard_versions
			(id, storyboard_id, version_number, status, script_version_id, director_plan_version_id, created_at)
			VALUES ('music-board-version', 'music-board', 1, 'approved', 'music-script-version',
				'music-plan-version', '2026-01-01T00:00:00Z')`, nil},
		{`INSERT INTO storyboard_items
			(id, storyboard_version_id, shot_id, ordinal, duration_seconds, status, created_at, updated_at)
			VALUES ('music-item', 'music-board-version', 'music-shot-1', 1, 4, 'approved',
				'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, nil},
	}
	for _, statement := range statements {
		if _, err := handle.SQL().ExecContext(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seeding the music fixture failed: %v\n%s", err, statement.sql)
		}
	}
	return "music-shot-1"
}

// TestTheProductionAdapterRecordsABedAgainstItsShot is the assertion the surviving mutation asked for.
func TestTheProductionAdapterRecordsABedAgainstItsShot(t *testing.T) {
	ctx := context.Background()
	harness := newMusicHarness(t)
	adapter := harness.drama.musicImporter

	// --- the five steps the import takes, over the REAL adapter.
	stored, err := adapter.Store(ctx, "theme.wav", []byte("RIFF\x00\x00\x00\x00WAVEfmt "))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	assetID, versionID, err := adapter.CreateBeddableAsset(ctx, harness.projectID, "theme.wav")
	if err != nil {
		t.Fatalf("CreateBeddableAsset: %v", err)
	}
	if err := adapter.Attach(ctx, versionID, stored.Hash); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := adapter.Approve(ctx, assetID, versionID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := adapter.Bed(ctx, versionID, harness.shotID); err != nil {
		t.Fatalf("Bed: %v", err)
	}

	// --- THE TWO STRINGS THE ADAPTER ALONE DECIDES.
	//
	// 1. The file is attached as `primary`, because that is the role the audio reader selects
	//    (`AudioFileFor` reads `role = 'primary'`). Under any other role the bytes are stored and never
	//    played — a silent bed, which is the same outcome as the defect this test exists for.
	var fileRole string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT role FROM asset_files WHERE asset_version_id = ?`, versionID).Scan(&fileRole); err != nil {
		t.Fatalf("reading the file role: %v", err)
	}
	if fileRole != string(asset.RolePrimary) {
		t.Fatalf("the bed's bytes are attached as %q, so the mixer cannot read them", fileRole)
	}

	// 2. The usage names the SHOT, and this is the assertion a mutation survived without: the mix joins
	//    `asset_usages` on (consumer_type='shot', consumer_id=<a row's shot>), so a bed recorded against
	//    any other consumer type is a row no read finds.
	var consumerType, consumerID, usageRole string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT consumer_type, consumer_id, usage_role FROM asset_usages WHERE asset_version_id = ?`,
		versionID).Scan(&consumerType, &consumerID, &usageRole); err != nil {
		t.Fatalf("reading the usage: %v", err)
	}
	if consumerType != string(asset.ConsumerShot) {
		t.Fatalf("the bed is recorded against the consumer type %q, which no read joins", consumerType)
	}
	if consumerID != harness.shotID {
		t.Fatalf("the bed is recorded against %q rather than its shot %q", consumerID, harness.shotID)
	}
	if usageRole != "audio_music" {
		t.Fatalf("the bed's usage role is %q", usageRole)
	}

	// 3. And the approval took: the mix's join requires the version to be the asset's current approved
	//    one, so an import that skipped the approval would be invisible for a third reason.
	var approved string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT current_approved_version_id FROM assets WHERE id = ?`, assetID).Scan(&approved); err != nil {
		t.Fatalf("reading the approval: %v", err)
	}
	if approved != versionID {
		t.Fatalf("the imported version is not the approved one (%q vs %q)", approved, versionID)
	}
}

// TestStoringBytesRecordsTheMetadataRow is the defect that made an entire shipped feature unusable.
//
// # What was wrong
//
// `asset_files.file_hash` has a FOREIGN KEY to `file_objects(hash)`. The store adapter called only the
// filesystem `Import` — which writes the file and nothing else — so `Store` reported success, the table
// held ZERO rows for the hash, and the `Link` that follows failed with "the requested asset no longer
// exists". The previs snapshot path (WP-21) performs exactly those two steps, so **it could never have
// worked in production**: its own suite supplies a double that records the metadata itself, and every
// test passed while the real composition could not complete a snapshot.
//
// # Why the assertion is the ROW COUNT
//
// Because that is what was wrong, and because it is what a caller cannot see: `Store` returned nil both
// before and after. A test that asserted only "the store succeeded" is precisely the test that let this
// survive a package and a half.
func TestStoringBytesRecordsTheMetadataRow(t *testing.T) {
	ctx := context.Background()
	harness := newMusicHarness(t)
	adapter := harness.drama.musicImporter

	payload := append([]byte("RIFF"), 0x00, 0x00, 0x00, 0x00)
	payload = append(payload, []byte("WAVEfmt ")...)
	stored, err := adapter.Store(ctx, "metadata.wav", payload)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	var count int
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM file_objects WHERE hash = ?`, stored.Hash).Scan(&count); err != nil {
		t.Fatalf("reading the metadata row: %v", err)
	}
	if count != 1 {
		t.Fatalf("%d metadata rows for a stored hash; the link that follows cannot succeed", count)
	}
	// And the row describes the object the store produced rather than a copy of the caller's claim.
	var size int64
	var mime string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT size_bytes, mime_type FROM file_objects WHERE hash = ?`, stored.Hash).Scan(&size, &mime); err != nil {
		t.Fatalf("reading the row's fields: %v", err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("the recorded size is %d", size)
	}
	if mime == "" {
		t.Fatal("the recorded type is empty, so the sniffer's answer was lost")
	}
}

// TestTheSnapshotStoreRecordsItsMetadataToo is the same defect at the OTHER caller, which is where it
// had been shipping unnoticed.
//
// The snapshot adapter and the music adapter share `DocumentStoring`, so the fix is one fix — and this
// asserts the sharing rather than trusting it: an adapter that fixed only its own path would leave the
// previs feature broken while its suite stayed green.
func TestTheSnapshotStoreRecordsItsMetadataToo(t *testing.T) {
	ctx := context.Background()
	harness := newMusicHarness(t)
	// The PNG signature, built rather than escaped so the file stays printable.
	signature := append([]byte{0x89}, []byte("PNG")...)
	signature = append(signature, 0x0d, 0x0a, 0x1a, 0x0a)
	signature = append(signature, []byte("previs")...)
	stored, err := harness.drama.snapshotStore.Store(ctx, "previs.png", signature)
	if err != nil {
		t.Fatalf("the snapshot store: %v", err)
	}
	var count int
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM file_objects WHERE hash = ?`, stored.Hash).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("%d metadata rows for a stored snapshot; WP-21's Link cannot succeed", count)
	}
}

// TestTheProductionAdapterRefusesABedWithoutAShot keeps the adapter from recording a usage nothing reads.
func TestTheProductionAdapterRefusesABedWithoutAShot(t *testing.T) {
	ctx := context.Background()
	harness := newMusicHarness(t)
	adapter := harness.drama.musicImporter
	// A version has to exist for the call to reach the usage step at all.
	stored, err := adapter.Store(ctx, "orphan.wav", []byte("RIFF\x00\x00\x00\x00WAVEfmt "))
	if err != nil {
		t.Fatal(err)
	}
	assetID, versionID, err := adapter.CreateBeddableAsset(ctx, harness.projectID, "orphan.wav")
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Attach(ctx, versionID, stored.Hash); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Approve(ctx, assetID, versionID); err != nil {
		t.Fatal(err)
	}
	// An empty shot id reaches the schema's foreign key or the asset service's validation, and either
	// way it must NOT produce a usage: a row with an empty consumer id is one the join can never match.
	if err := adapter.Bed(ctx, versionID, ""); err == nil {
		t.Fatal("a bed was recorded against no shot")
	}
	var count int
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_usages WHERE asset_version_id = ?`, versionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d usages were recorded for a bed with no shot", count)
	}
}
