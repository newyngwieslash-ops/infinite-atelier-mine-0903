package jobs

import (
	"context"
	"encoding/json"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// AdapterSource resolves a provider ID to the capability adapters. It uses the
// application ports directly so there is exactly one definition of each
// capability surface.
type AdapterSource interface {
	ImagePortFor(ctx context.Context, providerID string) (appjobs.ImagePort, error)
	VideoPortFor(ctx context.Context, providerID string) (appjobs.VideoPort, error)
	AudioPortFor(ctx context.Context, providerID string) (appjobs.AudioPort, error)
}

// Runner executes one job stage. It performs the network call outside any
// database transaction, then commits verified bytes through the result store.
type Runner struct {
	adapters  AdapterSource
	results   *ResultStore
	downloads DownloaderPort
	// MaxDownloadBytes caps a fetched result for this runner.
	MaxDownloadBytes int64
}

// NewRunner builds the runner.
//
// The runner performs no database writes of its own: every state change travels
// back through the worker's Outcome, so the worker's revision guard stays
// authoritative and a failed attempt can always be recorded.
func NewRunner(adapters AdapterSource, results *ResultStore, downloads DownloaderPort, maxDownloadBytes int64) *Runner {
	return &Runner{adapters: adapters, results: results, downloads: downloads, MaxDownloadBytes: maxDownloadBytes}
}

// imageInput carries the job's serialized image request.
type imageInput struct {
	Prompt         string   `json:"prompt"`
	Model          string   `json:"model"`
	ProviderID     string   `json:"providerId"`
	Count          int      `json:"count"`
	Size           string   `json:"size"`
	Quality        string   `json:"quality"`
	References     []string `json:"references,omitempty"`
	ReferenceMIMEs []string `json:"referenceMimes,omitempty"`
	Mask           string   `json:"mask,omitempty"`
	MaskMIME       string   `json:"maskMime,omitempty"`
}

type videoInput struct {
	Prompt     string `json:"prompt"`
	Model      string `json:"model"`
	ProviderID string `json:"providerId"`
	Seconds    int    `json:"seconds"`
	Size       string `json:"size"`
}

type audioInput struct {
	Text       string `json:"text"`
	Model      string `json:"model"`
	ProviderID string `json:"providerId"`
	Voice      string `json:"voice"`
	Format     string `json:"format"`
	Speed      string `json:"speed"`
}

// resultMetadata is what a finished job stores in result_json. It holds
// storage keys and provider identifiers only: never raw bytes, never secrets.
type resultMetadata struct {
	Files     []resultFile `json:"files,omitempty"`
	RemoteURL string       `json:"remoteUrl,omitempty"`
	RemoteID  string       `json:"remoteJobId,omitempty"`
	MIME      string       `json:"mime,omitempty"`
	Mode      string       `json:"mode,omitempty"`
}

type resultFile struct {
	StorageKey string `json:"storageKey"`
	MIME       string `json:"mime"`
	Size       int64  `json:"size"`
	Hash       string `json:"hash"`
}

// Run dispatches by job type. A job that produced a remote ID but no result yet
// parks in waiting_remote so the scheduler resumes it by polling.
func (r *Runner) Run(ctx context.Context, record job.Job) (appjobs.Outcome, error) {
	if r == nil || r.adapters == nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "Job execution is unavailable.")
	}
	switch record.JobType {
	case job.JobTypeImageGeneration, job.JobTypeImageEdit:
		return r.runImage(ctx, record)
	case job.JobTypeVideoGeneration:
		return r.runVideo(ctx, record)
	case job.JobTypeAudioGeneration:
		return r.runAudio(ctx, record)
	default:
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryUnsupported, "This job type is not supported yet.")
	}
}

