package script

import (
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// These tests cover the whole-content payload, its gapless-ordinal rule, the lock vocabulary and
// the diff. They are pure: no database, no clock, no identifier source.

// sampleStructure builds a valid two-scene structure with lines and shots.
func sampleStructure() ScriptStructure {
	return ScriptStructure{
		ScriptVersionID: "version-1",
		Scenes: []SceneStructure{
			{
				Scene: Scene{
					ID: "scene-1", ScriptVersionID: "version-1", Ordinal: 1,
					SceneNumber: "1", Slugline: "INT. 望江楼 - 日", InteriorExterior: InteriorINT,
					LocationEntityID: "loc-1", TimeOfDay: "日", Summary: "白掌柜念出那个名字。",
					DramaticGoal: "让观众知道铜牌的来历", EstimatedDurationSeconds: 90,
					SourceStoryEventID: "event-1",
				},
				DialogueLines: []DialogueLine{
					{ID: "line-1", SceneID: "scene-1", Ordinal: 1, Type: LineDialogue,
						CharacterEntityID: "char-1", Text: "这牌子不是你的。"},
					{ID: "line-2", SceneID: "scene-1", Ordinal: 2, Type: LineAction,
						Text: "雾气漫上来。"},
				},
				Shots: []Shot{
					{ID: "shot-1", SceneID: "scene-1", Ordinal: 1, ShotSize: "wide",
						VisualDescription: "河面起雾。", Status: versioning.StatusDraft},
				},
			},
			{
				Scene: Scene{
					ID: "scene-2", ScriptVersionID: "version-1", Ordinal: 2,
					SceneNumber: "2", Slugline: "EXT. 渡口 - 夜", InteriorExterior: InteriorEXT,
					TimeOfDay: "夜", EstimatedDurationSeconds: 60,
				},
				DialogueLines: []DialogueLine{},
				Shots:         []Shot{},
			},
		},
	}
}

// TestScriptStructureAcceptsAWholeVersion is the positive half.
func TestScriptStructureAcceptsAWholeVersion(t *testing.T) {
	structure := sampleStructure()
	if err := structure.Validate(); err != nil {
		t.Fatalf("a valid structure was refused: %v", err)
	}
	// The duration is SUMMED from the scenes rather than carried, which is the property
	// AGENT_CONTRACTS section 17 puts in the code's column.
	if total := structure.TotalDurationSeconds(); total != 150 {
		t.Fatalf("the summed duration is %d, want 150", total)
	}
}

// TestScriptStructureRefusesAGapInTheOrdinals is the rule a JSON Schema cannot express.
//
// A structure whose scene ordinals are 1 and 3 describes a version with a hole: the database
// would accept both rows, a reader would see two scenes, and nothing would say the second is
// missing. The check compares each ordinal against its POSITION, which is the only form that
// catches both a gap and a repeat.
func TestScriptStructureRefusesAGapInTheOrdinals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ScriptStructure)
	}{
		{"a gap", func(s *ScriptStructure) { s.Scenes[1].Ordinal = 3 }},
		{"a repeat", func(s *ScriptStructure) { s.Scenes[1].Ordinal = 1 }},
		{"a zero", func(s *ScriptStructure) { s.Scenes[0].Ordinal = 0 }},
		{"a line gap", func(s *ScriptStructure) { s.Scenes[0].DialogueLines[1].Ordinal = 5 }},
		{"a shot gap", func(s *ScriptStructure) {
			s.Scenes[0].Shots = append(s.Scenes[0].Shots, Shot{
				ID: "shot-3", SceneID: "scene-1", Ordinal: 3, Status: versioning.StatusDraft,
			})
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			structure := sampleStructure()
			testCase.mutate(&structure)
			if err := structure.Validate(); err == nil {
				t.Fatal("a structure with an ordinal fault was accepted")
			}
		})
	}
}

// TestScriptStructureRefusesAMisnestedChild covers the coherence rule.
//
// A line nested under scene 1 that NAMES scene 2 would be written into scene 1 by the write
// path while its own field says otherwise: the document would describe one thing and the
// database would hold another, and a read-back would disagree with both.
func TestScriptStructureRefusesAMisnestedChild(t *testing.T) {
	structure := sampleStructure()
	structure.Scenes[0].DialogueLines[0].SceneID = "scene-2"
	if err := structure.Validate(); err == nil {
		t.Fatal("a line naming another scene was accepted")
	}
	structure = sampleStructure()
	structure.Scenes[0].Shots[0].SceneID = "scene-2"
	if err := structure.Validate(); err == nil {
		t.Fatal("a shot naming another scene was accepted")
	}
}

