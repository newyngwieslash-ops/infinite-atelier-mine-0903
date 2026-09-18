package versioning

import "testing"

// TestStatusVocabularyPinsDocumentedValues is the T1 pin: exactly the eight
// statuses of DOMAIN_MODEL section 2.5 are accepted and nothing else. A status
// that stops being recognised would make every versioned table unreadable.
func TestStatusVocabularyPinsDocumentedValues(t *testing.T) {
	documented := []string{
		"draft", "candidate", "under_review", "approved",
		"rejected", "superseded", "deprecated", "stale",
	}
	if len(Statuses) != len(documented) {
		t.Fatalf("the status set has %d entries, the specification lists %d", len(Statuses), len(documented))
	}
	for index, want := range documented {
		if got := string(Statuses[index]); got != want {
			t.Fatalf("status %d is %q, want %q", index, got, want)
		}
		if !IsValidStatus(Status(want)) {
			t.Fatalf("documented status %q is not recognised", want)
		}
	}
	for _, rejected := range []string{"", "review", "pending", "Approved", "APPROVED", "passed", "completed", "obsolete"} {
		if IsValidStatus(Status(rejected)) {
			t.Fatalf("undocumented status %q is accepted", rejected)
		}
	}
}

// TestContentFreezesOnApprovalAndSupersession covers section 2.5's
// "版本内容批准后不可原地编辑".
func TestContentFreezesOnApprovalAndSupersession(t *testing.T) {
	for _, status := range []Status{StatusApproved, StatusSuperseded} {
		if !IsContentFrozen(status) {
			t.Fatalf("%s content must be frozen", status)
		}
	}
	for _, status := range []Status{StatusDraft, StatusCandidate, StatusUnderReview, StatusRejected, StatusDeprecated, StatusStale} {
		if IsContentFrozen(status) {
			t.Fatalf("%s content must stay editable, so a draft can be worked on", status)
		}
	}
}

// TestCanApproveRejectsFormerAndCurrentApprovals covers the two states that
// cannot be approved and the one that can.
func TestCanApproveRejectsFormerAndCurrentApprovals(t *testing.T) {
	cases := []struct {
		from Status
		ok   bool
		why  string
	}{
		{StatusDraft, true, "a draft that passed review is approved"},
		{StatusCandidate, true, "a candidate can be chosen"},
		{StatusUnderReview, true, "a reviewed version is approved"},
		{StatusRejected, true, "a rejected version can be revised and re-approved"},
		{StatusDeprecated, true, "a deprecated version can be brought back"},
		{StatusApproved, false, "an approved version is not approved twice"},
		{StatusSuperseded, false, "a superseded version is history"},
		{StatusStale, false, "section 15.2 requires a re-review before re-approval"},
	}
	for _, testCase := range cases {
		err := CanApprove(testCase.from, StatusApproved)
		if testCase.ok && err != nil {
			t.Fatalf("approving from %s must succeed (%s): %v", testCase.from, testCase.why, err)
		}
		if !testCase.ok && err == nil {
			t.Fatalf("approving from %s must fail (%s)", testCase.from, testCase.why)
		}
	}
	if err := CanApprove(StatusDraft, StatusRejected); err == nil {
		t.Fatal("CanApprove must refuse a transition that is not an approval")
	}
	if err := CanApprove(Status("nonsense"), StatusApproved); err == nil {
		t.Fatal("an unrecognised current status must be refused")
	}
}

// TestCanTransitionTable pins the state machine so an accidental widening or
// narrowing fails here rather than in production.
func TestCanTransitionTable(t *testing.T) {
	cases := []struct {
		from, to Status
		ok       bool
	}{
		{StatusDraft, StatusCandidate, true},
		{StatusDraft, StatusUnderReview, true},
		{StatusDraft, StatusApproved, false}, // approval goes through review
		{StatusCandidate, StatusUnderReview, true},
		{StatusCandidate, StatusApproved, false},
		{StatusUnderReview, StatusApproved, true},
		{StatusUnderReview, StatusRejected, true},
		{StatusApproved, StatusSuperseded, true},
		{StatusApproved, StatusStale, true}, // an upstream change staleness-marks it
		{StatusApproved, StatusDeprecated, false},
		{StatusRejected, StatusDraft, true},
		{StatusStale, StatusDraft, true},
		{StatusStale, StatusUnderReview, true},
		{StatusStale, StatusApproved, false}, // re-approval goes through re-review
		{StatusSuperseded, StatusApproved, false},
		{StatusSuperseded, StatusStale, false},
		{StatusDeprecated, StatusApproved, false},
		{StatusDeprecated, StatusDraft, false},
		{StatusDraft, StatusDraft, false}, // a no-op is not a transition
		{Status("nonsense"), StatusDraft, false},
		{StatusDraft, Status("nonsense"), false},
	}
	for _, testCase := range cases {
		if got := CanTransition(testCase.from, testCase.to); got != testCase.ok {
			t.Fatalf("CanTransition(%s, %s) = %v, want %v", testCase.from, testCase.to, got, testCase.ok)
		}
	}
}

// TestStalenessResponseIsRegenerateOrReReview proves the branch that matters to
// section 15: a stale artifact can be regenerated (draft) or sent back for the
// re-review the review_required classification demands, and cannot become
// approved again in one step or silently turn superseded.
func TestStalenessResponseIsRegenerateOrReReview(t *testing.T) {
	for _, target := range []Status{StatusDraft, StatusUnderReview} {
		if !CanTransition(StatusStale, target) {
			t.Fatalf("a stale version must be able to move to %s to be regenerated or re-reviewed", target)
		}
	}
	if CanTransition(StatusStale, StatusApproved) {
		t.Fatal("stale must not reach approved directly, which would skip the required re-review")
	}
	if CanTransition(StatusStale, StatusSuperseded) {
		t.Fatal("stale must not become superseded, which would erase the staleness reason")
	}
}

// TestSupersedePreviousOnlyFiresForAnActualApproval covers the rule that makes
// the schema's partial unique index satisfiable.
func TestSupersedePreviousOnlyFiresForAnActualApproval(t *testing.T) {
	if !SupersedePrevious(StatusApproved) {
		t.Fatal("approving over an existing approval must supersede it")
	}
	for _, status := range []Status{StatusDraft, StatusCandidate, StatusUnderReview, StatusRejected, StatusStale, StatusSuperseded, StatusDeprecated} {
		if SupersedePrevious(status) {
			t.Fatalf("%s is not an approval, so nothing is superseded", status)
		}
	}
}

// TestCreatedByVocabulary pins section 2.5's producer list.
func TestCreatedByVocabulary(t *testing.T) {
	for _, value := range []CreatedByType{CreatedByUser, CreatedByAgent, CreatedByMigration, CreatedBySystem} {
		if !IsValidCreatedByType(value) {
			t.Fatalf("documented producer %q is not recognised", value)
		}
	}
	for _, value := range []CreatedByType{"", "User", "service", "import"} {
		if IsValidCreatedByType(value) {
			t.Fatalf("undocumented producer %q is accepted", value)
		}
	}
}
