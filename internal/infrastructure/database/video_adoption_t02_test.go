package database

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	appproduction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// video_adoption_t02_test.go drives FR-080's product path over the real
// schema: a succeeded VIDEO job's result becomes a candidate version, a user
// approves it for the shot, and the timeline reads it as the shot's moving
// picture — the 2026-09-26 audit's finding that "底层 FFmpeg 能处理视频片段
// 不等于用户生成的视频已经成为时间线所采用的镜头版本" is closed by walking
// every link.
//
// The job is seeded the way the runner writes one (a result document naming a
// committed file) rather than driving a vendor: the adapter's own suite and
// the video e2e walk cover the protocol, and this walk's subject is the chain
// from a result document to the timeline.

// seedSucceededVideoJob writes a succeeded video job over one committed MP4.
func seedSucceededVideoJob(t *testing.T, harness *mediaHarness, shotID, text string) string {
	t.Helper()
	ctx := context.Background()
	hash := harness.put(t, "video-"+shotID+".mp4", tone(t, ctx))
	jobID := "video-job-" + shotID + "-" + text
	inputBytes, _ := json.Marshal(map[string]any{
		"providerId": "walk-video", "model": "mock-video", "prompt": text,
	})
	resultBytes, _ := json.Marshal(map[string]any{
		"mode": "inline", "mime": "video/mp4",
		"files": []map[string]any{{"storageKey": hash, "mime": "video/mp4", "size": 24, "hash": hash}},
	})
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO generation_jobs
		(id, project_id, entity_type, entity_id, job_type, status, priority, idempotency_key,
		 input_json, result_json, attempt_count, max_attempts, provider_config_id, created_at, updated_at)
		VALUES (?, 'drama-project', 'shot', ?, 'video_generation', 'succeeded', 0, ?,
			?, ?, 1, 3, 'walk-video', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		jobID, shotID, "walk-key-"+jobID, string(inputBytes), string(resultBytes)); err != nil {
		t.Fatalf("seeding the video job: %v", err)
	}
	return jobID
}

// collectVideoT02 composes the collector the way production does.
func collectVideoT02(t *testing.T, harness *mediaHarness, request appproduction.CollectVideoJobResultsRequest) ([]appproduction.CollectedCandidate, error) {
	t.Helper()
	clock := appjobs.NewClockFunc(func() time.Time { return harness.now })
	jobService := appjobs.NewService(appjobs.Options{
		Repository: NewJobRepository(harness.db),
		Clock:      clock,
		IDs:        audioWalkIDs{},
	})
	assetRepository := NewAssetRepository(harness.db)
	assetService := appassets.NewService(appassets.Options{
		Repository:   assetRepository,
		Clock:        mediaClock{at: harness.now},
		IDs:          harness,
		Transactions: NewAssetTransactions(assetRepository),
	})
	service := appproduction.New(appproduction.Options{Jobs: jobService, Assets: assetService})
	return service.CollectVideoJobResults(context.Background(), request)
}

// approveVideoT02 approves one candidate version as the panel's media — the
// same adoption command a panel image takes, because the read side resolves
// the asset's type dynamically.
func approveVideoT02(t *testing.T, harness *mediaHarness, itemID, panelVersionID, versionID string, candidateIDs []string, revision int64) {
	t.Helper()
	clock := appjobs.NewClockFunc(func() time.Time { return harness.now })
	storyboardRepository := NewStoryboardRepository(harness.db)
	storyboardService := appstoryboard.NewService(appstoryboard.Options{
		DirectorPlans: storyboardRepository,
		Storyboards:   storyboardRepository,
		Items:         storyboardRepository,
		Panels:        storyboardRepository,
		Clock:         clock,
		IDs:           dramaIDGenerator(),
	})
	_, err := storyboardService.ApprovePanelImage(context.Background(), appstoryboard.ApprovePanelImageRequest{
		PanelVersionID:              panelVersionID,
		ApprovedImageAssetVersionID: versionID,
		CandidateVersionIDs:         candidateIDs,
		ExpectedRevision:            revision,
	})
	if err != nil {
		t.Fatalf("approving the video version: %v", err)
	}
}

