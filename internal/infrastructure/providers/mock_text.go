package providers

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// MockTextAdapter implements the text contract without contacting any provider.
//
// It is WP-07 scope item 13, and it exists for the reason AGENT_CONTRACTS section
// 18.3 gives: "CI 不调用真实付费模型". A test that needed a provider key could not run
// in CI, and one that reached a real model would cost money per run and answer
// differently every time. So the runtime tests drive this adapter instead, through
// the same TextPort the real adapters implement.
//
// Reachability is the whole design, and it follows the pattern WP-03 established
// for the media mocks — three independent guards, because the mock must never be
// what a user's project silently talks to:
//
//  1. It is resolved ONLY for provider.KindMockText, through the same kind switch
//     that resolves every other adapter. A provider whose kind is
//     openai_compatible never reaches this file.
//  2. KindMockText is refused by IsUserConfigurableKind and by the database CHECK
//     on provider_configs.kind, so a configuration row cannot be created for it
//     through the application path: a build cannot be tricked into persisting one.
//  3. The scenario is chosen by an explicit SETTER (SetScenario) that no
//     production code path calls. The zero value is the normal path, so an adapter
//     that was composed and never configured behaves as a well-formed model rather
//     than as a missing one.
//
// What it is NOT: a stub. It answers with documents that satisfy the real schemas,
// derived from the prompt it was given, so a test that passes here proves something
// about the validator, the authorizer and the workflow engine. A mock that returned
// a fixed string would leave every one of those untested.

// MockScenario is which of section 18.3's cases the mock should play.
//
// The names are the section's own list, in its order, so this file can be read
// against it. Two entries of that list — "Tool Call" and "拒绝非法 Tool" — are
// behaviors rather than documents: they decide WHAT the reply contains, and the
// runtime's tool layer is what acts on it.
type MockScenario string

const (
	// MockScenarioNormal is "正常结构": a document that satisfies the contract.
	MockScenarioNormal MockScenario = "normal"
	// MockScenarioInvalidOnce is "一次无效结构后二次有效": the first reply for a
	// conversation is malformed and every later one is valid, so the one repair
	// round of section 14.3 runs end to end and the stage then succeeds.
	MockScenarioInvalidOnce MockScenario = "invalid_once"
	// MockScenarioInvalidAlways is not in section 18.3's list but is the other half
	// of its first two: a model that never satisfies the contract, which is what
	// proves a failed repair writes nothing.
	MockScenarioInvalidAlways MockScenario = "invalid_always"
	// MockScenarioToolCall is "Tool Call": the reply asks for a tool the agent may
	// call, so the tool registry and the authorizer run.
	MockScenarioToolCall MockScenario = "tool_call"
	// MockScenarioIllegalTool is "拒绝非法 Tool": the reply asks for a tool the ACL
	// must refuse, so AC-AGENT-001's denial path runs.
	MockScenarioIllegalTool MockScenario = "illegal_tool"
	// MockScenarioSupervisorIssues is "Supervisor issues": a review report that
	// fails, with findings a FIX decision can act on.
	MockScenarioSupervisorIssues MockScenario = "supervisor_issues"
	// MockScenarioTimeout is "超时": the call blocks until its context is done.
	MockScenarioTimeout MockScenario = "timeout"
	// MockScenarioCancelled is "取消": the call reports the cancellation it was
	// given rather than a generic failure.
	MockScenarioCancelled MockScenario = "cancelled"
	// MockScenarioProviderError is "Provider 错误": the call fails the way an outage
	// does, with a category the runtime classifies.
	MockScenarioProviderError MockScenario = "provider_error"
)

// MockScenarios is section 18.3's list as a set, in its order.
//
// It exists so a test can assert the mock covers the section rather than asserting
// a list written out twice.
func MockScenarios() []MockScenario {
	return []MockScenario{
		MockScenarioNormal,
		MockScenarioInvalidOnce,
		MockScenarioToolCall,
		MockScenarioIllegalTool,
		MockScenarioSupervisorIssues,
		MockScenarioTimeout,
		MockScenarioCancelled,
		MockScenarioProviderError,
	}
}

