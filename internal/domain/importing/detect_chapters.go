package importing

import (
	"regexp"
	"strings"
)

// detect_chapters.go holds the chapter-boundary rules.
//
// AGENT_CONTRACTS section 17 puts chapter offsets on the list of things that must
// be code rather than a model call, and DOMAIN_MODEL section 5.3 requires the
// offsets to be non-overlapping and inside the text. So detection is one pure
// function that returns boundaries a caller can validate against the same rules
// the schema enforces.
//
// The patterns cover the three shapes a Chinese or English manuscript actually
// uses. They are anchored to the start of a line and bounded in length, because
// an unanchored search for "第一章" would split a paragraph that merely mentions
// chapter one — and a boundary that moves when the prose changes is worse than
// no boundary.

// chapterPatterns are tried in order. The first match on a line wins, so a
// Markdown heading is never also read as a text pattern.
//
// titleGroup names the capture group holding the human-readable title. Naming
// it per pattern is necessary because the groups differ and "the last non-empty
// group" is wrong: a bare "Chapter 1" has an empty title group, so that rule
// would return the numeral "1" as the chapter's name.
var chapterPatterns = []struct {
	source     ChapterSource
	pattern    *regexp.Regexp
	titleGroup int
}{
	// Markdown ATX headings: one to six hashes, then the title. Group 2 is the
	// title, group 1 the hashes.
	{ChapterFromHeading, regexp.MustCompile(`^(#{1,6})[ \t]+(\S.*)$`), 2},
	// Chinese chapter markers: 第一章, 第 1 章, 第十二节, 第三回. The number may
	// be Arabic or Chinese numerals, and the unit is 章 (chapter), 节 (section)
	// or 回 (instalment), which are the three a serialised novel uses. Group 3
	// is the title and is often absent.
	{ChapterFromPattern, regexp.MustCompile(`^第\s*([0-9]+|[零一二三四五六七八九十百千两]+)\s*([章节回])\s*[：:、\.]?\s*(\S.*)?$`), 3},
	// English chapter markers: Chapter 1, CHAPTER XII, Part 2.
	{ChapterFromPattern, regexp.MustCompile(`(?i)^(chapter|part)\s+([0-9]+|[ivxlcdm]+)\s*[：:、\.]?\s*(\S.*)?$`), 3},
	// Chinese volume dividers: 第一卷. Treated as a chapter because the schema
	// has one level, and a volume boundary is the only structure the text names.
	{ChapterFromPattern, regexp.MustCompile(`^第\s*([0-9]+|[零一二三四五六七八九十百千两]+)\s*卷\s*[：:、\.]?\s*(\S.*)?$`), 3},
}

