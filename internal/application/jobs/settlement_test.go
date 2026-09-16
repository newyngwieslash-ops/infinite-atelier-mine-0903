package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// TestLeaseExceedsDownloadTimeout pins the relationship between the lease and
// the longest single network operation.
//
// If the lease expired during a legitimate download, a second worker could claim
// the job while the first was still fetching, and both would write the same
// job's outcome. The lease must therefore outlast the download.
func TestLeaseExceedsDownloadTimeout(t *testing.T) {
	download := phttp.DefaultDownloadLimits()
	if DefaultLeaseTTL <= download.TotalTimeout {
		t.Fatalf("lease %s must exceed the download total timeout %s, otherwise a download can outlive its lease",
			DefaultLeaseTTL, download.TotalTimeout)
	}
}

// TestParkedCancelIsSettled proves a job cancelled while parked (no worker in
// flight) reaches a terminal state.
//
// The candidate query excludes cancel-flagged jobs from ordinary scheduling, so
// without a settlement path the job would show as active forever and a restart
// would even resume it.
func TestParkedCancelIsSettled(t *testing.T) {
	for _, parked := range []job.Status{job.StatusWaitingRemote, job.StatusDownloading, job.StatusVerifying, job.StatusQueued, job.StatusRetryWait, job.StatusRecovering} {
		t.Run(string(parked), func(t *testing.T) {
			runner := &recordingJobRunner{}
			service, repository, _, _ := newTestService(t, runner)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
			if err != nil {
				t.Fatal(err)
			}
			stored, err := repository.Get(ctx, record.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Park the job in the state under test with no live lease.
			stored.Status = parked
			stored.LeaseOwner = ""
			stored.LeaseExpiresAt = time.Time{}
			if parked == job.StatusWaitingRemote {
				stored.RemoteJobID = "remote-parked"
			}
			if err := repository.Update(ctx, stored, stored.Revision); err != nil {
				t.Fatal(err)
			}

			if _, err := service.Cancel(ctx, record.ID); err != nil {
				t.Fatalf("Cancel: %v", err)
			}

			// Drive the scheduler so the settlement pass can run.
			service.Start(ctx, 1, 5*time.Millisecond)
			waitForStatus(t, repository, record.ID, job.StatusCancelled, 3*time.Second)
			_ = service.Stop(ctx)

			final, err := repository.Get(ctx, record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if final.Status != job.StatusCancelled {
				t.Fatalf("status = %q, want cancelled", final.Status)
			}
			if !final.FinishedAt.IsZero() == false {
				t.Fatal("cancelled job has no finish time")
			}
			if final.Status.IsActive() {
				t.Fatal("cancelled job still counts as active")
			}
			// The runner must not have produced work for a cancelled job.
			if runner.invocations() != 0 {
				t.Fatalf("runner executed %d time(s) for a cancelled job", runner.invocations())
			}
		})
	}
}

// TestRecoverSettlesCancelledJob proves a cancel that predates a restart is
// honoured rather than undone by the recovery scanner.
func TestRecoverSettlesCancelledJob(t *testing.T) {
	for _, parked := range []job.Status{job.StatusWaitingRemote, job.StatusDownloading, job.StatusRunning} {
		t.Run(string(parked), func(t *testing.T) {
			service, repository, clock, _ := newTestService(t, &recordingJobRunner{})
			ctx := context.Background()

			record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
			if err != nil {
				t.Fatal(err)
			}
			stored, err := repository.Get(ctx, record.ID)
			if err != nil {
				t.Fatal(err)
			}
			stored.Status = parked
			stored.CancelRequested = true
			stored.RemoteJobID = "remote-parked"
			stored.LeaseOwner = "dead-worker"
			stored.LeaseExpiresAt = clock.Now().Add(-time.Hour)
			if err := repository.Update(ctx, stored, stored.Revision); err != nil {
				t.Fatal(err)
			}

			report, err := service.Recover(ctx)
			if err != nil {
				t.Fatalf("Recover: %v", err)
			}
			if report.Cancelled != 1 {
				t.Fatalf("report = %+v, want one cancelled settlement", report)
			}
			// The job must not be resumed by any other disposition.
			if report.Resumed != 0 || report.Requeued != 0 || report.Redownload != 0 || report.Reverify != 0 {
				t.Fatalf("a cancelled job was resumed: %+v", report)
			}
			final, err := repository.Get(ctx, record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if final.Status != job.StatusCancelled {
				t.Fatalf("status = %q, want cancelled", final.Status)
			}
		})
	}
}

// TestOrphanNoteSurvivesTerminalWrite proves the orphan-candidate explanation
// recorded at cancel time is still present after the worker settles the job.
func TestOrphanNoteSurvivesTerminalWrite(t *testing.T) {
	runner := &recordingJobRunner{}
	service, repository, clock, _ := newTestService(t, runner)
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
	stored.RemoteJobID = "remote-orphan"
	if err := repository.Update(ctx, stored, stored.Revision); err != nil {
		t.Fatal(err)
	}

	cancelled, err := service.Cancel(ctx, record.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !cancelled.CancelledRemoteUnconfirmed {
		t.Fatal("the orphan candidate was not flagged")
	}

	// The worker then settles the job. Claim it through the real path so the
	// worker holds the current revision.
	claimed, err := repository.Claim(ctx, record.ID, "worker-1", clock.Now().Add(time.Hour), clock.Now())
	if err != nil {
		t.Fatalf("claim for settlement: %v", err)
	}
	service.execute(ctx, claimed)

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", final.Status)
	}
	if !final.CancelledRemoteUnconfirmed {
		t.Fatal("the orphan flag was wiped by the terminal write")
	}
	if final.ResultJSON == "" {
		t.Fatal("the orphan note was wiped by the terminal write")
	}
	if !containsSubstring(final.ResultJSON, "orphan_candidate") {
		t.Fatalf("orphan note not preserved: %q", final.ResultJSON)
	}
	if final.RemoteJobID != "remote-orphan" {
		t.Fatalf("the remote handle was lost: %q", final.RemoteJobID)
	}
}

func containsSubstring(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOfSubstring(haystack, needle) >= 0)
}

func indexOfSubstring(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}

// cancellingRunner interrupts its own attempt, which is what a shutdown looks
// like from inside the worker: the context is cancelled while Run is in flight.
type cancellingRunner struct {
	cancel context.CancelFunc
}

func (r *cancellingRunner) Run(ctx context.Context, _ job.Job) (Outcome, error) {
	r.cancel()
	return Outcome{}, ctx.Err()
}

// TestShutdownDuringAttemptSettlesJob proves an attempt interrupted by shutdown
// still reaches a terminal state with its attempt row finished.
//
// Worker queries fail on a cancelled context, so a terminal write on the worker
// context would leave the job claimed as "running" and the attempt row open
// forever; only the next restart would notice. The settle context exists to make
// the write land, and this test fails if that is undone.
func TestShutdownDuringAttemptSettlesJob(t *testing.T) {
	repository := newMemoryRepository()
	clock := newFakeClock()
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	runner := &cancellingRunner{cancel: cancelWorker}
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Publisher:  &recordingPublisher{},
		Runner:     runner,
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, MaxAttempts: 3},
	})

	record, _, err := service.Submit(context.Background(), submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.Claim(context.Background(), record.ID, "worker-1", clock.Now().Add(time.Hour), clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	// The attempt runs until shutdown cancels the worker context mid-flight.
	service.execute(workerCtx, claimed)
	if workerCtx.Err() == nil {
		t.Fatal("the runner did not cancel the worker context")
	}

	final, err := repository.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled after shutdown", final.Status)
	}
	if final.LeaseOwner != "" || !final.LeaseExpiresAt.IsZero() {
		t.Fatalf("job left claimed after shutdown: %+v", final)
	}
	if final.FinishedAt.IsZero() {
		t.Fatal("interrupted job has no finish time")
	}
	attempts, err := repository.ListAttempts(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt rows = %d, want 1", len(attempts))
	}
	if attempts[0].Status != job.AttemptCancelled {
		t.Fatalf("attempt status = %q, want cancelled (the history row is the evidence the run ended)",
			attempts[0].Status)
	}
	if attempts[0].FinishedAt.IsZero() {
		t.Fatal("interrupted attempt was never finished")
	}
}

