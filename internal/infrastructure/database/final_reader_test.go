package database

import (
	"context"
	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
	"testing"
)

// final_reader_test.go drives `FinalFactsReader` against the REAL schema, which is the test whose
// absence let a critical defect ship.
//
// # What went wrong
//
// The adapter prepared four SQL statements that did not compile against the twenty migrations this
// repository runs. `readApprovedBoard` selected `episode_id` from `storyboard_versions` — a column
// that table does not have, because migration 000010 puts it on `storyboards` — and because that read
// is UNCONDITIONAL it failed for every episode. `FinalFacts` therefore returned an error every time;
// the stage machine discards it (`if err == nil { deterministic = found }` in `stagepipeline`); so all
// eight of AGENT_CONTRACTS section 11.4's clauses contributed NOTHING in a real build.
//
// The ruleset's own tests were green throughout, because they drive the rules through a struct
// literal double that never touches SQL — and one of them asserted in a comment that this adapter "has
// its own tests over a real schema". It did not. A comment is not a test, and this file is the one
// that should have existed.
//
// # Why it asserts on the ERROR and then on the FACTS
//
// The first assertion is the cheap one that catches a broken statement: a read that cannot run says
// so. The rest are about what the read RETURNS, because a statement that prepares and returns nothing
// would pass the first half and still leave every rule blind.

// finalReaderHarness builds the reader over the media harness's migrated database.
func finalReaderHarness(t *testing.T) (*FinalFactsReader, *mediaHarness) {
	t.Helper()
	harness := newMediaHarness(t)
	return NewFinalFactsReader(harness.db), harness
}

// TestFinalFactsReaderReadsTheRealSchema is the statement-level assertion: every read runs.
//
// It reads an episode with a board, rows, an approved frame, a subtitle track and a stale candidate,
// so each of the adapter's statements is reached rather than short-circuited by an early "nothing to
// read". An episode with no data would exercise the happy path of `sql.ErrNoRows` and none of the
// joins.
func TestFinalFactsReaderReadsTheRealSchema(t *testing.T) {
	ctx := context.Background()
	reader, harness := finalReaderHarness(t)
	versionID := harness.approvedBoard(t, 2, 4)
	// APPROVE the script version, which is what the reader joins through.
	//
	// An earlier version of this fixture set `episodes.current_script_version_id` instead, and that
	// was the defect rather than the fix: NOTHING in this build writes that column — `UpdateEpisode`
	// carries it but its only caller preserves the value it read — so the production join found no
	// rows and this test passed against a state no build can produce. The duration rule then reported
	// nothing in production while reporting here, which is the kind of divergence a fixture that
	// hand-writes its own columns produces.
	if _, err := harness.db.ExecContext(ctx,
		`UPDATE script_versions SET status = 'approved', estimated_duration_seconds = 8
		 WHERE id = 'drama-script-version'`); err != nil {
		t.Fatal(err)
	}

	facts, err := reader.FinalFacts(ctx, "drama-episode")
	if err != nil {
		// THE ASSERTION THIS FILE EXISTS FOR. A statement that does not prepare produces an error
		// here, which is the shape that was missing: the defect shipped because nothing ever called
		// this method.
		t.Fatalf("FinalFacts over a real schema: %v", err)
	}
	if facts.EpisodeID != "drama-episode" {
		t.Fatalf("the read is about %q", facts.EpisodeID)
	}
	// The board's identity, reached through `storyboards` rather than from the version row — which is
	// the join that was wrong.
	if facts.BoardVersionID != versionID {
		t.Fatalf("the reader found board %q, want %q", facts.BoardVersionID, versionID)
	}
	if facts.ScriptVersionID != "drama-script-version" {
		t.Fatalf("the board renders script version %q", facts.ScriptVersionID)
	}
	if len(facts.Shots) != 2 {
		t.Fatalf("the reader returned %d shots, want 2", len(facts.Shots))
	}
	// The order is the board's own, and the durations are MILLISECONDS — the adapter converts once,
	// and a rule comparing milliseconds against seconds would fire on every episode.
	for index, shot := range facts.Shots {
		if shot.Ordinal != index+1 {
			t.Fatalf("shot %d carries ordinal %d", index, shot.Ordinal)
		}
		if shot.DurationMS != 4000 {
			t.Fatalf("shot %d lasts %dms, want 4000", index+1, shot.DurationMS)
		}
		if !shot.Required {
			t.Fatalf("shot %d is not required, and this build treats every boarded shot as required", index+1)
		}
		// The frame the fixture approved, resolved through the panel join.
		if shot.MediaVersionID == "" || shot.MediaHash == "" {
			t.Fatalf("shot %d has no approved media through the panel join: %+v", index+1, shot)
		}
		// And the file's own row, which is what the missing-file rule reads.
		file, present := facts.Files[shot.MediaHash]
		if !present || !file.Present || file.Size == 0 {
			t.Fatalf("shot %d's media file is not in the file table: %+v", index+1, file)
		}
	}
	if facts.ScriptDurationMS != 8000 {
		t.Fatalf("the script's estimate read as %dms, want 8000", facts.ScriptDurationMS)
	}
	// Nothing is approved for audio in this fixture, and the reader must say so rather than invent it:
	// this is what makes the audio rule fire on a real episode.
	for _, shot := range facts.Shots {
		if shot.AudioVersionID != "" {
			t.Fatalf("shot %d reports approved audio this fixture never wrote: %q", shot.Ordinal, shot.AudioVersionID)
		}
	}
	// No track and no export yet, which are ordinary states rather than errors.
	if facts.SubtitleTrack != "" {
		t.Fatalf("the reader found subtitle track %q", facts.SubtitleTrack)
	}
	if facts.Export != nil {
		t.Fatalf("the reader found export %q", facts.Export.ID)
	}
	// THE LICENCE RECORD (T17): migration 000031 gave the asset aggregate its
	// rights columns, so the reader now answers the licence question — an
	// episode with no asset records reports each used asset as Present=false,
	// which is the visible "unknown" rather than an unanswerable question.
	if !facts.LicensesChecked {
		t.Fatal("the reader does not claim to have checked licences; the schema now carries the record")
	}
}

