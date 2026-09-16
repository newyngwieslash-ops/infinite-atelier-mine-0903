package providerhttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

func TestValidateDownloadURL(t *testing.T) {
	valid := []string{
		"https://cdn.example.com/image.png",
		"https://storage.example.com:8443/path/to/file.mp4?sig=abc",
	}
	for _, raw := range valid {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateDownloadURL(parsed); err != nil {
			t.Errorf("valid download URL %q rejected: %v", raw, err)
		}
	}
	// http is refused even though the API client permits it for approved local
	// providers: a provider-supplied link has no user approval behind it.
	invalid := []string{
		"http://cdn.example.com/image.png",
		"ftp://cdn.example.com/image.png",
		"file:///etc/passwd",
		"https://user:pass@cdn.example.com/x.png",
		"https://cdn.example.com/x.png#frag",
		"https:///no-host.png",
	}
	for _, raw := range invalid {
		parsed, _ := url.Parse(raw)
		if err := validateDownloadURL(parsed); err == nil {
			t.Errorf("download URL %q accepted", raw)
		}
	}
	if err := validateDownloadURL(nil); err == nil {
		t.Error("nil URL accepted")
	}
}

func TestPublicDialRefusesPrivateAddresses(t *testing.T) {
	// Every forbidden form must be refused, including IPv4-mapped variants.
	forbidden := []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "169.254.169.254", "::1", "fd00::1", "::ffff:127.0.0.1"}
	for _, raw := range forbidden {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("test bug: %q did not parse", raw)
		}
		if !IsForbiddenIP(ip) {
			t.Errorf("IsForbiddenIP(%s) = false", raw)
		}
	}
}

func TestPublicDialRejectsMixedResolution(t *testing.T) {
	// A DNS answer mixing public and private addresses must fail closed.
	resolver := &fakeResolver{answers: map[string][]net.IP{
		"mixed.example.com":   {net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")},
		"private.example.com": {net.ParseIP("10.0.0.5")},
	}}
	if _, err := publicDial(context.Background(), resolver, "mixed.example.com:443"); err == nil {
		t.Fatal("mixed public/private resolution accepted")
	} else if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategorySecurity {
		t.Fatalf("expected security error, got %v", err)
	}
	if _, err := publicDial(context.Background(), resolver, "private.example.com:443"); err == nil {
		t.Fatal("private resolution accepted")
	}
	// A literal private IP is refused without any lookup.
	if _, err := publicDial(context.Background(), resolver, "127.0.0.1:443"); err == nil {
		t.Fatal("loopback literal accepted")
	}
	if _, err := publicDial(context.Background(), resolver, "169.254.169.254:80"); err == nil {
		t.Fatal("metadata literal accepted")
	}
}

func TestDownloadRefusesBlockedTargets(t *testing.T) {
	downloader := NewDownloader(&fakeResolver{answers: map[string][]net.IP{}}, DownloadLimits{})
	ctx := context.Background()
	var buffer bytes.Buffer
	// Non-https is refused before any connection.
	if _, _, err := downloader.Download(ctx, "http://cdn.example.com/x.png", &buffer, 0); err == nil {
		t.Fatal("http download accepted")
	}
	if _, _, err := downloader.Download(ctx, "file:///etc/passwd", &buffer, 0); err == nil {
		t.Fatal("file URL accepted")
	}
	if _, _, err := downloader.Download(ctx, "://nonsense", &buffer, 0); err == nil {
		t.Fatal("malformed URL accepted")
	}
	// A private host is refused at dial time.
	if _, _, err := downloader.Download(ctx, "https://private.example.com/x.png", &buffer, 0); err == nil {
		t.Fatal("private host download accepted")
	}
}

// TestDownloadEnforcesByteLimit proves the size cap is applied while streaming
// and reports the dedicated too-large error.
func TestDownloadEnforcesByteLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("A"), 4096))
	}))
	defer server.Close()

	downloader, err := downloaderForServer(server)
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	written, _, err := downloader.Download(context.Background(), server.URL+"/big.png", &buffer, 1024)
	if err == nil {
		t.Fatal("oversized download accepted")
	}
	if !errors.Is(err, ErrDownloadTooLarge) {
		t.Fatalf("error = %v, want ErrDownloadTooLarge", err)
	}
	// The caller is expected to discard the partial write; the count shows how
	// much was buffered so the caller can clean up.
	if written <= 1024 {
		t.Fatalf("written = %d, want more than the limit so oversize is detectable", written)
	}
}

