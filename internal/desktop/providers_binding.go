package desktop

import (
	"context"
	"sync"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// ProvidersBinding is the narrow Wails surface for provider configuration and
// text requests. It exposes no generic fetch, no Resolve, and no proxy.
type ProvidersBinding struct {
	mu       sync.RWMutex
	ctx      context.Context
	configs  *providers.Service
	requests *providers.RequestService
}

// ProviderConfigRequest is the frontend-supplied configuration payload.
type ProviderConfigRequest struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	DisplayName  string `json:"displayName"`
	BaseURL      string `json:"baseUrl"`
	LocalApprove bool   `json:"localApprove"`
	Enabled      bool   `json:"enabled"`
}

// TextRequestDTO is the frontend-supplied text generation payload.
type TextRequestDTO struct {
	ProviderID string                  `json:"providerId"`
	Model      string                  `json:"model"`
	Messages   []providers.TextMessage `json:"messages"`
}

// ListConfigs returns all provider configs. Configs contain a secret
// reference, never a secret value.
func (b *ProvidersBinding) ListConfigs() ([]providers.ConfigDTO, error) {
	if b == nil {
		return nil, bindingUnavailable()
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.configs
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return nil, bindingUnavailable()
	}
	configs, err := service.ListConfigs(ctx)
	if err != nil {
		return nil, toAppError(err)
	}
	return providers.ToConfigDTOs(configs), nil
}

// SaveConfig creates or updates a provider config.
func (b *ProvidersBinding) SaveConfig(request ProviderConfigRequest) (providers.ConfigDTO, error) {
	if b == nil {
		return providers.ConfigDTO{}, bindingUnavailable()
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.configs
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return providers.ConfigDTO{}, bindingUnavailable()
	}
	config, err := service.CreateOrUpdateConfig(ctx, provider.ConfigInput{
		ID:           request.ID,
		Kind:         provider.Kind(request.Kind),
		DisplayName:  request.DisplayName,
		BaseURL:      request.BaseURL,
		LocalApprove: request.LocalApprove,
		Enabled:      request.Enabled,
	})
	if err != nil {
		return providers.ConfigDTO{}, toAppError(err)
	}
	return providers.ToConfigDTO(config), nil
}

// DeleteConfig removes a provider config (secret deletion is a separate call).
func (b *ProvidersBinding) DeleteConfig(id string) error {
	if b == nil {
		return bindingUnavailable()
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.configs
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return bindingUnavailable()
	}
	return toAppError(service.DeleteConfig(ctx, id))
}

// GenerateText runs a non-streaming text request through the Go gateway.
func (b *ProvidersBinding) GenerateText(request TextRequestDTO) (providers.TextResult, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return providers.TextResult{}, err
	}
	result, err := service.Generate(ctx, providers.TextRequest{
		ProviderID: request.ProviderID,
		Model:      request.Model,
		Messages:   request.Messages,
	})
	if err != nil {
		return providers.TextResult{}, toAppError(err)
	}
	return result, nil
}

// StreamText starts a streaming text request and returns its stream ID. Deltas
// arrive as core events; cancellation uses CancelStream.
func (b *ProvidersBinding) StreamText(request TextRequestDTO) (string, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return "", err
	}
	streamID, err := service.StartStream(ctx, providers.TextRequest{
		ProviderID: request.ProviderID,
		Model:      request.Model,
		Messages:   request.Messages,
		Stream:     true,
	})
	if err != nil {
		return "", toAppError(err)
	}
	return streamID, nil
}

// CancelStream cancels an in-flight stream by ID.
func (b *ProvidersBinding) CancelStream(streamID string) error {
	service, _, err := b.requestService()
	if err != nil {
		return err
	}
	return toAppError(service.CancelStream(streamID))
}

// CheckHealth probes one provider's reachability.
func (b *ProvidersBinding) CheckHealth(providerID string) (provider.HealthState, error) {
	if b == nil {
		return provider.HealthState{}, bindingUnavailable()
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.configs
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return provider.HealthState{}, bindingUnavailable()
	}
	state, err := service.CheckHealth(ctx, providerID)
	if err != nil {
		return provider.HealthState{}, toAppError(err)
	}
	return state, nil
}

func (b *ProvidersBinding) requestService() (*providers.RequestService, context.Context, error) {
	if b == nil {
		return nil, nil, bindingUnavailable()
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.requests
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return nil, nil, bindingUnavailable()
	}
	return service, ctx, nil
}

// AttachProviders stores the Wails startup context and services. Not a Wails method.
func AttachProviders(binding *ProvidersBinding, ctx context.Context, configs *providers.Service, requests *providers.RequestService) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.configs = configs
	binding.requests = requests
	binding.mu.Unlock()
}

func bindingUnavailable() error {
	return apperror.New("DESKTOP_BINDING_UNAVAILABLE", "internal", false, "The desktop service is not available.", nil)
}

func bindingInvalidInput() error {
	return apperror.New("DESKTOP_BINDING_INVALID_INPUT", "internal", false, "The request was invalid.", nil)
}

// toAppError converts provider errors for the Wails boundary. It preserves the
// stable category as a code and never includes cause text.
func toAppError(err error) error {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*apperror.Error); ok {
		return appErr
	}
	providerErr, ok := provider.AsProviderError(err)
	if !ok {
		return apperror.New("PROVIDER_REQUEST_FAILED", "provider", false, "The provider request failed.", err)
	}
	return apperror.New(
		"PROVIDER_"+upperCategory(providerErr.Category),
		"provider",
		providerErr.Retriable,
		providerErr.SafeMessage,
		nil,
	)
}

func upperCategory(category provider.ErrorCategory) string {
	value := string(category)
	upper := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		upper = append(upper, ch)
	}
	return string(upper)
}
