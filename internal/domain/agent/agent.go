// Package agent is the vocabulary and the invariants of the three-layer agent
// runtime (AGENT_CONTRACTS sections 2 and 3), the run records of DOMAIN_MODEL
// section 13, and the tool permission matrix of AGENT_CONTRACTS section 6.3.
//
// It performs no I/O, mints no identifier (ADR-0005) and knows nothing about
// providers, skills or the workflow engine: those are the application layer's.
// What lives here is what must be true of an agent run before and after it runs,
// which is the part the specification states as a rule rather than as a
// procedure.
//
// Three of those rules are the ones AC-AGENT-001..005 are written about, and all
// three are enforced here rather than in the runtime's control flow:
//
//   - A LAYER DETERMINES WHAT IT MAY DO. A Decision agent may not hold a business
//     write tool, a Supervisor may not hold a write tool at all by default, and an
//     Execution agent may hold one only for its own stage. ToolAllowed is that
//     rule; the runtime calls it on every call, because SECURITY section 7.1
//     requires the ACL to be re-checked per invocation and not to depend on the
//     registration having been checked once.
//   - A RUN IS BOUND TO AN EXACT SKILL VERSION. Section 4.2: "Agent Run 保存准确
//     Skill Version". A run without one could not be reproduced, so Validate
//     refuses it.
//   - A SUPERVISOR'S VERDICT MUST BE CONSISTENT WITH ITS OWN FINDINGS. A report
//     that passes while carrying a major or critical issue is refused, which is
//     section 7.6's Validation list reduced to the part JSON Schema cannot say.
package agent

import (
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// AgentLayer is which of the three layers an agent belongs to (AGENT_CONTRACTS
// section 2).
//
// The three are separate LLM calls with separate skills, separate tool sets and
// separate outputs. Section 2.3 forbids the supervisor from reviewing the
// executor's summary, which is why the layer is part of an agent's identity and
// not a parameter: the same key cannot be used at two layers.
type AgentLayer string

const (
	LayerDecision    AgentLayer = "decision"
	LayerExecution   AgentLayer = "execution"
	LayerSupervision AgentLayer = "supervision"
)

// Layers lists the documented layers in the specification's order.
var Layers = []AgentLayer{LayerDecision, LayerExecution, LayerSupervision}

// IsValidLayer reports whether a layer may be registered.
func IsValidLayer(value AgentLayer) bool {
	for _, candidate := range Layers {
		if candidate == value {
			return true
		}
	}
	return false
}

// MaxAgentKeyLength bounds an agent key. It matches the schema's column bound on
// stage_runs.execution_agent_key and agent_runs.agent_key.
const MaxAgentKeyLength = 120

// ValidateAgentKey checks an agent key's shape.
//
// A key is a dotted path in the form `<pack>.<layer>[.<name>]`, such as
// `script.decision` or `script.execution.story_skeleton` (section 19's inventory).
// The shape is checked because the key is stored, logged and compared, and a key
// with a space or an empty segment makes every one of those ambiguous.
func ValidateAgentKey(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return InvalidError("An agent needs a key.")
	}
	if len([]rune(trimmed)) > MaxAgentKeyLength {
		return InvalidError("The agent key is too long.")
	}
	segments := strings.Split(trimmed, ".")
	if len(segments) < 2 {
		return InvalidError("An agent key names its pack and its layer, such as script.decision.")
	}
	for _, segment := range segments {
		if segment == "" {
			return InvalidError("An agent key cannot contain an empty segment.")
		}
		for _, character := range segment {
			if character == '_' || character == '-' || character == ':' {
				continue
			}
			if character >= 'a' && character <= 'z' {
				continue
			}
			if character >= '0' && character <= '9' {
				continue
			}
			return InvalidError("An agent key uses lower-case letters, digits, underscore, hyphen and colon.")
		}
	}
	return nil
}

// ToolMode is what a tool does, which is what the permission matrix is stated in
// terms of (AGENT_CONTRACTS section 6.3).
type ToolMode string

const (
	// ToolRead reads through an application service.
	ToolRead ToolMode = "read"
	// ToolWrite creates or updates through an application service command. It is
	// the mode the matrix restricts: Decision never holds one, Supervision holds
	// none by default.
	ToolWrite ToolMode = "write"
	// ToolExternal reaches outside the process, which today means queueing a
	// generation job rather than calling a provider directly.
	ToolExternal ToolMode = "external"
	// ToolControl changes the workflow: invoking another agent, requesting a user
	// gate. It is the one mode Decision holds by design.
	ToolControl ToolMode = "control"
)

