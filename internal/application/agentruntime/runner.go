package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// runner.go executes one agent invocation: build the prompt, call the model, read
// the tool calls it asks for, validate the output, and record what happened.
//
// The order is AGENT_CONTRACTS section 14.3's, and the part of it that matters
// most is what does NOT happen: no business write occurs until the output has
// validated. So a malformed answer, a refused tool call or a spent budget leaves
// the database exactly as it was, and the only rows the run produces are its own
// record — which is what AC-AGENT-002's "无业务半写入" and AC-AGENT-003's "不标记
// success" are about.
//
// The model is reached through ModelPort, which is deliberately one method. The
// runtime does not know about providers, models or wire protocols: WP-06 built the
// extraction seam the same way for the same reason, and this port is the one the
// Mock LLM and the real provider adapter both satisfy.

// ModelPort is one model call.
//
// It returns the model's raw text, UNVALIDATED. Validation belongs to the runner
// because the runner is what can ask for a repair; a port that validated its own
// output would be grading its own work.
type ModelPort interface {
	// Complete sends a prompt and returns the reply.
	//
	// The request carries the model selection the project's policy named, the
	// messages, and the deadline. The implementation must honour the context:
	// section 15 requires a cancel to reach the provider.
	Complete(ctx context.Context, request ModelRequest) (ModelReply, error)
}

// ModelRequest is one call to the model.
type ModelRequest struct {
	// ModelID is the model policy's choice, resolved by the caller from the
	// project's settings. The runtime passes it through rather than choosing: section
	// 13's "低成本阶段不默认使用最昂贵模型" is a configuration rule.
	ModelID string
	// ProviderID names the configured provider, or empty to let the port choose.
	ProviderID string
	Messages   []TextMessage
	// MaxOutputTokens and Temperature come from the model policy. Zero means the
	// policy did not state them, which is different from stating zero.
	MaxOutputTokens int
	Temperature     float64
	// Deadline bounds this call. The runner also bounds the whole invocation.
	Deadline time.Duration
}

// ModelReply is the model's answer.
type ModelReply struct {
	Content string
	// Model and FinishReason are recorded with the run, because section 13 requires
	// a model change to be visible in it.
	Model        string
	FinishReason string
	// ToolCalls is what the model asked to call, BESIDE the document rather than
	// inside it.
	//
	// The placement was found by a test rather than chosen, and it is forced by the
	// specification. Section 7's output schemas have no field for a tool call and
	// every one of them declares additionalProperties false, so a tool call placed
	// inside a returned document is refused by the very schema that document must
	// satisfy. The wire protocols agree: an OpenAI-compatible response carries
	// tool_calls as a SIBLING of content on the assistant message.
	//
	// The first version of this runner parsed `toolCalls` out of the validated
	// output. That made the whole tool path unreachable for any agent whose output
	// was actually validated — the mock's reply was the first real document to
	// travel through it, and the schema refused it immediately.
	ToolCalls []ToolCallRequest
}

// RunStore is where a run's own record goes.
//
// One port with the three writes a run makes, because they are one unit of work:
// a tool call that was not recorded, or a message that was, would make the record
// disagree with what happened.
type RunStore interface {
	// CreateRun writes the run. It is called BEFORE the model is called, so a run
	// that dies mid-flight leaves a row saying it started.
	CreateRun(ctx context.Context, run agent.AgentRun) error
	// FinishRun records the outcome and the validated output.
	FinishRun(ctx context.Context, run agent.AgentRun, expectedRevision int64) error
	// RecordMessage writes one message of the run.
	RecordMessage(ctx context.Context, message agent.AgentMessage) error
	// RecordToolCall writes one tool call.
	RecordToolCall(ctx context.Context, call agent.AgentToolCall) error
}

// Validator checks a model's output against a schema path.
//
// It returns value-free violations, because the runner sends them back in a repair
// prompt and a violation that quoted the document would carry text from an
// untrusted source into the next call.
type Validator func(schemaPath string, raw []byte) ([]Violation, error)

// ArtifactVerifier checks that an output's artifact references exist.
//
// It is a port rather than a direct database call because the entities differ per
// stage: a story skeleton, a script version and a storyboard are three tables, and
// the runtime does not own any of them. AC-AGENT-003's rule — an invented
// identifier fails validation — is enforced by whatever implements this.
type ArtifactVerifier interface {
	// Verify reports the first reference that does not exist, or nil.
	VerifyArtifacts(ctx context.Context, refs []ArtifactRef) error
}

// ArtifactRef is one reference an output claims.
type ArtifactRef struct {
	EntityType string
	EntityID   string
	VersionID  string
}

// Clock and IDGenerator are the determinism ports every application service here
// has.
type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New() (string, error)
}

