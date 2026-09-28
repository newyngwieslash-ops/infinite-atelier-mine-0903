package productionpipeline

import (
	"context"
	"encoding/json"
	"strings"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// video_candidate.go is the FR-080 adoption chain's first half: a succeeded
// VIDEO job's result becomes a CANDIDATE version a user can preview, compare
// and approve for its shot.
//
// # Why a candidate and not an approved take
//
// Audio speech is approved at collection because a line's take is not a choice
// among renders — it is what the user asked for. A shot's video IS a choice:
// FR-080 says 「同一 Shot 多个视频版本并批准其中一个」, so the same shot can hold
// several takes and a user picks among them. The collection therefore mirrors
// the image batch's candidate flow (a version the panel can offer), and the
// approval is the second half of the chain — a separate, deliberate act.
//
// # The read side was already video-capable
//
// The timeline's join resolves `asset_type` verbatim, the export branches on
// `media_kind == "video"` into a real video segment, and the manifest records
// whatever version is approved. What did not exist was this write path: a
// succeeded job's bytes were referenced only by the job itself, and no version
// was ever created — the state the 2026-09-26 audit names as FR-080's missing
// product path.
//
// # The shot is the collector's own read
//
// A video job's entity IS the shot (`media_jobs.go` submits with
// `EntityType: "shot"`), so the consumer needs no caller-supplied mapping: the
// usage is recorded against the shot that asked for the render. The ASSET is
// still the caller's to name, because which asset a shot's takes hang off is
// the same fact the image batch maps per panel — and the frontend keys it on
// the shot instance, so re-rendering a shot is a new take, not a new object.

// CollectVideoJobResultsRequest names the video jobs to collect.
type CollectVideoJobResultsRequest struct {
	// AssetByJob maps a JOB's identifier to the asset whose versions its
	// result becomes. A video job's entity is the SHOT, so the consumer comes
	// from the job itself; the asset is the one fact a caller states.
	AssetByJob map[string]string
	// JobIDs are the jobs to collect.
	JobIDs []string
	// UsageRole is what the versions are used AS. Empty is "video", the role
	// a shot's moving picture carries.
	UsageRole string
}

// CollectVideoJobResults turns each SUCCEEDED video job's result into a
// candidate asset version whose usage names the shot.
//
// The rules are the image batch's, restated for video: a job that has not
// succeeded is SKIPPED (a batch's jobs finish at different times), a succeeded
// job whose result names no file is REFUSED (there is nothing to preview), and
// a job already collected is REPORTED as a duplicate (a restarted collection
// continues).
func (s *Service) CollectVideoJobResults(ctx context.Context, request CollectVideoJobResultsRequest) ([]CollectedCandidate, error) {
	if s == nil || s.jobs == nil || s.assets == nil {
		return nil, agent.UnavailableError()
	}
	role := strings.TrimSpace(request.UsageRole)
	if role == "" {
		role = "video"
	}
	collected := make([]CollectedCandidate, 0, len(request.JobIDs))
	for _, rawJobID := range request.JobIDs {
		jobID := strings.TrimSpace(rawJobID)
		if jobID == "" {
			continue
		}
		record, err := s.jobs.Get(ctx, jobID)
		if err != nil {
			return collected, err
		}
		if record.Status != job.StatusSucceeded {
			continue
		}
		if record.JobType != job.JobTypeVideoGeneration {
			// A job of another kind in this list is a caller mistake: collecting
			// an audio job as video would attach sound where a picture is read.
			return collected, agent.InvalidError("That job is not a video job.")
		}
		shotID := strings.TrimSpace(record.EntityID)
		if shotID == "" {
			// A video job that does not name its shot cannot carry a usage the
			// timeline's join would find, so refusing names the actual gap.
			return collected, agent.InvalidError("A collected video job must name the shot it renders.")
		}
		assetID := strings.TrimSpace(request.AssetByJob[jobID])
		if assetID == "" {
			return collected, agent.InvalidError("A collected video job must name the asset its result belongs to.")
		}
		files := videoResultFiles(record.ResultJSON)
		if len(files) == 0 {
			return collected, agent.InvalidError("A succeeded video job's result named no committed file.")
		}
		input := videoJobInputOf(record.InputJSON)
		version, _, err := s.assets.AttachJobResult(ctx, appassets.AttachJobResultRequest{
			AssetID:          assetID,
			JobID:            record.ID,
			Prompt:           input.Prompt,
			ProviderConfigID: record.ProviderConfigID,
			ModelConfigID:    record.ModelConfigID,
			Files:            files,
		})
		if err != nil {
			if isDuplicateCandidate(err) {
				collected = append(collected, CollectedCandidate{
					JobID: record.ID, ItemID: shotID, AssetID: assetID, Duplicate: true,
				})
				continue
			}
			return collected, err
		}
		if _, err := s.assets.AddUsage(ctx, appassets.AddUsageRequest{
			AssetVersionID: version.ID,
			ConsumerType:   asset.ConsumerShot,
			ConsumerID:     shotID,
			UsageRole:      role,
			Required:       false,
		}); err != nil {
			return collected, err
		}
		collected = append(collected, CollectedCandidate{
			JobID: record.ID, ItemID: shotID, AssetID: assetID,
			VersionID: version.ID, VersionNumber: version.VersionNumber,
		})
	}
	return collected, nil
}

// videoResultFiles reads the committed files out of a video job's result,
// with the first file primary — the file a preview player and the exporter
// both open.
func videoResultFiles(resultJSON string) []appassets.AttachJobFile {
	files := resultFileHashes(resultJSON)
	out := make([]appassets.AttachJobFile, 0, len(files))
	for _, file := range files {
		out = append(out, appassets.AttachJobFile{
			FileHash: file.FileHash, Role: asset.RolePrimary, Ordinal: file.Ordinal,
		})
	}
	return out
}

// videoJobInput is the video job's stored input, read for the prompt a
// version records — the same shape the runner's own videoInput uses in the
// field this collector needs, duplicated for the reason audioJobInput states:
// this package must not depend on the runner's internals.
type videoJobInput struct {
	Prompt string `json:"prompt"`
}

// videoJobInputOf decodes the prompt, tolerating anything malformed: a job
// whose input cannot be read still has a result worth collecting, and the
// prompt is provenance rather than content.
func videoJobInputOf(inputJSON string) videoJobInput {
	var input videoJobInput
	trimmed := strings.TrimSpace(inputJSON)
	if trimmed == "" {
		return input
	}
	_ = json.Unmarshal([]byte(trimmed), &input)
	return input
}
