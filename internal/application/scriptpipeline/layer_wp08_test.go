package scriptpipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// layer_wp08_test.go covers what THIS layer supplies to the mechanism, and it is what remains of
// WP-08's revision tests after the mechanism was extracted for WP-09's use.
//
// The split matters for a reader: the FIX read-back, the gate ordering and the manual edit's
// two-step are the MECHANISM's and are tested in `application/stagepipeline`, against a test layer
// that states nothing. What stays here is the script layer's own knowledge — which agent serves
// which stage, which family a stage writes, and how a refusal names the pipeline a caller used.

// TestTheStageMapStatesTheArtifactTypeAndFamilyTheBuildActuallyHas covers the two entries the review
// found unasserted.
//
// `TestEveryStageArtifactTypeIsAWriteToolTarget` compared the map against a LITERAL written in the same
// test file, so changing a stage's artifact type to another value in that literal set stayed green — and
// the generation stage's `supervision` value is compared to nothing, because the registry deliberately
// cannot resolve it.
//
// This reads what is on DISK: the generated tool key list and the skill manifest. So the map is checked
// against the build rather than against a second copy of itself.
func TestTheStageMapStatesTheArtifactTypeAndFamilyTheBuildActuallyHas(t *testing.T) {
	keys := readToolKeys(t)
	for _, stage := range Stages() {
		agents, _ := AgentsForStage(stage)
		// The artifact type names a WRITE tool this build registers: `<something>.create_<artifact type>`.
		want := "create_" + agents.ArtifactType
		found := false
		for key := range keys {
			if strings.HasSuffix(key, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: the artifact type %q names no write tool in schemas/agent/tools/KEYS.txt",
				stage, agents.ArtifactType)
		}
		// The family is THIS LAYER's knowledge — the mechanism does not carry it — so it is read
		// through the layer's own mapping and checked against the DOMAIN's vocabulary: a family the
		// domain does not admit would make every lock read for this stage resolve to an empty field
		// set, which is a pin that enforces nothing.
		family, err := familyOfStage(stage)
		if err != nil {
			t.Errorf("%s: the layer cannot resolve a family: %v", stage, err)
			continue
		}
		if !scriptdomain.IsValidVersionFamily(family) {
			t.Errorf("%s: the family %q is not one the domain admits", stage, family)
		}
		if len(scriptdomain.LockableFields(family)) == 0 {
			t.Errorf("%s: the family %q admits no lockable fields", stage, family)
		}
		// And the FAMILY matches the artifact type's own prefix, which is the relation a reader would
		// check by eye: a `script_version` artifact belongs to the `script` family.
		if !strings.HasPrefix(agents.ArtifactType, string(family)) {
			t.Errorf("%s: the artifact type %q does not begin with its family %q",
				stage, agents.ArtifactType, family)
		}
	}
	// The generation stage's SUPERVISOR is the entry the map exists to state, and this is the assertion
	// the review found missing: the registry cannot resolve it, so only the manifest can check the value.
	if agents, _ := AgentsForStage(StageScriptGeneration); agents.Supervision != "script.supervision.script" {
		t.Errorf("the generation stage's supervisor is %q", agents.Supervision)
	}
	manifest := readSkillManifest(t)
	for _, key := range []string{"script.supervision.script", "script.execution.script_generation"} {
		if !manifest[key] {
			t.Errorf("the manifest does not register %q, so the map names an agent this build does not carry", key)
		}
	}
}

// TestTheLayerRefusesAnEmptyBaseVersion covers the walk's fail-closed direction.
//
// A base version whose fields cannot be read yields NO locks rather than an error: a version whose
// content was never written is a first draft, and refusing there would fail a revision that is fine.
// What the layer must never do is return a lock it could not verify, and that is the walk's own rule.
func TestTheLayerRefusesAnEmptyBaseVersion(t *testing.T) {
	// A service with no script repository refuses rather than reporting "no locks": the two send a
	// caller to different places, and a pin that silently vanished is what the refusal prevents.
	layer := NewLayer(nil)
	if _, err := layer.Locks(context.Background(), StageScriptGeneration, "version-1"); err == nil {
		t.Fatal("a lock read with no service reported no locks instead of refusing")
	}
	// A stage this layer does not drive is refused rather than mapped onto a family.
	if _, err := layer.Locks(context.Background(), "storyboard_table", "version-1"); err == nil {
		t.Fatal("a stage this layer does not drive resolved a family")
	}
	if err := layer.Approve(context.Background(), "storyboard_table", "version-1", ""); err == nil {
		t.Fatal("a stage this layer does not drive was approved through it")
	}
	if _, err := layer.WriteVersion(context.Background(), "storyboard_table", LiveStageAgents{},
		stagepipeline.ManualEditRequest{}); err == nil {
		t.Fatal("a stage this layer does not drive accepted a manual edit")
	}
}

