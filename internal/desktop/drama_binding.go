package desktop

import (
	"context"
	"errors"
	"sync"
	"time"

	appevents "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	appproductionpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstaleness "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/staleness"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	extractiondomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/extraction"
	importdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// DramaBinding is the narrow Wails surface for the drama production domain:
// source documents and chapters, the story fact graph, episodes and scripts,
// storyboards and panels, durable workflow runs, and staleness marks.
//
// It exposes the commands and queries a production UI needs and nothing else.
// There is deliberately no method that returns a file path, a file's bytes, a
// provider secret or arbitrary SQL: media travels through the job result reader
// by content hash, and a document version references its stored objects by hash
// rather than by location.
//
// Every service is optional. An unattached service leaves its methods failing
// closed with a stable DESKTOP_BINDING_UNAVAILABLE error rather than panicking,
// because the composition root attaches the drama services only once a writable
// database exists.
type DramaBinding struct {
	mu         sync.RWMutex
	ctx        context.Context
	story      *appstory.Service
	script     *appscript.Service
	storyboard *appstoryboard.Service
	workflow   *appworkflow.Service
	staleness  *appstaleness.Service
	events     *appevents.Service
	// batch drives the image batch and its gate. It is a separate slot from both pipelines
	// because it needs services neither does — a job store and the assets service — so a build
	// can run the five agent stages without it and say so when the batch is asked for.
	batch ProductionBatch
	// productionPipeline drives the five production stages. It is a SECOND field rather than
	// the same one, because the two pipelines answer for disjoint stage sets and a stage
	// names the layer it belongs to: a single slot would let the last one attached win, and
	// a script stage would then be run by the production layer, which refuses it.
	//
	// The commands route by stage through `pipelineFor`, so a caller does not choose a layer
	// — the stage it named does.
	productionPipeline StagePipeline
	// pipeline drives the three script stages. It is attached by the agent stack, which is where the
	// runtime and the engine are composed, and a nil one leaves the stage commands failing closed.
	pipeline StagePipeline
}

// AttachStory supplies the story service. A nil service leaves the story
// methods failing closed.
func AttachStory(binding *DramaBinding, ctx context.Context, service *appstory.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.story = service
	binding.mu.Unlock()
}

// AttachScript supplies the episode and script service.
func AttachScript(binding *DramaBinding, ctx context.Context, service *appscript.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.script = service
	binding.mu.Unlock()
}

// AttachStoryboard supplies the director-plan, storyboard and panel service.
func AttachStoryboard(binding *DramaBinding, ctx context.Context, service *appstoryboard.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.storyboard = service
	binding.mu.Unlock()
}

// AttachWorkflow supplies the durable workflow service.
func AttachWorkflow(binding *DramaBinding, ctx context.Context, service *appworkflow.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.workflow = service
	binding.mu.Unlock()
}

// AttachStaleness supplies the staleness service.
func AttachStaleness(binding *DramaBinding, ctx context.Context, service *appstaleness.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.staleness = service
	binding.mu.Unlock()
}

func (b *DramaBinding) context() context.Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *DramaBinding) storyService() *appstory.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.story
}

func (b *DramaBinding) scriptService() *appscript.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.script
}

func (b *DramaBinding) storyboardService() *appstoryboard.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.storyboard
}

func (b *DramaBinding) eventService() *appevents.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.events
}

func (b *DramaBinding) workflowService() *appworkflow.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.workflow
}

// AttachDomainEvents supplies the domain event stream.
func AttachDomainEvents(binding *DramaBinding, ctx context.Context, service *appevents.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.events = service
	binding.mu.Unlock()
}

func (b *DramaBinding) stalenessService() *appstaleness.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.staleness
}

// DramaBindingUnavailable is the fail-closed error the composition root returns
// when the drama services could not be composed.
func DramaBindingUnavailable() error {
	return bindingUnavailable()
}

// rfc3339OrEmpty renders a timestamp for the transport, leaving a zero time as
// an empty string.
//
// Several drama rows carry an optional instant — a stage attempt that has not
// started, a document that is not deleted — and the frontend distinguishes
// "absent" from "the epoch". The zero time is the domain's absence marker, so it
// travels as an empty string rather than as year one.
func rfc3339OrEmpty(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(rfc3339)
}

// Bounds for the drama commands. They exist so one call cannot send an
// unbounded list (SECURITY: a command must stay bounded).
//
// These are the only two collection arguments on this surface. The drama list
// queries take a parent identifier and no client-supplied page size: a chapter
// list belongs to one document version, a shot list to one scene, and so on, so
// a caller cannot widen a query by asking for more rows. Their row counts are
// bounded by what the aggregate holds rather than by a page size, which is why
// no clampLimit appears here.
const (
	// maxBatchPanelCandidates bounds the candidate list one panel approval may
	// name. storyboard.MaxBatchCandidates is the service's own bound; this call
	// refuses an oversize list before the service has to.
	maxBatchPanelCandidates = appstoryboard.MaxBatchCandidates
	// maxBatchReviewIssues bounds the findings one review report may carry. It is
	// the workflow service's own bound, applied here as well so an oversize
	// request never reaches the service.
	maxBatchReviewIssues = appworkflow.MaxBatchReviewIssues
	// maxEventTypeLength bounds an event-type filter. The names are short
	// camel-case words, so this only rejects something absurd before the
	// service's vocabulary check runs.
	maxEventTypeLength = 120
)

// ---------------------------------------------------------------------------
// Story: source documents, chapters and the fact graph
// ---------------------------------------------------------------------------

// SourceDocumentDTO is the transport view of an imported text.
type SourceDocumentDTO struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	// CurrentVersionID is empty until the first version is committed.
	CurrentVersionID string `json:"currentVersionId,omitempty"`
	Status           string `json:"status"`
	DeletedAt        string `json:"deletedAt,omitempty"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
	Revision         int64  `json:"revision"`
}

// SourceDocumentVersionDTO is the transport view of one import of a document.
//
// Both file references are content hashes, never paths: a chapter indexes the
// normalized text by offset, and the store owns the layout.
type SourceDocumentVersionDTO struct {
	ID                   string `json:"id"`
	SourceDocumentID     string `json:"sourceDocumentId"`
	VersionNumber        int    `json:"versionNumber"`
	PhysicalFileID       string `json:"physicalFileId,omitempty"`
	NormalizedTextFileID string `json:"normalizedTextFileId"`
	ContentHash          string `json:"contentHash,omitempty"`
	MIMEType             string `json:"mimeType,omitempty"`
	Encoding             string `json:"encoding,omitempty"`
	CharCount            int    `json:"charCount"`
	ImportMetadataJSON   string `json:"importMetadataJson,omitempty"`
	CreatedByType        string `json:"createdByType"`
	CreatedAt            string `json:"createdAt"`
}

// ChapterDTO is the transport view of one chapter of a document version.
type ChapterDTO struct {
	ID                      string `json:"id"`
	SourceDocumentVersionID string `json:"sourceDocumentVersionId"`
	Ordinal                 int    `json:"ordinal"`
	Title                   string `json:"title"`
	StartOffset             int    `json:"startOffset"`
	EndOffset               int    `json:"endOffset"`
	ContentHash             string `json:"contentHash,omitempty"`
	// Status is "detected", "confirmed" or "edited".
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Revision  int64  `json:"revision"`
}

// StoryEntityDTO is the transport view of a story entity.
type StoryEntityDTO struct {
	ID                      string `json:"id"`
	ProjectID               string `json:"projectId"`
	Type                    string `json:"type"`
	CanonicalName           string `json:"canonicalName"`
	Status                  string `json:"status"`
	SourceScope             string `json:"sourceScope"`
	CurrentProfileVersionID string `json:"currentProfileVersionId,omitempty"`
	DeletedAt               string `json:"deletedAt,omitempty"`
	CreatedAt               string `json:"createdAt"`
	UpdatedAt               string `json:"updatedAt"`
	Revision                int64  `json:"revision"`
}

// StoryEventDTO is the transport view of a story event.
type StoryEventDTO struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	ChapterID string `json:"chapterId,omitempty"`
	Ordinal   int    `json:"ordinal"`
	Name      string `json:"name"`
	// Description is the event text the UI shows.
	Description      string  `json:"description,omitempty"`
	EventType        string  `json:"eventType,omitempty"`
	StoryTimeText    string  `json:"storyTimeText,omitempty"`
	StoryTimeOrder   *int    `json:"storyTimeOrder,omitempty"`
	LocationEntityID string  `json:"locationEntityId,omitempty"`
	CauseSummary     string  `json:"causeSummary,omitempty"`
	ResultSummary    string  `json:"resultSummary,omitempty"`
	Importance       string  `json:"importance,omitempty"`
	Confidence       float64 `json:"confidence"`
	Status           string  `json:"status"`
	SourceScope      string  `json:"sourceScope"`
	// CreatedByAgentRunID is empty when a user wrote the event.
	CreatedByAgentRunID string `json:"createdByAgentRunId,omitempty"`
	DeletedAt           string `json:"deletedAt,omitempty"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
	Revision            int64  `json:"revision"`
}

// StoryRelationDTO is the transport view of one edge of the story graph.
type StoryRelationDTO struct {
	ID               string  `json:"id"`
	ProjectID        string  `json:"projectId"`
	Type             string  `json:"type"`
	SourceEntityType string  `json:"sourceEntityType"`
	SourceEntityID   string  `json:"sourceEntityId"`
	TargetEntityType string  `json:"targetEntityType"`
	TargetEntityID   string  `json:"targetEntityId"`
	ValidFromEventID string  `json:"validFromEventId,omitempty"`
	ValidToEventID   string  `json:"validToEventId,omitempty"`
	Confidence       float64 `json:"confidence"`
	Status           string  `json:"status"`
	SourceScope      string  `json:"sourceScope"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
	Revision         int64   `json:"revision"`
}

// CreateSourceDocumentRequest registers a document.
type CreateSourceDocumentRequest struct {
	ProjectID string `json:"projectId"`
	// Type is "novel", "story", "screenplay", "outline" or "notes".
	Type string `json:"type"`
	Name string `json:"name"`
}

// CreateSourceDocument stores a document. No version is created here: a version
// is one import with its own file references and hash.
func (b *DramaBinding) CreateSourceDocument(request CreateSourceDocumentRequest) (SourceDocumentDTO, error) {
	service := b.storyService()
	if service == nil {
		return SourceDocumentDTO{}, bindingUnavailable()
	}
	record, err := service.CreateSourceDocument(b.context(), appstory.CreateSourceDocumentRequest{
		ProjectID:    request.ProjectID,
		DocumentType: storydomain.DocumentType(request.Type),
		Name:         request.Name,
	})
	if err != nil {
		return SourceDocumentDTO{}, toDramaError(err)
	}
	return toSourceDocumentDTO(record), nil
}

// ListSourceDocuments returns a project's documents oldest first.
func (b *DramaBinding) ListSourceDocuments(projectID string) ([]SourceDocumentDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListSourceDocuments(b.context(), projectID)
	if err != nil {
		return nil, toDramaError(err)
	}
	documents := make([]SourceDocumentDTO, 0, len(records))
	for _, record := range records {
		documents = append(documents, toSourceDocumentDTO(record))
	}
	return documents, nil
}

// AddSourceDocumentVersionRequest carries one import of a document.
type AddSourceDocumentVersionRequest struct {
	SourceDocumentID string `json:"sourceDocumentId"`
	// PhysicalFileID is the committed object holding the original upload. It is
	// empty when the text was typed or pasted rather than imported from a file.
	PhysicalFileID string `json:"physicalFileId,omitempty"`
	// NormalizedTextFileID is the committed object holding the canonical text
	// that chapter offsets index. It is required.
	NormalizedTextFileID string `json:"normalizedTextFileId"`
	ContentHash          string `json:"contentHash,omitempty"`
	MIMEType             string `json:"mimeType,omitempty"`
	Encoding             string `json:"encoding,omitempty"`
	CharCount            int    `json:"charCount,omitempty"`
	ImportMetadataJSON   string `json:"importMetadataJson,omitempty"`
	// CreatedByType is "user", "agent", "migration" or "system". An empty value
	// becomes "user".
	CreatedByType string `json:"createdByType,omitempty"`
}

// AddSourceDocumentVersion appends a version and advances the document's
// current version pointer.
func (b *DramaBinding) AddSourceDocumentVersion(request AddSourceDocumentVersionRequest) (SourceDocumentVersionDTO, error) {
	service := b.storyService()
	if service == nil {
		return SourceDocumentVersionDTO{}, bindingUnavailable()
	}
	record, err := service.AddSourceDocumentVersion(b.context(), appstory.AddSourceDocumentVersionRequest{
		SourceDocumentID:     request.SourceDocumentID,
		PhysicalFileID:       request.PhysicalFileID,
		NormalizedTextFileID: request.NormalizedTextFileID,
		ContentHash:          request.ContentHash,
		MIMEType:             request.MIMEType,
		Encoding:             request.Encoding,
		CharCount:            request.CharCount,
		ImportMetadataJSON:   request.ImportMetadataJSON,
		CreatedByType:        storydomain.CreatedByType(request.CreatedByType),
	})
	if err != nil {
		return SourceDocumentVersionDTO{}, toDramaError(err)
	}
	return toSourceDocumentVersionDTO(record), nil
}

// CreateChapterRequest stores one chapter of a document version.
type CreateChapterRequest struct {
	SourceDocumentVersionID string `json:"sourceDocumentVersionId"`
	Ordinal                 int    `json:"ordinal"`
	Title                   string `json:"title"`
	StartOffset             int    `json:"startOffset"`
	EndOffset               int    `json:"endOffset"`
	ContentHash             string `json:"contentHash,omitempty"`
	// Status records how the boundary came to be trusted: "detected" (the import
	// pipeline's value, also what an empty field becomes), "confirmed" or
	// "edited".
	Status string `json:"status,omitempty"`
}

// CreateChapter stores a chapter boundary.
func (b *DramaBinding) CreateChapter(request CreateChapterRequest) (ChapterDTO, error) {
	service := b.storyService()
	if service == nil {
		return ChapterDTO{}, bindingUnavailable()
	}
	record, err := service.CreateChapter(b.context(), appstory.CreateChapterRequest{
		SourceDocumentVersionID: request.SourceDocumentVersionID,
		Ordinal:                 request.Ordinal,
		Title:                   request.Title,
		StartOffset:             request.StartOffset,
		EndOffset:               request.EndOffset,
		ContentHash:             request.ContentHash,
		Status:                  storydomain.ChapterStatus(request.Status),
	})
	if err != nil {
		return ChapterDTO{}, toDramaError(err)
	}
	return toChapterDTO(record), nil
}

// ListChapters returns a document version's chapters in reading order.
func (b *DramaBinding) ListChapters(sourceDocumentVersionID string) ([]ChapterDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListChapters(b.context(), sourceDocumentVersionID)
	if err != nil {
		return nil, toDramaError(err)
	}
	chapters := make([]ChapterDTO, 0, len(records))
	for _, record := range records {
		chapters = append(chapters, toChapterDTO(record))
	}
	return chapters, nil
}

// ReviseChapterRequest corrects a chapter boundary under a revision guard.
type ReviseChapterRequest struct {
	ChapterID   string `json:"chapterId"`
	Title       string `json:"title"`
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
	ContentHash string `json:"contentHash,omitempty"`
	// Status is the value the correction should record; an empty value keeps the
	// stored one.
	Status string `json:"status,omitempty"`
	// Revision is the revision the caller last read.
	Revision int64 `json:"revision"`
}

// ReviseChapter stores a corrected boundary and marks what the change invalidates.
//
// The propagation is what PRD FR-030 asks for: "删除或修改章节后，受影响事实被标记为待
// 复核". A chapter's boundaries decide which text an extracted fact was read from, so
// moving them can leave a fact citing a passage that no longer says what it did.
// The mark is what tells the user to look again.
//
// It runs AFTER the write and its failure is not the command's failure. The
// correction has already landed, and failing the response would tell the caller
// the edit did not happen when it did — which is worse than an unmarked project,
// because the user would retype an edit that is already stored. A propagation
// failure is therefore reported as a warning the caller can act on, and the
// interface says so: this is the same trade ADR-0009 records for notification
// events.
func (b *DramaBinding) ReviseChapter(request ReviseChapterRequest) (ChapterDTO, error) {
	service := b.storyService()
	if service == nil {
		return ChapterDTO{}, bindingUnavailable()
	}
	record, err := service.ReviseChapter(b.context(), appstory.ReviseChapterRequest{
		ChapterID:   request.ChapterID,
		Title:       request.Title,
		StartOffset: request.StartOffset,
		EndOffset:   request.EndOffset,
		ContentHash: request.ContentHash,
		Status:      storydomain.ChapterStatus(request.Status),
		Revision:    request.Revision,
	})
	if err != nil {
		return ChapterDTO{}, toDramaError(err)
	}
	b.propagateChapterChange(record)
	return toChapterDTO(record), nil
}

// propagateChapterChange marks the facts a chapter edit invalidates.
//
// A missing staleness service, a missing project, or a failed walk leaves the
// returned chapter unchanged: the correction is stored and the propagation is a
// best-effort consequence of it. The walk's own bounds mean it always returns,
// with Truncated set when it stopped early.
func (b *DramaBinding) propagateChapterChange(chapter storydomain.Chapter) {
	propagation := b.stalenessService()
	story := b.storyService()
	if propagation == nil || story == nil {
		return
	}
	ctx := b.context()
	projectID, err := b.projectForChapter(ctx, story, chapter)
	if err != nil || projectID == "" {
		return
	}
	_, _ = propagation.PropagateFrom(ctx, appstaleness.PropagateRequest{
		ChangedType: staleness.ArtifactChapter,
		ChangedID:   chapter.ID,
		ProjectID:   projectID,
		Reason:      "The chapter boundary was corrected.",
	})
}

// projectForChapter resolves the project a chapter belongs to.
//
// The walk is chapter to version to document, because a Chapter row carries only
// its version id. The staleness service resolves projects for the artifacts IT
// finds; the artifact the caller changed is the one it cannot resolve, since the
// propagation starts there and never looks it up.
func (b *DramaBinding) projectForChapter(ctx context.Context, service *appstory.Service, chapter storydomain.Chapter) (string, error) {
	version, err := service.GetSourceDocumentVersion(ctx, chapter.SourceDocumentVersionID)
	if err != nil {
		return "", err
	}
	document, err := service.GetSourceDocument(ctx, version.SourceDocumentID)
	if err != nil {
		return "", err
	}
	return document.ProjectID, nil
}

// CreateStoryEntityRequest adds a story entity.
type CreateStoryEntityRequest struct {
	ProjectID     string `json:"projectId"`
	Type          string `json:"type"`
	CanonicalName string `json:"canonicalName"`
	// SourceScope is "original" (the default for an empty value), "adaptation" or
	// "user".
	SourceScope string `json:"sourceScope,omitempty"`
}

// CreateStoryEntity stores an entity as a candidate, which is what the
// confirmation gate then decides on.
func (b *DramaBinding) CreateStoryEntity(request CreateStoryEntityRequest) (StoryEntityDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEntityDTO{}, bindingUnavailable()
	}
	record, err := service.CreateStoryEntity(b.context(), appstory.CreateStoryEntityRequest{
		ProjectID:     request.ProjectID,
		Type:          storydomain.EntityType(request.Type),
		CanonicalName: request.CanonicalName,
		SourceScope:   storydomain.SourceScope(request.SourceScope),
	})
	if err != nil {
		return StoryEntityDTO{}, toDramaError(err)
	}
	return toStoryEntityDTO(record), nil
}

// DecideStoryEntityRequest carries one confirmation decision on an entity.
type DecideStoryEntityRequest struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

// AcceptStoryEntity confirms a candidate entity.
func (b *DramaBinding) AcceptStoryEntity(request DecideStoryEntityRequest) (StoryEntityDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEntityDTO{}, bindingUnavailable()
	}
	record, err := service.AcceptStoryEntity(b.context(), appstory.FactDecisionRequest{
		ID: request.ID, Revision: request.Revision,
	})
	if err != nil {
		return StoryEntityDTO{}, toDramaError(err)
	}
	return toStoryEntityDTO(record), nil
}

// RejectStoryEntity refuses a candidate entity.
func (b *DramaBinding) RejectStoryEntity(request DecideStoryEntityRequest) (StoryEntityDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEntityDTO{}, bindingUnavailable()
	}
	record, err := service.RejectStoryEntity(b.context(), appstory.FactDecisionRequest{
		ID: request.ID, Revision: request.Revision,
	})
	if err != nil {
		return StoryEntityDTO{}, toDramaError(err)
	}
	return toStoryEntityDTO(record), nil
}

// CreateStoryEventRequest adds a story event.
type CreateStoryEventRequest struct {
	ProjectID string `json:"projectId"`
	// ChapterID is empty for an event that belongs to the episode outline rather
	// than to an imported chapter.
	ChapterID   string `json:"chapterId,omitempty"`
	Ordinal     int    `json:"ordinal,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	EventType   string `json:"eventType,omitempty"`
	// StoryTimeText is the diegetic time as the text states it.
	StoryTimeText string `json:"storyTimeText,omitempty"`
	// StoryTimeOrder is the position on the story clock once a pass has ordered
	// the events; it is absent until then.
	StoryTimeOrder   *int    `json:"storyTimeOrder,omitempty"`
	LocationEntityID string  `json:"locationEntityId,omitempty"`
	CauseSummary     string  `json:"causeSummary,omitempty"`
	ResultSummary    string  `json:"resultSummary,omitempty"`
	Importance       string  `json:"importance,omitempty"`
	Confidence       float64 `json:"confidence,omitempty"`
	SourceScope      string  `json:"sourceScope,omitempty"`
	// CreatedByAgentRunID names the extraction run that proposed the event; it is
	// empty when a user wrote it.
	CreatedByAgentRunID string `json:"createdByAgentRunId,omitempty"`
}

