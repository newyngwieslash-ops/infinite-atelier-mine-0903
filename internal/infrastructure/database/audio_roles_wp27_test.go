package database

import (
	"context"
	"os"
	"strings"
	"testing"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// audio_roles_wp27_test.go grades the read and the request that carry a role from an attachment into
// the mix.
//
// # The defect this closes was a rule no code could reach
//
// `DefaultGainFor(AudioRoleMusic)` returns 0.35 and `audio.go` reasons about it at length: music at
// unity under a spoken line makes the line unintelligible. `ExportService.buildMix` passed
// `AudioRoleDialogue` as a HARDCODED ARGUMENT for every clip it built, from a read that returned only
// version ids — so an imported bed would have mixed at unity and the 0.35 rule was unreachable.
//
// A test of `buildMix` alone could not catch it: the argument it got wrong is supplied by the READ. So
// these tests drive the whole pipeline over the real schema — an attachment, the repository's read,
// the timeline, and the request the export hands the engine — and assert the role at each boundary.
//
// # Why a recording engine rather than a real ffmpeg run
//
// The roles are decided before ffmpeg is asked anything, and the existing ffmpeg suites already prove
// that a role reaches the filtergraph (`internal/infrastructure/media/mix_test.go`). What was never
// asserted is that a role SURVIVES the read, which is observable in the request. Recording it keeps
// this test about the join rather than about the encoder.
//
// # Why the legacy value is a case of its own
//
// Rows written before this package carry `usage_role = 'audio'` (WP-11's own fixture writes it) and
// some carry the schema default `'reference'`. Treating either as anything but dialogue would change
// what an EXISTING project exports — data damage rather than a feature — so the compatibility rule is
// asserted rather than assumed.

// recordingEngine captures the request the export composes.
//
// IT WRITES THE OUTPUT FILE, and that is not incidental: the export streams whatever the engine
// produced into the store, so a double that named a path and wrote nothing would fail at `putFile`
// with "the export could not be read back" — a failure about the fixture rather than about the thing
// under test. The bytes are not a film and nothing decodes them: the assertions here are about the
// REQUEST, and the playable-output criterion is graded by the real ffmpeg suites.
type recordingEngine struct {
	composed []appmedia.ComposeRequest
}

func (e *recordingEngine) Available() bool                { return true }
func (e *recordingEngine) Diagnostic() string             { return "" }
func (e *recordingEngine) Version(context.Context) string { return "recording engine" }
func (e *recordingEngine) Probe(context.Context, string, appmedia.ProbeLimits) (appmedia.MediaInfo, error) {
	return appmedia.MediaInfo{DurationMS: 1000, Width: 64, Height: 48, Streams: 1}, nil
}

func (e *recordingEngine) Compose(_ context.Context, request appmedia.ComposeRequest) (appmedia.ComposeResult, error) {
	e.composed = append(e.composed, request)
	if err := os.WriteFile(request.OutputPath, []byte("recorded"), 0o600); err != nil {
		return appmedia.ComposeResult{}, err
	}
	return appmedia.ComposeResult{DurationMS: 4000, Width: 64, Height: 48}, nil
}

// attachAudioWithRole writes an audio asset, version, file and usage with a STATED usage role, so a
// test can produce the row shapes a real project has.
func (h *mediaHarness) attachAudioWithRole(t *testing.T, ctx context.Context, shotID, suffix, usageRole string) string {
	t.Helper()
	tone := tone(t, ctx)
	assetID := "role-asset-" + suffix
	versionID := "role-version-" + suffix
	hash := h.put(t, assetID+".wav", tone)
	if _, err := h.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES (?, 'drama-project', 'audio', ?, '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, assetID, assetID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_versions
		(id, asset_id, version_number, status, created_by_type, generation_job_id, created_at)
		VALUES (?, ?, 1, 'approved', 'user', ?, '2026-01-01T00:00:00Z')`,
		versionID, assetID, versionID+"-job"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_files
		(id, asset_version_id, file_hash, role, ordinal, created_at)
		VALUES (?, ?, ?, 'primary', 0, '2026-01-01T00:00:00Z')`, versionID+"-file", versionID, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE assets SET current_approved_version_id = ?
		WHERE id = ?`, versionID, assetID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_usages
		(id, asset_version_id, consumer_type, consumer_id, usage_role, created_at)
		VALUES (?, ?, 'shot', ?, ?, '2026-01-01T00:00:00Z')`,
		versionID+"-usage", versionID, shotID, usageRole); err != nil {
		t.Fatal(err)
	}
	return versionID
}

// TestTheReadCarriesEveryUsageRoleIntoTheMix is the role at the repository boundary.
func TestTheReadCarriesEveryUsageRoleIntoTheMix(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	boardVersionID := harness.approvedBoard(t, 1, 4)
	shotID := "wp11-shot-1"

	dialogue := harness.attachAudioWithRole(t, ctx, shotID, "line", appmedia.UsageRoleAudioDialogue)
	music := harness.attachAudioWithRole(t, ctx, shotID, "bed", appmedia.UsageRoleAudioMusic)
	effect := harness.attachAudioWithRole(t, ctx, shotID, "sting", appmedia.UsageRoleAudioEffect)

	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d rows", len(rows))
	}
	byVersion := map[string]appmedia.AudioRole{}
	for _, clip := range rows[0].AudioClips {
		byVersion[clip.VersionID] = clip.Role
	}
	// Every version is asserted, and the roles SEPARATELY rather than as a count: a read returning
	// three clips all called dialogue would pass a count assertion, and that is precisely the defect.
	for versionID, want := range map[string]appmedia.AudioRole{
		dialogue: appmedia.AudioRoleDialogue,
		music:    appmedia.AudioRoleMusic,
		effect:   appmedia.AudioRoleEffect,
	} {
		got, present := byVersion[versionID]
		if !present {
			t.Fatalf("the read lost the version %q", versionID)
		}
		if got != want {
			t.Fatalf("the version %q came back as %q, want %q", versionID, got, want)
		}
	}
	if !rows[0].AudioApproved {
		t.Fatal("a row with three approved clips reports no audio")
	}
	// And the timeline the export is assembled from carries them too, because THAT is the value
	// `buildMix` reads — a role that reached `BoardRow` and stopped there would still mix as dialogue.
	timeline, err := harness.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatalf("reading the timeline: %v", err)
	}
	if len(timeline.Shots) != 1 {
		t.Fatalf("%d timeline shots", len(timeline.Shots))
	}
	roles := map[appmedia.AudioRole]bool{}
	for _, clip := range timeline.Shots[0].AudioClips {
		roles[clip.Role] = true
	}
	for _, want := range []appmedia.AudioRole{appmedia.AudioRoleDialogue, appmedia.AudioRoleMusic, appmedia.AudioRoleEffect} {
		if !roles[want] {
			t.Fatalf("the timeline lost the %q role: %+v", want, timeline.Shots[0].AudioClips)
		}
	}
}

