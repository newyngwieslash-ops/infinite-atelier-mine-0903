package desktop

import (
	"strings"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// media_voices.go is the Wails surface for FR-080's V1 audio clauses that WP-27 built:
// 多角色声线映射 and 音效建议.
//
// # Why these are on the MEDIA binding rather than the drama one
//
// The voices are a media decision — they decide how a line SOUNDS — and the effects are a media
// decision — they decide what a shot carries. The drama binding owns the script, the board and the
// assets; everything that answers "what will the film be made of" answers through this one, which is
// where the timeline and the export already are.
//
// # The sound of a line, in one answer
//
// `ResolveVoice` returns the voice AND where it came from, and the source is the part that makes the
// feature legible: a user who hears the project's default on a character they cast needs to see that
// the cast is not what decided. It is a read, not a command, so the audio section can show it beside
// a line without a second call.

// CharacterVoiceDTO is one casting decision as the interface sees it.
type CharacterVoiceDTO struct {
	ID                string `json:"id"`
	ProjectID         string `json:"projectId"`
	CharacterEntityID string `json:"characterEntityId"`
	// CharacterName is the entity's canonical name, so a panel renders a character rather than an
	// identifier. It is empty when the character was deleted between the join and the response.
	CharacterName    string `json:"characterName,omitempty"`
	ProviderConfigID string `json:"providerConfigId,omitempty"`
	Model            string `json:"model,omitempty"`
	Voice            string `json:"voice"`
	Revision         int64  `json:"revision"`
}

// AssignVoiceRequest casts a character's voice.
type AssignVoiceRequest struct {
	ProjectID         string `json:"projectId"`
	CharacterEntityID string `json:"characterEntityId"`
	// ProviderConfigID and Model may be empty, which means "whatever the project's own audio
	// configuration names". An incomplete mapping inherits the missing half rather than resolving to
	// nothing — see `domainmedia.ResolveVoiceChoice`.
	ProviderConfigID string `json:"providerConfigId,omitempty"`
	Model            string `json:"model,omitempty"`
	Voice            string `json:"voice"`
	// ExpectedRevision guards an edit of an existing mapping. Zero means "this is a new assignment",
	// which is what a panel that has no mapping to show sends. A stale non-zero value is refused as a
	// conflict rather than overwriting a change somebody else made.
	ExpectedRevision int64 `json:"expectedRevision,omitempty"`
}

// ClearVoiceRequest removes a character's casting decision.
type ClearVoiceRequest struct {
	ProjectID         string `json:"projectId"`
	CharacterEntityID string `json:"characterEntityId"`
}

// ResolveVoiceRequest asks what a line should sound like.
type ResolveVoiceRequest struct {
	ProjectID         string `json:"projectId"`
	CharacterEntityID string `json:"characterEntityId,omitempty"`
	// ProjectVoice, ProjectModel and ProjectProvider are the user's own audio configuration, which is
	// NOT in the drama schema — it is a preference the desktop layer holds. They are passed in rather
	// than read here because the media layer must not reach into the settings surface, and because the
	// resolution's third level (nothing stated) has to be expressible.
	ProjectVoice    string `json:"projectVoice,omitempty"`
	ProjectModel    string `json:"projectModel,omitempty"`
	ProjectProvider string `json:"projectProvider,omitempty"`
}

// VoiceChoiceDTO is the resolved voice and its provenance.
type VoiceChoiceDTO struct {
	ProviderConfigID string `json:"providerConfigId,omitempty"`
	Model            string `json:"model,omitempty"`
	Voice            string `json:"voice,omitempty"`
	// Source is "character", "project" or "unset". A UI shows it so a user can tell a cast voice from
	// the project's default — the two are indistinguishable by sound alone.
	Source string `json:"source"`
	// IsSet reports whether any voice was resolved. When false the caller sends NO voice and the
	// provider applies its own default.
	IsSet bool `json:"isSet"`
}

// EffectSuggestionDTO is one proposed effect with the evidence for it.
type EffectSuggestionDTO struct {
	ShotID  string `json:"shotId"`
	Ordinal int    `json:"ordinal"`
	Intent  string `json:"intent"`
	Effect  string `json:"effect"`
	// Matched is the term that produced the suggestion, and it is the field that makes the suggestion
	// checkable rather than something to be trusted.
	Matched string `json:"matched"`
	Score   int    `json:"score"`
}

// DialogLineVoiceRequest asks for several lines' voices at once.
//
// It exists because the audio section lists lines, and resolving them one call per line would be one
// round trip per row. The lines without a character resolve to the project default, which is stated
// rather than skipped so a caller can tell "no character" from "not asked".
type DialogLineVoiceRequest struct {
	ProjectID       string   `json:"projectId"`
	CharacterIDs    []string `json:"characterIds"`
	ProjectVoice    string   `json:"projectVoice,omitempty"`
	ProjectModel    string   `json:"projectModel,omitempty"`
	ProjectProvider string   `json:"projectProvider,omitempty"`
}

// voiceService exposes the casting store, or nil when this build has none.
func (b *MediaBinding) voiceService() *appmedia.VoiceMappingService {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.voices
}

// idGenerator exposes the binding's identifier generator.
func (b *MediaBinding) idGenerator() *id.Generator {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ids
}

// ListCharacterVoices returns a project's casting decisions.
//
// An empty list rather than an error when the store is missing, because this is a QUERY: the desktop
// layer's rule is that a query answers empty and a command refuses. The panel then shows no cast
// rather than a failure a user cannot act on.
func (b *MediaBinding) ListCharacterVoices(projectID string) ([]CharacterVoiceDTO, error) {
	service := b.voiceService()
	if service == nil {
		return []CharacterVoiceDTO{}, nil
	}
	mappings, err := service.ListVoices(b.context(), projectID)
	if err != nil {
		return nil, toDramaError(err)
	}
	views := make([]CharacterVoiceDTO, 0, len(mappings))
	for _, mapping := range mappings {
		views = append(views, toCharacterVoiceDTO(mapping))
	}
	return views, nil
}

// AssignCharacterVoice casts a character's voice.
func (b *MediaBinding) AssignCharacterVoice(request AssignVoiceRequest) (CharacterVoiceDTO, error) {
	service := b.voiceService()
	if service == nil {
		return CharacterVoiceDTO{}, MediaBindingUnavailable()
	}
	// The identifier is minted HERE rather than in the service, because the application layer must not
	// import the platform's identifier package. It is minted per command rather than derived, so a
	// retry is a NEW assignment — which is what the revision guard is for: the retry that follows a
	// refusal reloads and names the revision it saw.
	generator := b.idGenerator()
	if generator == nil {
		return CharacterVoiceDTO{}, MediaBindingUnavailable()
	}
	id, err := generator.New()
	if err != nil {
		return CharacterVoiceDTO{}, toDramaError(err)
	}
	record, err := service.AssignVoice(b.context(), appmedia.AssignVoiceRequest{
		ID:                id,
		ProjectID:         strings.TrimSpace(request.ProjectID),
		CharacterEntityID: strings.TrimSpace(request.CharacterEntityID),
		ProviderConfigID:  strings.TrimSpace(request.ProviderConfigID),
		Model:             strings.TrimSpace(request.Model),
		Voice:             strings.TrimSpace(request.Voice),
		ExpectedRevision:  request.ExpectedRevision,
	})
	if err != nil {
		return CharacterVoiceDTO{}, toDramaError(err)
	}
	return CharacterVoiceDTO{
		ID: record.ID, ProjectID: record.ProjectID, CharacterEntityID: record.CharacterEntityID,
		ProviderConfigID: record.ProviderConfigID, Model: record.Model, Voice: record.Voice,
		Revision: record.Revision,
	}, nil
}

// ClearCharacterVoice removes a character's casting decision.
//
// It reports whether there was one to remove, which is what lets a panel tell "cleared" from "there was
// nothing there" without a second read.
func (b *MediaBinding) ClearCharacterVoice(request ClearVoiceRequest) (bool, error) {
	service := b.voiceService()
	if service == nil {
		return false, MediaBindingUnavailable()
	}
	removed, err := service.ClearVoice(b.context(), appmedia.ClearVoiceRequest{
		ProjectID:         strings.TrimSpace(request.ProjectID),
		CharacterEntityID: strings.TrimSpace(request.CharacterEntityID),
	})
	if err != nil {
		return false, toDramaError(err)
	}
	return removed, nil
}

// ResolveCharacterVoice answers what a character's line should be rendered with.
func (b *MediaBinding) ResolveCharacterVoice(request ResolveVoiceRequest) (VoiceChoiceDTO, error) {
	service := b.voiceService()
	if service == nil {
		// A build without the store still answers, using the caller's own configuration: a user who set
		// a project voice can render speech without the drama core, which is what the audio section did
		// before this package existed. Refusing here would remove a working capability from a degraded
		// build rather than add one.
		fallback := domainmedia.VoiceChoice{}
		if voice := strings.TrimSpace(request.ProjectVoice); voice != "" {
			fallback = domainmedia.VoiceChoice{
				ProviderConfigID: strings.TrimSpace(request.ProjectProvider),
				Model:            strings.TrimSpace(request.ProjectModel),
				Voice:            voice,
				Source:           domainmedia.VoiceSourceProject,
			}
		} else {
			fallback.Source = domainmedia.VoiceSourceUnset
		}
		return toVoiceChoiceDTO(fallback), nil
	}
	choice, err := service.ResolveVoice(b.context(), strings.TrimSpace(request.ProjectID),
		strings.TrimSpace(request.CharacterEntityID), domainmedia.VoiceChoice{
			ProviderConfigID: strings.TrimSpace(request.ProjectProvider),
			Model:            strings.TrimSpace(request.ProjectModel),
			Voice:            strings.TrimSpace(request.ProjectVoice),
			Source:           domainmedia.VoiceSourceProject,
		})
	if err != nil {
		return VoiceChoiceDTO{}, toDramaError(err)
	}
	return toVoiceChoiceDTO(choice), nil
}

// SuggestShotEffects proposes an effect for each shot whose intent names one.
//
// # The input, and why the CALLER supplies it
//
// Suggestions read `shots.audio_intent`, which lives in the script — and the script's structure is the
// drama binding's to read. Rather than have the media layer reach across, the caller passes the shots
// it already listed. The alternative would be a second reader of the script inside this binding, which
// is the "two answers to one question" shape this repository keeps finding.
func (b *MediaBinding) SuggestShotEffects(shots []ShotEffectInputDTO) ([]EffectSuggestionDTO, error) {
	inputs := make([]appmedia.ShotEffectInput, 0, len(shots))
	for _, shot := range shots {
		inputs = append(inputs, appmedia.ShotEffectInput{
			ShotID:  strings.TrimSpace(shot.ShotID),
			Ordinal: shot.Ordinal,
			Intent:  shot.AudioIntent,
		})
	}
	suggestions := appmedia.SuggestEffects(inputs)
	views := make([]EffectSuggestionDTO, 0, len(suggestions))
	for _, suggestion := range suggestions {
		views = append(views, EffectSuggestionDTO{
			ShotID: suggestion.ShotID, Ordinal: suggestion.Ordinal, Intent: suggestion.Intent,
			Effect: suggestion.Effect, Matched: suggestion.Matched, Score: suggestion.Score,
		})
	}
	return views, nil
}

// EffectVocabulary exposes the sounds this build can suggest, so a panel can show the vocabulary
// instead of a user guessing what it recognises.
func (b *MediaBinding) EffectVocabulary() ([]string, error) {
	vocabulary := appmedia.EffectVocabulary()
	effects := make([]string, 0, len(vocabulary))
	for _, entry := range vocabulary {
		effects = append(effects, entry.Effect)
	}
	return effects, nil
}

// ShotEffectInputDTO is one shot as the suggestion reads it.
type ShotEffectInputDTO struct {
	ShotID      string `json:"shotId"`
	Ordinal     int    `json:"ordinal"`
	AudioIntent string `json:"audioIntent,omitempty"`
}

// toCharacterVoiceDTO renders a stored mapping.
func toCharacterVoiceDTO(mapping appmedia.VoiceMapping) CharacterVoiceDTO {
	return CharacterVoiceDTO{
		ID:                mapping.ID,
		ProjectID:         mapping.ProjectID,
		CharacterEntityID: mapping.CharacterEntityID,
		CharacterName:     mapping.CharacterName,
		ProviderConfigID:  mapping.ProviderConfigID,
		Model:             mapping.Model,
		Voice:             mapping.Voice,
		Revision:          mapping.Revision,
	}
}

// toVoiceChoiceDTO renders a resolved choice.
func toVoiceChoiceDTO(choice domainmedia.VoiceChoice) VoiceChoiceDTO {
	return VoiceChoiceDTO{
		ProviderConfigID: choice.ProviderConfigID,
		Model:            choice.Model,
		Voice:            choice.Voice,
		Source:           string(choice.Source),
		IsSet:            choice.IsSet(),
	}
}
