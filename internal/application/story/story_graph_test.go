package story

import (
	"context"
	"strings"
	"testing"
	"time"

	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// These tests cover the story-graph commands and queries WP-06 added: the
// filtered lists the panel drives, the alias command, participation, and the
// evidence a fact cites. ADR-0007 recorded that these write paths were missing;
// this is where they are exercised.

// seedGraphProject writes one accepted entity and one candidate, so the list
// filters have something to separate.
func seedGraphProject(t *testing.T, service *Service, store *memoryStore) (accepted, candidate storydomain.StoryEntity) {
	t.Helper()
	ctx := context.Background()
	accepted, err := service.CreateStoryEntity(ctx, CreateStoryEntityRequest{
		ProjectID: "project-1", Type: storydomain.EntityCharacter, CanonicalName: "Mira",
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err = service.AcceptStoryEntity(ctx, FactDecisionRequest{ID: accepted.ID, Revision: accepted.Revision})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = service.CreateStoryEntity(ctx, CreateStoryEntityRequest{
		ProjectID: "project-1", Type: storydomain.EntityRelationship, CanonicalName: "The Oath",
	})
	if err != nil {
		t.Fatal(err)
	}
	return accepted, candidate
}

// TestListStoryEntitiesFiltersByStatus covers the two questions the panel asks:
// everything the project has, and the candidates awaiting review.
func TestListStoryEntitiesFiltersByStatus(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	accepted, candidate := seedGraphProject(t, service, store)
	ctx := context.Background()

	all, err := service.ListStoryEntities(ctx, "project-1", "")
	if err != nil {
		t.Fatalf("ListStoryEntities: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("an unfiltered list returned %d entities, want both", len(all))
	}
	onlyCandidates, err := service.ListStoryEntities(ctx, "project-1", storydomain.FactCandidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyCandidates) != 1 || onlyCandidates[0].ID != candidate.ID {
		t.Fatalf("the candidate filter returned %+v", onlyCandidates)
	}
	onlyAccepted, err := service.ListStoryEntities(ctx, "project-1", storydomain.FactAccepted)
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyAccepted) != 1 || onlyAccepted[0].ID != accepted.ID {
		t.Fatalf("the accepted filter returned %+v", onlyAccepted)
	}
	// A project with nothing in it returns an empty list rather than an error,
	// which is what the panel renders as its empty state.
	empty, err := service.ListStoryEntities(ctx, "project-2", "")
	if err != nil {
		t.Fatalf("an empty project must not be an error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("a different project returned %d entities", len(empty))
	}
	// A soft-deleted entity never appears, because a deleted fact in the graph is
	// the one thing a user cannot work around.
	deleted := candidate
	deleted.DeletedAt = time.Now().UTC()
	store.entities[candidate.ID] = deleted
	afterDelete, err := service.ListStoryEntities(ctx, "project-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(afterDelete) != 1 {
		t.Fatalf("a soft-deleted entity still appears: %d rows", len(afterDelete))
	}
}

// TestListStoryEntitiesRejectsAnUnknownStatus keeps the filter to the vocabulary
// the schema's CHECK accepts, so a typo in a binding surfaces as a refusal
// rather than an empty list the user reads as "there is nothing".
func TestListStoryEntitiesRejectsAnUnknownStatus(t *testing.T) {
	service := newTestService(newMemoryStore())
	ctx := context.Background()
	for _, status := range []storydomain.FactStatus{"pending", "Approved", "deleted"} {
		if _, err := service.ListStoryEntities(ctx, "project-1", status); err == nil {
			t.Fatalf("the status %q was accepted as a filter", status)
		}
	}
	if _, err := service.ListStoryEntities(ctx, "   ", ""); err == nil {
		t.Fatal("an empty project id was accepted")
	}
}

// TestListStoryEventsOrdersByStoryTime covers the property the timeline depends
// on: the list follows story order, not insertion order.
func TestListStoryEventsOrdersByStoryTime(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	// Written out of order on purpose. The second event is a flashback: narrated
	// after the first and happening before it.
	for _, request := range []CreateStoryEventRequest{
		{ProjectID: "project-1", ChapterID: "chapter-1", Ordinal: 5, Name: "The siege"},
		{ProjectID: "project-1", ChapterID: "chapter-1", Ordinal: 1, Name: "The oath"},
		{ProjectID: "project-1", ChapterID: "chapter-2", Ordinal: 3, Name: "The letter"},
	} {
		if _, err := service.CreateStoryEvent(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	all, err := service.ListStoryEvents(ctx, "project-1", "", "")
	if err != nil {
		t.Fatalf("ListStoryEvents: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("read %d events, want 3", len(all))
	}
	want := []string{"The oath", "The letter", "The siege"}
	for index, name := range want {
		if all[index].Name != name {
			t.Fatalf("position %d is %q, want %q — the list is not in story order", index, all[index].Name, name)
		}
	}
	// The chapter filter narrows to one chapter, which is what the chapter panel
	// asks for.
	oneChapter, err := service.ListStoryEvents(ctx, "project-1", "chapter-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(oneChapter) != 2 {
		t.Fatalf("the chapter filter returned %d events, want 2", len(oneChapter))
	}
}

// TestAddStoryEntityAlias covers the alias command, including the three refusals
// that keep the name list unambiguous.
func TestAddStoryEntityAlias(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	entity, _ := seedGraphProject(t, service, store)
	ctx := context.Background()

	// A plain alias with no source span is valid: a name a user typed has no
	// chapter.
	alias, err := service.AddStoryEntityAlias(ctx, AddStoryEntityAliasRequest{
		StoryEntityID: entity.ID, Alias: "  小艾  ",
	})
	if err != nil {
		t.Fatalf("AddStoryEntityAlias: %v", err)
	}
	if alias.Alias != "小艾" {
		t.Fatalf("the alias was not trimmed: %q", alias.Alias)
	}
	if alias.SourceStart != nil || alias.SourceEnd != nil {
		t.Fatalf("an alias with no source span gained one: %+v", alias)
	}

	// An alias found in the text carries its span.
	start, end := 120, 122
	spanned, err := service.AddStoryEntityAlias(ctx, AddStoryEntityAliasRequest{
		StoryEntityID: entity.ID, Alias: "阿艾", SourceChapterID: "chapter-1",
		SourceStart: &start, SourceEnd: &end,
	})
	if err != nil {
		t.Fatalf("an alias with a source span was refused: %v", err)
	}
	if spanned.SourceStart == nil || *spanned.SourceStart != 120 {
		t.Fatalf("the source span was not stored: %+v", spanned)
	}

	// The same spelling twice is a conflict, in either case, because the schema's
	// unique index is over the pair and two rows would make a merge ambiguous.
	for _, duplicate := range []string{"小艾", "  小艾 ", "阿艾"} {
		if _, err := service.AddStoryEntityAlias(ctx, AddStoryEntityAliasRequest{
			StoryEntityID: entity.ID, Alias: duplicate,
		}); err == nil {
			t.Fatalf("the duplicate alias %q was accepted", duplicate)
		}
	}
	// The canonical name is not an alias: the two lists must not disagree about
	// what the entity is called.
	if _, err := service.AddStoryEntityAlias(ctx, AddStoryEntityAliasRequest{
		StoryEntityID: entity.ID, Alias: "mira",
	}); err == nil {
		t.Fatal("the canonical name was accepted as an alias")
	}
	if _, err := service.AddStoryEntityAlias(ctx, AddStoryEntityAliasRequest{
		StoryEntityID: entity.ID, Alias: "   ",
	}); err == nil {
		t.Fatal("a blank alias was accepted")
	}
	if _, err := service.AddStoryEntityAlias(ctx, AddStoryEntityAliasRequest{
		StoryEntityID: "no-such-entity", Alias: "x",
	}); err == nil {
		t.Fatal("an alias for a missing entity was accepted")
	}

	// The stored list holds both, so the panel can render every name.
	stored, err := service.ListStoryEntityAliases(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("the alias list holds %d names, want 2", len(stored))
	}
}

// TestAliasIsRefusedOnALockedEntity covers the lock's meaning: a locked fact's
// names cannot be rewritten, because how a fact is found is part of the fact.
func TestAliasIsRefusedOnALockedEntity(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	entity, _ := seedGraphProject(t, service, store)
	ctx := context.Background()
	store.setEntityStatus(entity.ID, storydomain.FactLocked)
	if _, err := service.AddStoryEntityAlias(ctx, AddStoryEntityAliasRequest{
		StoryEntityID: entity.ID, Alias: "小艾",
	}); err == nil {
		t.Fatal("an alias was added to a locked entity")
	}
}

// TestAddStoryEventParticipant covers participation, including the invariant the
// primary key encodes: one entity may hold several roles in one event, but not
// the same role twice.
func TestAddStoryEventParticipant(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	entity, _ := seedGraphProject(t, service, store)
	event, err := service.CreateStoryEvent(ctx, CreateStoryEventRequest{
		ProjectID: "project-1", ChapterID: "chapter-1", Name: "Mira finds the letter",
	})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := service.AddStoryEventParticipant(ctx, AddStoryEventParticipantRequest{
		StoryEventID: event.ID, StoryEntityID: entity.ID,
		Role: storydomain.ParticipantActor, StateBefore: " unaware", StateAfter: "knows ",
	})
	if err != nil {
		t.Fatalf("AddStoryEventParticipant: %v", err)
	}
	if actor.StateBefore != "unaware" || actor.StateAfter != "knows" {
		t.Fatalf("the states were not trimmed: %+v", actor)
	}
	// A second role for the same entity and event is allowed; the same role is
	// not.
	if _, err := service.AddStoryEventParticipant(ctx, AddStoryEventParticipantRequest{
		StoryEventID: event.ID, StoryEntityID: entity.ID, Role: storydomain.ParticipantWitness,
	}); err != nil {
		t.Fatalf("a second role must be accepted: %v", err)
	}
	if _, err := service.AddStoryEventParticipant(ctx, AddStoryEventParticipantRequest{
		StoryEventID: event.ID, StoryEntityID: entity.ID, Role: storydomain.ParticipantActor,
	}); err == nil {
		t.Fatal("the same role twice was accepted")
	}
	// An unknown role is refused by the domain rather than reaching SQLite.
	if _, err := service.AddStoryEventParticipant(ctx, AddStoryEventParticipantRequest{
		StoryEventID: event.ID, StoryEntityID: entity.ID, Role: "culprit",
	}); err == nil {
		t.Fatal("an unknown role was accepted")
	}
	participants, err := service.ListStoryEventParticipants(ctx, event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 2 {
		t.Fatalf("the participant list holds %d rows, want 2", len(participants))
	}
}

// TestRecordFactSource covers the evidence row: it names the version it quotes
// from, carries an optional span, and refuses an unknown fact type.
func TestRecordFactSource(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	entity, _ := seedGraphProject(t, service, store)
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	start, end := 40, 96
	spanned, err := service.RecordFactSource(ctx, RecordFactSourceRequest{
		FactType: storydomain.FactEntity, FactID: entity.ID,
		ChapterID: "chapter-1", SourceDocumentVersionID: "docv-1",
		StartOffset: &start, EndOffset: &end,
		QuoteHash: digest, SourceKind: storydomain.SourceKindAgentInference,
	})
	if err != nil {
		t.Fatalf("RecordFactSource: %v", err)
	}
	if spanned.StartOffset == nil || *spanned.StartOffset != 40 {
		t.Fatalf("the span was not stored: %+v", spanned)
	}
	// A user-stated fact has no span and still names the version, which is what
	// §6.6 requires.
	bare, err := service.RecordFactSource(ctx, RecordFactSourceRequest{
		FactType: storydomain.FactEntity, FactID: entity.ID,
		SourceDocumentVersionID: "docv-1", SourceKind: storydomain.SourceKindUser,
	})
	if err != nil {
		t.Fatalf("evidence without a span must be accepted: %v", err)
	}
	if bare.StartOffset != nil {
		t.Fatalf("evidence without a span gained one: %+v", bare)
	}
	// The empty kind defaults to text rather than failing, because the common
	// caller is extraction and text is what it found.
	defaulted, err := service.RecordFactSource(ctx, RecordFactSourceRequest{
		FactType: storydomain.FactEntity, FactID: entity.ID,
		SourceDocumentVersionID: "docv-1",
	})
	if err != nil {
		t.Fatalf("evidence without a kind must be accepted: %v", err)
	}
	if defaulted.SourceKind != storydomain.SourceKindText {
		t.Fatalf("an omitted evidence kind became %q, want the text default", defaulted.SourceKind)
	}
	// An explicit kind is kept rather than replaced by the default.
	if bare.SourceKind != storydomain.SourceKindUser {
		t.Fatalf("an explicit evidence kind was replaced: %q", bare.SourceKind)
	}
	// The three refusals: an unknown fact type, a missing fact, and evidence with
	// no version to index.
	if _, err := service.RecordFactSource(ctx, RecordFactSourceRequest{
		FactType: "chapter", FactID: entity.ID, SourceDocumentVersionID: "docv-1",
	}); err == nil {
		t.Fatal("an unknown fact type was accepted")
	}
	if _, err := service.RecordFactSource(ctx, RecordFactSourceRequest{
		FactType: storydomain.FactEntity, FactID: "  ", SourceDocumentVersionID: "docv-1",
	}); err == nil {
		t.Fatal("evidence with no fact was accepted")
	}
	if _, err := service.RecordFactSource(ctx, RecordFactSourceRequest{
		FactType: storydomain.FactEntity, FactID: entity.ID,
	}); err == nil {
		t.Fatal("evidence with no document version was accepted")
	}
	// A reversed span is refused, because an offset pair that ends before it
	// starts cannot be read back.
	reversed, err := service.RecordFactSource(ctx, RecordFactSourceRequest{
		FactType: storydomain.FactEntity, FactID: entity.ID,
		SourceDocumentVersionID: "docv-1", StartOffset: &end, EndOffset: &start,
	})
	if err == nil {
		t.Fatalf("a reversed evidence span was accepted: %+v", reversed)
	}
	// And the read returns the two rows that were stored.
	sources, err := service.ListStoryFactSources(ctx, storydomain.FactEntity, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 {
		t.Fatalf("the evidence list holds %d rows, want the three that were stored", len(sources))
	}
	if _, err := service.ListStoryFactSources(ctx, "chapter", entity.ID); err == nil {
		t.Fatal("an unknown fact type was accepted as a query filter")
	}
}

// TestStoryGraphQueriesFailClosed proves an unattached service refuses rather
// than panicking, which is the rule every other command in this package follows.
func TestStoryGraphQueriesFailClosed(t *testing.T) {
	service := NewService(Options{})
	ctx := context.Background()
	checks := []struct {
		name string
		call func() error
	}{
		{"ListStoryEntities", func() error { _, err := service.ListStoryEntities(ctx, "p", ""); return err }},
		{"ListStoryEvents", func() error { _, err := service.ListStoryEvents(ctx, "p", "", ""); return err }},
		{"ListStoryRelations", func() error { _, err := service.ListStoryRelations(ctx, "p", ""); return err }},
		{"ListStoryEventParticipants", func() error { _, err := service.ListStoryEventParticipants(ctx, "e"); return err }},
		{"ListStoryEntityAliases", func() error { _, err := service.ListStoryEntityAliases(ctx, "e"); return err }},
		{"ListStoryFactSources", func() error {
			_, err := service.ListStoryFactSources(ctx, storydomain.FactEntity, "e")
			return err
		}},
		{"AddStoryEntityAlias", func() error {
			_, err := service.AddStoryEntityAlias(ctx, AddStoryEntityAliasRequest{StoryEntityID: "e", Alias: "a"})
			return err
		}},
		{"AddStoryEventParticipant", func() error {
			_, err := service.AddStoryEventParticipant(ctx, AddStoryEventParticipantRequest{StoryEventID: "e", StoryEntityID: "f", Role: "actor"})
			return err
		}},
		{"RecordFactSource", func() error {
			_, err := service.RecordFactSource(ctx, RecordFactSourceRequest{FactType: storydomain.FactEntity, FactID: "e", SourceDocumentVersionID: "v"})
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); err == nil {
				t.Fatal("an unattached service served the call instead of failing closed")
			}
		})
	}
}

// TestListStoryConflicts covers the conflict queue AC-STORY-002 names.
//
// WP-05 exposed OpenConflict and ResolveConflict with no way to list what had
// been recorded, so a conflict could be created and never found again. A recorded
// conflict nobody can list is not a record, which is why this is a scope item
// rather than a nicety.
func TestListStoryConflicts(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	first, _ := seedGraphProject(t, service, store)
	second, _ := service.CreateStoryEntity(context.Background(), CreateStoryEntityRequest{
		ProjectID: "project-1", Type: storydomain.EntityLocation, CanonicalName: "The Hall",
	})
	ctx := context.Background()

	// An empty list is not an error, which is what the panel's empty state needs.
	empty, err := service.ListStoryConflicts(ctx, "project-1", "")
	if err != nil {
		t.Fatalf("an empty conflict list must not be an error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("a project with no conflicts returned %d", len(empty))
	}

	opened, err := service.OpenConflict(ctx, OpenConflictRequest{
		ProjectID:    "project-1",
		LeftFactType: storydomain.FactEntity, LeftFactID: first.ID,
		RightFactType: storydomain.FactEntity, RightFactID: second.ID,
		ConflictType: "name",
	})
	if err != nil {
		t.Fatalf("OpenConflict: %v", err)
	}
	if opened.Status != storydomain.ConflictOpen {
		t.Fatalf("a new conflict is %q, want open", opened.Status)
	}
	all, err := service.ListStoryConflicts(ctx, "project-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != opened.ID {
		t.Fatalf("the conflict list returned %+v", all)
	}
	// The filter narrows to the open ones, which is the queue the panel shows.
	open, err := service.ListStoryConflicts(ctx, "project-1", storydomain.ConflictOpen)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("the open filter returned %d", len(open))
	}
	resolved, err := service.ListStoryConflicts(ctx, "project-1", storydomain.ConflictResolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 0 {
		t.Fatalf("the resolved filter returned %d before anything was resolved", len(resolved))
	}

	// After a resolution the row moves from one filter to the other, and the
	// resolution is recorded on it rather than only implied by the status.
	if _, err := service.ResolveConflict(ctx, ResolveConflictRequest{
		ConflictID: opened.ID, Resolution: "The hall is where Mira lives.", ResolvedBy: "user-1",
	}); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	open, err = service.ListStoryConflicts(ctx, "project-1", storydomain.ConflictOpen)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("a resolved conflict still appears in the open queue: %+v", open)
	}
	resolved, err = service.ListStoryConflicts(ctx, "project-1", storydomain.ConflictResolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("the resolved filter returned %d", len(resolved))
	}
	if resolved[0].Resolution == "" || resolved[0].ResolvedBy != "user-1" {
		t.Fatalf("the resolution was not recorded: %+v", resolved[0])
	}
	// A different project sees none of it, which is what keeps one drama's
	// disagreements out of another's queue.
	other, err := service.ListStoryConflicts(ctx, "project-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("another project saw %d conflicts", len(other))
	}
}

// TestListStoryConflictsRejectsAnUnknownStatus keeps the filter to the vocabulary
// the schema's CHECK accepts, so a typo surfaces as a refusal rather than as an
// empty queue the user reads as "nothing to resolve".
func TestListStoryConflictsRejectsAnUnknownStatus(t *testing.T) {
	service := newTestService(newMemoryStore())
	ctx := context.Background()
	for _, status := range []storydomain.ConflictStatus{"pending", "Open", "closed"} {
		if _, err := service.ListStoryConflicts(ctx, "project-1", status); err == nil {
			t.Fatalf("the status %q was accepted as a filter", status)
		}
	}
	if _, err := service.ListStoryConflicts(ctx, "  ", ""); err == nil {
		t.Fatal("an empty project id was accepted")
	}
}

// TestLockAndUnlock cover AC-STORY-002's third decision.
//
// The status existed and four gates refused to move a fact OUT of it, while
// nothing could put a fact INTO it: a state only the database could produce. These
// tests are what make it a user command.
func TestLockAndUnlock(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	entity, _ := seedGraphProject(t, service, store)
	ctx := context.Background()

	locked, err := service.LockStoryEntity(ctx, LockStoryEntityRequest{ID: entity.ID, Revision: entity.Revision})
	if err != nil {
		t.Fatalf("LockStoryEntity: %v", err)
	}
	if locked.Status != storydomain.FactLocked {
		t.Fatalf("the entity is %q after locking", locked.Status)
	}
	if locked.Revision != entity.Revision+1 {
		t.Fatalf("the revision is %d, want %d", locked.Revision, entity.Revision+1)
	}

	// Every gate refuses to move a locked fact, which is what the lock is for.
	if _, err := service.AcceptStoryEntity(ctx, FactDecisionRequest{ID: entity.ID, Revision: locked.Revision}); err == nil {
		t.Fatal("a locked entity was accepted")
	}
	if _, err := service.RejectStoryEntity(ctx, FactDecisionRequest{ID: entity.ID, Revision: locked.Revision}); err == nil {
		t.Fatal("a locked entity was rejected")
	}
	// Locking twice is a conflict rather than a no-op revision.
	if _, err := service.LockStoryEntity(ctx, LockStoryEntityRequest{ID: entity.ID, Revision: locked.Revision}); err == nil {
		t.Fatal("an already locked entity was locked again")
	}
	// A stale revision is a conflict, so a lock cannot silently overwrite another
	// window's change.
	if _, err := service.LockStoryEntity(ctx, LockStoryEntityRequest{ID: entity.ID, Revision: entity.Revision}); err == nil {
		t.Fatal("a lock with a stale revision was accepted")
	}

	// Releasing returns the fact to 'accepted' — the state a locked fact was in
	// before it was pinned — rather than to 'candidate', which would discard the
	// decision the lock was protecting.
	unlocked, err := service.UnlockStoryEntity(ctx, LockStoryEntityRequest{ID: entity.ID, Revision: locked.Revision})
	if err != nil {
		t.Fatalf("UnlockStoryEntity: %v", err)
	}
	if unlocked.Status != storydomain.FactAccepted {
		t.Fatalf("the entity is %q after unlocking, want accepted", unlocked.Status)
	}
	// Unlocking something that is not locked is refused rather than treated as a
	// confirmation the user did not make.
	if _, err := service.UnlockStoryEntity(ctx, LockStoryEntityRequest{ID: entity.ID, Revision: unlocked.Revision}); err == nil {
		t.Fatal("an unlocked entity was unlocked again")
	}
}

// TestLockStoryEvent covers the same rules for events, because they are separate
// commands over a separate table rather than one generic path.
func TestLockStoryEvent(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	event, err := service.CreateStoryEvent(ctx, CreateStoryEventRequest{
		ProjectID: "project-1", ChapterID: "chapter-1", Name: "Mira finds the letter",
	})
	if err != nil {
		t.Fatal(err)
	}
	locked, err := service.LockStoryEvent(ctx, LockStoryEntityRequest{ID: event.ID, Revision: event.Revision})
	if err != nil {
		t.Fatalf("LockStoryEvent: %v", err)
	}
	if locked.Status != storydomain.FactLocked {
		t.Fatalf("the event is %q after locking", locked.Status)
	}
	if _, err := service.AcceptStoryEvent(ctx, FactDecisionRequest{ID: event.ID, Revision: locked.Revision}); err == nil {
		t.Fatal("a locked event was accepted")
	}
	unlocked, err := service.UnlockStoryEvent(ctx, LockStoryEntityRequest{ID: event.ID, Revision: locked.Revision})
	if err != nil {
		t.Fatalf("UnlockStoryEvent: %v", err)
	}
	if unlocked.Status != storydomain.FactAccepted {
		t.Fatalf("the event is %q after unlocking", unlocked.Status)
	}
	// A lock on a fact that does not exist is a not-found rather than a storage
	// failure, so the UI can say which it is.
	if _, err := service.LockStoryEntity(ctx, LockStoryEntityRequest{ID: "no-such-entity", Revision: 1}); err == nil {
		t.Fatal("a missing entity was locked")
	}
}

// TestLockingIsRefusedOnAMissingOrStaleFact proves an unattached service fails
// closed for the lock commands too.
func TestLockCommandsFailClosed(t *testing.T) {
	service := NewService(Options{})
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"LockStoryEntity": func() error {
			_, err := service.LockStoryEntity(ctx, LockStoryEntityRequest{ID: "e", Revision: 1})
			return err
		},
		"UnlockStoryEntity": func() error {
			_, err := service.UnlockStoryEntity(ctx, LockStoryEntityRequest{ID: "e", Revision: 1})
			return err
		},
		"LockStoryEvent": func() error {
			_, err := service.LockStoryEvent(ctx, LockStoryEntityRequest{ID: "e", Revision: 1})
			return err
		},
		"UnlockStoryEvent": func() error {
			_, err := service.UnlockStoryEvent(ctx, LockStoryEntityRequest{ID: "e", Revision: 1})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Fatalf("%s served the call on an unattached service", name)
		}
	}
}

// TestReviseChapterMarksTheBoundaryManual covers the promise migration 000014
// makes: "A user-edited boundary is marked 'manual' by the command that edits it."
//
// The column exists so the import report and the staleness walk can tell a
// detector's boundary from a person's, and a revised chapter that still read
// 'regex' would make both of them wrong about where it came from. The migration
// recorded the rule and nothing implemented it.
func TestReviseChapterMarksTheBoundaryManual(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	// A chapter as the detector would have written it.
	store.chapters["chapter-1"] = storydomain.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: "version-1", Ordinal: 1, Title: "One",
		StartOffset: 0, EndOffset: 100, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterDetected, Revision: 1,
	}
	revised, err := service.ReviseChapter(ctx, ReviseChapterRequest{
		ChapterID: "chapter-1", Title: "One, corrected", StartOffset: 0, EndOffset: 120, Revision: 1,
	})
	if err != nil {
		t.Fatalf("ReviseChapter: %v", err)
	}
	if revised.SourceKind != storydomain.ChapterManual {
		t.Fatalf("the revised boundary reads %q, want manual", revised.SourceKind)
	}
	// And the stored row carries it, not only the returned value.
	stored, err := store.GetChapter(ctx, "chapter-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.SourceKind != storydomain.ChapterManual {
		t.Fatalf("the stored boundary reads %q, want manual", stored.SourceKind)
	}
}

// TestLockIsRefusedOnARejectedFact stops the lock from reversing a rejection.
//
// An independent review found this: lock was allowed from any status, and unlock
// returns a fact to 'accepted', so reject → lock → unlock turned a rejection into
// a confirmation through two individually-legal commands — reachable from the UI
// in two clicks. acceptGate refuses that outcome everywhere else ("A rejected fact
// keeps its decision until a new candidate is recorded").
func TestLockIsRefusedOnARejectedFact(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	// The CANDIDATE from the seed, not the accepted one: rejecting a confirmed
	// fact is refused by design, which is a different rule.
	_, candidate := seedGraphProject(t, service, store)
	ctx := context.Background()

	rejected, err := service.RejectStoryEntity(ctx, FactDecisionRequest{ID: candidate.ID, Revision: candidate.Revision})
	if err != nil {
		t.Fatalf("RejectStoryEntity: %v", err)
	}
	if rejected.Status != storydomain.FactRejected {
		t.Fatalf("the entity is %q after rejecting", rejected.Status)
	}
	if _, err := service.LockStoryEntity(ctx, LockStoryEntityRequest{ID: candidate.ID, Revision: rejected.Revision}); err == nil {
		t.Fatal("a rejected entity was locked, so unlocking it would confirm it")
	}
	// The refusal changed nothing: the rejection still stands.
	stored, err := store.GetStoryEntity(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != storydomain.FactRejected || stored.Revision != rejected.Revision {
		t.Fatalf("the refused lock modified the row: %+v", stored)
	}

	// The same rule for events, which is a separate command.
	event, err := service.CreateStoryEvent(ctx, CreateStoryEventRequest{
		ProjectID: "project-1", ChapterID: "chapter-1", Name: "n",
	})
	if err != nil {
		t.Fatal(err)
	}
	rejectedEvent, err := service.RejectStoryEvent(ctx, FactDecisionRequest{ID: event.ID, Revision: event.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.LockStoryEvent(ctx, LockStoryEntityRequest{ID: event.ID, Revision: rejectedEvent.Revision}); err == nil {
		t.Fatal("a rejected event was locked")
	}
}

// TestLockFromCandidateAndAcceptedBothUnlockToAccepted pins the states the lock IS
// reachable from, so the refusal above is not over-broad.
func TestLockFromCandidateAndAcceptedBothUnlockToAccepted(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	// A candidate: never confirmed, but pinning it is legitimate because unlocking
	// returns it to accepted, which is a state it could have reached anyway.
	candidate, err := service.CreateStoryEntity(ctx, CreateStoryEntityRequest{
		ProjectID: "project-1", Type: storydomain.EntityCharacter, CanonicalName: "Mira",
	})
	if err != nil {
		t.Fatal(err)
	}
	locked, err := service.LockStoryEntity(ctx, LockStoryEntityRequest{ID: candidate.ID, Revision: candidate.Revision})
	if err != nil {
		t.Fatalf("a candidate must be lockable: %v", err)
	}
	unlocked, err := service.UnlockStoryEntity(ctx, LockStoryEntityRequest{ID: candidate.ID, Revision: locked.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if unlocked.Status != storydomain.FactAccepted {
		t.Fatalf("unlocking a locked candidate gave %q", unlocked.Status)
	}
}

// TestSplitChapterCoversTheSameText is the invariant that makes a split safe: the
// two halves must tile exactly what the one chapter covered, or the version's
// chapters would no longer add up to the document.
func TestSplitChapterCoversTheSameText(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	store.versions["version-1"] = storydomain.SourceDocumentVersion{
		ID: "version-1", SourceDocumentID: "doc-1", VersionNumber: 1,
		NormalizedTextFileID: strings.Repeat("a", 64), CharCount: 300,
		CreatedByType: versioning.CreatedByUser,
	}
	store.chapters["chapter-1"] = storydomain.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: "version-1", Ordinal: 1, Title: "One",
		StartOffset: 0, EndOffset: 300, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}
	store.chapters["chapter-2"] = storydomain.Chapter{
		ID: "chapter-2", SourceDocumentVersionID: "version-1", Ordinal: 2, Title: "Two",
		StartOffset: 300, EndOffset: 400, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}

	halves, err := service.SplitChapter(ctx, SplitChapterRequest{
		ChapterID: "chapter-1", SplitAtOffset: 120, SecondTitle: "One (continued)", Revision: 1,
	})
	if err != nil {
		t.Fatalf("SplitChapter: %v", err)
	}
	if len(halves) != 2 {
		t.Fatalf("a split returned %d chapters", len(halves))
	}
	first, second := halves[0], halves[1]
	// The two halves cover exactly the original range.
	if first.StartOffset != 0 || first.EndOffset != 120 {
		t.Fatalf("the first half is %d..%d, want 0..120", first.StartOffset, first.EndOffset)
	}
	if second.StartOffset != 120 || second.EndOffset != 300 {
		t.Fatalf("the second half is %d..%d, want 120..300", second.StartOffset, second.EndOffset)
	}
	if first.EndOffset != second.StartOffset {
		t.Fatal("the halves do not meet, so the split lost or duplicated text")
	}
	if second.Ordinal != first.Ordinal+1 {
		t.Fatalf("the second half is ordinal %d, want %d", second.Ordinal, first.Ordinal+1)
	}
	// Both halves are the user's, and both are marked edited: a person made them.
	for _, chapter := range halves {
		if chapter.SourceKind != storydomain.ChapterManual {
			t.Fatalf("a split half reads source kind %q, want manual", chapter.SourceKind)
		}
		if chapter.Status != storydomain.ChapterEdited {
			t.Fatalf("a split half reads status %q, want edited", chapter.Status)
		}
	}
	// The chapter that followed moved up, so the ordinals have no duplicate.
	moved, err := store.GetChapter(ctx, "chapter-2")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Ordinal != 3 {
		t.Fatalf("the following chapter is ordinal %d, want 3", moved.Ordinal)
	}
	// And it kept its own range, so only its position changed.
	if moved.StartOffset != 300 || moved.EndOffset != 400 {
		t.Fatalf("the following chapter's range changed: %d..%d", moved.StartOffset, moved.EndOffset)
	}
	// A split at either end produces an empty half, which no detector would
	// produce and no user asked for.
	for _, offset := range []int{0, 300, -5, 1000} {
		if _, err := service.SplitChapter(ctx, SplitChapterRequest{
			ChapterID: "chapter-1", SplitAtOffset: offset, Revision: 2,
		}); err == nil {
			t.Fatalf("a split at offset %d was accepted", offset)
		}
	}
}

// TestMergeChapterRequiresAdjacency covers the two ways a merge could swallow text
// the user did not intend: a gap in the ranges, and a chapter that is not the next
// one by ordinal.
func TestMergeChapterRequiresAdjacency(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	store.versions["version-1"] = storydomain.SourceDocumentVersion{
		ID: "version-1", SourceDocumentID: "doc-1", VersionNumber: 1,
		NormalizedTextFileID: strings.Repeat("a", 64), CharCount: 400,
		CreatedByType: versioning.CreatedByUser,
	}
	store.chapters["chapter-1"] = storydomain.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: "version-1", Ordinal: 1, Title: "One",
		StartOffset: 0, EndOffset: 100, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}
	// A gap: chapter two starts at 120 rather than where chapter one ends.
	store.chapters["chapter-2"] = storydomain.Chapter{
		ID: "chapter-2", SourceDocumentVersionID: "version-1", Ordinal: 2, Title: "Two",
		StartOffset: 120, EndOffset: 200, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}
	if _, err := service.MergeChapter(ctx, MergeChapterRequest{
		FirstChapterID: "chapter-1", SecondChapterID: "chapter-2", Revision: 1,
	}); err == nil {
		t.Fatal("chapters with a gap between them were merged, which would hide the passage in between")
	}

	// Make them adjacent and the merge succeeds, covering the union.
	adjacent := store.chapters["chapter-2"]
	adjacent.StartOffset = 100
	store.chapters["chapter-2"] = adjacent
	merged, err := service.MergeChapter(ctx, MergeChapterRequest{
		FirstChapterID: "chapter-1", SecondChapterID: "chapter-2", Revision: 1,
	})
	if err != nil {
		t.Fatalf("MergeChapter: %v", err)
	}
	if merged.StartOffset != 0 || merged.EndOffset != 200 {
		t.Fatalf("the merged chapter is %d..%d, want 0..200", merged.StartOffset, merged.EndOffset)
	}
	if merged.Status != storydomain.ChapterEdited || merged.SourceKind != storydomain.ChapterManual {
		t.Fatalf("the merged chapter is %q/%q, want edited/manual", merged.Status, merged.SourceKind)
	}
	// The absorbed chapter is gone from the version.
	stored, err := store.ListChapters(ctx, "version-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID != "chapter-1" {
		t.Fatalf("the version holds %+v after the merge", stored)
	}

	// A chapter that is not the immediate successor cannot be merged, because the
	// ordinals could not express the result.
	store.chapters["chapter-3"] = storydomain.Chapter{
		ID: "chapter-3", SourceDocumentVersionID: "version-1", Ordinal: 3, Title: "Three",
		StartOffset: 200, EndOffset: 300, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}
	if _, err := service.MergeChapter(ctx, MergeChapterRequest{
		FirstChapterID: "chapter-1", SecondChapterID: "chapter-3", Revision: 2,
	}); err == nil {
		t.Fatal("a non-adjacent chapter was merged")
	}
}

// TestSplitAndMergeFailClosed proves the two commands refuse on an unattached
// service rather than panicking.
func TestSplitAndMergeFailClosed(t *testing.T) {
	service := NewService(Options{})
	ctx := context.Background()
	if _, err := service.SplitChapter(ctx, SplitChapterRequest{ChapterID: "c", SplitAtOffset: 1, Revision: 1}); err == nil {
		t.Fatal("an unattached service split a chapter")
	}
	if _, err := service.MergeChapter(ctx, MergeChapterRequest{FirstChapterID: "a", SecondChapterID: "b", Revision: 1}); err == nil {
		t.Fatal("an unattached service merged chapters")
	}
}
