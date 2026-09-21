package scriptpipeline

import (
	"context"
	"encoding/json"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// run.go starts a stage, runs its execution agent, and moves the stage to its review.
//
// It is the chain AGENT_CONTRACTS section 8 describes, in its order:
//
//	load state → start the attempt → assemble the prompt → run the agent →
//	move the stage to reviewing
//
// The last step is the one worth stating. The runtime validates a model's OUTPUT and never touches
// business state; the ENGINE owns the status machine. So a stage that has run is moved to `reviewing`
// HERE, and until it is moved the review has nothing to attach to — the engine's own
// `ApplySupervision` refuses a stage that is still `running`, which is the ordering rule enforced
// rather than trusted.

// StageRequest asks for one attempt at one script stage.
type StageRequest struct {
	WorkflowRunID string
	Stage         Stage
	// ProjectID and EpisodeID scope the run. They come from the caller rather than from a model, and
	// they are what the prompt's state layer and every tool's scope check are built from.
	ProjectID string
	EpisodeID string
	// Task is what this attempt is asked to do, in the caller's words. For a stage that reads the
	// source novel it is document-adjacent text, so the caller states whether it is untrusted.
	Task            string
	TaskIsUntrusted bool
	// UserMessage is the user's own instruction, which is trusted: it is the person the run belongs to,
	// speaking to their own agent.
	UserMessage string
	// FixFromStageRunID, when set, makes this attempt a FIX: the findings the user named on that
	// attempt's decision are read back from the database and rendered into the prompt.
	//
	// It is the ATTEMPT rather than a decision id, because that is what a caller has: a UI showing a
	// failed review knows which stage attempt it is looking at. Asking the caller to fetch the decision
	// first would put the durable read in the caller's hands — and a caller that passed an empty issue
	// list would produce a FIX that fixed nothing while still being recorded as one.
	FixFromStageRunID string
	// ModelID and ProviderID name the model this attempt runs on, resolved by the caller from the
	// project's policy for the agent's layer. The pipeline does not resolve policy: that is a fact
	// about a project's configuration, and the layer that owns it is `agent_wiring`.
	ModelID    string
	ProviderID string
	// SelectedEventIDs, SkeletonVersionID, StrategyVersionID and ScriptVersionID go into the prompt's
	// STATE layer, which is what the stage's tools need in order to name what they write.
	//
	// They come from the caller because the pipeline does not decide them: an episode's current
	// versions are what a stage is run against, and the episode's own columns already answer which
	// those are. Looking them up here would be a second answer to a question the schema answers once.
	//
	// A field the caller leaves empty is OMITTED from the state rather than rendered empty, because a
	// model reading `skeleton_version=` would take it for an identifier that exists and is blank.
	SelectedEventIDs  []string
	SkeletonVersionID string
	StrategyVersionID string
	ScriptVersionID   string
}

// StageResult is what one attempt produced.
type StageResult struct {
	// StageRun is the attempt, in whatever status the run left it: `reviewing` on success, and
	// whatever `StartStage` produced on a refusal.
	StageRun workflow.StageRun
	// Outcome is the runtime's own record of the run, including the run id a caller needs to look it
	// up and the tool calls it made.
	Outcome agentruntime.Outcome
	// ArtifactIDs are the entity identifiers the stage's tool calls created, in the order the calls
	// ran. They are read from the RECORDED CALLS rather than parsed out of the model's answer, because
	// an answer is a claim and a call's result is the row a write returned.
	ArtifactIDs []string
}

// RunStage starts one attempt, runs it, and parks the stage for review.
//
// A REFUSAL LEAVES THE ATTEMPT WHERE THE RUNTIME PUT IT. The attempt was created and moved to
// `running`, and a model failure does not undo that: the stage's record has to say an attempt
// happened, or a reader would see a stage that silently did nothing. The runtime has already recorded
// the run's own status; what this method does not do is move the stage onward, because a failed run
// has no review.
func (s *Service) RunStage(ctx context.Context, request StageRequest) (StageResult, error) {
	if !s.Available() {
		return StageResult{}, unavailable()
	}
	agents, ok := AgentsForStage(request.Stage)
	if !ok {
		return StageResult{}, agentRefusal(request.Stage)
	}
	workflowRunID := trimOrEmpty(request.WorkflowRunID)
	if workflowRunID == "" {
		return StageResult{}, agent.InvalidError("A stage run must name the workflow run it belongs to.")
	}
	// The revision's findings and pins are read BEFORE the attempt exists, because an attempt created
	// and then refused for a missing decision would be a stage in play with nothing to run — and a
	// caller could not tell that from a slow model.
	lockedRefs, fixIssueIDs, err := s.revisionContext(ctx, request)
	if err != nil {
		return StageResult{}, err
	}
	skill, skillVersion, err := s.skillFor(agents.Execution)
	if err != nil {
		return StageResult{}, err
	}
	attempt, err := s.engine.StartStage(ctx, agentruntime.StartStageRequest{
		WorkflowRunID: workflowRunID,
		Stage:         request.Stage,
		ExecutionKey:  agents.Execution,
		InputJSON:     stageInput(request),
		Actor:         stageActor(),
	})
	if err != nil {
		return StageResult{}, err
	}
	outcome, err := s.runtime.Run(ctx, agentruntime.Invocation{
		AgentKey:        agents.Execution,
		ProjectID:       trimOrEmpty(request.ProjectID),
		EpisodeID:       trimOrEmpty(request.EpisodeID),
		WorkflowRunID:   workflowRunID,
		StageRunID:      attempt.ID,
		Skill:           skill,
		SkillVersion:    skillVersion,
		WorkflowState:   s.stateFor(attempt, request),
		Task:            request.Task,
		TaskIsUntrusted: request.TaskIsUntrusted,
		UserMessage:     request.UserMessage,
		LockedRefs:      lockedRefs,
		FixIssueIDs:     fixIssueIDs,
		ModelID:         trimOrEmpty(request.ModelID),
		ProviderID:      trimOrEmpty(request.ProviderID),
	})
	if err != nil {
		// The attempt stays where the runtime left it. A caller retrying finds it in play and the
		// engine refuses a second attempt, which is the right answer: the first has a record, and a
		// re-run is a REVISION rather than a fresh attempt on the same stage.
		return StageResult{StageRun: attempt, Outcome: outcome}, err
	}
	reviewing, err := s.engine.Transition(ctx, agentruntime.StageTransitionRequest{
		StageRunID: attempt.ID,
		Status:     workflow.StageReviewing,
		Actor:      stageActor(),
	})
	if err != nil {
		return StageResult{StageRun: attempt, Outcome: outcome}, err
	}
	return StageResult{
		StageRun:    reviewing,
		Outcome:     outcome,
		ArtifactIDs: artifactIDsOf(outcome),
	}, nil
}

// revisionContext reads what a revision must know: the pins to respect and the findings to address.
//
// The pins come from the base version's lock rows — AC-SCRIPT-002's "锁定字段不变" — and from the
// decision's own `locked_entity_refs_json`; the findings come from the decision. All three are read
// HERE, from the database, and none is accepted as an argument: section 7.3 says lockedRefs and
// fixIssueIDs come "from the database; the agent cannot add to either", and a value the caller supplied
// could.
//
// A first attempt has neither, and that is not a gap: there is no base version to pin a field on and no
// decision to read findings from.
func (s *Service) revisionContext(ctx context.Context, request StageRequest) ([]agentruntime.LockedRef, []string, error) {
	base := trimOrEmpty(request.FixFromStageRunID)
	if base == "" {
		return nil, nil, nil
	}
	decision, ok, err := s.workflow.LatestDecisionForStage(ctx, base)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		// A revision with no decision is a caller error rather than an empty revision: the whole point
		// of naming the attempt is that a user decided something about it.
		return nil, nil, agent.InvalidError("That stage attempt has no user decision to revise against.")
	}
	findings, err := issueIDsOf(decision.IssueIDsJSON)
	if err != nil {
		return nil, nil, err
	}
	locks, err := s.locksFor(ctx, decision, request.Stage)
	if err != nil {
		return nil, nil, err
	}
	return locks, findings, nil
}

