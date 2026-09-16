package desktop

import (
	"context"
	"encoding/json"
	"io"
	"sync"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// JobsBinding is the narrow Wails surface for the persistent job manager.
//
// It exposes queue queries and user actions only. There is deliberately no
// method to execute a job, read a provider secret, resolve a file path, or run
// arbitrary SQL: the scheduler and workers own execution.
type JobsBinding struct {
	mu      sync.RWMutex
	ctx     context.Context
	service *appjobs.Service
	// results reads stored job artifacts by content-addressed key.
	results ResultReader
}

// ResultReader reads a stored artifact.
type ResultReader interface {
	Open(ctx context.Context, storageKey string) (io.ReadCloser, error)
}

// AttachJobResultReader supplies the artifact reader. It is separate from
// AttachJobs so the read path is only available when a FileStore exists.
func AttachJobResultReader(binding *JobsBinding, reader ResultReader) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.results = reader
	binding.mu.Unlock()
}

// isStorageKey enforces the content-addressed key shape before any file access.
func isStorageKey(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

// JobDTO is the transport view of a job. It carries storage keys and a
// category-level error code, never provider text or raw bytes.
type JobDTO struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	EntityType  string `json:"entityType"`
	EntityID    string `json:"entityId"`
	JobType     string `json:"jobType"`
	Status      string `json:"status"`
	Priority    int    `json:"priority"`
	RemoteJobID string `json:"remoteJobId,omitempty"`
	Progress    int    `json:"progress"`
	// ErrorCode is a stable category, not a provider message.
	ErrorCode    string `json:"errorCode,omitempty"`
	AttemptCount int    `json:"attemptCount"`
	MaxAttempts  int    `json:"maxAttempts"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	FinishedAt   string `json:"finishedAt,omitempty"`
	// ResultFiles lists the stored artifacts; empty until the job succeeds.
	ResultFiles []JobResultFileDTO `json:"resultFiles,omitempty"`
	// ResultRemoteOnly marks a result that exists only at the provider.
	ResultRemoteOnly bool `json:"resultRemoteOnly,omitempty"`
	// CancelledRemoteUnconfirmed marks a cancelled job whose provider-side work
	// may still be running.
	CancelledRemoteUnconfirmed bool `json:"cancelledRemoteUnconfirmed,omitempty"`
}

// JobResultFileDTO names one stored artifact.
type JobResultFileDTO struct {
	StorageKey string `json:"storageKey"`
	MIME       string `json:"mime"`
	Size       int64  `json:"size"`
}

// JobAttemptDTO is one attempt's history row.
type JobAttemptDTO struct {
	ID            string `json:"id"`
	AttemptNumber int    `json:"attemptNumber"`
	Status        string `json:"status"`
	ErrorCode     string `json:"errorCode,omitempty"`
	StartedAt     string `json:"startedAt"`
	FinishedAt    string `json:"finishedAt,omitempty"`
}

// ListJobsRequest is the query payload. Every field is optional.
type ListJobsRequest struct {
	Statuses   []string `json:"statuses,omitempty"`
	ActiveOnly bool     `json:"activeOnly,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Offset     int      `json:"offset,omitempty"`
}

// SubmitImageJobRequest asks for an image generation or edit job.
type SubmitImageJobRequest struct {
	ProjectID  string `json:"projectId"`
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
	Prompt     string `json:"prompt"`
	Count      int    `json:"count,omitempty"`
	Size       string `json:"size,omitempty"`
	Quality    string `json:"quality,omitempty"`
	// References and Mask carry base64 or data URLs. They are job input, not
	// secrets, and are stored on the job row as JSON.
	References     []string `json:"references,omitempty"`
	ReferenceMIMEs []string `json:"referenceMimes,omitempty"`
	Mask           string   `json:"mask,omitempty"`
	MaskMIME       string   `json:"maskMime,omitempty"`
	Priority       int      `json:"priority,omitempty"`
}

