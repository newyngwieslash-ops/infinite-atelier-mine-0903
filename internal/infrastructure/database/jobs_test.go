package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

func openWP03Repo(t *testing.T) (*JobRepository, *ProviderRepository) {
	t.Helper()
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp03Migrations(t), time.Now)
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
	return NewJobRepository(handle.SQL()), NewProviderRepository(handle.SQL())
}

func sampleJob(id string) job.Job {
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	return job.Job{
		ID:               id,
		ProjectID:        "proj-1",
		EntityType:       "canvas_node",
		EntityID:         "node-1",
		JobType:          job.JobTypeImageGeneration,
		Status:           job.StatusQueued,
		Priority:         0,
		IdempotencyKey:   "generate-image:" + id,
		ProviderConfigID: "relay-a",
		InputJSON:        `{"prompt":"a cat"}`,
		MaxAttempts:      3,
		CreatedAt:        now,
		UpdatedAt:        now,
		Revision:         1,
	}
}

func TestJobRepositoryInsertAndGet(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	record := sampleJob("job-1")
	if err := repo.Insert(ctx, record); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := repo.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != record.ID || got.Status != job.StatusQueued || got.JobType != job.JobTypeImageGeneration {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if got.InputJSON != record.InputJSON || got.MaxAttempts != 3 || got.Revision != 1 {
		t.Fatalf("fields lost: %+v", got)
	}
	if got.Progress != nil {
		t.Fatalf("progress should be unset, got %v", *got.Progress)
	}
	if !got.CreatedAt.Equal(record.CreatedAt) {
		t.Fatalf("created_at = %s, want %s", got.CreatedAt, record.CreatedAt)
	}
}

func TestJobRepositoryDuplicateIdempotencyKeyReturnsConflict(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	first := sampleJob("job-1")
	if err := repo.Insert(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := sampleJob("job-2")
	second.IdempotencyKey = first.IdempotencyKey
	err := repo.Insert(ctx, second)
	if err == nil {
		t.Fatal("duplicate idempotency key accepted")
	}
	jobErr, ok := job.AsJobError(err)
	if !ok || jobErr.Category != job.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	// The caller can look up the existing job by key.
	existing, err := repo.GetByIdempotencyKey(ctx, first.ProjectID, first.IdempotencyKey)
	if err != nil {
		t.Fatalf("GetByIdempotencyKey: %v", err)
	}
	if existing.ID != "job-1" {
		t.Fatalf("resolved wrong job: %s", existing.ID)
	}
}

func TestJobRepositoryGetMissingIsNotFound(t *testing.T) {
	repo, _ := openWP03Repo(t)
	_, err := repo.Get(context.Background(), "nope")
	if err == nil {
		t.Fatal("missing job returned no error")
	}
	if _, ok := job.AsJobError(err); !ok {
		t.Fatalf("expected job error, got %T", err)
	}
}

// TestJobRepositoryClaimIsExclusive proves two workers cannot both claim the
// same job (lease/revision guard).
func TestJobRepositoryClaimIsExclusive(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	if err := repo.Insert(ctx, sampleJob("job-1")); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	lease := now.Add(10 * time.Minute)

	claimed, err := repo.Claim(ctx, "job-1", "worker-1", lease, now)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if claimed.Status != job.StatusRunning || claimed.LeaseOwner != "worker-1" {
		t.Fatalf("claim did not take ownership: %+v", claimed)
	}
	if claimed.Revision != 2 {
		t.Fatalf("revision = %d, want 2", claimed.Revision)
	}

	// The second worker must lose: the revision moved.
	if _, err := repo.Claim(ctx, "job-1", "worker-2", lease, now); err == nil {
		t.Fatal("second worker claimed an already-claimed job")
	} else if jobErr, ok := job.AsJobError(err); !ok || jobErr.Category != job.CategoryConflict {
		t.Fatalf("expected conflict on second claim, got %v", err)
	}
}

// TestJobRepositoryCancelledJobIsClaimableForSettlement proves a cancelled job
// is handed to a worker so it can be settled to a terminal state.
//
// It must be claimable: the candidate query excludes cancel-flagged jobs from
// ordinary scheduling, so if Claim also refused them nothing would ever move
// them out of a non-terminal state, and the Job Center would show an active job
// the user already cancelled.
func TestJobRepositoryCancelledJobIsClaimableForSettlement(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	if err := repo.Insert(ctx, sampleJob("job-1")); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	if _, err := repo.RequestCancel(ctx, "job-1", now); err != nil {
		t.Fatal(err)
	}
	// The job is offered as a settlement candidate.
	candidates, err := repo.ClaimableCandidates(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.ID == "job-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("cancelled job not offered for settlement; it would strand forever")
	}
	// And it can be claimed.
	claimed, err := repo.Claim(ctx, "job-1", "worker-1", now.Add(time.Minute), now)
	if err != nil {
		t.Fatalf("cancelled job could not be claimed for settlement: %v", err)
	}
	if !claimed.CancelRequested {
		t.Fatal("the cancel flag was lost on claim")
	}
	// An ordinary queued job is still claimable in the normal way.
	if err := repo.Insert(ctx, sampleJob("job-2")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Claim(ctx, "job-2", "worker-1", now.Add(time.Minute), now); err != nil {
		t.Fatalf("ordinary job rejected: %v", err)
	}
}

// TestJobRepositoryClaimableRespectsRetryWindow proves a job in backoff is not
// offered early.
func TestJobRepositoryClaimableRespectsRetryWindow(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	record := sampleJob("job-1")
	record.Status = job.StatusRetryWait
	record.NextRetryAt = time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	if err := repo.Insert(ctx, record); err != nil {
		t.Fatal(err)
	}
	// Before the retry time: not offered.
	early := time.Date(2026, 9, 15, 10, 30, 0, 0, time.UTC)
	candidates, err := repo.ClaimableCandidates(ctx, early, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("job offered before its retry window: %+v", candidates)
	}
	// After the retry time: offered.
	late := time.Date(2026, 9, 15, 11, 30, 0, 0, time.UTC)
	candidates, err = repo.ClaimableCandidates(ctx, late, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "job-1" {
		t.Fatalf("job not offered after its retry window: %+v", candidates)
	}
}

// TestJobRepositoryParkedJobIsPacedByRetryWindow proves the SQL layer paces a
// remote poll.
//
// waiting_remote is written with a next_retry_at that is the earliest moment the
// job may be polled again. Both the candidate query and the claim guard must
// honour it: if the candidate query offered the job immediately, the scheduler
// would poll the provider at its own loop speed, and if the claim guard accepted
// it, a competing worker could poll once more inside the same interval.
func TestJobRepositoryParkedJobIsPacedByRetryWindow(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	record := sampleJob("job-1")
	record.Status = job.StatusWaitingRemote
	record.RemoteJobID = "remote-1"
	record.NextRetryAt = time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	if err := repo.Insert(ctx, record); err != nil {
		t.Fatal(err)
	}

	early := time.Date(2026, 9, 15, 10, 30, 0, 0, time.UTC)
	candidates, err := repo.ClaimableCandidates(ctx, early, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("a parked job was offered before its poll interval elapsed: %+v", candidates)
	}
	// The claim guard must agree with the candidate query.
	if _, err := repo.Claim(ctx, "job-1", "worker-1", early.Add(time.Minute), early); err == nil {
		t.Fatal("a parked job was claimable before its poll interval elapsed")
	}

	late := time.Date(2026, 9, 15, 11, 30, 0, 0, time.UTC)
	candidates, err = repo.ClaimableCandidates(ctx, late, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "job-1" {
		t.Fatalf("a parked job was not offered after its poll interval: %+v", candidates)
	}
	claimed, err := repo.Claim(ctx, "job-1", "worker-1", late.Add(time.Minute), late)
	if err != nil {
		t.Fatalf("a parked job could not be claimed after its poll interval: %v", err)
	}
	if claimed.Status != job.StatusRunning {
		t.Fatalf("claim did not move the job to running: %q", claimed.Status)
	}
}

// TestJobRepositoryParkedCancelSkipsThePollWindow proves a cancelled job parked
// in a paced state can still be claimed for settlement.
//
// The next_retry_at window exists to pace polls; a cancel needs no poll. If the
// claim guard applied the window to cancel-flagged jobs, the candidate query
// (which exempts them) would offer a job the claim then rejects, and the
// settlement would be delayed by a poll interval for no reason.
func TestJobRepositoryParkedCancelSkipsThePollWindow(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	record := sampleJob("job-1")
	record.Status = job.StatusWaitingRemote
	record.RemoteJobID = "remote-1"
	record.NextRetryAt = time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	if err := repo.Insert(ctx, record); err != nil {
		t.Fatal(err)
	}
	early := time.Date(2026, 9, 15, 10, 30, 0, 0, time.UTC)
	if _, err := repo.RequestCancel(ctx, "job-1", early); err != nil {
		t.Fatal(err)
	}
	candidates, err := repo.ClaimableCandidates(ctx, early, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("a cancelled job was not offered for settlement: %+v", candidates)
	}
	claimed, err := repo.Claim(ctx, "job-1", "worker-1", early.Add(time.Minute), early)
	if err != nil {
		t.Fatalf("a cancelled job could not be claimed before its poll window: %v", err)
	}
	if !claimed.CancelRequested {
		t.Fatal("the cancel flag was lost on claim")
	}
}

// TestJobRepositoryClaimableOrdersByPriority proves the scheduler order.
func TestJobRepositoryClaimableOrdersByPriority(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)

	low := sampleJob("job-low")
	low.Priority = -5
	low.CreatedAt = now.Add(-2 * time.Hour)
	low.UpdatedAt = low.CreatedAt
	high := sampleJob("job-high")
	high.Priority = 10
	high.CreatedAt = now.Add(-1 * time.Hour)
	high.UpdatedAt = high.CreatedAt
	mid := sampleJob("job-mid")
	mid.CreatedAt = now.Add(-30 * time.Minute)
	mid.UpdatedAt = mid.CreatedAt

	for _, record := range []job.Job{low, mid, high} {
		if err := repo.Insert(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := repo.ClaimableCandidates(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		order = append(order, candidate.ID)
	}
	want := []string{"job-high", "job-mid", "job-low"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestJobRepositoryClaimableReclaimsExpiredLease proves a job abandoned by a
// dead worker becomes runnable again.
func TestJobRepositoryClaimableReclaimsExpiredLease(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	record := sampleJob("job-1")
	record.Status = job.StatusRunning
	record.LeaseOwner = "dead-worker"
	record.LeaseExpiresAt = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	if err := repo.Insert(ctx, record); err != nil {
		t.Fatal(err)
	}
	// While the lease is live it is not offered.
	live := time.Date(2026, 9, 15, 9, 59, 0, 0, time.UTC)
	candidates, err := repo.ClaimableCandidates(ctx, live, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatal("job with a live lease was offered")
	}
	// After the lease expires it is reclaimable.
	expired := time.Date(2026, 9, 15, 10, 1, 0, 0, time.UTC)
	candidates, err = repo.ClaimableCandidates(ctx, expired, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expired lease not reclaimed: %+v", candidates)
	}
	if _, err := repo.Claim(ctx, "job-1", "worker-2", expired.Add(10*time.Minute), expired); err != nil {
		t.Fatalf("reclaim failed: %v", err)
	}
}

func TestJobRepositoryUpdateGuardsRevision(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	if err := repo.Insert(ctx, sampleJob("job-1")); err != nil {
		t.Fatal(err)
	}
	record, err := repo.Get(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	stale := record
	record.Status = job.StatusRunning
	record.UpdatedAt = time.Date(2026, 9, 15, 10, 5, 0, 0, time.UTC)
	if err := repo.Update(ctx, record, record.Revision); err != nil {
		t.Fatalf("first update: %v", err)
	}
	// A second writer holding the old revision must fail.
	stale.Status = job.StatusFailed
	if err := repo.Update(ctx, stale, stale.Revision); err == nil {
		t.Fatal("stale revision accepted")
	} else if jobErr, ok := job.AsJobError(err); !ok || jobErr.Category != job.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestJobRepositoryAttempts(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	if err := repo.Insert(ctx, sampleJob("job-1")); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	attempt := job.Attempt{ID: "att-1", JobID: "job-1", AttemptNumber: 1, Status: job.AttemptRunning, StartedAt: now}
	if err := repo.StartAttempt(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	attempt.Status = job.AttemptFailed
	attempt.ErrorCode = string(job.CategoryNetwork)
	attempt.ErrorMessage = "The provider could not be reached."
	attempt.FinishedAt = now.Add(time.Second)
	if err := repo.FinishAttempt(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	attempts, err := repo.ListAttempts(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	if attempts[0].Status != job.AttemptFailed || attempts[0].ErrorCode != string(job.CategoryNetwork) {
		t.Fatalf("attempt not persisted: %+v", attempts[0])
	}
	if !attempts[0].StartedAt.Equal(now) {
		t.Fatalf("started_at = %s, want %s", attempts[0].StartedAt, now)
	}
}

func TestJobRepositoryDependencies(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	for _, id := range []string{"job-1", "job-2"} {
		if err := repo.Insert(ctx, sampleJob(id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.AddDependency(ctx, job.Dependency{JobID: "job-2", DependsOnJobID: "job-1", Condition: job.ConditionSuccess}); err != nil {
		t.Fatal(err)
	}
	dependencies, err := repo.ListDependencies(ctx, "job-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies) != 1 || dependencies[0].DependsOnJobID != "job-1" || dependencies[0].Condition != job.ConditionSuccess {
		t.Fatalf("dependency not persisted: %+v", dependencies)
	}
	// Unknown condition is rejected before it reaches SQL.
	if err := repo.AddDependency(ctx, job.Dependency{JobID: "job-1", DependsOnJobID: "job-2", Condition: "whenever"}); err == nil {
		t.Fatal("unknown condition accepted")
	}
}

func TestJobRepositoryActiveCounts(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	first := sampleJob("job-1")
	second := sampleJob("job-2")
	second.Status = job.StatusRunning
	if err := repo.Insert(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, second); err != nil {
		t.Fatal(err)
	}
	counts, err := repo.ActiveCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts[job.StatusQueued] != 1 || counts[job.StatusRunning] != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestJobRepositoryListFilters(t *testing.T) {
	repo, _ := openWP03Repo(t)
	ctx := context.Background()
	queued := sampleJob("job-queued")
	done := sampleJob("job-done")
	done.Status = job.StatusSucceeded
	for _, record := range []job.Job{queued, done} {
		if err := repo.Insert(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	active, err := repo.List(ctx, jobs.ListFilter{ActiveOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "job-queued" {
		t.Fatalf("active filter = %+v", active)
	}
	filtered, err := repo.List(ctx, jobs.ListFilter{Statuses: []job.Status{job.StatusSucceeded}})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ID != "job-done" {
		t.Fatalf("status filter = %+v", filtered)
	}
	all, err := repo.List(ctx, jobs.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered list = %d, want 2", len(all))
	}
}

func TestJobRepositoryNilDatabaseFailsClosed(t *testing.T) {
	var repo *JobRepository
	ctx := context.Background()
	if err := repo.Insert(ctx, job.Job{}); err == nil {
		t.Fatal("nil repo Insert succeeded")
	}
	if _, err := repo.Get(ctx, "x"); err == nil {
		t.Fatal("nil repo Get succeeded")
	}
	if _, err := repo.GetByIdempotencyKey(ctx, "p", "k"); err == nil {
		t.Fatal("nil repo GetByIdempotencyKey succeeded")
	}
	if _, err := repo.List(ctx, jobs.ListFilter{}); err == nil {
		t.Fatal("nil repo List succeeded")
	}
	if _, err := repo.Claim(ctx, "x", "w", time.Now(), time.Now()); err == nil {
		t.Fatal("nil repo Claim succeeded")
	}
	if err := repo.Update(ctx, job.Job{}, 1); err == nil {
		t.Fatal("nil repo Update succeeded")
	}
	if _, err := repo.RequestCancel(ctx, "x", time.Now()); err == nil {
		t.Fatal("nil repo RequestCancel succeeded")
	}
	if _, err := repo.ClaimableCandidates(ctx, time.Now(), 1); err == nil {
		t.Fatal("nil repo ClaimableCandidates succeeded")
	}
	if _, err := repo.ActiveCounts(ctx); err == nil {
		t.Fatal("nil repo ActiveCounts succeeded")
	}
	if err := repo.StartAttempt(ctx, job.Attempt{}); err == nil {
		t.Fatal("nil repo StartAttempt succeeded")
	}
	if err := repo.FinishAttempt(ctx, job.Attempt{}); err == nil {
		t.Fatal("nil repo FinishAttempt succeeded")
	}
	if _, err := repo.ListAttempts(ctx, "x"); err == nil {
		t.Fatal("nil repo ListAttempts succeeded")
	}
	if err := repo.AddDependency(ctx, job.Dependency{}); err == nil {
		t.Fatal("nil repo AddDependency succeeded")
	}
	if _, err := repo.ListDependencies(ctx, "x"); err == nil {
		t.Fatal("nil repo ListDependencies succeeded")
	}
}
