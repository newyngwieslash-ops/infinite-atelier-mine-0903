// Package media is the media pipeline's domain: the shapes and the arithmetic, with no I/O.
//
// # What lives here and what does not
//
// Three things, and each is here because two callers need the SAME answer:
//
//   - TIMECODE. An SRT writes `00:00:01,500` and a VTT writes `00:00:01.500`; they are the same
//     instant written two ways, and a formatter on each side would be two rules for one fact. The
//     storage is milliseconds, exactly, and this package owns both round trips.
//   - THE CUE AND ITS TRACK. AC-MEDIA-002 asks that a subtitle be editable and that a missing line
//     be DETECTED; the first is a validation rule and the second is a join, and both are statements
//     about the cues rather than about a database.
//   - THE EXPORT MANIFEST. AC-MEDIA-003's "manifest traceability" means a frame can be traced to the
//     script version that produced it, so the manifest is a list of VERSION REFERENCES and this
//     package is what says which references a complete one has.
//
// It does not compose media (that is the engine, and it starts a process), does not store anything
// (that is the repositories) and does not read a file. Those are all I/O, and the domain has none.
package media

import (
	"strconv"
	"strings"
	"time"
)

// Timecode is one instant in a media timeline, as milliseconds from the start.
//
// The unit is the point. A subtitle file's two formats disagree about punctuation and agree about
// everything else, and milliseconds are the common denominator: an SRT's comma and a VTT's dot are
// renderings of the same integer, so a value that arrived from one and left as the other has not
// been converted — it has been formatted twice.
type Timecode int64

// MaxTimecodeMS bounds a timecode at twenty-four hours.
//
// It is a ceiling rather than a target: an episode is minutes, and a value beyond this is a parse
// that went wrong or a user who typed a duration into a start field. Refusing it here means the
// refusal names the timecode rather than turning up as an arithmetic overflow somewhere else.
const MaxTimecodeMS = 24 * 60 * 60 * 1000

// InvalidTimecodeError is the refusal every parse shares.
func invalidTimecode(value string) error {
	return InvalidError("The timecode " + strconv.Quote(value) + " is not a time a subtitle can use.")
}

// FormatSRT renders the timecode the way a SubRip file writes it: `HH:MM:SS,mmm`.
//
// The hours are not padded to two beyond two digits — an SRT allows `100:00:00,000` — but they are
// padded TO two, because a player that reads `1:00:00,000` as a hundred hours is a real bug in real
// players and this is not the place to find out which ones.
func (t Timecode) FormatSRT() string {
	return formatTimecode(t, ',')
}

// FormatVTT renders the timecode the way a WebVTT file writes it: `HH:MM:SS.mmm`.
func (t Timecode) FormatVTT() string {
	return formatTimecode(t, '.')
}

// formatTimecode renders `HH:MM:SS<separator>mmm`, which is where the two formats differ.
func formatTimecode(value Timecode, separator byte) string {
	milliseconds := int64(value)
	if milliseconds < 0 {
		// A negative timecode never reaches a file: the domain refuses one at validation, and this
		// branch exists so a caller that bypassed validation gets a readable value rather than a
		// broken one. It is the sign that is wrong, not the arithmetic.
		milliseconds = -milliseconds
	}
	hours := milliseconds / 3600000
	minutes := (milliseconds % 3600000) / 60000
	seconds := (milliseconds % 60000) / 1000
	remainder := milliseconds % 1000
	var builder strings.Builder
	writePadded(&builder, hours, 2)
	builder.WriteByte(':')
	writePadded(&builder, minutes, 2)
	builder.WriteByte(':')
	writePadded(&builder, seconds, 2)
	builder.WriteByte(separator)
	writePadded(&builder, remainder, 3)
	return builder.String()
}

// writePadded writes a non-negative integer with at least `width` digits.
func writePadded(builder *strings.Builder, value int64, width int) {
	digits := strconv.FormatInt(value, 10)
	for len(digits) < width {
		digits = "0" + digits
	}
	builder.WriteString(digits)
}

