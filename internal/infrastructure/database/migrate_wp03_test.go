package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func jobsSQL(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("migrations", "000003_jobs.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// wp03Migrations is the full migration set through version 3.
func wp03Migrations(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"000001_foundation.sql":        {Data: foundationSQL(t)},
		"000002_provider_security.sql": {Data: providerSecuritySQL(t)},
		"000003_jobs.sql":              {Data: jobsSQL(t)},
	}
}

func TestWP03MigrationFreshDatabase(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp03Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, 3)
	for _, table := range []string{"generation_jobs", "job_attempts", "job_dependencies"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 1 {
			t.Fatalf("table %s missing", table)
		}
	}
	// Indexes declared by DOMAIN_MODEL §18.
	for _, index := range []string{"idx_generation_jobs_schedule", "idx_generation_jobs_entity", "idx_job_attempts_job"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='"+index+"'") != 1 {
			t.Fatalf("index %s missing", index)
		}
	}
}

// TestWP03MigrationPreservesExistingRows is the critical upgrade test: v2
// databases may already hold provider configs and request rows, and the
// CHECK-widening rebuild must copy them without loss.
func TestWP03MigrationPreservesExistingRows(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")
	snapshotDir := filepath.Join(root, "snapshots")
	nowFn := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

	handle, err := open(context.Background(), dbPath, snapshotDir, wp02Migrations(t), nowFn)
	if err != nil {
		t.Fatal(err)
	}
	db := handle.SQL()
	ctx := context.Background()

	// Seed a realistic v2 state: a secret ref, a provider config, provider
	// configs for each existing kind, and a redacted request record.
	if _, err := db.ExecContext(ctx, `INSERT INTO secret_references (id, provider_id, secret_kind, display_hint, status, created_at, updated_at)
		VALUES ('InfiniteAtelier:provider:relay-a', 'relay-a', 'api_key', '****abcd', 'configured', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, local_approved, enabled, revision, created_at, updated_at)
		VALUES ('relay-a', 'openai_compatible', 'Relay A', 'https://relay.example.com/v1', 'InfiniteAtelier:provider:relay-a', 1, 1, 4, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_requests (id, provider_id, capability, model, status, http_status, latency_ms, request_id, error_code, input_units, output_units, estimated_cost, created_at)
		VALUES ('req-1', 'relay-a', 'text', 'gpt-test', 'succeeded', 200, 120, 'resp-1', '', 10, 20, '0.001', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(ctx); err != nil {
		t.Fatal(err)
	}

	// Upgrade to v3 through the production open path.
	upgraded, err := open(ctx, dbPath, snapshotDir, wp03Migrations(t), nowFn)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close(ctx)
	if upgraded.Mode() != ModeReady {
		t.Fatalf("upgrade not ready: %v", upgraded.Err())
	}
	if upgraded.SnapshotPath() == "" {
		t.Fatal("no pre-migration snapshot for v2→v3")
	}
	assertUserVersion(t, upgraded.SQL(), 3)

	// Existing rows survive the table rebuilds, including their values.
	var (
		displayName, baseURL, secretRef string
		revision                        int
		localApproved, enabled          int
	)
	err = upgraded.SQL().QueryRowContext(ctx, `SELECT display_name, base_url, secret_ref, revision, local_approved, enabled
		FROM provider_configs WHERE id = 'relay-a'`).
		Scan(&displayName, &baseURL, &secretRef, &revision, &localApproved, &enabled)
	if err != nil {
		t.Fatalf("provider_configs row lost in rebuild: %v", err)
	}
	if displayName != "Relay A" || baseURL != "https://relay.example.com/v1" || revision != 4 || localApproved != 1 || enabled != 1 {
		t.Fatalf("provider_configs values changed: %s %s %d %d %d", displayName, baseURL, revision, localApproved, enabled)
	}
	if secretRef != "InfiniteAtelier:provider:relay-a" {
		t.Fatalf("secret_ref changed: %q", secretRef)
	}

	var model, requestID, cost string
	var httpStatus, inputUnits, outputUnits int64
	err = upgraded.SQL().QueryRowContext(ctx, `SELECT model, request_id, estimated_cost, http_status, input_units, output_units
		FROM provider_requests WHERE id = 'req-1'`).
		Scan(&model, &requestID, &cost, &httpStatus, &inputUnits, &outputUnits)
	if err != nil {
		t.Fatalf("provider_requests row lost in rebuild: %v", err)
	}
	if model != "gpt-test" || requestID != "resp-1" || cost != "0.001" || httpStatus != 200 || inputUnits != 10 || outputUnits != 20 {
		t.Fatalf("provider_requests values changed: %s %s %s %d %d %d", model, requestID, cost, httpStatus, inputUnits, outputUnits)
	}

	// The foreign key from provider_configs to secret_references survives the
	// rebuild.
	if _, err := upgraded.SQL().ExecContext(ctx, `INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, revision, created_at, updated_at)
		VALUES ('orphan', 'openai_compatible', 'Orphan', 'https://a.example.com', 'missing-ref', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("FK not enforced after provider_configs rebuild")
	}
}

