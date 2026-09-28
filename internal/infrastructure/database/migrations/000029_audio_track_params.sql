-- 000029_audio_track_params.sql — T05's persistence start: per-USAGE audio
-- track parameters.
--
-- # Why the usage and not the version
--
-- The mix reads usages on a shot. A version (one approved music bed, for
-- example) can be used by several shots with different placements, so trim
-- and volume belong to the USE — the same dimensional argument migration
-- 000027 was built on. The column is a JSON document rather than six sparse
-- columns because the parameters grow per feature and a document is what the
-- timeline read and the mixer both parse. Empty stays the default and means
-- "no parameter was set", which the mixer already handles (gain zero falls
-- back to the role default, start falls back to the shot).
--
-- SQLite ADD COLUMN with a constant default is an in-place change, so no
-- rebuild is needed — unlike 000009/000023/000028.

ALTER TABLE asset_usages ADD COLUMN params_json TEXT NOT NULL DEFAULT '';
