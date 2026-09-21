package desktop

import (
	"context"
	"sync"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// AssetsBinding is the narrow Wails surface for the asset bible: assets, their
// versions and the committed files a version owns.
//
// It exposes identity, text and structured values only. A file crosses this
// boundary as a SHA-256 content hash and its role, never as a path or as bytes:
// the bytes live in the FileStore and are read through the job result reader,
// which validates the key's shape before touching the filesystem.
//
// The service is optional. An unattached binding fails closed with a stable
// DESKTOP_BINDING_UNAVAILABLE error rather than panicking.
type AssetsBinding struct {
	mu      sync.RWMutex
	ctx     context.Context
	service *appassets.Service
}

// AttachAssets supplies the asset service. A nil service leaves the binding
// unattached, and every method then fails closed.
func AttachAssets(binding *AssetsBinding, ctx context.Context, service *appassets.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.service = service
	binding.mu.Unlock()
}

// AssetsBindingUnavailable is the fail-closed error the composition root returns
// when the asset service could not be composed.
func AssetsBindingUnavailable() error {
	return bindingUnavailable()
}

func (b *AssetsBinding) context() context.Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *AssetsBinding) assetService() *appassets.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.service
}

// Bounds for the asset queries. They exist so one call cannot send an unbounded
// type filter or page size (SECURITY: a query must stay bounded).
const (
	// maxBatchAssetTypes bounds the type filter. asset.Types lists thirteen
	// values, so a request naming more than that is malformed rather than
	// merely wide.
	maxBatchAssetTypes = 32
	// maxPageSize is the largest page one list query may ask for.
	maxPageSize = 1000
	// maxBatchAssetFiles bounds the files one result may describe. A generated image
	// arrives with a primary and at most a handful of references, so a request naming
	// more is malformed rather than merely wide.
	maxBatchAssetFiles = 16
)

// clampPageSize bounds a client-supplied page size. A non-positive value takes
// the default page, which is what an omitted field means.
func clampPageSize(value int) int {
	if value <= 0 {
		return 200
	}
	if value > maxPageSize {
		return maxPageSize
	}
	return value
}

// AssetDTO is the transport view of an asset.
type AssetDTO struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// StoryEntityID links the asset to the story entity it depicts; it is empty
	// until that link is made.
	StoryEntityID string `json:"storyEntityId,omitempty"`
	// CurrentApprovedVersionID is empty when nothing has been approved yet.
	CurrentApprovedVersionID string `json:"currentApprovedVersionId,omitempty"`
	Status                   string `json:"status"`
	DeletedAt                string `json:"deletedAt,omitempty"`
	// LegacyMetadata retains migrated fields the schema does not model.
	LegacyMetadata string `json:"legacyMetadata,omitempty"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
	Revision       int64  `json:"revision"`
}

// AssetVersionDTO is the transport view of one asset version.
type AssetVersionDTO struct {
	ID            string `json:"id"`
	AssetID       string `json:"assetId"`
	VersionNumber int    `json:"versionNumber"`
	Status        string `json:"status"`
	// BasedOnVersionID links a revision to the version it edited.
	BasedOnVersionID string `json:"basedOnVersionId,omitempty"`
	// ParentAssetVersionID and VariantType are the §8.5 derivation facts: which version
	// this came from and what kind of derivation it is. They are what PRD FR-050's
	// "任何派生资产都能追溯父资产及变换原因" is read from.
	ParentAssetVersionID string `json:"parentAssetVersionId,omitempty"`
	VariantType          string `json:"variantType,omitempty"`
	Prompt               string `json:"prompt,omitempty"`
	NegativePrompt       string `json:"negativePrompt,omitempty"`
	ProviderConfigID     string `json:"providerConfigId,omitempty"`
	ModelConfigID        string `json:"modelConfigId,omitempty"`
	// ModelParameters is the generation settings document.
	ModelParameters string `json:"modelParameters,omitempty"`
	// Seed is the provider's reproduction key, empty when the provider returned none.
	Seed string `json:"seed,omitempty"`
	// GenerationJobID links the version to the job that produced it.
	GenerationJobID string `json:"generationJobId,omitempty"`
	// SourceAgentRunID names the agent run that produced it, which is AC-ASSET-002's
	// "agent/stage" half.
	SourceAgentRunID string `json:"sourceAgentRunId,omitempty"`
	Metadata         string `json:"metadata,omitempty"`
	CreatedByType    string `json:"createdByType"`
	// CreatedByID is which user or agent produced it.
	CreatedByID    string `json:"createdById,omitempty"`
	LegacyMetadata string `json:"legacyMetadata,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

