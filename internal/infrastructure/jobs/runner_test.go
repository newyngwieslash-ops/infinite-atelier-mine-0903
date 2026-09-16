package jobs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// stubImageAdapter returns a scripted outcome or error.
type stubImageAdapter struct {
	outcome appjobs.ImageOutcome
	err     error
	calls   int
	last    appjobs.ImageRequest
}

func (s *stubImageAdapter) Generate(_ context.Context, request appjobs.ImageRequest) (appjobs.ImageOutcome, error) {
	s.calls++
	s.last = request
	return s.outcome, s.err
}

// stubVideoAdapter walks the async contract with a scripted poll sequence.
type stubVideoAdapter struct {
	submitCalls int
	pollCalls   int
	cancelCalls int
	submitErr   error
	media       appjobs.MediaOutcome
	mediaErr    error
	polls       []appjobs.RemoteStatus
	remoteID    string
}

func (s *stubVideoAdapter) Submit(_ context.Context, _ appjobs.VideoRequest) (appjobs.RemoteJob, error) {
	s.submitCalls++
	if s.submitErr != nil {
		return appjobs.RemoteJob{}, s.submitErr
	}
	id := s.remoteID
	if id == "" {
		id = "remote-1"
	}
	return appjobs.RemoteJob{ID: id}, nil
}

func (s *stubVideoAdapter) Poll(context.Context, appjobs.RemoteJob) (appjobs.RemoteStatus, error) {
	s.pollCalls++
	if len(s.polls) == 0 {
		return appjobs.RemoteStatus{Done: true}, nil
	}
	status := s.polls[0]
	if len(s.polls) > 1 {
		s.polls = s.polls[1:]
	}
	return status, nil
}

func (s *stubVideoAdapter) Fetch(context.Context, appjobs.RemoteJob) (appjobs.MediaOutcome, error) {
	return s.media, s.mediaErr
}

func (s *stubVideoAdapter) Cancel(context.Context, appjobs.RemoteJob) error {
	s.cancelCalls++
	return nil
}

type stubAudioAdapter struct {
	outcome appjobs.AudioOutcome
	err     error
}

func (s *stubAudioAdapter) GenerateAudio(context.Context, appjobs.AudioRequest) (appjobs.AudioOutcome, error) {
	return s.outcome, s.err
}

// stubAdapters satisfies the runner's AdapterSource.
type stubAdapters struct {
	image appjobs.ImagePort
	video appjobs.VideoPort
	audio appjobs.AudioPort
}

func (s stubAdapters) ImagePortFor(context.Context, string) (appjobs.ImagePort, error) {
	if s.image == nil {
		return nil, job.FailedJobError(job.CategoryUnsupported, "no image adapter")
	}
	return s.image, nil
}

func (s stubAdapters) VideoPortFor(context.Context, string) (appjobs.VideoPort, error) {
	if s.video == nil {
		return nil, job.FailedJobError(job.CategoryUnsupported, "no video adapter")
	}
	return s.video, nil
}

func (s stubAdapters) AudioPortFor(context.Context, string) (appjobs.AudioPort, error) {
	if s.audio == nil {
		return nil, job.FailedJobError(job.CategoryUnsupported, "no audio adapter")
	}
	return s.audio, nil
}

func runnerFor(t *testing.T, adapters stubAdapters, store *ResultStore) *Runner {
	t.Helper()
	return NewRunner(adapters, store, &fakeDownloader{payload: append([]byte("\x89PNG\r\n\x1a\n"), []byte("downloaded")...)}, 1<<20)
}

func jobRecord(jobType job.JobType, input any) job.Job {
	encoded, _ := json.Marshal(input)
	return job.Job{
		ID:               "job-1",
		JobType:          jobType,
		ProviderConfigID: "prov-1",
		InputJSON:        string(encoded),
		Status:           job.StatusRunning,
		MaxAttempts:      3,
	}
}

