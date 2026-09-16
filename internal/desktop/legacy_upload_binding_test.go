package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	applegacy "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/legacy"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// memoryFiles is an in-memory content store, so the upload channel's
// verification is tested without touching the real FileStore.
type memoryFiles struct {
	mu       sync.Mutex
	objects  map[string][]byte
	keyed    map[string]string
	calls    int
	lastKey  string
	lastMIME string
}

func newMemoryFiles() *memoryFiles {
	return &memoryFiles{objects: map[string][]byte{}, keyed: map[string]string{}}
}

func (f *memoryFiles) CommitLegacyFile(_ context.Context, legacyKey, mimeType string, content []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastKey = legacyKey
	f.lastMIME = mimeType
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	f.objects[hash] = content
	f.keyed[legacyKey] = hash
	return hash, nil
}

func (f *memoryFiles) Open(_ context.Context, storageKey string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	content, ok := f.objects[storageKey]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (f *memoryFiles) objectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.objects)
}

func (f *memoryFiles) commitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func uploadFixture(t *testing.T) (*LegacyUploadBinding, *memoryFiles, string) {
	t.Helper()
	root := t.TempDir()
	files := newMemoryFiles()
	binding := NewLegacyUploadBinding()
	ConfigureLegacyUpload(binding, root, files, files)
	AttachLegacyUpload(binding, context.Background())
	return binding, files, root
}

// uploadAll runs a complete transfer of one payload.
func uploadAll(t *testing.T, binding *LegacyUploadBinding, key, mime string, content []byte) string {
	t.Helper()
	uploadID, err := binding.BeginLegacyFile(key, mime, float64(len(content)))
	if err != nil {
		t.Fatalf("BeginLegacyFile: %v", err)
	}
	// Send in chunk-sized pieces, which is what the frontend does.
	for offset := 0; offset < len(content); offset += MaxLegacyChunkBytes {
		end := offset + MaxLegacyChunkBytes
		if end > len(content) {
			end = len(content)
		}
		chunk := base64.StdEncoding.EncodeToString(content[offset:end])
		if _, err := binding.AppendLegacyFile(uploadID, chunk); err != nil {
			t.Fatalf("AppendLegacyFile: %v", err)
		}
	}
	if len(content) == 0 {
		// An empty payload still needs one chunk, because the channel refuses a
		// zero-length append.
		if _, err := binding.AppendLegacyFile(uploadID, base64.StdEncoding.EncodeToString([]byte{0})); err != nil {
			t.Fatalf("AppendLegacyFile: %v", err)
		}
	}
	sum := sha256.Sum256(content)
	if _, err := binding.FinishLegacyFile(uploadID, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("FinishLegacyFile: %v", err)
	}
	return uploadID
}

// TestLegacyUploadRoundTrip proves a chunked transfer lands in the store and
// can be read back by the import service.
func TestLegacyUploadRoundTrip(t *testing.T) {
	binding, files, _ := uploadFixture(t)
	content := []byte("legacy media bytes that span more than one chunk")
	uploadAll(t, binding, "image:legacy-1", "image/png", content)

	if files.objectCount() != 1 {
		t.Fatalf("stored %d objects, want 1", files.objectCount())
	}
	if files.lastKey != "image:legacy-1" || files.lastMIME != "image/png" {
		t.Fatalf("the commit lost the key or type: %q %q", files.lastKey, files.lastMIME)
	}
	// The import service reads it back through the media port.
	source := LegacyUploadMediaSource(binding)
	read, mimeType, ok, err := source.ReadLegacyMedia(context.Background(), "image:legacy-1")
	if err != nil {
		t.Fatalf("ReadLegacyMedia: %v", err)
	}
	if !ok {
		t.Fatal("the uploaded key is not readable")
	}
	if !bytes.Equal(read, content) {
		t.Fatal("the bytes changed in transit")
	}
	if mimeType != "image/png" {
		t.Fatalf("mime = %q", mimeType)
	}
	// No temporary file is left behind.
	if pending := binding.PendingUploads(); pending != 0 {
		t.Fatalf("%v uploads are still pending", pending)
	}
}

// TestLegacyUploadAcceptsMultiMegabytePayload proves a payload larger than one
// chunk survives, which is the case the chunking exists for.
func TestLegacyUploadAcceptsMultiMegabytePayload(t *testing.T) {
	binding, _, _ := uploadFixture(t)
	// Just over two chunks.
	content := bytes.Repeat([]byte{0xAB, 0xCD}, (MaxLegacyChunkBytes/2)+1024)
	uploadAll(t, binding, "video:legacy-big", "video/mp4", content)

	source := LegacyUploadMediaSource(binding)
	read, _, ok, err := source.ReadLegacyMedia(context.Background(), "video:legacy-big")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the large upload is not readable")
	}
	if len(read) != len(content) {
		t.Fatalf("read %d bytes, want %d", len(read), len(content))
	}
	if !bytes.Equal(read, content) {
		t.Fatal("the large payload changed in transit")
	}
}

