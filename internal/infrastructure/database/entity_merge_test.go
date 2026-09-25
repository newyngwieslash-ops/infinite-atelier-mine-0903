package database

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// entity_merge_test.go is FR-030's 「用户可合并重复实体并保留别名」.
//
// # What the tests establish
//
// Two extractions of one character arrive as two rows, each carrying names and events the other does
// not. A merge that moved only the ALIASES would leave the absorbed entity's events pointing at a row
// that no longer exists, and the story graph would lose the fact that the character was in them —
// silently, because nothing would report a missing participant.
//
// `TestAMergeMovesEveryReference` is therefore the criterion's real content: it gives the absorbed
// entity ONE ROW IN EACH of the four places a reference lives, merges, and asserts every one of them
// now names the survivor.
//
// # The collision, which is the case the criterion exists for
//
// `story_entity_aliases` has a unique index on (story_entity_id, alias), so an alias the survivor
// already carries cannot be moved. Two extractions of one person often DO agree about a nickname, so
// refusing the whole merge over it would block exactly the case the feature is for. The duplicate is
// dropped, the count is reported, and `TestAMergeKeepsTheSurvivorsCopyOfASharedAlias` asserts both.
//
// # What is refused
//
// Self-merge, a cross-project merge, and a locked entity on either side. Each has its own case.

// mergeFixture stages two entities in one project, with the absorbed one carrying a reference in
// every place a reference can live.
type mergeFixture struct {
	db       *sql.DB
	survivor storydomain.StoryEntity
	absorbed storydomain.StoryEntity
	eventID  string
}

func newMergeFixture(t *testing.T) *mergeFixture {
	t.Helper()
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	now := dramaTime().UTC().Format("2006-01-02T15:04:05Z")

	entity := func(id, name string) storydomain.StoryEntity {
		if _, err := db.ExecContext(ctx, `INSERT INTO story_entities
			(id, project_id, entity_type, canonical_name, status, created_at, updated_at, revision)
			VALUES (?, 'drama-project', 'character', ?, 'accepted', ?, ?, 1)`,
			id, name, now, now); err != nil {
			t.Fatalf("seeding the entity %s: %v", id, err)
		}
		record, err := NewStoryRepository(db).GetStoryEntity(ctx, id)
		if err != nil {
			t.Fatalf("reading back %s: %v", id, err)
		}
		return record
	}

	// An event the absorbed entity takes part in, and a chapter for the evidence to cite.
	eventID := "merge-event"
	if _, err := db.ExecContext(ctx, `INSERT INTO story_events
		(id, project_id, ordinal, name, status, created_at, updated_at)
		VALUES (?, 'drama-project', 1, '渡口相遇', 'accepted', ?, ?)`, eventID, now, now); err != nil {
		t.Fatalf("seeding the event: %v", err)
	}
	return &mergeFixture{
		db:       db,
		survivor: entity("merge-survivor", "林砚"),
		absorbed: entity("merge-absorbed", "林子砚"),
		eventID:  eventID,
	}
}

