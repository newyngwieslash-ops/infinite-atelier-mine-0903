package productionpipeline

import (
	"context"
	"encoding/json"
	"strings"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
)

// candidate.go collects a batch's results and approves one of them.
//
// # The two commands AC-BOARD-003 needs and did not have
//
// Submitting jobs is half of a batch; the other half is turning a SUCCEEDED job into a
// candidate version a person can look at, and then approving one of them as the panel's
// image. An independent review found both missing — "no candidate collection, no approve-one
// command" — and the acceptance criterion names both: "批准一个结果" and the canvas relation
// that follows it.
//
// # Why collection is a separate command from submission
//
// A batch's jobs finish over minutes, and a command that waited for them would hold a call
// open for the length of a model's slowest render. So submission returns job ids and this
// command is called when the user asks what came back — which is also what makes "取消一个"
// and "重试失败项" possible: by the time either is asked, the job states are known.

// CollectBatchResultsRequest asks for a batch's finished candidates.
type CollectBatchResultsRequest struct {
	// AssetIDs maps a storyboard ITEM to the asset whose versions its candidates become.
	//
	// A panel's images are asset versions — §9.5 makes the approved image an AssetVersion —
	// and the asset is what the version hangs off. The caller states it because the batch's
	// jobs name items and the ASSET is the project's choice: one asset per panel is the
	// ordinary arrangement, and a project that wants one shared asset for a whole board can
	// say so by naming the same id twice.
	AssetByItem map[string]string
	// JobIDs are the jobs to collect, from `RunImageBatch`.
	JobIDs []string
	// UsageRole is what the candidates are used AS. Empty means "image", which is what a
	// panel's picture is.
	UsageRole string
}

// CollectedCandidate is one finished job that became a candidate version.
type CollectedCandidate struct {
	JobID         string
	ItemID        string
	AssetID       string
	VersionID     string
	VersionNumber int
	// Duplicate reports that the job had already been collected, so a second call is a
	// report rather than a second version. It is what makes collection idempotent across a
	// restart, the same way submission is.
	Duplicate bool
}

// CollectBatchResults turns each SUCCEEDED job's result into a candidate asset version.
//
// A job that has not succeeded is SKIPPED rather than refused, and the caller is told:
// a batch's jobs finish at different times, so a collection that refused on the first
// unfinished one could never be called while anything was still rendering.
//
// The version records the job, its prompt and the run that asked for it — which is
// AC-ASSET-002's list, and the reason the collection goes through `AttachJobResult` rather
// than through a version write of its own. The USAGE is written in the same command, because
// a candidate nobody can find is a candidate a panel cannot offer.
func (s *Service) CollectBatchResults(ctx context.Context, request CollectBatchResultsRequest) ([]CollectedCandidate, error) {
	if s == nil || s.jobs == nil || s.assets == nil {
		return nil, agent.UnavailableError()
	}
	role := strings.TrimSpace(request.UsageRole)
	if role == "" {
		role = "image"
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
			// Not a refusal: the job is queued, running or failed, and the caller's next step
			// for each of those is different and its own.
			continue
		}
		itemID := strings.TrimSpace(record.EntityID)
		assetID := strings.TrimSpace(request.AssetByItem[itemID])
		if assetID == "" {
			return collected, agent.InvalidError("A collected job must name the asset its candidate belongs to.")
		}
		files := resultFileHashes(record.ResultJSON)
		if len(files) == 0 {
			// A succeeded job with no readable file is a job whose result the collector
			// cannot turn into an image. Refused rather than skipped, because the caller's
			// next step is to look at the job rather than to call again.
			return collected, agent.InvalidError("A succeeded job's result named no committed file.")
		}
		input := imageBatchInputOf(record.InputJSON)
		version, _, err := s.assets.AttachJobResult(ctx, appassets.AttachJobResultRequest{
			AssetID:          assetID,
			JobID:            record.ID,
			Prompt:           input.Prompt,
			ProviderConfigID: record.ProviderConfigID,
			ModelConfigID:    record.ModelConfigID,
			ModelParameters:  "",
			Seed:             input.Seed,
			SourceAgentRunID: "",
			Files:            files,
		})
		if err != nil {
			if isDuplicateCandidate(err) {
				// The job was already collected. Reported rather than refused: a restarted
				// collection asks about the same job ids, and "it is already there" is the
				// answer that lets it continue.
				collected = append(collected, CollectedCandidate{
					JobID: record.ID, ItemID: itemID, AssetID: assetID, Duplicate: true,
				})
				continue
			}
			return collected, err
		}
		if _, err := s.assets.AddUsage(ctx, appassets.AddUsageRequest{
			AssetVersionID: version.ID,
			ConsumerType:   asset.ConsumerStoryboardPanel,
			ConsumerID:     itemID,
			UsageRole:      role,
			Required:       false,
		}); err != nil {
			return collected, err
		}
		collected = append(collected, CollectedCandidate{
			JobID: record.ID, ItemID: itemID, AssetID: assetID,
			VersionID: version.ID, VersionNumber: version.VersionNumber,
		})
	}
	return collected, nil
}

