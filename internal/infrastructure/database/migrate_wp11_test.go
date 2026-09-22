package database

import (
	"context"
	"strings"
	"testing"
)

// TestWP11MigrationFreshDatabase covers the three tables the media package depends on.
func TestWP11MigrationFreshDatabase(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, wp05HeadVersion)

	for _, table := range []string{"subtitle_tracks", "subtitle_cues", "episode_exports"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 1 {
			t.Fatalf("table %s missing", table)
		}
	}
	foreignKeysClean(t, db)
}

// TestWP11MigrationCreatesTheMediaIndexes pins the four indexes the reads need.
//
// Two of them are the approved-per-episode partial indexes, which are not an optimisation: they are
// what makes "the subtitles in force" a question with one answer, and a migration that lost one would
// let two approved tracks exist while every test that reads a single track still passed.
func TestWP11MigrationCreatesTheMediaIndexes(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{
		"idx_subtitle_tracks_episode",
		"idx_subtitle_tracks_approved",
		"idx_subtitle_cues_track",
		// The join AC-MEDIA-002's "missing line detected" makes.
		"idx_subtitle_cues_line",
		"idx_episode_exports_episode",
		"idx_episode_exports_approved",
	} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='"+index+"'") != 1 {
			t.Fatalf("index %s missing", index)
		}
	}
}