// DetectChapters finds chapter boundaries in normalized text.
//
// It always returns at least one boundary. A document with no markers becomes a
// single chapter covering the whole text, which is the honest answer: the user
// has something to split rather than an empty list, and the merge/split UI has a
// starting point.
func DetectChapters(text string, format Format) ([]ChapterBoundary, error) {
	if strings.TrimSpace(text) == "" {
		return nil, InvalidError("The document has no text to split into chapters.")
	}
	lines := splitLines(text)
	total := len([]rune(text))

	// Collect the heading lines first, as rune offsets.
	type heading struct {
		start  int
		end    int
		title  string
		source ChapterSource
	}
	var headings []heading
	offset := 0
	for _, line := range lines {
		lineRunes := len([]rune(line))
		if candidate, ok := matchHeading(line, format); ok {
			headings = append(headings, heading{
				start: offset,
				end:   offset + lineRunes,
				// The title excludes any newline, which splitLines has already
				// removed, so the offsets below stay inside the line.
				title:  candidate.title,
				source: candidate.source,
			})
		}
		// +1 for the newline splitLines removed, except after the last line.
		offset += lineRunes + 1
	}
	if len(headings) > MaxChapters {
		return nil, InvalidError("The document has more chapter markers than an import accepts.")
	}

	if len(headings) == 0 {
		return []ChapterBoundary{{
			Ordinal:        1,
			Title:          firstLineTitle(text),
			StartOffset:    0,
			TitleEndOffset: 0,
			EndOffset:      total,
			Source:         ChapterWholeDocument,
		}}, nil
	}

	// Text before the first heading becomes its own chapter rather than being
	// dropped. A preface is content, and silently discarding it would lose text
	// the user can see in the preview.
	boundaries := make([]ChapterBoundary, 0, len(headings)+1)
	if headings[0].start > 0 {
		prefix := strings.TrimSpace(string([]rune(text)[:headings[0].start]))
		if prefix != "" {
			boundaries = append(boundaries, ChapterBoundary{
				Ordinal:        1,
				Title:          frontMatterTitle,
				StartOffset:    0,
				TitleEndOffset: 0,
				EndOffset:      headings[0].start,
				Source:         ChapterWholeDocument,
			})
		}
	}
	for index, item := range headings {
		end := total
		if index+1 < len(headings) {
			end = headings[index+1].start
		}
		boundaries = append(boundaries, ChapterBoundary{
			Ordinal:        len(boundaries) + 1,
			Title:          item.title,
			StartOffset:    item.start,
			TitleEndOffset: item.end,
			EndOffset:      end,
			Source:         item.source,
		})
	}
	// Ordinals are assigned above in order, so a gap is impossible; this
	// re-numbers defensively rather than trusting that reasoning silently.
	for index := range boundaries {
		boundaries[index].Ordinal = index + 1
	}
	return boundaries, nil
}

// frontMatterTitle labels the chapter that holds text appearing before the first
// marker. It is a fixed string rather than the text's first line because the
// user is about to rename it anyway, and a title lifted from prose reads like a
// real chapter name in a list.
const frontMatterTitle = "(front matter)"

// matchHeading reports whether a line is a chapter heading and what its title is.
//
// The length bound is the guard that keeps a paragraph from becoming a heading:
// a line longer than MaxHeadingLineRunes is prose even if it opens with a
// marker, which is what a sentence like "第一章里,他..." would otherwise be.
func matchHeading(line string, format Format) (struct {
	title  string
	source ChapterSource
}, bool) {
	var result struct {
		title  string
		source ChapterSource
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || len([]rune(trimmed)) > MaxHeadingLineRunes {
		return result, false
	}
	for _, candidate := range chapterPatterns {
		// Markdown headings are only meaningful in a Markdown document or in
		// plain text a user happened to write with hashes. A DOCX body never
		// contains them, and treating a stray "#" as structure would invent a
		// boundary the document does not have.
		if candidate.source == ChapterFromHeading && format == FormatDOCX {
			continue
		}
		match := candidate.pattern.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		title := headingTitle(match, candidate.titleGroup)
		if title == "" {
			// A bare marker with no title: keep the marker text as the title so
			// the list shows something the user recognises.
			title = trimmed
		}
		result.title = title
		result.source = candidate.source
		return result, true
	}
	return result, false
}

// headingTitle reads the group a pattern named as its title.
//
// The groups differ per pattern, so the pattern says which one holds the title.
// Reading "the last non-empty group" instead would return the chapter numeral
// for a bare "Chapter 1", whose title group is empty.
func headingTitle(groups []string, titleGroup int) string {
	if titleGroup <= 0 || titleGroup >= len(groups) {
		return ""
	}
	return strings.TrimSpace(groups[titleGroup])
}

// splitLines splits on LF only, because the text is already normalized.
func splitLines(text string) []string {
	return strings.Split(text, "\n")
}

// firstLineTitle picks a title for a document with no markers: its first
// non-empty line, clipped. It gives the single chapter a name the user can
// recognise in a list.
func firstLineTitle(text string) string {
	for _, line := range splitLines(text) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		runes := []rune(trimmed)
		if len(runes) > MaxTitleRunes {
			return strings.TrimSpace(string(runes[:MaxTitleRunes])) + "…"
		}
		return trimmed
	}
	return ""
}
