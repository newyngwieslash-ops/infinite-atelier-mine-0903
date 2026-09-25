# ADR-0024 The Audio Mix, the Silent Film It Closed, and Why the Measurement Moved

Status: Accepted
Date: 2026-09-25
Work package: WP-20 (P3 item 23)
Supersedes: none
Related: ADR-0015 (the audited subprocess and the export recipe), PRD FR-080's audio clauses,
AC-MEDIA-002, AC-MEDIA-003

## Context

PRD §16 lists 更完整的时间线、音效和混音, and FR-080 splits audio into what the MVP has (TTS for
dialogue, narration, imported audio files, basic volume and start/end times) and what V1 adds:

- 音效建议与生成适配;
- 背景音乐导入;
- 简单混音;
- 多角色声线映射.

The MVP clauses were marked delivered. Reconnaissance established what "delivered" meant, and one
finding was worse than a missing feature:

**THE EXPORT PRODUCED A SILENT FILM.** `ComposeRequest.AudioPaths` existed, but
`ExportService.compose` never passed it: the request carried segments and a subtitle path and no audio
at all. So an episode with approved dialogue composed a film without it, while `TimelineShot.HasAudio`
reported that the episode had audio. The acceptance walk recorded the silence as a known limit in its
own comment rather than as a bug, which is how a gap survives three packages — the walk asserted
"at least two streams" and was satisfied by the subtitle track.

Two more findings shaped the work:

**THE ADAPTER CONCATENATED RATHER THAN MIXED.** `AudioPaths` was a flat list and the adapter's filter
was `concat=n=N:v=0:a=1`, with its own comment naming the limit: "which is what a timeline of dialogue
means before any offset work exists. FR-080 puts offsets and mixing in V1". Two files played one after
the other rather than together, and every line played where the previous one ended rather than where
its shot is.

**`BoardRow.AudioApproved` THREW AWAY WHAT THE QUERY FOUND.** The SQL computed a COUNT of approved
audio versions and the scanner reduced it to a boolean, so the export could not know WHICH versions to
compose even if it had wanted to. The comment claimed "the export reads the actual files through the
same join" — a join that did not exist.

## Decision

**1. `AudioMix` replaces `AudioPaths`: a clip has a ROLE, a START, a GAIN and a LABEL.**
A flat list cannot say when a file plays or how loud, which is the whole of mixing. The role is stored
rather than derived because the same file could serve as a transition sting and as ambience — two
clips, not one row twice — and because the default gain depends on it.

**2. Music defaults to 0.35 and everything else to unity.** The one mixing decision this build makes
for a user, and the one every editor makes the same way: a bed at full level under a spoken line makes
the line unintelligible. It is a DEFAULT, not a rule — a clip's own gain overrides it — and the "unset"
value is zero with `Normalize` as a separate step, so "the caller stated nothing" and "silence" stay
distinguishable.

**3. The mix is one `filter_complex`: `adelay` for placement, `volume` for level, `amix` to layer.**
Two flags in it are load-bearing rather than cosmetic:

- **`normalize=0`.** amix's default NORMALISES, dividing each input by the number of inputs, so three
  dialogue lines would each play at a third of their level and the mix would get QUIETER as the episode
  got busier. Turning it off is what makes a clip's gain mean what it says.
- **`duration=longest`, and NO `-shortest`.** The concat version passed `-shortest`, which ends the
  output when the shortest input ends. A dialogue clip is usually shorter than the film, so that flag
  would truncate a one-minute episode to its last line. The film's length is the picture's.

**4. Every value in the graph is an index, a number or a constant.** No path and no user text reaches
it: the paths are `-i` arguments checked by `checkPathArgument`, and the numbers are integers computed
here. That confines the adapter's one escape-hatch expression — the subtitle filter's — to the place
that has `escapeFilterPath` for it.

**5. `BoardRow` carries the audio VERSION IDS and the timeline carries each shot's `StartMS`.**
The ids because the export needs the files; the start because a dialogue clip belongs at its shot's
start, and the timeline already computes the running total when it places the cues. A second
implementation of "where does shot six begin" is a second answer, and the two would eventually
disagree about the film.

**6. `ExportService` refuses rather than composing a silent film when it cannot read audio.**
`AudioFileReader` is a required port for an episode with dialogue. A build without it gets a refusal
naming the situation, because reproducing the silent-film defect quietly would be worse than failing.

