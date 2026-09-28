-- 000027_audio_asset_isolation.sql — T01's data repair (2026-09-26 audit).
--
-- # The shape being repaired
--
-- The audio view used to key every dialogue job's result to ONE project-wide
-- asset found by fixed name ("Line speech"), and every effect job's to another
-- ("Sound effects"). Two spoken lines therefore held versions of the SAME
-- asset, and the second collection superseded the first — whose usage rows
-- then failed the mix's `au.asset_version_id = aa.current_approved_version_id`
-- filter. The sounds a user generated were silently replaced by the newest
-- line's take.
--
-- New collections are isolated (one asset per line instance, keyed on the
-- job's entity id), but databases written by the old collector can still hold
-- a shared asset: several usage rows on one asset's versions, each meaning a
-- different line. This migration SPLITS those, one child asset per distinct
-- consuming shot, so each line's usage names a version of its own asset and
-- the mix reads every line again.
--
-- # The rules this follows
--
--   - The FIRST usage keeps the original asset and its version row. Each
--     further usage's version is COPIED onto a child asset (new version id,
--     same number, provenance, job reference and files), and that usage is
--     repointed at the copy.
--   - The copy carries 'approved' when the original was the asset's approved
--     one, so the child's pointer is repairable and the mix finds the sound.
--   - Assets whose versions carry ONE consumer are untouched.
--   - Nothing is deleted. A wrong split is recoverable, but a deleted version is
--     not.

-- 1. The ambiguous audio assets: any audio asset whose versions carry more
--    than one distinct consumer.
CREATE TEMP TABLE _split_assets AS
SELECT p.id AS asset_id, p.project_id, p.name
FROM assets p
WHERE p.asset_type = 'audio'
  AND (
      SELECT COUNT(DISTINCT au.consumer_type || '|' || au.consumer_id)
      FROM asset_usages au
      JOIN asset_versions v ON v.id = au.asset_version_id
      WHERE v.asset_id = p.id
  ) > 1;

-- 2. The split moves, one per (asset, consumer) beyond the first: ordered so
--    the earliest usage keeps the parent, deterministic on ids.
CREATE TEMP TABLE _split_moves AS
SELECT au.consumer_type,
       au.consumer_id,
       au.usage_role,
       au.asset_version_id AS old_version_id,
       s.project_id,
       s.name
FROM _split_assets s
JOIN asset_versions v ON v.asset_id = s.asset_id
JOIN asset_usages au ON au.asset_version_id = v.id
WHERE (
    SELECT COUNT(DISTINCT au3.consumer_type || '|' || au3.consumer_id)
    FROM asset_usages au3
    JOIN asset_versions v3 ON v3.id = au3.asset_version_id
    WHERE v3.asset_id = s.asset_id
      AND (au3.consumer_type || '|' || au3.consumer_id) < (au.consumer_type || '|' || au.consumer_id)
) >= 1;

-- 3. One child asset per distinct (parent version, consumer) in the move set.
CREATE TEMP TABLE _split_children AS
SELECT 'aud-' || substr(LOWER(HEX(RANDOMBLOB(8))), 1, 8) AS child_id,
       m.project_id,
       substr(m.name || ' · ' || m.consumer_id, 1, 200) AS child_name,
       m.old_version_id,
       m.consumer_type,
       m.consumer_id
FROM _split_moves m
GROUP BY m.old_version_id, m.consumer_type, m.consumer_id;

INSERT INTO assets (id, project_id, asset_type, name, description, story_entity_id,
                    current_approved_version_id, status, created_at, updated_at, revision)
SELECT c.child_id, c.project_id, 'audio', c.child_name, '', '', '', 'active',
       '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1
FROM _split_children c;

-- 4. The version copy: new id, the CHILD as its asset, the same number and
--    provenance. Approved when the original was the parent's approved one —
--    the sound that was in force belongs to the line it was generated for.
CREATE TEMP TABLE _split_copies AS
SELECT c.child_id,
       c.old_version_id,
       'audv-' || substr(LOWER(HEX(RANDOMBLOB(8))), 1, 12) AS new_version_id,
       CASE WHEN a.current_approved_version_id = c.old_version_id
            THEN 'approved' ELSE v.status END AS copy_status
