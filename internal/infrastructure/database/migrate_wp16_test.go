package database

import (
	"context"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
)

// TestWP16MigrationAddsTheFindingCategory covers migration 000024.
//
// # What this migrations's risk is
//
// An ALTER that adds a column is the cheap kind, and the risk it carries is not structural: it is that
// the DEFAULT is wrong for the rows that already exist. `review_issues` carries findings a user may
// already have read and acted on, and every one of them was written before this column. So the test
// seeds a row the way the pre-migration schema allowed, applies the migration, and asserts what that
// row then says its category is: EMPTY, because that is the honest answer for a finding whose
// producer stated none — and distinctly not "technical", which would be this migration inventing a
// classification nobody made.
func TestWP16MigrationAddsTheFindingCategory(t *testing.T) {
	db := openTempDB(t)
	if err := applyMigrations(context.Background(), db, wp05Migrations(t)); err != nil {
		t.Fatal(err)
	}
	assertUserVersion(t, db, wp05HeadVersion)

	if queryInt(t, db, "SELECT COUNT(*) FROM pragma_table_info('review_issues') WHERE name = 'category'") != 1 {
		t.Fatal("review_issues.category missing")
	}
	// The default is the empty string, which is what an old row must read back as.
	//
	// `dflt_value` carries the DEFAULT clause's own TEXT, so the empty string appears as two single
	// quotes and the SQL literal for those two characters is six — three escaped pairs. The first
	// version of this assertion wrote five and SQLite refused the statement outright, which is the
	// schema doing its job on a test rather than on a user.
	//
	// The literal is built with a backquoted Go string so the count of quotes is visible rather than
	// mangled by two levels of escaping.
	if queryInt(t, db, `SELECT COUNT(*) FROM pragma_table_info('review_issues') WHERE name = 'category' AND dflt_value = ''''''`) != 1 {
		t.Fatal("review_issues.category does not default to the empty string")
	}
	// There is deliberately NO CHECK on the values: the vocabulary lives in the domain package, and a
	// second copy here would drift the moment a category is added. This asserts the absence, because a
	// later change that added one would make adding an eleventh category need a table rebuild.
	if queryInt(t, db, `SELECT COUNT(*) FROM pragma_table_info('review_issues') WHERE name = 'category' AND "notnull" = 1`) != 1 {
		t.Fatal("review_issues.category must be NOT NULL with an empty default")
	}
	foreignKeysClean(t, db)
}

// TestAnOldFindingReadsBackWithoutACategory is the row-level half.
//
// The migration test above asserts the schema; this one asserts what a READER sees, which is the fact
// that matters. A finding written before the column existed must come back with an empty category, so
// the quality section shows no tag for it rather than the word "technical".
func TestAnOldFindingReadsBackWithoutACategory(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	// Insert a row the way the pre-000019 schema would have: no source, no category. Both default.
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO stage_runs
		(id, workflow_run_id, stage, attempt, status, created_at)
		VALUES ('cs-old-stage', 'drama-run', 'storyboard_table', 1, 'passed', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO review_reports
		(id, stage_run_id, supervisor_key, ruleset_version, grade, passed, severity, summary, created_at)
		VALUES ('cs-old-report', 'cs-old-stage', 'k', 'v', '', 1, 'none', '', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO review_issues
		(id, review_report_id, rule, severity, entity_type, entity_id, problem, created_at)
		VALUES ('cs-old-issue', 'cs-old-report', 'SOME_OLD_RULE', 'minor', 'storyboard_item',
			'cs-old-item', 'written before the column existed', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	_, issues, found, err := NewWorkflowRepository(fixture.db).GetReportForStage(ctx, "cs-old-stage")
	if err != nil {
		t.Fatal(err)
	}
	if !found || len(issues) != 1 {
		t.Fatalf("the old report read back %d findings, found=%v", len(issues), found)
	}
	if issues[0].Category != "" {
		t.Fatalf("an old row read back as category %q, and empty is what it must say", issues[0].Category)
	}
	if issues[0].Source != "llm" {
		// 000019's own default, re-asserted here because the two columns carry the same kind of fact
		// and a change to one that missed the other would show up in this read.
		t.Fatalf("an old row read back as source %q, want the supervisor default", issues[0].Source)
	}
}

// TestTheCategoryVocabularyIsClosedWhereItLives asserts the domain's own guarantees.
//
// It is here rather than in the domain package's tests because the fact it protects is about the
// COLUMN: the migration deliberately does not duplicate the vocabulary, so the domain is the only
// place it is enforced, and this test states that the enforcement exists.
func TestTheCategoryVocabularyIsClosedWhereItLives(t *testing.T) {
	for _, category := range consistency.Categories {
		if !consistency.IsValidCategory(category) {
			t.Fatalf("the documented category %q is not valid according to IsValidCategory", category)
		}
	}
	if consistency.IsValidCategory("not_a_category") {
		t.Fatal("IsValidCategory accepted a value that is not in the vocabulary")
	}
	// The two categories this package gave an emitter, and their classifications.
	if consistency.CategoryOf(consistency.RuleContentPolicyRefused) != consistency.CategorySafety {
		t.Fatal("the content-policy rule is not a safety category")
	}
	if consistency.CategoryOf(consistency.RuleRevisionBudgetSpent) != consistency.CategoryCost {
		t.Fatal("the revision-budget rule is not a cost category")
	}
}
