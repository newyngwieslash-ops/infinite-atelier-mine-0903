package providerhttp

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// Limits bounds a provider HTTP call. Zero values fall back to the defaults.
type Limits struct {
	ConnectTimeout      time.Duration
	TLSHandshakeTimeout time.Duration
	HeaderTimeout       time.Duration
	IdleTimeout         time.Duration
	TotalTimeout        time.Duration
	MaxRedirects        int
	MaxResponseBytes    int64
}

// DefaultLimits are the production limits from SECURITY.md §6.3.
func DefaultLimits() Limits {
	return Limits{
		ConnectTimeout:      10 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		HeaderTimeout:       30 * time.Second,
		IdleTimeout:         60 * time.Second,
		TotalTimeout:        5 * time.Minute,
		MaxRedirects:        3,
		MaxResponseBytes:    10 << 20, // 10 MiB
	}
}

func (l Limits) withDefaults() Limits {
	defaults := DefaultLimits()
	if l.ConnectTimeout <= 0 {
		l.ConnectTimeout = defaults.ConnectTimeout
	}
	if l.TLSHandshakeTimeout <= 0 {
		l.TLSHandshakeTimeout = defaults.TLSHandshakeTimeout
	}
	if l.HeaderTimeout <= 0 {
		l.HeaderTimeout = defaults.HeaderTimeout
	}
	if l.IdleTimeout <= 0 {
		l.IdleTimeout = defaults.IdleTimeout
	}
	if l.TotalTimeout <= 0 {
		l.TotalTimeout = defaults.TotalTimeout
	}
	if l.MaxRedirects <= 0 {
		l.MaxRedirects = defaults.MaxRedirects
	}
	if l.MaxResponseBytes <= 0 {
		l.MaxResponseBytes = defaults.MaxResponseBytes
	}
	return l
}

// Client is the controlled HTTP client. One Client serves one provider
// policy; it never carries ambient proxy configuration or user headers.
type Client struct {
	policy   Policy
	resolver Resolver
	limits   Limits
	client   *http.Client
}

// NewClient builds a controlled client. The returned client always verifies
// TLS, never uses environment proxies, and re-validates every redirect.
func NewClient(policy Policy, resolver Resolver, limits Limits) *Client {
	if resolver == nil {
		resolver = NewNetResolver()
	}
	limits = limits.withDefaults()
	transport := &http.Transport{
		// No environment proxy: proxies could observe Authorization headers.
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return validatedDial(ctx, resolver, policy, address)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       limits.IdleTimeout,
		TLSHandshakeTimeout:   limits.TLSHandshakeTimeout,
		ResponseHeaderTimeout: limits.HeaderTimeout,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// Never allow skipping verification; field is absent by design.
		},
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= limits.MaxRedirects {
				return provider.NewSecurityError()
			}
			if err := validateRedirect(policy, req.URL); err != nil {
				return err
			}
			// Drop the Authorization header on cross-host redirects; adapters
			// re-add it only for same-origin requests.
			if !sameHost(policy.Host, req.URL.Hostname()) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
	return &Client{policy: policy, resolver: resolver, limits: limits, client: client}
}

// Do executes a validated request. The body reader is wrapped with the
// response size limit; callers must close the returned response body.
func (c *Client) Do(ctx context.Context, request *http.Request) (*http.Response, error) {
	if c == nil || c.client == nil {
		return nil, provider.NewConfigurationError()
	}
	if err := validateRequestURL(c.policy, request.Method, request.URL); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.limits.TotalTimeout)
	// The caller owns the deadline via response body reading; cancel is kept
	// alive through a body wrapper to enforce the total deadline.
	response, err := c.client.Do(request.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, mapTransportError(err)
	}
	response.Body = &deadlineBody{ReadCloser: io.LimitReader(response.Body, c.limits.MaxResponseBytes), inner: response.Body, cancel: cancel}
	return response, nil
}

type deadlineBody struct {
	ReadCloser io.Reader
	inner      io.ReadCloser
	cancel     context.CancelFunc
}

func (b *deadlineBody) Read(p []byte) (int, error) {
	return b.ReadCloser.Read(p)
}

func (b *deadlineBody) Close() error {
	b.cancel()
	return b.inner.Close()
}

// ValidateURLForPolicy exposes the exact per-request URL validation the
// client applies, so callers (and tests) can assert that the endpoint they
// build is reachable under their own policy. It performs no I/O.
func ValidateURLForPolicy(policy Policy, method, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return provider.NewSecurityError()
	}
	return validateRequestURL(policy, method, parsed)
}

// validateRequestURL enforces method and URL policy before dialing.
func validateRequestURL(policy Policy, method string, u *url.URL) error {
	if u == nil {
		return provider.NewSecurityError()
	}
	if err := validateRedirect(policy, u); err != nil {
		return err
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodHead:
		return nil
	default:
		return provider.NewSecurityError()
	}
}

// validateRedirect re-checks scheme, host, port, and URL shape for the
// initial request and for every redirect hop.
//
// Port comparison normalizes an omitted URL port to the scheme default so a
// policy pinned to "443" accepts both "https://host/v1" and
// "https://host:443/v1" — they are the same endpoint, and a strict string
// comparison would reject the canonical form.
func validateRedirect(policy Policy, u *url.URL) error {
	if u == nil {
		return provider.NewSecurityError()
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && !(scheme == "http" && policy.AllowLocal && policy.Scheme == "http") {
		return provider.NewSecurityError()
	}
	if u.User != nil {
		return provider.NewSecurityError()
	}
	if u.Fragment != "" {
		return provider.NewSecurityError()
	}
	if !sameHost(policy.Host, u.Hostname()) {
		return provider.NewSecurityError()
	}
	policyPort := policy.Port
	if policyPort == "" {
		policyPort = defaultPortForScheme(policy.Scheme)
	}
	effectivePort := u.Port()
	if effectivePort == "" {
		effectivePort = defaultPortForScheme(scheme)
	}
	if effectivePort != policyPort {
		return provider.NewSecurityError()
	}
	return nil
}

func defaultPortForScheme(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

// mapTransportError converts transport failures into the provider taxonomy.
func mapTransportError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := provider.AsProviderError(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return provider.NewCancelledError()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return provider.NewTimeoutError()
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return provider.NewTimeoutError()
	}
	return provider.NewNetworkError()
}
