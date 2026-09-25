package story

import (
	"context"
	"strings"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// story_graph.go holds the commands and queries the story-graph panel drives:
// the reads that list the fact layer, the alias command, and the evidence a fact
// cites.
//
// ADR-0007 and the WP-05 record both noted that these write paths were missing.
// WP-05 built the tables and the candidate workflow for entities, events and
// relations, and left aliases, participants and evidence with no writer, because
// nothing in that package produced them. Extraction does, so they land here.

// MaxAliasesPerEntity bounds the aliases one entity may carry.
//
// A bound exists because the alias list is rendered whole in the graph panel and
// searched name by name, so an unbounded list is a slow query rather than a
// feature. The value is generous: a serialised novel gives a character a handful
// of names, not dozens.
const MaxAliasesPerEntity = 50

// ListStoryEntities returns a project's entities, oldest first.
//
// An empty status returns every live entity, which is what the graph's entity
// list shows. The candidate filter is the review queue.
func (s *Service) ListStoryEntities(ctx context.Context, projectID string, status storydomain.FactStatus) ([]storydomain.StoryEntity, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, storydomain.InvalidError("A project is required.")
	}
	if status != "" && !storydomain.IsValidFactStatus(status) {
		return nil, storydomain.InvalidError("That entity status is not recognised.")
	}
	return s.repository.ListStoryEntities(ctx, projectID, status)
}

// ListStoryEvents returns a project's events in story order.
//
// chapterID narrows the list to one chapter, which is what the chapter panel
// asks for; empty means the whole project.
func (s *Service) ListStoryEvents(ctx context.Context, projectID, chapterID string, status storydomain.FactStatus) ([]storydomain.StoryEvent, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, storydomain.InvalidError("A project is required.")
	}
	if status != "" && !storydomain.IsValidFactStatus(status) {
		return nil, storydomain.InvalidError("That event status is not recognised.")
	}
	return s.repository.ListStoryEvents(ctx, projectID, chapterID, status)
}

// ListStoryRelations returns a project's relations, oldest first.
func (s *Service) ListStoryRelations(ctx context.Context, projectID string, status storydomain.FactStatus) ([]storydomain.StoryRelation, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, storydomain.InvalidError("A project is required.")
	}
	if status != "" && !storydomain.IsValidFactStatus(status) {
		return nil, storydomain.InvalidError("That relation status is not recognised.")
	}
	return s.repository.ListStoryRelations(ctx, projectID, status)
}

// ListStoryEventParticipants returns an event's participants.
func (s *Service) ListStoryEventParticipants(ctx context.Context, storyEventID string) ([]storydomain.StoryEventParticipant, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if strings.TrimSpace(storyEventID) == "" {
		return nil, storydomain.InvalidError("An event is required.")
	}
	return s.repository.ListStoryEventParticipants(ctx, storyEventID)
}

// ListProjectEventParticipants returns every participation in one project.
//
// The status filter is validated here as well as in the store, and the validation
// is not redundant: a caller that passed an unknown status would otherwise get an
// empty list, which reads as "this project has no participations" rather than as
// the caller's mistake.
func (s *Service) ListProjectEventParticipants(ctx context.Context, projectID string, status storydomain.FactStatus) ([]storydomain.StoryEventParticipant, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, storydomain.InvalidError("A project is required.")
	}
	if status != "" && !storydomain.IsValidFactStatus(status) {
		return nil, storydomain.InvalidError("That participation status is not recognised.")
	}
	return s.repository.ListProjectEventParticipants(ctx, projectID, status)
}

// ListStoryEntityAliases returns an entity's aliases.
func (s *Service) ListStoryEntityAliases(ctx context.Context, storyEntityID string) ([]storydomain.StoryEntityAlias, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if strings.TrimSpace(storyEntityID) == "" {
		return nil, storydomain.InvalidError("An entity is required.")
	}
	return s.repository.ListStoryEntityAliases(ctx, storyEntityID)
}

