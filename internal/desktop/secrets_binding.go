package desktop

import (
	"context"
	"sync"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/secrets"
)

// SecretsBinding is the narrow Wails surface for provider secret lifecycle.
// It exposes status, set, replace, and delete only. It deliberately has NO
// Resolve method: secret values must never cross the binding boundary.
type SecretsBinding struct {
	mu      sync.RWMutex
	ctx     context.Context
	service *secrets.Service
}

// SetSecretRequest is the frontend-supplied write payload. The value field is
// transient: it is passed to the OS store and never persisted by the frontend
// or echoed back.
type SetSecretRequest struct {
	ProviderID string `json:"providerId"`
	Value      string `json:"value"`
}

// Status returns the safe secret status for a provider.
func (b *SecretsBinding) Status(providerID string) secrets.Status {
	if b == nil {
		return secrets.Status{ProviderID: providerID}
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.service
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return secrets.Status{ProviderID: providerID}
	}
	return service.Status(ctx, providerID)
}

// Set stores or replaces a provider secret.
func (b *SecretsBinding) Set(request SetSecretRequest) error {
	if b == nil {
		return bindingUnavailable()
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.service
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return bindingUnavailable()
	}
	if request.ProviderID == "" || request.Value == "" {
		return bindingInvalidInput()
	}
	// The plaintext exists only in this call frame and the OS credential
	// write; it is not logged, stored, or returned. Errors are mapped with the
	// same helper as provider calls so the frontend sees one error shape.
	return toAppError(service.SetSecret(ctx, request.ProviderID, []byte(request.Value)))
}

// Delete removes a provider secret.
func (b *SecretsBinding) Delete(providerID string) error {
	if b == nil {
		return bindingUnavailable()
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.service
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return bindingUnavailable()
	}
	return toAppError(service.DeleteSecret(ctx, providerID))
}

// AttachSecrets stores the Wails startup context and service. Not a Wails method.
func AttachSecrets(binding *SecretsBinding, ctx context.Context, service *secrets.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.service = service
	binding.mu.Unlock()
}
