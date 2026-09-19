package extraction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	extractiondomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/extraction"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// store.go is the write path: a validated document becomes candidate rows.
//
// Three properties hold here, and each is why the code is arranged the way it is.
//
// First, a REFERENCE must resolve. The model writes `"locationRef": "hall"`, a
// local key, because it cannot know an identifier the repository has not minted.
// This file mints the identifiers and resolves the keys, and refuses the whole
// document if a key names nothing. Dropping the dangling reference would be
// quieter and worse: a relation whose endpoint vanished is a fact the user never
// sees and therefore cannot correct.
//
// Second, a reference must resolve to the RIGHT KIND of thing. An event's
// locationRef must name an entity; a relation endpoint must name an entity; a
// validFromEventRef must name an event. The schema cannot express that
// cross-field rule, so it is enforced here, and the check is by SET MEMBERSHIP
// rather than by looking at the ref's own type — a ref that names an event is
// not a location even if some entity shares the spelling.
//
// Third, the evidence carries a REAL span. The chapter's text is searched for the
// names the model proposed, and the offsets recorded are where those names were
// actually found. When a name is not found, the evidence row records the whole
// chapter with no offsets rather than a guessed position: DOMAIN_MODEL §6.6 says
// the offsets index the version's text, and an offset that pointed at the wrong
// passage would make the evidence worse than absent.

// store writes a validated document as candidate facts.
func (s *Service) store(ctx context.Context, chapter ChapterText, document extractiondomain.Document) (Result, error) {
	entityRefs, eventRefs := document.Refs()
	if err := checkReferences(document, entityRefs, eventRefs); err != nil {
		return Result{}, err
	}
	result := Result{ChapterID: chapter.Chapter.ID, Summary: document.Summary}
	projectID := chapter.ProjectID

	// Entities first, because an event's location and a relation's endpoints are
	// entity ids. The map is ref to the id this process minted, and it is what
	// makes the resolution above possible.
	entityIDs := make(map[string]string, len(document.Entities))
	for _, proposal := range document.Entities {
		record, err := s.story.CreateStoryEntity(ctx, appstory.CreateStoryEntityRequest{
			ProjectID:     projectID,
			Type:          story.EntityType(proposal.Type),
			CanonicalName: proposal.CanonicalName,
			// Extraction reads the original document, so the scope says so. The
			// alternative — leaving it empty — would take the schema's default,
			// which happens to be the same value, and then a later change to that
			// default would silently reclassify every extracted fact.
			SourceScope: story.ScopeOriginal,
		})
		if err != nil {
			return Result{}, err
		}
		entityIDs[proposal.Ref] = record.ID
		result.Entities++
		if err := s.writeEntityEvidence(ctx, chapter, record.ID, proposal); err != nil {
			return Result{}, err
		}
		result.Evidence++
		for _, alias := range proposal.Aliases {
			span := findSpan(chapter.Text, alias)
			if _, err := s.story.AddStoryEntityAlias(ctx, appstory.AddStoryEntityAliasRequest{
				StoryEntityID:   record.ID,
				Alias:           alias,
				SourceChapterID: chapter.Chapter.ID,
				SourceStart:     span.start,
				SourceEnd:       span.end,
			}); err != nil {
				return Result{}, err
			}
			result.Aliases++
		}
	}

	eventIDs := make(map[string]string, len(document.Events))
	for index, proposal := range document.Events {
		locationID := ""
		if proposal.LocationRef != "" {
			locationID = entityIDs[proposal.LocationRef]
		}
		record, err := s.story.CreateStoryEvent(ctx, appstory.CreateStoryEventRequest{
			ProjectID:        projectID,
			ChapterID:        chapter.Chapter.ID,
			Ordinal:          index,
			Name:             proposal.Name,
			Description:      proposal.Description,
			EventType:        proposal.EventType,
			StoryTimeText:    proposal.StoryTimeText,
			StoryTimeOrder:   proposal.StoryTimeOrder,
			LocationEntityID: locationID,
			CauseSummary:     proposal.CauseSummary,
			ResultSummary:    proposal.ResultSummary,
			Importance:       proposal.Importance,
			Confidence:       proposal.Confidence,
			SourceScope:      story.ScopeOriginal,
		})
		if err != nil {
			return Result{}, err
		}
		eventIDs[proposal.Ref] = record.ID
		result.Events++
		if err := s.writeEventEvidence(ctx, chapter, record.ID, proposal); err != nil {
			return Result{}, err
		}
		result.Evidence++
		for _, participant := range proposal.Participants {
			if _, err := s.story.AddStoryEventParticipant(ctx, appstory.AddStoryEventParticipantRequest{
				StoryEventID:  record.ID,
				StoryEntityID: entityIDs[participant.EntityRef],
				Role:          story.ParticipantRole(participant.Role),
				StateBefore:   participant.StateBefore,
				StateAfter:    participant.StateAfter,
			}); err != nil {
				return Result{}, err
			}
			result.Participants++
		}
	}

	for _, proposal := range document.Relations {
		fromID, toID := "", ""
		if proposal.ValidFromEventRef != "" {
			fromID = eventIDs[proposal.ValidFromEventRef]
		}
		if proposal.ValidToEventRef != "" {
			toID = eventIDs[proposal.ValidToEventRef]
		}
		record, err := s.story.CreateStoryRelation(ctx, appstory.CreateStoryRelationRequest{
			ProjectID:        projectID,
			Type:             story.RelationType(proposal.Type),
			SourceEntityType: string(story.EntityConcept),
			SourceEntityID:   entityIDs[proposal.SourceRef],
			TargetEntityType: string(story.EntityConcept),
			TargetEntityID:   entityIDs[proposal.TargetRef],
			ValidFromEventID: fromID,
			ValidToEventID:   toID,
			Confidence:       proposal.Confidence,
			SourceScope:      story.ScopeOriginal,
		})
		if err != nil {
			return Result{}, err
		}
		result.Relations++
		if err := s.writeRelationEvidence(ctx, chapter, record.ID, proposal); err != nil {
			return Result{}, err
		}
		result.Evidence++
	}

	result.Extracted = len(document.Entities) + len(document.Events) + len(document.Relations)
	return result, nil
}

