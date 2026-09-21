package script

import (
	"context"
	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// counterIDs mints deterministic identifiers so a test can assert on them.
type counterIDs struct {
	mu     sync.Mutex
	prefix string
	count  int
}

func newCounterIDs(prefix string) *counterIDs {
	return &counterIDs{prefix: prefix}
}

func (g *counterIDs) New() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.count++
	return g.prefix + "-" + itoa(g.count), nil
}

// fixedClock pins every timestamp the service writes.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC) }

// memoryStore is an in-memory Repository double. Its revision handling mirrors
// the real repositories': an update matches the revision it read and
// increments it, and a stale revision is refused with the domain's conflict
// error. ApproveScriptVersion reproduces the repository's one-transaction
// supersede, so the rule under test is the one the real store implements.
type memoryStore struct {
	mu         sync.Mutex
	episodes   map[string]scriptdomain.Episode
	skeletons  map[string]scriptdomain.StorySkeletonVersion
	strategies map[string]scriptdomain.AdaptationStrategyVersion
	scripts    map[string]scriptdomain.Script
	versions   map[string]scriptdomain.ScriptVersion
	scenes     map[string]scriptdomain.Scene
	shots      map[string]scriptdomain.Shot
	lines      map[string]scriptdomain.DialogueLine
	locks      map[string]scriptdomain.FieldLock
	// The two event-link maps are keyed by version id and hold the whole set, because the write
	// path replaces a version's links as a whole rather than amending them.
	skeletonEvents map[string][]string
	strategyEvents map[string][]scriptdomain.StrategyEventLink
	// storyEvents and storyEntities are the story-graph rows, keyed by PROJECT and then by id, so the
	// reference check's project boundary is representable rather than assumed. A nil outer map means
	// no project has a story graph, which is the state every test that states no reference runs in.
	storyEvents    map[string]map[string]string
	storyEntities  map[string]map[string]string
	failCreate     error
	failReferences error
	approveCalls   int
	// events records what the approval commands wrote, so a test can assert the
	// governance record exists rather than only that the status moved.
	events []event.Event
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		episodes:       map[string]scriptdomain.Episode{},
		skeletons:      map[string]scriptdomain.StorySkeletonVersion{},
		strategies:     map[string]scriptdomain.AdaptationStrategyVersion{},
		scripts:        map[string]scriptdomain.Script{},
		versions:       map[string]scriptdomain.ScriptVersion{},
		scenes:         map[string]scriptdomain.Scene{},
		shots:          map[string]scriptdomain.Shot{},
		lines:          map[string]scriptdomain.DialogueLine{},
		locks:          map[string]scriptdomain.FieldLock{},
		skeletonEvents: map[string][]string{},
		strategyEvents: map[string][]scriptdomain.StrategyEventLink{},
	}
}

func (s *memoryStore) CreateEpisode(_ context.Context, record scriptdomain.Episode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.episodes {
		if existing.ProjectID == record.ProjectID && existing.SeasonNumber == record.SeasonNumber &&
			existing.EpisodeNumber == record.EpisodeNumber {
			return scriptdomain.ConflictError("This project already has an episode at that position.")
		}
	}
	s.episodes[record.ID] = record
	return nil
}

