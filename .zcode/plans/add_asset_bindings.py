import io

p = 'internal/desktop/assets_binding.go'
s = io.open(p, encoding='utf-8').read()

anchor = '// toAssetDTO converts a domain asset to its transport view.'

addition = '''// AddUsageRequest records that something consumes an asset version.
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
	Metadata         string `json:"metadata,omitempty"`
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
		Metadata:             request.Metadata,
	})
	if err != nil {
		return AssetVersionDTO{}, toAssetError(err)
	}
	return toAssetVersionDTO(record), nil
}

// toAssetDTO converts a domain asset to its transport view.'''

assert anchor in s, "anchor not found"
s = s.replace(anchor, addition, 1)

old_bounds = '''	// maxPageSize is the largest page one list query may ask for.
	maxPageSize = 1000
)'''
new_bounds = '''	// maxPageSize is the largest page one list query may ask for.
	maxPageSize = 1000
	// maxBatchAssetFiles bounds the files one result may describe. A generated image
	// arrives with a primary and at most a handful of references, so a request naming
	// more is malformed rather than merely wide.
	maxBatchAssetFiles = 16
)'''
assert old_bounds in s, "bounds anchor not found"
s = s.replace(old_bounds, new_bounds, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("bindings added")
