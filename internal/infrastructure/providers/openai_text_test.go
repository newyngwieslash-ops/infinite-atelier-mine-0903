package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

type staticConfigs struct {
	config provider.Config
}

func (s *staticConfigs) GetConfig(_ context.Context, id string) (provider.Config, error) {
	if s.config.ID != id {
		return provider.Config{}, provider.NewConfigurationError()
	}
	return s.config, nil
}

type staticSecret struct {
	value []byte
	err   error
	seen  int
}

func (s *staticSecret) ResolveInternal(_ context.Context, _ string) ([]byte, error) {
	s.seen++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]byte, len(s.value))
	copy(out, s.value)
	return out, nil
}

type captureAudit struct {
	records []provider.RequestRecord
}

func (a *captureAudit) SaveRequestRecord(_ context.Context, record provider.RequestRecord) error {
	a.records = append(a.records, record)
	return nil
}

// newTestAdapter wires an adapter against an httptest server with an
// AllowLocal policy (the server binds 127.0.0.1).
func newTestAdapter(t *testing.T, server *httptest.Server, secret []byte) (*OpenAITextAdapter, *captureAudit, *staticSecret) {
	t.Helper()
	config := provider.Config{
		ID:            "prov-1",
		Kind:          provider.KindOpenAICompatible,
		DisplayName:   "test",
		BaseURL:       server.URL + "/v1",
		SecretRef:     provider.SecretRefValue("prov-1"),
		LocalApproved: true,
		Enabled:       true,
		Revision:      1,
	}
	secrets := &staticSecret{value: secret}
	audit := &captureAudit{}
	registry := NewRegistry(&staticConfigs{config: config}, secrets, audit)
	adapter := NewOpenAITextAdapter(registry)
	adapter.clientFactory = func(config provider.Config) (*phttp.Client, error) {
		normalized, ok := provider.ValidateBaseURL(config.BaseURL)
		if !ok {
			return nil, provider.NewConfigurationError()
		}
		return phttp.NewClient(phttp.Policy{
			Host:       normalized.Host,
			Port:       normalized.Port,
			Scheme:     normalized.Scheme,
			AllowLocal: true,
		}, phttp.NewNetResolver(), phttp.Limits{}), nil
	}
	return adapter, audit, secrets
}

func testRequest(stream bool) appproviders.TextRequest {
	return appproviders.TextRequest{
		ProviderID: "prov-1",
		Model:      "gpt-test",
		Messages:   []appproviders.TextMessage{{Role: "user", Content: "hello"}},
		Stream:     stream,
	}
}

func TestGenerateSendsAuthorizationFromGo(t *testing.T) {
	var sawAuth string
	var sawBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		sawBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-test","choices":[{"message":{"content":"hi there"}}]}`))
	}))
	defer server.Close()

	adapter, audit, secrets := newTestAdapter(t, server, []byte("sk-test-secret-123"))
	result, err := adapter.Generate(context.Background(), testRequest(false))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Content != "hi there" {
		t.Fatalf("content = %q", result.Content)
	}
	if sawAuth != "Bearer sk-test-secret-123" {
		t.Fatalf("Authorization = %q", sawAuth)
	}
	if secrets.seen == 0 {
		t.Fatal("secret was not resolved")
	}
	if !strings.Contains(string(sawBody), `"model":"gpt-test"`) {
		t.Fatalf("body = %s", sawBody)
	}
	// Audit record carries no secret and no headers.
	encoded, _ := json.Marshal(audit.records)
	if strings.Contains(string(encoded), "sk-test-secret-123") {
		t.Fatal("audit record leaked the secret")
	}
	if len(audit.records) != 1 || audit.records[0].Status != provider.StatusSucceeded {
		t.Fatalf("audit records = %+v", audit.records)
	}
}

