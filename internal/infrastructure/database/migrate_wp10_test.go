package database

import (
	"context"
	"testing"
)

// TestWP10MigrationFreshDatabase covers the three tables and the added column the
// memory aggregate and the deterministic checks depend on.
func TestWP10MigrationFreshDatabase(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, wp05HeadVersion)

	for _, table := range []string{"memory_items", "memory_summary_sources", "memory_entity_links"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 1 {
			t.Fatalf("table %s missing", table)
		}
	}

	// The review issue's source mark, which AGENT_CONTRACTS section 11.4 requires so a
	// report can say which findings came from deterministic code and which from a model.
	if queryInt(t, db, "SELECT COUNT(*) FROM pragma_table_info('review_issues') WHERE name = 'source'") != 1 {
		t.Fatal("review_issues.source missing")
	}

	foreignKeysClean(t, db)
}

// TestWP10MigrationCreatesTheMemoryIndexes pins the index list DOMAIN_MODEL section 18
// requires "at least", plus the two this build's reads need. A missing index is a silent
// performance regression, so it fails here rather than in a benchmark.
func TestWP10MigrationCreatesTheMemoryIndexes(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	indexes := []string{
		// section 18's two, on the structured columns the queries filter by.
		"idx_memory_items_scope",
		"idx_memory_entity_links_entity",
		// The project-wide reads and the embedding-version read.
		"idx_memory_items_project_type",
		"idx_memory_items_embedding",
		// The reverse lookup a reader makes from a source memory to its summaries.
		"idx_memory_summary_sources_source",
	}
	for _, index := range indexes {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='"+index+"'") != 1 {
			t.Fatalf("index %s missing", index)
		}
	}
}

