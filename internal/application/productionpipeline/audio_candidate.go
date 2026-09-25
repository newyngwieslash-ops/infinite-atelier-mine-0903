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

// audio_candidate.go turns a SUCCEEDED audio job's result into the asset version a mix reads.
//
// # The gap this closes, and how it was found
//
// A TTS job's bytes were committed to the file store and the job was marked succeeded — and that was the
// end of it. Nothing turned the result into an asset version, so **the speech a user generated never
// reached the mix**: `attachAudioClips` joins `asset_usages` on a shot and reads the version's primary
// file, and no version was ever created. The only rows of that shape in the whole build were written by
// WP-11's acceptance walk BY HAND, under a comment that says it writes "the asset, version, file and
// usage **a TTS job's result leaves**" — a path that did not exist.
//
// The same shape as WP-29's two defects: every test passed because every test supplied its own
// equivalent of production. `AttachJobResult` has a Wails binding, the audio section listed a job's
// `resultFiles`, and nothing joined the two.
//
// # Why it lives beside the image batch's collector
//
// Because it is the same act, and because the alternative is a second implementation of "a job's result
// becomes a version with a role" — the duplication this repository treats as the defect rather than the
// accident. What differs between the two is only what a role MEANS, and that is a string.

// CollectAudioJobResultsRequest names the jobs to collect and the role their versions carry.
type CollectAudioJobResultsRequest struct {
	// AssetByJob maps a JOB's identifier to the asset whose versions its result becomes.
	//
	// It is keyed on the job rather than on the job's entity because an audio job's entity is a
	// dialogue LINE and the version belongs to an ASSET — which line a job rendered and which asset
	// holds the result are two facts, and a map from one to the other is where a caller states the
	// second. The image batch can key on the entity because its asset is per panel; audio has no such
	// rule, so the mapping is explicit.
	AssetByJob map[string]string
	// JobIDs are the jobs to collect.
	JobIDs []string
	// UsageRole is what the versions are used AS: `audio_dialogue` for a line's speech, `audio_effect`
	// for a sound effect. Empty means dialogue, which is what an audio job meant before roles existed.
	UsageRole string
	// ConsumerType and ConsumerID name what consumes the version, and both are required: the mix's join
	// looks for `consumer_type = 'shot'` with the consuming row's shot id, so a usage recorded any other
	// way is a row no read finds.
	ConsumerType string
	ConsumerID   string
}

