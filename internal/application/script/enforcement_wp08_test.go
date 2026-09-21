package script

import (
	"context"
	"strings"
	"testing"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// enforcement_wp08_test.go covers the guards that the mutation passes showed were UNTESTED.
//
// Two runs over WP-08's enforcement reported survivors, and these are the tests for them. Each one
// is here because a plausible defect in that code left the whole suite green — which is the
// definition of a check nobody is keeping.

// TestCompareFieldSetsRefusesAFieldItCannotCompare covers the branch that keeps a lock from being a
// decoration.
//
// The comparison refuses when a locked field is absent from either side's values, and the point of
// that branch is the case a future contributor creates: a field added to a family's lock vocabulary
// and forgotten in its comparison. Without the refusal the lock would sit in the table reading as a
// protection while nothing checked it — worse than no lock, because a reviewer trusts it.
func TestCompareFieldSetsRefusesAFieldItCannotCompare(t *testing.T) {
	lock := scriptdomain.FieldLock{
		VersionID: "v1", Family: scriptdomain.FamilyStorySkeleton,
		Field: scriptdomain.LockSkeletonEndingHook, LockedBy: "user-1",
	}
	hook := fieldValues{scriptdomain.LockSkeletonEndingHook: "the hook"}
	// A map that carries a DIFFERENT field, which is how "the comparison does not cover it" is
	// expressed without a nil map: the lookups still fail, but the map itself is present.
	other := fieldValues{scriptdomain.LockSkeletonClimax: "the climax"}

	// Both sides carry the field: a difference is refused and an equality is not.
	if err := compareFieldSets([]scriptdomain.FieldLock{lock}, scriptdomain.FamilyStorySkeleton,
		hook, fieldValues{scriptdomain.LockSkeletonEndingHook: "rewritten"}); err == nil {
		t.Fatal("a changed locked field was accepted")
	}
	if err := compareFieldSets([]scriptdomain.FieldLock{lock}, scriptdomain.FamilyStorySkeleton,
		hook, hook); err != nil {
		t.Fatalf("an unchanged locked field was refused: %v", err)
	}
	// The field absent from the OLD side is refused, not skipped — and the MESSAGE is asserted, not
	// only the refusal.
	//
	// That precision is the mutation pass's finding: weakening `||` to `&&` still refused these two
	// cases, because an absent value reads as an empty string and the value comparison then failed on
	// its own. The write was refused either way, so a test asserting only `err != nil` could not tell
	// the two apart — but the defect is real, because the message it produced said the field "was
	// changed" when it had never been compared. A refusal that describes the wrong problem sends a
	// model to rewrite a field that was never in question.
	cases := []struct {
		name   string
		before fieldValues
		after  fieldValues
	}{
		{"absent from the base", other, hook},
		{"absent from the revision", hook, other},
		{"absent from both", other, other},
	}
	for _, testCase := range cases {
		err := compareFieldSets([]scriptdomain.FieldLock{lock}, scriptdomain.FamilyStorySkeleton,
			testCase.before, testCase.after)
		if err == nil {
			t.Fatalf("%s: the lock was skipped", testCase.name)
		}
		if strings.Contains(err.Error(), "was changed") {
			t.Fatalf("%s: the refusal says the field was changed, but it was never compared: %v",
				testCase.name, err)
		}
		if !strings.Contains(err.Error(), "cannot be verified") {
			t.Fatalf("%s: the refusal reads %q", testCase.name, err)
		}
	}
	// An empty string IS a value, so returning a locked field empty is a CHANGE and refused as one.
	err := compareFieldSets([]scriptdomain.FieldLock{lock}, scriptdomain.FamilyStorySkeleton,
		hook, fieldValues{scriptdomain.LockSkeletonEndingHook: ""})
	if err == nil {
		t.Fatal("a locked field returned empty was accepted")
	}
	if !strings.Contains(err.Error(), "was changed") {
		t.Fatalf("the refusal reads %q, want a changed-field refusal", err)
	}
	// A lock of another family is refused rather than skipped.
	foreign := lock
	foreign.Family = scriptdomain.FamilyScript
	if err := compareFieldSets([]scriptdomain.FieldLock{foreign}, scriptdomain.FamilyStorySkeleton,
		hook, hook); err == nil {
		t.Fatal("a lock of another family was skipped")
	}
	// And an empty lock set compares nothing rather than refusing, which is what makes a first draft
	// and an unlocked revision writable.
	if err := compareFieldSets(nil, scriptdomain.FamilyScript, nil, nil); err != nil {
		t.Fatalf("an empty lock set was refused: %v", err)
	}
}

// TestAssertLocksCoveredRefusesAnUnclaimedField covers the guard the whole partition rests on.
//
// A script version's locks are split between two comparisons, so the coverage list is what states
// their union. This asserts the refusal directly rather than through a write path, because a write
// path can only reach it with a lock the vocabulary already admits.
func TestAssertLocksCoveredRefusesAnUnclaimedField(t *testing.T) {
	claimed := []scriptdomain.LockableField{scriptdomain.LockScriptSummary}
	// A claimed field passes.
	if err := assertLocksCovered([]scriptdomain.FieldLock{{
		VersionID: "v1", Family: scriptdomain.FamilyScript, Field: scriptdomain.LockScriptSummary,
	}}, scriptdomain.FamilyScript, claimed); err != nil {
		t.Fatalf("a claimed field was refused: %v", err)
	}
	// An unclaimed one is refused: this is the lock that would otherwise enforce nothing.
	err := assertLocksCovered([]scriptdomain.FieldLock{{
		VersionID: "v1", Family: scriptdomain.FamilyScript, Field: scriptdomain.LockScriptStructure,
	}}, scriptdomain.FamilyScript, claimed)
	if err == nil {
		t.Fatal("a lock no comparison claims was accepted")
	}
	if !strings.Contains(err.Error(), "cannot be verified") {
		t.Fatalf("the refusal reads %q", err)
	}
	// A lock of another family is refused even when its FIELD is claimed, so the check cannot be
	// satisfied by a name collision across two families' vocabularies.
	err = assertLocksCovered([]scriptdomain.FieldLock{{
		VersionID: "v1", Family: scriptdomain.FamilyStorySkeleton, Field: scriptdomain.LockScriptSummary,
	}}, scriptdomain.FamilyScript, claimed)
	if err == nil {
		t.Fatal("a lock of another family was accepted because its field name matched")
	}
	// An empty lock set needs no coverage, which is what makes a first draft writable.
	if err := assertLocksCovered(nil, scriptdomain.FamilyScript, nil); err != nil {
		t.Fatalf("an empty lock set was refused: %v", err)
	}
}

// TestTheScriptCoverageListCoversTheVocabulary states the property that makes the split safe.
//
// The script family's locks are compared in two places, so its coverage list and its vocabulary are
// two declarations that must agree. A vocabulary entry added without a comparison would leave a lock
// that nothing checks; this is the assertion that fails when that happens.
func TestTheScriptCoverageListCoversTheVocabulary(t *testing.T) {
	vocabulary := scriptdomain.LockableFields(scriptdomain.FamilyScript)
	// The list the write path passes, written here rather than read from the code: if the write path
	// stops claiming one, this comparison is what notices.
	claimed := map[scriptdomain.LockableField]bool{
		scriptdomain.LockScriptSummary:   true,
		scriptdomain.LockScriptStructure: true,
	}
	for _, field := range vocabulary {
		if !claimed[field] {
			t.Errorf("the script vocabulary admits %s and the write path claims no comparison for it", field)
		}
	}
	if len(claimed) != len(vocabulary) {
		t.Errorf("the write path claims %d fields and the vocabulary has %d", len(claimed), len(vocabulary))
	}
}

// TestTheSkeletonAndStrategyComparisonsCarryEveryLockableField covers the two families whose coverage
// is derived from the vocabulary rather than listed.
//
// Both write paths pass `LockableFields(family)` to `assertLocksCovered`, so this asserts the other
// half: that the VALUES each comparison builds carry every one of those fields. A field in the
// vocabulary and absent from the value maps refuses every write — loud, which is the right failure —
// but it is still a bug, and this is what catches it without waiting for a user.
func TestTheSkeletonAndStrategyComparisonsCarryEveryLockableField(t *testing.T) {
	ctx := context.Background()

	store := newMemoryStore()
	service := newTestService(store)
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-1")
	base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", CoreConflict: "conflict",
		TurningPointsJSON: `["a"]`, Climax: "climax", EndingHook: "ending",
		SelectedEventIDs: []string{"event-1"},
	})
	if err != nil {
		t.Fatalf("the base skeleton: %v", err)
	}
	for _, field := range scriptdomain.LockableFields(scriptdomain.FamilyStorySkeleton) {
		if err := service.LockScriptField(ctx, LockScriptFieldRequest{
			VersionID: base.ID, Family: scriptdomain.FamilyStorySkeleton, Field: field, LockedBy: "user-1",
		}); err != nil {
			t.Fatalf("locking %s: %v", field, err)
		}
	}
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook", CoreConflict: "conflict",
		TurningPointsJSON: `["a"]`, Climax: "climax", EndingHook: "ending",
		SelectedEventIDs: []string{"event-1"},
	}); err != nil {
		t.Fatalf("a skeleton restating every locked field was refused, so a comparison omits one: %v", err)
	}

	strategyStore := newMemoryStore()
	strategyService := newTestService(strategyStore)
	strategyEpisode := seedEpisode(t, strategyService)
	seedStoryGraph(strategyStore, "project-1", "event-1")
	strategyBase, err := strategyService.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: strategyEpisode.ID, StrategySummary: "balanced",
		AdaptationMode: scriptdomain.AdaptationBalanced, OriginalAdditions: "none",
		Rationale: "because", Risks: "few",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
		},
	})
	if err != nil {
		t.Fatalf("the base strategy: %v", err)
	}
	for _, field := range scriptdomain.LockableFields(scriptdomain.FamilyAdaptationStrategy) {
		if err := strategyService.LockScriptField(ctx, LockScriptFieldRequest{
			VersionID: strategyBase.ID, Family: scriptdomain.FamilyAdaptationStrategy, Field: field,
			LockedBy: "user-1",
		}); err != nil {
			t.Fatalf("locking %s: %v", field, err)
		}
	}
	if _, err := strategyService.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: strategyEpisode.ID, BasedOnVersionID: strategyBase.ID, StrategySummary: "balanced",
		AdaptationMode: scriptdomain.AdaptationBalanced, OriginalAdditions: "none",
		Rationale: "because", Risks: "few",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
		},
	}); err != nil {
		t.Fatalf("a strategy restating every locked field was refused, so a comparison omits one: %v", err)
	}
}

