package scriptpipeline

import (
	"context"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// service.go is this layer's entry point: the same four commands WP-08 shipped, now over
// the generic mechanism.
//
// THE COMMANDS DELEGATE RATHER THAN REPEAT. Each one does exactly two things the mechanism
// cannot: it supplies this layer's own state fields to the request, and it converts the
// layer's payload in or the mechanism's result out. Everything else — the agent map, the
// FIX read-back, the gate's ordering, the manual edit's two-step — is the mechanism's, so
// there is one implementation of each rule in the build.

// Service drives the three script stages.
type Service struct {
	stages *stagepipeline.Service
	script *appscript.Service
}

// Options configures the pipeline.
//
// The engine, the runtime, the workflow service and the skill source are the mechanism's;
// `Script` is this layer's, and `Episodes` is how the mechanism checks that a manual edit
// writes into the project it claims.
//
// `Episodes` is OPTIONAL and defaults to the script service: the check it enables reads an
// episode's project, which is this layer's own read, so a caller that supplies `Script` and
// nothing else still gets the check rather than a silently skipped one. A caller that
// supplies its own implementation overrides it.
type Options struct {
	Engine   *agentruntime.Engine
	Runtime  *agentruntime.Runtime
	Script   *appscript.Service
	Workflow *appworkflow.Service
	Assembly stagepipeline.SkillSource
	Runs     stagepipeline.RunReader
	Episodes stagepipeline.EpisodeProjectLookup
}

// New builds the pipeline.
func New(options Options) *Service {
	episodes := options.Episodes
	if episodes == nil {
		episodes = EpisodeProjects{Script: options.Script}
	}
	return &Service{
		stages: stagepipeline.New(stagepipeline.Options{
			Engine:   options.Engine,
			Runtime:  options.Runtime,
			Workflow: options.Workflow,
			Assembly: options.Assembly,
			Runs:     options.Runs,
			Layer:    NewLayer(options.Script),
			Episodes: episodes,
		}),
		script: options.Script,
	}
}

// Available reports whether the pipeline can drive anything.
func (s *Service) Available() bool {
	return s != nil && s.stages != nil && s.stages.Available() && s.script != nil
}

// Episodes returns the episode lookup the manual edit's scope check runs against.
//
// It exists for the LAYER's test to assert the check is wired, which is the defect the
// extraction introduced: the port replaced a direct service call, and nothing supplied an
// implementation, so a check whose failure mode is silent had stopped running.
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
		request.State = stageFieldsOf(stagepipeline.StageRequest{
			WorkflowRunID: "", Stage: "", EpisodeID: request.EpisodeID,
		})
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

// ManualEdit writes the user's own version and passes the stage.
//
// It takes the MECHANISM's request, with this layer's payload in the opaque field, because
// one desktop binding serves both pipelines and the payload is the only part that differs.
// `ManualEditScript` is the DTO-shaped convenience a caller with a form uses; this is the
// command that satisfies the shared interface.
//
// The payload is a TYPE ASSERTION rather than a compiled contract, which is the cost of one
// generic command for two layers' artifacts; a caller that supplied the wrong payload gets
// the layer's refusal naming what the stage needs.
func (s *Service) ManualEdit(ctx context.Context, request stagepipeline.ManualEditRequest) (stagepipeline.StageResult, error) {
	if !s.Available() {
		return stagepipeline.StageResult{}, agent.UnavailableError()
	}
	return s.stages.ManualEdit(ctx, request)
}

// ManualEditScript writes the user's own version from a form's DTOs.
//
// The conversion is this layer's because the shape is: a skeleton, a strategy and a
// structure are the three artifacts the SCRIPT stages produce, and a caller holding one of
// those payloads is asking this pipeline rather than the mechanism.
func (s *Service) ManualEditScript(ctx context.Context, request ManualEditRequest) (stagepipeline.StageResult, error) {
	if !s.Available() {
		return stagepipeline.StageResult{}, agent.UnavailableError()
	}
	return s.stages.ManualEdit(ctx, stagepipeline.ManualEditRequest{
		StageRunID:       request.StageRunID,
		Stage:            request.Stage,
		ProjectID:        request.ProjectID,
		EpisodeID:        request.EpisodeID,
		BasedOnVersionID: request.BasedOnVersionID,
		Summary:          request.Summary,
		CreatedByID:      request.CreatedByID,
		ChangeReason:     request.ChangeReason,
		Payload: ManualEditPayload{
			Structure:         request.Structure,
			Skeleton:          request.Skeleton,
			Strategy:          request.Strategy,
			SkeletonVersionID: request.SkeletonVersionID,
			StrategyVersionID: request.StrategyVersionID,
		},
	})
}

// ManualEditRequest is this layer's manual edit: the mechanism's fields plus the script
// payload.
type ManualEditRequest struct {
	StageRunID string
	Stage      Stage
	ProjectID  string
	EpisodeID  string
	// Structure is the user's content for a script version's scenes, lines and shots. It
	// carries no identifiers and no ordinals, exactly as the model's own write does: §17's
	// "ID、顺序和唯一性" is the code's job whoever is writing.
	Structure scriptdomain.ScriptStructureDraft
	// Skeleton and Strategy are the two upstream artifacts, for the stages whose content is
	// a row rather than a structure. Exactly one of the three is used, chosen by the stage.
	Skeleton *appscript.CreateStorySkeletonVersionRequest
	Strategy *appscript.CreateAdaptationStrategyVersionRequest
	Summary  string
	// BasedOnVersionID links the user's version to the one it edited, which is what makes
	// WP-08's "原版本保留" auditable for a manual edit as well as for a FIX.
	BasedOnVersionID string
	// SkeletonVersionID and StrategyVersionID are what the script stage's version cites.
	SkeletonVersionID string
	StrategyVersionID string
	CreatedByID       string
	ChangeReason      string
}

// stageFieldsOf reads this layer's state fields out of a request.
//
// The fields travel on the request's OWN type, so this package keeps the ergonomic struct
// its callers and tests use while the mechanism sees one opaque value. The conversion is
// the whole reason `Options.Layer` exists.
func stageFieldsOf(request stagepipeline.StageRequest) StateFields {
	if fields, ok := request.State.(StateFields); ok {
		return fields
	}
	// A caller that used the mechanism's own StageRequest directly has stated no fields,
	// which is legal: a stage with no upstream versions to name renders a state without
	// them rather than refusing.
	return StateFields{}
}
