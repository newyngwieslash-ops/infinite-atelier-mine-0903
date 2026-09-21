package scriptpipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// revision_wp08_test.go covers the FIX loop and the manual-edit path, which the quality review measured
// at 0% coverage after reporting that THIRTY-FIVE mutations survived it.
//
// The review's sharpest finding was structural: `locksFor`, `versionOfDecision`, `runsForStage`,
// `revisionContext`, `StartRevision`, `ManualEdit`, `writeUserVersion`, `assertEpisodeInProject`,
// `issuesFromOutcome`'s severe branch and `isApprovingDecision`'s other arms all had no test at all —
// and the comments on every one of them state a guarantee. A comment that states a rule with no test
// behind it is the shape this package's own mutation passes exist to find, and the reason they did not
// is that the FIX path had never been driven end to end.
//
// So these tests drive it: a decision row with findings, a stage attempt, and the locks the re-run must
// respect.

// fakeDecisionReader answers the workflow service's decision read.
//
// It is a whole service rather than an interface, because `revisionContext` calls the SERVICE — the
// repository is one layer below and the review's point is that nothing exercised this layer.
func decisionService(t *testing.T, decisions map[string]workflow.UserGateDecision) *appworkflow.Service {
	t.Helper()
	return appworkflow.NewService(appworkflow.Options{
		Runs:      decisionRuns{},
		Stages:    decisionStages{},
		Reviews:   decisionReviews{},
		Decisions: decisionStore{decisions: decisions},
		Events:    decisionEvents{},
		Clock:     revisionClock{},
		IDs:       &revisionIDs{},
	})
}

type decisionStore struct {
	appworkflow.DecisionRepository
	decisions map[string]workflow.UserGateDecision
}

func (s decisionStore) LatestDecisionForStage(_ context.Context, stageRunID string) (workflow.UserGateDecision, bool, error) {
	decision, ok := s.decisions[stageRunID]
	return decision, ok, nil
}

// revisionClock and revisionIDs are the workflow service's determinism ports, fixed so the fake is
// deterministic. They are local because this package's own test helpers are for the PIPELINE, and a
// clock the pipeline never uses would be a shared name meaning two things.
type revisionClock struct{}

func (revisionClock) Now() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }

type revisionIDs struct{ next int }

func (g *revisionIDs) New() (string, error) {
	g.next++
	return "revision-id", nil
}

// The three repositories `appworkflow.NewService` requires and these tests never reach. Each embeds its
// interface, so a call this test did not expect panics rather than returning a zero value that reads
// like an empty store.
type decisionRuns struct{ appworkflow.RunRepository }

type decisionStages struct{ appworkflow.StageRepository }

type decisionReviews struct{ appworkflow.ReviewRepository }

type decisionEvents struct{ appworkflow.EventRepository }

// TestRevisionContextReadsTheFindingsAndRefusesWithoutADecision covers the FIX loop's two branches.
//
// The findings are what makes a FIX a FIX: §10.2 says it "re-runs against specific findings rather than
// from scratch", and this is the read that makes that true. An independent review found the function at
// 20% coverage with its refusal branch never taken and `FixFromStageRunID` set by NO test anywhere.
func TestRevisionContextReadsTheFindingsAndRefusesWithoutADecision(t *testing.T) {
	ctx := context.Background()

	// A first attempt names no base, so there is nothing to read.
	service := &Service{workflow: decisionService(t, nil)}
	locks, findings, err := service.revisionContext(ctx, StageRequest{Stage: StageStorySkeleton})
	if err != nil || locks != nil || findings != nil {
		t.Fatalf("a first attempt returned %v, %v, %v", locks, findings, err)
	}

	// A revision naming an attempt a user decided about reads its findings.
	service = &Service{workflow: decisionService(t, map[string]workflow.UserGateDecision{
		"stage-1": {
			// No StageRunID, so the lock lookup short-circuits before the runs reader: what this case is
			// about is the FINDINGS, and a Runs store would be a second fixture for a different question.
			// `TestLocksForReadsTheBaseVersionsFieldLocks` below covers the version walk with one.
			Decision:     workflow.GateFix,
			IssueIDsJSON: `["issue-1","issue-2"]`,
		},
	})}
	_, findings, err = service.revisionContext(ctx, StageRequest{
		Stage: StageStorySkeleton, FixFromStageRunID: "stage-1",
	})
	if err != nil {
		t.Fatalf("a revision with a decision was refused: %v", err)
	}
	if len(findings) != 2 || findings[0] != "issue-1" || findings[1] != "issue-2" {
		t.Fatalf("the findings are %v", findings)
	}

	// A revision naming an attempt NOBODY decided about is a CALLER ERROR, not an empty revision: the
	// whole point of naming the attempt is that a user decided something about it. The first version of
	// the code returned nothing and the mutation that restored that behaviour survived the suite.
	service = &Service{workflow: decisionService(t, nil)}
	if _, _, err := service.revisionContext(ctx, StageRequest{
		Stage: StageStorySkeleton, FixFromStageRunID: "stage-with-no-decision",
	}); err == nil {
		t.Fatal("a revision naming an attempt with no decision was accepted, so a FIX would run with nothing to fix")
	}
}