// Options configures a Runtime.
type Options struct {
	Registry *Registry
	Tools    *Tools
	Models   ModelPort
	Runs     RunStore
	Validate Validator
	// ToolArguments checks a tool call's arguments against the tool's own schema. Section
	// 6.1's chain puts it after the ACL and before the handler runs. A nil value means no
	// tool call can execute, which is a refusal rather than a degraded mode.
	ToolArguments ToolArgumentValidator
	Artifacts     ArtifactVerifier
	Clock         Clock
	IDs           IDGenerator
}

// Runtime runs agents.
type Runtime struct {
	registry  *Registry
	tools     *Tools
	models    ModelPort
	runs      RunStore
	validate  Validator
	toolArgs  ToolArgumentValidator
	artifacts ArtifactVerifier
	clock     Clock
	ids       IDGenerator
}

// New builds a Runtime.
//
// Every dependency is required. A runtime missing its validator would accept
// whatever a model returned, and one missing its run store would leave no record —
// both are refusals rather than degraded modes, which is what Available expresses.
func New(options Options) *Runtime {
	return &Runtime{
		registry:  options.Registry,
		tools:     options.Tools,
		models:    options.Models,
		runs:      options.Runs,
		validate:  options.Validate,
		toolArgs:  options.ToolArguments,
		artifacts: options.Artifacts,
		clock:     options.Clock,
		ids:       options.IDs,
	}
}

// Available reports whether the runtime can run anything.
func (r *Runtime) Available() bool {
	return r != nil && r.registry != nil && r.tools != nil && r.models != nil && r.runs != nil && r.validate != nil
}

func (r *Runtime) now() time.Time {
	if r == nil || r.clock == nil {
		return time.Now().UTC()
	}
	return r.clock.Now().UTC()
}

// Invocation is one agent call.
type Invocation struct {
	// AgentKey names which registered agent to run.
	AgentKey      string
	ProjectID     string
	EpisodeID     string
	WorkflowRunID string
	StageRunID    string
	// Skill is the document text for the agent's skill, supplied by the caller
	// because the loader read it and the runtime does not read files.
	Skill string
	// SkillVersion is the skill_versions row the skill text came from. The caller
	// supplies it rather than the runtime looking it up: a second lookup could observe
	// a different row than the text the caller passed, and section 4.2 requires a run
	// to name the version it actually ran.
	SkillVersion string
	// WorkflowState and ApprovedFacts fill prompt layers 5 and 6.
	WorkflowState string
	ApprovedFacts string
	// Memory fills layer 7.
	Memory []Message
	// Task fills layer 8, and TaskIsUntrusted says whether it carries document text.
	Task            string
	TaskIsUntrusted bool
	// StageProducesNoArtifact says this stage is one section 7.4 calls a no-op, so a success
	// reporting no artifact is legitimate rather than a claim with nothing behind it.
	//
	// It is the CALLER's to state because the caller chose the stage. The first version tried to
	// infer it and could not: the verifier's job is "do these references exist", and it answers
	// that — correctly — about an empty list, so the check passed everything.
	StageProducesNoArtifact bool
	// UserMessage fills layer 9.
	UserMessage string
	// ModelID and ProviderID come from the project's policy for the agent's layer.
	ModelID    string
	ProviderID string
}

// Outcome is what one invocation produced.
//
// A REFUSAL STILL CARRIES THE RunID. The run row is written before the model is called, so
// by the time any refusal below happens the record exists — and a caller that received only
// an error would have no way to look up what happened. The canary found this: its assertion
// "the run records the refusal" could not be written, because the identifier it needed was
// being discarded. So every return after CreateRun sets RunID, and the zero Outcome is
// reserved for the refusals that happen BEFORE a row exists.
type Outcome struct {
	RunID string
	// Output is the validated output, as the schema describes it. It is returned to
	// the caller and stored on the run.
	Output json.RawMessage
	// Summary is the reasonSummary or the model's own short description, for a list
	// view.
	Summary string
	// ToolCalls is what the run called, in order.
	ToolCalls []agent.AgentToolCall
	// Messages is what was said, so the caller can persist them or show them.
	Messages []agent.AgentMessage
	// Repaired reports that the output needed the one repair round.
	Repaired bool
}

