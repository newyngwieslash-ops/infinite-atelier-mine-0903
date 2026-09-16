package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// newImageTestAdapter wires an OpenAI image adapter against an httptest server.
func newImageTestAdapter(t *testing.T, server *httptest.Server, secret []byte, localApprove bool) (*OpenAIImageAdapter, *captureAudit) {
	t.Helper()
	config := provider.Config{
		ID:            "prov-img",
		Kind:          provider.KindOpenAICompatible,
		DisplayName:   "image test",
		BaseURL:       server.URL + "/v1",
		SecretRef:     provider.SecretRefValue("prov-img"),
		LocalApproved: localApprove,
		Enabled:       true,
		Revision:      1,
	}
	audit := &captureAudit{}
	registry := NewRegistry(&staticConfigs{config: config}, &staticSecret{value: secret}, audit)
	adapter := NewOpenAIImageAdapter(registry)
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

func imageRequest() appjobs.ImageRequest {
	return appjobs.ImageRequest{
		JobID:      "job-1",
		ProviderID: "prov-img",
		Model:      "gpt-image-1",
		Prompt:     "a cat",
		Count:      2,
		Size:       "1024x1024",
	}
}

func TestOpenAIImageGenerateInlineBase64(t *testing.T) {
	pngBody := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\npayload"))
	var sawAuth, sawPath string
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + pngBody + `"}]}`))
	}))
	defer server.Close()

	adapter, audit := newImageTestAdapter(t, server, []byte("sk-image-secret"), true)
	outcome, err := adapter.Generate(context.Background(), imageRequest())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if sawAuth != "Bearer sk-image-secret" {
		t.Fatalf("Authorization = %q", sawAuth)
	}
	if sawPath != "/v1/images/generations" {
		t.Fatalf("path = %q", sawPath)
	}
	if sawBody["model"] != "gpt-image-1" || sawBody["prompt"] != "a cat" {
		t.Fatalf("body = %+v", sawBody)
	}
	if count, ok := sawBody["n"].(float64); !ok || int(count) != 2 {
		t.Fatalf("count not forwarded: %+v", sawBody)
	}
	if len(outcome.Results) != 1 || outcome.Results[0].Data == "" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(audit.records) != 1 || audit.records[0].Capability != provider.CapabilityImage {
		t.Fatalf("audit = %+v", audit.records)
	}
	encoded, _ := json.Marshal(audit.records)
	if strings.Contains(string(encoded), "sk-image-secret") {
		t.Fatal("audit leaked the secret")
	}
}

func TestOpenAIImageGenerateReturnsRemoteURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"url":"https://cdn.example.com/result.png"}]}`))
	}))
	defer server.Close()

	adapter, _ := newImageTestAdapter(t, server, []byte("sk-test"), true)
	outcome, err := adapter.Generate(context.Background(), imageRequest())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// A URL result is surfaced for the runner to download under policy; it is
	// never handed straight to the UI.
	if len(outcome.RemoteURLs) != 1 || outcome.RemoteURLs[0] != "https://cdn.example.com/result.png" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(outcome.Results) != 0 {
		t.Fatalf("inline results unexpected: %+v", outcome.Results)
	}
}

func TestOpenAIImageEditUsesMultipart(t *testing.T) {
	reference := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nref"))
	var contentType, path string
	var bodySeen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		bodySeen = string(raw)
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + reference + `"}]}`))
	}))
	defer server.Close()

	adapter, _ := newImageTestAdapter(t, server, []byte("sk-test"), true)
	request := imageRequest()
	request.References = []appjobs.ImageInput{{MIMEType: "image/png", Data: reference}}
	outcome, err := adapter.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("Generate(edit): %v", err)
	}
	if path != "/v1/images/edits" {
		t.Fatalf("path = %q", path)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		t.Fatalf("content type = %q", contentType)
	}
	if !strings.Contains(bodySeen, `name="image"`) {
		t.Fatalf("reference part missing: %s", bodySeen[:minInt(200, len(bodySeen))])
	}
	// The user's filename never appears; the part uses a neutral name.
	if !strings.Contains(bodySeen, "reference-0.png") {
		t.Fatal("neutral filename not used for the reference part")
	}
	if len(outcome.Results) != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestOpenAIImageRejectsEmptyPrompt(t *testing.T) {
	adapter, _ := newImageTestAdapter(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})), []byte("sk"), true)
	request := imageRequest()
	request.Prompt = "   "
	if _, err := adapter.Generate(context.Background(), request); err == nil {
		t.Fatal("empty prompt accepted")
	}
}

