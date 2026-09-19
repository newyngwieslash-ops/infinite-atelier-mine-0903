package database

import (
	"context"
	"database/sql"

	storyboardapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// StoryboardRepository is the SQLite implementation of the storyboard ports: it
// owns every statement touching director plans, storyboards, storyboard
// versions, storyboard items and panel versions.
//
// Column names come from migration 000010, which is the authority for the
// tables this type writes. The panel table carries no revision column, because
// migration 000010 stores one immutable row per panel version: the only column
// a panel write changes is approved_image_asset_version_id, and that write is
// guarded through the panel's parent storyboard item (see ApprovePanelImage).
type StoryboardRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewStoryboardRepository builds the repository over a database handle.
func NewStoryboardRepository(db *sql.DB) *StoryboardRepository {
	return &StoryboardRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
func (r *StoryboardRepository) WithinTx(tx *sql.Tx) *StoryboardRepository {
	return &StoryboardRepository{db: r.db, tx: tx}
}

func (r *StoryboardRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

const directorPlanSelectColumns = `SELECT id, episode_id, version_number, status, based_on_version_id,
	script_version_id, visual_rhythm, camera_language, color_lighting, staging, continuity_rules,
	audio_direction, shot_overrides_json, source_agent_run_id, created_by_type, created_by_id,
	change_reason, legacy_metadata_json, created_at FROM director_plan_versions`

// CreateDirectorPlanVersion stores a director plan version.
func (r *StoryboardRepository) CreateDirectorPlanVersion(ctx context.Context, version storyboard.DirectorPlanVersion) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO director_plan_versions
		(id, episode_id, version_number, status, based_on_version_id, script_version_id, visual_rhythm,
		 camera_language, color_lighting, staging, continuity_rules, audio_direction, shot_overrides_json,
		 source_agent_run_id, created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, version.EpisodeID, version.VersionNumber, string(version.Status),
		version.BasedOnVersionID, version.ScriptVersionID, version.VisualRhythm, version.CameraLanguage,
		version.ColorLighting, version.Staging, version.ContinuityRules, version.AudioDirection,
		version.ShotOverridesJSON, version.SourceAgentRunID, string(version.CreatedByType),
		version.CreatedByID, version.ChangeReason, version.LegacyMetadata, formatTime(version.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return storyboard.ConflictError("That version number is already used for this episode.")
		}
		if isForeignKeyViolation(err) {
			return storyboard.InvalidError("That episode or script version does not exist.")
		}
		return storageError("STORYBOARD_WRITE_FAILED", "The director plan could not be saved.", err)
	}
	return nil
}

// GetDirectorPlanVersion returns one director plan version.
func (r *StoryboardRepository) GetDirectorPlanVersion(ctx context.Context, id string) (storyboard.DirectorPlanVersion, error) {
	conn := r.conn()
	if conn == nil {
		return storyboard.DirectorPlanVersion{}, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, directorPlanSelectColumns+` WHERE id = ?`, id)
	version, err := scanDirectorPlanVersion(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return storyboard.DirectorPlanVersion{}, storyboard.NotFoundError()
		}
		return storyboard.DirectorPlanVersion{}, storageError("STORYBOARD_READ_FAILED", "The director plan could not be read.", err)
	}
	return version, nil
}

// MaxDirectorPlanVersionNumber reports the highest version number an episode's
// plans have, or zero when the episode has none.
func (r *StoryboardRepository) MaxDirectorPlanVersionNumber(ctx context.Context, episodeID string) (int, error) {
	return r.maxNumber(ctx, `SELECT MAX(version_number) FROM director_plan_versions WHERE episode_id = ?`, episodeID)
}

const storyboardSelectColumns = `SELECT id, episode_id, current_version_id, created_at, updated_at, revision
	FROM storyboards`

// CreateStoryboard stores a storyboard identity.
func (r *StoryboardRepository) CreateStoryboard(ctx context.Context, record storyboard.Storyboard) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO storyboards
		(id, episode_id, current_version_id, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?)`,
		record.ID, record.EpisodeID, record.CurrentVersionID, formatTime(record.CreatedAt),
		formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return storyboard.ConflictError("That episode already has a storyboard.")
		}
		if isForeignKeyViolation(err) {
			return storyboard.InvalidError("That episode does not exist.")
		}
		return storageError("STORYBOARD_WRITE_FAILED", "The storyboard could not be saved.", err)
	}
	return nil
}

// GetStoryboard returns one storyboard identity.
func (r *StoryboardRepository) GetStoryboard(ctx context.Context, id string) (storyboard.Storyboard, error) {
	conn := r.conn()
	if conn == nil {
		return storyboard.Storyboard{}, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyboardSelectColumns+` WHERE id = ?`, id)
	record, err := scanStoryboard(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return storyboard.Storyboard{}, storyboard.NotFoundError()
		}
		return storyboard.Storyboard{}, storageError("STORYBOARD_READ_FAILED", "The storyboard could not be read.", err)
	}
	return record, nil
}

// GetStoryboardByEpisode returns an episode's storyboard identity.
func (r *StoryboardRepository) GetStoryboardByEpisode(ctx context.Context, episodeID string) (storyboard.Storyboard, error) {
	conn := r.conn()
	if conn == nil {
		return storyboard.Storyboard{}, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyboardSelectColumns+` WHERE episode_id = ?`, episodeID)
	record, err := scanStoryboard(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return storyboard.Storyboard{}, storyboard.NotFoundError()
		}
		return storyboard.Storyboard{}, storageError("STORYBOARD_READ_FAILED", "The storyboard could not be read.", err)
	}
	return record, nil
}

const storyboardVersionSelectColumns = `SELECT id, storyboard_id, version_number, status, script_version_id,
	director_plan_version_id, based_on_version_id, source_agent_run_id, created_by_type, created_by_id,
	change_reason, legacy_metadata_json, created_at FROM storyboard_versions`

// CreateStoryboardVersion stores a storyboard version.
func (r *StoryboardRepository) CreateStoryboardVersion(ctx context.Context, version storyboard.StoryboardVersion) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO storyboard_versions
		(id, storyboard_id, version_number, status, script_version_id, director_plan_version_id,
		 based_on_version_id, source_agent_run_id, created_by_type, created_by_id, change_reason,
		 legacy_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, version.StoryboardID, version.VersionNumber, string(version.Status),
		version.ScriptVersionID, version.DirectorPlanVersionID, version.BasedOnVersionID,
		version.SourceAgentRunID, string(version.CreatedByType), version.CreatedByID,
		version.ChangeReason, version.LegacyMetadata, formatTime(version.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return storyboard.ConflictError("That version number is already used for this storyboard.")
		}
		if isForeignKeyViolation(err) {
			return storyboard.InvalidError("That storyboard, script version or director plan does not exist.")
		}
		return storageError("STORYBOARD_WRITE_FAILED", "The storyboard version could not be saved.", err)
	}
	return nil
}

// GetStoryboardVersion returns one storyboard version.
func (r *StoryboardRepository) GetStoryboardVersion(ctx context.Context, id string) (storyboard.StoryboardVersion, error) {
	conn := r.conn()
	if conn == nil {
		return storyboard.StoryboardVersion{}, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyboardVersionSelectColumns+` WHERE id = ?`, id)
	version, err := scanStoryboardVersion(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return storyboard.StoryboardVersion{}, storyboard.NotFoundError()
		}
		return storyboard.StoryboardVersion{}, storageError("STORYBOARD_READ_FAILED", "The storyboard version could not be read.", err)
	}
	return version, nil
}

// MaxStoryboardVersionNumber reports the highest version number a storyboard
// has, or zero when it has none.
func (r *StoryboardRepository) MaxStoryboardVersionNumber(ctx context.Context, storyboardID string) (int, error) {
	return r.maxNumber(ctx, `SELECT MAX(version_number) FROM storyboard_versions WHERE storyboard_id = ?`, storyboardID)
}

const storyboardItemSelectColumns = `SELECT id, storyboard_version_id, shot_id, ordinal, shot_size,
	camera_angle, camera_movement, duration_seconds, visual_description, action_description,
	dialogue_audio_summary, continuity_notes, status, created_at, updated_at, revision FROM storyboard_items`

// CreateStoryboardItem stores one shot's row in a storyboard version.
func (r *StoryboardRepository) CreateStoryboardItem(ctx context.Context, item storyboard.StoryboardItem) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO storyboard_items
		(id, storyboard_version_id, shot_id, ordinal, shot_size, camera_angle, camera_movement,
		 duration_seconds, visual_description, action_description, dialogue_audio_summary,
		 continuity_notes, status, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.StoryboardVersionID, item.ShotID, item.Ordinal, item.ShotSize, item.CameraAngle,
		item.CameraMovement, item.DurationSeconds, item.VisualDescription, item.ActionDescription,
		item.DialogueAudioSummary, item.ContinuityNotes, string(item.Status),
		formatTime(item.CreatedAt), formatTime(item.UpdatedAt), item.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			// The schema has two unique constraints on these columns, and the
			// driver message does not say which one fired, so the message names
			// both: the application checks them individually before it gets here,
			// and this is the backstop for the race it cannot close.
			return storyboard.ConflictError("This storyboard version already holds that shot, or already uses that position.")
		}
		if isForeignKeyViolation(err) {
			return storyboard.InvalidError("That storyboard version does not exist.")
		}
		return storageError("STORYBOARD_WRITE_FAILED", "The storyboard item could not be saved.", err)
	}
	return nil
}

// GetStoryboardItem returns one storyboard item.
func (r *StoryboardRepository) GetStoryboardItem(ctx context.Context, id string) (storyboard.StoryboardItem, error) {
	conn := r.conn()
	if conn == nil {
		return storyboard.StoryboardItem{}, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyboardItemSelectColumns+` WHERE id = ?`, id)
	item, err := scanStoryboardItem(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return storyboard.StoryboardItem{}, storyboard.NotFoundError()
		}
		return storyboard.StoryboardItem{}, storageError("STORYBOARD_READ_FAILED", "The storyboard item could not be read.", err)
	}
	return item, nil
}

// FindStoryboardItemByShot returns the item a version holds for a shot.
func (r *StoryboardRepository) FindStoryboardItemByShot(ctx context.Context, storyboardVersionID, shotID string) (storyboard.StoryboardItem, bool, error) {
	conn := r.conn()
	if conn == nil {
		return storyboard.StoryboardItem{}, false, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyboardItemSelectColumns+
		` WHERE storyboard_version_id = ? AND shot_id = ?`, storyboardVersionID, shotID)
	item, err := scanStoryboardItem(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return storyboard.StoryboardItem{}, false, nil
		}
		return storyboard.StoryboardItem{}, false, storageError("STORYBOARD_READ_FAILED", "The storyboard item could not be read.", err)
	}
	return item, true, nil
}

// FindStoryboardItemByOrdinal returns the item at one position of a version.
func (r *StoryboardRepository) FindStoryboardItemByOrdinal(ctx context.Context, storyboardVersionID string, ordinal int) (storyboard.StoryboardItem, bool, error) {
	conn := r.conn()
	if conn == nil {
		return storyboard.StoryboardItem{}, false, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyboardItemSelectColumns+
		` WHERE storyboard_version_id = ? AND ordinal = ?`, storyboardVersionID, ordinal)
	item, err := scanStoryboardItem(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return storyboard.StoryboardItem{}, false, nil
		}
		return storyboard.StoryboardItem{}, false, storageError("STORYBOARD_READ_FAILED", "The storyboard item could not be read.", err)
	}
	return item, true, nil
}

// ListStoryboardItems returns a version's items in ordinal order.
func (r *StoryboardRepository) ListStoryboardItems(ctx context.Context, storyboardVersionID string) ([]storyboard.StoryboardItem, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, storyboardItemSelectColumns+
		` WHERE storyboard_version_id = ? ORDER BY ordinal ASC, id ASC`, storyboardVersionID)
	if err != nil {
		return nil, storageError("STORYBOARD_READ_FAILED", "The storyboard items could not be read.", err)
	}
	defer rows.Close()
	var items []storyboard.StoryboardItem
	for rows.Next() {
		item, scanErr := scanStoryboardItem(rows)
		if scanErr != nil {
			return nil, storageError("STORYBOARD_READ_FAILED", "The storyboard items could not be read.", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("STORYBOARD_READ_FAILED", "The storyboard items could not be read.", err)
	}
	return items, nil
}

const panelSelectColumns = `SELECT id, storyboard_item_id, version_number, status, based_on_version_id,
	visual_prompt, negative_prompt, reference_policy_json, approved_image_asset_version_id,
	source_agent_run_id, created_by_type, created_by_id, change_reason, legacy_metadata_json,
	created_at FROM storyboard_panel_versions`

// CreatePanelVersion stores a panel version.
func (r *StoryboardRepository) CreatePanelVersion(ctx context.Context, panel storyboard.StoryboardPanelVersion) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO storyboard_panel_versions
		(id, storyboard_item_id, version_number, status, based_on_version_id, visual_prompt,
		 negative_prompt, reference_policy_json, approved_image_asset_version_id, source_agent_run_id,
		 created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		panel.ID, panel.StoryboardItemID, panel.VersionNumber, string(panel.Status),
		panel.BasedOnVersionID, panel.VisualPrompt, panel.NegativePrompt, panel.ReferencePolicyJSON,
		panel.ApprovedImageAssetVersionID, panel.SourceAgentRunID, string(panel.CreatedByType),
		panel.CreatedByID, panel.ChangeReason, panel.LegacyMetadata, formatTime(panel.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return storyboard.ConflictError("That version number is already used for this panel.")
		}
		if isForeignKeyViolation(err) {
			return storyboard.InvalidError("That storyboard item does not exist.")
		}
		return storageError("STORYBOARD_WRITE_FAILED", "The panel version could not be saved.", err)
	}
	return nil
}

// GetPanelVersion returns one panel version.
func (r *StoryboardRepository) GetPanelVersion(ctx context.Context, id string) (storyboard.StoryboardPanelVersion, error) {
	conn := r.conn()
	if conn == nil {
		return storyboard.StoryboardPanelVersion{}, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, panelSelectColumns+` WHERE id = ?`, id)
	panel, err := scanPanelVersion(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return storyboard.StoryboardPanelVersion{}, storyboard.NotFoundError()
		}
		return storyboard.StoryboardPanelVersion{}, storageError("STORYBOARD_READ_FAILED", "The panel version could not be read.", err)
	}
	return panel, nil
}

// MaxPanelVersionNumber reports the highest version number an item's panels
// have, or zero when it has none.
func (r *StoryboardRepository) MaxPanelVersionNumber(ctx context.Context, storyboardItemID string) (int, error) {
	return r.maxNumber(ctx, `SELECT MAX(version_number) FROM storyboard_panel_versions WHERE storyboard_item_id = ?`, storyboardItemID)
}

// ListPanelVersions returns an item's panels in version order.
func (r *StoryboardRepository) ListPanelVersions(ctx context.Context, storyboardItemID string) ([]storyboard.StoryboardPanelVersion, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, panelSelectColumns+
		` WHERE storyboard_item_id = ? ORDER BY version_number ASC`, storyboardItemID)
	if err != nil {
		return nil, storageError("STORYBOARD_READ_FAILED", "The panel versions could not be read.", err)
	}
	defer rows.Close()
	var panels []storyboard.StoryboardPanelVersion
	for rows.Next() {
		panel, scanErr := scanPanelVersion(rows)
		if scanErr != nil {
			return nil, storageError("STORYBOARD_READ_FAILED", "The panel versions could not be read.", scanErr)
		}
		panels = append(panels, panel)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("STORYBOARD_READ_FAILED", "The panel versions could not be read.", err)
	}
	return panels, nil
}

// ApprovePanelImage stores a panel's approved image under a revision guard.
//
// storyboard_panel_versions has no revision column (migration 000010 stores one
// immutable row per panel version), so the guard is taken on the parent
// storyboard item: the item's revision is incremented with the same
// compare-and-swap every other repository uses, and a caller whose expected
// revision no longer matches is refused rather than approving an image against
// a version list that has since changed. The guard and the approval are written
// in one transaction, so an approval cannot advance the item without landing.
//
// The two statements also run through the item's foreign key, which is what
// keeps the pair consistent when the repository is bound to a caller's
// transaction instead of opening its own.
func (r *StoryboardRepository) ApprovePanelImage(ctx context.Context, panelVersionID, approvedImageAssetVersionID string, expectedItemRevision int64) error {
	if r == nil || r.db == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	// A repository already bound to a caller's transaction writes through it:
	// SQLite has one writer, so opening a second transaction would deadlock.
	if r.tx != nil {
		return r.approvePanelImageOn(ctx, r, panelVersionID, approvedImageAssetVersionID, expectedItemRevision)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("STORYBOARD_TX_FAILED", "The approval could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			// A rollback after a successful commit is a no-op, so this is safe
			// on every path.
			_ = tx.Rollback()
		}
	}()
	if err := r.approvePanelImageOn(ctx, r.WithinTx(tx), panelVersionID, approvedImageAssetVersionID, expectedItemRevision); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError("STORYBOARD_TX_FAILED", "The approval could not be saved.", err)
	}
	committed = true
	return nil
}

// approvePanelImageOn performs the guarded approval through one bound
// repository.
func (r *StoryboardRepository) approvePanelImageOn(ctx context.Context, repo *StoryboardRepository, panelVersionID, approvedImageAssetVersionID string, expectedItemRevision int64) error {
	conn := repo.conn()
	if conn == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	var itemID string
	err := conn.QueryRowContext(ctx,
		`SELECT storyboard_item_id FROM storyboard_panel_versions WHERE id = ?`, panelVersionID).Scan(&itemID)
	if err != nil {
		if err == sql.ErrNoRows {
			return storyboard.NotFoundError()
		}
		return storageError("STORYBOARD_READ_FAILED", "The panel version could not be read.", err)
	}
	// The guard is on the item, as documented above.
	result, err := conn.ExecContext(ctx, `UPDATE storyboard_items
		SET revision = revision + 1
		WHERE id = ? AND revision = ?`, itemID, expectedItemRevision)
	if err != nil {
		return storageError("STORYBOARD_WRITE_FAILED", "The storyboard item could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("STORYBOARD_WRITE_FAILED", "The storyboard item could not be updated.", err)
	}
	if affected == 0 {
		return storyboard.ConflictError("This storyboard item changed in another window. Reload it and try again.")
	}
	if _, err := conn.ExecContext(ctx, `UPDATE storyboard_panel_versions
		SET approved_image_asset_version_id = ? WHERE id = ?`,
		approvedImageAssetVersionID, panelVersionID); err != nil {
		return storageError("STORYBOARD_WRITE_FAILED", "The approved image could not be saved.", err)
	}
	return nil
}

// maxNumber runs a MAX(version_number) query, returning zero when no row
// matches so the caller's next number is one.
func (r *StoryboardRepository) maxNumber(ctx context.Context, query string, args ...any) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	var value sql.NullInt64
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, storageError("STORYBOARD_READ_FAILED", "The storyboard versions could not be read.", err)
	}
	if !value.Valid {
		return 0, nil
	}
	return int(value.Int64), nil
}

// scanDirectorPlanVersion reads one director plan version row.
func scanDirectorPlanVersion(row rowScanner) (storyboard.DirectorPlanVersion, error) {
	var version storyboard.DirectorPlanVersion
	var status, createdByType, createdAt string
	if err := row.Scan(&version.ID, &version.EpisodeID, &version.VersionNumber, &status,
		&version.BasedOnVersionID, &version.ScriptVersionID, &version.VisualRhythm, &version.CameraLanguage,
		&version.ColorLighting, &version.Staging, &version.ContinuityRules, &version.AudioDirection,
		&version.ShotOverridesJSON, &version.SourceAgentRunID, &createdByType, &version.CreatedByID,
		&version.ChangeReason, &version.LegacyMetadata, &createdAt); err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	version.Status = versioning.Status(status)
	version.CreatedByType = versioning.CreatedByType(createdByType)
	version.CreatedAt = parseTime(createdAt)
	return version, nil
}

// scanStoryboard reads one storyboard row.
func scanStoryboard(row rowScanner) (storyboard.Storyboard, error) {
	var record storyboard.Storyboard
	var createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.EpisodeID, &record.CurrentVersionID, &createdAt,
		&updatedAt, &record.Revision); err != nil {
		return storyboard.Storyboard{}, err
	}
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// scanStoryboardVersion reads one storyboard version row.
func scanStoryboardVersion(row rowScanner) (storyboard.StoryboardVersion, error) {
	var version storyboard.StoryboardVersion
	var status, createdByType, createdAt string
	if err := row.Scan(&version.ID, &version.StoryboardID, &version.VersionNumber, &status,
		&version.ScriptVersionID, &version.DirectorPlanVersionID, &version.BasedOnVersionID,
		&version.SourceAgentRunID, &createdByType, &version.CreatedByID, &version.ChangeReason,
		&version.LegacyMetadata, &createdAt); err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	version.Status = versioning.Status(status)
	version.CreatedByType = versioning.CreatedByType(createdByType)
	version.CreatedAt = parseTime(createdAt)
	return version, nil
}

// scanStoryboardItem reads one storyboard item row.
func scanStoryboardItem(row rowScanner) (storyboard.StoryboardItem, error) {
	var item storyboard.StoryboardItem
	var status, createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.StoryboardVersionID, &item.ShotID, &item.Ordinal,
		&item.ShotSize, &item.CameraAngle, &item.CameraMovement, &item.DurationSeconds,
		&item.VisualDescription, &item.ActionDescription, &item.DialogueAudioSummary,
		&item.ContinuityNotes, &status, &createdAt, &updatedAt, &item.Revision); err != nil {
		return storyboard.StoryboardItem{}, err
	}
	item.Status = versioning.Status(status)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

// scanPanelVersion reads one panel version row.
func scanPanelVersion(row rowScanner) (storyboard.StoryboardPanelVersion, error) {
	var panel storyboard.StoryboardPanelVersion
	var status, createdByType, createdAt string
	if err := row.Scan(&panel.ID, &panel.StoryboardItemID, &panel.VersionNumber, &status,
		&panel.BasedOnVersionID, &panel.VisualPrompt, &panel.NegativePrompt, &panel.ReferencePolicyJSON,
		&panel.ApprovedImageAssetVersionID, &panel.SourceAgentRunID, &createdByType, &panel.CreatedByID,
		&panel.ChangeReason, &panel.LegacyMetadata, &createdAt); err != nil {
		return storyboard.StoryboardPanelVersion{}, err
	}
	panel.Status = versioning.Status(status)
	panel.CreatedByType = versioning.CreatedByType(createdByType)
	panel.CreatedAt = parseTime(createdAt)
	return panel, nil
}

// Ensure the repository satisfies the application ports.
var (
	_ storyboardapp.DirectorPlanRepository   = (*StoryboardRepository)(nil)
	_ storyboardapp.StoryboardRepository     = (*StoryboardRepository)(nil)
	_ storyboardapp.StoryboardItemRepository = (*StoryboardRepository)(nil)
	_ storyboardapp.PanelRepository          = (*StoryboardRepository)(nil)
)

// CurrentApprovedDirectorPlanVersionID returns the episode's approved director
// plan version, or "" when none is approved.
func (r *StoryboardRepository) CurrentApprovedDirectorPlanVersionID(ctx context.Context, episodeID string) (string, error) {
	return currentApprovedVersionID(ctx, r.db, familyDirectorPlans, episodeID)
}

// ApproveDirectorPlanVersion switches which plan version is approved, recording
// the event in the same transaction.
func (r *StoryboardRepository) ApproveDirectorPlanVersion(ctx context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error {
	return approveVersionWithEvent(ctx, r.db, familyDirectorPlans, versionID, episodeID, expectedStatus, record)
}

// CurrentApprovedStoryboardVersionID returns the storyboard's approved version,
// or "" when none is approved.
func (r *StoryboardRepository) CurrentApprovedStoryboardVersionID(ctx context.Context, storyboardID string) (string, error) {
	return currentApprovedVersionID(ctx, r.db, familyStoryboardVersions, storyboardID)
}

// ApproveStoryboardVersion switches which storyboard version is approved,
// recording the event in the same transaction.
func (r *StoryboardRepository) ApproveStoryboardVersion(ctx context.Context, versionID, storyboardID string, expectedStatus versioning.Status, record event.Event) error {
	return approveVersionWithEvent(ctx, r.db, familyStoryboardVersions, versionID, storyboardID, expectedStatus, record)
}

// ProjectOfEpisode returns the project an episode belongs to.
//
// The storyboard tables carry no project column, so this join is how an approval
// finds the project its event is filed under.
func (r *StoryboardRepository) ProjectOfEpisode(ctx context.Context, episodeID string) (string, error) {
	conn := r.conn()
	if conn == nil {
		return "", storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	var projectID string
	err := conn.QueryRowContext(ctx, `SELECT project_id FROM episodes WHERE id = ?`, episodeID).Scan(&projectID)
	if err == sql.ErrNoRows {
		return "", storyboard.NotFoundError()
	}
	if err != nil {
		return "", storageError("STORYBOARD_READ_FAILED", "The project could not be read.", err)
	}
	return projectID, nil
}

// ProjectOfStoryboard returns the project a storyboard belongs to, resolved
// through its episode.
func (r *StoryboardRepository) ProjectOfStoryboard(ctx context.Context, storyboardID string) (string, error) {
	conn := r.conn()
	if conn == nil {
		return "", storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	var projectID string
	err := conn.QueryRowContext(ctx, `SELECT episodes.project_id FROM storyboards
		JOIN episodes ON episodes.id = storyboards.episode_id
		WHERE storyboards.id = ?`, storyboardID).Scan(&projectID)
	if err == sql.ErrNoRows {
		return "", storyboard.NotFoundError()
	}
	if err != nil {
		return "", storageError("STORYBOARD_READ_FAILED", "The project could not be read.", err)
	}
	return projectID, nil
}
