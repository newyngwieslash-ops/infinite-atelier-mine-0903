// Package scriptpipeline drives the three script stages from end to end.
//
// # What this package is now
//
// WP-08 built the whole mechanism here: the stage machine calls, the FIX read-back, the
// gate, the manual edit. WP-09 needs the same mechanism for the production stages, and
// rather than write it twice — WP-08's own reviews found four defects of exactly that
// shape, "two places disagree about the same fact" — the mechanism moved to
// `application/stagepipeline` and this package became what a LAYER supplies.
//
// So what remains here is the script layer's knowledge and nothing else:
//
//   - the three stages and the agents that serve them (`stageAgents`);
//   - the prompt's state layer for a script stage (`stateFor`);
//   - approving a version of one of the three script families;
//   - the field locks a script version carries (AC-SCRIPT-002);
//   - writing a user's own skeleton, strategy or script version.
//
// # The one thing that could not stay generic, and where it went
//
// `Registry.SupervisionFor` matches a stage name against an agent key's LAST SEGMENT,
// which works for every stage in the inventory except `script_generation`: its supervisor
// is `script.supervision.script`, and `script` is not `script_generation`. The heuristic
// therefore finds nothing for the third script stage, and WP-07 recorded that as a known
// limit of the last-segment match.
//
// The fix is not to widen the heuristic. A last-segment match over the whole inventory is
// exactly the kind of guess this repository refuses, and for this pair there is nothing to
// match on: `script.supervision.script` reviewing `script_generation` cannot be derived,
// it can only be STATED. `stageAgents` states it, and this package's tests assert the
// registry's own answer agrees with the map for the two stages it CAN resolve — so the map
// is not a rival source of truth, it is the complete one.
package scriptpipeline

