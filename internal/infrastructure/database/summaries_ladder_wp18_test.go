package database

import (
	"context"
	"strconv"
	"strings"
	"testing"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// summaries_ladder_wp18_test.go covers FR-120's hierarchy: message → episode/session → project.
//
// # Why this file exists, and what it found
//
// The reconnaissance that preceded WP-18 established two facts with evidence:
//
//  1. THE SECOND LEVEL HAD NEVER RUN. Every `Level:` in the repository was `Level: 1` — six
//     occurrences across two files — so the middle rung of the ladder had no test of any kind.
//  2. THE SECOND LEVEL DID NOT MARK ITS CHILDREN, contradicting the comment two functions above it
//     in `summary.go`. `CreateSummaryWithSources` was called with `markSummarized = level == 1`, so a
//     level-two summary never set `summarized` on its sources — while `UnsummarisedItems` selects on
//     exactly that flag. **A second level-two run would condense the same children again**, producing
//     a second, near-identical summary of the same episode.
//
// The first test below is written to FAIL against the old behaviour, which is how the defect was
// confirmed rather than inferred. The rest cover the third rung, which is what the package is for.

// ladderIndex renders a small integer as the suffix a fixture id uses.
//
// It exists because the ids must be STABLE and readable in a failure message, and `string(rune('a'+i))`
// — which this file used first — produces a different id than a reader expects past the alphabet and
// is the kind of cleverness that costs an hour when it breaks.
func ladderIndex(value int) string {
	return strconv.Itoa(value)
}

// seedLadderMessages writes n episodic memories into a scope.
//
// It writes them through the repository rather than the bridge so the test can name the scope it
// wants: the ladder's whole subject is which scope a rung lands in.
func seedLadderMessages(t *testing.T, harness *wp10MemoryHarness, scope appmemory.Scope, prefix string, n int) []memory.MemoryItem {
	t.Helper()
	ctx := context.Background()
	repository := NewMemoryRepository(harness.db)
	written := make([]memory.MemoryItem, 0, n)
	for index := 0; index < n; index++ {
		record := memory.MemoryItem{
			ID:         prefix + "-" + ladderIndex(index),
			Type:       memory.TypeEpisodic,
			Scope:      scope,
			Role:       "user",
			Content:    prefix + " 第 " + ladderIndex(index) + " 条：渡口的调度继续推进。",
			Importance: 0.5,
			Confidence: 0.5,
			SourceType: memory.SourceMessage,
			SourceID:   prefix + "-msg-" + ladderIndex(index),
			CreatedAt:  dramaTime(),
			UpdatedAt:  dramaTime(),
			Revision:   1,
		}
		if err := repository.CreateItem(ctx, record); err != nil {
			t.Fatalf("CreateItem %s: %v", record.ID, err)
		}
		written = append(written, record)
	}
	return written
}

// TestASecondLevelTwoRunDoesNotRecondenseTheSameChildren is the defect this package found.
//
// # What it asserts
//
// A level-two summary covers its episode's unsummarised summaries. Once it exists, the SAME children
// must not be condensed again by a later run: the summary is the record of them, and a second
// summary of the same two rows is a duplicate that also lengthens the ladder without shortening it.
//
// # Why it failed before WP-18
//
// `CreateSummaryWithSources` was called with `markSummarized = level == 1`. Level one sets the flag
// on the messages it covers; level two set it on nothing, while `UnsummarisedItems` — the read that
// chooses what a summary covers — selects `summarized = 0`. So the second run read the same children
// and produced a second summary. The test is written to fail against that behaviour.
func TestASecondLevelTwoRunDoesNotRecondenseTheSameChildren(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	ctx := context.Background()
	scope := appmemory.ScopeFor("drama-project", "drama-episode", "agent-1")
	seedLadderMessages(t, harness, scope, "l2", 4)

	// Two level-one summaries, which is what level two needs as children.
	for index := 0; index < 2; index++ {
		if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: scope, Level: 1, Window: 2,
		}); err != nil {
			t.Fatalf("level one summarise %d: %v", index, err)
		} else if !created {
			t.Fatalf("level one summarise %d created nothing", index)
		}
	}

	first, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: scope, Level: 2,
	})
	if err != nil {
		t.Fatalf("level two summarise: %v", err)
	}
	if !created {
		t.Fatal("the first level-two summarise created nothing")
	}
	// The children are marked, so the next run has nothing to cover.
	second, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: scope, Level: 2,
	})
	if err != nil {
		t.Fatalf("the second level-two summarise: %v", err)
	}
	if created {
		t.Fatalf("a second level-two run condensed the SAME children again: %q", second.Content)
	}
	if second.ID != "" {
		t.Fatalf("a summarise that created nothing returned a row: %+v", second)
	}
	// And the first summary is still the one the store holds, with both children linked.
	sources, _, err := harness.service.ListSummarySources(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("the level-two summary cites %d children, want 2", len(sources))
	}
}

