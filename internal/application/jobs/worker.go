package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// Start launches the scheduler loop and the worker pool. It is idempotent: a
// second call is a no-op while running.
func (s *Service) Start(ctx context.Context, workerCount int, interval time.Duration) {
	if s == nil || s.repository == nil || s.runner == nil {
		return
	}
	if workerCount <= 0 {
		workerCount = DefaultWorkerCount
	}
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.stopping = false
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	work := make(chan job.Job)
	for index := 0; index < workerCount; index++ {
		s.wg.Add(1)
		go s.workerLoop(runCtx, work)
	}
	s.wg.Add(1)
	go s.scheduleLoop(runCtx, work, interval)
}

// Stop halts the scheduler and waits for in-flight attempts to finish or the
// context to expire. It is safe to call more than once.
func (s *Service) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if !s.started || s.stopping {
		s.mu.Unlock()
		return nil
	}
	s.stopping = true
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// Shutdown must not hang on an uncontrolled remote operation
		// (ARCHITECTURE §6.2). The workers observe cancellation and unwind; if
		// they are still mid-network the process exit ends them.
		return ctx.Err()
	}
}

// scheduleLoop claims runnable jobs and hands them to workers.
func (s *Service) scheduleLoop(ctx context.Context, work chan<- job.Job, interval time.Duration) {
	defer s.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			close(work)
			return
		case <-s.wake:
		case <-ticker.C:
		}
		if s.Paused() {
			continue
		}
		s.dispatch(ctx, work)
	}
}

// settleContext returns a context for terminal writes that must succeed even
// while the worker context is cancelled.
//
// Job queries fail on a cancelled context, so a shutdown that used the worker
// context would leave every in-flight job claimed and every abandoned claim
// held for its whole lease. The values of the parent are kept; only the
// cancellation is dropped, and the caller applies its own timeout.
func settleContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), settleTimeout)
}

// settleTimeout bounds a terminal write. It is short because the operations are
// local database writes.
const settleTimeout = 5 * time.Second

// dispatch claims as many runnable jobs as the workers can take. A bounded
// batch keeps each pass short so cancellation stays responsive.
func (s *Service) dispatch(ctx context.Context, work chan<- job.Job) {
	const batch = 16
	candidates, err := s.repository.ClaimableCandidates(ctx, s.now(), batch)
	if err != nil || len(candidates) == 0 {
		return
	}
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return
		}
		if s.Paused() {
			return
		}
		// Honour dependencies before spending a worker slot.
		if satisfied, reason := s.dependenciesSatisfied(ctx, candidate); !satisfied {
			if err := s.failDependency(ctx, candidate, reason); err != nil {
				// A persistence failure here must not stop the scheduler; the
				// job stays queued and is retried on the next pass.
				continue
			}
			continue
		}
		claim, err := s.repository.Claim(ctx, candidate.ID, s.newID("worker"), s.now().Add(DefaultLeaseTTL), s.now())
		if err != nil {
			// Lost the race or the job moved on; try the next candidate.
			continue
		}
		select {
		case work <- claim:
		case <-ctx.Done():
			// Shutting down: hand the claimed job back so it is runnable on the
			// next start instead of sitting on a fresh lease for its whole TTL.
			// The settling context is required: the worker context is already
			// cancelled, and every job query would fail on it.
			s.returnClaim(ctx, claim)
			return
		}
	}
}