// ToolModes lists the documented modes in the matrix's column order.
var ToolModes = []ToolMode{ToolRead, ToolWrite, ToolExternal, ToolControl}

// IsValidToolMode reports whether a mode may be registered.
func IsValidToolMode(value ToolMode) bool {
	for _, candidate := range ToolModes {
		if candidate == value {
			return true
		}
	}
	return false
}

// MaxToolKeyLength bounds a tool key.
const MaxToolKeyLength = 120

// ValidateToolKey checks a tool key's shape: `<domain>.<verb>_<object>`
// (AGENT_CONTRACTS section 6.2).
//
// The shape is enforced rather than merely documented because the key is what the
// permission matrix and the audit trail are read by, and a key that does not say
// its domain cannot be checked against a layer's allowance.
func ValidateToolKey(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return InvalidError("A tool needs a key.")
	}
	if len([]rune(trimmed)) > MaxToolKeyLength {
		return InvalidError("The tool key is too long.")
	}
	segments := strings.Split(trimmed, ".")
	if len(segments) != 2 {
		return InvalidError("A tool key is a domain and an action, such as story.read_events.")
	}
	domain, action := segments[0], segments[1]
	// The domain is a single lower-case word, because the matrix is per domain
	// family and a dotted domain would make the family ambiguous.
	for _, character := range domain {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return InvalidError("A tool key's domain is lower-case letters and digits.")
		}
	}
	// The action is `verb_object`, so it must contain the underscore that separates
	// the verb from what it acts on.
	if !strings.Contains(action, "_") {
		return InvalidError("A tool key's action is a verb and an object, such as read_events.")
	}
	for _, character := range action {
		if character == '_' {
			continue
		}
		if character >= 'a' && character <= 'z' {
			continue
		}
		if character >= '0' && character <= '9' {
			continue
		}
		return InvalidError("A tool key's action uses lower-case letters, digits and underscore.")
	}
	return nil
}

// ToolAllowed reports whether a layer may hold a tool of a given mode.
//
// It is AGENT_CONTRACTS section 6.3's matrix stated as a function, and it is the
// rule AC-AGENT-001's first three bullets test. The matrix's "少量高层读取" for
// Decision and "阶段相关" for Execution are refinements the runtime applies on top
// by listing the tools an agent may hold; what is here is the part that no agent
// registration may override.
//
//	Layer        Read   Write   External  Control
//	Decision     yes    NO      NO        yes
//	Execution    yes    yes     yes       NO
//	Supervision  yes    NO      NO        NO
func ToolAllowed(layer AgentLayer, mode ToolMode) bool {
	switch layer {
	case LayerDecision:
		// No business write and no external access: a decision selects among
		// actions, it does not perform them. Control is its purpose.
		return mode == ToolRead || mode == ToolControl
	case LayerExecution:
		// The stage's own work, including the candidate writes the stage owns. It
		// may not invoke other agents or request a gate: section 2.2 forbids an
		// execution from choosing the workflow's next stage.
		return mode == ToolRead || mode == ToolWrite || mode == ToolExternal
	case LayerSupervision:
		// Read-only by default. A supervisor that could write could fix what it is
		// reviewing, which section 2.3 forbids ("自行修复业务数据").
		return mode == ToolRead
	}
	return false
}

// ToolSpec is one registered tool's identity and the mode the matrix is checked
// against.
type ToolSpec struct {
	Key  string
	Mode ToolMode
	// Scope is the entity type this tool is scoped by, such as "project" or
	// "episode". It is the domain the schema will validate against, named here so
	// the registry can refuse a tool whose scope it cannot check.
	Scope string
	// MaxOutputBytes bounds one result, so a tool cannot return a whole novel into
	// a prompt (AGENT_CONTRACTS section 6.4's "返回结构化、限长数据").
	MaxOutputBytes int
}

// MaxToolOutputBytes is the ceiling a tool result may carry.
//
// It is deliberately larger than the domain event payload cap (16 KiB) because a
// tool result is structured data a model reads rather than an audit record, and
// smaller than the provider response cap (1 MiB) because a result that size is a
// document, which section 6.4 says travels as a FileRef plus a summary instead.
const MaxToolOutputBytes = 256 * 1024

