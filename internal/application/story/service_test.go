package story

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// counterIDs mints deterministic identifiers so a test can assert on them.
type counterIDs struct {
	mu     sync.Mutex
	prefix string
	count  int
}

func newCounterIDs(prefix string) *counterIDs {
	return &counterIDs{prefix: prefix}
}

func (g *counterIDs) New() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.count++
	return g.prefix + "-" + itoa(g.count), nil
}

// fixedClock pins every timestamp the service writes.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC) }

// memoryStore is an in-memory Repository double. It mirrors the real
// repositories' revision handling: an update matches the row's current
// revision and increments it, and an update that names a stale revision is
// refused with the domain's conflict error rather than writing.
type memoryStore struct {
	mu        sync.Mutex
	documents map[string]storydomain.SourceDocument
	versions  map[string]storydomain.SourceDocumentVersion
	chapters  map[string]storydomain.Chapter
	entities  map[string]storydomain.StoryEntity
	events    map[string]storydomain.StoryEvent
	// recorded holds the domain events the story commands wrote, which is a
	// different thing from the story events above.
	recorded  []event.Event
	relations map[string]storydomain.StoryRelation
	conflicts map[string]storydomain.StoryFactConflict
	// The three child rows of the fact layer. Each is keyed by what makes it
	// unique in the schema rather than by a surrogate, so the double refuses a
	// duplicate the way SQLite's constraints do.
	aliases      map[string]storydomain.StoryEntityAlias
	participants map[string]storydomain.StoryEventParticipant
	factSources  map[string]storydomain.StoryFactSource
	// failCreate makes the next write fail, so a storage failure is testable.
	failCreate error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		documents:    map[string]storydomain.SourceDocument{},
		versions:     map[string]storydomain.SourceDocumentVersion{},
		chapters:     map[string]storydomain.Chapter{},
		entities:     map[string]storydomain.StoryEntity{},
		events:       map[string]storydomain.StoryEvent{},
		relations:    map[string]storydomain.StoryRelation{},
		conflicts:    map[string]storydomain.StoryFactConflict{},
		aliases:      map[string]storydomain.StoryEntityAlias{},
		participants: map[string]storydomain.StoryEventParticipant{},
		factSources:  map[string]storydomain.StoryFactSource{},
	}
}

func (s *memoryStore) CreateSourceDocument(_ context.Context, record storydomain.SourceDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	if _, exists := s.documents[record.ID]; exists {
		return storydomain.ConflictError("A source document with that id already exists.")
	}
	s.documents[record.ID] = record
	return nil
}

