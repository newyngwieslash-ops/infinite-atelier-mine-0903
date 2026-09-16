package provider

import (
	"strings"
)

// NormalizedURL is the sanitized, validated base URL carried by a provider
// config. It is derived from user input by the application layer and only
// contains non-secret, policy-checkable fields.
type NormalizedURL struct {
	Scheme string
	Host   string
	Port   string
	Path   string
}

// ValidateBaseURL performs the domain-level structural validation of a
// provider base URL. It rejects credentials, fragments, non-HTTP(S) schemes,
// and empty hosts. Full SSRF/DNS checks happen in Infrastructure with the
// actual resolver; this keeps the domain rule pure and testable.
func ValidateBaseURL(raw string) (NormalizedURL, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return NormalizedURL{}, false
	}
	if strings.ContainsAny(trimmed, "@#") {
		// userinfo or fragment: forbidden by SECURITY.md §6.1
		return NormalizedURL{}, false
	}
	scheme, rest, ok := strings.Cut(trimmed, "://")
	if !ok {
		return NormalizedURL{}, false
	}
	switch strings.ToLower(scheme) {
	case "http", "https":
	default:
		return NormalizedURL{}, false
	}
	if rest == "" {
		return NormalizedURL{}, false
	}
	hostPort := rest
	path := ""
	if index := strings.IndexAny(rest, "/\\"); index >= 0 {
		hostPort = rest[:index]
		path = rest[index:]
	}
	if hostPort == "" {
		return NormalizedURL{}, false
	}
	host := hostPort
	port := ""
	if strings.Count(hostPort, ":") == 1 {
		hostPart, portPart, split := strings.Cut(hostPort, ":")
		if split && portPart != "" {
			if !isDigits(portPart) {
				return NormalizedURL{}, false
			}
			host = hostPart
			port = portPart
		}
	} else if strings.Count(hostPort, ":") > 1 {
		// Bare IPv6 literal or malformed multi-colon host: reject here; the
		// infrastructure resolver handles bracketed IPv6 forms.
		return NormalizedURL{}, false
	}
	if host == "" {
		return NormalizedURL{}, false
	}
	for _, r := range host {
		// Reject control characters, space, and DEL anywhere in the host.
		if r <= ' ' || r == 0x7f {
			return NormalizedURL{}, false
		}
	}
	return NormalizedURL{
		Scheme: strings.ToLower(scheme),
		Host:   strings.ToLower(host),
		Port:   port,
		Path:   strings.TrimRight(path, "/"),
	}, true
}

// HasPort reports whether the URL pinned an explicit port.
func (u NormalizedURL) HasPort() bool { return u.Port != "" }

// DefaultPort returns the scheme's default port when none is pinned.
func (u NormalizedURL) DefaultPort() string {
	if u.Port != "" {
		return u.Port
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

func isDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(value) > 0
}
