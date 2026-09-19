package desktop

import (
	"context"
	"strings"
	"testing"

	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appimporting "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/importing"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// These tests cover the production chapter reader, and they exist because an
// independent review found it had none.
//
// extraction_reader.go holds the one assignment that makes the evidence offsets
// version-absolute: it reports the chapter's start offset as ChapterText.BaseOffset.
// The application tests use a FAKE reader which computes that base from the
// chapter itself, so the fake and the real adapter could drift apart with every
// test still green — and a mutation setting the real one to 0, which reintroduces
// the whole coordinate defect, left the suite passing. The comment in those tests
// even claimed "the real one is covered by the desktop tests", which was false.

// chapterReaderFixture wires the real adapter over a real import service, so the
// text it reads is the text an import normalized.
type chapterReaderFixture struct {
	reader  *chapterTextReader
	story   *appstory.Service
	store   *chapterReaderStore
	reading *appimporting.Service
	files   *testFileStore
}

func newChapterReaderFixture(t *testing.T) *chapterReaderFixture {
	t.Helper()
	store := newChapterReaderStore()
	files := newTestFileStore()
	storyService := appstory.NewService(appstory.Options{
		Repository: store, Clock: fixedDramaClock{}, IDs: fixedIDs(),
	})
	reading := appimporting.NewService(appimporting.Options{
		Store: files, Story: storyService, Clock: fixedDramaClock{},
	})
	reader := NewChapterTextReader(storyService, reading).(*chapterTextReader)
	return &chapterReaderFixture{reader: reader, story: storyService, store: store, reading: reading, files: files}
}

// chapterReaderStore is a story repository holding one version and its chapters.
type chapterReaderStore struct {
	appstory.Repository
	documents map[string]storydomain.SourceDocument
	versions  map[string]storydomain.SourceDocumentVersion
	chapters  map[string]storydomain.Chapter
}

func newChapterReaderStore() *chapterReaderStore {
	return &chapterReaderStore{
		documents: map[string]storydomain.SourceDocument{},
		versions:  map[string]storydomain.SourceDocumentVersion{},
		chapters:  map[string]storydomain.Chapter{},
	}
}

func (s *chapterReaderStore) CreateSourceDocument(_ context.Context, record storydomain.SourceDocument) error {
	s.documents[record.ID] = record
	return nil
}

func (s *chapterReaderStore) GetSourceDocument(_ context.Context, id string) (storydomain.SourceDocument, error) {
	record, ok := s.documents[id]
	if !ok {
		return storydomain.SourceDocument{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *chapterReaderStore) CreateSourceDocumentVersion(_ context.Context, version storydomain.SourceDocumentVersion) error {
	s.versions[version.ID] = version
	return nil
}

func (s *chapterReaderStore) GetSourceDocumentVersion(_ context.Context, id string) (storydomain.SourceDocumentVersion, error) {
	version, ok := s.versions[id]
	if !ok {
		return storydomain.SourceDocumentVersion{}, storydomain.NotFoundError()
	}
	return version, nil
}

func (s *chapterReaderStore) CreateChapter(_ context.Context, chapter storydomain.Chapter) error {
	s.chapters[chapter.ID] = chapter
	return nil
}

func (s *chapterReaderStore) GetChapter(_ context.Context, id string) (storydomain.Chapter, error) {
	chapter, ok := s.chapters[id]
	if !ok {
		return storydomain.Chapter{}, storydomain.NotFoundError()
	}
	return chapter, nil
}

// TestChapterReaderReportsWhereTheChapterStarts is the test that was missing.
//
// The base offset is what turns a span found inside the chapter into a citation
// that indexes the VERSION, and section 6.6's reader indexes the version. A base
// of zero is only correct for a chapter that begins the document, so the fixture
// uses a chapter that starts in the middle of it.
func TestChapterReaderReportsWhereTheChapterStarts(t *testing.T) {
	fixture := newChapterReaderFixture(t)
	ctx := context.Background()

	// A document of five hundred characters, all distinct so a wrong slice is
	// visible rather than merely short.
	var builder strings.Builder
	for index := 0; index < 500; index++ {
		builder.WriteRune(rune(0x4E00 + index))
	}
	text := builder.String()
	normalized, err := fixture.files.Import(ctx, "doc.txt", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	fixture.store.documents["doc-1"] = storydomain.SourceDocument{
		ID: "doc-1", ProjectID: "project-1", Name: "The Novel",
		Type: storydomain.DocumentNovel, Status: storydomain.DocumentActive, Revision: 1,
	}
	fixture.store.versions["version-1"] = storydomain.SourceDocumentVersion{
		ID: "version-1", SourceDocumentID: "doc-1", VersionNumber: 1,
		NormalizedTextFileID: normalized.Hash, ContentHash: normalized.Hash,
		CharCount: len([]rune(text)), CreatedByType: versioning.CreatedByUser,
	}
	// A chapter that starts at 100, so its base cannot be coincidentally zero.
	const base = 100
	const end = 200
	fixture.store.chapters["chapter-1"] = storydomain.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: "version-1", Ordinal: 1, Title: "One",
		StartOffset: base, EndOffset: end, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}

	chapter, err := fixture.reader.ChapterWithText(ctx, "chapter-1")
	if err != nil {
		t.Fatalf("ChapterWithText: %v", err)
	}
	if chapter.BaseOffset != base {
		t.Fatalf("the base offset is %d, want %d — a zero here makes every evidence offset chapter-local",
			chapter.BaseOffset, base)
	}
	// The text is the chapter's own slice, which is what the base is the base OF.
	runes := []rune(text)
	if chapter.Text != string(runes[base:end]) {
		t.Fatalf("the text is not the chapter's slice of the version")
	}
	// The two together must reconstruct the version's range: text[i] is version
	// rune base+i. This is the property an extraction depends on, asserted where
	// the adapter produces it rather than where a fake imitates it.
	if len([]rune(chapter.Text)) != end-base {
		t.Fatalf("the text holds %d runes, want %d", len([]rune(chapter.Text)), end-base)
	}
	if got := string([]rune(chapter.Text)[0]); got != string(runes[base]) {
		t.Fatalf("the first rune of the text is %q, want the version's rune at %d", got, base)
	}
	// The version and the project travel with the text, because an evidence row
	// names both.
	if chapter.SourceDocumentVersionID != "version-1" {
		t.Fatalf("the version is %q", chapter.SourceDocumentVersionID)
	}
	if chapter.ProjectID != "project-1" {
		t.Fatalf("the project is %q", chapter.ProjectID)
	}
}

// TestChapterReaderRefusesAMissingChapterOrText covers the refusals, so a chapter
// that cannot be read does not produce an empty reading that looks like a result.
func TestChapterReaderRefusesAMissingChapterOrText(t *testing.T) {
	fixture := newChapterReaderFixture(t)
	ctx := context.Background()
	if _, err := fixture.reader.ChapterWithText(ctx, "no-such-chapter"); err == nil {
		t.Fatal("a chapter that does not exist was read")
	}
	// A chapter whose version has no normalized text: the reader must refuse rather
	// than return an empty string, which an extractor would read as "nothing here".
	fixture.store.documents["doc-1"] = storydomain.SourceDocument{ID: "doc-1", ProjectID: "project-1", Revision: 1}
	fixture.store.versions["version-1"] = storydomain.SourceDocumentVersion{
		ID: "version-1", SourceDocumentID: "doc-1", NormalizedTextFileID: "",
	}
	fixture.store.chapters["chapter-1"] = storydomain.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: "version-1", Ordinal: 1,
		StartOffset: 0, EndOffset: 10, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}
	if _, err := fixture.reader.ChapterWithText(ctx, "chapter-1"); err == nil {
		t.Fatal("a chapter with no normalized text was read")
	}
}

// TestChapterReaderRefusesAnUnattachedAdapter proves the adapter fails closed
// rather than panicking on a nil service, which is the rule every binding follows.
func TestChapterReaderRefusesAnUnattachedAdapter(t *testing.T) {
	reader := &chapterTextReader{}
	if _, err := reader.ChapterWithText(context.Background(), "chapter-1"); err == nil {
		t.Fatal("an adapter with no services read a chapter")
	}
}

// compile-time assertion that the test store satisfies what the adapter needs.
var _ = appfiles.Object{}
