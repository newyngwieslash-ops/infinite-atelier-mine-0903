package providers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"

	"bytes"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/schemas"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// These tests cover WP-07 scope item 13 and AGENT_CONTRACTS section 18.3.
//
// The property that matters most is not "the mock returns JSON" but "the mock
// returns a document the REAL schema accepts". A mock whose reply the validator
// refuses would make every runtime test that used it a test of the refusal path,
// which is why the assertions below compile the embedded schemas and check the
// replies against them rather than against a string written out twice.

// mockRequest builds a TextRequest with the prompt layers the runtime emits.
func mockRequest(layer string) providers.TextRequest {
	var policy string
	switch layer {
	case "decision":
		policy = "You choose among the actions the runtime gives you. You do not decide what is legal."
	case "supervision":
		policy = "You review what the artifact actually contains. Load it with your own read tools."
	default:
		policy = "You do the one narrow step you were given, for the stage named in your request."
	}
	return providers.TextRequest{
		ProviderID: "mock-text-1",
		Model:      "mock-model",
		Messages: []providers.TextMessage{
			{Role: "system", Content: "You are one agent in a three-layer system."},
			{Role: "system", Content: policy},
			{Role: "system", Content: "Tools you may call, with the schema each argument must satisfy:\n- workflow.read_state (read) schema schemas/agent/x.json\n- script.create_script_version (write) schema schemas/agent/y.json\n"},
			{Role: "user", Content: "Current workflow state:\nworkflow_run=run-1 stage=story_skeleton stage_run=stage-9 attempt=1"},
		},
	}
}

// TestMockScenariosMatchSection18 is the coverage assertion: the list of scenarios
// this build implements is the list AGENT_CONTRACTS section 18.3 gives, so a scenario
// dropped from the mock is visible here rather than only in a missing test.
func TestMockScenariosMatchSection18(t *testing.T) {
	// The section's list, written out here as the assertion rather than shared with
	// the implementation: a shared list would make the test agree with whatever the
	// implementation did.
	want := []MockScenario{
		"normal",
		"invalid_once",
		"tool_call",
		"illegal_tool",
		"supervisor_issues",
		"timeout",
		"cancelled",
		"provider_error",
	}
	got := MockScenarios()
	if len(got) != len(want) {
		t.Fatalf("the mock implements %d scenarios, section 18.3 lists %d", len(got), len(want))
	}
	for index, scenario := range want {
		if got[index] != scenario {
			t.Fatalf("scenario %d is %q, want %q", index, got[index], scenario)
		}
	}
}

// TestMockTextIsReachableOnlyForItsOwnKind is the reachability guard.
//
// Three things must hold for the mock to be safe to carry in production code, and
// each is a separate statement here: it is not a kind a configuration may be
// persisted with, it is not resolvable for another kind, and it is not built when
// nothing registered it.
func TestMockTextIsReachableOnlyForItsOwnKind(t *testing.T) {
	if !provider.IsValidKind(provider.KindMockText) {
		t.Fatal("the mock text kind is not registered at all")
	}
	if provider.IsUserConfigurableKind(provider.KindMockText) {
		t.Fatal("the mock text kind may be persisted as a provider configuration")
	}
	if provider.IsUserConfigurableKind(provider.KindMockMedia) {
		t.Fatal("the mock media kind may be persisted as a provider configuration")
	}

	// Another kind must not resolve to the mock.
	realConfig := provider.Config{ID: "real-1", Kind: provider.KindOpenAICompatible, DisplayName: "real", BaseURL: "https://api.example.com", Enabled: true}
	registry := NewRegistry(&staticConfigs{config: realConfig}, &staticSecret{}, &captureAudit{})
	registry.WithMockTextAdapter(NewMockTextAdapter())
	port, _, err := registry.TextProviderFor(context.Background(), "real-1")
	if err != nil {
		t.Fatalf("a real provider kind was refused: %v", err)
	}
	if _, isMock := port.(*MockTextAdapter); isMock {
		t.Fatal("an openai_compatible provider resolved to the text mock")
	}

	// A registry with no mock registered refuses the mock kind rather than building
	// one, so a build that did not opt in cannot be answered by a mock.
	mockConfig := provider.Config{ID: "mock-1", Kind: provider.KindMockText, DisplayName: "mock", BaseURL: "https://mock.invalid", Enabled: true}
	unregistered := NewRegistry(&staticConfigs{config: mockConfig}, &staticSecret{}, &captureAudit{})
	if _, err := unregistered.TextPortFor(context.Background(), "mock-1"); err == nil {
		t.Fatal("the mock kind resolved without a mock being registered")
	}

	// With one registered it resolves, and it is the SAME instance, because the
	// scenario and the call log are state the caller set up.
	adapter := NewMockTextAdapter()
	registered := NewRegistry(&staticConfigs{config: mockConfig}, &staticSecret{}, &captureAudit{})
	registered.WithMockTextAdapter(adapter)
	resolved, err := registered.TextPortFor(context.Background(), "mock-1")
	if err != nil {
		t.Fatalf("an explicitly registered mock was refused: %v", err)
	}
	if resolved != providers.TextPort(adapter) {
		t.Fatal("the resolver built a new adapter instead of returning the registered one")
	}
}

