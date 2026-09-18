-- WP-05: artifact staleness, the marker DOMAIN_MODEL section 15 requires.
--
-- This is the "stale 传播基础" the roadmap asks for: a table that records which
-- artifact became stale, why, from which upstream version, and how severe the
-- consequence is. The full Impact Analyzer, the regeneration workflow and the
-- impact graph UI belong to WP-07 and WP-10.
--
-- The table is generic rather than "stale columns on the five version tables"
-- because a stale mark has to name an upstream version that is not always a
-- foreign key (an asset version pointing at a script version, for example), and
-- five parallel columns would make the section 19 integrity query five queries.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP05SplitSQLCompatibility enforces it.

CREATE TABLE artifact_staleness (
    -- artifact_type is the section 15.2 chain's node names.
    artifact_type TEXT NOT NULL CHECK (
        artifact_type IN ('source_document_version', 'chapter', 'story_entity', 'story_event', 'story_relation',
                          'character_state', 'story_skeleton_version', 'adaptation_strategy_version',
                          'script_version', 'scene', 'shot', 'asset_version', 'director_plan_version',
                          'storyboard_version', 'storyboard_item', 'storyboard_panel_version',
                          'workflow_run', 'stage_run', 'canvas_node')
    ),
    artifact_id TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- section 15.2 classifies the consequence: breaking prevents downstream
    -- formal use, review_required allows it to stay but demands a re-review,
    -- and informational only records a notice.
    severity TEXT NOT NULL CHECK (severity IN ('breaking', 'review_required', 'informational')),
    reason TEXT NOT NULL DEFAULT '',
    -- The upstream that caused it. Nullable because a manual recompute can mark
    -- an artifact without naming a single source version.
    upstream_type TEXT NOT NULL DEFAULT '',
    upstream_id TEXT NOT NULL DEFAULT '',
    -- A waiver records that a user chose to keep a stale artifact. Section 15.3
    -- requires a UserGateDecision for that, so the decision id is kept here and
    -- the check on it is the writer's (WP-07).
    waived INTEGER NOT NULL DEFAULT 0 CHECK (waived IN (0, 1)),
    waived_by_decision_id TEXT NOT NULL DEFAULT '',
    waived_reason TEXT NOT NULL DEFAULT '',
    -- cleared_at marks a stale mark that no longer applies because the artifact
    -- was regenerated or the upstream was restored. The row is kept so the
    -- history of what was invalidated survives.
    cleared_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    PRIMARY KEY (artifact_type, artifact_id)
);

CREATE INDEX idx_artifact_staleness_project ON artifact_staleness(project_id, severity, cleared_at);

CREATE INDEX idx_artifact_staleness_upstream ON artifact_staleness(upstream_type, upstream_id);

CREATE INDEX idx_artifact_staleness_open ON artifact_staleness(artifact_type, cleared_at);