// TestTheThirdLevelSummarisesEpisodeSummaries is the project rung.
//
// # The ladder, and what each rung's scope must be
//
// A level-one summary covers one agent's conversation and stays in it. A level-two summary covers an
// episode's summaries and belongs to the EPISODE — episode set, agent and session cleared. A
// level-three summary covers a project's episode summaries and belongs to the PROJECT — episode
// cleared too. The scope is not a label: `scopeClauses` filters on the parts a scope NAMES, so a
// third-level row that kept an episode id would be invisible to a project-level read, which is the
// "written but unreachable" shape this repository keeps finding.
func TestTheThirdLevelSummarisesEpisodeSummaries(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	ctx := context.Background()
	// Two episodes, so the project rung has something that is genuinely project-wide.
	first := appmemory.ScopeFor("drama-project", "episode-1", "agent-1")
	second := appmemory.ScopeFor("drama-project", "episode-2", "agent-1")
	// FOUR messages per episode, summarised two at a time: the episode rung needs TWO children, so
	// one level-one summary is not enough for it to exist at all.
	seedLadderMessages(t, harness, first, "ep1", 4)
	seedLadderMessages(t, harness, second, "ep2", 4)

	for _, scope := range []appmemory.Scope{first, second} {
		for round := 0; round < 2; round++ {
			if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
				Scope: scope, Level: 1, Window: 2,
			}); err != nil || !created {
				t.Fatalf("level one %d for %s: created=%v err=%v", round, scope.Episode, created, err)
			}
		}
		if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: scope, Level: 2,
		}); err != nil || !created {
			t.Fatalf("level two for %s: created=%v err=%v", scope.Episode, created, err)
		}
	}

	project, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: first, Level: 3,
	})
	if err != nil {
		t.Fatalf("level three summarise: %v", err)
	}
	if !created {
		t.Fatal("the project summarise created nothing, and two episode summaries exist")
	}
	// THE SCOPE IS THE PROJECT'S: no episode, no agent, no session. A row that kept any of them
	// would be unreachable from a project-level read.
	if project.Scope.Episode != "" || project.Scope.AgentKey != "" || project.Scope.Session != "" {
		t.Fatalf("the project summary carries %q/%q/%q, and a project rung must name only the project",
			project.Scope.Episode, project.Scope.AgentKey, project.Scope.Session)
	}
	if project.Scope.Project != "drama-project" {
		t.Fatalf("the project summary is scoped to %q", project.Scope.Project)
	}
	if !strings.Contains(project.Content, "project") {
		t.Fatalf("the project summary's text does not say which rung it is: %q", project.Content)
	}
	// It cites the two EPISODE summaries, which is the rung below it.
	sources, _, err := harness.service.ListSummarySources(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("the project summary cites %d children, want the two episode summaries", len(sources))
	}
	// And a second project run does not recondense them, the same property level two now keeps.
	if _, again, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: first, Level: 3,
	}); err != nil {
		t.Fatal(err)
	} else if again {
		t.Fatal("a second project summarise condensed the same children again")
	}
}

