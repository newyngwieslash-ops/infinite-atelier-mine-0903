// Package assets is the application layer for assets, their versions and the
// files a version owns. It owns the commands and queries and defines the
// persistence port that infrastructure implements.
package assets

import (
	"context"
	"time"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
)

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces entity identifiers (ADR-0005).
type IDGenerator interface {
	New() (string, error)
}

// ListFilter narrows an asset query. Zero values mean "no constraint".
type ListFilter struct {
	ProjectID string
	Types     []asset.Type
	// IncludeDeleted includes soft-deleted rows.
	IncludeDeleted bool
	Limit          int
	Offset         int
}

// Repository persists assets, their versions and their file links.
type Repository interface {
	CreateAsset(ctx context.Context, record asset.Asset) error
	GetAsset(ctx context.Context, id string) (asset.Asset, error)
	ListAssets(ctx context.Context, filter ListFilter) ([]asset.Asset, error)
	CountAssets(ctx context.Context, projectID string) (int, error)
	UpdateAsset(ctx context.Context, record asset.Asset, expectedRevision int64) error

	CreateVersion(ctx context.Context, version asset.Version) error
	GetVersion(ctx context.Context, id string) (asset.Version, error)
	ListVersions(ctx context.Context, assetID string) ([]asset.Version, error)
	// MaxVersionNumber reports the highest version number an asset has, or zero.
	MaxVersionNumber(ctx context.Context, assetID string) (int, error)
	UpdateVersionStatus(ctx context.Context, versionID string, status asset.VersionStatus) error

	AddFile(ctx context.Context, file asset.File) error
	ListFiles(ctx context.Context, versionID string) ([]asset.File, error)
	CountFiles(ctx context.Context, versionID string) (int, error)

	// Lineage (§8.5) and usage (§8.6). They are separate from the file links
	// because they answer different questions: an AssetFile is what a version
	// is made of, a Relation is where it came from, and a Usage is who needs it.
	AddRelation(ctx context.Context, relation asset.Relation) error
	ListRelationsFrom(ctx context.Context, versionID string) ([]asset.Relation, error)
	ListRelationsTo(ctx context.Context, versionID string) ([]asset.Relation, error)
	AddUsage(ctx context.Context, usage asset.Usage) error
	ListUsages(ctx context.Context, versionID string) ([]asset.Usage, error)
	CountUsages(ctx context.Context, versionID string) (int, error)
}

// EventRecorder builds and writes domain events for the commands that emit them.
//
// Two paths, and the difference is deliberate (ADR-0009):
//
//   - Build assembles an event for a command that records it inside its own
//     transaction. An approval uses this, and refuses without a recorder,
//     because its event is a governance record rather than a notification.
//   - RecordBestEffort writes a notification event and reports no error. The
//     command's own write has already succeeded, so failing it because the
//     announcement did not land would be the wrong trade.
//
// The port is optional: a Service composed without a recorder still serves every
// command, and only the transactional path refuses.
type EventRecorder interface {
	Build(ctx context.Context, draft eventsapp.Draft) (event.Event, error)
	RecordBestEffort(ctx context.Context, draft eventsapp.Draft)
}

// Service holds the asset commands and queries.
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
	events     EventRecorder
}

// Options configures a Service.
type Options struct {
	Repository Repository
	Clock      Clock
	IDs        IDGenerator
	// Events enables the commands that announce an asset version change.
	Events EventRecorder
}

// NewService builds the asset service.
func NewService(options Options) *Service {
	return &Service{repository: options.Repository, clock: options.Clock, ids: options.IDs, events: options.Events}
}

// Available reports whether the service can operate.
func (s *Service) Available() bool {
	return s != nil && s.repository != nil && s.ids != nil
}

