package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// audio_provider_e2e_test.go drives the REAL speech adapter through the REAL runner and the REAL result
// store, against a fake VENDOR and nothing else.
//
// # Why this is the test the package needed
//
// The adapter's own suite proves it speaks the protocol. That is a different claim from "the pipeline
// accepts what it produces", made by different code — and the difference is not academic here: the result
// store's audio allowlist decides on the type the SNIFFER produced from the bytes, and a WAV header is
// reported as `audio/wave` while a provider might claim `audio/wav`. An adapter that reported its own
// spelling would pass its own tests and be refused here, with the failure landing on a user as a job that
// produces nothing.
//
// It also proves the MIME this adapter passes through is one the store accepts — which is why the
// fallback in `mimeOrMP3` is `audio/mpeg` and not something more natural-sounding.

// audioVendor plays a speech endpoint whose response the test sets.
type audioVendor struct {
	server *httptest.Server
	// bodies records every request body.
	bodies []string
	// authorizations records the Authorization header of every request, so an authentication failure
	// says so rather than reading as an unreachable provider.
	authorizations []string
}

func newAudioVendor(t *testing.T, contentType string, payload []byte) *audioVendor {
	t.Helper()
	vendor := &audioVendor{}
	vendor.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vendor.authorizations = append(vendor.authorizations, r.Header.Get("Authorization"))
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		vendor.bodies = append(vendor.bodies, string(body))
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(vendor.server.Close)
	return vendor
}

// liveAudioRegistry builds the REAL registry with the REAL adapter and no test seams.
func liveAudioRegistry(t *testing.T, baseURL string) (*providers.Registry, *jobsAuditSink) {
	t.Helper()
	audit := &jobsAuditSink{}
	registry := providers.NewRegistry(
		&jobsConfigSource{config: audioProviderConfig(baseURL)},
		&jobsSecretResolver{secret: []byte("vendor-secret-value")},
		audit,
	)
	registry.WithOpenAIAudioAdapter(providers.NewOpenAIAudioAdapter(registry))
	return registry, audit
}

// audioProviderConfig is a user-configured OpenAI-compatible provider pointed at the fake vendor.
func audioProviderConfig(baseURL string) provider.Config {
	return provider.Config{
		ID:            "prov-speech",
		Kind:          provider.KindOpenAICompatible,
		DisplayName:   "speech vendor",
		BaseURL:       baseURL + "/v1",
		SecretRef:     provider.SecretRefValue("prov-speech"),
		LocalApproved: true,
		Enabled:       true,
		Revision:      1,
	}
}

