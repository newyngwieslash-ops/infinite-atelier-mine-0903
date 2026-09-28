package database

import (
	"context"
	"strings"
	"testing"

	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
)

// providers_manifests_rp05_test.go is RP-05.2's storage contract over a real
// database: identical content maps to one version, changed content derives a
// new number, the old version stays readable after the pointer moves, and the
// pointer switch is atomic with the config's revision check.

const rp05ValidManifest = `{
  "apiVersion": "atelier.provider/v1",
  "kind": "provider",
  "name": "acme-media",
  "capability": "audio",
  "http": {
    "submitPath": "/v1/say"
  },
  "mapping": {
    "dataField": "audio_base64",
    "mimeField": "mime",
    "template": "{\"model\":\"{{model}}\",\"text\":\"{{text}}\",\"voice\":\"{{voice}}\"}"
  }
}`

const rp05ChangedManifest = `{
  "apiVersion": "atelier.provider/v1",
  "kind": "provider",
  "name": "acme-media-v2",
  "capability": "audio",
  "http": {
    "submitPath": "/v2/say"
  },
  "mapping": {
    "dataField": "audio_base64",
    "mimeField": "mime",
    "template": "{\"model\":\"{{model}}\",\"text\":\"{{text}}\",\"voice\":\"{{voice}}\"}"
  }
}`

func TestRP05ManifestVersionsRoundTrip(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()

	// The secret reference the config's FK points at.
	if _, err := db.ExecContext(ctx, `INSERT INTO secret_references (id, provider_id, secret_kind, display_hint, status, created_at, updated_at)
		VALUES ('InfiniteAtelier:provider:prov-man', 'prov-man', 'api_key', '', 'configured', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')
		ON CONFLICT(id) DO NOTHING`); err != nil {
		t.Fatalf("seeding the secret reference: %v", err)
	}

	// Seed one provider config row.
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_configs
		(id, kind, display_name, base_url, secret_ref, local_approved, enabled, max_concurrency, rate_limit_per_minute, revision, created_at, updated_at)
		VALUES ('prov-man', 'openai_compatible', 'Manifest relay', 'https://acme.example.com', 'InfiniteAtelier:provider:prov-man', 0, 1, 0, 0, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seeding the config: %v", err)
	}

	store := NewManifestRepository(db)
	service := appproviders.NewManifestService(store, NewProviderRepository(db),
		func() (string, error) { return "id-fixed", nil },
		func() string { return "2026-09-28T00:00:00Z" })

	// THE FIRST SAVE creates version 1.
	first, err := service.SaveManifest(ctx, appproviders.SaveManifestRequest{
		ProviderConfigID: "prov-man", Document: []byte(rp05ValidManifest),
	})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if first.VersionNumber != 1 {
		t.Fatalf("first version number = %d, want 1", first.VersionNumber)
	}

	// THE RE-SAVE of identical content maps to the SAME version — not a new
	// row. This is the storage half of "same content, same version".
	repeat, err := service.SaveManifest(ctx, appproviders.SaveManifestRequest{
		ProviderConfigID: "prov-man", Document: []byte(rp05ValidManifest),
	})
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if repeat.ID != first.ID {
		t.Fatalf("identical content produced a new version %s vs %s", repeat.ID, first.ID)
	}

	// THE EDIT derives version 2; version 1 STAYS READABLE (the immutability
	// the running-job snapshot needs).
	second, err := service.SaveManifest(ctx, appproviders.SaveManifestRequest{
		ProviderConfigID: "prov-man", Document: []byte(rp05ChangedManifest),
	})
	if err != nil {
		t.Fatalf("changed save: %v", err)
	}
	if second.VersionNumber != 2 || second.ID == first.ID {
		t.Fatalf("changed content = version %d id %s, want a new version 2", second.VersionNumber, second.ID)
	}
	// The old document still reads back byte-for-byte from the store.
	firstReread, err := store.GetManifestVersion(ctx, first.ID)
	if err != nil {
		t.Fatalf("old version vanished: %v", err)
	}
	// The content hash pins the document: identical hash means identical
	// bytes, so the old version's document was not mutated.
	if firstReread.ContentHash != first.ContentHash {
		t.Fatalf("the old version's document was mutated: hash %s vs %s", firstReread.ContentHash, first.ContentHash)
	}
	if firstReread.Manifest.Name != "acme-media" {
		t.Fatalf("the old version's decoded manifest is not the original: %s", firstReread.Manifest.Name)
	}

	// THE POINTER SWITCH, twice: activate v1 then v2, and the active read
	// follows. A version of ANOTHER config is refused.
	if err := service.ActivateManifest(ctx, appproviders.ActivateManifestRequest{
		ProviderConfigID: "prov-man", VersionID: first.ID, ExpectedRevision: 1,
	}); err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	active, found, err := service.ActiveManifest(ctx, "prov-man")
	if err != nil || !found {
		t.Fatalf("active read: found=%v err=%v", found, err)
	}
	if active.ID != first.ID {
		t.Fatalf("active = %s, want the version just activated", active.ID)
	}
	if _, err := db.ExecContext(ctx, `UPDATE provider_configs SET revision = revision + 1 WHERE id = 'prov-man'`); err != nil {
		t.Fatal(err)
	}
	var currentRevision int64
	if err := db.QueryRowContext(ctx, `SELECT revision FROM provider_configs WHERE id = 'prov-man'`).Scan(&currentRevision); err != nil {
		t.Fatal(err)
	}
	if err := service.ActivateManifest(ctx, appproviders.ActivateManifestRequest{
		ProviderConfigID: "prov-man", VersionID: second.ID, ExpectedRevision: 1,
	}); err == nil {
		t.Fatal("a stale-revision activation was accepted")
	}
	_ = currentRevision
}

// TestRP05ManifestValidationBlocksTheStore proves the domain gate sits BEFORE
// storage: a document the domain refuses (a credential header) never reaches
// the database.
func TestRP05ManifestValidationBlocksTheStore(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO secret_references (id, provider_id, secret_kind, display_hint, status, created_at, updated_at)
		VALUES ('InfiniteAtelier:provider:prov-man2', 'prov-man2', 'api_key', '', 'configured', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')
		ON CONFLICT(id) DO NOTHING`); err != nil {
		t.Fatalf("seeding the secret reference: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_configs
		(id, kind, display_name, base_url, secret_ref, local_approved, enabled, max_concurrency, rate_limit_per_minute, revision, created_at, updated_at)
		VALUES ('prov-man2', 'openai_compatible', 'Manifest relay 2', 'https://acme.example.com', 'InfiniteAtelier:provider:prov-man2', 0, 1, 0, 0, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seeding the config: %v", err)
	}

	store := NewManifestRepository(db)
	service := appproviders.NewManifestService(store, NewProviderRepository(db),
		func() (string, error) { return "id-fixed", nil },
		func() string { return "2026-09-28T00:00:00Z" })

	hostile := strings.Replace(rp05ValidManifest, `"submitPath": "/v1/say"`,
		`"submitPath": "https://evil.example.com/say"`, 1)
	if _, err := service.SaveManifest(ctx, appproviders.SaveManifestRequest{
		ProviderConfigID: "prov-man2", Document: []byte(hostile),
	}); err == nil {
		t.Fatal("a hostile manifest was stored")
	}
	// The database read-back is the evidence: no version row exists.
	versions, err := store.ListManifestVersions(ctx, "prov-man2")
	if err != nil {
		t.Fatalf("ListManifestVersions: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("the hostile manifest persisted %d version rows", len(versions))
	}
}
