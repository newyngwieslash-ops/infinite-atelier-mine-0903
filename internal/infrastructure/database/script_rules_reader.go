package database

import (
	"context"
	"database/sql"
	"strings"

	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
)

// script_rules_reader.go answers the SCRIPT ruleset's reads over the real schema.
//
// # Why these three reads live in the infrastructure layer
//
// Each is a JOIN rather than a domain operation: "the version's estimate against the sum of its
// scenes", "which lines are pinned", and "which events the strategy removed that a scene still
// dramatizes". None has a domain rule of its own — the rule is what the ruleset DOES with the answer
// — so putting them on the script service would grow its public surface to serve one caller, which is
// the same reasoning `StoryboardConsistencyChecker` records for its own ports.
//
// The reader is bound to one version, like the storyboard ruleset's `ScriptReader`, so a check
// cannot straddle two states of the database: every answer comes from the same version's rows.
type scriptRulesReader struct {
	db        *sql.DB
	versionID string
}

// ScriptRulesReaderFor builds a rules reader for one script version.
//
// It is the source `ScriptRulesSource` implements, and it answers a reader for ANY version the
// caller names — including one with no rows, which returns a reader whose queries find nothing rather
// than an error. A version that does not exist is not this layer's refusal to make: the ruleset
// returns no findings, which is the same answer every stage with nothing to say gives.
func (r *ScriptRepository) ScriptRulesReaderFor(_ context.Context, scriptVersionID string) appconsistency.ScriptRulesReader {
	if r == nil || r.db == nil {
		return nil
	}
	return &scriptRulesReader{db: r.db, versionID: strings.TrimSpace(scriptVersionID)}
}

// DurationOf returns the version's estimate and the sum of its scenes'.
//
// BOTH IN ONE CALL, because the rule is the comparison: two reads could straddle a write and report a
// discrepancy that exists in neither state.
func (r *scriptRulesReader) DurationOf(ctx context.Context, scriptVersionID string) (int, int, error) {
	versionSeconds := 0
	if err := r.db.QueryRowContext(ctx,
		`SELECT estimated_duration_seconds FROM script_versions WHERE id = ?`,
		scriptVersionID).Scan(&versionSeconds); err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	// COALESCE so a version with no scenes reads as zero rather than as NULL, which the rule treats
	// as "nothing to compare" rather than as a mismatch.
	scenesSeconds := 0
	if err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(estimated_duration_seconds), 0) FROM scenes WHERE script_version_id = ?`,
		scriptVersionID).Scan(&scenesSeconds); err != nil {
		return 0, 0, err
	}
	return versionSeconds, scenesSeconds, nil
}

// LockedLines returns the version's pinned dialogue lines.
//
// Ordered by scene and line ordinal so a report is reproducible, and scoped to the VERSION rather
// than to the script: a pin belongs to the lines it was taken on, and reading across versions would
// report a pin on a line this version does not have.
func (r *scriptRulesReader) LockedLines(ctx context.Context, scriptVersionID string) ([]appconsistency.LockedLine, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT l.id, s.id, l.ordinal
		FROM dialogue_lines l
		JOIN scenes s ON s.id = l.scene_id
		WHERE s.script_version_id = ? AND l.locked = 1
		ORDER BY s.ordinal ASC, l.ordinal ASC, l.id ASC`, scriptVersionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lines []appconsistency.LockedLine
	for rows.Next() {
		var line appconsistency.LockedLine
		if err := rows.Scan(&line.ID, &line.SceneID, &line.Ordinal); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// RemovedEventsWithScenes returns the events the strategy removed that a scene still dramatizes.
//
// # The join, stated
//
// A scene cites the story event it adapts (`scenes.source_story_event_id`). The version's strategy
// records what happened to each event it considered (`adaptation_strategy_event_links.treatment`,
// whose value list includes `removed`). The contradiction is a scene whose cited event's treatment is
// `removed` — the script dramatizes something the plan decided to cut.
//
// # Why the version's own strategy
//
// A script version cites the strategy it was written from (`adaptation_strategy_version_id`), so the
// join goes through IT rather than through any strategy of the episode: a later strategy that removes
// an event does not retroactively make an earlier script wrong, and a rule that used the newest
// strategy would report every historical version as contradicting it.
//
// A scene with no cited event is skipped by the join's own condition — an original adaptation (the
// strategy's `isOriginalAdaptation` scenes) has nothing to contradict.
func (r *scriptRulesReader) RemovedEventsWithScenes(ctx context.Context, scriptVersionID string) ([]appconsistency.RemovedEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.id, COALESCE(e.name, ''), s.id
		FROM scenes s
		JOIN script_versions v ON v.id = s.script_version_id
		JOIN adaptation_strategy_event_links k
			ON k.adaptation_strategy_version_id = v.adaptation_strategy_version_id
		JOIN story_events e ON e.id = k.story_event_id
		WHERE s.script_version_id = ?
		  AND s.source_story_event_id <> ''
		  AND s.source_story_event_id = k.story_event_id
		  AND k.treatment = 'removed'
		ORDER BY s.ordinal ASC, s.id ASC`, scriptVersionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var removed []appconsistency.RemovedEvent
	for rows.Next() {
		var event appconsistency.RemovedEvent
		if err := rows.Scan(&event.EventID, &event.EventName, &event.SceneID); err != nil {
			return nil, err
		}
		removed = append(removed, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return removed, nil
}
