package database

import (
	"context"
	"database/sql"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// querier is the subset of *sql.DB and *sql.Tx the repositories use.
//
// Both types satisfy it, which is what lets a repository run either standalone
// or inside a caller's transaction. The legacy import needs the latter: one
// transaction spanning projects, canvas, assets and the import bookkeeping, so
// a failure cannot leave half a project behind (AC-LEGACY-003).
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// connection returns the querier a repository should use. A nil database or
// transaction returns nil so the caller fails closed rather than panicking.
func connection(db *sql.DB, tx *sql.Tx) querier {
	if tx != nil {
		return tx
	}
	if db == nil {
		return nil
	}
	return db
}

// storageError builds the stable error a repository returns when it cannot
// reach its store. It never carries the driver message: that belongs in the
// cause, which is not rendered to the user.
func storageError(code, message string, cause error) error {
	return apperror.New(code, "storage", false, message, cause)
}

// conflict code shared by the project, canvas and asset repositories when a
// compare-and-swap update matches no row.
const codeRevisionConflict = "REVISION_CONFLICT"

// notFound code shared by the read paths of the new repositories.
const codeNotFound = "RECORD_NOT_FOUND"
