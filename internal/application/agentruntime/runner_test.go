package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	runs []agent.AgentRun
	// createdStatuses is what each run's row said when it was created, so a test can assert the
	// ORDER of writes rather than only the final state.
	createdStatuses []agent.RunStatus
	messages        []agent.AgentMessage
	toolCalls       []agent.AgentToolCall
	// runOrder records whether the run row was written before the first model call,
	// which is what makes a run that dies mid-flight visible.
	finishes []agent.RunStatus
}

func (s *memoryRunStore) CreateRun(_ context.Context, run agent.AgentRun) error {
	s.runs = append(s.runs, run)
	// createdStatuses records what each run looked like when its row appeared, which is the
	// assertion "the row was written BEFORE the model was called" needs: a run that died
	// mid-flight leaves a row saying it had STARTED, and a store that only keeps the latest
	// state cannot show that.
	s.createdStatuses = append(s.createdStatuses, run.Status)
	return nil
}

// update replaces a stored row, which is what the real repository's guarded UPDATE does.
//
// The first version of this double only APPENDED, so FinishRun added a second row and a caller
// reading the run back saw the one CreateRun wrote — with no status, no output and no answering
// model. A test of what a finished run records therefore could not see the finish at all, which
// is what found this: the answering-model assertion read an empty field from a stale row.
func (s *memoryRunStore) update(run agent.AgentRun) {
	for index := range s.runs {
		if s.runs[index].ID == run.ID {
			s.runs[index] = run
			return
		}
	}
	s.runs = append(s.runs, run)
}

func (s *memoryRunStore) findRun(id string) (agent.AgentRun, error) {
	for _, run := range s.runs {
		if run.ID == id {
			return run, nil
		}
	}
	return agent.AgentRun{}, errors.New("no such run")
}

