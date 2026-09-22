import io

path = "internal/application/agentruntime/runner.go"
text = io.open(path, encoding="utf-8", newline="").read()

# ---- 1. The MemoryPort, declared beside RunStore.
old = """// Validator checks a model's output against a schema path."""
new = """// MemoryPort is what the runtime does with memory around a run.
//
// # Why the runtime owns this and not the caller
//
// AGENT_CONTRACTS section 12.2 states an ORDER: "Build scope → Recall previous memory excluding
// current turn → Persist user message → Run Decision → Persist Decision/Execution/Supervision
// summaries". The section adds that an implementation may differ "但顺序和边界不得倒置" — the order
// and the boundaries must not be inverted.
//
// The boundary that matters is between the recall and the write. A caller that recalled AFTER
// writing the current turn would recall the turn it is answering; a caller that forgot to write
// the turn at all would leave the transcript with no user message in it, which is what this build
// did until WP-10 — the runtime recorded only the assistant's reply, so every conversation was
// half-written and no session could be reconstructed.
//
// So the two acts live here, adjacent, on either side of the model call, and a caller cannot
// reorder them because it does not perform them.
//
// It is a PORT rather than the memory service because the runtime must not depend on an
// application that depends on it: the service is this package's caller.
type MemoryPort interface {
	// Recall returns what the scope remembers, excluding the message being answered.
	//
	// The identifier travels because section 14.5's first invariant is 当前消息不召回自身 and
	// filtering afterwards would be a caller's job to remember. An empty exclusion is legal and
	// means "nothing to exclude", which is the case for a run that has no message yet.
	Recall(ctx context.Context, scope ScopePartsRequest, excludeMessageID string, limit int) ([]MemoryMessage, error)
	// Remember records one turn.
	//
	// It is called for the USER's message and for the model's reply, so both halves of a
	// conversation are written. An error is REPORTED rather than fatal: a memory that could not be
	// stored must not fail the run whose work is already validated, and the run's own transcript is
	// the record of what happened either way.
	Remember(ctx context.Context, message MemoryMessage) error
}

// ScopePartsRequest is DOMAIN_MODEL section 14.4's six parts, as the runtime knows them.
//
// It is a struct rather than the existing [6]string because a caller reading `parts[4]` cannot
// tell which field that is, and the six include two the runtime always leaves empty: the tenant is
// "local" for this single-machine build and there is no workspace or session concept yet.
type ScopePartsRequest struct {
	ProjectID string
	EpisodeID string
	AgentKey  string
}

// MemoryMessage is one turn on its way to the memory store.
type MemoryMessage struct {
	// MessageID is the agent_messages row this turn was written as, so the memory cites the
	// transcript rather than being a second copy of it. It is the LINK the whole design rests on:
	// section 14.1's source columns are what make a recalled memory attributable.
	MessageID  string
	ProjectID  string
	EpisodeID  string
	AgentKey   string
	AgentRunID string
	Role       string
	Content    string
}

// Validator checks a model's output against a schema path."""
assert old in text, "memory port anchor"
text = text.replace(old, new, 1)

# ---- 2. Options and Runtime gain the port.
old = """	Artifacts     ArtifactVerifier
	Clock         Clock
	IDs           IDGenerator
}"""
new = """	Artifacts     ArtifactVerifier
	Clock         Clock
	IDs           IDGenerator
	// Memory is OPTIONAL, and its absence is a stated state rather than a degraded mode: a build
	// with no memory store recalls nothing and writes no memories, which is exactly what WP-07
	// shipped before the store existed. A caller can tell the two apart because the port is either
	// there or it is not, and nothing here invents an empty result to stand in for it.
	Memory MemoryPort
}"""
assert old in text, "options anchor"
text = text.replace(old, new, 1)

old = """	artifacts ArtifactVerifier
	clock     Clock
	ids       IDGenerator
}"""
new = """	artifacts ArtifactVerifier
	clock     Clock
	ids       IDGenerator
	memory    MemoryPort
}"""
assert old in text, "runtime struct anchor"
text = text.replace(old, new, 1)

old = """		artifacts: options.Artifacts,
		clock:     options.Clock,
		ids:       options.IDs,
	}
}"""
new = """		artifacts: options.Artifacts,
		clock:     options.Clock,
		ids:       options.IDs,
		memory:    options.Memory,
	}
}"""
assert old in text, "new anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("runner ports OK")
