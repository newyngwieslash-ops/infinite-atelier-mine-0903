package database

import (
	"context"
	"database/sql"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// DramaSettingsRepository is the SQLite implementation of the drama project's
// configuration ports: settings, rules, style guides and provider policies.
//
// It owns every statement touching those four tables, which migration 000006
// creates. The settings table is keyed by the project rather than carrying its
// own identity (DOMAIN_MODEL §4.3 makes it a value object), so its update is
// guarded by the revision the caller read, exactly like an entity update.
type DramaSettingsRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewDramaSettingsRepository builds the repository over a database handle.
func NewDramaSettingsRepository(db *sql.DB) *DramaSettingsRepository {
	return &DramaSettingsRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
func (r *DramaSettingsRepository) WithinTx(tx *sql.Tx) *DramaSettingsRepository {
	return &DramaSettingsRepository{db: r.db, tx: tx}
}

func (r *DramaSettingsRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

const settingsSelectColumns = `SELECT project_id, target_platform, aspect_ratio, resolution,
	expected_episode_count, default_episode_duration_seconds, audience, content_rating,
	adaptation_mode, language, timezone, settings_version, revision, created_at, updated_at
	FROM project_settings`

// CreateSettings stores a project's settings.
func (r *DramaSettingsRepository) CreateSettings(ctx context.Context, settings project.Settings) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_SETTINGS_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO project_settings
		(project_id, target_platform, aspect_ratio, resolution, expected_episode_count,
		 default_episode_duration_seconds, audience, content_rating, adaptation_mode, language,
		 timezone, settings_version, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		settings.ProjectID, settings.TargetPlatform, settings.AspectRatio, settings.Resolution,
		settings.ExpectedEpisodeCount, settings.DefaultEpisodeDurationSecs, settings.Audience,
		settings.ContentRating, string(settings.AdaptationMode), settings.Language,
		settings.Timezone, settings.SettingsVersion, settings.Revision,
		formatTime(settings.CreatedAt), formatTime(settings.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return project.ConflictError("Those settings already exist.")
		}
		return storageError("PROJECT_SETTINGS_WRITE_FAILED", "The project settings could not be saved.", err)
	}
	return nil
}

// GetSettings returns one project's settings.
func (r *DramaSettingsRepository) GetSettings(ctx context.Context, projectID string) (project.Settings, error) {
	conn := r.conn()
	if conn == nil {
		return project.Settings{}, storageError("PROJECT_SETTINGS_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, settingsSelectColumns+` WHERE project_id = ?`, projectID)
	settings, err := scanSettings(row)
	if err == sql.ErrNoRows {
		return project.Settings{}, project.NotFoundError()
	}
	if err != nil {
		return project.Settings{}, storageError("PROJECT_SETTINGS_READ_FAILED", "The project settings could not be read.", err)
	}
	return settings, nil
}

// UpdateSettings persists a change under a revision guard.
func (r *DramaSettingsRepository) UpdateSettings(ctx context.Context, settings project.Settings, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_SETTINGS_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE project_settings SET
		target_platform = ?, aspect_ratio = ?, resolution = ?, expected_episode_count = ?,
		default_episode_duration_seconds = ?, audience = ?, content_rating = ?, adaptation_mode = ?,
		language = ?, timezone = ?, settings_version = ?, updated_at = ?, revision = revision + 1
		WHERE project_id = ? AND revision = ?`,
		settings.TargetPlatform, settings.AspectRatio, settings.Resolution, settings.ExpectedEpisodeCount,
		settings.DefaultEpisodeDurationSecs, settings.Audience, settings.ContentRating,
		string(settings.AdaptationMode), settings.Language, settings.Timezone, settings.SettingsVersion,
		formatTime(settings.UpdatedAt), settings.ProjectID, expectedRevision)
	if err != nil {
		return storageError("PROJECT_SETTINGS_WRITE_FAILED", "The project settings could not be saved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("PROJECT_SETTINGS_WRITE_FAILED", "The project settings could not be saved.", err)
	}
	if affected == 0 {
		return project.RevisionMismatchError()
	}
	return nil
}

func scanSettings(row rowScanner) (project.Settings, error) {
	var settings project.Settings
	var adaptationMode string
	var createdAt, updatedAt string
	err := row.Scan(
		&settings.ProjectID, &settings.TargetPlatform, &settings.AspectRatio, &settings.Resolution,
		&settings.ExpectedEpisodeCount, &settings.DefaultEpisodeDurationSecs, &settings.Audience,
		&settings.ContentRating, &adaptationMode, &settings.Language, &settings.Timezone,
		&settings.SettingsVersion, &settings.Revision, &createdAt, &updatedAt)
	if err != nil {
		return project.Settings{}, err
	}
	settings.AdaptationMode = project.AdaptationMode(adaptationMode)
	settings.CreatedAt = parseTime(createdAt)
	settings.UpdatedAt = parseTime(updatedAt)
	return settings, nil
}

const ruleSelectColumns = `SELECT id, project_id, category, name, content, strength, status,
	source_type, source_id, locked_by_user, deleted_at, deleted_by, created_at, updated_at, revision
	FROM project_rules`

// CreateRule stores a project rule.
func (r *DramaSettingsRepository) CreateRule(ctx context.Context, record project.Rule) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_RULE_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO project_rules
		(id, project_id, category, name, content, strength, status, source_type, source_id,
		 locked_by_user, deleted_at, deleted_by, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, string(record.Category), record.Name, record.Content,
		string(record.Strength), string(record.Status), string(record.SourceType), record.SourceID,
		boolInt(record.LockedByUser), formatTime(record.DeletedAt), record.DeletedBy,
		formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return project.ConflictError("A rule with that id already exists.")
		}
		return storageError("PROJECT_RULE_WRITE_FAILED", "The rule could not be saved.", err)
	}
	return nil
}

// GetRule returns one rule.
func (r *DramaSettingsRepository) GetRule(ctx context.Context, id string) (project.Rule, error) {
	conn := r.conn()
	if conn == nil {
		return project.Rule{}, storageError("PROJECT_RULE_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, ruleSelectColumns+` WHERE id = ?`, id)
	record, err := scanRule(row)
	if err == sql.ErrNoRows {
		return project.Rule{}, project.NotFoundError()
	}
	if err != nil {
		return project.Rule{}, storageError("PROJECT_RULE_READ_FAILED", "The rule could not be read.", err)
	}
	return record, nil
}

// ListRules returns a project's rules in category-then-name order.
func (r *DramaSettingsRepository) ListRules(ctx context.Context, projectID string, includeDeleted bool) ([]project.Rule, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("PROJECT_RULE_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	query := ruleSelectColumns + ` WHERE project_id = ?`
	if !includeDeleted {
		query += ` AND deleted_at = ''`
	}
	query += ` ORDER BY category ASC, name ASC, id ASC`
	rows, err := conn.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, storageError("PROJECT_RULE_READ_FAILED", "The rules could not be read.", err)
	}
	defer rows.Close()
	var records []project.Rule
	for rows.Next() {
		record, scanErr := scanRule(rows)
		if scanErr != nil {
			return nil, storageError("PROJECT_RULE_READ_FAILED", "The rules could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("PROJECT_RULE_READ_FAILED", "The rules could not be read.", err)
	}
	return records, nil
}

// UpdateRule persists a rule change under a revision guard.
//
// The guard is what makes the locked-rule rule enforceable at the storage
// layer: the caller read the rule, saw its lock state, and the update fails if
// someone else changed it in between.
func (r *DramaSettingsRepository) UpdateRule(ctx context.Context, record project.Rule, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_RULE_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE project_rules SET
		category = ?, name = ?, content = ?, strength = ?, status = ?, source_type = ?, source_id = ?,
		locked_by_user = ?, deleted_at = ?, deleted_by = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		string(record.Category), record.Name, record.Content, string(record.Strength), string(record.Status),
		string(record.SourceType), record.SourceID, boolInt(record.LockedByUser),
		formatTime(record.DeletedAt), record.DeletedBy, formatTime(record.UpdatedAt),
		record.ID, expectedRevision)
	if err != nil {
		return storageError("PROJECT_RULE_WRITE_FAILED", "The rule could not be saved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("PROJECT_RULE_WRITE_FAILED", "The rule could not be saved.", err)
	}
	if affected == 0 {
		return project.RevisionMismatchError()
	}
	return nil
}

func scanRule(row rowScanner) (project.Rule, error) {
	var record project.Rule
	var category, strength, status, sourceType, deletedAt, createdAt, updatedAt string
	var locked int
	err := row.Scan(&record.ID, &record.ProjectID, &category, &record.Name, &record.Content,
		&strength, &status, &sourceType, &record.SourceID, &locked, &deletedAt, &record.DeletedBy,
		&createdAt, &updatedAt, &record.Revision)
	if err != nil {
		return project.Rule{}, err
	}
	record.Category = project.RuleCategory(category)
	record.Strength = project.RuleStrength(strength)
	record.Status = project.RuleStatus(status)
	record.SourceType = project.RuleSourceType(sourceType)
	record.LockedByUser = locked != 0
	record.DeletedAt = parseTime(deletedAt)
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

const styleGuideSelectColumns = `SELECT id, project_id, version_number, status, based_on_version_id,
	visual_style, palette_json, lighting, composition, camera_language, negative_constraints,
	sound_direction, created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at
	FROM project_style_guides`

// CreateStyleGuide stores a style guide version.
func (r *DramaSettingsRepository) CreateStyleGuide(ctx context.Context, guide project.StyleGuide) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_STYLE_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO project_style_guides
		(id, project_id, version_number, status, based_on_version_id, visual_style, palette_json,
		 lighting, composition, camera_language, negative_constraints, sound_direction,
		 created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		guide.ID, guide.ProjectID, guide.VersionNumber, string(guide.Status), guide.BasedOnVersionID,
		guide.VisualStyle, guide.Palette, guide.Lighting, guide.Composition, guide.CameraLanguage,
		guide.NegativeConstraints, guide.SoundDirection, string(guide.CreatedByType), guide.CreatedByID,
		guide.ChangeReason, guide.LegacyMetadata, formatTime(guide.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return project.ConflictError("That style guide version already exists.")
		}
		return storageError("PROJECT_STYLE_WRITE_FAILED", "The style guide could not be saved.", err)
	}
	return nil
}

// ListStyleGuides returns a project's style guide versions, newest first.
func (r *DramaSettingsRepository) ListStyleGuides(ctx context.Context, projectID string) ([]project.StyleGuide, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("PROJECT_STYLE_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, styleGuideSelectColumns+` WHERE project_id = ? ORDER BY version_number DESC`, projectID)
	if err != nil {
		return nil, storageError("PROJECT_STYLE_READ_FAILED", "The style guides could not be read.", err)
	}
	defer rows.Close()
	var guides []project.StyleGuide
	for rows.Next() {
		var guide project.StyleGuide
		var status, createdByType, createdAt string
		if scanErr := rows.Scan(&guide.ID, &guide.ProjectID, &guide.VersionNumber, &status,
			&guide.BasedOnVersionID, &guide.VisualStyle, &guide.Palette, &guide.Lighting,
			&guide.Composition, &guide.CameraLanguage, &guide.NegativeConstraints, &guide.SoundDirection,
			&createdByType, &guide.CreatedByID, &guide.ChangeReason, &guide.LegacyMetadata, &createdAt); scanErr != nil {
			return nil, storageError("PROJECT_STYLE_READ_FAILED", "The style guides could not be read.", scanErr)
		}
		guide.Status = versioning.Status(status)
		guide.CreatedByType = versioning.CreatedByType(createdByType)
		guide.CreatedAt = parseTime(createdAt)
		guides = append(guides, guide)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("PROJECT_STYLE_READ_FAILED", "The style guides could not be read.", err)
	}
	return guides, nil
}

// MaxStyleGuideVersion reports the highest version number a project's style
// guides reach, or zero. The application numbers the next version from it,
// because the schema has a unique constraint on (project_id, version_number)
// and a count would reuse a number after a deletion.
func (r *DramaSettingsRepository) MaxStyleGuideVersion(ctx context.Context, projectID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("PROJECT_STYLE_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	var highest sql.NullInt64
	err := conn.QueryRowContext(ctx,
		`SELECT MAX(version_number) FROM project_style_guides WHERE project_id = ?`, projectID).Scan(&highest)
	if err != nil {
		return 0, storageError("PROJECT_STYLE_READ_FAILED", "The style guides could not be read.", err)
	}
	if !highest.Valid {
		return 0, nil
	}
	return int(highest.Int64), nil
}

const providerPolicySelectColumns = `SELECT id, project_id, layer, policy_json, created_at, updated_at, revision
	FROM project_provider_policies`

// UpsertProviderPolicy stores a project's policy for one layer.
//
// The table has a unique constraint on (project_id, layer), so this is an
// upsert: a project has at most one policy per layer, and re-saving a policy
// replaces it rather than accumulating rows for a reader to disambiguate.
func (r *DramaSettingsRepository) UpsertProviderPolicy(ctx context.Context, policy project.ProviderPolicy) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_POLICY_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO project_provider_policies
		(id, project_id, layer, policy_json, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id, layer) DO UPDATE SET
			policy_json = excluded.policy_json,
			updated_at = excluded.updated_at,
			revision = project_provider_policies.revision + 1`,
		policy.ID, policy.ProjectID, string(policy.Layer), policy.PolicyJSON,
		formatTime(policy.CreatedAt), formatTime(policy.UpdatedAt), policy.Revision)
	if err != nil {
		return storageError("PROJECT_POLICY_WRITE_FAILED", "The model policy could not be saved.", err)
	}
	return nil
}

// ListProviderPolicies returns a project's policies.
func (r *DramaSettingsRepository) ListProviderPolicies(ctx context.Context, projectID string) ([]project.ProviderPolicy, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("PROJECT_POLICY_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, providerPolicySelectColumns+` WHERE project_id = ? ORDER BY layer ASC`, projectID)
	if err != nil {
		return nil, storageError("PROJECT_POLICY_READ_FAILED", "The model policies could not be read.", err)
	}
	defer rows.Close()
	var policies []project.ProviderPolicy
	for rows.Next() {
		var policy project.ProviderPolicy
		var layer, createdAt, updatedAt string
		if scanErr := rows.Scan(&policy.ID, &policy.ProjectID, &layer, &policy.PolicyJSON,
			&createdAt, &updatedAt, &policy.Revision); scanErr != nil {
			return nil, storageError("PROJECT_POLICY_READ_FAILED", "The model policies could not be read.", scanErr)
		}
		policy.Layer = project.ProviderPolicyLayer(layer)
		policy.CreatedAt = parseTime(createdAt)
		policy.UpdatedAt = parseTime(updatedAt)
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("PROJECT_POLICY_READ_FAILED", "The model policies could not be read.", err)
	}
	return policies, nil
}

// Compile-time proof that the repository satisfies the port the project
// service declares for it.
var (
	_ projects.SettingsRepository = (*DramaSettingsRepository)(nil)
)

// CurrentApprovedStyleGuideVersionID returns the project's approved style guide
// version, or "" when none is approved.
func (r *DramaSettingsRepository) CurrentApprovedStyleGuideVersionID(ctx context.Context, projectID string) (string, error) {
	return currentApprovedVersionID(ctx, r.db, familyStyleGuides, projectID)
}

// ApproveStyleGuideVersion switches which guide version is approved, recording
// the event in the same transaction.
func (r *DramaSettingsRepository) ApproveStyleGuideVersion(ctx context.Context, versionID, projectID string, expectedStatus versioning.Status, record event.Event) error {
	return approveVersionWithEvent(ctx, r.db, familyStyleGuides, versionID, projectID, expectedStatus, record)
}

// GetStyleGuide returns one style guide version.
//
// It exists for the approval command, which reads the version before deciding:
// CanApprove needs the status the caller is moving away from, and the event
// needs the project the version belongs to.
func (r *DramaSettingsRepository) GetStyleGuide(ctx context.Context, id string) (project.StyleGuide, error) {
	conn := r.conn()
	if conn == nil {
		return project.StyleGuide{}, storageError("PROJECT_STYLE_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, styleGuideSelectColumns+` WHERE id = ?`, id)
	var guide project.StyleGuide
	var status, createdByType, createdAt string
	err := row.Scan(&guide.ID, &guide.ProjectID, &guide.VersionNumber, &status,
		&guide.BasedOnVersionID, &guide.VisualStyle, &guide.Palette, &guide.Lighting,
		&guide.Composition, &guide.CameraLanguage, &guide.NegativeConstraints, &guide.SoundDirection,
		&createdByType, &guide.CreatedByID, &guide.ChangeReason, &guide.LegacyMetadata, &createdAt)
	if err == sql.ErrNoRows {
		return project.StyleGuide{}, project.NotFoundError()
	}
	if err != nil {
		return project.StyleGuide{}, storageError("PROJECT_STYLE_READ_FAILED", "The style guide could not be read.", err)
	}
	guide.Status = versioning.Status(status)
	guide.CreatedByType = versioning.CreatedByType(createdByType)
	guide.CreatedAt = parseTime(createdAt)
	return guide, nil
}
