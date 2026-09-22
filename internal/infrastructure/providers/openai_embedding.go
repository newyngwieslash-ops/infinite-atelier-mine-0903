package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// embeddingsPath is the OpenAI-compatible embeddings endpoint.
const embeddingsPath = "/embeddings"

// embeddingResponseLimit bounds how much of a response this adapter will read.
//
// The text adapter's megabyte is generous for a completion and far too small for a batch of
// vectors: a 1536-dimension float is six kilobytes of JSON, so thirty-two of them is about two
// hundred kilobytes of digits. Four megabytes covers a large batch of a wide model and is still
// a bound rather than a stream into memory. The reader caps what it will parse
// (maxEmbeddingDimensions), so a response inside this limit cannot make the decode allocate
// beyond that.
const embeddingResponseLimit = 4 << 20

// maxEmbeddingDimensions is the widest vector this adapter will accept.
//
// It is the storage format's ceiling (domain/memory's MaxVectorDimensions), stated here as well
// because this is the boundary where an unchecked number would become an allocation. A model
// wider than this is not one this build can store, so accepting it would move the refusal to a
// place with less context.
const maxEmbeddingDimensions = 8192

// OpenAITextEmbeddingAdapter implements the embedding port against an OpenAI-compatible
// /embeddings endpoint.
//
// # Why an adapter rather than only the fallback
//
// PRD FR-120 requires the embedding provider to be REPLACEABLE: "Embedding Provider 可替换；本地
// 模式不得在未授权时上传项目文本". A port with one implementation cannot demonstrate that, and
// the fallback's own recorded limit — a paraphrase sharing no terms scores zero — is exactly what
// a real model is for. So the two adapters exist side by side and a project chooses by
// configuring a provider.
//
// # What it reuses rather than reimplements
//
// Everything security-relevant comes from the text adapter's own path and none of it is written
// twice: `guardedClient` (the SSRF-guarded client with its TLS, timeout and redirect policy),
// `endpointFor` (which validates the base URL and refuses anything the policy rejects),
// `authorize` (which resolves the secret from the store and zeroes it after use), `mapHTTPStatus`
// (the error taxonomy) and the registry's audit recorder. An embedding request carries project
// text, so it deserves exactly the same transport as a completion — a second implementation of
// those five would be a second place for the security rules to be got wrong.
type OpenAITextEmbeddingAdapter struct {
	registry *Registry
	// clientFactory allows tests to inject a controlled client; production always builds the
	// guarded one.
	clientFactory func(config provider.Config) (*phttp.Client, error)
}

// NewOpenAITextEmbeddingAdapter builds the adapter over the registry's ports.
func NewOpenAITextEmbeddingAdapter(registry *Registry) *OpenAITextEmbeddingAdapter {
	return &OpenAITextEmbeddingAdapter{registry: registry}
}

func (a *OpenAITextEmbeddingAdapter) clientFor(config provider.Config) (*phttp.Client, error) {
	if a.clientFactory != nil {
		return a.clientFactory(config)
	}
	return guardedClient(config)
}

// Embed performs one embeddings call for the request's texts.
//
// The texts travel in ONE request rather than one each: a rebuild of a thousand memories would
// otherwise be a thousand round trips, and the provider takes a list.
func (a *OpenAITextEmbeddingAdapter) Embed(ctx context.Context, request appproviders.EmbeddingRequest) (appproviders.EmbeddingResult, error) {
	if a == nil || a.registry == nil {
		return appproviders.EmbeddingResult{}, provider.NewUnsupportedError()
	}
	if err := request.Validate(); err != nil {
		return appproviders.EmbeddingResult{}, err
	}
	config, err := a.registry.configs.GetConfig(ctx, request.ProviderID)
	if err != nil {
		return appproviders.EmbeddingResult{}, err
	}
	started := time.Now()
	client, err := a.clientFor(config)
	if err != nil {
		return appproviders.EmbeddingResult{}, err
	}
	body, err := json.Marshal(map[string]any{
		"model": request.Model,
		"input": request.Texts,
	})
	if err != nil {
		return appproviders.EmbeddingResult{}, provider.NewInvalidInputError()
	}
	endpoint, err := endpointFor(config, embeddingsPath)
	if err != nil {
		return appproviders.EmbeddingResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return appproviders.EmbeddingResult{}, provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, 0, started, err, "")
		return appproviders.EmbeddingResult{}, err
	}
	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, request, config, statusForError(err), 0, started, err, "")
		return appproviders.EmbeddingResult{}, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)
	if response.StatusCode != http.StatusOK {
		wrapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID)
		return appproviders.EmbeddingResult{}, wrapped
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, embeddingResponseLimit))
	if err != nil {
		wrapped := provider.NewResponseInvalidError()
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID)
		return appproviders.EmbeddingResult{}, wrapped
	}
	result, err := parseEmbeddings(payload, request.Model, len(request.Texts))
	if err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, err, requestID)
		return appproviders.EmbeddingResult{}, err
	}
	a.audit(ctx, request, config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID)
	return result, nil
}

