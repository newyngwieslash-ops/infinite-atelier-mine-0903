package main

import (
	"context"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appevents "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstaleness "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/staleness"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
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

	dramaBinding  *desktop.DramaBinding
	assetsBinding *desktop.AssetsBinding
}

// composeDrama builds the drama stack over a writable database. It returns nil
// when the database is unavailable so the bindings stay unattached.
//
// One repository instance per family serves every port that family declares:
// the story repository satisfies all six of the story aggregate's read and
// write interfaces, the storyboard repository satisfies its four, and so on.
// They share the single connection the handle owns.
func composeDrama(handle *database.Handle) *dramaWiring {
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

	return &dramaWiring{
		story: appstory.NewService(appstory.Options{
			Repository: storyRepository,
			Clock:      clock,
			IDs:        ids,
		}),
		script: appscript.NewService(appscript.Options{
			Repository: scriptRepository,
			Clock:      clock,
			IDs:        ids,
			Events:     eventService,
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
		}),
		// WP-04 shipped this service with no composition root, recording that
		// it "exists for WP-05". This is that root.
		assets: appassets.NewService(appassets.Options{
			Repository: database.NewAssetRepository(connection),
			Clock:      clock,
			IDs:        ids,
		}),
		events: eventService,
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
}
