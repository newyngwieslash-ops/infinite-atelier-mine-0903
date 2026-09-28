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
	// manifests is RP-05.3's version-management service (preview, save,
	// activate). Optional: a build without it answers "unavailable" for the
	// manifest commands, and the config commands keep working.
	manifests *providers.ManifestService
}

// ProviderConfigRequest is the frontend-supplied configuration payload.
type ProviderConfigRequest struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
	BaseURL     string `json:"baseUrl"`
	// MaxConcurrency bounds this provider's jobs running at once, where ZERO means UNLIMITED
	// (ADR-0018). It is a plain int rather than a pointer because the UI's field has a value:
	// an empty box means zero, and zero is the reading that admits work.
	MaxConcurrency int `json:"maxConcurrency"`
	// RateLimitPerMinute bounds this provider's requests per rolling minute,
	// where ZERO means UNLIMITED (T07). Same convention, same zero reading.
	RateLimitPerMinute int `json:"rateLimitPerMinute"`
	// RateLimitPerMinuteSet distinguishes a request that stated the rate
	// limit (including an explicit 0) from one that omitted the field
	// (RP-02.1): the frontend sends true whenever the drawer's field was
	// rendered, so an update that touches nothing about rate limiting — or a
	// client from before the field existed — preserves the stored value.
	RateLimitPerMinuteSet bool `json:"rateLimitPerMinuteSet"`
	LocalApprove          bool `json:"localApprove"`
	Enabled               bool `json:"enabled"`
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
		ID:                    request.ID,
		Kind:                  provider.Kind(request.Kind),
		DisplayName:           request.DisplayName,
		BaseURL:               request.BaseURL,
		LocalApprove:          request.LocalApprove,
		Enabled:               request.Enabled,
		MaxConcurrency:        request.MaxConcurrency,
		RateLimitPerMinute:    request.RateLimitPerMinute,
		RateLimitPerMinuteSet: request.RateLimitPerMinuteSet,
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

// AttachProviderManifests supplies the RP-05.3 manifest version service.
// Not a Wails method.
func AttachProviderManifests(binding *ProvidersBinding, manifests *providers.ManifestService) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.manifests = manifests
	binding.mu.Unlock()
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

// --- RP-05.3: the manifest management surface on the binding ---
//
// Three narrow commands: PREVIEW (validate without storing), LIST (a
// config's version history), SAVE (store a version) and ACTIVATE (switch the
// active pointer under a revision check). The manifest never executes: it is
// data the Go side validates and the adapters interpret, and the preview's
// answer is the DOMAIN's validation result, so a hostile document is refused
// before anything persists.

// ManifestPreviewDTO is the import preview the UI shows before a save.
type ManifestPreviewDTO struct {
	Valid        bool     `json:"valid"`
	ErrorMessage string   `json:"errorMessage,omitempty"`
	Name         string   `json:"name,omitempty"`
	Capability   string   `json:"capability,omitempty"`
	SubmitPath   string   `json:"submitPath,omitempty"`
	Headers      []string `json:"headers,omitempty"`
	Async        bool     `json:"async"`
}

// PreviewManifest validates a manifest document WITHOUT storing it, and
// reports the facts a user needs to confirm: which capability, which paths
// under the configured base URL, whether it is async.
func (b *ProvidersBinding) PreviewManifest(document string) (ManifestPreviewDTO, error) {
	b.mu.RLock()
	ctx := b.ctx
	manifests := b.manifests
	b.mu.RUnlock()
	if ctx == nil || manifests == nil {
		return ManifestPreviewDTO{}, bindingUnavailable()
	}
	manifest, err := provider.LoadManifest([]byte(document))
	if err != nil {
		providerErr, _ := provider.AsProviderError(err)
		message := "The manifest is not a valid provider manifest."
		if providerErr != nil {
			message = providerErr.SafeMessage
		}
		return ManifestPreviewDTO{Valid: false, ErrorMessage: message}, nil
	}
	headers := make([]string, 0, len(manifest.Endpoints.Headers))
	for name := range manifest.Endpoints.Headers {
		headers = append(headers, name)
	}
	// Sorted, so the preview is stable for the same document.
	for i := 1; i < len(headers); i++ {
		for j := i; j > 0 && headers[j] < headers[j-1]; j-- {
			headers[j], headers[j-1] = headers[j-1], headers[j]
		}
	}
	return ManifestPreviewDTO{
		Valid:      true,
		Name:       manifest.Name,
		Capability: string(manifest.Capability),
		SubmitPath: manifest.Endpoints.SubmitPath,
		Headers:    headers,
		Async:      manifest.Capability == provider.CapabilityVideo,
	}, nil
}

