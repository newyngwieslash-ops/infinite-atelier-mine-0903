package media

import (
	"strings"
	"time"
)

// voice.go is FR-080's 多角色声线映射: which voice renders which character.
//
// # What was missing
//
// `AudioRequest.Voice` travelled per TTS submission and was stored nowhere that could be read back.
// The consequence is not that the feature was absent from the request — it is that "character X
// speaks with voice Y" was not a fact in this application. A user retyped the voice for every line,
// and the audio section's own note said the voice came from the project's configuration, which means
// a project with two characters had one voice for both.
//
// # Why the mapping is keyed by the CHARACTER
//
// `dialogue_lines.character_entity_id` is already how a line names its speaker. Keying a voice on
// anything else — a line, a scene, a shot — would need a join to answer "what does this character
// sound like", and would let two lines of the same character disagree.
//
// # Why the channel and model travel WITH the voice
//
// A voice name is not portable. `alloy` on one channel and `alloy` on another are different sounds,
// so a mapping that stored the name alone would let a channel change silently change a performance —
// a defect a user would hear and be unable to explain. The two extra fields are read back with the
// voice for exactly that reason.

// CharacterVoice is one casting decision: character X speaks with a given voice.
//
// It is a VALUE the application reads, writes and resolves. It is not an asset: it has no file, no
// version and nothing to approve, which is why it is not in the asset aggregate.
type CharacterVoice struct {
	ID        string
	ProjectID string
	// CharacterEntityID names the speaker. It is the same identifier a dialogue line carries, so a
	// line's voice is one lookup rather than a join through the script.
	CharacterEntityID string
	// ProviderConfigID and Model may be EMPTY, which means "whatever the project's audio
	// configuration names". A user who has cast voices but not pinned them to a channel is in this
	// state, and `ResolveVoiceChoice` decides what an incomplete mapping falls back to.
	ProviderConfigID string
	Model            string
	// Voice is the only required field beyond the character: a mapping that named no voice would be
	// a row saying nothing.
	Voice     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int64
}

// maxVoiceLength bounds a stored voice name.
//
// It is a length bound rather than a vocabulary, because a voice name is a provider's string and
// this build has no table of them: an OpenAI-compatible channel and a local engine name voices
// differently, and a CHECK listing one provider's names would refuse the other's. The bound exists
// because the value reaches a provider request and a database column, not because 200 is meaningful.
const maxVoiceLength = 200

// Validate checks a mapping before it is stored.
func (c CharacterVoice) Validate() error {
	if strings.TrimSpace(c.ProjectID) == "" {
		return InvalidError("A voice mapping must belong to a project.")
	}
	if strings.TrimSpace(c.CharacterEntityID) == "" {
		return InvalidError("A voice mapping must name the character it casts.")
	}
	voice := strings.TrimSpace(c.Voice)
	if voice == "" {
		return InvalidError("A voice mapping must name a voice.")
	}
	if len([]rune(voice)) > maxVoiceLength {
		return InvalidError("That voice name is longer than this application stores.")
	}
	if len([]rune(c.Model)) > maxVoiceLength {
		return InvalidError("That model name is longer than this application stores.")
	}
	if c.Revision < 1 {
		return InvalidError("A voice mapping needs a revision.")
	}
	return nil
}

// Normalized returns the mapping with its text fields trimmed, which is what is stored.
//
// It is separate from Validate for the same reason `AudioClip.Normalize` is: a function named
// Validate that rewrote its receiver would be a surprise.
func (c CharacterVoice) Normalized() CharacterVoice {
	c.CharacterEntityID = strings.TrimSpace(c.CharacterEntityID)
	c.ProviderConfigID = strings.TrimSpace(c.ProviderConfigID)
	c.Model = strings.TrimSpace(c.Model)
	c.Voice = strings.TrimSpace(c.Voice)
	return c
}

// VoiceSource says WHERE a resolved voice came from, so a user can be told why a line sounds the way
// it does.
//
// It exists because the resolution has three levels and the levels are invisible without it: a user
// who hears the project default on a character they cast would otherwise have no way to see that the
// cast was not the thing that decided.
type VoiceSource string

const (
	// VoiceSourceCharacter is a mapping that named this character.
	VoiceSourceCharacter VoiceSource = "character"
	// VoiceSourceProject is the project's audio configuration, used because the character has no
	// mapping.
	VoiceSourceProject VoiceSource = "project"
	// VoiceSourceUnset is neither: the provider applies its own default for a request that names no
	// voice.
	VoiceSourceUnset VoiceSource = "unset"
)

// IsValidVoiceSource reports whether a source may be shown.
func IsValidVoiceSource(value VoiceSource) bool {
	return value == VoiceSourceCharacter || value == VoiceSourceProject || value == VoiceSourceUnset
}

// VoiceChoice is the voice a line should be rendered with, and where it came from.
type VoiceChoice struct {
	ProviderConfigID string
	Model            string
	Voice            string
	Source           VoiceSource
}

// IsSet reports whether the choice names a voice at all.
//
// A caller uses it to decide whether to send the field: an unset voice is left OUT of the request so
// the provider applies its own default, rather than being sent as an empty string the provider may
// read as a voice with no name.
func (c VoiceChoice) IsSet() bool { return strings.TrimSpace(c.Voice) != "" }

// ResolveVoiceChoice decides which voice a character's line uses.
//
// # The order, and why it is a function rather than a method on a store
//
// Character mapping first, then the project's configuration, then nothing. It takes both inputs as
// ARGUMENTS rather than reading them, so the policy can be tested without a database and so a caller
// cannot accidentally resolve against a different project's default.
//
// `mapping` is the zero value when the character has none; `project` is the project's own audio
// configuration, whose fields are empty when the user has set nothing. The two are told apart by
// `found` rather than by emptiness, because a mapping that named a voice but no channel is still a
// mapping — it is the case the fallback below is for.
func ResolveVoiceChoice(mapping CharacterVoice, found bool, project VoiceChoice) VoiceChoice {
	if found && strings.TrimSpace(mapping.Voice) != "" {
		choice := VoiceChoice{
			ProviderConfigID: strings.TrimSpace(mapping.ProviderConfigID),
			Model:            strings.TrimSpace(mapping.Model),
			Voice:            strings.TrimSpace(mapping.Voice),
			Source:           VoiceSourceCharacter,
		}
		// AN INCOMPLETE MAPPING INHERITS THE MISSING HALF from the project rather than resolving to
		// nothing. A user who cast a voice and left the channel alone means "this voice, on whatever
		// channel I am using" — refusing would make the cast unusable until every channel was chosen,
		// and replacing the voice with the project's would discard the decision they made.
		if choice.ProviderConfigID == "" {
			choice.ProviderConfigID = strings.TrimSpace(project.ProviderConfigID)
		}
		if choice.Model == "" {
			choice.Model = strings.TrimSpace(project.Model)
		}
		return choice
	}
	if strings.TrimSpace(project.Voice) != "" {
		return VoiceChoice{
			ProviderConfigID: strings.TrimSpace(project.ProviderConfigID),
			Model:            strings.TrimSpace(project.Model),
			Voice:            strings.TrimSpace(project.Voice),
			Source:           VoiceSourceProject,
		}
	}
	return VoiceChoice{Source: VoiceSourceUnset}
}
