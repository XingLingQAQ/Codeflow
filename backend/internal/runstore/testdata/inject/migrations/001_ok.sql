-- Synthetic first migration used by TestMigrateFailureRollsBack. It is not part
-- of the embedded set: the runner is driven through migrateFS with this
-- directory so a failing second migration can be injected without touching
-- migrations/001_runtime.sql.
CREATE TABLE inject_first (
    id TEXT PRIMARY KEY
);
