package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

func markStatus(t *testing.T, repository *memoryRepository, id string, status job.Status, mutate func(*job.Job)) job.Job {
	t.Helper()
	record, err := repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	record.Status = status
	if mutate != nil {
		mutate(&record)
	}
	if err := repository.Update(context.Background(), record, record.Revision); err != nil {
		t.Fatal(err)
	}
	return record
}

// TestRecoverRequeuesRunningWithoutRemote proves a job interrupted mid-attempt
// with no remote side effect simply runs again (AC-FOUND-006 crash/restart).
func TestRecoverRequeuesRunningWithoutRemote(t *testing.T) {
	service, repository, clock, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a process that died while running: running, no live lease.
	markStatus(t, repository, record.ID, job.StatusRunning, func(record *job.Job) {
		record.LeaseOwner = "dead-worker"
		record.LeaseExpiresAt = clock.Now().Add(-time.Minute)
	})

	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if report.Requeued != 1 {
		t.Fatalf("report = %+v, want one requeue", report)
	}
	after, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != job.StatusQueued {
		t.Fatalf("status = %q, want queued", after.Status)
	}
	if after.LeaseOwner != "" || !after.LeaseExpiresAt.IsZero() {
		t.Fatalf("lease not released: %+v", after)
	}
}

// TestRecoverResumesRemoteWithoutResubmitting is the central safety rule: a job
// that already obtained a remote ID is polled, never submitted again, because a
// re-submit could double-charge the user.
func TestRecoverResumesRemoteWithoutResubmitting(t *testing.T) {
	service, repository, clock, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
	if err != nil {
		t.Fatal(err)
	}
	markStatus(t, repository, record.ID, job.StatusRunning, func(record *job.Job) {
		record.RemoteJobID = "remote-42"
		record.LeaseOwner = "dead-worker"
		record.LeaseExpiresAt = clock.Now().Add(-time.Minute)
	})

	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Resumed != 1 || report.Requeued != 0 {
		t.Fatalf("report = %+v, want one resume and no requeue", report)
	}
	after, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != job.StatusWaitingRemote {
		t.Fatalf("status = %q, want waiting_remote", after.Status)
	}
	if after.RemoteJobID != "remote-42" {
		t.Fatalf("remote id lost: %q", after.RemoteJobID)
	}
}

func TestRecoverWaitingRemoteWithoutIDIsOrphaned(t *testing.T) {
	service, repository, _, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeVideoGeneration, `{"prompt":"a clip"}`))
	if err != nil {
		t.Fatal(err)
	}
	markStatus(t, repository, record.ID, job.StatusWaitingRemote, nil)

	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Orphaned != 1 {
		t.Fatalf("report = %+v, want one orphan", report)
	}
	after, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != job.StatusOrphaned {
		t.Fatalf("status = %q, want orphaned", after.Status)
	}
	// Diagnostics are preserved rather than discarded.
	if after.ErrorCode == "" || after.ErrorMessage == "" {
		t.Fatalf("orphaned job has no diagnostics: %+v", after)
	}
	if after.FinishedAt.IsZero() {
		t.Fatal("orphaned job has no finish time")
	}
}

func TestRecoverDownloadingAndVerifying(t *testing.T) {
	service, repository, clock, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()

	downloading, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	markStatus(t, repository, downloading.ID, job.StatusDownloading, func(record *job.Job) {
		record.RemoteJobID = "remote-1"
		record.LeaseOwner = "dead"
		record.LeaseExpiresAt = clock.Now().Add(-time.Minute)
	})
	verifying, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"two"}`))
	if err != nil {
		t.Fatal(err)
	}
	markStatus(t, repository, verifying.ID, job.StatusVerifying, func(record *job.Job) {
		record.ResultJSON = `{"storageKey":"abc"}`
		record.LeaseOwner = "dead"
		record.LeaseExpiresAt = clock.Now().Add(-time.Minute)
	})

	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Redownload != 1 || report.Reverify != 1 {
		t.Fatalf("report = %+v, want one redownload and one reverify", report)
	}
	// A downloading job is re-queued so the runner resumes the fetch from the
	// persisted marker; it must not stay parked, and it must not re-submit.
	downloadRecord, err := repository.Get(ctx, downloading.ID)
	if err != nil {
		t.Fatal(err)
	}
	if downloadRecord.Status != job.StatusQueued {
		t.Fatalf("download status = %q, want queued so the fetch resumes", downloadRecord.Status)
	}
	if downloadRecord.RemoteJobID != "remote-1" {
		t.Fatalf("download job lost its remote handle: %q", downloadRecord.RemoteJobID)
	}
	verifyRecord, err := repository.Get(ctx, verifying.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verifyRecord.Status != job.StatusVerifying {
		t.Fatalf("verify status = %q", verifyRecord.Status)
	}
}

func TestRecoverSkipsLiveLease(t *testing.T) {
	service, repository, clock, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	// A live lease means another worker owns it right now.
	markStatus(t, repository, record.ID, job.StatusRunning, func(record *job.Job) {
		record.LeaseOwner = "live-worker"
		record.LeaseExpiresAt = clock.Now().Add(time.Hour)
	})
	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total() != 0 {
		t.Fatalf("recovery touched a live job: %+v", report)
	}
	after, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != job.StatusRunning || after.LeaseOwner != "live-worker" {
		t.Fatalf("live job disturbed: %+v", after)
	}
}

func TestRecoverNeverTouchesTerminalJobs(t *testing.T) {
	service, repository, _, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	for index, status := range []job.Status{job.StatusSucceeded, job.StatusFailed, job.StatusCancelled, job.StatusOrphaned, job.StatusRemoteOnly} {
		record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"p`+itoa(index)+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		markStatus(t, repository, record.ID, status, nil)
	}
	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total() != 0 {
		t.Fatalf("recovery touched terminal jobs: %+v", report)
	}
}

