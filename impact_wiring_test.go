package main

import (
	"context"
	"database/sql"
	"testing"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// impact_wiring_test.go covers §15.1's asset-version trigger end to end, through the
// composition root rather than through a double.
//
// THE DEFECT IT EXISTS FOR: WP-05 built the staleness graph with four storyboard artifact
// types AND built the propagation service, and nothing joined them to an approval switch.
// §15.1 lists "AssetVersion 默认批准版本切换" as a trigger, so the rule was written, the
// mechanism existed, and no code path fired it. A unit test of the graph cannot see that —
// from inside the graph, "no caller" and "no change" are the same state — which is why
// this drives the composed stack the way the application does.

// TestApprovingAnAssetVersionMarksThePanelsHoldingTheOldOne drives the whole chain.
//
// The assertion is on a MARK row read through the staleness service rather than through
// the asset service that wrote it: the propagation's whole point is that a reader who
// never saw the approval can find out what it disturbed.
func TestApprovingAnAssetVersionMarksThePanelsHoldingTheOldOne(t *testing.T) {
	ctx := context.Background()
	stack, canvasWriter, handle := composedDrama(t)
	projectID := seedWiringProject(t, canvasWriter)

	// A script version, because the storyboard version must name both the script and the
	// director plan it was built from.
	episode, err := stack.script.CreateEpisode(ctx, appscript.CreateEpisodeRequest{
		ProjectID: projectID, SeasonNumber: 1, EpisodeNumber: 1, Title: "Impact",
	})
	if err != nil {
		t.Fatalf("creating an episode: %v", err)
	}
	scriptRecord, err := stack.script.EnsureScript(ctx, episode.ID)
	if err != nil {
		t.Fatalf("ensuring the script: %v", err)
	}
	skeleton, err := stack.script.CreateStorySkeletonVersion(ctx, appscript.CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", EndingHook: "ending",
	})
	if err != nil {
		t.Fatalf("creating a skeleton: %v", err)
	}
	strategy, err := stack.script.CreateAdaptationStrategyVersion(ctx, appscript.CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "summary",
	})
	if err != nil {
		t.Fatalf("creating a strategy: %v", err)
	}
	scriptVersion, err := stack.script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
		ScriptID: scriptRecord.ID, StorySkeletonVersionID: skeleton.ID,
		AdaptationStrategyVersionID: strategy.ID, CreatedByType: versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("creating a script version: %v", err)
	}

	// An asset with two versions, each carrying a committed file.
	record, first, err := stack.assets.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: projectID, Type: asset.TypeImage, Name: "Lin's portrait",
	})
	if err != nil {
		t.Fatalf("creating the asset: %v", err)
	}
	commitWiringFile(t, handle.SQL(), "aaaa")
	commitWiringFile(t, handle.SQL(), "bbbb")
	if _, err := stack.assets.AttachFile(ctx, first.ID, wiringHash("aaaa"), asset.RolePrimary); err != nil {
		t.Fatalf("attaching the first file: %v", err)
	}
	second, err := stack.assets.AddVersion(ctx, appassets.AddVersionRequest{AssetID: record.ID})
	if err != nil {
		t.Fatalf("adding the second version: %v", err)
	}
	if _, err := stack.assets.AttachFile(ctx, second.ID, wiringHash("bbbb"), asset.RolePrimary); err != nil {
		t.Fatalf("attaching the second file: %v", err)
	}
	// The FIRST version is approved, so it is the one a panel can hold.
	if _, err := stack.assets.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: first.ID, ImpactAcknowledged: true,
	}); err != nil {
		t.Fatalf("approving the first version: %v", err)
	}

	// A storyboard with one item and one panel whose approved image is the first version.
	plan, err := stack.storyboard.CreateDirectorPlanVersion(ctx, appstoryboard.CreateDirectorPlanVersionRequest{
		EpisodeID: episode.ID, ScriptVersionID: scriptVersion.ID,
	})
	if err != nil {
		t.Fatalf("creating the director plan: %v", err)
	}
	storyboardRecord, err := stack.storyboard.EnsureStoryboard(ctx, episode.ID)
	if err != nil {
		t.Fatalf("ensuring the storyboard: %v", err)
	}
	storyboardVersion, err := stack.storyboard.CreateStoryboardVersion(ctx, appstoryboard.CreateStoryboardVersionRequest{
		StoryboardID: storyboardRecord.ID, ScriptVersionID: scriptVersion.ID,
		DirectorPlanVersionID: plan.ID, CreatedByType: versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("creating the storyboard version: %v", err)
	}
	item, err := stack.storyboard.CreateStoryboardItem(ctx, appstoryboard.CreateStoryboardItemRequest{
		StoryboardVersionID: storyboardVersion.ID, ShotID: "wiring-shot-1", Ordinal: 1,
		ShotSize: "MS", DurationSeconds: 4,
	})
	if err != nil {
		t.Fatalf("creating the storyboard item: %v", err)
	}
	panel, err := stack.storyboard.CreatePanelVersion(ctx, appstoryboard.CreatePanelVersionRequest{
		StoryboardItemID: item.ID, VisualPrompt: "a corridor", CreatedByType: versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("creating the panel: %v", err)
	}
	// The revision the approval guards against is the ITEM's, because the panel's row
	// belongs to it — see the request's own comment.
	storedItem, err := stack.storyboard.GetStoryboardItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if _, err := stack.storyboard.ApprovePanelImage(ctx, appstoryboard.ApprovePanelImageRequest{
		PanelVersionID:              panel.ID,
		ApprovedImageAssetVersionID: first.ID,
		CandidateVersionIDs:         []string{first.ID},
		ExpectedRevision:            storedItem.Revision,
	}); err != nil {
		t.Fatalf("approving the panel's image: %v", err)
	}
	// Nothing is stale yet, which is what makes the assertion below about the SWITCH.
	before, err := stack.staleness.ListMarks(ctx, projectID)
	if err != nil {
		t.Fatalf("reading the marks: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("the fixture already carries %d marks: %+v", len(before), before)
	}

	// THE ASSERTION. Approving the second version switches what is in force, and §15.1
	// makes that a trigger: the panel holding the first must be marked for re-review.
	if _, err := stack.assets.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: second.ID, ImpactAcknowledged: true,
	}); err != nil {
		t.Fatalf("approving the second version: %v", err)
	}
	marks, err := stack.staleness.ListMarks(ctx, projectID)
	if err != nil {
		t.Fatalf("reading the marks after the switch: %v", err)
	}
	if len(marks) == 0 {
		t.Fatal("approving a new asset version marked nothing, so §15.1's trigger fired into nothing")
	}
	found := false
	for _, mark := range marks {
		if mark.ArtifactType != staleness.ArtifactStoryboardPanel || mark.ArtifactID != panel.ID {
			continue
		}
		found = true
		// The severity is review_required because the panel's reference IS the changed
		// artifact — it named the version that is no longer in force.
		if mark.Severity != staleness.SeverityReviewRequired {
			t.Errorf("the panel's mark is %q, want review_required", mark.Severity)
		}
		if mark.UpstreamID != first.ID || mark.UpstreamType != staleness.ArtifactAssetVersion {
			t.Errorf("the mark attributes the change to %s/%s, want the version that was replaced",
				mark.UpstreamType, mark.UpstreamID)
		}
	}
	if !found {
		t.Fatalf("the panel holding the replaced version is not among the marks: %+v", marks)
	}
}

