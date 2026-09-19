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

// seedChapterRange writes one chapter with an explicit range, so a split or a
// merge has something concrete to move.
func seedChapterRange(t *testing.T, db querier, ctx context.Context, id string, ordinal, start, end int) {
	t.Helper()
	statement := `INSERT INTO chapters (id, source_document_version_id, ordinal, title, start_offset, end_offset,
		source_kind, status, created_at, updated_at, revision)
		VALUES (?, 'drama-document-version', ?, ?, ?, ?, 'regex', 'confirmed', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`
	if _, err := db.ExecContext(ctx, statement, id, ordinal, "Chapter "+itoaTest(ordinal), start, end); err != nil {
		t.Fatalf("seeding the chapter %s: %v", id, err)
	}
}

// readOrdinals returns a version's chapters as ordinal/range triples, which is
// what a split and a merge are judged against.
func readOrdinals(t *testing.T, repo *StoryRepository, ctx context.Context) []story.Chapter {
	t.Helper()
	chapters, err := repo.ListChapters(ctx, "drama-document-version")
	if err != nil {
		t.Fatalf("ListChapters: %v", err)
	}
	return chapters
}

// TestSplitChapterRenumbersAgainstTheDatabase covers the unique constraint the
// renumbering has to satisfy.
//
// The schema's UNIQUE (source_document_version_id, ordinal) is what makes this one
// transaction rather than three statements, and it is also what makes the ORDER of
// the shift load-bearing: moving the chapters after the split from the highest
// ordinal down never collides, and moving them upward collides immediately. This
// test is the one that would catch a wrong order.
func TestSplitChapterRenumbersAgainstTheDatabase(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	// The seed creates the version and a chapter at ordinal 1; both split tests
	// need their own ranges, so the seed's chapter is replaced.
	dramaSeedChapter(t, db, "chapter-seed")
	ctx := context.Background()
	repo := NewStoryRepository(db)
	if _, err := db.ExecContext(ctx, `DELETE FROM chapters WHERE id = 'chapter-seed'`); err != nil {
		t.Fatal(err)
	}
	// Three chapters including the one being split, so the shift has to move more
	// than one row.
	seedChapterRange(t, db, ctx, "chapter-one", 1, 0, 100)
	seedChapterRange(t, db, ctx, "chapter-two", 2, 100, 200)
	seedChapterRange(t, db, ctx, "chapter-three", 3, 200, 300)

	first := story.Chapter{
		ID: "chapter-one", SourceDocumentVersionID: "drama-document-version",
		Ordinal: 1, Title: "Chapter 1", StartOffset: 0, EndOffset: 40,
		SourceKind: story.ChapterManual, Status: story.ChapterEdited,
		Revision: 1, UpdatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	second := story.Chapter{
		ID: "chapter-one-b", SourceDocumentVersionID: "drama-document-version",
		Ordinal: 2, Title: "Second half", StartOffset: 40, EndOffset: 100,
		SourceKind: story.ChapterManual, Status: story.ChapterEdited,
		CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Revision: 1,
	}
	if err := repo.SplitChapter(ctx, first, second, 1); err != nil {
		t.Fatalf("SplitChapter: %v", err)
	}

	chapters := readOrdinals(t, repo, ctx)
	if len(chapters) != 4 {
		t.Fatalf("the version has %d chapters after a split, want 4", len(chapters))
	}
	// The ordinals must be 1..4 with no hole and no duplicate.
	for index, chapter := range chapters {
		if chapter.Ordinal != index+1 {
			t.Fatalf("position %d has ordinal %d, so the renumbering left a hole: %+v",
				index, chapter.Ordinal, chapters)
		}
	}
	// And the ranges must still tile: a split does not change what the document
	// covers, so every chapter must begin where the previous one ended.
	for index := 1; index < len(chapters); index++ {
		if chapters[index].StartOffset != chapters[index-1].EndOffset {
			t.Fatalf("the split left a gap or an overlap: chapter %d ends at %d and %d starts at %d",
				index-1, chapters[index-1].EndOffset, index, chapters[index].StartOffset)
		}
	}
	// The two halves cover exactly what the one chapter did.
	if chapters[0].StartOffset != 0 || chapters[0].EndOffset != 40 {
		t.Fatalf("the first half is %d..%d, want 0..40", chapters[0].StartOffset, chapters[0].EndOffset)
	}
	if chapters[1].StartOffset != 40 || chapters[1].EndOffset != 100 {
		t.Fatalf("the second half is %d..%d, want 40..100", chapters[1].StartOffset, chapters[1].EndOffset)
	}
	// The shifted chapters kept their own ranges, so only their ordinals moved.
	if chapters[2].EndOffset != 200 || chapters[3].EndOffset != 300 {
		t.Fatalf("a shifted chapter's range changed: %+v", chapters[2:])
	}
}

// TestMergeChapterClosesTheGapAgainstTheDatabase covers the other direction: the
// absorbed row is removed and the rows after it move UP, where a wrong order
// collides just as it does in a split.
func TestMergeChapterClosesTheGapAgainstTheDatabase(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	dramaSeedChapter(t, db, "chapter-seed")
	ctx := context.Background()
	repo := NewStoryRepository(db)
	if _, err := db.ExecContext(ctx, `DELETE FROM chapters WHERE id = 'chapter-seed'`); err != nil {
		t.Fatal(err)
	}
	seedChapterRange(t, db, ctx, "chapter-one", 1, 0, 100)
	seedChapterRange(t, db, ctx, "chapter-two", 2, 100, 200)
	seedChapterRange(t, db, ctx, "chapter-three", 3, 200, 300)
	seedChapterRange(t, db, ctx, "chapter-four", 4, 300, 400)

	merged := story.Chapter{
		ID: "chapter-one", SourceDocumentVersionID: "drama-document-version",
		Ordinal: 1, Title: "Chapter 1", StartOffset: 0, EndOffset: 200,
		SourceKind: story.ChapterManual, Status: story.ChapterEdited,
		Revision: 1, UpdatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := repo.MergeChapters(ctx, merged, "chapter-two", 1); err != nil {
		t.Fatalf("MergeChapters: %v", err)
	}
	chapters := readOrdinals(t, repo, ctx)
	if len(chapters) != 3 {
		t.Fatalf("the version has %d chapters after a merge, want 3", len(chapters))
	}
	for index, chapter := range chapters {
		if chapter.Ordinal != index+1 {
			t.Fatalf("ordinal %d at position %d, so the renumbering left a hole", chapter.Ordinal, index)
		}
	}
	// The absorbed chapter is gone, and the survivor covers both ranges.
	for _, chapter := range chapters {
		if chapter.ID == "chapter-two" {
			t.Fatal("the absorbed chapter is still present")
		}
	}
	if chapters[0].StartOffset != 0 || chapters[0].EndOffset != 200 {
		t.Fatalf("the merged chapter is %d..%d, want 0..200", chapters[0].StartOffset, chapters[0].EndOffset)
	}
	// The text still tiles after the merge.
	for index := 1; index < len(chapters); index++ {
		if chapters[index].StartOffset != chapters[index-1].EndOffset {
			t.Fatalf("the merge left a gap: %d ends at %d, %d starts at %d",
				index-1, chapters[index-1].EndOffset, index, chapters[index].StartOffset)
		}
	}
}
