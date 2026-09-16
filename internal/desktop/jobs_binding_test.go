package desktop

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// stubResultReader serves fixed content so the read path can be tested without
// a FileStore.
type stubResultReader struct {
	payload []byte
	err     error
	// opened records the key the binding asked for, so the key-shape gate can
	// be asserted.
	opened string
}

func (r *stubResultReader) Open(_ context.Context, storageKey string) (io.ReadCloser, error) {
	r.opened = storageKey
	if r.err != nil {
		return nil, r.err
	}
	return io.NopCloser(bytes.NewReader(r.payload)), nil
}

func TestReadResultFileReturnsDataURL(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("body")...)
	reader := &stubResultReader{payload: png}
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), appjobs.NewService(appjobs.Options{}))
	AttachJobResultReader(binding, reader)

	key := strings.Repeat("a", 64)
	content, err := binding.ReadResultFile(key)
	if err != nil {
		t.Fatalf("ReadResultFile: %v", err)
	}
	if content.MIME != "image/png" {
		t.Fatalf("mime = %q", content.MIME)
	}
	if !strings.HasPrefix(content.DataURL, "data:image/png;base64,") {
		t.Fatalf("data URL = %q", content.DataURL[:minIntLen(len(content.DataURL), 40)])
	}
	if content.Size != int64(len(png)) {
		t.Fatalf("size = %d, want %d", content.Size, len(png))
	}
	// The payload round-trips.
	encoded := strings.TrimPrefix(content.DataURL, "data:image/png;base64,")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, png) {
		t.Fatal("payload corrupted in the data URL")
	}
	if reader.opened != key {
		t.Fatalf("opened %q, want %q", reader.opened, key)
	}
}

// TestReadResultFileRejectsForgedKeys proves the binding refuses anything that
// is not a content-addressed hash, so it cannot become a path reader.
func TestReadResultFileRejectsForgedKeys(t *testing.T) {
	reader := &stubResultReader{payload: []byte("x")}
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), appjobs.NewService(appjobs.Options{}))
	AttachJobResultReader(binding, reader)

	forged := []string{
		"",
		"../etc/passwd",
		"/etc/passwd",
		"C:\\Windows\\system32\\config",
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("Z", 64),           // uppercase is not the stored form
		strings.Repeat("g", 64),           // outside the hex alphabet
		"file:///etc/passwd",              // scheme smuggling
		strings.Repeat("a", 32) + "/../x", // traversal inside a hash-shaped string
	}
	for _, key := range forged {
		if _, err := binding.ReadResultFile(key); err == nil {
			t.Fatalf("forged key %q accepted", key)
		}
	}
	if reader.opened != "" {
		t.Fatalf("a forged key reached the reader: %q", reader.opened)
	}
}

func TestReadResultFileFailsClosedWhenUnattached(t *testing.T) {
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), appjobs.NewService(appjobs.Options{}))
	// No reader attached.
	if _, err := binding.ReadResultFile(strings.Repeat("a", 64)); err == nil {
		t.Fatal("read succeeded without a result reader")
	}
	// No service attached.
	empty := &JobsBinding{}
	if _, err := empty.ReadResultFile(strings.Repeat("a", 64)); err == nil {
		t.Fatal("read succeeded without a service")
	}
}

func TestReadResultFileRejectsOversizedContent(t *testing.T) {
	// The cap is on the read, so a payload above it must fail rather than
	// materialise in the webview.
	reader := &stubResultReader{payload: bytes.Repeat([]byte("A"), maxResultFileBytes+1)}
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), appjobs.NewService(appjobs.Options{}))
	AttachJobResultReader(binding, reader)
	if _, err := binding.ReadResultFile(strings.Repeat("b", 64)); err == nil {
		t.Fatal("oversized result accepted")
	}
}

func TestReadResultFileMapsReaderFailureSafely(t *testing.T) {
	reader := &stubResultReader{err: errors.New("open C:\\Users\\Alice\\secret.db: denied")}
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), appjobs.NewService(appjobs.Options{}))
	AttachJobResultReader(binding, reader)
	_, err := binding.ReadResultFile(strings.Repeat("c", 64))
	if err == nil {
		t.Fatal("reader failure ignored")
	}
	// The error must not echo the underlying path.
	if strings.Contains(err.Error(), "secret.db") || strings.Contains(err.Error(), "Alice") {
		t.Fatalf("error leaked a local path: %v", err)
	}
}

