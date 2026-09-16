package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// FileReferenceRepository records which logical owner references a stored file.
// It is the write side of `file_references`, which previously had no writer.
//
// Ownership matters for two reasons: a job's produced asset must be
// discoverable after a restart, and a later garbage collector must be able to
// see that the content is still in use before deleting bytes.
type FileReferenceRepository struct {
	db *sql.DB
}

// NewFileReferenceRepository builds the reference repository.
func NewFileReferenceRepository(db *sql.DB) *FileReferenceRepository {
	return &FileReferenceRepository{db: db}
}

// AddReference records that ownerType/ownerID references the file. Repeating the
// same reference is a no-op so job retries stay idempotent.
func (r *FileReferenceRepository) AddReference(ctx context.Context, ownerType, ownerID, fileHash string) error {
	if r == nil || r.db == nil {
		return apperror.New("FILE_REFERENCE_FAILED", "storage", false, "The file reference could not be recorded.", nil)
	}
	if ownerType == "" || ownerID == "" || fileHash == "" {
		return apperror.New("FILE_REFERENCE_FAILED", "storage", false, "The file reference could not be recorded.", nil)
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO file_references (owner_type, owner_id, file_hash, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(owner_type, owner_id, file_hash) DO NOTHING`,
		ownerType, ownerID, fileHash, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return apperror.New("FILE_REFERENCE_FAILED", "storage", false, "The file reference could not be recorded.", err)
	}
	return nil
}

// ListReferences reports the owners of one file.
func (r *FileReferenceRepository) ListReferences(ctx context.Context, fileHash string) ([]FileReference, error) {
	if r == nil || r.db == nil {
		return nil, apperror.New("FILE_REFERENCE_FAILED", "storage", false, "The file references could not be read.", nil)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT owner_type, owner_id, created_at FROM file_references WHERE file_hash = ?`, fileHash)
	if err != nil {
		return nil, apperror.New("FILE_REFERENCE_FAILED", "storage", false, "The file references could not be read.", err)
	}
	defer rows.Close()
	var references []FileReference
	for rows.Next() {
		var reference FileReference
		var createdAt string
		if err := rows.Scan(&reference.OwnerType, &reference.OwnerID, &createdAt); err != nil {
			return nil, apperror.New("FILE_REFERENCE_FAILED", "storage", false, "The file references could not be read.", err)
		}
		reference.CreatedAt = parseTime(createdAt)
		references = append(references, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.New("FILE_REFERENCE_FAILED", "storage", false, "The file references could not be read.", err)
	}
	return references, nil
}

// RemoveReference drops one ownership edge.
func (r *FileReferenceRepository) RemoveReference(ctx context.Context, ownerType, ownerID, fileHash string) error {
	if r == nil || r.db == nil {
		return apperror.New("FILE_REFERENCE_FAILED", "storage", false, "The file reference could not be removed.", nil)
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM file_references
		WHERE owner_type = ? AND owner_id = ? AND file_hash = ?`, ownerType, ownerID, fileHash); err != nil {
		return apperror.New("FILE_REFERENCE_FAILED", "storage", false, "The file reference could not be removed.", err)
	}
	return nil
}

// FileReference is one ownership edge.
type FileReference struct {
	OwnerType string
	OwnerID   string
	CreatedAt time.Time
}
