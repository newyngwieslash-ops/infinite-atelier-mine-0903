package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

func migrationSQL(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func wp05SQL(t *testing.T, name string) []byte {
	t.Helper()
	return migrationSQL(t, name)
}

// wp05Migrations is the full migration set, at the head `wp05HeadVersion` states.
func wp05Migrations(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"000001_foundation.sql":                  {Data: foundationSQL(t)},
		"000002_provider_security.sql":           {Data: providerSecuritySQL(t)},
		"000003_jobs.sql":                        {Data: jobsSQL(t)},
		"000004_projects_canvas.sql":             {Data: projectsCanvasSQL(t)},
		"000005_legacy_project_fingerprints.sql": {Data: legacyProjectFingerprintsSQL(t)},
		"000006_project_settings_and_rules.sql":  {Data: wp05SQL(t, "000006_project_settings_and_rules.sql")},
		"000007_story_graph.sql":                 {Data: wp05SQL(t, "000007_story_graph.sql")},
		"000008_script.sql":                      {Data: wp05SQL(t, "000008_script.sql")},
		"000009_asset_aggregate_v2.sql":          {Data: wp05SQL(t, "000009_asset_aggregate_v2.sql")},
		"000010_storyboard.sql":                  {Data: wp05SQL(t, "000010_storyboard.sql")},
		"000011_workflow_review.sql":             {Data: wp05SQL(t, "000011_workflow_review.sql")},
		"000012_artifact_staleness.sql":          {Data: wp05SQL(t, "000012_artifact_staleness.sql")},
		"000013_domain_events.sql":               {Data: wp05SQL(t, "000013_domain_events.sql")},
		"000014_story_import.sql":                {Data: wp05SQL(t, "000014_story_import.sql")},
		"000015_agent_runtime.sql":               {Data: wp05SQL(t, "000015_agent_runtime.sql")},
		"000016_agent_response_model.sql":        {Data: wp05SQL(t, "000016_agent_response_model.sql")},
		"000017_script_field_locks.sql":          {Data: wp05SQL(t, "000017_script_field_locks.sql")},
		"000018_production.sql":                  {Data: wp05SQL(t, "000018_production.sql")},
		"000019_memory_and_consistency.sql":      {Data: wp05SQL(t, "000019_memory_and_consistency.sql")},
		"000020_media.sql":                       {Data: wp05SQL(t, "000020_media.sql")},
	}
}

// wp05HeadVersion is the user_version the shared migration set reaches.
//
// The helper keeps its wp05 name because every test calls it by that name and
// the set is the same set; only its head moves as migrations are added. It is 19
// since WP-11 added the subtitle tracks and the episode export record.
const wp05HeadVersion = 20

// applyMigrationFileSplits runs one migration file the way the runner does:
// splitSQL on the raw text, then execute each fragment in order. It returns the
// failing fragment so a caller can name it.
//
// Executing is the check that matters. A semicolon inside a comment is not a
// syntax error in the file as written, so nothing about the text looks wrong:
// it only becomes a defect once the runner has split it, because the fragment
// after the split begins mid-comment and no longer parses. A balance-the-
// parentheses census does not catch that (it was tried), so the test applies
// the statements to a real database instead.
func applyMigrationFileSplits(ctx context.Context, db *sql.DB, script string) (string, error) {
	statements := splitSQL(script)
	if len(statements) == 0 {
		return "", nil
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return statement, err
		}
	}
	return "", nil
}

// TestWP05SplitSQLCompatibility proves every WP-05 migration survives the
// runner's splitting by applying it. Each file is applied on top of the full
// WP-04 state and its predecessors, so a fragment that only fails in context is
// still caught.
func TestWP05SplitSQLCompatibility(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(t)
	if err := applyMigrations(ctx, db, wp04Migrations(t)); err != nil {
		t.Fatal(err)
	}
	files := []string{
		"000006_project_settings_and_rules.sql",
		"000007_story_graph.sql",
		"000008_script.sql",
		"000009_asset_aggregate_v2.sql",
		"000010_storyboard.sql",
		"000011_workflow_review.sql",
		"000012_artifact_staleness.sql",
		"000013_domain_events.sql",
		"000014_story_import.sql",
	}
	for _, name := range files {
		script := string(migrationSQL(t, name))
		if len(splitSQL(script)) < 3 {
			t.Fatalf("%s split into too few statements, which means the splitter ate it", name)
		}
		if statement, err := applyMigrationFileSplits(ctx, db, script); err != nil {
			t.Fatalf("%s: a fragment failed to execute: %v\n---\n%s\n---", name, err, statement)
		}
	}
}