// ListStoryFactSources returns the evidence a fact cites.
//
// It is the query behind "where did this come from": the panel shows the
// chapter and the offsets, and the offsets are read back through ReadRange, so
// the evidence row never carries a copy of the text.
func (s *Service) ListStoryFactSources(ctx context.Context, factType storydomain.FactType, factID string) ([]storydomain.StoryFactSource, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if !storydomain.IsValidFactType(factType) {
		return nil, storydomain.InvalidError("That fact type is not recognised.")
	}
	if strings.TrimSpace(factID) == "" {
		return nil, storydomain.InvalidError("A fact is required.")
	}
	return s.repository.ListStoryFactSources(ctx, factType, factID)
}

// AddStoryEntityAliasRequest adds an alternative name to an entity.
type AddStoryEntityAliasRequest struct {
	StoryEntityID string
	Alias         string
	// SourceChapterID and the two offsets are where the name appeared. All three
	// are optional: an alias a user typed has no source span, and the schema
	// distinguishes that from a span recorded at offset zero.
	SourceChapterID string
	SourceStart     *int
	SourceEnd       *int
}

// AddStoryEntityAlias records an alternative name for an entity.
//
// The alias is a separate row rather than a field because the alternatives are
// searched individually: a reader looking for 小艾 must find the entity whose
// canonical name is Mira. That is also why the count is bounded and the
// uniqueness is the schema's: two rows for one spelling would make a merge
// ambiguous.
//
// A locked entity refuses new aliases. The lock is what stops a later pass from
// rewriting a fact, and adding a name is a rewrite of how the fact is found.
func (s *Service) AddStoryEntityAlias(ctx context.Context, request AddStoryEntityAliasRequest) (storydomain.StoryEntityAlias, error) {
	if !s.Available() {
		return storydomain.StoryEntityAlias{}, storageFailure()
	}
	trimmed := strings.TrimSpace(request.Alias)
	if trimmed == "" {
		return storydomain.StoryEntityAlias{}, storydomain.InvalidError("An alias needs text.")
	}
	entity, err := s.repository.GetStoryEntity(ctx, request.StoryEntityID)
	if err != nil {
		return storydomain.StoryEntityAlias{}, err
	}
	if entity.Status == storydomain.FactLocked {
		return storydomain.StoryEntityAlias{}, storydomain.ConflictError("This entity is locked, so its names cannot be changed here.")
	}
	// Refusing a duplicate of the canonical name keeps the two lists from
	// disagreeing about what the entity is called.
	if strings.EqualFold(trimmed, entity.CanonicalName) {
		return storydomain.StoryEntityAlias{}, storydomain.ConflictError("That is already the entity's name.")
	}
	existing, err := s.repository.ListStoryEntityAliases(ctx, entity.ID)
	if err != nil {
		return storydomain.StoryEntityAlias{}, err
	}
	if len(existing) >= MaxAliasesPerEntity {
		return storydomain.StoryEntityAlias{}, storydomain.ConflictError("This entity already has as many aliases as the graph stores.")
	}
	for _, alias := range existing {
		if strings.EqualFold(alias.Alias, trimmed) {
			return storydomain.StoryEntityAlias{}, storydomain.ConflictError("That name is already an alias for this entity.")
		}
	}
	id, err := s.ids.New()
	if err != nil {
		return storydomain.StoryEntityAlias{}, storageFailure()
	}
	now := s.now()
	record := storydomain.StoryEntityAlias{
		ID:              id,
		StoryEntityID:   entity.ID,
		Alias:           trimmed,
		SourceChapterID: request.SourceChapterID,
		SourceStart:     request.SourceStart,
		SourceEnd:       request.SourceEnd,
		CreatedAt:       now,
	}
	if err := record.Validate(); err != nil {
		return storydomain.StoryEntityAlias{}, err
	}
	if err := s.repository.CreateStoryEntityAlias(ctx, record); err != nil {
		return storydomain.StoryEntityAlias{}, err
	}
	return record, nil
}

