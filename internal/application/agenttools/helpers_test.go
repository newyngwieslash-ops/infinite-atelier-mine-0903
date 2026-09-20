package agenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// These tests cover the pure helpers every handler calls. They are separated from the
// handler tests because they need no services: each is a small function whose whole
// behaviour is its arguments and its result, and each is on a path a mutation showed
// could be removed without a test noticing.
//
// That is the reason this file exists rather than being folded into the contract tests
// above: the first mutation run over this package reported twelve survivors, and eight
// of them were in these functions. A security-relevant helper with no test is a helper
// nobody would notice losing its check.

// TestDecodeArgumentsRefusesUnknownFields is the decoder's whole point.
//
// The runtime validated these arguments against the tool's schema, and every schema is
// additionalProperties false. So a field the schema does not describe means the
// validation and this decoder disagree about the contract. Ignoring it would let a
// future schema accept a field a handler silently dropped, which is a model's
// instruction going somewhere the reviewer did not see.
func TestDecodeArgumentsRefusesUnknownFields(t *testing.T) {
	var target struct {
		ChapterID string `json:"chapterId"`
	}
	// A known field decodes.
	if err := decodeArguments(json.RawMessage(`{"chapterId":"c1"}`), &target); err != nil {
		t.Fatalf("a known field was refused: %v", err)
	}
	if target.ChapterID != "c1" {
		t.Fatalf("the field decoded as %q", target.ChapterID)
	}
	// An unknown one is refused.
	if err := decodeArguments(json.RawMessage(`{"chapterId":"c1","projectId":"other"}`), &target); err == nil {
		t.Fatal("an unknown field was ignored")
	}
	// The empty forms are legal, because a tool whose arguments are all optional takes
	// none and that is what a model sends.
	for _, empty := range []string{"", "{}", "null", "  "} {
		if err := decodeArguments(json.RawMessage(empty), &target); err != nil {
			t.Errorf("the empty form %q was refused: %v", empty, err)
		}
	}
	// Malformed JSON is refused, and the refusal is the runtime's own error type so a
	// caller classifies it the same way as its other refusals.
	err := decodeArguments(json.RawMessage(`{"chapterId":`), &target)
	if err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	if _, ok := agent.AsError(err); !ok {
		t.Fatalf("the refusal is a %T, want the domain's error", err)
	}
}

// TestRequiredRefusesAnEmptyIdentifier covers the helper every write tool calls.
//
// A tool that silently used an empty identifier would read no row or the wrong one, and
// the failure would surface as an empty result rather than as a bad request — which is
// the class of failure that takes longest to diagnose.
func TestRequiredRefusesAnEmptyIdentifier(t *testing.T) {
	if got, err := required(" value ", "field"); err != nil || got != "value" {
		t.Fatalf("a present value returned %q, %v", got, err)
	}
	for _, empty := range []string{"", "   ", "\t", "\n"} {
		if _, err := required(empty, "chapter"); err == nil {
			t.Errorf("the empty value %q was accepted", empty)
		}
	}
	// The refusal names the field, because the caller's next step is to supply it.
	_, err := required("", "chapter")
	if err == nil {
		t.Fatal("no refusal")
	}
	if !strings.Contains(err.Error(), "chapter") {
		t.Fatalf("the refusal reads %q, which does not name the field", err)
	}
}

// TestLimitOfClampsToTheBounds covers the page-size helper.
//
// A model asking for a million rows gets the ceiling rather than an error, because the
// request is well formed and the bound is the runtime's. Zero means "not stated" — it is
// what an omitted field decodes to — so it takes the fallback rather than being treated
// as "return nothing".
func TestLimitOfClampsToTheBounds(t *testing.T) {
	cases := []struct {
		requested, fallback, ceiling, want int
	}{
		{0, 50, 200, 50},
		{-1, 50, 200, 50},
		{10, 50, 200, 10},
		{200, 50, 200, 200},
		{201, 50, 200, 200},
		{1000000, 50, 200, 200},
	}
	for _, testCase := range cases {
		if got := limitOf(testCase.requested, testCase.fallback, testCase.ceiling); got != testCase.want {
			t.Errorf("limitOf(%d, %d, %d) = %d, want %d",
				testCase.requested, testCase.fallback, testCase.ceiling, got, testCase.want)
		}
	}
}

// TestStoryStatusFilterRefusesAnUnknownStatus covers the closed vocabulary.
//
// story.FactStatus has a fixed set, and a query for a status that does not exist would
// return an empty list — which reads as "this project has no such facts" rather than as
// a bad request, and would send a model looking for a filter that never existed.
func TestStoryStatusFilterRefusesAnUnknownStatus(t *testing.T) {
	// Empty means "every status", which is the honest reading of an omitted filter.
	status, err := storyStatusFilter("")
	if err != nil || status != "" {
		t.Fatalf("an empty filter returned %q, %v", status, err)
	}
	// Each of the vocabulary's values is accepted.
	for _, value := range []string{"candidate", "accepted", "rejected", "locked"} {
		got, err := storyStatusFilter(value)
		if err != nil {
			t.Errorf("the status %q was refused: %v", value, err)
		}
		if got != story.FactStatus(value) {
			t.Errorf("the status %q returned %q", value, got)
		}
	}
	// Anything else is refused.
	for _, value := range []string{"approved", "deleted", "CANDIDATE"} {
		if _, err := storyStatusFilter(value); err == nil {
			t.Errorf("the unknown status %q was accepted", value)
		}
	}
}

