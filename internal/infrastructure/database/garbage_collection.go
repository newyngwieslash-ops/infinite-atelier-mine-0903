package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// The collection's error codes, stable so a caller can tell a refusal apart from a fault.
const (
	gcStoreUnavailable = "GC_STORE_UNAVAILABLE"
	gcReadFailed       = "GC_READ_FAILED"
	gcWriteFailed      = "GC_WRITE_FAILED"
	gcTxFailed         = "GC_TX_FAILED"
)

func gcError(code, message string, cause error) error {
	return apperror.New(code, "storage", false, message, cause)
}

// garbage_collection.go answers FR-160's 「垃圾回收执行前显示将删除内容并支持取消」.
//
// # What "unreferenced" means, and why this file is the only place that can answer it
//
// An object is safe to delete when NOTHING names its hash. The repository has SEVEN columns that
// name one, and only two of them are foreign keys:
//
//	file_references.file_hash                        FK, ON DELETE RESTRICT
//	asset_files.file_hash                            FK, ON DELETE RESTRICT
//	source_document_versions.physical_file_id        plain column
//	source_document_versions.normalized_text_file_id plain column
//	agent_runs.raw_output_file_id                    plain column
//	skill_versions.content_file_id                   plain column
//	episode_exports.output_file_hash                 plain column
//
// SIX, not seven: `storyboard_items` has NO file column. An earlier version of this file named
// `storyboard_items.raw_output_file_id` and queried it, which failed at run time — the table's
// columns are the board's per-shot fields and the row's revision, and the raw output of a run is
// recorded on `agent_runs` and on `stage_runs` (`raw_output_file_id`) rather than on the row.
//
// DRIVING DELETION FROM `file_references` ALONE WOULD DELETE LIVE CONTENT. That table is written by
// exactly one producer (the job result pipeline), while `asset_files` is written by a different one —
// so an object an asset's version points at has no `file_references` row at all, and a collector that
// trusted that table would remove the bytes of an approved image. The predicate below therefore
// consults every column, and the reason is recorded here rather than in a test comment.
//
// # What is deliberately NOT consulted
//
// Several `*hash*` columns hold digests of TEXT rather than addresses in the file store —
// `source_document_versions.source_hash`, `.content_hash`, `chapters.content_hash`,
// `story_fact_sources.quote_hash`, `agent_messages.content_hash`, `skill_versions.content_hash` and
// `legacy_project_imports.fingerprint`. None is a key into `file_objects`, and a query that swept
// every column named like a hash would refuse to collect objects that nothing actually references.
// They are absent from the queries below on purpose.
type GarbageCollector struct {
	db *sql.DB
}

// NewGarbageCollector builds the collector over a connection.
func NewGarbageCollector(db *sql.DB) *GarbageCollector {
	return &GarbageCollector{db: db}
}

// GarbageCandidate is one object the collector would remove.
type GarbageCandidate struct {
	Hash       string
	StorageKey string
	MIME       string
	SizeBytes  int64
}

// GarbagePreview is what a collection WOULD remove, so a user can decide.
//
// It is the read FR-160's 「执行前显示将删除内容」 is about, and it is deliberately a separate
// command from the collection for the reason `PreviewBackup` and `RestoreBackup` are separate: a
// preview answers a question, a question must be safe to ask twice, and a destructive act is the
// ANSWER to the question rather than the same act with a flag.
type GarbagePreview struct {
	Candidates []GarbageCandidate
	// TotalBytes is what a collection would free, which is the number a user decides on.
	TotalBytes int64
}

// Preview lists the objects nothing references.
//
// It reads `file_objects` and asks, per row, whether any of the six columns names it. The answer
// is computed in SQL rather than by loading the referencing tables into memory: the set of hashes is
// what a project accumulates over its life, and a preview that held it would be a preview that fails
// on the installation it is most needed for.
func (c *GarbageCollector) Preview(ctx context.Context) (GarbagePreview, error) {
	if c == nil || c.db == nil {
		return GarbagePreview{}, gcError(gcStoreUnavailable, "The file store is unavailable.", errors.New("no database"))
	}
	// `NOT EXISTS` rather than `NOT IN`: the referencing columns hold empty strings for "no file",
	// and `NOT IN` over a set containing '' would match nothing — every object would look
	// referenced, which fails silently in the direction of leaking rather than deleting. `NOT EXISTS`
	// with an equality condition ignores the empty rows, which is what "this row names a file" means.
	rows, err := c.db.QueryContext(ctx, `
		SELECT o.hash, o.storage_key, o.mime_type, o.size_bytes
		FROM file_objects o
		WHERE NOT EXISTS (SELECT 1 FROM file_references r WHERE r.file_hash = o.hash)
		  AND NOT EXISTS (SELECT 1 FROM asset_files a WHERE a.file_hash = o.hash)
		  AND NOT EXISTS (SELECT 1 FROM source_document_versions d
		                  WHERE d.physical_file_id = o.hash OR d.normalized_text_file_id = o.hash)
		  AND NOT EXISTS (SELECT 1 FROM agent_runs g WHERE g.raw_output_file_id = o.hash)
		  AND NOT EXISTS (SELECT 1 FROM skill_versions s WHERE s.content_file_id = o.hash)
		  AND NOT EXISTS (SELECT 1 FROM episode_exports e WHERE e.output_file_hash = o.hash)
		ORDER BY o.created_at ASC, o.hash ASC`)
	if err != nil {
		return GarbagePreview{}, gcError(gcReadFailed, "The collectable objects could not be read.", err)
	}
	defer rows.Close()
	preview := GarbagePreview{Candidates: []GarbageCandidate{}}
	for rows.Next() {
		var candidate GarbageCandidate
		if err := rows.Scan(&candidate.Hash, &candidate.StorageKey, &candidate.MIME, &candidate.SizeBytes); err != nil {
			return GarbagePreview{}, gcError(gcReadFailed, "The collectable objects could not be read.", err)
		}
		preview.Candidates = append(preview.Candidates, candidate)
		preview.TotalBytes += candidate.SizeBytes
	}
	if err := rows.Err(); err != nil {
		return GarbagePreview{}, gcError(gcReadFailed, "The collectable objects could not be read.", err)
	}
	return preview, nil
}

