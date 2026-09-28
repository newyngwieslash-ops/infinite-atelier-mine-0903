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
		key, arguments := mockScriptToolCall(request)
		result.ToolCalls = []providers.TextToolCall{{Key: key, Arguments: arguments}}
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

// mockScriptStageOf reports which of the three SCRIPT execution stages this request is for.
//
// It reads the SKILL layer's TITLE, which is a line beginning `# script/script.execution.<name>`, and
// nothing else — the first version scanned every message for the stage's substring and got the wrong
// answer, because a tool contract lists `script.create_story_skeleton_version` for the STRATEGY agent
// too (a strategy is written from a skeleton). A matcher that can be satisfied by a tool name is a
// matcher that reports whichever stage happens to appear first in a list.
//
// The title is the same signal `mockIsExtractionAgent` reads, for the same reason: it names the agent
// the runtime constructed the prompt for, and the pack generator and the manifest both derive it from
// the agent's own key, so a rename cannot silently break it.
//
// The HasPrefix is STRICTER THAN IT NEEDS TO BE, and a mutation pass established that: relaxing it to
// a Contains kills no test, because the generator always writes the title on the first line of the
// skill, so a later line quoting another stage's name can never be reached first. It is kept strict
// because a skill document is prose a person writes, and "the title is the first line" is a
// convention the generator currently honours rather than a rule this matcher enforces. The cost of
// keeping it is one comparison per line.
func mockScriptStageOf(request providers.TextRequest) string {
	return mockStageWithPrefix(request, "# script/script.execution.")
}

// mockProductionStageOf reports which production stage this request is for.
//
// The prefix is the PRODUCTION pack's, and the two are separate lookups rather than one
// with a longer list because the pack name is what tells them apart: a script stage and
// a production stage can share a name (`storyboard_table` appears in neither pack's
// execution keys, but a future one might), and a matcher that ignored the pack would
// answer the wrong stage's write.
func mockProductionStageOf(request providers.TextRequest) string {
	return mockStageWithPrefix(request, "# production/production.execution.")
}

// mockSupervisionStageOf reports which stage's SUPERVISOR this request is for.
//
// The supervisor's key has its own prefix, so the three lookups are disjoint: an
// execution request never matches here and a supervisor never matches either of the
// others. That is what lets one mock answer a whole pipeline without guessing.
func mockSupervisionStageOf(request providers.TextRequest) string {
	if stage := mockStageWithPrefix(request, "# production/production.supervision."); stage != "" {
		return stage
	}
	return mockStageWithPrefix(request, "# script/script.supervision.")
}

// mockStageWithPrefix finds the stage name in a skill title line.
//
// The title is the whole line, so anything after the name is not part of it — which is
// what makes the lookup specific rather than a substring match: a stage whose name is a
// PREFIX of another (a hypothetical `storyboard_table` and `storyboard_table_fix`) would
// otherwise be answered by whichever came first.
func mockStageWithPrefix(request providers.TextRequest, prefix string) string {
	for _, message := range request.Messages {
		for _, line := range strings.Split(message.Content, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}
			stage := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			if index := strings.IndexAny(stage, " \t"); index >= 0 {
				stage = stage[:index]
			}
			if stage != "" {
				return stage
			}
		}
	}
	return ""
}

// mockScriptToolCall is the write a script execution stage asks for.
//
// The KEY is chosen rather than searched for: the contract lists several tools and only one produces
// the artifact this stage owes, so "the first write tool" picks the wrong one for generation. The
// arguments come from the prompt for the reason `mockToolArguments` records — the workflow-state layer
// carries the episode and the versions the run is about, so a call that writes names what it writes
// about instead of inventing an identifier the artifact verifier would then refuse.
func mockScriptToolCall(request providers.TextRequest) (string, json.RawMessage) {
	if stage := mockProductionStageOf(request); stage != "" {
		return mockProductionToolCall(request, stage)
	}
	switch mockScriptStageOf(request) {
	case "story_skeleton":
		return "script.create_story_skeleton_version", mockSkeletonArguments(request)
	case "adaptation_strategy":
		return "script.create_adaptation_strategy_version", mockStrategyArguments(request)
	case "script_generation":
		// The STRUCTURE write is what this stage owes. Its version row is created by the pipeline
		// before the model is called, because a structure needs a version to hang off — so the state
		// layer names that version and this call fills it.
		return "script.create_script_structure", mockStructureArguments(request)
	default:
		key := mockOwnTool(request)
		return key, mockToolArguments(key, request)
	}
}

