import io

path = "internal/application/agentruntime/runner.go"
text = io.open(path, encoding="utf-8", newline="").read()

# ---- A recall result type, distinct from the write type: they are different directions.
old = """// MemoryMessage is one turn on its way to the memory store.
type MemoryMessage struct {"""
new = """// MemoryRecallItem is one remembered turn, on its way into the prompt.
//
// It is a distinct type from MemoryMessage because the two travel in opposite directions and carry
// different facts: a write names the message it is being recorded as, and a recall names where the
// thing it found came from. Sharing one struct would put a MessageID on results that have none and
// a Provenance on writes that cannot supply one.
type MemoryRecallItem struct {
	// Content is what was said.
	Content string
	// MessageID is the transcript row it was recorded as, when it has one. Section 12.2 requires
	// recalled context to carry its source, and this is the source's first half.
	MessageID string
	// Provenance is the run the turn belonged to, which is the source's second half.
	Provenance string
	// Role is who said it, so a prompt can render a conversation rather than a list.
	Role string
}

// MemoryMessage is one turn on its way to the memory store.
type MemoryMessage struct {"""
assert old in text, "recall item anchor"
text = text.replace(old, new, 1)

old = """	Recall(ctx context.Context, scope ScopePartsRequest, excludeMessageID string, limit int) ([]MemoryMessage, error)"""
new = """	Recall(ctx context.Context, scope ScopePartsRequest, excludeMessageID string, limit int) ([]MemoryRecallItem, error)"""
assert old in text, "recall signature anchor"
text = text.replace(old, new, 1)

# ---- Use the user message's identifier: it is what the run reports a memory failure against.
old = """	recalled := r.recallFor(ctx, invocation, spec.Key)
	userMessage := r.rememberMessage(ctx, invocation, spec.Key, agent.MessageUser, invocation.UserMessage, "")"""
new = """	recalled := r.recallFor(ctx, invocation, spec.Key)
	// The USER's turn is written here, before the model is called, which is the order section 12.2
	// states. It is written as the assistant's will be — through the same helper, citing the
	// transcript row — and the transcript row is created alongside it so the two records agree about
	// which turn this is. The identifier is the run's own plus a role suffix, because the user's
	// turn has no other name: it is not a model reply, so nothing else mints one for it.
	userMessageID := runID + ":user"
	userRecorded, userMessageErr := r.messageWithID(userMessageID, invocation, agent.MessageUser, invocation.UserMessage)
	if userMessageErr == nil {
		_ = r.runs.RecordMessage(ctx, userRecorded)
		_ = r.rememberMessage(ctx, invocation, spec.Key, agent.MessageUser, invocation.UserMessage, userRecorded.ID)
	} else if record.ErrorCode == "" {
		record.ErrorCode = "agent.message_not_stored"
	}"""
assert old in text, "user message anchor"
text = text.replace(old, new, 1)

# ---- messageWithID: one message row with a caller-chosen identifier.
old = """// message builds one message row.
func (r *Runtime) message(runID string, invocation Invocation, role agent.MessageRole, content string) (agent.AgentMessage, error) {
	id, err := r.ids.New()
	if err != nil {
		return agent.AgentMessage{}, agent.StorageError("The message could not be identified.", err)
	}"""
new = """// message builds one message row.
func (r *Runtime) message(runID string, invocation Invocation, role agent.MessageRole, content string) (agent.AgentMessage, error) {
	id, err := r.ids.New()
	if err != nil {
		return agent.AgentMessage{}, agent.StorageError("The message could not be identified.", err)
	}
	return r.messageWithID(id, invocation, role, content)
}

// messageWithID builds one message row with a stated identifier.
//
// It exists because the USER's turn must be written before the model runs, and its identifier has
// to be known before the write so the memory row can cite it in the same call. Minting one here
// would work too; naming it after the run and the role is what makes a transcript readable, and
// the derived form cannot collide with a minted identifier because a minted one never contains a
// colon.
func (r *Runtime) messageWithID(id string, invocation Invocation, role agent.MessageRole, content string) (agent.AgentMessage, error) {"""
assert old in text, "messageWithID anchor"
text = text.replace(old, new, 1)

# ---- recallFor: fill the Message from the recall item.
old = """	messages := make([]Message, 0, len(items))
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
}"""
new = """	messages := make([]Message, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		role := agent.MessageUser
		if item.Role != "" && agent.IsValidMessageRole(agent.MessageRole(item.Role)) {
			role = agent.MessageRole(item.Role)
		}
		messages = append(messages, Message{
			Role:    role,
			Content: item.Content,
			// Section 12.2 requires recalled context to carry its source, and the source is the run
			// the memory came from rather than the agent answering now.
			Provenance: item.Provenance,
		})
	}
	return messages
}"""
assert old in text, "recall fill anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
