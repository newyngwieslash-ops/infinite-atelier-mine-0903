import io

p = 'internal/desktop/script_binding.go'
s = io.open(p, encoding='utf-8').read()

# 1. The request gains the production fields.
old = '''	// The version identifiers the stage's state layer carries, so its tools can name what they write.
	SkeletonVersionID string   `json:"skeletonVersionId,omitempty"`
	StrategyVersionID string   `json:"strategyVersionId,omitempty"`
	ScriptVersionID   string   `json:"scriptVersionId,omitempty"`
	SelectedEventIDs  []string `json:"selectedEventIds,omitempty"`'''
new = '''	// The version identifiers the stage's state layer carries, so its tools can name what they write.
	//
	// The first four are the SCRIPT stages' and the rest are the production stages'. They travel
	// on one request because the two layers share this command — WP-09 extracted the mechanism so
	// one binding serves both — and which fields matter is decided by the STAGE, not by the
	// caller: the pipeline that drives the stage is what reads the ones it needs.
	SkeletonVersionID string   `json:"skeletonVersionId,omitempty"`
	StrategyVersionID string   `json:"strategyVersionId,omitempty"`
	ScriptVersionID   string   `json:"scriptVersionId,omitempty"`
	SelectedEventIDs  []string `json:"selectedEventIds,omitempty"`
	// DirectorPlanVersionID and StoryboardVersionID name the upstream artifacts a production
	// stage writes from.
	DirectorPlanVersionID string `json:"directorPlanVersionId,omitempty"`
	StoryboardVersionID   string `json:"storyboardVersionId,omitempty"`
	// StoryboardID is the episode's board identity, for the stage that writes a version of it.
	StoryboardID string `json:"storyboardId,omitempty"`
	// StoryboardItemID is the row a panel stage writes for.
	StoryboardItemID string `json:"storyboardItemId,omitempty"`
	// AssetGapReportID names the analysis a generation plan works from.
	AssetGapReportID string `json:"assetGapReportId,omitempty"`
	// ShotIDs are the shots a board stage must board, in the script's order.
	//
	// THEY ARE LOAD-BEARING rather than a convenience: the board write tool checks every row's
	// citation against the script's own shots, so a stage whose state named none would have every
	// row refused — and the refusal would name a shot problem rather than a missing field.
	ShotIDs []string `json:"shotIds,omitempty"`'''
assert old in s, "request anchor"
s = s.replace(old, new, 1)

# 2. The state is built by the pipeline that drives the stage.
old_state = '''		ModelID:           request.ModelID,
		ProviderID:        request.ProviderID,
		// The upstream versions and the event selection travel in the PROMPT's state layer,
		// which is what the stage's tools name: a strategy reads the skeleton it adapts, and a
		// generation stage reads both. Omitting them is not a harmless default — the state
		// renderer omits an empty field, so the model would be told nothing about what it is
		// writing from.
		State: appscriptpipeline.StateFields{
			SkeletonVersionID: request.SkeletonVersionID,
			StrategyVersionID: request.StrategyVersionID,
			ScriptVersionID:   request.ScriptVersionID,
			SelectedEventIDs:  request.SelectedEventIDs,
		},
	})'''
new_state = '''		ModelID:           request.ModelID,
		ProviderID:        request.ProviderID,
		// THE STATE IS THE LAYER'S, and it is built by the pipeline that drives the stage rather
		// than here. The two layers carry different fields — a script stage names a skeleton and
		// a strategy, a production stage names a plan and a board — and a binding that built one
		// layer's struct would leave the other layer's stage with NO state at all, which is what
		// the first version of this code did: a `storyboard_table` run from the UI had no
		// `shot_ids`, so every row it wrote was refused for citing a shot that "does not belong
		// to this script".
		//
		// The identifier fields are passed as a plain map because that is what crosses a layer
		// boundary; each pipeline converts the ones its layer reads.
		State: stageStateFor(request),
	})'''
assert old_state in s, "state anchor"
s = s.replace(old_state, new_state, 1)

# 3. The helper that builds both layers' state.
anchor = '''// RunScriptSupervisionRequest asks for a review of one attempt.'''
helper = '''// stageStateFor builds the prompt's state for whichever layer drives a stage.
//
// It returns ONE of the two layers' structs, chosen by the stage, and the mechanism passes it
// through opaquely to the layer that reads it. A single struct holding both layers' fields
// would make each layer's renderer responsible for ignoring the other's — and the first field
// a renderer forgot to ignore would be an identifier a tool then wrote about.
func stageStateFor(request RunScriptStageRequest) any {
	if appproductionpipeline.IsProductionStage(request.Stage) {
		return appproductionpipeline.StateFields{
			EpisodeID:             request.EpisodeID,
			ScriptVersionID:       request.ScriptVersionID,
			DirectorPlanVersionID: request.DirectorPlanVersionID,
			StoryboardID:          request.StoryboardID,
			StoryboardVersionID:   request.StoryboardVersionID,
			StoryboardItemID:      request.StoryboardItemID,
			AssetGapReportID:      request.AssetGapReportID,
			ShotIDs:               request.ShotIDs,
		}
	}
	return appscriptpipeline.StateFields{
		SkeletonVersionID: request.SkeletonVersionID,
		StrategyVersionID: request.StrategyVersionID,
		ScriptVersionID:   request.ScriptVersionID,
		SelectedEventIDs:  request.SelectedEventIDs,
	}
}

// RunScriptSupervisionRequest asks for a review of one attempt.'''
assert anchor in s, "helper anchor"
s = s.replace(anchor, helper, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("state routed by stage")