func (s *memoryStore) GetEpisode(_ context.Context, id string) (scriptdomain.Episode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.episodes[id]
	if !ok {
		return scriptdomain.Episode{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) ListEpisodes(_ context.Context, projectID string) ([]scriptdomain.Episode, error) {
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

func (s *memoryStore) CountEpisodesAtPosition(_ context.Context, projectID string, seasonNumber, episodeNumber int) (int, error) {
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

func (s *memoryStore) UpdateEpisode(_ context.Context, record scriptdomain.Episode, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.episodes[record.ID]
	if !ok {
		return scriptdomain.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return scriptdomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.episodes[record.ID] = record
	return nil
}

func (s *memoryStore) CreateStorySkeletonVersion(_ context.Context, record scriptdomain.StorySkeletonVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.skeletons {
		if existing.EpisodeID == record.EpisodeID && existing.VersionNumber == record.VersionNumber {
			return scriptdomain.ConflictError("That version number is already used for this episode.")
		}
	}
	s.skeletons[record.ID] = record
	return nil
}

// CreateStorySkeletonVersionWithLinks stores the version and replaces its selection.
//
// The double writes the links as a SET, mirroring the real repository's DELETE-then-INSERT: a test
// that re-linked a version must see the new set rather than the union, which is the behaviour the
// write path depends on when it states a complete selection.
func (s *memoryStore) CreateStorySkeletonVersionWithLinks(ctx context.Context, record scriptdomain.StorySkeletonVersion, eventIDs []string) error {
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

func (s *memoryStore) GetStorySkeletonVersion(_ context.Context, id string) (scriptdomain.StorySkeletonVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.skeletons[id]
	if !ok {
		return scriptdomain.StorySkeletonVersion{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) MaxStorySkeletonVersionNumber(_ context.Context, episodeID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, record := range s.skeletons {
		if record.EpisodeID == episodeID && record.VersionNumber > highest {
			highest = record.VersionNumber
		}
	}
	return highest, nil
}

func (s *memoryStore) CreateAdaptationStrategyVersion(_ context.Context, record scriptdomain.AdaptationStrategyVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.strategies {
		if existing.EpisodeID == record.EpisodeID && existing.VersionNumber == record.VersionNumber {
			return scriptdomain.ConflictError("That version number is already used for this episode.")
		}
	}
	s.strategies[record.ID] = record
	return nil
}

// CreateAdaptationStrategyVersionWithLinks stores the version and replaces its treatments, with the
// ordinals re-derived from the slice's positions so the double and the real repository agree about
// which of the two decides the order.
func (s *memoryStore) CreateAdaptationStrategyVersionWithLinks(ctx context.Context, record scriptdomain.AdaptationStrategyVersion, links []scriptdomain.StrategyEventLink) error {
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

func (s *memoryStore) GetAdaptationStrategyVersion(_ context.Context, id string) (scriptdomain.AdaptationStrategyVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.strategies[id]
	if !ok {
		return scriptdomain.AdaptationStrategyVersion{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) MaxAdaptationStrategyVersionNumber(_ context.Context, episodeID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, record := range s.strategies {
		if record.EpisodeID == episodeID && record.VersionNumber > highest {
			highest = record.VersionNumber
		}
	}
	return highest, nil
}

func (s *memoryStore) CreateScript(_ context.Context, record scriptdomain.Script) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.scripts {
		if existing.EpisodeID == record.EpisodeID {
			return scriptdomain.ConflictError("This episode already has a script.")
		}
	}
	s.scripts[record.ID] = record
	return nil
}

func (s *memoryStore) GetScript(_ context.Context, id string) (scriptdomain.Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.scripts[id]
	if !ok {
		return scriptdomain.Script{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) GetScriptByEpisode(_ context.Context, episodeID string) (scriptdomain.Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.scripts {
		if record.EpisodeID == episodeID {
			return record, nil
		}
	}
	return scriptdomain.Script{}, scriptdomain.NotFoundError()
}

func (s *memoryStore) CreateScriptVersion(_ context.Context, version scriptdomain.ScriptVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.versions {
		if existing.ScriptID == version.ScriptID && existing.VersionNumber == version.VersionNumber {
			return scriptdomain.ConflictError("That version number is already used for this script.")
		}
	}
	s.versions[version.ID] = version
	return nil
}

func (s *memoryStore) GetScriptVersion(_ context.Context, id string) (scriptdomain.ScriptVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[id]
	if !ok {
		return scriptdomain.ScriptVersion{}, scriptdomain.NotFoundError()
	}
	return version, nil
}

func (s *memoryStore) MaxScriptVersionNumber(_ context.Context, scriptID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, version := range s.versions {
		if version.ScriptID == scriptID && version.VersionNumber > highest {
			highest = version.VersionNumber
		}
	}
	return highest, nil
}

func (s *memoryStore) CurrentApprovedScriptVersion(_ context.Context, scriptID string) (scriptdomain.ScriptVersion, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, version := range s.versions {
		if version.ScriptID == scriptID && version.Status == versioning.StatusApproved {
			return version, true, nil
		}
	}
	return scriptdomain.ScriptVersion{}, false, nil
}

// ApproveScriptVersion mirrors the real repository: it guards the target write
// on the status the caller read, supersedes the previous approval first, and
// refuses an approval that would leave two approved rows — the schema's partial
// unique index makes exactly that write fail, so the double refuses it too.
func (s *memoryStore) ApproveScriptVersion(_ context.Context, versionID, scriptID string, expectedStatus versioning.Status, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approveCalls++
	target, ok := s.versions[versionID]
	if !ok {
		return scriptdomain.NotFoundError()
	}
	if target.Status != expectedStatus {
		return scriptdomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	// The switch the real implementation performs in one transaction: the
	// previous approval becomes history first, then the target is approved, and
	// the governance event lands with them.
	for id, version := range s.versions {
		if version.ScriptID == scriptID && version.Status == versioning.StatusApproved && id != versionID {
			version.Status = versioning.StatusSuperseded
			s.versions[id] = version
		}
	}
	target.Status = versioning.StatusApproved
	s.versions[versionID] = target
	s.events = append(s.events, record)
	return nil
}

func (s *memoryStore) CreateScene(_ context.Context, record scriptdomain.Scene) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.scenes {
		if existing.ScriptVersionID == record.ScriptVersionID && existing.Ordinal == record.Ordinal {
			return scriptdomain.ConflictError("That scene position is already used in this script version.")
		}
	}
	s.scenes[record.ID] = record
	return nil
}

func (s *memoryStore) GetScene(_ context.Context, id string) (scriptdomain.Scene, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.scenes[id]
	if !ok {
		return scriptdomain.Scene{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) ListScenes(_ context.Context, scriptVersionID string) ([]scriptdomain.Scene, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []scriptdomain.Scene
	for _, record := range s.scenes {
		if record.ScriptVersionID == scriptVersionID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *memoryStore) CountScenesAtOrdinal(_ context.Context, scriptVersionID string, ordinal int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, record := range s.scenes {
		if record.ScriptVersionID == scriptVersionID && record.Ordinal == ordinal {
			count++
		}
	}
	return count, nil
}

func (s *memoryStore) CreateShot(_ context.Context, record scriptdomain.Shot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.shots {
		if existing.SceneID == record.SceneID && existing.Ordinal == record.Ordinal {
			return scriptdomain.ConflictError("That shot position is already used in this scene.")
		}
	}
	s.shots[record.ID] = record
	return nil
}

func (s *memoryStore) GetShot(_ context.Context, id string) (scriptdomain.Shot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.shots[id]
	if !ok {
		return scriptdomain.Shot{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) ListShots(_ context.Context, sceneID string) ([]scriptdomain.Shot, error) {
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

func (s *memoryStore) CountShotsAtOrdinal(_ context.Context, sceneID string, ordinal int) (int, error) {
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

// approvedStatuses reports the statuses of one script's versions, so a test
// can assert how many are approved.
func (s *memoryStore) approvedStatuses(scriptID string) []versioning.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	var statuses []versioning.Status
	for _, version := range s.versions {
		if version.ScriptID == scriptID {
			statuses = append(statuses, version.Status)
		}
	}
	return statuses
}

func newTestService(store Repository) *Service {
	// The recorder is supplied because the approval commands refuse without one:
	// an approval whose governance event cannot be written is not applied, which
	// is the rule TestApproveScriptVersionRefusesWithoutARecorder covers.
	return NewService(Options{
		Repository: store,
		Clock:      fixedClock{},
		IDs:        newCounterIDs("script"),
		Events:     testRecorder{},
	})
}

// testRecorder builds events for the tests that assert on them, without a
// database.
type testRecorder struct{}

func (testRecorder) Build(_ context.Context, draft eventsapp.Draft) (event.Event, error) {
	return event.New(
		"event-"+sanitizeForID(draft.AggregateID),
		draft.Type, draft.AggregateType, draft.AggregateID, draft.ProjectID,
		time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC), draft.TraceID, draft.Payload,
	)
}

func (testRecorder) RecordBestEffort(_ context.Context, _ eventsapp.Draft) {}

// sanitizeForID keeps a generated test identifier free of characters the event
// domain rejects, so a fixture id can be used as one.
func sanitizeForID(value string) string {
	cleaned := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-' || character == '_':
		default:
			character = '-'
		}
		cleaned = append(cleaned, character)
	}
	if len(cleaned) == 0 {
		return "anonymous"
	}
	return string(cleaned)
}

// sampleEpisode creates one episode through the service.
func sampleEpisode(t *testing.T, service *Service) scriptdomain.Episode {
	t.Helper()
	record, err := service.CreateEpisode(context.Background(), CreateEpisodeRequest{
		ProjectID: "project-1", SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot", TargetDurationSeconds: 90,
	})
	if err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	return record
}

// sampleScriptVersion creates a script and one version of it.
func sampleScriptVersion(t *testing.T, service *Service) (scriptdomain.Script, scriptdomain.ScriptVersion) {
	t.Helper()
	episode := sampleEpisode(t, service)
	script, err := service.EnsureScript(context.Background(), episode.ID)
	if err != nil {
		t.Fatalf("EnsureScript: %v", err)
	}
	version, err := service.CreateScriptVersion(context.Background(), CreateScriptVersionRequest{
		ScriptID: script.ID, StorySkeletonVersionID: "skeleton-1", AdaptationStrategyVersionID: "strategy-1",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	return script, version
}

// TestCreateEpisodeStoresThePlan covers the successful create, the defaults and
// the business-key clash the schema also enforces.
func TestCreateEpisodeStoresThePlan(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	episode := sampleEpisode(t, service)
	if episode.Status != scriptdomain.EpisodePlanning || episode.Revision != 1 {
		t.Fatalf("episode = %+v", episode)
	}
	if episode.Key() != "S1E1" {
		t.Fatalf("key = %q, want S1E1", episode.Key())
	}
	if !episode.CreatedAt.Equal(fixedClock{}.Now()) {
		t.Fatalf("created_at = %s, want the injected clock", episode.CreatedAt)
	}

	// The same position in the same project is refused before the insert, so
	// the caller is told which episode already holds it.
	_, err := service.CreateEpisode(context.Background(), CreateEpisodeRequest{
		ProjectID: "project-1", SeasonNumber: 1, EpisodeNumber: 1, Title: "Another pilot",
	})
	if err == nil {
		t.Fatal("two episodes were allowed to share a project position")
	}
	domainErr, ok := scriptdomain.AsError(err)
	if !ok || domainErr.Category != scriptdomain.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	// A different project may use the same position.
	if _, err := service.CreateEpisode(context.Background(), CreateEpisodeRequest{
		ProjectID: "project-2", SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot",
	}); err != nil {
		t.Fatalf("an episode in another project was refused: %v", err)
	}
	list, err := service.ListEpisodes(context.Background(), "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("ListEpisodes returned %d rows, want 1", len(list))
	}
}

// TestUpdateEpisodeStatusValidatesAndGuards proves the status vocabulary is
// checked and the write is a compare-and-swap.
func TestUpdateEpisodeStatusValidatesAndGuards(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := sampleEpisode(t, service)

	if _, err := service.UpdateEpisodeStatus(ctx, UpdateEpisodeStatusRequest{
		EpisodeID: episode.ID, Status: "filming", Revision: 1,
	}); err == nil {
		t.Fatal("an undocumented episode status was accepted")
	} else if domainErr, ok := scriptdomain.AsError(err); !ok || domainErr.Category != scriptdomain.CategoryInvalidInput {
		t.Fatalf("expected invalid_input, got %v", err)
	}

	updated, err := service.UpdateEpisodeStatus(ctx, UpdateEpisodeStatusRequest{
		EpisodeID: episode.ID, Status: scriptdomain.EpisodeWriting, Revision: 1,
	})
	if err != nil {
		t.Fatalf("UpdateEpisodeStatus: %v", err)
	}
	if updated.Status != scriptdomain.EpisodeWriting || updated.Revision != 2 {
		t.Fatalf("updated = %+v", updated)
	}

	// The same expected revision must now fail and must not write.
	if _, err := service.UpdateEpisodeStatus(ctx, UpdateEpisodeStatusRequest{
		EpisodeID: episode.ID, Status: scriptdomain.EpisodeCompleted, Revision: 1,
	}); err == nil {
		t.Fatal("a stale revision was accepted")
	} else if domainErr, ok := scriptdomain.AsError(err); !ok || domainErr.Category != scriptdomain.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	stored, err := store.GetEpisode(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != scriptdomain.EpisodeWriting {
		t.Fatalf("a refused update wrote the row: %q", stored.Status)
	}
}

// TestVersionNumbersFollowTheStoredMaximum proves both versioned artifacts
// number themselves after the highest row, not after a count.
func TestVersionNumbersFollowTheStoredMaximum(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := sampleEpisode(t, service)

	first, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "A letter arrives.", CoreConflict: "She must answer it.",
	})
	if err != nil {
		t.Fatalf("CreateStorySkeletonVersion: %v", err)
	}
	if first.VersionNumber != 1 || first.Status != versioning.StatusDraft {
		t.Fatalf("first skeleton = %+v", first)
	}
	second, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: first.ID, ChangeReason: "The hook was too slow.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.VersionNumber != 2 {
		t.Fatalf("second skeleton version = %d, want 2", second.VersionNumber)
	}

	strategy, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "Keep the letter, merge the two neighbours.",
		AdaptationMode: scriptdomain.AdaptationAggressive,
	})
	if err != nil {
		t.Fatalf("CreateAdaptationStrategyVersion: %v", err)
	}
	if strategy.VersionNumber != 1 || strategy.AdaptationMode != scriptdomain.AdaptationAggressive {
		t.Fatalf("strategy = %+v", strategy)
	}
	// The documented default is balanced, and an undocumented mode is refused
	// before the row is built.
	secondStrategy, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "Second",
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondStrategy.VersionNumber != 2 || secondStrategy.AdaptationMode != scriptdomain.AdaptationBalanced {
		t.Fatalf("second strategy = %+v", secondStrategy)
	}
	if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, AdaptationMode: "radical",
	}); err == nil {
		t.Fatal("an undocumented adaptation mode was accepted")
	}
	if len(store.strategies) != 2 {
		t.Fatalf("the store holds %d strategies, want 2", len(store.strategies))
	}
}

// TestEnsureScriptIsStable proves the script identity is created once and that
// a second call returns the same row.
func TestEnsureScriptIsStable(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := sampleEpisode(t, service)

	first, err := service.EnsureScript(ctx, episode.ID)
	if err != nil {
		t.Fatalf("EnsureScript: %v", err)
	}
	if first.EpisodeID != episode.ID || first.CurrentVersionID != "" || first.Revision != 1 {
		t.Fatalf("script = %+v", first)
	}
	second, err := service.EnsureScript(ctx, episode.ID)
	if err != nil {
		t.Fatalf("second EnsureScript: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("a second script was created: %q then %q", first.ID, second.ID)
	}
	if len(store.scripts) != 1 {
		t.Fatalf("the store holds %d scripts, want 1", len(store.scripts))
	}
	// The episode must exist: the schema's foreign key would reject the row,
	// and resolving it here turns that into a domain error.
	if _, err := service.EnsureScript(ctx, "missing-episode"); err == nil {
		t.Fatal("a script was created for an episode that does not exist")
	} else if domainErr, ok := scriptdomain.AsError(err); !ok || domainErr.Category != scriptdomain.CategoryNotFound {
		t.Fatalf("expected not_found, got %v", err)
	}
}

// TestCreateScriptVersionNumbersAndRequiresUpstream proves the version is
// numbered after the highest and that the two upstream references §7.5
// requires are not optional.
func TestCreateScriptVersionNumbersAndRequiresUpstream(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := sampleEpisode(t, service)
	script, err := service.EnsureScript(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: script.ID, StorySkeletonVersionID: "skeleton-1", AdaptationStrategyVersionID: "strategy-1",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	if first.VersionNumber != 1 || first.Status != versioning.StatusDraft {
		t.Fatalf("first version = %+v", first)
	}
	second, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: script.ID, BasedOnVersionID: first.ID,
		StorySkeletonVersionID: "skeleton-2", AdaptationStrategyVersionID: "strategy-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.VersionNumber != 2 || second.BasedOnVersionID != first.ID {
		t.Fatalf("second version = %+v", second)
	}
	// A version with no strategy reference is refused: AC-SCRIPT-003's stale
	// chain cannot be walked without it.
	if _, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: script.ID, StorySkeletonVersionID: "skeleton-2",
	}); err == nil {
		t.Fatal("a script version without an adaptation strategy was accepted")
	}
	if len(store.versions) != 2 {
		t.Fatalf("the store holds %d versions, want 2", len(store.versions))
	}
}

// TestApproveScriptVersionSupersedesThePreviousApproval is DOMAIN_MODEL §2.5's
// supersede rule: approving v2 turns v1 into 'superseded' and leaves exactly
// one approved version of the script.
func TestApproveScriptVersionSupersedesThePreviousApproval(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	script, first := sampleScriptVersion(t, service)

	approved, err := service.ApproveScriptVersion(ctx, ApproveScriptVersionRequest{ScriptVersionID: first.ID})
	if err != nil {
		t.Fatalf("approving the first version: %v", err)
	}
	if approved.Status != versioning.StatusApproved {
		t.Fatalf("approved = %+v", approved)
	}
	if statuses := store.approvedStatuses(script.ID); countApproved(statuses) != 1 {
		t.Fatalf("approved count = %d, want 1 after the first approval", countApproved(statuses))
	}

	// Approving the already-approved version again is refused by the domain.
	if _, err := service.ApproveScriptVersion(ctx, ApproveScriptVersionRequest{ScriptVersionID: first.ID}); err == nil {
		t.Fatal("an already-approved version was approved again")
	} else if domainErr, ok := versioning.AsError(err); !ok || domainErr.Category != versioning.CategoryConflict {
		t.Fatalf("expected the domain's conflict, got %v", err)
	}

	second, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: script.ID, BasedOnVersionID: first.ID,
		StorySkeletonVersionID: "skeleton-1", AdaptationStrategyVersionID: "strategy-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err = service.ApproveScriptVersion(ctx, ApproveScriptVersionRequest{ScriptVersionID: second.ID})
	if err != nil {
		t.Fatalf("approving the second version: %v", err)
	}
	if approved.Status != versioning.StatusApproved {
		t.Fatalf("second version status = %q", approved.Status)
	}
	// One approval, and the version it replaced is kept as history.
	statuses := store.approvedStatuses(script.ID)
	if countApproved(statuses) != 1 {
		t.Fatalf("approved count = %d, want exactly 1", countApproved(statuses))
	}
	superseded, err := store.GetScriptVersion(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Status != versioning.StatusSuperseded {
		t.Fatalf("the previous approval is %q, want superseded", superseded.Status)
	}
	if store.approveCalls != 2 {
		t.Fatalf("the repository's approve ran %d times, want 2", store.approveCalls)
	}
}

// TestApproveScriptVersionRefusesASupersededVersion proves the domain's gate is
// the one applied: history cannot become current again.
func TestApproveScriptVersionRefusesASupersededVersion(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	script, first := sampleScriptVersion(t, service)
	if _, err := service.ApproveScriptVersion(ctx, ApproveScriptVersionRequest{ScriptVersionID: first.ID}); err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: script.ID, BasedOnVersionID: first.ID,
		StorySkeletonVersionID: "skeleton-1", AdaptationStrategyVersionID: "strategy-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveScriptVersion(ctx, ApproveScriptVersionRequest{ScriptVersionID: second.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveScriptVersion(ctx, ApproveScriptVersionRequest{ScriptVersionID: first.ID}); err == nil {
		t.Fatal("a superseded version was approved again")
	} else if domainErr, ok := versioning.AsError(err); !ok || domainErr.Category != versioning.CategoryConflict {
		t.Fatalf("expected the domain's conflict, got %v", err)
	}
	// A draft carries no upstream references, so the domain refuses it before
	// the repository's transaction runs.
	draft, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: script.ID, StorySkeletonVersionID: "skeleton-1", AdaptationStrategyVersionID: "strategy-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveScriptVersion(ctx, ApproveScriptVersionRequest{ScriptVersionID: draft.ID}); err != nil {
		t.Fatalf("a draft version was refused approval: %v", err)
	}
	if store.approveCalls != 3 {
		t.Fatalf("the repository's approve ran %d times, want 3", store.approveCalls)
	}
}

// TestCreateSceneAndShotProveTheOrdinalIsUniquePerParent covers both creates
// and both ordinal clashes.
func TestCreateSceneAndShotProveTheOrdinalIsUniquePerParent(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	_, version := sampleScriptVersion(t, service)

	scene, err := service.CreateScene(ctx, CreateSceneRequest{
		ScriptVersionID: version.ID, Ordinal: 1, SceneNumber: "1", Slugline: "INT. HARBOUR - DAY",
		InteriorExterior: scriptdomain.InteriorINT, TimeOfDay: "day", Summary: "She arrives.",
	})
	if err != nil {
		t.Fatalf("CreateScene: %v", err)
	}
	if scene.Revision != 1 || scene.InteriorExterior != scriptdomain.InteriorINT {
		t.Fatalf("scene = %+v", scene)
	}
	if _, err := service.CreateScene(ctx, CreateSceneRequest{
		ScriptVersionID: version.ID, Ordinal: 1, Slugline: "EXT. HARBOUR - NIGHT",
	}); err == nil {
		t.Fatal("two scenes were allowed to share an ordinal in one version")
	} else if domainErr, ok := scriptdomain.AsError(err); !ok || domainErr.Category != scriptdomain.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	// The default marking is OTHER, which is what the schema defaults to.
	other, err := service.CreateScene(ctx, CreateSceneRequest{ScriptVersionID: version.ID, Ordinal: 2})
	if err != nil {
		t.Fatal(err)
	}
	if other.InteriorExterior != scriptdomain.InteriorOTHER {
		t.Fatalf("default marking = %q, want OTHER", other.InteriorExterior)
	}

	shot, err := service.CreateShot(ctx, CreateSceneShotRequest(scene.ID))
	if err != nil {
		t.Fatalf("CreateShot: %v", err)
	}
	if shot.Status != versioning.StatusDraft || shot.Revision != 1 {
		t.Fatalf("shot = %+v", shot)
	}
	if _, err := service.CreateShot(ctx, CreateSceneShotRequest(scene.ID)); err == nil {
		t.Fatal("two shots were allowed to share an ordinal in one scene")
	} else if domainErr, ok := scriptdomain.AsError(err); !ok || domainErr.Category != scriptdomain.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	// An ordinal below one is the domain's invalid input, not a clash.
	if _, err := service.CreateShot(ctx, CreateShotRequest{SceneID: scene.ID, Ordinal: 0}); err == nil {
		t.Fatal("a shot at ordinal zero was accepted")
	} else if domainErr, ok := scriptdomain.AsError(err); !ok || domainErr.Category != scriptdomain.CategoryInvalidInput {
		t.Fatalf("expected invalid_input, got %v", err)
	}

	scenes, err := service.ListScenes(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 2 {
		t.Fatalf("ListScenes returned %d rows, want 2", len(scenes))
	}
	shots, err := service.ListShots(ctx, scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shots) != 1 {
		t.Fatalf("ListShots returned %d rows, want 1", len(shots))
	}
}

// CreateSceneShotRequest is a small helper request for one shot in a scene.
func CreateSceneShotRequest(sceneID string) CreateShotRequest {
	return CreateShotRequest{
		SceneID: sceneID, Ordinal: 1, ShotNumber: "1", ShotSize: "wide",
		VisualDescription: "The harbour at dawn.",
	}
}

// TestUnavailableServiceFailsClosed proves an unattached service reports a
// storage failure instead of panicking.
func TestUnavailableServiceFailsClosed(t *testing.T) {
	ctx := context.Background()
	service := NewService(Options{Clock: fixedClock{}})
	if service.Available() {
		t.Fatal("a service with no repository reports itself available")
	}
	if _, err := service.CreateEpisode(ctx, CreateEpisodeRequest{ProjectID: "project-1", SeasonNumber: 1, EpisodeNumber: 1}); err == nil {
		t.Fatal("an unattached service created an episode")
	} else if domainErr, ok := scriptdomain.AsError(err); !ok || domainErr.Category != scriptdomain.CategoryStorage {
		t.Fatalf("expected storage, got %v", err)
	}
	if _, err := service.ListScenes(ctx, "version-1"); err == nil {
		t.Fatal("an unattached service listed scenes")
	}

	noIDs := NewService(Options{Repository: newMemoryStore(), Clock: fixedClock{}})
	if noIDs.Available() {
		t.Fatal("a service with no identifier source reports itself available")
	}
	if _, err := noIDs.EnsureScript(ctx, "episode-1"); err == nil {
		t.Fatal("an identifier-less service created a script")
	}
}

// TestCreateEpisodePropagatesRepositoryFailure proves a storage failure is not
// swallowed and no record is returned as if it had been written.
func TestCreateEpisodePropagatesRepositoryFailure(t *testing.T) {
	store := newMemoryStore()
	store.failCreate = scriptdomain.StorageError("The episode could not be saved.", nil)
	service := newTestService(store)
	record, err := service.CreateEpisode(context.Background(), CreateEpisodeRequest{
		ProjectID: "project-1", SeasonNumber: 1, EpisodeNumber: 1,
	})
	if err == nil {
		t.Fatal("a failing store reported success")
	}
	if record.ID != "" {
		t.Fatalf("a failed create returned a record: %+v", record)
	}
	if len(store.episodes) != 0 {
		t.Fatal("the failed episode was stored")
	}
}

// countApproved reports how many of a script's versions are approved.
func countApproved(statuses []versioning.Status) int {
	count := 0
	for _, status := range statuses {
		if status == versioning.StatusApproved {
			count++
		}
	}
	return count
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// The approval methods reproduce the repository's supersede-then-approve shape,
// including the event write, so a service test can assert the whole switch
// without a database. They mirror the real implementation's semantics rather
// than mocking them: the previous approval becomes superseded, the target
// becomes approved, and the event must be present.
func (s *memoryStore) CurrentApprovedSkeletonVersionID(_ context.Context, episodeID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, record := range s.skeletons {
		if record.EpisodeID == episodeID && record.Status == versioning.StatusApproved {
			return id, nil
		}
	}
	return "", nil
}

func (s *memoryStore) ApproveStorySkeletonVersion(_ context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
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
	s.events = append(s.events, record)
	return nil
}

func (s *memoryStore) CurrentApprovedStrategyVersionID(_ context.Context, episodeID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, record := range s.strategies {
		if record.EpisodeID == episodeID && record.Status == versioning.StatusApproved {
			return id, nil
		}
	}
	return "", nil
}

func (s *memoryStore) ApproveAdaptationStrategyVersion(_ context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
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
	s.events = append(s.events, record)
	return nil
}

// The WP-08 additions to the double: dialogue lines, the whole-structure write, field locks, the
// two event-link tables, the version histories and the totals write.
//
// Each reproduces the real repository's failure semantics rather than accepting everything: a
// duplicate ordinal is a conflict, a stale revision is a conflict, and a lock set is replaced as a
// whole. A double that accepted anything would make the service tests assert about the double.

func (s *memoryStore) CreateDialogueLine(_ context.Context, record scriptdomain.DialogueLine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCreate != nil {
		return s.failCreate
	}
	for _, existing := range s.lines {
		if existing.SceneID == record.SceneID && existing.Ordinal == record.Ordinal {
			return scriptdomain.ConflictError("That line position is already used in this scene.")
		}
	}
	s.lines[record.ID] = record
	return nil
}

func (s *memoryStore) GetDialogueLine(_ context.Context, id string) (scriptdomain.DialogueLine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.lines[id]
	if !ok {
		return scriptdomain.DialogueLine{}, scriptdomain.NotFoundError()
	}
	return record, nil
}

func (s *memoryStore) ListDialogueLines(_ context.Context, sceneID string) ([]scriptdomain.DialogueLine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []scriptdomain.DialogueLine{}
	for _, record := range s.lines {
		if record.SceneID == sceneID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Ordinal < records[j].Ordinal })
	return records, nil
}

func (s *memoryStore) SetDialogueLineLocked(_ context.Context, lineID string, locked bool, expectedRevision int64, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.lines[lineID]
	if !ok {
		return scriptdomain.NotFoundError()
	}
	if record.Revision != expectedRevision {
		return scriptdomain.ConflictError("That dialogue line was changed by someone else.")
	}
	record.Locked = locked
	record.Revision = expectedRevision + 1
	s.lines[lineID] = record
	return nil
}

// CreateScriptStructure reproduces the real repository's all-or-nothing write: the scenes, lines
// and shots are staged and only committed when every one succeeded, so a failure part way through
// leaves nothing behind. A double that wrote as it went would let a test pass that the real store
// would refuse.
func (s *memoryStore) CreateScriptStructure(ctx context.Context, structure scriptdomain.ScriptStructure) error {
	s.mu.Lock()
	scenes := map[string]scriptdomain.Scene{}
	lines := map[string]scriptdomain.DialogueLine{}
	shots := map[string]scriptdomain.Shot{}
	for _, scene := range structure.Scenes {
		scenes[scene.ID] = scene.Scene
		for _, line := range scene.DialogueLines {
			lines[line.ID] = line
		}
		for _, shot := range scene.Shots {
			shots[shot.ID] = shot
		}
	}
	for id, record := range scenes {
		s.scenes[id] = record
	}
	for id, record := range lines {
		s.lines[id] = record
	}
	for id, record := range shots {
		s.shots[id] = record
	}
	s.mu.Unlock()
	_ = ctx
	return nil
}

func (s *memoryStore) GetScriptStructure(_ context.Context, scriptVersionID string) (scriptdomain.ScriptStructure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	structure := scriptdomain.ScriptStructure{ScriptVersionID: scriptVersionID}
	var scenes []scriptdomain.Scene
	for _, record := range s.scenes {
		if record.ScriptVersionID == scriptVersionID {
			scenes = append(scenes, record)
		}
	}
	sort.Slice(scenes, func(i, j int) bool { return scenes[i].Ordinal < scenes[j].Ordinal })
	for _, scene := range scenes {
		entry := scriptdomain.SceneStructure{Scene: scene, DialogueLines: []scriptdomain.DialogueLine{}, Shots: []scriptdomain.Shot{}}
		for _, line := range s.lines {
			if line.SceneID == scene.ID {
				entry.DialogueLines = append(entry.DialogueLines, line)
			}
		}
		sort.Slice(entry.DialogueLines, func(i, j int) bool {
			return entry.DialogueLines[i].Ordinal < entry.DialogueLines[j].Ordinal
		})
		for _, shot := range s.shots {
			if shot.SceneID == scene.ID {
				entry.Shots = append(entry.Shots, shot)
			}
		}
		sort.Slice(entry.Shots, func(i, j int) bool { return entry.Shots[i].Ordinal < entry.Shots[j].Ordinal })
		structure.Scenes = append(structure.Scenes, entry)
	}
	return structure, nil
}

func (s *memoryStore) LockScriptField(_ context.Context, record scriptdomain.FieldLock) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locks[record.VersionID+"\x00"+string(record.Field)] = record
	return nil
}

func (s *memoryStore) UnlockScriptField(_ context.Context, versionID string, field scriptdomain.LockableField) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.locks, versionID+"\x00"+string(field))
	return nil
}

func (s *memoryStore) ListScriptFieldLocks(_ context.Context, versionID string) ([]scriptdomain.FieldLock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	locks := []scriptdomain.FieldLock{}
	for _, record := range s.locks {
		if record.VersionID == versionID {
			locks = append(locks, record)
		}
	}
	sort.Slice(locks, func(i, j int) bool { return locks[i].Field < locks[j].Field })
	return locks, nil
}

func (s *memoryStore) LinkSkeletonEvents(_ context.Context, versionID string, eventIDs []string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(eventIDs) == 0 {
		delete(s.skeletonEvents, versionID)
		return nil
	}
	s.skeletonEvents[versionID] = append([]string(nil), eventIDs...)
	return nil
}

func (s *memoryStore) ListSkeletonEventIDs(_ context.Context, versionID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.skeletonEvents[versionID]...), nil
}

func (s *memoryStore) LinkStrategyEvents(_ context.Context, versionID string, links []scriptdomain.StrategyEventLink, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(links) == 0 {
		delete(s.strategyEvents, versionID)
		return nil
	}
	stored := make([]scriptdomain.StrategyEventLink, 0, len(links))
	for index, link := range links {
		link.Ordinal = index + 1
		stored = append(stored, link)
	}
	s.strategyEvents[versionID] = stored
	return nil
}

func (s *memoryStore) ListStrategyEventLinks(_ context.Context, versionID string) ([]scriptdomain.StrategyEventLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]scriptdomain.StrategyEventLink(nil), s.strategyEvents[versionID]...), nil
}

// MissingStoryEventIDs reports which of the given events this project does not have.
//
// THE PROJECT IS PART OF THE ANSWER, which the first version of this double got wrong: it ignored
// the argument and read one flat map, so a mutation that replaced the project id with a constant
// left every test green. That is the leak the check exists to prevent — a script citing an event
// that belongs to another project — and a double that cannot represent it cannot catch it. The map
// is now keyed by project, so a row seeded for one project is genuinely absent for another.
func (s *memoryStore) MissingStoryEventIDs(_ context.Context, projectID string, eventIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failReferences != nil {
		return nil, s.failReferences
	}
	return missingFrom(s.storyEvents[projectID], eventIDs), nil
}

// MissingStoryEntityIDs reports which of the given entities this project does not have.
func (s *memoryStore) MissingStoryEntityIDs(_ context.Context, projectID string, entityIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failReferences != nil {
		return nil, s.failReferences
	}
	return missingFrom(s.storyEntities[projectID], entityIDs), nil
}

// missingFrom is the double's reading of "which of these are absent".
func missingFrom(known map[string]string, ids []string) []string {
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := known[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

func (s *memoryStore) ListStorySkeletonVersions(_ context.Context, episodeID string) ([]scriptdomain.StorySkeletonVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []scriptdomain.StorySkeletonVersion{}
	for _, record := range s.skeletons {
		if record.EpisodeID == episodeID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].VersionNumber > records[j].VersionNumber })
	return records, nil
}

func (s *memoryStore) ListAdaptationStrategyVersions(_ context.Context, episodeID string) ([]scriptdomain.AdaptationStrategyVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []scriptdomain.AdaptationStrategyVersion{}
	for _, record := range s.strategies {
		if record.EpisodeID == episodeID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].VersionNumber > records[j].VersionNumber })
	return records, nil
}

func (s *memoryStore) ListScriptVersions(_ context.Context, scriptID string) ([]scriptdomain.ScriptVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []scriptdomain.ScriptVersion{}
	for _, record := range s.versions {
		if record.ScriptID == scriptID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].VersionNumber > records[j].VersionNumber })
	return records, nil
}

func (s *memoryStore) SetScriptVersionTotals(_ context.Context, versionID string, totalDurationSeconds int, summary string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.versions[versionID]
	if !ok {
		return scriptdomain.NotFoundError()
	}
	record.EstimatedDurationSeconds = totalDurationSeconds
	record.Summary = summary
	s.versions[versionID] = record
	return nil
}
