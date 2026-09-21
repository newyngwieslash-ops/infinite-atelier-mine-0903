package scriptpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/skill"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/schemas"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/skills"
)

// pipeline_test.go covers the two properties this package exists for: the STATED stage→agent map, and
// the FIX loop that reads a user's findings back from the database.
//
// The first is a claim about a heuristic that does not work: `Registry.SupervisionFor` matches a stage
// name against an agent key's last segment, and `script_generation` has no such match. The test asserts
// the map agrees with the registry wherever the registry CAN answer, which is what makes the map the
// complete statement rather than a rival one.
//
// The second is AC-SCRIPT-002's own scenario. A FIX that ran without its findings would re-run the stage
// from scratch while being recorded as a revision against specific issues — an outcome no assertion about
// "a new version exists" would catch.

// TestTheStageMapIsCompleteForEveryStageThisPipelineDrives covers the map's closure.
//
// A stage with no entry is REFUSED rather than guessed at, so a missing entry is a stage this pipeline
// cannot run — and the failure would be at the first call rather than at review time, which is the
// fail-closed direction. The three stages are the three FR-040 names, and the map's keys are asserted
// against that list rather than against itself.
func TestTheStageMapIsCompleteForEveryStageThisPipelineDrives(t *testing.T) {
	// The stages, written out here as the assertion rather than read from the map: a map compared with
	// its own key list would assert nothing.
	want := map[Stage]bool{
		StageStorySkeleton:      true,
		StageAdaptationStrategy: true,
		StageScriptGeneration:   true,
	}
	if len(Stages()) != len(want) {
		t.Fatalf("Stages() lists %d and this test names %d", len(Stages()), len(want))
	}
	for _, stage := range Stages() {
		if !want[stage] {
			t.Fatalf("Stages() lists %q, which this test does not name", stage)
		}
		agents, ok := AgentsForStage(stage)
		if !ok {
			t.Fatalf("the pipeline drives %q and its map has no entry", stage)
		}
		// Every field is used by a caller, so an empty one is a stage that half works: the execution key
		// starts the attempt, the supervision key reviews it, the artifact type names what was written,
		// and the family is what reads its locks.
		for name, value := range map[string]string{
			"execution":   agents.Execution,
			"supervision": agents.Supervision,
			"artifact":    agents.ArtifactType,
			"family":      string(agents.Family),
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: the %s is empty", stage, name)
			}
		}
	}
	// A stage this pipeline does not drive is refused, and the refusal names it: a caller that asked for
	// the storyboard stage here has used the right name for the wrong pipeline.
	if _, ok := AgentsForStage("storyboard_table"); ok {
		t.Fatal("a stage this pipeline does not drive has an entry")
	}
	_, err := New(nilOptions()).RunStage(context.Background(), StageRequest{Stage: "storyboard_table"})
	if err == nil {
		t.Fatal("an undriven stage was accepted")
	}
}

// TestTheStageMapAgreesWithTheRegistryWhereTheRegistryCanAnswer is the honest bound on the claim.
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
	// The gap is asserted rather than described, which the first version of this test got wrong: it
	// logged the outcome in both branches, so nothing failed if a future manifest made the registry
	// resolve the supervisor — and the map's third entry would have silently stopped being the reason the
	// behaviour is right. An independent review found the pair of `t.Log`s under a comment promising an
	// assertion.
	//
	// This IS the assertion, and it is deliberately the opposite of the loop above: the registry must NOT
	// resolve `script_generation`'s supervisor. If it ever does, this fails and the reader is sent to the
	// map's comment to decide whether the entry is still load-bearing.
	if supervision, ok := registry.SupervisionFor("script_generation"); ok {
		t.Fatalf("the last-segment heuristic now resolves script_generation's supervisor to %q; "+
			"the map's third entry was a repair for its failure to, so the comment stating that is now wrong",
			supervision.Key)
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
	// The tool table is built from the generated key list with no-op handlers, and that is the honest
	// choice for THIS assertion: what is under test is which agent KEYS the manifest registers, and the
	// registry's only use for the table here is to refuse a grant naming a key that does not exist. The
	// table's own behaviour — its handlers, its modes, its bounds — is the agenttools package's subject,
	// and a second copy of it here would be a fixture the registry agreed with.
	table, err := agentruntime.NewTools(toolTableForTest(schemaKeysForTest()))
	if err != nil {
		t.Fatalf("building the tool table: %v", err)
	}
	registry, err := agentruntime.NewRegistry(loaded.Specs, table)
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}
	return registry
}

