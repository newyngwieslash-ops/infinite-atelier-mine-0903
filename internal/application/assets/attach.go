package assets

import (
	"context"
	"strings"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
)

// attach.go turns a succeeded generation job into an asset version.
//
// # Why this is a separate command rather than part of AddVersion
//
// `AddVersion` appends a version somebody is AUTHORING. This one records one a PROVIDER
// produced, and the difference is not cosmetic: a generated version has a job, a
// provider and model, the prompt that was sent, the seed the model used, and at least one
// committed file — every one of which AC-ASSET-002 requires to be traceable. Those facts
// arrive together or not at all, so the command that writes them is one command.
//
// The status is CANDIDATE rather than draft, and that is what closes the loop the
// storyboard panel needs: §9.5 says a panel's approved image must come from its
// candidates, and before this command nothing in the build ever wrote the candidate
// status at all — so a panel could never have had a candidate to approve.
//
// # What the caller must supply, and why not less
//
// The file hashes come from the JOB's result, and this service does not read job storage:
// the job core owns `file_objects` and the reference rows, and a second reader here would
// be a second opinion about what a job produced. The caller — the batch orchestrator, or
// a binding — resolves the job's result files and passes the hashes; what this command
// checks is that they EXIST, through each file's own validation and the schema's foreign
// key. A version advertising bytes nobody committed is refused rather than stored.

// AttachJobResultRequest records one generated version.
type AttachJobResultRequest struct {
	// AssetID is the asset this version belongs to.
	AssetID string
	// JobID is the generation job that produced it, and it is required: a version
	// claiming to be generated without a job is a claim nothing can audit.
	JobID string
	// Files are the committed objects the job produced, in the order the caller wants
	// them stored, with the role each plays.
	Files []AttachJobFile
	// Prompt is what was sent to the provider. It is copied onto the version rather than
	// read back from the job, because the job's input is a transport payload and this is
	// the domain record of what was asked for.
	Prompt string
	// ProviderConfigID and ModelConfigID name what produced it.
	ProviderConfigID string
	ModelConfigID    string
	// ModelParameters and Seed are the reproduction facts.
	ModelParameters string
	Seed            string
	// BasedOnVersionID links a revision to the version it edited.
	BasedOnVersionID string
	// ParentAssetVersionID and VariantType are §8.5's derivation facts: which version
	// this came from and what kind of derivation it is. They are empty for a version
	// generated from a prompt alone, which is the ordinary case for a panel candidate.
	ParentAssetVersionID string
	VariantType          string
	// SourceAgentRunID names the agent run that asked for the generation, so
	// AC-ASSET-002's "agent/stage" is answerable.
	SourceAgentRunID string
	// CreatedByID names which user or agent asked for it. It travels beside
	// SourceAgentRunID rather than being derived from it because the two answer
	// different questions: a run is a record of what an agent did, while this is WHO
	// made the request — and a job a user started by hand has an author and no run.
	CreatedByID string
	// Metadata is the layer's own JSON, for facts the schema has no column for.
	Metadata string
}

// AttachJobFile is one committed object a job produced.
type AttachJobFile struct {
	// FileHash is the content-addressed key of the stored bytes.
	FileHash string
	// Role is what the file contributes to the version.
	Role asset.FileRole
	// Ordinal orders files sharing a role.
	Ordinal int
}