// TestWP11MediaConstraintsEnforceDocumentedValues runs the closed vocabularies and the one
// arithmetic rule against the real schema.
func TestWP11MediaConstraintsEnforceDocumentedValues(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	// The script version is what a subtitle track cites, and the parents seed writes it.
	if _, err := db.ExecContext(context.Background(), `INSERT INTO subtitle_tracks
		(id, episode_id, script_version_id, version_number, status, created_at)
		VALUES ('track-1', 'drama-episode', 'drama-script-version', 1, 'draft', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("a well-formed track was refused: %v", err)
	}
	// The version number is unique per episode, so a second track at 1 is refused rather than
	// silently shadowing the first.
	if _, err := db.ExecContext(context.Background(), `INSERT INTO subtitle_tracks
		(id, episode_id, script_version_id, version_number, status, created_at)
		VALUES ('track-2', 'drama-episode', 'drama-script-version', 1, 'draft', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("two tracks share a version number")
	}
	// The status vocabulary is the eight values the four version families use.
	if _, err := db.ExecContext(context.Background(), `INSERT INTO subtitle_tracks
		(id, episode_id, script_version_id, version_number, status, created_at)
		VALUES ('track-3', 'drama-episode', 'drama-script-version', 2, 'finished', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("an unknown track status was accepted")
	}
	// A cue needs a duration: end_ms > start_ms is a CHECK rather than a convention, because a cue
	// with no duration is invisible on screen.
	if _, err := db.ExecContext(context.Background(), `INSERT INTO subtitle_cues
		(id, track_id, ordinal, start_ms, end_ms, text, created_at)
		VALUES ('cue-1', 'track-1', 1, 1000, 1000, 'x', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a cue with no duration was accepted")
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO subtitle_cues
		(id, track_id, ordinal, start_ms, end_ms, text, created_at)
		VALUES ('cue-2', 'track-1', 1, 2000, 500, 'x', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a cue ending before it starts was accepted")
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO subtitle_cues
		(id, track_id, ordinal, start_ms, end_ms, text, created_at)
		VALUES ('cue-3', 'track-1', 1, 1000, 2000, 'a line', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("a well-formed cue was refused: %v", err)
	}
	// A negative start is a cue before the film begins.
	if _, err := db.ExecContext(context.Background(), `INSERT INTO subtitle_cues
		(id, track_id, ordinal, start_ms, end_ms, text, created_at)
		VALUES ('cue-4', 'track-1', 2, -1, 2000, 'x', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a cue starting before zero was accepted")
	}
	// The cue's status is the closed pair a regeneration reads: a cue is either the generator's or a
	// person's, and the column is what keeps the first from being replaced by a second draft.
	if _, err := db.ExecContext(context.Background(), `INSERT INTO subtitle_cues
		(id, track_id, ordinal, start_ms, end_ms, text, status, created_at)
		VALUES ('cue-5', 'track-1', 2, 2000, 3000, 'x', 'reviewed', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("an unknown cue status was accepted")
	}
	for _, status := range []string{"generated", "edited"} {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO subtitle_cues
			(id, track_id, ordinal, start_ms, end_ms, text, status, created_at)
			VALUES (?, 'track-1', ?, 2000, 3000, 'x', ?, '2026-01-01T00:00:00Z')`,
			"cue-"+status, 10+len(status), status); err != nil {
			t.Fatalf("the documented status %q was refused: %v", status, err)
		}
	}
	// And the column defaults to the generator's, so a row written without one is not a row no
	// regeneration knows about.
	if queryInt(t, db,
		"SELECT COUNT(*) FROM pragma_table_info('subtitle_cues') WHERE name='status' AND dflt_value='''generated'''") != 1 {
		t.Fatal("subtitle_cues.status does not default to 'generated'")
	}
	// An export's size and quality are closed the same way.
	if _, err := db.ExecContext(context.Background(), `INSERT INTO episode_exports
		(id, episode_id, version_number, quality, width, height, created_at)
		VALUES ('export-1', 'drama-episode', 1, 'master', 1920, 1080, '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("an unknown quality was accepted")
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO episode_exports
		(id, episode_id, version_number, quality, width, height, created_at)
		VALUES ('export-2', 'drama-episode', 1, 'final', 0, 1080, '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("an export with no width was accepted")
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO episode_exports
		(id, episode_id, version_number, quality, width, height, created_at)
		VALUES ('export-3', 'drama-episode', 1, 'final', 1080, 1920, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("a vertical export was refused: %v", err)
	}
}

// TestWP11OneApprovedTrackPerEpisode is the partial index doing its job.
//
// The same shape the four version families use: "the subtitles in force" is a question with one
// answer, and a plain unique index cannot express it because superseded versions are historical.
func TestWP11OneApprovedTrackPerEpisode(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	insert := func(id string, version int, status string) error {
		_, err := db.ExecContext(ctx, `INSERT INTO subtitle_tracks
			(id, episode_id, script_version_id, version_number, status, created_at)
			VALUES (?, 'drama-episode', 'drama-script-version', ?, ?, '2026-01-01T00:00:00Z')`,
			id, version, status)
		return err
	}
	if err := insert("track-a", 1, "approved"); err != nil {
		t.Fatal(err)
	}
	// A second approved track for the same episode is refused by the partial index.
	if err := insert("track-b", 2, "approved"); err == nil {
		t.Fatal("a second approved track was accepted for one episode")
	}
	// A superseded one is fine, and so are many of them: history is not capped.
	if err := insert("track-c", 2, "superseded"); err != nil {
		t.Fatalf("a superseded track was refused: %v", err)
	}
	if err := insert("track-d", 3, "superseded"); err != nil {
		t.Fatalf("a second superseded track was refused: %v", err)
	}
	// And the export record has the same rule.
	if _, err := db.ExecContext(ctx, `INSERT INTO episode_exports
		(id, episode_id, version_number, quality, width, height, status, created_at)
		VALUES ('export-a', 'drama-episode', 1, 'final', 1920, 1080, 'approved', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO episode_exports
		(id, episode_id, version_number, quality, width, height, status, created_at)
		VALUES ('export-b', 'drama-episode', 2, 'final', 1920, 1080, 'approved', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a second approved export was accepted for one episode")
	}
}

// TestWP11CuesCascadeWithTheirTrack proves a deleted track leaves no orphan lines.
//
// A cue without its track is a line nothing can render and nothing can delete: the same defect the
// memory tables' foreign keys prevent.
func TestWP11CuesCascadeWithTheirTrack(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO subtitle_tracks
		(id, episode_id, script_version_id, version_number, status, created_at)
		VALUES ('track-1', 'drama-episode', 'drama-script-version', 1, 'draft', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subtitle_cues
		(id, track_id, ordinal, start_ms, end_ms, text, created_at)
		VALUES ('cue-1', 'track-1', 1, 0, 1000, 'a line', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// A cue citing a track that does not exist is refused by the foreign key.
	if _, err := db.ExecContext(ctx, `INSERT INTO subtitle_cues
		(id, track_id, ordinal, start_ms, end_ms, text, created_at)
		VALUES ('cue-2', 'no-such-track', 1, 0, 1000, 'x', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a cue citing a track that does not exist was accepted")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM subtitle_tracks WHERE id = 'track-1'`); err != nil {
		t.Fatal(err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM subtitle_cues"); got != 0 {
		t.Fatalf("%d cues survived their track", got)
	}
	foreignKeysClean(t, db)
}

// TestWP11TheExportManifestIsNotTheArchiveManifest is the name collision ADR-0015 section 8 records.
//
// Two things in this repository are called a manifest and they answer different questions: the
// archive's says what bytes are in a zip, and an export's says which VERSIONS of which artifacts a
// film was made from. The test asserts the export's column holds the second, because a reader who
// confused them would look for file entries that are not there.
func TestWP11TheExportManifestIsNotTheArchiveManifest(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	manifest := `{"schemaVersion":1,"scriptVersionId":"drama-script-version","shots":[]}`
	if _, err := db.ExecContext(context.Background(), `INSERT INTO episode_exports
		(id, episode_id, version_number, quality, width, height, manifest_json, created_at)
		VALUES ('export-1', 'drama-episode', 1, 'preview', 640, 360, ?, '2026-01-01T00:00:00Z')`,
		manifest); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(`SELECT manifest_json FROM episode_exports WHERE id = 'export-1'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != manifest {
		t.Fatalf("the manifest did not round trip: %q", stored)
	}
	// It names a VERSION rather than a file, which is what makes traceability possible.
	if !containsAll(stored, "scriptVersionId", "schemaVersion") {
		t.Fatalf("the manifest does not carry version references: %q", stored)
	}
}

// containsAll reports whether every fragment appears in a string.
func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
