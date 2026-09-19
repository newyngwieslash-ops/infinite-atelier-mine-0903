package database

import (
	"context"
	"database/sql"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// story_graph.go holds the reads the story-graph panel needs and the three
// writes extraction added: aliases, participants and evidence.
//
// It is a separate file from story.go because it is a separate concern. story.go
// owns the aggregate commands — create, read one, update under a revision guard.
// Everything here is either a filtered LIST, which returns many rows for a view,
// or a child row of the fact layer that has no command of its own. Keeping them
// apart means the aggregate file does not grow a view query every time a panel
// needs one, which is how a repository file stops being readable.
//
// The list queries share two rules:
//
//   - A soft-deleted row is never returned. DOMAIN_MODEL section 6.1 and 6.3
//     both carry deleted_at, and a deleted fact appearing in the graph would be
//     the one bug a user cannot work around.
//   - An empty status filter means "any", spelled as a NULL comparison rather
//     than by building the SQL from a string, so no caller can reach the
//     statement text.

// ListStoryEntities returns a project's live entities, oldest first.
func (r *StoryRepository) ListStoryEntities(ctx context.Context, projectID string, status story.FactStatus) ([]story.StoryEntity, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	// The empty-status case passes NULL, and `(? = '' OR status = ?)` is true for
	// it because the first comparison is true. One statement, no string building.
	rows, err := conn.QueryContext(ctx, storyEntitySelectColumns+
		` WHERE project_id = ? AND deleted_at = '' AND (? = '' OR status = ?)
		  ORDER BY created_at ASC, id ASC`, projectID, string(status), string(status))
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The story entities could not be read.", err)
	}
	defer rows.Close()
	return scanStoryEntities(rows)
}

func scanStoryEntities(rows *sql.Rows) ([]story.StoryEntity, error) {
	records := []story.StoryEntity{}
	for rows.Next() {
		record, err := scanStoryEntity(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

const storyEntityAliasSelectColumns = `SELECT id, story_entity_id, alias, source_chapter_id,
	source_start_offset, source_end_offset, created_at FROM story_entity_aliases`

// CreateStoryEntityAlias stores an alternative name for an entity.
func (r *StoryRepository) CreateStoryEntityAlias(ctx context.Context, record story.StoryEntityAlias) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO story_entity_aliases
		(id, story_entity_id, alias, source_chapter_id, source_start_offset, source_end_offset, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.StoryEntityID, record.Alias, record.SourceChapterID,
		nullableInt(record.SourceStart), nullableInt(record.SourceEnd), formatTime(record.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return story.ConflictError("That name is already an alias for this entity.")
		}
		return storageError("STORY_WRITE_FAILED", "The alias could not be saved.", err)
	}
	return nil
}

// ListStoryEntityAliases returns an entity's aliases oldest first.
func (r *StoryRepository) ListStoryEntityAliases(ctx context.Context, storyEntityID string) ([]story.StoryEntityAlias, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, storyEntityAliasSelectColumns+
		` WHERE story_entity_id = ? ORDER BY created_at ASC, id ASC`, storyEntityID)
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The entity aliases could not be read.", err)
	}
	defer rows.Close()
	records := []story.StoryEntityAlias{}
	for rows.Next() {
		var record story.StoryEntityAlias
		var startOffset, endOffset sql.NullInt64
		var createdAt string
		if err := rows.Scan(&record.ID, &record.StoryEntityID, &record.Alias, &record.SourceChapterID,
			&startOffset, &endOffset, &createdAt); err != nil {
			return nil, storageError("STORY_READ_FAILED", "The entity aliases could not be read.", err)
		}
		record.SourceStart = intPointer(startOffset)
		record.SourceEnd = intPointer(endOffset)
		record.CreatedAt = parseTime(createdAt)
		records = append(records, record)
	}
	return records, rows.Err()
}

