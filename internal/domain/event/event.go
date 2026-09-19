// Package event owns the domain event vocabulary and envelope of
// docs/DOMAIN_MODEL.md §17.
//
// Section 17 fixes two things and leaves the rest to the implementation: the
// list of event names, and the envelope every event carries. This package holds
// both, plus the validation that keeps an event well formed. It performs no I/O
// and never mints an identifier (ADR-0005).
//
// What an event is FOR, in this codebase: it is the record that something
// happened, addressed to whoever needs to know. It is not the authority on what
// happened — the row is. That distinction decides where events are written:
// §16 lists "transaction" and "event" as separate requirements, and this
// package's callers write an event inside the command's transaction when the
// event itself is a governance record (an approval, a user decision) and
// immediately after the write otherwise. ADR-0009 records the line and which
// commands fall on each side.
package event

import "time"

// Type is the name of a domain event.
//
// The set is §17's list. It is closed on purpose: a query that filters by event
// type has to know the vocabulary, and a typo in an event name is a silent hole
// in a stream rather than a failure.
type Type string

const (
	// Project configuration.
	ProjectCreated         Type = "ProjectCreated"
	ProjectSettingsChanged Type = "ProjectSettingsChanged"
	// ProjectRuleLocked is emitted when the lock itself changes, not on every
	// rule edit: §17 names the lock because that is the moment a rule's
	// protection changes.
	ProjectRuleLocked Type = "ProjectRuleLocked"

	// Source documents and chapters.
	SourceDocumentImported     Type = "SourceDocumentImported"
	ChapterBoundariesConfirmed Type = "ChapterBoundariesConfirmed"

	// The story fact layer.
	StoryFactAccepted       Type = "StoryFactAccepted"
	StoryFactConflictOpened Type = "StoryFactConflictOpened"

	// Episodes and the script pipeline.
	EpisodeCreated             Type = "EpisodeCreated"
	StorySkeletonApproved      Type = "StorySkeletonApproved"
	AdaptationStrategyApproved Type = "AdaptationStrategyApproved"
	ScriptVersionCreated       Type = "ScriptVersionCreated"
	ScriptVersionApproved      Type = "ScriptVersionApproved"

	// Assets.
	AssetVersionCreated  Type = "AssetVersionCreated"
	AssetVersionApproved Type = "AssetVersionApproved"

	// Director plan and storyboard.
	DirectorPlanApproved      Type = "DirectorPlanApproved"
	StoryboardVersionApproved Type = "StoryboardVersionApproved"

	// Canvas.
	CanvasProjectionCreated Type = "CanvasProjectionCreated"

	// Workflow, review and the user gate.
	WorkflowStarted      Type = "WorkflowStarted"
	WorkflowStageChanged Type = "WorkflowStageChanged"
	ReviewReportCreated  Type = "ReviewReportCreated"
	UserGateDecided      Type = "UserGateDecided"

	// Generation jobs. These are emitted by the job core (WP-03) rather than by
	// the drama commands; they are in the vocabulary because §17 lists them and
	// a consumer filtering the stream must be able to name them.
	GenerationJobQueued    Type = "GenerationJobQueued"
	GenerationJobSucceeded Type = "GenerationJobSucceeded"
	GenerationJobFailed    Type = "GenerationJobFailed"

	// Memory. Owned by WP-10; declared here so the vocabulary is complete.
	MemoryCreated Type = "MemoryCreated"

	// Staleness.
	UpstreamVersionChanged Type = "UpstreamVersionChanged"
	ArtifactMarkedStale    Type = "ArtifactMarkedStale"

	// Backup. Emitted by the backup service.
	BackupCompleted Type = "BackupCompleted"
)

// Types lists the §17 events in the specification's order.
var Types = []Type{
	ProjectCreated, ProjectSettingsChanged, ProjectRuleLocked,
	SourceDocumentImported, ChapterBoundariesConfirmed,
	StoryFactAccepted, StoryFactConflictOpened,
	EpisodeCreated, StorySkeletonApproved, AdaptationStrategyApproved,
	ScriptVersionCreated, ScriptVersionApproved,
	AssetVersionCreated, AssetVersionApproved,
	DirectorPlanApproved, StoryboardVersionApproved,
	CanvasProjectionCreated,
	WorkflowStarted, WorkflowStageChanged, ReviewReportCreated, UserGateDecided,
	GenerationJobQueued, GenerationJobSucceeded, GenerationJobFailed,
	MemoryCreated,
	UpstreamVersionChanged, ArtifactMarkedStale,
	BackupCompleted,
}

// IsValidType reports whether an event name is one §17 defines.
func IsValidType(value Type) bool {
	for _, candidate := range Types {
		if candidate == value {
			return true
		}
	}
	return false
}

// SchemaVersion is the envelope version this package emits.
//
// It is bumped when the envelope's shape changes, not when an event type is
// added: a consumer reading version 1 can skip an event type it does not know,
// but it cannot parse an envelope whose fields moved.
const SchemaVersion = 1

// AggregateType names the kind of thing an event is about.
//
// §17's envelope carries it, and the values are the aggregate names §3 uses.
// It is a string rather than a closed enum because the set grows with every
// package that emits an event, and a closed list here would need editing by
// packages that own neither this file nor each other.
type AggregateType string

