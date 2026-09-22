package screenplay

import (
	"strings"
	"testing"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// These tests grade the FORMAT rather than the plumbing: a screenplay's conventions are the thing
// ROADMAP item 11's "Script Export" asks for, and each one is asserted separately so a rule that
// stopped being applied fails a named test rather than a diff of a whole document.

// fixture is one scene with every line type and, for the second scene, every interior/exterior
// marking a slugline has a spelling for.
func fixture() Document {
	return Document{
		ProjectName:   "渡口",
		EpisodeTitle:  "灯巷里的名字",
		SeasonNumber:  1,
		EpisodeNum:    3,
		VersionNumber: 2,
		Structure: scriptdomain.ScriptStructure{
			ScriptVersionID: "script-v2",
			Scenes: []scriptdomain.SceneStructure{
				{
					Scene: scriptdomain.Scene{
						ID: "scene-1", Ordinal: 1, SceneNumber: "1",
						Slugline: "渡口 - 夜", InteriorExterior: scriptdomain.InteriorEXT,
						TimeOfDay: "夜", Summary: "沈砚回到渡口。", DramaticGoal: "让他决定留下。",
					},
					DialogueLines: []scriptdomain.DialogueLine{
						{ID: "line-1", Ordinal: 1, Type: scriptdomain.LineAction, Text: "煤灯在风里晃。"},
						{ID: "line-2", Ordinal: 2, Type: scriptdomain.LineDialogue,
							CharacterEntityID: "沈砚", Text: "灯还亮着。", PerformanceNote: "低声"},
						{ID: "line-3", Ordinal: 3, Type: scriptdomain.LineNarration, Text: "那年的冬天格外长。"},
						{ID: "line-4", Ordinal: 4, Type: scriptdomain.LineTransition, Text: "CUT TO:"},
						{ID: "line-5", Ordinal: 5, Type: scriptdomain.LineNote, Text: "此处需要补拍。"},
					},
					Shots: []scriptdomain.Shot{
						{ID: "shot-1", Ordinal: 1, ShotNumber: "1", ShotSize: "MS",
							CameraAngle: "eye level", CameraMovement: "static",
							EstimatedDurationSeconds: 4, VisualDescription: "渡口的煤灯下，沈砚停步。"},
					},
				},
				{
					Scene: scriptdomain.Scene{
						ID: "scene-2", Ordinal: 2, SceneNumber: "2",
						Slugline: "盐仓", InteriorExterior: scriptdomain.InteriorINT, TimeOfDay: "日",
					},
					DialogueLines: []scriptdomain.DialogueLine{
						{ID: "line-6", Ordinal: 1, Type: scriptdomain.LineDialogue,
							CharacterEntityID: "老周", Text: "账本不在盐仓。"},
					},
				},
			},
		},
	}
}

// TestTheSluglineSpellsTheMarking pins the four markings' spellings.
//
// A slugline is where a scene begins for a reader, and its prefix is a CLAIM about where the scene
// happens. The four values are the schema's, so `OTHER` gets no prefix: a build that wrote `INT.` for
// a scene the version did not mark would be inventing a location fact.
//
// Every case uses the SAME place and time, so the marking is the only thing that changes between the
// expected strings and a failure names the marking rather than a fixture difference.
func TestTheSluglineSpellsTheMarking(t *testing.T) {
	cases := []struct {
		marking  scriptdomain.InteriorExterior
		expected string
	}{
		{scriptdomain.InteriorINT, "1. INT. 渡口 - 夜"},
		{scriptdomain.InteriorEXT, "1. EXT. 渡口 - 夜"},
		{scriptdomain.InteriorINTEXT, "1. INT./EXT. 渡口 - 夜"},
		// An `OTHER` scene gets no prefix at all.
		{scriptdomain.InteriorOTHER, "1. 渡口 - 夜"},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.marking), func(t *testing.T) {
			scene := scriptdomain.SceneStructure{Scene: scriptdomain.Scene{
				SceneNumber: "1", Slugline: "渡口", InteriorExterior: testCase.marking, TimeOfDay: "夜",
			}}
			if got := slugline(scene); got != testCase.expected {
				t.Fatalf("the slugline for %s is %q, want %q", testCase.marking, got, testCase.expected)
			}
		})
	}
	// A scene with no text at all still renders a heading: an unnamed scene is a state a draft is in,
	// and a document that printed a bare ordinal would look like a formatting bug.
	empty := scriptdomain.SceneStructure{Scene: scriptdomain.Scene{
		SceneNumber: "9", InteriorExterior: scriptdomain.InteriorOTHER,
	}}
	if got := slugline(empty); got != "9. 未命名场景" {
		t.Fatalf("an unnamed scene renders %q", got)
	}
	// A scene with no number renders the heading alone rather than a bare ordinal and a space.
	unnumbered := scriptdomain.SceneStructure{Scene: scriptdomain.Scene{
		Slugline: "渡口", InteriorExterior: scriptdomain.InteriorEXT, TimeOfDay: "夜",
	}}
	if got := slugline(unnumbered); got != "EXT. 渡口 - 夜" {
		t.Fatalf("an unnumbered scene renders %q", got)
	}
}