// CreateStoryEvent stores an event as a candidate.
func (b *DramaBinding) CreateStoryEvent(request CreateStoryEventRequest) (StoryEventDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEventDTO{}, bindingUnavailable()
	}
	record, err := service.CreateStoryEvent(b.context(), appstory.CreateStoryEventRequest{
		ProjectID:           request.ProjectID,
		ChapterID:           request.ChapterID,
		Ordinal:             request.Ordinal,
		Name:                request.Name,
		Description:         request.Description,
		EventType:           request.EventType,
		StoryTimeText:       request.StoryTimeText,
		StoryTimeOrder:      request.StoryTimeOrder,
		LocationEntityID:    request.LocationEntityID,
		CauseSummary:        request.CauseSummary,
		ResultSummary:       request.ResultSummary,
		Importance:          request.Importance,
		Confidence:          request.Confidence,
		SourceScope:         storydomain.SourceScope(request.SourceScope),
		CreatedByAgentRunID: request.CreatedByAgentRunID,
	})
	if err != nil {
		return StoryEventDTO{}, toDramaError(err)
	}
	return toStoryEventDTO(record), nil
}

// DecideStoryEventRequest carries one confirmation decision on an event.
type DecideStoryEventRequest struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

// AcceptStoryEvent confirms a candidate event.
func (b *DramaBinding) AcceptStoryEvent(request DecideStoryEventRequest) (StoryEventDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEventDTO{}, bindingUnavailable()
	}
	record, err := service.AcceptStoryEvent(b.context(), appstory.FactDecisionRequest{
		ID: request.ID, Revision: request.Revision,
	})
	if err != nil {
		return StoryEventDTO{}, toDramaError(err)
	}
	return toStoryEventDTO(record), nil
}

// RejectStoryEvent refuses a candidate event.
func (b *DramaBinding) RejectStoryEvent(request DecideStoryEventRequest) (StoryEventDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEventDTO{}, bindingUnavailable()
	}
	record, err := service.RejectStoryEvent(b.context(), appstory.FactDecisionRequest{
		ID: request.ID, Revision: request.Revision,
	})
	if err != nil {
		return StoryEventDTO{}, toDramaError(err)
	}
	return toStoryEventDTO(record), nil
}

// CreateStoryRelationRequest adds one edge of the story graph.
type CreateStoryRelationRequest struct {
	ProjectID        string  `json:"projectId"`
	Type             string  `json:"type"`
	SourceEntityType string  `json:"sourceEntityType"`
	SourceEntityID   string  `json:"sourceEntityId"`
	TargetEntityType string  `json:"targetEntityType"`
	TargetEntityID   string  `json:"targetEntityId"`
	ValidFromEventID string  `json:"validFromEventId,omitempty"`
	ValidToEventID   string  `json:"validToEventId,omitempty"`
	Confidence       float64 `json:"confidence,omitempty"`
	SourceScope      string  `json:"sourceScope,omitempty"`
}

// CreateStoryRelation stores a relation as a candidate, checking the relation
// type against the domain vocabulary before the write.
func (b *DramaBinding) CreateStoryRelation(request CreateStoryRelationRequest) (StoryRelationDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryRelationDTO{}, bindingUnavailable()
	}
	record, err := service.CreateStoryRelation(b.context(), appstory.CreateStoryRelationRequest{
		ProjectID:        request.ProjectID,
		Type:             storydomain.RelationType(request.Type),
		SourceEntityType: request.SourceEntityType,
		SourceEntityID:   request.SourceEntityID,
		TargetEntityType: request.TargetEntityType,
		TargetEntityID:   request.TargetEntityID,
		ValidFromEventID: request.ValidFromEventID,
		ValidToEventID:   request.ValidToEventID,
		Confidence:       request.Confidence,
		SourceScope:      storydomain.SourceScope(request.SourceScope),
	})
	if err != nil {
		return StoryRelationDTO{}, toDramaError(err)
	}
	return toStoryRelationDTO(record), nil
}

// ---------------------------------------------------------------------------
// The story-graph reads
//
// WP-05 exposed the fact layer's commands and no query that lists it, which the
// story-graph section recorded as a gap: an empty table would have claimed the
// project has no entities, which the interface could not know. These are the
// lists that close it.
//
// The status filter is a separate argument rather than folded into the project
// id, because the panel asks two different questions — everything the project
// has, and the candidates awaiting review — and an empty value means "any"
// rather than "none".
// ---------------------------------------------------------------------------

// ListStoryEntities returns a project's live entities oldest first.
func (b *DramaBinding) ListStoryEntities(projectID string, status string) ([]StoryEntityDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryEntities(b.context(), projectID, storydomain.FactStatus(status))
	if err != nil {
		return nil, toDramaError(err)
	}
	entities := make([]StoryEntityDTO, 0, len(records))
	for _, record := range records {
		entities = append(entities, toStoryEntityDTO(record))
	}
	return entities, nil
}

// ListStoryEvents returns a project's events in story order.
//
// chapterID narrows the list to one chapter, which is what the chapter panel
// asks for; empty means the whole project.
func (b *DramaBinding) ListStoryEvents(projectID string, chapterID string, status string) ([]StoryEventDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryEvents(b.context(), projectID, chapterID, storydomain.FactStatus(status))
	if err != nil {
		return nil, toDramaError(err)
	}
	events := make([]StoryEventDTO, 0, len(records))
	for _, record := range records {
		events = append(events, toStoryEventDTO(record))
	}
	return events, nil
}

// ListStoryRelations returns a project's relations oldest first.
func (b *DramaBinding) ListStoryRelations(projectID string, status string) ([]StoryRelationDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryRelations(b.context(), projectID, storydomain.FactStatus(status))
	if err != nil {
		return nil, toDramaError(err)
	}
	relations := make([]StoryRelationDTO, 0, len(records))
	for _, record := range records {
		relations = append(relations, toStoryRelationDTO(record))
	}
	return relations, nil
}

// StoryEntityAliasDTO is the transport view of an alternative name.
type StoryEntityAliasDTO struct {
	ID            string `json:"id"`
	StoryEntityID string `json:"storyEntityId"`
	Alias         string `json:"alias"`
	// The source span is omitted when the alias has none, which is the case for a
	// name a user typed: DOMAIN_MODEL section 6.2 marks both offsets nullable, and
	// zero would be indistinguishable from a span at the very start of the text.
	SourceChapterID string `json:"sourceChapterId,omitempty"`
	SourceStart     *int   `json:"sourceStart,omitempty"`
	SourceEnd       *int   `json:"sourceEnd,omitempty"`
	CreatedAt       string `json:"createdAt"`
}

// ListStoryEntityAliases returns one entity's alternative names.
func (b *DramaBinding) ListStoryEntityAliases(storyEntityID string) ([]StoryEntityAliasDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryEntityAliases(b.context(), storyEntityID)
	if err != nil {
		return nil, toDramaError(err)
	}
	aliases := make([]StoryEntityAliasDTO, 0, len(records))
	for _, record := range records {
		aliases = append(aliases, StoryEntityAliasDTO{
			ID: record.ID, StoryEntityID: record.StoryEntityID, Alias: record.Alias,
			SourceChapterID: record.SourceChapterID, SourceStart: record.SourceStart,
			SourceEnd: record.SourceEnd, CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		})
	}
	return aliases, nil
}