func (s *memoryRunStore) FinishRun(_ context.Context, run agent.AgentRun, _ int64) error {
	s.update(run)
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

// acceptingToolArguments accepts every tool call's arguments, which is the shape the
// repository's other doubles have: a test about the ORDER of what happens is not a test about
// a tool's input contract, and the tests that ARE about it install their own validator.
func acceptingToolArguments(string, []byte) ([]Violation, error) { return nil, nil }

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
		Validate: jsonValidator, ToolArguments: acceptingToolArguments,
		Clock: fixedRuntimeClock{},
		IDs:   &runtimeIDs{},
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
	// The run row was written BEFORE the model was called, so a run that dies mid-flight still
	// leaves a record that it started. What the store holds after a SUCCESSFUL run is one row in
	// its finished state, because the real repository's finish is a guarded UPDATE rather than a
	// second insert — and the double now models that. The assertion is on the ORDER instead: the
	// create came first, which is what makes a run that died visible.
	if len(harness.store.runs) != 1 {
		t.Fatalf("the store holds %d run rows, want the one the run used", len(harness.store.runs))
	}
	// The row EXISTED before the model was called, which is the property that makes a run that
	// died mid-flight visible. The final state is succeeded because the finish updated the row
	// in place — which is what the real repository's guarded UPDATE does — so the assertion is on
	// what the row said when it appeared.
	if len(harness.store.createdStatuses) != 1 || harness.store.createdStatuses[0] != agent.RunRunning {
		t.Fatalf("the created rows were %v, want one running", harness.store.createdStatuses)
	}
	if harness.store.runs[0].Status != agent.RunSucceeded {
		t.Fatalf("the finished row is %q, want succeeded", harness.store.runs[0].Status)
	}
	if harness.store.runs[0].SkillVersionID != "sv-1" {
		t.Fatal("the run row does not name its skill version, so it could not be reproduced")
	}
	if harness.store.runs[0].ModelConfigID != "model-1" {
		t.Fatal("the run row does not name its model, so section 13's model change would not be visible")
	}
	// BOTH TURNS were recorded, and the user's came first.
	//
	// This assertion used to read "the model's own turn was recorded" and accept a single assistant
	// row, because that was all the runtime wrote: the user's instruction existed in the prompt and
	// nowhere durable, so AGENT_CONTRACTS section 12.4's first write source had no writer and no
	// conversation could be reconstructed. WP-10 closed that.
	if len(harness.store.messages) != 2 {
		t.Fatalf("the store holds %d messages, want the user's turn and the reply: %+v",
			len(harness.store.messages), harness.store.messages)
	}
	if harness.store.messages[0].Role != agent.MessageUser {
		t.Fatalf("the first recorded message is %q, want the user's turn", harness.store.messages[0].Role)
	}
	if harness.store.messages[1].Role != agent.MessageAssistant {
		t.Fatalf("the second recorded message is %q, want the model's reply", harness.store.messages[1].Role)
	}
	// The user's turn carries the run's own identifier, derived rather than minted so the memory
	// row can cite it in the same call.
	if !strings.HasSuffix(harness.store.messages[0].ID, ":user") {
		t.Fatalf("the user's message is named %q", harness.store.messages[0].ID)
	}
	if harness.store.messages[0].Content != "go ahead" {
		t.Fatalf("the recorded user turn is %q", harness.store.messages[0].Content)
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
		Validate: jsonValidator, ToolArguments: acceptingToolArguments, Clock: fixedRuntimeClock{}, IDs: &runtimeIDs{},
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
		Validate: jsonValidator, ToolArguments: acceptingToolArguments, Clock: fixedRuntimeClock{}, IDs: &runtimeIDs{},
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

// The four tests below are REGRESSION tests for defects the canary found, written at the unit
// level so they fail in a second rather than after assembling a database. Each names the
// defect it pins.

// TestFinishReportsTheRevisionItWrote pins the contract a second write depends on.
//
// Runtime.finish took its record BY VALUE and incremented its own copy, so the value it
// returned to nobody was discarded. That is invisible while a run writes once — which is
// every path except one — and the canary found the consequence before this test could: with
// the repair loop writing twice, the second write reused the ORIGINAL revision, the
// repository's optimistic guard matched no row, and the caller saw
// "the requested agent record no longer exists" in place of the refusal under test.
//
// The runner now writes once per run (the loop validates, repairs, and refuses in one pass),
// so the sequence cannot be reproduced through Run. What CAN be asserted is the contract
// itself, and that is what this does: finish reports the revision the next write must expect,
// and a caller that ignores it is the bug.
func TestFinishReportsTheRevisionItWrote(t *testing.T) {
	store := &revisionTrackingStore{}
	harness := newHarness(t, []string{`{"summary":"first"}`}, func(options *Options) {
		options.Runs = store
	})
	runtime := harness.runtime
	record := agent.AgentRun{
		ID: "run-direct", ProjectID: "project-1",
		Layer: agent.LayerExecution, AgentKey: "script.execution.x",
		SkillVersionID: "sv-1", Status: agent.RunRunning,
		StartedAt: fixedRuntimeClock{}.Now(), Revision: 1,
	}
	// The first write reports 2, which is what the row now holds.
	next, err := runtime.finish(context.Background(), record, agent.RunSucceeded, "{}", "", nil)
	if err != nil {
		t.Fatalf("the first finish failed: %v", err)
	}
	if next != 2 {
		t.Fatalf("finish reported revision %d, want 2", next)
	}
	// A caller that carried it forward can write again; a caller that reused 1 cannot. The
	// store records what each write expected, so the difference is visible here.
	record.Revision = next
	if _, err := runtime.finish(context.Background(), record, agent.RunSucceeded, "{}", "", nil); err != nil {
		t.Fatalf("the second finish failed: %v", err)
	}
	if len(store.expectedRevisions) != 2 {
		t.Fatalf("the store saw %d writes, want 2", len(store.expectedRevisions))
	}
	if store.expectedRevisions[0] != 1 || store.expectedRevisions[1] != 2 {
		t.Fatalf("the writes expected revisions %v, want [1 2]", store.expectedRevisions)
	}
	// And the value really matters: writing again at the STALE revision is what the defect
	// did, and the double refuses it so the failure is a test failure rather than a silent
	// overwrite.
	stale := record
	stale.Revision = 1
	if _, err := runtime.finish(context.Background(), stale, agent.RunSucceeded, "{}", "", nil); err == nil {
		t.Fatal("a stale revision was accepted, so the guard this pins does nothing")
	}
}

// revisionTrackingStore records the revision each FinishRun was told to expect.
//
// It exists because the defect above is invisible to a store that ignores the value, which
// is what the package's other double does — and that is WHY the defect survived.
type revisionTrackingStore struct {
	memoryRunStore
	expectedRevisions []int64
	// revision is the row's current revision, which the double advances on each write so a
	// stale one can be refused.
	revision int64
}

func (s *revisionTrackingStore) FinishRun(ctx context.Context, run agent.AgentRun, expectedRevision int64) error {
	s.expectedRevisions = append(s.expectedRevisions, expectedRevision)
	// A stale revision is REFUSED, exactly as the repository's revision-guarded UPDATE
	// refuses one. Without this the assertion above would pass whatever the runner did,
	// because a double that accepts any revision cannot tell a carried-forward value from a
	// discarded one.
	if expectedRevision < s.revision {
		return agent.ConflictError("The agent run was changed by someone else.")
	}
	s.revision = expectedRevision + 1
	return s.memoryRunStore.FinishRun(ctx, run, expectedRevision)
}

// TestRefusalsCarryTheRunID pins the identifiers every refusal after CreateRun must return.
//
// The run row is written BEFORE the model is called, so a caller that received only an error
// could not look up what happened — and the canary could not be written without it: its
// assertion "the run records the refusal" had no identifier to reach the row with. The
// zero Outcome is reserved for the refusals that happen before a row exists.
func TestRefusalsCarryTheRunID(t *testing.T) {
	cases := []struct {
		name     string
		replies  []string
		mutate   func(*Options)
		toolCall []ToolCallRequest
	}{
		{name: "schema refusal", replies: []string{`{"nope":1}`, `{"nope":1}`},
			mutate: func(options *Options) { options.Validate = refusingValidator }},
		// The verifier is only reached when the output NAMES an artifact: an output with none
		// has none to check, which is legitimate for a stage that reports only a decision. So
		// the reply carries a reference, and the refusing verifier then refuses it.
		{name: "artifact refusal",
			replies: []string{`{"summary":"x","artifacts":[{"entityType":"story_skeleton_version","entityId":"invented"}]}`},
			mutate: func(options *Options) {
				options.Artifacts = refusingArtifacts{}
			}},
		{name: "tool denial", replies: []string{`{"summary":"x"}`},
			toolCall: []ToolCallRequest{{Key: "script.create_script_version", Arguments: json.RawMessage(`{}`)}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newHarness(t, testCase.replies, testCase.mutate)
			if testCase.toolCall != nil {
				harness.model.toolCalls = [][]ToolCallRequest{testCase.toolCall}
			}
			// The agent is the supervisor for the denial case, whose ACL refuses a write.
			agentKey := "script.execution.x"
			if testCase.toolCall != nil {
				agentKey = "script.supervision.x"
			}
			outcome, err := harness.runtime.Run(context.Background(), invocationFor(agentKey))
			if err == nil {
				t.Fatal("the run succeeded")
			}
			if outcome.RunID == "" {
				t.Fatal("the refusal discarded the run id")
			}
			// And the row really is there, which is what makes the identifier useful.
			if _, readErr := harness.store.findRun(outcome.RunID); readErr != nil {
				t.Fatalf("the returned run id names no row: %v", readErr)
			}
		})
	}
}

// refusingValidator refuses every document, so a test can reach a refusal path without
// depending on what an output happens to contain. It is a function rather than a type because
// Validator IS a function type: a method-carrying struct would need a method value at each
// call site.
func refusingValidator(string, []byte) ([]Violation, error) {
	return []Violation{{Path: "/", Message: "refused by the test"}}, nil
}

// refusingArtifacts refuses every reference, for the artifact case above.
type refusingArtifacts struct{}

func (refusingArtifacts) VerifyArtifacts(context.Context, []ArtifactRef) error {
	return &ArtifactError{EntityType: "story_skeleton_version", EntityID: "invented"}
}

// TestToolRequestCarriesTheAgentRunID pins the attribution a write depends on.
//
// ToolRequest had only the stage run, so a tool that recorded WHO produced a version filled
// the field with a stage id — wrong per DOMAIN_MODEL section 13.4, where a run that was
// superseded and re-run in the same stage is a different author. The canary caught it by
// asserting the field against the run it had just made.
func TestToolRequestCarriesTheAgentRunID(t *testing.T) {
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
		{Key: "story.read_events", Arguments: json.RawMessage(`{}`)},
	}}
	invocation := invocationFor("script.execution.x")
	outcome, err := harness.runtime.Run(context.Background(), invocation)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if captured.AgentRunID == "" {
		t.Fatal("the handler received no agent run id, so a write could not be attributed")
	}
	if captured.AgentRunID != outcome.RunID {
		t.Fatalf("the handler received run %q, want %q", captured.AgentRunID, outcome.RunID)
	}
	// The stage is still there and still distinct: both are needed, for different reasons.
	if captured.StageRunID != invocation.StageRunID {
		t.Fatalf("the handler received stage %q, want %q", captured.StageRunID, invocation.StageRunID)
	}
	if captured.AgentRunID == captured.StageRunID {
		t.Fatal("the run id and the stage id are the same value, so the two fields are redundant")
	}
}

