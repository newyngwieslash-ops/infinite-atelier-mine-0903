package main

import (
	"os"
	"strings"
	"sync"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/onnxemb"
)

// embedder_local.go builds the local ONNX embedder from the environment.
//
// # Why the environment, and why that is the honest placeholder rather than a shortcut
//
// The settings surface has no field for a model path yet (WP-23's wiring is the plumbing, not the
// panel), and a 43 MB native model is not something this repository can ship or guess the location
// of. So the configuration arrives as environment variables, which is the shape a packaging step or
// a developer sets once:
//
//	IA_ONNX_MODEL   the .onnx file
//	IA_ONNX_VOCAB   its vocabulary
//	IA_ONNX_RUNTIME an optional library path or directory
//
// # What each absence means, and why they differ
//
// A missing MODEL or VOCABULARY is nil rather than an error: "this build has no local model" is a
// configuration state, and the provider bridge behind it still works — that is the fallback FR-120's
// replaceability is for. A model that IS configured but unreadable is ALSO nil, because refusing to
// compose the whole agent stack over a path a user can fix would take the application down.
//
// WHAT MAKES THE SECOND CASE VISIBLE RATHER THAN SILENT: `localEmbedderStatus` below, which the
// settings surface can read. The failure mode this guards against is the one FR-120 names — a user
// who configured a local model, had it fail, and then had their text sent to a provider without
// being told. Losing the embedder is acceptable; losing it QUIETLY is not, so the reason is kept
// where a reader can find it rather than written to a log this file has no logger for.
func newLocalEmbedder() appmemory.Embedder {
	modelPath := strings.TrimSpace(os.Getenv("IA_ONNX_MODEL"))
	vocabPath := strings.TrimSpace(os.Getenv("IA_ONNX_VOCAB"))
	if modelPath == "" || vocabPath == "" {
		// Not configured. The provider bridge is the path, and nothing is logged: a build with no
		// local model is the ordinary state rather than a problem.
		return nil
	}
	embedder, err := onnxemb.New(onnxemb.Config{
		RuntimePath: strings.TrimSpace(os.Getenv("IA_ONNX_RUNTIME")),
		ModelPath:   modelPath,
		VocabPath:   vocabPath,
	})
	if err != nil {
		// CONFIGURED BUT UNUSABLE, which is the case worth telling somebody about: the user asked for
		// local embedding and will not get it, and any fallback sends their text to a provider they
		// may not have intended. The reason is recorded for the diagnostics surface to read, because
		// the failure mode this guards against is a QUIET loss of the local path.
		recordLocalEmbedderFailure(modelPath, err)
		return nil
	}
	return embedder
}

// localEmbedderStatus is the last construction outcome, for a diagnostics read.
//
// A package-level value rather than a field threaded through every constructor: the embedder is
// built once at composition, and a reader that wants to know why it is absent has no other place to
// ask. It is written once and read rarely, under a mutex because a diagnostics read can race
// composition in a test.
var (
	localEmbedderMu     sync.Mutex
	localEmbedderReason string
)

// recordLocalEmbedderFailure keeps the reason a configured embedder could not be built.
func recordLocalEmbedderFailure(modelPath string, cause error) {
	localEmbedderMu.Lock()
	defer localEmbedderMu.Unlock()
	localEmbedderReason = modelPath + ": " + cause.Error()
}

// LocalEmbedderStatus reports why the local embedder is absent, empty when it is present or was
// never configured.
//
// It exists so the settings surface can say "you configured a local model and it did not load,
// because ..." instead of showing a local-embedding toggle that is silently doing nothing.
func LocalEmbedderStatus() string {
	localEmbedderMu.Lock()
	defer localEmbedderMu.Unlock()
	return localEmbedderReason
}
