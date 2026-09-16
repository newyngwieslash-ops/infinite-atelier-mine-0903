package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// pendingPoller parks a remote job for a fixed number of polls before it
// finishes, which is what a real asynchronous provider does. The counters are
// mutex-guarded because a worker goroutine drives it.
type pendingPoller struct {
	mu        sync.Mutex
	remaining int
	polls     int
	// onPoll runs inside Run on a poll pass, which is how a test injects a
	// concurrent event (a cancel) that lands while the provider call is in flight.
	onPoll func(record job.Job)
}

func (r *pendingPoller) Run(_ context.Context, record job.Job) (Outcome, error) {
	if record.RemoteJobID == "" {
		return Outcome{Status: job.StatusWaitingRemote, RemoteJobID: "remote-1"}, nil
	}
	r.mu.Lock()
	if r.remaining > 0 {
		r.remaining--
		r.polls++
		hook := r.onPoll
		r.mu.Unlock()
		if hook != nil {
			hook(record)
		}
		return Outcome{Status: job.StatusWaitingRemote, RemoteJobID: record.RemoteJobID, PollOnly: true}, nil
	}
	r.mu.Unlock()
	return Outcome{Status: job.StatusSucceeded, RemoteJobID: record.RemoteJobID}, nil
}

func (r *pendingPoller) pollCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.polls
}

// TestRemotePollIsPaced proves a parked remote job is not re-polled at
// scheduler speed.
//
// Without pacing the scheduler re-claims waiting_remote as soon as a worker is
// free, which is the provider's own HTTP round-trip time: a real video job
// would be polled hundreds of times per minute, hitting rate limits, and each
// poll would also drain the retry budget.
func TestRemotePollIsPaced(t *testing.T) {
	runner := &pendingPoller{remaining: 1000}
	repository := newMemoryRepository()
	clock := newFakeClock()
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Runner:     runner,
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, MaxAttempts: 3},
		// A long interval relative to the test: several scheduler passes must
		// happen inside one interval without a second poll.
		RemotePollInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx, 1, 2*time.Millisecond)

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
	if err != nil {
		t.Fatal(err)
	}
	// The first pass submits and parks the job. Give the scheduler many passes.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		stored, err := repository.Get(ctx, record.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == job.StatusWaitingRemote && stored.RemoteJobID != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	_ = service.Stop(ctx)

	// One poll is expected (the pass that discovered "not done yet"). The
	// interval then gates every further pass, so the count must stay far below
	// the number of scheduler passes that ran in the same window.
	if got := runner.pollCount(); got > 1 {
		t.Fatalf("the remote job was polled %d times inside one poll interval", got)
	}
}

// TestParkedPollDoesNotConsumeAttemptBudget proves a poll that only checks
// progress neither advances attempt_count nor writes an attempt row.
//
// A video job can legitimately need hundreds of polls, and it is limited to
// three attempts: counting polls as attempts would fail a job that was working
// exactly as designed, and each poll would add a history row.
func TestParkedPollDoesNotConsumeAttemptBudget(t *testing.T) {
	// One poll reports "still running", the next one completes.
	runner := &pendingPoller{remaining: 1}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
	if err != nil {
		t.Fatal(err)
	}
	// First pass: submit, which is real work and must be counted.
	claimed := claimForExecution(t, repository, clock, record.ID)
	service.execute(ctx, claimed)
	afterSubmit, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterSubmit.Status != job.StatusWaitingRemote || afterSubmit.RemoteJobID == "" {
		t.Fatalf("submit pass did not park the job: %+v", afterSubmit)
	}
	if afterSubmit.AttemptCount != 1 {
		t.Fatalf("submit attempt count = %d, want 1 (a submission is an attempt)", afterSubmit.AttemptCount)
	}

	// Second pass: a pure poll.
	clock.Advance(time.Hour)
	pollClaim := claimForExecution(t, repository, clock, record.ID)
	service.execute(ctx, pollClaim)
	afterPoll, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterPoll.Status != job.StatusWaitingRemote {
		t.Fatalf("status = %q, want waiting_remote", afterPoll.Status)
	}
	if afterPoll.AttemptCount != afterSubmit.AttemptCount {
		t.Fatalf("a poll consumed the attempt budget: %d -> %d", afterSubmit.AttemptCount, afterPoll.AttemptCount)
	}
	attempts, err := repository.ListAttempts(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt rows = %d, want 1 (a poll is not an attempt)", len(attempts))
	}

	// Third pass: the poll completes, which is real work and must be counted.
	// The clock advances past the poll interval the previous pass scheduled.
	clock.Advance(time.Hour)
	successClaim := claimForExecution(t, repository, clock, record.ID)
	service.execute(ctx, successClaim)
	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", final.Status)
	}
	if final.AttemptCount != 2 {
		t.Fatalf("attempt count = %d, want 2 after a submitting pass and a completing pass", final.AttemptCount)
	}
}

// TestRemotePollFailureStillRetries proves a failing poll is treated as real
// work: it advances the attempt budget and is retried, instead of looping
// forever as a "poll".
func TestRemotePollFailureStillRetries(t *testing.T) {
	runner := &failingPoller{}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed := claimForExecution(t, repository, clock, record.ID)
	service.execute(ctx, claimed)
	afterSubmit, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterSubmit.Status != job.StatusWaitingRemote {
		t.Fatalf("submit pass did not park the job: %+v", afterSubmit)
	}

	clock.Advance(time.Hour)
	// The poll fails; the job must land in retry_wait, not stay parked.
	pollClaim := claimForExecution(t, repository, clock, record.ID)
	service.execute(ctx, pollClaim)
	after, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != job.StatusRetryWait {
		t.Fatalf("status = %q, want retry_wait after a failing poll", after.Status)
	}
	if after.AttemptCount != 2 {
		t.Fatalf("attempt count = %d, want 2 (a failing poll is an attempt)", after.AttemptCount)
	}
	attempts, err := repository.ListAttempts(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempt rows = %d, want 2", len(attempts))
	}
	if attempts[1].Status != job.AttemptFailed {
		t.Fatalf("failing poll attempt status = %q, want failed", attempts[1].Status)
	}
}

// failingPoller submits successfully and then always fails a poll.
type failingPoller struct{}

func (failingPoller) Run(_ context.Context, record job.Job) (Outcome, error) {
	if record.RemoteJobID == "" {
		return Outcome{Status: job.StatusWaitingRemote, RemoteJobID: "remote-1"}, nil
	}
	return Outcome{}, context.DeadlineExceeded
}
