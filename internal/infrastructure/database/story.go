package database

import (
	"context"
	"database/sql"
	"strings"

	storyapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// StoryRepository is the SQLite implementation of the story port. It owns
// every statement touching source documents, their versions, chapters and the
// fact layer of migration 000007.
//
// The fact layer reads and writes rows only; the extraction that proposes them
// and the rules that confirm them belong to the application layer, which is
// what keeps this file free of story logic.
type StoryRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewStoryRepository builds the repository over a database handle.
func NewStoryRepository(db *sql.DB) *StoryRepository {
	return &StoryRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
func (r *StoryRepository) WithinTx(tx *sql.Tx) *StoryRepository {
	return &StoryRepository{db: r.db, tx: tx}
}

func (r *StoryRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

const sourceDocumentSelectColumns = `SELECT id, project_id, document_type, name, current_version_id, status,
	deleted_at, deleted_by, created_at, updated_at, revision FROM source_documents`

// CreateSourceDocument stores a source document.
func (r *StoryRepository) CreateSourceDocument(ctx context.Context, record story.SourceDocument) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO source_documents
		(id, project_id, document_type, name, current_version_id, status, deleted_at, deleted_by,
		 created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, string(record.Type), record.Name, record.CurrentVersionID,
		string(record.Status), formatTime(record.DeletedAt), record.DeletedBy,
		formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return story.ConflictError("A source document with that id already exists.")
		}
		return storageError("STORY_WRITE_FAILED", "The source document could not be saved.", err)
	}
	return nil
}

// GetSourceDocument returns one source document.
func (r *StoryRepository) GetSourceDocument(ctx context.Context, id string) (story.SourceDocument, error) {
	conn := r.conn()
	if conn == nil {
		return story.SourceDocument{}, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, sourceDocumentSelectColumns+` WHERE id = ?`, id)
	record, err := scanSourceDocument(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return story.SourceDocument{}, story.NotFoundError()
		}
		return story.SourceDocument{}, storageError("STORY_READ_FAILED", "The source document could not be read.", err)
	}
	return record, nil
}

// ListSourceDocuments returns a project's documents oldest first, without the
// rows a soft delete hid.
func (r *StoryRepository) ListSourceDocuments(ctx context.Context, projectID string) ([]story.SourceDocument, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, sourceDocumentSelectColumns+
		` WHERE project_id = ? AND deleted_at = '' ORDER BY created_at ASC, id ASC`, projectID)
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The source documents could not be read.", err)
	}
	defer rows.Close()
	return scanSourceDocuments(rows)
}

// UpdateSourceDocument persists a change guarded by the expected revision.
//
// The document's soft-delete stamp is written from the record rather than
// cleared here: a trashed document stays trashed, and trashing is a status
// change the caller made.
func (r *StoryRepository) UpdateSourceDocument(ctx context.Context, record story.SourceDocument, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE source_documents
		SET document_type = ?, name = ?, current_version_id = ?, status = ?, deleted_at = ?, deleted_by = ?,
		    updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		string(record.Type), record.Name, record.CurrentVersionID, string(record.Status),
		formatTime(record.DeletedAt), record.DeletedBy, formatTime(record.UpdatedAt),
		record.ID, expectedRevision)
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The source document could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The source document could not be updated.", err)
	}
	if affected == 0 {
		return story.ConflictError("This item changed in another window. Reload it and try again.")
	}
	return nil
}

const sourceDocumentVersionSelectColumns = `SELECT id, source_document_id, version_number, source_hash,
	physical_file_id, normalized_text_file_id, content_hash, mime_type, encoding, char_count,
	import_metadata_json, created_by_type, created_at FROM source_document_versions`

// CreateSourceDocumentVersion stores one import of a document.
func (r *StoryRepository) CreateSourceDocumentVersion(ctx context.Context, version story.SourceDocumentVersion) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO source_document_versions
		(id, source_document_id, version_number, source_hash, physical_file_id, normalized_text_file_id, content_hash,
		 mime_type, encoding, char_count, import_metadata_json, created_by_type, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, version.SourceDocumentID, version.VersionNumber, version.SourceHash,
		version.PhysicalFileID,
		version.NormalizedTextFileID, version.ContentHash, version.MIMEType, version.Encoding,
		version.CharCount, version.ImportMetadataJSON, string(version.CreatedByType),
		formatTime(version.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return story.ConflictError("That version number is already used for this document.")
		}
		return storageError("STORY_WRITE_FAILED", "The document version could not be saved.", err)
	}
	return nil
}