// MockTextAdapter is a deterministic TextPort.
//
// It is safe for concurrent use: the invalid-once counter is shared state because
// a test may drive several agents in parallel.
type MockTextAdapter struct {
	mu sync.Mutex
	// scenario is what to play. It is set only through SetScenario.
	scenario MockScenario
	// seen counts replyable calls per conversation. It drives MockScenarioInvalidOnce,
	// which is "invalid the FIRST time" and valid afterwards.
	seen map[string]int
	// calls records every request, so a test can assert what the runtime sent —
	// which is how the prompt layers are checked from the provider's side rather
	// than from the runtime's.
	calls []providers.TextRequest
}

// NewMockTextAdapter builds the mock on its default scenario.
func NewMockTextAdapter() *MockTextAdapter {
	return &MockTextAdapter{seen: map[string]int{}}
}

// SetScenario chooses what the next replies should be.
//
// It RESETS the per-conversation counters, and that is a correction rather than a
// convenience: the counts exist to distinguish "the first reply for this conversation" from
// a repeat, and a scenario change is a new situation rather than a repeat of the old one. The
// first version kept the counts, so a test that ran the invalid-once scenario and then the
// invalid-always scenario got a VALID second reply — because the always-invalid scenario's
// first call was counted as the once-scenario's second. The canary found it: an assertion
// about a refusal saw a success.
//
// It is the third guard from the file comment made explicit, and it returns the adapter so a
// caller can chain it.
func (m *MockTextAdapter) SetScenario(scenario MockScenario) *MockTextAdapter {
	if m == nil {
		return m
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scenario = scenario
	m.seen = map[string]int{}
	return m
}

// Scenario reports the current scenario.
func (m *MockTextAdapter) Scenario() MockScenario {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scenario
}

// Calls returns every request the mock was given, in order.
func (m *MockTextAdapter) Calls() []providers.TextRequest {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]providers.TextRequest(nil), m.calls...)
}

// Generate answers the scenario.
//
// The reply is built from the PROMPT: it reads the messages it was given to decide
// whether it is being asked to review, to decide or to execute, and answers in the
// shape that agent's output schema describes. That is what makes the mock usable
// across all three layers without the caller having to tell it which one it is in.
func (m *MockTextAdapter) Generate(ctx context.Context, request providers.TextRequest) (providers.TextResult, error) {
	if m == nil {
		return providers.TextResult{}, provider.NewUnsupportedError()
	}
	if err := request.Validate(); err != nil {
		return providers.TextResult{}, err
	}
	m.mu.Lock()
	m.calls = append(m.calls, request)
	scenario := m.scenario
	conversation := conversationKey(request)
	m.seen[conversation]++
	call := m.seen[conversation]
	m.mu.Unlock()

	switch scenario {
	case MockScenarioTimeout:
		// Wait for the context rather than sleeping a fixed time, so a test takes as
		// long as the deadline it set and no longer. A cancel arrives here too,
		// because it also closes the context.
		<-ctx.Done()
		return providers.TextResult{}, ctx.Err()
	case MockScenarioCancelled:
		return providers.TextResult{}, provider.NewCancelledError()
	case MockScenarioProviderError:
		return providers.TextResult{}, provider.NewRemoteTransientError()
	}

	if scenario == MockScenarioInvalidOnce && call == 1 {
		// Valid JSON that violates the contract: the wrong schemaVersion, which is
		// the shape a real model failure takes and the one the repair round is for.
		return m.reply(`{"schemaVersion":2}`, request.Model), nil
	}
	if scenario == MockScenarioInvalidAlways {
		return m.reply(`{"schemaVersion":2}`, request.Model), nil
	}

	// The tool-call scenarios put the call BESIDE the document rather than inside it,
	// because section 7's schemas are additionalProperties false: a tool call inside a
	// returned document is refused by the schema that document must satisfy, so a mock
	// that put it there would produce a reply the validator rejects. textToolCalls
	// reads what the prompt's contract says the agent may and may not call.
	result := m.reply(mockReplyFor(request, scenario, call), request.Model)
	if scenario == MockScenarioToolCall {
		key := mockOwnTool(request)
		result.ToolCalls = []providers.TextToolCall{{
			Key: key, Arguments: mockToolArguments(key, request),
		}}
	}
	if scenario == MockScenarioIllegalTool {
		result.ToolCalls = []providers.TextToolCall{{
			Key: mockForeignTool(request), Arguments: json.RawMessage(`{}`),
		}}
	}
	return result, nil
}

