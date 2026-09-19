package importing

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/archive"
)

// parse.go is where bytes become text, including the DOCX container.
//
// A DOCX is a ZIP whose `word/document.xml` holds the body. Three properties
// matter here and all three are SECURITY requirements rather than conveniences:
//
//   - The ZIP is read through the hardened archive reader, so path traversal,
//     entry-count bombs, size lies and compression bombs are refused by the same
//     code the backup path already trusts (SECURITY section 8.2's "ZIP 限制").
//   - Only two entries are read: the content-type manifest, to confirm the file
//     really is an OOXML document, and the body. Nothing resolves a
//     relationship and nothing follows a hyperlink, which is what section 8.2's
//     "禁止外部关系自动访问" asks for.
//   - The XML decoder never resolves an external entity, and does not expand a
//     custom entity defined in a document type declaration. That is
//     `encoding/xml`'s default behaviour rather than a flag set here, which is
//     why the fixture that defines one exists: it pins what this actually does.
//
// The text extraction itself is deliberately shallow. It reads `<w:t>` runs and
// the paragraph, line-break and cell boundaries. Styles, headings, footnotes,
// comments and field codes are ignored, so a DOCX and a TXT of the same prose
// produce the same normalized text and therefore the same chapter boundaries.
// One detection rule for all three formats is what makes a DOCX chapter's offset
// comparable with a TXT chapter's.

// docxLimits tightens the archive limits for a document.
//
// The archive defaults are sized for a project backup: 4 GiB per entry, 20 GiB
// in total. A document is text, so a body of tens of megabytes is a mistake or
// an attack, and refusing it before decompression is cheaper than decompressing
// it to find out. The entry count is small because a DOCX has a handful of parts.
var docxLimits = archive.Limits{
	MaxEntries:          512,
	MaxEntryBytes:       32 << 20,
	MaxTotalBytes:       64 << 20,
	MaxCompressionRatio: 1000,
	MaxPathLength:       256,
}

// docxContentTypes is the entry every OOXML package must carry.
const docxContentTypes = "[Content_Types].xml"

// docxDocumentPath is the body part this importer reads.
const docxDocumentPath = "word/document.xml"

// detectFormat decides which container the bytes are.
//
// The bytes decide, not the extension. A `.docx` that is not a ZIP is a text
// file its author mis-named, and importing it as text is more useful than
// refusing it; a `.txt` that IS a ZIP is an archive, and saying so is more
// useful than importing binary as prose. The hint therefore only breaks a tie,
// which is why an unrecognised one is not an error.
func detectFormat(content []byte, hint string) (importing.Format, error) {
	if len(content) == 0 {
		return "", importing.InvalidError("The document is empty.")
	}
	if int64(len(content)) > importing.MaxInputBytes {
		return "", importing.InvalidError("The document is larger than an import accepts.")
	}
	normalizedHint := strings.ToLower(strings.TrimSpace(hint))
	if hasZipSignature(content) {
		if normalizedHint != "" && normalizedHint != "docx" &&
			normalizedHint != string(importing.FormatDOCX) {
			return "", importing.InvalidError("That file is an archive, not the text document its name suggests.")
		}
		return importing.FormatDOCX, nil
	}
	switch normalizedHint {
	case "md", "markdown":
		return importing.FormatMarkdown, nil
	case "docx":
		// The hint says DOCX but the bytes are not a ZIP. Reporting the mismatch
		// rather than importing as text means the user learns their file is
		// damaged instead of getting a document full of XML declarations.
		return "", importing.InvalidError("That file is named as a DOCX but is not a readable document.")
	case "", "txt", "text", "pasted":
		return importing.FormatText, nil
	default:
		// An unknown extension is not a reason to refuse: the bytes above
		// already decided, and everything that is not an archive is text.
		return importing.FormatText, nil
	}
}

// hasZipSignature reports whether the bytes open with a ZIP header.
//
// All three signatures are accepted: a local file header, an empty archive, and
// a spanned-archive marker. A DOCX always starts with the first, and accepting
// the other two means an empty or partial archive reaches the archive reader and
// gets a specific refusal rather than being mistaken for text.
func hasZipSignature(content []byte) bool {
	if len(content) < 4 || content[0] != 'P' || content[1] != 'K' {
		return false
	}
	return (content[2] == 0x03 && content[3] == 0x04) ||
		(content[2] == 0x05 && content[3] == 0x06) ||
		(content[2] == 0x07 && content[3] == 0x08)
}

// extractText returns the document's text in UTF-8, before encoding detection.
//
// A text format's bytes are returned unchanged, because encoding detection is
// the next step and it needs the original bytes rather than a decoded guess. A
// DOCX's body is XML, whose character encoding the XML declaration states, so it
// comes out of this step already decoded.
func extractText(content []byte, format importing.Format) ([]byte, error) {
	if format != importing.FormatDOCX {
		return content, nil
	}
	return docxText(content)
}

// docxText reads a DOCX body and returns its text as UTF-8.
func docxText(content []byte) ([]byte, error) {
	reader, err := archive.Open(content, docxLimits)
	if err != nil {
		// The archive layer classified the refusal — traversal, bomb, oversize,
		// corrupt — and its error carries a security code. Passing it through
		// keeps that category, which decides whether the caller may retry.
		return nil, err
	}
	// Confirm the container is OOXML before trusting any of its parts. Without
	// this, an arbitrary ZIP with a `word/document.xml` entry would be read as a
	// document.
	contentTypes, err := reader.Read(docxContentTypes)
	if err != nil || !bytes.Contains(contentTypes, []byte("wordprocessingml")) {
		return nil, importing.InvalidError("That file is a ZIP archive, not a Word document.")
	}
	if !reader.Has(docxDocumentPath) {
		return nil, importing.InvalidError("That Word document has no readable body.")
	}
	body, err := reader.Read(docxDocumentPath)
	if err != nil {
		return nil, importing.InvalidError("That Word document's body could not be read.")
	}
	text, err := parseWordBody(body)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// parseWordBody pulls the text out of a `word/document.xml`.
//
// The walk is a single pass over the token stream, with the state kept in local
// variables rather than in a decoder struct: the document is read once, and a
// stateful parser would be more machinery than one loop needs.
func parseWordBody(body []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var builder strings.Builder
	// inText is true while the walk is inside a `<w:t>` run, which is the only
	// element whose character data is body text.
	inText := false
	// paragraphBreak tracks whether a paragraph has already been written, so the
	// first one does not begin with a blank line.
	paragraphBreak := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A malformed body is refused rather than partially accepted: half a
			// chapter set computed from half a document is worse than none.
			return "", importing.InvalidError("That Word document's body could not be read.")
		}
		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "t":
				inText = true
			case "p", "tr":
				if paragraphBreak {
					builder.WriteString("\n")
				}
				paragraphBreak = true
			case "br", "cr":
				builder.WriteString("\n")
			case "tab":
				builder.WriteString("\t")
			}
		case xml.CharData:
			if inText {
				builder.Write(element)
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "t":
				inText = false
			case "tc":
				// A cell ends with a tab so two cells do not run together into
				// one word, which would change how a heading line is matched.
				builder.WriteString("\t")
			}
		}
	}
	return builder.String(), nil
}
