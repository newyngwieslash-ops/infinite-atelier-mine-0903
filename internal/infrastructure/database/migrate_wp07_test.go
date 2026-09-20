package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// These tests cover the two things migration 000015 does to an existing table:
// it reconciles a state the old writer allowed and then turns the invariant into a
// constraint. The reconciliation is the part that could silently lose information,
// so it is tested against a database that really holds the bad state.

// openAt14 opens a database migrated only through version 14, which is the state a
// database written by WP-05/WP-06 is in before this migration runs.
func openAt14(t *testing.T) *sql.DB {
	t.Helper()
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"),
		filepath.Join(t.TempDir(), "snapshots"), wp05UpTo(t, 14), time.Now)
	if err != nil {
		t.Fatalf("opening a version-14 database: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })
	if handle.SQL() == nil {
		t.Fatalf("the version-14 database did not open: %v", handle.Mode())
	}
	return handle.SQL()
}

// seedRunForReconciliation writes the parents a stage run needs, then the stages
// the caller asks for.
func seedRunForReconciliation(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO workspaces (id, name, created_at, updated_at)
		 VALUES ('ws-1', 'Workspace', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO projects (id, workspace_id, project_type, name, status, created_at, updated_at)
		 VALUES ('project-1', 'ws-1', 'drama', 'Drama', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO workflow_runs (id, project_id, workflow_type, status, created_at, updated_at)
		 VALUES ('run-1', 'project-1', 'episode_production', 'running', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the reconciliation fixture failed: %v\n%s", err, statement)
		}
	}
}

// TestWP07MigrationReconcilesMultipleActiveAttempts covers the state the old
// writer allowed: WP-05 wrote no check for "at most one active attempt", so a
// database can hold two. The migration must close the older one and keep the
// newest, and it must say why in the row.
func TestWP07MigrationReconcilesMultipleActiveAttempts(t *testing.T) {
	db := openAt14(t)
	ctx := context.Background()
	seedRunForReconciliation(t, db)

	// Two active attempts for one (run, stage), which the old schema permitted.
	for _, attempt := range []struct {
		id     string
		number int
		status string
	}{
		{"stage-a1", 1, "reviewing"},
		{"stage-a2", 2, "running"},
		// A third stage, untouched, to prove the statement is not blanket.
		{"stage-b1", 1, "pending"},
	} {
		statement := `INSERT INTO stage_runs (id, workflow_run_id, stage, attempt, status, created_at)
			VALUES (?, 'run-1', ?, ?, ?, '2026-01-01T00:00:00Z')`
		name := "s"
		if attempt.id == "stage-b1" {
			name = "other"
		}
		if _, err := db.ExecContext(ctx, statement, attempt.id, name, attempt.number, attempt.status); err != nil {
			t.Fatalf("seeding %s: %v", attempt.id, err)
		}
	}

	// Apply migration 15 alone, the way the runner applies it.
	fragment, err := applyMigrationFileSplits(ctx, db, string(migrationSQL(t, "000015_agent_runtime.sql")))
	if err != nil {
		t.Fatalf("migration 000015 failed at %q: %v", fragment, err)
	}

	// The newest active attempt survives, still active.
	var status, errorCode string
	if err := db.QueryRowContext(ctx, `SELECT status, error_code FROM stage_runs WHERE id = 'stage-a2'`).Scan(&status, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status != "running" || errorCode != "" {
		t.Fatalf("the newest attempt is %q/%q, want it untouched", status, errorCode)
	}
	// The older one is closed, and the row says which migration did it.
	if err := db.QueryRowContext(ctx, `SELECT status, error_code FROM stage_runs WHERE id = 'stage-a1'`).Scan(&status, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("the superseded attempt is %q, want failed", status)
	}
	if errorCode != "agent.multiple_active_attempts_reconciled" {
		t.Fatalf("the closed attempt records %q, so a reader cannot tell why it closed", errorCode)
	}
	// Its finish time is set, because the domain refuses a finished run and a
	// stage without one is a row nothing can reason about.
	var finishedAt string
	if err := db.QueryRowContext(ctx, `SELECT finished_at FROM stage_runs WHERE id = 'stage-a1'`).Scan(&finishedAt); err != nil {
		t.Fatal(err)
	}
	if finishedAt == "" {
		t.Fatal("the closed attempt records no finish time")
	}
	// The unrelated stage is untouched.
	if err := db.QueryRowContext(ctx, `SELECT status FROM stage_runs WHERE id = 'stage-b1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("an unrelated stage was changed to %q", status)
	}
}

// TestWP07MigrationEnforcesOneActiveAttempt covers the constraint itself, which
// is what turns migration 000011's comment into something the database refuses to
// violate.
func TestWP07MigrationEnforcesOneActiveAttempt(t *testing.T) {
	db := openAt14(t)
	ctx := context.Background()
	seedRunForReconciliation(t, db)
	if fragment, err := applyMigrationFileSplits(ctx, db, string(migrationSQL(t, "000015_agent_runtime.sql"))); err != nil {
		t.Fatalf("migration 000015 failed at %q: %v", fragment, err)
	}
	insert := func(id, stage string, attempt int, status string) error {
		_, err := db.ExecContext(ctx,
			`INSERT INTO stage_runs (id, workflow_run_id, stage, attempt, status, created_at)
			 VALUES (?, 'run-1', ?, ?, ?, '2026-01-01T00:00:00Z')`, id, stage, attempt, status)
		return err
	}
	// A finished attempt, then one active attempt: allowed.
	if err := insert("s1", "story_skeleton", 1, "passed"); err != nil {
		t.Fatalf("a finished attempt must be insertable: %v", err)
	}
	if err := insert("s2", "story_skeleton", 2, "running"); err != nil {
		t.Fatalf("one active attempt must be insertable: %v", err)
	}
	// A second active attempt: refused by the index, not by a comment.
	err := insert("s3", "story_skeleton", 3, "pending")
	if err == nil {
		t.Fatal("a second active attempt was accepted, so the invariant is not a constraint")
	}
	if !isUniqueViolation(err) {
		t.Fatalf("the refusal is %v, which is not the unique-constraint failure the writer maps to a conflict", err)
	}
	// Once the active attempt is terminal, a new attempt is allowed: attempts stay
	// historical.
	if _, err := db.ExecContext(ctx, `UPDATE stage_runs SET status = 'passed' WHERE id = 's2'`); err != nil {
		t.Fatal(err)
	}
	if err := insert("s4", "story_skeleton", 4, "pending"); err != nil {
		t.Fatalf("a new attempt after the previous one finished must be allowed: %v", err)
	}
	// A different stage in the same run is unaffected.
	if err := insert("t1", "adaptation_strategy", 1, "running"); err != nil {
		t.Fatalf("another stage must be insertable: %v", err)
	}
	// And a different run is unaffected.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO workflow_runs (id, project_id, workflow_type, status, created_at, updated_at)
		 VALUES ('run-2', 'project-1', 'episode_production', 'running', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO stage_runs (id, workflow_run_id, stage, attempt, status, created_at)
		 VALUES ('u1', 'run-2', 'story_skeleton', 1, 'running', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("the same stage in another run must be insertable: %v", err)
	}
}

// TestWP07AgentTablesRoundTrip covers the four new tables against real SQL: the
// agent-equivalent of the repository round-trip tests, done here because the
// repository itself does not exist yet in this checkpoint.
func TestWP07AgentTablesRoundTrip(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO skill_versions
		(id, skill_key, version, content_hash, manifest_json, content_file_id, status, created_at)
		VALUES ('sv-1', 'script', '1.0.0', ?, '{"apiVersion":"atelier.agent/v1"}', ?, 'active', '2026-01-01T00:00:00Z')`,
		hashOf("a"), hashOf("b")); err != nil {
		t.Fatalf("inserting a skill version: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_runs
		(id, project_id, workflow_run_id, stage_run_id, agent_layer, agent_key, skill_version_id, status, started_at, created_at, updated_at)
		VALUES ('ar-1', 'drama-project', 'drama-run', '', 'execution', 'script.execution.story_skeleton', 'sv-1', 'running', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("inserting an agent run: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_messages
		(id, agent_run_id, scope_key, scope_project, scope_agent_key, role, content, content_hash, created_at)
		VALUES ('am-1', 'ar-1', 'local|ws|drama-project||script.execution.story_skeleton|', 'drama-project', 'script.execution.story_skeleton', 'user', 'hello', ?, '2026-01-01T00:00:00Z')`,
		hashOf("c")); err != nil {
		t.Fatalf("inserting a message: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_tool_calls
		(id, agent_run_id, sequence, tool_key, input_json, output_json, status, created_at)
		VALUES ('at-1', 'ar-1', 0, 'story.read_events', '{}', '{"events":[]}', 'succeeded', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("inserting a tool call: %v", err)
	}

	// The constraints that carry a rule are worth asserting, because a constraint
	// that does not fire is the same as no constraint.
	// 1. An agent run must name a skill version that exists.
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_runs
		(id, project_id, agent_layer, agent_key, skill_version_id, status, created_at, updated_at)
		VALUES ('ar-x', 'drama-project', 'execution', 'script.execution.x', 'no-such-skill', 'pending', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("an agent run citing a skill version that does not exist was accepted")
	}
	// 2. The layer vocabulary is closed.
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_runs
		(id, project_id, agent_layer, agent_key, skill_version_id, status, created_at, updated_at)
		VALUES ('ar-y', 'drama-project', 'advisor', 'script.advisor.x', 'sv-1', 'pending', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("an unknown agent layer was accepted")
	}
	// 3. A denied tool call must carry the code that says it was denied.
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_tool_calls
		(id, agent_run_id, sequence, tool_key, status, created_at)
		VALUES ('at-2', 'ar-1', 1, 'script.create_script_version', 'denied', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a denied tool call with no reason was accepted")
	}
	// 4. One sequence number cannot be used twice in a run.
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_tool_calls
		(id, agent_run_id, sequence, tool_key, status, created_at)
		VALUES ('at-3', 'ar-1', 0, 'story.read_events', 'succeeded', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("two tool calls shared a sequence number in one run")
	}
	// 5. Deleting a run removes its messages and calls, so a pruned run leaves no
	// orphans behind.
	if _, err := db.ExecContext(ctx, `DELETE FROM agent_runs WHERE id = 'ar-1'`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"agent_messages", "agent_tool_calls"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s still holds %d rows after the run was deleted", table, count)
		}
	}
	// The skill version survives: it is history, not a child.
	var skills int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_versions`).Scan(&skills); err != nil {
		t.Fatal(err)
	}
	if skills != 1 {
		t.Fatalf("deleting a run removed %d skill versions", 1-skills)
	}
}

// hashOf builds a 64-character hex digest from a repeated character, so a test
// fixture does not need a real hash.
func hashOf(character string) string {
	out := ""
	for index := 0; index < 64; index++ {
		out += character
	}
	return out
}