func (r *Runner) runImage(ctx context.Context, record job.Job) (appjobs.Outcome, error) {
	var input imageInput
	if err := json.Unmarshal([]byte(record.InputJSON), &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed.")
	}
	providerID := input.ProviderID
	if providerID == "" {
		providerID = record.ProviderConfigID
	}
	if providerID == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryConfiguration, "No provider is configured for this job.")
	}
	// A previous attempt may already have obtained provider-hosted results and
	// been interrupted before the bytes were fetched. In that case the URLs are
	// persisted and the fetch resumes here — calling the provider again would
	// produce a second (billable) generation.
	if pending := parsePendingDownload(record.ResultJSON); len(pending) > 0 {
		return r.downloadPending(ctx, record, pending)
	}
	adapter, err := r.adapters.ImagePortFor(ctx, providerID)
	if err != nil {
		return appjobs.Outcome{}, err
	}
	request := appjobs.ImageRequest{
		JobID:      record.ID,
		ProviderID: providerID,
		Model:      input.Model,
		Prompt:     input.Prompt,
		Count:      input.Count,
		Size:       input.Size,
		Quality:    input.Quality,
	}
	for index, reference := range input.References {
		mimeType := "image/png"
		if index < len(input.ReferenceMIMEs) && input.ReferenceMIMEs[index] != "" {
			mimeType = input.ReferenceMIMEs[index]
		}
		request.References = append(request.References, appjobs.ImageInput{MIMEType: mimeType, Data: reference})
	}
	if input.Mask != "" {
		mimeType := input.MaskMIME
		if mimeType == "" {
			mimeType = "image/png"
		}
		request.Mask = &appjobs.ImageInput{MIMEType: mimeType, Data: input.Mask}
	}

	outcome, err := adapter.Generate(ctx, request)
	if err != nil {
		return appjobs.Outcome{}, err
	}
	// Inline results are stored immediately; remote URLs are fetched under the
	// download policy so the job never reports success for bytes that were not
	// verified locally.
	if len(outcome.Results) > 0 {
		metadata := resultMetadata{Mode: "inline"}
		for _, result := range outcome.Results {
			committed, commitErr := r.results.CommitInline(ctx, record.ID, "image", result.MIMEType, result.Data)
			if commitErr != nil {
				return appjobs.Outcome{}, commitErr
			}
			metadata.Files = append(metadata.Files, resultFile{
				StorageKey: committed.StorageKey,
				MIME:       committed.MIME,
				Size:       committed.Size,
				Hash:       committed.Hash,
			})
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "The job result could not be recorded.")
		}
		return appjobs.Outcome{Status: job.StatusSucceeded, ResultJSON: string(encoded)}, nil
	}
	if len(outcome.RemoteURLs) > 0 {
		// The provider produced results. Persist the URLs and return the
		// downloading phase so the worker records it as part of its own
		// revision chain; the next pass fetches the bytes without calling the
		// provider again.
		encoded, err := json.Marshal(pendingDownload{Mode: pendingDownloadMode, URLs: outcome.RemoteURLs})
		if err != nil {
			return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "The job result could not be recorded.")
		}
		return appjobs.Outcome{Status: job.StatusDownloading, ResultJSON: string(encoded)}, nil
	}
	if outcome.RemoteJobID != "" {
		// The port documents this as the asynchronous answer. No shipping image
		// adapter uses it yet, and the runner has no image poll implementation, so
		// honouring it as a success would fabricate a result and parking it would
		// strand the job. It is reported as unsupported instead of being silently
		// treated as "no image": a future adapter must add polling to the image
		// path before it may return a remote ID.
		return appjobs.Outcome{}, job.FailedJobError(
			job.CategoryUnsupported,
			"Asynchronous image jobs are not supported by this version.",
		)
	}
	return appjobs.Outcome{}, job.FailedJobError(job.CategoryResponseInvalid, "The provider returned no image.")
}

// pendingDownloadMode marks a job whose provider work is finished and whose
// bytes are still to be fetched. It is consumed by the next attempt.
const pendingDownloadMode = "pending_download"

// pendingDownload is the persisted marker described above.
type pendingDownload struct {
	Mode string   `json:"mode"`
	URLs []string `json:"urls"`
}

// parsePendingDownload reads the marker, returning no URLs when the job holds
// an ordinary result or an unreadable payload.
func parsePendingDownload(resultJSON string) []string {
	if resultJSON == "" {
		return nil
	}
	var pending pendingDownload
	if err := json.Unmarshal([]byte(resultJSON), &pending); err != nil {
		return nil
	}
	if pending.Mode != pendingDownloadMode {
		return nil
	}
	return pending.URLs
}

// downloadPending fetches already-produced results and commits them. It is the
// only path that runs after the provider work is complete.
func (r *Runner) downloadPending(ctx context.Context, record job.Job, urls []string) (appjobs.Outcome, error) {
	metadata := resultMetadata{Mode: "downloaded"}
	for _, remoteURL := range urls {
		committed, commitErr := r.results.DownloadAndCommit(ctx, r.downloads, record.ID, "image", remoteURL, r.MaxDownloadBytes)
		if commitErr != nil {
			// The provider work is already recorded, so a failure here is a
			// download failure: the retry resumes the fetch, not the generation.
			return appjobs.Outcome{}, commitErr
		}
		metadata.Files = append(metadata.Files, resultFile{
			StorageKey: committed.StorageKey,
			MIME:       committed.MIME,
			Size:       committed.Size,
			Hash:       committed.Hash,
		})
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "The job result could not be recorded.")
	}
	return appjobs.Outcome{Status: job.StatusSucceeded, ResultJSON: string(encoded)}, nil
}

