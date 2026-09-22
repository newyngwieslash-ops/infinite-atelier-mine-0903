package productionpipeline

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// batch.go is AC-BOARD-003: one image job per candidate per shot, bounded concurrency,
// and no duplicate submission across a restart.
//
// # Why the batch is not a stage
//
// §10.1 lists `storyboard_image` as a production stage, and this package does NOT drive
// it through the stage machine. The reason is what the work IS: a stage runs one agent
// and produces one artifact to review, while a batch submits N generation jobs and then
// collects their results. Driving it through the mechanism would mean an "attempt" that
// runs no model and a "candidate artifact" that is a set of job ids — which is a shape the
// review report cannot describe and the gate cannot approve.
//
// So the two halves are separate commands with separate rules: the agent stages write the
// PLAN (which shots need which images, with what prompts — that is
// `storyboard_panel_generation`), and this file submits the jobs that execute it.
//
// # The four decisions AC-BOARD-003 turns on
//
// The candidate index is part of the idempotency key, so two candidates for one shot are
// two jobs rather than one replayed twice. The concurrency limit is a semaphore over the
// submissions. A duplicate submission — the same shot, the same index, the same inputs —
// returns the EXISTING job rather than a second one, which is what makes a restart safe
// without a ledger of what was submitted. And a shot whose required assets are missing is
// refused BEFORE any job is submitted, which is AC-BOARD-001's "未通过时阻止批量生成".

// DefaultMaxImageConcurrency bounds how many submissions are in flight at once.
//
// It is a bound on SUBMISSION rather than on execution: the job scheduler owns how many
// jobs run, and this only stops one command from enqueuing its whole batch in a burst. Two
// is the default because the submission does real work — it validates the shot, resolves
// the assets and derives the prompt — and a batch of forty shots would otherwise hold a
// database transaction's worth of work in memory at once.
const DefaultMaxImageConcurrency = 2

// MaxBatchCandidates bounds how many candidates one shot may ask for.
//
// AC-BOARD-003's scenario is two. A larger number is not refused for being expensive — a
// provider charges per image and that is the user's decision — but for being unbounded: a
// command that asked for a thousand candidates would enqueue a thousand jobs from one
// click.
const MaxBatchCandidates = 8

// BatchOptions configures the batch orchestrator.
type BatchOptions struct {
	Jobs       *appjobs.Service
	Storyboard *appstoryboard.Service
	Assets     *appassets.Service
	// Gaps answers whether the episode's approved analysis still has required assets
	// missing. It is what AC-BOARD-001's block reads.
	Gaps *appassets.GapService
	// MaxImageConcurrency bounds submissions in flight. Zero takes the default.
	MaxImageConcurrency int
}

// RunImageBatchRequest asks for a batch of candidate images.
type RunImageBatchRequest struct {
	// StoryboardVersionID is the board whose shots are being imaged. It must be APPROVED:
	// generating images for a board that is still under review would spend the user's
	// provider budget on rows a FIX may replace.
	StoryboardVersionID string
	// EpisodeID is the episode the board belongs to, and it is what the gap gate reads.
	EpisodeID string
	// ProjectID is the project the jobs are filed under.
	ProjectID string
	// PerShotCandidates is how many candidates each shot gets.
	PerShotCandidates int
	// ShotIDs narrows the batch to those shots. Empty means every shot in the board, which
	// is AC-BOARD-003's "每 Shot 2 candidates".
	ShotIDs []string
	// ProviderID and ModelName are what the jobs are submitted to. They come from the
	// caller rather than from a model: §6.1 puts provider selection outside the agent's
	// reach.
	ProviderID string
	ModelName  string
	// PromptSuffix is appended to every derived prompt, for a project-wide style clause.
	PromptSuffix string
	// Seed is passed to the provider and recorded on the candidate the collection makes.
	// Empty means the provider chooses, which is the ordinary case and which the collection
	// then records as empty rather than inventing one.
	Seed string
}

// BatchSubmission is one job the batch submitted, or found already submitted.
type BatchSubmission struct {
	ShotID string
	ItemID string
	// CandidateIndex counts from one within one shot.
	CandidateIndex int
	JobID          string
	// Duplicate reports that an identical submission already existed, which is what a
	// restarted batch sees for the shots its previous run reached.
	Duplicate bool
}

// RunImageBatchResult reports what a batch did.
type RunImageBatchResult struct {
	Submitted []BatchSubmission
	// Duplicate is how many of the submissions already existed.
	Duplicate int
}

