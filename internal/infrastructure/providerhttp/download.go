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

// DownloadLimits bounds a provider-supplied result download. Zero values fall
// back to the defaults, which are deliberately tighter than the API client's
// because media files are large and a malicious response should not be able to
// exhaust memory or disk.
type DownloadLimits struct {
	ConnectTimeout      time.Duration
	TLSHandshakeTimeout time.Duration
	HeaderTimeout       time.Duration
	IdleTimeout         time.Duration
	TotalTimeout        time.Duration
	MaxRedirects        int
	// MaxBytes caps the downloaded body. It is enforced while streaming, not
	// after the fact, so an oversized response cannot fill the disk first.
	MaxBytes int64
}

// DefaultDownloadLimits is the WP-03 production policy for result downloads.
// 512 MiB accommodates a high-resolution video result while staying far below
// a disk-exhaustion attack; the per-capability callers may pass something
// smaller.
func DefaultDownloadLimits() DownloadLimits {
	return DownloadLimits{
		ConnectTimeout:      10 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		HeaderTimeout:       30 * time.Second,
		IdleTimeout:         60 * time.Second,
		TotalTimeout:        15 * time.Minute,
		MaxRedirects:        3,
		MaxBytes:            512 << 20,
	}
}

func (l DownloadLimits) withDefaults() DownloadLimits {
	defaults := DefaultDownloadLimits()
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
	if l.MaxBytes <= 0 {
		l.MaxBytes = defaults.MaxBytes
	}
	return l
}

// ErrDownloadTooLarge reports a body that exceeded MaxBytes. It is exported so
// callers can distinguish "the provider sent too much" from a transport error.
var ErrDownloadTooLarge = errors.New("providerhttp: download exceeded the configured byte limit")

// Downloader fetches provider-supplied result URLs under the download policy.
//
// The policy differs from the API Policy by design (docs/adr/0004): result URLs
// come from a remote service rather than from user configuration, so they are
// untrusted. Host equality is therefore not required, but every resolved
// address must be public, the scheme must be https, and each redirect hop is
// re-validated. A local-provider approval never extends to downloads.
type Downloader struct {
	resolver Resolver
	limits   DownloadLimits
	client   *http.Client
}

// NewDownloader builds a downloader. A nil resolver uses the system resolver.
func NewDownloader(resolver Resolver, limits DownloadLimits) *Downloader {
	if resolver == nil {
		resolver = NewNetResolver()
	}
	limits = limits.withDefaults()
	transport := &http.Transport{
		// No environment proxy: a proxy could observe or rewrite media bytes.
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return publicDial(ctx, resolver, address)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          2,
		IdleConnTimeout:       limits.IdleTimeout,
		TLSHandshakeTimeout:   limits.TLSHandshakeTimeout,
		ResponseHeaderTimeout: limits.HeaderTimeout,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// Never allow skipping verification; the field is absent by design.
		},
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= limits.MaxRedirects {
				return provider.NewSecurityError()
			}
			return validateDownloadURL(req.URL)
		},
	}
	return &Downloader{resolver: resolver, limits: limits, client: client}
}

// Download streams the URL into writer and returns the byte count plus the
// response content type. It never buffers the whole body.
//
// A non-nil error means the caller must discard whatever was written: the
// download is either incomplete or was refused.
func (d *Downloader) Download(ctx context.Context, rawURL string, writer io.Writer, maxBytes int64) (int64, string, error) {
	if d == nil || d.client == nil || writer == nil {
		return 0, "", provider.NewConfigurationError()
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0, "", provider.NewSecurityError()
	}
	if err := validateDownloadURL(parsed); err != nil {
		return 0, "", err
	}
	limit := maxBytes
	if limit <= 0 {
		limit = d.limits.MaxBytes
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return 0, "", provider.NewConfigurationError()
	}
	// A media fetch needs no credentials: sending the API key to a
	// provider-supplied CDN host would leak it to a third party.
	request.Header.Set("Accept", "*/*")

	ctx, cancel := context.WithTimeout(ctx, d.limits.TotalTimeout)
	defer cancel()
	response, err := d.client.Do(request.WithContext(ctx))
	if err != nil {
		return 0, "", mapTransportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, "", mapDownloadStatus(response.StatusCode)
	}

	// Read one byte past the limit so an oversized body is detected rather
	// than silently truncated into a corrupt asset.
	written, err := io.Copy(writer, io.LimitReader(response.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return written, "", mapTransportError(ctx.Err())
		}
		return written, "", provider.NewNetworkError()
	}
	if written > limit {
		return written, "", ErrDownloadTooLarge
	}
	contentType := response.Header.Get("Content-Type")
	if idx := strings.IndexByte(contentType, ';'); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	return written, contentType, nil
}

// validateDownloadURL enforces the download-specific URL rules.
func validateDownloadURL(u *url.URL) error {
	if u == nil {
		return provider.NewSecurityError()
	}
	if strings.ToLower(u.Scheme) != "https" {
		// Plain http is refused outright: a provider-supplied link has no
		// user approval behind it, so cleartext is never acceptable.
		return provider.NewSecurityError()
	}
	if u.User != nil || u.Fragment != "" {
		return provider.NewSecurityError()
	}
	if u.Hostname() == "" {
		return provider.NewSecurityError()
	}
	return nil
}

// publicDial resolves the host and refuses to connect when ANY resolved
// address is non-public. Requiring all addresses to be public is what makes
// this resistant to a DNS answer that mixes a public and a private address.
func publicDial(ctx context.Context, resolver Resolver, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, provider.NewSecurityError()
	}
	if literal := net.ParseIP(host); literal != nil {
		if IsForbiddenIP(literal) {
			return nil, provider.NewSecurityError()
		}
		return dialLiteral(ctx, literal, port)
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, provider.NewNetworkError()
	}
	if len(addresses) == 0 {
		return nil, provider.NewSecurityError()
	}
	for _, resolved := range addresses {
		if IsForbiddenIP(resolved.IP) {
			return nil, provider.NewSecurityError()
		}
	}
	// Every resolved address passed the public-address check, so each is tried
	// in turn; only the aggregate failure is reported so resolver details do
	// not reach the caller.
	for _, resolved := range addresses {
		conn, err := dialLiteral(ctx, resolved.IP, port)
		if err == nil {
			return conn, nil
		}
	}
	return nil, provider.NewNetworkError()
}

// mapDownloadStatus converts a non-200 response into the provider taxonomy.
func mapDownloadStatus(status int) error {
	switch {
	case status == http.StatusUnauthorized:
		return provider.NewUnauthorizedError()
	case status == http.StatusForbidden:
		return provider.NewForbiddenError()
	case status == http.StatusTooManyRequests:
		return provider.NewRateLimitedError(0)
	case status >= 500:
		return provider.NewRemoteTransientError()
	case status == http.StatusNotFound || status == http.StatusGone:
		// An expired signed URL is a permanent failure for this result.
		return provider.NewRemotePermanentError()
	default:
		return provider.NewResponseInvalidError()
	}
}

// DownloadLimitsOf exposes the effective limits (tests and diagnostics).
func (d *Downloader) DownloadLimitsOf() DownloadLimits {
	if d == nil {
		return DownloadLimits{}
	}
	return d.limits
}
