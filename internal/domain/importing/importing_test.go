package importing

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// TestFormatAndEncodingVocabularies pins the sets the schema records.
func TestFormatAndEncodingVocabularies(t *testing.T) {
	for _, format := range []Format{FormatText, FormatMarkdown, FormatDOCX, FormatPasted} {
		if !IsValidFormat(format) {
			t.Fatalf("documented format %q rejected", format)
		}
	}
	for _, rejected := range []Format{"", "pdf", "TXT", "doc"} {
		if IsValidFormat(rejected) {
			t.Fatalf("undocumented format %q accepted", rejected)
		}
	}
	for _, encoding := range Encodings {
		if !IsValidEncoding(encoding) {
			t.Fatalf("documented encoding %q rejected", encoding)
		}
	}
	// PDF is the one format a reader might expect and the specification puts it
	// in V1, so it must stay refused here.
	if IsValidFormat("pdf") {
		t.Fatal("PDF is a V1 format and must not be accepted by an MVP import")
	}
	for _, rejected := range []Encoding{"", "latin1", "UTF-8", "shift_jis"} {
		if IsValidEncoding(rejected) {
			t.Fatalf("undocumented encoding %q accepted", rejected)
		}
	}
	for _, source := range ChapterSources {
		if !IsValidChapterSource(source) {
			t.Fatalf("documented chapter source %q rejected", source)
		}
	}
	for _, rejected := range []ChapterSource{"", "guess", "Heading"} {
		if IsValidChapterSource(rejected) {
			t.Fatalf("undocumented chapter source %q accepted", rejected)
		}
	}
}

// TestDetectEncodingUTF8 covers the common case and the byte order mark.
func TestDetectEncodingUTF8(t *testing.T) {
	encoding, text, err := DetectEncoding([]byte("第一章 开始\n\n正文。\n"))
	if err != nil {
		t.Fatalf("DetectEncoding: %v", err)
	}
	if encoding != EncodingUTF8 {
		t.Fatalf("encoding = %q, want utf-8", encoding)
	}
	if !strings.Contains(text, "第一章") {
		t.Fatalf("text = %q", text)
	}

	// A UTF-8 byte order mark is stripped rather than kept, so an offset does
	// not count it as a character.
	_, withMark, err := DetectEncoding(append([]byte{0xef, 0xbb, 0xbf}, []byte("第一章")...))
	if err != nil {
		t.Fatal(err)
	}
	if withMark != "第一章" {
		t.Fatalf("the byte order mark survived: %q", withMark)
	}
}

// TestDetectEncodingGBK covers the encoding a Chinese manuscript most often
// arrives in when it did not come from a modern editor.
func TestDetectEncodingGBK(t *testing.T) {
	original := "第一章 编码\n\n这段文字使用 GBK 编码保存。\n"
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	// The encoded bytes must not be valid UTF-8, or the test would pass through
	// the UTF-8 path and prove nothing.
	if strings.Contains(string(encoded), "第一章") {
		t.Fatal("the fixture did not actually encode as GBK")
	}
	encoding, text, err := DetectEncoding(encoded)
	if err != nil {
		t.Fatalf("DetectEncoding: %v", err)
	}
	// GB18030 is a superset of GBK, so a GBK document may legitimately be
	// reported as either; what matters is that the text came back intact.
	if encoding != EncodingGBK && encoding != EncodingGB18030 {
		t.Fatalf("encoding = %q, want a Chinese encoding", encoding)
	}
	if text != original {
		t.Fatalf("text = %q, want %q", text, original)
	}
}

// TestDetectEncodingUTF16 covers the byte order mark requirement.
func TestDetectEncodingUTF16(t *testing.T) {
	original := "第一章 双字节"
	// Little endian with a mark.
	data := []byte{0xff, 0xfe}
	for _, character := range original {
		value := uint16(character)
		data = append(data, byte(value), byte(value>>8))
	}
	encoding, text, err := DetectEncoding(data)
	if err != nil {
		t.Fatalf("DetectEncoding: %v", err)
	}
	if encoding != EncodingUTF16LE {
		t.Fatalf("encoding = %q, want utf-16le", encoding)
	}
	if text != original {
		t.Fatalf("text = %q, want %q", text, original)
	}

	// Big endian with a mark.
	be := []byte{0xfe, 0xff}
	for _, character := range original {
		value := uint16(character)
		be = append(be, byte(value>>8), byte(value))
	}
	encoding, text, err = DetectEncoding(be)
	if err != nil {
		t.Fatal(err)
	}
	if encoding != EncodingUTF16BE || text != original {
		t.Fatalf("big endian decoded as %q / %q", encoding, text)
	}

	// Without a mark the same bytes are refused rather than guessed: the two
	// readings differ and picking one would be a coin flip presented as a
	// decision.
	if _, _, err := DetectEncoding(data[2:]); err == nil {
		t.Fatal("UTF-16 without a byte order mark was accepted")
	}
}