// dependenciesSatisfied reports whether every requirement is met. The second
// return value explains an unsatisfiable requirement.
func (s *Service) dependenciesSatisfied(ctx context.Context, record job.Job) (bool, string) {
	dependencies, err := s.repository.ListDependencies(ctx, record.ID)
	if err != nil || len(dependencies) == 0 {
		// A dependency read failure is treated as "no blocking requirement";
		// the job's own execution will fail loudly if it truly cannot proceed.
		return true, ""
	}
	for _, dependency := range dependencies {
		parent, err := s.repository.Get(ctx, dependency.DependsOnJobID)
		if err != nil {
			var jobErr *job.Error
			if errors.As(err, &jobErr) && jobErr.Category == job.CategoryConflict {
				// The parent is gone: the requirement can never be met.
				return false, "dependency-missing"
			}
			continue
		}
		if dependency.Condition.Satisfied(parent.Status) {
			continue
		}
		if parent.Status.IsTerminal() {
			return false, "dependency-failed"
		}
		// Parent still running: this job is not ready yet, but it is not
		// failed either. Skip it this pass by pretending it is ready and
		// letting the worker re-queue it.
		return false, "dependency-pending"
	}
	return true, ""
}

// failDependency marks a job whose requirement can never be met. A pending
// requirement is not a failure, so it is left queued.
func (s *Service) failDependency(ctx context.Context, record job.Job, reason string) error {
	if reason == "dependency-pending" {
		return nil
	}
	now := s.now()
	record.Status = job.StatusFailed
	record.ErrorCode = string(job.CategoryDependency)
	record.ErrorMessage = "A required earlier job did not finish successfully."
	record.FinishedAt = now
	record.UpdatedAt = now
	if err := s.repository.Update(ctx, record, record.Revision); err != nil {
		return err
	}
	s.publish(ctx, record)
	return nil
}

// returnClaim releases a claim that no worker will execute. It re-reads the
// row first so a concurrent settle is not clobbered, and it refuses to touch a
// job that already reached a terminal state.
func (s *Service) returnClaim(parent context.Context, record job.Job) {
	ctx, cancel := settleContext(parent)
	defer cancel()
	current, err := s.repository.Get(ctx, record.ID)
	if err != nil || current.Status.IsTerminal() {
		return
	}
	current.Status = job.StatusQueued
	current.LeaseOwner = ""
	current.LeaseExpiresAt = time.Time{}
	current.UpdatedAt = s.now()
	current.NextRetryAt = time.Time{}
	_ = s.repository.Update(ctx, current, current.Revision)
}

// workerLoop executes claimed jobs until the context is cancelled.
func (s *Service) workerLoop(ctx context.Context, work <-chan job.Job) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case record, ok := <-work:
			if !ok {
				return
			}
			s.execute(ctx, record)
		}
	}
}