// Stream is not the runtime's path.
//
// The runtime calls Generate, because section 8's call chain validates a COMPLETE
// document: a review that streamed its issues would have to be buffered whole
// before anything could be checked, so streaming would add a failure mode without
// removing one. The method exists because TextPort declares it, and it delivers the
// same reply as one delta followed by a done event so a caller that does stream
// still gets a consistent answer.
func (m *MockTextAdapter) Stream(ctx context.Context, request providers.TextRequest, sink providers.EventSink) error {
	if sink == nil {
		return provider.NewInvalidInputError()
	}
	result, err := m.Generate(ctx, request)
	if err != nil {
		sink.OnError(err)
		return err
	}
	if err := ctx.Err(); err != nil {
		sink.OnError(err)
		return err
	}
	sink.OnDelta(result.Content)
	sink.OnDone(result)
	return nil
}

// reply wraps content in a TextResult.
func (m *MockTextAdapter) reply(content, model string) providers.TextResult {
	if strings.TrimSpace(model) == "" {
		// A model that was not named still reports which one answered, so a run's
		// record is not blank.
		model = "mock-text"
	}
	return providers.TextResult{Content: content, Model: model, FinishReason: "stop"}
}

// conversationKey identifies the conversation a call belongs to.
//
// It uses the system messages rather than a counter, so "the first call" means the
// first call for THIS agent: the runtime sends the same policy and skill layers on
// every call of a run, and two runs of different agents differ in them. The
// alternative — counting globally — would make the repair round depend on how many
// other agents had run first, which is exactly the kind of order dependence a
// deterministic mock must not have.
func conversationKey(request providers.TextRequest) string {
	var builder strings.Builder
	for _, message := range request.Messages {
		if message.Role != "system" {
			continue
		}
		builder.WriteString(message.Content)
		builder.WriteByte('\n')
	}
	key := builder.String()
	if key == "" {
		// No system layer at all is not a shape the runtime produces, but a caller
		// driving the port directly might. One bucket is the honest answer.
		return "(no system messages)"
	}
	return key
}

// mockReplyFor builds the document the scenario asks for.
//
// The decision of WHICH document is made from the prompt, because the port has no
// other way to know what was asked: an agent's layer policy names the layer and its
// output schema is named in the manifest. Reading the prompt for that is what the
// mock is FOR — it answers in the shape the caller's schema describes.
//
// The two tool-call scenarios do not appear here: their tool calls travel BESIDE the
// document, and Generate attaches them. What this function returns for them is the
// document an agent reports around a tool call, which is a partial result rather than
// a success, because the mock has not run the tool and cannot know what it wrote.
func mockReplyFor(request providers.TextRequest, scenario MockScenario, call int) string {
	switch scenario {
	case MockScenarioSupervisorIssues:
		return mockReviewReport(request, false)
	case MockScenarioIllegalTool, MockScenarioToolCall:
		// One branch for both: the document is the same, and which tool is attached is
		// Generate's decision.
		return mockToolCallDocument(request)
	}
	// The extraction agent is an EXECUTION agent whose output contract is the event graph
	// rather than an ExecutionResult, so the layer alone does not decide the shape: the
	// agent's own schema does. Without this branch the mock answered an extraction run with
	// an execution-result document, which the extraction contract refused — so the runtime
	// reported a schema failure and the extraction path could not run against the mock at all.
	//
	// That is what the wiring test found: the seam WP-06 recorded was closed in code and
	// unverifiable, because the deterministic model could not answer the one agent the seam
	// exists for.
	if mockIsExtractionAgent(request) {
		return mockExtractionDocument(request)
	}
	switch mockLayerOf(request) {
	case "supervision":
		return mockReviewReport(request, true)
	case "decision":
		return mockDecisionResult(request, call)
	default:
		return mockExecutionResult(request)
	}
}

