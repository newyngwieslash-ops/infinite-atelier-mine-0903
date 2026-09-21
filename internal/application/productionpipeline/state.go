package productionpipeline

import (
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// state.go renders the workflow-state layer a production stage's prompt carries.
//
// It is the same shape the script layer's renderer uses — `name=value` pairs separated by
// spaces — and for the same reason: the deterministic mock parses this string, and the
// field names are constants so the two sides cannot drift without a test failing.
//
// THE FIELDS ARE THE IDENTIFIERS THIS STAGE'S TOOLS NEED, which is what makes the state
// layer load-bearing rather than decorative. §7.1 forbids a tool from taking a project or
// episode from a model, so a stage whose state omitted the episode would have an execution
// agent whose every write was refused by its own schema — and the refusal would look like
// the model's mistake.

// State field names, as the mock's `fieldOnLine` reads them.
const (
	fieldStage        = "stage="
	fieldStatus       = "status="
	fieldAttempt      = "attempt="
	fieldStageRun     = "stage_run="
	fieldWorkflow     = "workflow_run="
	fieldEpisode      = "episode="
	fieldScript       = "script_version="
	fieldDirectorPlan = "director_plan_version="
	fieldStoryboardID = "storyboard="
	fieldStoryboard   = "storyboard_version="
	fieldItem         = "storyboard_item="
	fieldGapReport    = "asset_gap_report="
	// fieldShotIDs is the comma-joined list of shots the storyboard stage is boarding.
	//
	// It travels because the stage's write tool CHECKS every row's citation against the
	// script's own shots — `storyboard_items.shot_id` has no foreign key, so the handler is
	// what keeps an invented id out — and a model that could not see the ids could only
	// guess them. The convention is the script layer's for a list in one field:
	// comma-separated inside it, because the separator between fields is a space.
	fieldShotIDs = "shot_ids="
)

// renderState renders the prompt's workflow-state layer for one attempt.
//
// Every field is stated when it is known and OMITTED when it is not, because a field
// rendered as an empty value would read to a model as an identifier that exists and is
// empty. The episode is the one field that always travels when the caller stated one:
// every write tool in this package names an episode somewhere, and a stage that could not
// name it could not write at all.
func renderState(attempt workflow.StageRun, request stagepipeline.StageRequest, fields StateFields) string {
	parts := []string{
		fieldWorkflow + strings.TrimSpace(request.WorkflowRunID),
		fieldStage + string(request.Stage),
		fieldStatus + string(attempt.Status),
		fieldAttempt + itoa(attempt.Attempt),
		fieldStageRun + attempt.ID,
	}
	// The episode travels from the REQUEST first and the layer's own fields second: a
	// caller that scoped its request has said where the run is, and a layer field naming
	// another episode would be a contradiction rather than a default.
	episode := strings.TrimSpace(request.EpisodeID)
	if episode == "" {
		episode = trimmed(fields.EpisodeID)
	}
	if episode != "" {
		parts = append(parts, fieldEpisode+episode)
	}
	// The shot list travels whole and comma-joined, and it is emitted only when it has one:
	// an empty `shot_ids=` would read to a model as "the script has shots and none were
	// named", which is a different statement from "this stage was not given them".
	if len(fields.ShotIDs) > 0 {
		parts = append(parts, fieldShotIDs+strings.Join(fields.ShotIDs, ","))
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{fieldScript, fields.ScriptVersionID},
		{fieldDirectorPlan, fields.DirectorPlanVersionID},
		{fieldStoryboardID, fields.StoryboardID},
		{fieldStoryboard, fields.StoryboardVersionID},
		{fieldItem, fields.StoryboardItemID},
		{fieldGapReport, fields.AssetGapReportID},
	} {
		if value := trimmed(field.value); value != "" {
			parts = append(parts, field.name+value)
		}
	}
	return strings.Join(parts, " ")
}
