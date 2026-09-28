package main

import (
	"context"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
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
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
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
// snapshotStoreAdapter joins the two things a previs snapshot needs: a content-addressed store for
// the bytes, and the asset service for the link.
//
// It exists as an adapter rather than as methods on either, because the two halves belong to different
// layers: the store is infrastructure and the service is application. Putting `Link` on the store
// would make the infrastructure layer know about asset versions; putting `Store` on the service would
// make the application layer own a filesystem. The composition root is where this repository joins
// things that must not know about each other.
type snapshotStoreAdapter struct {
	store     *appassets.Service
	documents *desktop.DocumentStoring
}

// The compile-time proof that this adapter satisfies the binding's port. The two halves are joined
// here rather than in either package, so the assertion belongs here too: a signature drift fails the
// build rather than leaving the binding unattached at runtime.
var _ desktop.SnapshotStore = snapshotStoreAdapter{}

func (a snapshotStoreAdapter) Store(ctx context.Context, displayName string, body []byte) (desktop.StoredBytes, error) {
	return a.documents.ImportBytes(ctx, displayName, body)
}

func (a snapshotStoreAdapter) Link(ctx context.Context, versionID, fileHash, role string) error {
	_, err := a.store.AttachFile(ctx, versionID, fileHash, asset.FileRole(role))
	return err
}

// musicImporterAdapter satisfies the music import's port over the asset service and the same
// content-addressed store the document path uses.
//
// # Why it is a second adapter rather than a method on the snapshot one
//
// The two ports share ONE operation (`Store`) and differ in everything else: a snapshot LINKS a file to
// a version that already exists, while an import CREATES the asset, its version, the file link, the
// approval and the usage. Merging them would give one adapter a five-step method no other caller wants,
// and the snapshot path would have to argue that it never reaches the parts it does not use.
type musicImporterAdapter struct {
	assets    *appassets.Service
	documents *desktop.DocumentStoring
}

// The compile-time proof that this adapter satisfies the binding's port.
var _ desktop.MusicImporter = musicImporterAdapter{}

func (a musicImporterAdapter) Store(ctx context.Context, displayName string, body []byte) (desktop.StoredBytes, error) {
	return a.documents.ImportBytes(ctx, displayName, body)
}

// CreateBeddableAsset creates an audio asset with its first version.
//
// The type is `audio` rather than a music-specific one, because the asset aggregate's vocabulary is the
// schema's and a bed is an audio file: what distinguishes it is the ROLE its usage carries, which is a
// different column. A seventh asset type would need a migration to say what a usage role already says.
func (a musicImporterAdapter) CreateBeddableAsset(ctx context.Context, projectID, name string) (string, string, error) {
	record, version, err := a.assets.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: projectID, Type: asset.TypeAudio, Name: name,
	})
	if err != nil {
		return "", "", err
	}
	return record.ID, version.ID, nil
}

func (a musicImporterAdapter) Attach(ctx context.Context, versionID, fileHash string) error {
	// `primary` is the role the audio reader selects (`AudioFileFor` reads `role = 'primary'`), so a
	// file attached under any other role would be stored and never played.
	_, err := a.assets.AttachFile(ctx, versionID, fileHash, asset.RolePrimary)
	return err
}

func (a musicImporterAdapter) Approve(ctx context.Context, assetID, versionID string) error {
	// The impact flag is set because the caller IS the impact step: a freshly imported track replaces
	// nothing, and `ApprovalImpactOf` would return an empty list for a version with no consumers. The
	// flag is a statement about having LOOKED, and this adapter has: the version was created one step
	// ago by this same command.
	_, err := a.assets.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: versionID, ImpactAcknowledged: true,
	})
	return err
}

func (a musicImporterAdapter) Bed(ctx context.Context, versionID, shotID string) error {
	// The role is the mixer's own vocabulary rather than a string invented here, and it is what makes the
	// clip a bed: `buildMix` places a `music` clip from the top and mixes it at the bed's gain.
	_, err := a.assets.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: versionID,
		ConsumerType:   asset.ConsumerShot,
		ConsumerID:     shotID,
		UsageRole:      appmedia.UsageRoleForAudio(appmedia.AudioRoleMusic),
		Required:       false,
	})
	return err
}

