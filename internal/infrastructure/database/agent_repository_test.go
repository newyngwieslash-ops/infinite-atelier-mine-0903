package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	memoryapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// These tests exercise the agent repository against real SQLite, because what they
// are about IS the schema: the four tables of migration 000015, the foreign keys that
// make a citation real, the unique pair that keeps a replayed tool call out, the
// CHECK that forces a denial to carry a reason, and the structured scope columns that
// make a recall's isolation a filter rather than a string prefix.
//
// An in-memory double would prove none of that, which is why there is none here.
//
// The fixture is built on dramaRepoHandle and dramaSeedParents — the helpers the
// WP-05 repository tests already use — so a column this package's schema requires is
// seeded exactly once, in one place, and cannot drift between two test files.

const (
	agentTestProject = "drama-project"
	agentTestRun     = "drama-run"
	agentTestStage   = "agent-stage-1"
	agentTestSkill   = "agent-skill-version-1"
	agentTestSecond  = "agent-project-2"
	agentStamp       = "2026-01-01T00:00:00Z"
)

// agentFixture is a migrated database with the rows a run cites.
type agentFixture struct {
	db      *sql.DB
	repo    *AgentRepository
	skillID string
	runID   string
	stageID string
	project string
}

// newAgentFixture seeds one stage attempt and one skill version on top of the drama
// fixture's workspace, project and workflow run.
func newAgentFixture(t *testing.T) *agentFixture {
	t.Helper()
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	statements := []string{
		fmt.Sprintf(`INSERT INTO stage_runs (id, workflow_run_id, stage, attempt, status, created_at)
			VALUES ('%s', '%s', 'chapter_event_extraction', 1, 'running', '%s')`,
			agentTestStage, agentTestRun, agentStamp),
		fmt.Sprintf(`INSERT INTO skill_versions (id, skill_key, version, content_hash, status, created_at)
			VALUES ('%s', 'script', '1.0.0', 'hash-1', 'active', '%s')`, agentTestSkill, agentStamp),
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the agent fixture failed: %v\n%s", err, statement)
		}
	}
	return &agentFixture{
		db: db, repo: NewAgentRepository(db),
		skillID: agentTestSkill, runID: agentTestRun,
		stageID: agentTestStage, project: agentTestProject,
	}
}

// seedSecondProject writes a second project, so a recall that crosses a boundary has
// somewhere to cross into.
func (f *agentFixture) seedSecondProject(t *testing.T) {
	t.Helper()
	_, err := f.db.ExecContext(context.Background(), fmt.Sprintf(`INSERT INTO projects
		(id, workspace_id, project_type, name, language, status, created_at, updated_at, revision)
		VALUES ('%s', 'drama-ws', 'drama', 'Other', 'zh-CN', 'active', '%s', '%s', 1)`,
		agentTestSecond, agentStamp, agentStamp))
	if err != nil {
		t.Fatalf("seeding the second project: %v", err)
	}
}

// seedAgentRun writes a minimal run row for a test that needs a parent for messages
// but is not about the repository's CreateRun.
func (f *agentFixture) seedAgentRun(t *testing.T, runID, projectID string) {
	t.Helper()
	_, err := f.db.ExecContext(context.Background(), fmt.Sprintf(`INSERT INTO agent_runs
		(id, project_id, agent_layer, agent_key, skill_version_id, status, started_at, created_at, updated_at)
		VALUES ('%s', '%s', 'execution', 'agent.x', '%s', 'running', '%s', '%s', '%s')`,
		runID, projectID, f.skillID, agentStamp, agentStamp, agentStamp))
	if err != nil {
		t.Fatalf("seeding agent run %s: %v", runID, err)
	}
}

// seedMessage writes one message row directly, for tests that are about the READ path
// and want to control the scope columns precisely.
func (f *agentFixture) seedMessage(t *testing.T, id, runID, scopeKey, projectID, agentKey, content string, offset int) {
	t.Helper()
	_, err := f.db.ExecContext(context.Background(), fmt.Sprintf(`INSERT INTO agent_messages
		(id, agent_run_id, scope_key, scope_tenant, scope_workspace, scope_project,
		 scope_episode, scope_agent_key, scope_session, role, content, created_at)
		VALUES ('%s', '%s', '%s', 'local', '', '%s', '', '%s', '', 'user', '%s', ?)`,
		id, runID, scopeKey, projectID, agentKey, content),
		time.Date(2026, 1, 1, 0, 0, offset, 0, time.UTC).Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seeding message %s: %v", id, err)
	}
}

