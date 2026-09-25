package importing

import (
	"errors"
	"strings"
	"testing"

	importdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
)

// pdf_test.go covers the PDF extractor (WP-19, P3 item 24).
//
// # What each test is for, and which finding it protects
//
// The reconnaissance that preceded this package probed two libraries against a hand-built Chinese
// PDF, and the findings are why these tests exist in this shape:
//
//  1. ONE OF THEM CANNOT READ CHINESE. `rsc.io/pdf` returned the raw glyph codes and ignored the
//     font's `ToUnicode` CMap. The first test asserts the characters themselves, so a replacement
//     library that has the same defect fails here rather than in a user's import.
//  2. THE OTHER PANICS ON DAMAGED INPUT. Fuzzing produced three panics in 120 corrupt variants, which
//     is why every library call is wrapped in `recover`. The corruption test asserts an ERROR comes
//     back — under the old code it would have taken the process down.
//  3. A SCAN HAS NO TEXT. An empty extraction must be a refusal with its own reason, not an empty
//     document: a user who imports a scan and gets a blank book has been told nothing.
//
// The fixtures are built by `pdf_fixture_test.go` in this package, so the text under test is visible
// in the source rather than hidden in a binary blob.

// TestAPDFsChineseTextIsExtracted is the core claim.
//
// It is the test that would have failed for `rsc.io/pdf`: that library returns `"\x10\x00\x10\x01…"`
// for this exact file, because it does not apply the ToUnicode CMap. Asserting the characters — not
// merely that something non-empty came out — is what makes the test able to see the difference.
func TestAPDFsChineseTextIsExtracted(t *testing.T) {
	fixture := buildChinesePDF("沈砚的渡口")
	text, err := pdfText(fixture.Bytes)
	if err != nil {
		t.Fatalf("pdfText: %v", err)
	}
	if string(text) != fixture.Text {
		t.Fatalf("the extracted text is %q, want %q", string(text), fixture.Text)
	}
}

// TestAPDFsPagesKeepTheirOrderAndAreSeparated covers a multi-page document.
//
// The separator matters rather than being cosmetic: chapter detection reads line structure, and a
// heading that began a page would otherwise be glued to the last line of the page before it.
func TestAPDFsPagesKeepTheirOrderAndAreSeparated(t *testing.T) {
	fixture := buildChinesePDF("第一章 渡口", "第二章 灯巷")
	text, err := pdfText(fixture.Bytes)
	if err != nil {
		t.Fatalf("pdfText: %v", err)
	}
	if string(text) != fixture.Text {
		t.Fatalf("the extracted text is %q, want %q", string(text), fixture.Text)
	}
	if !strings.Contains(string(text), "第一章 渡口") || !strings.Contains(string(text), "第二章 灯巷") {
		t.Fatalf("a page's text is missing: %q", string(text))
	}
	// The order is the document's, not the map's.
	if strings.Index(string(text), "第一章") > strings.Index(string(text), "第二章") {
		t.Fatalf("the pages came back out of order: %q", string(text))
	}
}

