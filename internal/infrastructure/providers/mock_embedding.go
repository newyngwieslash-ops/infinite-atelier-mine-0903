package providers

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"sync"
	"unicode"

	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// MockEmbeddingAdapter implements the embedding port without contacting any provider.
//
// # Why it exists
//
// Two reasons, and the second is a product requirement rather than a testing one.
//
// PRD FR-120 forbids sending a project's text anywhere without authorisation — "本地模式不得
// 在未授权时上传项目文本" — so a build with no embedding provider configured must still be
// able to offer a semantic channel, or the feature is simply absent for a user who has not
// signed a provider up. FR-120's MVP names the answer: "MVP 支持 Provider Embedding 与关键词
// 降级". This adapter IS the keyword fallback: its vectors are a bag-of-words representation,
// computed in-process, so the semantic channel works offline.
//
// And AGENT_CONTRACTS section 18.3 forbids a CI run from calling a paid provider. AC-MEM-003
// (a threshold with nothing above it) and AC-MEM-005 (a rerank over summaries) are both tests
// that need vectors to be comparable and reproducible, which is exactly what a deterministic
// local adapter gives and what a network call cannot.
//
// # The three guardrails, the same three the other mocks have
//
//  1. It is resolved ONLY for `provider.KindMockEmbedding`, through the registry's kind switch.
//     No other kind reaches it.
//  2. `KindMockEmbedding` is refused by `provider.IsUserConfigurableKind`, so no client can
//     register one and the kind cannot appear in `provider_configs`.
//  3. It is registered by an explicit `WithMockEmbeddingAdapter` call at the composition root,
//     so a build that did not opt in reports "unsupported" rather than being answered by a mock
//     that appeared anyway.
//
// # The vectors are real vectors
//
// A hashing trick over the text's terms, L2-normalised, so that:
//
//   - the same text always produces the same vector, which is what makes a rerank's ordering
//     stable between runs;
//   - two texts that share terms score POSITIVELY against each other, and two that share none
//     score exactly zero, so a threshold at any value above zero means something;
//   - a query with the same words as a memory finds it, which is the property AC-MEM-005's
//     "Summary 检索" needs to be a real retrieval rather than a coin toss.
//
// It is NOT a semantic model: a paraphrase that shares no terms scores zero. That limit is
// recorded in ADR-0014 rather than papered over, and it is why the real provider adapter exists
// alongside this one.
type MockEmbeddingAdapter struct {
	mu sync.Mutex
	// calls records what was asked for, so a test can assert that a rebuild batched its texts
	// rather than calling once per row.
	calls []string
}

// MockEmbeddingDimensions is the width of the adapter's vectors.
//
// It is small because the values are stored per memory as four bytes each, and a fallback
// representation does not deserve a real model's width. It is wide enough that ordinary Chinese
// and English text does not collide systematically: the hashing trick spreads terms over these
// slots, and at this width a drama project's vocabulary lands mostly in distinct ones.
const MockEmbeddingDimensions = 256

// MockEmbeddingModel and MockEmbeddingVersion name the adapter's vectors.
//
// They travel to the row, and section 14.5's index versioning is built on them: a project that
// switches from this fallback to a real model switches the version, and the rows embedded by the
// old one stop being candidates rather than being compared across two spaces.
const (
	MockEmbeddingModel   = "mock-embedding"
	MockEmbeddingVersion = "feature-hash/v1"
)

// NewMockEmbeddingAdapter builds the adapter.
func NewMockEmbeddingAdapter() *MockEmbeddingAdapter {
	return &MockEmbeddingAdapter{}
}

// Calls returns the texts the adapter has been asked to embed, in order.
func (m *MockEmbeddingAdapter) Calls() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]string, len(m.calls))
	copy(copied, m.calls)
	return copied
}