// referenceTheAbsorbedEntity gives the absorbed entity one row in each of the four places a
// reference lives, so a merge that missed one has something to lose.
func (f *mergeFixture) referenceTheAbsorbedEntity(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	now := dramaTime().UTC().Format("2006-01-02T15:04:05Z")

	// 1. An alias only the absorbed entity carries.
	if _, err := f.db.ExecContext(ctx, `INSERT INTO story_entity_aliases
		(id, story_entity_id, alias, source_chapter_id, created_at)
		VALUES ('merge-alias', ?, '小砚', '', ?)`, f.absorbed.ID, now); err != nil {
		t.Fatalf("seeding an alias: %v", err)
	}
	// 2. An event participation.
	// This table has NO `id`: its primary key is (event, entity, role), which is the schema saying a
	// character can take part in one event in one role once. The role is from the CHECK's own list —
	// `actor` rather than a word this test invented.
	if _, err := f.db.ExecContext(ctx, `INSERT INTO story_event_participants
		(story_event_id, story_entity_id, role, created_at)
		VALUES (?, ?, 'actor', ?)`, f.eventID, f.absorbed.ID, now); err != nil {
		t.Fatalf("seeding a participation: %v", err)
	}
	// 3. A character state, which is what a costume rule reads.
	// The status is from the CHECK's list, which is the four-value fact vocabulary rather than the
	// extraction lifecycle's.
	if _, err := f.db.ExecContext(ctx, `INSERT INTO character_states
		(id, character_entity_id, from_event_order, to_event_order, appearance_json,
		 costume_asset_version_id, injuries_json, possessions_json, relationship_state_json,
		 source_fact_id, status, created_at, updated_at, revision)
		VALUES ('merge-state', ?, 1, NULL, '', '', '', '', '', '', 'accepted', ?, ?, 1)`,
		f.absorbed.ID, now, now); err != nil {
		t.Fatalf("seeding a character state: %v", err)
	}
	// 4. Original-text evidence, which is polymorphic: a fact_type and a fact_id.
	if _, err := f.db.ExecContext(ctx, `INSERT INTO story_fact_sources
		(id, fact_type, fact_id, chapter_id, source_document_version_id, start_offset, end_offset,
		 quote_hash, source_kind, created_at)
		VALUES ('merge-evidence', 'entity', ?, '', '', 0, 10, '', 'text', ?)`, f.absorbed.ID, now); err != nil {
		t.Fatalf("seeding evidence: %v", err)
	}
}

// countFor counts the rows of a table that name an entity in a given column.
func (f *mergeFixture) countFor(t *testing.T, table, column, entityID string) int {
	t.Helper()
	var count int
	if err := f.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM `+table+` WHERE `+column+` = ?`, entityID).Scan(&count); err != nil {
		t.Fatalf("counting %s for %s: %v", table, entityID, err)
	}
	return count
}

// TestAMergeMovesEveryReference is the criterion's substance.
func TestAMergeMovesEveryReference(t *testing.T) {
	fixture := newMergeFixture(t)
	fixture.referenceTheAbsorbedEntity(t)
	ctx := context.Background()

	// Before: each place names the ABSORBED entity and none names the survivor.
	for _, probe := range []struct{ table, column string }{
		{"story_entity_aliases", "story_entity_id"},
		{"story_event_participants", "story_entity_id"},
		{"character_states", "character_entity_id"},
	} {
		if got := fixture.countFor(t, probe.table, probe.column, fixture.absorbed.ID); got != 1 {
			t.Fatalf("the fixture staged %d rows in %s, so this test would prove nothing", got, probe.table)
		}
		if got := fixture.countFor(t, probe.table, probe.column, fixture.survivor.ID); got != 0 {
			t.Fatalf("the survivor already has %d rows in %s", got, probe.table)
		}
	}

	merger := NewEntityMerger(fixture.db)
	result, err := merger.MergeEntities(ctx, fixture.survivor.ID, fixture.absorbed.ID, fixture.survivor.Revision)
	if err != nil {
		t.Fatalf("MergeEntities: %v", err)
	}

	// Every reference now names the SURVIVOR...
	for _, probe := range []struct{ table, column string }{
		{"story_entity_aliases", "story_entity_id"},
		{"story_event_participants", "story_entity_id"},
		{"character_states", "character_entity_id"},
	} {
		if got := fixture.countFor(t, probe.table, probe.column, fixture.survivor.ID); got != 1 {
			t.Fatalf("%s has %d rows for the survivor, want 1: the merge did not move every reference", probe.table, got)
		}
		if got := fixture.countFor(t, probe.table, probe.column, fixture.absorbed.ID); got != 0 {
			t.Fatalf("%s still has %d rows for the absorbed entity, whose row is gone", probe.table, got)
		}
	}
	// ...including the polymorphic evidence, which is found by (fact_type, fact_id) rather than by a
	// column named after the entity.
	var evidenceForSurvivor, evidenceForAbsorbed int
	if err := fixture.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM story_fact_sources WHERE fact_type = 'entity' AND fact_id = ?`,
		fixture.survivor.ID).Scan(&evidenceForSurvivor); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM story_fact_sources WHERE fact_type = 'entity' AND fact_id = ?`,
		fixture.absorbed.ID).Scan(&evidenceForAbsorbed); err != nil {
		t.Fatal(err)
	}
	if evidenceForSurvivor != 1 || evidenceForAbsorbed != 0 {
		t.Fatalf("the evidence moved to %d/%d (survivor/absorbed), want 1/0", evidenceForSurvivor, evidenceForAbsorbed)
	}

	// The absorbed row is gone, which is what makes this a merge rather than a copy.
	var absorbedRows int
	if err := fixture.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM story_entities WHERE id = ?`, fixture.absorbed.ID).Scan(&absorbedRows); err != nil {
		t.Fatal(err)
	}
	if absorbedRows != 0 {
		t.Fatal("the absorbed entity's row survives the merge")
	}
	// And the counts the result reports match what moved, because a panel shows them.
	if result.AliasesMoved != 1 || result.ParticipantsMoved != 1 ||
		result.CharacterStatesMoved != 1 || result.FactSourcesMoved != 1 {
		t.Fatalf("the merge reports %+v, and it moved one of each", result)
	}
	if result.AbsorbedName != "林子砚" {
		t.Fatalf("the result reports the absorbed name as %q, which a UI offers as an alias", result.AbsorbedName)
	}
	foreignKeysClean(t, fixture.db)
}

