package media

import (
	"context"
	"strings"

	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// voice_service.go is FR-080's 多角色声线映射: the commands and the read behind the resolution.
//
// # What this service is for, in one sentence
//
// It turns "character X speaks with voice Y" into a fact the application can read back, which is the
// thing that was missing: the voice travelled per submission and was stored nowhere, so a project
// with two characters had one voice for both and a user retyped it for every line.
//
// # What it deliberately does NOT own
//
// The PROJECT default voice. It comes from the user's audio configuration, which is not in the drama
// schema — it is a value the desktop layer passes in. This service stores the per-character mapping
// and RESOLVES against whatever default the caller supplies, so the two questions stay separate:
// "what did this project choose globally" is configuration, and "who has been cast" is a fact about
// the story. Merging them would put a UI preference into a table whose foreign keys are story
// entities.

// VoiceRepository is the persistence this service needs.
type VoiceRepository interface {
	// GetVoice returns one character's mapping, or found=false when there is none.
	GetVoice(ctx context.Context, projectID, characterEntityID string) (domainmedia.CharacterVoice, bool, error)
	// AssignVoice stores a mapping, replacing any previous one for that character. A non-zero
	// expectedRevision guards an edit.
	AssignVoice(ctx context.Context, record domainmedia.CharacterVoice, expectedRevision int64) (domainmedia.CharacterVoice, error)
	// ClearVoice removes a mapping and reports whether there was one.
	ClearVoice(ctx context.Context, projectID, characterEntityID string) (bool, error)
	// ListVoices returns every mapping in a project with the character's name.
	ListVoices(ctx context.Context, projectID string) ([]VoiceMapping, error)
}

// VoiceMapping is a stored casting decision with the character's name, for a panel to render.
//
// The name is carried HERE rather than looked up by a caller: a panel that listed entity identifiers
// would be unreadable, and a caller that resolved each name itself would issue one query per row.
type VoiceMapping struct {
	domainmedia.CharacterVoice
	CharacterName string
}

// VoiceMappingService assigns voices and resolves them.
type VoiceMappingService struct {
	repository VoiceRepository
}

// NewVoiceMappingService builds the service over its repository.
func NewVoiceMappingService(repository VoiceRepository) *VoiceMappingService {
	return &VoiceMappingService{repository: repository}
}

// Available reports whether the service can be used at all.
//
// It exists because a build without the drama core composes this service with no repository, and the
// desktop layer's rule is that a missing binding reads as empty while a missing CORE refuses with a
// message. A caller asks this before offering the feature rather than discovering it from an error.
func (s *VoiceMappingService) Available() bool {
	return s != nil && s.repository != nil
}

// AssignVoiceRequest names the casting decision to store.
type AssignVoiceRequest struct {
	ProjectID         string
	CharacterEntityID string
	// ProviderConfigID and Model may be empty, meaning "whatever the project's configuration names".
	// See domainmedia.ResolveVoiceChoice for what an incomplete mapping resolves to.
	ProviderConfigID string
	Model            string
	Voice            string
	// ExpectedRevision guards an edit of an existing mapping. Zero means "this is a new assignment".
	ExpectedRevision int64
	// ID is the identifier to store, and it is REQUIRED. The application layer does not mint
	// identifiers — `internal/platform/id` is an infrastructure concern, and a service that reached
	// for it would be an application package importing a platform one. The desktop layer generates it,
	// which also means a retried assignment carries the same id rather than creating a second row.
	ID string
}

// AssignVoice stores a character's voice.
//
// The revision is RETURNED as part of the record so an editing UI can hold it without guessing: a
// caller that assumed the new revision would be wrong the moment two assignments raced, and the
// conflict would surface as a confusing refusal on the next save.
func (s *VoiceMappingService) AssignVoice(ctx context.Context, request AssignVoiceRequest) (domainmedia.CharacterVoice, error) {
	if !s.Available() {
		return domainmedia.CharacterVoice{}, NotAvailableError(
			"Character voices need the drama store, which this build does not have.")
	}
	projectID := strings.TrimSpace(request.ProjectID)
	characterID := strings.TrimSpace(request.CharacterEntityID)
	if projectID == "" || characterID == "" {
		return domainmedia.CharacterVoice{}, InvalidError("A voice mapping needs a project and a character.")
	}
	if strings.TrimSpace(request.Voice) == "" {
		return domainmedia.CharacterVoice{}, InvalidError("A voice mapping must name a voice.")
	}
	id := strings.TrimSpace(request.ID)
	if id == "" {
		return domainmedia.CharacterVoice{}, InvalidError("A voice mapping needs an identifier.")
	}
	record := domainmedia.CharacterVoice{
		ID:                id,
		ProjectID:         projectID,
		CharacterEntityID: characterID,
		ProviderConfigID:  strings.TrimSpace(request.ProviderConfigID),
		Model:             strings.TrimSpace(request.Model),
		Voice:             strings.TrimSpace(request.Voice),
		Revision:          1,
	}
	return s.repository.AssignVoice(ctx, record, request.ExpectedRevision)
}

// ClearVoiceRequest names the character whose mapping is removed.
type ClearVoiceRequest struct {
	ProjectID         string
	CharacterEntityID string
}

// ClearVoice removes a character's mapping.
//
// It reports whether there WAS one, and treats absence as success: a user clearing a character they
// are unsure about is asking for the state, not for a confirmation, and a refusal would turn "already
// good" into an error to correct.
func (s *VoiceMappingService) ClearVoice(ctx context.Context, request ClearVoiceRequest) (bool, error) {
	if !s.Available() {
		return false, NotAvailableError(
			"Character voices need the drama store, which this build does not have.")
	}
	projectID := strings.TrimSpace(request.ProjectID)
	characterID := strings.TrimSpace(request.CharacterEntityID)
	if projectID == "" || characterID == "" {
		return false, InvalidError("Clearing a voice needs a project and a character.")
	}
	return s.repository.ClearVoice(ctx, projectID, characterID)
}

// ListVoices returns a project's casting decisions, ordered by character name.
func (s *VoiceMappingService) ListVoices(ctx context.Context, projectID string) ([]VoiceMapping, error) {
	if !s.Available() {
		// A build without the store lists nothing, which is the desktop rule for a query: an empty
		// panel rather than an error the user cannot act on.
		return []VoiceMapping{}, nil
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, InvalidError("Listing voices needs a project.")
	}
	return s.repository.ListVoices(ctx, strings.TrimSpace(projectID))
}

// ResolveVoice answers what a character's line should be rendered with.
//
// `projectDefault` is the user's audio configuration, supplied by the caller because it is not part
// of the drama schema. The resolution order lives in the DOMAIN (`ResolveVoiceChoice`) rather than
// here, so the policy is testable without a store and so a second caller cannot implement it
// differently.
//
// A character with no mapping and a project with no default resolves to an UNSET choice, which the
// caller sends as no voice at all — the provider then applies its own default. That is the honest
// outcome: this application has nothing to say about how that line sounds.
func (s *VoiceMappingService) ResolveVoice(ctx context.Context, projectID, characterEntityID string, projectDefault domainmedia.VoiceChoice) (domainmedia.VoiceChoice, error) {
	if !s.Available() {
		// With no store there is nothing to look up, so the project's own configuration is the whole
		// answer. It is NOT an error: a build without the drama core can still render speech using the
		// configuration the user set, which is what the audio section did before this package existed.
		if strings.TrimSpace(projectDefault.Voice) != "" {
			projectDefault.Source = domainmedia.VoiceSourceProject
			return projectDefault, nil
		}
		return domainmedia.VoiceChoice{Source: domainmedia.VoiceSourceUnset}, nil
	}
	projectID = strings.TrimSpace(projectID)
	characterEntityID = strings.TrimSpace(characterEntityID)
	if projectID == "" || characterEntityID == "" {
		// A line with NO character is legitimate — an action beat, a crowd line — and has no cast to
		// look up. The project default is the answer, which is exactly what `ResolveVoiceChoice` does
		// with found=false.
		return domainmedia.ResolveVoiceChoice(domainmedia.CharacterVoice{}, false, projectDefault), nil
	}
	mapping, found, err := s.repository.GetVoice(ctx, projectID, characterEntityID)
	if err != nil {
		return domainmedia.VoiceChoice{}, err
	}
	return domainmedia.ResolveVoiceChoice(mapping, found, projectDefault), nil
}
