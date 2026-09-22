package consistency

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
)

// These tests cover AGENT_CONTRACTS section 11.4's Final Ruleset.
//
// # What they are written against
//
// Each of the eight clauses gets a case where it FIRES and a case where it stays SILENT, because a
// rule that reports nothing when something is wrong and a rule that reports something when nothing
// is are different defects and a suite with only one direction cannot tell them apart. The silent
// cases matter more here than in most rulesets: these rules run before a supervisor on every final
// stage, so a rule that fired on a healthy episode would put a blocker in front of every export.
//
// # What they deliberately do not re-test, and the correction that matters
//
// The STORAGE is not exercised here. The first version of this comment claimed `FinalFactsReader "has
// its own tests over a real schema"`, and it did not — which is the whole reason a critical defect
// shipped: the adapter's SQL was written against columns that do not exist, so `FinalFacts` returned
// an error on every call, the stage machine's `if err == nil` discarded it, and all eight of section
// 11.4's clauses contributed nothing while these tests stayed green over facts no real build could
// produce.
//
// The storage tests exist now: `internal/infrastructure/database/final_reader_test.go` drives the real
// adapter over the real schema, and `TestTheFinalRulesetRunsOverTheAdapter` walks it into this ruleset.
// What stays HERE is the double, because it is what lets a case state ONE fault in isolation — a
// property a database-backed fixture cannot have, since a real episode has a dozen other facts a rule
// might react to.

// finalReader is the double every test here drives the ruleset through.
type finalReader struct {
	facts FinalFacts
	err   error
	// asked records the episode the ruleset named, so a test can assert the rules read the episode
	// they were asked about rather than one they invented.
	asked string
}

func (r *finalReader) FinalFacts(_ context.Context, episodeID string) (FinalFacts, error) {
	r.asked = episodeID
	if r.err != nil {
		return FinalFacts{}, r.err
	}
	facts := r.facts
	if facts.EpisodeID == "" {
		facts.EpisodeID = episodeID
	}
	// The maps are defaulted rather than left nil, so a rule reading them cannot tell a test's
	// omission from an empty answer — the same thing the real adapter guarantees.
	//
	// `LicensesChecked` is NOT defaulted, and that is deliberate: it is the field two tests set in
	// opposite directions, and a double that overwrote it would make the licence-gap case assert
	// nothing. `healthyEpisode` states it, so a test that wants the other path says so.
	if facts.Files == nil {
		facts.Files = map[string]FinalFile{}
	}
	if facts.Licenses == nil {
		facts.Licenses = map[string]FinalLicense{}
	}
	return facts, nil
}

// runFinal runs the ruleset over one set of facts.
func runFinal(t *testing.T, facts FinalFacts) []consistency.Finding {
	t.Helper()
	reader := &finalReader{facts: facts}
	ruleset := NewFinalRuleset(FinalOptions{Reader: reader})
	findings, err := ruleset.CheckEpisode(context.Background(), "episode-1")
	if err != nil {
		t.Fatalf("CheckEpisode: %v", err)
	}
	if reader.asked != "episode-1" {
		t.Fatalf("the ruleset read episode %q, want the one it was asked about", reader.asked)
	}
	return findings
}

// healthyEpisode is a production that passes every rule.
//
// It is the baseline every silent case starts from, and it is deliberately COMPLETE rather than
// minimal: two shots with media, audio and their files present, a subtitle track covering the one
// spoken line, an export whose manifest matches, and the licences recorded. A baseline that omitted
// something would make each silent case also a test of that omission.
func healthyEpisode() FinalFacts {
	return FinalFacts{
		EpisodeID:       "episode-1",
		BoardVersionID:  "board-1",
		ScriptVersionID: "script-1",
		Shots: []FinalShot{
			{
				Ordinal: 1, ItemID: "item-1", ShotID: "shot-1", DurationMS: 4000, Required: true,
				MediaVersionID: "media-1", MediaHash: "hash-a", MediaKind: "image",
				VideoMIME: "image/png", VideoBytes: 40000,
				AudioVersionID: "audio-1", AudioHash: "hash-b", AudioMIME: "audio/mpeg",
				AudioBytes: 20000, AudioDurationMS: 3800,
				LicenseAssetIDs: []string{"asset-1"},
			},
			{
				Ordinal: 2, ItemID: "item-2", ShotID: "shot-2", DurationMS: 4000, Required: true,
				MediaVersionID: "media-2", MediaHash: "hash-c", MediaKind: "image",
				VideoMIME: "image/png", VideoBytes: 42000,
				AudioVersionID: "audio-2", AudioHash: "hash-d", AudioMIME: "audio/mpeg",
				AudioBytes: 21000, AudioDurationMS: 3900,
				LicenseAssetIDs: []string{"asset-1"},
			},
		},
		TotalDurationMS:  8000,
		ScriptDurationMS: 8000,
		SubtitleTrack:    "track-1",
		CueCount:         2,
		// The licence question IS answerable in this fixture, which is what makes the per-asset rule
		// the one under test in every case that does not flip it. A build whose storage cannot answer
		// is the subject of its own test.
		LicensesChecked: true,
		Files: map[string]FinalFile{
			"hash-a":   {Hash: "hash-a", MIME: "image/png", Size: 40000, Present: true},
			"hash-b":   {Hash: "hash-b", MIME: "audio/mpeg", Size: 20000, Present: true},
			"hash-c":   {Hash: "hash-c", MIME: "image/png", Size: 42000, Present: true},
			"hash-d":   {Hash: "hash-d", MIME: "audio/mpeg", Size: 21000, Present: true},
			"hash-out": {Hash: "hash-out", MIME: "video/mp4", Size: 900000, Present: true},
		},
		Licenses: map[string]FinalLicense{
			"asset-1": {AssetID: "asset-1", Name: "Lin", License: "original", Present: true, AllowsUse: true},
		},
		Export: &FinalExport{
			ID: "export-1", VersionNumber: 1, Quality: "preview", Width: 1920, Height: 1080, FPS: 30,
			DurationMS: 8000, SubtitleMode: "sidecar", SubtitleTrack: "track-1",
			OutputHash:        "hash-out",
			ManifestEpisodeID: "episode-1",
			ManifestVersions: map[string]string{
				"shot:shot-1": "media-1", "shot:shot-2": "media-2", "script": "script-1",
				"board": "board-1", "subtitle": "track-1",
			},
			ManifestHashes: map[string]string{"shot:shot-1": "hash-a", "shot:shot-2": "hash-c"},
		},
	}
}

