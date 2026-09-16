package jobs

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// fakeContentStore mimics FileStore's content-addressing and MIME detection
// without touching the filesystem. The test is single-goroutine except for the
// streaming download case, so the map is guarded by a mutex.
type fakeContentStore struct {
	mu        sync.Mutex
	stored    map[string][]byte
	mimeByKey map[string]string
	putErr    error
}

func newFakeContentStore() *fakeContentStore {
	return &fakeContentStore{stored: map[string][]byte{}, mimeByKey: map[string]string{}}
}

func (s *fakeContentStore) Put(_ context.Context, displayName string, body io.Reader) (files.Object, error) {
	if s.putErr != nil {
		return files.Object{}, s.putErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := io.ReadAll(body)
	if err != nil {
		return files.Object{}, err
	}
	key := "hash-" + displayName
	s.stored[key] = payload
	// The real detector, not a local reimplementation. A hand-written sniffer
	// here would drift from production and accept payloads the real FileStore
	// rejects — the exact class of false pass this pipeline was fixed for.
	mimeType := http.DetectContentType(payload)
	s.mimeByKey[key] = mimeType
	return files.Object{Hash: key, StorageKey: key, MIME: mimeType, Size: int64(len(payload))}, nil
}

func (s *fakeContentStore) Open(_ context.Context, storageKey string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.stored[storageKey]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

// fakeMetadataStore records object metadata the way FileRepository does, so the
// pipeline's ordering (metadata before reference) is exercised in tests.
type fakeMetadataStore struct {
	mu      sync.Mutex
	objects map[string]files.Object
	err     error
}

func newFakeMetadataStore() *fakeMetadataStore {
	return &fakeMetadataStore{objects: map[string]files.Object{}}
}

func (s *fakeMetadataStore) UpsertObject(_ context.Context, object files.Object) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[object.Hash] = object
	return nil
}

func (s *fakeMetadataStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.objects)
}

type recordingReferences struct {
	ownerTypes []string
	ownerIDs   []string
	hashes     []string
	err        error
}

func (r *recordingReferences) AddReference(_ context.Context, ownerType, ownerID, fileHash string) error {
	if r.err != nil {
		return r.err
	}
	r.ownerTypes = append(r.ownerTypes, ownerType)
	r.ownerIDs = append(r.ownerIDs, ownerID)
	r.hashes = append(r.hashes, fileHash)
	return nil
}

const pngHeader = "\x89PNG\r\n\x1a\n"

func TestCommitInlineStoresVerifiedImage(t *testing.T) {
	files := newFakeContentStore()
	references := &recordingReferences{}
	store := NewResultStore(files, newFakeMetadataStore(), references)
	payload := append([]byte(pngHeader), []byte("fake-png-body")...)
	encoded := base64.StdEncoding.EncodeToString(payload)

	committed, err := store.CommitInline(context.Background(), "job-1", "image", "image/png", encoded)
	if err != nil {
		t.Fatalf("CommitInline: %v", err)
	}
	if committed.MIME != "image/png" || committed.Size != int64(len(payload)) || committed.StorageKey == "" {
		t.Fatalf("unexpected commit: %+v", committed)
	}
	if len(references.hashes) != 1 || references.ownerTypes[0] != "job" || references.ownerIDs[0] != "job-1" {
		t.Fatalf("reference not recorded: %+v", references)
	}
}

func TestCommitInlineAcceptsDataURL(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	payload := append([]byte(pngHeader), []byte("body")...)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
	committed, err := store.CommitInline(context.Background(), "job-1", "image", "", dataURL)
	if err != nil {
		t.Fatalf("CommitInline(data URL): %v", err)
	}
	if committed.MIME != "image/png" {
		t.Fatalf("mime = %q", committed.MIME)
	}
}

func TestCommitInlineAcceptsUnpaddedBase64(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	payload := append([]byte(pngHeader), []byte("unpadded")...)
	encoded := base64.RawStdEncoding.EncodeToString(payload)
	if _, err := store.CommitInline(context.Background(), "job-1", "image", "image/png", encoded); err != nil {
		t.Fatalf("unpadded base64 rejected: %v", err)
	}
}