// TestTheScriptSummaryLockIsEnforced is the routing assertion for the one family whose locks are
// compared in two places.
//
// A mutation that routed the summary lock to no comparison left every test green until this one: the
// structure lock had its own test, and the summary was assumed to travel the same path. It does not
// — it goes through the generic comparison, and the filter that selects it is what this asserts.
func TestTheScriptSummaryLockIsEnforced(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	base := seedScriptVersion(t, service)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: base.ScriptID, ScriptVersionID: base.ID, Structure: structureFor(base.ID),
		Summary: "the original summary",
	}); err != nil {
		t.Fatalf("the base version: %v", err)
	}
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: base.ID, Family: scriptdomain.FamilyScript,
		Field: scriptdomain.LockScriptSummary, LockedBy: "user-1",
	}); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	// A revision that restates the summary is accepted.
	next, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: base.ScriptID, BasedOnVersionID: base.ID,
		StorySkeletonVersionID: "skeleton-x", AdaptationStrategyVersionID: "strategy-x",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: next.ScriptID, ScriptVersionID: next.ID, BasedOnVersionID: base.ID,
		Structure: structureFor(next.ID), Summary: "the original summary",
	}); err != nil {
		t.Fatalf("a revision that kept the locked summary was refused: %v", err)
	}
	// One that rewrites it is refused, which is the assertion a mis-routed lock would miss.
	third, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: base.ScriptID, BasedOnVersionID: base.ID,
		StorySkeletonVersionID: "skeleton-x", AdaptationStrategyVersionID: "strategy-x",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	_, err = service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: third.ScriptID, ScriptVersionID: third.ID, BasedOnVersionID: base.ID,
		Structure: structureFor(third.ID), Summary: "a rewritten summary",
	})
	if err == nil {
		t.Fatal("a revision that rewrote the locked summary was accepted")
	}
	if !strings.Contains(err.Error(), "summary") {
		t.Fatalf("the refusal reads %q, which does not name the field", err)
	}
	// And the EMPTY summary is accepted, because an omitted summary means "leave it alone": the row
	// keeps the pinned value, so refusing would make the lock unsatisfiable for a FIX that did not
	// restate it — which is the shape AC-SCRIPT-002's revision has.
	fourth, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: base.ScriptID, BasedOnVersionID: base.ID,
		StorySkeletonVersionID: "skeleton-x", AdaptationStrategyVersionID: "strategy-x",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: fourth.ScriptID, ScriptVersionID: fourth.ID, BasedOnVersionID: base.ID,
		Structure: structureFor(fourth.ID),
	}); err != nil {
		t.Fatalf("a revision that left the locked summary alone was refused: %v", err)
	}
}

