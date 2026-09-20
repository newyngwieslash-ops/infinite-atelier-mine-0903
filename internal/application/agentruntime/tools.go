// Package agentruntime is the three-layer agent runtime of AGENT_CONTRACTS
// section 3: the registry, the tool registry and authorizer, the prompt
// assembler, the three runners and the workflow engine that decides when a stage
// advances.
//
// It is one package rather than five because the pieces are not usable
// separately: a runner without the authorizer would call tools unchecked, and an
// engine without the runners would have nothing to advance. What keeps it
// readable is that each file owns one question —
//
//	registry.go     which agents exist, and what may they do
//	tools.go        which tools exist, and may THIS agent call THIS one now
//	prompt.go       what order the model sees things in
//	runner.go       one invocation, end to end
//	engine.go       when a stage advances, and what a user decision does
//	reject.go       the refusals, classified the way section 14 needs them
//
// Three properties hold across all of them, and each is a specification rule
// rather than a design preference:
//
//   - THE RUNTIME DECIDES, NOT THE MODEL. A model's choice of stage, agent or
//     tool is a REQUEST. Every one is checked against the database and the
//     registry before it takes effect, which is what section 9's "Runtime 必须再次
//     校验，不因模型输出而改变规则" means in code.
//   - NOTHING IS WRITTEN UNTIL THE OUTPUT VALIDATES. Section 14.3's repair round
//     runs before any business write, so a malformed reading leaves no rows.
//   - A RUN RECORDS WHAT HAPPENED. Section 16's list, in the agent_runs,
//     agent_messages and agent_tool_calls tables.
package agentruntime

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// ToolHandler runs one tool.
//
// It receives already-validated arguments and returns the tool's structured
// result. It must not write anything except through an application service that
// owns the write, and it must honour the context: section 15 requires a cancel to
// stop the work, and a handler that ignores the context would keep a cancelled
// run alive.
type ToolHandler func(ctx context.Context, request ToolRequest) (any, error)

// ToolRequest is what a handler is given.
//
// Scopes are passed explicitly rather than read from the arguments, because they
// come from the run the tool was called for and not from the model: a model that
// could name its own project could read another project's story, which section
// 7.1 forbids.
type ToolRequest struct {
	// ProjectID is the run's project. Every handler must scope its work by it.
	ProjectID string
	// EpisodeID is the run's episode, or empty when the run is project-wide.
	EpisodeID string
	// WorkflowRunID and StageRunID are the run's context, or empty outside a
	// workflow.
	WorkflowRunID string
	StageRunID    string
	// Arguments is the model's JSON arguments, already schema-validated and size
	// bounded.
	Arguments json.RawMessage
}

// Tool is one registered capability.
type Tool struct {
	Spec    agent.ToolSpec
	Handler ToolHandler
	// SchemaPath names the input schema. Section 6.4 and 6.1 both give a tool an
	// input schema, and a tool without one would take whatever the model sent.
	SchemaPath string
}

// Tools is the built-in tool table.
//
// It is the whole set this build can call: section 4.2's "Tool Key 必须来自内置注册表"
// means a manifest, a skill or a model can only ever name something in here. The
// table is built once at composition and is read-only afterwards, so a run cannot
// add to it.
type Tools struct {
	byKey map[string]Tool
	keys  []string
}

// NewTools builds a tool table, validating every entry.
//
// It refuses the whole table on any invalid entry rather than dropping the bad
// one: a tool that failed validation is a build defect, and a table that silently
// lacked it would produce an agent whose skill names a tool that is not there.
func NewTools(tools []Tool) (*Tools, error) {
	table := &Tools{byKey: make(map[string]Tool, len(tools))}
	for _, tool := range tools {
		if err := tool.Spec.Validate(); err != nil {
			return nil, err
		}
		if tool.Handler == nil {
			return nil, agent.InvalidError("The tool " + tool.Spec.Key + " has no implementation.")
		}
		if strings.TrimSpace(tool.SchemaPath) == "" {
			return nil, agent.InvalidError("The tool " + tool.Spec.Key + " names no input schema.")
		}
		if _, exists := table.byKey[tool.Spec.Key]; exists {
			return nil, agent.InvalidError("The tool " + tool.Spec.Key + " is registered twice.")
		}
		table.byKey[tool.Spec.Key] = tool
	}
	table.keys = make([]string, 0, len(table.byKey))
	for key := range table.byKey {
		table.keys = append(table.keys, key)
	}
	sort.Strings(table.keys)
	return table, nil
}

// Lookup returns a tool by key.
func (t *Tools) Lookup(key string) (Tool, bool) {
	if t == nil {
		return Tool{}, false
	}
	tool, ok := t.byKey[key]
	return tool, ok
}

// Keys returns every registered key, sorted.
func (t *Tools) Keys() []string {
	if t == nil {
		return nil
	}
	return append([]string(nil), t.keys...)
}

// KeySet returns the keys as a set, which is what the skill loader's manifest
// validation wants.
func (t *Tools) KeySet() map[string]bool {
	if t == nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(t.keys))
	for _, key := range t.keys {
		out[key] = true
	}
	return out
}

