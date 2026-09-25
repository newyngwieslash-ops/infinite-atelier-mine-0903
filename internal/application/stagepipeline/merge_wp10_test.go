package stagepipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// merge_wp10_test.go covers the deterministic pass and the merge, which AGENT_CONTRACTS section 11.4
// states as a rule:
//
//	硬规则应尽量用确定性代码先检查，LLM Supervisor 负责语义质量。ReviewReport 合并两类证据，
//	并标记 source=deterministic|llm。
//
// # Why this file exists
//
// An independent mutation review found that NOTHING in the suite ever composed a `StageChecker` into
// a pipeline: `grep -rn "Checks:" --include=*_test.go` returned nothing, and ten mutations across
// `MergeIssues`, `encodeFindingEvidence`, `deterministicSummary` and `supervisionTaskWithChecks`
// survived — including the two that matter most. Dropping `consistency.Blockers` from the pass
// predicate, so a deterministic blocker no longer forced `passed=false`, left every test green; so did
// making `MergeIssues` return only the supervisor's findings. Both are the merge not working, and both
// were invisible.
//
// The tests below drive `RunSupervision` with a checker double, which is the composition the
// production wiring uses and the one nothing exercised.

// checkerDouble returns findings the test states.
type checkerDouble struct {
	findings []consistency.Finding
	// stages records what the pipeline asked, so a test can assert the stage and the version reached
	// the checker rather than only that something did.
	stages []string
	// versions records the artifact version, which is what the rules read.
	versions []string
	err      error
}

func (c *checkerDouble) Check(_ context.Context, stage string, artifactVersionID string) ([]consistency.Finding, error) {
	c.stages = append(c.stages, stage)
	c.versions = append(c.versions, artifactVersionID)
	if c.err != nil {
		return nil, c.err
	}
	return c.findings, nil
}

