-- WP-05: source documents, chapters and the story fact layer.
--
-- Field names follow docs/DOMAIN_MODEL.md sections 5 and 6, indexes follow
-- section 18. The fact layer is independent of canvas state and chat memory
-- (PRD FR-030): nothing here references canvas_nodes or any chat table.
--
-- The tables exist in WP-05 but the import pipeline that fills them is WP-06.
-- This migration creates the structure and its constraints, not a parser.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP05SplitSQLCompatibility enforces it.

CREATE TABLE source_documents (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    document_type TEXT NOT NULL CHECK (document_type IN ('novel', 'story', 'screenplay', 'outline', 'notes')),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    current_version_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'trashed')),
    deleted_at TEXT NOT NULL DEFAULT '',
    deleted_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_source_documents_project ON source_documents(project_id, status);

-- section 5.2. The original file and the normalized text are both tracked, and
-- a replacement never overwrites the old version. physical_file_id and
-- normalized_text_file_id reference committed objects by hash, which is what
-- makes a chapter unable to point at bytes that were never stored.
CREATE TABLE source_document_versions (
    id TEXT PRIMARY KEY,
    source_document_id TEXT NOT NULL REFERENCES source_documents(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    physical_file_id TEXT NOT NULL DEFAULT '',
    normalized_text_file_id TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    encoding TEXT NOT NULL DEFAULT '',
    char_count INTEGER NOT NULL DEFAULT 0 CHECK (char_count >= 0),
    import_metadata_json TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_at TEXT NOT NULL,
    UNIQUE (source_document_id, version_number)
);

CREATE INDEX idx_source_document_versions_document ON source_document_versions(source_document_id, version_number);

CREATE INDEX idx_source_document_versions_hash ON source_document_versions(content_hash);

-- section 5.3. status records whether a detected boundary was confirmed by a
-- user, which is what PRD FR-020 asks for ("章节拆分可人工修正并保存").
CREATE TABLE chapters (
    id TEXT PRIMARY KEY,
    source_document_version_id TEXT NOT NULL REFERENCES source_document_versions(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    title TEXT NOT NULL DEFAULT '',
    start_offset INTEGER NOT NULL DEFAULT 0 CHECK (start_offset >= 0),
    end_offset INTEGER NOT NULL DEFAULT 0 CHECK (end_offset >= 0),
    content_hash TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'detected' CHECK (status IN ('detected', 'confirmed', 'edited')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (source_document_version_id, ordinal),
    CHECK (end_offset >= start_offset)
);

CREATE INDEX idx_chapters_version_ordinal ON chapters(source_document_version_id, ordinal);

-- section 6.1. "candidate" is what an extraction produces and "accepted" is
-- what a user or rule promoted it to (PRD FR-030 requires that gate).
CREATE TABLE story_entities (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('character', 'location', 'organization', 'prop', 'concept', 'time')),
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

-- section 6.2. A conflict between names is reported rather than silently
-- merged, so no unique constraint is placed on canonical_name or alias.
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

-- section 6.3. created_by_agent_run_id stays a plain column because agent_run
-- tables arrive in WP-07, and ADR-0005 section 4 records the same decision for
-- generation_jobs.project_id: a forward migration adds the foreign key when the
-- referenced table exists.
CREATE TABLE story_events (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    chapter_id TEXT NOT NULL DEFAULT '',
    ordinal INTEGER NOT NULL DEFAULT 0 CHECK (ordinal >= 0),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL DEFAULT '',
    story_time_text TEXT NOT NULL DEFAULT '',
    story_time_order INTEGER,
    location_entity_id TEXT NOT NULL DEFAULT '',
    cause_summary TEXT NOT NULL DEFAULT '',
    result_summary TEXT NOT NULL DEFAULT '',
    importance TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'candidate' CHECK (status IN ('candidate', 'accepted', 'rejected', 'locked')),
    source_scope TEXT NOT NULL DEFAULT 'original' CHECK (source_scope IN ('original', 'adaptation', 'user')),
    created_by_agent_run_id TEXT NOT NULL DEFAULT '',
    deleted_at TEXT NOT NULL DEFAULT '',
    deleted_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_story_events_project_chapter_ordinal ON story_events(project_id, chapter_id, ordinal);

CREATE INDEX idx_story_events_status ON story_events(project_id, status);

-- section 6.4. The participant link carries the state before and after, which
-- is what CharacterState generalizes.
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

-- section 6.5. PRD FR-030 lists a wider relation set than the domain example
-- shows (participates_in, occurs_at, reveals, conflicts_with, transfers_to,
-- changes_state, knows, related_to and more), so the column carries the union
-- and the vocabulary is pinned in internal/domain/story. ADR-0007 records it.
CREATE TABLE story_relations (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL CHECK (
        relation_type IN ('participates_in', 'occurs_at', 'causes', 'precedes', 'reveals',
                          'conflicts_with', 'owns', 'transfers_to', 'changes_state', 'knows',
                          'related_to', 'located_in', 'contradicts', 'other')
    ),
    source_entity_type TEXT NOT NULL DEFAULT '',
    source_entity_id TEXT NOT NULL,
    target_entity_type TEXT NOT NULL DEFAULT '',
    target_entity_id TEXT NOT NULL,
    valid_from_event_id TEXT NOT NULL DEFAULT '',
    valid_to_event_id TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'candidate' CHECK (status IN ('candidate', 'accepted', 'rejected', 'locked')),
    source_scope TEXT NOT NULL DEFAULT 'original' CHECK (source_scope IN ('original', 'adaptation', 'user')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_story_relations_lookup ON story_relations(project_id, relation_type, source_entity_id, target_entity_id);

-- section 6.6. A fact's evidence is an offset range into a specific version of
-- a document, never a copy of the chapter.
CREATE TABLE story_fact_sources (
    id TEXT PRIMARY KEY,
    fact_type TEXT NOT NULL CHECK (fact_type IN ('entity', 'event', 'relation', 'character_state')),
    fact_id TEXT NOT NULL,
    chapter_id TEXT NOT NULL DEFAULT '',
    source_document_version_id TEXT NOT NULL DEFAULT '',
    start_offset INTEGER,
    end_offset INTEGER,
    quote_hash TEXT NOT NULL DEFAULT '',
    source_kind TEXT NOT NULL CHECK (source_kind IN ('text', 'user', 'agent_inference', 'adaptation')),
    created_at TEXT NOT NULL
);

CREATE INDEX idx_story_fact_sources_fact ON story_fact_sources(fact_type, fact_id);

CREATE INDEX idx_story_fact_sources_chapter ON story_fact_sources(chapter_id);

-- section 6.7. A conflict is a first-class row so it can be resolved, waived and
-- reported rather than living in a log line.
CREATE TABLE story_fact_conflicts (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    left_fact_type TEXT NOT NULL,
    left_fact_id TEXT NOT NULL,
    right_fact_type TEXT NOT NULL,
    right_fact_id TEXT NOT NULL,
    conflict_type TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'waived')),
    resolution TEXT NOT NULL DEFAULT '',
    resolved_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    resolved_at TEXT NOT NULL DEFAULT '',
    UNIQUE (left_fact_type, left_fact_id, right_fact_type, right_fact_id)
);

CREATE INDEX idx_story_fact_conflicts_project ON story_fact_conflicts(project_id, status);

-- section 6.8. "MVP 可在 Schema 受控 JSON 中保存稀疏状态" is the licence for the
-- json columns. from_event_order and to_event_order keep the state queryable in
-- the event order it applies to, which is what continuity checks will read.
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
