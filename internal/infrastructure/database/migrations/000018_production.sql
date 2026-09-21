-- WP-09: the three FR-070 descriptions a storyboard item owes, and the asset gap report.
--
-- TWO CHANGES, and they are here together because both answer the same question — "what does
-- the storyboard stage need that WP-05 did not build" — and splitting them across two files
-- would have made a package that is one unit look like two.
--
-- # The three columns
--
-- FR-070's storyboard table lists sixteen fields per shot, and "每个镜头至少包含" is a MUST.
-- WP-05's migration 000010 built the columns for thirteen of them and its comment pushed the
-- remaining three onto the Shot: "首帧描述、尾帧描述、视频运动描述 belong to the shot the
-- storyboard item cites". That reading is wrong, and the reason is what the two things ARE.
-- A Shot is a line of the SCRIPT — what happens, in what order, said by whom — and it is
-- written before anybody has decided what the camera does. A storyboard item is the SHOOTING
-- decision: the framing, the movement, and what the model must render. "The first frame shows
-- X" is not a fact about the script's shot, it is a decision the storyboard stage makes, and
-- nothing before this migration could store it anywhere.
--
-- They are added as NOT NULL DEFAULT '' rather than as nullable columns, which is how every
-- other optional text column in this schema is stated: an empty description is a storyboard
-- item whose author left it blank, and a NULL would be a second way to say the same thing.
--
-- # The gap report
--
-- DOMAIN_MODEL section 15.2 puts "Asset Gap Report" in the derivation chain between the script
-- and the storyboard, and nothing built it: the report is what says which of the script's
-- characters, locations, props and costumes have an asset and which do not, and it is what
-- AC-BOARD-001's "必需资产缺失时阻止批量" reads. It is a VERSIONED artifact rather than a
-- computed view because the analysis is an agent's (§10.1 gives asset_analysis a required user
-- gate) and a user approves it — so it has the same version, status and approval shape the
-- other four version families have.
--
-- # What is deliberately NOT here
--
-- No panel-candidate table. Section 9.5 says the approved image of a panel is an asset version
-- and that it must come from the panel's candidates or from an explicit link, and migration
-- 000009's asset_usages already carries `consumer_type = 'storyboard_panel'` with a
-- polymorphic consumer_id. A candidate IS a usage of an asset version by a panel — plus the
-- job that produced it, which asset_versions.generation_job_id already records. A second table
-- would be a second answer to "which images belong to this panel", which is the shape of defect
-- four of this repository's reviews have already found.
--
-- Constraint of the migration runner: splitSQL splits the file on every semicolon character,
-- comments included, so this file may not contain one outside a statement terminator.
-- TestWP05SplitSQLCompatibility enforces it.

ALTER TABLE storyboard_items ADD COLUMN first_frame_description TEXT NOT NULL DEFAULT '';

ALTER TABLE storyboard_items ADD COLUMN last_frame_description TEXT NOT NULL DEFAULT '';

ALTER TABLE storyboard_items ADD COLUMN video_motion_description TEXT NOT NULL DEFAULT '';

-- section 9.4's "StoryboardItem 必须唯一对应本版本的 Shot" is why the item table carries a
-- shot_id and no version: the story this report is about is the SCRIPT version it analysed.
-- The two foreign keys are real ones — unlike the lock table's — because a gap report without
-- its episode or its script version cannot be resolved to anything a user could act on.
--
-- There is no event column and no gap-report EVENT at all, and that is deliberate rather than
-- an omission. DOMAIN_MODEL section 17's event list is closed — the vocabulary comment in
-- domain/event says so — and it carries AssetVersionCreated, AssetVersionApproved,
-- DirectorPlanApproved and StoryboardVersionApproved but nothing for this artifact. Rather
-- than invent a name the specification does not define, the approval is governed exactly
-- where the other user gates are: through `UserGateDecided` on the workflow's own stream,
-- which records who decided and when. What this table adds is the audit LINK — the trace
-- identifier the decision carried — so a reader of the report can find the decision that put
-- it in force instead of having to match timestamps. ADR-0013 records the ruling.
CREATE TABLE asset_gap_reports (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    script_version_id TEXT NOT NULL REFERENCES script_versions(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    approval_trace_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (episode_id, version_number)
);

CREATE INDEX idx_asset_gap_reports_episode ON asset_gap_reports(episode_id, version_number);

-- One approved report per episode, which is the same partial-index shape the four version
-- families use: "the gap analysis in force" is a question with one answer, and the index is
-- what makes the database say so rather than a reader having to pick.
CREATE UNIQUE INDEX idx_asset_gap_reports_approved ON asset_gap_reports(episode_id) WHERE status = 'approved';

-- section 8.6's usages are what a gap item is about, and several report the same absence, so
-- asset_id is empty for an item nothing satisfies rather than pointing at a placeholder row.
-- story_entity_id names the STORY fact the asset is needed for, which is the only identifier a
-- missing asset can have: an asset that does not exist has no id to cite.
CREATE TABLE asset_gap_items (
    id TEXT PRIMARY KEY,
    report_id TEXT NOT NULL REFERENCES asset_gap_reports(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    asset_type TEXT NOT NULL CHECK (
        asset_type IN ('character', 'location', 'prop', 'costume', 'vehicle', 'creature',
                       'style', 'style_reference', 'derived_asset', 'image', 'video', 'audio', 'doc')
    ),
    story_entity_id TEXT NOT NULL DEFAULT '',
    story_entity_name TEXT NOT NULL DEFAULT '',
    asset_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'missing' CHECK (status IN ('missing', 'satisfied')),
    usage_role TEXT NOT NULL DEFAULT 'reference',
    -- required is what AC-BOARD-001's block reads: a missing REQUIRED asset stops a batch, and
    -- a missing optional one is a note. It is a column rather than a rule about asset_type
    -- because the same character can be essential in one episode and background in another.
    required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (report_id, ordinal)
);

CREATE INDEX idx_asset_gap_items_report ON asset_gap_items(report_id, ordinal);

CREATE INDEX idx_asset_gap_items_entity ON asset_gap_items(story_entity_id);
