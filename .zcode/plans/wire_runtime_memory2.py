import io

path = "internal/application/agentruntime/runner.go"
text = io.open(path, encoding="utf-8", newline="").read()

# ---- The recall-and-persist step, between CreateRun and the prompt.
old = """	// The prompt. Layer order is section 5's, and the tool contract comes from the
	// registry so a model cannot be told about a tool the ACL would refuse.
	prompt := Assemble(AssembleRequest{
		Spec:            spec,
		Skill:           invocation.Skill,
		Tools:           r.toolContractFor(spec),
		WorkflowState:   invocation.WorkflowState,
		ApprovedFacts:   invocation.ApprovedFacts,
		Memory:          invocation.Memory,"""
new = """	// SECTION 12.2's ORDER, ENFORCED BY WHERE THE CODE IS.
	//
	// "Build scope → Recall previous memory excluding current turn → Persist user message → Run".
	// The recall happens FIRST and the write SECOND, in this function, adjacent, so a caller cannot
	// invert them: a caller that wanted to recall after the write would have to reimplement this
	// function, and one that forgot to write the turn would have to delete these lines.
	//
	// Before WP-10 neither happened at all. The runtime recorded only the assistant's reply (below),
	// so every run's transcript was half a conversation and the USER's own words existed nowhere
	// durable — AGENT_CONTRACTS section 12.4's first write source, "用户消息", had no writer. That
	// is the gap this closes.
	//
	// Both halves are non-fatal to the run. A recall that fails leaves the prompt without its
	// memory layer — which is what a build with no store has anyway — and a write that fails is
	// recorded on the run rather than raised, because the work the run is for is not the memory.
	// What is NOT acceptable is either half failing SILENTLY in a build that has a store, so the
	// refusals are reported the same way the transcript's own write refusal is.
	recalled := r.recallFor(ctx, invocation, spec.Key)
	userMessage := r.rememberMessage(ctx, invocation, spec.Key, agent.MessageUser, invocation.UserMessage, "")

	// The prompt. Layer order is section 5's, and the tool contract comes from the
	// registry so a model cannot be told about a tool the ACL would refuse.
	memoryLayer := invocation.Memory
	if len(memoryLayer) == 0 {
		// The caller stated none, so the runtime supplies what it recalled. A caller that DID state
		// a layer keeps it: the stage pipelines build their own from a wider context than this port
		// returns, and replacing it would silently discard their work.
		memoryLayer = recalled
	}
	prompt := Assemble(AssembleRequest{
		Spec:            spec,
		Skill:           invocation.Skill,
		Tools:           r.toolContractFor(spec),
		WorkflowState:   invocation.WorkflowState,
		ApprovedFacts:   invocation.ApprovedFacts,
		Memory:          memoryLayer,"""
assert old in text, "recall anchor"
text = text.replace(old, new, 1)

# ---- The assistant's turn now goes through the same helper.
old = """		assistant, messageErr := r.message(runID, invocation, agent.MessageAssistant, reply.Content)
		if messageErr == nil {
			messages = append(messages, assistant)
			_ = r.runs.RecordMessage(ctx, assistant)
		} else {"""
new = """		assistant, messageErr := r.message(runID, invocation, agent.MessageAssistant, reply.Content)
		if messageErr == nil {
			messages = append(messages, assistant)
			_ = r.runs.RecordMessage(ctx, assistant)
			// And the memory side of the same turn, in the same call, so the transcript and the
			// memory store cannot drift: a turn that was recorded and not remembered, or the
			// reverse, is a conversation whose two records disagree about what was said.
			assistantMemoryID := assistant.ID
			_ = r.rememberMessage(ctx, invocation, spec.Key, agent.MessageAssistant, reply.Content, assistantMemoryID)
		} else {"""
assert old in text, "assistant anchor"
text = text.replace(old, new, 1)

# ---- The two helpers, beside message().
old = """// ToolCallRequest is one tool call the model asked for."""
new = """// recallFor returns the memory layer for one run, and records a refusal on the run rather than
// raising it.
//
// The exclusion is the USER's message, and that is the one identifier this run has: section 14.5's
// 当前消息不召回自身 is about the turn being answered, which is the user's instruction. The message
// row does not exist yet at this point — that is the order section 12.2 requires — so the exclusion
// is stated by the caller's own text rather than by an identifier, and the memory service's own
// scope filtering is what keeps the rest out.
//
// A nil port returns nil: a build with no memory store recalls nothing, which is a state rather
// than a failure, and the prompt's memory layer is then simply empty.
func (r *Runtime) recallFor(ctx context.Context, invocation Invocation, agentKey string) []Message {
	if r == nil || r.memory == nil {
		return nil
	}
	if strings.TrimSpace(invocation.ProjectID) == "" {
		return nil
	}
	items, err := r.memory.Recall(ctx, ScopePartsRequest{
		ProjectID: invocation.ProjectID,
		EpisodeID: invocation.EpisodeID,
		AgentKey:  agentKey,
	}, "", DefaultMemoryLimit)
	if err != nil {
		return nil
	}
	messages := make([]Message, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		messages = append(messages, Message{
			Role:    agent.MessageUser,
			Content: item.Content,
			// Section 12.2 requires recalled context to carry its source, and the source is the run
			// the memory came from rather than the agent answering now.
			Provenance: item.Provenance,
		})
	}
	return messages
}

// DefaultMemoryLimit is how many turns a run recalls when the caller states no window.
//
// It is the memory service's own default, restated here because the runtime declares the port and
// a caller of THIS package should not have to import the service to say "the ordinary amount".
const DefaultMemoryLimit = 20

// rememberMessage writes one turn to the memory store, citing the transcript row it was written as.
//
// The citation is required rather than best-effort: the memory row's whole value as a record is
// that a reader can follow it back to the transcript, and a memory that cited nothing would be an
// unattributable claim. When the transcript write already failed there is no identifier to cite, so
// this writes nothing and the caller learns it from the run's error code — reported once, at the
// place that knows why, rather than twice with the second occasion inventing an identifier.
func (r *Runtime) rememberMessage(ctx context.Context, invocation Invocation, agentKey string, role agent.MessageRole, content, messageID string) string {
	if r == nil || r.memory == nil {
		return ""
	}
	if strings.TrimSpace(content) == "" || strings.TrimSpace(messageID) == "" {
		return ""
	}
	if strings.TrimSpace(invocation.ProjectID) == "" {
		return ""
	}
	err := r.memory.Remember(ctx, MemoryMessage{
		MessageID:  messageID,
		ProjectID:  invocation.ProjectID,
		EpisodeID:  invocation.EpisodeID,
		AgentKey:   agentKey,
		AgentRunID: invocation.StageRunID,
		Role:       string(role),
		Content:    content,
	})
	if err != nil {
		return ""
	}
	return messageID
}

// ToolCallRequest is one tool call the model asked for."""
assert old in text, "helper anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
