package main

import (
	"context"
	"io"
	"os"
	"time"

	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	inframedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// media_wiring.go composes the WP-11 stack: the media engine, the subtitle service, the timeline read
// and the export service.
//
// # Why it is a separate file from the drama and agent wirings
//
// It needs both and belongs to neither. The export reads a storyboard and a script (the drama stack),
// stores its output through the file store (the project stack), and starts a subprocess (the media
// infrastructure). Composing it anywhere else would mean passing three of those across a module
// boundary, and it must run AFTER the drama stack because the timeline's reads join tables that stack
// owns.
//
// # The save path is injected rather than discovered
//
// `SaveFile` needs a dialog, which needs a Wails context, which exists only after startup. So the save
// function is a FIELD the composition root fills in once the context exists, and a build that never
// fills it leaves that one method failing closed with a reason rather than panicking on a nil window.
// It is the same pattern the agent binding's registry attach uses, and for the same reason.
//
// # A machine with no ffmpeg still composes
//
// The engine reports itself unavailable and the binding says so with a diagnostic. Composition does not
// fail, because a build with no engine can still draft subtitles and read a timeline — disabling the
// whole section would hide the parts that work. ARCHITECTURE: "媒体引擎不可用：禁用相关能力并显示诊断".
type mediaWiring struct {
	engine    *inframedia.FFmpegEngine
	subtitles *appmedia.SubtitleService
	timeline  *appmedia.TimelineService
	exports   *appmedia.ExportService
	saveFile  func(ctx context.Context, suggestedName string, write func(io.Writer) error) (string, error)
}

// composeMedia builds the media stack over a writable database and the project's file store.
//
// It returns nil when the database or the file store is unavailable, so the media binding stays
// unattached and every method fails closed.
func composeMedia(handle *database.Handle, script *appscript.Service, store *filestore.Store, tempDir string) *mediaWiring {
	if handle == nil || handle.SQL() == nil || script == nil || store == nil || tempDir == "" {
		return nil
	}
	connection := handle.SQL()
	engine := inframedia.NewFFmpegEngine(tempDir)
	subtitleRepository := database.NewSubtitleRepository(connection)
	exportRepository := database.NewExportRepository(connection)
	clock := mediaClock{}
	ids := id.NewGenerator()

	subtitleService := appmedia.NewSubtitleService(appmedia.SubtitleOptions{
		Tracks: subtitleRepository,
		// The line reader is the desktop adapter, because the two facts it needs come from the script
		// service and the media package must not import the script domain.
		Lines: desktop.NewSpokenLineReader(script),
		Clock: clock,
		IDs:   ids,
	})
	timelineService := appmedia.NewTimelineService(appmedia.TimelineOptions{Board: exportRepository})
	exportService := appmedia.NewExportService(appmedia.ExportOptions{
		Timeline: *timelineService,
		Engine:   engine,
		// The file store writes BYTES and the repository records the row they are addressable by. The
		// pair is what `asset_files.file_hash` has a foreign key to, so a composed export without the
		// row could not be cited by anything.
		Files:    mediaFileStore{store: store, repository: database.NewFileRepository(connection)},
		Exports:  exportRepository,
		Temp:     mediaTempDir{dir: tempDir},
		Subtitle: subtitleService,
		Clock:    clock,
		IDs:      ids,
	})
	return &mediaWiring{
		engine: engine, subtitles: subtitleService, timeline: timelineService, exports: exportService,
	}
}

// attach supplies the save path once a Wails context exists.
func (w *mediaWiring) attach(binding *desktop.MediaBinding, ctx context.Context) {
	if w == nil || binding == nil {
		return
	}
	desktop.AttachMedia(binding, ctx, w.exports, w.subtitles, w.timeline, w.saveFile)
}

// mediaClock is the application's time source, which every wiring here uses.
type mediaClock struct{}

func (mediaClock) Now() time.Time { return time.Now().UTC() }

// mediaFileStore is the export service's file port over the real content-addressed store.
//
// It writes the metadata row in the same call as the bytes, which is the pairing the file service
// makes: a store write without the row leaves a file nothing can reference.
type mediaFileStore struct {
	store      *filestore.Store
	repository *database.FileRepository
}

func (f mediaFileStore) Put(ctx context.Context, displayName string, body io.Reader) (appmedia.StoredObject, error) {
	object, err := f.store.Put(ctx, displayName, body)
	if err != nil {
		return appmedia.StoredObject{}, err
	}
	if err := f.repository.UpsertObject(ctx, appfiles.Object{
		Hash: object.Hash, StorageKey: object.StorageKey, MIME: object.MIME, Size: object.Size,
	}); err != nil {
		return appmedia.StoredObject{}, err
	}
	return appmedia.StoredObject{
		Hash: object.Hash, StorageKey: object.StorageKey, MIME: object.MIME, Size: object.Size,
	}, nil
}

func (f mediaFileStore) Open(ctx context.Context, storageKey string) (io.ReadCloser, error) {
	return f.store.Open(ctx, storageKey)
}

// mediaTempDir is the export service's scratch directory port.
//
// It is a separate type from the FileStore's own temporary directory even though both live under the
// same root, because the two have different lifetimes: the store's files are transient per put, and an
// export's directory holds a film's intermediates until the composition finishes.
type mediaTempDir struct{ dir string }

func (t mediaTempDir) NewScratchDir(_ context.Context, prefix string) (string, error) {
	return os.MkdirTemp(t.dir, prefix)
}

func (t mediaTempDir) RemoveScratchDir(path string) error {
	return os.RemoveAll(path)
}

// The compile-time proofs that the two adapters satisfy the ports the export service declares.
var (
	_ appmedia.FileStore = mediaFileStore{}
	_ appmedia.TempDir   = mediaTempDir{}
)
