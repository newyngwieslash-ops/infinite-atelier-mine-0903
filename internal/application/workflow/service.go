package workflow

import (
	"context"
	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	domainevent "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// Event types this package writes. PRD FR-100 fixes the requirement ("每次状态
// 变化写入审计事件") without fixing a value list, so the names are decided here
// and are stable identifiers the UI can group on.
const (
	// EventRunCreated records a run's creation, which is itself a state.
	EventRunCreated workflow.EventType = "workflow_run_created"
	// EventRunStatusChanged records a run status transition.
	EventRunStatusChanged workflow.EventType = "workflow_run_status_changed"
	// EventStageCreated records a stage attempt's creation.
	EventStageCreated workflow.EventType = "stage_run_created"
	// EventStageStatusChanged records a stage attempt status transition.
	EventStageStatusChanged workflow.EventType = "stage_run_status_changed"
)

// MaxBatchReviewIssues bounds the findings one review report may carry, so a
// malformed request cannot make a single call write an unbounded number of rows.
const MaxBatchReviewIssues = 2000

// maxReviewCategoryLength bounds a finding's category string.
//
// Sixty characters: FR-110's categories are single words, and the bound exists so a caller cannot
// store a paragraph in a column a UI shows as a tag. It is not a vocabulary check — that lives with
// the vocabulary, in `internal/domain/consistency`, and the application layer does not import it.
const maxReviewCategoryLength = 60

// NewService builds the workflow service.
func NewService(options Options) *Service {
	return &Service{
		runs:      options.Runs,
		stages:    options.Stages,
		reviews:   options.Reviews,
		decisions: options.Decisions,
		events:    options.Events,
		clock:     options.Clock,
		ids:       options.IDs,
		recorder:  options.Recorder,
	}
}

// Available reports whether the service has the dependencies it needs. An
// unattached binding fails closed rather than panicking.
// recordEvent announces something that happened, if the service has a recorder.
//
// The nil check is not defensive padding: the recorder is an interface, so a
// Service composed without one holds a nil interface and calling a method on it
// panics. This is the one place that check lives.
func (s *Service) recordEvent(ctx context.Context, draft eventsapp.Draft) {
	if s == nil || s.recorder == nil {
		return
	}
	s.recorder.RecordBestEffort(ctx, draft)
}

func (s *Service) Available() bool {
	return s != nil && s.runs != nil && s.stages != nil && s.reviews != nil && s.decisions != nil && s.events != nil && s.ids != nil
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// storageFailure is the fail-closed error for an unattached service.
func storageFailure() error {
	return workflow.StorageError("The workflow store is unavailable.", nil)
}

// actorOf returns the actor an event should record, defaulting to the local
// user.
//
// A state change is caused by someone, and the schema's actor_type has no empty
// value. A caller that names nobody is the local user acting in the UI, which is
// also the only producer that can drive a gate by hand.
func actorOf(requested Actor) Actor {
	if requested.Type == "" {
		return Actor{Type: versioning.CreatedByUser, ID: requested.ID}
	}
	return requested
}

// CreateRunRequest starts a workflow run.
type CreateRunRequest struct {
	ProjectID         string
	EpisodeID         string
	WorkflowType      string
	ConfigurationJSON string
	Actor             Actor
}

// CreateRun stores a new workflow run and the audit event recording it.
//
// The status starts at pending because nothing has executed yet, and the event
// is written by the same repository call as the run: PRD FR-100 requires an
// audit record of every state change, and a run whose creation left no trace
// would make the event log disagree with the runs it describes.
func (s *Service) CreateRun(ctx context.Context, request CreateRunRequest) (workflow.WorkflowRun, error) {
	if !s.Available() {
		return workflow.WorkflowRun{}, storageFailure()
	}
	id, err := s.ids.New()
	if err != nil {
		return workflow.WorkflowRun{}, storageFailure()
	}
	eventID, err := s.ids.New()
	if err != nil {
		return workflow.WorkflowRun{}, storageFailure()
	}
	now := s.now()
	record := workflow.WorkflowRun{
		ID:                id,
		ProjectID:         strings.TrimSpace(request.ProjectID),
		EpisodeID:         strings.TrimSpace(request.EpisodeID),
		WorkflowType:      strings.TrimSpace(request.WorkflowType),
		Status:            workflow.RunPending,
		ConfigurationJSON: request.ConfigurationJSON,
		CreatedAt:         now,
		UpdatedAt:         now,
		Revision:          1,
	}
	if err := record.Validate(); err != nil {
		return workflow.WorkflowRun{}, err
	}
	who := actorOf(request.Actor)
	event := workflow.WorkflowEvent{
		ID:            eventID,
		WorkflowRunID: record.ID,
		EventType:     EventRunCreated,
		ToStatus:      string(record.Status),
		ActorType:     who.Type,
		ActorID:       who.ID,
		CreatedAt:     now,
	}
	if err := event.Validate(); err != nil {
		return workflow.WorkflowRun{}, err
	}
	if err := s.runs.CreateRun(ctx, record, event); err != nil {
		return workflow.WorkflowRun{}, err
	}
	// Section 17's WorkflowStarted, which is a domain event and a different
	// thing from the workflow.WorkflowEvent written above: that one is the run's
	// own audit row and commits with the run, while this one is addressed to
	// whoever reads the project's stream. Best effort, because the run is
	// already committed either way.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          domainevent.WorkflowStarted,
		AggregateType: domainevent.AggregateWorkflow,
		AggregateID:   record.ID,
		ProjectID:     record.ProjectID,
	})
	return record, nil
}

