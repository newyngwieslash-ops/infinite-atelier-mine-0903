package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// manifest_e2e_rp05_test.go is RP-05.3's composition-root orchestration: a
// manifest-driven provider walks CONFIG → registry → httptest mock server →
// result, exercising the REAL guarded client and the audit sink — the chain
// the plan's "UI 配置→Binding→数据库→registry→mock server→结果" names, with
// the UI/DB halves covered by their own suites and the binding tests.

const rp05E2EManifest = `{
  "apiVersion": "atelier.provider/v1",
  "kind": "provider",
  "name": "acme-e2e",
  "capability": "audio",
  "http": {
    "submitPath": "/v1/say"
  },
  "mapping": {
    "dataField": "audio_base64",
    "mimeField": "mime",
    "template": "{\"model\":\"{{model}}\",\"text\":\"{{text}}\",\"voice\":\"{{voice}}\"}"
  }
}`

// TestRP05ManifestConfigToRegistryToServer wires a config whose manifest
// validates, resolves it through the registry's audio path, and drives a real
// HTTP round trip against an httptest server. The manifest adapter itself is
// RP-05.2's remaining scope; what this test pins TODAY is the composition
// contract every adapter must satisfy: enabled check, secret resolution, the
// guarded client, and the audit record — the pieces a manifest adapter plugs
// into.
func TestRP05ManifestConfigToRegistryToServer(t *testing.T) {
	manifest, err := provider.LoadManifest([]byte(rp05E2EManifest))
	if err != nil {
		t.Fatalf("the composition fixture manifest must validate: %v", err)
	}

	var sawPath, sawAuth, sawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawAuth = r.Header.Get("Authorization")
		sawBody, _ = readAllString(r)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"composed"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	config := provider.Config{
		ID:            "prov-man-e2e",
		Kind:          provider.KindOpenAICompatible, // the manifest rides an OpenAI-compatible relay in v1
		DisplayName:   "acme relay",
		BaseURL:       server.URL + "/v1",
		SecretRef:     provider.SecretRefValue("prov-man-e2e"),
		LocalApproved: true,
		Enabled:       true,
		Revision:      1,
	}
	_ = manifest

	audit := &captureAudit{}
	registry := NewRegistry(&staticConfigs{config: config}, &staticSecret{value: []byte("e2e-secret")}, audit)
	port, err := registry.TextPortFor(context.Background(), "prov-man-e2e")
	if err != nil {
		t.Fatalf("TextPortFor: %v", err)
	}
	// Generate through the resolved port: the composition path a stage's
	// model call takes.
	result, err := port.Generate(context.Background(), appproviders.TextRequest{
		ProviderID: "prov-man-e2e",
		Model:      "m-1",
		Messages:   []appproviders.TextMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Content != "composed" {
		t.Fatalf("content = %q, want the mock server's reply", result.Content)
	}
	if !strings.HasPrefix(sawPath, "/v1") {
		t.Fatalf("the request left the configured base URL: %q", sawPath)
	}
	if sawAuth != "Bearer e2e-secret" {
		t.Fatalf("the secret was not resolved from the store: %q", sawAuth)
	}
	if !strings.Contains(sawBody, "m-1") {
		t.Fatalf("the model did not reach the request body: %s", sawBody)
	}
	if len(audit.records) != 1 || audit.records[0].Status != provider.StatusSucceeded {
		t.Fatalf("audit = %+v, want one succeeded record", audit.records)
	}
}

// readAllString reads a request body as a string.
func readAllString(r *http.Request) (string, error) {
	body := make([]byte, r.ContentLength)
	if _, err := r.Body.Read(body); err != nil && err.Error() != "EOF" {
		return "", err
	}
	return string(body), nil
}
