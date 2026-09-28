// Package providers is the application layer for provider configuration,
// text generation, and health. It owns no HTTP or secret primitives; it
// delegates to ports implemented in Infrastructure.
package providers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// TextMessage is one chat-completion message.
type TextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// TextRequest is a capability-checked text generation request.
type TextRequest struct {
	ProviderID string
	Model      string
	Messages   []TextMessage
	// Stream requests SSE streaming when the adapter supports it.
	Stream bool
}

// Validate checks the request shape before it reaches infrastructure.
func (r TextRequest) Validate() error {
	if !provider.ValidKindlessID(r.ProviderID) {
		return provider.NewConfigurationError()
	}
	if r.Model == "" {
		return provider.NewConfigurationError()
	}
	if len(r.Messages) == 0 {
		return provider.NewConfigurationError()
	}
	for _, message := range r.Messages {
		switch message.Role {
		case "system", "user", "assistant":
		default:
			return provider.NewInvalidInputError()
		}
		if len(message.Content) == 0 {
			return provider.NewInvalidInputError()
		}
	}
	return nil
}

// TextToolCall is one tool call a model asked for.
//
// It is a TRANSPORT type rather than part of any output schema, and the placement is
// forced by the specification. AGENT_CONTRACTS section 7's output schemas have no
// field for a tool call, and each of them declares additionalProperties false, so a
// tool call cannot ride inside the document a model returns. The wire protocols
// agree: an OpenAI-compatible response carries tool_calls as a SIBLING of content on
// the assistant message, not inside it. So this is where a tool call travels, and
// WP-07's runtime reads it from the reply rather than parsing it out of the
// validated document.
//
// Arguments is the model's own JSON, unvalidated. Section 6.1's chain validates it
// against the tool's input schema, and that check belongs to the runtime that owns
// the schema — not to the adapter, which has no registry to check it against.
type TextToolCall struct {
	// Key is the tool name the model used, in the `<domain>.<verb>_<object>` form of
	// section 6.2. It is a REQUEST: the registry and the ACL decide whether it runs.
	Key string `json:"key"`
	// Arguments is the JSON object the model supplied, as it supplied it.
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// TextResult is the completed generation result.
type TextResult struct {
	Content      string `json:"content"`
	FinishReason string `json:"finishReason,omitempty"`
	Model        string `json:"model,omitempty"`
	// ToolCalls lists what the model asked to call, in the order it asked. An empty
	// list means the reply was a complete answer, which is not an error: a stage that
	// does its work in one document has no tool calls.
	ToolCalls []TextToolCall `json:"toolCalls,omitempty"`
}

// TextEvent is one streamed generation event delivered to the frontend.
type TextEvent struct {
	// Delta is the incremental content chunk.
	Delta string `json:"delta,omitempty"`
	// Done marks successful completion.
	Done bool `json:"done,omitempty"`
	// ErrorCode carries a safe error category when the stream failed.
	ErrorCode string `json:"errorCode,omitempty"`
	// Diagnostic is a correlation ID the user can quote in a bug report. It
	// carries no message content and is safe to display (AGENTS §8.2).
	Diagnostic string `json:"diagnostic,omitempty"`
	// Retriable tells the UI whether offering a retry is meaningful.
	Retriable bool `json:"retriable,omitempty"`
}

// EventSink receives streamed events. It must honor context cancellation.
type EventSink interface {
	OnDelta(delta string)
	OnDone(result TextResult)
	OnError(err error)
}

// TextPort is the capability port implemented by provider adapters.
type TextPort interface {
	Generate(ctx context.Context, request TextRequest) (TextResult, error)
	Stream(ctx context.Context, request TextRequest, sink EventSink) error
}

// TextPortResolver resolves a configured provider to its compiled-in text
// adapter. It fails closed for unknown kinds.
type TextPortResolver interface {
	TextPortFor(ctx context.Context, providerID string) (TextPort, error)
}

// EmbeddingRequest is a capability-checked embedding request.
//
// It is deliberately thin: PRD FR-120 requires the embedding provider to be replaceable and
// the input is text, so there is nothing to carry but the model and the texts. The MODEL is
// required rather than defaulted because DOMAIN_MODEL section 14.5 keys the memory index's
// version on it, and a request that named none would make the caller's rows unattributable.
type EmbeddingRequest struct {
	ProviderID string
	Model      string
	Texts      []string
}

// Validate checks the request shape before it reaches infrastructure.
func (r EmbeddingRequest) Validate() error {
	if !provider.ValidKindlessID(r.ProviderID) {
		return provider.NewConfigurationError()
	}
	if r.Model == "" {
		return provider.NewConfigurationError()
	}
	if len(r.Texts) == 0 {
		return provider.NewInvalidInputError()
	}
	for _, text := range r.Texts {
		if len(text) == 0 {
			return provider.NewInvalidInputError()
		}
	}
	return nil
}

// EmbeddingResult is one batch of vectors.
//
// Model is the model that ACTUALLY answered, captured from the response rather than echoed
// from the request, for the reason the text port captured `response_model` in WP-07: a
// fallback or a provider substitution would otherwise write a provenance that lies.
//
// Version is the embedding RECIPE's version. The wire response has no such field — providers
// version their models by name — so the adapter derives it from what it can observe, which for
// a real provider is the model plus the vector width. A change to either is a change to the
// space, and section 14.5's rebuild is what moves the rows across.
type EmbeddingResult struct {
	Model   string
	Version string
	// Vectors are in the request's order, one per text.
	Vectors [][]float32
}

// EmbeddingPort is the capability port implemented by embedding adapters.
type EmbeddingPort interface {
	Embed(ctx context.Context, request EmbeddingRequest) (EmbeddingResult, error)
}

// EmbeddingPortResolver resolves a configured provider to its compiled-in embedding adapter.
type EmbeddingPortResolver interface {
	EmbeddingPortFor(ctx context.Context, providerID string) (EmbeddingPort, error)
}

// HealthPort is the reachability check port.
type HealthPort interface {
	Check(ctx context.Context, providerID string) (provider.HealthState, error)
}

// Repository is the persistence port for provider configuration metadata.
// Rows never contain secret values.
type Repository interface {
	SaveConfig(ctx context.Context, config provider.Config) error
	ListConfigs(ctx context.Context) ([]provider.Config, error)
	GetConfig(ctx context.Context, id string) (provider.Config, error)
	DeleteConfig(ctx context.Context, id string) error
	SaveRequestRecord(ctx context.Context, record provider.RequestRecord) error
	ListHealth(ctx context.Context) ([]provider.HealthState, error)
}

// AuditPort persists redacted provider request audit records.
type AuditPort interface {
	Record(ctx context.Context, record provider.RequestRecord) error
}

// ConfigDTO is the safe transport view of a provider configuration.
type ConfigDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
	BaseURL     string `json:"baseUrl"`
	SecretRef   string `json:"secretRef"`
	// MaxConcurrency is how many jobs this provider may run at once, where ZERO means
	// UNLIMITED (ADR-0018). The comment is on the wire shape as well as the domain type
	// because the interface is where a user meets the reading: a field that showed 0 without
	// saying what it means would read as "no work may run".
	MaxConcurrency int    `json:"maxConcurrency"`
	// RateLimitPerMinute is the per-minute request ceiling, zero unlimited
	// (T07) — the same zero-means-unlimited reading the concurrency field
	// states.
	RateLimitPerMinute int  `json:"rateLimitPerMinute"`
	LocalApproved      bool `json:"localApproved"`
	Enabled        bool   `json:"enabled"`
	Revision       int64  `json:"revision"`
	UpdatedAt      string `json:"updatedAt,omitempty"`
}

