import io
path = "memory_bridge.go"
text = io.open(path, encoding="utf-8", newline="").read()

old = '''// Recall returns the recent turns of a run's scope.
//
// The scope is the service's own resolution, so the recall and the write cannot disagree about
// which conversation a turn belongs to: `ScopeFor` is the single place that mapping is written.
//
// A failure returns an empty list rather than an error, and the choice is the runtime's to make
// rather than this bridge's. The runtime calls this BEFORE the model does any work, so the thing a
// failure could damage is the memory layer of a prompt; failing the run instead would make a
// missing store fatal to every stage, which is exactly the coupling the optional port avoids. What
// keeps the failure from being silent in the build that DOES have a store is that the store's own
// errors surface on every user command, where a person can act on them.
func (b *memoryBridge) Recall(ctx context.Context, scope agentruntime.ScopePartsRequest, excludeMessageID string, limit int) ([]agentruntime.MemoryRecallItem, error) {
	if b == nil || b.service == nil || !b.service.Available() {
		return nil, nil
	}
	items, err := b.service.BuildRecent(ctx, appmemory.RecallRequest{
		Scope:            appmemory.ScopeFor(scope.ProjectID, scope.EpisodeID, scope.AgentKey),
		ExcludeMessageID: excludeMessageID,
		Limit:            limit,
	})
	if err != nil {
		return nil, err
	}
	recalled := make([]agentruntime.MemoryRecallItem, 0, len(items))
	for _, item := range items {
		recalled = append(recalled, agentruntime.MemoryRecallItem{
			Content:    item.Content,
			MessageID:  item.MessageID,
			Provenance: item.Provenance,
			Role:       string(item.Role),
		})
	}
	return recalled, nil
}'''

new = '''// Recall returns the memory layer for one run, across every channel section 12.1 names.
//
// # Why the whole context builder and not just the recent window
//
// The narrow reading of this port is "the last N turns". That is what WP-07 shipped and it is not
// what section 12.1 asks the memory layer to be: its output shape has FOUR lists — recent,
// summaries, semantic and facts — because those answer four different questions about the past.
// A layer carrying only the transcript tail would leave a memory summarised an hour ago, or a
// setting the user pinned, invisible to the run that most needs it.
//
// So this calls the multi-channel builder and flattens its channels in the order that a prompt
// should read them: the pinned facts first (they are the user's own statement of what matters),
// then the summaries (the past in one hop), then the semantic candidates, then the recent tail.
// The recent tail is last because it is the part the model is most likely to have seen already.
//
// # The query is the run's own task
//
// The semantic channel needs something to be similar TO, and the thing the run is about to do is
// the only honest query available at this point: the user's message for a decision stage, the
// task statement for an execution one. `UserMessage` is preferred because it is the person's own
// words rather than a stage's instructions.
//
// # The scope is the service's own resolution
//
// `ScopeFor` is the single place the run-to-scope mapping is written, so the recall and the write
// cannot disagree about which conversation a turn belongs to.
//
// A failure returns an empty list rather than an error, and the choice is this bridge's to make on
// the runtime's behalf. The runtime calls this BEFORE the model does any work, so the thing a
// failure could damage is the memory layer of a prompt; failing the run instead would make a
// missing store fatal to every stage, which is exactly the coupling the optional port avoids. What
// keeps a failure from being silent in the build that DOES have a store is that the store's own
// errors surface on every user command, where a person can act on them.
func (b *memoryBridge) Recall(ctx context.Context, scope agentruntime.ScopePartsRequest, excludeMessageID string, limit int) ([]agentruntime.MemoryRecallItem, error) {
	if b == nil || b.service == nil || !b.service.Available() {
		return nil, nil
	}
	memoryScope := appmemory.ScopeFor(scope.ProjectID, scope.EpisodeID, scope.AgentKey)
	context, err := b.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{
		Scope:            memoryScope,
		Query:            scope.Query,
		ExcludeMemoryIDs: nil,
	})
	if err != nil {
		// The multi-channel build failed, which for a service with a transcript port means the
		// transcript read itself failed. The narrow fallback is not a second attempt at the same
		// thing: it is the one channel that might still work, and returning nothing when something
		// can still be recalled would be a worse answer than a smaller layer.
		items, recentErr := b.service.BuildRecent(ctx, appmemory.RecallRequest{Scope: memoryScope, Limit: limit})
		if recentErr != nil {
			return nil, recentErr
		}
		return recentRecall(items), nil
	}
	recalled := make([]agentruntime.MemoryRecallItem, 0,
		len(context.Facts)+len(context.Summaries)+len(context.Semantic)+len(context.Recent))
	for _, scored := range context.Facts {
		recalled = append(recalled, scoredRecall(scored))
	}
	for _, scored := range context.Summaries {
		recalled = append(recalled, scoredRecall(scored))
	}
	for _, scored := range context.Semantic {
		recalled = append(recalled, scoredRecall(scored))
	}
	recalled = append(recalled, recentRecall(context.Recent)...)
	return recalled, nil
}

// recentRecall renders the transcript channel.
func recentRecall(items []appmemory.Item) []agentruntime.MemoryRecallItem {
	recalled := make([]agentruntime.MemoryRecallItem, 0, len(items))
	for _, item := range items {
		recalled = append(recalled, agentruntime.MemoryRecallItem{
			Content:    item.Content,
			MessageID:  item.MessageID,
			Provenance: item.Provenance,
			Role:       string(item.Role),
		})
	}
	return recalled
}

// scoredRecall renders one memory-store candidate.
//
// The provenance names the memory and the transcript row it came from, because section 12.2
// requires recalled context to carry its source and a reader checking the layer has to be able to
// get from a line of the prompt back to the row that produced it.
func scoredRecall(scored appmemory.ScoredMemory) agentruntime.MemoryRecallItem {
	provenance := "memory:" + scored.Item.ID
	if scored.Item.SourceID != "" {
		provenance += " message:" + scored.Item.SourceID
	}
	return agentruntime.MemoryRecallItem{
		Content:    scored.Item.Content,
		MessageID:  scored.Item.SourceID,
		Provenance: provenance,
		Role:       string(scored.Item.Role),
	}
}'''
assert old in text, "recall anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