// AssetRelationDTO is the transport view of one lineage edge.
type AssetRelationDTO struct {
	ID                   string `json:"id"`
	SourceAssetVersionID string `json:"sourceAssetVersionId"`
	TargetAssetVersionID string `json:"targetAssetVersionId"`
	Type                 string `json:"type"`
	CreatedAt            string `json:"createdAt"`
}

// AssetUsageDTO is the transport view of one usage: who consumes which version.
//
// ConsumerID names a row of another aggregate — a scene, a shot, a panel, a job — which
// is why it is a string here and §8.6's "consumer_id 多态" is why it carries no foreign
// key. Nothing about the consumer crosses this boundary except its identity and type.
type AssetUsageDTO struct {
	AssetVersionID string `json:"assetVersionId"`
	ConsumerType   string `json:"consumerType"`
	ConsumerID     string `json:"consumerId"`
	UsageRole      string `json:"usageRole"`
	Required       bool   `json:"required"`
	CreatedAt      string `json:"createdAt"`
}

// ApprovalImpactDTO is what approving a version would disturb.
//
// It is what the user sees BEFORE they approve: PRD FR-050's "替换批准版本时，系统列出受影响
// 的分镜和镜头" is about listing, and §15.3's waiver is the route for proceeding anyway.
type ApprovalImpactDTO struct {
	VersionID string `json:"versionId"`
	// Replaces is the version currently in force for the same asset, empty when nothing
	// is approved yet.
	Replaces string `json:"replaces,omitempty"`
	// Consumers is everything using the version being replaced.
	Consumers []AssetUsageDTO `json:"consumers"`
	// RequiredConsumers is the subset a consumer cannot render without.
	RequiredConsumers []AssetUsageDTO `json:"requiredConsumers"`
}

// AssetFileDTO is the transport view of one file link.
//
// FileHash is a SHA-256 content hash and Role says what the file contributes to
// the version. There is no path and no byte content on this surface.
type AssetFileDTO struct {
	VersionID string `json:"versionId"`
	FileHash  string `json:"fileHash"`
	Role      string `json:"role"`
	// Ordinal orders files that share a role, which is what lets a panel list
	// several reference images in a stable sequence.
	Ordinal   int    `json:"ordinal"`
	CreatedAt string `json:"createdAt"`
}

// CreateAssetRequest creates an asset.
type CreateAssetRequest struct {
	ProjectID string `json:"projectId"`
	// Type is one of the asset.Types vocabulary, such as "character", "location",
	// "image" or "video".
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// LegacyMetadata retains migrated fields the schema does not model.
	LegacyMetadata string `json:"legacyMetadata,omitempty"`
}

// CreateAsset stores a new asset with an initial draft version.
//
// The service creates that first version itself and returns it alongside the
// asset, because an asset with no version cannot be used by anything. The
// transport view carries the asset only, and on the one failure the service
// distinguishes — the asset stored but its first version not — the error is
// returned rather than a half-made asset, so the caller can find the row with
// ListAssets and its missing version with ListVersions.
func (b *AssetsBinding) CreateAsset(request CreateAssetRequest) (AssetDTO, error) {
	service := b.assetService()
	if service == nil {
		return AssetDTO{}, bindingUnavailable()
	}
	record, _, err := service.CreateAsset(b.context(), appassets.CreateAssetRequest{
		ProjectID:      request.ProjectID,
		Type:           asset.Type(request.Type),
		Name:           request.Name,
		Description:    request.Description,
		LegacyMetadata: request.LegacyMetadata,
	})
	if err != nil {
		return AssetDTO{}, toAssetError(err)
	}
	return toAssetDTO(record), nil
}