// mockSkeletonArguments states a skeleton with every field the schema wants.
//
// The selection comes from the prompt when the state names events, so the link set is a real relation
// to rows the project has. When it names none the selection is empty, which the schema allows and the
// service accepts: a mock that invented event ids would fail the reference check the service now
// makes, and the test would then be asserting about a failure it caused itself.
func mockSkeletonArguments(request providers.TextRequest) json.RawMessage {
	state := mockWorkflowStateOf(request)
	arguments := map[string]any{
		"openingHook":       "The deterministic mock's opening: a name is spoken before it is explained.",
		"coreConflict":      "The mock's conflict: the person who knows the name will not say it.",
		"turningPointsJson": `["the name is spoken","the board is found"]`,
		"climax":            "The mock's climax: the board turns up in the water.",
		"endingHook":        "The mock's ending: someone is waiting at the far bank.",
		"changeReason":      "The deterministic mock answered a story_skeleton stage.",
	}
	if episode := fieldOnLine(state, "episode="); episode != "" {
		arguments["episodeId"] = episode
	}
	if events := fieldOnLine(state, "selected_events="); events != "" {
		arguments["selectedEventIds"] = splitStateList(events)
	}
	return mockArgumentJSON(arguments)
}

// mockStrategyArguments states a strategy with one treatment per event the prompt named.
//
// Every event is RETAINED, and the treatment is stated rather than left out: a treatment with no
// decision is refused by the service, because "retained" and "removed" are opposite decisions and
// neither follows from an absence. The order is the state's, which is the adaptation's order.
func mockStrategyArguments(request providers.TextRequest) json.RawMessage {
	state := mockWorkflowStateOf(request)
	links := []any{}
	for _, event := range splitStateList(fieldOnLine(state, "selected_events=")) {
		links = append(links, map[string]any{"storyEventId": event, "treatment": "retained"})
	}
	arguments := map[string]any{
		"strategySummary":       "The deterministic mock keeps every event it was given, in order.",
		"adaptationMode":        "balanced",
		"mergedEventGroupsJson": `[]`,
		"rationale":             "The mock answered an adaptation_strategy stage.",
		"eventLinks":            links,
	}
	if episode := fieldOnLine(state, "episode="); episode != "" {
		arguments["episodeId"] = episode
	}
	return mockArgumentJSON(arguments)
}

