//go:build cgo

// Package onnxemb implements the memory service's Embedder port over a local ONNX model.
//
// # What this package is, and the limit it states rather than hides
//
// It is the 「本地模式」 half of PRD FR-120's 「Embedding Provider 可替换；本地模式不得在未授权时上传
// 项目文本」: an embedder that runs on this machine, sends nothing anywhere, and is selected by
// configuration exactly as the provider-backed one is.
//
// **IT DOES NOT MAKE THIS BUILD MULTILINGUAL, AND SAYING SO IS PART OF ITS JOB.** PRD section 16's
// item is 「本地多语言 ONNX Embedding」, and the model this host can actually reach is
// `all-MiniLM-L6-v2`, whose vocabulary covers 11 of the canary corpus's 27 Chinese characters and
// turns the ban line 「女主不能穿红色，这是全剧的禁令。」 into ten `[UNK]` pieces out of sixteen. So the
// adapter is real and verified — it runs a real model through real inference and produces real
// semantic signal in English (`cat~kitten` 0.6169 against `cat~stock` 0.0882, measured) — and the
// MULTILINGUAL claim belongs to whichever model a user configures. What is missing is a model file,
// not a mechanism, and ADR-0026 records the measurement rather than the adjective.
//
// # Why the tokenizer is minimal, and why that is not laziness
//
// A real multilingual embedding needs its model's own tokenizer (BPE or SentencePiece, plus the
// normalisation its authors chose). Shipping a general one here would be shipping a second
// implementation of somebody else's algorithm with no way to check it against the original. What
// this package ships is WordPiece, because that is what the BERT-family model it was verified
// against uses, and a seam: `Tokenizer` is an interface, so a model that needs another one gets
// another one rather than a fork of this file.
//
// # Why the runtime is not vendored
//
// `onnxruntime.dll` is 13 MB and platform-specific, and the two builds of it are NOT interchangeable
// — see ADR-0026 for the probe that found this the hard way (the official release imports an API set
// this host lacks; the Python wheel's build does not). So the library is resolved at run time from a
// configured path, and its absence DISABLES local embedding with a diagnostic rather than failing a
// recall: that is the same fail-soft shape the media engine uses for ffmpeg, and for the same reason.
package onnxemb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// modelVersion is the embedding recipe's version, recorded on every vector.
//
// It exists for the reason `EmbeddingRequest` states: a change to the LOCAL algorithm has to switch
// the index version rather than silently mix two vector spaces, and a store that compared only the
// model name would mix the old pooling with the new. WP-22's pooling is what this names.
const modelVersion = "onnx-meanpool/v1"

// Config is what an operator supplies.
//
// Everything is a PATH or a name, and nothing is discovered by convention beyond the search order
// below: a build that guessed where a 13 MB native library lives would fail on somebody else's
// machine in a way they could not fix.
type Config struct {
	// RuntimePath is the directory or file holding the ONNX runtime library. Empty means "look
	// beside the executable and on the system path".
	RuntimePath string
	// ModelPath is the .onnx file. Required: there is no default model, because the choice of model
	// is the choice of what "similar" means.
	ModelPath string
	// VocabPath is the tokenizer's vocabulary. Required with a model, for the same reason.
	VocabPath string
	// MaxTokens bounds one text's sequence length. Zero uses the default.
	MaxTokens int
}

// DefaultMaxTokens bounds one text's sequence length.
//
// It is the model's own training limit rather than a figure invented here: BERT-family models are
// trained at 512 positions, and a longer input is truncated anyway — so the bound makes the
// truncation explicit and keeps the tensors a predictable size.
const DefaultMaxTokens = 512

// Embedder is the port implementation.
//
// It is built once and used concurrently: the ONNX session is not safe to use from two goroutines at
// once, so `mu` serialises inference rather than protecting the fields. Retrieval embeds one query at
// a time, so serialising costs nothing real and makes the invariant checkable.
type Embedder struct {
	mu      sync.Mutex
	runtime *ortRuntime
	model   *model
}

// runtime is the loaded ONNX environment, kept apart so a failure to load it is distinguishable
// from a failure to load a model.
type ortRuntime struct {
	path string
}

// THE ENVIRONMENT IS PROCESS-WIDE, AND ITS INITIALISATION IS NOT IDEMPOTENT.
//
// `ort.InitializeEnvironment` FAILS when it is called a second time — "The onnxruntime has already
// been initialized" — so a per-Embedder flag cannot see another instance's successful call. The
// first version of this file had exactly that flag, and the SECOND embedder built in one test binary
// reported "the runtime could not be loaded" while the truth was "it is already loaded", which sent
// the debugging after a library problem that did not exist. The guard is therefore package-level,
// and "already initialized" is treated as SUCCESS: the postcondition a caller wants is "the
// environment is up", and it is up.
var (
	environmentMu     sync.Mutex
	environmentPath   string
	environmentOnline bool
)

