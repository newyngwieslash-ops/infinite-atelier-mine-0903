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
