package jobs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// video_provider_e2e_test.go drives the REAL asynchronous video adapter through the REAL runner and
// the REAL result store, against a fake VENDOR and nothing else.
//
// # Why this test exists when the adapter already has a suite
//
// The adapter's own tests prove it speaks the protocol. They do not prove the pipeline ACCEPTS what it
// produces — and that is a different claim, made by different code:
//
//   - THE MIME ALLOWLIST is the result store's, and it runs against the type the FileStore's SNIFFER
//     produced, never against a name the adapter chose. An adapter that returned a correct-looking
//     `MIMEType` over bytes the sniffer reads as `application/octet-stream` would pass its own tests
//     and be refused here, with the failure landing on a user as a job that produces nothing.
//   - THE PORT SHAPE is the runner's. `Poll` returning `Failed` has to become
//     `job.CategoryRemotePermanent`; `Poll` returning `Done` has to reach `Fetch`; a non-terminal poll
//     has to come back `PollOnly` so the attempt budget is not spent on waiting.
//   - THE CLIENT IS THE PRODUCTION ONE. This test injects NO `clientFactory`: the adapter resolves
//     `guardedClient` itself, so the SSRF policy, the redirect rules and the response ceiling are all
//     in the path rather than replaced by a test double.
//
// The last point is why the fake vendor is an `httptest` server on 127.0.0.1 with `LocalApproved` set:
// that flag is how a user authorises a local endpoint, and a test that bypassed the policy could not
// notice the policy breaking.
//
// # What is NOT proven here
//
// That a real vendor accepts the submission body. There is no authorised call to make (AGENTS 4.3), so
// the field names are an assumption recorded in ADR-0027 — what this test proves is that the protocol
// HANDLING and the pipeline's acceptance of its output both work.

// videoVendor plays an OpenAI-style video endpoint whose script the test sets.
type videoVendor struct {
	server *httptest.Server
	// statuses are handed out one per poll; the last one repeats.
	statuses []string
	polls    int

	// submissions counts the POSTs, so "the runner re-submitted instead of polling" is observable.
	submissions int
	// contents counts the byte fetches.
	contents int
	// authorizations records the Authorization header of every request, in order. It is what makes an
	// authentication failure say so: without it, a request that went out with a zeroed header fails at
	// the transport as "the provider could not be reached", which reads as a network fault.
	authorizations []string
	// bodies records every submission body, so a test can assert what was SENT. It is what found the
	// empty-reference defect: the request succeeded and the reference was blank, so counting requests
	// could not tell a working submission from one that carried nothing.
	bodies []string
}

func newVideoVendor(t *testing.T, statuses ...string) *videoVendor {
	t.Helper()
	vendor := &videoVendor{statuses: statuses}
	vendor.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vendor.authorizations = append(vendor.authorizations, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/videos"):
			vendor.submissions++
			payload, _ := io.ReadAll(r.Body)
			vendor.bodies = append(vendor.bodies, string(payload))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"vid-live-1"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/content"):
			vendor.contents++
			// THE REAL MP4 HEADER, because the sniffer is what decides the type here. A test that
			// returned arbitrary bytes would be refused by the allowlist and would read as an adapter
			// defect rather than the fixture's.
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(videoFTYPBytes())
		case r.Method == http.MethodGet:
			status := "in_progress"
			if vendor.polls < len(vendor.statuses) {
				status = vendor.statuses[vendor.polls]
			} else if len(vendor.statuses) > 0 {
				status = vendor.statuses[len(vendor.statuses)-1]
			}
			vendor.polls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"` + status + `","progress":40}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(vendor.server.Close)
	return vendor
}

// videoFTYPBytes is a minimal but genuinely sniffer-readable MP4 header.
//
// It is copied in shape from the duplicate-response test's payload, and the shape is not cosmetic: the
// sniffer requires a box size that is a multiple of four and inside the buffer, plus a four-byte
// aligned "mp4" brand outside the major-brand slot. A truncated box sniffs as
// `application/octet-stream` and the video allowlist refuses it.
func videoFTYPBytes() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00,
		'm', 'p', '4', '1', 'i', 's', 'o', 'm',
	}
}

// videoProviderConfig is a user-configured OpenAI-compatible provider pointed at the fake vendor.
func videoProviderConfig(baseURL string) provider.Config {
	return provider.Config{
		ID:            "prov-live",
		Kind:          provider.KindOpenAICompatible,
		DisplayName:   "live video vendor",
		BaseURL:       baseURL + "/v1",
		SecretRef:     provider.SecretRefValue("prov-live"),
		LocalApproved: true,
		Enabled:       true,
		Revision:      1,
	}
}

