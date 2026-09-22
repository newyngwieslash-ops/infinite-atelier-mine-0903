package memory

import (
	"context"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// service.go is the memory store's commands: the writes the pipeline and the user make, the
// multi-channel context the decision layer reads, and the deep recall the tool calls.

// storageUnavailable is the refusal every store-backed command gives when the memory store
// is not composed.
//
// It is a sentence rather than a code because it is what a user reads, and it says what is
// missing rather than what failed: a build with no memory store is a legitimate composition
// (the transcript port alone, which is what WP-07 shipped), and a caller has to be able to
// tell it apart from a store that broke.
func storageUnavailable() error {
	return memory.StorageError("No memory store is configured, so memories cannot be stored or searched.", nil)
}

// notFound reports whether an error is this domain's not-found.
func notFound(err error) bool {
	domainErr, ok := memory.AsError(err)
	return ok && domainErr.Category == memory.CategoryNotFound
}

// RememberMessageRequest records one turn as an episodic memory.
//
// It is what section 12.4's first write source is: "用户消息". The runtime calls it for the
// user's turn and for the model's, in the same invocation that records the transcript, which
// is what keeps the two from drifting.
type RememberMessageRequest struct {
	Scope   Scope
	Message string
	// MessageID is the agent_messages row this memory is the memory-side twin of, so a
	// reader can walk from a recalled memory to the transcript it came from. It is
	// REQUIRED: a memory with no citation would be an unattributable claim, which is what
	// section 12.4's list of things not to promote is fundamentally about.
	MessageID string
	Role      agent.MessageRole
	// AgentKey is the agent that produced the turn, kept apart from Scope.AgentKey because
	// a project-level memory has no agent in its scope and still names its producer.
	AgentKey string
	// AgentRunID links the memory to the run, so "which run said this" is answerable without
	// walking through the transcript.
	AgentRunID string
	// Importance and Confidence are the caller's scores. A zero Importance is read as
	// "unset" and defaulted, because the runtime has no importance model and a zero would
	// silently weight every turn it wrote out of section 12.2's fusion.
	Importance float64
	Confidence float64
	// Embed asks for the memory to be embedded when a provider is configured.
	Embed bool
}

// RememberMessage stores one turn.
//
// The row it writes is a MEMORY, not a transcript row: section 14.5 gives the user rights
// over it that a transcript row does not have, and ADR-0014 records why that is a separate
// table. The content is copied, and the copy is the cost of the ruling — what it buys is that
// deleting a memory does not rewrite the runtime's record of what happened.
//
// The importance default is a real decision rather than a convenience: PRD FR-120's fusion
// weights importance at 0.15, so a caller that left it zero would make every turn it wrote
// score lower than every turn some other caller scored, and the recall would order by which
// code path wrote the memory rather than by what mattered.
func (s *Service) RememberMessage(ctx context.Context, request RememberMessageRequest) (memory.MemoryItem, error) {
	if !s.StorageAvailable() {
		return memory.MemoryItem{}, storageUnavailable()
	}
	if err := request.Scope.Validate(); err != nil {
		return memory.MemoryItem{}, err
	}
	content := strings.TrimSpace(request.Message)
	if content == "" {
		return memory.MemoryItem{}, memory.InvalidError("A memory needs something said.")
	}
	messageID := strings.TrimSpace(request.MessageID)
	if messageID == "" {
		return memory.MemoryItem{}, memory.InvalidError("A memory must cite the message it came from.")
	}
	role := request.Role
	if role == "" {
		role = agent.MessageAssistant
	}
	if !agent.IsValidMessageRole(role) {
		return memory.MemoryItem{}, memory.InvalidError("The memory role is not recognised.")
	}
	id, err := s.mintID(ctx, "memory")
	if err != nil {
		return memory.MemoryItem{}, err
	}
	now := s.now()
	item := memory.MemoryItem{
		ID:         id,
		Type:       memory.TypeEpisodic,
		Scope:      request.Scope,
		Role:       role,
		AgentKey:   strings.TrimSpace(request.AgentKey),
		Content:    content,
		Importance: clampWeight(request.Importance, memory.DefaultImportance),
		Confidence: clampWeight(request.Confidence, memory.DefaultConfidence),
		SourceType: memory.SourceMessage,
		SourceID:   messageID,
		CreatedAt:  now,
		UpdatedAt:  now,
		Revision:   1,
	}
	if err := item.Validate(); err != nil {
		return memory.MemoryItem{}, err
	}
	if err := s.items.CreateItem(ctx, item); err != nil {
		return memory.MemoryItem{}, err
	}
	// The run the turn belonged to is recorded as a second link rather than as a column,
	// because section 14.1's source columns are a single pair and the message is the more
	// specific citation. A failure here does not undo the memory: the message citation is
	// what a reader follows, and this one is an index over it.
	if runID := strings.TrimSpace(request.AgentRunID); runID != "" {
		_ = s.items.AddEntityLinks(ctx, []memory.EntityLink{{
			MemoryID:     item.ID,
			EntityType:   "agent_run",
			EntityID:     runID,
			RelationType: memory.RelationReferences,
			CreatedAt:    now,
		}})
	}
	if request.Embed {
		// A failure to embed does not fail the write, for the reason the summary states: the
		// memory exists and its text is what a reader needs; the vector is an index over it,
		// and the rebuild command is the path that recovers a missing one.
		if embedded, err := s.embedItems(ctx, item.Scope.Project, []memory.MemoryItem{item}); err == nil && len(embedded) == 1 {
			item = embedded[0]
		}
	}
	return item, nil
}

// RememberFactRequest records a fact the USER established.
type RememberFactRequest struct {
	Scope      Scope
	Content    string
	Importance float64
	// EntityType and EntityID link the fact to what it is about, which is what the recall
	// and the UI both read. Both are optional: a preference like "keep the dialogue terse"
	// is about the project rather than about an entity.
	EntityType string
	EntityID   string
	// CreatedByID is the user who stated it.
	CreatedByID string
	Embed       bool
}

// RememberFact stores a semantic memory (DOMAIN_MODEL section 16's RememberProjectFact).
//
// Section 12.4's second list is what this method's shape enforces: "不要把以下内容自动当作
// 高置信事实: 未批准模型候选、Supervisor 建议、Agent 推测、被拒绝版本、Provider 错误文本".
// Nothing in this build calls it on a model's behalf — the only callers are the desktop
// binding and the pipeline's own decided facts — which is the strongest form the rule can
// take here. A tool that let an agent write one would make the list a matter of the agent's
// judgement.
func (s *Service) RememberFact(ctx context.Context, request RememberFactRequest) (memory.MemoryItem, error) {
	if !s.StorageAvailable() {
		return memory.MemoryItem{}, storageUnavailable()
	}
	if err := request.Scope.Validate(); err != nil {
		return memory.MemoryItem{}, err
	}
	content := strings.TrimSpace(request.Content)
	if content == "" {
		return memory.MemoryItem{}, memory.InvalidError("A remembered fact needs something stated.")
	}
	id, err := s.mintID(ctx, "memory")
	if err != nil {
		return memory.MemoryItem{}, err
	}
	now := s.now()
	item := memory.MemoryItem{
		ID:      id,
		Type:    memory.TypeSemantic,
		Scope:   request.Scope,
		Content: content,
		// A fact the user stated is what the user thinks, and confidence says how sure the
		// SOURCE is: a user's own statement is as sure as this system gets, and it is not a
		// model's guess. Importance is theirs, because importance is what keeps a fact in the
		// recall after the conversation has moved on.
		Importance: clampWeight(request.Importance, memory.DefaultImportance),
		Confidence: 1,
		CreatedAt:  now,
		UpdatedAt:  now,
		Revision:   1,
	}
	// The source pair is both-or-neither (the domain refuses half of one). A user-typed fact
	// cites the user when the caller named one, and cites nothing when it did not — which is
	// what SourceUnset is for rather than an empty identifier wearing a type.
	if createdBy := strings.TrimSpace(request.CreatedByID); createdBy != "" {
		item.SourceType = memory.SourceUser
		item.SourceID = createdBy
	}
	if err := item.Validate(); err != nil {
		return memory.MemoryItem{}, err
	}
	if err := s.items.CreateItem(ctx, item); err != nil {
		return memory.MemoryItem{}, err
	}
	if entityType, entityID := strings.TrimSpace(request.EntityType), strings.TrimSpace(request.EntityID); entityType != "" && entityID != "" {
		if err := s.items.AddEntityLinks(ctx, []memory.EntityLink{{
			MemoryID:     item.ID,
			EntityType:   entityType,
			EntityID:     entityID,
			RelationType: memory.RelationAbout,
			CreatedAt:    now,
		}}); err != nil {
			return item, err
		}
	}
	if request.Embed {
		if embedded, err := s.embedItems(ctx, item.Scope.Project, []memory.MemoryItem{item}); err == nil && len(embedded) == 1 {
			item = embedded[0]
		}
	}
	return item, nil
}

// mintID mints an identifier and refuses a collision.
//
// The identifier is minted in the application layer (ADR-0005), so a collision means the
// generator repeated itself rather than that the caller wrote twice. Checking turns a
// mysterious constraint failure into a retryable one.
func (s *Service) mintID(ctx context.Context, _ string) (string, error) {
	id, err := s.ids.New()
	if err != nil {
		return "", storageUnavailable()
	}
	exists, err := s.items.MemoryItemExists(ctx, id)
	if err != nil {
		return "", err
	}
	if exists {
		return "", memory.ConflictError("That memory identifier is already used.")
	}
	return id, nil
}

// MemoryContextRequest asks for the assembled memory context.
type MemoryContextRequest struct {
	Scope Scope
	// Query is what the caller is about to do, used for the semantic channel.
	Query string
	// ExcludeMemoryIDs are memories already in the context, so a second build does not
	// repeat them.
	ExcludeMemoryIDs []string
	// TokenBudget bounds the assembly. Zero uses DefaultContextTokenBudget.
	TokenBudget int
	// Threshold is the minimum similarity for the semantic channel. A negative value is
	// read as zero, because section 12.2's fusion is a convex combination and a negative
	// threshold would admit anti-correlated memories the caller cannot have meant.
	Threshold float64
}

// Bounds for the assembled context.
const (
	// DefaultContextTokenBudget is AGENT_CONTRACTS section 12.1's own example value.
	DefaultContextTokenBudget = 6000
	// MaxContextTokenBudget bounds what a caller may ask for, so a large request cannot
	// crowd out every layer above memory in section 5.3's priority order.
	MaxContextTokenBudget = 24000
	// DefaultThreshold is the similarity a candidate must reach when the caller states none.
	//
	// It is deliberately not zero. PRD FR-120: "有相似度阈值和 Top-K；不得无条件返回低相关
	// 结果" — a default of zero would make the threshold a feature nobody turned on, and the
	// criterion would be satisfied only in the tests that set it.
	DefaultThreshold = 0.30
	// DefaultContextSummaries and DefaultContextSemantic bound each scored channel.
	DefaultContextSummaries = 6
	DefaultContextSemantic  = 10
	// RecentWindow is how many recent turns the context carries, which is section 12.1's
	// first bullet.
	RecentWindow = 12
)

// MemoryContext is what the caller gets (AGENT_CONTRACTS section 12.1).
//
// The four lists and the three bookkeeping fields are that section's output shape, and the
// channel names are its own: recent, summaries, semantic, facts.
type MemoryContext struct {
	// Recent is the transcript's tail, oldest first.
	Recent []Item
	// Summaries are the scored summary candidates, best first.
	Summaries []ScoredMemory
	// Semantic are the scored non-summary candidates, best first.
	Semantic []ScoredMemory
	// Facts are the pinned high-importance memories, which section 12.2 recalls on their own
	// rule and AC-MEM-003 names as the threshold's exception.
	Facts []ScoredMemory
	// UsedTokens is what the assembly spent, by this package's estimator.
	UsedTokens int
	// Truncated reports that a bound stopped the assembly before the channels were
	// exhausted, which is a fact the caller has to be able to see: a context that quietly
	// dropped half its recall looks the same as a small one.
	Truncated bool
	// SemanticSearched reports whether the semantic channel ran at all. It is false when no
	// embedding provider is configured, and it is a FIELD rather than an error for the reason
	// the service has three availability methods: "this project has no semantic search" is a
	// state, not a failure.
	SemanticSearched bool
}

// ScoredMemory is one recalled memory with the reasoning that put it there.
//
// The score and its parts travel because a user has to be able to ask why a memory was
// recalled, and "the fused score was 0.41" is not an answer by itself. Section 12.2 states
// the weights as a formula precisely so a reader can check one.
type ScoredMemory struct {
	Item       memory.MemoryItem
	Score      float64
	Similarity float64
	// Channel says which list this came in on, so a caller can group them.
	Channel string
	// Pinned reports that the threshold did not apply, because the memory is locked and
	// important.
	Pinned bool
}

// BuildMemoryContext assembles the memory layers of a prompt (AGENT_CONTRACTS section 12.1).
//
// The order of the work is the specification's, and each step is where it is for a reason:
//
//  1. RECENT reads the transcript. It is first because it is the channel that is always
//     available and because section 12.1's diagram puts recent messages at the top.
//  2. FACTS are the pinned high-importance memories, which are recalled regardless of
//     similarity: the exception AC-MEM-003 names, and the one channel that does not need an
//     embedding provider.
//  3. SUMMARIES and SEMANTIC come from one scored pool split by type. The summary channel is
//     separate because a summary answers a question about the past in one hop, which is
//     AC-MEM-005's "Summary 检索".
//
// The scope filter runs inside the store, before any scoring happens, which is section 12.2's
// "Scope filter 在评分前".
func (s *Service) BuildMemoryContext(ctx context.Context, request MemoryContextRequest) (MemoryContext, error) {
	if !s.Available() {
		return MemoryContext{}, UnavailableError()
	}
	if err := request.Scope.Validate(); err != nil {
		return MemoryContext{}, err
	}
	budget := request.TokenBudget
	if budget <= 0 {
		budget = DefaultContextTokenBudget
	}
	if budget > MaxContextTokenBudget {
		budget = MaxContextTokenBudget
	}
	// THE THRESHOLD DEFAULTS, and the first version forgot to. It clamped a negative to zero and
	// passed a zero straight through, so PassesThreshold's `Similarity >= 0` admitted everything
	// the vector search returned: the constant below existed, was documented as the reason the
	// criterion would not be satisfied "only in the tests that set it", and was read by nothing on
	// this path. That is literally "无条件返回低相关结果", which PRD FR-120 forbids.
	threshold := request.Threshold
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	result := MemoryContext{
		Recent:    []Item{},
		Summaries: []ScoredMemory{},
		Semantic:  []ScoredMemory{},
		Facts:     []ScoredMemory{},
	}
	excluded := map[string]bool{}
	for _, id := range request.ExcludeMemoryIDs {
		excluded[strings.TrimSpace(id)] = true
	}

	// The Recent channel. Its failure is reported rather than swallowed: a caller that cannot
	// read the conversation it is continuing has to decide what to do, and this package must
	// not decide for it.
	recent, err := s.BuildRecent(ctx, RecallRequest{Scope: request.Scope, Limit: RecentWindow})
	if err != nil {
		return MemoryContext{}, err
	}
	used := 0
	for _, item := range recent {
		cost := EstimateTokens(item.Content)
		if used+cost > budget {
			result.Truncated = true
			break
		}
		used += cost
		result.Recent = append(result.Recent, item)
	}
	if !s.StorageAvailable() {
		// The transcript half still worked, so the result carries what it has and the caller
		// can see that the store-backed channels are absent. That is more honest than an
		// error, which would discard the recent window it did manage to read.
		result.UsedTokens = used
		return result, nil
	}

	pinned, err := s.pinnedCandidates(ctx, request.Scope, excluded)
	if err != nil {
		return MemoryContext{}, err
	}
	result.Facts = pinned

	// THE STORE'S OWN RECENT WINDOW, which section 12.2 lists as its first candidate:
	// "Recent 未摘要消息".
	//
	// It is here because its ABSENCE made the store unreachable for the common case. A memory that
	// is not pinned, not summarised and not embedded appears in no other channel: the transcript
	// channel reads agent_messages, the semantic channel needs an embedding provider, and the
	// summary channel needs a summary. So a project with no embedding provider configured — which
	// is every project until someone configures one — could store a conversation and recall none
	// of it, and FR-120's "关键词降级" promise was empty for exactly the case it exists for.
	// The wiring test found it: it wrote a memory through the bridge and read back nothing.
	//
	// The two lists are DEDUPED by message identifier, because a turn the runtime recorded reaches
	// this function twice: once as a transcript row and once as the memory that cites it. A prompt
	// carrying the same sentence twice would look like a conversation in which somebody repeated
	// themselves, which is a worse failure than a shorter context.
	seenMessages := map[string]bool{}
	for _, item := range result.Recent {
		seenMessages[item.MessageID] = true
	}
	recentMemories, err := s.recentMemories(ctx, request.Scope, excluded, seenMessages)
	if err != nil {
		return MemoryContext{}, err
	}
	result.Recent = append(result.Recent, recentMemories...)

	if s.SemanticAvailable(ctx, request.Scope.Project) && strings.TrimSpace(request.Query) != "" {
		scored, err := s.scoredCandidates(ctx, request.Scope, request.Query, threshold, excluded)
		if err != nil {
			return MemoryContext{}, err
		}
		result.SemanticSearched = true
		for _, candidate := range scored {
			if candidate.Pinned {
				// Already in the facts channel; a memory appears once.
				continue
			}
			if candidate.Item.Type == memory.TypeSummary {
				if len(result.Summaries) < DefaultContextSummaries {
					result.Summaries = append(result.Summaries, candidate)
				}
				continue
			}
			if len(result.Semantic) < DefaultContextSemantic {
				result.Semantic = append(result.Semantic, candidate)
			}
		}
	}

	// The budget is spent in a fixed channel order, so what survives a tight budget is the
	// same on every run rather than depending on which channel finished first. Facts first
	// because they are the memories the user pinned; semantic last because its membership
	// depends on a similarity number and a smaller context can best afford to lose it.
	for _, channel := range []struct {
		name  string
		items *[]ScoredMemory
	}{
		{"facts", &result.Facts},
		{"summaries", &result.Summaries},
		{"semantic", &result.Semantic},
	} {
		kept := make([]ScoredMemory, 0, len(*channel.items))
		for _, scored := range *channel.items {
			cost := EstimateTokens(scored.Item.Content)
			if used+cost > budget {
				result.Truncated = true
				break
			}
			used += cost
			kept = append(kept, scored)
		}
		*channel.items = kept
	}
	// The count bound is section 12.1's other limit, and it is applied after the budget so a
	// generous budget cannot produce a context larger than the prompt layer memory is allowed
	// to occupy. Semantic is trimmed first, then the oldest recent turns.
	if total := len(result.Recent) + len(result.Facts) + len(result.Summaries) + len(result.Semantic); total > MaxContextItems {
		overflow := total - MaxContextItems
		for overflow > 0 && len(result.Semantic) > 0 {
			result.Semantic = result.Semantic[:len(result.Semantic)-1]
			overflow--
		}
		for overflow > 0 && len(result.Recent) > 0 {
			result.Recent = result.Recent[1:]
			overflow--
		}
		result.Truncated = true
	}
	result.UsedTokens = used
	return result, nil
}

// recentMemories returns the store's recent unsummarised memories for a scope.
//
// The window is the service's own RecentWindow, and the filter is `summarized = 0`: a memory a
// summary already covers is reachable through the summary channel, and repeating it here would
// spend the budget twice on the same content.
//
// They come back as Item rather than ScoredMemory because the recent channel is not scored: section
// 12.2's fusion is for the semantic candidates, and a recency ordering is already what this window
// is. Rendering them as Items is also what lets them sit in the same list as the transcript's rows,
// which is what makes the dedupe above a comparison of like things.
func (s *Service) recentMemories(ctx context.Context, scope Scope, excluded map[string]bool, seenMessages map[string]bool) ([]Item, error) {
	if !s.StorageAvailable() {
		return nil, nil
	}
	items, err := s.items.ListItems(ctx, MemoryListFilter{
		ProjectID: scope.Project,
		Types:     []memory.MemoryType{memory.TypeEpisodic},
		Limit:     RecentWindow,
	})
	if err != nil {
		return nil, err
	}
	recent := make([]Item, 0, len(items))
	for _, item := range items {
		if excluded[item.ID] || item.Deleted() || item.Summarized {
			continue
		}
		// The scope narrows by agent when the caller named one, and an episode the memory belongs
		// to must not leak into another episode's window. A blank part on either side widens, which
		// is the same rule the store's own queries follow.
		if scope.AgentKey != "" && item.Scope.AgentKey != "" && item.Scope.AgentKey != scope.AgentKey {
			continue
		}
		if scope.Episode != "" && item.Scope.Episode != "" && item.Scope.Episode != scope.Episode {
			continue
		}
		if seenMessages[item.SourceID] {
			continue
		}
		recent = append(recent, Item{
			MessageID:  item.SourceID,
			Role:       item.Role,
			Content:    item.Content,
			Provenance: "memory:" + item.ID,
			CreatedAt:  item.CreatedAt,
		})
	}
	// Oldest first, which is the order the rest of the channel is in and the order a prompt reads.
	for left, right := 0, len(recent)-1; left < right; left, right = left+1, right-1 {
		recent[left], recent[right] = recent[right], recent[left]
	}
	return recent, nil
}

// scoredCandidates runs the vector search and the fusion.
func (s *Service) scoredCandidates(ctx context.Context, scope Scope, query string, threshold float64, excluded map[string]bool) ([]ScoredMemory, error) {
	embedding, err := s.embedder.Embed(ctx, scope.Project, EmbeddingRequest{Texts: []string{query}})
	if err != nil {
		return nil, err
	}
	if len(embedding.Vectors) != 1 {
		return nil, memory.StorageError("The embedding provider returned the wrong number of vectors.", nil)
	}
	queryVector := memory.Normalize(embedding.Vectors[0])
	hits, err := s.vectors.Search(ctx, scope, queryVector, SearchOptions{
		Model: embedding.Model, Version: embedding.Version, Limit: DefaultCandidates,
	})
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}
	byID := map[string]float64{}
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		byID[hit.ID] = hit.Similarity
		ids = append(ids, hit.ID)
	}
	items, err := s.itemsByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	now := s.now()
	candidates := make([]memory.Candidate, 0, len(items))
	for _, item := range items {
		if excluded[item.ID] || item.Deleted() {
			continue
		}
		candidates = append(candidates, memory.Candidate{Item: item, Similarity: byID[item.ID]})
	}
	selected := memory.Select(candidates, memory.Selection{
		Threshold: threshold, TopK: MaxCandidates, Now: now, HalfLife: memory.DefaultRecencyHalfLife,
	})
	scored := make([]ScoredMemory, 0, len(selected))
	for _, candidate := range selected {
		scored = append(scored, ScoredMemory{
			Item:       candidate.Item,
			Score:      memory.Score(candidate, now, memory.DefaultRecencyHalfLife),
			Similarity: candidate.Similarity,
			Channel:    "semantic",
			Pinned:     candidate.Item.IsLockedHighImportance(),
		})
	}
	return scored, nil
}

