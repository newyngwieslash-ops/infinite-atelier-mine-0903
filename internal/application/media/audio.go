package media

import "strings"

// audio.go is FR-080's V1 audio clauses: 简单混音, 背景音乐导入, 音效建议, 多角色声线映射.
//
// # What the export did before this file
//
// `ComposeRequest.AudioPaths` was a flat list of files, and the adapter CONCATENATED them:
//
//	graph := inputs + "concat=n=" + n + ":v=0:a=1[a]"
//
// The adapter's own comment named the limit: "which is what a timeline of dialogue means before any
// offset work exists. FR-080 puts offsets and mixing in V1". Concretely, two lines of dialogue in one
// episode played one after the other regardless of where their shots were, and a second file was
// appended rather than laid over the first — so a music bed and a line could not both be heard.
//
// # What a mix is
//
// Each clip is one audio file placed at a START and played at a GAIN, over a bed that runs from
// zero. That is the whole of 简单混音: `-filter_complex` with `adelay` for the offset, `volume` for
// the gain, and `amix` to lay them together. The vocabulary below is the part that has to be right
// before ffmpeg is asked anything, and it is a domain type rather than a set of arguments because the
// arguments are the engine's business.

// AudioRole says what an audio clip is FOR, which decides how it is mixed.
//
// FR-080 names three kinds and they are not interchangeable: dialogue has to be intelligible over
// everything else, music must sit under it, and an effect punctuates it. The role is stored rather
// than derived from the file because the same file could serve any of them — a sting used as a
// transition and as ambience is two clips, not one row twice — and because the MIX depends on it.
type AudioRole string

const (
	// AudioRoleDialogue is a spoken line, from a TTS job or an imported recording.
	AudioRoleDialogue AudioRole = "dialogue"
	// AudioRoleMusic is a bed: background music, imported by the user (背景音乐导入).
	AudioRoleMusic AudioRole = "music"
	// AudioRoleEffect is a sound effect, which FR-080 V1 calls 音效建议与生成适配.
	AudioRoleEffect AudioRole = "effect"
)

// AudioRoles lists the documented roles in a stable order.
var AudioRoles = []AudioRole{AudioRoleDialogue, AudioRoleMusic, AudioRoleEffect}

// IsValidAudioRole reports whether a role may be persisted or mixed.
func IsValidAudioRole(value AudioRole) bool {
	for _, candidate := range AudioRoles {
		if candidate == value {
			return true
		}
	}
	return false
}

// The `asset_usages.usage_role` values an audio version is attached as.
//
// # Why the mixer's role and the usage's role are different strings
//
// `AudioRole` is the MIXER's vocabulary and it predates this mapping: it names what a clip is for, and
// the engine's gain default is keyed on it. The usage role is the ASSET layer's, and it is namespaced
// (`audio_…`) because that column carries roles for every consumer — a shot, a panel, a prop — and a
// bare `music` there would collide with whatever a picture's role happened to be called.
//
// # Why the legacy value maps to dialogue
//
// Rows written before this package carry `usage_role = 'audio'` — the value WP-11's walk writes — and
// some carry the schema's default `'reference'`. Treating either as anything but dialogue would
// CHANGE WHAT AN EXISTING PROJECT EXPORTS, which is data damage rather than a feature: every such row
// was a line of speech, because speech was the only thing the mix could carry. Dialogue is the
// identity element here, and the forward-compatibility rule is that an unknown role is dialogue too.
const (
	// UsageRoleAudioDialogue is a spoken line. It is also what the legacy `audio` value means.
	UsageRoleAudioDialogue = "audio_dialogue"
	// UsageRoleAudioMusic is an imported bed.
	UsageRoleAudioMusic = "audio_music"
	// UsageRoleAudioEffect is a sound effect.
	UsageRoleAudioEffect = "audio_effect"
)

// AudioRoleForUsage maps a persisted `usage_role` into the mixer's vocabulary.
//
// It is total: every input has an answer, because the export cannot refuse a row it has already
// decided to compose — a role it did not recognise would otherwise drop a clip and produce a film
// that is quietly missing a line. An unrecognised value is DIALOGUE, which is the identity element
// and the meaning every pre-WP-27 row carries.
func AudioRoleForUsage(usageRole string) AudioRole {
	switch strings.TrimSpace(strings.ToLower(usageRole)) {
	case UsageRoleAudioMusic:
		return AudioRoleMusic
	case UsageRoleAudioEffect:
		return AudioRoleEffect
	default:
		// Covers UsageRoleAudioDialogue, the legacy bare `audio`, the schema default `reference`, and
		// anything a later package might write. See the block comment above for why this is the safe
		// direction rather than a silent guess.
		return AudioRoleDialogue
	}
}

// UsageRoleForAudio is the inverse, for the command that attaches an audio version.
//
// The inverse is written here beside the forward map so the two cannot drift: a round trip is asserted
// in the tests, and a change to one that forgot the other would fail it.
func UsageRoleForAudio(role AudioRole) string {
	switch role {
	case AudioRoleMusic:
		return UsageRoleAudioMusic
	case AudioRoleEffect:
		return UsageRoleAudioEffect
	default:
		return UsageRoleAudioDialogue
	}
}

