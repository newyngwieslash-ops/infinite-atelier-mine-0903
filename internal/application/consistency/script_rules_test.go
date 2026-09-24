package consistency

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// script_rules_test.go covers the SCRIPT ruleset — the mechanical half of AGENT_CONTRACTS 11.1.
//
// # What each test is for
//
// The ruleset has three ways to be wrong that matter, and each is a test here:
//
//  1. A rule that fires on a document that is FINE. This is the failure that breaks a product: a
//     supervisor told to fix something that is not wrong. Each rule's negative case is asserted.
//  2. A rule that does not fire on a document that IS wrong. Each rule's positive case is asserted.
//  3. A rule that reports the wrong CATEGORY. The category is what makes FR-110's classification
//     real rather than prose, so each finding's category is asserted, and a separate test asserts
//     every shipped rule has one.
//
// The reader is a double, because the rules' subject is what they DO with the answers — the reads
// themselves are the adapter's, and their evidence is the AC-E2E-002 walk over a real database.

// scriptRulesDouble answers the three reads from stated values.
type scriptRulesDouble struct {
	versionSeconds int
	scenesSeconds  int
	durationErr    error
	locked         []LockedLine
	lockedErr      error
	removed        []RemovedEvent
	removedErr     error
}

func (d *scriptRulesDouble) DurationOf(context.Context, string) (int, int, error) {
	return d.versionSeconds, d.scenesSeconds, d.durationErr
}

func (d *scriptRulesDouble) LockedLines(context.Context, string) ([]LockedLine, error) {
	return d.locked, d.lockedErr
}

func (d *scriptRulesDouble) RemovedEventsWithScenes(context.Context, string) ([]RemovedEvent, error) {
	return d.removed, d.removedErr
}

// scriptRulesSourceDouble hands back one reader for every version, which is what a test wants: the
// version is the caller's, and the ruleset's job is what it does with the answer.
type scriptRulesSourceDouble struct{ reader ScriptRulesReader }

func (s scriptRulesSourceDouble) ScriptRulesReaderFor(context.Context, string) ScriptRulesReader {
	return s.reader
}

func rulesetWith(reader ScriptRulesReader) *ScriptRuleset {
	return NewScriptRuleset(scriptRulesSourceDouble{reader: reader})
}

func findingsWithRule(findings []consistency.Finding, rule string) []consistency.Finding {
	matched := make([]consistency.Finding, 0, len(findings))
	for _, finding := range findings {
		if finding.Rule == rule {
			matched = append(matched, finding)
		}
	}
	return matched
}

// TestTheScriptDurationRuleFiresOutsideTheTolerance is the temporal rule's two directions.
func TestTheScriptDurationRuleFiresOutsideTheTolerance(t *testing.T) {
	cases := []struct {
		name                     string
		versionSeconds, scenes   int
		wantFinding              bool
		why                      string
	}{
		{"an exact match is silent", 300, 300, false, "nothing is wrong"},
		{"a match within the tolerance is silent", 320, 300, false,
			"scene estimates are rounded, so an exact match is not a property the data has"},
		{"a match inside the tolerance on the low side is silent", 280, 300, false,
			"the tolerance is symmetric: being under is the same kind of rounding"},
		{"a match just outside the tolerance fires", 331, 300, true, "31 seconds past the tolerance"},
		{"a large gap fires", 600, 300, true, "half the episode"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruleset := rulesetWith(&scriptRulesDouble{
				versionSeconds: testCase.versionSeconds, scenesSeconds: testCase.scenes,
			})
			findings, err := ruleset.Check(context.Background(), "script-v1")
			if err != nil {
				t.Fatal(err)
			}
			got := findingsWithRule(findings, ruleDurationScript)
			if testCase.wantFinding && len(got) == 0 {
				t.Fatalf("no finding for %d against %d (%s)", testCase.versionSeconds, testCase.scenes, testCase.why)
			}
			if !testCase.wantFinding && len(got) != 0 {
				t.Fatalf("a finding fired for %d against %d (%s): %+v", testCase.versionSeconds, testCase.scenes, testCase.why, got[0])
			}
			if testCase.wantFinding {
				if got[0].Category != consistency.CategoryTemporal {
					t.Fatalf("the duration finding is category %q, want temporal", got[0].Category)
				}
				if got[0].EntityType != "script_version" || got[0].EntityID != "script-v1" {
					t.Fatalf("the finding does not name the version it is about: %+v", got[0])
				}
				// The difference is in the message, because a reader deciding what to fix needs the
				// number rather than the fact.
				if !strings.Contains(got[0].Problem, "difference") {
					t.Fatalf("the problem does not state the difference: %q", got[0].Problem)
				}
			}
		})
	}
}

