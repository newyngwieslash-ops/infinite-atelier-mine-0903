package assets_test

import (
	"context"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// gap_service_test.go covers the gap service's commands against a double, where the
// questions are about ORDER and REFUSAL rather than about SQL.

type gapRepoFake struct {
	reports map[string]asset.GapReport
	items   map[string][]asset.GapItem
	order   []string
	// approvedByEpisode is the partial-index behaviour: at most one approved report.
	approvedByEpisode map[string]string
	createdReports    int
	createdItems      int
	approveCalls      int
	// lastExpectedStatus records the CAS value the service passed, so a test can check
	// it came from the ROW rather than from the request.
	lastExpectedStatus asset.VersionStatus
	lastTraceID        string
	failNextCreate     error
}

func newGapRepoFake() *gapRepoFake {
	return &gapRepoFake{
		reports:           map[string]asset.GapReport{},
		items:             map[string][]asset.GapItem{},
		approvedByEpisode: map[string]string{},
	}
}

func (f *gapRepoFake) CreateGapReportWithItems(_ context.Context, report asset.GapReport, items []asset.GapItem) error {
	if f.failNextCreate != nil {
		err := f.failNextCreate
		f.failNextCreate = nil
		return err
	}
	// The real store writes both or neither, so the double does too.
	f.createdReports++
	f.createdItems += len(items)
	f.reports[report.ID] = report
	f.items[report.ID] = append([]asset.GapItem{}, items...)
	f.order = append(f.order, report.ID)
	return nil
}

func (f *gapRepoFake) GetGapReport(_ context.Context, id string) (asset.GapReport, error) {
	report, ok := f.reports[id]
	if !ok {
		return asset.GapReport{}, asset.NotFoundError()
	}
	return report, nil
}

func (f *gapRepoFake) ListGapReports(_ context.Context, episodeID string) ([]asset.GapReport, error) {
	out := []asset.GapReport{}
	for index := len(f.order) - 1; index >= 0; index-- {
		report := f.reports[f.order[index]]
		if report.EpisodeID == episodeID {
			out = append(out, report)
		}
	}
	return out, nil
}

func (f *gapRepoFake) MaxGapReportVersionNumber(_ context.Context, episodeID string) (int, error) {
	highest := 0
	for _, report := range f.reports {
		if report.EpisodeID == episodeID && report.VersionNumber > highest {
			highest = report.VersionNumber
		}
	}
	return highest, nil
}

func (f *gapRepoFake) CurrentApprovedGapReport(_ context.Context, episodeID string) (asset.GapReport, bool, error) {
	id, ok := f.approvedByEpisode[episodeID]
	if !ok {
		return asset.GapReport{}, false, nil
	}
	return f.reports[id], true, nil
}

func (f *gapRepoFake) ListGapItems(_ context.Context, reportID string) ([]asset.GapItem, error) {
	return f.items[reportID], nil
}

func (f *gapRepoFake) ApproveGapReport(_ context.Context, reportID, episodeID string, expectedStatus asset.VersionStatus, traceID string, at time.Time) error {
	f.approveCalls++
	f.lastExpectedStatus = expectedStatus
	f.lastTraceID = traceID
	report, ok := f.reports[reportID]
	if !ok {
		return asset.NotFoundError()
	}
	if report.Status != expectedStatus {
		return asset.ConflictError("This gap report changed in another window. Reload it and try again.")
	}
	if previous, ok := f.approvedByEpisode[episodeID]; ok && previous != reportID {
		prior := f.reports[previous]
		prior.Status = asset.VersionSuperseded
		f.reports[previous] = prior
	}
	report.Status = asset.VersionApproved
	report.ApprovalTraceID = traceID
	report.UpdatedAt = at
	f.reports[reportID] = report
	f.approvedByEpisode[episodeID] = reportID
	return nil
}

var _ assets.GapRepository = (*gapRepoFake)(nil)

// gapIDs mints deterministic identifiers.
type gapIDs struct{ next int }

func (g *gapIDs) New() (string, error) {
	g.next++
	return "gap-id-" + itoa(g.next), nil
}

// gapClock is a fixed clock.
type gapClock struct{}

func (gapClock) Now() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }

func itoa(value int) string {
	return string(rune('0' + value))
}

func gapServiceWith(repo assets.GapRepository) *assets.GapService {
	return assets.NewGapService(assets.GapOptions{Gaps: repo, Clock: gapClock{}, IDs: &gapIDs{}})
}