// StoryEventParticipantDTO is the transport view of one entity's part in an event.
type StoryEventParticipantDTO struct {
	StoryEventID  string `json:"storyEventId"`
	StoryEntityID string `json:"storyEntityId"`
	Role          string `json:"role"`
	StateBefore   string `json:"stateBefore,omitempty"`
	StateAfter    string `json:"stateAfter,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// ListStoryEventParticipants returns one event's participants.
func (b *DramaBinding) ListStoryEventParticipants(storyEventID string) ([]StoryEventParticipantDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryEventParticipants(b.context(), storyEventID)
	if err != nil {
		return nil, toDramaError(err)
	}
	participants := make([]StoryEventParticipantDTO, 0, len(records))
	for _, record := range records {
		participants = append(participants, StoryEventParticipantDTO{
			StoryEventID: record.StoryEventID, StoryEntityID: record.StoryEntityID,
			Role: string(record.Role), StateBefore: record.StateBefore,
			StateAfter: record.StateAfter, CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		})
	}
	return participants, nil
}

// SplitChapterRequest divides one chapter into two.
type SplitChapterRequest struct {
	ChapterID string `json:"chapterId"`
	// SplitAtOffset is where the second chapter begins, in the version's
	// coordinates.
	SplitAtOffset int    `json:"splitAtOffset"`
	SecondTitle   string `json:"secondTitle,omitempty"`
	Revision      int64  `json:"revision"`
}

// SplitChapter divides a chapter in two.
//
// PRD FR-020 lists 手动合并/拆分 among the import flow's MUST items and
// AC-STORY-001's acceptance names 章节拆分可人工修正并保存.
func (b *DramaBinding) SplitChapter(request SplitChapterRequest) ([]ChapterDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.SplitChapter(b.context(), appstory.SplitChapterRequest{
		ChapterID:     request.ChapterID,
		SplitAtOffset: request.SplitAtOffset,
		SecondTitle:   request.SecondTitle,
		Revision:      request.Revision,
	})
	if err != nil {
		return nil, toDramaError(err)
	}
	chapters := make([]ChapterDTO, 0, len(records))
	for _, record := range records {
		chapters = append(chapters, toChapterDTO(record))
	}
	return chapters, nil
}

// MergeChapterRequest absorbs one chapter into the one before it.
type MergeChapterRequest struct {
	FirstChapterID  string `json:"firstChapterId"`
	SecondChapterID string `json:"secondChapterId"`
	Revision        int64  `json:"revision"`
}

// MergeChapter absorbs the chapter immediately after the given one.
func (b *DramaBinding) MergeChapter(request MergeChapterRequest) (ChapterDTO, error) {
	service := b.storyService()
	if service == nil {
		return ChapterDTO{}, bindingUnavailable()
	}
	record, err := service.MergeChapter(b.context(), appstory.MergeChapterRequest{
		FirstChapterID:  request.FirstChapterID,
		SecondChapterID: request.SecondChapterID,
		Revision:        request.Revision,
	})
	if err != nil {
		return ChapterDTO{}, toDramaError(err)
	}
	return toChapterDTO(record), nil
}

// LockStoryEntityRequest pins or releases a fact under a revision guard.
type LockStoryEntityRequest struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

// LockStoryEntity pins a fact so a later pass cannot revise it.
//
// AC-STORY-002 lists 接受/拒绝/锁定 as the three decisions a candidate may
// receive. The lock had no command at all: 'locked' was a status several gates
// refused to move a fact out of, and nothing could put a fact into it.
func (b *DramaBinding) LockStoryEntity(request LockStoryEntityRequest) (StoryEntityDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEntityDTO{}, bindingUnavailable()
	}
	record, err := service.LockStoryEntity(b.context(), appstory.LockStoryEntityRequest{
		ID: request.ID, Revision: request.Revision,
	})
	if err != nil {
		return StoryEntityDTO{}, toDramaError(err)
	}
	return toStoryEntityDTO(record), nil
}

// UnlockStoryEntity releases a lock, leaving the decision it protected standing.
func (b *DramaBinding) UnlockStoryEntity(request LockStoryEntityRequest) (StoryEntityDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEntityDTO{}, bindingUnavailable()
	}
	record, err := service.UnlockStoryEntity(b.context(), appstory.LockStoryEntityRequest{
		ID: request.ID, Revision: request.Revision,
	})
	if err != nil {
		return StoryEntityDTO{}, toDramaError(err)
	}
	return toStoryEntityDTO(record), nil
}

// LockStoryEvent pins an event.
func (b *DramaBinding) LockStoryEvent(request LockStoryEntityRequest) (StoryEventDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEventDTO{}, bindingUnavailable()
	}
	record, err := service.LockStoryEvent(b.context(), appstory.LockStoryEntityRequest{
		ID: request.ID, Revision: request.Revision,
	})
	if err != nil {
		return StoryEventDTO{}, toDramaError(err)
	}
	return toStoryEventDTO(record), nil
}

// UnlockStoryEvent releases an event's lock.
func (b *DramaBinding) UnlockStoryEvent(request LockStoryEntityRequest) (StoryEventDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryEventDTO{}, bindingUnavailable()
	}
	record, err := service.UnlockStoryEvent(b.context(), appstory.LockStoryEntityRequest{
		ID: request.ID, Revision: request.Revision,
	})
	if err != nil {
		return StoryEventDTO{}, toDramaError(err)
	}
	return toStoryEventDTO(record), nil
}

// StoryFactConflictDTO is the transport view of a recorded disagreement.
//
// Both sides are named by fact type and id rather than by a rendered sentence,
// because a conflict is between two FACTS and the panel resolves it by showing
// the two rows. A server-rendered description would be a second place the fact's
// text lives, and it would go stale.
type StoryFactConflictDTO struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	LeftFactType  string `json:"leftFactType"`
	LeftFactID    string `json:"leftFactId"`
	RightFactType string `json:"rightFactType"`
	RightFactID   string `json:"rightFactId"`
	ConflictType  string `json:"conflictType,omitempty"`
	Status        string `json:"status"`
	Resolution    string `json:"resolution,omitempty"`
	ResolvedBy    string `json:"resolvedBy,omitempty"`
	CreatedAt     string `json:"createdAt"`
	ResolvedAt    string `json:"resolvedAt,omitempty"`
}

// ListStoryConflicts returns a project's recorded conflicts newest first.
//
// WP-05 exposed the commands that open and resolve a conflict and no query that
// lists them, so a conflict could be created and never found again. AC-STORY-002
// names 冲突记录 as an acceptance point, and a record nobody can list is not one.
func (b *DramaBinding) ListStoryConflicts(projectID string, status string) ([]StoryFactConflictDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryConflicts(b.context(), projectID, storydomain.ConflictStatus(status))
	if err != nil {
		return nil, toDramaError(err)
	}
	conflicts := make([]StoryFactConflictDTO, 0, len(records))
	for _, record := range records {
		conflicts = append(conflicts, StoryFactConflictDTO{
			ID: record.ID, ProjectID: record.ProjectID,
			LeftFactType: string(record.LeftFactType), LeftFactID: record.LeftFactID,
			RightFactType: string(record.RightFactType), RightFactID: record.RightFactID,
			ConflictType: record.ConflictType, Status: string(record.Status),
			Resolution: record.Resolution, ResolvedBy: record.ResolvedBy,
			CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
			// An unresolved conflict has no resolution time, and the zero value is
			// rendered as empty rather than as year one, which is what the DTO's
			// omitempty expects.
			ResolvedAt: formatOptionalTime(record.ResolvedAt),
		})
	}
	return conflicts, nil
}

// OpenStoryConflictRequest records a disagreement between two facts.
type OpenStoryConflictRequest struct {
	ProjectID     string `json:"projectId"`
	LeftFactType  string `json:"leftFactType"`
	LeftFactID    string `json:"leftFactId"`
	RightFactType string `json:"rightFactType"`
	RightFactID   string `json:"rightFactId"`
	ConflictType  string `json:"conflictType,omitempty"`
}

// OpenStoryConflict records a disagreement.
func (b *DramaBinding) OpenStoryConflict(request OpenStoryConflictRequest) (StoryFactConflictDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryFactConflictDTO{}, bindingUnavailable()
	}
	record, err := service.OpenConflict(b.context(), appstory.OpenConflictRequest{
		ProjectID:     request.ProjectID,
		LeftFactType:  storydomain.FactType(request.LeftFactType),
		LeftFactID:    request.LeftFactID,
		RightFactType: storydomain.FactType(request.RightFactType),
		RightFactID:   request.RightFactID,
		ConflictType:  request.ConflictType,
	})
	if err != nil {
		return StoryFactConflictDTO{}, toDramaError(err)
	}
	return StoryFactConflictDTO{
		ID: record.ID, ProjectID: record.ProjectID,
		LeftFactType: string(record.LeftFactType), LeftFactID: record.LeftFactID,
		RightFactType: string(record.RightFactType), RightFactID: record.RightFactID,
		ConflictType: record.ConflictType, Status: string(record.Status),
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}, nil
}

// ResolveStoryConflictRequest records what was decided about a conflict.
type ResolveStoryConflictRequest struct {
	ConflictID string `json:"conflictId"`
	Resolution string `json:"resolution"`
	ResolvedBy string `json:"resolvedBy"`
}

// ResolveStoryConflict closes a conflict with a stated resolution.
func (b *DramaBinding) ResolveStoryConflict(request ResolveStoryConflictRequest) (StoryFactConflictDTO, error) {
	service := b.storyService()
	if service == nil {
		return StoryFactConflictDTO{}, bindingUnavailable()
	}
	record, err := service.ResolveConflict(b.context(), appstory.ResolveConflictRequest{
		ConflictID: request.ConflictID,
		Resolution: request.Resolution,
		ResolvedBy: request.ResolvedBy,
	})
	if err != nil {
		return StoryFactConflictDTO{}, toDramaError(err)
	}
	return StoryFactConflictDTO{
		ID: record.ID, ProjectID: record.ProjectID,
		LeftFactType: string(record.LeftFactType), LeftFactID: record.LeftFactID,
		RightFactType: string(record.RightFactType), RightFactID: record.RightFactID,
		ConflictType: record.ConflictType, Status: string(record.Status),
		Resolution: record.Resolution, ResolvedBy: record.ResolvedBy,
		CreatedAt:  record.CreatedAt.UTC().Format(rfc3339),
		ResolvedAt: formatOptionalTime(record.ResolvedAt),
	}, nil
}

// StoryFactSourceDTO is the transport view of the evidence a fact cites.
//
// It carries the version and the offsets rather than any text: DOMAIN_MODEL
// section 6.6 says the passage is read back through the offsets, so the row names
// where to look instead of copying what is there.
type StoryFactSourceDTO struct {
	ID                      string `json:"id"`
	FactType                string `json:"factType"`
	FactID                  string `json:"factId"`
	ChapterID               string `json:"chapterId,omitempty"`
	SourceDocumentVersionID string `json:"sourceDocumentVersionId"`
	StartOffset             *int   `json:"startOffset,omitempty"`
	EndOffset               *int   `json:"endOffset,omitempty"`
	QuoteHash               string `json:"quoteHash,omitempty"`
	SourceKind              string `json:"sourceKind"`
	CreatedAt               string `json:"createdAt"`
}

// ListStoryFactSources returns the evidence one fact cites.
func (b *DramaBinding) ListStoryFactSources(factType string, factID string) ([]StoryFactSourceDTO, error) {
	service := b.storyService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryFactSources(b.context(), storydomain.FactType(factType), factID)
	if err != nil {
		return nil, toDramaError(err)
	}
	sources := make([]StoryFactSourceDTO, 0, len(records))
	for _, record := range records {
		sources = append(sources, StoryFactSourceDTO{
			ID: record.ID, FactType: string(record.FactType), FactID: record.FactID,
			ChapterID: record.ChapterID, SourceDocumentVersionID: record.SourceDocumentVersionID,
			StartOffset: record.StartOffset, EndOffset: record.EndOffset,
			QuoteHash: record.QuoteHash, SourceKind: string(record.SourceKind),
			CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		})
	}
	return sources, nil
}

// ---------------------------------------------------------------------------
// Script: episodes, scripts, scenes and shots
// ---------------------------------------------------------------------------

// EpisodeDTO is the transport view of one instalment.
type EpisodeDTO struct {
	ID string `json:"id"`
	// Key is the business key, such as "S1E3".
	Key                   string `json:"key"`
	ProjectID             string `json:"projectId"`
	SeasonNumber          int    `json:"seasonNumber"`
	EpisodeNumber         int    `json:"episodeNumber"`
	Title                 string `json:"title"`
	Status                string `json:"status"`
	SourceChapterStartID  string `json:"sourceChapterStartId,omitempty"`
	SourceChapterEndID    string `json:"sourceChapterEndId,omitempty"`
	TargetDurationSeconds int    `json:"targetDurationSeconds"`
	// The three current-version pointers are empty until the corresponding
	// artifact exists.
	CurrentStorySkeletonVersionID      string `json:"currentStorySkeletonVersionId,omitempty"`
	CurrentAdaptationStrategyVersionID string `json:"currentAdaptationStrategyVersionId,omitempty"`
	CurrentScriptVersionID             string `json:"currentScriptVersionId,omitempty"`
	DeletedAt                          string `json:"deletedAt,omitempty"`
	CreatedAt                          string `json:"createdAt"`
	UpdatedAt                          string `json:"updatedAt"`
	Revision                           int64  `json:"revision"`
}

// ScriptDTO is the transport view of a script identity.
type ScriptDTO struct {
	ID               string `json:"id"`
	EpisodeID        string `json:"episodeId"`
	CurrentVersionID string `json:"currentVersionId,omitempty"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
	Revision         int64  `json:"revision"`
}

// ScriptVersionDTO is the transport view of one version of a script's content.
type ScriptVersionDTO struct {
	ID            string `json:"id"`
	ScriptID      string `json:"scriptId"`
	VersionNumber int    `json:"versionNumber"`
	Status        string `json:"status"`
	// BasedOnVersionID links a revision to the version a user edited.
	BasedOnVersionID            string `json:"basedOnVersionId,omitempty"`
	StorySkeletonVersionID      string `json:"storySkeletonVersionId"`
	AdaptationStrategyVersionID string `json:"adaptationStrategyVersionId"`
	EstimatedDurationSeconds    int    `json:"estimatedDurationSeconds"`
	Summary                     string `json:"summary,omitempty"`
	SourceAgentRunID            string `json:"sourceAgentRunId,omitempty"`
	CreatedByType               string `json:"createdByType"`
	CreatedByID                 string `json:"createdById,omitempty"`
	ChangeReason                string `json:"changeReason,omitempty"`
	// LegacyMetadata retains migrated fields the schema does not model.
	LegacyMetadata string `json:"legacyMetadata,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

// SceneDTO is the transport view of one scene of a script version.
type SceneDTO struct {
	ID              string `json:"id"`
	ScriptVersionID string `json:"scriptVersionId"`
	Ordinal         int    `json:"ordinal"`
	SceneNumber     string `json:"sceneNumber,omitempty"`
	Slugline        string `json:"slugline,omitempty"`
	// InteriorExterior is "INT", "EXT", "INT_EXT" or "OTHER".
	InteriorExterior         string `json:"interiorExterior"`
	LocationEntityID         string `json:"locationEntityId,omitempty"`
	TimeOfDay                string `json:"timeOfDay,omitempty"`
	Summary                  string `json:"summary,omitempty"`
	DramaticGoal             string `json:"dramaticGoal,omitempty"`
	EstimatedDurationSeconds int    `json:"estimatedDurationSeconds"`
	SourceStoryEventID       string `json:"sourceStoryEventId,omitempty"`
	// IsOriginalAdaptation marks a scene that is not in the source material.
	IsOriginalAdaptation bool   `json:"isOriginalAdaptation"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
	Revision             int64  `json:"revision"`
}

// ShotDTO is the transport view of one camera setup.
type ShotDTO struct {
	ID                       string `json:"id"`
	SceneID                  string `json:"sceneId"`
	Ordinal                  int    `json:"ordinal"`
	ShotNumber               string `json:"shotNumber,omitempty"`
	ShotSize                 string `json:"shotSize,omitempty"`
	CameraAngle              string `json:"cameraAngle,omitempty"`
	CameraMovement           string `json:"cameraMovement,omitempty"`
	EstimatedDurationSeconds int    `json:"estimatedDurationSeconds"`
	VisualDescription        string `json:"visualDescription,omitempty"`
	ActionDescription        string `json:"actionDescription,omitempty"`
	AudioIntent              string `json:"audioIntent,omitempty"`
	ContinuityNotes          string `json:"continuityNotes,omitempty"`
	Status                   string `json:"status"`
	CreatedAt                string `json:"createdAt"`
	UpdatedAt                string `json:"updatedAt"`
	Revision                 int64  `json:"revision"`
}

// CreateEpisodeRequest plans one instalment.
type CreateEpisodeRequest struct {
	ProjectID             string `json:"projectId"`
	SeasonNumber          int    `json:"seasonNumber"`
	EpisodeNumber         int    `json:"episodeNumber"`
	Title                 string `json:"title,omitempty"`
	TargetDurationSeconds int    `json:"targetDurationSeconds,omitempty"`
}

// CreateEpisode stores an episode in the planning state.
func (b *DramaBinding) CreateEpisode(request CreateEpisodeRequest) (EpisodeDTO, error) {
	service := b.scriptService()
	if service == nil {
		return EpisodeDTO{}, bindingUnavailable()
	}
	record, err := service.CreateEpisode(b.context(), appscript.CreateEpisodeRequest{
		ProjectID:             request.ProjectID,
		SeasonNumber:          request.SeasonNumber,
		EpisodeNumber:         request.EpisodeNumber,
		Title:                 request.Title,
		TargetDurationSeconds: request.TargetDurationSeconds,
	})
	if err != nil {
		return EpisodeDTO{}, toDramaError(err)
	}
	return toEpisodeDTO(record), nil
}

// ListEpisodes returns a project's episodes in season and episode order.
func (b *DramaBinding) ListEpisodes(projectID string) ([]EpisodeDTO, error) {
	service := b.scriptService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListEpisodes(b.context(), projectID)
	if err != nil {
		return nil, toDramaError(err)
	}
	episodes := make([]EpisodeDTO, 0, len(records))
	for _, record := range records {
		episodes = append(episodes, toEpisodeDTO(record))
	}
	return episodes, nil
}

// UpdateEpisodeStatusRequest carries a production state change.
type UpdateEpisodeStatusRequest struct {
	EpisodeID string `json:"episodeId"`
	// Status is "planning", "writing", "approved", "production" or "completed".
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
}

// UpdateEpisodeStatus stores an episode's production state under a revision
// guard.
func (b *DramaBinding) UpdateEpisodeStatus(request UpdateEpisodeStatusRequest) (EpisodeDTO, error) {
	service := b.scriptService()
	if service == nil {
		return EpisodeDTO{}, bindingUnavailable()
	}
	record, err := service.UpdateEpisodeStatus(b.context(), appscript.UpdateEpisodeStatusRequest{
		EpisodeID: request.EpisodeID,
		Status:    scriptdomain.EpisodeStatus(request.Status),
		Revision:  request.Revision,
	})
	if err != nil {
		return EpisodeDTO{}, toDramaError(err)
	}
	return toEpisodeDTO(record), nil
}

// EnsureScript returns an episode's script, creating it when the episode has
// none. A second call returns the same row.
func (b *DramaBinding) EnsureScript(episodeID string) (ScriptDTO, error) {
	service := b.scriptService()
	if service == nil {
		return ScriptDTO{}, bindingUnavailable()
	}
	record, err := service.EnsureScript(b.context(), episodeID)
	if err != nil {
		return ScriptDTO{}, toDramaError(err)
	}
	return toScriptDTO(record), nil
}

// CreateScriptVersionRequest adds one version of a script's content.
type CreateScriptVersionRequest struct {
	ScriptID         string `json:"scriptId"`
	BasedOnVersionID string `json:"basedOnVersionId,omitempty"`
	// StorySkeletonVersionID and AdaptationStrategyVersionID name the two inputs
	// the version was produced from. Both are required by the domain.
	StorySkeletonVersionID      string `json:"storySkeletonVersionId"`
	AdaptationStrategyVersionID string `json:"adaptationStrategyVersionId"`
	EstimatedDurationSeconds    int    `json:"estimatedDurationSeconds,omitempty"`
	Summary                     string `json:"summary,omitempty"`
	SourceAgentRunID            string `json:"sourceAgentRunId,omitempty"`
	CreatedByType               string `json:"createdByType,omitempty"`
	CreatedByID                 string `json:"createdById,omitempty"`
	ChangeReason                string `json:"changeReason,omitempty"`
	LegacyMetadata              string `json:"legacyMetadata,omitempty"`
}

// CreateScriptVersion appends a script version in the draft state.
func (b *DramaBinding) CreateScriptVersion(request CreateScriptVersionRequest) (ScriptVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return ScriptVersionDTO{}, bindingUnavailable()
	}
	record, err := service.CreateScriptVersion(b.context(), appscript.CreateScriptVersionRequest{
		ScriptID:                    request.ScriptID,
		BasedOnVersionID:            request.BasedOnVersionID,
		StorySkeletonVersionID:      request.StorySkeletonVersionID,
		AdaptationStrategyVersionID: request.AdaptationStrategyVersionID,
		EstimatedDurationSeconds:    request.EstimatedDurationSeconds,
		Summary:                     request.Summary,
		SourceAgentRunID:            request.SourceAgentRunID,
		CreatedByType:               versioning.CreatedByType(request.CreatedByType),
		CreatedByID:                 request.CreatedByID,
		ChangeReason:                request.ChangeReason,
		LegacyMetadata:              request.LegacyMetadata,
	})
	if err != nil {
		return ScriptVersionDTO{}, toDramaError(err)
	}
	return toScriptVersionDTO(record), nil
}

// ApproveScriptVersionRequest approves one script version.
type ApproveScriptVersionRequest struct {
	// ScriptVersionID names the version to approve. There is no expected revision
	// because a script version has none: its status is the concurrency token.
	ScriptVersionID string `json:"scriptVersionId"`
}

// ApproveScriptVersion makes one version the script's approved version, turning
// any previous approval into 'superseded'.
func (b *DramaBinding) ApproveScriptVersion(request ApproveScriptVersionRequest) (ScriptVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return ScriptVersionDTO{}, bindingUnavailable()
	}
	record, err := service.ApproveScriptVersion(b.context(), appscript.ApproveScriptVersionRequest{
		ScriptVersionID: request.ScriptVersionID,
	})
	if err != nil {
		return ScriptVersionDTO{}, toDramaError(err)
	}
	return toScriptVersionDTO(record), nil
}

// CreateSceneRequest adds one scene to a script version.
type CreateSceneRequest struct {
	ScriptVersionID string `json:"scriptVersionId"`
	Ordinal         int    `json:"ordinal"`
	SceneNumber     string `json:"sceneNumber,omitempty"`
	Slugline        string `json:"slugline,omitempty"`
	// InteriorExterior is "INT", "EXT", "INT_EXT" or "OTHER"; an empty value
	// becomes "OTHER".
	InteriorExterior         string `json:"interiorExterior,omitempty"`
	LocationEntityID         string `json:"locationEntityId,omitempty"`
	TimeOfDay                string `json:"timeOfDay,omitempty"`
	Summary                  string `json:"summary,omitempty"`
	DramaticGoal             string `json:"dramaticGoal,omitempty"`
	EstimatedDurationSeconds int    `json:"estimatedDurationSeconds,omitempty"`
	SourceStoryEventID       string `json:"sourceStoryEventId,omitempty"`
	IsOriginalAdaptation     bool   `json:"isOriginalAdaptation,omitempty"`
}

