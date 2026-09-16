//go:build !windows

// Package secretstore implements the secrets.Store port. On non-Windows
// platforms no backend is selected for WP-02, so the store reports itself
// unavailable and every operation fails closed. There is no plaintext
// fallback.
package secretstore

import (
	"context"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// UnavailableStore is the non-Windows stand-in. Every method fails closed.
type UnavailableStore struct{}

// NewUnavailableStore builds the fail-closed store used on non-Windows hosts.
func NewUnavailableStore() *UnavailableStore { return &UnavailableStore{} }

// Available reports false: external provider capabilities must be disabled.
func (s *UnavailableStore) Available() bool { return false }

// Put always fails closed.
func (s *UnavailableStore) Put(context.Context, string, []byte) error {
	return &provider.Error{
		Category:    provider.CategorySecurity,
		SafeMessage: "Secure secret storage is unavailable on this platform.",
	}
}

// Resolve always fails closed.
func (s *UnavailableStore) Resolve(context.Context, string) ([]byte, error) {
	return nil, &provider.Error{
		Category:    provider.CategorySecurity,
		SafeMessage: "Secure secret storage is unavailable on this platform.",
	}
}

// Delete always fails closed.
func (s *UnavailableStore) Delete(context.Context, string) error {
	return &provider.Error{
		Category:    provider.CategorySecurity,
		SafeMessage: "Secure secret storage is unavailable on this platform.",
	}
}

// Exists reports false without error: callers treat this as unconfigured.
func (s *UnavailableStore) Exists(context.Context, string) (bool, error) { return false, nil }

// DisplayHint returns an empty hint.
func (s *UnavailableStore) DisplayHint(context.Context, string) (string, error) { return "", nil }
