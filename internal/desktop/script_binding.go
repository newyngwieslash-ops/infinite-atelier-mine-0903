package desktop

import (
	"context"
	"strings"

	appproductionpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appscriptpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/scriptpipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/stagepipeline"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// script_binding.go is WP-08's binding surface: the two upstream version families, a script version's
// whole content, the field locks, the version diff and the canvas projection.
//
// # Why the upstream families had no bindings at all
//
// `application/script` has been able to create and approve story skeletons and adaptation strategies
// since WP-05, and NONE of it was reachable from the interface: the binding had `EnsureScript`,
// `CreateScriptVersion`, `ApproveScriptVersion` and the scene/shot reads, and nothing for the two
// artifacts the first two stages produce. So a Script UI could show a version history for the third
// stage only, and a user could not approve a skeleton through the desktop at all — the approval existed
// as a service method the frontend had no way to reach. WP-06 recorded the shape of that gap
// ("dangling references that no command reaches"); this file is the route.
//
// # The DTOs are the domain's fields, not its types
//
// Every method below converts explicitly rather than returning a domain record, and the conversions are
// written out one field at a time. That is deliberate: a domain type gaining a field would otherwise
// silently appear in the wire contract, and a binding is a PROMISE to the frontend. The cost is a
// conversion function per family, which is what a reader scanning for "what does the UI see" wants
// anyway.

// ScriptFamily names one of the three version families.
//
// It is a string rather than the domain's type because the frontend sends it back as one, and a binding
// that took a domain type would be a binding whose wire form changed when the domain's did. The value is
// validated against the domain's own vocabulary before anything is looked up.
type ScriptFamily = string

// ---------------------------------------------------------------------------
// Story skeleton
// ---------------------------------------------------------------------------

// StorySkeletonVersionDTO is one skeleton version as the interface sees it.
type StorySkeletonVersionDTO struct {
	VersionID        string `json:"versionId"`
	EpisodeID        string `json:"episodeId"`
	VersionNumber    int    `json:"versionNumber"`
	Status           string `json:"status"`
	BasedOnVersionID string `json:"basedOnVersionId,omitempty"`
	OpeningHook      string `json:"openingHook"`
	CoreConflict     string `json:"coreConflict"`
	TurningPoints    string `json:"turningPoints"`
	Climax           string `json:"climax"`
	EndingHook       string `json:"endingHook"`
	// EstimatedDurationSeconds is the version's own estimate, which the skeleton stage states rather
	// than deriving: a skeleton has no scenes to sum.
	EstimatedDurationSeconds int `json:"estimatedDurationSeconds"`
	// SelectedEventIDs is §7.4's link set, read from the link table rather than from the JSON field,
	// because the link table is what a reader can query.
	SelectedEventIDs []string `json:"selectedEventIds"`
	SourceAgentRunID string   `json:"sourceAgentRunId,omitempty"`
	CreatedByType    string   `json:"createdByType"`
	CreatedByID      string   `json:"createdById,omitempty"`
	ChangeReason     string   `json:"changeReason,omitempty"`
	CreatedAt        string   `json:"createdAt"`
}

// CreateStorySkeletonVersionRequest adds one skeleton version.
type CreateStorySkeletonVersionRequest struct {
	EpisodeID                string   `json:"episodeId"`
	BasedOnVersionID         string   `json:"basedOnVersionId,omitempty"`
	OpeningHook              string   `json:"openingHook,omitempty"`
	CoreConflict             string   `json:"coreConflict,omitempty"`
	TurningPointsJSON        string   `json:"turningPointsJson,omitempty"`
	Climax                   string   `json:"climax,omitempty"`
	EndingHook               string   `json:"endingHook,omitempty"`
	EstimatedDurationSeconds int      `json:"estimatedDurationSeconds,omitempty"`
	SelectedEventIDs         []string `json:"selectedEventIds,omitempty"`
	ChangeReason             string   `json:"changeReason,omitempty"`
}

// CreateStorySkeletonVersion appends a skeleton version.
//
// It writes the selection AND the version together, because §7.4 makes the selected events part of what
// a skeleton is: a version whose links were never written is a skeleton that decided nothing, and the
// service's own write path is what keeps the two in one transaction.
func (b *DramaBinding) CreateStorySkeletonVersion(request CreateStorySkeletonVersionRequest) (StorySkeletonVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return StorySkeletonVersionDTO{}, bindingUnavailable()
	}
	record, err := service.CreateStorySkeletonVersion(b.context(), appscript.CreateStorySkeletonVersionRequest{
		EpisodeID:                request.EpisodeID,
		BasedOnVersionID:         request.BasedOnVersionID,
		OpeningHook:              request.OpeningHook,
		CoreConflict:             request.CoreConflict,
		TurningPointsJSON:        request.TurningPointsJSON,
		Climax:                   request.Climax,
		EndingHook:               request.EndingHook,
		EstimatedDurationSeconds: request.EstimatedDurationSeconds,
		SelectedEventIDs:         request.SelectedEventIDs,
		// A version a USER creates is attributed to the user, which is what DOMAIN_MODEL §13.4's
		// distinction is for: a reviewer has to be able to tell which versions came from a model, and
		// this route is the user's own.
		CreatedByType: versioning.CreatedByUser,
		ChangeReason:  request.ChangeReason,
	})
	if err != nil {
		return StorySkeletonVersionDTO{}, toDramaError(err)
	}
	return b.skeletonDTO(b.context(), record)
}

// ListStorySkeletonVersions returns an episode's skeleton versions newest first.
func (b *DramaBinding) ListStorySkeletonVersions(episodeID string) ([]StorySkeletonVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	history, err := service.ListVersions(b.context(), scriptdomain.FamilyStorySkeleton, episodeID)
	if err != nil {
		return nil, toDramaError(err)
	}
	versions, ok := history.([]scriptdomain.StorySkeletonVersion)
	if !ok {
		return nil, bindingUnavailable()
	}
	out := make([]StorySkeletonVersionDTO, 0, len(versions))
	for _, version := range versions {
		dto, err := b.skeletonDTO(b.context(), version)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// ApproveStorySkeletonVersion makes one version the episode's approved skeleton.
func (b *DramaBinding) ApproveStorySkeletonVersion(versionID string) (StorySkeletonVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return StorySkeletonVersionDTO{}, bindingUnavailable()
	}
	record, err := service.ApproveStorySkeletonVersion(b.context(), appscript.ApproveStorySkeletonVersionRequest{
		VersionID: versionID,
	})
	if err != nil {
		return StorySkeletonVersionDTO{}, toDramaError(err)
	}
	return b.skeletonDTO(b.context(), record)
}

// skeletonDTO converts one version, reading its link set.
//
// The link read is separate because §7.4 puts the selection in a LINK TABLE rather than on the row, so a
// conversion that returned the row alone would report a skeleton with no events — which reads as a
// skeleton that selected nothing rather than as a field the converter forgot.
func (b *DramaBinding) skeletonDTO(ctx context.Context, record scriptdomain.StorySkeletonVersion) (StorySkeletonVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return StorySkeletonVersionDTO{}, bindingUnavailable()
	}
	selected, err := service.ListSkeletonEventIDs(ctx, record.ID)
	if err != nil {
		return StorySkeletonVersionDTO{}, toDramaError(err)
	}
	return StorySkeletonVersionDTO{
		VersionID:                record.ID,
		EpisodeID:                record.EpisodeID,
		VersionNumber:            record.VersionNumber,
		Status:                   string(record.Status),
		BasedOnVersionID:         record.BasedOnVersionID,
		OpeningHook:              record.OpeningHook,
		CoreConflict:             record.CoreConflict,
		TurningPoints:            record.TurningPointsJSON,
		Climax:                   record.Climax,
		EndingHook:               record.EndingHook,
		EstimatedDurationSeconds: record.EstimatedDurationSeconds,
		SelectedEventIDs:         selected,
		SourceAgentRunID:         record.SourceAgentRunID,
		CreatedByType:            string(record.CreatedByType),
		CreatedByID:              record.CreatedByID,
		ChangeReason:             record.ChangeReason,
		CreatedAt:                record.CreatedAt.UTC().Format(rfc3339),
	}, nil
}

