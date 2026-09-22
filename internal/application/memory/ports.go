package memory

import (
	"context"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// Bounds. Each one exists because the thing it bounds is reachable from a model or from a
// document, so the ceiling is a security property rather than a tuning knob
// (SECURITY section 7.5's "Memory 候选上限").
const (
	// MaxCandidates bounds how many memories a scored retrieval may read and score. It is
	// the ceiling the vector search applies before any math happens, so a project with a
	// hundred thousand memories costs the same as one with this many.
	MaxCandidates = 500
	// DefaultCandidates is what a retrieval reads when the caller states none.
	DefaultCandidates = 120
	// MaxSummaryWindow bounds how many memories one summary may cover. A window that grew
	// without limit would produce a summary whose own provenance exceeded the budget it was
	// meant to save.
	MaxSummaryWindow = 40
	// DefaultSummaryWindow is the window a pipeline run uses.
	DefaultSummaryWindow = 20
	// MaxSummaryParents bounds how many summaries one higher-level summary may condense.
	MaxSummaryParents = 8
	// MaxContextItems bounds a built context across every channel, so the layers below
	// memory in section 5.3's priority list are never crowded out by recall.
	MaxContextItems = 80
	// MaxDeepRecallSummaries and MaxDeepRecallMessages are AGENT_CONTRACTS section 12.3's
	// own bounds on the deep recall tool ("maxSummaries", "maxRawMessages").
	MaxDeepRecallSummaries = 12
	MaxDeepRecallMessages  = 30
)

// Repository is the memory store's write side and its administrative reads.
//
// It is separate from Store, which is the RECENT MESSAGE port the runtime already had:
// that one reads the agent transcript, and this one reads and writes the memory store.
// ADR-0014 records why they are two tables, and this interface is the other half of that
// ruling — a reader looking for "where is memory written" finds it here and not among the
// transcript's methods.
type Repository interface {
	CreateItem(ctx context.Context, item memory.MemoryItem) error
	MemoryItemExists(ctx context.Context, id string) (bool, error)
	GetItem(ctx context.Context, id string) (memory.MemoryItem, error)
	ListItems(ctx context.Context, filter MemoryListFilter) ([]memory.MemoryItem, error)
	RecallCandidates(ctx context.Context, scope memory.Scope, limit int) ([]memory.MemoryItem, error)
	DeleteItem(ctx context.Context, id, actor string, at time.Time) (bool, error)
	SetLocked(ctx context.Context, id string, locked bool, actor string, at time.Time) (bool, error)
	UpdateContent(ctx context.Context, id, content, actor string, at time.Time) (bool, error)
	AssignEmbedding(ctx context.Context, id string, embeddingBlob []byte, model, version string, at time.Time) error
	ItemsNeedingEmbedding(ctx context.Context, projectID, model, version string, limit int) ([]memory.MemoryItem, error)
	VectorCandidates(ctx context.Context, scope memory.Scope, model, version string, limit int) ([]memory.MemoryItem, error)
	// PinnedItems returns a project's locked high-importance memories, which is the channel
	// AC-MEM-003 names as the threshold's exception.
	PinnedItems(ctx context.Context, projectID string, limit int) ([]memory.MemoryItem, error)
	// ClearEmbedding removes the vectors of the named items, which is what Delete on the vector
	// index does: an item without a vector is a memory that has not been embedded yet.
	ClearEmbedding(ctx context.Context, ids []string, at time.Time) error
	CreateSummaryWithSources(ctx context.Context, summary memory.MemoryItem, sources []memory.SummarySource, markSummarized bool) error
	ListSummarySources(ctx context.Context, summaryID string) ([]memory.SummarySource, error)
	SummariesOf(ctx context.Context, sourceMemoryID string) ([]memory.MemoryItem, error)
	UnsummarisedItems(ctx context.Context, scope memory.Scope, limit int) ([]memory.MemoryItem, error)
	AddEntityLinks(ctx context.Context, links []memory.EntityLink) error
	ListEntityLinks(ctx context.Context, memoryID string) ([]memory.EntityLink, error)
	SetSummarized(ctx context.Context, ids []string, at time.Time) error
}

// MemoryListFilter narrows a list read. It is the repository's own type so a caller cannot
// hand the port a shape the store does not implement.
type MemoryListFilter struct {
	ProjectID string
	// Types is the set to include. Empty means every type.
	Types []memory.MemoryType
	// IncludeDeleted is what the memory section's list asks for, so a user can see what
	// they removed. Recall never sets it.
	IncludeDeleted bool
	Limit          int
}

// VectorItem is one vector handed to an index.
type VectorItem struct {
	ID     string
	Vector []float32
}

// VectorHit is one search result.
//
// The similarity is a cosine similarity in [-1, 1], because both sides are normalised at
// write time (section 12.3's "小规模归一化点积"). It is NOT clamped: two opposite vectors
// really do score -1, and a threshold's whole purpose is to be comparable against it.
type VectorHit struct {
	ID         string
	Similarity float64
}

// SearchOptions narrows one vector search.
//
// The model and version are required rather than optional, and that is section 14.5's
// "重建后切换索引版本" expressed as a parameter: a search that did not name an embedding
// version would compare a query vector against vectors from other spaces, which produces a
// confident wrong answer.
type SearchOptions struct {
	Model   string
	Version string
	TopK    int
	// Limit bounds how many candidates are read from the store before scoring. Zero uses
	// DefaultCandidates; the ceiling is MaxCandidates.
	Limit int
}

// VectorIndex is the searchable vector store (ARCHITECTURE section 12.3).
//
// The four methods are that section's, and the MVP is "Float32 BLOB、小规模归一化点积、
// Scope 过滤、Threshold + TopK、可测试排序". Scope filtering belongs HERE rather than in
// the caller, because AGENT_CONTRACTS section 12.2 requires it to happen before scoring and
// an index that returned another project's row would have already broken that rule.
type VectorIndex interface {
	// Upsert writes or replaces the vectors of the named items.
	Upsert(ctx context.Context, scope memory.Scope, items []VectorItem) error
	// Search returns the nearest items in one scope and one embedding version.
	Search(ctx context.Context, scope memory.Scope, vector []float32, options SearchOptions) ([]VectorHit, error)
	// Delete removes the named items' vectors.
	Delete(ctx context.Context, ids []string) error
	// Rebuild re-embeds a scope from scratch.
	Rebuild(ctx context.Context, scope memory.Scope) error
}

// EmbeddingRequest is one text to embed.
type EmbeddingRequest struct {
	// ProviderID names the provider to embed with. Empty means the caller has no
	// preference, which the adapter layer resolves from the project's policy.
	ProviderID string
	// Model is the embedding model, which the caller resolved from policy. It is required:
	// section 14.5 keys the index version on it.
	Model string
	// Texts are what to embed, in order. The results are returned in the same order.
	Texts []string
}

// EmbeddingResult is what an embedder produced.
type EmbeddingResult struct {
	// Model is the model that ACTUALLY answered, which may differ from the one asked for.
	// It is recorded on the row rather than assumed, for the reason the runtime records
	// `response_model`: a vector that cites a model that did not produce it is a
	// provenance that lies.
	Model string
	// Version is the embedding recipe's version, so a change to a local algorithm switches
	// the index version instead of silently mixing two spaces.
	Version string
	// Vectors are in the request's order. Each is normalised by the caller, not here.
	Vectors [][]float32
}

// Embedder turns text into vectors.
//
// It is a port rather than a direct provider call because PRD FR-120 requires the embedding
// provider to be replaceable — "Embedding Provider 可替换；本地模式不得在未授权时上传项目
// 文本" — and because the deterministic feature-hash embedder this build ships is what makes
// the semantic channel testable without a network.
type Embedder interface {
	// Available reports whether any embedding provider is configured.
	Available(ctx context.Context, projectID string) bool
	// Embed embeds the request's texts.
	Embed(ctx context.Context, projectID string, request EmbeddingRequest) (EmbeddingResult, error)
}

// Clock and IDGenerator are the determinism ports every application service here has.
type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New() (string, error)
}
