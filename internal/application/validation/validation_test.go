package validation

import (
	"errors"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/extraction"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/schemas"
)

// These tests exercise the real embedded schema, not a copy of it. A schema is
// the kind of artifact that gets edited and never re-tested, so every assertion
// here reads the file the binary would use.

// validDocument is the smallest document the contract accepts, in raw JSON. It is
// written as bytes rather than built from Go so the test cannot accidentally
// agree with the reader about a field name.
const validDocument = `{
  "schemaVersion": 1,
  "summary": "Mira finds a letter.",
  "entities": [
    {"ref": "mira", "type": "character", "canonicalName": "Mira", "aliases": ["小艾"]},
    {"ref": "hall", "type": "location", "canonicalName": "The Great Hall"}
  ],
  "events": [
    {
      "ref": "finds",
      "name": "Mira finds the letter",
      "eventType": "discovery",
      "storyTimeText": "that evening",
      "storyTimeOrder": 4,
      "locationRef": "hall",
      "importance": "major",
      "confidence": 0.8,
      "participants": [{"entityRef": "mira", "role": "actor"}]
    }
  ],
  "relations": [
    {"sourceRef": "mira", "targetRef": "hall", "relationType": "located_in", "validFromEventRef": "finds"}
  ]
}`

func TestEventExtractionAcceptsTheEmptyResult(t *testing.T) {
	// A chapter of pure description yields nothing, and that is a legitimate
	// reading rather than a failure. Requiring a non-empty array would push a
	// model to invent an event to satisfy the contract.
	document, err := EventExtraction([]byte(`{"schemaVersion":1,"entities":[],"events":[],"relations":[]}`))
	if err != nil {
		t.Fatalf("an empty result must validate: %v", err)
	}
	if len(document.Entities) != 0 || len(document.Events) != 0 || len(document.Relations) != 0 {
		t.Fatalf("an empty result produced %d entities, %d events, %d relations",
			len(document.Entities), len(document.Events), len(document.Relations))
	}
	// The optional summary is absent, which is allowed.
	if document.Summary != "" {
		t.Fatalf("an absent summary read as %q", document.Summary)
	}
}

func TestEventExtractionReadsEveryField(t *testing.T) {
	document, err := EventExtraction([]byte(validDocument))
	if err != nil {
		t.Fatalf("the reference document must validate: %v", err)
	}
	if document.Summary != "Mira finds a letter." {
		t.Fatalf("summary = %q", document.Summary)
	}
	if len(document.Entities) != 2 || len(document.Events) != 1 || len(document.Relations) != 1 {
		t.Fatalf("read %d entities, %d events, %d relations",
			len(document.Entities), len(document.Events), len(document.Relations))
	}
	first := document.Entities[0]
	if first.Ref != "mira" || first.Type != "character" || first.CanonicalName != "Mira" {
		t.Fatalf("the first entity read as %+v", first)
	}
	if len(first.Aliases) != 1 || first.Aliases[0] != "小艾" {
		t.Fatalf("the aliases read as %v", first.Aliases)
	}
	event := document.Events[0]
	if event.Name != "Mira finds the letter" || event.LocationRef != "hall" || event.Importance != "major" {
		t.Fatalf("the event read as %+v", event)
	}
	if event.StoryTimeOrder == nil || *event.StoryTimeOrder != 4 {
		t.Fatalf("storyTimeOrder read as %v, want 4", event.StoryTimeOrder)
	}
	if event.Confidence != 0.8 {
		t.Fatalf("confidence read as %v", event.Confidence)
	}
	if len(event.Participants) != 1 || event.Participants[0].Role != "actor" {
		t.Fatalf("the participants read as %+v", event.Participants)
	}
	relation := document.Relations[0]
	if relation.Type != "located_in" || relation.ValidFromEventRef != "finds" {
		t.Fatalf("the relation read as %+v", relation)
	}
}