// TestScriptStructureRefusesAnEmptyOrOversizedVersion covers the two bounds.
func TestScriptStructureRefusesAnEmptyOrOversizedVersion(t *testing.T) {
	empty := ScriptStructure{ScriptVersionID: "version-1"}
	if err := empty.Validate(); err == nil {
		t.Fatal("a structure with no scenes was accepted")
	}
	// The bound is on the COUNT rather than on the total size, and it is refused rather than
	// truncated: a truncated script would look complete.
	huge := ScriptStructure{ScriptVersionID: "version-1"}
	for index := 0; index <= MaxScenesPerVersion; index++ {
		huge.Scenes = append(huge.Scenes, SceneStructure{
			Scene: Scene{ID: "s", ScriptVersionID: "version-1", Ordinal: index + 1,
				InteriorExterior: InteriorINT},
		})
	}
	if err := huge.Validate(); err == nil {
		t.Fatal("a structure over the scene bound was accepted")
	}
	// And a structure with no version is refused, because its content could not be filed.
	orphan := sampleStructure()
	orphan.ScriptVersionID = ""
	if err := orphan.Validate(); err == nil {
		t.Fatal("a structure with no version was accepted")
	}
}

// TestLockVocabularyIsClosedPerFamily covers the lock set.
//
// The set is per FAMILY, and the test asserts the cross-product is refused: a strategy field
// locked on a skeleton must fail, or a caller could record a lock no diff would ever report —
// a protection that reads as being in force and is not.
func TestLockVocabularyIsClosedPerFamily(t *testing.T) {
	for _, family := range VersionFamilies() {
		fields := LockableFields(family)
		if len(fields) == 0 {
			t.Fatalf("the family %s has no lockable fields", family)
		}
		for _, field := range fields {
			if !IsLockableField(family, field) {
				t.Fatalf("%s rejects its own field %q", family, field)
			}
			lock := FieldLock{VersionID: "v1", Family: family, Field: field}
			if err := lock.Validate(); err != nil {
				t.Fatalf("a lock on %s.%s was refused: %v", family, field, err)
			}
		}
	}
	// A field from another family is refused.
	if IsLockableField(FamilyStorySkeleton, LockStrategyRationale) {
		t.Fatal("a strategy field is lockable on a skeleton")
	}
	if IsLockableField(FamilyScript, LockSkeletonEndingHook) {
		t.Fatal("a skeleton field is lockable on a script version")
	}
	// And an invented field is refused everywhere.
	for _, family := range VersionFamilies() {
		if IsLockableField(family, LockableField("whatever")) {
			t.Fatalf("%s accepts an invented field", family)
		}
	}
	// A lock with no version or an unknown family is refused.
	if err := (FieldLock{Family: FamilyScript, Field: LockScriptSummary}).Validate(); err == nil {
		t.Fatal("a lock with no version was accepted")
	}
	if err := (FieldLock{VersionID: "v1", Family: "nonsense", Field: LockScriptSummary}).Validate(); err == nil {
		t.Fatal("a lock with an unknown family was accepted")
	}
	// A VALID family with a field from another family is refused, and this case is the one that
	// reaches the field check at all: the two above are refused by the version check and the
	// family check, which come first. A mutation that disabled the field check left this file's
	// tests green until this assertion existed.
	if err := (FieldLock{VersionID: "v1", Family: FamilyScript, Field: LockSkeletonEndingHook}).Validate(); err == nil {
		t.Fatal("a skeleton field was lockable on a script version")
	}
	if err := (FieldLock{VersionID: "v1", Family: FamilyStorySkeleton, Field: LockStrategyRisks}).Validate(); err == nil {
		t.Fatal("a strategy field was lockable on a skeleton")
	}
	// And an invented field on a valid family, which is the third shape the check refuses.
	if err := (FieldLock{VersionID: "v1", Family: FamilyScript, Field: LockableField("invented")}).Validate(); err == nil {
		t.Fatal("an invented field was lockable")
	}
}

