package main

import (
	"context"
	"database/sql"

	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	appsecrets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/secrets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// providerWiring holds the composed provider stack so app.go stays a thin
// lifecycle owner. It is not a general dependency container.
type providerWiring struct {
	// Bindings are owned by app/main and filled in by composeProviders when a
	// writable database exists. In safe mode they stay unattached.
	secretsBinding   *desktop.SecretsBinding
	providersBinding *desktop.ProvidersBinding

	secretsService *appsecrets.Service
	configService  *appproviders.Service
	requests       *appproviders.RequestService
	// registry is the same configured provider stack the job runner uses, so
	// provider policy, secrets and audit are shared rather than duplicated.
	registry *infraproviders.Registry
}

// composeProviders builds the WP-02 provider stack over a writable database.
// It returns nil when the database is unavailable (safe mode), so provider
// bindings stay unattached and every provider call fails closed.
func composeProviders(db *sql.DB, publisher appproviders.EventPublisher) *providerWiring {
	if db == nil {
		return nil
	}
	secretService := appsecrets.NewService(newPlatformSecretStore())
	repository := database.NewProviderRepository(db)
	registry := infraproviders.NewRegistry(repository, secretService, repository)
	healthChecker := infraproviders.NewHealthChecker(registry)
	configService := appproviders.NewService(repository, registry, healthChecker)
	requests := appproviders.NewRequestService(registry, publisher)
	// Media adapters: WP-03 registers the mock video/audio implementations so
	// the job pipeline is exercisable end to end, and the mock stays registered
	// for `mock_media` — a build keeps it and configures a real provider beside
	// it rather than choosing one.
	registry.WithMediaAdapters(infraproviders.NewMockVideoAdapter(), infraproviders.NewMockAudioAdapter())
	// THE REAL VIDEO ADAPTER (WP-26), resolved for `openai_compatible`. This
	// line is what turns the async video protocol from an interface into a path
	// a user command reaches: before it, `VideoPortFor` returned `unsupported`
	// for every kind but the mock, which is the "interface with no real path"
	// shape this repository's reviews keep finding.
	//
	// It is registered unconditionally rather than behind a feature flag,
	// because a build that has the adapter and a user who has not configured a
	// video provider are two different states: the adapter refuses only when
	// somebody submits to a provider that does not answer, which is the
	// provider's answer rather than this build's opinion.
	registry.WithOpenAIVideoAdapter(infraproviders.NewOpenAIVideoAdapter(registry))
	// THE REAL SPEECH ADAPTER (WP-30), resolved for `openai_compatible` too. `AudioPortFor` resolved
	// the mock for `mock_media` and returned `unsupported` for EVERY other kind — the same sentence the
	// video side carried before WP-26, with the same consequence: a user could submit a line's speech
	// and only a deterministic mock could answer it.
	registry.WithOpenAIAudioAdapter(infraproviders.NewOpenAIAudioAdapter(registry))
	// The IMAGE mock is deliberately NOT registered here, and the reason is worth stating
	// because the absence looks like an omission.
	//
	// `ImagePortFor` resolves a provider by reading its CONFIG and dispatching on kind, and
	// `IsUserConfigurableKind` refuses to persist `mock_image` — so no config row can carry
	// that kind, and a mock registered here could never be returned. Registering it would be
	// an arm nothing can reach: the "interface with no real path" shape this repository
	// refuses, one level down.
	//
	// What a real build uses instead is a REAL image provider: `openai_compatible` and
	// `gemini_compatible` both resolve to adapters that WP-05 registered, so the storyboard
	// batch works against a configured provider. AGENT_CONTRACTS section 18.3's "CI does not
	// call a paid provider" is satisfied the other way round — a harness that wants the
	// deterministic image adapter builds its own registry and registers it, which is exactly
	// what the batch's tests do and what the mock-text adapter has always required.
	return &providerWiring{
		secretsService: secretService,
		configService:  configService,
		requests:       requests,
		registry:       registry,
	}
}

// attach publishes the startup context and services into the bindings. The
// binding structs are created before Wails startup; here they receive their
// services.
func (w *providerWiring) attach(ctx context.Context) {
	if w == nil {
		return
	}
	if w.secretsBinding != nil {
		desktop.AttachSecrets(w.secretsBinding, ctx, w.secretsService)
	}
	if w.providersBinding != nil {
		desktop.AttachProviders(w.providersBinding, ctx, w.configService, w.requests)
	}
}

// shutdown cancels any in-flight streams before the database closes.
func (w *providerWiring) shutdown() {
	if w == nil || w.requests == nil {
		return
	}
	if err := w.requests.CancelAll(); err != nil {
		// Shutdown must remain best-effort; individual cancels do not fail.
		_ = err
	}
}
