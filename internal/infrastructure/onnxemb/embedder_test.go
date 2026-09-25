//go:build cgo

package onnxemb

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
)

// embedder_test.go covers the local ONNX embedder.
//
// # What can be tested without a runtime, and what cannot
//
// Half of this package is decision-making that needs no inference: the vocabulary loader, the
// WordPiece matcher, the search order for the runtime, the refusals for missing configuration. Those
// run everywhere and are where the SILENT defects live — the CRLF vocabulary is the one that bit the
// probe this package was verified with, and it made four different sentences embed identically.
//
// The other half needs the runtime AND a model, neither of which this repository ships: the runtime
// is a 13 MB platform-specific native library (ADR-0026 explains why it is not vendored) and the
// model is a file a user chooses. So those tests find what is on the machine and SKIP — loudly,
// naming what was missing — rather than passing. **A skip is the honest report**: a test that
// asserted nothing about inference while appearing to would be evidence about nothing, which is the
// failure mode this repository has recorded five times.

// findRuntimeAndModel looks for a usable runtime and model on this machine.
//
// The paths are the ones ADR-0026's reconnaissance found; the test states them so a reader can
// reproduce the run rather than wondering what it picked up.
func findRuntimeAndModel(t *testing.T) (runtimeDir, modelPath, vocabPath string) {
	t.Helper()
	runtimeCandidates := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Python", "Python310", "lib",
			"site-packages", "onnxruntime", "capi"),
	}
	for _, candidate := range runtimeCandidates {
		if _, err := os.Stat(filepath.Join(candidate, runtimeFileName())); err == nil {
			runtimeDir = candidate
			break
		}
	}
	// The model ADR-0026 measured. It lives in ANOTHER project's data directory, which is why the
	// test reads a path from the environment instead of hardcoding one: this repository must not
	// depend on a file it does not ship.
	modelPath = os.Getenv("IA_ONNX_MODEL")
	vocabPath = os.Getenv("IA_ONNX_VOCAB")
	if modelPath == "" || vocabPath == "" {
		// A conventional location, tried so a developer with the model gets the test for free.
		guess := filepath.Join("..", "..", "..", "testdata", "models")
		model := filepath.Join(guess, "model.onnx")
		vocab := filepath.Join(guess, "vocab.txt")
		if _, err := os.Stat(model); err == nil {
			modelPath = model
		}
		if _, err := os.Stat(vocab); err == nil {
			vocabPath = vocab
		}
	}
	return runtimeDir, modelPath, vocabPath
}

// requireInference skips — naming both halves — when the runtime or the model is absent.
func requireInference(t *testing.T, runtimeDir, modelPath, vocabPath string) {
	t.Helper()
	if runtimeDir == "" {
		t.Skip("no ONNX runtime library on this machine, so inference cannot be checked: " +
			"set the runtime path or put the library beside the test binary")
	}
	if modelPath == "" || vocabPath == "" {
		t.Skip("no ONNX model on this machine, so inference cannot be checked: " +
			"set IA_ONNX_MODEL and IA_ONNX_VOCAB to a sentence-embedding model and its vocabulary")
	}
}