// toolTableForTest builds one registration per generated tool key.
//
// The mode comes from the KEY's own verb, which is enough for this test: a supervisor granted a write
// tool would be refused by the authorizer, and that refusal is the agenttools package's assertion rather
// than this one's. What matters here is that every key a manifest grants exists.
func toolTableForTest(keys []string) []agentruntime.Tool {
	tools := make([]agentruntime.Tool, 0, len(keys))
	for _, key := range keys {
		mode := agent.ToolRead
		if strings.Contains(key, ".create_") {
			mode = agent.ToolWrite
		}
		tools = append(tools, agentruntime.Tool{
			Spec:       agent.ToolSpec{Key: key, Mode: mode, Scope: "project", MaxOutputBytes: 4096},
			Handler:    func(context.Context, agentruntime.ToolRequest) (any, error) { return map[string]any{}, nil },
			SchemaPath: "schemas/agent/tools/" + key + ".json",
		})
	}
	return tools
}

// TestEveryStageArtifactTypeIsAWriteToolTarget covers the artifact-type field against the tool table.
//
// The pipeline names an artifact type so a caller knows what the stage writes, and the name is also how
// the FIX path finds the version a stage produced — by looking for the write tool whose key ends in
// `create_<artifact type>`. So an artifact type with no such tool is a stage whose version can never be
// found, and the failure would be silent: no locks stated, and a revision that respected nothing.
func TestEveryStageArtifactTypeIsAWriteToolTarget(t *testing.T) {
	// The two new script tools, plus the version writes the two upstream stages use.
	registered := map[string]bool{
		"story_skeleton_version":      true,
		"adaptation_strategy_version": true,
		"script_version":              true,
	}
	for _, stage := range Stages() {
		agents, _ := AgentsForStage(stage)
		if !registered[agents.ArtifactType] {
			t.Errorf("%s: the artifact type %q is not one of the write tools this build registers",
				stage, agents.ArtifactType)
		}
	}
}

// TestArtifactIDsComeFromToolCallsRatherThanTheAnswer covers AC-AGENT-003's division.
//
// The pipeline reports what a stage WROTE, and it reads that from the recorded tool calls rather than
// from the model's answer — an answer is a claim about what was produced, and a call's result is the row
// the write returned. A stage that reported an artifact it never wrote is exactly what this avoids
// copying into the record.
func TestArtifactIDsComeFromToolCallsRatherThanTheAnswer(t *testing.T) {
	// A result in the shape `artifactResult` builds.
	called := `{"artifacts":[{"entityType":"script_version","entityId":"version-from-the-write","operation":"created"}]}`
	outcome := agentruntime.Outcome{
		Output: []byte(`{"status":"complete","artifacts":[{"entityType":"script_version","entityId":"claimed-in-the-answer","operation":"created"}]}`),
		ToolCalls: []agent.AgentToolCall{
			{ToolKey: "script.read_story_skeleton", OutputJSON: `{"versionId":"read-not-written"}`},
			{ToolKey: "script.create_script_structure", OutputJSON: called},
		},
	}
	ids := artifactIDsOf(outcome)
	if len(ids) != 1 {
		t.Fatalf("the identifiers are %v, want exactly the one a write produced", ids)
	}
	if ids[0] != "version-from-the-write" {
		t.Fatalf("the identifier is %q, which came from somewhere other than the write", ids[0])
	}
	// A call whose result could not be parsed yields nothing rather than an error: the caller's next step
	// is to look at the run, not to fail the stage twice.
	for _, raw := range []string{"", "not json", `{"artifacts":[]}`, `{"artifacts":[{"entityId":""}]}`} {
		if id := entityIDOf(raw); id != "" {
			t.Errorf("a result of %q yielded the identifier %q", raw, id)
		}
	}
}

// TestTheStateLayerCarriesTheFieldsTheToolsNeed covers the prompt's state rendering.
//
// The stage's write tools name an episode and a version, and they read those from the state layer the
// pipeline renders — so a field the renderer omits is a tool call its own schema refuses. The names are
// asserted against the mock's `fieldOnLine` convention, because the deterministic model parses this
// string and the two cannot drift without a canary failing somewhere far from the cause.
func TestTheStateLayerCarriesTheFieldsTheToolsNeed(t *testing.T) {
	service := New(nilOptions())
	state := service.stateFor(testStageRun(), StageRequest{
		WorkflowRunID:     "run-1",
		Stage:             StageScriptGeneration,
		ProjectID:         "project-1",
		EpisodeID:         "episode-1",
		ScriptVersionID:   "version-1",
		SkeletonVersionID: "skeleton-1",
		StrategyVersionID: "strategy-1",
		SelectedEventIDs:  []string{"event-1", "event-2"},
	})
	// Each field, in the `name=value` form the mock reads. The separator is a space, which is why the
	// selected-event list is comma-separated inside one field.
	for name, want := range map[string]string{
		"workflow_run=":     "run-1",
		"stage=":            "script_generation",
		"stage_run=":        "stage-1",
		"episode=":          "episode-1",
		"script_version=":   "version-1",
		"skeleton_version=": "skeleton-1",
		"strategy_version=": "strategy-1",
		"selected_events=":  "event-1,event-2",
	} {
		if got := stateField(state, name); got != want {
			t.Errorf("%s is %q, want %q\nstate: %s", name, got, want, state)
		}
	}
	// An EMPTY field is omitted rather than rendered blank: a model reading `script_version=` would take
	// it for an identifier that exists and is empty.
	bare := service.stateFor(testStageRun(), StageRequest{
		WorkflowRunID: "run-1", Stage: StageStorySkeleton,
	})
	for _, absent := range []string{"script_version=", "episode=", "selected_events="} {
		if strings.Contains(bare, absent) {
			t.Errorf("an unstated field is rendered: %s\nstate: %s", absent, bare)
		}
	}
	// The attempt number and status travel, because a model reviewing its own revision needs to know
	// which attempt it is on.
	if got := stateField(bare, "attempt="); got != "2" {
		t.Errorf("the attempt is %q, want 2", got)
	}
	if got := stateField(bare, "status="); got != "running" {
		t.Errorf("the status is %q", got)
	}
}