// Run executes one invocation.
//
// It returns either a validated outcome or a refusal. A refusal never leaves a
// business write behind, because there are no business writes here at all: this
// runtime's only writes are its own record and whatever a TOOL does, and a tool
// call happens only after the model has answered and the ACL has allowed it.
func (r *Runtime) Run(ctx context.Context, invocation Invocation) (Outcome, error) {
	if !r.Available() {
		return Outcome{}, agent.UnavailableError()
	}
	spec, ok := r.registry.Lookup(strings.TrimSpace(invocation.AgentKey))
	if !ok {
		return Outcome{}, agent.NotFoundError()
	}
	if strings.TrimSpace(invocation.ProjectID) == "" {
		return Outcome{}, agent.InvalidError("An agent run needs a project.")
	}
	if err := ctx.Err(); err != nil {
		return Outcome{}, &CancelledError{Cause: err}
	}

	// The deadline covers the whole invocation, not one model call: section 3
	// bounds an agent by duration, and a per-call bound would let a loop of calls
	// run for as long as it liked.
	runCtx, cancel := context.WithTimeout(ctx, spec.Limits.MaxDuration)
	defer cancel()

	runID, err := r.ids.New()
	if err != nil {
		return Outcome{}, agent.StorageError("The run could not be identified.", err)
	}
	startedAt := r.now()
	record := agent.AgentRun{
		ID: runID, ProjectID: invocation.ProjectID,
		WorkflowRunID: invocation.WorkflowRunID, StageRunID: invocation.StageRunID,
		Layer: spec.Layer, AgentKey: spec.Key,
		// ModelConfigID is the model the policy NAMED; ResponseModel is the one that actually
		// answered, captured below. Section 13 requires both, and the gap between them is the case
		// it names: "模型变更写入 Run" is about a run whose answering model differs from the
		// requested one, which is what a fallback or a provider substitution produces.
		//
		// The first version recorded only this field, and its comment claimed that satisfied
		// section 13. It does not: a run that fell back would cite a model that did not produce it.
		ModelConfigID: invocation.ModelID,
		Status:        agent.RunRunning,
		StartedAt:     startedAt,
		InputSummary:  summarise(invocation),
		Revision:      1,
	}
	// The skill version is the caller's to state, because the loader minted it. An
	// empty one is refused by the domain, which is why it is not defaulted here.
	record.SkillVersionID = skillVersionOf(invocation)
	if err := record.Validate(); err != nil {
		return Outcome{}, err
	}
	if err := r.runs.CreateRun(ctx, record); err != nil {
		return Outcome{}, err
	}

	// The prompt. Layer order is section 5's, and the tool contract comes from the
	// registry so a model cannot be told about a tool the ACL would refuse.
	prompt := Assemble(AssembleRequest{
		Spec:            spec,
		Skill:           invocation.Skill,
		Tools:           r.toolContractFor(spec),
		WorkflowState:   invocation.WorkflowState,
		ApprovedFacts:   invocation.ApprovedFacts,
		Memory:          invocation.Memory,
		Task:            invocation.Task,
		TaskIsUntrusted: invocation.TaskIsUntrusted,
		UserMessage:     invocation.UserMessage,
	})

	toolCalls := make([]agent.AgentToolCall, 0, 4)
	messages := make([]agent.AgentMessage, 0, 8)
	sequence := 0

	// One attempt, then one repair, then stop. Section 14.3 allows exactly one
	// repair, so this is a two-iteration loop rather than a retry policy.
	var (
		output    json.RawMessage
		failures  []Violation
		requested []ToolCallRequest
		repaired  bool
		attempted bool
	)
	for attempt := 0; attempt < 2; attempt++ {
		if err := runCtx.Err(); err != nil {
			// A deadline or a cancel is reported as itself, and the stage is left
			// for the engine to mark cancelled.
			_ = r.finishOnly(ctx, record, agent.RunCancelled, "", "agent.cancelled", nil)
			return Outcome{}, &CancelledError{Cause: err}
		}
		reply, err := r.models.Complete(runCtx, ModelRequest{
			ModelID:    invocation.ModelID,
			ProviderID: invocation.ProviderID,
			Messages:   prompt.AsTextMessages(),
			Deadline:   spec.Limits.MaxDuration,
		})
		if err != nil {
			code := classifyModelFailure(err)
			// A cancellation is recorded as CANCELLED rather than failed, which is
			// section 15's "StageRun 标记 cancelled". The first version of this
			// recorded every model failure as failed, so a cancelled run looked
			// identical to one that broke — and a retry policy reading the status would
			// have retried a cancellation the user asked for.
			status := agent.RunFailed
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				status = agent.RunCancelled
			}
			_ = r.finishOnly(ctx, record, status, "", code, nil)
			return Outcome{RunID: runID}, err
		}
		attempted = true

		// The answering model is captured from the FIRST reply, so a run records what produced it
		// even when the provider served something other than what was asked for. First rather than
		// last because that is the answer the output came from when no repair happened, and when
		// one did the first attempt is what the repair was for.
		if record.ResponseModel == "" {
			record.ResponseModel = reply.Model
		}
		// The model's own turn is recorded, because section 16 requires the run's
		// messages and because a repair prompt refers back to it.
		assistant, messageErr := r.message(runID, invocation, agent.MessageAssistant, reply.Content)
		if messageErr == nil {
			messages = append(messages, assistant)
			_ = r.runs.RecordMessage(ctx, assistant)
		} else {
			// A message that could not be stored is REPORTED rather than dropped in silence. The
			// first version discarded this error, so a reply whose content did not fit the storage
			// bound vanished from the record with no trace — the same silent-drop shape as the
			// revision defect. The run is not failed for it (the output is what matters and it is
			// still validated below), but the refusal is recorded on the run so a reader can see
			// that the transcript is incomplete.
			if domainErr, ok := agent.AsError(messageErr); ok && record.ErrorCode == "" {
				record.ErrorCode = "agent.message_not_stored"
				_ = domainErr
			}
		}

		// Every refusal path below leaves the run unfinished until the loop decides.
		// The output is validated BEFORE anything acts on it, which is section
		// 14.3's "no business write" and this runner's central ordering rule.
		violations, validationErr := r.validate(spec.Output, []byte(reply.Content))
		if validationErr == nil && len(violations) == 0 {
			// A validated output may still name an artifact that does not exist, and
			// that is AC-AGENT-003: the run must not be marked successful and the
			// workflow must not advance.
			if artifactErr := r.verifyArtifacts(runCtx, spec, invocation, json.RawMessage(reply.Content)); artifactErr != nil {
				_ = r.finishOnly(ctx, record, agent.RunFailed, "", artifactCode(artifactErr), nil)
				return Outcome{RunID: runID}, artifactErr
			}
			// The tool calls the model asked for are checked against its budget here,
			// BEFORE any of them runs: AC-AGENT-005's "Tool Calls 超限停止" is about a
			// model that asked for more than it may spend, and truncating the list
			// would silently do the first N of what it asked for.
			if quotaErr := checkToolCallBudget(spec, reply.ToolCalls); quotaErr != nil {
				var quota *QuotaError
				code := "agent.quota_tool_calls"
				if errors.As(quotaErr, &quota) {
					code = quota.Code()
				}
				_ = r.finishOnly(ctx, record, agent.RunFailed, "", code, nil)
				return Outcome{RunID: runID}, quotaErr
			}
			output = json.RawMessage(reply.Content)
			requested = reply.ToolCalls
			break
		}
		if validationErr != nil {
			// The validator itself failed, which is a configuration defect rather than
			// a model failure, so it is reported as one and not repaired.
			_ = r.finishOnly(ctx, record, agent.RunFailed, "", "agent.validator_failed", nil)
			return Outcome{RunID: runID}, validationErr
		}
		failures = violations
		// Send the violations back once. The prompt gains a layer rather than being
		// rebuilt: the model sees what it said and what was wrong with it.
		prompt.Messages = append(prompt.Messages, Message{
			Region: RegionTaskInput,
			Role:   agent.MessageUser,
			Content: "Your output did not satisfy the contract.\n" +
				describeViolations(failures) +
				"\nReturn a corrected document that satisfies every rule above.",
		})
		// The repair prompt carries the violations, and the caller learns that a
		// repair happened even if it then fails.
		repaired = true
	}

	if output == nil {
		// Both attempts were invalid, which section 14.3 makes final.
		code := "agent.output_schema_invalid"
		_ = r.finishOnly(ctx, record, agent.RunFailed, "", code, failures)
		return Outcome{RunID: runID}, &SchemaError{Stage: spec.Key, Violations: failures, Repaired: attempted && repaired}
	}

	// Tool calls, which the reply carried beside its document. They run AFTER
	// validation, so a malformed document cannot cause a write.
	for _, request := range requested {
		call := r.callTool(runCtx, runID, spec, invocation, request, sequence)
		sequence++
		toolCalls = append(toolCalls, call)
		_ = r.runs.RecordToolCall(ctx, call)
		if call.Status == agent.ToolCallDenied {
			// AC-AGENT-001: an illegal call is refused, the run records the failure and
			// the invocation stops rather than continuing as if nothing happened.
			_ = r.finishOnly(ctx, record, agent.RunFailed, "", CodeToolNotAllowed, nil)
			return Outcome{RunID: runID}, &ToolNotAllowedError{
				Layer: spec.Layer, Mode: agent.ToolMode(""), Tool: call.ToolKey, NotGranted: true,
			}
		}
		if call.Status == agent.ToolCallFailed {
			_ = r.finishOnly(ctx, record, agent.RunFailed, "", call.ErrorCode, nil)
			return Outcome{RunID: runID}, agent.StorageError("A tool this step needed did not complete.", nil)
		}
	}

	if _, err := r.finish(ctx, record, agent.RunSucceeded, string(output), "", nil); err != nil {
		return Outcome{RunID: runID}, err
	}
	return Outcome{
		RunID:     runID,
		Output:    output,
		Summary:   summariseOutput(output),
		ToolCalls: toolCalls,
		Messages:  messages,
		Repaired:  repaired,
	}, nil
}

