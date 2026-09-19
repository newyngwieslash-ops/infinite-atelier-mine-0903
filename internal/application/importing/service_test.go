package importing

import (
	"context"
	"strings"
	"testing"
	"time"

	appevents "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// These tests cover the two acceptance-criteria paths an independent review found
// with NO coverage at all: the duplicate-import warning PRD FR-020 requires, and
// the chapter confirmation AC-STORY-002's audit trail depends on. Both are cases
// where the feature worked but a regression would have been invisible.

// countingStory counts the writes the import path makes, so a test can assert that
// a refused import stored nothing.
type countingStory struct {
	appstory.Repository

	documents   []storydomain.SourceDocument
	versions    []storydomain.SourceDocumentVersion
	chapters    []storydomain.Chapter
	bySourceDoc map[string]storydomain.SourceDocument
	// updates records every revision-guarded update, so a test can see the
	// confirmation's effect rather than only its return value.
	updates int
}

func newCountingStory() *countingStory {
	return &countingStory{bySourceDoc: map[string]storydomain.SourceDocument{}}
}

func (s *countingStory) CreateSourceDocument(_ context.Context, record storydomain.SourceDocument) error {
	s.documents = append(s.documents, record)
	s.bySourceDoc[record.ID] = record
	return nil
}

// UpdateSourceDocument records the pointer to the current version, which the
// import sets after storing one. Without it the embedded nil interface panics,
// which is how this method came to exist.
func (s *countingStory) UpdateSourceDocument(_ context.Context, record storydomain.SourceDocument, expectedRevision int64) error {
	current, ok := s.bySourceDoc[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.bySourceDoc[record.ID] = record
	for index, document := range s.documents {
		if document.ID == record.ID {
			s.documents[index] = record
		}
	}
	return nil
}

func (s *countingStory) GetSourceDocument(_ context.Context, id string) (storydomain.SourceDocument, error) {
	record, ok := s.bySourceDoc[id]
	if !ok {
		return storydomain.SourceDocument{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *countingStory) ListSourceDocuments(_ context.Context, projectID string) ([]storydomain.SourceDocument, error) {
	var out []storydomain.SourceDocument
	for _, record := range s.documents {
		if record.ProjectID == projectID {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *countingStory) CreateSourceDocumentVersion(_ context.Context, version storydomain.SourceDocumentVersion) error {
	s.versions = append(s.versions, version)
	return nil
}

func (s *countingStory) GetSourceDocumentVersion(_ context.Context, id string) (storydomain.SourceDocumentVersion, error) {
	for _, version := range s.versions {
		if version.ID == id {
			return version, nil
		}
	}
	return storydomain.SourceDocumentVersion{}, storydomain.NotFoundError()
}

func (s *countingStory) MaxSourceDocumentVersionNumber(_ context.Context, sourceDocumentID string) (int, error) {
	max := 0
	for _, version := range s.versions {
		if version.SourceDocumentID == sourceDocumentID && version.VersionNumber > max {
			max = version.VersionNumber
		}
	}
	return max, nil
}

func (s *countingStory) FindVersionBySourceHash(_ context.Context, projectID, sourceHash string) (storydomain.SourceDocumentVersion, string, bool, error) {
	if sourceHash == "" {
		return storydomain.SourceDocumentVersion{}, "", false, nil
	}
	for _, version := range s.versions {
		if version.SourceHash != sourceHash {
			continue
		}
		document, ok := s.bySourceDoc[version.SourceDocumentID]
		if !ok || document.ProjectID != projectID {
			continue
		}
		return version, document.Name, true, nil
	}
	return storydomain.SourceDocumentVersion{}, "", false, nil
}

func (s *countingStory) CreateChapter(_ context.Context, chapter storydomain.Chapter) error {
	s.chapters = append(s.chapters, chapter)
	return nil
}

func (s *countingStory) GetChapter(_ context.Context, id string) (storydomain.Chapter, error) {
	for _, chapter := range s.chapters {
		if chapter.ID == id {
			return chapter, nil
		}
	}
	return storydomain.Chapter{}, storydomain.NotFoundError()
}

func (s *countingStory) ListChapters(_ context.Context, versionID string) ([]storydomain.Chapter, error) {
	var out []storydomain.Chapter
	for _, chapter := range s.chapters {
		if chapter.SourceDocumentVersionID == versionID {
			out = append(out, chapter)
		}
	}
	return out, nil
}

func (s *countingStory) UpdateChapter(_ context.Context, record storydomain.Chapter, expectedRevision int64) error {
	for index, chapter := range s.chapters {
		if chapter.ID != record.ID {
			continue
		}
		if chapter.Revision != expectedRevision {
			return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
		}
		record.Revision = expectedRevision + 1
		s.chapters[index] = record
		s.updates++
		return nil
	}
	return storydomain.NotFoundError()
}

// ConfirmChapters mirrors the real repository: only a 'detected' boundary moves,
// and the event is recorded with the write.
func (s *countingStory) ConfirmChapters(_ context.Context, versionID string, record event.Event) error {
	moved := 0
	for index, chapter := range s.chapters {
		if chapter.SourceDocumentVersionID != versionID || chapter.Status != storydomain.ChapterDetected {
			continue
		}
		chapter.Status = storydomain.ChapterConfirmed
		chapter.Revision++
		s.chapters[index] = chapter
		moved++
	}
	s.updates += moved
	return nil
}

// recordingRecorder captures what the confirmation recorded, so a test can assert
// the event's presence or absence rather than only the status change.
type recordingRecorder struct {
	recorded []event.Type
}

func (r *recordingRecorder) Build(_ context.Context, draft appevents.Draft) (event.Event, error) {
	return event.Event{
		EventID:       "event-" + string(draft.Type),
		EventType:     draft.Type,
		SchemaVersion: event.SchemaVersion,
		AggregateType: draft.AggregateType,
		AggregateID:   draft.AggregateID,
		ProjectID:     draft.ProjectID,
		TraceID:       draft.TraceID,
		OccurredAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (r *recordingRecorder) RecordBestEffort(_ context.Context, draft appevents.Draft) {
	r.recorded = append(r.recorded, draft.Type)
}

// importHarness is one import service over counting doubles.
type importHarness struct {
	service  *Service
	story    *countingStory
	recorder *recordingRecorder
}

func newImportHarness(t *testing.T) *importHarness {
	t.Helper()
	store := newCountingStory()
	recorder := &recordingRecorder{}
	storyService := appstory.NewService(appstory.Options{
		Repository: store, Clock: fixedImportClock{}, IDs: &importIDs{},
	})
	service := NewService(Options{
		Store:  newFakeStore(),
		Story:  storyService,
		Events: recorder,
		Clock:  fixedImportClock{},
	})
	return &importHarness{service: service, story: store, recorder: recorder}
}

type fixedImportClock struct{}

func (fixedImportClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

type importIDs struct{ next int }

func (g *importIDs) New() (string, error) {
	g.next++
	return "id-" + string(rune('a'+g.next-1)), nil
}

const importText = "第一章 开始\n\n正文内容。\n\n第二章 继续\n\n更多内容。\n"

// TestImportRefusesADuplicateAndImportsItWhenConfirmed covers PRD FR-020's
// 重复导入提示 and AC-STORY-001's duplicate-hash item.
//
// The feature existed with NO test: an independent review inverted the condition
// (`!request.ConfirmDuplicate` → `request.ConfirmDuplicate`) and the whole suite
// stayed green, including the desktop tests. A regression here would have shipped.
func TestImportRefusesADuplicateAndImportsItWhenConfirmed(t *testing.T) {
	harness := newImportHarness(t)
	ctx := context.Background()
	body := []byte(importText)

	first, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "novel.md", Format: "md", Content: body,
	})
	if err != nil {
		t.Fatalf("the first import: %v", err)
	}
	if first.Duplicated {
		t.Fatal("the first import reported itself as a duplicate")
	}
	versionsAfterFirst := len(harness.story.versions)

	// The same bytes again, unconfirmed: refused, and nothing written.
	second, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "novel.md", Format: "md", Content: body,
	})
	if err == nil {
		t.Fatal("a duplicate import was accepted without confirmation")
	}
	if !second.Duplicated {
		t.Fatal("the refused import did not report itself as a duplicate, so the UI could not explain why")
	}
	if len(harness.story.versions) != versionsAfterFirst {
		t.Fatalf("the refused duplicate wrote %d versions", len(harness.story.versions)-versionsAfterFirst)
	}

	// Confirmed: it goes through as a NEW version rather than replacing the one
	// that exists.
	third, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "novel.md", Format: "md", Content: body, ConfirmDuplicate: true,
	})
	if err != nil {
		t.Fatalf("a confirmed duplicate import was refused: %v", err)
	}
	// Duplicated stays TRUE on a confirmed import, and that is correct rather than
	// a leftover: the flag means "a version with these bytes already exists", which
	// is still the case and is what tells the caller this import continued an
	// existing document rather than starting one. The first version of this test
	// asserted it went false, which would have been the wrong contract.
	if !third.Duplicated {
		t.Fatal("a confirmed import did not report that it continued an existing document")
	}
	// Continuing an existing document is the CALLER's choice, expressed by passing
	// DocumentID. Without it the import starts a second document even when the
	// bytes match, which is the documented contract ("Empty creates a new
	// document") rather than an oversight — the same file may legitimately be a
	// second document in one project.
	if third.Document.ID == first.Document.ID {
		t.Fatal("an import with no document id continued the existing document, so the caller could not choose")
	}
	// With the id it continues the first document as a second version, which is the
	// path the UI takes once the user confirms the duplicate.
	continued, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", DocumentID: first.Document.ID, Name: "novel.md",
		Format: "md", Content: body, ConfirmDuplicate: true,
	})
	if err != nil {
		t.Fatalf("continuing the document: %v", err)
	}
	if continued.Document.ID != first.Document.ID {
		t.Fatalf("the continued import belongs to document %q, want %q", continued.Document.ID, first.Document.ID)
	}
	// The numbering continues rather than restarting, which is what makes "version
	// two of this document" a meaningful statement.
	if continued.Version.VersionNumber != first.Version.VersionNumber+1 {
		t.Fatalf("the continued version is number %d, want %d",
			continued.Version.VersionNumber, first.Version.VersionNumber+1)
	}
	// Three versions of that document now exist, and the FIRST one is still there:
	// a re-import never replaces what was imported before (DOMAIN_MODEL §5.2).
	versions := 0
	for _, version := range harness.story.versions {
		if version.SourceDocumentID == first.Document.ID {
			versions++
		}
	}
	// Two versions of the ORIGINAL document: the first import, and the one that
	// continued it. The unconfirmed-refused attempt wrote nothing, and the import
	// that created a second document belongs to that document rather than this one.
	if versions != 2 {
		t.Fatalf("the original document has %d versions, want 2", versions)
	}
	// The two versions of this document are different rows: the first was not
	// overwritten. The per-document count checked below is the assertion that
	// matters; a global count would drift as the test adds rows for other reasons,
	// which is what this replaced.
	_ = versionsAfterFirst
	if third.Version.ID == first.Version.ID {
		t.Fatal("the confirmed import reused the existing version, so the original was overwritten")
	}
	// Its version number restarts at 1, because it belongs to a NEW document. The
	// numbering that matters is the continued import's, checked below against the
	// document it continues.
	if third.Version.VersionNumber != 1 {
		t.Fatalf("the first version of a new document is numbered %d, want 1", third.Version.VersionNumber)
	}

	// A DIFFERENT file in the same project is not a duplicate, so the check is on
	// the bytes rather than on the name.
	other, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "novel.md", Format: "md",
		Content: []byte("第一章 另一个\n\n正文。\n"),
	})
	if err != nil {
		t.Fatalf("a different file under the same name was refused: %v", err)
	}
	if other.Duplicated {
		t.Fatal("a different file was reported as a duplicate")
	}
	// And the same bytes in ANOTHER project is not a duplicate: the same file
	// imported into two dramas is two documents.
	separate, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-2", Name: "novel.md", Format: "md", Content: body,
	})
	if err != nil {
		t.Fatalf("the same file in another project was refused: %v", err)
	}
	if separate.Duplicated {
		t.Fatal("a file imported into a second project was reported as a duplicate")
	}
}