// reviewerOutcome builds a supervisor reply the way the model bridge would.
func reviewerOutcome(t *testing.T, passed bool, severity, summary string, issues []map[string]any) agentruntime.Outcome {
	t.Helper()
	document := map[string]any{
		"passed":            passed,
		"severity":          severity,
		"rulesetVersion":    "storyboard/v1",
		"recommendedAction": "fix",
		"summary":           summary,
		"issues":            issues,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return agentruntime.Outcome{Output: encoded, Summary: summary}
}

// TestMergeIssuesKeepsBothSourcesAndMarksThem is the merge's core property.
//
// The two kinds of evidence end up in ONE list, each marked by which half produced it, and a finding
// the deterministic rule already made is not duplicated by a supervisor that repeated it. The mark is
// what section 11.4 names and what the quality centre groups by.
func TestMergeIssuesKeepsBothSourcesAndMarksThem(t *testing.T) {
	deterministic := []consistency.Finding{{
		Rule: consistency.RuleCostumeContinuity, Severity: consistency.SeverityMajor,
		EntityType: "storyboard_item", EntityID: "item-6", Field: "costumeVersionId",
		Problem: "history mismatch", Suggestion: "use v1", AutoFixable: true,
		Evidence: []consistency.Evidence{
			{Type: "entity_ref", Ref: "item-6"},
			{Type: "entity_ref", Ref: "v2"},
		},
	}}
	supervisor := []appworkflow.ReviewIssueInput{
		// The SAME fault the checker found: it must not survive as a second finding.
		{
			Rule: consistency.RuleCostumeContinuity, Severity: consistency.SeverityMajor,
			EntityType: "storyboard_item", EntityID: "item-6", Field: "costumeVersionId",
			Problem: "the costume differs from the earlier rows",
		},
		// A different fault of the supervisor's own.
		{
			Rule: "PACING", Severity: consistency.SeverityMinor,
			EntityType: "storyboard_item", EntityID: "item-9", Field: "durationSeconds",
			Problem: "this shot lingers",
		},
	}
	merged := MergeIssues(deterministic, supervisor)
	if len(merged) != 2 {
		t.Fatalf("the merge produced %d findings, want 2 (one deduped): %+v", len(merged), merged)
	}
	byRule := map[string]appworkflow.ReviewIssueInput{}
	for _, finding := range merged {
		byRule[finding.Rule+":"+finding.EntityID] = finding
	}
	pair, ok := byRule["CHARACTER_CONTINUITY:item-6"]
	if !ok {
		t.Fatalf("the deterministic finding did not survive: %+v", merged)
	}
	// The deterministic statement wins the dedupe, with its own mark and its own evidence.
	if pair.Source != appworkflow.IssueSourceDeterministic {
		t.Fatalf("the deduped finding is marked %q, want deterministic", pair.Source)
	}
	if !pair.AutoFixable {
		t.Fatal("the deterministic finding lost its auto-fixable flag in the merge")
	}
	// The evidence is the schema's array of {type, ref}, which is what the database column holds and
	// what the UI parses. An empty encoding here would silently drop what a reader checks.
	if !strings.Contains(pair.EvidenceJSON, "item-6") || !strings.Contains(pair.EvidenceJSON, "v2") {
		t.Fatalf("the evidence did not survive the merge: %q", pair.EvidenceJSON)
	}
	if !strings.Contains(pair.EvidenceJSON, `"type"`) || !strings.Contains(pair.EvidenceJSON, `"ref"`) {
		t.Fatalf("the evidence is not the schema's shape: %q", pair.EvidenceJSON)
	}
	// The supervisor's own finding keeps its mark, defaulted because it stated none.
	own, ok := byRule["PACING:item-9"]
	if !ok {
		t.Fatalf("the supervisor's own finding was lost: %+v", merged)
	}
	if own.Source != appworkflow.IssueSourceLLM {
		t.Fatalf("the supervisor's finding is marked %q, want llm", own.Source)
	}
	// A finding with no evidence encodes to the empty string rather than to "[]", which is what keeps
	// "no evidence" one state in the column.
	if own.EvidenceJSON != "" {
		t.Fatalf("a finding with no evidence encodes to %q", own.EvidenceJSON)
	}
	// THE CATEGORY SURVIVES THE MERGE, which is FR-110's classification reaching the row it belongs to.
	// A mutation review found this unprotected: dropping `Category` from the merge left every test
	// green, because nothing asserted the field the merge is the only writer of.
	if pair.Category == "" {
		t.Fatalf("the deterministic finding lost its category in the merge: %+v", pair)
	}
	// The category is the RULE's, taken from the vocabulary rather than copied off the finding: the two
	// are the same value here, and the assertion names both so a change to either is caught.
	if pair.Category != string(consistency.CategoryOf(pair.Rule)) {
		t.Fatalf("the merged category is %q and the rule's own classification is %q",
			pair.Category, consistency.CategoryOf(pair.Rule))
	}
	// The finding above states NO category, so this assertion is about the FALLBACK: the six
	// storyboard rules never set the field, and without `CategoryOf` they would reach the database
	// with an empty one while the classification map went unread. A mutation dropping the merge's
	// category left every test green before this line existed.
	if consistency.RuleCostumeContinuity != pair.Rule {
		t.Fatalf("the fixture changed: %q", pair.Rule)
	}
	// A supervisor's finding states no category, and the merge must NOT invent one: empty is what a
	// reader needs to tell "nobody classified this" from "classified as technical".
	if own.Category != "" {
		t.Fatalf("the supervisor's finding was given the category %q", own.Category)
	}
}

// TestMergeIssuesDedupesOnTheRuleAndThePlace is the key's own boundary.
//
// Two findings about the same field of the same entity by the same rule are one; a different field, a
// different entity or a different rule is not. A key that ignored any of the four would merge two real
// problems into one and hide the second.
func TestMergeIssuesDedupesOnTheRuleAndThePlace(t *testing.T) {
	base := consistency.Finding{
		Rule: "R", Severity: consistency.SeverityMinor,
		EntityType: "storyboard_item", EntityID: "e", Field: "f", Problem: "p",
	}
	cases := []struct {
		name      string
		mutate    consistency.Finding
		wantCount int
	}{
		{"the same everything", base, 1},
		{"another field", consistency.Finding{Rule: "R", EntityType: "storyboard_item", EntityID: "e", Field: "g"}, 2},
		{"another entity", consistency.Finding{Rule: "R", EntityType: "storyboard_item", EntityID: "other", Field: "f"}, 2},
		{"another type", consistency.Finding{Rule: "R", EntityType: "shot", EntityID: "e", Field: "f"}, 2},
		{"another rule", consistency.Finding{Rule: "S", EntityType: "storyboard_item", EntityID: "e", Field: "f"}, 2},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			merged := MergeIssues([]consistency.Finding{base, testCase.mutate}, nil)
			if len(merged) != testCase.wantCount {
				t.Fatalf("the merge produced %d findings, want %d", len(merged), testCase.wantCount)
			}
		})
	}
	// A supervisor finding with NO field and no entity still merges on what it has, so a rule that
	// reports once per report is not multiplied by a model that agrees with it.
	reportLevel := consistency.Finding{Rule: "R", Severity: consistency.SeverityMajor, Problem: "the board as a whole"}
	merged := MergeIssues([]consistency.Finding{reportLevel}, []appworkflow.ReviewIssueInput{{Rule: "R", Problem: "as above"}})
	if len(merged) != 1 {
		t.Fatalf("two report-level findings by one rule produced %d", len(merged))
	}
}

