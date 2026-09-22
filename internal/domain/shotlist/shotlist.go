// Package shotlist renders a storyboard version as a document.
//
// # Why a table rather than prose
//
// A storyboard is a TABLE. DOMAIN_MODEL section 9.3's rows are one per shot with twelve fields, and
// the thing a production does with them is hand them to people: a shot list goes to the camera
// department, a prop list to the art department, and a spreadsheet to whoever is scheduling. So the
// formats here are the ones a table travels in — CSV for a tool, and an aligned plain-text table for
// a person who will read it on screen or print it.
//
// # What it does NOT do
//
// It does not write an image. A storyboard's panels are approved media rows in this schema, and a
// document that embedded them would be an archive rather than a shot list — the archive already
// exists (`internal/infrastructure/archive`), and duplicating it here would be a second, worse one.
// The panel's VERSION ID travels in the CSV, which is what makes a row traceable back to the frame
// that was approved for it.
package shotlist

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	storyboarddomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
)

// Format is how a shot list is written.
type Format string

const (
	// FormatText is an aligned table a person reads.
	FormatText Format = "txt"
	// FormatCSV is comma-separated, with a header row, for a spreadsheet.
	FormatCSV Format = "csv"
)

// Formats lists the documented formats in a stable order.
var Formats = []Format{FormatText, FormatCSV}

// IsValidFormat reports whether a value may be rendered.
//
// An empty value is not valid: a caller that named no format has not said what to write, and
// defaulting would produce a file whose extension and content disagree the moment the default moved.
func IsValidFormat(value Format) bool {
	for _, candidate := range Formats {
		if candidate == value {
			return true
		}
	}
	return false
}

// ExtensionFor is the file extension a format is written with.
func (f Format) ExtensionFor() string {
	switch f {
	case FormatCSV:
		return ".csv"
	default:
		return ".txt"
	}
}

// Row is one shot, with everything the document states about it.
//
// It is a flat struct rather than the domain's `StoryboardItem`, because a shot list is a JOIN: the
// row carries the board's own fields, the shot's identity from the script, and the panel version
// approved for it — three things that live in three places, assembled by the caller and rendered
// here. A renderer that read a repository would need a database to test.
type Row struct {
	Ordinal int
	// ShotID is the script's shot, which is what a schedule cites. ShotNumber is the label the board
	// shows, which may be empty on a draft.
	ShotID     string
	ShotNumber string
	// ShotSize, CameraAngle and CameraMovement are the framing decisions.
	ShotSize       string
	CameraAngle    string
	CameraMovement string
	// DurationSeconds is the row's own estimate, which is what the film's length is built from.
	DurationSeconds int
	// VisualDescription, ActionDescription, DialogueAudioSummary and ContinuityNotes are the four
	// prose fields.
	VisualDescription    string
	ActionDescription    string
	DialogueAudioSummary string
	ContinuityNotes      string
	// The three FR-070 fields: what a video model is given to render the shot's two ends and its
	// motion.
	FirstFrameDescription  string
	LastFrameDescription   string
	VideoMotionDescription string
	// PanelVersionID and ApprovedVersionID are the panel and the media version approved for the row,
	// both empty when nothing is approved. They are in the document because they are what makes a row
	// TRACEABLE: a printed shot list that cannot name the frame it refers to is a list somebody has to
	// match by eye.
	PanelVersionID    string
	ApprovedVersionID string
}

// MissingMedia reports whether this row has no approved media.
//
// IT IS A METHOD RATHER THAN A FIELD, and that is a correction: an earlier version carried a
// `MissingMedia bool` that `FromItem` derived and a caller building a `Row` literally had to remember
// to set. A field two callers can disagree about is derived state, and the disagreement is silent —
// a row with nothing approved that reports otherwise is scheduled as ready. Deriving it from the one
// fact it depends on makes the two impossible to separate.
func (r Row) MissingMedia() bool {
	return strings.TrimSpace(r.ApprovedVersionID) == ""
}

// Document is everything a rendering reads.
type Document struct {
	ProjectName  string
	EpisodeTitle string
	SeasonNumber int
	EpisodeNum   int
	// BoardVersionNumber is the version's position in its own history.
	BoardVersionNumber int
	// ScriptVersionID and DirectorPlanVersionID name what the board was drawn FROM, which is the
	// traceability a shot list shares with the manifest: a row is only meaningful beside the script
	// version it renders.
	ScriptVersionID       string
	DirectorPlanVersionID string
	// Rows are the shots in board order.
	Rows []Row
}

