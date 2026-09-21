package agenttools

import (
	"context"
	"strings"
	"testing"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// tool_arguments_wp08_test.go covers the conversions and the pass-throughs the mutation pass showed
// were unwatched.
//
// Nine of fifteen mutations over these two tools SURVIVED on the first run, and every one of them was
// a behaviour a model's arguments flow through: a default that stopped being applied, a ceiling that
// stopped being enforced, a link set that was dropped on the way to the service. The contract tests
// above assert which tools exist and what their schemas say; none of them reaches a handler, so none
// of them could see any of this.

// TestDraftDefaultsAreAppliedBeforeTheDatabaseSeesThem covers the two columns that carry a CHECK.
//
// `scenes.interior_exterior` and `dialogue_lines.line_type` are enumerated in SQL, so a default that
// stopped being applied would surface as a constraint failure — a message about a statement rather
// than about the field the model got wrong. Both defaults are asserted, and both are asserted to be
// the SCHEMA's own value rather than merely "not empty".
func TestDraftDefaultsAreAppliedBeforeTheDatabaseSeesThem(t *testing.T) {
	draft, err := scriptDraftFromArguments([]scriptStructureSceneArg{{
		Slugline: "INT. somewhere - day",
		DialogueLines: []scriptStructureLineArg{
			{Text: "no type stated"},
			{Text: "a type stated", Type: "narration"},
		},
	}})
	if err != nil {
		t.Fatalf("scriptDraftFromArguments: %v", err)
	}
	if draft.Scenes[0].InteriorExterior != scriptdomain.InteriorOTHER {
		t.Fatalf("an omitted interior marking became %q, want the schema's OTHER",
			draft.Scenes[0].InteriorExterior)
	}
	if draft.Scenes[0].DialogueLines[0].Type != scriptdomain.LineDialogue {
		t.Fatalf("an omitted line type became %q, want the schema's dialogue",
			draft.Scenes[0].DialogueLines[0].Type)
	}
	// A STATED type is kept, so the default is a fallback rather than an override.
	if draft.Scenes[0].DialogueLines[1].Type != scriptdomain.LineNarration {
		t.Fatalf("a stated line type became %q", draft.Scenes[0].DialogueLines[1].Type)
	}
	// A STATED marking is kept too, and the whitespace around it is trimmed rather than passed to a
	// CHECK constraint that would refuse it.
	padded, err := scriptDraftFromArguments([]scriptStructureSceneArg{{
		Slugline: "x", InteriorExterior: "  EXT  ",
	}})
	if err != nil {
		t.Fatalf("scriptDraftFromArguments: %v", err)
	}
	if padded.Scenes[0].InteriorExterior != scriptdomain.InteriorEXT {
		t.Fatalf("a padded marking became %q", padded.Scenes[0].InteriorExterior)
	}
}

// TestDraftCeilingsAreEnforced covers the three bounds a model's payload is measured against.
//
// The ceilings are the domain's (§7.6's per-version, per-scene and per-shot limits), and they are
// enforced HERE as well as in the validator because the tool is where a payload that is too large
// arrives: refusing it by name is what tells a model to split its answer, where a validator failure
// after the fact would tell it only that something was wrong.
func TestDraftCeilingsAreEnforced(t *testing.T) {
	// The exact bound passes, so the check is a ceiling rather than an off-by-one refusal.
	atCeiling := make([]scriptStructureSceneArg, 0, scriptdomain.MaxScenesPerVersion)
	for index := 0; index < scriptdomain.MaxScenesPerVersion; index++ {
		atCeiling = append(atCeiling, scriptStructureSceneArg{Slugline: "INT. x - day"})
	}
	if _, err := scriptDraftFromArguments(atCeiling); err != nil {
		t.Fatalf("a payload at the scene ceiling was refused: %v", err)
	}
	// One over is refused.
	over := append(atCeiling, scriptStructureSceneArg{Slugline: "INT. y - day"})
	if _, err := scriptDraftFromArguments(over); err == nil {
		t.Fatal("a payload past the scene ceiling was accepted")
	}
	// The per-scene line ceiling.
	lines := make([]scriptStructureLineArg, 0, scriptdomain.MaxLinesPerScene+1)
	for index := 0; index <= scriptdomain.MaxLinesPerScene; index++ {
		lines = append(lines, scriptStructureLineArg{Text: "line"})
	}
	if _, err := scriptDraftFromArguments([]scriptStructureSceneArg{{
		Slugline: "x", DialogueLines: lines,
	}}); err == nil {
		t.Fatal("a scene past the line ceiling was accepted")
	}
	// And the per-scene shot ceiling.
	shots := make([]scriptStructureShotArg, 0, scriptdomain.MaxShotsPerScene+1)
	for index := 0; index <= scriptdomain.MaxShotsPerScene; index++ {
		shots = append(shots, scriptStructureShotArg{VisualDescription: "shot"})
	}
	if _, err := scriptDraftFromArguments([]scriptStructureSceneArg{{
		Slugline: "x", Shots: shots,
	}}); err == nil {
		t.Fatal("a scene past the shot ceiling was accepted")
	}
}

// TestDraftRefusesAnEmptyAndANegativeValue covers the two shapes of nonsense.
//
// An empty payload would write a version with no scenes, which reads to a reviewer as a version that
// exists; a negative duration would fail the column's own CHECK for a reason no message explains.
func TestDraftRefusesAnEmptyAndANegativeValue(t *testing.T) {
	if _, err := scriptDraftFromArguments(nil); err == nil {
		t.Fatal("an empty payload was accepted")
	}
	_, err := scriptDraftFromArguments([]scriptStructureSceneArg{{Slugline: "x", EstimatedDurationSeconds: -1}})
	if err == nil {
		t.Fatal("a negative scene duration was accepted")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Fatalf("the refusal reads %q", err)
	}
	if _, err := scriptDraftFromArguments([]scriptStructureSceneArg{{
		Slugline: "x", Shots: []scriptStructureShotArg{{EstimatedDurationSeconds: -1}},
	}}); err == nil {
		t.Fatal("a negative shot duration was accepted")
	}
}

// TestLinkArgumentsReachTheService covers the two pass-throughs the mutation pass showed were
// unwatched.
//
// A selection and a treatment set are FACTS about an artifact — §7.4 makes which events an episode
// contains a queryable relation, §7.5 makes the treatments the strategy's content — so a handler that
// dropped them on the way to the service would produce a version that decided nothing while reporting
// success. Both are asserted through the real service, on the payload the repository was asked to
// store.
func TestLinkArgumentsReachTheService(t *testing.T) {
	// The skeleton.
	skeletonFake := fixtureForLinkTools()
	skeletonHandler := bindCreateStorySkeletonVersion(Deps{Script: scriptToolsService(skeletonFake)})
	if _, err := skeletonHandler(context.Background(), newScriptToolRequest("project-1", `{
		"episodeId":"episode-1","openingHook":"a hook",
		"selectedEventIds":["event-1","event-2"]}`)); err != nil {
		t.Fatalf("CreateStorySkeletonVersion: %v", err)
	}
	if len(skeletonFake.linkedEventIDs) != 2 ||
		skeletonFake.linkedEventIDs[0] != "event-1" || skeletonFake.linkedEventIDs[1] != "event-2" {
		t.Fatalf("the selection reached the repository as %v", skeletonFake.linkedEventIDs)
	}

	// The strategy, whose order is the adaptation's order.
	strategyFake := fixtureForLinkTools()
	strategyHandler := bindCreateAdaptationStrategyVersion(Deps{Script: scriptToolsService(strategyFake)})
	if _, err := strategyHandler(context.Background(), newScriptToolRequest("project-1", `{
		"episodeId":"episode-1","strategySummary":"a strategy",
		"eventLinks":[
			{"storyEventId":"event-2","treatment":"removed"},
			{"storyEventId":"event-1","treatment":"retained"}
		]}`)); err != nil {
		t.Fatalf("CreateAdaptationStrategyVersion: %v", err)
	}
	if len(strategyFake.linkedTreatments) != 2 {
		t.Fatalf("the treatments reached the repository as %v", strategyFake.linkedTreatments)
	}
	// The ORDER is the caller's, and the ordinals are the positions: that is the only thing §7.5's
	// "reordered" can mean, and a handler that sorted or dropped them would lose it.
	if strategyFake.linkedTreatments[0].StoryEventID != "event-2" ||
		strategyFake.linkedTreatments[0].Treatment != scriptdomain.TreatmentRemoved {
		t.Fatalf("the first treatment is %+v", strategyFake.linkedTreatments[0])
	}
	if strategyFake.linkedTreatments[0].Ordinal != 1 || strategyFake.linkedTreatments[1].Ordinal != 2 {
		t.Fatalf("the ordinals are %d and %d",
			strategyFake.linkedTreatments[0].Ordinal, strategyFake.linkedTreatments[1].Ordinal)
	}
}

// TestAnInventedTreatmentIsRefusedByTheHandler covers the vocabulary check at the tool boundary.
//
// The treatment column has a CHECK, so a value this handler let through would reach the database and
// come back as a constraint failure rather than as a refusal naming the field.
func TestAnInventedTreatmentIsRefusedByTheHandler(t *testing.T) {
	fake := fixtureForLinkTools()
	handler := bindCreateAdaptationStrategyVersion(Deps{Script: scriptToolsService(fake)})
	_, err := handler(context.Background(), newScriptToolRequest("project-1", `{
		"episodeId":"episode-1","strategySummary":"a strategy",
		"eventLinks":[{"storyEventId":"event-1","treatment":"rewritten"}]}`))
	if err == nil {
		t.Fatal("an invented treatment was accepted")
	}
	if !strings.Contains(err.Error(), "rewritten") {
		t.Fatalf("the refusal reads %q, which does not name the value", err)
	}
	if fake.linkedTreatments != nil {
		t.Fatal("a refused call still wrote")
	}
}

// TestTheDerivedDurationIsReported covers the number a supervisor compares against the episode's target.
//
// The handler has no duration field, so the value in the result is the one the SERVICE derived from
// the scenes. Reporting zero — or reporting the model's own sum, which is what an argument field would
// carry — would put a number into a review that no scene supports.
func TestTheDerivedDurationIsReported(t *testing.T) {
	fake := fixtureForStructureTools()
	handler := bindCreateScriptStructure(Deps{Script: scriptToolsService(fake)})
	result, err := handler(context.Background(), newScriptToolRequest("project-1", `{
		"versionId":"version-1",
		"scenes":[
			{"slugline":"INT. one - day","estimatedDurationSeconds":90},
			{"slugline":"EXT. two - night","estimatedDurationSeconds":60}
		]}`))
	if err != nil {
		t.Fatalf("CreateScriptStructure: %v", err)
	}
	view := result.(map[string]any)
	if view["estimatedDurationSeconds"] != 150 {
		t.Fatalf("the result reports duration %v, want the derived 150", view["estimatedDurationSeconds"])
	}
	if view["sceneCount"] != 2 {
		t.Fatalf("the result reports %v scenes", view["sceneCount"])
	}
	// The artifact reference is the version the write filled, so the runtime's verifier reads back a
	// row that exists.
	artifacts := view["artifacts"].([]map[string]any)
	if len(artifacts) != 1 || artifacts[0]["entityId"] != "version-1" {
		t.Fatalf("the result names %+v", artifacts)
	}
}

// fixtureForLinkTools is a fake whose link writes are recorded.
func fixtureForLinkTools() *linkRecordingFake {
	return &linkRecordingFake{
		scriptReaderFake: scriptReaderFake{
			episodes: map[string]string{"episode-1": "project-1"},
			scripts:  map[string]scriptdomain.Script{"script-1": {ID: "script-1", EpisodeID: "episode-1"}},
		},
	}
}

// linkRecordingFake records the two link sets a version write carries.
//
// It embeds the reader fake, so the reads the handlers make are answered and a read this test did not
// expect still panics rather than returning a zero value.
type linkRecordingFake struct {
	scriptReaderFake
	linkedEventIDs   []string
	linkedTreatments []scriptdomain.StrategyEventLink
}

func (f *linkRecordingFake) CreateStorySkeletonVersionWithLinks(_ context.Context, _ scriptdomain.StorySkeletonVersion, eventIDs []string) error {
	f.linkedEventIDs = append([]string(nil), eventIDs...)
	return nil
}

func (f *linkRecordingFake) CreateAdaptationStrategyVersionWithLinks(_ context.Context, _ scriptdomain.AdaptationStrategyVersion, links []scriptdomain.StrategyEventLink) error {
	f.linkedTreatments = append([]scriptdomain.StrategyEventLink(nil), links...)
	return nil
}

// The reads the two write paths make besides the link write itself, so the fixture can write a first
// version of each. Each is here because the fake's embedded interface is nil and the first call that
// was missing panicked — which is the pattern working: a read this test did not expect could not
// silently return a zero value that reads like an empty project.
func (f *linkRecordingFake) MaxStorySkeletonVersionNumber(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (f *linkRecordingFake) MaxAdaptationStrategyVersionNumber(_ context.Context, _ string) (int, error) {
	return 0, nil
}

// MissingStoryEventIDs answers "which of these events does this project have".
//
// It reports that every event EXISTS, which is what this test needs: the question here is whether the
// selection reaches the service, and a reference failure would refuse the write before the link path
// ran. The reference check has its own tests in the script package.
func (f *linkRecordingFake) MissingStoryEventIDs(_ context.Context, _ string, _ []string) ([]string, error) {
	return nil, nil
}

func (f *linkRecordingFake) MissingStoryEntityIDs(_ context.Context, _ string, _ []string) ([]string, error) {
	return nil, nil
}
