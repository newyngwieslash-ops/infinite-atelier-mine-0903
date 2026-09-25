package database

import (
	"context"
	"database/sql"
	"strings"
	"time"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// character_voices.go stores FR-080's 多角色声线映射 (migration 000026).
//
// # The two reads, and why there are only two
//
// `GetVoice` answers "what does this character sound like" — the one the submission path asks — and
// `ListVoices` answers "who has been cast in this project", which is what a casting panel shows. Both
// are keyed on the project, and the listing JOINS the character's name because a panel that showed
// entity identifiers would be unreadable.
//
// # Why an assignment is an upsert rather than an insert
//
// The schema's UNIQUE index on (project_id, character_entity_id) is what makes a second assignment
// update the first. Doing that in SQL rather than in Go means two concurrent assignments cannot both
// insert and leave the table with two answers — the constraint refuses the second, and this method
// turns that refusal into the update it was meant to be.

// VoiceRepository stores character voice mappings.
type VoiceRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewVoiceRepository builds the repository over a database handle.
func NewVoiceRepository(db *sql.DB) *VoiceRepository {
	return &VoiceRepository{db: db}
}

// WithinTx returns a repository bound to a transaction.
func (r *VoiceRepository) WithinTx(tx *sql.Tx) *VoiceRepository {
	return &VoiceRepository{db: r.db, tx: tx}
}

func (r *VoiceRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

const voiceSelectColumns = `SELECT id, project_id, character_entity_id, provider_config_id, model, voice,
	created_at, updated_at, revision FROM character_voices`

// GetVoice returns one character's mapping. The second result is false when the character has none,
// which is the ordinary state of a project that has not cast anybody yet — a not-found rather than an
// error, because the caller resolves a fallback rather than reporting a fault.
func (r *VoiceRepository) GetVoice(ctx context.Context, projectID, characterEntityID string) (media.CharacterVoice, bool, error) {
	conn := r.conn()
	if conn == nil {
		return media.CharacterVoice{}, false, media.StorageError("The voice store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, voiceSelectColumns+
		` WHERE project_id = ? AND character_entity_id = ?`, projectID, characterEntityID)
	record, err := scanCharacterVoice(row)
	if err == sql.ErrNoRows {
		return media.CharacterVoice{}, false, nil
	}
	if err != nil {
		return media.CharacterVoice{}, false, media.StorageError("The voice mapping could not be read.", err)
	}
	return record, true, nil
}

// AssignVoice records a casting decision, replacing any previous one for that character.
//
// `expectedRevision` is zero for "I am not editing an existing mapping", which is what a first
// assignment is. A non-zero value guards an edit: the update names it, so an assignment built from a
// stale panel is refused rather than silently overwriting a change somebody else made.
//
// The returned record is what was stored, because the caller shows it — a revision the caller guessed
// would be wrong the moment two assignments raced.
func (r *VoiceRepository) AssignVoice(ctx context.Context, record media.CharacterVoice, expectedRevision int64) (media.CharacterVoice, error) {
	conn := r.conn()
	if conn == nil {
		return media.CharacterVoice{}, media.StorageError("The voice store is unavailable.", nil)
	}
	record = record.Normalized()
	if err := record.Validate(); err != nil {
		return media.CharacterVoice{}, err
	}
	if strings.TrimSpace(record.ID) == "" {
		return media.CharacterVoice{}, media.InvalidError("A voice mapping needs an identifier.")
	}
	now := formatTime(time.Now().UTC())
	// The upsert is one statement so two concurrent assignments cannot both insert: the constraint
	// fires inside SQLite, and DO UPDATE is the update the second caller meant.
	//
	// THE REVISION GUARD IS IN THE WHERE CLAUSE of the update arm rather than in a separate read:
	// `revision = ?` matches only the row the caller saw, so a stale assignment changes nothing and
	// the SELECT after it reports what is actually there for the caller to disagree with.
	var revision int64
	err := conn.QueryRowContext(ctx, `INSERT INTO character_voices
		(id, project_id, character_entity_id, provider_config_id, model, voice, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(project_id, character_entity_id) DO UPDATE SET
			provider_config_id = excluded.provider_config_id,
			model = excluded.model,
			voice = excluded.voice,
			updated_at = excluded.updated_at,
			revision = character_voices.revision + 1
		WHERE (? = 0 OR character_voices.revision = ?)
		RETURNING revision`,
		record.ID, record.ProjectID, record.CharacterEntityID, record.ProviderConfigID, record.Model,
		record.Voice, now, now, expectedRevision, expectedRevision).Scan(&revision)
	if err == sql.ErrNoRows {
		// The conflict arm matched no row, which means the expected revision was stale. It is a
		// conflict rather than a storage fault: the caller's next step is to reload and look.
		return media.CharacterVoice{}, media.ConflictError(
			"Another change to this character's voice was saved first. Reload and try again.")
	}
	if err != nil {
		return media.CharacterVoice{}, media.StorageError("The voice mapping could not be saved.", err)
	}
	stored := record
	stored.Revision = revision
	stored.CreatedAt = parseTime(now)
	stored.UpdatedAt = stored.CreatedAt
	stored.ProviderConfigID = record.ProviderConfigID
	return stored, nil
}

// ClearVoice removes a character's mapping.
//
// It returns false when there was nothing to remove rather than reporting an error, because clearing
// an unmapped character is what a user does to be sure, and a refusal would make that a correction to
// make instead of a state to reach.
func (r *VoiceRepository) ClearVoice(ctx context.Context, projectID, characterEntityID string) (bool, error) {
	conn := r.conn()
	if conn == nil {
		return false, media.StorageError("The voice store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `DELETE FROM character_voices
		WHERE project_id = ? AND character_entity_id = ?`, projectID, characterEntityID)
	if err != nil {
		return false, media.StorageError("The voice mapping could not be cleared.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, media.StorageError("The voice mapping could not be cleared.", err)
	}
	return affected > 0, nil
}

// ListVoices returns every casting decision in a project, with the character's name.
//
// The name comes from the join rather than from a second read per row: a panel lists characters, and
// N+1 queries to render one table is the shape this repository avoids.
func (r *VoiceRepository) ListVoices(ctx context.Context, projectID string) ([]appmedia.VoiceMapping, error) {
	conn := r.conn()
	if conn == nil {
		return nil, media.StorageError("The voice store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT v.id, v.project_id, v.character_entity_id,
			e.canonical_name, v.provider_config_id, v.model, v.voice, v.created_at, v.updated_at, v.revision
		FROM character_voices v
		JOIN story_entities e ON e.id = v.character_entity_id
		WHERE v.project_id = ?
		ORDER BY e.canonical_name, v.id`, projectID)
	if err != nil {
		return nil, media.StorageError("The voice mappings could not be read.", err)
	}
	defer rows.Close()
	mappings := []appmedia.VoiceMapping{}
	for rows.Next() {
		var record media.CharacterVoice
		var mapping appmedia.VoiceMapping
		var createdAt, updatedAt string
		if err := rows.Scan(&record.ID, &record.ProjectID, &record.CharacterEntityID,
			&mapping.CharacterName, &record.ProviderConfigID, &record.Model, &record.Voice,
			&createdAt, &updatedAt, &record.Revision); err != nil {
			return nil, media.StorageError("The voice mappings could not be read.", err)
		}
		// The timestamps come back as TEXT and are converted here rather than scanned into a time.Time:
		// SQLite has no time type and the driver does not convert, so scanning one straight in fails
		// with a type error at RUN time rather than at compile time. `scanCharacterVoice` does the same
		// for the single-row read, which is why the two must be kept in step.
		record.CreatedAt = parseTime(createdAt)
		record.UpdatedAt = parseTime(updatedAt)
		mapping.CharacterVoice = record
		mappings = append(mappings, mapping)
	}
	if err := rows.Err(); err != nil {
		return nil, media.StorageError("The voice mappings could not be read.", err)
	}
	return mappings, nil
}

// scanCharacterVoice reads one row in the shared column order.
func scanCharacterVoice(row *sql.Row) (media.CharacterVoice, error) {
	var record media.CharacterVoice
	var createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.ProjectID, &record.CharacterEntityID, &record.ProviderConfigID,
		&record.Model, &record.Voice, &createdAt, &updatedAt, &record.Revision); err != nil {
		return media.CharacterVoice{}, err
	}
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// The compile-time proof that this repository satisfies the voice service's port.
//
// It lives here rather than beside the interface, because the assertion's value is the COMPILER
// noticing a signature drift in the implementation — an assertion in the application package would
// only restate what the interface already says.
var _ appmedia.VoiceRepository = (*VoiceRepository)(nil)