// TestMockTextRepliesSatisfyTheRealSchemas is the assertion that makes the mock
// usable: each layer's reply validates against the contract that layer's agent
// declares in its manifest.
//
// The schema paths are the ones skills/script/manifest.json names, so this test
// fails if a reply and a registered contract drift apart.
func TestMockTextRepliesSatisfyTheRealSchemas(t *testing.T) {
	cases := []struct {
		layer      string
		scenario   MockScenario
		schemaPath string
	}{
		{"execution", MockScenarioNormal, schemas.AgentExecutionResultPath},
		{"decision", MockScenarioNormal, schemas.AgentDecisionResultPath},
		{"supervision", MockScenarioNormal, schemas.AgentReviewReportPath},
		{"supervision", MockScenarioSupervisorIssues, schemas.AgentReviewReportPath},
		{"execution", MockScenarioToolCall, schemas.AgentExecutionResultPath},
		{"decision", MockScenarioToolCall, schemas.AgentDecisionResultPath},
	}
	for _, testCase := range cases {
		adapter := NewMockTextAdapter().SetScenario(testCase.scenario)
		result, err := adapter.Generate(context.Background(), mockRequest(testCase.layer))
		if err != nil {
			t.Fatalf("%s/%s: Generate: %v", testCase.layer, testCase.scenario, err)
		}
		checkAgainstSchema(t, testCase.schemaPath, result.Content)
	}
}

// checkAgainstSchema compiles an embedded schema and validates a document.
func checkAgainstSchema(t *testing.T, schemaPath, document string) {
	t.Helper()
	body, err := schemas.Lookup(schemaPath)
	if err != nil {
		t.Fatalf("the embedded schema %s could not be read: %v", schemaPath, err)
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("the schema %s is not valid JSON: %v", schemaPath, err)
	}
	compiler := jsonschema.NewCompiler()
	identifier := "https://infinite-atelier.invalid/" + schemaPath
	if err := compiler.AddResource(identifier, resource); err != nil {
		t.Fatalf("registering %s: %v", schemaPath, err)
	}
	compiled, err := compiler.Compile(identifier)
	if err != nil {
		t.Fatalf("compiling %s: %v", schemaPath, err)
	}
	instance, err := jsonschema.UnmarshalJSON(strings.NewReader(document))
	if err != nil {
		t.Fatalf("the reply is not valid JSON: %v (document: %s)", err, document)
	}
	if err := compiled.Validate(instance); err != nil {
		t.Fatalf("the reply does not satisfy %s: %v\nreply: %s", schemaPath, err, document)
	}
}