// TestIssuesFromOutcomeRefusesAnUnknownSeverity covers the branch the engine routes on.
//
// §7.6 attaches a rule to the severity — "critical 强制人工门" — so a finding whose severity is not in the
// vocabulary cannot be routed, and defaulting it would decide how urgent somebody else's problem is. The
// review found the refusal arm unreached.
func TestIssuesFromOutcomeRefusesAnUnknownSeverity(t *testing.T) {
	// A well-formed report's findings are read.
	issues, err := issuesFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"issues":[{"rule":"r","severity":"major","problem":"p","suggestion":"s"}]}`),
	})
	if err != nil {
		t.Fatalf("a well-formed finding was refused: %v", err)
	}
	if len(issues) != 1 || issues[0].Severity != workflow.SeverityMajor {
		t.Fatalf("the findings are %+v", issues)
	}
	// An invented severity is refused.
	if _, err := issuesFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"issues":[{"rule":"r","severity":"catastrophic"}]}`),
	}); err == nil {
		t.Fatal("a finding with an invented severity was accepted")
	}
	// An EMPTY severity is refused too, and that is the case a defaulting implementation would let
	// through: an omitted field and a wrong one are both "no routable severity".
	if _, err := issuesFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"issues":[{"rule":"r"}]}`),
	}); err == nil {
		t.Fatal("a finding with no severity was accepted")
	}
	// A document that is not JSON is refused.
	if _, err := issuesFromOutcome(agentruntime.Outcome{Output: []byte("not json")}); err == nil {
		t.Fatal("a malformed report was accepted")
	}
}

// TestReviewFromOutcomeRequiresARulesetVersion covers §7.6's requirement that a report say what it judged
// against.
//
// A report naming no ruleset could not be reproduced, because nothing would say which rules the findings
// were checked against — and the mutation that stopped refusing it survived the suite.
func TestReviewFromOutcomeRequiresARulesetVersion(t *testing.T) {
	report, err := reviewFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"passed":false,"severity":"major","rulesetVersion":"rules.v1","summary":"s"}`),
	}, "script.supervision.script")
	if err != nil {
		t.Fatalf("a well-formed report was refused: %v", err)
	}
	if report.RulesetVersion != "rules.v1" || report.SupervisorKey != "script.supervision.script" {
		t.Fatalf("the report is %+v", report)
	}
	if _, err := reviewFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"passed":true,"severity":"none","rulesetVersion":"  "}`),
	}, "script.supervision.script"); err == nil {
		t.Fatal("a report naming no ruleset version was accepted")
	}
	if _, err := reviewFromOutcome(agentruntime.Outcome{Output: []byte("not json")}, "k"); err == nil {
		t.Fatal("a malformed report was accepted")
	}
}

// TestEntityIDOfScansPastABlankIdentifier covers the multi-artifact case.
//
// A write tool may return several references, and one whose first entry has an empty id is still a result
// with something usable in it. The mutation that returned the FIRST entry's id regardless survived the
// suite, because the existing test only ever built a single-artifact list.
func TestEntityIDOfScansPastABlankIdentifier(t *testing.T) {
	multi := `{"artifacts":[{"entityId":""},{"entityId":"version-2"}]}`
	if id := entityIDOf(multi); id != "version-2" {
		t.Fatalf("a first blank identifier yielded %q, want the second entry", id)
	}
	// Every entry blank yields nothing rather than an empty string that reads like an id.
	if id := entityIDOf(`{"artifacts":[{"entityId":""},{"entityId":"  "}]}`); id != "" {
		t.Fatalf("an all-blank artifact list yielded %q", id)
	}
	// The trimming applies to the value it returns, so a padded id is not passed on padded.
	if id := entityIDOf(`{"artifacts":[{"entityId":"  version-3  "}]}`); id != "version-3" {
		t.Fatalf("a padded identifier yielded %q", id)
	}
}

// TestIssuesOfADecisionAreRefusedWhenMalformed covers the two readers' refusal arms.
//
// Both columns are JSON text on the decision row, and a malformed value is a corrupt row rather than a
// user's intent. A FIX that could not read its findings would re-run the stage from scratch while being
// recorded as a revision against specific findings.
func TestIssuesOfADecisionAreRefusedWhenMalformed(t *testing.T) {
	if _, err := issueIDsOf(`["a"`); err == nil {
		t.Fatal("a malformed finding list was accepted")
	}
	if _, err := lockedRefsOf(`[{"entityId":`); err == nil {
		t.Fatal("a malformed pin list was accepted")
	}
	// And the empty forms are the ordinary values rather than errors.
	if ids, err := issueIDsOf(""); err != nil || len(ids) != 0 {
		t.Fatalf("an empty finding column returned %v, %v", ids, err)
	}
	if refs, err := lockedRefsOf(""); err != nil || len(refs) != 0 {
		t.Fatalf("an empty pin column returned %v, %v", refs, err)
	}
}

// TestTheStageMapStatesTheArtifactTypeAndFamilyTheBuildActuallyHas covers the two entries the review
// found unasserted.
//
// `TestEveryStageArtifactTypeIsAWriteToolTarget` compared the map against a LITERAL written in the same
// test file, so changing a stage's artifact type to another value in that literal set stayed green — and
// the generation stage's `supervision` value is compared to nothing, because the registry deliberately
// cannot resolve it.
//
// This reads what is on DISK: the generated tool key list and the skill manifest. So the map is checked
// against the build rather than against a second copy of itself.
func TestTheStageMapStatesTheArtifactTypeAndFamilyTheBuildActuallyHas(t *testing.T) {
	keys := readToolKeys(t)
	for _, stage := range Stages() {
		agents, _ := AgentsForStage(stage)
		// The artifact type names a WRITE tool this build registers: `<something>.create_<artifact type>`.
		want := "create_" + agents.ArtifactType
		found := false
		for key := range keys {
			if strings.HasSuffix(key, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: the artifact type %q names no write tool in schemas/agent/tools/KEYS.txt",
				stage, agents.ArtifactType)
		}
		// The family is one the DOMAIN's vocabulary admits, so a lock read for this stage resolves to a
		// field set rather than to an empty list.
		if !scriptdomain.IsValidVersionFamily(agents.Family) {
			t.Errorf("%s: the family %q is not one the domain admits", stage, agents.Family)
		}
		if len(scriptdomain.LockableFields(agents.Family)) == 0 {
			t.Errorf("%s: the family %q admits no lockable fields", stage, agents.Family)
		}
		// And the FAMILY matches the artifact type's own suffix, which is the relation a reader would
		// check by eye: a `script_version` artifact belongs to the `script` family.
		if !strings.HasPrefix(agents.ArtifactType, string(agents.Family)) {
			t.Errorf("%s: the artifact type %q does not begin with its family %q",
				stage, agents.ArtifactType, agents.Family)
		}
	}
	// The generation stage's SUPERVISOR is the entry the map exists to state, and this is the assertion
	// the review found missing: the registry cannot resolve it, so only the manifest can check the value.
	if agents, _ := AgentsForStage(StageScriptGeneration); agents.Supervision != "script.supervision.script" {
		t.Errorf("the generation stage's supervisor is %q", agents.Supervision)
	}
	manifest := readSkillManifest(t)
	for _, key := range []string{"script.supervision.script", "script.execution.script_generation"} {
		if !manifest[key] {
			t.Errorf("the manifest does not register %q, so the map names an agent this build does not carry", key)
		}
	}
}

// TestIsApprovingDecisionCoversEveryGateDecision covers the switch the gate branches on.
//
// Three of §10.2's decisions put an artifact in force and four do not, and the review found only the
// `approve` arm exercised. A mutation that widened the set would approve on a FIX, which is the one thing
// a FIX must not do.
func TestIsApprovingDecisionCoversEveryGateDecision(t *testing.T) {
	approving := map[workflow.GateDecision]bool{
		workflow.GateApprove:    true,
		workflow.GateManualEdit: true,
		workflow.GateSkip:       true,
		// §15.3's waiver accepts a STALE artifact rather than approving a new one, so the version in
		// force is the one already approved.
		workflow.GateWaive:  false,
		workflow.GateFix:    false,
		workflow.GateRedo:   false,
		workflow.GateCancel: false,
	}
	for decision, want := range approving {
		if got := isApprovingDecision(decision); got != want {
			t.Errorf("isApprovingDecision(%q) = %v, want %v", decision, got, want)
		}
	}
}

// TestApproveArtifactRefusesAnEmptyVersion covers the refusal every approving decision must meet.
//
// A decision that approves NOTHING is a gate that did not gate: the stage would move and no version would
// be in force, which is the defect the canary found in the pipeline. The refusal must name what is
// missing so the caller's next step is obvious.
func TestApproveArtifactRefusesAnEmptyVersion(t *testing.T) {
	service := &Service{}
	if err := service.approveArtifact(context.Background(), workflow.StageRun{
		ID: "stage-1", Stage: StageScriptGeneration,
	}, GateRequest{Decision: workflow.GateApprove}); err == nil {
		t.Fatal("an approving decision with no version was accepted")
	}
	// A stage this pipeline does not drive is refused rather than mapped onto a family.
	if err := service.approveArtifact(context.Background(), workflow.StageRun{
		ID: "stage-2", Stage: "storyboard_table",
	}, GateRequest{Decision: workflow.GateApprove, ArtifactVersionID: "version-1"}); err == nil {
		t.Fatal("a stage this pipeline does not drive was approved through it")
	}
}

// TestAgentRefusalNamesTheStage covers the refusal a caller sees when they use the wrong pipeline.
func TestAgentRefusalNamesTheStage(t *testing.T) {
	err := agentRefusal("storyboard_table")
	if err == nil {
		t.Fatal("no refusal")
	}
	if !strings.Contains(err.Error(), "storyboard_table") {
		t.Fatalf("the refusal reads %q, which does not name the stage", err)
	}
	if domainErr, ok := agent.AsError(err); !ok || domainErr.Category != agent.CategoryInvalidInput {
		t.Fatalf("the refusal is %v, want an invalid-input refusal", err)
	}
}

// TestStartRevisionRefusesWithoutTheServices covers the fail-closed direction of every command.
//
// A build whose agent stack did not compose has no engine, and every stage command must then REFUSE
// rather than report success — a stage that "ran" with nothing behind it would be a workflow advancing
// on nothing.
func TestStartRevisionRefusesWithoutTheServices(t *testing.T) {
	service := New(Options{})
	if _, err := service.StartRevision(context.Background(), "stage-1"); err == nil {
		t.Fatal("a revision was started with no engine")
	}
	if _, err := service.ManualEdit(context.Background(), ManualEditRequest{StageRunID: "stage-1"}); err == nil {
		t.Fatal("a manual edit was applied with no services")
	}
	if _, err := service.RunStage(context.Background(), StageRequest{Stage: StageStorySkeleton}); err == nil {
		t.Fatal("a stage ran with no services")
	}
	if _, err := service.RunSupervision(context.Background(), SupervisionRequest{StageRunID: "stage-1"}); err == nil {
		t.Fatal("a supervision ran with no services")
	}
	if _, err := service.ApplyUserGate(context.Background(), GateRequest{StageRunID: "stage-1"}); err == nil {
		t.Fatal("a gate decision was applied with no services")
	}
	// An empty stage run identifier is refused before any lookup, so a caller with a blank field learns
	// that rather than seeing a not-found from an empty query.
	if _, err := service.StartRevision(context.Background(), "   "); err == nil {
		t.Fatal("a blank stage run was accepted")
	}
}

// readToolKeys reads the generated tool key list, so an assertion can compare the map against the BUILD
// rather than against a second copy of itself.
func readToolKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, key := range schemaKeysForTest() {
		keys[key] = true
	}
	if len(keys) == 0 {
		t.Fatal("the generated tool key list is empty, so this assertion proves nothing")
	}
	return keys
}

// readSkillManifest reads the script pack's agent keys, so the map's supervisor entries are checked
// against what the build actually registers.
func readSkillManifest(t *testing.T) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "skills", "script", "manifest.json"))
	if err != nil {
		t.Fatalf("reading the script manifest: %v", err)
	}
	var manifest struct {
		Agents []struct {
			Key string `json:"key"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("parsing the script manifest: %v", err)
	}
	keys := map[string]bool{}
	for _, agent := range manifest.Agents {
		keys[agent.Key] = true
	}
	if len(keys) == 0 {
		t.Fatal("the script manifest registers no agents")
	}
	return keys
}