func TestOpenAIImageErrorStatuses(t *testing.T) {
	cases := []struct {
		status       int
		wantCategory provider.ErrorCategory
	}{
		{status: http.StatusUnauthorized, wantCategory: provider.CategoryUnauthorized},
		{status: http.StatusTooManyRequests, wantCategory: provider.CategoryRateLimited},
		{status: http.StatusInternalServerError, wantCategory: provider.CategoryRemoteTransient},
		{status: http.StatusBadRequest, wantCategory: provider.CategoryInvalidInput},
	}
	for _, testCase := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(testCase.status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
		}))
		adapter, audit := newImageTestAdapter(t, server, []byte("sk-test"), true)
		_, err := adapter.Generate(context.Background(), imageRequest())
		server.Close()
		if err == nil {
			t.Fatalf("status %d accepted", testCase.status)
		}
		providerErr, ok := provider.AsProviderError(err)
		if !ok || providerErr.Category != testCase.wantCategory {
			t.Fatalf("status %d mapped to %v, want %q", testCase.status, err, testCase.wantCategory)
		}
		if len(audit.records) != 1 || audit.records[0].Status != provider.StatusFailed {
			t.Fatalf("failure not audited: %+v", audit.records)
		}
	}
}

func TestOpenAIImageRejectsBadJSONAndEmptyData(t *testing.T) {
	for name, payload := range map[string]string{
		"bad json":       `{"data": not-json`,
		"empty data":     `{"data":[]}`,
		"provider error": `{"error":{"message":"refused"}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(payload))
		}))
		adapter, _ := newImageTestAdapter(t, server, []byte("sk-test"), true)
		if _, err := adapter.Generate(context.Background(), imageRequest()); err == nil {
			t.Fatalf("%s accepted", name)
		}
		server.Close()
	}
}

func TestOpenAIImageFailsClosedWithoutSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("server reached without a secret")
	}))
	defer server.Close()
	config := provider.Config{ID: "prov-img", Kind: provider.KindOpenAICompatible, BaseURL: server.URL + "/v1", LocalApproved: true}
	secrets := &staticSecret{err: provider.NewSecurityError()}
	registry := NewRegistry(&staticConfigs{config: config}, secrets, &captureAudit{})
	adapter := NewOpenAIImageAdapter(registry)
	adapter.clientFactory = func(provider.Config) (*phttp.Client, error) {
		normalized, _ := provider.ValidateBaseURL(server.URL + "/v1")
		return phttp.NewClient(phttp.Policy{Host: normalized.Host, Port: normalized.Port, Scheme: normalized.Scheme, AllowLocal: true}, phttp.NewNetResolver(), phttp.Limits{}), nil
	}
	if _, err := adapter.Generate(context.Background(), imageRequest()); err == nil {
		t.Fatal("missing secret accepted")
	}
}

// Gemini adapter tests.

func newGeminiTestAdapter(t *testing.T, server *httptest.Server, secret []byte) (*GeminiImageAdapter, *captureAudit) {
	t.Helper()
	config := provider.Config{
		ID:            "prov-gem",
		Kind:          provider.KindGeminiCompatible,
		DisplayName:   "gemini test",
		BaseURL:       server.URL,
		SecretRef:     provider.SecretRefValue("prov-gem"),
		LocalApproved: true,
		Enabled:       true,
		Revision:      1,
	}
	audit := &captureAudit{}
	registry := NewRegistry(&staticConfigs{config: config}, &staticSecret{value: secret}, audit)
	adapter := NewGeminiImageAdapter(registry)
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

func TestGeminiImageGenerateInlineData(t *testing.T) {
	pngBody := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\ngemini"))
	var sawKey, sawPath string
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawKey = r.Header.Get("x-goog-api-key")
		sawPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + pngBody + `"}}]}}]}`))
	}))
	defer server.Close()

	adapter, audit := newGeminiTestAdapter(t, server, []byte("gemini-secret"))
	outcome, err := adapter.Generate(context.Background(), appjobs.ImageRequest{
		JobID:      "job-1",
		ProviderID: "prov-gem",
		Model:      "gemini-2-image",
		Prompt:     "a dog",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if sawKey != "gemini-secret" {
		t.Fatalf("api key header = %q", sawKey)
	}
	if !strings.Contains(sawPath, "gemini-2-image:generateContent") {
		t.Fatalf("path = %q", sawPath)
	}
	if len(outcome.Results) != 1 || outcome.Results[0].MIMEType != "image/png" {
		t.Fatalf("outcome = %+v", outcome)
	}
	// The request asks for image output.
	encoded, _ := json.Marshal(sawBody)
	if !strings.Contains(string(encoded), "IMAGE") {
		t.Fatalf("responseModalities missing: %s", encoded)
	}
	if len(audit.records) != 1 || audit.records[0].Capability != provider.CapabilityImage {
		t.Fatalf("audit = %+v", audit.records)
	}
}

func TestGeminiImageRejectsBadPayloadAndStatus(t *testing.T) {
	badPayload := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer badPayload.Close()
	adapter, _ := newGeminiTestAdapter(t, badPayload, []byte("k"))
	if _, err := adapter.Generate(context.Background(), appjobs.ImageRequest{ProviderID: "prov-gem", Model: "m", Prompt: "p"}); err == nil {
		t.Fatal("empty candidates accepted")
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer failing.Close()
	adapter2, _ := newGeminiTestAdapter(t, failing, []byte("k"))
	_, err := adapter2.Generate(context.Background(), appjobs.ImageRequest{ProviderID: "prov-gem", Model: "m", Prompt: "p"})
	if err == nil {
		t.Fatal("403 accepted")
	}
	if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategoryForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

// Registry routing tests.

func TestRegistryRoutesKindsToImageAdapters(t *testing.T) {
	openaiConfig := provider.Config{ID: "a", Kind: provider.KindOpenAICompatible, BaseURL: "https://api.example.com/v1"}
	registry := NewRegistry(&staticConfigs{config: openaiConfig}, &staticSecret{}, &captureAudit{})
	port, err := registry.ImagePortFor(context.Background(), "a")
	if err != nil {
		t.Fatalf("openai image port: %v", err)
	}
	if _, ok := port.(*OpenAIImageAdapter); !ok {
		t.Fatalf("wrong adapter type: %T", port)
	}

	geminiConfig := provider.Config{ID: "b", Kind: provider.KindGeminiCompatible, BaseURL: "https://generativelanguage.googleapis.com"}
	registry = NewRegistry(&staticConfigs{config: geminiConfig}, &staticSecret{}, &captureAudit{})
	port, err = registry.ImagePortFor(context.Background(), "b")
	if err != nil {
		t.Fatalf("gemini image port: %v", err)
	}
	if _, ok := port.(*GeminiImageAdapter); !ok {
		t.Fatalf("wrong adapter type: %T", port)
	}
}

func TestRegistryMediaPortsFailClosedByDefault(t *testing.T) {
	// A provider configured as a real HTTP kind must never resolve to a media
	// mock: that would fabricate a result for a provider that was never called.
	realConfig := provider.Config{
		ID:            "real-1",
		Kind:          provider.KindOpenAICompatible,
		DisplayName:   "real",
		BaseURL:       "https://api.example.com/v1",
		SecretRef:     provider.SecretRefValue("real-1"),
		Enabled:       true,
		LocalApproved: false,
		Revision:      1,
	}
	registry := NewRegistry(&staticConfigs{config: realConfig}, &staticSecret{}, &captureAudit{})
	registry.WithMediaAdapters(NewMockVideoAdapter(), NewMockAudioAdapter())
	if _, err := registry.VideoPortFor(context.Background(), "real-1"); err == nil {
		t.Fatal("a real provider kind resolved to the video mock")
	}
	if _, err := registry.AudioPortFor(context.Background(), "real-1"); err == nil {
		t.Fatal("a real provider kind resolved to the audio mock")
	}
	// Unknown providers fail closed.
	if _, err := registry.VideoPortFor(context.Background(), "missing"); err == nil {
		t.Fatal("unknown provider resolved to the video mock")
	}
	// A disabled provider is refused even for the mock kind.
	disabled := provider.Config{ID: "mock-1", Kind: provider.KindMockMedia, DisplayName: "mock", BaseURL: "https://mock.invalid", Enabled: false}
	disabledRegistry := NewRegistry(&staticConfigs{config: disabled}, &staticSecret{}, &captureAudit{})
	disabledRegistry.WithMediaAdapters(NewMockVideoAdapter(), NewMockAudioAdapter())
	if _, err := disabledRegistry.VideoPortFor(context.Background(), "mock-1"); err == nil {
		t.Fatal("disabled provider resolved to a media adapter")
	}

	// The mock kind is reachable only when it is explicitly configured, which
	// is how tests and local pipeline exercises opt in.
	mockConfig := provider.Config{ID: "mock-1", Kind: provider.KindMockMedia, DisplayName: "mock", BaseURL: "https://mock.invalid", Enabled: true}
	mockRegistry := NewRegistry(&staticConfigs{config: mockConfig}, &staticSecret{}, &captureAudit{})
	mockRegistry.WithMediaAdapters(NewMockVideoAdapter(), NewMockAudioAdapter())
	if _, err := mockRegistry.VideoPortFor(context.Background(), "mock-1"); err != nil {
		t.Fatalf("explicitly configured mock video provider rejected: %v", err)
	}
	if _, err := mockRegistry.AudioPortFor(context.Background(), "mock-1"); err != nil {
		t.Fatalf("explicitly configured mock audio provider rejected: %v", err)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
