package media

import (
	"strings"
	"testing"
)

// These are pure functions, so each is pinned exactly rather than observed through a store. The
// formats matter here in a way an application test could not show: an SRT's comma and a VTT's dot
// are the same instant written two ways, and a player that read `00:00:01.500` in an SRT file would
// show the subtitle at the wrong time.

func TestTimecodeFormatsBothWays(t *testing.T) {
	cases := []struct {
		ms  Timecode
		srt string
		vtt string
	}{
		{0, "00:00:00,000", "00:00:00.000"},
		{1, "00:00:00,001", "00:00:00.001"},
		{999, "00:00:00,999", "00:00:00.999"},
		{1000, "00:00:01,000", "00:00:01.000"},
		// The minute and hour boundaries, where an off-by-one in the modulo arithmetic shows up.
		{59999, "00:00:59,999", "00:00:59.999"},
		{60000, "00:01:00,000", "00:01:00.000"},
		{3599999, "00:59:59,999", "00:59:59.999"},
		{3600000, "01:00:00,000", "01:00:00.000"},
		{3661500, "01:01:01,500", "01:01:01.500"},
		// Ninety-nine hours is still two-digit hours; a hundred is three, which the format allows
		// and some players read wrongly if the field is short.
		{86399999, "23:59:59,999", "23:59:59.999"},
	}
	for _, testCase := range cases {
		if got := testCase.ms.FormatSRT(); got != testCase.srt {
			t.Fatalf("%dms renders as SRT %q, want %q", testCase.ms, got, testCase.srt)
		}
		if got := testCase.ms.FormatVTT(); got != testCase.vtt {
			t.Fatalf("%dms renders as VTT %q, want %q", testCase.ms, got, testCase.vtt)
		}
		// And each rendering parses back to the same instant, which is what makes the two formats
		// interchangeable rather than merely similar.
		if back, err := ParseTimecode(testCase.srt); err != nil || back != testCase.ms {
			t.Fatalf("SRT %q parsed to %d, %v", testCase.srt, back, err)
		}
		if back, err := ParseTimecode(testCase.vtt); err != nil || back != testCase.ms {
			t.Fatalf("VTT %q parsed to %d, %v", testCase.vtt, back, err)
		}
	}
}

func TestTimecodeParsesTheShortForms(t *testing.T) {
	// WebVTT permits a leading field to be omitted, and a reader that refused these would refuse a
	// subtitle file this application did not write.
	cases := map[string]Timecode{
		"00:01.500":      1500,
		"01.500":         1500,
		"1:02:03.004":    3723004,
		"  00:00:02,000": 2000, // whitespace is trimmed
		// A fraction shorter than three digits is right-padded: `.5` is five hundred, not five.
		"00:00:00.5":  500,
		"00:00:00.05": 50,
		// A single digit in the whole part is legal.
		"0:0:0.0": 0,
	}
	for input, want := range cases {
		got, err := ParseTimecode(input)
		if err != nil {
			t.Fatalf("%q was refused: %v", input, err)
		}
		if got != want {
			t.Fatalf("%q parsed to %d, want %d", input, got, want)
		}
	}
}

func TestTimecodeRefusesWhatIsNotATime(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"no timecode here",
		// No fractional part: both formats require one, and a value without it is a clock reading
		// rather than a subtitle time.
		"00:00:01",
		// A fraction longer than milliseconds is a precision this format cannot carry.
		"00:00:01.0000",
		"00:00:01,abcd",
		// Sixty minutes is an hour: an SRT that said `00:75:00,000` would be read differently by
		// different players, so it is refused rather than normalised.
		"00:75:00,000",
		"10:00:75,000",
		// Too many fields.
		"1:2:3:4.000",
		"::1.000",
		"1::2.000",
		// Beyond the ceiling.
		"99:00:00,000",
	}
	for _, input := range cases {
		if got, err := ParseTimecode(input); err == nil {
			t.Fatalf("%q was accepted as %d", input, got)
		}
	}
}

func TestParseTimeRangeReadsBothFormats(t *testing.T) {
	start, end, err := ParseTimeRange("00:00:01,000 --> 00:00:02,500")
	if err != nil {
		t.Fatal(err)
	}
	if start != 1000 || end != 2500 {
		t.Fatalf("SRT range parsed to %d..%d", start, end)
	}
	// A VTT line may carry settings after the end time, and the first field is the time.
	start, end, err = ParseTimeRange("00:00:01.000 --> 00:00:02.500 align:start position:10%")
	if err != nil {
		t.Fatal(err)
	}
	if start != 1000 || end != 2500 {
		t.Fatalf("VTT range parsed to %d..%d", start, end)
	}
	// A line with no arrow is not a time range.
	if _, _, err := ParseTimeRange("00:00:01,000 00:00:02,500"); err == nil {
		t.Fatal("a line with no arrow was accepted")
	}
	// Nor is one whose halves are not times.
	if _, _, err := ParseTimeRange("hello --> world"); err == nil {
		t.Fatal("a range of words was accepted")
	}
}

