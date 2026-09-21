package asset

import (
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// gap_test.go covers the two rules a report's readers depend on: a line that claims to
// be satisfied names what satisfies it, and a report cannot be approved while a
// required asset is still missing.

func gapItem(ordinal int, assetType Type, entityID string, required bool, status GapStatus, assetID string) GapItem {
	return GapItem{
		ReportID:      "report-1",
		Ordinal:       ordinal,
		AssetType:     assetType,
		StoryEntityID: entityID,
		AssetID:       assetID,
		Status:        status,
		Required:      required,
	}
}

// TestASatisfiedItemMustNameTheAssetThatSatisfiesIt covers the pairing a model would get
// wrong by filling in the status and leaving the reference blank.
//
// The check exists because the alternative — accepting `satisfied` with an empty asset id
// — is a claim no reader can verify, and it is the shape that would let a batch proceed
// against an asset nobody named.
func TestASatisfiedItemMustNameTheAssetThatSatisfiesIt(t *testing.T) {
	if err := gapItem(1, TypeCharacter, "entity-1", true, GapSatisfied, "asset-1").Validate(); err != nil {
		t.Fatalf("a well-formed satisfied item was refused: %v", err)
	}
	if err := gapItem(1, TypeCharacter, "entity-1", true, GapSatisfied, "").Validate(); err == nil {
		t.Fatal("a satisfied item naming no asset was accepted, so nothing says what satisfies it")
	}
	// The reverse is refused for the same reason: a line that names an asset is not missing.
	if err := gapItem(1, TypeCharacter, "entity-1", true, GapMissing, "asset-1").Validate(); err == nil {
		t.Fatal("a missing item naming an asset was accepted")
	}
	// A missing line has no asset id to cite, so the STORY fact is what it must name.
	if err := gapItem(1, TypeCharacter, "", true, GapMissing, "").Validate(); err == nil {
		t.Fatal("a missing item naming nothing at all was accepted")
	}
	// A name is enough when there is no story entity: an analyzer may find a costume the
	// story graph never modelled, and demanding an id would refuse a real gap.
	named := gapItem(1, TypeCostume, "", true, GapMissing, "")
	named.StoryEntityName = "Lin's travelling cloak"
	if err := named.Validate(); err != nil {
		t.Fatalf("a missing item named only by its story name was refused: %v", err)
	}
}

// TestTheBlockingPredicateNeedsBothHalves covers the conjunction AC-BOARD-001 turns on.
//
// The four combinations matter and only one is a blocker: a required line that is missing
// stops a batch, while a missing OPTIONAL asset is a note and a satisfied required one is
// not a gap. A predicate that dropped either half would either block on notes or let a
// batch run without a character.
func TestTheBlockingPredicateNeedsBothHalves(t *testing.T) {
	cases := []struct {
		name     string
		item     GapItem
		blocking bool
	}{
		{"required and missing", gapItem(1, TypeCharacter, "e", true, GapMissing, ""), true},
		{"required and satisfied", gapItem(1, TypeCharacter, "e", true, GapSatisfied, "a"), false},
		{"optional and missing", gapItem(1, TypeCharacter, "e", false, GapMissing, ""), false},
		{"optional and satisfied", gapItem(1, TypeCharacter, "e", false, GapSatisfied, "a"), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.item.Unsatisfied(); got != testCase.blocking {
				t.Fatalf("Unsatisfied() = %v, want %v", got, testCase.blocking)
			}
		})
	}
	// The reader returns the blocking lines in the report's own order, so a refusal that
	// names the first few names them the way the approver saw them.
	items := []GapItem{
		gapItem(1, TypeCharacter, "e1", false, GapMissing, ""),
		gapItem(2, TypeLocation, "e2", true, GapMissing, ""),
		gapItem(3, TypeProp, "e3", true, GapSatisfied, "a3"),
		gapItem(4, TypeCostume, "e4", true, GapMissing, ""),
	}
	unresolved := UnresolvedRequiredItems(items)
	if len(unresolved) != 2 || unresolved[0].Ordinal != 2 || unresolved[1].Ordinal != 4 {
		t.Fatalf("the unresolved required items are %+v", unresolved)
	}
}

// TestAGapReportCannotBeApprovedWhileSomethingRequiredIsMissing covers the refusal the
// approval path makes, and the three states that are refused for other reasons.
func TestAGapReportCannotBeApprovedWhileSomethingRequiredIsMissing(t *testing.T) {
	approved := []GapItem{
		gapItem(1, TypeCharacter, "e1", true, GapSatisfied, "a1"),
		gapItem(2, TypeLocation, "e2", false, GapMissing, ""),
	}
	if err := CanApproveGapReport(versioning.StatusCandidate, approved); err != nil {
		t.Fatalf("a report whose required assets are satisfied was refused: %v", err)
	}
	// One unresolved REQUIRED line is enough to refuse, and the message says why rather
	// than leaving the caller to find the line.
	blocked := append([]GapItem{}, approved...)
	blocked = append(blocked, gapItem(3, TypeCostume, "e3", true, GapMissing, ""))
	if err := CanApproveGapReport(versioning.StatusCandidate, blocked); err == nil {
		t.Fatal("a report with a required asset missing was approved, so the batch gate it feeds would pass too")
	}
	// An analysis that produced no items is refused: either the script has no cast, or the
	// run did not do its work.
	if err := CanApproveGapReport(versioning.StatusCandidate, nil); err == nil {
		t.Fatal("a report with no items was approved")
	}
	// The status gates are the versioning family's, so a report already approved is not
	// approved twice and a superseded one is not put back in force.
	if err := CanApproveGapReport(versioning.StatusApproved, approved); err == nil {
		t.Fatal("an already-approved report was approved again")
	}
	if err := CanApproveGapReport(versioning.StatusSuperseded, approved); err == nil {
		t.Fatal("a superseded report was approved")
	}
	if err := CanApproveGapReport(versioning.StatusStale, approved); err == nil {
		t.Fatal("a stale report was approved")
	}
	if err := CanApproveGapReport("invented", approved); err == nil {
		t.Fatal("a report in an invented status was approved")
	}
}

// TestAGapReportNamesBothTheEpisodeAndTheScriptVersion covers the two fields a report
// without which could not be acted on.
func TestAGapReportNamesBothTheEpisodeAndTheScriptVersion(t *testing.T) {
	base := GapReport{
		EpisodeID:       "episode-1",
		ScriptVersionID: "script-version-1",
		VersionNumber:   1,
		Status:          versioning.StatusCandidate,
		CreatedByType:   versioning.CreatedByAgent,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed report was refused: %v", err)
	}
	// An episode with no script version could not say which revision it analysed, which is
	// exactly the fact a re-analysis is compared against.
	noVersion := base
	noVersion.ScriptVersionID = "  "
	if err := noVersion.Validate(); err == nil {
		t.Fatal("a report naming no script version was accepted")
	}
	noEpisode := base
	noEpisode.EpisodeID = ""
	if err := noEpisode.Validate(); err == nil {
		t.Fatal("a report naming no episode was accepted")
	}
	// The version number starts at one and the producer is a closed vocabulary, because a
	// report nothing produced could not be audited.
	zero := base
	zero.VersionNumber = 0
	if err := zero.Validate(); err == nil {
		t.Fatal("a report at version zero was accepted")
	}
	unknown := base
	unknown.CreatedByType = "invented"
	if err := unknown.Validate(); err == nil {
		t.Fatal("a report from an invented producer was accepted")
	}
}
