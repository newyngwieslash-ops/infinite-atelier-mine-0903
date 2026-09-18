// Package workflow owns the durable-workflow vocabulary, the stage and run
// state machines, and the review and user-decision records of DOMAIN_MODEL
// section 11, PRD FR-100, FR-110 and PRD section 12.2.
//
// The engine that drives the transitions, the quality gate and the automatic
// revision budget belongs to WP-07. What is here is the part the schema already
// fixes (migration 000011): which statuses exist, which moves between them are
// legal, what a review report and its issues must carry, and what a user
// decision must record. Nothing here decides when a stage advances.
//
// Vocabulary rulings, from the migration header and ADR-0007:
//
//   - StageRun statuses use the PRD FR-100 spellings. DOMAIN_MODEL section 11.2
//     lists the same states under the older names (ready, executed,
//     under_review); storing one state two ways would make every query guess
//     which spelling a row holds, so those three are deliberately NOT accepted.
//     The domain model's superseded is kept, because its own invariant
//     "passed 后不可改写，只能 supersede" cannot be expressed without it.
//   - UserGateDecision takes PRD section 12.2's set and adds waive, which
//     DOMAIN_MODEL section 15.3 requires whenever a stale artifact is kept.
//
// Go has no function overloading, so the two state machines cannot share one
// CanTransition name: the run machine's function keeps the bare name and the
// stage machine's is CanStageTransition. The same applies to IsTerminal and
// IsStageTerminal.
//
// The package performs no I/O and never mints an identifier (ADR-0005).
package workflow

