-- WP-05: episodes, the three script stages and the scene/dialogue/shot model.
--
-- Field names follow docs/DOMAIN_MODEL.md section 7, indexes follow section 18.
-- The generated content of these tables belongs to WP-06 and WP-08, while this
-- migration only creates the structure, its vocabularies and its constraints.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP05SplitSQLCompatibility enforces it.

CREATE TABLE episodes (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    season_number INTEGER NOT NULL DEFAULT 1 CHECK (season_number >= 1),
    episode_number INTEGER NOT NULL CHECK (episode_number >= 1),
    title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'planning' CHECK (status IN ('planning', 'writing', 'approved', 'production', 'completed')),
    source_chapter_start_id TEXT NOT NULL DEFAULT '',
    source_chapter_end_id TEXT NOT NULL DEFAULT '',
    target_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (target_duration_seconds >= 0),
    current_story_skeleton_version_id TEXT NOT NULL DEFAULT '',
    current_adaptation_strategy_version_id TEXT NOT NULL DEFAULT '',
    current_script_version_id TEXT NOT NULL DEFAULT '',
    deleted_at TEXT NOT NULL DEFAULT '',
    deleted_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (project_id, season_number, episode_number)
);

CREATE INDEX idx_episodes_project ON episodes(project_id, season_number, episode_number);

-- section 7.2. turning_points_json is a controlled structure, while the
-- selected story events are a link table because section 2.6 forbids carrying a
-- "可查询状态" in JSON.
CREATE TABLE story_skeleton_versions (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    opening_hook TEXT NOT NULL DEFAULT '',
    core_conflict TEXT NOT NULL DEFAULT '',
    turning_points_json TEXT NOT NULL DEFAULT '',
    climax TEXT NOT NULL DEFAULT '',
    ending_hook TEXT NOT NULL DEFAULT '',
    estimated_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (estimated_duration_seconds >= 0),
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (episode_id, version_number)
);

CREATE INDEX idx_story_skeleton_versions_episode ON story_skeleton_versions(episode_id, version_number);

CREATE UNIQUE INDEX idx_story_skeleton_versions_approved ON story_skeleton_versions(episode_id) WHERE status = 'approved';

CREATE TABLE story_skeleton_event_links (
    skeleton_version_id TEXT NOT NULL REFERENCES story_skeleton_versions(id) ON DELETE CASCADE,
    story_event_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    PRIMARY KEY (skeleton_version_id, story_event_id)
);

CREATE INDEX idx_story_skeleton_event_links_event ON story_skeleton_event_links(story_event_id);

