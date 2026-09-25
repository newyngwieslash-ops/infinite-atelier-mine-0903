package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// openai_audio_test.go drives the SYNCHRONOUS speech protocol against a fake provider.
//
// # What makes this test possible without credentials, and why that matters
//
// The adapter is real — it makes HTTP calls, classifies provider errors, writes audit rows and reads two
// response shapes — and every one of those is exercised here against an `httptest` server. **No network
// and no key**, which is why FR-080's 生成适配 half could be finished on a host with no paid-provider
// authorisation: what needs authorisation is a call to a REAL vendor, not the protocol handling.
//
// # What each test is for
//
//   - THE BYTES MUST SURVIVE. The inline shape returns base64 that the pipeline commits; a round trip
//     that lost them would store a file the user cannot hear.
//   - AN EMPTY BODY MUST BE REFUSED. A provider that answers 200 with nothing would otherwise produce a
//     zero-byte file marked succeeded — the failure a user cannot see.
//   - THE TYPE IS THE PROVIDER'S, filled in only when absent. A stated type the allowlist refuses is
//     refused rather than rewritten, because rewriting it would be a way to smuggle one past the store.
//   - THE URL SHAPE MUST SURVIVE TOO, with NO MIME: the download path sniffs the bytes rather than
//     trusting a claim about a file the provider did not serve.
//   - THE AUDIT WRITES `CapabilityAudio`, which nothing in this build had ever written.

// audioTestServer is a fake speech provider whose response the tests set.
type audioTestServer struct {
	server *httptest.Server
	// bodies records every request body, so a test can assert what was sent.
	bodies []string
}

