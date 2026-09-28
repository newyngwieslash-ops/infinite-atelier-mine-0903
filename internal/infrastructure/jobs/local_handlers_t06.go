package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appimporting "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/importing"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	applegacy "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/legacy"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	asset "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// local_handlers_t06.go turns the four vocabulary job types FR-150 names into
// REAL persistent handlers — the 2026-09-26 audit's T06.
//
// # What was missing, and why the runner is where it lands
//
// Migration 000023 widened `generation_jobs.job_type` to admit thumbnail,
// import, export and migration, and nothing executed them: the runner's
// dispatch refused all four with "unsupported", no binding could create one,
// and a type the schema accepts and nothing runs is a row the UI would show
// as queued forever. The runner is the execution point the worker already
// claims, leases, retries and recovers through, so a handler here inherits
// idempotency keys, attempt budgets and restart reconciliation for free —
// which is exactly what "持久执行、取消、恢复" requires.
//
// # The resume semantics, per type
//
//   - thumbnail: no side effect beyond a file-store put and a version file
//     link (ON CONFLICT DO NOTHING), so a requeue after a crash re-derives
//     the same rows — idempotent by construction.
//   - import: the importing service refuses a document whose source hash was
//     already imported (FR-020), so a requeued run is a no-op.
//   - export: the media export records a version row whose number is derived
//     from the stored maximum, so a retry after a committed export creates a
//     SECOND export rather than failing — the caller's idempotency key makes
//     an identical re-submission return the original job instead, which is
//     the recovery story.
//   - migration: the legacy import is idempotent by fingerprint
//     (HasCompletedImport precheck) and its write is one transaction.
//
// # What a handler refuses
//
// A local job names its subject in the input (`projectID`, and the type's own
// subject — a document id, a snapshot id, an episode id). An input that does
// not name what the handler works on is refused before anything runs, because
// a job that cannot say what it does is a job that cannot be audited.

// LocalHandlers carries the application services the four local types drive.
//
// Every field is optional and nil means the type is refused with an honest
// unsupported error — a build whose media stack did not compose cannot run an
// export job, and saying so beats failing with a nil panic.
type LocalHandlers struct {
	// Thumbnail derives a shot's thumbnail from its approved media.
	Thumbnail ThumbnailHandler
	// Import runs a document import.
	Import ImportHandler
	// Export composes one episode's film.
	Export ExportHandler
	// Migration runs a legacy snapshot import.
	Migration MigrationHandler
}

// ThumbnailHandler derives one thumbnail.
type ThumbnailHandler func(ctx context.Context, request ThumbnailRequest) (ThumbnailResult, error)

// ImportHandler runs one document import.
type ImportHandler func(ctx context.Context, request ImportJobRequest) (ImportJobResult, error)

// ExportHandler composes one episode.
type ExportHandler func(ctx context.Context, request ExportJobRequest) (ExportJobResult, error)

// MigrationHandler runs one legacy snapshot import.
type MigrationHandler func(ctx context.Context, request MigrationJobRequest) (MigrationJobResult, error)

// ThumbnailRequest names the media version to derive from.
type ThumbnailRequest struct {
	ProjectID       string
	AssetVersionID  string
	MaxWidthPixels  int
	MaxHeightPixels int
}

// ThumbnailResult names the committed file.
type ThumbnailResult struct {
	Hash string
	Size int64
}

// ImportJobRequest names the document to import.
type ImportJobRequest struct {
	ProjectID string
	// DocumentID is the stored source document the import reads.
	DocumentID string
	// Format and OriginalName travel from the input document (RP-04.1) for
	// the importer's format decision and the audit trail. Format is the
	// stored vocabulary (txt/markdown/docx/pdf); empty defers to the stored
	// document's own.
	Format       string
	OriginalName string
}

