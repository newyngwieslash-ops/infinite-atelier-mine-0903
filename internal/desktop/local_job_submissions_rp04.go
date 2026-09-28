package desktop

import (
	"encoding/json"
	"strings"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	infrajobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/jobs"
)

// local_job_submissions_rp04.go is RP-04.4's user-facing surface: NARROW
// submission commands for the four local job types, one per capability.
//
// # Why narrow commands instead of a generic submit
//
// A single `SubmitLocalJob(jobType, json)` binding would be an arbitrary-input
// back door: any caller could queue any type with any payload, and the runner's
// per-type validation would be the only line of defence — after the row was
// already persisted and visible in the Job Center. Each narrow command names
// its own typed request, builds the versioned input document itself (the
// caller cannot state an unknown field, a wrong version, or an OS path), and
// refuses an input that does not name its subject BEFORE anything persists.
//
// The input documents are the typed ones from `local_input.go` (RP-04.1), so
// what the user submits and what the runner decodes are the same shape by
// construction.

// SubmitThumbnailJobRequest names the media version a thumbnail derives from.
type SubmitThumbnailJobRequest struct {
	ProjectID string `json:"projectId"`
	// AssetVersionID is the approved media version the thumbnail is of.
	AssetVersionID string `json:"assetVersionId"`
	// Max sizes are OPTIONAL; the handler's defaults (320×180) apply when
	// both are zero.
	MaxWidthPixels  int `json:"maxWidthPixels,omitempty"`
	MaxHeightPixels int `json:"maxHeightPixels,omitempty"`
}

// SubmitThumbnailJob queues one thumbnail derivation.
func (b *JobsBinding) SubmitThumbnailJob(request SubmitThumbnailJobRequest) (JobDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return JobDTO{}, err
	}
	projectID := strings.TrimSpace(request.ProjectID)
	versionID := strings.TrimSpace(request.AssetVersionID)
	if projectID == "" || versionID == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	input := infrajobs.ThumbnailInput{
		ProjectID:       projectID,
		Version:         infrajobs.LocalInputVersion,
		AssetVersionID:  versionID,
		MaxWidthPixels:  request.MaxWidthPixels,
		MaxHeightPixels: request.MaxHeightPixels,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return JobDTO{}, bindingInvalidInput()
	}
	record, _, err := service.Submit(ctx, appjobs.SubmitRequest{
		ProjectID:  projectID,
		EntityType: "asset_version",
		EntityID:   versionID,
		JobType:    job.JobTypeThumbnail,
		InputJSON:  string(encoded),
		Scope:      "submit-thumbnail-job",
	})
	if err != nil {
		return JobDTO{}, toAppError(err)
	}
	return toJobDTO(record), nil
}

// SubmitImportJobRequest names the stored document an import job continues.
type SubmitImportJobRequest struct {
	ProjectID string `json:"projectId"`
	// DocumentID is the stored source document. The job reads the document's
	// LATEST version's content from the managed store (RP-04.2); it never
	// accepts bytes or a path.
	DocumentID string `json:"documentId"`
}

// SubmitImportJob queues one document import.
func (b *JobsBinding) SubmitImportJob(request SubmitImportJobRequest) (JobDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return JobDTO{}, err
	}
	projectID := strings.TrimSpace(request.ProjectID)
	documentID := strings.TrimSpace(request.DocumentID)
	if projectID == "" || documentID == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	input := infrajobs.ImportInput{
		ProjectID:  projectID,
		Version:    infrajobs.LocalInputVersion,
		DocumentID: documentID,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return JobDTO{}, bindingInvalidInput()
	}
	record, _, err := service.Submit(ctx, appjobs.SubmitRequest{
		ProjectID:  projectID,
		EntityType: "source_document",
		EntityID:   documentID,
		JobType:    job.JobTypeImport,
		InputJSON:  string(encoded),
		Scope:      "submit-import-job",
	})
	if err != nil {
		return JobDTO{}, toAppError(err)
	}
	return toJobDTO(record), nil
}