// execute runs one attempt: mark running, call the runner outside any
// transaction, then persist the outcome under a revision guard.
func (s *Service) execute(ctx context.Context, record job.Job) {
	// A job the user cancelled may still be handed over by the scheduler so it
	// can be settled. There is nothing to run: close it out immediately.
	if record.CancelRequested {
		s.finish(ctx, record, job.StatusCancelled, job.Attempt{AttemptNumber: record.AttemptCount}, "")
		return
	}
	started := s.now()
	attempt := job.Attempt{
		ID:            s.newID("attempt"),
		JobID:         record.ID,
		AttemptNumber: s.nextAttemptNumber(ctx, record),
		Status:        job.AttemptRunning,
		StartedAt:     started,
	}
	// A pending poll of an already-accepted remote job is bookkeeping, not an
	// attempt: the provider work was committed on the submitting pass. It is
	// identified before the attempt row is written because the row's number must
	// stay unique while the counter does not move — writing a row per poll would
	// both exhaust attempt_number and fill the history with hundreds of entries
	// for one long video. If the poll fails, or if it completes and fetches the
	// result, the row is written then, so no work goes unrecorded.
	poll := pollPass(record)
	recording := !poll
	if recording {
		if err := s.repository.StartAttempt(ctx, attempt); err != nil {
			// Without an attempt row we cannot claim the job safely; release it.
			s.release(ctx, record, job.CategoryStorage, "The job attempt could not be recorded.")
			return
		}
		s.publish(ctx, record)
	}
	outcome, runErr := s.runner.Run(ctx, record)
	finished := s.now()
	attempt.FinishedAt = finished

	// The attempt ran outside any lock, so the row may have changed (a cancel,
	// a lease sweep). Re-read it before deciding the outcome: the worker's
	// revision guard would otherwise reject its own write and lose the result.
	//
	// The read runs on a settling context because the attempt may have ended
	// precisely because the process is shutting down. On the cancelled worker
	// context the read would fail, the revision would stay stale, and a cancel
	// that landed meanwhile would make the terminal write conflict — leaving the
	// job claimed as running until its lease expired.
	refreshCtx, cancelRefresh := settleContext(ctx)
	if refreshed, err := s.repository.Get(refreshCtx, record.ID); err == nil {
		record.Revision = refreshed.Revision
		if refreshed.CancelRequested {
			record.CancelRequested = true
		}
		// Preserve the orphan note: it describes a remote side effect the worker
		// knows nothing about, and its own outcome would otherwise erase it.
		record.CancelledRemoteUnconfirmed = refreshed.CancelledRemoteUnconfirmed
		if refreshed.CancelledRemoteUnconfirmed {
			record.ResultJSON = refreshed.ResultJSON
		}
	}
	cancelRefresh()

	if runErr != nil {
		if !recording {
			// A failed poll is real work (its retry budget must advance, or a
			// repeatedly failing fetch would loop forever), so its history row is
			// written now with its final state.
			//
			// If this write fails, the pass is still settled with the advanced
			// number so the budget cannot stall. attempt_count may then briefly
			// exceed the number of stored rows; the next attempt numbers itself
			// from the rows (nextAttemptNumber), so the two converge as soon as
			// one more attempt is recorded.
			if err := s.repository.StartAttempt(ctx, attempt); err == nil {
				recording = true
			}
		}
		attempt.Status = job.AttemptFailed
		category := job.Classify(runErr)
		if category == job.CategoryCancelled {
			attempt.Status = job.AttemptCancelled
		}
		if jobErr, ok := job.AsJobError(runErr); ok {
			attempt.ErrorCode = string(jobErr.Category)
			attempt.ErrorMessage = jobErr.SafeMessage
		} else {
			attempt.ErrorCode = string(category)
		}
		if recording {
			// Settled on a settling context for the same reason as the success
			// path: the attempt may have ended because the process is shutting
			// down, and a row left "running" would misreport the run.
			attemptCtx, cancelAttempt := settleContext(ctx)
			if err := s.repository.FinishAttempt(attemptCtx, attempt); err != nil {
				s.recordAttemptHistoryFailure(attemptCtx, attempt, err)
			}
			cancelAttempt()
		}

		if category == job.CategoryCancelled || record.CancelRequested {
			// The runner was interrupted (usually by shutdown). The terminal
			// write must still land, or the job stays claimed and the attempt
			// stays "running" forever.
			settle, cancel := settleContext(ctx)
			defer cancel()
			if recording {
				_ = s.repository.FinishAttempt(settle, attempt)
			}
			s.finish(settle, record, job.StatusCancelled, attempt, "")
			return
		}
		if s.policy.ShouldRetry(attempt.AttemptNumber, category) {
			s.scheduleRetry(ctx, record, attempt, category)
			return
		}
		s.finish(ctx, record, job.StatusFailed, attempt, "")
		return
	}

	if outcome.PollOnly {
		if record.CancelRequested {
			// The user cancelled while this poll was in flight. The job must not
			// be parked again: a cancelled job is settled now, and because a poll
			// is not an attempt the counter stays where it is. The zero result
			// keeps finish()'s orphan-note guard in charge of result_json.
			s.finish(ctx, record, job.StatusCancelled, job.Attempt{AttemptNumber: record.AttemptCount}, "")
			return
		}
		// Still pending: no attempt row, no attempt count, just the paced park.
		s.finishOutcome(ctx, record, outcome.Status, attempt, outcome)
		return
	}

	attempt.Status = job.AttemptSucceeded
	if !recording {
		// The poll completed and produced the artifact, so this pass is a real
		// attempt and is recorded as one.
		if err := s.repository.StartAttempt(ctx, attempt); err == nil {
			recording = true
		}
	}
	if recording {
		// The history row is closed out on a settling context: a worker can
		// finish an attempt exactly as shutdown cancels it, and a row left
		// "running" would misreport the run forever (the job's own state is
		// written by finish/finishOutcome, which settle for the same reason).
		attemptCtx, cancelAttempt := settleContext(ctx)
		if err := s.repository.FinishAttempt(attemptCtx, attempt); err != nil {
			// The job outcome matters more than the history row, but a permanently
			// "running" attempt would misreport the run, so it is recorded.
			s.recordAttemptHistoryFailure(attemptCtx, attempt, err)
		}
		cancelAttempt()
	}
	if record.CancelRequested {
		// The user cancelled while this attempt was in flight. The attempt's
		// side effects exist, so the remote artifact is recorded rather than
		// silently dropped.
		s.finish(ctx, record, job.StatusCancelled, attempt, outcome.ResultJSON)
		return
	}
	status := outcome.Status
	if status == "" {
		status = job.StatusSucceeded
	}
	if outcome.RemoteOnly {
		status = job.StatusRemoteOnly
	}
	s.finishOutcome(ctx, record, status, attempt, outcome)
}

