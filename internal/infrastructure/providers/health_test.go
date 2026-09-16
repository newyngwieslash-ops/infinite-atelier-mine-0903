package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// TestHealthCheckerReachableAndUnhealthy covers the production health path
// using the real constructors (no injected client factory).
func TestHealthCheckerReachableAndUnhealthy(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		wantHealthy bool
		wantDetail  string
	}{
		{name: "200 healthy", status: http.StatusOK, wantHealthy: true, wantDetail: "reachable"},
		{name: "401 reachable but credentials rejected", status: http.StatusUnauthorized, wantHealthy: true, wantDetail: "reachable, credentials rejected"},
		{name: "403 reachable but credentials rejected", status: http.StatusForbidden, wantHealthy: true, wantDetail: "reachable, credentials rejected"},
		{name: "500 unhealthy", status: http.StatusInternalServerError, wantHealthy: false, wantDetail: "provider error"},
		{name: "429 unhealthy", status: http.StatusTooManyRequests, wantHealthy: false, wantDetail: "rate limited"},
		{name: "404 unhealthy", status: http.StatusNotFound, wantHealthy: false, wantDetail: "request rejected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			defer server.Close()

			config := provider.Config{
				ID:            "prov-1",
				Kind:          provider.KindOpenAICompatible,
				BaseURL:       server.URL + "/v1",
				LocalApproved: true,
			}
			registry := NewRegistry(&staticConfigs{config: config}, &staticSecret{value: []byte("sk-test")}, &captureAudit{})
			checker := NewHealthChecker(registry)

			state, err := checker.Check(context.Background(), "prov-1")
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if state.Healthy != tc.wantHealthy {
				t.Fatalf("healthy = %v, want %v (%+v)", state.Healthy, tc.wantHealthy, state)
			}
			if tc.wantDetail != "" && state.Detail != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", state.Detail, tc.wantDetail)
			}
			if state.ProviderID != "prov-1" || state.CheckedAt.IsZero() {
				t.Fatalf("state = %+v", state)
			}
		})
	}
}

// TestHealthCheckerWithoutSecret reports credential-unavailable without
// contacting the server.
func TestHealthCheckerWithoutSecret(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	config := provider.Config{ID: "prov-1", Kind: provider.KindOpenAICompatible, BaseURL: server.URL + "/v1", LocalApproved: true}
	secrets := &staticSecret{err: provider.NewSecurityError()}
	registry := NewRegistry(&staticConfigs{config: config}, secrets, &captureAudit{})
	checker := NewHealthChecker(registry)

	state, err := checker.Check(context.Background(), "prov-1")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if state.Healthy || state.Detail != "credentials unavailable" {
		t.Fatalf("state = %+v", state)
	}
	if called {
		t.Fatal("health check contacted the provider without credentials")
	}
}

// TestHealthCheckerUnknownProviderFailsClosed proves health never invents a
// result for an unregistered provider.
func TestHealthCheckerUnknownProviderFailsClosed(t *testing.T) {
	registry := NewRegistry(&staticConfigs{}, &staticSecret{}, &captureAudit{})
	checker := NewHealthChecker(registry)
	if _, err := checker.Check(context.Background(), "missing"); err == nil {
		t.Fatal("unknown provider reported health")
	}
}

// TestHealthCheckerNilRegistryFailsClosed proves the nil-safety contract used
// by safe-mode startup.
func TestHealthCheckerNilRegistryFailsClosed(t *testing.T) {
	var checker *HealthChecker
	if _, err := checker.Check(context.Background(), "p"); err == nil {
		t.Fatal("nil checker returned a result")
	}
	empty := NewHealthChecker(nil)
	if _, err := empty.Check(context.Background(), "p"); err == nil {
		t.Fatal("nil-registry checker returned a result")
	}
}
