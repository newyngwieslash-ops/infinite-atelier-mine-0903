package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// Defaults for the scheduler and lease behavior.
const (
	// DefaultLeaseTTL bounds how long one worker owns a job before another may
	// reclaim it.
	//
	// It must exceed the longest single network operation, which is a provider
	// result download (providerhttp.DefaultDownloadLimits().TotalTimeout, 15
	// minutes). A shorter lease would let a second worker claim a job whose
	// first worker is still fetching, and both would then write the same job's
	// outcome. TestLeaseExceedsDownloadTimeout pins this relationship.
	DefaultLeaseTTL = 30 * time.Minute
	// DefaultPollInterval is how often the scheduler looks for work.
	DefaultPollInterval = 250 * time.Millisecond
	// DefaultWorkerCount is the number of concurrent job workers.
	DefaultWorkerCount = 3
	// DefaultRemotePollInterval is the wait between two polls of one remote job.
	//
	// Without it the scheduler would re-claim a parked waiting_remote job as soon
	// as a worker freed up, i.e. as fast as the provider's HTTP round-trip
	// allows: a real video job would be polled hundreds of times per minute. The
	// wait is applied through next_retry_at, which already means "earliest time
	// this job may run again" for retry_wait.
	DefaultRemotePollInterval = 5 * time.Second
)

// Service owns the job lifecycle: submission, cancellation, retry, and the
// scheduler loop that hands work to workers.
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
	publisher  Publisher
	policy     job.RetryPolicy

	// runner executes one job. It is provided by the composition root so the
	// application layer does not depend on provider adapters.
	runner JobRunner
	// remoteCanceller stops provider-side work when the user cancels. Optional:
	// without it a cancellation is local-only and records an orphan candidate.
	remoteCanceller RemoteCanceller
	// remotePollInterval paces polls of a waiting_remote job.
	remotePollInterval time.Duration

	mu       sync.Mutex
	paused   bool
	started  bool
	stopping bool
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	// wake nudges the scheduler when a job is submitted or retried.
	wake chan struct{}
}

// JobRunner performs the actual work of one job. Implementations live in
// infrastructure and use the provider ports.
type JobRunner interface {
	// Run executes the job's current stage and reports the resulting status
	// update. Returning an error means the job failed this attempt.
	Run(ctx context.Context, record job.Job) (Outcome, error)
}

// Outcome is a runner's instruction for the job's next state.
type Outcome struct {
	// Status is the status to persist. Zero means "leave as is".
	Status job.Status
	// RemoteJobID records a remote handle for a waiting_remote job.
	RemoteJobID string
	// ResultJSON is the serialized provider result metadata.
	ResultJSON string
	// RemoteOnly marks a result that is usable but not stored locally.
	RemoteOnly bool
	// Progress is the provider-reported completion, when known.
	Progress *int
	// PollOnly marks a pass that only asked the provider for progress without
	// producing or failing work (a not-yet-finished remote job). Such a pass is
	// deliberately not counted as an attempt: a video job that legitimately needs
	// hundreds of polls must not exhaust a three-attempt retry budget, and each
	// poll would otherwise add an attempt row and skew the Job Center's
	// attempts column.
	PollOnly bool
}

// Options configures a Service.
type Options struct {
	Repository Repository
	Clock      Clock
	IDs        IDGenerator
	Publisher  Publisher
	Runner     JobRunner
	Policy     job.RetryPolicy
	// RemotePollInterval paces polls of a waiting_remote job. Zero uses
	// DefaultRemotePollInterval.
	RemotePollInterval time.Duration
}

// NewService builds the job service. It does not start any goroutine; call
// Start for that.
func NewService(options Options) *Service {
	policy := options.Policy.Normalize()
	interval := options.RemotePollInterval
	if interval <= 0 {
		interval = DefaultRemotePollInterval
	}
	return &Service{
		repository:         options.Repository,
		clock:              options.Clock,
		ids:                options.IDs,
		publisher:          options.Publisher,
		policy:             policy,
		runner:             options.Runner,
		remotePollInterval: interval,
		wake:               make(chan struct{}, 1),
	}
}