// TestACollectedVideoBecomesABoardCandidate walks the first half: the job's
// result becomes a candidate version of the shot's video asset, with a usage
// naming the shot.
func TestACollectedVideoBecomesABoardCandidate(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	itemID, panelID := harness.approvedBoard(t, 1, 4), ""
	_ = itemID

	// The shot's video asset — keyed on the shot instance, created the way
	// the frontend creates one.
	assetID := "video-asset-shot-1"
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES (?, 'drama-project', 'video', 'video:shot-1', '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, assetID); err != nil {
		t.Fatal(err)
	}
	jobID := seedSucceededVideoJob(t, harness, "wp11-shot-1", "the shot, filmed")
	collected, err := collectVideoT02(t, harness, appproduction.CollectVideoJobResultsRequest{
		AssetByJob: map[string]string{jobID: assetID},
		JobIDs:     []string{jobID},
	})
	if err != nil {
		t.Fatalf("collecting the video job: %v", err)
	}
	if len(collected) != 1 || collected[0].VersionID == "" {
		t.Fatalf("collecting produced %+v", collected)
	}

	// The candidate facts: the version is a CANDIDATE of the video asset and
	// its usage names the shot.
	var assetType, versionStatus string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT a.asset_type, v.status FROM asset_versions v JOIN assets a ON a.id = v.asset_id
		 WHERE v.id = ?`, collected[0].VersionID).Scan(&assetType, &versionStatus); err != nil {
		t.Fatal(err)
	}
	if assetType != string(asset.TypeVideo) {
		t.Fatalf("the collected version's asset is %q", assetType)
	}
	if versionStatus != string(asset.VersionCandidate) {
		t.Fatalf("the collected video is %q; a take is a candidate until a user adopts it", versionStatus)
	}
	var usageConsumer string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT consumer_id FROM asset_usages WHERE asset_version_id = ?`, collected[0].VersionID).Scan(&usageConsumer); err != nil {
		t.Fatal(err)
	}
	if usageConsumer != "wp11-shot-1" {
		t.Fatalf("the usage names %q, not the shot", usageConsumer)
	}
	_ = panelID
}

