-- WP-05: the workflow, review and user-decision tables.
--
-- Field names follow docs/DOMAIN_MODEL.md section 11 and PRD FR-100/FR-110.
-- This is the "基础" the roadmap asks for: the structure, the vocabularies and
-- the constraints. The engine that drives stage transitions, quality gates and
-- automatic revision belongs to WP-07, so nothing here decides when a stage
-- advances.
--
-- Vocabulary rulings (ADR-0007 records them in full):
--
-- The PRD and the domain model disagree about StageRun statuses. PRD FR-100
-- lists pending, running, execution_succeeded, reviewing, passed, needs_fix,
-- needs_redo, waiting_user, failed and cancelled. DOMAIN_MODEL section 11.2
-- lists ready, running, executed, under_review, waiting_user, passed, failed,
-- cancelled and superseded. The PRD is the higher authority in AGENTS section 3
-- and is the behavioural specification, so its spellings are the ones stored.
-- The domain model's superseded is kept as well because its own invariant
-- ("passed 后不可改写，只能 supersede") cannot be expressed without it. The
-- three remaining domain names are the same states under different spellings
-- (ready = pending, executed = execution_succeeded, under_review = reviewing)
-- and are deliberately NOT accepted, because storing one state two ways would
-- make every query guess which spelling it holds.
--
-- UserGateDecision takes PRD section 12.2's set (approve, fix, redo,
-- manual_edit, skip, cancel) and adds waive, which DOMAIN_MODEL section 15.3
-- requires whenever a stale artifact is kept.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP05SplitSQLCompatibility enforces it.

CREATE TABLE workflow_runs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    episode_id TEXT NOT NULL DEFAULT '',
    workflow_type TEXT NOT NULL CHECK (length(workflow_type) BETWEEN 1 AND 120),
    current_stage TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'running', 'waiting_user', 'paused', 'completed', 'failed', 'cancelled')
    ),
    active_stage_run_id TEXT NOT NULL DEFAULT '',
    configuration_json TEXT NOT NULL DEFAULT '',
    retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

CREATE INDEX idx_workflow_runs_project_status_updated ON workflow_runs(project_id, status, updated_at);

CREATE INDEX idx_workflow_runs_episode ON workflow_runs(episode_id);

-- The stage name is free text on purpose. PRD FR-100 and AGENT_CONTRACTS
-- section 10.1 give two different key lists for the default quality gate, and
-- the gate itself is WP-07's to define. Pinning a list here would pick a winner
-- for a decision that is not this package's to make.
CREATE TABLE stage_runs (
    id TEXT PRIMARY KEY,
    workflow_run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    stage TEXT NOT NULL CHECK (length(stage) BETWEEN 1 AND 120),
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt >= 1),
    execution_agent_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'running', 'execution_succeeded', 'reviewing', 'passed', 'needs_fix',
                   'needs_redo', 'waiting_user', 'failed', 'cancelled', 'superseded')
    ),
    input_json TEXT NOT NULL DEFAULT '',
    validated_output_json TEXT NOT NULL DEFAULT '',
    raw_output_file_id TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT NOT NULL DEFAULT '',
    finished_at TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (workflow_run_id, stage, attempt)
);

CREATE INDEX idx_stage_runs_run_stage_attempt ON stage_runs(workflow_run_id, stage, attempt);

-- The invariant "同一 Workflow 同阶段可有多次 attempt，最多一个 active attempt"
-- cannot be a plain unique index because attempts are historical. It is
-- enforced by the WP-07 writer and reported by the section 19 integrity check.
CREATE INDEX idx_stage_runs_active ON stage_runs(workflow_run_id, stage, status);

-- section 11.3. One report per review, from either a deterministic ruleset or a
-- supervisor, and supervisor_key names which.
CREATE TABLE review_reports (
    id TEXT PRIMARY KEY,
    stage_run_id TEXT NOT NULL REFERENCES stage_runs(id) ON DELETE CASCADE,
    supervisor_key TEXT NOT NULL DEFAULT '',
    ruleset_version TEXT NOT NULL DEFAULT '',
    score REAL,
    grade TEXT NOT NULL DEFAULT '',
    passed INTEGER NOT NULL DEFAULT 0 CHECK (passed IN (0, 1)),
    severity TEXT NOT NULL DEFAULT '',
    recommended_action TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_review_reports_stage_run ON review_reports(stage_run_id);

-- section 11.4 plus the PRD FR-110 report shape. field and auto_fixable come
-- from FR-110's JSON example and are what "报告问题可以在 UI 中跳转到实体"
-- needs to address a specific location.
CREATE TABLE review_issues (
    id TEXT PRIMARY KEY,
    review_report_id TEXT NOT NULL REFERENCES review_reports(id) ON DELETE CASCADE,
    rule TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT '',
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    field TEXT NOT NULL DEFAULT '',
    problem TEXT NOT NULL DEFAULT '',
    suggestion TEXT NOT NULL DEFAULT '',
    evidence_json TEXT NOT NULL DEFAULT '',
    auto_fixable INTEGER NOT NULL DEFAULT 0 CHECK (auto_fixable IN (0, 1)),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'accepted', 'fixed', 'waived')),
    resolved_by TEXT NOT NULL DEFAULT '',
    resolved_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_review_issues_report ON review_issues(review_report_id, status);

CREATE INDEX idx_review_issues_entity ON review_issues(entity_type, entity_id);

-- section 11.5. "用户决策不可由 Agent 伪造" is why created_by is stored and why
-- the writer contract (WP-07) must come from a user command.
CREATE TABLE user_gate_decisions (
    id TEXT PRIMARY KEY,
    workflow_run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    stage_run_id TEXT NOT NULL DEFAULT '',
    decision TEXT NOT NULL CHECK (decision IN ('approve', 'fix', 'redo', 'manual_edit', 'skip', 'cancel', 'waive')),
    issue_ids_json TEXT NOT NULL DEFAULT '[]',
    instruction TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    locked_entity_refs_json TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_user_gate_decisions_run ON user_gate_decisions(workflow_run_id, created_at);

-- PRD FR-100 requires "每次状态变化写入审计事件" and an event log. The domain
-- aggregate lists WorkflowEvent without a field table, so the columns are the
-- ones the PRD requirement needs: which run, which stage, what changed, when.
CREATE TABLE workflow_events (
    id TEXT PRIMARY KEY,
    workflow_run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    stage_run_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL CHECK (length(event_type) BETWEEN 1 AND 120),
    from_status TEXT NOT NULL DEFAULT '',
    to_status TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '',
    actor_type TEXT NOT NULL DEFAULT 'system' CHECK (actor_type IN ('user', 'agent', 'migration', 'system')),
    actor_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_workflow_events_run ON workflow_events(workflow_run_id, created_at);

CREATE INDEX idx_workflow_events_stage_run ON workflow_events(stage_run_id);
