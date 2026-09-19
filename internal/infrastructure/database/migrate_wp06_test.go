package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// TestWP06MigrationPreservesTheRebuiltStoryTables upgrades a database that
// already holds WP-05-shaped story rows and asserts every one survives.
//
// The rebuild is the risky part of 000014: story_entities is dropped and
// recreated, and three tables reference it with ON DELETE CASCADE. A wrong drop
// order would take those children's rows with it, and the loss would be silent.
// So the fixture writes a row in every affected table first, and the assertions
// name the values rather than only the counts.
func TestWP06MigrationPreservesTheRebuiltStoryTables(t *testing.T) {
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp05UpTo(t, 13), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close(ctx)
	db := handle.SQL()
	seedWP05Parents(t, db)
	seedWP06StoryRows(t, db)

	if err := applyMigrations(ctx, db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, wp05HeadVersion)

	counts := map[string]int{
		"story_entities":           2,
		"story_entity_aliases":     2,
		"story_event_participants": 1,
		"character_states":         1,
	}
	for table, want := range counts {
		if got := queryInt(t, db, "SELECT COUNT(*) FROM "+table); got != want {
			t.Fatalf("%s holds %d rows after the upgrade, want %d", table, got, want)
		}
	}

	// The identities and the values a reference elsewhere would name.
	for _, row := range []struct{ query, want string }{
		{"SELECT canonical_name FROM story_entities WHERE id = 'entity-1'", "Mira"},
		{"SELECT canonical_name FROM story_entities WHERE id = 'entity-2'", "The Harbour"},
		{"SELECT entity_type FROM story_entities WHERE id = 'entity-2'", "location"},
		{"SELECT status FROM story_entities WHERE id = 'entity-1'", "accepted"},
		{"SELECT source_scope FROM story_entities WHERE id = 'entity-2'", "original"},
		{"SELECT alias FROM story_entity_aliases WHERE id = 'alias-1'", "Mira the elder"},
		{"SELECT story_entity_id FROM story_entity_aliases WHERE id = 'alias-1'", "entity-1"},
		{"SELECT role FROM story_event_participants WHERE story_entity_id = 'entity-1'", "actor"},
		{"SELECT appearance_json FROM character_states WHERE id = 'state-1'", `{"hair":"dark"}`},
	} {
		if got := queryText(t, db, row.query); got != row.want {
			t.Fatalf("%q returned %q, want %q", row.query, got, row.want)
		}
	}

	// The cascade is intact: deleting the entity removes its children. Without
	// this the rebuild could have dropped the foreign keys and left orphans
	// behind while every count above still matched.
	if _, err := db.ExecContext(ctx, "DELETE FROM story_entities WHERE id = 'entity-1'"); err != nil {
		t.Fatal(err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM story_entity_aliases WHERE story_entity_id = 'entity-1'"); got != 0 {
		t.Fatalf("deleting an entity left %d aliases behind", got)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM story_event_participants WHERE story_entity_id = 'entity-1'"); got != 0 {
		t.Fatalf("deleting an entity left %d participants behind", got)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM character_states WHERE character_entity_id = 'entity-1'"); got != 0 {
		t.Fatalf("deleting an entity left %d character states behind", got)
	}
	// The other entity is untouched, which proves the cascade was scoped.
	if got := queryInt(t, db, "SELECT COUNT(*) FROM story_entities WHERE id = 'entity-2'"); got != 1 {
		t.Fatal("the cascade removed an unrelated entity")
	}
	foreignKeysClean(t, db)
}

// TestWP06EntityTypeVocabularyIsWidened proves the two new kinds are storable
// and that the ones PRD FR-030 lists but the model does not have stay refused.
func TestWP06EntityTypeVocabularyIsWidened(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	seedWP05Parents(t, db)
	ctx := context.Background()

	// Every documented kind, including the two this migration adds.
	for index, kind := range []string{
		"character", "location", "organization", "prop", "concept", "time",
		"relationship", "timeline_marker",
	} {
		statement := `INSERT INTO story_entities (id, project_id, entity_type, canonical_name, created_at, updated_at)
			VALUES (?, 'project-1', ?, 'n', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`
		if _, err := db.ExecContext(ctx, statement, "entity-"+kind, kind); err != nil {
			t.Fatalf("documented entity type %q was rejected at index %d: %v", kind, index, err)
		}
	}
	// A value PRD FR-030 names as an entity but the model does not carry must be
	// refused rather than silently stored: the ADR records the mapping, and a
	// row that contradicts it would make the record wrong.
	for _, rejected := range []string{"prop_state", "PropState", "Character", "event"} {
		statement := `INSERT INTO story_entities (id, project_id, entity_type, canonical_name, created_at, updated_at)
			VALUES (?, 'project-1', ?, 'n', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`
		if _, err := db.ExecContext(ctx, statement, "bad-"+rejected, rejected); err == nil {
			t.Fatalf("undocumented entity type %q was accepted", rejected)
		}
	}
}

// TestWP06ImportColumnsAreStorable covers the two added columns and their
// defaults, including that an existing row gets a value the CHECK accepts.
func TestWP06ImportColumnsAreStorable(t *testing.T) {
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp05UpTo(t, 13), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close(ctx)
	db := handle.SQL()
	seedWP05Parents(t, db)
	// The chapter needs a document and a version to point at, so the parent
	// chain is written here rather than assumed from the shared fixture.
	for _, statement := range []string{
		`INSERT INTO source_documents (id, project_id, document_type, name, status, created_at, updated_at, revision)
		 VALUES ('doc-import', 'project-1', 'novel', 'Import', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO source_document_versions (id, source_document_id, version_number, normalized_text_file_id, content_hash, char_count, created_by_type, created_at)
		 VALUES ('sdv-import', 'doc-import', 1, '` + testHashA + `', '` + testHashB + `', 10, 'user', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	// A chapter written before the column existed.
	if _, err := db.ExecContext(ctx, `INSERT INTO chapters
		(id, source_document_version_id, ordinal, title, start_offset, end_offset, content_hash, status, created_at, updated_at, revision)
		VALUES ('chapter-old', 'sdv-import', 1, '第一章', 0, 10, '', 'detected', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}

	if err := applyMigrations(ctx, db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	// The default is a value the CHECK accepts, so the upgrade does not fail on
	// an existing row.
	if got := queryText(t, db, "SELECT source_kind FROM chapters WHERE id = 'chapter-old'"); got != "regex" {
		t.Fatalf("an existing chapter got source_kind %q, want regex", got)
	}
	// Each documented kind is storable.
	for _, kind := range []string{"heading", "regex", "whole", "manual"} {
		if _, err := db.ExecContext(ctx, `UPDATE chapters SET source_kind = ? WHERE id = 'chapter-old'`, kind); err != nil {
			t.Fatalf("chapter source %q was rejected: %v", kind, err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE chapters SET source_kind = 'guessed' WHERE id = 'chapter-old'`); err == nil {
		t.Fatal("an undocumented chapter source was accepted")
	}

	// source_hash accepts a digest and defaults to empty for a pasted document,
	// which has no original file.
	if _, err := db.ExecContext(ctx, `INSERT INTO source_document_versions
		(id, source_document_id, version_number, normalized_text_file_id, content_hash, char_count, created_by_type, created_at)
		VALUES ('sdv-hashed', 'doc-import', 2, ?, ?, 10, 'user', '2026-01-01T00:00:00Z')`,
		testHashA, testHashB); err != nil {
		t.Fatalf("a version with a source hash was rejected: %v", err)
	}
	if got := queryText(t, db, "SELECT source_hash FROM source_document_versions WHERE id = 'sdv-hashed'"); got != "" {
		t.Fatalf("source_hash defaulted to %q, want empty", got)
	}
	if _, err := db.ExecContext(ctx, `UPDATE source_document_versions SET source_hash = ? WHERE id = 'sdv-hashed'`, testHashC); err != nil {
		t.Fatal(err)
	}
	if got := queryText(t, db, "SELECT source_hash FROM source_document_versions WHERE id = 'sdv-hashed'"); got != testHashC {
		t.Fatalf("source_hash = %q, want %q", got, testHashC)
	}
}

// TestWP06MigrationIsIdempotent proves a second run changes nothing, which is
// what the checksum guard should produce.
func TestWP06MigrationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close(ctx)
	db := handle.SQL()
	seedWP05Parents(t, db)
	seedWP06StoryRows(t, db)

	before := queryInt(t, db, "SELECT COUNT(*) FROM story_entities")
	if err := applyMigrations(ctx, db, wp05Migrations(t)); err != nil {
		t.Fatalf("re-applying the same set must be a no-op: %v", err)
	}
	if after := queryInt(t, db, "SELECT COUNT(*) FROM story_entities"); after != before {
		t.Fatalf("a second run changed story_entities from %d to %d rows", before, after)
	}
	assertUserVersion(t, db, wp05HeadVersion)
}

// seedWP06StoryRows writes one row in each table 000014 rebuilds, so the upgrade
// has something real to preserve.
//
// The rows are written before the migration runs, which is the only way to test
// that a rebuild preserves data rather than only that it produces the right
// schema.
func seedWP06StoryRows(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO source_documents (id, project_id, document_type, name, current_version_id, status, created_at, updated_at, revision)
		 VALUES ('doc-1', 'project-1', 'novel', 'The Long Night', '', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO source_document_versions (id, source_document_id, version_number, normalized_text_file_id, content_hash, char_count, created_by_type, created_at)
		 VALUES ('sdv-1', 'doc-1', 1, '` + testHashA + `', '` + testHashB + `', 12, 'user', '2026-01-01T00:00:00Z')`,
		`INSERT INTO chapters (id, source_document_version_id, ordinal, title, start_offset, end_offset, content_hash, status, created_at, updated_at, revision)
		 VALUES ('chapter-1', 'sdv-1', 1, '第一章', 0, 6, '', 'detected', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO story_entities (id, project_id, entity_type, canonical_name, status, source_scope, created_at, updated_at, revision)
		 VALUES ('entity-1', 'project-1', 'character', 'Mira', 'accepted', 'original', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO story_entities (id, project_id, entity_type, canonical_name, status, source_scope, created_at, updated_at, revision)
		 VALUES ('entity-2', 'project-1', 'location', 'The Harbour', 'candidate', 'original', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO story_entity_aliases (id, story_entity_id, alias, source_chapter_id, created_at)
		 VALUES ('alias-1', 'entity-1', 'Mira the elder', 'chapter-1', '2026-01-01T00:00:00Z')`,
		`INSERT INTO story_entity_aliases (id, story_entity_id, alias, created_at)
		 VALUES ('alias-2', 'entity-2', 'the port', '2026-01-01T00:00:00Z')`,
		`INSERT INTO story_events (id, project_id, chapter_id, ordinal, name, status, source_scope, created_at, updated_at, revision)
		 VALUES ('event-1', 'project-1', 'chapter-1', 1, 'An arrival', 'candidate', 'original', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO story_event_participants (story_event_id, story_entity_id, role, created_at)
		 VALUES ('event-1', 'entity-1', 'actor', '2026-01-01T00:00:00Z')`,
		`INSERT INTO character_states (id, character_entity_id, from_event_order, appearance_json, status, created_at, updated_at, revision)
		 VALUES ('state-1', 'entity-1', 1, '{"hair":"dark"}', 'candidate', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the WP-06 fixture failed: %v\n%s", err, statement)
		}
	}
}

// wp05UpTo returns the migration set through a given version, so a test can
// build a database at an earlier version and then apply the rest. It exists
// because the upgrade path is what has to be tested, not only the fresh one.
func wp05UpTo(t *testing.T, version int) fstest.MapFS {
	t.Helper()
	full := wp05Migrations(t)
	trimmed := fstest.MapFS{}
	for name, file := range full {
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Fatalf("migration %s has no number", name)
		}
		number, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatalf("migration %s has an unreadable number: %v", name, err)
		}
		if number <= version {
			trimmed[name] = file
		}
	}
	if len(trimmed) != version {
		t.Fatalf("asked for %d migrations but selected %d", version, len(trimmed))
	}
	return trimmed
}
