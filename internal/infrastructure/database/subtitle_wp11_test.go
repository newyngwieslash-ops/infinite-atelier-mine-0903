package database

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// subtitle_wp11_test.go drives the real subtitle service over the real schema.
//
// # Why the fixture is the whole stack rather than a double
//
// AC-MEDIA-002's clauses are about relations between stored rows — a cue round trips through the
// milliard columns, an edit that reads back is the edit that was written, a missing line is a join —
// and WP-10's review is the evidence for what a service-only suite misses: three defects survived
// there because nothing drove the service the way the composition root does.

// lineReaderDouble answers the two facts the generator needs, so a test states its own script.
type lineReaderDouble struct {
	lines      []domainmedia.SpokenLine
	durationMS int
	err        error
}

func (d lineReaderDouble) Lines(context.Context, string) ([]domainmedia.SpokenLine, int, error) {
	if d.err != nil {
		return nil, 0, d.err
	}
	return d.lines, d.durationMS, nil
}

// subtitleHarness is the service over a migrated database.
type subtitleHarness struct {
	service *appmedia.SubtitleService
	tracks  *SubtitleRepository
	db      *sql.DB
	reader  lineReaderDouble
	next    int
	now     time.Time
}

func newSubtitleHarness(t *testing.T, reader lineReaderDouble) *subtitleHarness {
	t.Helper()
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	tracks := NewSubtitleRepository(db)
	harness := &subtitleHarness{
		tracks: tracks, db: db, reader: reader,
		now: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	harness.service = appmedia.NewSubtitleService(appmedia.SubtitleOptions{
		Tracks: harness.tracks, Lines: reader,
		Clock: subtitleClock{at: harness.now}, IDs: harness,
	})
	return harness
}

// New mints identifiers, so the harness satisfies the generator port.
func (h *subtitleHarness) New() (string, error) {
	h.next++
	return "subtitle-id-" + itoaWP10(h.next), nil
}

type subtitleClock struct{ at time.Time }

func (c subtitleClock) Now() time.Time { return c.at }

// draft generates a track from the harness's lines.
func (h *subtitleHarness) draft(t *testing.T) (domainmedia.Track, []domainmedia.Cue) {
	t.Helper()
	track, cues, err := h.service.Draft(context.Background(), appmedia.DraftRequest{
		EpisodeID: "drama-episode", ScriptVersionID: "drama-script-version",
		CreatedByType: "user", CreatedByID: "user-1",
	})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	return track, cues
}

// TestACMEDIA002ADraftPlacesSpokenLinesAndSkipsDirections is AC-MEDIA-002's first half.
//
//   - dialogue line → voice job；
//   - audio linked to character/line；
//   - subtitle editable；
//   - SRT/VTT valid；
//   - missing line detected。
//
// This test grades WHICH lines become cues. The distinction is DOMAIN_MODEL section 7.7's LineType: a
// dialogue or narration line is something a person hears, and an action, transition or note line is a
// direction to the production. A subtitle for one of those would be a caption nobody can hear.
func TestACMEDIA002ADraftPlacesSpokenLinesAndSkipsDirections(t *testing.T) {
	harness := newSubtitleHarness(t, lineReaderDouble{
		durationMS: 12000,
		lines: []domainmedia.SpokenLine{
			{LineID: "line-1", Type: "dialogue", CharacterEntityID: "char-1", Text: "灯还亮着。"},
			{LineID: "line-2", Type: "action", Text: "沈砚侧身让开一步。"},
			{LineID: "line-3", Type: "narration", Text: "那年的冬天格外长。"},
			{LineID: "line-4", Type: "transition", Text: "CUT TO:"},
			{LineID: "line-5", Type: "note", Text: "此处需要补拍。"},
			{LineID: "line-6", Type: "dialogue", CharacterEntityID: "char-2", Text: "我们走吧。"},
		},
	})
	track, cues := harness.draft(t)
	if track.VersionNumber != 1 {
		t.Fatalf("the first draft is version %d", track.VersionNumber)
	}
	if track.Status != "draft" {
		t.Fatalf("a draft is stored as %q", track.Status)
	}
	// Three cues: the two dialogue lines and the narration. The directions are skipped.
	if len(cues) != 3 {
		t.Fatalf("the draft has %d cues, want 3: %+v", len(cues), cues)
	}
	wantLines := []string{"line-1", "line-3", "line-6"}
	for index, want := range wantLines {
		if cues[index].DialogueLineID != want {
			t.Fatalf("cue %d cites %q, want %q", index+1, cues[index].DialogueLineID, want)
		}
	}
	// The cues run from one with no gap, which is what ValidateTrack also enforces.
	for index, cue := range cues {
		if cue.Ordinal != index+1 {
			t.Fatalf("cue %d has ordinal %d", index+1, cue.Ordinal)
		}
	}
	// The times are ordered and contiguous: each cue starts where the one before it ended, so the
	// draft covers the scene rather than leaving holes.
	if cues[0].Start != 0 {
		t.Fatalf("the first cue starts at %d", cues[0].Start)
	}
	for index := 1; index < len(cues); index++ {
		if cues[index].Start != cues[index-1].End {
			t.Fatalf("cue %d starts at %d but the previous ended at %d", index+1, cues[index].Start, cues[index-1].End)
		}
	}
	// The speaker travels onto the cue, which is what "audio linked to character" needs from the
	// subtitle side: a cue that could not say who was speaking would render differently from the one
	// beside it.
	if cues[0].CharacterEntityID != "char-1" || cues[2].CharacterEntityID != "char-2" {
		t.Fatalf("the speakers were not carried: %+v", cues)
	}
	// And a cue with no speaker is legal — a narration has none — so the field is not required.
	if cues[1].CharacterEntityID != "" {
		t.Fatalf("the narration carries a speaker: %q", cues[1].CharacterEntityID)
	}

	// Everything read back from the database is what was written: the round trip is the assertion,
	// because a cue whose times changed in storage would render at the wrong moment.
	stored, err := harness.service.Cues(context.Background(), track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(cues) {
		t.Fatalf("the track read back %d cues of %d", len(stored), len(cues))
	}
	for index := range cues {
		if stored[index].Start != cues[index].Start || stored[index].End != cues[index].End {
			t.Fatalf("cue %d round tripped to %d..%d, want %d..%d", index+1,
				stored[index].Start, stored[index].End, cues[index].Start, cues[index].End)
		}
		if stored[index].Text != cues[index].Text {
			t.Fatalf("cue %d's text round tripped to %q", index+1, stored[index].Text)
		}
		if stored[index].Status != domainmedia.CueGenerated {
			t.Fatalf("cue %d is stored as %q", index+1, stored[index].Status)
		}
	}
}

// TestACMEDIA002ASubtitleIsEditableAndReadsBack is the "subtitle editable" clause.
func TestACMEDIA002ASubtitleIsEditableAndReadsBack(t *testing.T) {
	harness := newSubtitleHarness(t, lineReaderDouble{
		durationMS: 4000,
		lines: []domainmedia.SpokenLine{
			{LineID: "line-1", Type: "dialogue", Text: "一句台词。"},
		},
	})
	track, cues := harness.draft(t)
	// The user rewrites the first cue's text and timing, and adds a line of their own with no
	// identifier — which is the case the edit's minting exists for.
	edited, err := harness.service.Edit(context.Background(), appmedia.EditRequest{
		TrackID: track.ID,
		Cues: []appmedia.CueEdit{
			{ID: cues[0].ID, StartMS: 500, EndMS: 2500, Text: "改过的台词。",
				CharacterEntityID: "char-1", DialogueLineID: "line-1",
				Status: domainmedia.CueEdited},
			{StartMS: 2500, EndMS: 3800, Text: "用户自己加的一句。"},
		},
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if len(edited) != 2 {
		t.Fatalf("the edit produced %d cues", len(edited))
	}
	// The first cue's identifier SURVIVES an edit, which is what lets a caller track one across
	// changes; the second was minted.
	if edited[0].ID != cues[0].ID {
		t.Fatalf("the edit replaced the cue's identifier: %q", edited[0].ID)
	}
	if edited[1].ID == "" {
		t.Fatal("the edit did not mint an identifier for the added cue")
	}
	if edited[1].DialogueLineID != "" {
		t.Fatalf("a hand-written cue cites a line: %q", edited[1].DialogueLineID)
	}
	// The statuses: the touched cue is marked edited, and the added one too — neither came from a
	// regeneration, so neither may be replaced by one.
	if edited[0].Status != domainmedia.CueEdited || edited[1].Status != domainmedia.CueEdited {
		t.Fatalf("the statuses are %q and %q", edited[0].Status, edited[1].Status)
	}
	// The ordinals are POSITIONS, one to n, so the list the user sees is the list that was stored.
	for index, cue := range edited {
		if cue.Ordinal != index+1 {
			t.Fatalf("the edited cue %d has ordinal %d", index+1, cue.Ordinal)
		}
	}
	// And the read-back is the edit.
	stored, err := harness.service.Cues(context.Background(), track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("the track read back %d cues", len(stored))
	}
	if stored[0].Text != "改过的台词。" || stored[0].Start != 500 || stored[0].End != 2500 {
		t.Fatalf("the edit did not persist: %+v", stored[0])
	}
	if stored[1].Text != "用户自己加的一句。" {
		t.Fatalf("the added cue did not persist: %+v", stored[1])
	}

	// An edit that breaks a rule is refused, and the STORED cues are untouched: a failed edit must not
	// leave the user with neither their old subtitles nor their new ones.
	_, err = harness.service.Edit(context.Background(), appmedia.EditRequest{
		TrackID: track.ID,
		Cues: []appmedia.CueEdit{
			{ID: stored[0].ID, StartMS: 2500, EndMS: 500, Text: "backwards"},
		},
	})
	if err == nil {
		t.Fatal("an edit with a backwards time range was accepted")
	}
	after, err := harness.service.Cues(context.Background(), track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 || after[0].Text != "改过的台词。" {
		t.Fatalf("a refused edit changed the stored cues: %+v", after)
	}
	// A gap in the ordinals cannot be stated either: the service derives them from positions, so a
	// caller's only way to express a hole would be to omit a cue, which renumbers the rest.
	if _, err := harness.service.Edit(context.Background(), appmedia.EditRequest{
		TrackID: track.ID,
		Cues: []appmedia.CueEdit{
			{StartMS: 0, EndMS: 1000, Text: "one"},
			{StartMS: 500, EndMS: 2000, Text: "overlapping"},
		},
	}); err == nil {
		t.Fatal("an edit with overlapping cues was accepted")
	}
}

// TestACMEDIA002SRTAndVTTAreValidAndRoundTrip is the "SRT/VTT valid" clause.
//
// "Valid" is graded by PARSING rather than by looking at the string: a document that renders and does
// not parse back is a document a player reads differently from the editor that wrote it.
func TestACMEDIA002SRTAndVTTAreValidAndRoundTrip(t *testing.T) {
	harness := newSubtitleHarness(t, lineReaderDouble{
		durationMS: 6000,
		lines: []domainmedia.SpokenLine{
			{LineID: "line-1", Type: "dialogue", Text: "第一句。"},
			{LineID: "line-2", Type: "dialogue", Text: "第二句，稍长一些。"},
		},
	})
	track, _ := harness.draft(t)
	ctx := context.Background()

	srt, err := harness.service.Export(ctx, appmedia.SubtitleExportRequest{
		TrackID: track.ID, Format: domainmedia.FormatSRT,
	})
	if err != nil {
		t.Fatalf("the SRT export failed: %v", err)
	}
	// The signature of an SRT: a numbered block, a comma-separated range, the text, then a blank line.
	if !strings.HasPrefix(srt, "1\n") || !strings.Contains(srt, ",") {
		t.Fatalf("the SRT does not look like one:\n%s", srt)
	}
	cues := parseSRTForTest(t, srt)
	if len(cues) != 2 {
		t.Fatalf("the SRT parsed to %d cues", len(cues))
	}
	if cues[0].Text != "第一句。" || cues[1].Text != "第二句，稍长一些。" {
		t.Fatalf("the SRT's text round tripped wrong: %+v", cues)
	}

	vtt, err := harness.service.Export(ctx, appmedia.SubtitleExportRequest{
		TrackID: track.ID, Format: domainmedia.FormatVTT,
	})
	if err != nil {
		t.Fatalf("the VTT export failed: %v", err)
	}
	if !strings.HasPrefix(vtt, "WEBVTT\n\n") {
		t.Fatalf("the VTT signature is missing:\n%q", vtt)
	}
	// The dot separator, which is the one thing a VTT timecode must not share with an SRT.
	if strings.Contains(strings.SplitN(vtt, "\n\n", 2)[1], ",") {
		t.Fatalf("the VTT contains an SRT separator:\n%s", vtt)
	}
	// And the two exports describe the SAME instants, which is what makes them two renditions of one
	// track rather than two tracks.
	vttCues := parseVTTForTest(t, vtt)
	if len(vttCues) != len(cues) {
		t.Fatalf("the VTT has %d cues against the SRT's %d", len(vttCues), len(cues))
	}
	for index := range cues {
		if vttCues[index].Start != cues[index].Start || vttCues[index].End != cues[index].End {
			t.Fatalf("cue %d differs between the formats: %d..%d against %d..%d", index+1,
				cues[index].Start, cues[index].End, vttCues[index].Start, vttCues[index].End)
		}
	}
	// An unknown format is refused rather than silently defaulting.
	if _, err := harness.service.Export(ctx, appmedia.SubtitleExportRequest{TrackID: track.ID, Format: "ass"}); err == nil {
		t.Fatal("an unknown format was exported")
	}
}

// TestACMEDIA002AMissingLineIsDetected is the clause the criterion names last.
//
// The detection is a JOIN — which spoken lines have no cue — and the test proves it by deleting the
// cue for a spoken line while leaving its script line in place.
func TestACMEDIA002AMissingLineIsDetected(t *testing.T) {
	harness := newSubtitleHarness(t, lineReaderDouble{
		durationMS: 8000,
		lines: []domainmedia.SpokenLine{
			{LineID: "line-1", Type: "dialogue", Text: "第一句。"},
			{LineID: "line-2", Type: "narration", Text: "旁白一句。"},
			{LineID: "line-3", Type: "dialogue", Text: "第三句。"},
		},
	})
	track, cues := harness.draft(t)
	ctx := context.Background()

	// A complete track reports nothing missing.
	missing, err := harness.service.Missing(ctx, appmedia.MissingLinesRequest{TrackID: track.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("a complete track reports %+v", missing)
	}
	// The user deletes the middle cue, which is the edit that creates the defect the criterion is
	// about. The line is still spoken; nothing renders it.
	if _, err := harness.service.Edit(ctx, appmedia.EditRequest{
		TrackID: track.ID,
		Cues: []appmedia.CueEdit{
			{ID: cues[0].ID, StartMS: int64(cues[0].Start), EndMS: int64(cues[0].End),
				Text: cues[0].Text, DialogueLineID: cues[0].DialogueLineID},
			{ID: cues[2].ID, StartMS: int64(cues[2].Start), EndMS: int64(cues[2].End),
				Text: cues[2].Text, DialogueLineID: cues[2].DialogueLineID},
		},
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	missing, err = harness.service.Missing(ctx, appmedia.MissingLinesRequest{TrackID: track.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].LineID != "line-2" {
		t.Fatalf("the missing line was not detected: %+v", missing)
	}
	// The detection names the LINE, which is what a user acts on: they know which line of the script
	// has no subtitle rather than only that one is absent.
	if missing[0].Text != "旁白一句。" {
		t.Fatalf("the missing line carries no text: %+v", missing[0])
	}
}

// TestTheSubtitleDraftRefusesWhatItCannotDraft covers the two refusals a caller can reach.
func TestTheSubtitleDraftRefusesWhatItCannotDraft(t *testing.T) {
	harness := newSubtitleHarness(t, lineReaderDouble{durationMS: 1000})
	ctx := context.Background()
	if _, _, err := harness.service.Draft(ctx, appmedia.DraftRequest{ScriptVersionID: "v"}); err == nil {
		t.Fatal("a draft with no episode was accepted")
	}
	if _, _, err := harness.service.Draft(ctx, appmedia.DraftRequest{EpisodeID: "e"}); err == nil {
		t.Fatal("a draft with no script version was accepted")
	}
	// A script whose reader fails reports the failure rather than an empty track: an empty track is a
	// legal subtitle, so swallowing the error would make "the script could not be read" look like "the
	// script has nothing spoken".
	failing := newSubtitleHarness(t, lineReaderDouble{err: domainmedia.NotFoundError()})
	if _, _, err := failing.service.Draft(ctx, appmedia.DraftRequest{
		EpisodeID: "drama-episode", ScriptVersionID: "missing",
	}); err == nil {
		t.Fatal("a failed read produced a draft")
	}
	// A script with nothing spoken produces an EMPTY track rather than an error.
	empty := newSubtitleHarness(t, lineReaderDouble{
		durationMS: 1000,
		lines:      []domainmedia.SpokenLine{{LineID: "line-1", Type: "action", Text: "a direction"}},
	})
	track, cues := empty.draft(t)
	if len(cues) != 0 {
		t.Fatalf("a script with nothing spoken produced %d cues", len(cues))
	}
	// And exporting that empty track is REFUSED, with a message that says which track was empty: an
	// empty subtitle file is legal and useless, and a caller that got one would have no way to tell it
	// from a broken render.
	if _, err := empty.service.Export(ctx, appmedia.SubtitleExportRequest{
		TrackID: track.ID, Format: domainmedia.FormatSRT,
	}); err == nil {
		t.Fatal("an empty track was exported")
	}
}

// TestApprovingATrackSupersedesTheOneBefore is the versioning rule.
func TestApprovingATrackSupersedesTheOneBefore(t *testing.T) {
	harness := newSubtitleHarness(t, lineReaderDouble{
		durationMS: 2000,
		lines:      []domainmedia.SpokenLine{{LineID: "line-1", Type: "dialogue", Text: "一句。"}},
	})
	ctx := context.Background()
	first, _ := harness.draft(t)
	second, _ := harness.draft(t)
	if second.VersionNumber <= first.VersionNumber {
		t.Fatalf("the second draft is version %d against the first's %d", second.VersionNumber, first.VersionNumber)
	}
	// A track must be under review before it can be approved, which is the state a review leaves it
	// in. Approving a draft directly is refused rather than silently allowed.
	if _, err := harness.service.Approve(ctx, second.ID, "drama-episode", "trace-0"); err == nil {
		t.Fatal("a draft was approved without review")
	}
	// THE SEQUENCE A USER ACTUALLY GOES THROUGH: the first track is reviewed and approved, and then a
	// second is reviewed and approved over it. Approving the second is what has to SUPERSEDE the
	// first, and a test that put both into review and then approved both would never exercise that
	// arm — which is what the first version of this test did, and the read-back caught it.
	for _, step := range []struct {
		id    string
		trace string
	}{{first.ID, "trace-1"}, {second.ID, "trace-2"}} {
		if _, err := harness.db.ExecContext(ctx,
			`UPDATE subtitle_tracks SET status = 'under_review' WHERE id = ?`, step.id); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.service.Approve(ctx, step.id, "drama-episode", step.trace); err != nil {
			t.Fatalf("Approve(%s): %v", step.id, err)
		}
	}
	approved, found, err := harness.tracks.CurrentApprovedTrack(ctx, "drama-episode")
	if err != nil || !found {
		t.Fatalf("no approved track: found=%v err=%v", found, err)
	}
	if approved.ID != second.ID {
		t.Fatalf("the approved track is %q, want %q", approved.ID, second.ID)
	}
	// The first is superseded rather than deleted, which is what lets a reader see what the subtitles
	// said when an export was made.
	superseded, err := harness.tracks.GetTrack(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Status != "superseded" {
		t.Fatalf("the replaced track is %q", superseded.Status)
	}
	// And approving a track from another episode is refused, so a caller cannot reach across.
	if _, err := harness.service.Approve(ctx, first.ID, "another-episode", "trace-3"); err == nil {
		t.Fatal("a track from another episode was approved")
	}
}

// parseSRTForTest reads a SubRip document the way a player does.
func parseSRTForTest(t *testing.T, document string) []domainmedia.Cue {
	t.Helper()
	cues := []domainmedia.Cue{}
	for _, block := range strings.Split(strings.TrimSpace(document), "\n\n") {
		lines := strings.Split(block, "\n")
		if len(lines) < 3 {
			t.Fatalf("a block has %d lines:\n%s", len(lines), block)
		}
		start, end, err := domainmedia.ParseTimeRange(lines[1])
		if err != nil {
			t.Fatalf("a block's time line: %v", err)
		}
		cues = append(cues, domainmedia.Cue{Ordinal: len(cues) + 1, Start: start, End: end,
			Text: strings.Join(lines[2:], "\n")})
	}
	return cues
}

// parseVTTForTest reads a WebVTT document, skipping the signature and any NOTE or STYLE blocks.
func parseVTTForTest(t *testing.T, document string) []domainmedia.Cue {
	t.Helper()
	cues := []domainmedia.Cue{}
	body := strings.TrimPrefix(document, "WEBVTT\n\n")
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		if block == "" || strings.HasPrefix(block, "NOTE") || strings.HasPrefix(block, "STYLE") {
			continue
		}
		lines := strings.Split(block, "\n")
		// The time line is the FIRST line that contains the arrow: a cue may open with an identifier.
		timeIndex := -1
		for index, line := range lines {
			if strings.Contains(line, "-->") {
				timeIndex = index
				break
			}
		}
		if timeIndex < 0 || timeIndex+1 > len(lines) {
			t.Fatalf("a VTT block has no time line:\n%s", block)
		}
		start, end, err := domainmedia.ParseTimeRange(lines[timeIndex])
		if err != nil {
			t.Fatalf("a VTT block's time line: %v", err)
		}
		cues = append(cues, domainmedia.Cue{
			Ordinal: len(cues) + 1, Start: start, End: end,
			Text: strings.Join(lines[timeIndex+1:], "\n"),
		})
	}
	return cues
}