// TestEventExtractionNullAndAbsentAreTheSame covers the optional references: the
// schema allows null, and the reader must not distinguish it from absence, or
// two spellings of "the chapter did not say" would behave differently.
func TestEventExtractionNullAndAbsentAreTheSame(t *testing.T) {
	withNull, err := EventExtraction([]byte(`{
		"schemaVersion": 1,
		"entities": [],
		"events": [{"ref": "e", "name": "n", "storyTimeOrder": null, "locationRef": null,
		            "causeSummary": null, "resultSummary": null, "eventType": null,
		            "storyTimeText": null, "description": null}],
		"relations": []
	}`))
	if err != nil {
		t.Fatalf("null optional fields must validate: %v", err)
	}
	if withNull.Events[0].StoryTimeOrder != nil {
		t.Fatalf("a null storyTimeOrder read as %v, want unknown", *withNull.Events[0].StoryTimeOrder)
	}
	if withNull.Events[0].LocationRef != "" {
		t.Fatalf("a null location read as %q, want empty", withNull.Events[0].LocationRef)
	}
	// The same event with the fields omitted must read identically, or the two
	// spellings would produce different rows.
	absent, err := EventExtraction([]byte(`{
		"schemaVersion": 1,
		"entities": [],
		"events": [{"ref": "e", "name": "n"}],
		"relations": []
	}`))
	if err != nil {
		t.Fatalf("omitted optional fields must validate: %v", err)
	}
	if absent.Events[0].StoryTimeOrder != nil || absent.Events[0].LocationRef != "" {
		t.Fatalf("an omitted optional field read as %+v", absent.Events[0])
	}
}

// TestEventExtractionRefusesAMissingTopLevelField covers the required set. Each
// of these would otherwise be written as an absent collection or a zero version,
// and an importer that guessed would be inventing the contract.
func TestEventExtractionRefusesAMissingTopLevelField(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		property string
	}{
		{"no schema version", `{"entities":[],"events":[],"relations":[]}`, "schemaVersion"},
		{"no entities", `{"schemaVersion":1,"events":[],"relations":[]}`, "entities"},
		{"no events", `{"schemaVersion":1,"entities":[],"relations":[]}`, "events"},
		{"no relations", `{"schemaVersion":1,"entities":[],"events":[]}`, "relations"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := EventExtraction([]byte(testCase.body))
			if err == nil {
				t.Fatal("a document missing a required field was accepted")
			}
			violations := violationsOfError(t, err)
			if !namesProperty(violations, testCase.property) {
				t.Fatalf("the refusal does not name %s: %+v", testCase.property, violations)
			}
		})
	}
}

// TestEventExtractionRefusesANonIntegerSchemaVersion covers the revision field.
//
// A string "1" or a null is refused. A numeric 1.0 is ACCEPTED, and that is
// correct rather than a hole: JSON Schema defines equality on numbers, so 1.0
// equals the integer 1, and refusing it would mean departing from the standard to
// reject a value whose meaning is unambiguous. The `type: integer` in the schema
// is documentation and defence in depth here rather than the load-bearing check:
// a mutation removing it left this test green, because `const: 1` alone already
// refuses 1.5.
func TestEventExtractionRefusesANonIntegerSchemaVersion(t *testing.T) {
	for _, version := range []string{"2", "0", `"1"`, "null", "true", "1.5", "[]"} {
		if _, err := EventExtraction([]byte(`{"schemaVersion":` + version + `,"entities":[],"events":[],"relations":[]}`)); err == nil {
			t.Fatalf("schemaVersion %s was accepted", version)
		}
	}
	// The documented integer and its zero-fraction spelling are the same value.
	for _, version := range []string{"1", "1.0"} {
		if _, err := EventExtraction([]byte(`{"schemaVersion":` + version + `,"entities":[],"events":[],"relations":[]}`)); err != nil {
			t.Fatalf("schemaVersion %s must be accepted: %v", version, err)
		}
	}
}