// TestASelectionIsASetNotAnOrder covers joinedEvents' normalisation.
//
// A skeleton selects WHICH events are in the episode; the order a viewer sees is the adaptation's,
// which §7.5 gives the strategy a link table with ordinals for. So a revision that writes the same
// events in a different order has not changed the selection, and refusing it would make the lock
// unsatisfiable for a reason no user asked for.
func TestASelectionIsASetNotAnOrder(t *testing.T) {
	ctx := context.Background()
	// The function itself, so the property is asserted directly rather than through a write path that
	// could be refusing for another reason.
	if joinedEvents([]string{"a", "b"}) != joinedEvents([]string{"b", "a"}) {
		t.Fatal("the same events in another order render differently")
	}
	if joinedEvents([]string{"a", "b"}) == joinedEvents([]string{"a", "c"}) {
		t.Fatal("different selections render the same")
	}
	if joinedEvents(nil) != "" {
		t.Fatalf("an empty selection renders as %q", joinedEvents(nil))
	}
	// And through the write path: the base selects two events, the revision lists them backwards.
	store := newMemoryStore()
	service := newTestService(store)
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-1", "event-2")
	base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{"event-1", "event-2"},
	})
	if err != nil {
		t.Fatalf("the base skeleton: %v", err)
	}
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: base.ID, Family: scriptdomain.FamilyStorySkeleton,
		Field: scriptdomain.LockSkeletonSelectedEvents, LockedBy: "user-1",
	}); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{"event-2", "event-1"},
	}); err != nil {
		t.Fatalf("the same selection in another order was refused: %v", err)
	}
	// A genuinely different selection is still refused, so the normalisation is not a blanket pass.
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{"event-1"},
	}); err == nil {
		t.Fatal("a changed selection was accepted")
	}
}

