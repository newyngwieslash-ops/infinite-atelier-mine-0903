-- WP-10: persistent memory, and the source mark the deterministic checks need.
--
-- Field names follow docs/DOMAIN_MODEL.md section 14 (Memory) and
-- PRD FR-120, and the retrieval shape follows docs/ARCHITECTURE.md section 12.
--
-- Three tables and one added column.
--
-- 1. memory_items is the memory store. It is NOT agent_messages, and that
--    separation is the ruling ADR-0014 records.
--
--    agent_messages is the agent RUNTIME's transcript: one row per turn of one
--    run, cascade-deleted with that run (migration 000015). A memory is the
--    USER's record: it outlives the run that produced it, it can be pinned,
--    edited, deleted and re-embedded, and section 14.5 makes "locked memory is
--    the user's to change" an invariant. Putting `locked` and `embedding_blob`
--    on the transcript would make "delete this memory" a write to the runtime's
--    record, which is why the columns are here instead. The episodic row cites
--    the message it came from through source_type and source_id, which is what
--    section 14.1's `source_type` / `source_id` are for.
--
--    scope_key is section 14.4's six parts encoded as the same stable string
--    agent_messages already uses, and the six structured columns are here for
--    the reason migration 000015 states: section 14.4 requires retrieval to
--    filter by STRUCTURE rather than by a fragile string prefix, so both the
--    encoded key and its parts are stored.
--
-- 2. memory_summary_sources is section 14.2, literally, and its name says which
--    identifiers it joins: a summary is linked to the MESSAGES it summarises.
--    AC-MEM-004's "Summary 关联源消息表" asks for exactly that relation, and the
--    role, agent and time a reader needs are the message row's own fields, so a
--    UI can jump from a summary to the message it came from in one hop.
--
--    The hierarchical summaries of PRD FR-120 are linked summary-to-summary
--    through memory_entity_links with relation_type 'summarizes' (section 14.3),
--    rather than through a second self-referencing table on this one.
--
-- 3. memory_entity_links is section 14.3.
--
-- 4. review_issues gains `source`. AGENT_CONTRACTS section 11.4 requires it:
--    "硬规则应尽量用确定性代码先检查，LLM Supervisor 负责语义质量。ReviewReport
--    合并两类证据，并标记 source=deterministic|llm". The column defaults to
--    'llm' so every row written before this migration keeps its meaning — those
--    findings were all the supervisor's — and a deterministic check that reports
--    through the same report marks its own findings 'deterministic'.
--
-- Constraint of the migration runner: splitSQL splits the file on every semicolon character,
-- comments included, so this file may not contain one outside a statement terminator.
-- TestWP05SplitSQLCompatibility enforces it.

CREATE TABLE memory_items (
    id TEXT PRIMARY KEY,
    scope_key TEXT NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 400),
    scope_tenant TEXT NOT NULL DEFAULT '',
    scope_workspace TEXT NOT NULL DEFAULT '',
    scope_project TEXT NOT NULL DEFAULT '',
    scope_episode TEXT NOT NULL DEFAULT '',
    scope_agent_key TEXT NOT NULL DEFAULT '',
    scope_session TEXT NOT NULL DEFAULT '',
    memory_type TEXT NOT NULL CHECK (
        memory_type IN ('episodic', 'semantic', 'procedural', 'artifact', 'summary')
    ),
    role TEXT NOT NULL DEFAULT '' CHECK (
        role IN ('', 'system', 'developer', 'user', 'assistant', 'tool')
    ),
    agent_key TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    importance REAL NOT NULL DEFAULT 0.5 CHECK (importance >= 0 AND importance <= 1),
    confidence REAL NOT NULL DEFAULT 0.5 CHECK (confidence >= 0 AND confidence <= 1),
    embedding_blob BLOB,
    embedding_model TEXT NOT NULL DEFAULT '',
    embedding_version TEXT NOT NULL DEFAULT '',
    embedded_at TEXT NOT NULL DEFAULT '',
    summarized INTEGER NOT NULL DEFAULT 0 CHECK (summarized IN (0, 1)),
    locked INTEGER NOT NULL DEFAULT 0 CHECK (locked IN (0, 1)),
    source_type TEXT NOT NULL DEFAULT '' CHECK (
        source_type IN ('', 'message', 'summary', 'artifact', 'user', 'agent_run')
    ),
    source_id TEXT NOT NULL DEFAULT '',
    deleted_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

-- The index section 18 asks for, on the STRUCTURED columns rather than on the
-- encoded key. The same reasoning migration 000015 gives for agent_messages
-- applies: a query filters by project and agent and orders by time, and a prefix
-- scan on a joined string would answer a different question from the one asked.
CREATE INDEX idx_memory_items_scope ON memory_items(scope_project, scope_agent_key, memory_type, created_at);

-- The second read the channels make is project-wide within one type: the recent
-- window needs every episodic row of a project, and the semantic channel needs
-- every semantic row of it, neither of which is narrowed by an agent key.
CREATE INDEX idx_memory_items_project_type ON memory_items(scope_project, memory_type, created_at);

-- The vector search reads candidates for one project inside one embedding
-- version, which is what section 14.5's "重建后切换索引版本" needs to be able to
-- state: a row left on an older model is not a candidate for the new one.
CREATE INDEX idx_memory_items_embedding ON memory_items(scope_project, embedding_model, embedding_version);

-- The summary a reader reverses is a memory item of type 'summary', so its own
-- lookup is the primary key. What these two foreign keys add is the guarantee
-- section 14.2 states as a primary key and section 19 checks as "Summary sources
-- 有效": a source row cannot outlive either end of the relation it asserts.
CREATE TABLE memory_summary_sources (
    summary_id TEXT NOT NULL REFERENCES memory_items(id) ON DELETE CASCADE,
    source_memory_id TEXT NOT NULL REFERENCES memory_items(id) ON DELETE CASCADE,
    source_order INTEGER NOT NULL CHECK (source_order >= 1),
    created_at TEXT NOT NULL,
    PRIMARY KEY (summary_id, source_memory_id)
);

-- section 18's index on the relation, which is how "which memories are about
-- this entity" is answered. It is the direction the recall and the UI both read.
CREATE INDEX idx_memory_summary_sources_source ON memory_summary_sources(source_memory_id);

CREATE TABLE memory_entity_links (
    memory_id TEXT NOT NULL REFERENCES memory_items(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL CHECK (length(entity_type) BETWEEN 1 AND 60),
    entity_id TEXT NOT NULL CHECK (length(entity_id) BETWEEN 1 AND 200),
    relation_type TEXT NOT NULL CHECK (length(relation_type) BETWEEN 1 AND 60),
    created_at TEXT NOT NULL,
    PRIMARY KEY (memory_id, entity_type, entity_id, relation_type)
);

-- section 18's index, verbatim in shape: a link is looked up from the ENTITY
-- side when a reader asks what a character or a version is remembered by.
CREATE INDEX idx_memory_entity_links_entity ON memory_entity_links(entity_type, entity_id);

-- ALTER rather than a rebuild: review_issues carries rows a user may already have
-- read, and a table rebuild for one defaulted column would be a rewrite of every
-- finding for no gain. The default is 'llm' because every finding stored before
-- this migration came from a supervisor, so an old row keeps exactly the meaning
-- it had.
ALTER TABLE review_issues ADD COLUMN source TEXT NOT NULL DEFAULT 'llm'
    CHECK (source IN ('deterministic', 'llm'));