// TestTheHealthyEpisodePassesEveryRule is the baseline assertion, and it is the most important one
// here: a Final Ruleset that reports a blocker on a good episode blocks every export, and the rules
// run before a supervisor on every final stage.
func TestTheHealthyEpisodePassesEveryRule(t *testing.T) {
	findings := runFinal(t, healthyEpisode())
	if len(findings) != 0 {
		t.Fatalf("a healthy episode produced %d findings: %+v", len(findings), findings)
	}
}

// TestACriticalFindingForAShotWithNoMedia covers clause one: 所有必需 Shot 有批准视频.
func TestACriticalFindingForAShotWithNoMedia(t *testing.T) {
	facts := healthyEpisode()
	facts.Shots[1].MediaVersionID = ""
	facts.Shots[1].MediaHash = ""
	facts.Shots[1].VideoMIME = ""
	facts.Shots[1].VideoBytes = 0
	findings := runFinal(t, facts)
	finding, ok := findFinding(findings, RuleShotMedia)
	if !ok {
		t.Fatalf("a shot with no media produced no %s finding: %+v", RuleShotMedia, findings)
	}
	if !finding.Blocker() {
		t.Fatalf("a shot with no media is not a blocker, so an export could pass with a hole in it")
	}
	// The finding must name the shot, because "a shot has no media" is not something a person can
	// find in a board of twelve rows.
	if finding.EntityID != "item-2" || !strings.Contains(finding.Location, "2") {
		t.Fatalf("the finding names %q at %q, want the second row", finding.EntityID, finding.Location)
	}
	// And it must NOT fire for the first shot, which has media: a rule that reported both would make
	// the report useless.
	for _, other := range findings {
		if other.Rule == RuleShotMedia && other.EntityID == "item-1" {
			t.Fatal("the rule reported a shot that has approved media")
		}
	}
}

// TestAnEmptyBoardIsNotReportedAsMissingMedia is the silent direction of clause one, and it is the
// false positive that would matter most: a version just created has no rows, and reporting every
// shot as missing media on a board with no rows would put a page of findings in front of a user
// whose episode has not been boarded yet.
func TestAnEmptyBoardIsNotReportedAsMissingMedia(t *testing.T) {
	facts := healthyEpisode()
	facts.Shots = nil
	facts.TotalDurationMS = 0
	facts.Export = nil
	for _, finding := range runFinal(t, facts) {
		if finding.Rule == RuleShotMedia {
			t.Fatalf("a board with no rows produced a missing-media finding: %+v", finding)
		}
	}
}

// TestAShotFlaggedNotRequiredIsNotReported covers the field the rules branch on. A shot a director
// deliberately left out must not block the export, or the flag would have no purpose.
//
// BOTH RULES ARE CHECKED, and the audio half was the defect an independent quality review found: the
// first version cleared shot 2's MEDIA and left its AUDIO approved, so the audio rule had no candidate
// to suppress and a mutation that dropped its `Required` guard survived. A fixture for "this shot is
// exempt" has to clear everything the exemption covers, or it only tests the rule it happened to
// disturb.
func TestAShotFlaggedNotRequiredIsNotReported(t *testing.T) {
	facts := healthyEpisode()
	// The shot is exempt from every completeness rule at once.
	facts.Shots[1].MediaVersionID = ""
	facts.Shots[1].MediaHash = ""
	facts.Shots[1].VideoMIME = ""
	facts.Shots[1].VideoBytes = 0
	facts.Shots[1].AudioVersionID = ""
	facts.Shots[1].AudioHash = ""
	facts.Shots[1].AudioMIME = ""
	facts.Shots[1].AudioBytes = 0
	facts.Shots[1].Required = false
	findings := runFinal(t, facts)
	for _, finding := range findings {
		if finding.Rule == RuleShotMedia {
			t.Fatalf("a shot marked not required produced a media blocker: %+v", finding)
		}
		if finding.Rule == RuleAudioComplete {
			t.Fatalf("a shot marked not required produced an audio blocker: %+v", finding)
		}
	}
	// And the SAME fixture with the flag flipped reports both, which is what says the two rules were
	// in a position to fire rather than being unreachable for some other reason.
	facts.Shots[1].Required = true
	required := runFinal(t, facts)
	if _, ok := findFinding(required, RuleShotMedia); !ok {
		t.Fatalf("the same shot, required, produced no media finding: %+v", required)
	}
	if _, ok := findFinding(required, RuleAudioComplete); !ok {
		t.Fatalf("the same shot, required, produced no audio finding: %+v", required)
	}
}