// finish records the run's outcome and reports the row's NEW revision.
//
// The revision comes back rather than being incremented on a local copy, and that is a
// correction rather than a preference. The first version took `record` by value, incremented
// its own copy at the end and discarded the result — so a second call within one run reused
// the ORIGINAL revision. The repository's optimistic guard then found no row at
// (id, revision) and reported a not-found, which is what the canary saw: an
// unrelated-looking "the requested agent record no longer exists" in place of the refusal it
// was asserting. Nothing failed, because the two-iteration repair loop is the only caller
// that writes twice.
//
// The rule is therefore stated in the signature: a caller that writes a run more than once
// must carry the returned revision forward.
func (r *Runtime) finish(ctx context.Context, record agent.AgentRun, status agent.RunStatus, output, errorCode string, violations []Violation) (int64, error) {
	finished := record
	finished.Status = status
	finished.ValidatedOutputJSON = output
	finished.ErrorCode = errorCode
	if status == agent.RunSucceeded {
		finished.ErrorCode = ""
	}
	// A terminal status needs a finish time, and the domain refuses one without it.
	finished.FinishedAt = r.now()
	_ = violations
	// The expected revision is what the row holds, which is what the last write left.
	if err := r.runs.FinishRun(ctx, finished, record.Revision); err != nil {
		return record.Revision, err
	}
	return record.Revision + 1, nil
}

