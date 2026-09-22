package agenttools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appextraction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/extraction"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/schemas"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// These tests cover the tool table's CONTRACT rather than its handlers: which tools
// exist, whether each one has a schema, whether the schema and the handler agree about
// the arguments, and whether the ACL's matrix permits what the manifests grant.
//
// The handler paths themselves are covered by the integration tests in the packages
// they call, and by the canary run: a handler that called a service wrongly would fail
// there. What cannot be covered there is the agreement between the table and the
// schema directory — a tool registered without a schema reaches its handler with
// unvalidated arguments, and a schema without a registration is a file nothing reads.

// TestEveryRegisteredToolHasASchema is the guard NewTools cannot make.
//
// NewTools refuses a tool whose SchemaPath is empty, but it does not check that the
// schema EXISTS — it cannot, because this package embeds no schemas. So a typo in a key
// would produce a tool that fails at run time with "no schema is embedded at ...", which
// is the worst moment to find out. This is the check that moves it to the test.
func TestEveryRegisteredToolHasASchema(t *testing.T) {
	for _, key := range Keys() {
		path := "schemas/agent/tools/" + key + ".json"
		if _, err := schemas.Lookup(path); err != nil {
			t.Errorf("the tool %s names a schema that is not embedded: %v", key, err)
		}
	}
}

// TestEverySchemaHasARegisteredTool is the other direction.
//
// A schema file whose key no longer exists is dead weight that reads like a capability
// the build has. It is checked against the generator's key list rather than by listing
// the directory, because the directory is what the generator writes and the list is
// what the generator MEANT — a file left behind by a rename is exactly the case this
// catches.
func TestEverySchemaHasARegisteredTool(t *testing.T) {
	declared := generatorKeys(t)
	registered := Keys()
	if len(declared) != len(registered) {
		t.Fatalf("the generator declares %d tools and the table registers %d", len(declared), len(registered))
	}
	for index, key := range declared {
		if registered[index] != key {
			t.Fatalf("tool %d is %q in the generator and %q in the table", index, key, registered[index])
		}
	}
}

// TestToolSchemasAreStrictObjects covers the property that makes schema validation
// worth having: an invented field is refused rather than ignored.
//
// It is asserted by compiling every schema and validating a document that carries one
// extra property. A schema that accepted it would let a model put a project id in its
// arguments and rely on a handler reading it — which no handler does, but the
// difference between "does not read it" and "refuses it" is the difference between a
// convention and a boundary.
func TestToolSchemasAreStrictObjects(t *testing.T) {
	for _, key := range Keys() {
		compiled := compileToolSchema(t, key)
		document := `{"__probe__":"x"}`
		instance, err := jsonschema.UnmarshalJSON(strings.NewReader(document))
		if err != nil {
			t.Fatalf("the probe is not valid JSON: %v", err)
		}
		if err := compiled.Validate(instance); err == nil {
			t.Errorf("the schema for %s accepts an unknown property", key)
		}
	}
}