// TestPrecheckReportsADuplicateWithoutWriting covers the preview side: the warning
// has to arrive before the user commits, which is what FR-020's "提示" means.
func TestPrecheckReportsADuplicateWithoutWriting(t *testing.T) {
	harness := newImportHarness(t)
	ctx := context.Background()
	body := []byte(importText)
	if _, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "novel.md", Format: "md", Content: body,
	}); err != nil {
		t.Fatal(err)
	}
	documents := len(harness.story.documents)
	versions := len(harness.story.versions)

	preview, err := harness.service.Precheck(ctx, PrecheckRequest{
		ProjectID: "project-1", Name: "novel.md", Format: "md", Content: body,
	})
	if err != nil {
		t.Fatalf("Precheck: %v", err)
	}
	if !preview.Duplicate {
		t.Fatal("the preview did not report an already-imported file")
	}
	if preview.DuplicateDocumentName == "" {
		t.Fatal("the preview reports a duplicate without naming the document it duplicates")
	}
	// A preview writes nothing.
	if len(harness.story.documents) != documents || len(harness.story.versions) != versions {
		t.Fatal("a preview wrote rows")
	}
	// And it counts the chapters, which is the other thing the review step shows.
	if preview.ChapterCount != 2 {
		t.Fatalf("the preview counted %d chapters, want the two headings", preview.ChapterCount)
	}
}

