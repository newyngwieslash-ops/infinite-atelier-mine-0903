// Package job defines the persistent generation-job domain. It is pure
// business vocabulary and state-machine rules: no Wails, SQLite, HTTP client,
// provider SDK, or OS API imports are allowed here.
package job

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Status is a job's persisted lifecycle state. The set is a superset of the
// two specification vocabularies; see docs/adr/0004 for the mapping to
// PRD/ARCHITECTURE naming.
type Status string

const (
	// StatusQueued means the job is accepted but no worker has claimed it.
	StatusQueued Status = "queued"
	// StatusRunning means a worker holds the lease and is executing local work
	// (including a remote submit).
	StatusRunning Status = "running"
	// StatusWaitingRemote means a remote provider job exists and is being polled.
	StatusWaitingRemote Status = "waiting_remote"
	// StatusDownloading means a remote result URL is being fetched.
	StatusDownloading Status = "downloading"
	// StatusVerifying means bytes are present and validation is in progress.
	StatusVerifying Status = "verifying"
	// StatusRetryWait means the job failed but has retries left.
	StatusRetryWait Status = "retry_wait"
	// StatusSucceeded means a verified result was committed. Terminal.
	StatusSucceeded Status = "succeeded"
	// StatusRemoteOnly means the provider returned a remote result that was
	// deliberately not downloaded. Terminal.
	StatusRemoteOnly Status = "remote_only"
	// StatusFailed means no retries remain or the failure is permanent. Terminal.
	StatusFailed Status = "failed"
	// StatusCancelled means the user cancelled. Terminal; the remote side may
	// still be running.
	StatusCancelled Status = "cancelled"
	// StatusOrphaned means the job could not be recovered after restart. Terminal.
	StatusOrphaned Status = "orphaned"
	// StatusRecovering is the transient marker the recovery scanner sets before
	// deciding a job's disposition. A job never rests here.
	StatusRecovering Status = "recovering"
)

// AllStatuses lists every persisted status, in lifecycle order.
func AllStatuses() []Status {
	return []Status{
		StatusQueued, StatusRunning, StatusWaitingRemote, StatusDownloading,
		StatusVerifying, StatusRetryWait, StatusSucceeded, StatusRemoteOnly,
		StatusFailed, StatusCancelled, StatusOrphaned, StatusRecovering,
	}
}

// IsValidStatus reports whether the value is a persisted status.
func IsValidStatus(status Status) bool {
	for _, candidate := range AllStatuses() {
		if candidate == status {
			return true
		}
	}
	return false
}

// IsTerminal reports whether the status ends the job's life. A terminal job
// accepts no further transitions; at most a new job may be submitted.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusRemoteOnly, StatusFailed, StatusCancelled, StatusOrphaned:
		return true
	default:
		return false
	}
}

// IsActive reports whether the job currently occupies the scheduler.
func (s Status) IsActive() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusWaitingRemote, StatusDownloading,
		StatusVerifying, StatusRetryWait, StatusRecovering:
		return true
	default:
		return false
	}
}

// CanTransition reports whether a move between statuses is allowed. The table
// is deliberately explicit: silent state jumps are the failure mode this
// guards against.
func CanTransition(from, to Status) bool {
	if !IsValidStatus(from) || !IsValidStatus(to) {
		return false
	}
	if from == to {
		return true
	}
	if from.IsTerminal() {
		// Only an explicit user retry moves a terminal job back to the queue,
		// and that is modelled as a new attempt on the same row.
		return to == StatusQueued || to == StatusRetryWait
	}
	switch from {
	case StatusQueued:
		return to == StatusRunning || to == StatusCancelled || to == StatusOrphaned || to == StatusRecovering
	case StatusRunning:
		// A synchronous job can finish inline; an asynchronous one parks in
		// waiting_remote; a failure either retries or ends.
		return to == StatusSucceeded || to == StatusRemoteOnly || to == StatusWaitingRemote ||
			to == StatusDownloading || to == StatusVerifying || to == StatusRetryWait ||
			to == StatusFailed || to == StatusCancelled || to == StatusOrphaned
	case StatusWaitingRemote:
		return to == StatusDownloading || to == StatusVerifying || to == StatusSucceeded ||
			to == StatusRemoteOnly || to == StatusRetryWait || to == StatusFailed ||
			to == StatusCancelled || to == StatusOrphaned || to == StatusRunning
	case StatusDownloading:
		return to == StatusVerifying || to == StatusSucceeded || to == StatusRemoteOnly ||
			to == StatusRetryWait || to == StatusFailed || to == StatusCancelled || to == StatusOrphaned
	case StatusVerifying:
		return to == StatusSucceeded || to == StatusRemoteOnly || to == StatusRetryWait ||
			to == StatusFailed || to == StatusCancelled || to == StatusOrphaned
	case StatusRetryWait:
		return to == StatusQueued || to == StatusRunning || to == StatusFailed ||
			to == StatusCancelled || to == StatusOrphaned
	case StatusRecovering:
		return to == StatusQueued || to == StatusRunning || to == StatusWaitingRemote ||
			to == StatusDownloading || to == StatusFailed || to == StatusCancelled || to == StatusOrphaned
	default:
		return false
	}
}

// JobType identifies what a job produces. WP-03 implements image generation
// and edit; video/audio exist as contracts until WP-11.
type JobType string

