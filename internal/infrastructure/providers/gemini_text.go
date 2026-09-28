package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// gemini_text.go implements providers.TextPort against the Gemini-native
// `generateContent` endpoint (RP-02.3).
//
// WHY A SEPARATE ADAPTER: routing a `gemini_compatible` kind to the
// OpenAI-compatible chat adapter (the previous wiring) sent Gemini traffic to
// `/chat/completions` with an `Authorization: Bearer` header — an endpoint
// shape and an authentication scheme the Gemini API does not serve. The
// image capability already had its own Gemini adapter (gemini_image.go,
// `x-goog-api-key` + `generateContent`); text now follows the same protocol,
// so a kind routes to ONE protocol family per capability and an unsupported
// combination is refused rather than guessed.
//
// PROTOCOL NOTES (fixture source: the shapes the offline tests in
// gemini_text_test.go pin; requests are POSTs under the configured base URL,
// path `/models/{model}:generateContent`, non-streaming, and
// `/models/{model}:streamGenerateContent?alt=sse`, streaming):
//   - auth: `x-goog-api-key` header, resolved in Go, zeroed after use —
//     the same secret handling the image adapter uses;
//   - request body: `{"contents":[{"role","parts":[{"text"}]}]}` with the
//     system prompt carried as a `systemInstruction` part;
//   - response body: `{"candidates":[{"content":{"parts":[{"text"}],
//     "finishReason"}]},"usageMetadata":{"promptTokenCount",
//     "candidatesTokenCount"}}`.
//
// The egress rules (guardedClient), the audit trail, the response size cap
// and the error taxonomy are the OpenAI path's, unchanged.

const (
	geminiGeneratePath      = "/models/%s:generateContent"
	geminiStreamGenerateAlt = "/models/%s:streamGenerateContent?alt=sse"
	// maxTextResponseBytes bounds one non-streaming reply body. The OpenAI
	// text path uses the same cap (1 MiB); a larger reply is a
	// response-invalid, not an unbounded read.
	maxTextResponseBytes = 1 << 20
)

// GeminiTextAdapter implements providers.TextPort against a Gemini-compatible
// generateContent endpoint.
type GeminiTextAdapter struct {
	registry *Registry
	// clientFactory allows tests to inject a controlled client; production
	// always builds the SSRF-guarded client via guardedClient.
	clientFactory func(config provider.Config) (*phttp.Client, error)
}

// NewGeminiTextAdapter builds the adapter over the registry's ports.
func NewGeminiTextAdapter(registry *Registry) *GeminiTextAdapter {
	return &GeminiTextAdapter{registry: registry}
}

func (a *GeminiTextAdapter) clientFor(config provider.Config) (*phttp.Client, error) {
	if a.clientFactory != nil {
		return a.clientFactory(config)
	}
	return guardedClient(config)
}

// Generate performs a non-streaming generateContent call.
func (a *GeminiTextAdapter) Generate(ctx context.Context, request providers.TextRequest) (providers.TextResult, error) {
	config, err := a.registry.configs.GetConfig(ctx, request.ProviderID)
	if err != nil {
		return providers.TextResult{}, err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return providers.TextResult{}, err
	}
	body, err := json.Marshal(buildGeminiTextBody(request, false))
	if err != nil {
		return providers.TextResult{}, provider.NewInvalidInputError()
	}
	endpoint, err := endpointFor(config, fmt.Sprintf(geminiGeneratePath, request.Model))
	if err != nil {
		return providers.TextResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return providers.TextResult{}, provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, 0, started, err, "")
		return providers.TextResult{}, err
	}

	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, request, config, statusForError(err), 0, started, err, "")
		return providers.TextResult{}, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)

	if response.StatusCode != http.StatusOK {
		mapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, mapped, requestID)
		return providers.TextResult{}, mapped
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxTextResponseBytes))
	if err != nil {
		wrapped := provider.NewResponseInvalidError()
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID)
		return providers.TextResult{}, wrapped
	}
	result, err := parseGeminiTextPayload(payload, request.Model)
	if err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, err, requestID)
		return providers.TextResult{}, err
	}
	a.auditWithUsage(ctx, request, config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID, parseGeminiUsage(payload))
	return result, nil
}

// Stream performs a streaming generateContent call (`alt=sse`) and forwards
// text deltas to the sink.
func (a *GeminiTextAdapter) Stream(ctx context.Context, request providers.TextRequest, sink providers.EventSink) error {
	if sink == nil {
		return provider.NewInvalidInputError()
	}
	config, err := a.registry.configs.GetConfig(ctx, request.ProviderID)
	if err != nil {
		return err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return err
	}
	body, err := json.Marshal(buildGeminiTextBody(request, true))
	if err != nil {
		return provider.NewInvalidInputError()
	}
	endpoint, err := endpointFor(config, fmt.Sprintf(geminiStreamGenerateAlt, request.Model))
	if err != nil {
		return err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, 0, started, err, "")
		return err
	}

	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, request, config, statusForError(err), 0, started, err, "")
		sink.OnError(err)
		return err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)

	if response.StatusCode != http.StatusOK {
		mapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, mapped, requestID)
		sink.OnError(mapped)
		return mapped
	}

	result, err := a.streamEvents(ctx, response.Body, sink)
	if err != nil {
		status := provider.StatusFailed
		if provider.IsCancellation(err) {
			status = provider.StatusCancelled
		}
		a.audit(ctx, request, config, status, response.StatusCode, started, err, requestID)
		sink.OnError(err)
		return err
	}
	a.audit(ctx, request, config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
	sink.OnDone(result)
	return nil
}

