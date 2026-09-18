package workflow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// testClock is a deterministic clock.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(value time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(value)
}

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
	if value <= 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// memoryRepo is an in-memory double for the workflow repositories.
//
// It reproduces the revision-CAS semantics the real store enforces and, more
// importantly for these tests, records writes only after every rule has passed:
// a command that the domain refused leaves no row here, which is exactly the
// property "a refused decision writes nothing" claims.
type memoryRepo struct {
	mu        sync.Mutex
	runs      map[string]workflow.WorkflowRun
	stages    map[string]workflow.StageRun
	reports   map[string][]workflow.ReviewReport
	issues    map[string][]workflow.ReviewIssue
	decisions []workflow.UserGateDecision
	events    []workflow.WorkflowEvent
	writeFail error
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		runs:    map[string]workflow.WorkflowRun{},
		stages:  map[string]workflow.StageRun{},
		reports: map[string][]workflow.ReviewReport{},
		issues:  map[string][]workflow.ReviewIssue{},
	}
}

func (r *memoryRepo) CreateRun(_ context.Context, record workflow.WorkflowRun, event workflow.WorkflowEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeFail != nil {
		return r.writeFail
	}
	if _, exists := r.runs[record.ID]; exists {
		return workflow.ConflictError("A run with that id already exists.")
	}
	r.runs[record.ID] = record
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepo) GetRun(_ context.Context, id string) (workflow.WorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.runs[id]
	if !ok {
		return workflow.WorkflowRun{}, workflow.NotFoundError()
	}
	return record, nil
}

func (r *memoryRepo) UpdateRun(_ context.Context, record workflow.WorkflowRun, expectedRevision int64, event workflow.WorkflowEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeFail != nil {
		return r.writeFail
	}
	stored, ok := r.runs[record.ID]
	if !ok {
		return workflow.NotFoundError()
	}
	if stored.Revision != expectedRevision {
		return workflow.ConflictError("This workflow run changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	r.runs[record.ID] = record
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepo) ListRuns(_ context.Context, projectID string) ([]workflow.WorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	runs := make([]workflow.WorkflowRun, 0)
	for _, record := range r.runs {
		if record.ProjectID == projectID {
			runs = append(runs, record)
		}
	}
	if len(runs) > 1 {
		// Newest first, matching the real read path.
		for index := 1; index < len(runs); index++ {
			for inner := index; inner > 0 && runs[inner].CreatedAt.After(runs[inner-1].CreatedAt); inner-- {
				runs[inner], runs[inner-1] = runs[inner-1], runs[inner]
			}
		}
	}
	return runs, nil
}

func (r *memoryRepo) CreateStage(_ context.Context, record workflow.StageRun, event workflow.WorkflowEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeFail != nil {
		return r.writeFail
	}
	for _, existing := range r.stages {
		if existing.WorkflowRunID == record.WorkflowRunID && existing.Stage == record.Stage && existing.Attempt == record.Attempt {
			return workflow.ConflictError("That stage attempt already exists.")
		}
	}
	r.stages[record.ID] = record
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepo) GetStage(_ context.Context, id string) (workflow.StageRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.stages[id]
	if !ok {
		return workflow.StageRun{}, workflow.NotFoundError()
	}
	return record, nil
}

func (r *memoryRepo) UpdateStage(_ context.Context, record workflow.StageRun, expectedRevision int64, event workflow.WorkflowEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeFail != nil {
		return r.writeFail
	}
	stored, ok := r.stages[record.ID]
	if !ok {
		return workflow.NotFoundError()
	}
	if stored.Revision != expectedRevision {
		return workflow.ConflictError("This stage attempt changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	r.stages[record.ID] = record
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepo) ListStages(_ context.Context, workflowRunID string) ([]workflow.StageRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stages := make([]workflow.StageRun, 0)
	for _, record := range r.stages {
		if record.WorkflowRunID == workflowRunID {
			stages = append(stages, record)
		}
	}
	return stages, nil
}

func (r *memoryRepo) CreateReport(_ context.Context, report workflow.ReviewReport, issues []workflow.ReviewIssue) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeFail != nil {
		return r.writeFail
	}
	r.reports[report.StageRunID] = append(r.reports[report.StageRunID], report)
	r.issues[report.ID] = append(r.issues[report.ID], issues...)
	return nil
}

func (r *memoryRepo) GetReportForStage(_ context.Context, stageRunID string) (workflow.ReviewReport, []workflow.ReviewIssue, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reports := r.reports[stageRunID]
	if len(reports) == 0 {
		return workflow.ReviewReport{}, nil, false, nil
	}
	newest := reports[len(reports)-1]
	for _, candidate := range reports {
		if candidate.CreatedAt.After(newest.CreatedAt) {
			newest = candidate
		}
	}
	return newest, append([]workflow.ReviewIssue{}, r.issues[newest.ID]...), true, nil
}

func (r *memoryRepo) CreateDecision(_ context.Context, decision workflow.UserGateDecision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeFail != nil {
		return r.writeFail
	}
	r.decisions = append(r.decisions, decision)
	return nil
}

func (r *memoryRepo) ListEvents(_ context.Context, workflowRunID string) ([]workflow.WorkflowEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := make([]workflow.WorkflowEvent, 0)
	for _, event := range r.events {
		if event.WorkflowRunID == workflowRunID {
			events = append(events, event)
		}
	}
	return events, nil
}

func (r *memoryRepo) decisionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.decisions)
}

func newTestService(repo *memoryRepo) *Service {
	return NewService(Options{
		Runs:      repo,
		Stages:    repo,
		Reviews:   repo,
		Decisions: repo,
		Events:    repo,
		Clock:     &testClock{now: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)},
		IDs:       &counterIDs{prefix: "id"},
	})
}

