package job

import "time"

// Attempt is one execution attempt of a job. The parent job keeps the
// aggregate; attempts are append-only history for diagnostics and for
// per-attempt error reporting.
type Attempt struct {
	ID                string
	JobID             string
	AttemptNumber     int
	Status            AttemptStatus
	ProviderRequestID string
	ErrorCode         string
	ErrorMessage      string
	StartedAt         time.Time
	FinishedAt        time.Time
}

// Dependency records that a job may only run after another job reached a
// state satisfying Condition.
type Dependency struct {
	JobID          string
	DependsOnJobID string
	Condition      DependencyCondition
}

// ProgressFromStatus derives a coarse progress value when a job has no
// provider-reported number. It exists so the UI can render a meaningful bar
// without inventing precision the backend does not have.
func ProgressFromStatus(status Status) int {
	switch status {
	case StatusQueued:
		return 0
	case StatusRecovering:
		return 0
	case StatusRunning:
		return 25
	case StatusWaitingRemote:
		return 50
	case StatusDownloading:
		return 80
	case StatusVerifying:
		return 95
	case StatusRetryWait:
		return 0
	default:
		return 0
	}
}
