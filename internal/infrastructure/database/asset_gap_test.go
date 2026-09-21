package database

import (
	"context"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// asset_gap_test.go covers the gap report's storage against the real schema: the
// round trip of every column, the one-transaction write, and the partial unique index
// that makes "the report in force" a question with one answer.

// gapFixture is a report and its lines, ready to be written.
func gapFixture(t *testing.T) (asset.GapReport, []asset.GapItem) {
	t.Helper()
	now := dramaTime()
	report := asset.GapReport{
		ID:               "gap-report-1",
		EpisodeID:        "drama-episode",
		ScriptVersionID:  "drama-script-version",
		VersionNumber:    1,
		Status:           asset.VersionDraft,
		SourceAgentRunID: "run-1",
		Summary:          "Two characters and one location are needed.",
		CreatedByType:    asset.CreatedByAgent,
		CreatedByID:      "agent-1",
		CreatedAt:        now,
		UpdatedAt:        now,
		Revision:         1,
	}
	items := []asset.GapItem{
		{
			ID: "gap-item-1", ReportID: report.ID, Ordinal: 1, AssetType: asset.TypeCharacter,
			StoryEntityID: "entity-lin", StoryEntityName: "Lin", Status: asset.GapMissing,
			UsageRole: "reference", Required: true, CreatedAt: now,
		},
		{
			ID: "gap-item-2", ReportID: report.ID, Ordinal: 2, AssetType: asset.TypeLocation,
			StoryEntityID: "entity-hall", StoryEntityName: "The hall", AssetID: "asset-hall",
			Status: asset.GapSatisfied, UsageRole: "reference", Required: true, CreatedAt: now,
		},
		{
			ID: "gap-item-3", ReportID: report.ID, Ordinal: 3, AssetType: asset.TypeProp,
			StoryEntityName: "A lantern", Status: asset.GapMissing, UsageRole: "reference",
			Required: false, Notes: "Only if the night scene is kept.", CreatedAt: now,
		},
	}
	return report, items
}

// TestTheGapReportRoundTripsThroughTheSchema covers every column, because the mapper
// reads them positionally: a column added to the SELECT without the scan would shift
// every field after it and read a plausible wrong value rather than failing.
func TestTheGapReportRoundTripsThroughTheSchema(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewAssetRepository(db)
	ctx := context.Background()
	report, items := gapFixture(t)

	if err := repo.CreateGapReportWithItems(ctx, report, items); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	stored, err := repo.GetGapReport(ctx, report.ID)
	if err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	if stored.EpisodeID != report.EpisodeID || stored.ScriptVersionID != report.ScriptVersionID ||
		stored.VersionNumber != report.VersionNumber || stored.Status != report.Status ||
		stored.SourceAgentRunID != report.SourceAgentRunID || stored.Summary != report.Summary ||
		stored.CreatedByType != report.CreatedByType || stored.CreatedByID != report.CreatedByID ||
		stored.Revision != report.Revision {
		t.Fatalf("the report came back as %+v", stored)
	}
	// The timestamps survive the round trip, which the format's truncation could break.
	if !stored.CreatedAt.Equal(report.CreatedAt) || !stored.UpdatedAt.Equal(report.UpdatedAt) {
		t.Fatalf("the report's times came back as %v / %v", stored.CreatedAt, stored.UpdatedAt)
	}
	listed, err := repo.ListGapItems(ctx, report.ID)
	if err != nil {
		t.Fatalf("reading the items: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("the report has %d items, want three", len(listed))
	}
	// The order is the report's own, and every field of every line survives: the
	// satisfied line's asset id, the optional line's note, and the boolean.
	if listed[0].StoryEntityID != "entity-lin" || !listed[0].Required || listed[0].Status != asset.GapMissing {
		t.Fatalf("the first item is %+v", listed[0])
	}
	if listed[1].AssetID != "asset-hall" || listed[1].Status != asset.GapSatisfied {
		t.Fatalf("the second item is %+v", listed[1])
	}
	if listed[2].Required || listed[2].Notes == "" || listed[2].StoryEntityName != "A lantern" {
		t.Fatalf("the third item is %+v", listed[2])
	}
	// The maximum is the last version number, which is what the service's numbering reads.
	highest, err := repo.MaxGapReportVersionNumber(ctx, "drama-episode")
	if err != nil || highest != 1 {
		t.Fatalf("the maximum version number is %d, %v", highest, err)
	}
	// And nothing is approved yet, which is the ordinary state of a fresh analysis.
	if _, found, err := repo.CurrentApprovedGapReport(ctx, "drama-episode"); err != nil || found {
		t.Fatalf("a fresh report is already in force: found=%v err=%v", found, err)
	}
}

// TestTheReportsLinesAreWrittenInOneTransaction covers the atomicity the gate depends on.
//
// A report stored with only SOME of its lines is worse than one that was not stored at
// all: the batch gate reads the stored lines to decide whether required assets are
// missing, so a partial write can drop the very line that would have blocked a batch.
// The second line here names an ordinal already used, so the whole write must roll back
// and the first line must not survive.
func TestTheReportsLinesAreWrittenInOneTransaction(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewAssetRepository(db)
	ctx := context.Background()
	report, items := gapFixture(t)
	items[1].Ordinal = 1

	if err := repo.CreateGapReportWithItems(ctx, report, items); err == nil {
		t.Fatal("a report whose two lines share an ordinal was written")
	}
	if _, err := repo.GetGapReport(ctx, report.ID); err == nil {
		t.Fatal("the report row survived a failed write, so the report and its lines can disagree")
	}
	listed, err := repo.ListGapItems(ctx, report.ID)
	if err != nil {
		t.Fatalf("reading items after the failed write: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("the first line survived a failed write: %+v", listed)
	}
}

// TestApproveGapReportSupersedesThePreviousAndRecordsTheTrace covers the one-approved
// index and the audit link in one case, because the two are written by one transaction.
func TestApproveGapReportSupersedesThePreviousAndRecordsTheTrace(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewAssetRepository(db)
	ctx := context.Background()
	first, firstItems := gapFixture(t)

	if err := repo.CreateGapReportWithItems(ctx, first, firstItems); err != nil {
		t.Fatalf("writing the first report: %v", err)
	}
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.ApproveGapReport(ctx, first.ID, first.EpisodeID, asset.VersionDraft, "trace-1", at); err != nil {
		t.Fatalf("approving the first report: %v", err)
	}
	approved, found, err := repo.CurrentApprovedGapReport(ctx, "drama-episode")
	if err != nil || !found {
		t.Fatalf("the approved report was not found: found=%v err=%v", found, err)
	}
	if approved.ID != first.ID || approved.ApprovalTraceID != "trace-1" {
		t.Fatalf("the report in force is %+v", approved)
	}

	// A second report approved afterwards makes the first superseded rather than
	// leaving two approved rows, which the partial unique index would reject.
	second := first
	second.ID = "gap-report-2"
	second.VersionNumber = 2
	second.ApprovalTraceID = ""
	secondItems := make([]asset.GapItem, len(firstItems))
	for index, item := range firstItems {
		copyItem := item
		copyItem.ReportID = second.ID
		copyItem.ID = item.ID + "-v2"
		secondItems[index] = copyItem
	}
	if err := repo.CreateGapReportWithItems(ctx, second, secondItems); err != nil {
		t.Fatalf("writing the second report: %v", err)
	}
	if err := repo.ApproveGapReport(ctx, second.ID, second.EpisodeID, asset.VersionDraft, "trace-2", at.Add(time.Hour)); err != nil {
		t.Fatalf("approving the second report: %v", err)
	}
	current, found, err := repo.CurrentApprovedGapReport(ctx, "drama-episode")
	if err != nil || !found || current.ID != second.ID {
		t.Fatalf("the report in force is %+v, found=%v, err=%v", current, found, err)
	}
	// The FIRST report is kept, not deleted: §15.2's chain requires the analysis a
	// later one replaced to remain readable.
	superseded, err := repo.GetGapReport(ctx, first.ID)
	if err != nil {
		t.Fatalf("reading the superseded report: %v", err)
	}
	if superseded.Status != asset.VersionSuperseded {
		t.Fatalf("the replaced report is %q, want superseded", superseded.Status)
	}
	// Its trace is untouched: the link names the decision that approved IT.
	if superseded.ApprovalTraceID != "trace-1" {
		t.Fatalf("the superseded report's trace is %q", superseded.ApprovalTraceID)
	}
}

// TestApproveGapReportRefusesAStaleStatus covers the CAS the approval makes.
//
// A caller whose copy of the status is out of date must not be told the approval
// succeeded: nothing moved, and the next step is to reload rather than to proceed as if
// the report were in force.
func TestApproveGapReportRefusesAStaleStatus(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewAssetRepository(db)
	ctx := context.Background()
	report, items := gapFixture(t)
	if err := repo.CreateGapReportWithItems(ctx, report, items); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	at := dramaTime()
	// The row is a draft; the caller believes it is a candidate.
	if err := repo.ApproveGapReport(ctx, report.ID, report.EpisodeID, asset.VersionCandidate, "trace-1", at); err == nil {
		t.Fatal("a report whose status had moved was approved anyway")
	}
	// And the refusal left it alone rather than half-approved.
	stored, err := repo.GetGapReport(ctx, report.ID)
	if err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	if stored.Status != asset.VersionDraft || stored.ApprovalTraceID != "" {
		t.Fatalf("the refused approval changed the row: %+v", stored)
	}
}

// TestTheGapSchemaRefusesALineWithoutItsReport covers the foreign key, so a line cannot
// exist where nothing reads it.
func TestTheGapSchemaRefusesALineWithoutItsReport(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewAssetRepository(db)
	ctx := context.Background()
	report, items := gapFixture(t)
	items[0].ReportID = "no-such-report"
	if err := repo.CreateGapReportWithItems(ctx, report, items); err == nil {
		t.Fatal("a line naming no report was written")
	}
}
