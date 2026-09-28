package database

import (
	"context"
	"testing"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// TestApprovingAPanelImageMovesTheStatusTheExportJoinsOn is the regression test for a defect that
// made every approved panel invisible to the export.
//
// # The defect
//
// The read the MP4 export is built from joins the panel with `AND p.status = 'approved'`
// (`timeline.go:101`, `final_reader.go:200`). `ApprovePanelImage` wrote
// `approved_image_asset_version_id` — and NOTHING in the repository wrote the status. A panel
// created by the production path therefore stayed `draft` forever, so the approval recorded an
// image the export could not see.
//
// It was found by MEASURING rather than by reading, and the measurement is worse than the reading
// suggested: with a previously approved panel on the row, the timeline did not report "no media" —
// it reported the PREVIOUS version, because that panel was the one still carrying the status the
// join wanted. A user who approved a replacement frame would export the old one.
//
// The same defect silently disabled a constraint: migration 000010's
// `idx_storyboard_panel_versions_approved ... WHERE status = 'approved'` exists to make "one
// approved panel per row" enforceable, and no row ever carried the value it constrains.
//
// # What this test asserts
//
// Both halves of the approval, through the real service over the real schema: the panel's status
// moves to `approved`, the image it names is recorded, and the timeline — the export's own read —
// resolves the row's media to the version that was just approved rather than to the one in force
// before.
//
// The fixture writes a PREVIOUSLY approved panel before the approval under test, because that is
// what turns "the status is missing" into "the wrong frame exports", which is the failure a user
// would have seen.
func TestApprovingAPanelImageMovesTheStatusTheExportJoinsOn(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()

	// A board with one row, written by the same helper WP-11's own walk uses, so the rows this
	// probe approves against are the ones the timeline's join expects to find.
	boardVersionID := harness.approvedBoard(t, 1, 3)
	rows := harness.rows(t, boardVersionID)
	if len(rows) == 0 {
		t.Fatal("the board has no rows")
	}
	row := rows[0]

	// An asset version that exists and has committed bytes, which is what an approved image names.
	hash := harness.put(t, "probe-approved.png", pngFixture(t, 64, 48, 128))
	if err := writeAssetWithVersion(ctx, harness.db, "probe-approved-asset", "probe-approved-version", hash, 7); err != nil {
		t.Fatalf("writing the asset version: %v", err)
	}

	// A panel version for the row, created through the SERVICE — the production path.
	repo := NewStoryboardRepository(harness.db)
	panels := appstoryboard.NewService(appstoryboard.Options{
		DirectorPlans: repo,
		Storyboards:   repo,
		Items:         repo,
		Panels:        repo,
		Clock:         mediaClock{at: harness.now},
		IDs:           harness,
		Events:        mediaEvents{},
	})
	panel, err := panels.CreatePanelVersion(ctx, appstoryboard.CreatePanelVersionRequest{
		StoryboardItemID: row.ID,
		VisualPrompt:     "a probe frame",
		CreatedByType:    versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("CreatePanelVersion: %v", err)
	}
	if panel.Status != versioning.StatusDraft {
		t.Fatalf("a created panel version is %q, want draft: an approval is what moves it, and this test is about the approval", panel.Status)
	}

	// The approval, through the service. The candidate list must contain the approved image
	// (section 9.5's rule), so it names it.
	current, err := panels.GetStoryboardItem(ctx, row.ID)
	if err != nil {
		t.Fatalf("GetStoryboardItem: %v", err)
	}
	approved, err := panels.ApprovePanelImage(ctx, appstoryboard.ApprovePanelImageRequest{
		PanelVersionID:              panel.ID,
		ApprovedImageAssetVersionID: "probe-approved-version",
		CandidateVersionIDs:         []string{"probe-approved-version"},
		ExpectedRevision:            current.Revision,
	})
	if err != nil {
		t.Fatalf("ApprovePanelImage: %v", err)
	}
	if approved.ApprovedImageAssetVersionID != "probe-approved-version" {
		t.Fatalf("the approval did not record the image: %+v", approved)
	}

	// The status is the half the export joins on, so it is read back from SQLite rather than
	// taken from the returned struct: a service that returned the right thing while the row said
	// otherwise is exactly the defect this test exists for.
	var storedStatus, storedImage string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT status, approved_image_asset_version_id FROM storyboard_panel_versions WHERE id = ?`,
		panel.ID).Scan(&storedStatus, &storedImage); err != nil {
		t.Fatalf("reading the panel back: %v", err)
	}
	if storedStatus != string(versioning.StatusApproved) {
		t.Fatalf("the approved panel's stored status is %q, want %q: the export's join carries `AND p.status = 'approved'`, so an approval that leaves the status behind records an image nothing can see",
			storedStatus, versioning.StatusApproved)
	}
	if storedImage != "probe-approved-version" {
		t.Fatalf("the panel records the image %q, want the one that was approved", storedImage)
	}

	// And the export's own read must resolve the row's media to it. This is the assertion the
	// defect failed: it resolves through the timeline rather than through the panel table,
	// because the timeline is what the MP4 is composed from.
	timeline, err := harness.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatalf("reading the timeline: %v", err)
	}
	var seen string
	for _, shot := range timeline.Shots {
		if shot.ItemID == row.ID {
			seen = shot.MediaVersionID
		}
	}
	if seen != "probe-approved-version" {
		t.Fatalf("the timeline reports the row's media as %q, want the version that was just approved: an export composed from this read would produce the WRONG FRAME, not a missing one",
			seen)
	}
}

// mediaEvents satisfies the storyboard service's event recorder with a no-op, because the probe
// asks about the READ path and an approval that could not announce itself must not be refused: the
// service treats the recorder as enabling, and a nil one leaves the approval failing closed in a
// way that has nothing to do with this question. The acceptance walk uses the real recorder.
type mediaEvents struct{}

func (mediaEvents) Build(context.Context, eventsapp.Draft) (event.Event, error) {
	return event.Event{}, nil
}

func (mediaEvents) RecordBestEffort(context.Context, eventsapp.Draft) {}

// Compile-time proof that the assets service is reachable from this package's tests, which the
// acceptance walk depends on.
var _ = appassets.NewService
var _ = asset.TypeImage

// TestApprovingASecondPanelImageSupersedesTheFirst is the replacement half of the same rule.
//
// §9.5's "one approved image per panel" is a CONSTRAINT — migration 000010's partial unique index
// on `(storyboard_item_id) WHERE status = 'approved'` — so approving a second image has to move the
// first out of `approved` rather than coexisting with it. The order matters and is the same one
// `approveVersionWithEvent` uses for the other four version families: supersede, then approve.
// Approving first would transiently leave two approved panels on one row, which the index refuses,
// so a reversed order fails on the INSERT rather than silently.
//
// This test also pins what the EXPORT sees afterwards: the row's media must resolve to the NEW
// image. Before the status fix both panels were `draft`, the join found neither, and the row read
// its previous version — so a replacement was invisible rather than refused.
func TestApprovingASecondPanelImageSupersedesTheFirst(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()

	boardVersionID := harness.approvedBoard(t, 1, 3)
	rows := harness.rows(t, boardVersionID)
	if len(rows) == 0 {
		t.Fatal("the board has no rows")
	}
	row := rows[0]

	for _, name := range []string{"first.png", "second.png"} {
		hash := harness.put(t, name, pngFixture(t, 64, 48, 90))
		if err := writeAssetWithVersion(ctx, harness.db, "asset-"+name, "version-"+name, hash, 7); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	repo := NewStoryboardRepository(harness.db)
	panels := appstoryboard.NewService(appstoryboard.Options{
		DirectorPlans: repo, Storyboards: repo, Items: repo, Panels: repo,
		Clock: mediaClock{at: harness.now}, IDs: harness, Events: mediaEvents{},
	})

	// Two panel versions for the same row, which is §9.5's "several candidates, one approved".
	var versionIDs []string
	for index, name := range []string{"first", "second"} {
		panel, err := panels.CreatePanelVersion(ctx, appstoryboard.CreatePanelVersionRequest{
			StoryboardItemID: row.ID,
			VisualPrompt:     name + " frame",
			CreatedByType:    versioning.CreatedByUser,
		})
		if err != nil {
			t.Fatalf("CreatePanelVersion(%s): %v", name, err)
		}
		versionIDs = append(versionIDs, panel.ID)
		_ = index
	}

	approve := func(panelID, imageVersion string) {
		t.Helper()
		current, err := panels.GetStoryboardItem(ctx, row.ID)
		if err != nil {
			t.Fatalf("GetStoryboardItem: %v", err)
		}
		if _, err := panels.ApprovePanelImage(ctx, appstoryboard.ApprovePanelImageRequest{
			PanelVersionID:              panelID,
			ApprovedImageAssetVersionID: imageVersion,
			CandidateVersionIDs:         []string{imageVersion},
			ExpectedRevision:            current.Revision,
		}); err != nil {
			t.Fatalf("ApprovePanelImage(%s): %v", imageVersion, err)
		}
	}

	approve(versionIDs[0], "version-first.png")
	approve(versionIDs[1], "version-second.png")

	// The first is superseded rather than still approved, and there is exactly one approved row.
	statusOf := func(panelID string) string {
		t.Helper()
		var status string
		if err := harness.db.QueryRowContext(ctx,
			`SELECT status FROM storyboard_panel_versions WHERE id = ?`, panelID).Scan(&status); err != nil {
			t.Fatalf("reading the panel status: %v", err)
		}
		return status
	}
	if got := statusOf(versionIDs[0]); got != string(versioning.StatusSuperseded) {
		t.Fatalf("the replaced panel is %q, want superseded: two approved panels on one row would violate the partial unique index the migration declares", got)
	}
	if got := statusOf(versionIDs[1]); got != string(versioning.StatusApproved) {
		t.Fatalf("the new panel is %q, want approved", got)
	}
	var approvedRows int
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM storyboard_panel_versions WHERE storyboard_item_id = ? AND status = 'approved'`,
		row.ID).Scan(&approvedRows); err != nil {
		t.Fatal(err)
	}
	if approvedRows != 1 {
		t.Fatalf("the row has %d approved panel versions, want exactly one", approvedRows)
	}

	// The export's read sees the NEW frame. Before the fix it saw neither and fell back to
	// whatever the fixture had approved, which is the "replacement silently did nothing" case.
	timeline, err := harness.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatalf("reading the timeline: %v", err)
	}
	for _, shot := range timeline.Shots {
		if shot.ItemID != row.ID {
			continue
		}
		if shot.MediaVersionID != "version-second.png" {
			t.Fatalf("the timeline reports the row's media as %q, want the replacement (%q)", shot.MediaVersionID, "version-second.png")
		}
	}
}
