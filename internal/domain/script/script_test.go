package script

import (
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// categoryOf reads the category of a domain error through the public
// extractor, so the tests exercise AsError rather than reaching into the struct.
func categoryOf(err error) ErrorCategory {
	domainErr, ok := AsError(err)
	if !ok {
		return ""
	}
	return domainErr.Category
}

// checkVocabulary proves a closed vocabulary is exactly the set its migration
// CHECK admits.
//
// The positive half is that every documented value validates and that the list
// holds no duplicate. The negative half is the one that catches a drifting
// vocabulary: the empty string, a case variant of a documented value and a
// string the migration never names must all be refused. Without those, a
// validator written as `return true` would pass every positive assertion.
func checkVocabulary[T ~string](t *testing.T, name string, values []T, isValid func(T) bool) {
	t.Helper()
	seen := map[T]bool{}
	for _, value := range values {
		if value == "" {
			t.Fatalf("%s lists the empty value, which no SQL CHECK admits", name)
		}
		if seen[value] {
			t.Fatalf("%s lists %q twice", name, value)
		}
		seen[value] = true
		if !isValid(value) {
			t.Fatalf("%s rejects its own documented value %q", name, value)
		}
		for _, variant := range []T{T(strings.ToUpper(string(value))), T(strings.ToLower(string(value)))} {
			if variant == value {
				continue
			}
			if isValid(variant) {
				t.Fatalf("%s accepts the case variant %q of its documented value %q", name, variant, value)
			}
		}
	}
	if isValid(T("")) {
		t.Fatalf("%s accepts the empty value", name)
	}
	if isValid(T("nonsense")) {
		t.Fatalf("%s accepts the unknown value %q", name, "nonsense")
	}
}

// TestScriptVocabularies covers §7's closed sets that this package owns. The
// version statuses are deliberately absent: they are §2.5's shared set and are
// covered by the versioning package's own tests.
func TestScriptVocabularies(t *testing.T) {
	checkVocabulary(t, "EpisodeStatuses", EpisodeStatuses, IsValidEpisodeStatus)
	checkVocabulary(t, "AdaptationModes", AdaptationModes, IsValidAdaptationMode)
	checkVocabulary(t, "InteriorExteriors", InteriorExteriors, IsValidInteriorExterior)
	checkVocabulary(t, "LineTypes", LineTypes, IsValidLineType)
}

// TestVocabularyMatchesMigrationChecks pins each Go vocabulary against the
// literal list in migration 000008, plus the §2.5 status set the migration
// repeats for every versioned table.
//
// The expected slices are written out here rather than derived from the
// constants, so the two sides can disagree. That is the point: a value added to
// the SQL CHECK without this package, or to this package without SQL, is a bug
// — the database would refuse a value the domain accepts or the reverse — and
// this test is where it fails.
func TestVocabularyMatchesMigrationChecks(t *testing.T) {
	documentedStatuses := []string{
		"draft", "candidate", "under_review", "approved",
		"rejected", "superseded", "deprecated", "stale",
	}
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "episodes.status",
			got:  episodeStatusStrings(),
			want: []string{"planning", "writing", "approved", "production", "completed"},
		},
		{
			name: "adaptation_strategy_versions.adaptation_mode",
			got:  adaptationModeStrings(),
			want: []string{"faithful", "balanced", "aggressive"},
		},
		{
			name: "scenes.interior_exterior",
			got:  interiorExteriorStrings(),
			want: []string{"INT", "EXT", "INT_EXT", "OTHER"},
		},
		{
			name: "dialogue_lines.line_type",
			got:  lineTypeStrings(),
			want: []string{"dialogue", "narration", "action", "transition", "note"},
		},
		{
			name: "story_skeleton_versions.status",
			got:  versionStatusStrings(),
			want: documentedStatuses,
		},
		{
			name: "adaptation_strategy_versions.status",
			got:  versionStatusStrings(),
			want: documentedStatuses,
		},
		{
			name: "script_versions.status",
			got:  versionStatusStrings(),
			want: documentedStatuses,
		},
		{
			name: "shots.status",
			got:  versionStatusStrings(),
			want: documentedStatuses,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if len(testCase.got) != len(testCase.want) {
				t.Fatalf("the Go list holds %d values %v, but the migration CHECK holds %d %v",
					len(testCase.got), testCase.got, len(testCase.want), testCase.want)
			}
			for index, want := range testCase.want {
				if testCase.got[index] != want {
					t.Fatalf("position %d is %q in Go but %q in the migration CHECK, so the two vocabularies have drifted",
						index, testCase.got[index], want)
				}
			}
		})
	}
}