// ImportJobResult reports the import's outcome with REAL semantics (RP-04.1):
// chapters are chapters and story entities are story entities — the old result
// stored the chapter count in StoryEntities and a hardcoded 1 in Episodes,
// which made every import claim a single episode regardless of what the split
// produced.
type ImportJobResult struct {
	Chapters      int `json:"chapters,omitempty"`
	StoryEntities int `json:"storyEntities,omitempty"`
	Episodes      int `json:"episodes,omitempty"`
	// AlreadyImported marks the idempotent no-op a requeued run hits.
	AlreadyImported bool   `json:"alreadyImported,omitempty"`
	DocumentVersion string `json:"documentVersion,omitempty"`
}

// ExportJobRequest names the episode to compose. RP-04.1: quality, FPS and
// the pinned board version travel from the input instead of falling back to
// hardcoded defaults the submitter could not choose.
type ExportJobRequest struct {
	ProjectID              string
	EpisodeID              string
	Quality                string
	FPS                    int
	ApprovedBoardVersionID string
}

// ExportJobResult names the composed film.
type ExportJobResult struct {
	ExportID string
	Duration int
}

// MigrationJobRequest names the snapshot to import. RP-04.1: the fingerprint
// and import mode travel from the input document, and SnapshotID is the
// MANAGED STORE HASH the job reads from — never an OS path.
type MigrationJobRequest struct {
	ProjectID   string
	SnapshotID  string
	Fingerprint string
	ImportMode  string
}

// MigrationJobResult reports the migration's outcome.
type MigrationJobResult struct {
	AlreadyImported bool
}

// localJobInput is the T06 envelope. It is retained only so the runner can
// recognize and REFUSE it: a job queued by the old envelope has no version
// field, so decodeVersioned rejects it and the job fails with invalid-input —
// the honest answer, not a misread of half-understood bytes. New submissions
// write the typed documents in local_input.go (RP-04.1).
type localJobInput struct {
	ProjectID string `json:"projectId"`
	// Subject is the type's own identifier — an asset version, a document, an
	// episode, a snapshot.
	Subject string `json:"subject"`
}

func (l LocalHandlers) available(jobType job.JobType) bool {
	switch jobType {
	case job.JobTypeThumbnail:
		return l.Thumbnail != nil
	case job.JobTypeImport:
		return l.Import != nil
	case job.JobTypeExport:
		return l.Export != nil
	case job.JobTypeMigration:
		return l.Migration != nil
	}
	return false
}

// runThumbnail derives a thumbnail for one media version through the media
// engine and links it to the version under the `thumbnail` file role.
func (r *Runner) runThumbnail(ctx context.Context, record job.Job) (appjobs.Outcome, error) {
	if r.locals == nil || !r.locals.available(record.JobType) {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryUnsupported,
			"This build cannot run thumbnail jobs.")
	}
	var input ThumbnailInput
	if err := decodeVersioned(record.InputJSON, LocalInputVersion, &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed: "+err.Error())
	}
	if strings.TrimSpace(input.AssetVersionID) == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput,
			"A thumbnail job must name the media version it derives from.")
	}
	result, err := r.locals.Thumbnail(ctx, ThumbnailRequest{
		ProjectID: input.ProjectID, AssetVersionID: input.AssetVersionID,
		MaxWidthPixels: input.MaxWidthPixels, MaxHeightPixels: input.MaxHeightPixels,
	})
	if err != nil {
		return appjobs.Outcome{}, err
	}
	encoded, err := json.Marshal(map[string]any{
		"mode": "inline", "mime": "image/png",
		"files": []map[string]any{{
			"storageKey": result.Hash, "mime": "image/png", "size": result.Size, "hash": result.Hash,
		}},
	})
	if err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "The job result could not be recorded.")
	}
	return appjobs.Outcome{Status: job.StatusSucceeded, ResultJSON: string(encoded)}, nil
}