// QueueSummaryDTO is the Job Center header.
type QueueSummaryDTO struct {
	Queued      int  `json:"queued"`
	Running     int  `json:"running"`
	Waiting     int  `json:"waiting"`
	Failed      int  `json:"failed"`
	Succeeded   int  `json:"succeeded"`
	Cancelled   int  `json:"cancelled"`
	ActiveTotal int  `json:"activeTotal"`
	Paused      bool `json:"paused"`
}

// ListJobs returns jobs matching the filter, newest first.
func (b *JobsBinding) ListJobs(request ListJobsRequest) ([]JobDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return nil, err
	}
	filter := appjobs.ListFilter{
		ActiveOnly: request.ActiveOnly,
		Limit:      request.Limit,
		Offset:     request.Offset,
	}
	for _, status := range request.Statuses {
		if !job.IsValidStatus(job.Status(status)) {
			return nil, bindingInvalidInput()
		}
		filter.Statuses = append(filter.Statuses, job.Status(status))
	}
	records, err := service.List(ctx, filter)
	if err != nil {
		return nil, toAppError(err)
	}
	dtos := make([]JobDTO, 0, len(records))
	for _, record := range records {
		dtos = append(dtos, toJobDTO(record))
	}
	return dtos, nil
}

// GetJob returns one job.
func (b *JobsBinding) GetJob(id string) (JobDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return JobDTO{}, err
	}
	record, err := service.Get(ctx, id)
	if err != nil {
		return JobDTO{}, toAppError(err)
	}
	return toJobDTO(record), nil
}

// JobAttempts returns a job's attempt history.
func (b *JobsBinding) JobAttempts(id string) ([]JobAttemptDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return nil, err
	}
	attempts, err := service.Attempts(ctx, id)
	if err != nil {
		return nil, toAppError(err)
	}
	dtos := make([]JobAttemptDTO, 0, len(attempts))
	for _, attempt := range attempts {
		dto := JobAttemptDTO{
			ID:            attempt.ID,
			AttemptNumber: attempt.AttemptNumber,
			Status:        string(attempt.Status),
			ErrorCode:     attempt.ErrorCode,
		}
		if !attempt.StartedAt.IsZero() {
			dto.StartedAt = attempt.StartedAt.UTC().Format(timeLayout)
		}
		if !attempt.FinishedAt.IsZero() {
			dto.FinishedAt = attempt.FinishedAt.UTC().Format(timeLayout)
		}
		dtos = append(dtos, dto)
	}
	return dtos, nil
}

// SubmitImageJob enqueues an image generation or edit job. The returned DTO is
// the existing job when an identical request was already submitted, so callers
// can treat submission as idempotent.
func (b *JobsBinding) SubmitImageJob(request SubmitImageJobRequest) (JobDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return JobDTO{}, err
	}
	if request.ProviderID == "" || request.Model == "" || request.Prompt == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	jobType := job.JobTypeImageGeneration
	if len(request.References) > 0 || request.Mask != "" {
		jobType = job.JobTypeImageEdit
	}
	count := request.Count
	if count <= 0 {
		count = 1
	}
	if count > maxImageBatch {
		// A single command must not enqueue an unbounded batch: bulk work has
		// a cost, and SECURITY requires an explicit limit.
		return JobDTO{}, bindingInvalidInput()
	}
	input := imageJobInput{
		Prompt:         request.Prompt,
		Model:          request.Model,
		ProviderID:     request.ProviderID,
		Count:          count,
		Size:           request.Size,
		Quality:        request.Quality,
		References:     request.References,
		ReferenceMIMEs: request.ReferenceMIMEs,
		Mask:           request.Mask,
		MaskMIME:       request.MaskMIME,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return JobDTO{}, bindingInvalidInput()
	}
	record, duplicate, err := service.Submit(ctx, appjobs.SubmitRequest{
		ProjectID:        request.ProjectID,
		EntityType:       request.EntityType,
		EntityID:         request.EntityID,
		JobType:          jobType,
		Priority:         request.Priority,
		ProviderConfigID: request.ProviderID,
		InputJSON:        string(encoded),
		Scope:            "submit-image-job",
	})
	if err != nil {
		return JobDTO{}, toAppError(err)
	}
	// A replayed request returns the existing job unchanged, including its
	// real job type: an edit job must not be reported as a plain generation.
	_ = duplicate
	return toJobDTO(record), nil
}

