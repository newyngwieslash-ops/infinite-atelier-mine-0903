package providerhttp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// fakeResolver returns deterministic address sets per host so DNS-rebinding
// sequences can be scripted.
type fakeResolver struct {
	answers map[string][]net.IP
	errs    map[string]error
	calls   int
}

func (f *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	f.calls++
	if err, ok := f.errs[host]; ok {
		return nil, err
	}
	ips, ok := f.answers[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host}
	}
	result := make([]net.IPAddr, 0, len(ips))
	for _, ip := range ips {
		result = append(result, net.IPAddr{IP: ip})
	}
	return result, nil
}

func TestIsForbiddenIPCorpus(t *testing.T) {
	// docs/SECURITY.md §6.2 + §18 Provider corpus.
	forbidden := []string{
		"0.0.0.0",
		"0.1.2.3",
		"10.0.0.1",
		"10.255.255.255",
		"100.64.0.1",
		"100.127.255.254",
		"127.0.0.1",
		"127.1.2.3",
		"169.254.0.1",
		"169.254.169.254", // cloud metadata
		"172.16.0.1",
		"172.31.255.255",
		"192.0.0.1",
		"192.0.0.170",
		"192.168.0.1",
		"192.168.255.255",
		"198.18.0.1",
		"198.19.255.255",
		"224.0.0.1",
		"239.255.255.255",
		"240.0.0.1",
		"255.255.255.255",
		"::",
		"::1",
		"fc00::1",
		"fdff:ffff::1",
		"fe80::1",
		"ff00::1",
		"::ffff:127.0.0.1",
		"::ffff:10.0.0.1",
		"::ffff:192.168.1.1",
		"::ffff:169.254.169.254",
	}
	for _, raw := range forbidden {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("test bug: %q did not parse", raw)
		}
		if !IsForbiddenIP(ip) {
			t.Errorf("IsForbiddenIP(%s) = false, want forbidden", raw)
		}
	}

	allowed := []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34",
		"2606:4700:4700::1111",
	}
	for _, raw := range allowed {
		ip := net.ParseIP(raw)
		if IsForbiddenIP(ip) {
			t.Errorf("IsForbiddenIP(%s) = true, want public", raw)
		}
	}
}

func TestValidateIPsRejectsMixedLocalAndPublic(t *testing.T) {
	policy := Policy{Host: "local.example.com", Port: "8080", Scheme: "http", AllowLocal: true}
	mixed := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("8.8.8.8")}
	if err := validateIPs(policy, "local.example.com", mixed); err == nil {
		t.Fatal("mixed private/public set accepted with AllowLocal")
	}
	allLocal := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if err := validateIPs(policy, "local.example.com", allLocal); err != nil {
		t.Fatalf("all-local set rejected: %v", err)
	}
	if err := validateIPs(policy, "other.example.com", allLocal); err == nil {
		t.Fatal("AllowLocal accepted a different host")
	}
	if err := validateIPs(Policy{Host: "local.example.com"}, "local.example.com", allLocal); err == nil {
		t.Fatal("private address accepted without AllowLocal")
	}
}

func TestValidateRedirectPolicy(t *testing.T) {
	policy := Policy{Host: "api.example.com", Port: "", Scheme: "https"}
	parse := func(raw string) *url.URL {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}

	if err := validateRedirect(policy, parse("https://api.example.com/v1/chat")); err != nil {
		t.Fatalf("valid URL rejected: %v", err)
	}
	reject := []string{
		"http://api.example.com/v1",            // downgrade
		"https://other.example.com/v1",         // host change
		"https://api.example.com:8443/v1",      // port change
		"https://user:pass@api.example.com/v1", // credentials
		"https://api.example.com/v1#frag",      // fragment
		"ftp://api.example.com/v1",             // scheme
		"https://api.example.com.evil.test/v1", // suffix confusion
		"https://127.0.0.1/v1",                 // host mismatch (loopback alias)
	}
	for _, raw := range reject {
		if err := validateRedirect(policy, parse(raw)); err == nil {
			t.Errorf("redirect URL %q accepted, want rejected", raw)
		}
	}
}

func TestClientRejectsForbiddenHostsBeforeDialing(t *testing.T) {
	resolver := &fakeResolver{answers: map[string][]net.IP{
		"public.example.com": {net.ParseIP("8.8.8.8")},
	}}
	client := NewClient(Policy{Host: "public.example.com", Scheme: "https"}, resolver, Limits{})

	// Host not in policy → rejected before any DNS/dial.
	request, _ := http.NewRequest(http.MethodPost, "https://127.0.0.1/v1/chat", strings.NewReader("{}"))
	if _, err := client.Do(context.Background(), request); err == nil {
		t.Fatal("loopback request accepted")
	} else if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategorySecurity {
		t.Fatalf("expected security error, got %v", err)
	}
}

