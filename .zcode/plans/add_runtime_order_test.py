import io
path = "internal/application/agentruntime/runner_test.go"
text = io.open(path, encoding="utf-8", newline="").read()
addition = '''
// TestTheRuntimeRecallsBeforeItWritesAndRemembersBothTurns is AC-MEM-002's ordering clause.
//
// The criterion reads "Recall 在写当前消息前或排除其 ID" — recall before the current message is
// written, or exclude its id — and this build does both. The exclusion alone would not be enough
// for a run whose transcript is shared: a recall that ran AFTER the write would see the turn it is
// answering, and no identifier would tell it which row that was, because the row it would have to
// exclude is the one it just created.
//
// So the ORDER is the property, and this is the test that pins it. It drives a real run through a
// recording port and asserts the sequence, which is the only way to observe an ordering the code
// could otherwise get right for the wrong reason.
func TestTheRuntimeRecallsBeforeItWritesAndRemembersBothTurns(t *testing.T) {
	harness := newRuntimeHarness(t)
	port := &recordingMemoryPort{}
	harness.runtime = New(Options{
		Registry: harness.registry,
		Tools:    harness.tools,
		Models:   harness.model,
		Runs:     harness.store,
		Validate: func(string, []byte) ([]Violation, error) { return nil, nil },
		Clock:    harness.clock,
		IDs:      harness.ids,
		Memory:   port,
	})
	if _, err := harness.runtime.Run(context.Background(), harness.invocation()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The recall came first, exactly once.
	if len(port.calls) < 3 {
		t.Fatalf("the memory port saw %d calls: %v", len(port.calls), port.calls)
	}
	if port.calls[0] != "recall" {
		t.Fatalf("the first memory call was %q, want the recall", port.calls[0])
	}
	// Then the user's turn, then the reply: both halves of the conversation, in that order.
	if port.calls[1] != "remember:user" {
		t.Fatalf("the second memory call was %q, want the user's turn", port.calls[1])
	}
	if port.calls[2] != "remember:assistant" {
		t.Fatalf("the third memory call was %q, want the model's reply", port.calls[2])
	}
	// The recall saw the user's words as its query and neither turn as a memory, because neither
	// had been written when it ran.
	if port.queries[0] != harness.invocation().UserMessage {
		t.Fatalf("the recall was given %q as its query", port.queries[0])
	}
	// Every remembered turn cites a transcript row, which is what makes the memory attributable.
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
// is optional, and its absence is a stated state rather than a degraded mode. The test asserts the
// run SUCCEEDS and the transcript is still written, because a missing memory store must not take
// the transcript with it.
func TestTheRuntimeWithoutAMemoryPortStillRuns(t *testing.T) {
	harness := newRuntimeHarness(t)
	if _, err := harness.runtime.Run(context.Background(), harness.invocation()); err != nil {
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
'''
marker = "\n// recordingMemoryPort"
assert marker not in text, "already added"
text = text.rstrip() + "\n" + addition
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