// CreateScene stores a scene of a script version.
func (b *DramaBinding) CreateScene(request CreateSceneRequest) (SceneDTO, error) {
	service := b.scriptService()
	if service == nil {
		return SceneDTO{}, bindingUnavailable()
	}
	record, err := service.CreateScene(b.context(), appscript.CreateSceneRequest{
		ScriptVersionID:          request.ScriptVersionID,
		Ordinal:                  request.Ordinal,
		SceneNumber:              request.SceneNumber,
		Slugline:                 request.Slugline,
		InteriorExterior:         scriptdomain.InteriorExterior(request.InteriorExterior),
		LocationEntityID:         request.LocationEntityID,
		TimeOfDay:                request.TimeOfDay,
		Summary:                  request.Summary,
		DramaticGoal:             request.DramaticGoal,
		EstimatedDurationSeconds: request.EstimatedDurationSeconds,
		SourceStoryEventID:       request.SourceStoryEventID,
		IsOriginalAdaptation:     request.IsOriginalAdaptation,
	})
	if err != nil {
		return SceneDTO{}, toDramaError(err)
	}
	return toSceneDTO(record), nil
}

// ListScenes returns a script version's scenes in script order.
func (b *DramaBinding) ListScenes(scriptVersionID string) ([]SceneDTO, error) {
	service := b.scriptService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListScenes(b.context(), scriptVersionID)
	if err != nil {
		return nil, toDramaError(err)
	}
	scenes := make([]SceneDTO, 0, len(records))
	for _, record := range records {
		scenes = append(scenes, toSceneDTO(record))
	}
	return scenes, nil
}

// CreateShotRequest adds one camera setup to a scene.
type CreateShotRequest struct {
	SceneID                  string `json:"sceneId"`
	Ordinal                  int    `json:"ordinal"`
	ShotNumber               string `json:"shotNumber,omitempty"`
	ShotSize                 string `json:"shotSize,omitempty"`
	CameraAngle              string `json:"cameraAngle,omitempty"`
	CameraMovement           string `json:"cameraMovement,omitempty"`
	EstimatedDurationSeconds int    `json:"estimatedDurationSeconds,omitempty"`
	VisualDescription        string `json:"visualDescription,omitempty"`
	ActionDescription        string `json:"actionDescription,omitempty"`
	AudioIntent              string `json:"audioIntent,omitempty"`
	ContinuityNotes          string `json:"continuityNotes,omitempty"`
}

// CreateShot stores a shot of a scene in the draft state.
func (b *DramaBinding) CreateShot(request CreateShotRequest) (ShotDTO, error) {
	service := b.scriptService()
	if service == nil {
		return ShotDTO{}, bindingUnavailable()
	}
	record, err := service.CreateShot(b.context(), appscript.CreateShotRequest{
		SceneID:                  request.SceneID,
		Ordinal:                  request.Ordinal,
		ShotNumber:               request.ShotNumber,
		ShotSize:                 request.ShotSize,
		CameraAngle:              request.CameraAngle,
		CameraMovement:           request.CameraMovement,
		EstimatedDurationSeconds: request.EstimatedDurationSeconds,
		VisualDescription:        request.VisualDescription,
		ActionDescription:        request.ActionDescription,
		AudioIntent:              request.AudioIntent,
		ContinuityNotes:          request.ContinuityNotes,
	})
	if err != nil {
		return ShotDTO{}, toDramaError(err)
	}
	return toShotDTO(record), nil
}

// ListShots returns a scene's shots in order.
func (b *DramaBinding) ListShots(sceneID string) ([]ShotDTO, error) {
	service := b.scriptService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListShots(b.context(), sceneID)
	if err != nil {
		return nil, toDramaError(err)
	}
	shots := make([]ShotDTO, 0, len(records))
	for _, record := range records {
		shots = append(shots, toShotDTO(record))
	}
	return shots, nil
}

// ---------------------------------------------------------------------------
// Storyboard: director plans, storyboards, items and panels
// ---------------------------------------------------------------------------

// DirectorPlanVersionDTO is the transport view of one director plan version.
type DirectorPlanVersionDTO struct {
	ID            string `json:"id"`
	EpisodeID     string `json:"episodeId"`
	VersionNumber int    `json:"versionNumber"`
	Status        string `json:"status"`
	// BasedOnVersionID links this plan to the one it revised.
	BasedOnVersionID  string `json:"basedOnVersionId,omitempty"`
	ScriptVersionID   string `json:"scriptVersionId"`
	VisualRhythm      string `json:"visualRhythm,omitempty"`
	CameraLanguage    string `json:"cameraLanguage,omitempty"`
	ColorLighting     string `json:"colorLighting,omitempty"`
	Staging           string `json:"staging,omitempty"`
	ContinuityRules   string `json:"continuityRules,omitempty"`
	AudioDirection    string `json:"audioDirection,omitempty"`
	ShotOverridesJSON string `json:"shotOverridesJson,omitempty"`
	SourceAgentRunID  string `json:"sourceAgentRunId,omitempty"`
	CreatedByType     string `json:"createdByType"`
	CreatedByID       string `json:"createdById,omitempty"`
	ChangeReason      string `json:"changeReason,omitempty"`
	LegacyMetadata    string `json:"legacyMetadata,omitempty"`
	CreatedAt         string `json:"createdAt"`
}

