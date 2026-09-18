-- WP-05: the asset aggregate rebuilt to the documented vocabulary, plus
-- relations, usages and aliases.
--
-- Why the rebuild: SQLite cannot alter a CHECK constraint, and migration
-- 000004 shipped three vocabularies that the specification contradicts.
--
-- 1. assets.asset_type lacked the PRD FR-050 production types (vehicle,
--    creature, style_reference, derived_asset). An asset of those kinds could
--    not be stored at all.
-- 2. asset_versions.status used 'review' where DOMAIN_MODEL section 2.5 says
--    'under_review', and lacked candidate and deprecated.
-- 3. asset_files.role lacked the section 8.4 roles (reference, mask,
--    first_frame, last_frame).
--
-- The rebuild also adds the columns section 8.2 and 8.4 list that 000004 did
-- not create. ADR-0007 records the vocabulary rulings.
--
-- Mechanics: staging copies, then children before parents, then new tables,
-- then backfill. ALTER TABLE RENAME is deliberately not used because a rename
-- does not rename the implicit indexes behind a UNIQUE constraint, so the old
-- autoindex name would collide with the one the new table creates.
-- PRAGMA foreign_keys is not toggled: it is a no-op inside a transaction, so
-- correctness comes from the drop order instead. The runner applies this file
-- in one transaction, and the application snapshots the database before any
-- pending migration runs, so a failure leaves the previous database intact.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP05SplitSQLCompatibility enforces it.

CREATE TABLE _wp05_assets_stage AS SELECT * FROM assets;

CREATE TABLE _wp05_asset_versions_stage AS SELECT * FROM asset_versions;

CREATE TABLE _wp05_asset_files_stage AS SELECT * FROM asset_files;

DROP TABLE asset_files;

DROP TABLE asset_versions;

DROP TABLE assets;

-- section 8.1 plus the FR-050 per-asset fields. asset_type carries the union of
-- the section 8.1 list and the FR-050 production list: 'style' is the generic
-- style asset of section 8.1 and 'style_reference' is FR-050's StyleReference,
-- which are different things and both kept.
CREATE TABLE assets (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    asset_type TEXT NOT NULL CHECK (
        asset_type IN ('character', 'location', 'prop', 'costume', 'vehicle', 'creature',
                       'style', 'style_reference', 'derived_asset', 'image', 'video', 'audio', 'doc')
    ),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '',
    story_entity_id TEXT NOT NULL DEFAULT '',
    current_approved_version_id TEXT NOT NULL DEFAULT '',
    structured_attributes_json TEXT NOT NULL DEFAULT '',
    inviolable_constraints TEXT NOT NULL DEFAULT '',
    negative_constraints TEXT NOT NULL DEFAULT '',
    prompt_template TEXT NOT NULL DEFAULT '',
    applies_from_episode_number INTEGER NOT NULL DEFAULT 0 CHECK (applies_from_episode_number >= 0),
    applies_to_episode_number INTEGER NOT NULL DEFAULT 0 CHECK (applies_to_episode_number >= 0),
    source_story_event_id TEXT NOT NULL DEFAULT '',
    source_script_version_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'trashed')),
    deleted_at TEXT NOT NULL DEFAULT '',
    deleted_by TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_assets_project_type_status ON assets(project_id, asset_type, status);

CREATE INDEX idx_assets_story_entity ON assets(story_entity_id);

-- section 6.2 requires a canonical name and its aliases without silently
-- merging duplicates, and FR-050 lists "名称、别名" per asset. The same rule
-- applies: aliases are rows, and a collision is reported rather than merged.
CREATE TABLE asset_aliases (
    id TEXT PRIMARY KEY,
    asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    alias TEXT NOT NULL CHECK (length(alias) BETWEEN 1 AND 200),
    created_at TEXT NOT NULL
);

CREATE INDEX idx_asset_aliases_asset ON asset_aliases(asset_id);

CREATE INDEX idx_asset_aliases_lookup ON asset_aliases(alias);

-- section 8.2 plus the section 2.5 version columns. The partial unique index is
-- the "approved 唯一" acceptance item expressed in the schema.
CREATE TABLE asset_versions (
    id TEXT PRIMARY KEY,
    asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    parent_asset_version_id TEXT NOT NULL DEFAULT '',
    variant_type TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    negative_prompt TEXT NOT NULL DEFAULT '',
    provider_config_id TEXT NOT NULL DEFAULT '',
    model_config_id TEXT NOT NULL DEFAULT '',
    model_parameters_json TEXT NOT NULL DEFAULT '',
    seed TEXT NOT NULL DEFAULT '',
    generation_job_id TEXT NOT NULL DEFAULT '',
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    legacy_metadata_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (asset_id, version_number)
);

CREATE INDEX idx_asset_versions_asset ON asset_versions(asset_id, version_number);

CREATE UNIQUE INDEX idx_asset_versions_approved ON asset_versions(asset_id) WHERE status = 'approved';

-- section 8.4. The file is identified by its content hash, which is the
-- physical file key per ADR-0005, so a link can only name bytes that were
-- actually committed.
CREATE TABLE asset_files (
    id TEXT PRIMARY KEY,
    asset_version_id TEXT NOT NULL REFERENCES asset_versions(id) ON DELETE CASCADE,
    file_hash TEXT NOT NULL REFERENCES file_objects(hash) ON DELETE RESTRICT,
    role TEXT NOT NULL DEFAULT 'primary' CHECK (
        role IN ('primary', 'thumbnail', 'reference', 'mask', 'first_frame', 'last_frame', 'source')
    ),
    ordinal INTEGER NOT NULL DEFAULT 0 CHECK (ordinal >= 0),
    created_at TEXT NOT NULL,
    UNIQUE (asset_version_id, file_hash, role)
);