// ListAssetsRequest narrows an asset query. Every field is optional.
type ListAssetsRequest struct {
	ProjectID string `json:"projectId,omitempty"`
	// Types narrows the query to those asset types. An empty list means no
	// constraint on type.
	Types []string `json:"types,omitempty"`
	// IncludeDeleted includes soft-deleted rows.
	IncludeDeleted bool `json:"includeDeleted,omitempty"`
	Limit          int  `json:"limit,omitempty"`
	Offset         int  `json:"offset,omitempty"`
}

// ListAssets returns assets matching the filter.
func (b *AssetsBinding) ListAssets(request ListAssetsRequest) ([]AssetDTO, error) {
	service := b.assetService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	if len(request.Types) > maxBatchAssetTypes {
		return nil, bindingInvalidInput()
	}
	filter := appassets.ListFilter{
		ProjectID:      request.ProjectID,
		IncludeDeleted: request.IncludeDeleted,
		Limit:          clampPageSize(request.Limit),
		Offset:         request.Offset,
	}
	for _, value := range request.Types {
		filter.Types = append(filter.Types, asset.Type(value))
	}
	records, err := service.ListAssets(b.context(), filter)
	if err != nil {
		return nil, toAssetError(err)
	}
	assets := make([]AssetDTO, 0, len(records))
	for _, record := range records {
		assets = append(assets, toAssetDTO(record))
	}
	return assets, nil
}

// GetAsset returns one asset.
func (b *AssetsBinding) GetAsset(id string) (AssetDTO, error) {
	service := b.assetService()
	if service == nil {
		return AssetDTO{}, bindingUnavailable()
	}
	record, err := service.GetAsset(b.context(), id)
	if err != nil {
		return AssetDTO{}, toAssetError(err)
	}
	return toAssetDTO(record), nil
}

// AddVersionRequest adds a version to an asset.
type AddVersionRequest struct {
	AssetID          string `json:"assetId"`
	BasedOnVersionID string `json:"basedOnVersionId,omitempty"`
	Prompt           string `json:"prompt,omitempty"`
	NegativePrompt   string `json:"negativePrompt,omitempty"`
	ProviderConfigID string `json:"providerConfigId,omitempty"`
	ModelConfigID    string `json:"modelConfigId,omitempty"`
	ModelParameters  string `json:"modelParameters,omitempty"`
	// GenerationJobID links the version to the job that produced it.
	GenerationJobID string `json:"generationJobId,omitempty"`
	Metadata        string `json:"metadata,omitempty"`
	// CreatedByType is "user" (the default for an empty value), "agent",
	// "migration" or "system".
	CreatedByType  string `json:"createdByType,omitempty"`
	LegacyMetadata string `json:"legacyMetadata,omitempty"`
}

// AddVersion appends a version in the draft state, numbering it after the
// highest existing one.
func (b *AssetsBinding) AddVersion(request AddVersionRequest) (AssetVersionDTO, error) {
	service := b.assetService()
	if service == nil {
		return AssetVersionDTO{}, bindingUnavailable()
	}
	record, err := service.AddVersion(b.context(), appassets.AddVersionRequest{
		AssetID:          request.AssetID,
		BasedOnVersionID: request.BasedOnVersionID,
		Prompt:           request.Prompt,
		NegativePrompt:   request.NegativePrompt,
		ProviderConfigID: request.ProviderConfigID,
		ModelConfigID:    request.ModelConfigID,
		ModelParameters:  request.ModelParameters,
		GenerationJobID:  request.GenerationJobID,
		Metadata:         request.Metadata,
		CreatedByType:    asset.CreatedByType(request.CreatedByType),
		LegacyMetadata:   request.LegacyMetadata,
	})
	if err != nil {
		return AssetVersionDTO{}, toAssetError(err)
	}
	return toAssetVersionDTO(record), nil
}