func TestTimecodeDurationStatesTheDirection(t *testing.T) {
	length, err := Timecode(1000).Duration(3500)
	if err != nil {
		t.Fatal(err)
	}
	if length.Milliseconds() != 2500 {
		t.Fatalf("the duration is %v", length)
	}
	// The pair the wrong way round is a REFUSAL rather than a negative length: a caller that wanted
	// a length from two cues should be told the order is wrong, not handed a number it then has to
	// interpret.
	if _, err := Timecode(3500).Duration(1000); err == nil {
		t.Fatal("a backwards range produced a duration")
	}
	// Equal endpoints are the zero-length cue the schema also refuses.
	if _, err := Timecode(1000).Duration(1000); err == nil {
		t.Fatal("a zero-length range produced a duration")
	}
}

// cueFor builds a well-formed cue, so a test states only the field it is about.
func cueFor(ordinal int, start, end Timecode, text string) Cue {
	return Cue{
		ID:      "cue-" + itoa(ordinal),
		TrackID: "track-1",
		Ordinal: ordinal,
		Start:   start,
		End:     end,
		Text:    text,
		Status:  CueGenerated,
	}
}

func TestCueValidateAcceptsTheDocumentedShape(t *testing.T) {
	if err := cueFor(1, 0, 2000, "a line of dialogue").Validate(); err != nil {
		t.Fatalf("a well-formed cue was refused: %v", err)
	}
	// An empty text is legal: a user who deleted the words but kept the timing is mid-edit, and a
	// validator that refused it would make the second half of the edit impossible.
	empty := cueFor(1, 0, 2000, "")
	if err := empty.Validate(); err != nil {
		t.Fatalf("a cue with no text was refused: %v", err)
	}
	// A cue with no source line is legal: a user may add one, and AC-MEDIA-002's editability means
	// the application cannot require every cue to come from the generator.
	manual := cueFor(1, 0, 2000, "spoken by nobody in the script")
	if err := manual.Validate(); err != nil {
		t.Fatalf("a cue with no source line was refused: %v", err)
	}
}

func TestCueValidateRefusesTheBrokenShapes(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Cue)
	}{
		{"no identifier", func(c *Cue) { c.ID = "" }},
		{"no track", func(c *Cue) { c.TrackID = "" }},
		{"ordinal zero", func(c *Cue) { c.Ordinal = 0 }},
		{"a negative start", func(c *Cue) { c.Start = -1 }},
		// The rule the schema's CHECK also carries: a cue with no duration is invisible on screen.
		{"end before start", func(c *Cue) { c.End = c.Start - 1 }},
		{"zero length", func(c *Cue) { c.End = c.Start }},
		{"beyond the ceiling", func(c *Cue) { c.End = MaxTimecodeMS + 1 }},
		{"too much text", func(c *Cue) { c.Text = strings.Repeat("字", MaxCueTextRunes+1) }},
		{"an unknown status", func(c *Cue) { c.Status = "published" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			cue := cueFor(1, 0, 2000, "text")
			testCase.change(&cue)
			if err := cue.Validate(); err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
		})
	}
}