-- section 7.3. The retained, merged, removed and reordered event lists are
-- relations to StoryEvent, not Markdown ("列表使用关联表或受控结构，不能只保存
-- Markdown"). The link table carries which treatment each event received.
CREATE TABLE adaptation_strategy_versions (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    strategy_summary TEXT NOT NULL DEFAULT '',
    adaptation_mode TEXT NOT NULL DEFAULT 'balanced' CHECK (adaptation_mode IN ('faithful', 'balanced', 'aggressive')),
    merged_event_groups_json TEXT NOT NULL DEFAULT '',
    original_additions TEXT NOT NULL DEFAULT '',
    rationale TEXT NOT NULL DEFAULT '',
    risks TEXT NOT NULL DEFAULT '',
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (episode_id, version_number)
);

CREATE INDEX idx_adaptation_strategy_versions_episode ON adaptation_strategy_versions(episode_id, version_number);

CREATE UNIQUE INDEX idx_adaptation_strategy_versions_approved ON adaptation_strategy_versions(episode_id) WHERE status = 'approved';

CREATE TABLE adaptation_strategy_event_links (
    strategy_version_id TEXT NOT NULL REFERENCES adaptation_strategy_versions(id) ON DELETE CASCADE,
    story_event_id TEXT NOT NULL,
    treatment TEXT NOT NULL CHECK (treatment IN ('retained', 'removed', 'reordered')),
    ordinal INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    PRIMARY KEY (strategy_version_id, story_event_id)
);

CREATE INDEX idx_adaptation_strategy_event_links_event ON adaptation_strategy_event_links(story_event_id);

-- section 7.4. Script is the stable identity and ScriptVersion the content.
CREATE TABLE scripts (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    current_version_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (episode_id)
);

-- section 7.5. A script version names the skeleton and strategy it was adapted
-- from, which is what the stale chain later walks.
CREATE TABLE script_versions (
    id TEXT PRIMARY KEY,
    script_id TEXT NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    story_skeleton_version_id TEXT NOT NULL DEFAULT '',
    adaptation_strategy_version_id TEXT NOT NULL DEFAULT '',
    estimated_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (estimated_duration_seconds >= 0),
    summary TEXT NOT NULL DEFAULT '',
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (script_id, version_number)
);

CREATE INDEX idx_script_versions_script ON script_versions(script_id, version_number);

CREATE UNIQUE INDEX idx_script_versions_approved ON script_versions(script_id) WHERE status = 'approved';

-- section 7.6. A scene belongs to one script version, so its ordinal is unique
-- within that version rather than within the script.
CREATE TABLE scenes (
    id TEXT PRIMARY KEY,
    script_version_id TEXT NOT NULL REFERENCES script_versions(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    scene_number TEXT NOT NULL DEFAULT '',
    slugline TEXT NOT NULL DEFAULT '',
    interior_exterior TEXT NOT NULL DEFAULT 'OTHER' CHECK (interior_exterior IN ('INT', 'EXT', 'INT_EXT', 'OTHER')),
    location_entity_id TEXT NOT NULL DEFAULT '',
    time_of_day TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    dramatic_goal TEXT NOT NULL DEFAULT '',
    estimated_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (estimated_duration_seconds >= 0),
    source_story_event_id TEXT NOT NULL DEFAULT '',
    is_original_adaptation INTEGER NOT NULL DEFAULT 0 CHECK (is_original_adaptation IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (script_version_id, ordinal)
);

CREATE INDEX idx_scenes_script_version_ordinal ON scenes(script_version_id, ordinal);

CREATE TABLE dialogue_lines (
    id TEXT PRIMARY KEY,
    scene_id TEXT NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    line_type TEXT NOT NULL DEFAULT 'dialogue' CHECK (line_type IN ('dialogue', 'narration', 'action', 'transition', 'note')),
    character_entity_id TEXT NOT NULL DEFAULT '',
    text TEXT NOT NULL DEFAULT '',
    emotion TEXT NOT NULL DEFAULT '',
    performance_note TEXT NOT NULL DEFAULT '',
    source_story_event_id TEXT NOT NULL DEFAULT '',
    locked INTEGER NOT NULL DEFAULT 0 CHECK (locked IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (scene_id, ordinal)
);

CREATE INDEX idx_dialogue_lines_scene ON dialogue_lines(scene_id, ordinal);

-- section 7.8. A shot exists from the draft stage onward and is refined by the
-- storyboard workflow, so it carries its own revision. Its asset references are
-- asset_usages rows (section 8.6), never a column here.
CREATE TABLE shots (
    id TEXT PRIMARY KEY,
    scene_id TEXT NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    shot_number TEXT NOT NULL DEFAULT '',
    shot_size TEXT NOT NULL DEFAULT '',
    camera_angle TEXT NOT NULL DEFAULT '',
    camera_movement TEXT NOT NULL DEFAULT '',
    estimated_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (estimated_duration_seconds >= 0),
    visual_description TEXT NOT NULL DEFAULT '',
    action_description TEXT NOT NULL DEFAULT '',
    audio_intent TEXT NOT NULL DEFAULT '',
    continuity_notes TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (scene_id, ordinal)
);

CREATE INDEX idx_shots_scene_ordinal ON shots(scene_id, ordinal);
