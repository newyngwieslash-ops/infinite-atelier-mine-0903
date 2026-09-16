// Package jobs is the application layer for the persistent job manager. It
// owns use cases (submit, cancel, retry, schedule) and defines the ports that
// infrastructure implements. It performs no I/O itself and never talks to
// Wails, SQLite, or an HTTP client directly.
package jobs

import (
	"context"
	"io"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// Clock abstracts time so scheduling and lease tests are deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces unique identifiers.
type IDGenerator interface {
	NewID(prefix string) string
}

// Repository is the persistence port for the job aggregate. Implementations
// must be safe for concurrent use; the scheduler and workers call it from
// several goroutines.
type Repository interface {
	// Insert stores a new job. A duplicate (project_id, idempotency_key) must
	// return job.DuplicateJobError so callers can fetch the existing row.
	Insert(ctx context.Context, record job.Job) error
	// Get returns one job by ID.
	Get(ctx context.Context, id string) (job.Job, error)
	// GetByIdempotencyKey returns the job already stored for the key.
	GetByIdempotencyKey(ctx context.Context, projectID, key string) (job.Job, error)
	// List returns jobs matching the filter, newest first.
	List(ctx context.Context, filter ListFilter) ([]job.Job, error)
	// Claim atomically moves a queued job to running for one worker. It returns
	// job.DuplicateJobError when another worker won the race.
	Claim(ctx context.Context, id, workerID string, leaseUntil time.Time, now time.Time) (job.Job, error)
	// Update persists a status/field change guarded by the expected revision.
	// A revision mismatch returns job.DuplicateJobError (conflict).
	Update(ctx context.Context, record job.Job, expectedRevision int64) error
	// RequestCancel flags a job for cancellation and reports the updated row.
	RequestCancel(ctx context.Context, id string, now time.Time) (job.Job, error)
	// ClaimableCandidates returns the IDs of jobs the scheduler may run now,
	// in priority order, bounded by limit.
	ClaimableCandidates(ctx context.Context, now time.Time, limit int) ([]job.Job, error)
	// ActiveCounts reports jobs per status for the queue summary.
	ActiveCounts(ctx context.Context) (map[job.Status]int, error)

	// Attempts.
	StartAttempt(ctx context.Context, attempt job.Attempt) error
	FinishAttempt(ctx context.Context, attempt job.Attempt) error
	ListAttempts(ctx context.Context, jobID string) ([]job.Attempt, error)

	// Dependencies.
	AddDependency(ctx context.Context, dependency job.Dependency) error
	ListDependencies(ctx context.Context, jobID string) ([]job.Dependency, error)
}

// ListFilter narrows a job query. Zero values mean "no constraint".
type ListFilter struct {
	Statuses []job.Status
	// ActiveOnly restricts to non-terminal jobs.
	ActiveOnly bool
	Limit      int
	Offset     int
}

// Attempt is the persistence view of one execution attempt.
type Attempt = job.Attempt

// Dependency is the persistence view of a job requirement.
type Dependency = job.Dependency

// ImageRequest is a provider-agnostic image generation or edit request.
type ImageRequest struct {
	JobID      string
	ProviderID string
	Model      string
	Prompt     string
	// Count is the number of images requested.
	Count int
	// Size and Quality are provider parameters passed through as configured.
	Size    string
	Quality string
	// References are input images for an edit or image-to-image request.
	References []ImageInput
	// Mask is an optional edit mask.
	Mask *ImageInput
}

// ImageInput is one input image. Data carries base64 or a data URL; the
// adapter decides how to transmit it. Bytes is used when the caller already
// holds raw content from the FileStore.
type ImageInput struct {
	MIMEType string
	Data     string
	Bytes    []byte
}

// ImageResult is one produced image.
type ImageResult struct {
	// Data is inline base64 when the provider returned it directly.
	Data string
	// URL is a remote reference when the provider returned a link instead.
	URL      string
	MIMEType string
	// RevisedPrompt is provider-reported and informational only.
	RevisedPrompt string
}

// ImagePort is the capability port for image generation and edits.
type ImagePort interface {
	// Generate produces images from a prompt. A non-nil RemoteJobID in the
	// result means the provider is asynchronous and must be polled.
	Generate(ctx context.Context, request ImageRequest) (ImageOutcome, error)
}

// ImageOutcome is the adapter's answer: either finished results, or a remote
// job that must be polled, or a durable remote URL.
type ImageOutcome struct {
	// Results are inline finished images.
	Results []ImageResult
	// RemoteJobID is set when the provider accepted an asynchronous job.
	RemoteJobID string
	// RemoteURLs are provider-hosted results that still need downloading.
	RemoteURLs []string
	// RemoteOnly reports that the adapter deliberately did not expose a
	// downloadable result (for example the provider returned an expiring link
	// and download is disabled).
	RemoteOnly bool
}

// VideoPort is the async video contract. WP-03 ships a mock implementation so
// the job pipeline is testable end to end; the real adapter belongs to WP-11.
type VideoPort interface {
	Submit(ctx context.Context, request VideoRequest) (RemoteJob, error)
	Poll(ctx context.Context, remote RemoteJob) (RemoteStatus, error)
	Fetch(ctx context.Context, remote RemoteJob) (MediaOutcome, error)
	Cancel(ctx context.Context, remote RemoteJob) error
}

// VideoRequest describes a video generation submission.
type VideoRequest struct {
	JobID      string
	ProviderID string
	Model      string
	Prompt     string
	Seconds    int
	Size       string
	References []ImageInput
}

// RemoteJob identifies an accepted asynchronous provider job.
type RemoteJob struct {
	ProviderID string
	ID         string
}

// RemoteStatus is one poll result.
type RemoteStatus struct {
	// Done reports that the provider finished (successfully or not).
	Done bool
	// Failed reports a terminal provider-side failure.
	Failed bool
	// Message is a safe provider message, never a raw body.
	Message string
	// Progress is provider-reported completion, when available.
	Progress *int
}

// MediaOutcome is a fetched media result.
type MediaOutcome struct {
	URL      string
	Data     string
	MIMEType string
}

// AudioPort is the TTS contract. WP-03 ships a mock; the real adapter belongs
// to a later package.
type AudioPort interface {
	GenerateAudio(ctx context.Context, request AudioRequest) (AudioOutcome, error)
}

// AudioRequest describes a text-to-speech request.
type AudioRequest struct {
	JobID      string
	ProviderID string
	Model      string
	Text       string
	Voice      string
	Format     string
	Speed      string
}

// AudioOutcome is a produced audio result.
type AudioOutcome struct {
	Data     string
	URL      string
	MIMEType string
	Bytes    []byte
}

// ResultWriter accepts a verified result payload into managed storage.
//
// `io` is used by the port's signature, so it stays imported here.
type ResultWriter interface {
	// Commit stores the bytes and returns the stable storage key plus the
	// detected MIME type. Implementations validate size and content.
	Commit(ctx context.Context, jobID, mimeHint string, body io.Reader, declaredSize int64) (CommittedFile, error)
}

// CommittedFile is the outcome of a successful result commit.
type CommittedFile struct {
	StorageKey string
	MIME       string
	Size       int64
	Hash       string
}

// Publisher emits job lifecycle events to the UI. Implementations must be
// non-blocking; a slow or missing UI must never stall a worker.
type Publisher interface {
	PublishJobChanged(ctx context.Context, event JobEvent)
}

// JobEventName is the event type carried in the desktop envelope.
const JobEventName = "job.changed"

// JobEvent is the payload published on every meaningful job transition.
type JobEvent struct {
	JobID    string `json:"jobId"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	// ErrorCode is a category string, never a raw provider message.
	ErrorCode string `json:"errorCode,omitempty"`
	// UpdatedAt is RFC3339.
	UpdatedAt string `json:"updatedAt"`
}

// Note: the download surface for provider-supplied result URLs lives in
// infrastructure (providerhttp.Downloader) because it needs the untrusted-URL
// policy from docs/adr/0004. It is not declared here because no application
// service calls it directly; the runner owns that step.