const (
	JobTypeImageGeneration JobType = "image_generation"
	JobTypeImageEdit       JobType = "image_edit"
	JobTypeVideoGeneration JobType = "video_generation"
	JobTypeAudioGeneration JobType = "audio_generation"
	JobTypeAssetDownload   JobType = "asset_download"
	// THE FOUR PRD FR-150 NAMES AND THIS BUILD DID NOT HAVE, added by WP-15.
	//
	// They are in the vocabulary so the SCHEMA accepts them — migration 000023 rebuilt
	// `generation_jobs` to widen its CHECK, because SQLite cannot alter a constraint in place. Being
	// in the vocabulary is NOT the same as being runnable: `Capability` answers the empty string for
	// all four, and the runner refuses a type with no adapter with `CategoryUnsupported`. The split is
	// deliberate — the domain and the schema state which vocabulary is legal, and the runner states
	// what this build can execute — so a job of one of these types is a row the database accepts and
	// the runner refuses with an honest error, rather than a constraint violation that looks like
	// corrupt data.
	//
	// Thumbnail is DERIVED preview work; import is a document transfer that must not block the UI;
	// export is a composition the media stack already runs inside a command; migration is a legacy
	// import's data movement.
	JobTypeThumbnail JobType = "thumbnail"
	JobTypeImport    JobType = "import"
	JobTypeExport    JobType = "export"
	JobTypeMigration JobType = "migration"
)

// AllJobTypes lists every accepted job type.
func AllJobTypes() []JobType {
	return []JobType{
		JobTypeImageGeneration, JobTypeImageEdit, JobTypeVideoGeneration,
		JobTypeAudioGeneration, JobTypeAssetDownload,
		JobTypeThumbnail, JobTypeImport, JobTypeExport, JobTypeMigration,
	}
}

// IsValidJobType reports whether the value is a persisted job type.
func IsValidJobType(value JobType) bool {
	for _, candidate := range AllJobTypes() {
		if candidate == value {
			return true
		}
	}
	return false
}

// Capability maps a job type to the provider capability it needs.
func (t JobType) Capability() string {
	switch t {
	case JobTypeImageGeneration, JobTypeImageEdit:
		return "image"
	case JobTypeVideoGeneration:
		return "video"
	case JobTypeAudioGeneration:
		return "audio"
	case JobTypeThumbnail, JobTypeImport, JobTypeExport, JobTypeMigration:
		// No provider capability: these are local or engine work rather than a model call, so the
		// provider. The runner refuses them until an adapter exists, which is the honest answer while
		// they are vocabulary rather than features.
		return ""
	default:
		return ""
	}
}

// AttemptStatus is the outcome of one execution attempt.
type AttemptStatus string

const (
	AttemptRunning   AttemptStatus = "running"
	AttemptSucceeded AttemptStatus = "succeeded"
	AttemptFailed    AttemptStatus = "failed"
	AttemptCancelled AttemptStatus = "cancelled"
)

// IsValidAttemptStatus reports whether the value is a persisted attempt status.
func IsValidAttemptStatus(status AttemptStatus) bool {
	switch status {
	case AttemptRunning, AttemptSucceeded, AttemptFailed, AttemptCancelled:
		return true
	default:
		return false
	}
}

// DependencyCondition is the requirement a dependent job places on its parent.
type DependencyCondition string

const (
	ConditionSuccess   DependencyCondition = "success"
	ConditionCompleted DependencyCondition = "completed"
	ConditionApproved  DependencyCondition = "approved"
)

// IsValidCondition reports whether the value is a persisted condition.
func IsValidCondition(condition DependencyCondition) bool {
	switch condition {
	case ConditionSuccess, ConditionCompleted, ConditionApproved:
		return true
	default:
		return false
	}
}

// Satisfied reports whether the parent's terminal status satisfies this
// condition. WP-03 has no asset-approval flow yet, so `approved` is satisfied
// only by a succeeded parent; the stricter asset semantics arrive with the
// asset domain in a later package and must then tighten this rule.
func (c DependencyCondition) Satisfied(parent Status) bool {
	switch c {
	case ConditionSuccess, ConditionApproved:
		return parent == StatusSucceeded
	case ConditionCompleted:
		return parent.IsTerminal()
	default:
		return false
	}
}

// Job is the persisted job aggregate.
type Job struct {
	ID               string
	ProjectID        string
	EntityType       string
	EntityID         string
	JobType          JobType
	Status           Status
	Priority         int
	IdempotencyKey   string
	ProviderConfigID string
	ModelConfigID    string
	RemoteJobID      string
	Progress         *int
	InputJSON        string
	ResultJSON       string
	ErrorCode        string
	ErrorMessage     string
	NextRetryAt      time.Time
	CancelRequested  bool
	// CancelledRemoteUnconfirmed records that a provider-side job existed when
	// the user cancelled and its cancellation could not be confirmed. The UI
	// shows this as an orphan candidate rather than implying the provider
	// stopped.
	CancelledRemoteUnconfirmed bool
	AttemptCount               int
	MaxAttempts                int
	LeaseOwner                 string
	LeaseExpiresAt             time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
	StartedAt                  time.Time
	FinishedAt                 time.Time
	Revision                   int64
}

// IsActive reports whether the job still occupies the scheduler.
func (j Job) IsActive() bool { return j.Status.IsActive() }

// LeaseHeld reports whether a live lease blocks other workers.
func (j Job) LeaseHeld(now time.Time) bool {
	return j.LeaseOwner != "" && j.LeaseExpiresAt.After(now)
}

// IdempotencyKey derives the scope-plus-input hash described by
// DOMAIN_MODEL §2.3. It is deterministic so the same command produces the same
// key and the database uniqueness constraint can reject duplicates.
func IdempotencyKey(scope string, input []byte) string {
	sum := sha256.Sum256(append([]byte(scope+"\x00"), input...))
	return scope + ":" + hex.EncodeToString(sum[:16])
}