// TestShutdownReturnsClaimedJobToQueue proves a claim that no worker took over
// is handed back, instead of sitting on a fresh lease for its whole TTL.
func TestShutdownReturnsClaimedJobToQueue(t *testing.T) {
	service, repository, _, _ := newTestService(t, &recordingJobRunner{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}

	// No reader: the hand-off blocks, so only the shutdown branch can proceed.
	work := make(chan job.Job)
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.dispatch(ctx, work)
	}()
	waitForClaim(t, repository, record.ID, 3*time.Second)

	// The claim exists; now the process begins shutting down.
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch did not stop after cancellation")
	}

	final, err := repository.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusQueued {
		t.Fatalf("status = %q, want queued so the next start runs it", final.Status)
	}
	if final.LeaseOwner != "" || !final.LeaseExpiresAt.IsZero() {
		t.Fatalf("lease still held after shutdown: owner=%q expires=%s", final.LeaseOwner, final.LeaseExpiresAt)
	}
}

// waitForClaim blocks until the job holds a lease, or fails the test.
func waitForClaim(t *testing.T, repository *memoryRepository, id string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		record, err := repository.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if record.LeaseOwner != "" {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the job was never claimed")
}

// failingSettleRepository rejects a bounded number of writes, so the worker's
// own outcome write can fail while the recovery write that follows it succeeds.
// Failing every write would model an unusable disk rather than the race this
// test is about.
type failingSettleRepository struct {
	*memoryRepository
	mu sync.Mutex
	// failNext is how many of the next Update calls are rejected.
	failNext int
}

func (r *failingSettleRepository) armFailures(count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failNext = count
}

func (r *failingSettleRepository) Update(ctx context.Context, record job.Job, expectedRevision int64) error {
	r.mu.Lock()
	if r.failNext > 0 {
		r.failNext--
		r.mu.Unlock()
		return job.FailedJobError(job.CategoryStorage, "The job could not be updated.")
	}
	r.mu.Unlock()
	return r.memoryRepository.Update(ctx, record, expectedRevision)
}

// successOnShutdownRunner cancels its own worker context and then reports
// success, which is what a provider call looks like when it completes just as
// the app begins shutting down.
type successOnShutdownRunner struct {
	cancel context.CancelFunc
}

func (r *successOnShutdownRunner) Run(_ context.Context, _ job.Job) (Outcome, error) {
	r.cancel()
	return Outcome{Status: job.StatusSucceeded}, nil
}

// TestShutdownAfterSuccessfulRunStillClosesTheAttempt proves the attempt history
// row is closed even when the worker context is cancelled after a successful run.
//
// The runner finished its work and reported success, so the job outcome is
// written on a settling context — but the history row would still be written on
// the cancelled worker context without the same treatment, leaving it "running"
// forever. A permanently running attempt is what the Job Center reports as an
// in-flight execution, so the run would misreport itself.
func TestShutdownAfterSuccessfulRunStillClosesTheAttempt(t *testing.T) {
	repository := newMemoryRepository()
	clock := newFakeClock()
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Publisher:  &recordingPublisher{},
		Runner:     &successOnShutdownRunner{cancel: cancelWorker},
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, MaxAttempts: 3},
	})

	record, _, err := service.Submit(context.Background(), submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.Claim(context.Background(), record.ID, "worker-1", clock.Now().Add(time.Hour), clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	service.execute(workerCtx, claimed)
	if workerCtx.Err() == nil {
		t.Fatal("the runner did not cancel the worker context")
	}

	final, err := repository.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", final.Status)
	}
	if final.LeaseOwner != "" {
		t.Fatalf("the job was left claimed by %q", final.LeaseOwner)
	}
	attempts, err := repository.ListAttempts(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt rows = %d, want 1", len(attempts))
	}
	if attempts[0].Status != job.AttemptSucceeded {
		t.Fatalf("attempt status = %q, want succeeded (a shutdown must not leave it running)", attempts[0].Status)
	}
	if attempts[0].FinishedAt.IsZero() {
		t.Fatal("the successful attempt was never closed out")
	}
}