// ListVersions returns an asset's versions.
func (b *AssetsBinding) ListVersions(assetID string) ([]AssetVersionDTO, error) {
	service := b.assetService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListVersions(b.context(), assetID)
	if err != nil {
		return nil, toAssetError(err)
	}
	versions := make([]AssetVersionDTO, 0, len(records))
	for _, record := range records {
		versions = append(versions, toAssetVersionDTO(record))
	}
	return versions, nil
}

// ApproveVersionRequest approves a version.
type ApproveVersionRequest struct {
	VersionID string `json:"versionId"`
	// ImpactAcknowledged records that the caller performed the impact analysis
	// DOMAIN_MODEL §8.2 requires before an approval switch. An unacknowledged
	// approval is refused rather than silently granted.
	ImpactAcknowledged bool `json:"impactAcknowledged"`
}

// ApproveVersion switches an asset's approved version.
func (b *AssetsBinding) ApproveVersion(request ApproveVersionRequest) (AssetVersionDTO, error) {
	service := b.assetService()
	if service == nil {
		return AssetVersionDTO{}, bindingUnavailable()
	}
	record, err := service.ApproveVersion(b.context(), appassets.ApproveVersionRequest{
		VersionID:          request.VersionID,
		ImpactAcknowledged: request.ImpactAcknowledged,
	})
	if err != nil {
		return AssetVersionDTO{}, toAssetError(err)
	}
	return toAssetVersionDTO(record), nil
}

// AttachFileRequest links a committed object to a version.
type AttachFileRequest struct {
	VersionID string `json:"versionId"`
	// FileHash is the SHA-256 hash of an object that is already committed to the
	// FileStore. The schema's foreign key rejects a hash that was never stored,
	// which is what stops a version from advertising bytes that do not exist.
	FileHash string `json:"fileHash"`
	// Role is "primary" (the default for an empty value), "thumbnail",
	// "reference", "mask", "first_frame", "last_frame" or "source".
	Role string `json:"role,omitempty"`
}

// AttachFile links a committed object to a version.
func (b *AssetsBinding) AttachFile(request AttachFileRequest) (AssetFileDTO, error) {
	service := b.assetService()
	if service == nil {
		return AssetFileDTO{}, bindingUnavailable()
	}
	record, err := service.AttachFile(b.context(), request.VersionID, request.FileHash, asset.FileRole(request.Role))
	if err != nil {
		return AssetFileDTO{}, toAssetError(err)
	}
	return toAssetFileDTO(record), nil
}

// ListFiles returns a version's file links.
func (b *AssetsBinding) ListFiles(versionID string) ([]AssetFileDTO, error) {
	service := b.assetService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListFiles(b.context(), versionID)
	if err != nil {
		return nil, toAssetError(err)
	}
	files := make([]AssetFileDTO, 0, len(records))
	for _, record := range records {
		files = append(files, toAssetFileDTO(record))
	}
	return files, nil
}

// AddUsageRequest records that something consumes an asset version.
type AddUsageRequest struct {
	AssetVersionID string `json:"assetVersionId"`
	// ConsumerType is one of asset.ConsumerTypes: "project_style", "scene", "shot",
	// "storyboard_panel", "job" or "export".
	ConsumerType string `json:"consumerType"`
	// ConsumerID names the consuming row. It is polymorphic by design (section 8.6), so
	// this surface checks only that it is present.
	ConsumerID string `json:"consumerId"`
	UsageRole  string `json:"usageRole,omitempty"`
	// Required marks a consumer that cannot render without the version, which is the
	// subset an approval switch is gated on.
	Required bool `json:"required,omitempty"`
}

