package stagepipeline

import (
	"encoding/json"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// helpers.go holds the small readers the mechanism shares: the two JSON columns on a
// decision, the tool-result shape, and the supervisor's answer.
//
// They are EXPORTED because each layer's own tests and its own state rendering need the
// same answers, and a second implementation is a second opinion about what a malformed
// decision row means. The one thing they are not is a place to put layer knowledge:
// everything here is about the SHAPE of a document the schema or the engine already fixed.

// IssueIDsOf reads the finding identifiers a decision names.
//
// The column is `issue_ids_json TEXT NOT NULL DEFAULT '[]'`, so the ordinary value is an
// empty array and a malformed one is a corrupt row rather than a user's intent — which is
// why a parse failure is a REFUSAL: a FIX that could not read its findings would re-run
// the stage from scratch while being recorded as a FIX.
func IssueIDsOf(raw string) ([]string, error) {
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

// LockedRefsOf reads the pins a decision names.
//
// The column is `locked_entity_refs_json TEXT NOT NULL DEFAULT ”`, and an EMPTY STRING is
// the ordinary value rather than an empty array — which is why the empty case is checked
// before parsing. The shape is the one section 7.3 describes: entityType, entityId, and an
// optional field.
//
// A pin with no identifier is DROPPED rather than passed on: a reference the prompt states
// but no tool can resolve would send the model looking for a row that does not exist.
func LockedRefsOf(raw string) ([]agentruntime.LockedRef, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
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

// EntityIDOf reads the entity identifier out of a tool result.
//
// The write tools all return `artifacts: [{entityId, ...}]`, which is the shape
// `agenttools.artifactResult` builds and the shape the runtime's verifier reads. A result
// that is not that shape yields "" rather than an error: a call whose result could not be
// parsed is a call that produced nothing this pipeline can name, and the caller's next
// step is to look at the run rather than to fail the stage twice.
//
// It scans the LIST rather than returning the first entry, because a write tool may report
// several references and the first may have an empty id — which is what a mutation run
// found surviving when this returned `Artifacts[0].EntityID` unconditionally.
func EntityIDOf(raw string) string {
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

// ArtifactIDsOf reads the identifiers a stage's tool calls wrote.
//
// From the run's TOOL CALLS rather than the model's answer, which is the whole point of
// AC-AGENT-003: an answer is a claim about what was produced, and a call's result is the
// row the write returned. A stage that reported an artifact it never wrote is exactly what
// this avoids copying into the record.
func ArtifactIDsOf(outcome agentruntime.Outcome) []string {
	ids := make([]string, 0, len(outcome.ToolCalls))
	for _, call := range outcome.ToolCalls {
		if id := EntityIDOf(call.OutputJSON); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// ReviewFromOutcome reads the verdict out of a supervisor's validated output.
//
// The output's SHAPE is the review-report contract, which the runtime has already
// validated, so this reads fields the schema guarantees rather than defending against a
// malformed document. What it does defend against is a document that is valid and says
// nothing usable — and a report naming no RULESET, which section 7.6 requires because a
// report that named none could not be reproduced.
func ReviewFromOutcome(outcome agentruntime.Outcome, supervisorKey string) (workflow.ReviewReport, error) {
	var document struct {
		Passed            bool   `json:"passed"`
		Severity          string `json:"severity"`
		RulesetVersion    string `json:"rulesetVersion"`
		RecommendedAction string `json:"recommendedAction"`
		Summary           string `json:"summary"`
	}
	if err := json.Unmarshal(outcome.Output, &document); err != nil {
		return workflow.ReviewReport{}, agent.InvalidError("The review report could not be read.")
	}
	report := workflow.ReviewReport{
		Passed:            document.Passed,
		Severity:          workflow.Severity(document.Severity),
		RulesetVersion:    strings.TrimSpace(document.RulesetVersion),
		RecommendedAction: strings.TrimSpace(document.RecommendedAction),
		Summary:           document.Summary,
		SupervisorKey:     supervisorKey,
	}
	if report.RulesetVersion == "" {
		return workflow.ReviewReport{}, agent.InvalidError("The review report names no ruleset version.")
	}
	return report, nil
}

// IssuesFromOutcome reads a report's findings out of the same validated output.
//
// The identifiers are MINTED by the workflow service, not here, so each finding carries
// the fields the report states and nothing that identifies it — the same division the
// write tools use, and for the same reason: an identifier a model supplies is one it can
// get wrong.
func IssuesFromOutcome(outcome agentruntime.Outcome) ([]appworkflow.ReviewIssueInput, error) {
	var document struct {
		Issues []struct {
			Rule         string `json:"rule"`
			Severity     string `json:"severity"`
			EntityType   string `json:"entityType"`
			EntityID     string `json:"entityId"`
			Location     string `json:"location"`
			Field        string `json:"field"`
			Problem      string `json:"problem"`
			Suggestion   string `json:"suggestion"`
			AutoFixable  bool   `json:"autoFixable"`
			EvidenceJSON string `json:"evidenceJson"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(outcome.Output, &document); err != nil {
		return nil, agent.InvalidError("The review report's findings could not be read.")
	}
	out := make([]appworkflow.ReviewIssueInput, 0, len(document.Issues))
	for _, issue := range document.Issues {
		severity := workflow.Severity(strings.TrimSpace(issue.Severity))
		if !workflow.IsValidSeverity(severity) {
			// A finding whose severity is unknown cannot be routed — the engine's
			// "critical 强制人工门" reads it — so it is refused rather than defaulted.
			// Defaulting would silently decide how urgent somebody else's problem is.
			return nil, agent.InvalidError("A finding states a severity that is not recognised.")
		}
		out = append(out, appworkflow.ReviewIssueInput{
			Rule:         strings.TrimSpace(issue.Rule),
			Severity:     severity,
			EntityType:   strings.TrimSpace(issue.EntityType),
			EntityID:     strings.TrimSpace(issue.EntityID),
			Location:     issue.Location,
			Field:        strings.TrimSpace(issue.Field),
			Problem:      issue.Problem,
			Suggestion:   issue.Suggestion,
			EvidenceJSON: issue.EvidenceJSON,
			AutoFixable:  issue.AutoFixable,
		})
	}
	return out, nil
}

// IsApprovingDecision reports whether a decision makes the artifact the one in force.
//
// Three of §10.2's decisions do, and they do it for different reasons: `approve` accepts
// the artifact, `manual_edit` makes the user's own version the artifact, and `skip` moves
// on without judging it. A WAIVER is not in the list because §15.3's waiver accepts a
// STALE artifact rather than approving a new one — the version in force is the one already
// approved. `fix` and `redo` do not, and `cancel` ends the run.
func IsApprovingDecision(decision workflow.GateDecision) bool {
	switch decision {
	case workflow.GateApprove, workflow.GateManualEdit, workflow.GateSkip:
		return true
	default:
		return false
	}
}

// supervisionTask states what the reviewer is asked to review.
//
// It names the artifact by REFERENCE and states the ruleset the report must cite, which
// are the two things the supervision contract requires of the request. It carries no
// content of the artifact: a supervisor loads what it reviews with its own read tools,
// which is what makes "Supervisor 读取 DB" a property rather than a claim.
func supervisionTask(attempt workflow.StageRun, request SupervisionRequest) string {
	parts := []string{
		"Review the " + string(attempt.Stage) + " attempt " + attempt.ID + ".",
	}
	if version := trimOrEmpty(request.ArtifactVersionID); version != "" {
		parts = append(parts, "The artifact under review is version "+version+"; load it with your own read tools.")
	} else {
		parts = append(parts, "Load the artifact this attempt produced with your own read tools.")
	}
	// The ruleset is what the findings are checked against, and §7.6 requires the report to
	// name it. It is stated as a source the reviewer reads rather than as a version to
	// repeat, because the version a PROJECT has is a fact about the project and this
	// pipeline does not read it.
	parts = append(parts, "Cite the ruleset version you reviewed against, and read the project's rules before judging.")
	return strings.Join(parts, " ")
}

// userActor is the attribution a user's own command records.
func userActor(createdByID string) agentruntime.Actor {
	return agentruntime.Actor{Type: "user", ID: trimOrEmpty(createdByID)}
}
