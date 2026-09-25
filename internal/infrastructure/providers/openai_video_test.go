package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// openai_video_test.go drives the ASYNC video protocol against a fake provider.
//
// # What makes this test possible without credentials, and why that matters
//
// The adapter is real — it makes HTTP calls, classifies provider errors, writes audit rows and speaks a
// create-then-poll protocol — and every one of those is exercised here against an `httptest` server
// playing a provider that accepts, reports running twice, completes and serves bytes. **No network and
// no key**, which is why P3 item 21 could be finished on a host with no paid-provider authorisation:
// what needs authorisation is a call to a REAL vendor, not the protocol handling.
//
// # What each test is for
//
// The async protocol has failure modes a synchronous one does not, and each is a test:
//
//   - THE ACCEPTANCE MUST CARRY AN IDENTIFIER. One without it would park a job whose remote id is empty
//     and poll nothing forever.
//   - POLLING MUST DISTINGUISH running FROM done, and it must do so across SEVERAL polls — which is the
//     difference between this adapter and every other one in the package.
//   - AN UNKNOWN STATUS MUST BE REFUSED. A provider that renames a state would otherwise look like a
//     generation that never finishes.
//   - THE ERROR TAXONOMY MUST BE PRESERVED, because `Retriable` decides whether the job manager retries.
//   - A CANCEL THE PROVIDER REFUSES MUST RETURN AN ERROR, because the job table has a column for
//     "remote cancel unconfirmed" and swallowing it would make that column unreachable.
//   - THE SECRET MUST NOT LEAK into an error or an audit row.

// videoTestServer is a fake provider whose state the tests advance.
type videoTestServer struct {
	server *httptest.Server
	// statuses are handed out one per poll, so a test can script running-then-complete.
	statuses []string
	polls    int
	// bodies records every request body the server received, so a test can assert what was sent.
	bodies []string
	// deleted records that a DELETE arrived.
	deleted bool
	// failCancel makes the DELETE return an error, for the unconfirmed-cancel test.
	failCancel bool
}

