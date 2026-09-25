package importing

import (
	"bytes"
	"compress/zlib"
	"fmt"
)

// pdf_fixture_test.go BUILDS the PDFs the extractor is tested against.
//
// # Why the fixtures are constructed rather than committed
//
// A PDF is binary. A committed one is a blob nobody can read, and the claim that matters about it —
// "this file's text is these characters" — would have to be taken on trust. Building it here means
// the characters under test are visible in the test source, and the file that carries them is
// constructed by the same code that asserts what comes out.
//
// # What makes the file a fair test of a Chinese document
//
// A Chinese PDF does not store ASCII. Its text is drawn as CID codes into a subset font, and a
// reader learns which character each code means ONLY from the font's `ToUnicode` CMap. A file
// without one is unreadable by construction, so the generator emits a real CMap — and that is
// exactly the construct that distinguishes the two candidate libraries: one of them ignores it and
// returns the raw codes.
//
// The generator also produces the file's damaged variants, because a corrupt PDF is the case an
// importer must survive and hand-built corruption is the only way to aim at a specific failure.

// pdfFixture is a document built by this file, with what its text should come out as.
type pdfFixture struct {
	// Bytes is the whole file.
	Bytes []byte
	// Text is what a correct extractor returns, character for character.
	Text string
}

// buildChinesePDF builds a one-or-more-page PDF whose text is `pages`.
//
// Each page gets its own Type0 font with an Identity-H encoding and an Identity CMaps ToUnicode
// CMap, which is the shape a real Chinese document has: the content stream draws two-byte codes and
// the CMap says which Unicode character each stands for.
func buildChinesePDF(pages ...string) pdfFixture {
	// The glyph codes are allotted per page, starting above the ASCII range so a file that happened
	// to read them as Latin would clearly produce the wrong text rather than accidentally the right
	// one.
	const firstCode = 0x1000

	var objects [][]byte
	add := func(body []byte) int {
		objects = append(objects, body)
		return len(objects)
	}

	// Object numbers are assigned as the pages are built, then the children are listed in the Pages
	// node, so the numbering is computed in one pass and written in a second.
	type pagePlan struct {
		pageNumber int
		chars      []rune
		codes      []int
	}
	plans := make([]pagePlan, 0, len(pages))
	for _, text := range pages {
		plan := pagePlan{chars: []rune(text)}
		for index := range plan.chars {
			plan.codes = append(plan.codes, firstCode+index)
		}
		plans = append(plans, plan)
	}

	// 1: catalog, 2: pages tree, then three objects per page (page, contents, font) plus the CMap
	// and the descriptor the font needs.
	catalogNumber := 1
	pagesNumber := 2
	objects = append(objects, nil, nil) // placeholders, written last
	_ = catalogNumber
	_ = pagesNumber

	pageObjects := make([]int, 0, len(plans))
	for pageIndex, plan := range plans {
		// The content stream: `[<code> <code> ...] TJ`, which is the array form. The single-operand
		// `Tj` form is refused by both candidate readers ("bad Tj operator"), and the array form is
		// what producers emit anyway.
		var stream bytes.Buffer
		stream.WriteString("BT /F1 24 Tf 72 700 Td [")
		for _, code := range plan.codes {
			fmt.Fprintf(&stream, "<%04X> ", code)
		}
		stream.WriteString("] TJ ET\n")
		compressed := deflate(stream.Bytes())
		contentsNumber := add([]byte(fmt.Sprintf(
			"<< /Length %d /Filter /FlateDecode >>\nstream\n", len(compressed))))
		objects[contentsNumber-1] = append(objects[contentsNumber-1], compressed...)
		objects[contentsNumber-1] = append(objects[contentsNumber-1], []byte("\nendstream")...)

		cmapNumber := add([]byte(buildToUnicodeCMap(plan.chars, plan.codes)))
		fontNumber := add([]byte(fmt.Sprintf(
			"<< /Type /Font /Subtype /Type0 /BaseFont /Subset+Song /Encoding /Identity-H "+
				"/DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>",
			// The descendant font is written right after the page object.
			len(objects)+2, cmapNumber)))
		descriptorNumber := add([]byte(
			"<< /Type /FontDescriptor /FontName /Subset+Song /Flags 4 " +
				"/FontBBox [0 -200 1000 900] /ItalicAngle 0 /Ascent 880 /Descent -120 " +
				"/CapHeight 700 /StemV 80 >>"))
		descendantNumber := add([]byte(fmt.Sprintf(
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Subset+Song "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> "+
				"/FontDescriptor %d 0 R /DW 1000 >>", descriptorNumber)))
		pageNumber := add([]byte(fmt.Sprintf(
			"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
				"/Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
			pagesNumber, fontNumber, contentsNumber)))
		pageObjects = append(pageObjects, pageNumber)
		_ = pageIndex
		_ = descendantNumber
	}

	// The catalog and the pages tree, which had placeholders.
	objects[catalogNumber-1] = []byte(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesNumber))
	kids := ""
	for index, number := range pageObjects {
		if index > 0 {
			kids += " "
		}
		kids += fmt.Sprintf("%d 0 R", number)
	}
	objects[pagesNumber-1] = []byte(fmt.Sprintf(
		"<< /Type /Pages /Kids [%s] /Count %d >>", kids, len(pageObjects)))

	out := &bytes.Buffer{}
	out.WriteString("%PDF-1.4\n")
	out.Write([]byte{'%', 0xe2, 0xe3, 0xcf, 0xd3, '\n'})
	offsets := make([]int, 0, len(objects))
	for index, body := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(out, "%d 0 obj\n", index+1)
		out.Write(body)
		out.WriteString("\nendobj\n")
	}
	xrefAt := out.Len()
	fmt.Fprintf(out, "xref\n0 %d\n", len(objects)+1)
	out.WriteString("0000000000 65535 f \n")
	for _, offset := range offsets {
		fmt.Fprintf(out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(out, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1, catalogNumber, xrefAt)

	// The EXPECTED text is what the extractor's own contract produces, not what went in: each page's
	// rows end with a newline — one line per row, which the chapter detector reads — and the pages
	// are joined by a blank line. Spelling that out here rather than inside each assertion keeps
	// every test's expectation the same shape, and the first version of this fixture got it wrong by
	// stating the INPUT text, which made two tests fail with a diff that looked like an extraction
	// bug and was not one.
	text := ""
	for index, page := range pages {
		if index > 0 {
			text += "\n\n"
		}
		text += page + "\n"
	}
	return pdfFixture{Bytes: out.Bytes(), Text: text}
}

// buildToUnicodeCMap writes the CMap that maps each glyph code to its character.
//
// This is the part that decides whether a reader can recover the text at all, and it is written in
// the `beginbfchar` form the PDF specification defines for exactly this purpose.
func buildToUnicodeCMap(chars []rune, codes []int) string {
	entries := make([]string, 0, len(chars))
	for index, ch := range chars {
		entries = append(entries, fmt.Sprintf("<%04X> <%04X>", codes[index], int(ch)))
	}
	lines := []string{
		"/CIDInit /ProcSet findresource begin",
		"12 dict begin",
		"begincmap",
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def",
		"/CMapName /Adobe-Identity-UCS def",
		"/CMapType 2 def",
		"1 begincodespacerange",
		"<0000> <FFFF>",
		"endcodespacerange",
		fmt.Sprintf("%d beginbfchar", len(entries)),
	}
	lines = append(lines, entries...)
	lines = append(lines, "endbfchar", "endcmap",
		"CMapName currentdict /CMap defineresource pop", "end", "end")
	body := ""
	for index, line := range lines {
		if index > 0 {
			body += "\n"
		}
		body += line
	}
	return "<< /Length " + fmt.Sprint(len(body)) + " >>\nstream\n" + body + "\nendstream"
}

// deflate compresses a content stream, which every real producer does.
func deflate(data []byte) []byte {
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
