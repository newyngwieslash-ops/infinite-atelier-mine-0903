-- 000033_app_settings.sql — RP-06.3 + RP-09.2: application settings with
-- revision-guarded writes.
--
-- # Why a table, and why now
--
-- Two features need the same primitive: the AGENT enable/disable switch that
-- must survive a restart (RP-06.3's RED case), and the auto-backup
-- configuration the scheduler currently reads from constants (RP-09.2). Both
-- are key-value settings with a revision so a concurrent writer loses
-- loudly instead of silently.
--
-- # The shape
--
-- One row per setting: the value is TEXT (JSON for structured values), the
-- revision guards read-modify-write cycles, and updated_at is the audit
-- trail's minimum. A setting absent from the table means "use the code
-- default" — the same convention every column default in this schema uses,
-- so an upgrade changes nothing until a user changes something.

CREATE TABLE app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL CHECK (revision >= 1),
    updated_at TEXT NOT NULL
);