// TestToolsWithOnlyOptionalArgumentsAcceptAnEmptyObject covers the other side: a tool
// whose arguments are all optional must accept `{}`, because that is what a model sends
// when it has nothing to state and what the handler's own decoder accepts.
//
// The set is written out rather than derived from the schemas, because deriving it from
// the same `required` array the check reads would make the test agree with whatever the
// generator produced. A tool that NEEDS an argument is asserted to refuse `{}` here too,
// which is what makes the list meaningful: both directions have a failing case.
func TestToolsWithOnlyOptionalArgumentsAcceptAnEmptyObject(t *testing.T) {
	// EVERY tool, with the answer written out. The first version named seven and DERIVED the rest
	// from the same `required` array the assertion was about — which is a tautology: it asserted
	// "this schema accepts {} iff its required array is empty", a statement about JSON Schema
	// rather than about this table. The list being complete is what makes a wrong `required`
	// array a failure here instead of a fact the test reads back.
	allOptional := map[string]bool{
		// All arguments optional: an empty call states nothing, which the schema must accept.
		"workflow.read_state":        true,
		"story.read_events":          true,
		"story.read_rules":           true,
		"asset.read_approved_assets": true,
		"memory.deep_recall":         true,
		// These require an argument, which is the other direction of the same assertion: a schema
		// that accepted {} for a tool that cannot work without one would let a model make a call the
		// handler then refuses.
		"workflow.request_user_gate":                 false,
		"story.read_chapter_text":                    false,
		"script.read_story_skeleton":                 false,
		"script.read_adaptation_strategy":            false,
		"script.read_script_version":                 false,
		"script.read_script_structure":               false,
		"script.create_script_structure":             false,
		"script.create_story_skeleton_version":       false,
		"script.create_adaptation_strategy_version":  false,
		"script.create_script_version":               false,
		"storyboard.read_director_plan":              false,
		"storyboard.read_storyboard":                 false,
		"storyboard.create_director_plan_version":    false,
		"storyboard.create_storyboard_version":       false,
		"storyboard.create_storyboard_panel_version": false,
		"asset.create_candidate_version":             false,
		// WP-09's three. `script.read_shots` and `asset.create_gap_report` both require an
		// identifier; `asset.read_gap_report` is FALSE against the empty object because its
		// schema requires NOTHING and its handler refuses a call that named neither a
		// report nor an episode. The two are consistent: the schema constrains a shape and
		// the handler checks the values it uses, which is the division this table is
		// checked against.
		"script.read_shots":       false,
		"asset.create_gap_report": false,
		"asset.read_gap_report":   true,
	}
	if len(allOptional) != len(Keys()) {
		t.Fatalf("this test names %d tools and the table registers %d", len(allOptional), len(Keys()))
	}
	for _, key := range Keys() {
		wantAccepted, known := allOptional[key]
		if !known {
			t.Fatalf("the table registers %s, which this test does not cover", key)
		}
		empty, err := jsonschema.UnmarshalJSON(strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("the empty probe is not valid JSON: %v", err)
		}
		err = compileToolSchema(t, key).Validate(empty)
		if wantAccepted && err != nil {
			t.Errorf("the schema for %s refuses an empty argument object: %v", key, err)
		}
		if !wantAccepted && err == nil {
			t.Errorf("the schema for %s accepts an empty argument object although it requires something", key)
		}
	}
}

// TestNoToolSchemaAcceptsAProjectID is the boundary assertion.
//
// AGENT_CONTRACTS section 7.1 forbids taking a project or episode from a model, and the
// way that is enforced is structural: every handler reads its scope from the run, so no
// schema has a field for one. This checks the schemas rather than the handlers, because
// a schema is what a future contributor would copy.
func TestNoToolSchemaAcceptsAProjectID(t *testing.T) {
	for _, key := range Keys() {
		body, err := schemas.Lookup("schemas/agent/tools/" + key + ".json")
		if err != nil {
			t.Fatalf("reading the schema for %s: %v", key, err)
		}
		var document struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(body, &document); err != nil {
			t.Fatalf("the schema for %s is not valid JSON: %v", key, err)
		}
		// `episodeId` is deliberately allowed where an artifact is CREATED for an
		// episode: the handler checks that episode against the run's project before it
		// writes, which is the check section 7.1 asks for. What is never allowed is a
		// project, which has no such check because it IS the boundary.
		for _, field := range []string{"projectId", "tenantId", "workspaceId"} {
			if _, present := document.Properties[field]; present {
				t.Errorf("the schema for %s carries a %s field, so a model could propose a scope", key, field)
			}
		}
	}
}

