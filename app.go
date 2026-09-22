package main

import (
	"context"
	"log/slog"
	"sync"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/health"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
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
	// dramaBinding and assetsBinding are the WP-05 surface: the studio reads and
	// writes the drama aggregates through the first, and the asset bible
	// through the second.
	dramaBinding  *desktop.DramaBinding
	assetsBinding *desktop.AssetsBinding
	// importBinding is the WP-06 surface: document import, chapter confirmation
	// and event extraction. importUploadBinding carries a document from the
	// webview in bounded chunks, so a large novel does not cross as one message.
	importBinding       *desktop.ImportBinding
	importUploadBinding *desktop.ImportUploadBinding
	// agentBinding is the WP-07 surface. It is declared before Wails starts so it
	// exists on the binding surface, and its services are attached only when the
	// agent stack composes.
	agentBinding *desktop.AgentBinding
	// agentStack is the composed WP-07 runtime, held so a later package can reach
	// the extraction service the runtime is the Extractor for.
	agentStack     *agentWiring
	providerWiring *providerWiring
	jobWiring      *jobWiring
	projectWiring  *projectWiring
	dramaWiring    *dramaWiring
	emit           func(context.Context, string, ...interface{})
	newEnvelope    func(string, any) (desktop.Envelope, error)
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

			// The WP-05 drama stack shares the same database. It is composed
			// separately from the project stack because it owns its own
			// services, but it fails closed on the same condition: a handle in
			// safe mode has no SQL pool, so composeDrama returns nil and every
			// drama method reports unavailable.
			// The canvas writer travels from the project stack into the drama stack, so the script
			// service's projection command has an implementation in a real build. Without it every
			// projection refuses, and the refusal is invisible until a user asks for one.
			var canvasWriter *appprojects.Service
			if a.projectWiring != nil {
				canvasWriter = a.projectWiring.projects
			}
			dramaStack := composeDrama(handle, store, canvasWriter)
			if dramaStack != nil {
				if a.dramaBinding != nil {
					dramaStack.dramaBinding = a.dramaBinding
				}
				if a.assetsBinding != nil {
					dramaStack.assetsBinding = a.assetsBinding
				}
				if a.importBinding != nil {
					dramaStack.importBinding = a.importBinding
				}
				if a.importUploadBinding != nil {
					dramaStack.importUploadBinding = a.importUploadBinding
				}
				dramaStack.attach(ctx)
				a.dramaWiring = dramaStack

				// The WP-07 agent stack is composed AFTER the drama stack, because
				// every tool's handler calls one of its services: the order here is
				// the dependency direction, made visible in one place.
				// The job service is passed so the production pipeline's image batch can
				// submit: the batch is a Job command rather than an agent stage, and the
				// job stack is composed above this point.
				var jobService *appjobs.Service
				if jobStack := a.jobWiring; jobStack != nil {
					jobService = jobStack.service
				}
				agentStack := composeAgents(agentDeps{
					Handle: handle, Drama: dramaStack,
					Providers: wiring.registry, Files: a.files,
					Jobs: jobService,
				})
				if agentStack != nil {
					if a.agentBinding != nil {
						desktop.AttachAgent(a.agentBinding, ctx,
							agentruntime.NewInspector(database.NewAgentRepository(handle.SQL())))
						desktop.AttachAgentRegistry(a.agentBinding, agentStack.assembly.Registry())
					}
					// The extraction service is REBUILT with the runtime as its Extractor and
					// re-attached, which is what closes the seam WP-06 recorded: "如果 Agent
					// Runtime 尚未完成，Event Extraction 先通过明确的 Application Service +
					// Mock/Provider 适配实现，WP-07 再接入统一 Runtime".
					//
					// Composing it and not attaching it was the defect an independent review
					// found: the drama stack's extractor-less service stayed on the binding, so
					// the runtime's Extractor implementation had no call site in a production
					// build — the "interface with no real path" AGENTS section 12 refuses. The
					// compile-time assertion proved the port was SATISFIED, not that anything
					// used it.
					//
					// The attachment goes through the drama stack's own binding, which is the
					// object the frontend already calls.
					if a.importBinding != nil {
						desktop.AttachExtraction(a.importBinding, ctx, agentStack.ExtractionService(dramaStack))
					}
					// The script pipeline is attached to the DRAMA binding, because that is the object the
					// frontend already calls for episodes and scripts. It is attached here rather than in
					// `dramaStack.attach` because it is composed by the agent stack — which is built after
					// the drama stack, since every tool's handler calls one of its services.
					//
					// Without this the pipeline existed, was tested, and was unreachable: WP-08's stage
					// commands had no caller, which is the same shape of gap as the canvas projector.
					if dramaStack != nil && a.dramaBinding != nil {
						desktop.AttachPipeline(a.dramaBinding, ctx, agentStack.Pipeline())
						// The production pipeline goes to the SAME binding, in its own
						// slot: the two answer for disjoint stage sets and the commands
						// route by the stage they were given, so one surface serves both
						// layers.
						desktop.AttachProductionPipeline(a.dramaBinding, ctx, agentStack.Production())
						// The batch goes to the same binding in its own slot: it needs the job
						// store, which neither pipeline does, so a build can run the five agent
						// stages without it and say so when a batch is asked for.
						desktop.AttachProductionBatch(a.dramaBinding, ctx, agentStack.Production())
					}
					a.agentStack = agentStack
				}
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