// TestTheOrdinalsAndIdentifiersAreAssignedRatherThanAccepted covers §17's "ID、顺序和唯一性".
//
// A caller states its lines in an order; the SERVICE numbers them, and it numbers them
// from the slice rather than from anything the caller supplied. A model that supplied its
// own ordinals could leave a hole or repeat one, and the schema would then refuse a write
// the model had no way to fix.
func TestTheOrdinalsAndIdentifiersAreAssignedRatherThanAccepted(t *testing.T) {
	repo := newGapRepoFake()
	service := gapServiceWith(repo)
	report, items, err := service.CreateGapReport(context.Background(), assets.CreateGapReportRequest{
		EpisodeID:       "episode-1",
		ScriptVersionID: "script-version-1",
		Summary:         "Two things are needed.",
		Items: []assets.GapItemInput{
			{AssetType: asset.TypeCharacter, StoryEntityID: "e1", Status: asset.GapMissing, Required: true},
			{AssetType: asset.TypeLocation, StoryEntityID: "e2", Status: asset.GapMissing, Required: false},
			{AssetType: asset.TypeProp, StoryEntityName: "A lantern", Status: asset.GapMissing, Required: true},
		},
	})
	if err != nil {
		t.Fatalf("creating the report: %v", err)
	}
	if report.VersionNumber != 1 || report.Status != asset.VersionDraft {
		t.Fatalf("the report is %+v", report)
	}
	if len(items) != 3 {
		t.Fatalf("the report has %d items", len(items))
	}
	for index, item := range items {
		if item.Ordinal != index+1 {
			t.Errorf("item %d has ordinal %d", index, item.Ordinal)
		}
		if item.ID == "" || item.ReportID != report.ID {
			t.Errorf("item %d is %+v", index, item)
		}
		if item.UsageRole != "reference" {
			t.Errorf("item %d has role %q, want the default", index, item.UsageRole)
		}
	}
	// A second report for the same episode takes the NEXT number rather than reusing one,
	// which the schema's unique constraint would refuse.
	second, _, err := service.CreateGapReport(context.Background(), assets.CreateGapReportRequest{
		EpisodeID:       "episode-1",
		ScriptVersionID: "script-version-1",
		Items: []assets.GapItemInput{
			{AssetType: asset.TypeCharacter, StoryEntityID: "e1", Status: asset.GapMissing, Required: true},
		},
	})
	if err != nil {
		t.Fatalf("creating the second report: %v", err)
	}
	if second.VersionNumber != 2 {
		t.Fatalf("the second report is version %d, want 2", second.VersionNumber)
	}
}

// TestAnAnalysisWithNoLinesIsRefused covers the refusal that keeps a vacuous gate honest.
//
// A report with no lines would satisfy "no required asset is missing" for the wrong
// reason, and the batch gate downstream would let a production run against a script
// nobody had analysed.
func TestAnAnalysisWithNoLinesIsRefused(t *testing.T) {
	repo := newGapRepoFake()
	service := gapServiceWith(repo)
	if _, _, err := service.CreateGapReport(context.Background(), assets.CreateGapReportRequest{
		EpisodeID:       "episode-1",
		ScriptVersionID: "script-version-1",
	}); err == nil {
		t.Fatal("an analysis with no lines was accepted")
	}
	if repo.createdReports != 0 || repo.createdItems != 0 {
		t.Fatal("the refused analysis was partially written")
	}
	// The two facts the report is about are required, and each refusal is separate so the
	// caller learns which one is missing.
	if _, _, err := service.CreateGapReport(context.Background(), assets.CreateGapReportRequest{
		ScriptVersionID: "script-version-1",
		Items:           []assets.GapItemInput{{AssetType: asset.TypeCharacter, StoryEntityID: "e1", Status: asset.GapMissing}},
	}); err == nil {
		t.Fatal("a report naming no episode was accepted")
	}
	if _, _, err := service.CreateGapReport(context.Background(), assets.CreateGapReportRequest{
		EpisodeID: "episode-1",
		Items:     []assets.GapItemInput{{AssetType: asset.TypeCharacter, StoryEntityID: "e1", Status: asset.GapMissing}},
	}); err == nil {
		t.Fatal("a report naming no script version was accepted")
	}
}

// TestASatisfiedLineWithoutItsAssetIsRefusedBeforeTheWrite covers the domain rule reaching
// the command path: a model that fills in `satisfied` and leaves the reference blank is
// refused here rather than stored.
func TestASatisfiedLineWithoutItsAssetIsRefusedBeforeTheWrite(t *testing.T) {
	repo := newGapRepoFake()
	service := gapServiceWith(repo)
	_, _, err := service.CreateGapReport(context.Background(), assets.CreateGapReportRequest{
		EpisodeID:       "episode-1",
		ScriptVersionID: "script-version-1",
		Items: []assets.GapItemInput{
			{AssetType: asset.TypeCharacter, StoryEntityID: "e1", Status: asset.GapSatisfied, Required: true},
		},
	})
	if err == nil {
		t.Fatal("a satisfied line naming no asset was accepted")
	}
	if repo.createdReports != 0 {
		t.Fatal("the invalid report was written")
	}
}