// TestAvailableIsFalseWithoutARepository proves the fail-closed rule.
func TestAvailableIsFalseWithoutARepository(t *testing.T) {
	service := NewService(Options{Clock: &testClock{}, IDs: &counterIDs{prefix: "id"}})
	if service.Available() {
		t.Fatal("a service with no repository reports available")
	}
	if _, err := service.CreateRun(context.Background(), CreateRunRequest{ProjectID: "p-1", WorkflowType: "episode_production"}); err == nil {
		t.Fatal("a command on an unattached service succeeded")
	}
	if _, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionRequest{}); err == nil {
		t.Fatal("a gate decision on an unattached service succeeded")
	}
	var nilService *Service
	if nilService.Available() {
		t.Fatal("a nil service reports available")
	}
}

// TestCreateRunRecordsTheCreationEvent covers PRD FR-100's "每次状态变化写入审计
// 事件" at its first state: a run cannot exist without the event that says so.
func TestCreateRunRecordsTheCreationEvent(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()

	run, err := service.CreateRun(ctx, CreateRunRequest{ProjectID: "p-1", EpisodeID: "ep-1", WorkflowType: "episode_production"})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.Status != workflow.RunPending {
		t.Fatalf("status = %q, want pending", run.Status)
	}
	events, err := service.ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("%d events after creation, want 1", len(events))
	}
	if events[0].EventType != EventRunCreated || events[0].ToStatus != string(workflow.RunPending) {
		t.Fatalf("creation event = %+v", events[0])
	}
	if events[0].ActorType != versioning.CreatedByUser {
		t.Fatalf("actor = %q, want the user default", events[0].ActorType)
	}
}

// TestCreateRunValidatesThroughTheDomain proves an unknown workflow type is
// refused by the domain rather than written.
func TestCreateRunValidatesThroughTheDomain(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	_, err := service.CreateRun(context.Background(), CreateRunRequest{ProjectID: "p-1"})
	if err == nil {
		t.Fatal("a run with no workflow type was accepted")
	}
	domainErr, ok := workflow.AsError(err)
	if !ok || domainErr.Category != workflow.CategoryInvalidInput {
		t.Fatalf("expected the domain's invalid_input, got %v", err)
	}
	if len(repo.runs) != 0 || len(repo.events) != 0 {
		t.Fatal("a refused run wrote a row")
	}
}

