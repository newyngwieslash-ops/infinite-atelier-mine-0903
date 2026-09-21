package providers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
)

// mock_script_stages_test.go covers the three script execution stages' mock branches.
//
// The branches exist because the GENERIC tool-call reply asks for the first write tool in an agent's
// contract, and for `script_generation` that is `script.create_script_version` — which writes an
// empty version ROW. A canary driving the script stage through the generic scenario would then review
// a script with no scenes, and every claim about the artifact being complete would be asserted about
// a version whose content was never written. So the stage decides the shape, and these are the tests
// that keep that true.

// scriptStageRequest builds the prompt the runtime emits for one script execution stage.
//
// The layers are the ones the runtime actually renders, in the order it renders them: the policy, the
// skill (whose TITLE names the agent, which is what the mock reads), the tool contract, then the
// workflow state. A request built any other way would be testing a prompt shape the runtime does not
// produce.
func scriptStageRequest(stage, state string) providers.TextRequest {
	return providers.TextRequest{
		ProviderID: "mock-text-1",
		Model:      "mock-model",
		Messages: []providers.TextMessage{
			{Role: "system", Content: "You are one agent in a three-layer system."},
			{Role: "system", Content: "You do the one narrow step you were given, for the stage named in your request."},
			{Role: "system", Content: "# script/script.execution." + stage + "\n\n# Role\n\nDo the " + stage + " step."},
			{Role: "system", Content: "Tools you may call, with the schema each argument must satisfy:\n" +
				"- script.read_story_skeleton (read) schema schemas/agent/x.json\n" +
				"- script.create_story_skeleton_version (write) schema schemas/agent/y.json\n" +
				"- script.create_script_version (write) schema schemas/agent/z.json\n" +
				"- script.create_script_structure (write) schema schemas/agent/w.json\n"},
			{Role: "user", Content: "Current workflow state:\n" + state},
		},
	}
}

// TestMockAsksForTheArtifactEachScriptStageOwes is the central assertion.
//
// Each stage owns ONE artifact, and the key it asks for is the tool that produces it. The generation
// case is the one that matters: the contract lists `script.create_script_version` first among the
// writes, so a branch that fell through to "the first write tool" would ask for an empty version and
// every assertion about the script's content would be about nothing.
func TestMockAsksForTheArtifactEachScriptStageOwes(t *testing.T) {
	cases := []struct {
		stage string
		want  string
	}{
		{"story_skeleton", "script.create_story_skeleton_version"},
		{"adaptation_strategy", "script.create_adaptation_strategy_version"},
		{"script_generation", "script.create_script_structure"},
	}
	for _, testCase := range cases {
		adapter := NewMockTextAdapter().SetScenario(MockScenarioToolCall)
		result, err := adapter.Generate(context.Background(), scriptStageRequest(testCase.stage,
			"workflow_run=run-1 stage="+testCase.stage+" stage_run=stage-9 attempt=1 "+
				"episode=episode-1 selected_events=event-1,event-2 "+
				"script_version=version-1 skeleton_version=skeleton-1 strategy_version=strategy-1"))
		if err != nil {
			t.Fatalf("%s: Generate: %v", testCase.stage, err)
		}
		if len(result.ToolCalls) != 1 {
			t.Fatalf("%s: the reply carries %d tool calls", testCase.stage, len(result.ToolCalls))
		}
		if result.ToolCalls[0].Key != testCase.want {
			t.Fatalf("%s: the mock asked for %q, want %q", testCase.stage, result.ToolCalls[0].Key, testCase.want)
		}
	}
}

