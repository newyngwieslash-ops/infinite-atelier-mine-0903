package memory

import (
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// eval.go scores one deep recall against what the caller expected of it.
//
// # Why this exists
//
// AGENT_CONTRACTS section 18.2 lists ten metrics, and two of them are about recall:
//
//	Memory 跨项目泄露率;
//	Deep Recall 命中率.
//
// Until WP-18 neither was implemented anywhere. The repository had a well-formed FIXTURE —
// `testdata/canary-drama/memory-recall.json`, which names the setting to bury, the question to ask,
// two decoys each with the reason it is a decoy, and the maximum number of raw messages — and a
// single integration test that walked the scenario and asserted a handful of properties by hand.
// What was missing was anything that turned an observation into a NUMBER, so "命中率" could be
// answered about a run rather than asserted about a test.
//
// # Why it is a pure function over values rather than a service
//
// The whole input to a metric is (what a recall returned) plus (what was expected). Both are
// already values: `DeepRecallResult` is what the service hands back, and the expectations are read
// off the fixture. A metric that needed a database and a provider would be a SECOND implementation
// of recall, and it would then be grading itself.
//
// # What it deliberately does not measure
//
// Section 18.2's other eight metrics — schema pass rate, tool selection, stage skips, supervisor
// localisation, and the rest — belong to the packages that own those behaviours. Claiming them here
// would be claiming coverage this file does not have, and the ADR says so rather than leaving a
// reader to assume ten.

// RecallExpectation is what one question was supposed to bring back.
//
// It is the fixture's shape (`mustRecall` plus the decoys) as a value this package can compare
// against. The field names are the fixture's own, so a reader can hold the two side by side.
type RecallExpectation struct {
	// Query is the question that was asked, carried so a score can quote it.
	Query string
	// MessageID is the transcript row the answer has to reach: AC-MEM-005's "恢复原始消息".
	MessageID string
	// ViaSummary requires the answer to have come through a summary rather than from the recent
	// window. Without it a recall that merely echoed the last turn would score as a hit.
	ViaSummary bool
	// ReturnsSource requires the result to carry provenance for the message it restored.
	ReturnsSource bool
	// DecoyMessageIDs are rows that must NOT be presented as the answer — a similarly worded fact in
	// the same project. They are separate from `ForeignProjectIDs` because the two failures have
	// different causes and different owners.
	DecoyMessageIDs []string
	// ForeignProjectIDs are projects whose rows must not appear AT ALL: the cross-project leak
	// DOMAIN_MODEL section 14.5 forbids. A single hit here is the whole of 「跨项目泄露率」's
	// numerator.
	ForeignProjectIDs []string
}

// RecallScore is one question's outcome.
//
// The booleans are separate rather than folded into one pass/fail because the metrics section asks
// for RATES: a run that hit the right message through the wrong path is a different defect from one
// that missed it, and averaging them together would report a number nobody can act on.
type RecallScore struct {
	Query string
	// Hit reports that the expected message was restored.
	Hit bool
	// ViaSummary reports that it was reached through a summary — the walk AC-MEM-005 describes —
	// rather than found another way. It is false when Hit is false.
	ViaSummary bool
	// ReturnedSource reports that provenance accompanied the restored message.
	ReturnedSource bool
	// DecoyHits counts rows from `DecoyMessageIDs` that the result presented.
	DecoyHits int
	// ForeignCitations counts results citing a project in `ForeignProjectIDs`. This is the
	// leak, counted per citation rather than per run so one result can report more than one.
	ForeignCitations int
	// SummaryCount and MessageCount are what the run returned, carried so a report can show the
	// SHAPE of a miss rather than only that it missed.
	SummaryCount int
	MessageCount int
}

// ScoreRecall grades one deep recall against one expectation.
//
// The three positive checks are independent, and that is deliberate: a recall can restore the right
// message (Hit) while having found it in the recent window rather than through the history it was
// asked about (ViaSummary false), and reporting that as a plain hit would make the metric blind to
// the thing deep recall exists for.
func ScoreRecall(result DeepRecallResult, expectation RecallExpectation) RecallScore {
	score := RecallScore{
		Query:        expectation.Query,
		SummaryCount: len(result.Summaries),
		MessageCount: len(result.Messages),
	}
	// Where each restored message came from, so "via a summary" is read from the PROVENANCE rather
	// than inferred from the fact that summaries were also returned: a run that returned a summary
	// and separately restored the message from its own row did not walk the summary to it.
	reachedViaSummary := map[string]bool{}
	for _, entry := range result.Provenance {
		if entry.MessageID != "" && strings.TrimSpace(entry.SummarizedBy) != "" {
			reachedViaSummary[entry.MessageID] = true
		}
		// A memory may carry its own source id instead of a transcript id; both name the row the
		// answer came from, and AC-MEM-005 asks for the row.
		if entry.MemoryID != "" && strings.TrimSpace(entry.SummarizedBy) != "" {
			reachedViaSummary[entry.MemoryID] = true
		}
	}
	for _, message := range result.Messages {
		if message.SourceID != expectation.MessageID && message.ID != expectation.MessageID {
			continue
		}
		score.Hit = true
		if reachedViaSummary[message.ID] || reachedViaSummary[message.SourceID] {
			score.ViaSummary = true
		}
		// The source travels with the message when the result carries provenance for it at all.
		for _, entry := range result.Provenance {
			if entry.MemoryID == message.ID {
				score.ReturnedSource = true
			}
		}
	}

	// The decoys: rows that must not be presented as the answer.
	decoySet := map[string]bool{}
	for _, id := range expectation.DecoyMessageIDs {
		decoySet[id] = true
	}
	for _, message := range result.Messages {
		if decoySet[message.SourceID] || decoySet[message.ID] {
			score.DecoyHits++
		}
	}

	// The leak: a citation naming another project. It is counted over EVERY row the result carries
	// — summaries, messages and provenance — because a leak that only reached the summary list is
	// still a leak, and one counted only in the messages would report zero for it.
	foreign := map[string]bool{}
	for _, id := range expectation.ForeignProjectIDs {
		foreign[id] = true
	}
	countForeign := func(projectID string) {
		if projectID != "" && foreign[projectID] {
			score.ForeignCitations++
		}
	}
	for _, summary := range result.Summaries {
		countForeign(summary.Item.Scope.Project)
	}
	for _, message := range result.Messages {
		countForeign(message.Scope.Project)
	}
	return score
}

// RecallMetrics aggregates scores into the two rates section 18.2 names.
type RecallMetrics struct {
	// Questions is how many were asked.
	Questions int
	// Hits is how many restored the expected message.
	Hits int
	// HitsThroughSummary is how many did it through a summary, which is the stricter reading of
	// 「Deep Recall 命中率」: a recall that answered from the recent window has not demonstrated the
	// history walk.
	HitsThroughSummary int
	// SourcesReturned is how many carried provenance with the restored message.
	SourcesReturned int
	// DecoyHits is the total number of decoy rows presented across every question.
	DecoyHits int
	// ForeignCitations is the total number of cross-project citations: 「跨项目泄露率」's numerator.
	ForeignCitations int
	// LeakingQuestions is how many questions had at least one foreign citation, which is the rate a
	// reader usually means by "how often does this leak".
	LeakingQuestions int
}

// Aggregate folds per-question scores into the metrics.
//
// Every rate is computed from its own numerator and denominator rather than averaged from
// per-question rates: a question that returned nothing at all should not weigh the same as one that
// returned twenty rows and cited one foreign project, and an average of ratios cannot tell them
// apart.
func Aggregate(scores []RecallScore) RecallMetrics {
	metrics := RecallMetrics{Questions: len(scores)}
	for _, score := range scores {
		if score.Hit {
			metrics.Hits++
		}
		if score.ViaSummary {
			metrics.HitsThroughSummary++
		}
		if score.ReturnedSource {
			metrics.SourcesReturned++
		}
		metrics.DecoyHits += score.DecoyHits
		metrics.ForeignCitations += score.ForeignCitations
		if score.ForeignCitations > 0 {
			metrics.LeakingQuestions++
		}
	}
	return metrics
}

// HitRate is the proportion of questions that restored the expected message.
func (m RecallMetrics) HitRate() float64 {
	if m.Questions == 0 {
		// Zero questions is not a 100% pass rate and not a 0% failure rate; it is an unknown, and
		// the caller has to see that rather than a number it would read as an answer.
		return 0
	}
	return float64(m.Hits) / float64(m.Questions)
}

// SummaryHitRate is the proportion that reached the message THROUGH a summary.
//
// It is the rate 「Deep Recall 命中率」 should be read as: the criterion exists to prove the history
// walk, and a recall that answered from the recent window has not demonstrated it.
func (m RecallMetrics) SummaryHitRate() float64 {
	if m.Questions == 0 {
		return 0
	}
	return float64(m.HitsThroughSummary) / float64(m.Questions)
}

// LeakRate is the proportion of questions that cited another project at all.
//
// Zero is the only acceptable value, and the metric is stated as a rate rather than a count so a
// report over a large evaluation cannot make one leak look like a rounding error.
func (m RecallMetrics) LeakRate() float64 {
	if m.Questions == 0 {
		return 0
	}
	return float64(m.LeakingQuestions) / float64(m.Questions)
}

// RecallFixtureDecoy is one decoy in the fixture's own vocabulary.
type RecallFixtureDecoy struct {
	MessageID string `json:"messageId"`
	Reason    string `json:"reason"`
}

// RecallFixtureMustRecall is the fixture's `mustRecall` block.
type RecallFixtureMustRecall struct {
	MessageID     string `json:"messageId"`
	ViaSummary    bool   `json:"viaSummary"`
	ReturnsSource bool   `json:"returnsSource"`
}

// ExpectationsFromFixture reads the canary fixture's recall claims into expectations.
//
// # Why this conversion lives in production code rather than in a test
//
// The fixture is `testdata/canary-drama/memory-recall.json`, generated by
// `scripts/gen-canary-fixture.mjs` and checked by `--check` on every verification run. Turning it
// into expectations is a small, total function over a document whose shape is the generator's
// contract, and a test that re-implemented it would be a second reader of the same JSON — free to
// agree by accident and to disagree silently after a fixture change. Keeping it here means one
// reader, and the test that uses it asserts the fixture drives the metrics rather than asserting
// how they are parsed.
//
// The `mustRecall` and decoy `reason` fields are the fixture's own vocabulary; a value it does not
// carry is left at its zero, because inventing one would be measuring something the fixture never
// claimed.
func ExpectationsFromFixture(fixture RecallFixture) RecallExpectation {
	expectation := RecallExpectation{
		Query:         fixture.Question,
		MessageID:     fixture.MustRecall.MessageID,
		ViaSummary:    fixture.MustRecall.ViaSummary,
		ReturnsSource: fixture.MustRecall.ReturnsSource,
	}
	// EVERY decoy is a same-project decoy from the metric's point of view, and the other project is
	// named ONCE at the top level.
	//
	// That is the fixture's actual shape, which a first version of this function got wrong: it
	// looked for a per-decoy `projectId` and found none, because the generator writes only
	// `messageId`, `role`, `content` and `reason` on a decoy. The two decoys differ by CONVENTION —
	// `canary-decoy-other-project` against `canary-decoy-same-project` — and by their `reason`,
	// which says which failure each one is for.
	//
	// So the assertion this feeds is the one that matters: `otherProjectId` is a project whose rows
	// must never appear, and every decoy message id is a row that must not be presented as the
	// answer. A result that returned the other-project decoy fails BOTH checks, which is correct:
	// it is a decoy AND it is the leak.
	for _, decoy := range fixture.Decoys {
		expectation.DecoyMessageIDs = append(expectation.DecoyMessageIDs, decoy.MessageID)
	}
	if otherProject := strings.TrimSpace(fixture.OtherProjectID); otherProject != "" && otherProject != fixture.ProjectID {
		expectation.ForeignProjectIDs = append(expectation.ForeignProjectIDs, otherProject)
	}
	return expectation
}

// RecallFixture is the canary fixture's shape, as `testdata/canary-drama/memory-recall.json` writes
// it (AGENT_CONTRACTS section 18.1: "Memory recall 问题").
//
// Only the fields the METRICS need are here. The fixture carries more — the burying turns, the note
// — and a struct that mirrored all of it would be a second copy of the generator's output shape,
// drifting from it one field at a time.
type RecallFixture struct {
	ProjectID string `json:"projectId"`
	// OtherProjectID is the project a decoy lives in, named at the top level as well as per decoy.
	OtherProjectID string `json:"otherProjectId"`
	Question       string `json:"question"`
	// MustRecall is what the question has to bring back. It is a named type rather than an inline
	// struct so a caller can construct one in a test without restating the fields — which is what
	// the first version of the database-package test had to do, and restating a shape in two places
	// is how the two drift.
	MustRecall RecallFixtureMustRecall `json:"mustRecall"`
	// Decoys are the rows that must NOT be the answer. Each states which failure it is FOR, which is
	// the fixture's own discipline: a decoy whose fault is only implicit cannot be checked, because
	// a test that passes proves the file was read rather than that the claim about it was true.
	Decoys []RecallFixtureDecoy `json:"decoys"`
}

// MemoryTypeNames is a compile-time anchor that this file is about MEMORIES.
//
// It is a no-op and it is here because the metrics are stated in terms of the memory domain's own
// type: a reader should not have to guess whether 「召回」 in this file means the same thing it
// means in `domain/memory`.
var _ = memory.TypeEpisodic