// TestFinalFactsReaderFindsTheMissingLines is the second statement that was wrong.
//
// `dialogue_lines` has no `script_version_id` — it names a SCENE, and the scene names the version.
// The original query filtered on the column that does not exist, so it failed to prepare. This test
// writes a version with two spoken lines and one direction, and a track covering one of the spoken
// ones, then asserts the reader names the uncovered line and not the direction.
func TestFinalFactsReaderFindsTheMissingLines(t *testing.T) {
	ctx := context.Background()
	reader, harness := finalReaderHarness(t)
	harness.approvedBoard(t, 1, 4)

	// A scene with three lines: two spoken, one a direction that must NOT be reported as missing.
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO scenes
		(id, script_version_id, ordinal, scene_number, slugline, interior_exterior, created_at, updated_at)
		VALUES ('fi-scene', 'drama-script-version', 1, '1', 'INT. ROOM', 'INT', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	lines := []struct{ id, kind, text string }{
		{"fi-line-1", "dialogue", "the covered line"},
		{"fi-line-2", "dialogue", "the uncovered line"},
		{"fi-line-3", "action", "she crosses the room"},
	}
	for index, line := range lines {
		if _, err := harness.db.ExecContext(ctx, `INSERT INTO dialogue_lines
			(id, scene_id, ordinal, line_type, text, created_at, updated_at)
			VALUES (?, 'fi-scene', ?, ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			line.id, index+1, line.kind, line.text); err != nil {
			t.Fatal(err)
		}
	}
	// A track whose one cue covers the FIRST line only.
	track := domainmedia.Track{
		ID: "fi-track", EpisodeID: "drama-episode", ScriptVersionID: "drama-script-version",
		VersionNumber: 1, Status: "approved", CreatedByType: "user", CreatedAt: harness.now,
	}
	cues := []domainmedia.Cue{{
		ID: "fi-cue-1", TrackID: "fi-track", Ordinal: 1,
		Start: 0, End: 2000, Text: "the covered line", DialogueLineID: "fi-line-1",
		Status: domainmedia.CueGenerated, CreatedAt: harness.now,
	}}
	if err := harness.subtitles.CreateTrackWithCues(ctx, track, cues); err != nil {
		t.Fatalf("CreateTrackWithCues: %v", err)
	}

	facts, err := reader.FinalFacts(ctx, "drama-episode")
	if err != nil {
		t.Fatalf("FinalFacts: %v", err)
	}
	if facts.SubtitleTrack != "fi-track" {
		t.Fatalf("the approved track read as %q", facts.SubtitleTrack)
	}
	if facts.CueCount != 1 {
		t.Fatalf("the reader counted %d cues, want 1", facts.CueCount)
	}
	if len(facts.MissingLines) != 1 {
		t.Fatalf("the reader found %d uncovered lines, want 1: %+v", len(facts.MissingLines), facts.MissingLines)
	}
	if facts.MissingLines[0].LineID != "fi-line-2" {
		t.Fatalf("the uncovered line is %q, want the second dialogue line", facts.MissingLines[0].LineID)
	}
	// The direction is NOT missing: an action line is not spoken, and reporting it would put a
	// permanent finding on every episode whose script describes what anybody does.
	for _, line := range facts.MissingLines {
		if line.LineID == "fi-line-3" {
			t.Fatal("an action line was reported as a missing subtitle, and nobody can hear an action line")
		}
	}
}

// TestFinalFactsReaderReportsOpenAndWaivedMarks is the third statement that was wrong.
//
// `artifact_staleness` has no `id` column — its primary key is the pair `(artifact_type,
// artifact_id)` — and the original statement selected one, so it failed to prepare. This test writes
// an open mark on the board version, a waived mark on the script version and a CLEARED mark that must
// not be reported, and asserts the reader distinguishes all three.
func TestFinalFactsReaderReportsOpenAndWaivedMarks(t *testing.T) {
	ctx := context.Background()
	reader, harness := finalReaderHarness(t)
	versionID := harness.approvedBoard(t, 1, 4)

	marks := []struct {
		artifactType, artifactID, clearedAt string
		waived                              int
	}{
		{"storyboard_version", versionID, "", 0},
		{"script_version", "drama-script-version", "", 1},
		// Cleared: resolved, so an export must not answer for it.
		{"storyboard_item", "not-a-real-id", "2026-01-02T00:00:00Z", 0},
	}
	for _, mark := range marks {
		if _, err := harness.db.ExecContext(ctx, `INSERT INTO artifact_staleness
			(artifact_type, artifact_id, project_id, severity, reason, waived, cleared_at, created_at, updated_at, revision)
			VALUES (?, ?, 'drama-project', 'review_required', 'an upstream moved', ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
			mark.artifactType, mark.artifactID, mark.waived, mark.clearedAt); err != nil {
			t.Fatal(err)
		}
	}

	facts, err := reader.FinalFacts(ctx, "drama-episode")
	if err != nil {
		t.Fatalf("FinalFacts: %v", err)
	}
	if len(facts.StaleMarks) != 2 {
		t.Fatalf("the reader returned %d marks, want the two uncleared ones: %+v", len(facts.StaleMarks), facts.StaleMarks)
	}
	byArtifact := map[string]appconsistency.FinalStaleMark{}
	for _, mark := range facts.StaleMarks {
		byArtifact[mark.Artifact] = mark
		// The identifier is COMPOSED from the pair, because that pair is the row's key: a finding
		// that named only the artifact id would not identify a row.
		if mark.ID != mark.Artifact+":"+mark.EntityID {
			t.Fatalf("the mark's identifier is %q, want the type and id it is keyed by", mark.ID)
		}
	}
	open, present := byArtifact["storyboard_version"]
	if !present {
		t.Fatalf("the open mark on the board was not returned: %+v", facts.StaleMarks)
	}
	if open.Waived {
		t.Fatal("an open mark was reported as waived, so it would be a remark instead of a blocker")
	}
	waived, present := byArtifact["script_version"]
	if !present {
		t.Fatal("the waived mark was not returned, so section 15.3's visible waiver would be a silent one")
	}
	if !waived.Waived {
		t.Fatal("a waived mark was reported as unwaived, so it would block an export somebody accepted")
	}
	// The finding's entity type is the staleness domain's own vocabulary, so a reader who knows the
	// mark command finds the row it is about.
	if open.EntityType != "storyboard_version" {
		t.Fatalf("the mark's entity type is %q", open.EntityType)
	}
	for _, mark := range facts.StaleMarks {
		if mark.EntityID == "not-a-real-id" {
			t.Fatal("a cleared mark was returned, so a resolved invalidation would block forever")
		}
	}
}

// TestFinalFactsReaderReportsAComposedExport covers the export half of the adapter, which is where
// the manifest's own traceability is read.
//
// A composed export is what makes the parameter and traceability rules do anything, so this reads one
// back through the adapter and asserts the manifest was DECODED rather than carried as opaque text.
func TestFinalFactsReaderReportsAComposedExport(t *testing.T) {
	ctx := context.Background()
	reader, harness := finalReaderHarness(t)
	boardVersionID := harness.approvedBoard(t, 2, 4)
	requireMediaEngine(t, harness)

	if _, _, err := harness.service.Export(ctx, exportRequestFor(boardVersionID)); err != nil {
		t.Fatalf("Export: %v", err)
	}

	facts, err := reader.FinalFacts(ctx, "drama-episode")
	if err != nil {
		t.Fatalf("FinalFacts: %v", err)
	}
	if facts.Export == nil {
		t.Fatal("the reader found no export although one was composed")
	}
	export := facts.Export
	if export.Quality == "" || export.Width <= 0 || export.Height <= 0 {
		t.Fatalf("the export's own parameters did not survive the read: %+v", export)
	}
	if export.OutputHash == "" {
		t.Fatal("the export has no output file, so the file rules have nothing to check")
	}
	if file, present := facts.Files[export.OutputHash]; !present || !file.Present {
		t.Fatalf("the export's output file is not in the file table: %+v", file)
	}
	// The manifest is DECODED: the episode it names and the references it carries are the traceability
	// rule's subject, and a string that was never parsed would leave every one of them empty.
	if export.ManifestEpisodeID != "drama-episode" {
		t.Fatalf("the manifest names episode %q", export.ManifestEpisodeID)
	}
	if len(export.ManifestVersions) == 0 {
		t.Fatal("the manifest's references were not decoded, so nothing can be traced")
	}
	// Every boarded shot's media must be traceable by the key the ruleset looks up.
	for _, shot := range facts.Shots {
		key := "shot:" + shot.ShotID
		if export.ManifestVersions[key] == "" {
			t.Fatalf("the manifest carries no entry for %s, so a superseded version there would go unseen", key)
		}
		if export.ManifestHashes[key] == "" {
			t.Fatalf("the manifest states no hash for %s, so changed bytes would go unseen", key)
		}
	}
}

// TestTheFinalRulesetRunsOverTheAdapter is the end-to-end check that ties the two halves together.
//
// The ruleset's own tests drive a double, and this adapter's own tests drive the reader directly.
// Neither proves the pair works, which is the seam an earlier version of this package fell through:
// the rules were green over facts that a real build could not produce. This walks the real adapter
// into the real ruleset over the real schema and asserts the findings are about the fixture's actual
// state rather than a page of unexplained refusals.
func TestTheFinalRulesetRunsOverTheAdapter(t *testing.T) {
	ctx := context.Background()
	reader, harness := finalReaderHarness(t)
	harness.approvedBoard(t, 2, 4)
	// An APPROVED script version, so the duration comparison has something to read. The approval is
	// the path a build produces; see `TestFinalFactsReaderReadsTheRealSchema` for why the episode's
	// pointer is not.
	if _, err := harness.db.ExecContext(ctx,
		`UPDATE script_versions SET status = 'approved' WHERE id = 'drama-script-version'`); err != nil {
		t.Fatal(err)
	}

	ruleset := appconsistency.NewFinalRuleset(appconsistency.FinalOptions{Reader: reader})
	findings, err := ruleset.CheckEpisode(ctx, "drama-episode")
	if err != nil {
		t.Fatalf("CheckEpisode over the real adapter: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("an episode with no audio, no subtitle track and no export produced no findings")
	}
	// The fixture has approved frames and no audio, so the audio rule must be among them: that is the
	// rule this fixture is built to trip, and a run that reported something else would be reporting
	// about a state nobody wrote.
	if _, found := findingFor(findings, appconsistency.RuleAudioComplete); !found {
		t.Fatalf("the audio rule did not fire on an episode with no approved audio: %+v", findings)
	}
	// And nothing may be about a fact the reader could not have known: a refusal from a broken
	// statement would arrive as an ordinary finding with an empty field, which is the shape that made
	// the original defect invisible.
	for _, finding := range findings {
		if finding.EntityID == "" || finding.Problem == "" {
			t.Fatalf("a finding carries no subject or no problem, which is what a failed read looks like: %+v", finding)
		}
	}
}

// requireMediaEngine skips a test when this machine cannot compose, and says so.
//
// A skipped test is not a pass: the caller's assertions about a composed export cannot run without an
// engine, and reporting one as passing would be evidence about nothing.
func requireMediaEngine(t *testing.T, harness *mediaHarness) {
	t.Helper()
	if !harness.engine.Available() {
		t.Skipf("this machine cannot compose: %s", harness.engine.Diagnostic())
	}
}

// findingFor returns the first finding with one rule.
func findingFor(findings []consistency.Finding, rule string) (consistency.Finding, bool) {
	for _, finding := range findings {
		if finding.Rule == rule {
			return finding, true
		}
	}
	return consistency.Finding{}, false
}

// exportRequestFor is the export this file's tests compose: a preview of the fixture's board.
func exportRequestFor(boardVersionID string) appmedia.ExportRequest {
	return appmedia.ExportRequest{
		EpisodeID:      "drama-episode",
		BoardVersionID: boardVersionID,
		Quality:        domainmedia.QualityPreview,
		FPS:            15,
		CreatedByType:  "user",
	}
}

// TestTheCheckerDispatchesTheFinalStageToTheFinalRuleset is the dispatch assertion, and it is the
// seam that had NO test at all — which is how the eight clauses ran against a broken reader.
//
// # Two failures, one shape
//
// The ruleset's own tests drive it through a double, and the adapter's own tests (above) drive the
// reader. Between them sit three lines of `StoryboardConsistencyChecker.Check` that decide which
// ruleset a stage gets, and nothing exercised them: a switch arm that fell through to `default` would
// have left the stage supervised by a model alone, with every test in the repository still green.
//
// The test drives the COMPOSED checker over a real database, exactly as the stage machine does, and
// asserts three things that can only be true together: the final stage produces findings, the
// storyboard stage produces ITS findings rather than the final ones, and an unknown stage produces
// none. The three together are what say the switch routes rather than merely answering.
func TestTheCheckerDispatchesTheFinalStageToTheFinalRuleset(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	boardVersionID := harness.approvedBoard(t, 2, 4)
	if _, err := harness.db.ExecContext(ctx,
		`UPDATE script_versions SET status = 'approved' WHERE id = 'drama-script-version'`); err != nil {
		t.Fatal(err)
	}

	// The composition root's own construction, minus the two ports the storyboard rules need and this
	// test does not assert on: passing nils is what a build with those repositories absent looks like,
	// and the storyboard ruleset skips the rules whose readers are missing rather than inventing them.
	checker := NewStoryboardConsistencyChecker(nil, nil, nil, nil).
		WithFinalRuleset(NewFinalFactsReader(harness.db))

	// The final stage: its artifact is the EPISODE, which is what `StageAgents.ArtifactType` says for
	// it, and the findings must be the fixture's real state (no audio, no track, no export).
	finalFindings, err := checker.Check(ctx, "final_episode", "drama-episode")
	if err != nil {
		t.Fatalf("the final stage's rules refused to run: %v", err)
	}
	if len(finalFindings) == 0 {
		t.Fatal("the final stage produced no findings on an episode with no audio, no subtitles and no export")
	}
	if _, found := findingFor(finalFindings, appconsistency.RuleAudioComplete); !found {
		t.Fatalf("the final stage's findings are not the Final Ruleset's: %+v", finalFindings)
	}
	// The traceability rule is the other one this fixture must trip, and it is the one AC-MEDIA-003
	// names as its own clause. Requiring both is what says the whole ruleset ran rather than one rule.
	if _, found := findingFor(finalFindings, appconsistency.RuleExportTraceable); !found {
		t.Fatalf("the traceability rule did not fire on an episode with no export: %+v", finalFindings)
	}

	// The storyboard stage goes to the OTHER ruleset. Its readers are absent here, so it REFUSES — and
	// the refusal is itself the assertion: `Checker.CheckStoryboard` reports "no storyboard reader is
	// configured" when it has none, which is a different answer from the final ruleset's findings. A
	// switch that fell through to the wrong arm would return the final findings instead of this error,
	// so the error is what says the routing happened.
	boardFindings, err := checker.Check(ctx, "storyboard_table", boardVersionID)
	if err == nil {
		t.Fatalf("the storyboard stage ran without its readers and produced %+v, so its port's guard is gone", boardFindings)
	}
	for _, finding := range boardFindings {
		if finding.Rule == appconsistency.RuleAudioComplete || finding.Rule == appconsistency.RuleExportTraceable {
			t.Fatalf("the storyboard stage was given the final ruleset's findings: %+v", finding)
		}
	}

	// An empty artifact identifier is refused by every arm rather than reaching a reader with nothing
	// to read, and a stage this build has no rules for answers "nothing mechanical to say".
	for _, stage := range []string{"final_episode", "storyboard_table", "a_stage_with_no_rules"} {
		findings, err := checker.Check(ctx, stage, "")
		if err != nil {
			t.Fatalf("%s with no artifact produced an error rather than nothing: %v", stage, err)
		}
		if len(findings) != 0 {
			t.Fatalf("%s with no artifact produced %d findings", stage, len(findings))
		}
	}
}