// TestTheManualEditsScopeCheckIsWiredByDefault covers the defect the EXTRACTION introduced.
//
// WP-08's `assertEpisodeInProject` read the episode through the script service it already held.
// Extracting the mechanism replaced that with a port — correctly, since the mechanism cannot
// import a layer's service — but nothing supplied an implementation for the script layer, and
// the check's own documented rule ("No project means the caller's own edit through a route that
// does not state one") made the gap INVISIBLE to every existing test: a caller that stated a
// project would get an unavailable-lookup refusal, and a caller that stated none got no check.
//
// So this asserts the WIRING rather than the rule: a pipeline built the ordinary way must carry
// a lookup, and an explicitly supplied one must be honoured rather than replaced.
func TestTheManualEditsScopeCheckIsWiredByDefault(t *testing.T) {
	// A caller that supplies the script service gets the check, because the default is the
	// service's own episode read.
	defaulted := New(Options{Script: appscript.NewService(appscript.Options{})})
	if defaulted.Episodes() == nil {
		t.Fatal("a pipeline built with a script service carries no episode lookup, so the manual edit's scope check can never run")
	}
	// An explicitly supplied implementation is honoured rather than overwritten, which is what
	// lets the production pipeline supply its own.
	sentinel := fixedEpisodeProjects{project: "project-7"}
	overridden := New(Options{Episodes: sentinel})
	got, err := overridden.Episodes().ProjectOfEpisode(context.Background(), "episode-1")
	if err != nil || got != "project-7" {
		t.Fatalf("a supplied lookup answered %q, %v", got, err)
	}
	// And the default reads the EPISODE'S project rather than assuming one: a service with no
	// repository refuses rather than reporting a project it did not read.
	empty := EpisodeProjects{}
	if _, err := empty.ProjectOfEpisode(context.Background(), "episode-1"); err == nil {
		t.Fatal("an episode lookup with no script service answered a project it never read")
	}
}

// fixedEpisodeProjects is an episode lookup that answers one project, for the wiring test.
type fixedEpisodeProjects struct{ project string }

func (f fixedEpisodeProjects) ProjectOfEpisode(context.Context, string) (string, error) {
	return f.project, nil
}

// TestAgentRefusalNamesTheStage covers the refusal a caller sees when they use the wrong pipeline.
func TestAgentRefusalNamesTheStage(t *testing.T) {
	// The mechanism's refusal is the one a caller sees, and it names the stage so the caller's next
	// step is to use the right pipeline rather than to hunt for a missing registration.
	layer := NewLayer(nil)
	err := layer.Approve(context.Background(), "storyboard_table", "v", "")
	if err == nil {
		t.Fatal("no refusal")
	}
	if !strings.Contains(err.Error(), "storyboard_table") {
		t.Fatalf("the refusal reads %q, which does not name the stage", err)
	}
	if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryInvalidInput {
		t.Fatalf("the refusal is %v, want an invalid-input refusal", err)
	}
}

// TestStartRevisionRefusesWithoutTheServices covers the fail-closed direction of every command.
//
// A build whose agent stack did not compose has no engine, and every stage command must then REFUSE
// rather than report success — a stage that "ran" with nothing behind it would be a workflow advancing
// on nothing.
func TestStartRevisionRefusesWithoutTheServices(t *testing.T) {
	service := New(Options{})
	if _, err := service.StartRevision(context.Background(), "stage-1"); err == nil {
		t.Fatal("a revision was started with no engine")
	}
	if _, err := service.ManualEdit(context.Background(), ManualEditRequest{StageRunID: "stage-1"}); err == nil {
		t.Fatal("a manual edit was applied with no services")
	}
	if _, err := service.RunStage(context.Background(), stagepipeline.StageRequest{Stage: StageStorySkeleton}); err == nil {
		t.Fatal("a stage ran with no services")
	}
	if _, err := service.RunSupervision(context.Background(), stagepipeline.SupervisionRequest{
		StageRunID: "stage-1",
	}); err == nil {
		t.Fatal("a supervision ran with no services")
	}
	if _, err := service.ApplyUserGate(context.Background(), stagepipeline.GateRequest{
		StageRunID: "stage-1",
	}); err == nil {
		t.Fatal("a gate decision was applied with no services")
	}
	// An empty stage run identifier is refused before any lookup, so a caller with a blank field learns
	// that rather than seeing a not-found from an empty query.
	if _, err := service.StartRevision(context.Background(), "   "); err == nil {
		t.Fatal("a blank stage run was accepted")
	}
}

// readToolKeys reads the generated tool key list, so an assertion can compare the stage map against
// the BUILD rather than against a second copy of itself.
func readToolKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, key := range schemaKeysForTest() {
		keys[key] = true
	}
	if len(keys) == 0 {
		t.Fatal("the generated tool key list is empty, so this assertion proves nothing")
	}
	return keys
}

// readSkillManifest reads the script pack's agent keys, so the map's supervisor entries are checked
// against what the build actually registers.
func readSkillManifest(t *testing.T) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "skills", "script", "manifest.json"))
	if err != nil {
		t.Fatalf("reading the script manifest: %v", err)
	}
	var manifest struct {
		Agents []struct {
			Key string `json:"key"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("parsing the script manifest: %v", err)
	}
	keys := map[string]bool{}
	for _, agent := range manifest.Agents {
		keys[agent.Key] = true
	}
	if len(keys) == 0 {
		t.Fatal("the script manifest registers no agents")
	}
	return keys
}
