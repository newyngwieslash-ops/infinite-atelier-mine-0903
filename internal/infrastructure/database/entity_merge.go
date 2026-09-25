package database

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// entity_merge.go is FR-030's 「用户可合并重复实体并保留别名」.
//
// # What a merge has to move, and why the list is the whole design
//
// Two entities that are the same person were extracted from different chapters, so each carries
// references the other does not. Merging means the SURVIVOR keeps everything and the ABSORBED row
// goes — and "everything" is four tables plus one polymorphic column:
//
//	story_entity_aliases.story_entity_id          the absorbed entity's other names
//	story_event_participants.story_entity_id      the events it takes part in
//	character_states.character_entity_id          what it wears, owns and knows, per story position
//	story_fact_sources.fact_id (fact_type=entity) the original-text evidence behind it
//
// A merge that moved only the aliases would leave the absorbed entity's EVENTS pointing at a row that
// no longer exists, and the story graph would lose the fact that the character was in them. The list
// is stated here rather than discovered per call, and
// `TestAMergeMovesEveryReference` asserts each entry by making one.
//
// # Why the aliases need a collision rule
//
// `story_entity_aliases` has a unique index on (story_entity_id, alias), so moving an alias the
// survivor already has is a conflict the database would refuse — and refusing the whole merge over a
// duplicate NAME would block exactly the case the criterion is about: two extractions of one person
// often agree about a nickname. The duplicate is therefore DROPPED rather than moved, because the
// survivor already carries that name, and the count is reported so the caller can say what happened.
//
// # What it deliberately does not do
//
// It does not merge the absorbed entity's `canonical_name` into the survivor as an alias. Which name
// a merged character should be KNOWN by is a writer's decision, and this command has no basis for
// making it: the caller names the survivor precisely so that decision is theirs. The absorbed name is
// reported in the result so a UI can OFFER it as an alias, which is a different act from performing
// it silently.
type entityMerger struct {
	db *sql.DB
}

// NewEntityMerger builds the merger over a connection.
func NewEntityMerger(db *sql.DB) *entityMerger {
	return &entityMerger{db: db}
}

// MergeResult reports what a merge moved.
type MergeResult struct {
	// AliasesMoved counts the absorbed entity's names that the survivor did not already have.
	AliasesMoved int
	// AliasesDropped counts the names both carried. They are reported rather than silently skipped,
	// because a user merging two extractions of one character expects to be told when the two agree.
	AliasesDropped int
	// ParticipantsMoved counts the event participations.
	ParticipantsMoved int
	// CharacterStatesMoved counts the per-position states.
	CharacterStatesMoved int
	// FactSourcesMoved counts the original-text evidence rows.
	FactSourcesMoved int
	// AbsorbedName is the absorbed entity's canonical name, so a UI can offer it as an alias.
	AbsorbedName string
}

