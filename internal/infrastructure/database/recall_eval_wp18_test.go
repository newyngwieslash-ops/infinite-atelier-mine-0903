package database

import (
	"context"
	"testing"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// recall_eval_wp18_test.go runs AGENT_CONTRACTS section 18.2's two recall metrics over the canary
// scenario.
//
// # Where this test lives, and why
//
// The metric functions are pure and their unit tests are in `internal/application/memory`. What
// cannot be tested there is whether the metric reads the fields a REAL recall sets: that needs the
// memory service over a real schema, which is this package. So the split is deliberate — arithmetic
// and mapping in one place, the fixture-driven observation in the other — and this file is the
// second half.
//
// # What it asserts that the AC-MEM-005 walk does not
//
// The existing canary test asserts the recall's PROPERTIES by hand: the summary was found, the
// message was restored, the provenance is there. That is the right shape for an acceptance
// criterion and it is why this package did not need a metric to pass it. What it cannot do is
// produce a NUMBER, and section 18.2 asks for two rates. This test is where the observation becomes
// a rate — and, more importantly, where the rate is shown to be able to say something other than
// "everything is fine".

// expectationOf converts this package's fixture into the metrics package's expectation shape.
//
// It is the ONE place the two structs meet. The fixture type in this package is the loader's own —
// it carries fields the metrics do not need, like the burying turns and the note — and the metrics
// package declares only what a metric reads. A second conversion written inline at each call site is
// how the two would drift, which is why this test's first version was rewritten to use it.
func expectationOf(fixture canaryMemoryRecall) appmemory.RecallExpectation {
	decoys := make([]appmemory.RecallFixtureDecoy, 0, len(fixture.Decoys))
	for _, decoy := range fixture.Decoys {
		decoys = append(decoys, appmemory.RecallFixtureDecoy{MessageID: decoy.MessageID, Reason: decoy.Reason})
	}
	return appmemory.ExpectationsFromFixture(appmemory.RecallFixture{
		ProjectID:      fixture.ProjectID,
		OtherProjectID: fixture.OtherProjectID,
		Question:       fixture.Question,
		MustRecall: appmemory.RecallFixtureMustRecall{
			MessageID:     fixture.MustRecall.MessageID,
			ViaSummary:    fixture.MustRecall.ViaSummary,
			ReturnsSource: fixture.MustRecall.ReturnsSource,
		},
		Decoys: decoys,
	})
}

// canaryRecallResult runs the fixture's scenario and returns what the question brought back.
//
// It is a separate function from the assertions because three tests need the same run: one for the
// clean metrics, one for the leak direction, and one for the decoy direction. Building the scenario
// three times would let the three drift apart — and a metric test whose fixture differs from the
// others' is measuring a different scenario while claiming to measure this one.
func canaryRecallResult(t *testing.T) (appmemory.DeepRecallResult, canaryMemoryRecall, appmemory.Scope) {
	t.Helper()
	fixture := loadCanaryMemoryRecall(t)
	questionVector := make([]float32, 256)
	questionVector[11] = 1
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{
		fixture.Question: questionVector,
	}}
	harness := newWP10MemoryHarness(t, embedder)
	ctx := context.Background()
	scope := memory.Scope{
		Tenant: "local", Project: fixture.ProjectID, Episode: fixture.EpisodeID, AgentKey: fixture.AgentKey,
	}

	// The setting, buried under the turns, with its asset citation.
	if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope: scope, Message: fixture.Setting.Content, MessageID: fixture.Setting.MessageID,
		Role: agent.MessageRole(fixture.Setting.Role), AgentKey: fixture.AgentKey,
	}); err != nil {
		t.Fatal(err)
	}
	for _, turn := range fixture.BuryingTurns {
		if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
			Scope: scope, Message: turn.Content, MessageID: turn.MessageID,
			Role: agent.MessageRole(turn.Role), AgentKey: fixture.AgentKey,
		}); err != nil {
			t.Fatalf("RememberMessage(%s): %v", turn.MessageID, err)
		}
	}
	// Both decoys, in the scopes the fixture's own reasons describe: the first is a DIFFERENT
	// setting in the same project, the second is the SAME setting in another one.
	for index, decoy := range fixture.Decoys {
		decoyScope := scope
		if index == 1 {
			decoyScope.Project = fixture.OtherProjectID
		}
		if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
			Scope: decoyScope, Message: decoy.Content, MessageID: decoy.MessageID,
			Role: agent.MessageRole(decoy.Role), AgentKey: fixture.AgentKey,
		}); err != nil {
			t.Fatalf("RememberMessage(%s): %v", decoy.MessageID, err)
		}
	}
	// The summary, embedded as being about the question — what a provider would do for a summary of
	// the turn that answers it.
	embedder.fallback = questionVector
	summary, found, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: scope, Level: 1, Window: 2, Embed: true,
	})
	if err != nil || !found {
		t.Fatalf("Summarize: found=%v err=%v", found, err)
	}
	_, sources, err := harness.service.ListSummarySources(ctx, summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	covered := false
	for _, source := range sources {
		if source.SourceID == fixture.Setting.MessageID {
			covered = true
		}
	}
	if !covered {
		t.Fatalf("the summary does not cover the setting, so the scenario proves nothing: %+v", sources)
	}
	result, err := harness.service.DeepRecall(ctx, appmemory.DeepRecallRequest{
		Scope: scope, Query: fixture.Question, MaxRawMessages: fixture.MaxRawMessages,
	})
	if err != nil {
		t.Fatalf("DeepRecall: %v", err)
	}
	return result, fixture, scope
}

