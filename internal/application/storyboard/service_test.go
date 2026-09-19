package storyboard

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// testClock is a deterministic clock.
type testClock struct {
	now time.Time
}

func (c testClock) Now() time.Time { return c.now }

// counterIDs mints deterministic identifiers so a test can assert on them.
type counterIDs struct {
	mu     sync.Mutex
	prefix string
	count  int
}

func (g *counterIDs) New() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.count++
	return g.prefix + "-" + itoa(g.count), nil
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

// memoryRepo is an in-memory double for the four storyboard repositories. It
// reproduces the revision-CAS semantics the real store enforces: an update
// whose expected revision does not match the stored row is refused and writes
// nothing, and the unique constraints the schema declares (one storyboard per
// episode, one item per shot and per ordinal in a version, one version number
// per parent) are enforced so the service's own checks are exercised against
// the same rules the database applies.
type memoryRepo struct {
	mu             sync.Mutex
	plans          map[string]storyboard.DirectorPlanVersion
	storyboards    map[string]storyboard.Storyboard
	versions       map[string]storyboard.StoryboardVersion
	items          map[string]storyboard.StoryboardItem
	panels         map[string]storyboard.StoryboardPanelVersion
	approvedPanels []string
	failNextWrite  error
	// events records what the approval commands wrote, so a test can assert the
	// governance record exists rather than only that the status moved.
	events []event.Event
	// episodeProjects maps an episode to its project, standing in for the join
	// the real repository does against the episodes table.
	episodeProjects map[string]string
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		plans:           map[string]storyboard.DirectorPlanVersion{},
		storyboards:     map[string]storyboard.Storyboard{},
		versions:        map[string]storyboard.StoryboardVersion{},
		items:           map[string]storyboard.StoryboardItem{},
		panels:          map[string]storyboard.StoryboardPanelVersion{},
		episodeProjects: map[string]string{},
	}
}

func (r *memoryRepo) CreateDirectorPlanVersion(_ context.Context, version storyboard.DirectorPlanVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNextWrite != nil {
		return r.failNextWrite
	}
	for _, existing := range r.plans {
		if existing.EpisodeID == version.EpisodeID && existing.VersionNumber == version.VersionNumber {
			return storyboard.ConflictError("That version number is already used for this episode.")
		}
	}
	r.plans[version.ID] = version
	return nil
}

func (r *memoryRepo) GetDirectorPlanVersion(_ context.Context, id string) (storyboard.DirectorPlanVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	version, ok := r.plans[id]
	if !ok {
		return storyboard.DirectorPlanVersion{}, storyboard.NotFoundError()
	}
	return version, nil
}

func (r *memoryRepo) MaxDirectorPlanVersionNumber(_ context.Context, episodeID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	highest := 0
	for _, version := range r.plans {
		if version.EpisodeID == episodeID && version.VersionNumber > highest {
			highest = version.VersionNumber
		}
	}
	return highest, nil
}

func (r *memoryRepo) GetStoryboardByEpisode(_ context.Context, episodeID string) (storyboard.Storyboard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.storyboards {
		if record.EpisodeID == episodeID {
			return record, nil
		}
	}
	return storyboard.Storyboard{}, storyboard.NotFoundError()
}

func (r *memoryRepo) GetStoryboard(_ context.Context, id string) (storyboard.Storyboard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.storyboards[id]
	if !ok {
		return storyboard.Storyboard{}, storyboard.NotFoundError()
	}
	return record, nil
}

func (r *memoryRepo) CreateStoryboard(_ context.Context, record storyboard.Storyboard) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNextWrite != nil {
		return r.failNextWrite
	}
	for _, existing := range r.storyboards {
		if existing.EpisodeID == record.EpisodeID {
			return storyboard.ConflictError("That episode already has a storyboard.")
		}
	}
	r.storyboards[record.ID] = record
	return nil
}

func (r *memoryRepo) GetStoryboardVersion(_ context.Context, id string) (storyboard.StoryboardVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	version, ok := r.versions[id]
	if !ok {
		return storyboard.StoryboardVersion{}, storyboard.NotFoundError()
	}
	return version, nil
}