// TestAssetTypesRefusesAnUnknownType covers the same rule for the asset filter.
func TestAssetTypesRefusesAnUnknownType(t *testing.T) {
	types, err := assetTypes(nil)
	if err != nil || types != nil {
		t.Fatalf("an omitted filter returned %v, %v", types, err)
	}
	known := asset.Types[0]
	got, err := assetTypes([]string{string(known)})
	if err != nil {
		t.Fatalf("the known type %q was refused: %v", known, err)
	}
	if len(got) != 1 || got[0] != known {
		t.Fatalf("the filter returned %v", got)
	}
	if _, err := assetTypes([]string{"not-a-type"}); err == nil {
		t.Fatal("an unknown type was accepted")
	}
	// One bad value refuses the whole filter rather than dropping it: a filter that
	// silently narrowed would return a list the caller believes is complete.
	if _, err := assetTypes([]string{string(known), "not-a-type"}); err == nil {
		t.Fatal("a partially unknown filter was accepted")
	}
}

// TestContextDoneReportsACancelledRun covers the check every handler makes first.
//
// AGENT_CONTRACTS section 15's "Runtime 停止流和后续 Tool" is a property of the tool
// layer as much as of the runner: the runner refuses the NEXT call, but it cannot recall
// one already running, so this is what stops it.
func TestContextDoneReportsACancelledRun(t *testing.T) {
	if err := contextDone(context.Background()); err != nil {
		t.Fatalf("a live context reported %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := contextDone(ctx)
	if err == nil {
		t.Fatal("a cancelled context was not reported")
	}
	// The refusal must be the runtime's own cancellation type, because that is what
	// makes a cancelled run recorded as cancelled rather than as failed.
	var cancelled *agentruntime.CancelledError
	if !asCancelled(err, &cancelled) {
		t.Fatalf("the refusal is a %T, want the runtime's cancellation", err)
	}
	// The deadline case is the same check: a context whose deadline passed is done.
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if err := contextDone(expired); err == nil {
		t.Fatal("an expired context was not reported")
	}
}

// asCancelled finds the runtime's cancellation through the chain.
func asCancelled(err error, target **agentruntime.CancelledError) bool {
	cancelled, ok := err.(*agentruntime.CancelledError)
	if ok {
		*target = cancelled
	}
	return ok
}

// TestItoaSmallRendersSmallNumbers covers the summary helper.
//
// It is trivial, and it is tested because a version number appears in a sentence a user
// reads: a wrong rendering would say "version 0" for a version that exists, which is the
// kind of small lie that costs a debugging session.
func TestItoaSmallRendersSmallNumbers(t *testing.T) {
	cases := map[int]string{0: "0", -1: "0", 1: "1", 9: "9", 10: "10", 42: "42", 100: "100", 12345: "12345"}
	for value, want := range cases {
		if got := itoaSmall(value); got != want {
			t.Errorf("itoaSmall(%d) = %q, want %q", value, got, want)
		}
	}
}

// TestArtifactResultNamesTheRowItWrote covers the shape every write tool returns.
//
// The identifier is what the runtime's ArtifactVerifier reads back, and AC-AGENT-003
// makes a stage that names an artifact which does not exist fail. A result with an empty
// artifacts array would make the verification vacuous: there would be nothing to check,
// so a write that silently did nothing would look like a success.
func TestArtifactResultNamesTheRowItWrote(t *testing.T) {
	result := artifactResult("story_skeleton", "story_skeleton_version", "version-1", 3, "draft", "story skeleton")
	if result["stage"] != "story_skeleton" {
		t.Fatalf("the result names stage %v", result["stage"])
	}
	artifacts, ok := result["artifacts"].([]map[string]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("the result carries %v as its artifacts", result["artifacts"])
	}
	artifact := artifacts[0]
	if artifact["entityType"] != "story_skeleton_version" || artifact["entityId"] != "version-1" {
		t.Fatalf("the artifact reference is %v", artifact)
	}
	if artifact["operation"] != "created" {
		t.Fatalf("the operation is %v, want created", artifact["operation"])
	}
	if result["versionNumber"] != 3 {
		t.Fatalf("the version number is %v", result["versionNumber"])
	}
	// The summary names the version, so a list view is not blank.
	summary, _ := result["summary"].(string)
	if !strings.Contains(summary, "3") {
		t.Fatalf("the summary reads %q", summary)
	}
}
