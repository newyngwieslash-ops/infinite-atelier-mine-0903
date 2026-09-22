package database

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// Mode is the database availability reported to health and lifecycle code.
type Mode string

const (
	ModeReady Mode = "ready"
	ModeSafe  Mode = "safe"
)

// Handle owns one SQLite connection pool and its close/checkpoint sequence.
type Handle struct {
	mode     Mode
	db       *sql.DB
	err      *apperror.Error
	snapshot string
	// databasePath is the file `open` was given, carried so a restore's atomic swap can
	// name the file it replaces. It is set on every path a caller can observe, including
	// the safe-mode one, because a caller reporting where the database is needs the
	// answer even when the database could not be opened.
	databasePath string
}

// Open opens or creates the application database, migrates it, and fails closed.
func Open(ctx context.Context, dbPath, snapshotDir string) (*Handle, error) {
	return open(ctx, dbPath, snapshotDir, embeddedMigrations, time.Now)
}

// open is the real constructor, and the one thing it does beyond delegating is record the
// database PATH on the handle it returns.
//
// The path is recorded here rather than at each of the six places inside that build a
// handle, because a safe-mode handle needs it too: a caller reporting where the database
// is must get an answer on the path where the database could not be opened, and a
// parameter threaded through six returns is six chances to forget one.
func open(ctx context.Context, dbPath, snapshotDir string, fsys fs.FS, now func() time.Time) (*Handle, error) {
	handle, err := openDatabase(ctx, dbPath, snapshotDir, fsys, now)
	if handle != nil {
		handle.databasePath = dbPath
	}
	return handle, err
}

func openDatabase(ctx context.Context, dbPath, snapshotDir string, fsys fs.FS, now func() time.Time) (*Handle, error) {
	if dbPath == "" || snapshotDir == "" {
		return nil, apperror.New("DATABASE_OPEN_FAILED", "storage", false, "The local database could not be opened.", errors.New("missing database path"))
	}
	db, err := openDriver(ctx, dbPath)
	if err != nil {
		return safeHandle(err, "DATABASE_OPEN_FAILED", "The local database could not be opened."), nil
	}
	if err := integrityCheck(ctx, db); err != nil {
		_ = db.Close()
		return safeHandle(err, "DATABASE_INTEGRITY_FAILED", "The local database failed an integrity check."), nil
	}
	pending, err := hasPendingMigrations(ctx, db, fsys)
	if err != nil {
		_ = db.Close()
		return safeHandle(err, "DATABASE_MIGRATION_FAILED", "The local database could not be updated."), nil
	}
	var snapshot string
	if pending {
		var userVersion int
		if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
			_ = db.Close()
			return safeHandle(err, "DATABASE_MIGRATION_FAILED", "The local database could not be updated."), nil
		}
		if userVersion > 0 {
			snapshot, err = vacuumSnapshot(ctx, db, snapshotDir, now())
			if err != nil {
				_ = db.Close()
				return safeHandle(err, "DATABASE_SNAPSHOT_FAILED", "The local database could not be copied before updating."), nil
			}
		}
	}
	if err := applyMigrations(ctx, db, fsys); err != nil {
		_ = db.Close()
		return &Handle{mode: ModeSafe, err: asAppError(err, "DATABASE_MIGRATION_FAILED", "The local database could not be updated."), snapshot: snapshot}, nil
	}
	return &Handle{mode: ModeReady, db: db, snapshot: snapshot, databasePath: dbPath}, nil
}

// Mode reports whether the handle is writable or in safe mode.
func (h *Handle) Mode() Mode {
	if h == nil {
		return ModeSafe
	}
	return h.mode
}

// SQL returns the writable pool, or nil in safe mode.
func (h *Handle) SQL() *sql.DB {
	if h == nil || h.mode != ModeReady {
		return nil
	}
	return h.db
}

// Err returns the stable failure that caused safe mode.
func (h *Handle) Err() *apperror.Error {
	if h == nil {
		return nil
	}
	return h.err
}

// DatabasePath is the live database file a promotion replaces.
//
// It is exposed because a restore's atomic swap has to name the file it moves, and the
// driver does not report the path it opened in a form a rename can use. The value is what
// `Open` was given, so a caller cannot be handed a path other than the one in use.
func (h *Handle) DatabasePath() string {
	if h == nil {
		return ""
	}
	return h.databasePath
}

// SnapshotPath is the pre-migration copy, if one was created.
func (h *Handle) SnapshotPath() string {
	if h == nil {
		return ""
	}
	return h.snapshot
}

// Close checkpoints WAL and closes the writable handle.
func (h *Handle) Close(ctx context.Context) error {
	if h == nil || h.db == nil {
		return nil
	}
	_, checkpointErr := h.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	err := errors.Join(checkpointErr, h.db.Close())
	h.db = nil
	h.mode = ModeSafe
	return err
}

func integrityCheck(ctx context.Context, db *sql.DB) error {
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return errors.New("integrity check failed")
	}
	return nil
}

func hasPendingMigrations(ctx context.Context, db *sql.DB, fsys fs.FS) (bool, error) {
	migrations, err := loadMigrationFiles(fsys)
	if err != nil {
		return false, err
	}
	applied, err := loadApplied(ctx, db)
	if err != nil {
		return false, err
	}
	for _, migration := range migrations {
		if _, ok := applied[migration.version]; !ok {
			return true, nil
		}
	}
	return false, nil
}

func vacuumSnapshot(ctx context.Context, db *sql.DB, snapshotDir string, now time.Time) (string, error) {
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(snapshotDir, now.UTC().Format("2006-01-02T15-04-05Z")+".sqlite")
	var quoted string
	if err := db.QueryRowContext(ctx, "SELECT quote(?)", dest).Scan(&quoted); err != nil {
		return "", err
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO "+quoted); err != nil {
		return "", err
	}
	file, err := os.OpenFile(dest, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	return dest, errors.Join(syncErr, closeErr)
}

func safeHandle(cause error, code, message string) *Handle {
	return &Handle{mode: ModeSafe, err: asAppError(cause, code, message)}
}

func asAppError(cause error, code, message string) *apperror.Error {
	var appErr *apperror.Error
	if errors.As(cause, &appErr) {
		return appErr
	}
	return apperror.New(code, "storage", false, message, cause)
}
