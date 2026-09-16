package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

func foundationSQL(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("migrations", "000001_foundation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func openTempDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openDriver(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func checksumOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestMigrationFreshAppliesVersionOnce(t *testing.T) {
	db := openTempDB(t)
	fsys := fstest.MapFS{"000001_foundation.sql": {Data: foundationSQL(t)}}
	if err := applyMigrations(context.Background(), db, fsys); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, 1)
	assertApplied(t, db, 1, checksumOf(foundationSQL(t)))
	if err := applyMigrations(context.Background(), db, fsys); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, 1)
	if queryInt(t, db, "SELECT COUNT(*) FROM schema_migrations") != 1 {
		t.Fatal("second run was not idempotent")
	}
}

func TestMigrationChecksumMismatchFailsClosed(t *testing.T) {
	db := openTempDB(t)
	original := foundationSQL(t)
	if err := applyMigrations(context.Background(), db, fstest.MapFS{"000001_foundation.sql": {Data: original}}); err != nil {
		t.Fatal(err)
	}
	tampered := append(append([]byte{}, original...), []byte("-- changed")...)
	err := applyMigrations(context.Background(), db, fstest.MapFS{"000001_foundation.sql": {Data: tampered}})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != "DATABASE_MIGRATION_FAILED" {
		t.Fatalf("checksum mismatch: %v", err)
	}
	assertUserVersion(t, db, 1)
	assertApplied(t, db, 1, checksumOf(original))
}

func TestMigrationBadSecondRollsBackTransaction(t *testing.T) {
	db := openTempDB(t)
	fsys := fstest.MapFS{
		"000001_foundation.sql": {Data: foundationSQL(t)},
		"000002_broken.sql":     {Data: []byte("CREATE TABLE broken (id INTEGER); SELECT RAISE(ABORT, 'boom');")},
	}
	if err := applyMigrations(context.Background(), db, fsys); err == nil {
		t.Fatal("broken migration succeeded")
	}
	assertUserVersion(t, db, 1)
	if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name='broken'") != 0 {
		t.Fatal("rolled-back table remains")
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM schema_migrations") != 1 {
		t.Fatal("version 2 recorded after rollback")
	}
}

func TestMigrationOrderIsNumericAndDuplicatesFail(t *testing.T) {
	db := openTempDB(t)
	fsys := fstest.MapFS{
		"10_later.sql":     {Data: []byte("CREATE TABLE later (hash TEXT NOT NULL REFERENCES file_objects(hash));")},
		"2_foundation.sql": {Data: foundationSQL(t)},
	}
	if err := applyMigrations(context.Background(), db, fsys); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, 10)
	if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name='later'") != 1 {
		t.Fatal("later table missing; numeric order failed")
	}
	dup := fstest.MapFS{
		"000001_a.sql": {Data: foundationSQL(t)},
		"000001_b.sql": {Data: foundationSQL(t)},
	}
	if err := applyMigrations(context.Background(), openTempDB(t), dup); err == nil {
		t.Fatal("duplicate versions accepted")
	}
}

func TestMigrationReadsAllAppliedRows(t *testing.T) {
	db := openTempDB(t)
	fsys := fstest.MapFS{
		"000001_foundation.sql": {Data: foundationSQL(t)},
		"000002_later.sql":      {Data: []byte("CREATE TABLE later (id INTEGER PRIMARY KEY);")},
	}
	if err := applyMigrations(context.Background(), db, fsys); err != nil {
		t.Fatal(err)
	}
	applied, err := loadApplied(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 || applied[1] == "" || applied[2] == "" {
		t.Fatalf("applied rows not fully iterated: %#v", applied)
	}
}

// headMigrationVersion reports the highest version in a migration set, so
// tests assert "the current schema head" instead of a hardcoded number that
// breaks every time a new forward migration lands.
func headMigrationVersion(t *testing.T, fsys fstest.MapFS) int {
	t.Helper()
	files, err := loadMigrationFiles(fsys)
	if err != nil {
		t.Fatal(err)
	}
	head := 0
	for _, file := range files {
		if file.version > head {
			head = file.version
		}
	}
	if head == 0 {
		t.Fatal("migration set is empty")
	}
	return head
}

func assertUserVersion(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	got := queryInt(t, db, "PRAGMA user_version")
	if got != want {
		t.Fatalf("user_version=%d, want %d", got, want)
	}
}

func assertApplied(t *testing.T, db *sql.DB, version int, checksum string) {
	t.Helper()
	applied, err := loadApplied(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if applied[version] != checksum {
		t.Fatalf("version %d checksum=%q, want %q", version, applied[version], checksum)
	}
}

func queryInt(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var value int
	if err := db.QueryRowContext(context.Background(), query).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