// TestAProjectSummaryIsReachableFromAnEpisodeQuestion is the reachability half.
//
// # Why this test exists at all
//
// Building a rung nobody can read is the failure this repository has recorded five times. A
// project-level summary is stored with an EMPTY episode, and `scoredCandidates` filters on the parts
// a scope names — so a deep recall that forced `EpisodeOnly()` (which is what it did before WP-18)
// could never find one. The rung would exist, its links would be correct, and no question could reach
// it.
//
// The rule the fix states: the semantic search runs in the scope the CALLER asked in. A caller that
// named an episode still reaches project-level rows, because an unnamed part is not a filter.
func TestAProjectSummaryIsReachableFromAnEpisodeQuestion(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	ctx := context.Background()
	scope := appmemory.ScopeFor("drama-project", "episode-1", "agent-1")
	second := appmemory.ScopeFor("drama-project", "episode-2", "agent-1")
	// FOUR messages per episode and TWO level-one summaries each: the episode rung needs two
	// children and the project rung needs two of those.
	seedLadderMessages(t, harness, scope, "reach", 4)
	seedLadderMessages(t, harness, second, "reach2", 4)
	for _, episodeScope := range []appmemory.Scope{scope, second} {
		for round := 0; round < 2; round++ {
			if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
				Scope: episodeScope, Level: 1, Window: 2, Embed: true,
			}); err != nil || !created {
				t.Fatalf("level one %d for %s: created=%v err=%v",
					round, episodeScope.Episode, created, err)
			}
		}
	}
	for _, episodeScope := range []appmemory.Scope{scope, second} {
		if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: episodeScope, Level: 2, Embed: true,
		}); err != nil || !created {
			t.Fatalf("level two: created=%v err=%v", created, err)
		}
	}
	project, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: scope, Level: 3, Embed: true,
	})
	if err != nil || !created {
		t.Fatalf("level three: created=%v err=%v", created, err)
	}

	// The question is asked in an EPISODE scope, which is the ordinary case: the user is working in
	// one episode. The project rung must still be findable.
	result, err := harness.service.DeepRecall(ctx, appmemory.DeepRecallRequest{
		Scope: scope, Query: "渡口的调度继续推进",
	})
	if err != nil {
		t.Fatalf("DeepRecall: %v", err)
	}
	found := false
	for _, selected := range result.Summaries {
		if selected.Item.ID == project.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the project-level summary is unreachable from an episode-scoped question: %+v", result.Summaries)
	}
}

// TestTheThirdLevelNeedsTwoChildren pins the rung's own floor.
//
// The rule level two already keeps: a summary of ONE summary is a copy with an extra hop. It would
// make the ladder longer without making it shorter, which is the whole reason the ladder exists.
func TestTheThirdLevelNeedsTwoChildren(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	ctx := context.Background()
	scope := appmemory.ScopeFor("drama-project", "episode-1", "agent-1")
	seedLadderMessages(t, harness, scope, "one", 4)
	for round := 0; round < 2; round++ {
		if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: scope, Level: 1, Window: 2,
		}); err != nil || !created {
			t.Fatalf("level one %d: created=%v err=%v", round, created, err)
		}
	}
	if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: scope, Level: 2,
	}); err != nil || !created {
		t.Fatalf("level two: created=%v err=%v", created, err)
	}
	// One episode summary exists, so the project rung has nothing to condense.
	if summary, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: scope, Level: 3,
	}); err != nil {
		t.Fatalf("level three: %v", err)
	} else if created {
		t.Fatalf("a project summary was made from ONE child: %q", summary.Content)
	}
}

// TestALevelFourIsRefused is the ladder's ceiling.
//
// FR-120's ladder ends at the project, and a caller that asked for a fourth rung has made a mistake
// rather than found an unimplemented feature — so it is refused with the reason, not answered with
// silence.
//
// # Why this test asserts WHICH LAYER refuses, and why that took a mutation to notice
//
// The first version asserted only that an error came back, and a mutation removing the command's own
// guard left it GREEN — because the STORE validates the rung as well (`UnsummarisedItemsForLevel`
// refuses a level the vocabulary does not have) and its refusal arrives at the same caller through the
// same return value. The two guards are deliberate defence in depth: the command refuses before it
// builds anything, and the store refuses because it is the only thing standing between a hand-written
// query and a level the schema's CHECK would reject.
//
// But an assertion that cannot tell them apart cannot see the command's guard being deleted, and the
// consequence of losing it is real: without it, `level: 4` reaches the WINDOW, which reads
// `summary_level = 3` and silently returns nothing — so the caller gets `created=false` from a bad
// request instead of a refusal. The message is what distinguishes the two, so the message is what
// this test checks.
func TestALevelFourIsRefused(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	ctx := context.Background()
	scope := appmemory.ScopeFor("drama-project", "episode-1", "agent-1")
	for _, level := range []int{4, -1} {
		_, _, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: scope, Level: level,
		})
		if err == nil {
			t.Fatalf("level %d was accepted", level)
		}
		// The COMMAND's refusal, not the store's. Its wording is the one that names the ladder, and
		// the store's is the one that names the level — so a test matching "level" would pass on
		// either, which is exactly how the first version of this test was blind.
		if !strings.Contains(err.Error(), "of messages, of episode summaries, or of a project's episodes") {
			t.Fatalf("level %d was refused by %q, and the command's own guard is what must refuse it",
				level, err.Error())
		}
	}
	// And a valid rung still works, so the guard is not refusing everything.
	seedLadderMessages(t, harness, scope, "ceiling", 2)
	if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: scope, Level: 1, Window: 2,
	}); err != nil || !created {
		t.Fatalf("the message rung was refused by the same guard: created=%v err=%v", created, err)
	}
}

