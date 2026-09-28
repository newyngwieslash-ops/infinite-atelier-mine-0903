package database

import (
	"context"
	"database/sql"

	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// providers_manifests.go is RP-05.2's storage half: the manifest version
// rows the migration 000032 table holds, read and written through the
// application's ManifestStore port.
//
// # The two invariants the SQL enforces
//
//   - content identity: the UNIQUE (provider_config_id, content_hash) index
//     makes a re-save of an identical document map to the existing version —
//     the storage half of "same content, same version";
//   - pointer atomicity: SetActiveManifestVersion runs the pointer switch and
//     the config's revision check in ONE UPDATE, so a concurrent settings-save
//     that bumped the revision makes the activation fail loudly instead of
//     landing on a stale base.

// ManifestRepository implements providers.ManifestStore over SQL.
type ManifestRepository struct {
	db *sql.DB
}

// NewManifestRepository builds the store.
func NewManifestRepository(db *sql.DB) *ManifestRepository {
	return &ManifestRepository{db: db}
}

func (r *ManifestRepository) conn() *sql.DB { return r.db }

const manifestVersionColumns = `SELECT v.id, v.provider_config_id, v.version_number, v.content_hash, v.manifest_json, v.created_at
	FROM provider_manifest_versions v`

// SaveManifestVersion stores one validated document. Identical content maps
// to the existing version row; changed content derives the next number.
func (r *ManifestRepository) SaveManifestVersion(ctx context.Context, providerConfigID, manifestJSON, contentHash, createdAt string) (appproviders.ManifestVersion, error) {
	conn := r.conn()
	if conn == nil {
		return appproviders.ManifestVersion{}, storageError("PROVIDER_STORE_UNAVAILABLE", "The provider store is unavailable.", nil)
	}
	// Same content, same version: the unique index agrees, so the re-save is
	// a read-back of the row that already exists.
	existing, found, err := r.findByHash(ctx, providerConfigID, contentHash)
	if err != nil {
		return appproviders.ManifestVersion{}, err
	}
	if found {
		return existing, nil
	}
	var versionNumber int
	if err := conn.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version_number), 0) + 1 FROM provider_manifest_versions WHERE provider_config_id = ?`,
		providerConfigID).Scan(&versionNumber); err != nil {
		return appproviders.ManifestVersion{}, storageError("PROVIDER_WRITE_FAILED", "The manifest version could not be recorded.", err)
	}
	id := "mfv-" + contentHash[:16] + "-" + itoaVersion(versionNumber)
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO provider_manifest_versions (id, provider_config_id, version_number, manifest_json, content_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(provider_config_id, content_hash) DO NOTHING`,
		id, providerConfigID, versionNumber, manifestJSON, contentHash, createdAt); err != nil {
		return appproviders.ManifestVersion{}, storageError("PROVIDER_WRITE_FAILED", "The manifest version could not be recorded: "+err.Error(), err)
	}
	return r.findByHashRequired(ctx, providerConfigID, contentHash)
}