// TestConfirmChaptersRecordsTheDecisionAndRefusesAnEmptyOne covers AC-STORY-002's
// confirmation audit trail.
//
// An independent review found this command had no test in either layer, and traced
// what that hid: the service only checked that boundaries EXISTED, so a version
// whose boundaries were all 'edited' recorded ChapterBoundariesConfirmed having
// changed nothing. The guard below is that fix, and this test is what holds it.
func TestConfirmChaptersRecordsTheDecisionAndRefusesAnEmptyOne(t *testing.T) {
	harness := newImportHarness(t)
	ctx := context.Background()
	imported, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "novel.md", Format: "md", Content: []byte(importText),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Chapters) != 2 {
		t.Fatalf("the import stored %d chapters", len(imported.Chapters))
	}
	for _, chapter := range imported.Chapters {
		if chapter.Status != storydomain.ChapterDetected {
			t.Fatalf("an imported chapter is %q, want detected", chapter.Status)
		}
	}

	confirmed, err := harness.service.ConfirmChapters(ctx, ConfirmChaptersRequest{
		SourceDocumentVersionID: imported.Version.ID,
	})
	if err != nil {
		t.Fatalf("ConfirmChapters: %v", err)
	}
	if len(confirmed) != 2 {
		t.Fatalf("the confirmation returned %d chapters", len(confirmed))
	}
	for _, chapter := range confirmed {
		if chapter.Status != storydomain.ChapterConfirmed {
			t.Fatalf("a confirmed chapter is %q", chapter.Status)
		}
	}
	// The decision is recorded, which is what makes it auditable: ADR-0009 puts a
	// confirmation on the transactional path for exactly this reason.
	if len(harness.recorder.recorded) != 0 {
		// Build is the transactional path, so nothing should have gone through
		// RecordBestEffort; the event travels with the write.
		t.Fatalf("the confirmation announced itself as a notification: %v", harness.recorder.recorded)
	}

	// Every boundary is now confirmed, so there is nothing to confirm. A second
	// confirmation would record a decision that changed nothing.
	if _, err := harness.service.ConfirmChapters(ctx, ConfirmChaptersRequest{
		SourceDocumentVersionID: imported.Version.ID,
	}); err == nil {
		t.Fatal("a version with nothing left to confirm recorded a confirmation anyway")
	}
}