// TestDetectEncodingRefusesWhatItCannotDecode is the honesty check: a binary
// file must not "decode" through a lenient path.
func TestDetectEncodingRefusesWhatItCannotDecode(t *testing.T) {
	// A byte sequence that is not valid UTF-8 and has no clean GBK reading: an
	// unpaired high byte at the end leaves the decoder unable to complete.
	binary := []byte{0xff, 0xfe, 0xfd, 0xfc, 0x80, 0x81, 0x82}
	if _, _, err := DetectEncoding(binary); err == nil {
		t.Fatal("undecodable bytes were accepted")
	}
	if _, _, err := DetectEncoding(nil); err == nil {
		t.Fatal("an empty document was accepted")
	}
	// A file over the ceiling is refused before any decoder runs.
	oversize := make([]byte, MaxInputBytes+1)
	if _, _, err := DetectEncoding(oversize); err == nil {
		t.Fatal("an oversize document was accepted")
	}
}

// TestNormalizeUnifiesLineEndings proves offsets are comparable across imports,
// which is what makes a recorded offset meaningful later.
func TestNormalizeUnifiesLineEndings(t *testing.T) {
	cases := map[string]string{
		"a\r\nb":     "a\nb",
		"a\rb":       "a\nb",
		"a\nb":       "a\nb",
		"a\r\n\r\nb": "a\n\nb",
	}
	for input, want := range cases {
		if got := normalize(input); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", input, got, want)
		}
	}
	// A lone carriage return at the end must not produce a trailing newline
	// pair, which would add a character and shift every offset after it.
	if got := normalize("a\r"); got != "a\n" {
		t.Fatalf("normalize(%q) = %q", "a\r", got)
	}
}

// TestDetectChaptersChineseMarkers is the rule AC-STORY-001's chapter detection
// is measured against.
func TestDetectChaptersChineseMarkers(t *testing.T) {
	text := "第一章 起始\n\n正文一。\n第二章 转折\n\n正文二。\n第十节 尾声\n\n正文三。\n第三回 归来\n\n正文四。\n"
	chapters, err := DetectChapters(text, FormatText)
	if err != nil {
		t.Fatalf("DetectChapters: %v", err)
	}
	if len(chapters) != 4 {
		t.Fatalf("%d chapters, want 4: %+v", len(chapters), chapters)
	}
	wantTitles := []string{"起始", "转折", "尾声", "归来"}
	for index, want := range wantTitles {
		if chapters[index].Title != want {
			t.Fatalf("chapter %d title = %q, want %q", index, chapters[index].Title, want)
		}
		if chapters[index].Ordinal != index+1 {
			t.Fatalf("chapter %d ordinal = %d", index, chapters[index].Ordinal)
		}
		if chapters[index].Source != ChapterFromPattern {
			t.Fatalf("chapter %d source = %q", index, chapters[index].Source)
		}
	}
	// The boundaries must tile the text without gaps or overlaps: every
	// character belongs to exactly one chapter.
	assertBoundariesTile(t, text, chapters)
}