// sampleRun builds a run the fixture's parents accept.
func (f *agentFixture) sampleRun(id string) agent.AgentRun {
	return agent.AgentRun{
		ID: id, ProjectID: f.project, WorkflowRunID: f.runID, StageRunID: f.stageID,
		Layer: agent.LayerExecution, AgentKey: "script.execution.event_extraction",
		ModelConfigID: "model-1", SkillVersionID: f.skillID,
		Status: agent.RunRunning, InputSummary: "agent=script.execution.event_extraction",
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Revision: 1,
	}
}

// TestAgentRepositorySatisfiesItsPorts is the compile-time assertion made visible in
// the test list. The var block at the top of agent_repository.go is what enforces it;
// this exists so a reader of the test names sees which four ports the type is
// composed as.
func TestAgentRepositorySatisfiesItsPorts(t *testing.T) {
	fixture := newAgentFixture(t)
	var store agentruntime.RunStore = fixture.repo
	var counter agentruntime.RevisionCounter = fixture.repo
	var locator agentruntime.StageRunLocator = fixture.repo
	var memory memoryapp.Store = fixture.repo
	if store == nil || counter == nil || locator == nil || memory == nil {
		t.Fatal("the repository does not satisfy one of its ports")
	}
}

// TestAgentRepositoryRoundTripsARun is the basic contract: what goes in comes back.
func TestAgentRepositoryRoundTripsARun(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	run := fixture.sampleRun("run-a")
	if err := fixture.repo.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	read, err := fixture.repo.GetRun(ctx, "run-a")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if read.ID != run.ID || read.AgentKey != run.AgentKey || read.Layer != run.Layer {
		t.Fatalf("the run came back as %+v", read)
	}
	if read.Status != agent.RunRunning {
		t.Fatalf("the status is %q", read.Status)
	}
	if read.SkillVersionID != fixture.skillID {
		t.Fatalf("the skill version is %q", read.SkillVersionID)
	}
	if !read.StartedAt.Equal(run.StartedAt) {
		t.Fatalf("the start time is %v, want %v", read.StartedAt, run.StartedAt)
	}
	if read.Revision != 1 {
		t.Fatalf("a fresh run carries revision %d", read.Revision)
	}
	// A run that is not there is a not-found rather than an empty record: the caller
	// acts differently on the two.
	if _, err := fixture.repo.GetRun(ctx, "run-missing"); err == nil {
		t.Fatal("a missing run was read")
	} else if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryNotFound {
		t.Fatalf("a missing run reports %v", err)
	}
}

// TestAgentRepositoryRefusesARunCitingAnUnregisteredSkill covers the foreign key that
// makes section 4.2's version binding real.
//
// The refusal must name the missing referent, because "the run could not be saved"
// would send someone looking at the project rather than at the skill they forgot to
// register — and the two foreign keys on this table make that distinction the only
// thing that makes the message useful.
func TestAgentRepositoryRefusesARunCitingAnUnregisteredSkill(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	run := fixture.sampleRun("run-a")
	run.SkillVersionID = "skill-version-does-not-exist"
	err := fixture.repo.CreateRun(ctx, run)
	if err == nil {
		t.Fatal("a run citing an unregistered skill version was accepted")
	}
	if !strings.Contains(err.Error(), "skill version") {
		t.Fatalf("the refusal reads %q, which does not name the missing skill version", err)
	}
	// And nothing was written: a half-inserted run would be a record that disagrees
	// with itself.
	if _, readErr := fixture.repo.GetRun(ctx, "run-a"); readErr == nil {
		t.Fatal("the refused run was written anyway")
	}
}

// TestAgentRepositoryRefusesARunInAnUnknownProject covers the other foreign key, and
// the other half of the message's usefulness.
func TestAgentRepositoryRefusesARunInAnUnknownProject(t *testing.T) {
	fixture := newAgentFixture(t)
	run := fixture.sampleRun("run-a")
	run.ProjectID = "project-does-not-exist"
	err := fixture.repo.CreateRun(context.Background(), run)
	if err == nil {
		t.Fatal("a run in an unknown project was accepted")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Fatalf("the refusal reads %q, which does not name the missing project", err)
	}
}

