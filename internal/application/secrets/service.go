// Package secrets is the application-layer boundary for OS-backed secret
// storage. The port mirrors docs/ARCHITECTURE.md §15: Resolve is for
// Infrastructure provider calls only and is never surfaced as a Wails binding.
package secrets

import (
	"context"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// Status is the safe view of one secret entry. It never contains the value.
type Status struct {
	ProviderID  string `json:"providerId"`
	Configured  bool   `json:"configured"`
	DisplayHint string `json:"displayHint"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	Available   bool   `json:"available"`
}

// Store is the SecretStore port (ARCHITECTURE §15). Implementations:
// - Windows Credential Manager (production);
// - non-Windows: unavailable (fail closed);
// - fake in-memory (tests only).
type Store interface {
	// Put stores a secret value and returns a stable reference. The value
	// must not be retained by the implementation after the call.
	Put(ctx context.Context, ref string, value []byte) error
	// Resolve returns the secret value for Infrastructure provider calls only.
	// It must never be exposed through Wails bindings.
	Resolve(ctx context.Context, ref string) ([]byte, error)
	// Delete removes the secret entry. Deleting a missing entry is not an error.
	Delete(ctx context.Context, ref string) error
	// Exists reports whether a secret is configured for the reference.
	Exists(ctx context.Context, ref string) (bool, error)
	// DisplayHint returns a safe suffix (for example the last four
	// characters) or an empty string when the backend cannot provide one.
	DisplayHint(ctx context.Context, ref string) (string, error)
	// Available reports whether the backend is usable on this platform.
	Available() bool
}

// Service is the application use-case surface for secret lifecycle. It never
// returns secret values; callers pass values in for storage and receive only
// Status views back.
type Service struct {
	store Store
}

// NewService builds a secrets service over a Store port.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// StoreAvailable reports whether secret storage is usable.
func (s *Service) StoreAvailable() bool {
	return s != nil && s.store != nil && s.store.Available()
}

// SetSecret stores or replaces the secret for a provider. It validates the
// provider ID shape and refuses empty values.
func (s *Service) SetSecret(ctx context.Context, providerID string, value []byte) error {
	if s == nil || s.store == nil {
		return provider.NewConfigurationError()
	}
	if !ValidProviderID(providerID) {
		return provider.NewConfigurationError()
	}
	if len(value) == 0 {
		return provider.NewConfigurationError()
	}
	if err := s.store.Put(ctx, secretRefFor(providerID), value); err != nil {
		return mapStoreError(err)
	}
	return nil
}

// DeleteSecret removes the secret for a provider.
func (s *Service) DeleteSecret(ctx context.Context, providerID string) error {
	if s == nil || s.store == nil {
		return provider.NewConfigurationError()
	}
	if !ValidProviderID(providerID) {
		return provider.NewConfigurationError()
	}
	if err := s.store.Delete(ctx, secretRefFor(providerID)); err != nil {
		return mapStoreError(err)
	}
	return nil
}

// Status returns the safe view for a provider's secret. When the store is
// unavailable the status is reported configured=false, available=false
// instead of an error, matching the fail-closed UX contract.
func (s *Service) Status(ctx context.Context, providerID string) Status {
	status := Status{ProviderID: providerID}
	if s == nil || s.store == nil || !ValidProviderID(providerID) {
		return status
	}
	if !s.store.Available() {
		status.Available = false
		return status
	}
	status.Available = true
	exists, err := s.store.Exists(ctx, secretRefFor(providerID))
	if err != nil {
		// Fail closed: report unconfigured on backend failure.
		return status
	}
	status.Configured = exists
	if exists {
		hint, hintErr := s.store.DisplayHint(ctx, secretRefFor(providerID))
		if hintErr == nil {
			status.DisplayHint = hint
		}
	}
	return status
}

// ResolveInternal is the Go-internal resolution used by provider
// infrastructure to build the Authorization header. It is intentionally not
// part of any desktop binding and must never be called from the frontend path.
func (s *Service) ResolveInternal(ctx context.Context, providerID string) ([]byte, error) {
	if s == nil || s.store == nil || !s.store.Available() {
		return nil, &provider.Error{
			Category:    provider.CategorySecurity,
			SafeMessage: "Secure secret storage is unavailable.",
		}
	}
	if !ValidProviderID(providerID) {
		return nil, provider.NewConfigurationError()
	}
	value, err := s.store.Resolve(ctx, secretRefFor(providerID))
	if err != nil {
		return nil, mapStoreError(err)
	}
	if len(value) == 0 {
		return nil, provider.NewConfigurationError()
	}
	return value, nil
}

// ValidProviderID enforces the ref-safe provider ID shape: lowercase
// alphanumeric with hyphens, 1..64 characters. This prevents forging
// credential-manager targets that collide with other applications' entries.
func ValidProviderID(providerID string) bool {
	return provider.ValidKindlessID(providerID)
}

// secretRefFor maps a provider ID to its stable secret reference. The
// namespace lives in the domain package so the persisted reference and the
// credential-manager target cannot drift apart.
func secretRefFor(providerID string) string {
	return provider.SecretRefValue(providerID)
}

// SecretRef returns the reference for a provider ID (used by repositories to
// persist the non-secret reference).
func SecretRef(providerID string) string { return secretRefFor(providerID) }

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if providerErr, ok := provider.AsProviderError(err); ok {
		return providerErr
	}
	return &provider.Error{
		Category:    provider.CategoryStorage,
		SafeMessage: "Secure secret storage failed.",
	}
}
