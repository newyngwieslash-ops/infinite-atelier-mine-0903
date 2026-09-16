package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// FileRepository persists file object metadata through parameterized SQL.
type FileRepository struct {
	db *sql.DB
}

// NewFileRepository stores metadata using the ready database handle.
func NewFileRepository(db *sql.DB) *FileRepository {
	return &FileRepository{db: db}
}

// UpsertObject inserts file metadata or rejects hash/storage/size conflicts.
func (r *FileRepository) UpsertObject(ctx context.Context, object files.Object) error {
	if r == nil || r.db == nil {
		return apperror.New("FILE_WRITE_FAILED", "storage", false, "The file could not be stored.", errors.New("database unavailable"))
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(hash) DO NOTHING`,
		object.Hash, object.StorageKey, object.MIME, object.Size, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return apperror.New("FILE_WRITE_FAILED", "storage", false, "The file could not be stored.", err)
	}
	var stored files.Object
	err = r.db.QueryRowContext(ctx, `SELECT hash, storage_key, mime_type, size_bytes FROM file_objects WHERE hash = ?`, object.Hash).Scan(
		&stored.Hash, &stored.StorageKey, &stored.MIME, &stored.Size)
	if err != nil {
		return apperror.New("FILE_WRITE_FAILED", "storage", false, "The file could not be stored.", err)
	}
	if stored.StorageKey != object.StorageKey || stored.Size != object.Size || stored.Hash != object.Hash {
		return apperror.New("FILE_CONFLICT", "storage", false, "The stored file metadata does not match the file contents.", errors.New("hash storage or size conflict"))
	}
	return nil
}