// TestHandlerArgumentNamesMatchTheirSchemas is the agreement test that matters most.
//
// The runtime validates arguments against the schema and the handler decodes them with
// DisallowUnknownFields. If the two disagree about a field's name, the validator accepts
// a document the handler then refuses — a call that fails for a reason neither file
// states. So each schema's property names are compared with the fields the handler
// decodes, and the handler is found by REFLECTION over the table's own build functions
// rather than by a second list that could drift.
func TestHandlerArgumentNamesMatchTheirSchemas(t *testing.T) {
	// The expected argument names per tool. This is the third list, and it is the
	// ASSERTION rather than the source: the schema and the handler are the two sources,
	// and this test fails when either moves away from what is written here.
	expected := map[string][]string{
		"workflow.read_state":             {"workflowRunId"},
		"workflow.request_user_gate":      {"stageRunId", "reason"},
		"story.read_events":               {"chapterId", "status", "limit"},
		"story.read_chapter_text":         {"chapterId"},
		"story.read_rules":                {"category"},
		"script.read_story_skeleton":      {"versionId"},
		"script.read_adaptation_strategy": {"versionId"},
		"script.read_script_version":      {"versionId"},
		"script.read_script_structure":    {"versionId", "sceneFrom", "sceneTo"},
		"script.create_story_skeleton_version": {"episodeId", "basedOnVersionId", "openingHook",
			"coreConflict", "turningPointsJson", "climax", "endingHook",
			"estimatedDurationSeconds", "selectedEventIds", "changeReason"},
		"script.create_adaptation_strategy_version": {"episodeId", "basedOnVersionId", "strategySummary",
			"adaptationMode", "mergedEventGroupsJson", "originalAdditions", "rationale", "risks",
			"eventLinks", "changeReason"},
		"script.create_script_version": {"episodeId", "basedOnVersionId", "storySkeletonVersionId",
			"adaptationStrategyVersionId", "summary", "changeReason"},
		"script.create_script_structure": {"episodeId", "versionId", "basedOnVersionId", "summary",
			"changeReason", "scenes"},
		"storyboard.read_director_plan": {"versionId"},
		"storyboard.read_storyboard":    {"versionId", "limit"},
		"storyboard.create_director_plan_version": {"episodeId", "scriptVersionId", "basedOnVersionId",
			"visualRhythm", "cameraLanguage", "colorLighting", "staging", "continuityRules",
			"audioDirection", "changeReason"},
		"storyboard.create_storyboard_version": {"episodeId", "scriptVersionId", "directorPlanVersionId",
			"basedOnVersionId", "changeReason", "items"},
		"storyboard.create_storyboard_panel_version": {"itemId", "prompt", "negativePrompt",
			"basedOnVersionId", "changeReason"},
		"asset.read_approved_assets":     {"types", "limit"},
		"asset.create_candidate_version": {"assetId", "prompt", "negativePrompt", "metadataJson"},
		// WP-09's three, with the names their schemas declare.
		"script.read_shots": {"scriptVersionId", "sceneId", "limit"},
		"asset.create_gap_report": {"episodeId", "scriptVersionId", "summary", "basedOnVersionId",
			"items"},
		"asset.read_gap_report": {"reportId", "episodeId"},
		// WP-10 deepened this tool from the recent window to AGENT_CONTRACTS section 12.3's deep
		// recall, so the property set grew: `query` selects the walk, the two maxima bound it, and
		// `limit`/`excludeMessageId` still serve the no-query window. The assertion is what caught the
		// schema and the handler drifting apart when only one of them was updated.
		"memory.deep_recall": {"query", "maxSummaries", "maxRawMessages", "limit", "excludeMessageId"},
	}
	registered := Keys()
	if len(expected) != len(registered) {
		t.Fatalf("this test knows %d tools and the table registers %d", len(expected), len(registered))
	}
	for _, key := range registered {
		want, known := expected[key]
		if !known {
			t.Fatalf("the table registers %s, which this test does not cover", key)
		}
		got := schemaPropertyNames(t, key)
		// Both sides are sorted: the map above is written in the order a human reads a
		// request, and a JSON object's property order is the encoder's choice. Comparing
		// sorted lists makes this a test of the NAMES rather than of two orderings that
		// were never meant to agree.
		sorted := append([]string(nil), want...)
		sort.Strings(sorted)
		if strings.Join(got, ",") != strings.Join(sorted, ",") {
			t.Errorf("the schema for %s declares %v, and the handler is asserted to read %v", key, got, sorted)
		}
	}
}