// checkReferences refuses a document that names a ref nothing defines.
//
// It runs before any write, so a refusal leaves the graph untouched. The two
// checks below are the ones the schema cannot make, because JSON Schema has no
// way to say "this value must also appear in that other array".
func checkReferences(document extractiondomain.Document, entityRefs, eventRefs map[string]bool) error {
	var violations []extractiondomain.Violation
	for index, event := range document.Events {
		if event.LocationRef != "" && !entityRefs[event.LocationRef] {
			violations = append(violations, extractiondomain.Violation{
				Path:    "/events/" + itoa(index) + "/locationRef",
				Message: "must name an entity defined in this document",
			})
		}
		for participantIndex, participant := range event.Participants {
			if !entityRefs[participant.EntityRef] {
				violations = append(violations, extractiondomain.Violation{
					Path:    "/events/" + itoa(index) + "/participants/" + itoa(participantIndex) + "/entityRef",
					Message: "must name an entity defined in this document",
				})
			}
		}
	}
	for index, relation := range document.Relations {
		if !entityRefs[relation.SourceRef] {
			violations = append(violations, extractiondomain.Violation{
				Path:    "/relations/" + itoa(index) + "/sourceRef",
				Message: "must name an entity defined in this document",
			})
		}
		if !entityRefs[relation.TargetRef] {
			violations = append(violations, extractiondomain.Violation{
				Path:    "/relations/" + itoa(index) + "/targetRef",
				Message: "must name an entity defined in this document",
			})
		}
		// The event references are checked against the EVENT set, not the entity
		// set: a key that names an entity is not a valid event reference, even
		// though it resolves to something.
		if relation.ValidFromEventRef != "" && !eventRefs[relation.ValidFromEventRef] {
			violations = append(violations, extractiondomain.Violation{
				Path:    "/relations/" + itoa(index) + "/validFromEventRef",
				Message: "must name an event defined in this document",
			})
		}
		if relation.ValidToEventRef != "" && !eventRefs[relation.ValidToEventRef] {
			violations = append(violations, extractiondomain.Violation{
				Path:    "/relations/" + itoa(index) + "/validToEventRef",
				Message: "must name an event defined in this document",
			})
		}
	}
	if len(violations) > 0 {
		return &extractiondomain.Error{Violations: violations}
	}
	return nil
}

// span is where a proposal was found in the chapter text, in RUNE offsets.
//
// Runes rather than bytes, because the domain's offsets index the normalized
// text a reader sees and every offset in this system is compared with a rune
// position. A byte offset would be wrong by a factor of three for Chinese text,
// which is the case these offsets matter most for.
type span struct {
	start *int
	end   *int
}

