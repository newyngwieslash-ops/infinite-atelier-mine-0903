// Package database owns SQLite access for the desktop infrastructure.
package database

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	_ "modernc.org/sqlite"
)

// openDriver accepts only an application-owned path; it is not a UI binding.
func openDriver(ctx context.Context, path string) (*sql.DB, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, driverOpenError(err)
	}
	uriPath := filepath.ToSlash(absPath)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath // Windows drive letters belong in the URI path.
	}
	dsn := url.URL{Scheme: "file", Path: uriPath, RawQuery: url.Values{
		"_pragma": {"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)"},
	}.Encode()}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, driverOpenError(err)
	}
	// ponytail: one connection serializes local access; expand only with measured
	// contention and transaction/concurrency evidence for the larger pool.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		return nil, driverOpenError(errors.Join(err, db.Close()))
	}
	return db, nil
}

func driverOpenError(cause error) *apperror.Error {
	return apperror.New("DATABASE_OPEN_FAILED", "storage", false,
		"The local database could not be opened.", cause)
}
