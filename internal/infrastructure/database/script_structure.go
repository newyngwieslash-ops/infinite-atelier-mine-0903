package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// script_structure.go is the repository's WP-08 surface: a whole version's content in one
// transaction, the dialogue lines migration 000008 created with no writer, the two event-link
// tables, the field locks and the version histories.
//
// It is a separate file because it is a separate unit of work. `script.go` writes one row at a
// time — a scene, a shot — which is right for a user editing one thing and wrong for a stage
// producing a whole version, and the two have different failure semantics: a single-row write
// that fails leaves nothing behind, while a structure write that fails must leave NOTHING of a
// hundred rows.

// withinTx runs fn inside a transaction of this repository's own database.
//
// A repository already bound to a caller's transaction runs fn against that transaction instead
// of opening a second one, which SQLite would refuse (one connection, one writer) and which would
// break the caller's atomicity. It mirrors the workflow repository's helper rather than sharing
// one, because the two are over different types and a generic version would need a type
// parameter for the receiver.
func (r *ScriptRepository) withinTx(ctx context.Context, fn func(repo *ScriptRepository) error) error {
	if r == nil || r.db == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	if r.tx != nil {
		return fn(r)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("SCRIPT_TX_FAILED", "The script change could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			// A rollback after a successful commit is a no-op, so this is safe on every path.
			_ = tx.Rollback()
		}
	}()
	if err := fn(r.WithinTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError("SCRIPT_TX_FAILED", "The script change could not be saved.", err)
	}
	committed = true
	return nil
}

