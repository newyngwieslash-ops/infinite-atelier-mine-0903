package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// ProviderRepository persists provider configuration metadata, secret
// references, and redacted request audit records. It never stores or returns
// secret values.
type ProviderRepository struct {
	db *sql.DB
}

// NewProviderRepository builds the SQLite provider repository.
func NewProviderRepository(db *sql.DB) *ProviderRepository {
	return &ProviderRepository{db: db}
}

// SaveConfig upserts a provider config and keeps its secret_references row in
// sync (reference metadata only).
func (r *ProviderRepository) SaveConfig(ctx context.Context, config provider.Config) error {
	if r == nil || r.db == nil {
		return provider.NewStorageError()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return provider.NewStorageError()
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO secret_references (id, provider_id, secret_kind, status, created_at, updated_at)
		VALUES (?, ?, 'api_key', 'missing', ?, ?)
		ON CONFLICT(provider_id) DO NOTHING`,
		config.SecretRef, config.ID, now, now); err != nil {
		return provider.NewStorageError()
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO provider_configs
		(id, kind, display_name, base_url, secret_ref, local_approved, enabled, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			kind = excluded.kind,
			display_name = excluded.display_name,
			base_url = excluded.base_url,
			secret_ref = excluded.secret_ref,
			local_approved = excluded.local_approved,
			enabled = excluded.enabled,
			revision = excluded.revision,
			updated_at = excluded.updated_at`,
		config.ID, string(config.Kind), config.DisplayName, config.BaseURL, config.SecretRef,
		boolInt(config.LocalApproved), boolInt(config.Enabled), config.Revision,
		config.CreatedAt.Format(time.RFC3339), config.UpdatedAt.Format(time.RFC3339)); err != nil {
		return provider.NewStorageError()
	}
	if err := tx.Commit(); err != nil {
		return provider.NewStorageError()
	}
	return nil
}

// ListConfigs returns all provider configs ordered by display name.
func (r *ProviderRepository) ListConfigs(ctx context.Context) ([]provider.Config, error) {
	if r == nil || r.db == nil {
		return nil, provider.NewStorageError()
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, kind, display_name, base_url, secret_ref, local_approved, enabled, revision, created_at, updated_at
		FROM provider_configs ORDER BY display_name`)
	if err != nil {
		return nil, provider.NewStorageError()
	}
	defer rows.Close()
	var configs []provider.Config
	for rows.Next() {
		config, err := scanConfig(rows)
		if err != nil {
			return nil, provider.NewStorageError()
		}
		configs = append(configs, config)
	}
	if err := rows.Err(); err != nil {
		return nil, provider.NewStorageError()
	}
	return configs, nil
}

// GetConfig returns one provider config.
func (r *ProviderRepository) GetConfig(ctx context.Context, id string) (provider.Config, error) {
	if r == nil || r.db == nil {
		return provider.Config{}, provider.NewStorageError()
	}
	row := r.db.QueryRowContext(ctx, `SELECT id, kind, display_name, base_url, secret_ref, local_approved, enabled, revision, created_at, updated_at
		FROM provider_configs WHERE id = ?`, id)
	config, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return provider.Config{}, provider.NewConfigurationError()
		}
		return provider.Config{}, provider.NewStorageError()
	}
	return config, nil
}

// DeleteConfig removes a provider config. Its secret_references row is kept
// because other state may reference it; explicit secret deletion is a separate
// secrets-service operation.
func (r *ProviderRepository) DeleteConfig(ctx context.Context, id string) error {
	if r == nil || r.db == nil {
		return provider.NewStorageError()
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM provider_configs WHERE id = ?`, id); err != nil {
		return provider.NewStorageError()
	}
	return nil
}

// SaveRequestRecord persists one redacted audit record. The record must not
// contain headers or secret values; this is enforced by the domain type.
func (r *ProviderRepository) SaveRequestRecord(ctx context.Context, record provider.RequestRecord) error {
	if r == nil || r.db == nil {
		return provider.NewStorageError()
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO provider_requests
		(id, job_id, provider_config_id, model, capability, status, http_status, latency_ms, request_id, error_code, input_units, output_units, estimated_cost, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.JobID, record.ProviderID, record.Model, string(record.Capability), record.Status,
		nullInt(record.HTTPStatus), record.LatencyMS, record.RequestID, record.ErrorCode,
		nullInt64(record.InputUnits), nullInt64(record.OutputUnits), record.EstimatedCost,
		record.CreatedAt.Format(time.RFC3339)); err != nil {
		return provider.NewStorageError()
	}
	return nil
}

// ListHealth returns the persisted secret status per provider.
func (r *ProviderRepository) ListHealth(ctx context.Context) ([]provider.HealthState, error) {
	if r == nil || r.db == nil {
		return nil, provider.NewStorageError()
	}
	rows, err := r.db.QueryContext(ctx, `SELECT provider_id, status, updated_at FROM secret_references`)
	if err != nil {
		return nil, provider.NewStorageError()
	}
	defer rows.Close()
	var states []provider.HealthState
	for rows.Next() {
		var id, status string
		var updated string
		if err := rows.Scan(&id, &status, &updated); err != nil {
			return nil, provider.NewStorageError()
		}
		states = append(states, provider.HealthState{
			ProviderID: id,
			Healthy:    status == "configured",
			Detail:     status,
			CheckedAt:  parseTime(updated),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, provider.NewStorageError()
	}
	return states, nil
}

// UpdateSecretStatus refreshes the non-secret secret_references status row for
// a provider (configured/missing/unavailable).
func (r *ProviderRepository) UpdateSecretStatus(ctx context.Context, providerID, status, displayHint string) error {
	if r == nil || r.db == nil {
		return provider.NewStorageError()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := r.db.ExecContext(ctx, `INSERT INTO secret_references (id, provider_id, secret_kind, display_hint, status, created_at, updated_at)
		VALUES (?, ?, 'api_key', ?, ?, ?, ?)
		ON CONFLICT(provider_id) DO UPDATE SET display_hint = excluded.display_hint, status = excluded.status, updated_at = excluded.updated_at`,
		provider.SecretRefValue(providerID), providerID, displayHint, status, now, now); err != nil {
		return provider.NewStorageError()
	}
	return nil
}

type configScanner interface {
	Scan(dest ...any) error
}

func scanConfig(scanner configScanner) (provider.Config, error) {
	var config provider.Config
	var kind string
	var localApproved, enabled int
	var createdAt, updatedAt string
	if err := scanner.Scan(&config.ID, &kind, &config.DisplayName, &config.BaseURL, &config.SecretRef,
		&localApproved, &enabled, &config.Revision, &createdAt, &updatedAt); err != nil {
		return provider.Config{}, err
	}
	config.Kind = provider.Kind(kind)
	config.LocalApproved = localApproved == 1
	config.Enabled = enabled == 1
	config.CreatedAt = parseTime(createdAt)
	config.UpdatedAt = parseTime(updatedAt)
	return config, nil
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
