package agenttools

import (
	"context"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// scope.go holds the project-boundary checks the handlers share.
//
// AGENT_CONTRACTS section 7.1 requires "每次 Tool Call 复检 project/episode scope", and
// these are that check. They exist as one function each rather than inline in every
// handler for the reason a repeated security check is worth centralizing: the first
// version of this file had the check written out eight times, and a reviewer cannot
// tell by reading eight copies whether they agree. One function is checkable.
//
// The direction of the walk is fixed by the schema: a script artifact names an EPISODE,
// an episode names a PROJECT, and an artifact's own row never carries a project id.
// So the check always loads the episode and compares — never the reverse, which would
// mean reading the artifact before knowing whether the caller may see it.

// assertEpisodeInProject refuses an episode that belongs to another project.
//
// A missing episode is a not-found rather than a refusal, because the two are different
// situations: one is a stale identifier, the other is an attempt to reach across a
// boundary. Reporting them the same way would tell a caller which identifiers exist in
// a project they cannot see, which is the leak the check is for.
func assertEpisodeInProject(ctx context.Context, deps Deps, projectID, episodeID string) error {
	trimmed := strings.TrimSpace(episodeID)
	if trimmed == "" {
		return agent.InvalidError("That artifact names no episode, so its project cannot be established.")
	}
	episode, err := deps.Script.GetEpisode(ctx, trimmed)
	if err != nil {
		return err
	}
	if episode.ProjectID != projectID {
		// The message is about the rule rather than about the artifact, for the reason
		// the tool ACL's refusal is: a caller must not be able to learn what an artifact
		// would have contained by reading the refusal.
		return agent.SecurityError("That artifact belongs to another project.")
	}
	return nil
}

// stageRunInProject reports whether a stage attempt belongs to a project.
//
// It is the walk the workflow tools need: a stage names its run, and a run names its
// project. The stage row has no project of its own, which is why two reads are
// required rather than one.
func stageRunInProject(ctx context.Context, deps Deps, projectID, stageRunID string) (bool, error) {
	stage, err := deps.Workflow.GetStage(ctx, strings.TrimSpace(stageRunID))
	if err != nil {
		return false, err
	}
	run, err := deps.Workflow.GetRun(ctx, stage.WorkflowRunID)
	if err != nil {
		return false, err
	}
	return run.ProjectID == projectID, nil
}

// Compile-time proof that a handler has the shape the table binds. A signature drift in
// the runtime's ToolHandler breaks this build rather than the table's.
var _ agentruntime.ToolHandler = func(context.Context, agentruntime.ToolRequest) (any, error) {
	return nil, nil
}
