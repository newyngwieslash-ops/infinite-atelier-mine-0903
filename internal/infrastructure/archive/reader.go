// Package archive reads and writes ZIP containers under the limits of
// docs/SECURITY.md §9.
//
// The reader is the trust boundary for every archive this application opens: a
// backup, a legacy project pack, or a future Skill pack. It therefore refuses
// hostile input by default rather than sanitising it afterwards:
//
//   - absolute paths, drive letters and UNC prefixes;
//   - any `..` element, including one that only appears after Unicode
//     normalisation;
//   - Windows device names, which are special even without a directory part;
//   - symlinks and other non-regular entries;
//   - duplicate paths that differ only by normalisation;
//   - more entries, larger entries, or a larger total than the caller allows;
//   - a compression ratio above the cap (the ZIP-bomb defence of §9.2);
//   - an entry whose declared size disagrees with what is read.
//
// Every rejection carries one of the `security.archive_*` codes from §17 so the
// caller can distinguish a policy refusal from an I/O failure, and none of them
// is retriable.
package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// Stable error codes from docs/SECURITY.md §17.
const (
	CodePathTraversal = "security.archive_path_traversal"
	CodeBomb          = "security.archive_bomb"
	CodeTooLarge      = "security.file_too_large"
	CodeIntegrity     = "security.integrity_check_failed"
	CodeManifest      = "security.archive_manifest_invalid"
)

// Limits bounds an archive read. A zero field means "use the default"; a
// negative field means "no limit", which callers should avoid.
type Limits struct {
	// MaxEntries caps the number of entries.
	MaxEntries int
	// MaxEntryBytes caps one uncompressed entry.
	MaxEntryBytes int64
	// MaxTotalBytes caps the sum of uncompressed entries.
	MaxTotalBytes int64
	// MaxCompressionRatio caps uncompressed/compressed for one entry, as a
	// cheap early signal of a zip bomb.
	//
	// It is a heuristic, not the defence itself: docs/SECURITY.md §9.2 makes the
	// hard limits (entry count, per-entry and total bytes) the enforcement, with
	// this ratio used to refuse before extraction begins. The default is
	// deliberately loose because real content is occasionally extremely
	// compressible — a zero-filled buffer reaches a ratio above 1,000, and a long
	// still video can reach several thousand. The threshold therefore sits well
	// above legitimate content and far below a real bomb (the classic 42.zip is
	// around 10^11), and the byte ceilings are what actually bound the damage.
	MaxCompressionRatio int64
	// MaxPathLength caps one entry's path in bytes.
	MaxPathLength int
}

// DefaultLimits are the ceilings this application uses for a project archive.
//
// They are generous enough for a real project (a 20 GiB media library across
// 50,000 entries) and small enough that a crafted archive cannot exhaust the
// disk or the address space before a limit fires.
func DefaultLimits() Limits {
	return Limits{
		MaxEntries:          50_000,
		MaxEntryBytes:       4 << 30,
		MaxTotalBytes:       20 << 30,
		MaxCompressionRatio: 10_000,
		MaxPathLength:       512,
	}
}

// normalise applies the Unicode normalisation §9.1 requires before any
// comparison, so a path that only looks like a duplicate is treated as one.
func (l Limits) withDefaults() Limits {
	defaults := DefaultLimits()
	if l.MaxEntries == 0 {
		l.MaxEntries = defaults.MaxEntries
	}
	if l.MaxEntryBytes == 0 {
		l.MaxEntryBytes = defaults.MaxEntryBytes
	}
	if l.MaxTotalBytes == 0 {
		l.MaxTotalBytes = defaults.MaxTotalBytes
	}
	if l.MaxCompressionRatio == 0 {
		l.MaxCompressionRatio = defaults.MaxCompressionRatio
	}
	if l.MaxPathLength == 0 {
		l.MaxPathLength = defaults.MaxPathLength
	}
	return l
}

// newBytesReaderAt adapts a byte slice to the ReaderAt zip.NewReader needs.
// It exists so callers hand over bytes rather than a file: the archive is
// already in memory after a validated read, and this keeps the streaming
// decision in one place.
func newBytesReaderAt(data []byte) *bytes.Reader { return bytes.NewReader(data) }

// Entry is one validated archive member.
type Entry struct {
	// Name is the normalised relative path, safe to use as a map key.
	Name string
	// DeclaredBytes is the size the archive claims.
	DeclaredBytes int64
	// CompressedBytes is the stored size.
	CompressedBytes int64
	// IsDir reports a directory entry, which carries no content.
	IsDir bool
}