// locksFor renders the pins a revision must respect.
//
// Two sources, because there are two kinds of pin. The decision's `locked_entity_refs_json` is what a
// user pinned while reviewing — section 7.3's lockedRefs, which the gate command stores. And the base
// VERSION's own lock rows are AC-SCRIPT-002's field locks, which is what makes "锁定字段不变"
// enforceable.
//
// The version is found through the ATTEMPT the decision names, and through the run's recorded tool
// calls rather than through a model's answer: the write is what proves the row exists.
func (s *Service) locksFor(ctx context.Context, decision workflow.UserGateDecision, stage Stage) ([]agentruntime.LockedRef, error) {
	refs, err := lockedRefsOf(decision.LockedEntityRefsJSON)
	if err != nil {
		return nil, err
	}
	agents, ok := AgentsForStage(stage)
	if !ok {
		return nil, agentRefusal(stage)
	}
	versionID, err := s.versionOfDecision(ctx, decision, agents)
	if err != nil {
		return nil, err
	}
	if versionID == "" {
		// The attempt produced no version — a failed or superseded attempt. There are no field locks
		// to state, and the decision's own pins still travel.
		return refs, nil
	}
	locks, err := s.script.ListScriptFieldLocks(ctx, versionID)
	if err != nil {
		return nil, err
	}
	for _, lock := range locks {
		if lock.Family != agents.Family {
			// The lock table has no foreign key across three version tables, so this is a corrupt row.
			// Fail closed rather than skip: a pin that reads as a protection and enforces nothing is
			// worse than no pin, which is the rule the lock vocabulary itself states.
			return nil, agent.SecurityError("A lock on that version names a different artifact family.")
		}
		refs = append(refs, agentruntime.LockedRef{
			EntityType: string(agents.Family) + "_version",
			EntityID:   versionID,
			Field:      string(lock.Field),
			Label:      "pinned by the user: " + string(lock.Field),
		})
	}
	return refs, nil
}

