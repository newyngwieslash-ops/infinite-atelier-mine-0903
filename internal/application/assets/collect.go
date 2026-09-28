package assets

import (
	"encoding/json"
	"context"
	"strings"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
)

// collect.go is the application command behind audio collection, and the reason
// it exists is a defect the 2026-09-26 audit named: the collector ran
// AttachJobResult, then ApproveVersion, then AddUsage — three commands, three
// commits. A failure between them left a row set the mix cannot read, and the
// audit's completion standard says a retry must end with a COMPLETE version,
// approval and usage, with no orphan candidate and no "already collected but
// the mix finds nothing" state.
//
// # One transaction, one command
//
// The atomic write lives in the repository (its `CollectJobResultVersion`),
// because a transaction is a storage concern. What this service adds is the
// facts the storage layer must not decide:
//
//   - the identifiers: version and usage ids are minted here (ADR-0005), so a
//     retry mints fresh ones and never reuses an id an earlier attempt wrote;
//   - the provenance: the prompt, provider, model and voice are carried from
//     the job onto the version, the same facts AttachJobResult records;
//   - the events: a NEW version emits AssetVersionCreated and
//     AssetVersionApproved through the same transaction, because an approval
//     with no governance record is the one outcome a §16 stream must not have.
//
// # The repeat rule
//
// A restarted collection asks about a job it already collected. The answer is
// "already there" — but only after the repeat has REPAIRED whatever the
// interrupted first attempt left behind (a candidate with no approval, an
// approval with no usage). Repairing on repeat is what makes the caller's retry
// idempotent without being a no-op.
//
// # The role the mix reads
//
// The consumer type and role arrive from the caller — the audio collector names
// `shot` and `audio_dialogue`/`audio_effect` — because which rows the export's
// join reads is a read-side contract, and the write side records what the
// caller states rather than inferring it.

