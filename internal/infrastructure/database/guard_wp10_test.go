package database

import (
	"context"
	"testing"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// guard_wp10_test.go covers the STORE'S OWN refusals, which the suite could not see.
//
// # Why these exist
//
// An independent mutation review proved the gap rather than asserting it: it added temporary probes
// that called the repository's methods DIRECTLY, and every one of the six mutations below was killed
// by a probe while the full suite stayed green. The reason is structural — every WP-10 test drives the
// application SERVICE, and the service validates before it calls the store, so the store's own guards
// are never the thing that refuses.
//
// That matters because the store is a port. `appmemory.Repository` is an interface with a compile-time
// assertion that this type satisfies it, which means the store is callable by anything that holds it —
// including a future caller that composes a service differently. A guard nothing exercises is a guard
// that can be deleted without a test noticing, which is exactly what the review did.
//
// The tests below are the probes, kept.

// TestWP10GuardEmptyProjectIsRefusedByTheStore covers the reads that take a project.
//
// An empty project is not a wildcard. Every one of these methods refuses it rather than returning
// every row in the database, and the refusal is what makes "a query must name its project" a property
// of the STORE rather than of the service in front of it.
func TestWP10GuardEmptyProjectIsRefusedByTheStore(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	repository := NewMemoryRepository(harness.db)
	ctx := context.Background()

	// A row to find, so an unguarded read would return something rather than an empty list.
	harness.remember(t, "guard-msg", "project-1", "script.decision", "user", "a memory")

	if _, err := repository.ListItems(ctx, MemoryListFilter{ProjectID: ""}); err == nil {
		t.Fatal("ListItems accepted an empty project")
	}
	if _, err := repository.PinnedItems(ctx, "", 10); err == nil {
		t.Fatal("PinnedItems accepted an empty project")
	}
	if _, err := repository.ItemsNeedingEmbedding(ctx, "", "m", "v", 10); err == nil {
		t.Fatal("ItemsNeedingEmbedding accepted an empty project")
	}
	if _, err := repository.VectorCandidates(ctx, memory.Scope{Project: ""}, "m", "v", 10); err == nil {
		t.Fatal("VectorCandidates accepted an empty project")
	}
	// A scope with no project is refused by the DOMAIN, which is the guard the store delegates to.
	if _, err := repository.UnsummarisedItems(ctx, memory.Scope{}, memory.TypeEpisodic, 10); err == nil {
		t.Fatal("UnsummarisedItems accepted a scope with no project")
	}
	// And a valid project is accepted, so the refusals above are not a read that never works.
	items, err := repository.ListItems(ctx, MemoryListFilter{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("a valid project was refused: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("the valid read returned %d rows", len(items))
	}
}

// TestWP10GuardAnEmbeddingNeedsItsProvenance is the store's own rule for a vector write.
//
// DOMAIN_MODEL section 14.5 keys the index on the model and version, so a vector stored without them
// is a row no search can ever reach. The service checks first; this is the store doing it too, so a
// caller that reached the store directly gets the same answer.
func TestWP10GuardAnEmbeddingNeedsItsProvenance(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	repository := NewMemoryRepository(harness.db)
	ctx := context.Background()
	item := harness.remember(t, "guard-emb", "project-1", "script.decision", "user", "embed me")
	blob := memory.EncodeVector([]float32{1, 0})

	if err := repository.AssignEmbedding(ctx, item.ID, blob, "", "v1", dramaTime()); err == nil {
		t.Fatal("AssignEmbedding stored a vector with no model")
	}
	if err := repository.AssignEmbedding(ctx, item.ID, blob, "m", "", dramaTime()); err == nil {
		t.Fatal("AssignEmbedding stored a vector with no version")
	}
	if err := repository.AssignEmbedding(ctx, item.ID, nil, "m", "v1", dramaTime()); err == nil {
		t.Fatal("AssignEmbedding stored an empty vector")
	}
	if err := repository.AssignEmbedding(ctx, item.ID, blob, "m", "v1", dramaTime()); err != nil {
		t.Fatalf("a complete embedding was refused: %v", err)
	}
}

// TestWP10GuardASearchNeedsAModelAndVersion is the read side of the same rule.
//
// A search that named no embedding version would compare a query vector against vectors from other
// spaces, which produces a confident wrong answer rather than an error. The store refuses.
func TestWP10GuardASearchNeedsAModelAndVersion(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	repository := NewMemoryRepository(harness.db)
	ctx := context.Background()
	scope := memory.Scope{Tenant: "local", Project: "project-1"}

	if _, err := repository.VectorCandidates(ctx, scope, "", "v1", 10); err == nil {
		t.Fatal("VectorCandidates accepted an empty model")
	}
	if _, err := repository.VectorCandidates(ctx, scope, "m", "", 10); err == nil {
		t.Fatal("VectorCandidates accepted an empty version")
	}
	if _, err := repository.VectorCandidates(ctx, scope, "m", "v1", 10); err != nil {
		t.Fatalf("a complete search was refused: %v", err)
	}
	// An unknown memory type is refused by the window read, so a caller cannot ask for a type the
	// schema's CHECK would reject.
	if _, err := repository.UnsummarisedItems(ctx, scope, "daydream", 10); err == nil {
		t.Fatal("UnsummarisedItems accepted an unknown memory type")
	}
}

// TestWP10GuardTheSecondaryReadsAreProjectScoped is the leak class, on the reads the acceptance test
// did not cover.
//
// `TestACME001ScopeIsolation` builds `project-1` and `project-10` — the prefix-sharing pair — and
// asserts the leak is zero on the list read and on the recall. The mutation review found three other
// reads that take a project and were never given a prefix-sharing neighbour: the summary window, the
// pinned channel, and the rebuild. A `LIKE 'project-1%'` mutation on any of them survived the suite
// and was only killed by a temporary probe. These are those probes, kept.
func TestWP10GuardTheSecondaryReadsAreProjectScoped(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	repository := NewMemoryRepository(harness.db)
	ctx := context.Background()

	// Two projects whose identifiers SHARE A PREFIX, each with the same content.
	for _, project := range []string{"project-1", "project-10"} {
		fact, err := harness.service.RememberFact(ctx, appmemory.RememberFactRequest{
			Scope:   memory.Scope{Tenant: "local", Project: project, AgentKey: "script.decision"},
			Content: "禁红规则 of " + project, Importance: 0.95, CreatedByID: "user-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		// PINNED, because the read under test is the pinned channel: an unpinned fact would be invisible
		// to it and the leak assertion would pass on an empty list.
		if _, err := harness.service.SetMemoryLocked(ctx, fact.ID, true, string(memory.ActorUser)); err != nil {
			t.Fatal(err)
		}
	}
	// The pinned read, which is the channel that bypasses the vector index's scope filter entirely.
	pinned, err := repository.PinnedItems(ctx, "project-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range pinned {
		if item.Scope.Project != "project-1" {
			t.Fatalf("PinnedItems returned a row from %q", item.Scope.Project)
		}
	}
	if len(pinned) == 0 {
		t.Fatal("the pinned read returned nothing, so the leak assertion proves nothing")
	}
	pinnedOther, err := repository.PinnedItems(ctx, "project-10", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinnedOther) != 1 || pinnedOther[0].Scope.Project != "project-10" {
		t.Fatalf("project ten's pinned read returned %+v", pinnedOther)
	}

	// The summary window: an episode memory in each project, and the window must see its own only.
	for _, project := range []string{"project-1", "project-10"} {
		if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
			Scope:   memory.Scope{Tenant: "local", Project: project, Episode: "episode-1", AgentKey: "script.decision"},
			Message: "a turn in " + project, MessageID: "win-" + project,
			Role: "user", AgentKey: "script.decision",
		}); err != nil {
			t.Fatal(err)
		}
	}
	window, err := repository.UnsummarisedItems(ctx,
		memory.Scope{Tenant: "local", Project: "project-1", Episode: "episode-1", AgentKey: "script.decision"},
		memory.TypeEpisodic, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(window) != 1 || window[0].Scope.Project != "project-1" {
		t.Fatalf("the summary window returned %+v", window)
	}

	// The rebuild read: a memory that needs embedding in each project.
	needing, err := repository.ItemsNeedingEmbedding(ctx, "project-1", "model", "version", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range needing {
		if item.Scope.Project != "project-1" {
			t.Fatalf("the rebuild read returned a row from %q", item.Scope.Project)
		}
	}
	if len(needing) == 0 {
		t.Fatal("the rebuild read returned nothing, so its leak assertion proves nothing")
	}
}
