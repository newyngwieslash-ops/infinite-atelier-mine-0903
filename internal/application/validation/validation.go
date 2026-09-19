// Package validation compiles the embedded agent schemas and checks a model's
// output against them.
//
// It exists so the schema file is the single statement of the contract. The
// alternative — describing the same shape a second time in Go struct tags and
// hoping the two agree — is the drift this repository has already been bitten by
// twice: a vocabulary correct in Go and stale in SQL, and a parity guard reading
// a definition the database had replaced. Here the JSON Schema is authoritative
// and this package only reports what it says.
//
// Validation is deterministic and total: it returns every violation rather than
// the first, because AGENT_CONTRACTS section 14.3 allows exactly one repair
// attempt and a model told about one problem at a time would need as many round
// trips as it has mistakes.
package validation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/extraction"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/schemas"
)

// Violation is re-exported from the domain so a caller has one type to handle.
type Violation = extraction.Violation

// compiled caches each schema. A schema is immutable at runtime, so compiling it
// per call would only add cost and a failure mode.
var (
	once     sync.Once
	compiled *jsonschema.Schema
	compile  error
)

// eventExtractionSchema compiles the embedded contract once.
func eventExtractionSchema() (*jsonschema.Schema, error) {
	once.Do(func() {
		body, err := schemas.AgentEventExtraction()
		if err != nil {
			compile = err
			return
		}
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
		if err != nil {
			compile = fmt.Errorf("the embedded event extraction schema is not valid JSON: %w", err)
			return
		}
		compiler := jsonschema.NewCompiler()
		// The schema is addressed by the $id it declares, and adding it under that
		// same URL is what lets a $ref inside it resolve without a loader.
		const schemaURL = "https://infinite-atelier.invalid/schemas/agent/event_extraction.v1.json"
		if err := compiler.AddResource(schemaURL, document); err != nil {
			compile = fmt.Errorf("registering the event extraction schema failed: %w", err)
			return
		}
		compiled, compile = compiler.Compile(schemaURL)
	})
	return compiled, compile
}

// EventExtraction validates raw extractor output against the contract and
// returns it as a domain document.
//
// The returned document is the only way the application layer can obtain one, so
// an unvalidated document cannot reach the writer. Every refusal carries every
// violation, bounded by the caller's own error formatting.
func EventExtraction(raw []byte) (extraction.Document, error) {
	schema, err := eventExtractionSchema()
	if err != nil {
		// A schema that will not compile is a build defect rather than bad input,
		// so it is reported as one and no document is accepted.
		return extraction.Document{}, fmt.Errorf("the event extraction schema is unusable: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return extraction.Document{}, &extraction.Error{Cause: err}
	}
	if err := schema.Validate(instance); err != nil {
		return extraction.Document{}, &extraction.Error{Violations: violationsOf(err)}
	}
	// The shape is right, so the document can be read. The read cannot fail in a
	// way validation did not already catch: every field it touches is required by
	// the schema, which is why the second step returns no error.
	return readDocument(instance), nil
}

// violationsOf flattens the library's error tree into one entry per problem.
//
// Each entry names the RULE that failed and the path it failed at, and stops
// there. That is a deliberate limit on what leaves this function, because these
// strings are what section 14.3 sends back to a model for the repair round, and a
// model's output quotes a document that is untrusted input. The library's own
// detailed message would name the offending value — it renders the ref that did
// not match the pattern, the enum value it did not recognise — so it is not used.
//
// What is used is the keyword path, which the library draws from the schema's own
// vocabulary: "type", "enum", "required", "maxLength" and the rest. That is a
// closed set the schema author controls, it names exactly what to fix, and it
// cannot carry a byte of the document.
//
// The library's basic message is kept only where it is already value-free, which
// is the `required` and `additionalProperties` families. `additionalProperties`
// DOES name the offending property, so its list is dropped here — a property name
// is attacker-chosen text and has no business in a prompt.
func violationsOf(err error) []Violation {
	validationErr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []Violation{{Path: "/", Message: "the document did not match the contract"}}
	}
	violations := make([]Violation, 0, 8)
	var walk func(node *jsonschema.ValidationError)
	walk = func(node *jsonschema.ValidationError) {
		if len(node.Causes) > 0 {
			for _, cause := range node.Causes {
				walk(cause)
			}
			return
		}
		// InstanceLocation is the token list; the JSON Pointer is rendered from it
		// here because the library's Basic form is what did that conversion, and
		// this walk deliberately does not use that form.
		violations = append(violations, Violation{Path: jsonPointer(node.InstanceLocation), Message: ruleName(node)})
	}
	walk(validationErr)
	// A document that failed without a single leaf is still a refusal, so the
	// result is never an empty error list attached to an error.
	if len(violations) == 0 {
		violations = append(violations, Violation{Path: "/", Message: "the document did not match the contract"})
	}
	return dedupeViolations(violations)
}