func TestDetectResultMIME(t *testing.T) {
	cases := map[string][]byte{
		"image/png":  {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
		"image/jpeg": {0xFF, 0xD8, 0xFF, 0xE0},
		"image/gif":  []byte("GIF89a"),
		"video/mp4":  []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'},
		"audio/mpeg": []byte("ID3\x04\x00"),
	}
	for want, payload := range cases {
		if got := detectResultMIME(payload); got != want {
			t.Errorf("detectResultMIME(%v) = %q, want %q", payload, got, want)
		}
	}
	// WAV needs the RIFF/WAVE pair.
	wav := []byte{'R', 'I', 'F', 'F', 36, 0, 0, 0, 'W', 'A', 'V', 'E'}
	if got := detectResultMIME(wav); got != "audio/wav" {
		t.Errorf("detectResultMIME(wav) = %q", got)
	}
	// WebP needs the WEBP tag, not just RIFF.
	webp := []byte{'R', 'I', 'F', 'F', 36, 0, 0, 0, 'W', 'E', 'B', 'P'}
	if got := detectResultMIME(webp); got != "image/webp" {
		t.Errorf("detectResultMIME(webp) = %q", got)
	}
	// An unknown payload is labelled as opaque rather than guessed.
	if got := detectResultMIME([]byte("random bytes")); got != "application/octet-stream" {
		t.Errorf("unknown payload = %q", got)
	}
	// A short payload must not panic on slicing.
	_ = detectResultMIME([]byte{1, 2})
	_ = detectResultMIME(nil)
}

// TestJobsBindingReplayPreservesJobType proves an idempotent replay reports the
// job's real type rather than normalising it to a generation.
func TestJobsBindingReplayPreservesJobType(t *testing.T) {
	repository := &stubJobRepository{records: map[string]job.Job{}}
	service := appjobs.NewService(appjobs.Options{Repository: repository, IDs: &counterIDsForTest{}})
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), service)

	request := SubmitImageJobRequest{
		ProviderID: "prov-1",
		Model:      "gpt-image-1",
		Prompt:     "a cat",
		EntityID:   "node-1",
		References: []string{"data:image/png;base64,AAAA"},
	}
	first, err := binding.SubmitImageJob(request)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if first.JobType != string(job.JobTypeImageEdit) {
		t.Fatalf("job type = %q, want image_edit", first.JobType)
	}
	// The same request is a replay and must keep the edit type.
	second, err := binding.SubmitImageJob(request)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replay created a new job: %s vs %s", second.ID, first.ID)
	}
	if second.JobType != string(job.JobTypeImageEdit) {
		t.Fatalf("replayed job type = %q, want image_edit", second.JobType)
	}
}

// TestJobsBindingDistinguishesEntitiesForTheSamePrompt proves two nodes with an
// identical prompt produce two jobs rather than collapsing into one.
func TestJobsBindingDistinguishesEntitiesForTheSamePrompt(t *testing.T) {
	repository := &stubJobRepository{records: map[string]job.Job{}}
	service := appjobs.NewService(appjobs.Options{Repository: repository, IDs: &counterIDsForTest{}})
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), service)

	base := SubmitImageJobRequest{ProviderID: "prov-1", Model: "m", Prompt: "same prompt"}
	first, err := binding.SubmitImageJob(base)
	if err != nil {
		t.Fatal(err)
	}
	second := base
	second.EntityID = "node-2"
	other, err := binding.SubmitImageJob(second)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == first.ID {
		t.Fatal("the same prompt on two nodes collapsed into one job")
	}
}

// TestJobsBindingBatchCaps proves a single command cannot enqueue or act on an
// unbounded batch (SECURITY: bulk generation needs a limit).
func TestJobsBindingBatchCaps(t *testing.T) {
	service := appjobs.NewService(appjobs.Options{Repository: &stubJobRepository{records: map[string]job.Job{}}, IDs: &counterIDsForTest{}})
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), service)

	oversizedCount := maxImageBatch + 1
	_, err := binding.SubmitImageJob(SubmitImageJobRequest{ProviderID: "p", Model: "m", Prompt: "x", Count: oversizedCount})
	if err == nil {
		t.Fatal("oversized image batch accepted")
	}
	ids := make([]string, maxBatchAction+1)
	for index := range ids {
		ids[index] = "job-x"
	}
	if _, err := binding.CancelJobs(ids); err == nil {
		t.Fatal("oversized cancel batch accepted")
	}
	if _, err := binding.RetryFailedJobs(ids); err == nil {
		t.Fatal("oversized retry batch accepted")
	}
	// Empty batches are a no-op, not an error.
	if count, err := binding.CancelJobs(nil); err != nil || count != 0 {
		t.Fatalf("empty cancel = %d, %v", count, err)
	}
}

