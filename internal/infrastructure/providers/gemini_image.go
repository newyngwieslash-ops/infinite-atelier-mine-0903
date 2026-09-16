package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// GeminiImageAdapter implements the image capability against a
// Gemini-compatible generateContent endpoint.
//
// Gemini uses a different authentication header (x-goog-api-key) and a
// different request/response shape, but the egress rules, audit trail, and
// error taxonomy are identical to the OpenAI-compatible adapter.
type GeminiImageAdapter struct {
	registry      *Registry
	clientFactory func(config provider.Config) (*phttp.Client, error)
}

// NewGeminiImageAdapter builds the adapter over the registry's ports.
func NewGeminiImageAdapter(registry *Registry) *GeminiImageAdapter {
	return &GeminiImageAdapter{registry: registry}
}

func (a *GeminiImageAdapter) clientFor(config provider.Config) (*phttp.Client, error) {
	if a.clientFactory != nil {
		return a.clientFactory(config)
	}
	return guardedClient(config)
}

// Generate posts a generateContent request and returns inline image parts or
// provider-hosted URIs.
func (a *GeminiImageAdapter) Generate(ctx context.Context, request appjobs.ImageRequest) (appjobs.ImageOutcome, error) {
	if a == nil || a.registry == nil || a.registry.configs == nil {
		return appjobs.ImageOutcome{}, provider.NewUnsupportedError()
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return appjobs.ImageOutcome{}, provider.NewInvalidInputError()
	}
	config, err := a.registry.configs.GetConfig(ctx, request.ProviderID)
	if err != nil {
		return appjobs.ImageOutcome{}, err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return appjobs.ImageOutcome{}, err
	}
	body, err := buildGeminiBody(request)
	if err != nil {
		return appjobs.ImageOutcome{}, err
	}
	path := "/models/" + request.Model + ":generateContent"
	endpoint, err := endpointFor(config, path)
	if err != nil {
		return appjobs.ImageOutcome{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return appjobs.ImageOutcome{}, provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, 0, started, err, "")
		return appjobs.ImageOutcome{}, err
	}

	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, request, config, statusForError(err), 0, started, err, "")
		return appjobs.ImageOutcome{}, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)
	if response.StatusCode != http.StatusOK {
		mapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, mapped, requestID)
		return appjobs.ImageOutcome{}, mapped
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxImageResponseBytes))
	if err != nil {
		wrapped := provider.NewResponseInvalidError()
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID)
		return appjobs.ImageOutcome{}, wrapped
	}
	outcome, err := parseGeminiImagePayload(payload)
	if err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, err, requestID)
		return appjobs.ImageOutcome{}, err
	}
	a.audit(ctx, request, config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
	return outcome, nil
}

func buildGeminiBody(request appjobs.ImageRequest) ([]byte, error) {
	parts := []map[string]any{{"text": request.Prompt}}
	for _, reference := range request.References {
		payload := reference.Bytes
		mimeType := reference.MIMEType
		if len(payload) == 0 && reference.Data != "" {
			decoded, declared, err := decodeImageData(reference.Data)
			if err != nil {
				return nil, err
			}
			payload = decoded
			if mimeType == "" {
				mimeType = declared
			}
		}
		if len(payload) == 0 {
			return nil, provider.NewInvalidInputError()
		}
		if mimeType == "" {
			mimeType = "image/png"
		}
		parts = append(parts, map[string]any{
			"inline_data": map[string]any{
				"mime_type": mimeType,
				"data":      base64.StdEncoding.EncodeToString(payload),
			},
		})
	}
	generationConfig := map[string]any{"responseModalities": []string{"IMAGE"}}
	if request.Count > 1 {
		generationConfig["candidateCount"] = request.Count
	}
	body := map[string]any{
		"contents":         []map[string]any{{"role": "user", "parts": parts}},
		"generationConfig": generationConfig,
	}
	return json.Marshal(body)
}

func parseGeminiImagePayload(payload []byte) (appjobs.ImageOutcome, error) {
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mime_type"`
						Data     string `json:"data"`
					} `json:"inline_data"`
					InlineDataCamel *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
					FileData *struct {
						FileURI      string `json:"file_uri"`
						FileURICamel string `json:"fileUri"`
					} `json:"file_data"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return appjobs.ImageOutcome{}, provider.NewResponseInvalidError()
	}
	if response.Error != nil {
		return appjobs.ImageOutcome{}, provider.NewInvalidInputError()
	}
	outcome := appjobs.ImageOutcome{}
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			switch {
			case part.InlineData != nil && part.InlineData.Data != "":
				outcome.Results = append(outcome.Results, appjobs.ImageResult{
					Data:     part.InlineData.Data,
					MIMEType: part.InlineData.MimeType,
				})
			case part.InlineDataCamel != nil && part.InlineDataCamel.Data != "":
				outcome.Results = append(outcome.Results, appjobs.ImageResult{
					Data:     part.InlineDataCamel.Data,
					MIMEType: part.InlineDataCamel.MimeType,
				})
			case part.FileData != nil:
				uri := part.FileData.FileURI
				if uri == "" {
					uri = part.FileData.FileURICamel
				}
				if uri != "" {
					outcome.RemoteURLs = append(outcome.RemoteURLs, uri)
				}
			}
		}
	}
	if len(outcome.Results) == 0 && len(outcome.RemoteURLs) == 0 {
		return appjobs.ImageOutcome{}, provider.NewResponseInvalidError()
	}
	return outcome, nil
}

// authorize sets the Gemini header. The key is resolved in Go and zeroed after
// use, exactly like the OpenAI-compatible path.
func (a *GeminiImageAdapter) authorize(ctx context.Context, request *http.Request, config provider.Config) error {
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

func (a *GeminiImageAdapter) audit(ctx context.Context, request appjobs.ImageRequest, config provider.Config, status string, httpStatus int, started time.Time, callErr error, requestID string) {
	if a.registry == nil || a.registry.audit == nil {
		return
	}
	record := provider.RequestRecord{
		ID:         newRecordID(),
		JobID:      request.JobID,
		ProviderID: config.ID,
		Capability: provider.CapabilityImage,
		Model:      request.Model,
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

// Compile-time proof that the Gemini adapter satisfies the image port.
var _ appjobs.ImagePort = (*GeminiImageAdapter)(nil)
