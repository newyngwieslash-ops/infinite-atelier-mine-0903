package database

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	infrajobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/jobs"
)

// TestResultPipelineWritesMetadataAndReference is the regression test for the
// defect where the job pipeline wrote an owner reference without recording the
// object metadata first. `file_references.file_hash` has a foreign key to
// `file_objects(hash)`, so every result commit failed against a real database
// and no job could ever succeed with a stored file.
//
// This test wires the production constructors together — real FileStore, real
// FileRepository, real FileReferenceRepository — instead of calling each in
// isolation, which is exactly the gap the unit tests left.
func TestResultPipelineWritesMetadataAndReference(t *testing.T) {
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp03Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if handle.Mode() != ModeReady {
		t.Fatalf("database not ready: %v", handle.Err())
	}

	filesRoot := t.TempDir()
	store, err := filestore.New(filepath.Join(filesRoot, "files"), filepath.Join(filesRoot, "temp"))
	if err != nil {
		t.Fatal(err)
	}
	fileRepository := NewFileRepository(handle.SQL())
	referenceRepository := NewFileReferenceRepository(handle.SQL())
	pipeline := infrajobs.NewResultStore(store, fileRepository, referenceRepository)
	ctx := context.Background()

	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("payload")...)
	committed, err := pipeline.CommitResult(ctx, "job-1", "image", "image/png", bytes.NewReader(png))
	if err != nil {
		t.Fatalf("CommitResult against the real repositories failed: %v", err)
	}
	if committed.Hash == "" || committed.StorageKey == "" || committed.Size != int64(len(png)) {
		t.Fatalf("unexpected commit: %+v", committed)
	}

	// The object metadata row must exist, otherwise the FK-constrained
	// reference could not have been written.
	var mimeType string
	var size int64
	if err := handle.SQL().QueryRowContext(ctx, `SELECT mime_type, size_bytes FROM file_objects WHERE hash = ?`, committed.Hash).
		Scan(&mimeType, &size); err != nil {
		t.Fatalf("file_objects row missing: %v", err)
	}
	if size != int64(len(png)) {
		t.Fatalf("size_bytes = %d, want %d", size, len(png))
	}

	// The owning reference must exist and point at the job.
	references, err := referenceRepository.ListReferences(ctx, committed.Hash)
	if err != nil {
		t.Fatalf("ListReferences: %v", err)
	}
	if len(references) != 1 || references[0].OwnerType != "job" || references[0].OwnerID != "job-1" {
		t.Fatalf("reference not recorded for the job: %+v", references)
	}
}

// TestResultPipelineRejectsBadContentWithoutReferencing proves a rejected
// content type never becomes a reference, even though the bytes may already be
// content-addressed.
func TestResultPipelineRejectsBadContentWithoutReferencing(t *testing.T) {
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp03Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })

	filesRoot := t.TempDir()
	store, err := filestore.New(filepath.Join(filesRoot, "files"), filepath.Join(filesRoot, "temp"))
	if err != nil {
		t.Fatal(err)
	}
	referenceRepository := NewFileReferenceRepository(handle.SQL())
	pipeline := infrajobs.NewResultStore(store, NewFileRepository(handle.SQL()), referenceRepository)
	ctx := context.Background()

	html := []byte("<html><script>alert(1)</script></html>")
	if _, err := pipeline.CommitResult(ctx, "job-2", "image", "image/png", bytes.NewReader(html)); err == nil {
		t.Fatal("HTML payload accepted as an image")
	}
	var referenceCount int
	if err := handle.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM file_references WHERE owner_id = 'job-2'`).Scan(&referenceCount); err != nil {
		t.Fatal(err)
	}
	if referenceCount != 0 {
		t.Fatalf("rejected content left %d reference(s)", referenceCount)
	}
}

// TestResultPipelineRejectsMetadataFailure proves a missing metadata store is a
// hard failure rather than a silently unreferenced result.
func TestResultPipelineRejectsMetadataFailure(t *testing.T) {
	filesRoot := t.TempDir()
	store, err := filestore.New(filepath.Join(filesRoot, "files"), filepath.Join(filesRoot, "temp"))
	if err != nil {
		t.Fatal(err)
	}
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("payload")...)

	// No metadata store at all.
	withoutMetadata := infrajobs.NewResultStore(store, nil, &recordingReferences{})
	if _, err := withoutMetadata.CommitResult(context.Background(), "job-3", "image", "image/png", bytes.NewReader(png)); err == nil {
		t.Fatal("commit without a metadata store succeeded")
	}
	// Metadata store present but failing.
	failing := infrajobs.NewResultStore(store, failingMetadata{}, &recordingReferences{})
	if _, err := failing.CommitResult(context.Background(), "job-3", "image", "image/png", bytes.NewReader(png)); err == nil {
		t.Fatal("commit with a failing metadata store succeeded")
	}
}

type recordingReferences struct{}

func (recordingReferences) AddReference(context.Context, string, string, string) error { return nil }

// errFailingMetadata stands in for a database write failure.
var errFailingMetadata = errors.New("metadata write failed")

type failingMetadata struct{}

func (failingMetadata) UpsertObject(context.Context, files.Object) error { return errFailingMetadata }