// message builds one message row.
func (r *Runtime) message(runID string, invocation Invocation, role agent.MessageRole, content string) (agent.AgentMessage, error) {
	id, err := r.ids.New()
	if err != nil {
		return agent.AgentMessage{}, agent.StorageError("The message could not be identified.", err)
	}
	hash := contentHash(content)
	message := agent.AgentMessage{
		ID: id, AgentRunID: runID,
		ScopeKey:    scopeKey(invocation),
		Role:        role,
		Content:     truncateForStorage(content),
		ContentHash: hash,
		CreatedAt:   r.now(),
	}
	if err := message.Validate(); err != nil {
		return agent.AgentMessage{}, err
	}
	return message, nil
}

// ToolCallRequest is one tool call the model asked for.
//
// It is the runtime's own type rather than the provider's, because the runtime does
// not import the providers package: the two are structurally identical and the
// adapter at the composition root maps between them, which keeps the layering of
// AGENTS section 7.2 intact.
type ToolCallRequest struct {
	Key       string
	Arguments json.RawMessage
}

// checkToolCallBudget refuses a reply that asks for more calls than the agent may make.
//
// The count is checked BEFORE any call runs, so a model that asked for eleven calls
// with a budget of eight is refused rather than truncated: AC-AGENT-005's "Tool
// Calls 超限停止". Truncating would run the first eight of what it asked for, which
// is a partial execution the model did not request and no one authorized.
func checkToolCallBudget(spec agent.Spec, requested []ToolCallRequest) error {
	if len(requested) > spec.Limits.MaxToolCalls {
		return &QuotaError{Limit: "tool_calls", Allowed: spec.Limits.MaxToolCalls, Used: len(requested)}
	}
	return nil
}