// TestCancelSettlementSurvivesCancelledWorkerContext proves the settlement
// writes do not depend on a live worker context.
//
// A worker can be handed a cancel-flagged job and then have its context
// cancelled by shutdown before it settles. Every read and write on that path
// would fail on the cancelled context, leaving the row claimed as running on a
// live 30-minute lease with no worker to finish it — a state only a restart
// could clear, and one the startup scanner deliberately skips while the lease
// looks live.
func TestCancelSettlementSurvivesCancelledWorkerContext(t *testing.T) {
	service, repository, clock, _ := newTestService(t, &recordingJobRunner{})
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	// The user cancels, then the scheduler claims the job so a worker can settle
	// it — and that worker's context is cancelled before it runs.
	if _, err := service.Cancel(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	claimed := claimForExecution(t, repository, clock, record.ID)
	workerCtx, cancelWorker := context.WithCancel(ctx)
	cancelWorker()
	service.execute(workerCtx, claimed)

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled after a shutdown during settlement", final.Status)
	}
	if final.LeaseOwner != "" || !final.LeaseExpiresAt.IsZero() {
		t.Fatalf("the job was left claimed: owner=%q expires=%s", final.LeaseOwner, final.LeaseExpiresAt)
	}
	if final.FinishedAt.IsZero() {
		t.Fatal("the settled job has no finish time")
	}
}

