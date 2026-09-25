-- WP-15 item 7: FR-150's four missing job types, which needs the CHECK widened.
--
-- # Why this migration REBUILDS the table rather than altering it
--
-- `generation_jobs.job_type` has carried `CHECK (job_type IN ('image_generation', 'image_edit',
-- 'video_generation', 'audio_generation', 'asset_download'))` since migration 000003, and PRD
-- FR-150's task list names eleven types. Four of them — thumbnail, import, export, migration — have
-- no value in the CHECK, so a row of one of those types cannot be inserted at all.
--
-- SQLite cannot alter a CHECK in place: the constraint is part of the table's definition, and the
-- only way to change it is to create a new table and copy. ADR-0004 predicted exactly this
-- ("Adding a future state requires a new forward migration to widen the CHECK constraint"), and
-- 000009 established the pattern this file follows: stage the affected tables, drop children before
-- parents, create the new tables, backfill, then drop the staging copies.
--
-- # What is staged, and in what order
--
-- `job_attempts` and `job_dependencies` both reference `generation_jobs(id)` with ON DELETE CASCADE,
-- so they must be staged and dropped BEFORE their parent and recreated AFTER it. The order in this
-- file is: stage all three, drop the children, drop the parent, create the parent, create the
-- children, backfill all three, drop the staging copies.
--
-- # Why ALTER TABLE RENAME is not used
--
-- A rename does not rename the indexes and constraints behind a UNIQUE clause, so a table that came
-- back from one would carry names pointing at a table that no longer exists — and the failure would
-- appear the next time a migration touched one of them rather than here. 000009's header records the
-- same reasoning for its own rebuild.
--
-- # The four types, and what each is FOR
--
--   thumbnail  a derived preview image, which is a job because it is provider or engine work
--   import     a document transfer or parse, which is a job because a large one must not block the UI
--   export     a media composition, which the media stack already runs inside a command
--   migration  a legacy import's data movement, which is a job because a corpus can be large
--
-- Widening the CHECK does NOT make these types runnable: `internal/infrastructure/jobs/runner.go`
-- dispatches on the capability each type needs, and a type with no adapter is refused there rather
-- than here. That split is deliberate — the schema states which vocabulary is legal and the runner
-- states what this build can actually execute — so a type this build cannot run is a value the
-- database accepts and the runner refuses with `unsupported`, which is an honest error rather than a
-- constraint violation that looks like a corrupt row.

-- 1. Stage the three tables.
CREATE TABLE _wp15_generation_jobs_stage AS SELECT * FROM generation_jobs;

CREATE TABLE _wp15_job_attempts_stage AS SELECT * FROM job_attempts;

CREATE TABLE _wp15_job_dependencies_stage AS SELECT * FROM job_dependencies;

-- 2. Drop the children, then the parent.
DROP TABLE job_attempts;

DROP TABLE job_dependencies;

DROP TABLE generation_jobs;

-- 3. The parent, with the widened CHECK. Every other column is 000003's, unchanged: this migration
--    changes exactly one constraint and nothing else, which is what keeps the rebuild's risk to the
--    copy rather than to the shape.
CREATE TABLE generation_jobs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL DEFAULT '',
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id TEXT NOT NULL DEFAULT '',
    job_type TEXT NOT NULL CHECK (job_type IN (
        'image_generation', 'image_edit', 'video_generation', 'audio_generation', 'asset_download',
        'thumbnail', 'import', 'export', 'migration'
    )),
    status TEXT NOT NULL CHECK (
        status IN ('queued', 'running', 'waiting_remote', 'downloading', 'verifying', 'retry_wait',
                   'succeeded', 'remote_only', 'failed', 'cancelled', 'orphaned', 'recovering')
    ),
    priority INTEGER NOT NULL DEFAULT 0,
    idempotency_key TEXT NOT NULL,
    provider_config_id TEXT NOT NULL DEFAULT '',
    model_config_id TEXT NOT NULL DEFAULT '',
    remote_job_id TEXT NOT NULL DEFAULT '',
    progress INTEGER CHECK (progress IS NULL OR (progress >= 0 AND progress <= 100)),
    input_json TEXT NOT NULL DEFAULT '{}',
    result_json TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    next_retry_at TEXT NOT NULL DEFAULT '',
    cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0, 1)),
    remote_cancel_unconfirmed INTEGER NOT NULL DEFAULT 0 CHECK (remote_cancel_unconfirmed IN (0, 1)),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts >= 1),
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_expires_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT NOT NULL DEFAULT '',
    finished_at TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (project_id, idempotency_key)
);

-- 4. The children, exactly as 000003 defined them.
CREATE TABLE job_attempts (
    id TEXT PRIMARY KEY,
    generation_job_id TEXT NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL CHECK (attempt_number >= 1),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    provider_request_id TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT '',
    UNIQUE (generation_job_id, attempt_number)
);

CREATE TABLE job_dependencies (
    job_id TEXT NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
    depends_on_job_id TEXT NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
    condition TEXT NOT NULL CHECK (condition IN ('success', 'completed', 'approved')),
    PRIMARY KEY (job_id, depends_on_job_id)
);

-- 5. Backfill, parent first so the children's foreign keys resolve.
INSERT INTO generation_jobs (
    id, project_id, entity_type, entity_id, job_type, status, priority, idempotency_key,
    provider_config_id, model_config_id, remote_job_id, progress, input_json, result_json,
    error_code, error_message, next_retry_at, cancel_requested, remote_cancel_unconfirmed,
    attempt_count, max_attempts, lease_owner, lease_expires_at, created_at, updated_at,
    started_at, finished_at, revision)
SELECT id, project_id, entity_type, entity_id, job_type, status, priority, idempotency_key,
    provider_config_id, model_config_id, remote_job_id, progress, input_json, result_json,
    error_code, error_message, next_retry_at, cancel_requested, remote_cancel_unconfirmed,
    attempt_count, max_attempts, lease_owner, lease_expires_at, created_at, updated_at,
    started_at, finished_at, revision
FROM _wp15_generation_jobs_stage;

INSERT INTO job_attempts (
    id, generation_job_id, attempt_number, status, provider_request_id, error_code,
    error_message, started_at, finished_at)
SELECT id, generation_job_id, attempt_number, status, provider_request_id, error_code,
    error_message, started_at, finished_at
FROM _wp15_job_attempts_stage;

INSERT INTO job_dependencies (job_id, depends_on_job_id, condition)
SELECT job_id, depends_on_job_id, condition FROM _wp15_job_dependencies_stage;

-- 6. The indexes, recreated because dropping the table dropped them with it.
CREATE INDEX idx_generation_jobs_schedule ON generation_jobs(status, priority, next_retry_at);

CREATE INDEX idx_generation_jobs_entity ON generation_jobs(project_id, entity_type, entity_id);

CREATE INDEX idx_job_attempts_job ON job_attempts(generation_job_id, attempt_number);

-- 7. The staging copies go last, so a failure anywhere above leaves the data recoverable.
DROP TABLE _wp15_generation_jobs_stage;

DROP TABLE _wp15_job_attempts_stage;

DROP TABLE _wp15_job_dependencies_stage;
