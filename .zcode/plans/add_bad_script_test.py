import io

p = "internal/infrastructure/database/canary_script_test.go"
s = io.open(p, encoding="utf-8").read()

anchor = "// TestCanaryFixturesCoverSection181 asserts AGENT_CONTRACTS \u00a718.1's list against the fixture directory."

new = '''// TestTheBadScriptFixtureIsRefusedByTheValidator is what the deliberately-wrong fixture is FOR.
//
// AGENT_CONTRACTS \u00a718.1 lists "\u6545\u610f\u9519\u8bef\u5267\u672c" as a corpus item, and a fixture nothing reads is a file
// whose only assertion is that it exists \u2014 which is what `TestCanaryFixturesCoverSection181` below
// checks and all it checks. An independent review found that gap: five named faults in a JSON document
// that no test fed to anything.
//
// So this test LOADS the fixture and drives its payload through the domain's validator, asserting that
// the whole document is refused AND that each fault is refused when applied ALONE to a valid structure.
// The second half is what makes the fixture a corpus item rather than a file: a document with five
// faults fails on the first one the validator reaches, so the document alone proves one rule.
//
// One fault cannot be checked by the domain: a scene's `sourceEventId` names a row in a PROJECT, and the
// domain has no project. That rule is the service's reference check, and `references_wp08_test.go` covers
// it \u2014 the comment on `applyFault` says so rather than leaving the reader to wonder.
func TestTheBadScriptFixtureIsRefusedByTheValidator(t *testing.T) {
	fixture := loadCanaryFixture(t, "bad-script.json")
	structure := structureFromBadScriptFixture(fixture)
	// The whole payload is refused: five faults, of which the validator reports the first.
	if err := structure.Validate(); err == nil {
		t.Fatal("the deliberately-wrong script passed validation, so the fixture proves nothing")
	}
	// Each fault alone.
	for _, fault := range fixture.Faults {
		single := validStructureForFaults()
		if err := applyFault(&single, fault); err != nil {
			// A fault that cannot be expressed is a defect in the fixture, not a pass.
			t.Fatalf("the fixture's fault %q cannot be applied: %v", fault.Field, err)
		}
		if fault.Field == "scenes[0].sourceStoryEventId" {
			// The domain cannot judge this one, so the assertion is that the payload CHANGED \u2014 which is
			// what the fixture owes the service's check, exercised in the script package.
			if single.Scenes[0].SourceStoryEventID != fault.Value {
				t.Fatalf("the fault %q did not reach the payload", fault.Field)
			}
			continue
		}
		if err := single.Validate(); err == nil {
			t.Errorf("the fault %q (%s) was accepted when applied alone, so the fixture names a rule nothing enforces",
				fault.Field, fault.Rule)
		}
	}
}

// badScriptFixture is the fixture's own shape.
type badScriptFixture struct {
	Note   string        `json:"note"`
	Faults []badScriptFault `json:"faults"`
	Script struct {
		Scenes []struct {
			Ordinal                  int    `json:"ordinal"`
			SceneNumber              string `json:"sceneNumber"`
			Slugline                 string `json:"slugline"`
			InteriorExterior         string `json:"interiorExterior"`
			Summary                  string `json:"summary"`
			EstimatedDurationSeconds int    `json:"estimatedDurationSeconds"`
			SourceStoryEventID       string `json:"sourceStoryEventId"`
			DialogueLines            []struct {
				Ordinal int    `json:"ordinal"`
				SceneID string `json:"sceneId"`
				Type    string `json:"type"`
				Text    string `json:"text"`
			} `json:"dialogueLines"`
		} `json:"scenes"`
	} `json:"script"`
}

// badScriptFault is one named fault: the field path, the wrong value, and the rule it breaks.
type badScriptFault struct {
	Field string `json:"field"`
	Value any    `json:"value"`
	Rule  string `json:"rule"`
}

// loadCanaryFixture reads one fixture from the corpus.
func loadCanaryFixture(t *testing.T, name string) badScriptFixture {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "canary-drama", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	var fixture badScriptFixture
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	if len(fixture.Faults) == 0 {
		t.Fatalf("%s names no faults, so it is a wrong script nobody described", name)
	}
	return fixture
}

// structureFromBadScriptFixture converts the fixture's payload into a domain structure.
//
// It states no identifiers of the fixture's, because the fixture carries none: a document written by hand
// describes a script the way a model does, and the identifiers are the code's to mint.
func structureFromBadScriptFixture(fixture badScriptFixture) scriptdomain.ScriptStructure {
	structure := scriptdomain.ScriptStructure{ScriptVersionID: "canary-bad-script"}
	for index, scene := range fixture.Script.Scenes {
		entry := scriptdomain.SceneStructure{Scene: scriptdomain.Scene{
			ID: "bad-scene-" + itoaForTest(index+1), ScriptVersionID: "canary-bad-script",
			Ordinal: scene.Ordinal, SceneNumber: scene.SceneNumber, Slugline: scene.Slugline,
			InteriorExterior:         scriptdomain.InteriorExterior(scene.InteriorExterior),
			Summary:                  scene.Summary,
			EstimatedDurationSeconds: scene.EstimatedDurationSeconds,
			SourceStoryEventID:       scene.SourceStoryEventID,
		}}
		for lineIndex, line := range scene.DialogueLines {
			entry.DialogueLines = append(entry.DialogueLines, scriptdomain.DialogueLine{
				ID: "bad-line-" + itoaForTest(index+1) + "-" + itoaForTest(lineIndex+1),
				// The fixture's own scene reference is copied VERBATIM, including the fault: a converter
				// that rewrote it to the parent would hide the fault it exists to carry.
				SceneID: line.SceneID, Ordinal: line.Ordinal,
				Type: scriptdomain.LineType(line.Type), Text: line.Text,
			})
		}
		structure.Scenes = append(structure.Scenes, entry)
	}
	return structure
}

// validStructureForFaults builds a two-scene structure that validates, so one fault can be applied alone.
func validStructureForFaults() scriptdomain.ScriptStructure {
	stamp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	structure := scriptdomain.ScriptStructure{ScriptVersionID: "fault-base"}
	for index := 1; index <= 2; index++ {
		entry := scriptdomain.SceneStructure{Scene: scriptdomain.Scene{
			ID: "fault-scene-" + itoaForTest(index), ScriptVersionID: "fault-base", Ordinal: index,
			Slugline: "INT. fault - day", InteriorExterior: scriptdomain.InteriorINT,
			EstimatedDurationSeconds: 60, CreatedAt: stamp, UpdatedAt: stamp, Revision: 1,
		}}
		if index == 1 {
			entry.DialogueLines = append(entry.DialogueLines, scriptdomain.DialogueLine{
				ID: "fault-line-1", SceneID: "fault-scene-1", Ordinal: 1,
				Type: scriptdomain.LineDialogue, Text: "a line",
				CreatedAt: stamp, UpdatedAt: stamp, Revision: 1,
			})
		}
		structure.Scenes = append(structure.Scenes, entry)
	}
	return structure
}

// applyFault applies one of the fixture's named faults to a valid structure.
//
// The mapping is by the fixture's OWN field path, and a path this function does not know is an ERROR
// rather than a skip: a fault the fixture names and this test cannot apply is a rule nothing checks.
func applyFault(structure *scriptdomain.ScriptStructure, fault badScriptFault) error {
	switch fault.Field {
	case "scenes[1].ordinal":
		structure.Scenes[1].Ordinal = int(fault.Value.(float64))
	case "scenes[0].dialogueLines[0].sceneId":
		structure.Scenes[0].DialogueLines[0].SceneID = fault.Value.(string)
	case "scenes[0].sourceStoryEventId":
		// The DOMAIN cannot check a reference's existence \u2014 it has no project \u2014 so this fault's rule is
		// the SERVICE's. The caller asserts the payload changed rather than that Validate refused it.
		structure.Scenes[0].SourceStoryEventID = fault.Value.(string)
	case "scenes[0].interiorExterior":
		structure.Scenes[0].InteriorExterior = scriptdomain.InteriorExterior(fault.Value.(string))
	case "scenes[1].estimatedDurationSeconds":
		structure.Scenes[1].EstimatedDurationSeconds = int(fault.Value.(float64))
	default:
		return fmt.Errorf("the fixture names the fault %q, which this test does not know how to apply", fault.Field)
	}
	return nil
}

'''

assert anchor in s, "anchor not found"
s = s.replace(anchor, new + anchor, 1)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("ok")
