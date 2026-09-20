package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// These tests cover the bridge between the runtime's ModelPort and a provider text
// adapter. It is the seam where two vocabularies meet, so what is asserted is the
// translation: the messages arrive unchanged, the tool calls arrive with their
// arguments, and a provider refusal is classified the way section 14 requires rather
// than the way the provider happened to mark it.

// stubGenerator is a TextGenerator that records what it was asked and returns a
// scripted answer.
type stubGenerator struct {
	result TextGenerationResult
	err    error
	seen   []TextGenerationRequest
}

func (g *stubGenerator) Generate(_ context.Context, request TextGenerationRequest) (TextGenerationResult, error) {
	g.seen = append(g.seen, request)
	if g.err != nil {
		return TextGenerationResult{}, g.err
	}
	return g.result, nil
}

// TestModelBridgePassesThePromptThroughUnchanged is the property the bridge must not
// break: section 5's layer order IS the security boundary, so a bridge that filtered,
// reordered or merged messages would be rewriting it where no test of the assembler
// would notice.
func TestModelBridgePassesThePromptThroughUnchanged(t *testing.T) {
	generator := &stubGenerator{result: TextGenerationResult{Content: `{"schemaVersion":1}`, Model: "m"}}
	bridge := NewModelBridge(generator)
	// The prompt is ASSEMBLED rather than hand-built, because the boundary markers are
	// what this test is about: a hand-built Prompt would carry whatever the test wrote
	// and prove nothing about whether the assembler's markings survive the trip.
	prompt := Assemble(AssembleRequest{
		Spec:            validSpec("script.execution.x", agent.LayerExecution, "story.read_events"),
		Skill:           "# Role\n\nDo the step.",
		WorkflowState:   "stage=story_skeleton attempt=1",
		Task:            "the chapter text",
		TaskIsUntrusted: true,
		UserMessage:     "go ahead",
	})
	if _, err := bridge.Complete(context.Background(), ModelRequest{
		ModelID: "model-1", ProviderID: "prov-1", Messages: prompt.AsTextMessages(),
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(generator.seen) != 1 {
		t.Fatalf("the generator was called %d times", len(generator.seen))
	}
	sent := generator.seen[0]
	if sent.ProviderID != "prov-1" || sent.Model != "model-1" {
		t.Fatalf("the request named %q/%q", sent.ProviderID, sent.Model)
	}
	want := prompt.AsTextMessages()
	if len(sent.Messages) != len(want) {
		t.Fatalf("the bridge sent %d messages, want %d", len(sent.Messages), len(want))
	}
	for index, message := range sent.Messages {
		if message.Role != want[index].Role || message.Content != want[index].Content {
			t.Fatalf("message %d was changed: %+v", index, message)
		}
	}
	// The untrusted boundary must survive the trip: it is the only prompt-layer
	// marking the provider protocol can carry, so a bridge that stripped it would
	// remove the boundary the model is told to respect.
	var sawBoundary bool
	for _, message := range sent.Messages {
		if strings.Contains(message.Content, UntrustedOpen) &&
			strings.Contains(message.Content, UntrustedClose) {
			sawBoundary = true
		}
	}
	if !sawBoundary {
		t.Fatal("the untrusted boundary was lost in translation")
	}
}

// TestModelBridgeMapsToolCalls covers the field-for-field translation that makes the
// bridge worth having rather than inlining.
func TestModelBridgeMapsToolCalls(t *testing.T) {
	generator := &stubGenerator{result: TextGenerationResult{
		Content: `{"schemaVersion":1}`,
		Model:   "m",
		ToolCalls: []TextGenerationToolCall{
			{Key: "story.read_events", Arguments: json.RawMessage(`{"chapterId":"c1"}`)},
			{Key: "workflow.read_state", Arguments: json.RawMessage(`{}`)},
		},
	}}
	reply, err := NewModelBridge(generator).Complete(context.Background(), ModelRequest{ModelID: "m"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(reply.ToolCalls) != 2 {
		t.Fatalf("the reply carries %d tool calls, want 2", len(reply.ToolCalls))
	}
	if reply.ToolCalls[0].Key != "story.read_events" {
		t.Fatalf("the first call names %q", reply.ToolCalls[0].Key)
	}
	// The arguments travel as bytes and are not re-encoded: the tool's input schema
	// validates what the model sent, not what a bridge produced.
	if !strings.Contains(string(reply.ToolCalls[0].Arguments), "c1") {
		t.Fatalf("the arguments were altered: %q", reply.ToolCalls[0].Arguments)
	}
}

// TestModelBridgeRefusesAnUnnamedToolCall mirrors the adapter's rule: a call that
// cannot be named cannot be authorized or refused, so it must not be silently
// dropped, because dropping turns "asked for something unchecked" into "asked for
// nothing".
func TestModelBridgeRefusesAnUnnamedToolCall(t *testing.T) {
	generator := &stubGenerator{result: TextGenerationResult{
		Content:   `{}`,
		ToolCalls: []TextGenerationToolCall{{Key: "  ", Arguments: json.RawMessage(`{}`)}},
	}}
	if _, err := NewModelBridge(generator).Complete(context.Background(), ModelRequest{ModelID: "m"}); err == nil {
		t.Fatal("a tool call with a blank key was accepted")
	}
}

// TestModelBridgeFailsClosedWithoutAGenerator covers the composition-failure shape: a
// build that resolved no provider refuses at run time with a reason rather than
// panicking at startup.
func TestModelBridgeFailsClosedWithoutAGenerator(t *testing.T) {
	bridge := NewModelBridge(nil)
	if bridge.Available() {
		t.Fatal("a bridge with no generator reports available")
	}
	if _, err := bridge.Complete(context.Background(), ModelRequest{ModelID: "m"}); err == nil {
		t.Fatal("a bridge with no generator produced a reply")
	}
	var none *ModelBridge
	if none.Available() {
		t.Fatal("a nil bridge reports available")
	}
	if _, err := none.Complete(context.Background(), ModelRequest{}); err == nil {
		t.Fatal("a nil bridge produced a reply")
	}
}

// TestProviderFailureClassificationFollowsSection14 is the policy assertion.
//
// Each case is a category section 14 names, and the two columns that matter are the
// domain category and whether a retry is allowed. The direction that would be a
// security defect is a refusal marked retriable: section 14.2 forbids retrying a
// security error, an invalid input, or a cancellation, so those are asserted
// individually rather than trusted to the provider's own flag.
func TestProviderFailureClassificationFollowsSection14(t *testing.T) {
	cases := []struct {
		name       string
		provider   *provider.Error
		wantCateg  agent.ErrorCategory
		wantRetry  bool
		wantCode   string
		wantCancel bool
	}{
		{
			name:      "rate limited is retriable",
			provider:  provider.NewRateLimitedError(0),
			wantCateg: agent.CategoryModel, wantRetry: true, wantCode: "agent.rate_limited",
		},
		{
			name:      "a transient remote failure is retriable",
			provider:  provider.NewRemoteTransientError(),
			wantCateg: agent.CategoryModel, wantRetry: true, wantCode: "agent.model_failed",
		},
		{
			name:      "a timeout is retriable and keeps its code",
			provider:  provider.NewTimeoutError(),
			wantCateg: agent.CategoryModel, wantRetry: true, wantCode: "agent.timeout",
		},
		{
			name:      "a network failure is retriable",
			provider:  provider.NewNetworkError(),
			wantCateg: agent.CategoryModel, wantRetry: true, wantCode: "agent.model_failed",
		},
		{
			name:      "an invalid input is never retried",
			provider:  provider.NewInvalidInputError(),
			wantCateg: agent.CategoryInvalidInput, wantRetry: false, wantCode: "agent.model_failed",
		},
		{
			name:      "a content policy refusal is never retried",
			provider:  provider.NewContentPolicyError(),
			wantCateg: agent.CategoryInvalidInput, wantRetry: false, wantCode: "agent.model_failed",
		},
		{
			name:      "a security refusal is never retried",
			provider:  provider.NewSecurityError(),
			wantCateg: agent.CategorySecurity, wantRetry: false, wantCode: "agent.model_failed",
		},
		{
			name:      "a 401 is never retried",
			provider:  provider.NewUnauthorizedError(),
			wantCateg: agent.CategoryUnavailable, wantRetry: false, wantCode: "agent.provider_unauthorized",
		},
		{
			name:      "a 403 is never retried",
			provider:  provider.NewForbiddenError(),
			wantCateg: agent.CategoryUnavailable, wantRetry: false, wantCode: "agent.provider_unauthorized",
		},
		{
			name:      "an unsupported capability is not retried",
			provider:  provider.NewUnsupportedError(),
			wantCateg: agent.CategoryUnavailable, wantRetry: false, wantCode: "agent.provider_unsupported",
		},
		{
			name:      "a configuration defect is not retried",
			provider:  provider.NewConfigurationError(),
			wantCateg: agent.CategoryUnavailable, wantRetry: false, wantCode: "agent.provider_configuration",
		},
		{
			name:      "a cancellation is not retried and is marked as one",
			provider:  provider.NewCancelledError(),
			wantCateg: agent.CategoryCancelled, wantRetry: false, wantCode: "agent.cancelled", wantCancel: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			failure := ClassifyProviderFailure(testCase.provider)
			classified, ok := AsProviderFailure(failure)
			if !ok {
				t.Fatalf("the classification is a %T", failure)
			}
			if classified.Category() != testCase.wantCateg {
				t.Fatalf("category is %q, want %q", classified.Category(), testCase.wantCateg)
			}
			if classified.Retriable() != testCase.wantRetry {
				t.Fatalf("retriable is %v, want %v", classified.Retriable(), testCase.wantRetry)
			}
			if classified.Code() != testCase.wantCode {
				t.Fatalf("code is %q, want %q", classified.Code(), testCase.wantCode)
			}
			// IsRetriable is the function the retry policy actually calls, so it must
			// agree with the method: a policy reading one and a record reading the other
			// would disagree about the same failure.
			if IsRetriable(failure) != testCase.wantRetry {
				t.Fatalf("IsRetriable is %v but the type says %v", IsRetriable(failure), testCase.wantRetry)
			}
			if testCase.wantCancel != errors.Is(failure, context.Canceled) {
				t.Fatalf("errors.Is(canceled) is %v, want %v", errors.Is(failure, context.Canceled), testCase.wantCancel)
			}
			// The safe message is what a user sees, so it must never be empty and must
			// never be the provider's raw text.
			if classified.Error() == "" {
				t.Fatal("the refusal has no safe message")
			}
		})
	}
}

// TestProviderFailureRefusesToMarkASecurityRefusalRetriable is the one case worth its
// own test: the provider layer is asked to say something is retriable that section
// 14.2 forbids retrying, and the specification must win.
//
// It is written this way because the provider's own flag is data the agent runtime
// does not control. A provider adapter with a bug, or a future category added to that
// domain, could carry Retriable true for a refusal that must never be retried, and
// the failure would be a silent retry loop against a security boundary.
func TestProviderFailureRefusesToMarkASecurityRefusalRetriable(t *testing.T) {
	lying := &provider.Error{
		Category:    provider.CategorySecurity,
		SafeMessage: "blocked",
		Retriable:   true,
	}
	classified, ok := AsProviderFailure(ClassifyProviderFailure(lying))
	if !ok {
		t.Fatal("the refusal was not classified")
	}
	if classified.Retriable() {
		t.Fatal("a security refusal was marked retriable because the provider said so")
	}
	if IsRetriable(classified) {
		t.Fatal("IsRetriable retried a security refusal")
	}
}

// TestClassifyProviderFailurePassesOtherErrorsThrough proves the classifier is not a
// wrapper that loses information: an error from another layer already carries its own
// category and is returned unchanged.
func TestClassifyProviderFailurePassesOtherErrorsThrough(t *testing.T) {
	if err := ClassifyProviderFailure(nil); err != nil {
		t.Fatalf("nil became %v", err)
	}
	own := agent.SecurityError("the ACL refused this")
	if got := ClassifyProviderFailure(own); got != error(own) {
		t.Fatalf("a domain error was wrapped: %v", got)
	}
	var failure *ProviderFailure
	if errors.As(ClassifyProviderFailure(own), &failure) {
		t.Fatal("a domain error was reported as a provider failure")
	}
}