// AddUsage records a usage of an asset version.
//
// DOMAIN_MODEL section 8.6 makes usages the input of an approval's impact analysis: PRD
// FR-050 requires that replacing an approved version LIST the affected shots and scenes,
// and that list is built from the usages pointing at the version being replaced. Without
// a writer for them the list was always empty, which is why this command is exposed
// rather than left as a method no build could reach.
func (b *AssetsBinding) AddUsage(request AddUsageRequest) (AssetUsageDTO, error) {
	service := b.assetService()
	if service == nil {
		return AssetUsageDTO{}, bindingUnavailable()
	}
	record, err := service.AddUsage(b.context(), appassets.AddUsageRequest{
		AssetVersionID: request.AssetVersionID,
		ConsumerType:   asset.ConsumerType(request.ConsumerType),
		ConsumerID:     request.ConsumerID,
		UsageRole:      request.UsageRole,
		Required:       request.Required,
	})
	if err != nil {
		return AssetUsageDTO{}, toAssetError(err)
	}
	return toAssetUsageDTO(record), nil
}

// ListUsages returns everything consuming an asset version.
func (b *AssetsBinding) ListUsages(versionID string) ([]AssetUsageDTO, error) {
	service := b.assetService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListUsages(b.context(), versionID)
	if err != nil {
		return nil, toAssetError(err)
	}
	usages := make([]AssetUsageDTO, 0, len(records))
	for _, record := range records {
		usages = append(usages, toAssetUsageDTO(record))
	}
	return usages, nil
}

// AddRelationRequest records where a version came from.
type AddRelationRequest struct {
	SourceAssetVersionID string `json:"sourceAssetVersionId"`
	TargetAssetVersionID string `json:"targetAssetVersionId"`
	// Type is one of "derived_from", "variant_of", "replaces", "references" or
	// "supersedes".
	Type string `json:"type"`
}

// AddRelation records a lineage edge between two asset versions.
//
// Section 8.5's "any derived asset can be traced to its parent and the reason for the
// change" is what this writes: the edge carries the meaning as well as the link, so a
// derived asset's origin and the reason for the derivation are one row.
func (b *AssetsBinding) AddRelation(request AddRelationRequest) (AssetRelationDTO, error) {
	service := b.assetService()
	if service == nil {
		return AssetRelationDTO{}, bindingUnavailable()
	}
	record, err := service.AddRelation(b.context(), appassets.AddRelationRequest{
		SourceAssetVersionID: request.SourceAssetVersionID,
		TargetAssetVersionID: request.TargetAssetVersionID,
		Type:                 asset.RelationType(request.Type),
	})
	if err != nil {
		return AssetRelationDTO{}, toAssetError(err)
	}
	return toAssetRelationDTO(record), nil
}

// LineageDTO is a version's lineage in both directions.
type LineageDTO struct {
	// From lists the versions this one came from.
	From []AssetRelationDTO `json:"from"`
	// To lists the versions that came from this one.
	To []AssetRelationDTO `json:"to"`
}

// ListLineage returns a version's lineage in both directions.
func (b *AssetsBinding) ListLineage(versionID string) (LineageDTO, error) {
	service := b.assetService()
	if service == nil {
		return LineageDTO{}, bindingUnavailable()
	}
	from, to, err := service.ListLineage(b.context(), versionID)
	if err != nil {
		return LineageDTO{}, toAssetError(err)
	}
	result := LineageDTO{From: make([]AssetRelationDTO, 0, len(from)), To: make([]AssetRelationDTO, 0, len(to))}
	for _, record := range from {
		result.From = append(result.From, toAssetRelationDTO(record))
	}
	for _, record := range to {
		result.To = append(result.To, toAssetRelationDTO(record))
	}
	return result, nil
}

// GetApprovalImpact reports what approving a version would disturb.
//
// It is the READ the approval dialog makes before the user commits: section 8.2 requires
// impact analysis before an approval switch, and `ApproveVersion` refuses without the
// caller's acknowledgement of it. A UI that cannot ask this question can only send the
// acknowledgement blind, which is what made the requirement unenforceable in practice.
func (b *AssetsBinding) GetApprovalImpact(versionID string) (ApprovalImpactDTO, error) {
	service := b.assetService()
	if service == nil {
		return ApprovalImpactDTO{}, bindingUnavailable()
	}
	impact, err := service.ApprovalImpactOf(b.context(), versionID)
	if err != nil {
		return ApprovalImpactDTO{}, toAssetError(err)
	}
	result := ApprovalImpactDTO{
		VersionID:         impact.VersionID,
		Replaces:          impact.Replaces,
		Consumers:         make([]AssetUsageDTO, 0, len(impact.Consumers)),
		RequiredConsumers: make([]AssetUsageDTO, 0, len(impact.RequiredConsumers)),
	}
	for _, record := range impact.Consumers {
		result.Consumers = append(result.Consumers, toAssetUsageDTO(record))
	}
	for _, record := range impact.RequiredConsumers {
		result.RequiredConsumers = append(result.RequiredConsumers, toAssetUsageDTO(record))
	}
	return result, nil
}

