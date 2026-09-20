package agent

import (
	"strings"
	"testing"
	"time"
)

// These tests cover the vocabulary and the invariants, which is where the
// specification states its rules as rules. AC-AGENT-001's first three bullets and
// AC-AGENT-005's bound are enforced here rather than in the runtime's control
// flow, so this is where they are tested.

// TestToolMatrixIsSection63 covers the permission matrix.
//
// It is written as a table of the matrix's own cells rather than as assertions
// about individual modes, so a change to ToolAllowed that opens a cell has to
// change this table too — and the table is the specification's.
func TestToolMatrixIsSection63(t *testing.T) {
	// Read yes, Write yes, External yes, Control yes per (layer, mode). The
	// specification's matrix, transcribed.
	matrix := map[AgentLayer]map[ToolMode]bool{
		LayerDecision:    {ToolRead: true, ToolWrite: false, ToolExternal: false, ToolControl: true},
		LayerExecution:   {ToolRead: true, ToolWrite: true, ToolExternal: true, ToolControl: false},
		LayerSupervision: {ToolRead: true, ToolWrite: false, ToolExternal: false, ToolControl: false},
	}
	for layer, row := range matrix {
		for mode, want := range row {
			if got := ToolAllowed(layer, mode); got != want {
				t.Fatalf("ToolAllowed(%s, %s) = %t, want %t", layer, mode, got, want)
			}
		}
	}
	// The three cells the specification calls out in prose, asserted separately so
	// a reader can find them: Decision has no business write tool, Supervisor has no
	// write at all, Execution cannot choose the workflow's next stage.
	if ToolAllowed(LayerDecision, ToolWrite) {
		t.Fatal("a Decision agent may hold a business write tool")
	}
	if ToolAllowed(LayerSupervision, ToolWrite) {
		t.Fatal("a Supervisor may hold a write tool by default")
	}
	if ToolAllowed(LayerExecution, ToolControl) {
		t.Fatal("an Execution agent may choose the workflow's next stage")
	}
	// An unknown layer or mode is refused rather than defaulted, so a typo cannot
	// grant a permission.
	if ToolAllowed("decide", ToolRead) {
		t.Fatal("an unknown layer was allowed a read tool")
	}
	if ToolAllowed(LayerExecution, "delete") {
		t.Fatal("an unknown mode was allowed")
	}
}

