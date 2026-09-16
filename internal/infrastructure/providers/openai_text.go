package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

const (
	chatCompletionsPath     = "/chat/completions"
	modelsPath              = "/models"
	providerRequestIDHeader = "X-Request-Id"
)

// OpenAITextAdapter implements providers.TextPort against an
// OpenAI-compatible chat completions endpoint.
type OpenAITextAdapter struct {
	registry *Registry
	// clientFactory allows tests to inject a controlled client; production
	// always builds the SSRF-guarded client via guardedClient.
	clientFactory func(config provider.Config) (*phttp.Client, error)
}

// NewOpenAITextAdapter builds the adapter over the registry's ports.
func NewOpenAITextAdapter(registry *Registry) *OpenAITextAdapter {
	return &OpenAITextAdapter{registry: registry}
}

func (a *OpenAITextAdapter) clientFor(config provider.Config) (*phttp.Client, error) {
	if a.clientFactory != nil {
		return a.clientFactory(config)
	}
	return guardedClient(config)
}

// Generate performs a non-streaming chat completion.
func (a *OpenAITextAdapter) Generate(ctx context.Context, request providers.TextRequest) (providers.TextResult, error) {
	config, err := a.registry.configs.GetConfig(ctx, request.ProviderID)
	if err != nil {
		return providers.TextResult{}, err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return providers.TextResult{}, err
	}
	body, err := json.Marshal(a.buildRequestBody(request, false))
	if err != nil {
		return providers.TextResult{}, provider.NewInvalidInputError()
	}
	endpoint, err := a.endpoint(config, chatCompletionsPath)
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
		err := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, err, requestID)
		return providers.TextResult{}, err
	}

	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		wrapped := provider.NewResponseInvalidError()
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID)
		return providers.TextResult{}, wrapped
	}
	result, err := parseCompletion(payload, request.Model)
	if err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, err, requestID)
		return providers.TextResult{}, err
	}
	a.auditWithUsage(ctx, request, config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID, parseUsage(payload))
	return result, nil
}

// Stream performs an SSE chat completion and forwards deltas to the sink.
func (a *OpenAITextAdapter) Stream(ctx context.Context, request providers.TextRequest, sink providers.EventSink) error {
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
	body, err := json.Marshal(a.buildRequestBody(request, true))
	if err != nil {
		return provider.NewInvalidInputError()
	}
	endpoint, err := a.endpoint(config, chatCompletionsPath)
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
		err := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, err, requestID)
		sink.OnError(err)
		return err
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

// streamEvents parses SSE lines, forwarding deltas. It returns the final
// result or a response-invalid/cancelled error.
func (a *OpenAITextAdapter) streamEvents(ctx context.Context, body io.Reader, sink providers.EventSink) (providers.TextResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var content strings.Builder
	sawDone := false
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
		if data == "[DONE]" {
			sawDone = true
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return providers.TextResult{}, provider.NewResponseInvalidError()
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if delta := chunk.Choices[0].Delta.Content; delta != "" {
			content.WriteString(delta)
			sink.OnDelta(delta)
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return providers.TextResult{}, provider.NewCancelledError()
		}
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	if !sawDone {
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	return providers.TextResult{Content: content.String()}, nil
}

func (a *OpenAITextAdapter) buildRequestBody(request providers.TextRequest, stream bool) map[string]any {
	messages := make([]map[string]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		messages = append(messages, map[string]string{"role": message.Role, "content": message.Content})
	}
	return map[string]any{
		"model":    request.Model,
		"messages": messages,
		"stream":   stream,
	}
}

// endpoint joins the configured base URL path with the API path.
func (a *OpenAITextAdapter) endpoint(config provider.Config, apiPath string) (string, error) {
	return endpointFor(config, apiPath)
}

// authorize resolves the secret and sets the Authorization header. The byte
// slice is zeroed after use to reduce its lifetime in memory.
func (a *OpenAITextAdapter) authorize(ctx context.Context, request *http.Request, config provider.Config) error {
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
	request.Header.Set("Authorization", "Bearer "+string(secret))
	return nil
}

// audit records a redacted request record. It never logs headers or payloads.
func (a *OpenAITextAdapter) audit(ctx context.Context, request providers.TextRequest, config provider.Config, status string, httpStatus int, started time.Time, callErr error, requestID string) {
	a.auditWithUsage(ctx, request, config, status, httpStatus, started, callErr, requestID, usageUnits{})
}

// usageUnits carries provider-reported token counts when the adapter can
// observe them. Zero means "not reported".
type usageUnits struct {
	promptTokens     int64
	completionTokens int64
}

func (a *OpenAITextAdapter) auditWithUsage(ctx context.Context, request providers.TextRequest, config provider.Config, status string, httpStatus int, started time.Time, callErr error, requestID string, usage usageUnits) {
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

// parseUsage extracts the OpenAI-compatible usage block when present. A
// missing or malformed block yields zero units rather than failing the call:
// unit accounting is optional metadata, not a correctness requirement.
func parseUsage(payload []byte) usageUnits {
	var response struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return usageUnits{}
	}
	return usageUnits{promptTokens: response.Usage.PromptTokens, completionTokens: response.Usage.CompletionTokens}
}

func statusForError(err error) string {
	if provider.IsCancellation(err) {
		return provider.StatusCancelled
	}
	return provider.StatusFailed
}

// mapHTTPStatus converts OpenAI-compatible HTTP failures to the taxonomy.
func mapHTTPStatus(status int, header http.Header) error {
	switch {
	case status == http.StatusUnauthorized:
		return provider.NewUnauthorizedError()
	case status == http.StatusForbidden:
		return provider.NewForbiddenError()
	case status == http.StatusTooManyRequests:
		return provider.NewRateLimitedError(parseRetryAfter(header.Get("Retry-After")))
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return provider.NewInvalidInputError()
	case status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout:
		return provider.NewRemoteTransientError()
	case status >= 500:
		return provider.NewRemoteTransientError()
	case status >= 400:
		return provider.NewRemotePermanentError()
	default:
		return provider.NewResponseInvalidError()
	}
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	seconds, err := time.ParseDuration(value + "s")
	if err != nil {
		return 0
	}
	if seconds < 0 {
		return 0
	}
	return seconds
}

// parseCompletion extracts the first choice's message content.
func parseCompletion(payload []byte, model string) (providers.TextResult, error) {
	var response struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	if len(response.Choices) == 0 {
		return providers.TextResult{}, provider.NewResponseInvalidError()
	}
	result := providers.TextResult{Content: response.Choices[0].Message.Content, Model: response.Model}
	if result.Model == "" {
		result.Model = model
	}
	return result, nil
}

// newRecordID builds a random audit record ID without shared mutable state.
func newRecordID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "req-unknown"
	}
	return "req-" + hex.EncodeToString(value[:])
}