// ManifestVersionDTO is one stored manifest version's transport view. The
// document itself is NOT returned here: the active version's content travels
// through GetActiveManifestDocument for an editor that wants to derive from
// it, and the rest of the history answers by hash and label.
type ManifestVersionDTO struct {
	ID            string `json:"id"`
	VersionNumber int    `json:"versionNumber"`
	ContentHash   string `json:"contentHash"`
	CreatedAt     string `json:"createdAt"`
	Active        bool   `json:"active"`
}

// ListManifestVersions returns a config's stored manifest versions, newest
// first, with the active one marked.
func (b *ProvidersBinding) ListManifestVersions(providerConfigID string) ([]ManifestVersionDTO, error) {
	b.mu.RLock()
	ctx := b.ctx
	manifests := b.manifests
	b.mu.RUnlock()
	if ctx == nil || manifests == nil {
		return nil, bindingUnavailable()
	}
	versions, err := manifests.ListManifestVersions(ctx, providerConfigID)
	if err != nil {
		return nil, toAppError(err)
	}
	active, found, err := manifests.ActiveManifest(ctx, providerConfigID)
	if err != nil {
		return nil, toAppError(err)
	}
	activeID := ""
	if found {
		activeID = active.ID
	}
	out := make([]ManifestVersionDTO, 0, len(versions))
	for _, version := range versions {
		out = append(out, ManifestVersionDTO{
			ID: version.ID, VersionNumber: version.VersionNumber,
			ContentHash: version.ContentHash, CreatedAt: version.CreatedAt,
			Active: version.ID == activeID,
		})
	}
	return out, nil
}

// SaveManifestRequest stores one validated manifest version for a provider.
type SaveManifestRequest struct {
	ProviderConfigID string `json:"providerConfigId"`
	// Document is the raw manifest JSON. The service validates it through the
	// domain before anything persists.
	Document string `json:"document"`
}

// SaveManifest stores one manifest version. Identical content maps to the
// existing version; changed content derives a new number. It does NOT
// activate — that is ActivateManifest, an explicit user decision.
func (b *ProvidersBinding) SaveManifest(request SaveManifestRequest) (ManifestVersionDTO, error) {
	b.mu.RLock()
	ctx := b.ctx
	manifests := b.manifests
	b.mu.RUnlock()
	if ctx == nil || manifests == nil {
		return ManifestVersionDTO{}, bindingUnavailable()
	}
	version, err := manifests.SaveManifest(ctx, providers.SaveManifestRequest{
		ProviderConfigID: request.ProviderConfigID,
		Document:         []byte(request.Document),
	})
	if err != nil {
		return ManifestVersionDTO{}, toAppError(err)
	}
	active, found, err := manifests.ActiveManifest(ctx, request.ProviderConfigID)
	if err != nil {
		return ManifestVersionDTO{}, toAppError(err)
	}
	return ManifestVersionDTO{
		ID: version.ID, VersionNumber: version.VersionNumber,
		ContentHash: version.ContentHash, CreatedAt: version.CreatedAt,
		Active: found && active.ID == version.ID,
	}, nil
}

// ActivateManifestRequest switches a config's active manifest version.
type ActivateManifestRequest struct {
	ProviderConfigID string `json:"providerConfigId"`
	VersionID        string `json:"versionId"`
	// ExpectedRevision is the config's revision the caller read. A mismatch
	// is a conflict a user can act on rather than a lost update.
	ExpectedRevision int64 `json:"expectedRevision"`
}

// ActivateManifest switches the active manifest version.
func (b *ProvidersBinding) ActivateManifest(request ActivateManifestRequest) error {
	b.mu.RLock()
	ctx := b.ctx
	manifests := b.manifests
	b.mu.RUnlock()
	if ctx == nil || manifests == nil {
		return bindingUnavailable()
	}
	return toAppError(manifests.ActivateManifest(ctx, providers.ActivateManifestRequest{
		ProviderConfigID: request.ProviderConfigID,
		VersionID:        request.VersionID,
		ExpectedRevision: request.ExpectedRevision,
	}))
}
