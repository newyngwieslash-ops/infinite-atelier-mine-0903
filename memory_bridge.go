package main

import (
	"context"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// memory_bridge.go adapts the memory service to the runtime's two-method memory port.
//
// # Why a bridge and not the service itself
//
// The runtime declares its port in its own terms — a scope of three parts, a recall item with a
// provenance — because it must not import an application that imports it. The service's terms are
// DOMAIN_MODEL section 14.4's six parts and its own item type. Something has to translate, and the
// translation is where two decisions live that neither end should make alone:
//
//   - WHICH SCOPE a run writes under. The service takes six parts; the runtime knows three and
//     leaves the tenant to the service. Getting this wrong is the leak section 14.5 forbids, so it
//     happens in one function, here.
//   - WHETHER A MEMORY CAN BE WRITTEN AT ALL. The service needs a store; a build without one must
//     report nothing rather than half-write, and a RECALL failure must not fail a run whose work is
//     already validated.
//
// Both ends are therefore fail-soft by design and honest about it: a run in a build with no store
// behaves exactly as it did before the store existed, and a run in a build with one gets memory.
type memoryBridge struct {
	service *appmemory.Service
}

// NewMemoryBridge builds the bridge. A nil service produces a bridge whose every call is a no-op,
// which is what a build with no memory store needs from the runtime's side.
func newMemoryBridge(service *appmemory.Service) *memoryBridge {
	return &memoryBridge{service: service}
}

// Recall returns the memory layer for one run, across every channel section 12.1 names.
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
}

// Remember records one turn as an episodic memory.
//
// The memory's own scope carries the AGENT KEY from the run, because that is what the runtime's
// scope parts name and what the recall filters on; a memory written under a different scope than
// the one it would be recalled from is a memory that never comes back.
//
// The content is the turn as the model saw it or produced it, which for the user's turn is the
// UNTRUSTED marker's host rather than the marker itself: `Invocation.UserMessage` is the text and
// the marker is added by the prompt assembly, so what is stored is the person's own words.
func (b *memoryBridge) Remember(ctx context.Context, message agentruntime.MemoryMessage) error {
	if b == nil || b.service == nil || !b.service.StorageAvailable() {
		return nil
	}
	role := agent.MessageRole(message.Role)
	if !agent.IsValidMessageRole(role) {
		role = agent.MessageAssistant
	}
	_, err := b.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope:      appmemory.ScopeFor(message.ProjectID, message.EpisodeID, message.AgentKey),
		Message:    message.Content,
		MessageID:  strings.TrimSpace(message.MessageID),
		Role:       role,
		AgentKey:   strings.TrimSpace(message.AgentKey),
		AgentRunID: strings.TrimSpace(message.AgentRunID),
	})
	return err
}
