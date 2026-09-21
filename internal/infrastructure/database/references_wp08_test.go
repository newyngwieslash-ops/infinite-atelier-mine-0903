package database

import (
	"context"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// scriptSkeletonForTest builds a minimal skeleton version row for a link write.
func scriptSkeletonForTest(id string, number int) script.StorySkeletonVersion {
	return script.StorySkeletonVersion{
		ID: id, EpisodeID: "ep-links", VersionNumber: number, Status: versioning.StatusDraft,
		CreatedByType: versioning.CreatedByAgent, CreatedAt: structureTime,
	}
}

// scriptStrategyForTest builds a minimal strategy version row for a link write.
func scriptStrategyForTest(id string, number int) script.AdaptationStrategyVersion {
	return script.AdaptationStrategyVersion{
		ID: id, EpisodeID: "ep-links", VersionNumber: number, Status: versioning.StatusDraft,
		AdaptationMode: script.AdaptationBalanced, CreatedByType: versioning.CreatedByAgent,
		CreatedAt: structureTime,
	}
}

// references_wp08_test.go covers the ORDER and the BOUNDARY of the reference query, against real
// SQLite.
//
// The mutation pass reported two survivors here, and both were in this query's return value: writing
// the link ordinals as zero, and ignoring the project id. Neither is reachable from the service's
// in-memory double — it re-derives the ordinals from the slice it was handed and it keys its graph by
// project — so the double would have agreed with either defect. What makes the query right is the
// statement it runs, and this is where a statement is exercised.

// TestLinkOrdinalsAreTheSlicePositions covers §7.4's and §7.5's ordinals.
//
// The ordinal is what a reader sees as the order, which is the whole content of §7.5's "reordered": a
// link set written with ordinals of zero would still list the right events and would lose the only
// thing that makes their order mean anything.
func TestLinkOrdinalsAreTheSlicePositions(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	seedEventLinkParents(t, db)

	// The skeleton's selection: three events in a chosen order.
	if err := repo.CreateStorySkeletonVersionWithLinks(ctx, scriptSkeletonForTest("skeleton-order", 2),
		[]string{"event-3", "event-1", "event-2"}); err != nil {
		t.Fatalf("CreateStorySkeletonVersionWithLinks: %v", err)
	}
	ids, err := repo.ListSkeletonEventIDs(ctx, "skeleton-order")
	if err != nil {
		t.Fatalf("ListSkeletonEventIDs: %v", err)
	}
	// The order the caller wrote is the order the query returns, because the query sorts by ordinal —
	// so this assertion fails if the ordinal is constant.
	want := []string{"event-3", "event-1", "event-2"}
	if len(ids) != len(want) {
		t.Fatalf("the selection is %v, want %v", ids, want)
	}
	for index := range want {
		if ids[index] != want[index] {
			t.Fatalf("the selection is %v, want %v in the caller's order", ids, want)
		}
	}
	// The same for a strategy's treatments, which carry the ordinal AND a treatment.
	if err := repo.CreateAdaptationStrategyVersionWithLinks(ctx,
		scriptStrategyForTest("strategy-order", 2),
		[]script.StrategyEventLink{
			{StoryEventID: "event-3", Treatment: script.TreatmentReordered},
			{StoryEventID: "event-1", Treatment: script.TreatmentRetained},
			{StoryEventID: "event-2", Treatment: script.TreatmentRemoved},
		},
	); err != nil {
		t.Fatalf("CreateAdaptationStrategyVersionWithLinks: %v", err)
	}
	links, err := repo.ListStrategyEventLinks(ctx, "strategy-order")
	if err != nil {
		t.Fatalf("ListStrategyEventLinks: %v", err)
	}
	if len(links) != 3 {
		t.Fatalf("the treatments are %+v", links)
	}
	for index, link := range links {
		if link.Ordinal != index+1 {
			t.Fatalf("link %d has ordinal %d, want %d", index, link.Ordinal, index+1)
		}
	}
	if links[0].StoryEventID != "event-3" || links[2].StoryEventID != "event-2" {
		t.Fatalf("the treatments came back as %+v", links)
	}
}

// TestMissingStoryReferencesIsScopedToTheProject covers the boundary the query exists to enforce.
//
// The event table is per-project and the link tables have no foreign key to it, so the project
// predicate in this query is the ONLY thing preventing a script from citing an event of another
// project. A mutation replacing the parameter with a constant left the suite green, because the
// service's double keys its own graph and never runs this statement.
func TestMissingStoryReferencesIsScopedToTheProject(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	seedEventLinkParents(t, db)
	// A second project with its own event, and the same event id spelled in both — which is what
	// makes a missing project predicate visible rather than accidentally correct.
	if _, err := db.ExecContext(ctx, `INSERT INTO projects
		(id, workspace_id, project_type, name, language, status, created_at, updated_at, revision)
		VALUES ('other-drama', 'drama-ws', 'drama', 'Elsewhere', 'zh-CN', 'active', '`+structureStamp+`', '`+structureStamp+`', 1)`); err != nil {
		t.Fatalf("seeding the other project: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO story_events (id, project_id, ordinal, name, status, created_at, updated_at)
		 VALUES ('event-shared', 'drama-project', 8, 'Ours', 'candidate', '` + structureStamp + `', '` + structureStamp + `')`,
		`INSERT INTO story_events (id, project_id, ordinal, name, status, created_at, updated_at)
		 VALUES ('event-elsewhere', 'other-drama', 9, 'Theirs', 'candidate', '` + structureStamp + `', '` + structureStamp + `')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding events: %v", err)
		}
	}

	// Our own event is not missing.
	missing, err := repo.MissingStoryEventIDs(ctx, "drama-project", []string{"event-shared"})
	if err != nil {
		t.Fatalf("MissingStoryEventIDs: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("our own event came back missing: %v", missing)
	}
	// The other project's event IS missing when asked about from ours, even though the row exists.
	missing, err = repo.MissingStoryEventIDs(ctx, "drama-project", []string{"event-elsewhere"})
	if err != nil {
		t.Fatalf("MissingStoryEventIDs: %v", err)
	}
	if len(missing) != 1 || missing[0] != "event-elsewhere" {
		t.Fatalf("another project's event resolved for us: %v", missing)
	}
	// And it is NOT missing when asked about from its own project, so the predicate is a comparison
	// rather than a constant that happens to hide everything.
	missing, err = repo.MissingStoryEventIDs(ctx, "other-drama", []string{"event-elsewhere"})
	if err != nil {
		t.Fatalf("MissingStoryEventIDs: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("its own event came back missing: %v", missing)
	}
	// The entity query is the same statement over another table, and it is asserted the SAME WAY as the
	// event query — with an entity of OUR project that must NOT come back missing.
	//
	// The first version asked only about another project's entity, so a query with the project id
	// hard-coded to the literal this test passes stayed GREEN: the mutant and the correct code gave the
	// same answer for the only input the test used. A mutation run found it, which is why both directions
	// are asserted now — the same shape the event query above already had.
	if _, err := db.ExecContext(ctx, `INSERT INTO story_entities
		(id, project_id, entity_type, canonical_name, status, created_at, updated_at)
		VALUES ('entity-ours', 'drama-project', 'character', 'Ours', 'accepted', '`+structureStamp+`', '`+structureStamp+`')`); err != nil {
		t.Fatalf("seeding our entity: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO story_entities
		(id, project_id, entity_type, canonical_name, status, created_at, updated_at)
		VALUES ('entity-elsewhere', 'other-drama', 'character', 'Theirs', 'accepted', '`+structureStamp+`', '`+structureStamp+`')`); err != nil {
		t.Fatalf("seeding the other project's entity: %v", err)
	}
	// Our own entity is NOT missing.
	missingEntities, err := repo.MissingStoryEntityIDs(ctx, "drama-project", []string{"entity-ours"})
	if err != nil {
		t.Fatalf("MissingStoryEntityIDs: %v", err)
	}
	if len(missingEntities) != 0 {
		t.Fatalf("our own entity came back missing: %v", missingEntities)
	}
	// The other project's is, asked about from ours.
	missingEntities, err = repo.MissingStoryEntityIDs(ctx, "drama-project", []string{"entity-elsewhere"})
	if err != nil {
		t.Fatalf("MissingStoryEntityIDs: %v", err)
	}
	if len(missingEntities) != 1 {
		t.Fatalf("another project's entity resolved for us: %v", missingEntities)
	}
	// And it is NOT missing when its own project asks, so the predicate is a comparison rather than a
	// constant that hides everything.
	missingEntities, err = repo.MissingStoryEntityIDs(ctx, "other-drama", []string{"entity-elsewhere"})
	if err != nil {
		t.Fatalf("MissingStoryEntityIDs: %v", err)
	}
	if len(missingEntities) != 0 {
		t.Fatalf("its own entity came back missing: %v", missingEntities)
	}
}

// TestMissingReferencesComeBackInTheCallersOrder covers the refusal's readability.
//
// The query returns the ids that are absent, and the service puts them in a message a model reads. A
// sorted list would name the same identifiers in an order the payload did not use, which is a small
// thing until a model has to match the message against what it wrote.
func TestMissingReferencesComeBackInTheCallersOrder(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	seedEventLinkParents(t, db)
	// The caller's order is deliberately neither sorted ascending nor descending, so a mutation that
	// sorts the result is visible.
	missing, err := repo.MissingStoryEventIDs(ctx, "drama-project",
		[]string{"zulu", "alpha", "mike"})
	if err != nil {
		t.Fatalf("MissingStoryEventIDs: %v", err)
	}
	want := []string{"zulu", "alpha", "mike"}
	if len(missing) != len(want) {
		t.Fatalf("the missing events are %v, want %v", missing, want)
	}
	for index := range want {
		if missing[index] != want[index] {
			t.Fatalf("the missing events are %v, want %v in the caller's order", missing, want)
		}
	}
	// A duplicate in the caller's list is collapsed rather than reported twice: the message names
	// each absent identifier once, which is what makes it readable.
	missing, err = repo.MissingStoryEventIDs(ctx, "drama-project", []string{"ghost", "ghost", "alpha"})
	if err != nil {
		t.Fatalf("MissingStoryEventIDs: %v", err)
	}
	if len(missing) != 2 || missing[0] != "ghost" || missing[1] != "alpha" {
		t.Fatalf("a repeated identifier came back as %v", missing)
	}
}