// TestAPlainDocumentCarriesEveryLineTypeGrades the human-readable renderer.
//
// Each type has its own convention and the test names it: a character cue is UPPERCASE above what
// they say, a narration is labelled rather than given a speaker, and an action is plain prose. A
// renderer that printed the raw text for all five would produce a document where a reader cannot tell
// who is speaking.
func TestAPlainDocumentCarriesEveryLineTypeGrades(t *testing.T) {
	document, err := Render(fixture(), FormatPlain)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		// The heading: which episode, which draft, how many scenes.
		"渡口", "第 1 季 第 3 集 灯巷里的名字", "版本 v2", "场景 2 个",
		// The slugline and the scene's own notes.
		"1. EXT. 渡口 - 夜", "摘要：沈砚回到渡口。", "戏剧目标：让他决定留下。",
		// The character cue is uppercase, and the parenthetical sits between it and the line.
		"沈砚", "（低声）", "灯还亮着。",
		// A narration is labelled rather than given a speaker.
		"旁白：那年的冬天格外长。",
		// A transition and a note.
		"CUT TO:", "[注] 此处需要补拍。",
		// The action line, which is plain prose.
		"煤灯在风里晃。",
	} {
		if !strings.Contains(document, want) {
			t.Fatalf("the plain document does not contain %q:\n%s", want, document)
		}
	}
	// The shots are NOT in a plain script unless the caller asked: a screenplay does not contain a
	// shot list, and a reader exporting a script for a person wants the script.
	if strings.Contains(document, "镜头：") {
		t.Fatal("the plain document carries a shot list although IncludeShots was false")
	}
	// And they ARE when it did.
	withShots := fixture()
	withShots.IncludeShots = true
	document, err = Render(withShots, FormatPlain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(document, "镜头：") || !strings.Contains(document, "MS / eye level / static / 4s") {
		t.Fatalf("the document with shots does not carry them:\n%s", document)
	}
}

// TestFountainUsesTheMarkupsOwnConventions grades the machine-readable renderer.
//
// Fountain's rules are small and each one is asserted: a scene heading is forced with a leading dot, a
// voice over carries `(V.O.)`, a transition is `>`, and a note is `[[ ]]`. A renderer that approximated
// them would produce a file a screenwriting tool reads as something else.
func TestFountainUsesTheMarkupsOwnConventions(t *testing.T) {
	document, err := Render(fixture(), FormatFountain)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		// The forced scene heading.
		".1. EXT. 渡口 - 夜",
		// A forced action line for the summary, so a summary that looks like a cue is still prose.
		"!沈砚回到渡口。",
		// A voice over carries the cue and the marking.
		"NARRATOR (V.O.)",
		"那年的冬天格外长。",
		// A transition.
		"> CUT TO:",
		// A Fountain note.
		"[[此处需要补拍。]]",
		// The character cue is bare and uppercase, which is how the markup finds it.
		"沈砚",
		"(低声)",
	} {
		if !strings.Contains(document, want) {
			t.Fatalf("the Fountain document does not contain %q:\n%s", want, document)
		}
	}
	// The action line is a forced one, so a line that begins with a word the markup would read as a
	// character cue is still prose.
	if !strings.Contains(document, "!煤灯在风里晃。") {
		t.Fatalf("the action line is not forced in the Fountain document:\n%s", document)
	}
}

// TestAnUnknownFormatIsRefused covers the boundary: a format this build does not write is refused
// rather than rendered as one of the two.
func TestAnUnknownFormatIsRefused(t *testing.T) {
	for _, format := range []Format{"", "pdf", "docx", "TXT"} {
		if _, err := Render(fixture(), format); err == nil {
			t.Fatalf("the format %q was accepted", format)
		}
		if IsValidFormat(format) {
			t.Fatalf("IsValidFormat says %q is valid", format)
		}
	}
	// The two documented formats are, and each has its own extension.
	for _, format := range Formats {
		if !IsValidFormat(format) {
			t.Fatalf("IsValidFormat says the documented format %q is invalid", format)
		}
		if format.ExtensionFor() == "" || !strings.HasPrefix(format.ExtensionFor(), ".") {
			t.Fatalf("the format %q has no extension", format)
		}
	}
}

// TestAnEmptyStructureStillRenders covers the episode whose script has no scenes yet.
//
// It is a legal state — a version is created before it is written — and the document is a title page
// rather than an error: a caller that asked for its script and got nothing would have no way to tell
// "no scenes" from "the read failed".
func TestAnEmptyStructureStillRenders(t *testing.T) {
	document := Document{EpisodeTitle: "空", EpisodeNum: 1, VersionNumber: 1}
	for _, format := range Formats {
		text, err := Render(document, format)
		if err != nil {
			t.Fatalf("Render(%s): %v", format, err)
		}
		if !strings.Contains(text, "版本 v1") {
			t.Fatalf("the %s document has no heading:\n%s", format, text)
		}
		if !strings.Contains(text, "场景 0 个") {
			t.Fatalf("the %s document does not say it has no scenes:\n%s", format, text)
		}
	}
}
