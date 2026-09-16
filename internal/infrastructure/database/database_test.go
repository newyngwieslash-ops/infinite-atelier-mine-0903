package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func TestOpenFreshAppliesFoundationWithoutSnapshot(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")
	snapshots := filepath.Join(root, "snapshots")
	handle, err := open(context.Background(), dbPath, snapshots, fstest.MapFS{"000001_foundation.sql": {Data: foundationSQL(t)}}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if handle.Mode() != ModeReady || handle.SQL() == nil {
		t.Fatalf("fresh open was not ready: mode=%s err=%v", handle.Mode(), handle.Err())
	}
	assertUserVersion(t, handle.SQL(), 1)
	entries, err := os.ReadDir(snapshots)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("fresh database created snapshots: %v", entries)
	}
}

func TestOpenSnapshotsBeforeUpgradeUsingQuotedPath(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")
	v1 := fstest.MapFS{"000001_foundation.sql": {Data: foundationSQL(t)}}
	seed, err := open(context.Background(), dbPath, filepath.Join(root, "unused"), v1, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshots := filepath.Join(root, "o'clock")
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	upgrade := fstest.MapFS{
		"000001_foundation.sql": {Data: foundationSQL(t)},
		"000002_later.sql":      {Data: []byte("CREATE TABLE later (id INTEGER PRIMARY KEY);")},
	}
	handle, err := open(context.Background(), dbPath, snapshots, upgrade, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if handle.Mode() != ModeReady {
		t.Fatalf("upgrade was not ready: %v", handle.Err())
	}
	assertUserVersion(t, handle.SQL(), 2)
	info, err := os.Stat(handle.SnapshotPath())
	if err != nil || info.Size() == 0 {
		t.Fatalf("missing snapshot %s: %v", handle.SnapshotPath(), err)
	}
	if filepath.Dir(handle.SnapshotPath()) != snapshots {
		t.Fatalf("snapshot escaped quoted directory: %s", handle.SnapshotPath())
	}
	snap, err := openDriver(context.Background(), handle.SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	assertUserVersion(t, snap, 1)
}

func TestOpenUsesEmbeddedFoundation(t *testing.T) {
	root := t.TempDir()
	handle, err := Open(context.Background(), filepath.Join(root, "app.db"), filepath.Join(root, "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if handle.Mode() != ModeReady {
		t.Fatalf("embedded open was not ready: %v", handle.Err())
	}
	// The embedded set now carries foundation plus WP-02 provider security;
	// the current expected version is the highest embedded migration.
	applied, err := loadApplied(context.Background(), handle.SQL())
	if err != nil {
		t.Fatal(err)
	}
	expected := 0
	for version := range applied {
		if version > expected {
			expected = version
		}
	}
	if expected == 0 {
		t.Fatal("no embedded migrations applied")
	}
	assertUserVersion(t, handle.SQL(), expected)
	if queryInt(t, handle.SQL(), "SELECT COUNT(*) FROM sqlite_master WHERE name='file_objects'") != 1 {
		t.Fatal("embedded foundation tables missing")
	}
}

func TestOpenSnapshotFailureReturnsSafeMode(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")
	v1 := fstest.MapFS{"000001_foundation.sql": {Data: foundationSQL(t)}}
	seed, err := open(context.Background(), dbPath, filepath.Join(root, "unused"), v1, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "o'clock")
	if err := os.WriteFile(blocked, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	upgrade := fstest.MapFS{
		"000001_foundation.sql": {Data: foundationSQL(t)},
		"000002_later.sql":      {Data: []byte("CREATE TABLE later (id INTEGER PRIMARY KEY);")},
	}
	handle, err := open(context.Background(), dbPath, blocked, upgrade, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if handle.Mode() != ModeSafe || handle.SQL() != nil || handle.Err() == nil || handle.Err().Code != "DATABASE_SNAPSHOT_FAILED" || handle.Err().Diagnostic == "" {
		t.Fatalf("snapshot failure did not fail closed: %+v %+v", handle.Mode(), handle.Err())
	}
	original, err := openDriver(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	assertUserVersion(t, original, 1)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal("original database was deleted")
	}
}

func TestOpenIntegrityFailureReturnsSafeMode(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")
	v1 := fstest.MapFS{"000001_foundation.sql": {Data: foundationSQL(t)}}
	seed, err := open(context.Background(), dbPath, filepath.Join(root, "unused"), v1, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	ready, err := open(context.Background(), dbPath, filepath.Join(root, "unused2"), v1, time.Now)
	if err != nil || ready.SQL() == nil {
		t.Fatalf("seed reopen failed: %v", err)
	}
	if _, err := ready.SQL().ExecContext(context.Background(), `INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at) VALUES (?, ?, 'text/plain', 1, '2026-09-07T00:00:00Z')`, checksumOf([]byte("payload")), checksumOf([]byte("payload"))); err != nil {
		t.Fatal(err)
	}
	if err := ready.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 200 {
		t.Fatalf("database too small to corrupt a payload page: %d", len(data))
	}
	offset := 4096
	if offset >= len(data) {
		offset = len(data) - 1
	}
	if offset < 100 {
		t.Fatal("cannot corrupt payload without touching the header")
	}
	data[offset] ^= 0xff
	if err := os.WriteFile(dbPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	handle, err := open(context.Background(), dbPath, filepath.Join(root, "snapshots"), v1, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if handle.Mode() != ModeSafe || handle.SQL() != nil || handle.Err() == nil || handle.Err().Diagnostic == "" {
		t.Fatalf("integrity failure did not fail closed: %+v %+v", handle.Mode(), handle.Err())
	}
	if handle.Err().Code != "DATABASE_INTEGRITY_FAILED" {
		t.Fatalf("code=%s", handle.Err().Code)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal("original database was deleted")
	}
}

func TestOpenBrokenMigrationReturnsSafeMode(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")
	v1 := fstest.MapFS{"000001_foundation.sql": {Data: foundationSQL(t)}}
	seed, err := open(context.Background(), dbPath, filepath.Join(root, "unused"), v1, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshots := filepath.Join(root, "o'clock")
	broken := fstest.MapFS{
		"000001_foundation.sql": {Data: foundationSQL(t)},
		"000002_broken.sql":     {Data: []byte("CREATE TABLE broken (id INTEGER); SELECT RAISE(ABORT, 'boom');")},
	}
	handle, err := open(context.Background(), dbPath, snapshots, broken, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if handle.Mode() != ModeSafe || handle.SQL() != nil || handle.Err() == nil || handle.Err().Diagnostic == "" {
		t.Fatalf("safe mode not returned: %+v", handle)
	}
	if handle.Err().Code != "DATABASE_MIGRATION_FAILED" {
		t.Fatalf("code=%s", handle.Err().Code)
	}
	original, err := openDriver(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	assertUserVersion(t, original, 1)
	if queryInt(t, original, "SELECT COUNT(*) FROM sqlite_master WHERE name='broken'") != 0 {
		t.Fatal("original database was mutated")
	}
	snap, err := openDriver(context.Background(), handle.SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	assertUserVersion(t, snap, 1)
}
