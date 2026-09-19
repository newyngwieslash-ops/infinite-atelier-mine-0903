package database

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// This file guards against the failure mode a schema/domain split invites: a
// vocabulary that is right in Go and wrong in SQL, or the reverse. A value the
// Go layer happily accepts but the schema rejects fails at runtime, inside a
// user's database, which is the worst place to find out.
//
// The check is mechanical. It reads the migration files, parses each CREATE
// TABLE body, extracts the `CHECK (<column> IN (...))` list, and compares it
// against the Go list named below. Drift in either direction fails, and the
// failure names the table, the column, and the values that differ.
//
// Parsing is per table rather than per column name, because several tables share
// a column spelled `status` with different vocabularies: a name-only scan would
// compare assets.status against asset_versions.status and report a mismatch
// that does not exist.

// tableCheckPattern finds `CHECK ( <column> IN ( <values> ) )` inside a table
// body. Both spellings the migrations use are matched: a single-line check, and
// the wrapped form the wider vocabularies take:
//
//	status TEXT NOT NULL DEFAULT 'draft' CHECK (
//	    status IN ('draft', 'candidate', ...)
//	),
//
// Whitespace after `CHECK (` is allowed for that second form, and the value
// group runs across newlines up to the first closing parenthesis, which is the
// end of the IN list.
var tableCheckPattern = regexp.MustCompile(`(?s)CHECK \(\s*([a-z_]+)\s+IN \(([^)]*)\)\s*\)`)

// quotedValuePattern extracts the quoted strings from an IN list.
var quotedValuePattern = regexp.MustCompile(`'([^']*)'`)

// migrationScript reads one migration file.
func migrationScript(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// defineTablePattern matches a CREATE TABLE for a named table. It is anchored to
// a line start so `CREATE TABLE x_stage AS SELECT` cannot match a request for
// `x`, and it allows the name to be quoted.
var defineTablePattern = regexp.MustCompile(`(?m)^CREATE TABLE (?:IF NOT EXISTS )?` + "`?" + `%s` + "`?" + ` ?\(`)

// tableDefinitionFile returns the migration that last defines a table.
//
// A table can be redefined: migration 000014 rebuilds story_entities to widen its
// entity_type CHECK, so the newest definition is the one the database actually
// enforces and the one a vocabulary must agree with. Reading a fixed filename
// would compare Go against a definition that no longer exists — which is how the
// entity_type drift went unnoticed, with the guard green while the schema
// accepted two values Go refused.
//
// The search is over the sorted filenames, so "newest" means the last migration
// the runner applies rather than a hand-maintained list.
func tableDefinitionFile(t *testing.T, table string) string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join("migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no migrations were found, so no table can be resolved")
	}
	// The filenames carry a zero-padded ordinal, so lexical order is apply order.
	sort.Strings(names)
	pattern := regexp.MustCompile(fmt.Sprintf(defineTablePattern.String(), regexp.QuoteMeta(table)))
	found := ""
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if pattern.Match(data) {
			// Later files win, so keep scanning rather than stopping at the first.
			found = filepath.Base(name)
		}
	}
	if found == "" {
		t.Fatalf("no migration defines the table %s", table)
	}
	return found
}

// effectiveCheckList returns a vocabulary from the migration that currently
// defines the table, so a case never has to name a filename.
//
// It searches the CREATE TABLE definitions first. A column added later by ALTER
// TABLE is not in any body, so a second pass looks for the column's own CHECK
// anywhere in the newest file that mentions it — which is how chapters.source_kind
// is defined.
func effectiveCheckList(t *testing.T, table, column string) []string {
	t.Helper()
	file := tableDefinitionFile(t, table)
	if values, err := checkListValues(migrationScript(t, file), table, column); err == nil {
		return values
	}
	if file := columnAlterFile(t, table, column); file != "" {
		body := migrationScript(t, file)
		if values, err := columnCheckValues(body, column); err == nil {
			return values
		}
	}
	t.Fatalf("%s.%s has no closed vocabulary in %s or in any later migration", table, column, file)
	return nil
}

// columnAlterFile returns the newest migration that constrains a column with an
// inline CHECK outside a CREATE TABLE body, which is the ALTER TABLE ADD COLUMN
// form migration 000014 uses.
func columnAlterFile(t *testing.T, table, column string) string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join("migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	// The statement must name both the table and the column, so an unrelated
	// table's identically named column cannot resolve it.
	statement := regexp.MustCompile(`(?s)ALTER TABLE ` + regexp.QuoteMeta(table) + `[^;]*?` + regexp.QuoteMeta(column) + `[^;]*?CHECK`)
	found := ""
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if statement.Match(data) {
			found = filepath.Base(name)
		}
	}
	return found
}

