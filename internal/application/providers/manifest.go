package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// manifest.go is RP-05.2's versioned manifest service: the application
// commands that STORE, ACTIVATE and READ manifest versions, and the
// invariants the storage layer must not decide.
//
// # The versioning contract
//
// A manifest is the protocol snapshot a provider config runs on, so:
//
//   - content is IMMUTABLE: saving the same document twice maps to one
//     version (the store keys on content hash), and saving CHANGED content
//     derives a new version — the old one stays readable, because a job
//     half-way through an async cycle must keep reading the protocol it
//     started with;
//   - activation is a POINTER SWITCH: the active version changes atomically
//     with the config's revision check, so a concurrent settings-save cannot
//     silently lose an activation;
//   - reads name the version a caller wants, or "active" for the runtime's
//     default lookup.
//
// # The security shape
//
// Documents are validated by the DOMAIN (LoadManifest) before they are
// stored — the service refuses anything the domain refuses, so a credential
// header, a host-override path or a hostile template never reaches the
// database. The secret reference stays on the config; the manifest never
// carries one.

// ManifestVersion is one stored, immutable protocol snapshot.
type ManifestVersion struct {
	ID               string
	ProviderConfigID string
	VersionNumber    int
	ContentHash      string
	Manifest         provider.Manifest
	CreatedAt        string
}

// ManifestStore is the persistence port for manifest versions.
type ManifestStore interface {
	// SaveManifestVersion stores one validated document. The store derives
	// the version number (max+1 per config) and keys on content hash: saving
	// identical content returns the existing version.
	SaveManifestVersion(ctx context.Context, providerConfigID, manifestJSON, contentHash, createdAt string) (ManifestVersion, error)
	// ListManifestVersions returns a config's versions, newest first.
	ListManifestVersions(ctx context.Context, providerConfigID string) ([]ManifestVersion, error)
	// GetManifestVersion returns one version by id.
	GetManifestVersion(ctx context.Context, versionID string) (ManifestVersion, error)
	// GetActiveManifestVersion returns the config's active version, with
	// found=false when no manifest is active.
	GetActiveManifestVersion(ctx context.Context, providerConfigID string) (ManifestVersion, bool, error)
	// SetActiveManifestVersion switches the pointer, guarded by the config's
	// expected revision.
	SetActiveManifestVersion(ctx context.Context, providerConfigID, versionID string, expectedRevision int64) error
}

// SaveManifestRequest asks to store one manifest document.
type SaveManifestRequest struct {
	ProviderConfigID string
	// Document is the RAW JSON the user supplied. The service validates it
	// through the domain before anything persists.
	Document []byte
}

// ActivateManifestRequest switches a config's active manifest version.
type ActivateManifestRequest struct {
	ProviderConfigID string
	VersionID        string
	// ExpectedRevision is the config's revision the caller read. A mismatch
	// is a conflict: a second window saved first.
	ExpectedRevision int64
}

// ManifestService implements the manifest commands over its ports.
type ManifestService struct {
	store      ManifestStore
	repository Repository
	ids        func() (string, error)
	now        func() string
}

// NewManifestService builds the manifest commands.
func NewManifestService(store ManifestStore, repository Repository, ids func() (string, error), now func() string) *ManifestService {
	return &ManifestService{store: store, repository: repository, ids: ids, now: now}
}

// SaveManifest validates and stores one manifest document. Changed content
// derives a new version; identical content maps to the existing one.
func (s *ManifestService) SaveManifest(ctx context.Context, request SaveManifestRequest) (ManifestVersion, error) {
	if s == nil || s.store == nil {
		return ManifestVersion{}, provider.NewConfigurationErrorWith("The manifest store is unavailable.")
	}
	if s.repository == nil {
		return ManifestVersion{}, provider.NewConfigurationErrorWith("The manifest service has no provider repository.")
	}
	if _, err := s.repository.GetConfig(ctx, request.ProviderConfigID); err != nil {
		return ManifestVersion{}, err
	}
	if _, err := provider.LoadManifest(request.Document); err != nil {
		return ManifestVersion{}, err
	}
	// The stored document is the VALIDATED document re-encoded, so what the
	// database holds is exactly what the domain accepted.
	normalized := strings.TrimSpace(string(request.Document))
	sum := sha256.Sum256([]byte(normalized))
	return s.store.SaveManifestVersion(ctx, request.ProviderConfigID, normalized, hex.EncodeToString(sum[:]), s.now())
}

// ListManifestVersions returns a config's manifest history, newest first.
func (s *ManifestService) ListManifestVersions(ctx context.Context, providerConfigID string) ([]ManifestVersion, error) {
	if s == nil || s.store == nil {
		return nil, provider.NewConfigurationErrorWith("The manifest store is unavailable.")
	}
	return s.store.ListManifestVersions(ctx, providerConfigID)
}

// ActivateManifest switches the active version under a revision check.
func (s *ManifestService) ActivateManifest(ctx context.Context, request ActivateManifestRequest) error {
	if s == nil || s.store == nil {
		return provider.NewConfigurationErrorWith("The manifest store is unavailable.")
	}
	version, err := s.store.GetManifestVersion(ctx, request.VersionID)
	if err != nil {
		return err
	}
	if version.ProviderConfigID != request.ProviderConfigID {
		// A version of ANOTHER config cannot be activated here: the pointer
		// switch must never cross a provider boundary.
		return provider.NewConfigurationErrorWith("That manifest version belongs to a different provider.")
	}
	return s.store.SetActiveManifestVersion(ctx, request.ProviderConfigID, request.VersionID, request.ExpectedRevision)
}

// ActiveManifest returns the config's active protocol snapshot, with
// found=false when the config runs a builtin adapter.
func (s *ManifestService) ActiveManifest(ctx context.Context, providerConfigID string) (ManifestVersion, bool, error) {
	if s == nil || s.store == nil {
		return ManifestVersion{}, false, provider.NewConfigurationErrorWith("The manifest store is unavailable.")
	}
	return s.store.GetActiveManifestVersion(ctx, providerConfigID)
}
