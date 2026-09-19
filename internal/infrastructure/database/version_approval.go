package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// This file implements the one approval switch that DOMAIN_MODEL §2.5 defines
// for every version family, rather than eight near-copies of it. There is
// exactly ONE implementation below (approveVersionWithEvent) and every family
// calls it: an earlier revision of this file carried a second, event-less copy
// that nothing called, which is precisely the drift this arrangement exists to
// prevent, and it was removed.
//
// Eight families are versioned and each needs the same three steps: read the
// version, run versioning.CanApprove, and in ONE transaction supersede the
// version currently approved before approving the new one. Only the table name
// and the parent column differ, so the shape lives here once. Eight copies would
// be eight places for the order of the two writes to drift, and the order is
// load-bearing: the schema's partial unique index refuses a second approved row,
// so approving before superseding fails.
//
// A note on the table names below. They are interpolated into the statement
// rather than bound as parameters, because SQL cannot parameterise an
// identifier. They are not user input: the only way to reach this function is
// through a package-level call with a literal from approvalFamily, which is a
// closed set. The values being written are still bound parameters, and the
// caller-supplied status is passed as one.

// approvalFamily names a version table and the column that groups its versions.
//
// The parent column is what the schema's partial unique index is built on
// ("CREATE UNIQUE INDEX ... ON <table>(<parent>) WHERE status = 'approved'"), so
// the pair here must match that index or the switch would miss the row it is
// meant to replace.
type approvalFamily struct {
	// table is the version table.
	table string
	// parentColumn groups the versions of one parent entity.
	parentColumn string
}

// The eight families §2.5 defines, each matching its migration's index.
var (
	familyStorySkeletonVersions = approvalFamily{table: "story_skeleton_versions", parentColumn: "episode_id"}
	familyAdaptationStrategies  = approvalFamily{table: "adaptation_strategy_versions", parentColumn: "episode_id"}
	familyScriptVersions        = approvalFamily{table: "script_versions", parentColumn: "script_id"}
	familyAssetVersions         = approvalFamily{table: "asset_versions", parentColumn: "asset_id"}
	familyStyleGuides           = approvalFamily{table: "project_style_guides", parentColumn: "project_id"}
	familyDirectorPlans         = approvalFamily{table: "director_plan_versions", parentColumn: "episode_id"}
	familyStoryboardVersions    = approvalFamily{table: "storyboard_versions", parentColumn: "storyboard_id"}
	familyStoryboardPanels      = approvalFamily{table: "storyboard_panel_versions", parentColumn: "storyboard_item_id"}
)

// CurrentApprovedVersionID returns the id of the approved version of a parent,
// or "" when none is approved.
//
// It is the read the approval switch needs to know what it is replacing, and the
// read a caller needs to answer "what is in force" without listing every version.
func currentApprovedVersionID(ctx context.Context, db *sql.DB, family approvalFamily, parentID string) (string, error) {
	if db == nil {
		return "", storageError("VERSION_STORE_UNAVAILABLE", "The version store is unavailable.", nil)
	}
	query := fmt.Sprintf(`SELECT id FROM %s WHERE %s = ? AND status = 'approved' LIMIT 1`, family.table, family.parentColumn)
	var id string
	err := db.QueryRowContext(ctx, query, parentID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", storageError("VERSION_READ_FAILED", "The approved version could not be read.", err)
	}
	return id, nil
}

// approveVersionWithEvent performs the approval switch and records the event in
// the same transaction.
//
// §16 asks every command for both a transaction and an event, and for an
// approval the two belong together: an approval that landed without its record,
// or a record of an approval that rolled back, are both worse than a clean
// failure. `record.EventID` is minted by the application layer (ADR-0005), so a
// retry produces a new event rather than reusing one.
func approveVersionWithEvent(ctx context.Context, db *sql.DB, family approvalFamily, versionID, parentID string, expectedStatus versioning.Status, record event.Event) error {
	if db == nil {
		return storageError("VERSION_STORE_UNAVAILABLE", "The version store is unavailable.", nil)
	}
	if family.table == "" || family.parentColumn == "" {
		return storageError("VERSION_STORE_UNAVAILABLE", "The version store is unavailable.", fmt.Errorf("unknown approval family"))
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("VERSION_TX_FAILED", "The approval could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	conn := connection(db, tx)

	supersede := fmt.Sprintf(`UPDATE %s SET status = 'superseded'
		WHERE %s = ? AND status = 'approved' AND id <> ?`, family.table, family.parentColumn)
	if _, err := conn.ExecContext(ctx, supersede, parentID, versionID); err != nil {
		if isUniqueViolation(err) {
			return versioning.ConflictError("Another version of this parent was approved at the same time. Reload and try again.")
		}
		return storageError("VERSION_WRITE_FAILED", "The previous approval could not be superseded.", err)
	}

	approve := fmt.Sprintf(`UPDATE %s SET status = 'approved'
		WHERE id = ? AND status = ?`, family.table)
	result, err := conn.ExecContext(ctx, approve, versionID, string(expectedStatus))
	if err != nil {
		if isUniqueViolation(err) {
			return versioning.ConflictError("This parent already has an approved version. Reload and try again.")
		}
		return storageError("VERSION_WRITE_FAILED", "The version could not be approved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("VERSION_WRITE_FAILED", "The version could not be approved.", err)
	}
	if affected == 0 {
		return versioning.ConflictError("This version changed in another window. Reload it and try again.")
	}

	if _, err := conn.ExecContext(ctx, `INSERT INTO domain_events
		(id, event_type, schema_version, aggregate_type, aggregate_id, project_id,
		 occurred_at, trace_id, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.EventID, string(record.EventType), record.SchemaVersion,
		string(record.AggregateType), record.AggregateID, record.ProjectID,
		formatTime(record.OccurredAt), record.TraceID, record.Payload,
		formatTime(record.OccurredAt)); err != nil {
		if isUniqueViolation(err) {
			return event.ConflictError("That event has already been recorded.")
		}
		if isForeignKeyViolation(err) {
			return event.InvalidError("That project does not exist.")
		}
		return storageError("EVENT_WRITE_FAILED", "The event could not be recorded.", err)
	}

	if err := tx.Commit(); err != nil {
		return storageError("VERSION_TX_FAILED", "The approval could not be committed.", err)
	}
	committed = true
	return nil
}

// AppendEvent records a domain event on its own.
//
// It exists so a repository that must write an event WITHOUT a change of its own
// (a workflow stage transition already does this inside its own transaction) has
// one place to do it, rather than each repository growing its own INSERT.
func AppendEvent(ctx context.Context, conn querier, record event.Event) error {
	if conn == nil {
		return storageError("EVENT_STORE_UNAVAILABLE", "The event store is unavailable.", nil)
	}
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
