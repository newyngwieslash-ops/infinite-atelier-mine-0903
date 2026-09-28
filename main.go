package main

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/appdirs"
	applicationlogging "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/logging"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const singleInstanceID = "9fe98766-f174-4dbf-9f76-3dd1ff6642ea"

const fallbackShutdownTimeout = 5 * time.Second

//go:embed all:web/dist
var assets embed.FS

func main() {
	if err := run(); err != nil {
		logStartupError(err)
		os.Exit(1)
	}
}

func run() error {
	dirs, err := appdirs.Ensure("")
	if err != nil {
		return err
	}

	logger, logCloser, err := applicationlogging.Open(dirs.Logs)
	if err != nil {
		return err
	}

	reportIndependent := func(err *apperror.Error) {
		logApplicationError(stderrLogger(), err)
	}

	application := newAppWithLogger(nil, logger)
	application.dirs = dirs
	application.health = &desktop.HealthBinding{}
	application.secretsBinding = &desktop.SecretsBinding{}
	application.providersBinding = &desktop.ProvidersBinding{}
	application.jobsBinding = &desktop.JobsBinding{}
	application.projectsBinding = &desktop.ProjectsBinding{}
	application.legacyUploadBinding = desktop.NewLegacyUploadBinding()
	application.backupBinding = &desktop.BackupBinding{}
	application.settingsBinding = &desktop.SettingsBinding{}
	// FR-180's diagnostics bundle. Declared here like the others so it exists on the binding
	// surface; its service is attached only when startup composes a writable database, because every
	// section it carries is read from the database or the log file.
	application.diagnosticsBinding = &desktop.DiagnosticsBinding{}
	// The WP-05 drama surface. Like the others it is declared before Wails
	// starts so it exists on the binding surface; its services are attached
	// only when startup composes a writable database.
	application.dramaBinding = &desktop.DramaBinding{}
	application.importBinding = &desktop.ImportBinding{}
	application.importUploadBinding = &desktop.ImportUploadBinding{}
	// The WP-07 agent surface: the Agent Center's run views and the agent
	// inventory. Its services are attached only when startup composes a
	// writable database, so a safe-mode build lists its agents and shows no
	// runs — the agents exist in the binary either way.
	application.agentBinding = &desktop.AgentBinding{}
	application.memoryBinding = &desktop.MemoryBinding{}
	application.mediaBinding = &desktop.MediaBinding{}
	application.assetsBinding = &desktop.AssetsBinding{}
	// The previs snapshot surface (WP-21). It is declared here so the binding exists on the Wails
	// surface from startup; its store is attached only when the composition root builds one, so a
	// safe-mode build refuses a snapshot rather than panicking.
	application.monoformBinding = &desktop.MonoformBinding{}
	// The background-music import (WP-29). Declared here so the binding exists from startup; its importer
	// is attached only when the composition root builds one, so a safe-mode build refuses an import rather
	// than panicking.
	application.musicBinding = &desktop.MusicImportBinding{}
	application.emit = wailsruntime.EventsEmit
	shutdown := newShutdownSequence(application.closeDatabase, logger, logCloser.Close, reportIndependent)
	application.shutdown = shutdown
	defer runFallbackShutdown(shutdown, reportIndependent)

	err = wails.Run(&options.App{
		Title:            "源铭振跃",
		Width:            1280,
		Height:           800,
		Frameless:        false,
		WindowStartState: options.Normal,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  application.startup,
		OnDomReady: application.domReady,
		OnShutdown: application.close,
		// The secret/provider bindings are always declared, but their services
		// are attached only when startup composes a writable database. In safe
		// mode they return a fail-closed unavailable error and expose no
		// Resolve method.
		Bind: []interface{}{
			application.health,
			application.secretsBinding,
			application.providersBinding,
			application.jobsBinding,
			application.projectsBinding,
			application.legacyUploadBinding,
			application.backupBinding,
			application.settingsBinding,
			application.diagnosticsBinding,
			application.dramaBinding,
			application.assetsBinding,
			application.importBinding,
			application.importUploadBinding,
			application.agentBinding,
			application.memoryBinding,
			application.mediaBinding,
			application.monoformBinding,
			application.musicBinding,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: singleInstanceID,
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				application.focusExistingWindow()
			},
		},
	})
	if err != nil {
		runtimeError := apperror.New(
			"DESKTOP_RUNTIME_FAILED",
			"internal",
			false,
			"Desktop runtime could not be started.",
			err,
		)
		logApplicationError(logger, runtimeError)
		return runtimeError
	}
	return nil
}

func logStartupError(err error) {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		appErr = apperror.New(
			"STARTUP_FAILED",
			"internal",
			false,
			"Application startup could not be completed.",
			err,
		)
	}
	logApplicationError(stderrLogger(), appErr)
}

func stderrLogger() *slog.Logger {
	return slog.New(applicationlogging.NewRedactingJSONHandler(os.Stderr, nil))
}

func runFallbackShutdown(shutdown func(context.Context) error, reportIndependent func(*apperror.Error)) {
	if shutdown == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fallbackShutdownTimeout)
	defer cancel()
	if err := shutdown(ctx); err != nil && reportIndependent != nil {
		reportIndependent(apperror.New(
			"APP_FALLBACK_SHUTDOWN_FAILED",
			"internal",
			false,
			"Application fallback shutdown did not complete cleanly.",
			err,
		))
	}
}
