package database

import (
	"context"
	"database/sql"
	"time"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// export.go is the storage of the episode export record (migration 000020).
//
// It holds the MANIFEST rather than being the manifest: the document is assembled by the export
// service out of what the timeline read, and this table is where it lands so that a frame can be
// traced a week later. The distinction matters because it is what makes the traceability claim
// checkable — the reader decodes the document and compares it against what is currently approved,
// and neither half lives here.

// ExportRepository stores episode export records.
type ExportRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewExportRepository builds a repository over a connection.
func NewExportRepository(db *sql.DB) *ExportRepository {
	return &ExportRepository{db: db}
}

// WithinTx returns a repository bound to a transaction.
func (r *ExportRepository) WithinTx(tx *sql.Tx) *ExportRepository {
	return &ExportRepository{db: r.db, tx: tx}
}

func (r *ExportRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

// withinTx runs fn inside a transaction of this repository's database.
func (r *ExportRepository) withinTx(ctx context.Context, fn func(repo *ExportRepository) error) error {
	if r == nil || r.db == nil {
		return media.StorageError("The export store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return media.StorageError("The export could not be saved.", err)
	}
	if err := fn(r.WithinTx(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return media.StorageError("The export could not be saved.", err)
	}
	return nil
}

const exportSelectColumns = `SELECT id, episode_id, version_number, status, quality, width, height,
	duration_ms, output_file_hash, subtitle_track_id, manifest_json, approval_trace_id,
	source_agent_run_id, created_by_type, created_by_id, change_reason, created_at FROM episode_exports`

// CreateExport writes one record.
func (r *ExportRepository) CreateExport(ctx context.Context, record appmedia.ExportRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	conn := r.conn()
	if conn == nil {
		return media.StorageError("The export store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO episode_exports
		(id, episode_id, version_number, status, quality, width, height, duration_ms,
		 output_file_hash, subtitle_track_id, manifest_json, approval_trace_id,
		 source_agent_run_id, created_by_type, created_by_id, change_reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.EpisodeID, record.VersionNumber, record.Status, record.Quality,
		record.Width, record.Height, record.DurationMS, record.OutputFileHash,
		record.SubtitleTrackID, record.ManifestJSON, record.ApprovalTraceID,
		record.SourceAgentRunID, record.CreatedByType, record.CreatedByID, record.ChangeReason,
		formatTime(record.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return media.ConflictError("That export version already exists for this episode.")
		}
		if isForeignKeyViolation(err) {
			return media.InvalidError("An export must name an existing episode.")
		}
		return media.StorageError("The export could not be saved.", err)
	}
	return nil
}

// GetExport returns one record.
func (r *ExportRepository) GetExport(ctx context.Context, id string) (appmedia.ExportRecord, error) {
	conn := r.conn()
	if conn == nil {
		return appmedia.ExportRecord{}, media.StorageError("The export store is unavailable.", nil)
	}
	record, err := scanExport(conn.QueryRowContext(ctx, exportSelectColumns+` WHERE id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return appmedia.ExportRecord{}, media.NotFoundError()
		}
		return appmedia.ExportRecord{}, media.StorageError("The export could not be read.", err)
	}
	return record, nil
}

// ListExports returns an episode's exports newest version first.
func (r *ExportRepository) ListExports(ctx context.Context, episodeID string) ([]appmedia.ExportRecord, error) {
	conn := r.conn()
	if conn == nil {
		return nil, media.StorageError("The export store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, exportSelectColumns+
		` WHERE episode_id = ? ORDER BY version_number DESC`, episodeID)
	if err != nil {
		return nil, media.StorageError("The exports could not be read.", err)
	}
	defer rows.Close()
	exports := []appmedia.ExportRecord{}
	for rows.Next() {
		record, err := scanExport(rows)
		if err != nil {
			return nil, media.StorageError("The exports could not be read.", err)
		}
		exports = append(exports, record)
	}
	if err := rows.Err(); err != nil {
		return nil, media.StorageError("The exports could not be read.", err)
	}
	return exports, nil
}

// MaxExportVersionNumber reports the highest version number an episode's exports reach.
func (r *ExportRepository) MaxExportVersionNumber(ctx context.Context, episodeID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, media.StorageError("The export store is unavailable.", nil)
	}
	var highest sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT MAX(version_number) FROM episode_exports WHERE episode_id = ?`, episodeID).Scan(&highest); err != nil {
		return 0, media.StorageError("The export version could not be read.", err)
	}
	if !highest.Valid {
		return 0, nil
	}
	return int(highest.Int64), nil
}

// CurrentApprovedExport returns the episode's approved export, or found=false.
func (r *ExportRepository) CurrentApprovedExport(ctx context.Context, episodeID string) (appmedia.ExportRecord, bool, error) {
	conn := r.conn()
	if conn == nil {
		return appmedia.ExportRecord{}, false, media.StorageError("The export store is unavailable.", nil)
	}
	record, err := scanExport(conn.QueryRowContext(ctx, exportSelectColumns+
		` WHERE episode_id = ? AND status = 'approved'`, episodeID))
	if err != nil {
		if err == sql.ErrNoRows {
			return appmedia.ExportRecord{}, false, nil
		}
		return appmedia.ExportRecord{}, false, media.StorageError("The export could not be read.", err)
	}
	return record, true, nil
}

// ApproveExport switches which export is approved, recording the decision's trace.
//
// The replaced export is superseded FIRST so the partial unique index never sees two approved rows,
// which is the order the gap report, the subtitle track and the four version families all use.
func (r *ExportRepository) ApproveExport(ctx context.Context, exportID, episodeID, traceID string, at time.Time) error {
	return r.withinTx(ctx, func(repo *ExportRepository) error {
		conn := repo.conn()
		if conn == nil {
			return media.StorageError("The export store is unavailable.", nil)
		}
		if _, err := conn.ExecContext(ctx, `UPDATE episode_exports SET status = 'superseded'
			WHERE episode_id = ? AND status = 'approved'`, episodeID); err != nil {
			return media.StorageError("The previous export could not be superseded.", err)
		}
		result, err := conn.ExecContext(ctx, `UPDATE episode_exports
			SET status = 'approved', approval_trace_id = ? WHERE id = ? AND status = 'under_review'`,
			traceID, exportID)
		if err != nil {
			return media.StorageError("The export could not be approved.", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return media.StorageError("The export could not be approved.", err)
		}
		if affected == 0 {
			// Nothing moved, so the caller's copy of the status was stale — or the export is not under
			// review, which is the state an approval requires.
			return media.ConflictError("That export is not under review, so it cannot be approved.")
		}
		return nil
	})
}

// MarkUnderReview moves a draft export to review, which is the state an approval requires.
func (r *ExportRepository) MarkUnderReview(ctx context.Context, exportID string) error {
	conn := r.conn()
	if conn == nil {
		return media.StorageError("The export store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx,
		`UPDATE episode_exports SET status = 'under_review' WHERE id = ? AND status = 'draft'`, exportID)
	if err != nil {
		return media.StorageError("The export could not be moved to review.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return media.StorageError("The export could not be moved to review.", err)
	}
	if affected == 0 {
		return media.ConflictError("That export is not a draft, so it cannot be moved to review.")
	}
	return nil
}

// scanExport reads one export row.
func scanExport(row rowScanner) (appmedia.ExportRecord, error) {
	var record appmedia.ExportRecord
	var createdAt string
	if err := row.Scan(&record.ID, &record.EpisodeID, &record.VersionNumber, &record.Status,
		&record.Quality, &record.Width, &record.Height, &record.DurationMS, &record.OutputFileHash,
		&record.SubtitleTrackID, &record.ManifestJSON, &record.ApprovalTraceID,
		&record.SourceAgentRunID, &record.CreatedByType, &record.CreatedByID, &record.ChangeReason,
		&createdAt); err != nil {
		return appmedia.ExportRecord{}, err
	}
	record.CreatedAt = parseTime(createdAt)
	return record, nil
}

// The compile-time proofs that this repository satisfies both of the export service's ports.
//
// Two assertions rather than one because a repository that satisfied only the record store would
// leave the timeline's reads unsatisfied, and the failure would be a nil port at composition — which
// reads at runtime as "no timeline reader is configured" rather than as a signature drift.
var (
	_ appmedia.ExportRepository   = (*ExportRepository)(nil)
	_ appmedia.TimelineRepository = (*ExportRepository)(nil)
)