func (s *memoryStore) GetSourceDocument(_ context.Context, id string) (storydomain.SourceDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.documents[id]
	if !ok {
		return storydomain.SourceDocument{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) ListSourceDocuments(_ context.Context, projectID string) ([]storydomain.SourceDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.SourceDocument
	for _, record := range s.documents {
		if record.ProjectID == projectID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *memoryStore) UpdateSourceDocument(_ context.Context, record storydomain.SourceDocument, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.documents[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.documents[record.ID] = record
	return nil
}

func (s *memoryStore) CreateSourceDocumentVersion(_ context.Context, version storydomain.SourceDocumentVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.versions {
		if existing.SourceDocumentID == version.SourceDocumentID && existing.VersionNumber == version.VersionNumber {
			return storydomain.ConflictError("That version number is already used for this document.")
		}
	}
	s.versions[version.ID] = version
	return nil
}

func (s *memoryStore) MaxSourceDocumentVersionNumber(_ context.Context, sourceDocumentID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, existing := range s.versions {
		if existing.SourceDocumentID == sourceDocumentID && existing.VersionNumber > highest {
			highest = existing.VersionNumber
		}
	}
	return highest, nil
}

func (s *memoryStore) CreateChapter(_ context.Context, chapter storydomain.Chapter) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.chapters {
		if existing.SourceDocumentVersionID == chapter.SourceDocumentVersionID && existing.Ordinal == chapter.Ordinal {
			return storydomain.ConflictError("That chapter position is already used for this version.")
		}
	}
	s.chapters[chapter.ID] = chapter
	return nil
}

func (s *memoryStore) GetChapter(_ context.Context, id string) (storydomain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.chapters[id]
	if !ok {
		return storydomain.Chapter{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) ListChapters(_ context.Context, sourceDocumentVersionID string) ([]storydomain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.Chapter
	for _, record := range s.chapters {
		if record.SourceDocumentVersionID == sourceDocumentVersionID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *memoryStore) UpdateChapter(_ context.Context, chapter storydomain.Chapter, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.chapters[chapter.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	chapter.Revision = expectedRevision + 1
	s.chapters[chapter.ID] = chapter
	return nil
}

func (s *memoryStore) CreateStoryEntity(_ context.Context, record storydomain.StoryEntity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	s.entities[record.ID] = record
	return nil
}

func (s *memoryStore) GetStoryEntity(_ context.Context, id string) (storydomain.StoryEntity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.entities[id]
	if !ok {
		return storydomain.StoryEntity{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) UpdateStoryEntity(_ context.Context, record storydomain.StoryEntity, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.entities[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.entities[record.ID] = record
	return nil
}

func (s *memoryStore) CreateStoryEvent(_ context.Context, record storydomain.StoryEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	s.events[record.ID] = record
	return nil
}

func (s *memoryStore) GetStoryEvent(_ context.Context, id string) (storydomain.StoryEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.events[id]
	if !ok {
		return storydomain.StoryEvent{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) UpdateStoryEvent(_ context.Context, record storydomain.StoryEvent, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.events[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.events[record.ID] = record
	return nil
}

func (s *memoryStore) CreateStoryRelation(_ context.Context, record storydomain.StoryRelation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	if _, exists := s.relations[record.ID]; exists {
		return storydomain.ConflictError("A story relation with that id already exists.")
	}
	s.relations[record.ID] = record
	return nil
}

func (s *memoryStore) GetStoryRelation(_ context.Context, id string) (storydomain.StoryRelation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.relations[id]
	if !ok {
		return storydomain.StoryRelation{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) CreateStoryConflict(_ context.Context, record storydomain.StoryFactConflict) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.conflicts {
		if existing.LeftFactType == record.LeftFactType && existing.LeftFactID == record.LeftFactID &&
			existing.RightFactType == record.RightFactType && existing.RightFactID == record.RightFactID {
			return storydomain.ConflictError("That conflict is already recorded.")
		}
	}
	s.conflicts[record.ID] = record
	return nil
}

func (s *memoryStore) GetStoryConflict(_ context.Context, id string) (storydomain.StoryFactConflict, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.conflicts[id]
	if !ok {
		return storydomain.StoryFactConflict{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) UpdateStoryConflict(_ context.Context, record storydomain.StoryFactConflict, expectedStatus storydomain.ConflictStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.conflicts[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Status != expectedStatus {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	s.conflicts[record.ID] = record
	return nil
}

// setStatusForTest writes a status directly, standing in for a row another
// window already moved.
func (s *memoryStore) setEntityStatus(id string, status storydomain.FactStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.entities[id]
	record.Status = status
	s.entities[id] = record
}

// entityCount reports how many entities the store holds.
func (s *memoryStore) entityCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entities)
}

func newTestService(store Repository) *Service {
	return NewService(Options{Repository: store, Clock: fixedClock{}, IDs: newCounterIDs("story")})
}

// TestCreateSourceDocumentStoresACandidate proves a created document is
// validated, stamped and handed to the repository with the application's own
// identifier.
func TestCreateSourceDocumentStoresACandidate(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	record, err := service.CreateSourceDocument(context.Background(), CreateSourceDocumentRequest{
		ProjectID: "project-1", DocumentType: storydomain.DocumentNovel, Name: "  The Novel  ",
	})
	if err != nil {
		t.Fatalf("CreateSourceDocument: %v", err)
	}
	if record.ID != "story-1" {
		t.Fatalf("id = %q, want the application's first identifier", record.ID)
	}
	if record.Name != "The Novel" {
		t.Fatalf("name = %q, want it trimmed", record.Name)
	}
	if record.Status != storydomain.DocumentActive || record.Revision != 1 {
		t.Fatalf("status = %q revision = %d", record.Status, record.Revision)
	}
	if !record.CreatedAt.Equal(fixedClock{}.Now()) {
		t.Fatalf("created_at = %s, want the injected clock", record.CreatedAt)
	}
	stored, err := store.GetSourceDocument(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CurrentVersionID != "" {
		t.Fatalf("a document with no import already names a version: %q", stored.CurrentVersionID)
	}
}

// TestCreateSourceDocumentRejectsAnUnknownType proves the domain's validation
// is reached and its message is the one returned.
func TestCreateSourceDocumentRejectsAnUnknownType(t *testing.T) {
	service := newTestService(newMemoryStore())
	_, err := service.CreateSourceDocument(context.Background(), CreateSourceDocumentRequest{
		ProjectID: "project-1", DocumentType: "comic", Name: "N",
	})
	if err == nil {
		t.Fatal("an undocumented document type was accepted")
	}
	domainErr, ok := storydomain.AsError(err)
	if !ok || domainErr.Category != storydomain.CategoryInvalidInput {
		t.Fatalf("expected invalid_input, got %v", err)
	}
}

// TestAddSourceDocumentVersionNumbersAfterTheHighest proves the version number
// comes from the stored maximum, so a later import cannot reuse a number the
// unique constraint already holds.
func TestAddSourceDocumentVersionNumbersAfterTheHighest(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	document, err := service.CreateSourceDocument(context.Background(), CreateSourceDocumentRequest{
		ProjectID: "project-1", DocumentType: storydomain.DocumentNovel, Name: "Novel",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := AddSourceDocumentVersionRequest{
		SourceDocumentID:     document.ID,
		NormalizedTextFileID: hashA,
		ContentHash:          hashB,
		CharCount:            120,
	}
	first, err := service.AddSourceDocumentVersion(context.Background(), request)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if first.VersionNumber != 1 {
		t.Fatalf("first version number = %d, want 1", first.VersionNumber)
	}
	if first.CreatedByType != storydomain.CreatedByUser {
		t.Fatalf("producer = %q, want the documented default", first.CreatedByType)
	}
	second, err := service.AddSourceDocumentVersion(context.Background(), request)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.VersionNumber != 2 {
		t.Fatalf("second version number = %d, want 2", second.VersionNumber)
	}
	reloaded, err := store.GetSourceDocument(context.Background(), document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CurrentVersionID != second.ID {
		t.Fatalf("current_version_id = %q, want the newest import %q", reloaded.CurrentVersionID, second.ID)
	}
	// Two imports, so the pointer moved twice: the document started at revision
	// 1 and each pointer write incremented it.
	if reloaded.Revision != 3 {
		t.Fatalf("document revision = %d, want 3 after two pointer writes", reloaded.Revision)
	}
}

// TestAddSourceDocumentVersionRequiresANormalizedText proves a version whose
// text is missing is refused before the write: every chapter offset indexes
// that text, so a version without it could carry no evidence.
func TestAddSourceDocumentVersionRequiresANormalizedText(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	document, err := service.CreateSourceDocument(context.Background(), CreateSourceDocumentRequest{
		ProjectID: "project-1", DocumentType: storydomain.DocumentNovel, Name: "Novel",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.AddSourceDocumentVersion(context.Background(), AddSourceDocumentVersionRequest{SourceDocumentID: document.ID})
	if err == nil {
		t.Fatal("a version without normalized text was accepted")
	}
	if len(store.versions) != 0 {
		t.Fatal("the refused version was written")
	}
}

// TestCreateChapterRoundTrip covers the successful create and the ordinal
// conflict the repository reports.
func TestCreateChapterRoundTrip(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	record, err := service.CreateChapter(context.Background(), CreateChapterRequest{
		SourceDocumentVersionID: "version-1", Ordinal: 1, Title: "One", StartOffset: 0, EndOffset: 100,
	})
	if err != nil {
		t.Fatalf("CreateChapter: %v", err)
	}
	if record.Status != storydomain.ChapterDetected {
		t.Fatalf("status = %q, want the import pipeline's detected boundary", record.Status)
	}
	second, err := service.CreateChapter(context.Background(), CreateChapterRequest{
		SourceDocumentVersionID: "version-1", Ordinal: 1, StartOffset: 100, EndOffset: 200,
	})
	if err == nil {
		t.Fatal("two chapters were allowed to share an ordinal")
	}
	if second.ID != "" {
		t.Fatalf("a refused create returned a record: %+v", second)
	}
	domainErr, ok := storydomain.AsError(err)
	if !ok || domainErr.Category != storydomain.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
}

// TestReviseChapterGuardsRevision proves the compare-and-swap: a stale revision
// is refused and the row keeps its stored values.
func TestReviseChapterGuardsRevision(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	created, err := service.CreateChapter(context.Background(), CreateChapterRequest{
		SourceDocumentVersionID: "version-1", Ordinal: 1, StartOffset: 0, EndOffset: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	revised, err := service.ReviseChapter(context.Background(), ReviseChapterRequest{
		ChapterID: created.ID, StartOffset: 0, EndOffset: 120, Status: storydomain.ChapterEdited, Revision: 1,
	})
	if err != nil {
		t.Fatalf("ReviseChapter: %v", err)
	}
	if revised.Revision != 2 {
		t.Fatalf("revision = %d, want 2 after one guarded update", revised.Revision)
	}
	if revised.Status != storydomain.ChapterEdited {
		t.Fatalf("status = %q", revised.Status)
	}
	_, err = service.ReviseChapter(context.Background(), ReviseChapterRequest{
		ChapterID: created.ID, StartOffset: 0, EndOffset: 999, Status: storydomain.ChapterEdited, Revision: 1,
	})
	if err == nil {
		t.Fatal("a stale revision was accepted")
	}
	domainErr, ok := storydomain.AsError(err)
	if !ok || domainErr.Category != storydomain.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	stored, err := store.GetChapter(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.EndOffset != 120 {
		t.Fatalf("a refused update wrote the row: end_offset = %d", stored.EndOffset)
	}
}

// TestAcceptRejectGateRefusesTheWrongDirection is the PRD FR-030 gate: a
// rejected entity cannot be confirmed and a confirmed one cannot be rejected.
func TestAcceptRejectGateRefusesTheWrongDirection(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	entity, err := service.CreateStoryEntity(ctx, CreateStoryEntityRequest{
		ProjectID: "project-1", Type: storydomain.EntityCharacter, CanonicalName: "Mira",
	})
	if err != nil {
		t.Fatal(err)
	}
	if entity.Status != storydomain.FactCandidate {
		t.Fatalf("a new entity is %q, want candidate", entity.Status)
	}
	accepted, err := service.AcceptStoryEntity(ctx, FactDecisionRequest{ID: entity.ID, Revision: 1})
	if err != nil {
		t.Fatalf("AcceptStoryEntity: %v", err)
	}
	if accepted.Status != storydomain.FactAccepted || accepted.Revision != 2 {
		t.Fatalf("accepted = %+v", accepted)
	}
	if _, err := service.RejectStoryEntity(ctx, FactDecisionRequest{ID: entity.ID, Revision: 2}); err == nil {
		t.Fatal("a confirmed entity was rejected")
	} else if domainErr, ok := storydomain.AsError(err); !ok || domainErr.Category != storydomain.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	// Re-accepting an already-confirmed entity is refused too, so a repeated
	// click cannot write a revision for no state change.
	if _, err := service.AcceptStoryEntity(ctx, FactDecisionRequest{ID: entity.ID, Revision: 2}); err == nil {
		t.Fatal("an already-confirmed entity was confirmed again")
	}

	rejected, err := service.CreateStoryEntity(ctx, CreateStoryEntityRequest{
		ProjectID: "project-1", Type: storydomain.EntityProp, CanonicalName: "Lantern",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RejectStoryEntity(ctx, FactDecisionRequest{ID: rejected.ID, Revision: 1}); err != nil {
		t.Fatalf("RejectStoryEntity: %v", err)
	}
	if _, err := service.AcceptStoryEntity(ctx, FactDecisionRequest{ID: rejected.ID, Revision: 2}); err == nil {
		t.Fatal("a rejected entity was confirmed")
	} else if domainErr, ok := storydomain.AsError(err); !ok || domainErr.Category != storydomain.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	// A locked fact is neither confirmed nor rejected by this layer.
	locked, err := service.CreateStoryEntity(ctx, CreateStoryEntityRequest{
		ProjectID: "project-1", Type: storydomain.EntityLocation, CanonicalName: "Harbour",
	})
	if err != nil {
		t.Fatal(err)
	}
	store.setEntityStatus(locked.ID, storydomain.FactLocked)
	if _, err := service.AcceptStoryEntity(ctx, FactDecisionRequest{ID: locked.ID, Revision: 1}); err == nil {
		t.Fatal("a locked entity was confirmed")
	}
	if _, err := service.RejectStoryEntity(ctx, FactDecisionRequest{ID: locked.ID, Revision: 1}); err == nil {
		t.Fatal("a locked entity was rejected")
	}
}

// TestFactDecisionGuardsRevision proves a decision taken against a stale
// revision does not overwrite the state another window wrote.
func TestFactDecisionGuardsRevision(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	event, err := service.CreateStoryEvent(ctx, CreateStoryEventRequest{
		ProjectID: "project-1", Name: "The letter arrives", Ordinal: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Confidence != 0 || event.SourceScope != storydomain.ScopeOriginal {
		t.Fatalf("event defaults are wrong: %+v", event)
	}
	if _, err := service.AcceptStoryEvent(ctx, FactDecisionRequest{ID: event.ID, Revision: 0}); err == nil {
		t.Fatal("a decision against a stale revision was accepted")
	}
	accepted, err := service.AcceptStoryEvent(ctx, FactDecisionRequest{ID: event.ID, Revision: 1})
	if err != nil {
		t.Fatalf("AcceptStoryEvent: %v", err)
	}
	if accepted.Status != storydomain.FactAccepted || accepted.Revision != 2 {
		t.Fatalf("accepted = %+v", accepted)
	}
	if _, err := service.RejectStoryEvent(ctx, FactDecisionRequest{ID: event.ID, Revision: 2}); err == nil {
		t.Fatal("a confirmed event was rejected")
	}
}

// TestCreateStoryRelationValidatesTheVocabulary proves an undocumented
// relation type is refused before the write, because the schema's CHECK would
// reject the row anyway and the caller deserves the domain's message.
func TestCreateStoryRelationValidatesTheVocabulary(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	record, err := service.CreateStoryRelation(ctx, CreateStoryRelationRequest{
		ProjectID: "project-1", Type: storydomain.RelationKnows,
		SourceEntityType: "character", SourceEntityID: "entity-1",
		TargetEntityType: "character", TargetEntityID: "entity-2",
	})
	if err != nil {
		t.Fatalf("CreateStoryRelation: %v", err)
	}
	if record.Status != storydomain.FactCandidate || record.Revision != 1 {
		t.Fatalf("relation = %+v", record)
	}
	if _, err := service.CreateStoryRelation(ctx, CreateStoryRelationRequest{
		ProjectID: "project-1", Type: "adores",
		SourceEntityType: "character", SourceEntityID: "entity-1",
		TargetEntityType: "character", TargetEntityID: "entity-2",
	}); err == nil {
		t.Fatal("an undocumented relation type was accepted")
	}
	if len(store.relations) != 1 {
		t.Fatalf("the store holds %d relations, want 1", len(store.relations))
	}
}

// TestResolveConflictDelegatesToTheDomain proves the domain's transition rules
// are the ones applied: an open conflict resolves once, and a second attempt is
// refused rather than rewriting the decision.
func TestResolveConflictDelegatesToTheDomain(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	conflict, err := service.OpenConflict(ctx, OpenConflictRequest{
		ProjectID: "project-1", LeftFactType: storydomain.FactEntity, LeftFactID: "entity-1",
		RightFactType: storydomain.FactEntity, RightFactID: "entity-2", ConflictType: "name_clash",
	})
	if err != nil {
		t.Fatalf("OpenConflict: %v", err)
	}
	if conflict.Status != storydomain.ConflictOpen || !conflict.ResolvedAt.IsZero() {
		t.Fatalf("conflict = %+v", conflict)
	}
	resolved, err := service.ResolveConflict(ctx, ResolveConflictRequest{
		ConflictID: conflict.ID, Resolution: "The longer name is canonical.", ResolvedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if resolved.Status != storydomain.ConflictResolved || resolved.ResolvedBy != "user-1" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if !resolved.ResolvedAt.Equal(fixedClock{}.Now()) {
		t.Fatalf("resolved_at = %s, want the injected clock", resolved.ResolvedAt)
	}
	if _, err := service.ResolveConflict(ctx, ResolveConflictRequest{ConflictID: conflict.ID, Resolution: "again"}); err == nil {
		t.Fatal("an already-resolved conflict was resolved again")
	} else if domainErr, ok := storydomain.AsError(err); !ok || domainErr.Category != storydomain.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if _, err := service.OpenConflict(ctx, OpenConflictRequest{
		ProjectID: "project-1", LeftFactType: storydomain.FactEntity, LeftFactID: "entity-1",
		RightFactType: storydomain.FactEntity, RightFactID: "entity-2",
	}); err == nil {
		t.Fatal("the same fact pair was recorded twice")
	}
}

// TestUnavailableServiceFailsClosed proves an unattached service reports a
// storage failure instead of panicking, and that it writes nothing.
func TestUnavailableServiceFailsClosed(t *testing.T) {
	ctx := context.Background()
	service := NewService(Options{Clock: fixedClock{}})
	if service.Available() {
		t.Fatal("a service with no repository reports itself available")
	}
	if _, err := service.CreateSourceDocument(ctx, CreateSourceDocumentRequest{
		ProjectID: "project-1", DocumentType: storydomain.DocumentNovel, Name: "N",
	}); err == nil {
		t.Fatal("an unattached service created a document")
	} else if domainErr, ok := storydomain.AsError(err); !ok || domainErr.Category != storydomain.CategoryStorage {
		t.Fatalf("expected storage, got %v", err)
	}
	if _, err := service.ListChapters(ctx, "version-1"); err == nil {
		t.Fatal("an unattached service listed chapters")
	}

	// A repository with no identifier source is unavailable for the same
	// reason: the application layer is what mints identifiers (ADR-0005).
	noIDs := NewService(Options{Repository: newMemoryStore(), Clock: fixedClock{}})
	if noIDs.Available() {
		t.Fatal("a service with no identifier source reports itself available")
	}
	if _, err := noIDs.CreateStoryEntity(ctx, CreateStoryEntityRequest{
		ProjectID: "project-1", Type: storydomain.EntityCharacter, CanonicalName: "Mira",
	}); err == nil {
		t.Fatal("an identifier-less service created an entity")
	}
}

// TestCreateStoryEntityPropagatesRepositoryFailure proves a storage failure is
// not swallowed and no record is returned as if it had been written.
func TestCreateStoryEntityPropagatesRepositoryFailure(t *testing.T) {
	store := newMemoryStore()
	store.failCreate = storydomain.StorageError("The fact could not be saved.", nil)
	service := newTestService(store)
	record, err := service.CreateStoryEntity(context.Background(), CreateStoryEntityRequest{
		ProjectID: "project-1", Type: storydomain.EntityCharacter, CanonicalName: "Mira",
	})
	if err == nil {
		t.Fatal("a failing store reported success")
	}
	if record.ID != "" {
		t.Fatalf("a failed create returned a record: %+v", record)
	}
	if store.entityCount() != 0 {
		t.Fatal("the failed entity was stored")
	}
}

// hashA and hashB are content hashes shaped like a SHA-256 digest, matching what
// the domain requires of a file reference.
const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// The three methods the WP-06 port additions require. They mirror the real
// repository's semantics rather than mocking them: a confirmation moves only
// the boundaries that are still 'detected', and the event is written with them.
func (s *memoryStore) GetSourceDocumentVersion(_ context.Context, id string) (storydomain.SourceDocumentVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[id]
	if !ok {
		return storydomain.SourceDocumentVersion{}, storydomain.NotFoundError()
	}
	return version, nil
}

func (s *memoryStore) FindVersionBySourceHash(_ context.Context, projectID, sourceHash string) (storydomain.SourceDocumentVersion, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sourceHash == "" {
		return storydomain.SourceDocumentVersion{}, "", false, nil
	}
	for id, version := range s.versions {
		if version.SourceHash != sourceHash {
			continue
		}
		document, ok := s.documents[version.SourceDocumentID]
		if !ok || document.ProjectID != projectID {
			continue
		}
		return s.versions[id], document.Name, true, nil
	}
	return storydomain.SourceDocumentVersion{}, "", false, nil
}

func (s *memoryStore) ConfirmChapters(_ context.Context, sourceDocumentVersionID string, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for id, chapter := range s.chapters {
		if chapter.SourceDocumentVersionID != sourceDocumentVersionID {
			continue
		}
		// An edited boundary keeps its status: it already carries a stronger
		// statement than confirmed.
		if chapter.Status != storydomain.ChapterDetected {
			continue
		}
		chapter.Status = storydomain.ChapterConfirmed
		chapter.Revision++
		s.chapters[id] = chapter
	}
	s.recorded = append(s.recorded, record)
	return nil
}

// The child rows of the fact layer: aliases, participants and evidence.

func (s *memoryStore) CreateStoryEntityAlias(_ context.Context, record storydomain.StoryEntityAlias) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	// The schema's unique index is over (entity, alias), so the double refuses a
	// duplicate pair the same way rather than letting a second row in.
	for _, existing := range s.aliases {
		if existing.StoryEntityID == record.StoryEntityID && existing.Alias == record.Alias {
			return storydomain.ConflictError("That name is already an alias for this entity.")
		}
	}
	s.aliases[record.ID] = record
	return nil
}

func (s *memoryStore) ListStoryEntityAliases(_ context.Context, storyEntityID string) ([]storydomain.StoryEntityAlias, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []storydomain.StoryEntityAlias{}
	for _, record := range s.aliases {
		if record.StoryEntityID == storyEntityID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

func (s *memoryStore) CreateStoryEventParticipant(_ context.Context, record storydomain.StoryEventParticipant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	// The primary key is (event, entity, role), so the same entity may hold two
	// roles but not the same one twice.
	key := record.StoryEventID + "\x00" + record.StoryEntityID + "\x00" + string(record.Role)
	if _, exists := s.participants[key]; exists {
		return storydomain.ConflictError("That entity already holds that role in this event.")
	}
	s.participants[key] = record
	return nil
}

func (s *memoryStore) ListStoryEventParticipants(_ context.Context, storyEventID string) ([]storydomain.StoryEventParticipant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []storydomain.StoryEventParticipant{}
	for _, record := range s.participants {
		if record.StoryEventID == storyEventID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Role < records[j].Role })
	return records, nil
}

func (s *memoryStore) CreateStoryFactSource(_ context.Context, record storydomain.StoryFactSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	s.factSources[record.ID] = record
	return nil
}

func (s *memoryStore) ListStoryFactSources(_ context.Context, factType storydomain.FactType, factID string) ([]storydomain.StoryFactSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []storydomain.StoryFactSource{}
	for _, record := range s.factSources {
		if record.FactType == factType && record.FactID == factID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

// The three list queries the graph panel drives. The double filters the same way
// the SQL does, including the empty-filter rule, so a service test that passes
// here is not passing because the double was permissive.

func (s *memoryStore) ListStoryEntities(_ context.Context, projectID string, status storydomain.FactStatus) ([]storydomain.StoryEntity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []storydomain.StoryEntity{}
	for _, record := range s.entities {
		if record.ProjectID != projectID {
			continue
		}
		// A soft-deleted row is never returned, which is the rule the SQL's
		// `deleted_at = ''` carries.
		if !record.DeletedAt.IsZero() {
			continue
		}
		if status != "" && record.Status != status {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

func (s *memoryStore) ListStoryEvents(_ context.Context, projectID, chapterID string, status storydomain.FactStatus) ([]storydomain.StoryEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []storydomain.StoryEvent{}
	for _, record := range s.events {
		if record.ProjectID != projectID {
			continue
		}
		if chapterID != "" && record.ChapterID != chapterID {
			continue
		}
		if status != "" && record.Status != status {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Ordinal != records[j].Ordinal {
			return records[i].Ordinal < records[j].Ordinal
		}
		return records[i].ID < records[j].ID
	})
	return records, nil
}

func (s *memoryStore) ListStoryRelations(_ context.Context, projectID string, status storydomain.FactStatus) ([]storydomain.StoryRelation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []storydomain.StoryRelation{}
	for _, record := range s.relations {
		if record.ProjectID != projectID {
			continue
		}
		if status != "" && record.Status != status {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

// ListStoryConflicts mirrors the real repository's newest-first order and its
// status filter, including that an empty status means "any".
func (s *memoryStore) ListStoryConflicts(_ context.Context, projectID string, status storydomain.ConflictStatus) ([]storydomain.StoryFactConflict, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []storydomain.StoryFactConflict{}
	for _, record := range s.conflicts {
		if record.ProjectID != projectID {
			continue
		}
		if status != "" && record.Status != status {
			continue
		}
		records = append(records, record)
	}
	// Newest first, which is the opposite of the other fact lists: a conflict is
	// a queue rather than a story.
	sort.Slice(records, func(i, j int) bool { return records[i].ID > records[j].ID })
	return records, nil
}

// SplitChapter mirrors the real repository: the surviving chapter is shortened and
// the chapters after it shift up by one.
func (s *memoryStore) SplitChapter(_ context.Context, first storydomain.Chapter, second storydomain.Chapter, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.chapters[first.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	if _, clash := s.chapters[second.ID]; clash {
		return storydomain.ConflictError("A story entity with that id already exists.")
	}
	// The shift happens before the insert, exactly as the SQL does, so a service
	// test would see the same collision the constraint produces.
	for id, chapter := range s.chapters {
		if chapter.SourceDocumentVersionID != first.SourceDocumentVersionID {
			continue
		}
		if chapter.Ordinal >= second.Ordinal {
			chapter.Ordinal++
			s.chapters[id] = chapter
		}
	}
	first.Revision = expectedRevision + 1
	s.chapters[first.ID] = first
	s.chapters[second.ID] = second
	return nil
}

// MergeChapters mirrors the real repository: the absorbed chapter is removed and
// the chapters after it shift down.
// MergeEntities is the port's merge, and this double answers it without moving anything.
//
// The refusal the SERVICE makes — a locked entity, a cross-project pair — is what these tests are
// about, and they read the entities through the getters the double already serves. A double that
// moved references would be a second implementation of the merge to keep in step, and the real one's
// evidence is the database package's tests over the actual tables.
func (s *memoryStore) MergeEntities(_ context.Context, survivorID, absorbedID string, expectedRevision int64) (MergeEntitiesResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entities[survivorID]; !ok {
		return MergeEntitiesResult{}, storydomain.NotFoundError()
	}
	absorbed, ok := s.entities[absorbedID]
	if !ok {
		return MergeEntitiesResult{}, storydomain.NotFoundError()
	}
	delete(s.entities, absorbedID)
	return MergeEntitiesResult{AbsorbedName: absorbed.CanonicalName}, nil
}

func (s *memoryStore) MergeChapters(_ context.Context, merged storydomain.Chapter, absorbedID string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.chapters[merged.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	absorbed, ok := s.chapters[absorbedID]
	if !ok {
		return storydomain.NotFoundError()
	}
	delete(s.chapters, absorbedID)
	for id, chapter := range s.chapters {
		if chapter.SourceDocumentVersionID != merged.SourceDocumentVersionID {
			continue
		}
		if chapter.Ordinal > absorbed.Ordinal {
			chapter.Ordinal--
			s.chapters[id] = chapter
		}
	}
	merged.Revision = expectedRevision + 1
	s.chapters[merged.ID] = merged
	return nil
}