// AttachJobResultRequest records a generation job's output as a candidate version.
type AttachJobResultRequest struct {
	AssetID string `json:"assetId"`
	// JobID is the job that produced it. A version claiming to be generated without one
	// is refused, because nothing could audit where it came from.
	JobID string `json:"jobId"`
	// Files are the committed objects the job produced, in order. Each hash must already
	// be in the object store: the schema's foreign key refuses bytes that were never
	// committed.
	Files []AttachJobResultFile `json:"files"`
	// Prompt is what was sent to the provider.
	Prompt           string `json:"prompt,omitempty"`
	ProviderConfigID string `json:"providerConfigId,omitempty"`
	ModelConfigID    string `json:"modelConfigId,omitempty"`
	ModelParameters  string `json:"modelParameters,omitempty"`
	Seed             string `json:"seed,omitempty"`
	BasedOnVersionID string `json:"basedOnVersionId,omitempty"`
	// ParentAssetVersionID and VariantType are section 8.5's derivation facts, empty for
	// a version generated from a prompt alone.
	ParentAssetVersionID string `json:"parentAssetVersionId,omitempty"`
	VariantType          string `json:"variantType,omitempty"`
	// SourceAgentRunID names the agent run that asked for the generation.
	SourceAgentRunID string `json:"sourceAgentRunId,omitempty"`
	// CreatedByID names which user or agent asked for it.
	CreatedByID string `json:"createdById,omitempty"`
	Metadata    string `json:"metadata,omitempty"`
}

// AttachJobResultFile is one committed object in a job's output.
type AttachJobResultFile struct {
	FileHash string `json:"fileHash"`
	// Role is "primary", "thumbnail", "reference", "mask", "first_frame", "last_frame"
	// or "source". The first file defaults to primary and the rest to reference.
	Role    string `json:"role,omitempty"`
	Ordinal int    `json:"ordinal,omitempty"`
}

// AttachJobResult records a job's output as a candidate version of an asset.
//
// The status is CANDIDATE rather than draft: section 9.5 says a panel's approved image
// must come from its candidates, so this is the command that gives a panel something to
// approve. It returns the version, because the caller's next step — recording the
// panel's usage, or showing the image — needs to know what was written.
func (b *AssetsBinding) AttachJobResult(request AttachJobResultRequest) (AssetVersionDTO, error) {
	service := b.assetService()
	if service == nil {
		return AssetVersionDTO{}, bindingUnavailable()
	}
	if len(request.Files) > maxBatchAssetFiles {
		// A single command must not describe an unbounded set of files: the batch has a
		// cost and SECURITY requires an explicit limit, the same rule the image
		// submission follows.
		return AssetVersionDTO{}, bindingInvalidInput()
	}
	files := make([]appassets.AttachJobFile, 0, len(request.Files))
	for _, file := range request.Files {
		files = append(files, appassets.AttachJobFile{
			FileHash: file.FileHash,
			Role:     asset.FileRole(file.Role),
			Ordinal:  file.Ordinal,
		})
	}
	record, _, err := service.AttachJobResult(b.context(), appassets.AttachJobResultRequest{
		AssetID:              request.AssetID,
		JobID:                request.JobID,
		Files:                files,
		Prompt:               request.Prompt,
		ProviderConfigID:     request.ProviderConfigID,
		ModelConfigID:        request.ModelConfigID,
		ModelParameters:      request.ModelParameters,
		Seed:                 request.Seed,
		BasedOnVersionID:     request.BasedOnVersionID,
		ParentAssetVersionID: request.ParentAssetVersionID,
		VariantType:          request.VariantType,
		SourceAgentRunID:     request.SourceAgentRunID,
		CreatedByID:          request.CreatedByID,
		Metadata:             request.Metadata,
	})
	if err != nil {
		return AssetVersionDTO{}, toAssetError(err)
	}
	return toAssetVersionDTO(record), nil
}