// TotalDurationSeconds sums the rows' own estimates.
//
// It is computed here rather than passed in, so a document cannot state a total that disagrees with
// the rows it prints. A shot list whose header says forty-eight seconds and whose rows add to
// fifty-two is a list somebody re-times by hand.
func (d Document) TotalDurationSeconds() int {
	total := 0
	for _, row := range d.Rows {
		total += row.DurationSeconds
	}
	return total
}

// MissingMediaCount counts the rows with nothing approved.
func (d Document) MissingMediaCount() int {
	count := 0
	for _, row := range d.Rows {
		if row.MissingMedia() {
			count++
		}
	}
	return count
}

// Render writes a document in one format.
func Render(document Document, format Format) (string, error) {
	switch format {
	case FormatText:
		return renderText(document), nil
	case FormatCSV:
		return renderCSV(document)
	default:
		return "", fmt.Errorf("a shot list cannot be written as %q", string(format))
	}
}

// The CSV header, in the columns' order.
//
// It is a VAR rather than a literal at the call site so a test can assert the header and the row
// builder agree: a header with thirteen names and a row with twelve values produces a file whose
// columns have silently shifted, which is the failure a spreadsheet user would find last.
var csvHeader = []string{
	"ordinal", "shotId", "shotNumber", "shotSize", "cameraAngle", "cameraMovement",
	"durationSeconds", "visualDescription", "actionDescription", "dialogueAudioSummary",
	"continuityNotes", "firstFrameDescription", "lastFrameDescription", "videoMotionDescription",
	"panelVersionId", "approvedVersionId", "missingMedia",
}

// CSVHeader returns the header row's names, so a caller can assert the contract.
func CSVHeader() []string {
	header := make([]string, len(csvHeader))
	copy(header, csvHeader)
	return header
}

// rowValues renders one row in the header's order.
func rowValues(row Row) []string {
	return []string{
		strconv.Itoa(row.Ordinal),
		row.ShotID,
		row.ShotNumber,
		row.ShotSize,
		row.CameraAngle,
		row.CameraMovement,
		strconv.Itoa(row.DurationSeconds),
		row.VisualDescription,
		row.ActionDescription,
		row.DialogueAudioSummary,
		row.ContinuityNotes,
		row.FirstFrameDescription,
		row.LastFrameDescription,
		row.VideoMotionDescription,
		row.PanelVersionID,
		row.ApprovedVersionID,
		strconv.FormatBool(row.MissingMedia()),
	}
}

