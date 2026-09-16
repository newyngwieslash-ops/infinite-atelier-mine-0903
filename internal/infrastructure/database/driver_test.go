package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

func TestDriverContract(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "atelier space # % & ' 中文.sqlite")
	db, err := openDriver(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if db.Stats().MaxOpenConnections != 1 {
		t.Fatalf("unbounded connection pool: %+v", db.Stats())
	}
	// Dropping idle connections proves the DSN applies settings on every open.
	db.SetMaxIdleConns(0)
	for i := 0; i < 2; i++ {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
				t.Error(err)
			}
		})
		checkDriverPragmas(t, ctx, conn)
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db.SetMaxIdleConns(1)
	for _, statement := range []string{
		"CREATE TABLE parent (id INTEGER PRIMARY KEY)",
		"CREATE TABLE child (parent_id INTEGER REFERENCES parent(id))",
		"INSERT INTO parent VALUES (1)",
		"INSERT INTO child VALUES (1)",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO child VALUES (2)"); err == nil {
		t.Fatal("foreign key violation accepted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database was not created at the exact requested path: %v", err)
	}
	var version string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("SQLite %s; WAL, foreign keys, busy_timeout=5000; pool=1", version)

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	blockedCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := db.PingContext(blockedCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool acquisition did not respect cancellation: %v", err)
	}
}

func checkDriverPragmas(t *testing.T, ctx context.Context, conn *sql.Conn) {
	t.Helper()
	var foreignKeys, busyTimeout int
	var journalMode string
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign_keys=%d: %v", foreignKeys, err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil || !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode=%s: %v", journalMode, err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil || busyTimeout != 5000 {
		t.Fatalf("busy_timeout=%d: %v", busyTimeout, err)
	}
}

func TestDriverOpenFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "private-database.sqlite")
	db, err := openDriver(context.Background(), path)
	var appErr *apperror.Error
	if db != nil || !errors.As(err, &appErr) || appErr.Code != "DATABASE_OPEN_FAILED" || appErr.Diagnostic == "" {
		t.Fatalf("expected diagnosable failure and no database: db=%v err=%v", db, err)
	}
	if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "private-database") {
		t.Fatal("safe error exposes database path")
	}
}

func TestDriverCanceledOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "canceled.sqlite")
	db, err := openDriver(ctx, path)
	if db != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation and no database: db=%v err=%v", db, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled open created a file: %v", err)
	}
}