// SubmitRequest is a caller's request to create a job.
type SubmitRequest struct {
	ProjectID        string
	EntityType       string
	EntityID         string
	JobType          job.JobType
	Priority         int
	ProviderConfigID string
	ModelConfigID    string
	InputJSON        string
	// Scope names the command that produced the request; it participates in
	// the idempotency key so different commands never collide.
	Scope string
}

// Submit creates a job, or returns the existing job when an identical request
// was already submitted. Idempotent by construction (AC-FOUND-006).
func (s *Service) Submit(ctx context.Context, request SubmitRequest) (job.Job, bool, error) {
	if s == nil || s.repository == nil {
		return job.Job{}, false, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	if !job.IsValidJobType(request.JobType) {
		return job.Job{}, false, job.FailedJobError(job.CategoryInvalidInput, "The requested job type is not supported.")
	}
	now := s.now()
	// The key must identify the work, not just the text: two nodes with the
	// same prompt are two different jobs, and a project's jobs must not
	// collide with another project's.
	key := job.IdempotencyKey(request.Scope+"|"+request.ProjectID+"|"+request.EntityType+"|"+request.EntityID, []byte(request.InputJSON))
	record := job.Job{
		ID:               s.newID("job"),
		ProjectID:        request.ProjectID,
		EntityType:       request.EntityType,
		EntityID:         request.EntityID,
		JobType:          request.JobType,
		Status:           job.StatusQueued,
		Priority:         request.Priority,
		IdempotencyKey:   key,
		ProviderConfigID: request.ProviderConfigID,
		ModelConfigID:    request.ModelConfigID,
		InputJSON:        request.InputJSON,
		MaxAttempts:      s.policy.MaxAttempts,
		CreatedAt:        now,
		UpdatedAt:        now,
		Revision:         1,
	}
	if err := s.repository.Insert(ctx, record); err != nil {
		var jobErr *job.Error
		if errors.As(err, &jobErr) && jobErr.Category == job.CategoryConflict {
			// Idempotent replay: return the job that already exists.
			existing, getErr := s.repository.GetByIdempotencyKey(ctx, request.ProjectID, key)
			if getErr != nil {
				return job.Job{}, false, getErr
			}
			return existing, true, nil
		}
		return job.Job{}, false, err
	}
	s.publish(ctx, record)
	s.nudge()
	return record, false, nil
}

// Cancel requests cancellation. A queued job stops immediately; a running or
// waiting job is flagged so the worker stops at its next safe point. The remote
// side is not guaranteed to stop, which is recorded on the job rather than
// assumed.
func (s *Service) Cancel(ctx context.Context, id string) (job.Job, error) {
	record, _, err := s.cancelJob(ctx, id)
	return record, err
}

// cancelJob performs the work and reports whether the job actually changed state.
// A job that was already terminal is a no-op, not an error, and must not be
// counted as newly cancelled.
func (s *Service) cancelJob(ctx context.Context, id string) (job.Job, bool, error) {
	if s == nil || s.repository == nil {
		return job.Job{}, false, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	now := s.now()
	record, err := s.repository.RequestCancel(ctx, id, now)
	if err != nil {
		return job.Job{}, false, err
	}
	if record.Status.IsTerminal() {
		// Already finished; cancellation is a no-op rather than an error.
		return record, false, nil
	}
	if record.Status == job.StatusCancelled {
		// Already cancelled by an earlier call.
		return record, false, nil
	}
	switch record.Status {
	case job.StatusQueued, job.StatusRetryWait, job.StatusRecovering:
		// Nothing is executing yet: transition immediately.
		record.Status = job.StatusCancelled
		record.FinishedAt = now
		record.UpdatedAt = now
		if err := s.repository.Update(ctx, record, record.Revision); err != nil {
			return job.Job{}, false, err
		}
		s.publish(ctx, record)
		s.nudge()
		return record, true, nil
	default:
		// running/waiting_remote/downloading/verifying: a worker owns the
		// attempt. The flag is persisted, the scheduler offers the job to a
		// worker for settlement, and the worker's own terminal write is what
		// moves it to cancelled.
		//
		// A remote side effect may already exist. Attempting to stop it is best
		// effort: when it cannot be confirmed, the remote handle is recorded so
		// the user can see the orphan candidate rather than assuming the
		// provider stopped billing.
		if record.RemoteJobID != "" {
			s.recordOrphanCandidate(ctx, record)
			// Return the refreshed row so the caller sees the orphan flag the
			// service just recorded rather than the pre-recording snapshot.
			if refreshed, getErr := s.repository.Get(ctx, record.ID); getErr == nil {
				record = refreshed
			}
		}
		// Nothing has changed yet from the user's point of view: the job is
		// flagged, not settled. Report it as such rather than claiming a
		// transition that has not happened.
		s.nudge()
		return record, false, nil
	}
}

// RemoteCanceller stops a provider-side job. It is optional: an adapter that
// cannot cancel a remote job reports unsupported, and the job is then recorded
// as an orphan candidate instead.
type RemoteCanceller interface {
	CancelRemote(ctx context.Context, record job.Job) error
}

// WithRemoteCanceller supplies the component that stops provider-side work.
// Without it, cancelling an in-flight job still stops local work but always
// records an orphan candidate.
func (s *Service) WithRemoteCanceller(canceller RemoteCanceller) *Service {
	if s == nil {
		return s
	}
	s.remoteCanceller = canceller
	return s
}

// CancelMany cancels several jobs and reports how many actually changed state.
//
// A job that was already terminal, or one whose worker still has to settle it,
// is not counted: the number is the count of real transitions, which is what
// the UI tells the user.
func (s *Service) CancelMany(ctx context.Context, ids []string) (int, error) {
	cancelled := 0
	for _, id := range ids {
		_, changed, err := s.cancelJob(ctx, id)
		if err != nil {
			var jobErr *job.Error
			if errors.As(err, &jobErr) && jobErr.Category == job.CategoryConflict {
				continue // already gone
			}
			return cancelled, err
		}
		if changed {
			cancelled++
		}
	}
	return cancelled, nil
}

// RetryFailed re-queues failed and orphaned jobs, leaving succeeded and
// cancelled ones alone. This is the "retry failed only" batch action.
func (s *Service) RetryFailed(ctx context.Context, ids []string) (int, error) {
	if s == nil || s.repository == nil {
		return 0, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	now := s.now()
	retried := 0
	for _, id := range ids {
		record, err := s.repository.Get(ctx, id)
		if err != nil {
			var jobErr *job.Error
			if errors.As(err, &jobErr) && jobErr.Category == job.CategoryConflict {
				continue
			}
			return retried, err
		}
		if record.Status != job.StatusFailed && record.Status != job.StatusOrphaned {
			continue
		}
		if !job.CanTransition(record.Status, job.StatusQueued) {
			continue
		}
		record.Status = job.StatusQueued
		record.ErrorCode = ""
		record.ErrorMessage = ""
		record.NextRetryAt = time.Time{}
		record.CancelRequested = false
		record.FinishedAt = time.Time{}
		record.LeaseOwner = ""
		record.LeaseExpiresAt = time.Time{}
		record.UpdatedAt = now
		if err := s.repository.Update(ctx, record, record.Revision); err != nil {
			return retried, err
		}
		s.publish(ctx, record)
		retried++
	}
	if retried > 0 {
		s.nudge()
	}
	return retried, nil
}

// Get returns one job.
func (s *Service) Get(ctx context.Context, id string) (job.Job, error) {
	if s == nil || s.repository == nil {
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	return s.repository.Get(ctx, id)
}

// Attempts returns a job's attempt history.
func (s *Service) Attempts(ctx context.Context, jobID string) ([]job.Attempt, error) {
	if s == nil || s.repository == nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	return s.repository.ListAttempts(ctx, jobID)
}

// List returns jobs matching the filter.
func (s *Service) List(ctx context.Context, filter ListFilter) ([]job.Job, error) {
	if s == nil || s.repository == nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	return s.repository.List(ctx, filter)
}

// QueueSummary is the Job Center's header data.
type QueueSummary struct {
	Queued    int  `json:"queued"`
	Running   int  `json:"running"`
	Waiting   int  `json:"waiting"`
	Failed    int  `json:"failed"`
	Succeeded int  `json:"succeeded"`
	Cancelled int  `json:"cancelled"`
	Paused    bool `json:"paused"`
	// ActiveTotal is the number of non-terminal jobs.
	ActiveTotal int `json:"activeTotal"`
}

// Summary reports queue counts plus the pause state.
func (s *Service) Summary(ctx context.Context) (QueueSummary, error) {
	if s == nil || s.repository == nil {
		return QueueSummary{}, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	counts, err := s.repository.ActiveCounts(ctx)
	if err != nil {
		return QueueSummary{}, err
	}
	summary := QueueSummary{
		Queued:    counts[job.StatusQueued],
		Running:   counts[job.StatusRunning],
		Waiting:   counts[job.StatusWaitingRemote],
		Failed:    counts[job.StatusFailed],
		Succeeded: counts[job.StatusSucceeded],
		Cancelled: counts[job.StatusCancelled],
		Paused:    s.Paused(),
	}
	summary.ActiveTotal = summary.Queued + summary.Running + summary.Waiting +
		counts[job.StatusDownloading] + counts[job.StatusVerifying] +
		counts[job.StatusRetryWait] + counts[job.StatusRecovering]
	return summary, nil
}

// Pause stops handing out new work. Running jobs finish their current attempt.
func (s *Service) Pause() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
}

// Resume allows work to be handed out again.
func (s *Service) Resume() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
	s.nudge()
}

// Paused reports the pause state.
func (s *Service) Paused() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// recordOrphanCandidate marks that a remote job may still be running. The
// attempt to stop it is best effort, and the outcome is recorded rather than
// assumed.
func (s *Service) recordOrphanCandidate(ctx context.Context, record job.Job) {
	detail := "Remote work may still be running at the provider."
	if s.remoteCanceller != nil {
		if err := s.remoteCanceller.CancelRemote(ctx, record); err != nil {
			detail = "The provider-side job could not be cancelled and may still be running."
		} else {
			detail = "Provider-side cancellation was requested; completion is not guaranteed."
		}
	}
	current, err := s.repository.Get(ctx, record.ID)
	if err != nil {
		return
	}
	// The orphan note lives in result_json, not error_message: the worker
	// writes error_code/message from the attempt outcome and would overwrite
	// the note, while result_json is the field the runner owns.
	encoded, encodeErr := json.Marshal(map[string]string{
		"mode":      "orphan_candidate",
		"remoteJob": current.RemoteJobID,
		"note":      detail,
	})
	if encodeErr != nil {
		return
	}
	current.ResultJSON = string(encoded)
	current.CancelledRemoteUnconfirmed = true
	current.UpdatedAt = s.now()
	// A revision conflict means a worker just wrote the row; its own write
	// carries the terminal state, so losing this update is harmless.
	_ = s.repository.Update(ctx, current, current.Revision)
}

func (s *Service) now() time.Time {
	if s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

func (s *Service) newID(prefix string) string {
	if s.ids == nil {
		return prefix + "-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return s.ids.NewID(prefix)
}

func (s *Service) publish(ctx context.Context, record job.Job) {
	if s.publisher == nil {
		return
	}
	progress := job.ProgressFromStatus(record.Status)
	if record.Progress != nil {
		progress = *record.Progress
	}
	s.publisher.PublishJobChanged(ctx, JobEvent{
		JobID:     record.ID,
		Status:    string(record.Status),
		Progress:  progress,
		ErrorCode: string(record.ErrorCode),
		UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func (s *Service) nudge() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