// versionOfDecision finds the version one attempt produced.
//
// It walks the decision's stage attempt to the RUNS that attempt owns, and then through their recorded
// TOOL CALLS. That is the long way round, and it is the honest one: the write is what proves a version
// exists, and the alternative — a version id stored on the decision or parsed out of a model's answer —
// would be a claim. A run whose call was recorded but whose output could not be read yields no id, and
// the caller then has no field locks to state rather than a lock on the wrong row.
func (s *Service) versionOfDecision(ctx context.Context, decision workflow.UserGateDecision, agents StageAgents) (string, error) {
	stageRunID := trimOrEmpty(decision.StageRunID)
	if stageRunID == "" {
		return "", nil
	}
	runs, err := s.runsForStage(ctx, decision.WorkflowRunID, stageRunID)
	if err != nil {
		return "", err
	}
	// The write tool's key is what identifies the artifact, and matching on it rather than on the
	// result's shape keeps this honest: a call whose result could not be parsed still names what it
	// tried to write.
	writeKey := "script.create_" + agents.ArtifactType
	for _, run := range runs {
		calls, err := s.runs.ListToolCalls(ctx, run.ID)
		if err != nil {
			return "", err
		}
		for _, call := range calls {
			if call.ToolKey != writeKey {
				continue
			}
			if id := entityIDOf(call.OutputJSON); id != "" {
				return id, nil
			}
		}
	}
	return "", nil
}