func TestRecoverHonoursCancelRequested(t *testing.T) {
	service, repository, clock, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	// The user asked to cancel, then the process died mid-attempt.
	markStatus(t, repository, record.ID, job.StatusRunning, func(record *job.Job) {
		record.CancelRequested = true
		record.LeaseOwner = "dead"
		record.LeaseExpiresAt = clock.Now().Add(-time.Minute)
	})
	if _, err := service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", after.Status)
	}
}

func TestRecoverFailsClosedWithoutRepository(t *testing.T) {
	service := NewService(Options{})
	if _, err := service.Recover(context.Background()); err == nil {
		t.Fatal("recovery without a repository succeeded")
	}
}

// TestCrashWithinLeaseStillRuns is the regression test for the crash window the
// startup scanner cannot cover.
//
// Recovery deliberately skips a job whose lease still looks live, because it
// cannot distinguish a crash from another running process. The most common crash
// point — after StartAttempt wrote attempt row N, before the settle — therefore
// leaves the job's counter reading its pre-crash value while the row exists. The
// lease is 30 minutes, so a restart inside it skips reconciliation entirely and
// the scheduler reclaims the job as soon as the lease expires or the process is
// restarted without a lease.
//
// Numbering the new attempt from the stored rows closes that window: without it,
// StartAttempt collides with the orphaned row and the job cycles through
// claim -> record-failure -> release forever, never executing the runner.
func TestCrashWithinLeaseStillRuns(t *testing.T) {
	runner := &recordingJobRunner{}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	// The crash: attempt row 1 exists, the counter was never advanced, and the
	// lease from the dead worker has not expired yet.
	if err := repository.StartAttempt(ctx, job.Attempt{
		ID:            "attempt-crashed",
		JobID:         record.ID,
		AttemptNumber: 1,
		Status:        job.AttemptRunning,
		StartedAt:     clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AttemptCount != 0 {
		t.Fatalf("precondition: attempt count = %d, want the pre-crash value 0", stored.AttemptCount)
	}
	stored.Status = job.StatusRunning
	stored.LeaseOwner = "dead-worker"
	// Still live as far as the scanner is concerned.
	stored.LeaseExpiresAt = clock.Now().Add(20 * time.Minute)
	if err := repository.Update(ctx, stored, stored.Revision); err != nil {
		t.Fatal(err)
	}

	// The scanner skips it (by design), so the counter stays stale.
	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total() != 0 {
		t.Fatalf("recovery touched a live-lease job: %+v", report)
	}

	// The lease expires and the scheduler claims it. The attempt must be
	// recordable, which is what the row-based numbering guarantees.
	clock.Advance(time.Hour)
	claimed := claimForExecution(t, repository, clock, record.ID)
	service.execute(ctx, claimed)

	if runner.invocations() != 1 {
		t.Fatalf("runner executed %d time(s), want 1: the job could not start its next attempt", runner.invocations())
	}
	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded (the collision would have left it retrying forever)", final.Status)
	}
	attempts, err := repository.ListAttempts(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempt rows = %d, want 2 (the crashed row and the new one)", len(attempts))
	}
	// Each number is unique, which is the invariant the collision violated.
	seen := map[int]bool{}
	for _, attempt := range attempts {
		if seen[attempt.AttemptNumber] {
			t.Fatalf("attempt number %d was reused", attempt.AttemptNumber)
		}
		seen[attempt.AttemptNumber] = true
	}
}

// TestRecoverReconcilesAttemptCounterAfterCrash is the regression test for a
// crash between StartAttempt and the settle.
//
// The process dies after the attempt row is written but before the job's
// attempt_count is raised. Recovery sees a job whose counter still reads zero.
// If it re-queues without reconciling, the next worker writes attempt_number 1
// again, the unique constraint rejects it, and the job cycles forever without
// ever running.
func TestRecoverReconcilesAttemptCounterAfterCrash(t *testing.T) {
	runner := &recordingJobRunner{}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	// The attempt row exists; the job row was never updated.
	if err := repository.StartAttempt(ctx, job.Attempt{
		ID:            "attempt-crashed",
		JobID:         record.ID,
		AttemptNumber: 2,
		Status:        job.AttemptRunning,
		StartedAt:     clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AttemptCount != 0 {
		t.Fatalf("precondition: attempt count = %d, want the pre-crash value 0", stored.AttemptCount)
	}
	stored.Status = job.StatusRunning
	stored.LeaseOwner = "dead-worker"
	stored.LeaseExpiresAt = clock.Now().Add(-time.Minute)
	if err := repository.Update(ctx, stored, stored.Revision); err != nil {
		t.Fatal(err)
	}

	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if report.Requeued != 1 {
		t.Fatalf("report = %+v, want one requeue", report)
	}
	recovered, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.AttemptCount < 2 {
		t.Fatalf("attempt count = %d, want it reconciled to the highest existing attempt (2)", recovered.AttemptCount)
	}

	// The next attempt must be recordable: this is the failure the
	// reconciliation prevents.
	if err := repository.StartAttempt(ctx, job.Attempt{
		ID:            "attempt-next",
		JobID:         record.ID,
		AttemptNumber: recovered.AttemptCount + 1,
		Status:        job.AttemptRunning,
		StartedAt:     clock.Now(),
	}); err != nil {
		t.Fatalf("the next attempt could not be recorded: %v", err)
	}
}