// pollPass reports whether this pass only asks an already-accepted remote job
// for progress.
//
// Only the async video contract works that way: it polls the remote handle it
// stored on an earlier pass. The image path instead resumes from a persisted
// pending-download marker (a fetch, not a poll) and the audio contract is
// synchronous, so neither is affected.
func pollPass(record job.Job) bool {
	return record.JobType == job.JobTypeVideoGeneration && record.RemoteJobID != ""
}

// nextAttemptNumber returns the number the new attempt row must carry.
//
// It is derived from what is actually stored, not from the job row alone,
// because the two can disagree: a crash between writing an attempt row and
// settling the job leaves the row behind while attempt_count still reads its
// old value. Starting at attempt_count+1 would then collide with the orphaned
// row and fail the UNIQUE (generation_job_id, attempt_number) constraint on
// every later pass, wedging the job in a claim/record-failure/release loop.
//
// The startup scanner reconciles this too, but it deliberately skips jobs whose
// lease still looks live, so a restart inside the lease window would go
// unreconciled. Reconciling here closes that window and makes the numbering
// self-correcting wherever an attempt actually starts.
func (s *Service) nextAttemptNumber(ctx context.Context, record job.Job) int {
	next := record.AttemptCount + 1
	// The read is a safety net, not the normal path: a storage error leaves the
	// derived number in place rather than blocking the attempt.
	if attempts, err := s.repository.ListAttempts(ctx, record.ID); err == nil {
		for _, attempt := range attempts {
			if attempt.AttemptNumber >= next {
				next = attempt.AttemptNumber + 1
			}
		}
	}
	return next
}

func (s *Service) scheduleRetry(ctx context.Context, record job.Job, attempt job.Attempt, category job.ErrorCategory) {
	now := s.now()
	delay := s.policy.NextDelay(attempt.AttemptNumber + 1)
	record.Status = job.StatusRetryWait
	record.ErrorCode = string(category)
	record.ErrorMessage = attempt.ErrorMessage
	record.AttemptCount = attempt.AttemptNumber
	record.NextRetryAt = now.Add(delay)
	record.LeaseOwner = ""
	record.LeaseExpiresAt = time.Time{}
	record.UpdatedAt = now
	if err := s.repository.Update(ctx, record, record.Revision); err != nil {
		s.release(ctx, record, category, attempt.ErrorMessage)
		return
	}
	s.publish(ctx, record)
	s.nudge()
}

