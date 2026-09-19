package story

import (
	"context"
	"strings"

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