// Service is the application service orchestrating configs, secrets status,
// text generation, and health. It never holds secret values.
type Service struct {
	repository Repository
	resolver   TextPortResolver
	health     HealthPort
	now        func() time.Time
}

// NewService builds the providers application service.
func NewService(repository Repository, resolver TextPortResolver, health HealthPort) *Service {
	return &Service{repository: repository, resolver: resolver, health: health, now: time.Now}
}

// CreateOrUpdateConfig validates and persists a non-secret provider config.
// It performs the domain URL validation here so infrastructure never has to
// trust the stored shape.
//
// The ID is required: an empty ID would create a row whose secret reference is
// the bare namespace prefix, and identity must never be implicit at the
// security boundary.
func (s *Service) CreateOrUpdateConfig(ctx context.Context, input provider.ConfigInput) (provider.Config, error) {
	if s == nil || s.repository == nil {
		return provider.Config{}, provider.NewConfigurationError()
	}
	if !provider.ValidKindlessID(input.ID) {
		return provider.Config{}, provider.NewConfigurationError()
	}
	if !provider.IsValidKind(input.Kind) {
		return provider.Config{}, provider.NewUnsupportedError()
	}
	if !provider.IsUserConfigurableKind(input.Kind) {
		// The deterministic in-process mock is not a provider the user may
		// register: it exists so tests and local pipeline exercises can opt in
		// through code, and letting a client persist it would let a synthetic
		// adapter masquerade as a real provider.
		return provider.Config{}, provider.NewUnsupportedError()
	}
	if _, ok := provider.ValidateBaseURL(input.BaseURL); !ok {
		return provider.Config{}, provider.NewConfigurationError()
	}
	if input.DisplayName == "" || len(input.DisplayName) > 100 {
		return provider.Config{}, provider.NewConfigurationError()
	}
	// A limit outside the range is REFUSED rather than clamped: a caller who asked for 8000 and
	// silently got 1000 would believe their provider admits more than it does, and the refusal
	// names the range it can act on.
	if !provider.IsValidMaxConcurrency(input.MaxConcurrency) {
		return provider.Config{}, provider.NewConfigurationError()
	}
	// THE RATE LIMIT (T07) is validated the same way: out of range is a
	// refusal, not a clamp.
	if input.RateLimitPerMinute < 0 || input.RateLimitPerMinute > provider.RateLimitCeiling {
		return provider.Config{}, provider.NewConfigurationError()
	}
	existing, err := s.repository.GetConfig(ctx, input.ID)
	if err != nil {
		providerErr, ok := provider.AsProviderError(err)
		if !ok || providerErr.Category != provider.CategoryConfiguration {
			return provider.Config{}, provider.NewStorageError()
		}
		// Not-found is the normal create path.
		existing = provider.Config{}
	}
	now := s.now().UTC()
	config := existing
	if config.ID == "" {
		config.ID = input.ID
		config.CreatedAt = now
		config.Revision = 1
	} else {
		config.Revision++
	}
	config.Kind = input.Kind
	config.DisplayName = input.DisplayName
	config.BaseURL = input.BaseURL
	config.LocalApproved = input.LocalApprove
	config.Enabled = input.Enabled
	config.MaxConcurrency = input.MaxConcurrency
	config.RateLimitPerMinute = input.RateLimitPerMinute
	config.SecretRef = provider.SecretRefValue(config.ID)
	config.UpdatedAt = now
	if err := s.repository.SaveConfig(ctx, config); err != nil {
		return provider.Config{}, provider.NewStorageError()
	}
	return config, nil
}

