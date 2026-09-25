-- 001_runtime.sql — the minimal persistent runtime domain for codeflow.db.
--
-- Scope (plan §15 T1.01, §28 T1.01.a): read-only snapshots of legacy parents,
-- the minimal Task with its S1 claim fields, Run and Attempt. This migration
-- deliberately does NOT create events/outbox (T1.05), command records (T1.11),
-- approval or usage tables; later cards append new migrations and must not
-- rename anything defined here (in particular the Attempt status set).
--
-- Cross-file rule (§27.1): the legacy Project/Flow/Session/Binding/Agent rows
-- live in the old database and are never reachable by a foreign key from this
-- file. project_refs is the local anchor that makes "a wrong parent id cannot
-- be inserted" true inside codeflow.db: every runtime fact points at a
-- captured snapshot instead of at the legacy row.
--
-- Time is Unix milliseconds (UTC) in INTEGER columns; money is an integer
-- amount in minor units plus a currency code, never a float (§27.1).
--
-- The migration runner creates runstore_migrations itself and records this
-- file's version and checksum there; this script must not touch it, and must
-- never use PRAGMA user_version (T12.01 migrates other domains into this same
-- file and a single-slot version counter would collide).

-- Legacy project snapshot. state is the only mutable column: archiving a
-- project after capture must make new runs refuse to start (§27.1), while the
-- captured hash stays exactly as captured.
CREATE TABLE project_refs (
    project_id      TEXT    PRIMARY KEY,
    -- NULL when the legacy projects table has no revision column; it is not a
    -- substitute for snapshot_hash.
    source_revision INTEGER NULL,
    snapshot_hash   TEXT    NOT NULL,
    state           TEXT    NOT NULL CHECK (state IN ('active', 'archived')),
    captured_at     INTEGER NOT NULL,
    verified_at     INTEGER NOT NULL
);

-- Read-only snapshots of the other legacy resources a runtime fact depends
-- on. The composite primary key is the natural identity of a reference.
CREATE TABLE legacy_resource_refs (
    project_id      TEXT    NOT NULL REFERENCES project_refs(project_id),
    kind            TEXT    NOT NULL CHECK (kind IN ('flow', 'session', 'binding', 'agent_revision')),
    resource_id     TEXT    NOT NULL,
    source_revision INTEGER NULL,
    snapshot_hash   TEXT    NOT NULL,
    captured_at     INTEGER NOT NULL,
    PRIMARY KEY (project_id, kind, resource_id)
);

-- Minimal Task. flow_id/stage_id are legacy references with no cross-file
-- foreign key; a task not attached to a flow leaves both NULL.
--
-- claim fields (lease_owner/lease_until/lease_epoch) are the S1 single-worker
-- claim: owner and deadline are set or cleared together, and lease_epoch is
-- the fencing token that only ever grows (T1.04.c consumes it).
CREATE TABLE tasks (
    id          TEXT    PRIMARY KEY,
    project_id  TEXT    NOT NULL REFERENCES project_refs(project_id),
    flow_id     TEXT    NULL,
    stage_id    TEXT    NULL,
    title       TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('code', 'document', 'manual')),
    status      TEXT    NOT NULL CHECK (status IN ('ready', 'queued', 'running', 'waiting_review', 'completed', 'failed', 'cancelled')),
    priority    INTEGER NOT NULL DEFAULT 0,
    -- input_json/input_hash are the frozen task input: changing a task's input
    -- requires creating a new task, never editing this one (§27.2.7).
    input_json  TEXT    NOT NULL,
    input_hash  TEXT    NOT NULL,
    lease_owner TEXT    NULL,
    lease_until INTEGER NULL,
    lease_epoch INTEGER NOT NULL DEFAULT 0 CHECK (lease_epoch >= 0),
    revision    INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    -- Redundant with the primary key, but required as the target of the
    -- composite foreign key below so that a run cannot name a task from a
    -- different project.
    UNIQUE (id, project_id),
    -- A claim is owner + deadline or nothing; a half-filled lease would let a
    -- worker believe it owns a task forever.
    CHECK ((lease_owner IS NULL) = (lease_until IS NULL))
);