// Validate checks a tool specification before it is registered.
func (s ToolSpec) Validate() error {
	if err := ValidateToolKey(s.Key); err != nil {
		return err
	}
	if !IsValidToolMode(s.Mode) {
		return InvalidError("The tool mode is not recognised.")
	}
	if strings.TrimSpace(s.Scope) == "" {
		return InvalidError("A tool must name the scope it is checked against.")
	}
	if s.MaxOutputBytes <= 0 || s.MaxOutputBytes > MaxToolOutputBytes {
		return InvalidError("A tool must bound its output within the registry's ceiling.")
	}
	return nil
}

// Limits bounds one agent invocation. Section 3 requires both to be bounded at
// registration, because an unbounded agent is one that can run until the user
// notices.
type Limits struct {
	MaxToolCalls int
	MaxDuration  time.Duration
}

// Registry bounds. The maxima are the specification's own figures where it gives
// them and a documented choice where it does not.
const (
	// MaxToolCallsCeiling is the largest tool-call budget a registration may ask
	// for. Section 4.2's example manifests use 6, 8 and 12.
	MaxToolCallsCeiling = 50
	// MaxDurationCeiling is the longest one invocation may run. Section 4.2's
	// example manifests use 180 and 300 seconds.
	MaxDurationCeiling = 30 * time.Minute
)

// Validate checks the bounds before registration.
func (l Limits) Validate() error {
	if l.MaxToolCalls <= 0 || l.MaxToolCalls > MaxToolCallsCeiling {
		return InvalidError("An agent must bound its tool calls within the registry's ceiling.")
	}
	if l.MaxDuration <= 0 || l.MaxDuration > MaxDurationCeiling {
		return InvalidError("An agent must bound its duration within the registry's ceiling.")
	}
	return nil
}

// Spec is one registered agent (AGENT_CONTRACTS section 3's AgentSpec).
//
// It is the registration record: what the agent may do, which skill it runs, and
// which schemas its input and output are validated against. The registry compiles
// every one of these at startup and refuses the whole registry if any is invalid,
// which is what makes an unreachable or inconsistent agent a startup failure
// rather than a runtime surprise.
type Spec struct {
	Key    string
	Layer  AgentLayer
	Skill  string
	Input  string
	Output string
	// AllowedTools lists tool keys this agent may call. The layer's allowance is
	// checked on top of this list, never instead of it: a registration cannot grant
	// a supervisor a write tool by listing one.
	AllowedTools []string
	Limits       Limits
	// PolicyLayer names the project model policy this agent reads, so a project can
	// run its supervisor on a different provider from its executor (section 13).
	PolicyLayer PolicyLayer
}

// Validate checks a registration.
func (s Spec) Validate() error {
	if err := ValidateAgentKey(s.Key); err != nil {
		return err
	}
	if !IsValidLayer(s.Layer) {
		return InvalidError("The agent layer is not recognised.")
	}
	// The key's layer segment must agree with the layer field, or the key would be
	// a second, contradictory statement of the same fact.
	if !keyNamesLayer(s.Key, s.Layer) {
		return InvalidError("The agent key's second segment must name its layer.")
	}
	if strings.TrimSpace(s.Skill) == "" {
		return InvalidError("An agent must name its skill.")
	}
	if strings.TrimSpace(s.Input) == "" || strings.TrimSpace(s.Output) == "" {
		return InvalidError("An agent must name the schemas its input and output are checked against.")
	}
	if err := s.Limits.Validate(); err != nil {
		return err
	}
	if !IsValidPolicyLayer(s.PolicyLayer) {
		return InvalidError("The agent must name a model policy layer.")
	}
	return nil
}

// keyNamesLayer reports whether a key's second segment is its layer.
func keyNamesLayer(key string, layer AgentLayer) bool {
	segments := strings.Split(key, ".")
	if len(segments) < 2 {
		return false
	}
	return segments[1] == string(layer)
}

// PolicyLayer is which project provider policy an agent reads (AGENT_CONTRACTS
// section 13).
//
// It mirrors project.ProviderPolicyLayer rather than importing it, because this
// package is the runtime's vocabulary and the project aggregate is the settings':
// the application layer resolves one to the other. It is a closed set of the four
// layers section 13 names.
type PolicyLayer string