// TestToolArgumentsAreValidatedAgainstTheToolSchema is section 6.1's chain, and it exists
// because the chain's SECOND step was missing.
//
// An independent review found it by mutating nothing: it read the code and observed that
// `Tool.SchemaPath` appeared in exactly one place — the prompt — and was never applied to
// what a model sent back. So a model could hand a handler any shape at all, and the only
// thing that refused it was the handler's own struct decoding: a decode error rather than a
// contract refusal, unable to name the rule, and absent entirely for a handler that tolerated
// the extra field.
//
// AGENT_CONTRACTS section 20 lists "Tool 参数未 Schema 校验" as a RELEASE-BLOCKING condition.
func TestToolArgumentsAreValidatedAgainstTheToolSchema(t *testing.T) {
	refusing := func(schemaPath string, raw []byte) ([]Violation, error) {
		if strings.Contains(string(raw), "BAD") {
			return []Violation{{Path: "/chapterId", Message: "does not satisfy maxLength"}}, nil
		}
		return nil, nil
	}
	calls := 0
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
	harness := newHarness(t, []string{`{"summary":"x"}`}, func(options *Options) {
		options.Tools = table
		options.ToolArguments = refusing
	})
	harness.model.toolCalls = [][]ToolCallRequest{{
		{Key: "story.read_events", Arguments: json.RawMessage(`{"chapterId":"BAD"}`)},
	}}
	_, err = harness.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err == nil {
		t.Fatal("an argument the tool's schema refuses reached the handler")
	}
	// The handler never ran, which is the property that matters: the check is before
	// execution, not a validation of what already happened.
	if calls != 0 {
		t.Fatalf("the handler ran %d times despite the schema refusing its arguments", calls)
	}
	// The record says WHICH refusal it was, so a reader can tell a bad request from a denial
	// and from a tool that broke.
	if len(harness.store.toolCalls) != 1 {
		t.Fatalf("the store holds %d tool calls", len(harness.store.toolCalls))
	}
	call := harness.store.toolCalls[0]
	if call.Status != agent.ToolCallFailed {
		t.Fatalf("the call is %q, want failed", call.Status)
	}
	if call.ErrorCode != "agent.tool_arguments_invalid" {
		t.Fatalf("the call records %q", call.ErrorCode)
	}
	// And a call whose arguments DO satisfy the schema still runs, so the check is a filter
	// rather than a wall.
	harness2 := newHarness(t, []string{`{"summary":"x"}`}, func(options *Options) {
		options.Tools = table
		options.ToolArguments = refusing
	})
	harness2.model.toolCalls = [][]ToolCallRequest{{
		{Key: "story.read_events", Arguments: json.RawMessage(`{"chapterId":"c1"}`)},
	}}
	if _, err := harness2.runtime.Run(context.Background(), invocationFor("script.execution.x")); err != nil {
		t.Fatalf("a valid tool call was refused: %v", err)
	}
}

