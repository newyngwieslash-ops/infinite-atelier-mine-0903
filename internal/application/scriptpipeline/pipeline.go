// Package scriptpipeline drives the three script stages from end to end.
//
// # Why this package exists
//
// ROADMAP's WP-08 acceptance says "Script Agent 是独立层", and that is a statement about the
// dependency direction as much as about the agents: the Script layer's stages must be runnable and
// testable on their own, without a Production stage beside them and without agentruntime knowing what
// a story skeleton is. So the orchestration lives here.
//
// `agentruntime` stays generic. It runs one agent against one prompt and returns a validated outcome;
// it has no idea which stage is which, what a stage's policy is, or that a FIX decision carries
// finding identifiers. Everything that is SCRIPT knowledge lives in this package:
//
//   - the stage → executor and stage → supervisor maps (see stageAgents);
//   - the per-stage policy lookup, which the engine owns (AGENT_CONTRACTS section 10.1);
//   - the workflow-state rendering a stage's prompt carries;
//   - the FIX path, which reads a user's findings back from the decision row;
//   - the manual-edit path, which writes a user's version and passes the gate.
//
// # The one thing that could not stay generic
//
// `Registry.SupervisionFor` matches a stage name against an agent key's LAST SEGMENT, which works for
// every stage in the inventory except `script_generation`: its supervisor is
// `script.supervision.script`, and `script` is not `script_generation`. The heuristic therefore finds
// nothing for the third script stage, and WP-07 recorded that as a known limit of the last-segment
// match.
//
// The fix is not to widen the heuristic. A last-segment match over the whole inventory is exactly the
// kind of guess this repository refuses, and for this pair there is nothing to match on:
// `script.supervision.script` reviewing `script_generation` cannot be derived, it can only be STATED.
// `stageAgents` states it, and the package's tests assert the registry's own answer agrees with the
// map for the two stages it CAN resolve — so the map is not a rival source of truth, it is the
// complete one.
package scriptpipeline

