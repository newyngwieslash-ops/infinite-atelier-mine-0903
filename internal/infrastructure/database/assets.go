package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// AssetRepository is the SQLite implementation of the asset port. It owns
// every statement touching assets, asset versions and asset file links.
type AssetRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewAssetRepository builds the repository over a database handle.
func NewAssetRepository(db *sql.DB) *AssetRepository {
	return &AssetRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
func (r *AssetRepository) WithinTx(tx *sql.Tx) *AssetRepository {
	return &AssetRepository{db: r.db, tx: tx}
}

func (r *AssetRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

const assetSelectColumns = `SELECT id, project_id, asset_type, name, description, story_entity_id,
	current_approved_version_id, status, deleted_at, deleted_by, legacy_metadata_json,
	license, license_source, allows_export_use, created_at, updated_at, revision FROM assets`

// CreateAsset stores an asset.
func (r *AssetRepository) CreateAsset(ctx context.Context, record asset.Asset) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, description, story_entity_id, current_approved_version_id,
		 status, deleted_at, deleted_by, legacy_metadata_json, license, license_source,
		 allows_export_use, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, string(record.Type), record.Name, record.Description,
		record.StoryEntityID, record.CurrentApprovedVersionID, string(record.Status),
		formatTime(record.DeletedAt), record.DeletedBy, record.LegacyMetadata,
		record.License, record.LicenseSource, record.AllowsExportUse,
		formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return asset.ConflictError("An asset with that id already exists.")
		}
		return storageError("ASSET_WRITE_FAILED", "The asset could not be saved.", err)
	}
	return nil
}

// GetAsset returns one asset.
func (r *AssetRepository) GetAsset(ctx context.Context, id string) (asset.Asset, error) {
	conn := r.conn()
	if conn == nil {
		return asset.Asset{}, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, assetSelectColumns+` WHERE id = ?`, id)
	record, err := scanAsset(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return asset.Asset{}, asset.NotFoundError()
		}
		return asset.Asset{}, storageError("ASSET_READ_FAILED", "The asset could not be read.", err)
	}
	return record, nil
}

// ListAssets returns a project's assets newest first.
func (r *AssetRepository) ListAssets(ctx context.Context, filter assets.ListFilter) ([]asset.Asset, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	query := assetSelectColumns
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if filter.ProjectID != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if len(filter.Types) > 0 {
		placeholders := ""
		for index, value := range filter.Types {
			if index > 0 {
				placeholders += ", "
			}
			placeholders += "?"
			args = append(args, string(value))
		}
		clauses = append(clauses, "asset_type IN ("+placeholders+")")
	}
	if !filter.IncludeDeleted {
		clauses = append(clauses, "deleted_at = ''")
	}
	if conditions := joinClauses(clauses); conditions != "" {
		query += " WHERE " + conditions
	}
	query += " ORDER BY created_at DESC, id DESC"
	limit := filter.Limit
	if limit <= 0 {
		limit = 500
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, filter.Offset)

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The assets could not be read.", err)
	}
	defer rows.Close()
	var records []asset.Asset
	for rows.Next() {
		record, scanErr := scanAsset(rows)
		if scanErr != nil {
			return nil, storageError("ASSET_READ_FAILED", "The assets could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The assets could not be read.", err)
	}
	return records, nil
}

// CountAssets reports how many assets a project has, for import verification.
func (r *AssetRepository) CountAssets(ctx context.Context, projectID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM assets WHERE project_id = ?`, projectID).Scan(&count); err != nil {
		return 0, storageError("ASSET_READ_FAILED", "The assets could not be counted.", err)
	}
	return count, nil
}

// UpdateAsset persists an asset change under a revision guard.
func (r *AssetRepository) UpdateAsset(ctx context.Context, record asset.Asset, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE assets
		SET asset_type = ?, name = ?, description = ?, story_entity_id = ?, current_approved_version_id = ?,
		    status = ?, deleted_at = ?, deleted_by = ?, legacy_metadata_json = ?,
		    license = ?, license_source = ?, allows_export_use = ?, updated_at = ?,
		    revision = revision + 1
		WHERE id = ? AND revision = ?`,
		string(record.Type), record.Name, record.Description, record.StoryEntityID,
		record.CurrentApprovedVersionID, string(record.Status), formatTime(record.DeletedAt),
		record.DeletedBy, record.LegacyMetadata, record.License, record.LicenseSource,
		record.AllowsExportUse, formatTime(record.UpdatedAt), record.ID, expectedRevision)
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The asset could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The asset could not be updated.", err)
	}
	if affected == 0 {
		return asset.ConflictError("This asset changed in another window. Reload it and try again.")
	}
	return nil
}

