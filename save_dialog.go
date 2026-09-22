package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// save_dialog.go is the one place this application asks the user where to put a file.
//
// # What it closes
//
// Before this package the build had NO path that wrote a file to a location the user chose: of the 184
// binding methods it had, `ExportBackup` returned base64 and had no frontend caller, and every other
// "save" is a browser download from `file-saver` reading browser-local storage rather than the Go file
// store. AC-MEDIA-003's export and FR-080's "输出包含导出清单和版本信息" both end at a file, so the
// path had to be built.
//
// # Why the dialog is the security boundary rather than a parameter
//
// SECURITY section 11 allows writing only where the user pointed — "用户选择导出目录时只写明确目标". A
// `destination` field on the request would be the opposite: a frontend that had been compromised could
// write anywhere the process can. So the binding takes a storage KEY and a suggested NAME, this file
// opens a dialog, and the only path that is ever written is the one the dialog returned.
//
// # Why the write happens through a callback
//
// The bytes come from the store rather than from this file's caller, so the caller passes a function
// that copies them into whatever the dialog opened. That ordering matters: a cancelled dialog must not
// have read anything, and with a callback there is nothing to read until a path exists.
//
// # Why a temp file and a rename
//
// A long export can be interrupted — the user closes the window, the disk fills — and a half-written
// file at the destination would look like a finished film. Writing beside the destination and renaming
// it into place means the destination either does not exist or is whole, which is the same rule
// FileStore's own Put follows for the same reason.

// saveFileWithDialog asks the user where to write, then writes the bytes there.
//
// It returns the chosen path, or an empty string when the user cancelled — which is NOT an error: a
// person who changed their mind should not see a failure message for it.
func saveFileWithDialog(ctx context.Context, suggestedName string, write func(io.Writer) error) (string, error) {
	pattern := dialogFilterFor(suggestedName)
	chosen, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title:           "Save the exported file",
		DefaultFilename: filepath.Base(sanitizeSuggestedName(suggestedName)),
		Filters:         []runtime.FileFilter{pattern},
	})
	if err != nil {
		// A dialog that could not open is a real failure — unlike a cancellation, which comes back as
		// an empty path with no error.
		return "", err
	}
	if strings.TrimSpace(chosen) == "" {
		return "", nil
	}
	destination := filepath.Clean(chosen)
	// The temporary file is written BESIDE the destination so the rename below is atomic: a rename
	// across volumes is a copy, and a copy is not atomic.
	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, ".infinite-atelier-export-*")
	if err != nil {
		return "", err
	}
	temporaryName := temporary.Name()
	// Every path out removes the temporary file: a failure must not leave a partial export beside the
	// user's chosen name, where it would look like the file they asked for.
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := write(temporary); err != nil {
		return "", err
	}
	// fsync before the rename, so a crash between the two leaves either nothing or the whole file
	// rather than a destination whose length is a lie.
	if err := temporary.Sync(); err != nil {
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return "", err
	}
	committed = true
	return destination, nil
}

// dialogFilterFor names the file kind a suggested name implies.
//
// The filter is a convenience for the user rather than a constraint on the write: the extension of what
// they choose is theirs, and the bytes are what the store holds either way.
func dialogFilterFor(suggestedName string) runtime.FileFilter {
	extension := strings.ToLower(filepath.Ext(suggestedName))
	switch extension {
	case ".srt":
		return runtime.FileFilter{DisplayName: "SubRip subtitles (*.srt)", Pattern: "*.srt"}
	case ".vtt":
		return runtime.FileFilter{DisplayName: "WebVTT subtitles (*.vtt)", Pattern: "*.vtt"}
	case ".mp4":
		return runtime.FileFilter{DisplayName: "MPEG-4 video (*.mp4)", Pattern: "*.mp4"}
	case ".json":
		return runtime.FileFilter{DisplayName: "Manifest (*.json)", Pattern: "*.json"}
	default:
		return runtime.FileFilter{DisplayName: "All files (*.*)", Pattern: "*.*"}
	}
}

// sanitizeSuggestedName reduces a caller's suggestion to a bare filename.
//
// The suggestion becomes a dialog's default, so a value carrying a directory would put the dialog
// somewhere the user did not choose, and a value carrying `..` would be a path traversal in a field that
// is only meant to be a name. Nothing here is a security boundary on the WRITE — the dialog decides
// that — but a suggestion is the application's own text and should not be able to point anywhere.
func sanitizeSuggestedName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "export.mp4"
	}
	// Both separators, because a Windows filename may not contain either and a caller might send one.
	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		trimmed = trimmed[index+1:]
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return "export.mp4"
	}
	// Windows refuses these names whatever their extension, so a suggestion that was one would make the
	// dialog's own default invalid.
	base := strings.ToUpper(strings.TrimSuffix(trimmed, filepath.Ext(trimmed)))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8",
		"COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "export.mp4"
	}
	return trimmed
}