// TestRenderTreatmentsCarriesBothTheEventAndItsTreatment covers the strategy's comparable value.
//
// Comparing only the event ids would let a revision flip "removed" to "retained" without a refusal —
// the single most consequential change a strategy can make, and the one a lock on
// `mergedEventGroups` exists to prevent.
func TestRenderTreatmentsCarriesBothTheEventAndItsTreatment(t *testing.T) {
	retained := renderTreatments([]scriptdomain.StrategyEventLink{
		{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
	})
	removed := renderTreatments([]scriptdomain.StrategyEventLink{
		{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRemoved},
	})
	if retained == removed {
		t.Fatal("the same event with opposite treatments renders the same")
	}
	if !strings.Contains(retained, "event-1") || !strings.Contains(retained, "retained") {
		t.Fatalf("the rendering %q names neither the event nor its treatment", retained)
	}
	first := renderTreatments([]scriptdomain.StrategyEventLink{
		{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
		{StoryEventID: "event-2", Treatment: scriptdomain.TreatmentRemoved},
	})
	second := renderTreatments([]scriptdomain.StrategyEventLink{
		{StoryEventID: "event-2", Treatment: scriptdomain.TreatmentRemoved},
		{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
	})
	if first != second {
		t.Fatalf("the same treatments in another order render differently:\n%s\n%s", first, second)
	}
	if renderTreatments(nil) != "" {
		t.Fatalf("an empty treatment set renders as %q", renderTreatments(nil))
	}
}

// TestAnEventWithNoTreatmentIsRefused covers the one vocabulary with no sensible default.
//
// A line type defaults to dialogue and an interior marking to OTHER because those are the common
// cases an omission implies. A treatment is different: "retained" and "removed" are opposite
// decisions about a story event, and neither follows from an absence — defaulting would attribute a
// decision to a model that never made one.
func TestAnEventWithNoTreatmentIsRefused(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := newTestService(store)
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-1")
	_, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",
		EventLinks: []scriptdomain.StrategyEventLink{{StoryEventID: "event-1"}},
	})
	if err == nil {
		t.Fatal("an event with no treatment was accepted")
	}
	if !strings.Contains(err.Error(), "treatment") {
		t.Fatalf("the refusal reads %q, which does not mention the treatment", err)
	}
	// An unknown treatment is refused by the domain vocabulary rather than reaching SQL.
	if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.EventTreatment("rewritten")},
		},
	}); err == nil {
		t.Fatal("an invented treatment was accepted")
	}
}