// CancelJobs cancels the given jobs and reports how many changed state.
func (b *JobsBinding) CancelJobs(ids []string) (int, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > maxBatchAction {
		return 0, bindingInvalidInput()
	}
	count, err := service.CancelMany(ctx, ids)
	if err != nil {
		return count, toAppError(err)
	}
	return count, nil
}

// RetryFailedJobs re-queues the failed/orphaned jobs among the given IDs.
func (b *JobsBinding) RetryFailedJobs(ids []string) (int, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > maxBatchAction {
		return 0, bindingInvalidInput()
	}
	count, err := service.RetryFailed(ctx, ids)
	if err != nil {
		return count, toAppError(err)
	}
	return count, nil
}

// PauseQueue stops handing out new work.
func (b *JobsBinding) PauseQueue() error {
	service, _, err := b.requestService()
	if err != nil {
		return err
	}
	service.Pause()
	return nil
}

// ResumeQueue starts handing out work again.
func (b *JobsBinding) ResumeQueue() error {
	service, _, err := b.requestService()
	if err != nil {
		return err
	}
	service.Resume()
	return nil
}

// GetQueueStatus returns the queue summary.
func (b *JobsBinding) GetQueueStatus() (QueueSummaryDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return QueueSummaryDTO{}, err
	}
	summary, err := service.Summary(ctx)
	if err != nil {
		return QueueSummaryDTO{}, toAppError(err)
	}
	return QueueSummaryDTO{
		Queued:      summary.Queued,
		Running:     summary.Running,
		Waiting:     summary.Waiting,
		Failed:      summary.Failed,
		Succeeded:   summary.Succeeded,
		Cancelled:   summary.Cancelled,
		ActiveTotal: summary.ActiveTotal,
		Paused:      summary.Paused,
	}, nil
}

func (b *JobsBinding) requestService() (*appjobs.Service, context.Context, error) {
	if b == nil {
		return nil, nil, bindingUnavailable()
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.service
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return nil, nil, bindingUnavailable()
	}
	return service, ctx, nil
}

// JobResultFileContent is the payload returned when reading a stored result.
type JobResultFileContent struct {
	MIME string `json:"mime"`
	// DataURL carries the bytes as a data URL so the canvas can display them
	// with the same code path it already uses for generated images.
	DataURL string `json:"dataUrl"`
	Size    int64  `json:"size"`
}

// ReadResultFile returns one stored job result by its content-addressed key.
//
// The key must be a 64-character lowercase hex hash: the FileStore validates
// the same shape and performs its own root-containment check, so this method
// cannot be used to read an arbitrary path. There is no path parameter and no
// directory listing.
func (b *JobsBinding) ReadResultFile(storageKey string) (JobResultFileContent, error) {
	if b == nil {
		return JobResultFileContent{}, bindingUnavailable()
	}
	b.mu.RLock()
	ctx := b.ctx
	reader := b.results
	b.mu.RUnlock()
	if ctx == nil || reader == nil {
		return JobResultFileContent{}, bindingUnavailable()
	}
	if !isStorageKey(storageKey) {
		return JobResultFileContent{}, bindingInvalidInput()
	}
	return readResultFile(ctx, reader, storageKey)
}

// AttachJobs stores the Wails startup context and service. Not a Wails method.
func AttachJobs(binding *JobsBinding, ctx context.Context, service *appjobs.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.service = service
	binding.mu.Unlock()
}

// Bounds for user-triggered batch actions. They exist so one command cannot
// queue unbounded work (SECURITY: bulk generation needs a limit).
const (
	maxImageBatch  = 8
	maxBatchAction = 200
)

const timeLayout = "2006-01-02T15:04:05Z07:00"
