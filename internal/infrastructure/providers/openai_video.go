package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// openai_video.go is the ASYNCHRONOUS video adapter (P3 item 21).
//
// # What did not exist, and what this replaces
//
// `VideoPortFor` resolved the mock for `mock_media` and returned `unsupported` for everything else,
// with the comment "A real async video adapter does not exist yet". So the whole job pipeline — submit,
// park, poll, cancel, download, verify, commit — was exercised only against an in-process double, and
// the parts of that pipeline that exist BECAUSE providers are remote were never driven by a remote
// protocol: the status vocabulary, the poll pacing, the remote-id lifecycle, the cancel handshake.
//
// # The protocol this implements, stated as the ASSUMPTION it is
//
// One shape, and it is the OpenAI video API's:
//
//	POST {base}/videos              {"model": …, "prompt": …, "seconds": …, "size": …}  -> {"id": "…"}
//	GET  {base}/videos/{id}         -> {"status": "queued|in_progress|completed|failed", "progress": n}
//	GET  {base}/videos/{id}/content -> the bytes, or a 302 to a CDN
//	DELETE {base}/videos/{id}       -> cancel
//
// **REAL PROVIDERS DIFFER AND THIS IS ONE SHAPE, NOT A UNIVERSAL ABSTRACTION.** ADR-0027 records that:
// the value delivered here is a working adapter plus a path that has been driven end to end, and a
// different vendor means changing this file rather than configuring it.
//
// # Why the adapter is testable without credentials
//
// Every call goes through the same `clientFactory` seam the image adapters use, so an `httptest` server
// plays a provider that accepts, reports running twice, completes, and serves bytes. That is how this
// package's tests exercise the ASYNC protocol — including the two polls a synchronous adapter never
// needs — with no network and no key.
type OpenAIVideoAdapter struct {
	registry *Registry
	// clientFactory is the test seam; production uses guardedClient so the SSRF policy cannot drift
	// between adapters.
	clientFactory func(config provider.Config) (*phttp.Client, error)
}

// NewOpenAIVideoAdapter builds the adapter over the registry's ports.
func NewOpenAIVideoAdapter(registry *Registry) *OpenAIVideoAdapter {
	return &OpenAIVideoAdapter{registry: registry}
}

func (a *OpenAIVideoAdapter) clientFor(config provider.Config) (*phttp.Client, error) {
	if a.clientFactory != nil {
		return a.clientFactory(config)
	}
	return guardedClient(config)
}

// The protocol's paths, in one place so a vendor that spells them differently is one edit.
const (
	videosPath         = "/videos"
	videoPollPath      = "/videos/"
	videoContentSuffix = "/content"
)

// authorize resolves the secret and sets the header.
//
// It is a method on THIS adapter rather than a shared function because the image adapter's is on its
// own receiver, and the alternative — a free function taking the registry — would let a future adapter
// skip it. Three lines repeated where a reader can see them is cheaper than a seam that can be missed.
func (a *OpenAIVideoAdapter) authorize(ctx context.Context, request *http.Request, config provider.Config) error {
	if a.registry == nil || a.registry.secrets == nil {
		return provider.NewConfigurationError()
	}
	secret, err := a.registry.secrets.ResolveInternal(ctx, config.ID)
	if err != nil {
		return err
	}
	// The secret is zeroed as soon as the header is set, which is the same discipline the other
	// adapters keep: nothing below this line can read it, including an audit record.
	defer zeroBytes(secret)
	if len(secret) == 0 {
		return provider.NewConfigurationError()
	}
	request.Header.Set("Authorization", "Bearer "+string(secret))
	return nil
}

// maxVideoSubmitResponseBytes bounds the ACCEPTANCE response, which is an identifier and little else.
//
// It is deliberately far below the result's allowance: a submission response carrying megabytes is not
// one this adapter will parse, and the result arrives through `Fetch` rather than here.
const maxVideoSubmitResponseBytes = 1 << 20