// CollectAudioJobResults turns each SUCCEEDED audio job's result into an approved asset version.
//
// # The version is APPROVED, not a candidate
//
// A panel's images are candidates because a user chooses among several; a line's speech is not a choice
// between renders, it is the take the user asked for. Approving here is what makes the mix find it: the
// audio read requires `au.asset_version_id = aa.current_approved_version_id`, so a candidate would be
// invisible until somebody approved it through a surface that does not exist for audio.
//
// # A job that has not succeeded is SKIPPED, and a succeeded one with no file is REFUSED
//
// The first mirrors the image collector: a batch's jobs finish at different times, so a collection that
// refused on the first unfinished one could never be called while anything was still rendering. The
// second is the opposite, for the same reason the image collector refuses it: a succeeded job whose
// result names no file is a job the collector cannot turn into audio, and skipping it would leave the
// user with a succeeded job and no sound.
func (s *Service) CollectAudioJobResults(ctx context.Context, request CollectAudioJobResultsRequest) ([]CollectedCandidate, error) {
	if s == nil || s.jobs == nil || s.assets == nil {
		return nil, agent.UnavailableError()
	}
	role := strings.TrimSpace(request.UsageRole)
	if role == "" {
		role = "audio_dialogue"
	}
	consumerType := strings.TrimSpace(request.ConsumerType)
	consumerID := strings.TrimSpace(request.ConsumerID)
	if consumerType == "" || consumerID == "" {
		// Refused rather than defaulted: the mix's join is (consumer_type='shot', consumer_id=<a shot>),
		// and a caller that did not say which shot would get a version nothing reads — which is the
		// defect this whole file exists to close.
		return nil, agent.InvalidError("A collected audio job must name what consumes it.")
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
			// Not a refusal: the job is queued, running or failed, and the caller's next step for each
			// of those is different and its own.
			continue
		}
		if record.JobType != job.JobTypeAudioGeneration {
			// A job of another kind in this list is a caller mistake rather than a skip: collecting an
			// image job as audio would attach an image to a shot's audio column.
			return collected, agent.InvalidError("That job is not an audio job.")
		}
		assetID := strings.TrimSpace(request.AssetByJob[jobID])
		if assetID == "" {
			return collected, agent.InvalidError("A collected audio job must name the asset its result belongs to.")
		}
		files := audioResultFiles(record.ResultJSON)
		if len(files) == 0 {
			return collected, agent.InvalidError("A succeeded audio job's result named no committed file.")
		}
		input := audioJobInputOf(record.InputJSON)
		version, _, err := s.assets.AttachJobResult(ctx, appassets.AttachJobResultRequest{
			AssetID:          assetID,
			JobID:            record.ID,
			Prompt:           input.Text,
			ProviderConfigID: record.ProviderConfigID,
			ModelConfigID:    record.ModelConfigID,
			ModelParameters:  input.Voice,
			Files:            files,
		})
		if err != nil {
			if isDuplicateCandidate(err) {
				// The job was already collected. Reported rather than refused, for the reason the image
				// batch reports it: a restarted collection asks about the same job ids, and "it is
				// already there" is the answer that lets it continue.
				collected = append(collected, CollectedCandidate{
					JobID: record.ID, ItemID: record.EntityID, AssetID: assetID, Duplicate: true,
				})
				continue
			}
			return collected, err
		}
		if _, err := s.assets.ApproveVersion(ctx, appassets.ApproveVersionRequest{
			VersionID: version.ID,
			// The flag is a statement about having LOOKED, and this collector has: the version was
			// created one step above by this same command, so it replaces nothing.
			ImpactAcknowledged: true,
		}); err != nil {
			return collected, err
		}
		if _, err := s.assets.AddUsage(ctx, appassets.AddUsageRequest{
			AssetVersionID: version.ID,
			ConsumerType:   asset.ConsumerType(consumerType),
			ConsumerID:     consumerID,
			UsageRole:      role,
			Required:       false,
		}); err != nil {
			return collected, err
		}
		collected = append(collected, CollectedCandidate{
			JobID: record.ID, ItemID: record.EntityID, AssetID: assetID,
			VersionID: version.ID, VersionNumber: version.VersionNumber,
		})
	}
	return collected, nil
}

// audioResultFiles reads the committed files out of a job's result document.
//
// It is `resultFileHashes`'s shape with a role attached, and the role is `primary` because that is what
// the audio reader selects: `AudioFileFor` reads `role = 'primary'`, so a file attached under any other
// role would be stored and never played.
func audioResultFiles(resultJSON string) []appassets.AttachJobFile {
	files := resultFileHashes(resultJSON)
	out := make([]appassets.AttachJobFile, 0, len(files))
	for _, file := range files {
		out = append(out, appassets.AttachJobFile{
			FileHash: file.FileHash, Role: asset.RolePrimary, Ordinal: file.Ordinal,
		})
	}
	return out
}

// audioJobInput is the audio job's stored input, read for the facts a version records.
//
// It mirrors the runner's own `audioInput` in the two fields this collector needs. The duplication is
// deliberate and is the same one the binding's own input type carries: this package must not depend on
// the runner's internals, and the two are kept in step by the test that submits through the pipeline and
// reads the version back.
type audioJobInput struct {
	Text  string `json:"text"`
	Voice string `json:"voice"`
}

// audioJobInputOf decodes the fields a version records, tolerating anything malformed.
//
// A job whose input cannot be read still has a result worth collecting — the bytes are the point, and the
// voice is provenance — so a decode failure yields the zero value rather than refusing the collection.
func audioJobInputOf(inputJSON string) audioJobInput {
	var input audioJobInput
	trimmed := strings.TrimSpace(inputJSON)
	if trimmed == "" {
		return input
	}
	_ = json.Unmarshal([]byte(trimmed), &input)
	return input
}