// TestTheScriptDurationRuleStaysSilentWithoutScenes covers the case the rule would otherwise fire on
// constantly: a version created before its scenes exist has a zero sum, and comparing against it
// would flag every draft.
func TestTheScriptDurationRuleStaysSilentWithoutScenes(t *testing.T) {
	ruleset := rulesetWith(&scriptRulesDouble{versionSeconds: 300, scenesSeconds: 0})
	findings, err := ruleset.Check(context.Background(), "script-v1")
	if err != nil {
		t.Fatal(err)
	}
	if got := findingsWithRule(findings, ruleDurationScript); len(got) != 0 {
		t.Fatalf("a version with no scenes was flagged: %+v", got[0])
	}
}

// TestTheLockedLineRuleReportsEachPin covers the fidelity rule: one finding per pinned line, each
// naming the LINE so FR-110's "jump to the entity" lands where the pin is.
func TestTheLockedLineRuleReportsEachPin(t *testing.T) {
	ruleset := rulesetWith(&scriptRulesDouble{locked: []LockedLine{
		{ID: "line-1", SceneID: "scene-1", Ordinal: 1},
		{ID: "line-2", SceneID: "scene-1", Ordinal: 2},
	}})
	findings, err := ruleset.Check(context.Background(), "script-v1")
	if err != nil {
		t.Fatal(err)
	}
	got := findingsWithRule(findings, ruleLockedChanged)
	if len(got) != 2 {
		t.Fatalf("%d findings for two pinned lines", len(got))
	}
	for _, finding := range got {
		if finding.EntityType != "dialogue_line" {
			t.Fatalf("the finding names %q rather than the line", finding.EntityType)
		}
		if finding.Category != consistency.CategoryFidelity {
			t.Fatalf("a pin is category %q, want fidelity", finding.Category)
		}
		// A pin is a CONSTRAINT rather than a defect, so it is minor and nothing is auto-fixable.
		if finding.Severity != workflow.SeverityMinor || finding.AutoFixable {
			t.Fatalf("a pin was reported as a defect to fix: %+v", finding)
		}
	}
	// And a version with no pins reports nothing: the rule is about pins, not about lines.
	clean := rulesetWith(&scriptRulesDouble{})
	findings, err = clean.Check(context.Background(), "script-v1")
	if err != nil {
		t.Fatal(err)
	}
	if got := findingsWithRule(findings, ruleLockedChanged); len(got) != 0 {
		t.Fatalf("an unpinned version was flagged: %+v", got[0])
	}
}

// TestTheRemovedEventRuleIsMajorAndCitesBothRecords covers the fidelity rule that is the real
// contradiction: the strategy removed an event and a scene still dramatizes it.
func TestTheRemovedEventRuleIsMajorAndCitesBothRecords(t *testing.T) {
	ruleset := rulesetWith(&scriptRulesDouble{removed: []RemovedEvent{
		{EventID: "event-7", EventName: "铜牌出水", SceneID: "scene-3"},
	}})
	findings, err := ruleset.Check(context.Background(), "script-v1")
	if err != nil {
		t.Fatal(err)
	}
	got := findingsWithRule(findings, ruleRemovedEventPresent)
	if len(got) != 1 {
		t.Fatalf("%d findings for one contradiction", len(got))
	}
	finding := got[0]
	if finding.Severity != workflow.SeverityMajor {
		t.Fatalf("the contradiction is %q, want major: the two records disagree about what the episode IS", finding.Severity)
	}
	if finding.Category != consistency.CategoryFidelity {
		t.Fatalf("the contradiction is category %q, want fidelity", finding.Category)
	}
	// Both records are cited, because the reader has to open BOTH to decide which is right.
	if len(finding.Evidence) != 2 {
		t.Fatalf("the finding cites %d references, want the event and the scene", len(finding.Evidence))
	}
	// The scene's own name is the message's subject, so a reader sees WHICH scene to open.
	if finding.EntityType != "scene" || finding.EntityID != "scene-3" {
		t.Fatalf("the finding does not name the scene: %+v", finding)
	}
	// And an empty version reports nothing.
	clean := rulesetWith(&scriptRulesDouble{})
	findings, err = clean.Check(context.Background(), "script-v1")
	if err != nil {
		t.Fatal(err)
	}
	if got := findingsWithRule(findings, ruleRemovedEventPresent); len(got) != 0 {
		t.Fatalf("a version with no removed events was flagged: %+v", got[0])
	}
}

