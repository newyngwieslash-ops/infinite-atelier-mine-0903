package database

import (
	"context"
	"testing"
	"time"

	appevents "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
)

// openWP05EventService opens a migrated database and returns the event service
// over its repository.
func openWP05EventService(t *testing.T) (*appevents.Service, *EventRepository) {
	t.Helper()
	return openWP05EventServiceWithClock(t, fixedClockProvider{})
}

// openWP05EventServiceWithClock is the fixture with a caller-supplied clock, so
// a test that needs distinct timestamps gets them without reaching into the
// service.
func openWP05EventServiceWithClock(t *testing.T, clock appevents.Clock) (*appevents.Service, *EventRepository) {
	t.Helper()
	ctx := context.Background()
	handle, err := open(ctx, t.TempDir()+"/app.db", t.TempDir()+"/snapshots", wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close(ctx) })
	db := handle.SQL()
	seedWP05Parents(t, db)
	repository := NewEventRepository(db)
	return appevents.NewService(appevents.Options{
		Repository: repository,
		Clock:      clock,
		IDs:        newTestIDGenerator(),
	}), repository
}

// TestRecordStoresAnEventInTheStream covers the write path and that the stored
// row round-trips every envelope field.
func TestRecordStoresAnEventInTheStream(t *testing.T) {
	service, repository := openWP05EventService(t)
	ctx := context.Background()

	recorded, err := service.Record(ctx, appevents.Draft{
		Type:          event.ProjectCreated,
		AggregateType: event.AggregateProject,
		AggregateID:   "project-1",
		ProjectID:     "project-1",
		Payload:       `{"name":"Drama"}`,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if recorded.EventID == "" {
		t.Fatal("the event has no identifier")
	}
	if recorded.SchemaVersion != event.SchemaVersion {
		t.Fatalf("schema version = %d", recorded.SchemaVersion)
	}

	stored, err := repository.ListEvents(ctx, ListFilter{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("%d events stored, want 1", len(stored))
	}
	got := stored[0]
	if got.EventID != recorded.EventID || got.EventType != event.ProjectCreated {
		t.Fatalf("stored event = %+v", got)
	}
	if got.AggregateType != event.AggregateProject || got.AggregateID != "project-1" {
		t.Fatalf("stored aggregate = %s/%s", got.AggregateType, got.AggregateID)
	}
	if got.Payload != `{"name":"Drama"}` {
		t.Fatalf("stored payload = %q", got.Payload)
	}
	if got.OccurredAt.IsZero() {
		t.Fatal("the stored event has no timestamp")
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("the stored event does not satisfy the domain: %v", err)
	}
}

// TestRecordRefusesADuplicateIdentifier covers the one write the stream must
// never accept: the same event twice would double-count for every consumer.
func TestRecordRefusesADuplicateIdentifier(t *testing.T) {
	_, repository := openWP05EventService(t)
	ctx := context.Background()
	record, err := event.New("event-1", event.ProjectCreated, event.AggregateProject,
		"project-1", "project-1", time.Now(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Append(ctx, record); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := repository.Append(ctx, record); err == nil {
		t.Fatal("the same event was appended twice")
	}
	// An event for a project that does not exist is refused by the foreign key.
	orphan, err := event.New("event-2", event.ProjectCreated, event.AggregateProject,
		"p", "no-such-project", time.Now(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Append(ctx, orphan); err == nil {
		t.Fatal("an event for a missing project was accepted")
	}
}

// TestListEventsFiltersAndOrders covers the three queries the stream serves,
// including that newest comes first.
func TestListEventsFiltersAndOrders(t *testing.T) {
	service, _ := openWP05EventService(t)
	ctx := context.Background()

	// The clock advances by a second per event so the ordering assertion has
	// something to order by. The shared fixture's fixed clock would give every
	// event the same timestamp and leave the id to break the tie, and the
	// identifier generator is random-backed (ADR-0005), so the ids are not in
	// insertion order. A test that asserted otherwise would be asserting a
	// property of the fixture rather than of the query.
	step := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	service, _ = openWP05EventServiceWithClock(t, steppingClock{current: &step})

	mustRecord := func(draft appevents.Draft) {
		t.Helper()
		if _, err := service.Record(ctx, draft); err != nil {
			t.Fatal(err)
		}
	}
	mustRecord(appevents.Draft{
		Type: event.ProjectCreated, AggregateType: event.AggregateProject,
		AggregateID: "project-1", ProjectID: "project-1",
	})
	mustRecord(appevents.Draft{
		Type: event.ScriptVersionCreated, AggregateType: event.AggregateScript,
		AggregateID: "sv-1", ProjectID: "project-1", TraceID: "trace-a",
	})
	mustRecord(appevents.Draft{
		Type: event.ScriptVersionApproved, AggregateType: event.AggregateScript,
		AggregateID: "sv-1", ProjectID: "project-1", TraceID: "trace-a",
	})

	all, err := service.List(ctx, appevents.ListFilter{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("%d events, want 3", len(all))
	}
	// Newest first, which the stepping clock makes unambiguous.
	if all[0].EventType != event.ScriptVersionApproved {
		t.Fatalf("the newest event is %s, want the approval", all[0].EventType)
	}
	if all[2].EventType != event.ProjectCreated {
		t.Fatalf("the oldest event is %s, want the project creation", all[2].EventType)
	}

	// One subject's history.
	subject, err := service.List(ctx, appevents.ListFilter{
		ProjectID: "project-1", AggregateType: string(event.AggregateScript), AggregateID: "sv-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(subject) != 2 {
		t.Fatalf("%d events for the subject, want 2", len(subject))
	}

	// One kind of event.
	approvals, err := service.List(ctx, appevents.ListFilter{
		ProjectID: "project-1", EventType: string(event.ScriptVersionApproved),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 {
		t.Fatalf("%d approvals, want 1", len(approvals))
	}

	// One action's events.
	traced, err := service.List(ctx, appevents.ListFilter{ProjectID: "project-1", TraceID: "trace-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(traced) != 2 {
		t.Fatalf("%d traced events, want 2", len(traced))
	}

	// Another project's stream is separate.
	other, err := service.List(ctx, appevents.ListFilter{ProjectID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("another project's stream returned %d events", len(other))
	}
}

// TestListClampsThePageSize covers the bound: a stream read is a window, not an
// export.
func TestListClampsThePageSize(t *testing.T) {
	service, _ := openWP05EventService(t)
	ctx := context.Background()
	for index := 0; index < 5; index++ {
		if _, err := service.Record(ctx, appevents.Draft{
			Type: event.ProjectCreated, AggregateType: event.AggregateProject,
			AggregateID: "project-1", ProjectID: "project-1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A limit above the ceiling is clamped rather than refused, and a limit of
	// zero takes the default.
	limited, err := service.List(ctx, appevents.ListFilter{ProjectID: "project-1", Limit: appevents.MaxPageSize + 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 5 {
		t.Fatalf("%d events with an over-large limit, want 5", len(limited))
	}
	defaulted, err := service.List(ctx, appevents.ListFilter{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaulted) != 5 {
		t.Fatalf("%d events with the default limit, want 5", len(defaulted))
	}
	// A query with no project is refused: the stream is project-scoped, and an
	// unscoped read would be a cross-project leak.
	if _, err := service.List(ctx, appevents.ListFilter{}); err == nil {
		t.Fatal("an unscoped event query was accepted")
	}
	if _, err := service.List(ctx, appevents.ListFilter{ProjectID: "project-1", EventType: "Nope"}); err == nil {
		t.Fatal("an unknown event type was accepted")
	}
}

// TestCountEvents covers the count query the stream's summary uses.
func TestCountEvents(t *testing.T) {
	service, _ := openWP05EventService(t)
	ctx := context.Background()
	if _, err := service.Record(ctx, appevents.Draft{
		Type: event.ProjectCreated, AggregateType: event.AggregateProject,
		AggregateID: "project-1", ProjectID: "project-1",
	}); err != nil {
		t.Fatal(err)
	}
	count, err := service.Count(ctx, appevents.ListFilter{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	count, err = service.Count(ctx, appevents.ListFilter{
		ProjectID: "project-1", EventType: string(event.ScriptVersionApproved),
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("count for an unrecorded type = %d, want 0", count)
	}
}

// TestEventServiceFailsClosedWithoutARepository proves the optional-port
// convention holds here too.
func TestEventServiceFailsClosedWithoutARepository(t *testing.T) {
	service := appevents.NewService(appevents.Options{Clock: fixedClockProvider{}})
	if service.Available() {
		t.Fatal("a service with no repository reports available")
	}
	ctx := context.Background()
	if _, err := service.Record(ctx, appevents.Draft{
		Type: event.ProjectCreated, AggregateType: event.AggregateProject,
		AggregateID: "p", ProjectID: "p",
	}); err == nil {
		t.Fatal("recording on an unattached service succeeded")
	}
	if _, err := service.List(ctx, appevents.ListFilter{ProjectID: "p"}); err == nil {
		t.Fatal("listing on an unattached service succeeded")
	}
	if _, err := service.Count(ctx, appevents.ListFilter{ProjectID: "p"}); err == nil {
		t.Fatal("counting on an unattached service succeeded")
	}
	// Build is the recorder path a command uses when it writes the event inside
	// its own transaction, so it must fail closed too.
	if _, err := service.Build(ctx, appevents.Draft{
		Type: event.ProjectCreated, AggregateType: event.AggregateProject,
		AggregateID: "p", ProjectID: "p",
	}); err == nil {
		t.Fatal("building on an unattached service succeeded")
	}
}

// steppingClock advances one second per read, so events recorded in sequence
// carry distinct timestamps.
type steppingClock struct {
	current *time.Time
}

func (c steppingClock) Now() time.Time {
	value := *c.current
	*c.current = value.Add(time.Second)
	return value
}