// TestTheComposedRequestOrdersBedsBeforeEffectsBeforeDialogue is the role at the export boundary.
//
// `amix` is symmetric, so the order does not change the sound — it decides whether a FAILURE's input
// number maps back to a clip a reader can find, and it makes the mix read the way channels do.
func TestTheComposedRequestOrdersBedsBeforeEffectsBeforeDialogue(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	harness.approvedBoard(t, 1, 4)
	shotID := "wp11-shot-1"

	// Attached dialogue-first on purpose: the order the rows were written in must not be the order
	// they mix in.
	harness.attachAudioWithRole(t, ctx, shotID, "line", appmedia.UsageRoleAudioDialogue)
	harness.attachAudioWithRole(t, ctx, shotID, "bed", appmedia.UsageRoleAudioMusic)
	harness.attachAudioWithRole(t, ctx, shotID, "sting", appmedia.UsageRoleAudioEffect)

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
	if len(clips) != 3 {
		t.Fatalf("%d clips in the request: %+v", len(clips), clips)
	}
	// The roles AND their gains: the gain is what the dead rule was about, so asserting the role
	// without it would leave the 0.35 unreachable in the same way it always was.
	wantOrder := []appmedia.AudioRole{appmedia.AudioRoleMusic, appmedia.AudioRoleEffect, appmedia.AudioRoleDialogue}
	for index, role := range wantOrder {
		if clips[index].Role != role {
			t.Fatalf("clip %d is %q, want %q", index, clips[index].Role, role)
		}
	}
	if clips[0].Gain != 0.35 {
		t.Fatalf("the music bed mixes at %v rather than the documented 0.35", clips[0].Gain)
	}
	// The label names the role, so a failure that said "line" for a bed cannot happen.
	for _, clip := range clips {
		if !strings.Contains(clip.Label, string(clip.Role)) {
			t.Fatalf("the clip label %q does not name its role %q", clip.Label, clip.Role)
		}
	}
}

// TestTheLegacyAudioRoleStillMixesAsDialogue is the data-compatibility rule.
//
// `'audio'` is what WP-11's walk writes and what any project exported before this package carries.
// `'reference'` is the schema's default, which a row inserted without a role gets. Both were speech —
// speech is all the mix could carry — so both must keep mixing as dialogue.
func TestTheLegacyAudioRoleStillMixesAsDialogue(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	boardVersionID := harness.approvedBoard(t, 1, 4)
	shotID := "wp11-shot-1"

	legacy := harness.attachAudioWithRole(t, ctx, shotID, "legacy", "audio")
	reference := harness.attachAudioWithRole(t, ctx, shotID, "default", "reference")

	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	roles := map[string]appmedia.AudioRole{}
	for _, clip := range rows[0].AudioClips {
		roles[clip.VersionID] = clip.Role
	}
	// Asserted against the MIXER's role rather than the usage string, because the mixer's role is what
	// the gain default is keyed on, and the point is that an old row still gets the dialogue treatment.
	for _, versionID := range []string{legacy, reference} {
		if roles[versionID] != appmedia.AudioRoleDialogue {
			t.Fatalf("the version %q with a legacy role mixes as %q", versionID, roles[versionID])
		}
	}
}

// TestTheUsageRoleRoundTripsThroughTheMapper guards the pair of functions against drift.
func TestTheUsageRoleRoundTripsThroughTheMapper(t *testing.T) {
	for _, role := range appmedia.AudioRoles {
		usage := appmedia.UsageRoleForAudio(role)
		if got := appmedia.AudioRoleForUsage(usage); got != role {
			t.Fatalf("%q mapped to %q and back to %q", role, usage, got)
		}
	}
	// The spellings a stored row might carry, stated rather than left to the default arm's silence.
	cases := map[string]appmedia.AudioRole{
		"":                appmedia.AudioRoleDialogue,
		"   ":             appmedia.AudioRoleDialogue,
		"audio":           appmedia.AudioRoleDialogue,
		"reference":       appmedia.AudioRoleDialogue,
		"audio_music":     appmedia.AudioRoleMusic,
		"AUDIO_MUSIC":     appmedia.AudioRoleMusic,
		"  audio_effect ": appmedia.AudioRoleEffect,
		"something_else":  appmedia.AudioRoleDialogue,
	}
	for usage, want := range cases {
		if got := appmedia.AudioRoleForUsage(usage); got != want {
			t.Fatalf("the stored role %q mapped to %q, want %q", usage, got, want)
		}
	}
}
