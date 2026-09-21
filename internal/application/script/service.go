package script

import (
	"context"
	"strings"
	"time"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// NewService builds the script service.
func NewService(options Options) *Service {
	// Every option is copied. The projector was added by WP-08 and NOT copied here at first, so the
	// field stayed nil however the composition root set it and every projection refused with
	// "this build cannot project onto a canvas" — a defect a service test found by asserting the
	// positive path rather than only the refusal.
	return &Service{
		repository: options.Repository,
		clock:      options.Clock,
		ids:        options.IDs,
		events:     options.Events,
		projector:  options.Projector,
	}
}

// Available reports whether the service can operate. An unattached binding
// fails closed rather than panicking.
func (s *Service) Available() bool {
	return s != nil && s.repository != nil && s.ids != nil
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
	// Section 17's EpisodeCreated. Best effort: the episode is committed and the
	// caller must not be told the command failed because the announcement did
	// not land. ADR-0009 records why this is not the transactional path.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          event.EpisodeCreated,
		AggregateType: event.AggregateEpisode,
		AggregateID:   record.ID,
		ProjectID:     record.ProjectID,
	})
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
	// SelectedEventIDs are the story events this version selects (§7.4's link table).
	//
	// They are a SET rather than an ordered list because a skeleton selects which events are in the
	// episode and does not state their order: the order a viewer sees is the ADAPTATION's, and §7.5
	// gives the strategy a link table with ordinals for exactly that. Two callers that selected the
	// same events in different orders have selected the same events.
	SelectedEventIDs []string
	SourceAgentRunID string
	CreatedByType    versioning.CreatedByType
	CreatedByID      string
	ChangeReason     string
	LegacyMetadata   string
}

// CreateStorySkeletonVersion appends a skeleton version, numbering it after
// the highest existing one.
//
// The number comes from the stored maximum rather than a count: the schema has
// a unique constraint on (episode_id, version_number), and a count would let a
// number already stored be handed out again.
//
// The version and its selected events are written together, and the locks pinned on the base
// version are enforced before either is written. Both are AC-SCRIPT-002's requirements and the
// first version of this method had neither: the schema's link tables had no writer, so a skeleton's
// selection existed only inside its own JSON, and a FIX could rewrite a field a user had pinned.
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
	selected, err := normaliseEventIDs(request.SelectedEventIDs)
	if err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	// The selection is checked against the project's story graph BEFORE the write, and this is the
	// ONLY place it can be: `story_skeleton_event_links.story_event_id` has no foreign key, for the
	// reason `linked_versions.go` records — a citation is provenance and outlives the row — so a
	// selection naming an event that does not exist would otherwise be stored and read back as a
	// reference to nothing.
	if err := s.assertStoryEventsExist(ctx, episode.ProjectID, selected); err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	if err := s.assertSkeletonLocks(ctx, skeleton, selected); err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	if err := s.repository.CreateStorySkeletonVersionWithLinks(ctx, skeleton, selected); err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	return skeleton, nil
}

// assertStoryEventsExist refuses a set of events that a project does not have.
//
// It is shared by the skeleton's selection, the strategy's treatments and a script structure's
// citations, because all three ask the same question and all three need the same answer: this
// project has these events, or the write is refused with the identifiers named.
func (s *Service) assertStoryEventsExist(ctx context.Context, projectID string, eventIDs []string) error {
	project := strings.TrimSpace(projectID)
	if project == "" || len(eventIDs) == 0 {
		// No project means no project-scoped graph to check against — see assertStructureReferences,
		// which explains why the tool path always has one.
		return nil
	}
	missing, err := s.repository.MissingStoryEventIDs(ctx, project, eventIDs)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return scriptdomain.InvalidError(
			"These story events do not exist in this project: " + strings.Join(missing, ", ") + ".")
	}
	return nil
}

