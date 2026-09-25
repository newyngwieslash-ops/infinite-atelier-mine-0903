//go:build !cgo

package onnxemb

import (
	"context"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// embedder_nocgo.go is the build without cgo.
//
// # Why this file exists rather than a note in a README
//
// The ONNX binding is cgo, so `CGO_ENABLED=0` excludes every one of its files and the BUILD FAILS
// with "build constraints exclude all Go files" — a message about a package, not about a feature. A
// build with cgo off is a real configuration (a cross-compile, a minimal container, a machine with no
// C toolchain), and the honest behaviour for it is a build that COMPILES and an embedder that
// reports itself unavailable, which is the same fail-soft answer the cgo build gives when the runtime
// library is missing.
//
// The shape mirrors `secretstore`'s platform split: a tagged implementation and an untagged one that
// refuses. What it must NOT do is silently become a different embedder — the feature-hash fallback in
// the memory package is the caller's choice, not this package's, so this returns an error rather than
// vectors that are not embeddings.

// Config is the same shape the cgo build takes, so a caller compiles unchanged.
type Config struct {
	RuntimePath string
	ModelPath   string
	VocabPath   string
	MaxTokens   int
}

// DefaultMaxTokens matches the cgo build's bound, so a caller reading either sees one number.
const DefaultMaxTokens = 512

// Embedder is the refusal the no-cgo build returns.
type Embedder struct{}

// New refuses in this build. It returns the embedder AND an error rather than only an error, so a
// caller that ignores the error still gets a value whose methods refuse rather than a nil deref.
func New(_ Config) (*Embedder, error) {
	return &Embedder{}, apperror.New("ONNX_NO_CGO", "unavailable", false,
		"This build has no C toolchain, so local ONNX embedding is not compiled in.", nil)
}

// Available reports false: nothing is loaded and nothing can be.
func (e *Embedder) Available(_ context.Context, _ string) bool { return false }

// Embed refuses rather than returning vectors that are not embeddings.
func (e *Embedder) Embed(_ context.Context, _ string, _ appmemory.EmbeddingRequest) (appmemory.EmbeddingResult, error) {
	return appmemory.EmbeddingResult{}, apperror.New("ONNX_NO_CGO", "unavailable", false,
		"This build has no C toolchain, so local ONNX embedding is not compiled in.", nil)
}

// Diagnostic explains the absence, so a settings panel can show it.
func (e *Embedder) Diagnostic() string {
	return "This build has no C toolchain, so local ONNX embedding is not compiled in."
}

// Describe names the state for a diagnostics panel.
func (e *Embedder) Describe() string { return "local embedding is not compiled into this build" }

// The compile-time proof that the no-cgo build still satisfies the port the memory service declares.
var _ appmemory.Embedder = (*Embedder)(nil)