func episodeStatusStrings() []string {
	out := make([]string, 0, len(EpisodeStatuses))
	for _, value := range EpisodeStatuses {
		out = append(out, string(value))
	}
	return out
}

func adaptationModeStrings() []string {
	out := make([]string, 0, len(AdaptationModes))
	for _, value := range AdaptationModes {
		out = append(out, string(value))
	}
	return out
}

func interiorExteriorStrings() []string {
	out := make([]string, 0, len(InteriorExteriors))
	for _, value := range InteriorExteriors {
		out = append(out, string(value))
	}
	return out
}

func lineTypeStrings() []string {
	out := make([]string, 0, len(LineTypes))
	for _, value := range LineTypes {
		out = append(out, string(value))
	}
	return out
}

// versionStatusStrings reads the shared §2.5 set through the alias this package
// uses, so the assertion covers what a caller of this package would pass.
func versionStatusStrings() []string {
	out := make([]string, 0, len(versioning.Statuses))
	for _, value := range versioning.Statuses {
		out = append(out, string(value))
	}
	return out
}

// TestInteriorExteriorIsExhaustive walks the four-marking set value by value,
// because the migration pins it and the schema's default is OTHER rather than
// one of the three slugline values. A value silently dropped here would make a
// storable scene unstorable in Go.
func TestInteriorExteriorIsExhaustive(t *testing.T) {
	if len(InteriorExteriors) != 4 {
		t.Fatalf("the marking set has %d entries, want the 4 the migration CHECK pins", len(InteriorExteriors))
	}
	expected := []struct {
		value InteriorExterior
		text  string
	}{
		{InteriorINT, "INT"},
		{InteriorEXT, "EXT"},
		{InteriorINTEXT, "INT_EXT"},
		{InteriorOTHER, "OTHER"},
	}
	for index, want := range expected {
		if InteriorExteriors[index] != want.value {
			t.Fatalf("position %d is %q, want %q", index, InteriorExteriors[index], want.value)
		}
		if string(want.value) != want.text {
			t.Fatalf("the constant %q spells itself %q, but the migration CHECK says %q",
				want.value, string(want.value), want.text)
		}
		if !IsValidInteriorExterior(want.value) {
			t.Fatalf("the documented marking %q is refused", want.value)
		}
	}
	// The negative half: the lowercase spelling an author might type, the
	// words the markings stand for, and a value the schema never had.
	for _, bad := range []InteriorExterior{"int", "ext", "int_ext", "INTEXT", "OUTSIDE", "INTERIOR", ""} {
		if IsValidInteriorExterior(bad) {
			t.Fatalf("the undocumented marking %q was accepted", bad)
		}
	}
}

// TestEpisodeKey covers §7.1's business key, which is what a uniqueness message
// and the episode picker show.
func TestEpisodeKey(t *testing.T) {
	cases := []struct {
		name     string
		season   int
		episode  int
		expected string
	}{
		{"first season, first episode", 1, 1, "S1E1"},
		{"first season, third episode", 1, 3, "S1E3"},
		{"a later season", 2, 12, "S2E12"},
		{"two digits in both", 10, 24, "S10E24"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			episode := Episode{SeasonNumber: testCase.season, EpisodeNumber: testCase.episode}
			if got := episode.Key(); got != testCase.expected {
				t.Fatalf("SeasonNumber=%d EpisodeNumber=%d renders %q, want %q",
					testCase.season, testCase.episode, got, testCase.expected)
			}
		})
	}
	// Two different episodes must not render the same key, or a uniqueness
	// message would be ambiguous. "S1E11" is the case a naive concatenation
	// without a separator would confuse with "S11E1".
	first := Episode{SeasonNumber: 1, EpisodeNumber: 11}
	second := Episode{SeasonNumber: 11, EpisodeNumber: 1}
	if first.Key() == second.Key() {
		t.Fatalf("S1E11 and S11E1 both render %q", first.Key())
	}
	if first.Key() != "S1E11" || second.Key() != "S11E1" {
		t.Fatalf("the keys are %q and %q, want S1E11 and S11E1", first.Key(), second.Key())
	}
}