// TestWP05SplitSQLCompatibilityDetectsStraySemicolon is the negative half: it
// proves the check above can actually fail. Without this, a green
// TestWP05SplitSQLCompatibility would not distinguish "the migrations are fine"
// from "the check stopped looking".
func TestWP05SplitSQLCompatibilityDetectsStraySemicolon(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(t)
	// This is the exact shape that broke 000008 during development: the header
	// comment ends a clause in the middle of a LINE with a real semicolon, so
	// the fragment after the split starts with the prose word that followed it
	// rather than with a comment marker or a statement keyword. A semicolon at
	// the end of a comment line would not reproduce it, because the next
	// fragment would still begin with "--" and parse.
	broken := "-- Field names follow the specification, and this header clause ends here;" + "\n" +
		" this line is prose that continues the sentence and is not a comment at all" + "\n" +
		"CREATE TABLE wp05_negative_probe (id TEXT);\n"
	if _, err := applyMigrationFileSplits(ctx, db, broken); err == nil {
		t.Fatal("a stray semicolon inside a comment must be reported, not silently applied")
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='wp05_negative_probe'") != 0 {
		t.Fatal("the probe table must not exist, so the failure really prevented the statement")
	}
}

// TestWP05MigrationFreshDatabase covers the tables the scope items and the
// section 18 index list depend on being present.
func TestWP05MigrationFreshDatabase(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, wp05HeadVersion)

	tables := []string{
		// 000006
		"project_settings", "project_rules", "project_style_guides", "project_provider_policies",
		// 000007
		"source_documents", "source_document_versions", "chapters", "story_entities",
		"story_entity_aliases", "story_events", "story_event_participants", "story_relations",
		"story_fact_sources", "story_fact_conflicts", "character_states",
		// 000008
		"episodes", "story_skeleton_versions", "story_skeleton_event_links",
		"adaptation_strategy_versions", "adaptation_strategy_event_links",
		"scripts", "script_versions", "scenes", "dialogue_lines", "shots",
		// 000009
		"assets", "asset_aliases", "asset_versions", "asset_files", "asset_relations", "asset_usages",
		// 000010
		"director_plan_versions", "storyboards", "storyboard_versions", "storyboard_items", "storyboard_panel_versions",
		// 000011
		"workflow_runs", "stage_runs", "review_reports", "review_issues", "user_gate_decisions", "workflow_events",
		// 000012
		"artifact_staleness",
		// 000013
		"domain_events",
	}
	for _, table := range tables {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 1 {
			t.Fatalf("table %s missing", table)
		}
	}

	// The staging tables the asset rebuild uses must not survive it.
	for _, table := range []string{"_wp05_assets_stage", "_wp05_asset_versions_stage", "_wp05_asset_files_stage"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='"+table+"'") != 0 {
			t.Fatalf("staging table %s was left behind", table)
		}
	}

	foreignKeysClean(t, db)
}

// TestWP05MigrationCreatesSection18Indexes pins the index list DOMAIN_MODEL
// section 18 requires "at least". A missing index is a silent performance
// regression, so it fails here rather than in a benchmark.
func TestWP05MigrationCreatesSection18Indexes(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	indexes := []string{
		"idx_chapters_version_ordinal",
		"idx_story_entities_project_type_name",
		"idx_story_events_project_chapter_ordinal",
		"idx_story_relations_lookup",
		"idx_episodes_project",
		"idx_scenes_script_version_ordinal",
		"idx_shots_scene_ordinal",
		"idx_assets_project_type_status",
		"idx_asset_versions_asset",
		"idx_asset_usages_consumer",
		"idx_workflow_runs_project_status_updated",
		"idx_stage_runs_run_stage_attempt",
		"idx_review_reports_stage_run",
	}
	for _, index := range indexes {
		if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='"+index+"'") != 1 {
			t.Fatalf("index %s missing", index)
		}
	}
}

