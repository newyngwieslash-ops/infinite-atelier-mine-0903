package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	appbackup "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"
)

// backup_scheduler.go is FR-180/NFR-002's automatic backup (T12): a periodic
// EXPORT of the ordinary (secret-free) backup on an interval the user
// configures, with retention and failure isolation.
//
// # What it reuses, and why that matters
//
// The scheduler calls the SAME `ExportService.Export` the manual backup
// binding calls — manifest, VACUUM-INTO snapshot, file objects, secret scan,
// checksums. A scheduled backup that assembled its archive differently would
// be a second backup implementation, and the audit's requirement is that the
// automatic backup be verifiable against AC-BACKUP-001 like the manual one.
//
// # The failure rule
//
// A FAILED scheduled backup never deletes the previous backup (fail-safe by
// construction: exports land in NEW timestamped files, and only successful
// exports count toward retention). A failed run is logged and surfaces in the
// next manual backup panel read via the last-result record.
//
// # Retention
//
// Keep the newest `retain` successful archives; older ones are removed after
// a successful new export. Retention only ever runs AFTER a successful
// export, so a failing backup cannot prune the history that protects it.

// backupExporter is what the scheduler needs from the export service — the
// same Export command the manual backup binding calls. Declared as an
// interface so a test can drive the schedule without a file store.
type backupExporter interface {
	Export(ctx context.Context) (appbackup.ExportResult, error)
}

// backupScheduler runs the ordinary backup on an interval.
type backupScheduler struct {
	mu          sync.Mutex
	exporter    backupExporter
	dir         string
	interval    time.Duration
	retain      int
	lastSuccess time.Time
	lastError   string
	cancel      context.CancelFunc
	done        chan struct{}
}

// newBackupScheduler composes the scheduler. A nil exporter or a non-positive
// interval disables scheduling — the caller reports disabled rather than
// running a ticker that does nothing.
func newBackupScheduler(exporter backupExporter, dir string, interval time.Duration, retain int) *backupScheduler {
	if exporter == nil || interval <= 0 {
		return nil
	}
	if retain <= 0 {
		retain = 3
	}
	return &backupScheduler{
		exporter: exporter,
		dir:      dir,
		interval: interval,
		retain:   retain,
		done:     make(chan struct{}),
	}
}

// start runs the loop: one export immediately if the first interval has
// already elapsed by construction, then every interval tick, until stopped.
func (s *backupScheduler) start(ctx context.Context) {
	if s == nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				s.runOnce(runCtx)
			}
		}
	}()
}

// stop ends the loop and waits for the in-flight export to settle.
func (s *backupScheduler) stop() {
	if s == nil || s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
}

// runOnce performs one scheduled export, writes the archive under its own
// timestamped name, and prunes retention.
//
// The archive is written by THIS scheduler (the manual binding returns base64
// to the webview's save dialog instead), so the two paths share the export
// command but own their own transport.
func (s *backupScheduler) runOnce(ctx context.Context) {
	s.mu.Lock()
	s.lastError = ""
	s.mu.Unlock()
	result, err := s.exporter.Export(ctx)
	if err != nil {
		s.mu.Lock()
		s.lastError = err.Error()
		s.mu.Unlock()
		slog.Warn("scheduled backup failed", "error", err.Error())
		return
	}
	name := "atelier-auto-" + time.Now().UTC().Format("20060102-150405") + ".atelierbak"
	path := filepath.Join(s.dir, name)
	if err := os.WriteFile(path, result.Bytes, 0o600); err != nil {
		s.mu.Lock()
		s.lastError = err.Error()
		s.mu.Unlock()
		slog.Warn("scheduled backup could not be written", "error", err.Error())
		return
	}
	s.mu.Lock()
	s.lastSuccess = time.Now().UTC()
	s.mu.Unlock()
	slog.Info("scheduled backup completed", "file", name, "bytes", len(result.Bytes))
	s.prune(ctx)
}

// prune keeps the newest retain archives, deleting only files the scheduler
// itself names (the timestamped prefix), so a user's manual export file is
// never a candidate.
func (s *backupScheduler) prune(ctx context.Context) {
	matches, err := filepath.Glob(filepath.Join(s.dir, "atelier-auto-*.atelierbak"))
	if err != nil || len(matches) <= s.retain {
		return
	}
	// Glob returns sorted names; the timestamp format sorts chronologically.
	excess := matches[:len(matches)-s.retain]
	for _, path := range excess {
		_ = removeFile(path)
	}
}

// status reports the scheduler's last outcome for the settings panel.
func (s *backupScheduler) status() (time.Time, string) {
	if s == nil {
		return time.Time{}, "disabled"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSuccess, s.lastError
}

// removeFile is a named indirection so the intent (scheduled backup retention)
// is greppable rather than buried in an os call.
func removeFile(path string) error {
	return os.Remove(path)
}