// TestAgentRepositoryFinishRunIsRevisionGuarded is the optimistic-concurrency rule
// every repository here follows. A second writer holding the row it first read must
// not silently overwrite a run somebody else has closed.
func TestAgentRepositoryFinishRunIsRevisionGuarded(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	run := fixture.sampleRun("run-a")
	if err := fixture.repo.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	finished := run
	finished.Status = agent.RunSucceeded
	finished.ValidatedOutputJSON = `{"schemaVersion":1}`
	finished.FinishedAt = run.StartedAt.Add(time.Second)
	if err := fixture.repo.FinishRun(ctx, finished, 1); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	// The same revision again is stale now, because the first write incremented it.
	err := fixture.repo.FinishRun(ctx, finished, 1)
	if err == nil {
		t.Fatal("a stale revision overwrote a finished run")
	}
	if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryConflict {
		t.Fatalf("a stale revision reports %v, want a conflict", err)
	}
	read, err := fixture.repo.GetRun(ctx, "run-a")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if read.Revision != 2 {
		t.Fatalf("the revision is %d, want 2", read.Revision)
	}
	if read.Status != agent.RunSucceeded {
		t.Fatalf("the status is %q", read.Status)
	}
	// Finishing a run that is not there is a not-found, not a conflict: the caller
	// acts differently on the two.
	missing := fixture.sampleRun("run-missing")
	err = fixture.repo.FinishRun(ctx, missing, 1)
	if err == nil {
		t.Fatal("finishing a missing run succeeded")
	}
	if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryNotFound {
		t.Fatalf("finishing a missing run reports %v", err)
	}
}

// TestAgentRepositoryRecordsMessagesAndToolCalls covers the two child tables and the
// order they come back in.
func TestAgentRepositoryRecordsMessagesAndToolCalls(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	if err := fixture.repo.CreateRun(ctx, fixture.sampleRun("run-a")); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	scopeKey := "local||" + agentTestProject + "||script.execution.event_extraction|"
	for index, role := range []agent.MessageRole{agent.MessageAssistant, agent.MessageUser} {
		message := agent.AgentMessage{
			ID: "message-" + itoaTest(index), AgentRunID: "run-a",
			ScopeKey: scopeKey,
			Role:     role, Content: "content " + itoaTest(index), ContentHash: "hash",
			CreatedAt: base.Add(time.Duration(index) * time.Second),
		}
		if err := fixture.repo.RecordMessage(ctx, message); err != nil {
			t.Fatalf("RecordMessage: %v", err)
		}
	}
	messages, err := fixture.repo.ListMessages(ctx, "run-a")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("the run holds %d messages", len(messages))
	}
	if messages[0].Content != "content 0" {
		t.Fatalf("the messages came back out of order: %+v", messages)
	}

	calls := []agent.AgentToolCall{
		{ID: "call-1", AgentRunID: "run-a", Sequence: 0, ToolKey: "story.read_events",
			InputJSON: `{}`, Status: agent.ToolCallSucceed, OutputJSON: `{"ok":true}`,
			StartedAt: base, FinishedAt: base.Add(time.Second)},
		{ID: "call-2", AgentRunID: "run-a", Sequence: 1, ToolKey: "script.create_script_version",
			InputJSON: `{}`, Status: agent.ToolCallDenied, ErrorCode: "security.tool_not_allowed",
			StartedAt: base, FinishedAt: base},
	}
	for _, call := range calls {
		if err := fixture.repo.RecordToolCall(ctx, call); err != nil {
			t.Fatalf("RecordToolCall: %v", err)
		}
	}
	stored, err := fixture.repo.ListToolCalls(ctx, "run-a")
	if err != nil {
		t.Fatalf("ListToolCalls: %v", err)
	}
	if len(stored) != 2 || stored[0].Sequence != 0 || stored[1].Sequence != 1 {
		t.Fatalf("the tool calls came back as %+v", stored)
	}
	if stored[0].Status != agent.ToolCallSucceed || stored[0].OutputJSON != `{"ok":true}` {
		t.Fatalf("the succeeded call came back as %+v", stored[0])
	}
	if stored[1].Status != agent.ToolCallDenied || stored[1].ErrorCode != "security.tool_not_allowed" {
		t.Fatalf("the denied call came back as %+v", stored[1])
	}
}