// ParseTimecode reads either format.
//
// It accepts both punctuations rather than taking a parameter, and that is deliberate: a caller
// reading a user's edit box does not know which format the field was showing, and the two are
// unambiguous — a comma cannot appear in a VTT timecode and a dot cannot appear in an SRT one. A
// parse that refused the other format would refuse a value the user could see.
//
// It also accepts the shortened forms WebVTT permits (`MM:SS.mmm` and `SS.mmm`), because a VTT file
// this application did not write may use them and a reader that refused to load one would be a
// reader that cannot open a subtitle.
func ParseTimecode(value string) (Timecode, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, invalidTimecode(value)
	}
	// The separator: the last comma or dot, because those are the only two the formats use and a
	// timecode has no other reason to contain one.
	separator := strings.LastIndexAny(trimmed, ",.")
	if separator < 0 {
		return 0, invalidTimecode(value)
	}
	fraction := trimmed[separator+1:]
	whole := trimmed[:separator]
	if len(fraction) == 0 || len(fraction) > 3 {
		return 0, invalidTimecode(value)
	}
	for _, symbol := range fraction {
		if symbol < '0' || symbol > '9' {
			return 0, invalidTimecode(value)
		}
	}
	// Right-pad the fraction to milliseconds: `.5` is five hundred, not five.
	millis, err := strconv.Atoi(fraction + strings.Repeat("0", 3-len(fraction)))
	if err != nil {
		return 0, invalidTimecode(value)
	}
	parts := strings.Split(whole, ":")
	if len(parts) < 1 || len(parts) > 3 {
		return 0, invalidTimecode(value)
	}
	values := make([]int, 0, 3)
	for _, part := range parts {
		if part == "" {
			return 0, invalidTimecode(value)
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return 0, invalidTimecode(value)
		}
		values = append(values, number)
	}
	// The shorter forms: `MM:SS` and `SS`. Hours are read from whichever end is left.
	hours, minutes, seconds := 0, 0, 0
	switch len(values) {
	case 3:
		hours, minutes, seconds = values[0], values[1], values[2]
	case 2:
		minutes, seconds = values[0], values[1]
	case 1:
		seconds = values[0]
	}
	// Minutes and seconds are bounded because a value beyond them is a mis-typed field rather than a
	// long film: sixty minutes is an hour, and an SRT that said `00:75:00,000` would be read
	// differently by different players.
	if minutes > 59 || seconds > 59 {
		return 0, invalidTimecode(value)
	}
	total := Timecode(hours)*3600000 + Timecode(minutes)*60000 + Timecode(seconds)*1000 + Timecode(millis)
	if total > MaxTimecodeMS {
		return 0, invalidTimecode(value)
	}
	return total, nil
}

// ParseTimeRange reads a subtitle file's `start --> end` line.
//
// The arrow is what both formats use, and the line is the only place a cue's two times are stated
// together — so the parse reads them together rather than leaving a caller to split the string and
// get the whitespace wrong.
func ParseTimeRange(value string) (Timecode, Timecode, error) {
	start, end, found := strings.Cut(value, "-->")
	if !found {
		return 0, 0, InvalidError("A subtitle cue's times must be written as `start --> end`.")
	}
	startAt, err := ParseTimecode(start)
	if err != nil {
		return 0, 0, err
	}
	// A VTT time line may carry settings after the end time ("align:start position:10%"), and the
	// first whitespace-delimited field is the time. Trimming the whole end would fail on those; taking
	// the first field is what the format means.
	endFields := strings.Fields(end)
	if len(endFields) == 0 {
		return 0, 0, invalidTimecode(end)
	}
	endAt, err := ParseTimecode(endFields[0])
	if err != nil {
		return 0, 0, err
	}
	return startAt, endAt, nil
}

// Duration returns how long the timecode would last if it were a start and `other` were its end.
//
// It is a helper rather than a subtraction at each call site so the DIRECTION is stated once: a
// negative result means the two were the wrong way round, and a caller that wanted a length from a
// pair should be told that rather than given a negative number.
func (t Timecode) Duration(other Timecode) (time.Duration, error) {
	if other <= t {
		return 0, InvalidError("A cue's end must come after its start.")
	}
	return time.Duration(other-t) * time.Millisecond, nil
}