// TestTheProjectRungDoesNotCondenseItsOwnOutput is the defect three rungs expose.
//
// # Why two rungs were safe and three are not
//
// A summary is `type = summary` at every rung above the first, and the columns that would tell the
// rungs apart do not: the memory centre sends no episode and no agent, so a level-one summary and a
// level-two summary written from that surface carry the SAME scope. So "the unsummarised summaries
// in this scope" identifies no rung at all — it returns whatever is uncondensed.
//
// With two rungs that was harmless by accident: after a level-two run, the only uncondensed summary
// in the episode is the level-two one, and the "at least two children" rule refused to condense it
// alone. **A third rung removes the accident.** Once a project rung exists and another episode
// summary appears, the project rung's window holds the PREVIOUS PROJECT SUMMARY and the new episode
// summary — two rows, so the floor is satisfied, and the run condenses its own earlier output with a
// child as if the two were siblings.
//
// # What the test demands
//
// A rung reads exactly the rung below it. The project rung's second run must see ONE new child (the
// new episode summary) rather than a child plus its own predecessor, and one child is below the
// floor — so it creates nothing. A THIRD episode summary then gives it two genuine children, and the
// summary it makes must cite those two and not the earlier project summary.
func TestTheProjectRungDoesNotCondenseItsOwnOutput(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	ctx := context.Background()
	first := appmemory.ScopeFor("drama-project", "episode-1", "agent-1")

	// One episode, summarised twice: message rung, then episode rung.
	seedLadderMessages(t, harness, first, "own1", 4)
	for round := 0; round < 2; round++ {
		if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: first, Level: 1, Window: 2,
		}); err != nil || !created {
			t.Fatalf("level one %d for episode one: created=%v err=%v", round, created, err)
		}
	}
	if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: first, Level: 2,
	}); err != nil || !created {
		t.Fatalf("level two for episode one: created=%v err=%v", created, err)
	}
	// A second episode gives the project rung two genuine children.
	second := appmemory.ScopeFor("drama-project", "episode-2", "agent-1")
	seedLadderMessages(t, harness, second, "own2", 4)
	for round := 0; round < 2; round++ {
		if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: second, Level: 1, Window: 2,
		}); err != nil || !created {
			t.Fatalf("level one %d for episode two: created=%v err=%v", round, created, err)
		}
	}
	if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: second, Level: 2,
	}); err != nil || !created {
		t.Fatalf("level two for episode two: created=%v err=%v", created, err)
	}
	project, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: first, Level: 3,
	})
	if err != nil || !created {
		t.Fatalf("the first project summarise: created=%v err=%v", created, err)
	}

	// A THIRD episode summary arrives. The project rung now has exactly ONE new child.
	third := appmemory.ScopeFor("drama-project", "episode-3", "agent-1")
	seedLadderMessages(t, harness, third, "own3", 4)
	for round := 0; round < 2; round++ {
		if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: third, Level: 1, Window: 2,
		}); err != nil || !created {
			t.Fatalf("level one %d for episode three: created=%v err=%v", round, created, err)
		}
	}
	if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: third, Level: 2,
	}); err != nil || !created {
		t.Fatalf("level two for episode three: created=%v err=%v", created, err)
	}
	if leaked, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: first, Level: 3,
	}); err != nil {
		t.Fatalf("the second project summarise: %v", err)
	} else if created {
		t.Fatalf("the project rung condensed its OWN earlier output: %q", leaked.Content)
	}

	// A FOURTH episode summary gives it two genuine children, and the summary must cite those two
	// rather than the earlier project summary.
	fourth := appmemory.ScopeFor("drama-project", "episode-4", "agent-1")
	seedLadderMessages(t, harness, fourth, "own4", 4)
	for round := 0; round < 2; round++ {
		if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: fourth, Level: 1, Window: 2,
		}); err != nil || !created {
			t.Fatalf("level one %d for episode four: created=%v err=%v", round, created, err)
		}
	}
	if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: fourth, Level: 2,
	}); err != nil || !created {
		t.Fatalf("level two for episode four: created=%v err=%v", created, err)
	}
	next, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: first, Level: 3,
	})
	if err != nil || !created {
		t.Fatalf("the third project summarise: created=%v err=%v", created, err)
	}
	sources, _, err := harness.service.ListSummarySources(ctx, next.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.SourceMemoryID == project.ID {
			t.Fatal("the new project summary cites the PREVIOUS project summary as a child")
		}
	}
	if len(sources) != 2 {
		t.Fatalf("the new project summary cites %d children, want the two new episode summaries", len(sources))
	}
}

