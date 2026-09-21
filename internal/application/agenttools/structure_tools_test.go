package agenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// structure_tools_test.go covers the two WP-08 tools' HANDLER behaviour, which the table's contract
// tests cannot: those assert which tools exist and what their schemas say, and neither reaches a
// handler.
//
// The three properties that matter here are the ones a schema cannot express:
//
//   - The version's episode is checked against the run's project before anything is written, because
//     a structure write is the largest a stage makes and a scope failure here would be the worst one.
//   - The draft the handler builds states NO identifier and NO ordinal, so the conversion is where
//     §17's "ID、顺序和唯一性" is honoured at the tool boundary.
//   - A paged read reports the duration of the WHOLE version rather than of the page, because that is
//     the number a supervisor compares against the episode's target.

// scriptReaderFake answers the reads these handlers make and nothing else.
//
// It embeds the interface, so a handler that reached for a read this test did not expect panics
// rather than receiving a zero value that reads like an empty project.
type scriptReaderFake struct {
	appscript.Repository
	episodes  map[string]string
	versions  map[string]scriptdomain.ScriptVersion
	scripts   map[string]scriptdomain.Script
	structure scriptdomain.ScriptStructure
	// written records what the REPOSITORY was asked to store, so a test asserts the payload that
	// would reach the database rather than the request the handler assembled. That is the stronger
	// claim: the ordinals and identifiers in it were MINTED by the service, which is exactly what
	// §17 requires and what the handler's arguments must not carry.
	written *scriptdomain.ScriptStructure
	// writtenVersion records the version the write was for, so a test can tell which row was filled.
	writtenVersion string
	// totals records the durations the service derived, in order.
	totals []int
}

func (f *scriptReaderFake) GetEpisode(_ context.Context, id string) (scriptdomain.Episode, error) {
	project, ok := f.episodes[id]
	if !ok {
		return scriptdomain.Episode{}, scriptdomain.NotFoundError()
	}
	return scriptdomain.Episode{ID: id, ProjectID: project}, nil
}