// writeVocab writes a vocabulary file with the given line ending.
func writeVocab(t *testing.T, ending string, words []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vocab.txt")
	content := strings.Join(words, ending) + ending
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestACRLFVocabularyLoads is the defect this package's own probe hit.
//
// `vocab.txt` ships with CRLF endings. A reader that splits on `\n` alone leaves a carriage return
// on every word, so EVERY lookup misses, every word becomes `[UNK]`, and four different sentences
// embed to vectors whose cosine is exactly 1.0000 — which reads as "the model has no signal" when
// the truth is "the reader dropped a byte". The test asserts both endings load identically.
func TestACRLFVocabularyLoads(t *testing.T) {
	words := []string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", "the", "cat", "##s"}
	lf := loadOrFail(t, writeVocab(t, "\n", words))
	crlf := loadOrFail(t, writeVocab(t, "\r\n", words))

	if lf.VocabSize() != len(words) || crlf.VocabSize() != len(words) {
		t.Fatalf("sizes are %d and %d for %d words", lf.VocabSize(), crlf.VocabSize(), len(words))
	}
	// The property that matters: a lookup finds the word in BOTH files, and the identifiers agree.
	for _, word := range []string{"the", "cat", "##s"} {
		lfID, _ := lf.tokens[word]
		crlfID, ok := crlf.tokens[word]
		if !ok {
			t.Fatalf("the word %q is missing from the CRLF vocabulary", word)
		}
		if lfID != crlfID {
			t.Fatalf("the word %q has id %d in LF and %d in CRLF", word, lfID, crlfID)
		}
	}
	// And no key carries a carriage return, which is the defect stated directly.
	for key := range crlf.tokens {
		if strings.ContainsRune(key, '\r') {
			t.Fatalf("the vocabulary has a key with a carriage return: %q", key)
		}
	}
}

func loadOrFail(t *testing.T, path string) *wordpiece {
	t.Helper()
	tokenizer, err := loadWordPiece(path)
	if err != nil {
		t.Fatalf("loading %s: %v", path, err)
	}
	return tokenizer
}

// TestAVocabularyWithoutTheSpecialTokensIsRefused pins the three required entries.
//
// A silent fallback to id zero would make every unknown word "the first token", which is a vector
// that looks like a result. The refusal is what keeps that from happening.
func TestAVocabularyWithoutTheSpecialTokensIsRefused(t *testing.T) {
	for _, missing := range []string{"[UNK]", "[CLS]", "[SEP]"} {
		words := []string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", "the"}
		kept := make([]string, 0, len(words))
		for _, word := range words {
			if word != missing {
				kept = append(kept, word)
			}
		}
		if _, err := loadWordPiece(writeVocab(t, "\n", kept)); err == nil {
			t.Fatalf("a vocabulary without %s was accepted", missing)
		}
	}
	// An empty file is refused rather than treated as a vocabulary of nothing.
	if _, err := loadWordPiece(writeVocab(t, "\n", nil)); err == nil {
		t.Fatal("an empty vocabulary was accepted")
	}
}

// TestEncodingWrapsTheTextInTheSequenceMarkers pins the shape a BERT-family encoder needs.
func TestEncodingWrapsTheTextInTheSequenceMarkers(t *testing.T) {
	tokenizer := loadOrFail(t, writeVocab(t, "\n", []string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", "the", "cat", "##s", "sat"}))
	tokenizer.maxTokens = 64

	ids := tokenizer.Encode("the cats sat")
	if len(ids) < 3 {
		t.Fatalf("an encoding of three words is %v", ids)
	}
	if ids[0] != tokenizer.clsID {
		t.Fatalf("the sequence does not begin with [CLS]: %v", ids)
	}
	if ids[len(ids)-1] != tokenizer.sepID {
		t.Fatalf("the sequence does not end with [SEP]: %v", ids)
	}
	// "cats" is not a token, so it splits into "cat" + "##s" — which is what a sub-word vocabulary is
	// FOR, and what a tokenizer that gave up at the whole word would turn into a single [UNK].
	foundPiece := false
	unknowns := 0
	for _, id := range ids {
		if id == tokenizer.unknownID {
			unknowns++
		}
		if id == tokenizer.tokens["##s"] {
			foundPiece = true
		}
	}
	if !foundPiece {
		t.Fatalf("the sub-word piece ##s was not used for \"cats\": %v", ids)
	}
	if unknowns != 0 {
		t.Fatalf("a word the vocabulary can spell became %d unknown tokens: %v", unknowns, ids)
	}
}

// TestAnUnknownWordBecomesOneUnknownToken is the other half of the sub-word rule.
func TestAnUnknownWordBecomesOneUnknownToken(t *testing.T) {
	tokenizer := loadOrFail(t, writeVocab(t, "\n", []string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", "the"}))
	tokenizer.maxTokens = 64
	ids := tokenizer.Encode("the \u9b3c\u602a the")
	unknowns := 0
	for _, id := range ids {
		if id == tokenizer.unknownID {
			unknowns++
		}
	}
	// ONE unknown for the word, not one per character: the reference tokenizers do the same, and a
	// per-character fallback would pad every unrecognised word into the sequence length.
	if unknowns != 1 {
		t.Fatalf("\u9b3c\u602a produced %d unknown tokens: %v", unknowns, ids)
	}
}

// TestTheEncodeBoundIsEnforced keeps a pathological input from asking for unbounded work.
func TestTheEncodeBoundIsEnforced(t *testing.T) {
	words := []string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", "the"}
	tokenizer := loadOrFail(t, writeVocab(t, "\n", words))
	tokenizer.maxTokens = 8
	long := strings.Repeat("the ", 500)
	ids := tokenizer.Encode(long)
	if len(ids) > 8 {
		t.Fatalf("the encoding is %d tokens and the bound is 8", len(ids))
	}
	if ids[len(ids)-1] != tokenizer.sepID {
		// The [SEP] must survive truncation: a sequence without it is one the model reads as
		// unterminated.
		t.Fatalf("truncation dropped the [SEP]: %v", ids)
	}
}

// TestAShortTextIsNotPaddedToTheBound proves the bound is a CEILING rather than a fixed length at
// the tokenizer's level — the padding happens in the tensor, where the mask makes it harmless.
func TestAShortTextIsNotPaddedToTheBound(t *testing.T) {
	tokenizer := loadOrFail(t, writeVocab(t, "\n", []string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", "the", "cat"}))
	tokenizer.maxTokens = 64
	ids := tokenizer.Encode("the cat")
	if len(ids) != 4 {
		t.Fatalf("a two-word text encodes to %d tokens, want [CLS] the cat [SEP]", len(ids))
	}
}

// TestAnUnconfiguredEmbedderRefusesRatherThanDegrading pins the construction refusals.
//
// A caller that asked for local embedding and silently got the feature-hash fallback would have no
// way to know its vectors are not the ones it configured, so a missing model is refused at
// construction rather than swallowed.
func TestAnUnconfiguredEmbedderRefusesRatherThanDegrading(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("an embedder with no model was built")
	}
	if _, err := New(Config{ModelPath: "model.onnx"}); err == nil {
		t.Fatal("an embedder with no vocabulary was built")
	}
	if _, err := New(Config{ModelPath: filepath.Join(t.TempDir(), "absent.onnx"), VocabPath: "v.txt"}); err == nil {
		t.Fatal("an embedder pointing at an absent model was built")
	}
}

// TestAMissingRuntimeDisablesRatherThanFails is the fail-soft rule.
//
// A missing native library is a fact about the MACHINE, not about the request — the same distinction
// the media engine makes for ffmpeg. `Available` reports false and `Diagnostic` explains, so a build
// without the library starts and says why local embedding is off.
func TestAMissingRuntimeDisablesRatherThanFails(t *testing.T) {
	vocab := writeVocab(t, "\n", []string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", "the"})
	modelPath := filepath.Join(t.TempDir(), "not-a-real-model.onnx")
	if err := os.WriteFile(modelPath, []byte("not a model"), 0o600); err != nil {
		t.Fatal(err)
	}
	embedder, err := New(Config{
		ModelPath: modelPath, VocabPath: vocab,
		// A directory with no library in it, so the search finds nothing.
		RuntimePath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("building an embedder with no runtime failed, and it must not: %v", err)
	}
	ctx := context.Background()
	if embedder.Available(ctx, "project-1") {
		t.Fatal("an embedder with no runtime reports itself available")
	}
	if embedder.Diagnostic() == "" {
		t.Fatal("an unavailable embedder gave no diagnostic")
	}
	// And an Embed call REFUSES rather than returning empty vectors, which a caller could store as
	// though they were embeddings.
	if _, err := embedder.Embed(ctx, "project-1", appmemory.EmbeddingRequest{Texts: []string{"hello"}}); err == nil {
		t.Fatal("embedding with no runtime returned a result")
	}
}

// TestEmbeddingProducesNormalisedVectors is the inference test, and it is the one that needs a real
// model: it asserts the shape, the unit length and the SEMANTIC SIGNAL — the last of which is what
// separates a working embedder from a plausible one.
func TestEmbeddingProducesNormalisedVectors(t *testing.T) {
	runtimeDir, modelPath, vocabPath := findRuntimeAndModel(t)
	requireInference(t, runtimeDir, modelPath, vocabPath)

	embedder, err := New(Config{RuntimePath: runtimeDir, ModelPath: modelPath, VocabPath: vocabPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if !embedder.Available(ctx, "project-1") {
		t.Skipf("the runtime or model did not load, so inference cannot be checked: %s", embedder.Diagnostic())
	}
	result, err := embedder.Embed(ctx, "project-1", appmemory.EmbeddingRequest{
		Texts: []string{"the cat sat on the mat", "a dog sat on the rug", "the stock market fell sharply today"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(result.Vectors) != 3 {
		t.Fatalf("three texts produced %d vectors", len(result.Vectors))
	}
	width := len(result.Vectors[0])
	if width == 0 {
		t.Fatal("the embedding has no dimensions")
	}
	for index, vector := range result.Vectors {
		if len(vector) != width {
			t.Fatalf("vector %d has %d dimensions and the first has %d", index, len(vector), width)
		}
		var sum float64
		for _, value := range vector {
			sum += float64(value) * float64(value)
		}
		if math.Abs(math.Sqrt(sum)-1) > 1e-3 {
			t.Fatalf("vector %d has length %.6f rather than 1", index, math.Sqrt(sum))
		}
	}
	// THE SEMANTIC SIGNAL. A cat and a kitten are nearer each other than a cat and a stock price, and
	// an embedder that returned noise would not show that. The margin is asserted rather than the
	// absolute values, because the numbers are the model's and not this package's.
	near := dot(result.Vectors[0], result.Vectors[1])
	far := dot(result.Vectors[0], result.Vectors[2])
	if near <= far {
		t.Fatalf("the related pair scores %.4f and the unrelated pair %.4f, so the embedder carries no signal", near, far)
	}
	// And the model and version are recorded from the ADAPTER, which is what makes a vector's
	// provenance true rather than inherited from a request.
	if result.Version == "" || result.Model == "" {
		t.Fatalf("the result names model %q and version %q", result.Model, result.Version)
	}
}

// TestTheSameTextEmbedsIdentically is the determinism a stored vector depends on.
//
// A rebuild compares vectors, so an embedder that produced different vectors for the same text on
// two calls would make every memory look changed.
func TestTheSameTextEmbedsIdentically(t *testing.T) {
	runtimeDir, modelPath, vocabPath := findRuntimeAndModel(t)
	requireInference(t, runtimeDir, modelPath, vocabPath)

	embedder, err := New(Config{RuntimePath: runtimeDir, ModelPath: modelPath, VocabPath: vocabPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if !embedder.Available(ctx, "project-1") {
		t.Skipf("no working runtime and model: %s", embedder.Diagnostic())
	}
	first, err := embedder.Embed(ctx, "project-1", appmemory.EmbeddingRequest{Texts: []string{"the cat sat"}})
	if err != nil {
		t.Fatal(err)
	}
	// A SECOND text in between, because the tensors are reused and a reader that left the previous
	// text's padding behind would make this one's vector depend on its predecessor.
	if _, err := embedder.Embed(ctx, "project-1", appmemory.EmbeddingRequest{
		Texts: []string{"a completely different sentence that is much longer than the first one"},
	}); err != nil {
		t.Fatal(err)
	}
	again, err := embedder.Embed(ctx, "project-1", appmemory.EmbeddingRequest{Texts: []string{"the cat sat"}})
	if err != nil {
		t.Fatal(err)
	}
	for index := range first.Vectors[0] {
		if math.Abs(float64(first.Vectors[0][index]-again.Vectors[0][index])) > 1e-6 {
			t.Fatalf("the same text embedded differently at %d: %.8f and %.8f",
				index, first.Vectors[0][index], again.Vectors[0][index])
		}
	}
}

// dot is the cosine similarity of two unit vectors, which is their dot product.
func dot(a, b []float32) float64 {
	sum := 0.0
	for index := range a {
		if index >= len(b) {
			break
		}
		sum += float64(a[index]) * float64(b[index])
	}
	return sum
}