// pinnedCandidates returns the locked high-importance memories of a scope.
//
// This is AC-MEM-003's "locked high-importance memory 走独立规则" as its own read: no query,
// no vector, no threshold. It runs whether or not the semantic channel can, which is what
// makes the rule independent rather than an exception inside the similarity path.
//
// The scope's EPISODE does not narrow it. A user pins a memory because it matters, and the
// pin is theirs rather than the conversation's; a pinned project-level setting that vanished
// in every other episode would be a pin that did not work.
func (s *Service) pinnedCandidates(ctx context.Context, scope Scope, excluded map[string]bool) ([]ScoredMemory, error) {
	items, err := s.items.PinnedItems(ctx, scope.Project, DefaultCandidates)
	if err != nil {
		return nil, err
	}
	now := s.now()
	pinned := []ScoredMemory{}
	for _, item := range items {
		if excluded[item.ID] || item.Deleted() || !item.IsLockedHighImportance() {
			continue
		}
		candidate := memory.Candidate{Item: item}
		pinned = append(pinned, ScoredMemory{
			Item:    item,
			Score:   memory.Score(candidate, now, memory.DefaultRecencyHalfLife),
			Channel: "facts",
			Pinned:  true,
		})
	}
	if len(pinned) > DefaultContextSemantic {
		pinned = pinned[:DefaultContextSemantic]
	}
	return pinned, nil
}