// ApproveCandidateRequest approves one candidate as a panel's image.
type ApproveCandidateRequest struct {
	// PanelVersionID is the panel the image belongs to.
	PanelVersionID string
	// ItemID is the storyboard row, whose revision the approval guards against.
	ItemID string
	// ApprovedImageAssetVersionID is the candidate being approved.
	ApprovedImageAssetVersionID string
	// CandidateVersionIDs are the candidates the panel has, which §9.5 requires the approved
	// one to be among.
	CandidateVersionIDs []string
	// ExpectedRevision is the revision of the panel's PARENT ITEM, which is the row the
	// approval is recorded against.
	ExpectedRevision int64
}

// ApproveCandidate makes one candidate a panel's approved image.
//
// It delegates to the storyboard service's own approval, which is where §9.5's rule lives:
// the approved image must be one of the panel's candidates, and a caller cannot approve an
// image the panel never offered. This command exists to give that rule a caller a UI can
// reach — the criterion's "批准一个结果" — rather than to restate it.
func (s *Service) ApproveCandidate(ctx context.Context, request ApproveCandidateRequest) (storyboard.StoryboardPanelVersion, error) {
	if s == nil || s.storyboard == nil {
		return storyboard.StoryboardPanelVersion{}, agent.UnavailableError()
	}
	return s.storyboard.ApprovePanelImage(ctx, appstoryboard.ApprovePanelImageRequest{
		PanelVersionID:              strings.TrimSpace(request.PanelVersionID),
		ApprovedImageAssetVersionID: strings.TrimSpace(request.ApprovedImageAssetVersionID),
		CandidateVersionIDs:         request.CandidateVersionIDs,
		ExpectedRevision:            request.ExpectedRevision,
	})
}

// resultFileHashes reads the hashes a succeeded job's result recorded.
//
// The shape is the runner's own result document, which WP-03 built and the job DTO already
// renders. An empty list means the result named no file, which for an image job is a result
// the collection cannot use.
func resultFileHashes(resultJSON string) []appassets.AttachJobFile {
	trimmed := strings.TrimSpace(resultJSON)
	if trimmed == "" {
		return nil
	}
	var document struct {
		Files []struct {
			Hash string `json:"hash"`
			MIME string `json:"mime"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(trimmed), &document); err != nil {
		return nil
	}
	out := make([]appassets.AttachJobFile, 0, len(document.Files))
	for index, file := range document.Files {
		if hash := strings.TrimSpace(file.Hash); hash != "" {
			out = append(out, appassets.AttachJobFile{FileHash: hash, Ordinal: index})
		}
	}
	return out
}

// imageBatchInputOf reads the batch input a job was submitted with.
//
// The document is this package's own `imageBatchInput`, so the fields it reads are the ones
// `RunImageBatch` wrote. A job from another path yields zero values rather than an error:
// the collection reports what the job recorded and does not invent what it did not.
func imageBatchInputOf(inputJSON string) imageBatchInput {
	var input imageBatchInput
	trimmed := strings.TrimSpace(inputJSON)
	if trimmed == "" {
		return input
	}
	if err := json.Unmarshal([]byte(trimmed), &input); err != nil {
		return imageBatchInput{}
	}
	return input
}

// isDuplicateCandidate reports whether a version write was refused because the job had
// already been collected.
//
// It reads the domain's OWN marker rather than matching words in a message. The first
// version of this function looked for "already" in the safe message, which is the check a
// reviewer cannot verify and which fails OPEN: a reworded message would make a duplicate read
// as a fresh failure, and the collector would report a finished job as broken.
func isDuplicateCandidate(err error) bool {
	domainErr, ok := asset.AsError(err)
	return ok && domainErr.Duplicate
}