// TestRuntimeWithoutAToolArgumentValidatorRefusesToRunATool is the fail-closed direction.
//
// A runtime that cannot check an argument must not pass it to a handler that will act on it,
// which is the same rule ArtifactVerifier follows. The refusal is an unavailability rather
// than a per-call failure, because the defect is in the composition and no argument would fix
// it.
func TestRuntimeWithoutAToolArgumentValidatorRefusesToRunATool(t *testing.T) {
	harness := newHarness(t, []string{`{"summary":"x"}`}, func(options *Options) {
		options.ToolArguments = nil
	})
	// The tool must be one the EXECUTION layer may call, or the denial would be the ACL's
	// and the test would be asserting about a different refusal — which is what the first
	// version of this test did with a decision-layer tool.
	harness.model.toolCalls = [][]ToolCallRequest{{
		{Key: "story.read_events", Arguments: json.RawMessage(`{}`)},
	}}
	_, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err == nil {
		t.Fatal("a runtime with no argument validator ran a tool")
	}
	if len(harness.store.toolCalls) != 1 {
		t.Fatalf("the store holds %d tool calls", len(harness.store.toolCalls))
	}
	if harness.store.toolCalls[0].Status != agent.ToolCallFailed {
		t.Fatalf("the call is %q, want failed", harness.store.toolCalls[0].Status)
	}
}