// AttachJobResult records a job's output as a candidate version of an asset.
//
// The version number is derived from the stored maximum, like every other version write
// here, so a superseded or rejected version cannot have its number reused.
func (s *Service) AttachJobResult(ctx context.Context, request AttachJobResultRequest) (asset.Version, []asset.File, error) {
	if !s.Available() {
		return asset.Version{}, nil, storageFailure()
	}
	assetID := strings.TrimSpace(request.AssetID)
	if assetID == "" {
		return asset.Version{}, nil, asset.InvalidError("A generated version must name the asset it belongs to.")
	}
	jobID := strings.TrimSpace(request.JobID)
	if jobID == "" {
		return asset.Version{}, nil, asset.InvalidError("A generated version must name the job that produced it.")
	}
	if len(request.Files) == 0 {
		// A generated version with no file is a version nothing can display or approve,
		// and `CanApprove` would refuse it later. Refusing here names the actual problem:
		// the job's result had no committed object in it.
		return asset.Version{}, nil, asset.InvalidError("A generated version needs at least one committed file.")
	}
	// The asset is read FIRST so a stale identifier is a not-found rather than a foreign
	// key failure from the version insert.
	record, err := s.repository.GetAsset(ctx, assetID)
	if err != nil {
		return asset.Version{}, nil, err
	}
	// The producer is the AGENT when a run is named and the SYSTEM otherwise, and the
	// distinction is what FR-100's audit reads: a job the user started by hand and a job an
	// agent asked for are different answers to "where did this image come from". A version
	// recorded as an agent's when nothing ran would put a provider's output in the model's
	// column.
	createdBy := asset.CreatedBySystem
	if strings.TrimSpace(request.SourceAgentRunID) != "" {
		createdBy = asset.CreatedByAgent
	}
	id, err := s.ids.New()
	if err != nil {
		return asset.Version{}, nil, storageFailure()
	}
	highest, err := s.repository.MaxVersionNumber(ctx, assetID)
	if err != nil {
		return asset.Version{}, nil, err
	}
	now := s.now()
	version := asset.Version{
		ID:                   id,
		AssetID:              assetID,
		VersionNumber:        highest + 1,
		Status:               asset.VersionCandidate,
		BasedOnVersionID:     strings.TrimSpace(request.BasedOnVersionID),
		ParentAssetVersionID: strings.TrimSpace(request.ParentAssetVersionID),
		VariantType:          strings.TrimSpace(request.VariantType),
		Prompt:               request.Prompt,
		ProviderConfigID:     strings.TrimSpace(request.ProviderConfigID),
		ModelConfigID:        strings.TrimSpace(request.ModelConfigID),
		ModelParameters:      request.ModelParameters,
		Seed:                 request.Seed,
		GenerationJobID:      jobID,
		SourceAgentRunID:     strings.TrimSpace(request.SourceAgentRunID),
		Metadata:             request.Metadata,
		CreatedByType:        createdBy,
		CreatedByID:          strings.TrimSpace(request.CreatedByID),
		CreatedAt:            now,
	}
	if err := version.Validate(); err != nil {
		return asset.Version{}, nil, err
	}
	files := make([]asset.File, 0, len(request.Files))
	for index, input := range request.Files {
		role := input.Role
		if role == "" {
			// The first file is the primary; the rest are references. That is the shape a
			// panel's candidates have — one image the user looks at — and a caller that
			// wants something else states it.
			role = asset.RoleReference
			if index == 0 {
				role = asset.RolePrimary
			}
		}
		file := asset.File{
			VersionID: version.ID,
			FileHash:  strings.TrimSpace(input.FileHash),
			Role:      role,
			Ordinal:   input.Ordinal,
			CreatedAt: now,
		}
		if err := file.Validate(); err != nil {
			return asset.Version{}, nil, err
		}
		files = append(files, file)
	}
	if err := s.repository.CreateVersion(ctx, version); err != nil {
		return asset.Version{}, nil, err
	}
	// The files are attached AFTER the version row, because the schema's foreign key needs
	// it. A failure here leaves the version with no files — which is a version
	// `CanApprove` refuses and a caller can retry — rather than no version at all, and the
	// error says so by returning the version that was written.
	for _, file := range files {
		if err := s.repository.AddFile(ctx, file); err != nil {
			return version, nil, err
		}
	}
	// Section 17's AssetVersionCreated, and NOT AssetVersionApproved: a candidate is not
	// in force, and an approval event for it would tell a consumer the opposite of the
	// truth. Best effort, for the reason `CreateAsset` states — the version and its files
	// are committed, so the caller must not be told the command failed because the
	// announcement did not land.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          event.AssetVersionCreated,
		AggregateType: event.AggregateAsset,
		AggregateID:   record.ID,
		ProjectID:     record.ProjectID,
	})
	return version, files, nil
}
