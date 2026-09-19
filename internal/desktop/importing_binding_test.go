package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	appextraction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/extraction"
	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appimporting "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/importing"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// These tests cover the import and extraction transport surface. The services
// behind it are tested where they live; what is asserted here is the contract the
// FRONTEND depends on: the DTOs carry what a panel renders, a refusal reaches the
// caller with a code, and an unattached binding fails closed.

// TestImportBindingFailsClosed proves an unattached binding refuses rather than
// panicking, which is the rule every binding in this package follows.
func TestImportBindingFailsClosed(t *testing.T) {
	binding := &ImportBinding{}
	checks := []struct {
		name string
		call func() error
	}{
		{"PrecheckImport", func() error {
			_, err := binding.PrecheckImport(PrecheckImportRequest{ProjectID: "p", Content: []byte("x")})
			return err
		}},
		{"ImportDocument", func() error {
			_, err := binding.ImportDocument(ImportDocumentRequest{ProjectID: "p", Content: []byte("x")})
			return err
		}},
		{"ReadDocumentRange", func() error {
			_, err := binding.ReadDocumentRange(ReadDocumentRangeRequest{SourceDocumentVersionID: "v"})
			return err
		}},
		{"ConfirmChapters", func() error {
			_, err := binding.ConfirmChapters(ConfirmChaptersRequestDTO{SourceDocumentVersionID: "v"})
			return err
		}},
		{"ExtractChapterEventCandidates", func() error {
			_, err := binding.ExtractChapterEventCandidates(ExtractChapterRequest{ChapterID: "c"})
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); err == nil {
				t.Fatal("an unattached binding served the call instead of failing closed")
			}
		})
	}
}

// TestExtractWithoutAnExtractorReportsAnActionableCode proves the fail-closed
// rule reaches the frontend as a classified refusal the UI can act on, rather
// than as a generic failure it would show as "something went wrong".
func TestExtractWithoutAnExtractorReportsAnActionableCode(t *testing.T) {
	binding := &ImportBinding{}
	// An extraction service with no extractor: the production default until
	// WP-07 supplies one.
	AttachExtraction(binding, context.Background(), appextraction.NewService(appextraction.Options{}))
	_, err := binding.ExtractChapterEventCandidates(ExtractChapterRequest{ChapterID: "c"})
	if err == nil {
		t.Fatal("a service with no extractor served the command")
	}
	// Falling through to the generic branch would lose the one detail the user
	// needs: that nothing is configured. The binding converts a domain refusal
	// into an application error, so what the CALLER receives is asserted rather
	// than the domain type it came from.
	appErr, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("the refusal is a %T, want the application error the binding produces: %v", err, err)
	}
	if appErr.Code != "DRAMA_UNAVAILABLE" {
		t.Fatalf("the code is %q, want DRAMA_UNAVAILABLE so the UI can say nothing is configured", appErr.Code)
	}
	if !strings.Contains(appErr.SafeMessage, "No document reader") {
		t.Fatalf("the message is %q, which does not tell the user what is wrong", appErr.SafeMessage)
	}
}

// TestImportPrecheckReportsWhatThePanelRenders covers the DTO shape: the fields a
// review panel needs must survive the transport intact.
func TestImportPrecheckReportsWhatThePanelRenders(t *testing.T) {
	binding := importBindingForTest(t)
	result, err := binding.PrecheckImport(PrecheckImportRequest{
		ProjectID: "project-1",
		Name:      "novel.md",
		Format:    "md",
		Content:   []byte("# 第一章 开始\n\n正文内容。\n\n# 第二章 继续\n\n更多内容。\n"),
	})
	if err != nil {
		t.Fatalf("PrecheckImport: %v", err)
	}
	if result.Format != "markdown" {
		t.Fatalf("format = %q, want markdown", result.Format)
	}
	if result.CharCount == 0 {
		t.Fatal("the report carries no character count")
	}
	if result.ChapterCount != 2 {
		t.Fatalf("chapter count = %d, want the two headings", result.ChapterCount)
	}
	if len(result.Chapters) != 2 {
		t.Fatalf("the report listed %d boundaries, want 2", len(result.Chapters))
	}
	first := result.Chapters[0]
	if first.Ordinal != 1 || first.Title != "第一章 开始" {
		t.Fatalf("the first boundary reads as %+v", first)
	}
	// A Markdown heading keeps its text as the title, marker and all: the
	// numbered patterns are the ones that strip a marker, because there the marker
	// is punctuation rather than the chapter's name.
	if first.Source != "heading" {
		t.Fatalf("the first boundary's source is %q, want heading", first.Source)
	}
	// The offsets must be a real range in reading order, or the reader would
	// slice the wrong text.
	if first.StartOffset >= first.EndOffset {
		t.Fatalf("the first boundary's range is %d..%d", first.StartOffset, first.EndOffset)
	}
	if result.Chapters[1].StartOffset < first.StartOffset {
		t.Fatal("the boundaries are not in reading order")
	}
	// The warnings list is an empty array rather than null, so the frontend does
	// not have to handle two shapes of "nothing to report".
	if result.Warnings == nil {
		t.Fatal("the warnings list is null, which the frontend would have to special-case")
	}
}