// TestAMajorFindingForAShotWithNoAudio covers clause two's audio half.
func TestAMajorFindingForAShotWithNoAudio(t *testing.T) {
	facts := healthyEpisode()
	facts.Shots[0].AudioVersionID = ""
	facts.Shots[0].AudioHash = ""
	findings := runFinal(t, facts)
	finding, ok := findFinding(findings, RuleAudioComplete)
	if !ok {
		t.Fatalf("a shot with no audio produced no %s finding: %+v", RuleAudioComplete, findings)
	}
	if finding.EntityID != "item-1" {
		t.Fatalf("the finding names %q, want the first row", finding.EntityID)
	}
	// The SEVERITY is asserted directly, because the assertion this replaced could not fail: it
	// compared the finding count against the count of `blockersOf` — a filter using `Blocker()`, which
	// is `Severity == Critical || Severity == Major` — over a list in which every element was already
	// one of those. The two lengths were equal by construction, so the claim "a finding of major
	// severity is not a blocker" was never tested, and a mutation that changed this severity to
	// `major` would have passed it. An independent quality review found it.
	if finding.Severity != consistency.SeverityMajor {
		t.Fatalf("the audio finding is %s, want major: missing audio is a defect a person must fix, "+
			"but the episode is not unexportable while they decide", finding.Severity)
	}
	if !finding.Blocker() {
		t.Fatal("a major finding does not block, so an episode with silent dialogue would pass review")
	}
}

// TestASubtitleTrackWithMissingLinesIsReported covers clause two's subtitle half and AC-MEDIA-002's
// "missing line detected".
func TestASubtitleTrackWithMissingLinesIsReported(t *testing.T) {
	facts := healthyEpisode()
	facts.MissingLines = []FinalLine{{LineID: "line-7", Type: "dialogue", Text: "I never said that."}}
	findings := runFinal(t, facts)
	finding, ok := findFinding(findings, RuleSubtitleComplete)
	if !ok {
		t.Fatalf("an uncovered line produced no %s finding: %+v", RuleSubtitleComplete, findings)
	}
	if finding.EntityID != "line-7" {
		t.Fatalf("the finding names %q, want the line", finding.EntityID)
	}
	// The line's own text travels in the problem, because a person deciding whether to fix the track
	// or export anyway needs to know which line it was.
	if !strings.Contains(finding.Problem, "I never said that") {
		t.Fatalf("the finding does not quote the line: %q", finding.Problem)
	}
	if !finding.AutoFixable {
		t.Fatal("a missing cue is mechanical — the text and its neighbours are known — but is not marked fixable")
	}
}

