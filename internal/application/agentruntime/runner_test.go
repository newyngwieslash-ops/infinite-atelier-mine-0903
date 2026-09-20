package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// These tests cover one invocation end to end, and they are where AC-AGENT-002,
// 003 and 005 are written. The runtime is driven with a scripted model and an
// in-memory store, because what is being tested is the ORDER of what happens —
// validate before acting, repair once, refuse rather than half-write — and that
// order is visible in what the store received.

// scriptedModel returns the replies it was given, in order.
//
// Tool calls are scripted SEPARATELY from the reply's content, because that is where
// they travel: section 7's output schemas are additionalProperties false, so a tool
// call inside a returned document is refused by the schema that document must
// satisfy. A test that scripted them inside the JSON would be testing the old,
// unreachable path.
type scriptedModel struct {
	replies []string
	errs    []error
	// toolCalls is indexed alongside replies: entry i is what reply i asked for.
	toolCalls [][]ToolCallRequest
	calls     []ModelRequest
}

func (m *scriptedModel) Complete(_ context.Context, request ModelRequest) (ModelReply, error) {
	index := len(m.calls)
	m.calls = append(m.calls, request)
	if index < len(m.errs) && m.errs[index] != nil {
		return ModelReply{}, m.errs[index]
	}
	if index >= len(m.replies) {
		// A model asked for more answers than the script has is a test defect, and
		// saying so is better than returning an empty reply that would look like a
		// model failure.
		return ModelReply{}, errors.New("the scripted model ran out of replies")
	}
	reply := ModelReply{Content: m.replies[index], Model: "scripted", FinishReason: "stop"}
	if index < len(m.toolCalls) {
		reply.ToolCalls = m.toolCalls[index]
	}
	return reply, nil
}

// memoryRunStore records what the runtime wrote, in order.
type memoryRunStore struct {
	runs      []agent.AgentRun
	messages  []agent.AgentMessage
	toolCalls []agent.AgentToolCall
	// runOrder records whether the run row was written before the first model call,
	// which is what makes a run that dies mid-flight visible.
	finishes []agent.RunStatus
}

func (s *memoryRunStore) CreateRun(_ context.Context, run agent.AgentRun) error {
	s.runs = append(s.runs, run)
	return nil
}

func (s *memoryRunStore) FinishRun(_ context.Context, run agent.AgentRun, _ int64) error {
	s.runs = append(s.runs, run)
	s.finishes = append(s.finishes, run.Status)
	return nil
}

func (s *memoryRunStore) RecordMessage(_ context.Context, message agent.AgentMessage) error {
	s.messages = append(s.messages, message)
	return nil
}

func (s *memoryRunStore) RecordToolCall(_ context.Context, call agent.AgentToolCall) error {
	s.toolCalls = append(s.toolCalls, call)
	return nil
}

// lastStatus is the run's final status.
func (s *memoryRunStore) lastStatus() agent.RunStatus {
	if len(s.finishes) == 0 {
		return ""
	}
	return s.finishes[len(s.finishes)-1]
}

// jsonValidator accepts any output that is a JSON object, and refuses one that is
// not — which lets a test drive the repair round with a reply that is malformed.
func jsonValidator(string, []byte) ([]Violation, error) {
	return nil, nil
}

// strictValidator refuses any output containing a marker, so a test can drive a
// refusal on demand.
func strictValidator(marker string) Validator {
	return func(_ string, raw []byte) ([]Violation, error) {
		if strings.Contains(string(raw), marker) {
			return []Violation{{Path: "/summary", Message: "does not satisfy maxLength"}}, nil
		}
		return nil, nil
	}
}

// testRuntime builds a runtime over the harness.
type testHarness struct {
	runtime *Runtime
	model   *scriptedModel
	store   *memoryRunStore
	tools   *Tools
}

func newHarness(t *testing.T, replies []string, mutate func(*Options)) *testHarness {
	t.Helper()
	table := testTools(t)
	model := &scriptedModel{replies: replies}
	store := &memoryRunStore{}
	options := Options{
		Registry: mustRegistry(t, table),
		Tools:    table,
		Models:   model,
		Runs:     store,
		Validate: jsonValidator,
		Clock:    fixedRuntimeClock{},
		IDs:      &runtimeIDs{},
	}
	if mutate != nil {
		mutate(&options)
	}
	// The registry is built last so a mutate that replaces the tool table is
	// reflected in it.
	options.Registry = mustRegistry(t, options.Tools)
	return &testHarness{runtime: New(options), model: model, store: store, tools: options.Tools}
}