func newVideoTestServer(t *testing.T, statuses ...string) *videoTestServer {
	t.Helper()
	fake := &videoTestServer{statuses: statuses}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/videos"):
			payload := make([]byte, r.ContentLength)
			if r.ContentLength > 0 {
				_, _ = r.Body.Read(payload)
			}
			fake.bodies = append(fake.bodies, string(payload))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"vid-abc"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/content"):
			// The bytes shape: the endpoint serves the video itself.
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("fake-mp4-bytes"))
		case r.Method == http.MethodGet:
			status := "in_progress"
			if fake.polls < len(fake.statuses) {
				status = fake.statuses[fake.polls]
			} else if len(fake.statuses) > 0 {
				status = fake.statuses[len(fake.statuses)-1]
			}
			fake.polls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"` + status + `","progress":50}`))
		case r.Method == http.MethodDelete:
			if fake.failCancel {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			fake.deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func newVideoTestAdapter(t *testing.T, server *httptest.Server, secret []byte) (*OpenAIVideoAdapter, *captureAudit) {
	t.Helper()
	config := provider.Config{
		ID:            "prov-video",
		Kind:          provider.KindOpenAICompatible,
		DisplayName:   "video test",
		BaseURL:       server.URL + "/v1",
		SecretRef:     provider.SecretRefValue("prov-video"),
		LocalApproved: true,
		Enabled:       true,
		Revision:      1,
	}
	audit := &captureAudit{}
	registry := NewRegistry(&staticConfigs{config: config}, &staticSecret{value: secret}, audit)
	adapter := NewOpenAIVideoAdapter(registry)
	adapter.clientFactory = func(config provider.Config) (*phttp.Client, error) {
		normalized, ok := provider.ValidateBaseURL(config.BaseURL)
		if !ok {
			return nil, provider.NewConfigurationError()
		}
		return phttp.NewClient(phttp.Policy{
			Host:       normalized.Host,
			Port:       normalized.Port,
			Scheme:     normalized.Scheme,
			AllowLocal: config.LocalApproved,
		}, phttp.NewNetResolver(), phttp.Limits{}), nil
	}
	return adapter, audit
}

func videoRequest() appjobs.VideoRequest {
	return appjobs.VideoRequest{
		JobID:      "job-video-1",
		ProviderID: "prov-video",
		Model:      "sora-2",
		Prompt:     "a lantern on the ferry at night",
		Seconds:    4,
		Size:       "1280x720",
		References: []appjobs.ImageInput{
			{MIMEType: "image/png", Bytes: []byte("reference-bytes")},
		},
	}
}

// TestTheVideoProtocolRunsFromSubmitToBytes is the end-to-end path.
//
// It walks what the runner walks: submit, then poll until done, then fetch. THE TWO INTERMEDIATE POLLS
// are the part a synchronous adapter has no equivalent of, and they are scripted rather than looped so
// the test asserts the RUNNER's contract: a non-terminal status must come back as "not done, no
// failure", which is what makes the job park instead of failing.
func TestTheVideoProtocolRunsFromSubmitToBytes(t *testing.T) {
	fake := newVideoTestServer(t, "queued", "in_progress", "completed")
	adapter, audit := newVideoTestAdapter(t, fake.server, []byte("test-secret-value"))
	ctx := context.Background()

	remote, err := adapter.Submit(ctx, videoRequest())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if remote.ID != "vid-abc" {
		t.Fatalf("the remote id is %q", remote.ID)
	}
	if remote.ProviderID != "prov-video" {
		t.Fatalf("the remote job names the provider %q", remote.ProviderID)
	}
	// WHAT WAS SENT: the prompt, the model, and the reference as a data URL. A submission that dropped
	// the reference would generate a different film, and the user would have no way to see why.
	if len(fake.bodies) != 1 {
		t.Fatalf("the server received %d bodies", len(fake.bodies))
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(fake.bodies[0]), &sent); err != nil {
		t.Fatalf("the submission body is not JSON: %s", fake.bodies[0])
	}
	if sent["prompt"] != "a lantern on the ferry at night" || sent["model"] != "sora-2" {
		t.Fatalf("the submission lost its prompt or model: %v", sent)
	}
	references, ok := sent["input_reference"].([]any)
	if !ok || len(references) != 1 {
		t.Fatalf("the reference did not travel: %v", sent["input_reference"])
	}
	first, _ := references[0].(map[string]any)
	imageURL, _ := first["image_url"].(string)
	if !strings.HasPrefix(imageURL, "data:image/png;base64,") {
		t.Fatalf("the reference is not a data URL: %q", imageURL)
	}
	if decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(imageURL, "data:image/png;base64,")); err != nil ||
		string(decoded) != "reference-bytes" {
		t.Fatalf("the reference bytes did not survive: %q", imageURL)
	}

	// THE POLLS. The first two are non-terminal, and each must say so without claiming a failure.
	for index, want := range []string{"queued", "in_progress"} {
		status, err := adapter.Poll(ctx, remote)
		if err != nil {
			t.Fatalf("poll %d (%s): %v", index, want, err)
		}
		if status.Done || status.Failed {
			t.Fatalf("poll %d returned done=%v failed=%v for the status %q", index, status.Done, status.Failed, want)
		}
	}
	// The progress the provider reported travels, which is what a job row shows.
	status, err := adapter.Poll(ctx, remote)
	if err != nil {
		t.Fatalf("the final poll: %v", err)
	}
	if !status.Done || status.Failed {
		t.Fatalf("the completed poll returned done=%v failed=%v", status.Done, status.Failed)
	}

	// THE RESULT.
	outcome, err := adapter.Fetch(ctx, remote)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if outcome.URL != "" {
		t.Fatalf("the bytes shape returned a URL: %q", outcome.URL)
	}
	if outcome.MIMEType != "video/mp4" {
		t.Fatalf("the result is typed %q", outcome.MIMEType)
	}
	decoded, err := base64.StdEncoding.DecodeString(outcome.Data)
	if err != nil || string(decoded) != "fake-mp4-bytes" {
		t.Fatalf("the bytes did not survive the round trip: %q", outcome.Data)
	}

	// THE AUDIT, with the capability the column has admitted since migration 000003 and nothing had
	// ever written. It is asserted for EVERY call the test made.
	records := audit.records
	if len(records) < 5 {
		t.Fatalf("%d audit rows for five calls (submit, three polls, fetch)", len(records))
	}
	for _, record := range records {
		if record.Capability != provider.CapabilityVideo {
			t.Fatalf("an audit row names the capability %q", record.Capability)
		}
		if strings.Contains(record.ErrorCode, "test-secret-value") {
			t.Fatal("an audit row carries the secret")
		}
	}
}

// TestAnUnknownStatusIsRefusedRatherThanPolledForever is the state-vocabulary rule.
//
// A provider that renames a state, or that this adapter has never seen, must NOT read as "still
// running": the job would poll until its attempt budget ran out and look to a user like a generation
// that never finishes, with nothing in the report to say why.
func TestAnUnknownStatusIsRefusedRatherThanPolledForever(t *testing.T) {
	seen := map[string]bool{}
	fake := newVideoTestServer(t, "queued")
	// A server that answers with a state no adapter knows. It is built here rather than through the
	// helper because the helper's statuses are the vocabulary.
	fake.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"reticulating"}`))
	})
	adapter, _ := newVideoTestAdapter(t, fake.server, []byte("test-secret-value"))
	if _, err := adapter.Poll(context.Background(), appjobs.RemoteJob{ProviderID: "prov-video", ID: "vid-abc"}); err == nil {
		t.Fatal("an unknown status was accepted")
	} else if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategoryResponseInvalid {
		t.Fatalf("the refusal is %v rather than a response-invalid error", err)
	}
	_ = seen
}