// TestAnEpisodeWithNoSubtitleTrackIsReported covers the other subtitle case: no approved track at
// all. It is a different finding from an uncovered line, and conflating them would report a missing
// track as N missing lines.
func TestAnEpisodeWithNoSubtitleTrackIsReported(t *testing.T) {
	facts := healthyEpisode()
	facts.SubtitleTrack = ""
	facts.CueCount = 0
	facts.Export.SubtitleMode = "off"
	facts.Export.SubtitleTrack = ""
	findings := runFinal(t, facts)
	finding, ok := findFinding(findings, RuleSubtitleComplete)
	if !ok {
		t.Fatalf("an episode with no track produced no finding: %+v", findings)
	}
	if finding.EntityType != "episode" {
		t.Fatalf("the finding is about a %s, want the episode", finding.EntityType)
	}
	// And exactly ONE: the loop over missing lines must not run when there is no track to have lines
	// missing from.
	count := 0
	for _, other := range findings {
		if other.Rule == RuleSubtitleComplete {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("an episode with no track produced %d subtitle findings, want one", count)
	}
}

// TestACriticalFindingForAFileThatIsNotInTheStore covers clause three: 媒体文件存在.
func TestACriticalFindingForAFileThatIsNotInTheStore(t *testing.T) {
	facts := healthyEpisode()
	// A hash with no row at all, which is what an interrupted job leaves behind.
	delete(facts.Files, "hash-c")
	findings := runFinal(t, facts)
	finding, ok := findFinding(findings, RuleMediaFilePresent)
	if !ok {
		t.Fatalf("a cited file with no row produced no finding: %+v", findings)
	}
	if !finding.Blocker() {
		t.Fatal("a missing media file is not a blocker, so the export would fail at composition instead")
	}
	if finding.Field != "media" {
		t.Fatalf("the finding names the %s role, want media", finding.Field)
	}
}

// TestAPresentFileIsNotReported is the silent direction of clause three.
func TestAPresentFileIsNotReported(t *testing.T) {
	for _, finding := range runFinal(t, healthyEpisode()) {
		if finding.Rule == RuleMediaFilePresent {
			t.Fatalf("a file that is present was reported missing: %+v", finding)
		}
	}
}

// TestOneMissingFileIsReportedOnce covers the dedupe: two shots using the same character's file must
// not produce two findings about the same hash.
func TestOneMissingFileIsReportedOnce(t *testing.T) {
	facts := healthyEpisode()
	facts.Shots[1].MediaHash = "hash-a"
	delete(facts.Files, "hash-a")
	seen := 0
	for _, finding := range runFinal(t, facts) {
		if finding.Rule == RuleMediaFilePresent {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("one missing file used by two shots produced %d findings, want one", seen)
	}
}

// TestACriticalFindingForPlaceholderMedia covers clause five: 黑帧/空帧/静音异常, as far as a probe
// can answer it. A twenty-four byte file is exactly what the mock video adapter returns, which is the
// case the rule exists for.
func TestACriticalFindingForPlaceholderMedia(t *testing.T) {
	facts := healthyEpisode()
	facts.Shots[0].VideoBytes = 24
	facts.Files["hash-a"] = FinalFile{Hash: "hash-a", MIME: "image/png", Size: 24, Present: true}
	findings := runFinal(t, facts)
	finding, ok := findFinding(findings, RuleMediaEmpty)
	if !ok {
		t.Fatalf("a 24-byte media file produced no finding: %+v", findings)
	}
	if !finding.Blocker() {
		t.Fatal("placeholder media is not a blocker, so an export could carry it")
	}
	if finding.Severity != consistency.SeverityCritical {
		t.Fatalf("placeholder media is %s, want critical: the file cannot be composed at all", finding.Severity)
	}
	if !strings.Contains(finding.Problem, "24") {
		t.Fatalf("the finding does not state the size: %q", finding.Problem)
	}
	// The AUDIO branch is the one an independent quality review found had no firing case: the same
	// fixture mutated the video size only, so a broken comparison on the audio side survived the whole
	// suite. A container header with no recording in it is what a failed text-to-speech job leaves.
	audio := healthyEpisode()
	audio.Shots[0].AudioBytes = 24
	audio.Shots[0].AudioDurationMS = 0
	audio.Files["hash-b"] = FinalFile{Hash: "hash-b", MIME: "audio/mpeg", Size: 24, Present: true}
	audioFinding, ok := findFinding(runFinal(t, audio), RuleMediaEmpty)
	if !ok {
		t.Fatal("a 24-byte audio file produced no finding")
	}
	if audioFinding.Field != "audio" {
		t.Fatalf("the finding names the %s field, want audio", audioFinding.Field)
	}
	if audioFinding.Severity != consistency.SeverityMajor {
		t.Fatalf("placeholder audio is %s, want major", audioFinding.Severity)
	}
	// A recording that merely has no duration recorded is NOT reported: the fixture above states zero
	// duration because a header has none, and the rule must fire on the SIZE rather than on the missing
	// metadata. A real recording whose length was never read back is a different situation.
	recorded := healthyEpisode()
	recorded.Shots[0].AudioDurationMS = 0
	recorded.Shots[0].AudioBytes = 20000
	for _, other := range runFinal(t, recorded) {
		if other.Rule == RuleMediaEmpty && other.Field == "audio" {
			t.Fatalf("a real recording with no duration read back was reported as empty: %+v", other)
		}
	}
}

// TestANonMediaTypeIsReported covers the other half of clause five: a file ffmpeg cannot compose.
func TestANonMediaTypeIsReported(t *testing.T) {
	facts := healthyEpisode()
	facts.Shots[0].VideoMIME = "application/pdf"
	findings := runFinal(t, facts)
	if _, ok := findFinding(findings, RuleMediaEmpty); !ok {
		t.Fatalf("a PDF as a shot's media produced no finding: %+v", findings)
	}
}

// TestHealthyMediaSizesAreNotReported is the silent direction of clause five, and the threshold is
// the thing under test: a rule whose floor was a megabyte would fire on a legitimate small PNG.
//
// THE NUMBERS ARE WRITTEN OUT rather than read from the constants. An independent quality review
// found this test, `TestAValidExportIsNotReported` and the bound table all spelled their fixtures with
// `DefaultMinMediaBytes` and `DefaultMaxWidth` — so a mutation that changed the constant moved the
// test WITH it and survived, and the test would have passed for any floor at all. A value asserted
// against itself is not a test; the literals below are the same numbers the constants use, and
// `TestTheDefaultsAreTheNumbersTheTestsPin` is what keeps the two from drifting apart silently.
func TestHealthyMediaSizesAreNotReported(t *testing.T) {
	facts := healthyEpisode()
	// Just at the floor: 1024 bytes.
	facts.Shots[0].VideoBytes = 1024
	facts.Files["hash-a"] = FinalFile{Hash: "hash-a", MIME: "image/png", Size: 1024, Present: true}
	for _, finding := range runFinal(t, facts) {
		if finding.Rule == RuleMediaEmpty {
			t.Fatalf("a file at the size floor was reported as empty: %+v", finding)
		}
	}
	// And a tiny but real PNG above the floor is not a placeholder either: the rule's subject is a
	// file that cannot be a picture, not one that is merely small.
	facts.Shots[0].VideoBytes = 2048
	facts.Files["hash-a"] = FinalFile{Hash: "hash-a", MIME: "image/png", Size: 2048, Present: true}
	for _, finding := range runFinal(t, facts) {
		if finding.Rule == RuleMediaEmpty {
			t.Fatalf("a two-kilobyte PNG was reported as a placeholder: %+v", finding)
		}
	}
}

// TestTheDefaultsAreTheNumbersTheTestsPin is the drift guard the tests above need.
//
// The literals in this file are deliberately NOT the constants, so that a changed constant fails a
// test instead of moving it. That property only holds while something states what the constants
// currently ARE — otherwise the two would drift and a reader would not know which was intended. This
// is that statement, and it is expected to fail (and be updated deliberately) when a bound changes.
func TestTheDefaultsAreTheNumbersTheTestsPin(t *testing.T) {
	cases := []struct {
		name     string
		actual   int
		expected int
	}{
		{"DefaultMinMediaBytes", DefaultMinMediaBytes, 1024},
		{"DefaultMaxWidth", DefaultMaxWidth, 3840},
		{"DefaultMaxHeight", DefaultMaxHeight, 2160},
		{"DefaultMaxFPS", DefaultMaxFPS, 60},
	}
	for _, testCase := range cases {
		if testCase.actual != testCase.expected {
			t.Fatalf("%s is %d and the tests in this file pin %d; changing a bound is a decision, so "+
				"update both", testCase.name, testCase.actual, testCase.expected)
		}
	}
	// A zero or negative bound would make the option's "unset" branch unreachable, which is how the
	// defaults would silently stop applying.
	ruleset := NewFinalRuleset(FinalOptions{})
	if ruleset.minMediaBytes != DefaultMinMediaBytes || ruleset.maxWidth != DefaultMaxWidth ||
		ruleset.maxHeight != DefaultMaxHeight || ruleset.maxFPS != DefaultMaxFPS {
		t.Fatal("NewFinalRuleset with no options did not apply the defaults")
	}
}

// TestAnOpenStaleMarkBlocksAndAWaivedOneIsRecorded covers clause four: stale/waiver.
//
// The two are DIFFERENT findings of different severity, and that is the whole clause: an open mark
// means something this episode was built from has changed, and a waived mark means a person accepted
// that knowingly. Section 15.3 requires the waiver to be VISIBLE in the final export, so the rule
// reports it as a remark rather than staying silent.
func TestAnOpenStaleMarkBlocksAndAWaivedOneIsRecorded(t *testing.T) {
	facts := healthyEpisode()
	facts.StaleMarks = []FinalStaleMark{
		{ID: "script_version:script-1", EntityType: "script_version", EntityID: "script-1", Artifact: "script_version"},
	}
	findings := runFinal(t, facts)
	open, ok := findFinding(findings, RuleStaleOpen)
	if !ok {
		t.Fatalf("an open stale mark produced no finding: %+v", findings)
	}
	if !open.Blocker() {
		t.Fatal("an open stale mark is not a blocker")
	}

	waived := healthyEpisode()
	waived.StaleMarks = []FinalStaleMark{
		{ID: "script_version:script-1", EntityType: "script_version", EntityID: "script-1",
			Artifact: "script_version", Waived: true},
	}
	waivedFindings := runFinal(t, waived)
	remark, ok := findFinding(waivedFindings, RuleStaleOpen)
	if !ok {
		t.Fatal("a waived stale mark was not recorded, so the export would hide a waiver")
	}
	if remark.Blocker() {
		t.Fatal("a waived stale mark blocks the export, so a user could never accept one")
	}
	if !strings.Contains(remark.Problem, "waived") {
		t.Fatalf("the finding does not say the mark was waived: %q", remark.Problem)
	}
}

// TestNoStaleMarksProduceNoFindings is the silent direction of clause four.
func TestNoStaleMarksProduceNoFindings(t *testing.T) {
	for _, finding := range runFinal(t, healthyEpisode()) {
		if finding.Rule == RuleStaleOpen {
			t.Fatalf("an episode with no stale marks produced a finding: %+v", finding)
		}
	}
}

// TestTheDurationRules compares clause six in both of its directions.
//
// The two comparisons are different questions and the severities differ because the consequences do:
// a board whose total drifts from the script's estimate is a planning remark, while an exported file
// whose length differs from the board's shots is a file that is not the film that was planned.
//
// THE CASES ARE MINUTE-SCALED, which the rule's tolerance requires rather than prefers: it is a tenth
// of the estimate with a FIVE-SECOND floor, so an eight-second fixture can never drift past it. That
// floor is right for the rule's subject — a real episode is minutes long, and a board states whole
// seconds per row — and a test written on seconds would be testing the floor rather than the rule.
func TestTheDurationRules(t *testing.T) {
	// A board a fifth longer than the script's estimate: twelve minutes against the script's ten,
	// which is double the tolerance.
	facts := healthyEpisode()
	facts.TotalDurationMS = 720000
	facts.ScriptDurationMS = 600000
	facts.Export = nil
	finding, ok := findFinding(runFinal(t, facts), RuleFinalDuration)
	if !ok {
		t.Fatal("a board a fifth longer than the script's estimate produced no finding")
	}
	if finding.Blocker() {
		t.Fatal("planning drift blocks the export, which would stop a director from making a longer cut")
	}

	// Inside the tolerance: a tenth of the estimate is the boundary, and five percent is not drift.
	facts = healthyEpisode()
	facts.TotalDurationMS = 800000
	facts.ScriptDurationMS = 760000
	facts.Export = nil
	for _, other := range runFinal(t, facts) {
		if other.Rule == RuleFinalDuration {
			t.Fatalf("a five-percent drift was reported: %+v", other)
		}
	}

	// The exported file disagreeing with the board is the other comparison, and it is major.
	facts = healthyEpisode()
	facts.Export.DurationMS = 30000
	finding, ok = findFinding(runFinal(t, facts), RuleFinalDuration)
	if !ok {
		t.Fatal("an exported file three times the board's length produced no finding")
	}
	if !finding.Blocker() {
		t.Fatal("a file that is not the film that was planned is not a blocker")
	}
	if finding.EntityType != "episode_export" {
		t.Fatalf("the finding is about a %s, want the export", finding.EntityType)
	}
}

// TestLicenceGapIsReportedRatherThanPassedSilently covers clause seven, and it asserts the property
// this build actually has: there is nowhere to read a licence from, so the rule reports the GAP.
//
// A rule that returned no findings for an unanswerable question would read as green, which is the
// silently-passing shape this repository refuses.
func TestLicenceGapIsReportedRatherThanPassedSilently(t *testing.T) {
	facts := healthyEpisode()
	facts.LicensesChecked = false
	facts.Licenses = map[string]FinalLicense{}
	findings := runFinal(t, facts)
	finding, ok := findFinding(findings, RuleLicenseMetadata)
	if !ok {
		t.Fatalf("a build with no licence storage reported nothing: %+v", findings)
	}
	if finding.Blocker() {
		t.Fatal("the gap blocks the export, which would make every episode unexportable on this build")
	}
	if !strings.Contains(finding.Problem, "licence") {
		t.Fatalf("the finding does not name what is missing: %q", finding.Problem)
	}
	// An episode with no shots is not reported: the gap only matters once there is something whose
	// rights would be exported.
	empty := healthyEpisode()
	empty.Shots = nil
	empty.LicensesChecked = false
	for _, other := range runFinal(t, empty) {
		if other.Rule == RuleLicenseMetadata {
			t.Fatalf("an episode with no shots reported the licence gap: %+v", other)
		}
	}
}

// TestAPerAssetLicenceProblemIsReportedOnce covers the per-asset branch, which runs only in a build
// whose storage can answer.
func TestAPerAssetLicenceProblemIsReportedOnce(t *testing.T) {
	facts := healthyEpisode()
	facts.Licenses = map[string]FinalLicense{}
	seen := 0
	var finding consistency.Finding
	for _, candidate := range runFinal(t, facts) {
		if candidate.Rule == RuleLicenseMetadata {
			seen++
			finding = candidate
		}
	}
	// asset-1 is used by BOTH shots, and one finding is the answer: twenty identical lines about one
	// costume is a report a person stops reading.
	if seen != 1 {
		t.Fatalf("one unlicensed asset used by two shots produced %d findings, want one", seen)
	}
	if !finding.Blocker() {
		t.Fatal("an asset with no licence metadata is not a blocker, so an episode could ship without rights")
	}
	if finding.EntityType != "asset" || finding.EntityID != "asset-1" {
		t.Fatalf("the finding names %s/%s, want the asset", finding.EntityType, finding.EntityID)
	}

	// A licence that forbids the use is the worse case and reads differently.
	facts.Licenses = map[string]FinalLicense{
		"asset-1": {AssetID: "asset-1", Name: "Stock plate", License: "CC-BY-NC", Present: true, AllowsUse: false},
	}
	finding, ok := findFinding(runFinal(t, facts), RuleLicenseMetadata)
	if !ok {
		t.Fatal("an asset whose licence forbids the use produced no finding")
	}
	if !strings.Contains(finding.Problem, "CC-BY-NC") {
		t.Fatalf("the finding does not name the licence: %q", finding.Problem)
	}
}

// TestExportParametersAreBounded covers clause eight, each bound separately.
func TestExportParametersAreBounded(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*FinalFacts)
		field    string
		contains string
	}{
		{"an unknown quality", func(f *FinalFacts) { f.Export.Quality = "ultra" }, "quality", "ultra"},
		{"no frame size", func(f *FinalFacts) { f.Export.Width = 0 }, "width", "no frame size"},
		{"a size above the bound", func(f *FinalFacts) { f.Export.Width = 7680 }, "width", "7680"},
		{"an odd dimension", func(f *FinalFacts) { f.Export.Height = 1081 }, "width", "even"},
		{"a frame rate above the bound", func(f *FinalFacts) { f.Export.FPS = 120 }, "fps", "120"},
		{"an unknown subtitle mode", func(f *FinalFacts) { f.Export.SubtitleMode = "soft" }, "subtitleMode", "soft"},
		{"subtitles asked for with no track", func(f *FinalFacts) {
			f.Export.SubtitleTrack = ""
			f.SubtitleTrack = ""
			f.Export.SubtitleMode = "burn"
		}, "subtitleTrackId", "no subtitle track"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			facts := healthyEpisode()
			testCase.mutate(&facts)
			findings := runFinal(t, facts)
			var found *consistency.Finding
			for index := range findings {
				if findings[index].Rule == RuleExportParameters && findings[index].Field == testCase.field {
					found = &findings[index]
					break
				}
			}
			if found == nil {
				t.Fatalf("%s produced no %s finding on field %s: %+v",
					testCase.name, RuleExportParameters, testCase.field, findings)
			}
			if !found.Blocker() {
				t.Fatalf("%s is not a blocker", testCase.name)
			}
			if !strings.Contains(found.Problem, testCase.contains) {
				t.Fatalf("the finding reads %q, which does not mention %q", found.Problem, testCase.contains)
			}
		})
	}
}

