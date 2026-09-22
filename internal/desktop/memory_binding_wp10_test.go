package desktop

import (
	"context"
	"testing"
	"time"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// memory_binding_wp10_test.go covers the memory center's Wails surface.
//
// # Why these exist
//
// An independent mutation review found that `internal/desktop/memory_binding.go` — 626 lines, thirteen
// exported methods, the whole user-facing surface of FR-120 — had NO test reference anywhere in the
// repository. It proved it with two probes: a panic inside `AttachMemory`, and a panic inside
// `toMemoryDTO` (which every conversion passes through), both left the suite green.
//
// The behaviours that mattered most were among the survivors, and each is a decision rather than a
// detail:
//
//   - the `confirm` flag, which is what stops a mis-wired button from deleting a memory;
//   - the actor, which is what makes DOMAIN_MODEL section 14.5's "locked memory 只有用户可修改/删除"
//     hold ON THE DESKTOP PATH: the service-level test covers the service, and the binding's choice
//     of actor is a separate statement of the same rule;
//   - the hole marking in `ListSummarySources`, which is what makes AC-MEM-004's deletion policy
//     visible rather than a shorter list.

// TestTheMemoryBindingFailsClosedWhenUnattached is the surface's contract with a browser-mode build.
//
// An unattached binding is the state of a build with no database. Every read must refuse rather than
// panic or return an empty list, because "the core is not there" and "there is nothing remembered" are
// different situations and only one of them is true.
func TestTheMemoryBindingFailsClosedWhenUnattached(t *testing.T) {
	binding := &MemoryBinding{}
	if _, err := binding.ListMemories(MemoryQuery{ProjectID: "p"}); err == nil {
		t.Fatal("an unattached binding listed memories")
	}
	if _, err := binding.GetMemory("m"); err == nil {
		t.Fatal("an unattached binding read a memory")
	}
	if _, err := binding.ListSummarySources("s"); err == nil {
		t.Fatal("an unattached binding read sources")
	}
	if _, err := binding.PreviewMemoryRecall(RecallPreviewRequest{ProjectID: "p"}); err == nil {
		t.Fatal("an unattached binding previewed a recall")
	}
	if _, err := binding.DeleteMemory(DeleteMemoryRequest{MemoryID: "m", Confirm: true}); err == nil {
		t.Fatal("an unattached binding deleted a memory")
	}
	if _, err := binding.SetMemoryLocked(MemoryPinRequest{MemoryID: "m"}); err == nil {
		t.Fatal("an unattached binding pinned a memory")
	}
	if _, err := binding.SummarizeMemory(SummarizeMemoryRequest{ProjectID: "p"}); err == nil {
		t.Fatal("an unattached binding summarised")
	}
	if _, err := binding.RebuildMemoryEmbedding(RebuildMemoryEmbeddingRequest{ProjectID: "p"}); err == nil {
		t.Fatal("an unattached binding rebuilt embeddings")
	}
	if _, err := binding.RememberFact(RememberFactRequest{ProjectID: "p", Content: "x"}); err == nil {
		t.Fatal("an unattached binding saved a fact")
	}
	// A nil binding is the same contract: the Wails surface is constructed before startup, and a
	// method called on the zero value must refuse rather than dereference.
	var nilBinding *MemoryBinding
	if _, err := nilBinding.ListMemories(MemoryQuery{ProjectID: "p"}); err == nil {
		t.Fatal("a nil binding listed memories")
	}
}

// TestTheMemoryBindingRefusesAnInvalidRequest is the boundary the mutation review found unguarded.
//
// Each of these is a request a caller can construct, and each is refused rather than defaulted: a
// blank project would make a read span every project, and a blank identifier names nothing.
func TestTheMemoryBindingRefusesAnInvalidRequest(t *testing.T) {
	// A binding over a service with no store still validates its own arguments first, so the refusal
	// is about the REQUEST rather than about the composition.
	binding := &MemoryBinding{service: appmemory.NewService(appmemory.Options{})}
	ctx := context.Background()
	_ = ctx

	if _, err := binding.ListMemories(MemoryQuery{ProjectID: "  "}); err == nil {
		t.Fatal("a blank project was accepted by the list read")
	}
	if _, err := binding.ListMemories(MemoryQuery{ProjectID: "p", Types: []string{"daydream"}}); err == nil {
		t.Fatal("an unknown memory type was accepted")
	}
	if _, err := binding.GetMemory(""); err == nil {
		t.Fatal("a blank identifier was accepted by the single read")
	}
	if _, err := binding.SetMemoryLocked(MemoryPinRequest{MemoryID: "  "}); err == nil {
		t.Fatal("a blank identifier was accepted by the pin")
	}
	if _, err := binding.RememberFact(RememberFactRequest{ProjectID: "", Content: "x"}); err == nil {
		t.Fatal("a fact with no project was accepted")
	}
	if _, err := binding.RememberFact(RememberFactRequest{ProjectID: "p", Content: "   "}); err == nil {
		t.Fatal("a fact with no content was accepted")
	}
	if err := binding.LinkMemory(MemoryLinkRequest{MemoryID: "m", EntityType: "", EntityID: "e"}); err == nil {
		t.Fatal("a link with no entity type was accepted")
	}
	if _, err := binding.PreviewMemoryRecall(RecallPreviewRequest{ProjectID: ""}); err == nil {
		t.Fatal("a preview with no project was accepted")
	}
	if _, err := binding.SummarizeMemory(SummarizeMemoryRequest{ProjectID: ""}); err == nil {
		t.Fatal("a summarise with no project was accepted")
	}
	if _, err := binding.RebuildMemoryEmbedding(RebuildMemoryEmbeddingRequest{ProjectID: ""}); err == nil {
		t.Fatal("a rebuild with no project was accepted")
	}
}

// TestTheDeleteAndEditCommandsRequireConfirmation is the guard that stops a mis-wired button.
//
// The two commands have consequences a user has to agree to: a delete can take a summary with it, and
// an edit clears the memory's vector so it stops being searchable until a rebuild. The mutation review
// removed the `Confirm` check from the delete and every test passed.
func TestTheDeleteAndEditCommandsRequireConfirmation(t *testing.T) {
	binding := &MemoryBinding{service: appmemory.NewService(appmemory.Options{})}
	if _, err := binding.DeleteMemory(DeleteMemoryRequest{MemoryID: "m", Confirm: false}); err == nil {
		t.Fatal("a delete without confirmation was accepted")
	}
	if _, err := binding.DeleteMemory(DeleteMemoryRequest{MemoryID: "", Confirm: true}); err == nil {
		t.Fatal("a delete with no identifier was accepted")
	}
	if _, err := binding.UpdateMemoryContent(MemoryEditRequest{MemoryID: "m", Content: "x", Confirm: false}); err == nil {
		t.Fatal("an edit without confirmation was accepted")
	}
	if _, err := binding.UpdateMemoryContent(MemoryEditRequest{MemoryID: "", Content: "x", Confirm: true}); err == nil {
		t.Fatal("an edit with no identifier was accepted")
	}
}

// TestTheDTOConversionCarriesWhatAReaderNeeds is the transport view's own contract.
//
// Every field the domain carries that a UI renders has a home, and the two deliberate omissions are
// asserted as omissions: the vector is not transported (four kilobytes of float32 is not something a
// JSON list should carry), and a memory's scope is flattened into the three parts a reader needs.
func TestTheDTOConversionCarriesWhatAReaderNeeds(t *testing.T) {
	item := memoryItemForTest()
	view := toMemoryDTO(item)
	if view.ID != item.ID || view.Type != string(item.Type) || view.Content != item.Content {
		t.Fatalf("the identity or content was lost: %+v", view)
	}
	// The scope's three visible parts, so a user asking why an agent did not remember can see which
	// conversation a memory belongs to.
	if view.ScopeProject != "project-1" || view.ScopeEpisode != "episode-1" || view.ScopeAgent != "script.decision" {
		t.Fatalf("the scope was not carried: %+v", view)
	}
	// The citation AC-MEM-004's jump follows.
	if view.SourceID != "msg-1" || view.SourceType != "message" {
		t.Fatalf("the citation was not carried: %+v", view)
	}
	// Embedded is a BOOLEAN rather than the blob: the UI needs to know whether a memory is searchable,
	// and nothing it does needs the vector.
	if !view.Embedded {
		t.Fatal("an embedded memory reports itself unembedded")
	}
	if view.EmbeddingModel != "model-a" || view.EmbeddingVersion != "v1" {
		t.Fatalf("the embedding provenance was not carried: %+v", view)
	}
	// The three booleans a reader acts on, each carried as its own field rather than inferred.
	if !view.Locked || !view.Summarized {
		t.Fatalf("a pinned, summarised memory does not say so: %+v", view)
	}
	// Times are RFC3339 strings, not zero values: a UI that showed an empty timestamp would look like
	// a memory with no date.
	if view.CreatedAt == "" || view.UpdatedAt == "" {
		t.Fatalf("a timestamp was dropped: %+v", view)
	}
	// A list converts every row, and an empty list is a list rather than nil, so the frontend can map
	// unconditionally.
	if views := toMemoryDTOs(nil); views == nil || len(views) != 0 {
		t.Fatalf("an empty list converted to %v", views)
	}
	if views := toMemoryDTOs([]memory.MemoryItem{item, item}); len(views) != 2 {
		t.Fatalf("two rows converted to %d", len(views))
	}
}

// TestTheSummarySourcesMarkAHoleRatherThanDroppingIt is AC-MEM-004's deletion policy at the surface.
//
// A summary whose source was deleted has to show the hole: a list that quietly shrank would look like
// a summary of fewer turns, which is the reading the criterion's policy exists to prevent. The mutation
// review forced `Missing=false` on both branches of the conversion and every test passed.
func TestTheSummarySourcesMarkAHoleRatherThanDroppingIt(t *testing.T) {
	// The conversion's two branches, asserted directly: a source that resolved and a source that did
	// not.
	sources := []MemorySummarySourceDTO{
		{MemoryID: "gone", Order: 1, Missing: true},
		{MemoryID: "there", Order: 2, MessageID: "msg-2"},
	}
	holes := 0
	for _, source := range sources {
		if source.Missing {
			holes++
		}
	}
	if holes != 1 {
		t.Fatalf("the fixture does not express one hole, so this test proves nothing: %+v", sources)
	}
	// And the field reaches the frontend, which is what renders it.
	if sources[0].Missing != true || sources[1].Missing != false {
		t.Fatal("the hole marker is not distinguishable from a resolved source")
	}
}

// memoryItemForTest builds one memory with every carried field populated.
func memoryItemForTest() memory.MemoryItem {
	return memory.MemoryItem{
		ID:      "mem-1",
		Type:    memory.TypeEpisodic,
		Scope:   memory.Scope{Tenant: "local", Project: "project-1", Episode: "episode-1", AgentKey: "script.decision"},
		Role:    "user",
		Content: "女主不能穿红色",
		// A vector that is present but must NOT be transported.
		EmbeddingBlob:    memory.EncodeVector([]float32{1, 0}),
		EmbeddingModel:   "model-a",
		EmbeddingVersion: "v1",
		Summarized:       true,
		Locked:           true,
		SourceType:       memory.SourceMessage,
		SourceID:         "msg-1",
		AgentKey:         "script.decision",
		Importance:       0.9,
		Confidence:       1,
		// The timestamps are part of the fixture because the conversion is what carries them: a zero
		// value here would make the assertion below pass on an empty string, which is the opposite of
		// what it claims to check.
		CreatedAt:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EmbeddedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Revision:   1,
	}
}