// TestAgentRepositoryRefusesADuplicateToolCallPosition covers the unique pair that
// keeps a replay from inserting a second call at the same position. Without it a
// retried run would record its tool calls twice and the trail would misrepresent what
// happened.
func TestAgentRepositoryRefusesADuplicateToolCallPosition(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	if err := fixture.repo.CreateRun(ctx, fixture.sampleRun("run-a")); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	call := agent.AgentToolCall{
		ID: "call-1", AgentRunID: "run-a", Sequence: 0, ToolKey: "story.read_events",
		Status: agent.ToolCallSucceed, StartedAt: base, FinishedAt: base,
	}
	if err := fixture.repo.RecordToolCall(ctx, call); err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}
	duplicate := call
	duplicate.ID = "call-1-again"
	err := fixture.repo.RecordToolCall(ctx, duplicate)
	if err == nil {
		t.Fatal("a second tool call at the same position was accepted")
	}
	if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryConflict {
		t.Fatalf("a duplicate position reports %v, want a conflict", err)
	}
}

// TestAgentRepositoryRefusesADeniedCallWithNoCode covers the CHECK migration 000015
// declares: "status != 'denied' OR length(error_code) > 0".
//
// A denial with no reason is exactly the unattributable refusal SECURITY section 7.1
// forbids, so the schema refuses it — and the repository must not paper over that by
// writing a different status, which is what this asserts by checking the refusal
// happens at all.
func TestAgentRepositoryRefusesADeniedCallWithNoCode(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	if err := fixture.repo.CreateRun(ctx, fixture.sampleRun("run-a")); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err := fixture.repo.RecordToolCall(ctx, agent.AgentToolCall{
		ID: "call-1", AgentRunID: "run-a", Sequence: 0, ToolKey: "story.read_events",
		Status: agent.ToolCallDenied, StartedAt: base, FinishedAt: base,
	})
	if err == nil {
		t.Fatal("a denied call with no code was accepted")
	}
	// Nothing was written.
	calls, err := fixture.repo.ListToolCalls(ctx, "run-a")
	if err != nil {
		t.Fatalf("ListToolCalls: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("the refused call was written anyway: %+v", calls)
	}
}

// TestAgentRepositoryRefusesAChildOfAMissingRun covers the foreign keys on both child
// tables: a message or tool call for a run that does not exist is a not-found, because
// the caller's run id is stale.
func TestAgentRepositoryRefusesAChildOfAMissingRun(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err := fixture.repo.RecordMessage(ctx, agent.AgentMessage{
		ID: "message-1", AgentRunID: "run-missing", ScopeKey: "local||" + agentTestProject + "||key|",
		Role: agent.MessageUser, Content: "x", CreatedAt: base,
	})
	if err == nil {
		t.Fatal("a message for a missing run was accepted")
	}
	if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryNotFound {
		t.Fatalf("a message for a missing run reports %v, want a not-found", err)
	}
	err = fixture.repo.RecordToolCall(ctx, agent.AgentToolCall{
		ID: "call-1", AgentRunID: "run-missing", Sequence: 0, ToolKey: "story.read_events",
		Status: agent.ToolCallSucceed, StartedAt: base, FinishedAt: base,
	})
	if err == nil {
		t.Fatal("a tool call for a missing run was accepted")
	}
}

// TestAgentRepositoryRevisionCountReadsTheAuditTrail is the engine's budget source.
//
// It is the assertion that matters most for AC-AGENT-005's second half: a FIX or REDO
// reuses the attempt row, so the attempt COUNT cannot answer "how many revisions has
// this had" and a budget keyed on it could never trip. The count comes from the
// workflow_events trail, and this proves it reads that rather than a column.
func TestAgentRepositoryRevisionCountReadsTheAuditTrail(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()

	count, err := fixture.repo.RevisionCount(ctx, fixture.stageID)
	if err != nil {
		t.Fatalf("RevisionCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("a fresh stage reports %d revisions", count)
	}

	// Three transitions into needs_fix/needs_redo, and one that is NOT a revision, so
	// the statement is not counting every event that mentions the stage.
	for index, toStatus := range []string{"needs_fix", "needs_redo", "needs_fix", "running"} {
		_, err := fixture.db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO workflow_events
			(id, workflow_run_id, stage_run_id, event_type, from_status, to_status, created_at)
			VALUES ('event-%d', '%s', '%s', 'stage_transition', '', '%s', '%s')`,
			index, agentTestRun, agentTestStage, toStatus, agentStamp))
		if err != nil {
			t.Fatalf("seeding event %d: %v", index, err)
		}
	}
	count, err = fixture.repo.RevisionCount(ctx, fixture.stageID)
	if err != nil {
		t.Fatalf("RevisionCount: %v", err)
	}
	if count != 3 {
		t.Fatalf("the revision count is %d, want 3", count)
	}
	// An event belonging to another stage must not count, which is what makes the
	// count belong to the ATTEMPT rather than to the run.
	_, err = fixture.db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO workflow_events
		(id, workflow_run_id, stage_run_id, event_type, from_status, to_status, created_at)
		VALUES ('event-other', '%s', 'stage-other', 'stage_transition', '', 'needs_fix', '%s')`,
		agentTestRun, agentStamp))
	if err != nil {
		t.Fatalf("seeding another stage's event: %v", err)
	}
	count, err = fixture.repo.RevisionCount(ctx, fixture.stageID)
	if err != nil {
		t.Fatalf("RevisionCount: %v", err)
	}
	if count != 3 {
		t.Fatalf("another stage's event was counted: %d", count)
	}
}

// TestAgentRepositoryWorkflowRunOfStage covers the locator the engine needs to find
// which run a stage belongs to.
func TestAgentRepositoryWorkflowRunOfStage(t *testing.T) {
	fixture := newAgentFixture(t)
	runID, err := fixture.repo.WorkflowRunOfStage(context.Background(), fixture.stageID)
	if err != nil {
		t.Fatalf("WorkflowRunOfStage: %v", err)
	}
	if runID != fixture.runID {
		t.Fatalf("the stage belongs to %q, want %q", runID, fixture.runID)
	}
	if _, err := fixture.repo.WorkflowRunOfStage(context.Background(), "stage-missing"); err == nil {
		t.Fatal("a missing stage was located")
	} else if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryNotFound {
		t.Fatalf("a missing stage reports %v", err)
	}
}

// TestAgentRepositoryRecallsOnlyItsScope is AC-MEM-001's core through the real schema.
//
// Two projects hold similarly-named messages, and a recall for one must not see the
// other. The scope is filtered on the STRUCTURED COLUMNS rather than the encoded key,
// which is DOMAIN_MODEL section 14.4's "检索实现必须能按结构字段隔离，不依赖脆弱字符串前缀".
func TestAgentRepositoryRecallsOnlyItsScope(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.seedSecondProject(t)
	fixture.seedAgentRun(t, "run-a", agentTestProject)
	fixture.seedAgentRun(t, "run-b", agentTestSecond)
	fixture.seedMessage(t, "message-a", "run-a",
		"local||"+agentTestProject+"||agent.x|", agentTestProject, "agent.x", "in project one", 1)
	fixture.seedMessage(t, "message-b", "run-b",
		"local||"+agentTestSecond+"||agent.x|", agentTestSecond, "agent.x", "in project two", 2)

	items, err := fixture.repo.RecentMessages(context.Background(),
		[6]string{"local", "", agentTestProject, "", "", ""}, "", 10)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("the recall returned %d messages, want 1", len(items))
	}
	if items[0].Content != "in project one" {
		t.Fatalf("the recall returned the other project's message: %q", items[0].Content)
	}
	if items[0].Provenance != "run-a" {
		t.Fatalf("the recalled message names run %q as its provenance", items[0].Provenance)
	}
}