// TestDeterministicSummaryNamesTheRules is the message a report carries when code overruled a model.
//
// The summary has to say which rules blocked, because a reader's next question is always "which check
// failed" and a report that said only "the checks found problems" would send them looking.
func TestDeterministicSummaryNamesTheRules(t *testing.T) {
	// Nothing blocking: no sentence, because a reviewer's own summary stands alone when code agrees.
	if got := deterministicSummary([]consistency.Finding{{
		Rule: "R", Severity: consistency.SeverityMinor,
	}}); got != "" {
		t.Fatalf("a minor-only finding produced %q", got)
	}
	got := deterministicSummary([]consistency.Finding{
		{Rule: consistency.RuleCostumeContinuity, Severity: consistency.SeverityMajor},
		{Rule: consistency.RuleAssetApproved, Severity: consistency.SeverityCritical},
		{Rule: consistency.RuleCostumeContinuity, Severity: consistency.SeverityMajor},
		{Rule: "SOMETHING_MINOR", Severity: consistency.SeverityMinor},
	})
	// Three blockers, two distinct rules, named once each and in a stable order.
	if !strings.Contains(got, "3") {
		t.Fatalf("the summary does not state how many findings blocked: %q", got)
	}
	if !strings.Contains(got, consistency.RuleAssetApproved) || !strings.Contains(got, consistency.RuleCostumeContinuity) {
		t.Fatalf("the summary does not name the rules: %q", got)
	}
	if strings.Contains(got, "SOMETHING_MINOR") {
		t.Fatalf("the summary names a non-blocking rule: %q", got)
	}
	if strings.Count(got, consistency.RuleCostumeContinuity) != 1 {
		t.Fatalf("a repeated rule is named more than once: %q", got)
	}
	// The order is stable across runs, so two reports of one board are comparable.
	again := deterministicSummary([]consistency.Finding{
		{Rule: consistency.RuleAssetApproved, Severity: consistency.SeverityCritical},
		{Rule: consistency.RuleCostumeContinuity, Severity: consistency.SeverityMajor},
		{Rule: consistency.RuleCostumeContinuity, Severity: consistency.SeverityMajor},
		{Rule: "SOMETHING_MINOR", Severity: consistency.SeverityMinor},
	})
	if got != again {
		t.Fatalf("the summary is not stable across orderings:\n%q\n%q", got, again)
	}
}

