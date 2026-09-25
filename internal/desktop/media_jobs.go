package desktop

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// media_jobs.go is the two media job submissions: a video from a storyboard row, and a line's TTS.
//
// # Why these are Jobs and not agent tools
//
// ADR-0011 section 6 rules it, quoting AGENT_CONTRACTS section 19: "媒体生成本身由 Job/Provider Service
// 执行，不让 LLM 阻塞等待大文件". A video generation is minutes of polling and a TTS call is seconds of
// synthesis, and an agent that waited for either would be holding a stage open on a provider's clock.
// So the submission is a COMMAND — a user pressed a button, or a pipeline submitted a batch — and the
// job core does the work with leases, retries and restart recovery it already has.
//
// # What the two submissions share, and what they do not
//
// Both validate their identifiers, marshal a typed input and submit with their OWN scope, because the
// scope is what the idempotency key is built from: two submissions that shared one would make a video
// request collide with an audio one that happened to name the same entity.

// Bounds on the media inputs, so one command cannot enqueue an unbounded payload.
const (
	// maxVideoSeconds bounds one video request. A shot is seconds long, and an episode's worth of
	// minutes in a single call is a caller that meant a batch.
	maxVideoSeconds = 60
	// maxMediaReferences bounds the reference images one video request carries: a first frame, a last
	// frame and a handful of style references.
	maxMediaReferences = 8
	// maxAudioTextRunes bounds one TTS request. It is well above a line of dialogue and far below a
	// whole script, which would be a batch.
	maxAudioTextRunes = 2000
)

// SubmitVideoJobRequest asks for one shot's video.
//
// It names the SHOT rather than a panel, because FR-080's "同一 Shot 可保留多个视频版本" makes the shot
// the unit: a panel is one frame of it, and the video is of the shot.
type SubmitVideoJobRequest struct {
	ProjectID string `json:"projectId"`
	EpisodeID string `json:"episodeId"`
	// ShotID is the shot the video is of, and it is the job's entity so a reader can find the media
	// that belongs to a shot without a second table.
	ShotID     string `json:"shotId"`
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
	Prompt     string `json:"prompt"`
	Seconds    int    `json:"seconds,omitempty"`
	Size       string `json:"size,omitempty"`
	// References, FirstFrame and LastFrame carry base64 or data URLs, which is what the image input
	// already does: they are job input rather than secrets, and they are stored on the job row.
	References     []string `json:"references,omitempty"`
	ReferenceMIMEs []string `json:"referenceMimes,omitempty"`
	FirstFrame     string   `json:"firstFrame,omitempty"`
	FirstFrameMIME string   `json:"firstFrameMime,omitempty"`
	LastFrame      string   `json:"lastFrame,omitempty"`
	LastFrameMIME  string   `json:"lastFrameMime,omitempty"`
	Priority       int      `json:"priority,omitempty"`
}

// SubmitVideoJob enqueues a video generation for one shot.
//
// The returned DTO is the existing job when an identical request was already submitted, so a caller
// that double-clicked gets one job rather than two: the idempotency key is the scope, the project, the
// shot and the input's hash, and a second identical call produces the same key.
func (b *JobsBinding) SubmitVideoJob(request SubmitVideoJobRequest) (JobDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return JobDTO{}, err
	}
	if strings.TrimSpace(request.ProjectID) == "" || strings.TrimSpace(request.EpisodeID) == "" ||
		strings.TrimSpace(request.ShotID) == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	if strings.TrimSpace(request.ProviderID) == "" || strings.TrimSpace(request.Model) == "" ||
		strings.TrimSpace(request.Prompt) == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	seconds := request.Seconds
	if seconds <= 0 {
		seconds = 4
	}
	if seconds > maxVideoSeconds {
		// A single command must not enqueue an unbounded clip: a provider bills by the second, and
		// SECURITY requires an explicit limit.
		return JobDTO{}, bindingInvalidInput()
	}
	if len(request.References)+len(request.ReferenceMIMEs) > maxMediaReferences*2 {
		return JobDTO{}, bindingInvalidInput()
	}
	input := videoJobInput{
		Prompt:         request.Prompt,
		Model:          request.Model,
		ProviderID:     request.ProviderID,
		Seconds:        seconds,
		Size:           request.Size,
		References:     request.References,
		ReferenceMIMEs: request.ReferenceMIMEs,
		FirstFrame:     request.FirstFrame,
		FirstFrameMIME: request.FirstFrameMIME,
		LastFrame:      request.LastFrame,
		LastFrameMIME:  request.LastFrameMIME,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return JobDTO{}, bindingInvalidInput()
	}
	record, _, err := service.Submit(ctx, appjobs.SubmitRequest{
		ProjectID:        request.ProjectID,
		EntityType:       "shot",
		EntityID:         request.ShotID,
		JobType:          job.JobTypeVideoGeneration,
		Priority:         request.Priority,
		ProviderConfigID: request.ProviderID,
		InputJSON:        string(encoded),
		// The scope names the KIND of submission, so a video request and an audio one can never share
		// an idempotency key even when they name the same entity.
		Scope: "submit-video-job",
	})
	if err != nil {
		return JobDTO{}, toAppError(err)
	}
	return toJobDTO(record), nil
}

