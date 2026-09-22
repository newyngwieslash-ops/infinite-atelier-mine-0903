# WP-08 mutations, part B/C/D: the tools, the mock's script-stage branches, and the wiring.
#
# Baseline for this suite (verified by hand before running the mutations):
#   go test ./internal/application/... ./internal/domain/... ./internal/infrastructure/... \
#           ./internal/desktop/ . -count=1   -> ok

PKG_TOOLS = ["go", "test", "./internal/application/agenttools/", "-count=1"]
PKG_MOCK = ["go", "test", "./internal/infrastructure/providers/", "-count=1"]
PKG_DB = ["go", "test", "./internal/infrastructure/database/", "-count=1"]
PKG_DESKTOP = ["go", "test", "./internal/desktop/", "-count=1"]
PKG_ROOT = ["go", "test", ".", "-count=1"]
PKG_AGENTRUNTIME = ["go", "test", "./internal/application/agentruntime/", "-count=1"]
PKG_DOMAIN = ["go", "test", "./internal/domain/script/", "-count=1"]

MUTATIONS = []

# ---------------------------------------------------------------------------
# B. one per new tool (agenttools/handlers.go, tools.go, scope.go)
# ---------------------------------------------------------------------------

MUTATIONS += [
    {
        "name": "B1 the structure write skips the project scope check",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif err := assertEpisodeInProject(ctx, deps, request.ProjectID, script.EpisodeID); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\t// A caller that ALSO named an episode must be naming this one, otherwise the argument is a\n\t\t// second, unchecked statement of where the write goes.",
        "new": "\t\tif err := error(nil); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\t// A caller that ALSO named an episode must be naming this one, otherwise the argument is a\n\t\t// second, unchecked statement of where the write goes.",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B2 a mismatched episode argument is read past",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif stated := strings.TrimSpace(arguments.EpisodeID); stated != \"\" && stated != script.EpisodeID {",
        "new": "\t\tif stated := strings.TrimSpace(arguments.EpisodeID); false && stated != script.EpisodeID {",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B3 an empty interior marking reaches the database",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif interior == \"\" {\n\t\t\tmarking = scriptdomain.InteriorOTHER\n\t\t} else if !scriptdomain.IsValidInteriorExterior(marking) {",
        "new": "\t\tif interior == \"\" {\n\t\t\tmarking = scriptdomain.InteriorExterior(interior)\n\t\t} else if !scriptdomain.IsValidInteriorExterior(marking) {",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B4 an empty line type reaches the database",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tif stated == \"\" {\n\t\t\t\tlineType = scriptdomain.LineDialogue\n\t\t\t} else if !scriptdomain.IsValidLineType(lineType) {",
        "new": "\t\t\tif stated == \"\" {\n\t\t\t\tlineType = scriptdomain.LineType(stated)\n\t\t\t} else if !scriptdomain.IsValidLineType(lineType) {",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B5 the scene ceiling is not enforced",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\tif len(scenes) > scriptdomain.MaxScenesPerVersion {",
        "new": "\tif false {\n\t\t_ = scriptdomain.MaxScenesPerVersion",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B6 a negative scene duration is accepted",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif entry.EstimatedDurationSeconds < 0 {",
        "new": "\t\tif false {\n\t\t\t_ = entry.EstimatedDurationSeconds",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B7 an empty scene list is accepted",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\tif len(scenes) == 0 {\n\t\treturn scriptdomain.ScriptStructureDraft{}, agent.InvalidError(\"The script version needs at least one scene.\")\n\t}",
        "new": "\tif false {\n\t\treturn scriptdomain.ScriptStructureDraft{}, agent.InvalidError(\"The script version needs at least one scene.\")\n\t}",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B8 an invented interior marking is not refused",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t} else if !scriptdomain.IsValidInteriorExterior(marking) {",
        "new": "\t\t} else if false {",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B9 an invented line type is not refused",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\t} else if !scriptdomain.IsValidLineType(lineType) {",
        "new": "\t\t\t} else if false {",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B10 a scene's dialogue line ceiling is not enforced",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif len(scene.DialogueLines) > scriptdomain.MaxLinesPerScene {",
        "new": "\t\tif false {\n\t\t\t_ = scriptdomain.MaxLinesPerScene",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B11 a scene's shot ceiling is not enforced",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif len(scene.Shots) > scriptdomain.MaxShotsPerScene {",
        "new": "\t\tif false {\n\t\t\t_ = scriptdomain.MaxShotsPerScene",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B12 the structure read skips the project scope check",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif err := assertEpisodeInProject(ctx, deps, request.ProjectID, script.EpisodeID); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tstructure, err := deps.Script.GetScriptStructure(ctx, version.ID)",
        "new": "\t\tif err := error(nil); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tstructure, err := deps.Script.GetScriptStructure(ctx, version.ID)",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B13 an inverted page range is accepted",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif from > to {",
        "new": "\t\tif false && from > to {",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B14 a page past the end is not clamped",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\tif to <= 0 || to > total {\n\t\tto = total\n\t}",
        "new": "\tif to <= 0 {\n\t\tto = total\n\t}",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B15 the page reports one scene's duration rather than the version's",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\t\"estimatedDurationSeconds\": structure.TotalDurationSeconds(),",
        "new": "\t\t\t\"estimatedDurationSeconds\": structure.Scenes[0].EstimatedDurationSeconds,",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B16 the strategy's treatment vocabulary is not checked",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tif !scriptdomain.IsValidEventTreatment(treatment) {",
        "new": "\t\t\tif false {\n\t\t\t\t_ = treatment",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B17 the skeleton's selection is dropped on the way to the service",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tSelectedEventIDs: arguments.SelectedEventIDs,",
        "new": "\t\t\tSelectedEventIDs: nil,",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B18 the strategy's treatments are dropped on the way to the service",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tEventLinks:            links,",
        "new": "\t\t\tEventLinks:            nil,",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B19 the derived duration is not reported by the write tool",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tresult[\"estimatedDurationSeconds\"] = updated.EstimatedDurationSeconds",
        "new": "\t\tresult[\"estimatedDurationSeconds\"] = 0",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B20 the write tool writes into the episode the caller named",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tProjectID:        request.ProjectID,\n\t\t\tSourceAgentRunID: request.AgentRunID,",
        "new": "\t\t\tProjectID:        \"\",\n\t\t\tSourceAgentRunID: request.AgentRunID,",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B21 the write tool attributes the version to nobody",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tCreatedByType:    createdByAgent,\n\t\t\tCreatedByID:      request.AgentRunID,",
        "new": "\t\t\tCreatedByType:    createdByAgent,\n\t\t\tCreatedByID:      \"\",",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B22 the structure tool refuses at the ceiling instead of just above it",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\tif len(scenes) > scriptdomain.MaxScenesPerVersion {",
        "new": "\tif len(scenes) > scriptdomain.MaxScenesPerVersion-1 {",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B23 the draft conversion states no scene number",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tSceneNumber:              strings.TrimSpace(scene.SceneNumber),",
        "new": "\t\t\tSceneNumber:              \"\",",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B24 a line's speaker is dropped by the draft conversion",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\t\tCharacterEntityID:  strings.TrimSpace(line.CharacterEntityID),",
        "new": "\t\t\t\tCharacterEntityID:  \"\",",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B25 a scene's cited event is dropped by the draft conversion",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tSourceStoryEventID:       strings.TrimSpace(scene.SourceStoryEventID),\n\t\t\tIsOriginalAdaptation:     scene.IsOriginalAdaptation,",
        "new": "\t\t\tSourceStoryEventID:       \"\",\n\t\t\tIsOriginalAdaptation:     scene.IsOriginalAdaptation,",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B26 the original-adaptation flag is dropped by the draft conversion",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tSourceStoryEventID:       strings.TrimSpace(scene.SourceStoryEventID),\n\t\t\tIsOriginalAdaptation:     scene.IsOriginalAdaptation,",
        "new": "\t\t\tSourceStoryEventID:       strings.TrimSpace(scene.SourceStoryEventID),\n\t\t\tIsOriginalAdaptation:     false,",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B27 a shot's negative duration is accepted",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tif shot.EstimatedDurationSeconds < 0 {",
        "new": "\t\t\tif false {\n\t\t\t\t_ = shot.EstimatedDurationSeconds",
        "cmd": PKG_TOOLS,
    },
    {
        "name": "B28 the scope check accepts any episode in any project",
        "file": "internal/application/agenttools/scope.go",
        "old": "\tif episode.ProjectID != projectID {",
        "new": "\tif false && episode.ProjectID != projectID {",
        "cmd": PKG_TOOLS,
    },
]