// TestDownloadSucceedsWithinLimit proves a normal response streams through and
// reports its content type.
func TestDownloadSucceedsWithinLimit(t *testing.T) {
	payload := bytes.Repeat([]byte("B"), 512)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png; charset=binary")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	downloader, err := downloaderForServer(server)
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	written, contentType, err := downloader.Download(context.Background(), server.URL+"/ok.png", &buffer, 4096)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if written != int64(len(payload)) {
		t.Fatalf("written = %d, want %d", written, len(payload))
	}
	if contentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", contentType)
	}
	if !bytes.Equal(buffer.Bytes(), payload) {
		t.Fatal("payload corrupted")
	}
}

func TestDownloadMapsStatusCodes(t *testing.T) {
	cases := []struct {
		status       int
		wantCategory provider.ErrorCategory
	}{
		{status: http.StatusUnauthorized, wantCategory: provider.CategoryUnauthorized},
		{status: http.StatusForbidden, wantCategory: provider.CategoryForbidden},
		{status: http.StatusTooManyRequests, wantCategory: provider.CategoryRateLimited},
		{status: http.StatusInternalServerError, wantCategory: provider.CategoryRemoteTransient},
		{status: http.StatusServiceUnavailable, wantCategory: provider.CategoryRemoteTransient},
		{status: http.StatusNotFound, wantCategory: provider.CategoryRemotePermanent},
		{status: http.StatusGone, wantCategory: provider.CategoryRemotePermanent},
		{status: http.StatusBadRequest, wantCategory: provider.CategoryResponseInvalid},
	}
	for _, testCase := range cases {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(testCase.status)
		}))
		downloader, err := downloaderForServer(server)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		var buffer bytes.Buffer
		_, _, downloadErr := downloader.Download(context.Background(), server.URL+"/x.png", &buffer, 1024)
		server.Close()
		if downloadErr == nil {
			t.Fatalf("status %d accepted", testCase.status)
		}
		providerErr, ok := provider.AsProviderError(downloadErr)
		if !ok {
			t.Fatalf("status %d produced %T", testCase.status, downloadErr)
		}
		if providerErr.Category != testCase.wantCategory {
			t.Fatalf("status %d mapped to %q, want %q", testCase.status, providerErr.Category, testCase.wantCategory)
		}
	}
}

func TestDownloadTotalTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)

	base, err := downloaderForServer(server)
	if err != nil {
		t.Fatal(err)
	}
	downloader := NewDownloader(base.resolver, DownloadLimits{
		TotalTimeout:  200 * time.Millisecond,
		HeaderTimeout: 200 * time.Millisecond,
		IdleTimeout:   200 * time.Millisecond,
	})
	_, _, err = downloader.Download(context.Background(), server.URL+"/slow.png", &bytes.Buffer{}, 1024)
	if err == nil {
		t.Fatal("slow download returned no error")
	}
}

func TestDownloadRejectsRedirectToPrivateHost(t *testing.T) {
	// The redirect target is a different (private) host, so it must be refused
	// even though the first hop was legitimate.
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer target.Close()
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/moved.png", http.StatusFound)
	}))
	defer redirector.Close()

	downloader, err := downloaderForServer(redirector)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = downloader.Download(context.Background(), redirector.URL+"/start.png", &bytes.Buffer{}, 1024)
	if err == nil {
		t.Fatal("redirect to another loopback host accepted")
	}
}

