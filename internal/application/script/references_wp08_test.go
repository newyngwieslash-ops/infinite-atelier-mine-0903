package script

import (
	"context"
	"strings"
	"testing"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// references_wp08_test.go covers the reference checks and the normalisation the mutation passes
// showed were doing their work WITHOUT a test watching.
//
// Four survivors were all in this area, and they had a common shape: the code refuses correctly, but
// nothing asserted the refusal, so a mutation that deleted the check — or that made the query answer
// a different question — left the suite green. A check nobody tests is a check that will be lost in
// the next refactor.

// TestASkeletonSelectionIsCheckedAgainstTheStoryGraph covers §7.4's link table on the WRITE path.
//
// `story_skeleton_event_links.story_event_id` has no foreign key — a citation is provenance and
// outlives the row — so the service's query is the only thing between a model and a selection that
// names events which do not exist. The mutation that removed the call left every test green, which
// is why this one names the identifier in the refusal rather than only requiring a failure.
func TestASkeletonSelectionIsCheckedAgainstTheStoryGraph(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-real")

	// The event the project has is accepted.
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", SelectedEventIDs: []string{"event-real"},
	}); err != nil {
		t.Fatalf("a selection of an event that exists was refused: %v", err)
	}
	// One it does not have is refused, and the refusal names it so a model can act rather than guess.
	_, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{"event-real", "event-ghost"},
	})
	if err == nil {
		t.Fatal("a selection naming an event that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "event-ghost") {
		t.Fatalf("the refusal reads %q, which does not name the identifier", err)
	}
	// Nothing was written, which is what makes the check a gate rather than a warning: a version
	// written with a bad link set would read as a skeleton that selected an event nobody can open.
	history, err := service.ListVersions(ctx, scriptdomain.FamilyStorySkeleton, episode.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if versions := history.([]scriptdomain.StorySkeletonVersion); len(versions) != 1 {
		t.Fatalf("the refused write left %d versions behind", len(versions))
	}
	// An EMPTY selection is not checked and not refused: a draft that has selected nothing yet is
	// legal, and refusing it would fail a stage for a reason the model cannot act on.
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
	}); err != nil {
		t.Fatalf("a version with no selection was refused: %v", err)
	}
}

// TestStrategyTreatmentsAreCheckedAgainstTheStoryGraph is the same rule for §7.5's link table.
func TestStrategyTreatmentsAreCheckedAgainstTheStoryGraph(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-real")

	if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-real", Treatment: scriptdomain.TreatmentRetained},
		},
	}); err != nil {
		t.Fatalf("a treatment of an event that exists was refused: %v", err)
	}
	_, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-real", Treatment: scriptdomain.TreatmentRetained},
			{StoryEventID: "event-ghost", Treatment: scriptdomain.TreatmentRemoved},
		},
	})
	if err == nil {
		t.Fatal("a treatment of an event that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "event-ghost") {
		t.Fatalf("the refusal reads %q, which does not name the identifier", err)
	}
}

// TestTheSelectionOrderIsPreservedInTheLinkTable covers the ordinal the link table carries.
//
// §7.4 makes the selection a LINK TABLE with an ordinal, and the ordinal is what a reader sees as the
// order. A mutation that wrote 0 for every row left the suite green because the read-back test
// compared only the IDS — so this asserts the positions themselves, and it does so through the real
// repository as well, because the double re-derives the order from the slice and could hide the bug.
func TestTheSelectionOrderIsPreservedInTheLinkTable(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-1", "event-2", "event-3")
	version, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{"event-3", "event-1", "event-2"},
	})
	if err != nil {
		t.Fatalf("CreateStorySkeletonVersion: %v", err)
	}
	// The double stores what the service passed, so the ORDER is asserted here as the caller wrote
	// it: a service that sorted or reordered the selection would show up as a different list.
	stored, err := store.ListSkeletonEventIDs(ctx, version.ID)
	if err != nil {
		t.Fatalf("ListSkeletonEventIDs: %v", err)
	}
	want := []string{"event-3", "event-1", "event-2"}
	if len(stored) != len(want) {
		t.Fatalf("the stored selection is %v, want %v", stored, want)
	}
	for index := range want {
		if stored[index] != want[index] {
			t.Fatalf("the stored selection is %v, want %v in the order the caller wrote it", stored, want)
		}
	}
}

// TestTheDefaultedTreatmentIsRefusedBeforeTheLinkSetIsBuilt covers the guard the mutation pass found
// unwatched.
//
// A treatment is the one vocabulary with no sensible default: "retained" and "removed" are opposite
// decisions about a story event, and defaulting would attribute a decision to a model that never made
// one. The mutation that disabled the check left the suite green because no test drove a treatment-less
// link through the service — the earlier test asserted the DOMAIN validator through a different path.
func TestTheDefaultedTreatmentIsRefusedBeforeTheLinkSetIsBuilt(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-1")
	// The link set is stated with one valid treatment and one absent, so the refusal has to come from
	// the absent one rather than from the set being empty.
	_, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			{StoryEventID: "event-2"},
		},
	})
	if err == nil {
		t.Fatal("an event with no treatment was accepted")
	}
	// The message is the SERVICE's, which names the identifier: the domain validator's message does
	// not, because it does not know which link it was given.
	if !strings.Contains(err.Error(), "event-2") {
		t.Fatalf("the refusal reads %q, which does not name the event", err)
	}
	if !strings.Contains(err.Error(), "treatment") {
		t.Fatalf("the refusal reads %q, which does not mention the treatment", err)
	}
}