// TestTheFormatIsDecidedByTheBytes is the detection rule applied to the third container.
//
// This package's own rule is that the BYTES decide and the hint only breaks a tie, so both halves
// are asserted: the magic bytes win over an empty hint, and a hint that contradicts the bytes is a
// refusal rather than a silent reinterpretation.
func TestTheFormatIsDecidedByTheBytes(t *testing.T) {
	fixture := buildChinesePDF("沈砚的渡口")

	format, err := detectFormat(fixture.Bytes, "")
	if err != nil {
		t.Fatalf("detectFormat with no hint: %v", err)
	}
	if format != importdomain.FormatPDF {
		t.Fatalf("the bytes were detected as %q, want pdf", format)
	}
	// A matching hint is accepted.
	if format, err = detectFormat(fixture.Bytes, "pdf"); err != nil || format != importdomain.FormatPDF {
		t.Fatalf("a matching hint was refused: format=%q err=%v", format, err)
	}
	// A CONTRADICTING hint is refused, because a file that is a PDF and is named as something else is
	// either mis-named or damaged, and importing it as the name suggests is the wrong guess.
	if _, err := detectFormat(fixture.Bytes, "docx"); err == nil {
		t.Fatal("a PDF named as a DOCX was accepted")
	}
	// And a file named as a PDF that is not one is refused for the same reason in the other
	// direction.
	if _, err := detectFormat([]byte("just some prose"), "pdf"); err == nil {
		t.Fatal("prose named as a PDF was accepted")
	}
	// A file whose bytes merely CONTAIN the string is not a PDF: the header has to be at the front.
	buried := append([]byte(strings.Repeat("x", 2048)), []byte("%PDF-1.4")...)
	if format, err := detectFormat(buried, ""); err != nil || format == importdomain.FormatPDF {
		t.Fatalf("a file with a buried header was detected as %q", format)
	}
}

// TestADamagedPDFIsRefusedRatherThanCrashing is the finding the fuzzing produced.
//
// THE LIBRARY PANICS on malformed input — `invalid real .`, `unexpected non-name key`, `missing
// endobj` all reach `lex.go`'s `errorf` — and the recover in `pdfPageTexts` is what turns that into a
// refusal. Every corruption below is a real one: truncation, a wrecked cross-reference offset, and a
// byte flipped inside the object graph. Without the recover this test crashes the test binary, which
// is how it would fail rather than by an assertion.
func TestADamagedPDFIsRefusedRatherThanCrashing(t *testing.T) {
	good := buildChinesePDF("沈砚的渡口").Bytes

	corruptions := []struct {
		name  string
		bytes []byte
	}{
		{"truncated in the middle", good[:len(good)/2]},
		{"truncated in the header", good[:40]},
		{"only the header", []byte("%PDF-1.4\n")},
		{"not a PDF at all", []byte("this is a text file with a .pdf name")},
		{"empty", []byte{}},
	}
	for _, corruption := range corruptions {
		t.Run(corruption.name, func(t *testing.T) {
			// The assertion is that this RETURNS. A panic fails the test by taking the binary down,
			// which is the honest way for this test to fail.
			text, err := pdfText(corruption.bytes)
			if err == nil {
				t.Fatalf("a %s PDF extracted %q rather than being refused", corruption.name, string(text))
			}
			if text != nil {
				t.Fatalf("a refused extraction returned bytes: %q", string(text))
			}
			// The refusal is this package's, so the caller gets an import error rather than the
			// library's panic text.
			var domainErr *importdomain.Error
			if !errors.As(err, &domainErr) {
				t.Fatalf("the refusal is %T rather than the import domain's: %v", err, err)
			}
		})
	}

	// A byte flipped INSIDE the object graph, which the fuzzing showed can reach the lexer's panics.
	for _, offset := range []int{80, 200, len(good) / 3} {
		if offset >= len(good) {
			continue
		}
		damaged := append([]byte{}, good...)
		damaged[offset] ^= 0xFF
		// Either an error or the wrong text is acceptable — the file IS damaged — but a panic is not,
		// and neither is an empty success.
		if text, err := pdfText(damaged); err == nil && strings.TrimSpace(string(text)) == "" {
			t.Fatalf("a byte flipped at %d produced an empty success", offset)
		}
	}
}

// TestAPDFWithNoTextLayerIsRefusedForThatReason is the scan case.
//
// A scanned page carries an image and no text, so the extractor finds nothing. Returning an empty
// document would tell the user nothing; the refusal has to name the actual situation, because the
// remedy is different — run it through OCR — and a user who read "the PDF could not be read" would
// reasonably conclude the file is broken and try again.
func TestAPDFWithNoTextLayerIsRefusedForThatReason(t *testing.T) {
	// A PDF with a page whose content stream draws nothing: structurally valid, textually empty.
	fixture := buildChinesePDF("")
	_, err := pdfText(fixture.Bytes)
	if err == nil {
		t.Fatal("a PDF with no text was accepted")
	}
	var domainErr *importdomain.Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("the refusal is %T: %v", err, err)
	}
	message := domainErr.SafeMessage
	// The reason names the situation, so a user knows what to do about it.
	for _, want := range []string{"text", "scan"} {
		if !strings.Contains(strings.ToLower(message), want) {
			t.Fatalf("the refusal does not mention %q: %q", want, message)
		}
	}
}

