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
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// audio_isolation_t01_test.go is the 2026-09-26 audit's T01+T04 evidence.
//
// # T01 — one asset per LINE, not one asset per project
//
// The audio view used to look up a project-wide asset by fixed name ("Line
// speech") for every dialogue job, so line A's approved take and line B's were
// versions of the SAME asset — and B's collection superseded A's, whose usage
// rows then failed the mix's `au.asset_version_id = aa.current_approved_version_id`
// filter. Two spoken lines shared one sound. The isolation rule these tests fix
// is the caller's side: the asset a job's result belongs to is keyed on the
// line (or effect) INSTANCE, so re-recording one line is a new VERSION of that
// line's asset and no other line's approval moves.
//
// # T04 — the three writes are one transaction
//
// The collector used to AttachJobResult, then ApproveVersion, then AddUsage as
// three commits. These tests drive the atomic command over the real schema and
// the real service composition, and assert the audit's completion standard:
// every intermediate state ends complete on a retry, and no repeat skips an
// approval or a usage that has not landed yet.

// collectAudioT01 composes the collector the way production does, over the
// harness's real services.
func collectAudioT01(t *testing.T, harness *mediaHarness, request appproduction.CollectAudioJobResultsRequest) ([]appproduction.CollectedCandidate, error) {
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
	return service.CollectAudioJobResults(context.Background(), request)
}

// seedSucceededAudioJobFor is seedSucceededAudioJob with the entity id the
// caller names, so two jobs can render two different lines.
func seedSucceededAudioJobFor(t *testing.T, harness *mediaHarness, entityID, text, jobID string) {
	t.Helper()
	ctx := context.Background()
	hash := harness.put(t, jobID+".wav", tone(t, ctx))
	inputBytes, _ := json.Marshal(map[string]any{
		"providerId": "walk-speech", "model": "tts-1", "text": text, "voice": defaultWalkVoice,
	})
	resultBytes, _ := json.Marshal(map[string]any{
		"mode": "inline", "mime": "audio/wave",
		"files": []map[string]any{{"storageKey": hash, "mime": "audio/wave", "size": 44, "hash": hash}},
	})
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO generation_jobs
		(id, project_id, entity_type, entity_id, job_type, status, priority, idempotency_key,
		 input_json, result_json, attempt_count, max_attempts, provider_config_id, created_at, updated_at)
		VALUES (?, 'drama-project', 'dialogue_line', ?, 'audio_generation', 'succeeded', 0, ?,
			?, ?, 1, 3, 'walk-speech', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		jobID, entityID, "walk-key-"+jobID, string(inputBytes), string(resultBytes)); err != nil {
		t.Fatalf("seeding the audio job: %v", err)
	}
}

// createAudioAssetT01 creates one audio asset with a distinct name and returns
// its id — the frontend's per-line keying at the boundary the mix reads.
func createAudioAssetT01(t *testing.T, harness *mediaHarness, name string) string {
	t.Helper()
	ctx := context.Background()
	id := "audio-asset-" + name
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES (?, 'drama-project', 'audio', ?, '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, id, name); err != nil {
		t.Fatalf("creating the audio asset %q: %v", name, err)
	}
	return id
}

// audioClipsForShot reads the mix's audio rows for one shot through BoardFacts.
func audioClipsForShot(t *testing.T, harness *mediaHarness, shotID string) []appmedia.AudioVersionRef {
	t.Helper()
	ctx := context.Background()
	boardVersionID := ""
	for _, version := range harness.boardVersions(t, ctx) {
		boardVersionID = version
	}
	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	var clips []appmedia.AudioVersionRef
	for _, row := range rows {
		if row.ShotID != shotID {
			continue
		}
		clips = append(clips, row.AudioClips...)
	}
	return clips
}