// ---------------------------------------------------------------------------
// Adaptation strategy
// ---------------------------------------------------------------------------

// StrategyEventLinkDTO is one per-event decision (§7.5).
type StrategyEventLinkDTO struct {
	StoryEventID string `json:"storyEventId"`
	Treatment    string `json:"treatment"`
	Ordinal      int    `json:"ordinal"`
}

// AdaptationStrategyVersionDTO is one strategy version as the interface sees it.
type AdaptationStrategyVersionDTO struct {
	VersionID             string                 `json:"versionId"`
	EpisodeID             string                 `json:"episodeId"`
	VersionNumber         int                    `json:"versionNumber"`
	Status                string                 `json:"status"`
	BasedOnVersionID      string                 `json:"basedOnVersionId,omitempty"`
	StrategySummary       string                 `json:"strategySummary"`
	AdaptationMode        string                 `json:"adaptationMode"`
	MergedEventGroupsJSON string                 `json:"mergedEventGroupsJson,omitempty"`
	OriginalAdditions     string                 `json:"originalAdditions,omitempty"`
	Rationale             string                 `json:"rationale,omitempty"`
	Risks                 string                 `json:"risks,omitempty"`
	EventLinks            []StrategyEventLinkDTO `json:"eventLinks"`
	SourceAgentRunID      string                 `json:"sourceAgentRunId,omitempty"`
	CreatedByType         string                 `json:"createdByType"`
	CreatedByID           string                 `json:"createdById,omitempty"`
	ChangeReason          string                 `json:"changeReason,omitempty"`
	CreatedAt             string                 `json:"createdAt"`
}

// CreateAdaptationStrategyVersionRequest adds one strategy version.
type CreateAdaptationStrategyVersionRequest struct {
	EpisodeID             string                 `json:"episodeId"`
	BasedOnVersionID      string                 `json:"basedOnVersionId,omitempty"`
	StrategySummary       string                 `json:"strategySummary,omitempty"`
	AdaptationMode        string                 `json:"adaptationMode,omitempty"`
	MergedEventGroupsJSON string                 `json:"mergedEventGroupsJson,omitempty"`
	OriginalAdditions     string                 `json:"originalAdditions,omitempty"`
	Rationale             string                 `json:"rationale,omitempty"`
	Risks                 string                 `json:"risks,omitempty"`
	EventLinks            []StrategyEventLinkDTO `json:"eventLinks,omitempty"`
	ChangeReason          string                 `json:"changeReason,omitempty"`
}

// CreateAdaptationStrategyVersion appends a strategy version.
//
// The treatments arrive as an ORDERED list and become the link table's ordinals by POSITION, which is the
// only thing §7.5's "reordered" can mean: a caller that reordered the list has changed the adaptation's
// order, and a payload that stated both an order and an ordinal could state two different ones.
func (b *DramaBinding) CreateAdaptationStrategyVersion(request CreateAdaptationStrategyVersionRequest) (AdaptationStrategyVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return AdaptationStrategyVersionDTO{}, bindingUnavailable()
	}
	links := make([]scriptdomain.StrategyEventLink, 0, len(request.EventLinks))
	for _, link := range request.EventLinks {
		links = append(links, scriptdomain.StrategyEventLink{
			StoryEventID: link.StoryEventID,
			Treatment:    scriptdomain.EventTreatment(link.Treatment),
		})
	}
	record, err := service.CreateAdaptationStrategyVersion(b.context(), appscript.CreateAdaptationStrategyVersionRequest{
		EpisodeID:             request.EpisodeID,
		BasedOnVersionID:      request.BasedOnVersionID,
		StrategySummary:       request.StrategySummary,
		AdaptationMode:        scriptdomain.AdaptationMode(request.AdaptationMode),
		MergedEventGroupsJSON: request.MergedEventGroupsJSON,
		OriginalAdditions:     request.OriginalAdditions,
		Rationale:             request.Rationale,
		Risks:                 request.Risks,
		EventLinks:            links,
		CreatedByType:         versioning.CreatedByUser,
		ChangeReason:          request.ChangeReason,
	})
	if err != nil {
		return AdaptationStrategyVersionDTO{}, toDramaError(err)
	}
	return b.strategyDTO(b.context(), record)
}

// ListAdaptationStrategyVersions returns an episode's strategy versions newest first.
func (b *DramaBinding) ListAdaptationStrategyVersions(episodeID string) ([]AdaptationStrategyVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	history, err := service.ListVersions(b.context(), scriptdomain.FamilyAdaptationStrategy, episodeID)
	if err != nil {
		return nil, toDramaError(err)
	}
	versions, ok := history.([]scriptdomain.AdaptationStrategyVersion)
	if !ok {
		return nil, bindingUnavailable()
	}
	out := make([]AdaptationStrategyVersionDTO, 0, len(versions))
	for _, version := range versions {
		dto, err := b.strategyDTO(b.context(), version)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// ApproveAdaptationStrategyVersion makes one version the episode's approved strategy.
func (b *DramaBinding) ApproveAdaptationStrategyVersion(versionID string) (AdaptationStrategyVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return AdaptationStrategyVersionDTO{}, bindingUnavailable()
	}
	record, err := service.ApproveAdaptationStrategyVersion(b.context(), appscript.ApproveAdaptationStrategyVersionRequest{
		VersionID: versionID,
	})
	if err != nil {
		return AdaptationStrategyVersionDTO{}, toDramaError(err)
	}
	return b.strategyDTO(b.context(), record)
}

// strategyDTO converts one version, reading its treatments.
func (b *DramaBinding) strategyDTO(ctx context.Context, record scriptdomain.AdaptationStrategyVersion) (AdaptationStrategyVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return AdaptationStrategyVersionDTO{}, bindingUnavailable()
	}
	links, err := service.ListStrategyEventLinks(ctx, record.ID)
	if err != nil {
		return AdaptationStrategyVersionDTO{}, toDramaError(err)
	}
	dto := AdaptationStrategyVersionDTO{
		VersionID:             record.ID,
		EpisodeID:             record.EpisodeID,
		VersionNumber:         record.VersionNumber,
		Status:                string(record.Status),
		BasedOnVersionID:      record.BasedOnVersionID,
		StrategySummary:       record.StrategySummary,
		AdaptationMode:        string(record.AdaptationMode),
		MergedEventGroupsJSON: record.MergedEventGroupsJSON,
		OriginalAdditions:     record.OriginalAdditions,
		Rationale:             record.Rationale,
		Risks:                 record.Risks,
		SourceAgentRunID:      record.SourceAgentRunID,
		CreatedByType:         string(record.CreatedByType),
		CreatedByID:           record.CreatedByID,
		ChangeReason:          record.ChangeReason,
		CreatedAt:             record.CreatedAt.UTC().Format(rfc3339),
	}
	// The treatments are API-visible as an ordered list, which is the shape a UI renders.
	dto.EventLinks = make([]StrategyEventLinkDTO, 0, len(links))
	for _, link := range links {
		dto.EventLinks = append(dto.EventLinks, StrategyEventLinkDTO{
			StoryEventID: link.StoryEventID,
			Treatment:    string(link.Treatment),
			Ordinal:      link.Ordinal,
		})
	}
	return dto, nil
}

// ---------------------------------------------------------------------------
// Script structure
// ---------------------------------------------------------------------------

// DialogueLineDTO is one line of a scene.
type DialogueLineDTO struct {
	LineID             string `json:"lineId"`
	SceneID            string `json:"sceneId"`
	Ordinal            int    `json:"ordinal"`
	Type               string `json:"type"`
	CharacterEntityID  string `json:"characterEntityId,omitempty"`
	Text               string `json:"text"`
	Emotion            string `json:"emotion,omitempty"`
	PerformanceNote    string `json:"performanceNote,omitempty"`
	SourceStoryEventID string `json:"sourceStoryEventId,omitempty"`
	// Locked is §7.7's per-line pin. It is on the wire because a UI shows it and toggles it, and the
	// toggle is a command of its own rather than a field of an update.
	Locked bool `json:"locked"`
}

