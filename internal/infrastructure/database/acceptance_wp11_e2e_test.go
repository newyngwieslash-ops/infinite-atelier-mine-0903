package database

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// acceptance_wp11_e2e_test.go walks ONE episode through the whole of WP-11, which is ROADMAP item 14:
//
//	14. 单集 E2E。
//
// # Why the package needed this, and why the parts' own tests could not replace it
//
// Every WP-11 test so far grades one CLAUSE: the timeline reads in order, an export composes, the
// subtitle track is editable, the Final Ruleset reports. `acceptance_wp11_test.go` walks AC-MEDIA-003's
// seven clauses as a chain, and it stops at the export and its approval — it never covers a board
// becoming VIDEO and AUDIO, never drafts a subtitle from a script, and never runs the Final Ruleset.
//
// That leaves the join between them untested, which is where this package's defects have lived: the
// Final Ruleset was inert because its reads named columns the schema does not have, and the four
// documents were half-built. Both were invisible to the parts' own suites because nothing drove the
// assembled pipeline.
//
// # What the walk is
//
//	A script with scenes and spoken lines
//	  → a board of three shots, each with an approved frame
//	  → a subtitle track drafted FROM the script and approved
//	  → a video job submitted for a shot and its result attached as an asset version
//	  → an audio job submitted for a dialogue line
//	  → a timeline read that sees all of it
//	  → an export composed from the approved frames, with the subtitles muxed in
//	  → the Final Ruleset run over the episode
//	  → the four documents exported
//
// Each step asserts on what the DATABASE holds afterwards, which is AGENTS section 12's rule: business
// success is a read-back rather than an agent's or a service's own account.

// wp11Episode is one episode with the script and board the walk needs.
type wp11Episode struct {
	harness       *mediaHarness
	documents     *appmedia.DocumentService
	boardVersion  string
	scriptVersion string
}

