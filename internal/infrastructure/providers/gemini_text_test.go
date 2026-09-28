package providers

import (
	"context"
	"encoding/json"
	"errors"
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

// gemini_text_test.go is RP-02.3's contract: a `gemini_compatible` provider's
// TEXT requests reach the Gemini-native generateContent endpoint — never the
// OpenAI chat-completions path the previous wiring sent them to — with the
// Gemini auth header, the Gemini request shape and the Gemini response
// vocabulary. All evidence comes from what a real httptest server RECEIVES
// and RETURNS, not from source strings.

// newGeminiTextFixture wires a Gemini text adapter against an httptest server
// with the AllowLocal policy the loopback server needs.
func newGeminiTextFixture(t *testing.T, server *httptest.Server, secret []byte) (*GeminiTextAdapter, *captureAudit, *staticSecret) {
	t.Helper()
	config := provider.Config{
		ID:            "prov-gem",
		Kind:          provider.KindGeminiCompatible,
		DisplayName:   "gem test",
		BaseURL:       server.URL + "/v1beta",
		SecretRef:     provider.SecretRefValue("prov-gem"),
		LocalApproved: true,
		Enabled:       true,
		Revision:      1,
	}
	secrets := &staticSecret{value: secret}
	audit := &captureAudit{}
	registry := NewRegistry(&staticConfigs{config: config}, secrets, audit)
	adapter := NewGeminiTextAdapter(registry)
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

func geminiTestRequest(stream bool) appproviders.TextRequest {
	return appproviders.TextRequest{
		ProviderID: "prov-gem",
		Model:      "gemini-test",
		Messages: []appproviders.TextMessage{
			{Role: "system", Content: "you are a storyboard supervisor"},
			{Role: "user", Content: "review this board"},
		},
		Stream: stream,
	}
}

// TestRP02GeminiTextReachesGenerateContentNotChat pins the ROUTING: the
// request arrives at /models/{model}:generateContent with the Gemini header
// and body, and the chat-completions path is NEVER touched.
func TestRP02GeminiTextReachesGenerateContentNotChat(t *testing.T) {
	var sawPath, sawAuth, sawAccept string
	var sawBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawAuth = r.Header.Get("x-goog-api-key")
		sawAccept = r.Header.Get("Accept")
		sawBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"the board is coherent"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":7}}`))
	}))
	defer server.Close()

	adapter, audit, secrets := newGeminiTextFixture(t, server, []byte("gem-secret"))
	result, err := adapter.Generate(context.Background(), geminiTestRequest(false))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if sawPath != "/v1beta/models/gemini-test:generateContent" {
		t.Fatalf("request path = %q, want the Gemini generateContent endpoint", sawPath)
	}
	if sawAuth != "gem-secret" {
		t.Fatalf("x-goog-api-key = %q, want the resolved secret; the OpenAI Bearer header must not appear on a Gemini call", sawAuth)
	}
	if sawAccept != "application/json" {
		t.Fatalf("Accept = %q", sawAccept)
	}
	var body map[string]any
	if err := json.Unmarshal(sawBody, &body); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	// The chat-completions fields must NOT appear; the Gemini fields must.
	if _, present := body["messages"]; present {
		t.Fatalf("request body carries the OpenAI `messages` field: %s", sawBody)
	}
	if _, present := body["contents"]; !present {
		t.Fatalf("request body lacks the Gemini `contents` field: %s", sawBody)
	}
	// The system message travels as systemInstruction, the user message as a
	// user-role content entry.
	if _, present := body["systemInstruction"]; !present {
		t.Fatalf("request body lacks systemInstruction for the system message: %s", sawBody)
	}
	if result.Content != "the board is coherent" {
		t.Fatalf("content = %q", result.Content)
	}
	if len(audit.records) != 1 || audit.records[0].Status != provider.StatusSucceeded {
		t.Fatalf("audit = %+v", audit.records)
	}
	if audit.records[0].InputUnits != 12 || audit.records[0].OutputUnits != 7 {
		t.Fatalf("usage units = %+v, want the Gemini usageMetadata counts", audit.records[0])
	}
	// The secret the adapter resolved was a copy; the original is intact for
	// the next call.
	if secrets.value == nil || string(secrets.value) != "gem-secret" {
		t.Fatalf("secret store value was mutated")
	}
}

