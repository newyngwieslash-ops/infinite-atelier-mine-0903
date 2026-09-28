package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	appbackup "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"

)

// backup_scheduler_t12_test.go is T12's acceptance at the scheduler's own
// boundary: the loop exports on its interval through the SAME export service
// the manual backup uses, a failed run never deletes the previous archive,
// and retention prunes only the scheduler's own files.

// stubBackupExporter produces a fixed archive and counts its calls.
type stubBackupExporter struct {
	calls int
	fail  bool
}

func (s *stubBackupExporter) Available() bool { return s != nil }
func (s *stubBackupExporter) Export(ctx context.Context) (appbackup.ExportResult, error) {
	s.calls++
	if s.fail {
		return appbackup.ExportResult{}, errBackupFailed()
	}
	return appbackup.ExportResult{Bytes: []byte("archive-" + string(rune('0'+s.calls)))}, nil
}

// TestTheSchedulerExportsOnItsInterval runs a two-tick schedule with a short
// interval and asserts one archive per tick landed in the directory.
func TestTheSchedulerExportsOnItsInterval(t *testing.T) {
	dir := t.TempDir()
	exporter := &stubBackupExporter{}
	scheduler := newBackupScheduler(exporter, dir, 20*time.Millisecond, 5)
	if scheduler == nil {
		t.Fatal("a valid scheduler configuration composed nothing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.start(ctx)

	// Wait for at least two archives with a deadline rather than a fixed
	// sleep: a scheduler that fires late is still correct, one that never
	// fires is the failure the deadline proves.
	deadline := time.Now().Add(3 * time.Second)
	archives := 0
	for time.Now().Before(deadline) {
		list, err := filepath.Glob(filepath.Join(dir, "atelier-auto-*.atelierbak"))
		if err != nil {
			t.Fatal(err)
		}
		archives = len(list)
		if archives >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	scheduler.stop()

	if archives < 2 {
		t.Fatalf("%d archives before the deadline; the schedule did not export on its cadence", archives)
	}
	for _, path := range mustArchives(t, dir) {
		body, err := os.ReadFile(path)
		if err != nil || len(body) == 0 {
			t.Fatalf("an archive is empty or unreadable (%s)", path)
		}
	}
}

// mustArchives lists the scheduler's archives.
func mustArchives(t *testing.T, dir string) []string {
	t.Helper()
	list, err := filepath.Glob(filepath.Join(dir, "atelier-auto-*.atelierbak"))
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// TestAFailedBackupNeverDeletesThePreviousArchive is the fail-safe rule: with
// the exporter failing, retention must not run and the last good archive must
// still be on disk.
func TestAFailedBackupNeverDeletesThePreviousArchive(t *testing.T) {
	dir := t.TempDir()
	exporter := &stubBackupExporter{}
	scheduler := newBackupScheduler(exporter, dir, 20*time.Millisecond, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// One good export lands an archive.
	scheduler.runOnce(ctx)
	good, err := filepath.Glob(filepath.Join(dir, "atelier-auto-*.atelierbak"))
	if err != nil || len(good) != 1 {
		t.Fatalf("the first export produced %d archives", len(good))
	}

	// Every later export fails; run several windows' worth of ticks.
	exporter.fail = true
	for i := 0; i < 5; i++ {
		scheduler.runOnce(ctx)
	}

	still, err := filepath.Glob(filepath.Join(dir, "atelier-auto-*.atelierbak"))
	if err != nil {
		t.Fatal(err)
	}
	if len(still) != 1 || still[0] != good[0] {
		t.Fatalf("%d archives remain; the failed runs must not have pruned the last good backup", len(still))
	}
	if _, lastErr := scheduler.status(); lastErr == "" {
		t.Fatal("the scheduler recorded no error after failed runs; the failure is invisible")
	}
}

// TestRetentionPrunesOnlyItsOwnFiles proves the glob is the boundary: a file
// a user (or the manual path) wrote is never a retention candidate.
func TestRetentionPrunesOnlyItsOwnFiles(t *testing.T) {
	dir := t.TempDir()
	exporter := &stubBackupExporter{}
	// Retain one, so every new export prunes the previous auto file.
	scheduler := newBackupScheduler(exporter, dir, time.Hour, 1)
	ctx := context.Background()

	userFile := filepath.Join(dir, "my-own-backup.atelierbak")
	if err := os.WriteFile(userFile, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	scheduler.runOnce(ctx)
	scheduler.runOnce(ctx)

	if _, err := os.Stat(userFile); err != nil {
		t.Fatal("the user's own backup file was pruned; retention must touch only atelier-auto-* files")
	}
}


// errBackupFailed is the stub's failure.
func errBackupFailed() error { return errBackupFailedType{} }

type errBackupFailedType struct{}

func (errBackupFailedType) Error() string { return "the backup export failed" }
