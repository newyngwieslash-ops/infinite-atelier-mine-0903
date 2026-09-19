// Package extraction turns a chapter's text into story fact candidates.
//
// It implements docs/ROADMAP.md scope items 6 and 7: the EventExtraction output
// schema, a Mock agent, and the candidate workflow. The rules that decide whether
// a proposal is shaped correctly live here as pure functions, so the same checks
// run against a mock in a test and against a provider later.
//
// Two boundaries this package exists to hold, both from SECURITY:
//
//   - A chapter is UNTRUSTED DATA. It is read, quoted and stored, and nothing in
//     its text is interpreted as an instruction. The injection fixture in
//     testdata/malicious-imports exists so a test can assert that.
//   - An extractor PROPOSES. Nothing here can approve a fact, set a status other
//     than 'candidate', or write a version. PRD FR-030 keeps a user or rule gate
//     in front of every confirmed fact, and the shape of the output makes the
//     gate structural rather than a convention: the schema has no status field
//     for a model to fill in.
package extraction

import (
	"errors"
	"strings"
)

// SchemaVersion is the revision of the EventExtraction contract this package
// accepts. A document declaring another version is refused rather than parsed
// with this revision's assumptions.
const SchemaVersion = 1

// MaxProposals bounds one document, so a runaway model cannot ask the importer
// to write an unbounded number of rows. The per-collection limits in the schema
// are the same idea one level down.
const (
	MaxEntities  = 200
	MaxEvents    = 200
	MaxRelations = 400
)

// Document is a validated extraction result.
//
// It is the schema's shape after validation, and the only way to obtain one is
// through Parse, which validates. There is deliberately no way to build a
// Document and skip the checks: the service takes a Document rather than raw
// bytes, so a caller cannot hand unvalidated model output to the writer.
type Document struct {
	// Summary is a label for the user's review. Nothing parses it.
	Summary string
	// Entities are proposed in the order the model listed them. The order is
	// kept because the service mints ids in it, and a stable order makes a
	// re-run's identifiers comparable.
	Entities []Entity
	Events   []Event
	// Relations reference entities and events by their local ref.
	Relations []Relation
}

// Entity is one proposed story entity.
type Entity struct {
	// Ref is a local key, unique within the document. It is not a database id:
	// the model cannot know an id the repository has not minted.
	Ref string
	// Type is one of the eight kinds the schema stores.
	Type string
	// CanonicalName is the name the graph should prefer.
	CanonicalName string
	// Aliases are the other names the chapter uses for the same entity.
	Aliases []string
	Summary string
}

// Event is one proposed story event.
type Event struct {
	Ref string
	// Name is a short label, and it is required: an event the user cannot read
	// in a list is not reviewable.
	Name        string
	Description string
	EventType   string
	// StoryTimeText is the time as the chapter states it.
	StoryTimeText string
	// StoryTimeOrder is the position in story time, which is not reading order.
	// Nil means the chapter did not say.
	StoryTimeOrder *int
	// LocationRef is a local ref, or empty.
	LocationRef   string
	CauseSummary  string
	ResultSummary string
	Importance    string
	Confidence    float64
	Participants  []Participant
}

// Participant is how one entity took part in one event.
type Participant struct {
	EntityRef   string
	Role        string
	StateBefore string
	StateAfter  string
}

// Relation is one proposed relation between two entities.
type Relation struct {
	SourceRef string
	TargetRef string
	Type      string
	// ValidFromEventRef and ValidToEventRef are local event refs, or empty.
	ValidFromEventRef string
	ValidToEventRef   string
	Confidence        float64
}

// Refs returns every local ref the document defines, split by kind.
//
// The service uses it to resolve a reference to the id it minted, and to refuse
// a document that names a ref nothing defines. A dangling reference is refused
// rather than dropped: a relation whose endpoint silently vanished would be a
// fact the user never sees and cannot correct.
func (d Document) Refs() (entities map[string]bool, events map[string]bool) {
	entities = make(map[string]bool, len(d.Entities))
	for _, entity := range d.Entities {
		entities[entity.Ref] = true
	}
	events = make(map[string]bool, len(d.Events))
	for _, event := range d.Events {
		events[event.Ref] = true
	}
	return entities, events
}

// Violation is one reason a document was refused.
//
// It carries a JSON path and a message rather than the raw document, because
// AGENT_CONTRACTS section 14.3 sends compact validation errors back to the model
// and the raw text may hold anything — including content that tries to be an
// instruction.
type Violation struct {
	// Path is a JSON Pointer to the offending value, such as
	// `/events/2/name`.
	Path string
	// Message describes what is wrong in one sentence.
	Message string
}

// Error is a refusal, with every violation found rather than only the first.
//
// All of them, because section 14.3 allows exactly one repair attempt: a model
// told about one problem at a time would need as many round trips as it has
// mistakes, and the contract permits one.
type Error struct {
	Violations []Violation
	// Cause is set when the bytes were not JSON at all.
	Cause error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Violations) == 0 {
		if e.Cause != nil {
			return "The extractor's output could not be read as JSON."
		}
		return "The extractor's output did not match the expected shape."
	}
	var builder strings.Builder
	builder.WriteString("The extractor's output did not match the expected shape: ")
	for index, violation := range e.Violations {
		if index > 0 {
			builder.WriteString("; ")
		}
		builder.WriteString(violation.Path)
		builder.WriteString(" ")
		builder.WriteString(violation.Message)
		// Bounded, so a document with hundreds of violations does not produce an
		// error longer than the document.
		if index == 19 {
			builder.WriteString("; and more")
			break
		}
	}
	return builder.String()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// RequestError is a refusal of the request rather than of the model's output:
// no extractor is attached, the chapter does not exist, or its text is empty.
//
// It is separate from Error because the two are handled differently. A malformed
// model response is worth one repair attempt (AGENT_CONTRACTS §14.3); an
// unavailable extractor is not worth retrying at all, and the caller needs to be
// able to tell them apart to decide.
//
// It carries a safe message and a code, and no payload: SECURITY §17 requires a
// security-shaped failure to be classified, and a caller that logs this message
// must not be logging document text.
type RequestError struct {
	// Code is a stable identifier the desktop layer maps.
	Code string
	// SafeMessage is what the user is shown.
	SafeMessage string
	// Retriable says whether trying again could help.
	Retriable bool
	// Cause is an optional wrapped failure.
	Cause error
}

func (e *RequestError) Error() string {
	if e == nil {
		return ""
	}
	return e.SafeMessage
}

func (e *RequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Codes for the refusals this domain reports.
//
// They are spelled the way the other domains spell a category — lower case with
// underscores — because the desktop layer builds a stable application code from
// them ("DRAMA_" plus the upper-cased value). A dotted or prefixed code would
// produce a code unlike every other one the frontend already handles.
const (
	// CodeUnavailable means no extractor is configured. It is deliberately not
	// retriable: nothing about waiting will attach one.
	CodeUnavailable = "unavailable"
	// CodeInvalidRequest means the request itself was wrong.
	CodeInvalidRequest = "invalid_request"
)

// InvalidRequestError refuses a malformed request.
func InvalidRequestError(message string) *RequestError {
	return &RequestError{Code: CodeInvalidRequest, SafeMessage: message}
}

// UnavailableError reports that no extractor is attached.
func UnavailableError() *RequestError {
	return &RequestError{
		Code:        CodeUnavailable,
		SafeMessage: "No document reader is configured, so extraction cannot run.",
	}
}

// AsError extracts a request error from a wrapped chain.
func AsError(err error) (*RequestError, bool) {
	var target *RequestError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
