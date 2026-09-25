# ADR-0031 The Speech Adapter, the Chain That Had a Hole in the Middle, and Style References

Status: Accepted
Date: 2026-09-25
Work package: WP-30 (FR-080's 音效建议与生成适配 and the video `references` field)
Supersedes: none
Related: ADR-0027 (the async video adapter), ADR-0028 (suggestions and casting), ADR-0030 (background
music import and the link that could not succeed), PRD FR-080

## Context

Two things were named open and stayed named through three packages:

> **音效 GENERATION**: FR-080 says 「音效建议与生成适配」. The 建议 and the path that accepts one are
> delivered; a real effect provider is adapter work of WP-26's shape. **Style references** have no picker.

Reading the audio side before writing the adapter found a third, larger thing — the same shape WP-29 had
just found twice:

**A TTS JOB'S RESULT NEVER BECAME AN ASSET VERSION.** The bytes were committed, the job was marked
succeeded, and that was the end. `attachAudioClips` joins `asset_usages` on a shot and requires the version
to be the asset's current approved one; **no version existed**, so the speech a user generated never
reached the mix. The only rows of that shape anywhere in the build were written by WP-11's acceptance walk
BY HAND, under a comment saying it writes "the asset, version, file and usage **a TTS job's result
leaves**" — a path that did not exist. `AttachJobResult` had a Wails binding, the audio section listed a
job's `resultFiles`, and nothing joined the two ends.

## Decision

**1. Speech is ONE synchronous protocol, and the adapter is shaped like the image one.**

`AudioPort` has one method where `VideoPort` has four: synthesis answers a request with bytes, video
answers with an identifier. So `openai_audio.go` is a request-then-response adapter (`POST
{base}/audio/speech`), not a submit/poll/fetch/cancel one. The port already stated which this is, and an
adapter that polled anyway would be inventing a protocol the port does not have.

Two response shapes, branched on the CONTENT TYPE: `audio/*` is inline bytes under a bound; JSON carries
`{url}` or `{data}`. A response that is neither is refused, and the refusal names the type rather than the
body — the body of an unexpected response is the thing most likely to carry something that must not travel.

The field names and the path are an ASSUMPTION, recorded as one, exactly as ADR-0027 records the video
protocol's. What is verified is the protocol handling.

**2. `openai_compatible` is reused, and the audit writes `CapabilityAudio` for the first time.**

`AudioPortFor` resolved the mock for `mock_media` and returned `unsupported` for every other kind — WP-26's
sentence verbatim, and the same consequence. The capability column has admitted `'audio'` since migration
000003 and nothing had ever written it, so a query for a project's speech calls found none regardless of how
many were made. That is now false, and a test asserts it.

**3. The bound is the TRANSPORT's ceiling, not a number of this adapter's choosing.**

The same correction WP-26 had to make: a bound larger than the guarded client's 10 MiB response cap is a
bound the client truncates before the adapter can reach it. 8 MiB is that ceiling, and a test feeds it a
9 MiB body — past the adapter's bound and under the client's — so the adapter's own check is what refuses.

**4. A job's result becomes an APPROVED version, through a collector that shares the image batch's
mechanism.**

`CollectAudioJobResults` attaches the result, approves it and records the usage. Approving rather than
leaving a candidate is what the mix needs: the read requires the current approved version, and there is no
audio approval surface for somebody to use later.

It lives beside `CollectBatchResults` because it is the same act over the same service, and a second file
would be a second implementation of "a job's result becomes a version with a role".

**5. The consumer is stated, never defaulted.**

The mix's join is `(consumer_type = 'shot', consumer_id = <a row's shot>)`. A collector that did not
require both would produce a perfectly valid usage row that NO READ FINDS — the defect this package exists
to close, in its data form. The command refuses instead.

**6. Style references are OTHER SHOTS' approved frames.**

The timeline already carries every shot's `mediaHash`, and WP-28's `loadShotFrame` loads one by exactly
that key — so the picker adds no binding. "Make this one look like that one" is what a reference means in
this build; an arbitrary upload is a different capability with its own transfer, its own allowlist and its
own story about where the bytes came from.

Two bounds, because both are real: the COUNT is the binding's own (eight), and the BYTES are this
section's (twelve megabytes) because a job's input is a DATABASE ROW — eight multi-megabyte frames would
make every read of that job expensive.

**7. Accepting a suggestion submits a job; the suggestion itself is still not stored.**

WP-27's ruling stands: a suggestion is a projection and would go stale the moment the intent is edited.
What was missing was not storage but the CONSEQUENCE — accepting one produced no audio at all, because
nothing collected the job's result. The prompt is built from the MATCHED term rather than the effect's
name, so a provider is asked in the language the script is written in.

## Consequences

- 音效的建议 half, its acceptance path, and now its GENERATION are reachable, and a generated effect reaches
  the mix as an effect rather than as a second dialogue track.
- **A line's generated speech now reaches the mix**, which was not this package's stated goal and is the
  more valuable outcome: a shipped capability was producing bytes nothing could hear.
- The chain from a job to the mix is driven END TO END by a test over the real schema
  (`audio_chain_wp30_test.go`), and that walk was verified to be non-vacuous: removing the collector's
  approval step makes it fail with "a generated effect did not reach the mix".
- A compile-time proof was added for the batch port (`var _ desktop.ProductionBatch =
  (*appproductionpipeline.Service)(nil)`). It was missing, and its absence is how WP-29's adapter went
  untested: a port satisfied only by the build's one call site is a port nothing checks.
- **What is NOT proven**: that a real vendor accepts this request body. No authorised call was possible
  (AGENTS §4.3), and STATUS does not claim otherwise.
