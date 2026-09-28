package database

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// migration_000027_test.go is the data-repair half of the 2026-09-26 audit's
// T01: databases written by the old collector hold ONE project-wide audio
// asset whose versions carry several lines' usages, and the newest collection
// had silenced the others. New collections are isolated; this migration splits
// the legacy row set so every line's usage names a version of its own asset
// again.

// applyMigration27 replays the split migration's statements over a database
// whose schema already carries every migration through 000027.
//
// It reads the .sql file rather than restating it, so a future edit to the
// migration changes what this walk drives — the same rule the runner's
// checksum enforces.
func applyMigration27(ctx context.Context, db *sql.DB) error {
	// Comments are stripped first because a "--" note may carry a semicolon,
	// and splitting raw text would cut a statement mid-note.
	source := migration27Source()
	lines := strings.Split(source, "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		clean = append(clean, line)
	}
	joined := strings.Join(clean, "\n")
	for _, statement := range strings.Split(joined, ";") {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, trimmed); err != nil {
			return err
		}
	}
	return nil
}

// TestMigrationSplitsSharedAudioAsset seeds the legacy shape the old
// collector wrote — one audio asset, two versions (one per line), the second
// approved and shadowing the first — and asserts the audit's outcome: both
// lines' usages name approved versions, on distinct assets, through the mix's
// own filter.
func TestMigrationSplitsSharedAudioAsset(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	db := harness.db

	// The legacy shape: one shared asset, two versions each naming its job,
	// two usages (one per shot) hanging off those versions. Version 2 is the
	// asset's approved one — which is what silenced line 1.
	if _, err := db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES ('shared-audio', 'drama-project', 'audio', 'Line speech', 'shared-v2', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id, jobID, status, number string
	}{
		{"shared-v1", "job-line-1", "superseded", "1"},
		{"shared-v2", "job-line-2", "approved", "2"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO asset_versions
			(id, asset_id, version_number, status, prompt, generation_job_id, created_by_type, created_at)
			VALUES (?, 'shared-audio', ?, ?, 'hello', ?, 'system', '2026-01-01T00:00:00Z')`,
			row.id, row.number, row.status, row.jobID); err != nil {
			t.Fatalf("seeding %s: %v", row.id, err)
		}
	}
	for _, usage := range []struct{ id, versionID, consumerID string }{
		{"usage-line-1", "shared-v1", "shot-line-1"},
		{"usage-line-2", "shared-v2", "shot-line-2"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO asset_usages
			(id, asset_version_id, consumer_type, consumer_id, usage_role, required, created_at)
			VALUES (?, ?, 'shot', ?, 'audio_dialogue', 0, '2026-01-01T00:00:00Z')`,
			usage.id, usage.versionID, usage.consumerID); err != nil {
			t.Fatalf("seeding the usage %s: %v", usage.id, err)
		}
	}

	// The mix's own filter, BEFORE the split: line 1's version is superseded,
	// so the join that requires
	// `au.asset_version_id = aa.current_approved_version_id` returns only
	// line 2.
	if clips := approvedAudioUsageCount(t, db, "shot-line-1"); clips != 0 {
		t.Fatalf("the legacy shape mixes %d clips for line 1 before the split; the defect is not reproduced", clips)
	}
	if clips := approvedAudioUsageCount(t, db, "shot-line-2"); clips != 1 {
		t.Fatalf("the legacy shape mixes %d clips for line 2 before the split; the seed is wrong", clips)
	}

	// The repair, driven through the migration's own statements.
	if err := applyMigration27(ctx, db); err != nil {
		t.Fatalf("applying the split: %v", err)
	}

	if clips := approvedAudioUsageCount(t, db, "shot-line-1"); clips != 1 {
		t.Fatalf("line 1 mixes %d clips after the split; the silenced line is still silenced", clips)
	}
	if clips := approvedAudioUsageCount(t, db, "shot-line-2"); clips != 1 {
		t.Fatalf("line 2 mixes %d clips after the split; the repair moved the wrong rows", clips)
	}
	// The two lines' usages name versions of DISTINCT assets now.
	rows, err := db.QueryContext(ctx, `SELECT v.asset_id FROM asset_usages au
		JOIN asset_versions v ON v.id = au.asset_version_id
		WHERE au.consumer_type = 'shot'
		  AND au.consumer_id IN ('shot-line-1', 'shot-line-2')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	assets := map[string]int{}
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			t.Fatal(err)
		}
		assets[assetID]++
	}
	if len(assets) != 2 {
		t.Fatalf("the two lines' usages sit on %d assets after the split; the split must separate them", len(assets))
	}
}

// TestMigrationLeavesIsolatedAssetsAlone proves the split touches only the
// ambiguous row sets: an audio asset whose versions carry ONE consumer's
// usages keeps its identity, its version ids and its approval pointer.
func TestMigrationLeavesIsolatedAssetsAlone(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	db := harness.db

	if _, err := db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES ('single-audio', 'drama-project', 'audio', 'speech:line-A', 'single-v1', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO asset_versions
		(id, asset_id, version_number, status, prompt, generation_job_id, created_by_type, created_at)
		VALUES ('single-v1', 'single-audio', 1, 'approved', 'hello', 'job-single', 'system', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO asset_usages
		(id, asset_version_id, consumer_type, consumer_id, usage_role, required, created_at)
		VALUES ('usage-single', 'single-v1', 'shot', 'shot-single', 'audio_dialogue', 0, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := applyMigration27(ctx, db); err != nil {
		t.Fatalf("applying the split: %v", err)
	}
	var name, approved string
	if err := db.QueryRowContext(ctx,
		`SELECT name, current_approved_version_id FROM assets WHERE id = 'single-audio'`).Scan(&name, &approved); err != nil {
		t.Fatal(err)
	}
	if name != "speech:line-A" || approved != "single-v1" {
		t.Fatalf("an isolated asset was disturbed (name %q, approval %q); the split must leave it alone", name, approved)
	}
}

// approvedAudioUsageCount counts the audio clips the mix's join returns for one
// shot — the same filter `attachAudioClips` applies, stated directly so the
// test asserts the read that decides what a user hears.
func approvedAudioUsageCount(t *testing.T, db *sql.DB, shotID string) int {
	t.Helper()
	ctx := context.Background()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM asset_usages au
		JOIN assets aa ON aa.id = (SELECT asset_id FROM asset_versions WHERE id = au.asset_version_id)
		WHERE au.consumer_type = 'shot' AND au.consumer_id = ?
		  AND aa.asset_type = 'audio'
		  AND au.asset_version_id = aa.current_approved_version_id`, shotID).Scan(&count); err != nil {
		t.Fatalf("reading the mix's audio for %s: %v", shotID, err)
	}
	return count
}

// migration27Source reads the split migration's own text, so the walk drives
// exactly the statements the runner will apply.
func migration27Source() string {
	data, err := embeddedMigrations.ReadFile("migrations/000027_audio_asset_isolation.sql")
	if err != nil {
		return ""
	}
	return string(data)
}