// DefaultGainFor is the mixer's starting level for a role, in ffmpeg's volume units.
//
// # Why music defaults below dialogue
//
// It is the one mixing decision this build makes FOR a user, and it is the one every editor makes the
// same way: music at full level under a spoken line makes the line unintelligible, and a user who has
// just imported a track has no reason to know that. 0.35 is a bed, not a floor — low enough that
// dialogue reads over it and high enough to be heard. Dialogue and effects default to unity because
// nothing in this build should quieten a performance the user approved.
//
// The value is a DEFAULT, not a rule: a clip's own gain overrides it, and a user who wants the music
// loud sets it loud.
func DefaultGainFor(role AudioRole) float64 {
	if role == AudioRoleMusic {
		return 0.35
	}
	return 1
}

// AudioClip is one piece of sound placed on the timeline.
//
// It is a VALUE the application builds and hands to the engine, and the engine is the only thing that
// turns it into arguments. `Path` is a filesystem path the adapter resolved from the file store and
// is never a request field: ADR-0015 section 1 records that user text travels in a file rather than
// in argv, and this type is one of the boundaries where that rule is visible.
type AudioClip struct {
	Role AudioRole
	Path string
	// StartMS is where the clip begins in the finished film. Zero means the beginning, which is
	// where a music bed starts and where a dialogue clip does NOT — a line's offset is its shot's
	// start, which the export service computes from the timeline.
	StartMS int
	// DurationMS is how long the clip plays, or zero for its own length. It is separate from
	// StartMS because trimming is a different act from placing: a user who wants the last two
	// seconds off a music bed changes this and not the start.
	DurationMS int
	// Gain is the clip's level, in ffmpeg's volume units where one is unity. Zero is NOT silence
	// here — it is "the caller stated nothing", which `Normalize` turns into the role's default.
	// Silence is a gain that is actually zero, and the two would be indistinguishable without a
	// separate way to say "unset" — which is what the pointer-free convention and `Normalize` give.
	Gain float64
	// Label names the clip in the engine's error output, for a failure a reader can act on. It is
	// a line or shot identifier, never a filename.
	Label string
}

// Validate checks one clip before anything is composed.
func (c AudioClip) Validate() error {
	if !IsValidAudioRole(c.Role) {
		return InvalidError("An audio clip must be dialogue, music or an effect.")
	}
	if strings.TrimSpace(c.Path) == "" {
		return InvalidError("An audio clip must name the file it plays.")
	}
	if c.StartMS < 0 {
		return InvalidError("An audio clip cannot start before the beginning.")
	}
	if c.DurationMS < 0 {
		return InvalidError("An audio clip cannot have a negative duration.")
	}
	if c.Gain < 0 {
		return InvalidError("An audio clip's gain cannot be negative.")
	}
	return nil
}

// Normalize returns the clip with its unset gain replaced by its role's default.
//
// It is a separate step from validation because it CHANGES the value, and a function named Validate
// that quietly rewrote its receiver would be a surprise. The mix path calls this once, before
// building anything.
func (c AudioClip) Normalize() AudioClip {
	if c.Gain == 0 {
		c.Gain = DefaultGainFor(c.Role)
	}
	return c
}

// AudioMix is the sound laid over a film.
//
// It exists instead of a `[]string` of paths because the list had no way to say WHEN a file played or
// HOW LOUD, which is the whole of mixing. `Clips` is ordered by the caller and the engine preserves
// that order in its arguments, so a failure's numbering is stable and a reader can map it back.
type AudioMix struct {
	Clips []AudioClip
}

// Validate checks every clip and the mix as a whole.
func (m AudioMix) Validate() error {
	for _, clip := range m.Clips {
		if err := clip.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Normalized returns the mix with every clip's gain resolved.
//
// `HasSound` is not a field: an empty mix is a silent film, which FR-080 permits and which an episode
// with no TTS yet produces. The engine's arguments differ for it — there is a documented ffmpeg trap
// in the single-stream path — so the check is `len(Clips) == 0` at the call site rather than a flag
// two places could disagree about.
func (m AudioMix) Normalized() AudioMix {
	clips := make([]AudioClip, 0, len(m.Clips))
	for _, clip := range m.Clips {
		clips = append(clips, clip.Normalize())
	}
	return AudioMix{Clips: clips}
}

// maxMixClips bounds how many audio clips one composition may carry.
//
// ffmpeg opens one INPUT FILE per clip and mixes them in one filtergraph, so the count is both a
// process-argument limit and a memory limit. Sixty-four is far above an episode's dialogue — a
// five-minute scene with a line every ten seconds is thirty — and far below what a hostile or
// buggy caller would produce. The bound is checked by the export service before the engine is
// called, so a refusal names the count rather than arriving as an ffmpeg parse error.
const maxMixClips = 64

// MaxAudioClips exposes the bound to the application layer, which checks it where it builds the mix.
func MaxAudioClips() int { return maxMixClips }