// SceneStructureShotDTO is one shot inside a scene.
type SceneStructureShotDTO struct {
	ShotID                   string `json:"shotId"`
	SceneID                  string `json:"sceneId"`
	Ordinal                  int    `json:"ordinal"`
	ShotNumber               string `json:"shotNumber,omitempty"`
	ShotSize                 string `json:"shotSize,omitempty"`
	CameraAngle              string `json:"cameraAngle,omitempty"`
	CameraMovement           string `json:"cameraMovement,omitempty"`
	EstimatedDurationSeconds int    `json:"estimatedDurationSeconds"`
	VisualDescription        string `json:"visualDescription,omitempty"`
	ActionDescription        string `json:"actionDescription,omitempty"`
	AudioIntent              string `json:"audioIntent,omitempty"`
	ContinuityNotes          string `json:"continuityNotes,omitempty"`
	Status                   string `json:"status"`
}

// SceneStructureDTO is one scene with its lines and shots.
type SceneStructureDTO struct {
	SceneID                  string                  `json:"sceneId"`
	ScriptVersionID          string                  `json:"scriptVersionId"`
	Ordinal                  int                     `json:"ordinal"`
	SceneNumber              string                  `json:"sceneNumber,omitempty"`
	Slugline                 string                  `json:"slugline"`
	InteriorExterior         string                  `json:"interiorExterior"`
	LocationEntityID         string                  `json:"locationEntityId,omitempty"`
	TimeOfDay                string                  `json:"timeOfDay,omitempty"`
	Summary                  string                  `json:"summary,omitempty"`
	DramaticGoal             string                  `json:"dramaticGoal,omitempty"`
	EstimatedDurationSeconds int                     `json:"estimatedDurationSeconds"`
	SourceStoryEventID       string                  `json:"sourceStoryEventId,omitempty"`
	IsOriginalAdaptation     bool                    `json:"isOriginalAdaptation"`
	DialogueLines            []DialogueLineDTO       `json:"dialogueLines"`
	Shots                    []SceneStructureShotDTO `json:"shots"`
}

// ScriptStructureDTO is a whole version's content.
type ScriptStructureDTO struct {
	ScriptVersionID          string              `json:"scriptVersionId"`
	Scenes                   []SceneStructureDTO `json:"scenes"`
	EstimatedDurationSeconds int                 `json:"estimatedDurationSeconds"`
}

// ScriptStructureLineInput is one line as the interface supplies it.
//
// It states no identifier and no ordinal, for the reason the tool's own schema does not: §17 puts both in
// the code's column, and this route is the USER's rather than a model's — a user editing a script through
// a form is not stating row ids either.
type ScriptStructureLineInput struct {
	Type               string `json:"type,omitempty"`
	CharacterEntityID  string `json:"characterEntityId,omitempty"`
	Text               string `json:"text,omitempty"`
	Emotion            string `json:"emotion,omitempty"`
	PerformanceNote    string `json:"performanceNote,omitempty"`
	SourceStoryEventID string `json:"sourceStoryEventId,omitempty"`
}

// ScriptStructureShotInput is one shot as the interface supplies it.
type ScriptStructureShotInput struct {
	ShotNumber               string `json:"shotNumber,omitempty"`
	ShotSize                 string `json:"shotSize,omitempty"`
	CameraAngle              string `json:"cameraAngle,omitempty"`
	CameraMovement           string `json:"cameraMovement,omitempty"`
	EstimatedDurationSeconds int    `json:"estimatedDurationSeconds,omitempty"`
	VisualDescription        string `json:"visualDescription,omitempty"`
	ActionDescription        string `json:"actionDescription,omitempty"`
	AudioIntent              string `json:"audioIntent,omitempty"`
	ContinuityNotes          string `json:"continuityNotes,omitempty"`
}

// ScriptStructureSceneInput is one scene as the interface supplies it.
type ScriptStructureSceneInput struct {
	SceneNumber              string                     `json:"sceneNumber,omitempty"`
	Slugline                 string                     `json:"slugline,omitempty"`
	InteriorExterior         string                     `json:"interiorExterior,omitempty"`
	LocationEntityID         string                     `json:"locationEntityId,omitempty"`
	TimeOfDay                string                     `json:"timeOfDay,omitempty"`
	Summary                  string                     `json:"summary,omitempty"`
	DramaticGoal             string                     `json:"dramaticGoal,omitempty"`
	EstimatedDurationSeconds int                        `json:"estimatedDurationSeconds,omitempty"`
	SourceStoryEventID       string                     `json:"sourceStoryEventId,omitempty"`
	IsOriginalAdaptation     bool                       `json:"isOriginalAdaptation,omitempty"`
	DialogueLines            []ScriptStructureLineInput `json:"dialogueLines,omitempty"`
	Shots                    []ScriptStructureShotInput `json:"shots,omitempty"`
}

// SaveScriptStructureRequest writes a whole script version's content.
type SaveScriptStructureRequest struct {
	ScriptVersionID string                      `json:"scriptVersionId"`
	ProjectID       string                      `json:"projectId,omitempty"`
	Scenes          []ScriptStructureSceneInput `json:"scenes"`
	Summary         string                      `json:"summary,omitempty"`
	ChangeReason    string                      `json:"changeReason,omitempty"`
}

// SaveScriptStructure writes a version's whole content.
//
// It is one call for the whole version, which is the same choice the tool layer made and for the same
// reason: a script version is an immutable artifact, and writing it in pieces would leave it observably
// half-written — with a duration that could not be summed until the last piece arrived. The DURATION is
// DERIVED here, from the scenes, exactly as it is for a model's write: no field of the request states it.
func (b *DramaBinding) SaveScriptStructure(request SaveScriptStructureRequest) (ScriptVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return ScriptVersionDTO{}, bindingUnavailable()
	}
	scenes := make([]scriptdomain.SceneDraft, 0, len(request.Scenes))
	for _, scene := range request.Scenes {
		interior := scriptdomain.InteriorExterior(scene.InteriorExterior)
		if interior == "" {
			interior = scriptdomain.InteriorOTHER
		}
		draft := scriptdomain.SceneDraft{
			SceneNumber:              scene.SceneNumber,
			Slugline:                 scene.Slugline,
			InteriorExterior:         interior,
			LocationEntityID:         scene.LocationEntityID,
			TimeOfDay:                scene.TimeOfDay,
			Summary:                  scene.Summary,
			DramaticGoal:             scene.DramaticGoal,
			EstimatedDurationSeconds: scene.EstimatedDurationSeconds,
			SourceStoryEventID:       scene.SourceStoryEventID,
			IsOriginalAdaptation:     scene.IsOriginalAdaptation,
			DialogueLines:            make([]scriptdomain.DialogueLineDraft, 0, len(scene.DialogueLines)),
			Shots:                    make([]scriptdomain.ShotDraft, 0, len(scene.Shots)),
		}
		for _, line := range scene.DialogueLines {
			lineType := scriptdomain.LineType(line.Type)
			if lineType == "" {
				lineType = scriptdomain.LineDialogue
			}
			draft.DialogueLines = append(draft.DialogueLines, scriptdomain.DialogueLineDraft{
				Type:               lineType,
				CharacterEntityID:  line.CharacterEntityID,
				Text:               line.Text,
				Emotion:            line.Emotion,
				PerformanceNote:    line.PerformanceNote,
				SourceStoryEventID: line.SourceStoryEventID,
			})
		}
		for _, shot := range scene.Shots {
			draft.Shots = append(draft.Shots, scriptdomain.ShotDraft{
				ShotNumber:               shot.ShotNumber,
				ShotSize:                 shot.ShotSize,
				CameraAngle:              shot.CameraAngle,
				CameraMovement:           shot.CameraMovement,
				EstimatedDurationSeconds: shot.EstimatedDurationSeconds,
				VisualDescription:        shot.VisualDescription,
				ActionDescription:        shot.ActionDescription,
				AudioIntent:              shot.AudioIntent,
				ContinuityNotes:          shot.ContinuityNotes,
			})
		}
		scenes = append(scenes, draft)
	}
	version, err := service.CreateScriptStructure(b.context(), appscript.CreateScriptStructureRequest{
		ScriptVersionID: request.ScriptVersionID,
		ProjectID:       request.ProjectID,
		Draft:           scriptdomain.ScriptStructureDraft{Scenes: scenes},
		Summary:         request.Summary,
		// The user's own edit, attributed to the user: §13.4's distinction is what lets a reviewer tell
		// which versions came from a model.
		CreatedByType: versioning.CreatedByUser,
		ChangeReason:  request.ChangeReason,
	})
	if err != nil {
		return ScriptVersionDTO{}, toDramaError(err)
	}
	return toScriptVersionDTO(version), nil
}

