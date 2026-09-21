package main

import (
	"context"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appevents "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	appextraction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/extraction"
	appimporting "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/importing"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstaleness "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/staleness"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// dramaWiring holds the composed WP-05 stack: the five drama services, the
// staleness propagator, and the assets service WP-04 declared but never
// composed.
//
// It is composed only over a writable database, so a database in safe mode
// leaves every binding unattached and every method failing closed.
type dramaWiring struct {
	story      *appstory.Service
	script     *appscript.Service
	storyboard *appstoryboard.Service
	workflow   *appworkflow.Service
	staleness  *appstaleness.Service
	assets     *appassets.Service
	events     *appevents.Service
	importing  *appimporting.Service
	extraction *appextraction.Service

	dramaBinding  *desktop.DramaBinding
	assetsBinding *desktop.AssetsBinding
	// importBinding is the WP-06 surface: document import and event extraction.
	// importUploadBinding is the chunked transfer that feeds it, because a Wails
	// message carrying a whole 100k-character novel as a byte array is work
	// proportional to the document on the webview's main thread.
	importBinding       *desktop.ImportBinding
	importUploadBinding *desktop.ImportUploadBinding
}

// composeDrama builds the drama stack over a writable database. It returns nil
// when the database is unavailable so the bindings stay unattached.
//
// One repository instance per family serves every port that family declares:
// the story repository satisfies all six of the story aggregate's read and
// write interfaces, the storyboard repository satisfies its four, and so on.
// They share the single connection the handle owns.
// composeDrama builds the drama stack over a writable database. It returns nil
// when the database is unavailable so the bindings stay unattached.
//
// `canvas` is the projects service, supplied by the composition root, and it is what the script service's
// projection command writes through. It comes from the CALLER rather than being built here, and that is a
// dependency decision: the projects service is composed by `composeProjects`, and a second instance over
// the same connection would be a second answer to "what is a projection" — the relation registry's
// validation lives in that service, so two of them could disagree about what may be projected.
//
// A nil canvas leaves the projector UNCOMPOSED, and the script service then refuses every projection rather
// than reporting a silent no-op. That is the honest state for a build whose project stack failed to
// compose, and the refusal names it.
//
// One repository instance per family serves every port that family declares:
// the story repository satisfies all six of the story aggregate's read and
// write interfaces, the storyboard repository satisfies its four, and so on.
// They share the single connection the handle owns.
func composeDrama(handle *database.Handle, store *filestore.Store, canvas *appprojects.Service) *dramaWiring {
	if handle == nil || handle.SQL() == nil {
		return nil
	}
	connection := handle.SQL()
	ids := id.NewGenerator()
	clock := appprojectsClock{}

	storyRepository := database.NewStoryRepository(connection)
	scriptRepository := database.NewScriptRepository(connection)
	storyboardRepository := database.NewStoryboardRepository(connection)
	workflowRepository := database.NewWorkflowRepository(connection)
	stalenessRepository := database.NewStalenessRepository(connection)

	// The event stream is composed first because three of the services below
	// record into it: the approvals that must write their governance event in
	// the same transaction as the change they describe.
	eventService := appevents.NewService(appevents.Options{
		Repository: database.NewEventRepository(connection),
		Clock:      clock,
		IDs:        ids,
	})

	// The story and import services are built as locals because three of the
	// services below are composed over them: extraction reads a chapter through
	// the import service, and the reader adapter needs both.
	storyService := appstory.NewService(appstory.Options{
		Repository: storyRepository,
		Clock:      clock,
		IDs:        ids,
		Events:     eventService,
	})
	importingService := appimporting.NewService(appimporting.Options{
		Store:  desktop.NewDocumentStore(store),
		Story:  storyService,
		Events: eventService,
		Clock:  clock,
	})

	return &dramaWiring{
		story: storyService,
		script: appscript.NewService(appscript.Options{
			Repository: scriptRepository,
			Clock:      clock,
			IDs:        ids,
			Events:     eventService,
			Projector:  canvasProjectorFor(canvas),
		}),
		storyboard: appstoryboard.NewService(appstoryboard.Options{
			DirectorPlans: storyboardRepository,
			Storyboards:   storyboardRepository,
			Items:         storyboardRepository,
			Panels:        storyboardRepository,
			Clock:         clock,
			IDs:           ids,
			Events:        eventService,
		}),
		workflow: appworkflow.NewService(appworkflow.Options{
			Runs:      workflowRepository,
			Stages:    workflowRepository,
			Reviews:   workflowRepository,
			Decisions: workflowRepository,
			Events:    workflowRepository,
			Clock:     clock,
			IDs:       ids,
			Recorder:  eventService,
		}),
		// The propagator needs the finder to resolve references and the project
		// resolver to keep a mark inside the project that asked for it; both
		// read the same connection.
		staleness: appstaleness.NewService(appstaleness.Options{
			Marks:      stalenessRepository,
			Dependents: database.NewDependentFinder(connection),
			Projects:   database.NewProjectResolver(connection),
			Clock:      clock,
			IDs:        ids,
			Events:     eventService,
		}),
		// WP-04 shipped this service with no composition root, recording that
		// it "exists for WP-05". This is that root.
		assets: appassets.NewService(appassets.Options{
			Repository: database.NewAssetRepository(connection),
			Clock:      clock,
			IDs:        ids,
			Events:     eventService,
		}),
		events:    eventService,
		importing: importingService,
		// The extractor is deliberately absent. There is no default: a build
		// without one refuses extraction with a reason instead of inventing
		// facts, and WP-07 supplies the implementation of the port.
		extraction: appextraction.NewService(appextraction.Options{
			Reader: desktop.NewChapterTextReader(storyService, importingService),
			Story:  storyService,
			Clock:  clock,
			IDs:    ids,
		}),
	}
}

// attach wires the composed stack into the bindings.
func (w *dramaWiring) attach(ctx context.Context) {
	if w == nil {
		return
	}
	if w.dramaBinding != nil {
		desktop.AttachStory(w.dramaBinding, ctx, w.story)
		desktop.AttachScript(w.dramaBinding, ctx, w.script)
		desktop.AttachStoryboard(w.dramaBinding, ctx, w.storyboard)
		desktop.AttachWorkflow(w.dramaBinding, ctx, w.workflow)
		desktop.AttachStaleness(w.dramaBinding, ctx, w.staleness)
		desktop.AttachDomainEvents(w.dramaBinding, ctx, w.events)
	}
	if w.assetsBinding != nil {
		desktop.AttachAssets(w.assetsBinding, ctx, w.assets)
	}
	if w.importBinding != nil {
		desktop.AttachImporting(w.importBinding, ctx, w.importing)
		desktop.AttachExtraction(w.importBinding, ctx, w.extraction)
		if w.importUploadBinding != nil {
			desktop.AttachImportUpload(w.importUploadBinding, ctx, w.importBinding)
		}
	}
}

// canvasProjector adapts the projects service's canvas writer to the script service's port.
//
// IT EXISTS BECAUSE THE PORT HAD NO PRODUCTION IMPLEMENTATION.  WP-08 wrote `ProjectScriptVersion` with an
// interface and a refusal for a build without one, and `composeDrama` supplied nothing — so every
// projection from the desktop refused with "this build cannot project onto a canvas", and
// `CanvasProjectionCreated` never fired.  The canary did not catch it because the canary's assertion used
// a TEST DOUBLE: a double that satisfies the port cannot notice that production does not compose one.
// That is the shape of gap a wiring change closes and a unit test cannot.
//
// The adapter is thin on purpose. The projects service owns what a projection IS — the relation registry's
// validation, the create-or-move semantics, the node's box — and this only translates one projector call
// into that command.
type canvasProjector struct {
	projects *appprojects.Service
}

// canvasProjectorFor returns the projector for a canvas writer, or nil when there is none.
//
// Returning a NIL INTERFACE rather than a non-nil adapter over a nil service is deliberate: the script
// service tests `s.projector == nil`, so an adapter whose method returned an error would take the other
// branch and report a storage failure instead of "this build cannot project".
func canvasProjectorFor(canvas *appprojects.Service) appscript.CanvasProjector {
	if canvas == nil {
		return nil
	}
	return &canvasProjector{projects: canvas}
}

// ProjectEntity writes or re-labels one entity's canvas node.
func (p *canvasProjector) ProjectEntity(ctx context.Context, projectID, entityType, entityID, label string) (string, error) {
	node, err := p.projects.CreateCanvasProjection(ctx, appprojects.ProjectionRequest{
		ProjectID:  projectID,
		EntityType: entityType,
		EntityID:   entityID,
		Title:      label,
	})
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// Compile-time proof that the adapter satisfies what the script service asks for.
var _ appscript.CanvasProjector = (*canvasProjector)(nil)
