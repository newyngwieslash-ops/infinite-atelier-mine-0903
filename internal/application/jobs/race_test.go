package jobs

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// recordingJobRunner returns a scripted error and counts invocations.
type recordingJobRunner struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (r *recordingJobRunner) Run(context.Context, job.Job) (Outcome, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	if r.err != nil {
		return Outcome{}, r.err
	}
	return Outcome{Status: job.StatusSucceeded}, nil
}

func (r *recordingJobRunner) invocations() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestRecoverDownloadingResumesWithoutReCallingProvider is the end-to-end
// regression test for the wedge: a job that reached the downloading phase and
// then lost its worker must become runnable again with its resume marker
// intact, so the next pass fetches the bytes instead of calling the provider.
func TestRecoverDownloadingResumesWithoutReCallingProvider(t *testing.T) {
	runner := &recordingJobRunner{}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The provider produced results and the worker persisted that phase; the
	// process then died before the download finished.
	stored.Status = job.StatusDownloading
	stored.ResultJSON = `{"mode":"pending_download","urls":["https://cdn.example.com/a.png"]}`
	stored.LeaseOwner = "dead-worker"
	stored.LeaseExpiresAt = clock.Now().Add(-time.Hour)
	if err := repository.Update(ctx, stored, stored.Revision); err != nil {
		t.Fatal(err)
	}

	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if report.Redownload != 1 {
		t.Fatalf("report = %+v, want one redownload", report)
	}
	recovered, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != job.StatusQueued {
		t.Fatalf("status = %q, want queued so the fetch resumes", recovered.Status)
	}
	if !strings.Contains(recovered.ResultJSON, "pending_download") {
		t.Fatalf("the resume marker was lost: %q", recovered.ResultJSON)
	}
}

// TestRecoverDownloadingWithoutMarkerIsOrphaned proves a job claiming to be
// downloading without evidence of provider work is not silently re-run.
func TestRecoverDownloadingWithoutMarkerIsOrphaned(t *testing.T) {
	service, repository, clock, _ := newTestService(t, &recordingJobRunner{})
	ctx := context.Background()
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Status = job.StatusDownloading
	stored.LeaseOwner = "dead-worker"
	stored.LeaseExpiresAt = clock.Now().Add(-time.Hour)
	if err := repository.Update(ctx, stored, stored.Revision); err != nil {
		t.Fatal(err)
	}
	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Orphaned != 1 {
		t.Fatalf("report = %+v, want one orphan", report)
	}
}

// TestCancelDuringFailingAttemptStillTerminates is the regression test for a
// cancel that lands while the runner is failing: the job must end cancelled
// rather than staying non-terminal because the worker's revision went stale.
func TestCancelDuringFailingAttemptStillTerminates(t *testing.T) {
	runner := &recordingJobRunner{err: context.DeadlineExceeded}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.Claim(ctx, record.ID, "worker-1", clock.Now().Add(time.Hour), clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	// The user cancels while the attempt is in flight.
	if _, err := repository.RequestCancel(ctx, record.ID, clock.Now()); err != nil {
		t.Fatal(err)
	}
	// The attempt then fails with a retriable error.
	service.execute(ctx, claimed)

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled after a cancel during a failing attempt", final.Status)
	}
	if final.FinishedAt.IsZero() {
		t.Fatal("cancelled job has no finish time")
	}
	if final.Status.IsActive() {
		t.Fatal("cancelled job still occupies the scheduler")
	}
}

// TestFailureAfterConcurrentWriteIsRecorded proves the worker re-reads the row
// before writing its outcome, so an unrelated concurrent write cannot silently
// discard a failure and leave the job running.
func TestFailureAfterConcurrentWriteIsRecorded(t *testing.T) {
	runner := &recordingJobRunner{err: context.DeadlineExceeded}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.Claim(ctx, record.ID, "worker-1", clock.Now().Add(time.Hour), clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	// An external write bumps the revision while the attempt runs.
	current, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Priority = 5
	if err := repository.Update(ctx, current, current.Revision); err != nil {
		t.Fatal(err)
	}
	service.execute(ctx, claimed)

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != job.StatusRetryWait {
		t.Fatalf("status = %q, want retry_wait (the failure must be recorded)", final.Status)
	}
	if final.ErrorCode != string(job.CategoryTimeout) {
		t.Fatalf("error code = %q, want timeout", final.ErrorCode)
	}
	if final.Status.IsActive() && final.NextRetryAt.IsZero() {
		t.Fatal("retry scheduled without a retry time")
	}
}

// TestFailureAfterCancelAndConcurrentWriteStillTerminates combines both races:
// a cancel and an unrelated write land while a failing attempt is running. The
// job must still reach a terminal state.
func TestFailureAfterCancelAndConcurrentWriteStillTerminates(t *testing.T) {
	runner := &recordingJobRunner{err: context.DeadlineExceeded}
	service, repository, clock, _ := newTestService(t, runner)
	ctx := context.Background()

	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.Claim(ctx, record.ID, "worker-1", clock.Now().Add(time.Hour), clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RequestCancel(ctx, record.ID, clock.Now()); err != nil {
		t.Fatal(err)
	}
	priority, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	priority.Priority = 3
	if err := repository.Update(ctx, priority, priority.Revision); err != nil {
		t.Fatal(err)
	}
	service.execute(ctx, claimed)

	final, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Status.IsTerminal() {
		t.Fatalf("status = %q, want a terminal state", final.Status)
	}
	if final.Status != job.StatusCancelled {
		t.Fatalf("status = %q, want cancelled (the cancel must win)", final.Status)
	}
}
