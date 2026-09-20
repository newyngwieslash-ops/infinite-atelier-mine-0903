package agentruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// These tests cover the Agent Center's read surface, which an independent review found had NO
// tests at all: `inspector.go` was 41 of 41 blocks at zero coverage, and its mutation run
// disabled four behaviours — the trace's project check, the list's limit clamp, the status
// counting and the inventory's layer grouping — with the whole suite staying green.
//
// The project check is the one that matters most: it is the only thing between the frontend and
// another project's agent trace, and the file's own comment calls it "the same boundary every tool
// applies".

// stubReader is a RunReader over fixed data.
//
// It embeds the interface with the value left nil so an unexpected call panics rather than
// returning a zero value that reads like success — this repository's established pattern.
type stubReader struct {
	RunReader
	runs     []agent.AgentRun
	messages []agent.AgentMessage
	calls    []agent.AgentToolCall
	// lastLimit records what the caller asked for, so a test can assert the clamp.
	lastLimit int
}

func (r *stubReader) ListRuns(_ context.Context, projectID string, limit int) ([]agent.AgentRun, error) {
	r.lastLimit = limit
	out := make([]agent.AgentRun, 0, len(r.runs))
	for _, run := range r.runs {
		if run.ProjectID == projectID {
			out = append(out, run)
		}
	}
	return out, nil
}

func (r *stubReader) GetRun(_ context.Context, id string) (agent.AgentRun, error) {
	for _, run := range r.runs {
		if run.ID == id {
			return run, nil
		}
	}
	return agent.AgentRun{}, agent.NotFoundError()
}

func (r *stubReader) ListMessages(_ context.Context, runID string) ([]agent.AgentMessage, error) {
	out := make([]agent.AgentMessage, 0, len(r.messages))
	for _, message := range r.messages {
		if message.AgentRunID == runID {
			out = append(out, message)
		}
	}
	return out, nil
}

func (r *stubReader) ListToolCalls(_ context.Context, runID string) ([]agent.AgentToolCall, error) {
	out := make([]agent.AgentToolCall, 0, len(r.calls))
	for _, call := range r.calls {
		if call.AgentRunID == runID {
			out = append(out, call)
		}
	}
	return out, nil
}

// inspectorFixture builds an inspector over two projects' runs.
func inspectorFixture() (*Inspector, *stubReader) {
	reader := &stubReader{
		runs: []agent.AgentRun{
			{ID: "run-mine", ProjectID: "project-mine", AgentKey: "script.execution.story_skeleton",
				Layer: agent.LayerExecution, Status: agent.RunSucceeded,
				SkillVersionID: "sv-1", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "run-other", ProjectID: "project-other", AgentKey: "script.decision",
				Layer: agent.LayerDecision, Status: agent.RunFailed, SkillVersionID: "sv-1"},
			{ID: "run-failed", ProjectID: "project-mine", AgentKey: "script.decision",
				Layer: agent.LayerDecision, Status: agent.RunFailed, SkillVersionID: "sv-1"},
		},
		messages: []agent.AgentMessage{
			{ID: "m1", AgentRunID: "run-mine", Role: agent.MessageUser, Content: "hello",
				ScopeKey: "local||project-mine||key|"},
		},
		calls: []agent.AgentToolCall{
			{ID: "c1", AgentRunID: "run-mine", Sequence: 0, ToolKey: "story.read_events",
				Status: agent.ToolCallSucceed},
		},
	}
	return NewInspector(reader), reader
}

// TestInspectorListsOnlyTheCallersProject is the boundary the frontend depends on.
func TestInspectorListsOnlyTheCallersProject(t *testing.T) {
	inspector, _ := inspectorFixture()
	runs, err := inspector.ListRuns(context.Background(), "project-mine", 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("the list holds %d runs, want the caller's two", len(runs))
	}
	for _, run := range runs {
		if run.ID == "run-other" {
			t.Fatal("another project's run appeared in the list")
		}
	}
	// An empty project is a bad request rather than an empty list: a list of nothing is a CLAIM
	// about the data, and "no project was named" is not a claim about anything.
	if _, err := inspector.ListRuns(context.Background(), "   ", 0); err == nil {
		t.Fatal("an empty project was accepted")
	}
}

// TestInspectorClampsTheRunLimit covers the bound the review disabled with nothing failing.
//
// A caller asking for a million runs gets the ceiling rather than the request: the query is
// bounded so one screen cannot ask the core to read a project's whole history into memory, and a
// caller asking for zero gets the default because zero is what an omitted field decodes to.
func TestInspectorClampsTheRunLimit(t *testing.T) {
	inspector, reader := inspectorFixture()
	if _, err := inspector.ListRuns(context.Background(), "project-mine", 0); err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if reader.lastLimit != DefaultRunLimit {
		t.Fatalf("an omitted limit reached the reader as %d, want the default", reader.lastLimit)
	}
	if _, err := inspector.ListRuns(context.Background(), "project-mine", 100000); err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if reader.lastLimit != MaxRunLimit {
		t.Fatalf("an enormous limit reached the reader as %d, want the ceiling", reader.lastLimit)
	}
	// A limit inside the bounds is passed through rather than replaced.
	if _, err := inspector.ListRuns(context.Background(), "project-mine", 5); err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if reader.lastLimit != 5 {
		t.Fatalf("a limit of 5 reached the reader as %d", reader.lastLimit)
	}
}