// columnCheckValues extracts the values of a single `CHECK ( <column> IN (...) )`
// from migration text without needing a table body.
func columnCheckValues(migration, column string) ([]string, error) {
	pattern := regexp.MustCompile(`(?s)CHECK \(\s*` + regexp.QuoteMeta(column) + `\s+IN \(([^)]*)\)\s*\)`)
	match := pattern.FindStringSubmatch(migration)
	if match == nil {
		return nil, fmt.Errorf("no CHECK constrains the column %s in this migration", column)
	}
	var values []string
	for _, valueMatch := range quotedValuePattern.FindAllStringSubmatch(match[1], -1) {
		values = append(values, valueMatch[1])
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("the CHECK on %s has no quoted values", column)
	}
	return values, nil
}

// tableBodyText returns the text between a CREATE TABLE's opening parenthesis
// and its terminator, so a column's CHECK is read from the right table.
//
// It returns an error rather than failing the test so the parsing can be
// exercised directly by the guard test below; tableBody is the t.Fatal wrapper
// the comparisons use.
func tableBodyText(migration, table string) (string, error) {
	marker := "CREATE TABLE " + table + " ("
	start := strings.Index(migration, marker)
	if start < 0 {
		return "", fmt.Errorf("table %s is not defined in the migration", table)
	}
	rest := migration[start+len(marker):]
	end := strings.Index(rest, "\n);")
	if end < 0 {
		return "", fmt.Errorf("table %s has no terminator, so its body cannot be isolated", table)
	}
	return rest[:end], nil
}