// runsForStage returns the agent runs one stage attempt owns.
//
// The engine has no read for this and the workflow service's own reads are about stages rather than
// runs, so the walk goes through the inspector's project-scoped list: the runs of a project, narrowed
// to the attempt. The limit is the inspector's ceiling, and it is the RIGHT ceiling for this question
// because a single stage attempt owns a handful of runs even when a stage has been revised many times.
func (s *Service) runsForStage(ctx context.Context, projectID, stageRunID string) ([]agent.AgentRun, error) {
	if s.runs == nil {
		return nil, agent.UnavailableError()
	}
	runs, err := s.runs.ListRuns(ctx, projectID, agentruntime.MaxRunLimit)
	if err != nil {
		return nil, err
	}
	owned := make([]agent.AgentRun, 0, 4)
	for _, run := range runs {
		if run.StageRunID == stageRunID {
			owned = append(owned, run)
		}
	}
	return owned, nil
}

// skillFor reads an agent's skill and the version its run must cite.
//
// A missing document is a REFUSAL rather than an empty prompt: a run with no skill would be a run this
// build's pack never described, and the model would answer from the policy layer alone — exactly the
// "undescribed agent" the pack format exists to prevent.
func (s *Service) skillFor(agentKey string) (string, string, error) {
	document, ok := s.assembly.SkillDocument(agentKey)
	if !ok || strings.TrimSpace(document) == "" {
		return "", "", agent.NotFoundError()
	}
	version, ok := s.assembly.SkillVersionOf(agentKey)
	if !ok {
		// §4.2 requires a run to name the skill version it ran. A run whose version could not be
		// resolved would cite nothing, which is worse than not running: the record would say a skill was
		// used and not which one.
		return "", "", agent.NotFoundError()
	}
	return document, version, nil
}

// stageActor is the attribution every stage transition this pipeline makes records.
//
// A SYSTEM actor rather than an agent one, and the distinction is what keeps the audit readable: a
// status the stage machine moved is not a status a model chose. The domain's own decision validation
// makes the same point from the other side — it refuses a USER decision written by an agent (§11.5).
func stageActor() agentruntime.Actor {
	return agentruntime.Actor{Type: "system", ID: "scriptpipeline"}
}

// stageInput renders what a caller asked for, for the attempt's own record.
//
// It carries identifiers and the task, and NO model output: the run has its own record with the
// transcript, and a second copy here could disagree with it.
func stageInput(request StageRequest) string {
	document := map[string]any{
		"projectId": trimOrEmpty(request.ProjectID),
		"episodeId": trimOrEmpty(request.EpisodeID),
	}
	if task := strings.TrimSpace(request.Task); task != "" {
		document["task"] = task
	}
	if base := trimOrEmpty(request.FixFromStageRunID); base != "" {
		document["fixFromStageRunId"] = base
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		// An input document that will not marshal is this file's bug. An empty object is recorded
		// rather than failing the attempt: the record's job is to say an attempt happened.
		return "{}"
	}
	return string(encoded)
}

// artifactIDsOf reads the identifiers a stage's tool calls wrote.
//
// From the run's TOOL CALLS rather than the model's answer, which is the whole point of AC-AGENT-003: an
// answer is a claim about what was produced, and a call's result is the row the write returned. A stage
// that reported an artifact it never wrote is exactly what this avoids copying into the record.
func artifactIDsOf(outcome agentruntime.Outcome) []string {
	ids := make([]string, 0, len(outcome.ToolCalls))
	for _, call := range outcome.ToolCalls {
		if id := entityIDOf(call.OutputJSON); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// entityIDOf reads the entity identifier out of a tool result.
//
// The write tools all return `artifacts: [{entityId, ...}]`, which is the shape `artifactResult` builds
// and the shape the runtime's verifier reads. A result that is not that shape yields "" rather than an
// error: a call whose result could not be parsed is a call that produced nothing this pipeline can
// name, and the caller's next step is to look at the run rather than to fail the stage twice.
func entityIDOf(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var decoded struct {
		Artifacts []struct {
			EntityID string `json:"entityId"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return ""
	}
	for _, artifact := range decoded.Artifacts {
		if id := strings.TrimSpace(artifact.EntityID); id != "" {
			return id
		}
	}
	return ""
}

// scriptFamily is the version family a stage's artifact belongs to.
//
// It is the domain's type under a local alias so this file's signatures read without the domain prefix.
type scriptFamily = script.VersionFamily