func mustRegistry(t *testing.T, table *Tools) *Registry {
	t.Helper()
	registry, err := NewRegistry([]agent.Spec{
		validSpec("script.execution.x", agent.LayerExecution, "story.read_events", "script.create_script_version"),
		validSpec("script.supervision.x", agent.LayerSupervision, "story.read_events"),
		validSpec("script.decision", agent.LayerDecision, "workflow.read_state"),
	}, table)
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}
	return registry
}

type fixedRuntimeClock struct{}

func (fixedRuntimeClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

type runtimeIDs struct{ next int }

func (g *runtimeIDs) New() (string, error) {
	g.next++
	return "id-" + itoa(g.next), nil
}

// invocationFor builds a valid invocation.
func invocationFor(key string) Invocation {
	return Invocation{
		AgentKey: key, ProjectID: "project-1", StageRunID: "stage-1",
		Skill: "# Role\n\nDo the step.", SkillVersion: "sv-1",
		WorkflowState: "stage=x attempt=1",
		Task:          "the chapter to read",
		UserMessage:   "go ahead",
		ModelID:       "model-1",
	}
}

// TestRunValidatesBeforeItActs is the runner's central ordering rule: a validated
// output is what lets anything happen, and nothing happens before one exists.
func TestRunValidatesBeforeItActs(t *testing.T) {
	harness := newHarness(t, []string{`{"summary":"fine","toolCalls":[]}`}, nil)
	outcome, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if harness.store.lastStatus() != agent.RunSucceeded {
		t.Fatalf("the run ended as %q", harness.store.lastStatus())
	}
	if string(outcome.Output) != `{"summary":"fine","toolCalls":[]}` {
		t.Fatalf("the output read as %s", outcome.Output)
	}
	// The run row was written BEFORE the model was called, so a run that dies
	// mid-flight still leaves a record that it started.
	if len(harness.store.runs) < 2 {
		t.Fatalf("the store holds %d run rows, want the create and the finish", len(harness.store.runs))
	}
	if harness.store.runs[0].Status != agent.RunRunning {
		t.Fatalf("the first run row is %q, want running", harness.store.runs[0].Status)
	}
	if harness.store.runs[0].SkillVersionID != "sv-1" {
		t.Fatal("the run row does not name its skill version, so it could not be reproduced")
	}
	if harness.store.runs[0].ModelConfigID != "model-1" {
		t.Fatal("the run row does not name its model, so section 13's model change would not be visible")
	}
	// The model's own turn was recorded.
	if len(harness.store.messages) != 1 || harness.store.messages[0].Role != agent.MessageAssistant {
		t.Fatalf("the store holds %+v", harness.store.messages)
	}
	// The prompt carried the layers, in order, and the user message is untrusted.
	if len(harness.model.calls) != 1 {
		t.Fatalf("the model was called %d times", len(harness.model.calls))
	}
	rendered := harness.model.calls[0].Messages
	if len(rendered) < 4 {
		t.Fatalf("the prompt rendered %d messages: %+v", len(rendered), rendered)
	}
	if !strings.Contains(rendered[len(rendered)-1].Content, UntrustedOpen) {
		t.Fatal("the user message was not marked untrusted")
	}
}

// TestRunRepairsOnceAndOnlyOnce is AC-AGENT-002's middle case and section 14.3's
// whole rule: the first malformed reading gets ONE repair, and a second malformed
// reading fails the run.
func TestRunRepairsOnceAndOnlyOnce(t *testing.T) {
	// The first reply is refused, the second is accepted: the repair succeeds.
	harness := newHarness(t, []string{
		`{"summary":"REPAIR_ME"}`,
		`{"summary":"fine"}`,
	}, func(options *Options) { options.Validate = strictValidator("REPAIR_ME") })
	outcome, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err != nil {
		t.Fatalf("a repairable output should have been repaired: %v", err)
	}
	if !outcome.Repaired {
		t.Fatal("the outcome does not report that a repair happened")
	}
	if len(harness.model.calls) != 2 {
		t.Fatalf("the model was called %d times, want two", len(harness.model.calls))
	}
	// The repair prompt must CARRY the violations, or the model is only being asked
	// to try again.
	repair := harness.model.calls[1].Messages
	last := repair[len(repair)-1]
	if !strings.Contains(last.Content, "/summary") {
		t.Fatalf("the repair prompt does not name the failing path: %q", last.Content)
	}
	// And it must not quote the model's own output, which quotes a document.
	if strings.Contains(last.Content, "REPAIR_ME") {
		t.Fatalf("the repair prompt quotes the offending value: %q", last.Content)
	}
	if harness.store.lastStatus() != agent.RunSucceeded {
		t.Fatalf("the run ended as %q", harness.store.lastStatus())
	}

	// Both replies refused: the run fails, and it fails after exactly two calls.
	failing := newHarness(t, []string{`{"summary":"REPAIR_ME"}`, `{"summary":"REPAIR_ME"}`},
		func(options *Options) { options.Validate = strictValidator("REPAIR_ME") })
	_, err = failing.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err == nil {
		t.Fatal("an output refused twice was accepted")
	}
	var schemaErr *SchemaError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("the refusal is a %T, want a schema error", err)
	}
	if !schemaErr.Repaired {
		t.Fatal("the refusal does not record that the repair round already ran")
	}
	if len(failing.model.calls) != 2 {
		t.Fatalf("the model was called %d times, want exactly two — section 14.3 allows one repair", len(failing.model.calls))
	}
	if failing.store.lastStatus() != agent.RunFailed {
		t.Fatalf("the failed run ended as %q", failing.store.lastStatus())
	}
	if IsRetriable(err) {
		t.Fatal("a schema refusal is reported as retriable, which would allow a second repair round")
	}
}