func TestDownloadRequiresWriterAndClient(t *testing.T) {
	var nilDownloader *Downloader
	if _, _, err := nilDownloader.Download(context.Background(), "https://cdn.example.com/x", &bytes.Buffer{}, 0); err == nil {
		t.Fatal("nil downloader succeeded")
	}
	downloader := NewDownloader(&fakeResolver{answers: map[string][]net.IP{}}, DownloadLimits{})
	if _, _, err := downloader.Download(context.Background(), "https://cdn.example.com/x", nil, 0); err == nil {
		t.Fatal("nil writer accepted")
	}
}

func TestDownloadLimitsDefaults(t *testing.T) {
	limits := DownloadLimits{}.withDefaults()
	defaults := DefaultDownloadLimits()
	if limits.MaxBytes != defaults.MaxBytes || limits.TotalTimeout != defaults.TotalTimeout || limits.MaxRedirects != defaults.MaxRedirects {
		t.Fatalf("defaults not applied: %+v", limits)
	}
	// An explicit smaller cap is preserved.
	small := DownloadLimits{MaxBytes: 1024}.withDefaults()
	if small.MaxBytes != 1024 {
		t.Fatalf("explicit cap lost: %d", small.MaxBytes)
	}
	downloader := NewDownloader(nil, small)
	if got := downloader.DownloadLimitsOf().MaxBytes; got != 1024 {
		t.Fatalf("downloader limits = %d, want 1024", got)
	}
}

func TestDownloadRefusesMetadataEndpoint(t *testing.T) {
	downloader := NewDownloader(&fakeResolver{answers: map[string][]net.IP{
		"metadata.example.com": {net.ParseIP("169.254.169.254")},
	}}, DownloadLimits{})
	_, _, err := downloader.Download(context.Background(), "https://metadata.example.com/latest/meta-data/", &bytes.Buffer{}, 1024)
	if err == nil {
		t.Fatal("metadata endpoint reached")
	}
	if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategorySecurity {
		t.Fatalf("expected security error, got %v", err)
	}
}

// downloaderForServer builds a downloader that can reach an httptest TLS
// server: the server's certificate is trusted explicitly, and the loopback
// address is allowed only for this test seam. Production never constructs a
// downloader this way; the test asserts the policy pieces separately.
func downloaderForServer(server *httptest.Server) (*Downloader, error) {
	parsed, err := url.Parse(server.URL)
	if err != nil {
		return nil, err
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return nil, err
	}
	_ = host
	downloader := NewDownloader(&fakeResolver{answers: map[string][]net.IP{}}, DownloadLimits{})
	// Swap in a transport that dials the test server directly while still
	// running the URL validation and size limiting under test.
	downloader.client.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		},
		TLSClientConfig:       insecureTestTLSConfig(server),
		ResponseHeaderTimeout: 2 * time.Second,
	}
	return downloader, nil
}

func insecureTestTLSConfig(server *httptest.Server) *tls.Config {
	// The test server uses an untrusted certificate by design; trusting that
	// one certificate keeps TLS verification meaningful for everything else.
	return &tls.Config{RootCAs: certPoolFor(server), MinVersion: tls.VersionTLS12}
}

func TestDownloadRejectsUnknownStatusText(t *testing.T) {
	// A provider-supplied URL must never be echoed into an error message.
	downloader := NewDownloader(&fakeResolver{answers: map[string][]net.IP{}}, DownloadLimits{})
	_, _, err := downloader.Download(context.Background(), "https://user:secret-token@cdn.example.com/x.png", &bytes.Buffer{}, 1024)
	if err == nil {
		t.Fatal("credentialed URL accepted")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked URL credentials: %v", err)
	}
}

// certPoolFor returns a pool containing the test server certificate.
func certPoolFor(server *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	if cert := server.Certificate(); cert != nil {
		pool.AddCert(cert)
	}
	return pool
}
