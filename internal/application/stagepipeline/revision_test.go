package stagepipeline

import (
	"context"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
	"time"
)

// revision_test.go covers the FIX read-back and the readers around it, which the WP-08 quality
// review measured at 0% coverage after THIRTY-FIVE mutations survived it.
//
// The finding was structural: `revisionContext`, `locksFor`, `versionOfDecision`, `runsForStage`
// and the readers' refusal arms all carried comments stating a rule and had no test at all. These
// moved here with the mechanism when WP-09 extracted it, so the rules stay covered for BOTH
// layers rather than only the one that happened to have the tests.

// decisionService builds a workflow service whose only useful read is the decision lookup.
//
// `decisions` is the map the store answers from, so a caller states what a stage attempt
// decided and nothing else this package's readers need.
func decisionService(decisions map[string]workflow.UserGateDecision) *appworkflow.Service {
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

// CreateDecision accepts any well-formed decision, which is what the gate writes.
func (s decisionStore) CreateDecision(_ context.Context, _ workflow.UserGateDecision) error {
	return nil
}

// The four repositories `appworkflow.NewService` requires and these tests never reach. Each embeds
// its interface, so a call this test did not expect panics rather than returning a zero value that
// reads like an empty store.
type decisionRuns struct{ appworkflow.RunRepository }

// GetRun fails, and that is the ordinary case here: section 17's UserGateDecided announcement
// skips a run it cannot read rather than failing a decision that is already stored.
func (decisionRuns) GetRun(_ context.Context, _ string) (workflow.WorkflowRun, error) {
	return workflow.WorkflowRun{}, workflow.NotFoundError()
}

type decisionStages struct {
	appworkflow.StageRepository
	stage workflow.StageRun
}

// decisionStages answers the stage lookup the mechanism's commands make.
//
// A stage ALWAYS belongs to a workflow run — the domain refuses a decision that names none, and
// the mechanism passes the attempt's run through — so the double states one rather than leaving a
// blank that would fail validation somewhere far from the cause.
func (s decisionStages) GetStage(_ context.Context, id string) (workflow.StageRun, error) {
	stage := s.stage
	if stage.ID == "" {
		stage.ID = id
	}
	if stage.WorkflowRunID == "" {
		stage.WorkflowRunID = "run-1"
	}
	return stage, nil
}

type decisionReviews struct{ appworkflow.ReviewRepository }

type decisionEvents struct{ appworkflow.EventRepository }

// testSkills satisfies the mechanism's skill source so `Available` is true in the gate test. It
// answers no document, which is all that test needs: it never runs an agent.
type testSkills struct{}

func (testSkills) SkillDocument(string) (string, bool)  { return "", false }
func (testSkills) SkillVersionOf(string) (string, bool) { return "", false }

// gateTransitioner is the smallest transitioner the engine's gate path accepts. It records the
// revision it was handed, which is the one assertion the gate test makes about it.
type gateTransitioner struct {
	stage     workflow.StageRun
	recorded  agentruntime.StageTransitionRequest
	callCount int
}

func (g *gateTransitioner) CreateStage(context.Context, agentruntime.StageCreationRequest) (workflow.StageRun, error) {
	return workflow.StageRun{}, workflow.StorageError("this double creates no stages", nil)
}

func (g *gateTransitioner) TransitionStage(_ context.Context, request agentruntime.StageTransitionRequest) (workflow.StageRun, error) {
	g.callCount++
	g.recorded = request
	if request.Revision != g.stage.Revision {
		return workflow.StageRun{}, workflow.ConflictError("This stage changed in another window. Reload it and try again.")
	}
	moved := g.stage
	moved.Status = request.Status
	moved.Revision++
	return moved, nil
}

func (g *gateTransitioner) ListStages(_ context.Context, workflowRunID string) ([]workflow.StageRun, error) {
	if g.stage.WorkflowRunID != workflowRunID {
		return nil, nil
	}
	return []workflow.StageRun{g.stage}, nil
}

func (g *gateTransitioner) GetRun(_ context.Context, _ string) (workflow.WorkflowRun, error) {
	return workflow.WorkflowRun{ID: "run-1", ProjectID: "project-1"}, nil
}

func (g *gateTransitioner) WorkflowRunOfStage(_ context.Context, _ string) (string, error) {
	return g.stage.WorkflowRunID, nil
}

// revisionClock and revisionIDs are the workflow service's determinism ports.
type revisionClock struct{}

func (revisionClock) Now() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }

