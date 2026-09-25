-- 002_input_snapshots.sql — the frozen input body a Run points at.
--
-- Scope (plan §28 T1.01.b): the input_snapshots table, the trigger that keeps
-- a Run's input_snapshot_id/hash consistent with a real snapshot row, and the
-- no-hard-delete guards for the runtime history. It deliberately does NOT add
-- events/outbox (T1.05), command records (T1.11), a state-transition table
-- (T1.02) or any backfill of the legacy domain (T12.01).
--
-- Why a separate table rather than a column on runs: 001 froze the snapshot
-- *identity* (id + hash) on the run row, but the body has to live somewhere
-- the revision trigger cannot rewrite, and several runs may pin the same body
-- (a retry re-runs the same frozen input). Storing the body here keeps "the
-- input of a run cannot change under it" true even for the payload, and lets
-- the store verify that the hash a run points at is really the hash of the
-- body it stores.
--
-- Secret rule (§27.1): content_json must never contain a secret value. The
-- store rejects secret-looking keys before it writes (runstore
-- ErrSecretInSnapshot); the column is a plain TEXT body of canonical JSON so
-- that the hash is reproducible from the stored bytes alone.
--
-- Time is Unix milliseconds (UTC) in INTEGER columns, as in 001.

-- One immutable frozen input body. The primary key is the caller's snapshot id
-- (an opaque UUID string, like every other id in this library).
CREATE TABLE input_snapshots (
    id           TEXT    PRIMARY KEY,
    -- The snapshot exists only to pin one project's input: a body captured for
    -- p-1 must not be usable by a run of p-2, which is why the project is part
    -- of the row and not only of the run.
    project_id   TEXT    NOT NULL REFERENCES project_refs(project_id),
    -- Canonical JSON: object keys sorted, no insignificant whitespace, so the
    -- same input always produces the same hash. Numbers are preserved as
    -- written (json.Number), never reformatted through a float.
    content_json TEXT    NOT NULL,
    -- "sha256:" + 64 lowercase hex characters of content_json's bytes.
    content_hash TEXT    NOT NULL,
    created_at   INTEGER NOT NULL
);

-- A snapshot body is written once and never rewritten: the whole point of
-- freezing a run's input is that the bytes the run started with stay readable
-- afterwards. A correction is a new snapshot with a new id (§27.2.7 applies the
-- same rule to task input).
CREATE TRIGGER trg_input_snapshots_immutable
BEFORE UPDATE ON input_snapshots
BEGIN
    SELECT RAISE(ABORT, 'input_snapshot_immutable');
END;

-- A run may only name a snapshot that exists, belongs to the same project and
-- really hashes to the hash the run pins. Without this, a caller could store
-- one body and pin a different hash (or another project's body) and the frozen
-- input would be unverifiable for the rest of the run's life. The store maps
-- this message to ErrInputSnapshotMismatch.
--
-- The three IS NOT NULL guards are not a weakening: all three columns are
-- declared NOT NULL, and a BEFORE INSERT trigger is evaluated before the
-- NOT NULL constraints are enforced. Without the guards a NULL input_snapshot_id
-- would be reported as "run_input_snapshot_mismatch", hiding the real cause and
-- changing the error class T1.01.a's nullability assertions pin. With them, a
-- NULL falls through to the NOT NULL check and a non-NULL mismatch is reported
-- here.
--
-- Note this is a BEFORE INSERT trigger, so it cannot fire on the UPDATE paths
-- the store uses for CAS: a run's snapshot columns are already frozen by 001's
-- run_frozen_input_immutable trigger.
CREATE TRIGGER trg_runs_input_snapshot_must_match
BEFORE INSERT ON runs
WHEN NEW.input_snapshot_id IS NOT NULL
 AND NEW.input_snapshot_hash IS NOT NULL
 AND NEW.project_id IS NOT NULL
 AND NOT EXISTS (
    SELECT 1 FROM input_snapshots s
    WHERE s.id = NEW.input_snapshot_id
      AND s.content_hash = NEW.input_snapshot_hash
      AND s.project_id = NEW.project_id
)
BEGIN
    SELECT RAISE(ABORT, 'run_input_snapshot_mismatch');
END;

-- ---------------------------------------------------------------------------
-- Immutable history. 001's guards all cover UPDATE; nothing stopped a DELETE
-- from erasing the evidence of what ran. Retention and cleanup are a separate
-- card (T12.02) and must go through an explicit migration, so for now the
-- database refuses hard deletes outright: deleting one run would silently
-- orphan its attempts and destroy the audit trail the plan requires.
-- ---------------------------------------------------------------------------

-- attempts is guarded first for the same ordering reason as the runs triggers
-- in 001: SQLite fires BEFORE DELETE triggers in creation order, and a cascade
-- or multi-table delete should report the child-most table.
CREATE TRIGGER trg_attempts_delete_forbidden
BEFORE DELETE ON attempts
BEGIN
    SELECT RAISE(ABORT, 'run_history_delete_forbidden');
END;

CREATE TRIGGER trg_runs_delete_forbidden
BEFORE DELETE ON runs
BEGIN
    SELECT RAISE(ABORT, 'run_history_delete_forbidden');
END;

CREATE TRIGGER trg_input_snapshots_delete_forbidden
BEFORE DELETE ON input_snapshots
BEGIN
    SELECT RAISE(ABORT, 'run_history_delete_forbidden');
END;