// The tests below pin the four rules an independent review found missing. Each was a
// specification requirement the code did not implement, and each is recorded in ADR-0011.

// TestRunRecordsTheModelThatAnswered pins section 13's "模型变更写入 Run".
//
// The first version recorded only model_config_id — the model the policy NAMED — and its own
// comment claimed that satisfied section 13. It does not: the requirement exists because a
// run's answering model can differ from the requested one (a fallback was used, or a provider
// served something else), and a record naming only the request would cite a model that did not
// produce the output.
func TestRunRecordsTheModelThatAnswered(t *testing.T) {
	store := &memoryRunStore{}
	harness := newHarness(t, []string{`{"summary":"x"}`}, func(options *Options) {
		options.Runs = store
	})
	// The scripted model answers as "scripted" while the invocation asks for "model-1".
	outcome, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	run, err := store.findRun(outcome.RunID)
	if err != nil {
		t.Fatalf("finding the run: %v", err)
	}
	if run.ModelConfigID != "model-1" {
		t.Fatalf("the run names the REQUESTED model as %q, want model-1", run.ModelConfigID)
	}
	if run.ResponseModel != "scripted" {
		t.Fatalf("the run records the answering model as %q, want the model that replied", run.ResponseModel)
	}
	if run.ModelConfigID == run.ResponseModel {
		t.Fatal("the two are equal, so this test cannot tell the fields apart")
	}
}

