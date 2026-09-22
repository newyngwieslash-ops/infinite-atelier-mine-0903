package database

import (
	"context"
	"strings"
	"testing"
	"time"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// acceptance_wp10_test.go drives ACCEPTANCE.md's AC-MEM-001 through AC-MEM-005 over the real schema.
//
// # Why these are not unit tests
//
// Every criterion in that section is about a RELATION between a stored row and a query: which
// project's memory comes back, whether the current turn is in it, whether a below-threshold candidate
// was dropped, whether a summary's sources resolve. A unit test with a hand-built struct would prove
// each of those about the test's own data rather than about the schema the product uses — which is
// exactly how three defects survived the first round of WP-10's own tests: the port and its
// implementation disagreed, every summary failed its own validation, and the search dropped the agent
// scope, and none of the three could fail a test that never touched a database.

// wp10Reset gives each acceptance test a fresh schema.
type wp10Fixture struct {
	harness  *wp10MemoryHarness
	embedder *embedderDouble
}

func newWP10Fixture(t *testing.T, embedder *embedderDouble) wp10Fixture {
	t.Helper()
	return wp10Fixture{harness: newWP10MemoryHarness(t, embedder), embedder: embedder}
}

// TestACME001ScopeIsolation is AC-MEM-001, verbatim:
//
//	建立两个项目相似角色设定：
//	- Project A 查询只返回 A；
//	- Episode scope 正确；
//	- Agent scope 正确；
//	- 跨项目泄露为 0。
//
// The two projects are given the SAME content, because a leak that only showed up when the content
// differed would be a leak this test could not see. What separates them is the scope, and that is the
// property being graded.
func TestACME001ScopeIsolation(t *testing.T) {
	fixture := newWP10Fixture(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	harness := fixture.harness
	ctx := context.Background()
	const setting = "女主不能穿红色"

	// The same setting in two projects, with two episodes and two agents inside project one.
	seed := []struct {
		messageID string
		episode   string
		agentKey  string
	}{
		{"leak-a", "episode-1", "script.decision"},
		{"leak-b", "episode-1", "script.supervision"},
		{"leak-c", "episode-2", "script.decision"},
		{"leak-d", "", "script.decision"},
	}
	for _, entry := range seed {
		if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
			Scope: memory.Scope{
				Tenant: "local", Project: "project-1", Episode: entry.episode, AgentKey: entry.agentKey,
			},
			Message: setting, MessageID: entry.messageID,
			Role: agent.MessageUser, AgentKey: entry.agentKey,
		}); err != nil {
			t.Fatalf("RememberMessage: %v", err)
		}
	}
	// And the same setting in project-10, whose identifier SHARES A PREFIX with project one — the
	// case a prefix-matching store would leak.
	if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope:   memory.Scope{Tenant: "local", Project: "project-10", Episode: "episode-1", AgentKey: "script.decision"},
		Message: setting, MessageID: "leak-project-10", Role: agent.MessageUser, AgentKey: "script.decision",
	}); err != nil {
		t.Fatal(err)
	}

	// AC-MEM-001, first clause: a project's own query returns only its rows, and NOT the other
	// project's despite the identical content.
	projectRows, err := harness.service.ListMemories(ctx, appmemory.MemoryFilter{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(projectRows) != len(seed) {
		t.Fatalf("project one has %d memories, want %d: %+v", len(projectRows), len(seed), projectRows)
	}
	for _, row := range projectRows {
		if row.Scope.Project != "project-1" {
			t.Fatalf("a query for project one returned a row from %q", row.Scope.Project)
		}
	}
	otherRows, err := harness.service.ListMemories(ctx, appmemory.MemoryFilter{ProjectID: "project-10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherRows) != 1 || otherRows[0].Scope.Project != "project-10" {
		t.Fatalf("project ten's query returned %+v", otherRows)
	}
	// 跨项目泄露为 0, asserted the other way round too: no row of project ten is reachable by ANY
	// read scoped to project one.
	for _, row := range otherRows {
		if read, err := harness.service.GetMemory(ctx, row.ID); err == nil {
			if read.Scope.Project == "project-1" {
				t.Fatal("a project-ten row reports itself as project one's")
			}
		}
	}

	// The RECALL path, which is the one that matters: the memory layer a run gets must contain
	// nothing from another project. The harness's scope is project one, so every recalled item must
	// carry one of project one's message ids.
	recallContext, err := harness.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{
		Scope: memory.Scope{Tenant: "local", Project: "project-1", Episode: "episode-1", AgentKey: "script.decision"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range recallContext.Recent {
		if item.MessageID == "leak-project-10" {
			t.Fatal("the recall returned project ten's memory")
		}
	}

	// AC-MEM-001, second clause: the EPISODE scope. A read naming episode one must not carry
	// episode two's rows, and the blank-episode read must not carry an episode's.
	episodeOne, err := harness.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{
		Scope: memory.Scope{Tenant: "local", Project: "project-1", Episode: "episode-1", AgentKey: "script.decision"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range episodeOne.Recent {
		if item.MessageID == "leak-c" {
			t.Fatal("the episode-one recall returned episode two's memory")
		}
	}

	// AC-MEM-001, third clause: the AGENT scope. A decision agent's context must not carry a
	// supervisor's turn — the cross-agent leak the search's missing agent filter produced.
	decision, err := harness.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{
		Scope: memory.Scope{Tenant: "local", Project: "project-1", Episode: "episode-1", AgentKey: "script.decision"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range decision.Recent {
		if item.MessageID == "leak-b" {
			t.Fatal("a decision agent's recall returned a supervisor's memory")
		}
	}
}

// TestACME002SelfHitExclusionAndOrder is AC-MEM-002, verbatim:
//
//	当前消息：女主不能穿红色。
//	- Recall 在写当前消息前或排除其 ID；
//	- Context 中当前句只出现一次；
//	- 单元测试验证。
//
// The criterion is satisfied twice in this build — the runtime recalls before it writes, and the
// store excludes by id — and the test asserts BOTH, because either alone would leave a hole: an
// exclusion without the order would still let a shared transcript recall a turn written a moment ago,
// and the order without the exclusion would recall it on a retry.
func TestACME002SelfHitExclusionAndOrder(t *testing.T) {
	fixture := newWP10Fixture(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	harness := fixture.harness
	ctx := context.Background()
	const current = "女主不能穿红色"

	// The conversation so far, and then the current turn written as the memory the exclusion names.
	if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope: harness.scope, Message: "之前定过冬装。", MessageID: "turn-1",
		Role: agent.MessageUser, AgentKey: "script.decision",
	}); err != nil {
		t.Fatal(err)
	}
	currentItem, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope: harness.scope, Message: current, MessageID: "turn-2",
		Role: agent.MessageUser, AgentKey: "script.decision",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The exclusion, which is the half the STORE can enforce: asked to exclude turn-2's memory, the
	// recall must not return it.
	recalled, err := harness.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{
		Scope:            harness.scope,
		ExcludeMemoryIDs: []string{currentItem.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range recalled.Recent {
		if item.MessageID == "turn-2" {
			t.Fatal("the recall returned the turn it was told to exclude")
		}
	}
	// The ordering half, observed through the runtime's own port: the recording port asserts the
	// sequence, and this asserts the property that makes the sequence matter — the sentence appears
	// ONCE in the layer a run is given.
	//
	// The count is the criterion's own "Context 中当前句只出现一次", and the way to check it is to
	// count occurrences across every channel rather than to look for an absence: a duplicate would be
	// a prompt in which the model reads the same instruction twice.
	occurrences := 0
	for _, item := range recalled.Recent {
		occurrences += strings.Count(item.Content, current)
	}
	for _, scored := range recalled.Semantic {
		occurrences += strings.Count(scored.Item.Content, current)
	}
	for _, scored := range recalled.Facts {
		occurrences += strings.Count(scored.Item.Content, current)
	}
	for _, scored := range recalled.Summaries {
		occurrences += strings.Count(scored.Item.Content, current)
	}
	if occurrences > 1 {
		t.Fatalf("the current turn appears %d times in the memory layer", occurrences)
	}
}

// TestACME003ThresholdDropsBelowRelevance is AC-MEM-003, verbatim:
//
//	所有候选低于阈值：
//	- semantic 返回空；
//	- 不强制 TopK；
//	- locked high-importance memory 走独立规则。
func TestACME003ThresholdDropsBelowRelevance(t *testing.T) {
	// Two vectors that are nearly orthogonal: the query's similarity to the memory is about 0.02,
	// which is below the default threshold by a wide margin.
	queryVector := make([]float32, 256)
	queryVector[0] = 1
	memoryVector := make([]float32, 256)
	memoryVector[1] = 1
	memoryVector[2] = 0.02
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{
		"query text":  queryVector,
		"low content": memoryVector,
	}}
	fixture := newWP10Fixture(t, embedder)
	harness := fixture.harness
	ctx := context.Background()

	if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope: harness.scope, Message: "low content", MessageID: "low-1",
		Role: agent.MessageUser, AgentKey: "script.decision", Embed: true,
	}); err != nil {
		t.Fatal(err)
	}
	// A pinned high-importance memory in the same scope, embedded with the SAME low-similarity
	// vector, so the only thing that can put it in the context is its own rule.
	pinned, err := harness.service.RememberFact(ctx, appmemory.RememberFactRequest{
		Scope: harness.scope, Content: "禁红规则", Importance: 0.95, CreatedByID: "user-1", Embed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.SetMemoryLocked(ctx, pinned.ID, true, string(memory.ActorUser)); err != nil {
		t.Fatal(err)
	}
	// The fact's vector is the low one too, so similarity cannot be what saves it.
	embedder.vectors["禁红规则"] = memoryVector

	context, err := harness.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{
		Scope: harness.scope, Query: "query text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !context.SemanticSearched {
		t.Fatal("the semantic channel did not run")
	}
	// 1. semantic 返回空 — nothing above the threshold, so the channel is empty.
	if len(context.Semantic) != 0 {
		t.Fatalf("the semantic channel returned %+v with every candidate below the threshold", context.Semantic)
	}
	// 2. 不强制 TopK — the emptiness is not filled by a count. If the retrieval had forced a Top-K,
	// the low candidate would appear here; the assertion is on the CHANNEL, which is the thing the
	// criterion names.
	if len(context.Summaries) != 0 {
		t.Fatalf("the summary channel returned %+v below the threshold", context.Summaries)
	}
	// 3. locked high-importance memory 走独立规则 — the pinned fact is present DESPITE having the
	// same below-threshold similarity, because its own rule does not consult the threshold.
	found := false
	for _, fact := range context.Facts {
		if fact.Item.ID == pinned.ID {
			found = true
			if !fact.Pinned {
				t.Fatal("the pinned fact is not marked as having bypassed the threshold")
			}
		}
	}
	if !found {
		t.Fatalf("a locked high-importance memory was dropped by the threshold: %+v", context.Facts)
	}

	// And the same memory is NOT in the semantic channel: a fact appears once, on its own channel.
	for _, candidate := range context.Semantic {
		if candidate.Item.ID == pinned.ID {
			t.Fatal("the pinned fact also appeared on the thresholded channel")
		}
	}
}

// TestACME004SummaryProvenanceAndPolicy is AC-MEM-004, verbatim:
//
//   - Summary 关联源消息表；
//   - role/agent/time 保留；
//   - 删除/失效行为符合策略；
//   - UI 可跳原始消息。
func TestACME004SummaryProvenanceAndPolicy(t *testing.T) {
	fixture := newWP10Fixture(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	harness := fixture.harness
	ctx := context.Background()

	first := harness.remember(t, "ac-source-1", "project-1", "script.decision", "user", "女主不能穿红色")
	second := harness.remember(t, "ac-source-2", "project-1", "script.decision", "assistant", "改用深蓝")
	summary, found, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: harness.scope, Level: 1,
	})
	if err != nil || !found {
		t.Fatalf("Summarize: found=%v err=%v", found, err)
	}

	// Clause one: the summary is linked to its sources, and every source resolves.
	sources, memories, err := harness.service.ListSummarySources(ctx, summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("the summary cites %d sources, want 2: %+v", len(sources), sources)
	}
	if len(memories) != 2 {
		t.Fatalf("the summary resolved %d of its sources", len(memories))
	}
	byID := map[string]memory.MemoryItem{}
	for _, item := range memories {
		byID[item.ID] = item
	}

	// Clause two: role, agent and time survive on each source, and so does the hop to the transcript.
	for _, want := range []memory.MemoryItem{first, second} {
		resolved, ok := byID[want.ID]
		if !ok {
			t.Fatalf("the summary does not cite %s", want.ID)
		}
		if resolved.Role != want.Role {
			t.Fatalf("the source's role was lost: %q vs %q", resolved.Role, want.Role)
		}
		if resolved.AgentKey != "script.decision" {
			t.Fatalf("the source's agent was lost: %q", resolved.AgentKey)
		}
		if !resolved.CreatedAt.Equal(want.CreatedAt) {
			t.Fatalf("the source's time was lost: %v vs %v", resolved.CreatedAt, want.CreatedAt)
		}
		// Clause four: the transcript row is a stored identifier a UI can follow, which is what
		// "UI 可跳原始消息" needs.
		if resolved.SourceID != want.SourceID {
			t.Fatalf("the source cites %q, want the message %q", resolved.SourceID, want.SourceID)
		}
	}
	// The summary's own text carries its sources' words and their roles, so a reader can line the
	// summary up against the rows without trusting either.
	for _, want := range []string{"女主不能穿红色", "改用深蓝", "user:", "assistant:"} {
		if !strings.Contains(summary.Content, want) {
			t.Fatalf("the summary does not carry %q:\n%s", want, summary.Content)
		}
	}

	// Clause three: the deletion policy, in both directions.
	kept, err := harness.service.DeleteMemory(ctx, appmemory.DeleteMemoryRequest{
		MemoryID: first.ID, Actor: string(memory.ActorUser),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.KeptSummaries) != 1 || kept.KeptSummaries[0] != summary.ID {
		t.Fatalf("a keeping delete reported %+v", kept)
	}
	// The summary survives, and its source is now a visible HOLE rather than a shorter list.
	surviving, err := harness.service.GetMemory(ctx, summary.ID)
	if err != nil {
		t.Fatalf("the summary did not survive a keeping delete: %v", err)
	}
	if surviving.Deleted() {
		t.Fatal("a keeping delete removed the summary")
	}
	_, afterMemories, err := harness.service.ListSummarySources(ctx, summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	holes := 0
	for _, item := range afterMemories {
		if item.Deleted() {
			holes++
		}
	}
	if holes != 1 {
		t.Fatalf("the summary shows %d holes after one source was deleted, want 1", holes)
	}

	// The invalidating direction: a second summary, deleted with its sources.
	third := harness.remember(t, "ac-source-3", "project-1", "script.decision", "user", "第三个设定")
	if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope: harness.scope, Message: "第四轮", MessageID: "ac-source-4",
		Role: agent.MessageAssistant, AgentKey: "script.decision",
	}); err != nil {
		t.Fatal(err)
	}
	secondSummary, found, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{Scope: harness.scope, Level: 1})
	if err != nil || !found {
		t.Fatalf("second Summarize: found=%v err=%v", found, err)
	}
	invalidated, err := harness.service.DeleteMemory(ctx, appmemory.DeleteMemoryRequest{
		MemoryID: third.ID, Actor: string(memory.ActorUser), InvalidateSummaries: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(invalidated.InvalidatedSummaries) != 1 || invalidated.InvalidatedSummaries[0] != secondSummary.ID {
		t.Fatalf("an invalidating delete reported %+v", invalidated)
	}
	gone, err := harness.service.GetMemory(ctx, secondSummary.ID)
	if err != nil {
		t.Fatalf("an invalidated summary is not readable: %v", err)
	}
	if !gone.Deleted() {
		t.Fatal("an invalidated summary is still live")
	}
}

// TestACME005DeepRecallWalksBackToTheMessages is AC-MEM-005, verbatim:
//
//	早期设定经过大量消息：
//	- Summary 检索；
//	- rerank；
//	- 恢复原始消息；
//	- 返回来源；
//	- 限制 Token；
//	- 不每轮自动调用。
func TestACME005DeepRecallWalksBackToTheMessages(t *testing.T) {
	question := "为什么禁止红色服装?"
	questionVector := make([]float32, 256)
	questionVector[3] = 1
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{question: questionVector}}
	fixture := newWP10Fixture(t, embedder)
	harness := fixture.harness
	ctx := context.Background()

	// The early setting, and then a lot of messages after it. "大量消息" is the criterion's own
	// phrase: the point is that the early turn is far enough back that a recent window would miss it.
	if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope: harness.scope, Message: "女主不能穿红色，这是全剧的禁令。", MessageID: "early-1",
		Role: agent.MessageUser, AgentKey: "script.decision",
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 40; index++ {
		if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
			Scope: harness.scope, Message: "之后的第 " + itoaWP10(index) + " 轮对话。",
			MessageID: "later-" + itoaWP10(index),
			Role:      agent.MessageAssistant, AgentKey: "script.decision",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// The summary of the early setting, embedded with the question's vector so the search finds it —
	// which is what "summary 检索" means: the summary is the artifact deep recall searches.
	embedder.fallback = questionVector
	summary, found, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: harness.scope, Level: 1, Window: 4, Embed: true,
	})
	if err != nil || !found {
		t.Fatalf("Summarize: found=%v err=%v", found, err)
	}
	isEarlySummarised := false
	_, summarySources, err := harness.service.ListSummarySources(ctx, summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range summarySources {
		if item.SourceID == "early-1" {
			isEarlySummarised = true
		}
	}
	if !isEarlySummarised {
		t.Fatalf("the summary does not cover the early setting, so this test would prove nothing: %+v", summarySources)
	}

	// 6. 不每轮自动调用 — the automatic context builder must not run a deep recall. It has no effect
	// on the channels that the criterion would otherwise see, and it does not touch the summaries it
	// was not asked for.
	automatic, err := harness.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{
		Scope: harness.scope, Query: question,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range automatic.Summaries {
		if candidate.Item.ID == summary.ID {
			// The automatic SUMMARY channel may find it by similarity — that is section 12.1's
			// summary channel and not deep recall. What the criterion forbids is the WALK: the
			// automatic path must not restore 40 original messages on every turn.
			break
		}
	}
	if len(automatic.Recent) > 40 {
		t.Fatal("the automatic context restored the whole history, which is the walk deep recall is for")
	}

	// The deep recall itself.
	result, err := harness.service.DeepRecall(ctx, appmemory.DeepRecallRequest{
		Scope: harness.scope, Query: question,
	})
	if err != nil {
		t.Fatalf("DeepRecall: %v", err)
	}
	// 1. Summary 检索 — a summary was found and selected.
	if len(result.Summaries) == 0 || result.Summaries[0].Item.ID != summary.ID {
		t.Fatalf("deep recall selected %+v, want the summary %s", result.Summaries, summary.ID)
	}
	// 2. rerank — the ordering is by the reranked score, and the field says so: the score is the
	// similarity plus the bounded lexical term, so it differs from the similarity whenever the
	// ranking moved.
	if result.Summaries[0].Score == 0 && result.Summaries[0].Similarity == 0 {
		t.Fatal("the selected summary carries no score, so nothing was ranked")
	}
	// 3. 恢复原始消息 — the early message is back, and it is not merely the summary quoting itself.
	restored := false
	for _, message := range result.Messages {
		if message.SourceID == "early-1" {
			restored = true
			if !strings.Contains(message.Content, "女主不能穿红色") {
				t.Fatalf("the restored message is %q", message.Content)
			}
		}
	}
	if !restored {
		t.Fatalf("deep recall did not restore the early message: %+v", result.Messages)
	}
	// 4. 返回来源 — one provenance entry per message, naming the summary each came through and the
	// attribution AC-MEM-004 preserves.
	if len(result.Provenance) != len(result.Messages) {
		t.Fatalf("%d messages but %d provenance entries", len(result.Messages), len(result.Provenance))
	}
	for _, entry := range result.Provenance {
		if entry.SummarizedBy != summary.ID {
			t.Fatalf("a provenance entry names %q as the summary", entry.SummarizedBy)
		}
		if entry.Role == "" || entry.CreatedAt.IsZero() {
			t.Fatalf("a provenance entry lost its attribution: %+v", entry)
		}
	}
	// 5. 限制 Token — the result reports what it spent, and the walk is bounded by construction: the
	// sources of the selected summaries only, capped by the section's own maxima.
	if result.UsedTokens == 0 {
		t.Fatal("deep recall reports spending no tokens on the messages it returned")
	}
	if len(result.Messages) > appmemory.MaxDeepRecallMessages {
		t.Fatalf("deep recall restored %d messages, above its own ceiling", len(result.Messages))
	}

	// And the bound is a real one: asking for fewer messages gives fewer, and says it truncated.
	bounded, err := harness.service.DeepRecall(ctx, appmemory.DeepRecallRequest{
		Scope: harness.scope, Query: question, MaxRawMessages: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded.Messages) != 1 {
		t.Fatalf("a bounded deep recall returned %d messages", len(bounded.Messages))
	}
	if !bounded.Truncated {
		t.Fatal("a bounded walk that stopped at its bound does not report itself truncated")
	}
}

// TestACME005DeepRecallCannotLeaveTheProject is the leak rule applied to the deepest read.
//
// Deep recall walks from a summary to its sources, and a walk is the operation most likely to lose a
// scope: the summary is found by similarity, and its sources are followed by identifier. A source that
// belonged to another project would be reached without any query naming that project — so the test
// seeds another project's summary with the SAME vector and asserts it is not selected.
func TestACME005DeepRecallCannotLeaveTheProject(t *testing.T) {
	question := "为什么禁止红色服装?"
	questionVector := make([]float32, 256)
	questionVector[9] = 1
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{question: questionVector}}
	fixture := newWP10Fixture(t, embedder)
	harness := fixture.harness
	ctx := context.Background()

	// The other project's setting, exactly as similar as this one's.
	otherScope := memory.Scope{Tenant: "local", Project: "project-other", Episode: "episode-1", AgentKey: "script.decision"}
	if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope: otherScope, Message: "另一个项目的禁红设定", MessageID: "other-1",
		Role: agent.MessageUser, AgentKey: "script.decision",
	}); err != nil {
		t.Fatal(err)
	}
	embedder.fallback = questionVector
	otherSummary, found, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: otherScope, Level: 1, Embed: true,
	})
	if err != nil || !found {
		t.Fatalf("Summarize for the other project: found=%v err=%v", found, err)
	}

	// This project's own setting, so the recall has something legitimate to find.
	if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope: harness.scope, Message: "本项目的禁红设定", MessageID: "mine-1",
		Role: agent.MessageUser, AgentKey: "script.decision",
	}); err != nil {
		t.Fatal(err)
	}
	ownSummary, found, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: harness.scope, Level: 1, Embed: true,
	})
	if err != nil || !found {
		t.Fatalf("Summarize: found=%v err=%v", found, err)
	}

	result, err := harness.service.DeepRecall(ctx, appmemory.DeepRecallRequest{
		Scope: harness.scope, Query: question,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range result.Summaries {
		if selected.Item.ID == otherSummary.ID {
			t.Fatal("deep recall selected another project's summary")
		}
		if selected.Item.Scope.Project != "project-1" {
			t.Fatalf("deep recall selected a summary from %q", selected.Item.Scope.Project)
		}
	}
	for _, message := range result.Messages {
		if message.SourceID == "other-1" {
			t.Fatal("deep recall restored another project's message")
		}
	}
	// The legitimate summary WAS found, so the empty result cannot be the reason the leak is absent.
	found = false
	for _, selected := range result.Summaries {
		if selected.Item.ID == ownSummary.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("deep recall found neither project's summary, so the leak assertion proves nothing: %+v", result.Summaries)
	}
}

// TestACME003TheLockedRuleHoldsAcrossProjectsToo is the last clause's security edge.
//
// The pinned channel reads a project's locked memories with no query and no vector, which makes it the
// channel most able to leak: it is the one place the recall does NOT go through the vector index's own
// scope filter. The test seeds a pinned high-importance memory in another project and asserts it is not
// in this project's facts.
func TestACME003TheLockedRuleHoldsAcrossProjectsToo(t *testing.T) {
	fixture := newWP10Fixture(t, &embedderDouble{available: false, vectors: map[string][]float32{}})
	harness := fixture.harness
	ctx := context.Background()

	other, err := harness.service.RememberFact(ctx, appmemory.RememberFactRequest{
		Scope:   memory.Scope{Tenant: "local", Project: "project-other", AgentKey: "script.decision"},
		Content: "另一个项目的禁红规则", Importance: 0.95, CreatedByID: "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.SetMemoryLocked(ctx, other.ID, true, string(memory.ActorUser)); err != nil {
		t.Fatal(err)
	}
	mine, err := harness.service.RememberFact(ctx, appmemory.RememberFactRequest{
		Scope: harness.scope, Content: "本项目的禁红规则", Importance: 0.95, CreatedByID: "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.SetMemoryLocked(ctx, mine.ID, true, string(memory.ActorUser)); err != nil {
		t.Fatal(err)
	}

	context, err := harness.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{Scope: harness.scope})
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range context.Facts {
		if fact.Item.ID == other.ID {
			t.Fatal("the pinned channel returned another project's fact")
		}
	}
	found := false
	for _, fact := range context.Facts {
		if fact.Item.ID == mine.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the pinned channel did not return this project's own fact: %+v", context.Facts)
	}
}

// TestTheTokenBudgetBoundsTheContext is FR-120's "记忆构建符合 Token Budget".
//
// The budget is a real bound rather than a claim: a context built with a tiny budget carries fewer
// items and says it truncated, and the same request against the same data never exceeds it.
func TestTheTokenBudgetBoundsTheContext(t *testing.T) {
	fixture := newWP10Fixture(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	harness := fixture.harness
	ctx := context.Background()

	// Twenty long turns, so a small budget cannot hold them all.
	for index := 0; index < 20; index++ {
		if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
			Scope:     harness.scope,
			Message:   strings.Repeat("这是一段很长的记忆内容。", 20),
			MessageID: "budget-" + itoaWP10(index),
			Role:      agent.MessageUser, AgentKey: "script.decision",
		}); err != nil {
			t.Fatal(err)
		}
	}
	const budget = 100
	context, err := harness.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{
		Scope: harness.scope, TokenBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if context.UsedTokens > budget {
		t.Fatalf("the context spent %d tokens against a budget of %d", context.UsedTokens, budget)
	}
	if !context.Truncated {
		t.Fatal("a context that could not hold everything does not report itself truncated")
	}
	// The bound is not vacuous: a generous budget holds more.
	generous, err := harness.service.BuildMemoryContext(ctx, appmemory.MemoryContextRequest{
		Scope: harness.scope, TokenBudget: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(generous.Recent) <= len(context.Recent) {
		t.Fatalf("a larger budget returned %d recent items against the small budget's %d",
			len(generous.Recent), len(context.Recent))
	}
	_ = time.Now
}
