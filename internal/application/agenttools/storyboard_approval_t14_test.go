package agenttools

import (
	"context"
	"testing"
	"time"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"

	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// storyboard_approval_t14_test.go is the 2026-09-26 audit's T14: the
// storyboard-creation tool cited a script/plan version after checking only the
// EPISODE, so a draft or rejected version could be boarded — and the audit's
// completion standard is "用同集 draft/rejected/superseded 输入走正式
// Tool/Binding，按明确策略拒绝；批准引用正常通过；不能仅验证 ID 存在".
//
// The tool now requires both cited versions to be `approved` — exactly that
// status, because a superseded version is history, not something in force to
// build on.

// storyboardFake answers the reads the board tool makes, over rows the test
// names — the same shape the script fake carries, for the storyboard side.
type storyboardFake struct {
	plans     map[string]storyboard.DirectorPlanVersion
	boards    map[string]storyboard.Storyboard
	versions  map[string]storyboard.StoryboardVersion
	structure scriptdomain.ScriptStructure
	// writtenVersions counts the version writes the tool asked for.
	writtenVersions int
	// writtenItems counts the item writes.
	writtenItems int
}

func (f *storyboardFake) GetDirectorPlanVersion(_ context.Context, id string) (storyboard.DirectorPlanVersion, error) {
	plan, ok := f.plans[id]
	if !ok {
		return storyboard.DirectorPlanVersion{}, storyboard.NotFoundError()
	}
	return plan, nil
}

func (f *storyboardFake) GetStoryboard(_ context.Context, id string) (storyboard.Storyboard, error) {
	board, ok := f.boards[id]
	if !ok {
		return storyboard.Storyboard{}, storyboard.NotFoundError()
	}
	return board, nil
}

func (f *storyboardFake) MaxStoryboardVersionNumber(_ context.Context, _ string) (int, error) {
	return len(f.versions), nil
}

func (f *storyboardFake) CreateStoryboardVersion(_ context.Context, version storyboard.StoryboardVersion) error {
	f.writtenVersions++
	f.versions[version.ID] = version
	return nil
}

func (f *storyboardFake) CreateStoryboardItem(_ context.Context, _ storyboard.StoryboardItem) error {
	f.writtenItems++
	return nil
}

func (f *storyboardFake) MaxVersionNumber() (int, error) { return len(f.versions), nil }

// The port methods this walk does not drive panic rather than returning zero
// rows: reaching one means the test went somewhere it did not mean to.
func (f *storyboardFake) ApproveDirectorPlanVersion(context.Context, string, string, versioning.Status, event.Event) error {
	panic("storyboardFake: ApproveDirectorPlanVersion is not part of this walk")
}
func (f *storyboardFake) CreateDirectorPlanVersion(context.Context, storyboard.DirectorPlanVersion) error {
	panic("storyboardFake: CreateDirectorPlanVersion is not part of this walk")
}
func (f *storyboardFake) MaxDirectorPlanVersionNumber(context.Context, string) (int, error) {
	return 0, nil
}
func (f *storyboardFake) CurrentApprovedDirectorPlanVersionID(context.Context, string) (string, error) {
	return "", nil
}
func (f *storyboardFake) ListDirectorPlanVersions(context.Context, string) ([]storyboard.DirectorPlanVersion, error) {
	return nil, nil
}
func (f *storyboardFake) UpdateDirectorPlanOverrides(context.Context, string, string) error {
	panic("storyboardFake: UpdateDirectorPlanOverrides is not part of this walk")
}
func (f *storyboardFake) ProjectOfEpisode(context.Context, string) (string, error) {
	return "project-1", nil
}
func (f *storyboardFake) GetStoryboardByEpisode(context.Context, string) (storyboard.Storyboard, error) {
	return storyboard.Storyboard{ID: "board-1"}, nil
}
func (f *storyboardFake) CreateStoryboard(context.Context, storyboard.Storyboard) error {
	panic("storyboardFake: CreateStoryboard is not part of this walk")
}
func (f *storyboardFake) GetStoryboardVersion(_ context.Context, id string) (storyboard.StoryboardVersion, error) {
	version, ok := f.versions[id]
	if !ok {
		return storyboard.StoryboardVersion{}, storyboard.NotFoundError()
	}
	return version, nil
}
func (f *storyboardFake) CurrentApprovedStoryboardVersionID(context.Context, string) (string, error) {
	return "", nil
}
func (f *storyboardFake) ApproveStoryboardVersion(context.Context, string, string, versioning.Status, event.Event) error {
	panic("storyboardFake: ApproveStoryboardVersion is not part of this walk")
}
func (f *storyboardFake) ProjectOfStoryboard(context.Context, string) (string, error) {
	return "project-1", nil
}
func (f *storyboardFake) ListStoryboardVersions(context.Context, string) ([]storyboard.StoryboardVersion, error) {
	return nil, nil
}
func (f *storyboardFake) CreatePanelVersion(context.Context, storyboard.StoryboardPanelVersion) error {
	panic("storyboardFake: CreatePanelVersion is not part of this walk")
}
func (f *storyboardFake) GetPanelVersion(context.Context, string) (storyboard.StoryboardPanelVersion, error) {
	return storyboard.StoryboardPanelVersion{}, storyboard.NotFoundError()
}
func (f *storyboardFake) GetStoryboardItem(context.Context, string) (storyboard.StoryboardItem, error) {
	return storyboard.StoryboardItem{}, storyboard.NotFoundError()
}

// GetScriptStructure is the script-side read the item walk makes; the fake
// script service answers it, so nothing is needed here.

// TestCreateStoryboardRefusesADraftScriptVersion is the headline negative: a
// board drawn from a draft script of the SAME episode is refused, because the
// stage's approval is not the cited version's.
func TestCreateStoryboardRefusesADraftScriptVersion(t *testing.T) {
	fake := fixtureForStructureTools() // version-1 is a draft of episode-1
	handler := bindCreateStoryboardVersion(Deps{
		Script:     scriptToolsService(fake),
		Storyboard: storyboardServiceForTest(&storyboardFake{plans: map[string]storyboard.DirectorPlanVersion{}}),
	})
	_, err := handler(context.Background(), newScriptToolRequest("project-1",
		`{"episodeId":"episode-1","scriptVersionId":"version-1","directorPlanVersionId":"plan-1","items":[]}`))
	if err == nil {
		t.Fatal("a board was drawn from a draft script version")
	}
	domainErr, ok := agent.AsError(err)
	if !ok || domainErr.Category != agent.CategoryInvalidInput {
		t.Fatalf("the refusal is %v, want the domain's invalid-input error", err)
	}
}

// TestCreateStoryboardRefusesADraftPlanVersion is the plan half: an unapproved
// plan is refused the same way, naming the plan rather than the script.
func TestCreateStoryboardRefusesADraftPlanVersion(t *testing.T) {
	fake := fixtureForStructureTools()
	fake.versions["version-1"] = scriptVersionApproved()
	plans := map[string]storyboard.DirectorPlanVersion{
		"plan-1": {ID: "plan-1", EpisodeID: "episode-1", Status: versioning.StatusDraft},
	}
	handler := bindCreateStoryboardVersion(Deps{
		Script:     scriptToolsService(fake),
		Storyboard: storyboardServiceForTest(&storyboardFake{plans: plans}),
	})
	_, err := handler(context.Background(), newScriptToolRequest("project-1",
		`{"episodeId":"episode-1","scriptVersionId":"version-1","directorPlanVersionId":"plan-1","items":[]}`))
	if err == nil {
		t.Fatal("a board was drawn from a draft director plan version")
	}
}

// TestCreateStoryboardAcceptsApprovedCitations is the positive half: approved
// script and plan versions of the same episode pass the checks and the write
// is attempted (the walk's empty item list reaches the version write).
func TestCreateStoryboardAcceptsApprovedCitations(t *testing.T) {
	fake := fixtureForStructureTools()
	fake.versions["version-1"] = scriptVersionApproved()
	board := storyboardFake{
		plans: map[string]storyboard.DirectorPlanVersion{
			"plan-1": {ID: "plan-1", EpisodeID: "episode-1", Status: versioning.StatusApproved},
		},
		boards:   map[string]storyboard.Storyboard{"board-1": {ID: "board-1", EpisodeID: "episode-1", Revision: 1}},
		versions: map[string]storyboard.StoryboardVersion{},
	}
	handler := bindCreateStoryboardVersion(Deps{
		Script:     scriptToolsService(fake),
		Storyboard: storyboardServiceForTest(&board),
	})
	if _, err := handler(context.Background(), newScriptToolRequest("project-1",
		`{"episodeId":"episode-1","scriptVersionId":"version-1","directorPlanVersionId":"plan-1","items":[]}`)); err != nil {
		t.Fatalf("approved citations were refused: %v", err)
	}
}

// scriptVersionApproved is version-1 with its status IN FORCE — the fact the
// tool now requires.
// storyboardServiceForTest builds the real storyboard service over the fake,
// so the tool drives the same code path production does.
func storyboardServiceForTest(fake *storyboardFake) *appstoryboard.Service {
	return appstoryboard.NewService(appstoryboard.Options{
		DirectorPlans: fake,
		Storyboards:   fake,
		Items:         fake,
		Panels:        fake,
		IDs:           idGeneratorForTest(),
	})
}

// idGeneratorForTest satisfies the service's ID port.
func idGeneratorForTest() idGen { return idGen{} }

type idGen struct{}

func (idGen) New() (string, error) {
	return "test-id-" + time.Now().UTC().Format("150405.000000000"), nil
}

func scriptVersionApproved() scriptdomain.ScriptVersion {
	return scriptdomain.ScriptVersion{
		ID: "version-1", ScriptID: "script-1", VersionNumber: 1,
		Status: versioning.StatusApproved, CreatedByType: versioning.CreatedByUser,
		StorySkeletonVersionID: "skeleton-1", AdaptationStrategyVersionID: "strategy-1",
	}
}

var _ = event.Event{}

func (f *storyboardFake) FindStoryboardItemByOrdinal(context.Context, string, int) (storyboard.StoryboardItem, bool, error) {
	return storyboard.StoryboardItem{}, false, nil
}
func (f *storyboardFake) FindStoryboardItemByShot(context.Context, string, string) (storyboard.StoryboardItem, bool, error) {
	return storyboard.StoryboardItem{}, false, nil
}
func (f *storyboardFake) ListStoryboardItems(context.Context, string) ([]storyboard.StoryboardItem, error) {
	return nil, nil
}

func (f *storyboardFake) CreateStoryboardItems(context.Context, []storyboard.StoryboardItem) error {
	panic("storyboardFake: CreateStoryboardItems is not part of this walk")
}

func (f *storyboardFake) ReorderStoryboardItem(context.Context, string, int) (appstoryboard.ReorderResult, error) {
	panic("storyboardFake: ReorderStoryboardItem is not part of this walk")
}

func (f *storyboardFake) UpdateStoryboardItem(context.Context, storyboard.StoryboardItem, int64) error {
	panic("storyboardFake: UpdateStoryboardItem is not part of this walk")
}

func (f *storyboardFake) MaxPanelVersionNumber(context.Context, string) (int, error) {
	return 0, nil
}

func (f *storyboardFake) ListPanelVersions(context.Context, string) ([]storyboard.StoryboardPanelVersion, error) {
	return nil, nil
}

func (f *storyboardFake) ApprovePanelImage(context.Context, string, string, int64) error {
	panic("storyboardFake: ApprovePanelImage is not part of this walk")
}