# ---------------------------------------------------------------------------
# C. the mock's script-stage branches
# ---------------------------------------------------------------------------

MUTATIONS += [
    {
        "name": "C1 the stage is matched by substring rather than by the skill title",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\t\tif !strings.HasPrefix(trimmed, prefix) {\n\t\t\t\tcontinue\n\t\t\t}",
        "new": "\t\t\tif !strings.Contains(trimmed, prefix) {\n\t\t\t\tcontinue\n\t\t\t}",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C2 generation asks for the version row instead of the structure",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\treturn \"script.create_script_structure\", mockStructureArguments(request)",
        "new": "\t\treturn \"script.create_script_version\", mockStructureArguments(request)",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C3 the strategy asks for the skeleton tool",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\treturn \"script.create_adaptation_strategy_version\", mockStrategyArguments(request)",
        "new": "\t\treturn \"script.create_story_skeleton_version\", mockStrategyArguments(request)",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C4 the skeleton asks for the strategy tool",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\treturn \"script.create_story_skeleton_version\", mockSkeletonArguments(request)",
        "new": "\t\treturn \"script.create_adaptation_strategy_version\", mockSkeletonArguments(request)",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C5 the skeleton's selection is never read from the state",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\tif events := fieldOnLine(state, \"selected_events=\"); events != \"\" {\n\t\targuments[\"selectedEventIds\"] = splitStateList(events)\n\t}",
        "new": "\tif false {\n\t\targuments[\"selectedEventIds\"] = splitStateList(\"\")\n\t}",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C6 the strategy states no treatment",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\tlinks = append(links, map[string]any{\"storyEventId\": event, \"treatment\": \"retained\"})",
        "new": "\t\tlinks = append(links, map[string]any{\"storyEventId\": event})",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C7 the strategy defaults its mode rather than stating one",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\t\"adaptationMode\":        \"balanced\",",
        "new": "\t\t\"adaptationMode\":        \"\",",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C8 the structure call names no version",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\tif version := fieldOnLine(state, \"script_version=\"); version != \"\" {\n\t\targuments[\"versionId\"] = version\n\t}",
        "new": "\tif false {\n\t\targuments[\"versionId\"] = \"\"\n\t}",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C9 the structure payload states an ordinal",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\t\t\t\"sceneNumber\":              \"1\",",
        "new": "\t\t\t\t\"sceneNumber\":              \"1\",\n\t\t\t\t\"ordinal\":                  1,",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C10 the structure payload states the version's own duration",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\t\"summary\": \"The deterministic mock's script: two scenes at the ferry crossing.\",",
        "new": "\t\t\"summary\":                 \"The deterministic mock's script: two scenes at the ferry crossing.\",\n\t\t\"estimatedDurationSeconds\": 150,",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C11 the artifact type of the skeleton is wrong",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\treturn \"story_skeleton_version\", fieldOnLine(state, \"skeleton_version=\")",
        "new": "\t\treturn \"script_version\", fieldOnLine(state, \"skeleton_version=\")",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C12 the artifact type of the strategy is wrong",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\treturn \"adaptation_strategy_version\", fieldOnLine(state, \"strategy_version=\")",
        "new": "\t\treturn \"script_version\", fieldOnLine(state, \"strategy_version=\")",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C13 the generation artifact reports another stage's version id",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\treturn \"script_version\", fieldOnLine(state, \"script_version=\")",
        "new": "\t\treturn \"script_version\", fieldOnLine(state, \"skeleton_version=\")",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C14 a complete reply omits the artifact even when the state names one",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\tdocument[\"artifacts\"] = []any{map[string]any{\n\t\t\t\"entityType\": entityType,\n\t\t\t\"entityId\":   entityID,\n\t\t\t\"operation\":  \"created\",\n\t\t}}",
        "new": "\t\t_ = entityType\n\t\t_ = entityID",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C15 a state that names no version still claims complete",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\tif entityID == \"\" {\n\t\t\tdocument[\"summary\"] = \"The deterministic mock ran the \" + scriptStage +\n\t\t\t\t\" stage, and the state named no version to report an artifact for.\"\n\t\t\treturn mockJSON(document)\n\t\t}",
        "new": "\t\tif false {\n\t\t\tdocument[\"summary\"] = \"\"\n\t\t\treturn mockJSON(document)\n\t\t}",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C16 a complete reply keeps nextAction at wait_user",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\tdocument[\"status\"] = \"complete\"\n\t\tdocument[\"nextAction\"] = \"review\"",
        "new": "\t\tdocument[\"status\"] = \"complete\"",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C17 the scenario branch for script stages is removed from the tool call",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\tkey, arguments := mockScriptToolCall(request)",
        "new": "\t\tkey, arguments := mockOwnTool(request), mockToolArguments(mockOwnTool(request), request)",
        "cmd": PKG_MOCK,
    },
    {
        "name": "C18 the script-stage branch answers a SUPERVISION prompt as a write",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\tif scriptStage := mockScriptStageOf(request); scriptStage != \"\" {",
        "new": "\tif scriptStage := mockScriptStageOf(request); scriptStage != \"\" && mockLayerOf(request) != \"supervision\" {",
        "cmd": PKG_MOCK,
    },
]

