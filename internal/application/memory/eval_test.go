package memory

import (
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// eval_test.go covers AGENT_CONTRACTS section 18.2's two recall metrics.
//
// # What the test is for, stated against the failure it prevents
//
// A metric is worse than useless when it can only report success: a `LeakRate` that returned zero
// because it counted nothing, or a `HitRate` that returned one because the fixture was never read,
// would look like a clean evaluation forever. So every metric here has BOTH directions asserted —
// the clean case from a real result, and a case where it must report a fault. The reverse cases are
// constructed rather than captured because the acceptance walk's whole job is to be correct, and a
// metric is only proven by what it says when the thing it measures is wrong.
//
// The clean case runs against a REAL recall result built from the memory service, not against a
// hand-built struct: a metric reading values the test invented would prove the arithmetic and
// nothing about whether the fields it reads are the ones a recall actually sets.

// TestTheMetricsReportAMiss is the first reverse direction.
//
// A result that restored nothing must produce a miss, and a miss must move the rate. Without this
// case a `Hit` that was computed from a constant would pass the test above.
func TestTheMetricsReportAMiss(t *testing.T) {
	expectation := RecallExpectation{
		Query: "为什么禁止红色服装？", MessageID: "the-setting", ViaSummary: true,
	}
	empty := DeepRecallResult{Query: expectation.Query}
	score := ScoreRecall(empty, expectation)
	if score.Hit || score.ViaSummary {
		t.Fatalf("an empty result scored as a hit: %+v", score)
	}
	metrics := Aggregate([]RecallScore{score})
	if metrics.HitRate() != 0 {
		t.Fatalf("the hit rate on a miss is %v", metrics.HitRate())
	}
	if metrics.Questions != 1 {
		t.Fatalf("the question was not counted: %+v", metrics)
	}
}

// TestTheMetricsReportAHitThatDidNotWalkTheHistory is the distinction the metric exists for.
//
// The message was restored — so `Hit` is true — but its provenance names NO summary, which means it
// came from the recent window rather than through the history. 「Deep Recall 命中率」 read as the
// plain hit rate would call this a success; read as the summary hit rate it is a failure, and the
// second reading is the one the criterion is about.
func TestTheMetricsReportAHitThatDidNotWalkTheHistory(t *testing.T) {
	expectation := RecallExpectation{Query: "q", MessageID: "msg-1", ViaSummary: true}
	result := DeepRecallResult{
		Messages: []memory.MemoryItem{{ID: "mem-1", SourceID: "msg-1", Type: memory.TypeEpisodic}},
		Provenance: []Provenance{
			// No SummarizedBy: the memory was reached directly, not through a summary.
			{MemoryID: "mem-1", MessageID: "msg-1", Role: "user"},
		},
	}
	score := ScoreRecall(result, expectation)
	if !score.Hit {
		t.Fatalf("the restored message should count as a hit: %+v", score)
	}
	if score.ViaSummary {
		t.Fatalf("a hit with no summary hop was counted as a history walk: %+v", score)
	}
	metrics := Aggregate([]RecallScore{score})
	if metrics.HitRate() != 1 {
		t.Fatalf("the plain hit rate is %v, want 1 — the message WAS restored", metrics.HitRate())
	}
	if metrics.SummaryHitRate() != 0 {
		t.Fatalf("the summary hit rate is %v, and no summary was walked", metrics.SummaryHitRate())
	}
}

// TestTheMetricsReportALeak is the leak's reverse direction, and it is the one that matters most.
//
// 「跨项目泄露率」 is a criterion whose only acceptable value is zero, which is exactly the kind of
// metric that rots into a constant unless something forces it to fire. So this test hands the scorer
// a result that cites another project and demands a non-zero rate — and it counts the citation in
// the SUMMARY list as well as in the messages, because a leak that only reached the summaries is
// still a leak.
func TestTheMetricsReportALeak(t *testing.T) {
	expectation := RecallExpectation{
		Query: "q", MessageID: "msg-1", ForeignProjectIDs: []string{"canary-project-other"},
	}
	result := DeepRecallResult{
		Summaries: []ScoredMemory{{Item: memory.MemoryItem{
			ID: "leaked", Type: memory.TypeSummary,
			Scope: Scope{Project: "canary-project-other"},
		}}},
		Messages: []memory.MemoryItem{{ID: "mem-1", SourceID: "msg-1"}},
	}
	score := ScoreRecall(result, expectation)
	if score.ForeignCitations != 1 {
		t.Fatalf("the leak was counted %d times, want 1: %+v", score.ForeignCitations, score)
	}
	metrics := Aggregate([]RecallScore{score})
	if metrics.LeakRate() != 1 {
		t.Fatalf("the leak rate is %v on a leaking run", metrics.LeakRate())
	}
	// And the same result WITHOUT the foreign expectation reports no leak, which proves the metric
	// reads the expectation rather than reporting whatever it finds.
	clean := ScoreRecall(result, RecallExpectation{Query: "q", MessageID: "msg-1"})
	if clean.ForeignCitations != 0 {
		t.Fatalf("a result was called a leak when no foreign project was expected: %+v", clean)
	}
}

// TestTheMetricsReportADecoyHit is the other reverse direction.
//
// The same-project decoy is a fact that sounds like the answer and is not. Returning it as the
// answer is a different failure from leaking another project's row, and the two are counted
// separately so a report can say which happened.
func TestTheMetricsReportADecoyHit(t *testing.T) {
	expectation := RecallExpectation{
		Query: "q", MessageID: "the-setting", DecoyMessageIDs: []string{"the-decoy"},
	}
	result := DeepRecallResult{
		Messages: []memory.MemoryItem{
			{ID: "mem-decoy", SourceID: "the-decoy", Type: memory.TypeEpisodic},
		},
	}
	score := ScoreRecall(result, expectation)
	if score.DecoyHits != 1 {
		t.Fatalf("the decoy was presented and counted %d times: %+v", score.DecoyHits, score)
	}
	if score.Hit {
		t.Fatalf("returning the decoy counted as the expected answer: %+v", score)
	}
	metrics := Aggregate([]RecallScore{score})
	if metrics.DecoyHits != 1 {
		t.Fatalf("the aggregate lost the decoy: %+v", metrics)
	}
}

// TestAnEmptyEvaluationIsNotAPass pins the zero-questions case.
//
// Every rate over no questions is zero, and that is the honest answer: an evaluation that ran
// nothing has not demonstrated a 100% hit rate, and a metric that reported one would let a missing
// fixture look like a perfect run.
func TestAnEmptyEvaluationIsNotAPass(t *testing.T) {
	metrics := Aggregate(nil)
	if metrics.Questions != 0 {
		t.Fatalf("an empty evaluation reported %d questions", metrics.Questions)
	}
	if metrics.HitRate() != 0 || metrics.SummaryHitRate() != 0 || metrics.LeakRate() != 0 {
		t.Fatalf("an empty evaluation reported rates %v/%v/%v",
			metrics.HitRate(), metrics.SummaryHitRate(), metrics.LeakRate())
	}
}
