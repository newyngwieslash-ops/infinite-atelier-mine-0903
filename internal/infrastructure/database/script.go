package database

import (
	"context"
	"database/sql"

	scriptapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// ScriptRepository is the SQLite implementation of the episode and script
// port. It owns every statement touching episodes, the story skeleton and
// adaptation strategy stages, scripts, their versions, scenes and shots.
//
// The three version families carry no revision column in migration 000008, so
// the writes that change their state guard on something else and the comments
// on those methods say what.
type ScriptRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewScriptRepository builds the repository over a database handle.
func NewScriptRepository(db *sql.DB) *ScriptRepository {
	return &ScriptRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
func (r *ScriptRepository) WithinTx(tx *sql.Tx) *ScriptRepository {
	return &ScriptRepository{db: r.db, tx: tx}
}

func (r *ScriptRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

const episodeSelectColumns = `SELECT id, project_id, season_number, episode_number, title, status,
	source_chapter_start_id, source_chapter_end_id, target_duration_seconds,
	current_story_skeleton_version_id, current_adaptation_strategy_version_id, current_script_version_id,
	deleted_at, deleted_by, created_at, updated_at, revision FROM episodes`

// CreateEpisode stores an episode. A duplicate (project, season, episode) triple
// is a conflict, which the schema's unique constraint states as well.
func (r *ScriptRepository) CreateEpisode(ctx context.Context, record script.Episode) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO episodes
		(id, project_id, season_number, episode_number, title, status, source_chapter_start_id,
		 source_chapter_end_id, target_duration_seconds, current_story_skeleton_version_id,
		 current_adaptation_strategy_version_id, current_script_version_id, deleted_at, deleted_by,
		 created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, record.SeasonNumber, record.EpisodeNumber, record.Title,
		string(record.Status), record.SourceChapterStartID, record.SourceChapterEndID,
		record.TargetDurationSeconds, record.CurrentStorySkeletonVersionID,
		record.CurrentAdaptationStrategyVersionID, record.CurrentScriptVersionID,
		formatTime(record.DeletedAt), record.DeletedBy, formatTime(record.CreatedAt),
		formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return script.ConflictError("This project already has an episode at that position.")
		}
		return storageError("SCRIPT_WRITE_FAILED", "The episode could not be saved.", err)
	}
	return nil
}

// GetEpisode returns one episode.
func (r *ScriptRepository) GetEpisode(ctx context.Context, id string) (script.Episode, error) {
	conn := r.conn()
	if conn == nil {
		return script.Episode{}, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, episodeSelectColumns+` WHERE id = ?`, id)
	record, err := scanEpisode(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return script.Episode{}, script.NotFoundError()
		}
		return script.Episode{}, storageError("SCRIPT_READ_FAILED", "The episode could not be read.", err)
	}
	return record, nil
}

// ListEpisodes returns a project's episodes in season and episode order,
// without the rows a soft delete hid.
func (r *ScriptRepository) ListEpisodes(ctx context.Context, projectID string) ([]script.Episode, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, episodeSelectColumns+
		` WHERE project_id = ? AND deleted_at = '' ORDER BY season_number ASC, episode_number ASC`,
		projectID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The episodes could not be read.", err)
	}
	defer rows.Close()
	var records []script.Episode
	for rows.Next() {
		record, scanErr := scanEpisode(rows)
		if scanErr != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The episodes could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The episodes could not be read.", err)
	}
	return records, nil
}

// CountEpisodesAtPosition reports how many episodes already hold a business key.
func (r *ScriptRepository) CountEpisodesAtPosition(ctx context.Context, projectID string, seasonNumber, episodeNumber int) (int, error) {
	return r.count(ctx, `SELECT COUNT(*) FROM episodes
		WHERE project_id = ? AND season_number = ? AND episode_number = ?`,
		projectID, seasonNumber, episodeNumber)
}