# ---------------------------------------------------------------------------
# D. the wiring: drama_wiring.go, agent_wiring.go, app.go, the binding
# ---------------------------------------------------------------------------

MUTATIONS += [
    {
        "name": "D1 canvasProjectorFor returns an adapter over a nil canvas",
        "file": "drama_wiring.go",
        "old": "\tif canvas == nil {\n\t\treturn nil\n\t}\n\treturn &canvasProjector{projects: canvas}",
        "new": "\treturn &canvasProjector{projects: canvas}",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D2 composeDrama supplies no projector",
        "file": "drama_wiring.go",
        "old": "\t\t\tProjector:  canvasProjectorFor(canvas),",
        "new": "\t\t\tProjector:  appscript.CanvasProjector(nil),",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D3 the projector projects every entity as a version",
        "file": "drama_wiring.go",
        "old": "\t\tEntityType: entityType,\n\t\tEntityID:   entityID,",
        "new": "\t\tEntityType: \"script_version\",\n\t\tEntityID:   entityID,",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D4 the projector passes the entity type as the node title",
        "file": "drama_wiring.go",
        "old": "\t\tTitle:      label,",
        "new": "\t\tTitle:      entityType,",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D5 the projector returns the entity id rather than the node id",
        "file": "drama_wiring.go",
        "old": "\tnode, err := p.projects.CreateCanvasProjection(ctx, appprojects.ProjectionRequest{\n\t\tProjectID:  projectID,\n\t\tEntityType: entityType,\n\t\tEntityID:   entityID,\n\t\tTitle:      label,\n\t})\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n\treturn node.ID, nil",
        "new": "\tnode, err := p.projects.CreateCanvasProjection(ctx, appprojects.ProjectionRequest{\n\t\tProjectID:  projectID,\n\t\tEntityType: entityType,\n\t\tEntityID:   entityID,\n\t\tTitle:      label,\n\t})\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n\t_ = node\n\treturn entityID, nil",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D6 the drama stack is composed but attached to nothing",
        "file": "drama_wiring.go",
        "old": "\tif w.dramaBinding != nil {\n\t\tdesktop.AttachStory(w.dramaBinding, ctx, w.story)",
        "new": "\tif false {\n\t\tdesktop.AttachStory(w.dramaBinding, ctx, w.story)",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D7 the pipeline is composed over the wrong run reader",
        "file": "agent_wiring.go",
        "old": "\t\t\tAssembly: assembly,\n\t\t\tRuns:     repository,",
        "new": "\t\t\tAssembly: assembly,\n\t\t\tRuns:     nil,",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D8 the pipeline is composed without the assembly",
        "file": "agent_wiring.go",
        "old": "\t\t\tWorkflow: deps.Drama.workflow,\n\t\t\tAssembly: assembly,",
        "new": "\t\t\tWorkflow: deps.Drama.workflow,\n\t\t\tAssembly: nil,",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D9 the pipeline is composed over the script service's own repository",
        "file": "agent_wiring.go",
        "old": "\tif deps.Drama != nil && deps.Drama.script != nil && deps.Drama.workflow != nil {",
        "new": "\tif deps.Drama != nil && deps.Drama.script != nil && deps.Drama.workflow != nil && false {",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D10 a named provider is honoured without checking it is enabled",
        "file": "agent_wiring.go",
        "old": "\t\tif enabled != 1 {\n\t\t\treturn \"\", agent.InvalidError(\"The provider this run named is disabled.\")\n\t\t}\n\t\treturn trimmed, nil",
        "new": "\t\t_ = enabled\n\t\treturn trimmed, nil",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D11 the policy lookup ignores the default row",
        "file": "agent_wiring.go",
        "old": "\tfor _, candidate := range []string{name, \"default\"} {",
        "new": "\tfor _, candidate := range []string{name} {",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D12 the policy lookup reads the default row before the layer's own",
        "file": "agent_wiring.go",
        "old": "\tfor _, candidate := range []string{name, \"default\"} {",
        "new": "\tfor _, candidate := range []string{\"default\", name} {",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D13 a policy naming a disabled provider falls through to the fallback",
        "file": "agent_wiring.go",
        "old": "\t\tif enabled != 1 {\n\t\t\t// Refused rather than skipped, so a disabled provider is a visible configuration problem\n\t\t\t// instead of a silent fallback to one the project did not choose.\n\t\t\treturn \"\", false, agent.InvalidError(\"The provider this project's policy names is disabled.\")\n\t\t}",
        "new": "\t\tif enabled != 1 {\n\t\t\tcontinue\n\t\t}",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D14 an unreadable policy refuses instead of falling through",
        "file": "agent_wiring.go",
        "old": "\tif err := json.Unmarshal([]byte(trimmed), &document); err != nil {\n\t\treturn \"\"\n\t}\n\treturn strings.TrimSpace(document.ProviderID)",
        "new": "\tif err := json.Unmarshal([]byte(trimmed), &document); err != nil {\n\t\treturn trimmed\n\t}\n\treturn strings.TrimSpace(document.ProviderID)",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D15 providerFromPolicy reads the model field as the provider",
        "file": "agent_wiring.go",
        "old": "\t\tProviderID string `json:\"providerId\"`",
        "new": "\t\tProviderID string `json:\"primaryModelId\"`",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D16 the layer policy is never consulted",
        "file": "agent_wiring.go",
        "old": "\tif resolved, ok, err := g.policyProvider(ctx, layer, projectID); err != nil {",
        "new": "\tif resolved, ok, err := g.policyProvider(ctx, layer, \"\"); err != nil {",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D17 the enablement check for a policy's provider reads the wrong row",
        "file": "agent_wiring.go",
        "old": "\t\tif err := g.db.QueryRowContext(ctx,\n\t\t\t`SELECT enabled FROM provider_configs WHERE id = ?`, providerID).Scan(&enabled); err != nil {",
        "new": "\t\tif err := g.db.QueryRowContext(ctx,\n\t\t\t`SELECT enabled FROM provider_configs WHERE id = ?`, name).Scan(&enabled); err != nil {",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D18 app.go composes the agent stack but attaches no pipeline",
        "file": "app.go",
        "old": "\t\t\t\t\tif dramaStack != nil && a.dramaBinding != nil {\n\t\t\t\t\t\tdesktop.AttachPipeline(a.dramaBinding, ctx, agentStack.Pipeline())\n\t\t\t\t\t}",
        "new": "\t\t\t\t\tif dramaStack != nil && a.dramaBinding != nil && false {\n\t\t\t\t\t\tdesktop.AttachPipeline(a.dramaBinding, ctx, agentStack.Pipeline())\n\t\t\t\t\t}",
        "cmd": PKG_ROOT,
    },
    {
        "name": "D19 the binding drops the stage's project scope",
        "file": "internal/desktop/script_binding.go",
        "old": "\t\tProjectID:         request.ProjectID,\n\t\tEpisodeID:         request.EpisodeID,\n\t\tTask:              request.Task,",
        "new": "\t\tProjectID:         \"\",\n\t\tEpisodeID:         request.EpisodeID,\n\t\tTask:              request.Task,",
        "cmd": PKG_DESKTOP,
    },
    {
        "name": "D20 the binding drops the stage's task",
        "file": "internal/desktop/script_binding.go",
        "old": "\t\tTask:              request.Task,\n\t\tTaskIsUntrusted:   request.TaskIsUntrusted,",
        "new": "\t\tTask:              \"\",\n\t\tTaskIsUntrusted:   request.TaskIsUntrusted,",
        "cmd": PKG_DESKTOP,
    },
    {
        "name": "D21 the binding reports the attempt's status as the artifact list",
        "file": "internal/desktop/script_binding.go",
        "old": "\t\tArtifactIDs: result.ArtifactIDs,\n\t\tRepaired:    result.Outcome.Repaired,",
        "new": "\t\tArtifactIDs: []string{result.Outcome.Summary},\n\t\tRepaired:    result.Outcome.Repaired,",
        "cmd": PKG_DESKTOP,
    },
    {
        "name": "D22 the gate binding drops the version the decision approves",
        "file": "internal/desktop/script_binding.go",
        "old": "\t\tArtifactVersionID:    request.ArtifactVersionID,",
        "new": "\t\tArtifactVersionID:    \"\",",
        "cmd": PKG_DESKTOP,
    },
    {
        "name": "D23 the manual-edit binding drops the user's own structure",
        "file": "internal/desktop/script_binding.go",
        "old": "\t\tedit.Structure = scriptdomain.ScriptStructureDraft{Scenes: sceneDraftsFromInput(request.Scenes)}",
        "new": "\t\tedit.Structure = scriptdomain.ScriptStructureDraft{}",
        "cmd": PKG_DESKTOP,
    },
    {
        "name": "D24 the structure DTO reports a version's own stale duration",
        "file": "internal/desktop/script_binding.go",
        "old": "\t\tEstimatedDurationSeconds: structure.TotalDurationSeconds(),",
        "new": "\t\tEstimatedDurationSeconds: 0,",
        "cmd": PKG_DESKTOP,
    },
    {
        "name": "D25 the line-lock binding maps the wrong DTO field",
        "file": "internal/desktop/script_binding.go",
        "old": "\t\tCharacterEntityID:  line.CharacterEntityID,\n\t\tText:               line.Text,\n\t\tEmotion:            line.Emotion,\n\t\tPerformanceNote:    line.PerformanceNote,\n\t\tSourceStoryEventID: line.SourceStoryEventID,\n\t\tLocked:             line.Locked,",
        "new": "\t\tCharacterEntityID:  line.CharacterEntityID,\n\t\tText:               line.Text,\n\t\tEmotion:            line.Emotion,\n\t\tPerformanceNote:    line.PerformanceNote,\n\t\tSourceStoryEventID: line.SourceStoryEventID,\n\t\tLocked:             line.Locked && false,",
        "cmd": PKG_DESKTOP,
    },
    {
        "name": "D26 the lock binding names no field",
        "file": "internal/desktop/script_binding.go",
        "old": "\t\tVersionID: versionID,\n\t\tField:     scriptdomain.LockableField(field),\n\t\tLockedBy:  lockedBy,",
        "new": "\t\tVersionID: versionID,\n\t\tField:     scriptdomain.LockableField(\"\"),\n\t\tLockedBy:  lockedBy,",
        "cmd": PKG_DESKTOP,
    },
    {
        "name": "D27 the unlock binding releases a different field",
        "file": "internal/desktop/script_binding.go",
        "old": "\tif err := service.UnlockScriptField(b.context(), versionID, scriptdomain.LockableField(field)); err != nil {",
        "new": "\tif err := service.UnlockScriptField(b.context(), versionID, scriptdomain.LockableField(\"summary\")); err != nil {",
        "cmd": PKG_DESKTOP,
    },
]
