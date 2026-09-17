package main

import (
	"context"
	"log/slog"
	"sync"

	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/health"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/buildinfo"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/appdirs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	infrajobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/jobs"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type app struct {
	mu          sync.RWMutex
	ctx         context.Context
	shutdown    func(context.Context) error
	focusWindow func(context.Context)
	logger      *slog.Logger
	dirs        appdirs.Dirs
	db          *database.Handle
	filesStore  *filestore.Store
	files       *appfiles.Service
	health      *desktop.HealthBinding
	// secretsBinding and providersBinding are declared before Wails startup so
	// they exist on the binding surface; their services are attached only when
	// startup composes a writable database.
	secretsBinding   *desktop.SecretsBinding
	providersBinding *desktop.ProvidersBinding
	jobsBinding      *desktop.JobsBinding
	// projectsBinding and legacyUploadBinding are the WP-04 surface: the canvas
	// reads and writes through the first, and a legacy blob travels through the
	// second.
	projectsBinding     *desktop.ProjectsBinding
	legacyUploadBinding *desktop.LegacyUploadBinding
	backupBinding       *desktop.BackupBinding
	providerWiring      *providerWiring
	jobWiring           *jobWiring
	projectWiring       *projectWiring
	emit                func(context.Context, string, ...interface{})
	newEnvelope         func(string, any) (desktop.Envelope, error)
}

func newApp(shutdown func(context.Context) error) *app {
	return newAppWithWindowFocusAndLogger(shutdown, focusWailsWindow, slog.Default())
}

func newAppWithWindowFocus(shutdown func(context.Context) error, focusWindow func(context.Context)) *app {
	return newAppWithWindowFocusAndLogger(shutdown, focusWindow, slog.Default())
}

func newAppWithLogger(shutdown func(context.Context) error, logger *slog.Logger) *app {
	return newAppWithWindowFocusAndLogger(shutdown, focusWailsWindow, logger)
}

func newAppWithWindowFocusAndLogger(shutdown func(context.Context) error, focusWindow func(context.Context), logger *slog.Logger) *app {
	return &app{
		shutdown:    shutdown,
		focusWindow: focusWindow,
		logger:      logger,
		newEnvelope: desktop.NewEnvelope,
	}
}

func (a *app) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	dirs := a.dirs
	healthBinding := a.health
	a.mu.Unlock()

	desktop.Attach(healthBinding, ctx, health.New(buildinfo.Version, dirs.Root, nil, true, "startup_unavailable"))
	if dirs.Database == "" {
		return
	}
	handle, err := database.Open(ctx, dirs.Database, dirs.Snapshots)
	if err != nil {
		appErr := apperror.New(
			"DATABASE_OPEN_FAILED",
			"storage",
			false,
			"The local database could not be opened.",
			err,
		)
		desktop.Attach(healthBinding, ctx, health.New(buildinfo.Version, dirs.Root, nil, true, appErr.Diagnostic))
		logApplicationError(a.logger, appErr)
		return
	}
	if handle.Mode() == database.ModeSafe && handle.Err() != nil {
		logApplicationError(a.logger, handle.Err())
	}
	a.mu.Lock()
	a.db = handle
	a.mu.Unlock()
	store, err := filestore.New(dirs.Files, dirs.Temp)
	if err != nil {
		appErr := apperror.New(
			"FILE_WRITE_FAILED",
			"storage",
			false,
			"The file store could not be prepared.",
			err,
		)
		desktop.Attach(healthBinding, ctx, health.New(buildinfo.Version, dirs.Root, nil, true, appErr.Diagnostic))
		logApplicationError(a.logger, appErr)
		return
	}
	a.mu.Lock()
	a.filesStore = store
	if handle.SQL() != nil {
		a.files = appfiles.NewService(store, database.NewFileRepository(handle.SQL()))
		publisher := &eventPublisher{emit: a.emit, newEnvelope: a.newEnvelope}
		wiring := composeProviders(handle.SQL(), publisher)
		if wiring != nil {
			if a.secretsBinding != nil {
				wiring.secretsBinding = a.secretsBinding
			}
			if a.providersBinding != nil {
				wiring.providersBinding = a.providersBinding
			}
			wiring.attach(ctx)
			a.providerWiring = wiring

			// The job manager shares the provider registry so one policy,
			// secret store and audit trail governs every outbound call.
			fileRepository := database.NewFileRepository(handle.SQL())
			results := infrajobs.NewResultStore(store, fileRepository, database.NewFileReferenceRepository(handle.SQL()))
			downloader := phttp.NewDownloader(phttp.NewNetResolver(), phttp.DownloadLimits{MaxBytes: maxJobResultBytes})
			jobStack := composeJobs(handle.SQL(), wiring.registry, results, downloader, publisher, store)
			if jobStack != nil {
				if a.jobsBinding != nil {
					jobStack.binding = a.jobsBinding
				}
				jobStack.attach(ctx)
				a.jobWiring = jobStack
			}

			// The WP-04 stack shares the same database and FileStore, so a
			// migrated project's media lands in the same content-addressed store
			// the job pipeline writes to.
			projectStack := composeProjects(handle, dirs, store, a.legacyUploadBinding, a.backupBinding)
			// A restore stages into the private temp area; a copy left by an
			// interrupted attempt is removed at startup rather than accumulating.
			if projectStack != nil {
				projectStack.cleanStaging()
			}
			if projectStack != nil {
				if a.projectsBinding != nil {
					projectStack.binding = a.projectsBinding
				}
				projectStack.attach(ctx)
				a.projectWiring = projectStack
			}
		}
	}
	diagnostic := ""
	if handle.Err() != nil {
		diagnostic = handle.Err().Diagnostic
	}
	var probe health.Probe
	if db := handle.SQL(); db != nil {
		probe = db
	}
	desktop.Attach(healthBinding, ctx, health.New(buildinfo.Version, dirs.Root, probe, handle.Mode() == database.ModeSafe, diagnostic))
	jobStack := a.jobWiring
	a.mu.Unlock()

	// Start the job manager outside the lock: the recovery scan touches the
	// database and must not hold up the rest of startup.
	if jobStack != nil {
		jobStack.start(ctx)
	}

	if a.logger != nil {
		a.logger.Info(
			"desktop core started",
			slog.String("component", "desktop"),
			slog.String("database", string(handle.Mode())),
		)
	}
}

