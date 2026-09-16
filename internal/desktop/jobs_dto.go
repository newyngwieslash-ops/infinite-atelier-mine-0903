package desktop

import (
	"encoding/json"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// imageJobInput is the serialized job input for an image job. It is duplicated
// from the runner's private type on purpose: the binding owns the shape the
// frontend submits, and the runner owns the shape it reads. If the two drift,
// the runner reports a malformed-input failure rather than misreading a field.
type imageJobInput struct {
	Prompt         string   `json:"prompt"`
	Model          string   `json:"model"`
	ProviderID     string   `json:"providerId"`
	Count          int      `json:"count"`
	Size           string   `json:"size,omitempty"`
	Quality        string   `json:"quality,omitempty"`
	References     []string `json:"references,omitempty"`
	ReferenceMIMEs []string `json:"referenceMimes,omitempty"`
	Mask           string   `json:"mask,omitempty"`
	MaskMIME       string   `json:"maskMime,omitempty"`
}

// resultMetadataDTO mirrors the runner's stored result shape for display.
type resultMetadataDTO struct {
	Files []struct {
		StorageKey string `json:"storageKey"`
		MIME       string `json:"mime"`
		Size       int64  `json:"size"`
		Hash       string `json:"hash"`
	} `json:"files"`
	RemoteURL string `json:"remoteUrl"`
	RemoteID  string `json:"remoteJobId"`
	Mode      string `json:"mode"`
}

// toJobDTO converts a domain job into the transport view. Only stable,
// non-sensitive fields cross the boundary: provider text, request payloads and
// secrets never appear here.
func toJobDTO(record job.Job) JobDTO {
	dto := JobDTO{
		ID:           record.ID,
		ProjectID:    record.ProjectID,
		EntityType:   record.EntityType,
		EntityID:     record.EntityID,
		JobType:      string(record.JobType),
		Status:       string(record.Status),
		Priority:     record.Priority,
		RemoteJobID:  record.RemoteJobID,
		ErrorCode:    record.ErrorCode,
		AttemptCount: record.AttemptCount,
		MaxAttempts:  record.MaxAttempts,
	}
	dto.Progress = job.ProgressFromStatus(record.Status)
	if record.Progress != nil {
		dto.Progress = *record.Progress
	}
	if !record.CreatedAt.IsZero() {
		dto.CreatedAt = record.CreatedAt.UTC().Format(timeLayout)
	}
	if !record.UpdatedAt.IsZero() {
		dto.UpdatedAt = record.UpdatedAt.UTC().Format(timeLayout)
	}
	if !record.FinishedAt.IsZero() {
		dto.FinishedAt = record.FinishedAt.UTC().Format(timeLayout)
	}
	if record.Status == job.StatusRemoteOnly {
		dto.ResultRemoteOnly = true
	}
	// A cancelled job whose provider-side work may still be running is shown as
	// an orphan candidate: the UI must not imply the provider stopped.
	if record.CancelledRemoteUnconfirmed {
		dto.ResultRemoteOnly = true
		dto.CancelledRemoteUnconfirmed = true
	}
	if record.ResultJSON != "" {
		var metadata resultMetadataDTO
		if err := json.Unmarshal([]byte(record.ResultJSON), &metadata); err == nil {
			for _, file := range metadata.Files {
				dto.ResultFiles = append(dto.ResultFiles, JobResultFileDTO{
					StorageKey: file.StorageKey,
					MIME:       file.MIME,
					Size:       file.Size,
				})
			}
			if metadata.Mode == "remote_only" || (len(metadata.Files) == 0 && record.Status == job.StatusRemoteOnly) {
				dto.ResultRemoteOnly = true
			}
		}
	}
	return dto
}
