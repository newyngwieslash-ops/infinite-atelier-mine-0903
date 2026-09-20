-- WP-08: the field locks a user pins, so a FIX cannot rewrite what the model got right.
--
-- AC-SCRIPT-002's scenario is a story skeleton whose ending hook is missing: the user asks for a
-- FIX on that finding, and the fields they were happy with must come back unchanged. The
-- mechanism therefore has to attach to a FIELD of a VERSION rather than to a line — migration
-- 000008's dialogue_lines.locked covers one part of the requirement (§7.7's per-line flag) and
-- nothing covered the rest.
--
-- ONE TABLE FOR THREE FAMILIES rather than a column per artifact. A lock is the same fact about
-- a skeleton, a strategy and a script version: "this field of this version is not to be
-- rewritten". Three columns would be three places for the rule to drift, and the family column
-- is what lets the write path validate the field name against the right vocabulary.
--
-- version_id is deliberately NOT a foreign key. The three version families live in three tables,
-- so a single column cannot reference all of them, and the alternative — three lock tables — is
-- what this design rejects. What keeps the column honest is the write path: it records a lock
-- only after reading the version row and confirming its family, and a lock whose version was
-- deleted is harmless rather than load-bearing. ADR-0012 records the ruling and its cost
-- (a delete leaves orphan locks, which the same migration's index makes cheap to sweep).
--
-- Constraint of the migration runner: splitSQL splits the file on every semicolon character,
-- comments included, so this file may not contain one outside a statement terminator.
-- TestWP05SplitSQLCompatibility enforces it.

CREATE TABLE script_version_field_locks (
    version_id TEXT NOT NULL,
    family TEXT NOT NULL CHECK (family IN ('story_skeleton', 'adaptation_strategy', 'script')),
    field TEXT NOT NULL CHECK (length(field) BETWEEN 1 AND 60),
    locked_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (version_id, field)
);

-- The read a lock check makes is "which fields of THIS version are locked", which the primary
-- key already serves. This second index serves the sweep a deleted version's orphans need, and
-- the family is its leading column because that is how the sweep finds them.
CREATE INDEX idx_script_version_field_locks_family ON script_version_field_locks(family, version_id);
