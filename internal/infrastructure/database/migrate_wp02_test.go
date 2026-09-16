package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func providerSecuritySQL(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("migrations", "000002_provider_security.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// wp02Migrations is the WP-02 migration set through version 2. It is kept
// verbatim so the v1-to-v2 upgrade path stays covered as a historical case.
func wp02Migrations(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"000001_foundation.sql":        {Data: foundationSQL(t)},
		"000002_provider_security.sql": {Data: providerSecuritySQL(t)},
	}
}

// TestWP02MigrationFreshDatabase proves a new database reaches schema version
// 2 with the provider tables created and constraints active.
func TestWP02MigrationFreshDatabase(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp03Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, headMigrationVersion(t, wp03Migrations(t)))
	for _, table := range []string{"secret_references", "provider_configs", "provider_requests"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 1 {
			t.Fatalf("table %s missing", table)
		}
	}
}

// TestWP02MigrationUpgradesVersionOne proves an existing v1 foundation
// database upgrades in place to v2 without touching released 000001 content.
func TestWP02MigrationUpgradesVersionOne(t *testing.T) {
	db := openTempDB(t)
	v1 := fstest.MapFS{"000001_foundation.sql": {Data: foundationSQL(t)}}
	if err := applyMigrations(context.Background(), db, v1); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, 1)
	if err := applyMigrations(context.Background(), db, wp03Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, headMigrationVersion(t, wp03Migrations(t)))
	assertApplied(t, db, 1, checksumOf(foundationSQL(t)))
	assertApplied(t, db, 2, checksumOf(providerSecuritySQL(t)))
	// Foundation tables intact.
	if queryInt(t, db, "SELECT COUNT(*) FROM file_objects") != 0 {
		t.Fatal("file_objects disturbed")
	}
}

// TestWP02MigrationFailureRollsBack proves a failing v2 leaves the database
// at version 1 with no half-created provider tables (fixture: tampered SQL).
func TestWP02MigrationFailureRollsBack(t *testing.T) {
	db := openTempDB(t)
	v1 := fstest.MapFS{"000001_foundation.sql": {Data: foundationSQL(t)}}
	if err := applyMigrations(context.Background(), db, v1); err != nil {
		t.Fatal(err)
	}
	broken := append(append([]byte{}, providerSecuritySQL(t)...), []byte("SELECT RAISE(ABORT, 'boom');")...)
	failing := fstest.MapFS{
		"000001_foundation.sql":        {Data: foundationSQL(t)},
		"000002_provider_security.sql": {Data: broken},
	}
	if err := applyMigrations(context.Background(), db, failing); err == nil {
		t.Fatal("broken v2 succeeded")
	}
	assertUserVersion(t, db, 1)
	if queryInt(t, db, "SELECT COUNT(*) FROM schema_migrations") != 1 {
		t.Fatal("v2 recorded after rollback")
	}
	for _, table := range []string{"secret_references", "provider_configs", "provider_requests"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 0 {
			t.Fatalf("rolled-back table %s remains", table)
		}
	}
}

// TestWP02SchemaConstraints proves the security invariants: no secret value
// column exists, FK from provider_configs to secret_references is enforced,
// and CHECK constraints reject bad rows.
func TestWP02SchemaConstraints(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp03Migrations(t)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// No column in provider tables may be named like a secret value storage.
	for _, table := range []string{"secret_references", "provider_configs", "provider_requests"} {
		rows, err := db.QueryContext(ctx, "SELECT name FROM pragma_table_info('"+table+"')")
		if err != nil {
			t.Fatal(err)
		}
		var columns []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			columns = append(columns, name)
		}
		rows.Close()
		for _, column := range columns {
			switch column {
			case "secret_value", "api_key", "value", "credential":
				t.Fatalf("table %s must not have a value column, found %q", table, column)
			}
		}
	}

	// FK: provider_configs.secret_ref must reference an existing secret_references row.
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, revision, created_at, updated_at)
		VALUES ('p1', 'openai_compatible', 'x', 'https://a.example.com', 'missing-ref', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("FK not enforced for secret_ref")
	}

	// Valid chain works.
	if _, err := db.ExecContext(ctx, `INSERT INTO secret_references (id, provider_id, secret_kind, status, created_at, updated_at)
		VALUES ('InfiniteAtelier:provider:p1', 'p1', 'api_key', 'configured', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, revision, created_at, updated_at)
		VALUES ('p1', 'openai_compatible', 'x', 'https://a.example.com', 'InfiniteAtelier:provider:p1', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	// CHECK: an unknown request status is rejected. (Capability values were
	// widened from text-only to the multimodal set by migration 000003; that
	// change is asserted in migrate_wp03_test.go, so this test keeps only the
	// still-invalid case.)
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_requests (id, provider_config_id, capability, model, status, latency_ms, created_at)
		VALUES ('r2', 'p1', 'text', 'm', 'weird', 1, '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("unknown request status accepted")
	}

	// Deleting a referenced secret row is restricted.
	if _, err := db.ExecContext(ctx, `DELETE FROM secret_references WHERE provider_id='p1'`); err == nil {
		t.Fatal("ON DELETE RESTRICT not enforced")
	}
}

// TestWP02OpenSnapshotBeforeUpgrade proves the production Open path creates
// a pre-migration snapshot when upgrading a v1 database with data.
func TestWP02OpenSnapshotBeforeUpgrade(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")
	snapshotDir := filepath.Join(root, "snapshots")
	fixedNow := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	nowFn := func() time.Time { return fixedNow }

	// Create a v1 database with real content through the production path.
	v1Handle, err := open(context.Background(), dbPath, snapshotDir, fstest.MapFS{"000001_foundation.sql": {Data: foundationSQL(t)}}, nowFn)
	if err != nil {
		t.Fatal(err)
	}
	db := v1Handle.SQL()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
		VALUES ('a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091', 'a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091', 'image/png', 10, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := v1Handle.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Now open with the full WP-02 migration set: must snapshot, then upgrade.
	handle, err := open(context.Background(), dbPath, snapshotDir, wp03Migrations(t), nowFn)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close(context.Background())
	if handle.Mode() != ModeReady {
		t.Fatalf("mode = %q, want ready", handle.Mode())
	}
	if handle.SnapshotPath() == "" {
		t.Fatal("pre-migration snapshot not created for v1→v2 upgrade")
	}
	if _, err := os.Stat(handle.SnapshotPath()); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}
	assertUserVersion(t, handle.SQL(), headMigrationVersion(t, wp03Migrations(t)))
	if queryInt(t, handle.SQL(), "SELECT COUNT(*) FROM file_objects") != 1 {
		t.Fatal("existing data lost during upgrade")
	}
}