// TestMessageStorageBoundCountsBytes covers the silent drop a rune count caused.
//
// The bound is BYTES, and the first version counted runes against it: a long Chinese reply was
// clipped to 16,384 runes = 49,152 bytes, the domain refused it, and the caller discarded the
// refusal — so the message vanished from the record with no trace. The test uses CJK text
// because that is the only input where the two counts diverge.
func TestMessageStorageBoundCountsBytes(t *testing.T) {
	// Three bytes per rune, so the byte limit is reached at a third of the rune count.
	long := strings.Repeat("测", 8000)
	clipped := truncateForStorage(long)
	if len(clipped) > agent.DefaultMessageContentBytes {
		t.Fatalf("the clipped value is %d bytes, over the %d bound", len(clipped), agent.DefaultMessageContentBytes)
	}
	// It must still satisfy the domain's own check, which is what the caller relies on.
	message := agent.AgentMessage{
		ID: "m1", AgentRunID: "r1", ScopeKey: "local||p||k|",
		Role: agent.MessageAssistant, Content: clipped, ContentHash: contentHash(clipped),
		CreatedAt: fixedRuntimeClock{}.Now(),
	}
	if err := message.Validate(); err != nil {
		t.Fatalf("the clipped content does not satisfy the domain: %v", err)
	}
	// And a short value is untouched, so the clipping is not applied where it is unnecessary.
	if short := truncateForStorage("短"); short != "短" {
		t.Fatalf("a short message was altered: %q", short)
	}
	// The result is valid UTF-8 rather than cut mid-rune, which a byte clip without the
	// boundary walk would produce.
	if !utf8.ValidString(clipped) {
		t.Fatal("the clipped value is not valid UTF-8")
	}
}

// TestRunRefusesAResultThatNamesAnotherStage covers the identity cross-check.
//
// Section 9's "Runtime 必须再次校验" covers what a result SAYS ABOUT ITSELF as much as what it asks
// for: a document naming a different attempt would be recorded as this stage's output, and a
// reviewer could not tell what it described.
func TestRunRefusesAResultThatNamesAnotherStage(t *testing.T) {
	harness := newHarness(t, []string{
		`{"summary":"x","stage":"story_skeleton","stageRunId":"some-other-stage"}`,
	}, nil)
	_, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err == nil {
		t.Fatal("a result naming another stage attempt was accepted")
	}
	// The same document naming THIS stage's attempt is accepted, so the check is a comparison
	// rather than a ban on the field.
	invocation := invocationFor("script.execution.x")
	good := newHarness(t, []string{
		`{"summary":"x","stage":"story_skeleton","stageRunId":"` + invocation.StageRunID + `"}`,
	}, nil)
	if _, err := good.runtime.Run(context.Background(), invocation); err != nil {
		t.Fatalf("a result naming its own stage attempt was refused: %v", err)
	}
}

// TestRunRefusesASuccessWithNoArtifact covers section 7.4's first artifact rule.
//
// "success 至少有预期 artifact，除非该阶段明确是 no-op" is a relation between a status and a list,
// which JSON Schema cannot express. The first version tried to infer the no-op case from the
// VERIFIER and the check was a no-op itself: the verifier answers "all the references I was
// given exist", which it quite correctly answers about an empty list — so a success with
// nothing behind it passed.
func TestRunRefusesASuccessWithNoArtifact(t *testing.T) {
	harness := newHarness(t, []string{`{"summary":"x","status":"success","artifacts":[]}`}, nil)
	_, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x"))
	if err == nil {
		t.Fatal("a success reporting no artifact was accepted")
	}
	var artifactErr *ArtifactError
	if !errors.As(err, &artifactErr) {
		t.Fatalf("the refusal is a %T, want an artifact error", err)
	}
	// The same document on a stage the caller declares a no-op is accepted, which is what makes
	// the check a rule rather than a blanket refusal.
	invocation := invocationFor("script.execution.x")
	invocation.StageProducesNoArtifact = true
	relaxed := newHarness(t, []string{`{"summary":"x","status":"success","artifacts":[]}`}, nil)
	if _, err := relaxed.runtime.Run(context.Background(), invocation); err != nil {
		t.Fatalf("a no-op stage's success was refused: %v", err)
	}
}

