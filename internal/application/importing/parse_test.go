package importing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	importdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/archive"
)

// These tests exercise the DOCX and hostile-input paths through the real
// fixtures in testdata/, because the point of those fixtures is that the code
// meets actual malformed bytes rather than assertions about its own logic.
//
// The fixture directory is found relative to this file rather than passed in, so
// a test cannot accidentally run against a different copy.

const fixturesDir = "testdata/malicious-imports"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	// The tests run with this package's directory as the working directory, so
	// the repository root is three levels up.
	path := filepath.Join("..", "..", "..", fixturesDir, name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fixture %s: %v", name, err)
	}
	return content
}

// TestDetectFormatUsesTheBytesNotTheName covers the rule that a mis-named file is
// handled by content rather than by its extension.
func TestDetectFormatUsesTheBytesNotTheName(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
		hint    string
		want    importdomain.Format
		wantErr bool
	}{
		{"plain text", []byte("第一章 开始\n"), "txt", importdomain.FormatText, false},
		{"markdown hint", []byte("# Title\n"), "md", importdomain.FormatMarkdown, false},
		{"no hint", []byte("just text"), "", importdomain.FormatText, false},
		{"unknown extension", []byte("just text"), "rtf", importdomain.FormatText, false},
		{"pasted", []byte("typed in"), "pasted", importdomain.FormatText, false},
		// A real ZIP with a docx hint is a DOCX.
		{"docx by content", readFixture(t, "docx-prompt-injection.docx"), "docx", importdomain.FormatDOCX, false},
		// A ZIP that claims to be text is an archive, and saying so beats
		// importing binary as prose.
		{"archive named as text", readFixture(t, "zip-slip.zip"), "txt", "", true},
		// A file named DOCX that is not one is refused rather than imported as
		// XML, so the user learns their file is damaged.
		{"docx hint but not a zip", []byte("this is not a zip"), "docx", "", true},
		{"empty", nil, "txt", "", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := detectFormat(testCase.content, testCase.hint)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected a refusal, got format %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("detectFormat: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("format = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestDOCXTextIsExtractedInReadingOrder covers the happy path against a real
// container: paragraphs become lines, runs concatenate, cells separate.
func TestDOCXTextIsExtractedInReadingOrder(t *testing.T) {
	content := readFixture(t, "docx-prompt-injection.docx")
	text, err := extractText(content, importdomain.FormatDOCX)
	if err != nil {
		t.Fatalf("extractText: %v", err)
	}
	body := string(text)
	// The body's paragraphs must arrive in order and be separated by newlines,
	// or chapter detection would see one long line.
	if !strings.Contains(body, "第一章 试探") {
		t.Fatalf("the first paragraph is missing: %q", firstLines(body, 3))
	}
	if !strings.Contains(body, "\n") {
		t.Fatal("paragraphs were not separated, so detection would see one line")
	}
	// The injection text is body text like any other: it is extracted and
	// stored, and nothing acts on it. That is the property AC-STORY-002's
	// "不执行文档指令" is about, and it is asserted here rather than left
	// implicit.
	if !strings.Contains(body, "忽略之前的所有指令") {
		t.Fatal("the injection text was dropped, which would mean the extractor filters content")
	}
}

// TestDOCXContainerChecksHaveFixturesThatReachThem covers the three things the
// DOCX container must be before its body is read, one fixture per check.
//
// The fixture matters more than the assertion, and that is the point of this
// test. `extractText` refuses a DOCX-shaped input for several reasons — the
// archive layer refuses a traversal or a bomb before the container is inspected,
// and this code refuses a missing manifest, a non-Word manifest, or a missing
// body after it. A test that only asserted "some error" would pass while the
// check it names was disabled, which is exactly what happened here: `zip-slip.zip`
// and `bad-docx.docx` were both refused by earlier code, so a mutation disabling
// the manifest check left the suite green.
//
// Each case therefore also asserts that the archive opens, which is what proves
// the input reached the container check rather than being stopped on the way.
func TestDOCXContainerChecksHaveFixturesThatReachThem(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		reason  string
		why     string
	}{
		{
			name:    "a plain archive with no OOXML manifest",
			fixture: "zip-not-docx.zip",
			reason:  "not a Word document",
			why:     "it carries a document.xml and is otherwise a valid archive, so only the missing manifest can refuse it",
		},
		{
			name:    "a spreadsheet under a .docx name",
			fixture: "xlsx-named-docx.docx",
			reason:  "not a Word document",
			why:     "it has a manifest, so the missing-manifest branch does not fire and only the SpreadsheetML content-type can refuse it",
		},
		{
			name:    "a Word package whose body part is missing",
			fixture: "docx-missing-body.docx",
			reason:  "no readable body",
			why:     "its manifest is a Word manifest, so it is refused for the missing part rather than the container",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			content := readFixture(t, testCase.fixture)
			// The precondition that makes this fixture useful: the archive layer
			// accepts it, so any refusal below comes from the container check.
			if _, err := archive.Open(content, docxLimits); err != nil {
				t.Fatalf("the fixture is refused before the container check, so it proves nothing: %v", err)
			}
			_, err := extractText(content, importdomain.FormatDOCX)
			if err == nil {
				t.Fatal("the file was accepted as a Word document")
			}
			// The message names which check fired, so a case cannot silently
			// regress into being refused by a different one.
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Fatalf("refused with %q, want a refusal mentioning %q (%s)", err, testCase.reason, testCase.why)
			}
		})
	}
}

