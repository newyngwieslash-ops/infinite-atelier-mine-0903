package productionpipeline

import (
	"context"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// service.go is this pipeline's entry point: the same five stage commands WP-08 shipped
// for the script stages, over the generic mechanism, plus the two commands that are
// production's own.
type Service struct {
	stages     *stagepipeline.Service
	storyboard *appstoryboard.Service
	gaps       *appassets.GapService
	jobs       *appjobs.Service
	// maxImageConcurrency is the batch's submission bound, copied from Options so the
	// batch does not reach back into a configuration struct on every call.
	maxImageConcurrency int
}

// Options configures the pipeline.
//
// The engine, the runtime, the workflow service and the skill source are the mechanism's;
// everything else is this layer's, and the three that are optional have their own
// fail-closed rules rather than silently degrading:
//
//   - `Gaps` is required for the batch gate, which cannot answer without it.
//   - `Jobs` is required for the batch itself.
//   - `Episodes` defaults to the storyboard service's own resolvers, which is the read
//     the manual edit's scope check makes.
type Options struct {
	Engine     *agentruntime.Engine
	Runtime    *agentruntime.Runtime
	Storyboard *appstoryboard.Service
	Workflow   *appworkflow.Service
	Assembly   stagepipeline.SkillSource
	Runs       stagepipeline.RunReader
	Assets     *appassets.Service
	Gaps       *appassets.GapService
	Jobs       *appjobs.Service
	Episodes   stagepipeline.EpisodeProjectLookup
	// MaxImageConcurrency bounds the batch's submissions in flight. Zero takes
	// `DefaultMaxImageConcurrency`.
	MaxImageConcurrency int
}

// New builds the pipeline.
func New(options Options) *Service {
	episodes := options.Episodes
	if episodes == nil {
		episodes = EpisodeProjects{Storyboards: options.Storyboard}
	}
	return &Service{
		stages: stagepipeline.New(stagepipeline.Options{
			Engine:   options.Engine,
			Runtime:  options.Runtime,
			Workflow: options.Workflow,
			Assembly: options.Assembly,
			Runs:     options.Runs,
			Layer:    NewLayer(options.Storyboard, options.Gaps, options.Assets),
			Episodes: episodes,
		}),
		storyboard:          options.Storyboard,
		gaps:                options.Gaps,
		jobs:                options.Jobs,
		maxImageConcurrency: options.MaxImageConcurrency,
	}
}

// Available reports whether the pipeline can drive anything.
func (s *Service) Available() bool {
	return s != nil && s.stages != nil && s.stages.Available() && s.storyboard != nil
}

// BatchAvailable reports whether the image batch can run.
//
// It is separate from `Available` because the two need different services: a build can
// drive the agent stages with no job store — a test or a headless analysis build — and
// the batch then refuses for the reason it cannot run rather than for a missing stage
// stack.
func (s *Service) BatchAvailable() bool {
	return s != nil && s.jobs != nil && s.storyboard != nil && s.gaps != nil
}

// Episodes returns the episode lookup the manual edit's scope check runs against.
func (s *Service) Episodes() stagepipeline.EpisodeProjectLookup {
	if s == nil || s.stages == nil {
		return nil
	}
	return s.stages.Episodes()
}

// RunStage starts one attempt, runs it, and parks the stage for review.
func (s *Service) RunStage(ctx context.Context, request stagepipeline.StageRequest) (stagepipeline.StageResult, error) {
	if !s.Available() {
		return stagepipeline.StageResult{}, agent.UnavailableError()
	}
	request.State = stageFieldsOf(request)
	return s.stages.RunStage(ctx, request)
}

// RunSupervision reviews one attempt and applies the report to the stage.
func (s *Service) RunSupervision(ctx context.Context, request stagepipeline.SupervisionRequest) (stagepipeline.SupervisionResult, error) {
	if !s.Available() {
		return stagepipeline.SupervisionResult{}, agent.UnavailableError()
	}
	if request.State == nil {
		request.State = StateFields{EpisodeID: request.EpisodeID}
	}
	return s.stages.RunSupervision(ctx, request)
}

// ApplyUserGate records the user's decision and moves the stage.
func (s *Service) ApplyUserGate(ctx context.Context, request stagepipeline.GateRequest) (workflow.StageRun, error) {
	if !s.Available() {
		return workflow.StageRun{}, agent.UnavailableError()
	}
	return s.stages.ApplyUserGate(ctx, request)
}

// StartRevision begins the attempt a FIX or REDO asked for.
func (s *Service) StartRevision(ctx context.Context, stageRunID string) (workflow.StageRun, error) {
	if !s.Available() {
		return workflow.StageRun{}, agent.UnavailableError()
	}
	return s.stages.StartRevision(ctx, stageRunID)
}

// ManualEdit is refused for every production stage.
//
// It travels through the mechanism so this type satisfies the same interface the script
// pipeline does — which is what lets one desktop binding serve both — and the refusal is
// the LAYER's, stated once in `WriteVersion`. The mechanism's manual edit writes a version
// and then approves it, and for these artifacts the approval has its own preconditions
// (a gap report with a missing required asset may not be approved) that a generic
// two-step cannot check.
func (s *Service) ManualEdit(ctx context.Context, request stagepipeline.ManualEditRequest) (stagepipeline.StageResult, error) {
	if !s.Available() {
		return stagepipeline.StageResult{}, agent.UnavailableError()
	}
	return s.stages.ManualEdit(ctx, request)
}

// GateCheckRequest asks whether a batch may run.
type GateCheckRequest struct {
	// EpisodeID is the episode whose gap analysis is read.
	EpisodeID string
	// StoryboardVersionID, when set, is checked for being approved.
	StoryboardVersionID string
	// StoryboardID, when set, has its approved version looked up.
	StoryboardID string
}

// stageFieldsOf reads this layer's state fields out of a request.
//
// The fields travel on the request's OWN type, so this package keeps the ergonomic struct
// its callers use while the mechanism sees one opaque value.
func stageFieldsOf(request stagepipeline.StageRequest) StateFields {
	if fields, ok := request.State.(StateFields); ok {
		if fields.EpisodeID == "" {
			fields.EpisodeID = request.EpisodeID
		}
		return fields
	}
	// A caller that used the mechanism's own request directly has stated no fields, which
	// is legal: a stage with no upstream versions to name renders a state without them
	// rather than refusing.
	return StateFields{EpisodeID: request.EpisodeID}
}

// EpisodeProjects answers which project an episode belongs to, through the storyboard
// service's own resolver.
//
// The port exists because the mechanism cannot import a layer's application service, and
// this layer's read is the storyboard repository's: every production artifact is reached
// through an episode, and that repository already resolves one to its project for the
// approvals that need it.
type EpisodeProjects struct {
	Storyboards *appstoryboard.Service
}

// ProjectOfEpisode returns the project an episode belongs to.
func (e EpisodeProjects) ProjectOfEpisode(ctx context.Context, episodeID string) (string, error) {
	if e.Storyboards == nil {
		return "", agent.UnavailableError()
	}
	return e.Storyboards.ProjectOfEpisode(ctx, episodeID)
}

// compile-time proof that the episode lookup satisfies the mechanism.
var _ stagepipeline.EpisodeProjectLookup = EpisodeProjects{}

// The versioning vocabulary this file compares against, stated so a reader sees which
// package the statuses come from.
var _ = versioning.StatusApproved