// TestMockScriptArgumentsNameWhatTheStateCarries covers the identifiers a write needs.
//
// The identifiers come from the PROMPT — the workflow-state layer the pipeline rendered from the
// database — so the call writes about the rows the run is about rather than inventing ids the
// artifact verifier would refuse. An argument the state cannot supply is absent rather than fabricated,
// which is the honest outcome and is asserted here.
func TestMockScriptArgumentsNameWhatTheStateCarries(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioToolCall)

	// The skeleton: the episode and the selection.
	skeleton, err := adapter.Generate(context.Background(), scriptStageRequest("story_skeleton",
		"workflow_run=run-1 stage=story_skeleton stage_run=stage-9 attempt=1 "+
			"episode=episode-1 selected_events=event-1,event-2"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	skeletonArgs := decodeArguments(t, skeleton.ToolCalls[0].Arguments)
	if skeletonArgs["episodeId"] != "episode-1" {
		t.Fatalf("the skeleton call names episode %v", skeletonArgs["episodeId"])
	}
	// The selection is a LIST, split on the comma: one field carries it, because `fieldOnLine` stops
	// at a space.
	selected, ok := skeletonArgs["selectedEventIds"].([]any)
	if !ok || len(selected) != 2 || selected[0] != "event-1" || selected[1] != "event-2" {
		t.Fatalf("the skeleton selection is %v", skeletonArgs["selectedEventIds"])
	}
	// Every field the schema's own vocabulary constrains is present, so the service's validators have
	// something to check rather than a set of empty strings.
	for _, field := range []string{"openingHook", "coreConflict", "turningPointsJson", "climax", "endingHook"} {
		if value, _ := skeletonArgs[field].(string); strings.TrimSpace(value) == "" {
			t.Fatalf("the skeleton call leaves %s empty", field)
		}
	}

	// The strategy: one treatment per selected event, each STATED.
	strategy, err := adapter.Generate(context.Background(), scriptStageRequest("adaptation_strategy",
		"workflow_run=run-1 stage=adaptation_strategy stage_run=stage-9 attempt=1 "+
			"episode=episode-1 selected_events=event-1,event-2"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	strategyArgs := decodeArguments(t, strategy.ToolCalls[0].Arguments)
	links, ok := strategyArgs["eventLinks"].([]any)
	if !ok || len(links) != 2 {
		t.Fatalf("the strategy treatments are %v", strategyArgs["eventLinks"])
	}
	for index, entry := range links {
		link, _ := entry.(map[string]any)
		if link["storyEventId"] == nil || strings.TrimSpace(link["storyEventId"].(string)) == "" {
			t.Fatalf("treatment %d names no event: %v", index, link)
		}
		// A treatment is STATED rather than omitted, because the service refuses an event with no
		// decision: "retained" and "removed" are opposite and neither follows from an absence.
		if link["treatment"] != "retained" {
			t.Fatalf("treatment %d is %v", index, link["treatment"])
		}
	}

	// The script: the version the pipeline created for this run.
	script, err := adapter.Generate(context.Background(), scriptStageRequest("script_generation",
		"workflow_run=run-1 stage=script_generation stage_run=stage-9 attempt=1 "+
			"episode=episode-1 script_version=version-1"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	scriptArgs := decodeArguments(t, script.ToolCalls[0].Arguments)
	if scriptArgs["versionId"] != "version-1" {
		t.Fatalf("the structure call names version %v", scriptArgs["versionId"])
	}
	if scriptArgs["episodeId"] != "episode-1" {
		t.Fatalf("the structure call names episode %v", scriptArgs["episodeId"])
	}
}

// TestMockStructureArgumentsStateNoIdentifierOrOrdinal covers §17 at the mock boundary.
//
// The schema has no field for an identifier, an ordinal or a duration, so the mock cannot state one —
// and this asserts the payload it produces carries none, which is what makes the canary's duration
// assertion a statement about the SERVICE's summing rather than about the mock's arithmetic.
func TestMockStructureArgumentsStateNoIdentifierOrOrdinal(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioToolCall)
	result, err := adapter.Generate(context.Background(), scriptStageRequest("script_generation",
		"workflow_run=run-1 stage=script_generation stage_run=stage-9 attempt=1 "+
			"episode=episode-1 script_version=version-1"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	arguments := decodeArguments(t, result.ToolCalls[0].Arguments)
	scenes, ok := arguments["scenes"].([]any)
	if !ok || len(scenes) == 0 {
		t.Fatalf("the structure call states %v", arguments["scenes"])
	}
	// The version's own duration is never stated: it is summed from the scenes, and a field for it
	// would be a second statement of the same fact.
	if _, present := arguments["estimatedDurationSeconds"]; present {
		t.Fatal("the structure call states the version's duration")
	}
	for index, entry := range scenes {
		scene, _ := entry.(map[string]any)
		for _, forbidden := range []string{"sceneId", "id", "ordinal", "scriptVersionId"} {
			if _, present := scene[forbidden]; present {
				t.Fatalf("scene %d states %s, which is the code's to assign", index, forbidden)
			}
		}
		// The scene's OWN estimate IS stated, because the sum needs it — that is the one duration a
		// model supplies, and §7.6 puts it on the scene.
		if _, present := scene["estimatedDurationSeconds"]; !present {
			t.Fatalf("scene %d states no duration, so the version's total would sum to zero", index)
		}
		lines, _ := scene["dialogueLines"].([]any)
		for lineIndex, lineEntry := range lines {
			line, _ := lineEntry.(map[string]any)
			for _, forbidden := range []string{"lineId", "id", "ordinal", "sceneId"} {
				if _, present := line[forbidden]; present {
					t.Fatalf("scene %d line %d states %s", index, lineIndex, forbidden)
				}
			}
		}
	}
	// The canary's assertion needs a KNOWN total, so the two scene durations are asserted here: if
	// they change, the canary's expectation has to change with them, and a failure here is a clearer
	// signal than a duration mismatch three layers away.
	total := 0.0
	for _, entry := range scenes {
		scene, _ := entry.(map[string]any)
		if seconds, ok := scene["estimatedDurationSeconds"].(float64); ok {
			total += seconds
		}
	}
	if total != 150 {
		t.Fatalf("the mock's scenes sum to %v, and the canary asserts 150", total)
	}
	// The second scene is an ORIGINAL, which is what lets the canary tell an invention from a faithful
	// adaptation (AC-SCRIPT-003's 原创改编标记).
	second, _ := scenes[1].(map[string]any)
	if second["isOriginalAdaptation"] != true {
		t.Fatalf("the second scene is not marked original: %v", second["isOriginalAdaptation"])
	}
}

// TestMockScriptResultReportsTheArtifactItWrote covers the success rule's other half.
//
// The tool call is what WRITES, and the document is what the runtime reads to decide whether the stage
// succeeded: §7.4's "success 至少有预期 artifact" is a rule about the DOCUMENT. So a script stage's
// reply says `complete` and names the row, and the identifier comes from the state layer — an
// invention here would fail the runtime's verifier for a reason that is this file's fault.
func TestMockScriptResultReportsTheArtifactItWrote(t *testing.T) {
	cases := []struct {
		stage        string
		state        string
		wantEntityID string
		wantType     string
	}{
		{
			"story_skeleton",
			"workflow_run=run-1 stage=story_skeleton stage_run=stage-9 episode=episode-1 skeleton_version=skeleton-7",
			"skeleton-7", "story_skeleton_version",
		},
		{
			"adaptation_strategy",
			"workflow_run=run-1 stage=adaptation_strategy stage_run=stage-9 episode=episode-1 strategy_version=strategy-7",
			"strategy-7", "adaptation_strategy_version",
		},
		{
			"script_generation",
			"workflow_run=run-1 stage=script_generation stage_run=stage-9 episode=episode-1 script_version=version-7",
			"version-7", "script_version",
		},
	}
	for _, testCase := range cases {
		adapter := NewMockTextAdapter().SetScenario(MockScenarioNormal)
		result, err := adapter.Generate(context.Background(), scriptStageRequest(testCase.stage, testCase.state))
		if err != nil {
			t.Fatalf("%s: Generate: %v", testCase.stage, err)
		}
		document := decodeArguments(t, []byte(result.Content))
		if document["status"] != "complete" {
			t.Fatalf("%s: the stage reports status %v", testCase.stage, document["status"])
		}
		artifacts, ok := document["artifacts"].([]any)
		if !ok || len(artifacts) != 1 {
			t.Fatalf("%s: the reply names %v", testCase.stage, document["artifacts"])
		}
		artifact, _ := artifacts[0].(map[string]any)
		if artifact["entityId"] != testCase.wantEntityID {
			t.Fatalf("%s: the artifact is %v, want %s", testCase.stage, artifact, testCase.wantEntityID)
		}
		if artifact["entityType"] != testCase.wantType {
			t.Fatalf("%s: the artifact type is %v", testCase.stage, artifact["entityType"])
		}
		// The stage names itself, which the runtime cross-checks against the invocation.
		if document["stage"] != testCase.stage {
			t.Fatalf("%s: the reply names stage %v", testCase.stage, document["stage"])
		}
	}
}

// TestMockScriptResultOmitsAnArtifactTheStateCannotName covers the honest failure.
//
// An identifier the state did not carry is left OUT rather than invented: the runtime's verifier reads
// these references back, so a fabricated id would fail it for a reason that is the mock's fault — and
// the failure would look like a defect in the code under test.
func TestMockScriptResultOmitsAnArtifactTheStateCannotName(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioNormal)
	result, err := adapter.Generate(context.Background(), scriptStageRequest("script_generation",
		"workflow_run=run-1 stage=script_generation stage_run=stage-9 episode=episode-1"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	document := decodeArguments(t, []byte(result.Content))
	if _, present := document["artifacts"]; present {
		t.Fatalf("the reply invented an artifact: %v", document["artifacts"])
	}
	// And the status is PARTIAL rather than complete, which is the rule a canary taught this file:
	// §7.4's "success 至少有预期 artifact" is enforced by the runtime on TOP of the schema, so a
	// `complete` with no reference is refused by the runner. Saying partial is the honest reading —
	// the stage did its work and this mock cannot name the row it produced.
	if document["status"] != "partial" {
		t.Fatalf("the stage reports status %v, want partial when no version can be named",
			document["status"])
	}
}

// TestANonScriptExecutionStageIsUnchanged states the boundary of the new branches.
//
// The extraction and production stages have their own replies, and a branch that matched too broadly
// would change them. This asserts the two that existed still behave as they did: no artifact, and the
// partial that needs a person.
func TestANonScriptExecutionStageIsUnchanged(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioNormal)
	request := scriptStageRequest("event_extraction", "workflow_run=run-1 stage=event_extraction stage_run=stage-9")
	result, err := adapter.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(result.Content, `"artifacts"`) {
		t.Fatalf("the extraction reply carries an artifact list: %s", result.Content)
	}
	// And a stage whose name this file does not know is answered by the generic branch, which reports
	// a partial rather than claiming an artifact nobody wrote.
	unknown := scriptStageRequest("something_else", "workflow_run=run-1 stage=something_else stage_run=stage-9")
	result, err = adapter.Generate(context.Background(), unknown)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	document := decodeArguments(t, []byte(result.Content))
	if document["status"] != "partial" {
		t.Fatalf("an unknown stage reports status %v", document["status"])
	}
}

// TestAQuotedStageTitleDoesNotChangeWhichStageThisIs covers the matcher's one real ambiguity.
//
// The stage is read from the skill layer's TITLE, and a skill document may legitimately QUOTE another
// agent's title — a generation skill that says "the skeleton stage is script.execution.story_skeleton"
// is describing the pipeline, not claiming to be that agent. A matcher satisfied by a substring would
// read the quoted name and answer as the wrong stage, which is the failure this test exists for.
//
// The prompt below is the shape a real skill produces: the title on its FIRST line, and a later line
// that mentions another stage's name mid-sentence.
func TestAQuotedStageTitleDoesNotChangeWhichStageThisIs(t *testing.T) {
	request := providers.TextRequest{
		ProviderID: "mock-text-1",
		Model:      "mock-model",
		Messages: []providers.TextMessage{
			{Role: "system", Content: "You are one agent in a three-layer system."},
			{Role: "system", Content: "You do the one narrow step you were given, for the stage named in your request."},
			{Role: "system", Content: "# script/script.execution.script_generation\n\n" +
				"# Role\n\nWrite the script. The skeleton stage is script.execution.story_skeleton,\n" +
				"and the strategy stage is script.execution.adaptation_strategy; read both before writing.\n"},
			{Role: "system", Content: "Tools you may call, with the schema each argument must satisfy:\n" +
				"- script.create_script_version (write) schema schemas/agent/z.json\n" +
				"- script.create_script_structure (write) schema schemas/agent/w.json\n"},
			{Role: "user", Content: "Current workflow state:\nworkflow_run=run-1 stage=script_generation " +
				"stage_run=stage-9 attempt=1 episode=episode-1 script_version=version-1"},
		},
	}
	// TWO scenario, because the two halves are built by two code paths and a quoted title could fool
	// either. The tool-call scenario decides which TOOL is asked for; the normal scenario decides
	// which ARTIFACT the result reports. Asserting one and inferring the other is what the first
	// version of this test did, and the artifact assertion failed for a reason that had nothing to do
	// with the matcher: the tool-call scenario's execution document reports no artifact at all.
	toolCall := NewMockTextAdapter().SetScenario(MockScenarioToolCall)
	result, err := toolCall.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("the reply carries %d tool calls", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Key != "script.create_script_structure" {
		t.Fatalf("a skill quoting another stage's title changed which stage this is: %q",
			result.ToolCalls[0].Key)
	}

	normal := NewMockTextAdapter().SetScenario(MockScenarioNormal)
	result, err = normal.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	document := decodeArguments(t, []byte(result.Content))
	if document["stage"] != "script_generation" {
		t.Fatalf("the reply names stage %v", document["stage"])
	}
	artifacts, ok := document["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("the reply names %v", document["artifacts"])
	}
	artifact, _ := artifacts[0].(map[string]any)
	if artifact["entityType"] != "script_version" {
		t.Fatalf("the artifact type is %v, want script_version", artifact["entityType"])
	}
}

// decodeArguments parses a JSON object into a map, failing the test when it will not parse.
func decodeArguments(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("the document is not JSON: %v\n%s", err, raw)
	}
	return document
}
