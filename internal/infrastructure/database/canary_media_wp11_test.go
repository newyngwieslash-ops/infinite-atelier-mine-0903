package database

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// canary_media_wp11_test.go grades the GENERATED media fixture against the real service.
//
// # Why the fixture needed a reader of its own
//
// `gen-canary-fixture.mjs` is JavaScript, so it cannot call the domain's `IsSpoken` rule or the
// service's cue arithmetic: it applies neither, and that is deliberate. The fixture states the INPUT
// (which lines exist, what they say, which one is a direction) and the INVARIANTS the output must
// satisfy, and this file runs the same lines through `SubtitleService.Draft` and asserts them.
//
// # Why the fixture does not state precomputed cue times
//
// An earlier version of it did, and it was wrong: the service distributes a scene's duration across
// its lines by TEXT LENGTH, so the generator's "one cue per shot, each the shot's own duration" agreed
// with the algorithm by accident and would have disagreed with it silently after any change. A fixture
// that re-implements the code it grades is a second version of the algorithm, and the two can only
// drift. So the fixture states what must be TRUE of any correct track — one cue per spoken line, in
// script order, starting at zero, tiling the duration with no gap — and this test checks those.

// canaryMediaFixture is the generated document `testdata/canary-drama/expected-media.json`.
//
// It is decoded into typed structs rather than read as a `map[string]any`, because the assertions are
// about numbers and ordinals: an untyped reader would compare a `float64` with an `int` and a changed
// shape would surface as a confusing failure rather than as "the fixture is not what this test reads".
type canaryMediaFixture struct {
	ShotCount     int `json:"shotCount"`
	ShotDurationM int `json:"shotDurationMs"`
	TotalDuration int `json:"totalDurationMs"`
	// SpokenLines are what the canary's script says, in script order, with the type that decides
	// whether a person hears them.
	SpokenLines []struct {
		Ordinal int    `json:"ordinal"`
		ShotID  string `json:"shotId"`
		Type    string `json:"type"`
		Speaker string `json:"speaker"`
		Text    string `json:"text"`
	} `json:"spokenLines"`
	SubtitleTrack struct {
		VersionNumber int  `json:"versionNumber"`
		ExpectedCount int  `json:"expectedCueCount"`
		CountIsSpoken bool `json:"countMustEqualTheSpokenLines"`
		StartsAtZero  bool `json:"mustStartAtZero"`
		TilesDuration bool `json:"mustCoverTheTotalDuration"`
	} `json:"subtitleTrack"`
	IncompleteTrackScenario struct {
		RemoveCueOrdinal int `json:"removeCueOrdinal"`
		// LineWithoutACueOrdinal is the SCRIPT ordinal of the line that then has no cue. An ordinal
		// rather than a count: a track with the right NUMBER of cues and a different six would satisfy
		// any assertion about length.
		LineWithoutACueOrdinal     int `json:"lineWithoutACueOrdinal"`
		FirstCueAfterTheGapOrdinal int `json:"firstCueAfterTheGapOrdinal"`
	} `json:"incompleteTrackScenario"`
	DirectionLineOrdinals []int `json:"directionLineOrdinals"`
	Faults                []struct {
		What string `json:"what"`
		Rule string `json:"rule"`
	} `json:"faults"`
}

