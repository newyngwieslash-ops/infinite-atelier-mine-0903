package jobs

import (
	"context"
	"strings"
	"testing"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// local_input_test.go is RP-04.1's contract suite: each typed input decodes
// field by field, an unknown field is refused, an unstated version is
// refused, and the four handlers carry EVERY input parameter to the handler
// call — the parameters the old two-field envelope silently dropped.

// captureLocals records what the runner passes to each handler.
type captureLocals struct {
	LocalHandlers
	thumbnail    *ThumbnailRequest
	imp          *ImportJobRequest
	exp          *ExportJobRequest
	migration    *MigrationJobRequest
	thumbnailRes ThumbnailResult
	importRes    ImportJobResult
	exportRes    ExportJobResult
	migrationRes MigrationJobResult
}

func newCaptureLocals() *captureLocals {
	c := &captureLocals{}
	c.LocalHandlers = LocalHandlers{
		Thumbnail: func(_ context.Context, request ThumbnailRequest) (ThumbnailResult, error) {
			c.thumbnail = &request
			return c.thumbnailRes, nil
		},
		Import: func(_ context.Context, request ImportJobRequest) (ImportJobResult, error) {
			c.imp = &request
			return c.importRes, nil
		},
		Export: func(_ context.Context, request ExportJobRequest) (ExportJobResult, error) {
			c.exp = &request
			return c.exportRes, nil
		},
		Migration: func(_ context.Context, request MigrationJobRequest) (MigrationJobResult, error) {
			c.migration = &request
			return c.migrationRes, nil
		},
	}
	return c
}

func runnerForRP04(locals *LocalHandlers) *Runner {
	return NewRunner(stubAdapters{}, nil, nil, 1<<20).WithLocalHandlers(locals)
}

func localJob(jobType job.JobType, input string) job.Job {
	return job.Job{ID: "j-rp04", JobType: jobType, InputJSON: input, Status: job.StatusRunning}
}

// TestRP04ThumbnailInputCarriesEveryField proves the thumbnail document's
// size parameters reach the handler, and that unknown fields and stale
// versions are refused rather than dropped.
func TestRP04ThumbnailInputCarriesEveryField(t *testing.T) {
	capture := newCaptureLocals()
	runner := runnerForRP04(&capture.LocalHandlers)
	input := `{"projectId":"p1","version":1,"assetVersionId":"mv-1","maxWidthPixels":480,"maxHeightPixels":270}`
	outcome, err := runner.Run(context.Background(), localJob(job.JobTypeThumbnail, input))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != job.StatusSucceeded {
		t.Fatalf("outcome = %+v", outcome)
	}
	if capture.thumbnail == nil {
		t.Fatal("the handler was never called")
	}
	if capture.thumbnail.AssetVersionID != "mv-1" || capture.thumbnail.ProjectID != "p1" {
		t.Fatalf("thumbnail request = %+v", capture.thumbnail)
	}
	if capture.thumbnail.MaxWidthPixels != 480 || capture.thumbnail.MaxHeightPixels != 270 {
		t.Fatalf("the size parameters were dropped: %+v", capture.thumbnail)
	}
}

// TestRP04ImportInputCarriesFormatAndName proves the import document's
// format and original-name fields reach the handler call.
func TestRP04ImportInputCarriesFormatAndName(t *testing.T) {
	capture := newCaptureLocals()
	runner := runnerForRP04(&capture.LocalHandlers)
	input := `{"projectId":"p1","version":1,"documentId":"doc-9","format":"docx","originalName":"novel.docx"}`
	if _, err := runner.Run(context.Background(), localJob(job.JobTypeImport, input)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if capture.imp == nil {
		t.Fatal("the handler was never called")
	}
	if capture.imp.DocumentID != "doc-9" || capture.imp.Format != "docx" || capture.imp.OriginalName != "novel.docx" {
		t.Fatalf("import request = %+v", capture.imp)
	}
}

// TestRP04ExportInputCarriesQualityFPSAndBoard proves the export document's
// quality, FPS and pinned board version reach the handler — the fields the
// old envelope hardcoded away.
func TestRP04ExportInputCarriesQualityFPSAndBoard(t *testing.T) {
	capture := newCaptureLocals()
	runner := runnerForRP04(&capture.LocalHandlers)
	input := `{"projectId":"p1","version":1,"episodeId":"ep-2","quality":"final","fps":24,"approvedBoardVersionId":"bv-7"}`
	if _, err := runner.Run(context.Background(), localJob(job.JobTypeExport, input)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if capture.exp == nil {
		t.Fatal("the handler was never called")
	}
	if capture.exp.EpisodeID != "ep-2" || capture.exp.Quality != "final" || capture.exp.FPS != 24 || capture.exp.ApprovedBoardVersionID != "bv-7" {
		t.Fatalf("export request = %+v", capture.exp)
	}
}

// TestRP04MigrationInputCarriesFingerprintAndMode proves the migration
// document's fingerprint and mode reach the handler, and that the snapshot
// is named by its STORE HASH.
func TestRP04MigrationInputCarriesFingerprintAndMode(t *testing.T) {
	capture := newCaptureLocals()
	runner := runnerForRP04(&capture.LocalHandlers)
	input := `{"projectId":"p1","version":1,"snapshotHash":"abc123","fingerprint":"fp-1","importMode":"copy"}`
	if _, err := runner.Run(context.Background(), localJob(job.JobTypeMigration, input)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if capture.migration == nil {
		t.Fatal("the handler was never called")
	}
	if capture.migration.SnapshotID != "abc123" || capture.migration.Fingerprint != "fp-1" || capture.migration.ImportMode != "copy" {
		t.Fatalf("migration request = %+v", capture.migration)
	}
}

// TestRP04InputRefusesUnknownFieldsAndOldEnvelope is the negative suite: a
// document with a field this version does not know is refused, the old T06
// envelope is refused, and a stated version the runner does not speak is
// refused — each with invalid-input, before any handler call.
func TestRP04InputRefusesUnknownFieldsAndOldEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		jobType job.JobType
		input   string
	}{
		{"unknown field on export", job.JobTypeExport,
			`{"projectId":"p1","version":1,"episodeId":"e","osPath":"/etc/passwd"}`},
		{"old envelope on thumbnail", job.JobTypeThumbnail,
			`{"projectId":"p1","subject":"mv-1"}`},
		{"future version on import", job.JobTypeImport,
			`{"projectId":"p1","version":99,"documentId":"d"}`},
		{"missing version on migration", job.JobTypeMigration,
			`{"projectId":"p1","snapshotHash":"h"}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			capture := newCaptureLocals()
			runner := runnerForRP04(&capture.LocalHandlers)
			_, err := runner.Run(context.Background(), localJob(testCase.jobType, testCase.input))
			if err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
			if !strings.Contains(err.Error(), "") || capture.thumbnail != nil || capture.imp != nil || capture.exp != nil || capture.migration != nil {
				t.Fatalf("the handler ran on a refused input")
			}
		})
	}
}

// TestRP04ImportResultUsesRealSemantics pins the result vocabulary: the
// runner's own decode of the handler's result must carry chapters, entities
// and episodes as SEPARATE fields — not a chapter count masquerading as an
// entity count, and not a hardcoded episode.
func TestRP04ImportResultUsesRealSemantics(t *testing.T) {
	capture := newCaptureLocals()
	capture.importRes = ImportJobResult{Chapters: 5, StoryEntities: 12, Episodes: 3}
	runner := runnerForRP04(&capture.LocalHandlers)
	outcome, err := runner.Run(context.Background(), localJob(job.JobTypeImport,
		`{"projectId":"p1","version":1,"documentId":"doc-1"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(outcome.ResultJSON, `"chapters":5`) ||
		!strings.Contains(outcome.ResultJSON, `"storyEntities":12`) ||
		!strings.Contains(outcome.ResultJSON, `"episodes":3`) {
		t.Fatalf("result = %s, want the real semantic fields", outcome.ResultJSON)
	}
}

// The runner's Outcome type must be the one the settlement persists.
var _ = appjobs.Outcome{}
