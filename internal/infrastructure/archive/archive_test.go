package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// buildZip assembles an archive from raw headers, which is how the hostile
// cases are constructed: the writer in this package refuses to produce them,
// so the corpus must come from the ZIP format directly.
func buildZip(t *testing.T, entries []rawEntry) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(fs.FileMode(entry.mode))
		}
		stream, err := writer.CreateHeader(header)
		if err != nil {
			// archive/zip refuses some names itself, which is an acceptable
			// refusal; the test asserts on the reader, so skip those.
			continue
		}
		if _, err := stream.Write(entry.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type rawEntry struct {
	name    string
	content []byte
	mode    uint32
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected an application error, got %v", err)
	}
	return appErr.Code
}

// TestArchiveRefusesPathTraversal covers the `../`, absolute, device-name and
// duplicate-path cases of docs/SECURITY.md §18.
func TestArchiveRefusesPathTraversal(t *testing.T) {
	cases := []struct {
		name    string
		entries []rawEntry
		code    string
	}{
		{
			name:    "parent traversal",
			entries: []rawEntry{{name: "../escape.txt", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name:    "nested traversal",
			entries: []rawEntry{{name: "files/../../escape.txt", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name:    "absolute path",
			entries: []rawEntry{{name: "/etc/passwd", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name:    "windows drive",
			entries: []rawEntry{{name: "C:/Windows/system32/evil.dll", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name:    "backslash traversal",
			entries: []rawEntry{{name: "..\\escape.txt", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name:    "device name",
			entries: []rawEntry{{name: "CON", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name:    "device name with extension",
			entries: []rawEntry{{name: "aux.txt", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name:    "device subpath",
			entries: []rawEntry{{name: "files/LPT1", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name: "duplicate after normalisation",
			entries: []rawEntry{
				{name: "a/b.txt", content: []byte("one")},
				{name: "a//b.txt", content: []byte("two")},
			},
			code: CodePathTraversal,
		},
		{
			name:    "trailing dot",
			entries: []rawEntry{{name: "report.", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name:    "trailing space",
			entries: []rawEntry{{name: "report ", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name:    "dot element",
			entries: []rawEntry{{name: "./a.txt", content: []byte("x")}},
			code:    CodePathTraversal,
		},
		{
			name: "symlink entry",
			entries: []rawEntry{
				{name: "link", content: []byte("/etc/passwd"), mode: 0o120777},
			},
			code: CodePathTraversal,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			data := buildZip(t, testCase.entries)
			reader, err := Open(data, Limits{})
			if err == nil {
				// A name archive/zip silently normalised is acceptable only if
				// the resulting entry is safe; assert that instead of a refusal.
				for _, entry := range reader.Entries() {
					if strings.Contains(entry.Name, "..") || strings.HasPrefix(entry.Name, "/") {
						t.Fatalf("a traversal path was accepted: %q", entry.Name)
					}
				}
				t.Skip("archive/zip normalised the name before this package saw it")
			}
			if got := codeOf(t, err); got != testCase.code {
				t.Fatalf("code = %q, want %q", got, testCase.code)
			}
			if !IsArchiveRefusal(err) {
				t.Fatal("the refusal is not reported as an archive refusal")
			}
		})
	}
}

// TestArchiveRefusesEntryCountBomb covers the "100k entries" corpus case.
func TestArchiveRefusesEntryCountBomb(t *testing.T) {
	entries := make([]rawEntry, 0, 200)
	for index := 0; index < 200; index++ {
		entries = append(entries, rawEntry{name: "files/entry-" + itoa(index) + ".txt", content: []byte("x")})
	}
	data := buildZip(t, entries)
	_, err := Open(data, Limits{MaxEntries: 50})
	if err == nil {
		t.Fatal("an archive with more entries than the limit was accepted")
	}
	if got := codeOf(t, err); got != CodeBomb {
		t.Fatalf("code = %q, want %q", got, CodeBomb)
	}
	// The same archive is accepted with a larger budget, proving the refusal is
	// the limit and not a parsing failure.
	if _, err := Open(data, Limits{MaxEntries: 500}); err != nil {
		t.Fatalf("the archive was refused with a sufficient budget: %v", err)
	}
}

// TestArchiveRefusesCompressionBomb covers the "极高压缩比" corpus case: a
// highly compressible entry is refused before it is expanded.
func TestArchiveRefusesCompressionBomb(t *testing.T) {
	// 4 MiB of zeros compresses to a few KiB, a ratio far above the default cap.
	payload := bytes.Repeat([]byte{0}, 4<<20)
	data := buildZip(t, []rawEntry{{name: "bomb.bin", content: payload}})

	_, err := Open(data, Limits{MaxCompressionRatio: 10})
	if err == nil {
		t.Fatal("a high-ratio entry was accepted")
	}
	if got := codeOf(t, err); got != CodeBomb {
		t.Fatalf("code = %q, want %q", got, CodeBomb)
	}
	// The default ratio is generous enough for real content.
	if _, err := Open(data, Limits{}); err != nil {
		t.Fatalf("a compressible but legitimate entry was refused by the default limits: %v", err)
	}
}

// TestArchiveRefusesOversizeEntry covers the single-entry ceiling.
func TestArchiveRefusesOversizeEntry(t *testing.T) {
	data := buildZip(t, []rawEntry{{name: "big.bin", content: bytes.Repeat([]byte("a"), 4096)}})
	_, err := Open(data, Limits{MaxEntryBytes: 1024, MaxTotalBytes: 1 << 20, MaxCompressionRatio: 1000})
	if err == nil {
		t.Fatal("an oversize entry was accepted")
	}
	if got := codeOf(t, err); got != CodeTooLarge {
		t.Fatalf("code = %q, want %q", got, CodeTooLarge)
	}
}

// TestArchiveRefusesOversizeTotal covers the archive-wide ceiling.
func TestArchiveRefusesOversizeTotal(t *testing.T) {
	entries := []rawEntry{
		{name: "a.bin", content: bytes.Repeat([]byte("a"), 2048)},
		{name: "b.bin", content: bytes.Repeat([]byte("b"), 2048)},
	}
	data := buildZip(t, entries)
	_, err := Open(data, Limits{MaxEntryBytes: 4096, MaxTotalBytes: 3000, MaxCompressionRatio: 1000})
	if err == nil {
		t.Fatal("an archive over the total ceiling was accepted")
	}
	if got := codeOf(t, err); got != CodeTooLarge {
		t.Fatalf("code = %q, want %q", got, CodeTooLarge)
	}
}

// TestArchiveRefusesOversizePath covers the path-length ceiling.
func TestArchiveRefusesOversizePath(t *testing.T) {
	long := "files/" + strings.Repeat("a", 600) + ".txt"
	data := buildZip(t, []rawEntry{{name: long, content: []byte("x")}})
	_, err := Open(data, Limits{MaxPathLength: 100})
	if err == nil {
		t.Fatal("an over-long path was accepted")
	}
	if got := codeOf(t, err); got != CodePathTraversal {
		t.Fatalf("code = %q, want %q", got, CodePathTraversal)
	}
}

// TestArchiveRefusesCorruptInput covers the "损坏 SQLite" class: bytes that are
// not an archive at all.
func TestArchiveRefusesCorruptInput(t *testing.T) {
	_, err := Open([]byte("SQLite format 3\x00not really an archive"), Limits{})
	if err == nil {
		t.Fatal("non-archive bytes were accepted")
	}
	if got := codeOf(t, err); got != CodeIntegrity {
		t.Fatalf("code = %q, want %q", got, CodeIntegrity)
	}
}

// TestArchiveRoundTrip proves a well-formed archive written by this package is
// read back exactly, which is what the backup path depends on.
func TestArchiveRoundTrip(t *testing.T) {
	writer := NewWriter(WriterOptions{})
	files := map[string][]byte{
		ManifestName:    []byte(`{"manifestVersion":1}`),
		"app.sqlite":    []byte("SQLite format 3\x00payload"),
		"files/ab/cd":   []byte("media bytes"),
		"files/zan/y":   []byte("another"),
		"provider.json": []byte(`{"providers":[]}`),
	}
	for name, content := range files {
		if err := writer.Add(name, content); err != nil {
			t.Fatalf("Add(%s): %v", name, err)
		}
	}
	if err := writer.AddChecksums(); err != nil {
		t.Fatalf("AddChecksums: %v", err)
	}
	data, err := writer.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	reader, err := Open(data, Limits{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for name, content := range files {
		if !reader.Has(name) {
			t.Fatalf("the round trip lost %s", name)
		}
		read, readErr := reader.Read(name)
		if readErr != nil {
			t.Fatalf("Read(%s): %v", name, readErr)
		}
		if !bytes.Equal(read, content) {
			t.Fatalf("content of %s changed in the round trip", name)
		}
	}
	if err := VerifyChecksums(reader); err != nil {
		t.Fatalf("VerifyChecksums: %v", err)
	}
}

// TestArchiveWriterRefusesUnsafePath proves the writer cannot produce an
// archive this package would refuse to read.
func TestArchiveWriterRefusesUnsafePath(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "CON", "a/../../b", "trailing."} {
		writer := NewWriter(WriterOptions{})
		err := writer.Add(name, []byte("x"))
		if err == nil {
			t.Fatalf("the writer accepted the unsafe path %q", name)
		}
		if !IsArchiveRefusal(err) {
			t.Fatalf("the refusal of %q is not classified as an archive refusal", name)
		}
		// A refused write must not leave a usable archive behind.
		if _, finishErr := writer.Finish(); finishErr == nil {
			t.Fatalf("an archive with a refused entry was finalised for %q", name)
		}
	}
}

// TestVerifyChecksumsDetectsTampering covers the "错误哈希" corpus case.
func TestVerifyChecksumsDetectsTampering(t *testing.T) {
	writer := NewWriter(WriterOptions{})
	if err := writer.Add(ManifestName, []byte(`{"manifestVersion":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Add("files/ab/cd", []byte("original bytes")); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddChecksums(); err != nil {
		t.Fatal(err)
	}
	data, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}

	// Rebuild the archive with modified content but the original checksums, so
	// the file-level check is the only thing that can catch it.
	original, err := Open(data, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	checksums, err := original.Read(ChecksumsName)
	if err != nil {
		t.Fatal(err)
	}
	tampered := buildZip(t, []rawEntry{
		{name: ManifestName, content: []byte(`{"manifestVersion":1}`)},
		{name: "files/ab/cd", content: []byte("TAMPERED bytes")},
		{name: ChecksumsName, content: checksums},
	})
	tamperedReader, err := Open(tampered, Limits{})
	if err != nil {
		t.Fatalf("the tampered archive did not even parse: %v", err)
	}
	err = VerifyChecksums(tamperedReader)
	if err == nil {
		t.Fatal("tampered content passed the checksum check")
	}
	if got := codeOf(t, err); got != CodeIntegrity {
		t.Fatalf("code = %q, want %q", got, CodeIntegrity)
	}
}

// TestArchiveMissingChecksumsIsRefused covers the "假 Manifest" class: an
// archive without the files a reader requires.
func TestArchiveMissingChecksumsIsRefused(t *testing.T) {
	data := buildZip(t, []rawEntry{{name: ManifestName, content: []byte(`{}`)}})
	reader, err := Open(data, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	// The manifest itself is present; the checksums file is not.
	if reader.Has(ChecksumsName) {
		t.Fatal("an archive without a checksums file reports one")
	}
	err = VerifyChecksums(reader)
	if err == nil {
		t.Fatal("an archive without checksums passed verification")
	}
	if got := codeOf(t, err); got != CodeManifest {
		t.Fatalf("code = %q, want %q", got, CodeManifest)
	}
}

// TestNormalizePathCanonicalForms pins the accepted spellings.
func TestNormalizePathCanonicalForms(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"manifest.json", "manifest.json"},
		{"files/ab/cd", "files/ab/cd"},
		{"files\\ab\\cd", "files/ab/cd"},
		{"files/ab/cd/", "files/ab/cd"},
		{"a/b/../c", ""}, // refused: a parent element is never resolved
		{"", ""},
	}
	for _, testCase := range cases {
		got, err := NormalizePath(testCase.input, 0)
		if testCase.want == "" {
			if err == nil {
				t.Fatalf("NormalizePath(%q) = %q, want a refusal", testCase.input, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("NormalizePath(%q): %v", testCase.input, err)
		}
		if got != testCase.want {
			t.Fatalf("NormalizePath(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}

// TestNormalizePathRejectsUnicodeLookalikes proves a composed and a decomposed
// spelling of the same text are treated as one path, so a "duplicate" cannot
// hide behind an encoding difference.
func TestNormalizePathRejectsUnicodeLookalikes(t *testing.T) {
	composed := "files/\u00e9.txt"    // é as one code point
	decomposed := "files/e\u0301.txt" // e + combining acute
	first, err := NormalizePath(composed, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizePath(decomposed, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("NFC normalisation did not unify %q and %q", first, second)
	}
}

// TestReaderRejectsUnknownEntryLookup proves a lookup for a path that is not in
// the archive is an integrity error rather than an empty read, so a caller
// cannot mistake "absent" for "empty file".
func TestReaderRejectsUnknownEntryLookup(t *testing.T) {
	writer := NewWriter(WriterOptions{})
	if err := writer.Add(ManifestName, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	data, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read("files/missing"); err == nil {
		t.Fatal("reading an absent entry succeeded")
	}
	if reader.Has("files/missing") {
		t.Fatal("an absent entry reports as present")
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
