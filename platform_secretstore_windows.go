//go:build windows

package main

import (
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/secrets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/secretstore"
)

// newPlatformSecretStore returns the production secret store for this
// platform. On Windows it is the Credential Manager; there is no plaintext
// fallback anywhere.
func newPlatformSecretStore() secrets.Store {
	return secretstore.NewWindowsStore()
}