// TestTwoLinesOnTwoShotsKeepTheirOwnSpeech is T01's headline: line A and line
// B each get their own asset, and BOTH reach the mix — the defect this package
// fixes had the second collection silencing the first.
func TestTwoLinesOnTwoShotsKeepTheirOwnSpeech(t *testing.T) {
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 2, 4)

	assetA := createAudioAssetT01(t, harness, "line-A")
	assetB := createAudioAssetT01(t, harness, "line-B")
	jobA := "audio-job-A"
	jobB := "audio-job-B"
	seedSucceededAudioJobFor(t, harness, "line-A", "hello", jobA)
	seedSucceededAudioJobFor(t, harness, "line-B", "world", jobB)

	for _, step := range []struct {
		jobID, assetID, shotID string
	}{
		{jobA, assetA, "wp11-shot-1"},
		{jobB, assetB, "wp11-shot-2"},
	} {
		collected, err := collectAudioT01(t, harness, appproduction.CollectAudioJobResultsRequest{
			AssetByJob:   map[string]string{step.jobID: step.assetID},
			JobIDs:       []string{step.jobID},
			UsageRole:    appmedia.UsageRoleAudioDialogue,
			ConsumerType: string(asset.ConsumerShot),
			ConsumerID:   step.shotID,
		})
		if err != nil {
			t.Fatalf("collecting %s: %v", step.jobID, err)
		}
		if len(collected) != 1 || collected[0].VersionID == "" {
			t.Fatalf("collecting %s produced %d results", step.jobID, len(collected))
		}
	}

	clipsA := audioClipsForShot(t, harness, "wp11-shot-1")
	clipsB := audioClipsForShot(t, harness, "wp11-shot-2")
	if len(clipsA) != 1 || len(clipsB) != 1 {
		t.Fatalf("shot 1 carries %d clips and shot 2 carries %d; two lines must each keep one", len(clipsA), len(clipsB))
	}
	if clipsA[0].VersionID == clipsB[0].VersionID {
		t.Fatal("both shots mix the SAME version; the lines share one sound")
	}
}