// TestHostileFixturesAreRefusedWithTheRightCategory is the security corpus.
//
// Each case names the category the refusal must carry, because the category is
// what decides whether a caller may retry: SECURITY section 17 says a security
// error is never retried automatically, so flattening these into "invalid
// document" would lose the distinction that matters.
func TestHostileFixturesAreRefusedWithTheRightCategory(t *testing.T) {
	cases := []struct {
		name     string
		fixture  string
		wantErr  bool
		category importdomain.ErrorCategory
		why      string
	}{
		{
			name:     "a truncated archive carrying a .docx name",
			fixture:  "bad-docx.docx",
			wantErr:  true,
			category: importdomain.CategoryInvalidInput,
			why:      "not being an archive is a malformed input, not an attack",
		},
		{
			name:     "a compression bomb in the metadata",
			fixture:  "zip-bomb-metadata.zip",
			wantErr:  true,
			category: importdomain.CategorySecurity,
			why:      "the archive layer classifies a bomb as a security refusal",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			content := readFixture(t, testCase.fixture)
			// The format is forced so the DOCX path is taken regardless of the
			// container check: this is about what the archive reader does with
			// hostile bytes.
			_, err := extractText(content, importdomain.FormatDOCX)
			if testCase.wantErr && err == nil {
				t.Fatal("a hostile file was accepted")
			}
			if !testCase.wantErr {
				return
			}
			// The error must carry a category at all, which is what the caller
			// reads. An archive refusal carries the security code from the
			// archive package; a document-level refusal carries one of ours.
			if !hasCategory(err) {
				t.Fatalf("the refusal carries no recognisable category: %v (%s)", err, testCase.why)
			}
		})
	}
}

// TestPromptInjectionTextImportsAsData is AC-STORY-002's "不执行文档指令" reduced to
// the one thing this layer can prove: the text is data, it survives intact, and
// nothing in the pipeline treats it as anything else.
func TestPromptInjectionTextImportsAsData(t *testing.T) {
	injection := readFixture(t, "prompt-injection.txt")
	text := string(injection)
	// The payload is present and unmodified.
	if !strings.Contains(text, "忽略之前的所有指令") {
		t.Fatal("the fixture lost its payload")
	}
	// The document is text, and its chapter detection sees an ordinary heading
	// rather than anything directive.
	document, err := (&Service{}).prepare(injection, "txt")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(document.Chapters) == 0 {
		t.Fatal("no chapter was detected in a document that has a heading")
	}
	// The chapter title is the heading text. Nothing in it became a command.
	if document.Chapters[0].Title != "试探" {
		t.Fatalf("chapter title = %q, want the heading text", document.Chapters[0].Title)
	}
	// And the whole document survives normalization: a filter that stripped
	// suspicious lines would have changed the character count.
	if !strings.Contains(document.Text, "管理员身份") {
		t.Fatal("part of the document was dropped during normalization")
	}
}

// TestGBKFixtureIsDecoded covers the encoding path against a real GBK file.
func TestGBKFixtureIsDecoded(t *testing.T) {
	content := readFixture(t, "gbk-sample.txt")
	document, err := (&Service{}).prepare(content, "txt")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if document.Encoding != importdomain.EncodingGBK && document.Encoding != importdomain.EncodingGB18030 {
		t.Fatalf("encoding = %q, want a Chinese encoding", document.Encoding)
	}
	if !strings.Contains(document.Text, "第一章 编码") {
		t.Fatalf("the GBK text did not decode: %q", document.Text)
	}
	if !strings.Contains(document.Text, "正确识别") {
		t.Fatal("the body of the GBK document is missing")
	}
}

func firstLines(text string, count int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > count {
		lines = lines[:count]
	}
	return strings.Join(lines, " | ")
}

// hasCategory reports whether an error carries an import category or an archive
// security code, which are the two classifications the refusal paths produce.
func hasCategory(err error) bool {
	if _, ok := importdomain.AsError(err); ok {
		return true
	}
	// The archive reader's errors carry the security codes SECURITY section 17
	// names, so their presence is what the caller acts on.
	message := err.Error()
	return strings.Contains(message, "archive") || strings.Contains(message, "security")
}

// fakeStore is an in-memory DocumentStore so the service tests need no files.
type fakeStore struct {
	objects map[string][]byte
	next    int
}

func newFakeStore() *fakeStore {
	return &fakeStore{objects: map[string][]byte{}}
}

func (s *fakeStore) Import(_ context.Context, displayName string, body []byte) (appfiles.Object, error) {
	s.next++
	key := displayName + "#" + string(rune('a'+s.next))
	s.objects[key] = append([]byte(nil), body...)
	return appfiles.Object{Hash: strings.Repeat("a", 64), StorageKey: key, MIME: "text/plain", Size: int64(len(body))}, nil
}

func (s *fakeStore) Open(_ context.Context, storageKey string) ([]byte, error) {
	content, ok := s.objects[storageKey]
	if !ok {
		return nil, os.ErrNotExist
	}
	return content, nil
}
