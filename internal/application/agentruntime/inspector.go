package agentruntime

import (
	"context"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// inspector.go is the read surface the Agent Center uses.
//
// It exists so the desktop layer does not reach a repository directly: AGENTS section 7.2
// puts an application service between a binding and persistence, and section 7.4's "服务端
// 事实通过 Query/Binding" is the same rule from the frontend's side. Every method here is a
// READ — nothing on this surface can change a run, a message or a tool call, which is what
// makes it safe to expose without an authorization question of its own. The project scope is
// still checked, because a run's trace is project data.
type Inspector struct {
	reader RunReader
}

// RunReader is the read side of the run record: the queries an inspector asks.
//
// It is separate from RunStore because the two are used by different callers — the runtime
// writes through RunStore, the Agent Center reads through this — and a single interface
// covering both would let a reader accidentally write.
type RunReader interface {
	// ListRuns returns a project's runs newest first.
	ListRuns(ctx context.Context, projectID string, limit int) ([]agent.AgentRun, error)
	// GetRun returns one run.
	GetRun(ctx context.Context, id string) (agent.AgentRun, error)
	// ListMessages returns one run's messages oldest first.
	ListMessages(ctx context.Context, runID string) ([]agent.AgentMessage, error)
	// ListToolCalls returns one run's tool calls in sequence order.
	ListToolCalls(ctx context.Context, runID string) ([]agent.AgentToolCall, error)
}

// NewInspector builds the inspector.
//
// Both dependencies are required, and a missing one makes every method refuse rather than
// return an empty list: an inspector that reported "no runs" for a database it could not read
// would tell a user their project is empty when it is not.
func NewInspector(reader RunReader) *Inspector {
	return &Inspector{reader: reader}
}

// Available reports whether the inspector can read.
func (i *Inspector) Available() bool {
	return i != nil && i.reader != nil
}

// MaxRunLimit bounds one list query.
const MaxRunLimit = 200

// DefaultRunLimit is how many runs a list returns when the caller states none.
const DefaultRunLimit = 50

// RunSummary is what a list view needs about one run.
//
// It is deliberately NOT the whole AgentRun: the validated output can be large and the raw
// output reference is a diagnostic detail. A list that carried them would send a project's
// entire agent history to the frontend to render a table.
type RunSummary struct {
	ID       string `json:"id"`
	AgentKey string `json:"agentKey"`
	Layer    string `json:"layer"`
	Status   string `json:"status"`
	// SkillVersionID is the version the run cited, which is what makes it reproducible.
	SkillVersionID string `json:"skillVersionId"`
	// ModelConfigID is the model the policy NAMED and ResponseModel is the one that answered.
	// Both travel because section 13 requires the difference to be visible, and a list showing
	// only the requested model would hide a run that fell back.
	ModelConfigID string `json:"modelConfigId"`
	ResponseModel string `json:"responseModel"`
	WorkflowRunID string `json:"workflowRunId"`
	StageRunID    string `json:"stageRunId"`
	InputSummary  string `json:"inputSummary"`
	ErrorCode     string `json:"errorCode"`
	StartedAt     string `json:"startedAt"`
	FinishedAt    string `json:"finishedAt"`
}

// RunTrace is one run with its messages and tool calls.
//
// Section 16 lists what a run's record holds, and this is the part a reviewer reads. It
// deliberately does NOT include a private reasoning trace: SECURITY section 7 and section 16
// both require the record to be reviewable "不要求/记录私有 Chain of Thought", so what travels
// is the reasonSummary the model itself produced, the tool calls it made and the versions it
// produced.
type RunTrace struct {
	Run       RunSummary     `json:"run"`
	Messages  []MessageView  `json:"messages"`
	ToolCalls []ToolCallView `json:"toolCalls"`
	// Output is the validated output, which is what the schema accepted. It is included
	// because a reviewer checking a stage's result needs it, and it has already been checked
	// against a contract — but it is bounded by the same storage cap the record uses.
	Output string `json:"output"`
}

// MessageView is one message as a reader sees it.
type MessageView struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
	// ScopeKey is shown because a reviewer debugging a recall needs to see which conversation
	// a message was filed under. It is a structured key and carries no secret.
	ScopeKey string `json:"scopeKey"`
}