**7. The loudness measurement lives in the ADAPTER, not in the tests.**
`FFmpegEngine.MeanVolumeDB` runs `volumedetect` and parses its stderr. See "Alternatives rejected".

## The defect this uncovered, and how

Wiring the mix exposed a defect that only a real composition could show:

**NAMING ANY `-map` DISABLES FFMPEG'S AUTOMATIC STREAM SELECTION.** The export's subtitle sidecar used
to be picked up implicitly. Adding `-map 0:v:0 -map [mixed]` for the mix silently dropped it — the
acceptance walk failed with "the film carries 2 streams" where three were expected. The fix maps the
subtitle input explicitly, and the walk's assertion was strengthened from "at least two streams" to
"three", because the count is what makes either defect visible.

That is the second time in this package's history that composing a REAL film found what argument
inspection could not, and it is why `TestTheMixPlacesAndLevelsItsClips` measures rather than asserts.

## Consequences

- An episode with approved dialogue now exports a film that carries it, and the acceptance walk asserts
  three streams: picture, subtitle, sound.
- Dialogue lands at its shot; a music bed and a spoken line can both be heard; a bed sits under the
  dialogue without being asked twice.
- The manifest gains a reference per composed audio clip, so the sound is traceable the same way the
  picture is.
- `MaxAudioClips` (64) is exposed to the application layer and checked there, so a refusal names the
  count rather than arriving as an ffmpeg argument limit.
- **多角色声线映射 is NOT built and is named as open.** `AudioRequest.Voice` already travels per TTS
  submission, so a voice IS chosen per line; what does not exist is anything that STORES "character X
  speaks with voice Y" — a user retypes it for every line. That is a mapping table and a picker, not a
  mix, and it is recorded in STATUS §0y rather than implied by this ADR's title.
- **音效建议与生成适配's "建议" half is NOT built**: `AudioRoleEffect` exists and mixes, but nothing
  SUGGESTS an effect for a shot. The role is the vocabulary a suggestion feature would use.

## Alternatives rejected

**Widening the security scan's `os/exec` allowlist so the tests could drive ffmpeg.** REJECTED, and the
scan is what raised it: two test files matched the `os/exec` rule and `verify.sh` FAILED. SECURITY
section 5 permits `os/exec` in exactly one audited file, and the allowlist is deliberately exact —
file plus rule plus owner plus reason, no wildcards. Widening it for tests would defeat the rule it
exists for. The measurement moved into the adapter instead, which is also where a future feature would
want it.

**`astats` with `ametadata=print:file=`, so the measurement reads a file instead of stderr.**
PROBED AND REJECTED: this ffmpeg build fails every variant of it when the output is a null muxer
("Failed to inject frame into filter network"), because the metadata-only chain starves the stream.
`volumedetect` runs cleanly, and `runReadingStderr` is the one addition it needed.

**A Go audio decoder, so the test could measure without ffmpeg.** Rejected: it would be a second
implementation of the thing under test, and it would need a dependency for a test's convenience.

**Translating a loudness level into "is there sound here".** Rejected: that is a mixing decision, and a
threshold invented in a measurement helper would be a product rule nobody chose. The function returns
the number and the caller judges.

**Keeping `AudioPaths` beside `AudioMix` for compatibility.** Rejected: the concat path is exactly the
behaviour being replaced, and a build where both exist is a build where a caller can silently get the
old one. There is one shape and it carries what a mix needs.

## Verification

- `go test ./... -count=1` — PASS, 57 packages. `npm test` — PASS, 113 tests.
- `sh scripts/verify.sh` — PASS, exit 0 (25 Playwright, security scans over 668 files, all fixture
  checks, SBOM, Wails production build).
- **10 mutations, 10/10 killed**, each restored byte-identically. ONE SURVIVED THE FIRST RUN: placing
  every dialogue clip at zero left the suite green, because the acceptance walk's one audible line is
  on the FIRST shot, whose start IS zero. `TestADialogueClipIsPlacedAtItsShot` gives the audio to the
  second shot so the difference is measurable, and the mutation is now killed.
- The mix is measured, not merely asserted: `TestTheMixPlacesAndLevelsItsClips` composes a film from
  two overlapping tones and measures both halves, so a mix that degraded into a concatenation fails.