const versionSelectColumns = `SELECT id, asset_id, version_number, status, based_on_version_id,
	parent_asset_version_id, variant_type, prompt, negative_prompt, provider_config_id,
	model_config_id, model_parameters_json, seed, generation_job_id, source_agent_run_id,
	metadata_json, created_by_type, created_by_id, legacy_metadata_json, created_at
	FROM asset_versions`

// CreateVersion stores an asset version.
func (r *AssetRepository) CreateVersion(ctx context.Context, version asset.Version) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO asset_versions
		(id, asset_id, version_number, status, based_on_version_id, parent_asset_version_id,
		 variant_type, prompt, negative_prompt, provider_config_id, model_config_id,
		 model_parameters_json, seed, generation_job_id, source_agent_run_id, metadata_json,
		 created_by_type, created_by_id, legacy_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, version.AssetID, version.VersionNumber, string(version.Status),
		version.BasedOnVersionID, version.ParentAssetVersionID, version.VariantType,
		version.Prompt, version.NegativePrompt, version.ProviderConfigID,
		version.ModelConfigID, version.ModelParameters, version.Seed, version.GenerationJobID,
		version.SourceAgentRunID, version.Metadata, string(version.CreatedByType),
		version.CreatedByID, version.LegacyMetadata, formatTime(version.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return asset.ConflictError("That version number is already used for this asset.")
		}
		return storageError("ASSET_WRITE_FAILED", "The version could not be saved.", err)
	}
	return nil
}

// GetVersion returns one asset version.
func (r *AssetRepository) GetVersion(ctx context.Context, id string) (asset.Version, error) {
	conn := r.conn()
	if conn == nil {
		return asset.Version{}, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, versionSelectColumns+` WHERE id = ?`, id)
	version, err := scanVersion(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return asset.Version{}, asset.NotFoundError()
		}
		return asset.Version{}, storageError("ASSET_READ_FAILED", "The version could not be read.", err)
	}
	return version, nil
}

// ListVersions returns an asset's versions newest first.
func (r *AssetRepository) ListVersions(ctx context.Context, assetID string) ([]asset.Version, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, versionSelectColumns+
		` WHERE asset_id = ? ORDER BY version_number DESC`, assetID)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The versions could not be read.", err)
	}
	defer rows.Close()
	var versions []asset.Version
	for rows.Next() {
		version, scanErr := scanVersion(rows)
		if scanErr != nil {
			return nil, storageError("ASSET_READ_FAILED", "The versions could not be read.", scanErr)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The versions could not be read.", err)
	}
	return versions, nil
}

// MaxVersionNumber reports the highest version number an asset has, or zero.
func (r *AssetRepository) MaxVersionNumber(ctx context.Context, assetID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	var value sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT MAX(version_number) FROM asset_versions WHERE asset_id = ?`, assetID).Scan(&value); err != nil {
		return 0, storageError("ASSET_READ_FAILED", "The versions could not be read.", err)
	}
	if !value.Valid {
		return 0, nil
	}
	return int(value.Int64), nil
}

// UpdateVersionStatus persists a version's review state.
func (r *AssetRepository) UpdateVersionStatus(ctx context.Context, versionID string, status asset.VersionStatus) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE asset_versions SET status = ? WHERE id = ?`, string(status), versionID)
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The version could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The version could not be updated.", err)
	}
	if affected == 0 {
		return asset.NotFoundError()
	}
	return nil
}

