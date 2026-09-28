package providers

import (
	"context"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// openai_effect.go is the real effect adapter, and its first statement is the
// refusal the 2026-09-26 audit's T03 asks for: the OpenAI-compatible speech
// endpoint (`/audio/speech`) synthesises WORDS, and this build has no verified
// effect-synthesis protocol to call instead. Rather than routing a description
// through the speech endpoint — which would read 「脚步」 aloud and file the
// recording as an effect — the adapter refuses until a channel that really
// produces sounds is wired in.
//
// A future real protocol plugs in here behind the same port: its request shape
// replaces the refusal, and the registry's `openaiEffect` field is what
// selects it. The port's contract (`EffectRequest`'s description and duration)
// is already the effect-shaped one, so nothing upstream changes.
type OpenAIEffectAdapter struct {
	registry *Registry
}

// NewOpenAIEffectAdapter builds the adapter over the registry's ports.
func NewOpenAIEffectAdapter(registry *Registry) *OpenAIEffectAdapter {
	return &OpenAIEffectAdapter{registry: registry}
}

// GenerateEffect refuses, with the reason a user can act on: the configured
// channel speaks, it does not produce sounds, and this build ships no verified
// effect protocol for its family. The refusal is audited in the provider
// stream, because a capability the channel was asked for and did not have is
// a fact a capability review reads.
func (a *OpenAIEffectAdapter) GenerateEffect(ctx context.Context, request appjobs.EffectRequest) (appjobs.AudioOutcome, error) {
	if a == nil || a.registry == nil {
		return appjobs.AudioOutcome{}, provider.NewUnsupportedError()
	}
	started := time.Now()
	err := provider.NewUnsupportedError()
	a.audit(ctx, request.JobID, request.Model, started, err)
	return appjobs.AudioOutcome{}, err
}

// audit records the refused call, on the same stream every other adapter
// records into. No secret, header or raw error text travels with it.
func (a *OpenAIEffectAdapter) audit(ctx context.Context, jobID, model string, started time.Time, callErr error) {
	if a == nil || a.registry == nil || a.registry.audit == nil {
		return
	}
	record := provider.RequestRecord{
		ID:         newRecordID(),
		JobID:      jobID,
		Capability: provider.CapabilityEffect,
		Model:      model,
		Status:     provider.StatusFailed,
		LatencyMS:  time.Since(started).Milliseconds(),
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