// runVideo drives the async contract. A job with no remote ID submits; a job
// that already has one polls. It never re-submits, which is what prevents a
// duplicate charge after a restart.
func (r *Runner) runVideo(ctx context.Context, record job.Job) (appjobs.Outcome, error) {
	var input videoInput
	if err := json.Unmarshal([]byte(record.InputJSON), &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed.")
	}
	providerID := input.ProviderID
	if providerID == "" {
		providerID = record.ProviderConfigID
	}
	if providerID == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryConfiguration, "No provider is configured for this job.")
	}
	adapter, err := r.adapters.VideoPortFor(ctx, providerID)
	if err != nil {
		return appjobs.Outcome{}, err
	}

	if record.RemoteJobID == "" {
		remote, err := adapter.Submit(ctx, appjobs.VideoRequest{
			JobID:      record.ID,
			ProviderID: providerID,
			Model:      input.Model,
			Prompt:     input.Prompt,
			Seconds:    input.Seconds,
			Size:       input.Size,
		})
		if err != nil {
			return appjobs.Outcome{}, err
		}
		// Persist the remote handle and park: the scheduler resumes by polling.
		return appjobs.Outcome{Status: job.StatusWaitingRemote, RemoteJobID: remote.ID}, nil
	}

	remote := appjobs.RemoteJob{ProviderID: providerID, ID: record.RemoteJobID}
	status, err := adapter.Poll(ctx, remote)
	if err != nil {
		return appjobs.Outcome{}, err
	}
	if !status.Done {
		// PollOnly tells the worker this pass produced nothing: it must be paced
		// and must not consume the attempt budget.
		return appjobs.Outcome{Status: job.StatusWaitingRemote, RemoteJobID: record.RemoteJobID, Progress: status.Progress, PollOnly: true}, nil
	}
	if status.Failed {
		// The provider reported a terminal failure for this remote job.
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryRemotePermanent, "The provider could not produce this result.")
	}
	media, err := adapter.Fetch(ctx, remote)
	if err != nil {
		return appjobs.Outcome{}, err
	}
	mimeType := media.MIMEType
	if mimeType == "" {
		mimeType = "video/mp4"
	}
	metadata := resultMetadata{Mode: "downloaded", RemoteID: record.RemoteJobID, MIME: mimeType}
	if media.Data != "" {
		committed, commitErr := r.results.CommitInline(ctx, record.ID, "video", mimeType, media.Data)
		if commitErr != nil {
			return appjobs.Outcome{}, commitErr
		}
		metadata.Files = append(metadata.Files, resultFile{
			StorageKey: committed.StorageKey, MIME: committed.MIME, Size: committed.Size, Hash: committed.Hash,
		})
	} else if media.URL != "" {
		committed, commitErr := r.results.DownloadAndCommit(ctx, r.downloads, record.ID, "video", media.URL, r.MaxDownloadBytes)
		if commitErr != nil {
			return appjobs.Outcome{}, commitErr
		}
		metadata.Files = append(metadata.Files, resultFile{
			StorageKey: committed.StorageKey, MIME: committed.MIME, Size: committed.Size, Hash: committed.Hash,
		})
	} else {
		// Nothing retrievable: the remote result is acknowledged but not stored
		// locally, which is a distinct terminal outcome rather than a success.
		return appjobs.Outcome{Status: job.StatusRemoteOnly, RemoteOnly: true, RemoteJobID: record.RemoteJobID}, nil
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "The job result could not be recorded.")
	}
	return appjobs.Outcome{Status: job.StatusSucceeded, ResultJSON: string(encoded), RemoteJobID: record.RemoteJobID}, nil
}

func (r *Runner) runAudio(ctx context.Context, record job.Job) (appjobs.Outcome, error) {
	var input audioInput
	if err := json.Unmarshal([]byte(record.InputJSON), &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed.")
	}
	providerID := input.ProviderID
	if providerID == "" {
		providerID = record.ProviderConfigID
	}
	if providerID == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryConfiguration, "No provider is configured for this job.")
	}
	adapter, err := r.adapters.AudioPortFor(ctx, providerID)
	if err != nil {
		return appjobs.Outcome{}, err
	}
	outcome, err := adapter.GenerateAudio(ctx, appjobs.AudioRequest{
		JobID:      record.ID,
		ProviderID: providerID,
		Model:      input.Model,
		Text:       input.Text,
		Voice:      input.Voice,
		Format:     input.Format,
		Speed:      input.Speed,
	})
	if err != nil {
		return appjobs.Outcome{}, err
	}
	mimeType := outcome.MIMEType
	if mimeType == "" {
		mimeType = "audio/mpeg"
	}
	metadata := resultMetadata{Mode: "inline", MIME: mimeType}
	if outcome.Data != "" {
		committed, commitErr := r.results.CommitInline(ctx, record.ID, "audio", mimeType, outcome.Data)
		if commitErr != nil {
			return appjobs.Outcome{}, commitErr
		}
		metadata.Files = append(metadata.Files, resultFile{
			StorageKey: committed.StorageKey, MIME: committed.MIME, Size: committed.Size, Hash: committed.Hash,
		})
	} else if outcome.URL != "" {
		committed, commitErr := r.results.DownloadAndCommit(ctx, r.downloads, record.ID, "audio", outcome.URL, r.MaxDownloadBytes)
		if commitErr != nil {
			return appjobs.Outcome{}, commitErr
		}
		metadata.Files = append(metadata.Files, resultFile{
			StorageKey: committed.StorageKey, MIME: committed.MIME, Size: committed.Size, Hash: committed.Hash,
		})
	} else {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryResponseInvalid, "The provider returned no audio.")
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "The job result could not be recorded.")
	}
	return appjobs.Outcome{Status: job.StatusSucceeded, ResultJSON: string(encoded)}, nil
}

// Ensure the provider registry satisfies the runner's adapter source.
var _ AdapterSource = (*providers.Registry)(nil)
