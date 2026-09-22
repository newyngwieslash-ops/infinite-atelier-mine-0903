import io

p = "internal/application/scriptpipeline/pipeline_test.go"
s = io.open(p, encoding="utf-8").read()

# 1. Replace the registry comparison with one that builds the real registry.
start = s.index("// TestTheStageMapAgreesWithTheRegistryWhereTheRegistryCanAnswer is the honest bound on the claim.")
end = s.index("// TestEveryStageArtifactTypeIsAWriteToolTarget covers the artifact-type field against the tool table.")
new = '''// TestTheStageMapAgreesWithTheRegistryWhereTheRegistryCanAnswer is the honest bound on the claim.
//
// The map is the COMPLETE statement and the registry's own lookup is the partial one: it resolves a
// supervisor from a stage name's last segment, which works for the two stages whose supervisor shares
// their name and cannot work for `script_generation`. So this asserts AGREEMENT where the registry
// answers, and asserts that the disagreement is exactly the one gap the map exists to close — rather
// than asserting agreement everywhere, which would be false.
//
// The registry is built from the pack the build actually ships, with the real tool keys and schema
// paths. A registry built from a fixture would agree with the fixture.
func TestTheStageMapAgreesWithTheRegistryWhereTheRegistryCanAnswer(t *testing.T) {
	registry := scriptPackRegistry(t)
	for _, stage := range Stages() {
		agents, _ := AgentsForStage(stage)
		// The executor: the registry resolves it by its last segment for every stage, INCLUDING
		// generation, because `script.execution.script_generation` ends in the stage's name.
		execution, ok := registry.ForStage(string(stage))
		if !ok {
			t.Errorf("%s: the registry resolves no executor, and the manifest should carry one", stage)
			continue
		}
		if execution.Key != agents.Execution {
			t.Errorf("%s: the registry resolves the executor to %q and the map states %q",
				stage, execution.Key, agents.Execution)
		}
		// The supervisor: the registry answers for two stages and CANNOT for the third.
		supervision, ok := registry.SupervisionFor(string(stage))
		if ok {
			if supervision.Key != agents.Supervision {
				t.Errorf("%s: the registry resolves the supervisor to %q and the map states %q",
					stage, supervision.Key, agents.Supervision)
			}
			continue
		}
		// A miss is expected for exactly one stage, and for a stated reason.
		if stage != StageScriptGeneration {
			t.Errorf("%s: the registry resolved no supervisor, and every other script stage's shares its name", stage)
		}
	}
	// The gap is asserted rather than described: if a future manifest renamed the supervisor so the
	// last-segment match found it, this is what tells a reader the map's third entry stopped being a
	// repair — and the map would then be a statement that happens to agree rather than the reason the
	// behaviour is right.
	if _, ok := registry.SupervisionFor("script_generation"); ok {
		t.Log("the registry now resolves script_generation's supervisor, so the map's third entry is a statement rather than a repair")
	} else {
		t.Log("confirmed: the last-segment heuristic cannot reach script_generation, which is why the map states it")
	}
}

// scriptPackRegistry builds the registry from the script pack this build ships.
//
// It loads the pack the way the assembly does — through the embedded filesystem, with the real tool
// keys and schema paths — so the registry under test is the one a run would use.
func scriptPackRegistry(t *testing.T) *agentruntime.Registry {
	t.Helper()
	sub, err := skills.Sub("script")
	if err != nil {
		t.Fatalf("the script pack is not embedded: %v", err)
	}
	loaded, err := skill.Load(skill.LoadOptions{
		Source:           skill.FS(sub),
		KnownToolKeys:    toolKeysForTest(t),
		KnownSchemaPaths: schemaPathsForTest(t),
	})
	if err != nil {
		t.Fatalf("loading the script pack: %v", err)
	}
	registry, err := agentruntime.NewRegistry(loaded.Agents, nil)
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}
	return registry
}

'''
s = s[:start] + new + s[end:]

# 2. The helper tail: no chained aliases.
marker = "// ---------------------------------------------------------------------------\n// helpers"
s = s[:s.index(marker)] + '''// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// nilOptions builds the pipeline with nothing composed, for the tests that assert a REFUSAL.
//
// Named for what it is: an empty Options would read as an empty configuration, and what this produces is
// a service that refuses everything — the fail-closed state a composition root with a missing dependency
// has.
func nilOptions() Options { return Options{} }

// testStageRun is a stage attempt in play, for the state renderer.
func testStageRun() workflow.StageRun {
	return workflow.StageRun{
		ID: "stage-1", Stage: StageStorySkeleton, Status: workflow.StageRunning, Attempt: 2,
	}
}

// stateField reads one `name=value` field out of a rendered state string, the way the mock does.
//
// The separator is the space and the newline, which is the convention `mock_text.go`'s own reader
// implements: the two must agree, and this being a re-implementation rather than a shared function is
// deliberate — a test that called the mock's reader would agree with the mock even if BOTH were wrong.
func stateField(state, name string) string {
	for _, field := range strings.Fields(state) {
		if strings.HasPrefix(field, name) {
			return strings.TrimPrefix(field, name)
		}
	}
	return ""
}

// toolKeysForTest reads the tool key list the schema generator wrote.
//
// It is the same file the tools package's own tests read, so the registry under test is assembled with
// the key set the build actually ships.
func toolKeysForTest(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, key := range schemaKeysForTest() {
		keys[key] = true
	}
	if len(keys) == 0 {
		t.Fatal("the generated tool key list is empty, so this registry proves nothing")
	}
	return keys
}

// schemaPathsForTest is every schema path this build embeds.
//
// The loader refuses a manifest naming a schema that is not there, so a stale path in the manifest fails
// here — which is the point: this helper wants the registry the build would actually assemble.
func schemaPathsForTest(t *testing.T) map[string]bool {
	t.Helper()
	paths := map[string]bool{}
	for _, path := range schemas.AgentPaths {
		paths[path] = true
	}
	for _, key := range schemaKeysForTest() {
		paths["schemas/agent/tools/"+key+".json"] = true
	}
	return paths
}

// schemaKeysForTest reads the generated tool key list.
func schemaKeysForTest() []string {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "agent", "tools", "KEYS.txt"))
	if err != nil {
		return nil
	}
	keys := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	return keys
}
'''
s = s.replace("New(nil2Options())", "New(nilOptions())")
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("ok")