// ruleName renders the schema rule that failed, without any instance value.
//
// The keyword path is preferred because it is the schema's own vocabulary and
// therefore closed. A keyword with no path falls back to the library's message,
// which is bounded — and the two families that quote an instance value are
// special-cased away rather than trusted to behave.
func ruleName(node *jsonschema.ValidationError) string {
	// `required` is the one keyword whose complaint is useless without the name it
	// is about, and the name is safe: it comes from the SCHEMA's required list, so
	// it is a value this repository chose rather than one the document did. A
	// model told only "does not satisfy required" cannot act; told which property
	// is missing, it can.
	if required, ok := node.ErrorKind.(*kind.Required); ok && len(required.Missing) > 0 {
		return "is missing the required property '" + strings.Join(required.Missing, "', '") + "'"
	}
	if node.ErrorKind != nil {
		if path := node.ErrorKind.KeywordPath(); len(path) > 0 {
			return "does not satisfy " + strings.Join(path, "/")
		}
	}
	message := node.Error()
	// `additional properties 'x' not allowed` names a property the INSTANCE chose,
	// so the name is dropped and only the rule is kept. This is the case the
	// sanitising exists for: that name is attacker-chosen text.
	if strings.HasPrefix(message, "additional propert") {
		return "has a property the contract does not define"
	}
	return truncate(sanitiseMessage(message))
}

// sanitiseMessage drops a quoted instance value from a library message.
//
// The library quotes instance values in several keywords (an enum's value, a
// regex that did not match, a format that did not parse). Rather than enumerate
// them, anything inside single quotes is removed, which is where every one of
// them appears. A message that loses too much becomes generic, and generic is the
// safe direction: a repair round with a vague complaint is better than a prompt
// carrying text from the document.
func sanitiseMessage(message string) string {
	var builder strings.Builder
	quoted := false
	for _, character := range message {
		switch character {
		case '\'':
			quoted = !quoted
			continue
		case '"':
			continue
		}
		if !quoted {
			builder.WriteRune(character)
		}
	}
	cleaned := strings.Join(strings.Fields(builder.String()), " ")
	if cleaned == "" {
		return "the document did not match the contract"
	}
	return cleaned
}

// truncate bounds a message, so a violation cannot echo a long stretch of the
// document back into a repair prompt.
func truncate(message string) string {
	const limit = 200
	if len(message) <= limit {
		return message
	}
	return message[:limit] + "…"
}

// dedupeViolations removes repeats while keeping the order the library reported,
// which is document order.
func dedupeViolations(violations []Violation) []Violation {
	seen := make(map[string]bool, len(violations))
	out := make([]Violation, 0, len(violations))
	for _, violation := range violations {
		key := violation.Path + "\x00" + violation.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, violation)
	}
	return out
}