// TransactionRunner is the port that scopes a command to one database
// transaction.
//
// It mirrors `projects.UnitOfWork`, declared there for the import's
// bookkeeping. The application layer declares the port rather than importing
// the infrastructure type, which keeps the dependency direction that §7.2
// fixes: application → ports, never application → infrastructure.
type TransactionRunner interface {
	// WithinTx runs fn inside one transaction. The function's error rolls the
	// transaction back; a nil error commits.
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// CollectEvent is one governance record the collection transaction writes.
//
// It wraps the domain event rather than exposing the event package's own type
// through the storage port, and its zero value is skipped by the writer, which
// is how a build without a recorder collects without events.
type CollectEvent struct {
	event event.Event
}

// Empty reports whether the record carries nothing to write — the value a
// build without a recorder passes, and what the storage writer skips.
func (c CollectEvent) Empty() bool {
	return c.event.EventID == "" || c.event.EventType == ""
}

// Record returns the domain event the transaction writes.
func (c CollectEvent) Record() event.Event {
	return c.event
}

// CollectStorageRequest is the full storage shape of one collection: the
// collect facts, the identifiers the application minted, and the governance
// events the transaction records when the version is new.
type CollectStorageRequest struct {
	AssetID          string
	JobID            string
	Prompt           string
	ProviderConfigID string
	ModelConfigID    string
	ModelParameters  string
	SourceAgentRunID string
	CreatedByID      string
	// Files are the committed objects the job produced, shape-validated by the
	// application layer. The schema's FK to file_objects is the check that
	// they exist.
	Files []AttachJobFile
	// Usage is the consumer row the mix reads; its AssetVersionID names the
	// version this command creates.
	Usage asset.Usage
	// VersionID is the identifier the new version is written under — minted by
	// the application so a retry cannot collide with a half-written id.
	VersionID string
	// CreatedEvent and ApprovedEvent are the governance records written in the
	// same transaction when the version is NEW. Zero values are skipped.
	CreatedEvent  CollectEvent
	ApprovedEvent CollectEvent
}

// CollectedJobVersion is the result of one collection.
type CollectedJobVersion struct {
	// VersionID is the version the collection produced or found.
	VersionID string
	// VersionNumber is that version's number within its asset.
	VersionNumber int
	// Approved records that the version is the asset's current approved one
	// when this command returned. A repeat that found the version already
	// approved reports true as well — it is the state the caller needs.
	Approved bool
	// Duplicate marks a repeat: the job had already produced a version of this
	// asset. The version's approval and usage are complete either way.
	Duplicate bool
}

// CollectJobResultRequest names the job result to collect and the rows its
// collection writes.
type CollectJobResultRequest struct {
	AssetID string
	JobID   string
	// Prompt is what was sent to the provider, carried onto the version.
	Prompt string
	// ProviderConfigID and ModelConfigID name what produced it.
	ProviderConfigID string
	ModelConfigID    string
	// ModelParameters carries the reproduction facts — for audio, the voice.
	ModelParameters string
	// SourceAgentRunID and CreatedByID are the provenance AttachJobResult
	// records.
	SourceAgentRunID string
	CreatedByID      string
	// Files are the committed objects the job produced.
	Files []AttachJobFile
	// ConsumerType and ConsumerID name what consumes the version, and
	// UsageRole is what it is used as (`audio_dialogue`, `audio_effect`).
	ConsumerType asset.ConsumerType
	ConsumerID   string
	UsageRole    string
	// ProjectID files the governance events under the project the asset
	// belongs to. Empty resolves from the asset itself.
	ProjectID string
}

// CollectJobResult turns a job's result into an approved version with its
// usage, atomically.
//
// The caller has already decided the job SUCCEEDED and its result names at
// least one committed file; the refusals for unfinished jobs and empty results
// stay in the collector, which owns the batch's skip rules. What this command
// refuses is a request that cannot produce a readable row set: no asset, no
// job, no files, no consumer.
func (s *Service) CollectJobResult(ctx context.Context, request CollectJobResultRequest) (CollectedJobVersion, error) {
	if !s.Available() {
		return CollectedJobVersion{}, storageFailure()
	}
	if s.transactions == nil {
		// Fail closed: a build whose repository cannot run one transaction
		// must not fall back to the three separate writes whose halfway
		// states this command exists to prevent.
		return CollectedJobVersion{}, agent.UnavailableError()
	}
	assetID := strings.TrimSpace(request.AssetID)
	jobID := strings.TrimSpace(request.JobID)
	if assetID == "" {
		return CollectedJobVersion{}, asset.InvalidError("A collected result must name the asset it belongs to.")
	}
	if jobID == "" {
		return CollectedJobVersion{}, asset.InvalidError("A collected result must name the job that produced it.")
	}
	if len(request.Files) == 0 {
		return CollectedJobVersion{}, asset.InvalidError("A collected result needs at least one committed file.")
	}
	consumerType := request.ConsumerType
	consumerID := strings.TrimSpace(request.ConsumerID)
	if !asset.IsValidConsumerType(consumerType) || consumerID == "" {
		return CollectedJobVersion{}, asset.InvalidError("A collected version must name what consumes it.")
	}
	usageRole := strings.TrimSpace(request.UsageRole)
	if usageRole == "" {
		// The same default AddUsage applies: a version used without a stated
		// role is a reference.
		usageRole = "reference"
	}

	versionID, err := s.ids.New()
	if err != nil {
		return CollectedJobVersion{}, storageFailure()
	}
	usageID, err := s.ids.New()
	if err != nil {
		return CollectedJobVersion{}, storageFailure()
	}
	now := s.now()

	// The events are BUILT here, before the write, so the transaction below
	// records them through the same connection. A repeat emits no new events:
	// the first attempt's records stand.
	createdEvent, approvedEvent, err := s.collectEvents(ctx, request, assetID)
	if err != nil {
		return CollectedJobVersion{}, err
	}

	files := make([]AttachJobFile, len(request.Files))
	copy(files, request.Files)
	usage := asset.Usage{
		ID:             usageID,
		AssetVersionID: versionID,
		ConsumerType:   consumerType,
		ConsumerID:     consumerID,
		UsageRole:      usageRole,
		Required:       false,
		CreatedAt:      now,
	}

	var result CollectedJobVersion
	err = s.transactions.WithinTx(ctx, func(ctx context.Context) error {
		created, existingID, written, inner := s.repository.CollectJobResultVersion(ctx, CollectStorageRequest{
			AssetID:          assetID,
			JobID:            jobID,
			Prompt:           request.Prompt,
			ProviderConfigID: request.ProviderConfigID,
			ModelConfigID:    request.ModelConfigID,
			ModelParameters:  request.ModelParameters,
			SourceAgentRunID: request.SourceAgentRunID,
			CreatedByID:      request.CreatedByID,
			Files:            files,
			Usage:            usage,
			VersionID:        versionID,
			CreatedEvent:     createdEvent,
			ApprovedEvent:    approvedEvent,
		})
		if inner != nil {
			return inner
		}
		// The identity and number are what the transaction wrote or found: a
		// repeat reports the EXISTING version — the one the interrupted first
		// attempt wrote — so a caller's retry names the row that is real
		// rather than the id this attempt minted.
		resultID := versionID
		if existingID != "" {
			resultID = existingID
		}
		result = CollectedJobVersion{
			VersionID:     resultID,
			VersionNumber: written,
			Approved:      true,
			Duplicate:     !created,
		}
		return nil
	})
	if err != nil {
		return CollectedJobVersion{}, err
	}
	return result, nil
}

// collectEvents builds the two governance records a NEW version emits.
//
// The created event is AssetVersionCreated (a candidate was written) and the
// approved one is AssetVersionApproved (the switch put it in force). A build
// without a recorder collects without them — the same trade
// `ApproveVersion`'s best-effort event makes — because refusing a collection
// over a notification the transaction could record elsewhere would leave the
// user's speech uncollected.
func (s *Service) collectEvents(ctx context.Context, request CollectJobResultRequest, assetID string) (CollectEvent, CollectEvent, error) {
	if s.events == nil {
		return CollectEvent{}, CollectEvent{}, nil
	}
	projectID := strings.TrimSpace(request.ProjectID)
	if projectID == "" {
		// The asset carries the project the events file under, so an unnamed
		// project is resolved rather than guessed.
		record, err := s.repository.GetAsset(ctx, assetID)
		if err != nil {
			return CollectEvent{}, CollectEvent{}, err
		}
		projectID = record.ProjectID
	}
	created, err := s.events.Build(ctx, eventsapp.Draft{
		Type:          event.AssetVersionCreated,
		AggregateType: event.AggregateAsset,
		AggregateID:   assetID,
		ProjectID:     projectID,
	})
	if err != nil {
		return CollectEvent{}, CollectEvent{}, err
	}
	approved, err := s.events.Build(ctx, eventsapp.Draft{
		Type:          event.AssetVersionApproved,
		AggregateType: event.AggregateAsset,
		AggregateID:   assetID,
		ProjectID:     projectID,
	})
	if err != nil {
		return CollectEvent{}, CollectEvent{}, err
	}
	return CollectEvent{event: created}, CollectEvent{event: approved}, nil
}

// SetUsageParamsRequest replaces one use's placement document (T05's editor
// command): the readback half of the track editor.
type SetUsageParamsRequest struct {
	// UsageID is the use being edited.
	UsageID string
	// OffsetMS/SourceStartMS/SourceEndMS/DurationMS/Volume/Muted mirror
	// media.TrackParams; nil means "not stated". A document with everything
	// nil clears the override entirely.
	OffsetMS       *int
	SourceStartMS  *int
	SourceEndMS    *int
	DurationMS     *int
	Volume         *float64
	Muted          *bool
	// DialogueLineID names the line a dialogue clip renders. Empty clears.
	DialogueLineID string
}

// SetUsageParams stores one use's placement document.
//
// It is the WRITE half of the editor loop whose READ is
// `media.ParseTrackParams` through the timeline: a UI writes here, the
// timeline read picks the document up on the next board read, and the mixer
// consumes it on the next export. Validation of the values (non-negative
// offsets, a volume in range) happens in the domain types the mixer applies;
// this command stores what the caller states.
func (s *Service) SetUsageParams(ctx context.Context, request SetUsageParamsRequest) error {
	if !s.Available() {
		return storageFailure()
	}
	if _, ok := s.repository.(interface {
		UpdateUsageParams(ctx context.Context, usageID, paramsJSON string) error
	}); !ok {
		return asset.StorageError("The asset store cannot edit usage parameters.", nil)
	}
	// All-nil clears the document back to the mixer defaults.
	params := TrackParamsShape{
		OffsetMS:       request.OffsetMS,
		SourceStartMS:  request.SourceStartMS,
		SourceEndMS:    request.SourceEndMS,
		DurationMS:     request.DurationMS,
		Volume:         request.Volume,
		Muted:          request.Muted,
		DialogueLineID: request.DialogueLineID,
	}
	encoded, err := marshalTrackParamsShape(params)
	if err != nil {
		return err
	}
	return s.repository.(interface {
		UpdateUsageParams(ctx context.Context, usageID, paramsJSON string) error
	}).UpdateUsageParams(ctx, request.UsageID, encoded)
}

// TrackParamsShape is the assets package's own view of the placement
// document. It mirrors media.TrackParams field for field rather than
// importing it — the assets layer sits below media in the dependency order.
type TrackParamsShape struct {
	OffsetMS       *int
	SourceStartMS  *int
	SourceEndMS    *int
	DurationMS     *int
	Volume         *float64
	Muted          *bool
	DialogueLineID string
}

// marshalTrackParamsShape encodes the document; all-nil is the empty string.
func marshalTrackParamsShape(params TrackParamsShape) (string, error) {
	type wire struct {
		OffsetMS       *int     `json:"offsetMs,omitempty"`
		SourceStartMS  *int     `json:"sourceStartMs,omitempty"`
		SourceEndMS    *int     `json:"sourceEndMs,omitempty"`
		DurationMS     *int     `json:"durationMs,omitempty"`
		Volume         *float64 `json:"volume,omitempty"`
		Muted          *bool    `json:"muted,omitempty"`
		DialogueLineID string   `json:"dialogueLineId,omitempty"`
	}
	document := wire{
		OffsetMS:       params.OffsetMS,
		SourceStartMS:  params.SourceStartMS,
		SourceEndMS:    params.SourceEndMS,
		DurationMS:     params.DurationMS,
		Volume:         params.Volume,
		Muted:          params.Muted,
		DialogueLineID: params.DialogueLineID,
	}
	if document == (wire{}) {
		return "", nil
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", asset.StorageError("The placement document could not be encoded.", err)
	}
	return string(encoded), nil
}

// ListUsageParams returns the uses one consumer has made, with their stored
// placement documents — the editor's read.
func (s *Service) ListUsagesOfConsumer(ctx context.Context, consumerType asset.ConsumerType, consumerID string) ([]asset.Usage, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if reader, ok := s.repository.(interface {
		ListUsagesOfConsumer(ctx context.Context, consumerType asset.ConsumerType, consumerID string) ([]asset.Usage, error)
	}); ok {
		return reader.ListUsagesOfConsumer(ctx, consumerType, consumerID)
	}
	return nil, asset.StorageError("The asset store cannot list a consumer's usages.", nil)
}