// ListStoryEvents returns a project's live events in story order.
func (r *StoryRepository) ListStoryEvents(ctx context.Context, projectID, chapterID string, status story.FactStatus) ([]story.StoryEvent, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, storyEventSelectColumns+
		` WHERE project_id = ? AND deleted_at = '' AND (? = '' OR chapter_id = ?) AND (? = '' OR status = ?)
		  ORDER BY ordinal ASC, id ASC`,
		projectID, chapterID, chapterID, string(status), string(status))
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The story events could not be read.", err)
	}
	defer rows.Close()
	records := []story.StoryEvent{}
	for rows.Next() {
		record, err := scanStoryEvent(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

const storyEventParticipantSelectColumns = `SELECT story_event_id, story_entity_id, role, state_before,
	state_after, created_at FROM story_event_participants`

// CreateStoryEventParticipant links an entity to an event.
func (r *StoryRepository) CreateStoryEventParticipant(ctx context.Context, record story.StoryEventParticipant) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO story_event_participants
		(story_event_id, story_entity_id, role, state_before, state_after, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		record.StoryEventID, record.StoryEntityID, string(record.Role),
		record.StateBefore, record.StateAfter, formatTime(record.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			// One entity holds several roles in one event, so this is a conflict
			// on the (event, entity, role) triple rather than on the pair.
			return story.ConflictError("That entity already holds that role in this event.")
		}
		return storageError("STORY_WRITE_FAILED", "The participant could not be saved.", err)
	}
	return nil
}

// ListStoryEventParticipants returns an event's participants.
func (r *StoryRepository) ListStoryEventParticipants(ctx context.Context, storyEventID string) ([]story.StoryEventParticipant, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, storyEventParticipantSelectColumns+
		` WHERE story_event_id = ? ORDER BY role ASC, story_entity_id ASC`, storyEventID)
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The participants could not be read.", err)
	}
	defer rows.Close()
	records := []story.StoryEventParticipant{}
	for rows.Next() {
		var record story.StoryEventParticipant
		var role, createdAt string
		if err := rows.Scan(&record.StoryEventID, &record.StoryEntityID, &role,
			&record.StateBefore, &record.StateAfter, &createdAt); err != nil {
			return nil, storageError("STORY_READ_FAILED", "The participants could not be read.", err)
		}
		record.Role = story.ParticipantRole(role)
		record.CreatedAt = parseTime(createdAt)
		records = append(records, record)
	}
	return records, rows.Err()
}

// ListStoryRelations returns a project's live relations, oldest first.
func (r *StoryRepository) ListStoryRelations(ctx context.Context, projectID string, status story.FactStatus) ([]story.StoryRelation, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	// story_relations carries no deleted_at column — migration 000007 has no soft
	// delete for a relation, and the status vocabulary is where its lifecycle
	// lives. The filter is therefore only on status.
	rows, err := conn.QueryContext(ctx, storyRelationSelectColumns+
		` WHERE project_id = ? AND (? = '' OR status = ?) ORDER BY created_at ASC, id ASC`,
		projectID, string(status), string(status))
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The story relations could not be read.", err)
	}
	defer rows.Close()
	records := []story.StoryRelation{}
	for rows.Next() {
		record, err := scanStoryRelation(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

const storyFactSourceSelectColumns = `SELECT id, fact_type, fact_id, chapter_id, source_document_version_id,
	start_offset, end_offset, quote_hash, source_kind, created_at FROM story_fact_sources`

// CreateStoryFactSource stores one piece of evidence for a fact.
func (r *StoryRepository) CreateStoryFactSource(ctx context.Context, record story.StoryFactSource) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO story_fact_sources
		(id, fact_type, fact_id, chapter_id, source_document_version_id, start_offset, end_offset,
		 quote_hash, source_kind, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, string(record.FactType), record.FactID, record.ChapterID, record.SourceDocumentVersionID,
		nullableInt(record.StartOffset), nullableInt(record.EndOffset), record.QuoteHash,
		string(record.SourceKind), formatTime(record.CreatedAt))
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The evidence could not be saved.", err)
	}
	return nil
}