// findSpan locates a name in the chapter and returns its rune range.
//
// The FIRST occurrence is taken rather than a model-supplied position, because
// the model's arithmetic is not verifiable and a wrong offset is worse than
// none. When the name is absent — the model may have normalised a name it read —
// no span is returned, and the evidence row then records the chapter without a
// range, which §6.6 allows.
func findSpan(text, name string) span {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return span{}
	}
	index := strings.Index(text, trimmed)
	if index < 0 {
		return span{}
	}
	// Index is a byte offset, so the prefix is measured in runes to convert it.
	start := utf8.RuneCountInString(text[:index])
	end := start + utf8.RuneCountInString(trimmed)
	return span{start: &start, end: &end}
}

// writeEntityEvidence records where an entity was found.
func (s *Service) writeEntityEvidence(ctx context.Context, chapter ChapterText, entityID string, proposal extractiondomain.Entity) error {
	location := findSpan(chapter.Text, proposal.CanonicalName)
	return s.recordEvidence(ctx, chapter, story.FactEntity, entityID, location, excerptHash(chapter.Text, location))
}

// writeEventEvidence records where an event was found.
//
// An event has no single name in the text to point at, so the evidence is the
// chapter without a range. That is the honest record: the event exists in this
// chapter, and the offsets that would say exactly where are not something this
// layer can compute from a label.
func (s *Service) writeEventEvidence(ctx context.Context, chapter ChapterText, eventID string, proposal extractiondomain.Event) error {
	location := findSpan(chapter.Text, proposal.Name)
	return s.recordEvidence(ctx, chapter, story.FactEvent, eventID, location, excerptHash(chapter.Text, location))
}

// writeRelationEvidence records where a relation was found. Like an event, a
// relation is a statement rather than a phrase, so it carries the chapter and,
// when the source entity's name appears, that name's range.
func (s *Service) writeRelationEvidence(ctx context.Context, chapter ChapterText, relationID string, proposal extractiondomain.Relation) error {
	location := span{}
	return s.recordEvidence(ctx, chapter, story.FactRelation, relationID, location, excerptHash(chapter.Text, location))
}

// recordEvidence writes one fact-source row.
//
// The kind is agent_inference rather than text: a fact a model proposed is an
// inference from the text, not something the text states in those words. Calling
// it 'text' would tell a later reader that the passage says what the fact says.
func (s *Service) recordEvidence(ctx context.Context, chapter ChapterText, factType story.FactType, factID string, location span, quoteHash string) error {
	_, err := s.story.RecordFactSource(ctx, appstory.RecordFactSourceRequest{
		FactType:                factType,
		FactID:                  factID,
		ChapterID:               chapter.Chapter.ID,
		SourceDocumentVersionID: chapter.SourceDocumentVersionID,
		StartOffset:             location.start,
		EndOffset:               location.end,
		QuoteHash:               quoteHash,
		SourceKind:              story.SourceKindAgentInference,
	})
	return err
}

// excerptHash is the digest of the passage the offsets point at.
//
// It is what lets the pipeline notice the text changed under a fact: the reader
// re-reads the range, hashes it, and compares. When there is no range the whole
// chapter is hashed, so the value is still a usable comparison and still not a
// copy of the text — §6.6 permits a limited excerpt, and a hash is smaller than
// the excerpt it stands for.
func excerptHash(text string, location span) string {
	excerpt := text
	if location.start != nil && location.end != nil {
		runes := []rune(text)
		start, end := *location.start, *location.end
		if start >= 0 && end <= len(runes) && start <= end {
			excerpt = string(runes[start:end])
		}
	}
	sum := sha256.Sum256([]byte(excerpt))
	return hex.EncodeToString(sum[:])
}

// There is no event on the write path, and that is a decision rather than an
// omission.
//
// DOMAIN_MODEL section 17 lists the domain events, the list is closed, and
// internal/domain/event pins all 28 names with a test. The list has
// StoryFactAccepted and StoryFactConflictOpened and nothing for a proposal,
// which is coherent: an extraction writes CANDIDATES, and a candidate is not yet
// a fact. Announcing one would put a row in the stream that says a fact changed
// when what happened is that a model suggested something.
//
// The user-visible signal is the result this command returns. WP-07 may add a
// stage event when extraction moves under the runtime, and that event belongs to
// the stage vocabulary rather than to this list.

// itoa renders an index for a violation path.
//
// A JSON Pointer's array index is part of the path the repair prompt receives, so
// it is spelled the way a pointer spells it rather than with fmt, which would
// pull a formatting package into a path builder.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