func (r *memoryRepo) MaxStoryboardVersionNumber(_ context.Context, storyboardID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	highest := 0
	for _, version := range r.versions {
		if version.StoryboardID == storyboardID && version.VersionNumber > highest {
			highest = version.VersionNumber
		}
	}
	return highest, nil
}

func (r *memoryRepo) CreateStoryboardVersion(_ context.Context, version storyboard.StoryboardVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNextWrite != nil {
		return r.failNextWrite
	}
	for _, existing := range r.versions {
		if existing.StoryboardID == version.StoryboardID && existing.VersionNumber == version.VersionNumber {
			return storyboard.ConflictError("That version number is already used for this storyboard.")
		}
	}
	r.versions[version.ID] = version
	return nil
}

func (r *memoryRepo) CreateStoryboardItem(_ context.Context, item storyboard.StoryboardItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNextWrite != nil {
		return r.failNextWrite
	}
	for _, existing := range r.items {
		if existing.StoryboardVersionID != item.StoryboardVersionID {
			continue
		}
		if existing.ShotID == item.ShotID || existing.Ordinal == item.Ordinal {
			return storyboard.ConflictError("That row already exists in this storyboard version.")
		}
	}
	r.items[item.ID] = item
	return nil
}

func (r *memoryRepo) GetStoryboardItem(_ context.Context, id string) (storyboard.StoryboardItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[id]
	if !ok {
		return storyboard.StoryboardItem{}, storyboard.NotFoundError()
	}
	return item, nil
}

func (r *memoryRepo) FindStoryboardItemByShot(_ context.Context, storyboardVersionID, shotID string) (storyboard.StoryboardItem, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		if item.StoryboardVersionID == storyboardVersionID && item.ShotID == shotID {
			return item, true, nil
		}
	}
	return storyboard.StoryboardItem{}, false, nil
}

func (r *memoryRepo) FindStoryboardItemByOrdinal(_ context.Context, storyboardVersionID string, ordinal int) (storyboard.StoryboardItem, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		if item.StoryboardVersionID == storyboardVersionID && item.Ordinal == ordinal {
			return item, true, nil
		}
	}
	return storyboard.StoryboardItem{}, false, nil
}

func (r *memoryRepo) ListStoryboardItems(_ context.Context, storyboardVersionID string) ([]storyboard.StoryboardItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]storyboard.StoryboardItem, 0)
	for _, item := range r.items {
		if item.StoryboardVersionID == storyboardVersionID {
			items = append(items, item)
		}
	}
	// Ordinal order, the same as the real read path's ORDER BY.
	for index := 1; index < len(items); index++ {
		for inner := index; inner > 0 && items[inner].Ordinal < items[inner-1].Ordinal; inner-- {
			items[inner], items[inner-1] = items[inner-1], items[inner]
		}
	}
	return items, nil
}

func (r *memoryRepo) CreatePanelVersion(_ context.Context, panel storyboard.StoryboardPanelVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNextWrite != nil {
		return r.failNextWrite
	}
	for _, existing := range r.panels {
		if existing.StoryboardItemID == panel.StoryboardItemID && existing.VersionNumber == panel.VersionNumber {
			return storyboard.ConflictError("That version number is already used for this panel.")
		}
	}
	r.panels[panel.ID] = panel
	return nil
}

func (r *memoryRepo) GetPanelVersion(_ context.Context, id string) (storyboard.StoryboardPanelVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	panel, ok := r.panels[id]
	if !ok {
		return storyboard.StoryboardPanelVersion{}, storyboard.NotFoundError()
	}
	return panel, nil
}

func (r *memoryRepo) MaxPanelVersionNumber(_ context.Context, storyboardItemID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	highest := 0
	for _, panel := range r.panels {
		if panel.StoryboardItemID == storyboardItemID && panel.VersionNumber > highest {
			highest = panel.VersionNumber
		}
	}
	return highest, nil
}