// TestAValidExportIsNotReported is the silent direction of clause eight.
//
// The numbers are LITERALS for the reason the media-size test states: a bound asserted against itself
// moves with the constant and pins nothing. `TestTheDefaultsAreTheNumbersTheTestsPin` is what says the
// literals and the constants still agree.
func TestAValidExportIsNotReported(t *testing.T) {
	facts := healthyEpisode()
	// The largest legal export, which is the boundary a bound must INCLUDE: 4K at 60fps.
	facts.Export.Quality = "final"
	facts.Export.Width = 3840
	facts.Export.Height = 2160
	facts.Export.FPS = 60
	for _, finding := range runFinal(t, facts) {
		if finding.Rule == RuleExportParameters {
			t.Fatalf("the largest legal export was refused: %+v", finding)
		}
	}
	// One pixel wider is refused, which is what says the bound is a bound rather than a ceiling with
	// slack. The pair is the assertion: without the second half, a rule that accepted anything would
	// pass the first.
	facts.Export.Width = 3842
	if _, ok := findFinding(runFinal(t, facts), RuleExportParameters); !ok {
		t.Fatal("a frame one step above the width bound was accepted")
	}
}

// TestAnEpisodeWithNoExportIsReportedByTraceability covers AC-MEDIA-003's manifest clause in the
// state before the first export.
func TestAnEpisodeWithNoExportIsReportedByTraceability(t *testing.T) {
	facts := healthyEpisode()
	facts.Export = nil
	finding, ok := findFinding(runFinal(t, facts), RuleExportTraceable)
	if !ok {
		t.Fatalf("an episode with no export produced no traceability finding: %+v", findingsOf(facts, t))
	}
	if finding.EntityType != "episode" {
		t.Fatalf("the finding is about a %s, want the episode", finding.EntityType)
	}
}

