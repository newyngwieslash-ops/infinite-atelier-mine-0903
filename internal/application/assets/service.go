// Package assets is the application layer for assets, their versions and the
// files a version owns. It owns the commands and queries and defines the
// persistence port that infrastructure implements.
package assets

import (
	"context"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
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
}

// Service holds the asset commands and queries.
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
}

// Options configures a Service.
type Options struct {
	Repository Repository
	Clock      Clock
	IDs        IDGenerator
}

// NewService builds the asset service.
func NewService(options Options) *Service {
	return &Service{repository: options.Repository, clock: options.Clock, ids: options.IDs}
}

// Available reports whether the service can operate.
func (s *Service) Available() bool {
	return s != nil && s.repository != nil && s.ids != nil
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
	// ImpactAcknowledged records that the caller performed the impact analysis
	// DOMAIN_MODEL §8.2 requires before an approval switch. WP-04 has no shot or
	// scene model to compute impact from, so the caller states it explicitly
	// rather than the service pretending it was checked.
	ImpactAcknowledged bool
}

// ApproveVersion switches an asset's approved version.
//
// The WP-04 preconditions are that the version has a committed file, that it is
// not superseded or stale, and that the caller acknowledged the impact step.
// The impact analysis itself belongs to WP-05, when shots and scenes exist to
// analyse; until then an unacknowledged approval is refused rather than
// silently granted.
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