-- One Run is one execution over one frozen input (§27.2.1). Every input column
-- below is frozen at insert by trigger; only status, revision, updated_at and
-- finished_at may move afterwards. A retry creates a new Run.
CREATE TABLE runs (
    id                  TEXT    PRIMARY KEY,
    task_id             TEXT    NOT NULL,
    project_id          TEXT    NOT NULL,
    -- The idempotency command that created this run; NULL for system-created
    -- runs. UNIQUE when present (several NULLs are allowed).
    command_id          TEXT    NULL UNIQUE,
    binding_id          TEXT    NOT NULL,
    binding_revision    INTEGER NOT NULL CHECK (binding_revision >= 1),
    -- Required even for a non-Git workspace, which is why base_commit is
    -- separate and nullable (§27.5.1).
    base_manifest_hash  TEXT    NOT NULL,
    base_commit         TEXT    NULL,
    agent_revision_id   TEXT    NOT NULL,
    input_snapshot_id   TEXT    NOT NULL,
    input_snapshot_hash TEXT    NOT NULL,
    budget_json         TEXT    NOT NULL DEFAULT '{}',
    status              TEXT    NOT NULL CHECK (status IN ('queued', 'starting', 'running', 'waiting_approval', 'paused', 'cancelling', 'recovering', 'completed', 'failed', 'cancelled', 'expired')),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    retry_of_run_id     TEXT    NULL REFERENCES runs(id),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    finished_at         INTEGER NULL,
    -- Composite foreign key: the task must exist AND belong to the same
    -- project. A run whose project_id disagrees with its task is rejected by
    -- the database, not only by the service layer.
    FOREIGN KEY (task_id, project_id) REFERENCES tasks(id, project_id)
);

-- At most one non-terminal Run per task (§19.1 tasks row). Terminal states are
-- listed explicitly here, so this index must be revisited if a later migration
-- ever adds a Run state.
CREATE UNIQUE INDEX idx_runs_one_active_per_task
    ON runs (task_id)
    WHERE status NOT IN ('completed', 'failed', 'cancelled', 'expired');

-- One backend process attempt for a Run. S1 runs a single Attempt per Run;
-- attempt_no exists so a later card can add attempts without a rewrite.
CREATE TABLE attempts (
    id               TEXT    PRIMARY KEY,
    run_id           TEXT    NOT NULL REFERENCES runs(id),
    attempt_no       INTEGER NOT NULL CHECK (attempt_no >= 1),
    backend          TEXT    NOT NULL,
    backend_version  TEXT    NULL,
    status           TEXT    NOT NULL CHECK (status IN ('starting', 'running', 'exited', 'terminated', 'abandoned')),
    owner_instance   TEXT    NULL,
    -- PID alone cannot prove process ownership; process_start_id must be
    -- recorded alongside it (§27.5.6).
    pid              INTEGER NULL,
    process_start_id TEXT    NULL,
    started_at       INTEGER NULL,
    finished_at      INTEGER NULL,
    exit_code        INTEGER NULL,
    exit_reason      TEXT    NULL,
    created_at       INTEGER NOT NULL,
    UNIQUE (run_id, attempt_no)
);

-- A Run may hold at most one active Attempt; an attempt that already exited
-- must not be able to block a new one.
CREATE UNIQUE INDEX idx_attempts_one_active_per_run
    ON attempts (run_id)
    WHERE status IN ('starting', 'running');

-- ---------------------------------------------------------------------------
-- Database-level guards. These are the last line of defence behind the store
-- (T1.01.b) and the state machine (T1.02); they exist because a Run's frozen
-- input and its terminal state are irreversible facts, and a bug in one caller
-- must not be able to rewrite history. Error names are stable and asserted by
-- runstore/schema_test.go.
-- ---------------------------------------------------------------------------

