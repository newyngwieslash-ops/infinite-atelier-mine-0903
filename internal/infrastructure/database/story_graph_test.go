package database

import (
	"context"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// TestStoryGraphListsAgainstTheDatabase covers the reads the graph panel drives,
// against real SQL rather than a double.
//
// The double in the application package mirrors the semantics it is told to
// mirror; only this proves the statements are right — that the soft-delete filter
// is on the right column, that the status filter's empty case really matches
// everything, and that the ordering is the one the panel depends on.

func seedGraphEntity(t *testing.T, db querier, ctx context.Context, id, name, status string) {
	t.Helper()
	statement := `INSERT INTO story_entities (id, project_id, entity_type, canonical_name, status, source_scope, created_at, updated_at, revision)
		VALUES (?, 'drama-project', 'character', ?, ?, 'original', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`
	if _, err := db.ExecContext(ctx, statement, id, name, status); err != nil {
		t.Fatalf("seeding the entity %s: %v", id, err)
	}
}

func TestStoryGraphListsAgainstTheDatabase(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	repo := NewStoryRepository(db)

	seedGraphEntity(t, db, ctx, "entity-accepted", "Mira", "accepted")
	seedGraphEntity(t, db, ctx, "entity-candidate", "The Oath", "candidate")
	seedGraphEntity(t, db, ctx, "entity-deleted", "Gone", "candidate")
	// The soft-deleted row is the one a naive statement would return.
	if _, err := db.ExecContext(ctx,
		`UPDATE story_entities SET deleted_at = '2026-02-01T00:00:00Z' WHERE id = 'entity-deleted'`); err != nil {
		t.Fatal(err)
	}

	// An empty status means any, soft-deleted rows are never returned, and the
	// count is the two live rows.
	all, err := repo.ListStoryEntities(ctx, "drama-project", "")
	if err != nil {
		t.Fatalf("ListStoryEntities: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("the unfiltered list returned %d entities, want 2: %+v", len(all), all)
	}
	for _, entity := range all {
		if entity.DeletedAt.IsZero() == false {
			t.Fatalf("a soft-deleted entity was returned: %+v", entity)
		}
	}
	// The status filter selects one.
	accepted, err := repo.ListStoryEntities(ctx, "drama-project", story.FactAccepted)
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted) != 1 || accepted[0].ID != "entity-accepted" {
		t.Fatalf("the accepted filter returned %+v", accepted)
	}
	// A project with nothing in it returns an empty list, not an error.
	none, err := repo.ListStoryEntities(ctx, "no-such-project", "")
	if err != nil {
		t.Fatalf("an unknown project must not be an error: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("an unknown project returned %d entities", len(none))
	}

	// The alias and evidence writes round-trip, which is what the extraction path
	// depends on: it writes through these and reads back to show the user where a
	// fact came from.
	alias := story.StoryEntityAlias{
		ID: "alias-1", StoryEntityID: "entity-accepted", Alias: "小艾",
		SourceChapterID: "drama-chapter",
		CreatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	start, end := 10, 12
	alias.SourceStart = &start
	alias.SourceEnd = &end
	if err := repo.CreateStoryEntityAlias(ctx, alias); err != nil {
		t.Fatalf("CreateStoryEntityAlias: %v", err)
	}
	aliases, err := repo.ListStoryEntityAliases(ctx, "entity-accepted")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 1 || aliases[0].Alias != "小艾" {
		t.Fatalf("the alias round trip returned %+v", aliases)
	}
	// The optional span survives as a pointer rather than becoming a zero, which
	// is the distinction section 6.2 draws between "found here" and "typed by a
	// user".
	if aliases[0].SourceStart == nil || *aliases[0].SourceStart != 10 {
		t.Fatalf("the alias span did not survive the round trip: %+v", aliases[0])
	}

	// A fact source with no span must read back with nil offsets rather than
	// zeroes, for the same reason.
	source := story.StoryFactSource{
		ID: "source-1", FactType: story.FactEntity, FactID: "entity-accepted",
		ChapterID: "drama-chapter", SourceDocumentVersionID: "drama-version",
		SourceKind: story.SourceKindAgentInference,
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := repo.CreateStoryFactSource(ctx, source); err != nil {
		t.Fatalf("CreateStoryFactSource: %v", err)
	}
	sources, err := repo.ListStoryFactSources(ctx, story.FactEntity, "entity-accepted")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("the evidence round trip returned %+v", sources)
	}
	if sources[0].StartOffset != nil || sources[0].EndOffset != nil {
		t.Fatalf("evidence with no range gained one: %+v", sources[0])
	}

	// The conflict list, newest first, with the soft-delete-free status filter.
	// Each conflict needs a DISTINCT fact pair, because the schema's unique
	// constraint is over (left, right): the same two facts disagreeing twice is
	// one disagreement, which is the constraint doing its job rather than an
	// inconvenience. A third entity gives the three rows three different pairs.
	seedGraphEntity(t, db, ctx, "entity-third", "The Letter", "candidate")
	for _, seed := range []struct{ id, status, createdAt, right string }{
		{"conflict-a", "open", "2026-01-01T00:00:00Z", "entity-candidate"},
		{"conflict-b", "open", "2026-03-01T00:00:00Z", "entity-third"},
		{"conflict-c", "resolved", "2026-02-01T00:00:00Z", "entity-deleted"},
	} {
		statement := `INSERT INTO story_fact_conflicts
			(id, project_id, left_fact_type, left_fact_id, right_fact_type, right_fact_id, conflict_type, status, created_at)
			VALUES (?, 'drama-project', 'entity', 'entity-accepted', 'entity', ?, 'name', ?, ?)`
		if _, err := db.ExecContext(ctx, statement, seed.id, seed.right, seed.status, seed.createdAt); err != nil {
			t.Fatalf("seeding the conflict %s: %v", seed.id, err)
		}
	}
	conflicts, err := repo.ListStoryConflicts(ctx, "drama-project", "")
	if err != nil {
		t.Fatalf("ListStoryConflicts: %v", err)
	}
	if len(conflicts) != 3 {
		t.Fatalf("the conflict list returned %d rows, want 3", len(conflicts))
	}
	// Newest first: conflict-b was created in March and conflict-a in January.
	if conflicts[0].ID != "conflict-b" || conflicts[2].ID != "conflict-a" {
		t.Fatalf("the conflicts are not newest first: %s … %s", conflicts[0].ID, conflicts[2].ID)
	}
	open, err := repo.ListStoryConflicts(ctx, "drama-project", story.ConflictOpen)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("the open filter returned %d conflicts, want 2", len(open))
	}
	for _, conflict := range open {
		if conflict.Status != story.ConflictOpen {
			t.Fatalf("the open filter returned a %q conflict", conflict.Status)
		}
	}
}