// TestTheVideoErrorMessageIsScrubbed is the redaction rule at the one place a provider's own words
// reach a user.
//
// A provider's failure text is written for a developer and can quote the request — a prompt, a URL
// with a token in it. The message is kept when it is harmless and replaced when it is not, because a
// partial redaction of an unknown shape is how a token survives in a log.
func TestTheVideoErrorMessageIsScrubbed(t *testing.T) {
	cases := []struct{ message, want string }{
		{"the model is overloaded", "the model is overloaded"},
		{"", ""},
		{"see https://api.example.com/jobs?token=abc123", "The provider reported an error."},
		{"invalid api_key sk-live-0123456789", "The provider reported an error."},
		{"authorization: Bearer abc", "The provider reported an error."},
	}
	for _, testCase := range cases {
		if got := safeProviderMessage(testCase.message); got != testCase.want {
			t.Fatalf("safeProviderMessage(%q) = %q, want %q", testCase.message, got, testCase.want)
		}
	}
	// A long but harmless message is truncated rather than replaced: a reader loses the tail, not the
	// fact that something went wrong.
	long := strings.Repeat("x", 400)
	if got := safeProviderMessage(long); len(got) > 320 {
		t.Fatalf("a 400-character message came back as %d characters", len(got))
	}
}

// TestTheVideoErrorTaxonomyIsPreserved is what decides whether a job retries.
//
// The category is not cosmetic: `IsRetriable` reads it, and the job manager retries `remote_transient`
// and `rate_limited` while refusing `unauthorized` and `invalid_input`. An adapter that collapsed every
// failure into one category would retry a wrong key forever or give up on a hiccup.
func TestTheVideoErrorTaxonomyIsPreserved(t *testing.T) {
	cases := []struct {
		status   int
		category provider.ErrorCategory
	}{
		{http.StatusUnauthorized, provider.CategoryUnauthorized},
		{http.StatusForbidden, provider.CategoryForbidden},
		{http.StatusBadRequest, provider.CategoryInvalidInput},
		{http.StatusTooManyRequests, provider.CategoryRateLimited},
		{http.StatusServiceUnavailable, provider.CategoryRemoteTransient},
		{http.StatusInternalServerError, provider.CategoryRemoteTransient},
	}
	for _, testCase := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(testCase.status)
		}))
		adapter, _ := newVideoTestAdapter(t, server, []byte("test-secret-value"))
		_, err := adapter.Submit(context.Background(), videoRequest())
		server.Close()
		if err == nil {
			t.Fatalf("a %d response was accepted", testCase.status)
		}
		providerErr, ok := provider.AsProviderError(err)
		if !ok {
			t.Fatalf("the %d refusal is %v rather than a provider error", testCase.status, err)
		}
		if providerErr.Category != testCase.category {
			t.Fatalf("a %d response is classified %q, want %q", testCase.status, providerErr.Category, testCase.category)
		}
		if strings.Contains(err.Error(), "test-secret-value") {
			t.Fatal("the error carries the secret")
		}
	}
}

