# ADR-0026 Local ONNX Embeddings, and the Model That Cannot Read the Corpus

Status: Accepted
Date: 2026-09-25
Work package: WP-23 (P3 item 17)
Supersedes: none
Related: ADR-0014 (the memory store and the deterministic embedder), PRD FR-120, PRD §16's v1.0 list,
AGENTS §6 (dependency vetting), SECURITY §15 (SBOM)

## Context

PRD §16 lists 本地多语言 ONNX Embedding, and FR-120's necessary rules state the requirement it serves:
「Embedding Provider 可替换；本地模式不得在未授权时上传项目文本」. The build shipped a deterministic
feature-hash embedder and a provider-backed one, and the comment above `projectEmbedder` recorded the
local path as deferred in as many words — "nothing registered it, and no project could name it".

Every piece of this package was probed before it was written, and **three probes changed the plan**:

1. **THE OFFICIAL ONNX RUNTIME RELEASE DOES NOT LOAD ON THIS HOST.** `onnxruntime.dll` 1.20.1 from
   Microsoft's release page fails with win32 error 126. The Python wheel's build of the SAME VERSION
   loads. The cause is in the PE import table, not in a guess: the official build imports
   `api-ms-win-core-path-l1-1-0.dll`, which this host lacks, and the wheel's build does not.
2. **THE BINDING VERSION MUST PAIR WITH THE RUNTIME.** `yalue/onnxruntime_go` requests an ORT **API
   base** version: v1.20.0 wants 22, v1.19 and v1.18 want 21, and **v1.17.0 wants 20** — which is what
   the loadable runtime provides. A newer binding is not a better binding here.
3. **INFERENCE WORKS, AND WAS PROVEN BEFORE THE ADAPTER EXISTED**: three int64 inputs →
   `last_hidden_state [1,5,384]` → attention-masked mean pooling → a unit vector, and then real
   semantic signal: `cat~kitten` 0.6169, `cat~dog` 0.5572, `cat~stock` 0.0882.

And one probe measured the limit that shapes this ADR's honesty: **the model this host can reach is
English-only.** `all-MiniLM-L6-v2` covers 11 of the canary corpus's 27 Chinese characters, and the ban
line 「女主不能穿红色，这是全剧的禁令。」 tokenises to **ten `[UNK]` pieces out of sixteen**.

## Decision

**1. The adapter is real, and the multilingual claim is NOT made.** PRD §16's item says
「本地多语言 ONNX Embedding」; this package delivers **本地 ONNX Embedding**. What separates them is a
model file — HuggingFace is unreachable from this host (measured) — not a mechanism, and **claiming the
adjective while shipping an English vocabulary would be the one dishonest thing this package could do**.

**2. The runtime is NOT vendored; it is a runtime dependency.** It is 13 MB, platform-specific, and the
two builds are not interchangeable (above). The embedder resolves it from a configured path, the
executable's directory, or the system path, and a MISSING library DISABLES local embedding with a
diagnostic rather than failing a recall — the same fail-soft shape the media engine uses for ffmpeg,
and for the same reason. THIRD_PARTY_NOTICES carries both the binding's MIT text and the runtime's
provenance, and says explicitly that a packager who bundles it must carry onnxruntime's own LICENSE.

**3. The binding is pinned to v1.17.0, and the pin is documented.** A version bump that ignored the API
base would break against the only runtime that loads here.

**4. `Tokenizer` is an interface, and the implementation is WordPiece.** WordPiece because that is what
the verified model uses; an interface because a multilingual model needs SentencePiece or
`tokenizer.json`, and a fork of the embedder would be the wrong seam. What this build ships is honest
about being English-focused.

**5. The environment is initialised ONCE PER PROCESS and that is enforced at package level.**
`InitializeEnvironment` is not idempotent — a second call fails with "already been initialized" — and a
per-embedder flag cannot see another instance's success. **"Already initialized" is treated as
SUCCESS**: the postcondition a caller wants is that the environment is up, and it is up.

**6. `CGO_ENABLED=0` COMPILES.** The binding is cgo, so that configuration excluded every file of the
package and the build failed with a message about constraints rather than about a feature. A
build-tagged fallback (the shape `secretstore` already uses) makes it compile with an embedder that
reports itself unavailable.

**7. Local is tried BEFORE any provider**, in both `Available` and `Embed`, which is what makes
FR-120's 「本地模式不得在未授权时上传项目文本」 a property of the code rather than of the configuration.

## Consequences

- A build with a local model answers every embedding on this machine and never reaches the network.
- A build without one behaves exactly as before: the provider bridge, unchanged.
- A configured-but-unusable model leaves its reason in `LocalEmbedderStatus()` for a settings panel,
  which is what keeps a QUIET loss of the local path from being the failure mode.
- `go test ./...` passes under BOTH `CGO_ENABLED=1` and `CGO_ENABLED=0`, and the ten ONNX tests pass
  against the real model — including two that assert normalisation, determinism and semantic signal.

## Three defects found while building it

1. **The environment is not idempotent**, and the per-instance flag made the second embedder in one
   process report "the runtime could not be loaded" when the truth was "it is already loaded".
2. **`tokenizer.modelPath` was declared and never assigned**, producing `Load model from  failed` — an
   error naming no file.
3. **`GetData()` returns the tensor's LIVE backing array**, so pooling in place mutated the buffer the
   next call read. The probe hit it as four sentences with cosine 1.0000, which reads as "no signal".

## The probe's own defect, which is the one worth keeping

`vocab.txt` ships with **CRLF** endings. Splitting on `\n` alone left a carriage return on every word,
so every lookup missed, every word became `[UNK]`, and four different sentences embedded identically.
`TestACRLFVocabularyLoads` asserts both endings, because **that failure is indistinguishable from a
model problem until somebody reads the bytes** — and blaming a model is the wrong place to look.

## Alternatives rejected

**Vendoring the runtime.** Rejected: it is 13 MB and platform-specific, and the build that works here
is a Python wheel's rather than Microsoft's release — redistributing somebody else's build from this
repository is not a dependency decision, it is a packaging one that belongs with the installer.

**The newest binding version.** Rejected by measurement: it requests an API base the loadable runtime
does not export.

**Claiming the multilingual half with this model.** Rejected: ten `[UNK]` pieces out of sixteen is not
multilingual embedding, and a STATUS line saying "本地多语言 done" would be false.

**A general-purpose tokenizer implementation.** Rejected: it would be a second implementation of
somebody else's algorithm with no way to check it against the original.

**Failing construction when the model is unusable.** Rejected: it would take the whole agent stack down
over a path a user can fix, and the provider fallback behind it is exactly what FR-120's
replaceability is for. The reason is recorded instead, so the fallback is not silent.