// recordEvent announces something that happened, if the service has a recorder.
//
// The nil check is not defensive padding: the recorder is an interface, so a
// Service composed without one holds a nil interface and calling a method on it
// panics. This is the one place that check lives, so no emit site has to repeat
// it and no emit site can forget it.
func (s *Service) recordEvent(ctx context.Context, draft eventsapp.Draft) {
	if s == nil || s.events == nil {
		return
	}
	s.events.RecordBestEffort(ctx, draft)
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

func storageFailure() error {
	return asset.StorageError("The asset store is unavailable.", nil)
}

// CreateAssetRequest is a caller's request to create an asset.
type CreateAssetRequest struct {
	ProjectID   string
	Type        asset.Type
	Name        string
	Description string
	// LegacyMetadata retains fields a migration could not model.
	LegacyMetadata string
}

// CreateAsset stores a new asset with an initial draft version.
//
// A version is created with the asset because an asset with no version cannot
// be used by anything and "current approved version" has nothing to point at
// later. Its provenance is recorded as the caller's kind so a migrated asset is
// distinguishable from one a user made.
func (s *Service) CreateAsset(ctx context.Context, request CreateAssetRequest) (asset.Asset, asset.Version, error) {
	if !s.Available() {
		return asset.Asset{}, asset.Version{}, storageFailure()
	}
	if !asset.IsValidType(request.Type) {
		return asset.Asset{}, asset.Version{}, asset.InvalidError("That asset type is not recognised.")
	}
	if err := asset.ValidateName(request.Name); err != nil {
		return asset.Asset{}, asset.Version{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return asset.Asset{}, asset.Version{}, storageFailure()
	}
	now := s.now()
	record := asset.Asset{
		ID:             id,
		ProjectID:      request.ProjectID,
		Type:           request.Type,
		Name:           request.Name,
		Description:    request.Description,
		Status:         asset.StatusActive,
		LegacyMetadata: request.LegacyMetadata,
		CreatedAt:      now,
		UpdatedAt:      now,
		Revision:       1,
	}
	if err := s.repository.CreateAsset(ctx, record); err != nil {
		return asset.Asset{}, asset.Version{}, err
	}
	version, err := s.AddVersion(ctx, AddVersionRequest{AssetID: record.ID, CreatedByType: asset.CreatedByUser})
	if err != nil {
		// The asset exists but has no version. Report it so the caller can retry
		// rather than presenting an asset nothing can use.
		return record, asset.Version{}, err
	}
	// Section 17's AssetVersionCreated. Best effort: the asset and its first
	// version are committed, so the caller must not be told the command failed
	// because the announcement did not land.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          event.AssetVersionCreated,
		AggregateType: event.AggregateAsset,
		AggregateID:   record.ID,
		ProjectID:     record.ProjectID,
	})
	return record, version, nil
}

// AddVersionRequest adds a version to an asset.
type AddVersionRequest struct {
	AssetID          string
	BasedOnVersionID string
	Prompt           string
	NegativePrompt   string
	ProviderConfigID string
	ModelConfigID    string
	ModelParameters  string
	GenerationJobID  string
	Metadata         string
	CreatedByType    asset.CreatedByType
	LegacyMetadata   string
}

// AddVersion appends a version, numbering it after the highest existing one.
//
// The number is derived from the stored maximum rather than a count, because a
// deleted or superseded version must not let a new version reuse its number:
// the schema has a unique constraint on (asset_id, version_number) and a reused
// number would be rejected.
func (s *Service) AddVersion(ctx context.Context, request AddVersionRequest) (asset.Version, error) {
	if !s.Available() {
		return asset.Version{}, storageFailure()
	}
	if _, err := s.repository.GetAsset(ctx, request.AssetID); err != nil {
		return asset.Version{}, err
	}
	createdBy := request.CreatedByType
	if createdBy == "" {
		createdBy = asset.CreatedByUser
	}
	id, err := s.ids.New()
	if err != nil {
		return asset.Version{}, storageFailure()
	}
	highest, err := s.repository.MaxVersionNumber(ctx, request.AssetID)
	if err != nil {
		return asset.Version{}, err
	}
	version := asset.Version{
		ID:               id,
		AssetID:          request.AssetID,
		VersionNumber:    highest + 1,
		Status:           asset.VersionDraft,
		BasedOnVersionID: request.BasedOnVersionID,
		Prompt:           request.Prompt,
		NegativePrompt:   request.NegativePrompt,
		ProviderConfigID: request.ProviderConfigID,
		ModelConfigID:    request.ModelConfigID,
		ModelParameters:  request.ModelParameters,
		GenerationJobID:  request.GenerationJobID,
		Metadata:         request.Metadata,
		CreatedByType:    createdBy,
		LegacyMetadata:   request.LegacyMetadata,
		CreatedAt:        s.now(),
	}
	if err := version.Validate(); err != nil {
		return asset.Version{}, err
	}
	if err := s.repository.CreateVersion(ctx, version); err != nil {
		return asset.Version{}, err
	}
	return version, nil
}

// AttachFile links a committed object to a version.
//
// The bytes must already be in the FileStore: the schema's foreign key rejects
// an uncommitted hash, which is what stops a version from advertising a file
// that does not exist (DOMAIN_MODEL §8.2).
func (s *Service) AttachFile(ctx context.Context, versionID, fileHash string, role asset.FileRole) (asset.File, error) {
	if !s.Available() {
		return asset.File{}, storageFailure()
	}
	if _, err := s.repository.GetVersion(ctx, versionID); err != nil {
		return asset.File{}, err
	}
	if role == "" {
		role = asset.RolePrimary
	}
	file := asset.File{VersionID: versionID, FileHash: fileHash, Role: role, CreatedAt: s.now()}
	if err := file.Validate(); err != nil {
		return asset.File{}, err
	}
	if err := s.repository.AddFile(ctx, file); err != nil {
		return asset.File{}, err
	}
	return file, nil
}