// RecordFactSourceRequest records where one fact was found.
type RecordFactSourceRequest struct {
	FactType                storydomain.FactType
	FactID                  string
	ChapterID               string
	SourceDocumentVersionID string
	StartOffset             *int
	EndOffset               *int
	QuoteHash               string
	SourceKind              storydomain.SourceKind
}

// RecordFactSource stores the evidence a fact cites.
//
// The caller must name the document version, because offsets index one version's
// normalized text and an offset without a version cannot be read back. A
// user-stated fact has no span but still names the version it is about.
func (s *Service) RecordFactSource(ctx context.Context, request RecordFactSourceRequest) (storydomain.StoryFactSource, error) {
	if !s.Available() {
		return storydomain.StoryFactSource{}, storageFailure()
	}
	id, err := s.ids.New()
	if err != nil {
		return storydomain.StoryFactSource{}, storageFailure()
	}
	kind := request.SourceKind
	if kind == "" {
		kind = storydomain.SourceKindText
	}
	record := storydomain.StoryFactSource{
		ID:                      id,
		FactType:                request.FactType,
		FactID:                  request.FactID,
		ChapterID:               request.ChapterID,
		SourceDocumentVersionID: request.SourceDocumentVersionID,
		StartOffset:             request.StartOffset,
		EndOffset:               request.EndOffset,
		QuoteHash:               request.QuoteHash,
		SourceKind:              kind,
		CreatedAt:               s.now(),
	}
	if err := record.Validate(); err != nil {
		return storydomain.StoryFactSource{}, err
	}
	if err := s.repository.CreateStoryFactSource(ctx, record); err != nil {
		return storydomain.StoryFactSource{}, err
	}
	return record, nil
}

// AddStoryEventParticipantRequest links an entity to an event.
type AddStoryEventParticipantRequest struct {
	StoryEventID  string
	StoryEntityID string
	Role          storydomain.ParticipantRole
	StateBefore   string
	StateAfter    string
}

// AddStoryEventParticipant records that an entity took part in an event.
//
// One entity may hold several roles in one event — a character can be the actor
// and the owner of what changed hands — so the role is part of the identity
// rather than a field, and a repeated triple is the conflict.
func (s *Service) AddStoryEventParticipant(ctx context.Context, request AddStoryEventParticipantRequest) (storydomain.StoryEventParticipant, error) {
	if !s.Available() {
		return storydomain.StoryEventParticipant{}, storageFailure()
	}
	record := storydomain.StoryEventParticipant{
		StoryEventID:  request.StoryEventID,
		StoryEntityID: request.StoryEntityID,
		Role:          request.Role,
		StateBefore:   strings.TrimSpace(request.StateBefore),
		StateAfter:    strings.TrimSpace(request.StateAfter),
		CreatedAt:     s.now(),
	}
	if err := record.Validate(); err != nil {
		return storydomain.StoryEventParticipant{}, err
	}
	if err := s.repository.CreateStoryEventParticipant(ctx, record); err != nil {
		return storydomain.StoryEventParticipant{}, err
	}
	return record, nil
}

// ListStoryConflicts returns a project's recorded conflicts, newest first.
//
// It is the read the conflict queue needs, and the reason WP-05's gap notice
// mentioned conflicts: OpenConflict and ResolveConflict existed as commands with
// no way to see what had been recorded, so a user could open a conflict and never
// find it again. AC-STORY-002 lists 冲突记录 as an acceptance point, and a
// recorded conflict nobody can list is not a record.
func (s *Service) ListStoryConflicts(ctx context.Context, projectID string, status storydomain.ConflictStatus) ([]storydomain.StoryFactConflict, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, storydomain.InvalidError("A project is required.")
	}
	if status != "" && !storydomain.IsValidConflictStatus(status) {
		return nil, storydomain.InvalidError("That conflict status is not recognised.")
	}
	return s.repository.ListStoryConflicts(ctx, projectID, status)
}

// LockStoryEntityRequest pins or releases a fact.
type LockStoryEntityRequest struct {
	ID       string
	Revision int64
}

