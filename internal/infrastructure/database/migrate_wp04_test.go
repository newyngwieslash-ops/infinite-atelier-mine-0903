package database

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func projectsCanvasSQL(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("migrations", "000004_projects_canvas.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// wp04Migrations is the full migration set through version 4.
func wp04Migrations(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"000001_foundation.sql":        {Data: foundationSQL(t)},
		"000002_provider_security.sql": {Data: providerSecuritySQL(t)},
		"000003_jobs.sql":              {Data: jobsSQL(t)},
		"000004_projects_canvas.sql":   {Data: projectsCanvasSQL(t)},
	}
}

// TestWP04MigrationFreshDatabase covers the tables and indexes the acceptance
// items depend on being present.
func TestWP04MigrationFreshDatabase(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp04Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, 4)

	tables := []string{
		"workspaces", "projects", "canvas_documents", "canvas_nodes", "canvas_edges",
		"canvas_chat_sessions", "assets", "asset_versions", "asset_files",
		"generation_history", "legacy_imports", "legacy_id_map",
	}
	for _, table := range tables {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 1 {
			t.Fatalf("table %s missing", table)
		}
	}
	// DOMAIN_MODEL §18 indexes for this package's entities.
	indexes := []string{
		"idx_projects_workspace_status", "idx_canvas_documents_project",
		"idx_canvas_nodes_document", "idx_canvas_nodes_entity", "idx_canvas_edges_document",
		"idx_canvas_chat_sessions_document", "idx_assets_project_type_status",
		"idx_asset_versions_asset", "idx_asset_files_hash",
		"idx_generation_history_project", "idx_legacy_imports_fingerprint",
		"idx_legacy_id_map_lookup",
	}
	for _, index := range indexes {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='"+index+"'") != 1 {
			t.Fatalf("index %s missing", index)
		}
	}

	// ADR-0005: file_objects stays the physical file table and no second one is
	// created for the same fact.
	if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='physical_files'") != 0 {
		t.Fatal("physical_files exists; ADR-0005 keeps file_objects as the single physical file table")
	}

	// ADR-0006 §8 / DOMAIN_MODEL §8: the default local workspace always exists
	// and cannot be deleted.
	if queryInt(t, db, "SELECT COUNT(*) FROM workspaces WHERE kind='local'") != 1 {
		t.Fatal("the default local workspace was not seeded exactly once")
	}
}

// TestWP04MigrationPreservesExistingRows is the upgrade test: a v3 database
// already holds provider configs, job rows and file metadata, and none of it
// may be disturbed by adding the new tables.
func TestWP04MigrationPreservesExistingRows(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")
	snapshotDir := filepath.Join(root, "snapshots")
	nowFn := func() time.Time { return time.Date(2026, 9, 16, 1, 2, 3, 0, time.UTC) }

	handle, err := open(context.Background(), dbPath, snapshotDir, wp03Migrations(t), nowFn)
	if err != nil {
		t.Fatal(err)
	}
	if handle.Mode() != ModeReady {
		t.Fatalf("v3 database not ready: %v", handle.Err())
	}
	// A job row, a file object and its reference: the state that must survive.
	seed := []string{
		`INSERT INTO generation_jobs (id, project_id, entity_type, entity_id, job_type, status, priority,
			idempotency_key, input_json, created_at, updated_at, revision)
		 VALUES ('job-legacy-1', 'legacy-project-1', 'canvas_node', 'node-1', 'image_generation', 'succeeded',
			0, 'k-1', '{}', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`,
		`INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
		 VALUES ('` + strings.Repeat("a", 64) + `', '` + strings.Repeat("a", 64) + `', 'image/png', 12, '2026-09-16T00:00:00Z')`,
		`INSERT INTO file_references (owner_type, owner_id, file_hash, created_at)
		 VALUES ('job', 'job-legacy-1', '` + strings.Repeat("a", 64) + `', '2026-09-16T00:00:00Z')`,
	}
	for _, statement := range seed {
		if _, err := handle.SQL().ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("seeding %q: %v", statement, err)
		}
	}
	if err := handle.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	upgraded, err := open(context.Background(), dbPath, snapshotDir, wp04Migrations(t), nowFn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = upgraded.Close(context.Background()) }()
	if upgraded.Mode() != ModeReady {
		t.Fatalf("upgraded database not ready: %v", upgraded.Err())
	}

	// The pre-migration snapshot exists (PRD NFR-002: 任何迁移先备份).
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no pre-migration snapshot was taken")
	}

	assertUserVersion(t, upgraded.SQL(), 4)
	if got := queryInt(t, upgraded.SQL(), "SELECT COUNT(*) FROM generation_jobs WHERE id='job-legacy-1'"); got != 1 {
		t.Fatalf("existing job row lost in upgrade (count=%d)", got)
	}
	if got := queryInt(t, upgraded.SQL(), "SELECT COUNT(*) FROM file_objects"); got != 1 {
		t.Fatalf("existing file object lost in upgrade (count=%d)", got)
	}
	if got := queryInt(t, upgraded.SQL(), "SELECT COUNT(*) FROM file_references WHERE owner_type='job'"); got != 1 {
		t.Fatalf("existing file reference lost in upgrade (count=%d)", got)
	}
	// The project id on the job row is a scope value, not a foreign key
	// (ADR-0005 §4): the row that references a project which does not exist must
	// still be insertable and readable.
	if got := queryInt(t, upgraded.SQL(), "SELECT COUNT(*) FROM generation_jobs WHERE project_id='legacy-project-1'"); got != 1 {
		t.Fatal("the job's project scope value was rewritten")
	}
}

