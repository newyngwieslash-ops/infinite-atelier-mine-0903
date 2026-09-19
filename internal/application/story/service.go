package story

import (
	"context"
	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"strings"
	"time"

	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// NewService builds the story service.
func NewService(options Options) *Service {
	return &Service{repository: options.Repository, clock: options.Clock, ids: options.IDs, events: options.Events}
}

// Available reports whether the service can operate. An unattached binding
// fails closed rather than panicking.
func (s *Service) Available() bool {
	return s != nil && s.repository != nil && s.ids != nil
}

// recordEvent announces something that happened, if the service has a recorder.
//
// The nil check is not defensive padding: the recorder is an interface, so a
// Service composed without one holds a nil interface and calling a method on it
// panics. This is the one place that check lives, so no emit site has to repeat
// it and no emit site can forget it.
func (s *Service) recordEvent(ctx context.Context, draft eventsapp.Draft) {
	if s == nil || s.events == nil {
		return
	}
	s.events.RecordBestEffort(ctx, draft)
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// storageFailure is the fail-closed error for an unattached service and for a
// failed identifier generation.
func storageFailure() error {
	return storydomain.StorageError("The story store is unavailable.", nil)
}

// CreateSourceDocumentRequest is a caller's request to register a document.
type CreateSourceDocumentRequest struct {
	ProjectID    string
	DocumentType storydomain.DocumentType
	Name         string
}

// CreateSourceDocument registers an imported text.
//
// No version is created here: a version is one import with its own file
// references and hash, and a document registered before its first import has
// none. CurrentVersionID stays empty until AddSourceDocumentVersion commits
// one, which is what DOMAIN_MODEL §5.1 says the field means.
func (s *Service) CreateSourceDocument(ctx context.Context, request CreateSourceDocumentRequest) (storydomain.SourceDocument, error) {
	if !s.Available() {
		return storydomain.SourceDocument{}, storageFailure()
	}
	id, err := s.ids.New()
	if err != nil {
		return storydomain.SourceDocument{}, storageFailure()
	}
	now := s.now()
	record := storydomain.SourceDocument{
		ID:        id,
		ProjectID: strings.TrimSpace(request.ProjectID),
		Type:      request.DocumentType,
		Name:      strings.TrimSpace(request.Name),
		Status:    storydomain.DocumentActive,
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
	}
	if err := record.Validate(); err != nil {
		return storydomain.SourceDocument{}, err
	}
	if err := s.repository.CreateSourceDocument(ctx, record); err != nil {
		return storydomain.SourceDocument{}, err
	}
	// Section 17's SourceDocumentImported. Best effort: the row is committed, so the caller must not be told the command failed because the announcement did not land.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          event.SourceDocumentImported,
		AggregateType: event.AggregateSourceDocument,
		AggregateID:   record.ID,
		ProjectID:     record.ProjectID,
	})
	return record, nil
}

// AddSourceDocumentVersionRequest carries one import of a document.
type AddSourceDocumentVersionRequest struct {
	SourceDocumentID string
	// SourceHash is the digest of the original upload. It is separate from
	// ContentHash so a re-export of the same file can still be recognised as a
	// duplicate of what was imported before.
	SourceHash           string
	PhysicalFileID       string
	NormalizedTextFileID string
	ContentHash          string
	MIMEType             string
	Encoding             string
	CharCount            int
	ImportMetadataJSON   string
	CreatedByType        storydomain.CreatedByType
}