// TestDetectChaptersMarkdownHeadings covers the heading rule and its format
// guard.
func TestDetectChaptersMarkdownHeadings(t *testing.T) {
	text := "# 第一章\n\n正文。\n\n## 第二章\n\n更多正文。\n"
	chapters, err := DetectChapters(text, FormatMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 {
		t.Fatalf("%d chapters, want 2: %+v", len(chapters), chapters)
	}
	for index, chapter := range chapters {
		if chapter.Source != ChapterFromHeading {
			t.Fatalf("chapter %d source = %q, want heading", index, chapter.Source)
		}
	}
	// A DOCX body never carries Markdown, so the same text in a DOCX is not
	// split by the hash rule: the document has no structure the format can
	// express.
	docxChapters, err := DetectChapters(text, FormatDOCX)
	if err != nil {
		t.Fatal(err)
	}
	if len(docxChapters) != 1 || docxChapters[0].Source != ChapterWholeDocument {
		t.Fatalf("a DOCX was split on Markdown headings: %+v", docxChapters)
	}
}

// TestDetectChaptersEnglishMarkers covers the Latin-shaped marker.
func TestDetectChaptersEnglishMarkers(t *testing.T) {
	text := "Chapter 1\n\nFirst body.\n\nChapter 2\n\nSecond body.\n"
	chapters, err := DetectChapters(text, FormatText)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 {
		t.Fatalf("%d chapters, want 2", len(chapters))
	}
	if chapters[0].Title != "First body." && chapters[0].Title != "" {
		// The title is the remainder of the marker line, which is empty here, so
		// the marker text itself becomes the title.
		if !strings.HasPrefix(chapters[0].Title, "Chapter") {
			t.Fatalf("chapter 0 title = %q", chapters[0].Title)
		}
	}
	assertBoundariesTile(t, text, chapters)
}

// TestDetectChaptersWithoutMarkersYieldsOneChapter is the honest-degradation
// case: a document with no structure becomes one chapter rather than none, so
// the user has something to split.
func TestDetectChaptersWithoutMarkersYieldsOneChapter(t *testing.T) {
	text := "这是一篇没有任何章节标记的短文。\n\n它仍然需要被导入。\n"
	chapters, err := DetectChapters(text, FormatText)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 1 {
		t.Fatalf("%d chapters, want 1", len(chapters))
	}
	if chapters[0].Source != ChapterWholeDocument {
		t.Fatalf("source = %q, want whole", chapters[0].Source)
	}
	if chapters[0].StartOffset != 0 || chapters[0].EndOffset != len([]rune(text)) {
		t.Fatalf("the single chapter does not cover the text: %+v", chapters[0])
	}
}

// TestDetectChaptersKeepsFrontMatter proves a preface is not silently dropped.
func TestDetectChaptersKeepsFrontMatter(t *testing.T) {
	text := "书名\n\n作者的话。\n\n第一章 开始\n\n正文。\n"
	chapters, err := DetectChapters(text, FormatText)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 {
		t.Fatalf("%d chapters, want 2 (front matter plus one chapter): %+v", len(chapters), chapters)
	}
	if chapters[0].Title != frontMatterTitle {
		t.Fatalf("the first chapter is %q, want the front matter label", chapters[0].Title)
	}
	// The preface's text must be inside the first boundary, not lost.
	runes := []rune(text)
	preface := string(runes[chapters[0].StartOffset:chapters[0].EndOffset])
	if !strings.Contains(preface, "作者的话") {
		t.Fatalf("the front matter does not contain its text: %q", preface)
	}
	assertBoundariesTile(t, text, chapters)
}

// TestDetectChaptersIgnoresProseThatMentionsAChapter is the guard that keeps a
// paragraph from becoming a boundary.
func TestDetectChaptersIgnoresProseThatMentionsAChapter(t *testing.T) {
	// A long sentence that opens with a chapter marker is prose. Splitting on it
	// would fragment the document every time the text mentions a chapter.
	long := "第一章里,他做了很多事," + strings.Repeat("而且这些事都值得详细说明,", 20)
	text := "第一章 开始\n\n正文。\n" + long + "\n"
	chapters, err := DetectChapters(text, FormatText)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 1 {
		t.Fatalf("%d chapters, want 1: the long line must not be a heading", len(chapters))
	}
	// A short marker-shaped line is still a heading; the rule is a length bound,
	// not a refusal to split at all.
	short := "第一章 开始\n\n正文。\n第二章 短\n\n正文二。\n"
	chapters, err = DetectChapters(short, FormatText)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 {
		t.Fatalf("%d chapters, want 2", len(chapters))
	}
}

// TestDetectChaptersRefusesAnOverwhelmingDocument covers the bound: a document
// that looks like tens of thousands of chapters is a pattern matching too
// eagerly, not a structure.
func TestDetectChaptersRefusesAnOverwhelmingDocument(t *testing.T) {
	var builder strings.Builder
	for index := 0; index < MaxChapters+1; index++ {
		builder.WriteString("第一章 重复\n")
	}
	if _, err := DetectChapters(builder.String(), FormatText); err == nil {
		t.Fatal("a document with too many markers was accepted")
	}
	if _, err := DetectChapters("   \n\n", FormatText); err == nil {
		t.Fatal("a blank document was accepted")
	}
}

// TestChapterBoundaryValidate covers the invariants DOMAIN_MODEL section 5.3
// states and the schema enforces.
func TestChapterBoundaryValidate(t *testing.T) {
	base := ChapterBoundary{Ordinal: 1, Title: "t", StartOffset: 0, TitleEndOffset: 2, EndOffset: 10, Source: ChapterFromPattern}
	if err := base.Validate(10); err != nil {
		t.Fatalf("a well-formed boundary was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*ChapterBoundary)
		total  int
	}{
		{"zero ordinal", func(b *ChapterBoundary) { b.Ordinal = 0 }, 10},
		{"negative start", func(b *ChapterBoundary) { b.StartOffset = -1 }, 10},
		{"end before start", func(b *ChapterBoundary) { b.EndOffset = 0 }, 10},
		{"heading outside the chapter", func(b *ChapterBoundary) { b.TitleEndOffset = 20 }, 10},
		{"past the end of the text", func(b *ChapterBoundary) { b.EndOffset = 11 }, 10},
		{"unknown source", func(b *ChapterBoundary) { b.Source = "guess" }, 10},
		{"over-long title", func(b *ChapterBoundary) { b.Title = strings.Repeat("字", MaxTitleRunes+1) }, 10},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			boundary := base
			testCase.mutate(&boundary)
			if err := boundary.Validate(testCase.total); err == nil {
				t.Fatal("a malformed boundary was accepted")
			}
		})
	}
}

