package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// asset_rules_wp16_helpers_test.go seeds the rows the asset and safety rules read.
//
// # Why these are helpers rather than statements inside each test
//
// Every one of them writes a row whose COLUMNS the rule reads, and a fixture that got a column wrong
// would fail in a way that looks like the rule not firing — which is exactly the defect this package
// exists to fix, so a helper that puts the wrong thing in the wrong column is the failure mode to
// design against. Each helper states which column it exists to set.

// seedPropUsage adds a prop asset whose story entity is `ownerEntityID`, cited by the FIRST board row.
//
// It also records the scene's source event, because the prop rule needs one: a scene adapted from no
// event is not a prop-continuity fault and the rule returns early, so a fixture that omitted the
// event would make the rule untestable through no fault of the rule's.
func seedPropUsage(t *testing.T, fixture wp10ConsistencyFixture, versionID, assetID, ownerEntityID string) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		// The scene's source event, which is what the prop rule joins through.
		`INSERT INTO story_events (id, project_id, ordinal, name, status, created_at, updated_at, revision)
		 VALUES ('cs-event', 'drama-project', 1, '渡口相遇', 'accepted',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`UPDATE scenes SET source_story_event_id = 'cs-event' WHERE id = 'cs-scene'`,
		// The prop's own story entity. It is a CHARACTER that takes no part in the event, which is
		// what makes the rule fire: a prop whose owner is absent from the scene cannot be in it.
		`INSERT INTO story_entities (id, project_id, entity_type, canonical_name, status, created_at, updated_at, revision)
		 VALUES ('` + ownerEntityID + `', 'drama-project', 'character', '路人', 'accepted',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		// The prop asset, with the entity link the rule reads.
		`INSERT INTO assets (id, project_id, asset_type, name, story_entity_id, current_approved_version_id,
			status, created_at, updated_at, revision)
		 VALUES ('` + assetID + `', 'drama-project', 'prop', '油纸伞', '` + ownerEntityID + `', '', 'active',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
	}
	for _, statement := range statements {
		if _, err := fixture.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the prop fixture failed: %v\n%s", err, statement)
		}
	}
	seedAssetVersionRow(t, fixture, assetID, versionID, 1, "approved")
	seedUsage(t, fixture, fixture.itemIDs[0], versionID, "prop")
}

// seedAssetWithVersion seeds an asset and one approved version of it, in force.
//
// It sets `current_approved_version_id`, which is what the APPROVAL rule compares a citation
// against — the location test needs that to stay silent so the only rule that can object is the
// location one.
func seedAssetWithVersion(t *testing.T, fixture wp10ConsistencyFixture, assetID, assetType, name, versionID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, story_entity_id, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES (?, 'drama-project', ?, ?, '', ?, 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		assetID, assetType, name, versionID); err != nil {
		t.Fatalf("seeding the asset failed: %v", err)
	}
	seedAssetVersionRow(t, fixture, assetID, versionID, 1, "approved")
}

// seedAssetVersionRow writes one asset_versions row with the lineage columns left empty.
//
// `based_on_version_id` and `parent_asset_version_id` are NOT NULL with an empty default, so a row
// written here starts with no lineage — which is the state the lineage rule is silent about and the
// state each lineage test then edits.
func seedAssetVersionRow(t *testing.T, fixture wp10ConsistencyFixture, assetID, versionID string, number int, status string) {
	t.Helper()
	if _, err := fixture.db.ExecContext(context.Background(), `INSERT INTO asset_versions
		(id, asset_id, version_number, status, created_by_type, created_at)
		VALUES (?, ?, ?, ?, 'user', '2026-01-01T00:00:00Z')`,
		versionID, assetID, number, status); err != nil {
		t.Fatalf("seeding the asset version failed: %v", err)
	}
}

// seedUsage records that one board row cites one asset version in a role.
//
// The role is the rule's whole input — `checkAssetApproval` reads every usage, the prop rule reads
// the ones `isPropRole` accepts, the location rule the ones `isLocationRole` accepts — so a test that
// wanted a rule to fire and got the role wrong would see silence and blame the rule.
func seedUsage(t *testing.T, fixture wp10ConsistencyFixture, itemID, versionID, role string) {
	t.Helper()
	if _, err := fixture.db.ExecContext(context.Background(), `INSERT INTO asset_usages
		(id, asset_version_id, consumer_type, consumer_id, usage_role, required, created_at)
		VALUES (?, ?, 'shot', ?, ?, 0, '2026-01-01T00:00:00Z')`,
		"cs-usage-"+role+"-"+itemID, versionID, itemID, role); err != nil {
		t.Fatalf("seeding the asset usage failed: %v", err)
	}
}

// attachPNGToVersion links a real image file object to one asset version.
//
// The MIME type is what the file-present rule compares against the asset's own type, so this helper
// exists to state a file that MATCHES: a `costume` holding an `image/png` is what a correct project
// looks like, and the test's negative case is that the critical finding disappears.
func attachPNGToVersion(t *testing.T, fixture wp10ConsistencyFixture, versionID string) {
	t.Helper()
	attachFile(t, fixture, versionID, "image/png", 2048)
}

// attachFile links a file object of one type and size to an asset version.
//
// It writes BOTH rows the rule joins: `file_objects` (which carries the mime type and the size) and
// `asset_files` (which carries the link). A fixture that wrote only the link would produce a hash with
// no object, which the rule reports as a DIFFERENT fault — "the link survives the bytes" — and the
// test would pass for the wrong reason.
func attachFile(t *testing.T, fixture wp10ConsistencyFixture, versionID, mimeType string, sizeBytes int64) {
	t.Helper()
	ctx := context.Background()
	// A distinct hash per (version, type) so a test that attaches two files does not collide on the
	// primary key, and the storage key must be 64 characters like the schema's own CHECK.
	sum := sha256.Sum256([]byte(versionID + "|" + mimeType))
	hash := hex.EncodeToString(sum[:])
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO file_objects
		(hash, storage_key, mime_type, size_bytes, created_at)
		VALUES (?, ?, ?, ?, '2026-01-01T00:00:00Z')`, hash, hash, mimeType, sizeBytes); err != nil {
		t.Fatalf("seeding the file object failed: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO asset_files
		(asset_version_id, file_hash, role, created_at)
		VALUES (?, ?, 'primary', '2026-01-01T00:00:00Z')`, versionID, hash); err != nil {
		t.Fatalf("linking the file failed: %v", err)
	}
}

// seedFailedJob writes a failed generation job for one board row.
//
// The error code is the rule's whole input, and the status must be `failed`: the rule reads failed
// jobs only, so a fixture that left the status at `queued` would produce silence the rule is not
// responsible for. The columns are the ones `000023_job_types.sql` declares NOT NULL.
func seedFailedJob(t *testing.T, fixture wp10ConsistencyFixture, jobID, itemID, errorCode string) {
	t.Helper()
	if _, err := fixture.db.ExecContext(context.Background(), `INSERT INTO generation_jobs
		(id, project_id, entity_type, entity_id, job_type, status, idempotency_key, error_code,
		 error_message, created_at, updated_at, finished_at)
		VALUES (?, 'drama-project', 'storyboard_item', ?, 'image_generation', 'failed', ?, ?, '',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z')`,
		jobID, itemID, "cs-key-"+jobID, errorCode); err != nil {
		t.Fatalf("seeding the failed job failed: %v", err)
	}
}

// recordReviewReport writes one report and its findings through the SERVICE, not the repository.
//
// # Why it goes through `RecordReview` and not `CreateReport`
//
// A mutation review found this: the first version of this helper built `workflow.ReviewIssue` values
// itself and called the repository, so it SKIPPED the application layer where `ReviewIssueInput.Category`
// is carried onto the row. Dropping that assignment left every test green — the helper had quietly
// stepped over the exact line under test, which is the failure mode of a fixture that constructs the
// thing it is supposed to observe travelling.
//
// So the helper takes the INPUT shape a caller supplies and lets the service mint the row. The service
// is built the way the composition root builds it (`drama_wiring.go`), with the repository satisfying
// all five ports, so this exercises the production path rather than a parallel one.
func recordReviewReport(t *testing.T, fixture wp10ConsistencyFixture, stageRunID string, issues []appworkflow.ReviewIssueInput) {
	t.Helper()
	ctx := context.Background()
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO stage_runs
		(id, workflow_run_id, stage, attempt, status, created_at)
		VALUES (?, 'drama-run', 'storyboard_table', 1, 'reviewing', '2026-01-01T00:00:00Z')`,
		stageRunID); err != nil {
		t.Fatalf("seeding the stage run failed: %v", err)
	}
	repository := NewWorkflowRepository(fixture.db)
	service := appworkflow.NewService(appworkflow.Options{
		Runs: repository, Stages: repository, Reviews: repository,
		Decisions: repository, Events: repository,
		Clock: dramaClock{}, IDs: dramaIDGenerator(),
	})
	if _, _, err := service.RecordReview(ctx, appworkflow.RecordReviewRequest{
		StageRunID: stageRunID, SupervisorKey: "production.supervision.storyboard_table",
		RulesetVersion: "deterministic/v1", Passed: false, Severity: workflow.SeverityCritical,
		Summary: "seeded", Issues: issues,
	}); err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
}

// dramaClock is the fixture clock, in the shape the workflow service's port wants.
type dramaClock struct{}

func (dramaClock) Now() time.Time { return dramaTime() }

// hasRule reports whether a finding list contains one rule's finding.
func hasRule(findings []consistency.Finding, rule string) bool {
	for _, finding := range findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}