// StoryboardDTO is the transport view of an episode's storyboard identity.
type StoryboardDTO struct {
	ID               string `json:"id"`
	EpisodeID        string `json:"episodeId"`
	CurrentVersionID string `json:"currentVersionId,omitempty"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
	Revision         int64  `json:"revision"`
}

// StoryboardVersionDTO is the transport view of one storyboard version.
type StoryboardVersionDTO struct {
	ID            string `json:"id"`
	StoryboardID  string `json:"storyboardId"`
	VersionNumber int    `json:"versionNumber"`
	Status        string `json:"status"`
	// ScriptVersionID and DirectorPlanVersionID name the two inputs the version
	// was built from; the stale chain walks both.
	ScriptVersionID       string `json:"scriptVersionId"`
	DirectorPlanVersionID string `json:"directorPlanVersionId"`
	BasedOnVersionID      string `json:"basedOnVersionId,omitempty"`
	SourceAgentRunID      string `json:"sourceAgentRunId,omitempty"`
	CreatedByType         string `json:"createdByType"`
	CreatedByID           string `json:"createdById,omitempty"`
	ChangeReason          string `json:"changeReason,omitempty"`
	LegacyMetadata        string `json:"legacyMetadata,omitempty"`
	CreatedAt             string `json:"createdAt"`
}

// StoryboardItemDTO is the transport view of one shot's row in a version.
type StoryboardItemDTO struct {
	ID                   string `json:"id"`
	StoryboardVersionID  string `json:"storyboardVersionId"`
	ShotID               string `json:"shotId"`
	Ordinal              int    `json:"ordinal"`
	ShotSize             string `json:"shotSize,omitempty"`
	CameraAngle          string `json:"cameraAngle,omitempty"`
	CameraMovement       string `json:"cameraMovement,omitempty"`
	DurationSeconds      int    `json:"durationSeconds"`
	VisualDescription    string `json:"visualDescription,omitempty"`
	ActionDescription    string `json:"actionDescription,omitempty"`
	DialogueAudioSummary string `json:"dialogueAudioSummary,omitempty"`
	ContinuityNotes      string `json:"continuityNotes,omitempty"`
	// FR-070's remaining three, added by migration 000018: what a video model is given to
	// render the shot's two ends and its motion. They are the SHOOTING decision rather than
	// a fact about the script's shot, which is why they live on the row and why they had
	// nowhere to be stored before WP-09.
	FirstFrameDescription  string `json:"firstFrameDescription,omitempty"`
	LastFrameDescription   string `json:"lastFrameDescription,omitempty"`
	VideoMotionDescription string `json:"videoMotionDescription,omitempty"`
	Status                 string `json:"status"`
	CreatedAt              string `json:"createdAt"`
	UpdatedAt              string `json:"updatedAt"`
	Revision               int64  `json:"revision"`
}

// StoryboardPanelVersionDTO is the transport view of one panel version.
type StoryboardPanelVersionDTO struct {
	ID               string `json:"id"`
	StoryboardItemID string `json:"storyboardItemId"`
	VersionNumber    int    `json:"versionNumber"`
	Status           string `json:"status"`
	// BasedOnVersionID links this panel to the one it revised.
	BasedOnVersionID string `json:"basedOnVersionId,omitempty"`
	VisualPrompt     string `json:"visualPrompt,omitempty"`
	NegativePrompt   string `json:"negativePrompt,omitempty"`
	// ReferencePolicyJSON is policy metadata. The approved image is
	// ApprovedImageAssetVersionID, which is a column rather than a JSON field.
	ReferencePolicyJSON string `json:"referencePolicyJson,omitempty"`
	// ApprovedImageAssetVersionID is empty until an image is approved.
	ApprovedImageAssetVersionID string `json:"approvedImageAssetVersionId,omitempty"`
	SourceAgentRunID            string `json:"sourceAgentRunId,omitempty"`
	CreatedByType               string `json:"createdByType"`
	CreatedByID                 string `json:"createdById,omitempty"`
	ChangeReason                string `json:"changeReason,omitempty"`
	LegacyMetadata              string `json:"legacyMetadata,omitempty"`
	CreatedAt                   string `json:"createdAt"`
}

// CreateDirectorPlanVersionRequest carries a new director plan.
type CreateDirectorPlanVersionRequest struct {
	EpisodeID string `json:"episodeId"`
	// ScriptVersionID names the script version the plan stages. It is required:
	// a plan is a reading of a specific script version.
	ScriptVersionID   string `json:"scriptVersionId"`
	BasedOnVersionID  string `json:"basedOnVersionId,omitempty"`
	VisualRhythm      string `json:"visualRhythm,omitempty"`
	CameraLanguage    string `json:"cameraLanguage,omitempty"`
	ColorLighting     string `json:"colorLighting,omitempty"`
	Staging           string `json:"staging,omitempty"`
	ContinuityRules   string `json:"continuityRules,omitempty"`
	AudioDirection    string `json:"audioDirection,omitempty"`
	ShotOverridesJSON string `json:"shotOverridesJson,omitempty"`
	SourceAgentRunID  string `json:"sourceAgentRunId,omitempty"`
	CreatedByType     string `json:"createdByType,omitempty"`
	CreatedByID       string `json:"createdById,omitempty"`
	ChangeReason      string `json:"changeReason,omitempty"`
	LegacyMetadata    string `json:"legacyMetadata,omitempty"`
}

// CreateDirectorPlanVersion appends a director plan version in the draft state.
func (b *DramaBinding) CreateDirectorPlanVersion(request CreateDirectorPlanVersionRequest) (DirectorPlanVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return DirectorPlanVersionDTO{}, bindingUnavailable()
	}
	record, err := service.CreateDirectorPlanVersion(b.context(), appstoryboard.CreateDirectorPlanVersionRequest{
		EpisodeID:         request.EpisodeID,
		ScriptVersionID:   request.ScriptVersionID,
		BasedOnVersionID:  request.BasedOnVersionID,
		VisualRhythm:      request.VisualRhythm,
		CameraLanguage:    request.CameraLanguage,
		ColorLighting:     request.ColorLighting,
		Staging:           request.Staging,
		ContinuityRules:   request.ContinuityRules,
		AudioDirection:    request.AudioDirection,
		ShotOverridesJSON: request.ShotOverridesJSON,
		SourceAgentRunID:  request.SourceAgentRunID,
		CreatedByType:     versioning.CreatedByType(request.CreatedByType),
		CreatedByID:       request.CreatedByID,
		ChangeReason:      request.ChangeReason,
		LegacyMetadata:    request.LegacyMetadata,
	})
	if err != nil {
		return DirectorPlanVersionDTO{}, toDramaError(err)
	}
	return toDirectorPlanVersionDTO(record), nil
}

// EnsureStoryboard returns an episode's storyboard identity, creating it the
// first time.
func (b *DramaBinding) EnsureStoryboard(episodeID string) (StoryboardDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardDTO{}, bindingUnavailable()
	}
	record, err := service.EnsureStoryboard(b.context(), episodeID)
	if err != nil {
		return StoryboardDTO{}, toDramaError(err)
	}
	return toStoryboardDTO(record), nil
}

// CreateStoryboardVersionRequest carries a new storyboard version.
type CreateStoryboardVersionRequest struct {
	StoryboardID string `json:"storyboardId"`
	// ScriptVersionID and DirectorPlanVersionID are required: section 9.3 makes
	// them fields of the version rather than provenance metadata, and the stale
	// chain walks both.
	ScriptVersionID       string `json:"scriptVersionId"`
	DirectorPlanVersionID string `json:"directorPlanVersionId"`
	BasedOnVersionID      string `json:"basedOnVersionId,omitempty"`
	SourceAgentRunID      string `json:"sourceAgentRunId,omitempty"`
	CreatedByType         string `json:"createdByType,omitempty"`
	CreatedByID           string `json:"createdById,omitempty"`
	ChangeReason          string `json:"changeReason,omitempty"`
	LegacyMetadata        string `json:"legacyMetadata,omitempty"`
}

// CreateStoryboardVersion appends a storyboard version in the draft state.
func (b *DramaBinding) CreateStoryboardVersion(request CreateStoryboardVersionRequest) (StoryboardVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardVersionDTO{}, bindingUnavailable()
	}
	record, err := service.CreateStoryboardVersion(b.context(), appstoryboard.CreateStoryboardVersionRequest{
		StoryboardID:          request.StoryboardID,
		ScriptVersionID:       request.ScriptVersionID,
		DirectorPlanVersionID: request.DirectorPlanVersionID,
		BasedOnVersionID:      request.BasedOnVersionID,
		SourceAgentRunID:      request.SourceAgentRunID,
		CreatedByType:         versioning.CreatedByType(request.CreatedByType),
		CreatedByID:           request.CreatedByID,
		ChangeReason:          request.ChangeReason,
		LegacyMetadata:        request.LegacyMetadata,
	})
	if err != nil {
		return StoryboardVersionDTO{}, toDramaError(err)
	}
	return toStoryboardVersionDTO(record), nil
}

// CreateStoryboardItemRequest carries one shot's row in a storyboard version.
type CreateStoryboardItemRequest struct {
	StoryboardVersionID  string `json:"storyboardVersionId"`
	ShotID               string `json:"shotId"`
	Ordinal              int    `json:"ordinal"`
	ShotSize             string `json:"shotSize,omitempty"`
	CameraAngle          string `json:"cameraAngle,omitempty"`
	CameraMovement       string `json:"cameraMovement,omitempty"`
	DurationSeconds      int    `json:"durationSeconds,omitempty"`
	VisualDescription    string `json:"visualDescription,omitempty"`
	ActionDescription    string `json:"actionDescription,omitempty"`
	DialogueAudioSummary string `json:"dialogueAudioSummary,omitempty"`
	ContinuityNotes      string `json:"continuityNotes,omitempty"`
}

// CreateStoryboardItem adds a shot's row to a storyboard version.
func (b *DramaBinding) CreateStoryboardItem(request CreateStoryboardItemRequest) (StoryboardItemDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardItemDTO{}, bindingUnavailable()
	}
	record, err := service.CreateStoryboardItem(b.context(), appstoryboard.CreateStoryboardItemRequest{
		StoryboardVersionID:  request.StoryboardVersionID,
		ShotID:               request.ShotID,
		Ordinal:              request.Ordinal,
		ShotSize:             request.ShotSize,
		CameraAngle:          request.CameraAngle,
		CameraMovement:       request.CameraMovement,
		DurationSeconds:      request.DurationSeconds,
		VisualDescription:    request.VisualDescription,
		ActionDescription:    request.ActionDescription,
		DialogueAudioSummary: request.DialogueAudioSummary,
		ContinuityNotes:      request.ContinuityNotes,
	})
	if err != nil {
		return StoryboardItemDTO{}, toDramaError(err)
	}
	return toStoryboardItemDTO(record), nil
}

// ListStoryboardItems returns a version's items in ordinal order.
func (b *DramaBinding) ListStoryboardItems(storyboardVersionID string) ([]StoryboardItemDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryboardItems(b.context(), storyboardVersionID)
	if err != nil {
		return nil, toDramaError(err)
	}
	items := make([]StoryboardItemDTO, 0, len(records))
	for _, record := range records {
		items = append(items, toStoryboardItemDTO(record))
	}
	return items, nil
}

// CreatePanelVersionRequest carries a new panel version for a storyboard item.
type CreatePanelVersionRequest struct {
	StoryboardItemID    string `json:"storyboardItemId"`
	BasedOnVersionID    string `json:"basedOnVersionId,omitempty"`
	VisualPrompt        string `json:"visualPrompt,omitempty"`
	NegativePrompt      string `json:"negativePrompt,omitempty"`
	ReferencePolicyJSON string `json:"referencePolicyJson,omitempty"`
	SourceAgentRunID    string `json:"sourceAgentRunId,omitempty"`
	CreatedByType       string `json:"createdByType,omitempty"`
	CreatedByID         string `json:"createdById,omitempty"`
	ChangeReason        string `json:"changeReason,omitempty"`
	LegacyMetadata      string `json:"legacyMetadata,omitempty"`
}

// CreatePanelVersion appends a panel version to a storyboard item.
func (b *DramaBinding) CreatePanelVersion(request CreatePanelVersionRequest) (StoryboardPanelVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardPanelVersionDTO{}, bindingUnavailable()
	}
	record, err := service.CreatePanelVersion(b.context(), appstoryboard.CreatePanelVersionRequest{
		StoryboardItemID:    request.StoryboardItemID,
		BasedOnVersionID:    request.BasedOnVersionID,
		VisualPrompt:        request.VisualPrompt,
		NegativePrompt:      request.NegativePrompt,
		ReferencePolicyJSON: request.ReferencePolicyJSON,
		SourceAgentRunID:    request.SourceAgentRunID,
		CreatedByType:       versioning.CreatedByType(request.CreatedByType),
		CreatedByID:         request.CreatedByID,
		ChangeReason:        request.ChangeReason,
		LegacyMetadata:      request.LegacyMetadata,
	})
	if err != nil {
		return StoryboardPanelVersionDTO{}, toDramaError(err)
	}
	return toStoryboardPanelVersionDTO(record), nil
}

// ListPanels returns an item's panel versions in version order.
func (b *DramaBinding) ListPanels(storyboardItemID string) ([]StoryboardPanelVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListPanels(b.context(), storyboardItemID)
	if err != nil {
		return nil, toDramaError(err)
	}
	panels := make([]StoryboardPanelVersionDTO, 0, len(records))
	for _, record := range records {
		panels = append(panels, toStoryboardPanelVersionDTO(record))
	}
	return panels, nil
}

// ApprovePanelImageRequest approves one candidate image as a panel's canonical
// one.
type ApprovePanelImageRequest struct {
	PanelVersionID              string `json:"panelVersionId"`
	ApprovedImageAssetVersionID string `json:"approvedImageAssetVersionId"`
	// CandidateVersionIDs are the asset versions this panel generated or that the
	// user associated with it. Candidacy is a property of the panel's job
	// history, so it is supplied per call.
	CandidateVersionIDs []string `json:"candidateVersionIds,omitempty"`
	// ExpectedRevision is the revision of the panel's parent storyboard item, the
	// row the approval is recorded against.
	ExpectedRevision int64 `json:"expectedRevision"`
}

// ListDirectorPlanVersions returns an episode's plan versions, newest first.
//
// A user looking at a plan needs its history to see what a revision changed, and reading
// each version by an id they do not hold is a query nobody can make. This is that read.
func (b *DramaBinding) ListDirectorPlanVersions(episodeID string) ([]DirectorPlanVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListDirectorPlanVersions(b.context(), episodeID)
	if err != nil {
		return nil, toDramaError(err)
	}
	versions := make([]DirectorPlanVersionDTO, 0, len(records))
	for _, record := range records {
		versions = append(versions, toDirectorPlanVersionDTO(record))
	}
	return versions, nil
}

// ListStoryboardVersions returns a storyboard's versions, newest first.
func (b *DramaBinding) ListStoryboardVersions(storyboardID string) ([]StoryboardVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryboardVersions(b.context(), storyboardID)
	if err != nil {
		return nil, toDramaError(err)
	}
	versions := make([]StoryboardVersionDTO, 0, len(records))
	for _, record := range records {
		versions = append(versions, toStoryboardVersionDTO(record))
	}
	return versions, nil
}

// GetStoryboardVersion returns one storyboard version.
func (b *DramaBinding) GetStoryboardVersion(id string) (StoryboardVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardVersionDTO{}, bindingUnavailable()
	}
	record, err := service.GetStoryboardVersion(b.context(), id)
	if err != nil {
		return StoryboardVersionDTO{}, toDramaError(err)
	}
	return toStoryboardVersionDTO(record), nil
}

// GetDirectorPlanVersion returns one plan version.
func (b *DramaBinding) GetDirectorPlanVersion(id string) (DirectorPlanVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return DirectorPlanVersionDTO{}, bindingUnavailable()
	}
	record, err := service.GetDirectorPlanVersion(b.context(), id)
	if err != nil {
		return DirectorPlanVersionDTO{}, toDramaError(err)
	}
	return toDirectorPlanVersionDTO(record), nil
}

// ApprovedStoryboardVersionID returns the version in force for a storyboard, or empty.
//
// Empty is the ordinary answer for a board nobody has approved, and it is reported rather
// than as an error: a caller asking "what is approved" about an unapproved board has asked
// a well-formed question.
func (b *DramaBinding) ApprovedStoryboardVersionID(storyboardID string) (string, error) {
	service := b.storyboardService()
	if service == nil {
		return "", bindingUnavailable()
	}
	versionID, err := service.ApprovedStoryboardVersionID(b.context(), storyboardID)
	if err != nil {
		return "", toDramaError(err)
	}
	return versionID, nil
}

// The production pipeline's batch commands.
//
// They are declared against a small INTERFACE rather than the concrete service so the binding
// can hold a nil one: a build without a job store still runs the five agent stages, and the
// batch then refuses with a reason rather than reporting a submission that never happened.
type ProductionBatch interface {
	CheckStoryboardGate(ctx context.Context, request appproductionpipeline.GateCheckRequest) error
	RunImageBatch(ctx context.Context, request appproductionpipeline.RunImageBatchRequest) (appproductionpipeline.RunImageBatchResult, error)
	CollectBatchResults(ctx context.Context, request appproductionpipeline.CollectBatchResultsRequest) ([]appproductionpipeline.CollectedCandidate, error)
	ApproveCandidate(ctx context.Context, request appproductionpipeline.ApproveCandidateRequest) (storyboard.StoryboardPanelVersion, error)
}

// AttachProductionBatch supplies the batch. A nil one leaves those commands failing closed.
func AttachProductionBatch(binding *DramaBinding, ctx context.Context, batch ProductionBatch) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.batch = batch
	binding.mu.Unlock()
}

func (b *DramaBinding) productionBatch() ProductionBatch {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.batch
}

// CheckStoryboardGateRequest asks whether an episode's board may be batched.
type CheckStoryboardGateRequest struct {
	EpisodeID           string `json:"episodeId"`
	StoryboardVersionID string `json:"storyboardVersionId,omitempty"`
	StoryboardID        string `json:"storyboardId,omitempty"`
}

// CheckStoryboardGate reports whether the batch gate would pass, and why not when it would
// not.
//
// It is the READ a UI makes before offering the button: AC-BOARD-001's "未通过时阻止批量生成" is
// a refusal the user should see as a REASON rather than as a failed click after the fact.
func (b *DramaBinding) CheckStoryboardGate(request CheckStoryboardGateRequest) error {
	batch := b.productionBatch()
	if batch == nil {
		return bindingUnavailable()
	}
	if err := batch.CheckStoryboardGate(b.context(), appproductionpipeline.GateCheckRequest{
		EpisodeID:           request.EpisodeID,
		StoryboardVersionID: request.StoryboardVersionID,
		StoryboardID:        request.StoryboardID,
	}); err != nil {
		return toDramaError(err)
	}
	return nil
}

// RunImageBatchRequest asks for a batch of candidate images.
type RunImageBatchRequest struct {
	StoryboardVersionID string `json:"storyboardVersionId"`
	EpisodeID           string `json:"episodeId"`
	ProjectID           string `json:"projectId"`
	// PerShotCandidates is how many candidates each shot gets.
	PerShotCandidates int `json:"perShotCandidates"`
	// ShotIDs narrows the batch to those shots. Empty means every shot in the board.
	ShotIDs []string `json:"shotIds,omitempty"`
	// ProviderID and ModelName are what the jobs are submitted to. They come from the caller:
	// section 6.1 puts provider selection outside the agent's reach.
	ProviderID string `json:"providerId"`
	ModelName  string `json:"modelName"`
	// PromptSuffix is appended to every derived prompt.
	PromptSuffix string `json:"promptSuffix,omitempty"`
	Seed         string `json:"seed,omitempty"`
}

// BatchSubmissionDTO is one job the batch submitted, or found already submitted.
type BatchSubmissionDTO struct {
	ShotID         string `json:"shotId"`
	ItemID         string `json:"itemId"`
	CandidateIndex int    `json:"candidateIndex"`
	JobID          string `json:"jobId"`
	Duplicate      bool   `json:"duplicate"`
}

// RunImageBatchResultDTO reports what a batch did.
type RunImageBatchResultDTO struct {
	Submissions []BatchSubmissionDTO `json:"submissions"`
	Duplicate   int                  `json:"duplicate"`
}

// RunImageBatch submits one job per candidate per shot.
func (b *DramaBinding) RunImageBatch(request RunImageBatchRequest) (RunImageBatchResultDTO, error) {
	batch := b.productionBatch()
	if batch == nil {
		return RunImageBatchResultDTO{}, bindingUnavailable()
	}
	result, err := batch.RunImageBatch(b.context(), appproductionpipeline.RunImageBatchRequest{
		StoryboardVersionID: request.StoryboardVersionID,
		EpisodeID:           request.EpisodeID,
		ProjectID:           request.ProjectID,
		PerShotCandidates:   request.PerShotCandidates,
		ShotIDs:             request.ShotIDs,
		ProviderID:          request.ProviderID,
		ModelName:           request.ModelName,
		PromptSuffix:        request.PromptSuffix,
		Seed:                request.Seed,
	})
	if err != nil {
		return RunImageBatchResultDTO{}, toDramaError(err)
	}
	out := RunImageBatchResultDTO{
		Submissions: make([]BatchSubmissionDTO, 0, len(result.Submitted)),
		Duplicate:   result.Duplicate,
	}
	for _, submission := range result.Submitted {
		out.Submissions = append(out.Submissions, BatchSubmissionDTO{
			ShotID: submission.ShotID, ItemID: submission.ItemID,
			CandidateIndex: submission.CandidateIndex, JobID: submission.JobID,
			Duplicate: submission.Duplicate,
		})
	}
	return out, nil
}

// CollectBatchResultsRequest asks for a batch's finished candidates.
type CollectBatchResultsRequest struct {
	// AssetByItem maps a storyboard item's id to the asset its candidates belong to.
	AssetByItem map[string]string `json:"assetByItem"`
	JobIDs      []string          `json:"jobIds"`
	UsageRole   string            `json:"usageRole,omitempty"`
}

// CollectedCandidateDTO is one finished job that became a candidate version.
type CollectedCandidateDTO struct {
	JobID         string `json:"jobId"`
	ItemID        string `json:"itemId"`
	AssetID       string `json:"assetId"`
	VersionID     string `json:"versionId,omitempty"`
	VersionNumber int    `json:"versionNumber,omitempty"`
	Duplicate     bool   `json:"duplicate"`
}

// CollectBatchResults turns each succeeded job's result into a candidate asset version.
func (b *DramaBinding) CollectBatchResults(request CollectBatchResultsRequest) ([]CollectedCandidateDTO, error) {
	batch := b.productionBatch()
	if batch == nil {
		return nil, bindingUnavailable()
	}
	collected, err := batch.CollectBatchResults(b.context(), appproductionpipeline.CollectBatchResultsRequest{
		AssetByItem: request.AssetByItem,
		JobIDs:      request.JobIDs,
		UsageRole:   request.UsageRole,
	})
	if err != nil {
		return nil, toDramaError(err)
	}
	out := make([]CollectedCandidateDTO, 0, len(collected))
	for _, candidate := range collected {
		out = append(out, CollectedCandidateDTO{
			JobID: candidate.JobID, ItemID: candidate.ItemID, AssetID: candidate.AssetID,
			VersionID: candidate.VersionID, VersionNumber: candidate.VersionNumber,
			Duplicate: candidate.Duplicate,
		})
	}
	return out, nil
}

// ApproveCandidateRequest approves one candidate as a panel's image.
type ApproveCandidateRequest struct {
	PanelVersionID              string   `json:"panelVersionId"`
	ApprovedImageAssetVersionID string   `json:"approvedImageAssetVersionId"`
	CandidateVersionIDs         []string `json:"candidateVersionIds"`
	// ExpectedRevision is the revision of the panel's parent storyboard item.
	ExpectedRevision int64 `json:"expectedRevision"`
}

// ApproveCandidate makes one candidate a panel's approved image.
//
// Section 9.5's rule — the approved image must be one of the panel's candidates — is enforced
// by the storyboard service this delegates to, not restated here.
func (b *DramaBinding) ApproveCandidate(request ApproveCandidateRequest) (StoryboardPanelVersionDTO, error) {
	batch := b.productionBatch()
	if batch == nil {
		return StoryboardPanelVersionDTO{}, bindingUnavailable()
	}
	record, err := batch.ApproveCandidate(b.context(), appproductionpipeline.ApproveCandidateRequest{
		PanelVersionID:              request.PanelVersionID,
		ApprovedImageAssetVersionID: request.ApprovedImageAssetVersionID,
		CandidateVersionIDs:         request.CandidateVersionIDs,
		ExpectedRevision:            request.ExpectedRevision,
	})
	if err != nil {
		return StoryboardPanelVersionDTO{}, toDramaError(err)
	}
	return toStoryboardPanelVersionDTO(record), nil
}

// SetShotOverridesRequest writes a plan's per-shot overrides document.
type SetShotOverridesRequest struct {
	VersionID string `json:"versionId"`
	// OverridesJSON replaces the stored document rather than merging into it.
	OverridesJSON string `json:"overridesJson"`
}

// SetShotOverrides writes the per-shot overrides of one plan version.
//
// It is how a previs camera reaches the shot it belongs to: the studio reports the camera,
// this binding composes the document, and this command stores it. FR-060's "保存后可在 Shot 中
// 看到摄像机参数" is the round trip this closes.
func (b *DramaBinding) SetShotOverrides(request SetShotOverridesRequest) (DirectorPlanVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return DirectorPlanVersionDTO{}, bindingUnavailable()
	}
	record, err := service.SetShotOverrides(b.context(), appstoryboard.SetShotOverridesRequest{
		VersionID:     request.VersionID,
		OverridesJSON: request.OverridesJSON,
	})
	if err != nil {
		return DirectorPlanVersionDTO{}, toDramaError(err)
	}
	return toDirectorPlanVersionDTO(record), nil
}

// ApproveStoryboardVersion switches which board version is in force.
//
// It is the gate AC-BOARD-001's batch reads: generating images for a board that is not
// approved would spend the user's provider budget on rows a FIX may replace.
func (b *DramaBinding) ApproveStoryboardVersion(request ApproveStoryboardVersionRequest) (StoryboardVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardVersionDTO{}, bindingUnavailable()
	}
	record, err := service.ApproveStoryboardVersion(b.context(), appstoryboard.ApproveStoryboardVersionRequest{
		VersionID: request.VersionID,
		TraceID:   request.TraceID,
	})
	if err != nil {
		return StoryboardVersionDTO{}, toDramaError(err)
	}
	return toStoryboardVersionDTO(record), nil
}

// ApproveDirectorPlanVersion switches which plan version is in force.
func (b *DramaBinding) ApproveDirectorPlanVersion(request ApproveDirectorPlanVersionRequest) (DirectorPlanVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return DirectorPlanVersionDTO{}, bindingUnavailable()
	}
	record, err := service.ApproveDirectorPlanVersion(b.context(), appstoryboard.ApproveDirectorPlanVersionRequest{
		VersionID: request.VersionID,
		TraceID:   request.TraceID,
	})
	if err != nil {
		return DirectorPlanVersionDTO{}, toDramaError(err)
	}
	return toDirectorPlanVersionDTO(record), nil
}

// UpdateStoryboardItem changes ONE row of a board, and nothing else.
//
// AC-BOARD-002's requirement is that a FIX to one shot leaves the others unchanged, and the
// strongest form of that is this command's shape: the other rows are never written at all.
// A version-rewrite path would have to copy them, and "unchanged" would then rest on a copy
// being faithful rather than on the row not being touched.
//
// Every field is POINTER so a nil means "leave this alone" — a caller changing a costume
// must not have to echo the descriptions it did not read.
func (b *DramaBinding) UpdateStoryboardItem(request UpdateStoryboardItemRequest) (StoryboardItemDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardItemDTO{}, bindingUnavailable()
	}
	record, err := service.UpdateStoryboardItem(b.context(), appstoryboard.UpdateStoryboardItemRequest{
		ItemID:                 request.ItemID,
		ExpectedRevision:       request.ExpectedRevision,
		ShotSize:               request.ShotSize,
		CameraAngle:            request.CameraAngle,
		CameraMovement:         request.CameraMovement,
		DurationSeconds:        request.DurationSeconds,
		VisualDescription:      request.VisualDescription,
		ActionDescription:      request.ActionDescription,
		DialogueAudioSummary:   request.DialogueAudioSummary,
		ContinuityNotes:        request.ContinuityNotes,
		FirstFrameDescription:  request.FirstFrameDescription,
		LastFrameDescription:   request.LastFrameDescription,
		VideoMotionDescription: request.VideoMotionDescription,
		// The status arrives as a string because that is what a transport view carries; the
		// domain's own type is applied here, and an unrecognised value is refused by the
		// service's validation rather than being coerced into one.
		Status: storyboardStatusPointer(request.Status),
	})
	if err != nil {
		return StoryboardItemDTO{}, toDramaError(err)
	}
	return toStoryboardItemDTO(record), nil
}

// ApproveStoryboardVersionRequest approves a board version.
type ApproveStoryboardVersionRequest struct {
	VersionID string `json:"versionId"`
	// TraceID is the audit identifier the governance event carries.
	TraceID string `json:"traceId,omitempty"`
}

// ApproveDirectorPlanVersionRequest approves a plan version.
type ApproveDirectorPlanVersionRequest struct {
	VersionID string `json:"versionId"`
	TraceID   string `json:"traceId,omitempty"`
}

// UpdateStoryboardItemRequest changes one row.
//
// The pointers are the contract: a field that is absent is one the caller does not want
// changed, which is what lets a single-shot redo be a change to one shot.
type UpdateStoryboardItemRequest struct {
	ItemID string `json:"itemId"`
	// ExpectedRevision is the revision the caller read, so a stale edit is refused rather
	// than overwriting a change another window made.
	ExpectedRevision       int64   `json:"expectedRevision"`
	ShotSize               *string `json:"shotSize,omitempty"`
	CameraAngle            *string `json:"cameraAngle,omitempty"`
	CameraMovement         *string `json:"cameraMovement,omitempty"`
	DurationSeconds        *int    `json:"durationSeconds,omitempty"`
	VisualDescription      *string `json:"visualDescription,omitempty"`
	ActionDescription      *string `json:"actionDescription,omitempty"`
	DialogueAudioSummary   *string `json:"dialogueAudioSummary,omitempty"`
	ContinuityNotes        *string `json:"continuityNotes,omitempty"`
	FirstFrameDescription  *string `json:"firstFrameDescription,omitempty"`
	LastFrameDescription   *string `json:"lastFrameDescription,omitempty"`
	VideoMotionDescription *string `json:"videoMotionDescription,omitempty"`
	Status                 *string `json:"status,omitempty"`
}

// ListPanelVersions returns a storyboard row's panel versions.
func (b *DramaBinding) ListPanelVersions(storyboardItemID string) ([]StoryboardPanelVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListPanels(b.context(), storyboardItemID)
	if err != nil {
		return nil, toDramaError(err)
	}
	panels := make([]StoryboardPanelVersionDTO, 0, len(records))
	for _, record := range records {
		panels = append(panels, toStoryboardPanelVersionDTO(record))
	}
	return panels, nil
}

// ApprovePanelImage records a panel's approved image. Section 9.5 requires the
// image to be one of the candidates the caller supplied, which the service
// checks.
func (b *DramaBinding) ApprovePanelImage(request ApprovePanelImageRequest) (StoryboardPanelVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardPanelVersionDTO{}, bindingUnavailable()
	}
	if len(request.CandidateVersionIDs) > maxBatchPanelCandidates {
		return StoryboardPanelVersionDTO{}, bindingInvalidInput()
	}
	record, err := service.ApprovePanelImage(b.context(), appstoryboard.ApprovePanelImageRequest{
		PanelVersionID:              request.PanelVersionID,
		ApprovedImageAssetVersionID: request.ApprovedImageAssetVersionID,
		CandidateVersionIDs:         request.CandidateVersionIDs,
		ExpectedRevision:            request.ExpectedRevision,
	})
	if err != nil {
		return StoryboardPanelVersionDTO{}, toDramaError(err)
	}
	return toStoryboardPanelVersionDTO(record), nil
}

// ---------------------------------------------------------------------------
// Workflow: runs, stage attempts, reviews and gate decisions
// ---------------------------------------------------------------------------

// WorkflowRunDTO is the transport view of one durable workflow run.
type WorkflowRunDTO struct {
	ID           string `json:"id"`
	ProjectID    string `json:"projectId"`
	EpisodeID    string `json:"episodeId,omitempty"`
	WorkflowType string `json:"workflowType"`
	// CurrentStage is empty before the first stage starts and after the last one
	// finishes.
	CurrentStage     string `json:"currentStage,omitempty"`
	Status           string `json:"status"`
	ActiveStageRunID string `json:"activeStageRunId,omitempty"`
	// ConfigurationJSON is the run's configuration document.
	ConfigurationJSON string `json:"configurationJson,omitempty"`
	RetryCount        int    `json:"retryCount"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
	// CompletedAt is empty while the run is not terminal.
	CompletedAt string `json:"completedAt,omitempty"`
	Revision    int64  `json:"revision"`
}