// TestACancelledVideoCarriesNoSecretAndReportsItsFailure is the cancel handshake.
//
// Two halves, and the second is the one the job table depends on: a DELETE that succeeds returns nil,
// and a DELETE the provider REFUSES returns an error rather than being swallowed — because
// `remote_cancel_unconfirmed` exists for exactly that state, and a Cancel that always returned nil
// would make the column unreachable.
func TestACancelledVideoCarriesNoSecretAndReportsItsFailure(t *testing.T) {
	remote := appjobs.RemoteJob{ProviderID: "prov-video", ID: "vid-abc"}

	fake := newVideoTestServer(t, "in_progress")
	adapter, audit := newVideoTestAdapter(t, fake.server, []byte("test-secret-value"))
	if err := adapter.Cancel(context.Background(), remote); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !fake.deleted {
		t.Fatal("the adapter reported success without sending the DELETE")
	}
	for _, record := range audit.records {
		if strings.Contains(record.ErrorCode, "test-secret-value") {
			t.Fatal("the audit carries the secret")
		}
	}

	refusing := newVideoTestServer(t, "in_progress")
	refusing.failCancel = true
	adapter2, _ := newVideoTestAdapter(t, refusing.server, []byte("test-secret-value"))
	if err := adapter2.Cancel(context.Background(), remote); err == nil {
		t.Fatal("a refused cancel reported success, which would leave remote_cancel_unconfirmed unreachable")
	}
}

// TestAFailureTheProviderReportsIsADoneAndFailedStatus is the port's OTHER terminal state.
//
// The port has exactly two, and the runner reads them apart: `Done && !Failed` is a generation to
// download, while `Done && Failed` is `job.CategoryRemotePermanent` — the job stops and the provider's
// message is what a user is shown. A mapping that returned `Done` alone for a failed status would read
// to the runner as a SUCCESS with nothing to download, so the failure would surface as a missing
// result rather than as the provider's own words.
func TestAFailureTheProviderReportsIsADoneAndFailedStatus(t *testing.T) {
	// Every word the protocol uses for a failure, including the second spelling of cancelled: the
	// vocabulary table is the only place that decides this, and an entry edited while its neighbour was
	// being changed is what a per-word assertion catches.
	for _, word := range []string{"failed", "error", "cancelled", "canceled"} {
		status, known := mapVideoStatus(word)
		if !known {
			t.Fatalf("the status %q is not in the vocabulary", word)
		}
		if !status.Done || !status.Failed {
			t.Fatalf("the status %q maps to done=%v failed=%v", word, status.Done, status.Failed)
		}
	}
	// And the same through the wire, because the mapping's return value is only half of it: `Poll`
	// builds the struct it returns, and a field dropped after the mapping would survive the assertions
	// above.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"failed","error":{"message":"the safety system refused this prompt"}}`))
	}))
	defer server.Close()
	adapter, audit := newVideoTestAdapter(t, server, []byte("test-secret-value"))
	status, err := adapter.Poll(context.Background(), appjobs.RemoteJob{ProviderID: "prov-video", ID: "vid-abc"})
	if err != nil {
		t.Fatalf("a failed generation is a status, not a call error: %v", err)
	}
	if !status.Done || !status.Failed {
		t.Fatalf("a failed generation returned done=%v failed=%v, which the runner reads as a success", status.Done, status.Failed)
	}
	// The provider's own words travel, because they are the whole explanation a user gets for a job
	// that produced no film.
	if !strings.Contains(status.Message, "safety system refused") {
		t.Fatalf("the failure message is %q", status.Message)
	}
	// AND THE AUDIT SAYS SO. This is the half that a status-only assertion cannot see, and it is the
	// half with consequences: `DiagnosticsReader.ErrorCodes` groups `provider_requests` by error_code,
	// so a failed generation audited as `succeeded` leaves the one PERSISTED record of the failure
	// blank, and a user asking "why do my videos never generate" finds nothing. The HTTP call did
	// succeed; the GENERATION did not, and the row is about the generation.
	if len(audit.records) != 1 {
		t.Fatalf("%d audit rows for one poll", len(audit.records))
	}
	if audit.records[0].Status != provider.StatusFailed {
		t.Fatalf("a failed generation is audited %q", audit.records[0].Status)
	}
	if audit.records[0].ErrorCode != string(provider.CategoryRemotePermanent) {
		t.Fatalf("the audit records the error code %q", audit.records[0].ErrorCode)
	}
}