// mockIndexString renders a small index as a decimal string, so the refs this file builds
// (e0, e1, e2) do not depend on a formatting package for three characters.
func mockIndexString(value int) string {
	if value <= 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// mockIsExtractionAgent reports whether the request is for the event-extraction agent.
//
// It reads the SKILL layer's title, which the generator writes as `# script/script.<name>`
// (and `# production/production.<name>`), so the check names the agent the runtime actually
// constructed the prompt for rather than guessing from the text. The tool contract would be
// a second way to tell — the extraction agent is the only one granted
// story.read_chapter_text — and the skill title is the cheaper one and the one a rename
// cannot silently break, because the pack generator and the manifest both derive from it.
func mockIsExtractionAgent(request providers.TextRequest) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, "event_extraction") {
			return true
		}
	}
	return false
}

// mockExtractionDocument answers an extraction run in the event graph's shape.
//
// The names come from the task's own text, by the same positional rule the extraction
// mock in the extraction package uses (the leading two characters of a Han run), so every
// name it proposes APPEARS in the chapter and the evidence offsets the service records are
// real. A mock that invented names would produce a document the service's reference
// resolution refused, and the test would be asserting about a failure it caused itself.
func mockExtractionDocument(request providers.TextRequest) string {
	text := mockTaskText(request)
	names := mockScanNames(text)
	entities := make([]any, 0, len(names))
	for index, name := range names {
		kind := "character"
		if index > 0 {
			kind = "location"
		}
		entities = append(entities, map[string]any{
			"ref": "e" + mockIndexString(index), "type": kind, "canonicalName": name,
		})
	}
	document := map[string]any{
		"schemaVersion": 1,
		"summary":       "A deterministic reading of the chapter.",
		"entities":      entities,
		"events":        []any{},
		"relations":     []any{},
	}
	if len(entities) == 0 {
		// A chapter with nothing name-like still produces a valid, empty reading rather than
		// a document with an unresolved reference.
		document["summary"] = "A deterministic reading that found nothing to propose."
		return mockJSON(document)
	}
	document["events"] = []any{map[string]any{
		"ref": "ev1", "name": "Something happens", "eventType": "scene",
		"storyTimeOrder": 1, "confidence": 0.5,
		"participants": []any{map[string]any{"entityRef": "e0", "role": "actor"}},
	}}
	if len(entities) > 1 {
		document["relations"] = []any{map[string]any{
			"sourceRef": "e0", "targetRef": "e1", "relationType": "knows",
			"validFromEventRef": "ev1", "confidence": 0.4,
		}}
	}
	return mockJSON(document)
}

// mockTaskText returns the untrusted task layer's content, which is the chapter's text.
func mockTaskText(request providers.TextRequest) string {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, UntrustedOpening) {
			if start := strings.Index(message.Content, UntrustedOpening); start >= 0 {
				body := message.Content[start+len(UntrustedOpening):]
				if end := strings.Index(body, UntrustedClosing); end >= 0 {
					return strings.TrimSpace(body[:end])
				}
				return strings.TrimSpace(body)
			}
		}
	}
	return ""
}