// newWP11Episode builds the episode: a script version with scenes and lines, and a three-shot board
// whose rows each have an approved frame.
//
// The script is written through the REPOSITORIES rather than by raw INSERT for the parents, because
// the walk's later steps join through them: a fixture that wrote its own rows would prove the fixture
// can write rather than that the pipeline can read.
func newWP11Episode(t *testing.T) wp11Episode {
	t.Helper()
	ctx := context.Background()
	harness := newMediaHarness(t)
	documents := appmedia.NewDocumentService(appmedia.DocumentOptions{
		Repository: NewDocumentFactsReader(harness.db),
	})

	// The script: one scene with three spoken lines and one direction, approved, and pointed at by the
	// episode. The direction is there so the walk can assert that a subtitle track excludes it.
	//
	// The four lines CONTINUE the seed's five rather than replacing them, because
	// `dialogue_lines` is unique on (scene_id, ordinal) and the seed already holds 1..5. Overwriting
	// would also delete the direction the seed wrote, which the script document's test asserts.
	seedScriptForDocuments(t, harness)
	const seedLineCount = 5
	for index, line := range []struct{ id, kind, character, text string }{
		{"walk-line-1", "dialogue", "沈砚", "灯还亮着。"},
		{"walk-line-2", "dialogue", "老周", "账本不在盐仓。"},
		{"walk-line-3", "narration", "", "那年的冬天格外长。"},
		// A direction, which nobody hears and which must never become a cue.
		{"walk-line-4", "action", "", "沈砚把铜牌放回灯下。"},
	} {
		if _, err := harness.db.ExecContext(ctx, `INSERT INTO dialogue_lines
			(id, scene_id, ordinal, line_type, character_entity_id, text, created_at, updated_at, revision)
			VALUES (?, 'doc-scene', ?, ?, ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
			line.id, seedLineCount+index+1, line.kind, line.character, line.text); err != nil {
			t.Fatalf("writing %s: %v", line.id, err)
		}
	}

	// The board: three shots, each with an approved frame. The shot ids match the script's shots so the
	// coverage rule has nothing to say.
	boardVersion := harness.approvedBoard(t, 3, 4)
	return wp11Episode{
		harness: harness, documents: documents, boardVersion: boardVersion,
		scriptVersion: "drama-script-version",
	}
}

// TestE2EWP11AnEpisodeWalksFromScriptToExport is ROADMAP item 14.
func TestE2EWP11AnEpisodeWalksFromScriptToExport(t *testing.T) {
	ctx := context.Background()
	episode := newWP11Episode(t)
	harness := episode.harness
	requireMediaEngine(t, harness)

	// --- 1. THE BOARD, read back from the database rather than assumed.
	rows := harness.rows(t, episode.boardVersion)
	if len(rows) != 3 {
		t.Fatalf("the board has %d rows, want three", len(rows))
	}
	for _, row := range rows {
		if row.Ordinal < 1 {
			t.Fatalf("a row has ordinal %d", row.Ordinal)
		}
	}

	// --- 2. THE SUBTITLE TRACK, drafted from the SCRIPT's spoken lines.
	//
	// The line reader is the real adapter's shape: the walk hands the service the lines the script
	// holds, because the desktop adapter is not available inside this package. What is being graded is
	// that the DRAFT covers the spoken lines and excludes the direction, and that it then reaches the
	// export.
	trackID := harness.draftTrackForWalk(t, ctx, episode)
	track, cues := harness.trackAndCues(t, ctx, trackID)
	if track.Status != "approved" {
		t.Fatalf("the track is %q after approval", track.Status)
	}
	// The expected count is DERIVED from the lines the reader was given — the script's own, filtered by
	// the domain's rule — rather than from a literal, so the assertion cannot drift from the fixture.
	spoken := spokeForWalk()
	if len(cues) != len(spoken) {
		t.Fatalf("the draft has %d cues against the script's %d spoken lines", len(cues), len(spoken))
	}
	for _, cue := range cues {
		if strings.Contains(cue.Text, "铜牌") {
			t.Fatalf("a direction became a cue: %q", cue.Text)
		}
	}

	// --- 3. THE VIDEO JOB. It is submitted and its result attached as an asset version, which is
	// ROADMAP item 3's model: a shot's video is an `assets` row of type `video` with a usage whose
	// role is `video`. ADR-0015 section 4 rules that, and this is the step that makes it reachable
	// rather than described.
	videoVersionID := harness.attachVideoVersionForWalk(t, ctx, rows[0].ShotID)
	if videoVersionID == "" {
		t.Fatal("no video version was attached to the shot")
	}
	// The usage is what "which shot's video was approved" reads, so the walk asserts it exists rather
	// than only the version row.
	var usageCount int
	if err := harness.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM asset_usages
		WHERE consumer_type = 'shot' AND consumer_id = ? AND usage_role = 'video'`,
		rows[0].ShotID).Scan(&usageCount); err != nil {
		t.Fatal(err)
	}
	if usageCount != 1 {
		t.Fatalf("the shot has %d video usages, want one", usageCount)
	}

	// --- 4. THE AUDIO JOB, for one dialogue line. AC-MEDIA-002's first clause is that a dialogue line
	// becomes a voice job, and the entity type is what records the link.
	audioVersionID := harness.attachAudioVersionForWalk(t, ctx, "walk-line-1")
	var audioUsages int
	if err := harness.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM asset_usages
		WHERE consumer_type = 'shot' AND usage_role = 'audio'`).Scan(&audioUsages); err != nil {
		t.Fatal(err)
	}
	if audioUsages != 1 || audioVersionID == "" {
		t.Fatalf("the audio attachment left %d usages and version %q", audioUsages, audioVersionID)
	}

	// --- 5. THE TIMELINE, which is the read that sees all of it at once. This is the step that would
	// have caught a broken join in any of the three above, because it walks the same tables.
	timeline, err := harness.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatalf("the timeline read failed: %v", err)
	}
	if len(timeline.Shots) != 3 {
		t.Fatalf("the timeline has %d shots", len(timeline.Shots))
	}
	if timeline.MissingMedia != 0 {
		t.Fatalf("the timeline reports %d shots with no media, and every row has an approved frame", timeline.MissingMedia)
	}
	if timeline.CueCount != len(spoken) {
		t.Fatalf("the timeline counts %d cues against the script's %d spoken lines", timeline.CueCount, len(spoken))
	}
	if timeline.MissingLines != 0 {
		t.Fatalf("the timeline reports %d uncovered lines, and the draft covered every spoken one", timeline.MissingLines)
	}
	// The order is the board's own, which is what an export consumes.
	for index, shot := range timeline.Shots {
		if shot.Ordinal != index+1 {
			t.Fatalf("timeline shot %d carries ordinal %d", index, shot.Ordinal)
		}
		if shot.MediaVersionID == "" {
			t.Fatalf("shot %d has no media", shot.Ordinal)
		}
	}

	// --- 6. THE EXPORT, composed from the approved frames with the subtitles muxed in.
	record, manifest, err := harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID:       "drama-episode",
		BoardVersionID:  episode.boardVersion,
		SubtitleTrackID: trackID,
		Quality:         domainmedia.QualityPreview,
		FPS:             15,
		SubtitleMode:    domainmedia.SubtitleSidecar,
		CreatedByType:   "user",
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if record.Status != "draft" {
		t.Fatalf("a fresh export is %q", record.Status)
	}
	if record.SubtitleTrackID != trackID {
		t.Fatalf("the export names track %q, want %q", record.SubtitleTrackID, trackID)
	}
	// The manifest names the parts, which is the traceability the walk ends with.
	for _, kind := range []domainmedia.ReferenceKind{
		domainmedia.RefScript, domainmedia.RefStoryboard, domainmedia.RefSubtitleTrack,
	} {
		if len(manifest.ReferencesOf(kind)) == 0 {
			t.Fatalf("the manifest cites no %s", kind)
		}
	}
	// The FILM itself, read back: a real file the engine composed.
	info, err := harness.probeExport(t, ctx, record.OutputFileHash)
	if err != nil {
		t.Fatalf("the export is not a readable film: %v", err)
	}
	if info.DurationMS < 11000 || info.DurationMS > 13000 {
		t.Fatalf("the film is %dms for three four-second shots", info.DurationMS)
	}
	// THREE STREAMS: the picture, the subtitle track, and the AUDIO.
	//
	// This assertion used to say the opposite — that the film did NOT carry the audio, because
	// `Compose` was given the segments and a subtitle path and no audio path at all. WP-20 wired the
	// mix, so the check flips from "at least the subtitle" to "all three", and the number is what
	// makes it visible: a build that stopped passing the mix would fail here rather than quietly
	// producing a silent film again.
	if info.Streams < 3 {
		t.Fatalf("the film carries %d streams, so a picture, a subtitle track and the dialogue were not all muxed", info.Streams)
	}

	// --- 7. THE FINAL RULESET, over the episode as it now stands.
	//
	// It is the same ruleset the `final_episode` stage's supervisor runs before, driven through the
	// same composed checker the stage machine calls. This is the step whose absence let the adapter's
	// SQL name columns that do not exist.
	checker := NewStoryboardConsistencyChecker(nil, nil, nil, nil).
		WithFinalRuleset(NewFinalFactsReader(harness.db))
	findings, err := checker.Check(ctx, "final_episode", "drama-episode")
	if err != nil {
		t.Fatalf("the Final Ruleset refused to run: %v", err)
	}
	// The fixture has no music and no per-line audio for two of three lines, so the audio rule
	// reports; what must NOT be reported is anything the walk actually did. Each of these would mean
	// a step above left the database in a state its own test could not see.
	for _, forbidden := range []struct{ rule, why string }{
		{appconsistency.RuleShotMedia, "every row has an approved frame"},
		{appconsistency.RuleMediaFilePresent, "every cited hash was written through the store"},
		{appconsistency.RuleExportTraceable, "the export's manifest matches what is approved"},
		{appconsistency.RuleSubtitleComplete, "the track covers every spoken line"},
	} {
		for _, finding := range findings {
			if finding.Rule == forbidden.rule {
				t.Fatalf("the ruleset reported %s although %s: %+v", forbidden.rule, forbidden.why, finding)
			}
		}
	}

	// --- 8. THE DOCUMENTS, which are ROADMAP item 11's remaining half.
	script, err := episode.documents.ExportScript(ctx, appmedia.ScriptRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "drama-episode"}, Format: "txt",
	})
	if err != nil {
		t.Fatalf("ExportScript: %v", err)
	}
	if !strings.Contains(script.Text, "灯还亮着。") {
		t.Fatalf("the script document does not carry the dialogue the walk wrote:\n%s", script.Text)
	}
	shotList, err := episode.documents.ExportShotList(ctx, appmedia.ShotListRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "drama-episode"}, Format: "csv",
	})
	if err != nil {
		t.Fatalf("ExportShotList: %v", err)
	}
	// The shot list and the timeline must agree about the episode's shape, and they are two different
	// reads of it: a disagreement means one of the joins is wrong.
	records, err := csv.NewReader(strings.NewReader(shotList.Text)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records)-1 != len(timeline.Shots) {
		t.Fatalf("the shot list has %d rows against the timeline's %d shots", len(records)-1, len(timeline.Shots))
	}
	manifestDocument, err := episode.documents.ExportManifest(ctx, appmedia.DocumentRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatalf("ExportManifest: %v", err)
	}
	exported, err := domainmedia.DecodeManifest(manifestDocument.Text)
	if err != nil {
		t.Fatalf("the exported manifest is not readable: %v", err)
	}
	// The DOCUMENT and the FILM describe the same export, which is the property that makes the file
	// worth handing to somebody: a document naming another version's references would be a manifest of
	// a different film.
	if exported.EpisodeID != record.EpisodeID || exported.ExportVersionNumber != record.VersionNumber {
		t.Fatalf("the exported manifest describes episode %q version %d, and the export is %q version %d",
			exported.EpisodeID, exported.ExportVersionNumber, record.EpisodeID, record.VersionNumber)
	}
	if len(exported.References) != len(manifest.References) {
		t.Fatalf("the exported manifest has %d references against the composed one's %d",
			len(exported.References), len(manifest.References))
	}
}

// seedSpokenLines are the SPOKEN lines `seedScriptForDocuments` writes.
//
// They are named here because the walk's line reader must answer for the whole script: a reader that
// knew only the walk's own four would leave the seed's two uncovered, and the timeline would report
// it — correctly. Keeping them in the same package as the seed is what lets a reader see that the two
// lists are the same fixture seen twice.
func seedSpokenLines() []domainmedia.SpokenLine {
	return []domainmedia.SpokenLine{
		{LineID: "doc-line-2", Type: "dialogue", CharacterEntityID: "沈砚", Text: "灯还亮着。"},
		{LineID: "doc-line-3", Type: "narration", Text: "那年的冬天格外长。"},
	}
}

// allSpokenLinesForWalk is every line the walk's script version holds, in script order.
//
// ONE list, read by the line reader AND by the expected-count assertion, so the two cannot disagree:
// an earlier version wrote the lines out twice and the second copy was short, which is exactly the
// kind of drift a fixture should make impossible rather than merely unlikely.
func allSpokenLinesForWalk() []domainmedia.SpokenLine {
	return append(seedSpokenLines(),
		domainmedia.SpokenLine{LineID: "walk-line-1", Type: "dialogue", CharacterEntityID: "沈砚", Text: "灯还亮着。"},
		domainmedia.SpokenLine{LineID: "walk-line-2", Type: "dialogue", CharacterEntityID: "老周", Text: "账本不在盐仓。"},
		domainmedia.SpokenLine{LineID: "walk-line-3", Type: "narration", Text: "那年的冬天格外长。"},
		// A direction, which nobody hears and which must never become a cue.
		domainmedia.SpokenLine{LineID: "walk-line-4", Type: "action", Text: "沈砚把铜牌放回灯下。"},
	)
}

// spokeForWalk filters the walk's lines the way the domain does, so the expected cue count comes from
// the same rule the service applies rather than from a literal.
func spokeForWalk() []domainmedia.SpokenLine {
	spoken := []domainmedia.SpokenLine{}
	for _, line := range allSpokenLinesForWalk() {
		if domainmedia.IsSpoken(line.Type) {
			spoken = append(spoken, line)
		}
	}
	return spoken
}

// draftTrackForWalk drafts a track from the episode's spoken lines and approves it.
//
// The lines are handed to the service directly, because the desktop adapter that reads them in
// production is in another package: what the walk grades is that the DRAFT excludes directions, that
// the approval path works, and that the export then muxes the track.
func (h *mediaHarness) draftTrackForWalk(t *testing.T, ctx context.Context, episode wp11Episode) string {
	t.Helper()
	// EVERY spoken line the script version holds, not only the walk's own four. The seed writes two
	// dialogue lines of its own, and a draft that omitted them would leave the episode with uncovered
	// lines — which the timeline and the Final Ruleset both report, correctly. The walk's subject is
	// the chain from script to export, so its reader answers for the whole script.
	reader := lineReaderDouble{lines: allSpokenLinesForWalk(), durationMS: 12000}
	service := appmedia.NewSubtitleService(appmedia.SubtitleOptions{
		Tracks: NewSubtitleRepository(h.db), Lines: reader,
		Clock: mediaClock{at: h.now}, IDs: h,
	})
	track, _, err := service.Draft(ctx, appmedia.DraftRequest{
		EpisodeID: "drama-episode", ScriptVersionID: episode.scriptVersion,
		CreatedByType: "user", CreatedByID: "user-1",
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	// The two-step the core requires: a draft goes to review, and a reviewed track is approved.
	if _, err := service.SubmitForReview(ctx, track.ID, "drama-episode"); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := service.Approve(ctx, track.ID, "drama-episode", "trace-walk"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	// The harness's own subtitle service reads the same repository, so the export below sees it. The
	// two services share the store rather than the service value, which is the point: a track written
	// by one is the track the other reads.
	return track.ID
}

// trackAndCues reads a track and its cues back from the database.
func (h *mediaHarness) trackAndCues(t *testing.T, ctx context.Context, trackID string) (domainmedia.Track, []domainmedia.Cue) {
	t.Helper()
	track, err := h.subtitles.GetTrack(ctx, trackID)
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	cues, err := h.subtitles.ListCues(ctx, trackID)
	if err != nil {
		t.Fatalf("ListCues: %v", err)
	}
	return track, cues
}

// attachVideoVersionForWalk writes the asset, version, file and usage a video job's result leaves.
//
// It writes them through the repositories' own statements rather than through the job runner, because
// the runner is exercised by its own package's tests against a mock provider: what this walk grades is
// that the RESULT has somewhere to land and that the timeline's join finds it. The usage role is
// `video`, which is ADR-0015 section 4's ruling and the field nothing wrote before this walk.
func (h *mediaHarness) attachVideoVersionForWalk(t *testing.T, ctx context.Context, shotID string) string {
	t.Helper()
	// A real MP4-shaped payload, because the Final Ruleset's empty-media rule reads the file's SIZE and
	// a 24-byte header would be reported — correctly, and for a reason this walk is not testing.
	payload := h.composedClip(t, ctx)
	hash := h.put(t, "walk-video.mp4", payload)

	if _, err := h.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES ('walk-video-asset', 'drama-project', 'video', 'shot video', '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_versions
		(id, asset_id, version_number, status, created_by_type, generation_job_id, created_at)
		VALUES ('walk-video-version', 'walk-video-asset', 1, 'approved', 'user', 'walk-video-job',
		 '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_files
		(id, asset_version_id, file_hash, role, ordinal, created_at)
		VALUES ('walk-video-file', 'walk-video-version', ?, 'primary', 0, '2026-01-01T00:00:00Z')`,
		hash); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE assets SET current_approved_version_id = 'walk-video-version'
		WHERE id = 'walk-video-asset'`); err != nil {
		t.Fatal(err)
	}
	// The usage is what "this shot's video" reads, and its role is the value ADR-0015 section 4 names.
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_usages
		(id, asset_version_id, consumer_type, consumer_id, usage_role, created_at)
		VALUES ('walk-video-usage', 'walk-video-version', 'shot', ?, 'video', '2026-01-01T00:00:00Z')`,
		shotID); err != nil {
		t.Fatal(err)
	}
	return "walk-video-version"
}