// ListConfigs returns all persisted provider configs.
func (s *Service) ListConfigs(ctx context.Context) ([]provider.Config, error) {
	if s == nil || s.repository == nil {
		return nil, provider.NewStorageError()
	}
	configs, err := s.repository.ListConfigs(ctx)
	if err != nil {
		return nil, provider.NewStorageError()
	}
	return configs, nil
}

// DeleteConfig removes a provider config. The caller is responsible for
// separately deleting the secret via the secrets service.
func (s *Service) DeleteConfig(ctx context.Context, id string) error {
	if s == nil || s.repository == nil || !provider.ValidKindlessID(id) {
		return provider.NewConfigurationError()
	}
	if err := s.repository.DeleteConfig(ctx, id); err != nil {
		return provider.NewStorageError()
	}
	return nil
}

// Generate performs a non-streaming text request.
func (s *Service) Generate(ctx context.Context, request TextRequest) (TextResult, error) {
	if s == nil || s.resolver == nil {
		return TextResult{}, provider.NewUnsupportedError()
	}
	if err := request.Validate(); err != nil {
		return TextResult{}, err
	}
	port, err := s.resolver.TextPortFor(ctx, request.ProviderID)
	if err != nil {
		return TextResult{}, err
	}
	return port.Generate(ctx, request)
}

// Stream performs a streaming text request, delivering events to the sink.
func (s *Service) Stream(ctx context.Context, request TextRequest, sink EventSink) error {
	if s == nil || s.resolver == nil {
		return provider.NewUnsupportedError()
	}
	if err := request.Validate(); err != nil {
		return err
	}
	port, err := s.resolver.TextPortFor(ctx, request.ProviderID)
	if err != nil {
		return err
	}
	return port.Stream(ctx, request, sink)
}

// CheckHealth queries one provider's reachability.
func (s *Service) CheckHealth(ctx context.Context, providerID string) (provider.HealthState, error) {
	if s == nil || s.health == nil {
		return provider.HealthState{}, provider.NewUnsupportedError()
	}
	if !provider.ValidKindlessID(providerID) {
		return provider.HealthState{}, provider.NewConfigurationError()
	}
	return s.health.Check(ctx, providerID)
}
