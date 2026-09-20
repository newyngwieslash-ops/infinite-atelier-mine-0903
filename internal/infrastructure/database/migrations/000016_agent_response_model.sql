-- WP-07 follow-up: agent_runs records the model that ANSWERED.
--
-- AGENT_CONTRACTS section 16 requires a run to save "模型和 Provider", and section 13's
-- "模型变更写入 Run" is more specific than it looks: the requirement exists because a run's
-- answering model can differ from the one the policy named — a fallback was used, or the
-- provider served something other than what was asked for. Migration 000015 recorded only
-- model_config_id, which is the REQUESTED model, so a run that fell back cited a model that
-- did not produce it.
--
-- ADD COLUMN with a constant default is all this needs, so no table rebuild is required and
-- existing rows keep a value the CHECK accepts (there is no CHECK on this column: a provider
-- may report a blank model, and the runner leaves the field empty rather than inventing one).
--
-- Constraint of the migration runner: splitSQL splits the file on every semicolon character,
-- comments included, so this file may not contain one outside a statement terminator.
-- TestWP05SplitSQLCompatibility enforces it.

ALTER TABLE agent_runs ADD COLUMN response_model TEXT NOT NULL DEFAULT '';

-- The lookup a reviewer makes is "which runs did NOT use the model I configured", so the
-- index is on the pair rather than on either column alone.
CREATE INDEX idx_agent_runs_response_model ON agent_runs(model_config_id, response_model);
