package database

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// wp10MemoryHarness drives the REAL service over a REAL migrated database.
//
// # Why this file exists
//
// An independent review of this package ran probes and found that the store and its port did not
// line up: `MemoryRepository` declared its own filter type and a `VectorCandidates` with a
// different signature, so the interface was unsatisfied, `StorageAvailable` was false in every
// composed build, and every memory command refused with "no memory store is configured" while the
// store sat there implemented and tested. Nothing failed to compile, because the service takes an
// interface and nothing asserted the implementation satisfied it.
//
// Three more defects were invisible for the same reason — summary rows failed their own
// validation, the context builder never applied its default threshold, and the vector search
// dropped the agent scope. Each was in a pure function the unit tests covered and in a PLUMBING
// step nothing covered.
//
// So the fix is not only the four repairs: it is this harness, which wires the service the way
// the composition root does and drives a whole scenario through it. Every one of those defects
// fails here.
type wp10MemoryHarness struct {
	service *appmemory.Service
	db      *sql.DB
	agents  *AgentRepository
	// scope is where this harness's memories live. It is a FIELD rather than derived per call
	// because a memory and the summary that covers it must be written in the SAME scope: the
	// first version of this harness wrote memories with a blank episode and asked for a summary of
	// an episode scope, so the summary window was empty and the failure looked like a code defect.
	scope appmemory.Scope
	ctx   context.Context
	now   time.Time
}

// embedderDouble is a deterministic embedder the tests control.
//
// It is a double rather than the real feature-hash adapter so a test can decide what is similar to
// what: an embedding test whose similarities depended on a hash would be testing the hash.
type embedderDouble struct {
	// vectors maps a text to its vector. A text that is not in the map gets fallback, which is a
	// vector orthogonal to everything a test registered unless the test pins it — so an unlisted
	// text never matches by accident, and a test that needs a text it cannot name in advance (a
	// summary's rendered text) says "this one is about that" by pinning the fallback BEFORE the
	// call that embeds it.
	vectors  map[string][]float32
	fallback []float32
	calls    int
	// available is what Available reports, so a test can exercise the no-provider path.
	available bool
}

func (e *embedderDouble) Available(context.Context, string) bool { return e.available }

func (e *embedderDouble) Embed(_ context.Context, _ string, request appmemory.EmbeddingRequest) (appmemory.EmbeddingResult, error) {
	e.calls++
	vectors := make([][]float32, 0, len(request.Texts))
	for _, text := range request.Texts {
		if vector, ok := e.vectors[text]; ok {
			vectors = append(vectors, vector)
			continue
		}
		if len(e.fallback) > 0 {
			vectors = append(vectors, e.fallback)
			continue
		}
		vectors = append(vectors, orthogonalVector(len(e.vectors)+1, len(e.vectors)+2))
	}
	return appmemory.EmbeddingResult{Model: "test-model", Version: "test/v1", Vectors: vectors}, nil
}

// orthogonalVector builds a two-dimensional vector.
func orthogonalVector(x, y int) []float32 {
	vector := make([]float32, 256)
	vector[x%256] = 1
	if y%256 != x%256 {
		vector[y%256] = 0
	}
	return vector
}

// newWP10MemoryHarness composes the service over a migrated database.
func newWP10MemoryHarness(t *testing.T, embedder appmemory.Embedder) *wp10MemoryHarness {
	t.Helper()
	db := dramaRepoHandle(t)
	repository := NewMemoryRepository(db)
	agents := NewAgentRepository(db)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	service := appmemory.NewService(appmemory.Options{
		Store:    agents,
		Items:    repository,
		Vectors:  NewMemoryVectorIndex(repository),
		Embedder: embedder,
		Clock:    wp10Clock{at: now},
		IDs:      &wp10IDs{},
	})
	return &wp10MemoryHarness{
		service: service, db: db, agents: agents,
		scope: appmemory.ScopeFor("project-1", "episode-1", "script.decision"),
		ctx:   context.Background(), now: now,
	}
}

type wp10Clock struct{ at time.Time }

func (c wp10Clock) Now() time.Time { return c.at }

type wp10IDs struct{ next int }

func (g *wp10IDs) New() (string, error) {
	g.next++
	return "mem-" + itoaWP10(g.next), nil
}