// TestValidateTrackSeesWhatOneCueCannot is the reason it exists as a separate rule.
//
// Each of the three problems is about the relationship between cues: an ordinal gap is invisible
// from one row, and an overlap needs two. A validator that only looked at one cue at a time would
// pass a track whose subtitles were on screen in the wrong order.
func TestValidateTrackSeesWhatOneCueCannot(t *testing.T) {
	good := []Cue{
		cueFor(1, 0, 1000, "one"),
		cueFor(2, 1000, 2000, "two"),
		cueFor(3, 2000, 3000, "three"),
	}
	if issues := ValidateTrack(good); len(issues) != 0 {
		t.Fatalf("a well-formed track reported %+v", issues)
	}
	// An EMPTY track is legal: it is what generating from a script with no dialogue produces.
	if issues := ValidateTrack(nil); len(issues) != 0 {
		t.Fatalf("an empty track reported %+v", issues)
	}
	// A gap in the ordinals.
	gapped := []Cue{cueFor(1, 0, 1000, "one"), cueFor(3, 1000, 2000, "three")}
	issues := ValidateTrack(gapped)
	if len(issues) != 1 || !strings.Contains(issues[0].Problem, "ordinal") {
		t.Fatalf("an ordinal gap reported %+v", issues)
	}
	// An overlap: two cues on screen at once, which a player renders as one replacing the other.
	overlapping := []Cue{cueFor(1, 0, 2000, "one"), cueFor(2, 1500, 3000, "two")}
	issues = ValidateTrack(overlapping)
	if len(issues) != 1 || !strings.Contains(issues[0].Problem, "together") {
		t.Fatalf("an overlap reported %+v", issues)
	}
	// A cue that ends before it starts, which the per-cue rule also catches — reported here too,
	// because a track validated as a unit must not depend on every caller validating each cue first.
	broken := []Cue{cueFor(1, 2000, 1000, "one")}
	issues = ValidateTrack(broken)
	if len(issues) != 1 || !strings.Contains(issues[0].Problem, "ends before it starts") {
		t.Fatalf("a backwards cue reported %+v", issues)
	}
	// EVERY problem is reported, not the first: a user fixing subtitles wants the list.
	multiple := []Cue{cueFor(1, 0, 2000, "one"), cueFor(3, 1500, 3000, "three")}
	if issues := ValidateTrack(multiple); len(issues) != 2 {
		t.Fatalf("a track with a gap and an overlap reported %d issues, want 2", len(issues))
	}
}

// TestMissingLinesDetectsUnsubtitledSpeech is AC-MEDIA-002's "missing line detected".
func TestMissingLinesDetectsUnsubtitledSpeech(t *testing.T) {
	lines := []SpokenLine{
		{LineID: "line-1", Type: "dialogue", CharacterEntityID: "char-1", Text: "spoken"},
		{LineID: "line-2", Type: "action", Text: "a direction, not speech"},
		{LineID: "line-3", Type: "narration", Text: "spoken over the picture"},
		{LineID: "line-4", Type: "transition", Text: "CUT TO"},
		{LineID: "line-5", Type: "note", Text: "a note"},
		{LineID: "line-6", Type: "dialogue", CharacterEntityID: "char-2", Text: "also spoken"},
	}
	// A cue for the first and the last, so the middle of the spoken ones is what is missing.
	cues := []Cue{
		cueFor(1, 0, 1000, "spoken"),
		cueFor(2, 1000, 2000, "also spoken"),
	}
	cues[0].DialogueLineID = "line-1"
	cues[1].DialogueLineID = "line-6"
	missing := MissingLines(lines, cues)
	if len(missing) != 1 || missing[0].LineID != "line-3" {
		t.Fatalf("missing lines are %+v, want only the narration", missing)
	}
	// The NON-spoken types are never reported, which is the rule the whole function turns on: an
	// action line's absence from a subtitle file is correct rather than missing.
	for _, line := range missing {
		if !IsSpoken(line.Type) {
			t.Fatalf("a %s line was reported as missing", line.Type)
		}
	}
	// A track covering every spoken line reports nothing.
	all := []Cue{
		cueFor(1, 0, 1000, "a"),
		cueFor(2, 1000, 2000, "b"),
	}
	all[0].DialogueLineID = "line-1"
	all[1].DialogueLineID = "line-3"
	if missing := MissingLines(lines, all); len(missing) != 1 || missing[0].LineID != "line-6" {
		t.Fatalf("a partially covered track reported %+v", missing)
	}
	// And a cue with NO source line does not cover anything: a user's own cue is not a rendering of a
	// script line, so its presence must not make a line look subtitled.
	manual := []Cue{cueFor(1, 0, 1000, "typed by hand")}
	// THREE spoken lines are in the fixture — two dialogue and one narration — and a cue with no
	// source line covers none of them, so all three come back. The first version of this assertion
	// said two, which was a miscount of the fixture rather than a property of the code.
	if missing := MissingLines(lines, manual); len(missing) != 3 {
		t.Fatalf("a hand-written cue left %d spoken lines uncovered, want 3", len(missing))
	}
}