type revisionIDs struct{ next int }

func (g *revisionIDs) New() (string, error) {
	g.next++
	return "revision-id", nil
}

// testLayer is the smallest Layer the mechanism accepts: it states nothing and refuses nothing,
// which is all the readers and the gate predicate need.
type testLayer struct{}

func (testLayer) Name() string                                         { return "testpipeline" }
func (testLayer) Stages() []Stage                                      { return []Stage{"a_stage"} }

// DependsOn declares nothing, which is the honest answer for a stub whose single stage has no
// predecessor — and it is why this file's tests are unaffected by the dependency gate: a stage that
// declares nothing is always admitted.
func (testLayer) DependsOn(Stage) []Stage { return nil }
func (testLayer) AgentsFor(Stage) (StageAgents, bool)                  { return StageAgents{ArtifactType: "x"}, true }
func (testLayer) StateFor(workflow.StageRun, StageRequest) string      { return "" }
func (testLayer) Approve(context.Context, Stage, string, string) error { return nil }
func (testLayer) Locks(context.Context, Stage, string) ([]agentruntime.LockedRef, error) {
	return nil, nil
}
func (testLayer) WriteVersion(context.Context, Stage, StageAgents, ManualEditRequest) (string, error) {
	return "version-1", nil
}

var _ Layer = testLayer{}

// TestRevisionContextReadsTheFindingsAndRefusesWithoutADecision covers the FIX loop's two branches.
//
// The findings are what makes a FIX a FIX: §10.2 says it "re-runs against specific findings rather than
// from scratch", and this is the read that makes that true. An independent review found the function at
// 20% coverage with its refusal branch never taken and `FixFromStageRunID` set by NO test anywhere.
func TestRevisionContextReadsTheFindingsAndRefusesWithoutADecision(t *testing.T) {
	ctx := context.Background()

	// A first attempt names no base, so there is nothing to read.
	service := &Service{workflow: decisionService(nil), layer: testLayer{}}
	locks, findings, err := service.revisionContext(ctx, Stage("story_skeleton"), "")
	if err != nil || locks != nil || findings != nil {
		t.Fatalf("a first attempt returned %v, %v, %v", locks, findings, err)
	}

	// A revision naming an attempt a user decided about reads its findings.
	service = &Service{workflow: decisionService(map[string]workflow.UserGateDecision{
		"stage-1": {
			// No StageRunID, so the lock lookup short-circuits before the runs reader: what this case is
			// about is the FINDINGS, and a Runs store would be a second fixture for a different question.
			// `TestLocksForReadsTheBaseVersionsFieldLocks` below covers the version walk with one.
			Decision:     workflow.GateFix,
			IssueIDsJSON: `["issue-1","issue-2"]`,
		},
	}), layer: testLayer{}}
	_, findings, err = service.revisionContext(ctx, Stage("story_skeleton"), "stage-1")
	if err != nil {
		t.Fatalf("a revision with a decision was refused: %v", err)
	}
	if len(findings) != 2 || findings[0] != "issue-1" || findings[1] != "issue-2" {
		t.Fatalf("the findings are %v", findings)
	}

	// A revision naming an attempt NOBODY decided about is a CALLER ERROR, not an empty revision: the
	// whole point of naming the attempt is that a user decided something about it. The first version of
	// the code returned nothing and the mutation that restored that behaviour survived the suite.
	service = &Service{workflow: decisionService(nil), layer: testLayer{}}
	if _, _, err := service.revisionContext(ctx, Stage("story_skeleton"), "stage-with-no-decision"); err == nil {
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
	issues, err := IssuesFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"issues":[{"rule":"r","severity":"major","problem":"p","suggestion":"s"}]}`),
	})
	if err != nil {
		t.Fatalf("a well-formed finding was refused: %v", err)
	}
	if len(issues) != 1 || issues[0].Severity != workflow.SeverityMajor {
		t.Fatalf("the findings are %+v", issues)
	}
	// An invented severity is refused.
	if _, err := IssuesFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"issues":[{"rule":"r","severity":"catastrophic"}]}`),
	}); err == nil {
		t.Fatal("a finding with an invented severity was accepted")
	}
	// An EMPTY severity is refused too, and that is the case a defaulting implementation would let
	// through: an omitted field and a wrong one are both "no routable severity".
	if _, err := IssuesFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"issues":[{"rule":"r"}]}`),
	}); err == nil {
		t.Fatal("a finding with no severity was accepted")
	}
	// A document that is not JSON is refused.
	if _, err := IssuesFromOutcome(agentruntime.Outcome{Output: []byte("not json")}); err == nil {
		t.Fatal("a malformed report was accepted")
	}
}

// TestReviewFromOutcomeRequiresARulesetVersion covers §7.6's requirement that a report say what it judged
// against.
//
// A report naming no ruleset could not be reproduced, because nothing would say which rules the findings
// were checked against — and the mutation that stopped refusing it survived the suite.
func TestReviewFromOutcomeRequiresARulesetVersion(t *testing.T) {
	report, err := ReviewFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"passed":false,"severity":"major","rulesetVersion":"rules.v1","summary":"s"}`),
	}, "script.supervision.script")
	if err != nil {
		t.Fatalf("a well-formed report was refused: %v", err)
	}
	if report.RulesetVersion != "rules.v1" || report.SupervisorKey != "script.supervision.script" {
		t.Fatalf("the report is %+v", report)
	}
	if _, err := ReviewFromOutcome(agentruntime.Outcome{
		Output: []byte(`{"passed":true,"severity":"none","rulesetVersion":"  "}`),
	}, "script.supervision.script"); err == nil {
		t.Fatal("a report naming no ruleset version was accepted")
	}
	if _, err := ReviewFromOutcome(agentruntime.Outcome{Output: []byte("not json")}, "k"); err == nil {
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
	if id := EntityIDOf(multi); id != "version-2" {
		t.Fatalf("a first blank identifier yielded %q, want the second entry", id)
	}
	// Every entry blank yields nothing rather than an empty string that reads like an id.
	if id := EntityIDOf(`{"artifacts":[{"entityId":""},{"entityId":"  "}]}`); id != "" {
		t.Fatalf("an all-blank artifact list yielded %q", id)
	}
	// The trimming applies to the value it returns, so a padded id is not passed on padded.
	if id := EntityIDOf(`{"artifacts":[{"entityId":"  version-3  "}]}`); id != "version-3" {
		t.Fatalf("a padded identifier yielded %q", id)
	}
}