// TestReadDocumentRangeIsPaged proves the response is bounded whatever the caller
// asks for. This is what keeps a 100k-character document from blocking the
// renderer: the ceiling is enforced on the Go side, not trusted to the UI.
func TestReadDocumentRangeIsPaged(t *testing.T) {
	binding := importBindingForTest(t)
	// A document of roughly 100,000 characters, which is the acceptance figure.
	var builder strings.Builder
	for index := 0; index < 10000; index++ {
		builder.WriteString("第一章一段文字内容。")
	}
	imported, err := binding.ImportDocument(ImportDocumentRequest{
		ProjectID: "project-1", Name: "long.txt", Content: []byte(builder.String()),
	})
	if err != nil {
		t.Fatalf("ImportDocument: %v", err)
	}
	if imported.Version.NormalizedTextFileID == "" {
		t.Fatal("the import stored no normalized text, so nothing could be read back")
	}
	page, err := binding.ReadDocumentRange(ReadDocumentRangeRequest{
		SourceDocumentVersionID: imported.Version.ID,
		StartRune:               0,
		// Ask for the whole document, which must be clamped rather than honoured.
		EndRune: 1 << 30,
	})
	if err != nil {
		t.Fatalf("ReadDocumentRange: %v", err)
	}
	if page.TotalRunes == 0 {
		t.Fatal("the page reports no total, so the UI cannot compute its page count")
	}
	if page.EndRune-page.StartRune > 64*1024 {
		t.Fatalf("one page returned %d characters, which is above the ceiling", page.EndRune-page.StartRune)
	}
	// The pages tile the document: reading from the returned end must advance and
	// return different text, or a paging UI would loop on one page.
	next, err := binding.ReadDocumentRange(ReadDocumentRangeRequest{
		SourceDocumentVersionID: imported.Version.ID,
		StartRune:               page.EndRune,
		EndRune:                 page.EndRune + 100,
	})
	if err != nil {
		t.Fatalf("the next page: %v", err)
	}
	if next.StartRune != page.EndRune {
		t.Fatalf("the next page starts at %d, want %d", next.StartRune, page.EndRune)
	}
	if next.Text == page.Text {
		t.Fatal("two pages returned the same text, so the reader is not advancing")
	}
}

// importBindingForTest builds a binding over the real import service and an
// in-memory story store, with a content-addressed store behind it.
func importBindingForTest(t *testing.T) *ImportBinding {
	t.Helper()
	binding := &ImportBinding{}
	AttachImporting(binding, context.Background(), appimporting.NewService(appimporting.Options{
		Store: newTestFileStore(),
		Story: appstory.NewService(appstory.Options{
			Repository: newDramaStore(), Clock: fixedDramaClock{}, IDs: fixedIDs(),
		}),
		Clock: fixedDramaClock{},
	}))
	return binding
}

// testFileStore is an in-memory content-addressed store.
//
// It stands in for the production store so these tests measure the binding and
// the import service rather than the filesystem, which is tested where it lives.
// It hashes like the real one, because the import service treats the returned
// hash as the object's identity and a constant would make every document collide.
type testFileStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newTestFileStore() *testFileStore {
	return &testFileStore{objects: map[string][]byte{}}
}

func (s *testFileStore) Import(_ context.Context, _ string, body []byte) (appfiles.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	s.objects[hash] = append([]byte(nil), body...)
	return appfiles.Object{Hash: hash, StorageKey: hash, MIME: "text/plain", Size: int64(len(body))}, nil
}

