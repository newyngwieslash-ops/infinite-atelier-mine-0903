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
//
// # The returned slice is CONSUMED, and an implementation must hand over a copy it can lose
//
// Every adapter in this package zeroes the slice as soon as the Authorization
// header is built, which is the discipline that keeps the secret out of
// everything below that line. An implementation that returned a buffer it
// intended to reuse — a cached field, a slice into a longer array — would find
// it zeroed after the first call and would authorise NOTHING thereafter.
//
// That failure is worth the paragraph because of how it presents: `http.Header.Set`
// with a NUL-bearing value makes the transport refuse the request with
// `net/http: invalid header field value for "Authorization"` wrapped in a
// url.Error, which `mapTransportError` classifies as a *network* fault. A
// resolver that shares its buffer therefore looks like an unreachable provider
// from the second request onwards, and the secret never appears in the error to
// hint otherwise. `ResolveInternal`'s production implementation reads from the
// credential store on each call, so it satisfies this; the test doubles in this
// package copy for exactly this reason.
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
	// openaiVideo is the real asynchronous video adapter, resolved for `openai_compatible`. It is a
	// field of its own rather than replacing `video`, because the mock serves `mock_media` and both
	// must be selectable: a build keeps the mock for its own tests and a user configures a real
	// provider beside it.
	openaiVideo appjobs.VideoPort
	// openaiAudio is the real synchronous speech adapter, resolved for `openai_compatible`. It is a
	// field of its own for the same reason `openaiVideo` is: the mock serves `mock_media` and both must
	// be selectable, so one field would make one of them unreachable.
	openaiAudio appjobs.AudioPort
	// effect and openaiEffect stand for the effect capability the same pair
	// audio/openaiAudio stand for speech: the mock serves `mock_media`, the
	// real adapter serves `openai_compatible` when the build carries one, and
	// a channel without its own effect adapter refuses effect jobs instead of
	// asking the speech adapter to perform them.
	effect       appjobs.EffectPort
	openaiEffect appjobs.EffectPort

	// mockText is the deterministic text adapter. It is optional and reachable
	// only for KindMockText, which no persisted configuration can carry; see
	// WithMockTextAdapter.
	mockText providers.TextPort

	// mockImage is the deterministic image adapter, with the same guardrails as
	// mockText and for the same reason: AC-BOARD-003 is a test about several image jobs,
	// and AGENT_CONTRACTS section 18.3 forbids CI from calling a paid provider.
	mockImage appjobs.ImagePort

	// mockEmbedding is the deterministic embedding adapter, with the same guardrails
	// again. It doubles as PRD FR-120's keyword fallback rather than serving tests alone:
	// "MVP 支持 Provider Embedding 与关键词降级", and a project with no embedding provider
	// configured must not have to send its text anywhere to get a semantic channel.
	mockEmbedding providers.EmbeddingPort
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

// WithOpenAIVideoAdapter registers the real asynchronous video adapter.
//
// It is separate from `WithMediaAdapters`, which takes the MOCK, because the two are selected by
// different provider kinds and a single field would make one unreachable. The composition root calls
// both, which is where a reader can see that a build has both a mock and a real adapter.
func (r *Registry) WithOpenAIVideoAdapter(adapter appjobs.VideoPort) *Registry {
	if r == nil {
		return r
	}
	r.openaiVideo = adapter
	return r
}

// WithOpenAIAudioAdapter registers the real synchronous speech adapter.
//
// It is a third registration rather than a widening of `WithMediaAdapters`, which takes the MOCK, and
// its reasoning is the video one: the two are selected by different provider kinds and a single field
// would make one unreachable.
func (r *Registry) WithOpenAIAudioAdapter(adapter appjobs.AudioPort) *Registry {
	if r == nil {
		return r
	}
	r.openaiAudio = adapter
	return r
}

// WithEffectAdapter registers the effect adapter for the mock-media kind. It
// is a registration of its own rather than a widening of `WithMediaAdapters`
// for the reason the video/audio split states: a channel's effect capability
// is its own fact, and a registry built without one refuses effect jobs with
// an honest unsupported error instead of asking the speech adapter to perform
// them.
func (r *Registry) WithEffectAdapter(adapter appjobs.EffectPort) *Registry {
	if r == nil {
		return r
	}
	r.effect = adapter
	return r
}

// WithOpenAIEffectAdapter registers the real effect adapter for
// `openai_compatible`, the same split as the video and speech pairs.
func (r *Registry) WithOpenAIEffectAdapter(adapter appjobs.EffectPort) *Registry {
	if r == nil {
		return r
	}
	r.openaiEffect = adapter
	return r
}

// WithMockTextAdapter registers the deterministic text adapter.
//
// It is registered separately from the media adapters and reachable only for
// KindMockText, which the application layer refuses to persist and the database
// CHECK rejects, so a real provider configuration cannot select it. Keeping the
// registration at the composition root makes it visible which build carries the
// mock, in the same way WithMediaAdapters does.
func (r *Registry) WithMockTextAdapter(adapter providers.TextPort) *Registry {
	if r == nil {
		return r
	}
	r.mockText = adapter
	return r
}

// WithMockImageAdapter registers the deterministic image adapter.
//
// It is registered separately from the media adapters and reachable only for
// KindMockImage, which IsUserConfigurableKind refuses and the application layer therefore
// will not persist, so a real provider configuration cannot select it. Keeping the
// registration at the composition root makes it visible which build carries the mock, in
// the same way WithMockTextAdapter does.
func (r *Registry) WithMockImageAdapter(adapter appjobs.ImagePort) *Registry {
	if r == nil {
		return r
	}
	r.mockImage = adapter
	return r
}

