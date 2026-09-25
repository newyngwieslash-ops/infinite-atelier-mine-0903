# ADR-0028 Multi-character Voice Casting, Effect Suggestions, and Two Rules No Code Could Reach

Status: Accepted
Date: 2026-09-25
Work package: WP-27 (P3 item 23's remaining two halves)
Supersedes: none
Related: ADR-0024 (the audio mix and the silent film), PRD FR-080's V1 audio list, PRD §16's v1.0 scope,
STATUS §0y (which named these two as still open)

## Context

WP-20 delivered FR-080's 简单混音 and left two clauses of the same V1 list open, named rather than
implied:

> **多角色声线映射 and 音效建议 are still open and named**: the voice travels per submission and is stored
> nowhere, and nothing suggests an effect.

Reconnaissance confirmed both, and found a third thing neither sentence says:

| Fact | Evidence | Consequence |
|---|---|---|
| The voice travelled and was stored nowhere | `media_jobs.go` puts `voice` into `audioJobInput`; `jobs/ports.go`'s `AudioRequest.Voice`; `runner.go` forwards it. **Nothing SELECTs it back.** | "Character X speaks with voice Y" was not a fact in this application — it was a value retyped per line |
| The UI had ONE voice per project | `audio-view.tsx` sent `config.audioVoice`; the locale note said 取自项目的音频设置，本分区不另设一份 | A project with two characters sounded identical |
| The role was a HARDCODED argument | `buildMix` passed `AudioRoleDialogue` for every clip, from a read returning only version ids; its `music` slice was initialized empty and never appended to | `DefaultGainFor(AudioRoleMusic)`'s 0.35 was unreachable — an imported bed would have mixed at unity |
| `AudioMix.Normalized()` had NO CALLER | The method existed with its own test, documented; `grep` found no non-test call site | Every clip reached the engine with `Gain == 0` → `volume=0` → **an imported bed would have been SILENT** |
| `shots.audio_intent` was authored and never read | Migration 000008 creates the column; the script agent writes it; the column is SELECTed back into the domain — and no code reads its CONTENT | The input for 音效建议 had existed since WP-08 |
| `story_entities.current_profile_version_id` is a dangling reference | Created in migrations 000007/000014; **no profile table exists** | A voice could not be attached to a character profile |

## Decision

**1. The casting is a TABLE keyed on the character, not a settings field.**

`character_voices(project_id, character_entity_id, provider_config_id, model, voice, …)` with a UNIQUE
index on (project, character) and a foreign key to `story_entities` `ON DELETE CASCADE`.

The mapping is a SET — one entry per character — so a single `project_settings` row cannot hold it. A
JSON column inside that row was the alternative and was rejected: it would make "what voice does this
character use" unqueryable, unconstrainable, and unable to follow the character's life. The UNIQUE
index is what makes an assignment an UPDATE rather than a second answer, and the cascade means deleting
a character deletes what was said about their voice.

The character is a `story_entities` row because `dialogue_lines.character_entity_id` already names a
line's speaker by that identifier — any other key would need a join to answer "what does this character
sound like".

**2. The provider and model travel WITH the voice.** A voice name is not portable: `alloy` on one
channel and `alloy` on another are different sounds, so a mapping storing the name alone would let a
channel change silently change a performance. Both may be EMPTY, meaning "whatever the project's audio
configuration names".

**3. The resolution has three levels, in the domain, and the SOURCE is returned.**

`ResolveVoiceChoice(mapping, found, projectDefault)` → character, then project, then nothing. It lives
in `internal/domain/media/voice.go` and takes both inputs as ARGUMENTS, so the policy is testable
without a database and a caller cannot accidentally resolve against another project's default.

An INCOMPLETE mapping (a voice with no channel) INHERITS the missing half from the project rather than
resolving to nothing: a user who cast a voice and left the channel alone means "this voice, on whatever
channel I am using". Refusing would make the cast unusable until every channel was chosen; substituting
the project's voice would discard the decision they made.

The `Source` field exists because the three levels are indistinguishable by sound alone: a user who
hears the project default on a character they cast needs to see that the cast is not what decided.

**4. The project default is passed IN, not read.** It is the user's audio configuration, which is not in
the drama schema — it is a preference the desktop layer holds. Keeping the two apart is what stops a UI
setting from becoming a row whose foreign keys are story entities.

**5. Effect suggestions are a pure function over `audio_intent`, and they name their evidence.**

`SuggestEffects` reads each shot's authored intent and matches it against a lexicon, returning at most
ONE suggestion per shot with the TERM that produced it.

Why a lexicon rather than a model: a suggestion a user cannot check is one they cannot trust, and "the
model gave it 0.72" is not checkable. `Matched` is what makes it checkable — a user reading 「雨声 ← 雨」
sees immediately whether this application understood the shot, and a wrong suggestion becomes a phrase
to add to the lexicon rather than a mystery. The score is the matched term's RUNE LENGTH and orders
competing suggestions; it is NOT a confidence and is not presented as one.

One per shot because a shot is a sound CUE: an intent mentioning rain and footsteps describes a scene,
not three requests, and the most specific match wins (「积水」 over 「水」). Determinism is required — the
output is ordered by ordinal, ties break on term length then lexicon order, and no map iteration is
involved — because a panel that reordered itself between loads would be unusable.

**6. A suggestion is NOT stored.** It is a pure function of the script, so storing it would create a
second answer that goes stale the moment the intent is edited. Accepting one submits an audio job
carrying `usage_role = 'audio_effect'`, and THAT is the fact, because it names a file and a version.

**7. The usage role travels to the mix, and the mixer's role is derived from it.**

`BoardRow.AudioVersionIDs []string` became `AudioClips []AudioVersionRef{VersionID, Role}`. The role was
a hardcoded argument at the call site; carrying it makes a mix that forgot which clip is music
unrepresentable rather than merely wrong.

`audio_dialogue`/`audio_music`/`audio_effect` are the values, and the mapping is TOTAL: an unrecognised
role — including the legacy bare `audio` WP-11's own fixture writes, and the schema's `reference`
default — maps to DIALOGUE. That direction is deliberate rather than convenient: every pre-WP-27 row was
speech (speech is all the mix could carry), so treating one as anything else would CHANGE WHAT AN
EXISTING PROJECT EXPORTS.

**8. `buildMix` normalizes the mix, and orders it beds → effects → dialogue.**

The normalization is the second half of the defect above: without it every gain was zero. The ORDER is
not about the sound (`amix` is symmetric) — it is about a failure naming an input number a reader can
map back, and about the mix reading the way channels do.

## Consequences

- A project can now cast a different voice per character, and a line's voice is resolved rather than
  retyped. The audio section shows which of the three levels decided.
- An imported bed mixes at 0.35, and an effect at unity — because the role reaches the mix and the gain
  is normalized. Neither was reachable before this package: **the music default was unreachable because
  the role was hardcoded, and EVERY default was unreachable because nothing called `Normalized`**.
- `BoardFacts` now issues TWO queries for a board rather than one. The `group_concat` it replaced could
  carry one fact per version (the id) and could not carry two without inventing a separator, escaping
  ids against it, and relying on an aggregate's order — three fragile things where a column exists. Two
  queries for a whole board is still not one per row, which is the property the original comment
  protected.
- The lexicon is a PRODUCT judgement encoded as data, readable and one line to extend. It is the part
  of this package most likely to need growth, and `Matched` is what tells a user which line to add.
- **`story_entities.current_profile_version_id` remains a dangling reference.** Building a character
  profile table is a different package, and attaching the voice to one would have been a larger change
  than the clause requires.

## What this package does NOT close

- **背景音乐导入's UI control**: `AudioRoleMusic` is reachable through the mix and has no way for a user
  to point at a music file. STATUS §0y named it and this package did not touch it — it is not one of
  the two clauses item 23 lists as open.
- **音效 GENERATION**: FR-080 says 「音效建议与生成适配」. This delivers the 建议 and the path that
  accepts one; a real effect provider is adapter work of the same shape as WP-26 and belongs in its own
  package. Recording the distinction rather than counting the clause as complete.