// TestToolKeyShapeIsSection62 covers `<domain>.<verb>_<object>`.
func TestToolKeyShapeIsSection62(t *testing.T) {
	for _, valid := range []string{
		"story.read_events",
		"script.create_story_skeleton_version",
		"workflow.read_state",
		"agent.invoke_execution",
		"memory.deep_recall",
		"provider.submit_image_job",
	} {
		if err := ValidateToolKey(valid); err != nil {
			t.Fatalf("the documented key %q was refused: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"",                         // empty
		"  ",                       // blank
		"story",                    // no action
		"story.read",               // the action names no object
		"story..read_x",            // empty segment
		"Story.read_x",             // upper case in the domain
		"story.Read_X",             // upper case in the action
		"story.read-events",        // hyphen instead of underscore
		"story.read_events.detail", // more than one dot
		"story .read_x",            // a space
	} {
		if err := ValidateToolKey(invalid); err == nil {
			t.Fatalf("the tool key %q was accepted", invalid)
		}
	}
	// The bound is inclusive at the limit, like the schema's own length checks. The
	// suffix "_b" is part of the key, so the padding accounts for it: the first
	// version of this test forgot to and asserted a key one character too long.
	const suffix = "_b"
	long := "story." + strings.Repeat("a", MaxToolKeyLength-len("story.")-len(suffix)) + suffix
	if len(long) != MaxToolKeyLength {
		t.Fatalf("the fixture is %d characters, not the %d it means to test", len(long), MaxToolKeyLength)
	}
	if err := ValidateToolKey(long); err != nil {
		t.Fatalf("a key of exactly the limit was refused: %v", err)
	}
	if err := ValidateToolKey(long + "c"); err == nil {
		t.Fatal("a key over the limit was accepted")
	}
}

// TestAgentKeyNamesItsLayer covers the rule that a key and its layer cannot
// disagree, which is what makes an agent's identity single-valued.
func TestAgentKeyNamesItsLayer(t *testing.T) {
	valid := Spec{
		Key: "script.decision", Layer: LayerDecision, Skill: "decision.md",
		Input:       "schemas/agent/decision-request.v1.json",
		Output:      "schemas/agent/decision-result.v1.json",
		Limits:      Limits{MaxToolCalls: 6, MaxDuration: 180 * time.Second},
		PolicyLayer: PolicyDecision,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a well-formed registration was refused: %v", err)
	}
	// The second segment must be the layer.
	mismatched := valid
	mismatched.Key = "script.execution.decision"
	if err := mismatched.Validate(); err == nil {
		t.Fatal("a key whose layer segment contradicts its layer field was accepted")
	}
	// A one-segment key names no layer, so it is refused.
	bare := valid
	bare.Key = "script"
	if err := bare.Validate(); err == nil {
		t.Fatal("a key with no layer segment was accepted")
	}
	// An unknown layer is refused rather than defaulted.
	unknown := valid
	unknown.Layer = "advisor"
	if err := unknown.Validate(); err == nil {
		t.Fatal("an unknown layer was accepted")
	}
}

// TestSpecRequiresASkillAndSchemas covers the registration completeness section 3
// lists: a key, a skill, both schemas, bounded limits and a resolvable policy.
func TestSpecRequiresASkillAndSchemas(t *testing.T) {
	base := Spec{
		Key: "script.supervision", Layer: LayerSupervision, Skill: "supervision.md",
		Input:       "schemas/agent/supervision-request.v1.json",
		Output:      "schemas/agent/review-report.v1.json",
		Limits:      Limits{MaxToolCalls: 12, MaxDuration: 300 * time.Second},
		PolicyLayer: PolicySupervision,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("the reference registration was refused: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Spec)
	}{
		{"no skill", func(s *Spec) { s.Skill = "  " }},
		{"no input schema", func(s *Spec) { s.Input = "" }},
		{"no output schema", func(s *Spec) { s.Output = "" }},
		{"no tool-call budget", func(s *Spec) { s.Limits.MaxToolCalls = 0 }},
		{"a negative tool-call budget", func(s *Spec) { s.Limits.MaxToolCalls = -1 }},
		{"a tool-call budget over the ceiling", func(s *Spec) { s.Limits.MaxToolCalls = MaxToolCallsCeiling + 1 }},
		{"no duration", func(s *Spec) { s.Limits.MaxDuration = 0 }},
		{"a duration over the ceiling", func(s *Spec) { s.Limits.MaxDuration = MaxDurationCeiling + time.Second }},
		{"an unknown policy layer", func(s *Spec) { s.PolicyLayer = "cheap" }},
		{"no policy layer", func(s *Spec) { s.PolicyLayer = "" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			spec := base
			testCase.mutate(&spec)
			if err := spec.Validate(); err == nil {
				t.Fatal("an incomplete registration was accepted")
			}
		})
	}
	// The ceilings themselves are allowed, so the bound is inclusive and an agent
	// may use the whole documented range.
	atCeiling := base
	atCeiling.Limits = Limits{MaxToolCalls: MaxToolCallsCeiling, MaxDuration: MaxDurationCeiling}
	if err := atCeiling.Validate(); err != nil {
		t.Fatalf("a registration at the ceiling was refused: %v", err)
	}
}

// TestToolSpecValidate covers the registry record: a key, a mode, a scope and a
// bounded output.
func TestToolSpecValidate(t *testing.T) {
	base := ToolSpec{Key: "story.read_events", Mode: ToolRead, Scope: "project", MaxOutputBytes: 64 * 1024}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed tool was refused: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*ToolSpec)
	}{
		{"a malformed key", func(s *ToolSpec) { s.Key = "story" }},
		{"an unknown mode", func(s *ToolSpec) { s.Mode = "modify" }},
		{"no scope", func(s *ToolSpec) { s.Scope = " " }},
		{"no output bound", func(s *ToolSpec) { s.MaxOutputBytes = 0 }},
		{"an output bound over the ceiling", func(s *ToolSpec) { s.MaxOutputBytes = MaxToolOutputBytes + 1 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			spec := base
			testCase.mutate(&spec)
			if err := spec.Validate(); err == nil {
				t.Fatal("a malformed tool was accepted")
			}
		})
	}
}

// TestAgentRunValidate covers section 13.1's record and the two states that could
// make "how long did this take" wrong.
func TestAgentRunValidate(t *testing.T) {
	digest := strings.Repeat("a", 64)
	finished := time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)
	base := AgentRun{
		ID: "run-1", ProjectID: "project-1", Layer: LayerExecution,
		AgentKey: "script.execution.story_skeleton", SkillVersionID: "skill-1",
		Status: RunSucceeded, InputSummary: "generate the skeleton",
		StartedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		FinishedAt: finished, Revision: 1,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed run was refused: %v", err)
	}
	// A running run must NOT carry a finish time, and a finished run must.
	running := base
	running.Status = RunRunning
	running.FinishedAt = time.Time{}
	if err := running.Validate(); err != nil {
		t.Fatalf("a running run was refused: %v", err)
	}
	badFinish := base
	badFinish.Status = RunRunning
	badFinish.FinishedAt = finished
	if err := badFinish.Validate(); err == nil {
		t.Fatal("a running run recorded a finish time")
	}
	missingFinish := base
	missingFinish.FinishedAt = time.Time{}
	if err := missingFinish.Validate(); err == nil {
		t.Fatal("a finished run recorded no finish time")
	}

	cases := []struct {
		name   string
		mutate func(*AgentRun)
	}{
		{"no project", func(r *AgentRun) { r.ProjectID = " " }},
		{"an unknown layer", func(r *AgentRun) { r.Layer = "advisor" }},
		{"a malformed key", func(r *AgentRun) { r.AgentKey = "nope" }},
		{"a key that contradicts the layer", func(r *AgentRun) {
			r.Layer = LayerSupervision
			// The key stays execution, so the two disagree.
		}},
		{"no skill version", func(r *AgentRun) { r.SkillVersionID = "" }},
		{"an unknown status", func(r *AgentRun) { r.Status = "queued" }},
		{"an over-long input summary", func(r *AgentRun) {
			r.InputSummary = strings.Repeat("字", MaxInputSummaryRunes+1)
		}},
		{"a revision below one", func(r *AgentRun) { r.Revision = 0 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			record := base
			testCase.mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatal("a malformed run was accepted")
			}
		})
	}
	// A run with no workflow or stage is legitimate: a first turn creates one.
	standalone := base
	standalone.WorkflowRunID = ""
	standalone.StageRunID = ""
	if err := standalone.Validate(); err != nil {
		t.Fatalf("a run outside a workflow was refused: %v", err)
	}
	_ = digest
}

