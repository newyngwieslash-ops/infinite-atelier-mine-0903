package consistency

import (
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
)

// budget_test.go covers FR-110's COST rule, which is a pure function over two numbers.
//
// # Why this rule gets a unit test and the others do not
//
// Every other rule in this package is a join, and this repository's convention is that a join's value
// is what it reads from a database — so those rules are tested through real rows. `RevisionBudgetFinding`
// reads nothing: the stage pipeline hands it the count and the budget the ENGINE already holds, and
// its whole behaviour is one comparison. That makes it a function whose boundaries are the whole
// specification, and a mutation review agreed: replacing the comparison with `budget <= 0` alone left
// every integration test green, because no fixture ever spent a budget.
//
// # The four cases, and what each one is for
//
//   - Below the budget: silent. This is the ordinary state and the one that must never fire, or every
//     review of a stage that has been revised once would carry a cost finding.
//   - AT the budget: fires. The boundary is the point — the engine refuses `StartRevision` when
//     `revisions > MaxAutoFix`, so a stage at exactly the budget is one whose next automatic revision
//     is refused, which is precisely the fact the finding reports.
//   - Zero budget: silent. A stage configured with no automatic revisions spends its budget
//     immediately, and reporting that would put a cost finding on every review of it.
//   - Nothing counted: the caller cannot express this, which is why the function takes plain ints and
//     the PIPELINE is what skips the call when the engine cannot count. Stated here so a reader does
//     not look for a "cannot count" branch in the function.
func TestRevisionBudgetFindingFiresOnlyWhenTheBudgetIsSpent(t *testing.T) {
	cases := []struct {
		name      string
		revisions int
		budget    int
		fires     bool
	}{
		{"no revisions yet", 0, 2, false},
		{"one of two revisions used", 1, 2, false},
		{"both revisions used", 2, 2, true},
		{"more than the budget, which a lowered policy can produce", 3, 2, true},
		{"a stage with no automatic revisions", 0, 0, false},
		{"a negative budget, which no policy produces but a zero value could", 1, -1, false},
		{"the ceiling the engine enforces", 2, 2, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			finding, fired := RevisionBudgetFinding("stage-run-1", testCase.revisions, testCase.budget)
			if fired != testCase.fires {
				t.Fatalf("revisions=%d budget=%d fired=%v, want %v",
					testCase.revisions, testCase.budget, fired, testCase.fires)
			}
			if !fired {
				// A non-firing call returns the zero finding, so a caller that ignored the boolean
				// would store an empty finding rather than nothing. That is the shape this assertion
				// pins.
				if finding.Rule != "" || finding.EntityID != "" {
					t.Fatalf("a non-firing call returned %+v rather than the zero finding", finding)
				}
				return
			}
			if finding.Rule != consistency.RuleRevisionBudgetSpent {
				t.Fatalf("the finding's rule is %q", finding.Rule)
			}
			if finding.Category != consistency.CategoryCost {
				t.Fatalf("the finding is classified %q, want FR-110's cost category", finding.Category)
			}
			// It names the ATTEMPT rather than a row, because the fact is about the review rather
			// than about any one artifact of it.
			if finding.EntityType != "stage_run" || finding.EntityID != "stage-run-1" {
				t.Fatalf("the finding addresses %s/%s", finding.EntityType, finding.EntityID)
			}
			// And it is NOT a blocker: a spent budget is a fact a person needs, not a defect in the
			// artifact, so it must not be able to fail a review on its own.
			if finding.Blocker() {
				t.Fatal("a spent revision budget blocks a review, and it is a remark rather than a fault")
			}
		})
	}
}