func (r *ManifestRepository) findByHash(ctx context.Context, providerConfigID, contentHash string) (appproviders.ManifestVersion, bool, error) {
	conn := r.conn()
	if conn == nil {
		return appproviders.ManifestVersion{}, false, storageError("PROVIDER_STORE_UNAVAILABLE", "The provider store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx,
		manifestVersionColumns+` WHERE v.provider_config_id = ? AND v.content_hash = ?`, providerConfigID, contentHash)
	version, err := r.scanVersionRow(row)
	if err == sql.ErrNoRows {
		return appproviders.ManifestVersion{}, false, nil
	}
	if err != nil {
		return appproviders.ManifestVersion{}, false, storageError("PROVIDER_READ_FAILED", "The manifest version could not be read.", err)
	}
	return version, true, nil
}

func (r *ManifestRepository) findByHashRequired(ctx context.Context, providerConfigID, contentHash string) (appproviders.ManifestVersion, error) {
	version, found, err := r.findByHash(ctx, providerConfigID, contentHash)
	if err != nil {
		return appproviders.ManifestVersion{}, err
	}
	if !found {
		return appproviders.ManifestVersion{}, storageError("PROVIDER_READ_FAILED", "The manifest version could not be read after its write.", nil)
	}
	return version, nil
}

func (r *ManifestRepository) scanVersionRow(row *sql.Row) (appproviders.ManifestVersion, error) {
	var version appproviders.ManifestVersion
	var manifestJSON, createdAt string
	if err := row.Scan(&version.ID, &version.ProviderConfigID, &version.VersionNumber, &version.ContentHash, &manifestJSON, &createdAt); err != nil {
		return appproviders.ManifestVersion{}, err
	}
	version.CreatedAt = createdAt
	manifest, err := provider.LoadManifest([]byte(manifestJSON))
	if err != nil {
		return appproviders.ManifestVersion{}, storageError("PROVIDER_READ_FAILED", "The stored manifest no longer validates.", err)
	}
	version.Manifest = manifest
	return version, nil
}

// ListManifestVersions returns a config's versions, newest first.
func (r *ManifestRepository) ListManifestVersions(ctx context.Context, providerConfigID string) ([]appproviders.ManifestVersion, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("PROVIDER_STORE_UNAVAILABLE", "The provider store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx,
		manifestVersionColumns+` WHERE v.provider_config_id = ? ORDER BY v.version_number DESC`, providerConfigID)
	if err != nil {
		return nil, storageError("PROVIDER_READ_FAILED", "The manifest versions could not be read.", err)
	}
	defer rows.Close()
	var versions []appproviders.ManifestVersion
	for rows.Next() {
		var version appproviders.ManifestVersion
		var manifestJSON, createdAt string
		if err := rows.Scan(&version.ID, &version.ProviderConfigID, &version.VersionNumber, &version.ContentHash, &manifestJSON, &createdAt); err != nil {
			return nil, storageError("PROVIDER_READ_FAILED", "The manifest versions could not be read.", err)
		}
		version.CreatedAt = createdAt
		manifest, err := provider.LoadManifest([]byte(manifestJSON))
		if err != nil {
			return nil, storageError("PROVIDER_READ_FAILED", "A stored manifest no longer validates.", err)
		}
		version.Manifest = manifest
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("PROVIDER_READ_FAILED", "The manifest versions could not be read.", err)
	}
	return versions, nil
}

// GetManifestVersion returns one version by id.
func (r *ManifestRepository) GetManifestVersion(ctx context.Context, versionID string) (appproviders.ManifestVersion, error) {
	conn := r.conn()
	if conn == nil {
		return appproviders.ManifestVersion{}, storageError("PROVIDER_STORE_UNAVAILABLE", "The provider store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, manifestVersionColumns+` WHERE v.id = ?`, versionID)
	version, err := r.scanVersionRow(row)
	if err == sql.ErrNoRows {
		return appproviders.ManifestVersion{}, provider.NewConfigurationErrorWith("That manifest version does not exist.")
	}
	if err != nil {
		return appproviders.ManifestVersion{}, storageError("PROVIDER_READ_FAILED", "The manifest version could not be read.", err)
	}
	return version, nil
}

// GetActiveManifestVersion returns the config's active version.
func (r *ManifestRepository) GetActiveManifestVersion(ctx context.Context, providerConfigID string) (appproviders.ManifestVersion, bool, error) {
	conn := r.conn()
	if conn == nil {
		return appproviders.ManifestVersion{}, false, storageError("PROVIDER_STORE_UNAVAILABLE", "The provider store is unavailable.", nil)
	}
	var versionID *string
	if err := conn.QueryRowContext(ctx,
		`SELECT active_manifest_version_id FROM provider_configs WHERE id = ?`, providerConfigID).Scan(&versionID); err != nil {
		if err == sql.ErrNoRows {
			return appproviders.ManifestVersion{}, false, provider.NewConfigurationErrorWith("That provider does not exist.")
		}
		return appproviders.ManifestVersion{}, false, storageError("PROVIDER_READ_FAILED", "The active manifest pointer could not be read.", err)
	}
	if versionID == nil {
		return appproviders.ManifestVersion{}, false, nil
	}
	version, err := r.GetManifestVersion(ctx, *versionID)
	if err != nil {
		return appproviders.ManifestVersion{}, false, err
	}
	return version, true, nil
}

// SetActiveManifestVersion switches the pointer under the config's revision
// check: one UPDATE does both, so a concurrent settings-save that bumped the
// revision makes the activation fail with a conflict instead of landing on a
// stale base.
func (r *ManifestRepository) SetActiveManifestVersion(ctx context.Context, providerConfigID, versionID string, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROVIDER_STORE_UNAVAILABLE", "The provider store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx,
		`UPDATE provider_configs
		 SET active_manifest_version_id = ?, revision = revision + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 WHERE id = ? AND revision = ?`,
		versionID, providerConfigID, expectedRevision)
	if err != nil {
		return storageError("PROVIDER_WRITE_FAILED", "The active manifest could not be switched.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("PROVIDER_WRITE_FAILED", "The active manifest could not be switched.", err)
	}
	if affected == 0 {
		return storageError("PROVIDER_CONFLICT", "That provider changed since it was read; reload and retry.", nil)
	}
	return nil
}

// itoaVersion renders a small positive integer.
func itoaVersion(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