import (
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// RunStatus is the lifecycle of one workflow run (PRD FR-100).
type RunStatus string

const (
	// RunPending is a run that exists and has not started.
	RunPending RunStatus = "pending"
	// RunRunning is a run whose active stage is executing.
	RunRunning RunStatus = "running"
	// RunWaitingUser is a run stopped at a user quality gate (PRD section 12.2).
	RunWaitingUser RunStatus = "waiting_user"
	// RunPaused is a run the user stopped and can resume.
	RunPaused RunStatus = "paused"
	// RunCompleted is a run whose final stage passed.
	RunCompleted RunStatus = "completed"
	// RunFailed is a run that cannot continue without user action.
	RunFailed RunStatus = "failed"
	// RunCancelled is a run the user abandoned. It is terminal.
	RunCancelled RunStatus = "cancelled"
)

// RunStatuses lists the documented run statuses in the schema's order.
var RunStatuses = []RunStatus{
	RunPending, RunRunning, RunWaitingUser, RunPaused, RunCompleted, RunFailed, RunCancelled,
}

// IsValidRunStatus reports whether a run status may be persisted.
func IsValidRunStatus(value RunStatus) bool {
	for _, candidate := range RunStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// CanTransition reports whether a workflow run may move between two statuses.
//
// The edges are the ones PRD FR-100's capabilities need, and the three terminal
// states are what make the run's outcome final:
//
//   - pending waits for a start command or a cancellation, and cannot fail
//     before it has run anything;
//   - running reaches waiting_user at a quality gate, paused on a user pause,
//     and the three outcomes. FR-100 requires "用户暂停/恢复" and "应用重启恢复",
//     so a pause has to survive a restart; migration 000011 stores it as a run
//     status value rather than as an in-process flag, and the domain models it
//     the same way;
//   - waiting_user returns to running for the decision section 12.2 describes
//     (approve, fix, redo, manual_edit and skip all lead on to more stage work or
//     to acceptance), or is paused or cancelled. This machine has no
//     waiting_user to completed edge: FR-100's "状态迁移不允许跳过未满足依赖的
//     阶段" means the decision is applied to the stage attempt first and the run
//     completes from running, once the guarded stage has reached its own end;
//   - paused resumes to running, is cancelled, or fails if the resume discovers
//     a broken prerequisite;
//   - completed, failed and cancelled are terminal. Retrying a failed run means
//     starting another one, because this machine keeps no edge back into a run
//     whose outcome is already recorded. AC-E2E-003 draws exactly that line
//     after a forced restart: a recoverable task continues, and one that cannot
//     recover "明确失败并可重试".
func CanTransition(from, to RunStatus) bool {
	if !IsValidRunStatus(from) || !IsValidRunStatus(to) {
		return false
	}
	if from == to {
		return false
	}
	switch from {
	case RunPending:
		switch to {
		case RunRunning, RunCancelled:
			return true
		}
	case RunRunning:
		switch to {
		case RunWaitingUser, RunPaused, RunCompleted, RunFailed, RunCancelled:
			return true
		}
	case RunWaitingUser:
		switch to {
		case RunRunning, RunPaused, RunCancelled:
			return true
		}
	case RunPaused:
		switch to {
		case RunRunning, RunCancelled, RunFailed:
			return true
		}
	case RunCompleted, RunFailed, RunCancelled:
		// Terminal: the run's outcome is recorded and history.
		return false
	}
	return false
}

// IsTerminal reports whether a run status admits no further transition.
//
// It is exactly "no edge leaves this status in CanTransition", so the two
// functions cannot disagree.
func IsTerminal(status RunStatus) bool {
	switch status {
	case RunCompleted, RunFailed, RunCancelled:
		return true
	default:
		return false
	}
}

// StageStatus is the lifecycle of one stage attempt (PRD FR-100).
type StageStatus string

const (
	// StagePending is an attempt that has not started executing.
	StagePending StageStatus = "pending"
	// StageRunning is an attempt whose execution is in flight.
	StageRunning StageStatus = "running"
	// StageExecutionSucceeded is an attempt whose structured output was
	// persisted and verified. PRD FR-100's name; DOMAIN_MODEL section 11.2 says
	// "executed" for the same state.
	StageExecutionSucceeded StageStatus = "execution_succeeded"
	// StageReviewing is an attempt a supervisor or ruleset is examining.
	StageReviewing StageStatus = "reviewing"
	// StagePassed is an attempt that satisfied its quality gate. Section 11.2:
	// "passed 后不可改写，只能 supersede".
	StagePassed StageStatus = "passed"
	// StageNeedsFix is an attempt the review wants revised in place, reusing the
	// unaffected and locked content (AGENT_CONTRACTS section 10.2 FIX).
	StageNeedsFix StageStatus = "needs_fix"
	// StageNeedsRedo is an attempt the review sends back to be regenerated from
	// the same upstream (AGENT_CONTRACTS section 10.2 REDO).
	StageNeedsRedo StageStatus = "needs_redo"
	// StageWaitingUser is an attempt parked at a user quality gate.
	StageWaitingUser StageStatus = "waiting_user"
	// StageFailed is an attempt that ended in an error.
	StageFailed StageStatus = "failed"
	// StageCancelled is an attempt the user abandoned. It is terminal.
	StageCancelled StageStatus = "cancelled"
	// StageSuperseded is a passed attempt that a newer attempt replaced. It is
	// the only status a passed attempt may move to.
	StageSuperseded StageStatus = "superseded"
)

// StageStatuses lists the documented stage statuses in the schema's order.
//
// This is the list migration 000011 stores, which is why TestStageStatusVocabularyMatchesMigration
// pins it against a second hardcoded copy.
var StageStatuses = []StageStatus{
	StagePending, StageRunning, StageExecutionSucceeded, StageReviewing, StagePassed,
	StageNeedsFix, StageNeedsRedo, StageWaitingUser, StageFailed, StageCancelled, StageSuperseded,
}

// IsValidStageStatus reports whether a stage status may be persisted.
func IsValidStageStatus(value StageStatus) bool {
	for _, candidate := range StageStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// CanStageTransition reports whether a stage attempt may move between two
// statuses.
//
// The edges follow PRD FR-100 and section 12.1's quality loop:
//
//   - pending starts or is cancelled;
//   - running reaches execution_succeeded once the stage's output is written and
//     read back, which section 11.2 makes the test of success ("Stage 成功以业务
//     写入和读取验证为准"). ARCHITECTURE section 10.1 also draws running straight
//     to the review state, and that edge is kept: it is what a stage whose
//     execution step is followed immediately by supervision looks like. Running
//     can also fail, be cancelled, or park at a user gate;
//   - execution_succeeded is the branch point AGENT_CONTRACTS section 10.2
//     describes: with a quality gate it goes to reviewing, and PRD FR-100's
//     default gate table has stages with supervision false (for example
//     asset_gap_analysis), which pass directly. It goes to failed when the
//     write-back verification that section 11.2 requires does not hold, because
//     then the stage did not in fact succeed;
//   - reviewing passes, asks for a fix or a redo, parks at a user gate, or
//     fails. Section 12.1's FIX and REDO arrows both continue as a new attempt;
//   - waiting_user is section 12.2's decision point: approve passes, fix and
//     redo land on their statuses, manual_edit re-runs the stage so the edited
//     version can be re-supervised (AGENT_CONTRACTS section 10.2 MANUAL_EDIT),
//     skip passes the attempt, and cancel cancels it. Section 12.2 writes skip
//     as "skipped with reason", but the migration's vocabulary has no skipped
//     status and this package does not invent one, so skip ends at passed and
//     its justification is the reason UserGateDecision.Validate requires;
//   - needs_fix and needs_redo return to running for the next attempt, which
//     AGENT_CONTRACTS section 10.2 has both decisions do ("创建新 Attempt/Version")
//     and which section 11.2 records as a new attempt row;
//   - passed can only be superseded. Section 11.2's "passed 后不可改写，只能
//     supersede" is why every other edge out of passed is absent;
//   - failed returns to pending for a retry, which is ARCHITECTURE section 10.1's
//     "failed -> ready", or is cancelled;
//   - cancelled and superseded are terminal.
func CanStageTransition(from, to StageStatus) bool {
	if !IsValidStageStatus(from) || !IsValidStageStatus(to) {
		return false
	}
	if from == to {
		return false
	}
	switch from {
	case StagePending:
		switch to {
		case StageRunning, StageCancelled:
			return true
		}
	case StageRunning:
		switch to {
		case StageExecutionSucceeded, StageReviewing, StageWaitingUser, StageFailed, StageCancelled:
			return true
		}
	case StageExecutionSucceeded:
		switch to {
		case StageReviewing, StageWaitingUser, StagePassed, StageFailed, StageCancelled:
			return true
		}
	case StageReviewing:
		switch to {
		case StagePassed, StageNeedsFix, StageNeedsRedo, StageWaitingUser, StageFailed, StageCancelled:
			return true
		}
	case StageWaitingUser:
		switch to {
		case StageRunning, StagePassed, StageNeedsFix, StageNeedsRedo, StageCancelled:
			return true
		}
	case StageNeedsFix, StageNeedsRedo:
		switch to {
		case StageRunning, StageCancelled:
			return true
		}
	case StagePassed:
		switch to {
		case StageSuperseded:
			return true
		}
	case StageFailed:
		// ARCHITECTURE section 10.1's "failed -> ready", plus cancellation.
		switch to {
		case StagePending, StageCancelled:
			return true
		}
	case StageCancelled, StageSuperseded:
		return false
	}
	return false
}

// IsStageTerminal reports whether a stage status admits no further transition.
//
// It is exactly "no edge leaves this status in CanStageTransition". passed is
// therefore NOT terminal: section 11.2 lets a passed attempt become superseded,
// and a passed attempt that could not would make the supersede invariant
// unenforceable. What "passed is final" means is that nothing else leaves it.
func IsStageTerminal(status StageStatus) bool {
	switch status {
	case StageCancelled, StageSuperseded:
		return true
	default:
		return false
	}
}

// StageName identifies one stage of a workflow run.
//
// The type is deliberately open. PRD FR-100's default quality gate and
// AGENT_CONTRACTS section 10.1 each list a set of stage keys and the two lists
// do not agree (FR-100 has script_generation and final_episode where section
// 10.1 has script_writing and final_review, among others), so pinning one list
// in the domain would decide a question that belongs to WP-07, which owns the
// gate. The schema agrees: stage_runs.stage is free text bounded to 120
// characters, with no value list. DocumentedStageNames below is the reference
// list, not a constraint.
type StageName string

// MaxStageNameLength is the schema's bound on a stage key.
const MaxStageNameLength = 120

// DocumentedStageNames lists PRD FR-100's ten default quality-gate keys.
//
// It is a REFERENCE list for documentation, error messages and the UI's default
// ordering. It is NOT a constraint: ValidateStageName accepts any non-empty
// name within MaxStageNameLength, because AGENT_CONTRACTS section 10.1 defines a
// different set for the same gate and choosing between them is WP-07's call.
var DocumentedStageNames = []string{
	"chapter_event_extraction",
	"story_skeleton",
	"adaptation_strategy",
	"script_generation",
	"asset_gap_analysis",
	"asset_generation",
	"storyboard_table",
	"storyboard_panel_generation",
	"video_generation",
	"final_episode",
}

// ValidateStageName checks a stage key against the schema's bounds only.
//
// Any non-empty name of at most MaxStageNameLength characters is accepted. See
// DocumentedStageNames for why the value set is not closed here.
func ValidateStageName(value StageName) error {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" {
		return InvalidError("A stage needs a name.")
	}
	if len([]rune(trimmed)) > MaxStageNameLength {
		return InvalidError("The stage name is too long.")
	}
	return nil
}

// IsValidStageName reports whether a stage key satisfies the bounds.
func IsValidStageName(value StageName) bool {
	return ValidateStageName(value) == nil
}

// MaxWorkflowTypeLength mirrors the schema's CHECK on workflow_runs.workflow_type.
const MaxWorkflowTypeLength = 120

// WorkflowRun is one durable workflow execution (DOMAIN_MODEL §11.1).
//
// Section 11.1's status is a run-level summary; the stage that is actually
// executing is named by current_stage and active_stage_run_id. Keeping both is
// what makes FR-100's "应用重启恢复" possible: on restart the run says which
// stage attempt was active, and the recovery path reads that attempt's own row.
type WorkflowRun struct {
	ID                string
	ProjectID         string
	EpisodeID         string
	WorkflowType      string
	CurrentStage      StageName
	Status            RunStatus
	ActiveStageRunID  string
	ConfigurationJSON string
	RetryCount        int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CompletedAt       time.Time
	Revision          int64
}

// Validate checks a workflow run before it is stored.
func (r WorkflowRun) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return InvalidError("A workflow run must belong to a project.")
	}
	workflowType := strings.TrimSpace(r.WorkflowType)
	if workflowType == "" {
		return InvalidError("A workflow run needs a workflow type.")
	}
	if len([]rune(workflowType)) > MaxWorkflowTypeLength {
		return InvalidError("The workflow type is too long.")
	}
	if !IsValidRunStatus(r.Status) {
		return InvalidError("The workflow run status is not recognised.")
	}
	// current_stage is empty before the first stage starts and after the last
	// one finishes, which the schema allows. A stage that is named must still
	// satisfy the same bounds as any other stage key.
	if strings.TrimSpace(string(r.CurrentStage)) != "" {
		if err := ValidateStageName(r.CurrentStage); err != nil {
			return err
		}
	}
	if r.RetryCount < 0 {
		return InvalidError("The retry count cannot be negative.")
	}
	if r.Revision < 1 {
		return InvalidError("A workflow run revision starts at one.")
	}
	return nil
}