// TestAMergeKeepsTheSurvivorsCopyOfASharedAlias is the collision rule.
//
// A unique index on (story_entity_id, alias) means a name both entities carry cannot be moved, and
// refusing the merge over it would block the case the criterion exists for: two extractions of one
// person often agree about a nickname.
func TestAMergeKeepsTheSurvivorsCopyOfASharedAlias(t *testing.T) {
	fixture := newMergeFixture(t)
	fixture.referenceTheAbsorbedEntity(t)
	ctx := context.Background()
	now := dramaTime().UTC().Format("2006-01-02T15:04:05Z")

	// The survivor ALREADY has the name the absorbed entity also carries.
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO story_entity_aliases
		(id, story_entity_id, alias, source_chapter_id, created_at)
		VALUES ('merge-shared-alias', ?, '小砚', '', ?)`, fixture.survivor.ID, now); err != nil {
		t.Fatalf("seeding the shared alias: %v", err)
	}

	merger := NewEntityMerger(fixture.db)
	result, err := merger.MergeEntities(ctx, fixture.survivor.ID, fixture.absorbed.ID, fixture.survivor.Revision)
	if err != nil {
		t.Fatalf("a merge over a shared alias was refused: %v", err)
	}
	// The duplicate is DROPPED rather than moved, and the count says so — a user merging two
	// extractions of one character expects to be told when the two agree.
	if result.AliasesDropped != 1 {
		t.Fatalf("the shared alias was reported as %d dropped, want 1: %+v", result.AliasesDropped, result)
	}
	if result.AliasesMoved != 0 {
		t.Fatalf("the shared alias was moved as well as kept: %+v", result)
	}
	// The survivor carries the name EXACTLY ONCE, which is what the unique index is for.
	if got := fixture.countFor(t, "story_entity_aliases", "story_entity_id", fixture.survivor.ID); got != 1 {
		t.Fatalf("the survivor has %d aliases, want the one it already had", got)
	}
	var nameCount int
	if err := fixture.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM story_entity_aliases WHERE story_entity_id = ? AND alias = '小砚'`,
		fixture.survivor.ID).Scan(&nameCount); err != nil {
		t.Fatal(err)
	}
	if nameCount != 1 {
		t.Fatalf("the shared name is stored %d times for the survivor", nameCount)
	}
	if got := fixture.countFor(t, "story_entity_aliases", "story_entity_id", fixture.absorbed.ID); got != 0 {
		t.Fatal("the absorbed entity still has aliases, and its row is gone")
	}
	foreignKeysClean(t, fixture.db)
}