// mockScanNames finds name-like spans by a positional rule.
//
// It is deliberately crude and stated rather than disguised: the leading two characters of
// each run of Han script, and the first Latin word of each run of letters. The rule has no
// notion of MEANING, which is what makes an injected document a real test — the text is
// scanned like any other text and cannot change what the mock returns.
func mockScanNames(text string) []string {
	var names []string
	seen := map[string]bool{}
	runes := []rune(text)
	add := func(name string) bool {
		if len([]rune(name)) < 2 || seen[name] {
			return false
		}
		seen[name] = true
		names = append(names, name)
		return len(names) == 3
	}
	for index := 0; index < len(runes); {
		switch {
		case isHanRune(runes[index]):
			start := index
			for index < len(runes) && isHanRune(runes[index]) {
				index++
			}
			if run := runes[start:index]; len(run) >= 2 {
				if add(string(run[:2])) {
					return names
				}
			}
		case isLatinRune(runes[index]):
			start := index
			for index < len(runes) && isLatinRune(runes[index]) {
				index++
			}
			if add(string(runes[start:index])) {
				return names
			}
		default:
			index++
		}
	}
	return names
}

// UntrustedOpening and UntrustedClosing delimit untrusted content in a prompt.
//
// They are the same two tags the runtime's assembler emits, written out here rather than
// imported because this package must not import the runtime: the adapter and the runtime meet
// only through the port, and the tags are part of the PROMPT format both ends agree on. A test
// asserts the two spellings match, so a rename on one side cannot silently break the other.
const (
	UntrustedOpening = "<UNTRUSTED_SOURCE_DOCUMENT>"
	UntrustedClosing = "</UNTRUSTED_SOURCE_DOCUMENT>"
)

// isHanRune reports whether a rune is in the CJK unified ideographs block.
func isHanRune(value rune) bool { return value >= 0x4E00 && value <= 0x9FFF }

// isLatinRune reports whether a rune is an ASCII letter.
func isLatinRune(value rune) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}

// mockLayerOf reads the layer out of the prompt.
//
// The layer policy layer (section 5's layer 2) is the runtime's own text and it
// begins by naming the layer, so this reads a fact the runtime wrote rather than
// guessing from a skill. A prompt with no policy layer reports the empty string,
// which lands on the execution shape: it is the one whose schema has the fewest
// required fields, so an unrecognised prompt produces a document that still gets
// exercised by the validator.
func mockLayerOf(request providers.TextRequest) string {
	for _, message := range request.Messages {
		content := message.Content
		switch {
		case strings.Contains(content, "You review what the artifact actually contains"):
			return "supervision"
		case strings.Contains(content, "You choose among the actions the runtime gives you"):
			return "decision"
		case strings.Contains(content, "You do the one narrow step you were given"):
			return "execution"
		}
	}
	return ""
}

// mockExecutionResult answers an execution agent.
//
// The shape follows ExecutionResult, whose required fields are schemaVersion,
// status, stage and stageRunId. The values come from the REQUEST, which carries the
// stage run identifier in its workflow-state layer, so the reply names the stage the
// runtime is actually running. A mock that invented one would fail the runtime's own
// cross-check and prove nothing.
//
// artifacts is deliberately ABSENT and nextAction is "wait_user": a stage that
// produced no artifact must not claim success, and section 7.4's rule that "success
// 至少有预期 artifact" is what decides. So the mock reports what an execution with
// no writes honestly is, which is a partial that needs a person — and a caller that
// wants the success path uses MockScenarioToolCall, whose tool does the write.
func mockExecutionResult(request providers.TextRequest) string {
	stage, stageRun := mockStageOf(request)
	document := map[string]any{
		"schemaVersion": 1,
		"status":        "partial",
		"stage":         stage,
		"stageRunId":    stageRun,
		"nextAction":    "wait_user",
		"summary":       "The deterministic mock read the stage and proposed nothing to write.",
	}
	return mockJSON(document)
}