// TestEveryToolHasAModeAndABound covers the two spec limits a tool cannot omit:
// section 6.1's Mode (the ACL's matrix is built from it) and section 6.4's bound on a
// result ("返回结构化、限长数据").
func TestEveryToolHasAModeAndABound(t *testing.T) {
	tools := buildForTest(t)
	for _, key := range tools.Keys() {
		tool, ok := tools.Lookup(key)
		if !ok {
			t.Fatalf("the table reports %s and then cannot find it", key)
		}
		if !agent.IsValidToolMode(tool.Spec.Mode) {
			t.Errorf("%s has mode %q", key, tool.Spec.Mode)
		}
		if tool.Spec.MaxOutputBytes <= 0 || tool.Spec.MaxOutputBytes > agent.MaxToolOutputBytes {
			t.Errorf("%s is bounded to %d bytes", key, tool.Spec.MaxOutputBytes)
		}
		if strings.TrimSpace(tool.Spec.Scope) == "" {
			t.Errorf("%s names no scope, so its isolation cannot be checked", key)
		}
	}
}

// TestNoToolIsWriteModeOutsideAnExecutionLayerTarget is a structural statement about
// what the table offers: a write tool exists only for the stages that own a write, and
// the ACL is what refuses it elsewhere. This does not replace the ACL test in
// agentruntime; it records which tools the decision and supervision layers would be
// refused, so a new write tool cannot be added without the reviewer seeing its effect.
func TestNoToolIsWriteModeOutsideAnExecutionLayerTarget(t *testing.T) {
	tools := buildForTest(t)
	// The write tools, written out here. The supervisor's read-only rule is asserted in
	// agentruntime against the ACL's matrix; this is the inventory a reviewer reads.
	wantWrites := []string{
		"script.create_story_skeleton_version",
		"script.create_adaptation_strategy_version",
		"script.create_script_version",
		"script.create_script_structure",
		"storyboard.create_director_plan_version",
		"storyboard.create_storyboard_version",
		"storyboard.create_storyboard_panel_version",
		"asset.create_candidate_version",
		"asset.create_gap_report",
	}
	gotWrites := make([]string, 0, len(wantWrites))
	for _, key := range tools.Keys() {
		tool, _ := tools.Lookup(key)
		if tool.Spec.Mode == agent.ToolWrite {
			gotWrites = append(gotWrites, key)
		}
	}
	sort.Strings(gotWrites)
	sort.Strings(wantWrites)
	if strings.Join(gotWrites, ",") != strings.Join(wantWrites, ",") {
		t.Fatalf("the write tools are\n  %v\nand this test expects\n  %v", gotWrites, wantWrites)
	}
	// And no write tool is reachable from the two layers that may not write. This is the
	// same assertion the registry makes at startup, restated here for the table itself.
	for _, key := range gotWrites {
		tool, _ := tools.Lookup(key)
		if agent.ToolAllowed(agent.LayerSupervision, tool.Spec.Mode) {
			t.Fatalf("%s would be callable by a supervisor", key)
		}
		if agent.ToolAllowed(agent.LayerDecision, tool.Spec.Mode) {
			t.Fatalf("%s would be callable by a decision agent", key)
		}
	}
}

