# ADR-0015 Media: the One Audited Subprocess, the Export Recipe, and What the Mock Video Really Is

- Status: Accepted (WP-11 scope)
- Date: 2026-09-23
- Deciders: Repository engineering under the approved WP-11 plan
- Related work package: WP-11 (视频、音频、字幕、时间线与导出)

## Context

WP-11 had to turn an approved storyboard into a playable episode: video jobs and their
versions, TTS against dialogue lines, subtitles with a real SRT/VTT export, a timeline, an
MP4 export with a manifest, and the Final Supervisor — over `docs/ROADMAP.md:536-571` (14
scope items), AC-MEDIA-001/002/003 in `docs/ACCEPTANCE.md:650-682`, PRD FR-080 and FR-100,
AGENT_CONTRACTS section 11.4's Final Ruleset, and SECURITY sections 5, 8.4, 9 and 19.

Three things had to be decided before any code, and the user chose the first of each:
a **REAL FFMPEG ADAPTER** rather than a container-format-only deliverable, a **COMPLETE MOCK**
for the video provider (which ROADMAP item 1 permits in as many words), and **REUSE OF THE
ASSET AGGREGATE** for shot video versions rather than a new table.

The rest were implemented on the recorded recommendation, so each ruling below is
**refutable**: a later package that disagrees should supersede this record rather than work
around it.

## Decision

### 1. `os/exec` appears in exactly one file, with structured argv

`internal/infrastructure/media/ffmpeg.go` is the only file in the repository that imports
`os/exec`. Every command is `exec.CommandContext(ctx, path, args...)` where `args` is a
`[]string`; no argument is ever concatenated into a command line, and no shell is involved.

**Ruling.** SECURITY section 5 says it outright: "允许 `os/exec` 的唯一位置是经过审计的
MediaEngine/系统集成 Adapter，参数必须结构化构造，禁止 Shell 字符串拼接." And section 19 lists
"媒体命令通过 Shell 字符串拼接" as a release blocker. Those two sentences together define both the
permission and its shape, so the adapter's job is to be the one place the permission is
exercised and to make the prohibited shape impossible to reach.

**The cost, stated plainly, and it is the largest cost in this package.** This repository had
zero subprocesses before this. Every process it starts is now attack surface: an argument that
a user controls, a file whose path came from a job, an ffmpeg build with a decoder bug. Four
things bound that surface:

- **The command set is closed.** `ffmpeg` and `ffprobe` by name, resolved once by
  `exec.LookPath`, never from configuration or from a request.
- **User-supplied text never reaches argv.** A prompt or a filename goes into a file the
  adapter writes, and the file's path — which the adapter generated — is the argument. `-`
  prefixed arguments are refused, because `-i` and `-y` are exactly the shape an injection
  takes in a tool that parses its own argv.
- **The scanner gate is exact.** `scripts/security-scan.mjs`'s `DYNAMIC_ALLOWLIST` gains one
  entry naming this file, the `os-exec` rule and this ADR. The allowlist refuses wildcards and
  fails on a stale entry, so a second `os/exec` import anywhere — and this ADR's own removal —
  cannot pass silently.
- **The engine fails soft.** `Probe` reports whether an ffmpeg exists; when it does not, the
  export capability is DISABLED with a diagnostic rather than crashing or pretending. A
  developer machine with ffmpeg on PATH is not evidence about a user's machine.

### 2. An export is composed from approved panel IMAGES, not from shot videos

`MediaEngine.Compose` takes an ordered list of segments, where a segment is an image with a
duration or a video file. WP-11's export feeds it approved panel images with each shot's own
duration.