// liveVideoRegistry builds the REAL registry with the REAL adapter and no test seams.
//
// The audit sink is returned as well, because "the production wiring has somewhere to write the
// capability" is one of the claims the walk makes: `provider_requests.capability` has admitted 'video'
// since migration 000003 and no adapter had ever written it, so a registry composed this way is the
// first thing in the build that does.
func liveVideoRegistry(t *testing.T, baseURL string) (*providers.Registry, *jobsAuditSink) {
	t.Helper()
	audit := &jobsAuditSink{}
	registry := providers.NewRegistry(
		&jobsConfigSource{config: videoProviderConfig(baseURL)},
		&jobsSecretResolver{secret: []byte("vendor-secret-value")},
		audit,
	)
	registry.WithOpenAIVideoAdapter(providers.NewOpenAIVideoAdapter(registry))
	return registry, audit
}

// TestTheRealAdapterSatisfiesTheRunnerAndTheResultStore is the end-to-end path that crosses both
// boundaries at once.
//
// It runs the same `Runner.Run` the worker runs, three times over one job record: the first pass
// submits and parks, the second polls and finds the provider still working, and the third finds it
// done, fetches it and commits it. THE COMMIT IS THE ASSERTION THAT MATTERS: it can only succeed if the
// real adapter's bytes pass the real result store's sniffer-backed allowlist.
func TestTheRealAdapterSatisfiesTheRunnerAndTheResultStore(t *testing.T) {
	ctx := context.Background()
	// queued -> in_progress -> completed, which is one poll per pass.
	vendor := newVideoVendor(t, "queued", "completed")
	registry, audit := liveVideoRegistry(t, vendor.server.URL)

	content := newFakeContentStore()
	store := NewResultStore(content, newFakeMetadataStore(), &recordingReferences{})
	runner := NewRunner(registry, store, &fakeDownloader{}, 1<<20)

	record := jobRecord(job.JobTypeVideoGeneration, map[string]any{
		"providerId": "prov-live", "model": "vendor-video-1", "prompt": "a lantern on the ferry", "seconds": 4,
	})
	record.ProviderConfigID = "prov-live"

	// PASS 1 — SUBMIT. The outcome must park with the provider's identifier, because that value is what
	// the next pass polls with: a submission whose id were dropped here would have the runner submit a
	// SECOND generation on the next pass, and the user would be billed twice for one shot.
	first, err := runner.Run(ctx, record)
	if err != nil {
		t.Fatalf("the submit pass: %v", err)
	}
	if first.Status != job.StatusWaitingRemote {
		t.Fatalf("the submit pass returned %q", first.Status)
	}
	if first.RemoteJobID != "vid-live-1" {
		t.Fatalf("the submit pass parked with the remote id %q", first.RemoteJobID)
	}
	if vendor.submissions != 1 {
		t.Fatalf("the submit pass made %d submissions", vendor.submissions)
	}

	// PASS 2 — POLL, NOT YET DONE. `PollOnly` is the flag that keeps a wait off the attempt budget, so
	// a job parked on a slow provider is not failed for being slow.
	record.RemoteJobID = first.RemoteJobID
	second, err := runner.Run(ctx, record)
	if err != nil {
		t.Fatalf("the waiting pass: %v", err)
	}
	if second.Status != job.StatusWaitingRemote || !second.PollOnly {
		t.Fatalf("the waiting pass returned status=%q pollOnly=%v", second.Status, second.PollOnly)
	}
	if vendor.submissions != 1 {
		t.Fatalf("the waiting pass submitted again (%d submissions)", vendor.submissions)
	}

	// PASS 3 — DONE, FETCHED AND COMMITTED.
	third, err := runner.Run(ctx, record)
	if err != nil {
		t.Fatalf("the fetch pass: %v", err)
	}
	if third.Status != job.StatusSucceeded {
		t.Fatalf("the fetch pass returned %q (result %s)", third.Status, third.ResultJSON)
	}
	var metadata resultMetadata
	if err := json.Unmarshal([]byte(third.ResultJSON), &metadata); err != nil {
		t.Fatalf("the recorded result is not a result document: %v", err)
	}
	if len(metadata.Files) != 1 {
		t.Fatalf("the committed result cites %d files", len(metadata.Files))
	}
	// THE MIME THE STORE DECIDED, not the one the adapter announced: the two agreeing is the point.
	if metadata.Files[0].MIME != "video/mp4" {
		t.Fatalf("the store committed the result as %q", metadata.Files[0].MIME)
	}
	if metadata.Files[0].Size != int64(len(videoFTYPBytes())) {
		t.Fatalf("the committed file is %d bytes", metadata.Files[0].Size)
	}
	if vendor.contents != 1 {
		t.Fatalf("the fetch pass made %d content requests", vendor.contents)
	}
	if vendor.submissions != 1 {
		t.Fatalf("the whole walk made %d submissions, want exactly 1", vendor.submissions)
	}
	// EVERY REQUEST CARRIED THE SECRET, and this is asserted per request rather than once because the
	// second call is where a resolver that hands out a shared buffer stops working: the adapter zeroes
	// the slice it is given, so such a resolver authorises the first call and sends a NUL-bearing header
	// on every one after. That failure surfaces at the transport as an unreachable provider, with
	// nothing to say the credential was the problem — which is why the assertion names the header.
	// EVERY CALL WAS AUDITED under the video capability, which is the column migration 000003 opened
	// and nothing had ever written through a production registry.
	if len(audit.records) != 4 {
		t.Fatalf("%d audit rows for four calls", len(audit.records))
	}
	for _, record := range audit.records {
		if record.Capability != provider.CapabilityVideo {
			t.Fatalf("an audit row names the capability %q", record.Capability)
		}
		if strings.Contains(record.ErrorCode, "vendor-secret-value") {
			t.Fatal("an audit row carries the secret")
		}
	}

	// FOUR requests: the submit, a poll on the waiting pass, then a poll AND a content fetch on the
	// pass that found it done. Counting them is what proves the walk did not skip or repeat a step.
	if len(vendor.authorizations) != 4 {
		t.Fatalf("the vendor received %d requests, want 4 (submit, two polls, one fetch)", len(vendor.authorizations))
	}
	for index, authorization := range vendor.authorizations {
		if authorization != "Bearer vendor-secret-value" {
			t.Fatalf("request %d carried the authorization %q", index, authorization)
		}
	}
	// The bytes a user would open are the bytes the vendor served.
	opened, err := content.Open(ctx, metadata.Files[0].StorageKey)
	if err != nil {
		t.Fatalf("the committed object cannot be opened: %v", err)
	}
	defer opened.Close()
	stored := make([]byte, metadata.Files[0].Size)
	if _, err := opened.Read(stored); err != nil {
		t.Fatalf("reading the committed object: %v", err)
	}
	if string(stored) != string(videoFTYPBytes()) {
		t.Fatalf("the committed bytes are %q", stored)
	}
}