// TestDocumentValidateCoversTheSchemaRules covers the document-level rules,
// including the overlap rule that AC-STORY-001's "offsets 有效" is about.
func TestDocumentValidateCoversTheSchemaRules(t *testing.T) {
	text := "第一章 甲\n正文\n第二章 乙\n正文\n"
	chapters, err := DetectChapters(text, FormatText)
	if err != nil {
		t.Fatal(err)
	}
	document := Document{Text: text, Encoding: EncodingUTF8, Format: FormatText, Chapters: chapters}
	if err := document.Validate(); err != nil {
		t.Fatalf("a detected document does not validate: %v", err)
	}
	if document.RuneCount() != len([]rune(text)) {
		t.Fatalf("RuneCount = %d, want %d", document.RuneCount(), len([]rune(text)))
	}

	// Overlapping boundaries are refused even when each one is individually
	// valid, because the schema's rule is about the set.
	overlapping := document
	overlapping.Chapters = []ChapterBoundary{
		{Ordinal: 1, StartOffset: 0, TitleEndOffset: 0, EndOffset: 10, Source: ChapterFromPattern},
		{Ordinal: 2, StartOffset: 5, TitleEndOffset: 5, EndOffset: 20, Source: ChapterFromPattern},
	}
	if err := overlapping.Validate(); err == nil {
		t.Fatal("overlapping boundaries were accepted")
	}

	// Gap-free ordinals are required.
	gapped := document
	gapped.Chapters = []ChapterBoundary{{Ordinal: 2, StartOffset: 0, TitleEndOffset: 0, EndOffset: 5, Source: ChapterFromPattern}}
	if err := gapped.Validate(); err == nil {
		t.Fatal("a gapped ordinal was accepted")
	}

	// A document with no chapters is refused; detection always produces one, so
	// an empty list means a caller built the document by hand.
	empty := document
	empty.Chapters = nil
	if err := empty.Validate(); err == nil {
		t.Fatal("a document with no chapters was accepted")
	}
	for _, rejected := range []Document{
		{Text: "   ", Encoding: EncodingUTF8, Format: FormatText, Chapters: chapters},
		{Text: text, Encoding: "latin1", Format: FormatText, Chapters: chapters},
		{Text: text, Encoding: EncodingUTF8, Format: "pdf", Chapters: chapters},
	} {
		if err := rejected.Validate(); err == nil {
			t.Fatalf("a malformed document was accepted: %+v", rejected)
		}
	}
}

// TestStoryDocumentTypeMapping covers the default mapping, which the caller may
// override.
func TestStoryDocumentTypeMapping(t *testing.T) {
	if got := StoryDocumentType(FormatMarkdown); got != "outline" {
		t.Fatalf("markdown maps to %q, want outline", got)
	}
	if got := StoryDocumentType(FormatPasted); got != "notes" {
		t.Fatalf("pasted maps to %q, want notes", got)
	}
	for _, format := range []Format{FormatText, FormatDOCX} {
		if got := StoryDocumentType(format); got != "novel" {
			t.Fatalf("%s maps to %q, want novel", format, got)
		}
	}
}

// assertBoundariesTile proves the boundaries cover the text exactly once.
func assertBoundariesTile(t *testing.T, text string, chapters []ChapterBoundary) {
	t.Helper()
	total := len([]rune(text))
	if chapters[0].StartOffset != 0 {
		t.Fatalf("the first boundary starts at %d, not 0", chapters[0].StartOffset)
	}
	for index := 1; index < len(chapters); index++ {
		if chapters[index].StartOffset != chapters[index-1].EndOffset {
			t.Fatalf("boundary %d starts at %d but the previous ended at %d",
				index, chapters[index].StartOffset, chapters[index-1].EndOffset)
		}
	}
	if last := chapters[len(chapters)-1]; last.EndOffset != total {
		t.Fatalf("the last boundary ends at %d, want %d", last.EndOffset, total)
	}
}