// TestASupersededMediaInTheManifestIsReported covers clause eight's real content: what the manifest
// claims must still be what is approved.
//
// ADR-0015 section 11 rules that this makes the export STALE rather than wrong, which is why it is
// major and not critical: a person resolves it by re-exporting or by accepting the earlier cut.
func TestASupersededMediaInTheManifestIsReported(t *testing.T) {
	facts := healthyEpisode()
	// Shot 2's approved media has moved on since the export was made.
	facts.Shots[1].MediaVersionID = "media-3"
	facts.Shots[1].MediaHash = "hash-e"
	facts.Files["hash-e"] = FinalFile{Hash: "hash-e", MIME: "image/png", Size: 43000, Present: true}
	finding, ok := findFinding(runFinal(t, facts), RuleExportTraceable)
	if !ok {
		t.Fatal("an export made from media the project has since replaced produced no finding")
	}
	if finding.EntityType != "episode_export" {
		t.Fatalf("the finding is about a %s, want the export", finding.EntityType)
	}
	if !strings.Contains(finding.Problem, "2") {
		t.Fatalf("the finding does not name the shot: %q", finding.Problem)
	}
	if !finding.Blocker() {
		t.Fatal("an export whose manifest no longer matches is not a blocker")
	}
}

// TestAManifestHashThatDisagreesWithItsVersionIsCritical covers the case the hash exists for: a
// version whose bytes changed under the same identifier.
func TestAManifestHashThatDisagreesWithItsVersionIsCritical(t *testing.T) {
	facts := healthyEpisode()
	facts.Export.ManifestHashes["shot:shot-1"] = "hash-something-else"
	findings := runFinal(t, facts)
	finding, ok := findFinding(findings, RuleExportTraceable)
	if !ok {
		t.Fatal("a manifest hash that disagrees with its version produced no finding")
	}
	if finding.Severity != consistency.SeverityCritical {
		t.Fatalf("the finding is %s, want critical: a hash that disagrees means the document or the file was written by something else", finding.Severity)
	}
}

