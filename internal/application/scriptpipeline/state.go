package scriptpipeline

import (
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// state.go renders the workflow-state layer a script stage's prompt carries.
//
// THE STATE IS RENDERED HERE RATHER THAN read back from the database, and that is a
// deliberate difference from the workflow service's own list view: §5's layer 5 is what the
// MODEL is told about where the run stands, and it has to name the identifiers this stage's
// tools will need. A stage that was told only its status could not name the episode it
// writes for, and its write tool would be refused by its own schema — which is exactly the
// failure the mock's state reading exists to avoid.
//
// The format is `name=value` pairs separated by spaces, and it is a FORMAT rather than a
// serialization: the deterministic mock parses it with `fieldOnLine`, and a test that drove
// a stage through the models would need the same field names. So the names are constants
// below, and the two sides cannot drift without a test failing.

// State field names, as the mock's `fieldOnLine` reads them.
//
// They are constants rather than literals because three places depend on them agreeing:
// this file's renderer, the mock that answers these stages, and the tests that assert both.
const (
	fieldStage      = "stage="
	fieldStatus     = "status="
	fieldAttempt    = "attempt="
	fieldEpisode    = "episode="
	fieldStageRun   = "stage_run="
	fieldWorkflow   = "workflow_run="
	fieldSkeleton   = "skeleton_version="
	fieldStrategy   = "strategy_version="
	fieldScript     = "script_version="
	fieldSelected   = "selected_events="
	fieldLockedRefs = "locked_refs="
)

// renderState renders the prompt's workflow-state layer for one attempt.
//
// Every field is stated when it is known and OMITTED when it is not, because a field
// rendered as an empty value would read to a model as an identifier that exists and is
// empty. The selected-event list is comma-separated inside one field, which is the
// convention `fieldOnLine`'s space separator requires: a list rendered with spaces would
// end the field at its first element.
func renderState(attempt workflow.StageRun, request stagepipeline.StageRequest, fields StateFields) string {
	parts := []string{
		fieldWorkflow + strings.TrimSpace(request.WorkflowRunID),
		fieldStage + string(request.Stage),
		fieldStatus + string(attempt.Status),
		fieldAttempt + itoa(attempt.Attempt),
		fieldStageRun + attempt.ID,
	}
	if episode := strings.TrimSpace(request.EpisodeID); episode != "" {
		parts = append(parts, fieldEpisode+episode)
	}
	// The selection travels so a strategy stage knows which events it must decide about: an
	// empty list would let it write a version that treated nothing, which the service
	// accepts and no reviewer could tell from a strategy about a different episode.
	if len(fields.SelectedEventIDs) > 0 {
		parts = append(parts, fieldSelected+strings.Join(fields.SelectedEventIDs, ","))
	}
	if version := strings.TrimSpace(fields.SkeletonVersionID); version != "" {
		parts = append(parts, fieldSkeleton+version)
	}
	if version := strings.TrimSpace(fields.StrategyVersionID); version != "" {
		parts = append(parts, fieldStrategy+version)
	}
	if version := strings.TrimSpace(fields.ScriptVersionID); version != "" {
		parts = append(parts, fieldScript+version)
	}
	return strings.Join(parts, " ")
}

// itoa renders a small non-negative integer without importing strconv for one call site.
func itoa(value int) string {
	if value <= 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