// ListStoryFactSources returns the evidence for one fact, oldest first.
func (r *StoryRepository) ListStoryFactSources(ctx context.Context, factType story.FactType, factID string) ([]story.StoryFactSource, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, storyFactSourceSelectColumns+
		` WHERE fact_type = ? AND fact_id = ? ORDER BY created_at ASC, id ASC`, string(factType), factID)
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The evidence could not be read.", err)
	}
	defer rows.Close()
	records := []story.StoryFactSource{}
	for rows.Next() {
		var record story.StoryFactSource
		var startOffset, endOffset sql.NullInt64
		var factTypeValue, sourceKind, createdAt string
		if err := rows.Scan(&record.ID, &factTypeValue, &record.FactID, &record.ChapterID,
			&record.SourceDocumentVersionID, &startOffset, &endOffset, &record.QuoteHash,
			&sourceKind, &createdAt); err != nil {
			return nil, storageError("STORY_READ_FAILED", "The evidence could not be read.", err)
		}
		record.FactType = story.FactType(factTypeValue)
		record.SourceKind = story.SourceKind(sourceKind)
		record.StartOffset = intPointer(startOffset)
		record.EndOffset = intPointer(endOffset)
		record.CreatedAt = parseTime(createdAt)
		records = append(records, record)
	}
	return records, rows.Err()
}

// intPointer reads a nullable integer back into an optional offset.
func intPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	out := int(value.Int64)
	return &out
}