// StageRun is one attempt at one stage of a workflow run (DOMAIN_MODEL §11.2).
//
// Section 11.2's invariants: "同一 Workflow 同阶段可有多次 attempt" and "最多一个
// active attempt". Attempts are historical rows, so the second invariant cannot
// be a plain unique index; migration 000011 states it is "enforced by the WP-07
// writer and reported by the section 19 integrity check", and IsActive below is
// the predicate both of those use.
type StageRun struct {
	ID                  string
	WorkflowRunID       string
	Stage               StageName
	Attempt             int
	ExecutionAgentKey   string
	Status              StageStatus
	InputJSON           string
	ValidatedOutputJSON string
	RawOutputFileID     string
	ErrorCode           string
	ErrorMessage        string
	CreatedAt           time.Time
	StartedAt           time.Time
	FinishedAt          time.Time
	Revision            int64
}

// Validate checks a stage attempt before it is stored.
func (r StageRun) Validate() error {
	if strings.TrimSpace(r.WorkflowRunID) == "" {
		return InvalidError("A stage run must belong to a workflow run.")
	}
	if err := ValidateStageName(r.Stage); err != nil {
		return err
	}
	if r.Attempt < 1 {
		return InvalidError("A stage attempt starts at one.")
	}
	if !IsValidStageStatus(r.Status) {
		return InvalidError("The stage status is not recognised.")
	}
	if r.Revision < 1 {
		return InvalidError("A stage run revision starts at one.")
	}
	return nil
}