// recordAttemptHistoryFailure notes that an attempt's history row could not
// be updated. It reports through the publisher when one is configured (the
// job's own error fields are owned by the outcome) and never blocks the worker.
func (s *Service) recordAttemptHistoryFailure(ctx context.Context, attempt job.Attempt, cause error) {
	if s.publisher == nil {
		// The publisher is optional; without it there is no channel to report
		// through, and panicking a worker goroutine would be far worse than a
		// missing warning.
		return
	}
	s.publisher.PublishJobChanged(ctx, JobEvent{
		JobID:     attempt.JobID,
		Status:    string(job.StatusRunning),
		Progress:  job.ProgressFromStatus(job.StatusRunning),
		ErrorCode: string(job.CategoryStorage),
		UpdatedAt: s.now().UTC().Format(time.RFC3339),
	})
}

// release returns a job to the queue when the outcome could not be persisted.
//
// It re-reads the row and retries once, because a concurrent write (a user
// cancel, a lease sweep) is exactly the situation that triggers this path. A
// job must never be left in a non-terminal state merely because two writers
// raced.
func (s *Service) release(parent context.Context, record job.Job, category job.ErrorCategory, message string) {
	// The re-read and the write must both land: this path is reached exactly when
	// a concurrent writer or a shutdown disturbed the worker, so the caller's
	// context may already be cancelled.
	ctx, cancel := settleContext(parent)
	defer cancel()
	current, err := s.repository.Get(ctx, record.ID)
	if err != nil {
		s.nudge()
		return
	}
	if current.Status.IsTerminal() {
		// Someone else settled the job; do not resurrect it.
		s.nudge()
		return
	}
	now := s.now()
	if current.CancelRequested {
		// The user cancelled, and this worker still owns the row (a cancel does
		// not settle a running job; it only flags it). Returning here would leave
		// the job claimed as running: the candidate query offers it, but the claim
		// guard refuses while the lease is live, so nothing would settle it until
		// the lease expired or the app restarted.
		//
		// The terminal write is made here rather than through finish(): finish
		// calls release on a failed write, and calling back into it would recurse
		// without bound if the store stayed unavailable. A single best-effort
		// write with no fallback cannot loop.
		current.Status = job.StatusCancelled
		current.LeaseOwner = ""
		current.LeaseExpiresAt = time.Time{}
		current.UpdatedAt = now
		current.FinishedAt = now
		if err := s.repository.Update(ctx, current, current.Revision); err != nil {
			s.nudge()
			return
		}
		s.publish(ctx, current)
		return
	}
	current.Status = job.StatusRetryWait
	current.ErrorCode = string(category)
	current.ErrorMessage = message
	// Never let the attempt counter move backwards: the database enforces one
	// attempt row per number, so a regression would make the next StartAttempt
	// fail forever and the job would cycle without ever running.
	if record.AttemptCount > current.AttemptCount {
		current.AttemptCount = record.AttemptCount
	}
	next := current.AttemptCount + 1
	// A zero delay means "retry immediately", which is only safe when the next
	// attempt can actually record itself. If StartAttempt just failed because a
	// row's number already exists, an immediate retry would spin at scheduler
	// speed; the attempt-number derivation removes the collision, but the floor
	// keeps a storage hiccup from becoming a hot loop.
	delay := s.policy.NextDelay(next)
	if delay < minReleaseDelay {
		delay = minReleaseDelay
	}
	current.NextRetryAt = now.Add(delay)
	current.LeaseOwner = ""
	current.LeaseExpiresAt = time.Time{}
	current.UpdatedAt = now
	if err := s.repository.Update(ctx, current, current.Revision); err != nil {
		// A second conflict means another writer is settling the job; leave it
		// to them rather than looping.
		s.nudge()
		return
	}
	s.publish(ctx, current)
	s.nudge()
}

// minReleaseDelay floors the retry delay a release schedules. A release means
// the outcome could not be persisted, so an immediate re-claim would simply
// repeat the failed write at scheduler speed.
const minReleaseDelay = time.Second

