package importing

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"

	importdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
)

// pdf.go extracts the text layer of a PDF (WP-19, P3 item 24).
//
// # Why this library, and why not the other one
//
// Two candidates were probed against a HAND-BUILT Chinese PDF carrying a `ToUnicode` CMap — the
// construct a real Chinese document always has, because its glyphs are not ASCII:
//
//   - `rsc.io/pdf` returned the RAW GLYPH CODES (`"\x10\x00\x10\x01…"`). It reads `ToUnicode` only
//     as far as the font dictionary and does not use it to map the text it extracts, so it cannot
//     read Chinese at all. The entire corpus this product imports is Chinese, which makes the
//     library useless here no matter how well maintained it is.
//   - `github.com/ledongthuc/pdf` returned `"沈砚的渡口"` — the expected text, character for
//     character.
//
// The probe files are built by the package's own test generator rather than committed as opaque
// bytes, so the claim above is re-checked on every run instead of resting on this comment.
//
// # Why every call is wrapped in recover
//
// Fuzzing the chosen library with 120 corrupted variants of that same file produced THREE PANICS —
// `invalid real .`, `unexpected non-name key int64(0) parsing dictionary`, and `missing endobj after
// indirect object definition`. All three come from `lex.go`'s `errorf`, which is how the library
// signals "this input is malformed": it panics instead of returning an error. None of the 120 hung,
// and four hand-built hostile files (not a PDF, truncated, an xref past the end, 400 nested objects)
// all returned errors.
//
// An importer must not crash because a user dragged in a damaged file, so the panic is recovered and
// turned into this package's own error. THE RECOVER IS DELIBERATELY NARROW — it wraps the library
// calls and nothing else — because a `recover` around a larger body would swallow this repository's
// own bugs as well, and a bug reported as "the PDF was malformed" is a bug nobody will find.
//
// # What it does not do
//
// No OCR: a scanned PDF has no text layer, and this returns a refusal that says so rather than an
// empty document. No decryption: an encrypted PDF is refused with its own reason. No layout
// reconstruction: text comes out in content-stream order, so a two-column page interleaves its
// columns' lines. That is the shared limit of every non-OCR extractor and it is recorded in
// ADR-0023 rather than glossed as "PDF support".

// pdfLimits is what one extraction will read.
//
// # Why the bounds are a VALUE rather than two constants read at the comparison
//
// Two reasons, and the second is the one that decided it. The first is that they belong together: a
// page bound and a per-page text bound are one policy about how much work a document may cost, and a
// reader should see them side by side.
//
// The second is TESTABILITY, which here is a correctness concern rather than a convenience. Probing
// this extractor against its real bounds meant either building two thousand real pages — which took
// over nine minutes, so the test would stop being run — or doctoring the page tree, which broke the
// cross-reference table and made the refusal come from the parser instead of from the bound. Passing
// the limits in means a test asserts the REAL check on a SMALL document: `pdfTextWithLimits(doc,
// pdfLimits{MaxPages: 1, ...})` exercises the same comparison the production path makes, without
// the production-sized fixture.
type pdfLimits struct {
	// MaxPages bounds how many pages one import will read. A novel is a few hundred pages; two
	// thousand is far above any manuscript and far below what a hostile file would claim.
	MaxPages int
	// MaxPageTextBytes bounds one page's extracted text. A page whose text layer decompresses to
	// hundreds of megabytes is a decompression bomb dressed as a document.
	MaxPageTextBytes int
}

// defaultPDFLimits is the production policy, and the zero value is not it: a caller that passed a
// zero-valued `pdfLimits` would bound every document to nothing, so `pdfTextWithLimits` treats a
// non-positive field as "use the default" rather than as "refuse everything".
func defaultPDFLimits() pdfLimits {
	return pdfLimits{
		MaxPages:         2000,
		MaxPageTextBytes: 4 << 20,
	}
}

// errPDFNoText is the refusal a document with no text layer gets.
//
// It is a named error value so the tests can assert WHICH refusal came back: "the PDF is corrupt"
// and "the PDF has no text layer" are different problems with different remedies — the first is a
// damaged file, the second is a scan the user has to run through OCR themselves — and a test that
// matched on the message text alone would not survive a rewording.
var errPDFNoText = importdomain.InvalidError(
	"That PDF has no text layer — it is probably a scan, and this importer reads text rather than " +
		"images. Run it through OCR first, or import the text another way.")