func (a *app) domReady(ctx context.Context) {
	if a.emit == nil || a.health == nil || a.newEnvelope == nil {
		return
	}
	event, err := a.newEnvelope("health.changed", a.health.Get())
	if err != nil {
		logApplicationError(a.logger, apperror.New(
			"EVENT_EMIT_FAILED",
			"internal",
			false,
			"Desktop health status could not be announced.",
			err,
		))
		return
	}
	a.emit(ctx, desktop.CoreEventName, event)
}

func (a *app) close(ctx context.Context) {
	a.mu.Lock()
	a.ctx = nil
	shutdown := a.shutdown
	a.mu.Unlock()

	if shutdown != nil {
		if err := shutdown(ctx); err != nil {
			logApplicationError(a.logger, apperror.New(
				"APP_SHUTDOWN_FAILED",
				"internal",
				false,
				"Application shutdown did not complete cleanly.",
				err,
			))
		}
	}
}

func (a *app) closeDatabase(ctx context.Context) error {
	a.mu.Lock()
	handle := a.db
	wiring := a.providerWiring
	jobStack := a.jobWiring
	a.db = nil
	a.providerWiring = nil
	a.jobWiring = nil
	a.mu.Unlock()
	// Stop the job scheduler first, then cancel in-flight provider streams,
	// so no worker writes into a closed database handle.
	if jobStack != nil {
		jobStack.shutdown()
	}
	if wiring != nil {
		wiring.shutdown()
	}
	if handle == nil {
		return nil
	}
	return handle.Close(ctx)
}

func (a *app) focusExistingWindow() {
	a.mu.RLock()
	ctx := a.ctx
	focusWindow := a.focusWindow
	a.mu.RUnlock()

	if ctx == nil || focusWindow == nil {
		return
	}

	focusWindow(ctx)
}

func focusWailsWindow(ctx context.Context) {
	wailsruntime.WindowUnminimise(ctx)
	wailsruntime.WindowShow(ctx)
}

func logApplicationError(logger *slog.Logger, err *apperror.Error) {
	if logger == nil || err == nil {
		return
	}
	logger.Error(
		err.SafeMessage,
		slog.String("error_code", err.Code),
		slog.String("category", err.Category),
		slog.Bool("retriable", err.Retriable),
		slog.String("diagnostic", err.Diagnostic),
	)
}

// newShutdownSequence owns ordered process cleanup. It intentionally handles only
// lifecycle resources; it is not a general dependency container.
func newShutdownSequence(
	shutdownResources func(context.Context) error,
	logger *slog.Logger,
	closeLogger func() error,
	reportIndependent func(*apperror.Error),
) func(context.Context) error {
	var once sync.Once
	return func(ctx context.Context) error {
		once.Do(func() {
			if shutdownResources != nil {
				if err := shutdownResources(ctx); err != nil {
					logApplicationError(logger, apperror.New(
						"APP_RESOURCE_SHUTDOWN_FAILED",
						"internal",
						false,
						"Application resources did not close cleanly.",
						err,
					))
				}
			}
			if closeLogger != nil {
				if err := closeLogger(); err != nil && reportIndependent != nil {
					reportIndependent(apperror.New(
						"LOG_CLOSE_FAILED",
						"internal",
						false,
						"Application logging did not close cleanly.",
						err,
					))
				}
			}
		})
		return nil
	}
}
