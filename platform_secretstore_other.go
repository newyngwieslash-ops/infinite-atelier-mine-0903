//go:build !windows

package main

import (
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/secrets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/secretstore"
)

// newPlatformSecretStore returns the fail-closed store on platforms without a
// selected secret backend in WP-02.
func newPlatformSecretStore() secrets.Store {
	return secretstore.NewUnavailableStore()
}
