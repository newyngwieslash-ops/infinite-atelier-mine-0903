package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// ManifestName is the versioned manifest every ordinary backup carries. A
// reader checks for it before anything else, so a false manifest is refused
// before an entry is extracted (docs/SECURITY.md §18).
const ManifestName = "manifest.json"

// ChecksumsName is the file listing a SHA-256 per archive entry.
const ChecksumsName = "checksums.txt"

// WriterOptions configures an archive write.
type WriterOptions struct {
	// CompressionLevel is the deflate level. The default keeps the ratio well
	// under the reader's cap so a round trip is not refused by this package's
	// own bomb defence.
	CompressionLevel int
	Limits           Limits
}

// Writer accumulates entries and produces a ZIP.
type Writer struct {
	buffer  *bytes.Buffer
	writer  *zip.Writer
	hashes  map[string]string
	limits  Limits
	written bool
	// refused records that an entry was rejected. A refused write leaves the
	// archive incomplete, so Finish refuses to produce bytes for it: silently
	// returning a partial archive would turn a caller's mistake into data loss.
	refused error
}

// NewWriter builds an archive writer.
func NewWriter(options WriterOptions) *Writer {
	limits := options.Limits.withDefaults()
	buffer := &bytes.Buffer{}
	return &Writer{
		buffer: buffer,
		writer: zip.NewWriter(buffer),
		hashes: map[string]string{},
		limits: limits,
	}
}

// Add writes one entry.
//
// The path is validated the same way a read is, so this package cannot produce
// an archive it would refuse to open. Media bytes are compressed with deflate;
// an already-compressed format inflates slightly, which is harmless because the
// reader's ratio check only rejects a ratio above 1:200 and a negative
// "compression" simply skips the check.
func (w *Writer) Add(name string, content []byte) error {
	if w == nil {
		return archiveError(CodeIntegrity, "The archive is not open for writing.", nil)
	}
	if w.written {
		return archiveError(CodeIntegrity, "The archive was already finalised.", nil)
	}
	normalised, err := NormalizePath(name, w.limits.MaxPathLength)
	if err != nil {
		w.refused = err
		return err
	}
	if int64(len(content)) > w.limits.MaxEntryBytes {
		err := archiveError(CodeTooLarge, "An entry is too large to archive.", nil)
		w.refused = err
		return err
	}
	header := &zip.FileHeader{Name: normalised, Method: zip.Deflate}
	header.SetMode(0o644)
	entry, err := w.writer.CreateHeader(header)
	if err != nil {
		return archiveError(CodeIntegrity, "An archive entry could not be created.", err)
	}
	if _, err := entry.Write(content); err != nil {
		return archiveError(CodeIntegrity, "An archive entry could not be written.", err)
	}
	sum := sha256.Sum256(content)
	w.hashes[normalised] = hex.EncodeToString(sum[:])
	return nil
}

// AddChecksums writes the checksums file covering every entry so far.
//
// It must be called after the content entries and before Finish, and it is not
// itself listed, because a file cannot contain its own hash.
func (w *Writer) AddChecksums() error {
	if w == nil {
		return archiveError(CodeIntegrity, "The archive is not open for writing.", nil)
	}
	names := make([]string, 0, len(w.hashes))
	for name := range w.hashes {
		names = append(names, name)
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		builder.WriteString(w.hashes[name])
		builder.WriteString("  ")
		builder.WriteString(name)
		builder.WriteString("\n")
	}
	return w.addRaw(ChecksumsName, []byte(builder.String()))
}

// addRaw writes an entry without recording its hash, for the checksums file.
func (w *Writer) addRaw(name string, content []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o644)
	entry, err := w.writer.CreateHeader(header)
	if err != nil {
		return archiveError(CodeIntegrity, "An archive entry could not be created.", err)
	}
	if _, err := entry.Write(content); err != nil {
		return archiveError(CodeIntegrity, "An archive entry could not be written.", err)
	}
	return nil
}

// Finish closes the archive and returns its bytes.
//
// It refuses when an entry was rejected earlier: the archive would be missing
// content the caller asked for, and returning it would present a partial
// backup as complete.
func (w *Writer) Finish() ([]byte, error) {
	if w == nil {
		return nil, archiveError(CodeIntegrity, "The archive is not open for writing.", nil)
	}
	if w.written {
		return nil, archiveError(CodeIntegrity, "The archive was already finalised.", nil)
	}
	if w.refused != nil {
		return nil, archiveError(CodeIntegrity, "The archive is incomplete because an entry was refused.", w.refused)
	}
	if err := w.writer.Close(); err != nil {
		return nil, archiveError(CodeIntegrity, "The archive could not be finalised.", err)
	}
	w.written = true
	// A self-check: the reader must accept what the writer produced. If it does
	// not, this package's own limits are inconsistent, and failing here is far
	// better than shipping an archive the application cannot reopen.
	if _, err := Open(w.buffer.Bytes(), w.limits); err != nil {
		var appErr *apperror.Error
		if ok := asAppError(err, &appErr); ok {
			return nil, archiveError(CodeIntegrity, "The archive failed its own validation.", err)
		}
		return nil, err
	}
	return w.buffer.Bytes(), nil
}

// asAppError is a tiny errors.As wrapper kept local so this file does not
// depend on the errors package for one call.
func asAppError(err error, target **apperror.Error) bool {
	for err != nil {
		if typed, ok := err.(*apperror.Error); ok {
			*target = typed
			return true
		}
		type unwrapper interface{ Unwrap() error }
		next, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = next.Unwrap()
	}
	return false
}

// VerifyChecksums checks an archive's entries against its checksums file.
//
// A mismatch is the "错误哈希" case of docs/SECURITY.md §18: the import must
// stop rather than accept bytes that changed in transit.
func VerifyChecksums(reader *Reader) error {
	if reader == nil {
		return archiveError(CodeIntegrity, "The archive is not open.", nil)
	}
	if !reader.Has(ChecksumsName) {
		return archiveError(CodeManifest, "The archive has no checksums file.", nil)
	}
	raw, err := reader.Read(ChecksumsName)
	if err != nil {
		return err
	}
	expected := parseChecksums(string(raw))
	if len(expected) == 0 {
		return archiveError(CodeManifest, "The archive checksums file is empty or malformed.", nil)
	}
	for _, entry := range reader.Entries() {
		if entry.IsDir || entry.Name == ChecksumsName {
			continue
		}
		want, ok := expected[entry.Name]
		if !ok {
			return archiveError(CodeManifest, "An archive entry is missing from the checksums file.", nil)
		}
		content, readErr := reader.Read(entry.Name)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != want {
			return archiveError(CodeIntegrity, "An archive entry does not match its checksum.", nil)
		}
	}
	return nil
}

// parseChecksums reads the `<hash>  <name>` lines the writer produces.
func parseChecksums(value string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(value, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		fields := strings.SplitN(trimmed, "  ", 2)
		if len(fields) != 2 {
			continue
		}
		name, err := NormalizePath(strings.TrimSpace(fields[1]), 0)
		if err != nil {
			continue
		}
		result[name] = strings.TrimSpace(fields[0])
	}
	return result
}