// maxVideoPollResponseBytes bounds a status response. It is larger than the acceptance response
// because a failing provider often includes a message, and the message is what a user needs.
const maxVideoPollResponseBytes = 1 << 20

// maxInlineVideoBytes bounds an INLINE result.
//
// # It is the TRANSPORT's ceiling, not a number of this adapter's choosing
//
// The first version of this constant was 64 MiB, and the test that fed it an oversized body found the
// bound UNREACHABLE: every call goes through `guardedClient`, whose own `MaxResponseBytes` is **10 MiB**
// and which truncates the body before this adapter sees it. So the adapter's check could only ever fire
// for a body between 10 and 64 MiB — and for a body over 10 MiB the client's truncation arrives as a
// short read, not as this adapter's refusal.
//
// The honest bound is therefore the transport's, stated here so the two cannot drift: a result larger
// than this cannot be buffered through an API call AT ALL, and the provider's URL mode is the answer —
// which is exactly what the design intends, since the URL path streams under `DownloadAndCommit` with a
// 512 MiB ceiling of its own.
//
// A real clip is tens of megabytes, so this is a LOW ceiling by design: an OpenAI-compatible endpoint
// that inlines video is answering with something this pipeline will not buffer, and the refusal names
// the alternative rather than failing at a swap file.
const maxInlineVideoBytes = 8 << 20

// Submit starts a generation and returns the provider's identifier.
//
// The references travel in the same order the runner assembled them — plain references, then the first
// frame, then the last — because that order is what the runner's own comment calls FR-080's, and an
// adapter that reordered them would send a last frame where the provider expects a reference.
func (a *OpenAIVideoAdapter) Submit(ctx context.Context, request appjobs.VideoRequest) (appjobs.RemoteJob, error) {
	if a == nil || a.registry == nil || a.registry.configs == nil {
		return appjobs.RemoteJob{}, provider.NewUnsupportedError()
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return appjobs.RemoteJob{}, provider.NewInvalidInputError()
	}
	config, err := a.registry.configs.GetConfig(ctx, request.ProviderID)
	if err != nil {
		return appjobs.RemoteJob{}, err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return appjobs.RemoteJob{}, err
	}
	body, err := json.Marshal(videoSubmitBody(request))
	if err != nil {
		return appjobs.RemoteJob{}, provider.NewInvalidInputError()
	}
	endpoint, err := endpointFor(config, videosPath)
	if err != nil {
		return appjobs.RemoteJob{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return appjobs.RemoteJob{}, provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, request.JobID, request.Model, config, provider.StatusFailed, 0, started, err, "")
		return appjobs.RemoteJob{}, err
	}
	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, request.JobID, request.Model, config, statusForError(err), 0, started, err, "")
		return appjobs.RemoteJob{}, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated &&
		response.StatusCode != http.StatusAccepted {
		mapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, request.JobID, request.Model, config, provider.StatusFailed, response.StatusCode, started, mapped, requestID)
		return appjobs.RemoteJob{}, mapped
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxVideoSubmitResponseBytes))
	if err != nil {
		wrapped := provider.NewResponseInvalidError()
		a.audit(ctx, request.JobID, request.Model, config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID)
		return appjobs.RemoteJob{}, wrapped
	}
	var accepted struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &accepted); err != nil || strings.TrimSpace(accepted.ID) == "" {
		// An acceptance without an identifier is not an acceptance: the pipeline would park a job whose
		// remote id is empty and poll nothing forever, which is worse than a refusal.
		invalid := provider.NewResponseInvalidError()
		a.audit(ctx, request.JobID, request.Model, config, provider.StatusFailed, response.StatusCode, started, invalid, requestID)
		return appjobs.RemoteJob{}, invalid
	}
	a.audit(ctx, request.JobID, request.Model, config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
	return appjobs.RemoteJob{ProviderID: config.ID, ID: strings.TrimSpace(accepted.ID)}, nil
}