import (
	"context"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// Stage is one of the three stages this pipeline drives.
//
// It is the workflow's own StageName under a local name, because the three are a CLOSED
// set here: the pipeline refuses a stage it does not drive rather than guessing an
// executor for it, which is what makes "the Script layer is independent" a property
// rather than a claim.
type Stage = stagepipeline.Stage

const (
	// StageStorySkeleton turns approved story events into a skeleton version (FR-040's S1).
	StageStorySkeleton Stage = "story_skeleton"
	// StageAdaptationStrategy decides what happens to each event (FR-040's S2).
	StageAdaptationStrategy Stage = "adaptation_strategy"
	// StageScriptGeneration writes the scenes, lines and shots (FR-040's S3).
	StageScriptGeneration Stage = "script_generation"
)

// Stages lists the three in pipeline order.
//
// The order is the dependency order and not a preference: a strategy is written from a
// skeleton and a script from both, so a caller walking this list is walking the sequence
// FR-040 describes.
func Stages() []Stage {
	return []Stage{StageStorySkeleton, StageAdaptationStrategy, StageScriptGeneration}
}

// LiveStageAgents is the STATED map from a stage to the two agents that serve it, under
// the name WP-08 gave it.
//
// It is `stagepipeline.StageAgents` under a local alias so this package's own tests and
// callers keep reading the way they did before the mechanism was extracted — and so a
// reader comparing the two pipelines sees the same shape in both.
type LiveStageAgents = stagepipeline.StageAgents

// WriteToolPrefix is what every script write tool's key starts with.
//
// It is stated once because two mechanisms depend on it agreeing: the artifact verifier
// reads the `entityType` a tool reports, and the pipeline's version walk looks for a call
// whose key is `script.create_<artifactType>`. A layer that renamed its tools without
// changing this would find no versions and state no locks — silently, which is why the
// prefix is a named constant with a test that every stage's write tool starts with it.
const WriteToolPrefix = "script.create_"

// stageAgents is the map, in pipeline order.
//
// EVERY stage this pipeline drives has an entry, and a stage without one is REFUSED rather
// than guessed. That is the fail-closed direction: a stage whose supervisor is unknown
// would otherwise run unsupervised — silently skipping the review its policy requires —
// or be supervised by whatever agent a substring match found, which is worse, because the
// report would look real.
var stageAgents = map[Stage]LiveStageAgents{
	StageStorySkeleton: {
		Execution:    "script.execution.story_skeleton",
		Supervision:  "script.supervision.story_skeleton",
		ArtifactType: "story_skeleton_version",
	},
	StageAdaptationStrategy: {
		Execution:    "script.execution.adaptation_strategy",
		Supervision:  "script.supervision.adaptation_strategy",
		ArtifactType: "adaptation_strategy_version",
	},
	StageScriptGeneration: {
		Execution: "script.execution.script_generation",
		// The supervisor of the script stage, and the entry that could not be derived: the
		// agent's key ends in `script`, the stage is `script_generation`, and no
		// last-segment match connects them.
		Supervision:  "script.supervision.script",
		ArtifactType: "script_version",
	},
}

// IsScriptStage reports whether this layer drives a stage.
//
// It is what the desktop binding routes on: a caller names a stage, and which layer answers
// for it is the LAYER's statement rather than a list the binding keeps. A second list in the
// binding would be a second answer to the same question, and the two would drift.
func IsScriptStage(stage string) bool {
	_, ok := stageAgents[Stage(stage)]
	return ok
}

// AgentsForStage returns the agents that serve one stage.
//
// The boolean is false for a stage this pipeline does not drive, and callers REFUSE rather
// than substituting one: a stage that ran with an invented executor would produce an
// artifact of the wrong family under the right stage's name, which is the kind of error a
// schema accepts and a reviewer cannot see.
func AgentsForStage(stage Stage) (LiveStageAgents, bool) {
	agents, ok := stageAgents[stage]
	if !ok {
		return LiveStageAgents{}, false
	}
	// The prefix is filled in here rather than repeated in every entry, so a rename is one
	// edit and a new stage cannot forget it.
	agents.WriteToolPrefix = WriteToolPrefix
	return agents, true
}

// Layer adapts this package's knowledge to the generic mechanism.
//
// It holds the script service, which is the only dependency the mechanism does not: every
// other one is the same service whichever layer is being driven.
type Layer struct {
	script *appscript.Service
}

// NewLayer builds the script layer over its service.
func NewLayer(script *appscript.Service) *Layer {
	return &Layer{script: script}
}

// Compile-time proof that this layer satisfies the mechanism.
var _ stagepipeline.Layer = (*Layer)(nil)

// Name identifies the layer in the actor every transition records.
func (l *Layer) Name() string { return "scriptpipeline" }

// Stages lists the three stages in pipeline order.
func (l *Layer) Stages() []Stage { return Stages() }

// AgentsFor returns the agents serving one stage.
func (l *Layer) AgentsFor(stage Stage) (stagepipeline.StageAgents, bool) {
	return AgentsForStage(stage)
}

// StateFor renders the prompt's workflow-state layer for one attempt.
//
// The fields are the ones this layer's tools need to name what they write: the episode a
// version belongs to, and the version identifiers a stage is run against. They come from
// the request's `State`, which is the script layer's own `StateFields`.
func (l *Layer) StateFor(attempt workflow.StageRun, request stagepipeline.StageRequest) string {
	fields, _ := request.State.(StateFields)
	return renderState(attempt, request, fields)
}

// Approve puts one script version in force for its family.
//
// The family comes from the STAGE, so a caller cannot approve a version of another kind by
// naming it: a skeleton version approved against the script stage would make "the approved
// artifact of this stage" a statement the database recorded and no reader could reconcile
// with what the stage produced.
func (l *Layer) Approve(ctx context.Context, stage Stage, versionID, traceID string) error {
	// The stage is resolved BEFORE the services, because the two refusals send a caller to
	// different places: a stage this pipeline does not drive means the caller used the wrong
	// pipeline, which they can fix, while a missing service means the build did not compose,
	// which they cannot. So the stage-first order is the one that lets a caller act.
	if _, err := familyOfStage(stage); err != nil {
		return err
	}
	if l == nil || l.script == nil {
		return agent.UnavailableError()
	}
	switch stage {
	case StageStorySkeleton:
		_, err := l.script.ApproveStorySkeletonVersion(ctx, appscript.ApproveStorySkeletonVersionRequest{
			VersionID: versionID, TraceID: traceID,
		})
		return err
	case StageAdaptationStrategy:
		_, err := l.script.ApproveAdaptationStrategyVersion(ctx, appscript.ApproveAdaptationStrategyVersionRequest{
			VersionID: versionID, TraceID: traceID,
		})
		return err
	case StageScriptGeneration:
		_, err := l.script.ApproveScriptVersion(ctx, appscript.ApproveScriptVersionRequest{
			ScriptVersionID: versionID, TraceID: traceID,
		})
		return err
	default:
		return agent.InvalidError("This pipeline does not drive the stage " + string(stage) + ".")
	}
}

// Locks returns the field pins a script version carries.
//
// It is AC-SCRIPT-002's half of the FIX prompt layer. A lock of another family is a
// REFUSAL rather than a skip: the lock table has no foreign key across three version
// tables, so a row naming the wrong family is corrupt, and a pin that reads as a
// protection and enforces nothing is worse than no pin.
func (l *Layer) Locks(ctx context.Context, stage Stage, versionID string) ([]agentruntime.LockedRef, error) {
	// Stage first, for the same reason `Approve` reads it first: which pipeline drives a
	// stage is the caller's mistake to fix, and the build's missing service is not.
	family, err := familyOfStage(stage)
	if err != nil {
		return nil, err
	}
	if l == nil || l.script == nil {
		return nil, agent.UnavailableError()
	}
	locks, err := l.script.ListScriptFieldLocks(ctx, versionID)
	if err != nil {
		return nil, err
	}
	refs := make([]agentruntime.LockedRef, 0, len(locks))
	for _, lock := range locks {
		if lock.Family != family {
			return nil, agent.SecurityError("A lock on that version names a different artifact family.")
		}
		refs = append(refs, agentruntime.LockedRef{
			EntityType: string(family) + "_version",
			EntityID:   versionID,
			Field:      string(lock.Field),
			Label:      "pinned by the user: " + string(lock.Field),
		})
	}
	return refs, nil
}

// familyOfStage maps a stage to the version family its artifact belongs to.
func familyOfStage(stage Stage) (scriptdomain.VersionFamily, error) {
	switch stage {
	case StageStorySkeleton:
		return scriptdomain.FamilyStorySkeleton, nil
	case StageAdaptationStrategy:
		return scriptdomain.FamilyAdaptationStrategy, nil
	case StageScriptGeneration:
		return scriptdomain.FamilyScript, nil
	default:
		return "", agent.InvalidError("This pipeline does not drive the stage " + string(stage) + ".")
	}
}

// WriteVersion writes the user's own version for a manual edit.
//
// The switch is by STAGE rather than by whether a payload field was populated: a caller
// that supplied a skeleton payload for the script stage has used the wrong request, and
// inferring from what was filled in would write a skeleton version into a script stage.
func (l *Layer) WriteVersion(ctx context.Context, stage Stage, _ stagepipeline.StageAgents, request stagepipeline.ManualEditRequest) (string, error) {
	// Stage first again: `familyOfStage` resolves exactly the stages this layer drives, so a
	// caller who named another pipeline's stage learns that here rather than from a build
	// whose services happen to be missing.
	if _, err := familyOfStage(stage); err != nil {
		return "", err
	}
	if l == nil || l.script == nil {
		return "", agent.UnavailableError()
	}
	payload, _ := request.Payload.(ManualEditPayload)
	createdByID := strings.TrimSpace(request.CreatedByID)
	changeReason := request.ChangeReason
	basedOn := strings.TrimSpace(request.BasedOnVersionID)
	episodeID := strings.TrimSpace(request.EpisodeID)
	switch stage {
	case StageStorySkeleton:
		if payload.Skeleton == nil {
			return "", agent.InvalidError("A manual edit of the skeleton stage needs a skeleton version.")
		}
		body := *payload.Skeleton
		body.EpisodeID = episodeID
		body.BasedOnVersionID = basedOn
		body.ChangeReason = changeReason
		body.CreatedByType = versioning.CreatedByUser
		body.CreatedByID = createdByID
		version, err := l.script.CreateStorySkeletonVersion(ctx, body)
		if err != nil {
			return "", err
		}
		return version.ID, nil
	case StageAdaptationStrategy:
		if payload.Strategy == nil {
			return "", agent.InvalidError("A manual edit of the strategy stage needs a strategy version.")
		}
		body := *payload.Strategy
		body.EpisodeID = episodeID
		body.BasedOnVersionID = basedOn
		body.ChangeReason = changeReason
		body.CreatedByType = versioning.CreatedByUser
		body.CreatedByID = createdByID
		version, err := l.script.CreateAdaptationStrategyVersion(ctx, body)
		if err != nil {
			return "", err
		}
		return version.ID, nil
	case StageScriptGeneration:
		// The script stage: the user's content is a STRUCTURE, and it needs a version row
		// to hang off. The row is created here with the user as its author, because a
		// manual edit is the user writing rather than a model that happens to be in play.
		scriptRecord, err := l.script.EnsureScript(ctx, episodeID)
		if err != nil {
			return "", err
		}
		version, err := l.script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
			ScriptID:                    scriptRecord.ID,
			BasedOnVersionID:            basedOn,
			StorySkeletonVersionID:      strings.TrimSpace(payload.SkeletonVersionID),
			AdaptationStrategyVersionID: strings.TrimSpace(payload.StrategyVersionID),
			Summary:                     request.Summary,
			CreatedByType:               versioning.CreatedByUser,
			CreatedByID:                 createdByID,
			ChangeReason:                changeReason,
		})
		if err != nil {
			return "", err
		}
		if len(payload.Structure.Scenes) == 0 {
			// A version row with no content is a manual edit that edited nothing. It is
			// refused HERE rather than left for the structure write, because the version row
			// is already committed by this point and the caller's next step is to supply the
			// scenes — an empty version would read to a reviewer as a script whose content is
			// missing.
			return "", agent.InvalidError("A manual edit of a script needs at least one scene.")
		}
		if _, err := l.script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
			ScriptID:        scriptRecord.ID,
			ScriptVersionID: version.ID,
			// The project travels so the reference check runs: a user's edit cites story
			// events and entities through the form, and those citations are checked like
			// any other.
			ProjectID:     strings.TrimSpace(request.ProjectID),
			Draft:         payload.Structure,
			Summary:       request.Summary,
			CreatedByType: versioning.CreatedByUser,
			CreatedByID:   createdByID,
			ChangeReason:  changeReason,
		}); err != nil {
			return "", err
		}
		return version.ID, nil
	default:
		return "", agent.InvalidError("This pipeline does not drive the stage " + string(stage) + ".")
	}
}