// AddFile links a committed object to a version. The foreign key rejects a
// hash that was never committed, which is what keeps a version from
// advertising bytes that do not exist.
func (r *AssetRepository) AddFile(ctx context.Context, file asset.File) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO asset_files (asset_version_id, file_hash, role, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(asset_version_id, file_hash, role) DO NOTHING`,
		file.VersionID, file.FileHash, string(file.Role), formatTime(file.CreatedAt))
	if err != nil {
		if isForeignKeyViolation(err) {
			return asset.ConflictError("That file is not available to link.")
		}
		return storageError("ASSET_WRITE_FAILED", "The file link could not be saved.", err)
	}
	return nil
}

// ListFiles returns a version's file links.
func (r *AssetRepository) ListFiles(ctx context.Context, versionID string) ([]asset.File, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx,
		`SELECT asset_version_id, file_hash, role, created_at FROM asset_files
		 WHERE asset_version_id = ? ORDER BY role ASC, file_hash ASC`, versionID)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The file links could not be read.", err)
	}
	defer rows.Close()
	var files []asset.File
	for rows.Next() {
		var file asset.File
		var role, createdAt string
		if scanErr := rows.Scan(&file.VersionID, &file.FileHash, &role, &createdAt); scanErr != nil {
			return nil, storageError("ASSET_READ_FAILED", "The file links could not be read.", scanErr)
		}
		file.Role = asset.FileRole(role)
		file.CreatedAt = parseTime(createdAt)
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The file links could not be read.", err)
	}
	return files, nil
}

// CountFiles reports how many links a version has.
func (r *AssetRepository) CountFiles(ctx context.Context, versionID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_files WHERE asset_version_id = ?`, versionID).Scan(&count); err != nil {
		return 0, storageError("ASSET_READ_FAILED", "The file links could not be counted.", err)
	}
	return count, nil
}

// scanAsset reads one asset row.
// SetAssetLicense records one asset's rights (T17): licence text, source,
// and the export-use tri-state. It is a NARROW write — a caller adjusting a
// licence must not rewrite the asset's identity columns as a side effect.
func (r *AssetRepository) SetAssetLicense(ctx context.Context, assetID, license, licenseSource, allowsExportUse string) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	if allowsExportUse != "" && allowsExportUse != "1" && allowsExportUse != "0" {
		return asset.InvalidError("The export-use answer is '', '1' or '0'.")
	}
	result, err := conn.ExecContext(ctx, `UPDATE assets
		SET license = ?, license_source = ?, allows_export_use = ?, updated_at = ?, revision = revision + 1
		WHERE id = ?`,
		license, licenseSource, allowsExportUse, formatTime(time.Now().UTC()), assetID)
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The licence could not be saved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The licence could not be saved.", err)
	}
	if affected == 0 {
		return asset.NotFoundError()
	}
	return nil
}

func scanAsset(row rowScanner) (asset.Asset, error) {
	var record asset.Asset
	var assetType, status, deletedAt, deletedBy, createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.ProjectID, &assetType, &record.Name, &record.Description,
		&record.StoryEntityID, &record.CurrentApprovedVersionID, &status, &deletedAt, &deletedBy,
		&record.LegacyMetadata, &record.License, &record.LicenseSource, &record.AllowsExportUse,
		&createdAt, &updatedAt, &record.Revision); err != nil {
		return asset.Asset{}, err
	}
	record.Type = asset.Type(assetType)
	record.Status = asset.Status(status)
	record.DeletedAt = parseTime(deletedAt)
	record.DeletedBy = deletedBy
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// scanVersion reads one asset version row.
func scanVersion(row rowScanner) (asset.Version, error) {
	var version asset.Version
	var status, createdAt string
	if err := row.Scan(&version.ID, &version.AssetID, &version.VersionNumber, &status,
		&version.BasedOnVersionID, &version.ParentAssetVersionID, &version.VariantType,
		&version.Prompt, &version.NegativePrompt, &version.ProviderConfigID,
		&version.ModelConfigID, &version.ModelParameters, &version.Seed, &version.GenerationJobID,
		&version.SourceAgentRunID, &version.Metadata, &version.CreatedByType,
		&version.CreatedByID, &version.LegacyMetadata, &createdAt); err != nil {
		return asset.Version{}, err
	}
	version.Status = asset.VersionStatus(status)
	version.CreatedAt = parseTime(createdAt)
	return version, nil
}