// TestAVideoSubmissionWithoutAnIdentifierIsRefused is the first async-specific defect.
//
// The acceptance is `{"id": "..."}` and nothing else is promised. A provider that answers 200 with an
// empty body — or with a document this adapter cannot read — would leave the pipeline parking a job
// whose remote id is empty, polling nothing, until the attempt budget ran out.
func TestAVideoSubmissionWithoutAnIdentifierIsRefused(t *testing.T) {
	for _, body := range []string{`{}`, `{"id":""}`, `{"id":"   "}`, `not json`, ``} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		adapter, _ := newVideoTestAdapter(t, server, []byte("test-secret-value"))
		_, err := adapter.Submit(context.Background(), videoRequest())
		server.Close()
		if err == nil {
			t.Fatalf("an acceptance of %q was taken as a submission", body)
		}
	}
}

// TestAVideoResultOverTheInlineBoundIsRefused is the third async-specific defect.
//
// The inline shape buffers a whole video through a JSON string, and a real clip is tens of megabytes:
// past the bound the adapter refuses rather than truncating, because a half a video committed as a
// result would be VERIFIED and stored as a complete generation. The provider's URL mode is the answer,
// and a refusal is what sends a caller there.
func TestAVideoResultOverTheInlineBoundIsRefused(t *testing.T) {
	// 9 MiB: past this adapter's bound and UNDER the guarded client's 10 MiB response ceiling, which is
	// what makes the adapter's own check the thing that refuses. The first version of this test used
	// 64 MiB and failed for the opposite reason — the CLIENT truncated first, so the adapter's bound was
	// unreachable — and that failure is why `maxInlineVideoBytes` is now the transport's ceiling rather
	// than a larger number of this adapter's choosing.
	oversized := strings.Repeat("A", maxInlineVideoBytes+16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte(oversized))
	}))
	adapter, _ := newVideoTestAdapter(t, server, []byte("test-secret-value"))
	_, err := adapter.Fetch(context.Background(), appjobs.RemoteJob{ProviderID: "prov-video", ID: "vid-abc"})
	server.Close()
	if err == nil {
		t.Fatal("a result past the inline bound was accepted")
	}
	// And an EMPTY result is refused too: a zero-byte video is a send that went wrong, and the
	// pipeline would otherwise verify and commit it.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
	}))
	adapter2, _ := newVideoTestAdapter(t, empty, []byte("test-secret-value"))
	if _, err := adapter2.Fetch(context.Background(), appjobs.RemoteJob{ProviderID: "prov-video", ID: "vid-abc"}); err == nil {
		t.Fatal("an empty result was accepted")
	}
	empty.Close()
}

