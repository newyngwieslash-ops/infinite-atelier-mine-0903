package database

import (
	"context"
	"database/sql"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"

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
			&usage.UsageRole, &required, &createdAt, &usage.Params); err != nil {
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

// ListFilesWithTypes returns one version's files with the facts section 11.2's "文件存在和类型"
// compares — whether bytes exist, what kind they are, how big.
//
// # Why it joins rather than calling ListFiles
//
// `asset_files` carries the hash and the role, and the TYPE and the SIZE live on `file_objects`, one
// join away. The rule needs all four, and a caller assembling them from two reads would do it once
// per file — or hold a map of hashes to objects that nothing invalidates. A LEFT JOIN is what states
// the fault this rule exists for: a link whose object is GONE comes back with both joined columns
// null, which is a state the rule reports as "the link survives the bytes" rather than as "no files".
//
// The scan handles those nulls with `sql.NullString` and `sql.NullInt64` rather than `COALESCE`,
// because the difference is the finding.
func (r *AssetRepository) ListFilesWithTypes(ctx context.Context, versionID string) ([]appconsistency.AssetFile, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT f.file_hash, f.role, o.mime_type, o.size_bytes
		FROM asset_files f
		LEFT JOIN file_objects o ON o.hash = f.file_hash
		WHERE f.asset_version_id = ?
		ORDER BY f.ordinal ASC, f.file_hash ASC`, versionID)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The asset version's files could not be read.", err)
	}
	defer rows.Close()
	files := []appconsistency.AssetFile{}
	for rows.Next() {
		var file appconsistency.AssetFile
		var role string
		var mime sql.NullString
		var size sql.NullInt64
		if err := rows.Scan(&file.Hash, &role, &mime, &size); err != nil {
			return nil, storageError("ASSET_READ_FAILED", "The asset version's files could not be read.", err)
		}
		file.Role = asset.FileRole(role)
		// An absent object leaves MIMEType empty and SizeBytes zero, which is exactly how the rule
		// distinguishes "the bytes are gone" from "the type is wrong".
		file.MIMEType = mime.String
		file.SizeBytes = size.Int64
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The asset version's files could not be read.", err)
	}
	return files, nil
}

// FailedJobsForConsumers returns the failed jobs of one consumer type among a set of ids.
//
// # Why the filter is a set and not one call per row
//
// A board has tens of rows and the safety rule asks about all of them at once, so the ids travel as
// one `IN` list: a call per row would be the N+1 shape this package's read models avoid, and the
// answer is a set the rule filters rather than a per-row fact it joins.
//
// # Why it selects only the three columns the rule reads
//
// `jobSelectColumns` carries the whole row — result JSON, cancellation flags, lease state — and a
// rule that wants to know WHY a generation failed would be dragging a megabyte of result payload
// across the wire per board. What the safety rule compares is the error CATEGORY, so that is what
// this reads.
//
// # Why failed only
//
// A refused job is one that FAILED: the provider returned an error and the worker classified it. A
// job that is retrying has not been refused yet, and one that succeeded was not refused at all. The
// status filter is therefore `failed` rather than "not succeeded", which would include every job
// still in flight.
func (r *JobRepository) FailedJobsForConsumers(ctx context.Context, consumerType string, consumerIDs []string) ([]appconsistency.JobFailure, error) {
	if r == nil || r.db == nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	if len(consumerIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, 0, len(consumerIDs))
	args := make([]any, 0, len(consumerIDs)+2)
	args = append(args, consumerType, string(job.StatusFailed))
	for _, id := range consumerIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, entity_type, entity_id, error_code
		FROM generation_jobs
		WHERE entity_type = ? AND status = ? AND entity_id IN (`+strings.Join(placeholders, ", ")+`)`,
		args...)
	if err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The failed jobs could not be read.")
	}
	defer rows.Close()
	failures := []appconsistency.JobFailure{}
	for rows.Next() {
		var failure appconsistency.JobFailure
		if err := rows.Scan(&failure.JobID, &failure.EntityType, &failure.EntityID, &failure.ErrorCode); err != nil {
			return nil, job.FailedJobError(job.CategoryStorage, "The failed jobs could not be read.")
		}
		failures = append(failures, failure)
	}
	if err := rows.Err(); err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The failed jobs could not be read.")
	}
	return failures, nil
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
