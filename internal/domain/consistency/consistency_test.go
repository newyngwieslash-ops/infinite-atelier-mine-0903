// The test is in the SAME package, so the rules are exercised as they are used. An external test
// package would be the more usual choice for a public API, and here it would add an import of the
// package under test to every assertion without buying anything: these tests are about the
// functions' behaviour, not about the surface they present.
package consistency

import (
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// These tests cover the merge and classification rules, which are pure functions. The RULES
// themselves — the six checks over a board — are covered end to end in the infrastructure package's
// memory_wp10 and consistency tests, because a rule's whole value is what it reads from the database
// and a unit test with a hand-built struct would prove only that the comparison works.

func finding(rule, entityID, field string, severity workflow.Severity) Finding {
	return Finding{
		Rule: rule, Severity: severity, EntityType: "storyboard_item",
		EntityID: entityID, Field: field, Problem: "p", Suggestion: "s",
	}
}

// TestBlockersAreCriticalAndMajor pins the classification the merge relies on.
//
// The rule decides whether a supervisor's happy verdict can be overruled, so which severities block
// is a behavioural choice rather than a label. A minor finding is a remark — a duration that drifted
// a little — and treating it as a blocker would make every board fail on arithmetic.
func TestBlockersAreCriticalAndMajor(t *testing.T) {
	if !finding("R", "e", "f", SeverityCritical).Blocker() {
		t.Fatal("a critical finding is not a blocker")
	}
	if !finding("R", "e", "f", SeverityMajor).Blocker() {
		t.Fatal("a major finding is not a blocker")
	}
	if finding("R", "e", "f", SeverityMinor).Blocker() {
		t.Fatal("a minor finding is a blocker")
	}
	// An empty severity is not a blocker either: a finding whose severity nobody stated must not be
	// able to fail a stage.
	if finding("R", "e", "f", "").Blocker() {
		t.Fatal("a finding with no severity is a blocker")
	}
	blockers := Blockers([]Finding{
		finding("A", "e", "f", SeverityMinor),
		finding("B", "e", "f", SeverityMajor),
		finding("C", "e", "f", SeverityCritical),
	})
	if len(blockers) != 2 || blockers[0].Rule != "B" || blockers[1].Rule != "C" {
		t.Fatalf("Blockers returned %+v", blockers)
	}
}

// TestDedupeKeepsTheMoreSevere is the property that makes a merge safe in both directions.
//
// It is used twice: once inside the rules, where a rule can reach the same row from two paths, and
// once across the two producers, where a supervisor that echoed a deterministic finding produces a
// duplicate. In both cases the more severe statement must survive — a duplicate cannot make a problem
// less serious.
func TestDedupeKeepsTheMoreSevere(t *testing.T) {
	deduped := Dedupe([]Finding{
		finding("CHARACTER_CONTINUITY", "item-1", "costumeVersionId", SeverityMinor),
		finding("CHARACTER_CONTINUITY", "item-1", "costumeVersionId", SeverityMajor),
		// A different field of the same row is a different finding.
		finding("CHARACTER_CONTINUITY", "item-1", "visualDescription", SeverityMajor),
		// A different row is a different finding.
		finding("CHARACTER_CONTINUITY", "item-2", "costumeVersionId", SeverityMajor),
	})
	if len(deduped) != 3 {
		t.Fatalf("Dedupe returned %d findings: %+v", len(deduped), deduped)
	}
	for _, kept := range deduped {
		if kept.EntityID == "item-1" && kept.Field == "costumeVersionId" {
			if kept.Severity != SeverityMajor {
				t.Fatalf("the duplicate kept %q, want the more severe statement", kept.Severity)
			}
		}
	}
	// The worse one arriving SECOND must still win, which is the case a "first wins" implementation
	// would get wrong.
	deduped = Dedupe([]Finding{
		finding("R", "e", "f", SeverityMajor),
		finding("R", "e", "f", SeverityCritical),
	})
	if len(deduped) != 1 || deduped[0].Severity != SeverityCritical {
		t.Fatalf("a later, more severe duplicate did not win: %+v", deduped)
	}
}

// TestSortIsTotal pins the ordering a report depends on.
//
// Two runs over the same rows must produce the same list in the same order: a review report is
// compared across versions, and a list that reordered itself would look like a changed set of
// problems where only the iteration order moved.
func TestSortIsTotal(t *testing.T) {
	first := Sort([]Finding{
		finding("B", "e2", "f", SeverityMinor),
		finding("A", "e2", "f", SeverityMinor),
		finding("A", "e1", "z", SeverityMinor),
		finding("A", "e1", "a", SeverityMinor),
	})
	want := []struct{ rule, entity, field string }{
		{"A", "e1", "a"}, {"A", "e1", "z"}, {"A", "e2", "f"}, {"B", "e2", "f"},
	}
	for index, expected := range want {
		got := first[index]
		if got.Rule != expected.rule || got.EntityID != expected.entity || got.Field != expected.field {
			t.Fatalf("position %d is %s/%s/%s, want %s/%s/%s", index,
				got.Rule, got.EntityID, got.Field, expected.rule, expected.entity, expected.field)
		}
	}
	// The same input in a different order sorts to the same output, which is what makes the order a
	// property of the findings rather than of how they arrived.
	shuffled := Sort([]Finding{
		finding("A", "e1", "a", SeverityMinor),
		finding("B", "e2", "f", SeverityMinor),
		finding("A", "e1", "z", SeverityMinor),
		finding("A", "e2", "f", SeverityMinor),
	})
	for index := range first {
		if first[index].Rule != shuffled[index].Rule || first[index].EntityID != shuffled[index].EntityID {
			t.Fatalf("two orderings of the same findings differ at %d", index)
		}
	}
}

// TestWorstSeverityNamesTheMostSerious covers the report's own severity field.
func TestWorstSeverityNamesTheMostSerious(t *testing.T) {
	if got := WorstSeverity(nil); got != workflow.SeverityNone {
		t.Fatalf("an empty list reports %q, want none", got)
	}
	if got := WorstSeverity([]Finding{
		finding("A", "e", "f", SeverityMinor),
		finding("B", "e", "f", SeverityCritical),
		finding("C", "e", "f", SeverityMajor),
	}); got != workflow.SeverityCritical {
		t.Fatalf("the worst severity is %q", got)
	}
}