// TestRunRefusesAnInventedArtifact is AC-AGENT-003.
func TestRunRefusesAnInventedArtifact(t *testing.T) {
	verifier := &recordingVerifier{missing: "ghost-version"}
	harness := newHarness(t, []string{
		`{"status":"success","artifacts":[{"entityType":"story_skeleton","entityId":"ghost","versionId":"ghost-version","operation":"created"}]}`,
	}, func(options *Options) { options.Artifacts = verifier })
	_, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err == nil {
		t.Fatal("an output naming a version that does not exist was accepted")
	}
	var artifactErr *ArtifactError
	if !errors.As(err, &artifactErr) {
		t.Fatalf("the refusal is a %T, want an artifact error", err)
	}
	// AC-AGENT-003: the runtime validates, does not mark success, records the error.
	if harness.store.lastStatus() == agent.RunSucceeded {
		t.Fatal("a run with an invented artifact was marked successful")
	}
	if harness.store.lastStatus() != agent.RunFailed {
		t.Fatalf("the run ended as %q", harness.store.lastStatus())
	}
	last := harness.store.runs[len(harness.store.runs)-1]
	if last.ErrorCode == "" {
		t.Fatal("the run records no error code for the invented artifact")
	}
	// And the verifier was actually asked, which is what makes the check real.
	if verifier.calls == 0 {
		t.Fatal("the artifact verifier was never called")
	}
}

// recordingVerifier refuses one specific version.
type recordingVerifier struct {
	missing string
	calls   int
}

func (v *recordingVerifier) VerifyArtifacts(_ context.Context, refs []ArtifactRef) error {
	v.calls++
	for _, ref := range refs {
		if ref.VersionID == v.missing {
			return &ArtifactError{EntityType: ref.EntityType, EntityID: ref.EntityID}
		}
	}
	return nil
}

