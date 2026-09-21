package scriptpipeline

import (
	"encoding/json"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// state.go renders the workflow-state layer a stage's prompt carries, and reads the small JSON
// documents the pipeline passes around.
//
// THE STATE IS RENDERED HERE RATHER THAN read back from the database, and that is a deliberate
// difference from the workflow service's own list view: §5's layer 5 is what the MODEL is told about
// where the run stands, and it has to name the identifiers this stage's tools will need. A stage that
// was told only its status could not name the episode it writes for, and its write tool would be
// refused by its own schema — which is exactly the failure the mock's state reading exists to avoid.
//
// The format is `name=value` pairs separated by spaces, one line per group, and it is a FORMAT rather
// than a serialization: the deterministic mock parses it with `fieldOnLine`, and a test that drove a
// stage through the models would need the same field names. So the names are constants below, and the
// two sides cannot drift without a test failing.

// State field names, as the mock's `fieldOnLine` reads them.
//
// They are constants rather than literals because three places depend on them agreeing: this file's
// renderer, the mock that answers these stages, and the tests that assert both.
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

// stateFor renders the prompt's workflow-state layer for one attempt.
//
// Every field is stated when it is known and OMITTED when it is not, because a field rendered as an
// empty value would read to a model as an identifier that exists and is empty. The selected-event list
// is comma-separated inside one field, which is the convention `fieldOnLine`'s space separator
// requires: a list rendered with spaces would end the field at its first element.
//
// The version identifiers come from the CALLER, because the pipeline does not read them: the episode's
// current versions are what a stage is run against, and looking them up here would mean deciding which
// version is "current" — a question the episode's own columns already answer, and one this package has
// no business answering twice.
func (s *Service) stateFor(attempt workflow.StageRun, request StageRequest) string {
	fields := []string{
		fieldWorkflow + trimOrEmpty(request.WorkflowRunID),
		fieldStage + string(request.Stage),
		fieldStatus + string(attempt.Status),
		fieldAttempt + itoa(attempt.Attempt),
		fieldStageRun + attempt.ID,
	}
	if episode := trimOrEmpty(request.EpisodeID); episode != "" {
		fields = append(fields, fieldEpisode+episode)
	}
	// The pins, so a model can see what it must not rewrite without needing the prompt layer — the
	// layer is the instruction, and this is the state the instruction is about.
	if len(request.SelectedEventIDs) > 0 {
		fields = append(fields, fieldSelected+strings.Join(request.SelectedEventIDs, ","))
	}
	if version := trimOrEmpty(request.SkeletonVersionID); version != "" {
		fields = append(fields, fieldSkeleton+version)
	}
	if version := trimOrEmpty(request.StrategyVersionID); version != "" {
		fields = append(fields, fieldStrategy+version)
	}
	if version := trimOrEmpty(request.ScriptVersionID); version != "" {
		fields = append(fields, fieldScript+version)
	}
	return strings.Join(fields, " ")
}

// issueIDsOf reads the finding identifiers a decision names.
//
// The column is `issue_ids_json TEXT NOT NULL DEFAULT '[]'`, so the ordinary value is an empty array
// and a malformed one is a corrupt row rather than a user's intent — which is why a parse failure is a
// REFUSAL: a FIX that could not read its findings would re-run the stage from scratch while being
// recorded as a FIX.
func issueIDsOf(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(trimmed), &ids); err != nil {
		return nil, agent.InvalidError("That decision's findings could not be read, so the revision cannot be run against them.")
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmedID := strings.TrimSpace(id); trimmedID != "" {
			out = append(out, trimmedID)
		}
	}
	return out, nil
}

// lockedRefsOf reads the pins a decision names.
//
// The column is `locked_entity_refs_json TEXT NOT NULL DEFAULT ”`, and an EMPTY STRING is the ordinary
// value rather than an empty array — which is why the empty case is checked before parsing. The shape
// is the one section 7.3 describes: entityType, entityId, and an optional field.
func lockedRefsOf(raw string) ([]agentruntime.LockedRef, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	// The gate command may store a bare `[]` or the section's array of references; both are read, and
	// anything else is refused for the reason issueIDsOf gives.
	var decoded []struct {
		EntityType string `json:"entityType"`
		EntityID   string `json:"entityId"`
		Field      string `json:"field"`
		Label      string `json:"label"`
	}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, agent.InvalidError("That decision's pinned references could not be read.")
	}
	out := make([]agentruntime.LockedRef, 0, len(decoded))
	for _, ref := range decoded {
		entityID := strings.TrimSpace(ref.EntityID)
		if entityID == "" {
			// A pin with no identifier names nothing, so it is dropped rather than passed on: a
			// reference the prompt states but no tool can resolve would send the model looking for a
			// row that does not exist.
			continue
		}
		out = append(out, agentruntime.LockedRef{
			EntityType: strings.TrimSpace(ref.EntityType),
			EntityID:   entityID,
			Field:      strings.TrimSpace(ref.Field),
			Label:      strings.TrimSpace(ref.Label),
		})
	}
	return out, nil
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