// Reader is a validated view of an archive.
type Reader struct {
	reader *zip.Reader
	limits Limits
	// entries maps a normalised path to its entry, which is what callers use to
	// look a member up without touching archive/zip again.
	entries map[string]*zip.File
	order   []Entry
}

// Open parses an archive and validates every entry against the limits.
//
// Validation happens up front, before any content is read, so a hostile archive
// is refused without writing anything: §9.2 requires the bomb estimate to be
// taken from the central directory before extraction begins.
func Open(data []byte, limits Limits) (*Reader, error) {
	resolved := limits.withDefaults()
	reader, err := zip.NewReader(newBytesReaderAt(data), int64(len(data)))
	if err != nil {
		return nil, archiveError(CodeIntegrity, "The archive could not be opened.", err)
	}
	if len(reader.File) > resolved.MaxEntries {
		return nil, archiveError(CodeBomb, "The archive contains too many entries.", nil)
	}

	result := &Reader{reader: reader, limits: resolved, entries: make(map[string]*zip.File, len(reader.File))}
	seen := make(map[string]bool, len(reader.File))
	var total int64

	for _, file := range reader.File {
		normalised, normalizeErr := NormalizePath(file.Name, resolved.MaxPathLength)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		if seen[normalised] {
			// Two entries that normalise to the same path are a duplicate-path
			// attack (§9.1): which one wins would depend on extraction order.
			return nil, archiveError(CodePathTraversal, "The archive contains two entries with the same path.", nil)
		}
		seen[normalised] = true

		if file.Mode()&os.ModeSymlink != 0 {
			// A symlink can redirect a later entry outside the extraction root.
			return nil, archiveError(CodePathTraversal, "The archive contains a symbolic link.", nil)
		}
		if !file.Mode().IsRegular() && !file.FileInfo().IsDir() {
			return nil, archiveError(CodePathTraversal, "The archive contains an unsupported entry type.", nil)
		}

		declared := int64(file.UncompressedSize64)
		if declared < 0 || declared > resolved.MaxEntryBytes {
			return nil, archiveError(CodeTooLarge, "An entry in the archive is too large.", nil)
		}
		total += declared
		if total > resolved.MaxTotalBytes {
			return nil, archiveError(CodeTooLarge, "The archive is too large.", nil)
		}
		compressed := int64(file.CompressedSize64)
		if declared > 0 {
			// The ratio guards against a small archive expanding without bound.
			// A high ratio is not proof of malice, but §9.2 makes it a refusal.
			if compressed <= 0 || declared/compressed > resolved.MaxCompressionRatio {
				return nil, archiveError(CodeBomb, "An entry in the archive is compressed too heavily.", nil)
			}
		}
		entry := Entry{Name: normalised, DeclaredBytes: declared, CompressedBytes: compressed, IsDir: file.FileInfo().IsDir()}
		result.entries[normalised] = file
		result.order = append(result.order, entry)
	}
	return result, nil
}

// Entries returns the validated entries in archive order.
func (r *Reader) Entries() []Entry {
	if r == nil {
		return nil
	}
	return append([]Entry{}, r.order...)
}

// Has reports whether the archive contains a path (after normalisation).
func (r *Reader) Has(name string) bool {
	if r == nil {
		return false
	}
	normalised, err := NormalizePath(name, r.limits.MaxPathLength)
	if err != nil {
		return false
	}
	_, ok := r.entries[normalised]
	return ok
}

// Read returns one entry's content.
//
// The declared size is enforced while reading, so an archive whose central
// directory lies about a size cannot stream past the limit it claimed.
func (r *Reader) Read(name string) ([]byte, error) {
	if r == nil {
		return nil, archiveError(CodeIntegrity, "The archive is not open.", nil)
	}
	normalised, err := NormalizePath(name, r.limits.MaxPathLength)
	if err != nil {
		return nil, err
	}
	file, ok := r.entries[normalised]
	if !ok {
		return nil, archiveError(CodeIntegrity, "The archive is missing a file it should contain.", nil)
	}
	if file.FileInfo().IsDir() {
		return nil, archiveError(CodeIntegrity, "That archive path is a directory.", nil)
	}
	stream, err := file.Open()
	if err != nil {
		return nil, archiveError(CodeIntegrity, "An archive entry could not be read.", err)
	}
	defer stream.Close()

	// A limited reader enforces the ceiling even if the header lied.
	limited := io.LimitReader(stream, r.limits.MaxEntryBytes+1)
	content, err := io.ReadAll(limited)
	if err != nil {
		return nil, archiveError(CodeIntegrity, "An archive entry could not be read.", err)
	}
	if int64(len(content)) > r.limits.MaxEntryBytes {
		return nil, archiveError(CodeTooLarge, "An archive entry exceeded its declared size.", nil)
	}
	if file.UncompressedSize64 != 0 && int64(len(content)) != int64(file.UncompressedSize64) {
		return nil, archiveError(CodeIntegrity, "An archive entry did not match its declared size.", nil)
	}
	return content, nil
}

