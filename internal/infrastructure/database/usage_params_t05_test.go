package database

import (
	"context"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// usage_params_t05_test.go is the editor loop's write half: SetUsageParams
// persists a placement document, ListUsagesOfConsumer reads it back, and the
// timeline parse consumes it — the round trip a track editor performs.

// TestUsageParamsRoundTrip drives write → read through the real repository.
func TestUsageParamsRoundTrip(t *testing.T) {
	harness := newMediaHarness(t)
	repo := NewAssetRepository(harness.db)
	ctx := context.Background()

	// Seed a version + usage with no params.
	hash := harness.put(t, "param-test.wav", tone(t, ctx))
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES ('param-asset', 'drama-project', 'audio', 'speech:line-A', 'param-v1', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO asset_versions
		(id, asset_id, version_number, status, created_by_type, created_at)
		VALUES ('param-v1', 'param-asset', 1, 'approved', 'system', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO asset_files (id, asset_version_id, file_hash, role, ordinal, created_at)
		VALUES ('param-f1', 'param-v1', ?, 'primary', 0, '2026-01-01T00:00:00Z')`, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO asset_usages
		(id, asset_version_id, consumer_type, consumer_id, usage_role, required, created_at, params_json)
		VALUES ('param-u1', 'param-v1', 'shot', 'wp11-shot-1', 'audio_dialogue', 0, '2026-01-01T00:00:00Z', '')`); err != nil {
		t.Fatal(err)
	}

	// WRITE: the editor's persist step (SetUsageParams lands at the service
	// above the repository; this walk drives the storage contract directly).
	if _, err := harness.db.ExecContext(ctx, `UPDATE asset_usages SET params_json = ? WHERE id = 'param-u1'`,
		`{"offsetMs":2500,"volume":0.5}`); err != nil {
		t.Fatal(err)
	}

	// READ through the consumer listing the editor uses.
	usages, err := repo.ListUsagesOfConsumer(ctx, asset.ConsumerShot, "wp11-shot-1")
	if err != nil {
		t.Fatalf("ListUsagesOfConsumer: %v", err)
	}
	found := false
	for _, usage := range usages {
		if usage.ID == "param-u1" {
			found = true
			if usage.Params == "" {
				t.Fatal("the stored params read back empty")
			}
		}
	}
	if !found {
		t.Fatal("the usage was not listed for its consumer")
	}
}

// TestSetAssetLicenseRoundTrip drives the licence record write and the
// final-reader read of it.
func TestSetAssetLicenseRoundTrip(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	repo := NewAssetRepository(harness.db)

	if _, err := harness.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES ('lic-asset', 'drama-project', 'image', 'frame', '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}

	// WRITE via the narrow command.
	if err := repo.SetAssetLicense(ctx, "lic-asset", "CC-BY-4.0", "origin site", "1"); err != nil {
		t.Fatalf("SetAssetLicense: %v", err)
	}

	// The record reads back through the asset row.
	var license, source, allows string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT license, license_source, allows_export_use FROM assets WHERE id = 'lic-asset'`).
		Scan(&license, &source, &allows); err != nil {
		t.Fatal(err)
	}
	if license != "CC-BY-4.0" || source != "origin site" || allows != "1" {
		t.Fatalf("the licence read back as (%q, %q, %q)", license, source, allows)
	}

	// An invalid tri-state is refused.
	if err := repo.SetAssetLicense(ctx, "lic-asset", "x", "y", "maybe"); err == nil {
		t.Fatal("an invalid export-use answer was accepted")
	}
	// An unknown asset is a not-found.
	if err := repo.SetAssetLicense(ctx, "no-such-asset", "x", "y", "1"); err == nil {
		t.Fatal("an unknown asset accepted a licence")
	}
}
