package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// JobRepository persists generation jobs, their attempts, and their
// dependencies. It owns all SQL for the job aggregate.
type JobRepository struct {
	db *sql.DB
}

// NewJobRepository builds the SQLite job repository.
func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

// Insert stores a new job. The unique (project_id, idempotency_key) constraint
// is translated to a conflict error so the application can return the
// existing job instead of failing.
func (r *JobRepository) Insert(ctx context.Context, record job.Job) error {
	if r == nil || r.db == nil {
		return job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO generation_jobs
		(id, project_id, entity_type, entity_id, job_type, status, priority, idempotency_key,
		 provider_config_id, model_config_id, remote_job_id, progress, input_json, result_json,
		 error_code, error_message, next_retry_at, cancel_requested, remote_cancel_unconfirmed,
		 attempt_count, max_attempts, lease_owner, lease_expires_at, created_at, updated_at, started_at, finished_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProjectID, record.EntityType, record.EntityID, string(record.JobType),
		string(record.Status), record.Priority, record.IdempotencyKey, record.ProviderConfigID,
		record.ModelConfigID, record.RemoteJobID, nullableInt(record.Progress), record.InputJSON,
		record.ResultJSON, record.ErrorCode, record.ErrorMessage, formatTime(record.NextRetryAt),
		boolInt(record.CancelRequested), boolInt(record.CancelledRemoteUnconfirmed), record.AttemptCount, record.MaxAttempts,
		record.LeaseOwner, formatTime(record.LeaseExpiresAt), formatTime(record.CreatedAt),
		formatTime(record.UpdatedAt), formatTime(record.StartedAt), formatTime(record.FinishedAt),
		record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return job.DuplicateJobError()
		}
		return job.FailedJobError(job.CategoryStorage, "The job could not be stored.")
	}
	return nil
}

// Get returns one job by ID.
func (r *JobRepository) Get(ctx context.Context, id string) (job.Job, error) {
	if r == nil || r.db == nil {
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	row := r.db.QueryRowContext(ctx, jobSelectColumns+` WHERE id = ?`, id)
	record, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return job.Job{}, job.JobNotFoundError(id)
		}
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job could not be read.")
	}
	return record, nil
}

// GetByIdempotencyKey returns the job stored for a key.
func (r *JobRepository) GetByIdempotencyKey(ctx context.Context, projectID, key string) (job.Job, error) {
	if r == nil || r.db == nil {
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	row := r.db.QueryRowContext(ctx, jobSelectColumns+` WHERE project_id = ? AND idempotency_key = ?`, projectID, key)
	record, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return job.Job{}, job.JobNotFoundError(key)
		}
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job could not be read.")
	}
	return record, nil
}

// List returns jobs matching the filter, newest first.
func (r *JobRepository) List(ctx context.Context, filter jobs.ListFilter) ([]job.Job, error) {
	if r == nil || r.db == nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	query := jobSelectColumns
	args := []any{}
	clauses := []string{}
	if filter.ActiveOnly {
		clauses = append(clauses, `status NOT IN ('succeeded', 'remote_only', 'failed', 'cancelled', 'orphaned')`)
	}
	if len(filter.Statuses) > 0 {
		placeholders := ""
		for index, status := range filter.Statuses {
			if index > 0 {
				placeholders += ", "
			}
			placeholders += "?"
			args = append(args, string(status))
		}
		clauses = append(clauses, "status IN ("+placeholders+")")
	}
	if len(clauses) > 0 {
		query += " WHERE "
		for index, clause := range clauses {
			if index > 0 {
				query += " AND "
			}
			query += clause
		}
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit, filter.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job list could not be read.")
	}
	defer rows.Close()
	records, err := scanJobs(rows)
	if err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job list could not be read.")
	}
	return records, nil
}