// TestIssuesOfADecisionAreRefusedWhenMalformed covers the two readers' refusal arms.
//
// Both columns are JSON text on the decision row, and a malformed value is a corrupt row
// rather than a user's intent. A FIX that could not read its findings would re-run the
// stage from scratch while being recorded as a revision against specific findings.
func TestIssuesOfADecisionAreRefusedWhenMalformed(t *testing.T) {
	if _, err := IssueIDsOf(`["a"`); err == nil {
		t.Fatal("a malformed finding list was accepted")
	}
	if _, err := LockedRefsOf(`[{"entityId":`); err == nil {
		t.Fatal("a malformed pin list was accepted")
	}
	// And the empty forms are the ordinary values rather than errors.
	if ids, err := IssueIDsOf(""); err != nil || len(ids) != 0 {
		t.Fatalf("an empty finding column returned %v, %v", ids, err)
	}
	if refs, err := LockedRefsOf(""); err != nil || len(refs) != 0 {
		t.Fatalf("an empty pin column returned %v, %v", refs, err)
	}
	// A pin with no identifier is DROPPED rather than passed on: a reference the prompt
	// states but no tool can resolve would send the model looking for a row that is not there.
	refs, err := LockedRefsOf(`[{"entityType":"scene","entityId":"  "},{"entityType":"scene","entityId":"scene-1"}]`)
	if err != nil {
		t.Fatalf("a pin list with one blank identifier was refused: %v", err)
	}
	if len(refs) != 1 || refs[0].EntityID != "scene-1" {
		t.Fatalf("the pins are %+v, want only the one with an identifier", refs)
	}
}