// normaliseEventIDs trims a selection and refuses one that repeats an event.
//
// A repetition is refused rather than deduplicated because the schema's primary key is
// (version, event) and a payload naming one event twice would either lose the second silently or
// fail as a raw constraint violation. Refusing it here says which identifier was repeated.
func normaliseEventIDs(ids []string) ([]string, error) {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if seen[trimmed] {
			return nil, scriptdomain.InvalidError("That story event is selected twice: " + trimmed + ".")
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out, nil
}

// assertSkeletonLocks refuses a skeleton revision that changed a field the user pinned.
//
// This is AC-SCRIPT-002's own scenario. The criterion is a FIX on a skeleton whose ending hook is
// missing, and the fields the user pins there are the ones the model got right — so the check has to
// cover the skeleton's own fields, not only a script version's.
//
// `selectedEventIds` is compared as a SET, through `joinedEvents`, because selecting the same events
// in a different order is the same selection.
func (s *Service) assertSkeletonLocks(ctx context.Context, incoming scriptdomain.StorySkeletonVersion, selectedEventIDs []string) error {
	locks, err := s.readLocks(ctx, incoming.BasedOnVersionID)
	if err != nil {
		return err
	}
	if len(locks) == 0 {
		return nil
	}
	if err := assertLocksCovered(locks, scriptdomain.FamilyStorySkeleton, scriptdomain.LockableFields(scriptdomain.FamilyStorySkeleton)); err != nil {
		return err
	}
	base, err := s.repository.GetStorySkeletonVersion(ctx, incoming.BasedOnVersionID)
	if err != nil {
		return err
	}
	baseSelection, err := s.repository.ListSkeletonEventIDs(ctx, base.ID)
	if err != nil {
		return err
	}
	return compareFieldSets(locks, scriptdomain.FamilyStorySkeleton,
		fieldValues{
			scriptdomain.LockSkeletonOpeningHook:    base.OpeningHook,
			scriptdomain.LockSkeletonCoreConflict:   base.CoreConflict,
			scriptdomain.LockSkeletonTurningPoints:  base.TurningPointsJSON,
			scriptdomain.LockSkeletonClimax:         base.Climax,
			scriptdomain.LockSkeletonEndingHook:     base.EndingHook,
			scriptdomain.LockSkeletonSelectedEvents: joinedEvents(baseSelection),
		},
		fieldValues{
			scriptdomain.LockSkeletonOpeningHook:    incoming.OpeningHook,
			scriptdomain.LockSkeletonCoreConflict:   incoming.CoreConflict,
			scriptdomain.LockSkeletonTurningPoints:  incoming.TurningPointsJSON,
			scriptdomain.LockSkeletonClimax:         incoming.Climax,
			scriptdomain.LockSkeletonEndingHook:     incoming.EndingHook,
			scriptdomain.LockSkeletonSelectedEvents: joinedEvents(selectedEventIDs),
		},
	)
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
		BasedOnVersionID:         strings.TrimSpace(request.BasedOnVersionID),
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
	// EventLinks are this version's per-event decisions (§7.5's link table): which events it kept,
	// which it dropped, and which it moved. The ORDER is the treatment's own state — "reordered"
	// means nothing without a position — so the slice's positions become the stored ordinals.
	EventLinks       []scriptdomain.StrategyEventLink
	SourceAgentRunID string
	CreatedByType    versioning.CreatedByType
	CreatedByID      string
	ChangeReason     string
	LegacyMetadata   string
}

// CreateAdaptationStrategyVersion appends a strategy version, numbering it
// after the highest existing one for the same reason the skeleton version does.
//
// The version and its event treatments are written together, and the locks pinned on the base
// version are enforced first. §7.5 makes the treatments part of what a strategy IS, so a version
// whose links were never written would be a strategy that decided nothing — the same gap the
// skeleton's own write path had.
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
		BasedOnVersionID:      strings.TrimSpace(request.BasedOnVersionID),
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
	links, err := normaliseEventLinks(record.ID, request.EventLinks)
	if err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	// The treatments name events, and the link table has no foreign key to check them — so this is
	// the check, and it runs before the write for the same reason the skeleton's does.
	treatedEvents := make([]string, 0, len(links))
	for _, link := range links {
		treatedEvents = append(treatedEvents, link.StoryEventID)
	}
	if err := s.assertStoryEventsExist(ctx, episode.ProjectID, treatedEvents); err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	if err := s.assertStrategyLocks(ctx, record, links); err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	if err := s.repository.CreateAdaptationStrategyVersionWithLinks(ctx, record, links); err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	return record, nil
}

