-- WP-27 (P3 item 23): FR-080's 多角色声线映射 gets a home, because a voice was travelling per
-- submission and being stored nowhere.
--
-- # What was missing
--
-- `SubmitAudioJob` carries a `voice` field into the job's input, the runner forwards it, and the
-- provider uses it — so a voice IS chosen per line. Nothing reads it back. The consequence is that
-- "character X speaks with voice Y" is not a fact in this application: it is a value a user retypes
-- for every line, and the UI's own note said so ("取自项目的音频设置，本分区不另设一份"), which means a
-- project with two characters had ONE voice for both.
--
-- # Why a table rather than a column or a JSON setting
--
-- The mapping is a SET — one entry per character — so a single `project_settings` row cannot hold it.
-- A JSON column inside that row was the other candidate and was rejected: it would make "what voice
-- does this character use" unqueryable, unconstrainable and unable to follow the character's life.
-- A table gets all three: the UNIQUE index makes a second assignment an UPDATE rather than a second
-- answer, and the foreign key means deleting a character deletes what was said about their voice
-- instead of leaving a row naming an entity that no longer exists.
--
-- # Why the CHARACTER is a story entity
--
-- `dialogue_lines.character_entity_id` is already the identifier a line names its speaker with, and
-- the entity is where a character's identity lives. Casting a voice on any other key would need a
-- join to discover which character a mapping was for.
--
-- # Why provider and model are columns, and why they may be empty
--
-- A voice NAME is not portable: `alloy` on one channel and `alloy` on another are different sounds,
-- so storing the name alone would let a channel change silently change a performance. Both columns
-- exist and are read back with the voice.
--
-- Empty provider/model means "whatever the project's audio configuration names", which is the state
-- of a user who has cast voices but not pinned them to a channel. The alternative — requiring them —
-- would make the mapping unusable until every channel had been chosen, and the resolution order in
-- `internal/application/media/voice.go` already decides what an incomplete mapping falls back to.
--
-- # SplitSQL
--
-- The runner splits this file on every semicolon, so no comment here may contain one.
CREATE TABLE character_voices (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    character_entity_id TEXT NOT NULL REFERENCES story_entities(id) ON DELETE CASCADE,
    provider_config_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    voice TEXT NOT NULL CHECK (length(voice) BETWEEN 1 AND 200),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
);

-- One voice per character per project. This is what makes an assignment idempotent: assigning twice
-- updates the row rather than creating a second answer to the same question.
CREATE UNIQUE INDEX idx_character_voices_project_character
    ON character_voices(project_id, character_entity_id);

-- The listing read is "every casting decision in one project", ordered by the character's name — the
-- join is what supplies the name, so this index covers the filter the query starts from.
CREATE INDEX idx_character_voices_project ON character_voices(project_id);