// ClaimableCandidates returns jobs the scheduler may run now, in priority
// order. A job is claimable when it is queued or its retry window elapsed, and
// its lease is free (including leases left by a dead process).
func (r *JobRepository) ClaimableCandidates(ctx context.Context, now time.Time, limit int) ([]job.Job, error) {
	if r == nil || r.db == nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	if limit <= 0 {
		limit = 16
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	// Three kinds of job are runnable: ordinary work (queued, or past its retry
	// window), a parked job whose earliest-run time elapsed (waiting_remote and
	// friends are paced through next_retry_at so a remote poll is not issued at
	// scheduler speed), or work abandoned by an expired lease. A cancel-flagged
	// job is also offered so a worker can settle it.
	rows, err := r.db.QueryContext(ctx, jobSelectColumns+`
		WHERE (
		    (cancel_requested = 0 AND (
		      status = 'queued'
		      OR (status = 'retry_wait' AND (next_retry_at = '' OR next_retry_at <= ?))
		      OR (status IN ('waiting_remote', 'downloading', 'verifying', 'recovering')
		          AND (next_retry_at = '' OR next_retry_at <= ?)
		          AND (lease_owner = '' OR lease_expires_at <= ?))
		      OR (status = 'running' AND (lease_owner = '' OR lease_expires_at <= ?))
		    ))
		    OR (cancel_requested = 1
		        AND status NOT IN ('succeeded', 'remote_only', 'failed', 'cancelled', 'orphaned')
		        AND (lease_owner = '' OR lease_expires_at <= ?))
		  )
		ORDER BY priority DESC, created_at ASC
		LIMIT ?`, nowText, nowText, nowText, nowText, nowText, limit)
	if err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "Runnable jobs could not be read.")
	}
	defer rows.Close()
	records, err := scanJobs(rows)
	if err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "Runnable jobs could not be read.")
	}
	return records, nil
}

// Claim atomically takes ownership of a claimable job.
//
// The guard has three parts: the job must currently be claimable (queued, in a
// retry window, or abandoned by an expired lease), it must not be flagged for
// cancellation, and the revision must still match the caller's read. Together
// they make exactly one competing worker win even when several pass the
// candidate query at the same time.
func (r *JobRepository) Claim(ctx context.Context, id, workerID string, leaseUntil time.Time, now time.Time) (job.Job, error) {
	if r == nil || r.db == nil {
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	current, err := r.Get(ctx, id)
	if err != nil {
		return job.Job{}, err
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	// The guard mirrors ClaimableCandidates exactly, including its two branches:
	//
	//   - ordinary work, where the next_retry_at window on parked states paces a
	//     remote poll (without it a competing worker could poll once more inside
	//     the same interval);
	//   - a cancel-flagged job, which needs ownership to be settled and is
	//     therefore exempt from the window: waiting for a poll interval before a
	//     cancelled job can be closed out would be an arbitrary delay, and the
	//     worker never polls a job it is settling.
	result, err := r.db.ExecContext(ctx, `UPDATE generation_jobs
		SET status = 'running', lease_owner = ?, lease_expires_at = ?,
		    started_at = CASE WHEN started_at = '' THEN ? ELSE started_at END,
		    updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?
		  AND (
		    (cancel_requested = 0 AND (
		      status = 'queued'
		      OR (status = 'retry_wait' AND (next_retry_at = '' OR next_retry_at <= ?))
		      OR (status IN ('waiting_remote', 'downloading', 'verifying', 'recovering')
		          AND (next_retry_at = '' OR next_retry_at <= ?)
		          AND (lease_owner = '' OR lease_expires_at <= ?))
		      OR (status = 'running' AND (lease_owner = '' OR lease_expires_at <= ?))
		    ))
		    OR (cancel_requested = 1
		        AND status NOT IN ('succeeded', 'remote_only', 'failed', 'cancelled', 'orphaned')
		        AND (lease_owner = '' OR lease_expires_at <= ?))
		  )`,
		workerID, leaseUntil.UTC().Format(time.RFC3339Nano), nowText, nowText, id, current.Revision,
		nowText, nowText, nowText, nowText, nowText)
	if err != nil {
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job could not be claimed.")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job could not be claimed.")
	}
	if affected == 0 {
		// Lost the race or the job moved on.
		return job.Job{}, job.DuplicateJobError()
	}
	return r.Get(ctx, id)
}

// Update persists a job change guarded by the expected revision.
func (r *JobRepository) Update(ctx context.Context, record job.Job, expectedRevision int64) error {
	if r == nil || r.db == nil {
		return job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE generation_jobs
		SET status = ?, priority = ?, remote_job_id = ?, progress = ?, result_json = ?,
		    error_code = ?, error_message = ?, next_retry_at = ?, cancel_requested = ?, remote_cancel_unconfirmed = ?,
		    attempt_count = ?, max_attempts = ?, lease_owner = ?, lease_expires_at = ?,
		    updated_at = ?, finished_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		string(record.Status), record.Priority, record.RemoteJobID, nullableInt(record.Progress),
		record.ResultJSON, record.ErrorCode, record.ErrorMessage, formatTime(record.NextRetryAt),
		boolInt(record.CancelRequested), boolInt(record.CancelledRemoteUnconfirmed), record.AttemptCount, record.MaxAttempts,
		record.LeaseOwner, formatTime(record.LeaseExpiresAt), formatTime(record.UpdatedAt),
		formatTime(record.FinishedAt), record.ID, expectedRevision)
	if err != nil {
		return job.FailedJobError(job.CategoryStorage, "The job could not be updated.")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return job.FailedJobError(job.CategoryStorage, "The job could not be updated.")
	}
	if affected == 0 {
		return job.DuplicateJobError()
	}
	return nil
}

// RequestCancel flags a job for cancellation and returns the fresh row.
func (r *JobRepository) RequestCancel(ctx context.Context, id string, now time.Time) (job.Job, error) {
	if r == nil || r.db == nil {
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE generation_jobs
		SET cancel_requested = 1, updated_at = ?, revision = revision + 1
		WHERE id = ?`, formatTime(now), id); err != nil {
		return job.Job{}, job.FailedJobError(job.CategoryStorage, "The job could not be cancelled.")
	}
	return r.Get(ctx, id)
}

// ActiveCounts reports jobs per status.
func (r *JobRepository) ActiveCounts(ctx context.Context) (map[job.Status]int, error) {
	if r == nil || r.db == nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM generation_jobs GROUP BY status`)
	if err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job counts could not be read.")
	}
	defer rows.Close()
	counts := map[job.Status]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, job.FailedJobError(job.CategoryStorage, "The job counts could not be read.")
		}
		counts[job.Status(status)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job counts could not be read.")
	}
	return counts, nil
}

