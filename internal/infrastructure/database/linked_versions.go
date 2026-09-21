package database

import (
	"context"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// linked_versions.go writes the two link tables migration 000008 created, together with the version
// each set belongs to.
//
// The pairing is the point. DOMAIN_MODEL §7.4 and §7.5 make "which events did this skeleton select"
// and "what did this strategy do with each event" part of what those artifacts ARE, so a version
// written without its links is not a smaller version — it is an artifact that decided nothing, and
// a reviewer reading it would see a strategy with no strategy in it. One transaction makes that
// state unrepresentable, which is why these methods exist instead of a separate `Link*` call the
// service could forget.
//
// # What the schema does and does not enforce here
//
// `story_event_id` has NO foreign key in either link table — the same choice `scenes` made — because
// a citation is provenance and provenance outlives the row it names: deleting a story event must not
// delete the strategy that decided to keep it. So the reference check is the SERVICE's, and the
// constraint these methods still reach is the primary key on (version, event), which refuses a set
// naming one event twice.
//
// Nothing here catches `isForeignKeyViolation` for that reason. An unknown event is refused by the
// service's `MissingStoryEventIDs` before the write; a foreign-key failure reaching these paths would
// mean the VERSION does not exist, which the caller's own read of the row has already excluded.

// CreateStorySkeletonVersionWithLinks stores one skeleton version and the events it selected.
//
// An empty selection is legal and writes no link rows: a skeleton that selected nothing is a draft
// whose selection has not been made yet, and refusing it would fail a stage for a reason the model
// cannot act on.
func (r *ScriptRepository) CreateStorySkeletonVersionWithLinks(ctx context.Context, record script.StorySkeletonVersion, eventIDs []string) error {
	if r == nil || r.db == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	return r.withinTx(ctx, func(repo *ScriptRepository) error {
		if err := repo.CreateStorySkeletonVersion(ctx, record); err != nil {
			return err
		}
		if len(eventIDs) == 0 {
			return nil
		}
		return repo.linkSkeletonEvents(ctx, record.ID, eventIDs, record.CreatedAt)
	})
}

// linkSkeletonEvents replaces one version's selection.
//
// The DELETE first is what makes the write a statement of the COMPLETE answer rather than an
// amendment: it is unreachable from the create path, where the version is brand new, but it is what
// a re-link would need and keeping it here means the two callers cannot disagree about whether a
// partial write leaves stale rows behind.
func (r *ScriptRepository) linkSkeletonEvents(ctx context.Context, versionID string, eventIDs []string, createdAt time.Time) error {
	conn := r.conn()
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
			return storageError("SCRIPT_WRITE_FAILED", "The selected events could not be saved.", err)
		}
	}
	return nil
}

// CreateAdaptationStrategyVersionWithLinks stores one strategy version and its treatments.
func (r *ScriptRepository) CreateAdaptationStrategyVersionWithLinks(ctx context.Context, record script.AdaptationStrategyVersion, links []script.StrategyEventLink) error {
	if r == nil || r.db == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	return r.withinTx(ctx, func(repo *ScriptRepository) error {
		if err := repo.CreateAdaptationStrategyVersion(ctx, record); err != nil {
			return err
		}
		if len(links) == 0 {
			return nil
		}
		return repo.linkStrategyEvents(ctx, record.ID, links, record.CreatedAt)
	})
}

// linkStrategyEvents replaces one version's treatments.
func (r *ScriptRepository) linkStrategyEvents(ctx context.Context, versionID string, links []script.StrategyEventLink, createdAt time.Time) error {
	conn := r.conn()
	if conn == nil {
		return storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	if _, err := conn.ExecContext(ctx,
		`DELETE FROM adaptation_strategy_event_links WHERE strategy_version_id = ?`, versionID); err != nil {
		return storageError("SCRIPT_WRITE_FAILED", "The event treatments could not be replaced.", err)
	}
	for position, link := range links {
		// The ordinal is the SLICE's position rather than the caller's value, because the slice IS
		// the order: a payload that stated both could state two different ones, and the order the
		// caller wrote is the one a reader would see. A caller's own Ordinal is therefore ignored
		// here, which is why the service sets it before calling rather than after: the two agree by
		// construction, and this is the one that decides.
		if _, err := conn.ExecContext(ctx, `INSERT INTO adaptation_strategy_event_links
			(strategy_version_id, story_event_id, treatment, ordinal, created_at)
			VALUES (?, ?, ?, ?, ?)`,
			versionID, link.StoryEventID, string(link.Treatment), position+1,
			formatTime(createdAt)); err != nil {
			return storageError("SCRIPT_WRITE_FAILED", "The event treatments could not be saved.", err)
		}
	}
	return nil
}

// MissingStoryEventIDs returns the event ids of a project that do not exist.
//
// One query rather than one per id, because a structure may cite twenty events and twenty round
// trips to answer one question is how a write path becomes slow enough that someone removes the
// check. The placeholders are built from the LENGTH of the list and the values travel as
// parameters, so the statement is still parameterised and the only interpolation is its arity.
//
// Deleted events count as missing, which is the fail-closed reading: `deleted_at` is how this schema
// soft-deletes, and a scene citing an event the user removed is citing something a reader cannot
// open. The status is deliberately NOT filtered — a `candidate` event is a real row that a script
// may dramatize before anyone accepts it, which is what the extraction stage produces.
func (r *ScriptRepository) MissingStoryEventIDs(ctx context.Context, projectID string, eventIDs []string) ([]string, error) {
	return r.missingStoryIDs(ctx, `SELECT id FROM story_events WHERE project_id = ?
		AND deleted_at = '' AND id IN (%s)`, projectID, eventIDs)
}

// MissingStoryEntityIDs returns the entity ids of a project that do not exist.
func (r *ScriptRepository) MissingStoryEntityIDs(ctx context.Context, projectID string, entityIDs []string) ([]string, error) {
	return r.missingStoryIDs(ctx, `SELECT id FROM story_entities WHERE project_id = ?
		AND deleted_at = '' AND id IN (%s)`, projectID, entityIDs)
}

// missingStoryIDs answers "which of these exist", returning the ones that do not.
func (r *ScriptRepository) missingStoryIDs(ctx context.Context, query, projectID string, ids []string) ([]string, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("SCRIPT_STORE_UNAVAILABLE", "The episode and script store is unavailable.", nil)
	}
	wanted := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		wanted = append(wanted, trimmed)
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(wanted)), ",")
	arguments := make([]any, 0, len(wanted)+1)
	arguments = append(arguments, projectID)
	for _, id := range wanted {
		arguments = append(arguments, id)
	}
	rows, err := conn.QueryContext(ctx, sprintfQuery(query, placeholders), arguments...)
	if err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The story references could not be checked.", err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, storageError("SCRIPT_READ_FAILED", "The story references could not be checked.", err)
		}
		found[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("SCRIPT_READ_FAILED", "The story references could not be checked.", err)
	}
	missing := make([]string, 0, len(wanted))
	for _, id := range wanted {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// sprintfQuery substitutes a statement's placeholder list.
//
// It exists so the substitution is one visible function rather than a `fmt.Sprintf` at each call
// site, and so a reader can see that the ONLY thing being substituted is a string of question marks
// whose length came from an argument count. No value ever travels through this path: they are all
// bound parameters.
func sprintfQuery(query, placeholders string) string {
	return strings.Replace(query, "%s", placeholders, 1)
}
