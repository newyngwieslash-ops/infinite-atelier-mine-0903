package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// mock_production_test.go covers the five production execution stages' mock branches and
// the deterministic image adapter.
//
// The branches exist for the same reason the script ones do: the GENERIC tool-call reply
// asks for the first write tool in an agent's contract, and for the storyboard stage that
// would be the wrong one — the stage owes ROWS, and a version with no rows is an empty
// board that every downstream assertion would be made about. So the stage decides the
// shape, and these tests keep that true.

// productionStageRequest builds the prompt the runtime emits for one production stage.
//
// The layers are the runtime's own order: the policy, the skill (whose TITLE names the
// agent, which is what the mock reads), the tool contract, then the workflow state. A
// request built any other way would test a prompt shape the runtime does not produce.
func productionStageRequest(stage, state string) providers.TextRequest {
	return providers.TextRequest{
		ProviderID: "mock-text-1",
		Model:      "mock-model",
		Messages: []providers.TextMessage{
			{Role: "system", Content: "You are one agent in a three-layer system."},
			{Role: "system", Content: "You do the one narrow step you were given, for the stage named in your request."},
			{Role: "system", Content: "# production/production.execution." + stage + "\n\n# Role\n\nDo the " + stage + " step."},
			{Role: "system", Content: "Tools you may call, with the schema each argument must satisfy:\n" +
				"- storyboard.create_director_plan_version (write) schema schemas/agent/x.json\n" +
				"- asset.create_gap_report (write) schema schemas/agent/y.json\n" +
				"- asset.create_candidate_version (write) schema schemas/agent/z.json\n" +
				"- storyboard.create_storyboard_version (write) schema schemas/agent/w.json\n" +
				"- storyboard.create_storyboard_panel_version (write) schema schemas/agent/v.json\n"},
			{Role: "user", Content: "Current workflow state:\n" + state},
		},
	}
}

