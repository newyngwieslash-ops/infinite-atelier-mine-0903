// Package stagepipeline drives an agent layer's stages from end to end.
//
// # What is generic and what is not
//
// WP-08 built this mechanism for the three script stages, and WP-09 needs the same
// mechanism for the six production stages. Writing it twice would be roughly six
// hundred duplicated lines, and WP-08's own reviews are the evidence for what that
// costs: four of the defects they found were "two places disagree about the same
// fact". So the mechanism lives here and each layer STATES what differs.
//
// The split is:
//
//	generic (this package)             the layer states (its Layer implementation)
//	-------------------------------    ------------------------------------------
//	start an attempt, run the agent,   which agent serves which stage
//	move the stage to review           what the prompt's state layer says
//	read a FIX's findings back         how a version is approved
//	run the supervisor, store the      which field locks a version carries
//	report, apply the verdict          how the user's own version is written
//	write the user's decision
//	approve the artifact, then move
//
// # Why the layer is an interface rather than a set of callbacks
//
// Every one of those five differences is consulted from more than one place — the
// agent map from `RunStage` and `RunSupervision`, the approval from the gate and
// from `ManualEdit` — so a callback per call site would let the two disagree. One
// interface, resolved once per command, is what makes "the stage's executor and the
// stage's approver are the same layer's answer" a property rather than a hope.
package stagepipeline

