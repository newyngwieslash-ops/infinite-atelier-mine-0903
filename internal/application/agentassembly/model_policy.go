package agentassembly

import (
	"context"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// model_policy.go is RP-06.5: the per-STAGE and per-CALL model policy keys
// FR-140 states (script_decision_model and its siblings), layered on the
// per-LAYER policy WP-08 built.
//
// # The resolution order, fixed
//
//	1. the call's OWN override (a single call naming its provider/model);
//	2. the project's STAGE override (a policy row for the stage's name);
//	3. the project's LAYER default (WP-08's per-layer policy, then 'default');
//	4. no match — refuse rather than quietly swapping providers.
//
// Case 1 already exists in the wiring (a named provider is honoured after an
// enabled check); case 3 is WP-08's `policyProvider`. This file supplies the
// KEY VOCABULARY and the STAGE-override read so cases 2 and 4 are honest:
// a stage naming a disabled provider is a refusal, not a fallback, and a
// stage with no policy falls through to the layer order instead of being
// invented one.
//
// # Why the keys live here
//
// ADR-0012 section 5 deferred these keys because "a stage is not a layer and
// the specification gives those keys no shape". The shape they now have is
// the one the schema already supported — a `project_provider_policies` row
// keyed on the stage name in a separate table namespace — so the deferral's
// reason (an invented shape) is answered rather than overruled.

// StageModelKeys are FR-140's per-capability model policy keys, mapped to the
// stages whose runs read them. The key is what a project's settings write;
// the stage is what a run executes.
var StageModelKeys = map[string]string{
	"script_decision_model":    "story_skeleton",
	"script_execution_model":   "adaptation_strategy",
	"script_supervision_model": "script_generation",
	"image_generation_model":   "asset_generation",
	"video_generation_model":   "storyboard_panel_generation",
	"tts_model":                "audio_generation",
	"embedding_model":          "memory_index",
}

// StagePolicySource reads a project's STAGE-level policy row. Returning
// found=false for a stage with no row is the normal path.
type StagePolicySource interface {
	// StageModelPolicy returns the provider (and optional model) a project's
	// stage override names.
	StageModelPolicy(ctx context.Context, projectID, stage string) (providerID string, modelID string, found bool, err error)
}

// ResolvedModelPolicy is what a call runs with, and WHERE each part came
// from — the provenance a run's snapshot records so a later reader can tell
// a stage override from a fallback.
type ResolvedModelPolicy struct {
	ProviderID string
	ModelID    string
	// ProviderSource is one of: "call", "stage", "layer", "default".
	ProviderSource string
}

// ResolveStageModelPolicy applies the fixed order for one call. The
// layerResolver is WP-08's existing per-layer read; it answers found=false
// when the project named nothing for the layer.
func ResolveStageModelPolicy(
	stageOverrideProvider, stageOverrideModel string,
	layerProvider string,
	firstEnabled string,
) (ResolvedModelPolicy, error) {
	if trimmed := strings.TrimSpace(stageOverrideProvider); trimmed != "" {
		return ResolvedModelPolicy{ProviderID: trimmed, ModelID: strings.TrimSpace(stageOverrideModel), ProviderSource: "stage"}, nil
	}
	if trimmed := strings.TrimSpace(layerProvider); trimmed != "" {
		return ResolvedModelPolicy{ProviderID: trimmed, ProviderSource: "layer"}, nil
	}
	if trimmed := strings.TrimSpace(firstEnabled); trimmed != "" {
		return ResolvedModelPolicy{ProviderID: trimmed, ProviderSource: "default"}, nil
	}
	// No match: the caller refuses rather than inventing a provider. A call
	// whose policy names nothing and whose project has no enabled provider is
	// the honest failure the fallback used to hide.
	return ResolvedModelPolicy{}, agentInvalidPolicy()
}

// agentInvalidPolicy is the refusal for a policy that resolves to nothing.
func agentInvalidPolicy() error {
	return agent.InvalidError("The model policy resolves to no provider: name one on the call, the stage, or the project's layer policy.")
}