// IsActive reports whether this attempt counts as the active one for its stage.
//
// Section 11.2 allows several attempts per stage and at most one active. An
// attempt is active while it is still in play: queued, executing, waiting for
// its review, or waiting for a user decision or a follow-up attempt. passed,
// failed, cancelled and superseded attempts are finished, and a finished
// attempt is what the next attempt replaces. The WP-07 writer applies this when
// it creates an attempt and the section 19 integrity check reports any stage
// that holds two active attempts at once.
func (r StageRun) IsActive() bool {
	switch r.Status {
	case StagePending, StageRunning, StageExecutionSucceeded, StageReviewing,
		StageWaitingUser, StageNeedsFix, StageNeedsRedo:
		return true
	default:
		return false
	}
}

// Severity is how serious a review finding is (AGENT_CONTRACTS §7.6).
//
// Both review_reports.severity and review_issues.severity use it, and section
// 7.6 attaches a rule to it: "passed=true 时 severity 不得为 major/critical",
// and "critical 强制人工门". The first is enforced by ReviewReport.Validate; the
// second is the gate's decision and belongs to WP-07.
type Severity string

const (
	SeverityNone     Severity = "none"
	SeverityMinor    Severity = "minor"
	SeverityMajor    Severity = "major"
	SeverityCritical Severity = "critical"
)

