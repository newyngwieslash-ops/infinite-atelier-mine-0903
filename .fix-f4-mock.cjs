const fs = require("fs");

// F-4: the orphan explanation must survive the worker's terminal write. Record
// it in `result_json` (which the runner owns) rather than error_code, which the
// worker legitimately rewrites for its own outcome.
let service = fs.readFileSync("internal/application/jobs/service.go", "utf8");
service = service.replace(`	current, err := s.repository.Get(ctx, record.ID)
	if err != nil {
		return
	}
	current.ErrorCode = string(job.CategoryCancelled)
	current.ErrorMessage = detail
	current.UpdatedAt = s.now()
	// A revision conflict means a worker just wrote the row; its own write
	// carries the terminal state, so losing this update is harmless.
	_ = s.repository.Update(ctx, current, current.Revision)`,
`	current, err := s.repository.Get(ctx, record.ID)
	if err != nil {
		return
	}
	// The orphan note is stored in result_json, not error_message: the worker
	// writes error_code/message from the attempt outcome and would overwrite the
	// note, while result_json is the field the runner owns.
	encoded, encodeErr := json.Marshal(map[string]string{
		"mode":      "orphan_candidate",
		"remoteJob": current.RemoteJobID,
		"note":      detail,
	})
	if encodeErr != nil {
		return
	}
	current.ResultJSON = string(encoded)
	current.CancelledRemoteUnconfirmed = true
	current.UpdatedAt = s.now()
	// A revision conflict means a worker just wrote the row; its own write
	// carries the terminal state, so losing this update is harmless.
	_ = s.repository.Update(ctx, current, current.Revision)`);
service = service.replace('import (\n\t"context"\n\t"errors"\n\t"sync"\n\t"time"', 'import (\n\t"context"\n\t"encoding/json"\n\t"errors"\n\t"sync"\n\t"time"');
fs.writeFileSync("internal/application/jobs/service.go", service);
console.log("orphan note stored in result_json");

// Domain: carry the flag so the DTO can surface it.
let domain = fs.readFileSync("internal/domain/job/job.go", "utf8");
domain = domain.replace(`	CancelRequested bool
	AttemptCount    int`, `	CancelRequested bool
	// CancelledRemoteUnconfirmed records that a provider-side job existed when
	// the user cancelled and its cancellation could not be confirmed. The UI
	// shows this as an orphan candidate rather than implying the provider
	// stopped.
	CancelledRemoteUnconfirmed bool
	AttemptCount               int`);
fs.writeFileSync("internal/domain/job/job.go", domain);

// The worker must preserve the flag on its terminal write.
let worker = fs.readFileSync("internal/application/jobs/worker.go", "utf8");
worker = worker.replace(`	if refreshed, err := s.repository.Get(ctx, record.ID); err == nil {
		record.Revision = refreshed.Revision
		if refreshed.CancelRequested {
			record.CancelRequested = true
		}
	}`,
`	if refreshed, err := s.repository.Get(ctx, record.ID); err == nil {
		record.Revision = refreshed.Revision
		if refreshed.CancelRequested {
			record.CancelRequested = true
		}
		// Preserve the orphan note: it describes a remote side effect the
		// worker knows nothing about.
		record.CancelledRemoteUnconfirmed = refreshed.CancelledRemoteUnconfirmed
		if refreshed.CancelledRemoteUnconfirmed {
			record.ResultJSON = refreshed.ResultJSON
		}
	}`);
fs.writeFileSync("internal/application/jobs/worker.go", worker);
console.log("worker preserves the orphan note");

// Repository: persist the flag.
let repo = fs.readFileSync("internal/infrastructure/database/jobs.go", "utf8");
repo = repo.replace(`		SET status = ?, priority = ?, remote_job_id = ?, progress = ?, result_json = ?,
		    error_code = ?, error_message = ?, next_retry_at = ?, cancel_requested = ?,`,
`		SET status = ?, priority = ?, remote_job_id = ?, progress = ?, result_json = ?,
		    error_code = ?, error_message = ?, next_retry_at = ?, cancel_requested = ?, remote_cancel_unconfirmed = ?,`);
