package database

import (
	"context"
	"database/sql"

	events "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
)

// EventRepository is the SQLite implementation of the domain event stream.
//
// It owns every statement touching domain_events. The table has no foreign key
// to an event's subject (an event outlives what it describes) and no revision:
// an event is a fact about the past, so nothing updates one.
type EventRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewEventRepository builds the repository over a database handle.
func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
//
// A command that must record its event atomically with the change does this:
// the approval and the event land together or neither does.
func (r *EventRepository) WithinTx(tx *sql.Tx) *EventRepository {
	return &EventRepository{db: r.db, tx: tx}
}

func (r *EventRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

const domainEventSelectColumns = `SELECT id, event_type, schema_version, aggregate_type, aggregate_id,
	project_id, occurred_at, trace_id, payload_json FROM domain_events`

// Append stores one event.
//
// There is no update and no delete. A stream that could be rewritten would not
// be a record of what happened, and a consumer that had already read the old
// value would have no way to learn it changed.
func (r *EventRepository) Append(ctx context.Context, record event.Event) error {
	conn := r.conn()
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
			// A repeated event id means the same event was appended twice, which
			// would double-count it for every consumer. Refusing is the only
			// safe answer.
			return event.ConflictError("That event has already been recorded.")
		}
		if isForeignKeyViolation(err) {
			return event.InvalidError("That project does not exist.")
		}
		return storageError("EVENT_WRITE_FAILED", "The event could not be recorded.", err)
	}
	return nil
}

// ListFilter is the application layer's event query shape.
//
// It is an alias rather than a second struct with the same fields: the port
// declares the query, and a repository that declared its own would be a second
// definition to keep in step. A caller in this package writes ListFilter and
// means the application's.
type ListFilter = events.ListFilter

// ListEvents returns events newest first.
func (r *EventRepository) ListEvents(ctx context.Context, filter ListFilter) ([]event.Event, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("EVENT_STORE_UNAVAILABLE", "The event store is unavailable.", nil)
	}
	query := domainEventSelectColumns + ` WHERE project_id = ?`
	args := []any{filter.ProjectID}
	if filter.AggregateType != "" && filter.AggregateID != "" {
		query += ` AND aggregate_type = ? AND aggregate_id = ?`
		args = append(args, filter.AggregateType, filter.AggregateID)
	}
	if filter.EventType != "" {
		query += ` AND event_type = ?`
		args = append(args, filter.EventType)
	}
	if filter.TraceID != "" {
		query += ` AND trace_id = ?`
		args = append(args, filter.TraceID)
	}
	query += ` ORDER BY occurred_at DESC, id DESC`
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query += ` LIMIT ?`
	args = append(args, limit)

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageError("EVENT_READ_FAILED", "The events could not be read.", err)
	}
	defer rows.Close()
	var records []event.Event
	for rows.Next() {
		record, scanErr := scanDomainEvent(rows)
		if scanErr != nil {
			return nil, storageError("EVENT_READ_FAILED", "The events could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("EVENT_READ_FAILED", "The events could not be read.", err)
	}
	return records, nil
}

// CountEvents reports how many events a filter matches.
func (r *EventRepository) CountEvents(ctx context.Context, filter ListFilter) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("EVENT_STORE_UNAVAILABLE", "The event store is unavailable.", nil)
	}
	query := `SELECT COUNT(*) FROM domain_events WHERE project_id = ?`
	args := []any{filter.ProjectID}
	if filter.AggregateType != "" && filter.AggregateID != "" {
		query += ` AND aggregate_type = ? AND aggregate_id = ?`
		args = append(args, filter.AggregateType, filter.AggregateID)
	}
	if filter.EventType != "" {
		query += ` AND event_type = ?`
		args = append(args, filter.EventType)
	}
	var count int
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, storageError("EVENT_READ_FAILED", "The events could not be read.", err)
	}
	return count, nil
}

func scanDomainEvent(row rowScanner) (event.Event, error) {
	var record event.Event
	var eventType, aggregateType, occurredAt string
	err := row.Scan(&record.EventID, &eventType, &record.SchemaVersion, &aggregateType,
		&record.AggregateID, &record.ProjectID, &occurredAt, &record.TraceID, &record.Payload)
	if err != nil {
		return event.Event{}, err
	}
	record.EventType = event.Type(eventType)
	record.AggregateType = event.AggregateType(aggregateType)
	record.OccurredAt = parseTime(occurredAt)
	return record, nil
}