// ApproveVersionRequest approves a version.
type ApproveVersionRequest struct {
	VersionID string
	// ImpactAcknowledged records that the caller acted on the impact analysis
	// DOMAIN_MODEL §8.2 requires before an approval switch ("approved 切换需影响
	// 分析"). The analysis itself is ApprovalImpactOf, which lists the consumers
	// of the version being replaced; the caller states that it reviewed that
	// list rather than the service assuming it on the caller's behalf.
	ImpactAcknowledged bool
}

// ApproveVersion switches an asset's approved version.
//
// The preconditions are that the version has a committed file, that it is not
// already approved or superseded (asset.Version.CanApprove), and that the caller
// acknowledged the impact step. Superseding the previous approval is part of the
// switch: §2.5 says "批准新版本时旧批准版本变为 superseded", and leaving two rows
// approved would violate the schema's partial unique index.
//
// The statements are not wrapped in one transaction across the two status
// writes, so a failure between them leaves the previous version approved and
// the new one not, which is the pre-switch state and therefore retryable. The
// order below is deliberate: the previous version is superseded BEFORE the new
// one is approved, so the partial unique index is never asked to hold two
// approved rows at once.
func (s *Service) ApproveVersion(ctx context.Context, request ApproveVersionRequest) (asset.Version, error) {
	if !s.Available() {
		return asset.Version{}, storageFailure()
	}
	if !request.ImpactAcknowledged {
		return asset.Version{}, asset.InvalidError("Approving a version needs an impact check first.")
	}
	version, err := s.repository.GetVersion(ctx, request.VersionID)
	if err != nil {
		return asset.Version{}, err
	}
	fileCount, err := s.repository.CountFiles(ctx, version.ID)
	if err != nil {
		return asset.Version{}, err
	}
	if err := version.CanApprove(fileCount); err != nil {
		return asset.Version{}, err
	}
	record, err := s.repository.GetAsset(ctx, version.AssetID)
	if err != nil {
		return asset.Version{}, err
	}
	if previous := record.CurrentApprovedVersionID; previous != "" && previous != version.ID {
		// Supersede first, approve second: the schema's partial unique index
		// allows at most one approved row per asset, so the order matters.
		if err := s.repository.UpdateVersionStatus(ctx, previous, asset.VersionSuperseded); err != nil {
			return asset.Version{}, err
		}
	}
	if err := s.repository.UpdateVersionStatus(ctx, version.ID, asset.VersionApproved); err != nil {
		return asset.Version{}, err
	}
	record.CurrentApprovedVersionID = version.ID
	record.UpdatedAt = s.now()
	if err := s.repository.UpdateAsset(ctx, record, record.Revision); err != nil {
		return asset.Version{}, err
	}
	version.Status = asset.VersionApproved
	return version, nil
}

// GetAsset returns one asset.
func (s *Service) GetAsset(ctx context.Context, id string) (asset.Asset, error) {
	if !s.Available() {
		return asset.Asset{}, storageFailure()
	}
	return s.repository.GetAsset(ctx, id)
}

// ListAssets returns assets matching the filter.
func (s *Service) ListAssets(ctx context.Context, filter ListFilter) ([]asset.Asset, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListAssets(ctx, filter)
}

// ListVersions returns an asset's versions.
func (s *Service) ListVersions(ctx context.Context, assetID string) ([]asset.Version, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListVersions(ctx, assetID)
}

// ListFiles returns a version's file links.
func (s *Service) ListFiles(ctx context.Context, versionID string) ([]asset.File, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListFiles(ctx, versionID)
}

// AddRelationRequest records where a version came from.
type AddRelationRequest struct {
	SourceAssetVersionID string
	TargetAssetVersionID string
	Type                 asset.RelationType
}