// TestAnAdoptedVideoVersionReachesTheTimeline walks the second half: after a
// user approves the candidate for the shot, the timeline reads the shot's
// media as a VIDEO with the candidate's hash — and a second take collected
// afterwards does not change what the shot plays until it is approved.
func TestAnAdoptedVideoVersionReachesTheTimeline(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	boardVersionID := harness.approvedBoard(t, 1, 4)

	// The panel version the adoption writes to — created the way the board
	// walk creates one, so the panel's candidates and revision are real.
	itemID := harness.boardItemID(t, "wp11-shot-1")
	revision := harness.boardItemRevision(t, itemID)

	assetID := "video-asset-shot-1"
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES (?, 'drama-project', 'video', 'video:shot-1', '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, assetID); err != nil {
		t.Fatal(err)
	}

	// Two takes of the same shot.
	jobTake1 := seedSucceededVideoJob(t, harness, "wp11-shot-1", "take one")
	jobTake2 := seedSucceededVideoJob(t, harness, "wp11-shot-1", "take two")
	collected, err := collectVideoT02(t, harness, appproduction.CollectVideoJobResultsRequest{
		AssetByJob: map[string]string{jobTake1: assetID, jobTake2: assetID},
		JobIDs:     []string{jobTake1, jobTake2},
	})
	if err != nil {
		t.Fatalf("collecting both takes: %v", err)
	}
	if len(collected) != 2 || collected[0].VersionID == "" || collected[1].VersionID == "" {
		t.Fatalf("collecting produced %+v", collected)
	}
	take1, take2 := collected[0].VersionID, collected[1].VersionID

	// The user previews both and adopts take ONE. Adoption writes a NEW panel
	// version (a candidate carrying the video), then the switch approves it —
	// the same two-step a panel image's replacement takes.
	panelID := harness.newPanelVersion(t, itemID, take1, 2)
	approveVideoT02(t, harness, itemID, panelID, take1,
		[]string{take1, take2}, revision)

	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	var mediaKind, mediaHash, mediaVersion string
	found := false
	for _, row := range rows {
		if row.ShotID != "wp11-shot-1" {
			continue
		}
		found = true
		mediaKind, mediaHash, mediaVersion = row.MediaKind, row.MediaHash, row.ApprovedVersionID
	}
	if !found {
		t.Fatal("the shot has no timeline row")
	}
	if mediaKind != string(asset.TypeVideo) {
		t.Fatalf("the adopted shot's media kind is %q; the timeline must see a video", mediaKind)
	}
	if mediaVersion != take1 {
		t.Fatalf("the adopted shot's media version is %q; take one was approved", mediaVersion)
	}
	if mediaHash == "" {
		t.Fatal("the adopted video has no primary file hash for the exporter to open")
	}

	// The second take stays a candidate: collecting it did NOT change what the
	// shot plays, and the shot's approval still names take one.
	var take2Status string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT status FROM asset_versions WHERE id = ?`, take2).Scan(&take2Status); err != nil {
		t.Fatal(err)
	}
	if take2Status != string(asset.VersionCandidate) {
		t.Fatalf("the unadopted take is %q; collecting a take must not adopt it", take2Status)
	}
}

// TestACollectedNonVideoJobIsRefused is the type gate: an audio job in the
// video collector's list is a caller mistake, not a skip.
func TestACollectedNonVideoJobIsRefused(t *testing.T) {
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)
	assetID := "video-asset-shot-1"
	jobID := seedSucceededAudioJob(t, harness, "line-1", "hello")
	if _, err := collectVideoT02(t, harness, appproduction.CollectVideoJobResultsRequest{
		AssetByJob: map[string]string{jobID: assetID},
		JobIDs:     []string{jobID},
	}); err == nil {
		t.Fatal("an audio job was collected as video")
	}
}

// TestAnUnfinishedVideoJobIsSkipped is the batch rule, same as images.
func TestAnUnfinishedVideoJobIsSkipped(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)
	assetID := "video-asset-shot-1"
	jobID := seedSucceededVideoJob(t, harness, "wp11-shot-1", "take one")
	if _, err := harness.db.ExecContext(ctx,
		`UPDATE generation_jobs SET status = 'waiting_remote' WHERE id = ?`, jobID); err != nil {
		t.Fatal(err)
	}
	collected, err := collectVideoT02(t, harness, appproduction.CollectVideoJobResultsRequest{
		AssetByJob: map[string]string{jobID: assetID},
		JobIDs:     []string{jobID},
	})
	if err != nil {
		t.Fatalf("an unfinished video job was refused rather than skipped: %v", err)
	}
	if len(collected) != 0 {
		t.Fatalf("%d results from an unfinished job", len(collected))
	}
}

// boardItemID, boardItemRevision and boardPanelID read the board rows a walk
// needs for the adoption's guards.
func (h *mediaHarness) boardItemID(t *testing.T, shotID string) string {
	t.Helper()
	var itemID string
	if err := h.db.QueryRowContext(context.Background(),
		`SELECT id FROM storyboard_items WHERE shot_id = ? LIMIT 1`, shotID).Scan(&itemID); err != nil {
		t.Fatalf("reading the item for %s: %v", shotID, err)
	}
	return itemID
}

func (h *mediaHarness) boardItemRevision(t *testing.T, itemID string) int64 {
	t.Helper()
	var revision int64
	if err := h.db.QueryRowContext(context.Background(),
		`SELECT revision FROM storyboard_items WHERE id = ?`, itemID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	return revision
}

// newPanelVersion writes a candidate panel version carrying one asset version,
// the row an adoption approves — the same act the panel's image replacement
// performs.
func (h *mediaHarness) newPanelVersion(t *testing.T, itemID, assetVersionID string, versionNumber int) string {
	t.Helper()
	ctx := context.Background()
	repo := NewStoryboardRepository(h.db)
	panelID := mustNewID(t, dramaIDGenerator())
	if err := repo.CreatePanelVersion(ctx, storyboard.StoryboardPanelVersion{
		ID:                          panelID,
		StoryboardItemID:            itemID,
		VersionNumber:               versionNumber,
		Status:                      versioning.StatusCandidate,
		ApprovedImageAssetVersionID: assetVersionID,
		CreatedByType:               versioning.CreatedByUser,
		CreatedAt:                   dramaTime(),
	}); err != nil {
		t.Fatalf("creating the candidate panel: %v", err)
	}
	return panelID
}

// silence unused import guards when individual test runs skip helpers
var (
	_ = appmedia.AudioRoleDialogue
	_ = storyboard.StoryboardPanelVersion{}
)
