package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"

	applegacy "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/legacy"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// LegacyUploadBinding is the chunked channel a legacy blob travels through.
//
// A Wails binding can only carry text, so binary crosses as base64. Sending a
// whole blob in one call would materialise it several times in memory, and the
// legacy stores legitimately hold videos of hundreds of megabytes, so the
// transfer is chunked:
//
//	BeginLegacyFile  -> an upload id and a private temporary file
//	AppendLegacyFile -> one bounded base64 chunk
//	FinishLegacyFile -> size and hash verified, then committed to the FileStore
//	AbortLegacyFile  -> the temporary file removed
//
// The temporary name is generated here and never derived from the caller's key,
// so no chunk can steer a write outside the managed temp directory.
type LegacyUploadBinding struct {
	mu      sync.Mutex
	ctx     context.Context
	uploads map[string]*legacyUpload
	// committed maps a legacy key to the hash of its stored bytes, which is what
	// the import service reads through.
	committed map[string]committedMedia
	// tempRoot is the managed temporary directory.
	tempRoot string
	// files commits a verified payload into content-addressed storage.
	files applegacy.FileCommitter
	// store reads a committed object back by hash.
	store ContentReader
	// maxFileBytes caps one transfer. It defaults to MaxLegacyFileBytes and is a
	// field so a test can exercise the ceiling without building a multi-gigabyte
	// payload.
	maxFileBytes int64
}

// committedMedia is one stored legacy blob.
type committedMedia struct {
	hash     string
	mimeType string
}

// legacyUpload is one in-flight transfer.
type legacyUpload struct {
	legacyKey string
	mimeType  string
	declared  int64
	path      string
	file      *os.File
	total     int64
}

// ContentReader reads a committed object by its content hash.
type ContentReader interface {
	Open(ctx context.Context, storageKey string) (io.ReadCloser, error)
}

// NewLegacyUploadBinding builds the channel.
//
// The binding is created before Wails startup, when it must already exist on the
// binding surface, so its dependencies arrive later through Configure.
func NewLegacyUploadBinding() *LegacyUploadBinding {
	return &LegacyUploadBinding{
		uploads:   map[string]*legacyUpload{},
		committed: map[string]committedMedia{},
	}
}

// ConfigureLegacyUpload supplies the dependencies and marks the channel usable.
//
// It is a package-level function rather than a method because Wails binds every
// exported method on a bound struct, and this one takes a filesystem path and an
// internal interface. Exposing either on the webview surface is exactly what the
// binding conventions forbid, so only the four transfer methods are methods and
// only they are reachable from the frontend.
func ConfigureLegacyUpload(binding *LegacyUploadBinding, tempRoot string, files applegacy.FileCommitter, store ContentReader) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	defer binding.mu.Unlock()
	binding.tempRoot = tempRoot
	binding.files = files
	binding.store = store
	if binding.uploads == nil {
		binding.uploads = map[string]*legacyUpload{}
	}
	if binding.committed == nil {
		binding.committed = map[string]committedMedia{}
	}
}

// AttachLegacyUpload supplies the context.
func AttachLegacyUpload(binding *LegacyUploadBinding, ctx context.Context) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.mu.Unlock()
}

// LegacyUploadMediaSource returns the reader the import service consumes.
//
// It reports the bytes that were uploaded, so the import commits exactly what
// the browser handed over rather than re-reading the browser store. It is a
// package-level function returning an unexported reader type: the reader must
// stay off the webview surface, which is why it is not a method on the binding.
func LegacyUploadMediaSource(binding *LegacyUploadBinding) applegacy.MediaSource {
	if binding == nil {
		return nil
	}
	return &legacyMediaReader{binding: binding}
}

// Bounds for one transfer.
const (
	// MaxLegacyChunkBytes is the decoded size of one chunk. Its base64 form is
	// about a third larger, so this bounds one binding call to roughly 5.4 MiB
	// of text.
	MaxLegacyChunkBytes = 4 << 20
	// MaxLegacyFileBytes caps one file. A legacy project can hold long videos,
	// so the ceiling is high; the archive limits bound a restore separately.
	MaxLegacyFileBytes = 2 << 30
	// MaxLegacyUploads bounds concurrent in-flight transfers.
	MaxLegacyUploads = 8
	// MaxCommittedMedia bounds how many legacy keys one session keeps resolvable.
	MaxCommittedMedia = 100_000
)

// uploadIDs mints transfer identifiers through the shared UUIDv7 generator
// (ADR-0005), so the bookkeeping uses the same identifier scheme as everything
// else.
var uploadIDs = id.NewGenerator()

