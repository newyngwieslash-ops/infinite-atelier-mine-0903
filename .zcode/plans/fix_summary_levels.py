import io

# ---- Port: the window's memory type is a parameter, because a summary of messages and a
# summary of summaries read different rows.
path = "internal/application/memory/ports.go"
text = io.open(path, encoding="utf-8", newline="").read()
old = "\tUnsummarisedItems(ctx context.Context, scope memory.Scope, limit int) ([]memory.MemoryItem, error)"
new = ("\t// UnsummarisedItems returns the window a summary at this LEVEL would cover. The type is a\n"
       "\t// parameter rather than a filter applied after the read, because the two levels read\n"
       "\t// different rows and reading the wrong ones is how a summary came to condense itself: a\n"
       "\t// level-one summary covers MESSAGES, so a summary row must not be among its sources.\n"
       "\tUnsummarisedItems(ctx context.Context, scope memory.Scope, memoryType memory.MemoryType, limit int) ([]memory.MemoryItem, error)")
assert old in text, "port anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("ports OK")

# ---- Storage: filter by type in SQL.
path = "internal/infrastructure/database/memory.go"
text = io.open(path, encoding="utf-8", newline="").read()
old_lines = [
    "// UnsummarisedItems returns the memories a summary window may cover, oldest first.",
    "//",
    "// The predicate is `summarized = 0`, which is what migration 000019's column is for: a",
    "// window that selected by position would re-summarise the same turns every time it ran,",
    "// and a window that selected by time would skip a burst of activity it happened to miss.",
    "func (r *MemoryRepository) UnsummarisedItems(ctx context.Context, scope memory.Scope, limit int) ([]memory.MemoryItem, error) {",
]
new_lines = [
    "// UnsummarisedItems returns the memories a summary window may cover, oldest first.",
    "//",
    "// Two predicates, and BOTH matter:",
    "//",
    "//   - `summarized = 0`, which is what migration 000019's column is for: a window that selected",
    "//     by position would re-summarise the same turns every time it ran, and a window that",
    "//     selected by time would skip a burst of activity it happened to miss.",
    "//   - `memory_type = ?`, which the integration test forced. The first version read every type,",
    "//     so a LEVEL-ONE summary — which covers messages — found its own row (and its siblings)",
    "//     among its sources: the second summarise run produced a summary OF the first summary, and",
    "//     the window never emptied. The type is the caller's because the two levels of PRD FR-120's",
    "//     ladder read different rows.",
    "func (r *MemoryRepository) UnsummarisedItems(ctx context.Context, scope memory.Scope, memoryType memory.MemoryType, limit int) ([]memory.MemoryItem, error) {",
]
assert "\n".join(old_lines) in text, "unsummarised signature anchor"
text = text.replace("\n".join(old_lines), "\n".join(new_lines), 1)

old = """	if limit <= 0 {
		limit = appmemory.MaxSummaryWindow
	}
	clauses, arguments := scopeClauses(scope)
	clauses = append(clauses, "summarized = 0", "deleted_at = ''")
	arguments = append(arguments, limit)"""
new = """	if !memory.IsValidMemoryType(memoryType) {
		return nil, memory.InvalidError("The memory type is not recognised.")
	}
	if limit <= 0 {
		limit = appmemory.MaxSummaryWindow
	}
	clauses, arguments := scopeClauses(scope)
	clauses = append(clauses, "memory_type = ?", "summarized = 0", "deleted_at = ''")
	arguments = append(arguments, string(memoryType), limit)"""
assert old in text, "unsummarised body anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("storage OK")