FROM _split_children c
JOIN asset_versions v ON v.id = c.old_version_id
JOIN assets a ON a.id = (SELECT asset_id FROM asset_versions WHERE id = c.old_version_id)
GROUP BY c.child_id, c.old_version_id;

INSERT INTO asset_versions (id, asset_id, version_number, status, based_on_version_id,
                            parent_asset_version_id, variant_type, prompt, negative_prompt,
                            provider_config_id, model_config_id, model_parameters_json, seed,
                            generation_job_id, source_agent_run_id, metadata_json,
                            created_by_type, created_by_id, legacy_metadata_json, created_at)
SELECT k.new_version_id,
       k.child_id,
       v.version_number,
       k.copy_status,
       v.based_on_version_id, v.parent_asset_version_id, v.variant_type, v.prompt,
       v.negative_prompt, v.provider_config_id, v.model_config_id, v.model_parameters_json,
       v.seed, v.generation_job_id, v.source_agent_run_id, v.metadata_json,
       v.created_by_type, v.created_by_id, v.legacy_metadata_json, v.created_at
FROM _split_copies k
JOIN asset_versions v ON v.id = k.old_version_id;

-- 5. The copies' files travel with them.
INSERT INTO asset_files (asset_version_id, file_hash, role, created_at)
SELECT k.new_version_id, f.file_hash, f.role, f.created_at
FROM _split_copies k
JOIN asset_files f ON f.asset_version_id = k.old_version_id;

-- 6. The usage rows are UPDATED in place rather than duplicated: their ids
--    and created_at are the history of "this shot uses this sound". Each moved
--    usage names its consumer's copy.
UPDATE asset_usages
SET asset_version_id = (
    SELECT k.new_version_id
    FROM _split_copies k
    WHERE k.old_version_id = asset_usages.asset_version_id
      AND EXISTS (
          SELECT 1 FROM _split_moves m
          WHERE m.old_version_id = asset_usages.asset_version_id
            AND m.consumer_type = asset_usages.consumer_type
            AND m.consumer_id = asset_usages.consumer_id
      )
)
WHERE EXISTS (
    SELECT 1 FROM _split_moves m
    WHERE m.old_version_id = asset_usages.asset_version_id
      AND m.consumer_type = asset_usages.consumer_type
      AND m.consumer_id = asset_usages.consumer_id
);

-- 7. Each child's approval pointer names its own approved copy.
UPDATE assets
SET current_approved_version_id = (
    SELECT v.id FROM asset_versions v WHERE v.asset_id = assets.id AND v.status = 'approved'
    ORDER BY v.version_number DESC LIMIT 1
)
WHERE id IN (SELECT child_id FROM _split_copies)
  AND current_approved_version_id = '';

-- 8. The parent's pointer names the version its REMAINING usage reads. After
--    the move, the pointer may still name the version whose usage was moved
--    away — while the first usage sits on a version the second collection had
--    superseded, which is exactly the silence being repaired. A pointer that
--    no usage reads is a row set the mix never plays, so the parent is
--    re-pointed at the version its usage names, and that version carries the
--    approved status itself (the mix's join reads BOTH the pointer and the
--    status).
UPDATE assets
SET current_approved_version_id = (
    SELECT v.id FROM asset_versions v
    WHERE v.asset_id = assets.id
      AND EXISTS (SELECT 1 FROM asset_usages au WHERE au.asset_version_id = v.id)
    ORDER BY v.version_number DESC LIMIT 1
)
WHERE asset_type = 'audio'
  AND id IN (SELECT asset_id FROM _split_assets)
  AND EXISTS (
      SELECT 1 FROM asset_versions v
      JOIN asset_usages au ON au.asset_version_id = v.id
      WHERE v.asset_id = assets.id AND v.id <> assets.current_approved_version_id
  );

UPDATE asset_versions
SET status = 'approved'
WHERE status = 'superseded'
  AND id IN (
      SELECT a.current_approved_version_id FROM assets a
      WHERE a.asset_type = 'audio' AND a.id IN (SELECT asset_id FROM _split_assets)
  )
  AND NOT EXISTS (
      SELECT 1 FROM asset_versions other
      WHERE other.asset_id = asset_versions.asset_id
        AND other.status = 'approved'
        AND other.id <> asset_versions.id
  );

DROP TABLE _split_copies;
DROP TABLE _split_children;
DROP TABLE _split_moves;
DROP TABLE _split_assets;
