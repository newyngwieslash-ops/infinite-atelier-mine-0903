package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appscriptpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/scriptpipeline"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// canary_script_test.go drives WP-08's fifteen scope items to an APPROVED SCRIPT.
//
// It is the acceptance test the ROADMAP's last scope item names: "Canary Drama E2E 到 approved Script".
// The chain is the three stages in FR-040's order, each through the real pipeline, the real engine and
// the real services:
//
//	skeleton → supervision → user PASS → strategy → supervision → PASS →
//	generation (version row + structure) → supervision → PASS → approved
//
// Everything but the model and the file store is production code, for the reason the existing canary
// records: section 18.3 forbids CI calling a paid provider, and the object store has its own tests. The
// model is the deterministic mock, whose three script-stage branches are what make this chain possible
// at all — a generic tool-call reply would ask for the version ROW in the generation stage and approve a
// script with no scenes.
//
// It also covers AC-SCRIPT-001, AC-SCRIPT-002 and AC-SCRIPT-003, each asserted where its subject is
// reached rather than in a separate walk.

// scriptCanary is the canary plus the pipeline under test.
type scriptCanary struct {
	*canary
	pipeline *appscriptpipeline.Service
}

// newScriptCanary composes the pipeline over the existing canary's stack.
//
// The pipeline takes the Inspector's READER rather than the inspector itself, which is what lets this
// composition reuse the canary's agent repository: the same object is the runtime's RunStore and the
// reader the pipeline walks to find a stage's artifact.
func newScriptCanary(t *testing.T) *scriptCanary {
	t.Helper()
	base := newCanary(t)
	pipeline := appscriptpipeline.New(appscriptpipeline.Options{
		Engine:   base.engine,
		Runtime:  base.runtime,
		Script:   base.script,
		Workflow: base.workflow,
		Assembly: base.assembly,
		Runs:     base.repo,
	})
	return &scriptCanary{canary: base, pipeline: pipeline}
}