// StageRunDTO is the transport view of one attempt at one stage.
type StageRunDTO struct {
	ID            string `json:"id"`
	WorkflowRunID string `json:"workflowRunId"`
	Stage         string `json:"stage"`
	Attempt       int    `json:"attempt"`
	// ExecutionAgentKey names the agent a stage ran under; it is empty for a
	// deterministic stage.
	ExecutionAgentKey string `json:"executionAgentKey,omitempty"`
	Status            string `json:"status"`
	InputJSON         string `json:"inputJson,omitempty"`
	// ValidatedOutputJSON is the verified structured output.
	ValidatedOutputJSON string `json:"validatedOutputJson,omitempty"`
	// RawOutputFileID is the content hash of the stored raw output, never a path.
	RawOutputFileID string `json:"rawOutputFileId,omitempty"`
	ErrorCode       string `json:"errorCode,omitempty"`
	// ErrorMessage is a safe message, not a provider response.
	ErrorMessage string `json:"errorMessage,omitempty"`
	CreatedAt    string `json:"createdAt"`
	// StartedAt and FinishedAt are empty until the attempt reaches those points.
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
	Revision   int64  `json:"revision"`
}

// ReviewIssueDTO is one finding a review report carries.
type ReviewIssueDTO struct {
	ID             string `json:"id"`
	ReviewReportID string `json:"reviewReportId"`
	Rule           string `json:"rule,omitempty"`
	Severity       string `json:"severity"`
	// EntityType and EntityID are the entity the UI can jump to; either both are
	// set or Location names where the problem is.
	EntityType   string `json:"entityType,omitempty"`
	EntityID     string `json:"entityId,omitempty"`
	Location     string `json:"location,omitempty"`
	Field        string `json:"field,omitempty"`
	Problem      string `json:"problem,omitempty"`
	Suggestion   string `json:"suggestion,omitempty"`
	EvidenceJSON string `json:"evidenceJson,omitempty"`
	AutoFixable  bool   `json:"autoFixable"`
	// Source marks which half of the review found this: a deterministic code rule, or a supervisor.
	// AGENT_CONTRACTS section 11.4 requires the mark, and the quality centre groups by it: a reader
	// deciding what to do about a finding needs to know whether it is a computation or a reading.
	Source     string `json:"source"`
	Status     string `json:"status"`
	ResolvedBy string `json:"resolvedBy,omitempty"`
	ResolvedAt string `json:"resolvedAt,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

// ReviewReportDTO is the transport view of one review of a stage attempt,
// with the findings the report is about.
type ReviewReportDTO struct {
	ID             string `json:"id"`
	StageRunID     string `json:"stageRunId"`
	SupervisorKey  string `json:"supervisorKey,omitempty"`
	RulesetVersion string `json:"rulesetVersion,omitempty"`
	// Score is absent when the reviewer produced no score; a zero score is a real
	// result, so the pointer keeps the two apart.
	Score             *float64 `json:"score,omitempty"`
	Grade             string   `json:"grade,omitempty"`
	Passed            bool     `json:"passed"`
	Severity          string   `json:"severity"`
	RecommendedAction string   `json:"recommendedAction,omitempty"`
	Summary           string   `json:"summary,omitempty"`
	CreatedAt         string   `json:"createdAt"`
	// Issues is always a list, so the UI needs no nil guard.
	Issues []ReviewIssueDTO `json:"issues"`
}

// UserGateDecisionDTO is the transport view of one user decision at a gate.
type UserGateDecisionDTO struct {
	ID            string `json:"id"`
	WorkflowRunID string `json:"workflowRunId"`
	StageRunID    string `json:"stageRunId,omitempty"`
	Decision      string `json:"decision"`
	IssueIDsJSON  string `json:"issueIdsJson,omitempty"`
	Instruction   string `json:"instruction,omitempty"`
	Reason        string `json:"reason,omitempty"`
	// LockedEntityRefsJSON carries the references a waiver locks.
	LockedEntityRefsJSON string `json:"lockedEntityRefsJson,omitempty"`
	CreatedByType        string `json:"createdByType"`
	CreatedByID          string `json:"createdById,omitempty"`
	CreatedAt            string `json:"createdAt"`
}

// WorkflowEventDTO is the transport view of one audit record.
type WorkflowEventDTO struct {
	ID            string `json:"id"`
	WorkflowRunID string `json:"workflowRunId"`
	StageRunID    string `json:"stageRunId,omitempty"`
	EventType     string `json:"eventType"`
	FromStatus    string `json:"fromStatus,omitempty"`
	ToStatus      string `json:"toStatus,omitempty"`
	PayloadJSON   string `json:"payloadJson,omitempty"`
	ActorType     string `json:"actorType"`
	ActorID       string `json:"actorId,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// CreateWorkflowRunRequest starts a workflow run.
type CreateWorkflowRunRequest struct {
	ProjectID    string `json:"projectId"`
	EpisodeID    string `json:"episodeId,omitempty"`
	WorkflowType string `json:"workflowType"`
	// ConfigurationJSON is the run's configuration document.
	ConfigurationJSON string `json:"configurationJson,omitempty"`
	// ActorType is "user" (the default for an empty value), "agent", "migration"
	// or "system".
	ActorType string `json:"actorType,omitempty"`
	ActorID   string `json:"actorId,omitempty"`
}

// CreateWorkflowRun stores a run in the pending state together with the audit
// event recording its creation.
func (b *DramaBinding) CreateWorkflowRun(request CreateWorkflowRunRequest) (WorkflowRunDTO, error) {
	service := b.workflowService()
	if service == nil {
		return WorkflowRunDTO{}, bindingUnavailable()
	}
	record, err := service.CreateRun(b.context(), appworkflow.CreateRunRequest{
		ProjectID:         request.ProjectID,
		EpisodeID:         request.EpisodeID,
		WorkflowType:      request.WorkflowType,
		ConfigurationJSON: request.ConfigurationJSON,
		Actor: appworkflow.Actor{
			Type: versioning.CreatedByType(request.ActorType),
			ID:   request.ActorID,
		},
	})
	if err != nil {
		return WorkflowRunDTO{}, toDramaError(err)
	}
	return toWorkflowRunDTO(record), nil
}

// ListWorkflowRuns returns a project's runs newest first.
func (b *DramaBinding) ListWorkflowRuns(projectID string) ([]WorkflowRunDTO, error) {
	service := b.workflowService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListRuns(b.context(), projectID)
	if err != nil {
		return nil, toDramaError(err)
	}
	runs := make([]WorkflowRunDTO, 0, len(records))
	for _, record := range records {
		runs = append(runs, toWorkflowRunDTO(record))
	}
	return runs, nil
}

// TransitionWorkflowRunRequest moves a run to another status.
type TransitionWorkflowRunRequest struct {
	RunID string `json:"runId"`
	// Status is one of the run statuses the domain machine admits from the
	// stored one.
	Status    string `json:"status"`
	Revision  int64  `json:"revision"`
	ActorType string `json:"actorType,omitempty"`
	ActorID   string `json:"actorId,omitempty"`
}

// TransitionWorkflowRun moves a run and records the transition as an audit
// event. The edge is decided against the stored status, never the caller's.
func (b *DramaBinding) TransitionWorkflowRun(request TransitionWorkflowRunRequest) (WorkflowRunDTO, error) {
	service := b.workflowService()
	if service == nil {
		return WorkflowRunDTO{}, bindingUnavailable()
	}
	record, err := service.TransitionRun(b.context(), appworkflow.TransitionRunRequest{
		RunID:    request.RunID,
		Status:   workflow.RunStatus(request.Status),
		Revision: request.Revision,
		Actor: appworkflow.Actor{
			Type: versioning.CreatedByType(request.ActorType),
			ID:   request.ActorID,
		},
	})
	if err != nil {
		return WorkflowRunDTO{}, toDramaError(err)
	}
	return toWorkflowRunDTO(record), nil
}

// CreateStageRunRequest adds a stage attempt to a run.
type CreateStageRunRequest struct {
	WorkflowRunID string `json:"workflowRunId"`
	// Stage is a stage key of at most 120 characters. The value set is deliberately
	// open; DocumentedStageNames is the reference list, not a constraint.
	Stage        string `json:"stage"`
	Attempt      int    `json:"attempt"`
	ExecutionKey string `json:"executionKey,omitempty"`
	InputJSON    string `json:"inputJson,omitempty"`
	ActorType    string `json:"actorType,omitempty"`
	ActorID      string `json:"actorId,omitempty"`
}

// CreateStageRun stores a new stage attempt and the event recording it.
func (b *DramaBinding) CreateStageRun(request CreateStageRunRequest) (StageRunDTO, error) {
	service := b.workflowService()
	if service == nil {
		return StageRunDTO{}, bindingUnavailable()
	}
	record, err := service.CreateStage(b.context(), appworkflow.CreateStageRequest{
		WorkflowRunID: request.WorkflowRunID,
		Stage:         workflow.StageName(request.Stage),
		Attempt:       request.Attempt,
		ExecutionKey:  request.ExecutionKey,
		InputJSON:     request.InputJSON,
		Actor: appworkflow.Actor{
			Type: versioning.CreatedByType(request.ActorType),
			ID:   request.ActorID,
		},
	})
	if err != nil {
		return StageRunDTO{}, toDramaError(err)
	}
	return toStageRunDTO(record), nil
}

// ListStageRuns returns a run's attempts oldest first.
func (b *DramaBinding) ListStageRuns(workflowRunID string) ([]StageRunDTO, error) {
	service := b.workflowService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStages(b.context(), workflowRunID)
	if err != nil {
		return nil, toDramaError(err)
	}
	stages := make([]StageRunDTO, 0, len(records))
	for _, record := range records {
		stages = append(stages, toStageRunDTO(record))
	}
	return stages, nil
}

// TransitionStageRunRequest moves a stage attempt to another status.
type TransitionStageRunRequest struct {
	StageRunID string `json:"stageRunId"`
	Status     string `json:"status"`
	Revision   int64  `json:"revision"`
	ActorType  string `json:"actorType,omitempty"`
	ActorID    string `json:"actorId,omitempty"`
}

// TransitionStageRun moves an attempt and records the transition as an audit
// event, under the same rules TransitionWorkflowRun follows.
func (b *DramaBinding) TransitionStageRun(request TransitionStageRunRequest) (StageRunDTO, error) {
	service := b.workflowService()
	if service == nil {
		return StageRunDTO{}, bindingUnavailable()
	}
	record, err := service.TransitionStage(b.context(), appworkflow.TransitionStageRequest{
		StageRunID: request.StageRunID,
		Status:     workflow.StageStatus(request.Status),
		Revision:   request.Revision,
		Actor: appworkflow.Actor{
			Type: versioning.CreatedByType(request.ActorType),
			ID:   request.ActorID,
		},
	})
	if err != nil {
		return StageRunDTO{}, toDramaError(err)
	}
	return toStageRunDTO(record), nil
}

// ReviewIssueInputRequest is one finding a reviewer reported.
type ReviewIssueInputRequest struct {
	Rule       string `json:"rule,omitempty"`
	Severity   string `json:"severity"`
	EntityType string `json:"entityType,omitempty"`
	EntityID   string `json:"entityId,omitempty"`
	Location   string `json:"location,omitempty"`
	Field      string `json:"field,omitempty"`
	Problem    string `json:"problem,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	// EvidenceJSON carries the evidence the finding cites.
	EvidenceJSON string `json:"evidenceJson,omitempty"`
	AutoFixable  bool   `json:"autoFixable,omitempty"`
}

// RecordReviewRequest stores one review report and its findings.
type RecordReviewRequest struct {
	StageRunID     string `json:"stageRunId"`
	SupervisorKey  string `json:"supervisorKey,omitempty"`
	RulesetVersion string `json:"rulesetVersion,omitempty"`
	// Score is absent when the reviewer produced no score; a zero score is a real
	// result.
	Score             *float64 `json:"score,omitempty"`
	Grade             string   `json:"grade,omitempty"`
	Passed            bool     `json:"passed"`
	Severity          string   `json:"severity"`
	RecommendedAction string   `json:"recommendedAction,omitempty"`
	Summary           string   `json:"summary,omitempty"`
	// Issues are the findings the report is about, written with the report in one
	// transaction.
	Issues []ReviewIssueInputRequest `json:"issues,omitempty"`
}

// RecordReview stores a report and its findings.
func (b *DramaBinding) RecordReview(request RecordReviewRequest) (ReviewReportDTO, error) {
	service := b.workflowService()
	if service == nil {
		return ReviewReportDTO{}, bindingUnavailable()
	}
	if len(request.Issues) > maxBatchReviewIssues {
		return ReviewReportDTO{}, bindingInvalidInput()
	}
	issues := make([]appworkflow.ReviewIssueInput, 0, len(request.Issues))
	for _, input := range request.Issues {
		issues = append(issues, appworkflow.ReviewIssueInput{
			Rule:         input.Rule,
			Severity:     workflow.Severity(input.Severity),
			EntityType:   input.EntityType,
			EntityID:     input.EntityID,
			Location:     input.Location,
			Field:        input.Field,
			Problem:      input.Problem,
			Suggestion:   input.Suggestion,
			EvidenceJSON: input.EvidenceJSON,
			AutoFixable:  input.AutoFixable,
		})
	}
	report, storedIssues, err := service.RecordReview(b.context(), appworkflow.RecordReviewRequest{
		StageRunID:        request.StageRunID,
		SupervisorKey:     request.SupervisorKey,
		RulesetVersion:    request.RulesetVersion,
		Score:             request.Score,
		Grade:             workflow.Grade(request.Grade),
		Passed:            request.Passed,
		Severity:          workflow.Severity(request.Severity),
		RecommendedAction: request.RecommendedAction,
		Summary:           request.Summary,
		Issues:            issues,
	})
	if err != nil {
		return ReviewReportDTO{}, toDramaError(err)
	}
	return toReviewReportDTO(report, storedIssues), nil
}

// GetReviewReport returns a stage attempt's newest review report and its
// findings.
func (b *DramaBinding) GetReviewReport(stageRunID string) (ReviewReportDTO, error) {
	service := b.workflowService()
	if service == nil {
		return ReviewReportDTO{}, bindingUnavailable()
	}
	report, issues, err := service.GetReviewReport(b.context(), stageRunID)
	if err != nil {
		return ReviewReportDTO{}, toDramaError(err)
	}
	return toReviewReportDTO(report, issues), nil
}

// SubmitGateDecisionRequest records what the user chose at a quality gate.
type SubmitGateDecisionRequest struct {
	WorkflowRunID string `json:"workflowRunId"`
	StageRunID    string `json:"stageRunId,omitempty"`
	// Decision is "approve", "fix", "redo", "manual_edit", "skip", "cancel" or
	// "waive".
	Decision     string `json:"decision"`
	IssueIDsJSON string `json:"issueIdsJson,omitempty"`
	Instruction  string `json:"instruction,omitempty"`
	// Reason is required for "skip" (PRD FR-100) and for "waive" (section 15.3).
	Reason               string `json:"reason,omitempty"`
	LockedEntityRefsJSON string `json:"lockedEntityRefsJson,omitempty"`
	// CreatedByType must not be "agent": section 11.5 makes an agent-authored
	// decision invalid, and the domain refuses one.
	CreatedByType string `json:"createdByType,omitempty"`
	CreatedByID   string `json:"createdById,omitempty"`
}

// SubmitGateDecision stores a user's decision at a quality gate. A refused
// decision writes nothing.
func (b *DramaBinding) SubmitGateDecision(request SubmitGateDecisionRequest) (UserGateDecisionDTO, error) {
	service := b.workflowService()
	if service == nil {
		return UserGateDecisionDTO{}, bindingUnavailable()
	}
	record, err := service.SubmitGateDecision(b.context(), appworkflow.SubmitGateDecisionRequest{
		WorkflowRunID:        request.WorkflowRunID,
		StageRunID:           request.StageRunID,
		Decision:             workflow.GateDecision(request.Decision),
		IssueIDsJSON:         request.IssueIDsJSON,
		Instruction:          request.Instruction,
		Reason:               request.Reason,
		LockedEntityRefsJSON: request.LockedEntityRefsJSON,
		CreatedBy:            versioning.CreatedByType(request.CreatedByType),
		CreatedByID:          request.CreatedByID,
	})
	if err != nil {
		return UserGateDecisionDTO{}, toDramaError(err)
	}
	return toUserGateDecisionDTO(record), nil
}

// ListWorkflowEvents returns a run's audit events oldest first.
func (b *DramaBinding) ListWorkflowEvents(workflowRunID string) ([]WorkflowEventDTO, error) {
	service := b.workflowService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListEvents(b.context(), workflowRunID)
	if err != nil {
		return nil, toDramaError(err)
	}
	events := make([]WorkflowEventDTO, 0, len(records))
	for _, record := range records {
		events = append(events, toWorkflowEventDTO(record))
	}
	return events, nil
}

// ---------------------------------------------------------------------------
// Staleness: marks on artifacts whose input changed
// ---------------------------------------------------------------------------

// StaleMarkDTO is the transport view of one staleness record.
type StaleMarkDTO struct {
	ArtifactType string `json:"artifactType"`
	ArtifactID   string `json:"artifactId"`
	ProjectID    string `json:"projectId"`
	// Severity is "breaking", "review_required" or "informational".
	Severity string `json:"severity"`
	Reason   string `json:"reason,omitempty"`
	// UpstreamType and UpstreamID name the artifact whose change caused this; both
	// are empty for a mark recorded by a manual recompute.
	UpstreamType string `json:"upstreamType,omitempty"`
	UpstreamID   string `json:"upstreamId,omitempty"`
	// Waived records that a user chose to keep the artifact despite the mark.
	Waived             bool   `json:"waived"`
	WaivedByDecisionID string `json:"waivedByDecisionId,omitempty"`
	WaivedReason       string `json:"waivedReason,omitempty"`
	// ClearedAt is empty while the mark still applies.
	ClearedAt string `json:"clearedAt,omitempty"`
}

// MarkStaleRequest records that an artifact's input changed.
type MarkStaleRequest struct {
	// ArtifactType is one of the section 15.2 chain nodes, such as "chapter" or
	// "script_version".
	ArtifactType string `json:"artifactType"`
	ArtifactID   string `json:"artifactId"`
	ProjectID    string `json:"projectId"`
	Severity     string `json:"severity"`
	Reason       string `json:"reason,omitempty"`
	// UpstreamType and UpstreamID may be empty when the mark comes from a manual
	// recompute rather than from one upstream row.
	UpstreamType string `json:"upstreamType,omitempty"`
	UpstreamID   string `json:"upstreamId,omitempty"`
}

// MarkStale records one stale mark. The table is keyed by
// (artifact_type, artifact_id), so re-marking replaces the earlier mark rather
// than adding a second row.
func (b *DramaBinding) MarkStale(request MarkStaleRequest) (StaleMarkDTO, error) {
	service := b.stalenessService()
	if service == nil {
		return StaleMarkDTO{}, bindingUnavailable()
	}
	record, err := service.MarkStale(b.context(), appstaleness.MarkStaleRequest{
		ArtifactType: staleness.ArtifactType(request.ArtifactType),
		ArtifactID:   request.ArtifactID,
		ProjectID:    request.ProjectID,
		Severity:     staleness.Severity(request.Severity),
		Reason:       request.Reason,
		UpstreamType: staleness.ArtifactType(request.UpstreamType),
		UpstreamID:   request.UpstreamID,
	})
	if err != nil {
		return StaleMarkDTO{}, toDramaError(err)
	}
	return toStaleMarkDTO(record), nil
}

// ClearStaleMarkRequest clears one artifact's mark.
type ClearStaleMarkRequest struct {
	ArtifactType string `json:"artifactType"`
	ArtifactID   string `json:"artifactId"`
}

// ClearStaleMark records that a mark no longer applies.
//
// The stored row is stamped rather than deleted, because migration 000012 keeps
// it so the history of what was invalidated survives. The boolean reports that
// the clear was applied: the service refuses an artifact that holds no mark with
// its invalid-input error rather than reporting a silent success, so a returned
// true means a mark was stamped.
func (b *DramaBinding) ClearStaleMark(request ClearStaleMarkRequest) (bool, error) {
	service := b.stalenessService()
	if service == nil {
		return false, bindingUnavailable()
	}
	if err := service.ClearMark(b.context(), appstaleness.ClearMarkRequest{
		ArtifactType: staleness.ArtifactType(request.ArtifactType),
		ArtifactID:   request.ArtifactID,
	}); err != nil {
		return false, toDramaError(err)
	}
	return true, nil
}

// WaiveStaleMarkRequest keeps a stale artifact on purpose.
type WaiveStaleMarkRequest struct {
	ArtifactType string `json:"artifactType"`
	ArtifactID   string `json:"artifactId"`
	// DecisionID names the UserGateDecision that granted the waiver; section 15.3
	// requires one.
	DecisionID string `json:"decisionId"`
	// Reason records why the artifact was kept; section 15.3 requires it too.
	Reason string `json:"reason"`
}

// WaiveStaleMark records that a user chose to keep a stale artifact. The
// boolean reports that the waiver was applied: an artifact holding no mark is
// refused with the service's invalid-input error, so a returned true means a
// waiver was recorded.
func (b *DramaBinding) WaiveStaleMark(request WaiveStaleMarkRequest) (bool, error) {
	service := b.stalenessService()
	if service == nil {
		return false, bindingUnavailable()
	}
	if _, err := service.WaiveMark(b.context(), appstaleness.WaiveMarkRequest{
		ArtifactType: staleness.ArtifactType(request.ArtifactType),
		ArtifactID:   request.ArtifactID,
		DecisionID:   request.DecisionID,
		Reason:       request.Reason,
	}); err != nil {
		return false, toDramaError(err)
	}
	return true, nil
}

// ListStaleMarks returns a project's marks, cleared and open alike.
func (b *DramaBinding) ListStaleMarks(projectID string) ([]StaleMarkDTO, error) {
	service := b.stalenessService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListMarks(b.context(), projectID)
	if err != nil {
		return nil, toDramaError(err)
	}
	return toStaleMarkDTOs(records), nil
}

// ListOpenStaleMarks returns a project's marks that are not cleared.
func (b *DramaBinding) ListOpenStaleMarks(projectID string) ([]StaleMarkDTO, error) {
	service := b.stalenessService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListOpenMarks(b.context(), projectID)
	if err != nil {
		return nil, toDramaError(err)
	}
	return toStaleMarkDTOs(records), nil
}

// ---------------------------------------------------------------------------
// Domain-to-transport conversion
// ---------------------------------------------------------------------------

// toSourceDocumentDTO converts a domain document to its transport view.
func toSourceDocumentDTO(record storydomain.SourceDocument) SourceDocumentDTO {
	return SourceDocumentDTO{
		ID: record.ID, ProjectID: record.ProjectID, Type: string(record.Type),
		Name: record.Name, CurrentVersionID: record.CurrentVersionID, Status: string(record.Status),
		DeletedAt: rfc3339OrEmpty(record.DeletedAt),
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt: record.UpdatedAt.UTC().Format(rfc3339),
		Revision:  record.Revision,
	}
}

// toSourceDocumentVersionDTO converts a domain document version. Its two file
// references are content hashes, so no location crosses the boundary.
func toSourceDocumentVersionDTO(record storydomain.SourceDocumentVersion) SourceDocumentVersionDTO {
	return SourceDocumentVersionDTO{
		ID: record.ID, SourceDocumentID: record.SourceDocumentID,
		VersionNumber: record.VersionNumber, PhysicalFileID: record.PhysicalFileID,
		NormalizedTextFileID: record.NormalizedTextFileID, ContentHash: record.ContentHash,
		MIMEType: record.MIMEType, Encoding: record.Encoding, CharCount: record.CharCount,
		ImportMetadataJSON: record.ImportMetadataJSON, CreatedByType: string(record.CreatedByType),
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toChapterDTO converts a domain chapter to its transport view.
func toChapterDTO(record storydomain.Chapter) ChapterDTO {
	return ChapterDTO{
		ID: record.ID, SourceDocumentVersionID: record.SourceDocumentVersionID,
		Ordinal: record.Ordinal, Title: record.Title,
		StartOffset: record.StartOffset, EndOffset: record.EndOffset,
		ContentHash: record.ContentHash, Status: string(record.Status),
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt: record.UpdatedAt.UTC().Format(rfc3339),
		Revision:  record.Revision,
	}
}

// toStoryEntityDTO converts a domain entity to its transport view.
func toStoryEntityDTO(record storydomain.StoryEntity) StoryEntityDTO {
	return StoryEntityDTO{
		ID: record.ID, ProjectID: record.ProjectID, Type: string(record.Type),
		CanonicalName: record.CanonicalName, Status: string(record.Status),
		SourceScope: string(record.SourceScope), CurrentProfileVersionID: record.CurrentProfileVersionID,
		DeletedAt: rfc3339OrEmpty(record.DeletedAt),
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt: record.UpdatedAt.UTC().Format(rfc3339),
		Revision:  record.Revision,
	}
}

// toStoryEventDTO converts a domain event to its transport view.
func toStoryEventDTO(record storydomain.StoryEvent) StoryEventDTO {
	return StoryEventDTO{
		ID: record.ID, ProjectID: record.ProjectID, ChapterID: record.ChapterID,
		Ordinal: record.Ordinal, Name: record.Name, Description: record.Description,
		EventType: record.EventType, StoryTimeText: record.StoryTimeText,
		StoryTimeOrder: record.StoryTimeOrder, LocationEntityID: record.LocationEntityID,
		CauseSummary: record.CauseSummary, ResultSummary: record.ResultSummary,
		Importance: record.Importance, Confidence: record.Confidence,
		Status: string(record.Status), SourceScope: string(record.SourceScope),
		CreatedByAgentRunID: record.CreatedByAgentRunID,
		DeletedAt:           rfc3339OrEmpty(record.DeletedAt),
		CreatedAt:           record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt:           record.UpdatedAt.UTC().Format(rfc3339),
		Revision:            record.Revision,
	}
}

// toStoryRelationDTO converts a domain relation to its transport view.
func toStoryRelationDTO(record storydomain.StoryRelation) StoryRelationDTO {
	return StoryRelationDTO{
		ID: record.ID, ProjectID: record.ProjectID, Type: string(record.Type),
		SourceEntityType: record.SourceEntityType, SourceEntityID: record.SourceEntityID,
		TargetEntityType: record.TargetEntityType, TargetEntityID: record.TargetEntityID,
		ValidFromEventID: record.ValidFromEventID, ValidToEventID: record.ValidToEventID,
		Confidence: record.Confidence, Status: string(record.Status),
		SourceScope: string(record.SourceScope),
		CreatedAt:   record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt:   record.UpdatedAt.UTC().Format(rfc3339),
		Revision:    record.Revision,
	}
}

// toEpisodeDTO converts a domain episode to its transport view.
func toEpisodeDTO(record scriptdomain.Episode) EpisodeDTO {
	return EpisodeDTO{
		ID: record.ID, Key: record.Key(), ProjectID: record.ProjectID,
		SeasonNumber: record.SeasonNumber, EpisodeNumber: record.EpisodeNumber,
		Title: record.Title, Status: string(record.Status),
		SourceChapterStartID: record.SourceChapterStartID, SourceChapterEndID: record.SourceChapterEndID,
		TargetDurationSeconds:              record.TargetDurationSeconds,
		CurrentStorySkeletonVersionID:      record.CurrentStorySkeletonVersionID,
		CurrentAdaptationStrategyVersionID: record.CurrentAdaptationStrategyVersionID,
		CurrentScriptVersionID:             record.CurrentScriptVersionID,
		DeletedAt:                          rfc3339OrEmpty(record.DeletedAt),
		CreatedAt:                          record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt:                          record.UpdatedAt.UTC().Format(rfc3339),
		Revision:                           record.Revision,
	}
}

// toScriptDTO converts a domain script identity to its transport view.
func toScriptDTO(record scriptdomain.Script) ScriptDTO {
	return ScriptDTO{
		ID: record.ID, EpisodeID: record.EpisodeID,
		CurrentVersionID: record.CurrentVersionID,
		CreatedAt:        record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt:        record.UpdatedAt.UTC().Format(rfc3339),
		Revision:         record.Revision,
	}
}

// toScriptVersionDTO converts a domain script version to its transport view.
func toScriptVersionDTO(record scriptdomain.ScriptVersion) ScriptVersionDTO {
	return ScriptVersionDTO{
		ID: record.ID, ScriptID: record.ScriptID, VersionNumber: record.VersionNumber,
		Status: string(record.Status), BasedOnVersionID: record.BasedOnVersionID,
		StorySkeletonVersionID:      record.StorySkeletonVersionID,
		AdaptationStrategyVersionID: record.AdaptationStrategyVersionID,
		EstimatedDurationSeconds:    record.EstimatedDurationSeconds,
		Summary:                     record.Summary, SourceAgentRunID: record.SourceAgentRunID,
		CreatedByType: string(record.CreatedByType), CreatedByID: record.CreatedByID,
		ChangeReason: record.ChangeReason, LegacyMetadata: record.LegacyMetadata,
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toSceneDTO converts a domain scene to its transport view.
func toSceneDTO(record scriptdomain.Scene) SceneDTO {
	return SceneDTO{
		ID: record.ID, ScriptVersionID: record.ScriptVersionID, Ordinal: record.Ordinal,
		SceneNumber: record.SceneNumber, Slugline: record.Slugline,
		InteriorExterior: string(record.InteriorExterior),
		LocationEntityID: record.LocationEntityID, TimeOfDay: record.TimeOfDay,
		Summary: record.Summary, DramaticGoal: record.DramaticGoal,
		EstimatedDurationSeconds: record.EstimatedDurationSeconds,
		SourceStoryEventID:       record.SourceStoryEventID,
		IsOriginalAdaptation:     record.IsOriginalAdaptation,
		CreatedAt:                record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt:                record.UpdatedAt.UTC().Format(rfc3339),
		Revision:                 record.Revision,
	}
}

// toShotDTO converts a domain shot to its transport view.
func toShotDTO(record scriptdomain.Shot) ShotDTO {
	return ShotDTO{
		ID: record.ID, SceneID: record.SceneID, Ordinal: record.Ordinal,
		ShotNumber: record.ShotNumber, ShotSize: record.ShotSize,
		CameraAngle: record.CameraAngle, CameraMovement: record.CameraMovement,
		EstimatedDurationSeconds: record.EstimatedDurationSeconds,
		VisualDescription:        record.VisualDescription,
		ActionDescription:        record.ActionDescription,
		AudioIntent:              record.AudioIntent, ContinuityNotes: record.ContinuityNotes,
		Status:    string(record.Status),
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt: record.UpdatedAt.UTC().Format(rfc3339),
		Revision:  record.Revision,
	}
}

// toDirectorPlanVersionDTO converts a domain director plan version.
func toDirectorPlanVersionDTO(record storyboard.DirectorPlanVersion) DirectorPlanVersionDTO {
	return DirectorPlanVersionDTO{
		ID: record.ID, EpisodeID: record.EpisodeID, VersionNumber: record.VersionNumber,
		Status: string(record.Status), BasedOnVersionID: record.BasedOnVersionID,
		ScriptVersionID: record.ScriptVersionID, VisualRhythm: record.VisualRhythm,
		CameraLanguage: record.CameraLanguage, ColorLighting: record.ColorLighting,
		Staging: record.Staging, ContinuityRules: record.ContinuityRules,
		AudioDirection: record.AudioDirection, ShotOverridesJSON: record.ShotOverridesJSON,
		SourceAgentRunID: record.SourceAgentRunID, CreatedByType: string(record.CreatedByType),
		CreatedByID: record.CreatedByID, ChangeReason: record.ChangeReason,
		LegacyMetadata: record.LegacyMetadata,
		CreatedAt:      record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toStoryboardDTO converts a domain storyboard identity to its transport view.
func toStoryboardDTO(record storyboard.Storyboard) StoryboardDTO {
	return StoryboardDTO{
		ID: record.ID, EpisodeID: record.EpisodeID, CurrentVersionID: record.CurrentVersionID,
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt: record.UpdatedAt.UTC().Format(rfc3339),
		Revision:  record.Revision,
	}
}

// toStoryboardVersionDTO converts a domain storyboard version.
func toStoryboardVersionDTO(record storyboard.StoryboardVersion) StoryboardVersionDTO {
	return StoryboardVersionDTO{
		ID: record.ID, StoryboardID: record.StoryboardID, VersionNumber: record.VersionNumber,
		Status: string(record.Status), ScriptVersionID: record.ScriptVersionID,
		DirectorPlanVersionID: record.DirectorPlanVersionID,
		BasedOnVersionID:      record.BasedOnVersionID,
		SourceAgentRunID:      record.SourceAgentRunID,
		CreatedByType:         string(record.CreatedByType), CreatedByID: record.CreatedByID,
		ChangeReason: record.ChangeReason, LegacyMetadata: record.LegacyMetadata,
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toStoryboardItemDTO converts a domain storyboard item.
func toStoryboardItemDTO(record storyboard.StoryboardItem) StoryboardItemDTO {
	return StoryboardItemDTO{
		ID: record.ID, StoryboardVersionID: record.StoryboardVersionID,
		ShotID: record.ShotID, Ordinal: record.Ordinal, ShotSize: record.ShotSize,
		CameraAngle: record.CameraAngle, CameraMovement: record.CameraMovement,
		DurationSeconds: record.DurationSeconds, VisualDescription: record.VisualDescription,
		ActionDescription:    record.ActionDescription,
		DialogueAudioSummary: record.DialogueAudioSummary,
		ContinuityNotes:      record.ContinuityNotes,
		// FR-070's three, which the mapper would otherwise drop on the way out — the same
		// shape of gap the asset mapper had with five columns.
		FirstFrameDescription:  record.FirstFrameDescription,
		LastFrameDescription:   record.LastFrameDescription,
		VideoMotionDescription: record.VideoMotionDescription,
		Status:                 string(record.Status),
		CreatedAt:              record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt:              record.UpdatedAt.UTC().Format(rfc3339),
		Revision:               record.Revision,
	}
}

// toStoryboardPanelVersionDTO converts a domain panel version.
func toStoryboardPanelVersionDTO(record storyboard.StoryboardPanelVersion) StoryboardPanelVersionDTO {
	return StoryboardPanelVersionDTO{
		ID: record.ID, StoryboardItemID: record.StoryboardItemID,
		VersionNumber: record.VersionNumber, Status: string(record.Status),
		BasedOnVersionID: record.BasedOnVersionID, VisualPrompt: record.VisualPrompt,
		NegativePrompt: record.NegativePrompt, ReferencePolicyJSON: record.ReferencePolicyJSON,
		ApprovedImageAssetVersionID: record.ApprovedImageAssetVersionID,
		SourceAgentRunID:            record.SourceAgentRunID,
		CreatedByType:               string(record.CreatedByType), CreatedByID: record.CreatedByID,
		ChangeReason: record.ChangeReason, LegacyMetadata: record.LegacyMetadata,
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toWorkflowRunDTO converts a domain workflow run.
func toWorkflowRunDTO(record workflow.WorkflowRun) WorkflowRunDTO {
	return WorkflowRunDTO{
		ID: record.ID, ProjectID: record.ProjectID, EpisodeID: record.EpisodeID,
		WorkflowType: record.WorkflowType, CurrentStage: string(record.CurrentStage),
		Status: string(record.Status), ActiveStageRunID: record.ActiveStageRunID,
		ConfigurationJSON: record.ConfigurationJSON, RetryCount: record.RetryCount,
		CreatedAt:   record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt:   record.UpdatedAt.UTC().Format(rfc3339),
		CompletedAt: rfc3339OrEmpty(record.CompletedAt),
		Revision:    record.Revision,
	}
}

// toStageRunDTO converts a domain stage attempt.
func toStageRunDTO(record workflow.StageRun) StageRunDTO {
	return StageRunDTO{
		ID: record.ID, WorkflowRunID: record.WorkflowRunID, Stage: string(record.Stage),
		Attempt: record.Attempt, ExecutionAgentKey: record.ExecutionAgentKey,
		Status: string(record.Status), InputJSON: record.InputJSON,
		ValidatedOutputJSON: record.ValidatedOutputJSON,
		RawOutputFileID:     record.RawOutputFileID,
		ErrorCode:           record.ErrorCode, ErrorMessage: record.ErrorMessage,
		CreatedAt:  record.CreatedAt.UTC().Format(rfc3339),
		StartedAt:  rfc3339OrEmpty(record.StartedAt),
		FinishedAt: rfc3339OrEmpty(record.FinishedAt),
		Revision:   record.Revision,
	}
}

// toReviewReportDTO converts a review report and its findings. The issue slice
// is always non-nil, so the frontend can map over it without a guard.
func toReviewReportDTO(record workflow.ReviewReport, issues []workflow.ReviewIssue) ReviewReportDTO {
	return ReviewReportDTO{
		ID: record.ID, StageRunID: record.StageRunID,
		SupervisorKey: record.SupervisorKey, RulesetVersion: record.RulesetVersion,
		Score: record.Score, Grade: string(record.Grade), Passed: record.Passed,
		Severity: string(record.Severity), RecommendedAction: record.RecommendedAction,
		Summary:   record.Summary,
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		Issues:    toReviewIssueDTOs(issues),
	}
}

// toReviewIssueDTOs converts a report's findings, always returning a list.
func toReviewIssueDTOs(records []workflow.ReviewIssue) []ReviewIssueDTO {
	issues := make([]ReviewIssueDTO, 0, len(records))
	for _, record := range records {
		issues = append(issues, ReviewIssueDTO{
			ID: record.ID, ReviewReportID: record.ReviewReportID, Rule: record.Rule,
			Severity: string(record.Severity), EntityType: record.EntityType,
			EntityID: record.EntityID, Location: record.Location, Field: record.Field,
			Problem: record.Problem, Suggestion: record.Suggestion,
			EvidenceJSON: record.EvidenceJSON, AutoFixable: record.AutoFixable,
			// An empty source is read as the supervisor's, which is what every finding written
			// before the column existed was. The default is stated here as well as in the service
			// and the repository so a reader of this DTO cannot receive an empty mark and have to
			// guess which half produced the finding.
			Source: sourceOrLLM(record.Source),
			Status: string(record.Status), ResolvedBy: record.ResolvedBy,
			ResolvedAt: rfc3339OrEmpty(record.ResolvedAt),
			CreatedAt:  record.CreatedAt.UTC().Format(rfc3339),
		})
	}
	return issues
}

// sourceOrLLM defaults a finding's source mark.
//
// The check is written out rather than calling strings.TrimSpace because this file does not import
// strings, and adding an import to one package for a four-character test would be a bigger change
// than the test. A mark that is only whitespace is empty, which is the case this exists for.
func sourceOrLLM(source string) string {
	for _, symbol := range source {
		switch symbol {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return source
		}
	}
	return string(appworkflow.IssueSourceLLM)
}

// toUserGateDecisionDTO converts a domain gate decision.
func toUserGateDecisionDTO(record workflow.UserGateDecision) UserGateDecisionDTO {
	return UserGateDecisionDTO{
		ID: record.ID, WorkflowRunID: record.WorkflowRunID, StageRunID: record.StageRunID,
		Decision: string(record.Decision), IssueIDsJSON: record.IssueIDsJSON,
		Instruction: record.Instruction, Reason: record.Reason,
		LockedEntityRefsJSON: record.LockedEntityRefsJSON,
		CreatedByType:        string(record.CreatedByType), CreatedByID: record.CreatedByID,
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toWorkflowEventDTO converts a domain audit event.
func toWorkflowEventDTO(record workflow.WorkflowEvent) WorkflowEventDTO {
	return WorkflowEventDTO{
		ID: record.ID, WorkflowRunID: record.WorkflowRunID, StageRunID: record.StageRunID,
		EventType: string(record.EventType), FromStatus: record.FromStatus,
		ToStatus: record.ToStatus, PayloadJSON: record.PayloadJSON,
		ActorType: string(record.ActorType), ActorID: record.ActorID,
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toStaleMarkDTO converts a domain staleness mark to its transport view.
func toStaleMarkDTO(record staleness.Mark) StaleMarkDTO {
	return StaleMarkDTO{
		ArtifactType: string(record.ArtifactType), ArtifactID: record.ArtifactID,
		ProjectID: record.ProjectID, Severity: string(record.Severity),
		Reason: record.Reason, UpstreamType: string(record.UpstreamType),
		UpstreamID: record.UpstreamID, Waived: record.Waived,
		WaivedByDecisionID: record.WaivedByDecisionID,
		WaivedReason:       record.WaivedReason, ClearedAt: record.ClearedAt,
	}
}

// toStaleMarkDTOs converts a mark list, always returning a list.
func toStaleMarkDTOs(records []staleness.Mark) []StaleMarkDTO {
	marks := make([]StaleMarkDTO, 0, len(records))
	for _, record := range records {
		marks = append(marks, toStaleMarkDTO(record))
	}
	return marks
}

// toDramaError maps a drama domain error to a stable application error.
//
// The category is preserved as the code fragment and the cause is dropped: a
// repository's driver text never reaches the user. An application error is
// passed through unchanged, because it already carries a safe message and a
// code the caller can act on.
func toDramaError(err error) error {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*apperror.Error); ok {
		return appErr
	}
	if category, message, ok := domainStoryError(err); ok {
		return apperror.New("DRAMA_"+upperText(category), "drama", false, message, nil)
	}
	if category, message, ok := domainScriptError(err); ok {
		return apperror.New("DRAMA_"+upperText(category), "drama", false, message, nil)
	}
	if category, message, ok := domainStoryboardError(err); ok {
		return apperror.New("DRAMA_"+upperText(category), "drama", false, message, nil)
	}
	if category, message, ok := domainWorkflowError(err); ok {
		return apperror.New("DRAMA_"+upperText(category), "drama", false, message, nil)
	}
	if category, message, ok := domainStalenessError(err); ok {
		return apperror.New("DRAMA_"+upperText(category), "drama", false, message, nil)
	}
	if category, message, ok := domainVersioningError(err); ok {
		return apperror.New("DRAMA_"+upperText(category), "drama", false, message, nil)
	}
	if category, message, ok := domainImportError(err); ok {
		return apperror.New("DRAMA_"+upperText(category), "drama", false, message, nil)
	}
	if category, message, ok := domainExtractionError(err); ok {
		return apperror.New("DRAMA_"+upperText(category), "drama", false, message, nil)
	}
	// The media errors are the application's rather than a domain's, and they are mapped here rather
	// than falling through to the branch below because one of them is not a failure at all: "this
	// machine has no ffmpeg" is a fact the section renders as a disabled button with a diagnostic, and
	// the generic message would replace the sentence that names the program to install.
	if category, message, ok := mediaError(err); ok {
		return apperror.New("MEDIA_"+upperText(category), "media", false, message, nil)
	}
	return apperror.New("DRAMA_REQUEST_FAILED", "drama", false, "The drama request failed.", err)
}

// domainStoryError extracts a story error's category and safe message.
func domainStoryError(err error) (category string, safeMessage string, ok bool) {
	domainErr, is := storydomain.AsError(err)
	if !is {
		return "", "", false
	}
	return string(domainErr.Category), domainErr.SafeMessage, true
}

// domainScriptError extracts a script error's category and safe message.
func domainScriptError(err error) (category string, safeMessage string, ok bool) {
	domainErr, is := scriptdomain.AsError(err)
	if !is {
		return "", "", false
	}
	return string(domainErr.Category), domainErr.SafeMessage, true
}

// domainStoryboardError extracts a storyboard error's category and safe message.
func domainStoryboardError(err error) (category string, safeMessage string, ok bool) {
	domainErr, is := storyboard.AsError(err)
	if !is {
		return "", "", false
	}
	return string(domainErr.Category), domainErr.SafeMessage, true
}

// domainWorkflowError extracts a workflow error's category and safe message.
func domainWorkflowError(err error) (category string, safeMessage string, ok bool) {
	domainErr, is := workflow.AsError(err)
	if !is {
		return "", "", false
	}
	return string(domainErr.Category), domainErr.SafeMessage, true
}

// domainStalenessError extracts a staleness error's category and safe message.
func domainStalenessError(err error) (category string, safeMessage string, ok bool) {
	domainErr, is := staleness.AsError(err)
	if !is {
		return "", "", false
	}
	return string(domainErr.Category), domainErr.SafeMessage, true
}

// domainVersioningError extracts a version rule's category and safe message.
//
// The shared version vocabulary decides whether a version may be approved or
// superseded, and its errors are neither this package's nor any one aggregate's:
// versioning.CanApprove raises them for a script version, a story skeleton and
// every other versioned family, so approving an already-approved script version
// arrives here rather than through the script domain.
func domainVersioningError(err error) (category string, safeMessage string, ok bool) {
	domainErr, is := versioning.AsError(err)
	if !is {
		return "", "", false
	}
	return string(domainErr.Category), domainErr.SafeMessage, true
}

// ListDomainEventsRequest narrows an event query.
//
// Every field is optional except the project: the stream is project-scoped
// because every drama query is, and an unscoped read would be a cross-project
// leak.
type ListDomainEventsRequest struct {
	ProjectID string `json:"projectId"`
	// AggregateType and AggregateID narrow to one subject's history. They are
	// applied together or not at all.
	AggregateType string `json:"aggregateType,omitempty"`
	AggregateID   string `json:"aggregateId,omitempty"`
	// EventType narrows to one kind of event.
	EventType string `json:"eventType,omitempty"`
	// TraceID narrows to the events of one action.
	TraceID string `json:"traceId,omitempty"`
	// Limit caps the result and is clamped by the service.
	Limit int `json:"limit,omitempty"`
}

// DomainEventDTO is the transport view of one domain event.
type DomainEventDTO struct {
	EventID       string `json:"eventId"`
	EventType     string `json:"eventType"`
	SchemaVersion int    `json:"schemaVersion"`
	AggregateType string `json:"aggregateType"`
	AggregateID   string `json:"aggregateId"`
	ProjectID     string `json:"projectId"`
	OccurredAt    string `json:"occurredAt"`
	TraceID       string `json:"traceId"`
	Payload       string `json:"payload"`
}

func toDomainEventDTO(record event.Event) DomainEventDTO {
	return DomainEventDTO{
		EventID:       record.EventID,
		EventType:     string(record.EventType),
		SchemaVersion: record.SchemaVersion,
		AggregateType: string(record.AggregateType),
		AggregateID:   record.AggregateID,
		ProjectID:     record.ProjectID,
		OccurredAt:    rfc3339OrEmpty(record.OccurredAt),
		TraceID:       record.TraceID,
		Payload:       record.Payload,
	}
}

// ListDomainEvents returns a project's events newest first.
func (b *DramaBinding) ListDomainEvents(request ListDomainEventsRequest) ([]DomainEventDTO, error) {
	service := b.eventService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	// The event type is validated by the service against the section 17
	// vocabulary; the length bound here only keeps an absurd string from
	// reaching that comparison.
	if len(request.EventType) > maxEventTypeLength {
		return nil, bindingInvalidInput()
	}
	records, err := service.List(b.context(), appevents.ListFilter{
		ProjectID:     request.ProjectID,
		AggregateType: request.AggregateType,
		AggregateID:   request.AggregateID,
		EventType:     request.EventType,
		TraceID:       request.TraceID,
		Limit:         request.Limit,
	})
	if err != nil {
		return nil, toDramaError(err)
	}
	// A non-nil slice, so the caller can map over an empty answer.
	result := make([]DomainEventDTO, 0, len(records))
	for _, record := range records {
		result = append(result, toDomainEventDTO(record))
	}
	return result, nil
}

// CountDomainEvents reports how many events match a query.
func (b *DramaBinding) CountDomainEvents(request ListDomainEventsRequest) (int, error) {
	service := b.eventService()
	if service == nil {
		return 0, bindingUnavailable()
	}
	count, err := service.Count(b.context(), appevents.ListFilter{
		ProjectID:     request.ProjectID,
		AggregateType: request.AggregateType,
		AggregateID:   request.AggregateID,
		EventType:     request.EventType,
	})
	if err != nil {
		return 0, toDramaError(err)
	}
	return count, nil
}

// domainImportError extracts an import error's category and safe message.
func domainImportError(err error) (category string, safeMessage string, ok bool) {
	domainErr, is := importdomain.AsError(err)
	if !is {
		return "", "", false
	}
	return string(domainErr.Category), domainErr.SafeMessage, true
}

// domainExtractionError extracts an extraction refusal's code and message.
//
// Extraction classifies its own failures rather than borrowing the import
// vocabulary, because the distinction the caller acts on is different: an import
// refusal is about the document, while an extraction refusal is about the request
// or the reading. The code is carried through as the category so a caller can
// tell "no extractor is configured" from "the request was wrong" without parsing
// the message.
func domainExtractionError(err error) (category string, safeMessage string, ok bool) {
	if domainErr, is := extractiondomain.AsError(err); is {
		return domainErr.Code, domainErr.SafeMessage, true
	}
	// A validation refusal is the other half of this domain's errors, and it is
	// the one the user most needs to see: it says the reading did not match the
	// contract and names the paths that were wrong, so the message carries the
	// violations rather than only a code.
	//
	// The first version of this mapper recognised only the request error, so a
	// validation failure fell through to the generic branch and arrived as "The
	// drama request failed." with the violations discarded — leaving both the user
	// and, before the repair round existed, the model with nothing to act on.
	var validationErr *extractiondomain.Error
	if errors.As(err, &validationErr) {
		return "EXTRACTION_INVALID_OUTPUT", validationErr.Error(), true
	}
	return "", "", false
}

// formatOptionalTime renders a time that may be unset.
//
// A conflict that is still open has no resolved_at, and the zero time formats as
// "0001-01-01T00:00:00Z" — a value a UI would render as a real date. Empty is
// what the DTO's omitempty expects and what a panel reads as "not yet".
func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(rfc3339)
}

// storyboardStatusPointer converts an optional transport status into the domain's type.
//
// It is a small function rather than an inline conversion because the nil case is the
// contract: a caller that did not mention the status wants the stored one kept, and a
// conversion written inline is where that distinction gets lost.
func storyboardStatusPointer(value *string) *versioning.Status {
	if value == nil {
		return nil
	}
	status := versioning.Status(*value)
	return &status
}
