package database

import (
	"context"
	"testing"
	"time"

	appevents "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// This file covers the five version families that had no way to reach
// `approved` before this work: skeleton, adaptation strategy, director plan,
// storyboard version and style guide version.
//
// Each command is the same §2.5 switch, so the tests check the same three things
// per family — the target becomes approved, the previous approval becomes
// superseded, and the §17 event lands. The assertion that matters most is the
// one in TestApprovalRefusesWhenTheEventCannotBeRecorded: an approval whose
// governance record cannot be written is refused rather than applied.

// approvalFixture holds the services and repositories the approval tests need.
//
// The fixture's episode-1 belongs to project-1 (seedWP05Parents writes it), so
// an approval can resolve the project its event is filed under.
type approvalFixture struct {
	scriptService    *appscript.Service
	boardService     *appstoryboard.Service
	scriptRepository *ScriptRepository
	boardRepository  *StoryboardRepository
	eventRepository  *EventRepository
}

func openApprovalFixture(t *testing.T) approvalFixture {
	t.Helper()
	ctx := context.Background()
	handle, err := open(ctx, t.TempDir()+"/app.db", t.TempDir()+"/snapshots", wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close(ctx) })
	db := handle.SQL()
	seedWP05Parents(t, db)

	eventRepository := NewEventRepository(db)
	eventService := appevents.NewService(appevents.Options{
		Repository: eventRepository,
		Clock:      fixedClockProvider{},
		IDs:        newTestIDGenerator(),
	})
	scriptRepository := NewScriptRepository(db)
	boardRepository := NewStoryboardRepository(db)
	return approvalFixture{
		scriptService: appscript.NewService(appscript.Options{
			Repository: scriptRepository,
			Clock:      fixedClockProvider{},
			IDs:        newTestIDGenerator(),
			Events:     eventService,
		}),
		boardService: appstoryboard.NewService(appstoryboard.Options{
			DirectorPlans: boardRepository,
			Storyboards:   boardRepository,
			Items:         boardRepository,
			Panels:        boardRepository,
			Clock:         fixedClockProvider{},
			IDs:           newTestIDGenerator(),
			Events:        eventService,
		}),
		scriptRepository: scriptRepository,
		boardRepository:  boardRepository,
		eventRepository:  eventRepository,
	}
}