// MergeEntities moves every reference from `absorbedID` to `survivorID` and removes the absorbed row.
//
// The whole thing is ONE TRANSACTION. A merge half-applied would leave the absorbed entity's events
// pointing at a row that is gone, which is a corrupted fact layer rather than an incomplete one — and
// the store has no way to tell which half ran.
func (m *entityMerger) MergeEntities(ctx context.Context, survivorID, absorbedID string, expectedRevision int64) (MergeResult, error) {
	if m == nil || m.db == nil {
		return MergeResult{}, mergeError("ENTITY_MERGE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	survivorID = strings.TrimSpace(survivorID)
	absorbedID = strings.TrimSpace(absorbedID)
	if survivorID == "" || absorbedID == "" {
		return MergeResult{}, mergeError("ENTITY_MERGE_INVALID", "A merge needs both entities.", nil)
	}
	if survivorID == absorbedID {
		return MergeResult{}, mergeError("ENTITY_MERGE_INVALID", "An entity cannot be merged into itself.", nil)
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return MergeResult{}, mergeError("ENTITY_MERGE_TX_FAILED", "The merge could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Both rows are read INSIDE the transaction, so the guard below and the moves that follow see one
	// state of the world.
	var absorbedName, absorbedStatus string
	if err := tx.QueryRowContext(ctx,
		`SELECT canonical_name, status FROM story_entities WHERE id = ?`, absorbedID).Scan(&absorbedName, &absorbedStatus); err != nil {
		if err == sql.ErrNoRows {
			return MergeResult{}, mergeError("ENTITY_MERGE_NOT_FOUND", "The entity to absorb does not exist.", err)
		}
		return MergeResult{}, mergeError("ENTITY_MERGE_READ_FAILED", "The entity could not be read.", err)
	}
	result := MergeResult{AbsorbedName: absorbedName}

	// The survivor's revision is the caller's concurrency token. A merge changes the survivor's own
	// row (its reference set), so a stale caller is refused rather than merged into a state it did not
	// read.
	if expectedRevision > 0 {
		update, err := tx.ExecContext(ctx,
			`UPDATE story_entities SET revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?`,
			formatTime(time.Now().UTC()), survivorID, expectedRevision)
		if err != nil {
			return MergeResult{}, mergeError("ENTITY_MERGE_WRITE_FAILED", "The surviving entity could not be updated.", err)
		}
		affected, err := update.RowsAffected()
		if err != nil {
			return MergeResult{}, mergeError("ENTITY_MERGE_WRITE_FAILED", "The surviving entity could not be updated.", err)
		}
		if affected == 0 {
			// Either the survivor does not exist or its revision moved. Both are the same answer to a
			// caller: reload and try again.
			return MergeResult{}, storydomain.ConflictError("That entity changed in another window. Reload it and try again.")
		}
	}

	// --- the aliases, with the collision rule ------------------------------------------------
	//
	// A name the survivor ALREADY has is dropped rather than moved: the unique index is over
	// (story_entity_id, alias), and refusing the whole merge over a duplicate name would block the
	// case the criterion exists for.
	moved, err := tx.ExecContext(ctx, `
		UPDATE story_entity_aliases SET story_entity_id = ?
		WHERE story_entity_id = ?
		  AND alias NOT IN (SELECT alias FROM story_entity_aliases WHERE story_entity_id = ?)`,
		survivorID, absorbedID, survivorID)
	if err != nil {
		return MergeResult{}, mergeError("ENTITY_MERGE_WRITE_FAILED", "The aliases could not be moved.", err)
	}
	result.AliasesMoved = rowsAffected(moved)
	var dropped int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM story_entity_aliases WHERE story_entity_id = ?`, absorbedID).Scan(&dropped); err != nil {
		return MergeResult{}, mergeError("ENTITY_MERGE_READ_FAILED", "The remaining aliases could not be counted.", err)
	}
	result.AliasesDropped = dropped
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM story_entity_aliases WHERE story_entity_id = ?`, absorbedID); err != nil {
		return MergeResult{}, mergeError("ENTITY_MERGE_WRITE_FAILED", "The duplicate aliases could not be removed.", err)
	}

	// --- the participations, the states and the evidence --------------------------------------
	for _, move := range []struct {
		table  string
		column string
		count  *int
	}{
		{"story_event_participants", "story_entity_id", &result.ParticipantsMoved},
		{"character_states", "character_entity_id", &result.CharacterStatesMoved},
	} {
		// The table and column names are literals from THIS list rather than a caller's strings, which
		// is what makes interpolating them safe: a parameterised table name is not a thing SQL supports.
		statement := "UPDATE " + move.table + " SET " + move.column + " = ? WHERE " + move.column + " = ?"
		outcome, err := tx.ExecContext(ctx, statement, survivorID, absorbedID)
		if err != nil {
			return MergeResult{}, mergeError("ENTITY_MERGE_WRITE_FAILED", "A reference could not be moved.", err)
		}
		*move.count = rowsAffected(outcome)
	}

	// The evidence rows are POLYMORPHIC: `story_fact_sources` pairs a `fact_type` with a `fact_id`, so
	// the entity's evidence is found by that pair rather than by a column name.
	evidence, err := tx.ExecContext(ctx,
		`UPDATE story_fact_sources SET fact_id = ? WHERE fact_type = 'entity' AND fact_id = ?`,
		survivorID, absorbedID)
	if err != nil {
		return MergeResult{}, mergeError("ENTITY_MERGE_WRITE_FAILED", "The evidence references could not be moved.", err)
	}
	result.FactSourcesMoved = rowsAffected(evidence)

	// --- the absorbed row goes --------------------------------------------------------------
	//
	// AFTER the moves, because the foreign keys are ON DELETE CASCADE: deleting first would take the
	// aliases, participations and states with it — which is exactly the data the merge exists to keep.
	if _, err := tx.ExecContext(ctx, `DELETE FROM story_entities WHERE id = ?`, absorbedID); err != nil {
		return MergeResult{}, mergeError("ENTITY_MERGE_WRITE_FAILED", "The absorbed entity could not be removed.", err)
	}
	if err := tx.Commit(); err != nil {
		return MergeResult{}, mergeError("ENTITY_MERGE_TX_FAILED", "The merge could not be saved.", err)
	}
	committed = true
	return result, nil
}

func rowsAffected(result sql.Result) int {
	count, err := result.RowsAffected()
	if err != nil {
		return 0
	}
	return int(count)
}

func mergeError(code, message string, cause error) error {
	return apperror.New(code, "storage", false, message, cause)
}