// itemsByID reads several memories, skipping the ones that are gone.
//
// A skipped row is a HOLE rather than an error, and that is the deliberate choice: the vector
// index and the item table are two writes, so a crash between them leaves a vector pointing
// at nothing. Refusing the whole recall for it would make one orphan break every query, while
// skipping it costs one candidate.
func (s *Service) itemsByID(ctx context.Context, ids []string) ([]memory.MemoryItem, error) {
	items := make([]memory.MemoryItem, 0, len(ids))
	for _, id := range ids {
		item, err := s.items.GetItem(ctx, id)
		if err != nil {
			if notFound(err) {
				continue
			}
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// DeepRecallRequest is AGENT_CONTRACTS section 12.3's tool input.
type DeepRecallRequest struct {
	Scope Scope
	Query string
	// MaxSummaries and MaxRawMessages are the section's own bounds. Zero uses the defaults;
	// the ceilings are MaxDeepRecallSummaries and MaxDeepRecallMessages.
	MaxSummaries   int
	MaxRawMessages int
	// Threshold is the summary similarity floor. Zero uses DefaultThreshold.
	Threshold float64
}

// DeepRecallResult is the section's flow, made visible.
//
// Each stage of "Summary vector candidates → threshold → rerank → selected summary IDs →
// source memory IDs → load original messages/artifact refs" is a field, because a user has to
// be able to see WHICH summaries were found and WHY the answer includes what it does.
// AC-MEM-005's "返回来源" is Provenance, and the rerank's ordering is Summaries.
type DeepRecallResult struct {
	// Query is the question that was asked, so a rendering can quote it.
	Query string
	// Summaries are the summaries that passed the threshold, best first.
	Summaries []ScoredMemory
	// Messages are the original memories the selected summaries cite, in order.
	Messages []memory.MemoryItem
	// Provenance is where each message came from, one entry per message.
	Provenance []Provenance
	// UsedTokens is what the result costs, by this package's estimator.
	UsedTokens int
	// Truncated reports that a bound stopped the walk.
	Truncated bool
}

// Provenance is one recalled memory's source, for a reader that has to check it.
type Provenance struct {
	MemoryID string
	// MessageID is the transcript row the memory came from, when it has one.
	MessageID string
	// AgentKey, Role and CreatedAt are the memory's own attribution: AC-MEM-004's
	// "role/agent/time 保留" applied to what deep recall hands back.
	AgentKey  string
	Role      string
	CreatedAt time.Time
	// SummarizedBy names the summary this memory was reached through, which is the hop
	// AC-MEM-005 asks to be visible.
	SummarizedBy string
}

// DeepRecall walks from a question to the original messages (AGENT_CONTRACTS section 12.3).
//
// It is a SEPARATE CALL rather than a wider BuildMemoryContext, and that separation is
// AC-MEM-005's "不每轮自动调用": the automatic recall is the context builder, which never
// calls this. A model reaches it through the `memory.deep_recall` tool, which the skills must
// name, so the cost is paid only when the agent says it needs history.
//
// The five steps are the section's, in its order, and each is a real step rather than a
// restatement: the threshold prunes BEFORE the rerank (otherwise the rerank would have to
// score everything), the rerank orders the survivors, and only the selected summaries'
// sources are loaded — which bounds the result by construction rather than by a count applied
// at the end.
func (s *Service) DeepRecall(ctx context.Context, request DeepRecallRequest) (DeepRecallResult, error) {
	result := DeepRecallResult{
		Query:      strings.TrimSpace(request.Query),
		Summaries:  []ScoredMemory{},
		Messages:   []memory.MemoryItem{},
		Provenance: []Provenance{},
	}
	if !s.StorageAvailable() {
		return result, storageUnavailable()
	}
	if err := request.Scope.Validate(); err != nil {
		return result, err
	}
	if result.Query == "" {
		return result, memory.InvalidError("A deep recall needs a question.")
	}
	if !s.SemanticAvailable(ctx, request.Scope.Project) {
		// Refused rather than silently answered with the recent window: a caller that asked
		// for history and got the last twelve turns would conclude the history was not there.
		return result, memory.StorageError("No embedding provider is configured for this project, so summaries cannot be searched.", nil)
	}
	maxSummaries := clampCount(request.MaxSummaries, MaxDeepRecallSummaries, MaxDeepRecallSummaries)
	maxMessages := clampCount(request.MaxRawMessages, MaxDeepRecallMessages, MaxDeepRecallMessages)
	threshold := request.Threshold
	if threshold <= 0 {
		threshold = DefaultThreshold
	}

	scored, err := s.scoredCandidates(ctx, request.Scope.EpisodeOnly(), result.Query, threshold, map[string]bool{})
	if err != nil {
		return result, err
	}
	// Only summaries answer a question about the past in one hop; the rest of the semantic
	// channel is what the ordinary context builder is for.
	//
	// A pinned high-importance memory is kept here even though it is not a summary: section
	// 12.2 gives that channel its own rule precisely so it survives a threshold, and dropping
	// it from a deep recall would be the one place the rule did not apply.
	answerable := make([]ScoredMemory, 0, len(scored))
	for _, candidate := range scored {
		if candidate.Item.Type == memory.TypeSummary || candidate.Pinned {
			answerable = append(answerable, candidate)
		}
	}
	// Step three: the rerank.
	//
	// The rerank is a STATED rule rather than a model call. What is measurable without a model
	// is how much of the query's own wording the summary's TEXT accounts for — and for a
	// summary written by extraction that is a real signal rather than a proxy, because the
	// summary quotes its sources' words. The vector similarity remains the major term, so the
	// rerank cannot promote a summary the query is not about; see rerankLexicalWeight.
	reranked := rerankSummaries(answerable, result.Query)
	if len(reranked) > maxSummaries {
		reranked = reranked[:maxSummaries]
		result.Truncated = true
	}
	result.Summaries = reranked

	// Steps four and five: the sources, then the original rows, bounded.
	seen := map[string]bool{}
	for _, summary := range result.Summaries {
		_, memories, err := s.ListSummarySources(ctx, summary.Item.ID)
		if err != nil {
			return result, err
		}
		for _, original := range memories {
			if seen[original.ID] {
				continue
			}
			if len(result.Messages) >= maxMessages {
				result.Truncated = true
				break
			}
			seen[original.ID] = true
			result.Messages = append(result.Messages, original)
			result.Provenance = append(result.Provenance, Provenance{
				MemoryID:     original.ID,
				MessageID:    original.SourceID,
				AgentKey:     original.AgentKey,
				Role:         string(original.Role),
				CreatedAt:    original.CreatedAt,
				SummarizedBy: summary.Item.ID,
			})
			result.UsedTokens += EstimateTokens(original.Content)
		}
		if len(result.Messages) >= maxMessages {
			break
		}
	}
	return result, nil
}

// rerankSummaries reorders summaries using how much of the query their text accounts for.
//
// The score is the vector similarity plus a bounded lexical term, and the bound is what keeps
// this a RERANK rather than a second search: the lexical term is at most rerankLexicalWeight,
// so it can move a summary past another that is close in similarity but cannot promote one
// the vector search found irrelevant.
func rerankSummaries(candidates []ScoredMemory, query string) []ScoredMemory {
	if len(candidates) == 0 {
		return candidates
	}
	terms := queryTerms(query)
	reranked := make([]ScoredMemory, len(candidates))
	copy(reranked, candidates)
	if len(terms) > 0 {
		for index := range reranked {
			content := strings.ToLower(reranked[index].Item.Content)
			overlap := 0
			for _, term := range terms {
				if strings.Contains(content, term) {
					overlap++
				}
			}
			reranked[index].Score = reranked[index].Similarity +
				rerankLexicalWeight*float64(overlap)/float64(len(terms))
		}
	}
	for index := 1; index < len(reranked); index++ {
		current := reranked[index]
		position := index - 1
		for position >= 0 {
			better := current.Score > reranked[position].Score
			if !better && current.Score == reranked[position].Score {
				better = current.Item.ID < reranked[position].Item.ID
			}
			if !better {
				break
			}
			reranked[position+1] = reranked[position]
			position--
		}
		reranked[position+1] = current
	}
	return reranked
}

// rerankLexicalWeight bounds the rerank's lexical term.
const rerankLexicalWeight = 0.2

// queryTerms splits a question into the terms the rerank counts.
//
// Han characters are single terms, because a Chinese question has no spaces to split on and a
// character is the smallest unit a substring test can use. A minimum length of two applies to
// the non-Han runs, so "a" does not match every summary that contains the letter.
func queryTerms(query string) []string {
	terms := []string{}
	current := strings.Builder{}
	flush := func() {
		if current.Len() >= 2 {
			terms = append(terms, current.String())
		}
		current.Reset()
	}
	for _, symbol := range strings.ToLower(query) {
		if symbol >= 0x4E00 && symbol <= 0x9FFF {
			flush()
			terms = append(terms, string(symbol))
			continue
		}
		if symbol == ' ' || symbol == '\t' || symbol == '\n' || symbol == '\r' ||
			strings.ContainsRune("，。！？、,.!?;:;:()（）\"'“”", symbol) {
			flush()
			continue
		}
		current.WriteRune(symbol)
	}
	flush()
	return terms
}

// clampCount bounds a caller's count, defaulting an unset zero and capping the ceiling.
func clampCount(value, fallback, ceiling int) int {
	if value <= 0 {
		return fallback
	}
	if value > ceiling {
		return ceiling
	}
	return value
}

// clampWeight keeps a caller's score inside [0,1], defaulting an unset zero.
//
// The asymmetry is deliberate: a zero means "unset" for importance and confidence, because
// nothing in this build's pipeline has an opinion about either, and the domain's default is
// what keeps every turn comparable. A caller that means zero has nothing to remember.
func clampWeight(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
