package workflow

import (
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// TestStageStatusVocabularyMatchesMigration is the vocabulary pin the migration
// header demands: Go and SQL must list exactly the same statuses, in the same
// order. A value Go accepts and SQL rejects fails at the insert, and a value SQL
// accepts and Go does not understand fails at the read, so the two lists are
// compared here against a third, hardcoded copy of migration 000011.
func TestStageStatusVocabularyMatchesMigration(t *testing.T) {
	migration := []string{
		"pending", "running", "execution_succeeded", "reviewing", "passed",
		"needs_fix", "needs_redo", "waiting_user", "failed", "cancelled", "superseded",
	}
	if len(StageStatuses) != len(migration) {
		t.Fatalf("the stage status set has %d entries, migration 000011 stores %d", len(StageStatuses), len(migration))
	}
	for index, want := range migration {
		if got := string(StageStatuses[index]); got != want {
			t.Fatalf("stage status %d is %q, the migration stores %q", index, got, want)
		}
		if !IsValidStageStatus(StageStatus(want)) {
			t.Fatalf("the migration's status %q is not recognised by the domain", want)
		}
	}
	// The three domain-model-only spellings must stay refused: storing one state
	// under two names is what the ruling exists to prevent.
	for _, rejected := range []string{"ready", "executed", "under_review"} {
		if IsValidStageStatus(StageStatus(rejected)) {
			t.Fatalf("DOMAIN_MODEL section 11.2's %q was accepted, but the migration does not store it", rejected)
		}
	}
	for _, rejected := range []string{"", "Pending", "PASSED", "completed", "skipped"} {
		if IsValidStageStatus(StageStatus(rejected)) {
			t.Fatalf("undocumented stage status %q is accepted", rejected)
		}
	}
}

// TestRunStatusVocabulary pins PRD FR-100's run status list.
func TestRunStatusVocabulary(t *testing.T) {
	documented := []string{"pending", "running", "waiting_user", "paused", "completed", "failed", "cancelled"}
	if len(RunStatuses) != len(documented) {
		t.Fatalf("the run status set has %d entries, FR-100 lists %d", len(RunStatuses), len(documented))
	}
	for index, want := range documented {
		if got := string(RunStatuses[index]); got != want {
			t.Fatalf("run status %d is %q, want %q", index, got, want)
		}
		if !IsValidRunStatus(RunStatus(want)) {
			t.Fatalf("documented run status %q is not recognised", want)
		}
	}
	for _, rejected := range []string{"", "Running", "queued", "retrying", "superseded"} {
		if IsValidRunStatus(RunStatus(rejected)) {
			t.Fatalf("undocumented run status %q is accepted", rejected)
		}
	}
}

// runEdges is every legal run transition, spelled out.
//
// Everything not listed must be refused, which is what makes the exhaustive
// check below a real test of the machine rather than of the table.
var runEdges = map[RunStatus][]RunStatus{
	RunPending:     {RunRunning, RunCancelled},
	RunRunning:     {RunWaitingUser, RunPaused, RunCompleted, RunFailed, RunCancelled},
	RunWaitingUser: {RunRunning, RunPaused, RunCancelled},
	RunPaused:      {RunRunning, RunCancelled, RunFailed},
	RunCompleted:   nil,
	RunFailed:      nil,
	RunCancelled:   nil,
}

// TestRunStateMachineIsExhaustive checks all 49 status pairs: every documented
// edge is allowed and every other pair is refused. A missing edge would strand a
// run, and an extra one would let it skip a gate.
func TestRunStateMachineIsExhaustive(t *testing.T) {
	for _, from := range RunStatuses {
		for _, to := range RunStatuses {
			want := false
			for _, allowed := range runEdges[from] {
				if allowed == to {
					want = true
					break
				}
			}
			if got := CanTransition(from, to); got != want {
				t.Fatalf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	// Undocumented values are refused in both directions.
	for _, status := range []RunStatus{"", "Running", "queued"} {
		if CanTransition(status, RunRunning) {
			t.Fatalf("an unrecognised run status %q was accepted as a source", status)
		}
		if CanTransition(RunRunning, status) {
			t.Fatalf("an unrecognised run status %q was accepted as a target", status)
		}
	}
}

// TestRunTerminality covers the three outcomes that end a run, and proves the
// predicate agrees with the machine rather than being a second opinion.
func TestRunTerminality(t *testing.T) {
	for _, status := range []RunStatus{RunCompleted, RunFailed, RunCancelled} {
		if !IsTerminal(status) {
			t.Fatalf("%s must be terminal", status)
		}
		for _, to := range RunStatuses {
			if CanTransition(status, to) {
				t.Fatalf("terminal %s has an edge to %s, so it is not terminal", status, to)
			}
		}
	}
	// Every non-terminal status has at least one way out, so no run is stranded.
	for _, status := range []RunStatus{RunPending, RunRunning, RunWaitingUser, RunPaused} {
		if IsTerminal(status) {
			t.Fatalf("%s must not be terminal", status)
		}
		exits := 0
		for _, to := range RunStatuses {
			if CanTransition(status, to) {
				exits++
			}
		}
		if exits == 0 {
			t.Fatalf("%s is not terminal but has no legal transition, so a run would be stuck", status)
		}
	}
}

// stageEdges is every legal stage transition, spelled out. As with runEdges,
// everything absent must be refused.
var stageEdges = map[StageStatus][]StageStatus{
	StagePending:            {StageRunning, StageCancelled},
	StageRunning:            {StageExecutionSucceeded, StageReviewing, StageWaitingUser, StageFailed, StageCancelled},
	StageExecutionSucceeded: {StageReviewing, StageWaitingUser, StagePassed, StageFailed, StageCancelled},
	StageReviewing:          {StagePassed, StageNeedsFix, StageNeedsRedo, StageWaitingUser, StageFailed, StageCancelled},
	StageWaitingUser:        {StageRunning, StagePassed, StageNeedsFix, StageNeedsRedo, StageCancelled},
	StageNeedsFix:           {StageRunning, StageCancelled},
	StageNeedsRedo:          {StageRunning, StageCancelled},
	StagePassed:             {StageSuperseded},
	StageFailed:             {StagePending, StageCancelled},
	StageCancelled:          nil,
	StageSuperseded:         nil,
}

// TestStageStateMachineIsExhaustive checks all 121 stage-status pairs.
//
// The two edges that matter most are the ones the vocabulary ruling preserves:
// passed to superseded must exist (section 11.2's "passed 后不可改写，只能
// supersede") and no other edge may leave passed.
func TestStageStateMachineIsExhaustive(t *testing.T) {
	for _, from := range StageStatuses {
		for _, to := range StageStatuses {
			want := false
			for _, allowed := range stageEdges[from] {
				if allowed == to {
					want = true
					break
				}
			}
			if got := CanStageTransition(from, to); got != want {
				t.Fatalf("CanStageTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	for _, status := range []StageStatus{"", "executed", "ready", "under_review"} {
		if CanStageTransition(status, StageRunning) {
			t.Fatalf("an unrecognised stage status %q was accepted as a source", status)
		}
		if CanStageTransition(StageRunning, status) {
			t.Fatalf("an unrecognised stage status %q was accepted as a target", status)
		}
	}
}

// TestPassedAttemptOnlyBecomesSuperseded is DOMAIN_MODEL section 11.2's
// "passed 后不可改写，只能 supersede". A passed attempt that could be edited or
// re-run in place would let an artifact change without the version history the
// section requires.
func TestPassedAttemptOnlyBecomesSuperseded(t *testing.T) {
	if !CanStageTransition(StagePassed, StageSuperseded) {
		t.Fatal("a passed attempt must be supersedable, or the invariant cannot be expressed")
	}
	for _, to := range StageStatuses {
		if to == StageSuperseded {
			continue
		}
		if CanStageTransition(StagePassed, to) {
			t.Fatalf("a passed attempt was allowed to become %s, which rewrites it", to)
		}
	}
}

// TestStageTerminality covers the two statuses nothing leaves. passed is
// deliberately not among them, because section 11.2 lets it become superseded.
func TestStageTerminality(t *testing.T) {
	if !IsStageTerminal(StageCancelled) {
		t.Fatal("a cancelled attempt must be terminal")
	}
	if !IsStageTerminal(StageSuperseded) {
		t.Fatal("a superseded attempt must be terminal")
	}
	if IsStageTerminal(StagePassed) {
		t.Fatal("a passed attempt is not terminal: it can still be superseded")
	}
	for _, status := range StageStatuses {
		terminal := IsStageTerminal(status)
		for _, to := range StageStatuses {
			if terminal && CanStageTransition(status, to) {
				t.Fatalf("%s is terminal but has an edge to %s", status, to)
			}
		}
	}
}

// TestStageNameIsOpenButBounded covers the ruling that the stage key list lives
// in WP-07, not here: any non-empty name within the schema's bound is accepted,
// including names neither specification lists.
func TestStageNameIsOpenButBounded(t *testing.T) {
	for _, name := range DocumentedStageNames {
		if err := ValidateStageName(StageName(name)); err != nil {
			t.Fatalf("FR-100's documented stage %q was rejected: %v", name, err)
		}
	}
	// ELEVEN, not FR-100's ten, and the difference is WP-09's: `director_plan` is
	// AGENT_CONTRACTS section 10.1's stage for an artifact PRD FR-060 requires, and
	// section 10.1 is the document that CONFIGURES it (supervision conditional, user
	// gate required). The reference list carries it so the policy lookup does not fall
	// through to the default for a stage the specification names.
	if len(DocumentedStageNames) != 11 {
		t.Fatalf("the reference list has %d stages, want FR-100's ten plus director_plan", len(DocumentedStageNames))
	}
	// AGENT_CONTRACTS section 10.1's OTHER spellings differ from FR-100's and must be
	// accepted too, because choosing between the two lists is not this
	// package's decision.
	for _, name := range []string{"event_extraction", "script_writing", "storyboard_image", "final_review"} {
		if err := ValidateStageName(StageName(name)); err != nil {
			t.Fatalf("AGENT_CONTRACTS section 10.1's stage %q was rejected: %v", name, err)
		}
	}
	// A name no specification mentions is still storable.
	if err := ValidateStageName("a_project_specific_stage"); err != nil {
		t.Fatalf("an open stage name must be accepted: %v", err)
	}
	// The reference list is documentation, not a constraint: it is not used to
	// decide validity, which this asserts by checking a name outside it passes.
	if !IsValidStageName("not_in_the_documented_list") {
		t.Fatal("the documented list must not be enforced as a closed vocabulary")
	}

	for _, bad := range []StageName{"", "   "} {
		if err := ValidateStageName(bad); err == nil {
			t.Fatalf("the empty stage name %q was accepted", bad)
		}
	}
	atLimit := StageName(strings.Repeat("s", MaxStageNameLength))
	if err := ValidateStageName(atLimit); err != nil {
		t.Fatalf("a stage name of exactly the maximum length must be accepted: %v", err)
	}
	overLimit := StageName(strings.Repeat("s", MaxStageNameLength+1))
	if err := ValidateStageName(overLimit); err == nil {
		t.Fatal("a stage name over the maximum length was accepted")
	}
	// The bound counts runes, not bytes, so a multi-byte name of legal length is
	// not refused for its encoding.
	multiByte := StageName(strings.Repeat("阶", MaxStageNameLength))
	if err := ValidateStageName(multiByte); err != nil {
		t.Fatalf("a multi-byte stage name at the limit must be accepted: %v", err)
	}
}

// TestWorkflowRunValidate covers §11.1's shape.
func TestWorkflowRunValidate(t *testing.T) {
	base := WorkflowRun{
		ProjectID:    "project-1",
		WorkflowType: "episode_production",
		Status:       RunPending,
		Revision:     1,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed workflow run was rejected: %v", err)
	}
	// A run that has not started names no current stage, which the schema allows.
	if base.CurrentStage != "" {
		t.Fatal("a fresh run must not claim a current stage")
	}
	withStage := base
	withStage.CurrentStage = "story_skeleton"
	if err := withStage.Validate(); err != nil {
		t.Fatalf("a run with a documented stage was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*WorkflowRun)
	}{
		{"no project", func(r *WorkflowRun) { r.ProjectID = "  " }},
		{"no workflow type", func(r *WorkflowRun) { r.WorkflowType = "" }},
		{"over-long workflow type", func(r *WorkflowRun) { r.WorkflowType = strings.Repeat("w", MaxWorkflowTypeLength+1) }},
		{"unknown status", func(r *WorkflowRun) { r.Status = "queued" }},
		{"over-long current stage", func(r *WorkflowRun) { r.CurrentStage = StageName(strings.Repeat("s", MaxStageNameLength+1)) }},
		{"negative retry count", func(r *WorkflowRun) { r.RetryCount = -1 }},
		{"zero revision", func(r *WorkflowRun) { r.Revision = 0 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			run := base
			testCase.mutate(&run)
			if err := run.Validate(); err == nil {
				t.Fatal("a malformed workflow run was accepted")
			}
		})
	}
}

// TestStageRunValidateAndActive covers §11.2's attempt rules.
func TestStageRunValidateAndActive(t *testing.T) {
	base := StageRun{
		WorkflowRunID: "run-1",
		Stage:         "story_skeleton",
		Attempt:       1,
		Status:        StagePending,
		Revision:      1,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed stage run was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StageRun)
	}{
		{"no workflow run", func(r *StageRun) { r.WorkflowRunID = "" }},
		{"no stage", func(r *StageRun) { r.Stage = "  " }},
		{"attempt zero", func(r *StageRun) { r.Attempt = 0 }},
		{"attempt negative", func(r *StageRun) { r.Attempt = -1 }},
		{"unknown status", func(r *StageRun) { r.Status = "executed" }},
		{"zero revision", func(r *StageRun) { r.Revision = 0 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			run := base
			testCase.mutate(&run)
			if err := run.Validate(); err == nil {
				t.Fatal("a malformed stage run was accepted")
			}
		})
	}

	// §11.2's "最多一个 active attempt": the predicate the WP-07 writer and the
	// section 19 check both use. The seven in-flight statuses are active and the
	// four finished ones are not.
	active := map[StageStatus]bool{
		StagePending:            true,
		StageRunning:            true,
		StageExecutionSucceeded: true,
		StageReviewing:          true,
		StageWaitingUser:        true,
		StageNeedsFix:           true,
		StageNeedsRedo:          true,
		StagePassed:             false,
		StageFailed:             false,
		StageCancelled:          false,
		StageSuperseded:         false,
	}
	for _, status := range StageStatuses {
		run := base
		run.Status = status
		if got := run.IsActive(); got != active[status] {
			t.Fatalf("IsActive() for %s = %v, want %v", status, got, active[status])
		}
	}
	// A finished attempt is not active even if it needs follow-up work, which is
	// what makes the next attempt the active one.
	for _, status := range []StageStatus{StagePassed, StageFailed, StageCancelled, StageSuperseded} {
		run := base
		run.Status = status
		if run.IsActive() {
			t.Fatalf("%s is finished and must not count as the active attempt", status)
		}
	}
}

// TestSeverityAndGradeVocabulary pins AGENT_CONTRACTS §7.6's severity set and
// the grade letters FR-110's example uses.
func TestSeverityAndGradeVocabulary(t *testing.T) {
	for _, severity := range []Severity{SeverityNone, SeverityMinor, SeverityMajor, SeverityCritical} {
		if !IsValidSeverity(severity) {
			t.Fatalf("documented severity %q is not recognised", severity)
		}
	}
	if len(Severities) != 4 {
		t.Fatalf("severity set has %d entries, the contract lists 4", len(Severities))
	}
	for _, severity := range []Severity{"", "None", "blocker", "fatal"} {
		if IsValidSeverity(severity) {
			t.Fatalf("undocumented severity %q is accepted", severity)
		}
	}
	for _, grade := range []Grade{GradeA, GradeB, GradeC, GradeD, GradeF} {
		if !IsValidGrade(grade) {
			t.Fatalf("documented grade %q is not recognised", grade)
		}
	}
	if len(Grades) != 5 {
		t.Fatalf("grade set has %d entries, want 5", len(Grades))
	}
	for _, grade := range []Grade{"", "a", "E", "G", "pass", "100"} {
		if IsValidGrade(grade) {
			t.Fatalf("undocumented grade %q is accepted", grade)
		}
	}
}

// TestReviewReportValidate covers §11.3 plus §7.6's passed/severity rule and the
// nullable score.
func TestReviewReportValidate(t *testing.T) {
	score := 76.0
	base := ReviewReport{
		StageRunID:     "sr-1",
		SupervisorKey:  "script_supervisor",
		RulesetVersion: "v1",
		Grade:          GradeC,
		Passed:         true,
		Severity:       SeverityNone,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed review report was rejected: %v", err)
	}

	// The score is a pointer because the column is nullable: absent and zero are
	// different facts and both must be storable.
	withoutScore := base
	if withoutScore.Score != nil {
		t.Fatal("a report with no score must carry a nil pointer")
	}
	if err := withoutScore.Validate(); err != nil {
		t.Fatalf("a report without a score must be valid: %v", err)
	}
	zero := base
	zero.Score = new(float64)
	if err := zero.Validate(); err != nil {
		t.Fatalf("a score of zero is a real result and must be valid: %v", err)
	}
	graded := base
	graded.Score = &score
	graded.Passed = false
	graded.Severity = SeverityMajor
	graded.Grade = GradeC
	if err := graded.Validate(); err != nil {
		t.Fatalf("FR-110's own example report must be valid: %v", err)
	}

	// §7.6: "passed=true 时 severity 不得为 major/critical".
	for _, severity := range []Severity{SeverityMajor, SeverityCritical} {
		report := base
		report.Severity = severity
		if err := report.Validate(); err == nil {
			t.Fatalf("a passing report with %s severity was accepted", severity)
		}
	}
	// The same severity on a failing report is exactly right.
	for _, severity := range []Severity{SeverityMajor, SeverityCritical} {
		report := base
		report.Passed = false
		report.Severity = severity
		if err := report.Validate(); err != nil {
			t.Fatalf("a failing report with %s severity must be valid: %v", severity, err)
		}
	}
	// An empty grade is allowed: reporting no letter is not the same as an
	// invalid one.
	ungraded := base
	ungraded.Grade = ""
	if err := ungraded.Validate(); err != nil {
		t.Fatalf("a report without a grade must be valid: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*ReviewReport)
	}{
		{"no stage run", func(r *ReviewReport) { r.StageRunID = "" }},
		{"unknown grade", func(r *ReviewReport) { r.Grade = "E" }},
		{"unknown severity", func(r *ReviewReport) { r.Severity = "blocker" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			report := base
			testCase.mutate(&report)
			if err := report.Validate(); err == nil {
				t.Fatal("a malformed review report was accepted")
			}
		})
	}
}

// TestReviewIssueValidate covers §11.4, FR-110's field and auto_fixable, and
// §7.6's "issue entity 必须存在或 location 明确".
func TestReviewIssueValidate(t *testing.T) {
	base := ReviewIssue{
		ReviewReportID: "report-1",
		Rule:           "CHARACTER_CONTINUITY",
		Severity:       SeverityMajor,
		EntityType:     "shot",
		EntityID:       "shot-013",
		Field:          "costumeVersionId",
		Problem:        "the costume does not match the previous shot",
		Suggestion:     "reference costume_version_004 instead",
		Status:         IssueOpen,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("FR-110's own example issue must be valid: %v", err)
	}
	// AutoFixable is a column rather than a rule: FR-110 carries it per issue
	// ("autoFixable": true in its example) and neither value is refused by
	// Validate, so both states are exercised here.
	for _, fixable := range []bool{true, false} {
		issue := base
		issue.AutoFixable = fixable
		if err := issue.Validate(); err != nil {
			t.Fatalf("an issue with auto_fixable=%v must be valid: %v", fixable, err)
		}
	}

	// A location is the alternative to an entity: §7.6 accepts either.
	byLocation := base
	byLocation.EntityType = ""
	byLocation.EntityID = ""
	byLocation.Location = "episode_1/scene_4/shot_3"
	if err := byLocation.Validate(); err != nil {
		t.Fatalf("an issue located by path must be valid: %v", err)
	}
	// An issue with neither cannot be navigated to and is refused.
	nowhere := base
	nowhere.EntityType = ""
	nowhere.EntityID = ""
	if err := nowhere.Validate(); err == nil {
		t.Fatal("an issue with neither an entity nor a location was accepted")
	}
	// A half-specified entity names no row, so the fragment is refused rather
	// than treated as a location.
	half := base
	half.EntityID = ""
	if err := half.Validate(); err == nil {
		t.Fatal("an issue naming an entity type without an id was accepted")
	}
	half = base
	half.EntityType = ""
	if err := half.Validate(); err == nil {
		t.Fatal("an issue naming an entity id without a type was accepted")
	}

	for _, status := range []IssueStatus{IssueOpen, IssueAccepted, IssueFixed, IssueWaived} {
		if !IsValidIssueStatus(status) {
			t.Fatalf("documented issue status %q is rejected", status)
		}
		issue := base
		issue.Status = status
		if err := issue.Validate(); err != nil {
			t.Fatalf("an issue in status %s must be valid: %v", status, err)
		}
	}
	if len(IssueStatuses) != 4 {
		t.Fatalf("issue status set has %d entries, the specification lists 4", len(IssueStatuses))
	}
	for _, status := range []IssueStatus{"", "resolved", "Open", "closed"} {
		if IsValidIssueStatus(status) {
			t.Fatalf("undocumented issue status %q is accepted", status)
		}
	}

	cases := []struct {
		name   string
		mutate func(*ReviewIssue)
	}{
		{"no report", func(i *ReviewIssue) { i.ReviewReportID = "" }},
		{"unknown severity", func(i *ReviewIssue) { i.Severity = "blocker" }},
		{"unknown status", func(i *ReviewIssue) { i.Status = "resolved" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			issue := base
			testCase.mutate(&issue)
			if err := issue.Validate(); err == nil {
				t.Fatal("a malformed review issue was accepted")
			}
		})
	}
}

// TestGateDecisionVocabulary pins PRD §12.2's six decisions plus §15.3's waive,
// and proves DOMAIN_MODEL §11.5's "pass" spelling is not accepted.
func TestGateDecisionVocabulary(t *testing.T) {
	documented := []string{"approve", "fix", "redo", "manual_edit", "skip", "cancel", "waive"}
	if len(GateDecisions) != len(documented) {
		t.Fatalf("decision set has %d entries, want %d", len(GateDecisions), len(documented))
	}
	for index, want := range documented {
		if got := string(GateDecisions[index]); got != want {
			t.Fatalf("decision %d is %q, want %q", index, got, want)
		}
		if !IsValidGateDecision(GateDecision(want)) {
			t.Fatalf("documented decision %q is rejected", want)
		}
	}
	for _, rejected := range []string{"", "pass", "Approve", "reject", "retry"} {
		if IsValidGateDecision(GateDecision(rejected)) {
			t.Fatalf("undocumented decision %q is accepted", rejected)
		}
	}
}

// TestUserGateDecisionRejectsAgentAuthors is DOMAIN_MODEL §11.5's "用户决策不可
// 由 Agent 伪造": a decision an agent could write is not a gate.
func TestUserGateDecisionRejectsAgentAuthors(t *testing.T) {
	base := UserGateDecision{
		WorkflowRunID: "run-1",
		StageRunID:    "sr-1",
		Decision:      GateApprove,
		CreatedByType: versioning.CreatedByUser,
		CreatedByID:   "user-1",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a user's decision must be valid: %v", err)
	}
	// Every non-agent producer is allowed: a migrated decision and a system
	// decision are not an agent forging one.
	for _, producer := range []versioning.CreatedByType{versioning.CreatedByUser, versioning.CreatedByMigration, versioning.CreatedBySystem} {
		decision := base
		decision.CreatedByType = producer
		if err := decision.Validate(); err != nil {
			t.Fatalf("a %s decision must be valid: %v", producer, err)
		}
	}
	agent := base
	agent.CreatedByType = versioning.CreatedByAgent
	err := agent.Validate()
	if err == nil {
		t.Fatal("an agent-authored gate decision was accepted")
	}
	domainErr, ok := AsError(err)
	if !ok {
		t.Fatalf("the refusal is not a domain error: %v", err)
	}
	if domainErr.Category != CategoryConflict {
		t.Fatalf("an agent-authored decision is a conflict, got category %q", domainErr.Category)
	}
	// The rule is about the author, not the decision: an agent cannot forge a
	// skip, a waiver or an approval either.
	for _, decision := range GateDecisions {
		forged := base
		forged.Decision = decision
		forged.Reason = "a reason"
		forged.CreatedByType = versioning.CreatedByAgent
		if err := forged.Validate(); err == nil {
			t.Fatalf("an agent forged the %s decision", decision)
		}
	}
	unknown := base
	unknown.CreatedByType = "robot"
	if err := unknown.Validate(); err == nil {
		t.Fatal("an unrecognised author was accepted")
	}
}

// TestUserGateDecisionRequiresReasonsForSkipAndWaive covers PRD FR-100's
// "跳过阶段需记录原因" and §15.3's waiver record.
func TestUserGateDecisionRequiresReasonsForSkipAndWaive(t *testing.T) {
	base := UserGateDecision{
		WorkflowRunID: "run-1",
		StageRunID:    "sr-1",
		CreatedByType: versioning.CreatedByUser,
		CreatedByID:   "user-1",
	}
	for _, decision := range []GateDecision{GateSkip, GateWaive} {
		without := base
		without.Decision = decision
		if err := without.Validate(); err == nil {
			t.Fatalf("the %s decision was accepted with no reason", decision)
		}
		blank := base
		blank.Decision = decision
		blank.Reason = "   "
		if err := blank.Validate(); err == nil {
			t.Fatalf("the %s decision was accepted with a blank reason", decision)
		}
		withReason := base
		withReason.Decision = decision
		withReason.Reason = "the shot is unusable and the episode ships without it"
		if err := withReason.Validate(); err != nil {
			t.Fatalf("the %s decision with a reason must be valid: %v", decision, err)
		}
	}
	// The other decisions need no reason, because they carry an instruction or
	// act on issue ids instead.
	for _, decision := range []GateDecision{GateApprove, GateFix, GateRedo, GateManualEdit, GateCancel} {
		value := base
		value.Decision = decision
		if err := value.Validate(); err != nil {
			t.Fatalf("the %s decision must be valid without a reason: %v", decision, err)
		}
	}

	cases := []struct {
		name   string
		mutate func(*UserGateDecision)
	}{
		{"no workflow run", func(d *UserGateDecision) { d.WorkflowRunID = "" }},
		{"unknown decision", func(d *UserGateDecision) { d.Decision = "pass" }},
		{"unknown author kind", func(d *UserGateDecision) { d.CreatedByType = "service" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decision := base
			decision.Decision = GateApprove
			testCase.mutate(&decision)
			if err := decision.Validate(); err == nil {
				t.Fatal("a malformed user decision was accepted")
			}
		})
	}
}

// TestWorkflowEventValidate covers FR-100's "每次状态变化写入审计事件", including
// the status columns that may hold either vocabulary.
func TestWorkflowEventValidate(t *testing.T) {
	base := WorkflowEvent{
		WorkflowRunID: "run-1",
		StageRunID:    "sr-1",
		EventType:     "stage_status_changed",
		FromStatus:    string(StageRunning),
		ToStatus:      string(StageExecutionSucceeded),
		ActorType:     versioning.CreatedBySystem,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed event was rejected: %v", err)
	}
	// A run-level event carries run statuses.
	runLevel := base
	runLevel.StageRunID = ""
	runLevel.FromStatus = string(RunPending)
	runLevel.ToStatus = string(RunRunning)
	if err := runLevel.Validate(); err != nil {
		t.Fatalf("a run-level event must be valid: %v", err)
	}
	// An event that is not a status change leaves both columns empty.
	other := base
	other.FromStatus = ""
	other.ToStatus = ""
	if err := other.Validate(); err != nil {
		t.Fatalf("an event with no status change must be valid: %v", err)
	}
	for _, actor := range []versioning.CreatedByType{versioning.CreatedByUser, versioning.CreatedByAgent, versioning.CreatedByMigration, versioning.CreatedBySystem} {
		event := base
		event.ActorType = actor
		if err := event.Validate(); err != nil {
			t.Fatalf("a %s event must be valid: %v", actor, err)
		}
	}

	if !IsValidEventType("stage_status_changed") {
		t.Fatal("an open event type must be accepted")
	}
	atLimit := EventType(strings.Repeat("e", MaxEventTypeLength))
	if !IsValidEventType(atLimit) {
		t.Fatal("an event type at the length limit must be accepted")
	}
	overLimit := EventType(strings.Repeat("e", MaxEventTypeLength+1))
	if IsValidEventType(overLimit) {
		t.Fatal("an event type over the length limit was accepted")
	}

	cases := []struct {
		name   string
		mutate func(*WorkflowEvent)
	}{
		{"no workflow run", func(e *WorkflowEvent) { e.WorkflowRunID = "" }},
		{"empty event type", func(e *WorkflowEvent) { e.EventType = "" }},
		{"blank event type", func(e *WorkflowEvent) { e.EventType = "  " }},
		{"over-long event type", func(e *WorkflowEvent) { e.EventType = overLimit }},
		{"unknown from status", func(e *WorkflowEvent) { e.FromStatus = "ready" }},
		{"unknown to status", func(e *WorkflowEvent) { e.ToStatus = "completed_successfully" }},
		{"unknown actor", func(e *WorkflowEvent) { e.ActorType = "robot" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			event := base
			testCase.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("a malformed workflow event was accepted")
			}
		})
	}
}
