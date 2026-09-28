package media

import (
	"encoding/json"
	"strings"
)

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
	// SourceStartMS and SourceEndMS trim the SOURCE FILE (T05): playback uses
	// the [start, end) window of the file rather than its whole length, which
	// is a different act from the playback `DurationMS` cap and the placement
	// `StartMS`. Zero start and zero end mean the whole file.
	SourceStartMS int
	SourceEndMS   int
	// Overrides is the USE's explicit level statement (T05): a stated volume
	// or mute. It is a POINTER-FREE struct of its own because Gain's zero
	// means "unset" — the convention `Normalize` implements — so a muted clip
	// cannot say "zero" through the same field without being normalized back
	// up to its role default. Nil means the use stated nothing.
	Overrides *TrackOverrides
	// Label names the clip in the engine's error output, for a failure a reader can act on. It is
	// a line or shot identifier, never a filename.
	Label string
}

// TrackOverrides is the level statement a use can make about its clip.
type TrackOverrides struct {
	// Volume replaces the role default when set.
	Volume *float64
	// Muted is literal silence: the clip keeps its row and its manifest
	// reference, and the engine formats `volume=0` for it.
	Muted bool
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
	if c.SourceStartMS < 0 || c.SourceEndMS < 0 {
		return InvalidError("A source trim cannot start before the file's beginning.")
	}
	if c.SourceEndMS > 0 && c.SourceEndMS <= c.SourceStartMS {
		return InvalidError("A source trim's end must be after its start.")
	}
	return nil
}

// Normalize returns the clip with its unset gain replaced by its role's default.
//
// It is a separate step from validation because it CHANGES the value, and a function named Validate
// that quietly rewrote its receiver would be a surprise. The mix path calls this once, before
// building anything.
func (c AudioClip) Normalize() AudioClip {
	// THE USE'S OWN LEVEL (T05) is the exception to the zero-means-unset
	// convention: a stated volume replaces the role default, and a stated
	// mute is literal silence — the gain stays zero because the mix applies
	// the override AFTER this pass. Filling it here would turn 「静音」 into
	// the role default, which is the exact ambiguity the override struct
	// exists to keep apart.
	if c.Overrides != nil {
		if c.Overrides.Muted {
			c.Gain = 0
			return c
		}
		if c.Overrides.Volume != nil {
			c.Gain = *c.Overrides.Volume
			return c
		}
	}
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

// TrackParams is one audio USE's own placement — the per-usage document
// migration 000029 stores, read by the timeline and consumed by the mixer.
//
// # Why the parameters are sparse
//
// Every field is a pointer: nil means "not set", and the mixer fills it from
// what it already knows — the shot's start, the role's default gain, the
// clip's own length. A document that had to restate everything would go stale
// the moment a shot moved; a sparse one only overrides what the user set.
type TrackParams struct {
	// OffsetMS moves the clip's start relative to its default (the shot's
	// start for dialogue and effects, zero for a bed). Positive is later.
	OffsetMS *int
	// SourceStartMS and SourceEndMS trim the SOURCE file: playback uses the
	// [start, end) window rather than the whole file. Nil start means zero,
	// nil end means the file's own end.
	SourceStartMS *int
	SourceEndMS   *int
	// DurationMS caps playback length after trimming. Nil means "until the
	// trimmed source ends".
	DurationMS *int
	// Volume is the clip's gain multiplier. Nil falls back to the role
	// default through Normalized().
	Volume *float64
	// Muted silences the clip while keeping its row — a mix that keeps the
	// take listed but plays nothing.
	Muted *bool
	// DialogueLineID names the line a dialogue clip renders, so a shot with
	// several lines can place each one by the line's own cue rather than at
	// the shot's start. Empty on effects and beds.
	DialogueLineID string
}

// ParseTrackParams reads the stored document, tolerating anything malformed:
// a row whose params cannot be parsed still plays, at its defaults — the
// same direction audioJobInputOf takes, because a placement the user set is
// an override, not a precondition.
func ParseTrackParams(paramsJSON string) TrackParams {
	var params TrackParams
	trimmed := strings.TrimSpace(paramsJSON)
	if trimmed == "" {
		return params
	}
	var document struct {
		OffsetMS       *int     `json:"offsetMs"`
		SourceStartMS  *int     `json:"sourceStartMs"`
		SourceEndMS    *int     `json:"sourceEndMs"`
		DurationMS     *int     `json:"durationMs"`
		Volume         *float64 `json:"volume"`
		Muted          *bool    `json:"muted"`
		DialogueLineID string   `json:"dialogueLineId"`
	}
	if err := json.Unmarshal([]byte(trimmed), &document); err != nil {
		return TrackParams{}
	}
	return TrackParams{
		OffsetMS:       document.OffsetMS,
		SourceStartMS:  document.SourceStartMS,
		SourceEndMS:    document.SourceEndMS,
		DurationMS:     document.DurationMS,
		Volume:         document.Volume,
		Muted:          document.Muted,
		DialogueLineID: document.DialogueLineID,
	}
}

// MarshalTrackParams writes the document a usage stores.
func MarshalTrackParams(params TrackParams) (string, error) {
	if params.Empty() {
		return "", nil
	}
	encoded, err := json.Marshal(struct {
		OffsetMS       *int     `json:"offsetMs,omitempty"`
		SourceStartMS  *int     `json:"sourceStartMs,omitempty"`
		SourceEndMS    *int     `json:"sourceEndMs,omitempty"`
		DurationMS     *int     `json:"durationMs,omitempty"`
		Volume         *float64 `json:"volume,omitempty"`
		Muted          *bool    `json:"muted,omitempty"`
		DialogueLineID string   `json:"dialogueLineId,omitempty"`
	}{
		OffsetMS:       params.OffsetMS,
		SourceStartMS:  params.SourceStartMS,
		SourceEndMS:    params.SourceEndMS,
		DurationMS:     params.DurationMS,
		Volume:         params.Volume,
		Muted:          params.Muted,
		DialogueLineID: params.DialogueLineID,
	})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// Empty reports whether the parameters override nothing.
func (t TrackParams) Empty() bool {
	return t.OffsetMS == nil && t.SourceStartMS == nil && t.SourceEndMS == nil &&
		t.DurationMS == nil && t.Volume == nil && t.Muted == nil && t.DialogueLineID == ""
}