// BeginLegacyFile starts a transfer and returns its upload id.
func (b *LegacyUploadBinding) BeginLegacyFile(legacyKey, mimeType string, declaredBytes float64) (string, error) {
	if b == nil || b.files == nil || b.tempRoot == "" {
		return "", bindingUnavailable()
	}
	if legacyKey == "" || len(legacyKey) > 200 {
		return "", bindingInvalidInput()
	}
	if declaredBytes < 0 || int64(declaredBytes) > b.fileLimit() {
		return "", apperror.New("IMPORT_FILE_TOO_LARGE", "import", false,
			"That file is too large to import.", nil)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.uploads) >= MaxLegacyUploads {
		return "", apperror.New("IMPORT_TOO_MANY_UPLOADS", "import", false,
			"Too many files are being imported at once.", nil)
	}
	directory := filepath.Join(b.tempRoot, "legacy-uploads")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", apperror.New("IMPORT_UPLOAD_FAILED", "storage", false,
			"The import could not be prepared.", err)
	}
	file, err := os.CreateTemp(directory, "upload-*.part")
	if err != nil {
		return "", apperror.New("IMPORT_UPLOAD_FAILED", "storage", false,
			"The import could not be prepared.", err)
	}
	uploadID, err := uploadIDs.New()
	if err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", apperror.New("IMPORT_UPLOAD_FAILED", "storage", false,
			"The import could not be prepared.", err)
	}
	b.uploads[uploadID] = &legacyUpload{
		legacyKey: legacyKey, mimeType: mimeType, declared: int64(declaredBytes),
		path: file.Name(), file: file,
	}
	return uploadID, nil
}