repo = repo.replace(`		boolInt(record.CancelRequested), record.AttemptCount, record.MaxAttempts,`,
`		boolInt(record.CancelRequested), boolInt(record.CancelledRemoteUnconfirmed), record.AttemptCount, record.MaxAttempts,`);
repo = repo.replace(`	SELECT id, project_id, entity_type, entity_id, job_type, status, priority,
	idempotency_key, provider_config_id, model_config_id, remote_job_id, progress, input_json,
	result_json, error_code, error_message, next_retry_at, cancel_requested, attempt_count,
	max_attempts, lease_owner, lease_expires_at, created_at, updated_at, started_at, finished_at, revision
	FROM generation_jobs`,
`	SELECT id, project_id, entity_type, entity_id, job_type, status, priority,
	idempotency_key, provider_config_id, model_config_id, remote_job_id, progress, input_json,
	result_json, error_code, error_message, next_retry_at, cancel_requested, remote_cancel_unconfirmed,
	attempt_count, max_attempts, lease_owner, lease_expires_at, created_at, updated_at, started_at, finished_at, revision
	FROM generation_jobs`);
repo = repo.replace(`	var cancelRequested int
	if err := scanner.Scan(`, `	var cancelRequested, remoteCancelUnconfirmed int
	if err := scanner.Scan(`);
repo = repo.replace(`		&record.ErrorMessage, &nextRetryAt, &cancelRequested, &record.AttemptCount, &record.MaxAttempts,`,
`		&record.ErrorMessage, &nextRetryAt, &cancelRequested, &remoteCancelUnconfirmed, &record.AttemptCount, &record.MaxAttempts,`);
repo = repo.replace(`	record.CancelRequested = cancelRequested == 1`, `	record.CancelRequested = cancelRequested == 1
	record.CancelledRemoteUnconfirmed = remoteCancelUnconfirmed == 1`);
repo = repo.replace(`		(id, project_id, entity_type, entity_id, job_type, status, priority, idempotency_key,
		 provider_config_id, model_config_id, remote_job_id, progress, input_json, result_json,
		 error_code, error_message, next_retry_at, cancel_requested, attempt_count, max_attempts,
		 lease_owner, lease_expires_at, created_at, updated_at, started_at, finished_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
`		(id, project_id, entity_type, entity_id, job_type, status, priority, idempotency_key,
		 provider_config_id, model_config_id, remote_job_id, progress, input_json, result_json,
		 error_code, error_message, next_retry_at, cancel_requested, remote_cancel_unconfirmed,
		 attempt_count, max_attempts, lease_owner, lease_expires_at, created_at, updated_at, started_at, finished_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`);
repo = repo.replace(`		boolInt(record.CancelRequested), record.AttemptCount, record.MaxAttempts,
		record.LeaseOwner, formatTime(record.LeaseExpiresAt), formatTime(record.CreatedAt),`,
`		boolInt(record.CancelRequested), boolInt(record.CancelledRemoteUnconfirmed), record.AttemptCount, record.MaxAttempts,
		record.LeaseOwner, formatTime(record.LeaseExpiresAt), formatTime(record.CreatedAt),`);
fs.writeFileSync("internal/infrastructure/database/jobs.go", repo);
console.log("repository persists the orphan flag");

// Migration: add the column.
let migration = fs.readFileSync("internal/infrastructure/database/migrations/000003_jobs.sql", "utf8");
migration = migration.replace(`    cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0, 1)),`,
`    cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0, 1)),
    remote_cancel_unconfirmed INTEGER NOT NULL DEFAULT 0 CHECK (remote_cancel_unconfirmed IN (0, 1)),`);
fs.writeFileSync("internal/infrastructure/database/migrations/000003_jobs.sql", migration);
console.log("migration adds the orphan flag column");

// DTO surfaces it.
let dto = fs.readFileSync("internal/desktop/jobs_dto.go", "utf8");
dto = dto.replace(`	if record.Status == job.StatusRemoteOnly {
		dto.ResultRemoteOnly = true
	}`, `	if record.Status == job.StatusRemoteOnly {
		dto.ResultRemoteOnly = true
	}
	// A cancelled job whose remo