# ---- Service: the two levels read their own type.
path = "internal/application/memory/summary.go"
text = io.open(path, encoding="utf-8", newline="").read()
old = """	// Unsummarised EPISODIC memories, which is what "the messages since the last
		// summary" is on the memory side. The transcript is not read here: a summary has to
		// cite memory rows, because those are what the source table's foreign key points at
		// and what survives the run that produced them.
		return s.items.UnsummarisedItems(ctx, scope, window)
	}
	// Level two: the summaries of this episode that are not yet condensed into a parent.
	// The same `summarized` flag is reused at this level, so a summary is read once by
	// whichever parent covers it.
	candidates, err := s.items.UnsummarisedItems(ctx, scope, window)
	if err != nil {
		return nil, err
	}
	summaries := make([]memory.MemoryItem, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Type == memory.TypeSummary {
			summaries = append(summaries, candidate)
		}
	}
	if len(summaries) > MaxSummaryParents {
		summaries = summaries[:MaxSummaryParents]
	}
	return summaries, nil
}"""
new = """	// Unsummarised EPISODIC memories, which is what "the messages since the last summary" is on
		// the memory side. The transcript is not read here: a summary has to cite memory rows,
		// because those are what the source table's foreign key points at and what survives the run
		// that produced them.
		//
		// THE TYPE IS THE LEVEL'S, and asking for the right one is not an optimisation. The first
		// version asked for every type and filtered level two in Go, which meant level ONE read the
		// summary rows too: the second summarise run condensed the first summary into a new one, the
		// `summarized` flag never cleared the window, and the run after that would have condensed
		// that. The integration test caught it as "a second summarise produced a second summary of
		// the same turns".
		return s.items.UnsummarisedItems(ctx, scope, memory.TypeEpisodic, window)
	}
	// Level two: the summaries of this episode that are not yet condensed into a parent. The same
	// `summarized` flag is reused at this level, so a summary is read once by whichever parent
	// covers it.
	return s.items.UnsummarisedItems(ctx, scope, memory.TypeSummary, MaxSummaryParents)
}"""
assert old in text, "summary sources anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("summary OK")

# ---- Test double: an unlisted text gets a vector the test can PIN.
path = "internal/infrastructure/database/memory_wp10_test.go"
text = io.open(path, encoding="utf-8", newline="").read()
old = """type embedderDouble struct {
	// vectors maps a text to its vector. A text that is not in the map gets a vector orthogonal
	// to everything else, so an unlisted text never matches by accident.
	vectors map[string][]float32
	calls   int
	// available is what Available reports, so a test can exercise the no-provider path.
	available bool
}"""
new = """type embedderDouble struct {
	// vectors maps a text to its vector. A text that is not in the map gets fallback, which is a
	// vector orthogonal to everything a test registered unless the test pins it — so an unlisted
	// text never matches by accident, and a test that needs a text it cannot name in advance (a
	// summary's rendered text) says "this one is about that" by pinning the fallback BEFORE the
	// call that embeds it.
	vectors  map[string][]float32
	fallback []float32
	calls    int
	// available is what Available reports, so a test can exercise the no-provider path.
	available bool
}"""
assert old in text, "double anchor"
text = text.replace(old, new, 1)

old = """		if vector, ok := e.vectors[text]; ok {
			vectors = append(vectors, vector)
			continue
		}
		vectors = append(vectors, orthogonalVector(len(e.vectors)+1, len(e.vectors)+2))"""
new = """		if vector, ok := e.vectors[text]; ok {
			vectors = append(vectors, vector)
			continue
		}
		if len(e.fallback) > 0 {
			vectors = append(vectors, e.fallback)
			continue
		}
		vectors = append(vectors, orthogonalVector(len(e.vectors)+1, len(e.vectors)+2))"""
assert old in text, "embed anchor"
text = text.replace(old, new, 1)

old = """	summary, found, err := harness.service.Summarize(harness.ctx, appmemory.SummarizeRequest{Scope: scope, Embed: true})
	if err != nil || !found {
		t.Fatalf("Summarize: found=%v err=%v", found, err)
	}
	// The summary's own embedding must be the question's, so the search finds it. The double maps
	// texts to vectors, and the summary's text is a rendering, so this is how a test says "this
	// summary is about that question".
	embedder.vectors[summary.Content] = questionVector"""
new = """	// The summary's text is a rendering this test cannot name in advance, so the double's fallback
	// is pinned to the question's vector BEFORE the call that embeds it: that is how a test says
	// "this summary is about that question". Pinning it after the fact was the first version's
	// mistake, and it failed because the row had already been written with the old vector.
	embedder.fallback = questionVector
	summary, found, err := harness.service.Summarize(harness.ctx, appmemory.SummarizeRequest{Scope: scope, Embed: true})
	if err != nil || !found {
		t.Fatalf("Summarize: found=%v err=%v", found, err)
	}"""
assert old in text, "deep recall anchor"
text = text.replace(old, new, 1)

old = """	// And it is searchable again, which is the whole point of rebuilding.
	query := make([]float32, 256)
	query[5] = 1
	embedder.vectors["改过的记忆"] = query"""
new = """	// And it is searchable again, which is the whole point of rebuilding. The mapping is registered
	// BEFORE the rebuild, because the rebuild is the call that embeds the row; registering it after
	// would leave the stored vector as the fallback and the search would find nothing.
	query := make([]float32, 256)
	query[5] = 1
	embedder.vectors["改过的记忆"] = query"""
assert old in text, "rebuild anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("test OK")
