CREATE TABLE secret_references (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL UNIQUE,
    secret_kind TEXT NOT NULL CHECK (secret_kind IN ('api_key')),
    display_hint TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('configured', 'missing', 'unavailable')) DEFAULT 'missing',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE provider_configs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('openai_compatible')),
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

CREATE TABLE provider_requests (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    capability TEXT NOT NULL CHECK (capability IN ('text')),
    model TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('succeeded', 'failed', 'cancelled')),
    http_status INTEGER,
    latency_ms INTEGER NOT NULL CHECK (latency_ms >= 0),
    request_id TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    input_units INTEGER,
    output_units INTEGER,
    estimated_cost TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_provider_requests_provider ON provider_requests(provider_id, created_at);