// TestJobsBindingRejectsUnknownStatusFilter proves the list filter validates
// its input rather than passing arbitrary strings to the query.
func TestJobsBindingRejectsUnknownStatusFilter(t *testing.T) {
	service := appjobs.NewService(appjobs.Options{Repository: &stubJobRepository{records: map[string]job.Job{}}, IDs: &counterIDsForTest{}})
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), service)
	if _, err := binding.ListJobs(ListJobsRequest{Statuses: []string{"awaiting_remote"}}); err == nil {
		t.Fatal("undocumented status filter accepted")
	}
	if _, err := binding.ListJobs(ListJobsRequest{Statuses: []string{"queued"}}); err != nil {
		t.Fatalf("valid status filter rejected: %v", err)
	}
}

// stubJobRepository is a minimal in-memory Repository for binding tests. It
// mirrors the real revision guard so replay and conflict behaviour match.
type stubJobRepository struct {
	records map[string]job.Job
}

func (r *stubJobRepository) Insert(_ context.Context, record job.Job) error {
	for _, existing := range r.records {
		if existing.ProjectID == record.ProjectID && existing.IdempotencyKey == record.IdempotencyKey {
			return job.DuplicateJobError()
		}
	}
	r.records[record.ID] = record
	return nil
}

func (r *stubJobRepository) Get(_ context.Context, id string) (job.Job, error) {
	record, ok := r.records[id]
	if !ok {
		return job.Job{}, job.JobNotFoundError(id)
	}
	return record, nil
}

func (r *stubJobRepository) GetByIdempotencyKey(_ context.Context, projectID, key string) (job.Job, error) {
	for _, record := range r.records {
		if record.ProjectID == projectID && record.IdempotencyKey == key {
			return record, nil
		}
	}
	return job.Job{}, job.JobNotFoundError(key)
}

func (r *stubJobRepository) List(_ context.Context, filter appjobs.ListFilter) ([]job.Job, error) {
	var out []job.Job
	for _, record := range r.records {
		if filter.ActiveOnly && !record.IsActive() {
			continue
		}
		if len(filter.Statuses) > 0 {
			matched := false
			for _, status := range filter.Statuses {
				if record.Status == status {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *stubJobRepository) Claim(_ context.Context, id, workerID string, leaseUntil time.Time, now time.Time) (job.Job, error) {
	record, ok := r.records[id]
	if !ok {
		return job.Job{}, job.JobNotFoundError(id)
	}
	if record.Status != job.StatusQueued || record.LeaseHeld(now) || record.CancelRequested {
		return job.Job{}, job.DuplicateJobError()
	}
	record.Status = job.StatusRunning
	record.LeaseOwner = workerID
	record.LeaseExpiresAt = leaseUntil
	record.Revision++
	r.records[id] = record
	return record, nil
}

func (r *stubJobRepository) Update(_ context.Context, record job.Job, expectedRevision int64) error {
	current, ok := r.records[record.ID]
	if !ok {
		return job.JobNotFoundError(record.ID)
	}
	if current.Revision != expectedRevision {
		return job.DuplicateJobError()
	}
	record.Revision = current.Revision + 1
	r.records[record.ID] = record
	return nil
}

func (r *stubJobRepository) RequestCancel(_ context.Context, id string, now time.Time) (job.Job, error) {
	record, ok := r.records[id]
	if !ok {
		return job.Job{}, job.JobNotFoundError(id)
	}
	record.CancelRequested = true
	record.UpdatedAt = now
	record.Revision++
	r.records[id] = record
	return record, nil
}

func (r *stubJobRepository) ClaimableCandidates(context.Context, time.Time, int) ([]job.Job, error) {
	return nil, nil
}

func (r *stubJobRepository) ActiveCounts(context.Context) (map[job.Status]int, error) {
	counts := map[job.Status]int{}
	for _, record := range r.records {
		counts[record.Status]++
	}
	return counts, nil
}

func (r *stubJobRepository) StartAttempt(context.Context, job.Attempt) error  { return nil }
func (r *stubJobRepository) FinishAttempt(context.Context, job.Attempt) error { return nil }
func (r *stubJobRepository) ListAttempts(context.Context, string) ([]job.Attempt, error) {
	return nil, nil
}
func (r *stubJobRepository) AddDependency(context.Context, job.Dependency) error { return nil }
func (r *stubJobRepository) ListDependencies(context.Context, string) ([]job.Dependency, error) {
	return nil, nil
}

// counterIDsForTest produces deterministic identifiers.
type counterIDsForTest struct {
	mu    sync.Mutex
	count int
}

func (g *counterIDsForTest) NewID(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.count++
	return prefix + "-test-" + strconv.Itoa(g.count)
}

func minIntLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