// Authorizer decides whether one agent may call one tool right now.
//
// It is the ACL of AGENT_CONTRACTS section 6.3 and SECURITY section 7.1, and it is
// a separate type from the registry because the two questions are different: the
// registry says what exists, and the authorizer says what this run may do. That
// split is what makes "Runtime 每次调用再次校验" possible — the registry is checked
// once at startup, the authorizer on every call.
type Authorizer struct{}

// AuthorizeRequest is one call to check.
type AuthorizeRequest struct {
	Spec agent.Spec
	Tool agent.ToolSpec
}

// Authorize decides, and its refusal is always a security error rather than an
// invalid-input one.
//
// The order of the checks matters for the message a caller sees, and the
// specification's order is: the layer's matrix first, because a supervisor asking
// for a write tool is a different mistake from an agent asking for a tool nobody
// registered.
func (a Authorizer) Authorize(request AuthorizeRequest) error {
	if !agent.IsValidLayer(request.Spec.Layer) {
		return agent.SecurityError("The agent's layer is not recognised, so no tool is permitted.")
	}
	if err := agent.ValidateToolKey(request.Tool.Key); err != nil {
		return agent.SecurityError("That tool is not registered.")
	}
	// The matrix. A layer may not hold a mode at all, whatever its spec lists.
	if !agent.ToolAllowed(request.Spec.Layer, request.Tool.Mode) {
		return &ToolNotAllowedError{
			Layer: request.Spec.Layer,
			Mode:  request.Tool.Mode,
			Tool:  request.Tool.Key,
		}
	}
	// The spec. Listing a tool is how an agent gets it, and the list is the
	// narrower of the two: the matrix is a ceiling, not a grant.
	if !containsString(request.Spec.AllowedTools, request.Tool.Key) {
		return &ToolNotAllowedError{
			Layer: request.Spec.Layer,
			Mode:  request.Tool.Mode,
			Tool:  request.Tool.Key,
			// The distinction is worth keeping in the record: a tool the layer may
			// never hold is a configuration defect, while one the agent simply does
			// not list is a model asking for something outside its remit.
			NotGranted: true,
		}
	}
	return nil
}

// ToolNotAllowedError is the refusal AC-AGENT-001 requires, carrying the stable
// code the acceptance criterion names.
//
// The message names the layer and the tool but not what the tool would have done:
// a caller must not be able to learn a tool's behaviour by being refused it.
type ToolNotAllowedError struct {
	Layer      agent.AgentLayer
	Mode       agent.ToolMode
	Tool       string
	NotGranted bool
}

// Code is the stable identifier SECURITY section 17 and AC-AGENT-001 name.
const CodeToolNotAllowed = "security.tool_not_allowed"

func (e *ToolNotAllowedError) Error() string {
	if e == nil {
		return ""
	}
	if e.NotGranted {
		return "This agent is not granted that tool."
	}
	return "The " + string(e.Layer) + " layer may not call that kind of tool."
}

// Code is the stable identifier AC-AGENT-001 requires an illegal call to carry.
//
// It is the same code whether the matrix or the grant refused the call, because
// from the caller's side it is one outcome: the tool is not permitted. Which rule
// fired is recorded on the error as NotGranted so the run's record can tell a
// configuration defect from a model reaching outside its remit, but the value the
// frontend and the acceptance criterion are keyed on is one string.
func (e *ToolNotAllowedError) Code() string { return CodeToolNotAllowed }

// Category reports the agent error category, which is what section 14.2's
// non-retriable list is keyed on.
func (e *ToolNotAllowedError) Category() agent.ErrorCategory {
	return agent.CategorySecurity
}

// Retriable reports that this refusal is never automatically retried. It exists so
// a caller does not have to remember section 14.2's list by heart.
func (e *ToolNotAllowedError) Retriable() bool { return false }

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// MaxToolResultBytes is re-exported from the domain so a caller bounding a
// handler's result uses the registry's own ceiling.
const MaxToolResultBytes = agent.MaxToolOutputBytes

// boundResult renders a handler's result as JSON within the tool's ceiling.
//
// A result that does not fit is truncated by REFUSAL rather than by truncation:
// half a JSON document is not a smaller answer, it is invalid JSON, and a model
// given invalid JSON would either fail its own parse or, worse, act on the part it
// could read. Section 6.4's "返回结构化、限长数据" is satisfied by a tool whose
// handlers page rather than by one that cuts.
func boundResult(value any, limit int) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		// A handler whose result will not marshal is a handler defect, so it is
		// reported as a storage failure rather than as a model or input problem.
		return "", agent.StorageError("The tool's result could not be encoded.", err)
	}
	if limit <= 0 {
		limit = MaxToolResultBytes
	}
	if len(encoded) > limit {
		return "", agent.InvalidError("The tool returned more data than one result may carry.")
	}
	return string(encoded), nil
}

// ToolCallOutcome is what one executed call produced, for the run record.
type ToolCallOutcome struct {
	ToolKey   string
	Status    agent.ToolCallStatus
	Output    string
	ErrorCode string
	Started   time.Time
	Finished  time.Time
}