// MaxSourceDocumentVersionNumber reports the highest version number a document
// has, or zero when it has none.
//
// MAX rather than COUNT, because a number that was used must not be handed out
// again: the unique constraint on (source_document_id, version_number) would
// reject the insert.
func (r *StoryRepository) MaxSourceDocumentVersionNumber(ctx context.Context, sourceDocumentID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	var value sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT MAX(version_number) FROM source_document_versions WHERE source_document_id = ?`,
		sourceDocumentID).Scan(&value); err != nil {
		return 0, storageError("STORY_READ_FAILED", "The document versions could not be read.", err)
	}
	if !value.Valid {
		return 0, nil
	}
	return int(value.Int64), nil
}

const chapterSelectColumns = `SELECT id, source_document_version_id, ordinal, title, start_offset, end_offset,
	content_hash, source_kind, status, created_at, updated_at, revision FROM chapters`

// CreateChapter stores a chapter of a document version.
func (r *StoryRepository) CreateChapter(ctx context.Context, chapter story.Chapter) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO chapters
		(id, source_document_version_id, ordinal, title, start_offset, end_offset, content_hash, source_kind, status,
		 created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chapter.ID, chapter.SourceDocumentVersionID, chapter.Ordinal, chapter.Title,
		chapter.StartOffset, chapter.EndOffset, chapter.ContentHash, string(chapter.SourceKind), string(chapter.Status),
		formatTime(chapter.CreatedAt), formatTime(chapter.UpdatedAt), chapter.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return story.ConflictError("That chapter position is already used for this version.")
		}
		return storageError("STORY_WRITE_FAILED", "The chapter could not be saved.", err)
	}
	return nil
}

// GetChapter returns one chapter.
func (r *StoryRepository) GetChapter(ctx context.Context, id string) (story.Chapter, error) {
	conn := r.conn()
	if conn == nil {
		return story.Chapter{}, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, chapterSelectColumns+` WHERE id = ?`, id)
	chapter, err := scanChapter(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return story.Chapter{}, story.NotFoundError()
		}
		return story.Chapter{}, storageError("STORY_READ_FAILED", "The chapter could not be read.", err)
	}
	return chapter, nil
}

// ListChapters returns a version's chapters in reading order.
func (r *StoryRepository) ListChapters(ctx context.Context, sourceDocumentVersionID string) ([]story.Chapter, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, chapterSelectColumns+
		` WHERE source_document_version_id = ? ORDER BY ordinal ASC`, sourceDocumentVersionID)
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The chapters could not be read.", err)
	}
	defer rows.Close()
	return scanChapters(rows)
}

// UpdateChapter persists a chapter correction under a revision guard.
func (r *StoryRepository) UpdateChapter(ctx context.Context, chapter story.Chapter, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE chapters
		SET title = ?, start_offset = ?, end_offset = ?, content_hash = ?, status = ?, updated_at = ?,
		    revision = revision + 1
		WHERE id = ? AND revision = ?`,
		chapter.Title, chapter.StartOffset, chapter.EndOffset, chapter.ContentHash,
		string(chapter.Status), formatTime(chapter.UpdatedAt), chapter.ID, expectedRevision)
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The chapter could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The chapter could not be updated.", err)
	}
	if affected == 0 {
		return story.ConflictError("This item changed in another window. Reload it and try again.")
	}
	return nil
}

const storyEntitySelectColumns = `SELECT id, project_id, entity_type, canonical_name, status, source_scope,
	current_profile_version_id, deleted_at, deleted_by, created_at, updated_at, revision FROM story_entities`

// CreateStoryEntity stores a story entity.
func (r *StoryRepository) CreateStoryEntity(ctx context.Context, record story.StoryEntity) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO story_entities
		(id, project_id, entity_type, canonical_name, status, source_scope, current_profile_version_id,
		 deleted_at, deleted_by, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, string(record.Type), record.CanonicalName, string(record.Status),
		string(record.SourceScope), record.CurrentProfileVersionID, formatTime(record.DeletedAt),
		record.DeletedBy, formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return story.ConflictError("A story entity with that id already exists.")
		}
		return storageError("STORY_WRITE_FAILED", "The story entity could not be saved.", err)
	}
	return nil
}

