package script

import (
	"context"
	"sync"
	"testing"
	"time"

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
	mu           sync.Mutex
	episodes     map[string]scriptdomain.Episode
	skeletons    map[string]scriptdomain.StorySkeletonVersion
	strategies   map[string]scriptdomain.AdaptationStrategyVersion
	scripts      map[string]scriptdomain.Script
	versions     map[string]scriptdomain.ScriptVersion
	scenes       map[string]scriptdomain.Scene
	shots        map[string]scriptdomain.Shot
	failCreate   error
	approveCalls int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		episodes:   map[string]scriptdomain.Episode{},
		skeletons:  map[string]scriptdomain.StorySkeletonVersion{},
		strategies: map[string]scriptdomain.AdaptationStrategyVersion{},
		scripts:    map[string]scriptdomain.Script{},
		versions:   map[string]scriptdomain.ScriptVersion{},
		scenes:     map[string]scriptdomain.Scene{},
		shots:      map[string]scriptdomain.Shot{},
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
func (s *memoryStore) ApproveScriptVersion(_ context.Context, target scriptdomain.ScriptVersion, supersededVersionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approveCalls++
	current, ok := s.versions[target.ID]
	if !ok {
		return scriptdomain.NotFoundError()
	}
	if current.Status != target.Status {
		return scriptdomain.ConflictError("This item changed in another window. Reload it and try again.")
	}
	if supersededVersionID == "" {
		for _, version := range s.versions {
			if version.ScriptID == target.ScriptID && version.Status == versioning.StatusApproved {
				return scriptdomain.ConflictError("This script already has an approved version.")
			}
		}
	}
	// The two writes the real repository performs in one transaction: the
	// previous approval becomes history first, then the target is approved.
	if supersededVersionID != "" {
		previous, ok := s.versions[supersededVersionID]
		if !ok {
			return scriptdomain.NotFoundError()
		}
		previous.Status = versioning.StatusSuperseded
		s.versions[previous.ID] = previous
	}
	target.Status = versioning.StatusApproved
	s.versions[target.ID] = target
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
	return NewService(Options{Repository: store, Clock: fixedClock{}, IDs: newCounterIDs("script")})
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