// TestLegacyUploadRejectsOversizeChunk proves one call cannot allocate an
// unbounded buffer.
func TestLegacyUploadRejectsOversizeChunk(t *testing.T) {
	binding, _, _ := uploadFixture(t)
	uploadID, err := binding.BeginLegacyFile("image:big-chunk", "image/png", 0)
	if err != nil {
		t.Fatal(err)
	}
	oversize := base64.StdEncoding.EncodeToString(make([]byte, MaxLegacyChunkBytes+1))
	_, err = binding.AppendLegacyFile(uploadID, oversize)
	if err == nil {
		t.Fatal("an oversize chunk was accepted")
	}
	if appErr, ok := err.(*apperror.Error); !ok || appErr.Code != "IMPORT_CHUNK_SIZE" {
		t.Fatalf("code = %v", err)
	}
	// The refused transfer is still open, so it is aborted to release its
	// temporary file before the test's directory is removed.
	if err := binding.AbortLegacyFile(uploadID); err != nil {
		t.Fatalf("AbortLegacyFile: %v", err)
	}
}

// TestLegacyUploadRejectsSizeMismatch proves a truncated transfer fails instead
// of committing partial bytes.
func TestLegacyUploadRejectsSizeMismatch(t *testing.T) {
	binding, files, _ := uploadFixture(t)
	uploadID, err := binding.BeginLegacyFile("image:truncated", "image/png", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binding.AppendLegacyFile(uploadID, base64.StdEncoding.EncodeToString([]byte("short"))); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.FinishLegacyFile(uploadID, ""); err == nil {
		t.Fatal("a truncated upload was committed")
	}
	if files.commitCount() != 0 {
		t.Fatal("a truncated upload reached the store")
	}
}

// TestLegacyUploadRejectsHashMismatch proves a payload whose bytes do not match
// its declared checksum is refused.
func TestLegacyUploadRejectsHashMismatch(t *testing.T) {
	binding, files, _ := uploadFixture(t)
	content := []byte("actual bytes")
	uploadID, err := binding.BeginLegacyFile("image:tampered", "image/png", float64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binding.AppendLegacyFile(uploadID, base64.StdEncoding.EncodeToString(content)); err != nil {
		t.Fatal(err)
	}
	wrong := strings.Repeat("a", 64)
	if _, err := binding.FinishLegacyFile(uploadID, wrong); err == nil {
		t.Fatal("a payload with the wrong checksum was committed")
	}
	if files.commitCount() != 0 {
		t.Fatal("a mismatched payload reached the store")
	}
	// A malformed hash is refused before the comparison, so a caller cannot
	// smuggle a non-hash value in.
	uploadID2, err := binding.BeginLegacyFile("image:malformed", "image/png", float64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binding.AppendLegacyFile(uploadID2, base64.StdEncoding.EncodeToString(content)); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.FinishLegacyFile(uploadID2, "../../etc/passwd"); err == nil {
		t.Fatal("a malformed hash was accepted")
	}
}

// TestLegacyUploadRejectsUnknownID proves a stale or forged upload id fails
// closed rather than acting on another transfer.
func TestLegacyUploadRejectsUnknownID(t *testing.T) {
	binding, _, _ := uploadFixture(t)
	if _, err := binding.AppendLegacyFile("00000000-0000-7000-8000-000000000000", "AAAA"); err == nil {
		t.Fatal("an unknown upload id was accepted")
	}
	if _, err := binding.FinishLegacyFile("00000000-0000-7000-8000-000000000000", ""); err == nil {
		t.Fatal("an unknown upload id was finished")
	}
	// Aborting an unknown id is a no-op, so a cleanup path cannot fail on a
	// race with completion.
	if err := binding.AbortLegacyFile("00000000-0000-7000-8000-000000000000"); err != nil {
		t.Fatalf("aborting an unknown upload returned an error: %v", err)
	}
}

// TestLegacyUploadAbortRemovesTemporaryFile proves an abandoned transfer leaves
// nothing behind, which is AC-LEGACY-003's "临时文件清理".
func TestLegacyUploadAbortRemovesTemporaryFile(t *testing.T) {
	binding, files, root := uploadFixture(t)
	uploadID, err := binding.BeginLegacyFile("image:abandoned", "image/png", 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binding.AppendLegacyFile(uploadID, base64.StdEncoding.EncodeToString([]byte("partial"))); err != nil {
		t.Fatal(err)
	}
	// A temporary file exists while the transfer is in flight.
	if countTemporaryFiles(t, root) == 0 {
		t.Fatal("no temporary file exists for an in-flight upload")
	}
	if err := binding.AbortLegacyFile(uploadID); err != nil {
		t.Fatalf("AbortLegacyFile: %v", err)
	}
	if countTemporaryFiles(t, root) != 0 {
		t.Fatal("an aborted upload left its temporary file behind")
	}
	if files.commitCount() != 0 {
		t.Fatal("an aborted upload reached the store")
	}
	if binding.PendingUploads() != 0 {
		t.Fatal("an aborted upload is still tracked")
	}
}

// TestLegacyUploadFinishRemovesTemporaryFile proves a completed transfer cleans
// up as well.
func TestLegacyUploadFinishRemovesTemporaryFile(t *testing.T) {
	binding, _, root := uploadFixture(t)
	uploadAll(t, binding, "image:done", "image/png", []byte("content"))
	if countTemporaryFiles(t, root) != 0 {
		t.Fatal("a finished upload left its temporary file behind")
	}
}

// TestLegacyUploadRejectsTooManyConcurrent proves the concurrency bound holds,
// so a caller cannot open transfers without limit.
func TestLegacyUploadRejectsTooManyConcurrent(t *testing.T) {
	binding, _, _ := uploadFixture(t)
	ids := make([]string, 0, MaxLegacyUploads)
	for index := 0; index < MaxLegacyUploads; index++ {
		id, err := binding.BeginLegacyFile("image:many-"+itoa(index), "image/png", 10)
		if err != nil {
			t.Fatalf("begin %d: %v", index, err)
		}
		ids = append(ids, id)
	}
	if _, err := binding.BeginLegacyFile("image:one-too-many", "image/png", 10); err == nil {
		t.Fatal("more concurrent uploads than the bound were accepted")
	}
	// Abort every transfer so its temporary file is released before the test's
	// directory is removed, which Windows would otherwise refuse.
	for _, id := range ids {
		if err := binding.AbortLegacyFile(id); err != nil {
			t.Fatalf("aborting %s: %v", id, err)
		}
	}
	if binding.PendingUploads() != 0 {
		t.Fatal("aborted uploads are still tracked")
	}
}

// TestLegacyUploadRejectsOversizeFile proves the per-file ceiling applies while
// receiving, not only at the start.
//
// The production ceiling is 2 GiB, so the limit is lowered for the test rather
// than building a multi-gigabyte payload.
func TestLegacyUploadRejectsOversizeFile(t *testing.T) {
	binding, _, _ := uploadFixture(t)
	binding.maxFileBytes = 3 * MaxLegacyChunkBytes
	uploadID, err := binding.BeginLegacyFile("image:greedy", "image/png", 0)
	if err != nil {
		t.Fatal(err)
	}
	chunk := base64.StdEncoding.EncodeToString(make([]byte, MaxLegacyChunkBytes))
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		if _, err := binding.AppendLegacyFile(uploadID, chunk); err != nil {
			lastErr = err
			break
		}
	}
	if lastErr == nil {
		t.Fatal("the per-file ceiling was never reached")
	}
	if appErr, ok := lastErr.(*apperror.Error); !ok || appErr.Code != "IMPORT_FILE_TOO_LARGE" {
		t.Fatalf("code = %v", lastErr)
	}
	// The refused transfer left nothing behind.
	if binding.PendingUploads() != 0 {
		t.Fatal("an over-limit upload is still tracked")
	}
}

// TestLegacyUploadUnknownKeyReportsAbsent proves a key that was never uploaded
// is reported as absent rather than failing, which is what lets the import
// record missing media instead of aborting a project.
func TestLegacyUploadUnknownKeyReportsAbsent(t *testing.T) {
	binding, _, _ := uploadFixture(t)
	source := LegacyUploadMediaSource(binding)
	_, _, ok, err := source.ReadLegacyMedia(context.Background(), "image:never-uploaded")
	if err != nil {
		t.Fatalf("a missing key returned an error: %v", err)
	}
	if ok {
		t.Fatal("a missing key reports as present")
	}
}

// TestLegacyUploadAcceptsOldStylePromptKey proves a legacy key with unusual but
// legal characters travels, since the legacy key is opaque in this channel.
func TestLegacyUploadAcceptsOldStylePromptKey(t *testing.T) {
	binding, _, _ := uploadFixture(t)
	// The legacy store uses prefixes with a colon and a nanoid body.
	key := "audio:V1StGXR8_Z5jdHi6B-myT"
	uploadAll(t, binding, key, "audio/wave", []byte("audio"))
	source := LegacyUploadMediaSource(binding)
	if _, _, ok, err := source.ReadLegacyMedia(context.Background(), key); err != nil || !ok {
		t.Fatalf("the key did not round trip: ok=%v err=%v", ok, err)
	}
	// A key that is empty or absurdly long is refused.
	if _, err := binding.BeginLegacyFile("", "image/png", 1); err == nil {
		t.Fatal("an empty key was accepted")
	}
	if _, err := binding.BeginLegacyFile(strings.Repeat("k", 201), "image/png", 1); err == nil {
		t.Fatal("an over-long key was accepted")
	}
}

// TestLegacyUploadSourceSatisfiesPort proves the binding is usable as the
// import service's media source.
func TestLegacyUploadSourceSatisfiesPort(t *testing.T) {
	binding, _, _ := uploadFixture(t)
	var source applegacy.MediaSource = LegacyUploadMediaSource(binding)
	if source == nil {
		t.Fatal("the media source is nil")
	}
}

// countTemporaryFiles counts the partial files under the upload directory.
func countTemporaryFiles(t *testing.T, root string) int {
	t.Helper()
	directory := filepath.Join(root, "legacy-uploads")
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".part") {
			count++
		}
	}
	return count
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