// newAudioTestServer answers with the given content type and payload.
func newAudioTestServer(t *testing.T, contentType string, payload []byte) *audioTestServer {
	t.Helper()
	fake := &audioTestServer{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		fake.bodies = append(fake.bodies, string(body))
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// newAudioTestAdapter builds the adapter over a server, with a secret.
func newAudioTestAdapter(t *testing.T, server *httptest.Server, secret []byte) (*OpenAIAudioAdapter, *captureAudit) {
	t.Helper()
	config := provider.Config{
		ID:            "prov-audio",
		Kind:          provider.KindOpenAICompatible,
		DisplayName:   "audio test",
		BaseURL:       server.URL + "/v1",
		SecretRef:     provider.SecretRefValue("prov-audio"),
		LocalApproved: true,
		Enabled:       true,
		Revision:      1,
	}
	audit := &captureAudit{}
	registry := NewRegistry(&staticConfigs{config: config}, &staticSecret{value: secret}, audit)
	adapter := NewOpenAIAudioAdapter(registry)
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

func audioRequest() appjobs.AudioRequest {
	return appjobs.AudioRequest{
		JobID:      "job-audio-1",
		ProviderID: "prov-audio",
		Model:      "tts-1",
		Text:       "夜里的渡轮靠岸了",
		Voice:      "alloy",
		Format:     "mp3",
		Speed:      "1.0",
	}
}

// TestTheSpeechProtocolRunsFromRequestToBytes is the end-to-end path.
func TestTheSpeechProtocolRunsFromRequestToBytes(t *testing.T) {
	wav := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	fake := newAudioTestServer(t, "audio/wave", wav)
	adapter, audit := newAudioTestAdapter(t, fake.server, []byte("test-secret-value"))

	outcome, err := adapter.GenerateAudio(context.Background(), audioRequest())
	if err != nil {
		t.Fatalf("GenerateAudio: %v", err)
	}
	if outcome.URL != "" {
		t.Fatalf("the bytes shape returned a URL: %q", outcome.URL)
	}
	if outcome.MIMEType != "audio/wave" {
		t.Fatalf("the result is typed %q", outcome.MIMEType)
	}
	decoded, err := base64.StdEncoding.DecodeString(outcome.Data)
	if err != nil {
		t.Fatalf("the payload is not base64: %v", err)
	}
	if string(decoded) != string(wav) {
		t.Fatalf("the bytes did not survive the round trip: %q", decoded)
	}

	// WHAT WAS SENT: the text, the model, and the three optional fields the caller stated. A request
	// that dropped the voice would render in the provider's default, which is a different performance
	// from the one the user configured.
	if len(fake.bodies) != 1 {
		t.Fatalf("the server received %d bodies", len(fake.bodies))
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(fake.bodies[0]), &sent); err != nil {
		t.Fatalf("the request body is not JSON: %s", fake.bodies[0])
	}
	for field, want := range map[string]string{
		"model": "tts-1", "input": "夜里的渡轮靠岸了", "voice": "alloy",
		"response_format": "mp3", "speed": "1.0",
	} {
		if sent[field] != want {
			t.Fatalf("the request field %q is %v, want %q", field, sent[field], want)
		}
	}

	// THE AUDIT, with the capability the column has admitted since migration 000003 and nothing had
	// ever written.
	if len(audit.records) != 1 {
		t.Fatalf("%d audit rows for one call", len(audit.records))
	}
	if audit.records[0].Capability != provider.CapabilityAudio {
		t.Fatalf("the audit names the capability %q", audit.records[0].Capability)
	}
	if strings.Contains(audit.records[0].ErrorCode, "test-secret-value") {
		t.Fatal("the audit carries the secret")
	}
}

// TestAnAbsentVoiceOrSpeedIsNotSent is the omission rule.
//
// A voice the caller did not choose is a provider's default to apply; sending an empty string would ask
// for a voice with no name. The same reasoning the video adapter's optional fields carry.
func TestAnAbsentVoiceOrSpeedIsNotSent(t *testing.T) {
	fake := newAudioTestServer(t, "audio/wave", []byte("RIFF\x00\x00\x00\x00WAVEfmt "))
	adapter, _ := newAudioTestAdapter(t, fake.server, []byte("test-secret-value"))
	request := audioRequest()
	request.Voice, request.Format, request.Speed = "", "  ", ""
	if _, err := adapter.GenerateAudio(context.Background(), request); err != nil {
		t.Fatalf("GenerateAudio: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(fake.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"voice", "response_format", "speed"} {
		// The assertion is the ABSENCE of the key, not a nil value: a key present with an empty string
		// would be a request the provider reads as "a voice whose name is empty".
		if _, present := sent[field]; present {
			t.Fatalf("the request carries %q with nothing stated: %v", field, sent[field])
		}
	}
	if sent["input"] != "夜里的渡轮靠岸了" {
		t.Fatalf("the text was dropped: %v", sent)
	}
}

// TestAnEmptyBodyIsRefused is the failure a user cannot see.
//
// A provider that answers 200 with nothing would otherwise produce a zero-byte file marked succeeded.
func TestAnEmptyBodyIsRefused(t *testing.T) {
	fake := newAudioTestServer(t, "audio/wave", nil)
	adapter, _ := newAudioTestAdapter(t, fake.server, []byte("test-secret-value"))
	if _, err := adapter.GenerateAudio(context.Background(), audioRequest()); err == nil {
		t.Fatal("an empty body was accepted as speech")
	} else if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategoryResponseInvalid {
		t.Fatalf("the refusal is %v rather than a response-invalid error", err)
	}
}

// TestATypeTheProviderDidNotNameBecomesMP3 covers the fallback, and where it does NOT apply.
//
// # The first version of this test asserted the wrong thing, and the failure was the fixture's
//
// It sent no `Content-Type` and expected the fallback. `httptest` SNAPSHOTS the body and sets the header
// itself, so the adapter received a stated `audio/wave` and took the audio branch — the test was
// asserting a response the fixture never produced. The corrected version separates the two cases: a
// body the server sniffs (a real type, passed through) and a body whose type is only in the JSON shape
// (no type, so the fallback applies).
func TestATypeTheProviderDidNotNameBecomesMP3(t *testing.T) {
	// A RIFF body with no explicit header: the server's own sniffer names it, and the adapter passes that
	// through rather than substituting its own opinion.
	riffHeader := append([]byte("RIFF"), 0x00, 0x00, 0x00, 0x00)
	riffHeader = append(riffHeader, []byte("WAVEfmt ")...)
	sniffed := newAudioTestServer(t, "", riffHeader)
	adapter, _ := newAudioTestAdapter(t, sniffed.server, []byte("test-secret-value"))
	outcome, err := adapter.GenerateAudio(context.Background(), audioRequest())
	if err != nil {
		t.Fatalf("GenerateAudio: %v", err)
	}
	if outcome.MIMEType != "audio/wave" {
		t.Fatalf("a sniffed response is typed %q", outcome.MIMEType)
	}

	// And the fallback's real case: a JSON document states no media type, so the adapter fills one in —
	// see `TestTheJSONInlineShapeIsAccepted`, which asserts it is `audio/mpeg`.
}

// TestTheURLShapeReturnsNoMIME is the download path's contract.
//
// `MIMEType` is left EMPTY for the URL case because the download path SNIFFS the bytes rather than
// trusting a claim about a file the provider did not serve.
func TestTheURLShapeReturnsNoMIME(t *testing.T) {
	document, _ := json.Marshal(map[string]string{"url": "https://cdn.example.com/speech.mp3"})
	fake := newAudioTestServer(t, "application/json", document)
	adapter, _ := newAudioTestAdapter(t, fake.server, []byte("test-secret-value"))
	outcome, err := adapter.GenerateAudio(context.Background(), audioRequest())
	if err != nil {
		t.Fatalf("GenerateAudio: %v", err)
	}
	if outcome.URL != "https://cdn.example.com/speech.mp3" {
		t.Fatalf("the URL is %q", outcome.URL)
	}
	if outcome.MIMEType != "" {
		t.Fatalf("the URL shape filled in a MIME it did not receive: %q", outcome.MIMEType)
	}
	if outcome.Data != "" {
		t.Fatalf("the URL shape also carried data: %q", outcome.Data)
	}
}

// TestTheJSONInlineShapeIsAccepted covers the second spelling of the inline result.
func TestTheJSONInlineShapeIsAccepted(t *testing.T) {
	payload := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	document, _ := json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(payload)})
	fake := newAudioTestServer(t, "application/json; charset=utf-8", document)
	adapter, _ := newAudioTestAdapter(t, fake.server, []byte("test-secret-value"))
	outcome, err := adapter.GenerateAudio(context.Background(), audioRequest())
	if err != nil {
		t.Fatalf("GenerateAudio: %v", err)
	}
	if outcome.Data == "" {
		t.Fatal("the inline document produced no data")
	}
	// The type is filled in, because a JSON document does not state one and the result store's allowlist
	// needs a name the sniffer can produce.
	if outcome.MIMEType != "audio/mpeg" {
		t.Fatalf("the inline document is typed %q", outcome.MIMEType)
	}
	decoded, err := base64.StdEncoding.DecodeString(outcome.Data)
	if err != nil || string(decoded) != string(payload) {
		t.Fatalf("the inline bytes did not survive: %q", outcome.Data)
	}
}

// TestAJSONDocumentNamingNothingIsRefused keeps a succeeded job from producing no audio at all.
func TestAJSONDocumentNamingNothingIsRefused(t *testing.T) {
	for _, body := range []string{`{}`, `{"url":""}`, `{"data":"   "}`, `not json`, ``} {
		fake := newAudioTestServer(t, "application/json", []byte(body))
		adapter, _ := newAudioTestAdapter(t, fake.server, []byte("test-secret-value"))
		if _, err := adapter.GenerateAudio(context.Background(), audioRequest()); err == nil {
			t.Fatalf("a document of %q produced a result", body)
		}
	}
}

// TestAnUnexpectedContentTypeIsRefused keeps an HTML error page from being stored as speech.
func TestAnUnexpectedContentTypeIsRefused(t *testing.T) {
	fake := newAudioTestServer(t, "text/html", []byte("<html>gateway timeout</html>"))
	adapter, _ := newAudioTestAdapter(t, fake.server, []byte("test-secret-value"))
	if _, err := adapter.GenerateAudio(context.Background(), audioRequest()); err == nil {
		t.Fatal("an HTML body was accepted as speech")
	}
}

// TestTheSpeechErrorTaxonomyIsPreserved is what decides whether a job retries.
//
// The category is not cosmetic: `IsRetriable` reads it, and the job manager retries `remote_transient`
// and `rate_limited` while refusing `unauthorized` and `invalid_input`.
func TestTheSpeechErrorTaxonomyIsPreserved(t *testing.T) {
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
		adapter, _ := newAudioTestAdapter(t, server, []byte("test-secret-value"))
		_, err := adapter.GenerateAudio(context.Background(), audioRequest())
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

// TestAnEmptyTextIsRefusedBeforeAnyCall is the one input a provider cannot be asked about.
func TestAnEmptyTextIsRefusedBeforeAnyCall(t *testing.T) {
	var reached bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	defer server.Close()
	adapter, _ := newAudioTestAdapter(t, server, []byte("test-secret-value"))
	for _, text := range []string{"", "   ", "\t"} {
		request := audioRequest()
		request.Text = text
		if _, err := adapter.GenerateAudio(context.Background(), request); err == nil {
			t.Fatalf("the text %q was accepted", text)
		}
	}
	if reached {
		t.Fatal("a textless request reached the provider")
	}
}

// TestTheAudioAdapterRefusesWithoutConfigOrSecret is the fail-closed rule.
func TestTheAudioAdapterRefusesWithoutConfigOrSecret(t *testing.T) {
	ctx := context.Background()
	var bare *OpenAIAudioAdapter
	if _, err := bare.GenerateAudio(ctx, audioRequest()); err == nil {
		t.Fatal("an unattached adapter generated audio")
	}
	reached := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	adapter, _ := newAudioTestAdapter(t, server, nil)
	if _, err := adapter.GenerateAudio(ctx, audioRequest()); err == nil {
		t.Fatal("a request without a secret was accepted")
	}
	if reached {
		t.Fatal("the request reached the provider without a secret")
	}
}

// TestAnOversizedAudioResultIsRefused is the transport-ceiling rule.
//
// 9 MiB: past this adapter's bound and UNDER the guarded client's 10 MiB response ceiling, which is what
// makes the adapter's own check the thing that refuses. A bound the transport cannot reach is a dead
// rule, and that is the correction WP-26 had to make to its own.
func TestAnOversizedAudioResultIsRefused(t *testing.T) {
	oversized := make([]byte, maxAudioInlineBytes+16)
	copy(oversized, []byte("RIFF"))
	fake := newAudioTestServer(t, "audio/wave", oversized)
	adapter, _ := newAudioTestAdapter(t, fake.server, []byte("test-secret-value"))
	if _, err := adapter.GenerateAudio(context.Background(), audioRequest()); err == nil {
		t.Fatal("a result past the bound was accepted")
	}
}

// TestTheRegistryResolvesTheRealAudioAdapterForAnOpenAIProvider is the reachability assertion.
//
// It is the one that would have failed before this package: `AudioPortFor` returned `unsupported` for
// every kind but `mock_media`, so the adapter could exist and no command could reach it. The mock must
// still resolve for its own kind, which is asserted in the same test because a change that made the real
// adapter reachable by breaking the mock would pass half of it.
func TestTheRegistryResolvesTheRealAudioAdapterForAnOpenAIProvider(t *testing.T) {
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
	registry.WithOpenAIAudioAdapter(NewOpenAIAudioAdapter(registry))

	real, err := registry.AudioPortFor(ctx, "prov-real")
	if err != nil {
		t.Fatalf("the real adapter did not resolve for an OpenAI provider: %v", err)
	}
	if _, ok := real.(*OpenAIAudioAdapter); !ok {
		t.Fatalf("the OpenAI provider resolved to %T", real)
	}
	mockPort, err := registry.AudioPortFor(ctx, "prov-mock")
	if err != nil {
		t.Fatalf("the mock did not resolve for its own kind: %v", err)
	}
	if _, ok := mockPort.(*MockAudioAdapter); !ok {
		t.Fatalf("the mock kind resolved to %T", mockPort)
	}
	// A DISABLED provider still refuses, which is the configuration rule rather than the kind rule.
	disabled := openai
	disabled.ID, disabled.Enabled = "prov-off", false
	registry2 := NewRegistry(&mapConfigs{configs: map[string]provider.Config{"prov-off": disabled}},
		&staticSecret{value: []byte("s")}, &captureAudit{})
	registry2.WithOpenAIAudioAdapter(NewOpenAIAudioAdapter(registry2))
	if _, err := registry2.AudioPortFor(ctx, "prov-off"); err == nil {
		t.Fatal("a disabled provider resolved an audio port")
	}
	// And an UNREGISTERED adapter refuses rather than silently doing nothing.
	registry3 := NewRegistry(&staticConfigs{config: openai}, &staticSecret{value: []byte("s")}, &captureAudit{})
	if _, err := registry3.AudioPortFor(ctx, "prov-real"); err == nil {
		t.Fatal("an unregistered adapter resolved an audio port")
	}
}