func (f *scriptReaderFake) GetScriptVersion(_ context.Context, id string) (scriptdomain.ScriptVersion, error) {
	record, ok := f.versions[id]
	if !ok {
		return scriptdomain.ScriptVersion{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (f *scriptReaderFake) GetScript(_ context.Context, id string) (scriptdomain.Script, error) {
	record, ok := f.scripts[id]
	if !ok {
		return scriptdomain.Script{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (f *scriptReaderFake) GetScriptStructure(_ context.Context, versionID string) (scriptdomain.ScriptStructure, error) {
	if f.written != nil {
		// After a write, the fake reports the structure it was GIVEN, so the read path in these tests
		// returns what the service produced rather than a second fixture that could disagree.
		return *f.written, nil
	}
	return f.structure, nil
}

// CreateScriptStructure is the REPOSITORY method, so what it receives is the materialised payload:
// the service has already minted every identifier and ordinal from the draft by this point. Recording
// it here is what lets a test assert the minting happened, which the request alone could not show.
func (f *scriptReaderFake) CreateScriptStructure(_ context.Context, structure scriptdomain.ScriptStructure) error {
	stored := structure
	f.written = &stored
	return nil
}

// SetScriptVersionTotals records the derived duration on the stored structure's version, so a read
// back through the version row is consistent with the content.
func (f *scriptReaderFake) SetScriptVersionTotals(_ context.Context, versionID string, total int, summary string) error {
	f.writtenVersion = versionID
	if f.written != nil {
		f.totals = append(f.totals, total)
	}
	return nil
}

func (f *scriptReaderFake) MaxScriptVersionNumber(_ context.Context, _ string) (int, error) {
	return 0, nil
}

// scriptToolsService builds the service over a repository fake.
//
// It takes the INTERFACE rather than the reader fake, because the link tests use a different fake over
// the same port — and the port is what the service sees, so widening the parameter is what lets one
// fixture serve both without either test knowing about the other.
func scriptToolsService(repository appscript.Repository) *appscript.Service {
	return appscript.NewService(appscript.Options{
		Repository: repository,
		Clock:      scopeClock{},
		IDs:        &counterIDs{},
	})
}

// counterIDs mints a fresh identifier per call, because a structure write needs every scene, line and
// shot to get a DIFFERENT one — the point of §17's "唯一性", and a fixed id would collapse them.
type counterIDs struct {
	next int
}

func (g *counterIDs) New() (string, error) {
	g.next++
	return "id-" + itoaSmall(g.next), nil
}

// newScriptToolRequest builds the request a stage's tool call arrives in.
func newScriptToolRequest(projectID, arguments string) agentruntime.ToolRequest {
	return agentruntime.ToolRequest{
		ProjectID:  projectID,
		EpisodeID:  "episode-1",
		AgentRunID: "run-1",
		Arguments:  json.RawMessage(arguments),
	}
}

// fixtureForStructureTools is one episode in one project with one draft script version.
func fixtureForStructureTools() *scriptReaderFake {
	return &scriptReaderFake{
		episodes: map[string]string{"episode-1": "project-1", "episode-other": "project-other"},
		scripts:  map[string]scriptdomain.Script{"script-1": {ID: "script-1", EpisodeID: "episode-1"}},
		versions: map[string]scriptdomain.ScriptVersion{
			"version-1": {
				ID: "version-1", ScriptID: "script-1", VersionNumber: 1,
				Status: versioning.StatusDraft, CreatedByType: versioning.CreatedByUser,
				StorySkeletonVersionID: "skeleton-1", AdaptationStrategyVersionID: "strategy-1",
			},
			"version-other": {
				ID: "version-other", ScriptID: "script-other", VersionNumber: 1,
				Status: versioning.StatusDraft, CreatedByType: versioning.CreatedByUser,
				StorySkeletonVersionID: "skeleton-1", AdaptationStrategyVersionID: "strategy-1",
			},
		},
	}
}

// TestCreateScriptStructureRefusesAVersionOfAnotherProject is the scope check.
//
// A structure write is the largest write a stage makes — a hundred scenes of dialogue and shots — so a
// scope failure here is the one that would be worst: an agent run in one project filling another
// project's script. The check is the same walk the other artifact tools make, and it runs BEFORE the
// write, so a refused call leaves nothing behind.
func TestCreateScriptStructureRefusesAVersionOfAnotherProject(t *testing.T) {
	fake := fixtureForStructureTools()
	// The version belongs to an episode in ANOTHER project.
	fake.scripts["script-other"] = scriptdomain.Script{ID: "script-other", EpisodeID: "episode-other"}
	handler := bindCreateScriptStructure(Deps{Script: scriptToolsService(fake)})

	_, err := handler(context.Background(), newScriptToolRequest("project-1",
		`{"versionId":"version-other","scenes":[{"slugline":"INT. \u6e21\u53e3 - \u65e5"}]}`))
	if err == nil {
		t.Fatal("a structure write into another project's script was accepted")
	}
	// The refusal is the SECURITY category, because the two situations it separates are "a stale
	// identifier" and "an attempt to reach across a boundary", and only the second is this.
	domainErr, ok := agent.AsError(err)
	if !ok {
		t.Fatalf("the refusal is a %T, want the domain's error", err)
	}
	if domainErr.Category != agent.CategorySecurity {
		t.Fatalf("the refusal is category %q, want security", domainErr.Category)
	}
	if fake.written != nil {
		t.Fatal("a refused call still wrote")
	}
}

// The ATTRIBUTION of a structure write is not asserted here, and the reason is a property of the
// write rather than an omission. `created_by_type` and `created_by_id` belong to the VERSION ROW,
// which `script.create_script_version` already writes and whose attribution is covered with the
// other write tools; a structure write fills in the content of a row that exists. What this tool
// supplies is the run id for `source_agent_run_id`, and the fake above sees it only when the service
// passes it, which the real service does — so asserting it here would be asserting the service's own
// plumbing twice.

// TestCreateScriptStructureRefusesAMismatchedEpisodeArgument covers the second statement of scope.
//
// The version's own episode is what the write follows, so an `episodeId` argument that names a
// different one is a contradiction rather than a preference — and reading past it would let a model
// state a scope that is not the one used.
func TestCreateScriptStructureRefusesAMismatchedEpisodeArgument(t *testing.T) {
	fake := fixtureForStructureTools()
	handler := bindCreateScriptStructure(Deps{Script: scriptToolsService(fake)})
	_, err := handler(context.Background(), newScriptToolRequest("project-1",
		`{"episodeId":"episode-other","versionId":"version-1","scenes":[{"slugline":"INT. \u6e21\u53e3 - \u65e5"}]}`))
	if err == nil {
		t.Fatal("an episode argument naming another episode was accepted")
	}
	if fake.written != nil {
		t.Fatal("a refused call still wrote")
	}
	// The MATCHING argument is accepted, so the check is a comparison and not a blanket refusal of
	// the field.
	if _, err := handler(context.Background(), newScriptToolRequest("project-1",
		`{"episodeId":"episode-1","versionId":"version-1","scenes":[{"slugline":"INT. \u6e21\u53e3 - \u65e5"}]}`)); err != nil {
		t.Fatalf("a matching episode argument was refused: %v", err)
	}
}

// TestCreateScriptStructureBuildsADraftWithNoIdentifiersOrOrdinals covers §17 at the tool boundary.
//
// The handler is where a model's arguments become a domain value, so it is where "the model states no
// id and no ordinal" has to hold. The assertion is on the MATERIALISED payload the repository was
// asked to store — not on the request the handler assembled — because that is what the database would
// hold, and because it is the service's own output: every identifier in it was minted and every
// ordinal is a position, which a model could not have supplied and therefore could not have got wrong.
func TestCreateScriptStructureBuildsADraftWithNoIdentifiersOrOrdinals(t *testing.T) {
	fake := fixtureForStructureTools()
	handler := bindCreateScriptStructure(Deps{Script: scriptToolsService(fake)})
	_, err := handler(context.Background(), newScriptToolRequest("project-1", `{
		"versionId":"version-1",
		"summary":"a pilot",
		"scenes":[
			{"slugline":"INT. 渡口 - 日","interiorExterior":"INT","estimatedDurationSeconds":90,
			 "dialogueLines":[{"text":"first"},{"text":"second","type":"narration"}],
			 "shots":[{"visualDescription":"河面起雾。"}]},
			{"slugline":"EXT. 渡口 - 夜","estimatedDurationSeconds":60}
		]}`))
	if err != nil {
		t.Fatalf("CreateScriptStructure: %v", err)
	}
	if fake.written == nil {
		t.Fatal("the handler did not write")
	}
	stored := *fake.written
	if len(stored.Scenes) != 2 {
		t.Fatalf("the stored version has %d scenes", len(stored.Scenes))
	}
	if stored.ScriptVersionID != "version-1" {
		t.Fatalf("the content was written for version %q", stored.ScriptVersionID)
	}
	// The ordinals are the POSITIONS, and every identifier was minted: a scene id is not a slugline
	// and not an empty string, which is what a model-supplied field would be.
	for index, scene := range stored.Scenes {
		if scene.Ordinal != index+1 {
			t.Fatalf("scene %d has ordinal %d", index, scene.Ordinal)
		}
		if scene.ID == "" || scene.ID == scene.Slugline {
			t.Fatalf("scene %d has identifier %q", index, scene.ID)
		}
		if scene.ScriptVersionID != "version-1" {
			t.Fatalf("scene %d names version %q", index, scene.ScriptVersionID)
		}
	}
	first := stored.Scenes[0]
	if first.Slugline != "INT. 渡口 - 日" {
		t.Fatalf("the first scene is %+v", first)
	}
	// The EMPTY line type defaulted rather than reaching the database, whose column has a CHECK.
	if len(first.DialogueLines) != 2 {
		t.Fatalf("the first scene has %d lines", len(first.DialogueLines))
	}
	if first.DialogueLines[0].Type != scriptdomain.LineDialogue {
		t.Fatalf("the first line's type is %q, want the default dialogue", first.DialogueLines[0].Type)
	}
	if first.DialogueLines[1].Type != scriptdomain.LineNarration {
		t.Fatalf("the second line's type is %q", first.DialogueLines[1].Type)
	}
	// Each line and shot names the scene it was NESTED under, which is the relation the ordinal could
	// only imply: a flat list with a scene id per row could name a scene the payload does not contain.
	for index, line := range first.DialogueLines {
		if line.SceneID != first.ID || line.Ordinal != index+1 {
			t.Fatalf("line %d is %+v, nested under %q", index, line, first.ID)
		}
	}
	if len(first.Shots) != 1 {
		t.Fatalf("the first scene has %d shots", len(first.Shots))
	}
	if first.Shots[0].SceneID != first.ID || first.Shots[0].Ordinal != 1 {
		t.Fatalf("the shot is %+v", first.Shots[0])
	}
	// A shot written by a stage is a DRAFT: its number and refinement belong to the storyboard stage.
	if first.Shots[0].Status != versioning.StatusDraft {
		t.Fatalf("the shot's status is %q", first.Shots[0].Status)
	}
	// The missing interior marking defaulted to OTHER, which is the schema's own default.
	if stored.Scenes[1].InteriorExterior != scriptdomain.InteriorOTHER {
		t.Fatalf("the second scene's marking is %q, want OTHER", stored.Scenes[1].InteriorExterior)
	}
	// The DURATION was derived: the handler has no field for it, and the service summed the scenes.
	if fake.writtenVersion != "version-1" {
		t.Fatalf("the totals were written for %q", fake.writtenVersion)
	}
	if len(fake.totals) != 1 || fake.totals[0] != 150 {
		t.Fatalf("the derived duration is %v, want the summed 150", fake.totals)
	}
}

// TestCreateScriptStructureRefusesAnInventedVocabularyValue covers the two closed vocabularies.
//
// The interior marking and the line type are CHECK constraints in SQL, so a value this function let
// through would surface as a constraint failure rather than as a refusal naming the field. The
// refusal is asserted to NAME the offending value, because that is what lets a model correct itself.
func TestCreateScriptStructureRefusesAnInventedVocabularyValue(t *testing.T) {
	fake := fixtureForStructureTools()
	handler := bindCreateScriptStructure(Deps{Script: scriptToolsService(fake)})
	cases := []struct {
		name      string
		arguments string
		names     string
	}{
		{
			"an invented interior marking",
			`{"versionId":"version-1","scenes":[{"slugline":"x","interiorExterior":"INSIDE"}]}`,
			"INSIDE",
		},
		{
			"an invented line type",
			`{"versionId":"version-1","scenes":[{"slugline":"x","dialogueLines":[{"text":"t","type":"song"}]}]}`,
			"song",
		},
	}
	for _, testCase := range cases {
		_, err := handler(context.Background(), newScriptToolRequest("project-1", testCase.arguments))
		if err == nil {
			t.Fatalf("%s: was accepted", testCase.name)
		}
		if !strings.Contains(err.Error(), testCase.names) {
			t.Fatalf("%s: the refusal reads %q, which does not name %q", testCase.name, err, testCase.names)
		}
		if fake.written != nil {
			t.Fatalf("%s: a refused call still wrote", testCase.name)
		}
	}
}

// TestReadScriptStructurePagesScenesAndReportsTheWholeDuration covers the read's two properties.
//
// The pages are what makes a large version readable at all (§6.4's bound), and the duration is the
// version's own rather than the page's, because a supervisor comparing it against the episode's
// target needs the number that describes the artifact rather than the window it happened to read.
func TestReadScriptStructurePagesScenesAndReportsTheWholeDuration(t *testing.T) {
	fake := fixtureForStructureTools()
	fake.structure = scriptdomain.ScriptStructure{
		ScriptVersionID: "version-1",
		Scenes: []scriptdomain.SceneStructure{
			{Scene: sceneAt(1, "INT. one - \u65e5", 90)},
			{Scene: sceneAt(2, "INT. two - \u65e5", 60)},
			{Scene: sceneAt(3, "EXT. three - \u591c", 30)},
		},
	}
	handler := bindReadScriptStructure(Deps{Script: scriptToolsService(fake)})

	// The whole version.
	result, err := handler(context.Background(), newScriptToolRequest("project-1", `{"versionId":"version-1"}`))
	if err != nil {
		t.Fatalf("ReadScriptStructure: %v", err)
	}
	whole := result.(map[string]any)
	if whole["sceneCount"] != 3 {
		t.Fatalf("the version reports %v scenes", whole["sceneCount"])
	}
	if whole["estimatedDurationSeconds"] != 180 {
		t.Fatalf("the duration is %v, want the summed 180", whole["estimatedDurationSeconds"])
	}
	if whole["sceneFrom"] != 1 || whole["sceneTo"] != 3 {
		t.Fatalf("the page is %v..%v", whole["sceneFrom"], whole["sceneTo"])
	}

	// A window: the duration is STILL the whole version's, which is the assertion a page-local sum
	// would fail.
	result, err = handler(context.Background(), newScriptToolRequest("project-1",
		`{"versionId":"version-1","sceneFrom":2,"sceneTo":2}`))
	if err != nil {
		t.Fatalf("ReadScriptStructure: %v", err)
	}
	page := result.(map[string]any)
	if page["estimatedDurationSeconds"] != 180 {
		t.Fatalf("a one-scene page reports duration %v, want the version's 180", page["estimatedDurationSeconds"])
	}
	if page["sceneCount"] != 3 {
		t.Fatalf("a one-scene page reports %v scenes", page["sceneCount"])
	}
	scenes := page["scenes"].([]map[string]any)
	if len(scenes) != 1 || scenes[0]["ordinal"] != 2 {
		t.Fatalf("the page returned %+v", scenes)
	}

	// A page past the end is CLAMPED rather than refused: a caller asking for scenes 10..20 of a
	// three-scene version asked a well-formed question whose answer is "the last one".
	result, err = handler(context.Background(), newScriptToolRequest("project-1",
		`{"versionId":"version-1","sceneFrom":10,"sceneTo":20}`))
	if err != nil {
		t.Fatalf("a page past the end was refused rather than clamped: %v", err)
	}
	clamped := result.(map[string]any)
	if clamped["sceneFrom"] != 3 || clamped["sceneTo"] != 3 {
		t.Fatalf("the clamped page is %v..%v, want 3..3", clamped["sceneFrom"], clamped["sceneTo"])
	}

	// An INVERTED range is the caller's own error and is refused: an empty list would read as "this
	// version ends sooner than you thought".
	if _, err := handler(context.Background(), newScriptToolRequest("project-1",
		`{"versionId":"version-1","sceneFrom":3,"sceneTo":1}`)); err == nil {
		t.Fatal("an inverted scene range was accepted")
	}
}

// TestReadScriptStructureRefusesAVersionOfAnotherProject is the read half of the scope check.
func TestReadScriptStructureRefusesAVersionOfAnotherProject(t *testing.T) {
	fake := fixtureForStructureTools()
	fake.scripts["script-other"] = scriptdomain.Script{ID: "script-other", EpisodeID: "episode-other"}
	fake.structure = scriptdomain.ScriptStructure{ScriptVersionID: "version-other",
		Scenes: []scriptdomain.SceneStructure{{Scene: sceneAt(1, "INT. elsewhere - \u591c", 60)}}}
	handler := bindReadScriptStructure(Deps{Script: scriptToolsService(fake)})
	_, err := handler(context.Background(), newScriptToolRequest("project-1", `{"versionId":"version-other"}`))
	if err == nil {
		t.Fatal("another project's structure was readable")
	}
	if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategorySecurity {
		t.Fatalf("the refusal is %v, want a security refusal", err)
	}
}

// TestReadScriptStructureRefusesAnUnwrittenVersion covers the empty-structure boundary.
//
// A version row exists from the moment the create tool writes it, and its content arrives later — so
// reading the content of a version nobody has filled in is a real state, not a hypothetical one. It
// is refused rather than answered with an empty list, because "this version has no scenes" is not a
// statement a page of zero scenes can distinguish from "your range was wrong".
func TestReadScriptStructureRefusesAnUnwrittenVersion(t *testing.T) {
	fake := fixtureForStructureTools()
	fake.structure = scriptdomain.ScriptStructure{ScriptVersionID: "version-1"}
	handler := bindReadScriptStructure(Deps{Script: scriptToolsService(fake)})
	if _, err := handler(context.Background(), newScriptToolRequest("project-1",
		`{"versionId":"version-1"}`)); err == nil {
		t.Fatal("a version with no content was readable")
	}
}

// TestReadScriptStructureFollowsTheContext covers §15's cancellation rule for this handler.
func TestReadScriptStructureFollowsTheContext(t *testing.T) {
	fake := fixtureForStructureTools()
	handler := bindReadScriptStructure(Deps{Script: scriptToolsService(fake)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := handler(ctx, newScriptToolRequest("project-1", `{"versionId":"version-1"}`))
	if err == nil {
		t.Fatal("a cancelled read was served")
	}
	var cancelled *agentruntime.CancelledError
	if !asCancelled(err, &cancelled) {
		t.Fatalf("the refusal is a %T, want the runtime's cancellation", err)
	}
}

// sceneAt builds one scene at an ordinal with a duration.
func sceneAt(ordinal int, slugline string, seconds int) scriptdomain.Scene {
	return scriptdomain.Scene{
		ID: "scene-" + itoaSmall(ordinal), ScriptVersionID: "version-1", Ordinal: ordinal,
		Slugline: slugline, InteriorExterior: scriptdomain.InteriorINT,
		EstimatedDurationSeconds: seconds, CreatedAt: fixtureTime(),
	}
}

// fixtureTime is a fixed clock value, so a fixture is deterministic.
func fixtureTime() time.Time { return time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC) }