// GetStoryEntity returns one story entity.
func (r *StoryRepository) GetStoryEntity(ctx context.Context, id string) (story.StoryEntity, error) {
	conn := r.conn()
	if conn == nil {
		return story.StoryEntity{}, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyEntitySelectColumns+` WHERE id = ?`, id)
	record, err := scanStoryEntity(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return story.StoryEntity{}, story.NotFoundError()
		}
		return story.StoryEntity{}, storageError("STORY_READ_FAILED", "The story entity could not be read.", err)
	}
	return record, nil
}

// UpdateStoryEntity persists a fact decision under a revision guard.
func (r *StoryRepository) UpdateStoryEntity(ctx context.Context, record story.StoryEntity, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE story_entities
		SET entity_type = ?, canonical_name = ?, status = ?, source_scope = ?, current_profile_version_id = ?,
		    deleted_at = ?, deleted_by = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		string(record.Type), record.CanonicalName, string(record.Status), string(record.SourceScope),
		record.CurrentProfileVersionID, formatTime(record.DeletedAt), record.DeletedBy,
		formatTime(record.UpdatedAt), record.ID, expectedRevision)
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The story entity could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The story entity could not be updated.", err)
	}
	if affected == 0 {
		return story.ConflictError("This item changed in another window. Reload it and try again.")
	}
	return nil
}

const storyEventSelectColumns = `SELECT id, project_id, chapter_id, ordinal, name, description, event_type,
	story_time_text, story_time_order, location_entity_id, cause_summary, result_summary, importance,
	confidence, status, source_scope, created_by_agent_run_id, deleted_at, deleted_by, created_at,
	updated_at, revision FROM story_events`

// CreateStoryEvent stores a story event.
func (r *StoryRepository) CreateStoryEvent(ctx context.Context, record story.StoryEvent) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO story_events
		(id, project_id, chapter_id, ordinal, name, description, event_type, story_time_text,
		 story_time_order, location_entity_id, cause_summary, result_summary, importance, confidence,
		 status, source_scope, created_by_agent_run_id, deleted_at, deleted_by, created_at, updated_at,
		 revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, record.ChapterID, record.Ordinal, record.Name, record.Description,
		record.EventType, record.StoryTimeText, nullableInt(record.StoryTimeOrder), record.LocationEntityID,
		record.CauseSummary, record.ResultSummary, record.Importance, record.Confidence,
		string(record.Status), string(record.SourceScope), record.CreatedByAgentRunID,
		formatTime(record.DeletedAt), record.DeletedBy, formatTime(record.CreatedAt),
		formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return story.ConflictError("A story event with that id already exists.")
		}
		return storageError("STORY_WRITE_FAILED", "The story event could not be saved.", err)
	}
	return nil
}

// GetStoryEvent returns one story event.
func (r *StoryRepository) GetStoryEvent(ctx context.Context, id string) (story.StoryEvent, error) {
	conn := r.conn()
	if conn == nil {
		return story.StoryEvent{}, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyEventSelectColumns+` WHERE id = ?`, id)
	record, err := scanStoryEvent(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return story.StoryEvent{}, story.NotFoundError()
		}
		return story.StoryEvent{}, storageError("STORY_READ_FAILED", "The story event could not be read.", err)
	}
	return record, nil
}

// UpdateStoryEvent persists a fact decision under a revision guard.
func (r *StoryRepository) UpdateStoryEvent(ctx context.Context, record story.StoryEvent, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE story_events
		SET chapter_id = ?, ordinal = ?, name = ?, description = ?, event_type = ?, story_time_text = ?,
		    story_time_order = ?, location_entity_id = ?, cause_summary = ?, result_summary = ?,
		    importance = ?, confidence = ?, status = ?, source_scope = ?, created_by_agent_run_id = ?,
		    deleted_at = ?, deleted_by = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		record.ChapterID, record.Ordinal, record.Name, record.Description, record.EventType,
		record.StoryTimeText, nullableInt(record.StoryTimeOrder), record.LocationEntityID,
		record.CauseSummary, record.ResultSummary, record.Importance, record.Confidence,
		string(record.Status), string(record.SourceScope), record.CreatedByAgentRunID,
		formatTime(record.DeletedAt), record.DeletedBy, formatTime(record.UpdatedAt),
		record.ID, expectedRevision)
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The story event could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The story event could not be updated.", err)
	}
	if affected == 0 {
		return story.ConflictError("This item changed in another window. Reload it and try again.")
	}
	return nil
}

