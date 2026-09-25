package database

import (
	"context"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// storyStatus converts a plain string to the domain's status type, so the loop in
// `TestTheProjectParticipantReadAgreesWithTheEventList` can walk the empty case and the named ones
// without three copies of one call.
func storyStatus(value string) story.FactStatus { return story.FactStatus(value) }

// story_participants_wp17_test.go covers the project-wide participation read that the graph needs.
//
// # Why a project-wide read exists at all
//
// `ListStoryEventParticipants` answers about ONE event, which is what the participants panel wants:
// a user clicks an event and sees who is in it. The graph needs the opposite shape — every edge at
// once — and calling the per-event method in a loop would make one round trip per event, which is
// the N+1 the graph view would hit on its first render.
//
// # What this test is for
//
// The read JOINs through `story_events` to reach the project, and two filters have to agree with the
// events list the same view reads:
//
//  1. the STATUS filter applies to the EVENT, not to the participation (which has no status of its
//     own), and
//  2. the soft delete is checked on the EVENT.
//
// A view that filtered events one way and participations another would draw edges to nodes it did
// not show — and the drawing code DROPS those silently, so the bug would look like "the graph is
// missing an edge" rather than like a filter mismatch. Asserting the two agree is the point.

// seedParticipantEvent writes one event with an explicit status.
func seedParticipantEvent(t *testing.T, db querier, ctx context.Context, id, name, status string, ordinal int) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO story_events
		(id, project_id, ordinal, name, status, source_scope, created_at, updated_at, revision)
		VALUES (?, 'drama-project', ?, ?, ?, 'original', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		id, ordinal, name, status); err != nil {
		t.Fatalf("seeding the event %s: %v", id, err)
	}
}

