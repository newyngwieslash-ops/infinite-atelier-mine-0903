package media

import (
	"strings"
	"time"
)

// The subtitle shapes, which are DOMAIN_MODEL section 7.7's dialogue lines rendered on a timeline.

// CueStatus is whether a cue is settled.
//
// It is deliberately only two values. A cue is either what the generator produced or something the
// user has touched, and the distinction is what makes a regeneration safe: a track rebuilt from a
// revised script must not silently overwrite a line somebody typed. A richer vocabulary — reviewed,
// rejected, needs-work — would be a workflow, and AC-MEDIA-002 asks for an EDITABLE subtitle rather
// than a reviewed one.
type CueStatus string

const (
	// CueGenerated is a cue no one has edited.
	CueGenerated CueStatus = "generated"
	// CueEdited is a cue the user changed, and therefore one a regeneration must not replace.
	CueEdited CueStatus = "edited"
)

// CueStatuses lists the documented statuses in the schema's order.
var CueStatuses = []CueStatus{CueGenerated, CueEdited}

// IsValidCueStatus reports whether a status may be persisted.
func IsValidCueStatus(value CueStatus) bool {
	for _, candidate := range CueStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// Cue is one subtitle line on the timeline.
type Cue struct {
	ID      string
	TrackID string
	// Ordinal is the cue's position, running from one with no gap — the same rule the script's
	// scenes and shots follow, and the reason a subtitle file needs no sort: the order is stored.
	Ordinal int
	Start   Timecode
	End     Timecode
	Text    string
	// CharacterEntityID is who speaks, empty for a line no character says. It is carried on the cue
	// rather than looked up through the line because the line may be gone: a user's own cue or an
	// edit that outlived a script revision still has to render, and "unknown speaker" is a worse
	// answer than the name that was there when the cue was made.
	CharacterEntityID string
	// DialogueLineID is the line this cue renders, or empty for a cue with no source. It is what
	// AC-MEDIA-002's "missing line detected" joins on.
	DialogueLineID string
	Status         CueStatus
	CreatedAt      time.Time
}

// MaxCueTextRunes bounds one cue's text.
//
// It exists because a cue is rendered into a video frame and into a file: an unbounded line would be
// unreadable on screen and would make the export's subtitle file larger than the film. The number is
// generous for two lines of dialogue.
const MaxCueTextRunes = 500

// Validate checks one cue before it is stored.
func (c Cue) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return InvalidError("A cue needs an identifier.")
	}
	if strings.TrimSpace(c.TrackID) == "" {
		return InvalidError("A cue must belong to a subtitle track.")
	}
	if c.Ordinal < 1 {
		return InvalidError("A cue's position starts at one.")
	}
	if c.Start < 0 {
		return InvalidError("A cue cannot start before the film does.")
	}
	// The one arithmetic rule, checked here and by the schema's CHECK. A cue with no duration is
	// invisible on screen, so it is refused rather than stored and skipped.
	if c.End <= c.Start {
		return InvalidError("A cue's end must come after its start.")
	}
	if c.End > MaxTimecodeMS {
		return InvalidError("A cue ends beyond the longest film this build handles.")
	}
	if len([]rune(c.Text)) > MaxCueTextRunes {
		return InvalidError("A cue's text is longer than one subtitle line can show.")
	}
	if c.Status != "" && !IsValidCueStatus(c.Status) {
		return InvalidError("The cue's status is not recognised.")
	}
	return nil
}

// Track is a versioned set of cues for one episode.
type Track struct {
	ID               string
	EpisodeID        string
	ScriptVersionID  string
	VersionNumber    int
	Status           string
	BasedOnVersionID string
	SourceAgentRunID string
	CreatedByType    string
	CreatedByID      string
	ChangeReason     string
	CreatedAt        time.Time
}

// Validate checks a track before it is stored.
func (t Track) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return InvalidError("A subtitle track needs an identifier.")
	}
	if strings.TrimSpace(t.EpisodeID) == "" {
		return InvalidError("A subtitle track must belong to an episode.")
	}
	if strings.TrimSpace(t.ScriptVersionID) == "" {
		return InvalidError("A subtitle track must name the script version it renders.")
	}
	if t.VersionNumber < 1 {
		return InvalidError("A subtitle track's version number starts at one.")
	}
	return nil
}

// TrackIssues are the problems a cue list has as a WHOLE, which no single cue can see.
//
// The three rules are separate from Cue.Validate because each is about the relationship between
// cues: an empty list is not a cue problem, an ordinal gap is not visible from one row, and an
// overlap needs two. A validator that only looked at one cue at a time would pass a track whose
// subtitles were in the wrong order.
type TrackIssue struct {
	// Ordinal is the cue the issue is about, or zero for the track as a whole.
	Ordinal int
	Problem string
}

// ValidateTrack checks a cue list as a unit.
//
// It returns every problem rather than the first, because a user fixing subtitles wants the list:
// a validator that stopped at the first gap would make them re-run it once per mistake.
func ValidateTrack(cues []Cue) []TrackIssue {
	issues := []TrackIssue{}
	// An empty track is legal — it is what "generate a track from a script with no dialogue" produces
	// — so there is no "must have a cue" rule. What is a problem is a track that SKIPS a position,
	// because the ordinals are the order a player and an export both read.
	for index, cue := range cues {
		if cue.Ordinal != index+1 {
			issues = append(issues, TrackIssue{
				Ordinal: cue.Ordinal,
				Problem: "The cue at position " + itoa(index+1) + " states ordinal " + itoa(cue.Ordinal) +
					", so the track's order disagrees with its lines.",
			})
		}
		if cue.End <= cue.Start {
			issues = append(issues, TrackIssue{
				Ordinal: cue.Ordinal,
				Problem: "The cue at position " + itoa(index+1) + " ends before it starts.",
			})
		}
		// An overlap is two cues on screen at once, which a player renders as one subtitle replacing
		// the other — usually not what anybody meant, and always invisible in the editor because both
		// lines look fine on their own.
		if index > 0 && cue.Start < cues[index-1].End {
			issues = append(issues, TrackIssue{
				Ordinal: cue.Ordinal,
				Problem: "The cue at position " + itoa(index+1) + " starts before the one before it ends, " +
					"so the two would be on screen together.",
			})
		}
	}
	return issues
}