// SubmitVideoBatchRequest asks for videos for SEVERAL shots at once.
//
// # Why this is a loop over the single-shot command rather than a new job type
//
// A provider's video API generates ONE clip per request — ADR-0027's protocol has no "several shots"
// form — so a batch is several submissions, and inventing a job type that meant "many" would be a
// second state machine with its own retry, cancellation and idempotency semantics. What a user wants
// from a batch is one control and one report, and both are delivered here over the submission that
// already exists.
type SubmitVideoBatchRequest struct {
	ProjectID  string `json:"projectId"`
	EpisodeID  string `json:"episodeId"`
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
	Prompt     string `json:"prompt,omitempty"`
	Seconds    int    `json:"seconds,omitempty"`
	Size       string `json:"size,omitempty"`
	// ShotIDs are the shots to generate for, in the order the caller listed them.
	ShotIDs  []string `json:"shotIds"`
	Priority int      `json:"priority,omitempty"`
}

// VideoBatchItemDTO is one shot's outcome.
//
// A REFUSAL carries a reason rather than only being absent from the successes, because a batch whose
// report said "3 of 5" would leave a user counting rows to find which two to retry.
type VideoBatchItemDTO struct {
	ShotID string `json:"shotId"`
	// JobID and Status are set when the submission succeeded. Status is the job's own status, which on
	// a first submission is "queued" and on a replayed one is whatever the existing job is in.
	JobID  string `json:"jobId,omitempty"`
	Status string `json:"status,omitempty"`
	// Duplicate reports that an identical request had already been submitted. It is not a failure: the
	// idempotency key is what makes a double-click one job, and saying so is more useful than silence.
	Duplicate bool `json:"duplicate,omitempty"`
	// Refused carries the safe message when the shot was not submitted.
	Refused string `json:"refused,omitempty"`
}

// SubmitVideoBatchResultDTO reports what a batch did.
//
// It is `Submitted` and `Refused` rather than one list with a flag, so a caller renders the two
// differently without inspecting every row: what succeeded is a queue to watch, and what was refused
// is a message to read.
type SubmitVideoBatchResultDTO struct {
	Submitted []VideoBatchItemDTO `json:"submitted"`
	Refused   []VideoBatchItemDTO `json:"refused"`
}

// SubmitVideoBatch enqueues one video generation per shot.
//
// # One shot's failure does not undo another's
//
// The submissions are independent and the report is per shot. Aborting the batch on the first refusal
// would leave the earlier submissions in the queue with nothing saying so — a user would see a failed
// batch and three jobs running. The same ruling `RunImageBatch` made.
func (b *JobsBinding) SubmitVideoBatch(request SubmitVideoBatchRequest) (SubmitVideoBatchResultDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return SubmitVideoBatchResultDTO{Submitted: []VideoBatchItemDTO{}, Refused: []VideoBatchItemDTO{}}, err
	}
	// The service is passed as the INTERFACE the loop needs rather than as the concrete type, which is
	// what lets `submitVideoBatch` be driven by a test double with no database — and it also means the
	// batch has no test-only field to install, which is the seam that lets production drift from what
	// the tests exercised.
	return submitVideoBatch(ctx, service, request)
}

