package memory

import (
	"context"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// The user's commands over their own memory, which PRD FR-120 states as a MUST:
// "用户可查看、固定、编辑、删除和重建 Embedding".
//
// # Why these are commands rather than parts of a tool
//
// Section 12.4 lists what must NOT be promoted into a high-confidence fact automatically,
// and the list is a set of things a MODEL produces: unapproved candidates, supervisor
// suggestions, agent guesses, rejected versions, provider error text. The strongest way to
// keep a model out of that list is to give the write path no tool at all, so the only writers
// are a user command and the pipeline's own decided facts. Deleting and pinning are the same
// argument read backwards: section 14.5 says a locked memory is the USER's to change, and a
// tool that could delete one would make the lock advisory.

// MemoryFilter narrows a list read from the UI.
//
// It is a distinct type from MemoryListFilter because the two answer different questions: the
// repository's filter is what a query is built from, and this one is what a caller asks for.
// Keeping them apart is what lets the repository's read stay an implementation detail.
type MemoryFilter struct {
	ProjectID string
	// Types is the set to include. Empty means every type.
	Types []memory.MemoryType
	// IncludeDeleted shows what the user removed, which is what makes a soft delete visible
	// rather than a disappearance.
	IncludeDeleted bool
	Limit          int
}

// ListMemories returns a project's memories newest first.
func (s *Service) ListMemories(ctx context.Context, filter MemoryFilter) ([]memory.MemoryItem, error) {
	if !s.StorageAvailable() {
		return nil, storageUnavailable()
	}
	if strings.TrimSpace(filter.ProjectID) == "" {
		return nil, memory.InvalidError("A memory list must name its project.")
	}
	return s.items.ListItems(ctx, MemoryListFilter{
		ProjectID:      filter.ProjectID,
		Types:          filter.Types,
		IncludeDeleted: filter.IncludeDeleted,
		Limit:          filter.Limit,
	})
}

// GetMemory returns one memory, deleted ones included.
//
// A deleted memory is still readable, and that is what makes AC-MEM-004's deletion policy
// checkable: a reader has to be able to see that a summary's source was deleted rather than
// that it never existed.
func (s *Service) GetMemory(ctx context.Context, id string) (memory.MemoryItem, error) {
	if !s.StorageAvailable() {
		return memory.MemoryItem{}, storageUnavailable()
	}
	if strings.TrimSpace(id) == "" {
		return memory.MemoryItem{}, memory.InvalidError("A memory read must name the memory.")
	}
	return s.items.GetItem(ctx, id)
}

// EntityLinks returns a memory's links to domain entities.
func (s *Service) EntityLinks(ctx context.Context, memoryID string) ([]memory.EntityLink, error) {
	if !s.StorageAvailable() {
		return nil, storageUnavailable()
	}
	return s.items.ListEntityLinks(ctx, memoryID)
}

// DeleteMemoryRequest soft-deletes one memory.
type DeleteMemoryRequest struct {
	MemoryID string
	// Actor is who is deleting. It is not defaulted to the user: the caller has to say, because
	// section 14.5's rule turns on it and a default would make every caller a user.
	Actor string
	// InvalidateSummaries controls what happens to the summaries that cite this memory.
	//
	// It is the caller's choice, and the choice is real rather than a formality. AC-MEM-004
	// asks that "删除/失效行为符合策略" and this build's policy has exactly two defensible
	// readings: a summary whose source is gone may be INVALIDATED (deleted with it, because
	// its provenance no longer resolves) or KEPT (because it is still a faithful condensation
	// of what was said, and a user who deleted one turn did not ask to lose the memory of the
	// session). The zero value is false — keep — because deleting more than the user asked for
	// is the unrecoverable direction.
	InvalidateSummaries bool
}

// DeleteMemoryResult reports what a delete did.
//
// It is a result rather than an error because "the summaries are now invalid" is not a failure:
// it is the fact AC-MEM-004 wants visible.
type DeleteMemoryResult struct {
	Deleted bool
	// InvalidatedSummaries are the summary identifiers that were deleted with it.
	InvalidatedSummaries []string
	// KeptSummaries are the summaries that still cite it. They are reported even when
	// InvalidateSummaries was true, because the ones that could not be removed are exactly what
	// a reader needs to know about.
	KeptSummaries []string
}

// DeleteMemory removes a memory and applies the policy to the summaries that cite it.
//
// The order matters: the summaries are read BEFORE the delete, because after it the reverse
// lookup still works (the source rows survive as rows) but a reader would have to reason about
// a row that is already gone. Reading first means the result reports what existed.
func (s *Service) DeleteMemory(ctx context.Context, request DeleteMemoryRequest) (DeleteMemoryResult, error) {
	result := DeleteMemoryResult{InvalidatedSummaries: []string{}, KeptSummaries: []string{}}
	if !s.StorageAvailable() {
		return result, storageUnavailable()
	}
	id := strings.TrimSpace(request.MemoryID)
	if id == "" {
		return result, memory.InvalidError("A delete must name the memory.")
	}
	if !memory.IsValidActorType(request.Actor) {
		return result, memory.InvalidError("The actor deleting a memory is not recognised.")
	}
	if _, err := s.items.GetItem(ctx, id); err != nil {
		return result, err
	}
	summaries, err := s.items.SummariesOf(ctx, id)
	if err != nil {
		return result, err
	}
	deleted, err := s.items.DeleteItem(ctx, id, request.Actor, s.now())
	if err != nil {
		return result, err
	}
	result.Deleted = deleted
	for _, summary := range summaries {
		if !request.InvalidateSummaries {
			result.KeptSummaries = append(result.KeptSummaries, summary.ID)
			continue
		}
		// A summary is deleted as the USER, whatever actor asked, and that is not a loophole:
		// the caller already passed the pinned check on the memory it named, and the summary is
		// a derived artifact rather than something anyone pinned. Refusing here would leave the
		// policy half-applied, which is worse than the alternative.
		if _, err := s.items.DeleteItem(ctx, summary.ID, string(memory.ActorUser), s.now()); err != nil {
			return result, err
		}
		result.InvalidatedSummaries = append(result.InvalidatedSummaries, summary.ID)
	}
	return result, nil
}

// SetMemoryLocked pins or unpins a memory.
//
// The actor is checked by the store, not here, so two callers racing on the same row cannot
// both pass a check each made against a stale read.
func (s *Service) SetMemoryLocked(ctx context.Context, memoryID string, locked bool, actor string) (bool, error) {
	if !s.StorageAvailable() {
		return false, storageUnavailable()
	}
	if strings.TrimSpace(memoryID) == "" {
		return false, memory.InvalidError("A pin must name the memory.")
	}
	if !memory.IsValidActorType(actor) {
		return false, memory.InvalidError("The actor pinning a memory is not recognised.")
	}
	return s.items.SetLocked(ctx, memoryID, locked, actor, s.now())
}

// UpdateMemoryContent replaces a memory's text and clears its vector.
//
// The cleared vector is the store's rule and it is the right one: an edit makes the old vector
// wrong, and a vector that still described the old text would make the memory match queries
// about something it no longer says. Re-embedding is RebuildEmbedding's job, so the gap is
// visible as "not embedded yet" rather than as a stale match.
func (s *Service) UpdateMemoryContent(ctx context.Context, memoryID, content, actor string) (bool, error) {
	if !s.StorageAvailable() {
		return false, storageUnavailable()
	}
	if strings.TrimSpace(memoryID) == "" {
		return false, memory.InvalidError("An edit must name the memory.")
	}
	if !memory.IsValidActorType(actor) {
		return false, memory.InvalidError("The actor editing a memory is not recognised.")
	}
	return s.items.UpdateContent(ctx, memoryID, content, actor, s.now())
}

// RebuildEmbeddingRequest re-embeds a project's memories.
type RebuildEmbeddingRequest struct {
	ProjectID string
	// Limit bounds one run, so a large project rebuilds in slices a caller can watch. Zero uses
	// DefaultCandidates.
	Limit int
}

// RebuildEmbeddingResult reports what one rebuild run did.
type RebuildEmbeddingResult struct {
	// Model and Version are the embedding recipe that was applied, which is what section 14.5's
	// "重建后切换索引版本" switches TO.
	Model   string
	Version string
	// Rebuilt is how many memories were re-embedded.
	Rebuilt int
	// Failed is how many could not be, and it is reported rather than raised because a rebuild
	// that skipped three rows out of a thousand is a rebuild that mostly worked.
	Failed int
	// Remaining reports that the run stopped at its bound and there is more to do.
	Remaining bool
}

// RebuildEmbedding re-embeds the memories that are not on the current model or version.
//
// It is FR-120's "重建 Embedding" and section 14.5's "重建后切换索引版本", and the predicate is
// "not current" rather than "has no vector": a project that changes its embedding model needs
// the rows embedded by the OLD one found again, and a predicate that only looked for an empty
// blob would skip every one of them.
//
// Nothing here overwrites a vector in place with a different model's: the row's model and
// version columns are written with the new values, so a row is on exactly one index version at
// a time and a search for the old one stops finding it. That is the switch.
func (s *Service) RebuildEmbedding(ctx context.Context, request RebuildEmbeddingRequest) (RebuildEmbeddingResult, error) {
	result := RebuildEmbeddingResult{}
	if !s.StorageAvailable() {
		return result, storageUnavailable()
	}
	project := strings.TrimSpace(request.ProjectID)
	if project == "" {
		return result, memory.InvalidError("A rebuild must name its project.")
	}
	if s.embedder == nil || !s.embedder.Available(ctx, project) {
		return result, memory.StorageError("No embedding provider is configured for this project, so nothing can be rebuilt.", nil)
	}
	// The recipe is resolved by embedding one probe text, which is what tells the caller what
	// model and version the rebuild is switching TO. It costs one call and removes a second
	// place for the model name to be configured.
	probe, err := s.embedder.Embed(ctx, project, EmbeddingRequest{Texts: []string{"probe"}})
	if err != nil {
		return result, err
	}
	result.Model = probe.Model
	result.Version = probe.Version
	limit := request.Limit
	if limit <= 0 {
		limit = DefaultCandidates
	}
	pending, err := s.items.ItemsNeedingEmbedding(ctx, project, result.Model, result.Version, limit)
	if err != nil {
		return result, err
	}
	result.Remaining = len(pending) == limit
	items := make([]memory.MemoryItem, 0, len(pending))
	for _, item := range pending {
		items = append(items, item)
	}
	// Embedded in BATCHES rather than one call per row: the provider takes a list, and a
	// thousand calls would be a thousand round trips for one command.
	const batchSize = 32
	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		embedded, err := s.embedItems(ctx, project, items[start:end])
		if err != nil {
			result.Failed += end - start
			continue
		}
		result.Rebuilt += len(embedded)
		result.Failed += (end - start) - len(embedded)
	}
	return result, nil
}