// LockStoryEntity pins a fact so a later pass cannot revise it.
//
// AC-STORY-002 lists 接受/拒绝/锁定 as the three decisions a candidate must be
// able to receive, and the lock was the one with no command: 'locked' existed as a
// status that four gates refused to move a fact OUT of, and nothing could put a
// fact INTO it. A state only the database can produce is not a feature.
//
// The lock is reachable from 'candidate' and 'accepted', and NOT from 'rejected'.
//
// The first version allowed it from any status, reasoning that pinning a rejection
// is a stronger statement than the rejection. That was wrong, and an independent
// review traced the consequence: because unlock returns a fact to 'accepted', the
// sequence reject → lock → unlock turned a rejection into a confirmation. That
// reverses a decision the user made, which acceptGate elsewhere in this package
// refuses to do ("A rejected fact keeps its decision until a new candidate is
// recorded"), and it was reachable from the UI in two clicks.
//
// A schema with no memory of the pre-lock state cannot restore it, so the honest
// fix is to refuse the transition rather than to guess at one. Pinning a rejection
// needs a column that records what was pinned, which is a schema change and a
// later package's decision — recorded in ADR-0010 rather than approximated here.
//
// It is also not reachable from 'locked', because re-locking would write a
// revision for no state change.
func (s *Service) LockStoryEntity(ctx context.Context, request LockStoryEntityRequest) (storydomain.StoryEntity, error) {
	if !s.Available() {
		return storydomain.StoryEntity{}, storageFailure()
	}
	record, err := s.repository.GetStoryEntity(ctx, request.ID)
	if err != nil {
		return storydomain.StoryEntity{}, err
	}
	if err := lockableGate(record.Status); err != nil {
		return storydomain.StoryEntity{}, err
	}
	record.Status = storydomain.FactLocked
	record.UpdatedAt = s.now()
	if err := record.Validate(); err != nil {
		return storydomain.StoryEntity{}, err
	}
	if err := s.repository.UpdateStoryEntity(ctx, record, request.Revision); err != nil {
		return storydomain.StoryEntity{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// LockStoryEvent pins an event, on the same terms as LockStoryEntity.
func (s *Service) LockStoryEvent(ctx context.Context, request LockStoryEntityRequest) (storydomain.StoryEvent, error) {
	if !s.Available() {
		return storydomain.StoryEvent{}, storageFailure()
	}
	record, err := s.repository.GetStoryEvent(ctx, request.ID)
	if err != nil {
		return storydomain.StoryEvent{}, err
	}
	if err := lockableGate(record.Status); err != nil {
		return storydomain.StoryEvent{}, err
	}
	record.Status = storydomain.FactLocked
	record.UpdatedAt = s.now()
	if err := record.Validate(); err != nil {
		return storydomain.StoryEvent{}, err
	}
	if err := s.repository.UpdateStoryEvent(ctx, record, request.Revision); err != nil {
		return storydomain.StoryEvent{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// UnlockStoryEntity releases a lock.
//
// It is a separate command rather than an argument on Lock because the two are
// different decisions with different consequences, and an argument is one
// keystroke away from the wrong one. Only 'locked' can be released: unlocking a
// fact that was never locked would silently move it to 'accepted', which is a
// confirmation the user did not make.
func (s *Service) UnlockStoryEntity(ctx context.Context, request LockStoryEntityRequest) (storydomain.StoryEntity, error) {
	if !s.Available() {
		return storydomain.StoryEntity{}, storageFailure()
	}
	record, err := s.repository.GetStoryEntity(ctx, request.ID)
	if err != nil {
		return storydomain.StoryEntity{}, err
	}
	if record.Status != storydomain.FactLocked {
		return storydomain.StoryEntity{}, storydomain.ConflictError("This fact is not locked.")
	}
	// Releasing returns the fact to 'accepted', which is the state a locked fact
	// was in before it was pinned: the lock protects a decision, and releasing it
	// leaves the decision standing rather than undoing it.
	record.Status = storydomain.FactAccepted
	record.UpdatedAt = s.now()
	if err := record.Validate(); err != nil {
		return storydomain.StoryEntity{}, err
	}
	if err := s.repository.UpdateStoryEntity(ctx, record, request.Revision); err != nil {
		return storydomain.StoryEntity{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// UnlockStoryEvent releases an event's lock.
func (s *Service) UnlockStoryEvent(ctx context.Context, request LockStoryEntityRequest) (storydomain.StoryEvent, error) {
	if !s.Available() {
		return storydomain.StoryEvent{}, storageFailure()
	}
	record, err := s.repository.GetStoryEvent(ctx, request.ID)
	if err != nil {
		return storydomain.StoryEvent{}, err
	}
	if record.Status != storydomain.FactLocked {
		return storydomain.StoryEvent{}, storydomain.ConflictError("This fact is not locked.")
	}
	record.Status = storydomain.FactAccepted
	record.UpdatedAt = s.now()
	if err := record.Validate(); err != nil {
		return storydomain.StoryEvent{}, err
	}
	if err := s.repository.UpdateStoryEvent(ctx, record, request.Revision); err != nil {
		return storydomain.StoryEvent{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// lockableGate refuses a lock on a fact whose pre-lock state unlock could not
// restore.
//
// The status vocabulary has no place to record what a fact was before it was
// pinned, so unlock always returns 'accepted'. That is correct for a candidate or
// an accepted fact and WRONG for a rejected one: it would turn a user's rejection
// into a confirmation through two individually-legal commands. Until a column
// records the pinned state, the transition is refused.
func lockableGate(current storydomain.FactStatus) error {
	switch current {
	case storydomain.FactLocked:
		return storydomain.ConflictError("This fact is already locked.")
	case storydomain.FactRejected:
		return storydomain.ConflictError("A rejected fact cannot be locked, because unlocking it would confirm it. Record a new candidate instead.")
	}
	return nil
}

// SplitChapterRequest divides one chapter into two at a rune offset.
type SplitChapterRequest struct {
	ChapterID string
	// SplitAtOffset is where the second chapter begins, in the VERSION's
	// coordinates. It must fall strictly inside the chapter: at either end would
	// produce an empty half, which is a boundary the user did not ask for.
	SplitAtOffset int
	// SecondTitle names the new second half. An empty value gives it "" — the
	// schema's title default — rather than inventing one, because a generated name
	// would look like something the document said.
	SecondTitle string
	// Revision is the revision the caller last read for the first chapter.
	Revision int64
}

// SplitChapter divides a chapter in two.
//
// PRD FR-020 lists 手动合并/拆分 among the import flow's MUST items and
// AC-STORY-001's acceptance names 章节拆分可人工修正并保存, so this and MergeChapter
// below are required rather than optional. The first draft of this package
// deferred them and described them as unspecified; an independent review showed
// the specification names them explicitly, and that description is corrected in
// ADR-0010.
//
// The offsets are the hard part. The text a chapter covers does not change when it
// is split — the two halves must tile exactly what the one chapter covered — or
// the document's chapters would no longer add up to the document. So the second
// half begins where the split point is and runs to the first half's old end, and
// the first half ends where the second begins.
func (s *Service) SplitChapter(ctx context.Context, request SplitChapterRequest) ([]storydomain.Chapter, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	first, err := s.repository.GetChapter(ctx, request.ChapterID)
	if err != nil {
		return nil, err
	}
	if request.SplitAtOffset <= first.StartOffset || request.SplitAtOffset >= first.EndOffset {
		// A split at either end produces an empty chapter, which is a boundary the
		// user did not ask for and which no detector would have produced.
		return nil, storydomain.InvalidError("A chapter can only be split inside its own range.")
	}
	id, err := s.ids.New()
	if err != nil {
		return nil, storageFailure()
	}
	now := s.now()
	second := storydomain.Chapter{
		ID:                      id,
		SourceDocumentVersionID: first.SourceDocumentVersionID,
		Ordinal:                 first.Ordinal + 1,
		Title:                   strings.TrimSpace(request.SecondTitle),
		StartOffset:             request.SplitAtOffset,
		EndOffset:               first.EndOffset,
		// The new boundary is the user's, and so is the shortened one: both halves
		// carry 'manual', which is what tells the import report a person made them.
		SourceKind: storydomain.ChapterManual,
		// A split chapter is 'edited' rather than 'detected': a person decided it.
		Status:    storydomain.ChapterEdited,
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
	}
	if err := second.Validate(); err != nil {
		return nil, err
	}
	shortened := first
	shortened.EndOffset = request.SplitAtOffset
	shortened.SourceKind = storydomain.ChapterManual
	shortened.Status = storydomain.ChapterEdited
	shortened.UpdatedAt = now
	if err := shortened.Validate(); err != nil {
		return nil, err
	}
	if err := s.repository.SplitChapter(ctx, shortened, second, request.Revision); err != nil {
		return nil, err
	}
	shortened.Revision = request.Revision + 1
	return []storydomain.Chapter{shortened, second}, nil
}

// MergeChapterRequest absorbs one chapter into the one before it.
type MergeChapterRequest struct {
	// FirstChapterID is the chapter that survives.
	FirstChapterID string
	// SecondChapterID is the adjacent chapter it absorbs. It must be the NEXT one
	// by ordinal: merging non-adjacent chapters would leave a gap the ordinals
	// cannot express.
	SecondChapterID string
	// Revision is the revision the caller last read for the first chapter.
	Revision int64
}

// MergeChapter absorbs the chapter after the given one.
//
// The title of the survivor is kept, because it is the one the user named when
// they chose which chapter to merge into; the absorbed chapter's title is
// discarded rather than concatenated, since joining two names produces a name no
// document contained. What the merged chapter covers is the union of the two
// ranges, so the text still adds up.
// MergeStoryEntityRequest merges one entity into another.
type MergeStoryEntityRequest struct {
	// SurvivorID is the entity that keeps its row. The caller names it because which of two
	// duplicates should be KNOWN by which name is a writer's decision, and this command has no basis
	// for making it.
	SurvivorID string
	// AbsorbedID is the entity whose references move and whose row is removed.
	AbsorbedID string
	// Revision is the SURVIVOR's revision, because a merge changes that row.
	Revision int64
}

// MergeStoryEntity moves every reference from one entity to another and removes the absorbed row.
//
// # FR-030's 「用户可合并重复实体并保留别名」
//
// Two extractions of one character arrive as two rows, each with the names and events its own chapter
// gave it. Merging is the user's act — the domain has recorded since WP-05 that a canonical-name
// collision is 「报告而非合并」, and this is the command that lets a person decide — and what it
// does is keep EVERYTHING: the absorbed entity's aliases, its event participations, its character
// states and its original-text evidence all move to the survivor.
//
// # What is refused
//
//   - merging an entity into itself, which would delete it;
//   - a survivor that does not exist, or one whose revision moved (the repository's guard);
//   - an absorbed entity that is LOCKED. A lock is a user's statement that this row is settled, and
//     a merge is exactly the kind of change the lock exists to prevent — the same refusal
//     `AddStoryEntityAlias` makes for the same reason.
//
// # What it does NOT do
//
// It does not turn the absorbed entity's canonical name into an alias of the survivor. Which name a
// merged character should be known by is the decision the caller made by naming the survivor, and
// adding the other name silently would be this command making it for them. The absorbed name comes
// back in the result so a UI can offer it.
func (s *Service) MergeStoryEntity(ctx context.Context, request MergeStoryEntityRequest) (MergeEntitiesResult, error) {
	if !s.Available() {
		return MergeEntitiesResult{}, storageFailure()
	}
	survivorID := strings.TrimSpace(request.SurvivorID)
	absorbedID := strings.TrimSpace(request.AbsorbedID)
	if survivorID == "" || absorbedID == "" {
		return MergeEntitiesResult{}, storydomain.InvalidError("A merge needs both entities.")
	}
	if survivorID == absorbedID {
		return MergeEntitiesResult{}, storydomain.InvalidError("An entity cannot be merged into itself.")
	}
	// Both rows are read first so the refusals below are about the STORED state rather than about what
	// the caller believes, which is the same order every command in this file keeps.
	survivor, err := s.repository.GetStoryEntity(ctx, survivorID)
	if err != nil {
		return MergeEntitiesResult{}, err
	}
	absorbed, err := s.repository.GetStoryEntity(ctx, absorbedID)
	if err != nil {
		return MergeEntitiesResult{}, err
	}
	if survivor.ProjectID != absorbed.ProjectID {
		return MergeEntitiesResult{}, storydomain.InvalidError("Only entities of the same project can be merged.")
	}
	// A lock is a user's statement that a row is settled, and a merge is exactly the change it exists
	// to prevent. The refusal is the one AddStoryEntityAlias makes for the same reason, and it names
	// WHICH entity is locked because the two need different actions.
	if absorbed.Status == storydomain.FactLocked {
		return MergeEntitiesResult{}, storydomain.ConflictError("The entity being absorbed is locked, so it cannot be merged. Unlock it first.")
	}
	if survivor.Status == storydomain.FactLocked {
		return MergeEntitiesResult{}, storydomain.ConflictError("The surviving entity is locked, so it cannot be merged. Unlock it first.")
	}
	result, err := s.repository.MergeEntities(ctx, survivorID, absorbedID, request.Revision)
	if err != nil {
		return MergeEntitiesResult{}, err
	}
	if result.AbsorbedName == "" {
		result.AbsorbedName = absorbed.CanonicalName
	}
	// Section 17's StoryFactAccepted, which is the vocabulary's name for a fact-layer change a person
	// made. A merge has no type of its own and this command does NOT invent one: the list is closed
	// and §17 defines it, so a new name would be this package writing a specification. `StoryFactAccepted`
	// is the honest fit — the survivor's fact set changed, and the event names the survivor as its
	// aggregate, which is what a reader following the stream needs.
	//
	// Best effort: the rows are committed, so a failed announcement must not tell the caller the merge
	// did not happen.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          event.StoryFactAccepted,
		AggregateType: event.AggregateStoryEntity,
		AggregateID:   survivorID,
		ProjectID:     survivor.ProjectID,
	})
	return result, nil
}

func (s *Service) MergeChapter(ctx context.Context, request MergeChapterRequest) (storydomain.Chapter, error) {
	if !s.Available() {
		return storydomain.Chapter{}, storageFailure()
	}
	first, err := s.repository.GetChapter(ctx, request.FirstChapterID)
	if err != nil {
		return storydomain.Chapter{}, err
	}
	second, err := s.repository.GetChapter(ctx, request.SecondChapterID)
	if err != nil {
		return storydomain.Chapter{}, err
	}
	if second.Ordinal != first.Ordinal+1 {
		return storydomain.Chapter{}, storydomain.InvalidError("Only the chapter immediately after this one can be merged into it.")
	}
	// The ranges must be adjacent as well as the ordinals. A gap between them is a
	// boundary a user moved earlier, and merging across it would silently swallow
	// the text in between.
	if second.StartOffset != first.EndOffset {
		return storydomain.Chapter{}, storydomain.InvalidError("Those chapters are not adjacent in the text, so merging them would skip a passage.")
	}
	now := s.now()
	merged := first
	merged.EndOffset = second.EndOffset
	merged.SourceKind = storydomain.ChapterManual
	merged.Status = storydomain.ChapterEdited
	merged.UpdatedAt = now
	if err := merged.Validate(); err != nil {
		return storydomain.Chapter{}, err
	}
	if err := s.repository.MergeChapters(ctx, merged, second.ID, request.Revision); err != nil {
		return storydomain.Chapter{}, err
	}
	merged.Revision = request.Revision + 1
	return merged, nil
}