// submitVideoBatch is the batch's body, over the one method it needs.
//
// It is unexported and takes the submitter as an argument so its tests can be about the LOOP — the
// bound, the duplicate, the per-item refusal — without a job store. The path from `Submit` to a stored
// row is the job service's own suite's business, and a batch test that needed a database would be
// testing two things at once.
func submitVideoBatch(ctx context.Context, service jobSubmitter, request SubmitVideoBatchRequest) (SubmitVideoBatchResultDTO, error) {
	result := SubmitVideoBatchResultDTO{Submitted: []VideoBatchItemDTO{}, Refused: []VideoBatchItemDTO{}}
	if strings.TrimSpace(request.ProjectID) == "" || strings.TrimSpace(request.EpisodeID) == "" {
		return result, bindingInvalidInput()
	}
	if strings.TrimSpace(request.ProviderID) == "" || strings.TrimSpace(request.Model) == "" {
		return result, bindingInvalidInput()
	}
	if len(request.ShotIDs) == 0 {
		// An empty batch is refused rather than reported as a success that did nothing: a user who
		// pressed the button with nothing selected asked a question, and "0 of 0" is not an answer.
		return result, bindingInvalidInput()
	}
	if len(request.ShotIDs) > maxVideoBatch {
		// THE BOUND IS LOWER THAN THE IMAGE BATCH'S, and the reason is cost rather than payload: one
		// video is billed by the SECOND of footage and takes minutes to produce, where an image is a
		// single render.
		//
		// THE REFUSAL NAMES THE LIMIT, which is not cosmetic: the UI carries a copy of this number to
		// render its hint, and a refusal that named the real bound is what turns a drifted copy into a
		// message a user can act on rather than a silently shortened batch. A refusal of the whole
		// request, before any submission, is also why the UI's hint cannot go stale in the harmful
		// direction — nothing is queued under a report that disagrees with it.
		return result, apperror.New("VIDEO_BATCH_TOO_LARGE", "invalid_input", false,
			"A video batch generates for at most "+strconv.Itoa(maxVideoBatch)+" shots at a time.", nil)
	}
	// The prompt is shared, and a batch with none is refused for the same reason the single command
	// refuses it: a video request without a prompt names nothing to generate.
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		prompt = defaultBatchVideoPrompt
	}
	seconds := request.Seconds
	if seconds <= 0 {
		seconds = 4
	}
	if seconds > maxVideoSeconds {
		return result, bindingInvalidInput()
	}

	seen := map[string]bool{}
	for _, rawShotID := range request.ShotIDs {
		shotID := strings.TrimSpace(rawShotID)
		item := VideoBatchItemDTO{ShotID: shotID}
		switch {
		case shotID == "":
			item.Refused = "A batch item names no shot."
		case seen[shotID]:
			// A repeated id would submit the same generation twice under one key, which the core's
			// idempotency turns into a duplicate rather than a second job — but the second entry in the
			// report would then claim a submission that never happened separately. Refused so the count
			// means what it says.
			item.Refused = "That shot appears more than once in this batch."
		default:
			seen[shotID] = true
			record, duplicate, err := submitOneVideo(ctx, service, request, shotID, prompt, seconds)
			if err != nil {
				item.Refused = safeBatchMessage(err)
				result.Refused = append(result.Refused, item)
				continue
			}
			item.JobID = record.ID
			item.Status = string(record.Status)
			item.Duplicate = duplicate
		}
		if item.Refused != "" {
			result.Refused = append(result.Refused, item)
			continue
		}
		result.Submitted = append(result.Submitted, item)
	}
	return result, nil
}

// submitOneVideo submits one shot's request, which is the SINGLE command's own path.
//
// It exists as a function rather than a second copy of the marshalling, because a batch that built its
// input differently from the single command would produce a DIFFERENT idempotency key for the same
// request — and a user who submitted one shot, then the batch containing it, would get two jobs.
func submitOneVideo(ctx context.Context, service jobSubmitter, request SubmitVideoBatchRequest,
	shotID, prompt string, seconds int) (job.Job, bool, error) {
	input := videoJobInput{
		Prompt: prompt, Model: request.Model, ProviderID: request.ProviderID,
		Seconds: seconds, Size: request.Size,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return job.Job{}, false, bindingInvalidInput()
	}
	record, duplicate, err := service.Submit(ctx, appjobs.SubmitRequest{
		ProjectID: request.ProjectID, EntityType: "shot", EntityID: shotID,
		JobType: job.JobTypeVideoGeneration, Priority: request.Priority,
		ProviderConfigID: request.ProviderID, InputJSON: string(encoded),
		Scope: "submit-video-job",
	})
	if err != nil {
		return job.Job{}, false, err
	}
	return record, duplicate, nil
}

// jobSubmitter is the one method the batch needs from the job service.
//
// It is an interface here rather than the concrete service so the batch's own tests can drive the
// refusal paths — a duplicate shot, a refused item — without a database. The alternative, passing the
// service itself, would make every batch test a database test.
type jobSubmitter interface {
	Submit(ctx context.Context, request appjobs.SubmitRequest) (job.Job, bool, error)
}

// maxVideoBatch bounds one batch. See the comment in SubmitVideoBatch for why it is below the image
// batch's eight: a video is billed by the second and takes minutes to produce.
const maxVideoBatch = 6

// defaultBatchVideoPrompt is what a batch submits when the caller stated none.
//
// A batch spans several shots, so it cannot carry one shot's prompt. The default says what the request
// is rather than leaving the field empty: a provider asked to generate from nothing either refuses or
// invents, and naming the shot's own drawing is the honest instruction.
const defaultBatchVideoPrompt = "Generate this shot as filmed, following the storyboard."

