// Package consistency holds the deterministic cross-stage checks of WP-10's scope items 13 and 14,
// "Consistency deterministic checks" and "Character/Costume/Prop/Location continuity".
//
// # Why deterministic checks exist at all, given a supervisor
//
// AGENT_CONTRACTS section 11.4 states the rule this package is the implementation of:
//
//	硬规则应尽量用确定性代码先检查，LLM Supervisor 负责语义质量。ReviewReport 合并两类证据，
//	并标记 source=deterministic|llm。
//
// The division is about which of the two can be WRONG. A supervisor reading a storyboard may
// notice that a costume changed between two rows — or may not, and its answer varies with the
// model, the temperature and the day. A row that cites a costume version the project did not
// approve is not a matter of judgement: it is a join, and code answers it the same way every time.
// So the mechanical half of a ruleset is code, and the model is left the part that needs reading.
//
// AC-E2E-004 is the acceptance criterion that shows why it matters: "人为制造一个角色服装引用错误
// — Supervisor 定位到具体 Shot". The fault is injected deliberately, and the check has to find it
// reliably enough that the test can assert it did.
//
// # The findings are the SAME shape the supervisor reports
//
// `Finding` mirrors `workflow.ReviewIssueInput` field for field, because section 11.4 requires the
// two kinds of evidence to MERGE into one report rather than sit in two. The merge happens in
// stagepipeline, which is where both are available; this package only knows how to produce them.
//
// # The rules are stated so each one can be checked
//
// Every rule names what it read, what it expected and what it found, so a reader can decide whether
// the rule is right rather than only whether it fired. The alternative — a rule that reports "the
// storyboard is inconsistent" — would be unusable by the person who has to fix it, which is the
// same standard FR-110's report shape sets.
package consistency

import (
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// Rule identifiers, in the `DOMAIN_CONCEPT_CHECK` form FR-110's example uses
// (`CHARACTER_CONTINUITY`). They are stable strings because they end up on review_issues rows that
// a user reads and a later ruleset compares against.
const (
	// RuleCostumeContinuity is scope item 14's costume clause: a row's costume reference must agree
	// with the costume state in force at that point in the story.
	RuleCostumeContinuity = "CHARACTER_CONTINUITY"
	// RulePropContinuity is scope item 14's prop clause: a row that cites a prop must cite one the
	// scene's own story event involves.
	RulePropContinuity = "PROP_CONTINUITY"
	// RuleLocationContinuity is scope item 14's location clause: rows of one scene must agree about
	// where it happens, and the location must exist and be approved.
	RuleLocationContinuity = "LOCATION_CONTINUITY"
	// RuleShotCoverage is section 11.3's "剧本覆盖" and "缺失镜头": every shot of the approved script
	// has exactly one row, in order.
	RuleShotCoverage = "SHOT_COVERAGE"
	// RuleDuration is section 11.3's "时长总和": the rows' durations must add up to the script's own
	// estimate.
	RuleDuration = "DURATION_TOTAL"
	// RuleAssetApproved is section 11.2's "Approved Version": a cited asset version must be the one
	// currently in force.
	RuleAssetApproved = "ASSET_APPROVED_VERSION"
)

// Severities reuse the workflow domain's vocabulary rather than inventing one, because the merged
// report is a single document: a deterministic finding and a supervisor finding have to be
// comparable for the report's overall severity to mean anything.
const (
	SeverityCritical = workflow.SeverityCritical
	SeverityMajor    = workflow.SeverityMajor
	SeverityMinor    = workflow.SeverityMinor
)

// Finding is one deterministic problem.
//
// It is a distinct type from the workflow's input so this package does not depend on the workflow
// service's request shapes, but its fields are that type's field for field: the merge in
// stagepipeline converts one to the other, and a field added here without a home there would be a
// finding that silently loses its evidence.
type Finding struct {
	Rule       string
	Severity   workflow.Severity
	EntityType string
	EntityID   string
	Location   string
	Field      string
	Problem    string
	Suggestion string
	// Evidence are the references a reader follows to check the finding. FR-110's own example cites
	// two ("shot_012", "asset_version_004"), and the shape exists so a report can say both what is
	// wrong and what it is wrong ABOUT.
	Evidence []Evidence
	// AutoFixable reports whether the fix is mechanical. A costume reference that disagrees with the
	// state in force IS mechanical — pointing it at the right version is the whole fix — which is
	// what makes AC-E2E-004's FIX step meaningful rather than a rewrite.
	AutoFixable bool
}

// Evidence is one reference behind a finding.
//
// The two-field shape is the review-report schema's (`$defs.evidence`: a type and a ref), because
// these findings are stored in the same column the supervisor's evidence uses.
type Evidence struct {
	Type string
	Ref  string
}

// Blocker reports whether a finding must prevent a pass.
//
// AIF-004's classification, as this package applies it: a critical or major finding is a failure,
// and a minor one is a remark. The rule matters because it is what makes the merge in stagepipeline
// safe — a deterministic blocker forces `passed=false` even when the supervisor was happy.
func (f Finding) Blocker() bool {
	return f.Severity == SeverityCritical || f.Severity == SeverityMajor
}

// Blockers filters a finding list down to the ones that must prevent a pass.
//
// It is exported because two callers need the same answer: the merge, which decides `passed`, and
// the batch gate, which decides whether a board may go to image generation. A second filter would
// be a second opinion about which severities block.
func Blockers(findings []Finding) []Finding {
	blocking := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if finding.Blocker() {
			blocking = append(blocking, finding)
		}
	}
	return blocking
}