// TestAPayloadCarriesEitherAStructureOrADraft covers the refusal that keeps two payloads from being
// merged into one nobody chose.
//
// Both fields populated would leave the question of which one wins to an unstated rule, and neither
// would write a version with no content — which reads to a reviewer as a version that exists. Both
// are refusals rather than a preference.
func TestAPayloadCarriesEitherAStructureOrADraft(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()

	// Neither: refused. A version with no scenes is not a smaller version, it is an artifact nobody
	// can judge — including its own duration, which is a sum over the scenes it does not have.
	empty := seedScriptVersion(t, service)
	_, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: empty.ScriptID, ScriptVersionID: empty.ID,
	})
	if err == nil {
		t.Fatal("a version with no content was accepted")
	}
	// The message is the DOMAIN validator's, because "no scenes" is a rule about a structure rather
	// than about the request's shape: the materialiser builds an empty structure and the validator
	// refuses it. It was a second check in the materialiser, stating the same rule in a place that
	// could drift from the first; the mutation pass is what showed the two were redundant.
	if !strings.Contains(err.Error(), "at least one scene") {
		t.Fatalf("the refusal reads %q", err)
	}

	// Both: refused.
	both := seedAnotherScriptVersion(t, service, empty.ScriptID)
	_, err = service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: both.ScriptID, ScriptVersionID: both.ID,
		Structure: structureFor(both.ID),
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{
			{Slugline: "INT. elsewhere - \u65e5", InteriorExterior: scriptdomain.InteriorINT},
		}},
	})
	if err == nil {
		t.Fatal("a request carrying both a structure and a draft was accepted")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Fatalf("the refusal reads %q", err)
	}
	// And nothing was written by either refusal.
	for _, id := range []string{empty.ID, both.ID} {
		structure, readErr := service.GetScriptStructure(ctx, id)
		if readErr != nil {
			t.Fatalf("GetScriptStructure: %v", readErr)
		}
		if len(structure.Scenes) != 0 {
			t.Fatalf("version %s gained %d scenes from a refused write", id, len(structure.Scenes))
		}
	}
}

// TestTheProjectBoundaryIsEnforcedOnReferences covers the cross-project leak the check exists for.
//
// A script must not cite a story event that belongs to ANOTHER project: the citation would resolve
// for the writer and not for the reader, and it would tell a user that an identifier exists which
// they cannot see. The double keys its graph by project precisely so this is representable.
func TestTheProjectBoundaryIsEnforcedOnReferences(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)
	// The event exists — but in another project.
	store.storyEvents = map[string]map[string]string{
		"drama-project": {"event-1": "drama-project"},
		"other-project": {"event-elsewhere": "other-project"},
	}
	// The event of THIS project is accepted.
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: version.ScriptID, ScriptVersionID: version.ID, ProjectID: "drama-project",
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
			Slugline: "INT. \u6e21\u53e3 - \u65e5", InteriorExterior: scriptdomain.InteriorINT,
			SourceStoryEventID: "event-1",
		}}},
	}); err != nil {
		t.Fatalf("an event of this project was refused: %v", err)
	}
	// The other project's event is refused even though the row exists.
	second := seedAnotherScriptVersion(t, service, version.ScriptID)
	_, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: second.ScriptID, ScriptVersionID: second.ID, ProjectID: "drama-project",
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
			Slugline: "INT. elsewhere - \u591c", InteriorExterior: scriptdomain.InteriorINT,
			SourceStoryEventID: "event-elsewhere",
		}}},
	})
	if err == nil {
		t.Fatal("an event of another project was accepted")
	}
	if !strings.Contains(err.Error(), "event-elsewhere") {
		t.Fatalf("the refusal reads %q, which does not name the identifier", err)
	}
}

// TestAStructureWithNoProjectSkipsTheReferenceCheck covers the boundary of the reference check.
//
// The check needs a project: references are project-scoped, and a request that states none has no
// graph to check against. That is the user's own edit path, where the UI supplies identifiers it
// read from the project. The skip is asserted so the behaviour is recorded rather than discovered,
// and the SAME payload with a project is refused — which is what makes it a boundary rather than a
// hole.
func TestAStructureWithNoProjectSkipsTheReferenceCheck(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)
	// The store has NO story graph at all, so the check would refuse if it ran with a project.
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: version.ScriptID, ScriptVersionID: version.ID,
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
			Slugline: "INT. \u6e21\u53e3 - \u65e5", InteriorExterior: scriptdomain.InteriorINT,
			SourceStoryEventID: "event-nothing", LocationEntityID: "entity-nothing",
		}}},
	}); err != nil {
		t.Fatalf("a write with no project was refused: %v", err)
	}
	other := seedAnotherScriptVersion(t, service, version.ScriptID)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: other.ScriptID, ScriptVersionID: other.ID, ProjectID: "project-1",
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
			Slugline: "INT. \u6e21\u53e3 - \u65e5", InteriorExterior: scriptdomain.InteriorINT,
			SourceStoryEventID: "event-nothing",
		}}},
	}); err == nil {
		t.Fatal("the same payload with a project was accepted, so the check is broken rather than skipped")
	}
}
