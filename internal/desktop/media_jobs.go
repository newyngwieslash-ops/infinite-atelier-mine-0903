package desktop

import (
	"encoding/json"
	"strings"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
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