func TestRunnerImageInlineSuccess(t *testing.T) {
	adapter := &stubImageAdapter{outcome: appjobs.ImageOutcome{
		Results: []appjobs.ImageResult{{Data: base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nbody")), MIMEType: "image/png"}},
	}}
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	runner := runnerFor(t, stubAdapters{image: adapter}, store)

	outcome, err := runner.Run(context.Background(), jobRecord(job.JobTypeImageGeneration, map[string]any{
		"providerId": "prov-1", "model": "gpt-image-1", "prompt": "a cat", "count": 1,
	}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != job.StatusSucceeded {
		t.Fatalf("status = %q", outcome.Status)
	}
	// The stored metadata names files, never bytes.
	var metadata resultMetadata
	if err := json.Unmarshal([]byte(outcome.ResultJSON), &metadata); err != nil {
		t.Fatalf("result json: %v", err)
	}
	if len(metadata.Files) != 1 || metadata.Files[0].StorageKey == "" || metadata.Files[0].Size == 0 {
		t.Fatalf("metadata = %+v", metadata)
	}
	if strings.Contains(outcome.ResultJSON, base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nbody"))) {
		t.Fatal("result json embedded the raw payload")
	}
}

// TestRunnerImageRemoteURLIsDownloaded covers the two-phase fetch: the first
// pass records that the provider produced results and stops, and a later pass
// downloads them without calling the provider again.
func TestRunnerImageRemoteURLIsDownloaded(t *testing.T) {
	adapter := &stubImageAdapter{outcome: appjobs.ImageOutcome{RemoteURLs: []string{"https://cdn.example.com/a.png"}}}
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	runner := runnerFor(t, stubAdapters{image: adapter}, store)
	record := jobRecord(job.JobTypeImageGeneration, map[string]any{
		"providerId": "prov-1", "model": "m", "prompt": "p", "count": 1,
	})

	first, err := runner.Run(context.Background(), record)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if first.Status != job.StatusDownloading {
		t.Fatalf("first pass status = %q, want downloading", first.Status)
	}
	if adapter.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", adapter.calls)
	}

	// The worker persists the phase; the next pass resumes the fetch.
	record.ResultJSON = first.ResultJSON
	second, err := runner.Run(context.Background(), record)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if second.Status != job.StatusSucceeded {
		t.Fatalf("second pass status = %q, want succeeded", second.Status)
	}
	if adapter.calls != 1 {
		t.Fatalf("the download pass called the provider again (calls = %d)", adapter.calls)
	}
	var metadata resultMetadata
	_ = json.Unmarshal([]byte(second.ResultJSON), &metadata)
	if metadata.Mode != "downloaded" || len(metadata.Files) != 1 {
		t.Fatalf("metadata = %+v", metadata)
	}
}

// TestRunnerImagePendingDownloadSurvivesProviderChange proves the pending
// marker is consumed from the persisted JSON, so a job whose provider became
// unavailable still finishes its download rather than erroring out.
func TestRunnerImagePendingDownloadSurvivesProviderChange(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	// No image adapter at all: the download path must not need one.
	runner := runnerFor(t, stubAdapters{}, store)
	record := jobRecord(job.JobTypeImageGeneration, map[string]any{"providerId": "prov-1", "model": "m", "prompt": "p"})
	record.ResultJSON = `{"mode":"pending_download","urls":["https://cdn.example.com/a.png"]}`

	outcome, err := runner.Run(context.Background(), record)
	if err != nil {
		t.Fatalf("pending download failed without an adapter: %v", err)
	}
	if outcome.Status != job.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", outcome.Status)
	}
}

func TestRunnerImagePropagatesDownloadFailure(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	// A downloader that fails must prevent a success outcome, and the failure
	// must be attributable to the download rather than the provider call.
	runner := NewRunner(stubAdapters{}, store, &fakeDownloader{err: errors.New("connection reset")}, 1<<20)
	record := jobRecord(job.JobTypeImageGeneration, map[string]any{"providerId": "prov-1", "model": "m", "prompt": "p"})
	record.ResultJSON = `{"mode":"pending_download","urls":["https://cdn.example.com/a.png"]}`

	if _, err := runner.Run(context.Background(), record); err == nil {
		t.Fatal("failed download produced a success outcome")
	}
}

func TestRunnerImageRejectsMalformedInput(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	runner := runnerFor(t, stubAdapters{image: &stubImageAdapter{}}, store)
	record := jobRecord(job.JobTypeImageGeneration, map[string]any{})
	record.InputJSON = "{not json"
	if _, err := runner.Run(context.Background(), record); err == nil {
		t.Fatal("malformed input accepted")
	}
	// A job with no provider cannot run.
	record = jobRecord(job.JobTypeImageGeneration, map[string]any{"model": "m", "prompt": "p"})
	record.ProviderConfigID = ""
	if _, err := runner.Run(context.Background(), record); err == nil {
		t.Fatal("job without a provider accepted")
	}
}

func TestRunnerImageFailsClosedWithoutAdapter(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	runner := runnerFor(t, stubAdapters{}, store)
	if _, err := runner.Run(context.Background(), jobRecord(job.JobTypeImageGeneration, map[string]any{"providerId": "p", "model": "m", "prompt": "x"})); err == nil {
		t.Fatal("missing image adapter accepted")
	}
}

// TestRunnerVideoFullAsyncFlow covers AC-MEDIA-001's submit -> poll -> fetch ->
// validate -> asset version sequence.
func TestRunnerVideoFullAsyncFlow(t *testing.T) {
	// A valid minimal MP4 ftyp box; the four-byte-aligned "mp41" brand at offset
	// 16 is what makes Go's sniffer report video/mp4 (it skips the major-brand
	// slot and the minor version).
	mp4 := []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00,
		'm', 'p', '4', '1', 'i', 's', 'o', 'm',
	}
	video := &stubVideoAdapter{
		media: appjobs.MediaOutcome{Data: base64.StdEncoding.EncodeToString(mp4), MIMEType: "video/mp4"},
		polls: []appjobs.RemoteStatus{{Done: false}, {Done: true}},
	}
	references := &recordingReferences{}
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), references)
	runner := runnerFor(t, stubAdapters{video: video}, store)

	// First pass submits and parks.
	record := jobRecord(job.JobTypeVideoGeneration, map[string]any{
		"providerId": "prov-1", "model": "video-1", "prompt": "a clip", "seconds": 5,
	})
	first, err := runner.Run(context.Background(), record)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if first.Status != job.StatusWaitingRemote || first.RemoteJobID == "" {
		t.Fatalf("submit outcome = %+v", first)
	}
	if video.submitCalls != 1 {
		t.Fatalf("submit calls = %d", video.submitCalls)
	}

	// Second pass polls (not done yet).
	record.RemoteJobID = first.RemoteJobID
	second, err := runner.Run(context.Background(), record)
	if err != nil {
		t.Fatalf("poll 1: %v", err)
	}
	if second.Status != job.StatusWaitingRemote {
		t.Fatalf("poll 1 outcome = %+v", second)
	}
	if video.submitCalls != 1 {
		t.Fatalf("a poll re-submitted the job (submit calls = %d)", video.submitCalls)
	}

	// Third pass completes and fetches.
	third, err := runner.Run(context.Background(), record)
	if err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	if third.Status != job.StatusSucceeded {
		t.Fatalf("final outcome = %+v", third)
	}
	if video.submitCalls != 1 {
		t.Fatalf("submit calls = %d, want exactly 1", video.submitCalls)
	}
	var metadata resultMetadata
	_ = json.Unmarshal([]byte(third.ResultJSON), &metadata)
	if len(metadata.Files) != 1 || metadata.Files[0].MIME != "video/mp4" {
		t.Fatalf("metadata = %+v", metadata)
	}
	if len(references.hashes) != 1 || references.ownerTypes[0] != "job" {
		t.Fatalf("reference not recorded: %+v", references)
	}
}

