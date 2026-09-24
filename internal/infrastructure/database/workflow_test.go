package database

import (
	"context"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// dramaRunFixture is one stored run, so a test can hang a stage off it.
type dramaRunFixture struct {
	RunID   string
	EventID string
}

// dramaCreateRun stores one run through the repository's production write path,
// including the audit event that records its creation.
func dramaCreateRun(t *testing.T, repo *WorkflowRepository, runID, eventID string, revision int64) dramaRunFixture {
	t.Helper()
	ctx := context.Background()
	now := dramaTime()
	if err := repo.CreateRun(ctx, workflow.WorkflowRun{
		ID: runID, ProjectID: "drama-project", EpisodeID: "drama-episode",
		WorkflowType: "episode_production", Status: workflow.RunPending,
		CreatedAt: now, UpdatedAt: now, Revision: revision,
	}, workflow.WorkflowEvent{
		ID: eventID, WorkflowRunID: runID, EventType: "workflow_run_created",
		ToStatus: string(workflow.RunPending), ActorType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	return dramaRunFixture{RunID: runID, EventID: eventID}
}

// TestWorkflowRepositoryRoundTrip covers the run, stage, report and event
// families: every value written comes back, and the vocabularies survive.
func TestWorkflowRepositoryRoundTrip(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewWorkflowRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	now := dramaTime()

	runID := mustNewID(t, generator)
	dramaCreateRun(t, repo, runID, mustNewID(t, generator), 1)

	run, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.ProjectID != "drama-project" || run.EpisodeID != "drama-episode" || run.WorkflowType != "episode_production" {
		t.Fatalf("run round trip changed the row: %+v", run)
	}
	if run.Status != workflow.RunPending || run.Revision != 1 {
		t.Fatalf("run status/revision = %q/%d", run.Status, run.Revision)
	}
	if !run.CreatedAt.Equal(now) {
		t.Fatalf("created_at = %v, want %v", run.CreatedAt, now)
	}
	runs, err := repo.ListRuns(ctx, "drama-project")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	// The fixture's own run is in the project, so the check is that this run is
	// listed and comes first: its created_at is the fixed clock's 2026-09-16,
	// later than the fixture's 2026-01-01.
	if len(runs) != 2 {
		t.Fatalf("ListRuns returned %d rows, want the created run and the fixture's", len(runs))
	}
	if runs[0].ID != runID {
		t.Fatalf("ListRuns is not newest first: %+v", runs)
	}

	stageID := mustNewID(t, generator)
	if err := repo.CreateStage(ctx, workflow.StageRun{
		ID: stageID, WorkflowRunID: runID, Stage: "story_skeleton", Attempt: 1,
		ExecutionAgentKey: "agent.story_skeleton", Status: workflow.StagePending,
		InputJSON: `{"episodeId":"drama-episode"}`, CreatedAt: now.Add(time.Minute), Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: stageID,
		EventType: "stage_run_created", ToStatus: string(workflow.StagePending),
		ActorType: versioning.CreatedByUser,
		// A minute after the creation event, so the read path's ordering is a
		// fact the fixture states rather than a tie broken by a generated id.
		CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	stage, err := repo.GetStage(ctx, stageID)
	if err != nil {
		t.Fatalf("GetStage: %v", err)
	}
	if stage.Stage != "story_skeleton" || stage.Attempt != 1 || stage.ExecutionAgentKey != "agent.story_skeleton" {
		t.Fatalf("stage round trip changed the row: %+v", stage)
	}
	if stage.Status != workflow.StagePending || stage.InputJSON != `{"episodeId":"drama-episode"}` {
		t.Fatalf("stage vocabulary lost: %+v", stage)
	}
	stages, err := repo.ListStages(ctx, runID)
	if err != nil {
		t.Fatalf("ListStages: %v", err)
	}
	if len(stages) != 1 || stages[0].ID != stageID {
		t.Fatalf("ListStages = %+v", stages)
	}
	// The attempt is unique per (run, stage, attempt), which the schema declares.
	if err := repo.CreateStage(ctx, workflow.StageRun{
		ID: mustNewID(t, generator), WorkflowRunID: runID, Stage: "story_skeleton", Attempt: 1,
		Status: workflow.StagePending, CreatedAt: now, Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, EventType: "stage_run_created",
		ToStatus: string(workflow.StagePending), ActorType: versioning.CreatedByUser, CreatedAt: now,
	}); err == nil {
		t.Fatal("a duplicate stage attempt was stored")
	}

	score := 0.75
	reportID := mustNewID(t, generator)
	if err := repo.CreateReport(ctx, workflow.ReviewReport{
		ID: reportID, StageRunID: stageID, SupervisorKey: "supervisor.story_skeleton",
		RulesetVersion: "rules-1", Score: &score, Grade: workflow.GradeC, Passed: false,
		Severity: workflow.SeverityMajor, RecommendedAction: "fix", Summary: "the turn is missing",
		CreatedAt: now,
	}, []workflow.ReviewIssue{
		{
			ID: mustNewID(t, generator), ReviewReportID: reportID, Rule: "turning_point_present",
			Severity: workflow.SeverityMajor, EntityType: "story_skeleton_version", EntityID: "ssv-1",
			Field: "turning_points_json", Problem: "no turn", Suggestion: "add one", AutoFixable: true,
			Status: workflow.IssueOpen, CreatedAt: now,
		},
		{
			// A second later than the first, so the read path's order is a fact
			// the fixture states rather than a tie broken by a generated id.
			ID: mustNewID(t, generator), ReviewReportID: reportID, Rule: "climax_present",
			Severity: workflow.SeverityMinor, Location: "climax", Problem: "thin", Suggestion: "thicken",
			Status: workflow.IssueOpen, CreatedAt: now.Add(time.Second),
		},
	}); err != nil {
		t.Fatalf("CreateReport: %v", err)
	}
	stored, issues, found, err := repo.GetReportForStage(ctx, stageID)
	if err != nil || !found {
		t.Fatalf("GetReportForStage: found=%v err=%v", found, err)
	}
	if stored.ID != reportID || stored.SupervisorKey != "supervisor.story_skeleton" {
		t.Fatalf("report round trip: %+v", stored)
	}
	if stored.Score == nil || *stored.Score != score {
		t.Fatalf("score = %v, want %v", stored.Score, score)
	}
	if stored.Passed || stored.Severity != workflow.SeverityMajor || stored.Grade != workflow.GradeC {
		t.Fatalf("report verdict lost: %+v", stored)
	}
	if len(issues) != 2 {
		t.Fatalf("%d issues read back, want 2", len(issues))
	}
	if issues[0].Severity != workflow.SeverityMajor || !issues[0].AutoFixable || issues[0].EntityID != "ssv-1" {
		t.Fatalf("issue round trip: %+v", issues[0])
	}
	if issues[1].Location != "climax" || issues[1].AutoFixable {
		t.Fatalf("the second issue lost its shape: %+v", issues[1])
	}

	// A report with no score must come back with no score, not with zero. It is
	// written a minute later than the first so "newest" is a fact the fixture
	// states rather than a tie broken by a generated id.
	unscoredID := mustNewID(t, generator)
	if err := repo.CreateReport(ctx, workflow.ReviewReport{
		ID: unscoredID, StageRunID: stageID, Severity: workflow.SeverityNone,
		CreatedAt: now.Add(time.Minute),
	}, nil); err != nil {
		t.Fatal(err)
	}
	// The newest report is the one returned: it was written after the first.
	newest, _, found, err := repo.GetReportForStage(ctx, stageID)
	if err != nil || !found {
		t.Fatalf("GetReportForStage: found=%v err=%v", found, err)
	}
	if newest.ID != unscoredID {
		t.Fatalf("the newest report is %q, want %q", newest.ID, unscoredID)
	}
	if newest.Score != nil {
		t.Fatalf("an unscored report reads back with score %v", *newest.Score)
	}
	if _, _, found, err := repo.GetReportForStage(ctx, "no-such-stage"); err != nil || found {
		t.Fatalf("an unreviewed stage reported found=%v err=%v", found, err)
	}

	events, err := repo.ListEvents(ctx, runID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("%d events, want the run creation and the stage creation", len(events))
	}
	if events[0].EventType != "workflow_run_created" || events[1].EventType != "stage_run_created" {
		t.Fatalf("event order = %q, %q", events[0].EventType, events[1].EventType)
	}
	if events[1].StageRunID != stageID || events[1].ToStatus != string(workflow.StagePending) {
		t.Fatalf("stage event = %+v", events[1])
	}
	foreignKeysClean(t, db)
}

// TestWorkflowRepositoryTransitionWritesTheEventInTheSameTransaction covers
// PRD FR-100's "每次状态变化写入审计事件": the revision-guarded update and the
// event either both land or neither does.
func TestWorkflowRepositoryTransitionWritesTheEventInTheSameTransaction(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewWorkflowRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	now := dramaTime()

	runID := mustNewID(t, generator)
	dramaCreateRun(t, repo, runID, mustNewID(t, generator), 1)
	run, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}

	from := run.Status
	run.Status = workflow.RunRunning
	run.UpdatedAt = now.Add(time.Minute)
	eventID := mustNewID(t, generator)
	// The event is stamped a minute after the creation's, so the read path's
	// ordering (created_at, then id) is decided by the fixture rather than by
	// which identifier happened to sort first.
	if err := repo.UpdateRun(ctx, run, 1, workflow.WorkflowEvent{
		ID: eventID, WorkflowRunID: runID, EventType: "workflow_run_status_changed",
		FromStatus: string(from), ToStatus: string(workflow.RunRunning),
		ActorType: versioning.CreatedByUser, CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	updated, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != workflow.RunRunning || updated.Revision != 2 {
		t.Fatalf("after the transition: %+v", updated)
	}
	events, err := repo.ListEvents(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("%d events after one transition, want 2", len(events))
	}
	if events[1].FromStatus != string(from) || events[1].ToStatus != string(workflow.RunRunning) {
		t.Fatalf("transition event = %+v", events[1])
	}

	// A stale revision is a conflict, and neither the row nor an event moves.
	stale := updated
	stale.Status = workflow.RunCompleted
	before, err := repo.ListEvents(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	err = repo.UpdateRun(ctx, stale, 1, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, EventType: "workflow_run_status_changed",
		FromStatus: string(updated.Status), ToStatus: string(workflow.RunCompleted),
		ActorType: versioning.CreatedByUser, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("a stale revision was accepted")
	}
	if domainErr, ok := workflow.AsError(err); !ok || domainErr.Category != workflow.CategoryConflict {
		t.Fatalf("expected the domain's conflict, got %v", err)
	}
	after, err := repo.ListEvents(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("a refused transition wrote an event: %d events before, %d after", len(before), len(after))
	}
	reRead, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if reRead.Status != workflow.RunRunning || reRead.Revision != 2 {
		t.Fatalf("a refused transition changed the row: %+v", reRead)
	}

	// The stage half of the same guarantee.
	stageID := mustNewID(t, generator)
	if err := repo.CreateStage(ctx, workflow.StageRun{
		ID: stageID, WorkflowRunID: runID, Stage: "script_generation", Attempt: 1,
		Status: workflow.StagePending, CreatedAt: now, Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: stageID,
		EventType: "stage_run_created", ToStatus: string(workflow.StagePending),
		ActorType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	stage, err := repo.GetStage(ctx, stageID)
	if err != nil {
		t.Fatal(err)
	}
	stage.Status = workflow.StageRunning
	stage.StartedAt = now
	if err := repo.UpdateStage(ctx, stage, 1, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: stageID,
		EventType: "stage_run_status_changed", FromStatus: string(workflow.StagePending),
		ToStatus: string(workflow.StageRunning), ActorType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatalf("UpdateStage: %v", err)
	}
	updatedStage, err := repo.GetStage(ctx, stageID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedStage.Status != workflow.StageRunning || updatedStage.Revision != 2 || updatedStage.StartedAt.IsZero() {
		t.Fatalf("after the stage transition: %+v", updatedStage)
	}
	staleStage := updatedStage
	staleStage.Status = workflow.StagePassed
	err = repo.UpdateStage(ctx, staleStage, 1, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: stageID,
		EventType: "stage_run_status_changed", ToStatus: string(workflow.StagePassed),
		ActorType: versioning.CreatedByUser, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("a stale stage revision was accepted")
	}
	afterStage, err := repo.ListEvents(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterStage) != 4 {
		t.Fatalf("%d events, want two per family", len(afterStage))
	}
	foreignKeysClean(t, db)
}

// TestWorkflowRepositoryRunCreationIsAtomic proves a run whose event cannot be
// written is not stored either: the creation is one transaction, so the log
// cannot be missing a run's first state.
func TestWorkflowRepositoryRunCreationIsAtomic(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewWorkflowRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	now := dramaTime()

	runID := mustNewID(t, generator)
	// The event names a run that is not the one being created, so its foreign
	// key fails after the run's own insert has already run inside the
	// transaction.
	err := repo.CreateRun(ctx, workflow.WorkflowRun{
		ID: runID, ProjectID: "drama-project", WorkflowType: "episode_production",
		Status: workflow.RunPending, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: "no-such-run", EventType: "workflow_run_created",
		ToStatus: string(workflow.RunPending), ActorType: versioning.CreatedByUser, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("a run was stored with an event naming another run")
	}
	if _, err := repo.GetRun(ctx, runID); err == nil {
		t.Fatal("the failed creation left the run behind, so it was not atomic")
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM workflow_runs") != 1 {
		t.Fatal("the fixture's run is missing, so the count proves nothing")
	}
	foreignKeysClean(t, db)
}

// TestWorkflowRepositoryStoresGateDecisions covers the user decision row and
// the one rule the schema depends on: an unknown decision value is refused.
func TestWorkflowRepositoryStoresGateDecisions(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewWorkflowRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	now := dramaTime()

	runID := mustNewID(t, generator)
	dramaCreateRun(t, repo, runID, mustNewID(t, generator), 1)
	decisionID := mustNewID(t, generator)
	if err := repo.CreateDecision(ctx, workflow.UserGateDecision{
		ID: decisionID, WorkflowRunID: runID, StageRunID: "",
		Decision: workflow.GateSkip, IssueIDsJSON: "", Instruction: "move on",
		Reason: "the chapter is cut from this episode", CreatedByType: versioning.CreatedByUser,
		CreatedByID: "local-user", CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM user_gate_decisions WHERE id = '"+decisionID+"'") != 1 {
		t.Fatal("the decision was not stored")
	}
	// The reason and instruction survive, and the author is stored where the
	// schema keeps it.
	if got := queryText(t, db, "SELECT reason FROM user_gate_decisions WHERE id = ?", decisionID); got == "" {
		t.Fatal("the decision lost its reason")
	}
	if got := queryText(t, db, "SELECT created_by FROM user_gate_decisions WHERE id = ?", decisionID); got == "" {
		t.Fatal("the decision lost its author")
	}
	// An undocumented decision value is refused by the schema's CHECK, which is
	// the last line of defence behind the domain's own vocabulary.
	err := repo.CreateDecision(ctx, workflow.UserGateDecision{
		ID: mustNewID(t, generator), WorkflowRunID: runID, Decision: "maybe",
		CreatedByType: versioning.CreatedByUser, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("an undocumented decision value was stored")
	}
	// A decision for a run that does not exist is refused by the foreign key.
	err = repo.CreateDecision(ctx, workflow.UserGateDecision{
		ID: mustNewID(t, generator), WorkflowRunID: "no-such-run", Decision: workflow.GateApprove,
		CreatedByType: versioning.CreatedByUser, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("a decision was stored for a run that does not exist")
	}
	foreignKeysClean(t, db)
}

// TestWorkflowRepositoryRejectsUnknownParents proves the foreign keys are
// reported as domain errors rather than raw driver messages.
func TestWorkflowRepositoryRejectsUnknownParents(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewWorkflowRepository(db)
	ctx := context.Background()
	now := dramaTime()
	missing := "00000000-0000-7000-8000-000000000002"

	// A run in a project that does not exist.
	err := repo.CreateRun(ctx, workflow.WorkflowRun{
		ID: mustNewID(t, dramaIDGenerator()), ProjectID: missing, WorkflowType: "episode_production",
		Status: workflow.RunPending, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, dramaIDGenerator()), WorkflowRunID: missing, EventType: "workflow_run_created",
		ToStatus: string(workflow.RunPending), ActorType: versioning.CreatedByUser, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("a run was created in a project that does not exist")
	}
	if domainErr, ok := workflow.AsError(err); !ok || domainErr.Category != workflow.CategoryInvalidInput {
		t.Fatalf("expected the domain's invalid_input, got %v", err)
	}
	// A stage under a run that does not exist.
	if err := repo.CreateStage(ctx, workflow.StageRun{
		ID: mustNewID(t, dramaIDGenerator()), WorkflowRunID: missing, Stage: "story_skeleton",
		Attempt: 1, Status: workflow.StagePending, CreatedAt: now, Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, dramaIDGenerator()), WorkflowRunID: missing, EventType: "stage_run_created",
		ToStatus: string(workflow.StagePending), ActorType: versioning.CreatedByUser, CreatedAt: now,
	}); err == nil {
		t.Fatal("a stage was created under a run that does not exist")
	}
	// A report for a stage that does not exist.
	if err := repo.CreateReport(ctx, workflow.ReviewReport{
		ID: mustNewID(t, dramaIDGenerator()), StageRunID: missing, Severity: workflow.SeverityNone,
		CreatedAt: now,
	}, nil); err == nil {
		t.Fatal("a report was created for a stage that does not exist")
	}
	// Reading a missing run is a not-found.
	if _, err := repo.GetRun(ctx, missing); err == nil {
		t.Fatal("reading a missing run succeeded")
	} else if domainErr, ok := workflow.AsError(err); !ok || domainErr.Category != workflow.CategoryNotFound {
		t.Fatalf("expected not_found, got %v", err)
	}
	foreignKeysClean(t, db)
}

// TestWorkflowRepositoryRefusesAnUndocumentedStatus proves the schema's CHECK
// is enforced through the write path, so a status the domain's machine does not
// know cannot be stored even by a caller that bypassed the service.
func TestWorkflowRepositoryRefusesAnUndocumentedStatus(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewWorkflowRepository(db)
	ctx := context.Background()
	now := dramaTime()

	err := repo.CreateRun(ctx, workflow.WorkflowRun{
		ID: mustNewID(t, dramaIDGenerator()), ProjectID: "drama-project",
		WorkflowType: "episode_production", Status: "ready", CreatedAt: now, UpdatedAt: now, Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, dramaIDGenerator()), WorkflowRunID: "drama-run", EventType: "workflow_run_created",
		ActorType: versioning.CreatedByUser, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("the superseded 'ready' spelling was stored as a run status")
	}
	err = repo.CreateStage(ctx, workflow.StageRun{
		ID: mustNewID(t, dramaIDGenerator()), WorkflowRunID: "drama-run", Stage: "story_skeleton",
		Attempt: 1, Status: "executed", CreatedAt: now, Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, dramaIDGenerator()), WorkflowRunID: "drama-run", EventType: "stage_run_created",
		ActorType: versioning.CreatedByUser, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("the superseded 'executed' spelling was stored as a stage status")
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM workflow_runs") != 1 || queryInt(t, db, "SELECT COUNT(*) FROM stage_runs") != 0 {
		t.Fatal("a refused row was written")
	}
	foreignKeysClean(t, db)
}

// TestWorkflowRepositoryProjectsTheRunStagePointer is the repository half of the run's stage
// pointer: DOMAIN_MODEL section 11.1's `current_stage`/`active_stage_run_id`, which the
// Studio's runs table renders and which FR-100's restart-recovery reads to learn which
// attempt was in play.
//
// The rule itself is pinned in the domain (TestProjectRunStageIsThePointerRule). What this
// test pins is the WRITE PATH, because that is where the defect was: both columns existed,
// both were read, and nothing wrote them — `UpdateRun` carried them and its only production
// caller copied them from the row it had just read, so a run driven from its first stage to
// its last reported an empty stage. So each assertion here drives a real statement
// (CreateStage, UpdateStage, UpdateRun) and reads the run row back from SQLite.
//
// Three paths, and each one is a transition the E2E walk does not reach:
//
//   - CREATE alone must project, because an attempt the machine has created but not yet
//     started is exactly what a crash between the two leaves behind.
//   - an ENDED attempt must CLEAR the pointer, which is a write that no other statement in
//     the package performs.
//   - a RUN status change AFTER a stage moved must leave the pointer where the stage machine
//     put it, which is what the removed `SET current_stage = ?` used to break.
func TestWorkflowRepositoryProjectsTheRunStagePointer(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewWorkflowRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	now := dramaTime()

	runID := mustNewID(t, generator)
	dramaCreateRun(t, repo, runID, mustNewID(t, generator), 1)

	pointerOf := func() (workflow.StageName, string) {
		t.Helper()
		run, err := repo.GetRun(ctx, runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		return run.CurrentStage, run.ActiveStageRunID
	}
	if current, active := pointerOf(); current != "" || active != "" {
		t.Fatalf("a fresh run points at %q/%q, want nothing", current, active)
	}

	// --- create alone projects -------------------------------------------------
	stageID := mustNewID(t, generator)
	if err := repo.CreateStage(ctx, workflow.StageRun{
		ID: stageID, WorkflowRunID: runID, Stage: "storyboard_table", Attempt: 1,
		Status: workflow.StagePending, CreatedAt: now, Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: stageID,
		EventType: "stage_run_created", ToStatus: string(workflow.StagePending),
		ActorType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	if current, active := pointerOf(); current != "storyboard_table" || active != stageID {
		t.Fatalf("after creating the attempt the run points at %q/%q, want storyboard_table/%s",
			current, active, stageID)
	}

	// --- a status change moves it ---------------------------------------------
	stage, err := repo.GetStage(ctx, stageID)
	if err != nil {
		t.Fatal(err)
	}
	stage.Status = workflow.StageRunning
	stage.StartedAt = now
	if err := repo.UpdateStage(ctx, stage, 1, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: stageID,
		EventType: "stage_run_status_changed", FromStatus: string(workflow.StagePending),
		ToStatus: string(workflow.StageRunning), ActorType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatalf("UpdateStage to running: %v", err)
	}
	if current, active := pointerOf(); current != "storyboard_table" || active != stageID {
		t.Fatalf("a running attempt left the run at %q/%q, want storyboard_table/%s", current, active, stageID)
	}

	// --- a run transition leaves the stage machine's answer alone -------------
	//
	// This is the assertion the old `UpdateRun` failed. The run's status is the caller's to
	// move; the stage pointer is derived, and a read-modify-write of the run must not be able
	// to carry a stale copy of it back over the top.
	run, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = workflow.RunRunning
	run.UpdatedAt = now.Add(time.Minute)
	if err := repo.UpdateRun(ctx, run, run.Revision, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID,
		EventType: "workflow_run_status_changed", FromStatus: string(workflow.RunPending),
		ToStatus: string(workflow.RunRunning), ActorType: versioning.CreatedByUser,
		CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	if current, active := pointerOf(); current != "storyboard_table" || active != stageID {
		t.Fatalf("a run status change left the pointer at %q/%q, want storyboard_table/%s — the derived pointer is not this statement's to write",
			current, active, stageID)
	}
	// And the columns the caller DID move are the ones that changed, so the assertion above is
	// about the pointer rather than about the statement having silently done nothing.
	moved, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Status != workflow.RunRunning || moved.Revision != run.Revision+1 {
		t.Fatalf("the run status change did not land: %+v", moved)
	}

	// --- an ended attempt clears it -------------------------------------------
	stage, err = repo.GetStage(ctx, stageID)
	if err != nil {
		t.Fatal(err)
	}
	stage.Status = workflow.StageFailed
	stage.ErrorCode = "STAGE_INTERRUPTED"
	stage.FinishedAt = now.Add(2 * time.Minute)
	if err := repo.UpdateStage(ctx, stage, stage.Revision, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: stageID,
		EventType: "stage_run_status_changed", FromStatus: string(workflow.StageRunning),
		ToStatus: string(workflow.StageFailed), ActorType: versioning.CreatedByUser,
		CreatedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("UpdateStage to failed: %v", err)
	}
	if current, active := pointerOf(); current != "" || active != "" {
		t.Fatalf("the attempt that ended left the run pointing at %q/%q, so a recovery path would chase a finished attempt",
			current, active)
	}

	// --- and a newer attempt takes it back ------------------------------------
	//
	// A run whose stage has ended is BETWEEN stages rather than finished, which is the state a
	// retry does not need permission to leave. This is also the state transition the E2E walk
	// drives, which is why the two together cover it.
	retryID := mustNewID(t, generator)
	if err := repo.CreateStage(ctx, workflow.StageRun{
		ID: retryID, WorkflowRunID: runID, Stage: "storyboard_table", Attempt: 2,
		Status: workflow.StagePending, CreatedAt: now.Add(3 * time.Minute), Revision: 1,
	}, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: retryID,
		EventType: "stage_run_created", ToStatus: string(workflow.StagePending),
		ActorType: versioning.CreatedByUser, CreatedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateStage (retry): %v", err)
	}
	if current, active := pointerOf(); current != "storyboard_table" || active != retryID {
		t.Fatalf("the retry left the run at %q/%q, want storyboard_table/%s", current, active, retryID)
	}

	foreignKeysClean(t, db)
}

// TestWorkflowRunTransitionDoesNotRegressTheStagePointer is the INTERLEAVING that makes the
// pointer's single-writer rule load-bearing rather than tidy.
//
// `projectRunStage` deliberately does not bump the run's revision — a stage moving must not
// invalidate a user's pending pause or cancel — so a `TransitionRun` that read the row before
// a stage moved still holds a valid revision at its compare-and-swap. If that statement also
// carried `current_stage`/`active_stage_run_id` (as it did until this was fixed), it would
// write the copy it read back over the top, and the run would point at the PREVIOUS stage
// while the new attempt was executing. Nothing would report an error: the run's status moved,
// its revision moved, and only the derived pointer regressed.
//
// So the sequence is the production one — read, stage moves, write — rather than a synthetic
// corruption, and the assertion is that the derived pointer is the stage machine's answer and
// not the reader's.
func TestWorkflowRunTransitionDoesNotRegressTheStagePointer(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewWorkflowRepository(db)
	ctx := context.Background()
	generator := dramaIDGenerator()
	now := dramaTime()

	runID := mustNewID(t, generator)
	dramaCreateRun(t, repo, runID, mustNewID(t, generator), 1)

	stage := func(stageName workflow.StageName, attempt int, at time.Time) workflow.StageRun {
		t.Helper()
		id := mustNewID(t, generator)
		if err := repo.CreateStage(ctx, workflow.StageRun{
			ID: id, WorkflowRunID: runID, Stage: stageName, Attempt: attempt,
			Status: workflow.StagePending, CreatedAt: at, Revision: 1,
		}, workflow.WorkflowEvent{
			ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: id,
			EventType: "stage_run_created", ToStatus: string(workflow.StagePending),
			ActorType: versioning.CreatedByUser, CreatedAt: at,
		}); err != nil {
			t.Fatalf("CreateStage(%s): %v", stageName, err)
		}
		record, err := repo.GetStage(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return record
	}

	first := stage("storyboard_table", 1, now)

	// --- the read that goes stale --------------------------------------------
	//
	// This is what `TransitionRun` holds between its `GetRun` and its `UpdateRun`: a run whose
	// pointer names the stage in play at the moment of the read.
	inFlight, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if inFlight.CurrentStage != "storyboard_table" || inFlight.ActiveStageRunID != first.ID {
		t.Fatalf("the run read for the transition points at %q/%q, want storyboard_table/%s",
			inFlight.CurrentStage, inFlight.ActiveStageRunID, first.ID)
	}

	// --- a stage moves under the reader --------------------------------------
	finished := first
	finished.Status = workflow.StagePassed
	finished.FinishedAt = now.Add(time.Minute)
	if err := repo.UpdateStage(ctx, finished, first.Revision, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID, StageRunID: first.ID,
		EventType: "stage_run_status_changed", FromStatus: string(workflow.StagePending),
		ToStatus: string(workflow.StagePassed), ActorType: versioning.CreatedByUser,
		CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("UpdateStage to passed: %v", err)
	}
	second := stage("storyboard_panel_generation", 1, now.Add(2*time.Minute))
	if after, err := repo.GetRun(ctx, runID); err != nil {
		t.Fatal(err)
	} else if after.CurrentStage != "storyboard_panel_generation" || after.ActiveStageRunID != second.ID {
		t.Fatalf("the run points at %q/%q after the next stage began, want storyboard_panel_generation/%s",
			after.CurrentStage, after.ActiveStageRunID, second.ID)
	}

	// --- and the transition's write must not undo it -------------------------
	//
	// The revision the reader holds is still current, which is the whole point: the pointer
	// write above does not touch it. So this succeeds, and what it writes is the caller's own
	// columns.
	inFlight.Status = workflow.RunRunning
	inFlight.UpdatedAt = now.Add(3 * time.Minute)
	if err := repo.UpdateRun(ctx, inFlight, inFlight.Revision, workflow.WorkflowEvent{
		ID: mustNewID(t, generator), WorkflowRunID: runID,
		EventType: "workflow_run_status_changed", FromStatus: string(workflow.RunPending),
		ToStatus: string(workflow.RunRunning), ActorType: versioning.CreatedByUser,
		CreatedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	after, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != workflow.RunRunning {
		t.Fatalf("the run status change did not land: %+v", after)
	}
	if after.CurrentStage != "storyboard_panel_generation" || after.ActiveStageRunID != second.ID {
		t.Fatalf("the run transition moved the stage pointer back to %q/%q; the pointer is derived from the attempts and must not be written by a read-modify-write of the run, which holds a copy from before the stage moved",
			after.CurrentStage, after.ActiveStageRunID)
	}

	foreignKeysClean(t, db)
}
