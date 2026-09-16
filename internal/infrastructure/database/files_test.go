package database

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

func TestFileRepositoryUpsertAndConflict(t *testing.T) {
	handle, err := Open(context.Background(), filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"))
	if err != nil || handle.SQL() == nil {
		t.Fatalf("open: %v %#v", err, handle)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })
	repo := NewFileRepository(handle.SQL())
	hash := strings.Repeat("a", 64)
	object := files.Object{Hash: hash, StorageKey: hash, MIME: "text/plain", Size: 4}
	if err := repo.UpsertObject(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertObject(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	conflict := object
	conflict.Size = 99
	var appErr *apperror.Error
	if err := repo.UpsertObject(context.Background(), conflict); !errors.As(err, &appErr) || appErr.Code != "FILE_CONFLICT" {
		t.Fatalf("size conflict: %v", err)
	}
	keyConflict := object
	keyConflict.StorageKey = strings.Repeat("b", 64)
	if err := repo.UpsertObject(context.Background(), keyConflict); !errors.As(err, &appErr) || appErr.Code != "FILE_CONFLICT" {
		t.Fatalf("storage key conflict: %v", err)
	}
}

func TestFileReferenceRequiresExistingObject(t *testing.T) {
	handle, err := Open(context.Background(), filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"))
	if err != nil || handle.SQL() == nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })
	_, err = handle.SQL().ExecContext(context.Background(), `INSERT INTO file_references (owner_type, owner_id, file_hash, created_at) VALUES ('asset', '1', ?, '2026-09-07T00:00:00Z')`, strings.Repeat("c", 64))
	if err == nil {
		t.Fatal("missing file object reference was accepted")
	}
}
