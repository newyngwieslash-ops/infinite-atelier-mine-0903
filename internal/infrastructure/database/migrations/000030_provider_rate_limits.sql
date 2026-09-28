-- 000030_provider_rate_limits.sql — T07: per-provider REQUEST RATE limits
-- (FR-150's 「并发和速率限制」, the half ADR-0018 Ruling 8 named undone).
--
-- # What a rate limit is, and how it differs from the concurrency one
--
-- Concurrency bounds how many jobs are IN FLIGHT at once. A rate limit bounds
-- how many REQUESTS may START inside a rolling window, so even a queue of
-- one-at-a-time jobs cannot exceed the provider's per-minute allowance. The
-- two are configured per provider and enforced in the same place — the
-- scheduler's admission pass — because that is where ADR-0018 Ruling 2 put
-- the decision and where the snapshot-window argument already lives.
--
-- # The schema
--
--   - `rate_limit_per_minute` on provider_configs: the request ceiling per
--     rolling sixty-second window. Zero (the default on every existing row)
--     means UNLIMITED, matching max_concurrency's convention.
--   - `provider_request_windows`: the durable ledger the admission read
--     counts. One row per (provider, window start minute) with the request
--     count. An upsert on each attempt's start keeps the ledger exact without
--     a scan of provider_requests, and old rows are pruned on write.
--
-- The ledger survives a restart by being a TABLE rather than memory: a
-- desktop app restarted mid-minute re-reads the same window's count, which is
-- the boundary test the audit asks for.

ALTER TABLE provider_configs ADD COLUMN rate_limit_per_minute INTEGER NOT NULL DEFAULT 0
    CHECK (rate_limit_per_minute >= 0);

CREATE TABLE provider_request_windows (
    provider_config_id TEXT NOT NULL,
    -- window_start is the UTC minute the window opens, formatted RFC3339.
    window_start TEXT NOT NULL,
    request_count INTEGER NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    PRIMARY KEY (provider_config_id, window_start)
);