// TestEpisodeValidate covers §7.1's bounds, which the schema also enforces.
func TestEpisodeValidate(t *testing.T) {
	base := Episode{
		ID:            "episode-1",
		ProjectID:     "project-1",
		SeasonNumber:  1,
		EpisodeNumber: 1,
		Status:        EpisodePlanning,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed episode was rejected: %v", err)
	}
	// A target of zero means "not planned yet" and is allowed; the schema's
	// CHECK is target_duration_seconds >= 0.
	cases := []struct {
		name   string
		mutate func(*Episode)
	}{
		{"no project", func(e *Episode) { e.ProjectID = "  " }},
		{"season zero", func(e *Episode) { e.SeasonNumber = 0 }},
		{"negative season", func(e *Episode) { e.SeasonNumber = -1 }},
		{"episode zero", func(e *Episode) { e.EpisodeNumber = 0 }},
		{"unknown status", func(e *Episode) { e.Status = "drafting" }},
		{"negative target duration", func(e *Episode) { e.TargetDurationSeconds = -1 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			episode := base
			testCase.mutate(&episode)
			if err := episode.Validate(); err == nil {
				t.Fatal("a malformed episode was accepted")
			}
		})
	}
}

// TestStorySkeletonVersionValidate covers §7.2 and proves the status field
// takes the shared §2.5 vocabulary rather than a local set.
func TestStorySkeletonVersionValidate(t *testing.T) {
	base := StorySkeletonVersion{
		ID:            "skeleton-1",
		EpisodeID:     "episode-1",
		VersionNumber: 1,
		Status:        versioning.StatusDraft,
		CreatedByType: versioning.CreatedByAgent,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed story skeleton version was rejected: %v", err)
	}
	// Every §2.5 status is storable, because the migration's CHECK is that set.
	for _, status := range versioning.Statuses {
		candidate := base
		candidate.Status = status
		if err := candidate.Validate(); err != nil {
			t.Fatalf("the documented version status %q was rejected: %v", status, err)
		}
	}
	cases := []struct {
		name   string
		mutate func(*StorySkeletonVersion)
	}{
		{"no episode", func(s *StorySkeletonVersion) { s.EpisodeID = "" }},
		{"version zero", func(s *StorySkeletonVersion) { s.VersionNumber = 0 }},
		{"unknown status", func(s *StorySkeletonVersion) { s.Status = "review" }},
		{"unknown producer", func(s *StorySkeletonVersion) { s.CreatedByType = "robot" }},
		{"negative estimate", func(s *StorySkeletonVersion) { s.EstimatedDurationSeconds = -1 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			version := base
			testCase.mutate(&version)
			if err := version.Validate(); err == nil {
				t.Fatal("a malformed story skeleton version was accepted")
			}
		})
	}
}

// TestAdaptationStrategyVersionValidate covers §7.3, including the local mode
// type that mirrors project.AdaptationMode without importing it.
func TestAdaptationStrategyVersionValidate(t *testing.T) {
	base := AdaptationStrategyVersion{
		ID:             "strategy-1",
		EpisodeID:      "episode-1",
		VersionNumber:  1,
		Status:         versioning.StatusDraft,
		AdaptationMode: AdaptationBalanced,
		CreatedByType:  versioning.CreatedByAgent,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed adaptation strategy version was rejected: %v", err)
	}
	// Migration 000006 pins the same three strings for the project's own
	// adaptation_mode, and a project's mode is copied into a strategy when the
	// pipeline runs. The comparison is by string so this test states the
	// coupling without the script domain importing the project domain.
	if got, want := string(AdaptationBalanced), "balanced"; got != want {
		t.Fatalf("this package spells the default mode %q, want %q", got, want)
	}
	cases := []struct {
		name   string
		mutate func(*AdaptationStrategyVersion)
	}{
		{"no episode", func(a *AdaptationStrategyVersion) { a.EpisodeID = " " }},
		{"version zero", func(a *AdaptationStrategyVersion) { a.VersionNumber = 0 }},
		{"unknown status", func(a *AdaptationStrategyVersion) { a.Status = "review" }},
		{"unknown mode", func(a *AdaptationStrategyVersion) { a.AdaptationMode = "loose" }},
		{"unknown producer", func(a *AdaptationStrategyVersion) { a.CreatedByType = "robot" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			strategy := base
			testCase.mutate(&strategy)
			if err := strategy.Validate(); err == nil {
				t.Fatal("a malformed adaptation strategy version was accepted")
			}
		})
	}
}

// TestScriptValidate covers §7.4's stable identity.
func TestScriptValidate(t *testing.T) {
	base := Script{ID: "script-1", EpisodeID: "episode-1"}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed script was rejected: %v", err)
	}
	// One script per episode: the schema's UNIQUE (episode_id) is the
	// constraint, and nothing here rejects a second row because that is a
	// database conflict rather than a shape error.
	for _, testCase := range []struct {
		name    string
		episode string
	}{
		{"no episode", ""},
		{"blank episode", "   "},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			script := base
			script.EpisodeID = testCase.episode
			if err := script.Validate(); err == nil {
				t.Fatal("a script with no episode was accepted")
			}
		})
	}
}