// TestCanaryScriptPipelineToApprovedScript is the acceptance walk.
func TestCanaryScriptPipelineToApprovedScript(t *testing.T) {
	canary := newScriptCanary(t)
	// The mock answers each stage with the write that stage owes.
	canary.mock.SetScenario(infraproviders.MockScenarioToolCall)

	// --- The story events the skeleton selects, so the link set names rows that exist ---
	eventIDs := canary.seedStoryEvents(t, 2)

	// --- S1: the story skeleton ---
	skeleton := canary.runScriptStage(t, appscriptpipeline.StageRequest{
		WorkflowRunID:    canary.ids.workflowRun,
		Stage:            appscriptpipeline.StageStorySkeleton,
		ProjectID:        canary.ids.project,
		EpisodeID:        canary.ids.episode,
		Task:             "Write the skeleton for this episode.",
		SelectedEventIDs: eventIDs,
	})
	// AC-SCRIPT-001's chain: the workflow was created, an independent AgentRun ran, a VERSION exists, and
	// the stage moved to reviews.
	canary.assertStageReviewed(t, skeleton.StageRun, "story_skeleton")

	// --- AC-SCRIPT-001: the supervisor reads the DATABASE, and its report is stored ---
	skeletonVersionID := canary.singleArtifactOf(t, skeleton, "story_skeleton")
	skeletonReview := canary.review(t, skeleton.StageRun, skeletonVersionID)
	if !skeletonReview.Report.Passed {
		t.Fatalf("the skeleton review failed: %s", skeletonReview.Report.Summary)
	}
	// The supervisor's verdict moved the stage to the user's gate, because the stage's policy requires
	// one — which is what makes the next step a user's decision rather than the pipeline's.
	if skeletonReview.StageRun.Status != workflow.StageWaitingUser {
		t.Fatalf("a passing review left the stage at %q, want waiting_user", skeletonReview.StageRun.Status)
	}

	// --- AC-SCRIPT-001: the user PASSes, and exactly one version is approved ---
	canary.pass(t, skeleton.StageRun, skeletonVersionID, "the skeleton is what we want to shoot.")
	approved := canary.approvedSkeleton(t)
	if approved.ID != skeletonVersionID {
		t.Fatalf("the approved skeleton is %q, want the version the stage wrote (%q)", approved.ID, skeletonVersionID)
	}

	// --- AC-SCRIPT-002's FIX: a revision that changes a PINNED field is refused ---
	//
	// The criterion's scenario is a skeleton whose ending hook is missing, with the fields the model got
	// right pinned. So the pin is placed on the version the user just approved, and the refusal is
	// asserted on the SERVICE — the write path — rather than on a prompt.
	canary.assertPinnedFieldIsProtected(t, skeletonVersionID)

	// --- S2: the adaptation strategy ---
	strategy := canary.runScriptStage(t, appscriptpipeline.StageRequest{
		WorkflowRunID: canary.ids.workflowRun,
		Stage:         appscriptpipeline.StageAdaptationStrategy,
		ProjectID:     canary.ids.project,
		EpisodeID:     canary.ids.episode,
		Task:          "Decide what happens to each event.",
	})
	canary.assertStageReviewed(t, strategy.StageRun, "adaptation_strategy")
	strategyVersionID := canary.singleArtifactOf(t, strategy, "adaptation_strategy")
	if reviewed := canary.review(t, strategy.StageRun, strategyVersionID); !reviewed.Report.Passed {
		t.Fatalf("the strategy review failed: %s", reviewed.Report.Summary)
	}
	canary.pass(t, strategy.StageRun, strategyVersionID, "the strategy is sound.")

	// --- S3: the script itself, which is a version ROW plus a STRUCTURE ---
	//
	// The row is created before the stage runs, because a structure needs a version to hang off and the
	// write tool takes the version's id. This is the ordering the pipeline's own state layer assumes: a
	// generation stage's prompt carries `script_version=` for exactly this reason.
	scriptVersionID := canary.seedScriptVersionRow(t, skeletonVersionID, strategyVersionID)
	generation := canary.runScriptStage(t, appscriptpipeline.StageRequest{
		WorkflowRunID:     canary.ids.workflowRun,
		Stage:             appscriptpipeline.StageScriptGeneration,
		ProjectID:         canary.ids.project,
		EpisodeID:         canary.ids.episode,
		Task:              "Write the script.",
		ScriptVersionID:   scriptVersionID,
		SkeletonVersionID: skeletonVersionID,
		StrategyVersionID: strategyVersionID,
	})
	canary.assertStageReviewed(t, generation.StageRun, "script_generation")
	// The write went to the version the state named, which is what makes the row and the content one
	// artifact rather than two.
	if ids := generation.ArtifactIDs; len(ids) != 1 || ids[0] != scriptVersionID {
		t.Fatalf("the generation stage wrote %v, want the version the state named (%q)", ids, scriptVersionID)
	}

	// --- AC-SCRIPT-003: the structure is formally persisted, and the duration is DERIVED ---
	canary.assertScriptStructure(t, scriptVersionID)
	if reviewed := canary.review(t, generation.StageRun, scriptVersionID); !reviewed.Report.Passed {
		t.Fatalf("the script review failed: %s", reviewed.Report.Summary)
	}
	// AC-SCRIPT-003's "版本 diff": the two script versions differ by everything, and the diff says so
	// through the domain's own function rather than through a string comparison.
	canary.assertVersionDiff(t, scriptVersionID)

	// --- AC-SCRIPT-001's last clause: approved is unique, and the run ends ---
	canary.pass(t, generation.StageRun, scriptVersionID, "shoot this.")
	approvedScript := canary.approvedScript(t, scriptVersionID)
	if approvedScript.ID != scriptVersionID {
		t.Fatalf("the approved script is %q, want %q", approvedScript.ID, scriptVersionID)
	}
	// AC-SCRIPT-003's "Canvas projection": the version's scenes are projected, which is the write path
	// WP-05 built with no caller until now.
	canary.assertCanvasProjection(t, scriptVersionID)

	// --- The frozen rule: an approved version cannot be rewritten ---
	//
	// This is the check WP-05 wrote `IsContentFrozen` for and recorded that nothing called; WP-08's write
	// paths are its callers. It is asserted AFTER approval, which is the only state it applies to.
	canary.assertApprovedVersionIsFrozen(t, scriptVersionID)
}

