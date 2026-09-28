-- 000032_provider_manifests.sql — RP-05.2: versioned declarative Provider
-- Manifests (FR-140's 「本地或自定义 HTTP Manifest Adapter」).
--
-- # What is versioned, and why immutability matters
--
-- A manifest is the PROTOCOL SNAPSHOT a provider config runs on: endpoint
-- paths, the request template, the response mapping, the poll rules. A
-- running job that is part-way through an async submit/poll/fetch cycle must
-- keep reading the protocol it STARTED with even after the user edits the
-- manifest — so manifests are IMMUTABLE once stored, and a config names a
-- version, not a document. Editing produces a new version, the old one stays
-- readable until nothing references it.
--
-- # The schema
--
--   - provider_manifest_versions: one immutable manifest document per
--     (config, version). The content hash is stored beside the document so a
--     reader can verify what it loaded is what was written, the hash of the
--     same content always maps to the same version row (unique constraint),
--     which makes a re-save of an unchanged manifest a no-op rather than a
--     new version.
--   - provider_configs.active_manifest_version_id: the pointer the runtime
--     reads. NULL means "no manifest" — the config runs a builtin adapter,
--     exactly as every pre-000032 row does.
--   - provider_configs.kind: the CHECK widens to admit `gemini_compatible`
--     (RP-02.1's routing needs it persistable) and `manifest` (a config whose
--     adapter IS its manifest). The rebuild keeps every column 000003–000031
--     added.

-- The kind CHECK is widened by rebuilding the table: SQLite cannot ALTER a
-- CHECK constraint. The rebuild preserves every column 000003/000022/000030
-- added, with their checks.
CREATE TABLE provider_manifest_versions (
    id TEXT PRIMARY KEY,
    provider_config_id TEXT NOT NULL,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    -- manifest_json is the validated document, stored rather than referenced:
    -- it is small (bounded by the domain's 256 KiB ceiling) and the row IS the
    -- protocol snapshot.
    manifest_json TEXT NOT NULL,
    -- content_hash is the SHA-256 of manifest_json. Two saves of identical
    -- content map to one row via the unique constraint below, and a reader
    -- can verify the document it loaded.
    content_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (provider_config_id, content_hash),
    UNIQUE (provider_config_id, version_number)
);

ALTER TABLE provider_configs RENAME TO provider_configs_old;

CREATE TABLE provider_configs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('openai_compatible', 'gemini_compatible', 'mock_media', 'manifest')),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 100),
    base_url TEXT NOT NULL,
    secret_ref TEXT NOT NULL,
    local_approved INTEGER NOT NULL DEFAULT 0 CHECK (local_approved IN (0, 1)),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    max_concurrency INTEGER NOT NULL DEFAULT 0 CHECK (max_concurrency >= 0),
    rate_limit_per_minute INTEGER NOT NULL DEFAULT 0 CHECK (rate_limit_per_minute >= 0),
    active_manifest_version_id TEXT REFERENCES provider_manifest_versions(id) ON DELETE SET NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (secret_ref) REFERENCES secret_references(id) ON DELETE RESTRICT
);

INSERT INTO provider_configs
    (id, kind, display_name, base_url, secret_ref, local_approved, enabled, max_concurrency, rate_limit_per_minute, revision, created_at, updated_at)
SELECT id, kind, display_name, base_url, secret_ref, local_approved, enabled, max_concurrency, rate_limit_per_minute, revision, created_at, updated_at
FROM provider_configs_old;

DROP TABLE provider_configs_old;

CREATE INDEX idx_provider_manifest_versions_config
    ON provider_manifest_versions(provider_config_id, version_number DESC);