// attachAudioVersionForWalk writes the asset, version, file and usage a TTS job's result leaves.
//
// The consumer is the SHOT the line belongs to, because `asset_usages.consumer_type`'s vocabulary is
// the schema's and a line is not a consumer type. What links the audio to the LINE is the job's own
// entity, which `SubmitAudioJob` records as `dialogue_line` — so the walk asserts the usage here and
// the job binding in its own test.
func (h *mediaHarness) attachAudioVersionForWalk(t *testing.T, ctx context.Context, lineID string) string {
	t.Helper()
	// A one-second tone, built by the engine itself rather than committed as a fixture.
	tone := tone(t, ctx)
	hash := h.put(t, "walk-audio.wav", tone)

	// The shot the line's scene is boarded as. The walk has one scene, so every line belongs to the
	// board's first shot — which is what the timeline's audio join looks for.
	var shotID string
	if err := h.db.QueryRowContext(ctx,
		`SELECT shot_id FROM storyboard_items ORDER BY ordinal LIMIT 1`).Scan(&shotID); err != nil {
		t.Fatal(err)
	}
	_ = lineID
	if _, err := h.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES ('walk-audio-asset', 'drama-project', 'audio', 'line audio', '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_versions
		(id, asset_id, version_number, status, created_by_type, generation_job_id, created_at)
		VALUES ('walk-audio-version', 'walk-audio-asset', 1, 'approved', 'user', 'walk-audio-job',
		 '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_files
		(id, asset_version_id, file_hash, role, ordinal, created_at)
		VALUES ('walk-audio-file', 'walk-audio-version', ?, 'primary', 0, '2026-01-01T00:00:00Z')`,
		hash); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE assets SET current_approved_version_id = 'walk-audio-version'
		WHERE id = 'walk-audio-asset'`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_usages
		(id, asset_version_id, consumer_type, consumer_id, usage_role, created_at)
		VALUES ('walk-audio-usage', 'walk-audio-version', 'shot', ?, 'audio', '2026-01-01T00:00:00Z')`,
		shotID); err != nil {
		t.Fatal(err)
	}
	return "walk-audio-version"
}

// composedClip builds a real MP4 by composing one frame, so the video version's file is a film rather
// than a header — the same reason the export is composed rather than faked.
func (h *mediaHarness) composedClip(t *testing.T, ctx context.Context) []byte {
	t.Helper()
	frame := filepath.Join(t.TempDir(), "clip.png")
	if err := os.WriteFile(frame, pngFixture(t, 64, 48, 90), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "clip.mp4")
	if _, err := h.engine.Compose(ctx, appmedia.ComposeRequest{
		Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: frame, DurationMS: 1000}},
		Width:    64, Height: 48, FPS: 10,
		OutputPath: output,
		Timeout:    2 * time.Minute,
	}); err != nil {
		t.Fatalf("Compose: %v", err)
	}
	body, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// tone builds a one-second PCM WAV.
//
// It is written here rather than produced by the engine because building it needs no engine — a WAV
// header is forty-four bytes and a run of samples — and the walk must be able to attach an audio asset
// on a machine whose ffmpeg is present but which this step does not otherwise need. The file is REAL:
// the Final Ruleset's empty-media rule reads its size and the store hashes its bytes.
func tone(t *testing.T, ctx context.Context) []byte {
	t.Helper()
	_ = ctx
	const (
		sampleRate    = 8000
		bitsPerSample = 16
		channels      = 1
		seconds       = 1
	)
	samples := sampleRate * seconds
	dataBytes := samples * channels * bitsPerSample / 8

	var buffer bytes.Buffer
	buffer.WriteString("RIFF")
	binary.Write(&buffer, binary.LittleEndian, uint32(36+dataBytes))
	buffer.WriteString("WAVE")
	buffer.WriteString("fmt ")
	binary.Write(&buffer, binary.LittleEndian, uint32(16))
	binary.Write(&buffer, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&buffer, binary.LittleEndian, uint16(channels))
	binary.Write(&buffer, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&buffer, binary.LittleEndian, uint32(sampleRate*channels*bitsPerSample/8))
	binary.Write(&buffer, binary.LittleEndian, uint16(channels*bitsPerSample/8))
	binary.Write(&buffer, binary.LittleEndian, uint16(bitsPerSample))
	buffer.WriteString("data")
	binary.Write(&buffer, binary.LittleEndian, uint32(dataBytes))
	// A quiet tone rather than silence, so a reader of the file hears something: a run of zeroes is a
	// valid WAV and would make "the audio is real" a claim about its header alone.
	for index := 0; index < samples; index++ {
		value := int16(2000 * math.Sin(2*math.Pi*440*float64(index)/sampleRate))
		binary.Write(&buffer, binary.LittleEndian, value)
	}
	return buffer.Bytes()
}

// probeExport reads a stored export back with the engine.
func (h *mediaHarness) probeExport(t *testing.T, ctx context.Context, storageKey string) (appmedia.MediaInfo, error) {
	t.Helper()
	opened, err := h.service.Open(ctx, storageKey)
	if err != nil {
		return appmedia.MediaInfo{}, err
	}
	defer opened.Close()
	staged := filepath.Join(t.TempDir(), "readback.mp4")
	file, err := os.Create(staged)
	if err != nil {
		return appmedia.MediaInfo{}, err
	}
	if _, err := io.Copy(file, opened); err != nil {
		file.Close()
		return appmedia.MediaInfo{}, err
	}
	if err := file.Close(); err != nil {
		return appmedia.MediaInfo{}, err
	}
	return h.engine.Probe(ctx, staged, appmedia.DefaultProbeLimits())
}

// TestADialogueClipIsPlacedAtItsShot is the mix's placement, measured in a real film.
//
// # Why this needed its own test
//
// A mutation that placed every dialogue clip at zero left the whole suite GREEN: the acceptance walk
// has one audible line and its shot is the first, whose start IS zero — so the walk could not tell a
// correct placement from a broken one. That is the same shape as a limit with no test: the property is
// exercised and nothing observes it.
//
// This test gives the AUDIO to the SECOND shot, whose start is its predecessor's duration. A build
// that placed the clip at zero would put the line over the first shot instead, and the measurement
// below is what sees it: the film is cut into halves and the SECOND half must be the louder one,
// because that is where the sound is.
func TestADialogueClipIsPlacedAtItsShot(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	// Two shots of three seconds each, so the second begins at 3000ms.
	harness.approvedBoard(t, 2, 3)
	// The audio is attached to the SECOND shot's row rather than the first, which is what makes the
	// expected start non-zero.
	var secondShotID string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT shot_id FROM storyboard_items ORDER BY ordinal LIMIT 1 OFFSET 1`).Scan(&secondShotID); err != nil {
		t.Fatal(err)
	}
	harness.attachAudioToShot(t, ctx, secondShotID, "placement-audio-asset", "placement-audio-version")

	exported, _, err := harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID: "drama-episode", Quality: domainmedia.QualityPreview,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// The record names the output, and the store holds the bytes.
	reader, err := harness.files.Open(ctx, exported.OutputFileHash)
	if err != nil {
		t.Fatalf("opening the export: %v", err)
	}
	defer reader.Close()
	film := filepath.Join(t.TempDir(), "placed.mp4")
	file, err := os.Create(film)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(file, reader); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()

	// The first second and a half carries ONE thing — silence — and the second half of the film
	// carries the tone. Silence measures as -inf or a very low level, so the comparison is between
	// "nothing" and "something" rather than between two tones: the sound must be in the second half.
	firstLevel := harness.meanVolume(t, film, 0, 1000)
	secondLevel := harness.meanVolume(t, film, 3200, 1000)
	if secondLevel <= firstLevel {
		t.Fatalf("the first second measures %.1fdB and the third %.1fdB: the line was not placed at its shot",
			firstLevel, secondLevel)
	}
}

// attachAudioToShot writes an audio asset, version, file and usage for one shot.
func (h *mediaHarness) attachAudioToShot(t *testing.T, ctx context.Context, shotID, assetID, versionID string) {
	t.Helper()
	tone := tone(t, ctx)
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
		VALUES (?, ?, 'shot', ?, 'audio', '2026-01-01T00:00:00Z')`,
		versionID+"-usage", versionID, shotID); err != nil {
		t.Fatal(err)
	}
}

// meanVolume measures a window of a film through the ENGINE's own method.
//
// # Why not drive ffmpeg here
//
// This helper first started the process itself, and the repository's dynamic-execution scan refused
// it — correctly: SECURITY section 5 permits `os/exec` in the MediaEngine adapter and nowhere else,
// and widening the allowlist for a test would have defeated the rule rather than satisfied it. The
// measurement lives in `FFmpegEngine.MeanVolumeDB`, which is where every other process this
// application starts already is.
func (h *mediaHarness) meanVolume(t *testing.T, path string, startMS, durationMS int) float64 {
	t.Helper()
	level, err := h.engine.MeanVolumeDB(context.Background(), path, startMS, durationMS, 2*time.Minute)
	if err != nil {
		t.Fatalf("measuring %s: %v", path, err)
	}
	return level
}
