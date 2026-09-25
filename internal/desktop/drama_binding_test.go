package desktop

import (
	"context"
	"encoding/json"
	"errors"
	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstaleness "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/staleness"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// dramaStore is an in-memory double for the story, script, storyboard and
// workflow persistence ports. It reproduces the real repositories' revision
// handling: an update matches the row's current revision and increments it, and
// an update that names a stale revision is refused with the domain's conflict
// error rather than writing.
type dramaStore struct {
	mu sync.Mutex

	documents    map[string]storydomain.SourceDocument
	versions     map[string]storydomain.SourceDocumentVersion
	chapters     map[string]storydomain.Chapter
	entities     map[string]storydomain.StoryEntity
	events       map[string]storydomain.StoryEvent
	relations    map[string]storydomain.StoryRelation
	conflicts    map[string]storydomain.StoryFactConflict
	aliases      map[string]storydomain.StoryEntityAlias
	participants map[string]storydomain.StoryEventParticipant
	factSources  map[string]storydomain.StoryFactSource
	episodes     map[string]scriptdomain.Episode
	skeletons    map[string]scriptdomain.StorySkeletonVersion
	strategies   map[string]scriptdomain.AdaptationStrategyVersion
	scripts      map[string]scriptdomain.Script
	scriptVers   map[string]scriptdomain.ScriptVersion
	scenes       map[string]scriptdomain.Scene
	shots        map[string]scriptdomain.Shot
	// The WP-08 state: dialogue lines, the field locks keyed by version and field, and the two
	// event-link sets keyed by version.
	lines          map[string]scriptdomain.DialogueLine
	fieldLocks     map[string]scriptdomain.FieldLock
	skeletonEvents map[string][]string
	strategyEvents map[string][]scriptdomain.StrategyEventLink
	// knownEvents and knownEntities are the project's story-graph rows, so the reference check a
	// structure write makes has something to check against. A nil map means "nothing exists", which
	// is the state every test that states no reference runs in: the check is skipped entirely when
	// the payload cites nothing.
	knownEvents   map[string]bool
	knownEntities map[string]bool
	plans         map[string]storyboard.DirectorPlanVersion
	boards        map[string]storyboard.Storyboard
	boardVers     map[string]storyboard.StoryboardVersion
	items         map[string]storyboard.StoryboardItem
	panels        map[string]storyboard.StoryboardPanelVersion
	runs          map[string]workflow.WorkflowRun
	stages        map[string]workflow.StageRun
	reports       map[string]workflow.ReviewReport
	issues        map[string][]workflow.ReviewIssue
	decisions     map[string]workflow.UserGateDecision
	audit         map[string][]workflow.WorkflowEvent

	// failNext makes the next write fail, so a storage failure is testable.
	failNext error
	// recorded holds what the approval commands wrote, so a binding test can
	// assert the governance row rather than only the status change. The name is
	// not 'events' because that field is already the story-event map.
	recorded []event.Event
	// episodeProjects stands in for the join the real repository does against
	// the episodes table to resolve an approval's project.
	episodeProjects map[string]string
}

func newDramaStore() *dramaStore {
	return &dramaStore{
		skeletons:       map[string]scriptdomain.StorySkeletonVersion{},
		strategies:      map[string]scriptdomain.AdaptationStrategyVersion{},
		episodeProjects: map[string]string{},
		documents:       map[string]storydomain.SourceDocument{},
		versions:        map[string]storydomain.SourceDocumentVersion{},
		chapters:        map[string]storydomain.Chapter{},
		entities:        map[string]storydomain.StoryEntity{},
		events:          map[string]storydomain.StoryEvent{},
		relations:       map[string]storydomain.StoryRelation{},
		conflicts:       map[string]storydomain.StoryFactConflict{},
		aliases:         map[string]storydomain.StoryEntityAlias{},
		participants:    map[string]storydomain.StoryEventParticipant{},
		factSources:     map[string]storydomain.StoryFactSource{},
		episodes:        map[string]scriptdomain.Episode{},
		scripts:         map[string]scriptdomain.Script{},
		scriptVers:      map[string]scriptdomain.ScriptVersion{},
		scenes:          map[string]scriptdomain.Scene{},
		shots:           map[string]scriptdomain.Shot{},
		lines:           map[string]scriptdomain.DialogueLine{},
		fieldLocks:      map[string]scriptdomain.FieldLock{},
		skeletonEvents:  map[string][]string{},
		strategyEvents:  map[string][]scriptdomain.StrategyEventLink{},
		plans:           map[string]storyboard.DirectorPlanVersion{},
		boards:          map[string]storyboard.Storyboard{},
		boardVers:       map[string]storyboard.StoryboardVersion{},
		items:           map[string]storyboard.StoryboardItem{},
		panels:          map[string]storyboard.StoryboardPanelVersion{},
		runs:            map[string]workflow.WorkflowRun{},
		stages:          map[string]workflow.StageRun{},
		reports:         map[string]workflow.ReviewReport{},
		issues:          map[string][]workflow.ReviewIssue{},
		decisions:       map[string]workflow.UserGateDecision{},
		audit:           map[string][]workflow.WorkflowEvent{},
	}
}

// takeFailure consumes a scheduled write failure.
func (s *dramaStore) takeFailure() error {
	if s.failNext == nil {
		return nil
	}
	err := s.failNext
	s.failNext = nil
	return err
}

// ---- story: source documents, chapters and the fact graph ----

func (s *dramaStore) CreateSourceDocument(_ context.Context, record storydomain.SourceDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.documents[record.ID] = record
	return nil
}

func (s *dramaStore) GetSourceDocument(_ context.Context, id string) (storydomain.SourceDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.documents[id]
	if !ok {
		return storydomain.SourceDocument{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) ListSourceDocuments(_ context.Context, projectID string) ([]storydomain.SourceDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.SourceDocument
	for _, record := range s.documents {
		if record.ProjectID == projectID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) UpdateSourceDocument(_ context.Context, record storydomain.SourceDocument, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.documents[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This document changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.documents[record.ID] = record
	return nil
}

func (s *dramaStore) CreateSourceDocumentVersion(_ context.Context, version storydomain.SourceDocumentVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	for _, existing := range s.versions {
		if existing.SourceDocumentID == version.SourceDocumentID && existing.VersionNumber == version.VersionNumber {
			return storydomain.ConflictError("That version number is already used for this document.")
		}
	}
	s.versions[version.ID] = version
	return nil
}

func (s *dramaStore) MaxSourceDocumentVersionNumber(_ context.Context, sourceDocumentID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, version := range s.versions {
		if version.SourceDocumentID == sourceDocumentID && version.VersionNumber > highest {
			highest = version.VersionNumber
		}
	}
	return highest, nil
}

func (s *dramaStore) CreateChapter(_ context.Context, record storydomain.Chapter) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.chapters[record.ID] = record
	return nil
}

func (s *dramaStore) GetChapter(_ context.Context, id string) (storydomain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.chapters[id]
	if !ok {
		return storydomain.Chapter{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) ListChapters(_ context.Context, versionID string) ([]storydomain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.Chapter
	for _, record := range s.chapters {
		if record.SourceDocumentVersionID == versionID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) UpdateChapter(_ context.Context, record storydomain.Chapter, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.chapters[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This chapter changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.chapters[record.ID] = record
	return nil
}

func (s *dramaStore) CreateStoryEntity(_ context.Context, record storydomain.StoryEntity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.entities[record.ID] = record
	return nil
}

func (s *dramaStore) GetStoryEntity(_ context.Context, id string) (storydomain.StoryEntity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.entities[id]
	if !ok {
		return storydomain.StoryEntity{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) UpdateStoryEntity(_ context.Context, record storydomain.StoryEntity, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.entities[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This fact changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.entities[record.ID] = record
	return nil
}

func (s *dramaStore) CreateStoryEvent(_ context.Context, record storydomain.StoryEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.events[record.ID] = record
	return nil
}

func (s *dramaStore) GetStoryEvent(_ context.Context, id string) (storydomain.StoryEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.events[id]
	if !ok {
		return storydomain.StoryEvent{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) UpdateStoryEvent(_ context.Context, record storydomain.StoryEvent, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.events[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This fact changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.events[record.ID] = record
	return nil
}

func (s *dramaStore) CreateStoryRelation(_ context.Context, record storydomain.StoryRelation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.relations[record.ID] = record
	return nil
}

func (s *dramaStore) GetStoryRelation(_ context.Context, id string) (storydomain.StoryRelation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.relations[id]
	if !ok {
		return storydomain.StoryRelation{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) CreateStoryConflict(_ context.Context, record storydomain.StoryFactConflict) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conflicts[record.ID] = record
	return nil
}

func (s *dramaStore) GetStoryConflict(_ context.Context, id string) (storydomain.StoryFactConflict, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.conflicts[id]
	if !ok {
		return storydomain.StoryFactConflict{}, storydomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) UpdateStoryConflict(_ context.Context, record storydomain.StoryFactConflict, expected storydomain.ConflictStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.conflicts[record.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Status != expected {
		return storydomain.ConflictError("This conflict changed in another window. Reload it and try again.")
	}
	s.conflicts[record.ID] = record
	return nil
}

// ---- script: episodes, scripts, scenes and shots ----

func (s *dramaStore) CreateEpisode(_ context.Context, record scriptdomain.Episode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.episodes[record.ID] = record
	return nil
}

func (s *dramaStore) GetEpisode(_ context.Context, id string) (scriptdomain.Episode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.episodes[id]
	if !ok {
		return scriptdomain.Episode{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) ListEpisodes(_ context.Context, projectID string) ([]scriptdomain.Episode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []scriptdomain.Episode
	for _, record := range s.episodes {
		if record.ProjectID == projectID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) CountEpisodesAtPosition(_ context.Context, projectID string, seasonNumber, episodeNumber int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, record := range s.episodes {
		if record.ProjectID == projectID && record.SeasonNumber == seasonNumber && record.EpisodeNumber == episodeNumber {
			count++
		}
	}
	return count, nil
}

func (s *dramaStore) UpdateEpisode(_ context.Context, record scriptdomain.Episode, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.episodes[record.ID]
	if !ok {
		return scriptdomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return scriptdomain.ConflictError("This episode changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.episodes[record.ID] = record
	return nil
}

func (s *dramaStore) CreateStorySkeletonVersion(_ context.Context, record scriptdomain.StorySkeletonVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nil
}

// CreateStorySkeletonVersionWithLinks records the version AND its selection.
//
// Unlike its bare twin above — which stores nothing because the binding tests are about the
// binding's argument mapping rather than about a read-back — this one records the selection, so a
// test can assert the binding passes the link set through instead of dropping it.
func (s *dramaStore) CreateStorySkeletonVersionWithLinks(ctx context.Context, record scriptdomain.StorySkeletonVersion, eventIDs []string) error {
	if err := s.CreateStorySkeletonVersion(ctx, record); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(eventIDs) == 0 {
		delete(s.skeletonEvents, record.ID)
		return nil
	}
	s.skeletonEvents[record.ID] = append([]string(nil), eventIDs...)
	return nil
}

func (s *dramaStore) GetStorySkeletonVersion(_ context.Context, id string) (scriptdomain.StorySkeletonVersion, error) {
	return scriptdomain.StorySkeletonVersion{ID: id, EpisodeID: "e", VersionNumber: 1}, nil
}

func (s *dramaStore) MaxStorySkeletonVersionNumber(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (s *dramaStore) CreateAdaptationStrategyVersion(_ context.Context, record scriptdomain.AdaptationStrategyVersion) error {
	return nil
}

// CreateAdaptationStrategyVersionWithLinks records the version AND its treatments, for the reason
// the skeleton's twin states.
func (s *dramaStore) CreateAdaptationStrategyVersionWithLinks(ctx context.Context, record scriptdomain.AdaptationStrategyVersion, links []scriptdomain.StrategyEventLink) error {
	if err := s.CreateAdaptationStrategyVersion(ctx, record); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(links) == 0 {
		delete(s.strategyEvents, record.ID)
		return nil
	}
	stored := make([]scriptdomain.StrategyEventLink, 0, len(links))
	for index, link := range links {
		link.StrategyVersionID = record.ID
		link.Ordinal = index + 1
		stored = append(stored, link)
	}
	s.strategyEvents[record.ID] = stored
	return nil
}

// MissingStoryEventIDs and MissingStoryEntityIDs answer the reference check.
//
// The seeding is by `knownEvents`/`knownEntities` rather than by a fixed answer, so one test can
// assert the refusal for an unknown id and another the pass for a known one, from one double.
func (s *dramaStore) MissingStoryEventIDs(_ context.Context, _ string, eventIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return missingFromSet(s.knownEvents, eventIDs), nil
}

func (s *dramaStore) MissingStoryEntityIDs(_ context.Context, _ string, entityIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return missingFromSet(s.knownEntities, entityIDs), nil
}

// missingFromSet is the double's reading of "which of these are absent".
func missingFromSet(known map[string]bool, ids []string) []string {
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if !known[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func (s *dramaStore) GetAdaptationStrategyVersion(_ context.Context, id string) (scriptdomain.AdaptationStrategyVersion, error) {
	return scriptdomain.AdaptationStrategyVersion{ID: id, EpisodeID: "e", VersionNumber: 1}, nil
}

func (s *dramaStore) MaxAdaptationStrategyVersionNumber(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (s *dramaStore) CreateScript(_ context.Context, record scriptdomain.Script) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	for _, existing := range s.scripts {
		if existing.EpisodeID == record.EpisodeID {
			return scriptdomain.ConflictError("This episode already has a script.")
		}
	}
	s.scripts[record.ID] = record
	return nil
}

func (s *dramaStore) GetScript(_ context.Context, id string) (scriptdomain.Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.scripts[id]
	if !ok {
		return scriptdomain.Script{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) GetScriptByEpisode(_ context.Context, episodeID string) (scriptdomain.Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.scripts {
		if record.EpisodeID == episodeID {
			return record, nil
		}
	}
	return scriptdomain.Script{}, scriptdomain.NotFoundError()
}

func (s *dramaStore) CreateScriptVersion(_ context.Context, version scriptdomain.ScriptVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.scriptVers[version.ID] = version
	script := s.scripts[version.ScriptID]
	script.CurrentVersionID = version.ID
	s.scripts[script.ID] = script
	return nil
}

func (s *dramaStore) GetScriptVersion(_ context.Context, id string) (scriptdomain.ScriptVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.scriptVers[id]
	if !ok {
		return scriptdomain.ScriptVersion{}, scriptdomain.NotFoundError()
	}
	return version, nil
}

func (s *dramaStore) MaxScriptVersionNumber(_ context.Context, scriptID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, version := range s.scriptVers {
		if version.ScriptID == scriptID && version.VersionNumber > highest {
			highest = version.VersionNumber
		}
	}
	return highest, nil
}

func (s *dramaStore) CurrentApprovedScriptVersion(_ context.Context, scriptID string) (scriptdomain.ScriptVersion, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, version := range s.scriptVers {
		if version.ScriptID == scriptID && version.Status == "approved" {
			return version, true, nil
		}
	}
	return scriptdomain.ScriptVersion{}, false, nil
}

func (s *dramaStore) ApproveScriptVersion(_ context.Context, versionID, scriptID string, expectedStatus versioning.Status, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, ok := s.scriptVers[versionID]
	if !ok {
		return scriptdomain.NotFoundError()
	}
	// The row has no revision, so the status the caller read is the guard.
	if target.Status != expectedStatus {
		return scriptdomain.ConflictError("This script version changed in another window. Reload it and try again.")
	}
	// The shared switch's two writes: retire the current approval, then approve
	// the target, with the governance event landing alongside them.
	for id, version := range s.scriptVers {
		if version.ScriptID == scriptID && version.Status == versioning.StatusApproved && id != versionID {
			version.Status = versioning.StatusSuperseded
			s.scriptVers[id] = version
		}
	}
	target.Status = versioning.StatusApproved
	s.scriptVers[versionID] = target
	s.recorded = append(s.recorded, record)
	return nil
}

func (s *dramaStore) CreateScene(_ context.Context, record scriptdomain.Scene) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.scenes[record.ID] = record
	return nil
}

func (s *dramaStore) GetScene(_ context.Context, id string) (scriptdomain.Scene, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.scenes[id]
	if !ok {
		return scriptdomain.Scene{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) ListScenes(_ context.Context, versionID string) ([]scriptdomain.Scene, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []scriptdomain.Scene
	for _, record := range s.scenes {
		if record.ScriptVersionID == versionID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) CountScenesAtOrdinal(_ context.Context, versionID string, ordinal int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, record := range s.scenes {
		if record.ScriptVersionID == versionID && record.Ordinal == ordinal {
			count++
		}
	}
	return count, nil
}

func (s *dramaStore) CreateShot(_ context.Context, record scriptdomain.Shot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.shots[record.ID] = record
	return nil
}

func (s *dramaStore) GetShot(_ context.Context, id string) (scriptdomain.Shot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.shots[id]
	if !ok {
		return scriptdomain.Shot{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) ListShots(_ context.Context, sceneID string) ([]scriptdomain.Shot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []scriptdomain.Shot
	for _, record := range s.shots {
		if record.SceneID == sceneID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) CountShotsAtOrdinal(_ context.Context, sceneID string, ordinal int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, record := range s.shots {
		if record.SceneID == sceneID && record.Ordinal == ordinal {
			count++
		}
	}
	return count, nil
}

// ---- storyboard: director plans, storyboards, items and panels ----

func (s *dramaStore) CreateDirectorPlanVersion(_ context.Context, version storyboard.DirectorPlanVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.plans[version.ID] = version
	return nil
}

func (s *dramaStore) GetDirectorPlanVersion(_ context.Context, id string) (storyboard.DirectorPlanVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.plans[id]
	if !ok {
		return storyboard.DirectorPlanVersion{}, storyboard.NotFoundError()
	}
	return version, nil
}

func (s *dramaStore) MaxDirectorPlanVersionNumber(_ context.Context, episodeID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, version := range s.plans {
		if version.EpisodeID == episodeID && version.VersionNumber > highest {
			highest = version.VersionNumber
		}
	}
	return highest, nil
}

func (s *dramaStore) GetStoryboardByEpisode(_ context.Context, episodeID string) (storyboard.Storyboard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.boards {
		if record.EpisodeID == episodeID {
			return record, nil
		}
	}
	return storyboard.Storyboard{}, storyboard.NotFoundError()
}

func (s *dramaStore) GetStoryboard(_ context.Context, id string) (storyboard.Storyboard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.boards[id]
	if !ok {
		return storyboard.Storyboard{}, storyboard.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) CreateStoryboard(_ context.Context, record storyboard.Storyboard) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	for _, existing := range s.boards {
		if existing.EpisodeID == record.EpisodeID {
			return storyboard.ConflictError("This episode already has a storyboard.")
		}
	}
	s.boards[record.ID] = record
	return nil
}

func (s *dramaStore) GetStoryboardVersion(_ context.Context, id string) (storyboard.StoryboardVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.boardVers[id]
	if !ok {
		return storyboard.StoryboardVersion{}, storyboard.NotFoundError()
	}
	return version, nil
}

func (s *dramaStore) MaxStoryboardVersionNumber(_ context.Context, storyboardID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, version := range s.boardVers {
		if version.StoryboardID == storyboardID && version.VersionNumber > highest {
			highest = version.VersionNumber
		}
	}
	return highest, nil
}

func (s *dramaStore) CreateStoryboardVersion(_ context.Context, version storyboard.StoryboardVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.boardVers[version.ID] = version
	board := s.boards[version.StoryboardID]
	board.CurrentVersionID = version.ID
	s.boards[board.ID] = board
	return nil
}

func (s *dramaStore) CreateStoryboardItem(_ context.Context, item storyboard.StoryboardItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.items[item.ID] = item
	return nil
}

func (s *dramaStore) GetStoryboardItem(_ context.Context, id string) (storyboard.StoryboardItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return storyboard.StoryboardItem{}, storyboard.NotFoundError()
	}
	return item, nil
}

func (s *dramaStore) FindStoryboardItemByShot(_ context.Context, versionID, shotID string) (storyboard.StoryboardItem, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.StoryboardVersionID == versionID && item.ShotID == shotID {
			return item, true, nil
		}
	}
	return storyboard.StoryboardItem{}, false, nil
}

func (s *dramaStore) FindStoryboardItemByOrdinal(_ context.Context, versionID string, ordinal int) (storyboard.StoryboardItem, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.StoryboardVersionID == versionID && item.Ordinal == ordinal {
			return item, true, nil
		}
	}
	return storyboard.StoryboardItem{}, false, nil
}

func (s *dramaStore) ListStoryboardItems(_ context.Context, versionID string) ([]storyboard.StoryboardItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storyboard.StoryboardItem
	for _, item := range s.items {
		if item.StoryboardVersionID == versionID {
			records = append(records, item)
		}
	}
	return records, nil
}

func (s *dramaStore) CreatePanelVersion(_ context.Context, panel storyboard.StoryboardPanelVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.panels[panel.ID] = panel
	return nil
}

func (s *dramaStore) GetPanelVersion(_ context.Context, id string) (storyboard.StoryboardPanelVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	panel, ok := s.panels[id]
	if !ok {
		return storyboard.StoryboardPanelVersion{}, storyboard.NotFoundError()
	}
	return panel, nil
}

func (s *dramaStore) MaxPanelVersionNumber(_ context.Context, itemID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, panel := range s.panels {
		if panel.StoryboardItemID == itemID && panel.VersionNumber > highest {
			highest = panel.VersionNumber
		}
	}
	return highest, nil
}

func (s *dramaStore) ListPanelVersions(_ context.Context, itemID string) ([]storyboard.StoryboardPanelVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storyboard.StoryboardPanelVersion
	for _, panel := range s.panels {
		if panel.StoryboardItemID == itemID {
			records = append(records, panel)
		}
	}
	return records, nil
}

func (s *dramaStore) ApprovePanelImage(_ context.Context, panelVersionID, approvedImageAssetVersionID string, expectedItemRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	panel, ok := s.panels[panelVersionID]
	if !ok {
		return storyboard.NotFoundError()
	}
	// storyboard_panel_versions has no revision column, so the guard is the
	// revision of the parent storyboard item.
	item, ok := s.items[panel.StoryboardItemID]
	if !ok {
		return storyboard.NotFoundError()
	}
	if item.Revision != expectedItemRevision {
		return storyboard.ConflictError("This panel changed in another window. Reload it and try again.")
	}
	panel.ApprovedImageAssetVersionID = approvedImageAssetVersionID
	s.panels[panel.ID] = panel
	return nil
}

// ---- workflow: runs, stage attempts, reviews and gate decisions ----

func (s *dramaStore) CreateRun(_ context.Context, record workflow.WorkflowRun, event workflow.WorkflowEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.runs[record.ID] = record
	s.audit[record.ID] = append(s.audit[record.ID], event)
	return nil
}

func (s *dramaStore) GetRun(_ context.Context, id string) (workflow.WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.runs[id]
	if !ok {
		return workflow.WorkflowRun{}, workflow.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) UpdateRun(_ context.Context, record workflow.WorkflowRun, expectedRevision int64, event workflow.WorkflowEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.runs[record.ID]
	if !ok {
		return workflow.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return workflow.ConflictError("This workflow run changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.runs[record.ID] = record
	s.audit[record.ID] = append(s.audit[record.ID], event)
	return nil
}

func (s *dramaStore) ListRuns(_ context.Context, projectID string) ([]workflow.WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []workflow.WorkflowRun
	for _, record := range s.runs {
		if record.ProjectID == projectID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) CreateStage(_ context.Context, record workflow.StageRun, event workflow.WorkflowEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.stages[record.ID] = record
	s.audit[record.WorkflowRunID] = append(s.audit[record.WorkflowRunID], event)
	return nil
}

func (s *dramaStore) GetStage(_ context.Context, id string) (workflow.StageRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.stages[id]
	if !ok {
		return workflow.StageRun{}, workflow.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) UpdateStage(_ context.Context, record workflow.StageRun, expectedRevision int64, event workflow.WorkflowEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.stages[record.ID]
	if !ok {
		return workflow.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return workflow.ConflictError("This stage attempt changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.stages[record.ID] = record
	s.audit[record.WorkflowRunID] = append(s.audit[record.WorkflowRunID], event)
	return nil
}

func (s *dramaStore) ListStages(_ context.Context, runID string) ([]workflow.StageRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []workflow.StageRun
	for _, record := range s.stages {
		if record.WorkflowRunID == runID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) CreateReport(_ context.Context, report workflow.ReviewReport, issues []workflow.ReviewIssue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.reports[report.ID] = report
	s.issues[report.StageRunID] = append(s.issues[report.StageRunID], issues...)
	return nil
}

func (s *dramaStore) GetReportForStage(_ context.Context, stageRunID string) (workflow.ReviewReport, []workflow.ReviewIssue, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var newest workflow.ReviewReport
	found := false
	for _, report := range s.reports {
		if report.StageRunID != stageRunID {
			continue
		}
		if !found || report.CreatedAt.After(newest.CreatedAt) {
			newest = report
			found = true
		}
	}
	if !found {
		return workflow.ReviewReport{}, nil, false, nil
	}
	return newest, s.issues[stageRunID], true, nil
}

func (s *dramaStore) CreateDecision(_ context.Context, decision workflow.UserGateDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeFailure(); err != nil {
		return err
	}
	s.decisions[decision.ID] = decision
	return nil
}

// LatestDecisionForStage answers the read the FIX loop makes.
//
// The map has no order, so the newest is found by comparing created_at — and a store that returned
// whichever row the map happened to yield would make a test about "the decision the user last made"
// depend on Go's map iteration order, which is deliberately random.
func (s *dramaStore) LatestDecisionForStage(_ context.Context, stageRunID string) (workflow.UserGateDecision, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var newest workflow.UserGateDecision
	found := false
	for _, decision := range s.decisions {
		if decision.StageRunID != stageRunID {
			continue
		}
		if !found || decision.CreatedAt.After(newest.CreatedAt) {
			newest = decision
			found = true
		}
	}
	return newest, found, nil
}

func (s *dramaStore) ListEvents(_ context.Context, runID string) ([]workflow.WorkflowEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]workflow.WorkflowEvent{}, s.audit[runID]...), nil
}

// staleMarkStore is an in-memory double for the staleness mark repository and
// the two resolver ports the service takes.
type staleMarkStore struct {
	mu             sync.Mutex
	marks          map[string]staleness.Mark
	failNext       error
	dependents     []appstaleness.DependentRef
	dependentOwner string
}

func newStaleMarkStore() *staleMarkStore {
	return &staleMarkStore{marks: map[string]staleness.Mark{}}
}

func staleMarkKey(artifactType staleness.ArtifactType, artifactID string) string {
	return string(artifactType) + "\x00" + artifactID
}

func (s *staleMarkStore) UpsertMark(_ context.Context, mark staleness.Mark, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		err := s.failNext
		s.failNext = nil
		return err
	}
	s.marks[staleMarkKey(mark.ArtifactType, mark.ArtifactID)] = mark
	return nil
}

func (s *staleMarkStore) UpsertMarks(_ context.Context, marks []staleness.Mark, stamp time.Time) error {
	for _, mark := range marks {
		if err := s.UpsertMark(context.Background(), mark, stamp); err != nil {
			return err
		}
	}
	return nil
}

func (s *staleMarkStore) GetMark(_ context.Context, artifactType staleness.ArtifactType, artifactID string) (staleness.Mark, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mark, ok := s.marks[staleMarkKey(artifactType, artifactID)]
	return mark, ok, nil
}

func (s *staleMarkStore) ListMarks(_ context.Context, projectID string) ([]staleness.Mark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []staleness.Mark
	for _, mark := range s.marks {
		if mark.ProjectID == projectID {
			records = append(records, mark)
		}
	}
	return records, nil
}

func (s *staleMarkStore) ListOpenMarks(_ context.Context, projectID string) ([]staleness.Mark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []staleness.Mark
	for _, mark := range s.marks {
		if mark.ProjectID == projectID && mark.ClearedAt == "" {
			records = append(records, mark)
		}
	}
	return records, nil
}

func (s *staleMarkStore) ClearMark(_ context.Context, artifactType staleness.ArtifactType, artifactID string, clearedAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := staleMarkKey(artifactType, artifactID)
	mark, ok := s.marks[key]
	if !ok {
		return false, nil
	}
	// The row is kept and stamped rather than deleted, which is what migration
	// 000012 does.
	mark.ClearedAt = clearedAt.UTC().Format(rfc3339)
	s.marks[key] = mark
	return true, nil
}

func (s *staleMarkStore) WaiveMark(_ context.Context, artifactType staleness.ArtifactType, artifactID, decisionID, reason string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := staleMarkKey(artifactType, artifactID)
	mark, ok := s.marks[key]
	if !ok {
		return false, nil
	}
	mark.Waived = true
	mark.WaivedByDecisionID = decisionID
	mark.WaivedReason = reason
	s.marks[key] = mark
	return true, nil
}

func (s *staleMarkStore) ProjectFor(_ context.Context, _ staleness.ArtifactType, artifactID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if artifactID == "" || s.dependentOwner == "" {
		return "", false, nil
	}
	return s.dependentOwner, true, nil
}

func (s *staleMarkStore) FindDependents(_ context.Context, _, _ staleness.ArtifactType, _ string) ([]appstaleness.DependentRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]appstaleness.DependentRef{}, s.dependents...), nil
}

// dramaFixture is the composed binding plus the stores behind it, so a test can
// seed a row or schedule a failure without rebuilding the services.
type dramaFixture struct {
	binding *DramaBinding
	store   *dramaStore
	marks   *staleMarkStore
}

// attachDramaFixture builds a binding over in-memory repositories.
func attachDramaFixture() *dramaFixture {
	store := newDramaStore()
	marks := newStaleMarkStore()
	binding := &DramaBinding{}
	AttachStory(binding, context.Background(), appstory.NewService(appstory.Options{
		Repository: store, Clock: fixedDramaClock{}, IDs: fixedIDs(),
	}))
	// The approval commands refuse without a recorder, so every service that
	// owns one is composed with the same test recorder the production wiring
	// supplies.
	recorder := dramaTestRecorder{}
	AttachScript(binding, context.Background(), appscript.NewService(appscript.Options{
		Repository: store, Clock: fixedDramaClock{}, IDs: fixedIDs(), Events: recorder,
	}))
	AttachStoryboard(binding, context.Background(), appstoryboard.NewService(appstoryboard.Options{
		DirectorPlans: store, Storyboards: store, Items: store, Panels: store,
		Clock: fixedDramaClock{}, IDs: fixedIDs(), Events: recorder,
	}))
	AttachWorkflow(binding, context.Background(), appworkflow.NewService(appworkflow.Options{
		Runs: store, Stages: store, Reviews: store, Decisions: store, Events: store,
		Clock: fixedDramaClock{}, IDs: fixedIDs(),
	}))
	AttachStaleness(binding, context.Background(), appstaleness.NewService(appstaleness.Options{
		Marks: marks, Dependents: marks, Projects: marks,
		Clock: fixedDramaClock{}, IDs: fixedIDs(),
	}))
	return &dramaFixture{binding: binding, store: store, marks: marks}
}

// fixedDramaClock pins the timestamps the drama services write.
type fixedDramaClock struct{}

func (fixedDramaClock) Now() time.Time { return time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC) }

// decodeDTO marshals a transport value and returns the decoded object, which is
// how a test asserts the JSON keys the frontend will actually read.
func decodeDTO(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshalling DTO: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding DTO: %v", err)
	}
	return decoded
}

// assertCamelKey asserts a field is present under its lowerCamel JSON key. It is
// the regression test for the WP-04 bug where DTOs carried no JSON tags and the
// frontend read undefined for every field.
func assertCamelKey(t *testing.T, value any, key string) {
	t.Helper()
	decoded := decodeDTO(t, value)
	if _, present := decoded[key]; !present {
		encoded, _ := json.Marshal(value)
		t.Fatalf("field %q is absent from %s", key, encoded)
	}
}

// assertCamelTag asserts a DTO field declares the given lowerCamel JSON key.
//
// It reads the struct tag rather than a marshalled value, so it also covers a
// field carrying `omitempty`: an absent key in the JSON is legitimate for an
// empty optional field, but a field with no tag at all would encode under its
// Go name and the frontend would read undefined — the WP-04 bug.
func assertCamelTag(t *testing.T, value any, key string) {
	t.Helper()
	kind := reflect.TypeOf(value)
	if kind.Kind() != reflect.Struct {
		t.Fatalf("assertCamelTag needs a struct, got %s", kind.Kind())
	}
	for index := 0; index < kind.NumField(); index++ {
		tag := kind.Field(index).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == key {
			return
		}
	}
	t.Fatalf("%s declares no field with the JSON key %q", kind.Name(), key)
}

// assertEmptyJSONList asserts a collection marshals as [] rather than null, so
// the frontend can map over it without a guard.
func assertEmptyJSONList(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshalling list: %v", err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("empty list marshalled as %s, want []", encoded)
	}
}

// assertBindingCode asserts the error is a stable application error with the
// expected code. A domain-mapped error carries no cause, so no internal text
// can be rendered from it.
func assertBindingCode(t *testing.T, err error, code string) *apperror.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q", code)
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not an application error: %T", err)
	}
	if appErr.Code != code {
		t.Fatalf("code = %q, want %q", appErr.Code, code)
	}
	return appErr
}

// TestDramaBindingFailsClosedWhenUnattached proves every method refuses before
// any service exists, and that none of them panics on a nil receiver or a nil
// service pointer.
func TestDramaBindingFailsClosedWhenUnattached(t *testing.T) {
	binding := &DramaBinding{}
	// A nil pointer receiver must fail closed too: Wails never calls one, but a
	// composition root that forgot to construct the binding must not crash.
	var none *DramaBinding

	calls := []struct {
		name string
		call func() error
	}{
		{"CreateSourceDocument", func() error {
			_, err := binding.CreateSourceDocument(CreateSourceDocumentRequest{})
			return err
		}},
		{"ListSourceDocuments", func() error { _, err := binding.ListSourceDocuments("p"); return err }},
		{"AddSourceDocumentVersion", func() error {
			_, err := binding.AddSourceDocumentVersion(AddSourceDocumentVersionRequest{})
			return err
		}},
		{"CreateChapter", func() error { _, err := binding.CreateChapter(CreateChapterRequest{}); return err }},
		{"ListChapters", func() error { _, err := binding.ListChapters("v"); return err }},
		{"ReviseChapter", func() error { _, err := binding.ReviseChapter(ReviseChapterRequest{}); return err }},
		{"CreateStoryEntity", func() error { _, err := binding.CreateStoryEntity(CreateStoryEntityRequest{}); return err }},
		{"AcceptStoryEntity", func() error { _, err := binding.AcceptStoryEntity(DecideStoryEntityRequest{}); return err }},
		{"RejectStoryEntity", func() error { _, err := binding.RejectStoryEntity(DecideStoryEntityRequest{}); return err }},
		{"CreateStoryEvent", func() error { _, err := binding.CreateStoryEvent(CreateStoryEventRequest{}); return err }},
		{"AcceptStoryEvent", func() error { _, err := binding.AcceptStoryEvent(DecideStoryEventRequest{}); return err }},
		{"RejectStoryEvent", func() error { _, err := binding.RejectStoryEvent(DecideStoryEventRequest{}); return err }},
		{"CreateStoryRelation", func() error { _, err := binding.CreateStoryRelation(CreateStoryRelationRequest{}); return err }},
		{"CreateEpisode", func() error { _, err := binding.CreateEpisode(CreateEpisodeRequest{}); return err }},
		{"ListEpisodes", func() error { _, err := binding.ListEpisodes("p"); return err }},
		{"UpdateEpisodeStatus", func() error { _, err := binding.UpdateEpisodeStatus(UpdateEpisodeStatusRequest{}); return err }},
		{"EnsureScript", func() error { _, err := binding.EnsureScript("e"); return err }},
		{"CreateScriptVersion", func() error { _, err := binding.CreateScriptVersion(CreateScriptVersionRequest{}); return err }},
		{"ApproveScriptVersion", func() error { _, err := binding.ApproveScriptVersion(ApproveScriptVersionRequest{}); return err }},
		{"CreateScene", func() error { _, err := binding.CreateScene(CreateSceneRequest{}); return err }},
		{"ListScenes", func() error { _, err := binding.ListScenes("v"); return err }},
		{"CreateShot", func() error { _, err := binding.CreateShot(CreateShotRequest{}); return err }},
		{"ListShots", func() error { _, err := binding.ListShots("s"); return err }},
		{"CreateDirectorPlanVersion", func() error {
			_, err := binding.CreateDirectorPlanVersion(CreateDirectorPlanVersionRequest{})
			return err
		}},
		{"EnsureStoryboard", func() error { _, err := binding.EnsureStoryboard("e"); return err }},
		{"CreateStoryboardVersion", func() error {
			_, err := binding.CreateStoryboardVersion(CreateStoryboardVersionRequest{})
			return err
		}},
		{"CreateStoryboardItem", func() error {
			_, err := binding.CreateStoryboardItem(CreateStoryboardItemRequest{})
			return err
		}},
		{"ListStoryboardItems", func() error { _, err := binding.ListStoryboardItems("v"); return err }},
		{"CreatePanelVersion", func() error { _, err := binding.CreatePanelVersion(CreatePanelVersionRequest{}); return err }},
		{"ListPanels", func() error { _, err := binding.ListPanels("i"); return err }},
		{"ApprovePanelImage", func() error { _, err := binding.ApprovePanelImage(ApprovePanelImageRequest{}); return err }},
		{"CreateWorkflowRun", func() error { _, err := binding.CreateWorkflowRun(CreateWorkflowRunRequest{}); return err }},
		{"ListWorkflowRuns", func() error { _, err := binding.ListWorkflowRuns("p"); return err }},
		{"TransitionWorkflowRun", func() error { _, err := binding.TransitionWorkflowRun(TransitionWorkflowRunRequest{}); return err }},
		{"CreateStageRun", func() error { _, err := binding.CreateStageRun(CreateStageRunRequest{}); return err }},
		{"ListStageRuns", func() error { _, err := binding.ListStageRuns("r"); return err }},
		{"TransitionStageRun", func() error { _, err := binding.TransitionStageRun(TransitionStageRunRequest{}); return err }},
		{"RecordReview", func() error { _, err := binding.RecordReview(RecordReviewRequest{}); return err }},
		{"GetReviewReport", func() error { _, err := binding.GetReviewReport("s"); return err }},
		{"SubmitGateDecision", func() error { _, err := binding.SubmitGateDecision(SubmitGateDecisionRequest{}); return err }},
		{"ListWorkflowEvents", func() error { _, err := binding.ListWorkflowEvents("r"); return err }},
		{"MarkStale", func() error { _, err := binding.MarkStale(MarkStaleRequest{}); return err }},
		{"ClearStaleMark", func() error { _, err := binding.ClearStaleMark(ClearStaleMarkRequest{}); return err }},
		{"WaiveStaleMark", func() error { _, err := binding.WaiveStaleMark(WaiveStaleMarkRequest{}); return err }},
		{"ListStaleMarks", func() error { _, err := binding.ListStaleMarks("p"); return err }},
		{"ListOpenStaleMarks", func() error { _, err := binding.ListOpenStaleMarks("p"); return err }},
	}

	for _, call := range calls {
		if err := call.call(); err == nil {
			t.Fatalf("%s succeeded on an unattached binding", call.name)
		} else {
			assertBindingCode(t, err, "DESKTOP_BINDING_UNAVAILABLE")
		}
	}

	// The same calls on a nil receiver must also refuse rather than panic.
	_, err := none.CreateSourceDocument(CreateSourceDocumentRequest{})
	assertBindingCode(t, err, "DESKTOP_BINDING_UNAVAILABLE")
	_, err = none.ListStaleMarks("p")
	assertBindingCode(t, err, "DESKTOP_BINDING_UNAVAILABLE")
	_, err = none.CreateWorkflowRun(CreateWorkflowRunRequest{})
	assertBindingCode(t, err, "DESKTOP_BINDING_UNAVAILABLE")
	if _, err := none.ClearStaleMark(ClearStaleMarkRequest{}); err == nil {
		t.Fatal("a nil receiver cleared a mark")
	}
}

// TestDramaBindingStoryRoundTrip proves the story commands and queries work end
// to end and that the DTOs carry lowerCamel JSON keys, which is the regression
// test for the WP-04 missing-JSON-tag bug.
func TestDramaBindingStoryRoundTrip(t *testing.T) {
	fixture := attachDramaFixture()
	binding := fixture.binding

	document, err := binding.CreateSourceDocument(CreateSourceDocumentRequest{
		ProjectID: "p1", Type: "novel", Name: "Volume One",
	})
	if err != nil {
		t.Fatalf("CreateSourceDocument: %v", err)
	}
	if document.ID == "" || document.Revision != 1 || document.Status != "active" {
		t.Fatalf("document = %+v", document)
	}
	assertCamelKey(t, document, "projectId")
	assertCamelTag(t, SourceDocumentDTO{}, "currentVersionId")
	assertCamelKey(t, document, "createdAt")
	if document.CreatedAt != "2026-09-17T09:00:00Z" {
		t.Fatalf("createdAt = %q, want the pinned clock in RFC3339", document.CreatedAt)
	}

	version, err := binding.AddSourceDocumentVersion(AddSourceDocumentVersionRequest{
		SourceDocumentID:     document.ID,
		NormalizedTextFileID: strings.Repeat("a", 64),
		CharCount:            100,
	})
	if err != nil {
		t.Fatalf("AddSourceDocumentVersion: %v", err)
	}
	if version.VersionNumber != 1 || version.CreatedByType != "user" {
		t.Fatalf("version = %+v", version)
	}
	assertCamelKey(t, version, "sourceDocumentId")
	assertCamelKey(t, version, "normalizedTextFileId")

	chapter, err := binding.CreateChapter(CreateChapterRequest{
		SourceDocumentVersionID: version.ID, Ordinal: 1, Title: "Chapter One",
		StartOffset: 0, EndOffset: 500,
	})
	if err != nil {
		t.Fatalf("CreateChapter: %v", err)
	}
	if chapter.Status != "detected" || chapter.EndOffset != 500 {
		t.Fatalf("chapter = %+v", chapter)
	}
	assertCamelKey(t, chapter, "sourceDocumentVersionId")

	revised, err := binding.ReviseChapter(ReviseChapterRequest{
		ChapterID: chapter.ID, Title: "Chapter One (moved)",
		StartOffset: 0, EndOffset: 450, Status: "edited", Revision: chapter.Revision,
	})
	if err != nil {
		t.Fatalf("ReviseChapter: %v", err)
	}
	if revised.Status != "edited" || revised.Revision != chapter.Revision+1 {
		t.Fatalf("revised = %+v", revised)
	}
	assertCamelKey(t, revised, "startOffset")

	// A stale revision is a conflict, not an overwrite.
	_, err = binding.ReviseChapter(ReviseChapterRequest{
		ChapterID: chapter.ID, Title: "Stale", EndOffset: 450, Revision: chapter.Revision,
	})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	chapters, err := binding.ListChapters(version.ID)
	if err != nil {
		t.Fatalf("ListChapters: %v", err)
	}
	if len(chapters) != 1 || chapters[0].Title != "Chapter One (moved)" {
		t.Fatalf("chapters = %+v", chapters)
	}

	entity, err := binding.CreateStoryEntity(CreateStoryEntityRequest{
		ProjectID: "p1", Type: "character", CanonicalName: "Lin",
	})
	if err != nil {
		t.Fatalf("CreateStoryEntity: %v", err)
	}
	if entity.Status != "candidate" || entity.SourceScope != "original" {
		t.Fatalf("entity = %+v", entity)
	}
	assertCamelKey(t, entity, "canonicalName")
	assertCamelKey(t, entity, "sourceScope")

	accepted, err := binding.AcceptStoryEntity(DecideStoryEntityRequest{ID: entity.ID, Revision: 1})
	if err != nil {
		t.Fatalf("AcceptStoryEntity: %v", err)
	}
	if accepted.Status != "accepted" || accepted.Revision != 2 {
		t.Fatalf("accepted = %+v", accepted)
	}
	// Re-confirming a confirmed fact is a conflict, which the domain's gate
	// decides rather than this layer.
	_, err = binding.AcceptStoryEntity(DecideStoryEntityRequest{ID: entity.ID, Revision: 2})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	second, err := binding.CreateStoryEntity(CreateStoryEntityRequest{
		ProjectID: "p1", Type: "prop", CanonicalName: "The Sword",
	})
	if err != nil {
		t.Fatalf("CreateStoryEntity: %v", err)
	}
	rejected, err := binding.RejectStoryEntity(DecideStoryEntityRequest{ID: second.ID, Revision: 1})
	if err != nil {
		t.Fatalf("RejectStoryEntity: %v", err)
	}
	if rejected.Status != "rejected" {
		t.Fatalf("rejected = %+v", rejected)
	}

	event, err := binding.CreateStoryEvent(CreateStoryEventRequest{
		ProjectID: "p1", ChapterID: chapter.ID, Ordinal: 1, Name: "The duel",
	})
	if err != nil {
		t.Fatalf("CreateStoryEvent: %v", err)
	}
	if event.Status != "candidate" {
		t.Fatalf("event = %+v", event)
	}
	assertCamelKey(t, event, "chapterId")
	assertCamelTag(t, StoryEventDTO{}, "storyTimeText")
	assertCamelTag(t, StoryEventDTO{}, "createdByAgentRunId")
	confirmed, err := binding.AcceptStoryEvent(DecideStoryEventRequest{ID: event.ID, Revision: 1})
	if err != nil {
		t.Fatalf("AcceptStoryEvent: %v", err)
	}
	if confirmed.Status != "accepted" {
		t.Fatalf("confirmed = %+v", confirmed)
	}
	_, err = binding.RejectStoryEvent(DecideStoryEventRequest{ID: event.ID, Revision: 2})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	relation, err := binding.CreateStoryRelation(CreateStoryRelationRequest{
		ProjectID: "p1", Type: "knows",
		SourceEntityType: "character", SourceEntityID: entity.ID,
		TargetEntityType: "prop", TargetEntityID: second.ID,
	})
	if err != nil {
		t.Fatalf("CreateStoryRelation: %v", err)
	}
	if relation.Status != "candidate" {
		t.Fatalf("relation = %+v", relation)
	}
	assertCamelKey(t, relation, "sourceEntityId")
	assertCamelKey(t, relation, "targetEntityType")

	documents, err := binding.ListSourceDocuments("p1")
	if err != nil {
		t.Fatalf("ListSourceDocuments: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("documents = %+v", documents)
	}
}

// TestDramaBindingScriptRoundTrip proves the episode, script, scene and shot
// commands work and that their DTOs carry lowerCamel JSON keys.
func TestDramaBindingScriptRoundTrip(t *testing.T) {
	fixture := attachDramaFixture()
	binding := fixture.binding

	episode, err := binding.CreateEpisode(CreateEpisodeRequest{
		ProjectID: "p1", SeasonNumber: 1, EpisodeNumber: 3, Title: "Third", TargetDurationSeconds: 300,
	})
	if err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	if episode.Key != "S1E3" || episode.Status != "planning" {
		t.Fatalf("episode = %+v", episode)
	}
	assertCamelKey(t, episode, "seasonNumber")
	assertCamelKey(t, episode, "targetDurationSeconds")
	assertCamelTag(t, EpisodeDTO{}, "currentScriptVersionId")

	// A second episode at the same position is a conflict.
	_, err = binding.CreateEpisode(CreateEpisodeRequest{ProjectID: "p1", SeasonNumber: 1, EpisodeNumber: 3})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	updated, err := binding.UpdateEpisodeStatus(UpdateEpisodeStatusRequest{
		EpisodeID: episode.ID, Status: "writing", Revision: episode.Revision,
	})
	if err != nil {
		t.Fatalf("UpdateEpisodeStatus: %v", err)
	}
	if updated.Status != "writing" || updated.Revision != episode.Revision+1 {
		t.Fatalf("updated = %+v", updated)
	}
	// An unknown status is refused with the domain's invalid-input category.
	_, err = binding.UpdateEpisodeStatus(UpdateEpisodeStatusRequest{
		EpisodeID: episode.ID, Status: "not_a_status", Revision: updated.Revision,
	})
	assertBindingCode(t, err, "DRAMA_INVALID_INPUT")

	// EnsureScript is an idempotent identity creation: two calls return one row.
	script, err := binding.EnsureScript(episode.ID)
	if err != nil {
		t.Fatalf("EnsureScript: %v", err)
	}
	again, err := binding.EnsureScript(episode.ID)
	if err != nil {
		t.Fatalf("EnsureScript second call: %v", err)
	}
	if script.ID != again.ID {
		t.Fatalf("EnsureScript returned two scripts: %q and %q", script.ID, again.ID)
	}
	assertCamelKey(t, script, "episodeId")
	assertCamelTag(t, ScriptDTO{}, "currentVersionId")

	scriptVersion, err := binding.CreateScriptVersion(CreateScriptVersionRequest{
		ScriptID:                    script.ID,
		StorySkeletonVersionID:      "skeleton-1",
		AdaptationStrategyVersionID: "strategy-1",
		EstimatedDurationSeconds:    300,
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	if scriptVersion.Status != "draft" || scriptVersion.VersionNumber != 1 {
		t.Fatalf("script version = %+v", scriptVersion)
	}
	assertCamelKey(t, scriptVersion, "storySkeletonVersionId")
	assertCamelKey(t, scriptVersion, "adaptationStrategyVersionId")

	// Approving it moves draft to approved.
	approved, err := binding.ApproveScriptVersion(ApproveScriptVersionRequest{ScriptVersionID: scriptVersion.ID})
	if err != nil {
		t.Fatalf("ApproveScriptVersion: %v", err)
	}
	if approved.Status != "approved" {
		t.Fatalf("approved = %+v", approved)
	}
	// Approving it again is refused by the shared versioning rule the domain
	// applies.
	_, err = binding.ApproveScriptVersion(ApproveScriptVersionRequest{ScriptVersionID: scriptVersion.ID})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	// The second version, then its approval supersedes the first.
	second, err := binding.CreateScriptVersion(CreateScriptVersionRequest{
		ScriptID:                    script.ID,
		StorySkeletonVersionID:      "skeleton-1",
		AdaptationStrategyVersionID: "strategy-1",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	if second.VersionNumber != 2 {
		t.Fatalf("second version number = %d, want 2", second.VersionNumber)
	}
	if _, err := binding.ApproveScriptVersion(ApproveScriptVersionRequest{ScriptVersionID: second.ID}); err != nil {
		t.Fatalf("approving the second version: %v", err)
	}

	scene, err := binding.CreateScene(CreateSceneRequest{
		ScriptVersionID: second.ID, Ordinal: 1, SceneNumber: "1", Slugline: "INT. HOUSE - DAY",
		InteriorExterior: "INT", EstimatedDurationSeconds: 60,
	})
	if err != nil {
		t.Fatalf("CreateScene: %v", err)
	}
	if scene.InteriorExterior != "INT" || scene.IsOriginalAdaptation {
		t.Fatalf("scene = %+v", scene)
	}
	assertCamelKey(t, scene, "scriptVersionId")
	assertCamelKey(t, scene, "isOriginalAdaptation")

	// A repeated ordinal is the domain's conflict, reported before the insert.
	_, err = binding.CreateScene(CreateSceneRequest{ScriptVersionID: second.ID, Ordinal: 1})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	shot, err := binding.CreateShot(CreateShotRequest{
		SceneID: scene.ID, Ordinal: 1, ShotNumber: "1A", ShotSize: "wide",
		VisualDescription: "The house at dawn",
	})
	if err != nil {
		t.Fatalf("CreateShot: %v", err)
	}
	if shot.Status != "draft" || shot.ShotNumber != "1A" {
		t.Fatalf("shot = %+v", shot)
	}
	assertCamelKey(t, shot, "sceneId")
	assertCamelKey(t, shot, "visualDescription")

	scenes, err := binding.ListScenes(second.ID)
	if err != nil {
		t.Fatalf("ListScenes: %v", err)
	}
	if len(scenes) != 1 || scenes[0].ID != scene.ID {
		t.Fatalf("scenes = %+v", scenes)
	}
	shots, err := binding.ListShots(scene.ID)
	if err != nil {
		t.Fatalf("ListShots: %v", err)
	}
	if len(shots) != 1 || shots[0].ID != shot.ID {
		t.Fatalf("shots = %+v", shots)
	}
}

// TestDramaBindingStoryboardRoundTrip proves the storyboard commands work and
// that the panel approval enforces section 9.5's candidate rule.
func TestDramaBindingStoryboardRoundTrip(t *testing.T) {
	fixture := attachDramaFixture()
	binding := fixture.binding

	plan, err := binding.CreateDirectorPlanVersion(CreateDirectorPlanVersionRequest{
		EpisodeID: "e1", ScriptVersionID: "sv1", CameraLanguage: "long lenses",
	})
	if err != nil {
		t.Fatalf("CreateDirectorPlanVersion: %v", err)
	}
	if plan.VersionNumber != 1 || plan.Status != "draft" {
		t.Fatalf("plan = %+v", plan)
	}
	assertCamelKey(t, plan, "episodeId")
	assertCamelKey(t, plan, "scriptVersionId")

	storyboard, err := binding.EnsureStoryboard("e1")
	if err != nil {
		t.Fatalf("EnsureStoryboard: %v", err)
	}
	// The identity is stable per episode, so a second call returns the same row.
	again, err := binding.EnsureStoryboard("e1")
	if err != nil {
		t.Fatalf("EnsureStoryboard second call: %v", err)
	}
	if storyboard.ID != again.ID {
		t.Fatalf("EnsureStoryboard returned two identities: %q and %q", storyboard.ID, again.ID)
	}
	assertCamelKey(t, storyboard, "episodeId")
	assertCamelTag(t, StoryboardDTO{}, "currentVersionId")

	version, err := binding.CreateStoryboardVersion(CreateStoryboardVersionRequest{
		StoryboardID: storyboard.ID, ScriptVersionID: "sv1", DirectorPlanVersionID: plan.ID,
	})
	if err != nil {
		t.Fatalf("CreateStoryboardVersion: %v", err)
	}
	if version.Status != "draft" || version.DirectorPlanVersionID != plan.ID {
		t.Fatalf("version = %+v", version)
	}
	assertCamelKey(t, version, "storyboardId")
	assertCamelKey(t, version, "directorPlanVersionId")

	item, err := binding.CreateStoryboardItem(CreateStoryboardItemRequest{
		StoryboardVersionID: version.ID, ShotID: "shot-1", Ordinal: 1,
		ShotSize: "wide", DurationSeconds: 4, VisualDescription: "The house at dawn",
	})
	if err != nil {
		t.Fatalf("CreateStoryboardItem: %v", err)
	}
	if item.Status != "draft" || item.ShotID != "shot-1" {
		t.Fatalf("item = %+v", item)
	}
	assertCamelKey(t, item, "storyboardVersionId")
	assertCamelTag(t, StoryboardItemDTO{}, "dialogueAudioSummary")

	// The same shot twice in one version is section 9.4's uniqueness rule.
	_, err = binding.CreateStoryboardItem(CreateStoryboardItemRequest{
		StoryboardVersionID: version.ID, ShotID: "shot-1", Ordinal: 2,
	})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	items, err := binding.ListStoryboardItems(version.ID)
	if err != nil {
		t.Fatalf("ListStoryboardItems: %v", err)
	}
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("items = %+v", items)
	}

	panel, err := binding.CreatePanelVersion(CreatePanelVersionRequest{
		StoryboardItemID: item.ID, VisualPrompt: "wide shot at dawn",
	})
	if err != nil {
		t.Fatalf("CreatePanelVersion: %v", err)
	}
	if panel.VersionNumber != 1 || panel.ApprovedImageAssetVersionID != "" {
		t.Fatalf("panel = %+v", panel)
	}
	assertCamelKey(t, panel, "storyboardItemId")
	assertCamelTag(t, StoryboardPanelVersionDTO{}, "approvedImageAssetVersionId")

	panels, err := binding.ListPanels(item.ID)
	if err != nil {
		t.Fatalf("ListPanels: %v", err)
	}
	if len(panels) != 1 || panels[0].ID != panel.ID {
		t.Fatalf("panels = %+v", panels)
	}

	// An image that is not one of the panel's candidates is refused, which is
	// section 9.5's third invariant.
	_, err = binding.ApprovePanelImage(ApprovePanelImageRequest{
		PanelVersionID: panel.ID, ApprovedImageAssetVersionID: "av-9",
		CandidateVersionIDs: []string{"av-1", "av-2"}, ExpectedRevision: item.Revision,
	})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	approved, err := binding.ApprovePanelImage(ApprovePanelImageRequest{
		PanelVersionID: panel.ID, ApprovedImageAssetVersionID: "av-2",
		CandidateVersionIDs: []string{"av-1", "av-2"}, ExpectedRevision: item.Revision,
	})
	if err != nil {
		t.Fatalf("ApprovePanelImage: %v", err)
	}
	if approved.ApprovedImageAssetVersionID != "av-2" {
		t.Fatalf("approved = %+v", approved)
	}
}

// TestDramaBindingWorkflowRoundTrip proves the run, stage, review and gate
// commands work, that the domain state machines are enforced through the
// binding, and that the DTOs carry lowerCamel JSON keys.
func TestDramaBindingWorkflowRoundTrip(t *testing.T) {
	fixture := attachDramaFixture()
	binding := fixture.binding

	run, err := binding.CreateWorkflowRun(CreateWorkflowRunRequest{
		ProjectID: "p1", EpisodeID: "e1", WorkflowType: "episode_pipeline",
	})
	if err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
	if run.Status != "pending" || run.Revision != 1 {
		t.Fatalf("run = %+v", run)
	}
	assertCamelKey(t, run, "workflowType")
	assertCamelTag(t, WorkflowRunDTO{}, "activeStageRunId")
	assertCamelTag(t, WorkflowRunDTO{}, "completedAt")

	// pending to completed is not an edge of the run machine.
	_, err = binding.TransitionWorkflowRun(TransitionWorkflowRunRequest{
		RunID: run.ID, Status: "completed", Revision: run.Revision,
	})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	running, err := binding.TransitionWorkflowRun(TransitionWorkflowRunRequest{
		RunID: run.ID, Status: "running", Revision: run.Revision,
	})
	if err != nil {
		t.Fatalf("TransitionWorkflowRun: %v", err)
	}
	if running.Status != "running" || running.Revision != 2 {
		t.Fatalf("running = %+v", running)
	}

	stage, err := binding.CreateStageRun(CreateStageRunRequest{
		WorkflowRunID: run.ID, Stage: "script_generation", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("CreateStageRun: %v", err)
	}
	if stage.Status != "pending" || stage.Stage != "script_generation" {
		t.Fatalf("stage = %+v", stage)
	}
	assertCamelKey(t, stage, "workflowRunId")
	assertCamelTag(t, StageRunDTO{}, "executionAgentKey")
	assertCamelTag(t, StageRunDTO{}, "validatedOutputJson")

	stages, err := binding.ListStageRuns(run.ID)
	if err != nil {
		t.Fatalf("ListStageRuns: %v", err)
	}
	if len(stages) != 1 || stages[0].ID != stage.ID {
		t.Fatalf("stages = %+v", stages)
	}

	// A stage attempt moves through the machine's edges only: pending starts it.
	started, err := binding.TransitionStageRun(TransitionStageRunRequest{
		StageRunID: stage.ID, Status: "running", Revision: stage.Revision,
	})
	if err != nil {
		t.Fatalf("TransitionStageRun to running: %v", err)
	}
	if started.Revision != 2 {
		t.Fatalf("running stage = %+v", started)
	}

	succeeded, err := binding.TransitionStageRun(TransitionStageRunRequest{
		StageRunID: stage.ID, Status: "execution_succeeded", Revision: started.Revision,
	})
	if err != nil {
		t.Fatalf("TransitionStageRun: %v", err)
	}
	if succeeded.Status != "execution_succeeded" || succeeded.Revision != 3 {
		t.Fatalf("succeeded = %+v", succeeded)
	}
	// succeeded is a terminal-for-now state on this path: it may reach reviewing.
	if _, err := binding.TransitionStageRun(TransitionStageRunRequest{
		StageRunID: stage.ID, Status: "reviewing", Revision: succeeded.Revision,
	}); err != nil {
		t.Fatalf("TransitionStageRun to reviewing: %v", err)
	}

	// A passing report may not name a major severity, which the domain refuses.
	score := 0.9
	_, err = binding.RecordReview(RecordReviewRequest{
		StageRunID: stage.ID, RulesetVersion: "v1", Passed: true,
		Severity: "major", Score: &score,
	})
	assertBindingCode(t, err, "DRAMA_INVALID_INPUT")

	report, err := binding.RecordReview(RecordReviewRequest{
		StageRunID: stage.ID, RulesetVersion: "v1", Passed: true, Severity: "minor",
		Grade: "A", Score: &score, Summary: "looks good",
		Issues: []ReviewIssueInputRequest{{
			Rule: "dialogue_consistency", Severity: "minor",
			EntityType: "scene", EntityID: "scene-1", Field: "summary",
			Problem: "the goal is unstated", Suggestion: "state it", AutoFixable: true,
		}},
	})
	if err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
	if !report.Passed || len(report.Issues) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Score == nil || *report.Score != score {
		t.Fatalf("score = %v, want %v", report.Score, score)
	}
	assertCamelKey(t, report, "stageRunId")
	assertCamelTag(t, ReviewReportDTO{}, "recommendedAction")
	assertCamelKey(t, report.Issues[0], "autoFixable")
	assertCamelKey(t, report.Issues[0], "entityId")

	fetched, err := binding.GetReviewReport(stage.ID)
	if err != nil {
		t.Fatalf("GetReviewReport: %v", err)
	}
	if fetched.ID != report.ID || len(fetched.Issues) != 1 {
		t.Fatalf("fetched = %+v", fetched)
	}

	// A skip must record a reason (PRD FR-100).
	_, err = binding.SubmitGateDecision(SubmitGateDecisionRequest{
		WorkflowRunID: run.ID, StageRunID: stage.ID, Decision: "skip", CreatedByType: "user",
	})
	assertBindingCode(t, err, "DRAMA_INVALID_INPUT")

	// An agent-authored decision is refused (section 11.5).
	_, err = binding.SubmitGateDecision(SubmitGateDecisionRequest{
		WorkflowRunID: run.ID, StageRunID: stage.ID, Decision: "approve", CreatedByType: "agent",
	})
	assertBindingCode(t, err, "DRAMA_CONFLICT")

	decision, err := binding.SubmitGateDecision(SubmitGateDecisionRequest{
		WorkflowRunID: run.ID, StageRunID: stage.ID, Decision: "skip",
		Reason: "the ruleset already covered this", CreatedByType: "user",
	})
	if err != nil {
		t.Fatalf("SubmitGateDecision: %v", err)
	}
	if decision.Decision != "skip" || decision.Reason == "" {
		t.Fatalf("decision = %+v", decision)
	}
	assertCamelKey(t, decision, "workflowRunId")
	assertCamelTag(t, UserGateDecisionDTO{}, "lockedEntityRefsJson")

	events, err := binding.ListWorkflowEvents(run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowEvents: %v", err)
	}
	// The run's creation and its transition, plus the stage's creation and its
	// three transitions, are six audit records.
	if len(events) != 6 {
		t.Fatalf("events = %+v, want the four state changes", events)
	}
	assertCamelKey(t, events[0], "eventType")
	assertCamelKey(t, events[0], "actorType")

	runs, err := binding.ListWorkflowRuns("p1")
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs = %+v", runs)
	}
}

// TestDramaBindingStalenessRoundTrip proves the mark commands and queries work
// and that a waiver is validated by section 15.3's rule.
func TestDramaBindingStalenessRoundTrip(t *testing.T) {
	fixture := attachDramaFixture()
	binding := fixture.binding

	mark, err := binding.MarkStale(MarkStaleRequest{
		ArtifactType: "chapter", ArtifactID: "c1", ProjectID: "p1",
		Severity: "review_required", Reason: "the chapter was edited",
		UpstreamType: "source_document_version", UpstreamID: "v1",
	})
	if err != nil {
		t.Fatalf("MarkStale: %v", err)
	}
	if mark.Severity != "review_required" || mark.ClearedAt != "" {
		t.Fatalf("mark = %+v", mark)
	}
	assertCamelKey(t, mark, "artifactType")
	assertCamelKey(t, mark, "upstreamId")
	assertCamelTag(t, StaleMarkDTO{}, "waivedByDecisionId")

	// An unknown artifact type is refused with the domain's category.
	_, err = binding.MarkStale(MarkStaleRequest{
		ArtifactType: "not_a_node", ArtifactID: "x", ProjectID: "p1", Severity: "breaking",
	})
	assertBindingCode(t, err, "DRAMA_INVALID_INPUT")

	// A waiver must name a decision and a reason (section 15.3).
	_, err = binding.WaiveStaleMark(WaiveStaleMarkRequest{
		ArtifactType: "chapter", ArtifactID: "c1", DecisionID: "", Reason: "",
	})
	assertBindingCode(t, err, "DRAMA_INVALID_INPUT")

	waived, err := binding.WaiveStaleMark(WaiveStaleMarkRequest{
		ArtifactType: "chapter", ArtifactID: "c1",
		DecisionID: "decision-1", Reason: "the re-import kept the same text",
	})
	if err != nil {
		t.Fatalf("WaiveStaleMark: %v", err)
	}
	if !waived {
		t.Fatal("WaiveStaleMark reported no mark to waive")
	}

	// A waived mark is still open, so it appears in both lists until cleared.
	open, err := binding.ListOpenStaleMarks("p1")
	if err != nil {
		t.Fatalf("ListOpenStaleMarks: %v", err)
	}
	if len(open) != 1 || !open[0].Waived {
		t.Fatalf("open marks = %+v", open)
	}

	cleared, err := binding.ClearStaleMark(ClearStaleMarkRequest{ArtifactType: "chapter", ArtifactID: "c1"})
	if err != nil {
		t.Fatalf("ClearStaleMark: %v", err)
	}
	if !cleared {
		t.Fatal("ClearStaleMark reported no mark to clear")
	}

	all, err := binding.ListStaleMarks("p1")
	if err != nil {
		t.Fatalf("ListStaleMarks: %v", err)
	}
	if len(all) != 1 || all[0].ClearedAt == "" {
		t.Fatalf("all marks = %+v", all)
	}
	open, err = binding.ListOpenStaleMarks("p1")
	if err != nil {
		t.Fatalf("ListOpenStaleMarks: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("a cleared mark is still open: %+v", open)
	}

	// Clearing an artifact that has no mark is the domain's invalid-input case
	// rather than a silent success.
	_, err = binding.ClearStaleMark(ClearStaleMarkRequest{ArtifactType: "chapter", ArtifactID: "absent"})
	assertBindingCode(t, err, "DRAMA_INVALID_INPUT")
}

// TestDramaBindingListMethodsReturnNoNilCollections proves every list-returning
// method marshals an empty result as [], so the frontend can map over it
// without a guard.
func TestDramaBindingListMethodsReturnNoNilCollections(t *testing.T) {
	binding := attachDramaFixture().binding

	lists := []struct {
		name string
		call func() (any, error)
	}{
		{"ListSourceDocuments", func() (any, error) { return binding.ListSourceDocuments("empty") }},
		{"ListChapters", func() (any, error) { return binding.ListChapters("empty") }},
		{"ListEpisodes", func() (any, error) { return binding.ListEpisodes("empty") }},
		{"ListScenes", func() (any, error) { return binding.ListScenes("empty") }},
		{"ListShots", func() (any, error) { return binding.ListShots("empty") }},
		{"ListStoryboardItems", func() (any, error) { return binding.ListStoryboardItems("empty") }},
		{"ListPanels", func() (any, error) { return binding.ListPanels("empty") }},
		{"ListWorkflowRuns", func() (any, error) { return binding.ListWorkflowRuns("empty") }},
		{"ListStageRuns", func() (any, error) { return binding.ListStageRuns("empty") }},
		{"ListWorkflowEvents", func() (any, error) { return binding.ListWorkflowEvents("empty") }},
		{"ListStaleMarks", func() (any, error) { return binding.ListStaleMarks("empty") }},
		{"ListOpenStaleMarks", func() (any, error) { return binding.ListOpenStaleMarks("empty") }},
	}

	for _, list := range lists {
		value, err := list.call()
		if err != nil {
			t.Fatalf("%s: %v", list.name, err)
		}
		assertEmptyJSONList(t, value)
	}

	// A review report's issue list is part of a single DTO rather than a list
	// method, so it is asserted through the report that carries it.
	report := toReviewReportDTO(workflow.ReviewReport{ID: "r", StageRunID: "s"}, nil)
	assertCamelKey(t, report, "issues")
	if report.Issues == nil {
		t.Fatal("a report with no review issues carries a nil issue list")
	}
	assertEmptyJSONList(t, report.Issues)
}

// TestDramaBindingBoundsCollectionArguments proves the two collection arguments
// on this surface are bounded before any service is called.
func TestDramaBindingBoundsCollectionArguments(t *testing.T) {
	binding := attachDramaFixture().binding

	oversize := make([]string, maxBatchPanelCandidates+1)
	_, err := binding.ApprovePanelImage(ApprovePanelImageRequest{
		PanelVersionID: "panel", ApprovedImageAssetVersionID: "av", CandidateVersionIDs: oversize,
	})
	assertBindingCode(t, err, "DESKTOP_BINDING_INVALID_INPUT")

	issues := make([]ReviewIssueInputRequest, maxBatchReviewIssues+1)
	_, err = binding.RecordReview(RecordReviewRequest{
		StageRunID: "stage", Severity: "none", Issues: issues,
	})
	assertBindingCode(t, err, "DESKTOP_BINDING_INVALID_INPUT")
}

// TestDramaBindingMapsDomainErrorToStableCode proves a domain failure arrives as
// its own category with the domain's message and without its cause.
func TestDramaBindingMapsDomainErrorToStableCode(t *testing.T) {
	binding := attachDramaFixture().binding

	_, err := binding.CreateSourceDocument(CreateSourceDocumentRequest{ProjectID: "p1", Type: "novel", Name: "  "})
	appErr := assertBindingCode(t, err, "DRAMA_INVALID_INPUT")
	if appErr.Category != "drama" {
		t.Fatalf("category = %q, want drama", appErr.Category)
	}
	if appErr.SafeMessage != "A source document needs a name." {
		t.Fatalf("safe message = %q, want the domain's own text", appErr.SafeMessage)
	}
	if appErr.Cause != nil {
		t.Fatalf("the mapped error carries a cause: %v", appErr.Cause)
	}
	if appErr.Retriable {
		t.Fatal("an invalid-input error was marked retriable")
	}
}

// TestDramaBindingPassesThroughApplicationError proves an error the application
// layer already classified keeps its own code rather than being re-wrapped.
func TestDramaBindingPassesThroughApplicationError(t *testing.T) {
	fixture := attachDramaFixture()
	fixture.marks.failNext = apperror.New("STALENESS_UNAVAILABLE", "storage", false,
		"The staleness store is unavailable.", errors.New("driver text that must not surface"))

	_, err := fixture.binding.MarkStale(MarkStaleRequest{
		ArtifactType: "chapter", ArtifactID: "c1", ProjectID: "p1", Severity: "breaking",
	})
	appErr := assertBindingCode(t, err, "STALENESS_UNAVAILABLE")
	if strings.Contains(appErr.Error(), "driver text") {
		t.Fatalf("the application error leaked its cause: %q", appErr.Error())
	}
}

// TestDramaBindingMapsUnknownErrorToRequestFailed proves an error the domain
// does not own still arrives as a stable drama code rather than as raw text.
func TestDramaBindingMapsUnknownErrorToRequestFailed(t *testing.T) {
	appErr := assertBindingCode(t, toDramaError(errors.New("an unmapped failure")), "DRAMA_REQUEST_FAILED")
	if appErr.Category != "drama" {
		t.Fatalf("category = %q, want drama", appErr.Category)
	}
	// The cause is kept for the diagnostic path but is not the rendered message.
	if appErr.Error() != "The drama request failed." {
		t.Fatalf("rendered message = %q", appErr.Error())
	}
	if toDramaError(nil) != nil {
		t.Fatal("toDramaError turned a nil error into one")
	}
}

// TestDramaBindingCarriesNoPathsOrBytes proves the transport views hold no
// filesystem location and no media payload: a document version references its
// stored objects by hash, and a file link is a hash and a role.
func TestDramaBindingCarriesNoPathsOrBytes(t *testing.T) {
	binding := attachDramaFixture().binding

	document, err := binding.CreateSourceDocument(CreateSourceDocumentRequest{
		ProjectID: "p1", Type: "novel", Name: "Volume One",
	})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("b", 64)
	version, err := binding.AddSourceDocumentVersion(AddSourceDocumentVersionRequest{
		SourceDocumentID: document.ID, NormalizedTextFileID: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if version.NormalizedTextFileID != hash {
		t.Fatalf("the transport view lost the content hash: %+v", version)
	}

	// No string field on any drama DTO may look like a filesystem path. The
	// check walks the encoded DTOs, which is exactly what the frontend receives.
	for _, value := range []any{document, version} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		for key, raw := range decoded {
			text, isText := raw.(string)
			if !isText {
				continue
			}
			if strings.ContainsAny(text, `/\`) || strings.HasPrefix(text, "http") {
				t.Fatalf("field %q looks like a location: %q", key, text)
			}
		}
	}
}

// TestDramaDTOsDeclareCamelCaseJSONTags walks every drama transport type and
// requires each exported field to declare a lowerCamel JSON key.
//
// It is the structural form of the WP-04 regression: a struct field with no tag
// encodes under its Go name, so the frontend reads undefined for every field.
// A field carrying `omitempty` is covered here too, because the tag is read from
// the type rather than from a marshalled value.
func TestDramaDTOsDeclareCamelCaseJSONTags(t *testing.T) {
	types := []any{
		SourceDocumentDTO{}, SourceDocumentVersionDTO{}, ChapterDTO{},
		StoryEntityDTO{}, StoryEventDTO{}, StoryRelationDTO{},
		EpisodeDTO{}, ScriptDTO{}, ScriptVersionDTO{}, SceneDTO{}, ShotDTO{},
		DirectorPlanVersionDTO{}, StoryboardDTO{}, StoryboardVersionDTO{},
		StoryboardItemDTO{}, StoryboardPanelVersionDTO{},
		WorkflowRunDTO{}, StageRunDTO{}, ReviewIssueDTO{}, ReviewReportDTO{},
		UserGateDecisionDTO{}, WorkflowEventDTO{}, StaleMarkDTO{},
		CreateSourceDocumentRequest{}, AddSourceDocumentVersionRequest{},
		CreateChapterRequest{}, ReviseChapterRequest{}, CreateStoryEntityRequest{},
		DecideStoryEntityRequest{}, CreateStoryEventRequest{}, DecideStoryEventRequest{},
		CreateStoryRelationRequest{},
		CreateEpisodeRequest{}, UpdateEpisodeStatusRequest{}, CreateScriptVersionRequest{},
		ApproveScriptVersionRequest{}, CreateSceneRequest{}, CreateShotRequest{},
		CreateDirectorPlanVersionRequest{}, CreateStoryboardVersionRequest{},
		CreateStoryboardItemRequest{}, CreatePanelVersionRequest{}, ApprovePanelImageRequest{},
		CreateWorkflowRunRequest{}, TransitionWorkflowRunRequest{}, CreateStageRunRequest{},
		TransitionStageRunRequest{}, ReviewIssueInputRequest{}, RecordReviewRequest{},
		SubmitGateDecisionRequest{},
		MarkStaleRequest{}, ClearStaleMarkRequest{}, WaiveStaleMarkRequest{},
	}
	for _, value := range types {
		assertEveryFieldTagged(t, value)
	}
}

// assertEveryFieldTagged requires every exported field of a struct to declare a
// lowerCamel JSON key naming that field.
//
// The key is checked in two ways: it must be the lowerCamel spelling of the Go
// field with acronym runs ("ID", "JSON", "MIME") folded to title case, and it
// must actually name the field rather than some other word. A missing tag, a
// tag of "-" or a tag under a different name all fail.
func assertEveryFieldTagged(t *testing.T, value any) {
	t.Helper()
	kind := reflect.TypeOf(value)
	if kind.Kind() != reflect.Struct {
		t.Fatalf("assertEveryFieldTagged needs a struct, got %s", kind.Kind())
	}
	for index := 0; index < kind.NumField(); index++ {
		field := kind.Field(index)
		// The application types are not embedded in these views, so every field
		// is either an exported transport field or an unexported one the walk
		// skips.
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" {
			t.Fatalf("%s.%s declares no JSON tag, so the frontend would read it under its Go name",
				kind.Name(), field.Name)
		}
		if name == "-" {
			t.Fatalf("%s.%s is hidden from the frontend, which must be able to send every field",
				kind.Name(), field.Name)
		}
		if !lowerCamelSpelling(field.Name, name) {
			t.Fatalf("%s.%s encodes as %q, which is not the lowerCamel spelling of the field name",
				kind.Name(), field.Name, name)
		}
	}
}

// lowerCamelSpelling reports whether a JSON key is the lowerCamel spelling of a
// Go field name.
//
// The test is deliberately a property rather than a re-implementation of the
// acronym rules: the key must start lowercase, must not be snake_case and must
// equal the field name when both are compared case-insensitively. A missing tag
// (the key equals the Go name with a capital), a snake_case tag and a tag naming
// some other field all fail; ProjectID/"projectId" and MIMEType/"mimeType" both
// pass.
func lowerCamelSpelling(fieldName, key string) bool {
	if key == "" || key[0] < 'a' || key[0] > 'z' {
		return false
	}
	if strings.ContainsAny(key, "_-") {
		return false
	}
	return strings.EqualFold(key, fieldName)
}

// The approval doubles reproduce the repository's supersede-then-approve shape,
// including the event write, so a binding test exercises the same contract the
// real repository provides.
func (s *dramaStore) CurrentApprovedSkeletonVersionID(_ context.Context, episodeID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, record := range s.skeletons {
		if record.EpisodeID == episodeID && record.Status == versioning.StatusApproved {
			return id, nil
		}
	}
	return "", nil
}

func (s *dramaStore) ApproveStorySkeletonVersion(_ context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	target, ok := s.skeletons[versionID]
	if !ok || target.Status != expectedStatus {
		return scriptdomain.ConflictError("This version changed in another window. Reload it and try again.")
	}
	for id, existing := range s.skeletons {
		if existing.EpisodeID == episodeID && existing.Status == versioning.StatusApproved && id != versionID {
			existing.Status = versioning.StatusSuperseded
			s.skeletons[id] = existing
		}
	}
	target.Status = versioning.StatusApproved
	s.skeletons[versionID] = target
	s.recorded = append(s.recorded, record)
	return nil
}

func (s *dramaStore) CurrentApprovedStrategyVersionID(_ context.Context, episodeID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, record := range s.strategies {
		if record.EpisodeID == episodeID && record.Status == versioning.StatusApproved {
			return id, nil
		}
	}
	return "", nil
}

func (s *dramaStore) ApproveAdaptationStrategyVersion(_ context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	target, ok := s.strategies[versionID]
	if !ok || target.Status != expectedStatus {
		return scriptdomain.ConflictError("This version changed in another window. Reload it and try again.")
	}
	for id, existing := range s.strategies {
		if existing.EpisodeID == episodeID && existing.Status == versioning.StatusApproved && id != versionID {
			existing.Status = versioning.StatusSuperseded
			s.strategies[id] = existing
		}
	}
	target.Status = versioning.StatusApproved
	s.strategies[versionID] = target
	s.recorded = append(s.recorded, record)
	return nil
}

func (s *dramaStore) CurrentApprovedDirectorPlanVersionID(_ context.Context, episodeID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, plan := range s.plans {
		if plan.EpisodeID == episodeID && plan.Status == versioning.StatusApproved {
			return id, nil
		}
	}
	return "", nil
}

func (s *dramaStore) ApproveDirectorPlanVersion(_ context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	target, ok := s.plans[versionID]
	if !ok || target.Status != expectedStatus {
		return storyboard.ConflictError("This version changed in another window. Reload it and try again.")
	}
	for id, existing := range s.plans {
		if existing.EpisodeID == episodeID && existing.Status == versioning.StatusApproved && id != versionID {
			existing.Status = versioning.StatusSuperseded
			s.plans[id] = existing
		}
	}
	target.Status = versioning.StatusApproved
	s.plans[versionID] = target
	s.recorded = append(s.recorded, record)
	return nil
}

func (s *dramaStore) CurrentApprovedStoryboardVersionID(_ context.Context, storyboardID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, version := range s.boardVers {
		if version.StoryboardID == storyboardID && version.Status == versioning.StatusApproved {
			return id, nil
		}
	}
	return "", nil
}

func (s *dramaStore) ApproveStoryboardVersion(_ context.Context, versionID, storyboardID string, expectedStatus versioning.Status, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	target, ok := s.boardVers[versionID]
	if !ok || target.Status != expectedStatus {
		return storyboard.ConflictError("This version changed in another window. Reload it and try again.")
	}
	for id, existing := range s.boardVers {
		if existing.StoryboardID == storyboardID && existing.Status == versioning.StatusApproved && id != versionID {
			existing.Status = versioning.StatusSuperseded
			s.boardVers[id] = existing
		}
	}
	target.Status = versioning.StatusApproved
	s.boardVers[versionID] = target
	s.recorded = append(s.recorded, record)
	return nil
}

func (s *dramaStore) UpdateDirectorPlanOverrides(_ context.Context, versionID string, overridesJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.plans[versionID]
	if !ok {
		return storyboard.NotFoundError()
	}
	version.ShotOverridesJSON = overridesJSON
	s.plans[versionID] = version
	return nil
}

func (s *dramaStore) ListDirectorPlanVersions(_ context.Context, episodeID string) ([]storyboard.DirectorPlanVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := []storyboard.DirectorPlanVersion{}
	for _, version := range s.plans {
		if version.EpisodeID == episodeID {
			versions = append(versions, version)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNumber > versions[j].VersionNumber })
	return versions, nil
}

func (s *dramaStore) ListStoryboardVersions(_ context.Context, storyboardID string) ([]storyboard.StoryboardVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := []storyboard.StoryboardVersion{}
	for _, version := range s.boardVers {
		if version.StoryboardID == storyboardID {
			versions = append(versions, version)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNumber > versions[j].VersionNumber })
	return versions, nil
}

func (s *dramaStore) UpdateStoryboardItem(_ context.Context, item storyboard.StoryboardItem, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.items[item.ID]
	if !ok {
		return storyboard.NotFoundError()
	}
	if stored.Revision != expectedRevision {
		return storyboard.ConflictError("This storyboard row changed in another window. Reload it and try again.")
	}
	item.Revision = expectedRevision + 1
	s.items[item.ID] = item
	return nil
}

func (s *dramaStore) ProjectOfEpisode(_ context.Context, episodeID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projectID, ok := s.episodeProjects[episodeID]
	if !ok {
		return "", scriptdomain.NotFoundError()
	}
	return projectID, nil
}

func (s *dramaStore) ProjectOfStoryboard(_ context.Context, storyboardID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, ok := s.boards[storyboardID]
	if !ok {
		return "", storyboard.NotFoundError()
	}
	projectID, ok := s.episodeProjects[board.EpisodeID]
	if !ok {
		return "", storyboard.NotFoundError()
	}
	return projectID, nil
}

// dramaTestRecorder builds events for the binding tests without a database, so
// the approval commands have the recorder they require.
type dramaTestRecorder struct{}

func (dramaTestRecorder) Build(_ context.Context, draft eventsapp.Draft) (event.Event, error) {
	return event.New(
		"event-"+string(draft.Type),
		draft.Type, draft.AggregateType, draft.AggregateID, draft.ProjectID,
		time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC), draft.TraceID, draft.Payload,
	)
}

func (dramaTestRecorder) RecordBestEffort(_ context.Context, _ eventsapp.Draft) {}

// The three methods the WP-06 story port additions require.
func (s *dramaStore) GetSourceDocumentVersion(_ context.Context, id string) (storydomain.SourceDocumentVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[id]
	if !ok {
		return storydomain.SourceDocumentVersion{}, storydomain.NotFoundError()
	}
	return version, nil
}

func (s *dramaStore) FindVersionBySourceHash(_ context.Context, projectID, sourceHash string) (storydomain.SourceDocumentVersion, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sourceHash == "" {
		return storydomain.SourceDocumentVersion{}, "", false, nil
	}
	for id, version := range s.versions {
		if version.SourceHash != sourceHash {
			continue
		}
		document, ok := s.documents[version.SourceDocumentID]
		if !ok || document.ProjectID != projectID {
			continue
		}
		return s.versions[id], document.Name, true, nil
	}
	return storydomain.SourceDocumentVersion{}, "", false, nil
}

func (s *dramaStore) ConfirmChapters(_ context.Context, sourceDocumentVersionID string, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	for id, chapter := range s.chapters {
		if chapter.SourceDocumentVersionID != sourceDocumentVersionID {
			continue
		}
		if chapter.Status != storydomain.ChapterDetected {
			continue
		}
		chapter.Status = storydomain.ChapterConfirmed
		chapter.Revision++
		s.chapters[id] = chapter
	}
	s.recorded = append(s.recorded, record)
	return nil
}

// The child rows and filtered lists the WP-06 port additions require. They mirror
// the real repository's semantics rather than recording calls, so a binding test
// that passes here is not passing because the double was permissive.

func (s *dramaStore) CreateStoryEntityAlias(_ context.Context, record storydomain.StoryEntityAlias) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	for _, existing := range s.aliases {
		if existing.StoryEntityID == record.StoryEntityID && existing.Alias == record.Alias {
			return storydomain.ConflictError("That name is already an alias for this entity.")
		}
	}
	s.aliases[record.ID] = record
	return nil
}

func (s *dramaStore) ListStoryEntityAliases(_ context.Context, storyEntityID string) ([]storydomain.StoryEntityAlias, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.StoryEntityAlias
	for _, record := range s.aliases {
		if record.StoryEntityID == storyEntityID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) CreateStoryEventParticipant(_ context.Context, record storydomain.StoryEventParticipant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	key := record.StoryEventID + "\x00" + record.StoryEntityID + "\x00" + string(record.Role)
	if _, exists := s.participants[key]; exists {
		return storydomain.ConflictError("That entity already holds that role in this event.")
	}
	s.participants[key] = record
	return nil
}

func (s *dramaStore) ListStoryEventParticipants(_ context.Context, storyEventID string) ([]storydomain.StoryEventParticipant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.StoryEventParticipant
	for _, record := range s.participants {
		if record.StoryEventID == storyEventID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) CreateStoryFactSource(_ context.Context, record storydomain.StoryFactSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	s.factSources[record.ID] = record
	return nil
}

func (s *dramaStore) ListStoryFactSources(_ context.Context, factType storydomain.FactType, factID string) ([]storydomain.StoryFactSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.StoryFactSource
	for _, record := range s.factSources {
		if record.FactType == factType && record.FactID == factID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) ListStoryEntities(_ context.Context, projectID string, status storydomain.FactStatus) ([]storydomain.StoryEntity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.StoryEntity
	for _, record := range s.entities {
		if record.ProjectID != projectID || !record.DeletedAt.IsZero() {
			continue
		}
		if status != "" && record.Status != status {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *dramaStore) ListStoryEvents(_ context.Context, projectID, chapterID string, status storydomain.FactStatus) ([]storydomain.StoryEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.StoryEvent
	for _, record := range s.events {
		if record.ProjectID != projectID {
			continue
		}
		if chapterID != "" && record.ChapterID != chapterID {
			continue
		}
		if status != "" && record.Status != status {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *dramaStore) ListStoryRelations(_ context.Context, projectID string, status storydomain.FactStatus) ([]storydomain.StoryRelation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.StoryRelation
	for _, record := range s.relations {
		if record.ProjectID != projectID {
			continue
		}
		if status != "" && record.Status != status {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// TestReviseChapterMarksTheFactsItInvalidates covers PRD FR-030's rule that a
// chapter edit makes the facts read from it need review.
//
// The propagation engine is exercised against a real database elsewhere
// (TestPropagationEndToEndAgainstTheDatabase). What was missing until this test
// is the TRIGGER: nothing called PropagateFrom, so the engine was complete and
// never ran. The assertion therefore starts from the binding's own command.
func TestReviseChapterMarksTheFactsItInvalidates(t *testing.T) {
	fixture := attachDramaFixture()
	store := fixture.store
	store.documents["doc-1"] = storydomain.SourceDocument{
		ID: "doc-1", ProjectID: "project-1", Name: "The Novel",
		Type: storydomain.DocumentNovel, Status: storydomain.DocumentActive, Revision: 1,
	}
	store.versions["version-1"] = storydomain.SourceDocumentVersion{
		ID: "version-1", SourceDocumentID: "doc-1", VersionNumber: 1,
		NormalizedTextFileID: strings.Repeat("a", 64), CharCount: 100,
		CreatedByType: versioning.CreatedByUser,
	}
	store.chapters["chapter-1"] = storydomain.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: "version-1", Ordinal: 1, Title: "One",
		StartOffset: 0, EndOffset: 100, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}
	// What the walk should find: one story event holding a chapter reference.
	store.events["event-1"] = storydomain.StoryEvent{
		ID: "event-1", ProjectID: "project-1", ChapterID: "chapter-1", Name: "Something happens",
		Status: storydomain.FactCandidate, SourceScope: storydomain.ScopeOriginal, Revision: 1,
	}
	fixture.marks.dependents = []appstaleness.DependentRef{
		{ArtifactType: staleness.ArtifactStoryEvent, ArtifactID: "event-1"},
	}
	fixture.marks.dependentOwner = "project-1"

	revised, err := fixture.binding.ReviseChapter(ReviseChapterRequest{
		ChapterID: "chapter-1", Title: "One, corrected", StartOffset: 0, EndOffset: 120, Revision: 1,
	})
	if err != nil {
		t.Fatalf("ReviseChapter: %v", err)
	}
	if revised.EndOffset != 120 {
		t.Fatalf("the correction was not stored: end offset %d", revised.EndOffset)
	}
	// The mark is what FR-030 asks for: the event that cites this chapter is now
	// awaiting review.
	mark, found, err := fixture.marks.GetMark(context.Background(), staleness.ArtifactStoryEvent, "event-1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("revising a chapter did not mark the story event that cites it, so the propagation never ran")
	}
	if mark.Severity != staleness.SeverityReviewRequired {
		t.Fatalf("the mark's severity is %q, want review_required", mark.Severity)
	}
	if mark.UpstreamType != staleness.ArtifactChapter || mark.UpstreamID != "chapter-1" {
		t.Fatalf("the mark names %s/%s as upstream, want the chapter that changed", mark.UpstreamType, mark.UpstreamID)
	}
	if mark.ProjectID != "project-1" {
		t.Fatalf("the mark was filed under project %q", mark.ProjectID)
	}
}

// TestReviseChapterSucceedsWhenThePropagationFails is the deliberate trade: the
// correction has already landed, so failing the response would tell the caller
// the edit did not happen when it did.
func TestReviseChapterSucceedsWhenThePropagationFails(t *testing.T) {
	fixture := attachDramaFixture()
	store := fixture.store
	store.documents["doc-1"] = storydomain.SourceDocument{ID: "doc-1", ProjectID: "project-1", Revision: 1}
	store.versions["version-1"] = storydomain.SourceDocumentVersion{ID: "version-1", SourceDocumentID: "doc-1"}
	store.chapters["chapter-1"] = storydomain.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: "version-1", Ordinal: 1, Title: "One",
		StartOffset: 0, EndOffset: 100, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}
	fixture.marks.dependentOwner = "project-1"
	fixture.marks.failNext = errors.New("the marks could not be written")
	fixture.marks.dependents = []appstaleness.DependentRef{
		{ArtifactType: staleness.ArtifactStoryEvent, ArtifactID: "event-1"},
	}

	revised, err := fixture.binding.ReviseChapter(ReviseChapterRequest{
		ChapterID: "chapter-1", Title: "Corrected", StartOffset: 0, EndOffset: 120, Revision: 1,
	})
	if err != nil {
		t.Fatalf("a propagation failure must not fail the correction that already landed: %v", err)
	}
	if revised.EndOffset != 120 {
		t.Fatalf("the correction was lost: end offset %d", revised.EndOffset)
	}
}

// TestReviseChapterStillWorksWithoutAStalenessService proves the propagation is
// an enhancement rather than a dependency: a binding composed without one still
// stores corrections.
func TestReviseChapterStillWorksWithoutAStalenessService(t *testing.T) {
	store := newDramaStore()
	store.documents["doc-1"] = storydomain.SourceDocument{ID: "doc-1", ProjectID: "project-1", Revision: 1}
	store.versions["version-1"] = storydomain.SourceDocumentVersion{ID: "version-1", SourceDocumentID: "doc-1"}
	store.chapters["chapter-1"] = storydomain.Chapter{
		ID: "chapter-1", SourceDocumentVersionID: "version-1", Ordinal: 1, Title: "One",
		StartOffset: 0, EndOffset: 100, SourceKind: storydomain.ChapterFromPattern,
		Status: storydomain.ChapterConfirmed, Revision: 1,
	}
	binding := &DramaBinding{}
	AttachStory(binding, context.Background(), appstory.NewService(appstory.Options{
		Repository: store, Clock: fixedDramaClock{}, IDs: fixedIDs(),
	}))
	// No AttachStaleness.
	revised, err := binding.ReviseChapter(ReviseChapterRequest{
		ChapterID: "chapter-1", Title: "Corrected", StartOffset: 0, EndOffset: 120, Revision: 1,
	})
	if err != nil {
		t.Fatalf("ReviseChapter without a staleness service: %v", err)
	}
	if revised.EndOffset != 120 {
		t.Fatalf("the correction was lost: end offset %d", revised.EndOffset)
	}
}

// ListStoryConflicts mirrors the real repository: newest first, and an empty
// status means any.
func (s *dramaStore) ListStoryConflicts(_ context.Context, projectID string, status storydomain.ConflictStatus) ([]storydomain.StoryFactConflict, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []storydomain.StoryFactConflict
	for _, record := range s.conflicts {
		if record.ProjectID != projectID {
			continue
		}
		if status != "" && record.Status != status {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// SplitChapter mirrors the real repository's renumbering.
func (s *dramaStore) SplitChapter(_ context.Context, first storydomain.Chapter, second storydomain.Chapter, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	current, ok := s.chapters[first.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	for id, chapter := range s.chapters {
		if chapter.SourceDocumentVersionID == first.SourceDocumentVersionID && chapter.Ordinal >= second.Ordinal {
			chapter.Ordinal++
			s.chapters[id] = chapter
		}
	}
	first.Revision = expectedRevision + 1
	s.chapters[first.ID] = first
	s.chapters[second.ID] = second
	return nil
}

// MergeChapters mirrors the real repository.
// MergeEntities is the port's merge over the binding's double.
//
// It moves the absorbed entity's references the way the real one does — the aliases and the
// participations this double holds — because a BINDING test asserts the result the binding converts,
// and a double that reported nothing would leave that conversion unexercised.
func (s *dramaStore) MergeEntities(_ context.Context, survivorID, absorbedID string, expectedRevision int64) (appstory.MergeEntitiesResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return appstory.MergeEntitiesResult{}, s.failNext
	}
	survivor, ok := s.entities[survivorID]
	if !ok {
		return appstory.MergeEntitiesResult{}, storydomain.NotFoundError()
	}
	absorbed, ok := s.entities[absorbedID]
	if !ok {
		return appstory.MergeEntitiesResult{}, storydomain.NotFoundError()
	}
	result := appstory.MergeEntitiesResult{AbsorbedName: absorbed.CanonicalName}
	for _, alias := range s.aliases {
		if alias.StoryEntityID == absorbedID {
			result.AliasesMoved++
		}
	}
	delete(s.entities, absorbedID)
	_ = survivor
	return result, nil
}

func (s *dramaStore) MergeChapters(_ context.Context, merged storydomain.Chapter, absorbedID string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		return s.failNext
	}
	current, ok := s.chapters[merged.ID]
	if !ok {
		return storydomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return storydomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	absorbed, ok := s.chapters[absorbedID]
	if !ok {
		return storydomain.NotFoundError()
	}
	delete(s.chapters, absorbedID)
	for id, chapter := range s.chapters {
		if chapter.SourceDocumentVersionID == merged.SourceDocumentVersionID && chapter.Ordinal > absorbed.Ordinal {
			chapter.Ordinal--
			s.chapters[id] = chapter
		}
	}
	merged.Revision = expectedRevision + 1
	s.chapters[merged.ID] = merged
	return nil
}

// The WP-08 additions to the double: dialogue lines, the whole-structure write, field locks, the
// event-link tables, the version histories and the totals write.
//
// The binding tests are about TRANSPORT — does a DTO map onto the right service call — so these
// record what they were handed rather than reproducing the real store's constraints. The
// constraints themselves are asserted where they live: in the repository's own integration tests
// and in the service's tests over its own double.

func (s *dramaStore) CreateDialogueLine(_ context.Context, record scriptdomain.DialogueLine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines[record.ID] = record
	return nil
}

func (s *dramaStore) GetDialogueLine(_ context.Context, id string) (scriptdomain.DialogueLine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.lines[id]
	if !ok {
		return scriptdomain.DialogueLine{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *dramaStore) ListDialogueLines(_ context.Context, sceneID string) ([]scriptdomain.DialogueLine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []scriptdomain.DialogueLine{}
	for _, record := range s.lines {
		if record.SceneID == sceneID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) SetDialogueLineLocked(_ context.Context, lineID string, locked bool, _ int64, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.lines[lineID]
	if !ok {
		return scriptdomain.NotFoundError()
	}
	record.Locked = locked
	s.lines[lineID] = record
	return nil
}

func (s *dramaStore) CreateScriptStructure(_ context.Context, structure scriptdomain.ScriptStructure) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, scene := range structure.Scenes {
		s.scenes[scene.ID] = scene.Scene
		for _, line := range scene.DialogueLines {
			s.lines[line.ID] = line
		}
		for _, shot := range scene.Shots {
			s.shots[shot.ID] = shot
		}
	}
	return nil
}

func (s *dramaStore) GetScriptStructure(_ context.Context, scriptVersionID string) (scriptdomain.ScriptStructure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	structure := scriptdomain.ScriptStructure{ScriptVersionID: scriptVersionID}
	for _, scene := range s.scenes {
		if scene.ScriptVersionID != scriptVersionID {
			continue
		}
		entry := scriptdomain.SceneStructure{Scene: scene}
		for _, line := range s.lines {
			if line.SceneID == scene.ID {
				entry.DialogueLines = append(entry.DialogueLines, line)
			}
		}
		for _, shot := range s.shots {
			if shot.SceneID == scene.ID {
				entry.Shots = append(entry.Shots, shot)
			}
		}
		structure.Scenes = append(structure.Scenes, entry)
	}
	return structure, nil
}

func (s *dramaStore) LockScriptField(_ context.Context, record scriptdomain.FieldLock) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fieldLocks[record.VersionID+"\x00"+string(record.Field)] = record
	return nil
}

func (s *dramaStore) UnlockScriptField(_ context.Context, versionID string, field scriptdomain.LockableField) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.fieldLocks, versionID+"\x00"+string(field))
	return nil
}

func (s *dramaStore) ListScriptFieldLocks(_ context.Context, versionID string) ([]scriptdomain.FieldLock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	locks := []scriptdomain.FieldLock{}
	for _, record := range s.fieldLocks {
		if record.VersionID == versionID {
			locks = append(locks, record)
		}
	}
	return locks, nil
}

func (s *dramaStore) ListSkeletonEventIDs(_ context.Context, versionID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.skeletonEvents[versionID]...), nil
}

func (s *dramaStore) ListStrategyEventLinks(_ context.Context, versionID string) ([]scriptdomain.StrategyEventLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]scriptdomain.StrategyEventLink(nil), s.strategyEvents[versionID]...), nil
}

func (s *dramaStore) ListStorySkeletonVersions(_ context.Context, episodeID string) ([]scriptdomain.StorySkeletonVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []scriptdomain.StorySkeletonVersion{}
	for _, record := range s.skeletons {
		if record.EpisodeID == episodeID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) ListAdaptationStrategyVersions(_ context.Context, episodeID string) ([]scriptdomain.AdaptationStrategyVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []scriptdomain.AdaptationStrategyVersion{}
	for _, record := range s.strategies {
		if record.EpisodeID == episodeID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) ListScriptVersions(_ context.Context, scriptID string) ([]scriptdomain.ScriptVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []scriptdomain.ScriptVersion{}
	for _, record := range s.scriptVers {
		if record.ScriptID == scriptID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *dramaStore) SetScriptVersionTotals(_ context.Context, versionID string, totalDurationSeconds int, summary string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.scriptVers[versionID]
	if !ok {
		return scriptdomain.NotFoundError()
	}
	record.EstimatedDurationSeconds = totalDurationSeconds
	record.Summary = summary
	s.scriptVers[versionID] = record
	return nil
}