**Ruling.** The video provider in this build is a MOCK, and the mock's payload is a 24-byte
`ftyp` box — a real container header, and not a decodable video. Feeding that to a concat
demuxer would fail on every episode, so an export built from shot videos would be a feature
that cannot run and a test that would have to be skipped. The panel images are **real PNGs**
(WP-09's mock image renders a decodable image), so "stitch the approved frames in shot order
for their shot durations" produces a genuinely playable MP4 — which is what AC-MEDIA-003's
"output playable" grades.

**Cost, stated.** The first export of a real project is a slideshow of approved frames rather
than moving footage. It is a real MP4 with real timing and real audio and real subtitles, and
it becomes a real film the moment a real video adapter exists — the segment shape does not
change, only which files fill it.

### 3. A locally derived artifact does not go through the provider result pipeline

Subtitles, the export manifest and the exported MP4 are written with `files.Store.Put`
directly. They do NOT go through `jobs.ResultStore`.

**Ruling.** `ResultStore` is "the only component that turns provider bytes into a durable asset
reference" (its own words), and its second step validates the detected content type against a
per-capability allowlist. A subtitle file is `text/plain` by sniffing, no capability accepts
that, and the refusal would arrive as `CategoryResponseInvalid` — a message that says the
provider returned something malformed about a file the provider never touched. The boundary is
therefore: **provider results go through `ResultStore`; locally derived artifacts go through
`Store.Put`.** `file_objects.mime_type` has no allowlist, so the store accepts them, and the
type it records is the type it sniffed.

**Cost, stated.** Two write paths to the same content-addressed store, and a rule a reader has
to know. It is one sentence and it is written at both call sites.

### 4. Shot video versions are asset versions

A shot's video is an `assets` row of type `video`; its versions are `asset_versions`; the link
to the shot is an `asset_usages` row with `consumer_type = 'shot'` and `usage_role = 'video'`;
approval is the existing `ApproveVersion`.

**Ruling.** The user chose this, and the pieces were already the right shape: `asset.Type`
includes `video`, `asset_files.role` already carries `first_frame`, `last_frame` and
`reference`, and `AttachJobResult` — built capability-agnostic in WP-09 — turns a job's files
into a candidate version without knowing what a video is. A `shot_video_versions` table would
be a second version model beside the one the asset aggregate already has, with its own
approval, its own supersede rule and its own partial unique index.

**Cost, stated.** "Which video is approved for shot N" is a join rather than a column, and a
shot's media is discoverable only through `asset_usages`. A reader looking for a table named
after the concept will not find one.

### 5. `video_generation` gets no agent; `final_episode` gets one

`production.execution.video_generation` does not exist. `production.execution.final_episode`
and `production.supervision.final_episode` do.

**Ruling.** ADR-0011 section 6 rules that media generation is a job rather than an agent tool,
quoting AGENT_CONTRACTS section 19: "媒体生成本身由 Job/Provider Service 执行，不让 LLM 阻塞等待大
文件". A video stage agent would be a model deciding to do what the job queue does better, and
its write tool would be a way for a model to spend money. The `video_generation` stage's policy
therefore stays configured and unused — which is honest, and is why this paragraph exists
rather than a silent absence.

The FINAL stage is the opposite case: PRD FR-100 gives it `supervision: true`, AGENT_CONTRACTS
section 11.4 gives it a ruleset, and its artifact — an export recipe — is a decision about
quality and framing rather than a byte stream. So it is an agent, and the deterministic half of
section 11.4's ruleset lives in a checker.

**CORRECTION, made after an independent review.** An earlier version of this paragraph said "its
write tool writes a recipe". The agent has NO write tool: its five granted tools are all reads, and
`schemas/agent/tools/KEYS.txt` carries no recipe-creating tool. What it produces is the model's own
structured output — the recipe in `payload` of `execution-result.v1.json` — recorded on the run and
read at the gate, not persisted as a version of its own family. That is the right shape (a recipe is
a decision inside a run, not an artifact with a lifecycle), but this paragraph described a tool that
does not exist, and a reader checking it would have found nothing.

The same review found something worse in the paragraph below: the deterministic half contributed
NOTHING in a real build, because `final_reader.go`'s statements were written against columns the
schema does not have, so every read errored and the stage machine discarded the error. It is fixed,
with storage-level tests this time — `internal/infrastructure/database/final_reader_test.go`.

### 6. The Final Ruleset's mechanical half is code, merged into the model's report

`internal/application/consistency` gains a final-episode checker with eight rules; its findings
merge into the supervisor's report through WP-10's `MergeIssues`, and `ReviewPassed` lets a
deterministic blocker overrule a happy verdict.

**Ruling.** AGENT_CONTRACTS section 11.4 states both halves: the bullets ("所有必需 Shot 有批准
视频；音频和字幕完整；媒体文件存在；stale/waiver；…") and the rule ("硬规则应尽量用确定性代码先检查
… 并标记 source=deterministic|llm"). Every bullet is a query over stored rows — does this shot
have an approved version, does this file exist, is this mark cleared — so none of them needs a
model, and each answers the same way on every run.

**Cost, stated.** The final stage pays for the checks even when the supervisor would have found
the same faults, and one of the eight — whether a waiver exists — is a rule the SUPERVISOR may
re-raise, per DOMAIN_MODEL section 15.3.

### 7. An export's staleness is a finding, not a staleness mark

`episode_exports.manifest_json` records the version identifiers and hashes every input; the
final checker compares them against what is currently approved and reports a mismatch as a
`review_required` finding.

**Ruling.** DOMAIN_MODEL section 15.2's chain ends "… → Video/Audio → Timeline/Export", and
`artifact_staleness.artifact_type` is a closed CHECK with nineteen values and no media node.
Extending it means rebuilding a table from migration 000012 — a published migration — for a
node whose only consumer is one finding. The manifest the criterion already requires
("导出清单可追溯") carries exactly the facts the comparison needs.

**Cost, stated.** An export does not appear in the Quality Center's staleness list; a reader
who expects it there finds a finding in the final review instead.

### 8. The manifest travels inside the export row, not in the archive's own manifest

`episode_exports.manifest_json` is the export manifest. The ZIP the export can be bundled into
uses `infrastructure/archive`'s `manifest.json` for the ARCHIVE's own entries, and the two are
different documents.

**Ruling.** They answer different questions: the archive manifest says what bytes are in a zip,
and the export manifest says which VERSIONS of which artifacts the episode was made from. A
reader tracing a frame back to the script version that produced it needs the second, and
presenting it as the first would be a name collision rather than a shared design.

**Cost, stated.** Two things called a manifest in one package. Named at both call sites.

### 9. The user receives a file through a save dialog, and the path never comes from a request

`MediaBinding.SaveFile` opens Wails' `SaveFileDialog`, then streams the stored object to the
path the dialog returned. The request names a storage key and a suggested filename; it cannot
name a destination.

**Ruling.** SECURITY section 11 allows writing only where the user pointed: "用户选择导出目录时
只写明确目标". And the reconnaissance for this package found the gap this closes: of the 184
binding methods the build had, **not one could write a file to a location the user chose** —
`ExportBackup` returns base64 and had no frontend caller, and every other "save" in the app is a
browser download from `file-saver` reading browser-local storage rather than the Go store.

**Cost, stated.** The first native dialog in the application, and therefore the first code path
that depends on the desktop shell being present: in a browser the export is unavailable, which
the section says rather than showing a button that cannot work.

### 10. `ReadResultFile` is not the export path

The export hands the user a file through `SaveFile`, never through the data URL reader.

**Ruling.** `ReadResultFile` materialises the whole object into a base64 data URL inside a JSON
message and caps it at 64 MiB. A two-minute 1080p MP4 exceeds that, so an export built on it
would fail on every real episode with an error that reads like a size limit rather than a size
limit hit by design.

**Cost, stated.** Two ways to get bytes out of the store, with different limits. The reader
stays for previews — a thumbnail, a panel image, a short audio clip — and the adapter's comment
says so.

## Consequences

- One new dependency on an external program, and one scanner allowlist entry naming this ADR.
- Two tables are added by migration 000020; no published migration changes.
- `asset_usages` gains a `usage_role` value (`video`) rather than a column; no migration needed.
- The `video_generation` stage stays configured with no agent, which STATUS section 0l lists.
- The Final Supervisor's mechanical half runs through the same merge WP-10 built, so a final
  report carries both kinds of evidence with the `source` mark.

## Verification

- `internal/infrastructure/media/ffmpeg_test.go` — argv is structured; a metacharacter in a
  user string arrives as one argument; a `-` prefixed argument is refused; a missing ffmpeg
  disables the engine rather than failing a call.
- `internal/infrastructure/media/compose_test.go` — the export is a real film: stills become a
  playable MP4, audio becomes a second stream, a sidecar track becomes a third, and a BURNED-IN
  track is drawn into the picture (equal stream count to the silent composition, which is what
  says the filter ran rather than being skipped).
- `internal/domain/media/*_test.go` — timecode round trips, cue validation, manifest shape.
- `internal/infrastructure/database/acceptance_wp11_test.go` — AC-MEDIA-003, as one walk.
  **AC-MEDIA-001's clauses** are graded by `internal/application/jobs` and
  `internal/infrastructure/jobs` (WP-03's runner and its restart test); **AC-MEDIA-002's** by
  `internal/infrastructure/database/subtitle_wp11_test.go` and `internal/domain/media/subtitle_test.go`.
  An earlier version of this list attributed all three criteria to the acceptance file alone, which
  was wrong in a way a reader would have discovered by looking.
- `internal/infrastructure/database/final_reader_test.go` — the Final Ruleset's own adapter against
  the real schema, which is the test whose absence let §11.4's eight clauses run against columns that
  do not exist.
- The export's playability is asserted with `ffprobe` when one is available, and the test
  reports a SKIP rather than a pass when it is not.

## What an independent review corrected, and when

Two defects in this work package were found by review rather than by its own tests, and both are
worth recording because each was a case of a comment standing in for a proof:

1. **The Final Ruleset was inert.** `final_reader.go` selected columns the schema does not have
   (`storyboard_versions.episode_id`, `dialogue_lines.script_version_id`, `artifact_staleness.id`,
   `script_versions.episode_id`), so every read errored, and `stagepipeline`'s `if err == nil`
   discarded the error. Fixed; `final_reader_test.go` now drives the adapter over the real schema, and
   the comment in `final_test.go` that claimed such a test existed has been corrected to say what is
   true.
2. **Burn-in subtitles failed on Windows.** `escapeFilterPath` escaped the drive letter's colon and
   returned the value unquoted, which ffmpeg rejects — the filtergraph reads `C` as an option name.
   The test that covered the function asserted on the string's SHAPE and never ran ffmpeg, and the
   only subtitle compose test used sidecar mode, which takes a different branch.
   `TestBurnedInSubtitlesComposeADecodableFilm` is the test that runs it.

## References

- `docs/SECURITY.md` sections 5, 8.1, 8.4, 9.3, 11, 18, 19; `docs/ARCHITECTURE.md:1058`; AGENTS.md
  特别禁止.
- ADR-0004 (job lifecycle, poll pacing, download policy); ADR-0005 (identifiers, the physical
  file table); ADR-0011 section 6 (media generation is not a tool); ADR-0013 section 6 (a
  registered but unreachable arm); ADR-0014 (the deterministic checks and their merge).
- `docs/ROADMAP.md:536-571`; `docs/ACCEPTANCE.md:650-682`; PRD FR-080, FR-100, FR-150;
  AGENT_CONTRACTS section 11.4; DOMAIN_MODEL sections 15.2, 15.3, 17.