func itoaWP10(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// seedTranscript writes an agent_messages row so the Recent channel has something to read.
//
// The row is written through the real repository, so the scope columns and the key are the
// runtime's own rather than a fixture's guess at them.
func (h *wp10MemoryHarness) seedTranscript(t *testing.T, id, projectID, agentKey, role, content string) {
	t.Helper()
	repository := h.agents
	run := agent.AgentRun{
		ID: "run-" + id, ProjectID: projectID, Layer: agent.LayerDecision, AgentKey: agentKey,
		SkillVersionID: "skill-1", Status: agent.RunSucceeded,
		StartedAt: h.now, Revision: 1,
	}
	if err := repository.CreateRun(h.ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	message := agent.AgentMessage{
		ID: id, AgentRunID: run.ID,
		ScopeKey: strings.Join([]string{"local", "", projectID, "", agentKey, ""}, "|"),
		Role:     agent.MessageRole(role), Content: content, ContentHash: "hash-" + id,
		CreatedAt: h.now,
	}
	if err := repository.RecordMessage(h.ctx, message); err != nil {
		t.Fatalf("RecordMessage: %v", err)
	}
}

// remember records one turn as an episodic memory.
func (h *wp10MemoryHarness) remember(t *testing.T, messageID, projectID, agentKey, role, content string) memory.MemoryItem {
	t.Helper()
	item, err := h.service.RememberMessage(h.ctx, appmemory.RememberMessageRequest{
		Scope:      appmemory.Scope{Tenant: "local", Project: projectID, Episode: h.scope.Episode, AgentKey: agentKey},
		Message:    content,
		MessageID:  messageID,
		Role:       agent.MessageRole(role),
		AgentKey:   agentKey,
		AgentRunID: "run-" + messageID,
		Embed:      true,
	})
	if err != nil {
		t.Fatalf("RememberMessage(%q): %v", content, err)
	}
	return item
}

// TestTheMemoryStoreSatisfiesItsPort is the conformance test the review asked for.
//
// It is written against the APPLICATION port, so it fails if either shape drifts. The assertion
// also lives in the repository as a package-level `var _`, which catches drift at compile time;
// this test is what makes the two names — the port and its implementation — appear together in a
// place a reader looking for "is this actually wired" will find.
func TestTheMemoryStoreSatisfiesItsPort(t *testing.T) {
	db := dramaRepoHandle(t)
	repository := NewMemoryRepository(db)
	var port appmemory.Repository = repository
	if port == nil {
		t.Fatal("the repository does not satisfy the port")
	}
	var index appmemory.VectorIndex = NewMemoryVectorIndex(repository)
	if index == nil {
		t.Fatal("the vector index does not satisfy the port")
	}
	// The composed service must report itself able to store, which is the property the review
	// found false in every real build.
	service := appmemory.NewService(appmemory.Options{
		Store: NewAgentRepository(db), Items: repository,
		Vectors: NewMemoryVectorIndex(repository),
		Clock:   wp10Clock{at: time.Now()}, IDs: &wp10IDs{},
	})
	if !service.StorageAvailable() {
		t.Fatal("a fully composed service reports that it has no store")
	}
}

// TestWritesAndReadsRoundTripThroughTheRealSchema drives the whole write path.
//
// AC-MEM-001's core: the memory store's own reads are project-scoped, so a query for one project
// returns only its rows. Two projects with SIMILAR content are seeded, because that is the case a
// similarity-based leak would show up in.
func TestWritesAndReadsRoundTripThroughTheRealSchema(t *testing.T) {
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{}}
	harness := newWP10MemoryHarness(t, embedder)

	first := harness.remember(t, "msg-a", "project-1", "script.decision", "user", "女主不能穿红色")
	second := harness.remember(t, "msg-b", "project-10", "script.decision", "user", "女主不能穿红色")

	if first.ID == second.ID {
		t.Fatal("two memories share an identifier")
	}
	items, err := harness.service.ListMemories(harness.ctx, appmemory.MemoryFilter{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("project one's list returned %+v, want only its own row", items)
	}
	// The prefix case: project-1's identifier is a prefix of project-10's.
	other, err := harness.service.ListMemories(harness.ctx, appmemory.MemoryFilter{ProjectID: "project-10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].ID != second.ID {
		t.Fatalf("project ten's list returned %+v", other)
	}
	// The round trip preserves what the writer stated, including the citation.
	read, err := harness.service.GetMemory(harness.ctx, first.ID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if read.SourceID != "msg-a" || read.SourceType != memory.SourceMessage {
		t.Fatalf("the citation did not survive the round trip: %+v", read)
	}
	if read.Role != agent.MessageUser || read.AgentKey != "script.decision" {
		t.Fatalf("the attribution did not survive the round trip: %+v", read)
	}
	if read.Importance != memory.DefaultImportance {
		t.Fatalf("importance is %v, want the default an unopinionated caller gets", read.Importance)
	}
}

// TestSummarizeProducesAProvableSummary is AC-MEM-004 through the real schema.
//
// The review found that EVERY summary failed its own validation, so the summary table could never
// be written and both AC-MEM-004 and AC-MEM-005 had no runnable path. This test is the one that
// would have caught it: it asserts that a summary exists, that its sources are the memories it
// covered, and that the reader can get from the summary to each source with its role, agent and
// time intact.
func TestSummarizeProducesAProvableSummary(t *testing.T) {
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{}}
	harness := newWP10MemoryHarness(t, embedder)
	scope := harness.scope

	first := harness.remember(t, "msg-1", "project-1", "script.decision", "user", "女主不能穿红色")
	second := harness.remember(t, "msg-2", "project-1", "script.decision", "assistant", "明白，改用深蓝")
	// A memory that has already been summarised must not be picked up twice.
	third := harness.remember(t, "msg-3", "project-1", "script.decision", "user", "第三轮")

	summary, found, err := harness.service.Summarize(harness.ctx, appmemory.SummarizeRequest{Scope: scope})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if !found {
		t.Fatal("Summarize found nothing to condense with three unsummarised memories")
	}
	if summary.Type != memory.TypeSummary {
		t.Fatalf("the summary's type is %q", summary.Type)
	}
	// The text names its sources' words, which is what "extraction" means and what makes the
	// provenance checkable rather than asserted.
	for _, want := range []string{"女主不能穿红色", "明白，改用深蓝", "user:", "assistant:"} {
		if !strings.Contains(summary.Content, want) {
			t.Fatalf("the summary does not contain %q:\n%s", want, summary.Content)
		}
	}
	// AC-MEM-004's chain: summary -> sources -> role/agent/time.
	sources, memories, err := harness.service.ListSummarySources(harness.ctx, summary.ID)
	if err != nil {
		t.Fatalf("ListSummarySources: %v", err)
	}
	if len(sources) != 3 || len(memories) != 3 {
		t.Fatalf("the summary cites %d sources and resolved %d memories", len(sources), len(memories))
	}
	if sources[0].SourceOrder != 1 || sources[2].SourceOrder != 3 {
		t.Fatalf("the source order is not 1,2,3: %+v", sources)
	}
	byID := map[string]memory.MemoryItem{}
	for _, item := range memories {
		byID[item.ID] = item
	}
	for _, want := range []memory.MemoryItem{first, second, third} {
		resolved, ok := byID[want.ID]
		if !ok {
			t.Fatalf("the summary does not cite %s", want.ID)
		}
		// The three fields AC-MEM-004 names, read back from the row rather than from the summary.
		if resolved.Role == "" || resolved.AgentKey == "" || resolved.CreatedAt.IsZero() {
			t.Fatalf("role/agent/time are not all preserved on %+v", resolved)
		}
		// And the hop to the transcript is a stored identifier, not a name to be resolved.
		if resolved.SourceID == "" {
			t.Fatalf("the source memory %s cites no message", resolved.ID)
		}
	}
	// The window is consumed: a second run has nothing left.
	_, found, err = harness.service.Summarize(harness.ctx, appmemory.SummarizeRequest{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("a second summarise produced a second summary of the same turns")
	}
	// What it did IT consume is marked, so the automatic path cannot loop over it.
	for _, want := range []memory.MemoryItem{first, second, third} {
		read, err := harness.service.GetMemory(harness.ctx, want.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !read.Summarized {
			t.Fatalf("memory %s is not marked summarised", want.ID)
		}
	}
}

// TestBuildMemoryContextAppliesTheThreshold is AC-MEM-003 through the service.
//
// The review found that the default threshold was never applied: a candidate of similarity 0.10
// entered the context because the caller had stated no threshold and the code passed a literal
// zero. The constant existed and was read by nothing.
func TestBuildMemoryContextAppliesTheThreshold(t *testing.T) {
	// "the red coat" is EXACTLY the query's vector; "unrelated" is orthogonal to it.
	query := "the red coat"
	similar := []float32{}
	orthogonal := []float32{}
	for index := 0; index < 256; index++ {
		similar = append(similar, 0)
		orthogonal = append(orthogonal, 0)
	}
	similar[0] = 1
	orthogonal[1] = 1
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{
		query:          similar,
		"the red coat": follow(similar),
		"unrelated":    orthogonal,
	}}
	harness := newWP10MemoryHarness(t, embedder)
	scope := harness.scope
	if _, err := harness.service.RememberMessage(harness.ctx, appmemory.RememberMessageRequest{
		Scope: scope, Message: "the red coat", MessageID: "msg-hit", Role: agent.MessageUser, Embed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.RememberMessage(harness.ctx, appmemory.RememberMessageRequest{
		Scope: scope, Message: "unrelated", MessageID: "msg-miss", Role: agent.MessageUser, Embed: true,
	}); err != nil {
		t.Fatal(err)
	}

	context, err := harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{
		Scope: scope, Query: query,
	})
	if err != nil {
		t.Fatalf("BuildMemoryContext: %v", err)
	}
	if !context.SemanticSearched {
		t.Fatal("the semantic channel did not run despite a configured embedder")
	}
	if len(context.Semantic) != 1 {
		t.Fatalf("the semantic channel returned %d candidates, want only the one above the default threshold: %+v",
			len(context.Semantic), context.Semantic)
	}
	if !strings.Contains(context.Semantic[0].Item.Content, "the red coat") {
		t.Fatalf("the threshold admitted the wrong candidate: %+v", context.Semantic[0].Item.Content)
	}
	// An explicit zero threshold is the caller asking for the similarity test to be off, which is
	// a different thing from stating nothing.
	permissive, err := harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{
		Scope: scope, Query: query, Threshold: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(permissive.Semantic) < 1 {
		t.Fatal("a caller that asked for everything got nothing")
	}
}

// follow returns a copy of a vector, so two map entries cannot share one slice.
func follow(vector []float32) []float32 {
	copied := make([]float32, len(vector))
	copy(copied, vector)
	return copied
}

// TestTheVectorSearchFiltersTheWholeScope is the defect the review found in the search.
//
// It dropped scope.AgentKey, so a decision agent could recall a supervisor's conversation by
// similarity while four doc comments asserted the scope filter ran in full. The transcript channel
// had always filtered the agent, so the two disagreed about what a scope was.
func TestTheVectorSearchFiltersTheWholeScope(t *testing.T) {
	// One vector, shared by both agents' memories, so the only thing that can separate them is the
	// scope filter.
	shared := make([]float32, 256)
	shared[7] = 1
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{
		"shared text": shared,
	}}
	harness := newWP10MemoryHarness(t, embedder)
	decisionScope := harness.scope
	supervisionScope := appmemory.ScopeFor("project-1", "episode-1", "script.supervision")
	for _, seed := range []struct {
		scope   appmemory.Scope
		message string
		id      string
	}{
		{decisionScope, "shared text", "msg-decision"},
		{supervisionScope, "shared text", "msg-supervision"},
	} {
		if _, err := harness.service.RememberMessage(harness.ctx, appmemory.RememberMessageRequest{
			Scope: seed.scope, Message: seed.message, MessageID: seed.id,
			Role: agent.MessageUser, AgentKey: seed.scope.AgentKey, Embed: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	context, err := harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{
		Scope: decisionScope, Query: "shared text",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range context.Semantic {
		if candidate.Item.Scope.AgentKey != "script.decision" {
			t.Fatalf("the semantic channel returned another agent's memory: %+v", candidate.Item)
		}
	}
	if len(context.Semantic) == 0 {
		t.Fatal("the semantic channel returned nothing, so the filter may be dropping everything")
	}
	// The supervisor's own scope still sees its own.
	supervisionContext, err := harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{
		Scope: supervisionScope, Query: "shared text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(supervisionContext.Semantic) != len(context.Semantic) {
		t.Fatalf("the two agents see different numbers of their own memories: %d and %d",
			len(context.Semantic), len(supervisionContext.Semantic))
	}
}

// TestDeepRecallReversesASummaryToItsMessages is AC-MEM-005 through the schema.
//
// The five steps of AGENT_CONTRACTS section 12.3: summary candidates, the threshold, the rerank,
// the sources, and the original rows — with the provenance that says which summary each came
// through, which is what the criterion's "返回来源" asks for.
func TestDeepRecallReversesASummaryToItsMessages(t *testing.T) {
	question := "为什么禁止红色服装?"
	questionVector := make([]float32, 256)
	questionVector[3] = 1
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{
		// The question and the setting share a vector, so the summary about it is found.
		question: questionVector,
	}}
	harness := newWP10MemoryHarness(t, embedder)
	scope := harness.scope

	if _, err := harness.service.RememberMessage(harness.ctx, appmemory.RememberMessageRequest{
		Scope: scope, Message: "女主不能穿红色", MessageID: "msg-red",
		Role: agent.MessageUser, AgentKey: "script.decision", Embed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.RememberMessage(harness.ctx, appmemory.RememberMessageRequest{
		Scope: scope, Message: "这段之后继续", MessageID: "msg-after",
		Role: agent.MessageAssistant, AgentKey: "script.decision", Embed: true,
	}); err != nil {
		t.Fatal(err)
	}
	// The summary's text is a rendering this test cannot name in advance, so the double's fallback
	// is pinned to the question's vector BEFORE the call that embeds it: that is how a test says
	// "this summary is about that question". Pinning it after the fact was the first version's
	// mistake, and it failed because the row had already been written with the old vector.
	embedder.fallback = questionVector
	summary, found, err := harness.service.Summarize(harness.ctx, appmemory.SummarizeRequest{Scope: scope, Embed: true})
	if err != nil || !found {
		t.Fatalf("Summarize: found=%v err=%v", found, err)
	}

	result, err := harness.service.DeepRecall(harness.ctx, appmemory.DeepRecallRequest{
		Scope: scope, Query: question,
	})
	if err != nil {
		t.Fatalf("DeepRecall: %v", err)
	}
	if len(result.Summaries) == 0 {
		t.Fatal("deep recall found no summary")
	}
	if result.Summaries[0].Item.ID != summary.ID {
		t.Fatalf("deep recall selected %s, want the summary %s", result.Summaries[0].Item.ID, summary.ID)
	}
	// The original messages come back, and the provenance says which summary they came through.
	if len(result.Messages) != 2 {
		t.Fatalf("deep recall restored %d messages, want 2", len(result.Messages))
	}
	if len(result.Provenance) != len(result.Messages) {
		t.Fatalf("%d messages but %d provenance entries", len(result.Messages), len(result.Provenance))
	}
	for _, entry := range result.Provenance {
		if entry.SummarizedBy != summary.ID {
			t.Fatalf("a provenance entry names %q as the summary", entry.SummarizedBy)
		}
		if entry.MessageID == "" || entry.Role == "" || entry.CreatedAt.IsZero() {
			t.Fatalf("a provenance entry is missing role/agent/time: %+v", entry)
		}
	}
	if result.UsedTokens == 0 {
		t.Fatal("deep recall reports spending no tokens on messages it returned")
	}
	// The bound is stated even when it is not reached, so a caller can tell "there was no more"
	// from "it stopped".
	if result.Truncated {
		t.Fatal("a two-message walk reports itself truncated")
	}
}

// TestDeepRecallRefusesWithoutAnEmbeddingProvider is the honest-refusal case.
//
// A caller that asked for history and was handed the recent window instead would conclude the
// history was not there, which is worse than being told the search is unavailable.
func TestDeepRecallRefusesWithoutAnEmbeddingProvider(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: false, vectors: map[string][]float32{}})
	scope := harness.scope
	_, err := harness.service.DeepRecall(harness.ctx, appmemory.DeepRecallRequest{Scope: scope, Query: "anything"})
	if err == nil {
		t.Fatal("deep recall answered with no embedding provider configured")
	}
	if !strings.Contains(err.Error(), "embedding") {
		t.Fatalf("the refusal reads %q, which does not say what is missing", err.Error())
	}
	// The same project CAN still build a context: the recent and summary channels do not need a
	// provider, and reporting the whole feature off would be wrong.
	context, err := harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{Scope: scope})
	if err != nil {
		t.Fatalf("BuildMemoryContext without an embedder: %v", err)
	}
	if context.SemanticSearched {
		t.Fatal("the semantic channel reports that it searched with no provider")
	}
}

// TestThePinnedChannelSurvivesWithoutAVector is AC-MEM-003's exception through the service.
//
// "locked high-importance memory 走独立规则": a pinned memory is recalled with no query, no vector
// and no threshold, which is why the channel runs even when the semantic one cannot.
func TestThePinnedChannelSurvivesWithoutAVector(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: false, vectors: map[string][]float32{}})
	scope := harness.scope
	pinned, err := harness.service.RememberFact(harness.ctx, appmemory.RememberFactRequest{
		Scope: scope, Content: "禁红规则：女主不得穿红色", Importance: 0.95, CreatedByID: "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.SetMemoryLocked(harness.ctx, pinned.ID, true, string(memory.ActorUser)); err != nil {
		t.Fatal(err)
	}
	context, err := harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Facts) != 1 || context.Facts[0].Item.ID != pinned.ID {
		t.Fatalf("the pinned channel returned %+v, want the pinned fact", context.Facts)
	}
	if !context.Facts[0].Pinned {
		t.Fatal("a fact is not marked pinned, so a reader cannot tell why the threshold did not apply")
	}
	// A pinned memory that is NOT important is not in the channel, which is what keeps a pinned
	// triviality out of every context.
	trivial, err := harness.service.RememberFact(harness.ctx, appmemory.RememberFactRequest{
		Scope: scope, Content: "记得买咖啡", Importance: 0.1, CreatedByID: "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.SetMemoryLocked(harness.ctx, trivial.ID, true, string(memory.ActorUser)); err != nil {
		t.Fatal(err)
	}
	context, err = harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range context.Facts {
		if fact.Item.ID == trivial.ID {
			t.Fatal("a pinned triviality entered the pinned channel")
		}
	}
}

// TestTheLockedRuleIsEnforcedByTheStoreNotTheCaller is DOMAIN_MODEL section 14.5's
// "locked memory 只有用户可修改/删除".
//
// The rule is in the UPDATE's WHERE clause, so two actors racing on one row cannot both pass a
// check each made against a stale read.
func TestTheLockedRuleIsEnforcedByTheStoreNotTheCaller(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	scope := appmemory.ScopeFor("project-1", "", "script.decision")
	item := harness.remember(t, "msg-locked", "project-1", "script.decision", "user", "pin me")
	if _, err := harness.service.SetMemoryLocked(harness.ctx, item.ID, true, string(memory.ActorUser)); err != nil {
		t.Fatal(err)
	}
	// An agent may not delete it.
	result, err := harness.service.DeleteMemory(harness.ctx, appmemory.DeleteMemoryRequest{
		MemoryID: item.ID, Actor: string(memory.ActorAgent),
	})
	if err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if result.Deleted {
		t.Fatal("an agent deleted a pinned memory")
	}
	if _, err := harness.service.GetMemory(harness.ctx, item.ID); err != nil {
		t.Fatalf("a refused delete removed the row anyway: %v", err)
	}
	// An agent may not edit it either.
	if edited, err := harness.service.UpdateMemoryContent(harness.ctx, item.ID, "rewritten", string(memory.ActorAgent)); err != nil {
		t.Fatal(err)
	} else if edited {
		t.Fatal("an agent edited a pinned memory")
	}
	_ = scope
	// The user may.
	if deleted, err := harness.service.DeleteMemory(harness.ctx, appmemory.DeleteMemoryRequest{
		MemoryID: item.ID, Actor: string(memory.ActorUser),
	}); err != nil {
		t.Fatal(err)
	} else if !deleted.Deleted {
		t.Fatal("the user could not delete their own pinned memory")
	}
	// A soft delete keeps the row, which is what makes AC-MEM-004's policy inspectable.
	read, err := harness.service.GetMemory(harness.ctx, item.ID)
	if err != nil {
		t.Fatalf("a soft-deleted memory is not readable: %v", err)
	}
	if !read.Deleted() {
		t.Fatal("the delete did not mark the row")
	}
}

// TestDeletingASourceAppliesTheSummaryPolicy is AC-MEM-004's deletion clause.
//
// Both readings of the policy are reachable and the result reports which was applied, so a user
// can see that a summary went with its source rather than discovering it later.
func TestDeletingASourceAppliesTheSummaryPolicy(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	scope := harness.scope
	source := harness.remember(t, "msg-policy", "project-1", "script.decision", "user", "早期设定")
	if _, err := harness.service.RememberMessage(harness.ctx, appmemory.RememberMessageRequest{
		Scope: scope, Message: "后续一轮", MessageID: "msg-policy-2", Role: agent.MessageAssistant,
	}); err != nil {
		t.Fatal(err)
	}
	summary, found, err := harness.service.Summarize(harness.ctx, appmemory.SummarizeRequest{Scope: scope})
	if err != nil || !found {
		t.Fatalf("Summarize: found=%v err=%v", found, err)
	}
	// The first delete keeps the summary, which is the default because deleting more than the user
	// asked for is the unrecoverable direction.
	kept, err := harness.service.DeleteMemory(harness.ctx, appmemory.DeleteMemoryRequest{
		MemoryID: source.ID, Actor: string(memory.ActorUser),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.KeptSummaries) != 1 || kept.KeptSummaries[0] != summary.ID {
		t.Fatalf("the kept summaries are %v, want [%s]", kept.KeptSummaries, summary.ID)
	}
	if len(kept.InvalidatedSummaries) != 0 {
		t.Fatalf("a keeping delete invalidated %v", kept.InvalidatedSummaries)
	}
	// A second memory, summarised, deleted with invalidation: the summary goes with it and the
	// result says so.
	second := harness.remember(t, "msg-policy-3", "project-1", "script.decision", "user", "第二个设定")
	if _, _, err := harness.service.Summarize(harness.ctx, appmemory.SummarizeRequest{Scope: scope}); err != nil {
		t.Fatal(err)
	}
	invalidated, err := harness.service.DeleteMemory(harness.ctx, appmemory.DeleteMemoryRequest{
		MemoryID: second.ID, Actor: string(memory.ActorUser), InvalidateSummaries: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(invalidated.InvalidatedSummaries) == 0 {
		t.Fatal("an invalidating delete invalidated nothing")
	}
	for _, id := range invalidated.InvalidatedSummaries {
		read, err := harness.service.GetMemory(harness.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !read.Deleted() {
			t.Fatalf("the summary %s is still live after an invalidating delete", id)
		}
	}
}

// TestRebuildEmbeddingSwitchesTheIndexVersion is DOMAIN_MODEL section 14.5's
// "重建后切换索引版本".
func TestRebuildEmbeddingSwitchesTheIndexVersion(t *testing.T) {
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{}}
	harness := newWP10MemoryHarness(t, embedder)
	scope := harness.scope
	item := harness.remember(t, "msg-rebuild", "project-1", "script.decision", "user", "一条记忆")

	// The rows are on the test model. A rebuild with the same recipe has nothing to do, which is
	// the honest answer rather than a re-embed of everything.
	result, err := harness.service.RebuildEmbedding(harness.ctx, appmemory.RebuildEmbeddingRequest{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("RebuildEmbedding: %v", err)
	}
	if result.Rebuilt != 0 {
		t.Fatalf("a rebuild on the current recipe re-embedded %d rows", result.Rebuilt)
	}
	if result.Model != "test-model" || result.Version != "test/v1" {
		t.Fatalf("the rebuild reports recipe %q/%q", result.Model, result.Version)
	}
	// An edit clears the vector, which makes the row a rebuild candidate: a vector that still
	// described the old text would make the memory match queries about something it no longer says.
	if _, err := harness.service.UpdateMemoryContent(harness.ctx, item.ID, "改过的记忆", string(memory.ActorUser)); err != nil {
		t.Fatal(err)
	}
	cleared, err := harness.service.GetMemory(harness.ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.EmbeddingBlob) != 0 {
		t.Fatal("an edit left the old vector in place")
	}
	// The mapping is registered BEFORE the rebuild, because the rebuild is the call that embeds the
	// row: registering it after would leave the stored vector as the untouched fallback and the
	// search would find nothing, which is a harness ordering mistake rather than a defect — the
	// first version of this test made it and read as a broken rebuild.
	query := make([]float32, 256)
	query[5] = 1
	embedder.vectors["改过的记忆"] = query
	result, err = harness.service.RebuildEmbedding(harness.ctx, appmemory.RebuildEmbeddingRequest{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rebuilt != 1 {
		t.Fatalf("the rebuild restored %d rows, want 1", result.Rebuilt)
	}
	// And it is searchable again, which is the whole point of rebuilding.
	context, err := harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{
		Scope: scope, Query: "改过的记忆",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Semantic) == 0 {
		t.Fatal("a rebuilt memory is not searchable")
	}
}