// Severities lists the documented severities in the order the contract names them.
var Severities = []Severity{SeverityNone, SeverityMinor, SeverityMajor, SeverityCritical}

// IsValidSeverity reports whether a severity may be persisted.
func IsValidSeverity(value Severity) bool {
	for _, candidate := range Severities {
		if candidate == value {
			return true
		}
	}
	return false
}

// Grade is the letter a review report assigns.
//
// PRD FR-110's example carries "grade": "C" next to "passed": false, so a report
// that did not pass still carries a letter. The set is A to F: AGENT_CONTRACTS
// section 7.6 lists A to D and this adds F so a failed report has a grade of its
// own instead of borrowing one from the passing range. Grade is optional, which
// is why the schema's column defaults to empty and Validate only checks a grade
// that is present.
type Grade string

const (
	GradeA Grade = "A"
	GradeB Grade = "B"
	GradeC Grade = "C"
	GradeD Grade = "D"
	GradeF Grade = "F"
)

// Grades lists the documented grades in descending order.
var Grades = []Grade{GradeA, GradeB, GradeC, GradeD, GradeF}

// IsValidGrade reports whether a grade may be persisted. The empty grade is not
// a value: it means the report carries no letter, and callers test for it
// before calling this.
func IsValidGrade(value Grade) bool {
	for _, candidate := range Grades {
		if candidate == value {
			return true
		}
	}
	return false
}

// ReviewReport is one review of one stage attempt (DOMAIN_MODEL §11.3).
//
// The migration comment describes it as "one report per review, from either a
// deterministic ruleset or a supervisor, and supervisor_key names which": one of
// supervisor_key and ruleset_version identifies the reviewer, and both are
// optional because a ruleset run names no supervisor key.
//
// PRD FR-110's per-issue field and autoFixable are NOT report fields. FR-110's
// JSON places them inside issues[], and migration 000011 stores them on
// review_issues, so they live on ReviewIssue.
type ReviewReport struct {
	ID             string
	StageRunID     string
	SupervisorKey  string
	RulesetVersion string
	// Score is nil when the reviewer produced no score. The schema's column is
	// nullable REAL, and a zero score is a real result (a report that failed
	// every check), so nil and 0 are different facts and the pointer is what
	// keeps them apart.
	Score             *float64
	Grade             Grade
	Passed            bool
	Severity          Severity
	RecommendedAction string
	Summary           string
	CreatedAt         time.Time
}

// Validate checks a review report before it is stored.
func (r ReviewReport) Validate() error {
	if strings.TrimSpace(r.StageRunID) == "" {
		return InvalidError("A review report must belong to a stage run.")
	}
	if r.Grade != "" && !IsValidGrade(r.Grade) {
		return InvalidError("The review grade is not recognised.")
	}
	if !IsValidSeverity(r.Severity) {
		return InvalidError("The review severity is not recognised.")
	}
	// AGENT_CONTRACTS section 7.6: "passed=true 时 severity 不得为 major/critical".
	// A report that passed while naming a major or critical problem contradicts
	// itself, and the gate that reads passed would let the problem through.
	if r.Passed && (r.Severity == SeverityMajor || r.Severity == SeverityCritical) {
		return InvalidError("A passing review cannot report a major or critical severity.")
	}
	return nil
}

// IssueStatus is what has happened to one review issue.
type IssueStatus string

const (
	IssueOpen     IssueStatus = "open"
	IssueAccepted IssueStatus = "accepted"
	IssueFixed    IssueStatus = "fixed"
	IssueWaived   IssueStatus = "waived"
)