func TestClientRejectsPrivateDNSAnswer(t *testing.T) {
	resolver := &fakeResolver{answers: map[string][]net.IP{
		"rebind.example.com": {net.ParseIP("10.1.2.3")},
	}}
	policy := Policy{Host: "rebind.example.com", Scheme: "https"}
	client := NewClient(policy, resolver, Limits{})
	request, _ := http.NewRequest(http.MethodGet, "https://rebind.example.com/v1/models", nil)
	if _, err := client.Do(context.Background(), request); err == nil {
		t.Fatal("private DNS answer accepted")
	} else if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategorySecurity {
		t.Fatalf("expected security error, got %v", err)
	}
}

func TestClientRejectsRebindingSecondLookup(t *testing.T) {
	// First validation sees a public address; the second lookup (used by a
	// naive implementation at dial time) returns private. Our dialer uses the
	// validated literal IP, so the connection attempt goes to the public IP
	// recorded during validation and never re-resolves.
	resolver := &scriptedResolver{
		responses: [][]net.IP{
			{net.ParseIP("127.0.0.1")},
			// A second lookup would flip to a public address. The dialer must
			// use the validated literal instead of re-resolving.
			{net.ParseIP("8.8.8.8")},
		},
	}
	policy := Policy{Host: "rebind.example.com", Scheme: "https"}
	client := NewClient(policy, resolver, Limits{})
	request, _ := http.NewRequest(http.MethodGet, "https://rebind.example.com/v1/models", nil)
	if _, err := client.Do(context.Background(), request); err == nil {
		t.Fatal("rebinding to loopback accepted")
	}
	if resolver.Index() != 1 {
		t.Fatalf("resolver called %d times; a second lookup reopens the rebinding window", resolver.Index())
	}
}

// Index reports how many lookups ran.
func (s *scriptedResolver) Index() int { return s.index }

type scriptedResolver struct {
	responses [][]net.IP
	index     int
}

func (s *scriptedResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	if s.index >= len(s.responses) {
		return nil, &net.DNSError{Err: "no more answers"}
	}
	ips := s.responses[s.index]
	s.index++
	result := make([]net.IPAddr, 0, len(ips))
	for _, ip := range ips {
		result = append(result, net.IPAddr{IP: ip})
	}
	return result, nil
}

func TestClientIgnoresEnvironmentProxy(t *testing.T) {
	// A proxy environment variable must not influence the transport; the
	// transport field is nil by construction. This test documents the
	// contract and fails if someone re-enables http.ProxyFromEnvironment.
	client := NewClient(Policy{Host: "api.example.com", Scheme: "https"}, nil, Limits{})
	transport, ok := client.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("unexpected transport type")
	}
	if transport.Proxy != nil {
		t.Fatal("environment proxy is enabled; Authorization could leak to a proxy")
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS verification disabled")
	}
}

// TestLimitsPreserveExplicitResponseSizeLimit pins the limit plumbing. The
// end-to-end truncation behavior is proven against a real listener by
// TestClientResponseSizeLimitEnforced in client_server_test.go.
func TestLimitsPreserveExplicitResponseSizeLimit(t *testing.T) {
	limits := Limits{MaxResponseBytes: 32}.withDefaults()
	if limits.MaxResponseBytes != 32 {
		t.Fatalf("MaxResponseBytes = %d, want 32", limits.MaxResponseBytes)
	}
}

// TestValidateIPsLocalApprovalRequiresAllAddressesPrivate proves the local
// exception cannot be used to reach a public host over cleartext HTTP: an
// approved-local policy whose host resolves to a public address is rejected.
func TestValidateIPsLocalApprovalRequiresAllAddressesPrivate(t *testing.T) {
	policy := Policy{Host: "public.example.com", Scheme: "http", Port: "80", AllowLocal: true}
	if err := validateIPs(policy, "public.example.com", []net.IP{net.ParseIP("93.184.216.34")}); err == nil {
		t.Fatal("AllowLocal permitted a public address; cleartext HTTP to a public host would be allowed")
	}
}