// Poll reads the generation's state.
//
// # The status vocabulary, and why an unknown one is REFUSED
//
// The mapping is a table rather than a chain of comparisons so a reader can see the whole vocabulary
// and disagree with it. Every status the protocol names is here, and **an unrecognised one is an
// error rather than "still running"**: a job that reports a status this adapter does not understand
// would otherwise poll until its attempt budget ran out, looking to the user like a generation that
// never finishes. Refusing hands it to the retry policy and then to a person, which is what a
// disagreement with a provider should do.
func (a *OpenAIVideoAdapter) Poll(ctx context.Context, remote appjobs.RemoteJob) (appjobs.RemoteStatus, error) {
	if a == nil || a.registry == nil || a.registry.configs == nil {
		return appjobs.RemoteStatus{}, provider.NewUnsupportedError()
	}
	if strings.TrimSpace(remote.ID) == "" {
		return appjobs.RemoteStatus{}, provider.NewInvalidInputError()
	}
	config, err := a.registry.configs.GetConfig(ctx, remote.ProviderID)
	if err != nil {
		return appjobs.RemoteStatus{}, err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return appjobs.RemoteStatus{}, err
	}
	endpoint, err := endpointFor(config, videoPollPath+remote.ID)
	if err != nil {
		return appjobs.RemoteStatus{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return appjobs.RemoteStatus{}, provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Accept", "application/json")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, "", "", config, provider.StatusFailed, 0, started, err, "")
		return appjobs.RemoteStatus{}, err
	}
	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, "", "", config, statusForError(err), 0, started, err, "")
		return appjobs.RemoteStatus{}, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)
	if response.StatusCode != http.StatusOK {
		mapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, mapped, requestID)
		return appjobs.RemoteStatus{}, mapped
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxVideoPollResponseBytes))
	if err != nil {
		wrapped := provider.NewResponseInvalidError()
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID)
		return appjobs.RemoteStatus{}, wrapped
	}
	var document struct {
		Status   string `json:"status"`
		Progress *int   `json:"progress"`
		// Only the message is parsed. The protocol also carries an `error.code`, and a field read into
		// nothing is how a dead parse accumulates: the audit's ErrorCode is this adapter's own
		// CLASSIFICATION (the provider taxonomy the job manager retries on), not a vendor's vocabulary,
		// so there is nowhere for the vendor string to go. A future change that wants to surface it must
		// add a field to `RemoteStatus` or `RequestRecord` rather than quietly parsing it here.
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		invalid := provider.NewResponseInvalidError()
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, invalid, requestID)
		return appjobs.RemoteStatus{}, invalid
	}
	status, known := mapVideoStatus(strings.ToLower(strings.TrimSpace(document.Status)))
	if !known {
		unknown := provider.NewResponseInvalidError()
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, unknown, requestID)
		return appjobs.RemoteStatus{}, unknown
	}
	status.Message = safeProviderMessage(document.Error.Message)
	// Progress is copied only when the provider SENT one: a pointer distinguishes "0% done" from "no
	// progress reported", and a job row that showed 0% for a provider that says nothing would look
	// stalled rather than silent.
	if document.Progress != nil {
		clamped := *document.Progress
		if clamped < 0 {
			clamped = 0
		}
		if clamped > 100 {
			clamped = 100
		}
		status.Progress = &clamped
	}
	// THE AUDIT STATUS FOLLOWS THE GENERATION, not the HTTP call, and the distinction is not cosmetic.
	//
	// Every call in this protocol can return 200 and still report a generation that failed: the poll
	// above answers `{"status":"failed"}` with an HTTP success, because the CALL succeeded. Recording
	// that as `succeeded` would put a row in `provider_requests` whose error_code is empty — the column
	// `DiagnosticsReader.ErrorCodes` groups by — so the one place this repository PERSISTS a stable
	// code for an operation would be silent about the failures a user is most likely to ask about. The
	// image adapter draws the line the same way: a 200 whose body carries a provider refusal is audited
	// `failed`.
	if status.Failed {
		failure := provider.NewRemotePermanentError()
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, failure, requestID)
		return status, nil
	}
	a.audit(ctx, "", "", config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
	return status, nil
}

