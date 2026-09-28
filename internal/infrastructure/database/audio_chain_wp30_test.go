package database

import (
	"context"
	"testing"

	"encoding/json"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	appproduction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// audio_chain_wp30_test.go drives the chain that had a hole in the middle: a job's result becoming the
// asset version a mix reads.
//
// # The hole, and why a walk is the only thing that finds it
//
// A TTS job's bytes were committed, the job was marked succeeded, and that was the end. Nothing turned
// the result into an asset version, so **the speech a user generated never reached the mix** — the read
// joins `asset_usages` on a shot and needs the version to be the asset's current approved one. Every test
// passed: the adapter's suite protects the protocol, the collector's protects its own loop, and WP-11's
// walk wrote the rows BY HAND under a comment claiming they were "a TTS job's result leaves".
//
// So this file drives the whole chain over the real schema and asserts at the last boundary, which is
// where a hole shows up: **does the audio a job produced arrive in the mix?**

// TestAGeneratedLineReachesTheMixThroughTheCollector is the acceptance walk for the chain.
func TestAGeneratedLineReachesTheMixThroughTheCollector(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)

	// --- 1. THE JOB. It is submitted through the real service and its result is written the way the
	// runner writes one: a result document naming the committed file's hash.
	jobID := seedSucceededAudioJob(t, harness, "walk-line-1", "line speech")
	versionID := collectAudioForWalk(t, harness, jobID, "wp11-shot-1", appmedia.UsageRoleAudioDialogue)

	// --- 2. THE MIX FINDS IT. This is the assertion the hole would have failed: before the collector,
	// no version existed for the usage to point at, and the read returned nothing.
	boardVersionID := ""
	for _, version := range harness.boardVersions(t, ctx) {
		boardVersionID = version
	}
	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	found := false
	for _, row := range rows {
		for _, clip := range row.AudioClips {
			if clip.VersionID != versionID {
				continue
			}
			found = true
			if clip.Role != appmedia.AudioRoleDialogue {
				t.Fatalf("the generated line mixes as %q", clip.Role)
			}
		}
	}
	if !found {
		t.Fatal("a generated line is a row NOTHING READS: the job's result never became the version the mix joins")
	}
}

// TestAGeneratedEffectReachesTheMixAsAnEffect is FR-080's 音效生成适配 at its last boundary.
//
// The suggestion half and the acceptance path were delivered by WP-27; what was missing was that
// accepting one produced no audio at all, because nothing collected the job's result. The role asserted
// here is what makes it an effect rather than a second dialogue track — `DefaultGainFor(AudioRoleEffect)`
// is unity, where dialogue is also unity, so the ROLE is the whole difference.
func TestAGeneratedEffectReachesTheMixAsAnEffect(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)

	jobID := seedSucceededAudioJob(t, harness, "effect-1", "rain on the roof")
	versionID := collectAudioForWalk(t, harness, jobID, "wp11-shot-1", appmedia.UsageRoleAudioEffect)

	boardVersionID := ""
	for _, version := range harness.boardVersions(t, ctx) {
		boardVersionID = version
	}
	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	var role appmedia.AudioRole
	found := false
	for _, row := range rows {
		for _, clip := range row.AudioClips {
			if clip.VersionID == versionID {
				found = true
				role = clip.Role
			}
		}
	}
	if !found {
		t.Fatal("a generated effect did not reach the mix")
	}
	if role != appmedia.AudioRoleEffect {
		t.Fatalf("the effect mixes as %q, so it is layered as a line of speech", role)
	}
}

// TestACollectedAudioVersionIsApprovedAndPrimary is what the mixer's two conditions need.
//
// The read requires the version to be the asset's CURRENT APPROVED one and the file to be `primary`. A
// collector that attached the file under another role, or left the version a candidate, would produce a
// version the mix silently skips — the same outcome as not collecting at all, with the extra confusion
// of a version row that looks right.
func TestACollectedAudioVersionIsApprovedAndPrimary(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)
	jobID := seedSucceededAudioJob(t, harness, "walk-line-1", "line speech")
	versionID := collectAudioForWalk(t, harness, jobID, "wp11-shot-1", appmedia.UsageRoleAudioDialogue)

	var fileRole string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT role FROM asset_files WHERE asset_version_id = ?`, versionID).Scan(&fileRole); err != nil {
		t.Fatalf("reading the file role: %v", err)
	}
	if fileRole != string(asset.RolePrimary) {
		t.Fatalf("the file is attached as %q, so the audio reader cannot find it", fileRole)
	}
	var approved string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT a.current_approved_version_id FROM assets a
		 JOIN asset_versions v ON v.asset_id = a.id WHERE v.id = ?`, versionID).Scan(&approved); err != nil {
		t.Fatalf("reading the approval: %v", err)
	}
	if approved != versionID {
		t.Fatalf("the collected version is not approved (%q vs %q)", approved, versionID)
	}
}

