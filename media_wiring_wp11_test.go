package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
)

// mediaStack composes the WP-11 stack the way app.go does, over a scratch database and store.
//
// It follows the startup order - drama first, because the media stack's timeline joins tables that
// stack owns - and returns the pieces each test makes its own claim about.
func mediaStack(t *testing.T) (*mediaWiring, *appscript.Service, *database.Handle) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	handle, err := database.Open(ctx, filepath.Join(dir, "studio.db"), filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	if err := handle.Err(); err != nil {
		t.Fatalf("the database is unusable: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })
	// One temp root for the file store and the media engine, which is what app.go does with `dirs.Temp`:
	// the store prefixes its staging directories and the engine creates its own, so sharing the root
	// keeps the two apart without a second location to clean.
	tempDir := filepath.Join(dir, "temp")
	store, err := filestore.New(filepath.Join(dir, "files"), tempDir)
	if err != nil {
		t.Fatalf("opening the file store: %v", err)
	}
	canvasWriter := appprojects.NewService(appprojects.Options{
		Projects: database.NewProjectRepository(handle.SQL()),
		Canvas:   database.NewCanvasRepository(handle.SQL()),
		Settings: database.NewDramaSettingsRepository(handle.SQL()),
		Clock:    appprojectsClock{},
		IDs:      &testIDs{},
	})
	drama := composeDrama(handle, store, canvasWriter)
	if drama == nil {
		t.Fatal("the drama stack did not compose, so the media stack cannot be judged")
	}
	media := composeMedia(handle, drama.script, store, tempDir)
	if media == nil {
		t.Fatal("the media stack did not compose over a writable database")
	}
	return media, drama.script, handle
}

// stubDialog stands in for the save dialog: it runs the write callback into a discard and reports the
// name it was offered, so a test can tell "the user was asked" from "the user was not asked".
type stubDialog struct {
	offered []string
	// written records how many bytes the callback actually produced.
	written int64
}

func (d *stubDialog) save(_ context.Context, suggestedName string, write func(io.Writer) error) (string, error) {
	d.offered = append(d.offered, suggestedName)
	if err := write(&countingWriter{d}); err != nil {
		return "", err
	}
	return filepath.Join("chosen", suggestedName), nil
}

// TestComposeMediaFillsEverySeamTheBindingReads is the wiring assertion for WP-11.
//
// The failure it guards against is the one WP-09's review found twice and WP-10's found once more: a
// service that exists, is tested, and is never handed to the object the frontend calls, so the feature
// is unreachable in a real build while every package's own suite is green. `composeMedia` returns four
// fields and `attach` passes three of them on; a composition root that dropped one would compile, and
// the method reading it would fail closed with "the media services are not composed" for the life of
// the product.
//
// So this test drives the BINDING rather than the wiring struct. Every read below goes through the
// object Wails exposes, which is what makes it a check on the composition rather than on the
// constructors.
func TestComposeMediaFillsEverySeamTheBindingReads(t *testing.T) {
	media, _, _ := mediaStack(t)
	ctx := context.Background()

	// The engine is composed whether or not this machine has ffmpeg, and the two facts are exclusive:
	// an available engine has nothing to diagnose, and an unavailable one names what to install.
	// ARCHITECTURE: "媒体引擎不可用：禁用相关能力并显示诊断".
	if media.engine == nil {
		t.Fatal("the media stack holds no engine")
	}
	if media.engine.Available() && media.engine.Diagnostic() != "" {
		t.Fatalf("an available engine carries the diagnostic %q", media.engine.Diagnostic())
	}
	if !media.engine.Available() && media.engine.Diagnostic() == "" {
		t.Fatal("an unavailable engine says nothing about why, so a user has nothing to act on")
	}

	dialog := &stubDialog{}
	media.saveFile = dialog.save
	binding := &desktop.MediaBinding{}
	media.attach(binding, ctx)

	capability, err := binding.MediaCapability()
	if err != nil {
		t.Fatalf("MediaCapability: %v", err)
	}
	if !capability.SaveAvailable {
		t.Fatal("the attached binding reports no save path, so an export could never leave the application")
	}
	// The capability must track the engine rather than being hardcoded: an engine that is unavailable
	// while the read says otherwise would offer a button whose every press fails.
	if capability.ExportAvailable != media.engine.Available() {
		t.Fatalf("the capability reports export=%v while the engine is available=%v",
			capability.ExportAvailable, media.engine.Available())
	}
	if !capability.ExportAvailable && capability.Diagnostic == "" {
		t.Fatal("export is unavailable and no diagnostic explains it")
	}
	if capability.ExportAvailable && capability.Diagnostic != "" {
		t.Fatalf("export is available and still carries the diagnostic %q", capability.Diagnostic)
	}

	// Every service must be reachable through the binding. Each call is a real read over the real
	// schema, so a nil service and a missing table both surface here rather than in the field.
	//
	// The timeline read is checked by its CODE rather than by "an error came back": a binding with no
	// timeline service also fails, with DESKTOP_BINDING_UNAVAILABLE, so an error alone cannot tell a
	// composition that dropped the service from one whose read correctly refused. Only the media code
	// proves the request reached the service.
	_, err = binding.ReadTimeline(desktop.TimelineRequest{EpisodeID: "episode-absent"})
	if err == nil {
		t.Fatal("a timeline read for an episode with no approved board succeeded")
	}
	if code := appErrorCode(err); code != "MEDIA_INVALID_INPUT" {
		t.Fatalf("the timeline read failed with %s, want the service's own refusal", code)
	}
	if tracks, err := binding.ListSubtitleTracks("episode-absent"); err != nil {
		t.Fatalf("listing an episode's tracks failed: %v", err)
	} else if len(tracks) != 0 {
		t.Fatalf("an episode with no tracks listed %d", len(tracks))
	}
	if records, err := binding.ListExports("episode-absent"); err != nil {
		t.Fatalf("listing an episode's exports failed: %v", err)
	} else if len(records) != 0 {
		t.Fatalf("an episode with no exports listed %d", len(records))
	}
	// The subtitle service is reached by the same argument: a track that is not there must produce the
	// service's not-found refusal rather than the binding's unavailable one. `ListSubtitleCues` will not
	// do, because an absent track and an empty track both answer with no rows — the read that names the
	// track is `MissingSubtitleLines`, whose first step is the track lookup.
	if _, err := binding.MissingSubtitleLines("track-absent"); appErrorCode(err) != "MEDIA_NOT_FOUND" {
		t.Fatalf("the missing-lines read for an absent track failed with %s", appErrorCode(err))
	}
	// The save path is exercised through the binding, and a malformed storage key is refused BEFORE the
	// dialog opens: that ordering is what keeps a malformed request away from every file operation.
	if _, err := binding.SaveExport(desktop.SaveExportRequest{StorageKey: "not-a-storage-key"}); err == nil {
		t.Fatal("a malformed storage key was accepted")
	}
	if len(dialog.offered) != 0 {
		t.Fatalf("the save dialog was opened for a request that was refused: %q", dialog.offered)
	}
	// The helper the assertions above rely on must not read a foreign error as an application one,
	// because a code of UNKNOWN is what a test would compare against and a false match there would make
	// every code assertion vacuous.
	if code := appErrorCode(errors.New("an ordinary error")); code != "UNKNOWN" {
		t.Fatalf("a foreign error mapped to %q", code)
	}
	// A well-formed key that the store does not hold reaches the dialog and fails inside the write,
	// which is the path a missing export takes. The dialog must have been opened, because the key's
	// SHAPE was valid, and the write must have reported the miss rather than writing an empty file.
	//
	// The code is the FILE STORE's rather than the media package's, and that is correct rather than an
	// oversight: `toDramaError` passes an application error through unchanged, so a refusal from the
	// store arrives as the store's own code and a user sees "the file could not be found" instead of a
	// translated restatement of it.
	missing := strings.Repeat("a", 64)
	_, err = binding.SaveExport(desktop.SaveExportRequest{
		StorageKey: missing, SuggestedName: "episode.mp4",
	})
	if err == nil {
		t.Fatal("a storage key the store does not hold produced a successful save")
	}
	if code := appErrorCode(err); code != "FILE_NOT_FOUND" {
		t.Fatalf("a missing export failed with %s, want the store's own refusal", code)
	}
	if len(dialog.offered) != 1 || dialog.offered[0] != "episode.mp4" {
		t.Fatalf("the dialog was offered %q, want the suggested name", dialog.offered)
	}
	if dialog.written != 0 {
		t.Fatalf("a failed read wrote %d bytes", dialog.written)
	}
}

// appErrorCode reports the stable code of an application error, or "UNKNOWN" for anything else.
//
// The wiring tests assert on codes rather than on messages because the CODE is the contract the
// frontend branches on, and because it distinguishes a refusal by the service from a refusal by the
// binding — which is exactly the difference between "composed" and "attached to nothing".
func appErrorCode(err error) string {
	if err == nil {
		return "NONE"
	}
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return "UNKNOWN"
}

// TestComposeMediaFailsClosedWithoutItsDependencies covers the composition root's guard.
//
// A media stack with no script service cannot read a spoken line, one with no file store cannot write an
// export, and one with no temp directory gives the engine nowhere to work. Each must refuse to compose
// rather than composing a stack that fails part way through an export the user has already waited for.
func TestComposeMediaFailsClosedWithoutItsDependencies(t *testing.T) {
	media, script, handle := mediaStack(t)
	store, err := filestore.New(filepath.Join(t.TempDir(), "files"), filepath.Join(t.TempDir(), "temp"))
	if err != nil {
		t.Fatal(err)
	}
	if got := composeMedia(handle, nil, store, t.TempDir()); got != nil {
		t.Fatal("a media stack composed with no script service, so no spoken line could ever be read")
	}
	if got := composeMedia(handle, script, nil, t.TempDir()); got != nil {
		t.Fatal("a media stack composed with no file store, so no export could be written")
	}
	if got := composeMedia(handle, script, store, ""); got != nil {
		t.Fatal("a media stack composed with no temp directory, so the engine would have nowhere to work")
	}
	if got := composeMedia(nil, script, store, t.TempDir()); got != nil {
		t.Fatal("a media stack composed with no database")
	}
	// A safe-mode handle has no SQL pool, and that is the state the application actually reaches when
	// the database could not be opened. It must be refused for the same reason.
	safe := &database.Handle{}
	if got := composeMedia(safe, script, store, t.TempDir()); got != nil {
		t.Fatal("a media stack composed over a handle with no SQL pool")
	}

	// The unattached binding is the state before startup, and every read must fail closed rather than
	// panic: a missed nil check is a crash in the webview rather than a message.
	unattached := &desktop.MediaBinding{}
	if _, err := unattached.ReadTimeline(desktop.TimelineRequest{EpisodeID: "e"}); err == nil {
		t.Fatal("an unattached binding returned a timeline")
	}
	if _, err := unattached.SaveExport(desktop.SaveExportRequest{StorageKey: strings.Repeat("a", 64)}); err == nil {
		t.Fatal("an unattached binding wrote a file")
	}
	capability, err := unattached.MediaCapability()
	if err != nil {
		t.Fatalf("an unattached capability read failed: %v", err)
	}
	if capability.ExportAvailable || capability.SaveAvailable {
		t.Fatal("an unattached binding reports capabilities it does not have")
	}
	if capability.Diagnostic == "" {
		t.Fatal("an unattached binding explains nothing")
	}
	// Attaching to a nil binding must not panic either: the composition root reaches this path when the
	// frontend's binding was never declared, and a panic there is a startup crash.
	media.attach(nil, context.Background())
}

// countingWriter counts what a write callback produced and discards it.
type countingWriter struct{ dialog *stubDialog }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.dialog.written += int64(len(p))
	return len(p), nil
}