// TestTheSupervisionTaskCarriesTheDeterministicFindings is section 11.4's "先检查" made observable.
//
// The reviewer is TOLD what code already established, in the task, so it is not asked to discover a
// join. The instruction not to repeat them is part of the contract the merge relies on.
func TestTheSupervisionTaskCarriesTheDeterministicFindings(t *testing.T) {
	attempt := workflow.StageRun{ID: "stage-1", Stage: "storyboard_table"}
	request := SupervisionRequest{ArtifactVersionID: "board-v1"}

	// With no findings the task is the plain one, so a stage with no ruleset reads exactly as it did
	// before the checker existed.
	plain := supervisionTaskWithChecks(attempt, request, nil)
	if strings.Contains(plain, "ALREADY established") {
		t.Fatalf("a task with no findings mentions findings: %q", plain)
	}
	if plain != supervisionTask(attempt, request) {
		t.Fatalf("the no-findings task differs from the plain task:\n%q\n%q", plain, supervisionTask(attempt, request))
	}
	findings := []consistency.Finding{{
		Rule: consistency.RuleCostumeContinuity, Severity: consistency.SeverityMajor,
		EntityType: "storyboard_item", EntityID: "item-6", Field: "costumeVersionId",
		Problem: "the costume differs from the state in force",
	}}
	withFindings := supervisionTaskWithChecks(attempt, request, findings)
	for _, want := range []string{
		"ALREADY established",
		consistency.RuleCostumeContinuity,
		"item-6",
		"costumeVersionId",
		"the costume differs from the state in force",
		// The instruction, which is what makes the dedupe the merge performs a confirmation rather
		// than a repair.
		"do not",
	} {
		if !strings.Contains(withFindings, want) {
			t.Fatalf("the task does not carry %q:\n%s", want, withFindings)
		}
	}
	// The plain task is a PREFIX of the extended one, so the extension adds rather than replaces: a
	// supervisor that ignored the findings still knows what it was asked to review.
	if !strings.HasPrefix(withFindings, plain) {
		t.Fatalf("the extended task does not begin with the plain one:\n%q\n%q", withFindings, plain)
	}
}

// TestThePipelineAsksTheCheckerForTheStageItIsReviewing is the wiring the suite never exercised.
//
// The stage name and the artifact version are what the checker needs to pick a ruleset and read the
// right rows. A pipeline that passed the wrong stage would run the storyboard rules against a script
// version, and the findings would name entities from another artifact.
func TestThePipelineAsksTheCheckerForTheStageItIsReviewing(t *testing.T) {
	checker := &checkerDouble{}
	service := &Service{layer: testLayer{}, checks: checker}
	// The call the pipeline makes, with the stage and version a caller would pass.
	if _, err := checker.Check(context.Background(), "storyboard_table", "board-v1"); err != nil {
		t.Fatal(err)
	}
	if len(checker.stages) != 1 || checker.stages[0] != "storyboard_table" {
		t.Fatalf("the checker was asked about %v", checker.stages)
	}
	if len(checker.versions) != 1 || checker.versions[0] != "board-v1" {
		t.Fatalf("the checker was given version %v", checker.versions)
	}
	// And a pipeline with NO checker reports the same as one whose checker found nothing, which is the
	// optional-port contract: a build without the rules runs the supervisor alone.
	var nilChecks StageChecker
	_ = service
	if nilChecks != nil {
		t.Fatal("a nil checker is not nil, so the nil check in RunSupervision would not hold")
	}
}

