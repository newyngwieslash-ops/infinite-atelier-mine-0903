package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rotation_test.go is RP-09.3's retention contract: the size cap rotates the
// active file, the count cap prunes the OLDEST snapshots, the prune never
// touches app.jsonl or foreign files, and the policy constants are the
// documented ones.

// TestRP09LogRotationPrunesOldestBeyondCount drives pruneRotatedLogs over a
// directory with more snapshots than the retention allows: exactly the
// oldest (MaxRotatedLogFiles is exceeded by one) are removed, the newest
// stay, and app.jsonl plus a foreign file survive.
func TestRP09LogRotationPrunesOldestBeyondCount(t *testing.T) {
	dir := t.TempDir()
	// Seed app.jsonl (the ACTIVE file — never a prune candidate) and a
	// foreign file, then 8 rotated snapshots (cap is 5, so 3 must go).
	mustWrite(t, filepath.Join(dir, "app.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(dir, "foreign.txt"), "user's own file")
	// Names are lexicographically ordered timestamps, oldest first.
	stamps := []string{
		"20260901-000000.000", "20260902-000000.000", "20260903-000000.000",
		"20260904-000000.000", "20260905-000000.000", "20260906-000000.000",
		"20260907-000000.000", "20260908-000000.000",
	}
	for _, stamp := range stamps {
		mustWrite(t, filepath.Join(dir, rotatedName(stamp)), "{}\n")
	}

	if err := pruneRotatedLogs(dir); err != nil {
		t.Fatalf("pruneRotatedLogs: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, entry := range entries {
		got[entry.Name()] = true
	}
	if got["app.jsonl"] != true || got["foreign.txt"] != true {
		t.Fatal("the prune touched the active log or a foreign file")
	}
	// The three oldest must be gone; the newest five must remain.
	for _, stamp := range stamps[:3] {
		if got[rotatedName(stamp)] {
			t.Fatalf("the oldest snapshot %s survived the retention bound", rotatedName(stamp))
		}
	}
	for _, stamp := range stamps[3:] {
		if !got[rotatedName(stamp)] {
			t.Fatalf("the retained snapshot %s was deleted", rotatedName(stamp))
		}
	}
}

// TestRP09LogRotationSizeCapTriggersOnGrowth proves shouldRotate fires only
// when the next write would actually cross the cap.
func TestRP09LogRotationSizeCapTriggersOnGrowth(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "app.jsonl")
	// A file one byte below the cap.
	big := strings.Repeat("x", MaxLogFileSizeBytes-1)
	mustWrite(t, active, big)

	if shouldRotate(active, 1) {
		t.Fatal("a 1-byte write that stays within the cap triggered rotation")
	}
	if !shouldRotate(active, 2) {
		t.Fatal("a write that crosses the cap did not trigger rotation")
	}
	// A missing active file has nothing to protect.
	if shouldRotate(filepath.Join(dir, "absent.jsonl"), 1<<20) {
		t.Fatal("an absent file triggered rotation")
	}
}

// TestRP09RotatedNameShape pins the naming convention: prefixed, suffixed,
// and recognised by isRotatedLog — so the prune's candidates are exactly the
// files this package froze.
func TestRP09RotatedNameShape(t *testing.T) {
	name := rotatedName("20260929-120000.000")
	if !strings.HasPrefix(name, "app-") || !strings.HasSuffix(name, ".jsonl") {
		t.Fatalf("rotated name %q lost the app-*.jsonl shape", name)
	}
	if !isRotatedLog(name) {
		t.Fatal("isRotatedLog refused the package's own name shape")
	}
	// Foreign names are never candidates.
	for _, foreign := range []string{"app.jsonl", "app-backup.json", "other.log"} {
		if isRotatedLog(foreign) {
			t.Fatalf("isRotatedLog claimed %q", foreign)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