// TestDiffSkeletonReportsWhatChanged covers the field-level diff.
//
// The unchanged fields are deliberately NOT reported: a reader asking "what did the revision
// touch" is not helped by the five fields the model left alone.
func TestDiffSkeletonReportsWhatChanged(t *testing.T) {
	before := StorySkeletonVersion{
		ID: "v1", OpeningHook: "same", CoreConflict: "same",
		TurningPointsJSON: `["a"]`, Climax: "same", EndingHook: "",
		EstimatedDurationSeconds: 120,
	}
	after := before
	after.ID = "v2"
	after.EndingHook = "The river takes the token."
	after.TurningPointsJSON = `["a","b"]`

	diff := DiffSkeleton(before, after)
	if diff.Family != FamilyStorySkeleton || diff.FromID != "v1" || diff.ToID != "v2" {
		t.Fatalf("the diff names %+v", diff)
	}
	fields := map[string]FieldChange{}
	for _, change := range diff.Fields {
		fields[change.Field] = change
	}
	if _, present := fields["openingHook"]; present {
		t.Fatal("an unchanged field was reported")
	}
	if _, present := fields["climax"]; present {
		t.Fatal("an unchanged field was reported")
	}
	hook, present := fields["endingHook"]
	if !present {
		t.Fatal("the changed ending hook was not reported")
	}
	if hook.Before != "" || hook.After != "The river takes the token." {
		t.Fatalf("the hook change reads %+v", hook)
	}
	if _, present := fields["turningPoints"]; !present {
		t.Fatal("the changed turning points were not reported")
	}
	// And a revision that changed nothing says so.
	same := DiffSkeleton(before, before)
	if same.Changed() {
		t.Fatal("a comparison of a version with itself reports a change")
	}
}

// TestDiffScriptStructureIsPositional covers the scene diff.
//
// The cases are the four a revision can produce, and the removals are checked explicitly
// because a diff that only walked the newer side would silently lose them — which is exactly
// what AC-SCRIPT-003's "原版本保留" is about.
func TestDiffScriptStructureIsPositional(t *testing.T) {
	before := sampleStructure()
	after := sampleStructure()
	after.ScriptVersionID = "version-2"
	// Scene 1 is modified (its goal), scene 2 is unchanged, and scene 3 is new.
	after.Scenes[0].DramaticGoal = "换一个目标"
	after.Scenes = append(after.Scenes, SceneStructure{
		Scene: Scene{ID: "scene-3", ScriptVersionID: "version-2", Ordinal: 3,
			InteriorExterior: InteriorINT, EstimatedDurationSeconds: 30},
	})

	diff := DiffScriptStructure(before, after)
	if diff.FromID != "version-1" || diff.ToID != "version-2" {
		t.Fatalf("the diff names %v -> %v", diff.FromID, diff.ToID)
	}
	if diff.Modified != 1 {
		t.Fatalf("the diff reports %d modified", diff.Modified)
	}
	if diff.Added != 1 {
		t.Fatalf("the diff reports %d added", diff.Added)
	}
	if diff.Unchanged != 1 {
		t.Fatalf("the diff reports %d unchanged", diff.Unchanged)
	}
	if diff.Removed != 0 {
		t.Fatalf("the diff reports %d removed", diff.Removed)
	}
	if !diff.Changed() {
		t.Fatal("a diff with changes reports none")
	}

	// The removal direction: the newer version lost a scene.
	smaller := sampleStructure()
	smaller.ScriptVersionID = "version-3"
	smaller.Scenes = smaller.Scenes[:1]
	removal := DiffScriptStructure(before, smaller)
	if removal.Removed != 1 {
		t.Fatalf("the removed direction reports %d removed", removal.Removed)
	}
	if removal.Unchanged != 1 {
		t.Fatalf("the removed direction reports %d unchanged", removal.Unchanged)
	}
	// A revision whose ONLY change is a deletion still changed. This assertion is separate from
	// the one above because Changed() could ignore the removal count and the case above would
	// still pass — its diff also carries an addition.
	if !removal.Changed() {
		t.Fatal("a diff whose only change is a removal reports no change")
	}
	// The same for a diff whose only difference is a removal AND a modification, so the
	// removal term cannot be dropped while the others carry the answer.
	pureRemoval := DiffScriptStructure(before, ScriptStructure{
		ScriptVersionID: "version-5",
		Scenes:          before.Scenes[:1],
	})
	if !pureRemoval.Changed() {
		t.Fatal("a removal-only diff reports no change")
	}
}

