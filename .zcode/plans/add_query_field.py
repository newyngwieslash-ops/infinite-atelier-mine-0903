import io
path = "internal/application/agentruntime/runner.go"
text = io.open(path, encoding="utf-8", newline="").read()
old = """type ScopePartsRequest struct {
	ProjectID string
	EpisodeID string
	AgentKey  string
}"""
new = """type ScopePartsRequest struct {
	ProjectID string
	EpisodeID string
	AgentKey  string
	// Query is what the run is about to do, which the scored channels need: a semantic search
	// without something to be similar to has nothing to rank. It is the user's own words when the
	// run has them, because those are what the run is actually answering.
	Query string
}"""
assert old in text, "scope request anchor"
text = text.replace(old, new, 1)
old = """	items, err := r.memory.Recall(ctx, ScopePartsRequest{
		ProjectID: invocation.ProjectID,
		EpisodeID: invocation.EpisodeID,
		AgentKey:  agentKey,
	}, "", DefaultMemoryLimit)"""
new = """	items, err := r.memory.Recall(ctx, ScopePartsRequest{
		ProjectID: invocation.ProjectID,
		EpisodeID: invocation.EpisodeID,
		AgentKey:  agentKey,
		Query:     invocation.UserMessage,
	}, "", DefaultMemoryLimit)"""
assert old in text, "recall call anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