func TestGenerateHandlesErrorStatuses(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		retryAfter   string
		wantCategory provider.ErrorCategory
	}{
		{name: "401 unauthorized", status: http.StatusUnauthorized, wantCategory: provider.CategoryUnauthorized},
		{name: "403 forbidden", status: http.StatusForbidden, wantCategory: provider.CategoryForbidden},
		{name: "429 rate limited", status: http.StatusTooManyRequests, retryAfter: "7", wantCategory: provider.CategoryRateLimited},
		{name: "500 transient", status: http.StatusInternalServerError, wantCategory: provider.CategoryRemoteTransient},
		{name: "503 transient", status: http.StatusServiceUnavailable, wantCategory: provider.CategoryRemoteTransient},
		{name: "400 invalid", status: http.StatusBadRequest, wantCategory: provider.CategoryInvalidInput},
		{name: "404 permanent", status: http.StatusNotFound, wantCategory: provider.CategoryRemotePermanent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
			}))
			defer server.Close()
			adapter, audit, _ := newTestAdapter(t, server, []byte("sk-test"))
			_, err := adapter.Generate(context.Background(), testRequest(false))
			if err == nil {
				t.Fatal("expected error")
			}
			providerErr, ok := provider.AsProviderError(err)
			if !ok {
				t.Fatalf("not a provider error: %T %v", err, err)
			}
			if providerErr.Category != tc.wantCategory {
				t.Fatalf("category = %q, want %q", providerErr.Category, tc.wantCategory)
			}
			if tc.retryAfter != "" {
				if providerErr.RetryAfter != 7*time.Second {
					t.Fatalf("retry-after = %v, want 7s", providerErr.RetryAfter)
				}
				if !providerErr.Retriable {
					t.Fatal("429 must be retriable")
				}
			}
			if len(audit.records) != 1 || audit.records[0].Status != provider.StatusFailed {
				t.Fatalf("audit = %+v", audit.records)
			}
			encoded, _ := json.Marshal(audit.records)
			if strings.Contains(string(encoded), "sk-test") {
				t.Fatal("failed audit leaked the secret")
			}
		})
	}
}

func TestGenerateRejectsBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices": not-json`))
	}))
	defer server.Close()
	adapter, _, _ := newTestAdapter(t, server, []byte("sk-test"))
	_, err := adapter.Generate(context.Background(), testRequest(false))
	if err == nil {
		t.Fatal("bad JSON accepted")
	}
	providerErr, ok := provider.AsProviderError(err)
	if !ok || providerErr.Category != provider.CategoryResponseInvalid {
		t.Fatalf("expected response_invalid, got %v", err)
	}
}

func TestGenerateRejectsEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()
	adapter, _, _ := newTestAdapter(t, server, []byte("sk-test"))
	_, err := adapter.Generate(context.Background(), testRequest(false))
	if err == nil {
		t.Fatal("empty choices accepted")
	}
}

func TestGenerateFailsClosedWithoutSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("server should not be reached without a secret")
	}))
	defer server.Close()
	config := provider.Config{ID: "prov-1", Kind: provider.KindOpenAICompatible, BaseURL: server.URL + "/v1", LocalApproved: true}
	secrets := &staticSecret{err: &provider.Error{Category: provider.CategorySecurity, SafeMessage: "unavailable"}}
	registry := NewRegistry(&staticConfigs{config: config}, secrets, &captureAudit{})
	adapter := NewOpenAITextAdapter(registry)
	adapter.clientFactory = func(provider.Config) (*phttp.Client, error) {
		normalized, _ := provider.ValidateBaseURL(server.URL + "/v1")
		return phttp.NewClient(phttp.Policy{Host: normalized.Host, Port: normalized.Port, Scheme: normalized.Scheme, AllowLocal: true}, phttp.NewNetResolver(), phttp.Limits{}), nil
	}
	_, err := adapter.Generate(context.Background(), testRequest(false))
	if err == nil {
		t.Fatal("missing secret accepted")
	}
}

func TestStreamDeliversSSEDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	adapter, audit, _ := newTestAdapter(t, server, []byte("sk-test"))
	var deltas []string
	var done appproviders.TextResult
	var streamErr error
	sink := &testSink{
		onDelta: func(d string) { deltas = append(deltas, d) },
		onDone:  func(r appproviders.TextResult) { done = r },
		onError: func(err error) { streamErr = err },
	}
	if err := adapter.Stream(context.Background(), testRequest(true), sink); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if streamErr != nil {
		t.Fatalf("sink error: %v", streamErr)
	}
	if strings.Join(deltas, "") != "Hello" {
		t.Fatalf("deltas = %v", deltas)
	}
	if done.Content != "Hello" {
		t.Fatalf("done content = %q", done.Content)
	}
	if len(audit.records) != 1 || audit.records[0].Status != provider.StatusSucceeded {
		t.Fatalf("audit = %+v", audit.records)
	}
}

func TestStreamRejectsTruncatedStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
		// No [DONE] terminator: the stream is interrupted.
	}))
	defer server.Close()
	adapter, _, _ := newTestAdapter(t, server, []byte("sk-test"))
	sink := &testSink{}
	err := adapter.Stream(context.Background(), testRequest(true), sink)
	if err == nil {
		t.Fatal("truncated stream accepted as success")
	}
	providerErr, ok := provider.AsProviderError(err)
	if !ok || providerErr.Category != provider.CategoryResponseInvalid {
		t.Fatalf("expected response_invalid, got %v", err)
	}
	if sink.err == nil {
		t.Fatal("sink was not notified of the failure")
	}
}

func TestStreamRejectsMalformedChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {broken\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	adapter, _, _ := newTestAdapter(t, server, []byte("sk-test"))
	err := adapter.Stream(context.Background(), testRequest(true), &testSink{})
	if err == nil {
		t.Fatal("malformed chunk accepted")
	}
}

func TestStreamCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"A\"}}]}\n\n"))
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)

	adapter, audit, _ := newTestAdapter(t, server, []byte("sk-test"))
	ctx, cancel := context.WithCancel(context.Background())
	sink := &testSink{}
	done := make(chan error, 1)
	go func() {
		done <- adapter.Stream(ctx, testRequest(true), sink)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled stream returned nil")
		}
		providerErr, ok := provider.AsProviderError(err)
		if !ok || providerErr.Category != provider.CategoryCancelled {
			t.Fatalf("expected cancelled, got %v", err)
		}
		if sink.err == nil {
			t.Fatal("sink not notified of cancellation")
		}
		if len(audit.records) != 1 || audit.records[0].Status != provider.StatusCancelled {
			t.Fatalf("audit = %+v", audit.records)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not stop after cancellation")
	}
}

func TestStreamErrorStatusNotifiesSink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	adapter, _, _ := newTestAdapter(t, server, []byte("sk-test"))
	sink := &testSink{}
	err := adapter.Stream(context.Background(), testRequest(true), sink)
	if err == nil {
		t.Fatal("429 stream accepted")
	}
	providerErr, ok := provider.AsProviderError(err)
	if !ok || providerErr.Category != provider.CategoryRateLimited {
		t.Fatalf("expected rate_limited, got %v", err)
	}
	if sink.err == nil {
		t.Fatal("sink not notified")
	}
}

func TestRegistryRejectsUnknownKind(t *testing.T) {
	config := provider.Config{ID: "prov-1", Kind: provider.Kind("gemini"), BaseURL: "https://api.example.com"}
	registry := NewRegistry(&staticConfigs{config: config}, &staticSecret{}, &captureAudit{})
	if _, _, err := registry.TextProviderFor(context.Background(), "prov-1"); err == nil {
		t.Fatal("unregistered kind resolved to an adapter")
	}
}

func TestRegistryReturnsAdapterForOpenAIKind(t *testing.T) {
	config := provider.Config{ID: "prov-1", Kind: provider.KindOpenAICompatible, BaseURL: "https://api.example.com"}
	registry := NewRegistry(&staticConfigs{config: config}, &staticSecret{}, &captureAudit{})
	adapter, got, err := registry.TextProviderFor(context.Background(), "prov-1")
	if err != nil {
		t.Fatalf("TextProviderFor: %v", err)
	}
	if adapter == nil || got.ID != "prov-1" {
		t.Fatalf("unexpected adapter/config: %v %+v", adapter, got)
	}
}

func TestEndpointRejectsCredentialedBaseURL(t *testing.T) {
	adapter := NewOpenAITextAdapter(NewRegistry(&staticConfigs{}, &staticSecret{}, &captureAudit{}))
	if _, err := adapter.endpoint(provider.Config{BaseURL: "https://user:pass@api.example.com/v1"}, "/chat/completions"); err == nil {
		t.Fatal("credentialed base URL accepted")
	}
	if _, err := adapter.endpoint(provider.Config{BaseURL: "ftp://api.example.com"}, "/chat/completions"); err == nil {
		t.Fatal("ftp base URL accepted")
	}
}

func TestParseCompletionFallbacks(t *testing.T) {
	result, err := parseCompletion([]byte(`{"choices":[{"message":{"content":"x"}}]}`), "gpt-fallback")
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "gpt-fallback" {
		t.Fatalf("model fallback = %q", result.Model)
	}
	if _, err := parseCompletion([]byte(`not json`), "m"); err == nil {
		t.Fatal("bad JSON accepted")
	}
}

type testSink struct {
	onDelta func(string)
	onDone  func(appproviders.TextResult)
	onError func(error)
	err     error
}

func (s *testSink) OnDelta(delta string) {
	if s.onDelta != nil {
		s.onDelta(delta)
	}
}

func (s *testSink) OnDone(result appproviders.TextResult) {
	if s.onDone != nil {
		s.onDone(result)
	}
}

func (s *testSink) OnError(err error) {
	s.err = err
	if s.onError != nil {
		s.onError(err)
	}
}
