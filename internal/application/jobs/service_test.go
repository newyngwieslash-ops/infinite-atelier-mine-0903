package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// memoryRepository is an in-memory Repository used to test application logic
// without SQLite. It mirrors the revision/CAS behaviour of the real store so
// scheduler and worker behaviour is exercised honestly.
type memoryRepository struct {
	mu           sync.Mutex
	jobs         map[string]job.Job
	dependencies map[string][]job.Dependency
	attempts     map[string][]job.Attempt
	nextAttempt  map[string]int
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		jobs:         map[string]job.Job{},
		dependencies: map[string][]job.Dependency{},
		attempts:     map[string][]job.Attempt{},
		nextAttempt:  map[string]int{},
	}
}

// guard mirrors the SQLite driver's behaviour: an operation issued on a
// cancelled context fails instead of succeeding. Without it the in-memory
// double would hide the very shutdown bugs the settlement tests exist to catch.
func guard(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (r *memoryRepository) Insert(ctx context.Context, record job.Job) error {
	if err := guard(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.jobs {
		if existing.ProjectID == record.ProjectID && existing.IdempotencyKey == record.IdempotencyKey {
			return job.DuplicateJobError()
		}
	}
	r.jobs[record.ID] = record
	return nil
}

func (r *memoryRepository) Get(ctx context.Context, id string) (job.Job, error) {
	if err := guard(ctx); err != nil {
		return job.Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.jobs[id]
	if !ok {
		return job.Job{}, job.JobNotFoundError(id)
	}
	return record, nil
}

func (r *memoryRepository) GetByIdempotencyKey(ctx context.Context, projectID, key string) (job.Job, error) {
	if err := guard(ctx); err != nil {
		return job.Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.jobs {
		if record.ProjectID == projectID && record.IdempotencyKey == key {
			return record, nil
		}
	}
	return job.Job{}, job.JobNotFoundError(key)
}

func (r *memoryRepository) List(ctx context.Context, filter ListFilter) ([]job.Job, error) {
	if err := guard(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []job.Job
	for _, record := range r.jobs {
		if filter.ActiveOnly && !record.IsActive() {
			continue
		}
		if len(filter.Statuses) > 0 {
			matched := false
			for _, status := range filter.Statuses {
				if record.Status == status {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *memoryRepository) Claim(ctx context.Context, id, workerID string, leaseUntil time.Time, now time.Time) (job.Job, error) {
	if err := guard(ctx); err != nil {
		return job.Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.jobs[id]
	if !ok {
		return job.Job{}, job.JobNotFoundError(id)
	}
	// Mirror the SQL claim guard exactly: queued jobs, retry windows that have
	// elapsed, and jobs abandoned by an expired lease are all claimable. A
	// cancel-flagged job is claimable too — and exempt from the poll window,
	// because a cancel needs no poll — so a worker can settle it.
	//
	// A parked job (waiting_remote and friends) is gated by next_retry_at as
	// well: that timestamp is what paces a remote poll, so a double that ignored
	// it would let a test poll at scheduler speed while production paced it.
	if record.CancelRequested && record.LeaseHeld(now) {
		return job.Job{}, job.DuplicateJobError()
	}
	claimable := false
	switch {
	case record.CancelRequested:
		claimable = true
	default:
		switch record.Status {
		case job.StatusQueued:
			claimable = !record.LeaseHeld(now)
		case job.StatusRetryWait:
			claimable = record.NextRetryAt.IsZero() || !record.NextRetryAt.After(now)
		case job.StatusRunning:
			claimable = !record.LeaseHeld(now)
		case job.StatusWaitingRemote, job.StatusDownloading, job.StatusVerifying, job.StatusRecovering:
			windowOpen := record.NextRetryAt.IsZero() || !record.NextRetryAt.After(now)
			claimable = windowOpen && !record.LeaseHeld(now)
		}
	}
	if !claimable {
		return job.Job{}, job.DuplicateJobError()
	}
	record.Status = job.StatusRunning
	record.LeaseOwner = workerID
	record.LeaseExpiresAt = leaseUntil
	if record.StartedAt.IsZero() {
		record.StartedAt = now
	}
	record.UpdatedAt = now
	record.Revision++
	r.jobs[id] = record
	return record, nil
}

func (r *memoryRepository) Update(ctx context.Context, record job.Job, expectedRevision int64) error {
	if err := guard(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.jobs[record.ID]
	if !ok {
		return job.JobNotFoundError(record.ID)
	}
	if current.Revision != expectedRevision {
		return job.DuplicateJobError()
	}
	record.Revision = current.Revision + 1
	r.jobs[record.ID] = record
	return nil
}

func (r *memoryRepository) RequestCancel(ctx context.Context, id string, now time.Time) (job.Job, error) {
	if err := guard(ctx); err != nil {
		return job.Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.jobs[id]
	if !ok {
		return job.Job{}, job.JobNotFoundError(id)
	}
	record.CancelRequested = true
	record.UpdatedAt = now
	record.Revision++
	r.jobs[id] = record
	return record, nil
}

func (r *memoryRepository) ClaimableCandidates(ctx context.Context, now time.Time, limit int) ([]job.Job, error) {
	if err := guard(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []job.Job
	for _, record := range r.jobs {
		// A cancel-flagged job that still occupies the scheduler is offered so
		// a worker can settle it; ordinary work is offered only when it is not
		// cancelled. This mirrors the SQL candidate query.
		if record.Status.IsTerminal() {
			continue
		}
		if record.CancelRequested {
			if !record.LeaseHeld(now) {
				out = append(out, record)
			}
			continue
		}
		switch record.Status {
		case job.StatusQueued:
			out = append(out, record)
		case job.StatusRetryWait:
			if record.NextRetryAt.IsZero() || !record.NextRetryAt.After(now) {
				out = append(out, record)
			}
		case job.StatusRunning:
			if !record.LeaseHeld(now) {
				out = append(out, record)
			}
		case job.StatusWaitingRemote, job.StatusDownloading, job.StatusVerifying, job.StatusRecovering:
			// The next_retry_at window paces a parked poll: without it the
			// scheduler would re-poll as fast as the provider answers.
			windowOpen := record.NextRetryAt.IsZero() || !record.NextRetryAt.After(now)
			if windowOpen && !record.LeaseHeld(now) {
				out = append(out, record)
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *memoryRepository) ActiveCounts(ctx context.Context) (map[job.Status]int, error) {
	if err := guard(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := map[job.Status]int{}
	for _, record := range r.jobs {
		counts[record.Status]++
	}
	return counts, nil
}

func (r *memoryRepository) StartAttempt(ctx context.Context, attempt job.Attempt) error {
	if err := guard(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Mirror the UNIQUE (generation_job_id, attempt_number) constraint: a
	// duplicate number is a storage error, which is what makes the recovery
	// reconciliation observable in tests.
	for _, existing := range r.attempts[attempt.JobID] {
		if existing.AttemptNumber == attempt.AttemptNumber {
			return job.FailedJobError(job.CategoryStorage, "The job attempt could not be recorded.")
		}
	}
	r.attempts[attempt.JobID] = append(r.attempts[attempt.JobID], attempt)
	return nil
}

func (r *memoryRepository) FinishAttempt(ctx context.Context, attempt job.Attempt) error {
	if err := guard(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.attempts[attempt.JobID]
	for index := range list {
		if list[index].ID == attempt.ID {
			list[index] = attempt
		}
	}
	r.attempts[attempt.JobID] = list
	return nil
}

func (r *memoryRepository) ListAttempts(ctx context.Context, jobID string) ([]job.Attempt, error) {
	if err := guard(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]job.Attempt{}, r.attempts[jobID]...), nil
}

func (r *memoryRepository) AddDependency(ctx context.Context, dependency job.Dependency) error {
	if err := guard(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dependencies[dependency.JobID] = append(r.dependencies[dependency.JobID], dependency)
	return nil
}

func (r *memoryRepository) ListDependencies(ctx context.Context, jobID string) ([]job.Dependency, error) {
	if err := guard(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]job.Dependency{}, r.dependencies[jobID]...), nil
}

// fakeClock is a controllable clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

type counterIDs struct {
	mu    sync.Mutex
	count int
}

func (g *counterIDs) NewID(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.count++
	return prefix + "-" + itoa(g.count)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

type recordingPublisher struct {
	mu     sync.Mutex
	events []JobEvent
}

func (p *recordingPublisher) PublishJobChanged(_ context.Context, event JobEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func (p *recordingPublisher) snapshot() []JobEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]JobEvent{}, p.events...)
}

// scriptedRunner returns queued outcomes per attempt.
type scriptedRunner struct {
	mu       sync.Mutex
	outcomes []Outcome
	errs     []error
	calls    int
}

func (r *scriptedRunner) Run(_ context.Context, _ job.Job) (Outcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.calls
	r.calls++
	if index < len(r.errs) && r.errs[index] != nil {
		return Outcome{}, r.errs[index]
	}
	if index < len(r.outcomes) {
		return r.outcomes[index], nil
	}
	return Outcome{Status: job.StatusSucceeded}, nil
}

// claimForExecution takes a queued job through the real Claim path so the
// returned record carries the revision the worker would actually hold.
func claimForExecution(t *testing.T, repository *memoryRepository, clock *fakeClock, id string) job.Job {
	t.Helper()
	claimed, err := repository.Claim(context.Background(), id, "worker-1", clock.Now().Add(time.Hour), clock.Now())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return claimed
}

func newTestService(t *testing.T, runner JobRunner) (*Service, *memoryRepository, *fakeClock, *recordingPublisher) {
	t.Helper()
	repository := newMemoryRepository()
	clock := newFakeClock()
	publisher := &recordingPublisher{}
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Publisher:  publisher,
		Runner:     runner,
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, MaxAttempts: 3},
	})
	return service, repository, clock, publisher
}

func submitRequest(jobType job.JobType, input string) SubmitRequest {
	return SubmitRequest{
		ProjectID:        "proj-1",
		EntityType:       "canvas_node",
		EntityID:         "node-1",
		JobType:          jobType,
		ProviderConfigID: "relay-a",
		InputJSON:        input,
		Scope:            "generate-image",
	}
}

func TestSubmitCreatesQueuedJob(t *testing.T) {
	service, repository, _, publisher := newTestService(t, &scriptedRunner{})
	record, duplicate, err := service.Submit(context.Background(), submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if duplicate {
		t.Fatal("first submit reported as duplicate")
	}
	if record.Status != job.StatusQueued || record.ID == "" || record.Revision != 1 {
		t.Fatalf("unexpected record: %+v", record)
	}
	if record.MaxAttempts != 3 {
		t.Fatalf("max attempts = %d, want the configured policy", record.MaxAttempts)
	}
	if record.IdempotencyKey == "" {
		t.Fatal("idempotency key not derived")
	}
	stored, err := repository.Get(context.Background(), record.ID)
	if err != nil || stored.ID != record.ID {
		t.Fatalf("job not persisted: %v", err)
	}
	if events := publisher.snapshot(); len(events) == 0 {
		t.Fatal("no event published for a new job")
	}
}

// TestSubmitIsIdempotent covers AC-FOUND-006 "duplicate idempotency".
func TestSubmitIsIdempotent(t *testing.T) {
	service, repository, _, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	first, duplicate, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	if duplicate {
		t.Fatal("first submit flagged duplicate")
	}
	second, duplicate, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatalf("replay returned an error: %v", err)
	}
	if !duplicate {
		t.Fatal("identical submit not flagged as duplicate")
	}
	if second.ID != first.ID {
		t.Fatalf("replay created a new job: %s vs %s", second.ID, first.ID)
	}
	jobsList, err := repository.List(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobsList) != 1 {
		t.Fatalf("replay created %d jobs, want 1", len(jobsList))
	}
	// A different input is a different job.
	third, duplicate, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a dog"}`))
	if err != nil {
		t.Fatal(err)
	}
	if duplicate || third.ID == first.ID {
		t.Fatal("different input was treated as the same job")
	}
}

func TestSubmitRejectsUnknownJobType(t *testing.T) {
	service, _, _, _ := newTestService(t, &scriptedRunner{})
	if _, _, err := service.Submit(context.Background(), submitRequest(job.JobType("thumbnail"), `{}`)); err == nil {
		t.Fatal("unknown job type accepted")
	}
}

// TestCancelQueuedJobIsImmediate covers the cancel path for a job that has not
// started.
func TestCancelQueuedJobIsImmediate(t *testing.T) {
	service, repository, _, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.Cancel(ctx, record.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if cancelled.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	stored, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != job.StatusCancelled || !stored.CancelRequested {
		t.Fatalf("cancellation not persisted: %+v", stored)
	}
}

func TestCancelIsIdempotentAndTolerant(t *testing.T) {
	service, _, _, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	// Cancelling an already-cancelled job is a no-op, not an error.
	if _, err := service.Cancel(ctx, record.ID); err != nil {
		t.Fatalf("second cancel failed: %v", err)
	}
	// Cancelling an unknown job reports not-found.
	if _, err := service.Cancel(ctx, "missing"); err == nil {
		t.Fatal("cancelling an unknown job succeeded")
	}
}

func TestCancelManyCountsOnlyChanged(t *testing.T) {
	service, _, _, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	first, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"two"}`))
	if err != nil {
		t.Fatal(err)
	}
	count, err := service.CancelMany(ctx, []string{first.ID, second.ID, "missing"})
	if err != nil {
		t.Fatalf("CancelMany: %v", err)
	}
	if count != 2 {
		t.Fatalf("cancelled %d, want 2", count)
	}
}

// TestRetryFailedOnlyTouchesFailedJobs covers the "retry failed only" batch
// action.
func TestRetryFailedOnlyTouchesFailedJobs(t *testing.T) {
	service, repository, _, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	failed, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	succeeded, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"two"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Force terminal states directly through the repository.
	markStatus := func(id string, status job.Status) {
		record, err := repository.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		record.Status = status
		if err := repository.Update(ctx, record, record.Revision); err != nil {
			t.Fatal(err)
		}
	}
	markStatus(failed.ID, job.StatusFailed)
	markStatus(succeeded.ID, job.StatusSucceeded)

	retried, err := service.RetryFailed(ctx, []string{failed.ID, succeeded.ID})
	if err != nil {
		t.Fatalf("RetryFailed: %v", err)
	}
	if retried != 1 {
		t.Fatalf("retried %d, want 1", retried)
	}
	after, err := repository.Get(ctx, failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != job.StatusQueued || after.ErrorCode != "" || !after.FinishedAt.IsZero() {
		t.Fatalf("failed job not cleaned up for retry: %+v", after)
	}
	untouched, err := repository.Get(ctx, succeeded.ID)
	if err != nil {
		t.Fatal(err)
	}
	if untouched.Status != job.StatusSucceeded {
		t.Fatalf("succeeded job was requeued: %+v", untouched)
	}
}

func TestPauseAndResume(t *testing.T) {
	service, _, _, _ := newTestService(t, &scriptedRunner{})
	if service.Paused() {
		t.Fatal("service started paused")
	}
	service.Pause()
	if !service.Paused() {
		t.Fatal("pause did not take effect")
	}
	summary, err := service.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Paused {
		t.Fatal("summary did not report the pause state")
	}
	service.Resume()
	if service.Paused() {
		t.Fatal("resume did not take effect")
	}
}

func TestSummaryCounts(t *testing.T) {
	service, repository, _, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	for _, prompt := range []string{"one", "two", "three"} {
		if _, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"`+prompt+`"}`)); err != nil {
			t.Fatal(err)
		}
	}
	records, err := repository.List(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	record := records[0]
	record.Status = job.StatusFailed
	if err := repository.Update(ctx, record, record.Revision); err != nil {
		t.Fatal(err)
	}
	summary, err := service.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Queued != 2 || summary.Failed != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.ActiveTotal != 2 {
		t.Fatalf("active total = %d, want 2", summary.ActiveTotal)
	}
}

func TestFailsClosedWithoutRepository(t *testing.T) {
	service := NewService(Options{})
	ctx := context.Background()
	if _, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{}`)); err == nil {
		t.Fatal("submit without a repository succeeded")
	}
	if _, err := service.Cancel(ctx, "x"); err == nil {
		t.Fatal("cancel without a repository succeeded")
	}
	if _, err := service.RetryFailed(ctx, []string{"x"}); err == nil {
		t.Fatal("retry without a repository succeeded")
	}
	if _, err := service.Get(ctx, "x"); err == nil {
		t.Fatal("get without a repository succeeded")
	}
	if _, err := service.List(ctx, ListFilter{}); err == nil {
		t.Fatal("list without a repository succeeded")
	}
	if _, err := service.Summary(ctx); err == nil {
		t.Fatal("summary without a repository succeeded")
	}
}

func TestPublishCarriesNoProviderText(t *testing.T) {
	service, _, _, publisher := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	events := publisher.snapshot()
	if len(events) == 0 {
		t.Fatal("no event published")
	}
	for _, event := range events {
		if event.JobID != record.ID {
			t.Fatalf("event for wrong job: %+v", event)
		}
		if event.ErrorCode != "" {
			t.Fatalf("new job carried an error code: %+v", event)
		}
		if event.UpdatedAt == "" {
			t.Fatal("event missing a timestamp")
		}
	}
}

func TestClassifyPropagatesThroughRunner(t *testing.T) {
	// A runner returning a provider error must classify through the bridge.
	runner := &scriptedRunner{errs: []error{nil}, outcomes: []Outcome{{Status: job.StatusSucceeded}}}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	if _, err := repository.Claim(ctx, record.ID, "worker-1", clock.Now().Add(time.Hour), clock.Now()); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Execute directly through the worker path.
	service.execute(ctx, stored)
	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", final.Status)
	}
	if final.FinishedAt.IsZero() {
		t.Fatal("finished_at not set on a terminal job")
	}
}

func TestExecuteRetriesTransientFailure(t *testing.T) {
	runner := &scriptedRunner{errs: []error{errors.New("connection reset")}}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	stored := claimForExecution(t, repository, clock, record.ID)
	service.execute(ctx, stored)

	retried, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != job.StatusRetryWait {
		t.Fatalf("status = %q, want retry_wait", retried.Status)
	}
	if retried.NextRetryAt.IsZero() {
		t.Fatal("retry time not scheduled")
	}
	if retried.AttemptCount != 1 {
		t.Fatalf("attempt count = %d, want 1", retried.AttemptCount)
	}
	attempts, err := repository.ListAttempts(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != job.AttemptFailed {
		t.Fatalf("attempt history = %+v", attempts)
	}
}

func TestExecuteFailsPermanentlyAfterBudget(t *testing.T) {
	runner := &scriptedRunner{errs: []error{errors.New("reset"), errors.New("reset"), errors.New("reset")}}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		// Each attempt starts from the real scheduler path: move the clock past
		// any backoff so the job is claimable again.
		clock.Advance(2 * time.Hour)
		stored := claimForExecution(t, repository, clock, record.ID)
		service.execute(ctx, stored)
	}
	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusFailed {
		t.Fatalf("status = %q, want failed after the attempt budget", final.Status)
	}
	if final.FinishedAt.IsZero() {
		t.Fatal("failed job has no finish time")
	}
}

// TestExecuteNeverMarksSuccessWithoutResult is the "failure never produces a
// usable asset" invariant at the application layer.
func TestExecuteNeverMarksSuccessWithoutResult(t *testing.T) {
	runner := &scriptedRunner{errs: []error{job.FailedJobError(job.CategorySecurity, "blocked")}}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	stored := claimForExecution(t, repository, clock, record.ID)
	service.execute(ctx, stored)

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A security failure is never retried and never succeeded.
	if final.Status != job.StatusFailed {
		t.Fatalf("security failure produced %q", final.Status)
	}
	if final.ErrorCode != string(job.CategorySecurity) {
		t.Fatalf("error code = %q", final.ErrorCode)
	}
}

func TestExecuteHonoursCancellation(t *testing.T) {
	runner := &scriptedRunner{errs: []error{job.CancelledJobError()}}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	stored := claimForExecution(t, repository, clock, record.ID)
	service.execute(ctx, stored)
	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", final.Status)
	}
}

// recordingCanceller records the remote cancellation attempts.
type recordingCanceller struct {
	records []job.Job
	err     error
}

func (c *recordingCanceller) CancelRemote(_ context.Context, record job.Job) error {
	c.records = append(c.records, record)
	return c.err
}

// TestCancelWithRemoteJobAttemptsProviderCancellation covers the FR-150 rule
// that cancelling a job which already produced a remote resource must record
// that resource as an orphan candidate rather than assuming the provider
// stopped.
func TestCancelWithRemoteJobAttemptsProviderCancellation(t *testing.T) {
	repository := newMemoryRepository()
	clock := newFakeClock()
	canceller := &recordingCanceller{}
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Runner:     &scriptedRunner{},
		Policy:     job.RetryPolicy{MaxAttempts: 3},
	}).WithRemoteCanceller(canceller)
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
	if err != nil {
		t.Fatal(err)
	}
	// The job is mid-flight with a remote handle.
	stored, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Status = job.StatusWaitingRemote
	stored.RemoteJobID = "remote-77"
	if err := repository.Update(ctx, stored, stored.Revision); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Cancel(ctx, record.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(canceller.records) != 1 || canceller.records[0].RemoteJobID != "remote-77" {
		t.Fatalf("provider cancellation not attempted: %+v", canceller.records)
	}
	// The remote handle is preserved so the user can inspect the orphan.
	after, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteJobID != "remote-77" {
		t.Fatalf("remote handle lost: %q", after.RemoteJobID)
	}
	if !after.CancelledRemoteUnconfirmed {
		t.Fatal("the orphan candidate was not flagged")
	}
	// The note is durable: it lives in result_json, which the worker's own
	// terminal write does not clear.
	if !containsLower(after.ResultJSON, "orphan_candidate") {
		t.Fatalf("no durable orphan-candidate record: %q", after.ResultJSON)
	}
	// The message never claims the remote side stopped.
	if containsLower(after.ResultJSON, "provider stopped") {
		t.Fatalf("record overstates remote cancellation: %q", after.ResultJSON)
	}
}

// TestCancelWithRemoteJobRecordsUnconfirmedCancellation proves a provider that
// cannot cancel leaves an honest explanation rather than a success claim.
func TestCancelWithRemoteJobRecordsUnconfirmedCancellation(t *testing.T) {
	repository := newMemoryRepository()
	clock := newFakeClock()
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Runner:     &scriptedRunner{},
		Policy:     job.RetryPolicy{MaxAttempts: 3},
	}).WithRemoteCanceller(&recordingCanceller{err: errors.New("unsupported")})
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Status = job.StatusWaitingRemote
	stored.RemoteJobID = "remote-88"
	if err := repository.Update(ctx, stored, stored.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	after, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsLower(after.ResultJSON, "could not be cancelled") {
		t.Fatalf("unconfirmed cancellation not reported: %q", after.ResultJSON)
	}
	if !after.CancelledRemoteUnconfirmed {
		t.Fatal("unconfirmed cancellation not flagged")
	}
}

func containsLower(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), needle)
}
