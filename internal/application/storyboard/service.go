package storyboard

import (
	"context"
	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// MaxBatchCandidates bounds the candidate list one approval may name.
//
// Candidacy is a property of the panel's generation history, not of the version
// row (see storyboard.StoryboardPanelVersion.CanApproveImage), so the list is
// supplied per call. The bound keeps a malformed request from making one call
// walk an unbounded slice.
const MaxBatchCandidates = 5000

// NewService builds the storyboard service.
func NewService(options Options) *Service {
	return &Service{
		directorPlans: options.DirectorPlans,
		storyboards:   options.Storyboards,
		items:         options.Items,
		panels:        options.Panels,
		clock:         options.Clock,
		ids:           options.IDs,
		events:        options.Events,
	}
}

// Available reports whether the service has the dependencies it needs. An
// unattached binding fails closed rather than panicking.
func (s *Service) Available() bool {
	return s != nil && s.directorPlans != nil && s.storyboards != nil && s.items != nil && s.panels != nil && s.ids != nil
}

// recordEvent announces something that happened, if the service has a recorder.
//
// The nil check is not defensive padding: the recorder is an interface, so a
// Service composed without one holds a nil interface and calling a method on it
// panics. This is the one place that check lives, so no emit site has to repeat
// it and no emit site can forget it.
func (s *Service) recordEvent(ctx context.Context, draft eventsapp.Draft) {
	if s == nil || s.events == nil {
		return
	}
	s.events.RecordBestEffort(ctx, draft)
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// storageFailure is the fail-closed error for an unattached service.
func storageFailure() error {
	return storyboard.StorageError("The storyboard store is unavailable.", nil)
}

// producer returns the version producer a request did not name.
//
// DOMAIN_MODEL section 2.5 attaches a producer to every version. A caller that
// omits it is the local user acting in the UI, so the default is the user
// rather than a value claiming a migration or an agent wrote the version.
func producer(value versioning.CreatedByType) versioning.CreatedByType {
	if value == "" {
		return versioning.CreatedByUser
	}
	return value
}

// CreateDirectorPlanVersionRequest carries a new director plan.
type CreateDirectorPlanVersionRequest struct {
	EpisodeID         string
	ScriptVersionID   string
	BasedOnVersionID  string
	VisualRhythm      string
	CameraLanguage    string
	ColorLighting     string
	Staging           string
	ContinuityRules   string
	AudioDirection    string
	ShotOverridesJSON string
	SourceAgentRunID  string
	CreatedByType     versioning.CreatedByType
	CreatedByID       string
	ChangeReason      string
	LegacyMetadata    string
}

// CreateDirectorPlanVersion appends a director plan version to an episode.
//
// The version number is derived from the stored maximum rather than a count,
// because the schema has UNIQUE (episode_id, version_number) and a reused
// number would be rejected. The status starts at draft: section 2.5 makes
// approval an explicit step, and migration 000010 allows at most one approved
// plan per episode through a partial unique index.
func (s *Service) CreateDirectorPlanVersion(ctx context.Context, request CreateDirectorPlanVersionRequest) (storyboard.DirectorPlanVersion, error) {
	if !s.Available() {
		return storyboard.DirectorPlanVersion{}, storageFailure()
	}
	highest, err := s.directorPlans.MaxDirectorPlanVersionNumber(ctx, request.EpisodeID)
	if err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return storyboard.DirectorPlanVersion{}, storageFailure()
	}
	version := storyboard.DirectorPlanVersion{
		ID:                id,
		EpisodeID:         strings.TrimSpace(request.EpisodeID),
		VersionNumber:     highest + 1,
		Status:            versioning.StatusDraft,
		BasedOnVersionID:  request.BasedOnVersionID,
		ScriptVersionID:   strings.TrimSpace(request.ScriptVersionID),
		VisualRhythm:      request.VisualRhythm,
		CameraLanguage:    request.CameraLanguage,
		ColorLighting:     request.ColorLighting,
		Staging:           request.Staging,
		ContinuityRules:   request.ContinuityRules,
		AudioDirection:    request.AudioDirection,
		ShotOverridesJSON: request.ShotOverridesJSON,
		SourceAgentRunID:  request.SourceAgentRunID,
		CreatedByType:     producer(request.CreatedByType),
		CreatedByID:       request.CreatedByID,
		ChangeReason:      request.ChangeReason,
		LegacyMetadata:    request.LegacyMetadata,
		CreatedAt:         s.now(),
	}
	if err := version.Validate(); err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	if err := s.directorPlans.CreateDirectorPlanVersion(ctx, version); err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	return version, nil
}

// EnsureStoryboard returns an episode's storyboard identity, creating it the
// first time.
//
// The identity is stable per episode (section 9.2 gives it no content and the
// schema has UNIQUE (episode_id)), so a caller that needs a storyboard should
// not have to know whether one exists yet. A creation race is tolerated: if the
// insert loses to another one, the stored row is read back rather than
// reporting a conflict, because both callers want the same identity.
func (s *Service) EnsureStoryboard(ctx context.Context, episodeID string) (storyboard.Storyboard, error) {
	if !s.Available() {
		return storyboard.Storyboard{}, storageFailure()
	}
	trimmed := strings.TrimSpace(episodeID)
	if trimmed == "" {
		return storyboard.Storyboard{}, storyboard.InvalidError("A storyboard must belong to an episode.")
	}
	existing, err := s.storyboards.GetStoryboardByEpisode(ctx, trimmed)
	if err == nil {
		return existing, nil
	}
	if _, ok := storyboard.AsError(err); !ok {
		return storyboard.Storyboard{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return storyboard.Storyboard{}, storageFailure()
	}
	now := s.now()
	record := storyboard.Storyboard{
		ID:        id,
		EpisodeID: trimmed,
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
	}
	if err := record.Validate(); err != nil {
		return storyboard.Storyboard{}, err
	}
	if err := s.storyboards.CreateStoryboard(ctx, record); err != nil {
		if _, ok := storyboard.AsError(err); ok {
			if stored, readErr := s.storyboards.GetStoryboardByEpisode(ctx, trimmed); readErr == nil {
				return stored, nil
			}
		}
		return storyboard.Storyboard{}, err
	}
	return record, nil
}

// CreateStoryboardVersionRequest carries a new storyboard version.
type CreateStoryboardVersionRequest struct {
	StoryboardID          string
	ScriptVersionID       string
	DirectorPlanVersionID string
	BasedOnVersionID      string
	SourceAgentRunID      string
	CreatedByType         versioning.CreatedByType
	CreatedByID           string
	ChangeReason          string
	LegacyMetadata        string
}

// CreateStoryboardVersion appends a storyboard version to a storyboard.
//
// The version must name both the script and the director plan it was built from
// (section 9.3 makes those two fields of the version, not provenance metadata),
// and the stored identity is checked against the caller's storyboard before the
// write. The number is derived from the stored maximum, as above.
func (s *Service) CreateStoryboardVersion(ctx context.Context, request CreateStoryboardVersionRequest) (storyboard.StoryboardVersion, error) {
	if !s.Available() {
		return storyboard.StoryboardVersion{}, storageFailure()
	}
	scriptVersionID := strings.TrimSpace(request.ScriptVersionID)
	directorPlanVersionID := strings.TrimSpace(request.DirectorPlanVersionID)
	if scriptVersionID == "" || directorPlanVersionID == "" {
		return storyboard.StoryboardVersion{}, storyboard.InvalidError("A storyboard version must name both the script version and the director plan it was built from.")
	}
	identity, err := s.storyboards.GetStoryboard(ctx, request.StoryboardID)
	if err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	highest, err := s.storyboards.MaxStoryboardVersionNumber(ctx, identity.ID)
	if err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return storyboard.StoryboardVersion{}, storageFailure()
	}
	version := storyboard.StoryboardVersion{
		ID:                    id,
		StoryboardID:          identity.ID,
		VersionNumber:         highest + 1,
		Status:                versioning.StatusDraft,
		ScriptVersionID:       scriptVersionID,
		DirectorPlanVersionID: directorPlanVersionID,
		BasedOnVersionID:      request.BasedOnVersionID,
		SourceAgentRunID:      request.SourceAgentRunID,
		CreatedByType:         producer(request.CreatedByType),
		CreatedByID:           request.CreatedByID,
		ChangeReason:          request.ChangeReason,
		LegacyMetadata:        request.LegacyMetadata,
		CreatedAt:             s.now(),
	}
	if err := version.Validate(); err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	// ValidateAgainst is the domain's rule that a version names the two inputs it
	// was built from. The values passed are the ones about to be stored, so the
	// check is exactly "does this row name both inputs, and did the caller supply
	// them", which is the same question the stale chain of section 15.2 will ask
	// of the stored row.
	if err := version.ValidateAgainst(scriptVersionID, directorPlanVersionID); err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	if err := s.storyboards.CreateStoryboardVersion(ctx, version); err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	return version, nil
}

// CreateStoryboardItemRequest carries one shot's row in a storyboard version.
type CreateStoryboardItemRequest struct {
	StoryboardVersionID  string
	ShotID               string
	Ordinal              int
	ShotSize             string
	CameraAngle          string
	CameraMovement       string
	DurationSeconds      int
	VisualDescription    string
	ActionDescription    string
	DialogueAudioSummary string
	ContinuityNotes      string
	// FirstFrameDescription, LastFrameDescription and VideoMotionDescription are
	// FR-070's remaining three fields, added by migration 000018. They are what a video
	// model is given to render the shot's two ends and its motion, which is why they
	// belong to the SHOOTING decision rather than to the script's shot.
	FirstFrameDescription  string
	LastFrameDescription   string
	VideoMotionDescription string
	// There is deliberately NO created-by here, and its absence is a fact about the
	// schema rather than an oversight: `storyboard_items` has no such column, so a field
	// would be one this method could not store. The attribution FR-100's audit reads
	// lives on the VERSION the rows belong to (`storyboard_versions.created_by_*`) and on
	// the domain event the caller records — which is where a reader already looks for
	// "who wrote this board".
}

// CreateStoryboardItem adds a shot's row to a storyboard version.
//
// Section 9.4's first invariant is "StoryboardItem 必须唯一对应本版本的 Shot",
// and migration 000010 expresses it together with the row's position as UNIQUE
// (storyboard_version_id, shot_id) and UNIQUE (storyboard_version_id, ordinal).
// Both are checked before the insert so the caller is told which one collided
// instead of receiving an unqualified constraint failure, and the check is a
// courtesy rather than the guarantee: the index is what actually enforces it.
func (s *Service) CreateStoryboardItem(ctx context.Context, request CreateStoryboardItemRequest) (storyboard.StoryboardItem, error) {
	if !s.Available() {
		return storyboard.StoryboardItem{}, storageFailure()
	}
	if _, err := s.storyboards.GetStoryboardVersion(ctx, request.StoryboardVersionID); err != nil {
		return storyboard.StoryboardItem{}, err
	}
	existing, found, err := s.items.FindStoryboardItemByShot(ctx, request.StoryboardVersionID, request.ShotID)
	if err != nil {
		return storyboard.StoryboardItem{}, err
	}
	if found {
		return storyboard.StoryboardItem{}, storyboard.ConflictError("This shot already has a row in this storyboard version. Edit " + existing.ID + " instead.")
	}
	occupied, found, err := s.items.FindStoryboardItemByOrdinal(ctx, request.StoryboardVersionID, request.Ordinal)
	if err != nil {
		return storyboard.StoryboardItem{}, err
	}
	if found {
		return storyboard.StoryboardItem{}, storyboard.ConflictError("That position is already taken by shot " + occupied.ShotID + " in this storyboard version. Choose another position or move the existing row.")
	}
	id, err := s.ids.New()
	if err != nil {
		return storyboard.StoryboardItem{}, storageFailure()
	}
	now := s.now()
	item := storyboard.StoryboardItem{
		ID:                     id,
		StoryboardVersionID:    request.StoryboardVersionID,
		ShotID:                 strings.TrimSpace(request.ShotID),
		Ordinal:                request.Ordinal,
		ShotSize:               request.ShotSize,
		CameraAngle:            request.CameraAngle,
		CameraMovement:         request.CameraMovement,
		DurationSeconds:        request.DurationSeconds,
		VisualDescription:      request.VisualDescription,
		ActionDescription:      request.ActionDescription,
		DialogueAudioSummary:   request.DialogueAudioSummary,
		ContinuityNotes:        request.ContinuityNotes,
		FirstFrameDescription:  request.FirstFrameDescription,
		LastFrameDescription:   request.LastFrameDescription,
		VideoMotionDescription: request.VideoMotionDescription,
		Status:                 versioning.StatusDraft,
		CreatedAt:              now,
		UpdatedAt:              now,
		Revision:               1,
	}

	if err := item.Validate(); err != nil {
		return storyboard.StoryboardItem{}, err
	}
	if err := s.items.CreateStoryboardItem(ctx, item); err != nil {
		return storyboard.StoryboardItem{}, err
	}
	return item, nil
}

// CreatePanelVersionRequest carries a new panel version for a storyboard item.
type CreatePanelVersionRequest struct {
	StoryboardItemID    string
	BasedOnVersionID    string
	VisualPrompt        string
	NegativePrompt      string
	ReferencePolicyJSON string
	SourceAgentRunID    string
	CreatedByType       versioning.CreatedByType
	CreatedByID         string
	ChangeReason        string
	LegacyMetadata      string
}

// CreatePanelVersion appends a panel version to a storyboard item.
//
// "每个 Shot 可有多个面板版本" (PRD FR-070) is why the number is derived from the
// item's stored maximum: the schema has UNIQUE (storyboard_item_id,
// version_number).
func (s *Service) CreatePanelVersion(ctx context.Context, request CreatePanelVersionRequest) (storyboard.StoryboardPanelVersion, error) {
	if !s.Available() {
		return storyboard.StoryboardPanelVersion{}, storageFailure()
	}
	if _, err := s.items.GetStoryboardItem(ctx, request.StoryboardItemID); err != nil {
		return storyboard.StoryboardPanelVersion{}, err
	}
	highest, err := s.panels.MaxPanelVersionNumber(ctx, request.StoryboardItemID)
	if err != nil {
		return storyboard.StoryboardPanelVersion{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return storyboard.StoryboardPanelVersion{}, storageFailure()
	}
	panel := storyboard.StoryboardPanelVersion{
		ID:                  id,
		StoryboardItemID:    request.StoryboardItemID,
		VersionNumber:       highest + 1,
		Status:              versioning.StatusDraft,
		BasedOnVersionID:    request.BasedOnVersionID,
		VisualPrompt:        request.VisualPrompt,
		NegativePrompt:      request.NegativePrompt,
		ReferencePolicyJSON: request.ReferencePolicyJSON,
		SourceAgentRunID:    request.SourceAgentRunID,
		CreatedByType:       producer(request.CreatedByType),
		CreatedByID:         request.CreatedByID,
		ChangeReason:        request.ChangeReason,
		LegacyMetadata:      request.LegacyMetadata,
		CreatedAt:           s.now(),
	}
	if err := panel.Validate(); err != nil {
		return storyboard.StoryboardPanelVersion{}, err
	}
	if err := s.panels.CreatePanelVersion(ctx, panel); err != nil {
		return storyboard.StoryboardPanelVersion{}, err
	}
	return panel, nil
}

// ApprovePanelImageRequest approves one candidate image as a panel's canonical
// one.
type ApprovePanelImageRequest struct {
	PanelVersionID              string
	ApprovedImageAssetVersionID string
	// CandidateVersionIDs are the asset versions this panel generated or the
	// user associated with it. Candidacy is a property of the panel's job
	// history, so it is supplied per call rather than stored on the version row.
	CandidateVersionIDs []string
	// ExpectedRevision is the revision of the panel's parent storyboard item,
	// the row the approval is recorded against (see PanelRepository).
	ExpectedRevision int64
}

// ApprovePanelImage records a panel's approved image.
//
// Section 9.5's third invariant is "approved image 必须属于该 Panel 的候选或经用户
// 明确关联", and CanApproveImage is the rule that decides it. The domain error it
// returns is propagated unchanged so the caller can offer the "associate it
// first" route the message names.
func (s *Service) ApprovePanelImage(ctx context.Context, request ApprovePanelImageRequest) (storyboard.StoryboardPanelVersion, error) {
	if !s.Available() {
		return storyboard.StoryboardPanelVersion{}, storageFailure()
	}
	if len(request.CandidateVersionIDs) > MaxBatchCandidates {
		return storyboard.StoryboardPanelVersion{}, storyboard.InvalidError("Too many candidate images in one approval.")
	}
	panel, err := s.panels.GetPanelVersion(ctx, request.PanelVersionID)
	if err != nil {
		return storyboard.StoryboardPanelVersion{}, err
	}
	if err := panel.CanApproveImage(request.ApprovedImageAssetVersionID, request.CandidateVersionIDs); err != nil {
		return storyboard.StoryboardPanelVersion{}, err
	}
	if err := s.panels.ApprovePanelImage(ctx, panel.ID, request.ApprovedImageAssetVersionID, request.ExpectedRevision); err != nil {
		return storyboard.StoryboardPanelVersion{}, err
	}
	panel.ApprovedImageAssetVersionID = request.ApprovedImageAssetVersionID
	return panel, nil
}

// ListStoryboardItems returns a version's items in ordinal order.
func (s *Service) ListStoryboardItems(ctx context.Context, storyboardVersionID string) ([]storyboard.StoryboardItem, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.items.ListStoryboardItems(ctx, storyboardVersionID)
}

// ListPanels returns an item's panel versions in version order.
func (s *Service) ListPanels(ctx context.Context, storyboardItemID string) ([]storyboard.StoryboardPanelVersion, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.panels.ListPanelVersions(ctx, storyboardItemID)
}

// ProjectOfEpisode returns the project an episode belongs to.
//
// The repository port has answered this since WP-05 — the approvals that must file a
// governance event in a project's stream use it — and it was not EXPOSED on the service,
// so the only callers were the two approval methods inside this package. WP-09's
// production pipeline needs the same fact for its own scope check, and a second
// implementation of "episode to project" would be a second answer to a question the
// episode table already settles.
//
// A missing episode is a domain not-found rather than an empty string, so "no such
// episode" and "an episode with no project" cannot be confused — which matters to the
// caller, because the first is a stale identifier and the second would be a corrupt row.
func (s *Service) ProjectOfEpisode(ctx context.Context, episodeID string) (string, error) {
	if !s.Available() {
		return "", storageFailure()
	}
	return s.directorPlans.ProjectOfEpisode(ctx, episodeID)
}

// GetDirectorPlanVersion returns one plan version by id.
func (s *Service) GetDirectorPlanVersion(ctx context.Context, id string) (storyboard.DirectorPlanVersion, error) {
	if !s.Available() {
		return storyboard.DirectorPlanVersion{}, storageFailure()
	}
	return s.directorPlans.GetDirectorPlanVersion(ctx, id)
}

// GetStoryboardVersion returns one storyboard version by id.
func (s *Service) GetStoryboardVersion(ctx context.Context, id string) (storyboard.StoryboardVersion, error) {
	if !s.Available() {
		return storyboard.StoryboardVersion{}, storageFailure()
	}
	return s.storyboards.GetStoryboardVersion(ctx, id)
}

// GetStoryboard returns one storyboard identity by id.
func (s *Service) GetStoryboard(ctx context.Context, id string) (storyboard.Storyboard, error) {
	if !s.Available() {
		return storyboard.Storyboard{}, storageFailure()
	}
	return s.storyboards.GetStoryboard(ctx, id)
}

// GetStoryboardItem returns one item by id, which is the read a panel command needs to
// find the version and, through it, the episode a caller must be authorized for.
func (s *Service) GetStoryboardItem(ctx context.Context, id string) (storyboard.StoryboardItem, error) {
	if !s.Available() {
		return storyboard.StoryboardItem{}, storageFailure()
	}
	return s.items.GetStoryboardItem(ctx, id)
}