// TestAgentRepositoryRecallExcludesTheCurrentMessage is DOMAIN_MODEL section 14.5's
// first invariant through the schema: "当前消息不召回自身".
func TestAgentRepositoryRecallExcludesTheCurrentMessage(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.seedAgentRun(t, "run-a", agentTestProject)
	scopeKey := "local||" + agentTestProject + "||script.execution.event_extraction|"
	fixture.seedMessage(t, "message-0", "run-a", scopeKey, agentTestProject,
		"script.execution.event_extraction", "older question", 0)
	fixture.seedMessage(t, "message-1", "run-a", scopeKey, agentTestProject,
		"script.execution.event_extraction", "the current one", 1)

	scope := [6]string{"local", "", agentTestProject, "", "script.execution.event_extraction", ""}
	items, err := fixture.repo.RecentMessages(context.Background(), scope, "message-1", 10)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(items) != 1 || items[0].Content != "older question" {
		t.Fatalf("the recall returned %+v, so the current message was not excluded", items)
	}
	// Without the exclusion both come back, which is what makes the assertion above
	// meaningful rather than a coincidence of there being one row.
	items, err = fixture.repo.RecentMessages(context.Background(), scope, "", 10)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("without an exclusion the recall returned %d messages, want 2", len(items))
	}
}

