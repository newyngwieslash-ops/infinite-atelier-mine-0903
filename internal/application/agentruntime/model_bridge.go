package agentruntime

import (
	"context"
	"errors"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// model_bridge.go connects the runtime's ModelPort to a provider text adapter.
//
// The two live on opposite sides of AGENTS section 7.2's dependency direction. The
// runtime is application code and must not know about wire protocols, provider
// configuration or secrets; a provider adapter is infrastructure and must not know
// about agents, prompts or schemas. Neither imports the other, so this file states
// the translation in one place, and the composition root wires the two together.
//
// The bridge exists because the SQLiteMock/kind switch is NOT the runtime's business:
// the runtime asks for "the model this project's policy named", and whether that is a
// paid provider or the deterministic mock is decided here by the provider's own kind.

// TextGenerator is the provider service's text surface, as this bridge uses it.
//
// It is one method rather than an import of the providers service, so this package
// depends on the CAPABILITY rather than on a particular service: the composition root
// passes whatever satisfies it, and a test passes a double.
type TextGenerator interface {
	Generate(ctx context.Context, request TextGenerationRequest) (TextGenerationResult, error)
}

// TextGenerationRequest is one provider call, in this package's own vocabulary.
type TextGenerationRequest struct {
	ProviderID string
	Model      string
	// Layer is which agent layer is asking, so a port that resolves an UNNAMED provider can read the
	// policy section 13 states per layer. Without it the only answer available to a port is "whichever
	// provider is enabled first", which makes the layer's policy unrepresentable.
	Layer agent.AgentLayer
	// ProjectID scopes the policy lookup: a model policy belongs to a PROJECT, so a port resolving one
	// needs to know which project's. Empty means the caller is not working in a project, and the policy
	// lookup is then skipped rather than guessed at.
	ProjectID string
	Messages  []TextMessage
}

// TextGenerationResult is a provider's answer.
//
// ToolCalls is what makes the bridge worth writing rather than inlining: the provider
// protocol carries a tool call beside the content, and the runtime's ModelReply does
// too, so the translation is field for field and a misplaced call would be a
// compile error at neither end.
type TextGenerationResult struct {
	Content   string
	Model     string
	ToolCalls []TextGenerationToolCall
}

// TextGenerationToolCall is one tool call the provider reported.
type TextGenerationToolCall struct {
	Key       string
	Arguments []byte
}

// ModelBridge adapts a TextGenerator to ModelPort.
type ModelBridge struct {
	generator TextGenerator
}

// NewModelBridge builds the bridge.
//
// A nil generator is accepted and makes the bridge unavailable rather than panicking,
// because a composition root that failed to resolve a provider should produce a
// refusal at run time naming the cause, not a crash at startup.
func NewModelBridge(generator TextGenerator) *ModelBridge {
	return &ModelBridge{generator: generator}
}

// Available reports whether the bridge can generate.
func (b *ModelBridge) Available() bool {
	return b != nil && b.generator != nil
}

// Complete implements ModelPort.
//
// The message roles are translated and nothing else: this bridge does not assemble,
// filter or reorder a prompt, because the order IS the security boundary and it is
// Assembler's to decide (section 5). A bridge that rearranged messages would be
// quietly rewriting the boundary.
func (b *ModelBridge) Complete(ctx context.Context, request ModelRequest) (ModelReply, error) {
	if !b.Available() {
		return ModelReply{}, agent.UnavailableError()
	}
	messages := make([]TextMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		messages = append(messages, TextMessage{Role: message.Role, Content: message.Content})
	}
	result, err := b.generator.Generate(ctx, TextGenerationRequest{
		ProviderID: request.ProviderID, Model: request.ModelID, Layer: request.Layer,
		ProjectID: request.ProjectID, Messages: messages,
	})
	if err != nil {
		return ModelReply{}, err
	}
	reply := ModelReply{Content: result.Content, Model: result.Model, FinishReason: "stop"}
	for _, call := range result.ToolCalls {
		// A call with no key is refused rather than dropped, for the reason the
		// adapter refuses the whole reply: a model that asked for something and was
		// silently ignored looks exactly like one that asked for nothing.
		if strings.TrimSpace(call.Key) == "" {
			return ModelReply{}, provider.NewResponseInvalidError()
		}
		reply.ToolCalls = append(reply.ToolCalls, ToolCallRequest{
			Key: call.Key, Arguments: call.Arguments,
		})
	}
	return reply, nil
}

// Compile-time proof that the bridge satisfies the port it stands in for: the runtime
// is handed a ModelBridge where it expects a ModelPort.
var _ ModelPort = (*ModelBridge)(nil)

// ClassifyProviderFailure maps a provider refusal onto the runtime's error taxonomy.
//
// It exists because section 14.2's retriability list is written in terms of the
// AGENT runtime's decisions while the categories come from the provider domain, and
// the mapping between them is a policy rather than a rename:
//
//	401/403, content policy, invalid input, security — never retried, and named
//	  outright by section 14.2.
//	429, network, 5xx-transient, timeout — retriable, and section 14.1's list.
//	cancellation — its own thing: Retriable is false because a cancellation is a
//	  decision the user made, not a fault to retry.
//
// An unmapped category is NOT retried. That direction matters: retrying a refusal
// the policy forbids is the behaviour section 14.2 exists to prevent, and the cost
// of not retrying a transient fault is one failed run a user can re-run.
func ClassifyProviderFailure(err error) error {
	if err == nil {
		return nil
	}
	providerErr, ok := provider.AsProviderError(err)
	if !ok {
		// An error from another layer passes through unchanged: it already carries
		// whatever category its own domain gave it, and re-wrapping would lose it.
		return err
	}
	return &ProviderFailure{
		ProviderCategory: providerErr.Category,
		SafeMessage:      providerErr.SafeMessage,
		Diagnostic:       providerErr.Diagnostic,
		IsRetriable:      providerErr.Retriable,
		IsCancelled:      providerErr.Category == provider.CategoryCancelled,
	}
}

// ProviderFailure is a provider refusal in the runtime's taxonomy.
//
// It implements Refusal, so IsRetriable answers from the policy above rather than
// from a table written at each call site.
type ProviderFailure struct {
	// ProviderCategory is the provider domain's own category, kept because this
	// type's Code and Category both derive from it and because a reader of a record
	// wants to see what the provider layer said rather than only the translation.
	ProviderCategory provider.ErrorCategory
	SafeMessage      string
	// Diagnostic is the correlation ID a user can quote. It carries no request,
	// header or key, which is what makes it safe to display (AGENTS section 8.2).
	Diagnostic string
	// IsRetriable is what the provider layer decided, which this type does not
	// second-guess: a 429's Retry-After is knowledge that layer has and this one
	// does not.
	IsRetriable bool
	// IsCancelled marks a cancellation, which section 15 records as cancelled rather
	// than failed.
	IsCancelled bool
}

func (e *ProviderFailure) Error() string {
	if e == nil {
		return ""
	}
	if e.SafeMessage != "" {
		return e.SafeMessage
	}
	return "The model call failed."
}

// Category maps the provider's category into this package's taxonomy.
//
// A provider timeout becomes CategoryModel rather than a category of its own,
// because the domain taxonomy has none and section 14 treats a model call that took
// too long as the attempt failing rather than as a separate situation. The wire form
// does distinguish "timeout" (see agent.WireCategory), and Code below keeps that
// visible in the record: a user reading a run sees a timeout, and the retry policy
// sees a model failure.
func (e *ProviderFailure) Category() agent.ErrorCategory {
	if e == nil {
		return agent.CategoryUnavailable
	}
	switch e.ProviderCategory {
	case provider.CategorySecurity:
		return agent.CategorySecurity
	case provider.CategoryInvalidInput, provider.CategoryContentPolicy:
		return agent.CategoryInvalidInput
	case provider.CategoryCancelled:
		return agent.CategoryCancelled
	case provider.CategoryStorage:
		return agent.CategoryStorage
	case provider.CategoryUnsupported, provider.CategoryConfiguration,
		provider.CategoryUnauthorized, provider.CategoryForbidden:
		// A configuration, credential or "not supported" refusal is not the model's
		// fault and is not fixed by retrying, which is the pair of facts the category
		// is for. The domain has no category of its own for it, and CategoryUnavailable
		// is the closest: the capability the run asked for is not available to it.
		return agent.CategoryUnavailable
	default:
		// The remote, network and timeout categories: the call was made and did not
		// produce an answer.
		return agent.CategoryModel
	}
}

// Code is the stable wire code for this refusal, which is what a caller branches on.
//
// It keeps the provider's own category rather than collapsing into the domain's, so
// a timeout and a remote 5xx are distinguishable in the record even though both are
// CategoryModel for the retry policy.
func (e *ProviderFailure) Code() string {
	if e == nil {
		return "agent.model_failed"
	}
	switch e.ProviderCategory {
	case provider.CategoryTimeout:
		return "agent.timeout"
	case provider.CategoryCancelled:
		return "agent.cancelled"
	case provider.CategoryRateLimited:
		return "agent.rate_limited"
	case provider.CategoryUnauthorized, provider.CategoryForbidden:
		return "agent.provider_unauthorized"
	case provider.CategoryUnsupported:
		return "agent.provider_unsupported"
	case provider.CategoryConfiguration:
		return "agent.provider_configuration"
	default:
		return "agent.model_failed"
	}
}

// Retriable follows section 14's division rather than the provider's own flag alone.
//
// The two agree for every category the provider layer maps, and where they could
// disagree the specification wins: a security or invalid-input refusal is never
// retried even if a provider marked it transient, because section 14.2 names those
// outright. That is the fail-closed direction and the reason this method exists
// rather than the field being read directly.
func (e *ProviderFailure) Retriable() bool {
	if e == nil {
		return false
	}
	switch e.Category() {
	case agent.CategorySecurity, agent.CategoryInvalidInput, agent.CategoryCancelled:
		return false
	}
	return e.IsRetriable
}

// Unwrap exposes the cancellation so errors.Is works on a wrapped context error.
func (e *ProviderFailure) Unwrap() error {
	if e == nil || !e.IsCancelled {
		return nil
	}
	return context.Canceled
}

// AsProviderFailure finds a provider failure through a wrapped chain.
//
// It is exported because the engine and the desktop layer both need to ask "was this
// a provider refusal, and which one", and doing it with errors.As at each site is how
// one of them ends up checking a different type.
func AsProviderFailure(err error) (*ProviderFailure, bool) {
	var failure *ProviderFailure
	if errors.As(err, &failure) {
		return failure, true
	}
	return nil, false
}