// readDocument converts a validated instance into the domain document.
//
// Every accessor here assumes the schema accepted the value, and the schema
// requires every field this reads, so the accessor cannot miss. The pattern is
// deliberate: the alternative is a second set of checks that could disagree with
// the schema, which is the thing this package is arranged to avoid.
//
// Numbers arrive as json.Number rather than float64, because the library decodes
// with UseNumber so that a large integer keeps its precision while being
// validated. A reader that type-asserted float64 would silently read every number
// as absent — which is what happened here until a test caught it.
func readDocument(instance any) extraction.Document {
	object, _ := instance.(map[string]any)
	document := extraction.Document{
		Summary: stringField(object, "summary"),
	}
	for _, item := range objectSlice(object, "entities") {
		entity := extraction.Entity{
			Ref:           stringField(item, "ref"),
			Type:          stringField(item, "type"),
			CanonicalName: stringField(item, "canonicalName"),
			Summary:       stringField(item, "summary"),
		}
		for _, alias := range stringSlice(item, "aliases") {
			entity.Aliases = append(entity.Aliases, alias)
		}
		document.Entities = append(document.Entities, entity)
	}
	for _, item := range objectSlice(object, "events") {
		event := extraction.Event{
			Ref:           stringField(item, "ref"),
			Name:          stringField(item, "name"),
			Description:   stringField(item, "description"),
			EventType:     stringField(item, "eventType"),
			StoryTimeText: stringField(item, "storyTimeText"),
			LocationRef:   stringField(item, "locationRef"),
			CauseSummary:  stringField(item, "causeSummary"),
			ResultSummary: stringField(item, "resultSummary"),
			Importance:    stringField(item, "importance"),
			Confidence:    numberField(item, "confidence"),
		}
		if order, ok := integerField(item, "storyTimeOrder"); ok {
			event.StoryTimeOrder = &order
		}
		for _, raw := range objectSlice(item, "participants") {
			event.Participants = append(event.Participants, extraction.Participant{
				EntityRef:   stringField(raw, "entityRef"),
				Role:        stringField(raw, "role"),
				StateBefore: stringField(raw, "stateBefore"),
				StateAfter:  stringField(raw, "stateAfter"),
			})
		}
		document.Events = append(document.Events, event)
	}
	for _, item := range objectSlice(object, "relations") {
		document.Relations = append(document.Relations, extraction.Relation{
			SourceRef:         stringField(item, "sourceRef"),
			TargetRef:         stringField(item, "targetRef"),
			Type:              stringField(item, "relationType"),
			ValidFromEventRef: stringField(item, "validFromEventRef"),
			ValidToEventRef:   stringField(item, "validToEventRef"),
			Confidence:        numberField(item, "confidence"),
		})
	}
	return document
}

// stringField reads a string, treating an absent or null value as empty. The
// schema allows null for the optional text fields, so absence and null are the
// same thing by the time the document is read.
//
// The schema's pattern for a ref rejects a value that is only whitespace, so no
// trimming happens here: the value is already the one the contract accepted.
func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

// numberField reads a JSON number. The schema bounds every number it declares, so
// a value that fails to parse cannot reach here — but a value that fails to parse
// would have to be a type the schema refused, so the fallback is zero rather than
// an error the caller cannot act on.
func numberField(object map[string]any, key string) float64 {
	number, ok := object[key].(json.Number)
	if !ok {
		return 0
	}
	value, err := number.Float64()
	if err != nil {
		return 0
	}
	return value
}

// integerField reads a JSON integer, reporting whether one was present.
//
// The bool is what distinguishes "the chapter did not say" from "the chapter said
// zero": storyTimeOrder is a position in story time, so zero is a real position
// and unknown must not collapse into it.
func integerField(object map[string]any, key string) (int, bool) {
	number, ok := object[key].(json.Number)
	if !ok {
		return 0, false
	}
	value, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return int(value), true
}

func objectSlice(object map[string]any, key string) []map[string]any {
	raw, ok := object[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

func stringSlice(object map[string]any, key string) []string {
	raw, ok := object[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

// jsonPointer renders a token list as a JSON Pointer.
//
// RFC 6901 escapes two characters: `~` becomes `~0` and `/` becomes `~1`. A token
// list built from array indices never contains either, but a property name can —
// and an unescaped pointer would address a different value than the one the
// violation is about, which in a repair prompt means the model edits the wrong
// field.
func jsonPointer(tokens []string) string {
	if len(tokens) == 0 {
		return "/"
	}
	var builder strings.Builder
	for _, token := range tokens {
		builder.WriteByte('/')
		for _, character := range token {
			switch character {
			case '~':
				builder.WriteString("~0")
			case '/':
				builder.WriteString("~1")
			default:
				builder.WriteRune(character)
			}
		}
	}
	return builder.String()
}