// TestAgentRepositoryRecallRefusesAnUnscopedProject covers the second invariant from
// the storage side: a recall with no project would match every project's rows.
func TestAgentRepositoryRecallRefusesAnUnscopedProject(t *testing.T) {
	fixture := newAgentFixture(t)
	_, err := fixture.repo.RecentMessages(context.Background(), [6]string{"local", "", "", "", "", ""}, "", 10)
	if err == nil {
		t.Fatal("an unscoped recall was performed")
	}
	if _, ok := err.(*memoryapp.Error); !ok {
		t.Fatalf("the refusal is a %T, want the memory port's own error", err)
	}
}

// TestAgentRepositoryRecallAgentFilterNarrowsToTheAgent covers the second question
// section 12.2 asks — "what did THIS agent say" — which is the agent key column.
func TestAgentRepositoryRecallAgentFilterNarrowsToTheAgent(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.seedAgentRun(t, "run-a", agentTestProject)
	fixture.seedMessage(t, "message-0", "run-a",
		"local||"+agentTestProject+"||agent.one|", agentTestProject, "agent.one", "one", 0)
	fixture.seedMessage(t, "message-1", "run-a",
		"local||"+agentTestProject+"||agent.two|", agentTestProject, "agent.two", "two", 1)

	items, err := fixture.repo.RecentMessages(context.Background(),
		[6]string{"local", "", agentTestProject, "", "agent.one", ""}, "", 10)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(items) != 1 || items[0].Content != "one" {
		t.Fatalf("the agent filter returned %+v", items)
	}
	// Without an agent key both come back: a project-wide recall is a different
	// question and must not be silently narrowed.
	items, err = fixture.repo.RecentMessages(context.Background(),
		[6]string{"local", "", agentTestProject, "", "", ""}, "", 10)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("a project-wide recall returned %d messages, want 2", len(items))
	}
}

// TestAgentRepositoryRecallIsNewestFirst covers the ordering the Store port promises
// and the memory service reverses.
func TestAgentRepositoryRecallIsNewestFirst(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.seedAgentRun(t, "run-a", agentTestProject)
	scopeKey := "local||" + agentTestProject + "||agent.x|"
	for index := 0; index < 3; index++ {
		fixture.seedMessage(t, "message-"+itoaTest(index), "run-a", scopeKey,
			agentTestProject, "agent.x", "message "+itoaTest(index), index)
	}
	items, err := fixture.repo.RecentMessages(context.Background(),
		[6]string{"local", "", agentTestProject, "", "agent.x", ""}, "", 10)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("the recall returned %d messages", len(items))
	}
	if items[0].Content != "message 2" {
		t.Fatalf("the recall starts with %q, want the newest", items[0].Content)
	}
	// The limit is honoured from the newest end, which is what a "recent window" means.
	items, err = fixture.repo.RecentMessages(context.Background(),
		[6]string{"local", "", agentTestProject, "", "agent.x", ""}, "", 2)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(items) != 2 || items[0].Content != "message 2" || items[1].Content != "message 1" {
		t.Fatalf("a bounded recall returned %+v", items)
	}
}

