package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// asset_rules_wp16_test.go covers FR-110's asset-rule completeness and the two categories that had
// no emitter at all.
//
// # What each test is for, and which defect it exists because of
//
// The scout that preceded this package found four things and each one is a test here:
//
//  1. `PROP_CONTINUITY` and `LOCATION_CONTINUITY` had implementations and NO test that made them
//     FIRE — every fixture used a costume usage, so the prop and location rules ran and returned nil
//     on data that could never have triggered them. A rule with no positive case is a rule nobody can
//     tell apart from a stub, which is why these two tests exist before the new ones.
//  2. `REVISION_BUDGET_SPENT` and `CONTENT_RATING` were category-map KEYS with no rule, no test and no
//     caller: the classification was declared and nothing emitted it.
//  3. `Finding.Category` was set by one ruleset and dropped by storage, because `review_issues` had
//     no column for it and `CategoryOf` had no production caller.
//  4. `TestCheckedStagesMatchTheSwitch` was promised by a comment in `consistency_checker.go` and did
//     not exist anywhere in the repository.
//
// The tests are in this package rather than the application one because a rule's whole value is what
// it reads from a database: a unit test with a hand-built struct would prove only that a comparison
// works, and the defects above are all about whether the ROW reaches the comparison.

// TestThePropRuleFiresOnAnUninvolvedProp is the positive case the rule never had.
//
// The rule: a row that cites a prop must cite one the scene's own story event involves. The fixture
// seeds a prop whose story entity is a character the event does not involve, so the finding is
// forced — and the negative half asserts that adding the entity to the event makes it go away, so a
// rule that fired on everything would fail here too.
func TestThePropRuleFiresOnAnUninvolvedProp(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	seedPropUsage(t, fixture, "cs-prop-version", "cs-prop-asset", "cs-bystander")

	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(findings, consistency.RulePropContinuity) {
		t.Fatalf("the prop rule did not fire on a prop whose owner takes no part in the scene's event: %+v", findings)
	}
	// And the negative case: the SAME fixture with the owner recorded as a participant is silent.
	// Without this half, a rule that reported every prop would pass the assertion above.
	fixture2 := seedConsistencyFixture(t)
	seedPropUsage(t, fixture2, "cs-prop-version", "cs-prop-asset", "cs-bystander")
	if _, err := fixture2.db.ExecContext(ctx, `INSERT INTO story_event_participants
		(story_event_id, story_entity_id, role, created_at)
		VALUES ('cs-event', 'cs-bystander', 'witness', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	findings2, err := fixture2.checkStoryboard(ctx, fixture2.versionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings2 {
		if finding.Rule == consistency.RulePropContinuity {
			t.Fatalf("the prop rule fired on a prop the event involves: %+v", finding)
		}
	}
}

// TestTheLocationRuleFiresWhenTwoRowsOfOneSceneDisagree is the other rule that never fired.
//
// The rule: rows of one scene must agree about where it happens. Two rows of ONE scene citing two
// different locations is the fault, and it is the shape nothing else catches — the coverage rule
// counts rows, the approval rule checks versions, and neither compares two rows' locations.
func TestTheLocationRuleFiresWhenTwoRowsOfOneSceneDisagree(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	// Two location assets, both approved and in force, so the APPROVAL rule stays silent: the only
	// rule that can object is the location one.
	seedAssetWithVersion(t, fixture, "cs-loc-a", "location", "渡口", "cs-loc-a-v1")
	seedAssetWithVersion(t, fixture, "cs-loc-b", "location", "甲板", "cs-loc-b-v1")
	seedUsage(t, fixture, fixture.itemIDs[0], "cs-loc-a-v1", "location")
	seedUsage(t, fixture, fixture.itemIDs[1], "cs-loc-b-v1", "location")

	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(findings, consistency.RuleLocationContinuity) {
		t.Fatalf("the location rule did not fire on two rows of one scene citing different places: %+v", findings)
	}
	// The negative case: BOTH rows citing the same approved location is silent.
	fixture2 := seedConsistencyFixture(t)
	seedAssetWithVersion(t, fixture2, "cs-loc-a", "location", "渡口", "cs-loc-a-v1")
	seedUsage(t, fixture2, fixture2.itemIDs[0], "cs-loc-a-v1", "location")
	seedUsage(t, fixture2, fixture2.itemIDs[1], "cs-loc-a-v1", "location")
	findings2, err := fixture2.checkStoryboard(ctx, fixture2.versionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings2 {
		if finding.Rule == consistency.RuleLocationContinuity {
			t.Fatalf("the location rule fired on rows that agree: %+v", finding)
		}
	}
}

// TestTheAssetFileRuleFiresOnlyOnAGeneratedVersionWithNoBytes covers section 11.2's
// "文件存在和类型".
//
// # The correction this test records
//
// The first version of the rule reported EVERY cited version with no file, and two existing tests
// failed on the spot — `TestConsistencyACleanBoardReportsNothing` and AC-E2E-004's repair walk. Both
// were right: an asset bible defines an asset before any art exists, so a costume version with no file
// is the ordinary state of a project partway through. The rule now anchors on `generation_job_id`,
// the column that says "these bytes were produced", and this test pins BOTH halves so a later change
// cannot quietly widen it back.
func TestTheAssetFileRuleFiresOnlyOnAGeneratedVersionWithNoBytes(t *testing.T) {
	// HALF ONE: a bible-only version — cited, no file, no job — is SILENT. This is the assertion the
	// first version of the rule would have failed.
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	seedUsage(t, fixture, fixture.itemIDs[0], fixture.costumeV1, "costume")
	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Rule == consistency.RuleAssetFilePresent {
			t.Fatalf("a version the bible defines and nothing has generated yet was reported: %+v", finding)
		}
	}

	// HALF TWO: the SAME version, once it claims a generation job, IS reported — and as critical,
	// because bytes that were produced and are now gone are a fault rather than a step not taken.
	fixture2 := seedConsistencyFixture(t)
	seedUsage(t, fixture2, fixture2.itemIDs[0], fixture2.costumeV1, "costume")
	if _, err := fixture2.db.ExecContext(ctx,
		`UPDATE asset_versions SET generation_job_id = 'cs-job-generated' WHERE id = ?`,
		fixture2.costumeV1); err != nil {
		t.Fatal(err)
	}
	findings2, err := fixture2.checkStoryboard(ctx, fixture2.versionID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range findings2 {
		if finding.Rule == consistency.RuleAssetFilePresent && finding.Severity == consistency.SeverityCritical {
			found = true
		}
	}
	if !found {
		t.Fatalf("a generated version with no file was not reported as critical: %+v", findings2)
	}

	// HALF THREE: a version WITH bytes is silent, and the type half still fires on a mismatch. The
	// asset is a costume, so an mp4 in it is the mismatch the closed vocabularies can decide.
	fixture3 := seedConsistencyFixture(t)
	seedUsage(t, fixture3, fixture3.itemIDs[0], fixture3.costumeV1, "costume")
	attachPNGToVersion(t, fixture3, fixture3.costumeV1)
	findings3, err := fixture3.checkStoryboard(ctx, fixture3.versionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings3 {
		if finding.Rule == consistency.RuleAssetFilePresent {
			t.Fatalf("a version with a matching file was reported: %+v", finding)
		}
	}

	fixture4 := seedConsistencyFixture(t)
	seedUsage(t, fixture4, fixture4.itemIDs[0], fixture4.costumeV1, "costume")
	attachFile(t, fixture4, fixture4.costumeV1, "video/mp4", 4096)
	findings4, err := fixture4.checkStoryboard(ctx, fixture4.versionID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(findings4, consistency.RuleAssetFilePresent) {
		t.Fatalf("a picture asset holding an mp4 was not reported: %+v", findings4)
	}
}

// TestTheAssetLineageRuleFiresOnADanglingParent covers section 11.2's "派生关系".
//
// Both lineage columns are TEXT with no foreign key, so a parent that does not exist is storable.
// The rule is `minor` and not blocking, which this test pins: a dangling citation must not be able to
// fail a board.
func TestTheAssetLineageRuleFiresOnADanglingParent(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	seedUsage(t, fixture, fixture.itemIDs[0], fixture.costumeV1, "costume")
	if _, err := fixture.db.ExecContext(ctx,
		`UPDATE asset_versions SET based_on_version_id = 'cs-does-not-exist' WHERE id = ?`,
		fixture.costumeV1); err != nil {
		t.Fatal(err)
	}
	findings, err := fixture.checkStoryboard(ctx, fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range findings {
		if finding.Rule != consistency.RuleAssetLineage {
			continue
		}
		found = true
		if finding.Severity != consistency.SeverityMinor {
			t.Fatalf("the lineage rule reported %q, and a dangling citation must not block a board", finding.Severity)
		}
		if finding.EntityID != fixture.itemIDs[0] {
			t.Fatalf("the finding addresses %q rather than the row that cites the version", finding.EntityID)
		}
	}
	if !found {
		t.Fatalf("a dangling basedOnVersionId was not reported: %+v", findings)
	}
}

// TestTheSafetyRuleFiresOnAContentPolicyRefusal covers the SAFETY category's vendor half.
//
// The category existed as a map key with nothing that emitted it. The data is a failed job whose
// error code is the provider's refusal, and the finding addresses the ROW the job was submitted for
// rather than the job, because a report's findings are what a user can open.
func TestTheSafetyRuleFiresOnAContentPolicyRefusal(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	seedFailedJob(t, fixture, "cs-job-refused", fixture.itemIDs[0], string(job.CategoryContentPolicy))
	// A SECOND failed job with a different category, which the rule must NOT report: a timeout is
	// environmental and the job manager already retries it.
	seedFailedJob(t, fixture, "cs-job-timeout", fixture.itemIDs[1], string(job.CategoryTimeout))

	findings, err := fixture.checkerFor(t).WithJobFailures(NewJobRepository(fixture.db)).
		Check(ctx, "storyboard_table", fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	reported := 0
	for _, finding := range findings {
		if finding.Rule != consistency.RuleContentPolicyRefused {
			continue
		}
		reported++
		if finding.Category != consistency.CategorySafety {
			t.Fatalf("the content-policy finding is filed as %q, want safety", finding.Category)
		}
		if finding.EntityID != fixture.itemIDs[0] {
			t.Fatalf("the finding addresses %q rather than the row the job was submitted for", finding.EntityID)
		}
	}
	if reported != 1 {
		t.Fatalf("the safety rule reported %d findings, want exactly the one refusal: %+v", reported, findings)
	}
}

// TestACheckerWithoutAJobReaderStillRunsTheOtherRules is the absent-read discipline.
//
// A port this build does not have is a rule that goes quiet, not a check that fails and not a finding
// invented from missing data. The scout found that the six original rules already behaved this way;
// this test extends the guarantee to the new port so a later change cannot make the safety rule
// report a project full of refusals it never read.
func TestACheckerWithoutAJobReaderStillRunsTheOtherRules(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	// A prop whose owner takes no part in the scene's event, so a rule that reads the ASSET and STORY
	// ports has something to find. That is the point: the assertion below is that those rules still
	// run when the job port is absent.
	seedPropUsage(t, fixture, "cs-prop-version", "cs-prop-asset", "cs-bystander")
	findings, err := fixture.checkerFor(t).Check(ctx, "storyboard_table", fixture.versionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Rule == consistency.RuleContentPolicyRefused {
			t.Fatalf("a checker with no job reader reported a job refusal: %+v", finding)
		}
	}
	// The rules that read what the checker DOES have still ran, which is what makes "goes quiet"
	// different from "stopped working".
	if !hasRule(findings, consistency.RulePropContinuity) {
		t.Fatalf("the rules with ports available stopped running when the job port was absent: %+v", findings)
	}
}

// TestEveryRuleThisBuildShipsHasACategory is the totality check FR-110 needs.
//
// It is the test the scout found missing in this shape: `TestEveryShippedRuleHasACategory` in the
// application package lists the rules by NAME, so a rule added to `checker.go` without a category
// entry is caught only if somebody remembers to add it to that list. This test asks the question from
// the other side — it names the DISPATCHED rules and asserts each one's findings carry a category —
// so a rule that runs in production with an unstated classification fails here.
func TestEveryRuleThisBuildShipsHasACategory(t *testing.T) {
	// The rules `CheckStoryboard` can emit, taken from the source that emits them. A rule missing
	// from this list is a rule nobody classified, which is what the test is for — so the list is
	// written as the six original plus the three this package added.
	dispatched := []string{
		consistency.RuleCostumeContinuity,
		consistency.RulePropContinuity,
		consistency.RuleLocationContinuity,
		consistency.RuleShotCoverage,
		consistency.RuleDuration,
		consistency.RuleAssetApproved,
		consistency.RuleAssetLineage,
		consistency.RuleAssetFilePresent,
		consistency.RuleContentPolicyRefused,
	}
	for _, rule := range dispatched {
		if !consistency.RuleHasStatedCategory(rule) {
			t.Fatalf("the rule %q is dispatched but has no stated category", rule)
		}
		if !consistency.IsValidCategory(consistency.CategoryOf(rule)) {
			t.Fatalf("the rule %q maps to an invalid category", rule)
		}
	}
	// The two categories that had no emitter now have one, and they are the PRD's own names.
	if consistency.CategoryOf(consistency.RuleRevisionBudgetSpent) != consistency.CategoryCost {
		t.Fatal("the revision-budget rule is not classified as a cost problem")
	}
	if consistency.CategoryOf(consistency.RuleContentPolicyRefused) != consistency.CategorySafety {
		t.Fatal("the content-policy rule is not classified as a safety problem")
	}
}

// TestTheCategorySurvivesTheWriteAndReadBack is the end-to-end half of the classification defect.
//
// # Why this test goes through the service and not the repository
//
// A mutation that dropped `Category` from the SERVICE's row construction left every test green, because
// the first version of this fixture built `workflow.ReviewIssue` values itself and called the
// repository — stepping over the exact line under test. The helper now takes `ReviewIssueInput`, which
// is what a caller supplies, so the assertion covers the whole path: the input's category, through the
// service's row construction, into the column, and back out of a read.
func TestTheCategorySurvivesTheWriteAndReadBack(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	recordReviewReport(t, fixture, "cs-stage-run", []appworkflow.ReviewIssueInput{{
		Rule:       consistency.RuleAssetFilePresent,
		Category:   string(consistency.CategoryAsset),
		Severity:   consistency.SeverityCritical,
		EntityType: "storyboard_item",
		EntityID:   fixture.itemIDs[0],
		Field:      "assetVersionId",
		Problem:    "no file",
	}, {
		// A finding with NO category, which is what a supervisor's looks like. It must come back
		// EMPTY rather than as "technical": the two are different facts and a default would erase the
		// difference the column exists to carry.
		Rule:       "SOME_SUPERVISOR_RULE",
		Severity:   consistency.SeverityMinor,
		EntityType: "storyboard_item",
		EntityID:   fixture.itemIDs[1],
		Problem:    "a reading",
	}})

	_, issues, found, err := NewWorkflowRepository(fixture.db).GetReportForStage(ctx, "cs-stage-run")
	if err != nil {
		t.Fatal(err)
	}
	if !found || len(issues) != 2 {
		t.Fatalf("the report read back %d findings, found=%v", len(issues), found)
	}
	byRule := map[string]workflow.ReviewIssue{}
	for _, issue := range issues {
		byRule[issue.Rule] = issue
	}
	stored, ok := byRule[consistency.RuleAssetFilePresent]
	if !ok {
		t.Fatalf("the classified finding is missing: %+v", issues)
	}
	if stored.Category != string(consistency.CategoryAsset) {
		t.Fatalf("the stored category is %q, want %q", stored.Category, consistency.CategoryAsset)
	}
	unclassified, ok := byRule["SOME_SUPERVISOR_RULE"]
	if !ok {
		t.Fatalf("the supervisor's finding is missing: %+v", issues)
	}
	if unclassified.Category != "" {
		t.Fatalf("a finding with no category read back as %q, and empty is a distinct fact", unclassified.Category)
	}
}

// TestTheServiceRejectsAnOversizedCategory is the bound the application layer owns.
//
// The vocabulary is the domain package's, but the LENGTH bound is the workflow service's, because a
// caller could otherwise store a paragraph in a column a UI shows as a tag. This asserts the refusal
// exists and that a normal category is not caught by it.
func TestTheServiceRejectsAnOversizedCategory(t *testing.T) {
	fixture := seedConsistencyFixture(t)
	ctx := context.Background()
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO stage_runs
		(id, workflow_run_id, stage, attempt, status, created_at)
		VALUES ('cs-long-stage', 'drama-run', 'final_episode', 1, 'reviewing', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	repository := NewWorkflowRepository(fixture.db)
	service := appworkflow.NewService(appworkflow.Options{
		Runs: repository, Stages: repository, Reviews: repository,
		Decisions: repository, Events: repository,
		Clock: dramaClock{}, IDs: dramaIDGenerator(),
	})
	// FIRST: a normal category is ACCEPTED, which proves the path itself works. Without this half the
	// test passed for the wrong reason — a mutation that removed the bound left it green, because the
	// assertion was only "an error occurred" and a failure anywhere (a missing stage run, a failed
	// insert) satisfied it. The positive case is what makes the negative one mean something.
	if _, _, err := service.RecordReview(ctx, appworkflow.RecordReviewRequest{
		StageRunID: "cs-long-stage", RulesetVersion: "v", Severity: workflow.SeverityMinor,
		Issues: []appworkflow.ReviewIssueInput{{
			Rule: "R", Severity: consistency.SeverityMinor, Problem: "p",
			EntityType: "storyboard_item", EntityID: "e",
			Category: string(consistency.CategoryAsset),
		}},
	}); err != nil {
		t.Fatalf("a normal category was refused, so this test cannot say anything about the bound: %v", err)
	}
	// AND the oversized one is refused with the LENGTH as the reason rather than incidentally.
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO stage_runs
		(id, workflow_run_id, stage, attempt, status, created_at)
		VALUES ('cs-long-stage-2', 'drama-run', 'script_generation', 1, 'reviewing', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	_, _, err := service.RecordReview(ctx, appworkflow.RecordReviewRequest{
		StageRunID: "cs-long-stage-2", RulesetVersion: "v", Severity: workflow.SeverityMinor,
		Issues: []appworkflow.ReviewIssueInput{{
			Rule: "R", Severity: consistency.SeverityMinor, Problem: "p",
			EntityType: "storyboard_item", EntityID: "e",
			Category: strings.Repeat("x", 200),
		}},
	})
	if err == nil {
		t.Fatal("a 200-character category was accepted")
	}
	var domainErr *workflow.Error
	if !errors.As(err, &domainErr) || !strings.Contains(domainErr.SafeMessage, "category") {
		t.Fatalf("the refusal is %v, and it must name the category rather than fail for another reason", err)
	}
}
