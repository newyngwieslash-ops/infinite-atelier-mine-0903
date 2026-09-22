package desktop

import (
	"context"
	"strings"
	"sync"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// MemoryBinding is the Wails surface for the memory center.
//
// # What it exposes, and what it deliberately does not
//
// PRD FR-120's MUST is "用户可查看、固定、编辑、删除和重建 Embedding" — the user can view, pin, edit,
// delete and rebuild. Those five are commands here, and the reads a person needs to use them: a list,
// a single memory, its entity links, and the sources of a summary. The recall PREVIEW is here too
// (ARCHITECTURE section 471 reserved the name `BuildMemoryContextPreview` for exactly this), because a
// user who cannot see what would be recalled cannot tell why an agent answered the way it did.
//
// It does NOT expose a way for a model to write a memory. Section 12.4 lists what must not become a
// high-confidence fact automatically, and the agent tool table has no memory write tool for that
// reason; a binding that offered one would put it back within reach of any caller.
//
// The service is optional. An unattached binding fails closed with a stable
// DESKTOP_BINDING_UNAVAILABLE error rather than panicking.
type MemoryBinding struct {
	mu      sync.RWMutex
	ctx     context.Context
	service *appmemory.Service
}

// AttachMemory supplies the memory service. A nil service leaves the binding unattached.
func AttachMemory(binding *MemoryBinding, ctx context.Context, service *appmemory.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.service = service
	binding.mu.Unlock()
}

// MemoryBindingUnavailable is the fail-closed error the composition root returns when the memory
// service could not be composed.
func MemoryBindingUnavailable() error {
	return bindingUnavailable()
}

func (b *MemoryBinding) context() context.Context {
	if b == nil {
		return context.Background()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *MemoryBinding) memoryService() *appmemory.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.service
}

// Bounds for the memory reads, so one call cannot ask for an unbounded page.
const (
	// MaxMemoryListLimit bounds a list read, matching the store's own ceiling.
	MaxMemoryListLimit = 500
	// MaxMemoryRecallPreview bounds a recall preview's channels.
	MaxMemoryRecallPreview = 200
)

// clampaMemoryLimit bounds a client-supplied limit.
func clampMemoryLimit(value int) int {
	if value <= 0 {
		return 100
	}
	if value > MaxMemoryListLimit {
		return MaxMemoryListLimit
	}
	return value
}

// MemoryDTO is the transport view of one memory.
//
// Every field the domain carries has a home here, and the two that a UI needs to explain a memory —
// where it came from and what it is about — are separate reads rather than nested documents: a list of
// two hundred memories should not carry two hundred link lists, and the drawer that wants one memory's
// links asks for them.
type MemoryDTO struct {
	ID string `json:"id"`
	// Type is episodic, semantic, procedural, artifact or summary.
	Type string `json:"type"`
	// ScopeProject is what the isolation is keyed on. Episode and AgentKey are shown because a user
	// asking "why did this agent not remember" needs to see which conversation a memory belongs to.
	ScopeProject string  `json:"scopeProject"`
	ScopeEpisode string  `json:"scopeEpisode,omitempty"`
	ScopeAgent   string  `json:"scopeAgent,omitempty"`
	Role         string  `json:"role,omitempty"`
	AgentKey     string  `json:"agentKey,omitempty"`
	Content      string  `json:"content"`
	Importance   float64 `json:"importance"`
	Confidence   float64 `json:"confidence"`
	// Embedded reports whether a vector is stored, WITHOUT exposing the vector: four kilobytes of
	// float32 is not something a JSON list should carry, and nothing in the UI can use it.
	Embedded         bool   `json:"embedded"`
	EmbeddingModel   string `json:"embeddingModel,omitempty"`
	EmbeddingVersion string `json:"embeddingVersion,omitempty"`
	EmbeddedAt       string `json:"embeddedAt,omitempty"`
	Summarized       bool   `json:"summarized"`
	Locked           bool   `json:"locked"`
	SourceType       string `json:"sourceType,omitempty"`
	// SourceID is the agent_messages row an episodic memory came from, which is what AC-MEM-004's
	// "UI 可跳原始消息" follows.
	SourceID  string `json:"sourceId,omitempty"`
	DeletedAt string `json:"deletedAt,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Revision  int64  `json:"revision"`
}

// toMemoryDTO converts one memory.
func toMemoryDTO(item memory.MemoryItem) MemoryDTO {
	return MemoryDTO{
		ID:               item.ID,
		Type:             string(item.Type),
		ScopeProject:     item.Scope.Project,
		ScopeEpisode:     item.Scope.Episode,
		ScopeAgent:       item.Scope.AgentKey,
		Role:             string(item.Role),
		AgentKey:         item.AgentKey,
		Content:          item.Content,
		Importance:       item.Importance,
		Confidence:       item.Confidence,
		Embedded:         len(item.EmbeddingBlob) > 0,
		EmbeddingModel:   item.EmbeddingModel,
		EmbeddingVersion: item.EmbeddingVersion,
		EmbeddedAt:       rfc3339OrEmpty(item.EmbeddedAt),
		Summarized:       item.Summarized,
		Locked:           item.Locked,
		SourceType:       string(item.SourceType),
		SourceID:         item.SourceID,
		DeletedAt:        rfc3339OrEmpty(item.DeletedAt),
		CreatedAt:        rfc3339OrEmpty(item.CreatedAt),
		UpdatedAt:        rfc3339OrEmpty(item.UpdatedAt),
		Revision:         int64(item.Revision),
	}
}

func toMemoryDTOs(items []memory.MemoryItem) []MemoryDTO {
	views := make([]MemoryDTO, 0, len(items))
	for _, item := range items {
		views = append(views, toMemoryDTO(item))
	}
	return views
}

// MemoryEntityLinkDTO is one edge from a memory to a domain entity.
type MemoryEntityLinkDTO struct {
	EntityType   string `json:"entityType"`
	EntityID     string `json:"entityId"`
	RelationType string `json:"relationType"`
}

// MemorySummarySourceDTO is one source of a summary, with what AC-MEM-004 asks to be preserved.
//
// Role, agent and time are the SOURCE MEMORY's own fields rather than fields a summary copied, and
// that is the point: a reader can see which turn contributed a line without trusting the summary to
// have recorded it.
type MemorySummarySourceDTO struct {
	MemoryID  string `json:"memoryId"`
	Order     int    `json:"order"`
	Role      string `json:"role,omitempty"`
	AgentKey  string `json:"agentKey,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	// MessageID is the transcript row the source memory cites, so a UI can jump to the original
	// message. It is empty for a source with no transcript row, which is what a summary of a
	// user-typed fact looks like.
	MessageID string `json:"messageId,omitempty"`
	// Missing reports that the source row is gone, so a reader sees a HOLE rather than a shorter list.
	Missing bool `json:"missing,omitempty"`
}

// MemoryQuery is a list read.
type MemoryQuery struct {
	ProjectID string `json:"projectId"`
	// Types is the set to include. Empty means every type.
	Types []string `json:"types,omitempty"`
	// IncludeDeleted shows what the user removed, which is what makes a soft delete visible.
	IncludeDeleted bool `json:"includeDeleted,omitempty"`
	Limit          int  `json:"limit,omitempty"`
}

// ListMemories returns a project's memories newest first.
func (b *MemoryBinding) ListMemories(query MemoryQuery) ([]MemoryDTO, error) {
	service := b.memoryService()
	if service == nil {
		return nil, MemoryBindingUnavailable()
	}
	if strings.TrimSpace(query.ProjectID) == "" {
		return nil, bindingInvalidInput()
	}
	types := make([]memory.MemoryType, 0, len(query.Types))
	for _, candidate := range query.Types {
		trimmed := memory.MemoryType(strings.TrimSpace(candidate))
		if !memory.IsValidMemoryType(trimmed) {
			return nil, bindingInvalidInput()
		}
		types = append(types, trimmed)
	}
	items, err := service.ListMemories(b.context(), appmemory.MemoryFilter{
		ProjectID:      strings.TrimSpace(query.ProjectID),
		Types:          types,
		IncludeDeleted: query.IncludeDeleted,
		Limit:          clampMemoryLimit(query.Limit),
	})
	if err != nil {
		return nil, toDramaError(err)
	}
	return toMemoryDTOs(items), nil
}

// GetMemory returns one memory.
func (b *MemoryBinding) GetMemory(id string) (MemoryDTO, error) {
	service := b.memoryService()
	if service == nil {
		return MemoryDTO{}, MemoryBindingUnavailable()
	}
	if strings.TrimSpace(id) == "" {
		return MemoryDTO{}, bindingInvalidInput()
	}
	item, err := service.GetMemory(b.context(), strings.TrimSpace(id))
	if err != nil {
		return MemoryDTO{}, toDramaError(err)
	}
	return toMemoryDTO(item), nil
}

// ListMemoryEntityLinks returns a memory's links to domain entities.
func (b *MemoryBinding) ListMemoryEntityLinks(memoryID string) ([]MemoryEntityLinkDTO, error) {
	service := b.memoryService()
	if service == nil {
		return nil, MemoryBindingUnavailable()
	}
	links, err := service.EntityLinks(b.context(), strings.TrimSpace(memoryID))
	if err != nil {
		return nil, toDramaError(err)
	}
	views := make([]MemoryEntityLinkDTO, 0, len(links))
	for _, link := range links {
		views = append(views, MemoryEntityLinkDTO{
			EntityType: link.EntityType, EntityID: link.EntityID, RelationType: link.RelationType,
		})
	}
	return views, nil
}

// ListSummarySources returns what a summary cites, in order.
//
// This is AC-MEM-004's "UI 可跳原始消息": the source rows come back with the memory's own role,
// agent, time and transcript citation, so a reader can follow a summary line to the turn behind it.
// The JUMP itself is the memory section's: it navigates to the source memory when a source row is
// clicked. This method's half of the criterion is the data a jump needs, and it is what the section
// renders.
//
// A source whose row is gone comes back marked rather than skipped, because a summary whose sources
// do not add up is exactly what the criterion's deletion policy has to be visible in.
func (b *MemoryBinding) ListSummarySources(summaryID string) ([]MemorySummarySourceDTO, error) {
	service := b.memoryService()
	if service == nil {
		return nil, MemoryBindingUnavailable()
	}
	sources, memories, err := service.ListSummarySources(b.context(), strings.TrimSpace(summaryID))
	if err != nil {
		return nil, toDramaError(err)
	}
	byID := map[string]memory.MemoryItem{}
	for _, item := range memories {
		byID[item.ID] = item
	}
	views := make([]MemorySummarySourceDTO, 0, len(sources))
	for _, source := range sources {
		view := MemorySummarySourceDTO{MemoryID: source.SourceMemoryID, Order: source.SourceOrder}
		if item, ok := byID[source.SourceMemoryID]; ok {
			view.Role = string(item.Role)
			view.AgentKey = item.AgentKey
			view.CreatedAt = rfc3339OrEmpty(item.CreatedAt)
			view.MessageID = item.SourceID
			view.Missing = item.Deleted()
		} else {
			view.Missing = true
		}
		views = append(views, view)
	}
	return views, nil
}

// DeleteMemoryRequest removes a memory, or the summaries that cite it.
type DeleteMemoryRequest struct {
	MemoryID string `json:"memoryId"`
	// InvalidateSummaries also deletes the summaries that cite it. False keeps them, which is the
	// default because deleting more than the user asked for is the unrecoverable direction.
	InvalidateSummaries bool `json:"invalidateSummaries,omitempty"`
	// Confirm must be true. A delete is not undoable through this API, and a field that says so is
	// what stops a mis-wired button from removing a memory.
	Confirm bool `json:"confirm"`
}

// MemoryDeleteResultDTO reports what a delete did, so a user sees which summaries went with it.
type MemoryDeleteResultDTO struct {
	Deleted              bool     `json:"deleted"`
	InvalidatedSummaries []string `json:"invalidatedSummaries"`
	KeptSummaries        []string `json:"keptSummaries"`
}

// DeleteMemory soft-deletes a memory.
//
// The actor is the USER, always, and it is stated here rather than taken from a request: this binding
// is the desktop surface a person uses, and section 14.5's rule is that a pinned memory is theirs to
// change. A caller that needs the agent path is not a caller of this method.
func (b *MemoryBinding) DeleteMemory(request DeleteMemoryRequest) (MemoryDeleteResultDTO, error) {
	service := b.memoryService()
	if service == nil {
		return MemoryDeleteResultDTO{}, MemoryBindingUnavailable()
	}
	if strings.TrimSpace(request.MemoryID) == "" || !request.Confirm {
		return MemoryDeleteResultDTO{}, bindingInvalidInput()
	}
	result, err := service.DeleteMemory(b.context(), appmemory.DeleteMemoryRequest{
		MemoryID:            strings.TrimSpace(request.MemoryID),
		Actor:               string(memory.ActorUser),
		InvalidateSummaries: request.InvalidateSummaries,
	})
	if err != nil {
		return MemoryDeleteResultDTO{}, toDramaError(err)
	}
	return MemoryDeleteResultDTO{
		Deleted:              result.Deleted,
		InvalidatedSummaries: nonEmpty(result.InvalidatedSummaries),
		KeptSummaries:        nonEmpty(result.KeptSummaries),
	}, nil
}

// MemoryPinRequest pins or unpins a memory, and edits it.
type MemoryPinRequest struct {
	MemoryID string `json:"memoryId"`
	Locked   bool   `json:"locked"`
}

// SetMemoryLocked pins or unpins a memory. The actor is the user, as DeleteMemory states.
func (b *MemoryBinding) SetMemoryLocked(request MemoryPinRequest) (bool, error) {
	service := b.memoryService()
	if service == nil {
		return false, MemoryBindingUnavailable()
	}
	if strings.TrimSpace(request.MemoryID) == "" {
		return false, bindingInvalidInput()
	}
	changed, err := service.SetMemoryLocked(b.context(), strings.TrimSpace(request.MemoryID),
		request.Locked, string(memory.ActorUser))
	if err != nil {
		return false, toDramaError(err)
	}
	return changed, nil
}

// RememberFactRequest saves something the user states as a semantic memory.
type RememberFactRequest struct {
	ProjectID string `json:"projectId"`
	// EpisodeID is optional: a preference about the whole project has no episode.
	EpisodeID string `json:"episodeId,omitempty"`
	Content   string `json:"content"`
	// Importance is what keeps a fact in the recall after the conversation has moved on. Zero takes
	// the domain's default rather than meaning "unimportant", which is the same convention every
	// other caller uses.
	Importance float64 `json:"importance,omitempty"`
	// EntityType and EntityID link the fact to what it is about. Both are optional; a preference
	// like "keep the dialogue terse" is about the project rather than about an entity.
	EntityType string `json:"entityType,omitempty"`
	EntityID   string `json:"entityId,omitempty"`
	// CreatedByID names the user who stated it, so the fact's citation is a person.
	CreatedByID string `json:"createdById,omitempty"`
	// Embed also embeds it, so it becomes searchable by meaning when a provider is configured.
	Embed bool `json:"embed,omitempty"`
}

// RememberFact saves a fact or preference the USER established.
//
// # Why this binding had to exist
//
// `RememberFact` is the only writer of `memory.TypeSemantic`, and an independent review found it had
// NO production caller at all: the desktop binding exposed eleven methods and not one of them wrote a
// memory, so in a composed build the semantic type was unreachable, FR-120's semantic channel could
// only ever be empty, and the facts channel's threshold exception — which AC-MEM-003 grades — could
// never fire, because nothing could set `locked` and `importance` on a row that did not exist.
//
// That is the "declared but unreachable" defect this repository's reviews keep finding, and the fix
// is the caller rather than the declaration.
//
// # Why the user is the only writer
//
// AGENT_CONTRACTS section 12.4 lists what must not be promoted automatically — unapproved candidates,
// supervisor suggestions, agent guesses, rejected versions, provider error text — and every item on
// that list is something a MODEL produces. The strongest way to keep them out is for the semantic
// write path to have no agent-facing surface, which is why the agent tool table carries no memory
// write tool and this binding is the only caller.
func (b *MemoryBinding) RememberFact(request RememberFactRequest) (MemoryDTO, error) {
	service := b.memoryService()
	if service == nil {
		return MemoryDTO{}, MemoryBindingUnavailable()
	}
	if strings.TrimSpace(request.ProjectID) == "" || strings.TrimSpace(request.Content) == "" {
		return MemoryDTO{}, bindingInvalidInput()
	}
	item, err := service.RememberFact(b.context(), appmemory.RememberFactRequest{
		Scope: appmemory.ScopeFor(strings.TrimSpace(request.ProjectID),
			strings.TrimSpace(request.EpisodeID), ""),
		Content:     request.Content,
		Importance:  request.Importance,
		EntityType:  strings.TrimSpace(request.EntityType),
		EntityID:    strings.TrimSpace(request.EntityID),
		CreatedByID: strings.TrimSpace(request.CreatedByID),
		Embed:       request.Embed,
	})
	if err != nil {
		return MemoryDTO{}, toDramaError(err)
	}
	return toMemoryDTO(item), nil
}

// MemoryLinkRequest attaches a memory to a domain entity.
type MemoryLinkRequest struct {
	MemoryID   string `json:"memoryId"`
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
}

// LinkMemory records that a memory is about a domain entity.
//
// It is a user command rather than something a write path infers, because a link is an assertion about
// the world: section 12.4 lists what must not become a high-confidence fact automatically, and an
// agent able to attach its own guesses to a character a user reads would be doing exactly that.
func (b *MemoryBinding) LinkMemory(request MemoryLinkRequest) error {
	service := b.memoryService()
	if service == nil {
		return MemoryBindingUnavailable()
	}
	if strings.TrimSpace(request.MemoryID) == "" || strings.TrimSpace(request.EntityType) == "" ||
		strings.TrimSpace(request.EntityID) == "" {
		return bindingInvalidInput()
	}
	if err := service.LinkMemory(b.context(), strings.TrimSpace(request.MemoryID),
		strings.TrimSpace(request.EntityType), strings.TrimSpace(request.EntityID)); err != nil {
		return toDramaError(err)
	}
	return nil
}

// MemoryEditRequest replaces a memory's text.
type MemoryEditRequest struct {
	MemoryID string `json:"memoryId"`
	Content  string `json:"content"`
	// Confirm must be true. An edit clears the memory's vector — a vector that still described the old
	// text would make the memory match queries about something it no longer says — so the memory stops
	// being searchable until a rebuild, and the caller has to say it meant that.
	Confirm bool `json:"confirm"`
}

// UpdateMemoryContent replaces a memory's text.
func (b *MemoryBinding) UpdateMemoryContent(request MemoryEditRequest) (bool, error) {
	service := b.memoryService()
	if service == nil {
		return false, MemoryBindingUnavailable()
	}
	if strings.TrimSpace(request.MemoryID) == "" || !request.Confirm {
		return false, bindingInvalidInput()
	}
	changed, err := service.UpdateMemoryContent(b.context(), strings.TrimSpace(request.MemoryID),
		request.Content, string(memory.ActorUser))
	if err != nil {
		return false, toDramaError(err)
	}
	return changed, nil
}

// RecallPreviewRequest asks what the memory layer WOULD carry for a query.
type RecallPreviewRequest struct {
	ProjectID string `json:"projectId"`
	EpisodeID string `json:"episodeId,omitempty"`
	AgentKey  string `json:"agentKey,omitempty"`
	Query     string `json:"query,omitempty"`
	// Threshold is the similarity floor. Zero uses the service's default, which is deliberately not
	// zero — see the memory service's DefaultThreshold.
	Threshold   float64 `json:"threshold,omitempty"`
	TokenBudget int     `json:"tokenBudget,omitempty"`
}

// MemoryRecallPreviewDTO is what a query would recall, with the reasoning visible.
//
// ARCHITECTURE section 471 reserved `BuildMemoryContextPreview` and this is it: the four channels of
// section 12.1 plus the score and similarity behind each scored candidate, because a user asking "why
// did the agent not remember" needs the number rather than the list. `SemanticSearched` is a field
// rather than a silent absence so a project with no embedding provider can be told apart from one
// whose query matched nothing.
type MemoryRecallPreviewDTO struct {
	Recent    []MemoryRecallItemDTO `json:"recent"`
	Facts     []MemoryScoredItemDTO `json:"facts"`
	Summaries []MemoryScoredItemDTO `json:"summaries"`
	Semantic  []MemoryScoredItemDTO `json:"semantic"`
	// UsedTokens and Truncated are the budget's own report.
	UsedTokens       int  `json:"usedTokens"`
	Truncated        bool `json:"truncated"`
	SemanticSearched bool `json:"semanticSearched"`
}

// MemoryRecallItemDTO is one line of the recent channel.
type MemoryRecallItemDTO struct {
	MessageID  string `json:"messageId"`
	Role       string `json:"role,omitempty"`
	Content    string `json:"content"`
	Provenance string `json:"provenance,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
}

// MemoryScoredItemDTO is one scored candidate, with the two numbers that put it in the context.
type MemoryScoredItemDTO struct {
	MemoryID string  `json:"memoryId"`
	Type     string  `json:"type"`
	Content  string  `json:"content"`
	Score    float64 `json:"score"`
	// Similarity is the raw cosine. It is separate from Score because the threshold applies to THIS
	// number, and a reader checking "was it below the threshold" needs it.
	Similarity float64 `json:"similarity"`
	Channel    string  `json:"channel"`
	// Pinned reports that the threshold did not apply, because the memory is locked and important.
	Pinned bool `json:"pinned"`
}

// PreviewMemoryRecall returns what a query would recall, without writing anything.
func (b *MemoryBinding) PreviewMemoryRecall(request RecallPreviewRequest) (MemoryRecallPreviewDTO, error) {
	service := b.memoryService()
	if service == nil {
		return MemoryRecallPreviewDTO{}, MemoryBindingUnavailable()
	}
	if strings.TrimSpace(request.ProjectID) == "" {
		return MemoryRecallPreviewDTO{}, bindingInvalidInput()
	}
	context, err := service.BuildMemoryContext(b.context(), appmemory.MemoryContextRequest{
		Scope: appmemory.ScopeFor(strings.TrimSpace(request.ProjectID),
			strings.TrimSpace(request.EpisodeID), strings.TrimSpace(request.AgentKey)),
		Query:       request.Query,
		Threshold:   request.Threshold,
		TokenBudget: request.TokenBudget,
	})
	if err != nil {
		return MemoryRecallPreviewDTO{}, toDramaError(err)
	}
	preview := MemoryRecallPreviewDTO{
		Recent:           make([]MemoryRecallItemDTO, 0, len(context.Recent)),
		Facts:            toScoredDTOs(context.Facts),
		Summaries:        toScoredDTOs(context.Summaries),
		Semantic:         toScoredDTOs(context.Semantic),
		UsedTokens:       context.UsedTokens,
		Truncated:        context.Truncated,
		SemanticSearched: context.SemanticSearched,
	}
	for _, item := range context.Recent {
		preview.Recent = append(preview.Recent, MemoryRecallItemDTO{
			MessageID:  item.MessageID,
			Role:       string(item.Role),
			Content:    item.Content,
			Provenance: item.Provenance,
			CreatedAt:  rfc3339OrEmpty(item.CreatedAt),
		})
	}
	return preview, nil
}

func toScoredDTOs(scored []appmemory.ScoredMemory) []MemoryScoredItemDTO {
	views := make([]MemoryScoredItemDTO, 0, len(scored))
	for _, candidate := range scored {
		views = append(views, MemoryScoredItemDTO{
			MemoryID:   candidate.Item.ID,
			Type:       string(candidate.Item.Type),
			Content:    candidate.Item.Content,
			Score:      candidate.Score,
			Similarity: candidate.Similarity,
			Channel:    candidate.Channel,
			Pinned:     candidate.Pinned,
		})
	}
	return views
}

// SummarizeMemoryRequest condenses a scope's unsummarised memories.
type SummarizeMemoryRequest struct {
	ProjectID string `json:"projectId"`
	EpisodeID string `json:"episodeId,omitempty"`
	AgentKey  string `json:"agentKey,omitempty"`
	// Level is 1 for a summary of messages and 2 for a summary of summaries. Zero means level one.
	Level int `json:"level,omitempty"`
	// Embed also embeds the summary, so it becomes searchable by meaning. False still writes the
	// summary; it simply has no vector until a rebuild.
	Embed bool `json:"embed,omitempty"`
}

// MemorySummarizeResultDTO reports what a summarise run produced.
type MemorySummarizeResultDTO struct {
	// Created is false when there was nothing new to condense, which is the ordinary result of running
	// this twice and NOT an error.
	Created bool       `json:"created"`
	Summary *MemoryDTO `json:"summary,omitempty"`
}

// SummarizeMemory condenses a scope's unsummarised memories into one summary.
func (b *MemoryBinding) SummarizeMemory(request SummarizeMemoryRequest) (MemorySummarizeResultDTO, error) {
	service := b.memoryService()
	if service == nil {
		return MemorySummarizeResultDTO{}, MemoryBindingUnavailable()
	}
	if strings.TrimSpace(request.ProjectID) == "" {
		return MemorySummarizeResultDTO{}, bindingInvalidInput()
	}
	summary, created, err := service.Summarize(b.context(), appmemory.SummarizeRequest{
		Scope: appmemory.ScopeFor(strings.TrimSpace(request.ProjectID),
			strings.TrimSpace(request.EpisodeID), strings.TrimSpace(request.AgentKey)),
		Level: request.Level,
		Embed: request.Embed,
	})
	if err != nil {
		return MemorySummarizeResultDTO{}, toDramaError(err)
	}
	result := MemorySummarizeResultDTO{Created: created}
	if created {
		view := toMemoryDTO(summary)
		result.Summary = &view
	}
	return result, nil
}

// RebuildMemoryEmbeddingRequest re-embeds a project's memories.
type RebuildMemoryEmbeddingRequest struct {
	ProjectID string `json:"projectId"`
	// Limit bounds one run, so a large project rebuilds in slices a caller can watch.
	Limit int `json:"limit,omitempty"`
}

// MemoryRebuildResultDTO reports what a rebuild run did.
type MemoryRebuildResultDTO struct {
	// Model and Version are the recipe the rebuild switched TO, which is what section 14.5's
	// "重建后切换索引版本" means for a user: after this, the old model's vectors are no longer searched.
	Model     string `json:"model"`
	Version   string `json:"version"`
	Rebuilt   int    `json:"rebuilt"`
	Failed    int    `json:"failed"`
	Remaining bool   `json:"remaining"`
}

// RebuildMemoryEmbedding re-embeds the memories that are not on the current model or version.
func (b *MemoryBinding) RebuildMemoryEmbedding(request RebuildMemoryEmbeddingRequest) (MemoryRebuildResultDTO, error) {
	service := b.memoryService()
	if service == nil {
		return MemoryRebuildResultDTO{}, MemoryBindingUnavailable()
	}
	if strings.TrimSpace(request.ProjectID) == "" {
		return MemoryRebuildResultDTO{}, bindingInvalidInput()
	}
	result, err := service.RebuildEmbedding(b.context(), appmemory.RebuildEmbeddingRequest{
		ProjectID: strings.TrimSpace(request.ProjectID),
		Limit:     request.Limit,
	})
	if err != nil {
		return MemoryRebuildResultDTO{}, toDramaError(err)
	}
	return MemoryRebuildResultDTO{
		Model: result.Model, Version: result.Version, Rebuilt: result.Rebuilt,
		Failed: result.Failed, Remaining: result.Remaining,
	}, nil
}

// nonEmpty returns a slice that is never nil, so the frontend can map unconditionally.
func nonEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