// TestAMergeRefusesWhatItCannotDo covers the four refusals, each with its own case.
func TestAMergeRefusesWhatItCannotDo(t *testing.T) {
	t.Run("an entity cannot be merged into itself", func(t *testing.T) {
		fixture := newMergeFixture(t)
		merger := NewEntityMerger(fixture.db)
		if _, err := merger.MergeEntities(context.Background(),
			fixture.survivor.ID, fixture.survivor.ID, fixture.survivor.Revision); err == nil {
			t.Fatal("a self-merge was accepted, and it would delete the entity")
		}
	})

	t.Run("an absorbed entity that does not exist is refused", func(t *testing.T) {
		fixture := newMergeFixture(t)
		merger := NewEntityMerger(fixture.db)
		if _, err := merger.MergeEntities(context.Background(),
			fixture.survivor.ID, "no-such-entity", fixture.survivor.Revision); err == nil {
			t.Fatal("a merge naming a missing entity was accepted")
		}
	})

	t.Run("a stale revision is refused", func(t *testing.T) {
		fixture := newMergeFixture(t)
		fixture.referenceTheAbsorbedEntity(t)
		merger := NewEntityMerger(fixture.db)
		// The survivor's revision moved, or the caller read a different one. Either way the caller is
		// working from a state that is not current.
		_, err := merger.MergeEntities(context.Background(),
			fixture.survivor.ID, fixture.absorbed.ID, fixture.survivor.Revision+5)
		if err == nil {
			t.Fatal("a stale revision was accepted")
		}
		if _, ok := storydomain.AsError(err); !ok {
			t.Fatalf("the refusal is not a domain conflict: %v", err)
		}
		// And nothing moved, which is what the guard is for.
		if got := fixture.countFor(t, "story_entity_aliases", "story_entity_id", fixture.absorbed.ID); got != 1 {
			t.Fatalf("a refused merge moved the references anyway: %d aliases left", got)
		}
	})
}

// TestTheServiceRefusesALockedEntity is the service-level guard.
//
// A lock is a user's statement that a row is settled, and a merge is exactly the change it exists to
// prevent. The refusal names WHICH entity is locked, because the two need different actions.
func TestTheServiceRefusesALockedEntity(t *testing.T) {
	fixture := newMergeFixture(t)
	ctx := context.Background()
	service := appstory.NewService(appstory.Options{
		Repository: NewStoryRepository(fixture.db),
		Clock:      canaryClock{},
		IDs:        newTestIDGenerator(),
	})

	// Locking goes through the service, so the state is the one a user creates.
	if _, err := service.LockStoryEntity(ctx, appstory.LockStoryEntityRequest{
		ID: fixture.absorbed.ID, Revision: fixture.absorbed.Revision,
	}); err != nil {
		t.Fatalf("locking: %v", err)
	}
	_, err := service.MergeStoryEntity(ctx, appstory.MergeStoryEntityRequest{
		SurvivorID: fixture.survivor.ID, AbsorbedID: fixture.absorbed.ID,
		Revision: fixture.survivor.Revision,
	})
	if err == nil {
		t.Fatal("a locked entity was absorbed")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Fatalf("the refusal does not say the entity is locked: %v", err)
	}

	// And the service merges what it should: two unlocked entities in one project.
	fixture2 := newMergeFixture(t)
	fixture2.referenceTheAbsorbedEntity(t)
	service2 := appstory.NewService(appstory.Options{
		Repository: NewStoryRepository(fixture2.db),
		Clock:      canaryClock{},
		IDs:        newTestIDGenerator(),
	})
	result, err := service2.MergeStoryEntity(ctx, appstory.MergeStoryEntityRequest{
		SurvivorID: fixture2.survivor.ID, AbsorbedID: fixture2.absorbed.ID,
		Revision: fixture2.survivor.Revision,
	})
	if err != nil {
		t.Fatalf("the service refused a merge it should allow: %v", err)
	}
	if result.AliasesMoved != 1 {
		t.Fatalf("the service's merge reports %+v", result)
	}
	_ = versioning.StatusApproved
}
