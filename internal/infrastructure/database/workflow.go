package database

import (
	"context"
	"database/sql"

	workflowapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// WorkflowRepository is the SQLite implementation of the workflow ports: it
// owns every statement touching workflow runs, stage attempts, review reports
// and their issues, user gate decisions and workflow events.
//
// Column names come from migration 000011, which is the authority for the
// tables this type writes. Every call that takes an event writes the change and
// the event in ONE transaction, because PRD FR-100 requires "每次状态变化写入审计
// 事件" and an event that could commit without its change (or the reverse) would
// make the log disagree with the runs it describes.
type WorkflowRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewWorkflowRepository builds the repository over a database handle.
func NewWorkflowRepository(db *sql.DB) *WorkflowRepository {
	return &WorkflowRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
func (r *WorkflowRepository) WithinTx(tx *sql.Tx) *WorkflowRepository {
	return &WorkflowRepository{db: r.db, tx: tx}
}

func (r *WorkflowRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

// withinTx runs fn inside a transaction of this repository's own database.
//
// The state-change calls use it to write the row and its audit event together.
// A repository already bound to a caller's transaction runs fn against that
// transaction instead of opening a second one, which SQLite would refuse (one
// connection, one writer) and which would break the caller's atomicity.
func (r *WorkflowRepository) withinTx(ctx context.Context, fn func(repo *WorkflowRepository) error) error {
	if r == nil || r.db == nil {
		return storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	if r.tx != nil {
		return fn(r)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("WORKFLOW_TX_FAILED", "The workflow change could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			// A rollback after a successful commit is a no-op, so this is safe
			// on every path and is what keeps a change from committing without
			// its event.
			_ = tx.Rollback()
		}
	}()
	if err := fn(r.WithinTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError("WORKFLOW_TX_FAILED", "The workflow change could not be saved.", err)
	}
	committed = true
	return nil
}

const runSelectColumns = `SELECT id, project_id, episode_id, workflow_type, current_stage, status,
	active_stage_run_id, configuration_json, retry_count, created_at, updated_at, completed_at,
	revision FROM workflow_runs`

// CreateRun stores a run and the event recording its creation, in one
// transaction.
func (r *WorkflowRepository) CreateRun(ctx context.Context, record workflow.WorkflowRun, event workflow.WorkflowEvent) error {
	return r.withinTx(ctx, func(repo *WorkflowRepository) error {
		_, err := repo.conn().ExecContext(ctx, `INSERT INTO workflow_runs
			(id, project_id, episode_id, workflow_type, current_stage, status, active_stage_run_id,
			 configuration_json, retry_count, created_at, updated_at, completed_at, revision)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			record.ID, record.ProjectID, record.EpisodeID, record.WorkflowType,
			string(record.CurrentStage), string(record.Status), record.ActiveStageRunID,
			record.ConfigurationJSON, record.RetryCount, formatTime(record.CreatedAt),
			formatTime(record.UpdatedAt), formatTime(record.CompletedAt), record.Revision)
		if err != nil {
			if isUniqueViolation(err) {
				return workflow.ConflictError("A workflow run with that id already exists.")
			}
			if isForeignKeyViolation(err) {
				return workflow.InvalidError("That project does not exist.")
			}
			return storageError("WORKFLOW_WRITE_FAILED", "The workflow run could not be saved.", err)
		}
		return repo.insertEvent(ctx, event)
	})
}

// GetRun returns one run.
func (r *WorkflowRepository) GetRun(ctx context.Context, id string) (workflow.WorkflowRun, error) {
	conn := r.conn()
	if conn == nil {
		return workflow.WorkflowRun{}, storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, runSelectColumns+` WHERE id = ?`, id)
	record, err := scanRun(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return workflow.WorkflowRun{}, workflow.NotFoundError()
		}
		return workflow.WorkflowRun{}, storageError("WORKFLOW_READ_FAILED", "The workflow run could not be read.", err)
	}
	return record, nil
}

// UpdateRun persists a change guarded by the expected revision, with the event
// recording it, in one transaction.
func (r *WorkflowRepository) UpdateRun(ctx context.Context, record workflow.WorkflowRun, expectedRevision int64, event workflow.WorkflowEvent) error {
	return r.withinTx(ctx, func(repo *WorkflowRepository) error {
		result, err := repo.conn().ExecContext(ctx, `UPDATE workflow_runs
			SET current_stage = ?, status = ?, active_stage_run_id = ?, configuration_json = ?,
			    retry_count = ?, updated_at = ?, completed_at = ?, revision = revision + 1
			WHERE id = ? AND revision = ?`,
			string(record.CurrentStage), string(record.Status), record.ActiveStageRunID,
			record.ConfigurationJSON, record.RetryCount, formatTime(record.UpdatedAt),
			formatTime(record.CompletedAt), record.ID, expectedRevision)
		if err != nil {
			return storageError("WORKFLOW_WRITE_FAILED", "The workflow run could not be updated.", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return storageError("WORKFLOW_WRITE_FAILED", "The workflow run could not be updated.", err)
		}
		if affected == 0 {
			return workflow.ConflictError("This workflow run changed in another window. Reload it and try again.")
		}
		return repo.insertEvent(ctx, event)
	})
}

// ListRuns returns a project's runs newest first.
func (r *WorkflowRepository) ListRuns(ctx context.Context, projectID string) ([]workflow.WorkflowRun, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, runSelectColumns+
		` WHERE project_id = ? ORDER BY created_at DESC, id DESC`, projectID)
	if err != nil {
		return nil, storageError("WORKFLOW_READ_FAILED", "The workflow runs could not be read.", err)
	}
	defer rows.Close()
	var runs []workflow.WorkflowRun
	for rows.Next() {
		record, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, storageError("WORKFLOW_READ_FAILED", "The workflow runs could not be read.", scanErr)
		}
		runs = append(runs, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("WORKFLOW_READ_FAILED", "The workflow runs could not be read.", err)
	}
	return runs, nil
}

const stageSelectColumns = `SELECT id, workflow_run_id, stage, attempt, execution_agent_key, status,
	input_json, validated_output_json, raw_output_file_id, error_code, error_message, created_at,
	started_at, finished_at, revision FROM stage_runs`

// CreateStage stores a stage attempt and the event recording its creation, in
// one transaction.
func (r *WorkflowRepository) CreateStage(ctx context.Context, record workflow.StageRun, event workflow.WorkflowEvent) error {
	return r.withinTx(ctx, func(repo *WorkflowRepository) error {
		_, err := repo.conn().ExecContext(ctx, `INSERT INTO stage_runs
			(id, workflow_run_id, stage, attempt, execution_agent_key, status, input_json,
			 validated_output_json, raw_output_file_id, error_code, error_message, created_at,
			 started_at, finished_at, revision)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			record.ID, record.WorkflowRunID, string(record.Stage), record.Attempt,
			record.ExecutionAgentKey, string(record.Status), record.InputJSON,
			record.ValidatedOutputJSON, record.RawOutputFileID, record.ErrorCode, record.ErrorMessage,
			formatTime(record.CreatedAt), formatTime(record.StartedAt), formatTime(record.FinishedAt),
			record.Revision)
		if err != nil {
			if isUniqueViolation(err) {
				return workflow.ConflictError("That stage attempt already exists for this run.")
			}
			if isForeignKeyViolation(err) {
				return workflow.InvalidError("That workflow run does not exist.")
			}
			return storageError("WORKFLOW_WRITE_FAILED", "The stage attempt could not be saved.", err)
		}
		return repo.insertEvent(ctx, event)
	})
}

// GetStage returns one stage attempt.
func (r *WorkflowRepository) GetStage(ctx context.Context, id string) (workflow.StageRun, error) {
	conn := r.conn()
	if conn == nil {
		return workflow.StageRun{}, storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, stageSelectColumns+` WHERE id = ?`, id)
	record, err := scanStage(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return workflow.StageRun{}, workflow.NotFoundError()
		}
		return workflow.StageRun{}, storageError("WORKFLOW_READ_FAILED", "The stage attempt could not be read.", err)
	}
	return record, nil
}

// UpdateStage persists a change guarded by the expected revision, with the
// event recording it, in one transaction.
func (r *WorkflowRepository) UpdateStage(ctx context.Context, record workflow.StageRun, expectedRevision int64, event workflow.WorkflowEvent) error {
	return r.withinTx(ctx, func(repo *WorkflowRepository) error {
		result, err := repo.conn().ExecContext(ctx, `UPDATE stage_runs
			SET execution_agent_key = ?, status = ?, input_json = ?, validated_output_json = ?,
			    raw_output_file_id = ?, error_code = ?, error_message = ?, started_at = ?,
			    finished_at = ?, revision = revision + 1
			WHERE id = ? AND revision = ?`,
			record.ExecutionAgentKey, string(record.Status), record.InputJSON,
			record.ValidatedOutputJSON, record.RawOutputFileID, record.ErrorCode, record.ErrorMessage,
			formatTime(record.StartedAt), formatTime(record.FinishedAt), record.ID, expectedRevision)
		if err != nil {
			return storageError("WORKFLOW_WRITE_FAILED", "The stage attempt could not be updated.", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return storageError("WORKFLOW_WRITE_FAILED", "The stage attempt could not be updated.", err)
		}
		if affected == 0 {
			return workflow.ConflictError("This stage attempt changed in another window. Reload it and try again.")
		}
		return repo.insertEvent(ctx, event)
	})
}

// ListStages returns a run's attempts oldest first.
func (r *WorkflowRepository) ListStages(ctx context.Context, workflowRunID string) ([]workflow.StageRun, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, stageSelectColumns+
		` WHERE workflow_run_id = ? ORDER BY created_at ASC, id ASC`, workflowRunID)
	if err != nil {
		return nil, storageError("WORKFLOW_READ_FAILED", "The stage attempts could not be read.", err)
	}
	defer rows.Close()
	var stages []workflow.StageRun
	for rows.Next() {
		record, scanErr := scanStage(rows)
		if scanErr != nil {
			return nil, storageError("WORKFLOW_READ_FAILED", "The stage attempts could not be read.", scanErr)
		}
		stages = append(stages, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("WORKFLOW_READ_FAILED", "The stage attempts could not be read.", err)
	}
	return stages, nil
}

const reviewSelectColumns = `SELECT id, stage_run_id, supervisor_key, ruleset_version, score, grade,
	passed, severity, recommended_action, summary, created_at FROM review_reports`

const reviewIssueSelectColumns = `SELECT id, review_report_id, rule, severity, entity_type, entity_id,
	location, field, problem, suggestion, evidence_json, auto_fixable, status, resolved_by, resolved_at,
	created_at FROM review_issues`

// CreateReport writes a report and its issues in one transaction, so a report
// is never stored without the findings it is about.
func (r *WorkflowRepository) CreateReport(ctx context.Context, report workflow.ReviewReport, issues []workflow.ReviewIssue) error {
	return r.withinTx(ctx, func(repo *WorkflowRepository) error {
		var score any
		if report.Score != nil {
			score = *report.Score
		}
		_, err := repo.conn().ExecContext(ctx, `INSERT INTO review_reports
			(id, stage_run_id, supervisor_key, ruleset_version, score, grade, passed, severity,
			 recommended_action, summary, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			report.ID, report.StageRunID, report.SupervisorKey, report.RulesetVersion, score,
			string(report.Grade), boolInt(report.Passed), string(report.Severity),
			report.RecommendedAction, report.Summary, formatTime(report.CreatedAt))
		if err != nil {
			if isForeignKeyViolation(err) {
				return workflow.InvalidError("That stage attempt does not exist.")
			}
			return storageError("WORKFLOW_WRITE_FAILED", "The review report could not be saved.", err)
		}
		for _, issue := range issues {
			if _, err := repo.conn().ExecContext(ctx, `INSERT INTO review_issues
				(id, review_report_id, rule, severity, entity_type, entity_id, location, field,
				 problem, suggestion, evidence_json, auto_fixable, status, resolved_by, resolved_at,
				 created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				issue.ID, issue.ReviewReportID, issue.Rule, string(issue.Severity), issue.EntityType,
				issue.EntityID, issue.Location, issue.Field, issue.Problem, issue.Suggestion,
				issue.EvidenceJSON, boolInt(issue.AutoFixable), string(issue.Status), issue.ResolvedBy,
				formatTime(issue.ResolvedAt), formatTime(issue.CreatedAt)); err != nil {
				if isUniqueViolation(err) {
					return workflow.ConflictError("A review issue with that id already exists.")
				}
				return storageError("WORKFLOW_WRITE_FAILED", "The review finding could not be saved.", err)
			}
		}
		return nil
	})
}

// GetReportForStage returns the newest report for a stage attempt and its
// issues.
//
// "Newest" is created_at, then id, because the schema allows several reports per
// attempt (one per review) and created_at can tie when a ruleset and a
// supervisor run in the same millisecond.
func (r *WorkflowRepository) GetReportForStage(ctx context.Context, stageRunID string) (workflow.ReviewReport, []workflow.ReviewIssue, bool, error) {
	conn := r.conn()
	if conn == nil {
		return workflow.ReviewReport{}, nil, false, storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, reviewSelectColumns+
		` WHERE stage_run_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, stageRunID)
	report, err := scanReport(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return workflow.ReviewReport{}, nil, false, nil
		}
		return workflow.ReviewReport{}, nil, false, storageError("WORKFLOW_READ_FAILED", "The review report could not be read.", err)
	}
	issues, err := r.listIssues(ctx, report.ID)
	if err != nil {
		return workflow.ReviewReport{}, nil, false, err
	}
	return report, issues, true, nil
}

// listIssues returns one report's findings in creation order.
func (r *WorkflowRepository) listIssues(ctx context.Context, reportID string) ([]workflow.ReviewIssue, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, reviewIssueSelectColumns+
		` WHERE review_report_id = ? ORDER BY created_at ASC, id ASC`, reportID)
	if err != nil {
		return nil, storageError("WORKFLOW_READ_FAILED", "The review findings could not be read.", err)
	}
	defer rows.Close()
	var issues []workflow.ReviewIssue
	for rows.Next() {
		issue, scanErr := scanIssue(rows)
		if scanErr != nil {
			return nil, storageError("WORKFLOW_READ_FAILED", "The review findings could not be read.", scanErr)
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("WORKFLOW_READ_FAILED", "The review findings could not be read.", err)
	}
	return issues, nil
}

// CreateDecision stores a user gate decision.
//
// The schema keeps the author in one created_by column. The domain splits it
// into a kind and an id, so the two are joined here with the separator the
// domain's own vocabulary cannot contain ("user" and an id never carry a colon
// prefix), and the kind is what the domain's rule about agents reads: the
// application refuses an agent-authored decision before this is reached.
func (r *WorkflowRepository) CreateDecision(ctx context.Context, decision workflow.UserGateDecision) error {
	conn := r.conn()
	if conn == nil {
		return storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO user_gate_decisions
		(id, workflow_run_id, stage_run_id, decision, issue_ids_json, instruction, reason,
		 locked_entity_refs_json, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		decision.ID, decision.WorkflowRunID, decision.StageRunID, string(decision.Decision),
		nonEmptyJSONArray(decision.IssueIDsJSON), decision.Instruction, decision.Reason,
		decision.LockedEntityRefsJSON, encodeCreatedBy(decision.CreatedByType, decision.CreatedByID),
		formatTime(decision.CreatedAt))
	if err != nil {
		if isForeignKeyViolation(err) {
			return workflow.InvalidError("That workflow run does not exist.")
		}
		return storageError("WORKFLOW_WRITE_FAILED", "The gate decision could not be saved.", err)
	}
	return nil
}

const eventSelectColumns = `SELECT id, workflow_run_id, stage_run_id, event_type, from_status,
	to_status, payload_json, actor_type, actor_id, created_at FROM workflow_events`

// insertEvent writes one audit event.
func (r *WorkflowRepository) insertEvent(ctx context.Context, event workflow.WorkflowEvent) error {
	conn := r.conn()
	if conn == nil {
		return storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO workflow_events
		(id, workflow_run_id, stage_run_id, event_type, from_status, to_status, payload_json,
		 actor_type, actor_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.WorkflowRunID, event.StageRunID, string(event.EventType), event.FromStatus,
		event.ToStatus, event.PayloadJSON, string(event.ActorType), event.ActorID,
		formatTime(event.CreatedAt))
	if err != nil {
		if isForeignKeyViolation(err) {
			return workflow.InvalidError("That workflow run does not exist.")
		}
		return storageError("WORKFLOW_WRITE_FAILED", "The workflow event could not be saved.", err)
	}
	return nil
}

// ListEvents returns a run's events oldest first.
func (r *WorkflowRepository) ListEvents(ctx context.Context, workflowRunID string) ([]workflow.WorkflowEvent, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("WORKFLOW_STORE_UNAVAILABLE", "The workflow store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, eventSelectColumns+
		` WHERE workflow_run_id = ? ORDER BY created_at ASC, id ASC`, workflowRunID)
	if err != nil {
		return nil, storageError("WORKFLOW_READ_FAILED", "The workflow events could not be read.", err)
	}
	defer rows.Close()
	var events []workflow.WorkflowEvent
	for rows.Next() {
		event, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, storageError("WORKFLOW_READ_FAILED", "The workflow events could not be read.", scanErr)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("WORKFLOW_READ_FAILED", "The workflow events could not be read.", err)
	}
	return events, nil
}

// encodeCreatedBy joins the producer kind and its id into the single column the
// schema stores.
//
// The separator is a colon, which the producer vocabulary's own values ("user",
// "agent", "migration", "system") cannot contain, and no identifier this
// application mints does either (UUIDv7 is lowercase hex and hyphens). A reader
// that only needs the kind takes the text before the first colon.
func encodeCreatedBy(kind versioning.CreatedByType, id string) string {
	if id == "" {
		return string(kind)
	}
	return string(kind) + ":" + id
}

// scanRun reads one workflow run row.
func scanRun(row rowScanner) (workflow.WorkflowRun, error) {
	var record workflow.WorkflowRun
	var currentStage, status, createdAt, updatedAt, completedAt string
	if err := row.Scan(&record.ID, &record.ProjectID, &record.EpisodeID, &record.WorkflowType,
		&currentStage, &status, &record.ActiveStageRunID, &record.ConfigurationJSON,
		&record.RetryCount, &createdAt, &updatedAt, &completedAt, &record.Revision); err != nil {
		return workflow.WorkflowRun{}, err
	}
	record.CurrentStage = workflow.StageName(currentStage)
	record.Status = workflow.RunStatus(status)
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	record.CompletedAt = parseTime(completedAt)
	return record, nil
}

// scanStage reads one stage attempt row.
func scanStage(row rowScanner) (workflow.StageRun, error) {
	var record workflow.StageRun
	var stage, status, createdAt, startedAt, finishedAt string
	if err := row.Scan(&record.ID, &record.WorkflowRunID, &stage, &record.Attempt,
		&record.ExecutionAgentKey, &status, &record.InputJSON, &record.ValidatedOutputJSON,
		&record.RawOutputFileID, &record.ErrorCode, &record.ErrorMessage, &createdAt,
		&startedAt, &finishedAt, &record.Revision); err != nil {
		return workflow.StageRun{}, err
	}
	record.Stage = workflow.StageName(stage)
	record.Status = workflow.StageStatus(status)
	record.CreatedAt = parseTime(createdAt)
	record.StartedAt = parseTime(startedAt)
	record.FinishedAt = parseTime(finishedAt)
	return record, nil
}

// scanReport reads one review report row.
func scanReport(row rowScanner) (workflow.ReviewReport, error) {
	var report workflow.ReviewReport
	var score sql.NullFloat64
	var grade, severity, createdAt string
	var passed int
	if err := row.Scan(&report.ID, &report.StageRunID, &report.SupervisorKey, &report.RulesetVersion,
		&score, &grade, &passed, &severity, &report.RecommendedAction, &report.Summary,
		&createdAt); err != nil {
		return workflow.ReviewReport{}, err
	}
	if score.Valid {
		// The column is nullable so "did not score" stays apart from "scored
		// zero" (see ReviewReport.Score).
		value := score.Float64
		report.Score = &value
	}
	report.Grade = workflow.Grade(grade)
	report.Passed = passed == 1
	report.Severity = workflow.Severity(severity)
	report.CreatedAt = parseTime(createdAt)
	return report, nil
}

// scanIssue reads one review issue row.
func scanIssue(row rowScanner) (workflow.ReviewIssue, error) {
	var issue workflow.ReviewIssue
	var severity, status, resolvedAt, createdAt string
	var autoFixable int
	if err := row.Scan(&issue.ID, &issue.ReviewReportID, &issue.Rule, &severity, &issue.EntityType,
		&issue.EntityID, &issue.Location, &issue.Field, &issue.Problem, &issue.Suggestion,
		&issue.EvidenceJSON, &autoFixable, &status, &issue.ResolvedBy, &resolvedAt,
		&createdAt); err != nil {
		return workflow.ReviewIssue{}, err
	}
	issue.Severity = workflow.Severity(severity)
	issue.Status = workflow.IssueStatus(status)
	issue.AutoFixable = autoFixable == 1
	issue.ResolvedAt = parseTime(resolvedAt)
	issue.CreatedAt = parseTime(createdAt)
	return issue, nil
}

// scanEvent reads one workflow event row.
func scanEvent(row rowScanner) (workflow.WorkflowEvent, error) {
	var event workflow.WorkflowEvent
	var eventType, actorType, createdAt string
	if err := row.Scan(&event.ID, &event.WorkflowRunID, &event.StageRunID, &eventType,
		&event.FromStatus, &event.ToStatus, &event.PayloadJSON, &actorType, &event.ActorID,
		&createdAt); err != nil {
		return workflow.WorkflowEvent{}, err
	}
	event.EventType = workflow.EventType(eventType)
	event.ActorType = versioning.CreatedByType(actorType)
	event.CreatedAt = parseTime(createdAt)
	return event, nil
}

// Ensure the repository satisfies the application ports.
var (
	_ workflowapp.RunRepository      = (*WorkflowRepository)(nil)
	_ workflowapp.StageRepository    = (*WorkflowRepository)(nil)
	_ workflowapp.ReviewRepository   = (*WorkflowRepository)(nil)
	_ workflowapp.DecisionRepository = (*WorkflowRepository)(nil)
	_ workflowapp.EventRepository    = (*WorkflowRepository)(nil)
)