// Ensure the repository satisfies the application port.
var _ assets.Repository = (*AssetRepository)(nil)

const assetRelationSelectColumns = `SELECT id, source_asset_version_id, target_asset_version_id, relation_type, created_at
	FROM asset_relations`

// AddRelation records a lineage edge between two asset versions.
//
// DOMAIN_MODEL §8.5 is what PRD FR-050's "任何派生资产都能追溯父资产及变换原因"
// is built on, and the edge carries the meaning as well as the link: a
// derived_from says a version came from another, while a variant_of says they
// are alternatives.
func (r *AssetRepository) AddRelation(ctx context.Context, relation asset.Relation) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO asset_relations
		(id, source_asset_version_id, target_asset_version_id, relation_type, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		relation.ID, relation.SourceAssetVersionID, relation.TargetAssetVersionID,
		string(relation.Type), formatTime(relation.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return asset.ConflictError("That lineage relation already exists.")
		}
		if isForeignKeyViolation(err) {
			// Either end names a version that does not exist. Reporting it as a
			// conflict rather than a storage failure is what lets the caller see
			// that the reference is wrong rather than the store being broken.
			return asset.ConflictError("A lineage relation must name two existing asset versions.")
		}
		return storageError("ASSET_WRITE_FAILED", "The lineage relation could not be saved.", err)
	}
	return nil
}

// ListRelationsFrom returns the lineage edges leaving a version, which is how a
// derived asset's parents are found.
func (r *AssetRepository) ListRelationsFrom(ctx context.Context, versionID string) ([]asset.Relation, error) {
	return r.listRelations(ctx, ` WHERE source_asset_version_id = ? ORDER BY created_at ASC, id ASC`, versionID)
}

// ListRelationsTo returns the lineage edges arriving at a version, which is how
// everything derived from a version is found.
func (r *AssetRepository) ListRelationsTo(ctx context.Context, versionID string) ([]asset.Relation, error) {
	return r.listRelations(ctx, ` WHERE target_asset_version_id = ? ORDER BY created_at ASC, id ASC`, versionID)
}

func (r *AssetRepository) listRelations(ctx context.Context, clause string, versionID string) ([]asset.Relation, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, assetRelationSelectColumns+clause, versionID)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The lineage could not be read.", err)
	}
	defer rows.Close()
	var relations []asset.Relation
	for rows.Next() {
		var relation asset.Relation
		var relationType, createdAt string
		if scanErr := rows.Scan(&relation.ID, &relation.SourceAssetVersionID,
			&relation.TargetAssetVersionID, &relationType, &createdAt); scanErr != nil {
			return nil, storageError("ASSET_READ_FAILED", "The lineage could not be read.", scanErr)
		}
		relation.Type = asset.RelationType(relationType)
		relation.CreatedAt = parseTime(createdAt)
		relations = append(relations, relation)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The lineage could not be read.", err)
	}
	return relations, nil
}

const assetUsageSelectColumns = `SELECT id, asset_version_id, consumer_type, consumer_id, usage_role, required, created_at, params_json
	FROM asset_usages`

// AddUsage records that something consumes an asset version.
//
// DOMAIN_MODEL §8.6 is what makes an approval switch analysable: PRD FR-050
// requires "替换批准版本时，系统列出受影响的分镜和镜头", and that list is the set
// of usages pointing at the version being replaced.
func (r *AssetRepository) AddUsage(ctx context.Context, usage asset.Usage) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO asset_usages
		(id, asset_version_id, consumer_type, consumer_id, usage_role, required, created_at, params_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		usage.ID, usage.AssetVersionID, string(usage.ConsumerType), usage.ConsumerID,
		usage.UsageRole, boolInt(usage.Required), formatTime(usage.CreatedAt), usage.Params)
	if err != nil {
		if isUniqueViolation(err) {
			return asset.ConflictError("That version is already used by that consumer in that role.")
		}
		if isForeignKeyViolation(err) {
			return asset.ConflictError("A usage must name an existing asset version.")
		}
		return storageError("ASSET_WRITE_FAILED", "The usage could not be saved.", err)
	}
	return nil
}