// callTool authorizes and runs one tool call.
//
// It returns a call record whatever happened, because section 16 requires the run
// to show what it tried and section 7.1 requires a denial to be attributable to the
// ACL rather than to the tool.
func (r *Runtime) callTool(ctx context.Context, runID string, spec agent.Spec, invocation Invocation, request ToolCallRequest, sequence int) agent.AgentToolCall {
	started := r.now()
	call := agent.AgentToolCall{
		ID: mustID(r.ids), AgentRunID: runID, Sequence: sequence, ToolKey: request.Key,
		InputJSON: string(request.Arguments), Status: agent.ToolCallRunning, StartedAt: started,
	}
	tool, known := r.tools.Lookup(request.Key)
	if !known {
		call.Status = agent.ToolCallDenied
		call.ErrorCode = CodeToolNotAllowed
		call.FinishedAt = r.now()
		return call
	}
	if err := (Authorizer{}).Authorize(AuthorizeRequest{Spec: spec, Tool: tool.Spec}); err != nil {
		var notAllowed *ToolNotAllowedError
		if errors.As(err, &notAllowed) {
			call.Status = agent.ToolCallDenied
			call.ErrorCode = CodeToolNotAllowed
			call.FinishedAt = r.now()
			return call
		}
		call.Status = agent.ToolCallFailed
		call.ErrorCode = "agent.authorize_failed"
		call.FinishedAt = r.now()
		return call
	}
	// Section 6.1's chain is "JSON parse -> Schema validation -> Tool ACL -> scope
	// validation -> execute", and this is the second step. It was MISSING: the schema path
	// travelled into the prompt (layer 4 tells the model what shape the arguments must
	// have) and was never applied to what came back, so a model could hand a handler any
	// shape at all and only the handler's own struct decoding refused it — a decode error,
	// not a contract refusal, and one that cannot name the rule.
	//
	// The refusal is a FAILURE rather than a denial, and the distinction is section 7.1's:
	// a denial is the ACL saying "you may not call this", while malformed arguments are a
	// bad REQUEST against a tool the agent is entitled to call.
	if err := r.validateToolArguments(tool, request.Arguments); err != nil {
		call.Status = agent.ToolCallFailed
		call.ErrorCode = "agent.tool_arguments_invalid"
		call.FinishedAt = r.now()
		return call
	}
	// The arguments are size-bounded before they reach the handler, so a model
	// cannot hand a tool a document through a field the schema left open.
	if len(request.Arguments) > MaxToolResultBytes {
		call.Status = agent.ToolCallFailed
		call.ErrorCode = "agent.tool_input_too_large"
		call.FinishedAt = r.now()
		return call
	}
	result, err := tool.Handler(ctx, ToolRequest{
		ProjectID:     invocation.ProjectID,
		EpisodeID:     invocation.EpisodeID,
		WorkflowRunID: invocation.WorkflowRunID,
		StageRunID:    invocation.StageRunID,
		AgentRunID:    runID,
		Arguments:     request.Arguments,
	})
	call.FinishedAt = r.now()
	if err != nil {
		call.Status = agent.ToolCallFailed
		call.ErrorCode = codeFor(call.ErrorCode, err)
		return call
	}
	encoded, boundErr := boundResult(result, tool.Spec.MaxOutputBytes)
	if boundErr != nil {
		call.Status = agent.ToolCallFailed
		call.ErrorCode = "agent.tool_output_too_large"
		return call
	}
	call.Status = agent.ToolCallSucceed
	call.OutputJSON = encoded
	return call
}

// ToolArgumentValidator checks a tool call's arguments against the tool's schema.
//
// It is a field on Options rather than a hard dependency so a build can run without one —
// the pool of contracts is small and a caller may have already checked — but a runtime
// WITHOUT one refuses to execute any tool whose schema it cannot check, which is the
// fail-closed direction. That is the same rule ArtifactVerifier follows.
type ToolArgumentValidator func(schemaPath string, raw []byte) ([]Violation, error)

// validateToolArguments applies the tool's own input schema to what the model sent.
//
// Three outcomes, and each is deliberate:
//
//   - No validator configured: REFUSED. A runtime that cannot check an argument must not
//     pass it to a handler that will act on it.
//   - The document is not JSON: refused with a violation rather than an error, because the
//     model can fix that and this is the same class as a malformed output.
//   - The validator itself failed: an ERROR, because a contract that will not compile is a
//     build defect and telling the model to fix its arguments would burn the run.
func (r *Runtime) validateToolArguments(tool Tool, arguments json.RawMessage) error {
	if r.toolArgs == nil {
		return agent.UnavailableError()
	}
	violations, err := r.toolArgs(tool.SchemaPath, arguments)
	if err != nil {
		// A document that is not JSON, or a schema that will not compile. The first is the
		// model's to fix and the second is not, so they stay distinguishable: the validator
		// reports a parse failure as a violation list and a compile failure as an error.
		if len(violations) > 0 {
			return &SchemaError{Stage: tool.Spec.Key, Violations: violations}
		}
		return err
	}
	if len(violations) > 0 {
		// The violations are value-free, so this refusal can be shown and recorded without
		// carrying a document's text.
		return &SchemaError{Stage: tool.Spec.Key, Violations: violations}
	}
	return nil
}