// runScriptStage runs one stage through the pipeline and asserts the attempt is in play.
func (c *scriptCanary) runScriptStage(t *testing.T, request appscriptpipeline.StageRequest) appscriptpipeline.StageResult {
	t.Helper()
	result, err := c.pipeline.RunStage(context.Background(), request)
	if err != nil {
		t.Fatalf("running the %s stage: %v", request.Stage, err)
	}
	if result.Outcome.RunID == "" {
		t.Fatal("the stage ran without recording a run")
	}
	return result
}

// assertStageReviewed covers AC-SCRIPT-001's "Execution 独立 AgentRun" and the review hand-off.
func (c *scriptCanary) assertStageReviewed(t *testing.T, stage workflow.StageRun, stageName string) {
	t.Helper()
	if stage.Stage != workflow.StageName(stageName) {
		t.Fatalf("the attempt names stage %q, want %q", stage.Stage, stageName)
	}
	// The status is REVIEWING, which is what the pipeline moved it to: the runtime validates output and
	// never touches business state, so a stage that has run is moved by the layer that owns the machine.
	if stage.Status != workflow.StageReviewing {
		t.Fatalf("%s: the stage is %q after running, want reviewing", stageName, stage.Status)
	}
	// The run is INDEPENDENT: its own row, with its own identifier, and it names the stage attempt.
	runs, err := c.repo.ListRuns(context.Background(), c.ids.project, 200)
	if err != nil {
		t.Fatalf("listing the runs: %v", err)
	}
	found := false
	for _, run := range runs {
		if run.StageRunID == stage.ID {
			found = true
			// §4.2 requires a run to name the skill version it ran, and a run without one could not be
			// reproduced.
			if strings.TrimSpace(run.SkillVersionID) == "" {
				t.Fatalf("%s: the run cites no skill version", stageName)
			}
		}
	}
	if !found {
		t.Fatalf("%s: no agent run was recorded against the attempt", stageName)
	}
}

// singleArtifactOf returns the one artifact a stage's write produced.
//
// The count is asserted, because a stage that wrote two versions or none would make every later
// assertion about "the version" ambiguous — and the ambiguity would be the bug.
func (c *scriptCanary) singleArtifactOf(t *testing.T, result appscriptpipeline.StageResult, stageName string) string {
	t.Helper()
	if len(result.ArtifactIDs) != 1 {
		t.Fatalf("%s: the stage wrote %v, want exactly one version", stageName, result.ArtifactIDs)
	}
	return result.ArtifactIDs[0]
}

// review runs the supervisor and applies its verdict.
//
// The supervisor is chosen by the pipeline's own map rather than by the caller, which is the property
// WP-08 exists to make true: `script_generation`'s supervisor cannot be found by the last-segment match,
// so the map states it.
//
// THE SCENARIO IS SWITCHED, and the switch is not a convenience. It is a property of the two layers: an
// execution stage WRITES, so it is driven by the tool-call scenario, and a supervisor may not write at
// all (§6.3's matrix, and the registry refuses a manifest that grants it one). A supervisor answering the
// tool-call scenario therefore asks for a write it is not allowed to make, and the ACL refuses it — which
// is the ACL working, and would make this walk a test of the refusal rather than of the review.
func (c *scriptCanary) review(t *testing.T, stage workflow.StageRun, artifactVersionID string) appscriptpipeline.SupervisionResult {
	t.Helper()
	c.mock.SetScenario(infraproviders.MockScenarioNormal)
	// Restored for the caller, because the next execution stage in the walk needs it.
	defer c.mock.SetScenario(infraproviders.MockScenarioToolCall)
	result, err := c.pipeline.RunSupervision(context.Background(), appscriptpipeline.SupervisionRequest{
		StageRunID:        stage.ID,
		ProjectID:         c.ids.project,
		EpisodeID:         c.ids.episode,
		ArtifactVersionID: artifactVersionID,
	})
	if err != nil {
		t.Fatalf("supervising %s: %v", stage.Stage, err)
	}
	// AC-SCRIPT-001's "ReviewReport": a stored report, naming the ruleset it judged against.
	if strings.TrimSpace(result.Report.RulesetVersion) == "" {
		t.Fatal("the stored review report names no ruleset version")
	}
	if result.Report.StageRunID != stage.ID {
		t.Fatalf("the report belongs to %q, want the attempt %q", result.Report.StageRunID, stage.ID)
	}
	// The report is persisted, which is what "Supervisor 读取 DB → ReviewReport" ends with: a verdict
	// that lived only in this process would not survive the review the user is about to make.
	stored, issues, err := c.workflow.GetReviewReport(context.Background(), stage.ID)
	if err != nil {
		t.Fatalf("reading the stored report: %v", err)
	}
	if stored.ID != result.Report.ID {
		t.Fatalf("the stored report is %q, want %q", stored.ID, result.Report.ID)
	}
	if len(issues) != len(result.Issues) {
		t.Fatalf("the stored report has %d findings and %d were recorded", len(issues), len(result.Issues))
	}
	result.Report = stored
	return result
}