// ListScriptVersions returns an episode's script versions newest first.
//
// # Why this existed as an asymmetry rather than as a gap
//
// `ListStorySkeletonVersions` and `ListAdaptationStrategyVersions` have been here since WP-08, and the
// SCRIPT family's equivalent was not — so the one version family a user spends most of their time in
// was the one whose history no interface could read. `ScriptRepository.ListScriptVersions` existed the
// whole time, and `Service.ListVersions` has dispatched `FamilyScript` to it the whole time; only the
// binding was missing.
//
// What that cost is worth recording, because it was paid twice. The audio section's comment claimed
// "no read enumerates a script version's dialogue lines" and cited this absence as the reason a user
// had to TYPE a line identifier — the first half was false (`GetScriptStructure` lists lines) and the
// second was true only because this method did not exist. And a subtitle draft's script version was a
// text input for the same reason. The read is here now, so those two sections can resolve a version
// the way a person would: by picking one.
func (b *DramaBinding) ListScriptVersions(episodeID string) ([]ScriptVersionDTO, error) {
	service := b.scriptService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	// The family's parent is the EPISODE, not the script row: `Service.ListVersions` resolves the
	// script from the episode itself, which is what the two sibling methods do and why a caller never
	// has to know a script's identifier.
	history, err := service.ListVersions(b.context(), scriptdomain.FamilyScript, episodeID)
	if err != nil {
		return nil, toDramaError(err)
	}
	versions, ok := history.([]scriptdomain.ScriptVersion)
	if !ok {
		// A type that is not the family's own is a wiring fault rather than an empty history, and
		// answering with an empty list would show a user "no versions" for a script that has several.
		return nil, bindingUnavailable()
	}
	out := make([]ScriptVersionDTO, 0, len(versions))
	for _, version := range versions {
		out = append(out, toScriptVersionDTO(version))
	}
	return out, nil
}

// GetScriptStructure returns a version's whole content.
func (b *DramaBinding) GetScriptStructure(scriptVersionID string) (ScriptStructureDTO, error) {
	service := b.scriptService()
	if service == nil {
		return ScriptStructureDTO{}, bindingUnavailable()
	}
	structure, err := service.GetScriptStructure(b.context(), scriptVersionID)
	if err != nil {
		return ScriptStructureDTO{}, toDramaError(err)
	}
	return toScriptStructureDTO(structure), nil
}

// toScriptStructureDTO converts a whole version.
func toScriptStructureDTO(structure scriptdomain.ScriptStructure) ScriptStructureDTO {
	dto := ScriptStructureDTO{
		ScriptVersionID:          structure.ScriptVersionID,
		Scenes:                   make([]SceneStructureDTO, 0, len(structure.Scenes)),
		EstimatedDurationSeconds: structure.TotalDurationSeconds(),
	}
	for _, scene := range structure.Scenes {
		entry := SceneStructureDTO{
			SceneID:                  scene.ID,
			ScriptVersionID:          scene.ScriptVersionID,
			Ordinal:                  scene.Ordinal,
			SceneNumber:              scene.SceneNumber,
			Slugline:                 scene.Slugline,
			InteriorExterior:         string(scene.InteriorExterior),
			LocationEntityID:         scene.LocationEntityID,
			TimeOfDay:                scene.TimeOfDay,
			Summary:                  scene.Summary,
			DramaticGoal:             scene.DramaticGoal,
			EstimatedDurationSeconds: scene.EstimatedDurationSeconds,
			SourceStoryEventID:       scene.SourceStoryEventID,
			IsOriginalAdaptation:     scene.IsOriginalAdaptation,
			DialogueLines:            make([]DialogueLineDTO, 0, len(scene.DialogueLines)),
			Shots:                    make([]SceneStructureShotDTO, 0, len(scene.Shots)),
		}
		for _, line := range scene.DialogueLines {
			entry.DialogueLines = append(entry.DialogueLines, DialogueLineDTO{
				LineID:             line.ID,
				SceneID:            line.SceneID,
				Ordinal:            line.Ordinal,
				Type:               string(line.Type),
				CharacterEntityID:  line.CharacterEntityID,
				Text:               line.Text,
				Emotion:            line.Emotion,
				PerformanceNote:    line.PerformanceNote,
				SourceStoryEventID: line.SourceStoryEventID,
				Locked:             line.Locked,
			})
		}
		for _, shot := range scene.Shots {
			entry.Shots = append(entry.Shots, SceneStructureShotDTO{
				ShotID:                   shot.ID,
				SceneID:                  shot.SceneID,
				Ordinal:                  shot.Ordinal,
				ShotNumber:               shot.ShotNumber,
				ShotSize:                 shot.ShotSize,
				CameraAngle:              shot.CameraAngle,
				CameraMovement:           shot.CameraMovement,
				EstimatedDurationSeconds: shot.EstimatedDurationSeconds,
				VisualDescription:        shot.VisualDescription,
				ActionDescription:        shot.ActionDescription,
				AudioIntent:              shot.AudioIntent,
				ContinuityNotes:          shot.ContinuityNotes,
				Status:                   string(shot.Status),
			})
		}
		dto.Scenes = append(dto.Scenes, entry)
	}
	return dto
}

// ---------------------------------------------------------------------------
// Field locks
// ---------------------------------------------------------------------------

