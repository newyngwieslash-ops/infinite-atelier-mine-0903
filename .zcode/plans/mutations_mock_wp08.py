# Mutation specification for the mock's three script-stage branches.
#
# The branches exist to make one thing true: a script stage's tool call writes the artifact that stage
# OWES. The mutations are the ways that stops being true — a stage that asks for the wrong tool, an
# argument that stops being passed, a result that claims an artifact nobody wrote.

TEST_CMD = ["go", "test", "./internal/infrastructure/providers/", "./internal/infrastructure/database/", "-count=1"]

MUTATIONS = [
    {
        "name": "the stage is matched by substring rather than by the skill title",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\t\tif !strings.HasPrefix(trimmed, prefix) {\n\t\t\t\tcontinue\n\t\t\t}",
        "new": "\t\t\tif !strings.Contains(trimmed, prefix) {\n\t\t\t\tcontinue\n\t\t\t}",
    },
    {
        "name": "generation asks for the version row instead of the structure",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\treturn \"script.create_script_structure\", mockStructureArguments(request)",
        "new": "\t\treturn \"script.create_script_version\", mockStructureArguments(request)",
    },
    {
        "name": "the strategy asks for the skeleton tool",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\treturn \"script.create_adaptation_strategy_version\", mockStrategyArguments(request)",
        "new": "\t\treturn \"script.create_story_skeleton_version\", mockStrategyArguments(request)",
    },
    {
        "name": "the skeleton's selection is never read from the state",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\tif events := fieldOnLine(state, \"selected_events=\"); events != \"\" {\n\t\targuments[\"selectedEventIds\"] = splitStateList(events)\n\t}",
        "new": "\tif false {\n\t\targuments[\"selectedEventIds\"] = splitStateList(\"\")\n\t}",
    },
    {
        "name": "the strategy states no treatment",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\tlinks = append(links, map[string]any{\"storyEventId\": event, \"treatment\": \"retained\"})",
        "new": "\t\tlinks = append(links, map[string]any{\"storyEventId\": event})",
    },
    {
        "name": "the structure call names no version",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\tif version := fieldOnLine(state, \"script_version=\"); version != \"\" {\n\t\targuments[\"versionId\"] = version\n\t}",
        "new": "\tif false {\n\t\targuments[\"versionId\"] = \"\"\n\t}",
    },
    {
        "name": "the structure payload states an ordinal",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\t\t\t\"sceneNumber\":              \"1\",",
        "new": "\t\t\t\t\"sceneNumber\":              \"1\",\n\t\t\t\t\"ordinal\":                  1,",
    },
    {
        "name": "the artifact type of the skeleton is wrong",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\treturn \"story_skeleton_version\", fieldOnLine(state, \"skeleton_version=\")",
        "new": "\t\treturn \"script_version\", fieldOnLine(state, \"skeleton_version=\")",
    },
    {
        "name": "a complete reply omits the artifact even when the state names one",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\tdocument[\"artifacts\"] = []any{map[string]any{\n\t\t\t\"entityType\": entityType,\n\t\t\t\"entityId\":   entityID,\n\t\t\t\"operation\":  \"created\",\n\t\t}}",
        "new": "\t\t_ = entityType\n\t\t_ = entityID",
    },
    {
        "name": "a state that names no version still claims complete",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\tif entityID == \"\" {\n\t\t\tdocument[\"summary\"] = \"The deterministic mock ran the \" + scriptStage +\n\t\t\t\t\" stage, and the state named no version to report an artifact for.\"\n\t\t\treturn mockJSON(document)\n\t\t}",
        "new": "\t\tif false {\n\t\t\tdocument[\"summary\"] = \"\"\n\t\t\treturn mockJSON(document)\n\t\t}",
    },
    {
        "name": "the scenario branch for script stages is removed from the tool call",
        "file": "internal/infrastructure/providers/mock_text.go",
        "old": "\t\tkey, arguments := mockScriptToolCall(request)",
        "new": "\t\tkey, arguments := mockOwnTool(request), mockToolArguments(mockOwnTool(request), request)",
    },
]