// mockDecisionResult answers a decision agent.
//
// nextAction is "request_approval" rather than "run_execution", because section 9
// makes the runtime — not the model — responsible for whether a stage may run, and
// a mock that asked to run one would be asserting a fact about the database it does
// not have. Asking for the user's decision is always legal, which is what makes this
// a reply the runtime can act on from any state.
func mockDecisionResult(request providers.TextRequest, call int) string {
	stage, _ := mockStageOf(request)
	document := map[string]any{
		"schemaVersion": 1,
		"status":        "wait_user",
		"intent":        "The deterministic mock's reading of the user's request.",
		"reasonSummary": "No execution was proposed: the mock reports the state it was given and asks for a decision.",
		"nextAction":    map[string]any{"type": "request_approval"},
		"userMessage":   "The mock reviewed the current state and is asking you to decide.",
	}
	if stage != "" {
		document["currentStage"] = stage
	}
	return mockJSON(document)
}

// mockReviewReport answers a supervision agent.
//
// A passing report carries no issues and severity "none", which is the pair the
// domain's ReviewReport.Validate demands: a report that passes may not carry a major
// or critical issue. The failing form carries one major issue, which is what a FIX
// decision acts on. Both are built here rather than in two functions because the
// difference between them is exactly two fields, and writing them twice is how they
// drift apart.
func mockReviewReport(request providers.TextRequest, passed bool) string {
	stage, stageRun := mockStageOf(request)
	document := map[string]any{
		"schemaVersion":     1,
		"passed":            passed,
		"severity":          "none",
		"stage":             stage,
		"stageRunId":        stageRun,
		"rulesetVersion":    mockRulesetVersion,
		"issues":            []any{},
		"recommendedAction": "pass",
	}
	if passed {
		document["summary"] = "The deterministic mock found nothing to report."
		return mockJSON(document)
	}
	document["severity"] = "major"
	document["recommendedAction"] = "fix"
	document["summary"] = "The deterministic mock reports one finding."
	document["issues"] = []any{map[string]any{
		"rule":     "MOCK_DETERMINISTIC_FINDING",
		"severity": "major",
		"problem":  "The deterministic mock reports a finding at a named location.",
		"location": "mock/location",
	}}
	return mockJSON(document)
}

// mockToolCallDocument is the document an agent reports around a tool call.
//
// It is a PARTIAL rather than a success, and that is the honest shape: the model has
// asked for a tool and has not been told what it returned, so it cannot report an
// artifact. The runtime's own rule — section 7.4's "success 至少有预期 artifact" —
// is what would refuse a success here, and this reply does not need to be refused.
func mockToolCallDocument(request providers.TextRequest) string {
	switch mockLayerOf(request) {
	case "decision":
		stage, _ := mockStageOf(request)
		document := map[string]any{
			"schemaVersion": 1,
			"status":        "wait_user",
			"intent":        "The deterministic mock's reading of the user's request.",
			"reasonSummary": "The mock asked for one tool before settling on an action.",
			"nextAction":    map[string]any{"type": "request_approval"},
			"userMessage":   "The mock is reading the current state before it decides.",
		}
		if stage != "" {
			document["currentStage"] = stage
		}
		return mockJSON(document)
	case "supervision":
		// A supervision agent's contract is the REVIEW REPORT, and it shares no fields with
		// an execution result. Without this branch a supervisor asking for a tool received an
		// execution-shaped document, which the review-report schema refused — so the ACL was
		// never reached and a test of the REFUSAL would have been testing the validator
		// instead. The canary found it.
		stage, stageRun := mockStageOf(request)
		return mockJSON(map[string]any{
			"schemaVersion":     1,
			"passed":            true,
			"severity":          "none",
			"stage":             stage,
			"stageRunId":        stageRun,
			"rulesetVersion":    mockRulesetVersion,
			"issues":            []any{},
			"summary":           "The mock read what it needed and has nothing to report.",
			"recommendedAction": "pass",
		})
	default:
		stage, stageRun := mockStageOf(request)
		return mockJSON(map[string]any{
			"schemaVersion": 1,
			"status":        "partial",
			"stage":         stage,
			"stageRunId":    stageRun,
			"nextAction":    "wait_user",
			"summary":       "The mock asked for one tool and has no result to report yet.",
		})
	}
}