// TestReRecordingLineBLeavesLineAUnchanged is the "重做 B 不改变 A" rule: B's
// second take is a new version of B's asset, and A's approval, usage and mix
// row are untouched.
func TestReRecordingLineBLeavesLineAUnchanged(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 2, 4)

	assetA := createAudioAssetT01(t, harness, "line-A")
	assetB := createAudioAssetT01(t, harness, "line-B")
	jobA := "audio-job-A"
	seedSucceededAudioJobFor(t, harness, "line-A", "hello", jobA)
	if _, err := collectAudioT01(t, harness, appproduction.CollectAudioJobResultsRequest{
		AssetByJob:   map[string]string{jobA: assetA},
		JobIDs:       []string{jobA},
		UsageRole:    appmedia.UsageRoleAudioDialogue,
		ConsumerType: string(asset.ConsumerShot),
		ConsumerID:   "wp11-shot-1",
	}); err != nil {
		t.Fatalf("collecting A: %v", err)
	}

	// B's first take, then B's REDO — a second job over the same line.
	jobB1 := "audio-job-B1"
	jobB2 := "audio-job-B2"
	seedSucceededAudioJobFor(t, harness, "line-B", "world take one", jobB1)
	seedSucceededAudioJobFor(t, harness, "line-B", "world take two", jobB2)
	for _, jobID := range []string{jobB1, jobB2} {
		if _, err := collectAudioT01(t, harness, appproduction.CollectAudioJobResultsRequest{
			AssetByJob:   map[string]string{jobID: assetB},
			JobIDs:       []string{jobID},
			UsageRole:    appmedia.UsageRoleAudioDialogue,
			ConsumerType: string(asset.ConsumerShot),
			ConsumerID:   "wp11-shot-2",
		}); err != nil {
			t.Fatalf("collecting %s: %v", jobID, err)
		}
	}

	// A still mixes, with the version its own collection produced.
	clipsA := audioClipsForShot(t, harness, "wp11-shot-1")
	if len(clipsA) != 1 {
		t.Fatalf("A mixes %d clips after B's redo; it must be exactly one", len(clipsA))
	}
	var aUsageCount, bVersionCount, aVersionCount int
	var aApproved string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT current_approved_version_id FROM assets WHERE id = ?`, assetA).Scan(&aApproved); err != nil {
		t.Fatal(err)
	}
	// The count reads that matter: A's version set did not grow, B's has two.
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_usages au JOIN asset_versions v ON v.id = au.asset_version_id
		 WHERE v.asset_id = ?`, assetA).Scan(&aUsageCount); err != nil {
		t.Fatal(err)
	}
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_versions WHERE asset_id = ?`, assetB).Scan(&bVersionCount); err != nil {
		t.Fatal(err)
	}
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_versions WHERE asset_id = ?`, assetA).Scan(&aVersionCount); err != nil {
		t.Fatal(err)
	}
	if aUsageCount != 1 || bVersionCount != 2 || aVersionCount != 1 {
		t.Fatalf("A carries %d usages over %d versions and B carries %d versions; the redo moved the wrong rows", aUsageCount, aVersionCount, bVersionCount)
	}
	// A's approved version is still the one its usage names.
	var aUsageVersion string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT au.asset_version_id FROM asset_usages au
		 JOIN asset_versions v ON v.id = au.asset_version_id WHERE v.asset_id = ?`, assetA).Scan(&aUsageVersion); err != nil {
		t.Fatal(err)
	}
	if aUsageVersion != aApproved {
		t.Fatalf("A's usage names %q but its approved version is %q", aUsageVersion, aApproved)
	}
	// B mixes its SECOND take — the newest approved version of B's asset.
	var bApproved string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT current_approved_version_id FROM assets WHERE id = ?`, assetB).Scan(&bApproved); err != nil {
		t.Fatal(err)
	}
	clipsB := audioClipsForShot(t, harness, "wp11-shot-2")
	if len(clipsB) != 1 || clipsB[0].VersionID != bApproved {
		t.Fatalf("B mixes %+v; it must mix its newest approved version %q", clipsB, bApproved)
	}
}

// TestMultipleEffectsAndDialogueCoexistOnOneShot is the same-shot rule: a
// dialogue line and two effects attach to one shot as three different assets,
// and all three rows reach the mix.
func TestMultipleEffectsAndDialogueCoexistOnOneShot(t *testing.T) {
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)

	assetLine := createAudioAssetT01(t, harness, "line-A")
	assetRain := createAudioAssetT01(t, harness, "effect-rain")
	assetSteps := createAudioAssetT01(t, harness, "effect-steps")
	jobLine := "audio-job-line"
	jobRain := "audio-job-rain"
	jobSteps := "audio-job-steps"
	seedSucceededAudioJobFor(t, harness, "line-A", "hello", jobLine)
	seedSucceededAudioJobFor(t, harness, "effect-rain", "rain", jobRain)
	seedSucceededAudioJobFor(t, harness, "effect-steps", "steps", jobSteps)

	steps := []struct {
		jobID, assetID, role string
	}{
		{jobLine, assetLine, appmedia.UsageRoleAudioDialogue},
		{jobRain, assetRain, appmedia.UsageRoleAudioEffect},
		{jobSteps, assetSteps, appmedia.UsageRoleAudioEffect},
	}
	for _, step := range steps {
		if _, err := collectAudioT01(t, harness, appproduction.CollectAudioJobResultsRequest{
			AssetByJob:   map[string]string{step.jobID: step.assetID},
			JobIDs:       []string{step.jobID},
			UsageRole:    step.role,
			ConsumerType: string(asset.ConsumerShot),
			ConsumerID:   "wp11-shot-1",
		}); err != nil {
			t.Fatalf("collecting %s: %v", step.jobID, err)
		}
	}
	clips := audioClipsForShot(t, harness, "wp11-shot-1")
	if len(clips) != 3 {
		t.Fatalf("the shot mixes %d clips; a line and two effects are three", len(clips))
	}
	roles := map[appmedia.AudioVersionRef]int{}
	for _, clip := range clips {
		roles[clip]++
	}
	dialogue, effects := 0, 0
	for _, clip := range clips {
		if clip.Role == appmedia.AudioRoleDialogue {
			dialogue++
		}
		if clip.Role == appmedia.AudioRoleEffect {
			effects++
		}
	}
	if dialogue != 1 || effects != 2 {
		t.Fatalf("%d dialogue and %d effect clips; expected 1 and 2", dialogue, effects)
	}
}