// TestWP04ConstraintsEnforceDocumentedValues enumerates the CHECK values this
// migration introduces, accepting the documented set and rejecting anything
// else.
func TestWP04ConstraintsEnforceDocumentedValues(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp04Migrations(t)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const workspace = "'00000000-0000-7000-8000-000000000001'"
	const project = "'p-1'"
	const document = "'d-1'"
	const node = "'n-1'"
	const node2 = "'n-2'"
	const imported = "'imp-1'"

	insert := func(t *testing.T, label, statement string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	rejects := func(t *testing.T, label, statement string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("%s: statement was accepted but should be rejected", label)
		}
	}

	insert(t, "workspace", `INSERT INTO workspaces (id, name, kind, created_at, updated_at, revision)
		VALUES ('ws-2', 'Second', 'local', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	// project_type: free_canvas | drama
	for _, validType := range []string{"free_canvas", "drama"} {
		insert(t, "project type "+validType, `INSERT INTO projects (id, workspace_id, project_type, name, language, status, created_at, updated_at, revision)
			VALUES ('p-'||'`+validType+`', `+workspace+`, '`+validType+`', 'Name', 'zh-CN', 'active', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	}
	rejects(t, "project type unknown", `INSERT INTO projects (id, workspace_id, project_type, name, created_at, updated_at, revision)
		VALUES ('p-bad', `+workspace+`, 'not_a_type', 'Name', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	// status: active | archived | trashed
	for _, status := range []string{"active", "archived", "trashed"} {
		insert(t, "project status "+status, `INSERT INTO projects (id, workspace_id, project_type, name, status, created_at, updated_at, revision)
			VALUES ('p-st-'||'`+status+`', `+workspace+`, 'free_canvas', 'Name', '`+status+`', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	}
	rejects(t, "project status unknown", `INSERT INTO projects (id, workspace_id, project_type, name, status, created_at, updated_at, revision)
		VALUES ('p-st-bad', `+workspace+`, 'free_canvas', 'Name', 'deleted', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	// A project must belong to an existing workspace.
	rejects(t, "project without workspace", `INSERT INTO projects (id, workspace_id, project_type, name, created_at, updated_at, revision)
		VALUES ('p-orphan', 'missing-ws', 'free_canvas', 'Name', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)

	insert(t, "project", `INSERT INTO projects (id, workspace_id, project_type, name, language, status, created_at, updated_at, revision)
		VALUES (`+project+`, `+workspace+`, 'free_canvas', 'P', 'zh-CN', 'active', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)

	// canvas_kind: free | drama | episode | storyboard | asset
	for _, kind := range []string{"free", "drama", "episode", "storyboard", "asset"} {
		insert(t, "canvas kind "+kind, `INSERT INTO canvas_documents (id, project_id, canvas_kind, created_at, updated_at, revision)
			VALUES ('d-'||'`+kind+`', `+project+`, '`+kind+`', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	}
	rejects(t, "canvas kind unknown", `INSERT INTO canvas_documents (id, project_id, canvas_kind, created_at, updated_at, revision)
		VALUES ('d-bad', `+project+`, 'timeline', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)

	insert(t, "document", `INSERT INTO canvas_documents (id, project_id, canvas_kind, created_at, updated_at, revision)
		VALUES (`+document+`, `+project+`, 'free', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)

	insert(t, "standalone node", `INSERT INTO canvas_nodes (id, canvas_document_id, node_type, title, created_at, updated_at, revision)
		VALUES (`+node+`, `+document+`, 'text', 'T', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	insert(t, "projection node", `INSERT INTO canvas_nodes (id, canvas_document_id, node_type, entity_type, entity_id, entity_version_id, created_at, updated_at, revision)
		VALUES (`+node2+`, `+document+`, 'image', 'storyboard_panel', 'panel-1', 'v-1', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	// DOMAIN_MODEL §10.2: entity_type and entity_id exist together.
	rejects(t, "half projection", `INSERT INTO canvas_nodes (id, canvas_document_id, node_type, entity_type, created_at, updated_at, revision)
		VALUES ('n-half', `+document+`, 'image', 'storyboard_panel', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)

	// relation_type: the registry of DOMAIN_MODEL §10.4 plus generic.
	relations := []string{
		"generic", "contains", "adapts_to", "references", "derived_from", "continues_from",
		"generated_by", "reviewed_by", "supersedes", "first_frame_of", "last_frame_of",
		"uses_character", "uses_location", "uses_prop", "uses_asset", "appears_in",
		"located_in", "causes", "precedes", "contradicts",
	}
	for index, relation := range relations {
		insert(t, "relation "+relation, `INSERT INTO canvas_edges (id, canvas_document_id, from_node_id, to_node_id, relation_type, created_at, updated_at, revision)
			VALUES ('e-`+strconv.Itoa(index)+`', `+document+`, `+node+`, `+node2+`, '`+relation+`', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	}
	rejects(t, "relation unknown", `INSERT INTO canvas_edges (id, canvas_document_id, from_node_id, to_node_id, relation_type, created_at, updated_at, revision)
		VALUES ('e-bad', `+document+`, `+node+`, `+node2+`, 'points_at', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	// validation_status: valid | invalid | stale | unknown
	for _, status := range []string{"valid", "invalid", "stale", "unknown"} {
		insert(t, "edge validation "+status, `INSERT INTO canvas_edges (id, canvas_document_id, from_node_id, to_node_id, validation_status, created_at, updated_at, revision)
			VALUES ('ev-'||'`+status+`', `+document+`, `+node+`, `+node2+`, '`+status+`', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	}
	rejects(t, "edge validation unknown", `INSERT INTO canvas_edges (id, canvas_document_id, from_node_id, to_node_id, validation_status, created_at, updated_at, revision)
		VALUES ('ev-bad', `+document+`, `+node+`, `+node2+`, 'maybe', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)

	// asset_type: the nine documented kinds.
	assetTypes := []string{"character", "location", "prop", "costume", "style", "image", "video", "audio", "doc"}
	for index, assetType := range assetTypes {
		insert(t, "asset type "+assetType, `INSERT INTO assets (id, project_id, asset_type, name, created_at, updated_at, revision)
			VALUES ('a-`+strconv.Itoa(index)+`', `+project+`, '`+assetType+`', 'A', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	}
	rejects(t, "asset type unknown", `INSERT INTO assets (id, project_id, asset_type, name, created_at, updated_at, revision)
		VALUES ('a-bad', `+project+`, 'sound', 'A', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)

	insert(t, "asset", `INSERT INTO assets (id, project_id, asset_type, name, created_at, updated_at, revision)
		VALUES ('asset-1', `+project+`, 'image', 'A', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1)`)
	insert(t, "asset version", `INSERT INTO asset_versions (id, asset_id, version_number, created_at)
		VALUES ('av-1', 'asset-1', 1, '2026-09-16T00:00:00Z')`)
	// UNIQUE (asset_id, version_number).
	rejects(t, "duplicate version number", `INSERT INTO asset_versions (id, asset_id, version_number, created_at)
		VALUES ('av-2', 'asset-1', 1, '2026-09-16T00:00:00Z')`)
	// version_number starts at 1.
	rejects(t, "zero version number", `INSERT INTO asset_versions (id, asset_id, version_number, created_at)
		VALUES ('av-0', 'asset-1', 0, '2026-09-16T00:00:00Z')`)

	// asset_files references a committed object; the FK makes uncommitted bytes
	// impossible to reference (ADR-0006).
	rejects(t, "asset file without object", `INSERT INTO asset_files (asset_version_id, file_hash, role, created_at)
		VALUES ('av-1', '`+strings.Repeat("b", 64)+`', 'primary', '2026-09-16T00:00:00Z')`)
	insert(t, "file object", `INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
		VALUES ('`+strings.Repeat("a", 64)+`', '`+strings.Repeat("a", 64)+`', 'image/png', 12, '2026-09-16T00:00:00Z')`)
	insert(t, "asset file", `INSERT INTO asset_files (asset_version_id, file_hash, role, created_at)
		VALUES ('av-1', '`+strings.Repeat("a", 64)+`', 'primary', '2026-09-16T00:00:00Z')`)

	// import status and mode vocabularies.
	for _, status := range []string{"completed", "failed", "already_imported"} {
		insert(t, "import status "+status, `INSERT INTO legacy_imports (id, source_fingerprint, mode, status, started_at, created_at)
			VALUES ('imp-'||'`+status+`', '`+strings.Repeat("c", 64)+`', 'initial', '`+status+`', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z')`)
	}
	rejects(t, "import status unknown", `INSERT INTO legacy_imports (id, source_fingerprint, mode, status, started_at, created_at)
		VALUES ('imp-bad', '`+strings.Repeat("c", 64)+`', 'initial', 'partial', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z')`)
	// The fingerprint length is pinned: it is a SHA-256 hex digest.
	rejects(t, "short fingerprint", `INSERT INTO legacy_imports (id, source_fingerprint, mode, status, started_at, created_at)
		VALUES ('imp-short', 'abc', 'initial', 'completed', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z')`)

	insert(t, "import", `INSERT INTO legacy_imports (id, source_fingerprint, mode, status, started_at, created_at)
		VALUES (`+imported+`, '`+strings.Repeat("d", 64)+`', 'initial', 'completed', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z')`)
	insert(t, "id map", `INSERT INTO legacy_id_map (import_id, kind, legacy_id, new_id, created_at)
		VALUES (`+imported+`, 'project', 'legacy-1', 'new-1', '2026-09-16T00:00:00Z')`)
	rejects(t, "id map duplicate", `INSERT INTO legacy_id_map (import_id, kind, legacy_id, new_id, created_at)
		VALUES (`+imported+`, 'project', 'legacy-1', 'new-2', '2026-09-16T00:00:00Z')`)
	rejects(t, "id map kind unknown", `INSERT INTO legacy_id_map (import_id, kind, legacy_id, new_id, created_at)
		VALUES (`+imported+`, 'chapter', 'legacy-9', 'new-9', '2026-09-16T00:00:00Z')`)
}

// TestWP04MigrationRollsBackOnFailure proves a failing migration leaves the
// database at the previous version with no partial tables.
func TestWP04MigrationRollsBackOnFailure(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp03Migrations(t)); err != nil {
		t.Fatal(err)
	}
	broken := wp04Migrations(t)
	data := string(projectsCanvasSQL(t))
	broken["000004_projects_canvas.sql"] = &fstest.MapFile{Data: []byte(data + "\nSELECT RAISE(ABORT, 'boom');")}
	if err := applyMigrations(context.Background(), db, broken); err == nil {
		t.Fatal("a failing migration reported success")
	}
	assertUserVersion(t, db, 3)
	if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='projects'") != 0 {
		t.Fatal("the projects table survived a rolled-back migration")
	}
}

// TestWP04SplitSQLCompatibility proves the migration survives the runner's
// naive statement splitting: a semicolon inside a literal or comment would
// silently truncate a statement.
func TestWP04SplitSQLCompatibility(t *testing.T) {
	statements := splitSQL(string(projectsCanvasSQL(t)))
	if len(statements) < 20 {
		t.Fatalf("split produced %d statements, which is fewer than the migration defines", len(statements))
	}
	for index, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}
		// Every statement must be a complete one; a truncated one shows up as an
		// unbalanced parenthesis.
		if strings.Count(trimmed, "(") != strings.Count(trimmed, ")") {
			t.Fatalf("statement %d has unbalanced parentheses, so a semicolon split it: %q", index, trimmed)
		}
	}
}