// TestAgentMessageValidate covers section 13.2 and the one legal empty content.
func TestAgentMessageValidate(t *testing.T) {
	base := AgentMessage{
		ID: "msg-1", AgentRunID: "run-1", ScopeKey: "local|ws|project-1|episode-1|script.decision|",
		Role: MessageUser, Content: "hello",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed message was refused: %v", err)
	}
	// An empty TOOL message is legal: a tool that found nothing returns nothing.
	empty := base
	empty.Role = MessageTool
	empty.Content = ""
	if err := empty.Validate(); err != nil {
		t.Fatalf("an empty tool result was refused: %v", err)
	}
	// An empty prompt layer is not.
	for _, role := range []MessageRole{MessageSystem, MessageDeveloper, MessageUser, MessageAssistant} {
		blank := base
		blank.Role = role
		blank.Content = "   "
		if err := blank.Validate(); err == nil {
			t.Fatalf("an empty %s message was accepted", role)
		}
	}
	cases := []struct {
		name   string
		mutate func(*AgentMessage)
	}{
		{"no run", func(m *AgentMessage) { m.AgentRunID = "" }},
		{"no scope", func(m *AgentMessage) { m.ScopeKey = "  " }},
		{"an unknown role", func(m *AgentMessage) { m.Role = "tool_call" }},
		{"content over the bound", func(m *AgentMessage) {
			m.Content = strings.Repeat("x", DefaultMessageContentBytes+1)
		}},
		{"a malformed hash", func(m *AgentMessage) { m.ContentHash = "not-a-hash" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			record := base
			testCase.mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatal("a malformed message was accepted")
			}
		})
	}
	// A present hash must be a digest, and the empty value stays legal so a message
	// can be stored before its hash is computed.
	hashed := base
	hashed.ContentHash = strings.Repeat("ab", 32)
	if err := hashed.Validate(); err != nil {
		t.Fatalf("a message with a digest was refused: %v", err)
	}
}

