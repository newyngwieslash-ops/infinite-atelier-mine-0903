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
		{"000007_story_graph.sql", "story_entities", "entity_type", goValues(story.EntityTypes)},
		{"000007_story_graph.sql", "story_entities", "status", goValues(story.FactStatuses)},
		{"000007_story_graph.sql", "story_events", "status", goValues(story.FactStatuses)},
		{"000007_story_graph.sql", "story_events", "source_scope", goValues(story.SourceScopes)},
		{"000007_story_graph.sql", "story_event_participants", "role", goValues(story.ParticipantRoles)},
		{"000007_story_graph.sql", "story_relations", "relation_type", goValues(story.RelationTypes)},
		{"000007_story_graph.sql", "story_fact_sources", "fact_type", goValues(story.FactTypes)},
		{"000007_story_graph.sql", "story_fact_sources", "source_kind", goValues(story.SourceKinds)},
		{"000007_story_graph.sql", "story_fact_conflicts", "status", goValues(story.ConflictStatuses)},
		{"000007_story_graph.sql", "chapters", "status", goValues(story.ChapterStatuses)},

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
			migration := migrationScript(t, testCase.file)
			sqlValues := checkListFor(t, migration, testCase.table, testCase.column)
			compareVocabularies(t, label, sqlValues, testCase.goVocab)
		})
	}
}

// TestWP05AssetVersionOwnerChecksMatchTheSchema checks the one foreign-key-shaped
// vocabulary the registry depends on: canvas_nodes.entity_type must accept every
// kind the relation registry can name, or a validated edge could not be stored.
func TestWP05AssetVersionOwnerChecksMatchTheSchema(t *testing.T) {
	migration := migrationScript(t, "000004_projects_canvas.sql")
	// canvas_nodes.entity_type is an open default-empty column, so assert the
	// weaker property the schema actually guarantees: it exists and is a text
	// column with an empty default, which is what lets a free node store no
	// reference at all.
	body := tableBody(t, migration, "canvas_nodes")
	if !strings.Contains(body, "entity_type TEXT NOT NULL DEFAULT ''") {
		t.Fatalf("canvas_nodes.entity_type is no longer an open text column, so the reference vocabulary is not what the domain expects:\n%s", body)
	}
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
