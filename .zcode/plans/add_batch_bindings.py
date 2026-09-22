import io

p = 'internal/desktop/drama_binding.go'
s = io.open(p, encoding='utf-8').read()

anchor = '''// SetShotOverridesRequest writes a plan's per-shot overrides document.'''

addition = '''// The production pipeline's batch commands.
//
// They are declared against a small INTERFACE rather than the concrete service so the binding
// can hold a nil one: a build without a job store still runs the five agent stages, and the
// batch then refuses with a reason rather than reporting a submission that never happened.
type ProductionBatch interface {
	CheckStoryboardGate(ctx context.Context, request appproductionpipeline.GateCheckRequest) error
	RunImageBatch(ctx context.Context, request appproductionpipeline.RunImageBatchRequest) (appproductionpipeline.RunImageBatchResult, error)
	CollectBatchResults(ctx context.Context, request appproductionpipeline.CollectBatchResultsRequest) ([]appproductionpipeline.CollectedCandidate, error)
	ApproveCandidate(ctx context.Context, request appproductionpipeline.ApproveCandidateRequest) (storyboard.StoryboardPanelVersion, error)
}

// AttachProductionBatch supplies the batch. A nil one leaves those commands failing closed.
func AttachProductionBatch(binding *DramaBinding, ctx context.Context, batch ProductionBatch) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.batch = batch
	binding.mu.Unlock()
}

func (b *DramaBinding) productionBatch() ProductionBatch {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.batch
}

// CheckStoryboardGateRequest asks whether an episode's board may be batched.
type CheckStoryboardGateRequest struct {
	EpisodeID           string `json:"episodeId"`
	StoryboardVersionID string `json:"storyboardVersionId,omitempty"`
	StoryboardID        string `json:"storyboardId,omitempty"`
}

// CheckStoryboardGate reports whether the batch gate would pass, and why not when it would
// not.
//
// It is the READ a UI makes before offering the button: AC-BOARD-001's "未通过时阻止批量生成" is
// a refusal the user should see as a REASON rather than as a failed click after the fact.
func (b *DramaBinding) CheckStoryboardGate(request CheckStoryboardGateRequest) error {
	batch := b.productionBatch()
	if batch == nil {
		return bindingUnavailable()
	}
	if err := batch.CheckStoryboardGate(b.context(), appproductionpipeline.GateCheckRequest{
		EpisodeID:           request.EpisodeID,
		StoryboardVersionID: request.StoryboardVersionID,
		StoryboardID:        request.StoryboardID,
	}); err != nil {
		return toDramaError(err)
	}
	return nil
}

// RunImageBatchRequest asks for a batch of candidate images.
type RunImageBatchRequest struct {
	StoryboardVersionID string `json:"storyboardVersionId"`
	EpisodeID           string `json:"episodeId"`
	ProjectID           string `json:"projectId"`
	// PerShotCandidates is how many candidates each shot gets.
	PerShotCandidates int `json:"perShotCandidates"`
	// ShotIDs narrows the batch to those shots. Empty means every shot in the board.
	ShotIDs []string `json:"shotIds,omitempty"`
	// ProviderID and ModelName are what the jobs are submitted to. They come from the caller:
	// section 6.1 puts provider selection outside the agent's reach.
	ProviderID string `json:"providerId"`
	ModelName  string `json:"modelName"`
	// PromptSuffix is appended to every derived prompt.
	PromptSuffix string `json:"promptSuffix,omitempty"`
	Seed         string `json:"seed,omitempty"`
}

// BatchSubmissionDTO is one job the batch submitted, or found already submitted.
type BatchSubmissionDTO struct {
	ShotID         string `json:"shotId"`
	ItemID         string `json:"itemId"`
	CandidateIndex int    `json:"candidateIndex"`
	JobID          string `json:"jobId"`
	Duplicate      bool   `json:"duplicate"`
}

// RunImageBatchResultDTO reports what a batch did.
type RunImageBatchResultDTO struct {
	Submissions []BatchSubmissionDTO `json:"submissions"`
	Duplicate   int                  `json:"duplicate"`
}

// RunImageBatch submits one job per candidate per shot.
func (b *DramaBinding) RunImageBatch(request RunImageBatchRequest) (RunImageBatchResultDTO, error) {
	batch := b.productionBatch()
	if batch == nil {
		return RunImageBatchResultDTO{}, bindingUnavailable()
	}
	result, err := batch.RunImageBatch(b.context(), appproductionpipeline.RunImageBatchRequest{
		StoryboardVersionID: request.StoryboardVersionID,
		EpisodeID:           request.EpisodeID,
		ProjectID:           request.ProjectID,
		PerShotCandidates:   request.PerShotCandidates,
		ShotIDs:             request.ShotIDs,
		ProviderID:          request.ProviderID,
		ModelName:           request.ModelName,
		PromptSuffix:        request.PromptSuffix,
		Seed:                request.Seed,
	})
	if err != nil {
		return RunImageBatchResultDTO{}, toDramaError(err)
	}
	out := RunImageBatchResultDTO{
		Submissions: make([]BatchSubmissionDTO, 0, len(result.Submitted)),
		Duplicate:   result.Duplicate,
	}
	for _, submission := range result.Submitted {
		out.Submissions = append(out.Submissions, BatchSubmissionDTO{
			ShotID: submission.ShotID, ItemID: submission.ItemID,
			CandidateIndex: submission.CandidateIndex, JobID: submission.JobID,
			Duplicate: submission.Duplicate,
		})
	}
	return out, nil
}

// CollectBatchResultsRequest asks for a batch's finished candidates.
type CollectBatchResultsRequest struct {
	// AssetByItem maps a storyboard item's id to the asset its candidates belong to.
	AssetByItem map[string]string `json:"assetByItem"`
	JobIDs      []string          `json:"jobIds"`
	UsageRole   string            `json:"usageRole,omitempty"`
}

// CollectedCandidateDTO is one finished job that became a candidate version.
type CollectedCandidateDTO struct {
	JobID         string `json:"jobId"`
	ItemID        string `json:"itemId"`
	AssetID       string `json:"assetId"`
	VersionID     string `json:"versionId,omitempty"`
	VersionNumber int    `json:"versionNumber,omitempty"`
	Duplicate     bool   `json:"duplicate"`
}

// CollectBatchResults turns each succeeded job's result into a candidate asset version.
func (b *DramaBinding) CollectBatchResults(request CollectBatchResultsRequest) ([]CollectedCandidateDTO, error) {
	batch := b.productionBatch()
	if batch == nil {
		return nil, bindingUnavailable()
	}
	collected, err := batch.CollectBatchResults(b.context(), appproductionpipeline.CollectBatchResultsRequest{
		AssetByItem: request.AssetByItem,
		JobIDs:      request.JobIDs,
		UsageRole:   request.UsageRole,
	})
	if err != nil {
		return nil, toDramaError(err)
	}
	out := make([]CollectedCandidateDTO, 0, len(collected))
	for _, candidate := range collected {
		out = append(out, CollectedCandidateDTO{
			JobID: candidate.JobID, ItemID: candidate.ItemID, AssetID: candidate.AssetID,
			VersionID: candidate.VersionID, VersionNumber: candidate.VersionNumber,
			Duplicate: candidate.Duplicate,
		})
	}
	return out, nil
}

// ApproveCandidateRequest approves one candidate as a panel's image.
type ApproveCandidateRequest struct {
	PanelVersionID              string   `json:"panelVersionId"`
	ApprovedImageAssetVersionID string   `json:"approvedImageAssetVersionId"`
	CandidateVersionIDs         []string `json:"candidateVersionIds"`
	// ExpectedRevision is the revision of the panel's parent storyboard item.
	ExpectedRevision int64 `json:"expectedRevision"`
}

// ApproveCandidate makes one candidate a panel's approved image.
//
// Section 9.5's rule — the approved image must be one of the panel's candidates — is enforced
// by the storyboard service this delegates to, not restated here.
func (b *DramaBinding) ApproveCandidate(request ApproveCandidateRequest) (StoryboardPanelVersionDTO, error) {
	batch := b.productionBatch()
	if batch == nil {
		return StoryboardPanelVersionDTO{}, bindingUnavailable()
	}
	record, err := batch.ApproveCandidate(b.context(), appproductionpipeline.ApproveCandidateRequest{
		PanelVersionID:              request.PanelVersionID,
		ApprovedImageAssetVersionID: request.ApprovedImageAssetVersionID,
		CandidateVersionIDs:         request.CandidateVersionIDs,
		ExpectedRevision:            request.ExpectedRevision,
	})
	if err != nil {
		return StoryboardPanelVersionDTO{}, toDramaError(err)
	}
	return toStoryboardPanelVersionDTO(record), nil
}

// SetShotOverridesRequest writes a plan's per-shot overrides document.'''
assert anchor in s, "anchor"
s = s.replace(anchor, addition, 1)

# The field.
s = s.replace('''	// productionPipeline drives the five production stages.''','''	// batch drives the image batch and its gate. It is a separate slot from both pipelines
	// because it needs services neither does — a job store and the assets service — so a build
	// can run the five agent stages without it and say so when the batch is asked for.
	batch ProductionBatch
	// productionPipeline drives the five production stages.''')

io.open(p, 'w', encoding='utf-8').write(s)
print("batch bindings added")