// TestACrossEpisodeLineDoesNotShadowAnother walks the cross-episode rule: two
// episodes each carry a line whose asset is its own, and collecting the second
// episode's line leaves the first episode's mix intact.
func TestACrossEpisodeLineDoesNotShadowAnother(t *testing.T) {
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 2, 4)

	assetA := createAudioAssetT01(t, harness, "ep1-line")
	assetB := createAudioAssetT01(t, harness, "ep2-line")
	jobA := "audio-job-ep1"
	jobB := "audio-job-ep2"
	seedSucceededAudioJobFor(t, harness, "ep1-line", "one", jobA)
	seedSucceededAudioJobFor(t, harness, "ep2-line", "two", jobB)
	for _, step := range []struct {
		jobID, assetID, shotID string
	}{
		{jobA, assetA, "wp11-shot-1"},
		{jobB, assetB, "wp11-shot-2"},
	} {
		if _, err := collectAudioT01(t, harness, appproduction.CollectAudioJobResultsRequest{
			AssetByJob:   map[string]string{step.jobID: step.assetID},
			JobIDs:       []string{step.jobID},
			UsageRole:    appmedia.UsageRoleAudioDialogue,
			ConsumerType: string(asset.ConsumerShot),
			ConsumerID:   step.shotID,
		}); err != nil {
			t.Fatalf("collecting %s: %v", step.jobID, err)
		}
	}
	if clips := audioClipsForShot(t, harness, "wp11-shot-1"); len(clips) != 1 {
		t.Fatalf("episode one's shot mixes %d clips after episode two collected", len(clips))
	}
}

