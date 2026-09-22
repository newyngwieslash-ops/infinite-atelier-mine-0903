import io

path = "internal/infrastructure/database/memory_wp10_test.go"
text = io.open(path, encoding="utf-8", newline="").read()

old = """	if len(cleared.EmbeddingBlob) != 0 {
		t.Fatal("an edit left the old vector in place")
	}
	result, err = harness.service.RebuildEmbedding(harness.ctx, appmemory.RebuildEmbeddingRequest{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rebuilt != 1 {
		t.Fatalf("the rebuild restored %d rows, want 1", result.Rebuilt)
	}
	// And it is searchable again, which is the whole point of rebuilding. The mapping is registered
	// BEFORE the rebuild, because the rebuild is the call that embeds the row; registering it after
	// would leave the stored vector as the fallback and the search would find nothing.
	query := make([]float32, 256)
	query[5] = 1
	embedder.vectors["改过的记忆"] = query
	context, err := harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{
		Scope: scope, Query: "改过的记忆",
	})"""
new = """	if len(cleared.EmbeddingBlob) != 0 {
		t.Fatal("an edit left the old vector in place")
	}
	// The mapping is registered BEFORE the rebuild, because the rebuild is the call that embeds the
	// row: registering it after would leave the stored vector as the untouched fallback and the
	// search would find nothing, which is a harness ordering mistake rather than a defect — the
	// first version of this test made it and read as a broken rebuild.
	query := make([]float32, 256)
	query[5] = 1
	embedder.vectors["改过的记忆"] = query
	result, err = harness.service.RebuildEmbedding(harness.ctx, appmemory.RebuildEmbeddingRequest{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rebuilt != 1 {
		t.Fatalf("the rebuild restored %d rows, want 1", result.Rebuilt)
	}
	// And it is searchable again, which is the whole point of rebuilding.
	context, err := harness.service.BuildMemoryContext(harness.ctx, appmemory.MemoryContextRequest{
		Scope: scope, Query: "改过的记忆",
	})"""
assert old in text, "rebuild ordering anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