// NormalizePath validates one archive path and returns its canonical form.
//
// The canonical form is forward-slash separated, NFC normalised, with no
// leading separator, no empty or dot elements, and no `..` element. Anything
// else is refused, so a caller never has to reason about a path that escaped.
func NormalizePath(value string, maxLength int) (string, error) {
	if value == "" {
		return "", archiveError(CodePathTraversal, "The archive contains an empty path.", nil)
	}
	if maxLength <= 0 {
		maxLength = DefaultLimits().MaxPathLength
	}
	if len(value) > maxLength {
		return "", archiveError(CodePathTraversal, "An archive path is too long.", nil)
	}
	if !utf8.ValidString(value) {
		return "", archiveError(CodePathTraversal, "An archive path is not valid text.", nil)
	}
	// Normalise first: two spellings of the same text must compare equal, and a
	// composed ".." must be seen as one.
	normalised := norm.NFC.String(value)
	// A backslash is a separator on Windows, so it cannot be left in a path.
	normalised = strings.ReplaceAll(normalised, "\\", "/")
	if strings.HasPrefix(normalised, "/") {
		return "", archiveError(CodePathTraversal, "The archive contains an absolute path.", nil)
	}
	// A drive letter or UNC prefix survives on Windows even without a leading
	// slash.
	if len(normalised) >= 2 && normalised[1] == ':' {
		return "", archiveError(CodePathTraversal, "The archive contains an absolute path.", nil)
	}
	if strings.HasPrefix(normalised, "//") {
		return "", archiveError(CodePathTraversal, "The archive contains an absolute path.", nil)
	}
	// NUL and control characters have no place in a path.
	for _, character := range normalised {
		if character < 0x20 || character == 0x7f {
			return "", archiveError(CodePathTraversal, "An archive path contains a control character.", nil)
		}
	}

	trimmed := strings.TrimSuffix(normalised, "/")
	if trimmed == "" {
		return "", archiveError(CodePathTraversal, "The archive contains an empty path.", nil)
	}
	elements := strings.Split(trimmed, "/")
	cleaned := make([]string, 0, len(elements))
	for _, element := range elements {
		switch element {
		case "":
			// An empty element means a doubled separator, which is ambiguous.
			return "", archiveError(CodePathTraversal, "An archive path is malformed.", nil)
		case ".":
			return "", archiveError(CodePathTraversal, "An archive path is malformed.", nil)
		case "..":
			return "", archiveError(CodePathTraversal, "An archive path escapes the archive root.", nil)
		}
		if isWindowsDeviceName(element) {
			return "", archiveError(CodePathTraversal, "An archive path uses a reserved device name.", nil)
		}
		if isTrailingDeviceElement(element) {
			return "", archiveError(CodePathTraversal, "An archive path is malformed.", nil)
		}
		cleaned = append(cleaned, element)
	}
	canonical := path.Join(cleaned...)
	if canonical == "" || canonical == "." {
		return "", archiveError(CodePathTraversal, "The archive contains an empty path.", nil)
	}
	return canonical, nil
}

// deviceNames are the Windows reserved names: they name a device even with an
// extension, so `CON.txt` is as special as `CON`.
var deviceNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// isWindowsDeviceName reports whether an element names a reserved device.
func isWindowsDeviceName(element string) bool {
	lowered := strings.ToLower(element)
	if deviceNames[lowered] {
		return true
	}
	if index := strings.IndexByte(lowered, '.'); index > 0 {
		return deviceNames[lowered[:index]]
	}
	return false
}

// isTrailingDeviceElement rejects the Windows "trailing dot or space" forms,
// which the filesystem silently strips: `a.` and `a ` both resolve to `a`, so
// two distinct archive entries could collide on disk.
func isTrailingDeviceElement(element string) bool {
	if element == "" {
		return false
	}
	last := element[len(element)-1]
	return last == '.' || last == ' '
}

// archiveError builds a security-classified archive error. It is never
// retriable: a refused archive will be refused again (§17).
func archiveError(code, message string, cause error) error {
	return apperror.New(code, "security", false, message, cause)
}

// IsArchiveRefusal reports whether an error came from this package's checks.
func IsArchiveRefusal(err error) bool {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return false
	}
	switch appErr.Code {
	case CodePathTraversal, CodeBomb, CodeTooLarge, CodeIntegrity, CodeManifest:
		return true
	default:
		return false
	}
}
