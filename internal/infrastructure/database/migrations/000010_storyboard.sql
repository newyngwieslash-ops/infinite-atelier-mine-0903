-- WP-05: director plan, storyboard table and storyboard panels.
--
-- Field names follow docs/DOMAIN_MODEL.md section 9, indexes follow section 18.
-- Generating the content of these tables belongs to WP-09, while this migration
-- creates the structure, its vocabularies and its constraints.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP05SplitSQLCompatibility enforces it.

-- section 9.1. "场次/镜头覆盖优先通过子表保存，MVP 可使用受控 JSON" is the
-- licence for shot_overrides_json.
CREATE TABLE director_plan_versions (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    script_version_id TEXT NOT NULL REFERENCES script_versions(id) ON DELETE RESTRICT,
    visual_rhythm TEXT NOT NULL DEFAULT '',
    camera_language TEXT NOT NULL DEFAULT '',
    color_lighting TEXT NOT NULL DEFAULT '',
    staging TEXT NOT NULL DEFAULT '',
    continuity_rules TEXT NOT NULL DEFAULT '',
    audio_direction TEXT NOT NULL DEFAULT '',
    shot_overrides_json TEXT NOT NULL DEFAULT '',
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (episode_id, version_number)
);

CREATE INDEX idx_director_plan_versions_episode ON director_plan_versions(episode_id, version_number);

CREATE UNIQUE INDEX idx_director_plan_versions_approved ON director_plan_versions(episode_id) WHERE status = 'approved';

-- section 9.2. Storyboard is the identity, while versions carry the content.
CREATE TABLE storyboards (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    current_version_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (episode_id)
);

-- section 9.3. The version names both the script and the director plan it was
-- derived from, which is what the stale chain walks.
CREATE TABLE storyboard_versions (
    id TEXT PRIMARY KEY,
    storyboard_id TEXT NOT NULL REFERENCES storyboards(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    script_version_id TEXT NOT NULL REFERENCES script_versions(id) ON DELETE RESTRICT,
    director_plan_version_id TEXT NOT NULL REFERENCES director_plan_versions(id) ON DELETE RESTRICT,
    based_on_version_id TEXT NOT NULL DEFAULT '',
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (storyboard_id, version_number)
);

CREATE INDEX idx_storyboard_versions_storyboard ON storyboard_versions(storyboard_id, version_number);

CREATE UNIQUE INDEX idx_storyboard_versions_approved ON storyboard_versions(storyboard_id) WHERE status = 'approved';

-- section 9.4. "StoryboardItem 必须唯一对应本版本的 Shot" is the unique
-- constraint below: one row per shot per storyboard version.
CREATE TABLE storyboard_items (
    id TEXT PRIMARY KEY,
    storyboard_version_id TEXT NOT NULL REFERENCES storyboard_versions(id) ON DELETE CASCADE,
    shot_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    shot_size TEXT NOT NULL DEFAULT '',
    camera_angle TEXT NOT NULL DEFAULT '',
    camera_movement TEXT NOT NULL DEFAULT '',
    duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0),
    visual_description TEXT NOT NULL DEFAULT '',
    action_description TEXT NOT NULL DEFAULT '',
    dialogue_audio_summary TEXT NOT NULL DEFAULT '',
    continuity_notes TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (storyboard_version_id, shot_id),
    UNIQUE (storyboard_version_id, ordinal)
);

CREATE INDEX idx_storyboard_items_version ON storyboard_items(storyboard_version_id, ordinal);

CREATE INDEX idx_storyboard_items_shot ON storyboard_items(shot_id);

-- section 9.5. The approved image is an asset version, and section 2.6 forbids
-- carrying a version relation in JSON, so reference_policy_json stays policy
-- metadata while the approval is a column.
CREATE TABLE storyboard_panel_versions (
    id TEXT PRIMARY KEY,
    storyboard_item_id TEXT NOT NULL REFERENCES storyboard_items(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    visual_prompt TEXT NOT NULL DEFAULT '',
    negative_prompt TEXT NOT NULL DEFAULT '',
    reference_policy_json TEXT NOT NULL DEFAULT '',
    approved_image_asset_version_id TEXT NOT NULL DEFAULT '',
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (storyboard_item_id, version_number)
);

CREATE INDEX idx_storyboard_panel_versions_item ON storyboard_panel_versions(storyboard_item_id, version_number);

CREATE UNIQUE INDEX idx_storyboard_panel_versions_approved ON storyboard_panel_versions(storyboard_item_id) WHERE status = 'approved';