// ManualEditPayload is the script layer's own payload for a manual edit.
//
// Exactly one of the three is used, chosen by the stage — which is why they travel in one
// struct rather than as three commands: the mechanism has one command, and the layer is
// what knows which member its stage needs.
type ManualEditPayload struct {
	Structure scriptdomain.ScriptStructureDraft
	Skeleton  *appscript.CreateStorySkeletonVersionRequest
	Strategy  *appscript.CreateAdaptationStrategyVersionRequest
	// SkeletonVersionID and StrategyVersionID are what the script stage's version cites.
	// §7.5 makes them FIELDS of the version rather than provenance, so the domain refuses a
	// script version without them — and a user's edit through a form has the same
	// obligation a model's write does.
	SkeletonVersionID string
	StrategyVersionID string
}

// EpisodeProjects answers which project an episode belongs to, through the script service.
//
// The mechanism needs this and cannot hold it: an episode's project is a script-family fact,
// so the read lives on the layer's own service. Before WP-09's extraction the check called
// `Service.GetEpisode` directly from the same code that did the manual edit; the port moves
// that call one step out without changing WHAT is read, which is what keeps the scope check
// working rather than silently skipping.
//
// The script service is the one the layer was built over, so a build with no script service
// has no episode lookup either — and `ManualEdit` refuses for the missing service before it
// ever asks the question.
type EpisodeProjects struct {
	Script *appscript.Service
}

