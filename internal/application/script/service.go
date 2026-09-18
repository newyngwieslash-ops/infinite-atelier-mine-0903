package script

import (
	"context"
	"strings"
	"time"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// NewService builds the script service.
func NewService(options Options) *Service {
	return &Service{repository: options.Repository, clock: options.Clock, ids: options.IDs}
}

// Available reports whether the service can operate. An unattached binding
// fails closed rather than panicking.
func (s *Service) Available() bool {
	return s != nil && s.repository != nil && s.ids != nil
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// storageFailure is the fail-closed error for an unattached service and for a
// failed identifier generation.
func storageFailure() error {
	return scriptdomain.StorageError("The episode and script store is unavailable.", nil)
}

// CreateEpisodeRequest is a caller's request to plan an episode.
type CreateEpisodeRequest struct {
	ProjectID             string
	SeasonNumber          int
	EpisodeNumber         int
	Title                 string
	TargetDurationSeconds int
}

// CreateEpisode plans one instalment.
//
// The (project, season, episode) uniqueness DOMAIN_MODEL §7.1 requires is
// checked before the insert so the caller is told which episode already holds
// that position. The check is not the guarantee: the schema's UNIQUE
// constraint is, and a concurrent write that slipped between the two is
// reported by the repository as a conflict with a less specific message.
func (s *Service) CreateEpisode(ctx context.Context, request CreateEpisodeRequest) (scriptdomain.Episode, error) {
	if !s.Available() {
		return scriptdomain.Episode{}, storageFailure()
	}
	projectID := strings.TrimSpace(request.ProjectID)
	id, err := s.ids.New()
	if err != nil {
		return scriptdomain.Episode{}, storageFailure()
	}
	now := s.now()
	record := scriptdomain.Episode{
		ID:                    id,
		ProjectID:             projectID,
		SeasonNumber:          request.SeasonNumber,
		EpisodeNumber:         request.EpisodeNumber,
		Title:                 request.Title,
		Status:                scriptdomain.EpisodePlanning,
		TargetDurationSeconds: request.TargetDurationSeconds,
		CreatedAt:             now,
		UpdatedAt:             now,
		Revision:              1,
	}
	if err := record.Validate(); err != nil {
		return scriptdomain.Episode{}, err
	}
	taken, err := s.repository.CountEpisodesAtPosition(ctx, projectID, record.SeasonNumber, record.EpisodeNumber)
	if err != nil {
		return scriptdomain.Episode{}, err
	}
	if taken > 0 {
		return scriptdomain.Episode{}, scriptdomain.ConflictError("This project already has an episode at that position (" + record.Key() + ").")
	}
	if err := s.repository.CreateEpisode(ctx, record); err != nil {
		return scriptdomain.Episode{}, err
	}
	return record, nil
}

// GetEpisode returns one episode.
func (s *Service) GetEpisode(ctx context.Context, id string) (scriptdomain.Episode, error) {
	if !s.Available() {
		return scriptdomain.Episode{}, storageFailure()
	}
	return s.repository.GetEpisode(ctx, id)
}

// ListEpisodes returns a project's episodes in season and episode order.
func (s *Service) ListEpisodes(ctx context.Context, projectID string) ([]scriptdomain.Episode, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListEpisodes(ctx, projectID)
}

// UpdateEpisodeStatusRequest carries a production state change.
type UpdateEpisodeStatusRequest struct {
	EpisodeID string
	Status    scriptdomain.EpisodeStatus
	Revision  int64
}

// UpdateEpisodeStatus stores an episode's production state under a revision
// guard.
//
// The status is checked against the domain vocabulary before the write, so an
// unspelled value is refused with the domain's message rather than the
// schema's CHECK failure. The transitions are not restricted: §7.1 lists the
// five states without a state machine, and the production order a particular
// studio follows is not this layer's ruling to make.
func (s *Service) UpdateEpisodeStatus(ctx context.Context, request UpdateEpisodeStatusRequest) (scriptdomain.Episode, error) {
	if !s.Available() {
		return scriptdomain.Episode{}, storageFailure()
	}
	if !scriptdomain.IsValidEpisodeStatus(request.Status) {
		return scriptdomain.Episode{}, scriptdomain.InvalidError("That episode status is not recognised.")
	}
	record, err := s.repository.GetEpisode(ctx, request.EpisodeID)
	if err != nil {
		return scriptdomain.Episode{}, err
	}
	record.Status = request.Status
	record.UpdatedAt = s.now()
	if err := record.Validate(); err != nil {
		return scriptdomain.Episode{}, err
	}
	if err := s.repository.UpdateEpisode(ctx, record, request.Revision); err != nil {
		return scriptdomain.Episode{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// CreateStorySkeletonVersionRequest adds one version of an episode's skeleton.
type CreateStorySkeletonVersionRequest struct {
	EpisodeID                string
	BasedOnVersionID         string
	OpeningHook              string
	CoreConflict             string
	TurningPointsJSON        string
	Climax                   string
	EndingHook               string
	EstimatedDurationSeconds int
	SourceAgentRunID         string
	CreatedByType            versioning.CreatedByType
	CreatedByID              string
	ChangeReason             string
	LegacyMetadata           string
}

// CreateStorySkeletonVersion appends a skeleton version, numbering it after
// the highest existing one.
//
// The number comes from the stored maximum rather than a count: the schema has
// a unique constraint on (episode_id, version_number), and a count would let a
// number already stored be handed out again.
func (s *Service) CreateStorySkeletonVersion(ctx context.Context, request CreateStorySkeletonVersionRequest) (scriptdomain.StorySkeletonVersion, error) {
	if !s.Available() {
		return scriptdomain.StorySkeletonVersion{}, storageFailure()
	}
	episode, err := s.repository.GetEpisode(ctx, request.EpisodeID)
	if err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	skeleton, err := s.newSkeletonVersion(ctx, episode, request)
	if err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	if err := s.repository.CreateStorySkeletonVersion(ctx, skeleton); err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	return skeleton, nil
}

func (s *Service) newSkeletonVersion(ctx context.Context, episode scriptdomain.Episode, request CreateStorySkeletonVersionRequest) (scriptdomain.StorySkeletonVersion, error) {
	createdBy := request.CreatedByType
	if createdBy == "" {
		createdBy = versioning.CreatedByUser
	}
	id, err := s.ids.New()
	if err != nil {
		return scriptdomain.StorySkeletonVersion{}, storageFailure()
	}
	highest, err := s.repository.MaxStorySkeletonVersionNumber(ctx, episode.ID)
	if err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	record := scriptdomain.StorySkeletonVersion{
		ID:                       id,
		EpisodeID:                episode.ID,
		VersionNumber:            highest + 1,
		Status:                   versioning.StatusDraft,
		BasedOnVersionID:         request.BasedOnVersionID,
		OpeningHook:              request.OpeningHook,
		CoreConflict:             request.CoreConflict,
		TurningPointsJSON:        request.TurningPointsJSON,
		Climax:                   request.Climax,
		EndingHook:               request.EndingHook,
		EstimatedDurationSeconds: request.EstimatedDurationSeconds,
		SourceAgentRunID:         request.SourceAgentRunID,
		CreatedByType:            createdBy,
		CreatedByID:              request.CreatedByID,
		ChangeReason:             request.ChangeReason,
		LegacyMetadata:           request.LegacyMetadata,
		CreatedAt:                s.now(),
	}
	if err := record.Validate(); err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	return record, nil
}

// CreateAdaptationStrategyVersionRequest adds one version of an episode's
// adaptation strategy.
type CreateAdaptationStrategyVersionRequest struct {
	EpisodeID             string
	BasedOnVersionID      string
	StrategySummary       string
	AdaptationMode        scriptdomain.AdaptationMode
	MergedEventGroupsJSON string
	OriginalAdditions     string
	Rationale             string
	Risks                 string
	SourceAgentRunID      string
	CreatedByType         versioning.CreatedByType
	CreatedByID           string
	ChangeReason          string
	LegacyMetadata        string
}

// CreateAdaptationStrategyVersion appends a strategy version, numbering it
// after the highest existing one for the same reason the skeleton version does.
func (s *Service) CreateAdaptationStrategyVersion(ctx context.Context, request CreateAdaptationStrategyVersionRequest) (scriptdomain.AdaptationStrategyVersion, error) {
	if !s.Available() {
		return scriptdomain.AdaptationStrategyVersion{}, storageFailure()
	}
	episode, err := s.repository.GetEpisode(ctx, request.EpisodeID)
	if err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	createdBy := request.CreatedByType
	if createdBy == "" {
		createdBy = versioning.CreatedByUser
	}
	mode := request.AdaptationMode
	if mode == "" {
		mode = scriptdomain.AdaptationBalanced
	}
	if !scriptdomain.IsValidAdaptationMode(mode) {
		return scriptdomain.AdaptationStrategyVersion{}, scriptdomain.InvalidError("That adaptation mode is not recognised.")
	}
	id, err := s.ids.New()
	if err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, storageFailure()
	}
	highest, err := s.repository.MaxAdaptationStrategyVersionNumber(ctx, episode.ID)
	if err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	record := scriptdomain.AdaptationStrategyVersion{
		ID:                    id,
		EpisodeID:             episode.ID,
		VersionNumber:         highest + 1,
		Status:                versioning.StatusDraft,
		BasedOnVersionID:      request.BasedOnVersionID,
		StrategySummary:       request.StrategySummary,
		AdaptationMode:        mode,
		MergedEventGroupsJSON: request.MergedEventGroupsJSON,
		OriginalAdditions:     request.OriginalAdditions,
		Rationale:             request.Rationale,
		Risks:                 request.Risks,
		SourceAgentRunID:      request.SourceAgentRunID,
		CreatedByType:         createdBy,
		CreatedByID:           request.CreatedByID,
		ChangeReason:          request.ChangeReason,
		LegacyMetadata:        request.LegacyMetadata,
		CreatedAt:             s.now(),
	}
	if err := record.Validate(); err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	if err := s.repository.CreateAdaptationStrategyVersion(ctx, record); err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	return record, nil
}

// EnsureScript returns an episode's script, creating it when the episode has
// none.
//
// The identity is created on demand because a script row holds no content: it
// is what scenes and downstream artifacts hang off, so it exists as soon as
// the first version is about to be written. A second call returns the same row
// rather than a second script, because the schema allows one per episode.
//
// The lookup and the insert are two statements, not one transaction. Two
// callers that both find nothing will both insert, and the second insert loses
// to the unique constraint on episode_id; the loser re-reads and returns the
// winner's row, so the call still produces exactly one script per episode.
func (s *Service) EnsureScript(ctx context.Context, episodeID string) (scriptdomain.Script, error) {
	if !s.Available() {
		return scriptdomain.Script{}, storageFailure()
	}
	episode, err := s.repository.GetEpisode(ctx, episodeID)
	if err != nil {
		return scriptdomain.Script{}, err
	}
	existing, err := s.repository.GetScriptByEpisode(ctx, episode.ID)
	if err == nil {
		return existing, nil
	}
	if !isNotFound(err) {
		return scriptdomain.Script{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return scriptdomain.Script{}, storageFailure()
	}
	now := s.now()
	record := scriptdomain.Script{
		ID:        id,
		EpisodeID: episode.ID,
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
	}
	if err := record.Validate(); err != nil {
		return scriptdomain.Script{}, err
	}
	if err := s.repository.CreateScript(ctx, record); err != nil {
		// Another caller may have created it between the read and this write.
		// The schema's unique constraint decided the race; the row that won is
		// the answer, so it is read back rather than shadowed by an error.
		if winner, readErr := s.repository.GetScriptByEpisode(ctx, episode.ID); readErr == nil {
			return winner, nil
		}
		return scriptdomain.Script{}, err
	}
	return record, nil
}

// isNotFound reports whether a repository error is the domain's not-found
// category, which EnsureScript reads as "no script yet" rather than a failure.
func isNotFound(err error) bool {
	domainErr, ok := scriptdomain.AsError(err)
	return ok && domainErr.Category == scriptdomain.CategoryNotFound
}

// CreateScriptVersionRequest adds one version of a script's content.
type CreateScriptVersionRequest struct {
	ScriptID                    string
	BasedOnVersionID            string
	StorySkeletonVersionID      string
	AdaptationStrategyVersionID string
	EstimatedDurationSeconds    int
	Summary                     string
	SourceAgentRunID            string
	CreatedByType               versioning.CreatedByType
	CreatedByID                 string
	ChangeReason                string
	LegacyMetadata              string
}

// CreateScriptVersion appends a script version, numbering it after the
// highest existing one.
func (s *Service) CreateScriptVersion(ctx context.Context, request CreateScriptVersionRequest) (scriptdomain.ScriptVersion, error) {
	if !s.Available() {
		return scriptdomain.ScriptVersion{}, storageFailure()
	}
	script, err := s.repository.GetScript(ctx, request.ScriptID)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	createdBy := request.CreatedByType
	if createdBy == "" {
		createdBy = versioning.CreatedByUser
	}
	id, err := s.ids.New()
	if err != nil {
		return scriptdomain.ScriptVersion{}, storageFailure()
	}
	highest, err := s.repository.MaxScriptVersionNumber(ctx, script.ID)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	version := scriptdomain.ScriptVersion{
		ID:                          id,
		ScriptID:                    script.ID,
		VersionNumber:               highest + 1,
		Status:                      versioning.StatusDraft,
		BasedOnVersionID:            request.BasedOnVersionID,
		StorySkeletonVersionID:      strings.TrimSpace(request.StorySkeletonVersionID),
		AdaptationStrategyVersionID: strings.TrimSpace(request.AdaptationStrategyVersionID),
		EstimatedDurationSeconds:    request.EstimatedDurationSeconds,
		Summary:                     request.Summary,
		SourceAgentRunID:            request.SourceAgentRunID,
		CreatedByType:               createdBy,
		CreatedByID:                 request.CreatedByID,
		ChangeReason:                request.ChangeReason,
		LegacyMetadata:              request.LegacyMetadata,
		CreatedAt:                   s.now(),
	}
	if err := version.Validate(); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	if err := s.repository.CreateScriptVersion(ctx, version); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	return version, nil
}

// ApproveScriptVersionRequest approves one script version.
//
// The request carries no expected revision because a script version has none:
// migration 000008 gives script_versions no revision column, and the version
// aggregate in internal/domain/script carries no Revision field either. The
// concurrency token is the version's own status, which the repository's write
// matches, plus the schema's partial unique index on the approved row.
type ApproveScriptVersionRequest struct {
	ScriptVersionID string
}

// ApproveScriptVersion makes one version the script's approved version.
//
// DOMAIN_MODEL §2.5 states both halves of the rule: at most one version of a
// parent may be approved, and approving a new one turns the previous approval
// into 'superseded'. versioning.CanApprove decides whether this version may be
// approved at all, so an already-approved version, a superseded one and a
// stale one are all refused with the domain's own message.
//
// The two writes go through one repository call because the schema's partial
// unique index (script_id WHERE status = 'approved') is what makes them
// order-dependent: the supersede must land before the approval, and a failure
// between the two would leave the script with no approved version. One
// transaction is the only way to have both or neither.
//
// The repository's write is guarded by the status this call read. It is not a
// revision compare-and-swap, because the row has no revision to compare; what
// it protects is that a version edited or re-reviewed under the caller is not
// approved silently on the strength of a stale read.
func (s *Service) ApproveScriptVersion(ctx context.Context, request ApproveScriptVersionRequest) (scriptdomain.ScriptVersion, error) {
	if !s.Available() {
		return scriptdomain.ScriptVersion{}, storageFailure()
	}
	version, err := s.repository.GetScriptVersion(ctx, request.ScriptVersionID)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	if err := versioning.CanApprove(version.Status, versioning.StatusApproved); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	current, approved, err := s.repository.CurrentApprovedScriptVersion(ctx, version.ScriptID)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	supersededID := ""
	if approved && versioning.SupersedePrevious(current.Status) {
		supersededID = current.ID
	}
	if err := s.repository.ApproveScriptVersion(ctx, version, supersededID); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	version.Status = versioning.StatusApproved
	return version, nil
}

// CreateSceneRequest adds one scene to a script version.
type CreateSceneRequest struct {
	ScriptVersionID          string
	Ordinal                  int
	SceneNumber              string
	Slugline                 string
	InteriorExterior         scriptdomain.InteriorExterior
	LocationEntityID         string
	TimeOfDay                string
	Summary                  string
	DramaticGoal             string
	EstimatedDurationSeconds int
	SourceStoryEventID       string
	IsOriginalAdaptation     bool
}

// CreateScene stores a scene of a script version.
//
// The ordinal is unique within the version (its own UNIQUE constraint), so the
// clash is reported as a conflict naming the position rather than as a raw
// constraint failure.
func (s *Service) CreateScene(ctx context.Context, request CreateSceneRequest) (scriptdomain.Scene, error) {
	if !s.Available() {
		return scriptdomain.Scene{}, storageFailure()
	}
	interior := request.InteriorExterior
	if interior == "" {
		interior = scriptdomain.InteriorOTHER
	}
	id, err := s.ids.New()
	if err != nil {
		return scriptdomain.Scene{}, storageFailure()
	}
	now := s.now()
	record := scriptdomain.Scene{
		ID:                       id,
		ScriptVersionID:          strings.TrimSpace(request.ScriptVersionID),
		Ordinal:                  request.Ordinal,
		SceneNumber:              request.SceneNumber,
		Slugline:                 request.Slugline,
		InteriorExterior:         interior,
		LocationEntityID:         strings.TrimSpace(request.LocationEntityID),
		TimeOfDay:                request.TimeOfDay,
		Summary:                  request.Summary,
		DramaticGoal:             request.DramaticGoal,
		EstimatedDurationSeconds: request.EstimatedDurationSeconds,
		SourceStoryEventID:       strings.TrimSpace(request.SourceStoryEventID),
		IsOriginalAdaptation:     request.IsOriginalAdaptation,
		CreatedAt:                now,
		UpdatedAt:                now,
		Revision:                 1,
	}
	if err := record.Validate(); err != nil {
		return scriptdomain.Scene{}, err
	}
	taken, err := s.repository.CountScenesAtOrdinal(ctx, record.ScriptVersionID, record.Ordinal)
	if err != nil {
		return scriptdomain.Scene{}, err
	}
	if taken > 0 {
		return scriptdomain.Scene{}, scriptdomain.ConflictError("That scene position is already used in this script version.")
	}
	if err := s.repository.CreateScene(ctx, record); err != nil {
		return scriptdomain.Scene{}, err
	}
	return record, nil
}

// CreateShotRequest adds one camera setup to a scene.
type CreateShotRequest struct {
	SceneID                  string
	Ordinal                  int
	ShotNumber               string
	ShotSize                 string
	CameraAngle              string
	CameraMovement           string
	EstimatedDurationSeconds int
	VisualDescription        string
	ActionDescription        string
	AudioIntent              string
	ContinuityNotes          string
}

// CreateShot stores a shot of a scene.
//
// The shot starts as 'draft' because §7.8 lets it exist from the draft stage
// and be refined by the storyboard workflow. The ordinal is unique within the
// scene; the check reports the clash before the insert does.
func (s *Service) CreateShot(ctx context.Context, request CreateShotRequest) (scriptdomain.Shot, error) {
	if !s.Available() {
		return scriptdomain.Shot{}, storageFailure()
	}
	id, err := s.ids.New()
	if err != nil {
		return scriptdomain.Shot{}, storageFailure()
	}
	now := s.now()
	record := scriptdomain.Shot{
		ID:                       id,
		SceneID:                  strings.TrimSpace(request.SceneID),
		Ordinal:                  request.Ordinal,
		ShotNumber:               request.ShotNumber,
		ShotSize:                 request.ShotSize,
		CameraAngle:              request.CameraAngle,
		CameraMovement:           request.CameraMovement,
		EstimatedDurationSeconds: request.EstimatedDurationSeconds,
		VisualDescription:        request.VisualDescription,
		ActionDescription:        request.ActionDescription,
		AudioIntent:              request.AudioIntent,
		ContinuityNotes:          request.ContinuityNotes,
		Status:                   versioning.StatusDraft,
		CreatedAt:                now,
		UpdatedAt:                now,
		Revision:                 1,
	}
	if err := record.Validate(); err != nil {
		return scriptdomain.Shot{}, err
	}
	taken, err := s.repository.CountShotsAtOrdinal(ctx, record.SceneID, record.Ordinal)
	if err != nil {
		return scriptdomain.Shot{}, err
	}
	if taken > 0 {
		return scriptdomain.Shot{}, scriptdomain.ConflictError("That shot position is already used in this scene.")
	}
	if err := s.repository.CreateShot(ctx, record); err != nil {
		return scriptdomain.Shot{}, err
	}
	return record, nil
}

// ListScenes returns a script version's scenes in script order.
func (s *Service) ListScenes(ctx context.Context, scriptVersionID string) ([]scriptdomain.Scene, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListScenes(ctx, scriptVersionID)
}

// ListShots returns a scene's shots in order.
func (s *Service) ListShots(ctx context.Context, sceneID string) ([]scriptdomain.Shot, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListShots(ctx, sceneID)
}