// TestAgentRepositoryRegistersSkillVersions covers the table a run cites, including
// the unique pair that makes "one version, one row" a constraint.
func TestAgentRepositoryRegistersSkillVersions(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	version := agent.SkillVersion{
		ID: "skill-version-2", SkillKey: "script", Version: "1.1.0",
		ContentHash: "hash-2", ManifestJSON: `{"apiVersion":"atelier.agent/v1"}`,
		Status: agent.SkillActive,
		// Newer than the seeded row, so the newest-active lookup finds it.
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := fixture.repo.RegisterSkillVersion(ctx, version); err != nil {
		t.Fatalf("RegisterSkillVersion: %v", err)
	}
	read, err := fixture.repo.SkillVersionByKey(ctx, "script")
	if err != nil {
		t.Fatalf("SkillVersionByKey: %v", err)
	}
	if read.ID != version.ID || read.ContentHash != version.ContentHash {
		t.Fatalf("the skill version came back as %+v", read)
	}
	if !read.CreatedAt.Equal(version.CreatedAt) {
		t.Fatalf("the created time is %v, want %v", read.CreatedAt, version.CreatedAt)
	}
	// The same (key, version) pair again is a conflict: two rows for one version would
	// make a run's citation ambiguous.
	duplicate := version
	duplicate.ID = "skill-version-3"
	err = fixture.repo.RegisterSkillVersion(ctx, duplicate)
	if err == nil {
		t.Fatal("a duplicate skill version was accepted")
	}
	if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryConflict {
		t.Fatalf("a duplicate skill version reports %v, want a conflict", err)
	}
	if _, err := fixture.repo.SkillVersionByKey(ctx, "no-such-skill"); err == nil {
		t.Fatal("an unregistered skill key returned a version")
	}
}

// TestAgentRepositorySkillLookupIgnoresSupersededVersions covers the status column: a
// superseded version must not be what a new run cites, or a run would be bound to a
// document nobody is using.
func TestAgentRepositorySkillLookupIgnoresSupersededVersions(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	_, err := fixture.db.ExecContext(ctx, fmt.Sprintf(`UPDATE skill_versions
		SET status = 'superseded' WHERE id = '%s'`, agentTestSkill))
	if err != nil {
		t.Fatalf("superseding the seeded version: %v", err)
	}
	if _, err := fixture.repo.SkillVersionByKey(ctx, "script"); err == nil {
		t.Fatal("a superseded skill version was returned as the active one")
	}
}

// TestAgentRepositoryListsRunsNewestFirst covers the read an Agent Center list is
// built on.
func TestAgentRepositoryListsRunsNewestFirst(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	for index, id := range []string{"run-old", "run-new"} {
		run := fixture.sampleRun(id)
		run.StartedAt = time.Date(2026, 1, 1+index, 0, 0, 0, 0, time.UTC)
		if err := fixture.repo.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun %s: %v", id, err)
		}
	}
	runs, err := fixture.repo.ListRuns(ctx, fixture.project, 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("the list holds %d runs", len(runs))
	}
	if runs[0].ID != "run-new" {
		t.Fatalf("the list starts with %q, want the newest", runs[0].ID)
	}
	// A project with no runs gets an empty list rather than a nil or an error.
	empty, err := fixture.repo.ListRuns(ctx, "project-none", 10)
	if err != nil {
		t.Fatalf("ListRuns for an empty project: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("an empty project returned %d runs", len(empty))
	}
	// The limit is honoured.
	one, err := fixture.repo.ListRuns(ctx, fixture.project, 1)
	if err != nil {
		t.Fatalf("ListRuns with a limit: %v", err)
	}
	if len(one) != 1 || one[0].ID != "run-new" {
		t.Fatalf("a bounded list returned %+v", one)
	}
}

// TestAgentRepositoryFailsClosedWithoutAConnection covers the shape a safe-mode
// database produces: every method refuses rather than panicking on a nil handle.
func TestAgentRepositoryFailsClosedWithoutAConnection(t *testing.T) {
	ctx := context.Background()
	var none *AgentRepository
	if err := none.CreateRun(ctx, agent.AgentRun{ID: "x"}); err == nil {
		t.Fatal("a nil repository accepted a run")
	}
	if _, err := none.GetRun(ctx, "x"); err == nil {
		t.Fatal("a nil repository read a run")
	}
	if _, err := none.ListRuns(ctx, "p", 10); err == nil {
		t.Fatal("a nil repository listed runs")
	}
	if err := none.FinishRun(ctx, agent.AgentRun{ID: "x"}, 1); err == nil {
		t.Fatal("a nil repository finished a run")
	}
	if err := none.RecordMessage(ctx, agent.AgentMessage{ID: "x"}); err == nil {
		t.Fatal("a nil repository recorded a message")
	}
	if err := none.RecordToolCall(ctx, agent.AgentToolCall{ID: "x"}); err == nil {
		t.Fatal("a nil repository recorded a tool call")
	}
	if _, err := none.ListMessages(ctx, "x"); err == nil {
		t.Fatal("a nil repository listed messages")
	}
	if _, err := none.ListToolCalls(ctx, "x"); err == nil {
		t.Fatal("a nil repository listed tool calls")
	}
	if _, err := none.RevisionCount(ctx, "x"); err == nil {
		t.Fatal("a nil repository counted revisions")
	}
	if _, err := none.WorkflowRunOfStage(ctx, "x"); err == nil {
		t.Fatal("a nil repository located a stage")
	}
	if _, err := none.RecentMessages(ctx, [6]string{"", "", "p", "", "", ""}, "", 10); err == nil {
		t.Fatal("a nil repository recalled messages")
	}
	if err := none.RegisterSkillVersion(ctx, agent.SkillVersion{ID: "x"}); err == nil {
		t.Fatal("a nil repository registered a skill version")
	}
	if _, err := none.SkillVersionByKey(ctx, "x"); err == nil {
		t.Fatal("a nil repository read a skill version")
	}
}

// TestSplitScopeKeyIsTheInverseOfTheRuntimeEncoding covers the translation this
// repository performs, including the case that would be a boundary defect: a key with
// more separators than the encoding defines must not shift a value into a later
// column.
func TestSplitScopeKeyIsTheInverseOfTheRuntimeEncoding(t *testing.T) {
	parts := splitScopeKey("local|ws||episode|agent.key|")
	want := [6]string{"local", "ws", "", "episode", "agent.key", ""}
	if parts != want {
		t.Fatalf("the key split as %q, want %q", parts, want)
	}
	// A short key fills the leading parts and leaves the rest empty rather than
	// panicking.
	short := splitScopeKey("local||project-1")
	if short[2] != "project-1" || short[5] != "" {
		t.Fatalf("a short key split as %q", short)
	}
	// Anything past the fifth separator belongs to the last part rather than being
	// dropped: dropping it would silently lose a session id, and shifting it left
	// would put a value in a column that means something else.
	overflow := splitScopeKey("a|b|c|d|e|f|g|h")
	if overflow[5] != "f|g|h" {
		t.Fatalf("an over-long key split as %q", overflow)
	}
}

// TestAgentRepositoryMessageScopeColumnsAreStored proves the write side of section
// 14.4: a message's scope travels as its six columns, not only as an encoded key, so
// a recall can filter on the structure.
func TestAgentRepositoryMessageScopeColumnsAreStored(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	if err := fixture.repo.CreateRun(ctx, fixture.sampleRun("run-a")); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	scopeKey := "local|workspace-1|" + agentTestProject + "|episode-1|agent.one|session-1"
	err := fixture.repo.RecordMessage(ctx, agent.AgentMessage{
		ID: "message-1", AgentRunID: "run-a", ScopeKey: scopeKey,
		Role: agent.MessageUser, Content: "x", ContentHash: "h",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordMessage: %v", err)
	}
	var tenant, workspace, project, episode, agentKey, session string
	err = fixture.db.QueryRowContext(ctx, `SELECT scope_tenant, scope_workspace, scope_project,
		scope_episode, scope_agent_key, scope_session FROM agent_messages WHERE id = 'message-1'`).
		Scan(&tenant, &workspace, &project, &episode, &agentKey, &session)
	if err != nil {
		t.Fatalf("reading the scope columns: %v", err)
	}
	if tenant != "local" || workspace != "workspace-1" || project != agentTestProject ||
		episode != "episode-1" || agentKey != "agent.one" || session != "session-1" {
		t.Fatalf("the scope columns are %q/%q/%q/%q/%q/%q",
			tenant, workspace, project, episode, agentKey, session)
	}
}