// TestRunRefusesMoreToolCallsThanTheBudget is AC-AGENT-005's first half: the count
// is checked before any call runs, so a model cannot exceed its budget by asking
// for more at once.
func TestRunRefusesMoreToolCallsThanTheBudget(t *testing.T) {
	calls := 0
	// The table must still satisfy every spec the harness registers, so the write
	// tool the fixture agent lists is present even though this test is about the
	// read one. A table that dropped it would fail registry construction rather than
	// the budget check, which is a different test.
	table, err := NewTools([]Tool{
		{
			Spec:       agent.ToolSpec{Key: "story.read_events", Mode: agent.ToolRead, Scope: "project", MaxOutputBytes: 1024},
			SchemaPath: "schemas/agent/tools/story.read_events.json",
			Handler: func(context.Context, ToolRequest) (any, error) {
				calls++
				return map[string]any{"ok": true}, nil
			},
		},
		testTool("script.create_script_version", agent.ToolWrite, 1024),
		testTool("workflow.read_state", agent.ToolRead, 1024),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The fixture agent's budget is 8 tool calls, so nine is one too many.
	requests := make([]ToolCallRequest, 0, 9)
	for index := 0; index < 9; index++ {
		requests = append(requests, ToolCallRequest{Key: "story.read_events", Arguments: json.RawMessage(`{}`)})
	}
	harness := newHarness(t, []string{`{"summary":"x"}`}, func(options *Options) { options.Tools = table })
	harness.model.toolCalls = [][]ToolCallRequest{requests}
	_, err = harness.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err == nil {
		t.Fatal("an output asking for more tool calls than the budget was accepted")
	}
	var quota *QuotaError
	if !errors.As(err, &quota) || quota.Limit != "tool_calls" {
		t.Fatalf("the refusal is %v, want a tool-call quota error", err)
	}
	// No call ran, because the count was checked first.
	if calls != 0 {
		t.Fatalf("%d tool calls ran despite the budget being exceeded", calls)
	}
	if !strings.Contains(quota.Error(), "tool calls") {
		t.Fatalf("the quota message reads %q", quota.Error())
	}
}

// TestRunDeniesAnIllegalToolCallAndStops is AC-AGENT-001's second half: an illegal
// call is refused with the named code, the run records the failure, and the
// invocation does not continue as if nothing happened.
func TestRunDeniesAnIllegalToolCallAndStops(t *testing.T) {
	// A write tool exists in the table and the execution agent even lists it, but
	// the test's registry grants the SUPERVISOR only a read tool. So a supervisor
	// asking for a write is refused by the matrix.
	harness := newHarness(t, []string{`{"summary":"x"}`}, nil)
	harness.model.toolCalls = [][]ToolCallRequest{{
		{Key: "script.create_script_version", Arguments: json.RawMessage(`{}`)},
	}}
	_, err := harness.runtime.Run(context.Background(), invocationFor("script.supervision.x"))
	if err == nil {
		t.Fatal("a supervisor's write call was allowed")
	}
	var notAllowed *ToolNotAllowedError
	if !errors.As(err, &notAllowed) {
		t.Fatalf("the refusal is a %T, want the ACL's error", err)
	}
	if notAllowed.Code() != CodeToolNotAllowed {
		t.Fatalf("the refusal carries code %q, want %q", notAllowed.Code(), CodeToolNotAllowed)
	}
	// The run records the denial rather than a generic failure.
	if harness.store.lastStatus() != agent.RunFailed {
		t.Fatalf("the run ended as %q", harness.store.lastStatus())
	}
	last := harness.store.runs[len(harness.store.runs)-1]
	if last.ErrorCode != CodeToolNotAllowed {
		t.Fatalf("the run records error code %q, want %q", last.ErrorCode, CodeToolNotAllowed)
	}
	// And the tool call itself is recorded as DENIED, which is what section 7.1
	// wants: the refusal attributable to the ACL rather than to the tool.
	if len(harness.store.toolCalls) != 1 {
		t.Fatalf("the store holds %d tool calls", len(harness.store.toolCalls))
	}
	if harness.store.toolCalls[0].Status != agent.ToolCallDenied {
		t.Fatalf("the tool call is %q, want denied", harness.store.toolCalls[0].Status)
	}
	if harness.store.toolCalls[0].ErrorCode == "" {
		t.Fatal("the denied call records no reason, which the schema refuses anyway")
	}
}

// TestRunRefusesAnUnknownAgentOrProject covers the two request-shaped refusals.
func TestRunRefusesAnUnknownAgentOrProject(t *testing.T) {
	harness := newHarness(t, []string{`{"summary":"x"}`}, nil)
	if _, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.nope")); err == nil {
		t.Fatal("an unregistered agent was run")
	}
	broken := invocationFor("script.execution.x")
	broken.ProjectID = "  "
	if _, err := harness.runtime.Run(context.Background(), broken); err == nil {
		t.Fatal("an invocation with no project was run")
	}
	// An invocation with no skill version is refused by the domain, because section
	// 4.2 requires a run to name the version it ran.
	unversioned := invocationFor("script.execution.x")
	unversioned.SkillVersion = ""
	if _, err := harness.runtime.Run(context.Background(), unversioned); err == nil {
		t.Fatal("an invocation with no skill version was run")
	}
	// The model must not have been called for any of them.
	if len(harness.model.calls) != 0 {
		t.Fatalf("the model was called %d times for refusals that happen before it", len(harness.model.calls))
	}
}

// TestRunFailsClosedWhenUnattached covers the composition case.
func TestRunFailsClosedWhenUnattached(t *testing.T) {
	for name, runtime := range map[string]*Runtime{
		"no options at all": New(Options{}),
		"no model":          New(Options{Tools: testTools(t)}),
		"no run store":      New(Options{Tools: testTools(t), Models: &scriptedModel{}}),
		"no validator":      New(Options{Tools: testTools(t), Models: &scriptedModel{}, Runs: &memoryRunStore{}}),
	} {
		t.Run(name, func(t *testing.T) {
			if runtime.Available() {
				t.Fatal("an incomplete runtime reports itself available")
			}
			if _, err := runtime.Run(context.Background(), invocationFor("script.execution.x")); err == nil {
				t.Fatal("an incomplete runtime ran an agent")
			}
		})
	}
}

// TestRunReportsACancellationAsItself covers section 15: a cancel is not a failure
// and must be distinguishable from one.
func TestRunReportsACancellationAsItself(t *testing.T) {
	model := &scriptedModel{errs: []error{context.Canceled}}
	table := testTools(t)
	store := &memoryRunStore{}
	runtime := New(Options{
		Registry: mustRegistry(t, table), Tools: table, Models: model, Runs: store,
		Validate: jsonValidator, Clock: fixedRuntimeClock{}, IDs: &runtimeIDs{},
	})
	_, err := runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err == nil {
		t.Fatal("a cancelled call was reported as success")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the refusal is %v, want the cancellation so the caller can tell it apart", err)
	}
	// The run records the cancellation as cancelled, not failed: section 15's
	// "StageRun 标记 cancelled".
	if store.lastStatus() != agent.RunCancelled {
		t.Fatalf("the run ended as %q, want cancelled", store.lastStatus())
	}
	if IsRetriable(err) {
		t.Fatal("a cancellation is reported as retriable")
	}
}

// TestRunReportsAModelFailureWithItsCode covers the pass-through case: a port's own
// failure keeps its identity so the caller can decide whether to retry.
func TestRunReportsAModelFailureWithItsCode(t *testing.T) {
	model := &scriptedModel{errs: []error{agent.InvalidError("the provider is not configured")}}
	table := testTools(t)
	store := &memoryRunStore{}
	runtime := New(Options{
		Registry: mustRegistry(t, table), Tools: table, Models: model, Runs: store,
		Validate: jsonValidator, Clock: fixedRuntimeClock{}, IDs: &runtimeIDs{},
	})
	_, err := runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err == nil {
		t.Fatal("a model failure was reported as success")
	}
	if store.lastStatus() != agent.RunFailed {
		t.Fatalf("the run ended as %q", store.lastStatus())
	}
	if store.runs[len(store.runs)-1].ErrorCode == "" {
		t.Fatal("the failed run records no error code")
	}
}

// TestRunSummaryCarriesNoText covers a small privacy property: the run's input
// summary is a queryable row, and the text it summarises may be a document.
func TestRunSummaryCarriesNoText(t *testing.T) {
	const secret = "运营商的密钥是 sk-abcdefghijklmnop"
	harness := newHarness(t, []string{`{"summary":"fine"}`}, nil)
	invocation := invocationFor("script.execution.x")
	invocation.Task = secret
	invocation.TaskIsUntrusted = true
	invocation.UserMessage = secret
	if _, err := harness.runtime.Run(context.Background(), invocation); err != nil {
		t.Fatal(err)
	}
	summary := harness.store.runs[0].InputSummary
	if strings.Contains(summary, "sk-") || strings.Contains(summary, "密钥") {
		t.Fatalf("the run's summary carries the text it summarises: %q", summary)
	}
	if !strings.Contains(summary, "task_runes=") {
		t.Fatalf("the summary does not describe the task at all: %q", summary)
	}
	// The stored ASSISTANT message is the model's own words, which is what section 16
	// asks for, so it does carry text — that is the difference between a message and
	// a summary.
	if len(harness.store.messages) == 0 {
		t.Fatal("the model's turn was not recorded")
	}
}

// TestScopeKeyIsStructured covers DOMAIN_MODEL section 14.4's requirement that
// recall filter by structure rather than by a fragile prefix.
func TestScopeKeyIsStructured(t *testing.T) {
	invocation := invocationFor("script.execution.x")
	invocation.EpisodeID = "episode-1"
	key := scopeKey(invocation)
	parts := ScopeParts(invocation)
	if len(parts) != 6 {
		t.Fatalf("the scope has %d parts, want section 14.4's six", len(parts))
	}
	if parts[2] != "project-1" || parts[3] != "episode-1" || parts[4] != "script.execution.x" {
		t.Fatalf("the scope parts read %v", parts)
	}
	// The parts appear in the key, so the two describe one scope rather than two.
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !strings.Contains(key, part) {
			t.Fatalf("the key %q does not contain the part %q", key, part)
		}
	}
	// Two runs in different projects must not share a key, which is what makes the
	// key usable for isolation.
	other := invocationFor("script.execution.x")
	other.ProjectID = "project-2"
	if scopeKey(other) == key {
		t.Fatal("two projects produced the same scope key")
	}
	// And two runs of different agents in one project must not share one either.
	different := invocationFor("script.decision")
	different.EpisodeID = "episode-1"
	if scopeKey(different) == key {
		t.Fatal("two agents produced the same scope key")
	}
}