// TestWP03MigrationWidensCapabilityAndKind proves the rebuilt CHECK constraints
// accept the new values while still rejecting unknown ones.
func TestWP03MigrationWidensCapabilityAndKind(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp03Migrations(t)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO secret_references (id, provider_id, secret_kind, status, created_at, updated_at)
		VALUES ('ref-g', 'gemini-1', 'api_key', 'configured', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// New kind accepted.
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, revision, created_at, updated_at)
		VALUES ('gemini-1', 'gemini_compatible', 'Gemini', 'https://generativelanguage.googleapis.com', 'ref-g', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("gemini_compatible kind rejected: %v", err)
	}
	// Unknown kind still rejected.
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, revision, created_at, updated_at)
		VALUES ('bad', 'anthropic', 'Bad', 'https://a.example.com', 'ref-g', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("unknown provider kind accepted")
	}

	// New capabilities accepted.
	for _, capability := range []string{"image", "video", "audio", "embedding"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO provider_requests (id, provider_config_id, capability, model, status, latency_ms, created_at)
			VALUES (?, 'gemini-1', ?, 'm', 'succeeded', 1, '2026-01-01T00:00:00Z')`, "req-"+capability, capability); err != nil {
			t.Fatalf("capability %s rejected: %v", capability, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_requests (id, provider_config_id, capability, model, status, latency_ms, created_at)
		VALUES ('req-bad', 'gemini-1', 'telepathy', 'm', 'succeeded', 1, '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("unknown capability accepted")
	}
}

// TestWP03JobStatusConstraint proves every documented status is accepted and
// that PRD's awaiting_remote spelling is NOT a stored value (ADR-0004).
func TestWP03JobStatusConstraint(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp03Migrations(t)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	accepted := []string{
		"queued", "running", "waiting_remote", "downloading", "verifying",
		"retry_wait", "succeeded", "remote_only", "failed", "cancelled",
		"orphaned", "recovering",
	}
	for index, status := range accepted {
		if _, err := db.ExecContext(ctx, `INSERT INTO generation_jobs (id, job_type, status, idempotency_key, created_at, updated_at)
			VALUES (?, 'image_generation', ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			"job-"+string(rune('a'+index)), status, "key-"+status); err != nil {
			t.Fatalf("status %s rejected: %v", status, err)
		}
	}
	for _, status := range []string{"awaiting_remote", "executed", "pending", "done"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO generation_jobs (id, job_type, status, idempotency_key, created_at, updated_at)
			VALUES (?, 'image_generation', ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			"bad-"+status, status, "key-bad-"+status); err == nil {
			t.Fatalf("undocumented status %s accepted", status)
		}
	}
}