// TestDiffScriptStructureReportsNestedChanges covers the line and shot diffs.
//
// A scene whose own fields are identical but whose LINES changed is a modified scene, and the
// nested field name says which line and which field. The lock is part of what is compared, so a
// reader can see a locked line that changed — which is the shape AC-SCRIPT-002's failure would
// take.
func TestDiffScriptStructureReportsNestedChanges(t *testing.T) {
	before := sampleStructure()
	after := sampleStructure()
	after.ScriptVersionID = "version-2"
	after.Scenes[0].DialogueLines[0].Text = "这牌子是我的。"
	after.Scenes[0].DialogueLines[0].Locked = true

	diff := DiffScriptStructure(before, after)
	if diff.Modified != 1 || diff.Unchanged != 1 {
		t.Fatalf("the diff reports %d modified and %d unchanged", diff.Modified, diff.Unchanged)
	}
	var sceneChange ItemChange
	for _, item := range diff.Items {
		if item.Ordinal == 1 {
			sceneChange = item
		}
	}
	if sceneChange.Kind != ChangeModified {
		t.Fatalf("the first scene is %q", sceneChange.Kind)
	}
	fields := map[string]FieldChange{}
	for _, change := range sceneChange.Fields {
		fields[change.Field] = change
	}
	text, present := fields["line.1.text"]
	if !present {
		t.Fatalf("the changed line text was not reported: %v", fields)
	}
	if text.Before != "这牌子不是你的。" || text.After != "这牌子是我的。" {
		t.Fatalf("the line change reads %+v", text)
	}
	if _, present := fields["line.1.locked"]; !present {
		t.Fatal("the lock change was not reported")
	}
	// A line that only appears in the newer version reads as a new field rather than as a
	// modification of an existing one.
	after.Scenes[0].DialogueLines = append(after.Scenes[0].DialogueLines, DialogueLine{
		ID: "line-3", SceneID: "scene-1", Ordinal: 3, Type: LineDialogue, Text: "新台词",
	})
	added := DiffScriptStructure(before, after)
	var sawAddedLine bool
	for _, item := range added.Items {
		for _, change := range item.Fields {
			if change.Field == "line.3" && change.After == "新台词" {
				sawAddedLine = true
			}
		}
	}
	if !sawAddedLine {
		t.Fatal("a line only the newer version has was not reported")
	}
	// And a line the newer version dropped reads as a removal.
	dropped := sampleStructure()
	dropped.ScriptVersionID = "version-4"
	dropped.Scenes[0].DialogueLines = dropped.Scenes[0].DialogueLines[:1]
	removal := DiffScriptStructure(before, dropped)
	var sawRemovedLine bool
	for _, item := range removal.Items {
		for _, change := range item.Fields {
			if change.Field == "line.2" && change.After == "" {
				sawRemovedLine = true
			}
		}
	}
	if !sawRemovedLine {
		t.Fatal("a dropped line was not reported")
	}
}

// TestLocksEqualIgnoresSurroundingWhitespace covers the comparison the write path enforces.
//
// A model that returned the same sentence with a trailing newline has not changed the field,
// and refusing it would make a lock impossible to satisfy — so the comparison trims. The test
// asserts both directions, because a comparison that always returned true would pass the first
// and a comparison that always returned false would pass the second.
func TestLocksEqualIgnoresSurroundingWhitespace(t *testing.T) {
	if !LocksEqual(LockSkeletonEndingHook, "The river takes the token.", "  The river takes the token.\n") {
		t.Fatal("a value differing only in whitespace was reported as changed")
	}
	if LocksEqual(LockSkeletonEndingHook, "one", "two") {
		t.Fatal("two different values were reported as equal")
	}
	if LocksEqual(LockSkeletonEndingHook, "a", "") {
		t.Fatal("a value and the empty string were reported as equal")
	}
}

// TestContentIsFrozenFollowsTheSharedRule pins the delegation.
//
// The rule is section 2.5's and lives in the versioning package; this wrapper exists so the
// script write path names it in its own vocabulary. The test asserts the two agree rather than
// asserting what the rule is — the versioning package's own test does that.
func TestContentIsFrozenFollowsTheSharedRule(t *testing.T) {
	for _, status := range versioning.Statuses {
		if ContentIsFrozen(status) != versioning.IsContentFrozen(status) {
			t.Fatalf("the two disagree about %q", status)
		}
	}
	if !ContentIsFrozen(versioning.StatusApproved) {
		t.Fatal("an approved version is not frozen, so a write path would rewrite it")
	}
	if !ContentIsFrozen(versioning.StatusSuperseded) {
		t.Fatal("a superseded version is not frozen")
	}
	if ContentIsFrozen(versioning.StatusDraft) {
		t.Fatal("a draft is frozen")
	}
}