// TestWP05MigrationPreservesExistingRows upgrades a database that already holds
// the data WP-04 could write and asserts every row survives. This is the
// AC-LEGACY-003 concern ("迁移失败不破坏旧数据") applied to a schema upgrade.
func TestWP05MigrationPreservesExistingRows(t *testing.T) {
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp04Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close(ctx)
	db := handle.SQL()
	seedWP04AssetData(t, db)

	if err := applyMigrations(ctx, db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, wp05HeadVersion)

	for table, want := range map[string]int{"assets": 2, "asset_versions": 3, "asset_files": 3} {
		if got := queryInt(t, db, "SELECT COUNT(*) FROM "+table); got != want {
			t.Fatalf("%s holds %d rows after upgrade, want %d", table, got, want)
		}
	}
	// The identities are what a reference elsewhere would name, so they must be
	// the same values the pre-upgrade database held.
	for _, row := range []struct{ table, column, value string }{
		{"assets", "id", "asset-1"},
		{"assets", "name", "Mira"},
		{"asset_versions", "id", "version-1"},
		{"asset_versions", "status", "approved"},
		{"asset_versions", "prompt", "a portrait"},
	} {
		if got := queryText(t, db, "SELECT "+row.column+" FROM "+row.table+" WHERE "+row.column+" = ?", row.value); got != row.value {
			t.Fatalf("%s.%s lost its value %q (read back %q)", row.table, row.column, row.value, got)
		}
	}
	foreignKeysClean(t, db)
}

// TestWP05AssetStatusVocabularyIsMigrated proves the one value the old schema
// spelled differently is rewritten. 'review' does not exist in the documented
// set, and leaving it behind would make the row unreadable by the new checks.
func TestWP05AssetStatusVocabularyIsMigrated(t *testing.T) {
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp04Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close(ctx)
	db := handle.SQL()
	seedWP04AssetData(t, db)

	if err := applyMigrations(ctx, db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM asset_versions WHERE status = 'review'"); got != 0 {
		t.Fatalf("the pre-upgrade 'review' status survived (%d rows)", got)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM asset_versions WHERE status = 'under_review'"); got != 1 {
		t.Fatalf("expected exactly one row mapped to under_review, got %d", got)
	}
	// Every status the fixture seeds must be readable by its documented name,
	// and the one that used to be spelled differently must be gone.
	for _, status := range []string{"draft", "under_review", "approved"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM asset_versions WHERE status = '"+status+"'") == 0 {
			t.Fatalf("status %s has no row, so the vocabulary is not exercised", status)
		}
	}
	// 'superseded' is written after the upgrade rather than seeded before it, so
	// this also proves the new set is writable, not merely readable.
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO asset_versions (id, asset_id, version_number, status, created_by_type, created_at)
		 VALUES ('version-4', 'asset-1', 3, 'superseded', 'user', '2026-01-04T00:00:00Z')`); err != nil {
		t.Fatalf("the documented status superseded was rejected after the upgrade: %v", err)
	}
}

// TestWP05AssetBackfillMintsValidIdentifiers checks the identifiers the
// backfill mints for asset_files. The old table had no id column, so the
// migration generates one in SQL; this asserts the generated values satisfy the
// application's own parser rather than merely looking plausible.
func TestWP05AssetBackfillMintsValidIdentifiers(t *testing.T) {
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp04Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close(ctx)
	db := handle.SQL()
	seedWP04AssetData(t, db)

	if err := applyMigrations(ctx, db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, created_at FROM asset_files`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var value, createdAt string
		if err := rows.Scan(&value, &createdAt); err != nil {
			t.Fatal(err)
		}
		seen++
		if !id.Valid(value) {
			t.Fatalf("the backfill minted %q, which is not a valid UUIDv7", value)
		}
		want, ok := id.Timestamp(value)
		if !ok {
			t.Fatalf("the backfill minted %q with no readable timestamp", value)
		}
		parsed, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			t.Fatalf("the fixture timestamp %q is not parseable: %v", createdAt, err)
		}
		// The timestamp half must come from the row, not from the migration
		// clock, so a migrated link stays ordered among its neighbours.
		if !want.Equal(parsed.Truncate(time.Millisecond)) {
			t.Fatalf("asset_file %s carries %s but its row says %s", value, want.Format(time.RFC3339Nano), parsed.Format(time.RFC3339Nano))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen == 0 {
		t.Fatal("no asset_files rows were migrated, so the check proved nothing")
	}
}

// TestWP05MigrationsRollBackOnFailure proves a migration that fails partway
// leaves no schema behind. The second run applies a deliberately broken file.
func TestWP05MigrationsRollBackOnFailure(t *testing.T) {
	ctx := context.Background()
	db := openTempDB(t)
	if err := applyMigrations(ctx, db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	// A migration set that adds one broken file on top must leave the previous
	// version as the head. The number is deliberately far above the real head so
	// it cannot collide with a migration added later.
	broken := wp05Migrations(t)
	broken["000099_broken.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE should_not_exist (id TEXT);\nCREATE TABLE oops (id TEXT REFERENCES nothing(id));\n")}
	if err := applyMigrations(ctx, db, broken); err == nil {
		t.Fatal("a broken migration must be reported")
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='should_not_exist'") != 0 {
		t.Fatal("the failed migration left a table behind, so it was not atomic")
	}
	assertUserVersion(t, db, wp05HeadVersion)
}