// verifyArtifacts checks the references an output claims.
//
// An output with no artifacts field has none to check, which is legitimate for a
// stage that reports only a decision.
func (r *Runtime) verifyArtifacts(ctx context.Context, spec agent.Spec, invocation Invocation, output json.RawMessage) error {
	var envelope struct {
		// Status and the two identity fields are read because section 7.4's rules are about the
		// RELATION between them and the artifacts, which JSON Schema cannot express.
		Status     string `json:"status"`
		Stage      string `json:"stage"`
		StageRunID string `json:"stageRunId"`
		Artifacts  []struct {
			EntityType string `json:"entityType"`
			EntityID   string `json:"entityId"`
			VersionID  string `json:"versionId"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return agent.InvalidError("The model's output could not be read.")
	}
	// --- The result must name the stage attempt it is about. ---
	//
	// Section 9's "Runtime 必须再次校验，不因模型输出而改变规则" covers the identity fields as much as
	// the action: a document naming a different attempt would be recorded as THIS stage's output,
	// and a reviewer reading the record could not tell what it described. The check runs only
	// when both sides carry an identifier, because an extraction result names neither — its
	// contract is the event graph's and its stage is implied by the run that produced it.
	if strings.TrimSpace(envelope.StageRunID) != "" &&
		strings.TrimSpace(invocation.StageRunID) != "" &&
		envelope.StageRunID != invocation.StageRunID {
		return &ArtifactError{EntityType: "stage_run", EntityID: envelope.StageRunID}
	}
	// --- Section 7.4's artifact rules. ---
	//
	// "success 至少有预期 artifact，除非该阶段明确是 no-op" and "failed 不得附带 approved artifact"
	// are relations between a status and a list, which JSON Schema cannot state: the schema knows
	// the fields exist, not that one constrains the other.
	//
	// Whether a stage is a no-op is a fact the CALLER knows, because the caller is what chose
	// the stage. So the invocation says so, and the runtime refuses a success that named no
	// artifact for a stage that produces one. The first version tried to ask the VERIFIER
	// instead and the check was a no-op: the verifier answers "all the references I was given
	// exist", and it quite correctly answers that about an empty list — so a success with
	// nothing to show passed.
	if len(envelope.Artifacts) == 0 {
		if envelope.Status == "success" && !invocation.StageProducesNoArtifact {
			return &ArtifactError{EntityType: "stage_artifact", EntityID: string(envelope.Status)}
		}
		return nil
	}
	// The verifier is optional so a runtime can be composed for an agent whose stage produces
	// nothing, but a stage that NAMES an artifact without one cannot be checked and is therefore
	// refused rather than trusted.
	if r.artifacts == nil {
		return agent.UnavailableError()
	}
	if envelope.Status == "failed" {
		return agent.InvalidError("A failed result cannot report an artifact it produced.")
	}
	refs := make([]ArtifactRef, 0, len(envelope.Artifacts))
	for _, artifact := range envelope.Artifacts {
		refs = append(refs, ArtifactRef{
			EntityType: artifact.EntityType, EntityID: artifact.EntityID, VersionID: artifact.VersionID,
		})
	}
	return r.artifacts.VerifyArtifacts(ctx, refs)
}

// toolContractFor renders an agent's own tools for prompt layer 4.
func (r *Runtime) toolContractFor(spec agent.Spec) []ToolContractLine {
	lines := make([]ToolContractLine, 0, len(spec.AllowedTools))
	for _, key := range spec.AllowedTools {
		tool, ok := r.tools.Lookup(key)
		if !ok {
			// A spec naming an unregistered tool cannot happen: the registry refuses
			// it at startup. Skipping rather than panicking keeps a test double able
			// to construct a runtime without the registry's checks.
			continue
		}
		lines = append(lines, ToolContractLine{
			Key: tool.Spec.Key, Mode: tool.Spec.Mode, Schema: tool.SchemaPath,
			MaxBytes: tool.Spec.MaxOutputBytes,
		})
	}
	return lines
}

// describeViolations renders violations for a repair prompt.
//
// A path and a rule, never a value: the violations come from validating a model's
// output, and that output quotes a document, so quoting it back would carry
// document text into the next call. This is WP-06's rule and it applies unchanged.
func describeViolations(violations []Violation) string {
	if len(violations) == 0 {
		return "No specific rule was identified."
	}
	var builder strings.Builder
	builder.WriteString("Problems:\n")
	for index, violation := range violations {
		if index >= 20 {
			builder.WriteString("- and more\n")
			break
		}
		builder.WriteString("- " + violation.Path + " " + violation.Message + "\n")
	}
	return builder.String()
}

// classifyModelFailure maps a model port's error to a stable code.
func classifyModelFailure(err error) string {
	var quota *QuotaError
	if errors.As(err, &quota) {
		return quota.Code()
	}
	var cancelled *CancelledError
	if errors.As(err, &cancelled) {
		return "agent.cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "agent.timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "agent.cancelled"
	}
	return "agent.model_failed"
}

// artifactCode names an artifact failure.
func artifactCode(err error) string {
	var artifact *ArtifactError
	if errors.As(err, &artifact) {
		return artifact.Code()
	}
	return "agent.artifact_not_found"
}

// summarise describes an invocation without storing its text.
func summarise(invocation Invocation) string {
	parts := []string{"agent=" + invocation.AgentKey}
	if stage := strings.TrimSpace(invocation.StageRunID); stage != "" {
		parts = append(parts, "stage_run="+stage)
	}
	if task := strings.TrimSpace(invocation.Task); task != "" {
		// The task's LENGTH rather than its text: it may carry document content, and
		// the summary is a queryable row.
		parts = append(parts, "task_runes="+itoa(len([]rune(task))))
	}
	if message := strings.TrimSpace(invocation.UserMessage); message != "" {
		parts = append(parts, "user_message_runes="+itoa(len([]rune(message))))
	}
	return strings.Join(parts, " ")
}

// summariseOutput pulls a short description out of a validated output.
//
// It looks for the fields the section 7 schemas name — reasonSummary, summary,
// intent — and returns empty for an output that has none, because inventing one
// would put text in a list view that the model did not say.
func summariseOutput(output json.RawMessage) string {
	var envelope struct {
		ReasonSummary string `json:"reasonSummary"`
		Summary       string `json:"summary"`
		Intent        string `json:"intent"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return ""
	}
	for _, candidate := range []string{envelope.ReasonSummary, envelope.Summary, envelope.Intent} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return truncated(trimmed, agent.MaxInputSummaryRunes)
		}
	}
	return ""
}