// TestRenderSRTAndVTTAreWellFormed checks the two documents against the formats' own rules.
func TestRenderSRTAndVTTAreWellFormed(t *testing.T) {
	cues := []Cue{
		cueFor(1, 0, 1500, "first line"),
		cueFor(2, 1500, 3000, "second line"),
	}
	srt := RenderSRT(cues)
	// An SRT numbers its cues from one and separates them with a blank line.
	if !strings.HasPrefix(srt, "1\n00:00:00,000 --> 00:00:01,500\nfirst line\n\n") {
		t.Fatalf("the SRT header is wrong:\n%s", srt)
	}
	if !strings.Contains(srt, "2\n00:00:01,500 --> 00:00:03,000\nsecond line\n") {
		t.Fatalf("the second cue is wrong:\n%s", srt)
	}
	// A trailing blank line means the last cue's block is complete, which is what every reader wants.
	if !strings.HasSuffix(srt, "\n\n") {
		t.Fatalf("the SRT does not end its last cue:\n%q", srt)
	}
	// And the numbering is the POSITION rather than the stored ordinal: a track with a gap would
	// otherwise write an SRT whose cues are numbered 1 and 3, which readers handle inconsistently.
	gapped := []Cue{cueFor(1, 0, 1000, "a"), cueFor(3, 1000, 2000, "b")}
	if !strings.Contains(RenderSRT(gapped), "2\n") {
		t.Fatalf("the SRT numbering follows the ordinals rather than the positions:\n%s", RenderSRT(gapped))
	}

	vtt := RenderVTT(cues)
	if !strings.HasPrefix(vtt, "WEBVTT\n\n") {
		t.Fatalf("the VTT signature is missing:\n%q", vtt)
	}
	// The dot separator, which is the one thing a VTT timecode must not share with an SRT.
	if strings.Contains(vtt, ",") {
		t.Fatalf("the VTT contains an SRT separator:\n%s", vtt)
	}
	if !strings.Contains(vtt, "00:00:00.000 --> 00:00:01.500") {
		t.Fatalf("the VTT timecodes are wrong:\n%s", vtt)
	}
}

// TestRenderRefusesAnUnknownFormat keeps the two formats a closed set.
func TestRenderRefusesAnUnknownFormat(t *testing.T) {
	if _, err := Render(nil, "ass"); err == nil {
		t.Fatal("an unknown format was rendered")
	}
	// Both documented formats render, so the refusal above is not a function that never works.
	for _, format := range SubtitleFormats {
		if _, err := Render(nil, format); err != nil {
			t.Fatalf("the documented format %q was refused: %v", format, err)
		}
		if !IsValidSubtitleFormat(format) {
			t.Fatalf("the documented format %q reports itself invalid", format)
		}
	}
	// The MIME and extension each format carries.
	if FormatVTT.MIMEFor() != "text/vtt" || FormatVTT.ExtensionFor() != "vtt" {
		t.Fatalf("the VTT's type is %q/%q", FormatVTT.MIMEFor(), FormatVTT.ExtensionFor())
	}
	if FormatSRT.ExtensionFor() != "srt" {
		t.Fatalf("the SRT's extension is %q", FormatSRT.ExtensionFor())
	}
}

// TestTheSRTRoundTripsThroughItsOwnParser is the strongest single assertion about the format code.
//
// A document that renders and does not parse back is a document a player reads differently from the
// editor that wrote it — and this build's own editor is the parser.
func TestTheSRTRoundTripsThroughItsOwnParser(t *testing.T) {
	cues := []Cue{
		cueFor(1, 0, 1500, "first line"),
		cueFor(2, 1500, 3000, "second line"),
		cueFor(3, 60000, 61500, "a line after a minute"),
	}
	document := RenderSRT(cues)
	parsed := parseSRT(t, document)
	if len(parsed) != len(cues) {
		t.Fatalf("the SRT round tripped to %d cues", len(parsed))
	}
	for index, want := range cues {
		if parsed[index].Start != want.Start || parsed[index].End != want.End {
			t.Fatalf("cue %d round tripped to %d..%d, want %d..%d",
				index, parsed[index].Start, parsed[index].End, want.Start, want.End)
		}
		if parsed[index].Text != want.Text {
			t.Fatalf("cue %d's text round tripped to %q", index, parsed[index].Text)
		}
	}
}

// parseSRT reads a SubRip document the way a player does: blocks separated by blank lines, a
// numbered first line, a time range, then text.
func parseSRT(t *testing.T, document string) []Cue {
	t.Helper()
	cues := []Cue{}
	for _, block := range strings.Split(strings.TrimSpace(document), "\n\n") {
		lines := strings.Split(block, "\n")
		if len(lines) < 3 {
			t.Fatalf("a block has %d lines, want at least three:\n%s", len(lines), block)
		}
		// The first line is the cue's number, which a player uses for nothing but display.
		if lines[0] != itoa(len(cues)+1) {
			t.Fatalf("block %d is numbered %q", len(cues)+1, lines[0])
		}
		start, end, err := ParseTimeRange(lines[1])
		if err != nil {
			t.Fatalf("block %d's time line: %v", len(cues)+1, err)
		}
		cues = append(cues, Cue{
			Ordinal: len(cues) + 1,
			Start:   start,
			End:     end,
			Text:    strings.Join(lines[2:], "\n"),
		})
	}
	return cues
}