// pdfText extracts a PDF's text as UTF-8.
//
// The pages are joined with a blank line, because chapter detection reads line structure: a heading
// that begins a page would otherwise be glued to the last line of the page before it, and a chapter
// boundary would land in the middle of a sentence.
func pdfText(content []byte) ([]byte, error) {
	return pdfTextWithLimits(content, defaultPDFLimits())
}

// pdfTextWithLimits extracts a PDF's text under an explicit policy.
//
// It is the function `pdfText` calls, and the split exists so a test can hold a document small and
// assert the real bound checks (see `pdfLimits`).
func pdfTextWithLimits(content []byte, limits pdfLimits) ([]byte, error) {
	pages, err := pdfPageTexts(content, limits)
	if err != nil {
		return nil, err
	}
	var builder strings.Builder
	for index, page := range pages {
		if index > 0 {
			// A blank line between pages, which is a paragraph break to the chapter detector.
			builder.WriteString("\n\n")
		}
		builder.WriteString(page)
	}
	text := builder.String()
	if strings.TrimSpace(text) == "" {
		return nil, errPDFNoText
	}
	return []byte(text), nil
}

// pdfPageTexts reads every page's text, recovering the library's panics.
//
// The returned error is always this package's: either the library's own (when it returned one), the
// recovered panic's reason (when it panicked), or a refusal this function made.
func pdfPageTexts(content []byte, limits pdfLimits) (pages []string, err error) {
	if limits.MaxPages <= 0 {
		limits.MaxPages = defaultPDFLimits().MaxPages
	}
	if limits.MaxPageTextBytes <= 0 {
		limits.MaxPageTextBytes = defaultPDFLimits().MaxPageTextBytes
	}
	// THE RECOVER, and it is the whole reason this function exists as a separate call: it must wrap
	// the library and nothing else. `err` is named so the recovery can set it.
	defer func() {
		if recovered := recover(); recovered != nil {
			// The library panics with a string from its lexer ("invalid real .") or with an error.
			// Both become this package's refusal: the caller asked us to read a document and the
			// document could not be read, which is what an invalid-input error means here.
			err = importdomain.InvalidError(fmt.Sprintf(
				"That PDF could not be read: the file is malformed. (%v)", recovered))
			pages = nil
		}
	}()

	reader, openErr := pdf.NewReader(bytes.NewReader(content), int64(len(content)))
	if openErr != nil {
		// The library's own refusal, which it returns (rather than panics) for a file that is not a
		// PDF at all, is truncated, or has a broken cross-reference table. Its text goes into ours
		// because it names the specific problem, and none of it is user-controlled beyond the file's
		// own structure.
		return nil, importdomain.InvalidError("That PDF could not be read: " + openErr.Error())
	}
	total := reader.NumPage()
	if total <= 0 {
		return nil, errPDFNoText
	}
	if total > limits.MaxPages {
		return nil, importdomain.InvalidError(fmt.Sprintf(
			"That PDF has %d pages, and an import reads at most %d.", total, limits.MaxPages))
	}

	pages = make([]string, 0, total)
	for number := 1; number <= total; number++ {
		page := reader.Page(number)
		if page.V.IsNull() {
			// A page the cross-reference table points at but which is not a page object. Skipping it
			// keeps the rest of the document readable, which is the charitable reading of a file
			// whose structure is imperfect but whose text is intact.
			pages = append(pages, "")
			continue
		}
		text, textErr := pdfPageText(page, limits)
		if textErr != nil {
			return nil, textErr
		}
		pages = append(pages, text)
	}
	return pages, nil
}

// pdfPageText reads one page's text, with the per-page bound applied.
func pdfPageText(page pdf.Page, limits pdfLimits) (string, error) {
	rows, err := page.GetTextByRow()
	if err != nil {
		return "", importdomain.InvalidError("That PDF's text could not be read: " + err.Error())
	}
	var builder strings.Builder
	for _, row := range rows {
		for _, word := range row.Content {
			builder.WriteString(word.S)
		}
		// One line per row, because a row IS a line of the document and the chapter detector reads
		// line structure. Joining a page's rows into one string would turn every page into a single
		// enormous line.
		builder.WriteString("\n")
		if builder.Len() > limits.MaxPageTextBytes {
			return "", importdomain.InvalidError(
				"One page of that PDF carries more text than an import accepts, so it is not a manuscript.")
		}
	}
	return builder.String(), nil
}