// TestMockAsksForTheArtifactEachProductionStageOwes is the central assertion.
//
// Each stage owns ONE artifact, and the key it asks for is the tool that produces it.
// The storyboard case is the one that matters most: its contract lists the plan and the
// panel tools too, so a branch that fell through to "the first write tool" would ask for a
// PLAN while the stage was meant to board shots.
func TestMockAsksForTheArtifactEachProductionStageOwes(t *testing.T) {
	cases := []struct {
		stage string
		want  string
	}{
		{"director_plan", "storyboard.create_director_plan_version"},
		{"asset_analysis", "asset.create_gap_report"},
		{"asset_generation_plan", "asset.create_candidate_version"},
		{"storyboard_table", "storyboard.create_storyboard_version"},
		{"storyboard_panel", "storyboard.create_storyboard_panel_version"},
	}
	for _, testCase := range cases {
		adapter := NewMockTextAdapter().SetScenario(MockScenarioToolCall)
		result, err := adapter.Generate(context.Background(), productionStageRequest(testCase.stage,
			"workflow_run=run-1 stage="+testCase.stage+" stage_run=stage-9 attempt=1 "+
				"episode=episode-1 script_version=script-1 director_plan_version=plan-1 "+
				"storyboard_item=item-1 shot_ids=shot-1,shot-2,shot-3"))
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

// TestTheBoardBranchWritesOneRowPerShotTheStateNamed covers the rows, which are what the
// stage owes.
//
// THE SHOT IDS COME FROM THE STATE, and that is load-bearing rather than tidy: the write
// tool checks every row's citation against the script's own shots, so a mock that invented
// ids would have its call refused and the failure would look like a bug in the mock rather
// than like the guard working.
func TestTheBoardBranchWritesOneRowPerShotTheStateNamed(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioToolCall)
	result, err := adapter.Generate(context.Background(), productionStageRequest("storyboard_table",
		"stage=storyboard_table episode=episode-1 script_version=script-1 director_plan_version=plan-1 "+
			"shot_ids=shot-a,shot-b"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var arguments struct {
		EpisodeID             string `json:"episodeId"`
		ScriptVersionID       string `json:"scriptVersionId"`
		DirectorPlanVersionID string `json:"directorPlanVersionId"`
		Items                 []struct {
			ShotID                 string `json:"shotId"`
			Ordinal                int    `json:"ordinal"`
			FirstFrameDescription  string `json:"firstFrameDescription"`
			LastFrameDescription   string `json:"lastFrameDescription"`
			VideoMotionDescription string `json:"videoMotionDescription"`
		} `json:"items"`
	}
	if err := json.Unmarshal(result.ToolCalls[0].Arguments, &arguments); err != nil {
		t.Fatalf("the mock's arguments do not decode: %v", err)
	}
	if arguments.EpisodeID != "episode-1" || arguments.ScriptVersionID != "script-1" ||
		arguments.DirectorPlanVersionID != "plan-1" {
		t.Fatalf("the board's citations are %+v", arguments)
	}
	if len(arguments.Items) != 2 {
		t.Fatalf("the board has %d rows for two shots", len(arguments.Items))
	}
	if arguments.Items[0].ShotID != "shot-a" || arguments.Items[1].ShotID != "shot-b" {
		t.Fatalf("the rows cite %q and %q", arguments.Items[0].ShotID, arguments.Items[1].ShotID)
	}
	// FR-070's three descriptions are present on every row: they are the fields a video
	// model is given, and a board without them is one the panel stage cannot render.
	for index, item := range arguments.Items {
		if item.FirstFrameDescription == "" || item.LastFrameDescription == "" ||
			item.VideoMotionDescription == "" {
			t.Errorf("row %d is missing FR-070's frame or motion descriptions", index)
		}
	}
	// NO ORDINAL is stated, because §17 makes order the code's job: the array's order IS
	// the order, and a model that supplied its own could leave a hole or repeat one.
	for _, item := range arguments.Items {
		if item.Ordinal != 0 {
			t.Errorf("a row states ordinal %d, which the service assigns", item.Ordinal)
		}
	}
	// A state that named NO shots yields no rows rather than one row citing an empty id:
	// the latter would be refused by the handler for a reason that is this file's fault.
	empty, err := adapter.Generate(context.Background(), productionStageRequest("storyboard_table",
		"stage=storyboard_table episode=episode-1 script_version=script-1 director_plan_version=plan-1"))
	if err != nil {
		t.Fatalf("Generate with no shots: %v", err)
	}
	var noRows struct {
		Items []any `json:"items"`
	}
	if err := json.Unmarshal(empty.ToolCalls[0].Arguments, &noRows); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(noRows.Items) != 0 {
		t.Fatalf("a state naming no shots produced %d rows", len(noRows.Items))
	}
}

// TestTheGapBranchWritesAnApprovableReport covers the one content decision this file makes.
//
// It writes a SATISFIED line, and the choice is not minimal: a report with a MISSING
// required line CANNOT BE APPROVED — `CanApproveGapReport` refuses it — so a mock that
// wrote one would produce an analysis no user could put in force, and every test
// downstream would be testing the refusal rather than the pipeline.
func TestTheGapBranchWritesAnApprovableReport(t *testing.T) {
	adapter := NewMockTextAdapter().SetScenario(MockScenarioToolCall)
	result, err := adapter.Generate(context.Background(), productionStageRequest("asset_analysis",
		"stage=asset_analysis episode=episode-1 script_version=script-1"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var arguments struct {
		EpisodeID       string `json:"episodeId"`
		ScriptVersionID string `json:"scriptVersionId"`
		Items           []struct {
			Status   string `json:"status"`
			AssetID  string `json:"assetId"`
			Required bool   `json:"required"`
		} `json:"items"`
	}
	if err := json.Unmarshal(result.ToolCalls[0].Arguments, &arguments); err != nil {
		t.Fatalf("the mock's arguments do not decode: %v", err)
	}
	if arguments.EpisodeID != "episode-1" || arguments.ScriptVersionID != "script-1" {
		t.Fatalf("the report's citations are %+v", arguments)
	}
	if len(arguments.Items) == 0 {
		t.Fatal("the mock wrote a report with no lines, which cannot be approved")
	}
	for index, item := range arguments.Items {
		if item.Status != "satisfied" {
			t.Errorf("line %d is %q, and only a report with no missing required line can be approved", index, item.Status)
		}
		if item.AssetID == "" {
			t.Errorf("line %d claims to be satisfied and names no asset, which the domain refuses", index)
		}
	}
}

// TestTheMockAnswersASupervisionStageDifferently covers the layer split.
//
// An execution stage writes and a supervisor reports, and the two prefixes are what tell
// them apart. A lookup that matched either would answer a supervisor's request with a
// WRITE — which the agent's contract does not grant, so the ACL would deny the call and the
// failure would name permissions rather than a mock branch.
func TestTheMockAnswersASupervisionStageDifferently(t *testing.T) {
	request := providers.TextRequest{
		ProviderID: "mock-text-1", Model: "mock-model",
		Messages: []providers.TextMessage{
			{Role: "system", Content: "# production/production.supervision.storyboard_table\n\n# Role\n\nReview."},
			{Role: "user", Content: "Current workflow state:\nstage=storyboard_table episode=episode-1"},
		},
	}
	if stage := mockProductionStageOf(request); stage != "" {
		t.Fatalf("a supervisor's request was read as the execution stage %q", stage)
	}
	if stage := mockSupervisionStageOf(request); stage != "storyboard_table" {
		t.Fatalf("the supervisor's stage read as %q", stage)
	}
	// And an execution request is not read as a supervisor's.
	execution := productionStageRequest("director_plan", "stage=director_plan episode=episode-1")
	if stage := mockSupervisionStageOf(execution); stage != "" {
		t.Fatalf("an execution request was read as the supervisor of %q", stage)
	}
}

// TestTheMockImageProducesDecodableDeterministicBytes covers the image adapter.
//
// The bytes are a real PNG rather than a fixed string, and that is the difference between
// a mock that makes a broken pipeline look healthy and one that does not: the job pipeline
// sniffs a result's MIME type and the FileStore hashes and serves what it stored, so a
// payload that were merely "image/png-ish" would pass a length check and fail the first
// real reader.
func TestTheMockImageProducesDecodableDeterministicBytes(t *testing.T) {
	adapter := NewMockImageAdapter()
	outcome, err := adapter.Generate(context.Background(), mockImageRequest("a corridor at dusk", 2))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(outcome.Results) != 2 {
		t.Fatalf("the adapter produced %d images for a count of two", len(outcome.Results))
	}
	for index, result := range outcome.Results {
		payload, err := base64Decode(result.Data)
		if err != nil {
			t.Fatalf("image %d is not base64: %v", index, err)
		}
		if !PNGSignature(payload) {
			t.Fatalf("image %d does not begin with the PNG signature", index)
		}
		width, height, ok := pngDimensions(payload)
		if !ok || width != 32 || height != 32 {
			t.Fatalf("image %d is %dx%d (ok=%v)", index, width, height, ok)
		}
		if result.MIMEType != "image/png" {
			t.Fatalf("image %d reports the MIME type %q", index, result.MIMEType)
		}
	}
	// TWO CANDIDATES OF ONE SHOT MUST DIFFER: a test asserting that a batch produced two
	// candidates would otherwise pass against one image copied twice.
	if outcome.Results[0].Data == outcome.Results[1].Data {
		t.Fatal("two candidates of one prompt produced identical bytes")
	}
	// And the same request reproduces the same bytes, which is what makes a retry
	// assertable.
	repeat, err := NewMockImageAdapter().Generate(context.Background(), mockImageRequest("a corridor at dusk", 2))
	if err != nil {
		t.Fatalf("re-running the mock: %v", err)
	}
	if repeat.Results[0].Data != outcome.Results[0].Data {
		t.Fatal("the same request produced different bytes on a second call")
	}
	if repeat.Results[1].Data != outcome.Results[1].Data {
		t.Fatal("the same request produced different bytes for the second candidate")
	}
}

// TestTheMockImageFailsTheTimesItWasAskedTo covers the retry seam.
//
// "重试失败项" is a test about the runner's CLASSIFICATION, so the mock has to be able to
// fail transiently: a failure that were permanent would exercise the dead-letter path
// instead, and one that did not fail at all would exercise nothing.
func TestTheMockImageFailsTheTimesItWasAskedTo(t *testing.T) {
	adapter := NewMockImageAdapter()
	adapter.FailNext("a corridor at dusk", 1)
	if _, err := adapter.Generate(context.Background(), mockImageRequest("a corridor at dusk", 1)); err == nil {
		t.Fatal("the mock did not fail when it was asked to")
	}
	if _, err := adapter.Generate(context.Background(), mockImageRequest("a corridor at dusk", 1)); err != nil {
		t.Fatalf("the mock failed twice for a single requested failure: %v", err)
	}
	// The classification is the taxonomy's remote-transient, which is the same shape a
	// real 5xx produces — so the runner's retry decision is the one it would make there.
	adapter.FailNext("another shot", 1)
	_, err := adapter.Generate(context.Background(), mockImageRequest("another shot", 1))
	if err == nil {
		t.Fatal("the mock did not fail")
	}
	if !providerRetriable(err) {
		t.Fatalf("the mock's failure is not retriable: %v", err)
	}
}

// TestTheMockImageRefusesAnEmptyPrompt covers the one input the adapter checks.
//
// An empty prompt is a malformed request rather than a provider fault, and the category
// matters: a caller seeing a transient failure would retry a request that can never
// succeed.
func TestTheMockImageRefusesAnEmptyPrompt(t *testing.T) {
	adapter := NewMockImageAdapter()
	if _, err := adapter.Generate(context.Background(), mockImageRequest("   ", 1)); err == nil {
		t.Fatal("an empty prompt was accepted")
	}
}

// mockImageRequest builds an image request for the deterministic adapter.
//
// It is named apart from `imageRequest` in `image_adapters_test.go`, which builds a
// request for the REAL adapters: the two answer different ports' questions, and one name
// for both would hide which adapter a test was exercising.
func mockImageRequest(prompt string, count int) appjobs.ImageRequest {
	return appjobs.ImageRequest{
		JobID: "job-1", ProviderID: "mock-image-1", Model: "mock-model",
		Prompt: prompt, Count: count,
	}
}

// base64Decode decodes a data payload.
func base64Decode(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(value))
}

// providerRetriable reports whether a provider error is one the runner retries.
func providerRetriable(err error) bool {
	providerErr, ok := provider.AsProviderError(err)
	return ok && providerErr.Retriable
}