// Sort orders findings so a report is reproducible.
//
// Rule first, then the entity, then the field: the same faults on the same artifact must come back
// in the same order every run, because a review report is compared across versions and a list that
// reordered itself would look like a changed set of problems.
func Sort(findings []Finding) []Finding {
	ordered := make([]Finding, len(findings))
	copy(ordered, findings)
	for index := 1; index < len(ordered); index++ {
		current := ordered[index]
		position := index - 1
		for position >= 0 && before(current, ordered[position]) {
			ordered[position+1] = ordered[position]
			position--
		}
		ordered[position+1] = current
	}
	return ordered
}

// before reports whether a sorts ahead of b.
func before(a, b Finding) bool {
	if a.Rule != b.Rule {
		return a.Rule < b.Rule
	}
	if a.EntityID != b.EntityID {
		return a.EntityID < b.EntityID
	}
	return a.Field < b.Field
}

// Dedupe removes findings that say the same thing about the same place.
//
// It is what makes the MERGE safe in both directions. A supervisor that read the deterministic
// findings and reported one anyway produces a duplicate, and a rule that fires on two of its own
// passes would do the same. The key is the rule, the entity and the field: two findings about the
// same field of the same entity by the same rule are one finding, and the more severe of the two
// wins because a duplicate cannot make a problem less serious.
func Dedupe(findings []Finding) []Finding {
	seen := map[string]int{}
	deduped := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		key := strings.Join([]string{finding.Rule, finding.EntityType, finding.EntityID, finding.Field}, "\x00")
		index, ok := seen[key]
		if !ok {
			seen[key] = len(deduped)
			deduped = append(deduped, finding)
			continue
		}
		if severityRank(finding.Severity) > severityRank(deduped[index].Severity) {
			// The more severe one wins, and its evidence is kept: the finding that replaces another
			// has to be at least as well supported.
			deduped[index] = finding
		}
	}
	return deduped
}

// severityRank orders severities for comparison.
func severityRank(severity workflow.Severity) int {
	switch severity {
	case SeverityCritical:
		return 3
	case SeverityMajor:
		return 2
	case SeverityMinor:
		return 1
	default:
		return 0
	}
}

// WorstSeverity returns the most severe of a list.
//
// The empty list answers SeverityNone, which is the vocabulary's name for "nothing to report" — the
// report's own default for a clean pass.
func WorstSeverity(findings []Finding) workflow.Severity {
	worst := workflow.SeverityNone
	rank := 0
	for _, finding := range findings {
		if candidate := severityRank(finding.Severity); candidate > rank {
			rank = candidate
			worst = finding.Severity
		}
	}
	return worst
}
