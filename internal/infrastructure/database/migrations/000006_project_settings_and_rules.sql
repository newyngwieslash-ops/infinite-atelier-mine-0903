-- WP-05: drama project settings, rules, style guides and provider policies.
--
-- Field names follow docs/DOMAIN_MODEL.md sections 4.3, 4.4 and 4.5. The
-- version vocabulary follows section 2.5 and the revision guard follows 2.3.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP05SplitSQLCompatibility enforces it.

-- One row per project. section 4.3 defines the settings as a value object, so
-- the primary key is the project itself rather than a minted id.
CREATE TABLE project_settings (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    target_platform TEXT NOT NULL DEFAULT '',
    aspect_ratio TEXT NOT NULL DEFAULT '',
    resolution TEXT NOT NULL DEFAULT '',
    expected_episode_count INTEGER NOT NULL DEFAULT 0 CHECK (expected_episode_count >= 0),
    default_episode_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (default_episode_duration_seconds >= 0),
    audience TEXT NOT NULL DEFAULT '',
    content_rating TEXT NOT NULL DEFAULT '',
    adaptation_mode TEXT NOT NULL DEFAULT 'balanced' CHECK (adaptation_mode IN ('faithful', 'balanced', 'aggressive')),
    language TEXT NOT NULL DEFAULT 'zh-CN',
    timezone TEXT NOT NULL DEFAULT '',
    settings_version INTEGER NOT NULL DEFAULT 1 CHECK (settings_version >= 1),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- section 4.4. "immutable" or locked_by_user rules accept only user commands,
-- so the writer must be part of the update path rather than a UI concern. The
-- strength and category vocabularies are closed here and mirrored in
-- internal/domain/project.
CREATE TABLE project_rules (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    category TEXT NOT NULL CHECK (category IN ('story', 'character', 'visual', 'camera', 'audio', 'safety', 'custom')),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    content TEXT NOT NULL DEFAULT '',
    strength TEXT NOT NULL DEFAULT 'advisory' CHECK (strength IN ('advisory', 'required', 'immutable')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    source_type TEXT NOT NULL DEFAULT 'user' CHECK (source_type IN ('user', 'imported', 'agent_suggested')),
    source_id TEXT NOT NULL DEFAULT '',
    locked_by_user INTEGER NOT NULL DEFAULT 0 CHECK (locked_by_user IN (0, 1)),
    deleted_at TEXT NOT NULL DEFAULT '',
    deleted_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_project_rules_project ON project_rules(project_id, category, status);

CREATE INDEX idx_project_rules_locked ON project_rules(project_id, locked_by_user, strength);

-- section 4.5 is versioned: the guide is approved as a whole rather than edited
-- in place, so it carries the section 2.5 version columns. The partial unique
-- index enforces "at most one approved version per parent" in the schema, which
-- is the acceptance item "approved 唯一".
CREATE TABLE project_style_guides (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    visual_style TEXT NOT NULL DEFAULT '',
    palette_json TEXT NOT NULL DEFAULT '',
    lighting TEXT NOT NULL DEFAULT '',
    composition TEXT NOT NULL DEFAULT '',
    camera_language TEXT NOT NULL DEFAULT '',
    negative_constraints TEXT NOT NULL DEFAULT '',
    sound_direction TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (project_id, version_number)
);

CREATE INDEX idx_project_style_guides_project ON project_style_guides(project_id, version_number);

CREATE UNIQUE INDEX idx_project_style_guides_approved ON project_style_guides(project_id) WHERE status = 'approved';

-- "参考资产版本" from section 4.5 is deliberately not a table here. Section 8.6
-- already models it exactly: an asset_usages row with consumer_type
-- 'project_style' and consumer_id set to the style guide version. Creating a
-- second link table would give the same fact two homes, and asset_usages is
-- created in migration 000009 with the foreign key this table could not have
-- (asset_versions is rebuilt there).

-- section 3 lists ProjectProviderPolicy in the project aggregate but section 4
-- defines no field table for it. PRD FR-020 requires drama settings to carry a
-- default model policy and AGENT_CONTRACTS section 13 gives that policy a shape,
-- so the policy object is stored as controlled JSON per section 2.6 ("非核心模型参数")
-- with the layer as a queryable column. ADR-0007 records the decision.
CREATE TABLE project_provider_policies (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    layer TEXT NOT NULL DEFAULT 'default' CHECK (layer IN ('default', 'decision', 'execution', 'supervision', 'embedding')),
    policy_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (project_id, layer)
);

CREATE INDEX idx_project_provider_policies_project ON project_provider_policies(project_id, layer);