// approvalEvents returns the events of one type in the fixture's project.
func (f approvalFixture) approvalEvents(t *testing.T, eventType event.Type) []event.Event {
	t.Helper()
	events, err := f.eventRepository.ListEvents(context.Background(), ListFilter{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	var matching []event.Event
	for _, record := range events {
		if record.EventType == eventType {
			matching = append(matching, record)
		}
	}
	return matching
}

// seedSkeleton writes one skeleton version through the repository.
func (f approvalFixture) seedSkeleton(t *testing.T, id string, number int) {
	t.Helper()
	if err := f.scriptRepository.CreateStorySkeletonVersion(context.Background(), scriptdomain.StorySkeletonVersion{
		ID: id, EpisodeID: "episode-1", VersionNumber: number,
		Status: versioning.StatusDraft, OpeningHook: "a hook",
		CreatedByType: versioning.CreatedByUser, CreatedAt: dramaTime(),
	}); err != nil {
		t.Fatalf("creating %s: %v", id, err)
	}
}

// TestApproveStorySkeletonVersionIsTheSection25Switch is the skeleton family:
// approve v1, approve v2, and v1 must become superseded with one approved row.
func TestApproveStorySkeletonVersionIsTheSection25Switch(t *testing.T) {
	fixture := openApprovalFixture(t)
	ctx := context.Background()
	fixture.seedSkeleton(t, "skeleton-1", 1)
	fixture.seedSkeleton(t, "skeleton-2", 2)

	approved, err := fixture.scriptService.ApproveStorySkeletonVersion(ctx, appscript.ApproveStorySkeletonVersionRequest{
		VersionID: "skeleton-1", TraceID: "trace-1",
	})
	if err != nil {
		t.Fatalf("approving skeleton-1: %v", err)
	}
	if approved.Status != versioning.StatusApproved {
		t.Fatalf("status = %q, want approved", approved.Status)
	}
	if _, err := fixture.scriptService.ApproveStorySkeletonVersion(ctx, appscript.ApproveStorySkeletonVersionRequest{
		VersionID: "skeleton-2",
	}); err != nil {
		t.Fatalf("approving skeleton-2: %v", err)
	}

	first, err := fixture.scriptRepository.GetStorySkeletonVersion(ctx, "skeleton-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != versioning.StatusSuperseded {
		t.Fatalf("the replaced skeleton is %q, want superseded", first.Status)
	}
	current, err := fixture.scriptService.ApprovedSkeletonVersionID(ctx, "episode-1")
	if err != nil {
		t.Fatal(err)
	}
	if current != "skeleton-2" {
		t.Fatalf("the approved skeleton is %q, want skeleton-2", current)
	}

	// Exactly one approved row, which the partial unique index enforces. The
	// count is read directly because it is the schema invariant the switch
	// exists to preserve.
	var approvedCount int
	if err := fixture.scriptRepository.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM story_skeleton_versions WHERE episode_id = ? AND status = 'approved'`,
		"episode-1").Scan(&approvedCount); err != nil {
		t.Fatal(err)
	}
	if approvedCount != 1 {
		t.Fatalf("%d approved skeletons, want exactly 1", approvedCount)
	}

	// Both approvals are in the stream.
	if events := fixture.approvalEvents(t, event.StorySkeletonApproved); len(events) != 2 {
		t.Fatalf("%d skeleton approval events, want 2", len(events))
	} else {
		for _, record := range events {
			if record.AggregateID == "skeleton-1" && record.TraceID != "trace-1" {
				t.Fatalf("the first approval's trace = %q, want trace-1", record.TraceID)
			}
		}
	}

	// An already-approved version is refused by the domain, so the switch cannot
	// be replayed into a second event.
	if _, err := fixture.scriptService.ApproveStorySkeletonVersion(ctx, appscript.ApproveStorySkeletonVersionRequest{
		VersionID: "skeleton-2",
	}); err == nil {
		t.Fatal("an already-approved skeleton was approved again")
	}
	if after := fixture.approvalEvents(t, event.StorySkeletonApproved); len(after) != 2 {
		t.Fatalf("a refused approval wrote an event: %d now", len(after))
	}
}

// TestApproveAdaptationStrategyVersionIsTheSection25Switch is the strategy
// family, the second of the two script-side ones.
func TestApproveAdaptationStrategyVersionIsTheSection25Switch(t *testing.T) {
	fixture := openApprovalFixture(t)
	ctx := context.Background()

	for _, version := range []struct {
		id     string
		number int
	}{{"strategy-1", 1}, {"strategy-2", 2}} {
		if err := fixture.scriptRepository.CreateAdaptationStrategyVersion(ctx, scriptdomain.AdaptationStrategyVersion{
			ID: version.id, EpisodeID: "episode-1", VersionNumber: version.number,
			Status: versioning.StatusDraft, StrategySummary: "a summary",
			AdaptationMode: scriptdomain.AdaptationBalanced,
			CreatedByType:  versioning.CreatedByUser, CreatedAt: dramaTime(),
		}); err != nil {
			t.Fatalf("creating %s: %v", version.id, err)
		}
	}
	if _, err := fixture.scriptService.ApproveAdaptationStrategyVersion(ctx, appscript.ApproveAdaptationStrategyVersionRequest{
		VersionID: "strategy-1",
	}); err != nil {
		t.Fatalf("approving strategy-1: %v", err)
	}
	if _, err := fixture.scriptService.ApproveAdaptationStrategyVersion(ctx, appscript.ApproveAdaptationStrategyVersionRequest{
		VersionID: "strategy-2",
	}); err != nil {
		t.Fatalf("approving strategy-2: %v", err)
	}
	first, err := fixture.scriptRepository.GetAdaptationStrategyVersion(ctx, "strategy-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != versioning.StatusSuperseded {
		t.Fatalf("the replaced strategy is %q, want superseded", first.Status)
	}
	current, err := fixture.scriptService.ApprovedStrategyVersionID(ctx, "episode-1")
	if err != nil {
		t.Fatal(err)
	}
	if current != "strategy-2" {
		t.Fatalf("the approved strategy is %q, want strategy-2", current)
	}
	if events := fixture.approvalEvents(t, event.AdaptationStrategyApproved); len(events) != 2 {
		t.Fatalf("%d strategy approval events, want 2", len(events))
	}
}

// TestApproveDirectorPlanVersionIsTheSection25Switch covers the director plan,
// whose event's project is resolved through the episode because the storyboard
// tables carry no project column.
func TestApproveDirectorPlanVersionIsTheSection25Switch(t *testing.T) {
	fixture := openApprovalFixture(t)
	ctx := context.Background()

	for _, version := range []struct {
		id     string
		number int
	}{{"plan-1", 1}, {"plan-2", 2}} {
		if err := fixture.boardRepository.CreateDirectorPlanVersion(ctx, storyboard.DirectorPlanVersion{
			ID: version.id, EpisodeID: "episode-1", VersionNumber: version.number,
			Status: versioning.StatusDraft, ScriptVersionID: "script-version-1",
			CreatedByType: versioning.CreatedByUser, CreatedAt: dramaTime(),
		}); err != nil {
			t.Fatalf("creating %s: %v", version.id, err)
		}
	}
	if _, err := fixture.boardService.ApproveDirectorPlanVersion(ctx, appstoryboard.ApproveDirectorPlanVersionRequest{
		VersionID: "plan-1",
	}); err != nil {
		t.Fatalf("approving plan-1: %v", err)
	}
	if _, err := fixture.boardService.ApproveDirectorPlanVersion(ctx, appstoryboard.ApproveDirectorPlanVersionRequest{
		VersionID: "plan-2", TraceID: "trace-plan",
	}); err != nil {
		t.Fatalf("approving plan-2: %v", err)
	}
	first, err := fixture.boardRepository.GetDirectorPlanVersion(ctx, "plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != versioning.StatusSuperseded {
		t.Fatalf("the replaced plan is %q, want superseded", first.Status)
	}
	current, err := fixture.boardService.ApprovedDirectorPlanVersionID(ctx, "episode-1")
	if err != nil {
		t.Fatal(err)
	}
	if current != "plan-2" {
		t.Fatalf("the approved plan is %q, want plan-2", current)
	}
	// The event's project came from the episode, which the plan does not name.
	events := fixture.approvalEvents(t, event.DirectorPlanApproved)
	if len(events) != 2 {
		t.Fatalf("%d plan approval events, want 2", len(events))
	}
	for _, record := range events {
		if record.ProjectID != "project-1" {
			t.Fatalf("a plan approval is filed under %q, want project-1", record.ProjectID)
		}
		if record.AggregateID == "plan-2" && record.TraceID != "trace-plan" {
			t.Fatalf("the second plan approval's trace = %q, want trace-plan", record.TraceID)
		}
	}
	if _, err := fixture.boardService.ApproveDirectorPlanVersion(ctx, appstoryboard.ApproveDirectorPlanVersionRequest{
		VersionID: "no-such-plan",
	}); err == nil {
		t.Fatal("approving a missing plan succeeded")
	}
}

// TestApproveStoryboardVersionIsTheSection25Switch covers the storyboard version
// family, including that a superseded version cannot come back.
func TestApproveStoryboardVersionIsTheSection25Switch(t *testing.T) {
	fixture := openApprovalFixture(t)
	ctx := context.Background()
	boardID, planID := dramaSeedBoard(t, fixture.boardRepository)

	for _, version := range []struct {
		id     string
		number int
	}{{"board-1", 1}, {"board-2", 2}} {
		if err := fixture.boardRepository.CreateStoryboardVersion(ctx, storyboard.StoryboardVersion{
			ID: version.id, StoryboardID: boardID, VersionNumber: version.number,
			Status: versioning.StatusDraft, ScriptVersionID: "script-version-1",
			DirectorPlanVersionID: planID, CreatedByType: versioning.CreatedByUser, CreatedAt: dramaTime(),
		}); err != nil {
			t.Fatalf("creating %s: %v", version.id, err)
		}
	}
	if _, err := fixture.boardService.ApproveStoryboardVersion(ctx, appstoryboard.ApproveStoryboardVersionRequest{
		VersionID: "board-1",
	}); err != nil {
		t.Fatalf("approving board-1: %v", err)
	}
	// Approving it twice is refused.
	if _, err := fixture.boardService.ApproveStoryboardVersion(ctx, appstoryboard.ApproveStoryboardVersionRequest{
		VersionID: "board-1",
	}); err == nil {
		t.Fatal("an already-approved storyboard version was approved again")
	}
	if _, err := fixture.boardService.ApproveStoryboardVersion(ctx, appstoryboard.ApproveStoryboardVersionRequest{
		VersionID: "board-2",
	}); err != nil {
		t.Fatalf("approving board-2: %v", err)
	}
	first, err := fixture.boardRepository.GetStoryboardVersion(ctx, "board-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != versioning.StatusSuperseded {
		t.Fatalf("the replaced version is %q, want superseded", first.Status)
	}
	// A superseded version cannot return.
	if _, err := fixture.boardService.ApproveStoryboardVersion(ctx, appstoryboard.ApproveStoryboardVersionRequest{
		VersionID: "board-1",
	}); err == nil {
		t.Fatal("a superseded version was approved again")
	}
	current, err := fixture.boardService.ApprovedStoryboardVersionID(ctx, boardID)
	if err != nil {
		t.Fatal(err)
	}
	if current != "board-2" {
		t.Fatalf("the approved version is %q, want board-2", current)
	}
	if events := fixture.approvalEvents(t, event.StoryboardVersionApproved); len(events) != 2 {
		t.Fatalf("%d storyboard approval events, want 2", len(events))
	}
}

// TestApprovalRefusesWhenTheEventCannotBeRecorded is the rule that makes the
// event a governance record rather than a notification.
//
// A service with no recorder must refuse the approval and leave the version
// untouched, because an approval nobody can audit is worse than an approval that
// did not happen.
func TestApprovalRefusesWhenTheEventCannotBeRecorded(t *testing.T) {
	fixture := openApprovalFixture(t)
	ctx := context.Background()
	boardID, planID := dramaSeedBoard(t, fixture.boardRepository)
	if err := fixture.boardRepository.CreateStoryboardVersion(ctx, storyboard.StoryboardVersion{
		ID: "board-no-recorder", StoryboardID: boardID, VersionNumber: 1,
		Status: versioning.StatusDraft, ScriptVersionID: "script-version-1",
		DirectorPlanVersionID: planID, CreatedByType: versioning.CreatedByUser, CreatedAt: dramaTime(),
	}); err != nil {
		t.Fatal(err)
	}

	// The same repositories, but no recorder: every other command still works
	// (the creation above did), and this one refuses.
	withoutRecorder := appstoryboard.NewService(appstoryboard.Options{
		DirectorPlans: fixture.boardRepository,
		Storyboards:   fixture.boardRepository,
		Items:         fixture.boardRepository,
		Panels:        fixture.boardRepository,
		Clock:         fixedClockProvider{},
		IDs:           newTestIDGenerator(),
	})
	if _, err := withoutRecorder.ApproveStoryboardVersion(ctx, appstoryboard.ApproveStoryboardVersionRequest{
		VersionID: "board-no-recorder",
	}); err == nil {
		t.Fatal("an approval without a recorder was applied")
	}
	stored, err := fixture.boardRepository.GetStoryboardVersion(ctx, "board-no-recorder")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status == versioning.StatusApproved {
		t.Fatal("the refused approval changed the version")
	}
	if events := fixture.approvalEvents(t, event.StoryboardVersionApproved); len(events) != 0 {
		t.Fatalf("a refused approval wrote %d events", len(events))
	}

	// The script side behaves the same way.
	fixture.seedSkeleton(t, "skeleton-no-recorder", 1)
	scriptWithoutRecorder := appscript.NewService(appscript.Options{
		Repository: fixture.scriptRepository,
		Clock:      fixedClockProvider{},
		IDs:        newTestIDGenerator(),
	})
	if _, err := scriptWithoutRecorder.ApproveStorySkeletonVersion(ctx, appscript.ApproveStorySkeletonVersionRequest{
		VersionID: "skeleton-no-recorder",
	}); err == nil {
		t.Fatal("a script approval without a recorder was applied")
	}
	reloaded, err := fixture.scriptRepository.GetStorySkeletonVersion(ctx, "skeleton-no-recorder")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status == versioning.StatusApproved {
		t.Fatal("the refused script approval changed the version")
	}
}

// dramaSeedBoard writes a storyboard identity and a director plan for the
// fixture's episode, which a storyboard version needs before it can exist.
func dramaSeedBoard(t *testing.T, repo *StoryboardRepository) (boardID, planID string) {
	t.Helper()
	ctx := context.Background()
	generator := dramaIDGenerator()
	planID = mustNewID(t, generator)
	boardID = mustNewID(t, generator)
	if err := repo.CreateDirectorPlanVersion(ctx, storyboard.DirectorPlanVersion{
		ID: planID, EpisodeID: "episode-1", VersionNumber: 1, Status: versioning.StatusDraft,
		ScriptVersionID: "script-version-1", CreatedByType: versioning.CreatedByUser, CreatedAt: dramaTime(),
	}); err != nil {
		t.Fatalf("CreateDirectorPlanVersion: %v", err)
	}
	if err := repo.CreateStoryboard(ctx, storyboard.Storyboard{
		ID: boardID, EpisodeID: "episode-1", CreatedAt: dramaTime(), UpdatedAt: dramaTime(), Revision: 1,
	}); err != nil {
		t.Fatalf("CreateStoryboard: %v", err)
	}
	return boardID, planID
}