// FieldLockDTO is one pin (AC-SCRIPT-002).
type FieldLockDTO struct {
	VersionID string `json:"versionId"`
	Family    string `json:"family"`
	Field     string `json:"field"`
	LockedBy  string `json:"lockedBy,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// LockableFieldDTO is one field a family admits, so a UI can offer the right checkboxes rather than a
// hard-coded list that drifts from the domain's vocabulary.
type LockableFieldDTO struct {
	Family string `json:"family"`
	Field  string `json:"field"`
}

// LockableFields returns every field the three families admit.
//
// It reads the DOMAIN's own vocabulary rather than a list written here, so a field added to the domain is
// immediately offerable and a field removed stops being offered — the drift a hard-coded list would have.
func (b *DramaBinding) LockableFields() ([]LockableFieldDTO, error) {
	out := []LockableFieldDTO{}
	for _, family := range scriptdomain.VersionFamilies() {
		for _, field := range scriptdomain.LockableFields(family) {
			out = append(out, LockableFieldDTO{Family: string(family), Field: string(field)})
		}
	}
	return out, nil
}

// ListScriptFieldLocks returns one version's pins.
func (b *DramaBinding) ListScriptFieldLocks(versionID string) ([]FieldLockDTO, error) {
	service := b.scriptService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	locks, err := service.ListScriptFieldLocks(b.context(), versionID)
	if err != nil {
		return nil, toDramaError(err)
	}
	out := make([]FieldLockDTO, 0, len(locks))
	for _, lock := range locks {
		out = append(out, FieldLockDTO{
			VersionID: lock.VersionID,
			Family:    string(lock.Family),
			Field:     string(lock.Field),
			LockedBy:  lock.LockedBy,
			CreatedAt: lock.CreatedAt,
		})
	}
	return out, nil
}

// LockScriptField pins one field of one version.
//
// The family is taken from the VERSION rather than from the caller, which is what keeps the lock table
// honest: it cannot be a foreign key across three version tables, so the service reads the version first
// and states its family — a caller that named the wrong one would otherwise record a lock no diff would
// report.
func (b *DramaBinding) LockScriptField(versionID string, field string, lockedBy string) ([]FieldLockDTO, error) {
	service := b.scriptService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	if err := service.LockScriptField(b.context(), appscript.LockScriptFieldRequest{
		VersionID: versionID,
		Field:     scriptdomain.LockableField(field),
		LockedBy:  lockedBy,
	}); err != nil {
		return nil, toDramaError(err)
	}
	return b.ListScriptFieldLocks(versionID)
}

// UnlockScriptField releases a pin.
func (b *DramaBinding) UnlockScriptField(versionID string, field string) ([]FieldLockDTO, error) {
	service := b.scriptService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	if err := service.UnlockScriptField(b.context(), versionID, scriptdomain.LockableField(field)); err != nil {
		return nil, toDramaError(err)
	}
	return b.ListScriptFieldLocks(versionID)
}

// SetDialogueLineLockedRequest pins or releases one dialogue line.
type SetDialogueLineLockedRequest struct {
	LineID   string `json:"lineId"`
	Locked   bool   `json:"locked"`
	Revision int64  `json:"revision"`
}

// SetDialogueLineLocked pins or releases one line.
//
// The revision travels because the line row has one: a second window's toggle would otherwise overwrite
// the first's, and the user would see a lock they did not set.
func (b *DramaBinding) SetDialogueLineLocked(request SetDialogueLineLockedRequest) (DialogueLineDTO, error) {
	service := b.scriptService()
	if service == nil {
		return DialogueLineDTO{}, bindingUnavailable()
	}
	line, err := service.SetDialogueLineLocked(b.context(), appscript.SetDialogueLineLockedRequest{
		LineID:   request.LineID,
		Locked:   request.Locked,
		Revision: request.Revision,
	})
	if err != nil {
		return DialogueLineDTO{}, toDramaError(err)
	}
	return DialogueLineDTO{
		LineID:             line.ID,
		SceneID:            line.SceneID,
		Ordinal:            line.Ordinal,
		Type:               string(line.Type),
		CharacterEntityID:  line.CharacterEntityID,
		Text:               line.Text,
		Emotion:            line.Emotion,
		PerformanceNote:    line.PerformanceNote,
		SourceStoryEventID: line.SourceStoryEventID,
		Locked:             line.Locked,
	}, nil
}

// ---------------------------------------------------------------------------
// Version diff
// ---------------------------------------------------------------------------

// FieldChangeDTO is one field-level difference.
type FieldChangeDTO struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// ItemChangeDTO is one item's change.
type ItemChangeDTO struct {
	Kind    string           `json:"kind"`
	Ordinal int              `json:"ordinal"`
	ItemID  string           `json:"itemId,omitempty"`
	Fields  []FieldChangeDTO `json:"fields,omitempty"`
	// Locked is whether the item was pinned, which is the field that says a locked thing MOVED — the
	// case AC-SCRIPT-002 exists to make visible.
	Locked bool `json:"locked"`
}

// VersionDiffDTO is what changed between two versions.
type VersionDiffDTO struct {
	Family    string           `json:"family"`
	FromID    string           `json:"fromId"`
	ToID      string           `json:"toId"`
	Fields    []FieldChangeDTO `json:"fields,omitempty"`
	Items     []ItemChangeDTO  `json:"items,omitempty"`
	Added     int              `json:"added"`
	Removed   int              `json:"removed"`
	Modified  int              `json:"modified"`
	Unchanged int              `json:"unchanged"`
}

// DiffVersions compares two versions of one artifact.
func (b *DramaBinding) DiffVersions(family string, fromID string, toID string) (VersionDiffDTO, error) {
	service := b.scriptService()
	if service == nil {
		return VersionDiffDTO{}, bindingUnavailable()
	}
	diff, err := service.DiffVersions(b.context(), scriptdomain.VersionFamily(family), fromID, toID)
	if err != nil {
		return VersionDiffDTO{}, toDramaError(err)
	}
	dto := VersionDiffDTO{
		Family:    string(diff.Family),
		FromID:    diff.FromID,
		ToID:      diff.ToID,
		Fields:    make([]FieldChangeDTO, 0, len(diff.Fields)),
		Items:     make([]ItemChangeDTO, 0, len(diff.Items)),
		Added:     diff.Added,
		Removed:   diff.Removed,
		Modified:  diff.Modified,
		Unchanged: diff.Unchanged,
	}
	for _, change := range diff.Fields {
		dto.Fields = append(dto.Fields, FieldChangeDTO{
			Field: change.Field, Before: change.Before, After: change.After,
		})
	}
	for _, item := range diff.Items {
		entry := ItemChangeDTO{
			Kind:    string(item.Kind),
			Ordinal: item.Ordinal,
			ItemID:  item.ID,
			Locked:  item.Locked,
		}
		for _, change := range item.Fields {
			entry.Fields = append(entry.Fields, FieldChangeDTO{
				Field: change.Field, Before: change.Before, After: change.After,
			})
		}
		dto.Items = append(dto.Items, entry)
	}
	return dto, nil
}

// ---------------------------------------------------------------------------
// Canvas projection
// ---------------------------------------------------------------------------

// ProjectScriptVersionRequest asks to project a version's scenes onto a canvas.
type ProjectScriptVersionRequest struct {
	ProjectID       string `json:"projectId"`
	ScriptVersionID string `json:"scriptVersionId"`
}

// ProjectScriptVersionResult reports what the projection wrote.
type ProjectScriptVersionResult struct {
	SceneNodeIDs []string `json:"sceneNodeIds"`
}

// ProjectScriptVersion projects a version's scenes onto the project's canvas.
//
// The projector is optional in the service and its absence is a REFUSAL rather than a silent no-op, so a
// build assembled without one answers this with an error a caller can see. That is the direction the
// service chose and this binding passes through unchanged: a projection that reported success and wrote
// nothing would show a user an empty board.
func (b *DramaBinding) ProjectScriptVersion(request ProjectScriptVersionRequest) (ProjectScriptVersionResult, error) {
	service := b.scriptService()
	if service == nil {
		return ProjectScriptVersionResult{}, bindingUnavailable()
	}
	result, err := service.ProjectScriptVersion(b.context(), appscript.ProjectScriptVersionRequest{
		ProjectID:       request.ProjectID,
		ScriptVersionID: request.ScriptVersionID,
	})
	if err != nil {
		return ProjectScriptVersionResult{}, toDramaError(err)
	}
	return ProjectScriptVersionResult{SceneNodeIDs: result.SceneNodeIDs}, nil
}

// ---------------------------------------------------------------------------
// Stage orchestration and the user's gate
// ---------------------------------------------------------------------------

// StagePipeline is what the binding drives a stage through.
//
// It is an INTERFACE rather than the pipeline's concrete type, and that is a wiring decision rather than a
// testing accommodation: a binding that named the concrete type would make the desktop package depend on
// the pipeline's Options and its whole dependency graph, so a change inside the pipeline would be a change
// to the Wails surface. The interface states exactly the five commands the interface offers.
//
// A nil pipeline is the ordinary state of a build without an agent stack, and every method below then
// refuses. That direction is deliberate: a stage that reported success without running would be a workflow
// advancing on nothing.
// The five commands are declared with the GENERIC mechanism's request and result types,
// because a production pipeline satisfies this interface too — that is what makes one
// binding serve both layers. The only script-specific member is `ManualEdit`, whose
// payload is the script layer's own; a production pipeline implements it by asserting its
// own payload, and the mechanism refuses a payload the stage cannot use.
type StagePipeline interface {
	// RunStage starts one attempt and runs its execution agent.
	RunStage(ctx context.Context, request stagepipeline.StageRequest) (stagepipeline.StageResult, error)
	// RunSupervision reviews one attempt and applies the verdict.
	RunSupervision(ctx context.Context, request stagepipeline.SupervisionRequest) (stagepipeline.SupervisionResult, error)
	// ApplyUserGate records the user's decision and moves the stage.
	ApplyUserGate(ctx context.Context, request stagepipeline.GateRequest) (workflow.StageRun, error)
	// StartRevision begins the attempt a FIX or REDO asked for.
	StartRevision(ctx context.Context, stageRunID string) (workflow.StageRun, error)
	// ManualEdit writes the user's own version and passes the stage.
	//
	// It takes the MECHANISM's request rather than a layer's, because one binding serves
	// both pipelines and the payload is the only part that differs: the script layer's is a
	// structure, a skeleton or a strategy, and the production layer's would be a plan, a
	// board or a panel. The layer asserts the payload it can use and refuses the rest, which
	// is why the field is `any` — a compiled contract for one layer would be a second
	// interface for every other.
	ManualEdit(ctx context.Context, request stagepipeline.ManualEditRequest) (stagepipeline.StageResult, error)
}

// AttachPipeline supplies the stage pipeline. A nil one leaves the stage commands failing closed.
func AttachPipeline(binding *DramaBinding, ctx context.Context, pipeline StagePipeline) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.pipeline = pipeline
	binding.mu.Unlock()
}

// RunScriptStageRequest asks for one attempt at one script stage.
type RunScriptStageRequest struct {
	WorkflowRunID string `json:"workflowRunId"`
	Stage         string `json:"stage"`
	ProjectID     string `json:"projectId"`
	EpisodeID     string `json:"episodeId"`
	// Task is what the attempt is asked to do. It is DOCUMENT-ADJACENT text for a stage that reads the
	// source novel, so the caller states whether it is untrusted text.
	Task            string `json:"task,omitempty"`
	TaskIsUntrusted bool   `json:"taskIsUntrusted,omitempty"`
	UserMessage     string `json:"userMessage,omitempty"`
	// FixFromStageRunID makes this attempt a FIX: the findings the user named on that attempt's decision
	// are read back from the database by the pipeline, not passed here.
	FixFromStageRunID string `json:"fixFromStageRunId,omitempty"`
	// The version identifiers the stage's state layer carries, so its tools can name what they write.
	//
	// The first four are the SCRIPT stages' and the rest are the production stages'. They travel
	// on one request because the two layers share this command — WP-09 extracted the mechanism so
	// one binding serves both — and which fields matter is decided by the STAGE, not by the
	// caller: the pipeline that drives the stage is what reads the ones it needs.
	SkeletonVersionID string   `json:"skeletonVersionId,omitempty"`
	StrategyVersionID string   `json:"strategyVersionId,omitempty"`
	ScriptVersionID   string   `json:"scriptVersionId,omitempty"`
	SelectedEventIDs  []string `json:"selectedEventIds,omitempty"`
	// DirectorPlanVersionID and StoryboardVersionID name the upstream artifacts a production
	// stage writes from.
	DirectorPlanVersionID string `json:"directorPlanVersionId,omitempty"`
	StoryboardVersionID   string `json:"storyboardVersionId,omitempty"`
	// StoryboardID is the episode's board identity, for the stage that writes a version of it.
	StoryboardID string `json:"storyboardId,omitempty"`
	// StoryboardItemID is the row a panel stage writes for.
	StoryboardItemID string `json:"storyboardItemId,omitempty"`
	// AssetGapReportID names the analysis a generation plan works from.
	AssetGapReportID string `json:"assetGapReportId,omitempty"`
	// ShotIDs are the shots a board stage must board, in the script's order.
	//
	// THEY ARE LOAD-BEARING rather than a convenience: the board write tool checks every row's
	// citation against the script's own shots, so a stage whose state named none would have every
	// row refused — and the refusal would name a shot problem rather than a missing field.
	ShotIDs []string `json:"shotIds,omitempty"`
	// ModelID and ProviderID name the model this attempt runs on. Empty means the project's policy for the
	// agent's layer decides, which is §13's rule.
	ModelID    string `json:"modelId,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
}