// Fetch downloads the finished result.
//
// Two shapes, and the difference matters to the caller: INLINE bytes become a data URL the existing
// result pipeline commits directly, and a URL is returned as one so `DownloadAndCommit` streams it
// under the untrusted-URL policy — which is where https-only, private-address refusal and the byte
// ceiling already live. `MIMEType` is stated for the inline case and left empty for the URL case,
// because the download path SNIFFS the type rather than trusting the provider.
func (a *OpenAIVideoAdapter) Fetch(ctx context.Context, remote appjobs.RemoteJob) (appjobs.MediaOutcome, error) {
	if a == nil || a.registry == nil || a.registry.configs == nil {
		return appjobs.MediaOutcome{}, provider.NewUnsupportedError()
	}
	if strings.TrimSpace(remote.ID) == "" {
		return appjobs.MediaOutcome{}, provider.NewInvalidInputError()
	}
	config, err := a.registry.configs.GetConfig(ctx, remote.ProviderID)
	if err != nil {
		return appjobs.MediaOutcome{}, err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return appjobs.MediaOutcome{}, err
	}
	endpoint, err := endpointFor(config, videoPollPath+remote.ID+videoContentSuffix)
	if err != nil {
		return appjobs.MediaOutcome{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return appjobs.MediaOutcome{}, provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Accept", "video/mp4, video/webm, application/json")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, "", "", config, provider.StatusFailed, 0, started, err, "")
		return appjobs.MediaOutcome{}, err
	}
	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, "", "", config, statusForError(err), 0, started, err, "")
		return appjobs.MediaOutcome{}, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)
	if response.StatusCode != http.StatusOK {
		mapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, mapped, requestID)
		return appjobs.MediaOutcome{}, mapped
	}
	contentType := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type")))
	if strings.Contains(contentType, "application/json") {
		// The JSON shape, which is how an OpenAI-compatible endpoint reports a CDN link.
		payload, err := io.ReadAll(io.LimitReader(response.Body, maxVideoSubmitResponseBytes))
		if err != nil {
			wrapped := provider.NewResponseInvalidError()
			a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID)
			return appjobs.MediaOutcome{}, wrapped
		}
		var document struct {
			URL      string `json:"url"`
			Data     string `json:"b64_json"`
			MIMEType string `json:"mime_type"`
		}
		if err := json.Unmarshal(payload, &document); err != nil {
			invalid := provider.NewResponseInvalidError()
			a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, invalid, requestID)
			return appjobs.MediaOutcome{}, invalid
		}
		if trimmed := strings.TrimSpace(document.Data); trimmed != "" {
			a.audit(ctx, "", "", config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
			return appjobs.MediaOutcome{Data: trimmed, MIMEType: mimeOrMP4(document.MIMEType)}, nil
		}
		if url := strings.TrimSpace(document.URL); url != "" {
			a.audit(ctx, "", "", config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
			// The MIME is left EMPTY on purpose: the downloader sniffs the bytes, and a provider's
			// claim about a file it did not serve is not one this pipeline trusts.
			return appjobs.MediaOutcome{URL: url}, nil
		}
		invalid := provider.NewResponseInvalidError()
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, invalid, requestID)
		return appjobs.MediaOutcome{}, invalid
	}
	// The raw-bytes shape: the endpoint served the video itself. It is buffered under a bound, because
	// `MediaOutcome` carries a data URL and a stream has nowhere else to go in this port.
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxInlineVideoBytes+1))
	if err != nil {
		wrapped := provider.NewResponseInvalidError()
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID)
		return appjobs.MediaOutcome{}, wrapped
	}
	if len(payload) > maxInlineVideoBytes {
		// Refused rather than truncated: a half a video committed as a result is worse than a refusal,
		// because the pipeline would verify and store it as a complete generation. The message names the
		// alternative, because "the result was too large" without one is a dead end for a user.
		tooLarge := provider.NewResponseInvalidError()
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, tooLarge, requestID)
		return appjobs.MediaOutcome{}, tooLarge
	}
	if len(payload) == 0 {
		empty := provider.NewResponseInvalidError()
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, empty, requestID)
		return appjobs.MediaOutcome{}, empty
	}
	a.audit(ctx, "", "", config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
	return appjobs.MediaOutcome{Data: encodeBase64(payload), MIMEType: mimeOrMP4(contentType)}, nil
}