// UpdateEpisode persists an episode change under a revision guard.
func (r *ScriptRepository) UpdateEpisode(ctx context.Context, record script.Episode, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE episodes
		SET season_number = ?, episode_number = ?, title = ?, status = ?, source_chapter_start_id = ?,
		    source_chapter_end_id = ?, target_duration_seconds = ?, current_story_skeleton_version_id = ?,
		    current_adaptation_strategy_version_id = ?, current_script_version_id = ?, deleted_at = ?,
		    deleted_by = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		record.SeasonNumber, record.EpisodeNumber, record.Title, string(record.Status),
		record.SourceChapterStartID, record.SourceChapterEndID, record.TargetDurationSeconds,
		record.CurrentStorySkeletonVersionID, record.CurrentAdaptationStrategyVersionID,
		record.CurrentScriptVersionID, formatTime(record.DeletedAt), record.DeletedBy,
		formatTime(record.UpdatedAt), record.ID, expectedRevision)
	if err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The episode could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The episode could not be updated.", err)
	}
	if affected == 0 {
		return script.ConflictError("This item changed in another window. Reload it and try again.")
	}
	return nil
}

const skeletonSelectColumns = `SELECT id, episode_id, version_number, status, based_on_version_id,
	opening_hook, core_conflict, turning_points_json, climax, ending_hook, estimated_duration_seconds,
	source_agent_run_id, created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at
	FROM story_skeleton_versions`

// CreateStorySkeletonVersion stores one version of an episode's skeleton.
func (r *ScriptRepository) CreateStorySkeletonVersion(ctx context.Context, record script.StorySkeletonVersion) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO story_skeleton_versions
		(id, episode_id, version_number, status, based_on_version_id, opening_hook, core_conflict,
		 turning_points_json, climax, ending_hook, estimated_duration_seconds, source_agent_run_id,
		 created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.EpisodeID, record.VersionNumber, string(record.Status),
		record.BasedOnVersionID, record.OpeningHook, record.CoreConflict, record.TurningPointsJSON,
		record.Climax, record.EndingHook, record.EstimatedDurationSeconds, record.SourceAgentRunID,
		string(record.CreatedByType), record.CreatedByID, record.ChangeReason, record.LegacyMetadata,
		formatTime(record.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return script.ConflictError("That version number is already used for this episode.")
		}
		return storageError("SCRIPT_WRITE_FAILED", "The story skeleton version could not be saved.", err)
	}
	return nil
}

// GetStorySkeletonVersion returns one skeleton version.
func (r *ScriptRepository) GetStorySkeletonVersion(ctx context.Context, id string) (script.StorySkeletonVersion, error) {
	conn := r.conn()
	if conn == nil {
		return script.StorySkeletonVersion{}, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, skeletonSelectColumns+` WHERE id = ?`, id)
	record, err := scanSkeleton(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return script.StorySkeletonVersion{}, script.NotFoundError()
		}
		return script.StorySkeletonVersion{}, storageError("SCRIPT_READ_FAILED", "The story skeleton version could not be read.", err)
	}
	return record, nil
}

// MaxStorySkeletonVersionNumber reports the highest version number an episode
// has, or zero when it has none. MAX rather than COUNT, because a number that
// was used must not be handed out again.
func (r *ScriptRepository) MaxStorySkeletonVersionNumber(ctx context.Context, episodeID string) (int, error) {
	return r.maxVersionNumber(ctx, `SELECT MAX(version_number) FROM story_skeleton_versions WHERE episode_id = ?`, episodeID)
}

const strategySelectColumns = `SELECT id, episode_id, version_number, status, based_on_version_id,
	strategy_summary, adaptation_mode, merged_event_groups_json, original_additions, rationale, risks,
	source_agent_run_id, created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at
	FROM adaptation_strategy_versions`

// CreateAdaptationStrategyVersion stores one version of an episode's strategy.
func (r *ScriptRepository) CreateAdaptationStrategyVersion(ctx context.Context, record script.AdaptationStrategyVersion) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO adaptation_strategy_versions
		(id, episode_id, version_number, status, based_on_version_id, strategy_summary, adaptation_mode,
		 merged_event_groups_json, original_additions, rationale, risks, source_agent_run_id,
		 created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.EpisodeID, record.VersionNumber, string(record.Status),
		record.BasedOnVersionID, record.StrategySummary, string(record.AdaptationMode),
		record.MergedEventGroupsJSON, record.OriginalAdditions, record.Rationale, record.Risks,
		record.SourceAgentRunID, string(record.CreatedByType), record.CreatedByID,
		record.ChangeReason, record.LegacyMetadata, formatTime(record.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return script.ConflictError("That version number is already used for this episode.")
		}
		return storageError("SCRIPT_WRITE_FAILED", "The adaptation strategy version could not be saved.", err)
	}
	return nil
}

