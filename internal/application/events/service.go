// Package events is the application layer for the domain event stream of
// docs/DOMAIN_MODEL.md §17.
//
// It owns two things: recording an event (minting the identifier, stamping the
// time, validating the envelope) and reading the stream back. The commands that
// emit events live in their own packages and depend on the narrow Recorder
// interface declared here, so a command needs to know nothing about how events
// are stored.
package events

import (
	"context"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
)

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces event identifiers (ADR-0005).
type IDGenerator interface {
	New() (string, error)
}

// Repository persists and reads the event stream.
type Repository interface {
	// Append stores one event. A repeated identifier is a conflict, because the
	// same event recorded twice would double-count for every consumer.
	Append(ctx context.Context, record event.Event) error
	// ListEvents returns a project's events newest first.
	ListEvents(ctx context.Context, filter ListFilter) ([]event.Event, error)
	// CountEvents reports how many events a filter matches.
	CountEvents(ctx context.Context, filter ListFilter) (int, error)
}

// ListFilter is a caller's event query.
type ListFilter struct {
	ProjectID string
	// AggregateType and AggregateID narrow to one subject's history. They are
	// applied together or not at all: an aggregate type with no id would list
	// every script in the project, which is not what a caller asking about one
	// thing means.
	AggregateType string
	AggregateID   string
	// EventType narrows to one kind of event.
	EventType string
	// TraceID narrows to the events of one action.
	TraceID string
	// Limit caps the result and is clamped by the service.
	Limit int
}

// Page size bounds. A stream read is a window, not a full export: a full export
// is a backup, and the backup service owns that.
const (
	// DefaultPageSize is what a caller gets when it states no limit.
	DefaultPageSize = 100
	// MaxPageSize is the largest window a caller may ask for.
	MaxPageSize = 500
)

// Service records and reads domain events.
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
}

// Options configures a Service.
type Options struct {
	Repository Repository
	Clock      Clock
	IDs        IDGenerator
}

// NewService builds the event service.
func NewService(options Options) *Service {
	return &Service{repository: options.Repository, clock: options.Clock, ids: options.IDs}
}

// Available reports whether the service can operate.
func (s *Service) Available() bool {
	return s != nil && s.repository != nil && s.ids != nil
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// storageFailure is the fail-closed error for an unattached service.
func storageFailure() error {
	return event.StorageError("The event store is unavailable.", nil)
}

// Draft is an event a command wants recorded, before the envelope is complete.
//
// A command knows what happened and to what; the identifier, the timestamp and
// the envelope version are supplied here so no caller has to remember them.
type Draft struct {
	Type          event.Type
	AggregateType event.AggregateType
	AggregateID   string
	ProjectID     string
	// TraceID correlates the events of one action. Empty is allowed: a manual
	// command has no run to correlate with, and demanding one would force a
	// fabricated value.
	TraceID string
	// Payload is a JSON object summarising the change, or empty.
	Payload string
}

// Record stores one event.
//
// It is the path for a command whose own write is already committed: the event
// describes what happened and does not gate it. A command that must record its
// event atomically with the change passes the built event to its repository
// instead, which is what the approval commands do — §16 asks for both
// "transaction" and "event", and the difference is whether the event is a
// governance record the change depends on or a notification about it.
func (s *Service) Record(ctx context.Context, draft Draft) (event.Event, error) {
	if !s.Available() {
		return event.Event{}, storageFailure()
	}
	record, err := s.Build(draft)
	if err != nil {
		return event.Event{}, err
	}
	if err := s.repository.Append(ctx, record); err != nil {
		return event.Event{}, err
	}
	return record, nil
}

// Build assembles and validates an event without storing it.
//
// A command that records its event inside its own transaction builds it here,
// so the envelope rules are applied once rather than per repository.
func (s *Service) Build(draft Draft) (event.Event, error) {
	if !s.Available() {
		return event.Event{}, storageFailure()
	}
	id, err := s.ids.New()
	if err != nil {
		return event.Event{}, storageFailure()
	}
	record, err := event.New(id, draft.Type, draft.AggregateType, draft.AggregateID,
		draft.ProjectID, s.now(), draft.TraceID, draft.Payload)
	if err != nil {
		return event.Event{}, err
	}
	return record, nil
}

// List returns a project's events newest first.
func (s *Service) List(ctx context.Context, filter ListFilter) ([]event.Event, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if trimmed := trimSpace(filter.ProjectID); trimmed == "" {
		return nil, event.InvalidError("An event query must name a project.")
	}
	if filter.EventType != "" && !event.IsValidType(event.Type(filter.EventType)) {
		return nil, event.InvalidError("That event type is not recognised.")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	return s.repository.ListEvents(ctx, ListFilter{
		ProjectID:     trimSpace(filter.ProjectID),
		AggregateType: trimSpace(filter.AggregateType),
		AggregateID:   trimSpace(filter.AggregateID),
		EventType:     trimSpace(filter.EventType),
		TraceID:       trimSpace(filter.TraceID),
		Limit:         limit,
	})
}

// Count reports how many events match.
func (s *Service) Count(ctx context.Context, filter ListFilter) (int, error) {
	if !s.Available() {
		return 0, storageFailure()
	}
	if trimmed := trimSpace(filter.ProjectID); trimmed == "" {
		return 0, event.InvalidError("An event query must name a project.")
	}
	return s.repository.CountEvents(ctx, ListFilter{
		ProjectID:     trimSpace(filter.ProjectID),
		AggregateType: trimSpace(filter.AggregateType),
		AggregateID:   trimSpace(filter.AggregateID),
		EventType:     trimSpace(filter.EventType),
	})
}

// Recorder is the narrow surface a command depends on when it emits an event.
//
// It is declared here and satisfied by Service, so a command package imports
// this package's interface rather than its implementation and cannot reach the
// stream's read side by accident.
type Recorder interface {
	// Build assembles a validated event without storing it, for a command that
	// records it inside its own transaction.
	Build(ctx context.Context, draft Draft) (event.Event, error)
}

// trimSpace trims ASCII whitespace, spelled out so this package's dependency on
// the standard library stays at what it actually needs.
func trimSpace(value string) string {
	start := 0
	for start < len(value) && isSpace(value[start]) {
		start++
	}
	end := len(value)
	for end > start && isSpace(value[end-1]) {
		end--
	}
	return value[start:end]
}

func isSpace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}
