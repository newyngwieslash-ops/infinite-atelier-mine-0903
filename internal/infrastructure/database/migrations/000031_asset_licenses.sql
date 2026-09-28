-- 000031_asset_licenses.sql — T17's per-asset rights record (2026-09-26 audit).
--
-- PRD R9's mitigation was THIRD_PARTY_NOTICES, which covers the SOFTWARE'S
-- dependencies. The audit's point is that it says nothing about the ASSETS a
-- user imports or a provider generates. The final ruleset's licence rule
-- today always reports the gap (LicensesChecked is hardcoded false), because
-- there was nowhere to record a licence.
--
-- Three columns on assets, all defaulting empty, are the record:
--   - license: the licence text or SPDX-style identifier the user states
--   - license_source: where it came from (the user, the asset's origin site,
--     the provider's terms) — a licence without provenance is not verifiable
--   - allows_export_use: an explicit tri-state via '' / '1' / '0'. Empty
--     means UNKNOWN, which the final ruleset reports rather than guesses.
--
-- Empty defaults keep every existing row unchanged: the rule reports the
-- same gap it always did until a user fills the record in.

ALTER TABLE assets ADD COLUMN license TEXT NOT NULL DEFAULT '';
ALTER TABLE assets ADD COLUMN license_source TEXT NOT NULL DEFAULT '';
ALTER TABLE assets ADD COLUMN allows_export_use TEXT NOT NULL DEFAULT '' CHECK (allows_export_use IN ('', '1', '0'));