-- Frozen input: every input column must compare equal with IS NOT (so NULLs
-- compare correctly) or the update is rejected. budget_json is frozen too: the
-- budget is part of what was authorised for this execution.
--
-- Ordering note: this trigger is created before trg_runs_revision_incremented
-- and SQLite fires BEFORE UPDATE triggers in creation order, so a statement
-- that both rewrites a frozen column and bumps the revision is reported as
-- run_frozen_input_immutable. schema_test.go asserts that specific message, so
-- do not reorder the runs triggers below without updating the test.
CREATE TRIGGER trg_runs_frozen_input_immutable
BEFORE UPDATE ON runs
WHEN NEW.task_id             IS NOT OLD.task_id
  OR NEW.project_id          IS NOT OLD.project_id
  OR NEW.command_id          IS NOT OLD.command_id
  OR NEW.binding_id          IS NOT OLD.binding_id
  OR NEW.binding_revision    IS NOT OLD.binding_revision
  OR NEW.base_manifest_hash  IS NOT OLD.base_manifest_hash
  OR NEW.base_commit         IS NOT OLD.base_commit
  OR NEW.agent_revision_id   IS NOT OLD.agent_revision_id
  OR NEW.input_snapshot_id   IS NOT OLD.input_snapshot_id
  OR NEW.input_snapshot_hash IS NOT OLD.input_snapshot_hash
  OR NEW.budget_json         IS NOT OLD.budget_json
  OR NEW.retry_of_run_id     IS NOT OLD.retry_of_run_id
  OR NEW.created_at          IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'run_frozen_input_immutable');
END;

-- A terminal Run can never leave its terminal state (§21.1, §27.2.5). The
-- trigger only forbids leaving a terminal state; it does not forbid updating
-- other columns (e.g. projection bookkeeping) while staying terminal.
CREATE TRIGGER trg_runs_terminal_immutable
BEFORE UPDATE ON runs
WHEN OLD.status IN ('completed', 'failed', 'cancelled', 'expired')
 AND NEW.status IS NOT OLD.status
BEGIN
    SELECT RAISE(ABORT, 'run_terminal_immutable');
END;

-- Every Run update must be an explicit revision bump, so that a lost update
-- fails loudly instead of silently overwriting a concurrent transition.
CREATE TRIGGER trg_runs_revision_incremented
BEFORE UPDATE ON runs
WHEN NEW.revision IS NOT OLD.revision + 1
BEGIN
    SELECT RAISE(ABORT, 'run_revision_not_incremented');
END;

-- Task input is immutable: a changed input is a new task (§27.2.7).
CREATE TRIGGER trg_tasks_input_immutable
BEFORE UPDATE ON tasks
WHEN NEW.input_json IS NOT OLD.input_json
  OR NEW.input_hash IS NOT OLD.input_hash
  OR NEW.project_id IS NOT OLD.project_id
  OR NEW.kind       IS NOT OLD.kind
BEGIN
    SELECT RAISE(ABORT, 'task_input_immutable');
END;

-- completed/cancelled tasks cannot be reopened. failed is intentionally absent
-- here: an explicit retry CASes it back to queued (§27.2.7).
CREATE TRIGGER trg_tasks_terminal_immutable
BEFORE UPDATE ON tasks
WHEN OLD.status IN ('completed', 'cancelled')
 AND NEW.status IS NOT OLD.status
BEGIN
    SELECT RAISE(ABORT, 'task_terminal_immutable');
END;

-- An attempt's identity is fixed at insert; a different run/number/backend
-- would be a different attempt.
CREATE TRIGGER trg_attempts_identity_immutable
BEFORE UPDATE ON attempts
WHEN NEW.run_id     IS NOT OLD.run_id
  OR NEW.attempt_no IS NOT OLD.attempt_no
  OR NEW.backend    IS NOT OLD.backend
BEGIN
    SELECT RAISE(ABORT, 'attempt_identity_immutable');
END;

-- A finished attempt never becomes active again, so a stale process cannot
-- re-register as the live owner of its run.
CREATE TRIGGER trg_attempts_terminal_immutable
BEFORE UPDATE ON attempts
WHEN OLD.status IN ('exited', 'terminated', 'abandoned')
 AND NEW.status IS NOT OLD.status
BEGIN
    SELECT RAISE(ABORT, 'attempt_terminal_immutable');
END;