// TestCancelDuringUnrecordableOutcomeStillSettles is the regression test for a
// cancel that lands while the worker is between its post-run refresh and its
// terminal write, on a job whose outcome write then fails.
//
// The worker's own write conflicts, so it hands the job to release(). release()
// must settle it: returning there — because the job is cancel-flagged — would
// leave the row claimed as running with a live 30-minute lease, and nothing
// would settle it until the lease expired or the app restarted, while the UI
// kept showing an active job the user already cancelled.
func TestCancelDuringUnrecordableOutcomeStillSettles(t *testing.T) {
	base := newMemoryRepository()
	repository := &failingSettleRepository{memoryRepository: base}
	clock := newFakeClock()
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Publisher:  &recordingPublisher{},
		Runner:     &recordingJobRunner{},
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, MaxAttempts: 3},
	})
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := base.Claim(ctx, record.ID, "worker-1", clock.Now().Add(time.Hour), clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	// The user cancels, and the worker's outcome write then fails.
	if _, err := base.RequestCancel(ctx, record.ID, clock.Now()); err != nil {
		t.Fatal(err)
	}
	repository.armFailures(1)
	service.execute(ctx, claimed)

	final, err := base.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled: a cancelled job must not stay claimed as running", final.Status)
	}
	if final.LeaseOwner != "" || !final.LeaseExpiresAt.IsZero() {
		t.Fatalf("the cancel left a live lease behind: owner=%q expires=%s", final.LeaseOwner, final.LeaseExpiresAt)
	}
	if final.FinishedAt.IsZero() {
		t.Fatal("cancelled job has no finish time")
	}
}

// TestCancelDuringPollStillSettles proves a cancel that lands while a poll is in
// flight settles the job instead of parking it again.
//
// The cancel is injected from inside the poll itself, so `execute` really
// reaches its PollOnly branch with the flag set — cancelling before the claim
// would be settled by the top-of-function branch and would not exercise this
// path at all.
func TestCancelDuringPollStillSettles(t *testing.T) {
	service, repository, clock, _ := newTestService(t, nil)
	ctx := context.Background()
	runner := &pendingPoller{remaining: 100}
	runner.onPoll = func(record job.Job) {
		if _, err := service.Cancel(ctx, record.ID); err != nil {
			t.Errorf("Cancel during poll: %v", err)
		}
	}
	service.runner = runner

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Submit and park.
	service.execute(ctx, claimForExecution(t, repository, clock, record.ID))
	parked, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parked.Status != job.StatusWaitingRemote {
		t.Fatalf("precondition: status = %q", parked.Status)
	}

	clock.Advance(time.Hour)
	service.execute(ctx, claimForExecution(t, repository, clock, record.ID))

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled after a cancel during a poll", final.Status)
	}
	if final.LeaseOwner != "" {
		t.Fatalf("a cancelled poll left its lease held by %q", final.LeaseOwner)
	}
	if !final.NextRetryAt.IsZero() {
		t.Fatalf("a cancelled job was left parked for another poll: %s", final.NextRetryAt)
	}
	if runner.pollCount() != 1 {
		t.Fatalf("polls = %d, want 1 (the cancelled poll must not have been parked for another)", runner.pollCount())
	}
}

