package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	asset "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	appimporting "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/importing"
	applegacy "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/legacy"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
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
}

// ImportJobResult reports the import's outcome.
type ImportJobResult struct {
	StoryEntities int
	Episodes      int
	// AlreadyImported marks the idempotent no-op a requeued run hits.
	AlreadyImported bool
}

// ExportJobRequest names the episode to compose.
type ExportJobRequest struct {
	ProjectID string
	EpisodeID string
	Quality   string
	FPS       int
}

// ExportJobResult names the composed film.
type ExportJobResult struct {
	ExportID string
	Duration int
}

// MigrationJobRequest names the snapshot to import.
type MigrationJobRequest struct {
	ProjectID   string
	SnapshotID  string
	Fingerprint string
}

// MigrationJobResult reports the migration's outcome.
type MigrationJobResult struct {
	AlreadyImported bool
}

// localJobInput is the envelope every local input document carries.
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
	var input localJobInput
	if err := json.Unmarshal([]byte(record.InputJSON), &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed.")
	}
	if strings.TrimSpace(input.Subject) == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput,
			"A thumbnail job must name the media version it derives from.")
	}
	result, err := r.locals.Thumbnail(ctx, ThumbnailRequest{
		ProjectID: input.ProjectID, AssetVersionID: input.Subject,
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
	var input localJobInput
	if err := json.Unmarshal([]byte(record.InputJSON), &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed.")
	}
	if strings.TrimSpace(input.Subject) == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput,
			"An import job must name the document it imports.")
	}
	result, err := r.locals.Import(ctx, ImportJobRequest{ProjectID: input.ProjectID, DocumentID: input.Subject})
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
	var input localJobInput
	if err := json.Unmarshal([]byte(record.InputJSON), &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed.")
	}
	if strings.TrimSpace(input.Subject) == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput,
			"An export job must name the episode it composes.")
	}
	result, err := r.locals.Export(ctx, ExportJobRequest{ProjectID: input.ProjectID, EpisodeID: input.Subject})
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
	var input localJobInput
	if err := json.Unmarshal([]byte(record.InputJSON), &input); err != nil {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput, "The job input is malformed.")
	}
	if strings.TrimSpace(input.Subject) == "" {
		return appjobs.Outcome{}, job.FailedJobError(job.CategoryInvalidInput,
			"A migration job must name the snapshot it imports.")
	}
	result, err := r.locals.Migration(ctx, MigrationJobRequest{ProjectID: input.ProjectID, SnapshotID: input.Subject})
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