// renderCSV writes the header and one line per row.
func renderCSV(document Document) (string, error) {
	var out strings.Builder
	// encoding/csv rather than a hand-rolled join, and the difference is not cosmetic: a
	// `visualDescription` containing a comma, a quote or a newline is ordinary prose, and a renderer
	// that joined with commas would produce a file whose columns shift on exactly the rows a person
	// wrote most carefully.
	writer := csv.NewWriter(&out)
	if err := writer.Write(csvHeader); err != nil {
		return "", err
	}
	for _, row := range document.Rows {
		if err := writer.Write(rowValues(row)); err != nil {
			return "", err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// renderText writes the aligned table.
// renderText writes the aligned table.
//
// EVERY VALUE IS FLATTENED, and that is not cosmetic: a `visualDescription` is ordinary prose and may
// contain a newline, and a newline written into a table cell breaks the table from that row down —
// every later field lands in the wrong column and the document reads as garbled. The CSV renderer
// keeps the newline, because a quoted CSV cell is DEFINED to hold one and a spreadsheet shows it
// properly; here the layout is the whole point, so a break becomes a space.
//
// The three FR-070 fields are written in the SCHEMA's order rather than a map's, so two runs of this
// renderer produce byte-identical documents. An earlier draft ranged over a map, which produced a
// different field order per run — a document that differs from itself is one a diff cannot review.
func renderText(document Document) string {
	var out strings.Builder
	writeHeading(&out, document)

	// The columns a reader scans: the position, the framing, the length and what happens. The long
	// prose fields go BELOW each row rather than into columns, because a description that wraps inside
	// a column makes the table unreadable at any width that fits a page.
	out.WriteString("\n")
	out.WriteString(fmt.Sprintf("%-4s %-8s %-10s %-12s %-14s %6s  %s\n",
		"#", "镜头", "景别", "角度", "运动", "时长", "内容"))
	out.WriteString(strings.Repeat("-", 100) + "\n")
	for _, row := range document.Rows {
		out.WriteString(fmt.Sprintf("%-4d %-8s %-10s %-12s %-14s %5ds  %s\n",
			row.Ordinal,
			flatten(truncate(row.ShotNumber, 8)),
			flatten(truncate(row.ShotSize, 10)),
			flatten(truncate(row.CameraAngle, 12)),
			flatten(truncate(row.CameraMovement, 14)),
			row.DurationSeconds,
			flatten(truncate(row.VisualDescription, 40)),
		))
		if action := strings.TrimSpace(row.ActionDescription); action != "" {
			out.WriteString("     动作：" + flatten(action) + "\n")
		}
		if dialogue := strings.TrimSpace(row.DialogueAudioSummary); dialogue != "" {
			out.WriteString("     声音：" + flatten(dialogue) + "\n")
		}
		if continuity := strings.TrimSpace(row.ContinuityNotes); continuity != "" {
			out.WriteString("     连续性：" + flatten(continuity) + "\n")
		}
		for _, field := range []struct{ label, value string }{
			{"首帧", row.FirstFrameDescription},
			{"尾帧", row.LastFrameDescription},
			{"运动", row.VideoMotionDescription},
		} {
			if trimmed := strings.TrimSpace(field.value); trimmed != "" {
				out.WriteString("     " + field.label + "：" + flatten(trimmed) + "\n")
			}
		}
		// The traceability line, and the missing state said in words rather than by an absence: a
		// reader deciding what to schedule needs to see which rows have no approved frame.
		switch {
		case row.MissingMedia():
			out.WriteString("     媒体：未批准\n")
		case row.ApprovedVersionID != "":
			out.WriteString("     媒体：" + row.ApprovedVersionID + "\n")
		}
	}
	return out.String()
}

// flatten turns a value into one line.
//
// It escapes nothing: the table is aligned text rather than a format with a grammar, so replacing a
// run of whitespace with a single space is the whole job.
func flatten(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// writeHeading writes the document's own header.
func writeHeading(out *strings.Builder, document Document) {
	if name := strings.TrimSpace(document.ProjectName); name != "" {
		out.WriteString(name + "\n")
	}
	episode := strings.TrimSpace(document.EpisodeTitle)
	switch {
	case episode != "" && document.SeasonNumber > 0:
		out.WriteString(fmt.Sprintf("第 %d 季 第 %d 集 %s\n", document.SeasonNumber, document.EpisodeNum, episode))
	case episode != "":
		out.WriteString(fmt.Sprintf("第 %d 集 %s\n", document.EpisodeNum, episode))
	default:
		out.WriteString(fmt.Sprintf("第 %d 集\n", document.EpisodeNum))
	}
	out.WriteString(fmt.Sprintf("分镜版本 v%d\n", document.BoardVersionNumber))
	out.WriteString(fmt.Sprintf("镜头 %d 个，合计 %d 秒\n", len(document.Rows), document.TotalDurationSeconds()))
	if missing := document.MissingMediaCount(); missing > 0 {
		out.WriteString(fmt.Sprintf("其中 %d 个镜头尚无批准媒体\n", missing))
	}
	// What the board was drawn from, which is the traceability a shot list shares with the export
	// manifest: a row is only meaningful beside the script version it renders.
	if script := strings.TrimSpace(document.ScriptVersionID); script != "" {
		out.WriteString("剧本版本：" + script + "\n")
	}
	if plan := strings.TrimSpace(document.DirectorPlanVersionID); plan != "" {
		out.WriteString("导演方案版本：" + plan + "\n")
	}
}

// truncate shortens a cell to a column's width, marking the cut.
func truncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

// FromItem assembles one document row from a board row and the versions resolved for it.
//
// It is a FUNCTION rather than a struct conversion, and an earlier draft's `var _ = func(...)` was
// the shape an independent review caught elsewhere in this repository: a package-level function VALUE
// proves nothing at all, because it compiles whatever the signature is. Three of these fields are
// decisions rather than copies — whether the media is approved, and which identifier labels the shot
// — so stating them in one place is what keeps a caller from inventing a second answer.
//
// `approvedVersionID` empty means the row has no approved media, which `Row.MissingMedia` answers from
// that one fact rather than from a field this constructor would have to remember to set.
func FromItem(item storyboarddomain.StoryboardItem, panelVersionID, approvedVersionID string) Row {
	return Row{
		Ordinal:                item.Ordinal,
		ShotID:                 item.ShotID,
		ShotNumber:             strings.TrimSpace(item.ShotID),
		ShotSize:               item.ShotSize,
		CameraAngle:            item.CameraAngle,
		CameraMovement:         item.CameraMovement,
		DurationSeconds:        item.DurationSeconds,
		VisualDescription:      item.VisualDescription,
		ActionDescription:      item.ActionDescription,
		DialogueAudioSummary:   item.DialogueAudioSummary,
		ContinuityNotes:        item.ContinuityNotes,
		FirstFrameDescription:  item.FirstFrameDescription,
		LastFrameDescription:   item.LastFrameDescription,
		VideoMotionDescription: item.VideoMotionDescription,
		PanelVersionID:         panelVersionID,
		ApprovedVersionID:      approvedVersionID,
	}
}
