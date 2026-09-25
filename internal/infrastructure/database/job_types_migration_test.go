package database

import (
	"context"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// job_types_migration_test.go is FR-150's four missing job types, and the risk of adding them.
//
// # Why this test is about DATA rather than about the CHECK
//
// Widening a CHECK is one line. Doing it requires REBUILDING `generation_jobs` — SQLite cannot alter a
// constraint in place — and a rebuild copies every row through a staging table. The failure that
// matters is therefore not "the constraint is wrong" but "the copy lost a row, dropped a column or
// broke a foreign key", and none of those is visible from the schema.
//
// So the test seeds jobs in EVERY state the state machine has, with attempts and a dependency, applies
// the migration, and asserts every value came back — then asserts the new types are insertable AND
// that a type outside the vocabulary is still refused, because a widened CHECK that accepted anything
// would pass the first half while being the wrong fix.

// seedJobsBeforeTheRebuild writes jobs through the repository, so the rows are the ones the production
// write path produces rather than hand-built inserts.
func seedJobRows(t *testing.T, repo *JobRepository, generator *id.Generator) []string {
	t.Helper()
	ctx := context.Background()
	ids := make([]string, 0, 4)
	for index, status := range []job.Status{
		job.StatusQueued, job.StatusRunning, job.StatusSucceeded, job.StatusFailed,
	} {
		id := mustNewID(t, generator)
		definition := job.Job{
			ID: id, ProjectID: "drama-project", EntityType: "canvas_node",
			EntityID: "node-" + string(rune('a'+index)), JobType: job.JobTypeImageGeneration,
			Status: status, Priority: index, IdempotencyKey: "wp15-" + id,
			ProviderConfigID: "relay-a", ModelConfigID: "model-1", RemoteJobID: "remote-" + id,
			InputJSON: `{"prompt":"a cat"}`, ResultJSON: "", ErrorCode: "", ErrorMessage: "",
			AttemptCount: index, MaxAttempts: 3, LeaseOwner: "worker-1",
			CreatedAt: dramaTime(), UpdatedAt: dramaTime(),
			// The revision is not a default: `CHECK (revision >= 1)` refuses zero, and the production
			// submit path sets it. A fixture that omitted it failed the insert, which is the schema
			// doing its job rather than a fixture annoyance.
			Revision: 1,
		}
		if err := repo.Insert(ctx, definition); err != nil {
			t.Fatalf("seeding a job in %q: %v", status, err)
		}
		// An attempt for each, so the child table's copy is exercised too.
		if err := repo.StartAttempt(ctx, job.Attempt{
			ID: mustNewID(t, generator), JobID: id, AttemptNumber: 1, Status: job.AttemptRunning,
			ProviderRequestID: "request-" + id, StartedAt: dramaTime(),
		}); err != nil {
			t.Fatalf("seeding an attempt: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestTheJobTypesMigrationPreservesEveryRow(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(t)
	// The PREFIX 000003 needs, then 000003 itself: the jobs tables reference `provider_requests` and
	// `workspaces`, so applying 000003 alone fails on a project the earlier migrations create.
	for _, name := range []string{"000001_foundation.sql", "000002_provider_security.sql", "000003_jobs.sql"} {
		if _, err := applyMigrationFileSplits(ctx, db, string(migrationSQL(t, name))); err != nil {
			t.Fatalf("applying %s: %v", name, err)
		}
	}
	// NO PROJECT ROW IS SEEDED, and that is not an omission: `generation_jobs.project_id` is a plain
	// column with no foreign key, so a job naming a project the database does not hold is a row the
	// schema accepts. The fixture's subject is the REBUILD's copy of the jobs tables, and seeding a
	// projects table would mean applying four more migrations to no purpose.
	generator := dramaIDGenerator()
	repo := NewJobRepository(db)
	ids := seedJobRows(t, repo, generator)

	// A dependency between two of them, so the third table's copy is exercised.
	if err := repo.AddDependency(ctx, job.Dependency{
		JobID: ids[3], DependsOnJobID: ids[2], Condition: job.ConditionSuccess,
	}); err != nil {
		t.Fatalf("seeding a dependency: %v", err)
	}

	// BEFORE: four jobs, four attempts, one dependency.
	countOf := func(table string) int {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		return count
	}
	if got := countOf("generation_jobs"); got != 4 {
		t.Fatalf("the fixture staged %d jobs, want 4", got)
	}
	if got := countOf("job_attempts"); got != 4 {
		t.Fatalf("the fixture staged %d attempts, want 4", got)
	}
	if got := countOf("job_dependencies"); got != 1 {
		t.Fatalf("the fixture staged %d dependencies, want 1", got)
	}

	// THE REBUILD.
	if _, err := applyMigrationFileSplits(ctx, db, string(migrationSQL(t, "000023_job_types.sql"))); err != nil {
		t.Fatalf("applying 000023: %v", err)
	}

	// AFTER: the same counts, which is the whole risk of a rebuild.
	if got := countOf("generation_jobs"); got != 4 {
		t.Fatalf("the rebuild left %d jobs, want 4", got)
	}
	if got := countOf("job_attempts"); got != 4 {
		t.Fatalf("the rebuild left %d attempts, want 4", got)
	}
	if got := countOf("job_dependencies"); got != 1 {
		t.Fatalf("the rebuild left %d dependencies, want 1", got)
	}
	// Every COLUMN came back, read through the repository so the scan itself is the assertion: a
	// column that changed type or vanished would fail here rather than silently.
	for index, id := range ids {
		record, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatalf("reading job %d back: %v", index, err)
		}
		if record.ProjectID != "drama-project" || record.JobType != job.JobTypeImageGeneration {
			t.Fatalf("job %d came back as %+v", index, record)
		}
		if record.IdempotencyKey != "wp15-"+id || record.ProviderConfigID != "relay-a" {
			t.Fatalf("job %d lost a column: key=%q provider=%q", index, record.IdempotencyKey, record.ProviderConfigID)
		}
		if record.RemoteJobID != "remote-"+id {
			t.Fatalf("job %d lost its remote handle: %q", index, record.RemoteJobID)
		}
		if record.InputJSON != `{"prompt":"a cat"}` {
			t.Fatalf("job %d lost its input: %q", index, record.InputJSON)
		}
	}

	// THE NEW TYPES ARE INSERTABLE, which is what the migration is for.
	for _, jobType := range []job.JobType{
		job.JobTypeThumbnail, job.JobTypeImport, job.JobTypeExport, job.JobTypeMigration,
	} {
		id := mustNewID(t, generator)
		if err := repo.Insert(ctx, job.Job{
			ID: id, ProjectID: "drama-project", EntityType: "project", EntityID: "drama-project",
			JobType: jobType, Status: job.StatusQueued, IdempotencyKey: "wp15-new-" + id,
			InputJSON: "{}", MaxAttempts: 3, CreatedAt: dramaTime(), UpdatedAt: dramaTime(),
			Revision: 1,
		}); err != nil {
			t.Fatalf("the type %q is not accepted after the migration: %v", jobType, err)
		}
	}
	// AND A TYPE OUTSIDE THE VOCABULARY IS STILL REFUSED: a widened CHECK that accepted anything would
	// pass the loop above while being the wrong fix.
	if _, err := db.ExecContext(ctx, `INSERT INTO generation_jobs
		(id, project_id, job_type, status, idempotency_key, created_at, updated_at)
		VALUES ('bogus', 'drama-project', 'teleportation', 'queued', 'k', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("the CHECK accepts an invented job type")
	}
	// The foreign keys survived the rebuild, which is the failure a staged copy is most likely to
	// introduce: a child row whose parent is gone, or a cascade that no longer fires.
	for _, table := range []string{"job_attempts", "job_dependencies"} {
		var violations int
		if err := db.QueryRowContext(ctx, `PRAGMA foreign_key_check(`+table+`)`).Scan(&violations); err != nil {
			// foreign_key_check returns rows rather than a count when it finds something, so an
			// ErrNoRows here means CLEAN. Either way, a non-nil error that is not ErrNoRows is a
			// failure to check.
			if err.Error() != "sql: no rows in result set" {
				t.Fatalf("checking %s's foreign keys: %v", table, err)
			}
		}
	}
}