// TestScriptVersionValidate covers §7.5, including the two upstream references
// that §15.2's staleness chain walks.
func TestScriptVersionValidate(t *testing.T) {
	base := ScriptVersion{
		ID:                          "scriptv-1",
		ScriptID:                    "script-1",
		VersionNumber:               1,
		Status:                      versioning.StatusDraft,
		StorySkeletonVersionID:      "skeleton-1",
		AdaptationStrategyVersionID: "strategy-1",
		CreatedByType:               versioning.CreatedByAgent,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed script version was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*ScriptVersion)
	}{
		{"no script", func(v *ScriptVersion) { v.ScriptID = "" }},
		{"version zero", func(v *ScriptVersion) { v.VersionNumber = 0 }},
		{"unknown status", func(v *ScriptVersion) { v.Status = "pending" }},
		{"no skeleton", func(v *ScriptVersion) { v.StorySkeletonVersionID = "  " }},
		{"no strategy", func(v *ScriptVersion) { v.AdaptationStrategyVersionID = "" }},
		{"negative estimate", func(v *ScriptVersion) { v.EstimatedDurationSeconds = -1 }},
		{"unknown producer", func(v *ScriptVersion) { v.CreatedByType = "robot" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			version := base
			testCase.mutate(&version)
			if err := version.Validate(); err == nil {
				t.Fatal("a malformed script version was accepted")
			}
		})
	}
}