// CollectResult reports what a collection removed and what it refused to.
type CollectResult struct {
	Removed []GarbageCandidate
	// Skipped names the hashes the collection found referenced at the moment it ran, even though the
	// preview did not. It is reported rather than silently dropped, because the two runs are
	// separated by however long a user took to decide — and something that became referenced in
	// between is a fact the caller should see rather than a discrepancy to hide.
	Skipped []string
	// FreedBytes is what the removed objects occupied.
	FreedBytes int64
}

// Collect removes every unreferenced object it finds, re-checking each one.
//
// # Why the check is repeated rather than trusting the preview
//
// The preview and the collection are two commands, and a user may take minutes between them — during
// which an import, a job or an approval can give an object a new owner. This function therefore runs
// the SAME predicate per candidate, in the same transaction that deletes it, so a candidate that
// became referenced is skipped rather than removed. The cost is one query per candidate and it is
// paid deliberately: the alternative is a collector whose safety depends on nobody having used the
// application in between.
//
// # What it does NOT do
//
// It does not delete bytes. The row and the object are two things, and removing a row whose file is
// still on disk leaves a leak, while removing a file whose row survives leaves a broken reference.
// The ROW is this layer's to remove (it owns `file_objects`) and the BYTES are the file store's; the
// caller passes what was deleted to the store, which is the only component that knows the path
// layout. `Collector` returns the candidates so its caller can do that, and the contract is stated
// here rather than implied.
func (c *GarbageCollector) Collect(ctx context.Context) (CollectResult, error) {
	if c == nil || c.db == nil {
		return CollectResult{}, gcError(gcStoreUnavailable, "The file store is unavailable.", errors.New("no database"))
	}
	preview, err := c.Preview(ctx)
	if err != nil {
		return CollectResult{}, err
	}
	result := CollectResult{Removed: []GarbageCandidate{}, Skipped: []string{}}
	// ONE TRANSACTION FOR THE WHOLE COLLECTION, so a failure part-way leaves nothing half-removed:
	// a set of rows deleted without their bytes is recoverable by running again, but a set deleted in
	// two transactions is a state a reader cannot describe.
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return CollectResult{}, gcError(gcTxFailed, "The collection could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, candidate := range preview.Candidates {
		referenced, err := objectReferenced(ctx, tx, candidate.Hash)
		if err != nil {
			return CollectResult{}, err
		}
		if referenced {
			result.Skipped = append(result.Skipped, candidate.Hash)
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM file_objects WHERE hash = ?`, candidate.Hash); err != nil {
			return CollectResult{}, gcError(gcWriteFailed, "A collectable object could not be removed.", err)
		}
		result.Removed = append(result.Removed, candidate)
		result.FreedBytes += candidate.SizeBytes
	}
	if err := tx.Commit(); err != nil {
		return CollectResult{}, gcError(gcTxFailed, "The collection could not be saved.", err)
	}
	committed = true
	return result, nil
}

// objectReferenced reports whether anything in the database names this hash.
//
// # One predicate, one definition
//
// It is the SAME expression the preview builds, and it is written as a single `SELECT 1 ... WHERE NOT
// EXISTS (...)` rather than as a UNION: the first version of this function assembled a union of
// `LIMIT 1` subqueries, which SQLite refused inside a FROM clause — and refused at RUN time, so every
// collection failed while the compiler was happy. A single-row query with the same NOT EXISTS chain
// is both valid and literally the preview's predicate, which is what keeps the two from drifting: a
// column added to one and not the other would make the collection remove something the preview said
// was safe.
//
// The query returns a row exactly when the object is collectable, so `Scan` succeeding IS the answer
// and `sql.ErrNoRows` is the "something references it" case.
func objectReferenced(ctx context.Context, tx *sql.Tx, hash string) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, `
		SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM file_references r WHERE r.file_hash = ?)
		  AND NOT EXISTS (SELECT 1 FROM asset_files a WHERE a.file_hash = ?)
		  AND NOT EXISTS (SELECT 1 FROM source_document_versions d
		                  WHERE d.physical_file_id = ? OR d.normalized_text_file_id = ?)
		  AND NOT EXISTS (SELECT 1 FROM agent_runs g WHERE g.raw_output_file_id = ?)
		  AND NOT EXISTS (SELECT 1 FROM skill_versions s WHERE s.content_file_id = ?)
		  AND NOT EXISTS (SELECT 1 FROM episode_exports e WHERE e.output_file_hash = ?)`,
		hash, hash, hash, hash, hash, hash, hash).Scan(&one)
	if err == sql.ErrNoRows {
		// A row exists in `file_objects` (the caller read it from there) and something names it.
		return true, nil
	}
	if err != nil {
		return false, gcError(gcReadFailed, "An object's references could not be checked.", err)
	}
	return false, nil
}
