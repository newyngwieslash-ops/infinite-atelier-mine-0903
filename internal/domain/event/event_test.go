package event

import (
	"strings"
	"testing"
	"time"
)

// TestEventVocabularyMatchesSection17 pins the event list against §17's, in
// order. A missing event name is a stream a consumer silently never sees, so the
// list is asserted literally rather than by count.
func TestEventVocabularyMatchesSection17(t *testing.T) {
	documented := []string{
		"ProjectCreated", "ProjectSettingsChanged", "ProjectRuleLocked",
		"SourceDocumentImported", "ChapterBoundariesConfirmed",
		"StoryFactAccepted", "StoryFactConflictOpened",
		"EpisodeCreated", "StorySkeletonApproved", "AdaptationStrategyApproved",
		"ScriptVersionCreated", "ScriptVersionApproved",
		"AssetVersionCreated", "AssetVersionApproved",
		"DirectorPlanApproved", "StoryboardVersionApproved",
		"CanvasProjectionCreated",
		"WorkflowStarted", "WorkflowStageChanged", "ReviewReportCreated", "UserGateDecided",
		"GenerationJobQueued", "GenerationJobSucceeded", "GenerationJobFailed",
		"MemoryCreated",
		"UpstreamVersionChanged", "ArtifactMarkedStale",
		"BackupCompleted",
	}
	if len(Types) != len(documented) {
		t.Fatalf("the vocabulary lists %d events, §17 names %d", len(Types), len(documented))
	}
	for index, want := range documented {
		if got := string(Types[index]); got != want {
			t.Fatalf("event %d is %q, want %q", index, got, want)
		}
		if !IsValidType(Type(want)) {
			t.Fatalf("§17 event %q is not recognised", want)
		}
	}
	// A misspelling or a case variant must be refused: an event whose name is
	// wrong is a hole in the stream that no query would report.
	for _, rejected := range []string{"", "projectCreated", "ProjectCreated ", "ScriptApproved", "ProjectDeleted"} {
		if IsValidType(Type(rejected)) {
			t.Fatalf("undocumented event %q is accepted", rejected)
		}
	}
}

func sampleEvent(t *testing.T) Event {
	t.Helper()
	record, err := New(
		"0193f0a1-7c2e-7d31-9a4b-5c6d7e8f9012",
		ScriptVersionApproved,
		AggregateScript,
		"sv-1",
		"project-1",
		time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC),
		"trace-1",
		`{"versionNumber":2}`,
	)
	if err != nil {
		t.Fatalf("building a well-formed event failed: %v", err)
	}
	return record
}

// TestNewBuildsAValidEnvelope covers the happy path and the fields §17 fixes.
func TestNewBuildsAValidEnvelope(t *testing.T) {
	record := sampleEvent(t)
	if record.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", record.SchemaVersion, SchemaVersion)
	}
	if record.EventType != ScriptVersionApproved {
		t.Fatalf("event type = %q", record.EventType)
	}
	if record.AggregateType != AggregateScript || record.AggregateID != "sv-1" {
		t.Fatalf("aggregate = %s/%s", record.AggregateType, record.AggregateID)
	}
	if record.ProjectID != "project-1" || record.TraceID != "trace-1" {
		t.Fatalf("scope = %s/%s", record.ProjectID, record.TraceID)
	}
	// The timestamp is normalised to UTC, which §2.2 requires of stored times.
	if record.OccurredAt.Location() != time.UTC {
		t.Fatalf("occurredAt is in %v, want UTC", record.OccurredAt.Location())
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("a freshly built event does not validate: %v", err)
	}
}

// TestValidateRejectsMalformedEvents covers every field the envelope requires.
func TestValidateRejectsMalformedEvents(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Event)
	}{
		{"unknown event type", func(e *Event) { e.EventType = "ScriptApproved" }},
		{"empty event type", func(e *Event) { e.EventType = "" }},
		{"wrong schema version", func(e *Event) { e.SchemaVersion = 2 }},
		{"missing aggregate type", func(e *Event) { e.AggregateType = "" }},
		{"missing aggregate id", func(e *Event) { e.AggregateID = "  " }},
		{"missing project", func(e *Event) { e.ProjectID = "" }},
		{"missing timestamp", func(e *Event) { e.OccurredAt = time.Time{} }},
		{"over-long trace id", func(e *Event) { e.TraceID = strings.Repeat("t", MaxTraceIDLength+1) }},
		{"over-long aggregate id", func(e *Event) { e.AggregateID = strings.Repeat("a", MaxAggregateIDLength+1) }},
		{"payload that is not an object", func(e *Event) { e.Payload = `["not", "an", "object"]` }},
		{"payload that is a bare scalar", func(e *Event) { e.Payload = `"text"` }},
		{"payload that is a fragment", func(e *Event) { e.Payload = `{unterminated` }},
		{"over-large payload", func(e *Event) {
			e.Payload = "{" + strings.Repeat("a", MaxPayloadBytes) + "}"
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			record := sampleEvent(t)
			testCase.mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatal("a malformed event was accepted")
			}
		})
	}
	// An empty payload is allowed: an event may carry only its envelope, which
	// is what an event whose meaning is entirely in its type looks like.
	empty := sampleEvent(t)
	empty.Payload = ""
	if err := empty.Validate(); err != nil {
		t.Fatalf("an event with no payload was refused: %v", err)
	}
	// A trace id is optional too: a manual command has no workflow to correlate
	// with, and demanding one would force a fabricated value.
	noTrace := sampleEvent(t)
	noTrace.TraceID = ""
	if err := noTrace.Validate(); err != nil {
		t.Fatalf("an event with no trace id was refused: %v", err)
	}
}

// TestNewRequiresAnIdentifier covers the one field New supplies rather than
// validates: ADR-0005 makes the application layer mint it, so an empty value
// means the caller forgot rather than that the event is anonymous.
func TestNewRequiresAnIdentifier(t *testing.T) {
	_, err := New("", ProjectCreated, AggregateProject, "p-1", "p-1", time.Now(), "", "")
	if err == nil {
		t.Fatal("an event with no identifier was built")
	}
	if _, ok := AsError(err); !ok {
		t.Fatalf("error = %v, want a domain error", err)
	}
}

// TestFrameTrimsSurroundingSpace keeps a whitespace-only id from passing the
// emptiness check.
func TestFrameTrimsSurroundingSpace(t *testing.T) {
	record := sampleEvent(t)
	record.AggregateID = "  sv-1  "
	if err := record.Validate(); err != nil {
		t.Fatalf("a padded identifier was refused: %v", err)
	}
	// The trim is what New applies; Validate tolerates padding because a stored
	// row was already trimmed on the way in.
	built, err := New("id-1", ProjectCreated, AggregateProject, "  p-1  ", " p-1 ", time.Now(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if built.AggregateID != "p-1" || built.ProjectID != "p-1" {
		t.Fatalf("New did not trim: %+v", built)
	}
}