// TestTheVideoResultURLIsReturnedForTheDownloader is the URL shape.
//
// The MIME is deliberately EMPTY here: the download path sniffs the bytes, and a provider's claim
// about a file it did not serve is not one this pipeline trusts. Asserting the emptiness states that
// rule where a later change might fill it in for tidiness.
func TestTheVideoResultURLIsReturnedForTheDownloader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"https://cdn.example.com/v/abc.mp4"}`))
	}))
	adapter, _ := newVideoTestAdapter(t, server, []byte("test-secret-value"))
	outcome, err := adapter.Fetch(context.Background(), appjobs.RemoteJob{ProviderID: "prov-video", ID: "vid-abc"})
	server.Close()
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if outcome.URL != "https://cdn.example.com/v/abc.mp4" {
		t.Fatalf("the URL did not survive: %q", outcome.URL)
	}
	if outcome.MIMEType != "" {
		t.Fatalf("the URL shape states a MIME (%q), and the downloader must sniff it instead", outcome.MIMEType)
	}
}

// TestTheVideoAdapterRefusesWithoutConfigOrSecret is the fail-closed rule.
//
// The secret half is the one worth asserting with a server that must NOT be reached: an adapter that
// sent the request anyway would have made an unauthenticated call to a provider endpoint.
func TestTheVideoAdapterRefusesWithoutConfigOrSecret(t *testing.T) {
	ctx := context.Background()
	// No registry at all.
	var bare *OpenAIVideoAdapter
	if _, err := bare.Submit(ctx, videoRequest()); err == nil {
		t.Fatal("an unattached adapter submitted")
	}
	if _, err := bare.Poll(ctx, appjobs.RemoteJob{ID: "x"}); err == nil {
		t.Fatal("an unattached adapter polled")
	}
	if _, err := bare.Fetch(ctx, appjobs.RemoteJob{ID: "x"}); err == nil {
		t.Fatal("an unattached adapter fetched")
	}
	if err := bare.Cancel(ctx, appjobs.RemoteJob{ID: "x"}); err == nil {
		t.Fatal("an unattached adapter cancelled")
	}

	reached := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	adapter, _ := newVideoTestAdapter(t, server, nil)
	if _, err := adapter.Submit(ctx, videoRequest()); err == nil {
		t.Fatal("a submission without a secret was accepted")
	}
	if reached {
		t.Fatal("the request reached the provider without a secret")
	}
	// An empty prompt is refused before any call, for the same reason.
	adapter2, _ := newVideoTestAdapter(t, server, []byte("test-secret-value"))
	empty := videoRequest()
	empty.Prompt = "   "
	if _, err := adapter2.Submit(ctx, empty); err == nil {
		t.Fatal("a submission with no prompt was accepted")
	}
	if reached {
		t.Fatal("a promptless submission reached the provider")
	}
	// And a remote job with no identifier cannot be polled, fetched or cancelled: those calls would
	// address `/videos/` and get whatever that path happens to answer.
	//
	// THE ASSERTION IS NOT "AN ERROR CAME BACK". adapter2 is built against a live server, so a call that
	// got past the guard would fail anyway — the server answers 200 with an empty body and the adapter
	// would report a response it could not read. That error would be indistinguishable from a refusal
	// here, and a mutation deleting the guard would survive. So two things are asserted instead: the
	// refusal is `invalid_input` (the guard's category, not the parser's `response_invalid`), and the
	// REQUEST NEVER REACHED THE PROVIDER. The same layer-versus-outcome distinction is what the secret
	// case above needs, and it is why `reached` is reset here rather than reused as a running flag.
	reached = false
	for _, id := range []string{"", "   "} {
		if _, err := adapter2.Poll(ctx, appjobs.RemoteJob{ProviderID: "prov-video", ID: id}); err == nil {
			t.Fatalf("a poll with the id %q was accepted", id)
		} else if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategoryInvalidInput {
			t.Fatalf("the poll with the id %q was refused as %v rather than invalid input", id, err)
		}
		if _, err := adapter2.Fetch(ctx, appjobs.RemoteJob{ProviderID: "prov-video", ID: id}); err == nil {
			t.Fatalf("a fetch with the id %q was accepted", id)
		} else if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategoryInvalidInput {
			t.Fatalf("the fetch with the id %q was refused as %v rather than invalid input", id, err)
		}
		if err := adapter2.Cancel(ctx, appjobs.RemoteJob{ProviderID: "prov-video", ID: id}); err == nil {
			t.Fatalf("a cancel with the id %q was accepted", id)
		} else if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategoryInvalidInput {
			t.Fatalf("the cancel with the id %q was refused as %v rather than invalid input", id, err)
		}
	}
	if reached {
		t.Fatal("a request with no remote identifier reached the provider, which would address the collection")
	}
}

// TestTheRegistryResolvesTheRealVideoAdapterForAnOpenAIProvider is the reachability assertion.
//
// It is the one that would have failed before this package: `VideoPortFor` returned `unsupported` for
// every kind but `mock_media`, so the adapter could exist and no command could reach it. The mock must
// still resolve for its own kind, which is asserted in the same test because a change that made the
// real adapter reachable by breaking the mock would pass half of it.
func TestTheRegistryResolvesTheRealVideoAdapterForAnOpenAIProvider(t *testing.T) {
	ctx := context.Background()
	openai := provider.Config{
		ID: "prov-real", Kind: provider.KindOpenAICompatible, DisplayName: "real",
		BaseURL: "https://api.example.com/v1", SecretRef: provider.SecretRefValue("prov-real"),
		Enabled: true, Revision: 1,
	}
	mock := provider.Config{
		ID: "prov-mock", Kind: provider.KindMockMedia, DisplayName: "mock",
		BaseURL: "https://mock.invalid", SecretRef: provider.SecretRefValue("prov-mock"),
		Enabled: true, Revision: 1,
	}
	registry := NewRegistry(&mapConfigs{configs: map[string]provider.Config{
		"prov-real": openai, "prov-mock": mock,
	}}, &staticSecret{value: []byte("s")}, &captureAudit{})
	registry.WithMediaAdapters(NewMockVideoAdapter(), NewMockAudioAdapter())
	registry.WithOpenAIVideoAdapter(NewOpenAIVideoAdapter(registry))

	real, err := registry.VideoPortFor(ctx, "prov-real")
	if err != nil {
		t.Fatalf("the real adapter did not resolve for an OpenAI provider: %v", err)
	}
	if _, ok := real.(*OpenAIVideoAdapter); !ok {
		t.Fatalf("the OpenAI provider resolved to %T", real)
	}
	mockPort, err := registry.VideoPortFor(ctx, "prov-mock")
	if err != nil {
		t.Fatalf("the mock did not resolve for its own kind: %v", err)
	}
	if _, ok := mockPort.(*MockVideoAdapter); !ok {
		t.Fatalf("the mock kind resolved to %T", mockPort)
	}
	// A DISABLED provider still refuses, which is the configuration rule rather than the kind rule.
	disabled := openai
	disabled.ID, disabled.Enabled = "prov-off", false
	registry2 := NewRegistry(&mapConfigs{configs: map[string]provider.Config{"prov-off": disabled}},
		&staticSecret{value: []byte("s")}, &captureAudit{})
	registry2.WithOpenAIVideoAdapter(NewOpenAIVideoAdapter(registry2))
	if _, err := registry2.VideoPortFor(ctx, "prov-off"); err == nil {
		t.Fatal("a disabled provider resolved a video port")
	}
	// And an UNREGISTERED adapter refuses rather than silently doing nothing: a build composed without
	// the real adapter must say unsupported.
	registry3 := NewRegistry(&staticConfigs{config: openai}, &staticSecret{value: []byte("s")}, &captureAudit{})
	if _, err := registry3.VideoPortFor(ctx, "prov-real"); err == nil {
		t.Fatal("an unregistered adapter resolved a video port")
	}
	_ = errors.Is
}

// mapConfigs resolves a provider by identifier from a map.
//
// It exists beside `staticConfigs` because that double holds ONE config, and the reachability test
// needs two: the real adapter's kind and the mock's must resolve differently in the same registry, and
// a double that could only hold one would test each in isolation while the bug worth catching is the
// two coexisting.
type mapConfigs struct {
	configs map[string]provider.Config
}

func (m *mapConfigs) GetConfig(_ context.Context, id string) (provider.Config, error) {
	if config, ok := m.configs[id]; ok {
		return config, nil
	}
	return provider.Config{}, provider.NewConfigurationError()
}