// TestOrphanNoteSurvivesCancelledPoll proves the durable orphan note recorded at
// cancel time is still present when a cancel settles a job mid-poll.
//
// The cancel is injected inside the poll, so the PollOnly branch is the one that
// settles the job; a poll outcome carries an empty result_json, so without the
// note guard the durable explanation would be erased on exactly this path.
func TestOrphanNoteSurvivesCancelledPoll(t *testing.T) {
	service, repository, clock, _ := newTestService(t, nil)
	ctx := context.Background()
	runner := &pendingPoller{remaining: 100}
	runner.onPoll = func(record job.Job) {
		if _, err := service.Cancel(ctx, record.ID); err != nil {
			t.Errorf("Cancel during poll: %v", err)
		}
	}
	service.runner = runner

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
	if err != nil {
		t.Fatal(err)
	}
	service.execute(ctx, claimForExecution(t, repository, clock, record.ID))

	clock.Advance(time.Hour)
	service.execute(ctx, claimForExecution(t, repository, clock, record.ID))

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", final.Status)
	}
	if !final.CancelledRemoteUnconfirmed {
		t.Fatal("precondition: the orphan candidate was not flagged by the cancel")
	}
	if final.ResultJSON == "" || !containsSubstring(final.ResultJSON, "orphan_candidate") {
		t.Fatalf("the orphan note was erased by the poll settle: %q", final.ResultJSON)
	}
}

// TestOutOfRangeProgressIsClamped proves a provider-reported percentage outside
// the column's range cannot make the outcome unwritable.
//
// The value is remote input written into a CHECK-constrained column; a provider
// reporting 120 would otherwise fail the row update even after the artifact was
// fetched, producing a retry loop that never settles.
func TestOutOfRangeProgressIsClamped(t *testing.T) {
	cases := []struct {
		name     string
		reported int
		want     int
	}{
		{"above 100", 120, 100},
		{"negative", -5, 0},
		{"in range", 42, 42},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			reported := testCase.reported
			runner := &scriptedRunner{outcomes: []Outcome{{
				Status:   job.StatusWaitingRemote,
				PollOnly: true,
				Progress: &reported,
			}}}
			service, repository, clock, _ := newTestService(t, runner)
			ctx := context.Background()
			record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
			if err != nil {
				t.Fatal(err)
			}
			service.execute(ctx, claimForExecution(t, repository, clock, record.ID))

			final, err := repository.Get(ctx, record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if final.Progress == nil {
				t.Fatal("progress was dropped instead of being clamped")
			}
			if *final.Progress != testCase.want {
				t.Fatalf("progress = %d, want %d", *final.Progress, testCase.want)
			}
		})
	}
}

// TestReleaseIsNotImmediate proves a release schedules a real wait, so a failed
// write cannot spin the scheduler.
func TestReleaseIsNotImmediate(t *testing.T) {
	service, repository, clock, _ := newTestService(t, &recordingJobRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	service.release(ctx, record, job.CategoryStorage, "write failed")

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusRetryWait {
		t.Fatalf("status = %q, want retry_wait", final.Status)
	}
	if !final.NextRetryAt.After(clock.Now()) {
		t.Fatalf("release scheduled an immediate retry (%s), which would spin the scheduler", final.NextRetryAt)
	}
}