// TestWP03JobConstraints covers idempotency scope, progress range, dependency
// conditions, and the attempt FK.
func TestWP03JobConstraints(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp03Migrations(t)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	insertJob := func(id, projectID, key string) error {
		_, err := db.ExecContext(ctx, `INSERT INTO generation_jobs (id, project_id, job_type, status, idempotency_key, created_at, updated_at)
			VALUES (?, ?, 'image_generation', 'queued', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, id, projectID, key)
		return err
	}

	if err := insertJob("job-1", "proj-a", "same-key"); err != nil {
		t.Fatal(err)
	}
	// Same key, same project: rejected.
	if err := insertJob("job-2", "proj-a", "same-key"); err == nil {
		t.Fatal("duplicate idempotency key accepted within a project")
	}
	// Same key, different project: allowed (scope is per project).
	if err := insertJob("job-3", "proj-b", "same-key"); err != nil {
		t.Fatalf("idempotency key rejected across projects: %v", err)
	}
	// Jobs with no project share the global scope.
	if err := insertJob("job-4", "", "global-key"); err != nil {
		t.Fatal(err)
	}
	if err := insertJob("job-5", "", "global-key"); err == nil {
		t.Fatal("duplicate global idempotency key accepted")
	}

	// Progress range.
	if _, err := db.ExecContext(ctx, `UPDATE generation_jobs SET progress = 101 WHERE id = 'job-1'`); err == nil {
		t.Fatal("progress > 100 accepted")
	}
	if _, err := db.ExecContext(ctx, `UPDATE generation_jobs SET progress = -1 WHERE id = 'job-1'`); err == nil {
		t.Fatal("negative progress accepted")
	}
	if _, err := db.ExecContext(ctx, `UPDATE generation_jobs SET progress = 50 WHERE id = 'job-1'`); err != nil {
		t.Fatalf("valid progress rejected: %v", err)
	}

	// Dependency conditions.
	if _, err := db.ExecContext(ctx, `INSERT INTO job_dependencies (job_id, depends_on_job_id, condition) VALUES ('job-1', 'job-3', 'success')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_dependencies (job_id, depends_on_job_id, condition) VALUES ('job-1', 'job-3', 'approved')`); err == nil {
		t.Fatal("duplicate dependency row accepted")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_dependencies (job_id, depends_on_job_id, condition) VALUES ('job-3', 'job-1', 'whenever')`); err == nil {
		t.Fatal("unknown dependency condition accepted")
	}

	// Attempt FK + numbering uniqueness.
	if _, err := db.ExecContext(ctx, `INSERT INTO job_attempts (id, generation_job_id, attempt_number, status, started_at) VALUES ('att-1', 'job-1', 1, 'running', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_attempts (id, generation_job_id, attempt_number, status, started_at) VALUES ('att-2', 'job-1', 1, 'failed', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("duplicate attempt number accepted")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_attempts (id, generation_job_id, attempt_number, status, started_at) VALUES ('att-3', 'missing-job', 1, 'running', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("attempt for unknown job accepted")
	}
}

// TestWP03MigrationFailureRollsBack proves a failing v3 leaves the database at
// version 2 with no half-created job tables.
func TestWP03MigrationFailureRollsBack(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp02Migrations(t)); err != nil {
		t.Fatal(err)
	}
	broken := append(append([]byte{}, jobsSQL(t)...), []byte("SELECT RAISE(ABORT, 'boom');")...)
	failing := fstest.MapFS{
		"000001_foundation.sql":        {Data: foundationSQL(t)},
		"000002_provider_security.sql": {Data: providerSecuritySQL(t)},
		"000003_jobs.sql":              {Data: broken},
	}
	if err := applyMigrations(context.Background(), db, failing); err == nil {
		t.Fatal("broken v3 succeeded")
	}
	assertUserVersion(t, db, 2)
	if queryInt(t, db, "SELECT COUNT(*) FROM schema_migrations") != 2 {
		t.Fatal("v3 recorded after rollback")
	}
	for _, table := range []string{"generation_jobs", "job_attempts", "job_dependencies"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 0 {
			t.Fatalf("rolled-back table %s remains", table)
		}
	}
	// The provider tables must still exist under their original names.
	for _, table := range []string{"provider_configs", "provider_requests"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 1 {
			t.Fatalf("table %s was disturbed by the failed migration", table)
		}
	}
}
