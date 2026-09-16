// Package providers hosts trusted, compiled-in provider adapters. WP-02
// registers exactly one: an OpenAI-compatible text adapter. Adapters must be
// built from trusted Go code; no user script or dynamic plugin path exists.
package providers

import (
	"context"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// SecretResolver resolves a provider's secret for outbound authorization. It
// is implemented by the secrets service and only used inside adapter code.
type SecretResolver interface {
	ResolveInternal(ctx context.Context, providerID string) ([]byte, error)
}

// ConfigSource loads provider configuration metadata by ID.
type ConfigSource interface {
	GetConfig(ctx context.Context, id string) (provider.Config, error)
}

// AuditSink persists redacted request records.
type AuditSink interface {
	SaveRequestRecord(ctx context.Context, record provider.RequestRecord) error
}

// Registry resolves a configured provider to its compiled-in adapter.
type Registry struct {
	configs ConfigSource
	secrets SecretResolver
	audit   AuditSink
	now     func() time.Time

	// Media adapters are optional. WP-03 supplies the mocks; a real adapter is
	// registered here when the media work package lands.
	video appjobs.VideoPort
	audio appjobs.AudioPort
}

// NewRegistry builds the provider registry.
func NewRegistry(configs ConfigSource, secrets SecretResolver, audit AuditSink) *Registry {
	return &Registry{configs: configs, secrets: secrets, audit: audit, now: time.Now}
}

// WithMediaAdapters registers the video and audio capability adapters. They are
// reachable only for providers whose kind is KindMockMedia, which the settings
// UI does not offer, so a production configuration cannot select them.
// Keeping this separate from NewRegistry makes it obvious at the composition
// root which media adapters (mock or real) a build carries.
func (r *Registry) WithMediaAdapters(video appjobs.VideoPort, audio appjobs.AudioPort) *Registry {
	if r == nil {
		return r
	}
	r.video = video
	r.audio = audio
	return r
}

// ImagePortFor returns the image adapter for a provider ID.
func (r *Registry) ImagePortFor(ctx context.Context, providerID string) (appjobs.ImagePort, error) {
	if r == nil || r.configs == nil {
		return nil, provider.NewUnsupportedError()
	}
	config, err := r.configs.GetConfig(ctx, providerID)
	if err != nil {
		return nil, err
	}
	switch config.Kind {
	case provider.KindOpenAICompatible:
		return NewOpenAIImageAdapter(r), nil
	case provider.KindGeminiCompatible:
		return NewGeminiImageAdapter(r), nil
	default:
		return nil, provider.NewUnsupportedError()
	}
}

// VideoPortFor returns the video adapter for a provider ID.
//
// The adapter is resolved through the same kind switch as the image and text
// capabilities, so an unregistered provider fails closed instead of silently
// receiving whatever mock happens to be installed.
func (r *Registry) VideoPortFor(ctx context.Context, providerID string) (appjobs.VideoPort, error) {
	if r == nil || r.configs == nil {
		return nil, provider.NewUnsupportedError()
	}
	config, err := r.configs.GetConfig(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !config.Enabled {
		return nil, provider.NewConfigurationError()
	}
	switch config.Kind {
	case provider.KindMockMedia:
		if r.video == nil {
			return nil, provider.NewUnsupportedError()
		}
		return r.video, nil
	default:
		// A real async video adapter does not exist yet: reporting "unsupported"
		// is honest, whereas returning the mock would fabricate a result.
		return nil, provider.NewUnsupportedError()
	}
}

// AudioPortFor returns the audio adapter for a provider ID, with the same
// resolution rules as video.
func (r *Registry) AudioPortFor(ctx context.Context, providerID string) (appjobs.AudioPort, error) {
	if r == nil || r.configs == nil {
		return nil, provider.NewUnsupportedError()
	}
	config, err := r.configs.GetConfig(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !config.Enabled {
		return nil, provider.NewConfigurationError()
	}
	switch config.Kind {
	case provider.KindMockMedia:
		if r.audio == nil {
			return nil, provider.NewUnsupportedError()
		}
		return r.audio, nil
	default:
		return nil, provider.NewUnsupportedError()
	}
}

// TextPortFor implements the application TextPortResolver: it loads the
// provider config and returns the adapter for its registered kind.
func (r *Registry) TextPortFor(ctx context.Context, providerID string) (providers.TextPort, error) {
	port, _, err := r.TextProviderFor(ctx, providerID)
	return port, err
}

// TextProviderFor returns the text adapter for a provider ID after validating
// that the provider kind is registered. It fails closed for unsupported
// kinds.
func (r *Registry) TextProviderFor(ctx context.Context, providerID string) (providers.TextPort, provider.Config, error) {
	if r == nil || r.configs == nil {
		return nil, provider.Config{}, provider.NewUnsupportedError()
	}
	config, err := r.configs.GetConfig(ctx, providerID)
	if err != nil {
		return nil, provider.Config{}, err
	}
	adapter, err := buildTextAdapter(config.Kind, r)
	if err != nil {
		return nil, provider.Config{}, err
	}
	return adapter, config, nil
}

// buildTextAdapter maps a kind to its adapter constructor. Adding a kind here
// is the only way to make it reachable.
func buildTextAdapter(kind provider.Kind, registry *Registry) (providers.TextPort, error) {
	switch kind {
	case provider.KindOpenAICompatible, provider.KindGeminiCompatible:
		return NewOpenAITextAdapter(registry), nil
	default:
		return nil, provider.NewUnsupportedError()
	}
}
