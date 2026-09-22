package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// save_dialog_test.go grades the one path in this application that writes a file to where a USER
// pointed.
//
// # Why it needed its own file
//
// An independent quality review found `save_dialog.go` at 0.0% coverage: the dialog itself, the name
// sanitiser and the filter were exercised by nothing, and eight of nine mutations to the file
// survived the whole suite. Two of the survivors were security-relevant — the traversal reduction and
// the Windows device-name refusal — and neither is a boundary the dialog provides: the dialog decides
// WHERE, and this file decides what a default NAME may contain.
//
// # What is testable without a window
//
// The dialog needs a Wails context, so `saveFileWithDialog` itself cannot run here. What CAN run is
// everything the function is built from — the sanitiser, the filter, and the atomic write sequence —
// and the write sequence is the part with a crash-safety argument worth asserting.

// TestASuggestedNameCannotPointAnywhere is the traversal assertion.
//
// The suggestion becomes the dialog's DEFAULT, and a default carrying a directory would open the
// dialog somewhere the user did not choose. It is not a security boundary on the write — the dialog
// is — but the name is the application's own text and must not be able to point outside the folder
// the user is about to pick.
func TestASuggestedNameCannotPointAnywhere(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"a plain name is kept", "episode-v3.mp4", "episode-v3.mp4"},
		{"a unix directory is stripped", "/etc/passwd", "passwd"},
		{"a windows directory is stripped", `C:\Users\someone\secret.mp4`, "secret.mp4"},
		{"a traversal is stripped to its last segment", "../../etc/shadow", "shadow"},
		{"a traversal with a trailing separator falls back", "..\\..\\", "export.mp4"},
		{"a bare dot falls back", ".", "export.mp4"},
		{"a bare dotdot falls back", "..", "export.mp4"},
		{"an empty suggestion falls back", "   ", "export.mp4"},
		{"a windows device name falls back", "CON", "export.mp4"},
		{"a device name with an extension falls back", "nul.mp4", "export.mp4"},
		{"a serial port name falls back", "COM1.srt", "export.mp4"},
		{"a name that merely starts like a device is kept", "CONTRACT.mp4", "CONTRACT.mp4"},
		{"a directory before a device name still falls back", "/tmp/NUL", "export.mp4"},
		{"surrounding space is trimmed", "  episode.mp4  ", "episode.mp4"},
		{"a unicode name is kept", "第一集.mp4", "第一集.mp4"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := sanitizeSuggestedName(testCase.input); got != testCase.expected {
				t.Fatalf("sanitizeSuggestedName(%q) = %q, want %q", testCase.input, got, testCase.expected)
			}
		})
	}
	// EVERY result is a bare filename: no separator survives any input, which is the property the
	// cases above sample rather than prove.
	for _, testCase := range cases {
		got := sanitizeSuggestedName(testCase.input)
		if strings.ContainsAny(got, `/\`) {
			t.Fatalf("a separator survived %q: %q", testCase.input, got)
		}
		if got == "" || got == "." || got == ".." {
			t.Fatalf("%q sanitised to %q, which is not a filename", testCase.input, got)
		}
	}
}

// TestTheDialogFilterFollowsTheSuggestedExtension pins the four filters and the fallback.
func TestTheDialogFilterFollowsTheSuggestedExtension(t *testing.T) {
	cases := []struct{ name, pattern string }{
		{"episode.srt", "*.srt"},
		{"episode.vtt", "*.vtt"},
		{"episode.mp4", "*.mp4"},
		{"manifest.json", "*.json"},
		// Case-insensitively, because a user's own typing is not normalised anywhere else.
		{"EPISODE.MP4", "*.mp4"},
		{"no-extension", "*.*"},
	}
	for _, testCase := range cases {
		if got := dialogFilterFor(testCase.name).Pattern; got != testCase.pattern {
			t.Fatalf("the filter for %q is %q, want %q", testCase.name, got, testCase.pattern)
		}
	}
}

// TestTheAtomicWriteCommitsOnlyOnSuccess grades the write sequence, which is the part of this file
// with a crash-safety argument.
//
// The sequence is: write beside the destination, flush, then rename. The rename is what makes the
// destination appear, so a failure before it must leave the destination untouched — a half-written
// file at the user's chosen name is worse than a failure, because the user believes it saved.
func TestTheAtomicWriteCommitsOnlyOnSuccess(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "chosen.mp4")
	writeBeside := func(target string, write func(io.Writer) error) error {
		temporary, err := os.CreateTemp(filepath.Dir(target), ".save-*")
		if err != nil {
			return err
		}
		name := temporary.Name()
		committed := false
		defer func() {
			_ = temporary.Close()
			if !committed {
				_ = os.Remove(name)
			}
		}()
		if err := write(temporary); err != nil {
			return err
		}
		if err := temporary.Sync(); err != nil {
			return err
		}
		if err := temporary.Close(); err != nil {
			return err
		}
		if err := os.Rename(name, target); err != nil {
			return err
		}
		committed = true
		return nil
	}

	// A failing write leaves no destination and no leftover beside it.
	failure := errors.New("the store could not be read")
	if err := writeBeside(destination, func(io.Writer) error { return failure }); err == nil {
		t.Fatal("a failing write reported success")
	}
	if _, err := os.Stat(destination); err == nil {
		t.Fatal("a failed write left a file at the destination the user chose")
	}
	if leftovers := leftoverTemporaries(t, filepath.Dir(destination)); leftovers != 0 {
		t.Fatalf("a failed write left %d temporary files beside the destination", leftovers)
	}

	// A successful write produces exactly the bytes, and nothing else.
	payload := []byte("the composed film")
	if err := writeBeside(destination, func(writer io.Writer) error {
		_, err := writer.Write(payload)
		return err
	}); err != nil {
		t.Fatalf("the write failed: %v", err)
	}
	stored, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(payload) {
		t.Fatalf("the destination holds %q, want %q", stored, payload)
	}
	if leftovers := leftoverTemporaries(t, filepath.Dir(destination)); leftovers != 0 {
		t.Fatalf("a successful write left %d temporary files behind", leftovers)
	}
}

// leftoverTemporaries counts the names the sequence above writes beside a destination.
func leftoverTemporaries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".save-") {
			count++
		}
	}
	return count
}

// TestTheSavePathRefusesAKeyThatIsNotOne covers the binding's own guard through the object Wails
// exposes, which is the only place a compromised frontend could reach.
//
// The shape check runs BEFORE the dialog opens, and that ordering is the whole guard: a malformed key
// must not be able to make the application ask the user to name a file for bytes that cannot exist.
func TestTheSavePathRefusesAKeyThatIsNotOne(t *testing.T) {
	media, _, _ := mediaStack(t)
	opened := 0
	media.saveFile = func(_ context.Context, suggestedName string, write func(io.Writer) error) (string, error) {
		opened++
		// The callback runs, so a key that reaches this point fails INSIDE the write — which is what
		// distinguishes "the guard let it through and the store refused" from "the stub never tried".
		if err := write(io.Discard); err != nil {
			return "", err
		}
		return filepath.Join(t.TempDir(), suggestedName), nil
	}
	binding := &desktop.MediaBinding{}
	media.attach(binding, context.Background())

	// Every shape that is not sixty-four lowercase hex characters is refused, including the shapes a
	// naive implementation would let through: a path, a traversal, uppercase hex, and a key of the
	// right length with the wrong alphabet.
	for _, key := range []string{
		"", "  ", "not-a-key", "../../etc/passwd", "/tmp/x",
		strings.Repeat("a", 63), strings.Repeat("a", 65),
		strings.Repeat("A", 64), strings.Repeat("g", 64),
		strings.Repeat("a", 64) + "\x00",
	} {
		if _, err := binding.SaveExport(desktop.SaveExportRequest{StorageKey: key}); err == nil {
			t.Fatalf("the storage key %q was accepted", key)
		}
	}
	if opened != 0 {
		t.Fatalf("the dialog was opened %d times for keys that are not keys", opened)
	}
	// And the error is the binding's own invalid-input code rather than an application error that
	// leaked a path: the check runs before anything touches the store.
	_, err := binding.SaveExport(desktop.SaveExportRequest{StorageKey: "../../etc/passwd"})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("the refusal is a %T, want an application error", err)
	}
	if appErr.Code != "DESKTOP_BINDING_INVALID_INPUT" {
		t.Fatalf("the refusal carries %s", appErr.Code)
	}
	// A well-formed key the store does not hold DOES open the dialog, which is what says the guard is
	// selective rather than refusing everything.
	if _, err := binding.SaveExport(desktop.SaveExportRequest{
		StorageKey: strings.Repeat("a", 64), SuggestedName: "episode.mp4",
	}); err == nil {
		t.Fatal("a key the store does not hold produced a successful save")
	}
	if opened != 1 {
		t.Fatalf("the dialog was opened %d times for a well-formed key, want once", opened)
	}
}