// TestAFirstAndLastFrameReachTheVendorAsBytes is the defect a WP-28 probe found, crossed by the
// boundary that hid it.
//
// # The defect
//
// The runner assembles references as `ImageInput{Data: …}` — a data URL — and leaves `Bytes` empty.
// WP-26's adapter read `Bytes` alone, so every reference travelled as `data:image/png;base64,` with
// nothing after the comma, and a provider would read that as a zero-byte image. The package's comment
// claimed the first/last-frame pipe was complete; the pipe WAS complete and it delivered an empty
// string end to end.
//
// # Why this test and not the adapter's
//
// The adapter's suite now has a regression for the same bug, and neither test is redundant: that one
// builds the runner's SHAPE by hand, and this one has the RUNNER build it. If the runner ever changes
// which field it fills — the exact drift that caused the defect — the hand-built test keeps passing
// while this one fails.
func TestAFirstAndLastFrameReachTheVendorAsBytes(t *testing.T) {
	ctx := context.Background()
	vendor := newVideoVendor(t, "completed")
	registry, _ := liveVideoRegistry(t, vendor.server.URL)
	runner := NewRunner(registry, NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{}),
		&fakeDownloader{}, 1<<20)

	// The two frames carry a real PNG signature as BYTES, escaped rather than written literally so
	// the source file stays printable and the assertion is about content rather than about a base64
	// round trip of a constant.
	firstFrameBytes := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("first")...)
	lastFrameBytes := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("last")...)
	firstFrame := "data:image/png;base64," + base64.StdEncoding.EncodeToString(firstFrameBytes)
	lastFrame := "data:image/png;base64," + base64.StdEncoding.EncodeToString(lastFrameBytes)
	record := jobRecord(job.JobTypeVideoGeneration, map[string]any{
		"providerId": "prov-live", "model": "vendor-video-1", "prompt": "a lantern on the ferry",
		"seconds": 4, "firstFrame": firstFrame, "firstFrameMime": "image/png",
		"lastFrame": lastFrame, "lastFrameMime": "image/png",
	})
	record.ProviderConfigID = "prov-live"

	if _, err := runner.Run(ctx, record); err != nil {
		t.Fatalf("the submit pass: %v", err)
	}
	if len(vendor.bodies) != 1 {
		t.Fatalf("the vendor received %d submissions", len(vendor.bodies))
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(vendor.bodies[0]), &sent); err != nil {
		t.Fatalf("the submission body is not JSON: %s", vendor.bodies[0])
	}
	raw, ok := sent["input_reference"].([]any)
	if !ok || len(raw) != 2 {
		t.Fatalf("the submission carries %v references, want the first and last frame", sent["input_reference"])
	}
	// EACH FRAME IS ASSERTED SEPARATELY, in the ORDER the runner assembled them: a command that sent
	// two copies of one frame would pass a count assertion.
	for index, want := range []string{"first", "last"} {
		entry, _ := raw[index].(map[string]any)
		imageURL, _ := entry["image_url"].(string)
		prefix := "data:image/png;base64,"
		if !strings.HasPrefix(imageURL, prefix) {
			t.Fatalf("frame %d is not a PNG data URL: %q", index, imageURL)
		}
		encoded := strings.TrimPrefix(imageURL, prefix)
		if encoded == "" {
			t.Fatalf("frame %d travelled as an EMPTY data URL, which is the defect this test exists for", index)
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatalf("frame %d is not base64: %q", index, encoded)
		}
		if !strings.HasSuffix(string(decoded), want) {
			t.Fatalf("frame %d carries %q, want the %s frame", index, decoded, want)
		}
	}
}

