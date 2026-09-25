package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// openai_audio.go is the SYNCHRONOUS speech adapter (FR-080's 音效建议与生成适配).
//
// # What did not exist, and what this replaces
//
// `AudioPortFor` resolved the mock for `mock_media` and returned `unsupported` for EVERY other kind —
// the same sentence WP-26 found on the video side, with the same consequence: the audio job pipeline's
// remote half had never been driven by a provider protocol. A user could submit a line's speech and only
// a deterministic mock could answer it.
//
// # Why this adapter is SYNCHRONOUS, and the video one is not
//
// `AudioPort` has ONE method — `GenerateAudio` — where `VideoPort` has four. Speech synthesis answers a
// request with bytes; video generation answers with an identifier and finishes later. So this adapter is
// shaped like `openai_image.go` (request, then a response body that is either bytes or a URL) rather
// than like `openai_video.go` (submit, poll, fetch, cancel). The port already stated which one this is,
// and an adapter that polled anyway would be inventing a protocol the port does not have.
//
// # The protocol this implements, stated as the ASSUMPTION it is
//
// One shape, and it is the OpenAI speech API's:
//
//	POST {base}/audio/speech  {"model": …, "input": …, "voice": …, "response_format": …}
//	    -> audio bytes (Content-Type: audio/*), or a JSON body carrying a URL
//
// **REAL PROVIDERS DIFFER AND THIS IS ONE SHAPE, NOT A UNIVERSAL ABSTRACTION** (ADR-0031). The value
// delivered here is a working adapter plus a path driven end to end against a fake vendor; a different
// vendor means changing this file rather than configuring it.
//
// # Why the adapter is testable without credentials
//
// Every call goes through the same `clientFactory` seam the image and video adapters use, so an
// `httptest` server plays a provider. That is how this package's tests exercise the protocol with no
// network and no key.

// OpenAIAudioAdapter implements the audio capability against an OpenAI-compatible endpoint.
type OpenAIAudioAdapter struct {
	registry *Registry
	// clientFactory is the test seam; production uses guardedClient so the SSRF policy cannot drift
	// between adapters.
	clientFactory func(config provider.Config) (*phttp.Client, error)
}

// NewOpenAIAudioAdapter builds the adapter over the registry's ports.
func NewOpenAIAudioAdapter(registry *Registry) *OpenAIAudioAdapter {
	return &OpenAIAudioAdapter{registry: registry}
}

func (a *OpenAIAudioAdapter) clientFor(config provider.Config) (*phttp.Client, error) {
	if a.clientFactory != nil {
		return a.clientFactory(config)
	}
	return guardedClient(config)
}

// audioSpeechPath is the protocol's path, in one place so a vendor that spells it differently is one
// edit. It is the OpenAI speech endpoint's.
const audioSpeechPath = "/audio/speech"

// maxAudioInlineBytes bounds an INLINE result.
//
// # It is the TRANSPORT's ceiling, not a number of this adapter's choosing
//
// This is the same correction WP-26 had to make: the first version of a bound like this was larger than
// the guarded client's own 10 MiB response cap, which meant the client would truncate first and the
// adapter's check could never fire. A bound the transport cannot reach is a dead rule.
//
// Speech is small — a minute of mono MP3 is well under a megabyte — so the transport's ceiling is far
// above anything real, and a response that reaches it is a provider answering with something that is
// not speech.
const maxAudioInlineBytes = 8 << 20