CREATE INDEX idx_asset_files_hash ON asset_files(file_hash);

CREATE INDEX idx_asset_files_version ON asset_files(asset_version_id, ordinal);

-- section 8.5. Lineage between versions, which is what makes a derived asset
-- traceable to its parent and the reason it changed.
CREATE TABLE asset_relations (
    id TEXT PRIMARY KEY,
    source_asset_version_id TEXT NOT NULL REFERENCES asset_versions(id) ON DELETE CASCADE,
    target_asset_version_id TEXT NOT NULL REFERENCES asset_versions(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL CHECK (relation_type IN ('derived_from', 'variant_of', 'replaces', 'references', 'supersedes')),
    created_at TEXT NOT NULL,
    UNIQUE (source_asset_version_id, target_asset_version_id, relation_type)
);

CREATE INDEX idx_asset_relations_source ON asset_relations(source_asset_version_id, relation_type);

CREATE INDEX idx_asset_relations_target ON asset_relations(target_asset_version_id, relation_type);

-- section 8.6. consumer_id is polymorphic on purpose (scene, shot, panel,
-- project_style, job, export), so it carries no foreign key. The unique
-- constraint is the one the section names.
CREATE TABLE asset_usages (
    id TEXT PRIMARY KEY,
    asset_version_id TEXT NOT NULL REFERENCES asset_versions(id) ON DELETE CASCADE,
    consumer_type TEXT NOT NULL CHECK (consumer_type IN ('project_style', 'scene', 'shot', 'storyboard_panel', 'job', 'export')),
    consumer_id TEXT NOT NULL,
    usage_role TEXT NOT NULL DEFAULT 'reference',
    required INTEGER NOT NULL DEFAULT 0 CHECK (required IN (0, 1)),
    created_at TEXT NOT NULL,
    UNIQUE (asset_version_id, consumer_type, consumer_id, usage_role)
);

CREATE INDEX idx_asset_usages_consumer ON asset_usages(consumer_type, consumer_id);

CREATE INDEX idx_asset_usages_version ON asset_usages(asset_version_id);

-- Backfill. Every old column is carried over and the three vocabulary changes
-- are applied on the way in: 'review' becomes 'under_review', and the new
-- columns take their defaults.
INSERT INTO assets (
    id, project_id, asset_type, name, description, story_entity_id,
    current_approved_version_id, status, deleted_at, deleted_by,
    legacy_metadata_json, created_at, updated_at, revision
)
SELECT
    id, project_id, asset_type, name, description, story_entity_id,
    current_approved_version_id, status, deleted_at, deleted_by,
    legacy_metadata_json, created_at, updated_at, revision
FROM _wp05_assets_stage;

INSERT INTO asset_versions (
    id, asset_id, version_number, status, based_on_version_id, prompt,
    negative_prompt, provider_config_id, model_config_id, model_parameters_json,
    generation_job_id, metadata_json, created_by_type, legacy_metadata_json, created_at
)
SELECT
    id, asset_id, version_number,
    CASE WHEN status = 'review' THEN 'under_review' ELSE status END,
    based_on_version_id, prompt,
    negative_prompt, provider_config_id, model_config_id, model_parameters_json,
    generation_job_id, metadata_json, created_by_type, legacy_metadata_json, created_at
FROM _wp05_asset_versions_stage;

-- The old table had no id column, so one is minted here in the documented
-- UUIDv7 layout (ADR-0005): 48-bit millisecond timestamp, version nibble 7,
-- 12 bits of randomness, the RFC 4122 variant, then 62 more random bits.
-- The timestamp half comes from the row's own created_at, so a migrated link
-- keeps a time-ordered identity rather than being stamped with the migration
-- time. The column lengths below are written out so the 8-4-4-4-12 grouping is
-- checkable by eye: millis contributes 8+4, then '7' plus 3 random hex, then a
-- variant nibble plus 3 random hex, then 12 random hex.
-- TestWP05AssetBackfillMintsValidIdentifiers asserts every value passes the
-- application's id.Valid, which is what proves the layout.
INSERT INTO asset_files (id, asset_version_id, file_hash, role, ordinal, created_at)
SELECT
    printf('%s-%s-7%s-%s%s-%s',
        substr(printf('%012x', COALESCE(CAST(strftime('%s', created_at) AS INTEGER), 0) * 1000), 1, 8),
        substr(printf('%012x', COALESCE(CAST(strftime('%s', created_at) AS INTEGER), 0) * 1000), 9, 4),
        substr(lower(hex(randomblob(2))), 2, 3),
        substr('89ab', (random() & 3) + 1, 1),
        substr(lower(hex(randomblob(2))), 2, 3),
        lower(hex(randomblob(6)))),
    asset_version_id, file_hash, role, 0, created_at
FROM _wp05_asset_files_stage;

DROP TABLE _wp05_asset_files_stage;

DROP TABLE _wp05_asset_versions_stage;

DROP TABLE _wp05_assets_stage;
