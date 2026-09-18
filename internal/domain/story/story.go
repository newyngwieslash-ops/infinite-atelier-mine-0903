// Package story owns the source-document, chapter and story-fact vocabulary and
// invariants of docs/DOMAIN_MODEL.md §5 and §6.
//
// Section 5 describes how an imported document becomes normalized text and then
// chapters, and section 6 describes the fact layer built on top of those
// chapters: entities, events, relations, the evidence each fact cites and the
// conflicts between facts. The fact layer is deliberately independent of canvas
// state and chat memory — PRD FR-030 asks that the event graph be "独立于聊天
// 记忆和画布状态" — which is why nothing in this package references a canvas node
// or a chat session.
//
// The package performs no I/O, mints no identifier and reads no clock: the
// application layer supplies identifiers, timestamps and hashes (ADR-0005,
// DOMAIN_MODEL §2.1-§2.2). Revision counters are carried but not validated
// here, because §2.3 makes revision a concurrency token the write path assigns
// and increments, not a property a freshly built entity can state. The closed
// vocabularies below are the exact sets migration 000007 pins in SQL CHECK
// constraints, so a value admitted here is one the database will also store and
// a value refused here is one SQL refuses too.
package story

import (
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// MaxNameLength mirrors the length(name) BETWEEN 1 AND 200 checks that
// migration 000007 places on source_documents.name, story_entities
// .canonical_name, story_entity_aliases.alias and story_events.name.
const MaxNameLength = 200

// DocumentType is what kind of text a source document holds (DOMAIN_MODEL §5.1).
type DocumentType string

const (
	DocumentNovel      DocumentType = "novel"
	DocumentStory      DocumentType = "story"
	DocumentScreenplay DocumentType = "screenplay"
	DocumentOutline    DocumentType = "outline"
	DocumentNotes      DocumentType = "notes"
)

// DocumentTypes lists the documented document types in the schema's order.
var DocumentTypes = []DocumentType{
	DocumentNovel, DocumentStory, DocumentScreenplay, DocumentOutline, DocumentNotes,
}

// IsValidDocumentType reports whether a document type may be persisted.
func IsValidDocumentType(value DocumentType) bool {
	for _, candidate := range DocumentTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// DocumentStatus is the lifecycle of a source document.
//
// It is not the version vocabulary: a document's trash state is about the
// document row, while its versions carry no review status at all (§5.2 has no
// status column, because an import is a fact rather than a proposal).
type DocumentStatus string

const (
	DocumentActive   DocumentStatus = "active"
	DocumentArchived DocumentStatus = "archived"
	DocumentTrashed  DocumentStatus = "trashed"
)

// DocumentStatuses lists the documented document statuses in the schema's
// order.
var DocumentStatuses = []DocumentStatus{DocumentActive, DocumentArchived, DocumentTrashed}

// IsValidDocumentStatus reports whether a document status may be persisted.
func IsValidDocumentStatus(value DocumentStatus) bool {
	for _, candidate := range DocumentStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// SourceDocument is an imported text that the story graph is extracted from
// (DOMAIN_MODEL §5.1).
type SourceDocument struct {
	ID        string
	ProjectID string
	Type      DocumentType
	Name      string
	// CurrentVersionID is empty until the first version is committed. §5.2
	// keeps every version beside the others, so this names the newest of them
	// rather than standing in for them.
	CurrentVersionID string
	Status           DocumentStatus
	// DeletedAt is zero for a live row. Soft delete is §2.4's default, which
	// the schema mirrors with deleted_at/deleted_by columns.
	DeletedAt time.Time
	DeletedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int64
}

// Validate checks a document before it is stored.
func (d SourceDocument) Validate() error {
	if strings.TrimSpace(d.ProjectID) == "" {
		return InvalidError("A source document must belong to a project.")
	}
	if !IsValidDocumentType(d.Type) {
		return InvalidError("The document type is not recognised.")
	}
	trimmed := strings.TrimSpace(d.Name)
	if trimmed == "" {
		return InvalidError("A source document needs a name.")
	}
	if len([]rune(trimmed)) > MaxNameLength {
		return InvalidError("The document name is too long.")
	}
	if !IsValidDocumentStatus(d.Status) {
		return InvalidError("The document status is not recognised.")
	}
	return nil
}

// SourceDocumentVersion is one import of a document (DOMAIN_MODEL §5.2).
//
// §5.2's invariants are structural here. The original upload and the normalized
// text are two separate references rather than one rewritten field, which is
// what makes "原文件和规范化文本均可追踪" hold; and a replacement import is a new
// row with the next version number, which is what makes "替换文档不覆盖旧版本"
// hold. The normalized text is the artifact every chapter offset and every fact
// quote indexes, so a version without it could not carry evidence and is
// refused.
//
// Both file references are content hashes rather than paths. Migration 000007's
// header states the reason — "reference committed objects by hash, which is what
// makes a chapter unable to point at bytes that were never stored" — and the
// FileStore owns the layout, so a domain that carried a path would be holding a
// fact only the store may decide (ADR-0005 §3).
type SourceDocumentVersion struct {
	ID               string
	SourceDocumentID string
	VersionNumber    int
	// PhysicalFileID is the committed object holding the original upload. It is
	// empty when a document was typed or pasted rather than imported from a
	// file, which §5.2 allows by marking the field nullable.
	PhysicalFileID string
	// NormalizedTextFileID is the committed object holding the canonical text
	// that chapter offsets index. It is required, because a version whose text
	// is missing could not have valid chapters.
	NormalizedTextFileID string
	// ContentHash is the digest of the normalized text. §5.2's "相同内容哈希需
	// 提示重复" is a lookup on this column, so it is stored even when the caller
	// also keeps the file-level hash. It is optional because a version may be
	// recorded before its digest is computed, in which case the duplicate check
	// simply cannot run yet.
	ContentHash string
	MIMEType    string
	Encoding    string
	CharCount   int
	// ImportMetadataJSON carries parser settings. It is display metadata, not
	// state: §2.6 forbids JSON standing in for a queryable fact, and nothing
	// here is queried.
	ImportMetadataJSON string
	CreatedByType      CreatedByType
	CreatedAt          time.Time
}

// CreatedByType records who produced a source document version.
//
// It aliases the shared vocabulary rather than declaring a second copy: §2.5
// defines one producer set for every versioned record, and two spellings would
// drift apart. The values are the schema's CHECK list exactly.
type CreatedByType = versioning.CreatedByType

const (
	CreatedByUser      = versioning.CreatedByUser
	CreatedByAgent     = versioning.CreatedByAgent
	CreatedByMigration = versioning.CreatedByMigration
	CreatedBySystem    = versioning.CreatedBySystem
)

// CreatedByTypes lists the documented producers in the schema's order.
var CreatedByTypes = versioning.CreatedByTypes

// IsValidCreatedByType reports whether a producer kind may be persisted.
func IsValidCreatedByType(value CreatedByType) bool {
	return versioning.IsValidCreatedByType(value)
}

// Validate checks a document version before it is stored.
func (v SourceDocumentVersion) Validate() error {
	if strings.TrimSpace(v.SourceDocumentID) == "" {
		return InvalidError("A document version must belong to a source document.")
	}
	if v.VersionNumber < 1 {
		return InvalidError("A document version number starts at one, so an import never overwrites the version before it.")
	}
	if strings.TrimSpace(v.NormalizedTextFileID) == "" {
		return InvalidError("A document version needs the normalized text its chapters index.")
	}
	if !isLowerHexDigest(v.NormalizedTextFileID) {
		return InvalidError("A document version must reference a committed object by its SHA-256 hash.")
	}
	if v.PhysicalFileID != "" && !isLowerHexDigest(v.PhysicalFileID) {
		return InvalidError("The original file must be referenced by its SHA-256 hash.")
	}
	// The hash is optional because a version may be stored before its digest is
	// computed, but a value that is present must be a digest.
	if v.ContentHash != "" && !isLowerHexDigest(v.ContentHash) {
		return InvalidError("The content hash must be a SHA-256 digest or empty.")
	}
	if v.CharCount < 0 {
		return InvalidError("A character count cannot be negative.")
	}
	if !IsValidCreatedByType(v.CreatedByType) {
		return InvalidError("The document version producer is not recognised.")
	}
	return nil
}

// ChapterStatus records how a chapter boundary came to be trusted.
//
// PRD FR-020 asks that a detected split be correctable by hand ("章节拆分可人工
// 修正并保存"), so the three values distinguish a machine guess from a boundary a
// user looked at: 'detected' is what the import pipeline writes, 'confirmed' is
// a boundary a user accepted unchanged and 'edited' is one a user moved. §5.3's
// "用户确认后的边界更改创建新的章节集合版本或修订记录" then uses the distinction
// to decide what a further correction writes.
type ChapterStatus string

const (
	ChapterDetected  ChapterStatus = "detected"
	ChapterConfirmed ChapterStatus = "confirmed"
	ChapterEdited    ChapterStatus = "edited"
)

// ChapterStatuses lists the documented chapter statuses in the schema's order.
var ChapterStatuses = []ChapterStatus{ChapterDetected, ChapterConfirmed, ChapterEdited}

// IsValidChapterStatus reports whether a chapter status may be persisted.
func IsValidChapterStatus(value ChapterStatus) bool {
	for _, candidate := range ChapterStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// Chapter is one section of a document version (DOMAIN_MODEL §5.3).
//
// §5.2 requires that "任何 Chapter 必须指向具体 SourceDocumentVersion", so the
// reference is a version rather than the document: a re-import lands in a new
// version and the existing chapters keep pointing at the text they were carved
// from. Offsets index that version's normalized text.
type Chapter struct {
	ID                      string
	SourceDocumentVersionID string
	Ordinal                 int
	Title                   string
	StartOffset             int
	EndOffset               int
	// ContentHash is the digest of the chapter's slice of the normalized text.
	// It is optional so a chapter may be stored before its digest is computed,
	// but a present value must be a digest — a fragment would make two chapters'
	// hashes incomparable.
	ContentHash string
	Status      ChapterStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Revision    int64
}

// Validate checks a chapter before it is stored.
func (c Chapter) Validate() error {
	if strings.TrimSpace(c.SourceDocumentVersionID) == "" {
		return InvalidError("A chapter must belong to a document version.")
	}
	if c.Ordinal < 1 {
		return InvalidError("A chapter ordinal starts at one.")
	}
	if c.StartOffset < 0 {
		return InvalidError("A chapter start offset cannot be negative.")
	}
	if c.EndOffset < c.StartOffset {
		return InvalidError("A chapter cannot end before it starts.")
	}
	if c.ContentHash != "" && !isLowerHexDigest(c.ContentHash) {
		return InvalidError("The chapter content hash must be a SHA-256 digest or empty.")
	}
	if !IsValidChapterStatus(c.Status) {
		return InvalidError("The chapter status is not recognised.")
	}
	return nil
}

// Contains reports whether an offset into the normalized text falls inside the
// chapter.
//
// The range is half-open, [StartOffset, EndOffset). §5.3 requires chapter
// offsets not to overlap, and a half-open range is what makes that hold at a
// shared boundary: chapter one ending at 1000 and chapter two starting at 1000
// touch without either claiming the other's first character, so an evidence
// offset resolves to exactly one chapter. An empty chapter, where the two
// offsets are equal, therefore contains nothing — which is the schema's
// end_offset >= start_offset case and not a defect.
func (c Chapter) Contains(offset int) bool {
	return offset >= c.StartOffset && offset < c.EndOffset
}

// EntityType is what a story entity names (DOMAIN_MODEL §6.1).
//
// The six values are §6.1's list. PRD FR-030 names other nouns as well, but
// they are not entity kinds: an event, a relationship and a character state are
// rows of their own tables, so folding them into this set would give one
// concept two homes. A new kind would need a CHECK migration and a review path,
// which is why the set stays closed.
type EntityType string

const (
	EntityCharacter    EntityType = "character"
	EntityLocation     EntityType = "location"
	EntityOrganization EntityType = "organization"
	EntityProp         EntityType = "prop"
	EntityConcept      EntityType = "concept"
	EntityTime         EntityType = "time"
)

// EntityTypes lists the documented entity types in the schema's order.
var EntityTypes = []EntityType{
	EntityCharacter, EntityLocation, EntityOrganization, EntityProp, EntityConcept, EntityTime,
}

// IsValidEntityType reports whether an entity type may be persisted.
func IsValidEntityType(value EntityType) bool {
	for _, candidate := range EntityTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// FactStatus is the review state shared by every fact the graph stores:
// entities, events, relations and character states.
//
// PRD FR-030 requires that an extracted candidate pass a user or rule gate
// before it counts as a confirmed fact ("候选事实在进入'已确认事实'前需通过用户或
// 规则校验"), which is why 'candidate' and 'accepted' are two states rather than
// one: the first is what extraction may write on its own and the second is what
// only a user command or an explicit rule may reach. 'locked' is the state a
// user pins so a later pass cannot revise it.
type FactStatus string

const (
	FactCandidate FactStatus = "candidate"
	FactAccepted  FactStatus = "accepted"
	FactRejected  FactStatus = "rejected"
	FactLocked    FactStatus = "locked"
)

// FactStatuses lists the documented fact statuses in the schema's order.
var FactStatuses = []FactStatus{FactCandidate, FactAccepted, FactRejected, FactLocked}

// IsValidFactStatus reports whether a fact status may be persisted.
func IsValidFactStatus(value FactStatus) bool {
	for _, candidate := range FactStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// SourceScope records where a fact came from relative to the adaptation.
//
// §6.1 lists the three values and §6.3 and §6.5 carry the same column. Scope is
// not the same question as the evidence kind in SourceKind: a fact read out of
// a chapter is 'original' text even when a user typed the chapter in, because
// the scope describes the material rather than the act.
type SourceScope string

const (
	ScopeOriginal   SourceScope = "original"
	ScopeAdaptation SourceScope = "adaptation"
	ScopeUser       SourceScope = "user"
)

// SourceScopes lists the documented scopes in the schema's order.
var SourceScopes = []SourceScope{ScopeOriginal, ScopeAdaptation, ScopeUser}

// IsValidSourceScope reports whether a source scope may be persisted.
func IsValidSourceScope(value SourceScope) bool {
	for _, candidate := range SourceScopes {
		if candidate == value {
			return true
		}
	}
	return false
}

// StoryEntity is a character, place or thing the story refers to
// (DOMAIN_MODEL §6.1).
//
// A conflict between two spellings is reported rather than merged, so no rule
// here forces canonical names to be unique: §6.2 says a clash "需提示，不强制静默
// 合并", which makes the merge a user decision. The schema's index on
// (project_id, entity_type, canonical_name) is a lookup index, not a unique
// constraint, for the same reason.
type StoryEntity struct {
	ID        string
	ProjectID string
	Type      EntityType
	// CanonicalName is the name the graph prefers. Aliases live in
	// StoryEntityAlias rows rather than in a JSON column, because the
	// alternative spellings are searched individually.
	CanonicalName string
	Status        FactStatus
	SourceScope   SourceScope
	// CurrentProfileVersionID is empty until a profile version is approved.
	CurrentProfileVersionID string
	DeletedAt               time.Time
	DeletedBy               string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	Revision                int64
}

// Validate checks an entity before it is stored.
func (e StoryEntity) Validate() error {
	if strings.TrimSpace(e.ProjectID) == "" {
		return InvalidError("A story entity must belong to a project.")
	}
	if !IsValidEntityType(e.Type) {
		return InvalidError("The entity type is not recognised.")
	}
	trimmed := strings.TrimSpace(e.CanonicalName)
	if trimmed == "" {
		return InvalidError("A story entity needs a canonical name.")
	}
	if len([]rune(trimmed)) > MaxNameLength {
		return InvalidError("The entity name is too long.")
	}
	if !IsValidFactStatus(e.Status) {
		return InvalidError("The entity status is not recognised.")
	}
	if !IsValidSourceScope(e.SourceScope) {
		return InvalidError("The entity source scope is not recognised.")
	}
	return nil
}

// StoryEntityAlias is an alternative name for an entity (DOMAIN_MODEL §6.2).
//
// The optional offsets record where in a chapter the name appeared, which is
// what §6.2's source_start_offset/source_end_offset columns are for. They are
// pointers because absence is meaningful: §6.2 marks both nullable, an alias a
// user typed has no source span at all, and a zero value would be
// indistinguishable from a real span recorded at the very start of the text.
type StoryEntityAlias struct {
	ID              string
	StoryEntityID   string
	Alias           string
	SourceChapterID string
	SourceStart     *int
	SourceEnd       *int
	CreatedAt       time.Time
}

// Validate checks an alias before it is stored.
func (a StoryEntityAlias) Validate() error {
	if strings.TrimSpace(a.StoryEntityID) == "" {
		return InvalidError("An alias must belong to a story entity.")
	}
	trimmed := strings.TrimSpace(a.Alias)
	if trimmed == "" {
		return InvalidError("An alias needs text.")
	}
	if len([]rune(trimmed)) > MaxNameLength {
		return InvalidError("The alias is too long.")
	}
	if err := validateOffsetPair(a.SourceStart, a.SourceEnd); err != nil {
		return err
	}
	return nil
}

// ParticipantRole is how an entity took part in an event (DOMAIN_MODEL §6.4).
type ParticipantRole string

const (
	ParticipantActor    ParticipantRole = "actor"
	ParticipantTarget   ParticipantRole = "target"
	ParticipantWitness  ParticipantRole = "witness"
	ParticipantOwner    ParticipantRole = "owner"
	ParticipantAffected ParticipantRole = "affected"
	ParticipantOther    ParticipantRole = "other"
)

// ParticipantRoles lists the documented roles in the schema's order.
var ParticipantRoles = []ParticipantRole{
	ParticipantActor, ParticipantTarget, ParticipantWitness, ParticipantOwner, ParticipantAffected, ParticipantOther,
}

// IsValidParticipantRole reports whether a participant role may be persisted.
func IsValidParticipantRole(value ParticipantRole) bool {
	for _, candidate := range ParticipantRoles {
		if candidate == value {
			return true
		}
	}
	return false
}

// StoryEvent is something that happens in the story (DOMAIN_MODEL §6.3).
type StoryEvent struct {
	ID        string
	ProjectID string
	// ChapterID is empty for an event that belongs to the episode outline
	// rather than to an imported chapter.
	ChapterID string
	// Ordinal positions the event among its neighbours. The schema's check is
	// ordinal >= 0 rather than the >= 1 that chapters use, so zero is a valid
	// first position for an event that was placed before any ordering pass ran.
	Ordinal     int
	Name        string
	Description string
	// EventType is free text because the specification gives no closed set:
	// §6.3 lists the field without values, and an extraction may propose a kind
	// the vocabulary does not yet have.
	EventType string
	// StoryTimeText is the diegetic time as the text states it ("three days
	// later"); StoryTimeOrder is the position on the story clock once a pass
	// has ordered the events. They are separate because the prose cannot be
	// sorted and the order cannot be quoted.
	StoryTimeText  string
	StoryTimeOrder *int
	// LocationEntityID references a location entity, empty when the event's
	// place is not (yet) a graph node.
	LocationEntityID string
	CauseSummary     string
	ResultSummary    string
	// Importance is free text for the same reason as EventType: §6.3 sets no
	// scale, and inventing one here would reject data the schema stores.
	Importance string
	// Confidence is the extractor's own score in [0, 1]. The schema gives it no
	// check, so this package does not impose one either.
	Confidence  float64
	Status      FactStatus
	SourceScope SourceScope
	// CreatedByAgentRunID names the extraction run that proposed the event,
	// empty when a user wrote it. §6.3 marks it nullable for that reason, and
	// the migration keeps it a plain column until the agent_run tables exist.
	CreatedByAgentRunID string
	DeletedAt           time.Time
	DeletedBy           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Revision            int64
}

// Validate checks an event before it is stored.
func (e StoryEvent) Validate() error {
	if strings.TrimSpace(e.ProjectID) == "" {
		return InvalidError("A story event must belong to a project.")
	}
	if e.Ordinal < 0 {
		return InvalidError("An event ordinal cannot be negative.")
	}
	trimmed := strings.TrimSpace(e.Name)
	if trimmed == "" {
		return InvalidError("A story event needs a name.")
	}
	if len([]rune(trimmed)) > MaxNameLength {
		return InvalidError("The event name is too long.")
	}
	if !IsValidFactStatus(e.Status) {
		return InvalidError("The event status is not recognised.")
	}
	if !IsValidSourceScope(e.SourceScope) {
		return InvalidError("The event source scope is not recognised.")
	}
	return nil
}

// StoryEventParticipant links an entity to an event (DOMAIN_MODEL §6.4).
//
// The row carries the state on both sides of the event, which is the per-event
// form of the spanning record CharacterState holds in §6.8. There is no
// identifier of its own: the schema makes (event, entity, role) the primary key,
// because one entity can take several roles in one event and the roles together
// are the identity.
type StoryEventParticipant struct {
	StoryEventID  string
	StoryEntityID string
	Role          ParticipantRole
	StateBefore   string
	StateAfter    string
	CreatedAt     time.Time
}

// Validate checks a participation link before it is stored.
func (p StoryEventParticipant) Validate() error {
	if strings.TrimSpace(p.StoryEventID) == "" {
		return InvalidError("A participant must belong to a story event.")
	}
	if strings.TrimSpace(p.StoryEntityID) == "" {
		return InvalidError("A participant must name a story entity.")
	}
	if !IsValidParticipantRole(p.Role) {
		return InvalidError("The participant role is not recognised.")
	}
	return nil
}

// RelationType is how two graph nodes relate (DOMAIN_MODEL §6.5).
//
// The set is the union of the two lists that describe it. PRD FR-030 names
// eleven relations (participates_in through related_to) and §6.5's example line
// adds located_in and contradicts; migration 000007 pins all fourteen with
// 'other' as the escape hatch for an extraction that finds a relation the
// vocabulary does not cover. ADR-0007 records the ruling.
type RelationType string

const (
	RelationParticipatesIn RelationType = "participates_in"
	RelationOccursAt       RelationType = "occurs_at"
	RelationCauses         RelationType = "causes"
	RelationPrecedes       RelationType = "precedes"
	RelationReveals        RelationType = "reveals"
	RelationConflictsWith  RelationType = "conflicts_with"
	RelationOwns           RelationType = "owns"
	RelationTransfersTo    RelationType = "transfers_to"
	RelationChangesState   RelationType = "changes_state"
	RelationKnows          RelationType = "knows"
	RelationRelatedTo      RelationType = "related_to"
	RelationLocatedIn      RelationType = "located_in"
	RelationContradicts    RelationType = "contradicts"
	RelationOther          RelationType = "other"
)

// RelationTypes lists the documented relations in the schema's order.
var RelationTypes = []RelationType{
	RelationParticipatesIn, RelationOccursAt, RelationCauses, RelationPrecedes,
	RelationReveals, RelationConflictsWith, RelationOwns, RelationTransfersTo,
	RelationChangesState, RelationKnows, RelationRelatedTo, RelationLocatedIn,
	RelationContradicts, RelationOther,
}

// IsValidRelationType reports whether a relation may be persisted.
func IsValidRelationType(value RelationType) bool {
	for _, candidate := range RelationTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// StoryRelation is one edge of the story graph (DOMAIN_MODEL §6.5).
//
// The two entity-type fields are plain text rather than an EntityType: §6.5
// lists them without a vocabulary, and an edge may join anything the canvas
// relation registry knows about, which is a set this package must not own. The
// registry in internal/domain/project §10.4 is what validates them.
//
// The optional event references bound when the relation held. A relation like
// 'owns' is true only between two events, and without the bounds a later
// continuity query could not tell which scenes it applies to.
type StoryRelation struct {
	ID               string
	ProjectID        string
	Type             RelationType
	SourceEntityType string
	SourceEntityID   string
	TargetEntityType string
	TargetEntityID   string
	ValidFromEventID string
	ValidToEventID   string
	Confidence       float64
	Status           FactStatus
	SourceScope      SourceScope
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Revision         int64
}

// Validate checks a relation before it is stored.
func (r StoryRelation) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return InvalidError("A story relation must belong to a project.")
	}
	if !IsValidRelationType(r.Type) {
		return InvalidError("The relation type is not recognised.")
	}
	if strings.TrimSpace(r.SourceEntityID) == "" || strings.TrimSpace(r.TargetEntityID) == "" {
		return InvalidError("A story relation needs both ends.")
	}
	if !IsValidFactStatus(r.Status) {
		return InvalidError("The relation status is not recognised.")
	}
	if !IsValidSourceScope(r.SourceScope) {
		return InvalidError("The relation source scope is not recognised.")
	}
	return nil
}

// FactType is which table a fact source or conflict refers to
// (DOMAIN_MODEL §6.6, §6.7).
//
// The value names the family and the companion identifier names the row, which
// is what lets one evidence table serve four aggregates without a nullable
// foreign key per aggregate. 'character_state' is in the set because a state is
// a fact like any other and must cite where it came from.
type FactType string

const (
	FactEntity         FactType = "entity"
	FactEvent          FactType = "event"
	FactRelation       FactType = "relation"
	FactCharacterState FactType = "character_state"
)

// FactTypes lists the documented fact families in the schema's order.
var FactTypes = []FactType{FactEntity, FactEvent, FactRelation, FactCharacterState}

// IsValidFactType reports whether a fact family may be referenced.
func IsValidFactType(value FactType) bool {
	for _, candidate := range FactTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// SourceKind is how a fact's evidence came to exist (DOMAIN_MODEL §6.6).
//
// 'text' means the fact was read out of the document, and §6.6's "原文片段展示通过
// offset 从规范化文本读取" is the path that reads it back. The other three explain
// a fact no chapter contains — a user edit, an inference and an adaptation
// choice — so the four stay distinguishable in an audit. The two offset columns
// are nullable in the schema and are not forced on a text source here, because
// an excerpt may be recorded before its span is computed.
type SourceKind string

const (
	SourceKindText           SourceKind = "text"
	SourceKindUser           SourceKind = "user"
	SourceKindAgentInference SourceKind = "agent_inference"
	SourceKindAdaptation     SourceKind = "adaptation"
)

// SourceKinds lists the documented evidence kinds in the schema's order.
var SourceKinds = []SourceKind{
	SourceKindText, SourceKindUser, SourceKindAgentInference, SourceKindAdaptation,
}

// IsValidSourceKind reports whether an evidence kind may be persisted.
func IsValidSourceKind(value SourceKind) bool {
	for _, candidate := range SourceKinds {
		if candidate == value {
			return true
		}
	}
	return false
}

// StoryFactSource is the evidence one fact cites (DOMAIN_MODEL §6.6).
//
// The row names the document version and the chapter rather than copying text:
// §6.6 says the passage is read back through the offsets and that only a
// limited excerpt may be stored for verification. Every
// source therefore names the version it quotes from — a range of offsets is
// meaningless without the text they index — and the quote hash lets the
// pipeline notice that the underlying text changed under a fact.
//
// §6.6 marks chapter_id, start_offset, end_offset and quote_hash nullable and
// leaves source_document_version_id unmarked, so this package requires the
// version while treating the others as optional. The schema spells all of them
// NOT NULL with an empty-string default and relies on that empty value where
// the specification says nullable; the domain check is the stricter of the two,
// which is the safe direction for a rule the SQL cannot express.
type StoryFactSource struct {
	ID       string
	FactType FactType
	FactID   string
	// ChapterID is empty when the evidence is not tied to a chapter, such as
	// evidence a user stated rather than one found in the text.
	ChapterID string
	// SourceDocumentVersionID is required: offsets index one version's
	// normalized text, so a source without it could not be read back.
	SourceDocumentVersionID string
	StartOffset             *int
	EndOffset               *int
	QuoteHash               string
	SourceKind              SourceKind
	CreatedAt               time.Time
}

// Validate checks an evidence row before it is stored.
func (s StoryFactSource) Validate() error {
	if !IsValidFactType(s.FactType) {
		return InvalidError("The fact type is not recognised.")
	}
	if strings.TrimSpace(s.FactID) == "" {
		return InvalidError("Evidence must name the fact it supports.")
	}
	if strings.TrimSpace(s.SourceDocumentVersionID) == "" {
		return InvalidError("Evidence must name the document version it quotes from.")
	}
	if err := validateOffsetPair(s.StartOffset, s.EndOffset); err != nil {
		return err
	}
	if s.QuoteHash != "" && !isLowerHexDigest(s.QuoteHash) {
		return InvalidError("The quote hash must be a SHA-256 digest or empty.")
	}
	if !IsValidSourceKind(s.SourceKind) {
		return InvalidError("The evidence kind is not recognised.")
	}
	return nil
}

// ConflictStatus is the state of a recorded fact conflict
// (DOMAIN_MODEL §6.7).
type ConflictStatus string

const (
	ConflictOpen     ConflictStatus = "open"
	ConflictResolved ConflictStatus = "resolved"
	ConflictWaived   ConflictStatus = "waived"
)

// ConflictStatuses lists the documented statuses in the schema's order.
var ConflictStatuses = []ConflictStatus{ConflictOpen, ConflictResolved, ConflictWaived}

// IsValidConflictStatus reports whether a conflict status may be persisted.
func IsValidConflictStatus(value ConflictStatus) bool {
	for _, candidate := range ConflictStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// StoryFactConflict is a disagreement between two facts
// (DOMAIN_MODEL §6.7).
//
// §6.7 makes the conflict a first-class row rather than a log line so it can be
// listed, resolved, waived and counted. The two facts are identified by family
// and identifier because a conflict can join facts of different kinds, and the
// pair is stored left/right because the schema's UNIQUE constraint is over the
// ordered pair. Keeping one row per disagreement therefore requires the writer
// to canonicalise the order; this package states the direction only by naming
// the fields.
type StoryFactConflict struct {
	ID            string
	ProjectID     string
	LeftFactType  FactType
	LeftFactID    string
	RightFactType FactType
	RightFactID   string
	// ConflictType is free text: §6.7 lists the field with no set, and a
	// detection pass may name a kind before the vocabulary settles.
	ConflictType string
	Status       ConflictStatus
	Resolution   string
	ResolvedBy   string
	CreatedAt    time.Time
	// ResolvedAt is zero while the conflict is open.
	ResolvedAt time.Time
}

// Validate checks a conflict before it is stored.
func (c StoryFactConflict) Validate() error {
	if strings.TrimSpace(c.ProjectID) == "" {
		return InvalidError("A conflict must belong to a project.")
	}
	if !IsValidFactType(c.LeftFactType) || !IsValidFactType(c.RightFactType) {
		return InvalidError("The conflict fact type is not recognised.")
	}
	if strings.TrimSpace(c.LeftFactID) == "" || strings.TrimSpace(c.RightFactID) == "" {
		return InvalidError("A conflict needs both facts.")
	}
	if !IsValidConflictStatus(c.Status) {
		return InvalidError("The conflict status is not recognised.")
	}
	return nil
}

// Resolve records what a conflict was decided to be.
//
// §6.7: an agent that finds a conflict creates the row, while a user or an
// explicit rule resolves it ("Agent 发现冲突后创建记录；用户或明确规则解决"). The
// transition is therefore one-way from open: a resolved conflict stays
// resolved and a waived one stays waived, because re-opening either would erase
// the decision that closed it, and a resolution needs text for the same reason
// — a closed conflict with no stated resolution cannot be audited.
//
// The method does not stamp ResolvedAt. The domain reads no clock (§2.2 keeps
// the clock injectable at the application layer), so the caller sets it from
// the same clock that stamps the rest of the transaction.
func (c *StoryFactConflict) Resolve(resolution, resolvedBy string) error {
	if !IsValidConflictStatus(c.Status) {
		return InvalidError("The conflict status is not recognised.")
	}
	switch c.Status {
	case ConflictResolved:
		return ConflictError("This conflict is already resolved.")
	case ConflictWaived:
		return ConflictError("This conflict was waived, so it cannot be resolved. Record a new conflict instead.")
	}
	trimmed := strings.TrimSpace(resolution)
	if trimmed == "" {
		return InvalidError("A resolution must state what was decided.")
	}
	c.Status = ConflictResolved
	c.Resolution = trimmed
	c.ResolvedBy = resolvedBy
	return nil
}

// CharacterState is a character's condition across a span of events
// (DOMAIN_MODEL §6.8).
//
// It exists for cross-event continuity, so the event-order columns are the
// queryable part: FromEventOrder is where the state starts and ToEventOrder,
// when set, is where it stops applying. §6.8 allows the state itself to be
// sparse JSON for the MVP ("核心状态应逐步正规化；MVP 可在 Schema 受控 JSON 中保存
// 稀疏状态"), so the four JSON fields stay opaque strings here — they are
// display and continuity notes, not facts another query joins on.
type CharacterState struct {
	ID                string
	CharacterEntityID string
	FromEventOrder    int
	// ToEventOrder is nil for a state that is still in force.
	ToEventOrder          *int
	AppearanceJSON        string
	CostumeAssetVersionID string
	InjuriesJSON          string
	PossessionsJSON       string
	RelationshipStateJSON string
	// SourceFactID cites the fact this state was derived from, empty when a
	// user entered it directly.
	SourceFactID string
	Status       FactStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Revision     int64
}

// Validate checks a character state before it is stored.
func (c CharacterState) Validate() error {
	if strings.TrimSpace(c.CharacterEntityID) == "" {
		return InvalidError("A character state must belong to a character entity.")
	}
	if c.FromEventOrder < 0 {
		return InvalidError("An event order cannot be negative.")
	}
	// A span that ends before it starts would make every continuity query
	// against it meaningless, so the two ends are ordered here as well as by
	// the caller that writes them.
	if c.ToEventOrder != nil && *c.ToEventOrder < c.FromEventOrder {
		return InvalidError("A character state cannot end before it starts.")
	}
	if !IsValidFactStatus(c.Status) {
		return InvalidError("The character state status is not recognised.")
	}
	return nil
}

// validateOffsetPair checks an optional evidence range.
//
// Both ends are pointers because absence carries meaning: §6.6 marks
// start_offset and end_offset nullable, so a fact a user stated has no span in
// the text at all, and nil/nil is how that is told apart from a recorded span
// that happens to sit at offset zero. When a range is present it must be a
// range — a non-negative start and an end at or after it — because the pair is
// read as a slice of the normalized text.
func validateOffsetPair(start, end *int) error {
	if start != nil && *start < 0 {
		return InvalidError("An evidence offset cannot be negative.")
	}
	if end != nil && *end < 0 {
		return InvalidError("An evidence offset cannot be negative.")
	}
	if start != nil && end != nil && *end < *start {
		return InvalidError("An evidence range cannot end before it starts.")
	}
	return nil
}

// isLowerHexDigest reports whether a value is a 64-character lowercase hex
// SHA-256 digest.
//
// §2.1 fixes SHA-256 for file content, and the story tables store the same
// shape for a document version's content hash, a chapter's hash and a quoted
// excerpt's hash. Uppercase hex is refused so one digest has exactly one
// spelling, which is what makes §5.2's duplicate-content lookup by hash
// ("相同内容哈希需提示重复") dependable rather than case-dependent.
func isLowerHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		default:
			return false
		}
	}
	return true
}