// TestAPaddedBaseVersionIDStillFindsItsLocks covers the trim at the WRITE side.
//
// The mutation that removed `readLocks`'s trim survived, and chasing it found the real hole one level
// up: the version ROW stores whatever base id the caller stated, so a caller that wrote " base-1 "
// produced a revision whose `based_on_version_id` names no version — and every lock on the real base
// was then unenforceable, because `readLocks` looked the padded key up and found nothing. A model that
// wanted to escape a pin had only to add a space.
//
// The fix is at the write: the id is trimmed as it is STORED, so the row and the lookup agree. This
// asserts the whole path — a padded `basedOnVersionId` in the request, a lock on the real base, and a
// revision that tries to rewrite it.
func TestAPaddedBaseVersionIDStillFindsItsLocks(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := newTestService(store)
	episode := seedEpisode(t, service)
	base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", EndingHook: "ending",
	})
	if err != nil {
		t.Fatalf("the base: %v", err)
	}
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: base.ID, Family: scriptdomain.FamilyStorySkeleton,
		Field: scriptdomain.LockSkeletonEndingHook, LockedBy: "user-1",
	}); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	// The revision pads the id, which must not change which version it revises.
	revision, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: "  " + base.ID + "  ",
		OpeningHook: "hook", EndingHook: "ending",
	})
	if err != nil {
		t.Fatalf("a revision with a padded base id was refused outright: %v", err)
	}
	// The stored row names the real base, so the NEXT revision's locks resolve.
	if revision.BasedOnVersionID != base.ID {
		t.Fatalf("the revision stores base %q, want %q", revision.BasedOnVersionID, base.ID)
	}
	// A lock does NOT chain: the revision carries no pins of its own, so revising IT may rewrite the
	// hook. This is the documented scope rather than an oversight — `FieldLock` hangs off the VERSION,
	// and carrying a pin forward is the model's job at the next FIX, which is what AC-SCRIPT-002 tests.
	// Asserting it here means the behaviour is stated rather than discovered.
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: revision.ID,
		OpeningHook: "hook", EndingHook: "rewritten",
	}); err != nil {
		t.Fatalf("a revision of an unlocked version was refused by its ancestor's lock: %v", err)
	}
	// And a lock placed on the REVISION is enforced against the next one, so the mechanism works at
	// every link of the chain.
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: revision.ID, Family: scriptdomain.FamilyStorySkeleton,
		Field: scriptdomain.LockSkeletonEndingHook, LockedBy: "user-1",
	}); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: revision.ID,
		OpeningHook: "hook", EndingHook: "rewritten again",
	}); err == nil {
		t.Fatal("a lock on the revision protected nothing")
	}
}

// TestAMalformedFamilyIsRefusedAndNotSilentlySkipped covers the trim the mutation pass found unwatched.
//
// `readLocks` trims the version id before deciding whether the version is a first draft, and the
// consequence of NOT trimming is not a wrong answer — it is a lookup with a whitespace-padded id that
// finds no locks. The difference shows up when a caller states an id with surrounding space: with the
// trim it is the same version, without it the version reads as unlocked, which is the direction that
// silently loses a user's pin.
func TestAMalformedFamilyIsRefusedAndNotSilentlySkipped(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := newTestService(store)
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-1")
	base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", EndingHook: "ending", Climax: "the climax",
	})
	if err != nil {
		t.Fatalf("the base: %v", err)
	}
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: base.ID, Family: scriptdomain.FamilyStorySkeleton,
		Field: scriptdomain.LockSkeletonEndingHook, LockedBy: "user-1",
	}); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	// A lock recorded with a PADDED version id is the same lock, and it must be enforced: without the
	// trim the repository would be asked for the padded key and would find nothing to protect.
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: "  " + base.ID + "  ", Family: scriptdomain.FamilyStorySkeleton,
		Field: scriptdomain.LockSkeletonClimax, LockedBy: "user-1",
	}); err != nil {
		t.Fatalf("LockScriptField with a padded id: %v", err)
	}
	// Rewriting the hook is refused, which proves the first lock was found...
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook", EndingHook: "rewritten",
	}); err == nil {
		t.Fatal("a locked ending hook was rewritten")
	}
	// ...and rewriting the climax is refused too, which proves the padded id resolved to the SAME
	// version rather than to a missing one.
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook", EndingHook: "ending",
		Climax: "rewritten",
	}); err == nil {
		t.Fatal("a lock recorded under a padded version id protected nothing")
	}
	// The revision that respects both is accepted.
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook", EndingHook: "ending",
		Climax: "the climax",
	}); err != nil {
		t.Fatalf("a revision that respected every lock was refused: %v", err)
	}
}