// StartAttempt records the beginning of an attempt.
func (r *JobRepository) StartAttempt(ctx context.Context, attempt job.Attempt) error {
	if r == nil || r.db == nil {
		return job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO job_attempts
		(id, generation_job_id, attempt_number, status, provider_request_id, error_code, error_message, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.ID, attempt.JobID, attempt.AttemptNumber, string(attempt.Status),
		attempt.ProviderRequestID, attempt.ErrorCode, attempt.ErrorMessage,
		formatTime(attempt.StartedAt), formatTime(attempt.FinishedAt)); err != nil {
		return job.FailedJobError(job.CategoryStorage, "The job attempt could not be recorded.")
	}
	return nil
}

// FinishAttempt updates an attempt's outcome.
func (r *JobRepository) FinishAttempt(ctx context.Context, attempt job.Attempt) error {
	if r == nil || r.db == nil {
		return job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE job_attempts
		SET status = ?, provider_request_id = ?, error_code = ?, error_message = ?, finished_at = ?
		WHERE id = ?`,
		string(attempt.Status), attempt.ProviderRequestID, attempt.ErrorCode,
		attempt.ErrorMessage, formatTime(attempt.FinishedAt), attempt.ID); err != nil {
		return job.FailedJobError(job.CategoryStorage, "The job attempt could not be updated.")
	}
	return nil
}

// ListAttempts returns a job's attempts in order.
func (r *JobRepository) ListAttempts(ctx context.Context, jobID string) ([]job.Attempt, error) {
	if r == nil || r.db == nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, generation_job_id, attempt_number, status, provider_request_id,
		error_code, error_message, started_at, finished_at
		FROM job_attempts WHERE generation_job_id = ? ORDER BY attempt_number`, jobID)
	if err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job attempts could not be read.")
	}
	defer rows.Close()
	var attempts []job.Attempt
	for rows.Next() {
		var attempt job.Attempt
		var status, startedAt, finishedAt string
		if err := rows.Scan(&attempt.ID, &attempt.JobID, &attempt.AttemptNumber, &status,
			&attempt.ProviderRequestID, &attempt.ErrorCode, &attempt.ErrorMessage, &startedAt, &finishedAt); err != nil {
			return nil, job.FailedJobError(job.CategoryStorage, "The job attempts could not be read.")
		}
		attempt.Status = job.AttemptStatus(status)
		attempt.StartedAt = parseTime(startedAt)
		attempt.FinishedAt = parseTime(finishedAt)
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job attempts could not be read.")
	}
	return attempts, nil
}