// TestLockedFieldsOfReadsASet covers the helper both the write path and the API use.
func TestLockedFieldsOfReadsASet(t *testing.T) {
	locked := LockedFieldsOf([]FieldLock{
		{VersionID: "v1", Family: FamilyStorySkeleton, Field: LockSkeletonEndingHook},
		{VersionID: "v1", Family: FamilyStorySkeleton, Field: LockSkeletonClimax},
	})
	if !locked[LockSkeletonEndingHook] || !locked[LockSkeletonClimax] {
		t.Fatalf("the set lost a lock: %v", locked)
	}
	if locked[LockSkeletonCoreConflict] {
		t.Fatal("the set invented a lock")
	}
	if len(LockedFieldsOf(nil)) != 0 {
		t.Fatal("an empty lock list produced a non-empty set")
	}
}

// TestTrimmedEqualIsWhitespaceOnly pins the helper the two above share, so a change to it is
// visible as a failure here rather than as a surprising diff.
func TestTrimmedEqualIsWhitespaceOnly(t *testing.T) {
	if !trimmedEqual(" a ", "a") {
		t.Fatal("whitespace-only differences were not ignored")
	}
	if trimmedEqual("a b", "ab") {
		t.Fatal("an inner space was ignored")
	}
	if !trimmedEqual("", "  ") {
		t.Fatal("two blank strings were reported as different")
	}
}

// TestVersionFamiliesVocabularyIsClosed covers the three-family set.
func TestVersionFamiliesVocabularyIsClosed(t *testing.T) {
	checkVocabulary(t, "VersionFamilies", VersionFamilies(), IsValidVersionFamily)
	if strings.Join([]string{string(FamilyStorySkeleton), string(FamilyAdaptationStrategy), string(FamilyScript)}, ",") !=
		"story_skeleton,adaptation_strategy,script" {
		t.Fatal("the family names changed, so a stored lock would no longer resolve")
	}
}

// TestScriptStructureRefusesOverlongScenesAndShots covers the two per-scene ceilings.
//
// They were untested: a mutation that disabled either left every test green, because the only
// oversized case in the suite was the SCENE count. A scene with a thousand lines is not a
// script, it is a document in the wrong shape, and the refusal has to exist for the same reason
// the scene ceiling does.
func TestScriptStructureRefusesOverlongScenesAndShots(t *testing.T) {
	// Lines: one over the bound.
	manyLines := sampleStructure()
	for index := 0; index <= MaxLinesPerScene; index++ {
		manyLines.Scenes[0].DialogueLines = append(manyLines.Scenes[0].DialogueLines, DialogueLine{
			ID: "l", SceneID: "scene-1", Ordinal: len(manyLines.Scenes[0].DialogueLines) + 1,
			Type: LineAction, Text: "x",
		})
	}
	if err := manyLines.Validate(); err == nil {
		t.Fatal("a scene over the line bound was accepted")
	}
	// Shots: one over the bound.
	manyShots := sampleStructure()
	for index := 0; index <= MaxShotsPerScene; index++ {
		manyShots.Scenes[0].Shots = append(manyShots.Scenes[0].Shots, Shot{
			ID: "s", SceneID: "scene-1", Ordinal: len(manyShots.Scenes[0].Shots) + 1,
			Status: versioning.StatusDraft,
		})
	}
	if err := manyShots.Validate(); err == nil {
		t.Fatal("a scene over the shot bound was accepted")
	}
	// And a scene exactly AT each bound is accepted, so the ceilings are bounds rather than
	// off-by-one refusals of the last legal value.
	atLineBound := sampleStructure()
	atLineBound.Scenes[0].DialogueLines = nil
	for index := 0; index < MaxLinesPerScene; index++ {
		atLineBound.Scenes[0].DialogueLines = append(atLineBound.Scenes[0].DialogueLines, DialogueLine{
			ID: "l", SceneID: "scene-1", Ordinal: index + 1, Type: LineAction, Text: "x",
		})
	}
	if err := atLineBound.Validate(); err != nil {
		t.Fatalf("a scene at the line bound was refused: %v", err)
	}
}