type dramaWiring struct {
	story      *appstory.Service
	script     *appscript.Service
	storyboard *appstoryboard.Service
	workflow   *appworkflow.Service
	staleness  *appstaleness.Service
	assets     *appassets.Service
	gaps       *appassets.GapService
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
	// monoformBinding is the previs snapshot surface (WP-21).
	monoformBinding *desktop.MonoformBinding
	// musicBinding is the background-music import surface (WP-29).
	musicBinding *desktop.MusicImportBinding
	// musicImporter is what that binding commits through. It is a second adapter rather than a second
	// method on the snapshot one: the two share the store and differ in the other four operations.
	musicImporter musicImporterAdapter
	// snapshotStore is what that binding commits through, built here because this is where the file
	// store and the asset service are both in scope. `attach` receives only a context, so carrying the
	// built adapter is what lets the two halves meet without either learning about the other.
	snapshotStore snapshotStoreAdapter
	// ids mints the upload identifiers the snapshot transfer hands out. Its concrete type is the
	// platform generator, named here rather than through an interface because the binding takes a
	// function and a function is what this field supplies.
	ids *id.Generator
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
	assetRepository := database.NewAssetRepository(connection)
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
	// The propagator needs the finder to resolve references and the project resolver to
	// keep a mark inside the project that asked for it; both read the same connection.
	//
	// It is a LOCAL because two services below use it: the asset approval's impact
	// analysis reaches it through an adapter, and the drama binding's own propagation
	// commands call it directly. Composing a second instance would be a second answer to
	// "what does a change reach".
	stalenessService := appstaleness.NewService(appstaleness.Options{
		Marks:      stalenessRepository,
		Dependents: database.NewDependentFinder(connection),
		Projects:   database.NewProjectResolver(connection),
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

	assetService := appassets.NewService(appassets.Options{
		Repository: assetRepository,
		Clock:      clock,
		IDs:        ids,
		Events:     eventService,
		// The atomic collection command's transaction scope (`collect.go`).
		// The repository itself runs it: `WithinTx` is the method the import
		// already used to bind its cross-repository writes, so the same
		// implementation now scopes audio collection too — one composition,
		// not a second transaction mechanism.
		Transactions: database.NewAssetTransactions(assetRepository),
		// The impact half of an approval switch. It is supplied HERE rather than
		// declared inside the assets package because that package sits below the
		// staleness service in the dependency order: the service resolves projects
		// through the asset tables, so importing it there would be a cycle. Without
		// this adapter section 15.1's trigger fires into nothing, which is the state
		// WP-05 left it in — the graph and the graph's four storyboard types existed
		// and nothing joined them.
		Propagator: stalenessPropagatorFor(stalenessService),
	})

	return &dramaWiring{
		snapshotStore: snapshotStoreAdapter{
			store: assetService, documents: desktop.NewDocumentStoreForSnapshots(store, database.NewFileRepository(connection)),
		},
		musicImporter: musicImporterAdapter{
			assets: assetService, documents: desktop.NewDocumentStoreForSnapshots(store, database.NewFileRepository(connection)),
		},
		ids:   ids,
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
		staleness: stalenessService,
		// WP-04 shipped this service with no composition root, recording that
		// it "exists for WP-05". This is that root.
		//
		// It is built into a VARIABLE rather than inline because the previs snapshot adapter needs the
		// same service to link a stored image to a version, and two constructions would be two services
		// where one is meant.
		assets: assetService,
		// The gap report's own service, over its own port on the same repository. It is
		// built HERE because a report is what AC-BOARD-001's batch gate reads: without a
		// composed service the gate would refuse for a missing dependency in every real
		// build, which is the "interface with no real path" shape this repository's
		// reviews have found twice.
		gaps: appassets.NewGapService(appassets.GapOptions{
			Gaps:  assetRepository,
			Clock: clock,
			IDs:   ids,
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
	if w.monoformBinding != nil {
		// The store is the SAME two halves the import uses: the content-addressed store for the bytes
		// and the asset service for the link. The snapshot path adds no storage of its own, which is
		// the ruling ADR-0025 records.
		desktop.AttachMonoform(w.monoformBinding, ctx, w.snapshotStore, w.ids.New)
	}
	if w.musicBinding != nil {
		// The same content-addressed store the import and the snapshot use, joined here with the asset
		// service: this path adds no storage of its own.
		desktop.AttachMusicImport(w.musicBinding, ctx, w.musicImporter, w.ids.New)
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

// stalenessPropagator adapts the staleness service to the asset service's impact port.
//
// IT EXISTS FOR THE SAME REASON `canvasProjector` DOES, one package over: WP-05 built the
// staleness graph with four storyboard artifact types and built the propagation service,
// and NOTHING JOINED THEM to an approval switch. §15.1 lists "AssetVersion 默认批准版本
// 切换" as a trigger, so the rule was written, the mechanism existed, and no code path
// fired it.
//
// It is thin because the two request types differ only in which package declares the
// artifact-type constant: the assets package cannot import the staleness application
// service, so it states the same four fields as its own port and this translates them.
type stalenessPropagator struct {
	staleness *appstaleness.Service
}

// stalenessPropagatorFor returns the adapter for a staleness service, or nil when there
// is none. A nil adapter is what makes the asset service's impact notice optional rather
// than a refusal.
func stalenessPropagatorFor(service *appstaleness.Service) appassets.ImpactPropagator {
	if service == nil {
		return nil
	}
	return &stalenessPropagator{staleness: service}
}

// PropagateFrom marks what a changed artifact reaches.
func (p *stalenessPropagator) PropagateFrom(ctx context.Context, request appassets.ImpactRequest) error {
	_, err := p.staleness.PropagateFrom(ctx, appstaleness.PropagateRequest{
		ChangedType: staleness.ArtifactType(request.ChangedType),
		ChangedID:   request.ChangedID,
		ProjectID:   request.ProjectID,
		Reason:      request.Reason,
	})
	return err
}

// Compile-time proof that the adapter satisfies the asset service's impact port.
var _ appassets.ImpactPropagator = (*stalenessPropagator)(nil)