// TransitionRunRequest moves a run to another status.
type TransitionRunRequest struct {
	RunID    string
	Status   workflow.RunStatus
	Revision int64
	Actor    Actor
}

// TransitionRun moves a run and records the transition as an event.
//
// The edge is decided against the status read from the store rather than one
// the caller supplies, because a caller's idea of the current status may be a
// reload out of date and the state machine is a property of the stored row. The
// write is guarded by the caller's revision, so a transition that raced another
// change is refused by the compare-and-swap rather than applied twice.
func (s *Service) TransitionRun(ctx context.Context, request TransitionRunRequest) (workflow.WorkflowRun, error) {
	if !s.Available() {
		return workflow.WorkflowRun{}, storageFailure()
	}
	if !workflow.IsValidRunStatus(request.Status) {
		return workflow.WorkflowRun{}, workflow.InvalidError("The workflow run status is not recognised.")
	}
	record, err := s.runs.GetRun(ctx, request.RunID)
	if err != nil {
		return workflow.WorkflowRun{}, err
	}
	if !workflow.CanTransition(record.Status, request.Status) {
		return workflow.WorkflowRun{}, workflow.ConflictError("The workflow run cannot move to that status from its current one.")
	}
	eventID, err := s.ids.New()
	if err != nil {
		return workflow.WorkflowRun{}, storageFailure()
	}
	now := s.now()
	from := record.Status
	record.Status = request.Status
	record.UpdatedAt = now
	// completed_at records when the run reached its final state, so a terminal
	// status stamps it. The state machine admits no edge out of a terminal
	// status, so the clearing branch is a defensive reset rather than a
	// transition this package can produce.
	if workflow.IsTerminal(record.Status) {
		record.CompletedAt = now
	} else {
		record.CompletedAt = time.Time{}
	}
	who := actorOf(request.Actor)
	event := workflow.WorkflowEvent{
		ID:            eventID,
		WorkflowRunID: record.ID,
		EventType:     EventRunStatusChanged,
		FromStatus:    string(from),
		ToStatus:      string(record.Status),
		ActorType:     who.Type,
		ActorID:       who.ID,
		CreatedAt:     now,
	}
	if err := event.Validate(); err != nil {
		return workflow.WorkflowRun{}, err
	}
	if err := s.runs.UpdateRun(ctx, record, request.Revision, event); err != nil {
		return workflow.WorkflowRun{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// CreateStageRequest adds a stage attempt to a run.
type CreateStageRequest struct {
	WorkflowRunID string
	Stage         workflow.StageName
	Attempt       int
	ExecutionKey  string
	InputJSON     string
	Actor         Actor
}

// CreateStage stores a new stage attempt and the event recording it.
//
// The stage key is validated by the domain's ValidateStageName, which checks
// the schema's bounds only: the value set is deliberately open because PRD
// FR-100 and AGENT_CONTRACTS section 10.1 give two different key lists and
// choosing between them is WP-07's call, not this layer's.
func (s *Service) CreateStage(ctx context.Context, request CreateStageRequest) (workflow.StageRun, error) {
	if !s.Available() {
		return workflow.StageRun{}, storageFailure()
	}
	if err := workflow.ValidateStageName(request.Stage); err != nil {
		return workflow.StageRun{}, err
	}
	if _, err := s.runs.GetRun(ctx, request.WorkflowRunID); err != nil {
		return workflow.StageRun{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return workflow.StageRun{}, storageFailure()
	}
	eventID, err := s.ids.New()
	if err != nil {
		return workflow.StageRun{}, storageFailure()
	}
	now := s.now()
	record := workflow.StageRun{
		ID:                id,
		WorkflowRunID:     request.WorkflowRunID,
		Stage:             workflow.StageName(strings.TrimSpace(string(request.Stage))),
		Attempt:           request.Attempt,
		ExecutionAgentKey: request.ExecutionKey,
		Status:            workflow.StagePending,
		InputJSON:         request.InputJSON,
		CreatedAt:         now,
		Revision:          1,
	}
	if err := record.Validate(); err != nil {
		return workflow.StageRun{}, err
	}
	who := actorOf(request.Actor)
	event := workflow.WorkflowEvent{
		ID:            eventID,
		WorkflowRunID: record.WorkflowRunID,
		StageRunID:    record.ID,
		EventType:     EventStageCreated,
		ToStatus:      string(record.Status),
		ActorType:     who.Type,
		ActorID:       who.ID,
		CreatedAt:     now,
	}
	if err := event.Validate(); err != nil {
		return workflow.StageRun{}, err
	}
	if err := s.stages.CreateStage(ctx, record, event); err != nil {
		return workflow.StageRun{}, err
	}
	return record, nil
}

// TransitionStageRequest moves a stage attempt to another status.
type TransitionStageRequest struct {
	StageRunID string
	Status     workflow.StageStatus
	Revision   int64
	Actor      Actor
}

// TransitionStage moves a stage attempt and records the transition as an event.
//
// Same shape as TransitionRun: the edge is decided against the stored status,
// the write is revision-guarded, and the event commits with the change. Whether
// a stage *should* advance is WP-07's decision; this method only refuses an
// edge the domain's machine does not allow.
func (s *Service) TransitionStage(ctx context.Context, request TransitionStageRequest) (workflow.StageRun, error) {
	if !s.Available() {
		return workflow.StageRun{}, storageFailure()
	}
	if !workflow.IsValidStageStatus(request.Status) {
		return workflow.StageRun{}, workflow.InvalidError("The stage status is not recognised.")
	}
	record, err := s.stages.GetStage(ctx, request.StageRunID)
	if err != nil {
		return workflow.StageRun{}, err
	}
	if !workflow.CanStageTransition(record.Status, request.Status) {
		return workflow.StageRun{}, workflow.ConflictError("The stage attempt cannot move to that status from its current one.")
	}
	eventID, err := s.ids.New()
	if err != nil {
		return workflow.StageRun{}, storageFailure()
	}
	now := s.now()
	from := record.Status
	record.Status = request.Status
	// started_at records when the attempt first began executing, so it is
	// stamped once. finished_at records when it stopped being in play, which is
	// exactly IsStageTerminal.
	if record.Status == workflow.StageRunning && record.StartedAt.IsZero() {
		record.StartedAt = now
	}
	if workflow.IsStageTerminal(record.Status) {
		record.FinishedAt = now
	}
	who := actorOf(request.Actor)
	event := workflow.WorkflowEvent{
		ID:            eventID,
		WorkflowRunID: record.WorkflowRunID,
		StageRunID:    record.ID,
		EventType:     EventStageStatusChanged,
		FromStatus:    string(from),
		ToStatus:      string(record.Status),
		ActorType:     who.Type,
		ActorID:       who.ID,
		CreatedAt:     now,
	}
	if err := event.Validate(); err != nil {
		return workflow.StageRun{}, err
	}
	if err := s.stages.UpdateStage(ctx, record, request.Revision, event); err != nil {
		return workflow.StageRun{}, err
	}
	record.Revision = request.Revision + 1
	// Section 17's WorkflowStageChanged. Best effort: the transition and its
	// workflow audit row are committed, so the caller must not be told the
	// command failed because the stream's copy did not land. A stage run names
	// its workflow run rather than a project, so the project is resolved through
	// it; a failed lookup skips the announcement rather than failing the command.
	if run, runErr := s.runs.GetRun(ctx, record.WorkflowRunID); runErr == nil {
		s.recordEvent(ctx, eventsapp.Draft{
			Type:          domainevent.WorkflowStageChanged,
			AggregateType: domainevent.AggregateWorkflow,
			AggregateID:   record.ID,
			ProjectID:     run.ProjectID,
		})
	}
	return record, nil
}

// IssueSource marks which half of a review produced a finding.
//
// AGENT_CONTRACTS section 11.4 requires the mark: "ReviewReport 合并两类证据，并标记
// source=deterministic|llm". It matters because the two kinds of claim have different force — a
// deterministic finding is a computation over stored rows, and a supervisor's is a reading — and a
// user deciding what to do about one needs to know which they are looking at.
type IssueSource string

const (
	// IssueSourceLLM is a finding a supervisor reported.
	IssueSourceLLM IssueSource = "llm"
	// IssueSourceDeterministic is a finding a code rule established.
	IssueSourceDeterministic IssueSource = "deterministic"
)

// IssueSources lists the documented marks in the schema's order.
var IssueSources = []IssueSource{IssueSourceLLM, IssueSourceDeterministic}

// IsValidIssueSource reports whether a mark may be persisted.
func IsValidIssueSource(value IssueSource) bool {
	for _, candidate := range IssueSources {
		if candidate == value {
			return true
		}
	}
	return false
}

// ReviewIssueInput is one finding a reviewer reported.
type ReviewIssueInput struct {
	Rule         string
	Severity     workflow.Severity
	EntityType   string
	EntityID     string
	Location     string
	Field        string
	Problem      string
	Suggestion   string
	EvidenceJSON string
	AutoFixable  bool
	// Source marks which half of the review the finding came from. An empty value is read as the
	// supervisor's, which is what every finding written before WP-10 was — the column's own default,
	// set so an old row keeps exactly the meaning it had.
	Source IssueSource
	// Category is FR-110's quality-rule classification, stated by a DETERMINISTIC ruleset and left
	// empty by a supervisor, which does not classify its own findings.
	//
	// It travels as a plain string because the vocabulary lives in `internal/domain/consistency`,
	// which this package must not import: the domain's rules are a peer of this one, not a dependency
	// of it. The value is checked against that vocabulary by the adapter that owns the mapping, and
	// what this layer enforces is the shape — a category long enough to be a paragraph is refused
	// here, and a category that is not in the vocabulary is refused there.
	Category string
}

// RecordReviewRequest stores one review report and its findings.
type RecordReviewRequest struct {
	StageRunID     string
	SupervisorKey  string
	RulesetVersion string
	// Score is nil when the reviewer produced no score. A zero score is a real
	// result, so the pointer keeps "did not score" apart from "scored zero".
	Score             *float64
	Grade             workflow.Grade
	Passed            bool
	Severity          workflow.Severity
	RecommendedAction string
	Summary           string
	Issues            []ReviewIssueInput
}

// RecordReview stores a report and its findings.
//
// The report and its issues are written by one repository call, so a report is
// never stored without the findings it is about (PRD FR-110's report shape puts
// the issues inside the report). Identifiers are minted here, not by the store
// (ADR-0005), and the domain's Validate refuses the one combination the
// specification singles out: a report that passed while naming a major or
// critical severity (AGENT_CONTRACTS section 7.6).
func (s *Service) RecordReview(ctx context.Context, request RecordReviewRequest) (workflow.ReviewReport, []workflow.ReviewIssue, error) {
	if !s.Available() {
		return workflow.ReviewReport{}, nil, storageFailure()
	}
	if len(request.Issues) > MaxBatchReviewIssues {
		return workflow.ReviewReport{}, nil, workflow.InvalidError("Too many findings in one review report.")
	}
	if _, err := s.stages.GetStage(ctx, request.StageRunID); err != nil {
		return workflow.ReviewReport{}, nil, err
	}
	reportID, err := s.ids.New()
	if err != nil {
		return workflow.ReviewReport{}, nil, storageFailure()
	}
	now := s.now()
	report := workflow.ReviewReport{
		ID:                reportID,
		StageRunID:        request.StageRunID,
		SupervisorKey:     request.SupervisorKey,
		RulesetVersion:    request.RulesetVersion,
		Score:             request.Score,
		Grade:             request.Grade,
		Passed:            request.Passed,
		Severity:          request.Severity,
		RecommendedAction: request.RecommendedAction,
		Summary:           request.Summary,
		CreatedAt:         now,
	}
	if err := report.Validate(); err != nil {
		return workflow.ReviewReport{}, nil, err
	}
	issues := make([]workflow.ReviewIssue, 0, len(request.Issues))
	for _, input := range request.Issues {
		issueID, idErr := s.ids.New()
		if idErr != nil {
			return workflow.ReviewReport{}, nil, storageFailure()
		}
		// The mark is defaulted rather than required, because an empty one means "a supervisor
		// reported it" and that is what every finding written before this column existed was. A
		// caller that states a value it invented is refused, so the mark stays a closed vocabulary.
		source := input.Source
		if strings.TrimSpace(string(source)) == "" {
			source = IssueSourceLLM
		}
		if !IsValidIssueSource(source) {
			return workflow.ReviewReport{}, nil, workflow.InvalidError("The finding's source is not recognised.")
		}
		// The category is TRIMMED and length-bounded but not validated as vocabulary here, and the
		// division is deliberate: this layer must not import the domain package that owns the list,
		// and a second copy of ten constants is a second list to keep in step. What a reader gets
		// instead is the vocabulary test in `internal/application/consistency`, which asserts every
		// rule this build ships has a stated category — the answer a wrong string here would have to
		// get past to reach a row.
		category := strings.TrimSpace(input.Category)
		if len(category) > maxReviewCategoryLength {
			return workflow.ReviewReport{}, nil, workflow.InvalidError("The finding's category is too long.")
		}
		issue := workflow.ReviewIssue{
			ID:             issueID,
			ReviewReportID: report.ID,
			Rule:           input.Rule,
			Severity:       input.Severity,
			EntityType:     strings.TrimSpace(input.EntityType),
			EntityID:       strings.TrimSpace(input.EntityID),
			Location:       input.Location,
			Field:          input.Field,
			Problem:        input.Problem,
			Suggestion:     input.Suggestion,
			EvidenceJSON:   input.EvidenceJSON,
			AutoFixable:    input.AutoFixable,
			Source:         string(source),
			Category:       category,
			Status:         workflow.IssueOpen,
			CreatedAt:      now,
		}
		if err := issue.Validate(); err != nil {
			return workflow.ReviewReport{}, nil, err
		}
		issues = append(issues, issue)
	}
	if err := s.reviews.CreateReport(ctx, report, issues); err != nil {
		return workflow.ReviewReport{}, nil, err
	}
	// Section 17's ReviewReportCreated. The report names its stage run, and the
	// project comes from that run's workflow; a failed lookup skips the
	// announcement rather than failing a report that is already stored.
	projectID := s.projectOfStage(ctx, report.StageRunID)
	if projectID != "" {
		s.recordEvent(ctx, eventsapp.Draft{
			Type:          domainevent.ReviewReportCreated,
			AggregateType: domainevent.AggregateReview,
			AggregateID:   report.ID,
			ProjectID:     projectID,
		})
	}
	return report, issues, nil
}

// projectOfStage resolves the project a stage run belongs to, through its
// workflow run. It returns "" when either lookup misses, which the callers treat
// as "skip the announcement" rather than as a failure.
func (s *Service) projectOfStage(ctx context.Context, stageRunID string) string {
	if stageRunID == "" {
		return ""
	}
	stage, err := s.stages.GetStage(ctx, stageRunID)
	if err != nil {
		return ""
	}
	run, err := s.runs.GetRun(ctx, stage.WorkflowRunID)
	if err != nil {
		return ""
	}
	return run.ProjectID
}

// SubmitGateDecisionRequest records what the user chose at a quality gate.
type SubmitGateDecisionRequest struct {
	WorkflowRunID        string
	StageRunID           string
	Decision             workflow.GateDecision
	IssueIDsJSON         string
	Instruction          string
	Reason               string
	LockedEntityRefsJSON string
	// CreatedBy is the decision's author. It must not be an agent: section 11.5
	// says "用户决策不可由 Agent 伪造", and the domain refuses that value. The
	// field is passed through unchanged so the refusal happens in the domain's
	// own rule rather than in a copy of it here.
	CreatedBy   CreatedByType
	CreatedByID string
}

// LatestDecisionForStage returns the newest decision recorded for one stage attempt.
//
// It is the READ the FIX loop needs, and it exists because the decision row IS the instruction: section
// 12.2 stores the findings a FIX names on the decision (`issue_ids_json`), and the pipeline that builds
// the next attempt reads them back from HERE rather than receiving them as an argument. A value threaded
// from the gate command to the re-run would be lost by a restart between the two, and the re-run would
// then be a FIX that fixed nothing while still being recorded as one.
//
// The boolean is false when the attempt has no decision, which is the ordinary state of an attempt
// nobody has reviewed — not an error.
func (s *Service) LatestDecisionForStage(ctx context.Context, stageRunID string) (workflow.UserGateDecision, bool, error) {
	if !s.Available() {
		return workflow.UserGateDecision{}, false, storageFailure()
	}
	trimmed := strings.TrimSpace(stageRunID)
	if trimmed == "" {
		return workflow.UserGateDecision{}, false, workflow.InvalidError("A stage run is required.")
	}
	return s.decisions.LatestDecisionForStage(ctx, trimmed)
}

// SubmitGateDecision stores a user's decision at a quality gate.
//
// The decision is validated by the domain before it is written, which is what
// makes three specification rules hold at the persistence boundary: an
// agent-authored decision is refused (section 11.5), a skip must record a reason
// (PRD FR-100's "跳过阶段需记录原因"), and a waiver must record why the stale
// artifact was kept (section 15.3). A refused decision writes nothing.
func (s *Service) SubmitGateDecision(ctx context.Context, request SubmitGateDecisionRequest) (workflow.UserGateDecision, error) {
	if !s.Available() {
		return workflow.UserGateDecision{}, storageFailure()
	}
	id, err := s.ids.New()
	if err != nil {
		return workflow.UserGateDecision{}, storageFailure()
	}
	record := workflow.UserGateDecision{
		ID:                   id,
		WorkflowRunID:        strings.TrimSpace(request.WorkflowRunID),
		StageRunID:           strings.TrimSpace(request.StageRunID),
		Decision:             request.Decision,
		IssueIDsJSON:         request.IssueIDsJSON,
		Instruction:          request.Instruction,
		Reason:               request.Reason,
		LockedEntityRefsJSON: request.LockedEntityRefsJSON,
		CreatedByType:        request.CreatedBy,
		CreatedByID:          request.CreatedByID,
		CreatedAt:            s.now(),
	}
	if err := record.Validate(); err != nil {
		return workflow.UserGateDecision{}, err
	}
	if err := s.decisions.CreateDecision(ctx, record); err != nil {
		return workflow.UserGateDecision{}, err
	}
	// Section 17's UserGateDecided. The decision names its workflow run, which
	// carries the project; a failed lookup skips the announcement rather than
	// failing a decision that is already stored.
	if run, runErr := s.runs.GetRun(ctx, record.WorkflowRunID); runErr == nil {
		s.recordEvent(ctx, eventsapp.Draft{
			Type:          domainevent.UserGateDecided,
			AggregateType: domainevent.AggregateWorkflow,
			AggregateID:   record.ID,
			ProjectID:     run.ProjectID,
		})
	}
	return record, nil
}

// GetRun returns one workflow run.
//
// It exists because WP-07's engine drives transitions through this service, and its
// StageTransitioner interface loads a run's state before deciding anything. The gap
// was found by trying to write the compile-time assertion that the REAL service
// satisfies that interface: before this method nothing could, so the engine could only
// ever have been driven by a test double — which is the "interface with no real path"
// AGENTS section 12 forbids.
func (s *Service) GetRun(ctx context.Context, id string) (workflow.WorkflowRun, error) {
	if !s.Available() {
		return workflow.WorkflowRun{}, storageFailure()
	}
	if strings.TrimSpace(id) == "" {
		return workflow.WorkflowRun{}, workflow.InvalidError("A workflow run is required.")
	}
	return s.runs.GetRun(ctx, id)
}

// GetStage returns one stage attempt.
//
// It is the read the runtime's stage-scope checks need: a tool handed a stage
// identifier must establish which run that stage belongs to before it can decide
// whether the caller may touch it.
func (s *Service) GetStage(ctx context.Context, id string) (workflow.StageRun, error) {
	if !s.Available() {
		return workflow.StageRun{}, storageFailure()
	}
	if strings.TrimSpace(id) == "" {
		return workflow.StageRun{}, workflow.InvalidError("A stage run is required.")
	}
	return s.stages.GetStage(ctx, id)
}

// ListRuns returns a project's runs newest first.
func (s *Service) ListRuns(ctx context.Context, projectID string) ([]workflow.WorkflowRun, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.runs.ListRuns(ctx, projectID)
}

// ListStages returns a run's attempts oldest first.
func (s *Service) ListStages(ctx context.Context, runID string) ([]workflow.StageRun, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.stages.ListStages(ctx, runID)
}

// ListEvents returns a run's audit events oldest first.
func (s *Service) ListEvents(ctx context.Context, runID string) ([]workflow.WorkflowEvent, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.events.ListEvents(ctx, runID)
}

// GetReviewReport returns a stage attempt's newest review report and its issues.
func (s *Service) GetReviewReport(ctx context.Context, stageRunID string) (workflow.ReviewReport, []workflow.ReviewIssue, error) {
	if !s.Available() {
		return workflow.ReviewReport{}, nil, storageFailure()
	}
	report, issues, found, err := s.reviews.GetReportForStage(ctx, stageRunID)
	if err != nil {
		return workflow.ReviewReport{}, nil, err
	}
	if !found {
		return workflow.ReviewReport{}, nil, workflow.NotFoundError()
	}
	return report, issues, nil
}
