package shotlist

import (
	"encoding/csv"
	"strconv"
	"strings"
	"testing"

	storyboarddomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// fixture is a three-shot board: two rows with approved media and one without, which is the state a
// scheduler most needs to see.
func fixture() Document {
	return Document{
		ProjectName:           "渡口",
		EpisodeTitle:          "灯巷里的名字",
		SeasonNumber:          1,
		EpisodeNum:            3,
		BoardVersionNumber:    2,
		ScriptVersionID:       "script-v2",
		DirectorPlanVersionID: "plan-v1",
		Rows: []Row{
			{
				Ordinal: 1, ShotID: "shot-1", ShotNumber: "1", ShotSize: "MS",
				CameraAngle: "eye level", CameraMovement: "static", DurationSeconds: 4,
				VisualDescription: "煤灯在画面左侧。", ActionDescription: "沈砚停步。",
				DialogueAudioSummary: "沈砚：灯还亮着。", ContinuityNotes: "冬装。",
				FirstFrameDescription: "煤灯左。", LastFrameDescription: "煤灯右。",
				VideoMotionDescription: "轻微横移。",
				PanelVersionID:         "panel-1", ApprovedVersionID: "media-1",
			},
			{
				// A row with a comma, a quote AND a newline in its prose, which is ordinary writing and
				// the exact content a hand-rolled CSV join would break on.
				Ordinal: 2, ShotID: "shot-2", ShotNumber: "2", ShotSize: "CU",
				CameraAngle: "high angle", CameraMovement: "slow push in", DurationSeconds: 6,
				VisualDescription: "他低声说：“账本, 不在盐仓。”\n然后转身。",
				ActionDescription: `她念了一句"走了"。`,
				PanelVersionID:    "panel-2", ApprovedVersionID: "media-2",
			},
			{
				// Nothing approved: the state the document must make visible rather than drop.
				Ordinal: 3, ShotID: "shot-3", ShotNumber: "3", ShotSize: "WS",
				DurationSeconds: 5, VisualDescription: "空船。",
			},
		},
	}
}

// TestTheTotalsComeFromTheRows pins that a document cannot state a total its own rows contradict.
//
// A shot list whose header says fifteen seconds and whose rows add to sixteen is a list somebody
// re-times by hand, which is the failure a printed call sheet produces.
//
// The two formats carry the total DIFFERENTLY and the test says so: the text document states it in its
// heading, and the CSV does NOT state one at all — a total row in a CSV is a row a spreadsheet would
// read as a shot. For the CSV the property that matters is still checkable: the duration column adds
// up to the same number, read back through the parser rather than grepped, because "15" appears inside
// a description in any real document.
func TestTheTotalsComeFromTheRows(t *testing.T) {
	document := fixture()
	if got := document.TotalDurationSeconds(); got != 15 {
		t.Fatalf("the total is %d, want the rows' own 15", got)
	}
	if got := document.MissingMediaCount(); got != 1 {
		t.Fatalf("the missing-media count is %d, want 1", got)
	}
	// An empty board is zero rather than an error: a board is created before it is filled.
	empty := Document{}
	if empty.TotalDurationSeconds() != 0 || empty.MissingMediaCount() != 0 {
		t.Fatal("an empty document reports a non-zero total")
	}

	// The text document STATES the total, so a reader does not add the column up themselves.
	text, err := Render(document, FormatText)
	if err != nil {
		t.Fatalf("Render(text): %v", err)
	}
	if !strings.Contains(text, "合计 15 秒") || !strings.Contains(text, "其中 1 个镜头尚无批准媒体") {
		t.Fatalf("the text document does not state its own totals: %s", text)
	}

	// The CSV carries the durations as data, and the assertion is that they ADD UP to the same number
	// the text document states.
	csvText, err := Render(document, FormatCSV)
	if err != nil {
		t.Fatalf("Render(csv): %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(csvText)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	durationColumn := -1
	for index, name := range records[0] {
		if name == "durationSeconds" {
			durationColumn = index
		}
	}
	if durationColumn < 0 {
		t.Fatal("the CSV has no duration column")
	}
	total := 0
	for _, record := range records[1:] {
		value, err := strconv.Atoi(record[durationColumn])
		if err != nil {
			t.Fatalf("a row's duration is %q: %v", record[durationColumn], err)
		}
		total += value
	}
	if total != document.TotalDurationSeconds() {
		t.Fatalf("the CSV's durations add to %d, want the document's %d", total, document.TotalDurationSeconds())
	}
}

// TestTheCSVHeaderAndTheRowsAgree is the column contract, and it is the assertion a spreadsheet user
// would have to make by eye.
//
// A header with seventeen names and a row with sixteen values produces a file whose columns have
// silently shifted from that row on — every value under the wrong heading, and nothing that looks
// wrong until somebody reads it.
func TestTheCSVHeaderAndTheRowsAgree(t *testing.T) {
	text, err := Render(fixture(), FormatCSV)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatalf("the CSV document is not readable: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("the CSV has %d rows, want a header and three shots", len(records))
	}
	header := records[0]
	if len(header) != len(csvHeader) {
		t.Fatalf("the header has %d columns against the declared %d", len(header), len(csvHeader))
	}
	for index, name := range csvHeader {
		if header[index] != name {
			t.Fatalf("column %d is %q, want %q", index, header[index], name)
		}
	}
	for index, record := range records[1:] {
		if len(record) != len(header) {
			t.Fatalf("row %d has %d values against the header's %d columns", index, len(record), len(header))
		}
	}
	// The PROSE survived, which is the property encoding/csv is here for: a comma, a quote and a
	// newline inside a cell are ordinary writing.
	if !strings.Contains(records[2][7], "账本, 不在盐仓") {
		t.Fatalf("the second row's description lost its comma: %q", records[2][7])
	}
	if !strings.Contains(records[2][7], "\n") {
		t.Fatalf("the second row's description lost its newline: %q", records[2][7])
	}
	if !strings.Contains(records[2][8], `"走了"`) {
		t.Fatalf("the second row's action lost its quotes: %q", records[2][8])
	}
	// The ordinal column is the position, and the missing-media column says so for the third row
	// only — an absence a reader has to infer from an empty cell is the thing this column exists to
	// avoid.
	if records[1][0] != "1" || records[2][0] != "2" || records[3][0] != "3" {
		t.Fatalf("the ordinals are %q, %q, %q", records[1][0], records[2][0], records[3][0])
	}
	if records[1][16] != "false" || records[2][16] != "false" {
		t.Fatalf("a row with approved media reports missingMedia=%q", records[1][16])
	}
	if records[3][16] != "true" {
		t.Fatalf("the row with nothing approved reports missingMedia=%q", records[3][16])
	}
}

// TestTheCSVHeaderIsACopy pins that a caller cannot corrupt the contract.
//
// `CSVHeader` exists so a test can assert the columns; a caller that mutated the slice it returned
// would change what every later render writes, which is the shape of a package-level mutable.
func TestTheCSVHeaderIsACopy(t *testing.T) {
	first := CSVHeader()
	if len(first) == 0 {
		t.Fatal("the header is empty")
	}
	first[0] = "corrupted"
	if second := CSVHeader(); second[0] == "corrupted" {
		t.Fatal("mutating the returned header changed the package's own")
	}
}

// TestTheTextDocumentCarriesEveryField grades the human-readable renderer.
func TestTheTextDocumentCarriesEveryField(t *testing.T) {
	text, err := Render(fixture(), FormatText)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		// The heading: what this is, which board version, and the two counts a scheduler reads first.
		"渡口", "第 1 季 第 3 集 灯巷里的名字", "分镜版本 v2",
		"镜头 3 个，合计 15 秒", "其中 1 个镜头尚无批准媒体",
		// The board's own provenance, which is the traceability a shot list shares with the manifest.
		"剧本版本：script-v2", "导演方案版本：plan-v1",
		// A row's framing, its length and its prose.
		"MS", "eye level", "static", "4s", "煤灯在画面左侧。",
		// The four prose fields are labelled rather than folded into the table.
		"动作：沈砚停步。", "声音：沈砚：灯还亮着。", "连续性：冬装。",
		// The three FR-070 fields, which a video model is given.
		"首帧：煤灯左。", "尾帧：煤灯右。", "运动：轻微横移。",
		// The approved media, and the missing state said in WORDS rather than by an absence.
		"媒体：media-1", "媒体：未批准",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the text document does not contain %q:\n%s", want, text)
		}
	}
	// Only the third row is missing, so the "未批准" line appears exactly once: a renderer that printed
	// it for every row would make a complete board look incomplete.
	if count := strings.Count(text, "媒体：未批准"); count != 1 {
		t.Fatalf("the missing-media line appears %d times, want once", count)
	}
}

// TestFromItemDecidesTheMissingState is the constructor's own test.
//
// `MissingMedia` is derived rather than copied, and it is the one field a caller could get wrong in a
// way nothing would notice: a row with no approved media that reported otherwise would be scheduled
// as ready.
func TestFromItemDecidesTheMissingState(t *testing.T) {
	item := storyboarddomain.StoryboardItem{
		ID: "item-1", ShotID: "shot-7", Ordinal: 7, DurationSeconds: 4,
		ShotSize: "MS", VisualDescription: "煤灯。", Status: versioning.StatusApproved,
	}
	withMedia := FromItem(item, "panel-1", "media-1")
	if withMedia.MissingMedia() {
		t.Fatal("a row with an approved version reports missing media")
	}
	if withMedia.ShotID != "shot-7" || withMedia.ShotNumber != "shot-7" || withMedia.Ordinal != 7 {
		t.Fatalf("the identities did not travel: %+v", withMedia)
	}
	withoutMedia := FromItem(item, "", "")
	if !withoutMedia.MissingMedia() {
		t.Fatal("a row with no approved version does not report missing media")
	}
	// A whitespace-only identifier is not an approval either: the schema stores an empty string, and a
	// caller that passed padding would otherwise mark a row ready on nothing.
	padded := FromItem(item, "", "   ")
	if !padded.MissingMedia() {
		t.Fatal("a whitespace-only version was accepted as an approval")
	}
	// A row built LITERALLY answers the same way, which is the property the method exists for: the
	// earlier field version left this to a caller who would not know to set it.
	literal := Row{Ordinal: 1, ShotID: "shot-1", DurationSeconds: 4}
	if !literal.MissingMedia() {
		t.Fatal("a literally-built row with no approved version reports media")
	}
	literal.ApprovedVersionID = "media-9"
	if literal.MissingMedia() {
		t.Fatal("a literally-built row with an approved version reports missing media")
	}
}

// TestAnUnknownFormatIsRefused covers the boundary.
func TestAnUnknownFormatIsRefused(t *testing.T) {
	for _, format := range []Format{"", "xlsx", "pdf", "CSV"} {
		if _, err := Render(fixture(), format); err == nil {
			t.Fatalf("the format %q was accepted", format)
		}
		if IsValidFormat(format) {
			t.Fatalf("IsValidFormat says %q is valid", format)
		}
	}
	for _, format := range Formats {
		if !IsValidFormat(format) {
			t.Fatalf("IsValidFormat says the documented format %q is invalid", format)
		}
	}
	// And the two have DIFFERENT extensions, so a caller naming a file from the format cannot produce
	// a CSV called `.txt`.
	if Formats[0].ExtensionFor() == Formats[1].ExtensionFor() {
		t.Fatal("two formats share an extension")
	}
}

// TestAnEmptyBoardStillRenders covers the board created before it was filled.
//
// The two formats answer the case DIFFERENTLY, and that difference is the assertion rather than an
// oversight: the text document carries a heading and says it has no shots, while the CSV is its header
// row alone — a heading written into a CSV would BE a data row, and a spreadsheet would read "分镜版本
// v1" as the first shot's ordinal. An earlier version of this test asserted the heading in both and
// was wrong about the format it was testing.
func TestAnEmptyBoardStillRenders(t *testing.T) {
	document := Document{EpisodeTitle: "空", EpisodeNum: 1, BoardVersionNumber: 1}
	text, err := Render(document, FormatText)
	if err != nil {
		t.Fatalf("Render(text): %v", err)
	}
	for _, want := range []string{"第 1 集 空", "分镜版本 v1", "镜头 0 个，合计 0 秒"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the text document does not contain %q:\n%s", want, text)
		}
	}
	// A missing-media line would be wrong here: there are no rows to be missing anything, and a
	// document that said "1 of 0 shots has no media" would be arithmetic nobody would trust again.
	if strings.Contains(text, "尚无批准媒体") {
		t.Fatalf("an empty board reports missing media:\n%s", text)
	}

	csvText, err := Render(Document{}, FormatCSV)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(csvText)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0]) != len(csvHeader) {
		t.Fatalf("the empty board's CSV has %d rows, want the header alone", len(records))
	}
	for index, name := range csvHeader {
		if records[0][index] != name {
			t.Fatalf("column %d is %q, want %q", index, records[0][index], name)
		}
	}
}
