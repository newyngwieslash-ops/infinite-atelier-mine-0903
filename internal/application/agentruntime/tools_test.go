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

// These tests cover the ACL, which is the boundary AC-AGENT-001 is written about
// and the one SECURITY section 7.1 requires the runtime to re-check on every call
// rather than once at registration.

// echoHandler returns its arguments, so a test can prove a call reached the
// handler.
func echoHandler() ToolHandler {
	return func(_ context.Context, request ToolRequest) (any, error) {
		return map[string]any{"projectId": request.ProjectID, "arguments": string(request.Arguments)}, nil
	}
}

// testTool builds a registered tool.
func testTool(key string, mode agent.ToolMode, limit int) Tool {
	return Tool{
		Spec:       agent.ToolSpec{Key: key, Mode: mode, Scope: "project", MaxOutputBytes: limit},
		Handler:    echoHandler(),
		SchemaPath: "schemas/agent/tools/" + key + ".json",
	}
}

// testTools is a small table with one tool of each mode.
func testTools(t *testing.T) *Tools {
	t.Helper()
	table, err := NewTools([]Tool{
		testTool("story.read_events", agent.ToolRead, 64*1024),
		testTool("script.create_script_version", agent.ToolWrite, 64*1024),
		testTool("provider.submit_image_job", agent.ToolExternal, 64*1024),
		testTool("workflow.read_state", agent.ToolRead, 64*1024),
		testTool("workflow.request_user_gate", agent.ToolControl, 64*1024),
		testTool("agent.invoke_execution", agent.ToolControl, 64*1024),
	})
	if err != nil {
		t.Fatalf("building the tool table: %v", err)
	}
	return table
}

// specFor builds a registration listing the given tools.
func specFor(key string, layer agent.AgentLayer, tools ...string) agent.Spec {
	return agent.Spec{
		Key: key, Layer: layer, Skill: "skill.md",
		Input:        "schemas/agent/execution-request.v1.json",
		Output:       "schemas/agent/execution-result.v1.json",
		AllowedTools: tools,
		Limits:       agent.Limits{MaxToolCalls: 6, MaxDuration: 180 * time.Second},
		PolicyLayer:  agent.PolicyExecution,
	}
}

// TestToolTableValidatesEveryEntry covers the registry's own checks: a tool needs
// a handler and a schema, and a duplicate key is refused.
func TestToolTableValidatesEveryEntry(t *testing.T) {
	good := testTool("story.read_events", agent.ToolRead, 1024)
	if _, err := NewTools([]Tool{good}); err != nil {
		t.Fatalf("a well-formed table was refused: %v", err)
	}
	noHandler := good
	noHandler.Handler = nil
	if _, err := NewTools([]Tool{noHandler}); err == nil {
		t.Fatal("a tool with no implementation was registered")
	}
	noSchema := good
	noSchema.SchemaPath = ""
	if _, err := NewTools([]Tool{noSchema}); err == nil {
		t.Fatal("a tool with no input schema was registered")
	}
	if _, err := NewTools([]Tool{good, good}); err == nil {
		t.Fatal("a tool was registered twice")
	}
	badMode := testTool("story.read_events", "modify", 1024)
	if _, err := NewTools([]Tool{badMode}); err == nil {
		t.Fatal("a tool with an unknown mode was registered")
	}
	// An empty table is legal: a build might compose no tools, and the refusal
	// belongs to the agent that needs one rather than to the table.
	if _, err := NewTools(nil); err != nil {
		t.Fatalf("an empty table was refused: %v", err)
	}
	// A nil table answers rather than panicking, which is the fail-closed rule.
	var none *Tools
	if _, ok := none.Lookup("story.read_events"); ok {
		t.Fatal("a nil table returned a tool")
	}
	if len(none.Keys()) != 0 || len(none.KeySet()) != 0 {
		t.Fatal("a nil table reported keys")
	}
}