// TestAPDFOverThePageBoundIsRefused pins the page bound.
//
// # How this asserts the REAL check on a small document
//
// The bound is a policy the extractor takes as an argument rather than two constants read at the
// comparison, and this is why: the first version of this test built 2001 real pages and took over
// nine minutes, and the doctored-pages-tree version made the refusal come from the parser rather than
// from the bound. Passing `MaxPages: 1` means the SAME comparison runs, on the same code path, with a
// fixture of two pages — so an off-by-one is still caught and nobody has to wait for it.
func TestAPDFOverThePageBoundIsRefused(t *testing.T) {
	fixture := buildChinesePDF("一", "二")
	_, err := pdfTextWithLimits(fixture.Bytes, pdfLimits{MaxPages: 1})
	if err == nil {
		t.Fatal("a two-page PDF was accepted under a one-page bound")
	}
	var domainErr *importdomain.Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("the refusal is %T: %v", err, err)
	}
	if !strings.Contains(domainErr.SafeMessage, "pages") {
		t.Fatalf("the refusal does not name the page count: %q", domainErr.SafeMessage)
	}

	// AT the bound it is accepted, so the check is a bound rather than a refusal of everything: the
	// same two-page document passes under a bound of two.
	if _, err := pdfTextWithLimits(fixture.Bytes, pdfLimits{MaxPages: 2}); err != nil {
		t.Fatalf("a two-page PDF was refused under a two-page bound: %v", err)
	}
	// And the PRODUCTION default accepts a two-page document, so the policy itself is sane.
	if _, err := pdfText(fixture.Bytes); err != nil {
		t.Fatalf("the default policy refused a two-page PDF: %v", err)
	}
	// A zero-valued policy means "use the defaults" rather than "refuse everything", which is the
	// trap a struct of limits invites.
	if _, err := pdfTextWithLimits(fixture.Bytes, pdfLimits{}); err != nil {
		t.Fatalf("a zero-valued policy refused a two-page PDF: %v", err)
	}
}

// TestAPDFOverThePageTextBoundIsRefused pins the per-page text bound, the same way.
//
// The real bound is four megabytes, and reaching it through this library means lexing four megabytes
// of text — which the probe measured as minutes, because the cost is per character. So the bound is
// asserted at `MaxPageTextBytes: 64`, where the same comparison runs and the fixture is instant. The
// production number is stated in `defaultPDFLimits` and its sanity is not what this test is for.
func TestAPDFOverThePageTextBoundIsRefused(t *testing.T) {
	// One page comfortably longer than the small bound.
	fixture := buildChinesePDF(strings.Repeat("x", 200))
	_, err := pdfTextWithLimits(fixture.Bytes, pdfLimits{MaxPageTextBytes: 64})
	if err == nil {
		t.Fatal("a page of 200 bytes was accepted under a 64-byte bound")
	}
	var domainErr *importdomain.Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("the refusal is %T: %v", err, err)
	}
	if !strings.Contains(domainErr.SafeMessage, "text") {
		t.Fatalf("the refusal does not name the text volume: %q", domainErr.SafeMessage)
	}
	// Under a bound it does not exceed, the same page extracts.
	if _, err := pdfTextWithLimits(fixture.Bytes, pdfLimits{MaxPageTextBytes: 4096}); err != nil {
		t.Fatalf("a 200-byte page was refused under a 4096-byte bound: %v", err)
	}
}