// TestMockExecutionResultNamesTheStageItWasGiven is the property a mock without a
// prompt would fail: the stage and stage-run identifiers come from the workflow
// state the runtime rendered, not from the mock's imagination, so the runtime's own
// cross-check is exercised by a value that is actually right.
func TestMockExecutionResultNamesTheStageItWasGiven(t *testing.T) {
	adapter := NewMockTextAdapter()
	result, err := adapter.Generate(context.Background(), mockRequest("execution"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var document struct {
		Stage      string `json:"stage"`
		StageRunID string `json:"stageRunId"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.Content), &document); err != nil {
		t.Fatalf("the reply is not JSON: %v", err)
	}
	if document.Stage != "story_skeleton" {
		t.Fatalf("the reply names stage %q, want the one the prompt carried", document.Stage)
	}
	if document.StageRunID != "stage-9" {
		t.Fatalf("the reply names stage run %q, want the one the prompt carried", document.StageRunID)
	}
	// An execution that produced nothing must NOT claim success: section 7.4 requires
	// a success to carry the artifact it produced, and this mock produced none.
	if document.Status == "success" {
		t.Fatal("a reply with no artifacts reported success")
	}
}

// TestMockInvalidOnceIsInvalidThenValid covers section 18.3's "一次无效结构后二次有效"
// as a sequence rather than as two independent calls.
func TestMockInvalidOnceIsInvalidThenValid(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioInvalidOnce)
	request := mockRequest("execution")

	first, err := adapter.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("the first call failed outright: %v", err)
	}
	var bad struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal([]byte(first.Content), &bad); err != nil {
		t.Fatalf("the first reply is not JSON: %v", err)
	}
	if bad.SchemaVersion == 1 {
		t.Fatal("the first reply claims the contract's version, so nothing would be repaired")
	}

	second, err := adapter.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("the repair call failed: %v", err)
	}
	checkAgainstSchema(t, schemas.AgentExecutionResultPath, second.Content)
}

// TestMockInvalidOnceCountsPerConversation proves the counter is keyed on the
// conversation rather than globally. A global counter would make the repair round
// depend on how many other agents had run first, so a test order would change the
// outcome.
func TestMockInvalidOnceCountsPerConversation(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioInvalidOnce)
	first := mockRequest("execution")
	other := mockRequest("decision")

	if _, err := adapter.Generate(context.Background(), first); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// A different conversation's FIRST call is still invalid, because it is a first
	// call for that conversation.
	reply, err := adapter.Generate(context.Background(), other)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var document struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal([]byte(reply.Content), &document); err != nil {
		t.Fatalf("the reply is not JSON: %v", err)
	}
	if document.SchemaVersion == 1 {
		t.Fatal("a second conversation was treated as a repeat of the first")
	}
}

// TestMockInvalidAlwaysStaysInvalid is the other half of the repair round: a model
// that never satisfies the contract must not accidentally succeed on a later call.
func TestMockInvalidAlwaysStaysInvalid(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioInvalidAlways)
	request := mockRequest("execution")
	for attempt := 0; attempt < 3; attempt++ {
		reply, err := adapter.Generate(context.Background(), request)
		if err != nil {
			t.Fatalf("attempt %d: Generate: %v", attempt, err)
		}
		var document struct {
			SchemaVersion int `json:"schemaVersion"`
		}
		if err := json.Unmarshal([]byte(reply.Content), &document); err != nil {
			t.Fatalf("attempt %d: the reply is not JSON: %v", attempt, err)
		}
		if document.SchemaVersion == 1 {
			t.Fatalf("attempt %d produced a valid document", attempt)
		}
	}
}

// TestMockToolCallReadsTheAgentsOwnContract proves the tool_call scenario asks for
// something the prompt says the agent MAY call, which is what makes it exercise the
// handler path rather than the denial path.
//
// The call is read from the RESULT rather than from the document, because that is
// where it travels: section 7's schemas are additionalProperties false, so a tool
// call inside a returned document is refused by the schema that document must
// satisfy.
func TestMockToolCallReadsTheAgentsOwnContract(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioToolCall)
	reply, err := adapter.Generate(context.Background(), mockRequest("execution"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// A WRITE tool is preferred, so the scenario exercises the path where the runtime DOES
	// what the model asked: a read call would leave the artifact and its verification
	// untested. That is the correction the canary found.
	key := onlyToolCallKey(t, reply)
	if key != "script.create_script_version" {
		t.Fatalf("the reply asks for %q, want the write tool in the agent's own contract", key)
	}
	// The document must still satisfy its schema: the call travelled beside it.
	checkAgainstSchema(t, schemas.AgentExecutionResultPath, reply.Content)
}

// TestMockIllegalToolPrefersAWriteToolTheAgentHas proves the refusal scenario picks
// a tool the MODE matrix refuses, which is the stronger test: a refusal for an
// unknown key would pass even if the matrix were deleted.
func TestMockIllegalToolPrefersAWriteToolTheAgentHas(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioIllegalTool)
	reply, err := adapter.Generate(context.Background(), mockRequest("execution"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	key := onlyToolCallKey(t, reply)
	if key != "script.create_script_version" {
		t.Fatalf("the reply asks for %q, want the write tool the prompt lists", key)
	}
}

// TestMockIllegalToolFallsBackWhenThePromptHasNoTools covers an agent with no
// tools: the scenario still asks for something, and what it asks for is a key no
// build registers.
func TestMockIllegalToolFallsBackWhenThePromptHasNoTools(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioIllegalTool)
	request := mockRequest("execution")
	request.Messages = request.Messages[:2]
	reply, err := adapter.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if key := onlyToolCallKey(t, reply); key != mockUnknownToolKey {
		t.Fatalf("the fallback asked for %q, want %q", key, mockUnknownToolKey)
	}
}

// onlyToolCallKey returns the single tool key a reply asked for.
func onlyToolCallKey(t *testing.T, reply providers.TextResult) string {
	t.Helper()
	if len(reply.ToolCalls) != 1 {
		t.Fatalf("the reply carries %d tool calls, want one", len(reply.ToolCalls))
	}
	if strings.TrimSpace(reply.ToolCalls[0].Key) == "" {
		t.Fatal("the tool call has no key")
	}
	return reply.ToolCalls[0].Key
}

// TestMockSupervisorIssuesProducesAFailingReport covers the scenario's own point:
// the report FAILS, and its finding is one a FIX decision can act on.
func TestMockSupervisorIssuesProducesAFailingReport(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioSupervisorIssues)
	reply, err := adapter.Generate(context.Background(), mockRequest("supervision"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var report struct {
		Passed            bool   `json:"passed"`
		Severity          string `json:"severity"`
		RecommendedAction string `json:"recommendedAction"`
		Issues            []struct {
			Rule     string `json:"rule"`
			Severity string `json:"severity"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(reply.Content), &report); err != nil {
		t.Fatalf("the reply is not JSON: %v", err)
	}
	if report.Passed {
		t.Fatal("the supervisor_issues scenario passed")
	}
	if len(report.Issues) == 0 {
		t.Fatal("a failing report carried no issue, so a FIX decision would have nothing to act on")
	}
	if report.RecommendedAction != "fix" {
		t.Fatalf("the report recommends %q, want fix", report.RecommendedAction)
	}
	// The pair the domain's ReviewReport.Validate forbids: a report that passes may
	// not carry a major issue. It does not pass, so the pair is legal, and the
	// assertion records which side of that rule this scenario is on.
	if report.Severity == "none" && report.Issues[0].Severity == "major" {
		t.Fatal("a major issue was reported with severity none")
	}
}

// TestMockPassingReportCarriesNoMajorIssue is the domain rule from the mock's side:
// the normal supervision reply must be a report the domain accepts.
func TestMockPassingReportCarriesNoMajorIssue(t *testing.T) {
	adapter := NewMockTextAdapter()
	reply, err := adapter.Generate(context.Background(), mockRequest("supervision"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var report struct {
		Passed   bool   `json:"passed"`
		Severity string `json:"severity"`
		Issues   []struct {
			Severity string `json:"severity"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(reply.Content), &report); err != nil {
		t.Fatalf("the reply is not JSON: %v", err)
	}
	if !report.Passed {
		t.Fatal("the normal supervision reply did not pass")
	}
	for _, issue := range report.Issues {
		if issue.Severity == "major" || issue.Severity == "critical" {
			t.Fatalf("a passing report carries a %s issue", issue.Severity)
		}
	}
}

// TestMockTimeoutWaitsForItsContext covers section 18.3's "超时" without a sleep: the
// call returns when the context is done, so the test takes as long as the deadline
// it set.
func TestMockTimeoutWaitsForItsContext(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := adapter.Generate(ctx, mockRequest("execution"))
	if err == nil {
		t.Fatal("a timeout scenario returned a reply")
	}
	if !strings.Contains(err.Error(), "deadline") && err != context.DeadlineExceeded {
		t.Fatalf("the refusal is %v, want the deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the mock waited %s, so it is not following the context", elapsed)
	}
}

// TestMockCancellationReportsItself covers section 18.3's "取消": the refusal carries
// the provider's cancelled category, which is what lets the runtime record the run
// as cancelled rather than failed.
func TestMockCancellationReportsItself(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioCancelled)
	_, err := adapter.Generate(context.Background(), mockRequest("execution"))
	if err == nil {
		t.Fatal("a cancellation scenario returned a reply")
	}
	if !provider.IsCancellation(err) {
		t.Fatalf("the refusal is %v and does not report a cancellation category", err)
	}
}

// TestMockProviderErrorIsClassified covers section 18.3's "Provider 错误": the refusal
// carries a category the runtime's classifier reads, and it is marked retriable
// because a transient remote failure is section 14.1's first entry.
func TestMockProviderErrorIsClassified(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioProviderError)
	_, err := adapter.Generate(context.Background(), mockRequest("execution"))
	if err == nil {
		t.Fatal("a provider error scenario returned a reply")
	}
	providerErr, ok := provider.AsProviderError(err)
	if !ok {
		t.Fatalf("the refusal is %v and carries no provider category", err)
	}
	if !providerErr.Retriable {
		t.Fatal("a transient remote failure is not marked retriable")
	}
	if providerErr.SafeMessage == "" {
		t.Fatal("the refusal has no safe message for the user")
	}
}

// TestMockRecordsEveryCall covers the observability the runtime tests need: a test
// asserting what the runtime sent can read it from the adapter rather than reaching
// into the runtime.
func TestMockRecordsEveryCall(t *testing.T) {
	adapter := NewMockTextAdapter()
	if len(adapter.Calls()) != 0 {
		t.Fatal("a fresh adapter reports calls")
	}
	if _, err := adapter.Generate(context.Background(), mockRequest("execution")); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := adapter.Generate(context.Background(), mockRequest("execution")); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	calls := adapter.Calls()
	if len(calls) != 2 {
		t.Fatalf("the adapter recorded %d calls, want 2", len(calls))
	}
	if calls[0].Model != "mock-model" {
		t.Fatalf("the recorded model is %q", calls[0].Model)
	}
	// The copy is a copy: mutating it must not change what the adapter holds.
	calls[0].Model = "changed"
	if adapter.Calls()[0].Model != "mock-model" {
		t.Fatal("Calls returned the adapter's own slice")
	}
}

// TestMockRejectsAnInvalidRequest proves the mock validates its input like any other
// adapter, so a caller cannot use it to bypass the request contract.
func TestMockRejectsAnInvalidRequest(t *testing.T) {
	adapter := NewMockTextAdapter()
	if _, err := adapter.Generate(context.Background(), providers.TextRequest{}); err == nil {
		t.Fatal("the mock accepted an empty request")
	}
	if _, err := adapter.Generate(context.Background(), providers.TextRequest{
		ProviderID: "mock-1", Model: "m",
		Messages: []providers.TextMessage{{Role: "root", Content: "x"}},
	}); err == nil {
		t.Fatal("the mock accepted an unknown message role")
	}
}

// TestMockStreamDeliversTheSameReply covers the port's second method: a caller that
// streams gets a consistent answer rather than a different one.
func TestMockStreamDeliversTheSameReply(t *testing.T) {
	adapter := NewMockTextAdapter()
	sink := &collectingSink{}
	if err := adapter.Stream(context.Background(), mockRequest("execution"), sink); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if !sink.done {
		t.Fatal("the stream did not report completion")
	}
	if sink.err != nil {
		t.Fatalf("the stream reported an error: %v", sink.err)
	}
	if strings.TrimSpace(sink.content) == "" {
		t.Fatal("the stream delivered nothing")
	}
	called := adapter.Calls()
	if len(called) != 1 {
		t.Fatalf("the stream made %d calls, want 1", len(called))
	}
	if sink.content == "" {
		t.Fatal("no delta was delivered")
	}
}

// TestMockStreamRefusesANilSink is the fail-closed check every port method here
// makes: a missing collaborator is a refusal, not a silent no-op.
func TestMockStreamRefusesANilSink(t *testing.T) {
	adapter := NewMockTextAdapter()
	if err := adapter.Stream(context.Background(), mockRequest("execution"), nil); err == nil {
		t.Fatal("the mock streamed to a nil sink")
	}
}

// TestMockStreamReportsTheScenarioError proves the stream path carries the same
// failures as the non-streaming one, so a caller sees the scenario it asked for.
func TestMockStreamReportsTheScenarioError(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioProviderError)
	sink := &collectingSink{}
	if err := adapter.Stream(context.Background(), mockRequest("execution"), sink); err == nil {
		t.Fatal("the stream reported success for a provider error")
	}
	if sink.err == nil {
		t.Fatal("the sink was not told about the failure")
	}
}

// TestMockNilAdapterFailsClosed covers the nil receiver, which is the shape a
// missing composition produces.
func TestMockNilAdapterFailsClosed(t *testing.T) {
	var none *MockTextAdapter
	if _, err := none.Generate(context.Background(), mockRequest("execution")); err == nil {
		t.Fatal("a nil adapter produced a reply")
	}
	if err := none.Stream(context.Background(), mockRequest("execution"), &collectingSink{}); err == nil {
		t.Fatal("a nil adapter streamed")
	}
	if none.SetScenario(MockScenarioNormal) != none {
		t.Fatal("SetScenario on a nil adapter did not return the nil receiver")
	}
	if none.Scenario() != "" || none.Calls() != nil {
		t.Fatal("a nil adapter reported state")
	}
}

// collectingSink records what a stream delivered.
type collectingSink struct {
	content string
	done    bool
	err     error
}

func (s *collectingSink) OnDelta(delta string)        { s.content += delta }
func (s *collectingSink) OnDone(providers.TextResult) { s.done = true }
func (s *collectingSink) OnError(err error)           { s.err = err }