// embedItems embeds a set of memories and stores their vectors.
//
// The returns are the memories that were successfully embedded, WITH their vector fields
// filled, so a caller can report what happened without re-reading the rows.
func (s *Service) embedItems(ctx context.Context, projectID string, items []memory.MemoryItem) ([]memory.MemoryItem, error) {
	if s.embedder == nil || s.vectors == nil {
		return nil, memory.StorageError("No embedding provider is configured.", nil)
	}
	embedable := make([]memory.MemoryItem, 0, len(items))
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		embedable = append(embedable, item)
		texts = append(texts, item.Content)
	}
	if len(texts) == 0 {
		return nil, nil
	}
	embedding, err := s.embedder.Embed(ctx, projectID, EmbeddingRequest{Texts: texts})
	if err != nil {
		return nil, err
	}
	if len(embedding.Vectors) != len(texts) {
		return nil, memory.StorageError("The embedding provider returned the wrong number of vectors.", nil)
	}
	if strings.TrimSpace(embedding.Model) == "" || strings.TrimSpace(embedding.Version) == "" {
		return nil, memory.StorageError("The embedding provider did not name the model that produced the vectors.", nil)
	}
	now := s.now()
	embedded := make([]memory.MemoryItem, 0, len(embedable))
	// The scope filter belongs to the index, so the write is grouped by scope: section 12.2
	// requires the filter to happen before scoring, and an index that stored a vector without
	// knowing whose it was could not honour that.
	byScope := map[string]struct {
		scope Scope
		items []VectorItem
	}{}
	for index, item := range embedable {
		normalized := memory.Normalize(embedding.Vectors[index])
		encoded := memory.EncodeVector(normalized)
		if err := s.items.AssignEmbedding(ctx, item.ID, encoded, embedding.Model, embedding.Version, now); err != nil {
			return embedded, err
		}
		item.EmbeddingBlob = encoded
		item.EmbeddingModel = embedding.Model
		item.EmbeddingVersion = embedding.Version
		item.EmbeddedAt = now
		embedded = append(embedded, item)
		key := item.Scope.Key()
		group := byScope[key]
		group.scope = item.Scope
		// The MODEL AND VERSION ride with the vector, and leaving them out was a real defect the
		// integration test caught: the index refuses a vector that names no model, because section
		// 14.5 makes the model part of what a vector is and a search selects by it. The refusal was
		// returned to this function's caller, which swallowed it by design (an embedding failure
		// does not undo the memory), so the observable effect was a memory with no vector and no
		// error anywhere — the exact "silently not stored" shape the index's contract exists to
		// prevent.
		group.items = append(group.items, VectorItem{
			ID:      item.ID,
			Vector:  normalized,
			Model:   embedding.Model,
			Version: embedding.Version,
		})
		byScope[key] = group
	}
	for _, group := range byScope {
		if err := s.vectors.Upsert(ctx, group.scope, group.items); err != nil {
			return embedded, err
		}
	}
	return embedded, nil
}