// TestWP10MemoryConstraintsEnforceDocumentedValues is the table-driven check the other
// migration tests use: each closed vocabulary accepts its documented values and refuses
// everything else.
//
// It runs against the real schema rather than against the Go constants, because the point
// is that the DATABASE refuses a value the domain would never send: a migration that lost
// a CHECK would leave the domain as the only guard, and the domain is not the only writer
// in a build where a future service can be added without reading this file.
func TestWP10MemoryConstraintsEnforceDocumentedValues(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	base := `INSERT INTO memory_items (id, scope_key, scope_project, memory_type, role, content,
		importance, confidence, summarized, locked, source_type, source_id, deleted_at,
		created_at, updated_at, revision) VALUES `

	insert := func(id, memoryType, role, sourceType string) error {
		_, err := db.ExecContext(ctx, base+
			`(?, 'local||p1|||', 'p1', ?, ?, 'text', 0.5, 0.5, 0, 0, ?, '', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
			id, memoryType, role, sourceType)
		return err
	}

	for _, memoryType := range []string{"episodic", "semantic", "procedural", "artifact", "summary"} {
		if err := insert("m-"+memoryType, memoryType, "user", "message"); err != nil {
			t.Fatalf("memory_type %q was refused: %v", memoryType, err)
		}
	}
	if err := insert("m-bad-type", "daydream", "user", "message"); err == nil {
		t.Fatal("an unknown memory_type was accepted")
	}
	// The role is a bounded vocabulary, and the empty value is legal because a
	// non-episodic memory has no speaking role.
	if err := insert("m-empty-role", "semantic", "", "message"); err != nil {
		t.Fatalf("an empty role was refused: %v", err)
	}
	if err := insert("m-bad-role", "episodic", "narrator", "message"); err == nil {
		t.Fatal("an unknown role was accepted")
	}
	if err := insert("m-bad-source", "episodic", "user", "guess"); err == nil {
		t.Fatal("an unknown source_type was accepted")
	}
	// The scope key is bounded, so a caller cannot store an unbounded one.
	longKey := ""
	for len(longKey) < 401 {
		longKey += "x"
	}
	if _, err := db.ExecContext(ctx, base+
		`('m-long', ?, 'p1', 'episodic', 'user', 'text', 0.5, 0.5, 0, 0, 'message', '', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		longKey); err == nil {
		t.Fatal("an oversized scope key was accepted")
	}
	// Importance and confidence are weights, so a value outside [0,1] is refused rather
	// than clamped: a clamp would hide the caller's mistake.
	if _, err := db.ExecContext(ctx, base+
		`('m-heavy', 'local||p1|||', 'p1', 'episodic', 'user', 'text', 1.5, 0.5, 0, 0, 'message', '', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err == nil {
		t.Fatal("an importance above one was accepted")
	}

	// The summary source's foreign keys are real: a source row cannot outlive either end
	// of the relation it asserts, which is section 19's "Summary sources 有效".
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_summary_sources
		(summary_id, source_memory_id, source_order, created_at)
		VALUES ('m-episodic', 'no-such-memory', 1, '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a summary source citing a memory that does not exist was accepted")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_summary_sources
		(summary_id, source_memory_id, source_order, created_at)
		VALUES ('m-episodic', 'm-semantic', 1, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("a well-formed summary source was refused: %v", err)
	}
	// The primary key is the pair, so the same source cannot be listed twice for one
	// summary — which would make the summary's text depend on how often it was read.
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_summary_sources
		(summary_id, source_memory_id, source_order, created_at)
		VALUES ('m-episodic', 'm-semantic', 2, '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a duplicated summary source was accepted")
	}
	// Order is a position, so zero is refused.
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_summary_sources
		(summary_id, source_memory_id, source_order, created_at)
		VALUES ('m-summary', 'm-semantic', 0, '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a summary source at position zero was accepted")
	}

	// The entity link's two identifiers are bounded and its relation is a vocabulary the
	// reader can switch on.
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_entity_links
		(memory_id, entity_type, entity_id, relation_type, created_at)
		VALUES ('m-episodic', '', 'c1', 'about', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("an entity link with no entity type was accepted")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_entity_links
		(memory_id, entity_type, entity_id, relation_type, created_at)
		VALUES ('m-episodic', 'character', 'c1', 'about', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("a well-formed entity link was refused: %v", err)
	}

	// The review issue's source mark: the two documented values, and a default for a row
	// written without one — which is what keeps every pre-migration finding meaning what
	// it meant.
	if queryInt(t, db, "SELECT COUNT(*) FROM pragma_table_info('review_issues') WHERE name='source' AND dflt_value='''llm'''") != 1 {
		t.Fatal("review_issues.source does not default to 'llm'")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO review_issues
		(id, review_report_id, rule, severity, source, created_at)
		VALUES ('i1', 'no-report', 'r', 'minor', 'deterministic', '2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("a review issue citing a report that does not exist was accepted")
	}
}

// TestWP10MemoryDeleteCascades proves a deleted memory leaves no dangling links.
//
// The relations are citations rather than content, so a link whose memory is gone is a
// row that would make a reader resolve an identifier to nothing. The foreign keys are what
// make that impossible rather than a sweeper's job.
func TestWP10MemoryDeleteCascades(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, id := range []string{"m1", "m2"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO memory_items
			(id, scope_key, scope_project, memory_type, role, content, importance, confidence,
			 summarized, locked, source_type, source_id, deleted_at, created_at, updated_at, revision)
			VALUES (?, 'local||p1|||', 'p1', 'episodic', 'user', 'text', 0.5, 0.5, 0, 0, 'message', '', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_summary_sources
		(summary_id, source_memory_id, source_order, created_at)
		VALUES ('m1', 'm2', 1, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_entity_links
		(memory_id, entity_type, entity_id, relation_type, created_at)
		VALUES ('m2', 'character', 'c1', 'about', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM memory_items WHERE id = 'm2'`); err != nil {
		t.Fatal(err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM memory_summary_sources"); got != 0 {
		t.Fatalf("%d summary sources survived their memory", got)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM memory_entity_links"); got != 0 {
		t.Fatalf("%d entity links survived their memory", got)
	}
	foreignKeysClean(t, db)
}