// TestAReadFailureDoesNotFailTheCheck is the ruleset's failure policy, and it is the same one the
// storyboard ruleset records: a rule whose read failed reports NOTHING rather than an error, because
// failing the check would turn a storage fault into a failed review — and the supervisor's own half
// still runs.
func TestAReadFailureDoesNotFailTheCheck(t *testing.T) {
	ruleset := rulesetWith(&scriptRulesDouble{
		durationErr: errors.New("the store is gone"),
		lockedErr:   errors.New("the store is gone"),
		removedErr:  errors.New("the store is gone"),
	})
	findings, err := ruleset.Check(context.Background(), "script-v1")
	if err != nil {
		t.Fatalf("a read failure failed the check: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("a read failure produced findings: %+v", findings)
	}
}

// TestAnUnavailableRulesetReportsNothing covers the optional-port rule: a build without a script
// repository runs, and its script stages are supervised by the model alone.
func TestAnUnavailableRulesetReportsNothing(t *testing.T) {
	var empty *ScriptRuleset
	if findings, err := empty.Check(context.Background(), "script-v1"); err != nil || findings != nil {
		t.Fatalf("an unavailable ruleset answered %v, %v", findings, err)
	}
	if NewScriptRuleset(nil).Available() {
		t.Fatal("a ruleset with no source reports itself available")
	}
	// And an empty version id is no subject rather than an error.
	ruleset := rulesetWith(&scriptRulesDouble{versionSeconds: 600, scenesSeconds: 1})
	if findings, err := ruleset.Check(context.Background(), "   "); err != nil || findings != nil {
		t.Fatalf("a check with no version answered %v, %v", findings, err)
	}
}

// TestEveryShippedRuleHasACategory is the third case, and it is the one that keeps FR-110's
// classification from decaying: a rule with no entry falls through to the TECHNICAL default, which
// would file a costume problem as a technical one — wrong rather than merely unstated.
func TestEveryShippedRuleHasACategory(t *testing.T) {
	// The storyboard ruleset's rules are named through the DOMAIN package, where their constants
	// live; this file's own three are unqualified because they are defined here.
	shipped := []string{
		consistency.RuleCostumeContinuity, consistency.RulePropContinuity, consistency.RuleLocationContinuity,
		consistency.RuleShotCoverage, consistency.RuleDuration, consistency.RuleAssetApproved,
		// The script ruleset's three, named here so a rule added to that file without a category
		// fails this test rather than silently reporting as technical.
		ruleDurationScript, ruleLockedChanged, ruleRemovedEventPresent,
	}
	for _, rule := range shipped {
		category := consistency.CategoryOf(rule)
		if !consistency.IsValidCategory(category) {
			t.Fatalf("the rule %q maps to the unknown category %q", rule, category)
		}
		// The default is TECHNICAL, so a rule that lands there without an entry is caught by
		// checking the map itself rather than the function's answer.
		if !consistency.RuleHasStatedCategory(rule) {
			t.Fatalf("the rule %q has no stated category, so it would report as technical", rule)
		}
	}
	// And an unknown rule is TECHNICAL rather than an invalid value: the default is an answer.
	if consistency.CategoryOf("SOME_RULE_THIS_BUILD_DOES_NOT_SHIP") != consistency.CategoryTechnical {
		t.Fatal("an unknown rule did not fall back to technical")
	}
}