// TestTheWidenedSearchDoesNotReachAnotherAgent is the leak half of the reachability fix.
//
// # Why this test exists
//
// Making the project rung reachable required SEARCHING wider — a project-level summary has an empty
// episode, so an episode-scoped search cannot see it. Searching wider is how a memory leaks: a
// project-wide vector search matches every episode AND every agent key in the project, and the recall
// would then hand one agent another's turns.
//
// `keepInRecallScope` is what stops that, and a mutation that made it keep every candidate left the
// whole suite green — because the test that prompted the fix asserted only that the project rung WAS
// found, never that its neighbours were NOT. This is that half: two other agents and one other
// episode, each holding a summary that the query matches exactly, and none of them may come back.
func TestTheWidenedSearchDoesNotReachAnotherAgent(t *testing.T) {
	harness := newWP10MemoryHarness(t, &embedderDouble{available: true, vectors: map[string][]float32{}})
	ctx := context.Background()
	question := "渡口的调度继续推进"
	// ONE vector for everything, so the only thing that can separate these rows is the filter: a hash
	// would let the test pass because the vectors differed rather than because the filter worked.
	shared := make([]float32, 256)
	shared[3] = 1
	harness.embedder.(*embedderDouble).fallback = shared

	mine := appmemory.ScopeFor("drama-project", "episode-1", "agent-1")
	// Three scopes a widened search would reach and the caller may not see: the same episode under a
	// different agent, a different episode under the same agent, and a different episode under a
	// different agent.
	foreign := []appmemory.Scope{
		appmemory.ScopeFor("drama-project", "episode-1", "agent-2"),
		appmemory.ScopeFor("drama-project", "episode-2", "agent-1"),
		appmemory.ScopeFor("drama-project", "episode-2", "agent-2"),
	}
	foreignSummaryIDs := map[string]bool{}
	for index, scope := range foreign {
		seedLadderMessages(t, harness, scope, "leak"+ladderIndex(index), 4)
		for round := 0; round < 2; round++ {
			if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
				Scope: scope, Level: 1, Window: 2, Embed: true,
			}); err != nil || !created {
				t.Fatalf("level one %d for %s: created=%v err=%v", round, scope.AgentKey, created, err)
			}
		}
		if _, created, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
			Scope: scope, Level: 2, Embed: true,
		}); err != nil || !created {
			t.Fatalf("level two for %s: created=%v err=%v", scope.AgentKey, created, err)
		}
	}

	result, err := harness.service.DeepRecall(ctx, appmemory.DeepRecallRequest{
		Scope: mine, Query: question,
	})
	if err != nil {
		t.Fatalf("DeepRecall: %v", err)
	}
	// The caller's own episode is empty until it is summarised, so the search finds the foreign rows
	// and the filter is the only thing that can reject them — which is what makes this test the leak
	// test rather than a restatement of the scope query.
	for _, selected := range result.Summaries {
		scope := selected.Item.Scope
		if scope.Episode == "" && scope.AgentKey == "" {
			continue
		}
		if scope.Episode != mine.Episode || scope.AgentKey != mine.AgentKey {
			t.Fatalf("the recall returned %q/%q to a caller in %q/%q",
				scope.Episode, scope.AgentKey, mine.Episode, mine.AgentKey)
		}
	}
	for _, message := range result.Messages {
		scope := message.Scope
		if scope.Episode == "" && scope.AgentKey == "" {
			continue
		}
		if scope.Episode != mine.Episode || scope.AgentKey != mine.AgentKey {
			t.Fatalf("the recall restored a message from %q/%q to a caller in %q/%q",
				scope.Episode, scope.AgentKey, mine.Episode, mine.AgentKey)
		}
	}
	_ = foreignSummaryIDs
}