// TestAgentToolCallValidate covers section 13.3, including that a denial must say
// why.
func TestAgentToolCallValidate(t *testing.T) {
	base := AgentToolCall{
		ID: "call-1", AgentRunID: "run-1", Sequence: 0, ToolKey: "story.read_events",
		InputJSON: `{"projectId":"project-1"}`, OutputJSON: `{"events":[]}`, Status: ToolCallSucceed,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed tool call was refused: %v", err)
	}
	// Sequence zero is the first call, so it is legal.
	if base.Sequence != 0 {
		t.Fatal("the fixture is not testing sequence zero")
	}
	// A denied call must carry its reason, because section 7.1 wants the refusal
	// attributable to the ACL.
	denied := base
	denied.Status = ToolCallDenied
	if err := denied.Validate(); err == nil {
		t.Fatal("a denied tool call without a reason was accepted")
	}
	denied.ErrorCode = "security.tool_not_allowed"
	if err := denied.Validate(); err != nil {
		t.Fatalf("a denied tool call with a reason was refused: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*AgentToolCall)
	}{
		{"no run", func(c *AgentToolCall) { c.AgentRunID = "  " }},
		{"a negative sequence", func(c *AgentToolCall) { c.Sequence = -1 }},
		{"a malformed tool key", func(c *AgentToolCall) { c.ToolKey = "story" }},
		{"an unknown status", func(c *AgentToolCall) { c.Status = "skipped" }},
		{"an oversized input", func(c *AgentToolCall) {
			c.InputJSON = strings.Repeat("x", MaxToolCallJSONBytes+1)
		}},
		{"an oversized output", func(c *AgentToolCall) {
			c.OutputJSON = strings.Repeat("x", MaxToolCallJSONBytes+1)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			record := base
			testCase.mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatal("a malformed tool call was accepted")
			}
		})
	}
}

// TestSkillVersionValidate covers section 13.4's requirement that a version is
// addressed by hash.
func TestSkillVersionValidate(t *testing.T) {
	base := SkillVersion{
		ID: "sv-1", SkillKey: "script", Version: "1.0.0",
		ContentHash: strings.Repeat("a", 64), ManifestJSON: `{"apiVersion":"atelier.agent/v1"}`,
		ContentFileID: strings.Repeat("b", 64), Status: SkillActive,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed skill version was refused: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*SkillVersion)
	}{
		{"no skill key", func(s *SkillVersion) { s.SkillKey = "" }},
		{"an over-long skill key", func(s *SkillVersion) {
			s.SkillKey = strings.Repeat("k", MaxSkillKeyLength+1)
		}},
		{"no version", func(s *SkillVersion) { s.Version = "  " }},
		{"no content hash", func(s *SkillVersion) { s.ContentHash = "" }},
		{"a malformed content hash", func(s *SkillVersion) { s.ContentHash = "abc" }},
		{"an upper-case hash", func(s *SkillVersion) { s.ContentHash = strings.Repeat("A", 64) }},
		{"no manifest", func(s *SkillVersion) { s.ManifestJSON = "" }},
		{"no content file", func(s *SkillVersion) { s.ContentFileID = "" }},
		{"an unknown status", func(s *SkillVersion) { s.Status = "draft" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			record := base
			testCase.mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatal("a malformed skill version was accepted")
			}
		})
	}
}

// TestVocabulariesRejectCaseVariantsAndUnknowns covers the shared shape every
// closed set in this package has: the documented values are accepted, a case
// variant is not (it is a different string, so SQL would refuse it too), and an
// unknown value is refused.
func TestVocabulariesRejectCaseVariantsAndUnknowns(t *testing.T) {
	check := func(name string, values []string, isValid func(string) bool) {
		t.Helper()
		for _, value := range values {
			if !isValid(value) {
				t.Fatalf("%s rejects its own documented value %q", name, value)
			}
			for _, variant := range []string{strings.ToUpper(value), strings.ToLower(value)} {
				if variant == value {
					continue
				}
				if isValid(variant) {
					t.Fatalf("%s accepts the case variant %q of %q", name, variant, value)
				}
			}
		}
		if isValid("") {
			t.Fatalf("%s accepts the empty value", name)
		}
		if isValid("nonsense") {
			t.Fatalf("%s accepts an unknown value", name)
		}
	}
	check("Layers", stringsOf(Layers), func(v string) bool { return IsValidLayer(AgentLayer(v)) })
	check("ToolModes", stringsOf(ToolModes), func(v string) bool { return IsValidToolMode(ToolMode(v)) })
	check("PolicyLayers", stringsOf(PolicyLayers), func(v string) bool { return IsValidPolicyLayer(PolicyLayer(v)) })
	check("RunStatuses", stringsOf(RunStatuses), func(v string) bool { return IsValidRunStatus(RunStatus(v)) })
	check("MessageRoles", stringsOf(MessageRoles), func(v string) bool { return IsValidMessageRole(MessageRole(v)) })
	check("ToolCallStatuses", stringsOf(ToolCallStatuses), func(v string) bool { return IsValidToolCallStatus(ToolCallStatus(v)) })
	check("SkillStatuses", stringsOf(SkillStatuses), func(v string) bool { return IsValidSkillStatus(SkillStatus(v)) })
}

