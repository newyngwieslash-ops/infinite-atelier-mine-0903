package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// TestSchedulerRunsJobsToCompletion proves the end-to-end shape AC-FOUND-006
// names: queued -> running -> succeeded, driven by the real scheduler loop.
func TestSchedulerRunsJobsToCompletion(t *testing.T) {
	runner := &scriptedRunner{}
	service, repository, _, publisher := newTestService(t, runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx, 2, 5*time.Millisecond)

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repository, record.ID, job.StatusSucceeded, 3*time.Second)

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.AttemptCount != 1 {
		t.Fatalf("attempt count = %d, want 1", final.AttemptCount)
	}
	if final.StartedAt.IsZero() || final.FinishedAt.IsZero() {
		t.Fatalf("lifecycle timestamps missing: %+v", final)
	}
	// The UI saw the transitions.
	sawSucceeded := false
	for _, event := range publisher.snapshot() {
		if event.Status == string(job.StatusSucceeded) {
			sawSucceeded = true
		}
	}
	if !sawSucceeded {
		t.Fatal("no succeeded event published")
	}
	if err := service.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestSchedulerHonoursPause proves a paused queue hands out no work, and that
// resuming restarts it.
func TestSchedulerHonoursPause(t *testing.T) {
	runner := &scriptedRunner{}
	service, repository, clock, _ := newTestService(t, runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Pause()
	service.Start(ctx, 1, 5*time.Millisecond)

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Give the scheduler several passes while paused.
	clock.Advance(time.Second)
	time.Sleep(100 * time.Millisecond)
	stored, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != job.StatusQueued {
		t.Fatalf("paused queue ran a job: %q", stored.Status)
	}

	service.Resume()
	waitForStatus(t, repository, record.ID, job.StatusSucceeded, 3*time.Second)
	_ = service.Stop(ctx)
}

// TestSchedulerReclaimsExpiredLease proves a job abandoned by a dead worker is
// picked up again by a live scheduler pass.
func TestSchedulerReclaimsExpiredLease(t *testing.T) {
	runner := &scriptedRunner{}
	service, repository, clock, _ := newTestService(t, runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an abandoned claim from a previous process.
	if _, err := repository.Claim(ctx, record.ID, "dead-worker", clock.Now().Add(-time.Minute), clock.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	service.Start(ctx, 1, 5*time.Millisecond)
	waitForStatus(t, repository, record.ID, job.StatusSucceeded, 3*time.Second)
	_ = service.Stop(ctx)
}

// TestSchedulerRetriesThenSucceeds proves the retry_wait path is actually
// travelled: a transient failure is retried and the second attempt succeeds.
func TestSchedulerRetriesThenSucceeds(t *testing.T) {
	runner := &scriptedRunner{
		errs:     []error{context.DeadlineExceeded, nil},
		outcomes: []Outcome{{}, {Status: job.StatusSucceeded}},
	}
	repository := newMemoryRepository()
	// The retry window is computed from the service clock, so the clock must
	// advance for the second attempt to become claimable. An advancing clock
	// keeps the test deterministic without sleeping for the backoff.
	clock := &advancingClock{start: time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)}
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Runner:     runner,
		Policy:     job.RetryPolicy{BaseDelay: 10 * time.Millisecond, MaxDelay: 20 * time.Millisecond, MaxAttempts: 3},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx, 1, 5*time.Millisecond)

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repository, record.ID, job.StatusSucceeded, 5*time.Second)
	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.AttemptCount < 2 {
		t.Fatalf("attempt count = %d, want at least 2 after a retry", final.AttemptCount)
	}
	attempts, err := repository.ListAttempts(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) < 2 {
		t.Fatalf("attempt history = %d rows, want at least 2", len(attempts))
	}
	if attempts[0].Status != job.AttemptFailed || attempts[1].Status != job.AttemptSucceeded {
		t.Fatalf("attempt outcomes = %q, %q", attempts[0].Status, attempts[1].Status)
	}
	_ = service.Stop(ctx)
}