// normaliseEventLinks trims a treatment list and refuses one that names an event twice.
//
// The version id is stamped here rather than trusted from the caller, for the reason identifiers
// are never taken from a model: the id is the transaction's business, and a link naming another
// version would write a row into a version its own transaction is not creating.
func normaliseEventLinks(versionID string, links []scriptdomain.StrategyEventLink) ([]scriptdomain.StrategyEventLink, error) {
	seen := make(map[string]bool, len(links))
	out := make([]scriptdomain.StrategyEventLink, 0, len(links))
	for index, link := range links {
		eventID := strings.TrimSpace(link.StoryEventID)
		if eventID == "" {
			continue
		}
		if seen[eventID] {
			return nil, scriptdomain.InvalidError("That story event is listed twice: " + eventID + ".")
		}
		seen[eventID] = true
		treatment := link.Treatment
		if treatment == "" {
			// A treatment has no sensible default the way a line type or an interior marking does,
			// so an omitted one is refused rather than guessed: "retained" and "removed" are
			// opposite decisions and neither follows from an absence.
			return nil, scriptdomain.InvalidError("A story event needs a treatment: " + eventID + ".")
		}
		record := scriptdomain.StrategyEventLink{
			StrategyVersionID: versionID,
			StoryEventID:      eventID,
			Treatment:         treatment,
			// The ordinal is the POSITION, so a caller cannot state one that disagrees with the
			// order it wrote — which is the only thing "reordered" can mean.
			Ordinal: index + 1,
		}
		if err := record.Validate(); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

// assertStrategyLocks refuses a strategy revision that changed a field the user pinned.
//
// `mergedEventGroups` is compared as the TREATMENTS it names rather than as the JSON text: the
// field is a controlled structure (§2.6), and two payloads that differ only in key order or
// whitespace state the same strategy. Comparing the raw text would refuse a revision that changed
// nothing a reader could see.
func (s *Service) assertStrategyLocks(ctx context.Context, incoming scriptdomain.AdaptationStrategyVersion, links []scriptdomain.StrategyEventLink) error {
	locks, err := s.readLocks(ctx, incoming.BasedOnVersionID)
	if err != nil {
		return err
	}
	if len(locks) == 0 {
		return nil
	}
	if err := assertLocksCovered(locks, scriptdomain.FamilyAdaptationStrategy, scriptdomain.LockableFields(scriptdomain.FamilyAdaptationStrategy)); err != nil {
		return err
	}
	base, err := s.repository.GetAdaptationStrategyVersion(ctx, incoming.BasedOnVersionID)
	if err != nil {
		return err
	}
	baseLinks, err := s.repository.ListStrategyEventLinks(ctx, base.ID)
	if err != nil {
		return err
	}
	return compareFieldSets(locks, scriptdomain.FamilyAdaptationStrategy,
		fieldValues{
			scriptdomain.LockStrategySummary:      base.StrategySummary,
			scriptdomain.LockStrategyMode:         string(base.AdaptationMode),
			scriptdomain.LockStrategyMergedEvents: renderTreatments(baseLinks),
			scriptdomain.LockStrategyOriginalAdds: base.OriginalAdditions,
			scriptdomain.LockStrategyRationale:    base.Rationale,
			scriptdomain.LockStrategyRisks:        base.Risks,
		},
		fieldValues{
			scriptdomain.LockStrategySummary:      incoming.StrategySummary,
			scriptdomain.LockStrategyMode:         string(incoming.AdaptationMode),
			scriptdomain.LockStrategyMergedEvents: renderTreatments(links),
			scriptdomain.LockStrategyOriginalAdds: incoming.OriginalAdditions,
			scriptdomain.LockStrategyRationale:    incoming.Rationale,
			scriptdomain.LockStrategyRisks:        incoming.Risks,
		},
	)
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
		BasedOnVersionID:            strings.TrimSpace(request.BasedOnVersionID),
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
	// Section 17's ScriptVersionCreated. Best effort: the version is committed,
	// so the caller must not be told the command failed because the announcement
	// did not land. The project comes from the episode the script belongs to,
	// which the version itself does not name; a failed lookup means the
	// announcement is skipped, not that the command failed.
	if episode, episodeErr := s.repository.GetEpisode(ctx, script.EpisodeID); episodeErr == nil {
		s.recordEvent(ctx, eventsapp.Draft{
			Type:          event.ScriptVersionCreated,
			AggregateType: event.AggregateScript,
			AggregateID:   version.ID,
			ProjectID:     episode.ProjectID,
		})
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
	// TraceID correlates the approval's event with the action that caused it.
	// Optional: a manual approval has no run to correlate with.
	TraceID string
}

// ApproveScriptVersion makes one version the script's approved version.
//
// DOMAIN_MODEL §2.5 states both halves of the rule: at most one version of a
// parent may be approved, and approving a new one turns the previous approval
// into 'superseded'. versioning.CanApprove decides whether this version may be
// approved at all, so an already-approved version, a superseded one and a
// stale one are all refused with the domain's own message.
//
// The switch goes through one repository call because the schema's partial
// unique index makes its two writes order-dependent: the previous approval must
// leave 'approved' before the target enters it. The shared implementation in
// version_approval.go owns that order for all eight families, so this command
// cannot disagree with the others about it.
//
// The section 17 event is recorded in the same transaction, so an approval
// nobody can audit does not happen. A service composed without a recorder
// therefore refuses rather than approving silently.
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
	// The governance event, built before the switch so a service without a
	// recorder refuses rather than approving unrecorded.
	scriptRecord, err := s.repository.GetScript(ctx, version.ScriptID)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	episode, err := s.repository.GetEpisode(ctx, scriptRecord.EpisodeID)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	record, err := s.approveEvent(ctx, event.ScriptVersionApproved, event.AggregateScript,
		version.ID, episode.ProjectID, request.TraceID, "")
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	if err := s.repository.ApproveScriptVersion(ctx, version.ID, version.ScriptID, version.Status, record); err != nil {
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

// GetStorySkeletonVersion returns one skeleton version by id.
//
// It is a READ, and it exists because the agent runtime's tools need it: an
// execution agent creating a new version must be able to read what the previous one
// said, and a supervisor must be able to load the artifact it is reviewing rather
// than reading the executor's summary of it (AGENT_CONTRACTS section 5.1's rule that
// a reviewer loads the artifact with its own tools).
func (s *Service) GetStorySkeletonVersion(ctx context.Context, id string) (scriptdomain.StorySkeletonVersion, error) {
	if !s.Available() {
		return scriptdomain.StorySkeletonVersion{}, storageFailure()
	}
	return s.repository.GetStorySkeletonVersion(ctx, id)
}

// GetAdaptationStrategyVersion returns one strategy version by id.
func (s *Service) GetAdaptationStrategyVersion(ctx context.Context, id string) (scriptdomain.AdaptationStrategyVersion, error) {
	if !s.Available() {
		return scriptdomain.AdaptationStrategyVersion{}, storageFailure()
	}
	return s.repository.GetAdaptationStrategyVersion(ctx, id)
}

// GetScriptVersion returns one script version by id, and it reads the version
// whatever its approval status: a supervisor reviewing a draft needs the draft, and
// a read that silently returned only approved versions would make the review look at
// something other than what it was asked about.
func (s *Service) GetScriptVersion(ctx context.Context, id string) (scriptdomain.ScriptVersion, error) {
	if !s.Available() {
		return scriptdomain.ScriptVersion{}, storageFailure()
	}
	return s.repository.GetScriptVersion(ctx, id)
}

// GetScript returns one script by id, and it is the read a version needs to reach its
// episode: a ScriptVersion carries a ScriptID and the episode is the script's, so a
// caller checking which project an artifact belongs to walks version → script → episode.
func (s *Service) GetScript(ctx context.Context, id string) (scriptdomain.Script, error) {
	if !s.Available() {
		return scriptdomain.Script{}, storageFailure()
	}
	return s.repository.GetScript(ctx, id)
}