// TestUnresolvedRequiredItemsRefusesWhenNothingIsApproved covers the distinction the batch
// gate turns on: "no analysis" is NOT "nothing is missing".
func TestUnresolvedRequiredItemsRefusesWhenNothingIsApproved(t *testing.T) {
	repo := newGapRepoFake()
	service := gapServiceWith(repo)
	if _, err := service.UnresolvedRequiredItems(context.Background(), "episode-1"); err == nil {
		t.Fatal("an episode with no approved report reported no missing assets")
	}
	// With an approved report the answer is the blocking lines, and only those.
	report, items, err := service.CreateGapReport(context.Background(), assets.CreateGapReportRequest{
		EpisodeID:       "episode-1",
		ScriptVersionID: "script-version-1",
		Items: []assets.GapItemInput{
			{AssetType: asset.TypeCharacter, StoryEntityID: "e1", Status: asset.GapMissing, Required: true},
			{AssetType: asset.TypeProp, StoryEntityName: "A lantern", Status: asset.GapMissing, Required: false},
			{AssetType: asset.TypeLocation, StoryEntityID: "e2", AssetID: "asset-1", Status: asset.GapSatisfied, Required: true},
		},
	})
	if err != nil {
		t.Fatalf("creating the report: %v", err)
	}
	_ = items
	if _, err := service.ApproveGapReport(context.Background(), assets.ApproveGapReportRequest{ReportID: report.ID}); err == nil {
		t.Fatal("a report with a required asset missing was approved")
	}
	// A report whose required lines are satisfied is approved, and then the query answers
	// with the blocking lines rather than refusing.
	satisfied, _, err := service.CreateGapReport(context.Background(), assets.CreateGapReportRequest{
		EpisodeID:        "episode-1",
		ScriptVersionID:  "script-version-1",
		BasedOnVersionID: report.ID,
		Items: []assets.GapItemInput{
			{AssetType: asset.TypeCharacter, StoryEntityID: "e1", AssetID: "asset-char", Status: asset.GapSatisfied, Required: true},
			{AssetType: asset.TypeProp, StoryEntityName: "A lantern", Status: asset.GapMissing, Required: false},
		},
	})
	if err != nil {
		t.Fatalf("creating the satisfied report: %v", err)
	}
	if _, err := service.ApproveGapReport(context.Background(), assets.ApproveGapReportRequest{ReportID: satisfied.ID, TraceID: "trace-1"}); err != nil {
		t.Fatalf("approving a report whose required assets are satisfied: %v", err)
	}
	if repo.lastExpectedStatus != asset.VersionDraft {
		t.Fatalf("the CAS carried %q, want the status the service READ", repo.lastExpectedStatus)
	}
	if repo.lastTraceID != "trace-1" {
		t.Fatalf("the trace stored is %q", repo.lastTraceID)
	}
	unresolved, err := service.UnresolvedRequiredItems(context.Background(), "episode-1")
	if err != nil {
		t.Fatalf("asking for the unresolved items: %v", err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("the unresolved items are %+v, and the missing one is optional", unresolved)
	}
	// The refused report is STILL A DRAFT rather than superseded, and the distinction is
	// the point: nothing ever put it in force, so there is nothing for the newer report to
	// replace. A reader following the chain sees the analysis that was never approved
	// alongside the one that was, which is what §15.2's history is for.
	first, _, err := service.GetGapReport(context.Background(), report.ID)
	if err != nil {
		t.Fatalf("reading the first report: %v", err)
	}
	if first.Status != asset.VersionDraft {
		t.Fatalf("the never-approved report is %q, want draft", first.Status)
	}
	if first.ApprovalTraceID != "" {
		t.Fatalf("a report that was never approved carries the trace %q", first.ApprovalTraceID)
	}
	// And the report in force is the newer one, which is the single answer the partial
	// unique index guarantees.
	current, found, err := service.CurrentApprovedGapReport(context.Background(), "episode-1")
	if err != nil || !found || current.ID != satisfied.ID {
		t.Fatalf("the report in force is %+v, found=%v, err=%v", current, found, err)
	}
}

// TestTheApprovalCannotHappenWithoutItsReport covers the not-found direction, which is
// separate from every refusal the domain makes.
func TestTheApprovalCannotHappenWithoutItsReport(t *testing.T) {
	service := gapServiceWith(newGapRepoFake())
	if _, err := service.ApproveGapReport(context.Background(), assets.ApproveGapReportRequest{ReportID: "no-such-report"}); err == nil {
		t.Fatal("a report that does not exist was approved")
	}
}

// TestTheServiceRefusesWithoutItsDependencies covers the fail-closed direction.
func TestTheServiceRefusesWithoutItsDependencies(t *testing.T) {
	service := assets.NewGapService(assets.GapOptions{})
	if service.Available() {
		t.Fatal("a service with no dependencies reports itself available")
	}
	if _, _, err := service.CreateGapReport(context.Background(), assets.CreateGapReportRequest{
		EpisodeID: "episode-1", ScriptVersionID: "v1",
	}); err == nil {
		t.Fatal("a creation was served with no store")
	}
	if _, err := service.UnresolvedRequiredItems(context.Background(), "episode-1"); err == nil {
		t.Fatal("a query was served with no store")
	}
	if _, err := service.ApproveGapReport(context.Background(), assets.ApproveGapReportRequest{ReportID: "r"}); err == nil {
		t.Fatal("an approval was served with no store")
	}
	if _, err := service.ListGapReports(context.Background(), "episode-1"); err == nil {
		t.Fatal("a list was served with no store")
	}
	if _, _, err := service.GetGapReport(context.Background(), "r"); err == nil {
		t.Fatal("a read was served with no store")
	}
}