// TestDiffStrategyReportsItsOwnFields covers the second of the three diff functions.
//
// DiffStrategy had NO test, so a mutation dropping any of its six field comparisons survived.
// The other two diffs were covered, which is exactly the shape of gap a mutation run finds and
// a reading pass misses.
func TestDiffStrategyReportsItsOwnFields(t *testing.T) {
	before := AdaptationStrategyVersion{
		ID: "v1", StrategySummary: "same", AdaptationMode: AdaptationFaithful,
		MergedEventGroupsJSON: `[["e1","e2"]]`, OriginalAdditions: "same",
		Rationale: "same", Risks: "none",
	}
	after := before
	after.ID = "v2"
	after.AdaptationMode = AdaptationAggressive
	after.Risks = "the ending may not land"

	diff := DiffStrategy(before, after)
	if diff.Family != FamilyAdaptationStrategy || diff.FromID != "v1" || diff.ToID != "v2" {
		t.Fatalf("the diff names %+v", diff)
	}
	fields := map[string]FieldChange{}
	for _, change := range diff.Fields {
		fields[change.Field] = change
	}
	if len(diff.Fields) != 2 {
		t.Fatalf("the diff reports %d fields, want the two that changed", len(diff.Fields))
	}
	mode, present := fields["adaptationMode"]
	if !present {
		t.Fatal("the changed adaptation mode was not reported")
	}
	if mode.Before != string(AdaptationFaithful) || mode.After != string(AdaptationAggressive) {
		t.Fatalf("the mode change reads %+v", mode)
	}
	risks, present := fields["risks"]
	if !present {
		t.Fatal("the changed risks were not reported")
	}
	if risks.After != "the ending may not land" {
		t.Fatalf("the risk change reads %+v", risks)
	}
	// Each of the four remaining comparisons is asserted, because a mutation dropping any ONE
	// of them would leave the two above green.
	for field, want := range map[string]string{
		"strategySummary":   "a different summary",
		"mergedEventGroups": `[["e1"]]`,
		"originalAdditions": "something new",
		"rationale":         "because",
	} {
		changed := before
		switch field {
		case "strategySummary":
			changed.StrategySummary = want
		case "mergedEventGroups":
			changed.MergedEventGroupsJSON = want
		case "originalAdditions":
			changed.OriginalAdditions = want
		case "rationale":
			changed.Rationale = want
		}
		found := false
		for _, change := range DiffStrategy(before, changed).Fields {
			if change.Field == field {
				found = true
			}
		}
		if !found {
			t.Fatalf("a change to %s was not reported", field)
		}
	}
	// And an unchanged pair reports nothing.
	if DiffStrategy(before, before).Changed() {
		t.Fatal("a comparison of a version with itself reports a change")
	}
}

// TestDiffScriptStructureReportsADroppedShot covers the nested-shot removal path.
//
// The dropped-LINE path was tested and the dropped-shot path was not, so a mutation that removed
// the shot loop survived. The two branches are written the same way and a reader would assume
// they were covered together — which is why the mutation run is the thing that found it.
func TestDiffScriptStructureReportsADroppedShot(t *testing.T) {
	before := sampleStructure()
	// Two shots, so the newer version can drop one and keep one — a version with NO shots would
	// exercise the same branch but would leave the "kept" case untested.
	before.Scenes[0].Shots = append(before.Scenes[0].Shots, Shot{
		ID: "shot-2", SceneID: "scene-1", Ordinal: 2,
		VisualDescription: "铜牌特写。", Status: versioning.StatusDraft,
	})
	after := sampleStructure()
	after.ScriptVersionID = "version-2"

	diff := DiffScriptStructure(before, after)
	var sawDroppedShot bool
	for _, item := range diff.Items {
		for _, change := range item.Fields {
			if change.Field == "shot.2" && change.After == "" && change.Before == "铜牌特写。" {
				sawDroppedShot = true
			}
		}
	}
	if !sawDroppedShot {
		t.Fatalf("a dropped shot was not reported: %+v", diff.Items)
	}
	// The kept shot is unchanged, so the scene reports a change for the removal alone.
	if diff.Modified != 1 {
		t.Fatalf("the scene is reported as %d modified", diff.Modified)
	}
}