// AddSourceDocumentVersion appends a version, numbering it after the highest
// existing one.
//
// The number is derived from the stored maximum rather than a count, because a
// version the schema still holds must not have its number reused: the unique
// constraint on (source_document_id, version_number) would reject the insert,
// and a count could not see the rows it was counting past.
//
// The document's current_version_id is advanced in the same call so the field
// names the newest import (§5.1). The two writes are separate statements, and
// the pointer write is guarded by the document revision read at the start of
// this call: if another import advanced the document in between, the guard
// fails and the caller gets the stored version back alongside the conflict
// error, rather than this call overwriting a newer pointer.
func (s *Service) AddSourceDocumentVersion(ctx context.Context, request AddSourceDocumentVersionRequest) (storydomain.SourceDocumentVersion, error) {
	if !s.Available() {
		return storydomain.SourceDocumentVersion{}, storageFailure()
	}
	document, err := s.repository.GetSourceDocument(ctx, request.SourceDocumentID)
	if err != nil {
		return storydomain.SourceDocumentVersion{}, err
	}
	createdBy := request.CreatedByType
	if createdBy == "" {
		createdBy = storydomain.CreatedByUser
	}
	id, err := s.ids.New()
	if err != nil {
		return storydomain.SourceDocumentVersion{}, storageFailure()
	}
	highest, err := s.repository.MaxSourceDocumentVersionNumber(ctx, request.SourceDocumentID)
	if err != nil {
		return storydomain.SourceDocumentVersion{}, err
	}
	now := s.now()
	version := storydomain.SourceDocumentVersion{
		ID:                   id,
		SourceDocumentID:     document.ID,
		VersionNumber:        highest + 1,
		SourceHash:           strings.TrimSpace(request.SourceHash),
		PhysicalFileID:       strings.TrimSpace(request.PhysicalFileID),
		NormalizedTextFileID: strings.TrimSpace(request.NormalizedTextFileID),
		ContentHash:          strings.TrimSpace(request.ContentHash),
		MIMEType:             request.MIMEType,
		Encoding:             request.Encoding,
		CharCount:            request.CharCount,
		ImportMetadataJSON:   request.ImportMetadataJSON,
		CreatedByType:        createdBy,
		CreatedAt:            now,
	}
	if err := version.Validate(); err != nil {
		return storydomain.SourceDocumentVersion{}, err
	}
	if err := s.repository.CreateSourceDocumentVersion(ctx, version); err != nil {
		return storydomain.SourceDocumentVersion{}, err
	}
	document.CurrentVersionID = version.ID
	document.UpdatedAt = now
	// The guard names the revision just read rather than a caller's: the
	// pointer is derived from the version that was just committed, and a
	// document that changed underneath loses the compare-and-swap instead of
	// overwriting the other write.
	if err := s.repository.UpdateSourceDocument(ctx, document, document.Revision); err != nil {
		return version, err
	}
	return version, nil
}

// GetSourceDocument returns one document.
func (s *Service) GetSourceDocument(ctx context.Context, id string) (storydomain.SourceDocument, error) {
	if !s.Available() {
		return storydomain.SourceDocument{}, storageFailure()
	}
	return s.repository.GetSourceDocument(ctx, id)
}

// ListSourceDocuments returns a project's documents oldest first.
func (s *Service) ListSourceDocuments(ctx context.Context, projectID string) ([]storydomain.SourceDocument, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListSourceDocuments(ctx, projectID)
}

// CreateChapterRequest adds one chapter to a document version.
type CreateChapterRequest struct {
	SourceDocumentVersionID string
	Ordinal                 int
	Title                   string
	StartOffset             int
	EndOffset               int
	ContentHash             string
	// SourceKind records how the boundary was decided. The zero value is the
	// detector's 'regex'; a caller creating a boundary by hand passes 'manual'.
	SourceKind storydomain.ChapterSourceKind
	// Status records how the boundary came to be trusted. The zero value is the
	// import pipeline's 'detected'; a caller correcting a boundary passes
	// 'edited' or 'confirmed' from the domain vocabulary.
	Status storydomain.ChapterStatus
}

// CreateChapter stores a chapter of a document version.
func (s *Service) CreateChapter(ctx context.Context, request CreateChapterRequest) (storydomain.Chapter, error) {
	if !s.Available() {
		return storydomain.Chapter{}, storageFailure()
	}
	status := request.Status
	if status == "" {
		status = storydomain.ChapterDetected
	}
	// The detector's output is a pattern match, so that is the default. A caller
	// creating a boundary by hand passes 'manual', and one wrapping a whole
	// document passes 'whole'.
	sourceKind := request.SourceKind
	if sourceKind == "" {
		sourceKind = storydomain.ChapterFromPattern
	}
	id, err := s.ids.New()
	if err != nil {
		return storydomain.Chapter{}, storageFailure()
	}
	now := s.now()
	record := storydomain.Chapter{
		ID:                      id,
		SourceDocumentVersionID: strings.TrimSpace(request.SourceDocumentVersionID),
		Ordinal:                 request.Ordinal,
		Title:                   request.Title,
		StartOffset:             request.StartOffset,
		EndOffset:               request.EndOffset,
		ContentHash:             strings.TrimSpace(request.ContentHash),
		SourceKind:              sourceKind,
		Status:                  status,
		CreatedAt:               now,
		UpdatedAt:               now,
		Revision:                1,
	}
	if err := record.Validate(); err != nil {
		return storydomain.Chapter{}, err
	}
	if err := s.repository.CreateChapter(ctx, record); err != nil {
		return storydomain.Chapter{}, err
	}
	return record, nil
}

