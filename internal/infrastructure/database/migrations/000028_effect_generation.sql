-- 000028_effect_generation.sql — T03's vocabulary: sound-effect synthesis is
-- its own job type and provider capability, not speech with a different role.
--
-- The 2026-09-26 audit traced what reusing `audio_generation` for effects
-- costs: the effect's matched word travels as TTS `text`, the speech endpoint
-- READS IT ALOUD, and the result is filed as an effect. 「脚步」 became the
-- words "footsteps" spoken. A type of its own lets a provider channel refuse
-- what it cannot do and lets a real effect adapter answer the right contract.
--
-- SQLite cannot ALTER a CHECK, so both constrained tables are rebuilt on the
-- staged pattern migration 000023 used: rename, recreate with the widened
-- CHECK, copy rows back, drop the stage. Foreign keys reference these tables
-- by id, and the runner's foreign_keys pragma is ON, so the stage tables carry
-- the same FK shapes before the originals are dropped.

ALTER TABLE generation_jobs RENAME TO generation_jobs_000028_stage;
ALTER TABLE provider_requests RENAME TO provider_requests_000028_stage;

CREATE TABLE generation_jobs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL DEFAULT '',
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id TEXT NOT NULL DEFAULT '',
    job_type TEXT NOT NULL CHECK (job_type IN (
        'image_generation', 'image_edit', 'video_generation', 'audio_generation',
        'effect_generation', 'asset_download',
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

INSERT INTO generation_jobs
SELECT * FROM generation_jobs_000028_stage;

DROP TABLE generation_jobs_000028_stage;

CREATE TABLE provider_requests (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL DEFAULT '',
    agent_run_id TEXT NOT NULL DEFAULT '',
    provider_config_id TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    model_config_id TEXT NOT NULL DEFAULT '',
    capability TEXT NOT NULL CHECK (capability IN ('text', 'image', 'video', 'audio', 'effect', 'embedding')),
    request_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('succeeded', 'failed', 'cancelled')),
    http_status INTEGER,
    input_units INTEGER,
    output_units INTEGER,
    estimated_cost TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL CHECK (latency_ms >= 0),
    error_code TEXT NOT NULL DEFAULT '',
    redacted_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

INSERT INTO provider_requests
SELECT * FROM provider_requests_000028_stage;

DROP TABLE provider_requests_000028_stage;

-- The children are rebuilt with the parent because their foreign keys name
-- generation_jobs(id), which the rename broke.
ALTER TABLE job_attempts RENAME TO job_attempts_000028_stage;
ALTER TABLE job_dependencies RENAME TO job_dependencies_000028_stage;

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

INSERT INTO job_attempts
SELECT * FROM job_attempts_000028_stage;

INSERT INTO job_dependencies
SELECT * FROM job_dependencies_000028_stage;

DROP TABLE job_attempts_000028_stage;
DROP TABLE job_dependencies_000028_stage;

CREATE INDEX idx_generation_jobs_schedule ON generation_jobs(status, priority, next_retry_at);

CREATE INDEX idx_generation_jobs_entity ON generation_jobs(project_id, entity_type, entity_id);

CREATE INDEX idx_job_attempts_job ON job_attempts(generation_job_id, attempt_number);

CREATE INDEX idx_provider_requests_provider ON provider_requests(provider_config_id, created_at);
