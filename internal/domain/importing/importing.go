// Package importing owns the document-import vocabulary and the pure rules that
// turn raw bytes into a normalized document with chapter boundaries.
//
// It implements docs/ROADMAP.md scope items 1-5 (TXT/Markdown/DOCX import,
// encoding and normalization, chapter detection, offsets, large-text chunking)
// to the extent those are decisions rather than I/O. Everything here is a pure
// function over bytes and text: no file is opened, no database is touched, and
// no clock is read. The application layer owns the orchestration that stores the
// results.
//
// Why the rules live in the domain: AGENT_CONTRACTS section 17 puts "章节 offset"
// (chapter offsets) on the list of things that must be code rather than a model
// call. A boundary that a model guessed is not reproducible, and every fact
// downstream cites a chapter offset as its evidence.
//
// The package performs no I/O and never mints an identifier (ADR-0005).
package importing

import (
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// Format is the container a document arrived in.
type Format string

const (
	// FormatText is a plain text file. Everything that is not recognised as
	// another format lands here, because a text file has no signature to check.
	FormatText Format = "txt"
	// FormatMarkdown is text whose headings may carry chapter structure.
	FormatMarkdown Format = "markdown"
	// FormatDOCX is an Office Open XML document, which is a ZIP container.
	FormatDOCX Format = "docx"
	// FormatPasted is text a user typed or pasted rather than uploaded. It has
	// no original file, which is why the schema allows an empty physical file
	// reference.
	FormatPasted Format = "pasted"
)

// Formats lists the documented formats in a stable order.
var Formats = []Format{FormatText, FormatMarkdown, FormatDOCX, FormatPasted}

// IsValidFormat reports whether a format may be stored.
func IsValidFormat(value Format) bool {
	for _, candidate := range Formats {
		if candidate == value {
			return true
		}
	}
	return false
}

// Encoding is the character encoding a document was decoded from.
//
// The set is deliberately small. PRD FR-020 requires 编码检测 (encoding
// detection) without naming a set, and every encoding admitted here is one the
// decoder can actually handle and the schema can record. UTF-16 needs a byte
// order mark to be distinguishable from arbitrary binary, so it is only
// accepted with one.
type Encoding string

const (
	EncodingUTF8    Encoding = "utf-8"
	EncodingUTF16LE Encoding = "utf-16le"
	EncodingUTF16BE Encoding = "utf-16be"
	// EncodingGBK covers the GBK and GB2312 family. GB18030 is a superset, so a
	// document decoded as GB18030 is recorded as GB18030 rather than guessed
	// down to GBK: the decoder cannot tell which one produced the bytes.
	EncodingGBK     Encoding = "gbk"
	EncodingGB18030 Encoding = "gb18030"
)

// Encodings lists the documented encodings in a stable order.
var Encodings = []Encoding{EncodingUTF8, EncodingUTF16LE, EncodingUTF16BE, EncodingGBK, EncodingGB18030}

// IsValidEncoding reports whether an encoding may be recorded.
func IsValidEncoding(value Encoding) bool {
	for _, candidate := range Encodings {
		if candidate == value {
			return true
		}
	}
	return false
}

// Limits bounds what one import may accept.
//
// The numbers are chosen against the specification's own floors rather than
// invented: AC-STORY-001 imports at least 30,000 Chinese characters and the
// performance target is 100,000, so the input ceiling has to clear both by a
// wide margin. A novel that exceeds it is a hostile or mistaken input rather
// than a large one.
const (
	// MaxInputBytes bounds the raw upload. 64 MiB is far above any novel's text
	// and far below what would exhaust memory during normalization.
	MaxInputBytes int64 = 64 << 20
	// MaxChapters bounds how many boundaries one document may produce. A
	// document that looks like 100,000 chapters is a pattern that matched too
	// eagerly, not a structure.
	MaxChapters = 5000
	// MaxTitleRunes mirrors the schema's CHECK on chapters.title and
	// story_* names.
	MaxTitleRunes = 200
	// MaxHeadingLineRunes bounds how long a line may be and still count as a
	// heading. Without it, a paragraph that happens to start with "第一章" would
	// split the document.
	MaxHeadingLineRunes = 120
	// ReadPageBytes bounds one page of source text handed to the UI.
	//
	// SECURITY section 7.5 requires 大文本分页读取 (paged reading of large text).
	// 64 KiB is roughly 20,000 Chinese characters in UTF-8, so a 100,000
	// character document is five pages: few enough that paging is invisible and
	// small enough that one response cannot stall a renderer.
	ReadPageBytes = 64 << 10
	// MaxExcerptRunes bounds a quote stored as evidence. DOMAIN_MODEL section 6.6
	// allows 有限摘录 (a limited excerpt) for verification but forbids copying a
	// whole chapter into the database.
	MaxExcerptRunes = 200
)

// ChapterSource records how a boundary was decided. It is a queryable column
// rather than display metadata, because "did the user edit this" is a question
// the import report and the staleness walk both ask.
type ChapterSource string

const (
	// ChapterFromHeading is a Markdown ATX heading.
	ChapterFromHeading ChapterSource = "heading"
	// ChapterFromPattern is a text pattern: 第N章, Chapter N.
	ChapterFromPattern ChapterSource = "regex"
	// ChapterWholeDocument is the single implicit boundary a document with no
	// markers gets, so the user has something to split rather than nothing.
	ChapterWholeDocument ChapterSource = "whole"
	// ChapterManual is a boundary the user edited or created.
	ChapterManual ChapterSource = "manual"
)

// ChapterSources lists the documented sources in a stable order.
var ChapterSources = []ChapterSource{ChapterFromHeading, ChapterFromPattern, ChapterWholeDocument, ChapterManual}

// IsValidChapterSource reports whether a source may be stored.
func IsValidChapterSource(value ChapterSource) bool {
	for _, candidate := range ChapterSources {
		if candidate == value {
			return true
		}
	}
	return false
}

// ChapterBoundary is one detected chapter.
//
// Offsets are rune offsets into the normalized text, not byte offsets. The
// schema stores integers and the UI slices by character, and mixing the two is
// the classic way to corrupt a Chinese document: a byte offset would land in
// the middle of a multi-byte character.
type ChapterBoundary struct {
	// Ordinal starts at one and increases by one across the document.
	Ordinal int
	// Title is the heading line's text with any marker removed.
	Title string
	// StartOffset is where the chapter begins: the start of its heading line.
	StartOffset int
	// TitleEndOffset is where the heading line ends and the body begins. It
	// equals StartOffset for a boundary with no heading.
	TitleEndOffset int
	// EndOffset is where the chapter ends: the start of the next boundary, or
	// the length of the text for the last one.
	EndOffset int
	Source    ChapterSource
}

// Validate checks a boundary against the invariants DOMAIN_MODEL section 5.3
// states and the schema enforces.
func (b ChapterBoundary) Validate(textRunes int) error {
	if b.Ordinal < 1 {
		return InvalidError("A chapter ordinal starts at one.")
	}
	if b.StartOffset < 0 {
		return InvalidError("A chapter cannot start before the text does.")
	}
	if b.EndOffset < b.StartOffset {
		return InvalidError("A chapter cannot end before it starts.")
	}
	if b.TitleEndOffset < b.StartOffset || b.TitleEndOffset > b.EndOffset {
		return InvalidError("A chapter's heading must fall inside the chapter.")
	}
	// Section 5.3: "offset 不重叠且在规范化文本长度内".
	if b.EndOffset > textRunes {
		return InvalidError("A chapter cannot end past the end of the text.")
	}
	if len([]rune(b.Title)) > MaxTitleRunes {
		return InvalidError("The chapter title is too long.")
	}
	if !IsValidChapterSource(b.Source) {
		return InvalidError("The chapter source is not recognised.")
	}
	return nil
}

// Document is a decoded, normalized document with its detected boundaries.
type Document struct {
	// Text is the normalized text: line endings unified, byte order mark
	// removed, Unicode composed (NFC).
	Text string
	// Encoding is what the bytes were decoded from.
	Encoding Encoding
	// Format is the container they arrived in.
	Format Format
	// Chapters are the detected boundaries, in order.
	Chapters []ChapterBoundary
}

// RuneCount is the document's length in characters, which is what the schema's
// char_count records and what every offset is measured in.
func (d Document) RuneCount() int {
	return len([]rune(d.Text))
}

// Validate checks the document's internal consistency.
func (d Document) Validate() error {
	if !IsValidFormat(d.Format) {
		return InvalidError("The document format is not recognised.")
	}
	if !IsValidEncoding(d.Encoding) {
		return InvalidError("The document encoding is not recognised.")
	}
	if strings.TrimSpace(d.Text) == "" {
		return InvalidError("The document has no text.")
	}
	total := d.RuneCount()
	if len(d.Chapters) == 0 {
		return InvalidError("A document needs at least one chapter boundary.")
	}
	if len(d.Chapters) > MaxChapters {
		return InvalidError("The document has more chapters than an import accepts.")
	}
	previousEnd := 0
	for index, chapter := range d.Chapters {
		if chapter.Ordinal != index+1 {
			return InvalidError("Chapter ordinals must run from one without gaps.")
		}
		if err := chapter.Validate(total); err != nil {
			return err
		}
		// Section 5.3: "offset 不重叠". A boundary that starts before the previous
		// one ended would make two chapters claim the same text.
		if chapter.StartOffset < previousEnd {
			return InvalidError("Chapter boundaries must not overlap.")
		}
		previousEnd = chapter.EndOffset
	}
	return nil
}

// StoryDocumentType maps an import format onto the story domain's document type.
//
// The two vocabularies answer different questions: a format is how the bytes are
// shaped, a document type is what the text is for. A pasted fragment is notes
// until the user says otherwise, and Markdown is a novel as often as it is a
// screenplay, so the mapping is a default the caller may override rather than a
// derivation.
func StoryDocumentType(format Format) story.DocumentType {
	switch format {
	case FormatMarkdown:
		return story.DocumentOutline
	case FormatPasted:
		return story.DocumentNotes
	default:
		return story.DocumentNovel
	}
}