// TestRunTerminalAgreesWithTheStatusList keeps IsRunTerminal and RunStatuses from
// drifting: the terminal set is defined by what the statuses are for, and a new
// status silently classified as non-terminal would leave a run "running" forever.
func TestRunTerminalAgreesWithTheStatusList(t *testing.T) {
	want := map[RunStatus]bool{RunPending: false, RunRunning: false, RunSucceeded: true, RunFailed: true, RunCancelled: true}
	if len(RunStatuses) != len(want) {
		t.Fatalf("RunStatuses has %d values and the terminal table has %d, so one was added without a ruling on whether it is terminal",
			len(RunStatuses), len(want))
	}
	for _, status := range RunStatuses {
		if got := IsRunTerminal(status); got != want[status] {
			t.Fatalf("IsRunTerminal(%s) = %t, want %t", status, got, want[status])
		}
	}
}

// TestErrorCategories covers the taxonomy the desktop layer maps, including the
// two this package adds over its siblings.
func TestErrorCategories(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorCategory
	}{
		{"invalid", InvalidError("x"), CategoryInvalidInput},
		{"not found", NotFoundError(), CategoryNotFound},
		{"conflict", ConflictError("x"), CategoryConflict},
		{"storage", StorageError("x", nil), CategoryStorage},
		{"security", SecurityError("x"), CategorySecurity},
		{"unavailable", UnavailableError(), CategoryUnavailable},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			domainErr, ok := AsError(testCase.err)
			if !ok {
				t.Fatalf("AsError did not recognise %T", testCase.err)
			}
			if domainErr.Category != testCase.want {
				t.Fatalf("category is %q, want %q", domainErr.Category, testCase.want)
			}
		})
	}
	// A security refusal must not be retriable by category: section 14.2 lists it
	// as never automatically retried.
	if domainErr, ok := AsError(SecurityError("x")); !ok || domainErr.Category != CategorySecurity {
		t.Fatal("a security refusal lost its category")
	}
	// A wrapped domain error is still found, and a nil one is safe to print.
	var nilErr *Error
	if nilErr.Error() != "" || nilErr.Unwrap() != nil {
		t.Fatal("a nil domain error must be safe to print and unwrap")
	}
	wrapped := StorageError("the write failed", InvalidError("cause"))
	if category, ok := AsError(wrapped); !ok || category.Category != CategoryStorage {
		t.Fatal("a wrapped storage error lost its category")
	}
	if wrapped.Unwrap() == nil {
		t.Fatal("a storage error must keep its cause")
	}
}

func stringsOf[T ~string](values []T) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

// TestWireCategoriesCoverEveryDomainCategory is the parity guard this repository has
// needed twice before, per validation.go's own account of the drift it was bitten by.
//
// The domain taxonomy and the contract's enum are different sets on purpose, so the
// guarantee cannot be "they are equal". It is that every domain category maps into
// the contract's vocabulary, and that the vocabulary is exactly section 7.7's list —
// a category added to the domain without a decision about where it belongs on the
// wire would otherwise silently become "internal".
func TestWireCategoriesCoverEveryDomainCategory(t *testing.T) {
	domainCategories := []ErrorCategory{
		CategoryInvalidInput, CategoryNotFound, CategoryConflict,
		CategoryStorage, CategorySecurity, CategoryUnavailable,
		CategoryModel, CategoryTool, CategoryCancelled,
	}
	for _, category := range domainCategories {
		wire := category.Wire()
		if !IsValidWireCategory(wire) {
			t.Fatalf("domain category %q maps to %q, which is not in the contract", category, wire)
		}
	}
	// The contract's list, written out here rather than shared with the implementation
	// so a change to the enum is visible in the diff of a test.
	want := []WireCategory{
		"configuration", "input", "model", "tool", "timeout",
		"cancelled", "security", "storage", "internal",
	}
	got := WireCategories()
	if len(got) != len(want) {
		t.Fatalf("the wire vocabulary has %d entries, section 7.7 lists %d", len(got), len(want))
	}
	for index, category := range want {
		if got[index] != category {
			t.Fatalf("wire category %d is %q, want %q", index, got[index], category)
		}
	}
	// The specific mappings that lose information are asserted rather than left to the
	// loop above, because "not_found becomes input" is a decision a reviewer should
	// have to disagree with explicitly.
	if CategoryNotFound.Wire() != WireInput {
		t.Fatal("a not-found does not map to the input category")
	}
	if CategoryConflict.Wire() != WireInput {
		t.Fatal("a conflict does not map to the input category")
	}
	if CategoryUnavailable.Wire() != WireConfiguration {
		t.Fatal("an unavailable runtime does not map to the configuration category")
	}
}