// mockStructureArguments states a whole script version's content.
//
// It writes TWO SCENES with lines and a shot, and it states no identifier, ordinal or duration — the
// schema has no field for any of them, which is §17's "ID、顺序和唯一性" and "时长求和" being the code's
// job rather than a model's. The scene durations are FIXED constants rather than derived from the
// prompt, because what a test needs from this mock is a version whose summed duration is a known
// number: the canary asserts the derived total against what the scenes state, and a mock that varied
// its durations would make that assertion depend on the fixture instead of on the code.
//
// The second scene is marked `isOriginalAdaptation` (原创改编标记) and cites no source event, so the
// canary can tell an invention from a faithful adaptation — the distinction AC-SCRIPT-003 asks for.
// mockStructureArguments builds the generation stage's structure write.
//
// THE DOCUMENT IS FIXED UNLESS A CALLER ASKS FOR MORE, and that is what keeps every existing test
// honest: the canary asserts a derived duration against the scenes stated here, so a mock that
// varied its output would make those assertions depend on a fixture rather than on the code. A
// caller with a SHAPE requirement — AC-E2E-002 asks for a board of at least twelve shots from one
// episode — writes `shot_count=` into the state, and only then does this document grow. The fixed
// branch is the default; the sized branch is opt-in.
func mockStructureArguments(request providers.TextRequest) json.RawMessage {
	state := mockWorkflowStateOf(request)
	if wanted := parseIntField(fieldOnLine(state, "shot_count=")); wanted > 1 {
		return mockSizedStructureArguments(state, wanted, fieldOnLine(state, "episode="), fieldOnLine(state, "script_version="))
	}
	arguments := map[string]any{
		"summary": "The deterministic mock's script: two scenes at the ferry crossing.",
		"scenes": []any{
			map[string]any{
				"sceneNumber":              "1",
				"slugline":                 "INT. 渡口 - 日",
				"interiorExterior":         "INT",
				"timeOfDay":                "日",
				"summary":                  "白掌柜念出那个名字。",
				"dramaticGoal":             "交代铜牌",
				"estimatedDurationSeconds": 90,
				"isOriginalAdaptation":     false,
				"dialogueLines": []any{
					map[string]any{"type": "dialogue", "text": "这牌子不是你的。"},
					map[string]any{"type": "narration", "text": "雾气漫上来。"},
				},
				"shots": []any{
					map[string]any{
						"shotNumber": "1A", "shotSize": "wide", "visualDescription": "河面起雾，渡船靠岸。",
					},
				},
			},
			map[string]any{
				"sceneNumber":              "2",
				"slugline":                 "EXT. 渡口 - 夜",
				"interiorExterior":         "EXT",
				"timeOfDay":                "夜",
				"summary":                  "铜牌入水。",
				"dramaticGoal":             "留下悬念",
				"estimatedDurationSeconds": 60,
				"isOriginalAdaptation":     true,
				"dialogueLines":            []any{},
				"shots":                    []any{},
			},
		},
	}
	if version := fieldOnLine(state, "script_version="); version != "" {
		arguments["versionId"] = version
	}
	if episode := fieldOnLine(state, "episode="); episode != "" {
		arguments["episodeId"] = episode
	}
	return mockArgumentJSON(arguments)
}

