package providerhttp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// localPolicyFor builds an AllowLocal policy matching one httptest server.
func localPolicyFor(t *testing.T, server *httptest.Server) Policy {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return Policy{Host: parsed.Hostname(), Scheme: "http", Port: parsed.Port(), AllowLocal: true}
}

func TestClientEnforcesRedirectHostChange(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/elsewhere", http.StatusFound)
	}))
	defer redirector.Close()

	client := NewClient(localPolicyFor(t, redirector), &fakeResolver{answers: map[string][]net.IP{}}, Limits{})
	request, _ := http.NewRequest(http.MethodGet, redirector.URL+"/start", nil)
	_, err := client.Do(context.Background(), request)
	if err == nil {
		t.Fatal("cross-host redirect accepted")
	}
	if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategorySecurity {
		t.Fatalf("expected security error, got %v", err)
	}
}

func TestClientFollowsSameHostRedirect(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	client := NewClient(localPolicyFor(t, server), &fakeResolver{answers: map[string][]net.IP{}}, Limits{})
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/start", nil)
	response, err := client.Do(context.Background(), request)
	if err != nil {
		t.Fatalf("same-host redirect rejected: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestClientRejectsRedirectToPrivateViaPublicLookalike(t *testing.T) {
	// A redirect to a loopback literal must be rejected even though the policy
	// uses AllowLocal for its own server: the redirect target differs.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.2:9/steal", http.StatusFound)
	}))
	defer server.Close()

	client := NewClient(localPolicyFor(t, server), &fakeResolver{answers: map[string][]net.IP{}}, Limits{})
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/start", nil)
	if _, err := client.Do(context.Background(), request); err == nil {
		t.Fatal("redirect to different loopback accepted")
	}
}

func TestClientResponseSizeLimitEnforced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("A", 4096)))
	}))
	defer server.Close()

	limits := Limits{MaxResponseBytes: 64}
	client := NewClient(localPolicyFor(t, server), &fakeResolver{answers: map[string][]net.IP{}}, limits)
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/big", nil)
	response, err := client.Do(context.Background(), request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(body) != 64 {
		t.Fatalf("read %d bytes, want 64 limited bytes", len(body))
	}
}

func TestClientContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(localPolicyFor(t, server), &fakeResolver{answers: map[string][]net.IP{}}, Limits{})
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/slow", nil)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := client.Do(ctx, request)
	if err == nil {
		t.Fatal("cancelled request returned nil error")
	}
	if !errors.Is(err, context.Canceled) {
		providerErr, ok := provider.AsProviderError(err)
		if !ok || providerErr.Category != provider.CategoryCancelled {
			t.Fatalf("expected cancellation, got %v", err)
		}
	}
}

func TestClientTotalTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	limits := Limits{TotalTimeout: 100 * time.Millisecond, HeaderTimeout: 100 * time.Millisecond, IdleTimeout: 100 * time.Millisecond}
	client := NewClient(localPolicyFor(t, server), &fakeResolver{answers: map[string][]net.IP{}}, limits)
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/slow", nil)
	_, err := client.Do(context.Background(), request)
	if err == nil {
		t.Fatal("slow request returned nil error")
	}
	providerErr, ok := provider.AsProviderError(err)
	if !ok || providerErr.Category != provider.CategoryTimeout {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestValidatedDialRejectsPolicyMismatch(t *testing.T) {
	policy := Policy{Host: "expected.example.com", Scheme: "https"}
	if _, err := validatedDial(context.Background(), &fakeResolver{}, policy, "other.example.com:443"); err == nil {
		t.Fatal("dial to non-policy host accepted")
	}
	if _, err := validatedDial(context.Background(), &fakeResolver{}, policy, "bad-address"); err == nil {
		t.Fatal("malformed address accepted")
	}
}

func TestValidatedDialLiteralForbidden(t *testing.T) {
	policy := Policy{Host: "127.0.0.1", Scheme: "http", Port: "80"}
	if _, err := validatedDial(context.Background(), &fakeResolver{}, policy, "127.0.0.1:80"); err == nil {
		t.Fatal("loopback literal dial accepted without AllowLocal")
	}
}