// CreateScriptStructure writes a version's scenes, dialogue lines and shots in ONE transaction.
//
// The whole point of this method is the transaction: PRD FR-040's S3 produces one version, and a
// version that existed with half its scenes would be an artifact nobody could judge — including
// its own duration, which is the sum over those scenes. So the caller's Validate has already
// checked the whole payload, and this either writes all of it or none.
//
// It does NOT check for an existing version, because a version is written once: the caller
// created the script_versions row in its own transaction and passes its id, and the schema's
// foreign key refuses a structure for a version that does not exist.
func (r *ScriptRepository) CreateScriptStructure(ctx context.Context, structure script.ScriptStructure) error {
	if r == nil || r.db == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	return r.withinTx(ctx, func(repo *ScriptRepository) error {
		for _, scene := range structure.Scenes {
			if err := repo.CreateScene(ctx, scene.Scene); err != nil {
				return err
			}
			for _, line := range scene.DialogueLines {
				if err := repo.CreateDialogueLine(ctx, line); err != nil {
					return err
				}
			}
			for _, shot := range scene.Shots {
				if err := repo.CreateShot(ctx, shot); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// GetScriptStructure reads a version's whole content.
//
// Three queries rather than one join, and the reason is that a join over scenes × lines × shots
// multiplies rows: a scene with five lines and three shots produces fifteen rows, and
// reassembling them needs the same grouping this does without the multiplication. Each query is
// ordered by its own ordinal, so the result is the payload the version was written from.
func (r *ScriptRepository) GetScriptStructure(ctx context.Context, scriptVersionID string) (script.ScriptStructure, error) {
	scenes, err := r.ListScenes(ctx, scriptVersionID)
	if err != nil {
		return script.ScriptStructure{}, err
	}
	structure := script.ScriptStructure{
		ScriptVersionID: scriptVersionID,
		Scenes:          make([]script.SceneStructure, 0, len(scenes)),
	}
	for _, scene := range scenes {
		entry := script.SceneStructure{Scene: scene}
		if entry.DialogueLines, err = r.ListDialogueLines(ctx, scene.ID); err != nil {
			return script.ScriptStructure{}, err
		}
		if entry.Shots, err = r.ListShots(ctx, scene.ID); err != nil {
			return script.ScriptStructure{}, err
		}
		structure.Scenes = append(structure.Scenes, entry)
	}
	return structure, nil
}

const dialogueSelectColumns = `SELECT id, scene_id, ordinal, line_type, character_entity_id, text,
	emotion, performance_note, source_story_event_id, locked, created_at, updated_at, revision
	FROM dialogue_lines`

// CreateDialogueLine stores one line. A duplicate (scene, ordinal) pair is a conflict.
//
// It is the writer migration 000008's `dialogue_lines` table never had: WP-05 created the table,
// the domain type and its validation, and WP-06 recorded the gap in STATUS ("dialogue lines have
// no application path"). A script without its lines is not a script — FR-040's S3 output lists
// 对白 and 旁白 as first-class — so this is what makes the third stage's artifact real.
func (r *ScriptRepository) CreateDialogueLine(ctx context.Context, record script.DialogueLine) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO dialogue_lines
		(id, scene_id, ordinal, line_type, character_entity_id, text, emotion, performance_note,
		 source_story_event_id, locked, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.SceneID, record.Ordinal, string(record.Type), record.CharacterEntityID,
		record.Text, record.Emotion, record.PerformanceNote, record.SourceStoryEventID,
		boolInt(record.Locked), formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return script.ConflictError("That line position is already used in this scene.")
		}
		return storageError("SCRIPT_WRITE_FAILED", "The dialogue line could not be saved.", err)
	}
	return nil
}

// GetDialogueLine returns one line by id.
func (r *ScriptRepository) GetDialogueLine(ctx context.Context, id string) (script.DialogueLine, error) {
	conn := r.conn()
	if conn == nil {
		return script.DialogueLine{}, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	record, err := scanDialogueLine(conn.QueryRowContext(ctx, dialogueSelectColumns+` WHERE id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return script.DialogueLine{}, script.NotFoundError()
		}
		return script.DialogueLine{}, storageError("SCRIPT_READ_FAILED", "The dialogue line could not be read.", err)
	}
	return record, nil
}

// ListDialogueLines returns a scene's lines in order.
func (r *ScriptRepository) ListDialogueLines(ctx context.Context, sceneID string) ([]script.DialogueLine, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, dialogueSelectColumns+` WHERE scene_id = ? ORDER BY ordinal ASC`, sceneID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The dialogue lines could not be read.", err)
	}
	defer rows.Close()
	records := make([]script.DialogueLine, 0, 8)
	for rows.Next() {
		record, scanErr := scanDialogueLine(rows)
		if scanErr != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The dialogue lines could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The dialogue lines could not be read.", err)
	}
	return records, nil
}

// SetDialogueLineLocked records whether a line is protected from regeneration.
//
// It is revision-guarded, and it is the only UPDATE this file performs. A lock is the only thing
// a user changes about an existing line in this build: a line's TEXT is rewritten wholesale by a
// new version, which is what makes a version immutable, so there is no general update for a
// caller to reach for and no way to rewrite content through this method.
func (r *ScriptRepository) SetDialogueLineLocked(ctx context.Context, lineID string, locked bool, expectedRevision int64, updatedAt time.Time) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE dialogue_lines
		SET locked = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		boolInt(locked), formatTime(updatedAt), lineID, expectedRevision)
	if err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The line lock could not be saved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The line lock could not be saved.", err)
	}
	if affected == 0 {
		// Zero rows means the row moved under us or is not there. The two are told apart because
		// the caller acts differently: a conflict is retried after a reload, a missing row is a
		// stale identifier.
		if _, readErr := r.GetDialogueLine(ctx, lineID); readErr != nil {
			return readErr
		}
		return script.ConflictError("That dialogue line was changed by someone else.")
	}
	return nil
}

// LockScriptField records a lock on one field of one version.
//
// The insert is an UPSERT rather than a plain INSERT: the user's intent is "this field stays", and
// repeating that is not a conflict. Locking is idempotent for the same reason unlocking is —
// neither carries a value, so there is nothing for a second call to contradict.
func (r *ScriptRepository) LockScriptField(ctx context.Context, record script.FieldLock) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO script_version_field_locks
		(version_id, family, field, locked_by, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(version_id, field) DO UPDATE SET
			family = excluded.family,
			locked_by = excluded.locked_by`,
		record.VersionID, string(record.Family), string(record.Field), record.LockedBy, record.CreatedAt)
	if err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The field lock could not be saved.", err)
	}
	return nil
}

// UnlockScriptField removes a lock.
func (r *ScriptRepository) UnlockScriptField(ctx context.Context, versionID string, field script.LockableField) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `DELETE FROM script_version_field_locks
		WHERE version_id = ? AND field = ?`, versionID, string(field))
	if err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The field lock could not be removed.", err)
	}
	// Zero rows affected is NOT a refusal: unlocking an unlocked field is the state the caller
	// asked for, and reporting a not-found would make a double click an error.
	return nil
}

// ListScriptFieldLocks returns a version's locks.
func (r *ScriptRepository) ListScriptFieldLocks(ctx context.Context, versionID string) ([]script.FieldLock, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT version_id, family, field, locked_by, created_at
		FROM script_version_field_locks WHERE version_id = ? ORDER BY field ASC`, versionID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The field locks could not be read.", err)
	}
	defer rows.Close()
	locks := make([]script.FieldLock, 0, 4)
	for rows.Next() {
		var lock script.FieldLock
		var family, field string
		if err := rows.Scan(&lock.VersionID, &family, &field, &lock.LockedBy, &lock.CreatedAt); err != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The field locks could not be read.", err)
		}
		lock.Family = script.VersionFamily(family)
		lock.Field = script.LockableField(field)
		locks = append(locks, lock)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The field locks could not be read.", err)
	}
	return locks, nil
}

// LinkSkeletonEvents records which story events one skeleton version selected.
//
// It runs in one transaction with its deletes because the link set is replaced as a whole: a
// caller stating the events it selected is stating the complete answer, and a partial update
// would leave rows from a previous statement behind. §7.4 makes this a LINK TABLE rather than a
// JSON column ("Lists must be link tables or controlled structures"), which is why the write is
// rows and not a field.
func (r *ScriptRepository) LinkSkeletonEvents(ctx context.Context, versionID string, eventIDs []string, createdAt time.Time) error {
	if r == nil || r.db == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	return r.withinTx(ctx, func(repo *ScriptRepository) error {
		conn := repo.conn()
		if conn == nil {
			return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
		}
		if _, err := conn.ExecContext(ctx,
			`DELETE FROM story_skeleton_event_links WHERE skeleton_version_id = ?`, versionID); err != nil {
			return storageError("SCRIPT_WRITE_FAILED", "The selected events could not be replaced.", err)
		}
		for index, eventID := range eventIDs {
			if _, err := conn.ExecContext(ctx, `INSERT INTO story_skeleton_event_links
				(skeleton_version_id, story_event_id, ordinal, created_at)
				VALUES (?, ?, ?, ?)`,
				versionID, eventID, index+1, formatTime(createdAt)); err != nil {
				if isForeignKeyViolation(err) {
					return script.InvalidError("That story event does not exist.")
				}
				return storageError("SCRIPT_WRITE_FAILED", "The selected events could not be saved.", err)
			}
		}
		return nil
	})
}

// ListSkeletonEventIDs returns the events one skeleton version selected.
func (r *ScriptRepository) ListSkeletonEventIDs(ctx context.Context, versionID string) ([]string, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT story_event_id FROM story_skeleton_event_links
		WHERE skeleton_version_id = ? ORDER BY ordinal ASC`, versionID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The selected events could not be read.", err)
	}
	defer rows.Close()
	ids := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The selected events could not be read.", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The selected events could not be read.", err)
	}
	return ids, nil
}