// pass applies the user's approval of the artifact one attempt produced.
//
// The version travels with the decision because the two acts are separate: the gate moves the STAGE, and
// the approval makes a version the one in force. AC-SCRIPT-001's "approved 唯一" is about the second.
func (c *scriptCanary) pass(t *testing.T, stage workflow.StageRun, versionID, instruction string) {
	t.Helper()
	moved, err := c.pipeline.ApplyUserGate(context.Background(), appscriptpipeline.GateRequest{
		StageRunID:        stage.ID,
		Decision:          workflow.GateApprove,
		ArtifactVersionID: versionID,
		Instruction:       instruction,
		CreatedByID:       "user-1",
	})
	if err != nil {
		t.Fatalf("approving %s: %v", stage.Stage, err)
	}
	if moved.Status != workflow.StagePassed {
		t.Fatalf("%s: the stage is %q after approval, want passed", stage.Stage, moved.Status)
	}
}

// approvedSkeleton returns the episode's approved skeleton version.
func (c *scriptCanary) approvedSkeleton(t *testing.T) scriptdomain.StorySkeletonVersion {
	t.Helper()
	id, err := NewScriptRepository(c.db).CurrentApprovedSkeletonVersionID(context.Background(), c.ids.episode)
	if err != nil {
		t.Fatalf("reading the approved skeleton: %v", err)
	}
	if id == "" {
		t.Fatal("no skeleton version is approved")
	}
	version, err := NewScriptRepository(c.db).GetStorySkeletonVersion(context.Background(), id)
	if err != nil {
		t.Fatalf("reading the approved skeleton version: %v", err)
	}
	// AC-SCRIPT-001's "approved 唯一": the schema enforces it with a partial unique index, and this
	// counts the rows to prove the enforcement is doing something rather than being assumed.
	count := c.countApprovedSkeletons(t)
	if count != 1 {
		t.Fatalf("%d skeleton versions are approved, want exactly one", count)
	}
	return version
}

// countApprovedSkeletons counts the episode's approved skeleton versions.
func (c *scriptCanary) countApprovedSkeletons(t *testing.T) int {
	t.Helper()
	var count int
	if err := c.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM story_skeleton_versions WHERE episode_id = ? AND status = 'approved'`,
		c.ids.episode).Scan(&count); err != nil {
		t.Fatalf("counting the approved skeletons: %v", err)
	}
	return count
}

// approvedScript asserts that the user's gate approved the script version.
//
// It does NOT approve it, and the first version of this helper did — which failed with "this version is
// already approved", because the gate approves the artifact itself. That is the right division: a caller
// submits a DECISION and the pipeline makes the version the one in force, so a canary that approved it
// again would be testing its own call rather than the pipeline's.
func (c *scriptCanary) approvedScript(t *testing.T, versionID string) scriptdomain.ScriptVersion {
	t.Helper()
	ctx := context.Background()
	version, err := c.script.GetScriptVersion(ctx, versionID)
	if err != nil {
		t.Fatalf("reading the approved script version: %v", err)
	}
	if version.Status != versioning.StatusApproved {
		t.Fatalf("the script version is %q, want approved", version.Status)
	}
	// "approved 唯一": a second approved version for the same script would make "the approved script"
	// ambiguous, and the schema's partial unique index is what prevents it.
	var count int
	if err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM script_versions WHERE script_id = ? AND status = 'approved'`,
		version.ScriptID).Scan(&count); err != nil {
		t.Fatalf("counting the approved scripts: %v", err)
	}
	if count != 1 {
		t.Fatalf("%d script versions are approved, want exactly one", count)
	}
	return version
}