import (
	"context"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// Stage is a workflow stage name under this package's name.
//
// It is an alias rather than a new type so a caller passes the value the engine
// already uses, and so a layer's `Stages()` and the registry's answer cannot be two
// different vocabularies.
type Stage = workflow.StageName

// StageAgents names the two agents that serve one stage, and what the stage writes.
type StageAgents struct {
	Execution   string
	Supervision string
	// ArtifactType names the entity a stage's write produces, which is what a caller
	// reports and what the runtime's artifact verifier checks. It is stated per stage
	// rather than derived from the agent key, because the two do not agree: the script
	// generation agent writes a `script_version` and its key ends in `script_generation`.
	ArtifactType string
	// WriteToolPrefix is what the stage's write tool key starts with, so a FIX can find
	// the version an attempt produced by looking for the call that wrote it. It is a
	// field rather than `"script.create_"` hard-coded, because the production layer's
	// tools live under their own domains.
	WriteToolPrefix string
}

// Layer is the knowledge a concrete pipeline supplies.
//
// Every method is called with a stage the layer itself declared in `Stages()`, so an
// implementation may refuse an unknown stage by returning the zero value — but it must
// not PANIC on one, because the generic mechanism's first act is to check membership
// and a panic would be a crash where a refusal belongs.
type Layer interface {
	// Name identifies the layer in the actor every transition records, so an audit can
	// tell which pipeline moved a stage.
	Name() string
	// Stages lists the stages this layer drives, in pipeline order.
	Stages() []Stage
	// AgentsFor returns the agents serving one stage. The boolean is false for a stage
	// this layer does not drive, and the mechanism REFUSES rather than substituting.
	AgentsFor(stage Stage) (StageAgents, bool)
	// StateFor renders the prompt's workflow-state layer for one attempt. It is the
	// layer's because the fields a stage's tools need are the layer's: a script stage
	// needs its skeleton version and a production stage needs its storyboard.
	StateFor(attempt workflow.StageRun, request StageRequest) string
	// Approve puts one version in force for its family. It is separate from moving the
	// stage because they are two acts: a gate that only moved the stage would leave the
	// project with no approved artifact while the workflow reported success.
	Approve(ctx context.Context, stage Stage, versionID, traceID string) error
	// Locks returns the field pins a version carries, for the FIX prompt layer. A layer
	// whose artifacts have no field locks returns nil rather than an error: "this layer
	// pins nothing" is an answer, and an error would stop a revision that is fine.
	Locks(ctx context.Context, stage Stage, versionID string) ([]agentruntime.LockedRef, error)
	// WriteVersion writes the user's own version for a manual edit and returns its
	// identifier. The request carries the layer's own payload, which this method is the
	// only reader of.
	WriteVersion(ctx context.Context, stage Stage, agents StageAgents, request ManualEditRequest) (string, error)
}

// EpisodeProjectLookup answers which project an episode belongs to.
//
// It is a port rather than a call to the script service because BOTH layers need it and
// neither owns it: an episode is the unit a drama project is produced in (DOMAIN_MODEL
// section 7.1), so both layers walk to it and the composition root supplies whichever
// service can answer.
//
// WP-08 reached the same fact through `appscript.Service.GetEpisode`, which the script
// layer already held. The port replaces that because the mechanism cannot import a layer's
// service, and the script layer supplies it SHORTLY: `scriptpipeline.Options.Episodes` is
// filled from the same `Script` service, so the check runs against the same read it always
// did rather than being disabled by the extraction.
type EpisodeProjectLookup interface {
	// ProjectOfEpisode returns the project an episode belongs to. A missing episode is a
	// not-found rather than an empty string, so "no such episode" and "an episode with no
	// project" cannot be confused.
	ProjectOfEpisode(ctx context.Context, episodeID string) (string, error)
}

// StageRequest asks for one attempt at one stage.
type StageRequest struct {
	WorkflowRunID string
	Stage         Stage
	// ProjectID and EpisodeID scope the run. They come from the caller rather than from a
	// model, and they are what the prompt's state layer and every tool's scope check are
	// built from.
	ProjectID string
	EpisodeID string
	// Task is what this attempt is asked to do, in the caller's words. For a stage that
	// reads imported text it is document-adjacent, so the caller states whether it is
	// untrusted.
	Task            string
	TaskIsUntrusted bool
	// UserMessage is the user's own instruction, which is trusted: it is the person the
	// run belongs to, speaking to their own agent.
	UserMessage string
	// FixFromStageRunID, when set, makes this attempt a FIX: the findings the user named
	// on that attempt's decision are read back from the database and rendered into the
	// prompt.
	//
	// It is the ATTEMPT rather than a decision id, because that is what a caller has: a
	// UI showing a failed review knows which stage attempt it is looking at. Asking the
	// caller to fetch the decision first would put the durable read in the caller's
	// hands — and a caller that passed an empty issue list would produce a FIX that fixed
	// nothing while still being recorded as one.
	FixFromStageRunID string
	// ModelID and ProviderID name the model this attempt runs on, resolved by the caller
	// from the project's policy for the agent's layer. The pipeline does not resolve
	// policy: that is a fact about a project's configuration, and the layer that owns it
	// is `agent_wiring`.
	ModelID    string
	ProviderID string
	// State is the layer's own extra state for the prompt's state layer, which `StateFor`
	// is the only reader of. It travels as an opaque value for the reason the manual
	// edit's payload does: the two pipelines need different fields, and naming them all
	// here would make this package know what a storyboard is.
	State any
}

// StageResult is what one attempt produced.
type StageResult struct {
	// StageRun is the attempt, in whatever status the run left it: `reviewing` on
	// success, and whatever `StartStage` produced on a refusal.
	StageRun workflow.StageRun
	// Outcome is the runtime's own record of the run, including the run id a caller needs
	// to look it up and the tool calls it made.
	Outcome agentruntime.Outcome
	// ArtifactIDs are the entity identifiers the stage's tool calls created, in the order
	// the calls ran. They are read from the RECORDED CALLS rather than parsed out of the
	// model's answer, because an answer is a claim and a call's result is the row a write
	// returned.
	ArtifactIDs []string
}

// SupervisionRequest asks for one review of one attempt.
type SupervisionRequest struct {
	StageRunID string
	ProjectID  string
	EpisodeID  string
	// ArtifactVersionID is the version the review is about, which the supervisor reads
	// with its own tools. The pipeline does not infer it: a review of the wrong version
	// is a report about an artifact nobody is judging.
	ArtifactVersionID string
	ModelID           string
	ProviderID        string
	// State is the layer's own extra state, as in StageRequest.
	State any
}

// SupervisionResult is what one review produced.
type SupervisionResult struct {
	StageRun workflow.StageRun
	Outcome  agentruntime.Outcome
	Report   workflow.ReviewReport
	Issues   []workflow.ReviewIssue
}

// GateRequest carries a user's decision about one attempt.
type GateRequest struct {
	StageRunID  string
	Decision    workflow.GateDecision
	Instruction string
	Reason      string
	// LockedEntityRefsJSON is what the user pinned while deciding, in section 7.3's shape.
	// It is stored on the decision and read back by the next attempt's FIX, which is why
	// it travels as JSON rather than as a decoded value: the pipeline stores what the
	// user's command produced.
	LockedEntityRefsJSON string
	// IssueIDsJSON are the findings a FIX must address, as the JSON array section 12.2
	// stores on the decision. It is a string rather than a slice because the column is
	// JSON and the pipeline must not re-encode what the user's command stated.
	IssueIDsJSON string
	// ArtifactVersionID names the version an approving decision puts in force, and it is
	// REQUIRED for one that does.
	//
	// It is here because moving a stage and approving an artifact are TWO acts, and
	// "approved is unique" is about the second: an episode has one approved skeleton
	// version, and the schema enforces that with a partial unique index. A gate that only
	// moved the stage would leave the project with no approved version while the workflow
	// reported the stage passed — which is what WP-08's canary found, and it is not a
	// small thing: every later stage reads the APPROVED version.
	ArtifactVersionID string
	CreatedByID       string
}

// ManualEditRequest is the generic half of a user's own version.
//
// The payload is opaque here and read only by the layer's `WriteVersion`: what a user
// edits depends on the artifact, and a script's is a structure of scenes while a
// storyboard's is a table of items. Naming every possible payload in this package would
// make it know every artifact in the build.
type ManualEditRequest struct {
	StageRunID string
	Stage      Stage
	ProjectID  string
	EpisodeID  string
	// BasedOnVersionID links the user's version to the one it edited, which is what makes
	// "the original version is kept" auditable for a manual edit as well as for a FIX.
	BasedOnVersionID string
	Summary          string
	CreatedByID      string
	ChangeReason     string
	Payload          any
}

// trimOrEmpty is the trim this package uses wherever an identifier arrives from outside.
func trimOrEmpty(value string) string { return strings.TrimSpace(value) }

// unavailable is the refusal every command returns without the services.
//
// It does not name which one is missing: the caller's next step is to look at the
// composition root, where every dependency is named on one screen, and a message naming
// one of five would be right only until a second was also absent.
func unavailable() error { return agent.UnavailableError() }