// TestCollectingTwiceDoesNotDuplicateTheVersion is the restart rule.
//
// A collection that asked about the same job twice — which a restarted run does — must report the
// existing version rather than create a second one, for the reason the image batch reports duplicates:
// a caller retrying has to be able to continue.
func TestCollectingTwiceDoesNotDuplicateTheVersion(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)
	jobID := seedSucceededAudioJob(t, harness, "walk-line-1", "line speech")
	collectAudioForWalk(t, harness, jobID, "wp11-shot-1", appmedia.UsageRoleAudioDialogue)

	collected, err := collectAudio(t, harness, appproduction.CollectAudioJobResultsRequest{
		AssetByJob:   map[string]string{jobID: "walk-audio-asset"},
		JobIDs:       []string{jobID},
		UsageRole:    appmedia.UsageRoleAudioDialogue,
		ConsumerType: string(asset.ConsumerShot),
		ConsumerID:   "wp11-shot-1",
	})
	if err != nil {
		t.Fatalf("the second collection: %v", err)
	}
	if len(collected) != 1 {
		t.Fatalf("%d results from the second collection", len(collected))
	}
	if !collected[0].Duplicate {
		t.Fatal("a replayed collection reported a NEW version rather than the existing one")
	}
	var versions int
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_versions WHERE asset_id = 'walk-audio-asset'`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Fatalf("%d versions after collecting the same job twice", versions)
	}
}

// TestACollectionWithoutAConsumerIsRefused is the data-path rule.
//
// A version with no usage is a version no read finds, which is the defect this file exists for — so the
// collector refuses rather than attaching one.
func TestACollectionWithoutAConsumerIsRefused(t *testing.T) {
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)
	jobID := seedSucceededAudioJob(t, harness, "walk-line-1", "line speech")
	for _, request := range []appproduction.CollectAudioJobResultsRequest{
		{AssetByJob: map[string]string{jobID: "walk-audio-asset"}, JobIDs: []string{jobID}},
		{AssetByJob: map[string]string{jobID: "walk-audio-asset"}, JobIDs: []string{jobID}, ConsumerType: "shot"},
		{AssetByJob: map[string]string{jobID: "walk-audio-asset"}, JobIDs: []string{jobID}, ConsumerID: "wp11-shot-1"},
	} {
		if _, err := collectAudio(t, harness, request); err == nil {
			t.Fatal("a collection naming no consumer was accepted")
		}
	}
}

// TestAnUnfinishedJobIsSkippedRatherThanRefused is the batch rule.
//
// A batch's jobs finish at different times, so a collection that refused on the first unfinished one
// could never be called while anything was still rendering.
func TestAnUnfinishedJobIsSkippedRatherThanRefused(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)
	jobID := seedSucceededAudioJob(t, harness, "walk-line-1", "line speech")
	// The job is moved back to running, which is what an unfinished one looks like.
	if _, err := harness.db.ExecContext(ctx,
		`UPDATE generation_jobs SET status = 'running' WHERE id = ?`, jobID); err != nil {
		t.Fatal(err)
	}
	collected, err := collectAudio(t, harness, appproduction.CollectAudioJobResultsRequest{
		AssetByJob:   map[string]string{jobID: "walk-audio-asset"},
		JobIDs:       []string{jobID},
		UsageRole:    appmedia.UsageRoleAudioDialogue,
		ConsumerType: string(asset.ConsumerShot),
		ConsumerID:   "wp11-shot-1",
	})
	if err != nil {
		t.Fatalf("an unfinished job was refused rather than skipped: %v", err)
	}
	if len(collected) != 0 {
		t.Fatalf("%d results from an unfinished job", len(collected))
	}
}

// collectAudio runs the collector over the harness's real services.
//
// The two halves it joins are the ones production composes: the JOB service, whose read is what tells the
// collector whether a job succeeded, and the ASSET service, which owns what a version and a usage are.
func collectAudio(t *testing.T, harness *mediaHarness, request appproduction.CollectAudioJobResultsRequest) ([]appproduction.CollectedCandidate, error) {
	t.Helper()
	clock := appjobs.NewClockFunc(func() time.Time { return harness.now })
	jobService := appjobs.NewService(appjobs.Options{
		Repository: NewJobRepository(harness.db),
		Clock:      clock,
		// The job port is `NewID(prefix)` where the asset port is `New()`; the harness implements the
		// latter, so this adapts the same generator to the former.
		IDs: audioWalkIDs{},
	})
	assetRepository := NewAssetRepository(harness.db)
	assetService := appassets.NewService(appassets.Options{
		Repository: assetRepository,
		Clock:      mediaClock{at: harness.now},
		IDs:        harness,
		// The atomic collection command's scope, composed the way production
		// composes it (`drama_wiring.go`): the same repository's adapter owns
		// the transaction the collection writes through.
		Transactions: NewAssetTransactions(assetRepository),
	})
	service := appproduction.New(appproduction.Options{Jobs: jobService, Assets: assetService})
	return service.CollectAudioJobResults(context.Background(), request)
}

// audioWalkIDs adapts the platform generator to the job service's ID port.
type audioWalkIDs struct{}

func (audioWalkIDs) NewID(prefix string) string {
	value, err := id.NewGenerator().New()
	if err != nil {
		// The generator's only failure is entropy, which a test cannot recover from.
		panic(err)
	}
	return prefix + "-" + value
}

// seedSucceededAudioJob writes a job row whose result names one committed file.
//
// It writes the row rather than running a provider, because this walk's subject is the CHAIN from a
// result document to the mix — the adapter's own suite and WP-30's jobs-side walk cover the protocol, and
// a fixture that drove a fake vendor here would make one test prove two things.
func seedSucceededAudioJob(t *testing.T, harness *mediaHarness, entityID, text string) string {
	t.Helper()
	ctx := context.Background()
	hash := harness.put(t, entityID+".wav", tone(t, ctx))
	jobID := "audio-job-" + entityID
	inputBytes, _ := json.Marshal(map[string]any{
		"providerId": "walk-speech", "model": "tts-1", "text": text, "voice": defaultWalkVoice,
	})
	input := string(inputBytes)
	resultBytes, _ := json.Marshal(map[string]any{
		"mode": "inline", "mime": "audio/wave",
		"files": []map[string]any{{"storageKey": hash, "mime": "audio/wave", "size": 44, "hash": hash}},
	})
	result := string(resultBytes)
	// The table is `generation_jobs` and the column is `attempt_count` — read from migration 000023
	// rather than guessed, which the first version of this fixture did and failed on.
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO generation_jobs
		(id, project_id, entity_type, entity_id, job_type, status, priority, idempotency_key,
		 input_json, result_json, attempt_count, max_attempts, provider_config_id, created_at, updated_at)
		VALUES (?, 'drama-project', 'dialogue_line', ?, 'audio_generation', 'succeeded', 0, ?,
			?, ?, 1, 3, 'walk-speech', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		jobID, entityID, "walk-key-"+jobID, input, result); err != nil {
		t.Fatalf("seeding the audio job: %v", err)
	}
	return jobID
}

// defaultWalkVoice is the voice the fixture's job input names, so the walk can assert provenance.
const defaultWalkVoice = "alloy"

// collectAudioForWalk runs the collector over one job and returns the version it produced.
func collectAudioForWalk(t *testing.T, harness *mediaHarness, jobID, shotID, role string) string {
	t.Helper()
	ctx := context.Background()
	// The asset the version hangs off, created the way the import creates one.
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES ('walk-audio-asset', 'drama-project', 'audio', 'generated line', '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatalf("creating the audio asset: %v", err)
	}
	collected, err := collectAudio(t, harness, appproduction.CollectAudioJobResultsRequest{
		AssetByJob:   map[string]string{jobID: "walk-audio-asset"},
		JobIDs:       []string{jobID},
		UsageRole:    role,
		ConsumerType: string(asset.ConsumerShot),
		ConsumerID:   shotID,
	})
	if err != nil {
		t.Fatalf("collecting the audio job: %v", err)
	}
	if len(collected) != 1 {
		t.Fatalf("%d results collected", len(collected))
	}
	if collected[0].VersionID == "" {
		t.Fatal("the collector produced no version")
	}
	return collected[0].VersionID
}

// boardVersions lists the storyboard versions a harness wrote, so a walk can name the approved one.
func (h *mediaHarness) boardVersions(t *testing.T, ctx context.Context) []string {
	t.Helper()
	rows, err := h.db.QueryContext(ctx, `SELECT id FROM storyboard_versions WHERE status = 'approved'`)
	if err != nil {
		t.Fatalf("reading the board versions: %v", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}
