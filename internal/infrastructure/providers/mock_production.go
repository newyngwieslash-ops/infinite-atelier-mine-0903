package providers

import (
	"encoding/json"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
)

// mock_production.go is the deterministic mock's PRODUCTION arm: what each of WP-09's
// stages writes, and what it reports having written.
//
// # Why the mock needs this at all
//
// AGENT_CONTRACTS §18.3 forbids CI from calling a paid model, and WP-09's acceptance
// criteria are end-to-end: AC-BOARD-001 wants a board generated from an approved script,
// the director plan and the assets, and AC-BOARD-002 wants a supervisor to locate Shot 6.
// Neither can be exercised without a model, so the mock answers these stages the way it
// already answers the three script ones.
//
// # The shape it follows
//
// The three script stages established the pattern and this file keeps it: a stage's write
// is a TOOL CALL rather than a document, its arguments come from the workflow-state layer
// the pipeline rendered (so a call names the episode and the versions the run is about
// rather than inventing identifiers the artifact verifier would refuse), and the reply's
// artifact reference is what the runtime's §7.4 check reads.

// mockProductionKind is which artifact a production stage's write produces.
//
// It is a small closed set rather than a string passed around, because the write's TOOL
// KEY is derived from it and a typo would produce a call the ACL denies — which is a
// failure that looks like a permission problem rather than a mock bug.
type mockProductionKind int

const (
	mockWritesDirectorPlan mockProductionKind = iota
	mockWritesGapReport
	mockWritesAssetCandidate
	mockWritesStoryboard
	mockWritesPanel
	mockWritesNothing
)

// mockProductionToolCall returns the write one production stage asks for.
func mockProductionToolCall(request providers.TextRequest, stage string) (string, json.RawMessage) {
	switch stage {
	case "director_plan":
		return "storyboard.create_director_plan_version", mockDirectorPlanArguments(request)
	case "asset_analysis":
		return "asset.create_gap_report", mockGapReportArguments(request)
	case "asset_generation_plan":
		return "asset.create_candidate_version", mockAssetCandidateArguments(request)
	case "storyboard_table":
		return "storyboard.create_storyboard_version", mockStoryboardArguments(request)
	case "storyboard_panel":
		return "storyboard.create_storyboard_panel_version", mockPanelArguments(request)
	default:
		// An unknown stage writes nothing, and the reply below says so: a call the mock
		// invented for a stage it does not know would be a write nobody asked for, and
		// the ACL would deny it with a message about permissions rather than about a
		// missing branch.
		return "", nil
	}
}

// mockProductionArtifactOf names the row a production stage's write creates.
//
// It reads the workflow state for the identifier, exactly as `mockScriptArtifactOf`
// does: the state is what the pipeline rendered from the database, so the reference is
// the row the run is actually about. An empty id means the state did not name one, and
// the caller omits the artifact list rather than filling it with an invention.
func mockProductionArtifactOf(request providers.TextRequest, stage string) (string, string) {
	state := mockWorkflowStateOf(request)
	switch stage {
	case "director_plan":
		// The plan version is CREATED by this call rather than named by the state, so
		// the reference is the id the call's result returns — which the runtime reads
		// from the tool call, not from this document. See the note in the reply builder.
		return "director_plan_version", fieldOnLine(state, "director_plan_version=")
	case "asset_analysis":
		return "asset_gap_report", fieldOnLine(state, "asset_gap_report=")
	case "storyboard_table":
		return "storyboard_version", fieldOnLine(state, "storyboard_version=")
	case "storyboard_panel":
		return "storyboard_panel_version", fieldOnLine(state, "storyboard_panel_version=")
	default:
		// `asset_generation_plan` has no single row to report: a candidate existing is
		// what the analysis asked for, and the state names none because the write
		// creates it. An empty id leaves the reply at `partial`, which is the honest
		// reading and the one this file's script arm already takes for the same reason.
		return "", ""
	}
}

// mockDirectorPlanArguments builds the plan write from the state.
func mockDirectorPlanArguments(request providers.TextRequest) json.RawMessage {
	state := mockWorkflowStateOf(request)
	document := map[string]any{
		"episodeId":       fieldOnLine(state, "episode="),
		"scriptVersionId": fieldOnLine(state, "script_version="),
		"visualRhythm":    "The mock plan holds a steady medium rhythm, quickening at each scene's turn.",
		"cameraLanguage":  "The mock plan favours eye-level mediums and reserves a high angle for the reveal.",
		"colorLighting":   "The mock plan keeps a cool key with warm practicals.",
		"staging":         "The mock plan blocks each scene along one axis.",
		"continuityRules": "The mock plan keeps every costume and prop continuous within a scene.",
		"audioDirection":  "The mock plan leaves room for dialogue and marks the montage for score.",
		"changeReason":    "The deterministic mock wrote this plan.",
	}
	return mockArgumentJSON(document)
}