// TestAManifestForAnotherEpisodeIsReported covers the third traceability case.
func TestAManifestForAnotherEpisodeIsReported(t *testing.T) {
	facts := healthyEpisode()
	facts.Export.ManifestEpisodeID = "episode-2"
	finding, ok := findFinding(runFinal(t, facts), RuleExportTraceable)
	if !ok {
		t.Fatal("a manifest naming another episode produced no finding")
	}
	if !finding.Blocker() {
		t.Fatal("a manifest that describes another film is not a blocker")
	}
	if !strings.Contains(finding.Problem, "different episode") {
		t.Fatalf("the finding does not say what is wrong: %q", finding.Problem)
	}
}

// TestAnUnapprovedBoardIsNotAnError covers the composition case: an episode with no approved board is
// a state the RULES report, not a read that fails, because a failure would surface to a user as a
// storage fault rather than as "there is nothing to export yet".
func TestAnUnapprovedBoardIsNotAnError(t *testing.T) {
	facts := healthyEpisode()
	facts.BoardVersionID = ""
	facts.Shots = nil
	facts.Export = nil
	findings := runFinal(t, facts)
	// What it must produce is the traceability remark about no export, and NOT a missing-media
	// finding about every shot of a board that does not exist.
	if _, ok := findFinding(findings, RuleExportTraceable); !ok {
		t.Fatalf("an episode with no board and no export produced no finding at all: %+v", findings)
	}
	for _, finding := range findings {
		if finding.Rule == RuleShotMedia {
			t.Fatalf("a board that does not exist produced a missing-media finding: %+v", finding)
		}
	}
}

