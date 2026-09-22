import io

p = 'internal/application/agenttools/handlers.go'
s = io.open(p, encoding='utf-8').read()

old = '''		var arguments struct {
			EpisodeID             string `json:"episodeId"`
			ScriptVersionID       string `json:"scriptVersionId"`
			DirectorPlanVersionID string `json:"directorPlanVersionId"`
			BasedOnVersionID      string `json:"basedOnVersionId"`
			ChangeReason          string `json:"changeReason"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		episodeID, err := required(arguments.EpisodeID, "episode")'''
new = '''		var arguments struct {
			EpisodeID             string `json:"episodeId"`
			ScriptVersionID       string `json:"scriptVersionId"`
			DirectorPlanVersionID string `json:"directorPlanVersionId"`
			BasedOnVersionID      string `json:"basedOnVersionId"`
			ChangeReason          string `json:"changeReason"`
			// Items are the board's rows, one per shot. WP-09 added them, because FR-070
			// makes every one of these fields a MUST per shot and the tool previously
			// wrote only the version row — so a model asked to board a script produced a
			// VERSION with no rows and the board was empty.
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
		episodeID, err := required(arguments.EpisodeID, "episode")'''
assert old in s, "arguments anchor"
s = s.replace(old, new, 1)

old_write = '''		createdBy, createdByID := agentActor(request)
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
		return artifactResult("storyboard_table", "storyboard_version", version.ID,
			version.VersionNumber, string(version.Status), "storyboard"), nil'''
new_write = '''		// The shots the rows must cite are read BEFORE the version is written, so a row
		// naming a shot that does not belong to this script is refused while the write is
		// still a single call — `storyboard_items.shot_id` has NO foreign key (§9.4
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
				return nil, agent.InvalidError("Row " + itoaTool(index+1) + " names a shot that does not belong to this script version.")
			}
			if item.DurationSeconds < 0 {
				return nil, agent.InvalidError("A storyboard row cannot have a negative duration.")
			}
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
				CreatedByType:          createdBy,
				CreatedByID:            createdByID,
			})
			if err != nil {
				return nil, err
			}
			itemIDs = append(itemIDs, row.ID)
			// The asset references are recorded as USAGES rather than as a column, because
			// §7.8 normalises them: "Shot 与资产引用通过 AssetUsage/ShotAssetReference
			// 正规化". A row naming an asset that has no approved version is refused by the
			// resolution below rather than stored as a dangling reference.
			for _, ref := range item.AssetRefs {
				if err := recordShotAssetUsage(ctx, deps, request.ProjectID, row.ID, ref.AssetID, ref.UsageRole); err != nil {
					return nil, err
				}
			}
		}
		return artifactResultWithItems("storyboard_table", "storyboard_version", version.ID,
			version.VersionNumber, string(version.Status), "storyboard", itemIDs), nil'''
assert old_write in s, "write anchor"
s = s.replace(old_write, new_write, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("storyboard handler extended")