// splitStateList reads a comma-separated list out of a workflow-state field.
//
// The separator is a comma rather than the space `fieldOnLine` stops at, so a state can carry a
// list in one field. Blank entries are dropped rather than passed through, because an empty id would
// be refused by the service's own trimming and the refusal would name nothing.
func splitStateList(value string) []string {
	out := []string{}
	for _, entry := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// mockArgumentJSON renders a tool's arguments deterministically.
//
// Separate from `mockJSON`, which renders a SCHEMA document. The encoder's sorted map ordering is what
// makes the output stable, so two runs of the same scenario produce the same bytes — which is the
// property every assertion in this file rests on.
func mockArgumentJSON(document map[string]any) json.RawMessage {
	encoded, err := json.Marshal(document)
	if err != nil {
		// An argument document that will not marshal is this file's bug. Returning an empty object
		// lets the tool's own schema refuse it, which is a readable failure in a test rather than a
		// panic that takes the binary down for a reason unrelated to what was being tested.
		return json.RawMessage(`{}`)
	}
	return encoded
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
//
// ONE EXCEPTION, and it is the script stages'. The tool-call scenario is what writes
// their artifact, so a reply that said `partial` with no artifact would be describing
// a run whose WRITE is happening beside it — and the runtime's own rule reads the
// document, not the tool call. A `script.*` execution stage therefore answers
// `complete` WITH the reference the tool is about to create, which is the shape the
// runtime's success rule requires. The reference names the version the state layer
// carried, so it is the row the tool will write rather than an invented one.
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
	// The three script stages write through a tool call, so their document carries the artifact
	// reference and says what it is doing.
	//
	// The reference is REQUIRED for `complete`, and that is §7.4's rule rather than this file's
	// choice: "success 至少有预期 artifact", which the runtime enforces ON TOP of the schema. A reply
	// that said complete with no artifact is refused by the runner — a canary found exactly that when
	// this branch was added — so a state that cannot name the row leaves the reply at `partial`. That
	// is the honest reading: the stage did its work, and this mock cannot say what row it produced.
	if productionStage := mockProductionStageOf(request); productionStage != "" {
		entityType, entityID := mockProductionArtifactOf(request, productionStage)
		if entityID == "" {
			document["summary"] = "The deterministic mock ran the " + productionStage +
				" stage, and the state named no version to report an artifact for."
			return mockJSON(document)
		}
		document["status"] = "complete"
		document["nextAction"] = "review"
		document["summary"] = "The deterministic mock wrote the " + productionStage + " artifact."
		document["artifacts"] = []any{map[string]any{
			"entityType": entityType,
			"entityId":   entityID,
			"operation":  "created",
		}}
		return mockJSON(document)
	}
	if scriptStage := mockScriptStageOf(request); scriptStage != "" {
		entityType, entityID := mockScriptArtifactOf(request, scriptStage)
		if entityID == "" {
			document["summary"] = "The deterministic mock ran the " + scriptStage +
				" stage, and the state named no version to report an artifact for."
			return mockJSON(document)
		}
		document["status"] = "complete"
		document["nextAction"] = "review"
		document["summary"] = "The deterministic mock wrote the " + scriptStage + " artifact."
		document["artifacts"] = []any{map[string]any{
			"entityType": entityType,
			"entityId":   entityID,
			"operation":  "created",
		}}
	}
	return mockJSON(document)
}

// mockScriptArtifactOf names the row a script stage's write creates.
//
// The identifier comes from the workflow state, which the pipeline rendered from the database — so
// this reports the version the run is actually about. An empty id means the state did not name one,
// and the artifact list is then omitted rather than filled with an invention: the runtime's verifier
// reads these references back, and a fabricated id would fail it for a reason that is this file's
// fault rather than the code under test's.
func mockScriptArtifactOf(request providers.TextRequest, scriptStage string) (string, string) {
	state := mockWorkflowStateOf(request)
	switch scriptStage {
	case "story_skeleton":
		return "story_skeleton_version", fieldOnLine(state, "skeleton_version=")
	case "adaptation_strategy":
		return "adaptation_strategy_version", fieldOnLine(state, "strategy_version=")
	default:
		return "script_version", fieldOnLine(state, "script_version=")
	}
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
	// THE FINDING NAMES A ROW when the state names one, and that is the difference between
	// a review a user can act on and one they cannot. AC-BOARD-002's criterion is that a
	// supervisor LOCATES the shot at fault and that a FIX revises that shot alone: a finding
	// with no entity id is a statement about the board, and a FIX run against it has nothing
	// to point the revision at.
	//
	// It also carries EVIDENCE pointing at what conflicts, because the criterion asks for
	// two versions. The storyboard case points at the row and the plan's continuity rule; a
	// general report points at nothing rather than at an invented reference, which is what
	// the artifact verifier would refuse anyway.
	finding := map[string]any{
		"rule":     "MOCK_DETERMINISTIC_FINDING",
		"severity": "major",
		"problem":  "The deterministic mock reports a finding at a named location.",
		"location": "mock/location",
	}
	if stage == "storyboard_table" {
		// THE FINDING NAMES A ROW when the state names one, and that is what separates a
		// review a user can act on from a statement about the board: AC-BOARD-002's criterion
		// is that a supervisor LOCATES the shot at fault and that a FIX revises that shot
		// alone, and a finding with no entity id gives the revision nothing to point at.
		//
		// The EVIDENCE is the schema's array of references rather than a JSON blob: section
		// 7.6 requires evidence to name what the run really loaded, and the runtime checks the
		// references against the tool calls the run made — so this names the ROW as an entity
		// reference and the plan's rule as a rule reference, which are two of the four kinds
		// the schema admits.
		state := mockWorkflowStateOf(request)
		itemID := fieldOnLine(state, "storyboard_item=")
		planVersionID := fieldOnLine(state, "director_plan_version=")
		if itemID != "" {
			finding["entityType"] = "storyboard_item"
			finding["entityId"] = itemID
			finding["field"] = "continuityNotes"
			finding["problem"] = "The deterministic mock reports a continuity finding at this row."
			// `location` is DROPPED when an entity pair is present: the domain's
			// ReviewIssue.Validate requires one or the other, and stating both would be two
			// answers to "where is this".
			delete(finding, "location")
			evidence := []any{map[string]any{"type": "entity_ref", "ref": itemID}}
			if planVersionID != "" {
				// The second reference is what the criterion means by "evidence 指向两个版本":
				// the row and the plan whose continuity rule it breaks.
				evidence = append(evidence, map[string]any{"type": "entity_ref", "ref": planVersionID})
			}
			finding["evidence"] = evidence
		}
	}
	document["issues"] = []any{finding}
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

// mockSizedStructureArguments writes a structure with at least the shots the caller asked for.
//
// The shots are spread over as many scenes as it takes to keep a scene's shot count plausible, with
// four per scene — the same shape a boarded episode has, and the shape the board stage then boards
// one row per shot. Every scene carries a slugline, a summary and a duration, because the pipeline's
// validator checks the structure's completeness rather than only counting its rows: a document with
// scenes and no dialogue would be refused for a different reason than the one this branch exists to
// satisfy.
func mockSizedStructureArguments(state string, wanted int, episode, version string) json.RawMessage {
	const shotsPerScene = 4
	scenes := make([]any, 0, (wanted+shotsPerScene-1)/shotsPerScene)
	written := 0
	for sceneIndex := 1; written < wanted; sceneIndex++ {
		shots := make([]any, 0, shotsPerScene)
		for shotIndex := 0; shotIndex < shotsPerScene && written < wanted; shotIndex++ {
			written++
			shots = append(shots, map[string]any{
				"shotNumber":               mockShotNumber(sceneIndex, shotIndex),
				"shotSize":                 "MS",
				"cameraAngle":              "eye level",
				"cameraMovement":           "static",
				"visualDescription":        "The mock script's shot " + mockSmallInt(written) + ".",
				"actionDescription":        "The mock script's action for shot " + mockSmallInt(written) + ".",
				"audioIntent":              "The mock script's audio for shot " + mockSmallInt(written) + ".",
				"estimatedDurationSeconds": 5,
			})
		}
		dialogue := []any{
			map[string]any{"type": "dialogue", "text": "第" + mockSmallInt(sceneIndex) + "场的台词。"},
		}
		scenes = append(scenes, map[string]any{
			"sceneNumber":              mockSmallInt(sceneIndex),
			"slugline":                 "INT. 渡口 " + mockSmallInt(sceneIndex) + " - 日",
			"interiorExterior":         "INT",
			"timeOfDay":                "日",
			"summary":                  "第" + mockSmallInt(sceneIndex) + "场。",
			"dramaticGoal":             "推进第" + mockSmallInt(sceneIndex) + "场的目标。",
			"estimatedDurationSeconds": 30,
			"isOriginalAdaptation":     false,
			"dialogueLines":            dialogue,
			"shots":                    shots,
		})
	}
	arguments := map[string]any{
		"summary": "The deterministic mock's script, sized to the caller's shot count.",
		"scenes":  scenes,
	}
	if version != "" {
		arguments["versionId"] = version
	}
	if episode != "" {
		arguments["episodeId"] = episode
	}
	_ = state
	return mockArgumentJSON(arguments)
}

// parseIntField reads a small non-negative integer from a state field, or zero.
//
// Zero is the honest answer for "absent" here because every caller of this reader treats it as "no
// constraint": a malformed field must not be read as a request for zero shots, which would produce
// an empty document rather than the fixed one.
func parseIntField(value string) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}
	total := 0
	for _, digit := range trimmed {
		if digit < '0' || digit > '9' {
			return 0
		}
		total = total*10 + int(digit-'0')
		if total > 10000 {
			return 0
		}
	}
	return total
}

// mockShotNumber renders a shot's number the way a script writes one: the scene's number followed
// by a letter, so scene 3's second shot is "3B".
func mockShotNumber(scene, shot int) string {
	const alphabet = "ABCDEFGH"
	if shot < 0 || shot >= len(alphabet) {
		return mockSmallInt(scene)
	}
	return mockSmallInt(scene) + string(alphabet[shot])
}