// TestAnUnattachedRulesetRefuses covers the composition guard: a ruleset with no reader must refuse
// rather than report a clean episode, which would be an export passing on nothing.
func TestAnUnattachedRulesetRefuses(t *testing.T) {
	ruleset := NewFinalRuleset(FinalOptions{})
	if ruleset.Available() {
		t.Fatal("a ruleset with no reader reports itself available")
	}
	if _, err := ruleset.CheckEpisode(context.Background(), "episode-1"); err == nil {
		t.Fatal("a ruleset with no reader produced a report")
	}
	// An empty episode identifier is refused rather than defaulted: a report about no episode is a
	// report nobody can act on.
	attached := NewFinalRuleset(FinalOptions{Reader: &finalReader{}})
	if _, err := attached.CheckEpisode(context.Background(), "   "); err == nil {
		t.Fatal("a ruleset read an episode with no identifier")
	}
}

// TestAReadFailureIsReportedRatherThanSwallowed covers the choice this ruleset does NOT make: a
// caller that cannot read the episode's state may continue without the deterministic half or stop,
// and that is the caller's decision.
func TestAReadFailureIsReportedRatherThanSwallowed(t *testing.T) {
	ruleset := NewFinalRuleset(FinalOptions{Reader: &finalReader{err: errors.New("the store is gone")}})
	if _, err := ruleset.CheckEpisode(context.Background(), "episode-1"); err == nil {
		t.Fatal("a failing read reported a clean report")
	}
}

// TestTheStagePortIsTheSameRuleSet pins the two entry points the stage machine uses.
func TestTheStagePortIsTheSameRuleSet(t *testing.T) {
	facts := healthyEpisode()
	facts.Shots[0].MediaVersionID = ""
	facts.Shots[0].MediaHash = ""
	reader := &finalReader{facts: facts}
	ruleset := NewFinalRuleset(FinalOptions{Reader: reader})
	// The port's signature takes the artifact version, which for this stage is the episode. Both
	// entry points must produce the same findings, or a stage would be reviewed differently
	// depending on which one the pipeline called.
	direct, err := ruleset.CheckEpisode(context.Background(), "episode-1")
	if err != nil {
		t.Fatal(err)
	}
	throughPort, err := ruleset.CheckFinalVersion(context.Background(), "episode-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(direct) != len(throughPort) {
		t.Fatalf("the two entry points produced %d and %d findings", len(direct), len(throughPort))
	}
	if len(direct) == 0 {
		t.Fatal("the fixture produced no findings, so this comparison proves nothing")
	}
}

// findFinding returns the first finding with one rule.
func findFinding(findings []consistency.Finding, rule string) (consistency.Finding, bool) {
	for _, finding := range findings {
		if finding.Rule == rule {
			return finding, true
		}
	}
	return consistency.Finding{}, false
}

// blockersOf filters to the findings that must prevent a pass.
func blockersOf(findings []consistency.Finding) []consistency.Finding {
	blocking := []consistency.Finding{}
	for _, finding := range findings {
		if finding.Blocker() {
			blocking = append(blocking, finding)
		}
	}
	return blocking
}

// findingsOf runs the rules for a case whose assertion failed while BUILDING the message, which is
// how a failure inside a Fatalf argument reaches a helper.
func findingsOf(facts FinalFacts, t *testing.T) []consistency.Finding {
	t.Helper()
	ruleset := NewFinalRuleset(FinalOptions{Reader: &finalReader{facts: facts}})
	findings, err := ruleset.CheckEpisode(context.Background(), "episode-1")
	if err != nil {
		return nil
	}
	return findings
}
