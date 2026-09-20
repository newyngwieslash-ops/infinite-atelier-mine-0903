-- WP-07: the agent runtime's records, and the stage-attempt invariant made a
-- constraint.
--
-- Field names follow docs/DOMAIN_MODEL.md section 13 (Agent 与 Skill) and
-- AGENT_CONTRACTS sections 3, 4 and 16.
--
-- Four tables, and one change to a table WP-05 created:
--
-- 1. skill_versions, agent_runs, agent_messages, agent_tool_calls. Section 13.1
--    through 13.4. agent_runs is the record AGENT_CONTRACTS section 16 requires:
--    layer, key, the exact skill version, the model and provider, the workflow and
--    stage, input refs, memory ids, tool calls, and the timing.
--
--    The columns that other migrations left as unconstrained text now have their
--    referent, which those migrations anticipated. Migration 000007 section 6.3
--    says created_by_agent_run_id "stays a plain column because agent_run tables
--    arrive in WP-07, and ADR-0005 section 4 records the same decision", and
--    migration 000003's provider_requests.agent_run_id does the same. This
--    migration deliberately does NOT add those foreign keys: agent_runs rows are
--    pruned before the artifacts that cite them would be, so an FK would either
--    block a legitimate prune or cascade away the citation. The columns stay
--    text and keep meaning "the run that produced this", which is what they were
--    for. ADR-0011 records the ruling.
--
-- 2. A partial unique index on stage_runs, which turns section 11.2's "最多一个
--    active attempt" from a promise into a constraint. Migration 000011 said the
--    invariant "cannot be a plain unique index because attempts are historical",
--    and that is true of a plain index. It is not true of a PARTIAL one: a unique
--    index restricted to the active statuses permits any number of finished
--    attempts and exactly one in flight. A probe against this SQLite (3.53.4)
--    confirmed it refuses a second active attempt and permits a new one once the
--    previous attempt is terminal. The index is added after a deterministic
--    reconciliation, because an existing database could already hold two active
--    attempts for one stage: the WP-05 writer had no check and the invariant lived
--    only in a comment.
--
-- Reconciliation: for each (workflow_run_id, stage) holding more than one active
-- attempt, the highest attempt number is kept and the others are marked failed
-- with a stable error code. Highest rather than lowest because the newest attempt
-- is the one a user was most recently shown. The alternative — refusing to
-- migrate — would leave a database that cannot be opened, which is worse than a
-- row whose status is corrected with a code that says why.
--
-- Constraint of the migration runner: splitSQL splits the file on every semicolon
-- character, comments included, so this file may not contain one outside a
-- statement terminator. TestWP05SplitSQLCompatibility enforces it.

-- 1. The skill version a run is bound to (section 13.4).
--
-- content_hash is the hash of the manifest AND the documents, so a run is
-- reproducible: a skill edited after a run does not change what that run recorded.
-- manifest_json is stored rather than referenced because it is the small, structured
-- part a reviewer reads, while the documents travel as a file reference because they
-- are prompt material.
CREATE TABLE skill_versions (
    id TEXT PRIMARY KEY,
    skill_key TEXT NOT NULL CHECK (length(skill_key) BETWEEN 1 AND 120),
    version TEXT NOT NULL CHECK (length(version) BETWEEN 1 AND 60),
    content_hash TEXT NOT NULL CHECK (length(content_hash) BETWEEN 1 AND 128),
    manifest_json TEXT NOT NULL DEFAULT '',
    content_file_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded')),
    created_at TEXT NOT NULL,
    UNIQUE (skill_key, version)
);

CREATE INDEX idx_skill_versions_key_status ON skill_versions(skill_key, status);

-- section 13.1. One invocation of one agent.
--
-- status vocabulary: pending | running | succeeded | failed | cancelled. It is this
-- package's own because section 13.1 names no values and the stage vocabulary is
-- about a different lifecycle: a stage attempt can be superseded by a later
-- attempt, while a run is simply done or not.
--
-- WorkflowRunID and StageRunID are unconstrained text with empty defaults rather
-- than foreign keys: a Decision run may exist for a project with no workflow yet,
-- which is the case a first user turn creates, and a run outlives the stage it
-- served when a stage is superseded. Deleting a run must not delete the audit
-- trail of what an agent did.
--
-- validated_output_json is the output AFTER schema validation. raw_output_file_id
-- points at the unvalidated text, which SECURITY section 7.3 and ARCHITECTURE
-- section 11 keep for diagnosis only, so it is a FileRef and never a column of
-- model text.
CREATE TABLE agent_runs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_run_id TEXT NOT NULL DEFAULT '',
    stage_run_id TEXT NOT NULL DEFAULT '',
    agent_layer TEXT NOT NULL CHECK (agent_layer IN ('decision', 'execution', 'supervision')),
    agent_key TEXT NOT NULL CHECK (length(agent_key) BETWEEN 1 AND 120),
    model_config_id TEXT NOT NULL DEFAULT '',
    skill_version_id TEXT NOT NULL REFERENCES skill_versions(id),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')
    ),
    input_summary TEXT NOT NULL DEFAULT '',
    validated_output_json TEXT NOT NULL DEFAULT '',
    raw_output_file_id TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL DEFAULT '',
    finished_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_agent_runs_project_status ON agent_runs(project_id, status, created_at);