// GenerateAudio performs one synthesis request.
func (a *OpenAIAudioAdapter) GenerateAudio(ctx context.Context, request appjobs.AudioRequest) (appjobs.AudioOutcome, error) {
	if a == nil || a.registry == nil || a.registry.configs == nil {
		return appjobs.AudioOutcome{}, provider.NewUnsupportedError()
	}
	if strings.TrimSpace(request.Text) == "" {
		// Refused before any call, for the reason the mock refuses it: a synthesis request with no text
		// names nothing to say, and a provider asked anyway either refuses or invents.
		return appjobs.AudioOutcome{}, provider.NewInvalidInputError()
	}
	config, err := a.registry.configs.GetConfig(ctx, request.ProviderID)
	if err != nil {
		return appjobs.AudioOutcome{}, err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return appjobs.AudioOutcome{}, err
	}
	body, err := json.Marshal(audioSpeechBody(request))
	if err != nil {
		return appjobs.AudioOutcome{}, provider.NewInvalidInputError()
	}
	endpoint, err := endpointFor(config, audioSpeechPath)
	if err != nil {
		return appjobs.AudioOutcome{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return appjobs.AudioOutcome{}, provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	// The Accept names what this adapter can actually read: a speech response is audio bytes or a JSON
	// document carrying a link. Accepting `*/*` would invite a provider to answer with something neither
	// branch handles.
	httpRequest.Header.Set("Accept", "audio/*, application/json")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, request.JobID, request.Model, config, provider.StatusFailed, 0, started, err, "")
		return appjobs.AudioOutcome{}, err
	}

	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, request.JobID, request.Model, config, statusForError(err), 0, started, err, "")
		return appjobs.AudioOutcome{}, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)
	if response.StatusCode != http.StatusOK {
		mapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, request.JobID, request.Model, config, provider.StatusFailed, response.StatusCode, started, mapped, requestID)
		return appjobs.AudioOutcome{}, mapped
	}
	outcome, err := a.parseAudioResponse(response)
	if err != nil {
		a.audit(ctx, request.JobID, request.Model, config, provider.StatusFailed, response.StatusCode, started, err, requestID)
		return appjobs.AudioOutcome{}, err
	}
	a.audit(ctx, request.JobID, request.Model, config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
	return outcome, nil
}

// audioSpeechBody renders the request body.
//
// Zero values are OMITTED rather than sent: a voice the caller did not choose is a provider's default
// to apply, and sending an empty string would ask for a voice with no name. The same reasoning the
// video adapter's optional fields carry.
func audioSpeechBody(request appjobs.AudioRequest) map[string]any {
	body := map[string]any{
		"model": request.Model,
		"input": request.Text,
	}
	if voice := strings.TrimSpace(request.Voice); voice != "" {
		body["voice"] = voice
	}
	if format := strings.TrimSpace(request.Format); format != "" {
		body["response_format"] = format
	}
	if speed := strings.TrimSpace(request.Speed); speed != "" {
		// The speed travels as a STRING because that is how the job input carries it — the binding's
		// field is a string and the runner forwards it. A provider that wants a number is a different
		// protocol, recorded as such in ADR-0031 rather than guessed at here.
		body["speed"] = speed
	}
	return body
}

// parseAudioResponse reads whichever of the two shapes the provider answered with.
//
// # The branch is the CONTENT TYPE, not a guess
//
// A speech endpoint that returns bytes says `audio/...`; one that returns a link says
// `application/json`. Reading the type is what lets one adapter serve both without a flag, and it is
// also what makes a refusal possible: a response that is neither is a provider answering with something
// this adapter cannot turn into speech, and it is refused rather than stored.
func (a *OpenAIAudioAdapter) parseAudioResponse(response *http.Response) (appjobs.AudioOutcome, error) {
	contentType := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type")))
	if index := strings.IndexByte(contentType, ';'); index >= 0 {
		contentType = strings.TrimSpace(contentType[:index])
	}
	if strings.HasPrefix(contentType, "audio/") || contentType == "application/ogg" {
		return a.readAudioBytes(response, contentType)
	}
	if strings.Contains(contentType, "application/json") {
		return a.readAudioDocument(response)
	}
	// Neither shape. Refused with the type NAMED in the diagnostic rather than the body, because the
	// body of an unexpected response is the thing most likely to carry something that must not travel.
	return appjobs.AudioOutcome{}, provider.NewResponseInvalidError()
}

// readAudioBytes reads the inline shape under the transport's ceiling.
func (a *OpenAIAudioAdapter) readAudioBytes(response *http.Response, contentType string) (appjobs.AudioOutcome, error) {
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxAudioInlineBytes+1))
	if err != nil {
		return appjobs.AudioOutcome{}, provider.NewResponseInvalidError()
	}
	if len(payload) > maxAudioInlineBytes {
		// Refused rather than truncated: half a clip committed as a result would be VERIFIED and stored
		// as a complete generation, and a user would hear it stop.
		return appjobs.AudioOutcome{}, provider.NewResponseInvalidError()
	}
	if len(payload) == 0 {
		// An empty body with an audio content type is a provider that answered with nothing. It is a
		// refusal rather than a silent result, because the pipeline would store a zero-byte file and mark
		// the job succeeded.
		return appjobs.AudioOutcome{}, provider.NewResponseInvalidError()
	}
	// The provider's own type is passed through, with `mimeOrMP3` filling in only what it did not name.
	// An Ogg container arrives as `application/ogg` — which the result store's allowlist accepts under
	// that spelling, because that is what the sniffer reports — so rewriting it here would be this
	// adapter substituting its own opinion for the store's.
	return appjobs.AudioOutcome{Data: encodeBase64(payload), MIMEType: mimeOrMP3(contentType)}, nil
}