// ScriptStageResultDTO reports what one attempt produced.
type ScriptStageResultDTO struct {
	StageRunID string `json:"stageRunId"`
	Status     string `json:"status"`
	Attempt    int    `json:"attempt"`
	AgentRunID string `json:"agentRunId"`
	// ArtifactIDs are the rows the stage's tool calls wrote, read from the recorded calls rather than from
	// the model's answer.
	ArtifactIDs []string `json:"artifactIds"`
	Repaired    bool     `json:"repaired"`
	Summary     string   `json:"summary,omitempty"`
}

// RunScriptStage runs one attempt at one script stage.
func (b *DramaBinding) RunScriptStage(request RunScriptStageRequest) (ScriptStageResultDTO, error) {
	pipeline := b.pipelineFor(request.Stage)
	if pipeline == nil {
		return ScriptStageResultDTO{}, bindingUnavailable()
	}
	result, err := pipeline.RunStage(b.context(), stagepipeline.StageRequest{
		WorkflowRunID:     request.WorkflowRunID,
		Stage:             appscriptpipeline.Stage(request.Stage),
		ProjectID:         request.ProjectID,
		EpisodeID:         request.EpisodeID,
		Task:              request.Task,
		TaskIsUntrusted:   request.TaskIsUntrusted,
		UserMessage:       request.UserMessage,
		FixFromStageRunID: request.FixFromStageRunID,
		ModelID:           request.ModelID,
		ProviderID:        request.ProviderID,
		// THE STATE IS THE LAYER'S, and it is built by the pipeline that drives the stage rather
		// than here. The two layers carry different fields — a script stage names a skeleton and
		// a strategy, a production stage names a plan and a board — and a binding that built one
		// layer's struct would leave the other layer's stage with NO state at all, which is what
		// the first version of this code did: a `storyboard_table` run from the UI had no
		// `shot_ids`, so every row it wrote was refused for citing a shot that "does not belong
		// to this script".
		//
		// The identifier fields are passed as a plain map because that is what crosses a layer
		// boundary; each pipeline converts the ones its layer reads.
		State: stageStateFor(request),
	})
	if err != nil {
		return ScriptStageResultDTO{}, toDramaError(err)
	}
	return ScriptStageResultDTO{
		StageRunID:  result.StageRun.ID,
		Status:      string(result.StageRun.Status),
		Attempt:     result.StageRun.Attempt,
		AgentRunID:  result.Outcome.RunID,
		ArtifactIDs: result.ArtifactIDs,
		Repaired:    result.Outcome.Repaired,
		Summary:     result.Outcome.Summary,
	}, nil
}

// stageStateFor builds the prompt's state for whichever layer drives a stage.
//
// It returns ONE of the two layers' structs, chosen by the stage, and the mechanism passes it
// through opaquely to the layer that reads it. A single struct holding both layers' fields
// would make each layer's renderer responsible for ignoring the other's — and the first field
// a renderer forgot to ignore would be an identifier a tool then wrote about.
func stageStateFor(request RunScriptStageRequest) any {
	if appproductionpipeline.IsProductionStage(request.Stage) {
		return appproductionpipeline.StateFields{
			EpisodeID:             request.EpisodeID,
			ScriptVersionID:       request.ScriptVersionID,
			DirectorPlanVersionID: request.DirectorPlanVersionID,
			StoryboardID:          request.StoryboardID,
			StoryboardVersionID:   request.StoryboardVersionID,
			StoryboardItemID:      request.StoryboardItemID,
			AssetGapReportID:      request.AssetGapReportID,
			ShotIDs:               request.ShotIDs,
		}
	}
	return appscriptpipeline.StateFields{
		SkeletonVersionID: request.SkeletonVersionID,
		StrategyVersionID: request.StrategyVersionID,
		ScriptVersionID:   request.ScriptVersionID,
		SelectedEventIDs:  request.SelectedEventIDs,
	}
}