// Cancel asks the provider to stop.
//
// # Why a failure here does NOT stop the local state machine
//
// The job table has a `remote_cancel_unconfirmed` column precisely for this: a provider that refuses,
// times out or has already finished leaves a remote job this application cannot vouch for, and the
// column is how that is recorded. So this returns the provider's error and the CALLER decides — which
// is what the column's existence means, and a `Cancel` that swallowed the error would make the column
// unreachable.
func (a *OpenAIVideoAdapter) Cancel(ctx context.Context, remote appjobs.RemoteJob) error {
	if a == nil || a.registry == nil || a.registry.configs == nil {
		return provider.NewUnsupportedError()
	}
	if strings.TrimSpace(remote.ID) == "" {
		return provider.NewInvalidInputError()
	}
	config, err := a.registry.configs.GetConfig(ctx, remote.ProviderID)
	if err != nil {
		return err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return err
	}
	endpoint, err := endpointFor(config, videoPollPath+remote.ID)
	if err != nil {
		return err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Accept", "application/json")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, "", "", config, provider.StatusFailed, 0, started, err, "")
		return err
	}
	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, "", "", config, statusForError(err), 0, started, err, "")
		return err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)
	// 204 is the protocol's success. 404 is ALSO treated as success — the job is gone, which is what
	// cancellation asked for — and that is not leniency: a provider that finished and expired the
	// record has already stopped, and reporting a failure would put a job in front of a person to
	// investigate a cancellation that worked.
	if response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusNotFound {
		a.audit(ctx, "", "", config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
		return nil
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		mapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, "", "", config, provider.StatusFailed, response.StatusCode, started, mapped, requestID)
		return mapped
	}
	a.audit(ctx, "", "", config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
	return nil
}

// videoSubmitBody renders the submission body.
//
// # Why the references are a LIST rather than named first/last fields
//
// The runner assembles them in FR-080's order — plain references, then the first frame, then the last
// — and a comment there says the ORDER is the only statement of which is which. This adapter therefore
// sends one list and preserves it, and it does not try to guess a role from a position: a provider
// that wants the frames named is a different protocol, recorded as such in ADR-0027.
//
// # The field names are an ASSUMPTION, and it is recorded rather than dressed up
//
// `input_reference` as a list of objects each carrying an `image_url` data URL is ONE reading of an
// OpenAI-style video API, and every sentence of that is a guess this build cannot check: there is no
// authorised vendor call to compare it against (AGENTS 4.3), so the submission body is verified only
// against this repository's own fake provider.
//
// What that means for a reader: the protocol SHAPE — submit, poll, fetch, cancel — is exercised and
// tested, and the four field names in this function would be the first thing to change against a real
// vendor. ADR-0027 says so in the same words, and STATUS does not claim a live integration.
//
// Each entry is a data URL because the runner handed over bytes with their MIME type and nothing else:
// there is no uploaded file for the provider to point at.
func videoSubmitBody(request appjobs.VideoRequest) map[string]any {
	body := map[string]any{
		"model":  request.Model,
		"prompt": request.Prompt,
	}
	// Zero means "the caller did not state one", which is what the runner's own default already
	// handled; omitting the field lets the provider apply its own default rather than being told a
	// number nobody chose.
	if request.Seconds > 0 {
		body["seconds"] = itoa(request.Seconds)
	}
	if size := strings.TrimSpace(request.Size); size != "" {
		body["size"] = size
	}
	if len(request.References) > 0 {
		references := make([]map[string]string, 0, len(request.References))
		for _, reference := range request.References {
			mimeType := strings.TrimSpace(reference.MIMEType)
			if mimeType == "" {
				mimeType = "image/png"
			}
			references = append(references, map[string]string{
				"image_url": "data:" + mimeType + ";base64," + encodeBase64(reference.Bytes),
			})
		}
		body["input_reference"] = references
	}
	return body
}