// seedScriptEvents writes the story events a skeleton's selection names.
//
// The link table has no foreign key to `story_events` — a citation is provenance and outlives the row —
// so the SERVICE's reference check is what refuses an unknown id. Seeding real rows is what makes the
// canary's selection a real relation rather than a string that happens to be stored.
func (c *scriptCanary) seedStoryEvents(t *testing.T, count int) []string {
	t.Helper()
	ctx := context.Background()
	ids := make([]string, 0, count)
	for index := 1; index <= count; index++ {
		id := "canary-event-" + itoaForTest(index)
		if _, err := c.db.ExecContext(ctx, `INSERT INTO story_events
			(id, project_id, ordinal, name, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'accepted', ?, ?)`,
			id, c.ids.project, index, "Canary event "+itoaForTest(index), canaryStamp, canaryStamp); err != nil {
			t.Fatalf("seeding a story event: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// seedScriptVersionRow creates the script version row the generation stage fills.
//
// The row is created HERE rather than by the pipeline, and the reason is the pipeline's own contract: a
// structure needs a version to hang off, so the version write is a command of its own
// (`script.create_script_version`) and the content write takes the version's id. A stage that created the
// row itself would have to decide its status and its citations, which are the caller's facts.
func (c *scriptCanary) seedScriptVersionRow(t *testing.T, skeletonVersionID, strategyVersionID string) string {
	t.Helper()
	ctx := context.Background()
	scriptRecord, err := c.script.EnsureScript(ctx, c.ids.episode)
	if err != nil {
		t.Fatalf("ensuring the episode's script: %v", err)
	}
	version, err := c.script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
		ScriptID:                    scriptRecord.ID,
		StorySkeletonVersionID:      skeletonVersionID,
		AdaptationStrategyVersionID: strategyVersionID,
		Summary:                     "The canary's first script version.",
		CreatedByType:               versioning.CreatedByAgent,
		CreatedByID:                 "canary",
	})
	if err != nil {
		t.Fatalf("creating the script version row: %v", err)
	}
	return version.ID
}

// itoaForTest renders a small integer without importing strconv in a test file.
func itoaForTest(value int) string {
	if value <= 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// TestCanaryFixturesCoverSection181 asserts AGENT_CONTRACTS §18.1's list against the fixture directory.
//
// The section names eight items the canary corpus must contain, and three of them are WP-08's: the
// approved facts, the expected skeleton's key points, and a deliberately WRONG script. Asserting the
// list here is what keeps a fixture from being deleted while the requirement stands.
func TestCanaryFixturesCoverSection181(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "canary-drama")
	// The section's list, with which item is which file. The three WP-08 adds are marked.
	want := map[string]string{
		"source.md":                    "章节文本",
		"expected-chapters.json":       "章节期望（WP-06）",
		"approved-facts.json":          "已批准事实",
		"expected-story-skeleton.json": "期望故事骨架关键点",
		"bad-script.json":              "故意错误剧本",
	}
	for name, purpose := range want {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("the fixture %s (%s) is missing: %v", name, purpose, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("the fixture %s (%s) is empty", name, purpose)
		}
	}
	// The remaining items of §18.1's list are other work packages', and they are named here so a reader
	// can see which ones this list does NOT claim: 故意错误资产引用 and 分镜连续性错误 are WP-09/WP-11's,
	// Memory recall 问题是 WP-10's, and Prompt Injection 文本 exists as the extraction canary's own fixture.
}