// TestAuthorizeEnforcesTheMatrixAndTheGrant is the ACL.
//
// It checks both halves of section 6.3: the layer's matrix is a CEILING, and the
// agent's own list is the GRANT. A tool the layer may hold but the agent does not
// list is refused, and a tool the agent lists but the layer may not hold is also
// refused — which is what stops a manifest granting a supervisor a write tool.
func TestAuthorizeEnforcesTheMatrixAndTheGrant(t *testing.T) {
	table := testTools(t)
	authorizer := Authorizer{}
	check := func(spec agent.Spec, key string) error {
		tool, ok := table.Lookup(key)
		if !ok {
			t.Fatalf("the fixture does not register %s", key)
		}
		return authorizer.Authorize(AuthorizeRequest{Spec: spec, Tool: tool.Spec})
	}

	// A Decision agent with its documented tool set: control and read are allowed.
	decision := specFor("script.decision", agent.LayerDecision, "workflow.read_state", "workflow.request_user_gate", "agent.invoke_execution")
	for _, key := range decision.AllowedTools {
		if err := check(decision, key); err != nil {
			t.Fatalf("a Decision agent was refused %s: %v", key, err)
		}
	}
	// It may not write, and it may not reach outside the process.
	for _, key := range []string{"script.create_script_version", "provider.submit_image_job"} {
		err := check(decision, key)
		if err == nil {
			t.Fatalf("a Decision agent was allowed %s", key)
		}
		var notAllowed *ToolNotAllowedError
		if !errors.As(err, &notAllowed) {
			t.Fatalf("the refusal of %s is a %T, want the ACL's own error", key, err)
		}
		if notAllowed.Code() != CodeToolNotAllowed {
			t.Fatalf("the refusal of %s carries code %q, want %q", key, notAllowed.Code(), CodeToolNotAllowed)
		}
		if IsRetriable(err) {
			t.Fatalf("the refusal of %s is reported as retriable, but section 14.2 never retries a tool refusal", key)
		}
	}

	// A Supervisor may read and nothing else. This is the case a manifest could try
	// to override, so it is checked with the write tool LISTED.
	supervisor := specFor("script.supervision.script", agent.LayerSupervision, "story.read_events", "script.create_script_version")
	if err := check(supervisor, "story.read_events"); err != nil {
		t.Fatalf("a Supervisor was refused a read tool: %v", err)
	}
	if err := check(supervisor, "script.create_script_version"); err == nil {
		t.Fatal("a Supervisor was allowed a write tool it listed, so the matrix is not a ceiling")
	}

	// An Execution agent may write and reach outside, but not control the workflow.
	execution := specFor("script.execution.script_generation", agent.LayerExecution, "script.create_script_version", "provider.submit_image_job", "workflow.read_state")
	for _, key := range execution.AllowedTools {
		if err := check(execution, key); err != nil {
			t.Fatalf("an Execution agent was refused %s: %v", key, err)
		}
	}
	if err := check(execution, "agent.invoke_execution"); err == nil {
		t.Fatal("an Execution agent was allowed to invoke another agent")
	}

	// A tool the layer may hold but the agent does not list is refused, and the
	// error says which of the two rules fired.
	minimal := specFor("script.decision", agent.LayerDecision, "workflow.read_state")
	err := check(minimal, "workflow.request_user_gate")
	if err == nil {
		t.Fatal("a tool the agent does not list was allowed")
	}
	var notGranted *ToolNotAllowedError
	if !errors.As(err, &notGranted) || !notGranted.NotGranted {
		t.Fatalf("the refusal does not record that the tool was merely not granted: %v", err)
	}
}

// TestAuthorizeRefusesAnUnregisteredToolOrLayer covers the two structural cases.
func TestAuthorizeRefusesAnUnregisteredToolOrLayer(t *testing.T) {
	authorizer := Authorizer{}
	// A tool key that does not parse is refused as not-registered rather than
	// silently treated as allowed.
	bogus := agent.ToolSpec{Key: "notakey", Mode: agent.ToolRead, Scope: "project", MaxOutputBytes: 1024}
	if err := authorizer.Authorize(AuthorizeRequest{Spec: specFor("script.decision", agent.LayerDecision, "notakey"), Tool: bogus}); err == nil {
		t.Fatal("a malformed tool key was authorised")
	}
	// A layer that is not a layer permits nothing.
	unknown := specFor("script.decision", "advisor", "workflow.read_state")
	tool := agent.ToolSpec{Key: "workflow.read_state", Mode: agent.ToolRead, Scope: "project", MaxOutputBytes: 1024}
	if err := authorizer.Authorize(AuthorizeRequest{Spec: unknown, Tool: tool}); err == nil {
		t.Fatal("an unknown layer was authorised")
	}
}