// AppendLegacyFile writes one base64 chunk and reports the bytes received so
// far, which lets the caller show progress without a second query.
func (b *LegacyUploadBinding) AppendLegacyFile(uploadID, chunkBase64 string) (float64, error) {
	if b == nil {
		return 0, bindingUnavailable()
	}
	decoded, err := base64.StdEncoding.DecodeString(chunkBase64)
	if err != nil {
		return 0, bindingInvalidInput()
	}
	if len(decoded) == 0 || len(decoded) > MaxLegacyChunkBytes {
		return 0, apperror.New("IMPORT_CHUNK_SIZE", "import", false,
			"The upload chunk was not a usable size.", nil)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	upload, ok := b.uploads[uploadID]
	if !ok {
		return 0, apperror.New("IMPORT_UPLOAD_UNKNOWN", "import", false,
			"That upload is no longer in progress.", nil)
	}
	if upload.total+int64(len(decoded)) > b.fileLimit() {
		b.discardLocked(uploadID)
		return 0, apperror.New("IMPORT_FILE_TOO_LARGE", "import", false,
			"That file is too large to import.", nil)
	}
	written, writeErr := upload.file.Write(decoded)
	if writeErr != nil {
		b.discardLocked(uploadID)
		return 0, apperror.New("IMPORT_UPLOAD_FAILED", "storage", false,
			"The file could not be received.", writeErr)
	}
	upload.total += int64(written)
	return float64(upload.total), nil
}

// FinishLegacyFile verifies the transfer and commits it to the FileStore.
//
// The declared size and the optional declared hash are checked before the bytes
// are committed, so a truncated or corrupted upload fails here rather than
// becoming a usable object the import would then reference.
func (b *LegacyUploadBinding) FinishLegacyFile(uploadID, declaredSHA256 string) (bool, error) {
	if b == nil || b.files == nil {
		return false, bindingUnavailable()
	}
	b.mu.Lock()
	upload, ok := b.uploads[uploadID]
	if !ok {
		b.mu.Unlock()
		return false, apperror.New("IMPORT_UPLOAD_UNKNOWN", "import", false,
			"That upload is no longer in progress.", nil)
	}
	legacyKey := upload.legacyKey
	mimeType := upload.mimeType
	declared := upload.declared
	total := upload.total
	path := upload.path
	// The handle must be closed before the bytes are read: Windows locks an open
	// file against a second reader, so leaving it open would make the read fail
	// on the platform this ships on.
	if upload.file != nil {
		_ = upload.file.Close()
		upload.file = nil
	}
	ctx := b.ctx
	delete(b.uploads, uploadID)
	b.mu.Unlock()

	// The temporary file is removed on every path out of this function, so a
	// failed finish cannot leak it (AC-LEGACY-003's temporary-file cleanup).
	defer func() {
		if path != "" {
			_ = os.Remove(path)
		}
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	if declared > 0 && total != declared {
		return false, apperror.New("IMPORT_SIZE_MISMATCH", "import", false,
			"The file did not arrive completely.", nil)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false, apperror.New("IMPORT_UPLOAD_FAILED", "storage", false,
			"The received file could not be read.", err)
	}
	if declaredSHA256 != "" {
		// The declared hash must be a content hash, so a caller cannot smuggle a
		// path or a malformed value into the comparison.
		if !isStorageKey(declaredSHA256) {
			return false, bindingInvalidInput()
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != declaredSHA256 {
			return false, apperror.New("IMPORT_HASH_MISMATCH", "import", false,
				"The file did not match its recorded checksum.", nil)
		}
	}
	hash, err := b.files.CommitLegacyFile(ctx, legacyKey, mimeType, content)
	if err != nil {
		return false, toImportError(err)
	}
	b.mu.Lock()
	// A session that uploaded more keys than the table should hold is bounded
	// here rather than growing without limit.
	if len(b.committed) >= MaxCommittedMedia {
		b.committed = map[string]committedMedia{}
	}
	b.committed[legacyKey] = committedMedia{hash: hash, mimeType: mimeType}
	b.mu.Unlock()
	return true, nil
}

// AbortLegacyFile abandons a transfer and removes its temporary file.
func (b *LegacyUploadBinding) AbortLegacyFile(uploadID string) error {
	if b == nil {
		return bindingUnavailable()
	}
	b.mu.Lock()
	if upload, ok := b.uploads[uploadID]; ok {
		// The handle is closed under the lock, then the file is removed outside
		// it: closing and unlinking must not overlap another goroutine's write.
		if upload.file != nil {
			_ = upload.file.Close()
			upload.file = nil
		}
		delete(b.uploads, uploadID)
		b.mu.Unlock()
		if upload.path != "" {
			_ = os.Remove(upload.path)
		}
		return nil
	}
	b.mu.Unlock()
	// Aborting an unknown or already-finished transfer is a no-op, so a caller's
	// cleanup path cannot fail on a race with completion.
	return nil
}

// discardLocked removes an in-flight upload and its temporary file. The caller
// holds the lock.
func (b *LegacyUploadBinding) discardLocked(uploadID string) {
	upload, ok := b.uploads[uploadID]
	if !ok {
		return
	}
	if upload.file != nil {
		_ = upload.file.Close()
		upload.file = nil
	}
	delete(b.uploads, uploadID)
	if upload.path != "" {
		_ = os.Remove(upload.path)
	}
}

// fileLimit reports the per-file ceiling in force.
func (b *LegacyUploadBinding) fileLimit() int64 {
	if b == nil || b.maxFileBytes <= 0 {
		return MaxLegacyFileBytes
	}
	return b.maxFileBytes
}

// PendingUploads reports how many transfers are in flight, so a caller can tell
// whether an abort left anything behind.
func (b *LegacyUploadBinding) PendingUploads() float64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return float64(len(b.uploads))
}

// legacyMediaReader resolves a legacy key to its committed bytes.
//
// It is a separate type, not the bound struct, so the reader is unreachable from
// the webview: Wails binds the methods of the structs it is given, and this one
// is only ever handed to the import service. Reading a legacy key is an internal
// step of the import, never a user action.
type legacyMediaReader struct {
	binding *LegacyUploadBinding
}

// ReadLegacyMedia returns the committed bytes for a legacy key.
//
// The key resolves through the committed table to a content hash, and the bytes
// come from the store. A key that was never uploaded reports as absent rather
// than failing, so the import records it as missing media (AC-LEGACY-001's
// missing-media case) instead of aborting a project over one absent file.
func (r *legacyMediaReader) ReadLegacyMedia(ctx context.Context, legacyKey string) ([]byte, string, bool, error) {
	b := r.binding
	if b == nil {
		return nil, "", false, nil
	}
	b.mu.Lock()
	record, ok := b.committed[legacyKey]
	store := b.store
	b.mu.Unlock()
	if !ok || store == nil {
		return nil, "", false, nil
	}
	stream, err := store.Open(ctx, record.hash)
	if err != nil {
		return nil, "", false, err
	}
	defer stream.Close()
	// The read is bounded so a corrupted store cannot stream without limit.
	content, err := io.ReadAll(io.LimitReader(stream, b.fileLimit()+1))
	if err != nil {
		return nil, "", false, err
	}
	if int64(len(content)) > b.fileLimit() {
		return nil, "", false, apperror.New("IMPORT_FILE_TOO_LARGE", "import", false,
			"That file is too large to import.", nil)
	}
	return content, record.mimeType, true, nil
}

// Ensure the reader satisfies the import service's media port.
//
// The assertion is on the reader, not on the bound struct: only the bound
// struct's exported methods reach the webview, and the reader must not.
var _ applegacy.MediaSource = (*legacyMediaReader)(nil)
