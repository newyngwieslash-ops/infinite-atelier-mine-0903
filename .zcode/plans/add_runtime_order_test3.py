import io

path = "internal/application/agentruntime/runner_test.go"
text = io.open(path, encoding="utf-8", newline="").read()

# THE FILE'S OWN LINE ENDING. This file is CRLF while most of the tree is LF, so an anchor written
# with bare \n silently does not match — the same trap WP-09's mutation harness documents, hit here
# by the test patch. Every anchor below is rewritten before it is matched.
ending = "\r\n" if "\r\n" in text else "\n"

def anchor(body):
    return body.replace("\n", ending)

old = anchor("""	// The model's own turn was recorded.
	if len(harness.store.messages) != 1 || harness.store.messages[0].Role != agent.MessageAssistant {
		t.Fatalf("the store holds %+v", harness.store.messages)
	}""")
new = anchor("""	// BOTH TURNS were recorded, and the user's came first.
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
	}""")
assert old in text, "transcript anchor"
text = text.replace(old, new, 1)

addition = anchor('''
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
''')
text = text.rstrip("\r\n") + ending + addition
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK, ending is", repr(ending))