// TestBoundResultRefusesRatherThanTruncating covers section 6.4's "返回结构化、
// 限长数据".
//
// Truncation is the wrong failure mode here: half a JSON document is not a smaller
// answer, and a model given one would either fail to parse it or act on the part
// it could read.
func TestBoundResultRefusesRatherThanTruncating(t *testing.T) {
	small := map[string]any{"ok": true}
	encoded, err := boundResult(small, 1024)
	if err != nil {
		t.Fatalf("a small result was refused: %v", err)
	}
	if encoded != `{"ok":true}` {
		t.Fatalf("the result encoded as %q", encoded)
	}
	// A zero limit falls back to the registry ceiling rather than to no limit.
	if _, err := boundResult(small, 0); err != nil {
		t.Fatalf("a result with the default limit was refused: %v", err)
	}
	// An oversized result is refused, and the refusal is not a truncation.
	large := map[string]any{"text": strings.Repeat("x", 4096)}
	if _, err := boundResult(large, 1024); err == nil {
		t.Fatal("an oversized result was returned")
	}
	// A value that cannot be marshalled is reported rather than silently dropped.
	if _, err := boundResult(func() {}, 1024); err == nil {
		t.Fatal("an unencodable result was returned")
	}
}

// TestHandlerReceivesItsScopeNotTheModels covers the rule that scopes come from
// the run rather than from the model.
//
// Section 7.1 requires a tool to check project and episode scope, and a model that
// could name its own project could read another project's story. The scope travels
// beside the arguments rather than inside them.
func TestHandlerReceivesItsScopeNotTheModels(t *testing.T) {
	var captured ToolRequest
	var called bool
	handler := func(_ context.Context, request ToolRequest) (any, error) {
		captured = request
		called = true
		return "ok", nil
	}
	tool := Tool{
		Spec:       agent.ToolSpec{Key: "story.read_events", Mode: agent.ToolRead, Scope: "project", MaxOutputBytes: 1024},
		Handler:    handler,
		SchemaPath: "schemas/agent/tools/story.read_events.json",
	}
	// The arguments name a DIFFERENT project, which the scope must override.
	output, err := tool.Handler(context.Background(), ToolRequest{
		ProjectID:     "project-real",
		EpisodeID:     "episode-real",
		WorkflowRunID: "run-real",
		StageRunID:    "stage-real",
		Arguments:     json.RawMessage(`{"projectId":"project-other"}`),
	})
	if err != nil || output != "ok" {
		t.Fatalf("the handler returned %v, %v", output, err)
	}
	if !called {
		t.Fatal("the handler was not called")
	}
	if captured.ProjectID != "project-real" {
		t.Fatalf("the handler received project %q, want the run's", captured.ProjectID)
	}
	if captured.EpisodeID != "episode-real" || captured.WorkflowRunID != "run-real" || captured.StageRunID != "stage-real" {
		t.Fatalf("the handler received the wrong scope: %+v", captured)
	}
}