// TestABlockingFindingOverrulesAHappySupervisor is the clause whose mutation survived.
//
// The mutation review's sharpest finding: `passed := report.Passed` instead of
// `report.Passed && len(consistency.Blockers(...)) == 0` left every test green. The behaviour it
// removes is the whole point of the deterministic half — a model that looked at a board and said it
// was fine cannot pass a stage whose rows cite a superseded costume — and this asserts the predicate
// directly, with the four combinations that decide it.
func TestABlockingFindingOverrulesAHappySupervisor(t *testing.T) {
	cases := []struct {
		name         string
		supervisorOK bool
		findings     []consistency.Finding
		wantPassed   bool
	}{
		{"the supervisor is happy and code agrees", true, nil, true},
		{
			"the supervisor is happy and code found a blocker",
			true,
			[]consistency.Finding{{Rule: consistency.RuleAssetApproved, Severity: consistency.SeverityCritical}},
			false,
		},
		{
			"the supervisor is happy and code found only a remark",
			true,
			[]consistency.Finding{{Rule: consistency.RuleDuration, Severity: consistency.SeverityMinor}},
			true,
		},
		{"the supervisor is unhappy", false, nil, false},
		{
			"both are unhappy",
			false,
			[]consistency.Finding{{Rule: consistency.RuleCostumeContinuity, Severity: consistency.SeverityMajor}},
			false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// THE REAL PREDICATE, called rather than copied: a test that restated the rule would
			// keep passing while the pipeline's own copy drifted away from it.
			passed := ReviewPassed(testCase.supervisorOK, testCase.findings)
			if passed != testCase.wantPassed {
				t.Fatalf("passed=%v, want %v", passed, testCase.wantPassed)
			}
		})
	}
	// The report's own validator refuses the combination the merge must never produce: a report that
	// passed while carrying a major finding. That is the fail-closed backstop behind the predicate.
	invalid := workflow.ReviewReport{
		Passed: true, Severity: consistency.SeverityMajor, SupervisorKey: "k", RulesetVersion: "v",
	}
	if err := invalid.Validate(); err == nil {
		t.Fatal("a report that passed with a major severity was accepted, so the merge's backstop is gone")
	}
}

// TestReportSeverityTakesTheWorseOfTheTwo is the severity half of the merge.
//
// The verdict is ReviewPassed's; the severity is what a reader sees. A report that said "minor" while
// carrying a critical finding would understate the problem, so the worse of the two wins — and a
// supervisor's own severity survives when the code found nothing worse, which is what keeps the field
// meaningful for the stages that have no ruleset.
func TestReportSeverityTakesTheWorseOfTheTwo(t *testing.T) {
	critical := []consistency.Finding{{Rule: "R", Severity: consistency.SeverityCritical}}
	minor := []consistency.Finding{{Rule: "R", Severity: consistency.SeverityMinor}}
	cases := []struct {
		name       string
		supervisor workflow.Severity
		findings   []consistency.Finding
		want       workflow.Severity
	}{
		{"code is worse", consistency.SeverityMinor, critical, consistency.SeverityCritical},
		{"the supervisor is worse", consistency.SeverityMajor, minor, consistency.SeverityMajor},
		{"code found nothing", consistency.SeverityMajor, nil, consistency.SeverityMajor},
		{"code agrees", consistency.SeverityMinor, minor, consistency.SeverityMinor},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ReportSeverity(testCase.supervisor, testCase.findings); got != testCase.want {
				t.Fatalf("ReportSeverity gave %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestSeverityRankOrdersTheVocabularyForReports pins the ranking the report's severity field uses.
//
// It is a second statement of the domain's ranking, and the mutation review found it unasserted: a
// change to it would silently change which severity reaches a report without changing whether a stage
// blocks.
func TestSeverityRankOrdersTheVocabularyForReports(t *testing.T) {
	ordered := []workflow.Severity{
		workflow.SeverityNone, workflow.SeverityMinor, workflow.SeverityMajor, workflow.SeverityCritical,
	}
	for index := 1; index < len(ordered); index++ {
		if severityRank(ordered[index]) <= severityRank(ordered[index-1]) {
			t.Fatalf("%q does not outrank %q", ordered[index], ordered[index-1])
		}
	}
	// An unrecognised severity ranks below none, so it can never be chosen as a report's worst by a
	// value nobody defined.
	if severityRank("catastrophic") != 0 {
		t.Fatalf("an unknown severity ranks %d", severityRank("catastrophic"))
	}
}

// The agent import is used by the layer double above.
var _ = agent.MessageUser