// CheckStoryboardGate answers whether a batch may run at all.
//
// AC-BOARD-001 says the batch is blocked when the required assets are missing, and the
// four facts it reads are separate approvals rather than one: the script, the director
// plan, the storyboard, and the episode's gap report. Each refusal names which one, so a
// caller's next step is obvious.
//
// IT REFUSES WHEN ANYTHING IS UNKNOWN, which is the direction that matters: a gate that
// treated "no approved gap report" as "nothing missing" would let a batch run against a
// script nobody analysed, and the images would then be generated for shots whose required
// characters have no approved design.
func (s *Service) CheckStoryboardGate(ctx context.Context, request GateCheckRequest) error {
	if s == nil || s.storyboard == nil || s.gaps == nil {
		return agent.UnavailableError()
	}
	episodeID := trimmed(request.EpisodeID)
	if episodeID == "" {
		return agent.InvalidError("A batch must name the episode it images.")
	}
	// The gap report is checked FIRST because it is the one the user is most likely to
	// have forgotten: the other three are artifacts they created on purpose.
	if _, err := s.gaps.UnresolvedRequiredItems(ctx, episodeID); err != nil {
		return err
	}
	if request.StoryboardVersionID != "" {
		version, err := s.storyboard.GetStoryboardVersion(ctx, request.StoryboardVersionID)
		if err != nil {
			return err
		}
		if version.Status != versioning.StatusApproved {
			return agent.InvalidError("That storyboard version is not approved, so its shots are still under review.")
		}
	}
	storyboardID := trimmed(request.StoryboardID)
	if storyboardID == "" {
		return nil
	}
	approvedID, err := s.storyboard.ApprovedStoryboardVersionID(ctx, storyboardID)
	if err != nil {
		return err
	}
	if trimmed(approvedID) == "" {
		return agent.InvalidError("This episode has no approved storyboard version, so there is nothing to image.")
	}
	return nil
}

// RunImageBatch submits one job per candidate per shot.
func (s *Service) RunImageBatch(ctx context.Context, request RunImageBatchRequest) (RunImageBatchResult, error) {
	if s == nil || s.jobs == nil || s.storyboard == nil {
		return RunImageBatchResult{}, agent.UnavailableError()
	}
	versionID := trimmed(request.StoryboardVersionID)
	if versionID == "" {
		return RunImageBatchResult{}, agent.InvalidError("A batch must name the storyboard version whose shots it images.")
	}
	projectID := trimmed(request.ProjectID)
	if projectID == "" {
		return RunImageBatchResult{}, agent.InvalidError("A batch must name the project its jobs belong to.")
	}
	if trimmed(request.ProviderID) == "" || trimmed(request.ModelName) == "" {
		return RunImageBatchResult{}, agent.InvalidError("A batch must name the provider and model it generates with.")
	}
	candidates := request.PerShotCandidates
	if candidates <= 0 {
		candidates = 1
	}
	if candidates > MaxBatchCandidates {
		return RunImageBatchResult{}, agent.InvalidError("A batch may not ask for that many candidates per shot.")
	}
	// THE GATE RUNS BEFORE ANY SUBMISSION. AC-BOARD-001's "未通过时阻止批量生成" is not a
	// warning: a batch that submitted some jobs and then refused would have spent the
	// user's provider budget on work it decided not to do.
	if err := s.CheckStoryboardGate(ctx, GateCheckRequest{
		EpisodeID: request.EpisodeID, StoryboardVersionID: versionID,
	}); err != nil {
		return RunImageBatchResult{}, err
	}
	items, err := s.storyboard.ListStoryboardItems(ctx, versionID)
	if err != nil {
		return RunImageBatchResult{}, err
	}
	if len(items) == 0 {
		return RunImageBatchResult{}, agent.InvalidError("That storyboard version has no shots to image.")
	}
	// The selection is resolved into a SET before the loop, so a caller that named the
	// same shot twice does not get its work done twice.
	wanted := map[string]bool{}
	for _, id := range request.ShotIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			wanted[trimmed] = true
		}
	}
	selected := make([]storyboard.StoryboardItem, 0, len(items))
	for _, item := range items {
		if len(wanted) == 0 || wanted[item.ShotID] || wanted[item.ID] {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return RunImageBatchResult{}, agent.InvalidError("None of the named shots belong to that storyboard version.")
	}

	limit := s.maxImageConcurrency
	if limit <= 0 {
		limit = DefaultMaxImageConcurrency
	}
	// A BUFFERED SEMAPHORE rather than a worker pool: the order of the result is the
	// board's order, so a caller can zip it with the shots, and the limit bounds how many
	// submissions are in flight rather than how many jobs exist.
	type outcome struct {
		submission BatchSubmission
		err        error
	}
	results := make([]outcome, len(selected)*candidates)
	slots := make(chan struct{}, limit)
	var wait sync.WaitGroup
	index := 0
	for _, item := range selected {
		for candidate := 1; candidate <= candidates; candidate++ {
			position := index
			index++
			wait.Add(1)
			slots <- struct{}{}
			// The goroutine is started only once a slot is free, so the loop itself is
			// what applies the limit and no work is queued behind it.
			go func(item storyboard.StoryboardItem, candidate, position int) {
				defer wait.Done()
				defer func() { <-slots }()
				submission, err := s.submitCandidate(ctx, request, projectID, item, candidate)
				results[position] = outcome{submission: submission, err: err}
			}(item, candidate, position)
		}
	}
	wait.Wait()

	batch := RunImageBatchResult{Submitted: make([]BatchSubmission, 0, len(results))}
	for _, result := range results {
		if result.err != nil {
			// The failures are REPORTED rather than discarded, and the submissions that
			// succeeded are returned with them: a batch that half-ran is a state the user
			// must be able to see, and hiding the successes would make them re-submit
			// work that already exists.
			return batch, result.err
		}
		if result.submission.Duplicate {
			batch.Duplicate++
		}
		batch.Submitted = append(batch.Submitted, result.submission)
	}
	return batch, nil
}

