import io
path = "internal/application/memory/service.go"
text = io.open(path, encoding="utf-8", newline="").read()

old = """	pinned, err := s.pinnedCandidates(ctx, request.Scope, excluded)
	if err != nil {
		return MemoryContext{}, err
	}
	result.Facts = pinned
"""
new = """	pinned, err := s.pinnedCandidates(ctx, request.Scope, excluded)
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
"""
assert old in text, "recent anchor"
text = text.replace(old, new, 1)

old = """// scoredCandidates runs the vector search and the fusion."""
new = """// recentMemories returns the store's recent unsummarised memories for a scope.
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

// scoredCandidates runs the vector search and the fusion."""
assert old in text, "scored anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