// TestTheGateRefusesAnApprovingDecisionThatNamesNoVersion covers the refusal every
// approving decision must meet, and the ORDER that makes an approval recoverable.
//
// A decision that approves NOTHING is a gate that did not gate: the stage would move and no
// version would be in force, which is the defect the WP-08 canary found in the pipeline.
// The other half is the order — the version is approved BEFORE the stage moves, so a failed
// transition leaves an approved version and a stage a user can retry, rather than a stage
// that claims a success the project has nothing to build from.
//
// Both halves are the MECHANISM's, which is why they are asserted here rather than per
// layer: a layer cannot be asked to approve a version nobody named.
func TestTheGateRefusesAnApprovingDecisionThatNamesNoVersion(t *testing.T) {
	ctx := context.Background()
	layer := &recordingLayer{}
	transitioner := &gateTransitioner{stage: workflow.StageRun{
		ID: "stage-1", WorkflowRunID: "run-1", Stage: "a_stage", Attempt: 1,
		Status: workflow.StageWaitingUser, Revision: 3,
	}}
	service := gateService(layer, transitioner)

	// An approving decision naming no version is refused, and the refusal says what is
	// missing so the caller's next step is obvious.
	if _, err := service.ApplyUserGate(ctx, GateRequest{
		StageRunID: "stage-1", Decision: workflow.GateApprove,
	}); err == nil {
		t.Fatal("an approving decision with no version was accepted")
	}
	if len(layer.approved) != 0 || transitioner.callCount != 0 {
		t.Fatal("a refused decision still approved or moved something")
	}

	// A decision that approves nothing BY DESIGN is unaffected: a FIX moves the stage on its
	// findings rather than on an artifact, so it must not be made to name a version — and it
	// must not approve one either.
	if _, err := service.ApplyUserGate(ctx, GateRequest{
		StageRunID: "stage-1", Decision: workflow.GateFix,
	}); err != nil {
		t.Fatalf("a FIX was refused for naming no version: %v", err)
	}
	if transitioner.callCount != 1 || transitioner.recorded.Status != workflow.StageNeedsFix {
		t.Fatalf("the FIX moved the stage to %q in %d calls",
			transitioner.recorded.Status, transitioner.callCount)
	}
	if len(layer.approved) != 0 {
		t.Fatalf("a FIX approved %v", layer.approved)
	}
	// The engine moves the stage on the revision it READ, so the gate cannot conflict with a
	// decision row written by another command after the caller loaded the stage.
	if transitioner.recorded.Revision != 3 {
		t.Fatalf("the transition carried revision %d, want the one the engine read",
			transitioner.recorded.Revision)
	}

	// And an approval WITH a version approves it BEFORE the stage moves, which the layer
	// observes from inside the approval rather than after it.
	transitioner.stage.Status = workflow.StageWaitingUser
	transitioner.stage.Revision = 5
	transitioner.callCount = 0
	layer.onApprove = func() { layer.approvedBeforeTransition = transitioner.callCount == 0 }
	if _, err := service.ApplyUserGate(ctx, GateRequest{
		StageRunID: "stage-1", Decision: workflow.GateApprove, ArtifactVersionID: "version-1",
	}); err != nil {
		t.Fatalf("an approval naming a version was refused: %v", err)
	}
	if len(layer.approved) != 1 || layer.approved[0] != "version-1" {
		t.Fatalf("the approval reached the layer as %v", layer.approved)
	}
	if !layer.approvedBeforeTransition {
		t.Fatal("the stage moved before its version was approved, so the order is not recoverable")
	}
	if transitioner.recorded.Status != workflow.StagePassed {
		t.Fatalf("the approval moved the stage to %q", transitioner.recorded.Status)
	}
}

// gateService is the mechanism over doubles, with every dependency `Available` requires present.
//
// The runtime contributes nothing but its presence: the gate path runs no agent, and a test that
// needed a registry, a tool table and a model port to exercise the ORDER of two writes would be
// testing the runtime instead.
func gateService(layer Layer, transitioner *gateTransitioner) *Service {
	return &Service{
		engine: agentruntime.NewEngine(agentruntime.EngineOptions{
			Runtime: new(agentruntime.Runtime), Transitioner: transitioner,
		}),
		runtime:  new(agentruntime.Runtime),
		workflow: decisionService(nil),
		layer:    layer,
		runs:     &runReaderFake{},
		assembly: testSkills{},
	}
}

// recordingLayer is the mechanism's test layer with a memory: it records what the gate
// asked it to approve, and whether the transition had already happened by then.
type recordingLayer struct {
	testLayer
	approved                 []string
	approvedBeforeTransition bool
	// onApprove is called when an approval arrives, so a test can observe the transitioner's
	// state at that moment rather than after the fact.
	onApprove func()
}

func (l *recordingLayer) Approve(_ context.Context, _ Stage, versionID, _ string) error {
	l.approved = append(l.approved, versionID)
	if l.onApprove != nil {
		l.onApprove()
	}
	return nil
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
		if got := IsApprovingDecision(decision); got != want {
			t.Errorf("IsApprovingDecision(%q) = %v, want %v", decision, got, want)
		}
	}
}