// TestEventExtractionRefusesAClosedVocabularyValue covers the three closed sets
// that mirror a database CHECK. A value outside one would be refused by SQLite
// after the import had already reported success, so it must fail here.
func TestEventExtractionRefusesAClosedVocabularyValue(t *testing.T) {
	cases := []struct {
		name string
		body string
		path string
	}{
		{
			name: "unknown entity type",
			body: `{"schemaVersion":1,"entities":[{"ref":"a","type":"vehicle","canonicalName":"n"}],"events":[],"relations":[]}`,
			path: "/entities/0/type",
		},
		{
			name: "the PRD's PropState, which the model does not have",
			body: `{"schemaVersion":1,"entities":[{"ref":"a","type":"prop_state","canonicalName":"n"}],"events":[],"relations":[]}`,
			path: "/entities/0/type",
		},
		{
			name: "case variant of a documented value",
			body: `{"schemaVersion":1,"entities":[{"ref":"a","type":"Character","canonicalName":"n"}],"events":[],"relations":[]}`,
			path: "/entities/0/type",
		},
		{
			name: "unknown participant role",
			body: `{"schemaVersion":1,"entities":[],"events":[{"ref":"e","name":"n","participants":[{"entityRef":"a","role":"culprit"}]}],"relations":[]}`,
			path: "/events/0/participants/0/role",
		},
		{
			name: "unknown relation type",
			body: `{"schemaVersion":1,"entities":[],"events":[],"relations":[{"sourceRef":"a","targetRef":"b","relationType":"loves"}]}`,
			path: "/relations/0/relationType",
		},
		{
			name: "unknown importance",
			body: `{"schemaVersion":1,"entities":[],"events":[{"ref":"e","name":"n","importance":"huge"}],"relations":[]}`,
			path: "/events/0/importance",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := EventExtraction([]byte(testCase.body))
			if err == nil {
				t.Fatal("a value outside a closed vocabulary was accepted")
			}
			if violations := violationsOfError(t, err); !mentionsPath(violations, testCase.path) {
				t.Fatalf("the refusal does not name %s: %+v", testCase.path, violations)
			}
		})
	}
}

// TestEventExtractionRefusesAStatusField is the structural guarantee that a model
// cannot approve its own output. The schema closes the object, so a status the
// model tried to set is a refusal rather than a silently ignored field.
func TestEventExtractionRefusesAStatusField(t *testing.T) {
	bodies := []string{
		`{"schemaVersion":1,"entities":[{"ref":"a","type":"character","canonicalName":"n","status":"accepted"}],"events":[],"relations":[]}`,
		`{"schemaVersion":1,"status":"success","entities":[],"events":[],"relations":[]}`,
		`{"schemaVersion":1,"entities":[],"events":[{"ref":"e","name":"n","status":"locked"}],"relations":[]}`,
	}
	for index, body := range bodies {
		if _, err := EventExtraction([]byte(body)); err == nil {
			t.Fatalf("body %d set a status and was accepted, so a model can approve its own output", index)
		}
	}
}