// TestTransitionRunRefusesAnIllegalEdge covers the state machine guard: the
// edge is decided against the stored status, and an illegal one is refused with
// the domain's conflict and no write.
func TestTransitionRunRefusesAnIllegalEdge(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()
	run, err := service.CreateRun(ctx, CreateRunRequest{ProjectID: "p-1", WorkflowType: "episode_production"})
	if err != nil {
		t.Fatal(err)
	}

	// pending -> completed is not an edge: a run that has executed nothing
	// cannot have finished.
	_, err = service.TransitionRun(ctx, TransitionRunRequest{RunID: run.ID, Status: workflow.RunCompleted, Revision: 1})
	if err == nil {
		t.Fatal("pending -> completed was accepted")
	}
	domainErr, ok := workflow.AsError(err)
	if !ok || domainErr.Category != workflow.CategoryConflict {
		t.Fatalf("expected the domain's conflict, got %v", err)
	}
	reRead, err := repo.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reRead.Status != workflow.RunPending || reRead.Revision != 1 {
		t.Fatalf("a refused transition wrote the run: %+v", reRead)
	}
	if len(repo.events) != 1 {
		t.Fatalf("a refused transition wrote an event (%d events)", len(repo.events))
	}

	// An unknown status is refused as input rather than as a conflict.
	if _, err := service.TransitionRun(ctx, TransitionRunRequest{RunID: run.ID, Status: "nonsense", Revision: 1}); err == nil {
		t.Fatal("an unrecognised status was accepted")
	}

	// pending -> running is a legal edge, and records from/to.
	updated, err := service.TransitionRun(ctx, TransitionRunRequest{RunID: run.ID, Status: workflow.RunRunning, Revision: 1})
	if err != nil {
		t.Fatalf("pending -> running was refused: %v", err)
	}
	if updated.Status != workflow.RunRunning || updated.Revision != 2 {
		t.Fatalf("updated run = %+v", updated)
	}
	events, err := service.ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("%d events after one transition, want 2", len(events))
	}
	last := events[1]
	if last.EventType != EventRunStatusChanged || last.FromStatus != string(workflow.RunPending) || last.ToStatus != string(workflow.RunRunning) {
		t.Fatalf("transition event = %+v", last)
	}

	// A stale revision is refused by the compare-and-swap.
	if _, err := service.TransitionRun(ctx, TransitionRunRequest{RunID: run.ID, Status: workflow.RunWaitingUser, Revision: 1}); err == nil {
		t.Fatal("a stale revision was accepted")
	}
}