func TestCommitInlineRejectsMalformedData(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), nil)
	for _, payload := range []string{"", "not base64 at all!!!", "data:image/png;base64"} {
		if _, err := store.CommitInline(context.Background(), "job-1", "image", "image/png", payload); err == nil {
			t.Fatalf("malformed payload accepted: %q", payload)
		}
	}
}

// TestCommitRejectsDisallowedContentType is the "provider cannot place a
// non-media file in the asset library" rule.
func TestCommitRejectsDisallowedContentType(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	html := base64.StdEncoding.EncodeToString([]byte("<html><script>alert(1)</script>"))
	_, err := store.CommitInline(context.Background(), "job-1", "image", "image/png", html)
	if err == nil {
		t.Fatal("HTML payload accepted as an image")
	}
	jobErr, ok := job.AsJobError(err)
	if !ok || jobErr.Category != job.CategoryResponseInvalid {
		t.Fatalf("expected response_invalid, got %v", err)
	}
}

func TestMIMEAllowlistPerCapability(t *testing.T) {
	if !MIMEAllowed("image", "image/png") || !MIMEAllowed("image", "image/jpeg; charset=binary") {
		t.Error("valid image types rejected")
	}
	if MIMEAllowed("image", "text/html") || MIMEAllowed("image", "video/mp4") {
		t.Error("invalid image types accepted")
	}
	if !MIMEAllowed("video", "video/mp4") || MIMEAllowed("video", "image/png") {
		t.Error("video allowlist wrong")
	}
	if !MIMEAllowed("audio", "audio/mpeg") || MIMEAllowed("audio", "application/octet-stream") {
		t.Error("audio allowlist wrong")
	}
	// An unknown capability has no allowlist, so nothing passes.
	if MIMEAllowed("telepathy", "image/png") {
		t.Error("unknown capability allowed content")
	}
}

func TestCommitResultRejectsReferenceFailure(t *testing.T) {
	references := &recordingReferences{err: errors.New("disk full")}
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), references)
	payload := append([]byte(pngHeader), []byte("body")...)
	_, err := store.CommitResult(context.Background(), "job-1", "image", "image/png", bytes.NewReader(payload))
	if err == nil {
		t.Fatal("reference failure ignored")
	}
	if jobErr, ok := job.AsJobError(err); !ok || jobErr.Category != job.CategoryStorage {
		t.Fatalf("expected storage error, got %v", err)
	}
}

func TestCommitFailsClosedWithoutStore(t *testing.T) {
	store := NewResultStore(nil, nil, nil)
	if _, err := store.CommitInline(context.Background(), "job-1", "image", "image/png", "AAAA"); err == nil {
		t.Fatal("commit without a store succeeded")
	}
	var nilStore *ResultStore
	if _, err := nilStore.CommitInline(context.Background(), "job-1", "image", "image/png", "AAAA"); err == nil {
		t.Fatal("nil store commit succeeded")
	}
	if _, err := store.DownloadAndCommit(context.Background(), nil, "job-1", "image", "https://cdn.example.com/x", 0); err == nil {
		t.Fatal("download without a downloader succeeded")
	}
}

func TestDisplayNameFor(t *testing.T) {
	cases := map[string]string{
		"image/png":       "png",
		"image/jpeg":      "jpg",
		"video/mp4":       "mp4",
		"audio/mpeg":      "mp3",
		"application/xml": "bin",
	}
	for mimeType, extension := range cases {
		name := displayNameFor("image", mimeType)
		if !strings.HasSuffix(name, "."+extension) {
			t.Errorf("displayNameFor(%q) = %q, want .%s", mimeType, name, extension)
		}
	}
	// The name never carries a user-supplied path fragment.
	if strings.ContainsAny(displayNameFor("image", "image/png"), `/\:`) {
		t.Fatal("display name contains a path separator")
	}
}

// fakeDownloader feeds a fixed payload or error to the pipeline.
type fakeDownloader struct {
	payload []byte
	err     error
	// limitSeen records the cap the pipeline passed, so the test can prove the
	// store does not silently widen it.
	limitSeen int64
}

func (d *fakeDownloader) Download(_ context.Context, _ string, writer io.Writer, maxBytes int64) (int64, string, error) {
	d.limitSeen = maxBytes
	if d.err != nil {
		return 0, "", d.err
	}
	written, err := writer.Write(d.payload)
	return int64(written), "image/png", err
}

