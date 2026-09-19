package desktop

import (
	"context"
	"strings"

	appextraction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/extraction"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	importdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
)

// extraction_reader.go adapts the story graph and the import service into the
// extraction service's input port.
//
// It lives in the desktop layer because it is the one place that can see both
// services. The extraction package declares the capability it needs and knows
// neither, which is what keeps it testable without a database.
//
// The walk is chapter → version → document, because a Chapter row carries only
// its version id. The TEXT is not read here: the import service already owns
// reading a version's normalized text, including the rune slicing and the
// clamping a chapter's offsets need, so this adapter delegates rather than
// opening the same object a second time. Two readers of the same column would be
// two places for the byte-versus-character rule to be got wrong.

// chapterTextReader reads a chapter and the text it indexes.
type chapterTextReader struct {
	story   *appstory.Service
	reading readingService
}

// readingService is the part of the import service this adapter uses.
//
// It is an interface declared here rather than the concrete service so the
// adapter can be exercised without a file store, and so it is obvious that the
// only thing it does with the import service is read a chapter's text.
type readingService interface {
	ChapterText(ctx context.Context, versionID string, startRune, endRune int) (string, error)
}

// NewChapterTextReader builds the adapter.
func NewChapterTextReader(story *appstory.Service, reading readingService) appextraction.ChapterReader {
	return &chapterTextReader{story: story, reading: reading}
}

// ChapterWithText returns one chapter with its text and its project.
func (r *chapterTextReader) ChapterWithText(ctx context.Context, chapterID string) (appextraction.ChapterText, error) {
	if r == nil || r.story == nil || r.reading == nil {
		return appextraction.ChapterText{}, importdomain.StorageError("The chapter could not be read.", nil)
	}
	chapter, err := r.story.GetChapter(ctx, strings.TrimSpace(chapterID))
	if err != nil {
		return appextraction.ChapterText{}, err
	}
	version, err := r.story.GetSourceDocumentVersion(ctx, chapter.SourceDocumentVersionID)
	if err != nil {
		return appextraction.ChapterText{}, err
	}
	document, err := r.story.GetSourceDocument(ctx, version.SourceDocumentID)
	if err != nil {
		return appextraction.ChapterText{}, err
	}
	// The chapter's own span is the range. The import service clamps it to the
	// text that exists and refuses a chapter larger than one reading may cover,
	// so an offset that ran past the end is reported rather than silently
	// producing a short read.
	text, err := r.reading.ChapterText(ctx, version.ID, chapter.StartOffset, chapter.EndOffset)
	if err != nil {
		return appextraction.ChapterText{}, err
	}
	return appextraction.ChapterText{
		Chapter:                 chapter,
		ProjectID:               document.ProjectID,
		SourceDocumentVersionID: version.ID,
		Text:                    text,
		Language:                languageOf(document.Name, chapter.Title),
	}, nil
}

// languageOf reports the language an extractor should answer in.
//
// It is a heuristic and deliberately a weak one: the only thing it decides is
// which language a summary is written in, and getting it wrong costs a summary
// in the other language rather than a wrong fact. It reads values the caller
// already has, so it needs no second field and no configuration.
func languageOf(name, title string) string {
	for _, value := range []string{title, name} {
		for _, character := range value {
			if character >= 0x4E00 && character <= 0x9FFF {
				return "zh"
			}
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') {
				return "en"
			}
		}
	}
	// Unknown is reported as empty rather than guessed at.
	return ""
}