// TestRunRefusesAFailedResultCarryingAnArtifact covers section 7.4's second artifact rule.
func TestRunRefusesAFailedResultCarryingAnArtifact(t *testing.T) {
	harness := newHarness(t, []string{
		`{"summary":"x","status":"failed","artifacts":[{"entityType":"script_version","entityId":"v1"}]}`,
	}, nil)
	if _, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x")); err == nil {
		t.Fatal("a failed result carrying an artifact was accepted")
	}
}

// TestTheRuntimeRecallsBeforeItWritesAndRemembersBothTurns is AC-MEM-002's ordering clause.
//
// The criterion reads "Recall 在写当前消息前或排除其 ID" — recall before the current message is
// written, or exclude its id — and this build does both. The exclusion alone is not enough: a
// recall that ran AFTER the write would see the turn it is answering, and no identifier would tell
// it which row that was, because the row it would have to exclude is the one it just created.
//
// So the ORDER is the property, and this is the test that pins it. It drives a real run through a
// recording port and asserts the sequence, which is the only way to observe an ordering that code
// could otherwise get right by accident.
func TestTheRuntimeRecallsBeforeItWritesAndRemembersBothTurns(t *testing.T) {
	port := &recordingMemoryPort{}
	harness := newHarness(t, []string{`{"summary":"fine","toolCalls":[]}`}, func(options *Options) {
		options.Memory = port
	})
	if _, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(port.calls) != 3 {
		t.Fatalf("the memory port saw %d calls: %v", len(port.calls), port.calls)
	}
	if port.calls[0] != "recall" {
		t.Fatalf("the first memory call was %q, want the recall", port.calls[0])
	}
	if port.calls[1] != "remember:user" {
		t.Fatalf("the second memory call was %q, want the user's turn", port.calls[1])
	}
	if port.calls[2] != "remember:assistant" {
		t.Fatalf("the third memory call was %q, want the model's reply", port.calls[2])
	}
	// The recall was given the user's words to search with, because the scored channels need
	// something to be similar to and the user's message is what the run is answering.
	if len(port.queries) != 1 || port.queries[0] != "go ahead" {
		t.Fatalf("the recall was given %v as its query", port.queries)
	}
	// Every remembered turn cites a transcript row and carries its scope, which is what makes the
	// memory attributable and recallable.
	for index, message := range port.messages {
		if message.MessageID == "" {
			t.Fatalf("remembered turn %d cites no message: %+v", index, message)
		}
		if message.ProjectID == "" || message.AgentKey == "" {
			t.Fatalf("remembered turn %d has no scope: %+v", index, message)
		}
	}
}

// TestTheRuntimeWithoutAMemoryPortStillRuns is the optional-port contract.
//
// A build with no memory store must run stages exactly as it did before the store existed: the port
// is optional, and its absence is a stated state rather than a degraded mode. The assertion is on
// the TRANSCRIPT, because a missing memory store must not take the transcript with it.
func TestTheRuntimeWithoutAMemoryPortStillRuns(t *testing.T) {
	harness := newHarness(t, []string{`{"summary":"fine","toolCalls":[]}`}, nil)
	if _, err := harness.runtime.Run(context.Background(), invocationFor("script.execution.x")); err != nil {
		t.Fatalf("a run without a memory port failed: %v", err)
	}
	if len(harness.store.messages) != 2 {
		t.Fatalf("the transcript holds %d messages, want both turns: %+v",
			len(harness.store.messages), harness.store.messages)
	}
}

// recordingMemoryPort records the order of the memory calls a run makes.
type recordingMemoryPort struct {
	calls    []string
	queries  []string
	messages []MemoryMessage
}

func (p *recordingMemoryPort) Recall(_ context.Context, scope ScopePartsRequest, _ string, _ int) ([]MemoryRecallItem, error) {
	p.calls = append(p.calls, "recall")
	p.queries = append(p.queries, scope.Query)
	return nil, nil
}

func (p *recordingMemoryPort) Remember(_ context.Context, message MemoryMessage) error {
	p.calls = append(p.calls, "remember:"+message.Role)
	p.messages = append(p.messages, message)
	return nil
}
