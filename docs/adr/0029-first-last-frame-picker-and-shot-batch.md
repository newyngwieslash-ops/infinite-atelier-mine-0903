# ADR-0029 The First/Last-Frame Picker, the Empty Data URL Behind It, and the Shot Batch

Status: Accepted
Date: 2026-09-25
Work package: WP-28 (P3 item 21's remaining two halves)
Supersedes: none
Related: ADR-0027 (the async video adapter and the protocol shape it assumes), PRD FR-080's video
clauses, STATUS §0zd (whose claim this record corrects)

## Context

STATUS §0zd closed WP-26 with an honest-sounding sentence:

> **首尾帧: the PIPE is complete** — `SubmitVideoJob` carries `references`/`firstFrame`/`lastFrame` with
> their MIME pairs, the runner assembles them in FR-080's order, and the adapter encodes them as data
> URLs. **What is missing is a UI picker.**

The first clause was **wrong**, and the way it was wrong is the reason this ADR opens with it. A probe
that submitted a frame the way the RUNNER does — before any UI existed — found that every reference
travelled as:

```json
{"image_url": "data:image/png;base64,"}
```

An empty data URL. The pipe was complete in the sense that it delivered a string from the run's input to
the provider's request, and the string was empty.

## Decision

**1. The reference's bytes are resolved from either field, and an empty one is REFUSED.**

`ImageInput` carries `Data string` (a data URL) and `Bytes []byte`. The runner fills `Data`. WP-26's
adapter read `Bytes` alone. `openai_image.go` and `gemini_image.go` both resolve `Bytes` → `Data` →
refuse, and the video adapter now does the same, with `videoSubmitBody` returning an error so `Submit`
can refuse before a request reaches a provider.

The refusal matters as much as the resolution: a provider handed a zero-byte image either errors with a
message about the image or — worse — generates a film without the frame the user chose, and neither says
which reference was empty.

**2. The defect was invisible because every test built its own input.**

Each reference test constructed `ImageInput{Bytes: …}` directly. **No real caller produces that shape** —
the runner is the only thing that builds one, and it builds `Data`. A test that constructs its own input
tests the adapter against itself. Two regressions now exist and neither is redundant: the adapter's
suite builds the runner's shape by hand, and the jobs suite has the RUNNER build it, so a change to
which field the runner fills fails the second while the first keeps passing.

**3. The picker's source is the shot's OWN approved panel image.**

A frame in this build has an exact source: the timeline row's `mediaHash`, which is the approved panel
image's content-addressed key, and `ReadResultFile` already opens an object by exactly that key. **No new
binding was added.** A call for "give me this shot's frame" would be a second answer to a question the
file store answers, and the second answer is the one that drifts.

A file chooser was rejected: "any image at all" is the style-reference capability, which is a different
feature and is still unoffered.

**4. A frame that cannot be loaded is a STATE, not an error — and never an empty string.**

The UI loads the frame when a shot is selected. A shot with no approved media, or a frame that cannot be
read, disables the controls and says which. What it never does is send `firstFrame: ""`: on the wire an
empty value and a real absence are indistinguishable, and the provider reads the first as a zero-byte
image — the defect above, in its other form.

**5. A batch is a LOOP over the single command, not a new job type.**

A provider's video API generates one clip per request — ADR-0027's protocol has no "several shots" form —
so a batch is several submissions. A job type meaning "many" would be a second state machine with its own
retry, cancellation and idempotency semantics, and what a user wants from a batch is one control and one
report.

`submitOneVideo` is shared with the single command **because the idempotency key is built from the
scope**: a batch that marshalled its own input would produce a different key for the same request, and a
user who submitted one shot, then the batch containing it, would get two jobs.

**6. One shot's failure does not undo another's, and the report is per item.**

Aborting on the first refusal would leave the earlier submissions RUNNING under a report that said the
batch failed. The result is `submitted` and `refused` as separate lists, so a caller renders "a queue to
watch" and "a message to read" without inspecting every row. The same ruling `RunImageBatch` made.

**7. The bound is SIX, below the image batch's eight, and the refusal names it.**

One video is billed by the second of footage and takes minutes to produce; an image is a single render.
The refusal carries the number rather than the generic "the request was invalid", because the UI holds a
copy of it to render its hint — and a refusal that names the real bound is what turns a drifted copy into
a message a user can act on rather than a silently shortened batch once.

**8. The batch goes through the same safe-message conversion as every other command.**

`safeBatchMessage` calls `toAppError`, which classifies a provider failure into the application's taxonomy
and never lets a raw error's text reach a user. A batch that rendered `err.Error()` would be a second
implementation of the redaction rule with a second set of leaks to miss — and a batch report is a LIST,
so a leak would repeat once per failed row.

## Consequences

- The first/last-frame capability is **reachable**: a user picks the shot's approved frame, and the bytes
  the provider receives are the bytes of that image.
- **§0zd's "the PIPE is complete" sentence is corrected in place**, because a reader who found it and
  stopped looking is exactly how this defect survived one package.
- The batch is bounded, per-item reported, idempotent with the single command, and refuses an empty
  selection rather than reporting "0 of 0" as a success.
- **What is still NOT delivered**: style references (`references`) have no picker, and the size field has
  no control. Both are the binding's to accept and this section's to leave out, and the section says so.
- **The frame travels inside the job's input JSON**, base64-encoded, and there is deliberately no new byte
  bound: `SubmitVideoJob`'s existing bound is a COUNT (eight references), and the UI can only produce one
  approved panel image per shot. A caller building its own payload is bounded by the count and by the
  fact that the bytes come from a read capped at 64 MiB.