// parseEmbeddings reads an embeddings response.
//
// # Two compliance rules, both refusals rather than repairs
//
//   - The response must carry exactly as many vectors as the request had texts. A short list
//     would silently pair one text's vector with another text's row, which is a defect no later
//     reader could attribute: the vectors are all valid, they are just the wrong ones.
//   - The `index` field is honoured, because the wire format does not promise order. Each vector
//     is placed at the index it declares, and a response whose indices do not cover the request
//     is refused.
//
// The version is derived from what the adapter can actually observe — the model the response
// names and the width of the vectors it returned. A provider that silently served a different
// model therefore writes a different index version, which is what makes section 14.5's rebuild
// able to notice.
func parseEmbeddings(payload []byte, requestedModel string, expected int) (appproviders.EmbeddingResult, error) {
	var document struct {
		Model string `json:"model"`
		Data  []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		return appproviders.EmbeddingResult{}, provider.NewResponseInvalidError()
	}
	if document.Error != nil {
		// The provider's own error text is NOT surfaced: it may quote the request, and the
		// request is project text (SECURITY section's minimisation rules).
		return appproviders.EmbeddingResult{}, provider.NewRemoteTransientError()
	}
	if len(document.Data) == 0 || len(document.Data) != expected {
		return appproviders.EmbeddingResult{}, provider.NewResponseInvalidError()
	}
	vectors := make([][]float32, expected)
	width := 0
	for _, entry := range document.Data {
		if entry.Index < 0 || entry.Index >= expected {
			return appproviders.EmbeddingResult{}, provider.NewResponseInvalidError()
		}
		if len(entry.Embedding) == 0 || len(entry.Embedding) > maxEmbeddingDimensions {
			return appproviders.EmbeddingResult{}, provider.NewResponseInvalidError()
		}
		if width == 0 {
			width = len(entry.Embedding)
		}
		if len(entry.Embedding) != width {
			// Ragged widths mean the response is not one model's output.
			return appproviders.EmbeddingResult{}, provider.NewResponseInvalidError()
		}
		if vectors[entry.Index] != nil {
			// A duplicate index means the placement is ambiguous, so one text would silently
			// lose its vector.
			return appproviders.EmbeddingResult{}, provider.NewResponseInvalidError()
		}
		converted := make([]float32, len(entry.Embedding))
		for position, value := range entry.Embedding {
			converted[position] = float32(value)
		}
		vectors[entry.Index] = converted
	}
	for _, vector := range vectors {
		if vector == nil {
			return appproviders.EmbeddingResult{}, provider.NewResponseInvalidError()
		}
	}
	model := strings.TrimSpace(document.Model)
	if model == "" {
		// A response that names no model still produced these vectors, and the caller has to
		// record SOMETHING on the row or the vectors are unattributable. The requested model is
		// the honest fallback here — unlike a silent substitution, the caller did ask for it.
		model = requestedModel
	}
	return appproviders.EmbeddingResult{
		Model: model,
		Version: "openai-compatible/v1/" + strings.TrimSpace(requestedModel) + "/" +
			itoa(width),
		Vectors: vectors,
	}, nil
}

// authorize resolves the secret and sets the Authorization header, the same way the text
// adapter does and for the same reason (the header is never logged and the bytes are zeroed).
func (a *OpenAITextEmbeddingAdapter) authorize(ctx context.Context, request *http.Request, config provider.Config) error {
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
//
// The capability is embedding and the unit count is the number of texts, because that is what the
// caller asked for and this adapter has no tokeniser to count with. The record is built the same
// way the text adapter's is — identifier, category, no message text — so an audit reader sees one
// shape for every capability.
func (a *OpenAITextEmbeddingAdapter) audit(ctx context.Context, request appproviders.EmbeddingRequest, config provider.Config, status string, httpStatus int, started time.Time, callErr error, requestID string) {
	if a.registry == nil || a.registry.audit == nil {
		return
	}
	record := provider.RequestRecord{
		ID:         newRecordID(),
		ProviderID: config.ID,
		Capability: provider.CapabilityEmbedding,
		Model:      request.Model,
		Status:     status,
		HTTPStatus: httpStatus,
		LatencyMS:  time.Since(started).Milliseconds(),
		RequestID:  requestID,
		InputUnits: int64(len(request.Texts)),
		CreatedAt:  time.Now().UTC(),
	}
	if callErr != nil {
		if providerErr, ok := provider.AsProviderError(callErr); ok {
			record.ErrorCode = string(providerErr.Category)
		} else {
			record.ErrorCode = "provider_call_failed"
		}
	}
	_ = a.registry.audit.SaveRequestRecord(ctx, record)
}
