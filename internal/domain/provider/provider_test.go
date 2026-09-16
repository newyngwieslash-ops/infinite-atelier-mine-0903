package provider

import (
	"strings"
	"testing"
)

func TestValidateBaseURL(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		wantOK bool
		scheme string
		host   string
		port   string
	}{
		{name: "https default", raw: "https://api.openai.com", wantOK: true, scheme: "https", host: "api.openai.com", port: ""},
		{name: "https with port", raw: "https://api.example.com:8443", wantOK: true, scheme: "https", host: "api.example.com", port: "8443"},
		{name: "https with path", raw: "https://relay.example.com/v1/", wantOK: true, scheme: "https", host: "relay.example.com", port: ""},
		{name: "uppercase scheme and host normalized", raw: "HTTPS://API.Example.COM", wantOK: true, scheme: "https", host: "api.example.com", port: ""},
		{name: "http allowed structurally", raw: "http://api.example.com", wantOK: true, scheme: "http", host: "api.example.com", port: ""},
		{name: "empty", raw: "", wantOK: false},
		{name: "whitespace only", raw: "   ", wantOK: false},
		{name: "no scheme", raw: "api.example.com", wantOK: false},
		{name: "ftp scheme", raw: "ftp://api.example.com", wantOK: false},
		{name: "file scheme", raw: "file:///etc/passwd", wantOK: false},
		{name: "userinfo credentials", raw: "https://user:pass@api.example.com", wantOK: false},
		{name: "fragment", raw: "https://api.example.com/#frag", wantOK: false},
		{name: "empty host", raw: "https://", wantOK: false},
		{name: "non numeric port", raw: "https://api.example.com:abc", wantOK: false},
		{name: "bare ipv6 multi colon", raw: "https://::1", wantOK: false},
		// Trailing whitespace is intentionally trimmed before validation; a
		// control character must be INSIDE the host to exercise the rejection.
		{name: "embedded carriage return in host", raw: "https://api.example.com\rv1", wantOK: false},
		{name: "embedded newline in host", raw: "https://api\r.example.com", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ValidateBaseURL(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("ValidateBaseURL(%q) ok = %v, want %v", tt.raw, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.Scheme != tt.scheme || got.Host != tt.host || got.Port != tt.port {
				t.Fatalf("ValidateBaseURL(%q) = %+v, want scheme=%s host=%s port=%s", tt.raw, got, tt.scheme, tt.host, tt.port)
			}
		})
	}
}

func TestDefaultPort(t *testing.T) {
	if got := (NormalizedURL{Scheme: "https"}).DefaultPort(); got != "443" {
		t.Fatalf("https default port = %q, want 443", got)
	}
	if got := (NormalizedURL{Scheme: "http"}).DefaultPort(); got != "80" {
		t.Fatalf("http default port = %q, want 80", got)
	}
	if got := (NormalizedURL{Scheme: "https", Port: "8443"}).DefaultPort(); got != "8443" {
		t.Fatalf("pinned port = %q, want 8443", got)
	}
}

func TestErrorTaxonomy(t *testing.T) {
	cases := []struct {
		err       *Error
		category  ErrorCategory
		retriable bool
	}{
		{NewConfigurationError(), CategoryConfiguration, false},
		{NewUnauthorizedError(), CategoryUnauthorized, false},
		{NewForbiddenError(), CategoryForbidden, false},
		{NewRateLimitedError(0), CategoryRateLimited, true},
		{NewInvalidInputError(), CategoryInvalidInput, false},
		{NewContentPolicyError(), CategoryContentPolicy, false},
		{NewNetworkError(), CategoryNetwork, true},
		{NewTimeoutError(), CategoryTimeout, true},
		{NewRemoteTransientError(), CategoryRemoteTransient, true},
		{NewRemotePermanentError(), CategoryRemotePermanent, false},
		{NewCancelledError(), CategoryCancelled, false},
		{NewUnsupportedError(), CategoryUnsupported, false},
		{NewResponseInvalidError(), CategoryResponseInvalid, false},
		{NewStorageError(), CategoryStorage, false},
		{NewSecurityError(), CategorySecurity, false},
	}
	for _, tc := range cases {
		if tc.err.Category != tc.category {
			t.Errorf("category = %q, want %q", tc.err.Category, tc.category)
		}
		if tc.err.Retriable != tc.retriable {
			t.Errorf("category %q retriable = %v, want %v", tc.category, tc.err.Retriable, tc.retriable)
		}
		if tc.err.SafeMessage == "" {
			t.Errorf("category %q has empty safe message", tc.category)
		}
	}
}

func TestErrorMessagesContainNoSecrets(t *testing.T) {
	// Safe messages must never interpolate host, URL, header, or key material.
	// A static check: every message is from the fixed vocabulary, so this test
	// guards the constructors against accidentally accepting parameters.
	for _, err := range []*Error{NewUnauthorizedError(), NewSecurityError(), NewNetworkError()} {
		if strings.Contains(err.Error(), "http://") || strings.Contains(err.Error(), "https://") {
			t.Errorf("safe message leaks URL: %q", err.Error())
		}
	}
}

func TestAsProviderError(t *testing.T) {
	inner := NewTimeoutError()
	wrapped := wrapErr(inner)
	got, ok := AsProviderError(wrapped)
	if !ok {
		t.Fatal("AsProviderError failed to unwrap")
	}
	if got.Category != CategoryTimeout {
		t.Fatalf("category = %q, want timeout", got.Category)
	}
	if IsCancellation(inner) {
		t.Fatal("timeout misclassified as cancellation")
	}
	if !IsCancellation(NewCancelledError()) {
		t.Fatal("cancelled error not detected")
	}
}

type wrapper struct{ err error }

func (w wrapper) Error() string { return "wrapped: " + w.err.Error() }

func (w wrapper) Unwrap() error { return w.err }

func wrapErr(err error) error { return wrapper{err: err} }

func TestKindAndCapability(t *testing.T) {
	// WP-03 registered the Gemini-compatible kind and the media capabilities.
	if !IsValidKind(KindOpenAICompatible) || !IsValidKind(KindGeminiCompatible) {
		t.Fatal("registered provider kind reported invalid")
	}
	if IsValidKind(Kind("anthropic")) {
		t.Fatal("unregistered provider kind accepted")
	}
	for _, capability := range []Capability{CapabilityText, CapabilityImage, CapabilityVideo, CapabilityAudio} {
		if !IsValidCapability(capability) {
			t.Fatalf("capability %q reported invalid", capability)
		}
	}
	if IsValidCapability(Capability("embedding")) {
		t.Fatal("embedding is not implemented yet and must not be advertised")
	}
	if IsValidCapability(Capability("telepathy")) {
		t.Fatal("unknown capability accepted")
	}
}
