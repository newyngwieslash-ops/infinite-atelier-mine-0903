-- WP-04: workspaces, projects, canvas documents/nodes/edges/chats, assets,
-- archived generation history, and the legacy import bookkeeping.
--
-- Field names and constraints follow docs/DOMAIN_MODEL.md §10, §12, §16 and the
-- index list in §18. ADR-0005 records why file_objects doubles as the physical
-- file table and why external ids are UUIDv7. ADR-0006 records the import and
-- backup contracts this schema serves.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP04SplitSQLCompatibility enforces it.

CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    kind TEXT NOT NULL DEFAULT 'local' CHECK (kind IN ('local', 'team_future')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    project_type TEXT NOT NULL CHECK (project_type IN ('free_canvas', 'drama')),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '',
    language TEXT NOT NULL DEFAULT 'zh-CN',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'trashed')),
    cover_asset_version_id TEXT NOT NULL DEFAULT '',
    deleted_at TEXT NOT NULL DEFAULT '',
    deleted_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_projects_workspace_status ON projects(workspace_id, status, updated_at);

CREATE TABLE canvas_documents (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    canvas_kind TEXT NOT NULL DEFAULT 'free' CHECK (canvas_kind IN ('free', 'drama', 'episode', 'storyboard', 'asset')),
    viewport_json TEXT NOT NULL DEFAULT '',
    background_json TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_canvas_documents_project ON canvas_documents(project_id, canvas_kind);

-- A node either projects a domain entity or stands alone. The two halves of a
-- projection must arrive together, which the CHECK enforces so a row cannot
-- describe half a reference.
CREATE TABLE canvas_nodes (
    id TEXT PRIMARY KEY,
    canvas_document_id TEXT NOT NULL REFERENCES canvas_documents(id) ON DELETE CASCADE,
    node_type TEXT NOT NULL CHECK (length(node_type) BETWEEN 1 AND 120),
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id TEXT NOT NULL DEFAULT '',
    entity_version_id TEXT NOT NULL DEFAULT '',
    workflow_run_id TEXT NOT NULL DEFAULT '',
    position_x REAL NOT NULL DEFAULT 0,
    position_y REAL NOT NULL DEFAULT 0,
    width REAL NOT NULL DEFAULT 0 CHECK (width >= 0),
    height REAL NOT NULL DEFAULT 0 CHECK (height >= 0),
    z_index INTEGER NOT NULL DEFAULT 0,
    -- title is not in DOMAIN_MODEL §10.2: the legacy canvas node carries a
    -- user-visible title that AC-CANVAS-004's editing flows act on, and holding
    -- it in ui_state_json would bury a displayed value inside opaque state.
    title TEXT NOT NULL DEFAULT '',
    ui_state_json TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    CHECK ((entity_type = '' AND entity_id = '') OR (entity_type <> '' AND entity_id <> ''))
);

CREATE INDEX idx_canvas_nodes_document ON canvas_nodes(canvas_document_id, z_index);
CREATE INDEX idx_canvas_nodes_entity ON canvas_nodes(canvas_document_id, entity_type, entity_id);

-- Legacy connections carry no relation type. They are imported as 'generic'
-- (PRD FR-130 requires untyped legacy links to survive as generic rather than
-- being dropped) and the original row is kept in legacy_metadata_json.
CREATE TABLE canvas_edges (
    id TEXT PRIMARY KEY,
    canvas_document_id TEXT NOT NULL REFERENCES canvas_documents(id) ON DELETE CASCADE,
    from_node_id TEXT NOT NULL REFERENCES canvas_nodes(id) ON DELETE CASCADE,
    to_node_id TEXT NOT NULL REFERENCES canvas_nodes(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL DEFAULT 'generic' CHECK (
        relation_type IN ('generic', 'contains', 'adapts_to', 'references', 'derived_from',
                          'continues_from', 'generated_by', 'reviewed_by', 'supersedes',
                          'first_frame_of', 'last_frame_of', 'uses_character', 'uses_location',
                          'uses_prop', 'uses_asset', 'appears_in', 'located_in', 'causes',
                          'precedes', 'contradicts')
    ),
    from_port TEXT NOT NULL DEFAULT '',
    to_port TEXT NOT NULL DEFAULT '',
    required INTEGER NOT NULL DEFAULT 0 CHECK (required IN (0, 1)),
    validation_status TEXT NOT NULL DEFAULT 'unknown' CHECK (validation_status IN ('valid', 'invalid', 'stale', 'unknown')),
    metadata_json TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_canvas_edges_document ON canvas_edges(canvas_document_id, from_node_id, to_node_id);

CREATE TABLE canvas_chat_sessions (
    id TEXT PRIMARY KEY,
    canvas_document_id TEXT NOT NULL REFERENCES canvas_documents(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    messages_json TEXT NOT NULL DEFAULT '[]',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_canvas_chat_sessions_document ON canvas_chat_sessions(canvas_document_id, created_at);

CREATE TABLE assets (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    asset_type TEXT NOT NULL CHECK (
        asset_type IN ('character', 'location', 'prop', 'costume', 'style', 'image', 'video', 'audio', 'doc')
    ),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '',
    story_entity_id TEXT NOT NULL DEFAULT '',
    current_approved_version_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'trashed')),
    deleted_at TEXT NOT NULL DEFAULT '',
    deleted_by TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_assets_project_type_status ON assets(project_id, asset_type, status);

CREATE TABLE asset_versions (
    id TEXT PRIMARY KEY,
    asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'review', 'approved', 'rejected', 'superseded', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    negative_prompt TEXT NOT NULL DEFAULT '',
    provider_config_id TEXT NOT NULL DEFAULT '',
    model_config_id TEXT NOT NULL DEFAULT '',
    model_parameters_json TEXT NOT NULL DEFAULT '',
    generation_job_id TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'migration' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (asset_id, version_number)
);

CREATE INDEX idx_asset_versions_asset ON asset_versions(asset_id, version_number);

-- Which stored object plays which role for a version. The FK to file_objects is
-- what makes a reference to uncommitted bytes impossible (ADR-0006).
CREATE TABLE asset_files (
    asset_version_id TEXT NOT NULL REFERENCES asset_versions(id) ON DELETE CASCADE,
    file_hash TEXT NOT NULL REFERENCES file_objects(hash) ON DELETE RESTRICT,
    role TEXT NOT NULL DEFAULT 'primary' CHECK (role IN ('primary', 'thumbnail', 'source', 'attachment')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (asset_version_id, file_hash, role)
);

CREATE INDEX idx_asset_files_hash ON asset_files(file_hash);

-- Legacy generation history is archived here rather than turned into provider
-- audit rows: the audit tables record real calls, and the legacy list has no
-- call metadata. ADR-0006 §7 records the deviation.
CREATE TABLE generation_history (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    legacy_id TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    images_json TEXT NOT NULL DEFAULT '[]',
    success_count INTEGER NOT NULL DEFAULT 0 CHECK (success_count >= 0),
    fail_count INTEGER NOT NULL DEFAULT 0 CHECK (fail_count >= 0),
    generated_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_generation_history_project ON generation_history(project_id, generated_at);

-- One row per import attempt that reached a verdict. A failed attempt keeps its
-- row (status 'failed') so the report survives, but it is never treated as
-- already-imported: only 'completed' rows satisfy the fingerprint check.
CREATE TABLE legacy_imports (
    id TEXT PRIMARY KEY,
    source_fingerprint TEXT NOT NULL CHECK (length(source_fingerprint) = 64),
    source_case TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'initial' CHECK (mode IN ('initial', 'copy', 'precheck')),
    status TEXT NOT NULL CHECK (status IN ('completed', 'failed', 'already_imported')),
    report_json TEXT NOT NULL DEFAULT '',
    warnings_json TEXT NOT NULL DEFAULT '[]',
    legacy_root TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_legacy_imports_fingerprint ON legacy_imports(source_fingerprint, status);

-- The mapping table of docs/DOMAIN_MODEL.md §20.1. One legacy id can map to
-- several new ids when a user imports a project twice in copy mode, so the
-- import is part of the key.
CREATE TABLE legacy_id_map (
    import_id TEXT NOT NULL REFERENCES legacy_imports(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('project', 'node', 'asset', 'media', 'generation_history')),
    legacy_id TEXT NOT NULL CHECK (length(legacy_id) BETWEEN 1 AND 200),
    new_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (import_id, kind, legacy_id)
);

CREATE INDEX idx_legacy_id_map_lookup ON legacy_id_map(kind, legacy_id);

-- The default local workspace is created once, here, because DOMAIN_MODEL §8
-- requires that it always exists and cannot be deleted. The id is a fixed
-- value so every installation and every test addresses the same row.
INSERT INTO workspaces (id, name, kind, created_at, updated_at, revision)
VALUES ('00000000-0000-7000-8000-000000000001', 'Local', 'local', '2026-09-16T00:00:00Z', '2026-09-16T00:00:00Z', 1);