// tableBody is tableBodyText with a test failure on error.
func tableBody(t *testing.T, migration, table string) string {
	t.Helper()
	body, err := tableBodyText(migration, table)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// checkListValues returns the allowed values of a closed vocabulary column, or
// an error when the table or column has none.
func checkListValues(migration, table, column string) ([]string, error) {
	body, err := tableBodyText(migration, table)
	if err != nil {
		return nil, err
	}
	for _, match := range tableCheckPattern.FindAllStringSubmatch(body, -1) {
		if match[1] != column {
			continue
		}
		var values []string
		for _, valueMatch := range quotedValuePattern.FindAllStringSubmatch(match[2], -1) {
			values = append(values, valueMatch[1])
		}
		if len(values) == 0 {
			return nil, fmt.Errorf("%s.%s has a CHECK with no quoted values", table, column)
		}
		return values, nil
	}
	return nil, fmt.Errorf("table %s has no closed vocabulary for column %s, which the parity check requires", table, column)
}

// checkListFor is checkListValues with a test failure on error.
func checkListFor(t *testing.T, migration, table, column string) []string {
	t.Helper()
	values, err := checkListValues(migration, table, column)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

// goValues renders a Go vocabulary list as strings for comparison.
func goValues[T ~string](values []T) []string {
	rendered := make([]string, 0, len(values))
	for _, value := range values {
		rendered = append(rendered, string(value))
	}
	return rendered
}

// vocabulariesAgree reports whether two lists describe the same set with no
// repetition on either side. It is the predicate compareVocabularies asserts, so
// the guard tests can exercise it directly: t.Fatalf cannot be recovered from
// (it calls runtime.Goexit), so a deferred recover would never see it.
func vocabulariesAgree(sqlValues, goVocab []string) (agree bool, sqlOnly, goOnly []string, countsDiffer bool) {
	sqlSet := map[string]bool{}
	for _, value := range sqlValues {
		sqlSet[value] = true
	}
	goSet := map[string]bool{}
	for _, value := range goVocab {
		goSet[value] = true
	}
	for value := range sqlSet {
		if !goSet[value] {
			sqlOnly = append(sqlOnly, value)
		}
	}
	for value := range goSet {
		if !sqlSet[value] {
			goOnly = append(goOnly, value)
		}
	}
	sort.Strings(sqlOnly)
	sort.Strings(goOnly)
	return len(sqlOnly) == 0 && len(goOnly) == 0 && len(sqlValues) == len(goVocab), sqlOnly, goOnly, len(sqlValues) != len(goVocab)
}

// compareVocabularies fails when the two sets differ, naming the difference.
func compareVocabularies(t *testing.T, label string, sqlValues, goVocab []string) {
	t.Helper()
	agree, sqlOnly, goOnly, countsDiffer := vocabulariesAgree(sqlValues, goVocab)
	if len(sqlOnly) > 0 || len(goOnly) > 0 {
		t.Fatalf("%s drifted: the schema accepts %v but Go refuses them, and Go accepts %v but the schema refuses them",
			label, sqlOnly, goOnly)
	}
	if !agree || countsDiffer {
		// Equal as sets but different in length means one side repeats a value,
		// which makes "the documented list" ambiguous.
		t.Fatalf("%s lists %d SQL values and %d Go values, so one side repeats an entry", label, len(sqlValues), len(goVocab))
	}
}

// TestWP05VocabulariesMatchTheSchema is the cross-check. Every case names a
// table, the column whose CHECK constrains it, and the Go list that must agree.
func TestWP05VocabulariesMatchTheSchema(t *testing.T) {
	cases := []struct {
		file    string
		table   string
		column  string
		goVocab []string
	}{
		{"000006_project_settings_and_rules.sql", "project_settings", "adaptation_mode", goValues(project.AdaptationModes)},
		{"000006_project_settings_and_rules.sql", "project_rules", "category", goValues(project.RuleCategories)},
		{"000006_project_settings_and_rules.sql", "project_rules", "strength", goValues(project.RuleStrengths)},
		{"000006_project_settings_and_rules.sql", "project_rules", "status", goValues([]project.RuleStatus{project.RuleActive, project.RuleArchived})},
		{"000006_project_settings_and_rules.sql", "project_rules", "source_type", goValues(project.RuleSourceTypes)},
		{"000006_project_settings_and_rules.sql", "project_style_guides", "status", goValues(versioning.Statuses)},
		{"000006_project_settings_and_rules.sql", "project_provider_policies", "layer", goValues(project.ProviderPolicyLayers)},

		{"000007_story_graph.sql", "source_documents", "document_type", goValues(story.DocumentTypes)},
		{"000007_story_graph.sql", "stage_runs_placeholder", "", nil},
		{"000007_story_graph.sql", "story_entities", "status", goValues(story.FactStatuses)},
		{"000007_story_graph.sql", "story_events", "status", goValues(story.FactStatuses)},
		{"000007_story_graph.sql", "story_events", "source_scope", goValues(story.SourceScopes)},
		{"000007_story_graph.sql", "story_relations", "relation_type", goValues(story.RelationTypes)},
		{"000007_story_graph.sql", "story_fact_sources", "fact_type", goValues(story.FactTypes)},
		{"000007_story_graph.sql", "story_fact_sources", "source_kind", goValues(story.SourceKinds)},
		{"000007_story_graph.sql", "story_fact_conflicts", "status", goValues(story.ConflictStatuses)},
		{"000007_story_graph.sql", "chapters", "status", goValues(story.ChapterStatuses)},

		// An empty file means "wherever this table or column is currently
		// defined", which is the only correct answer for the four tables
		// migration 000014 rebuilds and the one column it adds. Pinning these to
		// 000007 would compare Go against a superseded CHECK: that is how the
		// entity_type drift went unnoticed, with the guard green while the schema
		// accepted two values Go refused.
		{"", "story_entities", "entity_type", goValues(story.EntityTypes)},
		{"", "story_event_participants", "role", goValues(story.ParticipantRoles)},
		{"", "character_states", "status", goValues(story.FactStatuses)},
		{"", "chapters", "source_kind", goValues(story.ChapterSourceKinds)},

		{"000008_script.sql", "episodes", "status", goValues(script.EpisodeStatuses)},
		{"000008_script.sql", "scenes", "interior_exterior", goValues(script.InteriorExteriors)},
		{"000008_script.sql", "dialogue_lines", "line_type", goValues(script.LineTypes)},
		{"000008_script.sql", "story_skeleton_versions", "status", goValues(versioning.Statuses)},
		{"000008_script.sql", "adaptation_strategy_versions", "adaptation_mode", goValues(script.AdaptationModes)},
		{"000008_script.sql", "shots", "status", goValues(versioning.Statuses)},

		{"000009_asset_aggregate_v2.sql", "assets", "asset_type", goValues(asset.Types)},
		{"000009_asset_aggregate_v2.sql", "assets", "status", goValues([]asset.Status{asset.StatusActive, asset.StatusArchived, asset.StatusTrashed})},
		{"000009_asset_aggregate_v2.sql", "asset_versions", "status", goValues(versioning.Statuses)},
		{"000009_asset_aggregate_v2.sql", "asset_files", "role", goValues(asset.FileRoles)},
		{"000009_asset_aggregate_v2.sql", "asset_relations", "relation_type", goValues(asset.RelationTypes)},
		{"000009_asset_aggregate_v2.sql", "asset_usages", "consumer_type", goValues(asset.ConsumerTypes)},

		{"000010_storyboard.sql", "director_plan_versions", "status", goValues(versioning.Statuses)},
		{"000010_storyboard.sql", "storyboard_versions", "status", goValues(versioning.Statuses)},
		{"000010_storyboard.sql", "storyboard_items", "status", goValues(versioning.Statuses)},
		{"000010_storyboard.sql", "storyboard_panel_versions", "status", goValues(versioning.Statuses)},

		{"000011_workflow_review.sql", "workflow_runs", "status", goValues(workflow.RunStatuses)},
		{"000011_workflow_review.sql", "stage_runs", "status", goValues(workflow.StageStatuses)},
		{"000011_workflow_review.sql", "review_issues", "status", goValues(workflow.IssueStatuses)},
		{"000011_workflow_review.sql", "user_gate_decisions", "decision", goValues(workflow.GateDecisions)},
		{"000011_workflow_review.sql", "workflow_events", "actor_type", goValues(versioning.CreatedByTypes)},

		{"000012_artifact_staleness.sql", "artifact_staleness", "artifact_type", goValues(staleness.Chain)},
		{"000012_artifact_staleness.sql", "artifact_staleness", "severity", goValues(staleness.Severities)},
	}
	for _, testCase := range cases {
		if testCase.goVocab == nil {
			continue
		}
		label := testCase.table + "." + testCase.column
		t.Run(label, func(t *testing.T) {
			// An empty file means "wherever this table is currently defined",
			// which is the only correct answer for a table a later migration
			// rebuilt. A named file is still honoured for the cases that pin a
			// definition deliberately.
			if testCase.file == "" {
				compareVocabularies(t, label, effectiveCheckList(t, testCase.table, testCase.column), testCase.goVocab)
				return
			}
			migration := migrationScript(t, testCase.file)
			sqlValues := checkListFor(t, migration, testCase.table, testCase.column)
			compareVocabularies(t, label, sqlValues, testCase.goVocab)
		})
	}
}

// TestWP05CanvasNodeEntityColumnsStayOpen pins the one property the relation
// registry depends on from the canvas schema: canvas_nodes.entity_type must stay
// an open text column with an empty default, so a free node can store no
// reference while a projection stores one.
//
// It does NOT compare the column against project.EntityRefTypes, because the
// schema does not constrain it — an earlier version of this test was named as if
// it did. The relation vocabulary that does have a closed list is compared by
// TestWP04CanvasEdgeRelationVocabulary below.
func TestWP05CanvasNodeEntityColumnsStayOpen(t *testing.T) {
	migration := migrationScript(t, "000004_projects_canvas.sql")
	body := tableBody(t, migration, "canvas_nodes")
	if !strings.Contains(body, "entity_type TEXT NOT NULL DEFAULT ''") {
		t.Fatalf("canvas_nodes.entity_type is no longer an open text column, so the reference vocabulary is not what the domain expects:\n%s", body)
	}
	if !strings.Contains(body, "entity_id TEXT NOT NULL DEFAULT ''") {
		t.Fatalf("canvas_nodes.entity_id is no longer an open text column:\n%s", body)
	}
	// The pair invariant must still be in the schema: half a reference is what
	// the projection command refuses, and the CHECK is what makes that refusal a
	// statement about the database rather than a habit.
	if !strings.Contains(body, "entity_type = '' AND entity_id = ''") {
		t.Fatalf("the canvas node reference pair is no longer enforced together:\n%s", body)
	}
}

// TestWP04CanvasEdgeRelationVocabulary guards the one closed vocabulary the
// parity guard cannot reach: canvas_edges.relation_type lives in the WP-04
// migration, and project.RelationTypes is the Go list that must agree with it.
// Adding a relation to one side and not the other is exactly the drift the guard
// exists to prevent, and without this it would pass the whole suite.
func TestWP04CanvasEdgeRelationVocabulary(t *testing.T) {
	migration := migrationScript(t, "000004_projects_canvas.sql")
	compareVocabularies(t, "canvas_edges.relation_type",
		checkListFor(t, migration, "canvas_edges", "relation_type"),
		goValues(project.RelationTypes))
}

// TestWP05ExtractorActuallyFindsVocabularies guards the guard. If the parser
// stopped matching, every comparison above would pass on nothing.
func TestWP05ExtractorActuallyFindsVocabularies(t *testing.T) {
	migration := migrationScript(t, "000009_asset_aggregate_v2.sql")
	assetTypes := checkListFor(t, migration, "assets", "asset_type")
	if !containsString(assetTypes, "vehicle") {
		t.Fatalf("the asset_type vocabulary parsed as %v, which is not the migration's list", assetTypes)
	}
	// Per-table parsing is what keeps two `status` columns apart.
	assetStatus := checkListFor(t, migration, "assets", "status")
	versionStatus := checkListFor(t, migration, "asset_versions", "status")
	if containsString(assetStatus, "draft") {
		t.Fatal("assets.status parsed with the version vocabulary, so the table boundary is not enforced")
	}
	if !containsString(versionStatus, "under_review") {
		t.Fatalf("asset_versions.status parsed as %v, which is not the version vocabulary", versionStatus)
	}
	// A deliberate mismatch must be detected rather than tolerated.
	if agree, sqlOnly, _, _ := vocabulariesAgree([]string{"a", "b"}, []string{"a", "c"}); agree {
		t.Fatalf("a differing vocabulary compared as equal (schema-only %v)", sqlOnly)
	}
	// A duplicate must also be detected, so a list that repeats a value cannot
	// pass by comparing equal as a set.
	if agree, _, _, countsDiffer := vocabulariesAgree([]string{"a", "b"}, []string{"a", "a", "b"}); agree || !countsDiffer {
		t.Fatal("a repeated value on one side must be reported, not treated as agreement")
	}
	// The agreeing case must be recognised, or the guard would pass vacuously.
	if agree, _, _, _ := vocabulariesAgree([]string{"a", "b"}, []string{"b", "a"}); !agree {
		t.Fatal("the same values in a different order must compare as agreement")
	}
	// A missing table or column must be reported, not silently skipped.
	if _, err := tableBodyText(migration, "no_such_table"); err == nil {
		t.Fatal("asking for a table that does not exist must be an error, not an empty body")
	}
	if _, err := checkListValues(migration, "assets", "no_such_column"); err == nil {
		t.Fatal("asking for a column with no closed vocabulary must be an error")
	}
	// A column that exists but has no CHECK must also be reported, so a table
	// whose constraint was dropped cannot pass by returning an empty list.
	if _, err := checkListValues(migration, "asset_versions", "prompt"); err == nil {
		t.Fatal("a column with no CHECK must be reported, not treated as an empty vocabulary")
	}
}

// TestWP06TableVocabularyResolverPicksTheNewestDefinition guards the resolver the
// story cases above now depend on.
//
// Those cases pass an empty filename so they compare against whatever defines the
// table today. If the resolver quietly kept returning 000007, every one of them
// would pass while checking a superseded CHECK — the same blindness that let the
// entity_type drift through in the first place. So the resolution itself is
// asserted rather than assumed.
func TestWP06TableVocabularyResolverPicksTheNewestDefinition(t *testing.T) {
	// A rebuilt table resolves to the migration that rebuilt it.
	if got := tableDefinitionFile(t, "story_entities"); got != "000014_story_import.sql" {
		t.Fatalf("story_entities resolved to %s, want the migration that rebuilt it", got)
	}
	// The vocabulary found there must be the widened one. The two definitions
	// differ, so this is what proves the newest was read.
	values := effectiveCheckList(t, "story_entities", "entity_type")
	if !containsString(values, "timeline_marker") {
		t.Fatalf("story_entities.entity_type resolved as %v, which is the pre-000014 vocabulary", values)
	}
	// A table defined once still resolves to its own migration.
	if got := tableDefinitionFile(t, "story_relations"); got != "000007_story_graph.sql" {
		t.Fatalf("story_relations resolved to %s, want its defining migration", got)
	}
	// A column added by ALTER TABLE has no CREATE TABLE body to read, so it must
	// resolve through the column search instead.
	if added := effectiveCheckList(t, "chapters", "source_kind"); !containsString(added, "manual") {
		t.Fatalf("chapters.source_kind resolved as %v, want the added column's vocabulary", added)
	}
	// A staging table's name contains the real one, so the pattern must be
	// anchored to a whole name: otherwise a rebuilt table resolves to its own
	// copy, and the copy's foreign keys are not a definition of anything.
	pattern := regexp.MustCompile(fmt.Sprintf(defineTablePattern.String(), regexp.QuoteMeta("story_entities")))
	if pattern.MatchString("CREATE TABLE _wp06_story_entities_stage AS SELECT * FROM story_entities;") {
		t.Fatal("the definition pattern matched a staging table, so a rebuilt table could resolve to its own copy")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