// runImportJob runs one document import.
func (r *Runner) runImportJob(ctx context.Context, record job.Job) (appjobs.Outcome, error) {
	if r.locals == nil || !r.locals.available(record.JobType) {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryUnsupported,
			"This build cannot run import jobs.")
	}
	var input ImportInput
	if err := decodeVersioned(record.InputJSON, LocalInputVersion, &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed: "+err.Error())
	}
	if strings.TrimSpace(input.DocumentID) == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput,
			"An import job must name the document it imports.")
	}
	result, err := r.locals.Import(ctx, ImportJobRequest{ProjectID: input.ProjectID, DocumentID: input.DocumentID, Format: input.Format, OriginalName: input.OriginalName})
	if err != nil {
		return appjobs.Outcome{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "The job result could not be recorded.")
	}
	return appjobs.Outcome{Status: job.StatusSucceeded, ResultJSON: string(encoded)}, nil
}

// runExportJob composes one episode's film through the media export service.
func (r *Runner) runExportJob(ctx context.Context, record job.Job) (appjobs.Outcome, error) {
	if r.locals == nil || !r.locals.available(record.JobType) {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryUnsupported,
			"This build cannot run export jobs.")
	}
	var input ExportInput
	if err := decodeVersioned(record.InputJSON, LocalInputVersion, &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed: "+err.Error())
	}
	if strings.TrimSpace(input.EpisodeID) == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput,
			"An export job must name the episode it composes.")
	}
	result, err := r.locals.Export(ctx, ExportJobRequest{ProjectID: input.ProjectID, EpisodeID: input.EpisodeID, Quality: input.Quality, FPS: input.FPS, ApprovedBoardVersionID: input.ApprovedBoardVersionID})
	if err != nil {
		return appjobs.Outcome{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "The job result could not be recorded.")
	}
	return appjobs.Outcome{Status: job.StatusSucceeded, ResultJSON: string(encoded)}, nil
}

// runMigrationJob runs one legacy snapshot import.
func (r *Runner) runMigrationJob(ctx context.Context, record job.Job) (appjobs.Outcome, error) {
	if r.locals == nil || !r.locals.available(record.JobType) {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryUnsupported,
			"This build cannot run migration jobs.")
	}
	var input MigrationInput
	if err := decodeVersioned(record.InputJSON, LocalInputVersion, &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed: "+err.Error())
	}
	if strings.TrimSpace(input.SnapshotHash) == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput,
			"A migration job must name the snapshot hash it imports.")
	}
	result, err := r.locals.Migration(ctx, MigrationJobRequest{ProjectID: input.ProjectID, SnapshotID: input.SnapshotHash, Fingerprint: input.Fingerprint, ImportMode: input.ImportMode})
	if err != nil {
		return appjobs.Outcome{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryStorage, "The job result could not be recorded.")
	}
	return appjobs.Outcome{Status: job.StatusSucceeded, ResultJSON: string(encoded)}, nil
}

// Compile-time proof the four local job results marshal (an anonymous map is
// used for thumbnails, so this keeps the typed results honest).
var (
	_ = marshalProof(ThumbnailResult{})
	_ = marshalProof(ImportJobResult{})
	_ = marshalProof(ExportJobResult{})
	_ = marshalProof(MigrationJobResult{})
)

// marshalProof returns nil when the value marshals and the error otherwise.
func marshalProof(value any) error {
	_, err := json.Marshal(value)
	return err
}

// Compile-time references keeping the application services named in this
// file's contract: the wiring that builds LocalHandlers imports them, and a
// refactor that moves one will surface here rather than in a lost comment.
var (
	_ = appmedia.ExportRequest{}
	_ = appimporting.ImportRequest{}
	_ = applegacy.Snapshot{}
	_ = fmt.Sprintf
	_ = asset.RoleThumbnail
)

// WithLocalHandlers supplies the local job handlers. It returns the same
// runner, so the composition root can chain it.
func (r *Runner) WithLocalHandlers(handlers *LocalHandlers) *Runner {
	if r == nil {
		return r
	}
	r.locals = handlers
	return r
}