// ReviseChapterRequest carries a chapter correction.
type ReviseChapterRequest struct {
	ChapterID   string
	Title       string
	StartOffset int
	EndOffset   int
	ContentHash string
	Status      storydomain.ChapterStatus
	Revision    int64
}

// ReviseChapter stores a corrected boundary under a revision guard.
//
// The ordinal is not part of the request. It is the chapter's position within
// its version and the schema makes (version, ordinal) unique, so moving a
// chapter to another position is a re-split of the version rather than an edit
// of one row. A revision is what §5.3 asks for when a user changes a boundary.
func (s *Service) ReviseChapter(ctx context.Context, request ReviseChapterRequest) (storydomain.Chapter, error) {
	if !s.Available() {
		return storydomain.Chapter{}, storageFailure()
	}
	record, err := s.repository.GetChapter(ctx, request.ChapterID)
	if err != nil {
		return storydomain.Chapter{}, err
	}
	record.Title = request.Title
	record.StartOffset = request.StartOffset
	record.EndOffset = request.EndOffset
	record.ContentHash = strings.TrimSpace(request.ContentHash)
	if request.Status != "" {
		record.Status = request.Status
	}
	// The boundary is now a person's rather than the detector's, which is what
	// chapters.source_kind exists to record: migration 000014 states that a
	// user-edited boundary is marked 'manual' by the command that edits it, and a
	// boundary that was revised and still reads 'regex' would make the import
	// report and the staleness walk both wrong about where it came from.
	//
	// It is set before the domain check so a caller cannot pass a source kind that
	// contradicts the command that ran.
	record.SourceKind = storydomain.ChapterManual
	record.UpdatedAt = s.now()
	if err := record.Validate(); err != nil {
		return storydomain.Chapter{}, err
	}
	if err := s.repository.UpdateChapter(ctx, record, request.Revision); err != nil {
		return storydomain.Chapter{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// GetChapter returns one chapter by id.
//
// The read exists for the extraction reader, which reaches a chapter from the
// graph side and needs its offsets to slice the text it indexes.
func (s *Service) GetChapter(ctx context.Context, id string) (storydomain.Chapter, error) {
	if !s.Available() {
		return storydomain.Chapter{}, storageFailure()
	}
	if strings.TrimSpace(id) == "" {
		return storydomain.Chapter{}, storydomain.InvalidError("A chapter is required.")
	}
	return s.repository.GetChapter(ctx, strings.TrimSpace(id))
}

// ListChapters returns a document version's chapters in reading order.
func (s *Service) ListChapters(ctx context.Context, sourceDocumentVersionID string) ([]storydomain.Chapter, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListChapters(ctx, sourceDocumentVersionID)
}

// CreateStoryEntityRequest adds a story entity.
type CreateStoryEntityRequest struct {
	ProjectID     string
	Type          storydomain.EntityType
	CanonicalName string
	// SourceScope is where the entity came from relative to the adaptation.
	// The zero value is the schema's 'original'.
	SourceScope storydomain.SourceScope
}

// CreateStoryEntity stores an entity as a candidate.
//
// The row is written as 'candidate' rather than 'accepted' because PRD FR-030
// makes confirmation a separate decision ("候选事实在进入'已确认事实'前需通过用户或
// 规则校验"), and this command is the extraction path: it proposes a fact and
// does not confirm it.
func (s *Service) CreateStoryEntity(ctx context.Context, request CreateStoryEntityRequest) (storydomain.StoryEntity, error) {
	if !s.Available() {
		return storydomain.StoryEntity{}, storageFailure()
	}
	scope := request.SourceScope
	if scope == "" {
		scope = storydomain.ScopeOriginal
	}
	id, err := s.ids.New()
	if err != nil {
		return storydomain.StoryEntity{}, storageFailure()
	}
	now := s.now()
	record := storydomain.StoryEntity{
		ID:            id,
		ProjectID:     strings.TrimSpace(request.ProjectID),
		Type:          request.Type,
		CanonicalName: strings.TrimSpace(request.CanonicalName),
		Status:        storydomain.FactCandidate,
		SourceScope:   scope,
		CreatedAt:     now,
		UpdatedAt:     now,
		Revision:      1,
	}
	if err := record.Validate(); err != nil {
		return storydomain.StoryEntity{}, err
	}
	if err := s.repository.CreateStoryEntity(ctx, record); err != nil {
		return storydomain.StoryEntity{}, err
	}
	// Section 17's StoryFactAccepted. Best effort: the row is committed, so the caller must not be told the command failed because the announcement did not land.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          event.StoryFactAccepted,
		AggregateType: event.AggregateStoryEntity,
		AggregateID:   record.ID,
		ProjectID:     record.ProjectID,
	})
	return record, nil
}

// FactDecisionRequest carries one confirmation decision.
type FactDecisionRequest struct {
	ID       string
	Revision int64
}

// AcceptStoryEntity confirms a candidate entity.
//
// The gate is the decision PRD FR-030 requires. Accepting a rejected entity
// would overwrite a decision a user or a rule already made, and re-accepting a
// confirmed one would write a new revision for no state change; both are
// refused as conflicts, so the caller learns the current state instead of
// silently replacing it. A locked entity is refused too: the lock is the state
// that keeps a later pass from rewriting it, and no command in this layer
// releases one.
func (s *Service) AcceptStoryEntity(ctx context.Context, request FactDecisionRequest) (storydomain.StoryEntity, error) {
	if !s.Available() {
		return storydomain.StoryEntity{}, storageFailure()
	}
	record, err := s.repository.GetStoryEntity(ctx, request.ID)
	if err != nil {
		return storydomain.StoryEntity{}, err
	}
	if err := acceptGate(record.Status); err != nil {
		return storydomain.StoryEntity{}, err
	}
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

// RejectStoryEntity refuses a candidate entity.
func (s *Service) RejectStoryEntity(ctx context.Context, request FactDecisionRequest) (storydomain.StoryEntity, error) {
	if !s.Available() {
		return storydomain.StoryEntity{}, storageFailure()
	}
	record, err := s.repository.GetStoryEntity(ctx, request.ID)
	if err != nil {
		return storydomain.StoryEntity{}, err
	}
	if err := rejectGate(record.Status); err != nil {
		return storydomain.StoryEntity{}, err
	}
	record.Status = storydomain.FactRejected
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

// acceptGate refuses a confirmation a rejected or already-confirmed fact
// cannot receive. A rejected fact keeps its decision until a new candidate is
// recorded, because a rejection is evidence of what was considered and
// discarded.
func acceptGate(current storydomain.FactStatus) error {
	switch current {
	case storydomain.FactRejected:
		return storydomain.ConflictError("This fact was rejected, so it cannot be confirmed. Record a new candidate instead.")
	case storydomain.FactAccepted:
		return storydomain.ConflictError("This fact is already confirmed.")
	case storydomain.FactLocked:
		return storydomain.ConflictError("This fact is locked, so its state cannot be changed here.")
	}
	return nil
}

// rejectGate refuses a rejection a confirmed or already-rejected fact cannot
// receive. A confirmed fact would otherwise disappear from the graph through a
// single command that left no trace of the decision it reversed.
func rejectGate(current storydomain.FactStatus) error {
	switch current {
	case storydomain.FactAccepted:
		return storydomain.ConflictError("This fact is confirmed, so it cannot be rejected. Record a new candidate instead.")
	case storydomain.FactRejected:
		return storydomain.ConflictError("This fact is already rejected.")
	case storydomain.FactLocked:
		return storydomain.ConflictError("This fact is locked, so its state cannot be changed here.")
	}
	return nil
}

// CreateStoryEventRequest adds a story event.
type CreateStoryEventRequest struct {
	ProjectID           string
	ChapterID           string
	Ordinal             int
	Name                string
	Description         string
	EventType           string
	StoryTimeText       string
	StoryTimeOrder      *int
	LocationEntityID    string
	CauseSummary        string
	ResultSummary       string
	Importance          string
	Confidence          float64
	SourceScope         storydomain.SourceScope
	CreatedByAgentRunID string
}

// CreateStoryEvent stores an event as a candidate, for the same reason
// CreateStoryEntity does: extraction proposes, the gate confirms.
func (s *Service) CreateStoryEvent(ctx context.Context, request CreateStoryEventRequest) (storydomain.StoryEvent, error) {
	if !s.Available() {
		return storydomain.StoryEvent{}, storageFailure()
	}
	scope := request.SourceScope
	if scope == "" {
		scope = storydomain.ScopeOriginal
	}
	id, err := s.ids.New()
	if err != nil {
		return storydomain.StoryEvent{}, storageFailure()
	}
	now := s.now()
	record := storydomain.StoryEvent{
		ID:                  id,
		ProjectID:           strings.TrimSpace(request.ProjectID),
		ChapterID:           strings.TrimSpace(request.ChapterID),
		Ordinal:             request.Ordinal,
		Name:                strings.TrimSpace(request.Name),
		Description:         request.Description,
		EventType:           request.EventType,
		StoryTimeText:       request.StoryTimeText,
		StoryTimeOrder:      request.StoryTimeOrder,
		LocationEntityID:    strings.TrimSpace(request.LocationEntityID),
		CauseSummary:        request.CauseSummary,
		ResultSummary:       request.ResultSummary,
		Importance:          request.Importance,
		Confidence:          request.Confidence,
		Status:              storydomain.FactCandidate,
		SourceScope:         scope,
		CreatedByAgentRunID: request.CreatedByAgentRunID,
		CreatedAt:           now,
		UpdatedAt:           now,
		Revision:            1,
	}
	if err := record.Validate(); err != nil {
		return storydomain.StoryEvent{}, err
	}
	if err := s.repository.CreateStoryEvent(ctx, record); err != nil {
		return storydomain.StoryEvent{}, err
	}
	// Section 17's StoryFactAccepted. Best effort: the row is committed, so the caller must not be told the command failed because the announcement did not land.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          event.StoryFactAccepted,
		AggregateType: event.AggregateStoryEvent,
		AggregateID:   record.ID,
		ProjectID:     record.ProjectID,
	})
	return record, nil
}

// AcceptStoryEvent confirms a candidate event through the same gate as
// AcceptStoryEntity.
func (s *Service) AcceptStoryEvent(ctx context.Context, request FactDecisionRequest) (storydomain.StoryEvent, error) {
	if !s.Available() {
		return storydomain.StoryEvent{}, storageFailure()
	}
	record, err := s.repository.GetStoryEvent(ctx, request.ID)
	if err != nil {
		return storydomain.StoryEvent{}, err
	}
	if err := acceptGate(record.Status); err != nil {
		return storydomain.StoryEvent{}, err
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

// RejectStoryEvent refuses a candidate event through the same gate as
// RejectStoryEntity.
func (s *Service) RejectStoryEvent(ctx context.Context, request FactDecisionRequest) (storydomain.StoryEvent, error) {
	if !s.Available() {
		return storydomain.StoryEvent{}, storageFailure()
	}
	record, err := s.repository.GetStoryEvent(ctx, request.ID)
	if err != nil {
		return storydomain.StoryEvent{}, err
	}
	if err := rejectGate(record.Status); err != nil {
		return storydomain.StoryEvent{}, err
	}
	record.Status = storydomain.FactRejected
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

// CreateStoryRelationRequest adds one edge of the story graph.
type CreateStoryRelationRequest struct {
	ProjectID        string
	Type             storydomain.RelationType
	SourceEntityType string
	SourceEntityID   string
	TargetEntityType string
	TargetEntityID   string
	ValidFromEventID string
	ValidToEventID   string
	Confidence       float64
	SourceScope      storydomain.SourceScope
}

// CreateStoryRelation stores a relation as a candidate.
//
// The relation type is checked against the domain vocabulary before the write
// (ADR-0007 pins the same list in the schema's CHECK), so an extraction cannot
// invent a meaning the graph has no rule for. The two entity-type fields stay
// free text: §6.5 lets a relation join anything the canvas registry knows
// about, which is a set this layer does not own.
func (s *Service) CreateStoryRelation(ctx context.Context, request CreateStoryRelationRequest) (storydomain.StoryRelation, error) {
	if !s.Available() {
		return storydomain.StoryRelation{}, storageFailure()
	}
	if !storydomain.IsValidRelationType(request.Type) {
		return storydomain.StoryRelation{}, storydomain.InvalidError("That relation type is not recognised.")
	}
	scope := request.SourceScope
	if scope == "" {
		scope = storydomain.ScopeOriginal
	}
	id, err := s.ids.New()
	if err != nil {
		return storydomain.StoryRelation{}, storageFailure()
	}
	now := s.now()
	record := storydomain.StoryRelation{
		ID:               id,
		ProjectID:        strings.TrimSpace(request.ProjectID),
		Type:             request.Type,
		SourceEntityType: strings.TrimSpace(request.SourceEntityType),
		SourceEntityID:   strings.TrimSpace(request.SourceEntityID),
		TargetEntityType: strings.TrimSpace(request.TargetEntityType),
		TargetEntityID:   strings.TrimSpace(request.TargetEntityID),
		ValidFromEventID: strings.TrimSpace(request.ValidFromEventID),
		ValidToEventID:   strings.TrimSpace(request.ValidToEventID),
		Confidence:       request.Confidence,
		Status:           storydomain.FactCandidate,
		SourceScope:      scope,
		CreatedAt:        now,
		UpdatedAt:        now,
		Revision:         1,
	}
	if err := record.Validate(); err != nil {
		return storydomain.StoryRelation{}, err
	}
	if err := s.repository.CreateStoryRelation(ctx, record); err != nil {
		return storydomain.StoryRelation{}, err
	}
	return record, nil
}

// OpenConflictRequest records a disagreement between two facts.
type OpenConflictRequest struct {
	ProjectID     string
	LeftFactType  storydomain.FactType
	LeftFactID    string
	RightFactType storydomain.FactType
	RightFactID   string
	ConflictType  string
}

// OpenConflict stores an open disagreement.
//
// The row is created by the pass that noticed the disagreement (§6.7: "Agent
// 发现冲突后创建记录"), so it is written as 'open' and only a resolution closes
// it. The fact pair is stored in the order the caller gave; the schema's
// unique constraint is over the ordered pair, so reporting the same two facts
// in the other order creates a second row.
func (s *Service) OpenConflict(ctx context.Context, request OpenConflictRequest) (storydomain.StoryFactConflict, error) {
	if !s.Available() {
		return storydomain.StoryFactConflict{}, storageFailure()
	}
	id, err := s.ids.New()
	if err != nil {
		return storydomain.StoryFactConflict{}, storageFailure()
	}
	record := storydomain.StoryFactConflict{
		ID:            id,
		ProjectID:     strings.TrimSpace(request.ProjectID),
		LeftFactType:  request.LeftFactType,
		LeftFactID:    strings.TrimSpace(request.LeftFactID),
		RightFactType: request.RightFactType,
		RightFactID:   strings.TrimSpace(request.RightFactID),
		ConflictType:  request.ConflictType,
		Status:        storydomain.ConflictOpen,
		CreatedAt:     s.now(),
	}
	if err := record.Validate(); err != nil {
		return storydomain.StoryFactConflict{}, err
	}
	if err := s.repository.CreateStoryConflict(ctx, record); err != nil {
		return storydomain.StoryFactConflict{}, err
	}
	// Section 17's StoryFactConflictOpened. Best effort: the row is committed, so the caller must not be told the command failed because the announcement did not land.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          event.StoryFactConflictOpened,
		AggregateType: event.AggregateStoryConflict,
		AggregateID:   record.ID,
		ProjectID:     record.ProjectID,
	})
	return record, nil
}

// ResolveConflictRequest closes a conflict.
type ResolveConflictRequest struct {
	ConflictID string
	Resolution string
	ResolvedBy string
}

// ResolveConflict records what was decided.
//
// The transition itself is the domain's: StoryFactConflict.Resolve decides
// that an already-resolved or waived conflict cannot be closed again and that
// a resolution must state something, and its error is returned unchanged. This
// method contributes the clock — the domain reads none (§2.2) — and the write,
// which is guarded by the status that was read, because the conflict table has
// no revision column for a compare-and-swap to name.
func (s *Service) ResolveConflict(ctx context.Context, request ResolveConflictRequest) (storydomain.StoryFactConflict, error) {
	if !s.Available() {
		return storydomain.StoryFactConflict{}, storageFailure()
	}
	record, err := s.repository.GetStoryConflict(ctx, request.ConflictID)
	if err != nil {
		return storydomain.StoryFactConflict{}, err
	}
	expectedStatus := record.Status
	if err := record.Resolve(request.Resolution, request.ResolvedBy); err != nil {
		return storydomain.StoryFactConflict{}, err
	}
	record.ResolvedAt = s.now()
	if err := s.repository.UpdateStoryConflict(ctx, record, expectedStatus); err != nil {
		return storydomain.StoryFactConflict{}, err
	}
	return record, nil
}

// GetSourceDocumentVersion returns one version by id.
func (s *Service) GetSourceDocumentVersion(ctx context.Context, id string) (storydomain.SourceDocumentVersion, error) {
	if !s.Available() {
		return storydomain.SourceDocumentVersion{}, storageFailure()
	}
	return s.repository.GetSourceDocumentVersion(ctx, strings.TrimSpace(id))
}

// SourceHashMatch names the document a duplicate import belongs to.
type SourceHashMatch struct {
	Version      storydomain.SourceDocumentVersion
	DocumentID   string
	DocumentName string
}

// FindVersionBySourceHash reports whether a project already holds a document
// whose original upload has this hash.
//
// PRD FR-020 requires a duplicate-import warning, and this is the lookup that
// drives it. The check is scoped to the project because the same file imported
// into two dramas is two documents rather than a duplicate: warning a user about
// a match in someone else's story would be worse than saying nothing.
//
// An empty hash returns no match rather than every row with an empty hash. A
// pasted document has no original file, so "hash is empty" means "cannot be
// compared", not "matches all the other pasted documents".
func (s *Service) FindVersionBySourceHash(ctx context.Context, projectID, sourceHash string) (SourceHashMatch, bool, error) {
	if !s.Available() {
		return SourceHashMatch{}, false, storageFailure()
	}
	trimmedHash := strings.TrimSpace(sourceHash)
	if trimmedHash == "" {
		return SourceHashMatch{}, false, nil
	}
	version, documentName, found, err := s.repository.FindVersionBySourceHash(ctx, strings.TrimSpace(projectID), trimmedHash)
	if err != nil {
		return SourceHashMatch{}, false, err
	}
	if !found {
		return SourceHashMatch{}, false, nil
	}
	return SourceHashMatch{
		Version:      version,
		DocumentID:   version.SourceDocumentID,
		DocumentName: documentName,
	}, true, nil
}

// ConfirmChaptersRequest confirms every boundary of a version.
type ConfirmChaptersRequest struct {
	SourceDocumentVersionID string
	// Event is the governance record of the decision, built by the caller
	// because only it knows the project and the trace. It is written in the same
	// transaction as the status changes, so a confirmation nobody can audit does
	// not happen.
	Event event.Event
}

// ConfirmChapters moves a version's detected boundaries to confirmed.
//
// DOMAIN_MODEL section 5.3 makes a user confirmation what takes a boundary out
// of 'detected', and §16 names this command. The two writes — the statuses and
// the event — go through one repository call because they are one act.
//
// An 'edited' boundary is left alone: it already carries a stronger statement
// than 'confirmed', and rewriting it would discard the fact that a person
// changed it. The returned list reflects that, so a caller can see which
// boundaries it actually confirmed.
func (s *Service) ConfirmChapters(ctx context.Context, request ConfirmChaptersRequest) ([]storydomain.Chapter, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	versionID := strings.TrimSpace(request.SourceDocumentVersionID)
	if versionID == "" {
		return nil, storydomain.InvalidError("A confirmation must name a document version.")
	}
	chapters, err := s.repository.ListChapters(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, storydomain.InvalidError("There are no chapter boundaries to confirm.")
	}
	if err := s.repository.ConfirmChapters(ctx, versionID, request.Event); err != nil {
		return nil, err
	}
	// Re-read so the caller sees what the store holds rather than what this
	// method hoped it would hold.
	confirmed, err := s.repository.ListChapters(ctx, versionID)
	if err != nil {
		return nil, err
	}
	return confirmed, nil
}