func (r *memoryRepo) ListPanelVersions(_ context.Context, storyboardItemID string) ([]storyboard.StoryboardPanelVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	panels := make([]storyboard.StoryboardPanelVersion, 0)
	for _, panel := range r.panels {
		if panel.StoryboardItemID == storyboardItemID {
			panels = append(panels, panel)
		}
	}
	return panels, nil
}

func (r *memoryRepo) ApprovePanelImage(_ context.Context, panelVersionID, approvedImageAssetVersionID string, expectedItemRevision int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	panel, ok := r.panels[panelVersionID]
	if !ok {
		return storyboard.NotFoundError()
	}
	item, ok := r.items[panel.StoryboardItemID]
	if !ok {
		return storyboard.NotFoundError()
	}
	if item.Revision != expectedItemRevision {
		return storyboard.ConflictError("This storyboard item changed in another window. Reload it and try again.")
	}
	item.Revision++
	r.items[item.ID] = item
	panel.ApprovedImageAssetVersionID = approvedImageAssetVersionID
	r.panels[panelVersionID] = panel
	r.approvedPanels = append(r.approvedPanels, panelVersionID)
	return nil
}

func newTestService(repo *memoryRepo) *Service {
	return NewService(Options{
		DirectorPlans: repo,
		Storyboards:   repo,
		Items:         repo,
		Panels:        repo,
		Clock:         testClock{now: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)},
		IDs:           &counterIDs{prefix: "id"},
	})
}

// TestAvailableIsFalseWithoutARepository proves the fail-closed rule: a service
// with no repository reports unavailable and its commands refuse rather than
// panicking on a nil interface.
func TestAvailableIsFalseWithoutARepository(t *testing.T) {
	service := NewService(Options{Clock: testClock{}, IDs: &counterIDs{prefix: "id"}})
	if service.Available() {
		t.Fatal("a service with no repository reports available")
	}
	if _, err := service.CreateDirectorPlanVersion(context.Background(), CreateDirectorPlanVersionRequest{EpisodeID: "ep-1", ScriptVersionID: "sv-1"}); err == nil {
		t.Fatal("a command on an unattached service succeeded")
	}
	if _, err := service.EnsureStoryboard(context.Background(), "ep-1"); err == nil {
		t.Fatal("EnsureStoryboard on an unattached service succeeded")
	}
	var nilService *Service
	if nilService.Available() {
		t.Fatal("a nil service reports available")
	}
}

// TestAvailableIsFalseWithoutAnIDGenerator proves the identifier half of the
// same rule: the application layer mints identifiers (ADR-0005), so a service
// that cannot is unavailable even with a working store.
func TestAvailableIsFalseWithoutAnIDGenerator(t *testing.T) {
	service := NewService(Options{DirectorPlans: newMemoryRepo()})
	if service.Available() {
		t.Fatal("a service with no identifier generator reports available")
	}
}

