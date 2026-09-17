package main

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	appbackup "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"
	applegacy "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/legacy"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/buildinfo"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/appdirs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// projectWiring holds the composed WP-04 stack: the project service the canvas
// uses, the legacy import pipeline, the backup pair, and the chunked upload
// channel a migrating browser hands its media to.
type projectWiring struct {
	projects    *appprojects.Service
	imports     *applegacy.Service
	importStore *database.LegacyRepository
	export      *appbackup.ExportService
	restore     *appbackup.RestoreService
	upload      *desktop.LegacyUploadBinding
	backup      *desktop.BackupBinding
	// backupStore is both the export source and the restore sink.
	backupStore *database.BackupStore
	binding     *desktop.ProjectsBinding
}

// composeProjects builds the project, import and backup stack over a writable
// database. It returns nil when the database is unavailable so the binding
// stays unattached and fails closed.
// composeProjects builds the stack. The upload binding is supplied by the
// caller because it is declared before Wails startup; this function only
// configures it, so the Wails surface is the same object either way.
func composeProjects(handle *database.Handle, dirs appdirs.Dirs, store *filestore.Store, uploadBinding *desktop.LegacyUploadBinding, backupBinding *desktop.BackupBinding) *projectWiring {
	if handle == nil || handle.SQL() == nil {
		return nil
	}
	connection := handle.SQL()
	ids := id.NewGenerator()
	clock := appprojectsClock{}

	projectService := appprojects.NewService(appprojects.Options{
		Projects: database.NewProjectRepository(connection),
		Canvas:   database.NewCanvasRepository(connection),
		Clock:    clock,
		IDs:      ids,
	})

	// The import pipeline needs a file committer (bytes into the FileStore) and
	// a media source (the bytes the browser uploaded). The upload binding is
	// both: it commits what it receives and reads it back for the import.
	committer := resultCommitter{store: store, repo: database.NewFileRepository(connection)}
	if uploadBinding == nil {
		uploadBinding = desktop.NewLegacyUploadBinding()
	}
	desktop.ConfigureLegacyUpload(uploadBinding, dirs.Temp, committer, store)
	importStore := database.NewLegacyRepository(connection)
	importService := applegacy.NewService(applegacy.Options{
		Store: importStore,
		Files: committer,
		Media: desktop.LegacyUploadMediaSource(uploadBinding),
		IDs:   ids,
		Clock: clock,
	})

	backupStore := database.NewBackupStore(connection, dirs.Files, dirs.Temp, dirs.Snapshots, buildinfo.Version)
	exportService := appbackup.NewExportService(appbackup.ExportOptions{
		Source: backupStore, Clock: clock, AppVersion: buildinfo.Version, WorkDir: dirs.Temp,
	})
	restoreService := appbackup.NewRestoreService(backupStore)

	return &projectWiring{
		projects:    projectService,
		imports:     importService,
		importStore: importStore,
		export:      exportService,
		restore:     restoreService,
		upload:      uploadBinding,
		backup:      backupBinding,
		backupStore: backupStore,
	}
}

// attach wires the composed stack into the bindings.
func (w *projectWiring) attach(ctx context.Context) {
	if w == nil {
		return
	}
	if w.binding != nil {
		desktop.AttachProjects(w.binding, ctx, w.projects)
		desktop.AttachProjectImport(w.binding, &importAdapter{service: w.imports, store: w.importStore})
	}
	if w.upload != nil {
		desktop.AttachLegacyUpload(w.upload, ctx)
	}
	if w.backup != nil {
		// The store is both the export source and the restore sink: the export
		// reads through it and a restore stages into its private temp area.
		desktop.AttachBackup(w.backup, ctx, w.export, w.restore, w.backupStore)
	}
}

// importAdapter adapts the import service to the JSON-shaped binding surface.
//
// The binding exchanges JSON text because the snapshot is a document the
// frontend extracted; this adapter is where that text becomes the typed
// snapshot the service works with, and where the result becomes text again.
type importAdapter struct {
	service *applegacy.Service
	store   *database.LegacyRepository
}

// Precheck validates a snapshot and reports its scope as JSON.
func (a *importAdapter) Precheck(ctx context.Context, snapshotJSON string) (string, error) {
	if a == nil || a.service == nil {
		return "", desktop.ProjectBindingUnavailable()
	}
	var snapshot applegacy.Snapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		return "", desktop.ProjectImportInvalid()
	}
	precheck, err := a.service.Precheck(ctx, snapshot)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(precheck)
	if err != nil {
		return "", desktop.ProjectImportInvalid()
	}
	return string(encoded), nil
}

// Import writes a snapshot atomically and reports the result as JSON.
func (a *importAdapter) Import(ctx context.Context, snapshotJSON, mode, sourceCase, legacyRoot string) (string, error) {
	if a == nil || a.service == nil {
		return "", desktop.ProjectBindingUnavailable()
	}
	var snapshot applegacy.Snapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		return "", desktop.ProjectImportInvalid()
	}
	result, err := a.service.Import(ctx, snapshot, applegacy.ImportOptions{
		Mode: mode, SourceCase: sourceCase, LegacyRoot: legacyRoot,
	})
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", desktop.ProjectImportInvalid()
	}
	return string(encoded), nil
}

// ListImports returns recent runs as JSON.
func (a *importAdapter) ListImports(ctx context.Context, limit int) (string, error) {
	if a == nil || a.service == nil {
		return "", desktop.ProjectBindingUnavailable()
	}
	records, err := a.store.ListImports(ctx, limit)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return "", desktop.ProjectImportInvalid()
	}
	return string(encoded), nil
}

// resultCommitter writes bytes into the content-addressed FileStore and records
// the object metadata, which is the order the pipeline requires (bytes, then
// metadata, then a reference).
type resultCommitter struct {
	store *filestore.Store
	repo  *database.FileRepository
}

// CommitLegacyFile stores one legacy blob and returns its content hash.
func (c resultCommitter) CommitLegacyFile(ctx context.Context, legacyKey, mimeType string, content []byte) (string, error) {
	// The display name is a neutral placeholder: the FileStore assigns the real
	// content-addressed key, and a browser-supplied key must never become a path.
	object, err := c.store.Put(ctx, "legacy-media.bin", bytes.NewReader(content))
	if err != nil {
		return "", err
	}
	if err := c.repo.UpsertObject(ctx, object); err != nil {
		return "", err
	}
	return object.Hash, nil
}

// appprojectsClock adapts the wall clock to the services' Clock ports.
type appprojectsClock struct{}

func (appprojectsClock) Now() time.Time { return time.Now().UTC() }

// cleanStaging removes any restore staging left by an interrupted attempt.
//
// It runs at startup: a restore that failed or was interrupted leaves a copy of
// an archive's database and files in the private temp area, and nothing else
// would remove it (AC-BACKUP-002's temporary-directory cleanup).
func (w *projectWiring) cleanStaging() {
	if w == nil || w.backupStore == nil {
		return
	}
	_ = w.backupStore.CleanStaging()
}
