package database

import (
	"context"
	"testing"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// music_bed_wp29_test.go drives the whole path a background-music import takes over the REAL schema:
// an audio asset, its approved version, the file, the `audio_music` usage, the timeline read and the
// mix the export hands the engine.
//
// # Why this file exists when the binding has its own suite
//
// The binding's tests grade the ORDER of its five steps over a double. What a double cannot answer is the
// question that decides whether the feature exists at all: **does the usage it writes reach the mix?**
// The mix joins `asset_usages` WHERE `consumer_type = 'shot' AND consumer_id = <a row's shot>`, so a
// usage recorded any other way — however valid the row — is a row NO READ FINDS. That is the "interface
// with no real path" shape in its data form, and only a test over the real join can see it.

// importBedForWalk performs the five steps the import binding performs, against the real services.
//
// It goes through the SERVICES rather than the binding because the binding lives in another package and
// the point here is the rows, not the transfer. The order is the binding's own and is asserted there.
func importBedForWalk(t *testing.T, harness *mediaHarness, ctx context.Context, shotID, name string) string {
	t.Helper()
	// The repository carries the files, so the service needs no second port for them.
	assets := appassets.NewService(appassets.Options{
		Repository: NewAssetRepository(harness.db),
		Clock:      mediaClock{at: harness.now},
		IDs:        harness,
	})
	// The bytes go through the same store the import uses, so the file row a version cites is real.
	key := harness.put(t, name, tone(t, ctx))

	_, version, err := assets.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "drama-project", Type: asset.TypeAudio, Name: name,
	})
	if err != nil {
		t.Fatalf("creating the bed's asset: %v", err)
	}
	if _, err := assets.AttachFile(ctx, version.ID, key, asset.RolePrimary); err != nil {
		t.Fatalf("attaching the bed's file: %v", err)
	}
	// `primary` is the role the audio reader selects, so a file attached under any other role would be
	// stored and never played.
	if _, err := assets.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: version.ID, ImpactAcknowledged: true,
	}); err != nil {
		t.Fatalf("approving the bed's version: %v", err)
	}
	if _, err := assets.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: version.ID,
		ConsumerType:   asset.ConsumerShot,
		ConsumerID:     shotID,
		UsageRole:      appmedia.UsageRoleForAudio(appmedia.AudioRoleMusic),
	}); err != nil {
		t.Fatalf("recording the bed's usage: %v", err)
	}
	return version.ID
}

// TestAnImportedBedReachesTheMix is the acceptance walk for FR-080's 背景音乐导入.
//
// It asserts at every boundary the value crosses: the repository's read finds the version, the timeline
// carries it with the music role, and the request the export hands the engine places it at the top with
// the bed's gain.
func TestAnImportedBedReachesTheMix(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	boardVersionID := harness.approvedBoard(t, 2, 4)

	// The bed is attached to the SECOND shot, and the placement must not depend on that: a bed runs from
	// the top, so the assertion below is about the film's beginning rather than about the shot's.
	bedVersion := importBedForWalk(t, harness, ctx, "wp11-shot-2", "walk-bed.wav")
	if bedVersion == "" {
		t.Fatal("the import produced no version")
	}

	// --- 1. THE REPOSITORY'S READ FINDS IT. This is the join that decides whether the feature exists.
	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	found := false
	for _, row := range rows {
		for _, clip := range row.AudioClips {
			if clip.VersionID != bedVersion {
				continue
			}
			found = true
			if clip.Role != appmedia.AudioRoleMusic {
				t.Fatalf("the read labelled the bed %q", clip.Role)
			}
		}
	}
	if !found {
		t.Fatal("an imported bed is a row NOTHING READS: the usage did not reach the mix's join")
	}

	// --- 2. THE TIMELINE CARRIES IT.
	timeline, err := harness.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatalf("reading the timeline: %v", err)
	}
	carried := false
	for _, shot := range timeline.Shots {
		for _, clip := range shot.AudioClips {
			if clip.VersionID == bedVersion {
				carried = true
			}
		}
	}
	if !carried {
		t.Fatal("the timeline lost the imported bed")
	}

	// --- 3. THE REQUEST PLACES IT AT THE TOP, WITH THE BED'S GAIN.
	engine := &recordingEngine{}
	harness.service = appmedia.NewExportService(appmedia.ExportOptions{
		Timeline: *harness.timeline, Engine: engine, Files: harness.files, Exports: harness.exports,
		Audio: NewAssetRepository(harness.db), Temp: scratchTemp{root: t.TempDir()},
		Subtitle: harness.subtitle, Clock: mediaClock{at: harness.now}, IDs: harness,
	})
	if _, _, err := harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID: "drama-episode", Quality: domainmedia.QualityPreview, FPS: 15, CreatedByType: "user",
	}); err != nil {
		t.Fatalf("the export: %v", err)
	}
	if len(engine.composed) != 1 {
		t.Fatalf("the engine was asked to compose %d times", len(engine.composed))
	}
	clips := engine.composed[0].AudioMix.Clips
	if len(clips) != 1 {
		t.Fatalf("%d clips in the request: %+v", len(clips), clips)
	}
	if clips[0].Role != appmedia.AudioRoleMusic {
		t.Fatalf("the clip reaches the engine as a %q", clips[0].Role)
	}
	// THE START IS THE FILM'S, not the shot's: a bed hung off the second shot must not begin at 4000ms.
	if clips[0].StartMS != 0 {
		t.Fatalf("the imported bed begins at %dms; a bed runs from the top", clips[0].StartMS)
	}
	// And the gain is the bed's documented default, which is what makes it a bed rather than a second
	// dialogue track.
	if clips[0].Gain != 0.35 {
		t.Fatalf("the bed mixes at %v rather than 0.35", clips[0].Gain)
	}
}

// TestABedWithoutAnApprovalIsInvisibleToTheMix is the order the import's steps exist for.
//
// A usage pointing at a version the asset has not approved is a row the join skips, because the join
// requires `au.asset_version_id = aa.current_approved_version_id`. This asserts that from the other
// direction: it is what the binding's approval step prevents, and a test that only checked the happy
// path could not tell the two apart.
func TestABedWithoutAnApprovalIsInvisibleToTheMix(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	boardVersionID := harness.approvedBoard(t, 1, 4)
	// The repository carries the files, so the service needs no second port for them.
	assets := appassets.NewService(appassets.Options{
		Repository: NewAssetRepository(harness.db),
		Clock:      mediaClock{at: harness.now},
		IDs:        harness,
	})
	key := harness.put(t, "unapproved-bed.wav", tone(t, ctx))
	_, version, err := assets.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "drama-project", Type: asset.TypeAudio, Name: "unapproved",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assets.AttachFile(ctx, version.ID, key, asset.RolePrimary); err != nil {
		t.Fatal(err)
	}
	// The usage is recorded and the approval is NOT.
	if _, err := assets.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: version.ID, ConsumerType: asset.ConsumerShot, ConsumerID: "wp11-shot-1",
		UsageRole: appmedia.UsageRoleForAudio(appmedia.AudioRoleMusic),
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	for _, row := range rows {
		if len(row.AudioClips) != 0 {
			t.Fatalf("an unapproved bed reached the mix: %+v", row.AudioClips)
		}
	}
}