// TestRunnerVideoRemoteOnlyOutcome covers the documented remote_only terminal
// state: the provider acknowledged a result that is not stored locally.
func TestRunnerVideoRemoteOnlyOutcome(t *testing.T) {
	video := &stubVideoAdapter{media: appjobs.MediaOutcome{}, polls: []appjobs.RemoteStatus{{Done: true}}}
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	runner := runnerFor(t, stubAdapters{video: video}, store)

	record := jobRecord(job.JobTypeVideoGeneration, map[string]any{"providerId": "p", "model": "m", "prompt": "x"})
	record.RemoteJobID = "remote-1"
	outcome, err := runner.Run(context.Background(), record)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != job.StatusRemoteOnly || !outcome.RemoteOnly {
		t.Fatalf("outcome = %+v, want remote_only", outcome)
	}
}

func TestRunnerVideoProviderFailureIsTerminal(t *testing.T) {
	video := &stubVideoAdapter{polls: []appjobs.RemoteStatus{{Done: true, Failed: true, Message: "provider gave up"}}}
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	runner := runnerFor(t, stubAdapters{video: video}, store)

	record := jobRecord(job.JobTypeVideoGeneration, map[string]any{"providerId": "p", "model": "m", "prompt": "x"})
	record.RemoteJobID = "remote-1"
	_, err := runner.Run(context.Background(), record)
	if err == nil {
		t.Fatal("provider failure produced a success")
	}
	if jobErr, ok := job.AsJobError(err); !ok || jobErr.Category != job.CategoryRemotePermanent {
		t.Fatalf("expected remote_permanent, got %v", err)
	}
}