// loadCanaryMedia reads the generated fixture and REFUSES one that is unusable, so a fixture that
// changed shape is a failure rather than a test that silently asserts nothing.
func loadCanaryMedia(t *testing.T) canaryMediaFixture {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "canary-drama", "expected-media.json"))
	if err != nil {
		t.Fatalf("reading the canary media fixture: %v", err)
	}
	var fixture canaryMediaFixture
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("parsing the canary media fixture: %v", err)
	}
	if fixture.ShotCount == 0 || len(fixture.SpokenLines) == 0 || fixture.TotalDuration == 0 {
		t.Fatal("the canary media fixture is empty, so every assertion below would be vacuous")
	}
	// The scenario has to be USABLE, which means its ordinals have to be ones a track of the expected
	// size has. A fixture naming a cue that does not exist would make every assertion about the gap
	// vacuous.
	scenario := fixture.IncompleteTrackScenario
	if scenario.RemoveCueOrdinal < 1 || scenario.RemoveCueOrdinal > fixture.SubtitleTrack.ExpectedCount {
		t.Fatalf("the scenario removes cue %d, which a track of %d does not have",
			scenario.RemoveCueOrdinal, fixture.SubtitleTrack.ExpectedCount)
	}
	if scenario.LineWithoutACueOrdinal == 0 {
		t.Fatal("the scenario names no line, so the missing-line assertion would be vacuous")
	}
	if len(fixture.DirectionLineOrdinals) == 0 {
		t.Fatal("the fixture names no direction line, so nothing shows that directions are excluded")
	}
	// The fixture has to SAY what is wrong with it, which is the rule the earlier packages' fixtures
	// established: a fixture whose fault is implicit cannot be checked, because a passing test proves
	// the file was read rather than that the claim about it was true.
	if len(fixture.Faults) == 0 {
		t.Fatal("the canary media fixture names no deliberate fault, so it is not a canary")
	}
	return fixture
}

// canaryLines renders the fixture's lines in the shape the service reads.
func canaryLines(fixture canaryMediaFixture) []domainmedia.SpokenLine {
	lines := make([]domainmedia.SpokenLine, 0, len(fixture.SpokenLines))
	for _, line := range fixture.SpokenLines {
		lines = append(lines, domainmedia.SpokenLine{
			LineID: canaryLineID(line.Ordinal),
			Type:   line.Type,
			Text:   line.Text,
			// The speaker travels as the character, which is what AC-MEDIA-002's "audio linked to
			// character/line" reads on the cue.
			CharacterEntityID: speakerEntity(line.Speaker),
		})
	}
	return lines
}

// canaryLineID is the identifier this test gives a fixture line, so a missing-line assertion can name
// the one the scenario leaves uncovered.
func canaryLineID(ordinal int) string { return "canary-line-" + itoaWP10(ordinal) }

// isSpokenLineType restates the domain's rule HERE, which is what makes it an assertion rather than a
// second implementation: the fixture's expected count is derived from this, so a change to
// `domainmedia.IsSpoken` that this file does not make would fail the count below.
func isSpokenLineType(lineType string) bool {
	return lineType == "dialogue" || lineType == "narration"
}