// LinkStrategyEvents records one strategy version's per-event treatments.
func (r *ScriptRepository) LinkStrategyEvents(ctx context.Context, versionID string, links []script.StrategyEventLink, createdAt time.Time) error {
	if r == nil || r.db == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	return r.withinTx(ctx, func(repo *ScriptRepository) error {
		conn := repo.conn()
		if conn == nil {
			return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
		}
		if _, err := conn.ExecContext(ctx,
			`DELETE FROM adaptation_strategy_event_links WHERE strategy_version_id = ?`, versionID); err != nil {
			return storageError("SCRIPT_WRITE_FAILED", "The event treatments could not be replaced.", err)
		}
		for index, link := range links {
			// The ordinal is the array's position rather than the caller's value, because the
			// array IS the order: a payload that stated both could state two different ones, and
			// the order the caller wrote is the one a reader would see.
			if _, err := conn.ExecContext(ctx, `INSERT INTO adaptation_strategy_event_links
				(strategy_version_id, story_event_id, treatment, ordinal, created_at)
				VALUES (?, ?, ?, ?, ?)`,
				link.StrategyVersionID, link.StoryEventID, string(link.Treatment), index+1,
				formatTime(createdAt)); err != nil {
				if isForeignKeyViolation(err) {
					return script.InvalidError("That story event does not exist.")
				}
				return storageError("SCRIPT_WRITE_FAILED", "The event treatments could not be saved.", err)
			}
		}
		return nil
	})
}