// TestEventExtractionRefusesOutOfRangeNumbers covers the numeric bounds, which
// are the ones a model most often exceeds.
func TestEventExtractionRefusesOutOfRangeNumbers(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"confidence above one", `{"schemaVersion":1,"entities":[],"events":[{"ref":"e","name":"n","confidence":1.5}],"relations":[]}`},
		{"negative confidence", `{"schemaVersion":1,"entities":[],"events":[{"ref":"e","name":"n","confidence":-0.1}],"relations":[]}`},
		{"a percentage instead of a fraction", `{"schemaVersion":1,"entities":[],"events":[{"ref":"e","name":"n","confidence":80}],"relations":[]}`},
		{"fractional story time order", `{"schemaVersion":1,"entities":[],"events":[{"ref":"e","name":"n","storyTimeOrder":1.5}],"relations":[]}`},
		{"empty event name", `{"schemaVersion":1,"entities":[],"events":[{"ref":"e","name":""}],"relations":[]}`},
		{"empty canonical name", `{"schemaVersion":1,"entities":[{"ref":"a","type":"character","canonicalName":""}],"events":[],"relations":[]}`},
		{"a ref with a space", `{"schemaVersion":1,"entities":[{"ref":"a b","type":"character","canonicalName":"n"}],"events":[],"relations":[]}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := EventExtraction([]byte(testCase.body)); err == nil {
				t.Fatal("an out-of-range value was accepted")
			}
		})
	}
}

// TestEventExtractionBoundsTheProposalCount covers the collection limits. An
// unbounded document is a way to make one model response cost an unbounded
// number of writes, so the cap is part of the contract rather than a policy the
// writer applies afterwards.
func TestEventExtractionBoundsTheProposalCount(t *testing.T) {
	var builder strings.Builder
	builder.WriteString(`{"schemaVersion":1,"entities":[`)
	for index := 0; index <= extraction.MaxEntities; index++ {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(`{"ref":"e`)
		builder.WriteString(itoa(index))
		builder.WriteString(`","type":"character","canonicalName":"n"}`)
	}
	builder.WriteString(`],"events":[],"relations":[]}`)
	if _, err := EventExtraction([]byte(builder.String())); err == nil {
		t.Fatalf("a document with more than %d entities was accepted", extraction.MaxEntities)
	}
}

// TestEventExtractionRefusesNonJSON covers the bytes that are not a document at
// all. A refusal here is an extraction failure, not a malformed story, and the
// error says so by carrying the parse cause.
func TestEventExtractionRefusesNonJSON(t *testing.T) {
	for _, body := range []string{"", "not json at all", "```json\n{}\n```", "[1,2,3]", `"a string"`, "null"} {
		_, err := EventExtraction([]byte(body))
		if err == nil {
			t.Fatalf("the body %q was accepted", body)
		}
		var domainErr *extraction.Error
		if !errors.As(err, &domainErr) {
			t.Fatalf("the body %q produced %T, want an extraction error", body, err)
		}
	}
}

// TestEventExtractionReportsEveryViolation covers section 14.3's one repair
// attempt: a model told about one problem at a time would need one round trip per
// mistake, and the contract permits one.
func TestEventExtractionReportsEveryViolation(t *testing.T) {
	_, err := EventExtraction([]byte(`{
		"schemaVersion": 1,
		"entities": [{"ref": "a", "type": "vehicle", "canonicalName": ""}],
		"events": [{"ref": "e", "name": ""}],
		"relations": [{"sourceRef": "a", "targetRef": "b", "relationType": "loves"}]
	}`))
	if err == nil {
		t.Fatal("a document with four mistakes was accepted")
	}
	violations := violationsOfError(t, err)
	if len(violations) < 4 {
		t.Fatalf("only %d violations were reported, so a repair round would miss some: %+v", len(violations), violations)
	}
	// Each mistake must be named, or the model would fix one and resubmit.
	for _, path := range []string{"/entities/0/type", "/entities/0/canonicalName", "/events/0/name", "/relations/0/relationType"} {
		if !mentionsPath(violations, path) {
			t.Fatalf("the refusal omits %s: %+v", path, violations)
		}
	}
}

// TestViolationsAreBounded stops a pathological document from producing an error
// longer than the document itself, which would then be sent back to a model.
func TestViolationsAreBounded(t *testing.T) {
	var builder strings.Builder
	builder.WriteString(`{"schemaVersion":1,"entities":[`)
	for index := 0; index < 60; index++ {
		if index > 0 {
			builder.WriteString(",")
		}
		// Every one of these has two problems: an unknown type and an empty name.
		builder.WriteString(`{"ref":"e`)
		builder.WriteString(itoa(index))
		builder.WriteString(`","type":"vehicle","canonicalName":""}`)
	}
	builder.WriteString(`],"events":[],"relations":[]}`)
	_, err := EventExtraction([]byte(builder.String()))
	if err == nil {
		t.Fatal("a document full of unknown types was accepted")
	}
	message := err.Error()
	// The formatter stops after twenty violations and says so.
	if len(message) > 4096 {
		t.Fatalf("the refusal is %d bytes long, which is too much to send back", len(message))
	}
	if !strings.Contains(message, "and more") {
		t.Fatalf("a truncated refusal must say it was truncated: %q", message)
	}
}

// TestSchemaLookupResolvesTheSpecificationPath covers the embed package's
// contract: the path AGENT_CONTRACTS section 4.2 would name resolves to the same
// bytes the validator compiles.
func TestSchemaLookupResolvesTheSpecificationPath(t *testing.T) {
	body, err := schemas.Lookup(schemas.AgentEventExtractionPath)
	if err != nil {
		t.Fatalf("looking up the specification path: %v", err)
	}
	if !strings.Contains(string(body), "event_extraction") && !strings.Contains(string(body), "EventExtraction") {
		t.Fatalf("the path resolved to something else: %q", firstBytes(body, 80))
	}
	if _, err := schemas.Lookup("schemas/agent/no_such_schema.v1.json"); err == nil {
		t.Fatal("an unknown schema path must be an error, not an empty document")
	}
}

// TestEnumsAgreeWithTheDomainVocabularies closes the triangle between the three
// places a closed vocabulary is written down: the SQL CHECK, the Go list, and
// this schema's enums.
//
// The other two sides are already checked — the database package compares the
// CHECK against the Go list mechanically. Nothing compared either against the
// schema until this test, and the gap was real: migration 000014 widened
// entity_type to eight values and the schema still listed six, so extraction
// could never propose the two kinds the migration was written to allow. A
// mutation removing them from the enum left the whole suite green.
//
// The enums are read from the compiled schema by validating probe documents
// rather than by re-parsing the JSON, so this asserts what the validator
// actually enforces instead of what the file appears to say.
func TestEnumsAgreeWithTheDomainVocabularies(t *testing.T) {
	// Every documented entity kind must be proposable.
	for _, kind := range story.EntityTypes {
		body := `{"schemaVersion":1,"entities":[{"ref":"a","type":"` + string(kind) + `","canonicalName":"n"}],"events":[],"relations":[]}`
		if _, err := EventExtraction([]byte(body)); err != nil {
			t.Fatalf("the entity type %q is in the domain and the schema but the contract refuses it: %v", kind, err)
		}
	}
	// And the schema must refuse what the domain refuses, or a document would
	// validate and then fail on the way into SQLite.
	for _, kind := range []string{"vehicle", "prop_state", "Event", "character_state"} {
		body := `{"schemaVersion":1,"entities":[{"ref":"a","type":"` + kind + `","canonicalName":"n"}],"events":[],"relations":[]}`
		if _, err := EventExtraction([]byte(body)); err == nil {
			t.Fatalf("the entity type %q is not in the domain but the contract accepts it", kind)
		}
	}
	// The same for the two other closed sets a participant or a relation carries.
	for _, role := range story.ParticipantRoles {
		body := `{"schemaVersion":1,"entities":[],"events":[{"ref":"e","name":"n","participants":[{"entityRef":"a","role":"` + string(role) + `"}]}],"relations":[]}`
		if _, err := EventExtraction([]byte(body)); err != nil {
			t.Fatalf("the participant role %q is in the domain and the schema but the contract refuses it: %v", role, err)
		}
	}
	for _, relation := range story.RelationTypes {
		body := `{"schemaVersion":1,"entities":[],"events":[],"relations":[{"sourceRef":"a","targetRef":"b","relationType":"` + string(relation) + `"}]}`
		if _, err := EventExtraction([]byte(body)); err != nil {
			t.Fatalf("the relation type %q is in the domain and the schema but the contract refuses it: %v", relation, err)
		}
	}
}

// TestViolationPathsAreJSONPointers pins the form section 14.3 sends back. A path
// that were empty or None-shaped would leave a repair prompt pointing at nothing.
func TestViolationPathsAreJSONPointers(t *testing.T) {
	_, err := EventExtraction([]byte(`{"schemaVersion":1,"entities":[{"ref":"a","type":"vehicle","canonicalName":""}],"events":[],"relations":[]}`))
	if err == nil {
		t.Fatal("an unknown entity type was accepted")
	}
	violations := violationsOfError(t, err)
	for _, violation := range violations {
		if violation.Path == "" {
			t.Fatalf("a violation carries no path: %+v", violation)
		}
		if !strings.HasPrefix(violation.Path, "/") {
			t.Fatalf("the path %q is not a JSON Pointer", violation.Path)
		}
		if violation.Message == "" {
			t.Fatalf("the violation %q carries no message", violation.Path)
		}
	}
}

func firstBytes(body []byte, count int) string {
	if len(body) <= count {
		return string(body)
	}
	return string(body[:count])
}

// violationsOfError extracts the violations from a refusal, failing the test if
// the error is not one of this package's.
func violationsOfError(t *testing.T, err error) []Violation {
	t.Helper()
	var extractionErr *extraction.Error
	if !errors.As(err, &extractionErr) {
		t.Fatalf("the refusal is a %T rather than an extraction error: %v", err, err)
	}
	if len(extractionErr.Violations) == 0 {
		t.Fatalf("the refusal carries no violations: %v", err)
	}
	return extractionErr.Violations
}

// mentionsPath reports whether one violation names a JSON Pointer exactly.
func mentionsPath(violations []Violation, path string) bool {
	for _, violation := range violations {
		if violation.Path == path {
			return true
		}
	}
	return false
}

// namesProperty reports whether one violation complains about a property, in
// either of the two shapes the library uses: the property's own path, or the
// parent object's path with the property named in the message. A missing required
// property produces the second, because there is no value to point at.
func namesProperty(violations []Violation, property string) bool {
	for _, violation := range violations {
		if strings.Contains(violation.Path, "/"+property) {
			return true
		}
		if strings.Contains(violation.Message, "'"+property+"'") {
			return true
		}
	}
	return false
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