// Settlement writes
//
// Every method below persists job state that the scheduler needs in order to
// make progress: a terminal state, a retry window, a paced park. They all run
// on a settling context rather than the caller's, because the caller may be a
// worker whose context was cancelled precisely when the state must be written —
// a shutdown, or a cancel that arrived while the attempt was in flight. On the
// cancelled context the read and the guarded write would simply fail, leaving
// the job claimed on a live lease with no worker to settle it, which is a state
// only a restart could clear (ARCHITECTURE §6.2 requires scheduler state to be
// persisted before exit). The timeout keeps the uncancellable window bounded.
func (s *Service) finish(parent context.Context, record job.Job, status job.Status, attempt job.Attempt, resultJSON string) {
	ctx, cancel := settleContext(parent)
	defer cancel()
	if !job.CanTransition(record.Status, status) {
		status = job.StatusFailed
	}
	now := s.now()
	record.Status = status
	record.AttemptCount = attempt.AttemptNumber
	// A cancelled job keeps the orphan note recorded when the user cancelled:
	// the note explains why a provider-side job may still be running, and the
	// runner's (usually empty) result would otherwise erase it. A poll carries an
	// empty result by construction, so the same rule applies to it explicitly:
	// erasing the note there would lose the only durable explanation the user has.
	if !(status == job.StatusCancelled && record.CancelledRemoteUnconfirmed) {
		record.ResultJSON = resultJSON
	}
	record.LeaseOwner = ""
	record.LeaseExpiresAt = time.Time{}
	record.UpdatedAt = now
	record.FinishedAt = now
	if status == job.StatusFailed {
		record.ErrorCode = attempt.ErrorCode
		record.ErrorMessage = attempt.ErrorMessage
	}
	if err := s.repository.Update(ctx, record, record.Revision); err != nil {
		s.release(ctx, record, job.CategoryStorage, "The job outcome could not be recorded.")
		return
	}
	s.publish(ctx, record)
}

func (s *Service) finishOutcome(parent context.Context, record job.Job, status job.Status, attempt job.Attempt, outcome Outcome) {
	ctx, cancel := settleContext(parent)
	defer cancel()
	if !job.CanTransition(record.Status, status) {
		status = job.StatusFailed
	}
	now := s.now()
	record.Status = status
	// A pure poll is not an attempt: it neither consumed a provider generation
	// nor produced a result. Counting it would exhaust a short retry budget on a
	// long remote job (a video can legitimately need hundreds of polls) and would
	// make the Job Center report attempt numbers that never existed.
	if !outcome.PollOnly {
		record.AttemptCount = attempt.AttemptNumber
	}
	record.ResultJSON = outcome.ResultJSON
	record.RemoteJobID = outcome.RemoteJobID
	record.Progress = clampProgress(outcome.Progress)
	record.LeaseOwner = ""
	record.LeaseExpiresAt = time.Time{}
	record.UpdatedAt = now
	if status.IsTerminal() {
		record.FinishedAt = now
	}
	// A parked remote job must not be re-polled at scheduler speed. next_retry_at
	// already means "earliest time this job may run again" for retry_wait, and the
	// candidate query and claim guard both honour it for waiting_remote too, so a
	// poll interval needs no new column.
	if outcome.PollOnly && !status.IsTerminal() {
		record.NextRetryAt = now.Add(s.remotePollInterval)
	}
	if err := s.repository.Update(ctx, record, record.Revision); err != nil {
		s.release(ctx, record, job.CategoryStorage, "The job outcome could not be recorded.")
		return
	}
	s.publish(ctx, record)
	// A non-terminal outcome (for example waiting_remote) means the job will
	// need another pass, so keep the scheduler moving.
	if !status.IsTerminal() {
		s.nudge()
	}
}

// clampProgress bounds a provider-reported percentage to the range the column
// and the UI accept.
//
// The value comes from a remote service and is written into a CHECK-constrained
// column; a provider reporting 120 (or -1) would make the whole row update fail
// even after the artifact was fetched, which turns a reporting quirk into a
// retry loop that never settles. Out-of-range values are pinned to the nearest
// valid bound rather than dropped, so progress still moves.
func clampProgress(progress *int) *int {
	if progress == nil {
		return nil
	}
	value := *progress
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return &value
}
