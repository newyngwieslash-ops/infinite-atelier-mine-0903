package memory

import (
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// recall_scope_test.go covers `keepInRecallScope`, the filter that makes the widened search safe.
//
// # Why this is a direct test rather than one driven through DeepRecall
//
// The filter was introduced so a project-scoped search could find the project RUNG — a summary stored
// with an empty episode — without handing one agent another's memories. A mutation that made it keep
// everything SURVIVED a whole-suite run, because the scenario that exercises it end to end found no
// foreign rows to leak: they were filtered out by the threshold and the rerank before the filter was
// consulted at all. A property that a pipeline happens to protect by accident is a property with no
// test, so this one states the rule directly over the function that owns it.
//
// The cases are the three things the filter must decide, and each corresponds to a leak class:
func TestKeepInRecallScopeDecidesTheThreeCases(t *testing.T) {
	caller := Scope{Tenant: "local", Project: "p", Episode: "ep-1", AgentKey: "agent-1"}
	item := func(id string, scope Scope) ScoredMemory {
		return ScoredMemory{Item: memory.MemoryItem{ID: id, Scope: scope}}
	}

	candidates := []ScoredMemory{
		// The caller's own conversation: kept.
		item("mine", Scope{Tenant: "local", Project: "p", Episode: "ep-1", AgentKey: "agent-1"}),
		// The PROJECT rung: empty episode AND empty agent. This is the row the widening exists to
		// reach, and dropping it would make the project level unreachable again.
		item("project", Scope{Tenant: "local", Project: "p"}),
		// Another AGENT in the same episode: a leak.
		item("other-agent", Scope{Tenant: "local", Project: "p", Episode: "ep-1", AgentKey: "agent-2"}),
		// Another EPISODE for the same agent: a leak.
		item("other-episode", Scope{Tenant: "local", Project: "p", Episode: "ep-2", AgentKey: "agent-1"}),
		// A DIFFERENT PROJECT entirely: the leak section 14.5 forbids.
		item("other-project", Scope{Tenant: "local", Project: "q", Episode: "ep-1", AgentKey: "agent-1"}),
	}

	kept := keepInRecallScope(candidates, caller)
	keptIDs := map[string]bool{}
	for _, candidate := range kept {
		keptIDs[candidate.Item.ID] = true
	}
	for _, want := range []string{"mine", "project"} {
		if !keptIDs[want] {
			t.Fatalf("%q was filtered out, and it is in scope: %+v", want, kept)
		}
	}
	for _, unwanted := range []string{"other-agent", "other-episode", "other-project"} {
		if keptIDs[unwanted] {
			t.Fatalf("%q survived the filter, and it is a leak: %+v", unwanted, kept)
		}
	}
	if len(kept) != 2 {
		t.Fatalf("the filter kept %d candidates, want 2: %+v", len(kept), kept)
	}
}

// TestMergeScoredKeepsTheBetterScore pins the merge's one decision.
//
// The two searches see the same row when a caller's own episode holds a project-level summary's
// children, and the score must not depend on which search happened to run first — so the merge keeps
// the HIGHER similarity rather than the first it saw. A merge that kept the first would make a
// row's reported relevance depend on call order, which is a number a rerank and a threshold both
// read.
func TestMergeScoredKeepsTheBetterScore(t *testing.T) {
	low := ScoredMemory{Item: memory.MemoryItem{ID: "a"}, Similarity: 0.4}
	high := ScoredMemory{Item: memory.MemoryItem{ID: "a"}, Similarity: 0.9}
	other := ScoredMemory{Item: memory.MemoryItem{ID: "b"}, Similarity: 0.5}

	merged := mergeScored([]ScoredMemory{low, other}, []ScoredMemory{high})
	if len(merged) != 2 {
		t.Fatalf("the merge returned %d rows, want 2: %+v", len(merged), merged)
	}
	for _, candidate := range merged {
		if candidate.Item.ID == "a" && candidate.Similarity != 0.9 {
			t.Fatalf("the merge kept the similarity %v rather than the better one", candidate.Similarity)
		}
	}
	// And the other direction, so the answer does not depend on which list came first.
	reversed := mergeScored([]ScoredMemory{high}, []ScoredMemory{low, other})
	for _, candidate := range reversed {
		if candidate.Item.ID == "a" && candidate.Similarity != 0.9 {
			t.Fatalf("with the lists reversed the merge kept %v", candidate.Similarity)
		}
	}
}