// safeBatchMessage renders a submission failure for one batch item.
//
// It goes through `toAppError` — the SAME conversion every other command uses — rather than reading a
// message off the error itself. That is what keeps the safe-message rule in one place: `toAppError`
// classifies a provider failure into the application's taxonomy and never lets a raw error's text
// reach a user, and a batch that rendered its own message would be a second implementation of the rule
// with a second set of leaks to miss.
func safeBatchMessage(err error) string {
	if err == nil {
		return ""
	}
	converted := toAppError(err)
	if appErr, ok := converted.(*apperror.Error); ok && strings.TrimSpace(appErr.SafeMessage) != "" {
		return appErr.SafeMessage
	}
	return "That shot could not be submitted."
}

// videoJobInput is the video job's stored input.
//
// It mirrors the runner's own `videoInput` field for field, and the duplication is deliberate: the
// binding must not import the infrastructure layer, and a shared type would make the Wails surface
// depend on the runner's internals. The two are kept in step by the test that submits a request and
// reads the job's input back.
type videoJobInput struct {
	Prompt         string   `json:"prompt"`
	Model          string   `json:"model"`
	ProviderID     string   `json:"providerId"`
	Seconds        int      `json:"seconds"`
	Size           string   `json:"size,omitempty"`
	References     []string `json:"references,omitempty"`
	ReferenceMIMEs []string `json:"referenceMimes,omitempty"`
	FirstFrame     string   `json:"firstFrame,omitempty"`
	FirstFrameMIME string   `json:"firstFrameMime,omitempty"`
	LastFrame      string   `json:"lastFrame,omitempty"`
	LastFrameMIME  string   `json:"lastFrameMime,omitempty"`
}

// SubmitAudioJobRequest asks for one dialogue line's speech.
type SubmitAudioJobRequest struct {
	ProjectID string `json:"projectId"`
	EpisodeID string `json:"episodeId"`
	// DialogueLineID is the line the speech renders, and it is the job's entity — which is what
	// AC-MEDIA-002's "audio linked to character/line" reads: the job names the line, and the line names
	// the character.
	DialogueLineID string `json:"dialogueLineId"`
	ProviderID     string `json:"providerId"`
	Model          string `json:"model"`
	Text           string `json:"text"`
	Voice          string `json:"voice,omitempty"`
	Format         string `json:"format,omitempty"`
	Speed          string `json:"speed,omitempty"`
	Priority       int    `json:"priority,omitempty"`
}

// SubmitAudioJob enqueues a TTS request for one dialogue line.
func (b *JobsBinding) SubmitAudioJob(request SubmitAudioJobRequest) (JobDTO, error) {
	service, ctx, err := b.requestService()
	if err != nil {
		return JobDTO{}, err
	}
	if strings.TrimSpace(request.ProjectID) == "" || strings.TrimSpace(request.EpisodeID) == "" ||
		strings.TrimSpace(request.DialogueLineID) == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	if strings.TrimSpace(request.ProviderID) == "" || strings.TrimSpace(request.Model) == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return JobDTO{}, bindingInvalidInput()
	}
	// The bound exists because a TTS call is billed by the character: a caller that sent a whole
	// script would be buying an audiobook, and this command is for one line.
	if len([]rune(text)) > maxAudioTextRunes {
		return JobDTO{}, bindingInvalidInput()
	}
	input := audioJobInput{
		Text:       text,
		Model:      request.Model,
		ProviderID: request.ProviderID,
		Voice:      request.Voice,
		Format:     request.Format,
		Speed:      request.Speed,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return JobDTO{}, bindingInvalidInput()
	}
	record, _, err := service.Submit(ctx, appjobs.SubmitRequest{
		ProjectID:        request.ProjectID,
		EntityType:       "dialogue_line",
		EntityID:         request.DialogueLineID,
		JobType:          job.JobTypeAudioGeneration,
		Priority:         request.Priority,
		ProviderConfigID: request.ProviderID,
		InputJSON:        string(encoded),
		Scope:            "submit-audio-job",
	})
	if err != nil {
		return JobDTO{}, toAppError(err)
	}
	return toJobDTO(record), nil
}

// audioJobInput is the audio job's stored input, mirroring the runner's own `audioInput`.
type audioJobInput struct {
	Text       string `json:"text"`
	Model      string `json:"model"`
	ProviderID string `json:"providerId"`
	Voice      string `json:"voice,omitempty"`
	Format     string `json:"format,omitempty"`
	Speed      string `json:"speed,omitempty"`
}
