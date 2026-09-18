package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	storyapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// openWP05StoryRepo opens the full WP-05 migration set over a temporary
// database, seeds the project the story rows hang off, and returns the story
// repository with the raw handle for tests that read a row directly.
//
// The fixture is the production open path, so the constraints, the foreign keys
// and the partial indexes under test are the ones a user's database has.
func openWP05StoryRepo(t *testing.T) (*StoryRepository, *sql.DB) {
	t.Helper()
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"),
		filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if handle.Mode() != ModeReady {
		t.Fatalf("database not ready: %v", handle.Err())
	}
	db := handle.SQL()
	seedWP05Parents(t, db)
	return NewStoryRepository(db), db
}

// sampleSourceDocument builds a document with every field set, so a round trip
// that drops one is visible.
func sampleSourceDocument(t *testing.T) story.SourceDocument {
	t.Helper()
	now := fixedClock()()
	return story.SourceDocument{
		ID:        "doc-1",
		ProjectID: "project-1",
		Type:      story.DocumentNovel,
		Name:      "The Long Winter",
		Status:    story.DocumentActive,
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
	}
}

// sampleSourceDocumentVersion builds one import of that document.
func sampleSourceDocumentVersion(t *testing.T) story.SourceDocumentVersion {
	t.Helper()
	return story.SourceDocumentVersion{
		ID:                   "doc-version-1",
		SourceDocumentID:     "doc-1",
		VersionNumber:        1,
		PhysicalFileID:       testHashA,
		NormalizedTextFileID: testHashB,
		ContentHash:          testHashC,
		MIMEType:             "text/plain",
		Encoding:             "utf-8",
		CharCount:            4096,
		ImportMetadataJSON:   `{"parser":"plain"}`,
		CreatedByType:        story.CreatedByUser,
		CreatedAt:            fixedClock()(),
	}
}