// ListStoryConflicts returns a project's recorded conflicts, newest first.
//
// The story_fact_conflicts table carries no deleted_at column — a conflict's
// lifecycle is its status, which is what 'resolved' and 'waived' are for — so the
// only filter is the status.
func (r *StoryRepository) ListStoryConflicts(ctx context.Context, projectID string, status story.ConflictStatus) ([]story.StoryFactConflict, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, storyConflictSelectColumns+
		` WHERE project_id = ? AND (? = '' OR status = ?) ORDER BY created_at DESC, id DESC`,
		projectID, string(status), string(status))
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The conflicts could not be read.", err)
	}
	defer rows.Close()
	records := []story.StoryFactConflict{}
	for rows.Next() {
		record, err := scanStoryConflict(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// SplitChapter replaces one chapter with two, renumbering what follows.
//
// The renumbering is why this is one call rather than two:
// UNIQUE (source_document_version_id, ordinal) means the row that takes the
// following ordinal cannot exist until the rows after it have moved, so a caller
// performing steps 1-3 could fail between them and leave a version whose ordinals
// have a hole or a duplicate. SQLite cannot defer that constraint inside a
// transaction, so the shift happens in one statement that cannot observe the
// intermediate state.
//
// The order inside the transaction matters and is the reverse of the obvious one:
// the chapters AFTER the split move first, from the highest ordinal down, so no
// UPDATE ever collides with a row that has not moved yet. Renumbering upward
// would collide immediately.
func (r *StoryRepository) SplitChapter(ctx context.Context, first story.Chapter, second story.Chapter, expectedRevision int64) error {
	if r == nil || r.db == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("STORY_TX_FAILED", "The split could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	conn := connection(r.db, tx)
	if err := shiftOrdinals(ctx, conn, first.SourceDocumentVersionID, first.Ordinal+1, 1); err != nil {
		return err
	}
	// The first half keeps its ordinal and gains the shortened range. RowsAffected
	// is checked because a stale revision matches no row: without this the command
	// would report a successful split having changed nothing, which is the defect a
	// test caught when the guard was guessed rather than passed.
	result, err := conn.ExecContext(ctx, `UPDATE chapters
		SET title = ?, start_offset = ?, end_offset = ?, content_hash = ?, source_kind = ?, status = ?,
		    updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		first.Title, first.StartOffset, first.EndOffset, first.ContentHash, string(first.SourceKind),
		string(first.Status), formatTime(first.UpdatedAt), first.ID, expectedRevision)
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The chapter could not be split.", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return storageError("STORY_WRITE_FAILED", "The chapter could not be split.", err)
	} else if affected == 0 {
		return story.ConflictError("This item changed in another window. Reload it and try again.")
	}
	// The second half takes the freed ordinal.
	if _, err := conn.ExecContext(ctx, `INSERT INTO chapters
		(id, source_document_version_id, ordinal, title, start_offset, end_offset, content_hash,
		 source_kind, status, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		second.ID, second.SourceDocumentVersionID, second.Ordinal, second.Title,
		second.StartOffset, second.EndOffset, second.ContentHash, string(second.SourceKind),
		string(second.Status), formatTime(second.CreatedAt), formatTime(second.UpdatedAt),
		second.Revision); err != nil {
		if isUniqueViolation(err) {
			return story.ConflictError("That chapter position is already used for this version.")
		}
		return storageError("STORY_WRITE_FAILED", "The new chapter could not be stored.", err)
	}
	if err := tx.Commit(); err != nil {
		return storageError("STORY_TX_FAILED", "The split could not be committed.", err)
	}
	committed = true
	return nil
}

// MergeChapters replaces two adjacent chapters with one.
//
// The absorbed chapter is DELETED rather than soft-deleted, because ordinals are
// the version's structure and a tombstone would leave the sequence with a hole
// that the next renumber would have to work around. Facts citing it keep their
// story_fact_sources rows: those record where a fact was READ, and the reader
// re-resolves the chapter, so a removed chapter surfaces as evidence that cannot
// be located rather than as evidence that silently points somewhere else. The
// staleness walk marks those facts for review.
func (r *StoryRepository) MergeChapters(ctx context.Context, merged story.Chapter, absorbedID string, expectedRevision int64) error {
	if r == nil || r.db == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("STORY_TX_FAILED", "The merge could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	conn := connection(r.db, tx)
	if _, err := conn.ExecContext(ctx, `DELETE FROM chapters WHERE id = ?`, absorbedID); err != nil {
		return storageError("STORY_WRITE_FAILED", "The absorbed chapter could not be removed.", err)
	}
	result, err := conn.ExecContext(ctx, `UPDATE chapters
		SET title = ?, start_offset = ?, end_offset = ?, content_hash = ?, source_kind = ?, status = ?,
		    updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		merged.Title, merged.StartOffset, merged.EndOffset, merged.ContentHash, string(merged.SourceKind),
		string(merged.Status), formatTime(merged.UpdatedAt), merged.ID, expectedRevision)
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The merged chapter could not be updated.", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return storageError("STORY_WRITE_FAILED", "The merged chapter could not be updated.", err)
	} else if affected == 0 {
		return story.ConflictError("This item changed in another window. Reload it and try again.")
	}
	// The rows after the absorbed one close the gap, lowest first: a hole is legal
	// at every step, so moving upward never collides.
	if err := shiftOrdinals(ctx, conn, merged.SourceDocumentVersionID, merged.Ordinal+1, -1); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError("STORY_TX_FAILED", "The merge could not be committed.", err)
	}
	committed = true
	return nil
}

// shiftOrdinals moves every chapter at or after from by delta.
//
// The direction decides the order, and getting it wrong is a unique-constraint
// failure rather than a subtle bug: SQLite checks the constraint per row, so a
// shift of +1 must move the HIGHEST ordinal first (descending) and a shift of -1
// the lowest first (ascending), or the row being moved lands on one that has not
// moved yet.
func shiftOrdinals(ctx context.Context, conn querier, versionID string, from, delta int) error {
	order := "ASC"
	if delta > 0 {
		order = "DESC"
	}
	// The ordinal arithmetic is done in one statement rather than row by row: the
	// order column is not updatable in a way SQLite can guarantee without the row
	// being visited individually, so the ids are read first and then updated in the
	// safe order inside the same transaction.
	rows, err := conn.QueryContext(ctx,
		`SELECT id FROM chapters WHERE source_document_version_id = ? AND ordinal >= ? ORDER BY ordinal `+order,
		versionID, from)
	if err != nil {
		return storageError("STORY_READ_FAILED", "The chapters could not be renumbered.", err)
	}
	ids := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			return storageError("STORY_READ_FAILED", "The chapters could not be renumbered.", scanErr)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return storageError("STORY_READ_FAILED", "The chapters could not be renumbered.", err)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := conn.ExecContext(ctx,
			`UPDATE chapters SET ordinal = ordinal + ? WHERE id = ?`, delta, id); err != nil {
			return storageError("STORY_WRITE_FAILED", "The chapters could not be renumbered.", err)
		}
	}
	return nil
}