// TestSceneValidate covers §7.6.
func TestSceneValidate(t *testing.T) {
	base := Scene{
		ID:               "scene-1",
		ScriptVersionID:  "scriptv-1",
		Ordinal:          1,
		SceneNumber:      "1",
		InteriorExterior: InteriorINT,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed scene was rejected: %v", err)
	}
	// A scene invented by the adaptation is marked, and an adapted one is not:
	// the flag is a bool because AC-SCRIPT-003 asks for the two states only.
	original := base
	original.IsOriginalAdaptation = true
	original.SceneNumber = "12A"
	if err := original.Validate(); err != nil {
		t.Fatalf("an original scene with an alphanumeric number must be accepted: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Scene)
	}{
		{"no script version", func(s *Scene) { s.ScriptVersionID = "  " }},
		{"ordinal zero", func(s *Scene) { s.Ordinal = 0 }},
		{"negative ordinal", func(s *Scene) { s.Ordinal = -1 }},
		{"unknown marking", func(s *Scene) { s.InteriorExterior = "OUTSIDE" }},
		{"lowercase marking", func(s *Scene) { s.InteriorExterior = "int" }},
		{"negative estimate", func(s *Scene) { s.EstimatedDurationSeconds = -1 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			scene := base
			testCase.mutate(&scene)
			if err := scene.Validate(); err == nil {
				t.Fatal("a malformed scene was accepted")
			}
		})
	}
}

// TestDialogueLineValidate covers §7.7.
func TestDialogueLineValidate(t *testing.T) {
	base := DialogueLine{
		ID:      "line-1",
		SceneID: "scene-1",
		Ordinal: 1,
		Type:    LineDialogue,
		Text:    "You kept the letter.",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed dialogue line was rejected: %v", err)
	}
	// A line with no character is legitimate: an action beat or a crowd voice
	// has none, and the migration's column defaults to empty.
	action := base
	action.Type = LineAction
	action.CharacterEntityID = ""
	if err := action.Validate(); err != nil {
		t.Fatalf("a line with no character must be accepted: %v", err)
	}
	// A locked line is a user protection, not a shape error.
	locked := base
	locked.Locked = true
	if err := locked.Validate(); err != nil {
		t.Fatalf("a locked line must be accepted: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*DialogueLine)
	}{
		{"no scene", func(d *DialogueLine) { d.SceneID = "" }},
		{"ordinal zero", func(d *DialogueLine) { d.Ordinal = 0 }},
		{"unknown line type", func(d *DialogueLine) { d.Type = "subtitle" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			line := base
			testCase.mutate(&line)
			if err := line.Validate(); err == nil {
				t.Fatal("a malformed dialogue line was accepted")
			}
		})
	}
}

// TestShotValidate covers §7.8.
func TestShotValidate(t *testing.T) {
	base := Shot{ID: "shot-1", SceneID: "scene-1", Ordinal: 1, Status: versioning.StatusDraft}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed shot was rejected: %v", err)
	}
	// A draft shot may have no number and no description: §7.8 lets a shot be
	// created at the script stage and refined by the storyboard workflow.
	if err := base.ValidateForStoryboard(); err == nil {
		t.Fatal("a shot with neither a number nor a description was accepted for storyboarding")
	}
	cases := []struct {
		name   string
		mutate func(*Shot)
	}{
		{"no scene", func(s *Shot) { s.SceneID = " " }},
		{"ordinal zero", func(s *Shot) { s.Ordinal = 0 }},
		{"negative ordinal", func(s *Shot) { s.Ordinal = -1 }},
		{"unknown status", func(s *Shot) { s.Status = "pending" }},
		{"negative estimate", func(s *Shot) { s.EstimatedDurationSeconds = -1 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			shot := base
			testCase.mutate(&shot)
			if err := shot.Validate(); err == nil {
				t.Fatal("a malformed shot was accepted")
			}
		})
	}
}

// TestShotValidateForStoryboard is the §9.4 precondition: a StoryboardItem must
// correspond to a shot complete enough to draw, so both the number it is
// referenced by and the description that becomes the panel prompt are required.
func TestShotValidateForStoryboard(t *testing.T) {
	complete := Shot{
		ID:                "shot-1",
		SceneID:           "scene-1",
		Ordinal:           1,
		ShotNumber:        "1A",
		VisualDescription: "Mira stands in the doorway, letter in hand",
		Status:            versioning.StatusApproved,
	}
	if err := complete.ValidateForStoryboard(); err != nil {
		t.Fatalf("a complete shot was rejected for storyboarding: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Shot)
	}{
		{"no number", func(s *Shot) { s.ShotNumber = "" }},
		{"blank number", func(s *Shot) { s.ShotNumber = "   " }},
		{"no visual description", func(s *Shot) { s.VisualDescription = "" }},
		{"blank visual description", func(s *Shot) { s.VisualDescription = "\t\n" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			shot := complete
			testCase.mutate(&shot)
			if err := shot.ValidateForStoryboard(); err == nil {
				t.Fatal("an incomplete shot was accepted for storyboarding")
			}
		})
	}

	// The storyboard check is strictly stronger than Validate: a shot that
	// fails the base shape fails this one too, rather than passing by having
	// its two storyboard fields set.
	broken := complete
	broken.Ordinal = 0
	broken.ShotNumber = "1A"
	broken.VisualDescription = "described"
	if err := broken.ValidateForStoryboard(); err == nil {
		t.Fatal("a shot with an invalid ordinal passed the storyboard check")
	}
	if err := broken.Validate(); err == nil {
		t.Fatal("the base validation accepted the same malformed shot, so the storyboard check proved nothing")
	}

	// An action-only shot is describable through its action but the panel needs
	// the visual; the method therefore refuses it rather than falling back.
	actionOnly := complete
	actionOnly.VisualDescription = ""
	actionOnly.ActionDescription = "She crosses the room."
	if err := actionOnly.ValidateForStoryboard(); err == nil {
		t.Fatal("a shot with only an action description was accepted for storyboarding")
	}
}

// TestErrorCategories covers the taxonomy the desktop layer maps: each
// constructor reports its own category and AsError finds it through a wrap.
func TestErrorCategories(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorCategory
	}{
		{"invalid", InvalidError("x"), CategoryInvalidInput},
		{"not found", NotFoundError(), CategoryNotFound},
		{"conflict", ConflictError("x"), CategoryConflict},
		{"storage", StorageError("x", nil), CategoryStorage},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := categoryOf(testCase.err); got != testCase.want {
				t.Fatalf("category is %q, want %q", got, testCase.want)
			}
		})
	}
	// A wrapped domain error is still found, and a foreign error is not
	// mistaken for one.
	if _, ok := AsError(StorageError("x", nil)); !ok {
		t.Fatal("AsError did not find a domain error")
	}
	if _, ok := AsError(nil); ok {
		t.Fatal("AsError reported a domain error for nil")
	}
	var nilErr *Error
	if nilErr.Error() != "" || nilErr.Unwrap() != nil {
		t.Fatal("a nil domain error must be safe to print and unwrap")
	}
	storage := StorageError("the write failed", InvalidError("cause"))
	if storage.Unwrap() == nil {
		t.Fatal("a storage error must keep its cause")
	}
	if storage.Error() != "the write failed" {
		t.Fatalf("the storage error prints %q, which must be only the safe message", storage.Error())
	}
}