// mockGapReportArguments builds the analysis write from the state.
//
// IT WRITES ONE SATISFIED LINE, and the choice is deliberate rather than minimal: a
// report with a MISSING required line cannot be approved — `CanApproveGapReport` refuses
// it — so a mock that wrote one would produce an analysis no user could ever put in
// force, and every test downstream of it would be testing the refusal rather than the
// pipeline. A mock that wants to exercise the refusal sets it up in its own test.
func mockGapReportArguments(request providers.TextRequest) json.RawMessage {
	state := mockWorkflowStateOf(request)
	document := map[string]any{
		"episodeId":       fieldOnLine(state, "episode="),
		"scriptVersionId": fieldOnLine(state, "script_version="),
		"summary":         "The deterministic mock found one story fact this episode needs.",
		"items": []any{map[string]any{
			"assetType":       "character",
			"storyEntityId":   "mock-entity-1",
			"storyEntityName": "The protagonist",
			"assetId":         "mock-asset-version-1",
			"status":          "satisfied",
			"usageRole":       "reference",
			"required":        true,
		}},
	}
	return mockArgumentJSON(document)
}

// mockAssetCandidateArguments builds an asset candidate write from the state.
func mockAssetCandidateArguments(request providers.TextRequest) json.RawMessage {
	state := mockWorkflowStateOf(request)
	document := map[string]any{
		"assetId": "mock-asset-1",
		"prompt":  "The deterministic mock candidate for the asset this episode needs.",
		// The state's episode travels in the metadata so a reader of the version can see
		// which run proposed it, which is what FR-100's audit reads.
		"metadataJson": `{"episodeId":"` + fieldOnLine(state, "episode=") + `"}`,
	}
	return mockArgumentJSON(document)
}

// mockStoryboardArguments builds the board write from the state.
//
// THE ROWS CITE THE SHOTS THE STATE NAMED, and that is not a convenience: the write tool
// checks every row's citation against the script's own shots, so a mock that invented ids
// would have its call refused — and the refusal would look like a mock bug rather than
// like the guard working. The state carries them as one comma-joined field, which is the
// convention `fieldOnLine`'s space separator requires.
func mockStoryboardArguments(request providers.TextRequest) json.RawMessage {
	state := mockWorkflowStateOf(request)
	// `splitStateList` is the script arm's own reader for a comma-joined field, reused
	// rather than reimplemented: the convention is one thing, and two readers of it would
	// be two places for it to change.
	shotIDs := splitStateList(fieldOnLine(state, "shot_ids="))
	items := make([]any, 0, len(shotIDs))
	for index, shotID := range shotIDs {
		number := mockSmallInt(index + 1)
		items = append(items, map[string]any{
			"shotId":                 shotID,
			"shotSize":               "MS",
			"cameraAngle":            "eye level",
			"cameraMovement":         "static",
			"durationSeconds":        4,
			"visualDescription":      "The mock board's shot " + number + ".",
			"actionDescription":      "The mock board's action for shot " + number + ".",
			"dialogueAudioSummary":   "The mock board's line for shot " + number + ".",
			"continuityNotes":        "The mock board keeps shot " + number + " continuous with its neighbours.",
			"firstFrameDescription":  "Shot " + number + " opens on its subject.",
			"lastFrameDescription":   "Shot " + number + " closes on its subject.",
			"videoMotionDescription": "Shot " + number + " holds steady.",
		})
	}
	document := map[string]any{
		"episodeId":             fieldOnLine(state, "episode="),
		"scriptVersionId":       fieldOnLine(state, "script_version="),
		"directorPlanVersionId": fieldOnLine(state, "director_plan_version="),
		"items":                 items,
	}
	return mockArgumentJSON(document)
}

// mockPanelArguments builds a panel write from the state.
func mockPanelArguments(request providers.TextRequest) json.RawMessage {
	state := mockWorkflowStateOf(request)
	document := map[string]any{
		"itemId":       fieldOnLine(state, "storyboard_item="),
		"prompt":       "The deterministic mock panel for this storyboard row.",
		"changeReason": "The deterministic mock wrote this panel.",
	}
	return mockArgumentJSON(document)
}

// mockSmallInt renders a small positive integer, since this file does not import strconv
// for one call site.
func mockSmallInt(value int) string {
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