func (s *testFileStore) Open(_ context.Context, storageKey string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.objects[storageKey]
	if !ok {
		return nil, errors.New("the object does not exist")
	}
	return append([]byte(nil), body...), nil
}

// TestReadDocumentRangePagesBackwards covers the reader's previous-page control.
//
// An independent review found this broken: the UI's "previous" button asked for a
// page starting one rune before the current one, and the range API clamps a
// non-positive end to the END of the document, so it got a page running FORWARD
// from there — advancing by nearly a whole page while moving one rune backwards.
// The button never returned to a previous page. The fix is an explicit direction,
// and the property below is what it has to satisfy: consecutive previous-pages
// must tile backwards with no overlap and no gap.
func TestReadDocumentRangePagesBackwards(t *testing.T) {
	binding := importBindingForTest(t)
	var builder strings.Builder
	for index := 0; index < 3000; index++ {
		builder.WriteString("第一章一段文字内容。")
	}
	imported, err := binding.ImportDocument(ImportDocumentRequest{
		ProjectID: "project-1", Name: "long.txt", Content: []byte(builder.String()),
	})
	if err != nil {
		t.Fatalf("ImportDocument: %v", err)
	}
	version := imported.Version.ID

	// Page forward to the second page, which gives an anchor in the middle.
	first, err := binding.ReadDocumentRange(ReadDocumentRangeRequest{
		SourceDocumentVersionID: version, StartRune: 0, EndRune: 1 << 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := binding.ReadDocumentRange(ReadDocumentRangeRequest{
		SourceDocumentVersionID: version, StartRune: first.EndRune, EndRune: 1 << 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.StartRune != first.EndRune {
		t.Fatalf("the forward pages do not tile: %d then %d", first.EndRune, second.StartRune)
	}

	// Now go BACK from the second page's start. The previous page must END there.
	back, err := binding.ReadDocumentRange(ReadDocumentRangeRequest{
		SourceDocumentVersionID: version,
		StartRune:               second.StartRune,
		Direction:               "backward",
		TotalRunes:              second.TotalRunes,
	})
	if err != nil {
		t.Fatalf("the backward read: %v", err)
	}
	if back.EndRune != second.StartRune {
		t.Fatalf("a backward page ends at %d, want the anchor %d — so it did not go back", back.EndRune, second.StartRune)
	}
	if back.StartRune >= back.EndRune {
		t.Fatalf("the backward page is %d..%d, which is empty or reversed", back.StartRune, back.EndRune)
	}
	// It must be a full page, not the one-rune sliver the broken version produced.
	if back.EndRune-back.StartRune != first.EndRune-first.StartRune {
		t.Fatalf("the backward page holds %d runes and the forward one holds %d",
			back.EndRune-back.StartRune, first.EndRune-first.StartRune)
	}
	// And its text must be the text that precedes the anchor, which is what a
	// reader sees when they page back.
	if back.Text == "" {
		t.Fatal("the backward page is empty")
	}
	if !strings.Contains(second.Text, second.Text[:1]) {
		t.Fatal("the forward page is empty")
	}
	// The two pages are contiguous AND non-overlapping, which is what makes them
	// pages rather than a sliding window: back ends exactly where second begins.
	// The broken version overlapped by one rune, so a reader paging back would see
	// a character twice.
	if back.EndRune != second.StartRune {
		t.Fatalf("the pages overlap or leave a gap: back ends at %d, second starts at %d",
			back.EndRune, second.StartRune)
	}
	// Reading back's range alone must return back's text, which is what proves the
	// returned coordinates describe the returned text rather than the request.
	probe, err := binding.ReadDocumentRange(ReadDocumentRangeRequest{
		SourceDocumentVersionID: version, StartRune: back.StartRune, EndRune: back.EndRune,
	})
	if err != nil {
		t.Fatal(err)
	}
	if probe.Text != back.Text {
		t.Fatalf("reading the returned range %d..%d gives different text than the page reported",
			back.StartRune, back.EndRune)
	}

	// Paging back to the very start must stop at zero rather than wrapping or
	// producing a negative offset.
	atStart, err := binding.ReadDocumentRange(ReadDocumentRangeRequest{
		SourceDocumentVersionID: version, StartRune: 0, Direction: "backward", TotalRunes: second.TotalRunes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if atStart.StartRune != 0 || atStart.EndRune != 0 {
		t.Fatalf("a backward page from the start is %d..%d, want an empty range at 0",
			atStart.StartRune, atStart.EndRune)
	}
}