// TestTheApprovalImpactNamesThePanelsHoldingTheOldVersion covers PRD FR-050's sentence.
//
// "替换批准版本时，系统列出受影响的分镜和镜头" is about LISTING, not about marking: a user
// about to approve a new version must be able to see what the switch disturbs BEFORE
// they make it. `ApprovalImpactOf` answers that, and this asserts the answer's two lists.
func TestTheApprovalImpactNamesThePanelsHoldingTheOldVersion(t *testing.T) {
	ctx := context.Background()
	stack, canvasWriter, handle := composedDrama(t)
	projectID := seedWiringProject(t, canvasWriter)
	record, first, err := stack.assets.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: projectID, Type: asset.TypeImage, Name: "Lin's portrait",
	})
	if err != nil {
		t.Fatalf("creating the asset: %v", err)
	}
	commitWiringFile(t, handle.SQL(), "cccc")
	commitWiringFile(t, handle.SQL(), "dddd")
	if _, err := stack.assets.AttachFile(ctx, first.ID, wiringHash("cccc"), asset.RolePrimary); err != nil {
		t.Fatalf("attaching the first file: %v", err)
	}
	second, err := stack.assets.AddVersion(ctx, appassets.AddVersionRequest{AssetID: record.ID})
	if err != nil {
		t.Fatalf("adding the second version: %v", err)
	}
	if _, err := stack.assets.AttachFile(ctx, second.ID, wiringHash("dddd"), asset.RolePrimary); err != nil {
		t.Fatalf("attaching the second file: %v", err)
	}
	if _, err := stack.assets.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: first.ID, ImpactAcknowledged: true,
	}); err != nil {
		t.Fatalf("approving the first version: %v", err)
	}
	// A usage naming the version in force, which is the reference the list is built from:
	// §8.6's consumer_type for a panel's image is `storyboard_panel`.
	if _, err := stack.assets.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: first.ID, ConsumerType: asset.ConsumerStoryboardPanel,
		ConsumerID: "panel-1", UsageRole: "reference", Required: true,
	}); err != nil {
		t.Fatalf("recording the usage: %v", err)
	}

	impact, err := stack.assets.ApprovalImpactOf(ctx, second.ID)
	if err != nil {
		t.Fatalf("reading the impact: %v", err)
	}
	if impact.Replaces != first.ID {
		t.Fatalf("the impact says it replaces %q, want the version in force", impact.Replaces)
	}
	if len(impact.Consumers) != 1 || impact.Consumers[0].ConsumerID != "panel-1" {
		t.Fatalf("the impact lists %+v, want the panel using the old version", impact.Consumers)
	}
	// The REQUIRED subset is what a caller gates on, so it is reported separately rather
	// than left for the caller to filter.
	if len(impact.RequiredConsumers) != 1 {
		t.Fatalf("the required consumers are %+v", impact.RequiredConsumers)
	}
	// An OPTIONAL usage appears in the full list and NOT in the required subset, which is
	// the distinction the two fields exist for.
	if _, err := stack.assets.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: first.ID, ConsumerType: asset.ConsumerScene,
		ConsumerID: "scene-1", UsageRole: "reference", Required: false,
	}); err != nil {
		t.Fatalf("recording the optional usage: %v", err)
	}
	impact, err = stack.assets.ApprovalImpactOf(ctx, second.ID)
	if err != nil {
		t.Fatalf("re-reading the impact: %v", err)
	}
	if len(impact.Consumers) != 2 {
		t.Fatalf("the impact lists %d consumers, want both", len(impact.Consumers))
	}
	if len(impact.RequiredConsumers) != 1 {
		t.Fatalf("the required consumers are %+v, want only the panel", impact.RequiredConsumers)
	}
	// And an asset with NOTHING approved has nothing to replace, which is the direction
	// that keeps the analysis from inventing work. The version read is the asset's FIRST
	// version — the one `CreateAsset` writes — because `CurrentApprovedVersionID` is empty
	// here and there is no version to name by that route.
	fresh, freshFirst, err := stack.assets.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: projectID, Type: asset.TypeImage, Name: "Untouched",
	})
	if err != nil {
		t.Fatalf("creating the second asset: %v", err)
	}
	if fresh.CurrentApprovedVersionID != "" {
		t.Fatalf("a fresh asset reports %q as approved", fresh.CurrentApprovedVersionID)
	}
	impact, err = stack.assets.ApprovalImpactOf(ctx, freshFirst.ID)
	if err != nil {
		t.Fatalf("reading the impact of an unapproved asset: %v", err)
	}
	if impact.Replaces != "" || len(impact.Consumers) != 0 || len(impact.RequiredConsumers) != 0 {
		t.Fatalf("an asset with nothing approved reports %+v", impact)
	}
}

// commitWiringFile stores one file object so an asset file link has a hash to cite.
//
// The row is written directly rather than through the FileStore, and that is the right
// level here: what the asset link needs is the `file_objects` row its foreign key
// references, and the FileStore's own round trip is covered by its own tests.
func commitWiringFile(t *testing.T, db *sql.DB, seed string) {
	t.Helper()
	hash := wiringHash(seed)
	if _, err := db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
		 VALUES (?, ?, 'image/png', 10, '2026-01-01T00:00:00Z')`, hash, hash); err != nil {
		t.Fatalf("committing the file %s: %v", hash, err)
	}
}

// wiringHash builds a 64-character hash from a seed, which the schema's foreign key and
// the domain's validator both require.
func wiringHash(seed string) string {
	out := ""
	for len(out) < 64 {
		out += seed
	}
	return out[:64]
}
