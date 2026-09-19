package events

import (
	"context"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
)

// RecordBestEffort writes an event and reports nothing to the caller.
//
// It is for a command whose event is a notification rather than a governance
// record: the row is already committed, and the caller must not be told the
// command failed because a log line could not be written. ADR-0009 draws the
// line and names which commands fall on each side — an approval records its
// event inside its own transaction and refuses without one, while a creation or
// a mark announces itself and carries on.
//
// The error is deliberately dropped rather than returned: a caller that could
// act on it would be a caller whose command should have used the transactional
// path instead. A Service with no repository therefore does nothing here rather
// than panicking, which is what lets a service be composed without events at all.
func (s *Service) RecordBestEffort(ctx context.Context, draft Draft) {
	if s == nil || !s.Available() {
		return
	}
	if _, err := s.Record(ctx, draft); err != nil {
		// Swallowed on purpose: see the doc comment. The alternative would be
		// to fail a command whose write already succeeded.
		return
	}
}

// MustBuild is the recorder path for a command that needs the event to exist
// before its own write, and therefore needs the error.
//
// It exists so a command does not have to nil-check the recorder itself: a
// Service composed without one returns an event and a nil error, which tells the
// command there is nothing to record rather than that recording failed.
func (s *Service) MustBuild(ctx context.Context, draft Draft) (event.Event, bool, error) {
	if s == nil || !s.Available() {
		return event.Event{}, false, nil
	}
	record, err := s.Build(ctx, draft)
	if err != nil {
		return event.Event{}, false, err
	}
	return record, true, nil
}
