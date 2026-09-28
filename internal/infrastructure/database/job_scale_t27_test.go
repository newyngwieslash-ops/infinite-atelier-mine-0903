package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// job_scale_t27_test.go is T27's "500 任务" scalability probe against the REAL
// job store: 500 persisted generation jobs across mixed statuses and two
// providers, measuring the two reads the Job Center and scheduler make —
// ListJobs (the UI's newest-first page) and ClaimableCandidates (the
// scheduler's dispatch query, with its provider/status/priority filters).
//
// This fills the last gap in the audit's T27 list: 1000 nodes/2000 edges and
// 10k materials were measured in scale_records_wp12_test.go; the 500-task
// figure is now a number rather than an assumption.
//
// Like every T27 probe, this test is SKIPped under the race build (perf
// separation, ADR-0032 ruling 9) and measures in the normal build only.

func TestT27JobStoreScale500Tasks(t *testing.T) {
	if raceBuild {
		t.Skip("performance probes are measured in a normal build; race instrumentation invalidates the timing comparison (T24/T27)")
	}
	ctx := context.Background()
	repo := NewJobRepository(openJobsScaleDBHandle(t))
	db := repo.RawDB()

	// 500 jobs: 300 succeeded (terminal, the Job Center history), 100 queued
	// (runnable by the scheduler), 50 waiting_remote on provider A, 50 queued
	// on provider B. Mixed providers make the dispatch query exercise its
	// per-provider filters rather than a single-partition fast path.
	providerOf := func(i int) string { return map[int]string{0: "prov-a", 1: "prov-b"}[i%2] }
	statusOf := func(i int) string {
		switch {
		case i < 300:
			return "succeeded"
		case i < 400:
			return "queued"
		case i < 450:
			return "waiting_remote"
		default:
			return "queued"
		}
	}
	for i := 0; i < 500; i++ {
		input, _ := json.Marshal(map[string]any{"providerId": providerOf(i), "model": "m", "text": fmt.Sprintf("task %d", i)})
		status := statusOf(i)
		remote := ""
		if status == "waiting_remote" {
			remote = "remote-" + fmt.Sprint(i)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO generation_jobs
			(id, project_id, entity_type, entity_id, job_type, status, priority, idempotency_key,
			 provider_config_id, remote_job_id, input_json, attempt_count, max_attempts, created_at, updated_at)
			VALUES (?, 't27-project', 'shot', ?, 'image_generation', ?, 0, ?,
			 ?, ?, ?, 1, 3, '2026-09-27T00:00:00Z', '2026-09-27T00:00:00Z')`,
			fmt.Sprintf("t27-job-%04d", i), fmt.Sprintf("shot-%d", i), status,
			"t27-key-"+fmt.Sprint(i), providerOf(i), remote, string(input)); err != nil {
			t.Fatalf("seeding job %d: %v", i, err)
		}
	}

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

	// THE UI READ: ListJobs newest first, the Job Center's default page.
	listMS := wp12MeasureT27(t, "ListJobs(200) over 500 tasks", func() error {
		_, err := repo.List(ctx, appjobs.ListFilter{Limit: 200})
		return err
	})

	// THE SCHEDULER READ: ClaimableCandidates with its live-lease/next-retry
	// predicate over the same 500 rows.
	claimMS := wp12MeasureT27(t, "ClaimableCandidates over 500 tasks", func() error {
		_, err := repo.ClaimableCandidates(ctx, now, 16)
		return err
	})

	// The scheduler's next step, claimed by a worker with a live lease: the
	// per-provider admission reads.
	counts, err := repo.ActiveProviderCounts(ctx, now)
	if err != nil {
		t.Fatalf("ActiveProviderCounts: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("no leases yet, but counts = %v", counts)
	}

	// FIGURES recorded (the deliverable): STATUS's T27 row cites these.
	t.Logf("[T27] jobs=500 list(200)=%v claimable=%v", listMS, claimMS)

	// Guards, generous for CI jitter (same policy as the WP-12 probes).
	if listMS > 500*time.Millisecond {
		t.Fatalf("ListJobs over 500 tasks took %v, over the 500ms guard", listMS)
	}
	if claimMS > 200*time.Millisecond {
		t.Fatalf("ClaimableCandidates over 500 tasks took %v, over the 200ms guard", claimMS)
	}

	// Correctness beside the timing: the scheduler's candidate query returns
	// the 150 queued rows PLUS the 50 waiting_remote (their remote work may
	// have finished — recovery/poll semantics, recovery.go) and skips the 300
	// succeeded. The statuses must all be runnable ones.
	candidates, err := repo.ClaimableCandidates(ctx, now, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 200 {
		t.Fatalf("ClaimableCandidates returned %d rows for 150 queued + 50 waiting_remote jobs", len(candidates))
	}
	for _, candidate := range candidates {
		if candidate.Status != job.StatusQueued && candidate.Status != job.StatusWaitingRemote {
			t.Fatalf("a %s job was claimed as runnable", candidate.Status)
		}
	}
	_ = appjobs.DefaultWorkerCount
}

// openJobsScaleDBHandle opens a migrated database for the job scale probe.
func openJobsScaleDBHandle(t *testing.T) *sql.DB {
	t.Helper()
	return dramaRepoHandle(t)
}

// RawDB exposes the handle the seeding statements run against — the probe
// seeds rows directly the way the WP-12 scale fixtures do.
func (r *JobRepository) RawDB() *sql.DB { return r.db }

// wp12MeasureT27 is the measurement helper matching scale_records_wp12_test.go's
// shape (start, run, report) so all T27 probes read the same.
func wp12MeasureT27(t *testing.T, label string, run func() error) time.Duration {
	t.Helper()
	start := time.Now()
	if err := run(); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	elapsed := time.Since(start)
	t.Logf("[T27] %s: %v", label, elapsed)
	return elapsed
}