// ListUsages returns everything consuming a version.
func (r *AssetRepository) ListUsages(ctx context.Context, versionID string) ([]asset.Usage, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx,
		assetUsageSelectColumns+` WHERE asset_version_id = ? ORDER BY created_at ASC, id ASC`, versionID)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The usages could not be read.", err)
	}
	defer rows.Close()
	var usages []asset.Usage
	for rows.Next() {
		var usage asset.Usage
		var consumerType, usageRole, createdAt string
		var required int
		if scanErr := rows.Scan(&usage.ID, &usage.AssetVersionID, &consumerType, &usage.ConsumerID,
			&usageRole, &required, &createdAt, &usage.Params); scanErr != nil {
			return nil, storageError("ASSET_READ_FAILED", "The usages could not be read.", scanErr)
		}
		usage.ConsumerType = asset.ConsumerType(consumerType)
		usage.UsageRole = usageRole
		usage.Required = required != 0
		usage.CreatedAt = parseTime(createdAt)
		usages = append(usages, usage)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The usages could not be read.", err)
	}
	return usages, nil
}

// UpdateUsageParams replaces ONE use's placement document — the editor
// command behind T05's readback loop.
//
// It is a NARROW write rather than a full usage update, for the same reason
// the storyboard's UpdateDirectorPlanOverrides is: a caller that read a usage
// must not be able to rewrite its identity columns (which version, which
// consumer) as a side effect of adjusting how it plays. The row's identity
// decides whether the update touches anything, and a row that is gone is a
// not-found rather than a silent no-op.
func (r *AssetRepository) UpdateUsageParams(ctx context.Context, usageID, paramsJSON string) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE asset_usages SET params_json = ? WHERE id = ?`,
		paramsJSON, usageID)
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The usage could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The usage could not be updated.", err)
	}
	if affected == 0 {
		return asset.NotFoundError()
	}
	return nil
}

// ListUsagesOfConsumer returns every use one consumer makes — the editor's
// read: a shot's audio panel lists ITS tracks by looking here.
func (r *AssetRepository) ListUsagesOfConsumer(ctx context.Context, consumerType asset.ConsumerType, consumerID string) ([]asset.Usage, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx,
		assetUsageSelectColumns+` WHERE consumer_type = ? AND consumer_id = ? ORDER BY created_at ASC, id ASC`,
		string(consumerType), consumerID)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The usages could not be read.", err)
	}
	defer rows.Close()
	var usages []asset.Usage
	for rows.Next() {
		var usage asset.Usage
		var consumerTypeValue, usageRole, createdAt string
		var required int
		if scanErr := rows.Scan(&usage.ID, &usage.AssetVersionID, &consumerTypeValue, &usage.ConsumerID,
			&usageRole, &required, &createdAt, &usage.Params); scanErr != nil {
			return nil, storageError("ASSET_READ_FAILED", "The usages could not be read.", scanErr)
		}
		usage.ConsumerType = asset.ConsumerType(consumerTypeValue)
		usage.UsageRole = usageRole
		usage.Required = required != 0
		usage.CreatedAt = parseTime(createdAt)
		usages = append(usages, usage)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The usages could not be read.", err)
	}
	return usages, nil
}

// CountUsages reports how many consumers a version has.
func (r *AssetRepository) CountUsages(ctx context.Context, versionID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_usages WHERE asset_version_id = ?`, versionID).Scan(&count); err != nil {
		return 0, storageError("ASSET_READ_FAILED", "The usages could not be read.", err)
	}
	return count, nil
}