// TestInspectorRefusesAnotherProjectsTrace is the check the review disabled.
//
// A trace carries a run's messages, its tool calls and its validated output, so reaching one
// across the boundary would disclose another project's work in full — which is why the run is
// read FIRST and its project compared before anything else is fetched.
func TestInspectorRefusesAnotherProjectsTrace(t *testing.T) {
	inspector, _ := inspectorFixture()
	_, err := inspector.GetTrace(context.Background(), "project-mine", "run-other")
	if err == nil {
		t.Fatal("another project's trace was returned")
	}
	domainErr, ok := agent.AsError(err)
	if !ok {
		t.Fatalf("the refusal is a %T, want a domain error", err)
	}
	if domainErr.Category != agent.CategorySecurity {
		t.Fatalf("the refusal is category %q, want security", domainErr.Category)
	}
	// The caller's own trace comes back complete.
	trace, err := inspector.GetTrace(context.Background(), "project-mine", "run-mine")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if trace.Run.ID != "run-mine" {
		t.Fatalf("the trace names run %q", trace.Run.ID)
	}
	if len(trace.Messages) != 1 || len(trace.ToolCalls) != 1 {
		t.Fatalf("the trace carries %d messages and %d calls, want one of each",
			len(trace.Messages), len(trace.ToolCalls))
	}
	// An empty run id is a bad request.
	if _, err := inspector.GetTrace(context.Background(), "project-mine", ""); err == nil {
		t.Fatal("an empty run id was accepted")
	}
}

// TestInspectorReportsTimestampsHonestly covers the zero-time rendering.
//
// A run that has not finished has no finish time, and the empty string says that. The zero time
// rendered as "0001-01-01T00:00:00Z" would be a date a reader could compare against a real
// instant and be misled by.
func TestInspectorReportsTimestampsHonestly(t *testing.T) {
	inspector, reader := inspectorFixture()
	// A run that started and has not finished.
	reader.runs = append(reader.runs, agent.AgentRun{
		ID: "run-running", ProjectID: "project-mine", AgentKey: "script.execution.story_skeleton",
		Layer: agent.LayerExecution, Status: agent.RunRunning, SkillVersionID: "sv-1",
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	trace, err := inspector.GetTrace(context.Background(), "project-mine", "run-running")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if trace.Run.StartedAt == "" {
		t.Fatal("a started run reports no start time")
	}
	if trace.Run.FinishedAt != "" {
		t.Fatalf("a running run reports finish time %q, want the empty string", trace.Run.FinishedAt)
	}
}

// TestInspectorRefusesANilReader is the fail-closed direction.
//
// A binding attached to no inspector must report unavailable rather than an empty list, because a
// list of nothing is a claim about the project's data.
func TestInspectorRefusesANilReader(t *testing.T) {
	inspector := NewInspector(nil)
	if inspector.Available() {
		t.Fatal("an inspector with no reader reports available")
	}
	if _, err := inspector.ListRuns(context.Background(), "project-mine", 0); err == nil {
		t.Fatal("a list was produced with no reader")
	}
	if _, err := inspector.GetTrace(context.Background(), "project-mine", "run-mine"); err == nil {
		t.Fatal("a trace was produced with no reader")
	}
	var none *Inspector
	if none.Available() {
		t.Fatal("a nil inspector reports available")
	}
	if _, err := none.ListRuns(context.Background(), "project-mine", 0); err == nil {
		t.Fatal("a nil inspector produced a list")
	}
}

// TestInspectorSummaryCarriesWhatAListShows covers the projection's shape.
//
// The summary deliberately omits the validated output and the raw-output reference: a list that
// carried them would send a project's whole agent history to the frontend to render a table.
func TestInspectorSummaryCarriesWhatAListShows(t *testing.T) {
	inspector, reader := inspectorFixture()
	reader.runs[0].ValidatedOutputJSON = `{"huge":"document"}`
	reader.runs[0].RawOutputFileID = "file-1"
	reader.runs[0].ErrorCode = "agent.timeout"
	summary, err := inspector.GetTrace(context.Background(), "project-mine", "run-mine")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	// The output travels on the TRACE, which is the detail view's subject.
	if !strings.Contains(summary.Output, "huge") {
		t.Fatal("the trace carries no validated output")
	}
	// The summary names the identity fields a list shows.
	if summary.Run.AgentKey == "" || summary.Run.SkillVersionID == "" || summary.Run.Layer == "" {
		t.Fatalf("the summary is missing an identity field: %+v", summary.Run)
	}
	if summary.Run.ErrorCode != "agent.timeout" {
		t.Fatalf("the summary loses the error code: %q", summary.Run.ErrorCode)
	}
}

// TestInspectorMessagesCarryTheirScope covers the field a reviewer debugging a recall needs.
//
// The scope key is what tells a reader which conversation a message was filed under, and it is a
// structured key rather than a secret — the recalled text is what would be sensitive, and it is
// the message's own content.
func TestInspectorMessagesCarryTheirScope(t *testing.T) {
	inspector, _ := inspectorFixture()
	trace, err := inspector.GetTrace(context.Background(), "project-mine", "run-mine")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if trace.Messages[0].ScopeKey == "" {
		t.Fatal("a recalled message carries no scope")
	}
	if trace.Messages[0].Content != "hello" {
		t.Fatalf("the message content is %q", trace.Messages[0].Content)
	}
}

// TestInspectorRefusesAnUnreadableRun covers the not-found path.
func TestInspectorRefusesAnUnreadableRun(t *testing.T) {
	inspector, _ := inspectorFixture()
	_, err := inspector.GetTrace(context.Background(), "project-mine", "run-that-does-not-exist")
	if err == nil {
		t.Fatal("a missing run produced a trace")
	}
	var domainErr *agent.Error
	if !errors.As(err, &domainErr) || domainErr.Category != agent.CategoryNotFound {
		t.Fatalf("the refusal is %v, want a not-found", err)
	}
}