// toAssetDTO converts a domain asset to its transport view.
func toAssetDTO(record asset.Asset) AssetDTO {
	return AssetDTO{
		ID: record.ID, ProjectID: record.ProjectID, Type: string(record.Type),
		Name: record.Name, Description: record.Description,
		StoryEntityID:            record.StoryEntityID,
		CurrentApprovedVersionID: record.CurrentApprovedVersionID,
		Status:                   string(record.Status),
		DeletedAt:                rfc3339OrEmpty(record.DeletedAt),
		LegacyMetadata:           record.LegacyMetadata,
		CreatedAt:                record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt:                record.UpdatedAt.UTC().Format(rfc3339),
		Revision:                 record.Revision,
	}
}

// toAssetVersionDTO converts a domain asset version to its transport view.
func toAssetVersionDTO(record asset.Version) AssetVersionDTO {
	return AssetVersionDTO{
		ID: record.ID, AssetID: record.AssetID, VersionNumber: record.VersionNumber,
		Status: string(record.Status), BasedOnVersionID: record.BasedOnVersionID,
		ParentAssetVersionID: record.ParentAssetVersionID, VariantType: record.VariantType,
		Prompt: record.Prompt, NegativePrompt: record.NegativePrompt,
		ProviderConfigID: record.ProviderConfigID, ModelConfigID: record.ModelConfigID,
		ModelParameters: record.ModelParameters, Seed: record.Seed,
		GenerationJobID: record.GenerationJobID, SourceAgentRunID: record.SourceAgentRunID,
		Metadata: record.Metadata, CreatedByType: string(record.CreatedByType),
		CreatedByID: record.CreatedByID, LegacyMetadata: record.LegacyMetadata,
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toAssetRelationDTO converts a lineage edge to its transport view.
func toAssetRelationDTO(record asset.Relation) AssetRelationDTO {
	return AssetRelationDTO{
		ID: record.ID, SourceAssetVersionID: record.SourceAssetVersionID,
		TargetAssetVersionID: record.TargetAssetVersionID, Type: string(record.Type),
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toAssetUsageDTO converts a usage to its transport view.
func toAssetUsageDTO(record asset.Usage) AssetUsageDTO {
	return AssetUsageDTO{
		AssetVersionID: record.AssetVersionID, ConsumerType: string(record.ConsumerType),
		ConsumerID: record.ConsumerID, UsageRole: record.UsageRole,
		Required: record.Required, CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toAssetFileDTO converts a domain file link to its transport view. The hash is
// the whole reference: the store owns the layout, so no path is carried.
func toAssetFileDTO(record asset.File) AssetFileDTO {
	return AssetFileDTO{
		VersionID: record.VersionID, FileHash: record.FileHash,
		Role: string(record.Role), Ordinal: record.Ordinal,
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
	}
}

// toAssetError maps an asset domain error to a stable application error.
//
// The category is preserved as the code fragment and the cause is dropped, so a
// driver message never reaches the user. An application error — the storage
// codes the repository raises, such as ASSET_READ_FAILED — is passed through
// unchanged, because its code and category are already safe and stable.
func toAssetError(err error) error {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*apperror.Error); ok {
		return appErr
	}
	domainErr, ok := asset.AsError(err)
	if !ok {
		return apperror.New("ASSET_REQUEST_FAILED", "asset", false, "The asset request failed.", err)
	}
	return apperror.New("ASSET_"+upperText(string(domainErr.Category)), "asset", false, domainErr.SafeMessage, nil)
}