CREATE INDEX idx_agent_runs_stage ON agent_runs(stage_run_id);

CREATE INDEX idx_agent_runs_layer ON agent_runs(project_id, agent_layer, created_at);

-- section 13.2. One message of one run.
--
-- scope_key is the structured scope of section 14.4 encoded as a stable string.
-- Section 14.4 requires retrieval to filter by its STRUCTURE rather than by a
-- fragile prefix, so agent_messages carries the parts as separate columns too:
-- scope_key is what a query filters on, and the parts are what make the encoding
-- checkable rather than assumed.
--
-- content_hash lets a message be identified and de-duplicated without reading it.
CREATE TABLE agent_messages (
    id TEXT PRIMARY KEY,
    agent_run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    scope_key TEXT NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 400),
    scope_tenant TEXT NOT NULL DEFAULT '',
    scope_workspace TEXT NOT NULL DEFAULT '',
    scope_project TEXT NOT NULL DEFAULT '',
    scope_episode TEXT NOT NULL DEFAULT '',
    scope_agent_key TEXT NOT NULL DEFAULT '',
    scope_session TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('system', 'developer', 'user', 'assistant', 'tool')),
    content TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_agent_messages_run ON agent_messages(agent_run_id, created_at);

CREATE INDEX idx_agent_messages_scope ON agent_messages(scope_project, scope_agent_key, created_at);

-- section 13.3. One tool invocation.
--
-- sequence orders the calls within a run, because the order a model asked for them
-- in is part of what happened. The pair (agent_run_id, sequence) is unique so a
-- replay cannot insert a second call at the same position.
--
-- status 'denied' is separate from 'failed' because SECURITY section 7.1 and
-- AC-AGENT-001 make an authorisation refusal a distinct outcome: it is never
-- retried (AGENT_CONTRACTS section 14.2), and the run must record that rather than
-- a generic failure. A CHECK requires a denied call to carry the code that says so.
CREATE TABLE agent_tool_calls (
    id TEXT PRIMARY KEY,
    agent_run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL DEFAULT 0 CHECK (sequence >= 0),
    tool_key TEXT NOT NULL CHECK (length(tool_key) BETWEEN 1 AND 120),
    input_json TEXT NOT NULL DEFAULT '',
    output_json TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'running', 'succeeded', 'failed', 'denied')
    ),
    error_code TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL DEFAULT '',
    finished_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (agent_run_id, sequence),
    CHECK (status != 'denied' OR length(error_code) > 0)
);

CREATE INDEX idx_agent_tool_calls_run ON agent_tool_calls(agent_run_id, sequence);

CREATE INDEX idx_agent_tool_calls_key ON agent_tool_calls(tool_key, created_at);

-- 2. Reconcile any stage that already holds several active attempts.
--
-- This runs BEFORE the unique index, because the index would otherwise fail to
-- create on a database that has the very state it forbids. The subquery selects,
-- per (run, stage), every active attempt except the one with the highest attempt
-- number, and marks those failed with a code that names why.
--
-- The error code is a literal rather than a formatted string so the statement
-- stays a single UPDATE and the reason is greppable.
--
-- finished_at takes created_at rather than a fresh instant because stage_runs has
-- no updated_at column: the only times it carries are when the row appeared, when
-- it started and when it finished. Using created_at keeps the correction inside
-- the row's own history rather than inventing a moment the migration cannot know.
-- A row that never started therefore records that it finished when it was created,
-- which is what "closed without running" means.
UPDATE stage_runs
SET status = 'failed',
    error_code = 'agent.multiple_active_attempts_reconciled',
    error_message = 'A later attempt for this stage was already active, so this one was closed by migration 000015.',
    finished_at = created_at,
    revision = revision + 1
WHERE status IN ('pending', 'running', 'execution_succeeded', 'reviewing', 'waiting_user', 'needs_fix', 'needs_redo')
  AND EXISTS (
      SELECT 1 FROM stage_runs AS newer
      WHERE newer.workflow_run_id = stage_runs.workflow_run_id
        AND newer.stage = stage_runs.stage
        AND newer.attempt > stage_runs.attempt
        AND newer.status IN ('pending', 'running', 'execution_succeeded', 'reviewing', 'waiting_user', 'needs_fix', 'needs_redo')
  );

-- The invariant, now a constraint. The status list is exactly the active set
-- workflow.StageRun.IsActive reports, so the predicate and the index cannot
-- disagree about which attempts are in play.
CREATE UNIQUE INDEX idx_stage_runs_single_active ON stage_runs(workflow_run_id, stage)
    WHERE status IN ('pending', 'running', 'execution_succeeded', 'reviewing', 'waiting_user', 'needs_fix', 'needs_redo');
