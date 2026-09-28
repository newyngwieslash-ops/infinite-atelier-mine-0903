package desktop

import (
	"context"
	"strings"
	"testing"
	"time"

	appprov "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// manifest_binding_rp05_test.go is RP-05.3's "全路径拒绝" contract: a hostile
// manifest that enters through the BINDING is refused before storage, the
// preview reports the domain's own message, and a saved version cannot be
// activated across providers. These drive the real in-memory double the
// binding tests use, so the path tested is the path a user's request takes.

type rp05MemManifestStore struct {
	versions map[string]appprov.ManifestVersion
	counter  int
	revision int64
}

func newRP05MemManifestStore() *rp05MemManifestStore {
	return &rp05MemManifestStore{versions: map[string]appprov.ManifestVersion{}}
}

func (s *rp05MemManifestStore) SaveManifestVersion(_ context.Context, providerConfigID, manifestJSON, contentHash, createdAt string) (appprov.ManifestVersion, error) {
	for _, version := range s.versions {
		if version.ProviderConfigID == providerConfigID && version.ContentHash == contentHash {
			return version, nil
		}
	}
	s.counter++
	version := appprov.ManifestVersion{
		ID:               "mfv-test-" + strings.Repeat("0", 4) + time.Now().UTC().Format("05"),
		ProviderConfigID: providerConfigID,
		VersionNumber:    s.counter,
		ContentHash:      contentHash,
		CreatedAt:        createdAt,
	}
	// Re-derive the decoded manifest the way the real store returns it.
	manifest, err := provider.LoadManifest([]byte(manifestJSON))
	if err != nil {
		return appprov.ManifestVersion{}, err
	}
	version.Manifest = manifest
	s.versions[version.ID] = version
	return version, nil
}

func (s *rp05MemManifestStore) ListManifestVersions(_ context.Context, providerConfigID string) ([]appprov.ManifestVersion, error) {
	var out []appprov.ManifestVersion
	for _, version := range s.versions {
		if version.ProviderConfigID == providerConfigID {
			out = append(out, version)
		}
	}
	return out, nil
}

func (s *rp05MemManifestStore) GetManifestVersion(_ context.Context, versionID string) (appprov.ManifestVersion, error) {
	version, ok := s.versions[versionID]
	if !ok {
		return appprov.ManifestVersion{}, provider.NewConfigurationErrorWith("That manifest version does not exist.")
	}
	return version, nil
}

func (s *rp05MemManifestStore) GetActiveManifestVersion(_ context.Context, providerConfigID string) (appprov.ManifestVersion, bool, error) {
	return appprov.ManifestVersion{}, false, nil
}

func (s *rp05MemManifestStore) SetActiveManifestVersion(_ context.Context, providerConfigID, versionID string, expectedRevision int64) error {
	return nil
}

func (s *rp05MemManifestStore) SkillRevision(context.Context, string) (int64, error) { return 0, nil }

func (s *rp05MemManifestStore) SkillContent(_ context.Context, contentFileID string) (string, error) {
	return "", nil
}

func TestRP05PreviewRefusesHostileManifestWithDomainMessage(t *testing.T) {
	binding := &ProvidersBinding{}
	AttachProviders(binding, context.Background(), nil, nil)
	AttachProviderManifests(binding, appprov.NewManifestService(newRP05MemManifestStore(), nil, nil, func() string { return "" }))

	hostile := `{"apiVersion":"atelier.provider/v1","kind":"provider","name":"evil","capability":"audio",
		"http":{"submitPath":"https://evil.example.com/say","headers":{"Authorization":"Bearer x"}},
		"mapping":{"dataField":"d","template":"{\"model\":\"{{model}}\"}"}}`
	preview, err := binding.PreviewManifest(hostile)
	if err != nil {
		t.Fatalf("PreviewManifest returned a transport error instead of a validation answer: %v", err)
	}
	if preview.Valid {
		t.Fatal("a hostile manifest previewed as valid")
	}
	if preview.ErrorMessage == "" {
		t.Fatal("the preview carried no domain error message")
	}
}

func TestRP05SaveRefusesHostileManifestBeforeStorage(t *testing.T) {
	store := newRP05MemManifestStore()
	binding := &ProvidersBinding{}
	AttachProviders(binding, context.Background(), nil, nil)
	AttachProviderManifests(binding, appprov.NewManifestService(store, rp05NoopConfigRepo{}, nil, func() string { return "" }))

	// A credential header in the manifest: the domain refuses it, so nothing
	// lands in the store.
	hostile := `{"apiVersion":"atelier.provider/v1","kind":"provider","name":"evil","capability":"audio",
		"http":{"submitPath":"/say","headers":{"X-Api-Key":"sk-user-typed"}},"mapping":{"dataField":"d","template":"{\"model\":\"{{model}}\"}"}}`
	if _, err := binding.SaveManifest(SaveManifestRequest{ProviderConfigID: "prov-1", Document: hostile}); err == nil {
		t.Fatal("a hostile manifest was saved")
	}
	if len(store.versions) != 0 {
		t.Fatalf("the hostile manifest persisted %d version rows", len(store.versions))
	}
}

func TestRP05ActivateWithoutStoreFailsClosed(t *testing.T) {
	binding := &ProvidersBinding{}
	AttachProviders(binding, context.Background(), nil, nil)
	// No AttachProviderManifests: the manifest surface is absent.
	if err := binding.ActivateManifest(ActivateManifestRequest{ProviderConfigID: "p", VersionID: "v"}); err == nil {
		t.Fatal("activation succeeded without a manifest store")
	}
	if _, err := binding.ListManifestVersions("p"); err == nil {
		t.Fatal("listing versions succeeded without a manifest store")
	}
}

// rp05NoopConfigRepo satisfies the providers.Repository the manifest service
// consults for the config's existence; it answers an unknown config so the
// service's config lookup is exercised without a database.
type rp05NoopConfigRepo struct{}

func (rp05NoopConfigRepo) GetConfig(_ context.Context, id string) (provider.Config, error) {
	return provider.Config{ID: id, Kind: provider.KindOpenAICompatible, DisplayName: "x", BaseURL: "https://x.example.com", Enabled: true, Revision: 1}, nil
}

func (rp05NoopConfigRepo) SaveConfig(_ context.Context, _ provider.Config) error  { return nil }
func (rp05NoopConfigRepo) ListConfigs(context.Context) ([]provider.Config, error) { return nil, nil }
func (rp05NoopConfigRepo) DeleteConfig(_ context.Context, _ string) error         { return nil }
func (rp05NoopConfigRepo) SaveRequestRecord(context.Context, provider.RequestRecord) error {
	return nil
}
func (rp05NoopConfigRepo) ListHealth(context.Context) ([]provider.HealthState, error) {
	return nil, nil
}