const storyRelationSelectColumns = `SELECT id, project_id, relation_type, source_entity_type, source_entity_id,
	target_entity_type, target_entity_id, valid_from_event_id, valid_to_event_id, confidence, status,
	source_scope, created_at, updated_at, revision FROM story_relations`

// CreateStoryRelation stores one edge of the story graph.
func (r *StoryRepository) CreateStoryRelation(ctx context.Context, record story.StoryRelation) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO story_relations
		(id, project_id, relation_type, source_entity_type, source_entity_id, target_entity_type,
		 target_entity_id, valid_from_event_id, valid_to_event_id, confidence, status, source_scope,
		 created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, string(record.Type), record.SourceEntityType, record.SourceEntityID,
		record.TargetEntityType, record.TargetEntityID, record.ValidFromEventID, record.ValidToEventID,
		record.Confidence, string(record.Status), string(record.SourceScope),
		formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return story.ConflictError("A story relation with that id already exists.")
		}
		return storageError("STORY_WRITE_FAILED", "The story relation could not be saved.", err)
	}
	return nil
}

// GetStoryRelation returns one story relation.
func (r *StoryRepository) GetStoryRelation(ctx context.Context, id string) (story.StoryRelation, error) {
	conn := r.conn()
	if conn == nil {
		return story.StoryRelation{}, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyRelationSelectColumns+` WHERE id = ?`, id)
	record, err := scanStoryRelation(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return story.StoryRelation{}, story.NotFoundError()
		}
		return story.StoryRelation{}, storageError("STORY_READ_FAILED", "The story relation could not be read.", err)
	}
	return record, nil
}

const storyConflictSelectColumns = `SELECT id, project_id, left_fact_type, left_fact_id, right_fact_type,
	right_fact_id, conflict_type, status, resolution, resolved_by, created_at, resolved_at
	FROM story_fact_conflicts`

// CreateStoryConflict stores a fact conflict. The schema's unique constraint is
// over the ordered fact pair, so the same pair in one order is a conflict.
func (r *StoryRepository) CreateStoryConflict(ctx context.Context, record story.StoryFactConflict) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO story_fact_conflicts
		(id, project_id, left_fact_type, left_fact_id, right_fact_type, right_fact_id, conflict_type,
		 status, resolution, resolved_by, created_at, resolved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, string(record.LeftFactType), record.LeftFactID,
		string(record.RightFactType), record.RightFactID, record.ConflictType, string(record.Status),
		record.Resolution, record.ResolvedBy, formatTime(record.CreatedAt), formatTime(record.ResolvedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return story.ConflictError("That conflict is already recorded.")
		}
		return storageError("STORY_WRITE_FAILED", "The conflict could not be saved.", err)
	}
	return nil
}

// GetStoryConflict returns one fact conflict.
func (r *StoryRepository) GetStoryConflict(ctx context.Context, id string) (story.StoryFactConflict, error) {
	conn := r.conn()
	if conn == nil {
		return story.StoryFactConflict{}, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, storyConflictSelectColumns+` WHERE id = ?`, id)
	record, err := scanStoryConflict(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return story.StoryFactConflict{}, story.NotFoundError()
		}
		return story.StoryFactConflict{}, storageError("STORY_READ_FAILED", "The conflict could not be read.", err)
	}
	return record, nil
}

