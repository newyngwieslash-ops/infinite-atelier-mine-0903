# ADR-0023 PDF Import: the Library That Cannot Read Chinese, and the One That Panics

Status: Accepted
Date: 2026-09-25
Work package: WP-19 (P3 item 24)
Supersedes: none
Related: ADR-0007 (the fact layer), the importing pipeline's own formats (WP-06), PRD FR-020,
AC-STORY-001, AGENTS §6, SECURITY (untrusted input)

## Context

PRD §16's v1.0 list carries PDF 导入. The import pipeline already had four layers — format detection,
text extraction, encoding detection, chapter splitting — and a deliberate rule stated in its own
comment: **the bytes decide the format, not the extension**. So the question was not "how do we build
an importer" but "which extractor drops into the second layer", and that question was answered by
probe rather than by preference.

AGENTS §6 requires vetting a new dependency's licence and maintenance state before adding it. This
package adds the first Go dependency since the SQLite driver, so the vetting is recorded here.

## The probe, and what it settled

A Chinese PDF was built by hand (the generator now lives in `pdf_fixture_test.go`, so the probe is
re-run by the test suite rather than resting on this document): one page, a Type0 font with an
Identity-H encoding, and a `ToUnicode` CMap mapping each two-byte glyph code to its character. **That
CMap is the whole question.** A Chinese document's text is not stored as characters; it is stored as
CID codes into a subset font, and a reader learns which character each code means only from the CMap.

Two candidates were probed against it:

| library | result on `沈砚的渡口` | verdict |
|---|---|---|
| `rsc.io/pdf` v0.1.1 | `"\x10\x00\x10\x01\x10\x02\x10\x03\x10\x04"` — the RAW GLYPH CODES | **rejected** |
| `github.com/ledongthuc/pdf` (pseudo-version) | `"沈砚的渡口"` | chosen |

**`rsc.io/pdf` cannot read Chinese.** It reads the font dictionary far enough to see `ToUnicode` and
does not apply it to the text it extracts. Every document this product imports is Chinese, so the
library is useless here however well maintained it is — and it is the obvious first choice, which is
exactly why the rejection is recorded rather than assumed.

The chosen library was then fuzzed, because an importer parses untrusted bytes:

- **120 corrupted variants of that same file: THREE PANICS.** `invalid real .`, `unexpected non-name
  key int64(0) parsing dictionary`, and `missing endobj after indirect object definition`. All three
  come from `lex.go`'s `errorf`, which is how the library signals malformed input: it panics instead
  of returning an error.
- **Zero hangs**, and four hand-built hostile files (not a PDF, truncated, an xref past the end, 400
  nested objects) all returned errors.

So the library is usable **with a recover**, and unusable without one: an importer that takes a
corrupted file from a user's disk and takes the process down with it is not an importer.

## Decision

**1. `github.com/ledongthuc/pdf` is the extractor, and the reason is the table above.** Not
popularity, not maintenance: the other candidate cannot produce the text this product needs.

**2. Every call into it is wrapped in a narrow `recover`.** `pdfPageTexts` exists as a separate
function for exactly this reason — the recover must wrap the library and nothing else. A `recover`
around a larger body would swallow this repository's own bugs and report them as "the PDF was
malformed", which is a bug nobody would find.

**3. The limits are a VALUE the extractor takes, not constants read at the comparison.**
`pdfLimits{MaxPages, MaxPageTextBytes}` with `defaultPDFLimits()` for production. This is a
testability decision that turned into a correctness one: probing the real bounds meant either building
2001 real pages — NINE MINUTES, so the test would stop being run — or doctoring the page tree, which
broke the cross-reference table and made the refusal come from the parser rather than from the bound.
Passing the limits in lets a test assert the REAL comparison on a SMALL document. A zero-valued policy
means "use the defaults" rather than "refuse everything", which is the trap a struct of limits invites
and is asserted.

**4. A document with no text layer is refused for THAT reason.** A scan carries an image and no text;
returning an empty document would tell a user nothing, and the remedy — run it through OCR — is
different from the remedy for a corrupt file. The refusal says "probably a scan" and names the
alternative.

**5. No OCR, no decryption, no layout reconstruction.** An encrypted PDF is refused. Text comes out in
content-stream order, so a two-column page interleaves its columns' lines. That is the shared limit of
every non-OCR extractor, and it is stated here rather than glossed as "PDF support".

**6. `FormatPDF` joins the format vocabulary, and the vocabulary stays CLOSED.** Two tests used to
assert that `pdf` was REFUSED, with the reasoning "the specification puts it in V1, so it must stay
refused by an MVP import". WP-19 is that V1 item, so those assertions flip to the new state with the
old reasoning quoted in place. A reader who remembers the old rule finds out that it changed and where,
rather than discovering the test is gone.

## Consequences

- A user can import a text-layer PDF, and its characters come out as characters. The claim is asserted
  against characters, not against "something non-empty", which is what makes the test able to see the
  difference the probe found.
- The chapter detector, the encoding detector and the offset arithmetic all work on PDF text unchanged:
  the extractor emits UTF-8, so `DetectEncoding` passes it through. That reuse is the payoff of not
  writing a second importer.
- **A real maintenance risk is recorded rather than hidden**: the module has no tagged release and is
  pinned to a pseudo-version by commit. The mitigation is that exactly one function calls it and it does
  one thing, so a replacement would be a one-file change. AGENTS §6's licence requirement is met —
  BSD-3-Clause (Go Authors), the same terms as the SQLite driver's tree — and both the SBOM and
  `THIRD_PARTY_NOTICES.md` carry it. The verification gate FAILED on the first run after the
  dependency was added, which is the gate working: `sbom/cyclonedx.json` had drifted and was
  regenerated.

## Alternatives rejected

**`rsc.io/pdf`.** Cannot read Chinese. See the table.

**No library: parse the PDF ourselves.** Rejected: PDF is a large specification, a hand-written lexer
is a fuzzing target with our name on it, and the value here is a document reader rather than a PDF
engine.

**OCR, so scans work too.** Rejected for this package: it is a much larger capability with its own
dependency and its own resource questions, and PRD §16 lists it nowhere. A user with a scan gets a
refusal that names their actual situation.

**Trust the declared page count without a bound.** Rejected: the count is attacker-controlled and the
work is ours, which is the same reasoning the DOCX archive limits already encode.

**Let the panic propagate and treat it as a crash the shell handles.** Rejected: a document a user
chose is not a reason to lose their session, and a panic inside a lexer is not distinguishable from a
bug of ours at the shell's level.
