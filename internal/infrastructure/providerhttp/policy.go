// Package providerhttp implements the controlled HTTP client used by provider
// adapters. Every request and redirect is validated against
// docs/SECURITY.md §6: scheme, host normalization, DNS A/AAAA checks, IP
// range denial, allowlist and port policy, redirect re-validation, TLS
// verification, timeouts, and response size limits.
package providerhttp

import (
	"net"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// Policy is the immutable request policy for a provider configuration.
type Policy struct {
	// Host is the normalized, lowercase host the provider is allowed to reach.
	Host string
	// Port is the pinned port; empty means the scheme default (443/80).
	Port string
	// Scheme is "https" (default) or "http" for an explicitly approved local
	// provider.
	Scheme string
	// AllowLocal permits exactly Host:Port when it resolves to a private or
	// loopback address. It never opens the whole private network.
	AllowLocal bool
}

// forbiddenNetworks lists the ranges denied by SECURITY.md §6.2, including
// IPv4-mapped equivalents via the To4() normalization below.
var forbiddenNetworks = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"::/128",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	}
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("providerhttp: invalid forbidden network " + cidr)
		}
		networks = append(networks, network)
	}
	return networks
}()

// IsForbiddenIP reports whether an address falls in any denied range. IPv4
// addresses embedded in IPv6 (::ffff:127.0.0.1) are normalized first.
func IsForbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	normalized := ip
	if v4 := ip.To4(); v4 != nil {
		normalized = v4
	}
	if normalized.IsUnspecified() || normalized.IsLoopback() ||
		normalized.IsPrivate() || normalized.IsLinkLocalUnicast() ||
		normalized.IsLinkLocalMulticast() || normalized.IsMulticast() ||
		normalized.IsInterfaceLocalMulticast() {
		return true
	}
	for _, network := range forbiddenNetworks {
		if network.Contains(normalized) {
			return true
		}
	}
	// Cloud metadata endpoints are covered by 169.254.0.0/16 and ::/128 above,
	// but keep an explicit guard for the well-known aliases.
	if normalized.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}
	return false
}

// validateIPs applies the address policy for the exact host.
//
// Two mutually exclusive modes:
//
//   - default (AllowLocal=false): no resolved address may be in a forbidden
//     range, so public providers can never reach loopback/private/metadata.
//   - approved local provider (AllowLocal=true): EVERY resolved address must
//     be a private/loopback address, and none may be a cloud metadata
//     endpoint. Requiring all addresses to be local is what makes the
//     exception "a local provider" rather than a general escape hatch; a host
//     that resolves to a public address is not a local provider, and allowing
//     it here would permit cleartext HTTP to the public internet.
func validateIPs(policy Policy, host string, ips []net.IP) error {
	if len(ips) == 0 {
		return provider.NewSecurityError()
	}
	if !policy.AllowLocal {
		for _, ip := range ips {
			if IsForbiddenIP(ip) {
				return provider.NewSecurityError()
			}
		}
		return nil
	}
	if !sameHost(policy.Host, host) {
		return provider.NewSecurityError()
	}
	for _, ip := range ips {
		if !IsForbiddenIP(ip) {
			// A "local" provider that resolves to a public address is not the
			// address the user approved.
			return provider.NewSecurityError()
		}
		if isCloudMetadataIP(ip) {
			return provider.NewSecurityError()
		}
	}
	return nil
}

// metadataAddresses lists the well-known cloud instance-metadata endpoints.
// They stay blocked even under an explicit local-provider approval.
var metadataAddresses = []net.IP{
	net.ParseIP("169.254.169.254"), // AWS/GCP/Azure IMDS
	net.ParseIP("169.254.170.2"),   // AWS ECS task metadata
	net.ParseIP("fd00:ec2::254"),   // AWS IMDS over IPv6
}

func isCloudMetadataIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	normalized := ip
	if v4 := ip.To4(); v4 != nil {
		normalized = v4
	}
	for _, metadata := range metadataAddresses {
		if normalized.Equal(metadata) {
			return true
		}
	}
	return false
}

func sameHost(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}