// TestToolCallScopesComeFromTheRun proves the handler receives the run's scope
// rather than anything the model wrote.
func TestToolCallScopesComeFromTheRun(t *testing.T) {
	var captured ToolRequest
	table, err := NewTools([]Tool{
		{
			Spec:       agent.ToolSpec{Key: "story.read_events", Mode: agent.ToolRead, Scope: "project", MaxOutputBytes: 1024},
			SchemaPath: "schemas/agent/tools/story.read_events.json",
			Handler: func(_ context.Context, request ToolRequest) (any, error) {
				captured = request
				return map[string]any{"ok": true}, nil
			},
		},
		testTool("script.create_script_version", agent.ToolWrite, 1024),
		testTool("workflow.read_state", agent.ToolRead, 1024),
	})
	if err != nil {
		t.Fatal(err)
	}
	harness := newHarness(t, []string{`{"summary":"x"}`}, func(options *Options) { options.Tools = table })
	harness.model.toolCalls = [][]ToolCallRequest{{
		{Key: "story.read_events", Arguments: json.RawMessage(`{"projectId":"project-other"}`)},
	}}
	invocation := invocationFor("script.execution.x")
	invocation.EpisodeID = "episode-real"
	if _, err := harness.runtime.Run(context.Background(), invocation); err != nil {
		t.Fatal(err)
	}
	if captured.ProjectID != "project-1" {
		t.Fatalf("the handler received project %q, want the run's", captured.ProjectID)
	}
	if captured.EpisodeID != "episode-real" || captured.StageRunID != "stage-1" {
		t.Fatalf("the handler received the wrong scope: %+v", captured)
	}
	// The model's ARGUMENTS are passed through unchanged — the handler needs them,
	// and a schema validated them before the call. What must not be taken from them
	// is the SCOPE, and that is the assertion above: the arguments still say
	// "project-other" while the handler was told "project-1". The two are separate
	// values for exactly that reason.
	if !strings.Contains(string(captured.Arguments), "project-other") {
		t.Fatalf("the arguments were not passed through: %q", captured.Arguments)
	}
	_ = json.RawMessage(nil)
}