// TestRP02GeminiTextStreamsParsesCandidates drives the SSE path: deltas are
// forwarded per part, the combined content is the result, and a cancelled
// context stops the stream rather than reading on.
func TestRP02GeminiTextStreamsParsesCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "alt=sse") {
			http.Error(w, "missing alt=sse", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, chunk := range []string{
			`data: {"candidates":[{"content":{"parts":[{"text":"first "}],"role":"model"}}]}`,
			`data: {"candidates":[{"content":{"parts":[{"text":"second"}],"role":"model"}}]}`,
		} {
			_, _ = w.Write([]byte(chunk + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	adapter, audit, _ := newGeminiTextFixture(t, server, []byte("k"))
	sink := &capturingSink{}
	if err := adapter.Stream(context.Background(), geminiTestRequest(true), sink); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if sink.deltas != "first second" {
		t.Fatalf("deltas = %q", sink.deltas)
	}
	if sink.done == nil || sink.done.Content != "first second" {
		t.Fatalf("done = %+v", sink.done)
	}
	if len(audit.records) != 1 || audit.records[0].Status != provider.StatusSucceeded {
		t.Fatalf("audit = %+v", audit.records)
	}
}

type capturingSink struct {
	deltas string
	done   *appproviders.TextResult
	failed error
}

func (s *capturingSink) OnDelta(delta string)                  { s.deltas += delta }
func (s *capturingSink) OnDone(result appproviders.TextResult) { s.done = &result }
func (s *capturingSink) OnError(err error)                     { s.failed = err }

// TestRP02GeminiTextErrorShapesRefuse maps failure shapes to the taxonomy: a
// non-200 is rate-limit/unauthorized mapped, a 200 envelope carrying a Gemini
// error object is response-invalid (never a silent success).
func TestRP02GeminiTextErrorShapesRefuse(t *testing.T) {
	t.Run("401 maps to unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "denied", http.StatusUnauthorized)
		}))
		defer server.Close()
		adapter, audit, _ := newGeminiTextFixture(t, server, []byte("k"))
		_, err := adapter.Generate(context.Background(), geminiTestRequest(false))
		providerErr, ok := provider.AsProviderError(err)
		if !ok || providerErr.Category != provider.CategoryUnauthorized {
			t.Fatalf("err = %v, want the unauthorized category", err)
		}
		if len(audit.records) != 1 || audit.records[0].Status != provider.StatusFailed {
			t.Fatalf("audit = %+v", audit.records)
		}
	})

	t.Run("200 with error object is not a success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`))
		}))
		defer server.Close()
		adapter, _, _ := newGeminiTextFixture(t, server, []byte("k"))
		_, err := adapter.Generate(context.Background(), geminiTestRequest(false))
		if err == nil {
			t.Fatalf("a 200 envelope with an error object succeeded")
		}
	})

	t.Run("empty candidates is response-invalid", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"candidates":[]}`))
		}))
		defer server.Close()
		adapter, _, _ := newGeminiTextFixture(t, server, []byte("k"))
		_, err := adapter.Generate(context.Background(), geminiTestRequest(false))
		if err == nil {
			t.Fatalf("an empty candidate list succeeded")
		}
	})

	t.Run("context cancel stops the stream", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"a\"}]}}]}\n\n"))
			flusher.Flush()
			<-r.Context().Done()
		}))
		defer server.Close()
		adapter, _, _ := newGeminiTextFixture(t, server, []byte("k"))
		// The timeout IS the cancellation trigger: the server holds the
		// stream open after one chunk, so a client read that honoured no
		// deadline would block for the guarded client's full ceiling.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		sink := &capturingSink{}
		started := time.Now()
		err := adapter.Stream(ctx, geminiTestRequest(true), sink)
		if err == nil {
			t.Fatalf("a cancelled stream returned no error")
		}
		if elapsed := time.Since(started); elapsed > 30*time.Second {
			t.Fatalf("cancellation took %v to propagate", elapsed)
		}
		if !provider.IsCancellation(errors.Unwrap(err)) && !provider.IsCancellation(err) {
			t.Fatalf("err = %v, want a cancellation", err)
		}
	})
}

// TestRP02RegistryRoutesGeminiKindToGeminiAdapter is the routing regression
// at the registry: TextPortFor for a gemini_compatible config returns the
// Gemini adapter, and a Generate through it hits generateContent — the
// composition-root path the settings drawer's kind reaches.
func TestRP02RegistryRoutesGeminiKindToGeminiAdapter(t *testing.T) {
	var sawPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}]}`))
	}))
	defer server.Close()

	config := provider.Config{
		ID:            "prov-route",
		Kind:          provider.KindGeminiCompatible,
		DisplayName:   "route test",
		BaseURL:       server.URL,
		SecretRef:     provider.SecretRefValue("prov-route"),
		LocalApproved: true,
		Enabled:       true,
		Revision:      1,
	}
	registry := NewRegistry(&staticConfigs{config: config}, &staticSecret{value: []byte("k")}, &captureAudit{})
	port, err := registry.TextPortFor(context.Background(), "prov-route")
	if err != nil {
		t.Fatalf("TextPortFor: %v", err)
	}
	if _, ok := port.(*GeminiTextAdapter); !ok {
		t.Fatalf("TextPortFor returned %T, want *GeminiTextAdapter", port)
	}
	result, err := port.Generate(context.Background(), appproviders.TextRequest{
		ProviderID: "prov-route",
		Model:      "m",
		Messages:   []appproviders.TextMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if sawPath != "/models/m:generateContent" {
		t.Fatalf("routed path = %q", sawPath)
	}
	if result.Content != "ok" {
		t.Fatalf("content = %q", result.Content)
	}
}