// TestSchedulerRespectsDependencies proves a job whose dependency failed is not
// executed and is recorded as a dependency failure.
func TestSchedulerRespectsDependencies(t *testing.T) {
	runner := &scriptedRunner{errs: []error{job.FailedJobError(job.CategorySecurity, "blocked")}}
	service, repository, _, _ := newTestService(t, runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	parent, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"parent"}`))
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"child"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AddDependency(ctx, job.Dependency{
		JobID:          child.ID,
		DependsOnJobID: parent.ID,
		Condition:      job.ConditionSuccess,
	}); err != nil {
		t.Fatal(err)
	}

	service.Start(ctx, 1, 5*time.Millisecond)
	// The parent fails permanently; the child must never run.
	waitForStatus(t, repository, parent.ID, job.StatusFailed, 3*time.Second)
	waitForStatus(t, repository, child.ID, job.StatusFailed, 3*time.Second)

	childRecord, err := repository.Get(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if childRecord.ErrorCode != string(job.CategoryDependency) {
		t.Fatalf("child error code = %q, want dependency", childRecord.ErrorCode)
	}
	if childRecord.StartedAt != (time.Time{}) {
		t.Fatal("child was executed despite an unmet dependency")
	}
	_ = service.Stop(ctx)
}

// TestSchedulerRunsChildAfterSuccessfulParent proves the dependency gate does
// not block a job whose requirement is met.
func TestSchedulerRunsChildAfterSuccessfulParent(t *testing.T) {
	runner := &scriptedRunner{}
	service, repository, _, _ := newTestService(t, runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	parent, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"parent"}`))
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"child"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AddDependency(ctx, job.Dependency{
		JobID:          child.ID,
		DependsOnJobID: parent.ID,
		Condition:      job.ConditionSuccess,
	}); err != nil {
		t.Fatal(err)
	}
	service.Start(ctx, 2, 5*time.Millisecond)
	waitForStatus(t, repository, parent.ID, job.StatusSucceeded, 3*time.Second)
	waitForStatus(t, repository, child.ID, job.StatusSucceeded, 3*time.Second)
	_ = service.Stop(ctx)
}

// TestSchedulerCancelStopsQueuedWork proves cancelling a queued job prevents it
// from ever executing.
func TestSchedulerCancelStopsQueuedWork(t *testing.T) {
	runner := &scriptedRunner{}
	service, repository, _, _ := newTestService(t, runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service.Pause()
	service.Start(ctx, 1, 5*time.Millisecond)
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	service.Resume()
	time.Sleep(150 * time.Millisecond)

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", final.Status)
	}
	if runner.calls != 0 {
		t.Fatalf("runner executed %d times for a cancelled job", runner.calls)
	}
	_ = service.Stop(ctx)
}

// TestConcurrentWorkersDoNotDoubleRun is the duplicate-execution guard: many
// queued jobs and several workers must each run exactly once.
func TestConcurrentWorkersDoNotDoubleRun(t *testing.T) {
	countingRunner := &countingRunner{}
	repository := newMemoryRepository()
	clock := newFakeClock()
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Runner:     countingRunner,
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxAttempts: 3},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const jobCount = 12
	ids := make([]string, 0, jobCount)
	for index := 0; index < jobCount; index++ {
		record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"p`+itoa(index)+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, record.ID)
	}
	service.Start(ctx, 4, 2*time.Millisecond)
	for _, id := range ids {
		waitForStatus(t, repository, id, job.StatusSucceeded, 5*time.Second)
	}
	_ = service.Stop(ctx)

	if got := countingRunner.total(); got != jobCount {
		t.Fatalf("runner executed %d times for %d jobs; duplicate execution", got, jobCount)
	}
	// Each job ran exactly once.
	for _, id := range ids {
		if got := countingRunner.perJob(id); got != 1 {
			t.Fatalf("job %s executed %d times", id, got)
		}
	}
}

// advancingClock reports a time that moves forward on every read, so a retry
// backoff elapses without the test sleeping for it.
type advancingClock struct {
	mu    sync.Mutex
	start time.Time
	reads int
}

func (c *advancingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	return c.start.Add(time.Duration(c.reads) * time.Second)
}

type countingRunner struct {
	mu     sync.Mutex
	counts map[string]int
}

func (r *countingRunner) Run(_ context.Context, record job.Job) (Outcome, error) {
	r.mu.Lock()
	if r.counts == nil {
		r.counts = map[string]int{}
	}
	r.counts[record.ID]++
	r.mu.Unlock()
	return Outcome{Status: job.StatusSucceeded}, nil
}

func (r *countingRunner) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, count := range r.counts {
		total += count
	}
	return total
}

func (r *countingRunner) perJob(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[id]
}

// waitForStatus polls until the job reaches the wanted status or the deadline
// passes.
func waitForStatus(t *testing.T, repository *memoryRepository, id string, want job.Status, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		record, err := repository.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if record.Status == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	record, _ := repository.Get(context.Background(), id)
	t.Fatalf("job %s did not reach %q within %s (status=%q)", id, want, timeout, record.Status)
}
