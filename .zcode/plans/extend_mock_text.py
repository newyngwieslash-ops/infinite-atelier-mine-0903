import io

p = 'internal/infrastructure/providers/mock_text.go'
s = io.open(p, encoding='utf-8').read()

# 1. The stage reader gains the six production stages.
old_stage = '''func mockScriptStageOf(request providers.TextRequest) string {
	const prefix = "# script/script.execution."
	for _, message := range request.Messages {
		for _, line := range strings.Split(message.Content, "\\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}
			stage := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			// The title is the whole line, so anything after the name is not part of it.
			if index := strings.IndexAny(stage, " \\t"); index >= 0 {
				stage = stage[:index]
			}
			switch stage {
			case "story_skeleton", "adaptation_strategy", "script_generation":
				return stage
			}
		}
	}
	return ""
}'''
new_stage = '''func mockScriptStageOf(request providers.TextRequest) string {
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
		for _, line := range strings.Split(message.Content, "\\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}
			stage := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			if index := strings.IndexAny(stage, " \\t"); index >= 0 {
				stage = stage[:index]
			}
			if stage != "" {
				return stage
			}
		}
	}
	return ""
}'''
assert old_stage in s, "stage anchor"
s = s.replace(old_stage, new_stage, 1)

# 2. The tool-call dispatcher gains the production branch.
old_call = '''func mockScriptToolCall(request providers.TextRequest) (string, json.RawMessage) {
	switch mockScriptStageOf(request) {'''
new_call = '''func mockScriptToolCall(request providers.TextRequest) (string, json.RawMessage) {
	if stage := mockProductionStageOf(request); stage != "" {
		return mockProductionToolCall(request, stage)
	}
	switch mockScriptStageOf(request) {'''
assert old_call in s, "call anchor"
s = s.replace(old_call, new_call, 1)

# 3. The artifact reporting branch gains the production arm.
old_artifact = '''	if scriptStage := mockScriptStageOf(request); scriptStage != "" {
		entityType, entityID := mockScriptArtifactOf(request, scriptStage)'''
new_artifact = '''	if productionStage := mockProductionStageOf(request); productionStage != "" {
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
		entityType, entityID := mockScriptArtifactOf(request, scriptStage)'''
assert old_artifact in s, "artifact anchor"
s = s.replace(old_artifact, new_artifact, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("mock_text extended")