// ListStrategyEventLinks returns one strategy version's treatments.
func (r *ScriptRepository) ListStrategyEventLinks(ctx context.Context, versionID string) ([]script.StrategyEventLink, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT strategy_version_id, story_event_id, treatment, ordinal
		FROM adaptation_strategy_event_links WHERE strategy_version_id = ? ORDER BY ordinal ASC`, versionID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The event treatments could not be read.", err)
	}
	defer rows.Close()
	links := make([]script.StrategyEventLink, 0, 8)
	for rows.Next() {
		var link script.StrategyEventLink
		var treatment string
		if err := rows.Scan(&link.StrategyVersionID, &link.StoryEventID, &treatment, &link.Ordinal); err != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The event treatments could not be read.", err)
		}
		link.Treatment = script.EventTreatment(treatment)
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The event treatments could not be read.", err)
	}
	return links, nil
}

// ListStorySkeletonVersions returns an episode's skeleton versions newest first.
//
// The ORDER is the maximum version number rather than created_at, because the number is what the
// schema makes unique per episode: two versions created in the same clock tick would otherwise
// come back in whichever order SQLite chose.
func (r *ScriptRepository) ListStorySkeletonVersions(ctx context.Context, episodeID string) ([]script.StorySkeletonVersion, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, skeletonSelectColumns+` WHERE episode_id = ?
		ORDER BY version_number DESC`, episodeID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The skeleton versions could not be read.", err)
	}
	defer rows.Close()
	records := make([]script.StorySkeletonVersion, 0, 4)
	for rows.Next() {
		record, scanErr := scanSkeleton(rows)
		if scanErr != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The skeleton versions could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The skeleton versions could not be read.", err)
	}
	return records, nil
}

// ListAdaptationStrategyVersions returns an episode's strategy versions newest first.
func (r *ScriptRepository) ListAdaptationStrategyVersions(ctx context.Context, episodeID string) ([]script.AdaptationStrategyVersion, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, strategySelectColumns+` WHERE episode_id = ?
		ORDER BY version_number DESC`, episodeID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The strategy versions could not be read.", err)
	}
	defer rows.Close()
	records := make([]script.AdaptationStrategyVersion, 0, 4)
	for rows.Next() {
		record, scanErr := scanStrategy(rows)
		if scanErr != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The strategy versions could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The strategy versions could not be read.", err)
	}
	return records, nil
}

// ListScriptVersions returns a script's versions newest first.
func (r *ScriptRepository) ListScriptVersions(ctx context.Context, scriptID string) ([]script.ScriptVersion, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, scriptVersionSelectColumns+` WHERE script_id = ?
		ORDER BY version_number DESC`, scriptID)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The script versions could not be read.", err)
	}
	defer rows.Close()
	records := make([]script.ScriptVersion, 0, 4)
	for rows.Next() {
		record, scanErr := scanScriptVersion(rows)
		if scanErr != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The script versions could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The script versions could not be read.", err)
	}
	return records, nil
}

// SetScriptVersionTotals writes the summed duration and the summary onto a version row.
//
// It is the second half of the structure write and deliberately not part of its transaction: the
// content is committed by then, and a failure here leaves a version with content and a stale
// duration rather than no version at all. A stale duration is repairable — the sum is derivable
// from rows that exist — where a missing version is not.
func (r *ScriptRepository) SetScriptVersionTotals(ctx context.Context, versionID string, totalDurationSeconds int, summary string) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE script_versions
		SET estimated_duration_seconds = ?, summary = ? WHERE id = ?`,
		totalDurationSeconds, summary, versionID)
	if err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The version totals could not be saved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The version totals could not be saved.", err)
	}
	if affected == 0 {
		// Zero rows is a missing version rather than a conflict: this UPDATE is not
		// revision-guarded, because a version's totals are derived from its own content and two
		// writers computing the same sum produce the same value.
		return script.NotFoundError()
	}
	return nil
}