// mockStageOf reads the stage and stage-run identifiers out of the prompt.
//
// Both come from the workflow-state layer (section 5's layer 5), which the runtime
// rendered from the database. Reading them from there rather than inventing them is
// what makes the reply consistent with the run: the runtime cross-checks an
// ExecutionResult's stage and stageRunId against the invocation, and a mock that
// guessed would be testing that check with a value it made up.
func mockStageOf(request providers.TextRequest) (string, string) {
	state := mockWorkflowStateOf(request)
	return fieldOnLine(state, "stage="), fieldOnLine(state, "stage_run=")
}

// mockWorkflowStateOf returns the prompt's workflow-state layer, if any.
func mockWorkflowStateOf(request providers.TextRequest) string {
	for _, message := range request.Messages {
		if strings.HasPrefix(message.Content, "Current workflow state:") {
			return message.Content
		}
	}
	return ""
}

// fieldOnLine finds `name=value` inside a block of state text.
//
// The separator is the newline and the space, so a value stops where the runtime's
// own rendering put the next field. It returns the empty string when the field is
// absent, and the callers above treat that as "the state did not say" rather than
// substituting something.
func fieldOnLine(state, name string) string {
	if state == "" {
		return ""
	}
	for _, line := range strings.FieldsFunc(state, func(r rune) bool { return r == '\n' || r == ' ' }) {
		if strings.HasPrefix(line, name) {
			return strings.TrimPrefix(line, name)
		}
	}
	return ""
}

// mockOwnTool returns a tool the agent may call.
//
// It reads the prompt's own tool contract (layer 4), which is where the runtime
// listed exactly the tools the agent is granted. So this returns something the ACL
// will ALLOW, which is what makes MockScenarioToolCall exercise the handler path
// rather than the denial path.
func mockOwnTool(request providers.TextRequest) string {
	contract := mockToolContractOf(request)
	// A WRITE tool is preferred, and that is the correction the canary found: the scenario
	// exists to exercise the path where a model asks for something and the runtime DOES it,
	// so a call that only reads would leave the interesting half — the artifact the stage
	// reports and the verification that follows it — untested. The first version returned the
	// first tool in the contract, which for every execution agent is a read tool, so the
	// stage's write path was never reached by this scenario at all.
	fallback := ""
	for _, line := range strings.Split(contract, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "- "))
		if len(fields) == 0 {
			continue
		}
		key := fields[0]
		if !strings.Contains(key, ".") || strings.HasSuffix(key, "s:") {
			continue
		}
		if strings.Contains(line, "(write)") {
			return key
		}
		if fallback == "" {
			fallback = key
		}
	}
	if fallback != "" {
		return fallback
	}
	// No tool contract means the agent has no tools, which is legitimate. The reply
	// then names a key that is certainly not registered, so the denial path runs and
	// the scenario still tests what it says it does.
	return mockUnknownToolKey
}

// mockForeignTool returns a tool the ACL must refuse.
//
// It looks for a tool in the contract that the agent's layer may NOT call — the
// first write tool offered to a supervision agent, or the first tool of any kind
// when the agent has none. Finding one from the agent's own list is the stronger
// test, because it proves the refusal came from the MODE matrix rather than from the
// key being unknown; the fallback covers an agent with no tools at all.
func mockForeignTool(request providers.TextRequest) string {
	contract := mockToolContractOf(request)
	for _, line := range strings.Split(contract, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if strings.Contains(line, "(write)") || strings.Contains(line, "(external)") {
			fields := strings.Fields(strings.TrimPrefix(line, "- "))
			if len(fields) > 0 && strings.Contains(fields[0], ".") {
				return fields[0]
			}
		}
	}
	return mockUnknownToolKey
}