// seedParticipant links one entity to one event in one role.
func seedParticipant(t *testing.T, db querier, ctx context.Context, eventID, entityID, role string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO story_event_participants
		(story_event_id, story_entity_id, role, state_before, state_after, created_at)
		VALUES (?, ?, ?, '', '', '2026-01-01T00:00:00Z')`,
		eventID, entityID, role); err != nil {
		t.Fatalf("seeding the participant %s/%s: %v", eventID, entityID, err)
	}
}

func TestTheProjectParticipantReadFiltersOnTheEvent(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	repo := NewStoryRepository(db)

	seedGraphEntity(t, db, ctx, "part-mira", "Mira", "accepted")
	seedGraphEntity(t, db, ctx, "part-oath", "The Oath", "accepted")
	// One accepted event, one candidate, one soft-deleted. All three carry a participant, so a read
	// that ignored the event's state would return three where the events list shows one.
	seedParticipantEvent(t, db, ctx, "part-event-accepted", "The Crossing", "accepted", 1)
	seedParticipantEvent(t, db, ctx, "part-event-candidate", "The Rumour", "candidate", 2)
	seedParticipantEvent(t, db, ctx, "part-event-deleted", "The Cut", "accepted", 3)
	if _, err := db.ExecContext(ctx,
		`UPDATE story_events SET deleted_at = '2026-02-01T00:00:00Z' WHERE id = 'part-event-deleted'`); err != nil {
		t.Fatal(err)
	}
	seedParticipant(t, db, ctx, "part-event-accepted", "part-mira", "actor")
	seedParticipant(t, db, ctx, "part-event-accepted", "part-oath", "target")
	seedParticipant(t, db, ctx, "part-event-candidate", "part-mira", "witness")
	seedParticipant(t, db, ctx, "part-event-deleted", "part-mira", "actor")

	// An empty status means any, which still excludes the soft-deleted event: three participations
	// across two live events.
	all, err := repo.ListProjectEventParticipants(ctx, "drama-project", "")
	if err != nil {
		t.Fatalf("ListProjectEventParticipants: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("the unfiltered read returned %d participations, want 3: %+v", len(all), all)
	}
	for _, record := range all {
		if record.StoryEventID == "part-event-deleted" {
			t.Fatal("a participation of a soft-deleted event was returned")
		}
	}
	// The accepted filter narrows to the accepted event's two, which is the agreement with the
	// events list that keeps the graph from drawing an edge to a node it does not show.
	accepted, err := repo.ListProjectEventParticipants(ctx, "drama-project", "accepted")
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted) != 2 {
		t.Fatalf("the accepted filter returned %d participations, want 2: %+v", len(accepted), accepted)
	}
	for _, record := range accepted {
		if record.StoryEventID != "part-event-accepted" {
			t.Fatalf("the accepted filter returned a participation of %q", record.StoryEventID)
		}
	}
	// An unknown project is an empty list rather than an error, the same discipline every other read
	// in this package keeps.
	none, err := repo.ListProjectEventParticipants(ctx, "no-such-project", "")
	if err != nil {
		t.Fatalf("an unknown project must not be an error: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("an unknown project returned %d participations", len(none))
	}
}

// TestTheProjectParticipantReadAgreesWithTheEventList is the agreement stated directly.
//
// The two reads are made separately by the same view, and the property it depends on is that the
// SET OF EVENTS they cover is identical. Asserting it as a set equality rather than by counting is
// what makes a future filter change fail here: a status added to one read and not the other shows
// up as a difference in the event ids, which is the actual fault rather than a symptom of it.
func TestTheProjectParticipantReadAgreesWithTheEventList(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	repo := NewStoryRepository(db)

	seedGraphEntity(t, db, ctx, "agree-mira", "Mira", "accepted")
	seedParticipantEvent(t, db, ctx, "agree-a", "One", "accepted", 1)
	seedParticipantEvent(t, db, ctx, "agree-b", "Two", "candidate", 2)
	seedParticipantEvent(t, db, ctx, "agree-c", "Three", "accepted", 3)
	seedParticipant(t, db, ctx, "agree-a", "agree-mira", "actor")
	seedParticipant(t, db, ctx, "agree-b", "agree-mira", "actor")
	seedParticipant(t, db, ctx, "agree-c", "agree-mira", "actor")

	for _, status := range []string{"", "accepted", "candidate"} {
		events, err := repo.ListStoryEvents(ctx, "drama-project", "", storyStatus(status))
		if err != nil {
			t.Fatal(err)
		}
		participations, err := repo.ListProjectEventParticipants(ctx, "drama-project", storyStatus(status))
		if err != nil {
			t.Fatal(err)
		}
		// Every event the list shows has exactly one participation here, and every participation
		// belongs to an event the list shows.
		shown := map[string]bool{}
		for _, event := range events {
			shown[event.ID] = true
		}
		if len(participations) != len(shown) {
			t.Fatalf("for status %q the event list shows %d events and the participant read returns %d rows",
				status, len(shown), len(participations))
		}
		for _, record := range participations {
			if !shown[record.StoryEventID] {
				t.Fatalf("for status %q a participation names the event %q, which the list does not show",
					status, record.StoryEventID)
			}
		}
	}
}

// TestTheProjectParticipantReadOrdersByEventThenRole keeps the drawing deterministic.
//
// The graph lays nodes out in the order it receives them and draws edges in the order it receives
// those, so a read whose order changed between two calls would render a different picture for the
// same data — the same discipline the other list reads in this package state in their comments.
func TestTheProjectParticipantReadOrdersByEventThenRole(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	repo := NewStoryRepository(db)

	seedGraphEntity(t, db, ctx, "order-mira", "Mira", "accepted")
	seedGraphEntity(t, db, ctx, "order-oath", "The Oath", "accepted")
	// Seeded in a deliberately wrong order: the second event's participant first, and within the
	// first event the alphabetically later role first.
	seedParticipantEvent(t, db, ctx, "order-b", "Second", "accepted", 2)
	seedParticipantEvent(t, db, ctx, "order-a", "First", "accepted", 1)
	seedParticipant(t, db, ctx, "order-b", "order-mira", "actor")
	seedParticipant(t, db, ctx, "order-a", "order-oath", "target")
	seedParticipant(t, db, ctx, "order-a", "order-mira", "actor")

	records, err := repo.ListProjectEventParticipants(ctx, "drama-project", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("the read returned %d rows: %+v", len(records), records)
	}
	// The first event comes first because its ORDINAL is 1, not because its id sorts earlier.
	if records[0].StoryEventID != "order-a" || records[1].StoryEventID != "order-a" {
		t.Fatalf("the rows are not grouped by event ordinal: %+v", records)
	}
	// And within the event, actor before target.
	if records[0].Role != "actor" || records[1].Role != "target" {
		t.Fatalf("the roles are not in order within the event: %+v", records)
	}
	if records[2].StoryEventID != "order-b" {
		t.Fatalf("the second event's row is not last: %+v", records)
	}
}