// ensureEnvironment initialises the ONNX environment once per process, for the given library.
//
// A SECOND library path is a REFUSAL rather than a re-initialisation: the environment is global and
// a process cannot run two runtimes at once, so silently ignoring the second path would make a
// configuration change appear to take effect when it had not.
func ensureEnvironment(libraryPath string) error {
	environmentMu.Lock()
	defer environmentMu.Unlock()
	if environmentOnline {
		if environmentPath != libraryPath {
			return apperror.New("ONNX_RUNTIME_CONFLICT", "configuration", false,
				"An ONNX runtime is already loaded from a different path.", nil)
		}
		return nil
	}
	ort.SetSharedLibraryPath(libraryPath)
	if err := ort.InitializeEnvironment(); err != nil {
		if strings.Contains(err.Error(), "already been initialized") {
			environmentOnline, environmentPath = true, libraryPath
			return nil
		}
		return apperror.New("ONNX_RUNTIME_FAILED", "unavailable", false,
			"The ONNX runtime could not be loaded, so local embedding is unavailable.", err)
	}
	environmentOnline, environmentPath = true, libraryPath
	return nil
}

// model is a loaded session with the shapes its inputs expect.
type model struct {
	// inputs is the loaded session and its five tensors, or nil until it is opened.
	inputs *modelInputs
	// sequenceLength and dimensions come from the MODEL's declared shapes rather than from
	// configuration, because a mismatch is a refusal rather than something to accommodate.
	sequenceLength int
	dimensions     int
	// tokenizer is the model's own encoder, read from the file the caller named: WordPiece from a
	// `vocab.txt`, Unigram from a `tokenizer.json`.
	tokenizer *configuredTokenizer
}

// New builds an embedder from configuration.
//
// # What it refuses, and why each refusal is its own error
//
// A missing model or vocabulary is a CONFIGURATION mistake and is refused here, at construction,
// because a caller that asked for local embedding and silently got the feature-hash fallback would
// have no way to know the vectors are not the ones they configured. A missing RUNTIME is different:
// it is a fact about the MACHINE, so `Available` reports false and `Diagnostic` explains, which is
// what lets a build ship without the library and still start.
func New(config Config) (*Embedder, error) {
	modelPath := strings.TrimSpace(config.ModelPath)
	vocabPath := strings.TrimSpace(config.VocabPath)
	if modelPath == "" {
		return nil, apperror.New("ONNX_MODEL_MISSING", "configuration", false,
			"A local embedding model must be configured.", nil)
	}
	if vocabPath == "" {
		return nil, apperror.New("ONNX_VOCAB_MISSING", "configuration", false,
			"A local embedding model needs its vocabulary file.", nil)
	}
	if _, err := os.Stat(modelPath); err != nil {
		return nil, apperror.New("ONNX_MODEL_UNREADABLE", "configuration", false,
			"The configured embedding model could not be read.", err)
	}
	// WHICH TOKENIZER IS DECIDED BY THE FILE, not by configuration. A `vocab.txt` is WordPiece (the
	// BERT family) and a `tokenizer.json` whose model type is Unigram is SentencePiece (XLM-RoBERTa
	// and its multilingual relatives). The caller names a FILE and this reads what it is, which is the
	// same "the bytes decide" rule the import pipeline keeps and which saves a user from having to know
	// which family their model belongs to.
	tokenizer, err := loadTokenizer(vocabPath, modelPath)
	if err != nil {
		return nil, err
	}
	maxTokens := config.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	tokenizer.setMaxTokens(maxTokens)

	rt := &ortRuntime{path: resolveRuntime(config.RuntimePath)}
	return &Embedder{runtime: rt, model: &model{tokenizer: tokenizer}}, nil
}

// configuredTokenizer is what `New` hands the model: an encoder plus the path it came from.
//
// The path travels WITH the tokenizer because that is the object the session holder keeps, and the two
// are configured together: a vocabulary without its model is meaningless. The field was once declared
// and never assigned, which produced "Load model from  failed" — an error naming no file.
type configuredTokenizer struct {
	Tokenizer
	modelPath string
	maxTokens int
}

func (c *configuredTokenizer) setMaxTokens(limit int) {
	c.maxTokens = limit
	// The limit lives on the concrete implementation, which is where `Encode` reads it. Both
	// implementations carry the same field for the same reason.
	switch typed := c.Tokenizer.(type) {
	case *wordpiece:
		typed.maxTokens = limit
	case *unigram:
		typed.maxTokens = limit
	}
}