// ProjectOfEpisode returns the project an episode belongs to.
func (e EpisodeProjects) ProjectOfEpisode(ctx context.Context, episodeID string) (string, error) {
	if e.Script == nil {
		return "", agent.UnavailableError()
	}
	episode, err := e.Script.GetEpisode(ctx, episodeID)
	if err != nil {
		return "", err
	}
	return episode.ProjectID, nil
}

// StateFields are the script layer's extra state for the prompt's state layer.
//
// A structure rather than positional arguments because the renderer reads them by name and
// a fifth field would otherwise change every call site.
type StateFields struct {
	SelectedEventIDs  []string
	SkeletonVersionID string
	StrategyVersionID string
	ScriptVersionID   string
	// ShotCount asks the generation stage for a script with at least this many shots.
	//
	// It is OPTIONAL and zero means "no constraint", which is what keeps every existing caller's
	// behaviour: the deterministic mock writes a fixed two-scene, one-shot document when it is not
	// asked for more, and a test that asserts those scene durations depends on that document rather
	// than on a number a caller happened to pass.
	//
	// It exists because a caller can have a REQUIREMENT about shape rather than about content:
	// PRD §19's AC-E2E-002 asks for a board of at least twelve shots from one episode, and a
	// pipeline that could only ever produce one shot could not satisfy it. Asking for the count is
	// the caller's act; producing a document that meets it is the model's.
	ShotCount int
}