// RunScriptSupervisionRequest asks for a review of one attempt.
type RunScriptSupervisionRequest struct {
	StageRunID        string `json:"stageRunId"`
	ProjectID         string `json:"projectId"`
	EpisodeID         string `json:"episodeId"`
	ArtifactVersionID string `json:"artifactVersionId,omitempty"`
	ModelID           string `json:"modelId,omitempty"`
	ProviderID        string `json:"providerId,omitempty"`
}

// RunScriptSupervision reviews one attempt and applies the report to the stage.
func (b *DramaBinding) RunScriptSupervision(request RunScriptSupervisionRequest) (ReviewReportDTO, error) {
	pipeline := b.pipelineForStageRun(request.StageRunID)
	if pipeline == nil {
		return ReviewReportDTO{}, bindingUnavailable()
	}
	result, err := pipeline.RunSupervision(b.context(), stagepipeline.SupervisionRequest{
		StageRunID:        request.StageRunID,
		ProjectID:         request.ProjectID,
		EpisodeID:         request.EpisodeID,
		ArtifactVersionID: request.ArtifactVersionID,
		ModelID:           request.ModelID,
		ProviderID:        request.ProviderID,
	})
	if err != nil {
		return ReviewReportDTO{}, toDramaError(err)
	}
	dto, err := b.GetReviewReport(result.StageRun.ID)
	if err != nil {
		return ReviewReportDTO{}, err
	}
	return dto, nil
}

// ApplyScriptGateRequest is a user's decision about one script stage attempt.
type ApplyScriptGateRequest struct {
	StageRunID string `json:"stageRunId"`
	Decision   string `json:"decision"`
	// ArtifactVersionID names the version an approving decision puts in force. Required for approve,
	// manual_edit and skip: moving a stage and approving an artifact are two acts, and "approved 唯一" is
	// about the second.
	ArtifactVersionID string `json:"artifactVersionId,omitempty"`
	// IssueIDsJSON and LockedEntityRefsJSON are what a FIX must address and what the user pinned, in the
	// shapes §12.2's decision row stores. They are JSON strings rather than typed fields because the
	// pipeline stores what the user's command produced rather than re-encoding it.
	IssueIDsJSON         string `json:"issueIdsJson,omitempty"`
	LockedEntityRefsJSON string `json:"lockedEntityRefsJson,omitempty"`
	Instruction          string `json:"instruction,omitempty"`
	Reason               string `json:"reason,omitempty"`
	CreatedByID          string `json:"createdById,omitempty"`
}

// ApplyScriptGate records a user's decision and moves the stage.
func (b *DramaBinding) ApplyScriptGate(request ApplyScriptGateRequest) (StageRunDTO, error) {
	pipeline := b.pipelineForStageRun(request.StageRunID)
	if pipeline == nil {
		return StageRunDTO{}, bindingUnavailable()
	}
	moved, err := pipeline.ApplyUserGate(b.context(), stagepipeline.GateRequest{
		StageRunID:           request.StageRunID,
		Decision:             workflow.GateDecision(request.Decision),
		ArtifactVersionID:    request.ArtifactVersionID,
		IssueIDsJSON:         request.IssueIDsJSON,
		LockedEntityRefsJSON: request.LockedEntityRefsJSON,
		Instruction:          request.Instruction,
		Reason:               request.Reason,
		CreatedByID:          request.CreatedByID,
	})
	if err != nil {
		return StageRunDTO{}, toDramaError(err)
	}
	return toStageRunDTO(moved), nil
}

// StartScriptRevision begins the attempt a FIX or REDO asked for.
//
// It is separate from the gate because it runs a model: a user who decided to fix something has not
// thereby decided to spend a run, and a UI that submitted a decision should not silently start one.
func (b *DramaBinding) StartScriptRevision(stageRunID string) (StageRunDTO, error) {
	pipeline := b.pipelineForStageRun(stageRunID)
	if pipeline == nil {
		return StageRunDTO{}, bindingUnavailable()
	}
	moved, err := pipeline.StartRevision(b.context(), stageRunID)
	if err != nil {
		return StageRunDTO{}, toDramaError(err)
	}
	return toStageRunDTO(moved), nil
}

// ManualEditScriptRequest is a user's own version of a script stage's artifact.
//
// It carries three shapes because the three stages produce three kinds of artifact, and exactly one is
// used — chosen by the STAGE rather than by which field the caller filled in. A caller that supplied a
// skeleton for the script stage has used the wrong request, and inferring from what was populated would
// write a skeleton version into a script stage.
type ManualEditScriptRequest struct {
	StageRunID        string `json:"stageRunId"`
	Stage             string `json:"stage"`
	ProjectID         string `json:"projectId"`
	EpisodeID         string `json:"episodeId"`
	BasedOnVersionID  string `json:"basedOnVersionId,omitempty"`
	SkeletonVersionID string `json:"skeletonVersionId,omitempty"`
	StrategyVersionID string `json:"strategyVersionId,omitempty"`
	// Skeleton and Strategy are the two upstream artifacts' content, for the stages whose artifact is a
	// row rather than a structure.
	Skeleton *EditSkeletonInput `json:"skeleton,omitempty"`
	Strategy *EditStrategyInput `json:"strategy,omitempty"`
	// Scenes is the script stage's content, in the same shape `SaveScriptStructure` takes.
	Scenes       []ScriptStructureSceneInput `json:"scenes,omitempty"`
	Summary      string                      `json:"summary,omitempty"`
	ChangeReason string                      `json:"changeReason,omitempty"`
	CreatedByID  string                      `json:"createdById,omitempty"`
}

// EditSkeletonInput is a user's skeleton version.
type EditSkeletonInput struct {
	OpeningHook              string   `json:"openingHook,omitempty"`
	CoreConflict             string   `json:"coreConflict,omitempty"`
	TurningPointsJSON        string   `json:"turningPointsJson,omitempty"`
	Climax                   string   `json:"climax,omitempty"`
	EndingHook               string   `json:"endingHook,omitempty"`
	EstimatedDurationSeconds int      `json:"estimatedDurationSeconds,omitempty"`
	SelectedEventIDs         []string `json:"selectedEventIds,omitempty"`
}

// EditStrategyInput is a user's strategy version.
type EditStrategyInput struct {
	StrategySummary       string                 `json:"strategySummary,omitempty"`
	AdaptationMode        string                 `json:"adaptationMode,omitempty"`
	MergedEventGroupsJSON string                 `json:"mergedEventGroupsJson,omitempty"`
	OriginalAdditions     string                 `json:"originalAdditions,omitempty"`
	Rationale             string                 `json:"rationale,omitempty"`
	Risks                 string                 `json:"risks,omitempty"`
	EventLinks            []StrategyEventLinkDTO `json:"eventLinks,omitempty"`
}

