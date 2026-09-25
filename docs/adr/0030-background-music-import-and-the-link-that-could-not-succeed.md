# ADR-0030 Background Music Import, the Bed That Started Eight Seconds Late, and the Link That Could Never Succeed

Status: Accepted
Date: 2026-09-25
Work package: WP-29 (FR-080's 背景音乐导入)
Supersedes: none
Related: ADR-0024 (the audio mix and the silent film), ADR-0025 (the MONOFORM snapshot), ADR-0028 (voice
casting and effect suggestions), PRD FR-080's V1 audio list, STATUS §0y (which named this as open)

## Context

FR-080's V1 audio list is 「音效建议与生成适配；背景音乐导入；简单混音；多角色声线映射」. Three of the four
were delivered by WP-20, WP-27 and WP-27 again, and `AudioRoleMusic` mixes at a documented 0.35. What a
user could not do was **point at a file**: STATUS §0y recorded it in as many words, and the timeline
section had no control that could produce an `audio_music` usage.

Building the control found **three defects**, two of which were in code that already shipped.

## Decision

**1. The import is its own transfer, with its own ceiling and its own allowlist.**

`MusicImportBinding` runs Begin → Append (bounded chunks) → Finish, with the size verified rather than
trusted. It does not reuse `ImportUploadBinding`, whose subject is a DOCUMENT: it parses the bytes into
chapters and a script, and its ceiling is the import domain's input limit. A music file is never parsed,
it is stored and played, so sharing that path would have meant a track passing through a parser that
would refuse it.

The allowlist is the types the file store's SNIFFER can actually produce — `audio/wave`, `audio/mpeg`,
`application/ogg` — rather than a list of audio formats in general. An allowlist entry the sniffer never
emits is a DEAD RULE: it protects nothing and hides which payloads are genuinely accepted, which the job
pipeline's own allowlist documents as a trap. The check runs against the sniffed type, so a file renamed
to `.mp3` is stored as what it actually is.

**2. A bed is attached to a SHOT, and the choice is about which mix carries it.**

The obvious design was a `project_style` usage — a bed belongs to an episode, not a shot. It was
rejected after reading the read: `attachAudioClips` joins `asset_usages` WHERE
`consumer_type = 'shot' AND consumer_id = <a row's shot>`, so a usage recorded against anything else is a
**perfectly valid row that no read finds** — the "interface with no real path" shape in its data form.
The binding therefore requires a shot id and REFUSES without one, and the UI passes the episode's first
shot.

The placement does not depend on which shot: `buildMix` starts a bed at zero wherever it was attached.
The attachment says which mix carries the music; where it begins is the mixer's.

**3. The five steps run in the order that leaves the aggregate consistent at every point.**

Store the bytes, create the asset, attach the file as `primary`, approve the version, record the usage.
A version approved before its file exists is a version the export refuses; a usage pointing at an
unapproved version is invisible to the mix's join. A failure leaves the earlier steps done rather than
rolling back — the store is content-addressed so a retry reuses the object, and an asset with a draft
version is a row a user can see and delete. Rolling back across the store and the aggregate would need a
transaction neither has.

`primary` rather than another role because that is the role the audio reader selects: under any other
role the bytes are stored and never played.

**4. The UI control is a file input in the timeline section, and the project comes from the episode.**

The timeline section deliberately takes no `projectId` prop — its note explains that every other read and
write there is episode-scoped. The import is its first command needing a project, and the active episode
already names one, so it is derived rather than added as a second prop. The file input's `accept` is a
HINT rather than a check; the core checks the sniffer's answer.

## The three defects building it found

**D1. A MUSIC BED STARTED AT ITS SHOT.** `AudioClip.StartMS` documents that "zero means the beginning,
which is where a music bed starts and where a dialogue clip does NOT" — and `buildMix` passed EVERY clip
the offset of the shot it was attached to. A bed hung off shot 3 of a four-second-per-shot episode began
at 8000ms: the first eight seconds were silent, which is the opposite of what a bed is for.

**No existing test could see it** because every one of them attached its music to the FIRST shot, where
the shot's start is zero and the two answers coincide. A fixture that puts a bed anywhere but the
beginning is what makes the difference exist.

**D2. THE PREVIS SNAPSHOT COULD NEVER WORK IN PRODUCTION.** `asset_files.file_hash` has a FOREIGN KEY to
`file_objects(hash)`. The store adapter called only the filesystem `Import` — which writes the file and
NOTHING else — so `Store` reported success, the table held ZERO rows for the hash, and the `Link` that
follows failed with "the requested asset no longer exists".

**This was a shipped feature.** WP-21's snapshot path performs exactly those two steps, and its own suite
supplies a DOUBLE that records the metadata itself — so every test passed while the real composition
could not complete a snapshot. The music import found it because it performs the same two steps against
the real stack. The fix is in the shared adapter (`DocumentStoring` now stores the row), so both callers
are repaired, and a regression asserts the row for EACH caller.

**D3. THE PRODUCTION ADAPTER THAT DECIDES THE ROLE HAD NO TEST.** The first mutation run reported a
HARNESS ERROR — an anchor matching nothing — and following that anchor led to `musicImporterAdapter` in
the composition root, the code that decides which consumer type a bed is recorded against. It was only
compile-checked: the binding's suite uses a double, and the database walk builds the same rows BY HAND.
A mutation changing `asset.ConsumerShot` to `asset.ConsumerJob` there **survived**.

The consequence would have been D1's shape again: a bed recorded against a job is a valid row no read
finds, the import would report success, and the music would never play.

## Consequences

- 背景音乐导入 is reachable: a user picks a file, and it plays from the top of the film at the bed's gain.
- **The previs snapshot works again**, which was not this package's goal and is the more valuable
  outcome: a feature's own suite passing is not evidence that its composition works.
- The lesson the three defects share is worth stating: **every one of them was invisible to tests that
  supplied their own equivalent of production.** A hand-built `ImageInput` (WP-28), a hand-built audio
  usage (WP-29's database walk), a double that records metadata (WP-21), a hand-passed shot offset (D1).
  The countermeasure is a test that drives the REAL composition root over a real database, which
  `music_wiring_test.go` now does for the music path and should be the model for the next one.
- **Still open**: 音效 GENERATION (FR-080's 生成适配 half — the suggestions and the path that accepts one
  are delivered, a real effect provider is adapter work of WP-26's shape) and style references, which
  have no picker.