// TestTheRealSpeechAdapterSatisfiesTheRunnerAndTheResultStore is the end-to-end path.
//
// It runs the same `Runner.Run` the worker runs and asserts the COMMIT, which can only succeed if the
// real adapter's bytes pass the real result store's sniffer-backed audio allowlist.
func TestTheRealSpeechAdapterSatisfiesTheRunnerAndTheResultStore(t *testing.T) {
	ctx := context.Background()
	// A RIFF/WAVE payload, because that is what the sniffer reports as `audio/wave` — the type the
	// store's audio allowlist accepts. A test that sent arbitrary bytes would be refused by the sniffer
	// and would read as an adapter defect rather than the fixture's.
	wav := append([]byte("RIFF"), 0x00, 0x00, 0x00, 0x00)
	wav = append(wav, []byte("WAVEfmt ")...)
	vendor := newAudioVendor(t, "audio/wave", wav)
	registry, audit := liveAudioRegistry(t, vendor.server.URL)

	content := newFakeContentStore()
	store := NewResultStore(content, newFakeMetadataStore(), &recordingReferences{})
	runner := NewRunner(registry, store, &fakeDownloader{}, 1<<20)

	record := jobRecord(job.JobTypeAudioGeneration, map[string]any{
		"providerId": "prov-speech", "model": "tts-1", "text": "夜里的渡轮靠岸了", "voice": "alloy",
	})
	record.ProviderConfigID = "prov-speech"

	outcome, err := runner.Run(ctx, record)
	if err != nil {
		t.Fatalf("the speech pass: %v", err)
	}
	if outcome.Status != job.StatusSucceeded {
		t.Fatalf("the speech pass returned %q (result %s)", outcome.Status, outcome.ResultJSON)
	}
	var metadata resultMetadata
	if err := json.Unmarshal([]byte(outcome.ResultJSON), &metadata); err != nil {
		t.Fatalf("the recorded result is not a result document: %v", err)
	}
	if len(metadata.Files) != 1 {
		t.Fatalf("the committed result cites %d files", len(metadata.Files))
	}
	// THE MIME THE STORE DECIDED, not the one the adapter announced: the two agreeing is the point, and
	// it is what proves the fallback and the pass-through are both names the allowlist admits.
	if metadata.Files[0].MIME != "audio/wave" {
		t.Fatalf("the store committed the result as %q", metadata.Files[0].MIME)
	}
	if metadata.Files[0].Size != int64(len(wav)) {
		t.Fatalf("the committed file is %d bytes", metadata.Files[0].Size)
	}
	// The bytes a user would hear are the bytes the vendor served.
	opened, err := content.Open(ctx, metadata.Files[0].StorageKey)
	if err != nil {
		t.Fatalf("the committed object cannot be opened: %v", err)
	}
	defer opened.Close()
	stored := make([]byte, metadata.Files[0].Size)
	if _, err := opened.Read(stored); err != nil {
		t.Fatalf("reading the committed object: %v", err)
	}
	if string(stored) != string(wav) {
		t.Fatalf("the committed bytes are %q", stored)
	}

	// The vendor was asked for speech, with the text and the voice the job named.
	if len(vendor.bodies) != 1 {
		t.Fatalf("the vendor received %d requests", len(vendor.bodies))
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(vendor.bodies[0]), &sent); err != nil {
		t.Fatalf("the request body is not JSON: %s", vendor.bodies[0])
	}
	if sent["input"] != "夜里的渡轮靠岸了" || sent["voice"] != "alloy" {
		t.Fatalf("the request lost its text or voice: %v", sent)
	}
	if vendor.authorizations[0] != "Bearer vendor-secret-value" {
		t.Fatalf("the request carried the authorization %q", vendor.authorizations[0])
	}
	// And the audit records the AUDIO capability, which this build had never written.
	if len(audit.records) != 1 {
		t.Fatalf("%d audit rows for one call", len(audit.records))
	}
	if audit.records[0].Capability != provider.CapabilityAudio {
		t.Fatalf("the audit names the capability %q", audit.records[0].Capability)
	}
	if strings.Contains(audit.records[0].ErrorCode, "vendor-secret-value") {
		t.Fatal("the audit carries the secret")
	}
}

// TestAVendorRefusalStopsTheJobRatherThanStoringNothing is the failing arm through the runner.
//
// A provider that answers with a 4xx must fail the job with the classified category rather than
// committing an empty result — the failure a user cannot see.
func TestAVendorRefusalStopsTheJobRatherThanStoringNothing(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	registry, _ := liveAudioRegistry(t, server.URL)
	content := newFakeContentStore()
	runner := NewRunner(registry, NewResultStore(content, newFakeMetadataStore(), &recordingReferences{}),
		&fakeDownloader{}, 1<<20)

	record := jobRecord(job.JobTypeAudioGeneration, map[string]any{
		"providerId": "prov-speech", "model": "tts-1", "text": "hello",
	})
	record.ProviderConfigID = "prov-speech"

	if _, err := runner.Run(ctx, record); err == nil {
		t.Fatal("a refused synthesis was reported as a finished job")
	}
	if len(content.stored) != 0 {
		t.Fatalf("a refused synthesis stored %d objects", len(content.stored))
	}
}