// TestTheCanarySubtitleTrackSatisfiesItsInvariants is the fixture's main reader.
func TestTheCanarySubtitleTrackSatisfiesItsInvariants(t *testing.T) {
	fixture := loadCanaryMedia(t)
	harness := newSubtitleHarness(t, lineReaderDouble{
		lines: canaryLines(fixture), durationMS: fixture.TotalDuration,
	})
	track, cues := harness.draft(t)

	if track.VersionNumber != fixture.SubtitleTrack.VersionNumber {
		t.Fatalf("the draft is version %d, the fixture says %d",
			track.VersionNumber, fixture.SubtitleTrack.VersionNumber)
	}

	// ONE CUE PER SPOKEN LINE. The expected count is derived here from the domain's OWN rule applied
	// to the fixture's lines, so the fixture's number and the code's rule are compared rather than
	// both being trusted.
	spoken := make([]domainmedia.SpokenLine, 0, len(fixture.SpokenLines))
	for _, line := range canaryLines(fixture) {
		if domainmedia.IsSpoken(line.Type) {
			spoken = append(spoken, line)
		}
	}
	if len(spoken) != fixture.SubtitleTrack.ExpectedCount {
		t.Fatalf("the fixture says %d cues and this file's reading of its own lines finds %d",
			fixture.SubtitleTrack.ExpectedCount, len(spoken))
	}
	if len(cues) != len(spoken) {
		t.Fatalf("the draft has %d cues, want one per spoken line (%d)", len(cues), len(spoken))
	}

	// IN SCRIPT ORDER, each cue carrying the line it renders. This is what a subtitle is FOR: a cue
	// whose text belongs to another line places the wrong words at the wrong time.
	for index, cue := range cues {
		if cue.DialogueLineID != spoken[index].LineID {
			t.Fatalf("cue %d cites %q, want the spoken line %q", index, cue.DialogueLineID, spoken[index].LineID)
		}
		if cue.Ordinal != index+1 {
			t.Fatalf("cue %d carries ordinal %d", index, cue.Ordinal)
		}
		if strings.TrimSpace(cue.Text) == "" {
			t.Fatalf("cue %d carries no text", index)
		}
	}

	// THE CUES TILE THE DURATION: the first starts at zero, each follows the one before with no gap,
	// and the last ends at the total. A track with a hole in it is a passage where a viewer reads
	// nothing and hears something.
	if len(cues) > 0 {
		if cues[0].Start != 0 {
			t.Fatalf("the first cue starts at %dms, want zero", cues[0].Start)
		}
		for index := 1; index < len(cues); index++ {
			if cues[index].Start != cues[index-1].End {
				t.Fatalf("cue %d starts at %dms and the one before ends at %dms, so the track has a gap or an overlap",
					index, cues[index].Start, cues[index-1].End)
			}
		}
		if int(cues[len(cues)-1].End) != fixture.TotalDuration {
			t.Fatalf("the last cue ends at %dms, want the script's %dms", cues[len(cues)-1].End, fixture.TotalDuration)
		}
	}

	// NO DIRECTION BECOMES A CUE. The fixture names the direction ordinals, so this asserts the RULE
	// rather than a count: an action line is a note to the production and nobody hears it.
	for _, ordinal := range fixture.DirectionLineOrdinals {
		var text string
		for _, line := range fixture.SpokenLines {
			if line.Ordinal == ordinal {
				text = line.Text
			}
		}
		if text == "" {
			t.Fatalf("the fixture names ordinal %d as a direction and carries no such line", ordinal)
		}
		for _, cue := range cues {
			if strings.Contains(cue.Text, text) {
				t.Fatalf("a direction at ordinal %d became a cue: %q", ordinal, text)
			}
		}
	}
}

