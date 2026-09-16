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
	// the job pipeline is exercisable end to end. A real adapter replaces them
	// in the media work package; until then a real video provider is not
	// silently simulated, because submission only happens for jobs whose
	// provider kind resolves to a registered adapter.
	registry.WithMediaAdapters(infraproviders.NewMockVideoAdapter(), infraproviders.NewMockAudioAdapter())
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