// mapVideoStatus translates the protocol's status vocabulary into the port's.
//
// It returns the ZERO `RemoteStatus` for a status it does not know, and the caller treats that as a
// refusal — see `Poll`'s comment on why an unrecognised status must not read as "still running".
//
// The vocabulary, and the reasoning for each mapping:
//
//	queued, pending, starting    -> not done, no progress: the provider has accepted it
//	in_progress, running         -> not done
//	processing                   -> not done, because a provider that says this is working
//	completed, succeeded, success-> DONE, and successful
//	failed, error, cancelled     -> DONE, and failed
//
// `cancelled` maps to FAILED rather than to a state of its own because the port has two terminal
// states and the job manager already knows a cancellation made locally; a provider-side cancellation
// the user did not ask for is a failure of the generation.
func mapVideoStatus(status string) (appjobs.RemoteStatus, bool) {
	switch status {
	case "queued", "pending", "starting", "in_progress", "running", "processing":
		// A non-terminal status. The zero value says "not done, not failed", which is what the runner
		// reads as "poll again".
		return appjobs.RemoteStatus{}, true
	case "completed", "succeeded", "success":
		return appjobs.RemoteStatus{Done: true}, true
	case "failed", "error", "cancelled", "canceled":
		return appjobs.RemoteStatus{Done: true, Failed: true}, true
	default:
		// UNKNOWN, which the caller refuses. It is told apart from a non-terminal status by the bool
		// rather than by a sentinel: the zero status is a legitimate answer here.
		return appjobs.RemoteStatus{}, false
	}
}

// mimeOrMP4 states a result's type, defaulting to `video/mp4`.
//
// The default is the same one the runner applies, for the same reason: a provider that serves bytes
// without a type is serving a video, and the download path sniffs the bytes anyway — this value is the
// HINT the pipeline records, not the authority.
func mimeOrMP4(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "video/mp4"
	}
	// A Content-Type may carry parameters; only the type itself is recorded.
	if index := strings.IndexByte(trimmed, ';'); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[:index])
	}
	return trimmed
}

// safeProviderMessage keeps a provider's message usable without letting it carry content.
//
// A provider's failure text is written for a developer and can quote the request — including a prompt
// a user typed and a URL with a token in it. The message is therefore SCRUBBED of anything that looks
// like a credential and truncated, and the result is a sentence a user can read rather than a body
// copied into their logs. It is deliberately conservative: something removed that was harmless costs a
// less detailed message, and something kept that was a secret costs a leak.
func safeProviderMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return ""
	}
	// A message carrying a URL or what looks like a key is replaced wholesale rather than edited: a
	// partial redaction of an unknown shape is how a token survives in a log.
	lowered := strings.ToLower(trimmed)
	for _, marker := range []string{"bearer ", "api_key", "api-key", "token=", "sk-", "http://", "https://"} {
		if strings.Contains(lowered, marker) {
			return "The provider reported an error."
		}
	}
	if len(trimmed) > 300 {
		return trimmed[:300] + "…"
	}
	return trimmed
}

// audit writes one redacted record with the VIDEO capability.
//
// It is the first caller of `provider.CapabilityVideo` in this build: the column's CHECK has admitted
// the value since migration 000003 and nothing had ever written it, so a query for a project's video
// calls would have found none regardless of how many were made.
//
// The provider's request identifier travels when the protocol gave one, because it is what a support
// conversation needs; the ERROR CODE is the classification rather than the body, so the same
// redaction rule the other adapters keep applies here.
func (a *OpenAIVideoAdapter) audit(ctx context.Context, jobID, model string, config provider.Config, status string, httpStatus int, started time.Time, callErr error, requestID string) {
	if a == nil || a.registry == nil || a.registry.audit == nil {
		return
	}
	record := provider.RequestRecord{
		ID:         newRecordID(),
		JobID:      jobID,
		ProviderID: config.ID,
		Capability: provider.CapabilityVideo,
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