// TestTheCanaryTrackIsMissingExactlyTheLineTheScenarioNames grades the fixture's scenario.
//
// AC-MEDIA-002's "missing line detected". The fixture's track is COMPLETE — the service drafts every
// spoken line — so the incomplete state is produced here by applying the fixture's own act: remove the
// cue its scenario names, save the rest, and assert `Missing` finds the line that then has no cue, and
// no other.
//
// An earlier version of this test asserted that the fixture's track WAS incomplete, which was false:
// a fixture cannot claim a service produces a broken track, because it does not. What it states is the
// ACT that produces the gap.
func TestTheCanaryTrackIsMissingExactlyTheLineTheScenarioNames(t *testing.T) {
	fixture := loadCanaryMedia(t)
	ctx := context.Background()
	scenario := fixture.IncompleteTrackScenario
	harness := newSubtitleHarness(t, lineReaderDouble{
		lines: canaryLines(fixture), durationMS: fixture.TotalDuration,
	})
	track, cues := harness.draft(t)

	// The act: every cue the fixture keeps, with the named one dropped.
	kept := make([]appmedia.CueEdit, 0, len(cues))
	for _, cue := range cues {
		if cue.Ordinal == scenario.RemoveCueOrdinal {
			continue
		}
		kept = append(kept, appmedia.CueEdit{
			StartMS: int64(cue.Start), EndMS: int64(cue.End), Text: cue.Text,
			DialogueLineID: cue.DialogueLineID, CharacterEntityID: cue.CharacterEntityID,
		})
	}
	if len(kept) != len(cues)-1 {
		t.Fatalf("the act removed %d cues, want exactly one", len(cues)-len(kept))
	}
	if _, err := harness.service.Edit(ctx, appmedia.EditRequest{TrackID: track.ID, Cues: kept}); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	wantID := canaryLineID(scenario.LineWithoutACueOrdinal)
	missing, err := harness.service.Missing(ctx, appmedia.MissingLinesRequest{TrackID: track.ID})
	if err != nil {
		t.Fatalf("Missing: %v", err)
	}
	if len(missing) != 1 {
		t.Fatalf("the track is missing %d lines, want exactly the one the scenario leaves uncovered: %+v",
			len(missing), missing)
	}
	if missing[0].LineID != wantID {
		t.Fatalf("the missing line is %q, want %q (script ordinal %d)",
			missing[0].LineID, wantID, scenario.LineWithoutACueOrdinal)
	}
	// The line it names is a SPOKEN one, which is what makes the finding worth reporting.
	if !isSpokenLineType(missing[0].Type) {
		t.Fatalf("the missing line is a %s, which nobody hears", missing[0].Type)
	}

	// And the COMPLETE track reports NOTHING, which is the direction that says `Missing` reads the
	// track rather than always naming the last line it saw. This is the fixture's own track.
	complete := newSubtitleHarness(t, lineReaderDouble{
		lines: canaryLines(fixture), durationMS: fixture.TotalDuration,
	})
	completeTrack, _ := complete.draft(t)
	remaining, err := complete.service.Missing(ctx, appmedia.MissingLinesRequest{TrackID: completeTrack.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("the fixture's own track reports %d missing lines, so the gap is not the act's doing: %+v",
			len(remaining), remaining)
	}
}

// TestTheCanarySubtitleRendersAValidDocument grades the track through the renderer, which is
// AC-MEDIA-002's "SRT/VTT valid" against the canary rather than against a synthetic list.
//
// The document is PARSED BACK rather than compared with a string: a renderer that wrote a well-formed
// looking file with the wrong millisecond separator would pass a text comparison against its own
// output and fail this.
func TestTheCanarySubtitleRendersAValidDocument(t *testing.T) {
	fixture := loadCanaryMedia(t)
	harness := newSubtitleHarness(t, lineReaderDouble{
		lines: canaryLines(fixture), durationMS: fixture.TotalDuration,
	})
	track, drafted := harness.draft(t)

	for _, format := range []domainmedia.SubtitleFormat{domainmedia.FormatSRT, domainmedia.FormatVTT} {
		document, err := harness.service.Export(context.Background(), appmedia.SubtitleExportRequest{
			TrackID: track.ID, Format: format,
		})
		if err != nil {
			t.Fatalf("rendering %s: %v", format, err)
		}
		// The document is parsed by the SAME parsers the SRT/VTT criterion's own test uses, rather than
		// by a third one written here: a reader local to this file could share a mistake with the
		// renderer, and the two existing helpers are the ones the format rules are graded against.
		var cues []domainmedia.Cue
		switch format {
		case domainmedia.FormatSRT:
			cues = parseSRTForTest(t, document)
		case domainmedia.FormatVTT:
			cues = parseVTTForTest(t, document)
		default:
			t.Fatalf("no parser for %s", format)
		}
		if len(cues) != len(drafted) {
			t.Fatalf("the %s document carries %d cues against the track's %d", format, len(cues), len(drafted))
		}
		// The times and the text survive the round trip, which is what a format's commas and dots are
		// for. Comparing against the DRAFT rather than against a fixture value is the point: the two
		// sides are the renderer and the parser, and a fixture in between would add a third.
		for index := range drafted {
			if cues[index].Start != drafted[index].Start || cues[index].End != drafted[index].End {
				t.Fatalf("the %s document's cue %d spans %d..%dms against the track's %d..%dms",
					format, index, cues[index].Start, cues[index].End, drafted[index].Start, drafted[index].End)
			}
			if strings.TrimSpace(cues[index].Text) != strings.TrimSpace(drafted[index].Text) {
				t.Fatalf("the %s document's cue %d reads %q against the track's %q",
					format, index, cues[index].Text, drafted[index].Text)
			}
		}
	}
}

// speakerEntity maps a fixture speaker name to the entity a cue cites.
//
// An empty speaker is a NARRATION, which has nobody to attribute: DOMAIN_MODEL section 7.7 gives a
// narration line no character, and a cue naming one would attribute the author's voice to a character
// who never said it.
func speakerEntity(speaker string) string {
	if strings.TrimSpace(speaker) == "" {
		return ""
	}
	return "character-" + speaker
}