// TestWP05ConstraintsEnforceDocumentedValues checks the closed vocabularies the
// specification defines. Each insert names a value that must be accepted and
// one that must be rejected, so a check that stops being enforced fails here.
func TestWP05ConstraintsEnforceDocumentedValues(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	seedWP05Parents(t, db)

	cases := []struct {
		name    string
		accept  string
		reject  string
		acceptQ string
		rejectQ string
	}{
		{
			name:    "project_rules.category",
			accept:  "safety",
			reject:  "nonsense",
			acceptQ: `INSERT INTO project_rules (id, project_id, category, name, created_at, updated_at) VALUES ('rule-ok', 'project-1', 'safety', 'r', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO project_rules (id, project_id, category, name, created_at, updated_at) VALUES ('rule-bad', 'project-1', 'nonsense', 'r', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		},
		{
			name:    "project_rules.strength",
			accept:  "immutable",
			reject:  "hard",
			acceptQ: `INSERT INTO project_rules (id, project_id, category, name, strength, created_at, updated_at) VALUES ('rule-ok2', 'project-1', 'story', 'r', 'immutable', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO project_rules (id, project_id, category, name, strength, created_at, updated_at) VALUES ('rule-bad2', 'project-1', 'story', 'r', 'hard', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		},
		{
			name:    "assets.asset_type accepts the FR-050 production types",
			accept:  "vehicle",
			reject:  "spaceship",
			acceptQ: `INSERT INTO assets (id, project_id, asset_type, name, created_at, updated_at) VALUES ('asset-ok', 'project-1', 'vehicle', 'a', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO assets (id, project_id, asset_type, name, created_at, updated_at) VALUES ('asset-bad', 'project-1', 'spaceship', 'a', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		},
		{
			name:    "asset_versions.status uses the section 2.5 set",
			accept:  "candidate",
			reject:  "review",
			acceptQ: `INSERT INTO asset_versions (id, asset_id, version_number, status, created_at) VALUES ('v-ok', 'asset-ok', 1, 'candidate', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO asset_versions (id, asset_id, version_number, status, created_at) VALUES ('v-bad', 'asset-ok', 2, 'review', '2026-01-01T00:00:00Z')`,
		},
		{
			name:    "asset_files.role accepts the section 8.4 roles",
			accept:  "first_frame",
			reject:  "attachment",
			acceptQ: `INSERT INTO asset_files (id, asset_version_id, file_hash, role, created_at) VALUES ('af-ok', 'v-ok', '` + testHashA + `', 'first_frame', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO asset_files (id, asset_version_id, file_hash, role, created_at) VALUES ('af-bad', 'v-ok', '` + testHashA + `', 'attachment', '2026-01-01T00:00:00Z')`,
		},
		{
			name:    "stage_runs.status takes the PRD FR-100 spelling",
			accept:  "execution_succeeded",
			reject:  "executed",
			acceptQ: `INSERT INTO stage_runs (id, workflow_run_id, stage, attempt, status, created_at) VALUES ('sr-ok', 'run-1', 'story_skeleton', 1, 'execution_succeeded', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO stage_runs (id, workflow_run_id, stage, attempt, status, created_at) VALUES ('sr-bad', 'run-1', 'story_skeleton', 2, 'executed', '2026-01-01T00:00:00Z')`,
		},
		{
			name:    "user_gate_decisions.decision takes the section 12.2 set",
			accept:  "waive",
			reject:  "pass",
			acceptQ: `INSERT INTO user_gate_decisions (id, workflow_run_id, decision, created_at) VALUES ('ug-ok', 'run-1', 'waive', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO user_gate_decisions (id, workflow_run_id, decision, created_at) VALUES ('ug-bad', 'run-1', 'pass', '2026-01-01T00:00:00Z')`,
		},
		{
			name:    "artifact_staleness.severity",
			accept:  "review_required",
			reject:  "critical",
			acceptQ: `INSERT INTO artifact_staleness (artifact_type, artifact_id, project_id, severity, created_at, updated_at) VALUES ('script_version', 'sv-1', 'project-1', 'review_required', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO artifact_staleness (artifact_type, artifact_id, project_id, severity, created_at, updated_at) VALUES ('script_version', 'sv-2', 'project-1', 'critical', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		},
		{
			name:    "story_events.status",
			accept:  "locked",
			reject:  "confirmed",
			acceptQ: `INSERT INTO story_events (id, project_id, name, status, created_at, updated_at) VALUES ('se-ok', 'project-1', 'e', 'locked', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO story_events (id, project_id, name, status, created_at, updated_at) VALUES ('se-bad', 'project-1', 'e', 'confirmed', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		},
		{
			name:    "scenes.interior_exterior",
			accept:  "INT_EXT",
			reject:  "OUTSIDE",
			acceptQ: `INSERT INTO scenes (id, script_version_id, ordinal, interior_exterior, created_at, updated_at) VALUES ('sc-ok', 'script-version-1', 1, 'INT_EXT', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			rejectQ: `INSERT INTO scenes (id, script_version_id, ordinal, interior_exterior, created_at, updated_at) VALUES ('sc-bad', 'script-version-1', 2, 'OUTSIDE', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := db.ExecContext(context.Background(), testCase.acceptQ); err != nil {
				t.Fatalf("the documented value %q was rejected: %v", testCase.accept, err)
			}
			if _, err := db.ExecContext(context.Background(), testCase.rejectQ); err == nil {
				t.Fatalf("the undocumented value %q was accepted", testCase.reject)
			}
		})
	}
}

// TestWP05ApprovedVersionIsUnique proves the partial unique indexes do what the
// acceptance item "approved 唯一" claims, for ALL EIGHT version families
// DOMAIN_MODEL §2.5 defines. Covering three of them would let a future migration
// drop the UNIQUE on the other five and ship silently, which is what an earlier
// revision of this test allowed: mutating those five indexes to plain indexes
// left the suite green.
func TestWP05ApprovedVersionIsUnique(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	seedWP05Parents(t, db)
	// The storyboard families need parents the shared fixture does not carry, and
	// they are seeded here rather than there so no other test's fixture changes.
	// Order matters: a storyboard version names a director plan.
	seedApprovedUniquenessParents(t, db)
	ctx := context.Background()

	families := []struct {
		name string
		// index is the partial unique index this family's rule lives in, asserted
		// directly so a missing index is reported as such rather than as a
		// puzzling insert failure.
		index      string
		insertOne  string
		insertTwo  string
		insertFree string
	}{
		{
			name:  "asset_versions",
			index: "idx_asset_versions_approved",
			insertOne: `INSERT INTO asset_versions (id, asset_id, version_number, status, created_at)
				VALUES ('av-1', 'asset-1', 1, 'approved', '2026-01-01T00:00:00Z')`,
			insertTwo: `INSERT INTO asset_versions (id, asset_id, version_number, status, created_at)
				VALUES ('av-2', 'asset-1', 2, 'approved', '2026-01-01T00:00:00Z')`,
			insertFree: `INSERT INTO asset_versions (id, asset_id, version_number, status, created_at)
				VALUES ('av-3', 'asset-1', 3, 'rejected', '2026-01-01T00:00:00Z')`,
		},
		{
			name:  "script_versions",
			index: "idx_script_versions_approved",
			// The parent fixture already holds script_version 1, so this family
			// starts at 2. Version numbers are unique per script.
			insertOne: `INSERT INTO script_versions (id, script_id, version_number, status, created_at)
				VALUES ('sv-2', 'script-1', 2, 'approved', '2026-01-01T00:00:00Z')`,
			insertTwo: `INSERT INTO script_versions (id, script_id, version_number, status, created_at)
				VALUES ('sv-3', 'script-1', 3, 'approved', '2026-01-01T00:00:00Z')`,
			insertFree: `INSERT INTO script_versions (id, script_id, version_number, status, created_at)
				VALUES ('sv-4', 'script-1', 4, 'superseded', '2026-01-01T00:00:00Z')`,
		},
		{
			name:  "story_skeleton_versions",
			index: "idx_story_skeleton_versions_approved",
			insertOne: `INSERT INTO story_skeleton_versions (id, episode_id, version_number, status, created_at)
				VALUES ('ssv-1', 'episode-1', 1, 'approved', '2026-01-01T00:00:00Z')`,
			insertTwo: `INSERT INTO story_skeleton_versions (id, episode_id, version_number, status, created_at)
				VALUES ('ssv-2', 'episode-1', 2, 'approved', '2026-01-01T00:00:00Z')`,
			insertFree: `INSERT INTO story_skeleton_versions (id, episode_id, version_number, status, created_at)
				VALUES ('ssv-3', 'episode-1', 3, 'stale', '2026-01-01T00:00:00Z')`,
		},
		{
			name:  "adaptation_strategy_versions",
			index: "idx_adaptation_strategy_versions_approved",
			insertOne: `INSERT INTO adaptation_strategy_versions (id, episode_id, version_number, status, created_at)
				VALUES ('asv-1', 'episode-1', 1, 'approved', '2026-01-01T00:00:00Z')`,
			insertTwo: `INSERT INTO adaptation_strategy_versions (id, episode_id, version_number, status, created_at)
				VALUES ('asv-2', 'episode-1', 2, 'approved', '2026-01-01T00:00:00Z')`,
			insertFree: `INSERT INTO adaptation_strategy_versions (id, episode_id, version_number, status, created_at)
				VALUES ('asv-3', 'episode-1', 3, 'candidate', '2026-01-01T00:00:00Z')`,
		},
		{
			name:  "project_style_guides",
			index: "idx_project_style_guides_approved",
			insertOne: `INSERT INTO project_style_guides (id, project_id, version_number, status, created_at)
				VALUES ('psg-1', 'project-1', 1, 'approved', '2026-01-01T00:00:00Z')`,
			insertTwo: `INSERT INTO project_style_guides (id, project_id, version_number, status, created_at)
				VALUES ('psg-2', 'project-1', 2, 'approved', '2026-01-01T00:00:00Z')`,
			insertFree: `INSERT INTO project_style_guides (id, project_id, version_number, status, created_at)
				VALUES ('psg-3', 'project-1', 3, 'deprecated', '2026-01-01T00:00:00Z')`,
		},
		{
			name:  "director_plan_versions",
			index: "idx_director_plan_versions_approved",
			insertOne: `INSERT INTO director_plan_versions (id, episode_id, version_number, status, script_version_id, created_at)
				VALUES ('dpv-1', 'episode-1', 1, 'approved', 'au-script-version', '2026-01-01T00:00:00Z')`,
			insertTwo: `INSERT INTO director_plan_versions (id, episode_id, version_number, status, script_version_id, created_at)
				VALUES ('dpv-2', 'episode-1', 2, 'approved', 'au-script-version', '2026-01-01T00:00:00Z')`,
			insertFree: `INSERT INTO director_plan_versions (id, episode_id, version_number, status, script_version_id, created_at)
				VALUES ('dpv-3', 'episode-1', 3, 'under_review', 'au-script-version', '2026-01-01T00:00:00Z')`,
		},
		{
			name:  "storyboard_versions",
			index: "idx_storyboard_versions_approved",
			insertOne: `INSERT INTO storyboard_versions (id, storyboard_id, version_number, status, script_version_id, director_plan_version_id, created_at)
				VALUES ('sbv-2', 'au-storyboard', 2, 'approved', 'au-script-version', 'au-director-plan', '2026-01-01T00:00:00Z')`,
			insertTwo: `INSERT INTO storyboard_versions (id, storyboard_id, version_number, status, script_version_id, director_plan_version_id, created_at)
				VALUES ('sbv-3', 'au-storyboard', 3, 'approved', 'au-script-version', 'au-director-plan', '2026-01-01T00:00:00Z')`,
			insertFree: `INSERT INTO storyboard_versions (id, storyboard_id, version_number, status, script_version_id, director_plan_version_id, created_at)
				VALUES ('sbv-4', 'au-storyboard', 4, 'stale', 'au-script-version', 'au-director-plan', '2026-01-01T00:00:00Z')`,
		},
		{
			name:  "storyboard_panel_versions",
			index: "idx_storyboard_panel_versions_approved",
			insertOne: `INSERT INTO storyboard_panel_versions (id, storyboard_item_id, version_number, status, created_at)
				VALUES ('spv-1', 'au-storyboard-item', 1, 'approved', '2026-01-01T00:00:00Z')`,
			insertTwo: `INSERT INTO storyboard_panel_versions (id, storyboard_item_id, version_number, status, created_at)
				VALUES ('spv-2', 'au-storyboard-item', 2, 'approved', '2026-01-01T00:00:00Z')`,
			insertFree: `INSERT INTO storyboard_panel_versions (id, storyboard_item_id, version_number, status, created_at)
				VALUES ('spv-3', 'au-storyboard-item', 3, 'candidate', '2026-01-01T00:00:00Z')`,
		},
	}
	// Every family §2.5 defines must appear here. The count is asserted so a
	// family added to the vocabulary cannot be left untested by omission.
	if len(families) != 8 {
		t.Fatalf("the approved-uniqueness table covers %d families, but §2.5 defines 8", len(families))
	}
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			// The index must exist and must be UNIQUE. Checking the SQL directly
			// means a missing or weakened index is reported as exactly that,
			// rather than as a confusing insert behaviour.
			var definition string
			if err := db.QueryRowContext(ctx,
				`SELECT sql FROM sqlite_master WHERE type='index' AND name=?`, family.index).Scan(&definition); err != nil {
				t.Fatalf("the approved-uniqueness index %s does not exist: %v", family.index, err)
			}
			if !strings.Contains(strings.ToUpper(definition), "UNIQUE") {
				t.Fatalf("%s is not a UNIQUE index: %s", family.index, definition)
			}
			if !strings.Contains(definition, "WHERE status = 'approved'") {
				t.Fatalf("%s is not partial on the approved status: %s", family.index, definition)
			}

			if _, err := db.ExecContext(ctx, family.insertOne); err != nil {
				t.Fatalf("the first approved version was rejected: %v", err)
			}
			if _, err := db.ExecContext(ctx, family.insertTwo); err == nil {
				t.Fatal("a second approved version of the same parent was accepted")
			}
			if _, err := db.ExecContext(ctx, family.insertFree); err != nil {
				t.Fatalf("a non-approved status must remain storable: %v", err)
			}
		})
	}
}

// TestWP05AssetFileBackfillIsIdempotentAcrossUpgradePatterns covers the second
// import of a database that was already upgraded: applying the same set again
// must be a no-op rather than rewriting the asset rows a second time.
func TestWP05ApplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp04Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close(ctx)
	db := handle.SQL()
	seedWP04AssetData(t, db)

	if err := applyMigrations(ctx, db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	before := queryInt(t, db, "SELECT COUNT(*) FROM asset_files")
	if err := applyMigrations(ctx, db, wp05Migrations(t)); err != nil {
		t.Fatalf("re-applying the same set must be a no-op: %v", err)
	}
	if after := queryInt(t, db, "SELECT COUNT(*) FROM asset_files"); after != before {
		t.Fatalf("a second run changed asset_files from %d to %d rows", before, after)
	}
	assertUserVersion(t, db, wp05HeadVersion)
}

// seedWP04AssetData writes the rows WP-04 could store, including the one status
// the new vocabulary renames, so the upgrade has something real to preserve.
func seedWP04AssetData(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO workspaces (id, name, kind, created_at, updated_at, revision)
		 VALUES ('ws-1', 'Local', 'local', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO projects (id, workspace_id, project_type, name, language, status, created_at, updated_at, revision)
		 VALUES ('project-1', 'ws-1', 'free_canvas', 'Legacy', 'zh-CN', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
		 VALUES ('` + testHashA + `', '` + testHashA + `', 'image/png', 10, '2026-01-01T00:00:00Z')`,
		`INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
		 VALUES ('` + testHashB + `', '` + testHashB + `', 'image/png', 20, '2026-01-01T00:00:00Z')`,
		`INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
		 VALUES ('` + testHashC + `', '` + testHashC + `', 'image/png', 30, '2026-01-01T00:00:00Z')`,
		`INSERT INTO assets (id, project_id, asset_type, name, description, status, created_at, updated_at, revision)
		 VALUES ('asset-1', 'project-1', 'character', 'Mira', 'the lead', 'active', '2026-01-02T03:04:05Z', '2026-01-02T03:04:05Z', 1)`,
		`INSERT INTO assets (id, project_id, asset_type, name, status, created_at, updated_at, revision)
		 VALUES ('asset-2', 'project-1', 'prop', 'Lantern', 'active', '2026-01-03T00:00:00Z', '2026-01-03T00:00:00Z', 1)`,
		`INSERT INTO asset_versions (id, asset_id, version_number, status, prompt, created_by_type, created_at)
		 VALUES ('version-1', 'asset-1', 1, 'approved', 'a portrait', 'migration', '2026-01-02T03:04:05Z')`,
		`INSERT INTO asset_versions (id, asset_id, version_number, status, created_by_type, created_at)
		 VALUES ('version-2', 'asset-1', 2, 'review', 'migration', '2026-01-02T04:00:00Z')`,
		`INSERT INTO asset_versions (id, asset_id, version_number, status, created_by_type, created_at)
		 VALUES ('version-3', 'asset-2', 1, 'draft', 'migration', '2026-01-03T00:00:00Z')`,
		`INSERT INTO asset_files (asset_version_id, file_hash, role, created_at)
		 VALUES ('version-1', '` + testHashA + `', 'primary', '2026-01-02T03:04:05Z')`,
		`INSERT INTO asset_files (asset_version_id, file_hash, role, created_at)
		 VALUES ('version-1', '` + testHashB + `', 'thumbnail', '2026-01-02T03:04:06Z')`,
		`INSERT INTO asset_files (asset_version_id, file_hash, role, created_at)
		 VALUES ('version-3', '` + testHashC + `', 'source', '2026-01-03T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the WP-04 fixture failed: %v\n%s", err, statement)
		}
	}
}

// seedWP05Parents writes the rows the constraint tests hang rows off.
func seedWP05Parents(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
		 VALUES ('` + testHashA + `', '` + testHashA + `', 'image/png', 10, '2026-01-01T00:00:00Z')`,
		`INSERT INTO workspaces (id, name, kind, created_at, updated_at, revision)
		 VALUES ('ws-1', 'Local', 'local', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO projects (id, workspace_id, project_type, name, language, status, created_at, updated_at, revision)
		 VALUES ('project-1', 'ws-1', 'drama', 'Drama', 'zh-CN', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO assets (id, project_id, asset_type, name, created_at, updated_at)
		 VALUES ('asset-1', 'project-1', 'character', 'Mira', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO episodes (id, project_id, season_number, episode_number, title, created_at, updated_at)
		 VALUES ('episode-1', 'project-1', 1, 1, 'Pilot', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO scripts (id, episode_id, created_at, updated_at)
		 VALUES ('script-1', 'episode-1', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO script_versions (id, script_id, version_number, status, created_at)
		 VALUES ('script-version-1', 'script-1', 1, 'draft', '2026-01-01T00:00:00Z')`,
		`INSERT INTO workflow_runs (id, project_id, workflow_type, status, created_at, updated_at)
		 VALUES ('run-1', 'project-1', 'episode_production', 'pending', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the WP-05 fixture failed: %v\n%s", err, statement)
		}
	}
}

// seedApprovedUniquenessParents writes the rows the storyboard version families
// hang off, which the shared fixture deliberately does not carry.
//
// They use their own ids rather than the shared 'scene-1'/'shot-1' names,
// because the scene and shot tables have a UNIQUE (parent, ordinal) constraint
// and seeding a second scene at ordinal 1 of the same script version would
// collide with what another test writes.
func seedApprovedUniquenessParents(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO script_versions (id, script_id, version_number, status, created_at)
		 VALUES ('au-script-version', 'script-1', 90, 'draft', '2026-01-01T00:00:00Z')`,
		`INSERT INTO director_plan_versions (id, episode_id, version_number, status, script_version_id, created_at)
		 VALUES ('au-director-plan', 'episode-1', 90, 'draft', 'au-script-version', '2026-01-01T00:00:00Z')`,
		`INSERT INTO storyboards (id, episode_id, created_at, updated_at)
		 VALUES ('au-storyboard', 'episode-1', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO scenes (id, script_version_id, ordinal, created_at, updated_at)
		 VALUES ('au-scene', 'au-script-version', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO shots (id, scene_id, ordinal, created_at, updated_at)
		 VALUES ('au-shot', 'au-scene', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO storyboard_versions (id, storyboard_id, version_number, status, script_version_id, director_plan_version_id, created_at)
		 VALUES ('au-storyboard-version', 'au-storyboard', 1, 'draft', 'au-script-version', 'au-director-plan', '2026-01-01T00:00:00Z')`,
		`INSERT INTO storyboard_items (id, storyboard_version_id, shot_id, ordinal, created_at, updated_at)
		 VALUES ('au-storyboard-item', 'au-storyboard-version', 'au-shot', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the approved-uniqueness parents failed: %v\n%s", err, statement)
		}
	}
}

// testHashA and friends are content hashes shaped like a SHA-256 digest.
const (
	testHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testHashC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// queryText reads one text column, returning "" when no row matches.
func queryText(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var value string
	err := db.QueryRowContext(context.Background(), query, args...).Scan(&value)
	if err != nil {
		return ""
	}
	return value
}

// foreignKeysClean fails the test when the database holds a dangling reference.
func foreignKeysClean(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		var rowid int64
		var parent string
		var fkID int64
		if err := rows.Scan(&table, &rowid, &parent, &fkID); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("dangling foreign key: %s row %d references %s", table, rowid, parent)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