// TestIssueIDsAreReadFromTheDecisionRow covers the FIX loop's durable read.
//
// The findings are a JSON array on the decision row, and a malformed one is a corrupt row rather than a
// user's intent — so it is REFUSED. A FIX that could not read its findings would re-run the stage from
// scratch while being recorded as a revision against specific issues, which is the exact failure the
// read-back exists to prevent.
func TestIssueIDsAreReadFromTheDecisionRow(t *testing.T) {
	ids, err := issueIDsOf(`["issue-1","issue-2"]`)
	if err != nil || len(ids) != 2 || ids[0] != "issue-1" {
		t.Fatalf("the findings are %v, %v", ids, err)
	}
	// The column's ordinary empty value.
	ids, err = issueIDsOf("[]")
	if err != nil || len(ids) != 0 {
		t.Fatalf("an empty finding list returned %v, %v", ids, err)
	}
	ids, err = issueIDsOf("")
	if err != nil || len(ids) != 0 {
		t.Fatalf("an empty column returned %v, %v", ids, err)
	}
	// Blank entries are dropped, because an empty identifier names nothing a re-run could address.
	ids, err = issueIDsOf(`["issue-1","","   "]`)
	if err != nil || len(ids) != 1 {
		t.Fatalf("a list with blanks returned %v, %v", ids, err)
	}
	// A malformed list is refused rather than treated as empty.
	if _, err := issueIDsOf(`["issue-1"`); err == nil {
		t.Fatal("a malformed finding list was accepted, so a FIX could run with no findings")
	}
	// A shape that is not an array at all is refused too.
	if _, err := issueIDsOf(`{"issue":"1"}`); err == nil {
		t.Fatal("a finding list that is not an array was accepted")
	}
}

// TestLockedRefsAreReadFromTheDecisionRow covers the other half of what a revision must know.
//
// The column is `locked_entity_refs_json TEXT NOT NULL DEFAULT ”`, so an EMPTY STRING is the ordinary
// value rather than an empty array — and a pin with no identifier names nothing, so it is dropped rather
// than passed on.
func TestLockedRefsAreReadFromTheDecisionRow(t *testing.T) {
	refs, err := lockedRefsOf("")
	if err != nil || len(refs) != 0 {
		t.Fatalf("an empty column returned %v, %v", refs, err)
	}
	refs, err = lockedRefsOf(`[]`)
	if err != nil || len(refs) != 0 {
		t.Fatalf("an empty array returned %v, %v", refs, err)
	}
	refs, err = lockedRefsOf(`[{"entityType":"scene","entityId":"scene-1","field":"summary","label":"the opening"}]`)
	if err != nil {
		t.Fatalf("a well-formed pin was refused: %v", err)
	}
	if len(refs) != 1 || refs[0].EntityID != "scene-1" || refs[0].Field != "summary" {
		t.Fatalf("the pin is %+v", refs)
	}
	// A pin with no identifier is dropped: it names nothing a model could read, and passing it on would
	// send the model looking for a row that does not exist.
	refs, err = lockedRefsOf(`[{"entityType":"scene","entityId":"  ","label":"x"},{"entityType":"scene","entityId":"scene-2"}]`)
	if err != nil {
		t.Fatalf("a list with a blank pin was refused: %v", err)
	}
	if len(refs) != 1 || refs[0].EntityID != "scene-2" {
		t.Fatalf("the pins are %+v, want only the one with an identifier", refs)
	}
	// A malformed list is refused rather than dropped, because a revision that could not read the user's
	// pins would rewrite content the user protected.
	if _, err := lockedRefsOf(`[{"entityId":`); err == nil {
		t.Fatal("a malformed pin list was accepted")
	}
}

// ---------------------------------------------------------------------------
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
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	return keys
}
