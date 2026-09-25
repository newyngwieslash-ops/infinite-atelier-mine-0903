package main

import (
	"context"
	"database/sql"
	"strings"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	domainmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
	projectdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// embedder.go is the bridge from the memory service's embedder port to the provider registry.
//
// # Why a bridge rather than a direct port
//
// PRD FR-120 requires the embedding provider to be replaceable — "Embedding Provider 可替换" —
// and the two ends of that sentence are in two layers the memory package cannot see: the port it
// declares is `Available` plus `Embed`, and the registry resolves a provider id to an adapter
// through `provider_configs`. Something has to join them, and the join is a POLICY decision:
// which provider a project's embedding goes to, and what to do when it has none.
//
// It is the SAME decision `textGenerator` makes for the agent runtime (agent_wiring.go), one
// layer down, and its resolution order is deliberately the same shape:
//
//  1. An explicit provider id, which must exist and be enabled.
//  2. The project's policy for the `embedding` LAYER, which the schema has allowed since
//     migration 000006 — `project_provider_policies.layer` carries 'embedding' in its CHECK, and
//     `PolicyEmbedding` is its Go constant.
//  3. The first enabled provider whose kind can actually embed.
//
// # Step three is where this differs from the text bridge, and the difference is the security
// rule
//
// The text bridge's third step is "the first enabled provider", because a text provider is what
// the user configured and a run must go somewhere. An EMBEDDING call is not the same kind of
// thing: it sends the project's own text to a model whose only job is to produce a number, and
// PRD FR-120 says "本地模式不得在未授权时上传项目文本" while SECURITY section 16.2 asks for
// "Embedding 默认本地优先". So this bridge will NOT fall back to whatever provider happens to be
// enabled: it uses a provider only when the project NAMED one for embeddings.
//
// The consequence is that a project which has configured no embedding provider has no semantic
// channel, and this bridge reports that rather than silently sending its text somewhere.
//
// # FR-120's "关键词降级" in this build, stated exactly
//
// The deterministic feature-hash adapter EXISTS and is registered by
// `Registry.WithMockEmbeddingAdapter`; the `mock_embedding` branch below is therefore live for any
// build that registers one. What this build does NOT do is create such a provider CONFIGURATION:
// `IsUserConfigurableKind` refuses the kind, and `provider_configs.kind` CHECKs against
// `('openai_compatible','gemini_compatible','mock_media')` — so no row can carry it and this bridge
// will not resolve one in a composed build.
//
// That is the same three-guardrail treatment the text, image and media mocks have, and it is
// deliberate rather than an omission: a kind a user can persist is a kind a project can silently
// talk to, and an embedding call sends the project's own text. What serves FR-120's keyword fallback
// in a real deployment is a provider the user configures — a local one, whose endpoint this bridge
// reaches through the same `openai_compatible` arm — and the offline path for CI is the mock, which a
// harness registers the way the batch tests register the image mock.
//
// The first version of this comment claimed the fallback was "the documented local answer" and that
// "a build that wants it registers it as a provider the project can name". Both halves were false:
// nothing registered it, and no project could name it. An independent review found the contradiction.
// The deferral is recorded here rather than papered over, and STATUS section 0k lists it as a limit.
type projectEmbedder struct {
	registry *infraproviders.Registry
	db       *sql.DB
	// local is the ONNX embedder, nil in a build with no local model configured. It is tried FIRST,
	// which is what makes PRD FR-120's 「本地模式不得在未授权时上传项目文本」 true rather than
	// aspirational: a project whose text can be embedded on this machine never reaches the network.
	local appmemory.Embedder
}

// newProjectEmbedder builds the embedder bridge.
//
// `local` may be nil, and a nil local embedder is the state this build shipped in until WP-23: the
// comment above `projectEmbedder` recorded the deferral in as many words — "nothing registered it,
// and no project could name it". Now something registers it, and the resolution order below is where
// FR-120's local-first rule lives.
func newProjectEmbedder(registry *infraproviders.Registry, db *sql.DB, local appmemory.Embedder) *projectEmbedder {
	return &projectEmbedder{registry: registry, db: db, local: local}
}

// localFirst reports whether the local embedder can answer for this project.
func (e *projectEmbedder) localFirst(ctx context.Context, projectID string) bool {
	return e != nil && e.local != nil && e.local.Available(ctx, projectID)
}

// Available reports whether the project has an embedding provider it named.
//
// The question is answered by the same resolution Embed would do, minus the call: a project that
// named a provider which cannot embed must not report itself available, or the memory service
// would offer a semantic channel that fails on first use.
func (e *projectEmbedder) Available(ctx context.Context, projectID string) bool {
	if e == nil {
		return false
	}
	// THE LOCAL EMBEDDER FIRST, and this ordering IS the privacy rule. A build with a local model
	// answers every embedding on this machine, so no project text leaves it; only a build without one
	// falls through to a provider the user configured. An implementation that tried the provider
	// first would upload the text before discovering a local model was available.
	if e.localFirst(ctx, projectID) {
		return true
	}
	if e.registry == nil || e.db == nil {
		return false
	}
	providerID, ok := e.namedProvider(ctx, projectID)
	if !ok {
		return false
	}
	config, err := e.registry.ConfigFor(ctx, providerID)
	if err != nil {
		return false
	}
	return e.canEmbed(config.Kind)
}

// Embed embeds the request's texts with the project's embedding provider.
func (e *projectEmbedder) Embed(ctx context.Context, projectID string, request appmemory.EmbeddingRequest) (appmemory.EmbeddingResult, error) {
	if e == nil {
		return appmemory.EmbeddingResult{}, embeddingUnavailable("No embedding provider is configured.")
	}
	// The same local-first order as `Available`, and it has to be repeated here rather than assumed:
	// a caller reaches Embed directly as often as through Available, and a provider-first Embed would
	// upload text from a build that had a local model all along.
	//
	// A LOCAL FAILURE FALLS THROUGH RATHER THAN STOPPING. A model file that was deleted while the
	// application ran, or a runtime that failed to load, disables the local path without disabling
	// embedding — the same fail-soft shape the media engine uses for ffmpeg, and the same reason: a
	// fact about the MACHINE must not become a failed command when a configured alternative exists.
	if e.localFirst(ctx, projectID) {
		if result, err := e.local.Embed(ctx, projectID, request); err == nil {
			return result, nil
		}
	}
	if e.registry == nil || e.db == nil {
		return appmemory.EmbeddingResult{}, embeddingUnavailable("No embedding provider is configured.")
	}
	// An explicit provider wins over the project's policy, which is what FR-140's "支持项目默认、
	// 阶段覆盖和单次覆盖" means one layer down: a caller may state where its text goes.
	providerID := strings.TrimSpace(request.ProviderID)
	if providerID == "" {
		var ok bool
		providerID, ok = e.namedProvider(ctx, projectID)
		if !ok {
			return appmemory.EmbeddingResult{}, embeddingUnavailable(
				"This project names no embedding provider, so its text is not sent anywhere.")
		}
	}
	config, err := e.registry.ConfigFor(ctx, providerID)
	if err != nil {
		return appmemory.EmbeddingResult{}, err
	}
	if !e.canEmbed(config.Kind) {
		return appmemory.EmbeddingResult{}, embeddingUnavailable(
			"That provider cannot produce embeddings.")
	}
	port, err := e.registry.EmbeddingPortFor(ctx, providerID)
	if err != nil {
		return appmemory.EmbeddingResult{}, err
	}
	// The model is required by the port below, and a project that named no model gets the
	// provider's own default by asking with an empty one and letting the adapter's contract
	// decide. The model that ANSWERED is what comes back, and that is what the row records.
	result, err := port.Embed(ctx, appproviders.EmbeddingRequest{
		ProviderID: providerID,
		Model:      strings.TrimSpace(request.Model),
		Texts:      request.Texts,
	})
	if err != nil {
		return appmemory.EmbeddingResult{}, err
	}
	return appmemory.EmbeddingResult{
		Model:   result.Model,
		Version: result.Version,
		Vectors: result.Vectors,
	}, nil
}

// namedProvider resolves the provider a project's embedding layer names.
//
// An empty result means the project has said nothing about embeddings, which is the ordinary
// state and NOT an error: it is the answer that keeps this bridge from choosing a provider on the
// project's behalf.
func (e *projectEmbedder) namedProvider(ctx context.Context, projectID string) (string, bool) {
	project := strings.TrimSpace(projectID)
	if project == "" {
		return "", false
	}
	// The project's own `embedding` row, then its `default` row: the same two-step the agent
	// runtime's policy uses, so "where does my project's text go" has one answer shape.
	for _, layer := range []string{string(projectdomain.PolicyEmbedding), string(projectdomain.PolicyDefault)} {
		var policyJSON string
		err := e.db.QueryRowContext(ctx, `SELECT policy_json FROM project_provider_policies
			WHERE project_id = ? AND layer = ?`, project, layer).Scan(&policyJSON)
		if err != nil {
			continue
		}
		if providerID := providerFromPolicy(policyJSON); providerID != "" {
			return providerID, true
		}
	}
	return "", false
}

// embeddingUnavailable builds the refusal the memory service understands.
//
// It is a memory-domain error rather than a provider one because it is the MEMORY service that
// will render it, and its message is a sentence about the project's configuration rather than
// about a transport: a user reading it should learn that nothing was sent, not that a call
// failed.
func embeddingUnavailable(message string) error {
	return domainmemory.StorageError(message, nil)
}

// canEmbed reports whether a provider kind has an embedding adapter in this build.
//
// The default project row is deliberately excluded at this point rather than at the call site:
// a project whose DEFAULT provider is an image or media one must not have its text sent to it
// because embeddings fell through to "whatever is configured".
func (e *projectEmbedder) canEmbed(kind provider.Kind) bool {
	switch kind {
	case provider.KindOpenAICompatible, provider.KindMockEmbedding:
		return true
	default:
		return false
	}
}