// mockUnknownToolKey is a well-formed tool key that no build registers.
//
// It exists so MockScenarioIllegalTool has an answer even for an agent with no
// tools. Written rather than generated, because a generated key could collide with a
// real tool in a future table and turn a refusal test into a success test.
const mockUnknownToolKey = "mock.nonexistent_tool"

// mockRulesetVersion names the mock's ruleset.
//
// Section 7.6 requires a ruleset version, and it is a fact about the review that a
// caller may branch on. A constant rather than the skill version, because the
// ruleset is what the findings were checked against and the mock checks none.
const mockRulesetVersion = "mock-ruleset.v1"

// mockToolContractOf returns the prompt's tool-contract layer.
func mockToolContractOf(request providers.TextRequest) string {
	for _, message := range request.Messages {
		if strings.HasPrefix(message.Content, "Tools you may call") ||
			message.Content == "You have no tools for this step." {
			return message.Content
		}
	}
	return ""
}

// mockJSON renders a document deterministically.
//
// encoding/json is used rather than a hand-written string for the reason WP-06's
// mock hand-wrote its escaping and had to: this mock's documents carry no document
// text, so there is nothing whose escaping has to be pinned, and the encoder's map
// ordering is sorted, which makes the output stable anyway.
func mockJSON(document map[string]any) string {
	// Marshal cannot fail for a map of strings, numbers, booleans and slices, which
	// is all this file builds. The fallback is a valid document rather than a panic:
	// a mock that panicked would take the test binary down for a reason that has
	// nothing to do with what it was testing.
	encoded, err := json.Marshal(document)
	if err != nil {
		return `{"schemaVersion":1}`
	}
	return string(encoded)
}

// Compile-time proof that the mock satisfies the port it stands in for.
var _ providers.TextPort = (*MockTextAdapter)(nil)

// mockToolArguments builds the arguments a mock tool call carries.
//
// The FIRST version sent `{}` for every tool, which the tool's own input schema then refused
// — so the runtime recorded a tool failure rather than the success the scenario exists to
// produce, and a test of the write path was testing the failure path instead. The canary
// found it: the stage failed with "a tool this step needed did not complete".
//
// The values come from the PROMPT, which is where the runtime put them: the workflow-state
// layer carries the episode and the stage run, so a call that writes a version can name the
// episode the run is about. An argument this cannot supply is omitted, and the schema then
// refuses the call — which is the honest outcome for a scenario whose prompt did not carry
// what the tool needs, rather than a fabricated identifier.
func mockToolArguments(key string, request providers.TextRequest) json.RawMessage {
	state := mockWorkflowStateOf(request)
	arguments := map[string]any{}
	// Every write tool that creates a version for an episode takes this field, and the
	// runtime's own state layer named the episode when the caller supplied one.
	if episode := fieldOnLine(state, "episode="); episode != "" {
		arguments["episodeId"] = episode
	}
	// The storyboard panel tool takes an item rather than an episode, and the state layer
	// names one when the run is a panel run.
	if item := fieldOnLine(state, "storyboard_item="); item != "" {
		arguments["itemId"] = item
	}
	// A read that narrows by chapter takes one, for the same reason.
	if chapter := fieldOnLine(state, "chapter="); chapter != "" {
		arguments["chapterId"] = chapter
	}
	if len(arguments) == 0 {
		// Nothing usable was in the prompt. An empty object is returned rather than a guess,
		// and the tool's schema decides whether that is acceptable.
		return json.RawMessage(`{}`)
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}
