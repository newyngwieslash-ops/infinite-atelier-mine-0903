-- WP-06: the columns document import needs, and the entity vocabulary widened to
-- PRD FR-030's list.
--
-- Three changes, and the first is the one that needs care.
--
-- 1. story_entities.entity_type gains 'relationship' and 'timeline_marker'.
--    SQLite cannot alter a CHECK constraint, so the table is rebuilt. Three
--    tables reference it with ON DELETE CASCADE, so the rebuild stages them all
--    and drops children before parents: dropping story_entities first would
--    cascade the children's rows away.
--
--    Why those two values, and why not a third. PRD FR-030 lists nine entity
--    kinds, while migration 000007 pinned six. 'relationship' and
--    'timeline_marker' are the two the extraction pipeline can actually produce
--    and store. The
--    PRD's 'PropState' is deliberately NOT added: section 6.8 already models
--    character state as a sparse controlled-JSON row, the schema has a 'prop'
--    entity for the object itself, and the PRD gives no fields for a separate
--    prop-state table. ADR-0010 records the mapping, including that
--    PRD's 'Relationship' is normally the story_relations table rather than an
--    entity, and that the entity value exists for a relationship the text names
--    as a thing in its own right.
--
-- 2. source_document_versions.source_hash records the hash of the ORIGINAL file,
--    as distinct from content_hash, which is the hash of the normalized text.
--    PRD FR-020 requires a duplicate-import warning ("同一文件重复导入时给出明确
--    提示") and DOMAIN_MODEL section 5.2 says "相同内容哈希需提示重复". The two
--    hashes answer different questions: the same source file re-exported with
--    different line endings has the same source_hash and a different
--    content_hash, and a user who renamed a file needs the source_hash to be
--    told it is the one they already imported.
--
-- 3. chapters.source_kind records how a boundary was decided ('heading', 'regex',
--    'whole' or 'manual'). Section 2.6 forbids carrying a queryable state in
--    JSON, and "did the user edit this boundary" is exactly that: the import
--    report and the staleness walk both ask it.
--
-- ALTER TABLE ADD COLUMN is used for the two new columns because a constant
-- default is all they need, which keeps the rebuild to the one table that
-- genuinely requires it.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP05SplitSQLCompatibility enforces it.

-- Stage the four tables before dropping anything. These copies are what makes
-- the rebuild lossless: the drops below destroy the originals, and the backfill
-- at the end of this file restores every row from here.
CREATE TABLE _wp06_story_entities_stage AS SELECT * FROM story_entities;

CREATE TABLE _wp06_story_entity_aliases_stage AS SELECT * FROM story_entity_aliases;

CREATE TABLE _wp06_story_event_participants_stage AS SELECT * FROM story_event_participants;

CREATE TABLE _wp06_character_states_stage AS SELECT * FROM character_states;

-- Children first. A probe against this schema showed that dropping the parent
-- first DOES cascade the children's rows away, so the order is deliberate
-- defence in depth. It is not what preserves the data, though: the staged copies
-- taken above are, and they are why the rebuild survives either order. Keeping
-- the children first means the rebuild does not depend on the cascade firing at
-- all, which matters because a future migration could turn foreign keys off.
--
-- PRAGMA foreign_keys is not toggled because it is a no-op inside a transaction
-- and the runner applies this file in one.
DROP TABLE story_entity_aliases;

DROP TABLE story_event_participants;

DROP TABLE character_states;

DROP TABLE story_entities;

-- The rebuilt entity table: the same columns in the same order, with the two
-- new entity kinds accepted.
CREATE TABLE story_entities (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL CHECK (
        entity_type IN ('character', 'location', 'organization', 'prop', 'concept', 'time',
                        'relationship', 'timeline_marker')
    ),
    canonical_name TEXT NOT NULL CHECK (length(canonical_name) BETWEEN 1 AND 200),
    status TEXT NOT NULL DEFAULT 'candidate' CHECK (status IN ('candidate', 'accepted', 'rejected', 'locked')),
    source_scope TEXT NOT NULL DEFAULT 'original' CHECK (source_scope IN ('original', 'adaptation', 'user')),
    current_profile_version_id TEXT NOT NULL DEFAULT '',
    deleted_at TEXT NOT NULL DEFAULT '',
    deleted_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_story_entities_project_type_name ON story_entities(project_id, entity_type, canonical_name);

-- The three children, unchanged apart from being recreated after their parent.
CREATE TABLE story_entity_aliases (
    id TEXT PRIMARY KEY,
    story_entity_id TEXT NOT NULL REFERENCES story_entities(id) ON DELETE CASCADE,
    alias TEXT NOT NULL CHECK (length(alias) BETWEEN 1 AND 200),
    source_chapter_id TEXT NOT NULL DEFAULT '',
    source_start_offset INTEGER,
    source_end_offset INTEGER,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_story_entity_aliases_entity ON story_entity_aliases(story_entity_id);

CREATE TABLE story_event_participants (
    story_event_id TEXT NOT NULL REFERENCES story_events(id) ON DELETE CASCADE,
    story_entity_id TEXT NOT NULL REFERENCES story_entities(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('actor', 'target', 'witness', 'owner', 'affected', 'other')),
    state_before TEXT NOT NULL DEFAULT '',
    state_after TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (story_event_id, story_entity_id, role)
);

CREATE INDEX idx_story_event_participants_entity ON story_event_participants(story_entity_id);

CREATE TABLE character_states (
    id TEXT PRIMARY KEY,
    character_entity_id TEXT NOT NULL REFERENCES story_entities(id) ON DELETE CASCADE,
    from_event_order INTEGER NOT NULL DEFAULT 0,
    to_event_order INTEGER,
    appearance_json TEXT NOT NULL DEFAULT '',
    costume_asset_version_id TEXT NOT NULL DEFAULT '',
    injuries_json TEXT NOT NULL DEFAULT '',
    possessions_json TEXT NOT NULL DEFAULT '',
    relationship_state_json TEXT NOT NULL DEFAULT '',
    source_fact_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'candidate' CHECK (status IN ('candidate', 'accepted', 'rejected', 'locked')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_character_states_character ON character_states(character_entity_id, from_event_order);

-- Backfill the parent first, then the children that reference it.
INSERT INTO story_entities (
    id, project_id, entity_type, canonical_name, status, source_scope,
    current_profile_version_id, deleted_at, deleted_by, created_at, updated_at, revision
)
SELECT id, project_id, entity_type, canonical_name, status, source_scope,
       current_profile_version_id, deleted_at, deleted_by, created_at, updated_at, revision
FROM _wp06_story_entities_stage;

INSERT INTO story_entity_aliases (
    id, story_entity_id, alias, source_chapter_id, source_start_offset, source_end_offset, created_at
)
SELECT id, story_entity_id, alias, source_chapter_id, source_start_offset, source_end_offset, created_at
FROM _wp06_story_entity_aliases_stage;

INSERT INTO story_event_participants (
    story_event_id, story_entity_id, role, state_before, state_after, created_at
)
SELECT story_event_id, story_entity_id, role, state_before, state_after, created_at
FROM _wp06_story_event_participants_stage;

INSERT INTO character_states (
    id, character_entity_id, from_event_order, to_event_order, appearance_json,
    costume_asset_version_id, injuries_json, possessions_json, relationship_state_json,
    source_fact_id, status, created_at, updated_at, revision
)
SELECT id, character_entity_id, from_event_order, to_event_order, appearance_json,
       costume_asset_version_id, injuries_json, possessions_json, relationship_state_json,
       source_fact_id, status, created_at, updated_at, revision
FROM _wp06_character_states_stage;

DROP TABLE _wp06_character_states_stage;

DROP TABLE _wp06_story_event_participants_stage;

DROP TABLE _wp06_story_entity_aliases_stage;

DROP TABLE _wp06_story_entities_stage;

-- The import columns. Both take a constant default, so a plain ADD COLUMN is
-- enough and the existing rows keep a value the CHECK accepts.
ALTER TABLE source_document_versions ADD COLUMN source_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_source_document_versions_source_hash ON source_document_versions(source_hash);

-- 'regex' is the default because every boundary the detector writes comes from a
-- pattern or a heading, and a row written before this column existed was one of
-- those. A user-edited boundary is marked 'manual' by the command that edits it.
ALTER TABLE chapters ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'regex' CHECK (
    source_kind IN ('heading', 'regex', 'whole', 'manual')
);

CREATE INDEX idx_chapters_source_kind ON chapters(source_document_version_id, source_kind);