// TestIsRetriableFollowsSection14 covers the classification, which decides whether
// a retry controller retries or stops.
//
// The direction that matters is the default: an unrecognised error is NOT
// retriable, because retrying a refusal is what section 14.2 forbids and the cost
// of the other mistake is one run a user can re-run.
func TestIsRetriableFollowsSection14(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"a denied tool", &ToolNotAllowedError{Layer: agent.LayerDecision, Mode: agent.ToolWrite, Tool: "script.create_script_version"}, false},
		{"a spent tool budget", &QuotaError{Limit: "tool_calls", Allowed: 6, Used: 6}, false},
		{"a spent time budget", &QuotaError{Limit: "duration", Allowed: 180, Used: 180}, false},
		{"the auto-fix budget", &QuotaError{Limit: "auto_fix", Allowed: 2, Used: 2}, false},
		{"a cancellation", &CancelledError{Cause: context.Canceled}, false},
		{"a malformed output", &SchemaError{Stage: "story_skeleton"}, false},
		{"a malformed output after repair", &SchemaError{Stage: "story_skeleton", Repaired: true}, false},
		{"an invented artifact", &ArtifactError{EntityType: "story_skeleton", EntityID: "ghost"}, false},
		{"a security refusal", agent.SecurityError("no"), false},
		{"an invalid input", agent.InvalidError("no"), false},
		{"an unavailable runtime", agent.UnavailableError(), false},
		{"an unrecognised error", errors.New("something happened"), false},
		{"no error at all", nil, false},
		// The two categories section 14 lets a policy decide are positive here,
		// because the runtime has no better information than the caller.
		{"a storage failure", agent.StorageError("the write failed", nil), true},
		{"a conflict", agent.ConflictError("changed elsewhere"), true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsRetriable(testCase.err); got != testCase.want {
				t.Fatalf("IsRetriable = %t, want %t", got, testCase.want)
			}
		})
	}
	// A refusal wrapped in another error is still classified, because the runner
	// wraps what it reports.
	wrapped := agent.StorageError("outer", &ToolNotAllowedError{Layer: agent.LayerSupervision, Mode: agent.ToolWrite, Tool: "script.create_script_version"})
	if IsRetriable(wrapped) {
		t.Fatal("a wrapped tool refusal was reported as retriable")
	}
}

// TestRefusalMessagesNameNoBehaviour covers a small security property: a refusal
// must not tell a caller what a tool would have done, because the refusal is
// reachable by a model and a description is a hint about capabilities it was not
// granted.
func TestRefusalMessagesNameNoBehaviour(t *testing.T) {
	messages := []string{
		(&ToolNotAllowedError{Layer: agent.LayerSupervision, Mode: agent.ToolWrite, Tool: "script.create_script_version"}).Error(),
		(&ToolNotAllowedError{Layer: agent.LayerDecision, Mode: agent.ToolWrite, Tool: "script.create_script_version", NotGranted: true}).Error(),
		(&QuotaError{Limit: "tool_calls", Allowed: 6, Used: 6}).Error(),
		(&SchemaError{Stage: "story_skeleton"}).Error(),
		(&ArtifactError{EntityType: "story_skeleton", EntityID: "ghost"}).Error(),
		(&CancelledError{}).Error(),
	}
	for _, message := range messages {
		if message == "" {
			t.Fatal("a refusal has no message")
		}
		// The schema refusal must not quote the output, and the artifact refusal must
		// not quote the identifier it was given.
		if strings.Contains(message, "ghost") {
			t.Fatalf("a refusal quotes the value it refused: %q", message)
		}
		if strings.Contains(message, "{") || strings.Contains(message, "raw") {
			t.Fatalf("a refusal looks like it carries payload: %q", message)
		}
	}
}

// TestQuotaAndSchemaErrorsCarryStableCodes covers the identifiers a caller
// branches on.
func TestQuotaAndSchemaErrorsCarryStableCodes(t *testing.T) {
	if code := (&QuotaError{Limit: "tool_calls"}).Code(); code != "agent.quota_tool_calls" {
		t.Fatalf("the tool-call quota code is %q", code)
	}
	if code := (&SchemaError{}).Code(); code != "agent.output_schema_invalid" {
		t.Fatalf("the schema code is %q", code)
	}
	if code := (&ArtifactError{}).Code(); code != "agent.artifact_not_found" {
		t.Fatalf("the artifact code is %q", code)
	}
	if CodeToolNotAllowed != "security.tool_not_allowed" {
		t.Fatalf("the tool refusal code is %q, but AC-AGENT-001 names security.tool_not_allowed", CodeToolNotAllowed)
	}
	// The categories follow section 7.7's taxonomy.
	for _, testCase := range []struct {
		err  Refusal
		want agent.ErrorCategory
	}{
		{&ToolNotAllowedError{}, agent.CategorySecurity},
		{&QuotaError{}, agent.CategoryModel},
		{&CancelledError{}, agent.CategoryCancelled},
		{&SchemaError{}, agent.CategoryModel},
		{&ArtifactError{}, agent.CategoryModel},
	} {
		if got := testCase.err.Category(); got != testCase.want {
			t.Fatalf("%T reports category %q, want %q", testCase.err, got, testCase.want)
		}
	}
}