// ManualEditScript writes the user's own version and passes the stage.
func (b *DramaBinding) ManualEditScript(request ManualEditScriptRequest) (ScriptStageResultDTO, error) {
	pipeline := b.pipelineFor(request.Stage)
	if pipeline == nil {
		return ScriptStageResultDTO{}, bindingUnavailable()
	}
	// The payload travels as the SCRIPT layer's own struct, carried in the mechanism's
	// opaque field: `appscriptpipeline.ManualEdit` is what knows how to build it, and this
	// binding is what knows which DTO the form supplied.
	payload := appscriptpipeline.ManualEditPayload{
		SkeletonVersionID: request.SkeletonVersionID,
		StrategyVersionID: request.StrategyVersionID,
	}
	edit := stagepipeline.ManualEditRequest{
		StageRunID:       request.StageRunID,
		Stage:            appscriptpipeline.Stage(request.Stage),
		ProjectID:        request.ProjectID,
		EpisodeID:        request.EpisodeID,
		BasedOnVersionID: request.BasedOnVersionID,
		Summary:          request.Summary,
		ChangeReason:     request.ChangeReason,
		CreatedByID:      request.CreatedByID,
		Payload:          payload,
	}
	if request.Skeleton != nil {
		skeleton := &appscript.CreateStorySkeletonVersionRequest{
			EpisodeID:                request.EpisodeID,
			OpeningHook:              request.Skeleton.OpeningHook,
			CoreConflict:             request.Skeleton.CoreConflict,
			TurningPointsJSON:        request.Skeleton.TurningPointsJSON,
			Climax:                   request.Skeleton.Climax,
			EndingHook:               request.Skeleton.EndingHook,
			EstimatedDurationSeconds: request.Skeleton.EstimatedDurationSeconds,
			SelectedEventIDs:         request.Skeleton.SelectedEventIDs,
		}
		payload.Skeleton = skeleton
	}
	if request.Strategy != nil {
		links := make([]scriptdomain.StrategyEventLink, 0, len(request.Strategy.EventLinks))
		for _, link := range request.Strategy.EventLinks {
			links = append(links, scriptdomain.StrategyEventLink{
				StoryEventID: link.StoryEventID,
				Treatment:    scriptdomain.EventTreatment(link.Treatment),
			})
		}
		strategy := &appscript.CreateAdaptationStrategyVersionRequest{
			EpisodeID:             request.EpisodeID,
			StrategySummary:       request.Strategy.StrategySummary,
			AdaptationMode:        scriptdomain.AdaptationMode(request.Strategy.AdaptationMode),
			MergedEventGroupsJSON: request.Strategy.MergedEventGroupsJSON,
			OriginalAdditions:     request.Strategy.OriginalAdditions,
			Rationale:             request.Strategy.Rationale,
			Risks:                 request.Strategy.Risks,
			EventLinks:            links,
		}
		payload.Strategy = strategy
	}
	if len(request.Scenes) > 0 {
		payload.Structure = scriptdomain.ScriptStructureDraft{Scenes: sceneDraftsFromInput(request.Scenes)}
	}
	// The payload is attached AFTER the three branches, because which one was populated is
	// what the layer reads: assigning it inside a branch would leave the other two unable to
	// reach it, and the layer refuses a stage whose payload is missing.
	edit.Payload = payload
	result, err := pipeline.ManualEdit(b.context(), edit)
	if err != nil {
		return ScriptStageResultDTO{}, toDramaError(err)
	}
	return ScriptStageResultDTO{
		StageRunID:  result.StageRun.ID,
		Status:      string(result.StageRun.Status),
		Attempt:     result.StageRun.Attempt,
		AgentRunID:  result.Outcome.RunID,
		ArtifactIDs: result.ArtifactIDs,
	}, nil
}

// stagePipeline reads the pipeline under the lock.
func (b *DramaBinding) stagePipeline() StagePipeline {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pipeline
}

// productionStagePipeline returns the production layer's pipeline, or nil.
func (b *DramaBinding) productionStagePipeline() StagePipeline {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.productionPipeline
}

// AttachProductionPipeline supplies the production layer's pipeline.
//
// It is a separate attachment from `AttachPipeline` because the two answer for disjoint
// stage sets: WP-09's extraction made them share a MECHANISM, not a stage list, and a
// binding that held one slot would route a production stage to the script layer — which
// refuses it, so the caller would see a refusal that names the wrong problem.
func AttachProductionPipeline(binding *DramaBinding, ctx context.Context, pipeline StagePipeline) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.productionPipeline = pipeline
	binding.mu.Unlock()
}

// pipelineForStageRun returns the pipeline that drives the stage an attempt belongs to.
//
// The three attempt-scoped commands (`RunSupervision`, `ApplyUserGate`, `StartRevision`)
// name a STAGE RUN rather than a stage, so the layer has to be discovered from the record.
// The read is the workflow service's own, which is already attached; a failure to read it
// yields nil and the command then refuses with the binding's usual message.
//
// The alternative — remembering which pipeline started the attempt — would be a second
// answer to a question the database already holds, and it would be wrong after a restart.
func (b *DramaBinding) pipelineForStageRun(stageRunID string) StagePipeline {
	workflow := b.workflowService()
	if workflow == nil {
		return nil
	}
	attempt, err := workflow.GetStage(b.context(), strings.TrimSpace(stageRunID))
	if err != nil {
		return nil
	}
	return b.pipelineFor(string(attempt.Stage))
}

// pipelineFor returns the pipeline that drives a stage, or nil when neither does.
//
// THE STAGE DECIDES, not the caller and not the order things were attached in. A stage
// neither layer drives yields nil, and the command then refuses with the binding's usual
// message rather than running the wrong layer's stage machine.
func (b *DramaBinding) pipelineFor(stage string) StagePipeline {
	if appscriptpipeline.IsScriptStage(stage) {
		return b.stagePipeline()
	}
	if appproductionpipeline.IsProductionStage(stage) {
		return b.productionStagePipeline()
	}
	// An unknown stage goes to the SCRIPT pipeline when it exists, so the refusal names the
	// stage rather than a missing pipeline: a caller that misspelled a stage should learn
	// that, not that the build has no agent stack.
	if pipeline := b.stagePipeline(); pipeline != nil {
		return pipeline
	}
	return b.productionStagePipeline()
}

// sceneDraftsFromInput converts the binding's scene inputs into the domain's drafts.
//
// One conversion rather than two: `SaveScriptStructure` and `ManualEditScript` take the same scene shape,
// and two copies would be two chances for the manual-edit path to stop defaulting a line type.
func sceneDraftsFromInput(scenes []ScriptStructureSceneInput) []scriptdomain.SceneDraft {
	out := make([]scriptdomain.SceneDraft, 0, len(scenes))
	for _, scene := range scenes {
		interior := scriptdomain.InteriorExterior(scene.InteriorExterior)
		if interior == "" {
			interior = scriptdomain.InteriorOTHER
		}
		draft := scriptdomain.SceneDraft{
			SceneNumber:              scene.SceneNumber,
			Slugline:                 scene.Slugline,
			InteriorExterior:         interior,
			LocationEntityID:         scene.LocationEntityID,
			TimeOfDay:                scene.TimeOfDay,
			Summary:                  scene.Summary,
			DramaticGoal:             scene.DramaticGoal,
			EstimatedDurationSeconds: scene.EstimatedDurationSeconds,
			SourceStoryEventID:       scene.SourceStoryEventID,
			IsOriginalAdaptation:     scene.IsOriginalAdaptation,
			DialogueLines:            make([]scriptdomain.DialogueLineDraft, 0, len(scene.DialogueLines)),
			Shots:                    make([]scriptdomain.ShotDraft, 0, len(scene.Shots)),
		}
		for _, line := range scene.DialogueLines {
			lineType := scriptdomain.LineType(line.Type)
			if lineType == "" {
				lineType = scriptdomain.LineDialogue
			}
			draft.DialogueLines = append(draft.DialogueLines, scriptdomain.DialogueLineDraft{
				Type:               lineType,
				CharacterEntityID:  line.CharacterEntityID,
				Text:               line.Text,
				Emotion:            line.Emotion,
				PerformanceNote:    line.PerformanceNote,
				SourceStoryEventID: line.SourceStoryEventID,
			})
		}
		for _, shot := range scene.Shots {
			draft.Shots = append(draft.Shots, scriptdomain.ShotDraft{
				ShotNumber:               shot.ShotNumber,
				ShotSize:                 shot.ShotSize,
				CameraAngle:              shot.CameraAngle,
				CameraMovement:           shot.CameraMovement,
				EstimatedDurationSeconds: shot.EstimatedDurationSeconds,
				VisualDescription:        shot.VisualDescription,
				ActionDescription:        shot.ActionDescription,
				AudioIntent:              shot.AudioIntent,
				ContinuityNotes:          shot.ContinuityNotes,
			})
		}
		out = append(out, draft)
	}
	return out
}
