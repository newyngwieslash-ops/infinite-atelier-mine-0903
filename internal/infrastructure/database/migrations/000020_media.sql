-- WP-11: subtitles and the episode export record.
--
-- Field names follow docs/DOMAIN_MODEL.md sections 7.7 (dialogue), 7.8 (shots) and 9.5
-- (the panel version's shape, which these two mirror) -- and the export record follows PRD
-- FR-080's 成片导出 requirements and AGENT_CONTRACTS section 11.4's Final Ruleset.
--
-- Three tables, and the reason each exists is a criterion rather than a preference.
--
-- 1. subtitle_tracks is a VERSIONED artifact, and that shape is deliberate.
--
--    AC-MEDIA-002 requires that a subtitle be EDITABLE, and an editable artifact in this
--    repository is a version: the same rule the script, the storyboard and the director
--    plan follow. An edit that overwrote its predecessor would leave a user unable to see
--    what the subtitles said when an export was made, and PRD FR-080's "输出包含导出清单和
--    版本信息" needs exactly that answer. The eight-status vocabulary and the
--    approved-per-episode partial index are the same ones migration 000008 established, so
--    a reader who knows a script version knows this row.
--
-- 2. subtitle_cues is one line of a track, and every column earns its place.
--
--    dialogue_line_id is why the table exists rather than a text blob: AC-MEDIA-002 asks
--    for "missing line detected", which is a join — which dialogue lines have no cue — and a
--    blob cannot be joined. character_entity_id is the speaker, carried on the cue because a
--    subtitle file may keep a cue whose line was deleted (the user's own edit), and a row
--    that could not say who was speaking would render differently from the one beside it.
--
--    The time range is MILLISECONDS as integers, not the HH:MM:SS,mmm a subtitle file
--    writes. Storage is exact and the file format is a rendering: an SRT's comma and a
--    VTT's dot are the same instant written two ways, and a column holding one of them would
--    make the other a conversion. The domain's Timecode type owns the round trip.
--
--    end_ms > start_ms is a CHECK rather than a convention: a cue with no duration is
--    invisible on screen, and a subtitle nobody can read is not a subtitle.
--
-- 3. episode_exports is the manifest AC-MEDIA-003's "manifest traceability" asks for.
--
--    manifest_json holds the versions the export was made FROM — the script, the plan, the
--    board, each panel version, each media version, with hashes — because a frame that
--    cannot be traced to the script that produced it is a frame nobody can defend a week
--    later. output_file_hash points at the produced MP4 through file_objects, which is the
--    same content-addressed row every other artifact uses.
--
--    There is no `export` value in artifact_staleness.artifact_type and this migration does
--    NOT add one: that CHECK is closed and lives in a published migration, and ADR-0015
--    section 7 records the ruling — staleness for an export is a deterministic finding that
--    compares manifest_json against what is currently approved, not a second artifact node.
--
--    approval_trace_id is the audit LINK to the user gate decision that put an export in
--    force, following the asset gap report's precedent in migration 000018: DOMAIN_MODEL
--    section 17's event list is closed and has no export event, so the governance travels on
--    the workflow's own stream and this column is what connects the two.
--
-- Constraint of the migration runner: splitSQL splits the file on every semicolon character,
-- comments included, so this file may not contain one outside a statement terminator.
-- TestWP05SplitSQLCompatibility enforces it.

CREATE TABLE subtitle_tracks (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    script_version_id TEXT NOT NULL REFERENCES script_versions(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    based_on_version_id TEXT NOT NULL DEFAULT '',
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (episode_id, version_number)
);

CREATE INDEX idx_subtitle_tracks_episode ON subtitle_tracks(episode_id, version_number);

-- One approved track per episode, which is the same partial-index shape the four version
-- families and the asset gap report use: "the subtitles in force" is a question with one
-- answer, and the index is what makes the database say so rather than a reader having to pick.
CREATE UNIQUE INDEX idx_subtitle_tracks_approved ON subtitle_tracks(episode_id) WHERE status = 'approved';

CREATE TABLE subtitle_cues (
    id TEXT PRIMARY KEY,
    track_id TEXT NOT NULL REFERENCES subtitle_tracks(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    start_ms INTEGER NOT NULL CHECK (start_ms >= 0),
    end_ms INTEGER NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    character_entity_id TEXT NOT NULL DEFAULT '',
    -- The line this cue renders. It is a plain column rather than a foreign key for the reason
    -- storyboard_items.shot_id is: a dialogue line belongs to a SCRIPT VERSION and this table
    -- cites a line by identifier, so a deleted version would cascade away cues whose TEXT the
    -- user may have edited by hand. The write path checks the line exists, and a cue whose
    -- line is gone is a cue the user wrote or kept, not one the generator invented.
    dialogue_line_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (track_id, ordinal),
    CHECK (end_ms > start_ms)
);

CREATE INDEX idx_subtitle_cues_track ON subtitle_cues(track_id, ordinal);

-- The join AC-MEDIA-002's "missing line detected" makes: which dialogue lines have no cue.
-- The index is on the line identifier because that is the direction the check reads.
CREATE INDEX idx_subtitle_cues_line ON subtitle_cues(dialogue_line_id);

CREATE TABLE episode_exports (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (
        status IN ('draft', 'candidate', 'under_review', 'approved', 'rejected', 'superseded', 'deprecated', 'stale')
    ),
    -- Which recipe produced it. preview and final are FR-080's two qualities, and the value is
    -- stored rather than derived because the SAME episode can have both and the difference
    -- decides how much a re-export costs.
    quality TEXT NOT NULL DEFAULT 'preview' CHECK (quality IN ('preview', 'final')),
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    -- What the OUTPUT says its duration is, read back after composing rather than taken from
    -- the request: a concatenation that dropped a segment shows up here.
    duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    output_file_hash TEXT NOT NULL DEFAULT '',
    subtitle_track_id TEXT NOT NULL DEFAULT '',
    -- The versions the export was made from, as a JSON document. The manifest's own shape is
    -- the domain's (internal/domain/media), and this column is where it travels.
    manifest_json TEXT NOT NULL DEFAULT '',
    approval_trace_id TEXT NOT NULL DEFAULT '',
    source_agent_run_id TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent', 'migration', 'system')),
    created_by_id TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (episode_id, version_number)
);

CREATE INDEX idx_episode_exports_episode ON episode_exports(episode_id, version_number);

CREATE UNIQUE INDEX idx_episode_exports_approved ON episode_exports(episode_id) WHERE status = 'approved';