// TestTransitionRunStampsCompletion covers completed_at: it is set on a
// terminal status and left zero otherwise.
func TestTransitionRunStampsCompletion(t *testing.T) {
	repo := newMemoryRepo()
	clock := &testClock{now: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}
	service := NewService(Options{
		Runs: repo, Stages: repo, Reviews: repo, Decisions: repo, Events: repo,
		Clock: clock, IDs: &counterIDs{prefix: "id"},
	})
	ctx := context.Background()
	run, err := service.CreateRun(ctx, CreateRunRequest{ProjectID: "p-1", WorkflowType: "episode_production"})
	if err != nil {
		t.Fatal(err)
	}
	running, err := service.TransitionRun(ctx, TransitionRunRequest{RunID: run.ID, Status: workflow.RunRunning, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !running.CompletedAt.IsZero() {
		t.Fatalf("a running run carries completed_at = %v", running.CompletedAt)
	}
	clock.advance(time.Hour)
	completed, err := service.TransitionRun(ctx, TransitionRunRequest{RunID: run.ID, Status: workflow.RunCompleted, Revision: 2})
	if err != nil {
		t.Fatal(err)
	}
	if completed.CompletedAt.IsZero() {
		t.Fatal("a completed run has no completed_at")
	}
	if want := clock.Now(); !completed.CompletedAt.Equal(want) {
		t.Fatalf("completed_at = %v, want %v", completed.CompletedAt, want)
	}
}

// TestCreateStageValidatesTheStageName covers the one rule ValidateStageName
// makes: a stage key must be non-empty and within the schema's length.
func TestCreateStageValidatesTheStageName(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()
	run, err := service.CreateRun(ctx, CreateRunRequest{ProjectID: "p-1", WorkflowType: "episode_production"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateStage(ctx, CreateStageRequest{WorkflowRunID: run.ID, Stage: "  ", Attempt: 1}); err == nil {
		t.Fatal("an empty stage name was accepted")
	}
	// The value set is deliberately open (FR-100 and AGENT_CONTRACTS 10.1 give
	// different key lists), so a name from neither list is still a valid key.
	stage, err := service.CreateStage(ctx, CreateStageRequest{WorkflowRunID: run.ID, Stage: "a_stage_of_our_own", Attempt: 1})
	if err != nil {
		t.Fatalf("a documented-bounds stage key was refused: %v", err)
	}
	if stage.Status != workflow.StagePending || stage.Attempt != 1 {
		t.Fatalf("created stage = %+v", stage)
	}
	if _, err := service.CreateStage(ctx, CreateStageRequest{WorkflowRunID: "missing", Stage: "story_skeleton", Attempt: 1}); err == nil {
		t.Fatal("a stage was created under a run that does not exist")
	}
	events, err := service.ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.EventType == EventStageCreated && event.StageRunID == stage.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the stage creation wrote no event")
	}
}

// TestTransitionStageRefusesAnIllegalEdge covers the stage state machine: the
// illegal edge is refused against the stored status, and the legal one records
// started_at.
func TestTransitionStageRefusesAnIllegalEdge(t *testing.T) {
	repo := newMemoryRepo()
	clock := &testClock{now: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}
	service := NewService(Options{
		Runs: repo, Stages: repo, Reviews: repo, Decisions: repo, Events: repo,
		Clock: clock, IDs: &counterIDs{prefix: "id"},
	})
	ctx := context.Background()
	stage := seedStage(t, service, "story_skeleton", 1)

	// pending -> passed skips the whole execution and review the domain's
	// machine requires.
	_, err := service.TransitionStage(ctx, TransitionStageRequest{StageRunID: stage.ID, Status: workflow.StagePassed, Revision: 1})
	if err == nil {
		t.Fatal("pending -> passed was accepted")
	}
	domainErr, ok := workflow.AsError(err)
	if !ok || domainErr.Category != workflow.CategoryConflict {
		t.Fatalf("expected the domain's conflict, got %v", err)
	}
	reRead, err := repo.GetStage(ctx, stage.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reRead.Status != workflow.StagePending || reRead.Revision != 1 {
		t.Fatalf("a refused transition wrote the stage: %+v", reRead)
	}

	running, err := service.TransitionStage(ctx, TransitionStageRequest{StageRunID: stage.ID, Status: workflow.StageRunning, Revision: 1})
	if err != nil {
		t.Fatalf("pending -> running was refused: %v", err)
	}
	if running.StartedAt.IsZero() {
		t.Fatal("a running attempt has no started_at")
	}
	if !running.FinishedAt.IsZero() {
		t.Fatal("a running attempt already has a finished_at")
	}

	// passed is not terminal in the domain's sense (it may still be
	// superseded), so reaching it must not stamp finished_at.
	reviewing, err := service.TransitionStage(ctx, TransitionStageRequest{StageRunID: stage.ID, Status: workflow.StageReviewing, Revision: 2})
	if err != nil {
		t.Fatal(err)
	}
	passed, err := service.TransitionStage(ctx, TransitionStageRequest{StageRunID: stage.ID, Status: workflow.StagePassed, Revision: reviewing.Revision})
	if err != nil {
		t.Fatalf("reviewing -> passed was refused: %v", err)
	}
	if !passed.FinishedAt.IsZero() {
		t.Fatalf("a passed attempt has finished_at = %v, but it can still be superseded", passed.FinishedAt)
	}

	// passed -> superseded is the one edge out of passed, and superseded is
	// terminal, so that is where finished_at belongs.
	superseded, err := service.TransitionStage(ctx, TransitionStageRequest{StageRunID: stage.ID, Status: workflow.StageSuperseded, Revision: passed.Revision})
	if err != nil {
		t.Fatalf("passed -> superseded was refused: %v", err)
	}
	if superseded.FinishedAt.IsZero() {
		t.Fatal("a superseded attempt has no finished_at")
	}
	if _, err := service.TransitionStage(ctx, TransitionStageRequest{StageRunID: stage.ID, Status: workflow.StageRunning, Revision: superseded.Revision}); err == nil {
		t.Fatal("superseded -> running was accepted")
	}
}

// TestRecordReviewWritesReportAndIssuesTogether covers FR-110's report shape:
// the findings are stored with the report they belong to, and the domain's own
// refusal (a passing report naming a critical severity) is honoured.
func TestRecordReviewWritesReportAndIssuesTogether(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()
	stage := seedStage(t, service, "story_skeleton", 1)

	score := 0.5
	report, issues, err := service.RecordReview(ctx, RecordReviewRequest{
		StageRunID:     stage.ID,
		SupervisorKey:  "supervisor.story_skeleton",
		RulesetVersion: "rules-1",
		Score:          &score,
		Grade:          workflow.GradeC,
		Passed:         false,
		Severity:       workflow.SeverityMajor,
		Summary:        "the skeleton drops the act two turn",
		Issues: []ReviewIssueInput{
			{Severity: workflow.SeverityMajor, EntityType: "story_skeleton_version", EntityID: "ssv-1", Field: "climax", Problem: "no climax", Suggestion: "add one", AutoFixable: true},
			{Severity: workflow.SeverityMinor, Location: "turning_points[2]", Problem: "thin", Suggestion: "thicken"},
		},
	})
	if err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
	if report.ID == "" || report.StageRunID != stage.ID {
		t.Fatalf("report = %+v", report)
	}
	if report.Score == nil || *report.Score != score {
		t.Fatalf("score = %v, want %v", report.Score, score)
	}
	if len(issues) != 2 {
		t.Fatalf("%d issues returned, want 2", len(issues))
	}
	for _, issue := range issues {
		if issue.ReviewReportID != report.ID {
			t.Fatalf("issue %q does not belong to the report", issue.ID)
		}
		if issue.Status != workflow.IssueOpen {
			t.Fatalf("issue status = %q, want open", issue.Status)
		}
	}
	stored, storedIssues, err := service.GetReviewReport(ctx, stage.ID)
	if err != nil {
		t.Fatalf("GetReviewReport: %v", err)
	}
	if stored.ID != report.ID || len(storedIssues) != 2 {
		t.Fatalf("read back report %+v with %d issues", stored, len(storedIssues))
	}

	// AGENT_CONTRACTS 7.6: a passing report may not name a major or critical
	// severity, and the refusal writes nothing.
	if _, _, err := service.RecordReview(ctx, RecordReviewRequest{
		StageRunID: stage.ID, Passed: true, Severity: workflow.SeverityCritical,
	}); err == nil {
		t.Fatal("a passing report with a critical severity was accepted")
	}
	if len(repo.reports[stage.ID]) != 1 {
		t.Fatalf("%d reports stored, want the one that succeeded", len(repo.reports[stage.ID]))
	}

	// An issue that names neither an entity nor a location is refused too.
	if _, _, err := service.RecordReview(ctx, RecordReviewRequest{
		StageRunID: stage.ID, Severity: workflow.SeverityMinor,
		Issues: []ReviewIssueInput{{Severity: workflow.SeverityMinor, Problem: "somewhere"}},
	}); err == nil {
		t.Fatal("an unaddressable issue was accepted")
	}
	// The report it refused was not written either: nothing half-stored.
	if len(repo.reports[stage.ID]) != 1 {
		t.Fatal("the refused report was stored")
	}

	if _, _, err := service.GetReviewReport(ctx, "missing"); err == nil {
		t.Fatal("a report was read for a stage attempt that has none")
	}
}

// TestSubmitGateDecisionRefusesAnAgentCreator is the section 11.5 rule: "用户
// 决策不可由 Agent 伪造". The refusal comes from the domain, and the row is not
// written.
func TestSubmitGateDecisionRefusesAnAgentCreator(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()
	stage := seedStage(t, service, "story_skeleton", 1)

	_, err := service.SubmitGateDecision(ctx, SubmitGateDecisionRequest{
		WorkflowRunID: stage.WorkflowRunID,
		StageRunID:    stage.ID,
		Decision:      workflow.GateApprove,
		CreatedBy:     versioning.CreatedByAgent,
		CreatedByID:   "agent-1",
	})
	if err == nil {
		t.Fatal("an agent-authored gate decision was accepted")
	}
	domainErr, ok := workflow.AsError(err)
	if !ok || domainErr.Category != workflow.CategoryConflict {
		t.Fatalf("expected the domain's conflict, got %v", err)
	}
	if repo.decisionCount() != 0 {
		t.Fatalf("%d decisions written by a refused call, want 0", repo.decisionCount())
	}

	// The user's own decision is accepted, and carries the author through.
	decision, err := service.SubmitGateDecision(ctx, SubmitGateDecisionRequest{
		WorkflowRunID: stage.WorkflowRunID,
		StageRunID:    stage.ID,
		Decision:      workflow.GateApprove,
		CreatedBy:     versioning.CreatedByUser,
		CreatedByID:   "local-user",
	})
	if err != nil {
		t.Fatalf("SubmitGateDecision: %v", err)
	}
	if decision.CreatedByType != versioning.CreatedByUser || decision.CreatedByID != "local-user" {
		t.Fatalf("stored decision author = %q/%q", decision.CreatedByType, decision.CreatedByID)
	}
	if repo.decisionCount() != 1 {
		t.Fatalf("%d decisions after the accepted call, want 1", repo.decisionCount())
	}
}

// TestSubmitGateDecisionRequiresReasonsForSkipAndWaive covers the two decisions
// whose whole content is the justification for not doing the work: PRD FR-100's
// "跳过阶段需记录原因" and section 15.3's waiver reason.
func TestSubmitGateDecisionRequiresReasonsForSkipAndWaive(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo)
	ctx := context.Background()

	for _, decision := range []workflow.GateDecision{workflow.GateSkip, workflow.GateWaive} {
		_, err := service.SubmitGateDecision(ctx, SubmitGateDecisionRequest{
			WorkflowRunID: "run-1",
			Decision:      decision,
			CreatedBy:     versioning.CreatedByUser,
		})
		if err == nil {
			t.Fatalf("%s with no reason was accepted", decision)
		}
		domainErr, ok := workflow.AsError(err)
		if !ok || domainErr.Category != workflow.CategoryInvalidInput {
			t.Fatalf("%s: expected the domain's invalid_input, got %v", decision, err)
		}
	}
	if repo.decisionCount() != 0 {
		t.Fatalf("a refused decision wrote %d rows", repo.decisionCount())
	}
	if _, err := service.SubmitGateDecision(ctx, SubmitGateDecisionRequest{
		WorkflowRunID: "run-1", Decision: workflow.GateSkip, Reason: "the chapter is not needed in this cut",
		CreatedBy: versioning.CreatedByUser,
	}); err != nil {
		t.Fatalf("a justified skip was refused: %v", err)
	}
	if repo.decisionCount() != 1 {
		t.Fatalf("%d decisions written, want 1", repo.decisionCount())
	}
	// An unrecognised decision is refused as input.
	if _, err := service.SubmitGateDecision(ctx, SubmitGateDecisionRequest{
		WorkflowRunID: "run-1", Decision: "maybe", CreatedBy: versioning.CreatedByUser,
	}); err == nil {
		t.Fatal("an unrecognised decision was accepted")
	}
}

// TestListQueriesRequireTheStore proves the read paths fail closed too.
func TestListQueriesRequireTheStore(t *testing.T) {
	service := NewService(Options{})
	for name, call := range map[string]func() error{
		"ListRuns": func() error { _, err := service.ListRuns(context.Background(), "p-1"); return err },
		"ListStages": func() error {
			_, err := service.ListStages(context.Background(), "run-1")
			return err
		},
		"ListEvents": func() error {
			_, err := service.ListEvents(context.Background(), "run-1")
			return err
		},
		"GetReviewReport": func() error {
			_, _, err := service.GetReviewReport(context.Background(), "stage-1")
			return err
		},
	} {
		if err := call(); err == nil {
			t.Fatalf("%s on an unattached service succeeded", name)
		}
	}
}

// seedStage creates a run and one stage attempt through the service.
func seedStage(t *testing.T, service *Service, name workflow.StageName, attempt int) workflow.StageRun {
	t.Helper()
	ctx := context.Background()
	run, err := service.CreateRun(ctx, CreateRunRequest{ProjectID: "p-1", WorkflowType: "episode_production"})
	if err != nil {
		t.Fatal(err)
	}
	stage, err := service.CreateStage(ctx, CreateStageRequest{WorkflowRunID: run.ID, Stage: name, Attempt: attempt})
	if err != nil {
		t.Fatal(err)
	}
	return stage
}