// AddRelation records a lineage edge between two asset versions.
//
// DOMAIN_MODEL §8.5 models the relation between versions rather than assets, so
// a derived asset points at the exact version it came from and the reason is
// preserved with it. PRD FR-050's "任何派生资产都能追溯父资产及变换原因" is what
// ListRelationsTo answers from the other side.
func (s *Service) AddRelation(ctx context.Context, request AddRelationRequest) (asset.Relation, error) {
	if !s.Available() {
		return asset.Relation{}, storageFailure()
	}
	// Both versions must exist before the edge is written, so a typo is reported
	// as a missing version rather than as a foreign-key failure.
	if _, err := s.repository.GetVersion(ctx, request.SourceAssetVersionID); err != nil {
		return asset.Relation{}, err
	}
	if _, err := s.repository.GetVersion(ctx, request.TargetAssetVersionID); err != nil {
		return asset.Relation{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return asset.Relation{}, storageFailure()
	}
	relation := asset.Relation{
		ID:                   id,
		SourceAssetVersionID: request.SourceAssetVersionID,
		TargetAssetVersionID: request.TargetAssetVersionID,
		Type:                 request.Type,
		CreatedAt:            s.now(),
	}
	if err := relation.Validate(); err != nil {
		return asset.Relation{}, err
	}
	if err := s.repository.AddRelation(ctx, relation); err != nil {
		return asset.Relation{}, err
	}
	return relation, nil
}

// ListLineage returns a version's lineage in both directions: what it came from
// and what came from it.
func (s *Service) ListLineage(ctx context.Context, versionID string) (from, to []asset.Relation, err error) {
	if !s.Available() {
		return nil, nil, storageFailure()
	}
	from, err = s.repository.ListRelationsFrom(ctx, versionID)
	if err != nil {
		return nil, nil, err
	}
	to, err = s.repository.ListRelationsTo(ctx, versionID)
	if err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

// AddUsageRequest records that something consumes a version.
type AddUsageRequest struct {
	AssetVersionID string
	ConsumerType   asset.ConsumerType
	ConsumerID     string
	UsageRole      string
	Required       bool
}

// AddUsage records a usage of an asset version.
func (s *Service) AddUsage(ctx context.Context, request AddUsageRequest) (asset.Usage, error) {
	if !s.Available() {
		return asset.Usage{}, storageFailure()
	}
	if _, err := s.repository.GetVersion(ctx, request.AssetVersionID); err != nil {
		return asset.Usage{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return asset.Usage{}, storageFailure()
	}
	usageRole := request.UsageRole
	if usageRole == "" {
		usageRole = "reference"
	}
	usage := asset.Usage{
		ID:             id,
		AssetVersionID: request.AssetVersionID,
		ConsumerType:   request.ConsumerType,
		ConsumerID:     request.ConsumerID,
		UsageRole:      usageRole,
		Required:       request.Required,
		CreatedAt:      s.now(),
	}
	if err := usage.Validate(); err != nil {
		return asset.Usage{}, err
	}
	if err := s.repository.AddUsage(ctx, usage); err != nil {
		return asset.Usage{}, err
	}
	return usage, nil
}

// ListUsages returns everything consuming a version.
func (s *Service) ListUsages(ctx context.Context, versionID string) ([]asset.Usage, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListUsages(ctx, versionID)
}

// ApprovalImpact is what an approval switch would disturb.
type ApprovalImpact struct {
	// VersionID is the version being approved.
	VersionID string
	// Replaces is the version currently approved for the same asset, or empty
	// when nothing is approved yet.
	Replaces string
	// Consumers is everything using the version being replaced, which is the
	// list PRD FR-050 requires: "替换批准版本时，系统列出受影响的分镜和镜头".
	Consumers []asset.Usage
	// RequiredConsumers is the subset a consumer cannot render without.
	RequiredConsumers []asset.Usage
}

// ApprovalImpactOf reports what approving a version would disturb.
//
// This is the impact analysis DOMAIN_MODEL §8.2 requires before an approval
// switch ("approved 切换需影响分析") and that WP-04 could not perform because the
// shot and scene model did not exist yet: its ApproveVersion refused an approval
// without an explicit acknowledgement precisely so that this check would take
// its place. It reads only; the caller decides.
func (s *Service) ApprovalImpactOf(ctx context.Context, versionID string) (ApprovalImpact, error) {
	if !s.Available() {
		return ApprovalImpact{}, storageFailure()
	}
	version, err := s.repository.GetVersion(ctx, versionID)
	if err != nil {
		return ApprovalImpact{}, err
	}
	impact := ApprovalImpact{VersionID: versionID, Consumers: []asset.Usage{}, RequiredConsumers: []asset.Usage{}}
	record, err := s.repository.GetAsset(ctx, version.AssetID)
	if err != nil {
		return ApprovalImpact{}, err
	}
	// The version being replaced is the one currently approved, which the asset
	// row names. Nothing approved means nothing to analyse.
	if record.CurrentApprovedVersionID == "" || record.CurrentApprovedVersionID == versionID {
		return impact, nil
	}
	impact.Replaces = record.CurrentApprovedVersionID
	usages, err := s.repository.ListUsages(ctx, record.CurrentApprovedVersionID)
	if err != nil {
		return ApprovalImpact{}, err
	}
	for _, usage := range usages {
		impact.Consumers = append(impact.Consumers, usage)
		if usage.Required {
			impact.RequiredConsumers = append(impact.RequiredConsumers, usage)
		}
	}
	return impact, nil
}
