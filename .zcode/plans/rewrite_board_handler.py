"""Rewrite bindCreateStoryboardVersion so its rows are written.

The several earlier attempts to patch this function with string replacement all failed
to apply — the anchors did not match and the failures were silent. This does it by LINE
RANGE, which cannot silently no-op: the boundaries are asserted before anything is
written.
"""
import io

PATH = 'internal/application/agenttools/handlers.go'
START = 'func bindCreateStoryboardVersion(deps Deps) agentruntime.ToolHandler {'
END = 'func bindCreatePanelVersion(deps Deps) agentruntime.ToolHandler {'

BODY = '''func bindCreateStoryboardVersion(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			EpisodeID             string `json:"episodeId"`
			ScriptVersionID       string `json:"scriptVersionId"`
			DirectorPlanVersionID string `json:"directorPlanVersionId"`
			BasedOnVersionID      string `json:"basedOnVersionId"`
			ChangeReason          string `json:"changeReason"`
			// Items are the board's rows, one per shot. WP-09 added them, because FR-070
			// makes every one of these fields a MUST per shot and the tool previously wrote
			// only the version row — so a model asked to board a script produced a VERSION
			// with no rows and the board was empty.
			Items []struct {
				ShotID                 string `json:"shotId"`
				ShotSize               string `json:"shotSize"`
				CameraAngle            string `json:"cameraAngle"`
				CameraMovement         string `json:"cameraMovement"`
				DurationSeconds        int    `json:"durationSeconds"`
				VisualDescription      string `json:"visualDescription"`
				ActionDescription      string `json:"actionDescription"`
				DialogueAudioSummary   string `json:"dialogueAudioSummary"`
				ContinuityNotes        string `json:"continuityNotes"`
				FirstFrameDescription  string `json:"firstFrameDescription"`
				LastFrameDescription   string `json:"lastFrameDescription"`
				VideoMotionDescription string `json:"videoMotionDescription"`
				AssetRefs              []struct {
					AssetID   string `json:"assetId"`
					UsageRole string `json:"usageRole"`
				} `json:"assetRefs"`
			} `json:"items"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		episodeID, err := required(arguments.EpisodeID, "episode")
		if err != nil {
			return nil, err
		}
		// Section 9.3 makes the script and the plan FIELDS of a storyboard version
		// rather than provenance: a board that names neither is a board of nothing.
		scriptVersionID, err := required(arguments.ScriptVersionID, "script version")
		if err != nil {
			return nil, err
		}
		planVersionID, err := required(arguments.DirectorPlanVersionID, "director plan version")
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, episodeID); err != nil {
			return nil, err
		}
		// Both upstream versions must belong to this episode, checked before the write
		// for the same reason the script stage checks its own: a citation pointing at
		// another episode's artifact looks valid and is not.
		scriptVersion, err := deps.Script.GetScriptVersion(ctx, scriptVersionID)
		if err != nil {
			return nil, err
		}
		script, err := deps.Script.GetScript(ctx, scriptVersion.ScriptID)
		if err != nil {
			return nil, err
		}
		if script.EpisodeID != episodeID {
			return nil, agent.InvalidError("The script version belongs to a different episode.")
		}
		plan, err := deps.Storyboard.GetDirectorPlanVersion(ctx, planVersionID)
		if err != nil {
			return nil, err
		}
		if plan.EpisodeID != episodeID {
			return nil, agent.InvalidError("The director plan version belongs to a different episode.")
		}
		// The shots the rows must cite are read BEFORE the version is written, so a row
		// naming a shot that does not belong to this script is refused while the write is
		// still one call — `storyboard_items.shot_id` has NO foreign key (section 9.4
		// normalises the reference away), so the database would accept an invented one.
		shotsInScript := map[string]bool{}
		if len(arguments.Items) > 0 {
			structure, err := deps.Script.GetScriptStructure(ctx, scriptVersion.ID)
			if err != nil {
				return nil, err
			}
			for _, scene := range structure.Scenes {
				for _, shot := range scene.Shots {
					shotsInScript[shot.ID] = true
				}
			}
		}
		// The ordinals come from the ARRAY'S ORDER, and a repeated shot is refused here
		// rather than by the schema: `UNIQUE (storyboard_version_id, shot_id)` would
		// reject it, but the refusal would name a constraint rather than the row.
		seenShots := map[string]bool{}
		for index, item := range arguments.Items {
			shotID := strings.TrimSpace(item.ShotID)
			if shotID == "" {
				return nil, agent.InvalidError("Every storyboard row must name the shot it boards.")
			}
			if seenShots[shotID] {
				return nil, agent.InvalidError("A shot appears twice in this board, so its duration and position would be ambiguous.")
			}
			seenShots[shotID] = true
			if !shotsInScript[shotID] {
				return nil, agent.InvalidError("Row " + itoaSmall(index+1) + " names a shot that does not belong to this script version.")
			}
			if item.DurationSeconds < 0 {
				return nil, agent.InvalidError("A storyboard row cannot have a negative duration.")
			}
		}
		board, err := deps.Storyboard.EnsureStoryboard(ctx, episodeID)
		if err != nil {
			return nil, err
		}
		createdBy, createdByID := agentActor(request)
		version, err := deps.Storyboard.CreateStoryboardVersion(ctx, appstoryboard.CreateStoryboardVersionRequest{
			StoryboardID:          board.ID,
			ScriptVersionID:       scriptVersion.ID,
			DirectorPlanVersionID: plan.ID,
			BasedOnVersionID:      strings.TrimSpace(arguments.BasedOnVersionID),
			SourceAgentRunID:      request.AgentRunID,
			CreatedByType:         createdBy,
			CreatedByID:           createdByID,
			ChangeReason:          arguments.ChangeReason,
		})
		if err != nil {
			return nil, err
		}
		itemIDs := make([]string, 0, len(arguments.Items))
		for index, item := range arguments.Items {
			row, err := deps.Storyboard.CreateStoryboardItem(ctx, appstoryboard.CreateStoryboardItemRequest{
				StoryboardVersionID:  version.ID,
				ShotID:               strings.TrimSpace(item.ShotID),
				Ordinal:              index + 1,
				ShotSize:             item.ShotSize,
				CameraAngle:          item.CameraAngle,
				CameraMovement:       item.CameraMovement,
				DurationSeconds:      item.DurationSeconds,
				VisualDescription:    item.VisualDescription,
				ActionDescription:    item.ActionDescription,
				DialogueAudioSummary: item.DialogueAudioSummary,
				ContinuityNotes:      item.ContinuityNotes,
				// FR-070's three, added by migration 000018. They are the SHOOTING
				// decisions a video model is given, which is why they are the item's
				// rather than the script shot's.
				FirstFrameDescription:  item.FirstFrameDescription,
				LastFrameDescription:   item.LastFrameDescription,
				VideoMotionDescription: item.VideoMotionDescription,
			})
			if err != nil {
				return nil, err
			}
			itemIDs = append(itemIDs, row.ID)
			// The asset references are recorded as USAGES rather than as a column, because
			// section 7.8 normalises them: "Shot 与资产引用通过 AssetUsage/ShotAssetReference
			// 正规化". A row naming an asset that has no approved version is refused by the
			// resolution below rather than stored as a dangling reference.
			for _, ref := range item.AssetRefs {
				if err := recordShotAssetUsage(ctx, deps, request.ProjectID, row.ID, ref.AssetID, ref.UsageRole); err != nil {
					return nil, err
				}
			}
		}
		return artifactResultWithItems("storyboard_table", "storyboard_version", version.ID,
			version.VersionNumber, string(version.Status), "storyboard", itemIDs), nil
	}
}

'''

s = io.open(PATH, encoding='utf-8').read()
start = s.index(START)
end = s.index(END)
assert start < end, "boundaries are inverted"
before, after = s[:start], s[end:]
assert 'artifactResult("storyboard_table"' in s[start:end], "the old body is not what this expects"
io.open(PATH, 'w', encoding='utf-8').write(before + BODY + after)
print("rewrote bindCreateStoryboardVersion: %d bytes -> %d" % (end - start, len(BODY)))