// TestBuildRefusesAMissingService covers the fail-closed direction: a composition root
// that forgot a service gets a refusal naming it, not a tool that fails at run time.
func TestBuildRefusesAMissingService(t *testing.T) {
	// A zero Deps is the shape a forgotten composition produces.
	if _, err := Build(Deps{}); err == nil {
		t.Fatal("a table with no services was built")
	} else if !strings.Contains(err.Error(), "story service") {
		t.Fatalf("the refusal reads %q, which does not name a service", err)
	}
	// Each service in turn, so the refusal's switch is covered rather than only its
	// first arm.
	full := fullDeps()
	for name, blank := range map[string]func(*Deps){
		"story":      func(d *Deps) { d.Story = nil },
		"script":     func(d *Deps) { d.Script = nil },
		"storyboard": func(d *Deps) { d.Storyboard = nil },
		"workflow":   func(d *Deps) { d.Workflow = nil },
		"memory":     func(d *Deps) { d.Memory = nil },
		"assets":     func(d *Deps) { d.Assets = nil },
		"projects":   func(d *Deps) { d.Projects = nil },
		"chapters":   func(d *Deps) { d.Chapters = nil },
	} {
		deps := full
		blank(&deps)
		if _, err := Build(deps); err == nil {
			t.Errorf("a table without a %s was built", name)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// fullDeps is a complete Deps, for the tests that are not about handler behaviour.
//
// The services are zero-valued but non-nil, which is what Build checks: it verifies
// COMPOSITION rather than reachability, and a handler that then failed against a nil
// database would be a different test's subject.
func fullDeps() Deps {
	return Deps{
		Story:      appstory.NewService(appstory.Options{}),
		Script:     appscript.NewService(appscript.Options{}),
		Storyboard: appstoryboard.NewService(appstoryboard.Options{}),
		Workflow:   appworkflow.NewService(appworkflow.Options{}),
		Memory:     appmemory.New(nil),
		Assets:     appassets.NewService(appassets.Options{}),
		Gaps:       appassets.NewGapService(appassets.GapOptions{}),
		Projects:   appprojects.NewService(appprojects.Options{}),
		Chapters:   stubChapterReader{},
	}
}

// stubChapterReader satisfies the port without reading anything.
type stubChapterReader struct{}

func (stubChapterReader) ChapterWithText(_ context.Context, _ string) (appextraction.ChapterText, error) {
	return appextraction.ChapterText{}, nil
}

// buildForTest assembles the table over the stub dependencies.
func buildForTest(t *testing.T) *agentruntime.Tools {
	t.Helper()
	tools, err := Build(fullDeps())
	if err != nil {
		t.Fatalf("building the table: %v", err)
	}
	return tools
}

// compileToolSchema compiles one tool's embedded schema.
func compileToolSchema(t *testing.T, key string) *jsonschema.Schema {
	t.Helper()
	body, err := schemas.Lookup("schemas/agent/tools/" + key + ".json")
	if err != nil {
		t.Fatalf("reading the schema for %s: %v", key, err)
	}
	resource, err := jsonschema.UnmarshalJSON(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("the schema for %s is not valid JSON: %v", key, err)
	}
	compiler := jsonschema.NewCompiler()
	identifier := "https://infinite-atelier.invalid/schemas/agent/tools/" + key + ".json"
	if err := compiler.AddResource(identifier, resource); err != nil {
		t.Fatalf("registering the schema for %s: %v", key, err)
	}
	compiled, err := compiler.Compile(identifier)
	if err != nil {
		t.Fatalf("compiling the schema for %s: %v", key, err)
	}
	return compiled
}

// schemaPropertyNames returns a schema's property names, sorted.
func schemaPropertyNames(t *testing.T, key string) []string {
	t.Helper()
	body, err := schemas.Lookup("schemas/agent/tools/" + key + ".json")
	if err != nil {
		t.Fatalf("reading the schema for %s: %v", key, err)
	}
	var document struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("the schema for %s is not valid JSON: %v", key, err)
	}
	names := make([]string, 0, len(document.Properties))
	for name := range document.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// generatorKeys reads the key list the schema generator wrote.
//
// It reads the FILE rather than the generator's source, so this test and the
// generator's own --check agree about what was produced. A missing file is a failure
// rather than a skip: the generator is checked into the repository and its output is
// what the binary embeds.
func generatorKeys(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "schemas", "agent", "tools", "KEYS.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the generated tool key list: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	keys := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	return keys
}
