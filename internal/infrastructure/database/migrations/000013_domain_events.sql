-- WP-05: the domain event stream, DOMAIN_MODEL section 17.
--
-- Section 16 requires every command to implement "event", and section 17 fixes
-- both the event names and the envelope they carry. This table is that stream.
--
-- Why one table rather than columns on each aggregate: an event is addressed to
-- consumers, not to its subject. A reader asking "what happened in this project"
-- or "what has been approved" scans one table in time order. Spreading the same
-- facts across a dozen aggregate tables would make that a union of a dozen
-- queries and would lose the ordering between them.
--
-- The event is a record that something happened, never the authority on what
-- happened: the row it describes is. So nothing here is a foreign key to the
-- subject. An event outlives the thing it describes — a deleted episode still
-- has an EpisodeCreated row — and a foreign key would either cascade a history
-- away or refuse the delete.
--
-- Constraint of the migration runner: splitSQL splits the file on every
-- semicolon character, comments included, so this file may not contain one
-- outside a statement terminator. TestWP05SplitSQLCompatibility enforces it.

CREATE TABLE domain_events (
    -- The event's own identifier. ADR-0005 makes it a UUIDv7 minted by the
    -- application layer, which is what gives the stream a stable order and lets
    -- a consumer de-duplicate a replayed range.
    id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL CHECK (
        event_type IN ('ProjectCreated', 'ProjectSettingsChanged', 'ProjectRuleLocked',
                       'SourceDocumentImported', 'ChapterBoundariesConfirmed',
                       'StoryFactAccepted', 'StoryFactConflictOpened',
                       'EpisodeCreated', 'StorySkeletonApproved', 'AdaptationStrategyApproved',
                       'ScriptVersionCreated', 'ScriptVersionApproved',
                       'AssetVersionCreated', 'AssetVersionApproved',
                       'DirectorPlanApproved', 'StoryboardVersionApproved',
                       'CanvasProjectionCreated',
                       'WorkflowStarted', 'WorkflowStageChanged', 'ReviewReportCreated', 'UserGateDecided',
                       'GenerationJobQueued', 'GenerationJobSucceeded', 'GenerationJobFailed',
                       'MemoryCreated',
                       'UpstreamVersionChanged', 'ArtifactMarkedStale',
                       'BackupCompleted')
    ),
    -- The envelope version, so a consumer can tell an old-shape row from a new
    -- one rather than parsing it wrong.
    schema_version INTEGER NOT NULL CHECK (schema_version >= 1),
    aggregate_type TEXT NOT NULL CHECK (length(aggregate_type) BETWEEN 1 AND 120),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 200),
    -- Every drama query is project-scoped, so the stream is too. This is the
    -- one reference the table keeps, because a project is the boundary of
    -- everything the drama domain holds and an event with no project could not
    -- be listed.
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    occurred_at TEXT NOT NULL,
    trace_id TEXT NOT NULL DEFAULT '' CHECK (length(trace_id) <= 64),
    -- A summary of the change, bounded by the application layer. It may be
    -- empty: an event whose meaning is entirely in its type needs no payload.
    payload_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

-- The two queries the stream serves: everything that happened to one subject,
-- and everything that happened in one project, both newest first.
CREATE INDEX idx_domain_events_aggregate ON domain_events(aggregate_type, aggregate_id, occurred_at);

CREATE INDEX idx_domain_events_project ON domain_events(project_id, occurred_at);

-- A consumer that wants one kind of event ("show me every approval") reads by
-- type, and a correlation query groups by trace.
CREATE INDEX idx_domain_events_type ON domain_events(event_type, occurred_at);

CREATE INDEX idx_domain_events_trace ON domain_events(trace_id);