// SubmitExportJobRequest names the episode an export composes, with the
// parameters the interactive export exposes.
type SubmitExportJobRequest struct {
	ProjectID string `json:"projectId"`
	EpisodeID string `json:"episodeId"`
	// Quality is the exporter's vocabulary (preview/final). Empty is preview.
	Quality string `json:"quality,omitempty"`
	// FPS is the composed film's frame rate. Zero is the exporter's default.
	FPS int `json:"fps,omitempty"`
	// ApprovedBoardVersionID pins the board version. Empty uses the episode's
	// current approved board.
	ApprovedBoardVersionID string `json:"approvedBoardVersionId,omitempty"`
}

// SubmitExportJob queues one episode composition.
func (b *JobsBinding) SubmitExportJob(request SubmitExportJobRequest) (JobDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return JobDTO{}, err
	}
	projectID := strings.TrimSpace(request.ProjectID)
	episodeID := strings.TrimSpace(request.EpisodeID)
	if projectID == "" || episodeID == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	if quality := strings.TrimSpace(request.Quality); quality != "" && quality != "preview" && quality != "final" {
		return JobDTO{}, bindingInvalidInput()
	}
	if request.FPS < 0 || request.FPS > 120 {
		return JobDTO{}, bindingInvalidInput()
	}
	input := infrajobs.ExportInput{
		ProjectID:              projectID,
		Version:                infrajobs.LocalInputVersion,
		EpisodeID:              episodeID,
		Quality:                strings.TrimSpace(request.Quality),
		FPS:                    request.FPS,
		ApprovedBoardVersionID: strings.TrimSpace(request.ApprovedBoardVersionID),
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return JobDTO{}, bindingInvalidInput()
	}
	record, _, err := service.Submit(ctx, appjobs.SubmitRequest{
		ProjectID:  projectID,
		EntityType: "episode",
		EntityID:   episodeID,
		JobType:    job.JobTypeExport,
		InputJSON:  string(encoded),
		Scope:      "submit-export-job",
	})
	if err != nil {
		return JobDTO{}, toAppError(err)
	}
	return toJobDTO(record), nil
}

// SubmitMigrationJobRequest names the managed-store snapshot a migration
// imports.
type SubmitMigrationJobRequest struct {
	ProjectID string `json:"projectId"`
	// SnapshotHash is the MANAGED STORE hash of the uploaded snapshot — the
	// result of the upload path, never a user-supplied path.
	SnapshotHash string `json:"snapshotHash"`
	// Fingerprint is the snapshot's identity for the idempotent precheck.
	Fingerprint string `json:"fingerprint,omitempty"`
	// ImportMode is the legacy importer's vocabulary (initial/copy). Empty is
	// initial.
	ImportMode string `json:"importMode,omitempty"`
}

// SubmitMigrationJob queues one legacy snapshot import.
func (b *JobsBinding) SubmitMigrationJob(request SubmitMigrationJobRequest) (JobDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return JobDTO{}, err
	}
	projectID := strings.TrimSpace(request.ProjectID)
	snapshotHash := strings.TrimSpace(request.SnapshotHash)
	if projectID == "" || snapshotHash == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	if mode := strings.TrimSpace(request.ImportMode); mode != "" && mode != "initial" && mode != "copy" {
		return JobDTO{}, bindingInvalidInput()
	}
	input := infrajobs.MigrationInput{
		ProjectID:    projectID,
		Version:      infrajobs.LocalInputVersion,
		SnapshotHash: snapshotHash,
		Fingerprint:  strings.TrimSpace(request.Fingerprint),
		ImportMode:   strings.TrimSpace(request.ImportMode),
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return JobDTO{}, bindingInvalidInput()
	}
	record, _, err := service.Submit(ctx, appjobs.SubmitRequest{
		ProjectID:  projectID,
		EntityType: "legacy_snapshot",
		EntityID:   snapshotHash,
		JobType:    job.JobTypeMigration,
		InputJSON:  string(encoded),
		Scope:      "submit-migration-job",
	})
	if err != nil {
		return JobDTO{}, toAppError(err)
	}
	return toJobDTO(record), nil
}