// ConfigFor returns a provider's configuration metadata.
//
// It exists so a caller can decide WHETHER a provider can serve a capability before it decides
// to call one: the embedding bridge in particular must not send a project's text to a provider
// whose kind has no embedding adapter, and learning that from an "unsupported" error would mean
// discovering it at the moment the text was already in the request.
//
// It returns metadata only. Nothing here can read a secret: the secret is resolved inside an
// adapter's authorize step and nowhere else.
func (r *Registry) ConfigFor(ctx context.Context, providerID string) (provider.Config, error) {
	if r == nil || r.configs == nil {
		return provider.Config{}, provider.NewUnsupportedError()
	}
	return r.configs.GetConfig(ctx, providerID)
}

// WithMockEmbeddingAdapter registers the deterministic embedding adapter.
//
// It is registered separately from the others and reachable only for KindMockEmbedding, which
// IsUserConfigurableKind refuses and the application layer therefore will not persist, so a real
// provider configuration cannot select it. Keeping the registration at the composition root makes
// it visible which build carries the mock, in the same way WithMockTextAdapter and
// WithMockImageAdapter do.
func (r *Registry) WithMockEmbeddingAdapter(adapter providers.EmbeddingPort) *Registry {
	if r == nil {
		return r
	}
	r.mockEmbedding = adapter
	return r
}

// EmbeddingPortFor returns the embedding adapter for a provider ID.
//
// The mock is returned as the SAME instance it was registered as rather than built per call,
// because its call log is state a test set up deliberately. A registry with no mock registered
// reports "unsupported" rather than building one, so a build that did not opt in cannot be
// answered by a mock that appeared anyway.
func (r *Registry) EmbeddingPortFor(ctx context.Context, providerID string) (providers.EmbeddingPort, error) {
	if r == nil || r.configs == nil {
		return nil, provider.NewUnsupportedError()
	}
	config, err := r.configs.GetConfig(ctx, providerID)
	if err != nil {
		return nil, err
	}
	switch config.Kind {
	case provider.KindOpenAICompatible:
		return NewOpenAITextEmbeddingAdapter(r), nil
	case provider.KindMockEmbedding:
		if r.mockEmbedding == nil {
			return nil, provider.NewUnsupportedError()
		}
		return r.mockEmbedding, nil
	default:
		return nil, provider.NewUnsupportedError()
	}
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
	case provider.KindMockImage:
		// The mock is returned as the SAME instance it was registered as rather than
		// built per call, because its call log and its failure queue are state a test set
		// up deliberately: a fresh adapter per lookup would discard both.
		//
		// A registry with no mock registered reports "unsupported" rather than building
		// one, so a build that did not opt in cannot be answered by a mock that appeared
		// anyway.
		if r.mockImage == nil {
			return nil, provider.NewUnsupportedError()
		}
		return r.mockImage, nil
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
	case provider.KindOpenAICompatible:
		// A REAL adapter now resolves here (WP-26). The kind is reused rather than added, because the
		// protocol this adapter speaks — JSON in, an identifier out, poll, download — is the
		// OpenAI-compatible family's, and a new kind would have cost a migration plus a validation
		// change to buy a NAME.
		//
		// WHAT KEEPS A TEXT PROVIDER FROM BEING ASKED FOR VIDEO is not this switch: it is the provider's
		// own answer. An endpoint with no video capability refuses the submission with an HTTP error,
		// the adapter classifies it, and the job fails with a message — which is how a real provider
		// behaves and what a user needs to see, rather than a local guess about what a URL can do.
		if r.openaiVideo == nil {
			return nil, provider.NewUnsupportedError()
		}
		return r.openaiVideo, nil
	default:
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
	case provider.KindOpenAICompatible:
		// A REAL adapter now resolves here (WP-30), for the same reason the video one does: the protocol
		// — JSON in, audio bytes out — is the OpenAI-compatible family's, and a fourth kind would have
		// cost a migration plus a validation change to buy a NAME.
		//
		// WHAT KEEPS A TEXT PROVIDER FROM BEING ASKED FOR SPEECH is not this switch: it is the provider's
		// own answer. An endpoint with no speech capability refuses the request with an HTTP error, the
		// adapter classifies it, and the job fails with a message — which is how a real provider behaves
		// and what a user needs to see, rather than a local guess about what a URL can do.
		if r.openaiAudio == nil {
			return nil, provider.NewUnsupportedError()
		}
		return r.openaiAudio, nil
	default:
		return nil, provider.NewUnsupportedError()
	}
}

// EffectPortFor returns the effect adapter for a provider ID. The rules are
// AudioPortFor's: an enabled config of the mock kind resolves the mock effect
// adapter, an openai_compatible one resolves the real adapter when the build
// carries one, and every other kind is refused — a text channel asked for an
// effect is an unsupported capability, not a request.
func (r *Registry) EffectPortFor(ctx context.Context, providerID string) (appjobs.EffectPort, error) {
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
		if r.effect == nil {
			return nil, provider.NewUnsupportedError()
		}
		return r.effect, nil
	case provider.KindOpenAICompatible:
		if r.openaiEffect == nil {
			return nil, provider.NewUnsupportedError()
		}
		return r.openaiEffect, nil
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
	case provider.KindMockText:
		// The mock is returned as the SAME instance it was registered as rather than
		// built per call, because its scenario and its call log are state a caller set
		// up deliberately: a fresh adapter per lookup would discard both, and the
		// invalid-once scenario would then be invalid every time.
		//
		// A registry with no mock registered reports "unsupported" rather than
		// building one, so a build that did not opt in cannot be answered by a mock
		// that appeared anyway.
		if registry == nil || registry.mockText == nil {
			return nil, provider.NewUnsupportedError()
		}
		return registry.mockText, nil
	default:
		return nil, provider.NewUnsupportedError()
	}
}