// streamEvents parses the SSE stream of generateContent chunks, forwarding
// each part's text as a delta. It returns the final result or a
// response-invalid/cancelled error.
func (a *GeminiTextAdapter) streamEvents(ctx context.Context, body io.Reader, sink providers.EventSink) (providers.TextResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxTextResponseBytes)
	var content strings.Builder
	sawChunk := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return providers.TextResult{}, provider.NewCancelledError()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return providers.TextResult{}, provider.NewResponseInvalidError()
		}
		if len(chunk.Candidates) == 0 {
			continue
		}
		sawChunk = true
		for _, part := range chunk.Candidates[0].Content.Parts {
			if part.Text != "" {
				content.WriteString(part.Text)
				sink.OnDelta(part.Text)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return providers.TextResult{}, provider.NewCancelledError()
		}
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	if !sawChunk {
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	return providers.TextResult{Content: content.String()}, nil
}

// buildGeminiTextBody maps the port's messages onto the generateContent
// shape: the system message becomes `systemInstruction`, everything else a
// `contents` entry with role `user`/`model` (Gemini's own role vocabulary).
func buildGeminiTextBody(request providers.TextRequest, stream bool) map[string]any {
	contents := make([]map[string]any, 0, len(request.Messages))
	var systemParts []map[string]string
	for _, message := range request.Messages {
		switch message.Role {
		case "system":
			systemParts = append(systemParts, map[string]string{"text": message.Content})
			continue
		case "assistant":
			contents = append(contents, map[string]any{
				"role": "model", "parts": []map[string]string{{"text": message.Content}},
			})
		default:
			contents = append(contents, map[string]any{
				"role": "user", "parts": []map[string]string{{"text": message.Content}},
			})
		}
	}
	body := map[string]any{"contents": contents}
	if len(systemParts) > 0 {
		body["systemInstruction"] = map[string]any{"parts": systemParts}
	}
	_ = stream
	return body
}

// parseGeminiTextPayload extracts the first candidate's text.
func parseGeminiTextPayload(payload []byte, model string) (providers.TextResult, error) {
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	if response.Error != nil {
		// A Gemini error body with a 200 envelope is still a failure.
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	if len(response.Candidates) == 0 {
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	var content strings.Builder
	for _, part := range response.Candidates[0].Content.Parts {
		content.WriteString(part.Text)
	}
	if content.Len() == 0 {
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	return providers.TextResult{Content: content.String(), Model: model}, nil
}

// parseGeminiUsage extracts the Gemini usageMetadata block when present.
// A missing or malformed block yields zero units rather than failing the
// call: unit accounting is optional metadata, not a correctness requirement.
func parseGeminiUsage(payload []byte) usageUnits {
	var response struct {
		UsageMetadata struct {
			PromptTokenCount     int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return usageUnits{}
	}
	return usageUnits{promptTokens: response.UsageMetadata.PromptTokenCount, completionTokens: response.UsageMetadata.CandidatesTokenCount}
}

// authorize resolves the secret and sets the Gemini header. The byte slice
// is zeroed after use, exactly like the image adapter's.
func (a *GeminiTextAdapter) authorize(ctx context.Context, request *http.Request, config provider.Config) error {
	if a.registry == nil || a.registry.secrets == nil {
		return provider.NewConfigurationError()
	}
	secret, err := a.registry.secrets.ResolveInternal(ctx, config.ID)
	if err != nil {
		return err
	}
	defer zeroBytes(secret)
	if len(secret) == 0 {
		return provider.NewConfigurationError()
	}
	request.Header.Set("x-goog-api-key", string(secret))
	return nil
}

// audit records a redacted request record. It never logs headers or payloads.
func (a *GeminiTextAdapter) audit(ctx context.Context, request providers.TextRequest, config provider.Config, status string, httpStatus int, started time.Time, callErr error, requestID string) {
	a.auditWithUsage(ctx, request, config, status, httpStatus, started, callErr, requestID, usageUnits{})
}

func (a *GeminiTextAdapter) auditWithUsage(ctx context.Context, request providers.TextRequest, config provider.Config, status string, httpStatus int, started time.Time, callErr error, requestID string, usage usageUnits) {
	if a.registry == nil || a.registry.audit == nil {
		return
	}
	record := provider.RequestRecord{
		ID:          newRecordID(),
		ProviderID:  config.ID,
		Capability:  provider.CapabilityText,
		Model:       request.Model,
		Status:      status,
		HTTPStatus:  httpStatus,
		LatencyMS:   time.Since(started).Milliseconds(),
		RequestID:   requestID,
		InputUnits:  usage.promptTokens,
		OutputUnits: usage.completionTokens,
		CreatedAt:   time.Now().UTC(),
	}
	if callErr != nil {
		if providerErr, ok := provider.AsProviderError(callErr); ok {
			record.ErrorCode = string(providerErr.Category)
		} else {
			record.ErrorCode = string(provider.CategoryNetwork)
		}
	}
	// Audit failures must not break the call path or leak errors to callers.
	_ = a.registry.audit.SaveRequestRecord(ctx, record)
}
