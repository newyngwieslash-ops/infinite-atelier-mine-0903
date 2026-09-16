package jobs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// TestVideoDuplicateResponseDoesNotCreateSecondAsset covers AC-MEDIA-001's
// "duplicate response" item: a provider that reports the same finished result
// twice must not produce a second stored artifact.
func TestVideoDuplicateResponseDoesNotCreateSecondAsset(t *testing.T) {
	// A genuinely valid minimal MP4 ftyp box. The sniffer requires the declared
	// box size to be a multiple of four and within the buffer, plus a
	// four-byte-aligned "mp4" brand outside the major-brand slot; a truncated box
	// sniffs as application/octet-stream and the video allowlist rejects it, so a
	// test that used one would be asserting against a payload production refuses.
	payload := []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00,
		'm', 'p', '4', '1', 'i', 's', 'o', 'm',
	}
	videoAdapter := &stubVideoAdapter{
		remoteID: "remote-dup",
		// Both polls report completion, which is the duplicate-response case.
		polls: []appjobs.RemoteStatus{{Done: true}, {Done: true}},
		media: appjobs.MediaOutcome{Data: base64.StdEncoding.EncodeToString(payload), MIMEType: "video/mp4"},
	}
	content := newFakeContentStore()
	references := &recordingReferences{}
	store := NewResultStore(content, newFakeMetadataStore(), references)
	runner := NewRunner(stubAdapters{video: videoAdapter}, store, &fakeDownloader{}, 1<<20)

	ctx := context.Background()

	record := jobRecord(job.JobTypeVideoGeneration, map[string]any{"providerId": "prov-1", "model": "m", "prompt": "x"})
	record.RemoteJobID = "remote-dup"

	first, err := runner.Run(ctx, record)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	second, err := runner.Run(ctx, record)
	if err != nil {
		t.Fatalf("duplicate fetch: %v", err)
	}

	var firstMeta, secondMeta resultMetadata
	if err := json.Unmarshal([]byte(first.ResultJSON), &firstMeta); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(second.ResultJSON), &secondMeta); err != nil {
		t.Fatal(err)
	}
	if len(firstMeta.Files) != 1 || len(secondMeta.Files) != 1 {
		t.Fatalf("expected one file per response: %+v / %+v", firstMeta, secondMeta)
	}
	// Content addressing means the same bytes resolve to the same key, so the
	// duplicate cannot create a second distinct asset.
	if firstMeta.Files[0].StorageKey != secondMeta.Files[0].StorageKey {
		t.Fatalf("duplicate response produced a second asset: %s vs %s", firstMeta.Files[0].StorageKey, secondMeta.Files[0].StorageKey)
	}
	if len(content.stored) != 1 {
		t.Fatalf("duplicate response stored %d objects, want 1", len(content.stored))
	}
}

// TestVideoMockContractExercisesTheFullLifecycle proves the registered mock
// satisfies the video contract end to end, which is what WP-03 ships until the
// real adapter lands.
func TestVideoMockContractExercisesTheFullLifecycle(t *testing.T) {
	mock := providers.NewMockVideoAdapter()
	ctx := context.Background()

	remote, err := mock.Submit(ctx, appjobs.VideoRequest{JobID: "job-1", ProviderID: "prov-1", Prompt: "a clip", Seconds: 5})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if remote.ID == "" {
		t.Fatal("mock returned no remote ID")
	}
	// The mock needs a couple of polls before completion.
	status, err := mock.Poll(ctx, remote)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if status.Done {
		t.Fatal("mock finished on the first poll; the test cannot observe polling")
	}
	status, err = mock.Poll(ctx, remote)
	if err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if !status.Done {
		t.Fatal("mock never reported completion")
	}
	media, err := mock.Fetch(ctx, remote)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if media.MIMEType != "video/mp4" || media.Data == "" {
		t.Fatalf("media = %+v", media)
	}
	// The payload must satisfy the same allowlist the real pipeline enforces.
	decoded, _, err := decodeInline(media.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !videoMagicOK(decoded) {
		t.Fatal("mock payload does not carry MP4 magic bytes")
	}
	if err := mock.Cancel(ctx, remote); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// After cancellation the remote job reports a terminal failure rather than
	// pretending it completed.
	status, err = mock.Poll(ctx, remote)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Done || !status.Failed {
		t.Fatalf("cancelled mock job reported %+v", status)
	}
	// An unknown remote ID is a permanent failure, not a silent success.
	if _, err := mock.Poll(ctx, appjobs.RemoteJob{ID: "missing"}); err == nil {
		t.Fatal("unknown remote job accepted")
	}
}

// TestAudioMockContract proves the audio mock returns a payload the pipeline's
// allowlist accepts.
func TestAudioMockContract(t *testing.T) {
	mock := providers.NewMockAudioAdapter()
	outcome, err := mock.GenerateAudio(context.Background(), appjobs.AudioRequest{JobID: "job-1", ProviderID: "prov-1", Text: "hello"})
	if err != nil {
		t.Fatalf("GenerateAudio: %v", err)
	}
	// Go sniffs RIFF/WAVE as "audio/wave", which the audio allowlist accepts.
	if outcome.MIMEType != "audio/wave" || outcome.Data == "" {
		t.Fatalf("outcome = %+v", outcome)
	}
	decoded, _, err := decodeInline(outcome.Data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) < 12 || string(decoded[0:4]) != "RIFF" || string(decoded[8:12]) != "WAVE" {
		t.Fatal("mock payload does not carry WAV magic bytes")
	}
	// An empty request is rejected rather than producing an empty asset.
	if _, err := mock.GenerateAudio(context.Background(), appjobs.AudioRequest{ProviderID: "p"}); err == nil {
		t.Fatal("empty text accepted by the audio mock")
	}
}

// videoMagicOK reports whether the bytes look like an MP4 container header.
func videoMagicOK(payload []byte) bool {
	return len(payload) >= 12 && string(payload[4:8]) == "ftyp"
}