// TestAProviderFailureStopsTheJobRatherThanDownloading is the failing arm through the runner.
//
// The adapter maps a provider-side failure to `Done && Failed`, and THIS is what reads it: the runner
// must classify it `remote_permanent` and stop, rather than falling through to `Fetch` and committing
// whatever the content endpoint happens to answer for a job that never completed. A runner that
// fetched anyway would store an error page as the user's video.
func TestAProviderFailureStopsTheJobRatherThanDownloading(t *testing.T) {
	ctx := context.Background()
	vendor := newVideoVendor(t, "failed")
	registry, audit := liveVideoRegistry(t, vendor.server.URL)
	content := newFakeContentStore()
	runner := NewRunner(registry, NewResultStore(content, newFakeMetadataStore(), &recordingReferences{}),
		&fakeDownloader{}, 1<<20)

	record := jobRecord(job.JobTypeVideoGeneration, map[string]any{
		"providerId": "prov-live", "model": "vendor-video-1", "prompt": "x",
	})
	record.ProviderConfigID = "prov-live"
	record.RemoteJobID = "vid-live-1"

	_, err := runner.Run(ctx, record)
	if err == nil {
		t.Fatal("a failed generation was reported as a finished job")
	}
	var jobError *job.Error
	if !errors.As(err, &jobError) || jobError.Category != job.CategoryRemotePermanent {
		t.Fatalf("the failure is %v rather than a remote-permanent job error", err)
	}
	if vendor.contents != 0 {
		t.Fatalf("a failed generation was fetched %d times", vendor.contents)
	}
	if len(content.stored) != 0 {
		t.Fatalf("a failed generation stored %d objects", len(content.stored))
	}
	// The failure was audited too, and the row count is ONE because this walk makes one call: the
	// record already carried a remote id, so the runner polled and never submitted. A record written
	// only for successes would leave the call a user most needs to see out of the audit.
	if len(audit.records) != 1 {
		t.Fatalf("%d audit rows for one poll", len(audit.records))
	}
	if audit.records[0].Status != provider.StatusFailed {
		t.Fatalf("the poll's audit row is marked %q", audit.records[0].Status)
	}
}

// jobsConfigSource resolves one provider configuration.
type jobsConfigSource struct{ config provider.Config }

func (s *jobsConfigSource) GetConfig(context.Context, string) (provider.Config, error) {
	return s.config, nil
}

// jobsSecretResolver returns one secret so the adapter can authorise.
//
// IT COPIES, and the copy is not incidental. `authorize` zeroes the slice it is handed as soon as the
// Authorization header is set — the discipline every adapter in this package keeps — so a double that
// returned the same backing array would serve ZEROS from the second call onwards. That failure is not
// hypothetical: it is what this test hit first, and it arrives as `net/http: invalid header field value
// for "Authorization"` mapped to a *network* error, which points at the transport rather than at the
// fixture. The package's `staticSecret` copies for the same reason.
type jobsSecretResolver struct{ secret []byte }

func (s *jobsSecretResolver) ResolveInternal(context.Context, string) ([]byte, error) {
	out := make([]byte, len(s.secret))
	copy(out, s.secret)
	return out, nil
}

// jobsAuditSink accepts audit rows. The adapter's own suite asserts their content; here the point is
// that a registry built the production way has somewhere to write them.
type jobsAuditSink struct{ records []provider.RequestRecord }

func (s *jobsAuditSink) SaveRequestRecord(_ context.Context, record provider.RequestRecord) error {
	s.records = append(s.records, record)
	return nil
}