// TestAFailedCollectionRetriesToACompleteState is T04's acceptance: the atomic
// command cannot leave a halfway state, so the proof is that EVERY prefix of
// the old three-write sequence, seeded the way the old collector would have
// left it, is REPAIRED by a repeat collection rather than reported done.
func TestAFailedCollectionRetriesToACompleteState(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		write func(t *testing.T, harness *mediaHarness, assetID, versionID, jobID string)
	}{
		{
			// AttachJobResult landed; the process died before ApproveVersion.
			name: "version without approval",
			write: func(t *testing.T, harness *mediaHarness, assetID, versionID, jobID string) {
				_, err := harness.db.Exec(`INSERT INTO asset_versions
					(id, asset_id, version_number, status, prompt, generation_job_id, created_by_type, created_at)
					VALUES (?, ?, 1, 'candidate', 'hello', ?, 'system', '2026-01-01T00:00:00Z')`,
					versionID, assetID, jobID)
				if err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			// ApproveVersion landed; the process died before AddUsage.
			name: "approval without usage",
			write: func(t *testing.T, harness *mediaHarness, assetID, versionID, jobID string) {
				_, err := harness.db.Exec(`INSERT INTO asset_versions
					(id, asset_id, version_number, status, prompt, generation_job_id, created_by_type, created_at)
					VALUES (?, ?, 1, 'approved', 'hello', ?, 'system', '2026-01-01T00:00:00Z')`,
					versionID, assetID, jobID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := harness.db.Exec(`UPDATE assets SET current_approved_version_id = ? WHERE id = ?`,
					versionID, assetID); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			harness := newMediaHarness(t)
			harness.approvedBoard(t, 1, 4)
			assetID := createAudioAssetT01(t, harness, "line-A")
			jobID := "audio-job-A"
			seedSucceededAudioJobFor(t, harness, "line-A", "hello", jobID)
			versionID := "halfway-version"
			scenario.write(t, harness, assetID, versionID, jobID)

			// The repeat collection — the same call a restarted run makes.
			collected, err := collectAudioT01(t, harness, appproduction.CollectAudioJobResultsRequest{
				AssetByJob:   map[string]string{jobID: assetID},
				JobIDs:       []string{jobID},
				UsageRole:    appmedia.UsageRoleAudioDialogue,
				ConsumerType: string(asset.ConsumerShot),
				ConsumerID:   "wp11-shot-1",
			})
			if err != nil {
				t.Fatalf("the repeat collection: %v", err)
			}
			if len(collected) != 1 || !collected[0].Duplicate {
				t.Fatalf("the repeat reported %+v; a repaired collection is a duplicate", collected)
			}

			// The audit's completion standard: a complete version, approval and
			// usage — and the mix finds the file.
			ctx := context.Background()
			var approved string
			if err := harness.db.QueryRowContext(ctx,
				`SELECT current_approved_version_id FROM assets WHERE id = ?`, assetID).Scan(&approved); err != nil {
				t.Fatal(err)
			}
			if approved != versionID {
				t.Fatalf("the version is not the asset's approval (%q vs %q)", approved, versionID)
			}
			var files int
			if err := harness.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM asset_files WHERE asset_version_id = ? AND role = 'primary'`, versionID).Scan(&files); err != nil {
				t.Fatal(err)
			}
			if files == 0 {
				t.Fatal("the repaired version carries no primary file")
			}
			var usages int
			if err := harness.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM asset_usages WHERE asset_version_id = ? AND consumer_type = 'shot'`, versionID).Scan(&usages); err != nil {
				t.Fatal(err)
			}
			if usages != 1 {
				t.Fatalf("%d usages after the repair; the mix needs exactly one", usages)
			}
			if clips := audioClipsForShot(t, harness, "wp11-shot-1"); len(clips) != 1 {
				t.Fatalf("the shot mixes %d clips after the repair; the repaired row set must reach the mix", len(clips))
			}
		})
	}
}

// TestACollectionIsRefusedWithoutTransactions proves the fail-closed rule: a
// service composed without the transaction runner refuses the atomic command
// rather than falling back to the three separate writes.
func TestACollectionIsRefusedWithoutTransactions(t *testing.T) {
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 1, 4)
	assetID := createAudioAssetT01(t, harness, "line-A")
	jobID := "audio-job-A"
	seedSucceededAudioJobFor(t, harness, "line-A", "hello", jobID)

	clock := appjobs.NewClockFunc(func() time.Time { return harness.now })
	jobService := appjobs.NewService(appjobs.Options{
		Repository: NewJobRepository(harness.db), Clock: clock, IDs: audioWalkIDs{},
	})
	assetService := appassets.NewService(appassets.Options{
		Repository: NewAssetRepository(harness.db), Clock: mediaClock{at: harness.now}, IDs: harness,
	})
	service := appproduction.New(appproduction.Options{Jobs: jobService, Assets: assetService})
	if _, err := service.CollectAudioJobResults(context.Background(), appproduction.CollectAudioJobResultsRequest{
		AssetByJob:   map[string]string{jobID: assetID},
		JobIDs:       []string{jobID},
		UsageRole:    appmedia.UsageRoleAudioDialogue,
		ConsumerType: string(asset.ConsumerShot),
		ConsumerID:   "wp11-shot-1",
	}); err == nil {
		t.Fatal("a collection without a transaction runner was accepted; the halfway states it prevents are not guarded")
	}
}
