package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// rotation.go is RP-09.3's log retention: FR-180's 「日志保留时间和大小限制」
// as a size-capped, count-bounded rotation over the app.jsonl log the Open
// constructor owns.
//
// # The policy, stated in constants so it is auditable
//
//   - maxLogFileSizeBytes caps ONE log file. A write that would push the
//     active file past the cap rotates it first: app.jsonl becomes
//     app-<timestamp>.jsonl (a frozen snapshot, never appended again) and a
//     fresh app.jsonl starts.
//   - maxRotatedLogFiles bounds how many frozen snapshots remain, oldest
//     deleted first. Retention is by COUNT because the cap is per file: the
//     worst case is maxRotatedLogFiles × maxLogFileSizeBytes, a bound a user
//     can compute.
//
// The rotation is enforced at startup (pruneRotatedLogs) and before each
// append decision (shouldRotate), which is the whole write path — there is
// no other writer of app.jsonl.
//
// SECURITY note: the log content is already redacted by the redacting
// handler; rotation never reads or transforms log lines, only file names and
// sizes, so it cannot leak what it moves.

const (
	// MaxLogFileSizeBytes caps the active log file (10 MiB).
	MaxLogFileSizeBytes = 10 << 20
	// MaxRotatedLogFiles bounds the retained rotated snapshots.
	MaxRotatedLogFiles = 5
)

// rotatedName is the frozen snapshot's file name for a timestamp.
func rotatedName(timestamp string) string {
	return "app-" + timestamp + ".jsonl"
}

// isRotatedLog reports whether a directory entry is one of our frozen
// snapshots (never a user file that happens to sit in the log directory).
func isRotatedLog(name string) bool {
	return strings.HasPrefix(name, "app-") && strings.HasSuffix(name, ".jsonl")
}

// shouldRotate reports whether appending to the active file would push it
// past the size cap.
func shouldRotate(activePath string, nextWriteBytes int) bool {
	info, err := os.Stat(activePath)
	if err != nil {
		// A missing file has no size to protect.
		return false
	}
	return info.Size()+int64(nextWriteBytes) > MaxLogFileSizeBytes
}

// rotate freezes the active log under a timestamped name and returns the
// rotated snapshot's path. The caller re-opens app.jsonl afterwards.
func rotate(logDirectory, activePath string) (string, error) {
	snapshot := filepath.Join(logDirectory, rotatedName(utcStamp()))
	if err := os.Rename(activePath, snapshot); err != nil {
		return "", err
	}
	return snapshot, nil
}

// pruneRotatedLogs deletes the OLDEST rotated snapshots beyond the retention
// count. Only files matching isRotatedLog are candidates — the prune cannot
// touch app.jsonl itself or any other file in the directory.
func pruneRotatedLogs(logDirectory string) error {
	entries, err := os.ReadDir(logDirectory)
	if err != nil {
		return err
	}
	var rotated []string
	for _, entry := range entries {
		if !entry.IsDir() && isRotatedLog(entry.Name()) {
			rotated = append(rotated, entry.Name())
		}
	}
	if len(rotated) <= MaxRotatedLogFiles {
		return nil
	}
	// Oldest first by name — the timestamp ordering is lexicographic by
	// construction (UTC, fixed-width fields).
	sort.Strings(rotated)
	for _, name := range rotated[:len(rotated)-MaxRotatedLogFiles] {
		if err := os.Remove(filepath.Join(logDirectory, name)); err != nil {
			return fmt.Errorf("pruning %s: %w", name, err)
		}
	}
	return nil
}

// utcStamp renders the rotation timestamp: UTC, fixed width, sortable.
func utcStamp() string {
	return time.Now().UTC().Format("20060102-150405.000")
}
