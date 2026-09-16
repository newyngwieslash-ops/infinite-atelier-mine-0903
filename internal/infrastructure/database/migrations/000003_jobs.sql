CREATE TABLE generation_jobs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL DEFAULT '',
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id TEXT NOT NULL DEFAULT '',
    job_type TEXT NOT NULL CHECK (job_type IN ('image_generation', 'image_edit', 'video_generation', 'audio_generation', 'asset_download')),
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'waiting_remote', 'downloading', 'verifying', 'retry_wait', 'succeeded', 'remote_only', 'failed', 'cancelled', 'orphaned', 'recovering')),
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

CREATE INDEX idx_generation_jobs_schedule ON generation_jobs(status, priority, next_retry_at);

CREATE INDEX idx_generation_jobs_entity ON generation_jobs(project_id, entity_type, entity_id);

CREATE INDEX idx_job_attempts_job ON job_attempts(generation_job_id, attempt_number);

ALTER TABLE provider_requests RENAME TO provider_requests_old;

CREATE TABLE provider_requests (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL DEFAULT '',
    agent_run_id TEXT NOT NULL DEFAULT '',
    provider_config_id TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    model_config_id TEXT NOT NULL DEFAULT '',
    capability TEXT NOT NULL CHECK (capability IN ('text', 'image', 'video', 'audio', 'embedding')),
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

INSERT INTO provider_requests (id, provider_config_id, model, capability, request_id, status, http_status, input_units, output_units, estimated_cost, latency_ms, error_code, created_at)
SELECT id, provider_id, model, capability, request_id, status, http_status, input_units, output_units, estimated_cost, latency_ms, error_code, created_at FROM provider_requests_old;

DROP TABLE provider_requests_old;

CREATE INDEX idx_provider_requests_provider ON provider_requests(provider_config_id, created_at);

ALTER TABLE provider_configs RENAME TO provider_configs_old;

CREATE TABLE provider_configs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('openai_compatible', 'gemini_compatible', 'mock_media')),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 100),
    base_url TEXT NOT NULL,
    secret_ref TEXT NOT NULL,
    local_approved INTEGER NOT NULL DEFAULT 0 CHECK (local_approved IN (0, 1)),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (secret_ref) REFERENCES secret_references(id) ON DELETE RESTRICT
);

INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, local_approved, enabled, revision, created_at, updated_at)
SELECT id, kind, display_name, base_url, secret_ref, local_approved, enabled, revision, created_at, updated_at FROM provider_configs_old;

DROP TABLE provider_configs_old;
