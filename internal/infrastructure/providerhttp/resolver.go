package providerhttp

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// Resolver abstracts DNS so tests can supply deterministic answers.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// netResolver is the production resolver.
type netResolver struct {
	resolver *net.Resolver
}

// NewNetResolver wraps the system resolver.
func NewNetResolver() Resolver {
	return &netResolver{resolver: net.DefaultResolver}
}

func (r *netResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return r.resolver.LookupIPAddr(ctx, host)
}

// validatedDial resolves a host, enforces the IP policy, and then dials the
// validated address directly. Dialing the literal IP (not the name) prevents
// DNS rebinding between validation and connection.
func validatedDial(ctx context.Context, resolver Resolver, policy Policy, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, provider.NewSecurityError()
	}
	if !sameHost(policy.Host, host) {
		return nil, provider.NewSecurityError()
	}
	if policy.Port != "" && port != policy.Port {
		return nil, provider.NewSecurityError()
	}
	// A literal IP must satisfy the policy without a lookup.
	if literal := net.ParseIP(host); literal != nil {
		if err := validateIPs(policy, host, []net.IP{literal}); err != nil {
			return nil, err
		}
		return dialLiteral(ctx, literal, port)
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, provider.NewNetworkError()
	}
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		ips = append(ips, address.IP)
	}
	if err := validateIPs(policy, host, ips); err != nil {
		return nil, err
	}
	// Dial every validated address in order; all are policy-compliant.
	// All resolved addresses are policy-compliant, so each one is tried in
	// turn; only the aggregate failure is reported, because a per-address error
	// would leak resolver details to the caller.
	for _, ip := range ips {
		conn, err := dialLiteral(ctx, ip, port)
		if err == nil {
			return conn, nil
		}
	}
	return nil, provider.NewNetworkError()
}

func dialLiteral(ctx context.Context, ip net.IP, port string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", ip.String(), err)
	}
	return conn, nil
}