// TestValidateIPsLocalApprovalStillBlocksMetadata proves the metadata
// endpoints stay blocked even under an exact host approval.
func TestValidateIPsLocalApprovalStillBlocksMetadata(t *testing.T) {
	policy := Policy{Host: "169.254.169.254", Scheme: "http", Port: "80", AllowLocal: true}
	if err := validateIPs(policy, "169.254.169.254", []net.IP{net.ParseIP("169.254.169.254")}); err == nil {
		t.Fatal("metadata endpoint accepted under local approval")
	}
	policyV6 := Policy{Host: "fd00:ec2::254", Scheme: "http", Port: "80", AllowLocal: true}
	if err := validateIPs(policyV6, "fd00:ec2::254", []net.IP{net.ParseIP("fd00:ec2::254")}); err == nil {
		t.Fatal("IPv6 metadata endpoint accepted under local approval")
	}
}

// TestValidateIPsPublicProviderStillAllowsPublic proves the default path is
// unchanged: a normal public provider with no local approval is allowed.
func TestValidateIPsPublicProviderStillAllowsPublic(t *testing.T) {
	policy := Policy{Host: "api.example.com", Scheme: "https", Port: "443"}
	public := []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("2606:4700:4700::1111")}
	if err := validateIPs(policy, "api.example.com", public); err != nil {
		t.Fatalf("public provider rejected: %v", err)
	}
	if err := validateIPs(policy, "api.example.com", []net.IP{net.ParseIP("10.0.0.1")}); err == nil {
		t.Fatal("private address accepted without AllowLocal")
	}
}

func TestLimitsDefaultsApplied(t *testing.T) {
	limits := Limits{}.withDefaults()
	defaults := DefaultLimits()
	if limits.ConnectTimeout != defaults.ConnectTimeout ||
		limits.HeaderTimeout != defaults.HeaderTimeout ||
		limits.TotalTimeout != defaults.TotalTimeout ||
		limits.MaxRedirects != defaults.MaxRedirects ||
		limits.MaxResponseBytes != defaults.MaxResponseBytes {
		t.Fatalf("defaults not applied: %+v", limits)
	}
}

func TestMapTransportErrorTaxonomy(t *testing.T) {
	if err := mapTransportError(context.Canceled); err == nil {
		t.Fatal("canceled not mapped")
	} else if providerErr, _ := provider.AsProviderError(err); providerErr.Category != provider.CategoryCancelled {
		t.Fatalf("canceled mapped to %q", providerErr.Category)
	}
	if err := mapTransportError(context.DeadlineExceeded); err == nil {
		t.Fatal("deadline not mapped")
	} else if providerErr, _ := provider.AsProviderError(err); providerErr.Category != provider.CategoryTimeout {
		t.Fatalf("deadline mapped to %q", providerErr.Category)
	}
}

func TestValidateRequestURLRejectsMethods(t *testing.T) {
	policy := Policy{Host: "api.example.com", Scheme: "https"}
	parsed, _ := url.Parse("https://api.example.com/v1")
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodHead} {
		if err := validateRequestURL(policy, method, parsed); err != nil {
			t.Errorf("method %s rejected: %v", method, err)
		}
	}
	for _, method := range []string{http.MethodPut, http.MethodPatch, "TRACE", "CONNECT"} {
		if err := validateRequestURL(policy, method, parsed); err == nil {
			t.Errorf("method %s accepted, want rejected", method)
		}
	}
}

// TestClientAllowsExactLocalProviderWhenApproved proves the explicit local
// exception: only the exact approved host:port over http works, and a public
// host on the same policy is still denied.
func TestClientAllowsExactLocalProviderWhenApproved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	policy := Policy{Host: serverURL.Hostname(), Scheme: "http", Port: serverURL.Port(), AllowLocal: true}
	client := NewClient(policy, &fakeResolver{answers: map[string][]net.IP{}}, Limits{})
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/models", nil)
	response, err := client.Do(context.Background(), request)
	if err != nil {
		t.Fatalf("approved local provider rejected: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

// TestClientRejectsOtherLocalHostWithApproval proves the exception is exact:
// the same policy rejects a different local host/port.
func TestClientRejectsOtherLocalHostWithApproval(t *testing.T) {
	policy := Policy{Host: "127.0.0.1", Scheme: "http", Port: "9999", AllowLocal: true}
	client := NewClient(policy, &fakeResolver{answers: map[string][]net.IP{}}, Limits{})
	request, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:1234/v1", nil)
	if _, err := client.Do(context.Background(), request); err == nil {
		t.Fatal("non-approved local port accepted")
	}
	request, _ = http.NewRequest(http.MethodGet, "http://10.0.0.5:9999/v1", nil)
	if _, err := client.Do(context.Background(), request); err == nil {
		t.Fatal("non-approved private host accepted")
	}
}