// ToolCallView is one tool call as a reader sees it.
type ToolCallView struct {
	ID         string `json:"id"`
	Sequence   int    `json:"sequence"`
	ToolKey    string `json:"toolKey"`
	Status     string `json:"status"`
	ErrorCode  string `json:"errorCode"`
	InputJSON  string `json:"inputJson"`
	OutputJSON string `json:"outputJson"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
}

// ListRuns returns a project's runs newest first.
func (i *Inspector) ListRuns(ctx context.Context, projectID string, limit int) ([]RunSummary, error) {
	if !i.Available() {
		return nil, agent.UnavailableError()
	}
	scope, err := requireProject(projectID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultRunLimit
	}
	if limit > MaxRunLimit {
		limit = MaxRunLimit
	}
	runs, err := i.reader.ListRuns(ctx, scope, limit)
	if err != nil {
		return nil, err
	}
	summaries := make([]RunSummary, 0, len(runs))
	for _, run := range runs {
		summaries = append(summaries, summariseRun(run))
	}
	return summaries, nil
}

// GetTrace returns one run with its messages and tool calls.
//
// The run is read FIRST and its project checked against the caller's, so a trace cannot be
// fetched by naming a run in another project — the same boundary every tool applies, applied
// here because this surface is reachable from the frontend rather than only from a run.
func (i *Inspector) GetTrace(ctx context.Context, projectID, runID string) (RunTrace, error) {
	if !i.Available() {
		return RunTrace{}, agent.UnavailableError()
	}
	scope, err := requireProject(projectID)
	if err != nil {
		return RunTrace{}, err
	}
	trimmed := strings.TrimSpace(runID)
	if trimmed == "" {
		return RunTrace{}, agent.InvalidError("A run is required.")
	}
	run, err := i.reader.GetRun(ctx, trimmed)
	if err != nil {
		return RunTrace{}, err
	}
	if run.ProjectID != scope {
		return RunTrace{}, agent.SecurityError("That run belongs to another project.")
	}
	messages, err := i.reader.ListMessages(ctx, run.ID)
	if err != nil {
		return RunTrace{}, err
	}
	calls, err := i.reader.ListToolCalls(ctx, run.ID)
	if err != nil {
		return RunTrace{}, err
	}
	trace := RunTrace{
		Run:       summariseRun(run),
		Messages:  make([]MessageView, 0, len(messages)),
		ToolCalls: make([]ToolCallView, 0, len(calls)),
		Output:    run.ValidatedOutputJSON,
	}
	for _, message := range messages {
		trace.Messages = append(trace.Messages, MessageView{
			ID: message.ID, Role: string(message.Role), Content: message.Content,
			CreatedAt: formatTimestamp(message.CreatedAt), ScopeKey: message.ScopeKey,
		})
	}
	for _, call := range calls {
		trace.ToolCalls = append(trace.ToolCalls, ToolCallView{
			ID: call.ID, Sequence: call.Sequence, ToolKey: call.ToolKey,
			Status: string(call.Status), ErrorCode: call.ErrorCode,
			InputJSON: call.InputJSON, OutputJSON: call.OutputJSON,
			StartedAt:  formatTimestamp(call.StartedAt),
			FinishedAt: formatTimestamp(call.FinishedAt),
		})
	}
	return trace, nil
}

// requireProject trims and checks a project identifier.
func requireProject(projectID string) (string, error) {
	trimmed := strings.TrimSpace(projectID)
	if trimmed == "" {
		return "", agent.InvalidError("A project is required.")
	}
	return trimmed, nil
}

// summariseRun reduces a run to what a list view shows.
func summariseRun(run agent.AgentRun) RunSummary {
	return RunSummary{
		ID: run.ID, AgentKey: run.AgentKey, Layer: string(run.Layer),
		Status: string(run.Status), SkillVersionID: run.SkillVersionID,
		ModelConfigID: run.ModelConfigID, ResponseModel: run.ResponseModel,
		WorkflowRunID: run.WorkflowRunID,
		StageRunID:    run.StageRunID, InputSummary: run.InputSummary,
		ErrorCode:  run.ErrorCode,
		StartedAt:  formatTimestamp(run.StartedAt),
		FinishedAt: formatTimestamp(run.FinishedAt),
	}
}

// formatTimestamp renders a time for a DTO.
//
// The zero time becomes the empty string rather than "0001-01-01T00:00:00Z", because a run
// that has not finished has no finish time and a reader comparing one against a real instant
// would be misled by a placeholder.
func formatTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