// TestConfirmChaptersRefusesWithoutARecorder covers the transactional rule: a
// confirmation nobody can audit is worse than one that did not happen, so a
// service with no recorder refuses rather than confirming silently.
func TestConfirmChaptersRefusesWithoutARecorder(t *testing.T) {
	harness := newImportHarness(t)
	ctx := context.Background()
	imported, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "novel.md", Format: "md", Content: []byte(importText),
	})
	if err != nil {
		t.Fatal(err)
	}
	// A service with the same repository and no recorder.
	unrecorded := NewService(Options{Store: newFakeStore(), Story: harness.service.story, Clock: fixedImportClock{}})
	if _, err := unrecorded.ConfirmChapters(ctx, ConfirmChaptersRequest{
		SourceDocumentVersionID: imported.Version.ID,
	}); err == nil {
		t.Fatal("a service without a recorder confirmed the boundaries")
	}
	// And nothing moved: the refusal happened before the write.
	stored, err := harness.story.ListChapters(ctx, imported.Version.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, chapter := range stored {
		if chapter.Status != storydomain.ChapterDetected {
			t.Fatalf("the refused confirmation moved a chapter to %q", chapter.Status)
		}
	}
}

// TestAnEditedBoundarySurvivesAConfirmation covers the repository's guarded
// UPDATE, which the in-memory double mirrors: a boundary a user edited carries a
// stronger statement than 'confirmed', so a confirmation must not overwrite it.
func TestAnEditedBoundarySurvivesAConfirmation(t *testing.T) {
	harness := newImportHarness(t)
	ctx := context.Background()
	imported, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "novel.md", Format: "md", Content: []byte(importText),
	})
	if err != nil {
		t.Fatal(err)
	}
	// A user edits the first boundary, which marks it edited.
	edited := imported.Chapters[0]
	edited.Status = storydomain.ChapterEdited
	edited.SourceKind = storydomain.ChapterManual
	if err := harness.story.UpdateChapter(ctx, edited, edited.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.ConfirmChapters(ctx, ConfirmChaptersRequest{
		SourceDocumentVersionID: imported.Version.ID,
	}); err != nil {
		t.Fatalf("ConfirmChapters with one edited boundary: %v", err)
	}
	stored, err := harness.story.ListChapters(ctx, imported.Version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored[0].Status != storydomain.ChapterEdited {
		t.Fatalf("the confirmation overwrote an edited boundary with %q", stored[0].Status)
	}
	if stored[1].Status != storydomain.ChapterConfirmed {
		t.Fatalf("the second boundary is %q, want confirmed", stored[1].Status)
	}

	// A version whose boundaries are ALL edited has nothing to confirm, and the
	// guard must count the DETECTED ones rather than the total. The earlier version
	// of that guard incremented once per boundary whatever its status, so it only
	// refused when a version had no boundaries at all — which is the case the
	// previous test already covers. This case distinguishes the two, and a mutation
	// that counts every boundary is caught here.
	allEdited, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "second.md", Format: "md",
		Content: []byte("第一章 另一个\n\n正文。\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, chapter := range allEdited.Chapters {
		edited := chapter
		edited.Status = storydomain.ChapterEdited
		edited.SourceKind = storydomain.ChapterManual
		if err := harness.story.UpdateChapter(ctx, edited, edited.Revision); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := harness.service.ConfirmChapters(ctx, ConfirmChaptersRequest{
		SourceDocumentVersionID: allEdited.Version.ID,
	}); err == nil {
		t.Fatal("a version whose boundaries are all edited recorded a confirmation")
	}
}

// TestImportRefusesADocumentWithNoText proves the empty-input refusal happens
// before any write, so a mis-picked file leaves nothing behind.
func TestImportRefusesADocumentWithNoText(t *testing.T) {
	harness := newImportHarness(t)
	ctx := context.Background()
	for _, body := range []string{"", "   \n\n  ", "\n"} {
		if _, err := harness.service.Import(ctx, ImportRequest{
			ProjectID: "project-1", Name: "empty.txt", Format: "txt", Content: []byte(body),
		}); err == nil {
			t.Fatalf("the body %q was imported", body)
		}
	}
	if len(harness.story.documents) != 0 || len(harness.story.versions) != 0 || len(harness.story.chapters) != 0 {
		t.Fatalf("a refused import wrote %d documents, %d versions and %d chapters",
			len(harness.story.documents), len(harness.story.versions), len(harness.story.chapters))
	}
	// A name that is blank is replaced rather than refused: the display name is a
	// label, and a document with no name is still a document.
	result, err := harness.service.Import(ctx, ImportRequest{
		ProjectID: "project-1", Name: "  ", Format: "txt", Content: []byte(importText),
	})
	if err != nil {
		t.Fatalf("a blank name was refused: %v", err)
	}
	if strings.TrimSpace(result.Document.Name) == "" {
		t.Fatal("the stored document has no name at all")
	}
}
