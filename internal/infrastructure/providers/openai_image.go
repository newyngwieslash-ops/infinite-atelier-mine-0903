package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

const (
	imagesGenerationsPath = "/images/generations"
	imagesEditsPath       = "/images/edits"
)

// OpenAIImageAdapter implements the image capability against an
// OpenAI-compatible endpoint. It mirrors the text adapter's structure: the
// secret is resolved in Go, the SSRF-guarded client is the only egress, and
// every call is audited with a redacted record.
type OpenAIImageAdapter struct {
	registry *Registry
	// clientFactory is the test seam; production uses guardedClient so policy
	// cannot drift between adapters.
	clientFactory func(config provider.Config) (*phttp.Client, error)
}

// NewOpenAIImageAdapter builds the adapter over the registry's ports.
func NewOpenAIImageAdapter(registry *Registry) *OpenAIImageAdapter {
	return &OpenAIImageAdapter{registry: registry}
}

func (a *OpenAIImageAdapter) clientFor(config provider.Config) (*phttp.Client, error) {
	if a.clientFactory != nil {
		return a.clientFactory(config)
	}
	return guardedClient(config)
}

// Generate performs an image generation or edit request.
func (a *OpenAIImageAdapter) Generate(ctx context.Context, request appjobs.ImageRequest) (appjobs.ImageOutcome, error) {
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

	isEdit := len(request.References) > 0 || request.Mask != nil
	var bodyReader io.Reader
	contentType := "application/json"
	if isEdit {
		bodyReader, contentType, err = buildEditBody(request)
	} else {
		bodyReader, contentType, err = buildGenerationBody(request)
	}
	if err != nil {
		return appjobs.ImageOutcome{}, err
	}

	path := imagesGenerationsPath
	if isEdit {
		path = imagesEditsPath
	}
	endpoint, err := endpointFor(config, path)
	if err != nil {
		return appjobs.ImageOutcome{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bodyReader)
	if err != nil {
		return appjobs.ImageOutcome{}, provider.NewInvalidInputError()
	}
	httpRequest.Header.Set("Content-Type", contentType)
	httpRequest.Header.Set("Accept", "application/json")
	if err := a.authorize(ctx, httpRequest, config); err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, 0, started, err, "", 0, 0)
		return appjobs.ImageOutcome{}, err
	}

	response, err := client.Do(ctx, httpRequest)
	if err != nil {
		a.audit(ctx, request, config, statusForError(err), 0, started, err, "", 0, 0)
		return appjobs.ImageOutcome{}, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get(providerRequestIDHeader)

	if response.StatusCode != http.StatusOK {
		mapped := mapHTTPStatus(response.StatusCode, response.Header)
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, mapped, requestID, 0, 0)
		return appjobs.ImageOutcome{}, mapped
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxImageResponseBytes))
	if err != nil {
		wrapped := provider.NewResponseInvalidError()
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, wrapped, requestID, 0, 0)
		return appjobs.ImageOutcome{}, wrapped
	}
	outcome, err := parseImagePayload(payload)
	if err != nil {
		a.audit(ctx, request, config, provider.StatusFailed, response.StatusCode, started, err, requestID, 0, 0)
		return appjobs.ImageOutcome{}, err
	}
	usage := parseUsage(payload)
	a.audit(ctx, request, config, provider.StatusSucceeded, response.StatusCode, started, nil, requestID, usage.promptTokens, usage.completionTokens)
	return outcome, nil
}

// maxImageResponseBytes bounds an inline image response. A base64 image block
// is roughly 4/3 the encoded size, so 64 MiB covers a large multi-image reply
// without allowing an unbounded read.
const maxImageResponseBytes = 64 << 20

func buildGenerationBody(request appjobs.ImageRequest) (io.Reader, string, error) {
	payload := map[string]any{
		"model":  request.Model,
		"prompt": request.Prompt,
	}
	if request.Count > 0 {
		payload["n"] = request.Count
	}
	if request.Size != "" {
		payload["size"] = request.Size
	}
	if request.Quality != "" {
		payload["quality"] = request.Quality
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", provider.NewInvalidInputError()
	}
	return bytes.NewReader(encoded), "application/json", nil
}

