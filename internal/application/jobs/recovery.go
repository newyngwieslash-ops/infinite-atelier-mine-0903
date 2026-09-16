package jobs

import (
	"context"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// RecoveryReport summarizes what the startup scan did, for logging and tests.
type RecoveryReport struct {
	// Reququed jobs had no remote side effect and can simply run again.
	Requeued int
	// Resumed jobs had a remote job ID; they continue by polling.
	Resumed int
	// Orphaned jobs could not be recovered safely.
	Orphaned int
	// Redownload marks jobs whose result must be fetched again.
	Redownload int
	// Reverify marks jobs whose downloaded bytes must be validated again.
	Reverify int
	// Cancelled counts jobs the user had already cancelled; recovery closed
	// them out rather than resuming them.
	Cancelled int
}

// Total reports how many jobs the scan touched.
func (r RecoveryReport) Total() int {
	return r.Requeued + r.Resumed + r.Orphaned + r.Redownload + r.Reverify + r.Cancelled
}

// Recover runs the startup scan described by ARCHITECTURE §14.2:
//
//   - running without a live lease: the process died mid-attempt. Re-queue it,
//     unless the job had already obtained a remote job ID, in which case the
//     remote side may be billing and must be polled instead of re-submitted.
//   - waiting_remote: resume polling by the stored remote ID. Never re-submit.
//   - downloading: the temporary download may be incomplete; re-download.
//   - verifying: bytes exist locally; re-run validation before committing.
//   - anything unclassifiable: orphaned with diagnostics preserved.
//
// The scan never deletes a job and never marks a job succeeded.
func (s *Service) Recover(ctx context.Context) (RecoveryReport, error) {
	report := RecoveryReport{}
	if s == nil || s.repository == nil {
		return report, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	now := s.now()
	// A job that was mid-flight when the process stopped can be in any
	// non-terminal, non-queued state. queued and retry_wait jobs are already
	// runnable and need no recovery action.
	stuck, err := s.repository.List(ctx, ListFilter{
		Statuses: []job.Status{
			job.StatusRunning,
			job.StatusWaitingRemote,
			job.StatusDownloading,
			job.StatusVerifying,
			job.StatusRecovering,
		},
		Limit: 500,
	})
	if err != nil {
		return report, err
	}

	for _, record := range stuck {
		if record.CancelRequested {
			// The user cancelled before the restart. Resuming would undo an
			// explicit instruction, so the job is closed out instead.
			record.Status = job.StatusCancelled
			record.LeaseOwner = ""
			record.LeaseExpiresAt = time.Time{}
			record.FinishedAt = now
			record.UpdatedAt = now
			if err := s.repository.Update(ctx, record, record.Revision); err != nil {
				continue
			}
			if recovered, err := s.repository.Get(ctx, record.ID); err == nil {
				s.publish(ctx, recovered)
			}
			report.Cancelled++
			continue
		}
		if record.LeaseHeld(now) {
			// Another live worker owns it; leave it alone. After a restart
			// there are no leases of ours, but a lease can outlive a crash, so
			// this check keeps recovery safe if it is ever run concurrently.
			continue
		}
		// A crash can leave an attempt row behind while the job's counter still
		// reads zero (the process died after StartAttempt, before the settle).
		// Requeuing without reconciling would make every later StartAttempt fail
		// the unique attempt-number constraint, so the counter is raised to the
		// highest number that already exists.
		if attempts, err := s.repository.ListAttempts(ctx, record.ID); err == nil {
			for _, attempt := range attempts {
				if attempt.AttemptNumber > record.AttemptCount {
					record.AttemptCount = attempt.AttemptNumber
				}
			}
		}
		next, disposition := recoveryDisposition(record)
		// One write, not two: marking the job `recovering` and then moving it
		// again would need the revision the first write produced, and the extra
		// step buys nothing for a single-process scanner. The `recovering`
		// status remains available for a scan that is itself interrupted, and
		// is handled on the next start.
		record.Status = next
		record.LeaseOwner = ""
		record.LeaseExpiresAt = time.Time{}
		record.UpdatedAt = now
		if disposition == dispositionOrphan {
			record.ErrorCode = string(job.CategoryStorage)
			record.ErrorMessage = "The job could not be recovered after restart."
			record.FinishedAt = now
		}
		if err := s.repository.Update(ctx, record, record.Revision); err != nil {
			// Could not persist the recovery decision; try the next job rather
			// than aborting the whole scan.
			continue
		}
		recovered, err := s.repository.Get(ctx, record.ID)
		if err != nil {
			continue
		}
		s.publish(ctx, recovered)
		switch disposition {
		case dispositionRequeue:
			report.Requeued++
		case dispositionResume:
			report.Resumed++
		case dispositionRedownload:
			report.Redownload++
		case dispositionReverify:
			report.Reverify++
		case dispositionOrphan:
			report.Orphaned++
		}
	}
	if report.Total() > 0 {
		s.nudge()
	}
	return report, nil
}

type disposition int

const (
	dispositionRequeue disposition = iota
	dispositionResume
	dispositionRedownload
	dispositionReverify
	dispositionOrphan
)

// recoveryDisposition decides where a stuck job goes. The rules encode the
// "never re-submit an unknown-billing remote task" requirement: a job that
// already has a remote ID always resumes by polling.
func recoveryDisposition(record job.Job) (job.Status, disposition) {
	switch record.Status {
	case job.StatusRunning:
		if record.RemoteJobID != "" {
			// A remote job was accepted before the crash. Poll it; do not
			// submit again.
			return job.StatusWaitingRemote, dispositionResume
		}
		if record.CancelRequested {
			return job.StatusCancelled, dispositionRequeue
		}
		// No remote side effect was recorded, so running it again is safe.
		return job.StatusQueued, dispositionRequeue
	case job.StatusWaitingRemote:
		if record.RemoteJobID == "" {
			// Nothing to poll; the job cannot continue safely.
			return job.StatusOrphaned, dispositionOrphan
		}
		return job.StatusWaitingRemote, dispositionResume
	case job.StatusDownloading:
		// The worker writes this state only after the provider produced a
		// result, and the runner resumes the fetch from the persisted marker, so
		// re-queueing cannot cause a second provider call. Without a marker there
		// is nothing to resume and the job is orphaned rather than silently
		// re-submitted.
		if record.RemoteJobID == "" && record.ResultJSON == "" {
			return job.StatusOrphaned, dispositionOrphan
		}
		return job.StatusQueued, dispositionRedownload
	case job.StatusVerifying:
		if record.ResultJSON == "" {
			return job.StatusOrphaned, dispositionOrphan
		}
		return job.StatusVerifying, dispositionReverify
	case job.StatusRecovering:
		// A previous recovery run was interrupted. Re-queue only when there is
		// no remote side effect to reconcile.
		if record.RemoteJobID != "" {
			return job.StatusWaitingRemote, dispositionResume
		}
		return job.StatusQueued, dispositionRequeue
	default:
		return job.StatusOrphaned, dispositionOrphan
	}
}