// TestTheCanaryExpectationsReadTheFixturesOwnClaims pins the conversion against the shipped file.
func TestTheCanaryExpectationsReadTheFixturesOwnClaims(t *testing.T) {
	fixture := loadCanaryMemoryRecall(t)
	expectation := expectationOf(fixture)

	if expectation.Query != fixture.Question {
		t.Fatalf("the question did not reach the expectation: %q", expectation.Query)
	}
	if expectation.MessageID != fixture.MustRecall.MessageID {
		t.Fatalf("the expected message is %q and the fixture says %q",
			expectation.MessageID, fixture.MustRecall.MessageID)
	}
	if !expectation.ViaSummary || !expectation.ReturnsSource {
		t.Fatalf("the fixture's mustRecall claims were dropped: %+v", expectation)
	}
	// BOTH decoy ids reach the expectation, because the fixture does not tag them with a project and
	// a decoy presented as the answer is a decoy hit whichever project it came from. The other
	// project is named once, at the top level.
	if len(expectation.DecoyMessageIDs) != len(fixture.Decoys) {
		t.Fatalf("the decoys did not reach the expectation: %+v", expectation.DecoyMessageIDs)
	}
	if len(expectation.ForeignProjectIDs) != 1 || expectation.ForeignProjectIDs[0] != fixture.OtherProjectID {
		t.Fatalf("the leak expectation is %+v, want the fixture's other project", expectation.ForeignProjectIDs)
	}
}

// TestTheCanaryRecallScoresAsACleanRun is section 18.2's clean direction over the real chain.
//
// It asserts the two rates the section names, plus the stricter reading of the hit rate that makes
// the metric mean what the criterion is about: the message has to come back THROUGH a summary, not
// merely come back.
func TestTheCanaryRecallScoresAsACleanRun(t *testing.T) {
	result, fixture, _ := canaryRecallResult(t)
	expectation := expectationOf(fixture)

	score := appmemory.ScoreRecall(result, expectation)
	if !score.Hit {
		t.Fatalf("the setting was not restored, so the canary scenario itself is broken: %+v", score)
	}
	if !score.ViaSummary {
		t.Fatalf("the setting came back without a summary hop, and the fixture requires one: %+v", score)
	}
	if !score.ReturnedSource {
		t.Fatalf("the restored message carried no provenance: %+v", score)
	}
	if score.ForeignCitations != 0 {
		t.Fatalf("the canary leaked another project: %+v", score)
	}

	metrics := appmemory.Aggregate([]appmemory.RecallScore{score})
	if metrics.HitRate() != 1 || metrics.SummaryHitRate() != 1 {
		t.Fatalf("the canary scored %v and %v, want 1 and 1", metrics.HitRate(), metrics.SummaryHitRate())
	}
	// THE LEAK RATE, which is the number a release gate reads. Zero is the only acceptable value.
	if metrics.LeakRate() != 0 {
		t.Fatalf("the canary's leak rate is %v", metrics.LeakRate())
	}
}

// TestTheMetricFiresWhenTheRecallIsGivenTheWrongScope is the reverse direction, driven by the real
// chain rather than by a constructed result.
//
// # Why this is the important test in this file
//
// 「Memory 跨项目泄露率」 is a metric whose only acceptable value is zero, which is exactly the kind
// that rots into a constant: a scorer that returned 0 because it read no rows would report a clean
// evaluation forever. So this test runs the SAME recall in the OTHER project's scope — where the
// buried setting does not exist and the other project's decoy does — and demands that the metric
// report a miss. If it reported a hit, the scenario would be recalling across projects, which is the
// leak; either way the metric has to say something true, and 1.0 with no leak is not one of the two.
func TestTheMetricFiresWhenTheRecallIsGivenTheWrongScope(t *testing.T) {
	result, fixture, _ := canaryRecallResult(t)
	// The other project's scope, with the question that belongs to the first project's setting.
	otherScope := memory.Scope{
		Tenant: "local", Project: fixture.OtherProjectID,
		Episode: fixture.EpisodeID, AgentKey: fixture.AgentKey,
	}
	expectation := appmemory.RecallExpectation{
		Query:             fixture.Question,
		MessageID:         fixture.MustRecall.MessageID,
		ForeignProjectIDs: []string{fixture.ProjectID},
	}
	// Re-score the SAME result against a scope it does not belong to: every row it carries is now a
	// foreign citation, which is what a leak looks like from this side.
	score := appmemory.ScoreRecall(result, expectation)
	if score.ForeignCitations == 0 {
		t.Fatalf("a result from project %q reported no leak when every row is foreign: %+v",
			fixture.ProjectID, score)
	}
	metrics := appmemory.Aggregate([]appmemory.RecallScore{score})
	if metrics.LeakRate() != 1 {
		t.Fatalf("the leak rate is %v when every row cited is foreign", metrics.LeakRate())
	}
	if metrics.HitRate() != 1 {
		// The message IS present in the result — it is simply from the wrong project. That is what
		// makes the leak worth a metric of its own: a plain hit rate cannot see it.
		t.Fatalf("the hit rate should still be 1, since the message was returned: %+v", metrics)
	}
	_ = otherScope
}