func TestRunnerAudioSuccess(t *testing.T) {
	audio := &stubAudioAdapter{outcome: appjobs.AudioOutcome{
		Data:     base64.StdEncoding.EncodeToString([]byte("RIFF\x00\x00\x00\x00WAVEfmt ")),
		MIMEType: "audio/wav",
	}}
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	runner := runnerFor(t, stubAdapters{audio: audio}, store)

	outcome, err := runner.Run(context.Background(), jobRecord(job.JobTypeAudioGeneration, map[string]any{
		"providerId": "p", "model": "tts-1", "text": "hello", "voice": "alloy",
	}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != job.StatusSucceeded {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestRunnerRejectsUnsupportedJobType(t *testing.T) {
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	runner := runnerFor(t, stubAdapters{}, store)
	if _, err := runner.Run(context.Background(), jobRecord(job.JobTypeAssetDownload, map[string]any{})); err == nil {
		t.Fatal("unsupported job type accepted")
	}
}

func TestRunnerFailsClosedWithoutAdapters(t *testing.T) {
	var runner *Runner
	if _, err := runner.Run(context.Background(), jobRecord(job.JobTypeImageGeneration, map[string]any{})); err == nil {
		t.Fatal("nil runner executed a job")
	}
}

// TestRunnerNeverCommitsDisallowedContent proves the content allowlist applies
// to the runner's commit path, not just the store.
func TestRunnerNeverCommitsDisallowedContent(t *testing.T) {
	adapter := &stubImageAdapter{outcome: appjobs.ImageOutcome{
		Results: []appjobs.ImageResult{{Data: base64.StdEncoding.EncodeToString([]byte("<html>nope</html>")), MIMEType: "text/html"}},
	}}
	references := &recordingReferences{}
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), references)
	runner := runnerFor(t, stubAdapters{image: adapter}, store)

	_, err := runner.Run(context.Background(), jobRecord(job.JobTypeImageGeneration, map[string]any{
		"providerId": "p", "model": "m", "prompt": "x", "count": 1,
	}))
	if err == nil {
		t.Fatal("HTML result accepted as an image")
	}
	if len(references.hashes) != 0 {
		t.Fatalf("a rejected result created a reference: %+v", references)
	}
}

// Ensure the fake downloader satisfies the runner's port.
var _ DownloaderPort = (*fakeDownloader)(nil)