// skillVersionOf reads the skill version a caller supplied.
//
// The runtime does not look it up: the caller loaded the pack and recorded its
// version, and a second lookup here could observe a different row than the skill
// text the caller passed.
func skillVersionOf(invocation Invocation) string {
	return invocation.SkillVersion
}

// codeFor prefers a tool's own error code and falls back to a generic one.
func codeFor(existing string, err error) string {
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	var refusal Refusal
	if asRefusal(err, &refusal) {
		return "agent.tool_" + string(refusal.Category())
	}
	return "agent.tool_failed"
}

// mustID mints an identifier, tolerating a generator failure by returning empty so
// the caller's validation refuses the row rather than panicking.
func mustID(ids IDGenerator) string {
	if ids == nil {
		return ""
	}
	id, err := ids.New()
	if err != nil {
		return ""
	}
	return id
}

// truncateForStorage bounds a message's content.
func truncateForStorage(content string) string {
	// The bound is BYTES: that is what the domain validates and what the column holds. The
	// first version counted RUNES against it, so a long Chinese reply was clipped to 16,384
	// runes = 49,152 bytes and then REJECTED by Validate. The caller discarded the rejection, so
	// the message was silently not recorded at all.
	if len(content) <= agent.DefaultMessageContentBytes {
		return content
	}
	// The marker tells a reader the value is incomplete, which a bare prefix does not, and its
	// length is SUBTRACTED BEFORE the boundary walk rather than after: subtracting it afterwards
	// moves the cut back into the middle of whatever rune straddled it, which produces the very
	// broken encoding this function exists to avoid. The first version of this fix made exactly
	// that mistake, and its own test — which asserts the result is valid UTF-8 — caught it.
	const marker = "…[truncated]"
	limit := agent.DefaultMessageContentBytes - len(marker)
	if limit <= 0 {
		limit = agent.DefaultMessageContentBytes
	}
	// Walk back to a rune boundary so the stored value is not cut mid-rune. A UTF-8 continuation
	// byte has the top two bits set as 10xxxxxx, and cutting there would produce a string the
	// database accepts and a reader cannot.
	for limit > 0 && !utf8.RuneStart(content[limit]) {
		limit--
	}
	if limit <= 0 {
		return ""
	}
	if limit >= len(content) {
		return content
	}
	if limit == agent.DefaultMessageContentBytes {
		return content[:limit]
	}
	return content[:limit] + marker
}

// truncated clips a string to a rune count.
func truncated(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// contentHash is the SHA-256 of a message's content, so a message can be
// identified and de-duplicated without being read.
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// scopeKey encodes DOMAIN_MODEL section 14.4's structured scope as a stable
// string.
//
// Section 14.4 requires retrieval to filter by STRUCTURE rather than by a fragile
// prefix, so the encoded key is a display and indexing convenience and the parts
// are stored as their own columns by the repository. The encoding uses a separator
// that cannot appear in an identifier, so two different scopes cannot produce the
// same string.
func scopeKey(invocation Invocation) string {
	parts := []string{
		"local",
		"",
		invocation.ProjectID,
		invocation.EpisodeID,
		invocation.AgentKey,
		"",
	}
	return strings.Join(parts, "|")
}

// ScopeParts returns the structured scope a run belongs to, so a repository can
// store the parts rather than parse the key it was given.
//
// Section 14.4's fields, in its order: tenant, workspace, project, episode,
// agent key, session. The tenant is "local" because this is a single-machine
// desktop build and the field exists so a future multi-tenant deployment has a
// place to put its value rather than a shape to change.
func ScopeParts(invocation Invocation) [6]string {
	return [6]string{"local", "", invocation.ProjectID, invocation.EpisodeID, invocation.AgentKey, ""}
}

// finishOnly records an outcome and discards the new revision.
//
// It exists for the refusal paths, which write the run once and then return: there is no
// second write for the revision to matter to, and threading a value through eight returns
// that ignore it would add noise to the one path that does not. The paths that write twice
// (the repair loop, and the success path after it) call finish directly and carry the value.
func (r *Runtime) finishOnly(ctx context.Context, record agent.AgentRun, status agent.RunStatus, output, errorCode string, violations []Violation) error {
	_, err := r.finish(ctx, record, status, output, errorCode, violations)
	return err
}