// buildEditBody assembles the multipart request for image edits. Reference
// images are attached as files with a neutral filename; the user's own
// filename never reaches the request.
func buildEditBody(request appjobs.ImageRequest) (io.Reader, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	fields := map[string]string{
		"model":  request.Model,
		"prompt": request.Prompt,
	}
	if request.Count > 0 {
		fields["n"] = itoa(request.Count)
	}
	if request.Size != "" {
		fields["size"] = request.Size
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			return nil, "", provider.NewInvalidInputError()
		}
	}
	for index, reference := range request.References {
		if err := writeImagePart(writer, "image", reference, index); err != nil {
			return nil, "", err
		}
	}
	if request.Mask != nil {
		if err := writeImagePart(writer, "mask", *request.Mask, 0); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", provider.NewInvalidInputError()
	}
	return bytes.NewReader(buffer.Bytes()), writer.FormDataContentType(), nil
}

func writeImagePart(writer *multipart.Writer, field string, input appjobs.ImageInput, index int) error {
	payload := input.Bytes
	if len(payload) == 0 && input.Data != "" {
		// A data URL or bare base64 reference is decoded here so the request
		// carries raw bytes rather than a nested encoding.
		decoded, _, err := decodeImageData(input.Data)
		if err != nil {
			return err
		}
		payload = decoded
	}
	if len(payload) == 0 {
		return provider.NewInvalidInputError()
	}
	mimeType := input.MIMEType
	if mimeType == "" {
		mimeType = "image/png"
	}
	header := make(textproto.MIMEHeader)
	filename := "reference-" + itoa(index) + extensionFor(mimeType)
	header.Set("Content-Disposition", `form-data; name="`+field+`"; filename="`+filename+`"`)
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return provider.NewInvalidInputError()
	}
	if _, err := part.Write(payload); err != nil {
		return provider.NewInvalidInputError()
	}
	return nil
}

func extensionFor(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

// parseImagePayload extracts inline base64 images or remote URLs from the
// OpenAI-compatible response shape.
func parseImagePayload(payload []byte) (appjobs.ImageOutcome, error) {
	var response struct {
		Data []struct {
			B64JSON       string `json:"b64_json"`
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return appjobs.ImageOutcome{}, provider.NewResponseInvalidError()
	}
	if response.Error != nil {
		// The provider refused the request; only the category reaches the user.
		return appjobs.ImageOutcome{}, provider.NewInvalidInputError()
	}
	outcome := appjobs.ImageOutcome{}
	for _, item := range response.Data {
		switch {
		case item.B64JSON != "":
			outcome.Results = append(outcome.Results, appjobs.ImageResult{
				Data:          item.B64JSON,
				MIMEType:      "image/png",
				RevisedPrompt: item.RevisedPrompt,
			})
		case item.URL != "":
			// The URL is provider-supplied and therefore untrusted; the runner
			// downloads it under the download policy rather than handing it to
			// the UI directly.
			outcome.RemoteURLs = append(outcome.RemoteURLs, item.URL)
		}
	}
	if len(outcome.Results) == 0 && len(outcome.RemoteURLs) == 0 {
		return appjobs.ImageOutcome{}, provider.NewResponseInvalidError()
	}
	return outcome, nil
}

// authorize resolves the secret and sets the Authorization header.
func (a *OpenAIImageAdapter) authorize(ctx context.Context, request *http.Request, config provider.Config) error {
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

// audit writes one redacted record. It uses the image capability so the audit
// trail distinguishes media calls from text calls.
func (a *OpenAIImageAdapter) audit(ctx context.Context, request appjobs.ImageRequest, config provider.Config, status string, httpStatus int, started time.Time, callErr error, requestID string, inputUnits, outputUnits int64) {
	if a.registry == nil || a.registry.audit == nil {
		return
	}
	record := provider.RequestRecord{
		ID:          newRecordID(),
		JobID:       request.JobID,
		ProviderID:  config.ID,
		Capability:  provider.CapabilityImage,
		Model:       request.Model,
		Status:      status,
		HTTPStatus:  httpStatus,
		LatencyMS:   time.Since(started).Milliseconds(),
		RequestID:   requestID,
		InputUnits:  inputUnits,
		OutputUnits: outputUnits,
		CreatedAt:   time.Now().UTC(),
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

// Compile-time proof that the image adapters satisfy the capability port.
var _ appjobs.ImagePort = (*OpenAIImageAdapter)(nil)
