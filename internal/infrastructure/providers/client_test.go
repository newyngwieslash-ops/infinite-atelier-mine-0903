package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// validateEndpoint asserts the exact URL the adapter will send passes the
// same policy the guarded client enforces.
func validateEndpoint(t *testing.T, config provider.Config, endpoint string) error {
	t.Helper()
	policy, err := policyFor(config)
	if err != nil {
		t.Fatalf("policyFor: %v", err)
	}
	return phttp.ValidateURLForPolicy(policy, http.MethodPost, endpoint)
}

// TestGuardedClientAcceptsCanonicalPortlessURLs is the regression test for the
// production path bug found in spec review: guardedClient pins the effective
// port (443/80), while endpointFor emits a URL with NO explicit port. A naive
// string comparison of policy port vs URL port rejected every normal
// `https://host/v1` provider. This test uses the real constructors with no
// injected client factory, so the bug cannot hide behind a test seam again.
func TestGuardedClientAcceptsCanonicalPortlessURLs(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		scheme string
		host   string
		port   string
	}{
		{name: "https no port", base: "https://api.example.com/v1", scheme: "https", host: "api.example.com", port: "443"},
		{name: "https explicit 443", base: "https://api.example.com:443/v1", scheme: "https", host: "api.example.com", port: "443"},
		{name: "http no port", base: "http://api.example.com/v1", scheme: "http", host: "api.example.com", port: "80"},
		{name: "https pinned custom port", base: "https://api.example.com:8443/v1", scheme: "https", host: "api.example.com", port: "8443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := provider.Config{
				ID:            "prov-1",
				Kind:          provider.KindOpenAICompatible,
				BaseURL:       tc.base,
				LocalApproved: tc.scheme == "http",
			}
			// The endpoint URL must be accepted by the policy built from the
			// same config.
			endpoint, err := endpointFor(config, "/chat/completions")
			if err != nil {
				t.Fatalf("endpointFor: %v", err)
			}
			client, err := guardedClient(config)
			if err != nil {
				t.Fatalf("guardedClient: %v", err)
			}
			// Validate the exact URL the adapter will send. A security-policy
			// rejection here means the provider could never be called.
			if err := validateEndpoint(t, config, endpoint); err != nil {
				t.Fatalf("policy rejected its own endpoint %q: %v", endpoint, err)
			}
			_ = client
		})
	}
}

// TestGuardedClientRejectsWrongPort proves the normalization did not open a
// hole: a different explicit port is still rejected.
func TestGuardedClientRejectsWrongPort(t *testing.T) {
	config := provider.Config{ID: "prov-1", Kind: provider.KindOpenAICompatible, BaseURL: "https://api.example.com/v1"}
	if err := validateEndpoint(t, config, "https://api.example.com:8443/v1/chat/completions"); err == nil {
		t.Fatal("policy accepted a non-pinned port")
	}
	if err := validateEndpoint(t, config, "https://api.example.com:80/v1/chat/completions"); err == nil {
		t.Fatal("policy accepted a downgraded port on https")
	}
}

// TestGuardedClientAllowsLocalProviderForReal exercises the allow-local path
// end to end against a real listener using the production constructors.
func TestGuardedClientAllowsLocalProviderForReal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	config := provider.Config{
		ID:            "prov-1",
		Kind:          provider.KindOpenAICompatible,
		BaseURL:       server.URL + "/v1",
		LocalApproved: true,
	}
	endpoint, err := endpointFor(config, "/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	client, err := guardedClient(config)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(context.Background(), request)
	if err != nil {
		t.Fatalf("approved local provider rejected by production path: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
}