// AddDependency records a requirement between two jobs.
func (r *JobRepository) AddDependency(ctx context.Context, dependency job.Dependency) error {
	if r == nil || r.db == nil {
		return job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	if !job.IsValidCondition(dependency.Condition) {
		return job.FailedJobError(job.CategoryInvalidInput, "The dependency condition is not supported.")
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO job_dependencies (job_id, depends_on_job_id, condition)
		VALUES (?, ?, ?)`, dependency.JobID, dependency.DependsOnJobID, string(dependency.Condition)); err != nil {
		return job.FailedJobError(job.CategoryStorage, "The job dependency could not be recorded.")
	}
	return nil
}

// ListDependencies returns a job's requirements.
func (r *JobRepository) ListDependencies(ctx context.Context, jobID string) ([]job.Dependency, error) {
	if r == nil || r.db == nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job store is unavailable.")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT job_id, depends_on_job_id, condition
		FROM job_dependencies WHERE job_id = ?`, jobID)
	if err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job dependencies could not be read.")
	}
	defer rows.Close()
	var dependencies []job.Dependency
	for rows.Next() {
		var dependency job.Dependency
		var condition string
		if err := rows.Scan(&dependency.JobID, &dependency.DependsOnJobID, &condition); err != nil {
			return nil, job.FailedJobError(job.CategoryStorage, "The job dependencies could not be read.")
		}
		dependency.Condition = job.DependencyCondition(condition)
		dependencies = append(dependencies, dependency)
	}
	if err := rows.Err(); err != nil {
		return nil, job.FailedJobError(job.CategoryStorage, "The job dependencies could not be read.")
	}
	return dependencies, nil
}

const jobSelectColumns = `SELECT id, project_id, entity_type, entity_id, job_type, status, priority,
	idempotency_key, provider_config_id, model_config_id, remote_job_id, progress, input_json,
	result_json, error_code, error_message, next_retry_at, cancel_requested, remote_cancel_unconfirmed,
	attempt_count,
	max_attempts, lease_owner, lease_expires_at, created_at, updated_at, started_at, finished_at, revision
	FROM generation_jobs`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(scanner rowScanner) (job.Job, error) {
	var record job.Job
	var jobType, status, nextRetryAt, leaseExpiresAt, createdAt, updatedAt, startedAt, finishedAt string
	var progress sql.NullInt64
	var cancelRequested, remoteCancelUnconfirmed int
	if err := scanner.Scan(
		&record.ID, &record.ProjectID, &record.EntityType, &record.EntityID, &jobType, &status,
		&record.Priority, &record.IdempotencyKey, &record.ProviderConfigID, &record.ModelConfigID,
		&record.RemoteJobID, &progress, &record.InputJSON, &record.ResultJSON, &record.ErrorCode,
		&record.ErrorMessage, &nextRetryAt, &cancelRequested, &remoteCancelUnconfirmed, &record.AttemptCount, &record.MaxAttempts,
		&record.LeaseOwner, &leaseExpiresAt, &createdAt, &updatedAt, &startedAt, &finishedAt,
		&record.Revision); err != nil {
		return job.Job{}, err
	}
	record.JobType = job.JobType(jobType)
	record.Status = job.Status(status)
	if progress.Valid {
		value := int(progress.Int64)
		record.Progress = &value
	}
	record.CancelRequested = cancelRequested == 1
	record.CancelledRemoteUnconfirmed = remoteCancelUnconfirmed == 1
	record.NextRetryAt = parseTime(nextRetryAt)
	record.LeaseExpiresAt = parseTime(leaseExpiresAt)
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	record.StartedAt = parseTime(startedAt)
	record.FinishedAt = parseTime(finishedAt)
	return record, nil
}

func scanJobs(rows *sql.Rows) ([]job.Job, error) {
	var records []job.Job
	for rows.Next() {
		record, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// formatTime renders a timestamp in the storage format. The zero time becomes
// an empty string so "unset" stays distinguishable from a real instant.
func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

// isUniqueViolation detects the SQLite unique-constraint failure without
// depending on driver-specific error types.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	for _, needle := range []string{"UNIQUE constraint failed", "constraint failed: UNIQUE"} {
		if contains(message, needle) {
			return true
		}
	}
	return false
}

// isForeignKeyViolation detects the SQLite foreign-key failure without
// depending on driver-specific error types. The asset repository uses it to
// report "that file is not available to link" when a version is pointed at a
// hash that was never committed.
func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	for _, needle := range []string{"FOREIGN KEY constraint failed", "constraint failed: FOREIGN KEY"} {
		if contains(message, needle) {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
