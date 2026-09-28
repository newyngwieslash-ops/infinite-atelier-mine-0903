package database

import (
	"context"
	"path/filepath"
	"testing"

	"bytes"
	"io"
	"strings"
	"time"

	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appimporting "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/importing"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	importdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
)

// rp04DocStore adapts the file store to the importing package's DocumentStore
// port, in-test: the same shape desktop.NewDocumentStore composes in the app,
// rebuilt here because the database test package cannot import desktop.
type rp04DocStore struct{ store *filestore.Store }

func (d rp04DocStore) Import(ctx context.Context, displayName string, body []byte) (appfiles.Object, error) {
	return d.store.Put(ctx, displayName, bytes.NewReader(body))
}

func (d rp04DocStore) Open(ctx context.Context, storageKey string) ([]byte, error) {
	stream, err := d.store.Open(ctx, storageKey)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	// The extra byte distinguishes "exactly the limit" from "over it": a
	// read that fills limit+1 is refused, the same contract the desktop
	// adapter implements.
	content, err := io.ReadAll(io.LimitReader(stream, importdomain.MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > importdomain.MaxInputBytes {
		return nil, importdomain.InvalidError("The stored document is larger than an import accepts.")
	}
	return content, nil
}

// local_job_sources_rp04_test.go is RP-04.2's contract: an IMPORT job reads
// the document's REAL content from the managed store — the same store the
// interactive import writes to — and produces locatable chapters. The
// assertions are on database read-back, not on mocks.

// rp04ImportFixture wires a real importing service over a real file store and
// the test database, the composition the import job handler drives.
type rp04ImportFixture struct {
	store     *filestore.Store
	importing *appimporting.Service
	story     *appstory.Service
}

var fixedClockAt = time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

func newRP04ImportFixture(t *testing.T) *rp04ImportFixture {
	t.Helper()
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	root := t.TempDir()
	store, err := filestore.New(filepath.Join(root, "files"), filepath.Join(root, "temp"))
	if err != nil {
		t.Fatalf("opening the file store: %v", err)
	}
	fixture := &rp04ImportFixture{store: store}
	fixture.story = appstory.NewService(appstory.Options{
		Repository: NewStoryRepository(db),
		Clock:      mediaClock{at: fixedClockAt},
		IDs:        dramaIDGenerator(),
	})
	fixture.importing = appimporting.NewService(appimporting.Options{
		Store: rp04DocStore{store: store},
		Story: fixture.story,
		Clock: mediaClock{at: fixedClockAt},
	})
	return fixture
}

// TestRP04ImportJobReadsRealContentAndFindsChapters drives the loop the job
// handler runs: store a document version through the importer (the upload
// path), then read the content BACK from the store through the job's read
// and import it again — the continuation the job performs — and confirm the
// chapters are locatable rows.
func TestRP04ImportJobReadsRealContentAndFindsChapters(t *testing.T) {
	fixture := newRP04ImportFixture(t)
	ctx := context.Background()

	content := []byte("第一章 出发\n他推开门，雨水顺着屋檐落下。\n第二章 渡河\n船夫点起一盏灯，河面浮起薄雾。")

	// The upload half: the interactive import stores the document. This is
	// the state a job's input names.
	created, err := fixture.importing.Import(ctx, appimporting.ImportRequest{
		ProjectID: "drama-project",
		Name:      "rp04-novel",
		Format:    "txt",
		Content:   content,
	})
	if err != nil {
		t.Fatalf("initial import: %v", err)
	}
	if created.Duplicated {
		t.Fatal("a fresh document reported itself duplicated")
	}
	if len(created.Chapters) != 2 {
		t.Fatalf("the upload detected %d chapters, want 2", len(created.Chapters))
	}

	// THE JOB'S READ (RP-04.2): the content comes back from the MANAGED store
	// by the version's own file reference — no OS path, no caller bytes.
	versions, err := fixture.story.ListSourceDocumentVersions(ctx, created.Document.ID)
	if err != nil {
		t.Fatalf("ListSourceDocumentVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("the document lists %d versions, want 1", len(versions))
	}
	readBack, err := fixture.importing.ReadStored(ctx, versions[len(versions)-1].ID)
	if err != nil {
		t.Fatalf("ReadStored: %v", err)
	}
	if string(readBack) != string(content) {
		t.Fatalf("the stored content did not read back byte-identical: %d vs %d bytes", len(readBack), len(content))
	}

	// THE JOB'S WRITE: the continuation import with the read-back content
	// produces a SECOND version whose chapters are real rows.
	continued, err := fixture.importing.Import(ctx, appimporting.ImportRequest{
		ProjectID:        "drama-project",
		DocumentID:       created.Document.ID,
		Name:             "rp04-novel",
		Content:          readBack,
		ConfirmDuplicate: true,
	})
	if err != nil {
		t.Fatalf("continuation import: %v", err)
	}
	if len(continued.Chapters) != 2 {
		t.Fatalf("the continuation produced %d chapters, want 2", len(continued.Chapters))
	}
	// The chapter boundaries index the VERSION's text and are stored rows.
	chapters, err := fixture.story.ListChapters(ctx, continued.Version.ID)
	if err != nil {
		t.Fatalf("ListChapters: %v", err)
	}
	if len(chapters) != 2 {
		t.Fatalf("the continued version has %d chapter rows, want 2", len(chapters))
	}
	for _, chapter := range chapters {
		if chapter.StartOffset < 0 || chapter.EndOffset <= chapter.StartOffset {
			t.Fatalf("chapter %q has an unusable range [%d,%d)", chapter.Title, chapter.StartOffset, chapter.EndOffset)
		}
		if chapter.EndOffset > len([]rune(string(readBack))) {
			t.Fatalf("chapter %q indexes past the text it belongs to", chapter.Title)
		}
	}
}

// TestRP04ImportJobRefusesContentlessDocument is the negative case the old
// handler hid: a document id whose version carries no stored original is
// REFUSED rather than imported as empty content.
func TestRP04ImportJobRefusesContentlessDocument(t *testing.T) {
	fixture := newRP04ImportFixture(t)
	ctx := context.Background()

	// A version row with NO physical file: a pasted document, which the
	// schema allows but an import job cannot continue.
	document, err := fixture.story.CreateSourceDocument(ctx, appstory.CreateSourceDocumentRequest{
		ProjectID: "drama-project", DocumentType: storydomain.DocumentNovel, Name: "no-file",
	})
	if err != nil {
		t.Fatalf("CreateSourceDocument: %v", err)
	}
	// The hash-shaped ids satisfy the schema, but the store holds NO object
	// at them: a pasted document whose original upload never existed.
	missingHash := strings.Repeat("e", 64)
	version, err := fixture.story.AddSourceDocumentVersion(ctx, appstory.AddSourceDocumentVersionRequest{
		SourceDocumentID:     document.ID,
		SourceHash:           strings.Repeat("d", 64),
		PhysicalFileID:       missingHash,
		NormalizedTextFileID: strings.Repeat("f", 64),
		CharCount:            0,
	})
	if err != nil {
		t.Fatalf("AddSourceDocumentVersion: %v", err)
	}
	if _, err := fixture.importing.ReadStored(ctx, version.ID); err == nil {
		t.Fatal("ReadStored answered for a version whose original is missing from the store")
	}
}

// TestRP04BoundedReadRefusesOversizedStoreObject proves the read the job
// performs is bounded: an object larger than the import ceiling is refused
// rather than buffered. The ceiling is the importer's own, so the test reads
// it from there.
func TestRP04BoundedReadRefusesOversizedStoreObject(t *testing.T) {
	fixture := newRP04ImportFixture(t)
	ctx := context.Background()

	// A store object of MaxInputBytes+1 bytes, stored directly — simulating a
	// managed object that outgrew the rule the importer enforces on uploads.
	big := make([]byte, importdomain.MaxInputBytes+1)
	object, err := fixture.store.Put(ctx, "oversized.bin", bytes.NewReader(big))
	if err != nil {
		t.Fatalf("storing the oversized object: %v", err)
	}
	_ = object
	// The read through the importer's store must refuse it.
	store := rp04DocStore{store: fixture.store}
	stream, openErr := fixture.store.Open(ctx, object.Hash)
	if openErr != nil {
		t.Fatalf("the store itself could not open the object: %v", openErr)
	}
	stream.Close()
	_, err = store.Open(ctx, object.Hash)
	if err == nil {
		t.Fatal("an oversized store object read back through the bounded reader")
	}
}
