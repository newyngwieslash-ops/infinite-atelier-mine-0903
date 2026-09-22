package database

import (
	"context"
	"database/sql"

	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// consistency_reads.go holds the two reads the deterministic checks need and nothing else had.
//
// They are on the ASSET and STORY repositories' own tables, so they live in those repositories'
// files' package rather than in a new one — but they are gathered here because they exist for one
// caller and reading them together says why. A reader looking at UsagesForConsumer should know that
// the consumer is a storyboard item and the caller is a continuity rule.

// UsagesForConsumer returns the usages recorded against one consumer.
//
// `asset_usages` has an index on the CONSUMER (`idx_asset_usages_consumer`), so this is the read
// that index was for: "what does this storyboard row use". The existing `ListUsages` reads the other
// direction — the consumers of one asset VERSION — which answers "what breaks if this changes"
// rather than "what does this row cite".
func (r *AssetRepository) UsagesForConsumer(ctx context.Context, consumerType asset.ConsumerType, consumerID string) ([]asset.Usage, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	if !asset.IsValidConsumerType(consumerType) {
		return nil, asset.InvalidError("The consumer kind is not recognised.")
	}
	rows, err := conn.QueryContext(ctx, assetUsageSelectColumns+
		` WHERE consumer_type = ? AND consumer_id = ? ORDER BY usage_role, id`, string(consumerType), consumerID)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The asset usages could not be read.", err)
	}
	defer rows.Close()
	usages := []asset.Usage{}
	for rows.Next() {
		var usage asset.Usage
		var consumerType string
		var required int
		var createdAt string
		if err := rows.Scan(&usage.ID, &usage.AssetVersionID, &consumerType, &usage.ConsumerID,
			&usage.UsageRole, &required, &createdAt); err != nil {
			return nil, storageError("ASSET_READ_FAILED", "The asset usages could not be read.", err)
		}
		usage.ConsumerType = asset.ConsumerType(consumerType)
		usage.Required = required != 0
		usage.CreatedAt = parseTime(createdAt)
		usages = append(usages, usage)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The asset usages could not be read.", err)
	}
	return usages, nil
}

// CostumeStateAt returns the costume asset version a character is wearing at a story position.
//
// It answers DOMAIN_MODEL section 6.8's question: a CharacterState spans FromEventOrder to
// ToEventOrder (nil meaning "still in force"), so the state in force at position N is the one whose
// span contains N. The candidate with the HIGHEST FromEventOrder wins when spans overlap, because a
// state that begins later is the more specific statement about that point — a character's costume
// across a whole act and then a change within it is two rows, and the change is the answer.
//
// The second return is `found`: a character with no state covering the position is a real and
// common case (nobody has recorded their costume), and the caller must be able to tell it apart from
// a state that names no costume version. Returning an empty string in both cases would make the
// continuity rule fire on every un-costumed character, which is the false-positive shape that makes
// a deterministic check worse than no check.
func (r *StoryRepository) CostumeStateAt(ctx context.Context, characterEntityID string, eventOrder int) (string, bool, error) {
	if r == nil || r.db == nil {
		return "", false, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	row := r.db.QueryRowContext(ctx, `SELECT costume_asset_version_id FROM character_states
		WHERE character_entity_id = ?
		  AND from_event_order <= ?
		  AND (to_event_order IS NULL OR to_event_order >= ?)
		  AND status IN ('candidate', 'accepted', 'locked')
		ORDER BY from_event_order DESC, id DESC LIMIT 1`,
		characterEntityID, eventOrder, eventOrder)
	var costume string
	err := row.Scan(&costume)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, storageError("STORY_READ_FAILED", "The character state could not be read.", err)
	}
	return costume, true, nil
}

// StoryEventParticipantsFor returns the entity ids involved in one story event.
//
// It is the read the prop rule needs: a row may cite a prop only when the prop's own story entity
// takes part in the event the scene is adapting. The existing read takes a story event id and
// returns the participant rows; this one answers the question the rule asks, which is membership.
func (r *StoryRepository) StoryEventParticipantsFor(ctx context.Context, storyEventID string) ([]string, error) {
	if r == nil || r.db == nil {
		return nil, storageError("STORY_STORE_UNAVAILABLE", "The story store is unavailable.", nil)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT story_entity_id FROM story_event_participants
		WHERE story_event_id = ? ORDER BY story_entity_id`, storyEventID)
	if err != nil {
		return nil, storageError("STORY_READ_FAILED", "The event participants could not be read.", err)
	}
	defer rows.Close()
	participants := []string{}
	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			return nil, storageError("STORY_READ_FAILED", "The event participants could not be read.", err)
		}
		participants = append(participants, entityID)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("STORY_READ_FAILED", "The event participants could not be read.", err)
	}
	return participants, nil
}

// The compile-time proofs that these reads satisfy the checker's ports.
//
// Both are narrow interfaces declared by the consistency package, and the same reasoning the memory
// repository's assertions state applies: a signature drift is invisible without them, because the
// checker takes interfaces and a repository that stopped satisfying one would simply leave the rule
// unable to run.
var (
	_ appconsistency.AssetReader      = (*AssetRepository)(nil)
	_ appconsistency.StoryStateReader = (*StoryRepository)(nil)
)

// storyStateReaderAlias keeps the story import used when the assertions above are the only
// reference to a story type. It is a compile-time no-op.
var _ = story.FactStatus("")