// TestCreateDirectorPlanVersionNumbersFromTheStoredMaximum covers the version
// numbering: the first is 1, the second is 2, and each starts as a draft.
func TestCreateDirectorPlanVersionNumbersFromTheStoredMaximum(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()

	first, err := service.CreateDirectorPlanVersion(ctx, CreateDirectorPlanVersionRequest{EpisodeID: "ep-1", ScriptVersionID: "sv-1"})
	if err != nil {
		t.Fatalf("CreateDirectorPlanVersion: %v", err)
	}
	if first.VersionNumber != 1 {
		t.Fatalf("first version number = %d, want 1", first.VersionNumber)
	}
	if first.Status != versioning.StatusDraft {
		t.Fatalf("status = %q, want draft", first.Status)
	}
	if first.CreatedByType != versioning.CreatedByUser {
		t.Fatalf("producer = %q, want the user default", first.CreatedByType)
	}
	second, err := service.CreateDirectorPlanVersion(ctx, CreateDirectorPlanVersionRequest{EpisodeID: "ep-1", ScriptVersionID: "sv-1", CreatedByType: versioning.CreatedByAgent})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if second.VersionNumber != 2 {
		t.Fatalf("second version number = %d, want 2", second.VersionNumber)
	}
	if second.CreatedByType != versioning.CreatedByAgent {
		t.Fatalf("an explicitly named producer was replaced: %q", second.CreatedByType)
	}
	// A different episode starts at 1, because numbering is per episode.
	other, err := service.CreateDirectorPlanVersion(ctx, CreateDirectorPlanVersionRequest{EpisodeID: "ep-2", ScriptVersionID: "sv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if other.VersionNumber != 1 {
		t.Fatalf("another episode's first plan is %d, want 1", other.VersionNumber)
	}
}

// TestCreateDirectorPlanVersionValidatesThroughTheDomain proves the domain's
// own rule is what refuses a plan with no script: the error category is the
// domain's, not one this layer invented.
func TestCreateDirectorPlanVersionValidatesThroughTheDomain(t *testing.T) {
	service := newTestService(newMemoryRepo())
	_, err := service.CreateDirectorPlanVersion(context.Background(), CreateDirectorPlanVersionRequest{EpisodeID: "ep-1"})
	if err == nil {
		t.Fatal("a director plan with no script version was accepted")
	}
	domainErr, ok := storyboard.AsError(err)
	if !ok || domainErr.Category != storyboard.CategoryInvalidInput {
		t.Fatalf("expected the domain's invalid_input, got %v", err)
	}
}

// TestEnsureStoryboardIsStablePerEpisode covers the get-or-create contract: the
// second call returns the first identity rather than creating another one.
func TestEnsureStoryboardIsStablePerEpisode(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()

	first, err := service.EnsureStoryboard(ctx, "ep-1")
	if err != nil {
		t.Fatalf("EnsureStoryboard: %v", err)
	}
	if first.ID == "" || first.EpisodeID != "ep-1" || first.Revision != 1 {
		t.Fatalf("created storyboard = %+v", first)
	}
	second, err := service.EnsureStoryboard(ctx, "ep-1")
	if err != nil {
		t.Fatalf("second EnsureStoryboard: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("the second call minted %q, want the stored %q", second.ID, first.ID)
	}
	if len(repo.storyboards) != 1 {
		t.Fatalf("%d storyboards stored, want 1", len(repo.storyboards))
	}
	if _, err := service.EnsureStoryboard(ctx, "  "); err == nil {
		t.Fatal("an empty episode id was accepted")
	}
}

// TestCreateStoryboardVersionRequiresBothInputs covers section 9.3: the version
// names the script and the director plan it was built from, so a request that
// names only one is refused.
func TestCreateStoryboardVersionRequiresBothInputs(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()
	identity, err := service.EnsureStoryboard(ctx, "ep-1")
	if err != nil {
		t.Fatal(err)
	}

	for _, request := range []CreateStoryboardVersionRequest{
		{StoryboardID: identity.ID, DirectorPlanVersionID: "dp-1"},
		{StoryboardID: identity.ID, ScriptVersionID: "sv-1"},
		{StoryboardID: identity.ID},
	} {
		if _, err := service.CreateStoryboardVersion(ctx, request); err == nil {
			t.Fatalf("a version missing an input was accepted: %+v", request)
		}
	}
	version, err := service.CreateStoryboardVersion(ctx, CreateStoryboardVersionRequest{
		StoryboardID: identity.ID, ScriptVersionID: "sv-1", DirectorPlanVersionID: "dp-1",
	})
	if err != nil {
		t.Fatalf("CreateStoryboardVersion: %v", err)
	}
	if version.VersionNumber != 1 || version.ScriptVersionID != "sv-1" || version.DirectorPlanVersionID != "dp-1" {
		t.Fatalf("stored version = %+v", version)
	}
	// ValidateAgainst is the domain's rule for this pair, so the stored row can
	// answer the question the stale chain will ask of it.
	if err := version.ValidateAgainst("sv-1", "dp-1"); err != nil {
		t.Fatalf("the stored version does not validate against its own inputs: %v", err)
	}
	if err := version.ValidateAgainst("sv-2", "dp-1"); err == nil {
		t.Fatal("the version validated against inputs it does not name")
	}
}

// TestCreateStoryboardItemNamesTheCollidingConstraint covers section 9.4's
// invariant: the schema's two unique constraints on a version's rows are
// checked, and the message says which one collided.
func TestCreateStoryboardItemNamesTheCollidingConstraint(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()
	identity, err := service.EnsureStoryboard(ctx, "ep-1")
	if err != nil {
		t.Fatal(err)
	}
	version, err := service.CreateStoryboardVersion(ctx, CreateStoryboardVersionRequest{
		StoryboardID: identity.ID, ScriptVersionID: "sv-1", DirectorPlanVersionID: "dp-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.CreateStoryboardItem(ctx, CreateStoryboardItemRequest{
		StoryboardVersionID: version.ID, ShotID: "shot-1", Ordinal: 1, ShotSize: "MCU",
	})
	if err != nil {
		t.Fatalf("CreateStoryboardItem: %v", err)
	}
	if first.Status != versioning.StatusDraft || first.Revision != 1 {
		t.Fatalf("created item = %+v", first)
	}

	// The same shot again is the shot constraint.
	_, err = service.CreateStoryboardItem(ctx, CreateStoryboardItemRequest{
		StoryboardVersionID: version.ID, ShotID: "shot-1", Ordinal: 2,
	})
	if err == nil {
		t.Fatal("a shot was added twice to one version")
	}
	domainErr, ok := storyboard.AsError(err)
	if !ok || domainErr.Category != storyboard.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if !strings.Contains(domainErr.SafeMessage, "shot") {
		t.Fatalf("the conflict does not name the shot constraint: %q", domainErr.SafeMessage)
	}

	// A different shot at the taken position is the ordinal constraint, and the
	// message names the shot that holds it.
	_, err = service.CreateStoryboardItem(ctx, CreateStoryboardItemRequest{
		StoryboardVersionID: version.ID, ShotID: "shot-2", Ordinal: 1,
	})
	if err == nil {
		t.Fatal("two shots were placed at one ordinal")
	}
	domainErr, ok = storyboard.AsError(err)
	if !ok || domainErr.Category != storyboard.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if !strings.Contains(domainErr.SafeMessage, "shot-1") {
		t.Fatalf("the conflict does not name the occupying shot: %q", domainErr.SafeMessage)
	}

	// The next free position is accepted, and the list comes back in order.
	if _, err := service.CreateStoryboardItem(ctx, CreateStoryboardItemRequest{
		StoryboardVersionID: version.ID, ShotID: "shot-2", Ordinal: 2,
	}); err != nil {
		t.Fatalf("the second shot was refused at a free position: %v", err)
	}
	items, err := service.ListStoryboardItems(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Ordinal != 1 || items[1].Ordinal != 2 {
		t.Fatalf("items = %+v", items)
	}
}

// TestCreateStoryboardItemRejectsAnUnknownVersion proves the parent is resolved
// before the write, so the caller gets a domain not-found rather than an
// unqualified constraint failure.
func TestCreateStoryboardItemRejectsAnUnknownVersion(t *testing.T) {
	service := newTestService(newMemoryRepo())
	_, err := service.CreateStoryboardItem(context.Background(), CreateStoryboardItemRequest{
		StoryboardVersionID: "missing", ShotID: "shot-1", Ordinal: 1,
	})
	if err == nil {
		t.Fatal("an item was created under a version that does not exist")
	}
	if _, ok := storyboard.AsError(err); !ok {
		t.Fatalf("expected a domain error, got %v", err)
	}
}

// TestCreatePanelVersionNumbersPerItem covers FR-070's "每个 Shot 可有多个面板
// 版本": numbering is per storyboard item, not per version.
func TestCreatePanelVersionNumbersPerItem(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()
	itemA := seedItem(t, service, "shot-1", 1)
	itemB := seedItem(t, service, "shot-2", 1)

	first, err := service.CreatePanelVersion(ctx, CreatePanelVersionRequest{StoryboardItemID: itemA.ID, VisualPrompt: "a"})
	if err != nil {
		t.Fatalf("CreatePanelVersion: %v", err)
	}
	second, err := service.CreatePanelVersion(ctx, CreatePanelVersionRequest{StoryboardItemID: itemA.ID, VisualPrompt: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if first.VersionNumber != 1 || second.VersionNumber != 2 {
		t.Fatalf("panel numbering = %d, %d, want 1, 2", first.VersionNumber, second.VersionNumber)
	}
	other, err := service.CreatePanelVersion(ctx, CreatePanelVersionRequest{StoryboardItemID: itemB.ID, VisualPrompt: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if other.VersionNumber != 1 {
		t.Fatalf("another item's first panel is %d, want 1", other.VersionNumber)
	}
	panels, err := service.ListPanels(ctx, itemA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(panels) != 2 {
		t.Fatalf("ListPanels returned %d rows, want 2", len(panels))
	}
}

// TestApprovePanelImageDelegatesToTheDomain covers section 9.5's third
// invariant: only a candidate may be approved, and the domain's own refusal is
// what the caller sees.
func TestApprovePanelImageDelegatesToTheDomain(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()
	item := seedItem(t, service, "shot-1", 1)
	panel, err := service.CreatePanelVersion(ctx, CreatePanelVersionRequest{StoryboardItemID: item.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Not a candidate: refused, and nothing written.
	_, err = service.ApprovePanelImage(ctx, ApprovePanelImageRequest{
		PanelVersionID:              panel.ID,
		ApprovedImageAssetVersionID: "av-9",
		CandidateVersionIDs:         []string{"av-1", "av-2"},
		ExpectedRevision:            1,
	})
	if err == nil {
		t.Fatal("an image that is not a candidate was approved")
	}
	domainErr, ok := storyboard.AsError(err)
	if !ok || domainErr.Category != storyboard.CategoryConflict {
		t.Fatalf("expected the domain's conflict, got %v", err)
	}
	if len(repo.approvedPanels) != 0 {
		t.Fatal("a refused approval wrote the row")
	}

	// An empty approval names no image at all, which is the other refusal the
	// domain makes.
	if _, err := service.ApprovePanelImage(ctx, ApprovePanelImageRequest{
		PanelVersionID: panel.ID, CandidateVersionIDs: []string{"av-1"}, ExpectedRevision: 1,
	}); err == nil {
		t.Fatal("an empty approved image was accepted")
	}

	// A candidate is approved, and the stored row carries it.
	approved, err := service.ApprovePanelImage(ctx, ApprovePanelImageRequest{
		PanelVersionID:              panel.ID,
		ApprovedImageAssetVersionID: "av-2",
		CandidateVersionIDs:         []string{"av-1", "av-2"},
		ExpectedRevision:            1,
	})
	if err != nil {
		t.Fatalf("ApprovePanelImage: %v", err)
	}
	if approved.ApprovedImageAssetVersionID != "av-2" {
		t.Fatalf("returned panel carries %q", approved.ApprovedImageAssetVersionID)
	}
	stored, err := repo.GetPanelVersion(ctx, panel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ApprovedImageAssetVersionID != "av-2" {
		t.Fatalf("stored approval = %q, want av-2", stored.ApprovedImageAssetVersionID)
	}
}

// TestApprovePanelImageGuardsTheItemRevision proves the compare-and-swap guard
// survives the service: a stale expected revision is refused.
func TestApprovePanelImageGuardsTheItemRevision(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()
	item := seedItem(t, service, "shot-1", 1)
	panel, err := service.CreatePanelVersion(ctx, CreatePanelVersionRequest{StoryboardItemID: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApprovePanelImage(ctx, ApprovePanelImageRequest{
		PanelVersionID: panel.ID, ApprovedImageAssetVersionID: "av-1",
		CandidateVersionIDs: []string{"av-1"}, ExpectedRevision: 7,
	}); err == nil {
		t.Fatal("a stale revision was accepted")
	}
	if len(repo.approvedPanels) != 0 {
		t.Fatal("a rejected approval wrote the row")
	}
}

// TestStorageFailureIsReportedNotPanicked proves a failing store surfaces as an
// error the caller can act on.
func TestStorageFailureIsReportedNotPanicked(t *testing.T) {
	repo := newMemoryRepo()
	repo.failNextWrite = errors.New("the disk is full")
	service := newTestService(repo)
	_, err := service.CreateDirectorPlanVersion(context.Background(), CreateDirectorPlanVersionRequest{
		EpisodeID: "ep-1", ScriptVersionID: "sv-1",
	})
	if err == nil {
		t.Fatal("a failing store reported success")
	}
}

// seedItem creates a storyboard item through the service, so the fixture obeys
// the same rules a caller does.
func seedItem(t *testing.T, service *Service, shotID string, ordinal int) storyboard.StoryboardItem {
	t.Helper()
	ctx := context.Background()
	identity, err := service.EnsureStoryboard(ctx, "ep-1")
	if err != nil {
		t.Fatal(err)
	}
	version, err := service.CreateStoryboardVersion(ctx, CreateStoryboardVersionRequest{
		StoryboardID: identity.ID, ScriptVersionID: "sv-1", DirectorPlanVersionID: "dp-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.CreateStoryboardItem(ctx, CreateStoryboardItemRequest{
		StoryboardVersionID: version.ID, ShotID: shotID, Ordinal: ordinal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// The approval doubles reproduce the repository's supersede-then-approve shape,
// including the event write, so a service test can assert the whole switch
// without a database.
func (r *memoryRepo) CurrentApprovedDirectorPlanVersionID(_ context.Context, episodeID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, plan := range r.plans {
		if plan.EpisodeID == episodeID && plan.Status == versioning.StatusApproved {
			return id, nil
		}
	}
	return "", nil
}

func (r *memoryRepo) ApproveDirectorPlanVersion(_ context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNextWrite != nil {
		return r.failNextWrite
	}
	target, ok := r.plans[versionID]
	if !ok || target.Status != expectedStatus {
		return storyboard.ConflictError("This version changed in another window. Reload it and try again.")
	}
	for id, existing := range r.plans {
		if existing.EpisodeID == episodeID && existing.Status == versioning.StatusApproved && id != versionID {
			existing.Status = versioning.StatusSuperseded
			r.plans[id] = existing
		}
	}
	target.Status = versioning.StatusApproved
	r.plans[versionID] = target
	r.events = append(r.events, record)
	return nil
}

func (r *memoryRepo) CurrentApprovedStoryboardVersionID(_ context.Context, storyboardID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, version := range r.versions {
		if version.StoryboardID == storyboardID && version.Status == versioning.StatusApproved {
			return id, nil
		}
	}
	return "", nil
}

func (r *memoryRepo) ApproveStoryboardVersion(_ context.Context, versionID, storyboardID string, expectedStatus versioning.Status, record event.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNextWrite != nil {
		return r.failNextWrite
	}
	target, ok := r.versions[versionID]
	if !ok || target.Status != expectedStatus {
		return storyboard.ConflictError("This version changed in another window. Reload it and try again.")
	}
	for id, existing := range r.versions {
		if existing.StoryboardID == storyboardID && existing.Status == versioning.StatusApproved && id != versionID {
			existing.Status = versioning.StatusSuperseded
			r.versions[id] = existing
		}
	}
	target.Status = versioning.StatusApproved
	r.versions[versionID] = target
	r.events = append(r.events, record)
	return nil
}

func (r *memoryRepo) ProjectOfEpisode(_ context.Context, episodeID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	projectID, ok := r.episodeProjects[episodeID]
	if !ok {
		return "", storyboard.NotFoundError()
	}
	return projectID, nil
}

func (r *memoryRepo) ProjectOfStoryboard(_ context.Context, storyboardID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	board, ok := r.storyboards[storyboardID]
	if !ok {
		return "", storyboard.NotFoundError()
	}
	projectID, ok := r.episodeProjects[board.EpisodeID]
	if !ok {
		return "", storyboard.NotFoundError()
	}
	return projectID, nil
}