// readAudioDocument reads the JSON shape, which carries either inline base64 or a URL.
func (a *OpenAIAudioAdapter) readAudioDocument(response *http.Response) (appjobs.AudioOutcome, error) {
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxAudioInlineBytes))
	if err != nil {
		return appjobs.AudioOutcome{}, provider.NewResponseInvalidError()
	}
	var document struct {
		URL  string `json:"url"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		return appjobs.AudioOutcome{}, provider.NewResponseInvalidError()
	}
	if data := strings.TrimSpace(document.Data); data != "" {
		// The inline shape is not bounded here: the document was read under the transport's ceiling, so a
		// payload larger than that already arrived truncated and `json.Unmarshal` would have refused a
		// document cut in half.
		return appjobs.AudioOutcome{Data: data, MIMEType: mimeOrMP3("")}, nil
	}
	if url := strings.TrimSpace(document.URL); url != "" {
		// The MIME is left EMPTY on purpose: the download path sniffs the bytes, and a provider's claim
		// about a file it did not serve is not one this pipeline trusts.
		return appjobs.AudioOutcome{URL: url}, nil
	}
	// A JSON document naming neither. Refused, because the alternative is a job that succeeds with no
	// audio at all.
	return appjobs.AudioOutcome{}, provider.NewResponseInvalidError()
}

// mimeOrMP3 fills in the type a provider did not name.
//
// The fallback is `audio/mpeg` because that is what a speech endpoint returns when it is asked for
// nothing in particular, and because the result store's audio allowlist accepts it. It is applied only
// when the provider stated nothing — a stated type that the allowlist refuses is refused rather than
// rewritten, which is what keeps this from being a way to smuggle a type past the store.
func mimeOrMP3(stated string) string {
	trimmed := strings.TrimSpace(stated)
	if trimmed == "" {
		return "audio/mpeg"
	}
	// A provider may name a file with an extension in a JSON document; only a real MIME type is used.
	if _, _, err := mime.ParseMediaType(trimmed); err != nil {
		return "audio/mpeg"
	}
	return trimmed
}

// audit writes one redacted record with the AUDIO capability.
//
// It is the first caller of `provider.CapabilityAudio` in this build: the column's CHECK has admitted the
// value since migration 000003 and nothing had ever written it — the same gap WP-26 found for `video`, so
// a query for a project's speech calls would have found none regardless of how many were made.
func (a *OpenAIAudioAdapter) audit(ctx context.Context, jobID, model string, config provider.Config, status string, httpStatus int, started time.Time, callErr error, requestID string) {
	if a == nil || a.registry == nil || a.registry.audit == nil {
		return
	}
	record := provider.RequestRecord{
		ID:         newRecordID(),
		JobID:      jobID,
		ProviderID: config.ID,
		Capability: provider.CapabilityAudio,
		Model:      model,
		Status:     status,
		HTTPStatus: httpStatus,
		LatencyMS:  time.Since(started).Milliseconds(),
		RequestID:  requestID,
		CreatedAt:  time.Now().UTC(),
	}
	if callErr != nil {
		if providerErr, ok := provider.AsProviderError(callErr); ok {
			record.ErrorCode = string(providerErr.Category)
		} else {
			record.ErrorCode = string(provider.CategoryNetwork)
		}
	}
	_ = a.registry.audit.SaveRequestRecord(ctx, record)
}

// authorize resolves the secret and sets the header.
//
// It is a method on THIS adapter rather than a shared function because the other adapters' are on their
// own receivers, and the alternative — a free function taking the registry — would let a future adapter
// skip it. Three lines repeated where a reader can see them is cheaper than a seam that can be missed.
func (a *OpenAIAudioAdapter) authorize(ctx context.Context, request *http.Request, config provider.Config) error {
	if a.registry == nil || a.registry.secrets == nil {
		return provider.NewConfigurationError()
	}
	secret, err := a.registry.secrets.ResolveInternal(ctx, config.ID)
	if err != nil {
		return err
	}
	// The secret is zeroed as soon as the header is set, which is the same discipline the other adapters
	// keep: nothing below this line can read it, including an audit record.
	defer zeroBytes(secret)
	if len(secret) == 0 {
		return provider.NewConfigurationError()
	}
	request.Header.Set("Authorization", "Bearer "+string(secret))
	return nil
}