const (
	AggregateProject        AggregateType = "project"
	AggregateSourceDocument AggregateType = "source_document"
	AggregateChapter        AggregateType = "chapter"
	AggregateStoryEntity    AggregateType = "story_entity"
	AggregateStoryEvent     AggregateType = "story_event"
	AggregateStoryConflict  AggregateType = "story_conflict"
	AggregateEpisode        AggregateType = "episode"
	AggregateStorySkeleton  AggregateType = "story_skeleton"
	AggregateStrategy       AggregateType = "adaptation_strategy"
	AggregateScript         AggregateType = "script"
	AggregateAsset          AggregateType = "asset"
	AggregateDirectorPlan   AggregateType = "director_plan"
	AggregateStoryboard     AggregateType = "storyboard"
	AggregateCanvas         AggregateType = "canvas"
	AggregateWorkflow       AggregateType = "workflow"
	AggregateReview         AggregateType = "review"
	AggregateStaleness      AggregateType = "staleness"
	AggregateBackup         AggregateType = "backup"
	AggregateMemory         AggregateType = "memory"
	AggregateJob            AggregateType = "generation_job"
)

// MaxAggregateIDLength bounds the identifier an event names, mirroring the
// schema's CHECK.
const MaxAggregateIDLength = 200

// MaxTraceIDLength bounds the correlation id.
const MaxTraceIDLength = 64

// MaxPayloadBytes bounds the serialised payload.
//
// A payload is a summary of the change, not a copy of the row: §2.6 forbids
// using JSON where a column belongs, and §17's payload exists so a consumer can
// react without re-reading. A payload large enough to hold a script would make
// the event stream a second copy of the database, so it is bounded.
const MaxPayloadBytes = 16 * 1024

// Event is one domain event in its §17 envelope.
//
// Field names mirror the envelope exactly, so a reader comparing this struct to
// the specification does not have to translate.
type Event struct {
	// EventID is the event's own identifier. ADR-0005 makes it a UUIDv7 minted
	// by the application layer, which is what lets a consumer de-duplicate a
	// replayed stream.
	EventID string
	// EventType is the §17 name.
	EventType Type
	// SchemaVersion is the envelope version.
	SchemaVersion int
	// AggregateType and AggregateID name what the event is about.
	AggregateType AggregateType
	AggregateID   string
	// ProjectID scopes the event, because every drama query is project-scoped.
	ProjectID string
	// OccurredAt is when it happened, in UTC (§2.2).
	OccurredAt time.Time
	// TraceID correlates the events of one user action or workflow run.
	TraceID string
	// Payload is a JSON object summarising the change. It may be empty, which
	// means the event carries only its envelope.
	Payload string
}

// Validate checks an event before it is stored.
func (e Event) Validate() error {
	if !IsValidType(e.EventType) {
		return InvalidError("The event type is not recognised.")
	}
	if e.SchemaVersion != SchemaVersion {
		return InvalidError("The event envelope version is not recognised.")
	}
	if e.AggregateType == "" {
		return InvalidError("An event must name the kind of thing it is about.")
	}
	if trimmed := trimSpace(e.AggregateID); trimmed == "" {
		return InvalidError("An event must name the thing it is about.")
	}
	if len([]rune(e.AggregateID)) > MaxAggregateIDLength {
		return InvalidError("The event's subject identifier is too long.")
	}
	if trimSpace(e.ProjectID) == "" {
		return InvalidError("An event must belong to a project.")
	}
	if e.OccurredAt.IsZero() {
		return InvalidError("An event must record when it happened.")
	}
	if len([]rune(e.TraceID)) > MaxTraceIDLength {
		return InvalidError("The event's trace identifier is too long.")
	}
	if len(e.Payload) > MaxPayloadBytes {
		return InvalidError("The event payload is too large.")
	}
	if e.Payload != "" && !isJSONObject(e.Payload) {
		return InvalidError("The event payload must be a JSON object.")
	}
	return nil
}

// isJSONObject reports whether the text is a JSON object literal.
//
// The check is shallow on purpose: it rejects the shapes that would make the
// column unreadable to a consumer (an array, a bare scalar, a fragment) without
// pretending to validate fields this package does not own.
func isJSONObject(value string) bool {
	trimmed := trimSpace(value)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return false
	}
	return true
}

// trimSpace trims the ASCII whitespace the envelope's fields can carry.
//
// It is spelled out rather than imported from strings so this package's import
// list stays empty: the domain layer takes no dependency it does not need, and
// the check is small enough that a standard-library import would be noise.
func trimSpace(value string) string {
	start := 0
	for start < len(value) && isSpace(value[start]) {
		start++
	}
	end := len(value)
	for end > start && isSpace(value[end-1]) {
		end--
	}
	return value[start:end]
}

func isSpace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

// New builds a validated event.
//
// The identifier, the clock and the payload are supplied by the caller, because
// the application layer mints identifiers (ADR-0005) and owns the time source
// (§2.2), and because only the emitting command knows what belongs in the
// payload.
func New(id string, eventType Type, aggregateType AggregateType, aggregateID, projectID string, occurredAt time.Time, traceID, payload string) (Event, error) {
	record := Event{
		EventID:       trimSpace(id),
		EventType:     eventType,
		SchemaVersion: SchemaVersion,
		AggregateType: aggregateType,
		AggregateID:   trimSpace(aggregateID),
		ProjectID:     trimSpace(projectID),
		OccurredAt:    occurredAt.UTC(),
		TraceID:       trimSpace(traceID),
		Payload:       payload,
	}
	if record.EventID == "" {
		return Event{}, InvalidError("An event needs an identifier.")
	}
	if err := record.Validate(); err != nil {
		return Event{}, err
	}
	return record, nil
}