// GetAdaptationStrategyVersion returns one adaptation strategy version.
func (r *ScriptRepository) GetAdaptationStrategyVersion(ctx context.Context, id string) (script.AdaptationStrategyVersion, error) {
	conn := r.conn()
	if conn == nil {
		return script.AdaptationStrategyVersion{}, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, strategySelectColumns+` WHERE id = ?`, id)
	record, err := scanStrategy(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return script.AdaptationStrategyVersion{}, script.NotFoundError()
		}
		return script.AdaptationStrategyVersion{}, storageError("SCRIPT_READ_FAILED", "The adaptation strategy version could not be read.", err)
	}
	return record, nil
}

// MaxAdaptationStrategyVersionNumber reports the highest version number an
// episode has, or zero when it has none.
func (r *ScriptRepository) MaxAdaptationStrategyVersionNumber(ctx context.Context, episodeID string) (int, error) {
	return r.maxVersionNumber(ctx, `SELECT MAX(version_number) FROM adaptation_strategy_versions WHERE episode_id = ?`, episodeID)
}

const scriptSelectColumns = `SELECT id, episode_id, current_version_id, created_at, updated_at, revision
	FROM scripts`

// CreateScript stores the stable script identity of an episode. The schema
// allows one script per episode, so a second insert is a conflict.
func (r *ScriptRepository) CreateScript(ctx context.Context, record script.Script) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO scripts
		(id, episode_id, current_version_id, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?)`,
		record.ID, record.EpisodeID, record.CurrentVersionID, formatTime(record.CreatedAt),
		formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return script.ConflictError("This episode already has a script.")
		}
		return storageError("SCRIPT_WRITE_FAILED", "The script could not be saved.", err)
	}
	return nil
}

// GetScript returns one script.
func (r *ScriptRepository) GetScript(ctx context.Context, id string) (script.Script, error) {
	conn := r.conn()
	if conn == nil {
		return script.Script{}, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, scriptSelectColumns+` WHERE id = ?`, id)
	record, err := scanScript(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return script.Script{}, script.NotFoundError()
		}
		return script.Script{}, storageError("SCRIPT_READ_FAILED", "The script could not be read.", err)
	}
	return record, nil
}

// GetScriptByEpisode returns the episode's script. A not-found error is the
// ordinary answer for an episode whose script has not been created yet, which
// is what the application layer's EnsureScript reads it as.
func (r *ScriptRepository) GetScriptByEpisode(ctx context.Context, episodeID string) (script.Script, error) {
	conn := r.conn()
	if conn == nil {
		return script.Script{}, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, scriptSelectColumns+` WHERE episode_id = ?`, episodeID)
	record, err := scanScript(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return script.Script{}, script.NotFoundError()
		}
		return script.Script{}, storageError("SCRIPT_READ_FAILED", "The script could not be read.", err)
	}
	return record, nil
}

const scriptVersionSelectColumns = `SELECT id, script_id, version_number, status, based_on_version_id,
	story_skeleton_version_id, adaptation_strategy_version_id, estimated_duration_seconds, summary,
	source_agent_run_id, created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at
	FROM script_versions`

// CreateScriptVersion stores one version of a script's content.
func (r *ScriptRepository) CreateScriptVersion(ctx context.Context, version script.ScriptVersion) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO script_versions
		(id, script_id, version_number, status, based_on_version_id, story_skeleton_version_id,
		 adaptation_strategy_version_id, estimated_duration_seconds, summary, source_agent_run_id,
		 created_by_type, created_by_id, change_reason, legacy_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, version.ScriptID, version.VersionNumber, string(version.Status),
		version.BasedOnVersionID, version.StorySkeletonVersionID, version.AdaptationStrategyVersionID,
		version.EstimatedDurationSeconds, version.Summary, version.SourceAgentRunID,
		string(version.CreatedByType), version.CreatedByID, version.ChangeReason, version.LegacyMetadata,
		formatTime(version.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return script.ConflictError("That version number is already used for this script.")
		}
		return storageError("SCRIPT_WRITE_FAILED", "The script version could not be saved.", err)
	}
	return nil
}

// GetScriptVersion returns one script version.
func (r *ScriptRepository) GetScriptVersion(ctx context.Context, id string) (script.ScriptVersion, error) {
	conn := r.conn()
	if conn == nil {
		return script.ScriptVersion{}, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, scriptVersionSelectColumns+` WHERE id = ?`, id)
	version, err := scanScriptVersion(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return script.ScriptVersion{}, script.NotFoundError()
		}
		return script.ScriptVersion{}, storageError("SCRIPT_READ_FAILED", "The script version could not be read.", err)
	}
	return version, nil
}

// MaxScriptVersionNumber reports the highest version number a script has, or
// zero when it has none.
func (r *ScriptRepository) MaxScriptVersionNumber(ctx context.Context, scriptID string) (int, error) {
	return r.maxVersionNumber(ctx, `SELECT MAX(version_number) FROM script_versions WHERE script_id = ?`, scriptID)
}

// CurrentApprovedScriptVersion returns the script's approved version.
//
// False rather than an error when none is approved: the schema's partial unique
// index means there is at most one, and "not approved yet" is the ordinary
// state of a script whose versions are still drafts.
func (r *ScriptRepository) CurrentApprovedScriptVersion(ctx context.Context, scriptID string) (script.ScriptVersion, bool, error) {
	conn := r.conn()
	if conn == nil {
		return script.ScriptVersion{}, false, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, scriptVersionSelectColumns+
		` WHERE script_id = ? AND status = 'approved'`, scriptID)
	version, err := scanScriptVersion(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return script.ScriptVersion{}, false, nil
		}
		return script.ScriptVersion{}, false, storageError("SCRIPT_READ_FAILED", "The approved script version could not be read.", err)
	}
	return version, true, nil
}

// ApproveScriptVersion makes one version approved and supersedes the previous
// approval, in one transaction.
//
// DOMAIN_MODEL §2.5 allows at most one approved version of a script, which
// migration 000008 enforces with the partial unique index
// idx_script_versions_approved (script_id WHERE status = 'approved'). Because
// the index is partial and immediate, the two writes are order-dependent: the
// previous approval has to leave 'approved' before the target enters it, and
// running only the second would be rejected by the index. One transaction is
// what makes the pair atomic, so a failure between them cannot leave the script
// with no approved version at all.
//
// The guard on the target write is the status the caller read, because
// script_versions has no revision column to compare. A version whose status
// moved under the caller matches no row here and is reported as a conflict.
func (r *ScriptRepository) ApproveScriptVersion(ctx context.Context, target script.ScriptVersion, supersededVersionID string) error {
	if r == nil || r.db == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("SCRIPT_TX_FAILED", "The approval could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			// A rollback after a successful commit is a no-op, so this is safe
			// on every path and is what keeps the two writes atomic.
			_ = tx.Rollback()
		}
	}()

	conn := connection(r.db, tx)
	if supersededVersionID != "" {
		// The previous approval becomes history first. Matching on the status it
		// held is what makes this the same compare-and-swap the target write uses.
		result, err := conn.ExecContext(ctx, `UPDATE script_versions SET status = 'superseded'
			WHERE id = ? AND status = 'approved'`, supersededVersionID)
		if err != nil {
			return storageError("SCRIPT_WRITE_FAILED", "The previous approval could not be superseded.", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return storageError("SCRIPT_WRITE_FAILED", "The previous approval could not be superseded.", err)
		}
		if affected == 0 {
			return script.ConflictError("The version this approval would replace is no longer approved. Reload and try again.")
		}
	}

	result, err := conn.ExecContext(ctx, `UPDATE script_versions SET status = 'approved'
		WHERE id = ? AND status = ?`, target.ID, string(target.Status))
	if err != nil {
		if isUniqueViolation(err) {
			// The index refuses a second approved row, which is what makes this
			// decision safe without holding a lock on the script.
			return script.ConflictError("This script already has an approved version.")
		}
		return storageError("SCRIPT_WRITE_FAILED", "The script version could not be approved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The script version could not be approved.", err)
	}
	if affected == 0 {
		return script.ConflictError("This version changed in another window. Reload it and try again.")
	}

	if err := tx.Commit(); err != nil {
		return storageError("SCRIPT_TX_FAILED", "The approval could not be committed.", err)
	}
	committed = true
	return nil
}

const sceneSelectColumns = `SELECT id, script_version_id, ordinal, scene_number, slugline, interior_exterior,
	location_entity_id, time_of_day, summary, dramatic_goal, estimated_duration_seconds,
	source_story_event_id, is_original_adaptation, created_at, updated_at, revision FROM scenes`

// CreateScene stores a scene. A duplicate (version, ordinal) pair is a conflict.
func (r *ScriptRepository) CreateScene(ctx context.Context, record script.Scene) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO scenes
		(id, script_version_id, ordinal, scene_number, slugline, interior_exterior, location_entity_id,
		 time_of_day, summary, dramatic_goal, estimated_duration_seconds, source_story_event_id,
		 is_original_adaptation, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ScriptVersionID, record.Ordinal, record.SceneNumber, record.Slugline,
		string(record.InteriorExterior), record.LocationEntityID, record.TimeOfDay, record.Summary,
		record.DramaticGoal, record.EstimatedDurationSeconds, record.SourceStoryEventID,
		boolInt(record.IsOriginalAdaptation), formatTime(record.CreatedAt), formatTime(record.UpdatedAt),
		record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return script.ConflictError("That scene position is already used in this script version.")
		}
		return storageError("SCRIPT_WRITE_FAILED", "The scene could not be saved.", err)
	}
	return nil
}

// GetScene returns one scene.
func (r *ScriptRepository) GetScene(ctx context.Context, id string) (script.Scene, error) {
	conn := r.conn()
	if conn == nil {
		return script.Scene{}, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, sceneSelectColumns+` WHERE id = ?`, id)
	record, err := scanScene(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return script.Scene{}, script.NotFoundError()
		}
		return script.Scene{}, storageError("SCRIPT_READ_FAILED", "The scene could not be read.", err)
	}
	return record, nil
}

// ListScenes returns a version's scenes in script order.
func (r *ScriptRepository) ListScenes(ctx context.Context, scriptVersionID string) ([]script.Scene, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, sceneSelectColumns+
		` WHERE script_version_id = ? ORDER BY ordinal ASC`, scriptVersionID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The scenes could not be read.", err)
	}
	defer rows.Close()
	var records []script.Scene
	for rows.Next() {
		record, scanErr := scanScene(rows)
		if scanErr != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The scenes could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The scenes could not be read.", err)
	}
	return records, nil
}

// CountScenesAtOrdinal reports how many scenes already hold a position in a
// version.
func (r *ScriptRepository) CountScenesAtOrdinal(ctx context.Context, scriptVersionID string, ordinal int) (int, error) {
	return r.count(ctx, `SELECT COUNT(*) FROM scenes WHERE script_version_id = ? AND ordinal = ?`,
		scriptVersionID, ordinal)
}

const shotSelectColumns = `SELECT id, scene_id, ordinal, shot_number, shot_size, camera_angle, camera_movement,
	estimated_duration_seconds, visual_description, action_description, audio_intent, continuity_notes,
	status, created_at, updated_at, revision FROM shots`

// CreateShot stores a shot. A duplicate (scene, ordinal) pair is a conflict.
func (r *ScriptRepository) CreateShot(ctx context.Context, record script.Shot) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO shots
		(id, scene_id, ordinal, shot_number, shot_size, camera_angle, camera_movement,
		 estimated_duration_seconds, visual_description, action_description, audio_intent,
		 continuity_notes, status, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.SceneID, record.Ordinal, record.ShotNumber, record.ShotSize,
		record.CameraAngle, record.CameraMovement, record.EstimatedDurationSeconds,
		record.VisualDescription, record.ActionDescription, record.AudioIntent, record.ContinuityNotes,
		string(record.Status), formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return script.ConflictError("That shot position is already used in this scene.")
		}
		return storageError("SCRIPT_WRITE_FAILED", "The shot could not be saved.", err)
	}
	return nil
}

// GetShot returns one shot.
func (r *ScriptRepository) GetShot(ctx context.Context, id string) (script.Shot, error) {
	conn := r.conn()
	if conn == nil {
		return script.Shot{}, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, shotSelectColumns+` WHERE id = ?`, id)
	record, err := scanShot(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return script.Shot{}, script.NotFoundError()
		}
		return script.Shot{}, storageError("SCRIPT_READ_FAILED", "The shot could not be read.", err)
	}
	return record, nil
}

// ListShots returns a scene's shots in order.
func (r *ScriptRepository) ListShots(ctx context.Context, sceneID string) ([]script.Shot, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, shotSelectColumns+
		` WHERE scene_id = ? ORDER BY ordinal ASC`, sceneID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The shots could not be read.", err)
	}
	defer rows.Close()
	var records []script.Shot
	for rows.Next() {
		record, scanErr := scanShot(rows)
		if scanErr != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The shots could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The shots could not be read.", err)
	}
	return records, nil
}

// CountShotsAtOrdinal reports how many shots already hold a position in a scene.
func (r *ScriptRepository) CountShotsAtOrdinal(ctx context.Context, sceneID string, ordinal int) (int, error) {
	return r.count(ctx, `SELECT COUNT(*) FROM shots WHERE scene_id = ? AND ordinal = ?`, sceneID, ordinal)
}

// maxVersionNumber reads one MAX(version_number) query, mapping no row to zero.
func (r *ScriptRepository) maxVersionNumber(ctx context.Context, query string, args ...any) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	var value sql.NullInt64
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, storageError("SCRIPT_READ_FAILED", "The version numbers could not be read.", err)
	}
	if !value.Valid {
		return 0, nil
	}
	return int(value.Int64), nil
}

// count runs one COUNT query. A nil connection fails closed rather than
// reporting zero rows, because zero is a meaningful answer here.
func (r *ScriptRepository) count(ctx context.Context, query string, args ...any) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	var value int
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, storageError("SCRIPT_READ_FAILED", "The rows could not be counted.", err)
	}
	return value, nil
}

// scanEpisode reads one episode row.
func scanEpisode(row rowScanner) (script.Episode, error) {
	var record script.Episode
	var status, deletedAt, deletedBy, createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.ProjectID, &record.SeasonNumber, &record.EpisodeNumber,
		&record.Title, &status, &record.SourceChapterStartID, &record.SourceChapterEndID,
		&record.TargetDurationSeconds, &record.CurrentStorySkeletonVersionID,
		&record.CurrentAdaptationStrategyVersionID, &record.CurrentScriptVersionID, &deletedAt,
		&deletedBy, &createdAt, &updatedAt, &record.Revision); err != nil {
		return script.Episode{}, err
	}
	record.Status = script.EpisodeStatus(status)
	record.DeletedAt = parseTime(deletedAt)
	record.DeletedBy = deletedBy
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// scanSkeleton reads one story skeleton version row.
func scanSkeleton(row rowScanner) (script.StorySkeletonVersion, error) {
	var record script.StorySkeletonVersion
	var status, createdByType, createdAt string
	if err := row.Scan(&record.ID, &record.EpisodeID, &record.VersionNumber, &status,
		&record.BasedOnVersionID, &record.OpeningHook, &record.CoreConflict, &record.TurningPointsJSON,
		&record.Climax, &record.EndingHook, &record.EstimatedDurationSeconds, &record.SourceAgentRunID,
		&createdByType, &record.CreatedByID, &record.ChangeReason, &record.LegacyMetadata,
		&createdAt); err != nil {
		return script.StorySkeletonVersion{}, err
	}
	record.Status = versioning.Status(status)
	record.CreatedByType = versioning.CreatedByType(createdByType)
	record.CreatedAt = parseTime(createdAt)
	return record, nil
}

// scanStrategy reads one adaptation strategy version row.
func scanStrategy(row rowScanner) (script.AdaptationStrategyVersion, error) {
	var record script.AdaptationStrategyVersion
	var status, mode, createdByType, createdAt string
	if err := row.Scan(&record.ID, &record.EpisodeID, &record.VersionNumber, &status,
		&record.BasedOnVersionID, &record.StrategySummary, &mode, &record.MergedEventGroupsJSON,
		&record.OriginalAdditions, &record.Rationale, &record.Risks, &record.SourceAgentRunID,
		&createdByType, &record.CreatedByID, &record.ChangeReason, &record.LegacyMetadata,
		&createdAt); err != nil {
		return script.AdaptationStrategyVersion{}, err
	}
	record.Status = versioning.Status(status)
	record.AdaptationMode = script.AdaptationMode(mode)
	record.CreatedByType = versioning.CreatedByType(createdByType)
	record.CreatedAt = parseTime(createdAt)
	return record, nil
}

// scanScript reads one script row.
func scanScript(row rowScanner) (script.Script, error) {
	var record script.Script
	var createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.EpisodeID, &record.CurrentVersionID, &createdAt,
		&updatedAt, &record.Revision); err != nil {
		return script.Script{}, err
	}
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// scanScriptVersion reads one script version row.
func scanScriptVersion(row rowScanner) (script.ScriptVersion, error) {
	var version script.ScriptVersion
	var status, createdByType, createdAt string
	if err := row.Scan(&version.ID, &version.ScriptID, &version.VersionNumber, &status,
		&version.BasedOnVersionID, &version.StorySkeletonVersionID,
		&version.AdaptationStrategyVersionID, &version.EstimatedDurationSeconds, &version.Summary,
		&version.SourceAgentRunID, &createdByType, &version.CreatedByID, &version.ChangeReason,
		&version.LegacyMetadata, &createdAt); err != nil {
		return script.ScriptVersion{}, err
	}
	version.Status = versioning.Status(status)
	version.CreatedByType = versioning.CreatedByType(createdByType)
	version.CreatedAt = parseTime(createdAt)
	return version, nil
}

// scanScene reads one scene row.
func scanScene(row rowScanner) (script.Scene, error) {
	var record script.Scene
	var interior string
	var isOriginal int
	var createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.ScriptVersionID, &record.Ordinal, &record.SceneNumber,
		&record.Slugline, &interior, &record.LocationEntityID, &record.TimeOfDay, &record.Summary,
		&record.DramaticGoal, &record.EstimatedDurationSeconds, &record.SourceStoryEventID,
		&isOriginal, &createdAt, &updatedAt, &record.Revision); err != nil {
		return script.Scene{}, err
	}
	record.InteriorExterior = script.InteriorExterior(interior)
	record.IsOriginalAdaptation = isOriginal == 1
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// scanShot reads one shot row.
func scanShot(row rowScanner) (script.Shot, error) {
	var record script.Shot
	var status, createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.SceneID, &record.Ordinal, &record.ShotNumber, &record.ShotSize,
		&record.CameraAngle, &record.CameraMovement, &record.EstimatedDurationSeconds,
		&record.VisualDescription, &record.ActionDescription, &record.AudioIntent,
		&record.ContinuityNotes, &status, &createdAt, &updatedAt, &record.Revision); err != nil {
		return script.Shot{}, err
	}
	record.Status = versioning.Status(status)
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// Ensure the repository satisfies the application port.
var _ scriptapp.Repository = (*ScriptRepository)(nil)

// CurrentApprovedSkeletonVersionID returns the episode's approved skeleton
// version, or "" when none is approved.
func (r *ScriptRepository) CurrentApprovedSkeletonVersionID(ctx context.Context, episodeID string) (string, error) {
	return currentApprovedVersionID(ctx, r.db, familyStorySkeletonVersions, episodeID)
}

// ApproveStorySkeletonVersion switches which skeleton version is approved,
// recording the event in the same transaction.
func (r *ScriptRepository) ApproveStorySkeletonVersion(ctx context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error {
	return approveVersionWithEvent(ctx, r.db, familyStorySkeletonVersions, versionID, episodeID, expectedStatus, record)
}

// CurrentApprovedStrategyVersionID returns the episode's approved adaptation
// strategy version, or "" when none is approved.
func (r *ScriptRepository) CurrentApprovedStrategyVersionID(ctx context.Context, episodeID string) (string, error) {
	return currentApprovedVersionID(ctx, r.db, familyAdaptationStrategies, episodeID)
}

// ApproveAdaptationStrategyVersion switches which strategy version is approved,
// recording the event in the same transaction.
func (r *ScriptRepository) ApproveAdaptationStrategyVersion(ctx context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error {
	return approveVersionWithEvent(ctx, r.db, familyAdaptationStrategies, versionID, episodeID, expectedStatus, record)
}