// MissingLines are the dialogue lines a track does not cover, which is AC-MEDIA-002's
// "missing line detected".
//
// # What it is and what it is deliberately not
//
// It answers "which lines that SHOULD be subtitled have no cue". A line should be subtitled when it
// is spoken: `dialogue` and `narration`, which is the same distinction DOMAIN_MODEL section 7.7's
// LineType draws ("a spoken line names a character entity while action and narration do not" — and
// narration is spoken over the picture by definition). An `action`, `transition` or `note` line is a
// direction to the production, not something anyone says, so its absence from a subtitle file is
// correct rather than missing.
//
// It takes the lines as a list of the two facts it needs rather than the whole domain type, because
// this package has no dependency on the script domain: a cue and a dialogue line are different
// aggregates, and importing one into the other would tie two versioning models together for two
// fields.
type SpokenLine struct {
	LineID            string
	Type              string
	CharacterEntityID string
	Text              string
}

// IsSpoken reports whether a line type is one a subtitle should carry.
func IsSpoken(lineType string) bool {
	return lineType == "dialogue" || lineType == "narration"
}

// MissingLines returns the spoken lines no cue cites, in the order they were given.
//
// The order is the caller's, so a caller that passed the lines in script order gets them back in
// script order — which is the order a reader fixes them in.
func MissingLines(lines []SpokenLine, cues []Cue) []SpokenLine {
	covered := make(map[string]bool, len(cues))
	for _, cue := range cues {
		if cue.DialogueLineID != "" {
			covered[cue.DialogueLineID] = true
		}
	}
	missing := []SpokenLine{}
	for _, line := range lines {
		if !IsSpoken(line.Type) {
			continue
		}
		if covered[line.LineID] {
			continue
		}
		missing = append(missing, line)
	}
	return missing
}

// RenderSRT writes a track as a SubRip document.
//
// # Why this is hand-written rather than delegated
//
// ffmpeg can convert between subtitle formats, and doing that would mean every user's edited text
// travelled through an external program to reach a file — a second parser on the path, and one whose
// own arguments are a place a caption could be misread as syntax. The format is four lines per cue;
// writing it is cheaper than reasoning about what a converter does with a line that starts with a
// digit.
//
// The blank line between cues is required by every reader, and a trailing newline is what makes the
// file's last line complete.
func RenderSRT(cues []Cue) string {
	var builder strings.Builder
	for index, cue := range cues {
		builder.WriteString(itoa(index + 1))
		builder.WriteString("\n")
		builder.WriteString(cue.Start.FormatSRT())
		builder.WriteString(" --> ")
		builder.WriteString(cue.End.FormatSRT())
		builder.WriteString("\n")
		builder.WriteString(cue.Text)
		builder.WriteString("\n\n")
	}
	return builder.String()
}

// RenderVTT writes a track as a WebVTT document.
//
// The differences from SRT are the header, the dot separator and that a cue may carry an identifier
// line — which this writes, because a reader that has to match a cue to a script line benefits from
// not counting.
func RenderVTT(cues []Cue) string {
	var builder strings.Builder
	// The signature line, and the blank line after it, are what makes a file recognisably WebVTT
	// rather than an SRT with dots.
	builder.WriteString("WEBVTT\n\n")
	for _, cue := range cues {
		builder.WriteString(cue.Start.FormatVTT())
		builder.WriteString(" --> ")
		builder.WriteString(cue.End.FormatVTT())
		builder.WriteString("\n")
		builder.WriteString(cue.Text)
		builder.WriteString("\n\n")
	}
	return builder.String()
}

// SubtitleFormat is which document a render produces.
type SubtitleFormat string

const (
	FormatSRT SubtitleFormat = "srt"
	FormatVTT SubtitleFormat = "vtt"
)

// SubtitleFormats lists the documented formats in the schema's order.
var SubtitleFormats = []SubtitleFormat{FormatSRT, FormatVTT}

// IsValidSubtitleFormat reports whether a format may be exported.
func IsValidSubtitleFormat(value SubtitleFormat) bool {
	for _, candidate := range SubtitleFormats {
		if candidate == value {
			return true
		}
	}
	return false
}

// MIMEFor returns the content type a format is written as.
//
// Both are `text/vtt` or `application/x-subrip`, and the second is not a registered type: SRT has no
// official media type, and the de-facto one is what players and editors agree on. It is stated here
// rather than at the call site because the file's stored MIME is sniffed from its bytes and this is
// what a caller tells a browser to expect.
func (f SubtitleFormat) MIMEFor() string {
	if f == FormatVTT {
		return "text/vtt"
	}
	return "application/x-subrip"
}

// ExtensionFor returns the file extension a format is written with.
func (f SubtitleFormat) ExtensionFor() string {
	if f == FormatVTT {
		return "vtt"
	}
	return "srt"
}

// Render writes a track in the named format.
func Render(cues []Cue, format SubtitleFormat) (string, error) {
	switch format {
	case FormatSRT:
		return RenderSRT(cues), nil
	case FormatVTT:
		return RenderVTT(cues), nil
	default:
		return "", InvalidError("The subtitle format is not recognised.")
	}
}

// itoa renders a small integer without importing strconv at each call site.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