// Embed implements the embedding port.
//
// The result's Model is this adapter's own name rather than the requested one, which is the
// honest answer and the same rule the text adapters follow: a run records the model that
// ANSWERED, and a fallback embedder that claimed the requested model would write a provenance
// that lies.
func (m *MockEmbeddingAdapter) Embed(ctx context.Context, request appproviders.EmbeddingRequest) (appproviders.EmbeddingResult, error) {
	if m == nil {
		return appproviders.EmbeddingResult{}, provider.NewUnsupportedError()
	}
	if err := request.Validate(); err != nil {
		return appproviders.EmbeddingResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return appproviders.EmbeddingResult{}, err
	}
	m.mu.Lock()
	m.calls = append(m.calls, request.Texts...)
	m.mu.Unlock()
	vectors := make([][]float32, 0, len(request.Texts))
	for _, text := range request.Texts {
		vectors = append(vectors, m.vectorFor(text))
	}
	return appproviders.EmbeddingResult{
		Model:   MockEmbeddingModel,
		Version: MockEmbeddingVersion,
		Vectors: vectors,
	}, nil
}

// vectorFor hashes a text's terms into a unit vector.
//
// # The rule, stated so a reader can check it
//
// The text is split into terms by the same rule the recall's rerank uses (a Han character is a
// term; a run of other letters and digits is a term). Each term is hashed to a slot and the slot
// is incremented. The vector is then L2-normalised, which makes the dot product between two of
// them a cosine similarity in [0, 1]: non-negative because counts are, and one for a text
// against itself.
//
// A term adds its weight at a hashed slot rather than at a fixed index, so the representation is
// a HASH and two unrelated terms can collide. At this width, with a project's vocabulary, the
// collisions are noise rather than a false match, and the alternative — a dictionary the adapter
// would have to keep — is a model, which is what the real adapter is for.
func (m *MockEmbeddingAdapter) vectorFor(text string) []float32 {
	vector := make([]float32, MockEmbeddingDimensions)
	terms := mockTerms(text)
	if len(terms) == 0 {
		// A text with no terms still gets a vector rather than nil: nil means "not embedded" on
		// the row, and a blank string that was deliberately embedded is embedded.
		return vector
	}
	for _, term := range terms {
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(term))
		slot := int(hash.Sum32() % uint32(MockEmbeddingDimensions))
		vector[slot]++
	}
	var sumSquares float64
	for _, value := range vector {
		sumSquares += float64(value) * float64(value)
	}
	if sumSquares == 0 {
		return vector
	}
	length := math.Sqrt(sumSquares)
	for index := range vector {
		vector[index] = float32(float64(vector[index]) / length)
	}
	return vector
}

// mockTerms splits a text into the terms the adapter counts.
//
// It is deliberately the same rule the recall's rerank uses rather than a second one, and it is
// written out here rather than shared through an import because the two live in different
// layers: the rerank is the application's and this is an adapter's. The duplication is one
// function with one rule, and a divergence would show up as a fallback embedder whose matches did
// not line up with the rerank's overlap count — which the AC-MEM-005 test covers.
func mockTerms(text string) []string {
	terms := []string{}
	current := strings.Builder{}
	flush := func() {
		if current.Len() > 0 {
			terms = append(terms, current.String())
		}
		current.Reset()
	}
	for _, symbol := range strings.ToLower(text) {
		if isHanTerm(symbol) {
			flush()
			terms = append(terms, string(symbol))
			continue
		}
		if unicode.IsLetter(symbol) || unicode.IsDigit(symbol) {
			current.WriteRune(symbol)
			continue
		}
		flush()
	}
	flush()
	return terms
}

// isHanTerm reports whether a rune is a Han character.
func isHanTerm(symbol rune) bool {
	if symbol >= 0x4E00 && symbol <= 0x9FFF {
		return true
	}
	if symbol >= 0x3400 && symbol <= 0x4DBF {
		return true
	}
	if symbol >= 0xF900 && symbol <= 0xFAFF {
		return true
	}
	return unicode.Is(unicode.Han, symbol)
}