// loadTokenizer reads whichever tokenizer the given file is.
func loadTokenizer(path, modelPath string) (*configuredTokenizer, error) {
	lowered := strings.ToLower(path)
	if strings.HasSuffix(lowered, ".json") {
		unigram, err := loadUnigram(path, modelPath)
		if err != nil {
			return nil, err
		}
		return &configuredTokenizer{Tokenizer: unigram, modelPath: modelPath}, nil
	}
	wordpiece, err := loadWordPiece(path)
	if err != nil {
		return nil, err
	}
	return &configuredTokenizer{Tokenizer: wordpiece, modelPath: modelPath}, nil
}

// Available reports whether the runtime AND the model are loaded.
//
// It loads both lazily and CACHES the failure: a build with no runtime must not attempt a 13 MB
// library load on every recall.
func (e *Embedder) Available(_ context.Context, _ string) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.loadLocked() == nil
}

// Diagnostic explains why local embedding is unavailable, empty when it is available.
func (e *Embedder) Diagnostic() string {
	if e == nil {
		return "no local embedder is configured"
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.loadLocked(); err != nil {
		return err.Error()
	}
	return ""
}

// loadLocked brings the runtime and the session up, once.
//
// The error is deliberately NOT cached as a value: each call re-attempts and re-reports, because the
// two failures it can hit are both things a user can fix while the application runs (drop the
// library in, point the setting at a model), and a cached refusal would need a restart to clear.
func (e *Embedder) loadLocked() error {
	if e.model.inputs != nil {
		return nil
	}
	if e.runtime.path == "" {
		return apperror.New("ONNX_RUNTIME_MISSING", "unavailable", false,
			"The ONNX runtime library was not found, so local embedding is unavailable.", nil)
	}
	if err := ensureEnvironment(e.runtime.path); err != nil {
		return err
	}
	inputs, shapes, err := openSession(e.model.tokenizer.modelPath)
	if err != nil {
		return err
	}
	e.model.inputs = inputs
	e.model.sequenceLength = shapes.sequence
	e.model.dimensions = shapes.dimensions
	return nil
}

// Embed implements the port.
//
// Both `Model` and `Version` in the result come from the ADAPTER rather than from the request: the
// request says what the caller asked for, and the result has to say what actually answered — which
// here is always the configured file.
func (e *Embedder) Embed(ctx context.Context, _ string, request appmemory.EmbeddingRequest) (appmemory.EmbeddingResult, error) {
	if e == nil {
		return appmemory.EmbeddingResult{}, unavailable("No local embedder is configured.")
	}
	if len(request.Texts) == 0 {
		return appmemory.EmbeddingResult{}, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.loadLocked(); err != nil {
		return appmemory.EmbeddingResult{}, err
	}
	vectors := make([][]float32, 0, len(request.Texts))
	for _, text := range request.Texts {
		// The context is checked between texts rather than inside the session: ONNX Runtime's Run is
		// synchronous and not cancellable, so a cancel arrives at the next text. Batch sizes here are
		// one query or a bounded rebuild, so that is a short wait.
		if err := ctx.Err(); err != nil {
			return appmemory.EmbeddingResult{}, err
		}
		vector, err := e.embedOne(text)
		if err != nil {
			return appmemory.EmbeddingResult{}, err
		}
		vectors = append(vectors, vector)
	}
	return appmemory.EmbeddingResult{
		Model:   filepath.Base(e.model.tokenizer.modelPath),
		Version: modelVersion,
		Vectors: vectors,
	}, nil
}

// embedOne runs one text through the model and pools the result.
//
// # The three steps, and why each is where it is
//
//  1. TOKENIZE, with the model's own vocabulary. An empty text is still tokenised, to `[CLS] [SEP]`,
//     because a zero-length input is not a valid sequence and the model would refuse it.
//  2. RUN, with the three tensors a BERT-family encoder declares. The mask is what makes padding
//     harmless; without it the pooled vector would include every pad position and two texts of
//     different lengths would drift apart for a reason that has nothing to do with their meaning.
//  3. POOL, by attention-masked mean, then NORMALISE. Mean rather than `[CLS]` because that is what
//     sentence-transformers models are trained for, and normalised because cosine similarity is what
//     the memory index computes and a unit vector makes that a dot product.
func (e *Embedder) embedOne(text string) ([]float32, error) {
	tokens := e.model.tokenizer.Encode(text)
	length := e.model.sequenceLength

	ids := make([]int64, length)
	mask := make([]int64, length)
	types := make([]int64, length)
	for index, token := range tokens {
		if index >= length {
			break
		}
		ids[index] = token
		mask[index] = 1
	}
	// The three tensors are REUSED across texts, so their contents are rewritten wholesale: a
	// previous text's padding left behind would be read as real tokens, which is the silent
	// corruption that makes one text's vector depend on the one before it.
	copy(e.model.inputs.ids.GetData(), ids)
	copy(e.model.inputs.mask.GetData(), mask)
	copy(e.model.inputs.types.GetData(), types)

	if err := e.model.inputs.session.Run(); err != nil {
		return nil, apperror.New("ONNX_INFERENCE_FAILED", "unavailable", true,
			"The embedding model could not be run.", err)
	}
	// A COPY of the output, because `GetData` returns the tensor's live backing array: pooling in
	// place would mutate the buffer the next call reads. The probe that verified this package hit
	// exactly that, and every text came back identical.
	raw := e.model.inputs.out.GetData()
	rows := len(raw) / e.model.dimensions
	if rows == 0 || e.model.dimensions == 0 {
		return nil, apperror.New("ONNX_OUTPUT_SHAPE", "unavailable", false,
			"The embedding model returned no rows.", nil)
	}
	pooled := make([]float32, e.model.dimensions)
	used := float32(0)
	for row := 0; row < rows; row++ {
		if row < len(mask) && mask[row] == 0 {
			continue
		}
		used++
		base := row * e.model.dimensions
		for dimension := 0; dimension < e.model.dimensions; dimension++ {
			pooled[dimension] += raw[base+dimension]
		}
	}
	if used == 0 {
		// Every position masked: an input the model cannot represent. Refused rather than answered
		// with a zero vector, which would be a vector that matches nothing and looks like a result.
		return nil, apperror.New("ONNX_EMPTY_INPUT", "invalid_input", false,
			"The text produced no tokens for the embedding model.", nil)
	}
	for dimension := range pooled {
		pooled[dimension] /= used
	}
	return normalise(pooled), nil
}

// normalise scales a vector to unit length.
//
// A zero vector is returned unchanged rather than divided by zero: it means the model produced
// nothing, and turning that into NaN would spread the problem into every comparison.
func normalise(vector []float32) []float32 {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	if sum == 0 {
		return vector
	}
	length := float32(1)
	// The square root is taken in float64 and converted once, so a long vector does not accumulate
	// rounding in the scaling of every component.
	length = float32(sqrt64(sum))
	for index := range vector {
		vector[index] /= length
	}
	return vector
}

// sqrt64 is math.Sqrt without importing math for one call, kept local so this file's dependencies
// stay visible: the package needs exactly one transcendental function.
func sqrt64(value float64) float64 {
	if value <= 0 {
		return 0
	}
	// Newton's method converges from a reasonable seed in a handful of iterations for the magnitudes
	// in play here (a squared norm of a unit vector is 1).
	guess := value
	for iteration := 0; iteration < 32; iteration++ {
		next := 0.5 * (guess + value/guess)
		if next == guess {
			break
		}
		guess = next
	}
	return guess
}

// resolveRuntime finds the ONNX runtime library.
//
// # The search order, and why the executable's directory is in it
//
// A packaged desktop build puts native libraries beside the executable, which is the one location a
// user does not have to configure. The configured path wins because an explicit setting is a
// decision, and the system path is last because a library found there is one this application did
// not place. What is NOT done is a recursive search: a build that walked the filesystem for a native
// library would be slow and would load whatever it found first.
func resolveRuntime(configured string) string {
	candidates := []string{}
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		// A configured path may be the library itself or the directory holding it, because both are
		// reasonable things for a user to paste.
		if info, err := os.Stat(trimmed); err == nil {
			if info.IsDir() {
				candidates = append(candidates, filepath.Join(trimmed, runtimeFileName()))
			} else {
				candidates = append(candidates, trimmed)
			}
		}
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), runtimeFileName()))
	}
	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(workingDir, runtimeFileName()))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// runtimeFileName is the library's name on this platform.
func runtimeFileName() string {
	switch {
	case isWindows():
		return "onnxruntime.dll"
	case isDarwin():
		return "libonnxruntime.dylib"
	default:
		return "libonnxruntime.so"
	}
}

// unavailable builds the error the port's callers see when nothing is configured.
func unavailable(message string) error {
	return apperror.New("ONNX_EMBEDDER_UNAVAILABLE", "unavailable", false, message, nil)
}

// The compile-time proof that this satisfies the port the memory service declares.
var _ appmemory.Embedder = (*Embedder)(nil)

// ErrNoRuntime is returned by the loader when no library was found, so a caller can distinguish it
// from a library that was found and refused.
var ErrNoRuntime = errors.New("no onnx runtime library was found")

// Describe reports what is configured, for a diagnostics panel. It never loads anything, so it is
// safe to call from a settings screen.
func (e *Embedder) Describe() string {
	if e == nil || e.model == nil || e.model.tokenizer == nil {
		return "no local embedding model is configured"
	}
	return fmt.Sprintf("%s (runtime: %s)", filepath.Base(e.model.tokenizer.modelPath), e.runtime.path)
}
