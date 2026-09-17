-- WP-04 fix: per-project import fingerprints.
--
-- An import decides whether to skip a project by asking "has this project's
-- content already been imported". The check needs one row per project, keyed by
-- the content fingerprint, and `legacy_imports` holds one row per run whose
-- fingerprint covers the whole snapshot. A run-level value cannot answer a
-- per-project question, which is why this table exists.
--
-- A row is written only when the project's rows committed, so a failed import
-- leaves no fingerprint and a retry is a retry rather than a duplicate.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator.

CREATE TABLE legacy_project_imports (
    fingerprint TEXT PRIMARY KEY CHECK (length(fingerprint) = 64),
    import_id TEXT NOT NULL REFERENCES legacy_imports(id) ON DELETE CASCADE,
    legacy_project_id TEXT NOT NULL CHECK (length(legacy_project_id) BETWEEN 1 AND 200),
    project_id TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_legacy_project_imports_project ON legacy_project_imports(project_id);

CREATE INDEX idx_legacy_project_imports_import ON legacy_project_imports(import_id);