import (
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// Stage is one of the three stages this pipeline drives.
//
// It is the workflow's own StageName under a local name, because the three are a CLOSED set here: the
// pipeline refuses a stage it does not drive rather than guessing an executor for it, which is what
// makes "the Script layer is independent" a property rather than a claim.
type Stage = workflow.StageName

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
// The order is the dependency order and not a preference: a strategy is written from a skeleton and a
// script from both, so a caller walking this list is walking the sequence FR-040 describes.
func Stages() []Stage {
	return []Stage{StageStorySkeleton, StageAdaptationStrategy, StageScriptGeneration}
}

// StageAgents names the two agents that serve one stage.
type StageAgents struct {
	Execution   string
	Supervision string
	// ArtifactType names the entity a stage's write produces, which is what a caller reports and what
	// the runtime's artifact verifier checks. It is stated per stage rather than derived from the
	// agent key, because the two do not agree: the generation agent writes a script_version, and its
	// key ends in `script_generation`.
	ArtifactType string
	// Family is which of the three version families the stage's artifact belongs to. It is what a
	// caller needs to read a stage's locks, and stating it here is what keeps a stage and its family
	// from drifting apart.
	Family script.VersionFamily
}

// stageAgents is the map, in pipeline order.
//
// EVERY stage this pipeline drives has an entry, and a stage without one is REFUSED rather than
// guessed. That is the fail-closed direction: a stage whose supervisor is unknown would otherwise run
// unsupervised — silently skipping the review its policy requires — or be supervised by whatever
// agent a substring match found, which is worse, because the report would look real.
var stageAgents = map[Stage]StageAgents{
	StageStorySkeleton: {
		Execution:    "script.execution.story_skeleton",
		Supervision:  "script.supervision.story_skeleton",
		ArtifactType: "story_skeleton_version",
		Family:       script.FamilyStorySkeleton,
	},
	StageAdaptationStrategy: {
		Execution:    "script.execution.adaptation_strategy",
		Supervision:  "script.supervision.adaptation_strategy",
		ArtifactType: "adaptation_strategy_version",
		Family:       script.FamilyAdaptationStrategy,
	},
	StageScriptGeneration: {
		Execution: "script.execution.script_generation",
		// The supervisor of the script stage, and the entry that could not be derived: the agent's key
		// ends in `script`, the stage is `script_generation`, and no last-segment match connects them.
		Supervision:  "script.supervision.script",
		ArtifactType: "script_version",
		Family:       script.FamilyScript,
	},
}

// AgentsForStage returns the agents that serve one stage.
//
// The boolean is false for a stage this pipeline does not drive, and callers REFUSE rather than
// substituting one: a stage that ran with an invented executor would produce an artifact of the wrong
// family under the right stage's name, which is the kind of error a schema accepts and a reviewer
// cannot see.
func AgentsForStage(stage Stage) (StageAgents, bool) {
	agents, ok := stageAgents[stage]
	return agents, ok
}

// SkillSource is what the pipeline reads a skill from.
//
// It is an interface rather than the concrete assembly so a test can supply documents directly, which
// is what lets this package be exercised without a pack directory, a database or a provider. The
// assembly satisfies it; nothing else in the build does.
type SkillSource interface {
	// SkillDocument returns the document text for one agent key.
	SkillDocument(agentKey string) (string, bool)
	// SkillVersionOf returns the version a run should cite for that agent.
	SkillVersionOf(agentKey string) (string, bool)
}

// Options configures the pipeline.
//
// Every dependency is required, and their absence is a REFUSAL for every command rather than a
// degraded mode: a pipeline that ran a stage without a supervisor would be skipping the review its
// policy requires, which is not something a caller can see in the result.
type Options struct {
	Engine   *agentruntime.Engine
	Runtime  *agentruntime.Runtime
	Script   *appscript.Service
	Workflow *appworkflow.Service
	Assembly SkillSource
	// Runs reads the agent runs a stage attempt owns, which is how the pipeline finds the version a
	// stage wrote. It is the read side of the run record, declared as the runtime's own narrow port
	// rather than as the Inspector: the Inspector is a presentation surface for the Agent Center, and
	// this package needs rows and tool calls rather than a summarised view.
	Runs RunReader
}

// RunReader is the run record's read side.
//
// It is `agentruntime.RunReader` under a local name, so this package's dependency is visible on its own
// signature rather than hidden behind a type from another package. The inspector's reader satisfies it;
// nothing in this package writes through it.
type RunReader = agentruntime.RunReader

// Service drives script stages.
type Service struct {
	engine   *agentruntime.Engine
	runtime  *agentruntime.Runtime
	script   *appscript.Service
	workflow *appworkflow.Service
	assembly SkillSource
	runs     RunReader
}

// New builds the pipeline.
func New(options Options) *Service {
	return &Service{
		engine:   options.Engine,
		runtime:  options.Runtime,
		script:   options.Script,
		workflow: options.Workflow,
		assembly: options.Assembly,
		runs:     options.Runs,
	}
}

// Available reports whether the pipeline can drive anything.
func (s *Service) Available() bool {
	return s != nil && s.engine != nil && s.runtime != nil && s.script != nil &&
		s.workflow != nil && s.assembly != nil && s.runs != nil
}

// unavailable is the refusal every command returns without the services.
//
// It does not name which one is missing: the caller's next step is to look at the composition root,
// where every dependency is named on one screen, and a message naming one of five would be right only
// until a second was also absent.
func unavailable() error {
	return agent.UnavailableError()
}

// trimOrEmpty is the trim this file uses wherever an identifier arrives from outside.
func trimOrEmpty(value string) string { return strings.TrimSpace(value) }

// agentRefusal reports a stage this pipeline does not drive.
//
// It is an invalid-input refusal rather than a not-found, because the stage NAME is what the caller
// got wrong and there is nothing to look up: a caller that asked to run `storyboard_table` here has
// used the right name for the wrong pipeline.
func agentRefusal(stage Stage) error {
	return agent.InvalidError("This pipeline does not drive the stage " + string(stage) + ".")
}