// UpdateStoryConflict persists a resolution.
//
// The guard is the status the caller read, because migration 000007 gives
// story_fact_conflicts no revision column: the row's state is the concurrency
// token, and a conflict resolved in another window matches no row here.
func (r *StoryRepository) UpdateStoryConflict(ctx context.Context, record story.StoryFactConflict, expectedStatus story.ConflictStatus) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE story_fact_conflicts
		SET conflict_type = ?, status = ?, resolution = ?, resolved_by = ?, resolved_at = ?
		WHERE id = ? AND status = ?`,
		record.ConflictType, string(record.Status), record.Resolution, record.ResolvedBy,
		formatTime(record.ResolvedAt), record.ID, string(expectedStatus))
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The conflict could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("STORY_WRITE_FAILED", "The conflict could not be updated.", err)
	}
	if affected == 0 {
		return story.ConflictError("This item changed in another window. Reload it and try again.")
	}
	return nil
}

// scanSourceDocument reads one source document row.
func scanSourceDocument(row rowScanner) (story.SourceDocument, error) {
	var record story.SourceDocument
	var documentType, status, deletedAt, deletedBy, createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.ProjectID, &documentType, &record.Name, &record.CurrentVersionID,
		&status, &deletedAt, &deletedBy, &createdAt, &updatedAt, &record.Revision); err != nil {
		return story.SourceDocument{}, err
	}
	record.Type = story.DocumentType(documentType)
	record.Status = story.DocumentStatus(status)
	record.DeletedAt = parseTime(deletedAt)
	record.DeletedBy = deletedBy
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// scanSourceDocuments reads a source document result set.
func scanSourceDocuments(rows *sql.Rows) ([]story.SourceDocument, error) {
	var records []story.SourceDocument
	for rows.Next() {
		record, err := scanSourceDocument(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// scanChapter reads one chapter row.
func scanChapter(row rowScanner) (story.Chapter, error) {
	var chapter story.Chapter
	var sourceKind, status, createdAt, updatedAt string
	if err := row.Scan(&chapter.ID, &chapter.SourceDocumentVersionID, &chapter.Ordinal, &chapter.Title,
		&chapter.StartOffset, &chapter.EndOffset, &chapter.ContentHash, &sourceKind, &status, &createdAt, &updatedAt,
		&chapter.Revision); err != nil {
		return story.Chapter{}, err
	}
	chapter.SourceKind = story.ChapterSourceKind(sourceKind)
	chapter.Status = story.ChapterStatus(status)
	chapter.CreatedAt = parseTime(createdAt)
	chapter.UpdatedAt = parseTime(updatedAt)
	return chapter, nil
}

// scanChapters reads a chapter result set.
func scanChapters(rows *sql.Rows) ([]story.Chapter, error) {
	var records []story.Chapter
	for rows.Next() {
		record, err := scanChapter(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// scanStoryEntity reads one story entity row.
func scanStoryEntity(row rowScanner) (story.StoryEntity, error) {
	var record story.StoryEntity
	var entityType, status, scope, deletedAt, deletedBy, createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.ProjectID, &entityType, &record.CanonicalName, &status, &scope,
		&record.CurrentProfileVersionID, &deletedAt, &deletedBy, &createdAt, &updatedAt,
		&record.Revision); err != nil {
		return story.StoryEntity{}, err
	}
	record.Type = story.EntityType(entityType)
	record.Status = story.FactStatus(status)
	record.SourceScope = story.SourceScope(scope)
	record.DeletedAt = parseTime(deletedAt)
	record.DeletedBy = deletedBy
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// scanStoryEvent reads one story event row.
//
// story_time_order is nullable in the schema and maps to a pointer, so an event
// whose story-clock position has not been computed stays distinguishable from
// one recorded at order zero (the same distinction jobs.go makes for progress).
func scanStoryEvent(row rowScanner) (story.StoryEvent, error) {
	var record story.StoryEvent
	var storyTimeOrder sql.NullInt64
	var eventType, storyTimeText, locationEntityID, causeSummary, resultSummary, importance, status, scope,
		agentRunID, deletedAt, deletedBy, createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.ProjectID, &record.ChapterID, &record.Ordinal, &record.Name,
		&record.Description, &eventType, &storyTimeText, &storyTimeOrder, &locationEntityID,
		&causeSummary, &resultSummary, &importance, &record.Confidence, &status, &scope, &agentRunID,
		&deletedAt, &deletedBy, &createdAt, &updatedAt, &record.Revision); err != nil {
		return story.StoryEvent{}, err
	}
	if storyTimeOrder.Valid {
		value := int(storyTimeOrder.Int64)
		record.StoryTimeOrder = &value
	}
	record.EventType = eventType
	record.StoryTimeText = storyTimeText
	record.LocationEntityID = locationEntityID
	record.CauseSummary = causeSummary
	record.ResultSummary = resultSummary
	record.Importance = importance
	record.Status = story.FactStatus(status)
	record.SourceScope = story.SourceScope(scope)
	record.CreatedByAgentRunID = agentRunID
	record.DeletedAt = parseTime(deletedAt)
	record.DeletedBy = deletedBy
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// scanStoryRelation reads one story relation row.
func scanStoryRelation(row rowScanner) (story.StoryRelation, error) {
	var record story.StoryRelation
	var relationType, sourceEntityType, sourceEntityID, targetEntityType, targetEntityID,
		validFromEventID, validToEventID, status, scope, createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.ProjectID, &relationType, &sourceEntityType, &sourceEntityID,
		&targetEntityType, &targetEntityID, &validFromEventID, &validToEventID, &record.Confidence,
		&status, &scope, &createdAt, &updatedAt, &record.Revision); err != nil {
		return story.StoryRelation{}, err
	}
	record.Type = story.RelationType(relationType)
	record.SourceEntityType = sourceEntityType
	record.SourceEntityID = sourceEntityID
	record.TargetEntityType = targetEntityType
	record.TargetEntityID = targetEntityID
	record.ValidFromEventID = validFromEventID
	record.ValidToEventID = validToEventID
	record.Status = story.FactStatus(status)
	record.SourceScope = story.SourceScope(scope)
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// scanStoryConflict reads one fact conflict row.
func scanStoryConflict(row rowScanner) (story.StoryFactConflict, error) {
	var record story.StoryFactConflict
	var leftFactType, rightFactType, status, createdAt, resolvedAt string
	if err := row.Scan(&record.ID, &record.ProjectID, &leftFactType, &record.LeftFactID, &rightFactType,
		&record.RightFactID, &record.ConflictType, &status, &record.Resolution, &record.ResolvedBy,
		&createdAt, &resolvedAt); err != nil {
		return story.StoryFactConflict{}, err
	}
	record.LeftFactType = story.FactType(leftFactType)
	record.RightFactType = story.FactType(rightFactType)
	record.Status = story.ConflictStatus(status)
	record.CreatedAt = parseTime(createdAt)
	record.ResolvedAt = parseTime(resolvedAt)
	return record, nil
}

// Ensure the repository satisfies the application port.
var _ storyapp.Repository = (*StoryRepository)(nil)

// scanSourceDocumentVersion reads one version row.
func scanSourceDocumentVersion(row rowScanner) (story.SourceDocumentVersion, error) {
	var version story.SourceDocumentVersion
	var createdByType, createdAt string
	if err := row.Scan(&version.ID, &version.SourceDocumentID, &version.VersionNumber, &version.SourceHash,
		&version.PhysicalFileID, &version.NormalizedTextFileID, &version.ContentHash, &version.MIMEType,
		&version.Encoding, &version.CharCount, &version.ImportMetadataJSON, &createdByType,
		&createdAt); err != nil {
		return story.SourceDocumentVersion{}, err
	}
	version.CreatedByType = story.CreatedByType(createdByType)
	version.CreatedAt = parseTime(createdAt)
	return version, nil
}

// GetSourceDocumentVersion returns one version by id.
func (r *StoryRepository) GetSourceDocumentVersion(ctx context.Context, id string) (story.SourceDocumentVersion, error) {
	conn := r.conn()
	if conn == nil {
		return story.SourceDocumentVersion{}, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, sourceDocumentVersionSelectColumns+` WHERE id = ?`, id)
	version, err := scanSourceDocumentVersion(row)
	if err == sql.ErrNoRows {
		return story.SourceDocumentVersion{}, story.NotFoundError()
	}
	if err != nil {
		return story.SourceDocumentVersion{}, storageError("STORY_READ_FAILED", "The document version could not be read.", err)
	}
	return version, nil
}

// FindVersionBySourceHash returns the newest version in a project whose original
// upload has this hash, with its document's name.
//
// The comparison is against source_hash rather than content_hash because PRD
// FR-020's warning is about the FILE a user is importing, and the same file
// re-exported with different line endings normalizes to a different content
// hash. Nothing has a reason to match an empty hash: a pasted document has no
// original file, so the caller is expected not to ask, and this refuses rather
// than returning an arbitrary row.
func (r *StoryRepository) FindVersionBySourceHash(ctx context.Context, projectID, sourceHash string) (story.SourceDocumentVersion, string, bool, error) {
	conn := r.conn()
	if conn == nil {
		return story.SourceDocumentVersion{}, "", false, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	if strings.TrimSpace(sourceHash) == "" {
		return story.SourceDocumentVersion{}, "", false, nil
	}
	// The join carries the document's name so the caller can name what it found
	// without a second read, and the project filter is what keeps the check
	// inside one drama.
	query := `SELECT ` + versionColumnsForJoin + `, source_documents.name
		FROM source_document_versions
		JOIN source_documents ON source_documents.id = source_document_versions.source_document_id
		WHERE source_documents.project_id = ? AND source_document_versions.source_hash = ?
		ORDER BY source_document_versions.created_at DESC, source_document_versions.id DESC
		LIMIT 1`
	row := conn.QueryRowContext(ctx, query, projectID, sourceHash)
	var version story.SourceDocumentVersion
	var documentName, createdByType, createdAt string
	err := row.Scan(&version.ID, &version.SourceDocumentID, &version.VersionNumber, &version.SourceHash,
		&version.PhysicalFileID, &version.NormalizedTextFileID, &version.ContentHash, &version.MIMEType,
		&version.Encoding, &version.CharCount, &version.ImportMetadataJSON, &createdByType,
		&createdAt, &documentName)
	if err == sql.ErrNoRows {
		return story.SourceDocumentVersion{}, "", false, nil
	}
	if err != nil {
		return story.SourceDocumentVersion{}, "", false, storageError("STORY_READ_FAILED", "The imported documents could not be read.", err)
	}
	version.CreatedByType = story.CreatedByType(createdByType)
	version.CreatedAt = parseTime(createdAt)
	return version, documentName, true, nil
}

// ConfirmChapters marks a version's boundaries confirmed and records the
// governance event in the same transaction.
//
// One transaction because the two are one act: a status change with no record of
// who confirmed it, or a record of a confirmation that rolled back, are both
// worse than a clean failure (ADR-0009).
//
// The UPDATE is guarded on status so an 'edited' boundary survives: a user who
// changed a boundary already made a stronger statement than 'confirmed', and
// overwriting the status would discard that.
func (r *StoryRepository) ConfirmChapters(ctx context.Context, sourceDocumentVersionID string, record event.Event) error {
	if r == nil || r.db == nil {
		return storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("STORY_TX_FAILED", "The confirmation could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	conn := connection(r.db, tx)

	if _, err := conn.ExecContext(ctx, `UPDATE chapters SET status = 'confirmed', updated_at = ?, revision = revision + 1
		WHERE source_document_version_id = ? AND status = 'detected'`,
		formatTime(record.OccurredAt), sourceDocumentVersionID); err != nil {
		return storageError("STORY_WRITE_FAILED", "The chapters could not be confirmed.", err)
	}
	if err := insertDomainEvent(ctx, conn, record); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError("STORY_TX_FAILED", "The confirmation could not be committed.", err)
	}
	committed = true
	return nil
}

// versionColumnsForJoin is the version column list qualified for a join, so the
// same columns can be read through source_documents without ambiguity.
const versionColumnsForJoin = `source_document_versions.id, source_document_versions.source_document_id,
	source_document_versions.version_number, source_document_versions.source_hash,
	source_document_versions.physical_file_id, source_document_versions.normalized_text_file_id,
	source_document_versions.content_hash, source_document_versions.mime_type, source_document_versions.encoding,
	source_document_versions.char_count, source_document_versions.import_metadata_json,
	source_document_versions.created_by_type, source_document_versions.created_at`

// insertDomainEvent records a §17 event inside a caller's transaction.
//
// The version_approval path has its own copy of this statement because it writes
// through a table name it composes; this one is shared by the story commands
// that record an event with their change.
func insertDomainEvent(ctx context.Context, conn querier, record event.Event) error {
	_, err := conn.ExecContext(ctx, `INSERT INTO domain_events
		(id, event_type, schema_version, aggregate_type, aggregate_id, project_id,
		 occurred_at, trace_id, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.EventID, string(record.EventType), record.SchemaVersion,
		string(record.AggregateType), record.AggregateID, record.ProjectID,
		formatTime(record.OccurredAt), record.TraceID, record.Payload,
		formatTime(record.OccurredAt))
	if err != nil {
		if isUniqueViolation(err) {
			return event.ConflictError("That event has already been recorded.")
		}
		if isForeignKeyViolation(err) {
			return event.InvalidError("That project does not exist.")
		}
		return storageError("EVENT_WRITE_FAILED", "The event could not be recorded.", err)
	}
	return nil
}