// TestStoryRepositorySourceDocumentRoundTrip covers create, read and list, and
// checks that the document's version pointer starts empty and survives an
// update.
func TestStoryRepositorySourceDocumentRoundTrip(t *testing.T) {
	repo, _ := openWP05StoryRepo(t)
	ctx := context.Background()
	document := sampleSourceDocument(t)

	if err := repo.CreateSourceDocument(ctx, document); err != nil {
		t.Fatalf("CreateSourceDocument: %v", err)
	}
	loaded, err := repo.GetSourceDocument(ctx, document.ID)
	if err != nil {
		t.Fatalf("GetSourceDocument: %v", err)
	}
	if loaded.ID != document.ID || loaded.ProjectID != document.ProjectID || loaded.Type != story.DocumentNovel {
		t.Fatalf("round trip changed the row: %+v", loaded)
	}
	if loaded.Name != document.Name || loaded.Status != story.DocumentActive || loaded.Revision != 1 {
		t.Fatalf("round trip changed the row: %+v", loaded)
	}
	if !loaded.CreatedAt.Equal(document.CreatedAt) || !loaded.UpdatedAt.Equal(document.UpdatedAt) {
		t.Fatalf("timestamps round-tripped as %s / %s", loaded.CreatedAt, loaded.UpdatedAt)
	}
	if loaded.CurrentVersionID != "" {
		t.Fatalf("a document with no import names a version: %q", loaded.CurrentVersionID)
	}

	// The version pointer is what AddSourceDocumentVersion advances; this is the
	// repository's half of that write.
	document.CurrentVersionID = "doc-version-1"
	document.UpdatedAt = fixedClock()()
	if err := repo.UpdateSourceDocument(ctx, document, 1); err != nil {
		t.Fatalf("UpdateSourceDocument: %v", err)
	}
	advanced, err := repo.GetSourceDocument(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.CurrentVersionID != "doc-version-1" || advanced.Revision != 2 {
		t.Fatalf("pointer write lost: %+v", advanced)
	}

	// A second document in the same project is listed beside the first.
	second := sampleSourceDocument(t)
	second.ID = "doc-2"
	second.Name = "Second"
	if err := repo.CreateSourceDocument(ctx, second); err != nil {
		t.Fatal(err)
	}
	list, err := repo.ListSourceDocuments(ctx, "project-1")
	if err != nil {
		t.Fatalf("ListSourceDocuments: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListSourceDocuments returned %d rows, want 2", len(list))
	}
	// A soft-deleted document is hidden by the list query.
	second.Status = story.DocumentTrashed
	second.DeletedAt = fixedClock()()
	second.UpdatedAt = fixedClock()()
	if err := repo.UpdateSourceDocument(ctx, second, 1); err != nil {
		t.Fatal(err)
	}
	list, err = repo.ListSourceDocuments(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("a trashed document is still listed (%d rows)", len(list))
	}
}

// TestStoryRepositoryVersionAndChapterRoundTrip covers the document version
// (read back through the repository's own column list) and the chapter.
func TestStoryRepositoryVersionAndChapterRoundTrip(t *testing.T) {
	repo, db := openWP05StoryRepo(t)
	ctx := context.Background()
	if err := repo.CreateSourceDocument(ctx, sampleSourceDocument(t)); err != nil {
		t.Fatal(err)
	}
	version := sampleSourceDocumentVersion(t)
	if err := repo.CreateSourceDocumentVersion(ctx, version); err != nil {
		t.Fatalf("CreateSourceDocumentVersion: %v", err)
	}

	// There is no version read port: the version is an import record the story
	// graph only ever counts. Reading it back with the repository's own select
	// list is what proves every column was written and maps back.
	row := db.QueryRowContext(ctx, sourceDocumentVersionSelectColumns+` WHERE id = ?`, version.ID)
	var loaded story.SourceDocumentVersion
	var createdByType, createdAt string
	if err := row.Scan(&loaded.ID, &loaded.SourceDocumentID, &loaded.VersionNumber, &loaded.PhysicalFileID,
		&loaded.NormalizedTextFileID, &loaded.ContentHash, &loaded.MIMEType, &loaded.Encoding,
		&loaded.CharCount, &loaded.ImportMetadataJSON, &createdByType, &createdAt); err != nil {
		t.Fatalf("reading the version back: %v", err)
	}
	loaded.CreatedByType = story.CreatedByType(createdByType)
	loaded.CreatedAt = parseTime(createdAt)
	if loaded.PhysicalFileID != testHashA || loaded.NormalizedTextFileID != testHashB || loaded.ContentHash != testHashC {
		t.Fatalf("file references round-tripped wrong: %+v", loaded)
	}
	if loaded.MIMEType != "text/plain" || loaded.Encoding != "utf-8" || loaded.CharCount != 4096 {
		t.Fatalf("version metadata round-tripped wrong: %+v", loaded)
	}
	if loaded.ImportMetadataJSON != `{"parser":"plain"}` || loaded.CreatedByType != story.CreatedByUser {
		t.Fatalf("version provenance round-tripped wrong: %+v", loaded)
	}
	if !loaded.CreatedAt.Equal(version.CreatedAt) {
		t.Fatalf("created_at = %s, want %s", loaded.CreatedAt, version.CreatedAt)
	}

	highest, err := repo.MaxSourceDocumentVersionNumber(ctx, version.SourceDocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if highest != 1 {
		t.Fatalf("MaxSourceDocumentVersionNumber = %d, want 1", highest)
	}
	if highest, err = repo.MaxSourceDocumentVersionNumber(ctx, "doc-with-no-versions"); err != nil {
		t.Fatal(err)
	} else if highest != 0 {
		t.Fatalf("a document with no versions reports %d, want 0", highest)
	}

	chapter := story.Chapter{
		ID:                      "chapter-1",
		SourceDocumentVersionID: version.ID,
		Ordinal:                 1,
		Title:                   "One",
		StartOffset:             0,
		EndOffset:               1000,
		ContentHash:             testHashC,
		Status:                  story.ChapterDetected,
		CreatedAt:               fixedClock()(),
		UpdatedAt:               fixedClock()(),
		Revision:                1,
	}
	if err := repo.CreateChapter(ctx, chapter); err != nil {
		t.Fatalf("CreateChapter: %v", err)
	}
	loadedChapter, err := repo.GetChapter(ctx, chapter.ID)
	if err != nil {
		t.Fatalf("GetChapter: %v", err)
	}
	if loadedChapter.Ordinal != 1 || loadedChapter.StartOffset != 0 || loadedChapter.EndOffset != 1000 {
		t.Fatalf("chapter offsets round-tripped wrong: %+v", loadedChapter)
	}
	if loadedChapter.ContentHash != testHashC || loadedChapter.Status != story.ChapterDetected {
		t.Fatalf("chapter metadata round-tripped wrong: %+v", loadedChapter)
	}
	chapters, err := repo.ListChapters(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 1 {
		t.Fatalf("ListChapters returned %d rows, want 1", len(chapters))
	}
	// A chapter whose slice is empty is allowed: end_offset >= start_offset is
	// the schema's rule and this is its boundary case.
	empty := chapter
	empty.ID = "chapter-2"
	empty.Ordinal = 2
	empty.StartOffset = 1000
	empty.EndOffset = 1000
	empty.Status = story.ChapterConfirmed
	if err := repo.CreateChapter(ctx, empty); err != nil {
		t.Fatalf("an empty chapter was refused: %v", err)
	}

	foreignKeysClean(t, db)
}

// TestStoryRepositoryFactLayerRoundTrip covers entities, events, relations and
// conflicts, including the nullable story_time_order column.
func TestStoryRepositoryFactLayerRoundTrip(t *testing.T) {
	repo, db := openWP05StoryRepo(t)
	ctx := context.Background()
	now := fixedClock()()

	entity := story.StoryEntity{
		ID: "entity-1", ProjectID: "project-1", Type: story.EntityCharacter,
		CanonicalName: "Mira", Status: story.FactCandidate, SourceScope: story.ScopeOriginal,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := repo.CreateStoryEntity(ctx, entity); err != nil {
		t.Fatalf("CreateStoryEntity: %v", err)
	}
	loadedEntity, err := repo.GetStoryEntity(ctx, entity.ID)
	if err != nil {
		t.Fatalf("GetStoryEntity: %v", err)
	}
	if loadedEntity.CanonicalName != "Mira" || loadedEntity.Type != story.EntityCharacter {
		t.Fatalf("entity round-tripped wrong: %+v", loadedEntity)
	}
	if loadedEntity.Status != story.FactCandidate || loadedEntity.SourceScope != story.ScopeOriginal {
		t.Fatalf("entity vocabulary round-tripped wrong: %+v", loadedEntity)
	}

	order := 3
	event := story.StoryEvent{
		ID: "event-1", ProjectID: "project-1", Ordinal: 2, Name: "The letter arrives",
		Description: "A letter reaches the harbour.", EventType: "inciting",
		StoryTimeText: "three days later", StoryTimeOrder: &order, LocationEntityID: "entity-1",
		CauseSummary: "The sender wrote it.", ResultSummary: "She must answer.",
		Importance: "high", Confidence: 0.75, Status: story.FactCandidate,
		SourceScope: story.ScopeOriginal, CreatedByAgentRunID: "run-1",
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := repo.CreateStoryEvent(ctx, event); err != nil {
		t.Fatalf("CreateStoryEvent: %v", err)
	}
	loadedEvent, err := repo.GetStoryEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("GetStoryEvent: %v", err)
	}
	if loadedEvent.Name != event.Name || loadedEvent.Ordinal != 2 || loadedEvent.EventType != "inciting" {
		t.Fatalf("event round-tripped wrong: %+v", loadedEvent)
	}
	if loadedEvent.StoryTimeOrder == nil || *loadedEvent.StoryTimeOrder != 3 {
		t.Fatalf("story_time_order = %v, want 3", loadedEvent.StoryTimeOrder)
	}
	if loadedEvent.Confidence != 0.75 || loadedEvent.CreatedByAgentRunID != "run-1" {
		t.Fatalf("event provenance round-tripped wrong: %+v", loadedEvent)
	}
	// An event with no computed story position keeps nil rather than zero, which
	// is the nullable column's whole point.
	unordered := event
	unordered.ID = "event-2"
	unordered.Name = "The reply"
	unordered.StoryTimeOrder = nil
	if err := repo.CreateStoryEvent(ctx, unordered); err != nil {
		t.Fatal(err)
	}
	loadedUnordered, err := repo.GetStoryEvent(ctx, unordered.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedUnordered.StoryTimeOrder != nil {
		t.Fatalf("an unset story_time_order read back as %d", *loadedUnordered.StoryTimeOrder)
	}

	relation := story.StoryRelation{
		ID: "relation-1", ProjectID: "project-1", Type: story.RelationKnows,
		SourceEntityType: "character", SourceEntityID: "entity-1",
		TargetEntityType: "character", TargetEntityID: "entity-2",
		ValidFromEventID: "event-1", Confidence: 0.5, Status: story.FactCandidate,
		SourceScope: story.ScopeOriginal, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := repo.CreateStoryRelation(ctx, relation); err != nil {
		t.Fatalf("CreateStoryRelation: %v", err)
	}
	loadedRelation, err := repo.GetStoryRelation(ctx, relation.ID)
	if err != nil {
		t.Fatalf("GetStoryRelation: %v", err)
	}
	if loadedRelation.Type != story.RelationKnows || loadedRelation.TargetEntityID != "entity-2" {
		t.Fatalf("relation round-tripped wrong: %+v", loadedRelation)
	}
	if loadedRelation.ValidFromEventID != "event-1" || loadedRelation.Confidence != 0.5 {
		t.Fatalf("relation bounds round-tripped wrong: %+v", loadedRelation)
	}

	conflict := story.StoryFactConflict{
		ID: "conflict-1", ProjectID: "project-1",
		LeftFactType: story.FactEntity, LeftFactID: "entity-1",
		RightFactType: story.FactEntity, RightFactID: "entity-2",
		ConflictType: "name_clash", Status: story.ConflictOpen, CreatedAt: now,
	}
	if err := repo.CreateStoryConflict(ctx, conflict); err != nil {
		t.Fatalf("CreateStoryConflict: %v", err)
	}
	loadedConflict, err := repo.GetStoryConflict(ctx, conflict.ID)
	if err != nil {
		t.Fatalf("GetStoryConflict: %v", err)
	}
	if loadedConflict.LeftFactID != "entity-1" || loadedConflict.RightFactType != story.FactEntity {
		t.Fatalf("conflict round-tripped wrong: %+v", loadedConflict)
	}
	if loadedConflict.Status != story.ConflictOpen || !loadedConflict.ResolvedAt.IsZero() {
		t.Fatalf("an open conflict is not open: %+v", loadedConflict)
	}

	// A conflict closes under the status guard, and the second attempt is
	// refused because the row no longer matches the status that was read.
	conflict.Status = story.ConflictResolved
	conflict.Resolution = "The longer name is canonical."
	conflict.ResolvedBy = "user-1"
	conflict.ResolvedAt = now
	if err := repo.UpdateStoryConflict(ctx, conflict, story.ConflictOpen); err != nil {
		t.Fatalf("UpdateStoryConflict: %v", err)
	}
	resolved, err := repo.GetStoryConflict(ctx, conflict.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != story.ConflictResolved || resolved.ResolvedBy != "user-1" {
		t.Fatalf("resolution round-tripped wrong: %+v", resolved)
	}
	if !resolved.ResolvedAt.Equal(now) {
		t.Fatalf("resolved_at = %s, want %s", resolved.ResolvedAt, now)
	}
	if err := repo.UpdateStoryConflict(ctx, conflict, story.ConflictOpen); err == nil {
		t.Fatal("a conflict was closed twice against the same expected status")
	} else if domainErr, ok := story.AsError(err); !ok || domainErr.Category != story.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}

	foreignKeysClean(t, db)
}

// TestStoryRepositoryUpdateGuardsRevision proves the compare-and-swap: a stale
// revision is a conflict and the stored row is untouched.
func TestStoryRepositoryUpdateGuardsRevision(t *testing.T) {
	repo, _ := openWP05StoryRepo(t)
	ctx := context.Background()
	now := fixedClock()()
	entity := story.StoryEntity{
		ID: "entity-1", ProjectID: "project-1", Type: story.EntityCharacter,
		CanonicalName: "Mira", Status: story.FactCandidate, SourceScope: story.ScopeOriginal,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := repo.CreateStoryEntity(ctx, entity); err != nil {
		t.Fatal(err)
	}

	entity.Status = story.FactAccepted
	entity.UpdatedAt = now
	if err := repo.UpdateStoryEntity(ctx, entity, 1); err != nil {
		t.Fatalf("first update failed: %v", err)
	}
	after, err := repo.GetStoryEntity(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != 2 || after.Status != story.FactAccepted {
		t.Fatalf("after one update: %+v", after)
	}

	stale := after
	stale.Status = story.FactRejected
	if err := repo.UpdateStoryEntity(ctx, stale, 1); err == nil {
		t.Fatal("a stale revision was accepted")
	} else if domainErr, ok := story.AsError(err); !ok || domainErr.Category != story.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	reRead, err := repo.GetStoryEntity(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reRead.Status != story.FactAccepted {
		t.Fatalf("a rejected update wrote the row: %q", reRead.Status)
	}

	// A chapter's document version is a foreign key, so a version that was never
	// stored cannot be pointed at from a chapter row.
	chapter := story.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: "version-x", Ordinal: 1,
		StartOffset: 0, EndOffset: 10, Status: story.ChapterDetected,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := repo.CreateChapter(ctx, chapter); err == nil {
		t.Fatal("a chapter was stored against a document version that does not exist")
	}
}

// TestStoryRepositoryChapterAndEventUpdatesRoundTrip covers the two guarded
// writes the other fact-layer tests do not reach: a chapter correction and an
// event's state change.
func TestStoryRepositoryChapterAndEventUpdatesRoundTrip(t *testing.T) {
	repo, db := openWP05StoryRepo(t)
	ctx := context.Background()
	now := fixedClock()()
	if err := repo.CreateSourceDocument(ctx, sampleSourceDocument(t)); err != nil {
		t.Fatal(err)
	}
	version := sampleSourceDocumentVersion(t)
	if err := repo.CreateSourceDocumentVersion(ctx, version); err != nil {
		t.Fatal(err)
	}

	chapter := story.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: version.ID, Ordinal: 1, Title: "One",
		StartOffset: 0, EndOffset: 100, Status: story.ChapterDetected,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := repo.CreateChapter(ctx, chapter); err != nil {
		t.Fatal(err)
	}
	chapter.EndOffset = 140
	chapter.Title = "One, corrected"
	chapter.Status = story.ChapterEdited
	if err := repo.UpdateChapter(ctx, chapter, 1); err != nil {
		t.Fatalf("UpdateChapter: %v", err)
	}
	after, err := repo.GetChapter(ctx, chapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.EndOffset != 140 || after.Title != "One, corrected" || after.Status != story.ChapterEdited {
		t.Fatalf("the correction did not land: %+v", after)
	}
	if after.Revision != 2 {
		t.Fatalf("revision = %d, want 2", after.Revision)
	}
	if err := repo.UpdateChapter(ctx, chapter, 1); err == nil {
		t.Fatal("a stale chapter revision was accepted")
	} else if domainErr, ok := story.AsError(err); !ok || domainErr.Category != story.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}

	event := story.StoryEvent{
		ID: "event-1", ProjectID: "project-1", Ordinal: 1, Name: "The letter arrives",
		Status: story.FactCandidate, SourceScope: story.ScopeOriginal,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := repo.CreateStoryEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	event.Status = story.FactAccepted
	event.Description = "Accepted after review."
	if err := repo.UpdateStoryEvent(ctx, event, 1); err != nil {
		t.Fatalf("UpdateStoryEvent: %v", err)
	}
	afterEvent, err := repo.GetStoryEvent(ctx, event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterEvent.Status != story.FactAccepted || afterEvent.Description != "Accepted after review." {
		t.Fatalf("the decision did not land: %+v", afterEvent)
	}
	if afterEvent.Revision != 2 {
		t.Fatalf("revision = %d, want 2", afterEvent.Revision)
	}
	if err := repo.UpdateStoryEvent(ctx, event, 1); err == nil {
		t.Fatal("a stale event revision was accepted")
	} else if domainErr, ok := story.AsError(err); !ok || domainErr.Category != story.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	reRead, err := repo.GetStoryEvent(ctx, event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reRead.Status != story.FactAccepted {
		t.Fatalf("a refused update wrote the row: %q", reRead.Status)
	}

	foreignKeysClean(t, db)
}

// TestStoryRepositoryUniqueViolationIsAConflict proves each unique constraint
// this layer relies on is reported as a domain conflict rather than a raw
// constraint failure.
func TestStoryRepositoryUniqueViolationIsAConflict(t *testing.T) {
	repo, db := openWP05StoryRepo(t)
	ctx := context.Background()
	document := sampleSourceDocument(t)
	if err := repo.CreateSourceDocument(ctx, document); err != nil {
		t.Fatal(err)
	}
	duplicate := document
	duplicate.Name = "A different name"
	err := repo.CreateSourceDocument(ctx, duplicate)
	if err == nil {
		t.Fatal("a duplicate document id was accepted")
	}
	if domainErr, ok := story.AsError(err); !ok || domainErr.Category != story.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM source_documents"); got != 1 {
		t.Fatalf("the duplicate created %d rows", got)
	}

	// The (document, version number) pair is the other unique constraint.
	version := sampleSourceDocumentVersion(t)
	if err := repo.CreateSourceDocumentVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
	duplicateVersion := version
	duplicateVersion.ID = "doc-version-2"
	err = repo.CreateSourceDocumentVersion(ctx, duplicateVersion)
	if err == nil {
		t.Fatal("a reused version number was accepted")
	}
	if domainErr, ok := story.AsError(err); !ok || domainErr.Category != story.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}

	// And the (version, ordinal) pair on chapters.
	chapter := story.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: version.ID, Ordinal: 1,
		StartOffset: 0, EndOffset: 10, Status: story.ChapterDetected,
		CreatedAt: fixedClock()(), UpdatedAt: fixedClock()(), Revision: 1,
	}
	if err := repo.CreateChapter(ctx, chapter); err != nil {
		t.Fatal(err)
	}
	clashing := chapter
	clashing.ID = "chapter-2"
	if err := repo.CreateChapter(ctx, clashing); err == nil {
		t.Fatal("a reused chapter ordinal was accepted")
	} else if domainErr, ok := story.AsError(err); !ok || domainErr.Category != story.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}

	// The ordered fact pair is the conflict table's unique constraint.
	conflict := story.StoryFactConflict{
		ID: "conflict-1", ProjectID: "project-1",
		LeftFactType: story.FactEntity, LeftFactID: "entity-1",
		RightFactType: story.FactEntity, RightFactID: "entity-2",
		Status: story.ConflictOpen, CreatedAt: fixedClock()(),
	}
	if err := repo.CreateStoryConflict(ctx, conflict); err != nil {
		t.Fatal(err)
	}
	samePair := conflict
	samePair.ID = "conflict-2"
	if err := repo.CreateStoryConflict(ctx, samePair); err == nil {
		t.Fatal("the same fact pair was recorded twice")
	} else if domainErr, ok := story.AsError(err); !ok || domainErr.Category != story.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	// The reverse order is a different row, which is what the schema's ordered
	// unique constraint means.
	reversed := conflict
	reversed.ID = "conflict-3"
	reversed.LeftFactID, reversed.RightFactID = conflict.RightFactID, conflict.LeftFactID
	if err := repo.CreateStoryConflict(ctx, reversed); err != nil {
		t.Fatalf("the reversed pair was refused: %v", err)
	}
}

// TestStoryRepositoryMissingRowIsNotFound proves a missing row maps to the
// domain's not-found category rather than a raw SQL error.
func TestStoryRepositoryMissingRowIsNotFound(t *testing.T) {
	repo, _ := openWP05StoryRepo(t)
	ctx := context.Background()
	const missing = "00000000-0000-7000-8000-00000000dead"
	cases := []struct {
		name string
		read func() error
	}{
		{"document", func() error { _, err := repo.GetSourceDocument(ctx, missing); return err }},
		{"chapter", func() error { _, err := repo.GetChapter(ctx, missing); return err }},
		{"entity", func() error { _, err := repo.GetStoryEntity(ctx, missing); return err }},
		{"event", func() error { _, err := repo.GetStoryEvent(ctx, missing); return err }},
		{"relation", func() error { _, err := repo.GetStoryRelation(ctx, missing); return err }},
		{"conflict", func() error { _, err := repo.GetStoryConflict(ctx, missing); return err }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.read()
			if err == nil {
				t.Fatalf("reading a missing %s succeeded", testCase.name)
			}
			domainErr, ok := story.AsError(err)
			if !ok || domainErr.Category != story.CategoryNotFound {
				t.Fatalf("expected not_found, got %v", err)
			}
		})
	}
}

// TestStoryRepositoryWithinTxSharesOneTransaction proves the transaction-bound
// copy writes through the transaction and that a rollback undoes what it wrote.
func TestStoryRepositoryWithinTxSharesOneTransaction(t *testing.T) {
	repo, db := openWP05StoryRepo(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	bound := repo.WithinTx(tx)
	if bound.conn() == nil {
		t.Fatal("WithinTx produced a repository with no connection")
	}
	document := sampleSourceDocument(t)
	document.ID = "doc-in-tx"
	if err := bound.CreateSourceDocument(ctx, document); err != nil {
		t.Fatalf("the transaction-bound repository could not write: %v", err)
	}
	// The row is visible inside the transaction and not outside it.
	if _, err := bound.GetSourceDocument(ctx, document.ID); err != nil {
		t.Fatalf("the row is not readable inside its own transaction: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetSourceDocument(ctx, document.ID); err == nil {
		t.Fatal("a rolled-back write is visible to the unattached repository")
	}

	// A committed transaction leaves the row behind, so the port is usable for
	// the multi-table writes the import path performs.
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	bound = repo.WithinTx(tx)
	if err := bound.CreateSourceDocument(ctx, document); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetSourceDocument(ctx, document.ID); err != nil {
		t.Fatalf("a committed write is missing: %v", err)
	}
}

// TestStoryRepositoryUnattachedFailsClosed proves a repository with no
// connection reports the store as unavailable rather than panicking, for both a
// read and a write.
func TestStoryRepositoryUnattachedFailsClosed(t *testing.T) {
	repo := NewStoryRepository(nil)
	ctx := context.Background()
	if _, err := repo.GetSourceDocument(ctx, "doc-1"); err == nil {
		t.Fatal("an unattached repository read a row")
	}
	if err := repo.CreateSourceDocument(ctx, sampleSourceDocument(t)); err == nil {
		t.Fatal("an unattached repository wrote a row")
	}
	if _, err := repo.GetStoryConflict(ctx, "conflict-1"); err == nil {
		t.Fatal("an unattached repository read a conflict")
	}
	var typedNil *StoryRepository
	if _, err := typedNil.GetStoryEntity(ctx, "entity-1"); err == nil {
		t.Fatal("a nil repository read a row")
	}
}

// Ensure the repository satisfies the port through this package's own
// compilation, beside the assertion in story.go.
var _ storyapp.Repository = (*StoryRepository)(nil)