func TestDownloadAndCommitStoresStreamedResult(t *testing.T) {
	files := newFakeContentStore()
	references := &recordingReferences{}
	store := NewResultStore(files, newFakeMetadataStore(), references)
	downloader := &fakeDownloader{payload: append([]byte(pngHeader), []byte("downloaded")...)}

	committed, err := store.DownloadAndCommit(context.Background(), downloader, "job-9", "image", "https://cdn.example.com/a.png", 4096)
	if err != nil {
		t.Fatalf("DownloadAndCommit: %v", err)
	}
	if committed.MIME != "image/png" || committed.Size != int64(len(downloader.payload)) {
		t.Fatalf("unexpected commit: %+v", committed)
	}
	if downloader.limitSeen != 4096 {
		t.Fatalf("byte cap = %d, want 4096 (the caller's limit must not be widened)", downloader.limitSeen)
	}
	if len(references.hashes) != 1 || references.ownerIDs[0] != "job-9" {
		t.Fatalf("reference not recorded: %+v", references)
	}
}

// TestDownloadAndCommitNeverCommitsFailedDownload proves a partial download
// cannot become an asset.
func TestDownloadAndCommitNeverCommitsFailedDownload(t *testing.T) {
	files := newFakeContentStore()
	references := &recordingReferences{}
	store := NewResultStore(files, newFakeMetadataStore(), references)
	downloader := &fakeDownloader{err: errors.New("connection reset")}

	_, err := store.DownloadAndCommit(context.Background(), downloader, "job-9", "image", "https://cdn.example.com/a.png", 4096)
	if err == nil {
		t.Fatal("failed download reported success")
	}
	if len(references.hashes) != 0 {
		t.Fatalf("a failed download created a reference: %+v", references)
	}
}

func TestDownloadAndCommitRejectsDisallowedType(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	downloader := &fakeDownloader{payload: []byte("<html>nope</html>")}
	_, err := store.DownloadAndCommit(context.Background(), downloader, "job-9", "image", "https://cdn.example.com/a.png", 4096)
	if err == nil {
		t.Fatal("HTML download accepted as an image")
	}
}

// TestDownloadAndCommitReleasesDownloaderWhenStoreFails proves the streaming
// hand-off cannot deadlock when the store abandons the write.
//
// The pipe between the downloader and the store is unbuffered, so the downloader
// is sitting inside Write when the store gives up (a disk error, a cancelled
// context). Without closing the reader with the failure, that Write would block
// forever: the download goroutine and its HTTP response body would leak, and
// this function would then wait on downloadErr forever. The test fails by
// timing out if either side stops unblocking the other.
func TestDownloadAndCommitReleasesDownloaderWhenStoreFails(t *testing.T) {
	files := newFakeContentStore()
	files.putErr = errors.New("disk full")
	store := NewResultStore(files, newFakeMetadataStore(), &recordingReferences{})
	// A payload larger than the pipe buffer guarantee: the downloader must be
	// mid-Write when the store fails, which is the blocking case.
	downloader := &fakeDownloader{payload: append([]byte(pngHeader), make([]byte, 64*1024)...)}

	done := make(chan error, 1)
	go func() {
		_, err := store.DownloadAndCommit(context.Background(), downloader, "job-9", "image", "https://cdn.example.com/a.png", 1<<20)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a store failure reported a committed result")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DownloadAndCommit blocked after the store stopped reading")
	}
}

// TestCommitIsIdempotentOnRepeat proves re-committing the same content is safe:
// content addressing deduplicates the bytes and the reference insert is a
// no-op at the SQL layer.
func TestCommitIsIdempotentOnRepeat(t *testing.T) {
	files := newFakeContentStore()
	references := &recordingReferences{}
	store := NewResultStore(files, newFakeMetadataStore(), references)
	payload := append([]byte(pngHeader), []byte("same")...)
	encoded := base64.StdEncoding.EncodeToString(payload)

	first, err := store.CommitInline(context.Background(), "job-1", "image", "image/png", encoded)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CommitInline(context.Background(), "job-1", "image", "image/png", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if first.StorageKey != second.StorageKey || first.Hash != second.Hash {
		t.Fatalf("repeat commit produced a different object: %+v vs %+v", first, second)
	}
}

var _ appjobs.ResultWriter = (*ResultStore)(nil)