const (
	PolicyDecision    PolicyLayer = "decision"
	PolicyExecution   PolicyLayer = "execution"
	PolicySupervision PolicyLayer = "supervision"
	PolicyDefault     PolicyLayer = "default"
)

// PolicyLayers lists the layers in the order the project schema declares them.
var PolicyLayers = []PolicyLayer{PolicyDefault, PolicyDecision, PolicyExecution, PolicySupervision}

// IsValidPolicyLayer reports whether a policy layer may be named.
func IsValidPolicyLayer(value PolicyLayer) bool {
	for _, candidate := range PolicyLayers {
		if candidate == value {
			return true
		}
	}
	return false
}

// RunStatus is the lifecycle of one agent invocation.
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// RunStatuses lists the statuses in the schema's order.
var RunStatuses = []RunStatus{RunPending, RunRunning, RunSucceeded, RunFailed, RunCancelled}

// IsValidRunStatus reports whether a run status may be persisted.
func IsValidRunStatus(value RunStatus) bool {
	for _, candidate := range RunStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// IsRunTerminal reports whether a run has finished.
func IsRunTerminal(status RunStatus) bool {
	return status == RunSucceeded || status == RunFailed || status == RunCancelled
}

// MessageRole is who said one message of a run.
type MessageRole string

const (
	MessageSystem    MessageRole = "system"
	MessageDeveloper MessageRole = "developer"
	MessageUser      MessageRole = "user"
	MessageAssistant MessageRole = "assistant"
	MessageTool      MessageRole = "tool"
)

// MessageRoles lists the roles in the order section 5 assembles them.
//
// The first four are the prompt layers and the fifth is a tool result. Section 5
// names the layers rather than their roles, so this list is the mapping: policy
// and skill are system, tool schemas and limits are developer, workflow state and
// facts are user-supplied context, and the model's own turns are assistant.
var MessageRoles = []MessageRole{MessageSystem, MessageDeveloper, MessageUser, MessageAssistant, MessageTool}

// IsValidMessageRole reports whether a role may be persisted.
func IsValidMessageRole(value MessageRole) bool {
	for _, candidate := range MessageRoles {
		if candidate == value {
			return true
		}
	}
	return false
}

// ToolCallStatus is how one tool call ended.
type ToolCallStatus string

const (
	ToolCallPending ToolCallStatus = "pending"
	ToolCallRunning ToolCallStatus = "running"
	ToolCallSucceed ToolCallStatus = "succeeded"
	ToolCallFailed  ToolCallStatus = "failed"
	ToolCallDenied  ToolCallStatus = "denied"
)

// ToolCallStatuses lists the statuses in the schema's order.
//
// ToolCallDenied is separate from ToolCallFailed because section 7.1 and
// AC-AGENT-001 make an authorisation refusal a distinct outcome: it is not
// retriable (section 14.2 lists "Tool 权限拒绝"), and a run whose tool call was
// denied must record that rather than a generic failure.
var ToolCallStatuses = []ToolCallStatus{ToolCallPending, ToolCallRunning, ToolCallSucceed, ToolCallFailed, ToolCallDenied}

// IsValidToolCallStatus reports whether a tool call status may be persisted.
func IsValidToolCallStatus(value ToolCallStatus) bool {
	for _, candidate := range ToolCallStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// SkillStatus is the review state of a skill version.
type SkillStatus string

const (
	SkillActive     SkillStatus = "active"
	SkillSuperseded SkillStatus = "superseded"
)

// SkillStatuses lists the statuses in the schema's order.
var SkillStatuses = []SkillStatus{SkillActive, SkillSuperseded}

// IsValidSkillStatus reports whether a skill status may be persisted.
func IsValidSkillStatus(value SkillStatus) bool {
	for _, candidate := range SkillStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// AgentRun is one invocation of one agent (DOMAIN_MODEL section 13.1).
//
// ProjectID is required because every drama query is project-scoped and because a
// domain event cannot be recorded without one. WorkflowRunID and StageRunID are
// optional because a Decision agent may run for a project with no workflow yet,
// which is the case a first turn creates.
type AgentRun struct {
	ID            string
	ProjectID     string
	WorkflowRunID string
	StageRunID    string
	Layer         AgentLayer
	AgentKey      string
	ModelConfigID string
	// SkillVersionID is required: section 4.2 requires a run to name the exact
	// skill version it ran, and a run without one could not be reproduced.
	SkillVersionID string
	Status         RunStatus
	// InputSummary is a bounded description of what the run was asked to do. The
	// full input is not stored here: it may contain document text, which section
	// 5.2 marks untrusted and which does not belong in a queryable row.
	InputSummary string
	// ValidatedOutputJSON is the output AFTER schema validation. RawOutputFileID
	// points at the unvalidated text, which is kept only for diagnosis.
	ValidatedOutputJSON string
	RawOutputFileID     string
	ErrorCode           string
	StartedAt           time.Time
	FinishedAt          time.Time
	Revision            int64
}

// MaxInputSummaryRunes bounds the stored summary of a run's input.
const MaxInputSummaryRunes = 2000

// Validate checks a run before it is stored.
func (r AgentRun) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return InvalidError("An agent run must belong to a project.")
	}
	if !IsValidLayer(r.Layer) {
		return InvalidError("The agent layer is not recognised.")
	}
	if err := ValidateAgentKey(r.AgentKey); err != nil {
		return err
	}
	if !keyNamesLayer(r.AgentKey, r.Layer) {
		return InvalidError("The run's layer does not match its agent key.")
	}
	if strings.TrimSpace(r.SkillVersionID) == "" {
		return InvalidError("An agent run must name the skill version it ran.")
	}
	if !IsValidRunStatus(r.Status) {
		return InvalidError("The run status is not recognised.")
	}
	if len([]rune(r.InputSummary)) > MaxInputSummaryRunes {
		return InvalidError("The run's input summary is too long.")
	}
	if r.Revision < 1 {
		return InvalidError("A stored run has a revision of at least one.")
	}
	// A finished run names when it finished, and an unfinished one does not. Storing
	// a finish time on a running row would make "how long did this take" wrong.
	if IsRunTerminal(r.Status) && r.FinishedAt.IsZero() {
		return InvalidError("A finished run must record when it finished.")
	}
	if !IsRunTerminal(r.Status) && !r.FinishedAt.IsZero() {
		return InvalidError("An unfinished run cannot record a finish time.")
	}
	return nil
}

// ProjectID is what every query is scoped by, so a run without one is unreachable
// rather than merely malformed.
func (r AgentRun) BelongsToProject(projectID string) bool {
	return strings.TrimSpace(projectID) != "" && r.ProjectID == projectID
}

// AgentMessage is one message of one run (DOMAIN_MODEL section 13.2).
//
// ContentHash is stored so a message can be compared and de-duplicated without
// reading its content, and ScopeKey so recall can select by scope without
// scanning. Section 13.2 says large content may live in the FileStore instead,
// which is why Content is bounded here rather than unbounded.
type AgentMessage struct {
	ID         string
	AgentRunID string
	// ScopeKey is the structured scope of DOMAIN_MODEL section 14.4, encoded as a
	// stable string. Retrieval filters by its STRUCTURE rather than by prefix
	// matching, which is what section 14.4 warns against.
	ScopeKey string
	Role     MessageRole
	Content  string
	// ContentHash is the SHA-256 of Content, so a message can be identified without
	// being read.
	ContentHash string
	CreatedAt   time.Time
}

// DefaultMessageContentBytes bounds one stored message.
//
// It matches the domain event payload ceiling's order of magnitude rather than the
// provider's, because a message is a prompt layer or a model turn and a turn that
// large is a document being pasted into a prompt — which section 5.3 says to
// fetch through a tool instead.
const DefaultMessageContentBytes = 16 * 1024

// Validate checks a message before it is stored.
func (m AgentMessage) Validate() error {
	if strings.TrimSpace(m.AgentRunID) == "" {
		return InvalidError("A message must belong to an agent run.")
	}
	if strings.TrimSpace(m.ScopeKey) == "" {
		return InvalidError("A message must carry its scope, or recall could not isolate it.")
	}
	if !IsValidMessageRole(m.Role) {
		return InvalidError("The message role is not recognised.")
	}
	if strings.TrimSpace(m.Content) == "" && m.Role != MessageTool {
		// An empty tool result is legal: a tool that found nothing returns nothing,
		// and refusing that would make an empty answer look like a failure.
		return InvalidError("A message needs content.")
	}
	if len(m.Content) > DefaultMessageContentBytes {
		return InvalidError("The message is too long to store.")
	}
	if m.ContentHash != "" && !isLowerHexDigest(m.ContentHash) {
		return InvalidError("The content hash must be a SHA-256 digest or empty.")
	}
	return nil
}

// AgentToolCall is one tool invocation (DOMAIN_MODEL section 13.3).
//
// Sequence orders the calls within a run, because a model may call several tools
// and the order it asked for them in is part of what happened.
type AgentToolCall struct {
	ID         string
	AgentRunID string
	Sequence   int
	ToolKey    string
	InputJSON  string
	// OutputJSON is bounded and redacted before it is stored: section 6.4 forbids
	// returning a secret, a raw SQL string or an absolute path from a tool.
	OutputJSON string
	Status     ToolCallStatus
	ErrorCode  string
	StartedAt  time.Time
	FinishedAt time.Time
}

// MaxToolCallJSONBytes bounds one stored tool input or output.
const MaxToolCallJSONBytes = MaxToolOutputBytes

// Validate checks a tool call before it is stored.
func (c AgentToolCall) Validate() error {
	if strings.TrimSpace(c.AgentRunID) == "" {
		return InvalidError("A tool call must belong to an agent run.")
	}
	if c.Sequence < 0 {
		return InvalidError("A tool call's sequence cannot be negative.")
	}
	if err := ValidateToolKey(c.ToolKey); err != nil {
		return err
	}
	if len(c.InputJSON) > MaxToolCallJSONBytes {
		return InvalidError("The tool call's input is too large to store.")
	}
	if len(c.OutputJSON) > MaxToolCallJSONBytes {
		return InvalidError("The tool call's output is too large to store.")
	}
	if !IsValidToolCallStatus(c.Status) {
		return InvalidError("The tool call status is not recognised.")
	}
	// A denied call is recorded with the code that says so, because section 7.1
	// wants the refusal attributable to the ACL rather than to the tool.
	if c.Status == ToolCallDenied && strings.TrimSpace(c.ErrorCode) == "" {
		return InvalidError("A denied tool call must record why it was denied.")
	}
	return nil
}

// SkillVersion is one immutable revision of one skill (DOMAIN_MODEL section 13.4).
//
// ContentHash is what makes a run reproducible: the manifest and the documents are
// addressed by hash, so a skill edited after a run does not change what that run
// recorded. The status moves only forward, from active to superseded.
type SkillVersion struct {
	ID           string
	SkillKey     string
	Version      string
	ContentHash  string
	ManifestJSON string
	// ContentFileID is the hash of the stored skill documents, which is a reference
	// rather than the text: skill text is prompt material and does not belong in a
	// queryable column.
	ContentFileID string
	Status        SkillStatus
	CreatedAt     time.Time
}

// MaxSkillKeyLength bounds a skill key.
const MaxSkillKeyLength = 120

// Validate checks a skill version before it is stored.
func (s SkillVersion) Validate() error {
	if strings.TrimSpace(s.SkillKey) == "" {
		return InvalidError("A skill version must name its skill.")
	}
	if len([]rune(s.SkillKey)) > MaxSkillKeyLength {
		return InvalidError("The skill key is too long.")
	}
	if strings.TrimSpace(s.Version) == "" {
		return InvalidError("A skill version needs a version.")
	}
	if !isLowerHexDigest(s.ContentHash) {
		return InvalidError("A skill version needs the SHA-256 hash of its content.")
	}
	if strings.TrimSpace(s.ManifestJSON) == "" {
		return InvalidError("A skill version must store the manifest it was loaded from.")
	}
	if strings.TrimSpace(s.ContentFileID) == "" {
		return InvalidError("A skill version must name the file holding its documents.")
	}
	if !IsValidSkillStatus(s.Status) {
		return InvalidError("The skill status is not recognised.")
	}
	return nil
}

// CreatedByType is re-exported so a caller recording who triggered a run does not
// need to import the versioning package for the vocabulary alone.
type CreatedByType = versioning.CreatedByType

// isLowerHexDigest reports whether a value is a SHA-256 digest in lower-case hex.
//
// It is duplicated from the story domain's own helper rather than shared, because
// the alternative is a utility package that both import for one predicate, and
// AGENTS section 7.4 forbids a general-purpose utils package.
func isLowerHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') {
			continue
		}
		return false
	}
	return true
}