// IssueStatuses lists the documented issue statuses in the schema's order.
var IssueStatuses = []IssueStatus{IssueOpen, IssueAccepted, IssueFixed, IssueWaived}

// IsValidIssueStatus reports whether an issue status may be persisted.
func IsValidIssueStatus(value IssueStatus) bool {
	for _, candidate := range IssueStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// ReviewIssue is one problem a review report found (DOMAIN_MODEL §11.4 plus
// PRD FR-110's report shape).
//
// Field and AutoFixable come from FR-110's JSON example, and the migration
// records why they are columns: they are what "报告问题可以在 UI 中跳转到实体"
// needs, because EntityType and EntityID reach the row while Field names the
// part of it that is wrong (FR-110's example is field "costumeVersionId" on
// entityId "shot_013"). Location is the human-readable path when there is no
// entity to point at.
type ReviewIssue struct {
	ID             string
	ReviewReportID string
	Rule           string
	Severity       Severity
	EntityType     string
	EntityID       string
	Location       string
	Field          string
	Problem        string
	Suggestion     string
	EvidenceJSON   string
	AutoFixable    bool
	Status         IssueStatus
	ResolvedBy     string
	ResolvedAt     time.Time
	CreatedAt      time.Time
}

// Validate checks a review issue before it is stored.
func (r ReviewIssue) Validate() error {
	if strings.TrimSpace(r.ReviewReportID) == "" {
		return InvalidError("A review issue must belong to a review report.")
	}
	if !IsValidSeverity(r.Severity) {
		return InvalidError("The issue severity is not recognised.")
	}
	if !IsValidIssueStatus(r.Status) {
		return InvalidError("The issue status is not recognised.")
	}
	// AGENT_CONTRACTS section 7.6: "issue entity 必须存在或 location 明确". An
	// issue the user cannot be sent to is not actionable, which is what FR-110's
	// "报告问题可以在 UI 中跳转到实体" asks for. A half-specified entity is
	// refused too, because a type without an id names no row.
	hasEntity := strings.TrimSpace(r.EntityType) != "" || strings.TrimSpace(r.EntityID) != ""
	if hasEntity {
		if strings.TrimSpace(r.EntityType) == "" || strings.TrimSpace(r.EntityID) == "" {
			return InvalidError("A review issue that names an entity must name both its type and its id.")
		}
	} else if strings.TrimSpace(r.Location) == "" {
		return InvalidError("A review issue must name the entity it is about or where the problem is.")
	}
	return nil
}

// GateDecision is what the user chose at a quality gate.
//
// The set is PRD section 12.2's six decisions plus waive, which DOMAIN_MODEL
// section 15.3 requires whenever a stale artifact is kept. DOMAIN_MODEL section
// 11.5 spells the first one "pass"; the schema and section 12.2 both say
// approve, and the stored value is the one migration 000011 accepts.
type GateDecision string

const (
	GateApprove    GateDecision = "approve"
	GateFix        GateDecision = "fix"
	GateRedo       GateDecision = "redo"
	GateManualEdit GateDecision = "manual_edit"
	GateSkip       GateDecision = "skip"
	GateCancel     GateDecision = "cancel"
	GateWaive      GateDecision = "waive"
)

// GateDecisions lists the documented decisions in the schema's order.
var GateDecisions = []GateDecision{
	GateApprove, GateFix, GateRedo, GateManualEdit, GateSkip, GateCancel, GateWaive,
}

// IsValidGateDecision reports whether a decision may be persisted.
func IsValidGateDecision(value GateDecision) bool {
	for _, candidate := range GateDecisions {
		if candidate == value {
			return true
		}
	}
	return false
}

// UserGateDecision is one user decision at a quality gate (DOMAIN_MODEL §11.5).
//
// Section 11.5's single sentence is the rule: "用户决策不可由 Agent 伪造". The
// migration echoes it ("is why created_by is stored and why the writer contract
// (WP-07) must come from a user command"), and Validate enforces the half the
// domain can decide, by refusing a decision attributed to an agent. The pair
// CreatedByType and CreatedByID follows section 2.5's producer convention; the
// schema keeps it in one created_by column.
//
// IssueIDsJSON and LockedEntityRefsJSON are JSON because FR-110's FIX carries
// "指定 Review Issue" and section 15.3's waiver carries locked references.
// Section 2.6 forbids JSON as a substitute for a queryable relation, so these
// are the sets a decision names, not a place to hide a reference.
type UserGateDecision struct {
	ID                   string
	WorkflowRunID        string
	StageRunID           string
	Decision             GateDecision
	IssueIDsJSON         string
	Instruction          string
	Reason               string
	LockedEntityRefsJSON string
	CreatedByType        versioning.CreatedByType
	CreatedByID          string
	CreatedAt            time.Time
}

// Validate checks a user decision before it is stored.
func (d UserGateDecision) Validate() error {
	if strings.TrimSpace(d.WorkflowRunID) == "" {
		return InvalidError("A user decision must belong to a workflow run.")
	}
	if !IsValidGateDecision(d.Decision) {
		return InvalidError("The gate decision is not recognised.")
	}
	if !versioning.IsValidCreatedByType(d.CreatedByType) {
		return InvalidError("The decision author is not recognised.")
	}
	// Section 11.5: "用户决策不可由 Agent 伪造". A decision an agent could write
	// is a decision the gate does not actually gate, so an agent-authored one is
	// refused here rather than at the writer.
	if d.CreatedByType == versioning.CreatedByAgent {
		return ConflictError("A user gate decision must come from a user command, not from an agent.")
	}
	// PRD FR-100: "跳过阶段需记录原因". A skip with no reason is an unexplained
	// hole in the audit trail, and it is the one decision whose whole content is
	// the justification for not doing the work.
	if d.Decision == GateSkip && strings.TrimSpace(d.Reason) == "" {
		return InvalidError("Skipping a stage must record a reason.")
	}
	// DOMAIN_MODEL section 15.3 requires a waiver to record why the stale
	// artifact was kept, and PRD FR-110's final review re-raises it.
	if d.Decision == GateWaive && strings.TrimSpace(d.Reason) == "" {
		return InvalidError("Waiving a requirement must record a reason.")
	}
	return nil
}

// EventType names what a workflow event records (PRD FR-100's event log).
//
// The type is open like StageName: FR-100 requires "每次状态变化写入审计事件"
// without fixing a value list, and the schema bounds the column to 120
// characters without constraining its values. Names are therefore decided by the
// code that writes the event.
type EventType string

// MaxEventTypeLength mirrors the schema's CHECK on workflow_events.event_type.
const MaxEventTypeLength = 120

// IsValidEventType reports whether an event type may be persisted. The schema
// requires at least one character, so the empty value is refused.
func IsValidEventType(value EventType) bool {
	trimmed := strings.TrimSpace(string(value))
	return trimmed != "" && len([]rune(trimmed)) <= MaxEventTypeLength
}

// WorkflowEvent is one audit record of a state change (PRD FR-100).
//
// DOMAIN_MODEL's aggregate list names WorkflowEvent without a field table, so the
// fields are what FR-100's requirement needs: "每次状态变化写入审计事件". Which
// run, which stage attempt, what changed, who caused it, and when.
//
// ActorType reuses the section 2.5 producer vocabulary, which is the same set the
// schema's actor_type CHECK accepts.
type WorkflowEvent struct {
	ID            string
	WorkflowRunID string
	StageRunID    string
	EventType     EventType
	FromStatus    string
	ToStatus      string
	PayloadJSON   string
	ActorType     versioning.CreatedByType
	ActorID       string
	CreatedAt     time.Time
}

// Validate checks a workflow event before it is stored.
func (e WorkflowEvent) Validate() error {
	if strings.TrimSpace(e.WorkflowRunID) == "" {
		return InvalidError("A workflow event must belong to a workflow run.")
	}
	if !IsValidEventType(e.EventType) {
		return InvalidError("The workflow event type is not recognised.")
	}
	if !versioning.IsValidCreatedByType(e.ActorType) {
		return InvalidError("The workflow event actor is not recognised.")
	}
	// The status columns carry a run status or a stage status, and which one is
	// decided by whether the event names a stage attempt. Recording anything
	// else would put a value in the audit log that no state machine can explain,
	// so a documented value from either vocabulary is accepted and the rest are
	// refused. Both are empty for an event that is not a status change.
	for _, status := range []string{e.FromStatus, e.ToStatus} {
		if strings.TrimSpace(status) == "" {
			continue
		}
		if !IsValidRunStatus(RunStatus(status)) && !IsValidStageStatus(StageStatus(status)) {
			return InvalidError("The workflow event status is not recognised.")
		}
	}
	return nil
}