// submitCandidate submits one shot's one candidate.
func (s *Service) submitCandidate(ctx context.Context, request RunImageBatchRequest, projectID string, item storyboard.StoryboardItem, candidate int) (BatchSubmission, error) {
	prompt := imagePromptFor(item, request.PromptSuffix)
	// THE INPUT NAMES THE CANDIDATE, which is what makes two candidates of one shot two
	// jobs rather than one replayed: `Submit` derives its key from the scope, the entity
	// and this document, so an input that did not carry the index would collide with
	// itself.
	input := imageBatchInput{
		Prompt:         prompt,
		Model:          request.ModelName,
		ProviderID:     request.ProviderID,
		Count:          1,
		CandidateIndex: candidate,
		ShotID:         item.ShotID,
		ItemID:         item.ID,
		Seed:           request.Seed,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return BatchSubmission{}, agent.InvalidError("That shot's prompt could not be encoded.")
	}
	// The entity is the ITEM rather than the shot: an image belongs to one row of one
	// board, and two boards of the same episode are two different sets of images.
	record, duplicate, err := s.jobs.Submit(ctx, appjobs.SubmitRequest{
		ProjectID:        projectID,
		EntityType:       "storyboard_item",
		EntityID:         item.ID,
		JobType:          job.JobTypeImageGeneration,
		ProviderConfigID: request.ProviderID,
		ModelConfigID:    request.ModelName,
		InputJSON:        string(encoded),
		Scope:            "storyboard-panel-batch",
	})
	if err != nil {
		return BatchSubmission{}, err
	}
	return BatchSubmission{
		ShotID: item.ShotID, ItemID: item.ID, CandidateIndex: candidate,
		JobID: record.ID, Duplicate: duplicate,
	}, nil
}

// imageBatchInput is the job input document one candidate's generation carries.
//
// It is a STRUCT rather than a bare prompt string because the field names are part of the
// job's identity: `Submit` hashes this document into the idempotency key, so a change to
// any of these fields is a different job and a replay of the same values is the same one.
type imageBatchInput struct {
	Prompt         string `json:"prompt"`
	Model          string `json:"model"`
	ProviderID     string `json:"providerId"`
	Count          int    `json:"count"`
	CandidateIndex int    `json:"candidateIndex"`
	ShotID         string `json:"shotId"`
	ItemID         string `json:"itemId"`
	// Seed is what the provider used, when the caller states one. It travels so the
	// COLLECTION can record it: AC-ASSET-002 names the seed, and a version whose seed was
	// dropped is a render nothing can reproduce.
	Seed string `json:"seed,omitempty"`
}

// imagePromptFor derives the generation prompt for one storyboard item.
//
// The prompt is built from the item's OWN descriptions, which are the storyboard stage's
// output: FR-070's visual, action and first-frame fields are what a model is asked to
// render. A caller's suffix is appended rather than replacing them, so a project-wide
// style clause cannot silently discard the shot's content.
//
// The order puts the visual description first because that is the subject, and the first
// frame last because it is the most specific. A field that is empty is SKIPPED rather than
// rendered as a blank line: a prompt with an empty clause tells the model nothing and
// costs tokens.
func imagePromptFor(item storyboard.StoryboardItem, suffix string) string {
	parts := make([]string, 0, 6)
	for _, part := range []string{
		item.VisualDescription,
		item.ActionDescription,
		item.ShotSize,
		item.CameraAngle,
		item.FirstFrameDescription,
	} {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if trimmed := strings.TrimSpace(suffix); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, ". ")
}
