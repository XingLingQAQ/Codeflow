-- 003_events.sql — the event fact stream, its delivery outbox and the scope
-- counters that assign its sequence numbers.
--
-- Scope (plan §28 T1.05.a, §15 T1.05, §19.1, §19.2, §19.3, §27.4): the events
-- table, the outbox table and scope_counters. It deliberately does NOT add
-- consumer_offsets (T1.05.b), the dispatcher's lease columns (T1.05.b), the
-- approvals/artifact_versions/bookmarks tables (T1.02/T1.03/T1.11) or any
-- command record (T1.11).
--
-- The three facts this file exists to make true at the database level:
--
--   1. An event is immutable. §19.3/§27.4 treat the event stream as the record
--      of what happened; a correction is a new event, never an edit. BEFORE
--      UPDATE and BEFORE DELETE triggers refuse both, so even a hand-written
--      statement cannot rewrite history. (002 already refused deletes on runs,
--      attempts and input_snapshots for the same reason.)
--   2. A sequence number is allocated inside the caller's transaction. The
--      counters live in this database and are updated by the same transaction
--      that inserts the event, so a rolled-back transaction consumes nothing
--      and the sequence stays gap-free per scope (§19.1 "序号事务分配",
--      §27.4 "同事务分配 sequence、写状态、event、outbox").
--   3. A delivery is a separate, retryable fact. The outbox row is created in
--      the same transaction as the event (§19.3 item 1) but the delivery
--      itself happens later, outside it. dead_letter rows are retained: a
--      failed notification must never be able to delete the evidence that the
--      run reached a terminal state (§15 T1.05).
--
-- Time is Unix milliseconds (UTC) in INTEGER columns, as in 001 and 002.
--
-- Migration 001 already created project_refs, tasks, runs and attempts; the
-- foreign keys below point at those tables because events, runs and attempts
-- live in the same database (§27.4 "单库"), which is what makes "an event
-- cannot name a run that does not exist" checkable by SQLite itself.

-- One counter per event scope. The scope key is the literal form the event
-- envelope uses (§20.2 "run:run_88", §27.3 "project:<id>" / "run:<id>"):
-- 'project:<project_id>' or 'run:<run_id>'. The two families share one table
-- and one key space so that a scope can never be counted twice under two
-- spellings.
--
-- value is the last sequence handed out for that scope; the next allocation is
-- value + 1, so the first event of a scope is 1 (§20.2: after=0 with floor=1 is
-- a legal first replay). A row is only created when the scope is first used, so
-- a project with no events has no counter row at all.
CREATE TABLE scope_counters (
    scope TEXT    PRIMARY KEY,
    -- CHECK(value >= 0) is not decoration: a counter that went backwards (or
    -- negative) would hand out a sequence number twice, and the unique index on
    -- events would then reject a legitimate event for reasons the caller cannot
    -- act on.
    value INTEGER NOT NULL CHECK (value >= 0)
);

-- One event. §19.1 fixes the column list; this table is that list verbatim.
--
-- The same row is readable from two views (§27.4 "同一 event 可从 project/run
-- 投影视图读取"): (project_id, project_seq) is the project timeline and
-- (run_id, run_seq) is the run timeline, both unique. The two sequences are
-- independent counters — a run's event 1 is not a project's event 1 — and the
-- API projects whichever one the subscriber's scope asked for. run_seq is NULL
-- exactly when the event has no run (a Gate decision, a manual document, a
-- system recovery event: §27.1), which is why the two columns are tied
-- together by a CHECK rather than each being independently nullable.
--
-- The two UNIQUE constraints are also the indexes the two read paths need:
-- ListProjectEvents scans (project_id, project_seq) and ListRunEvents scans
-- (run_id, run_seq), so neither list read needs an extra index.
CREATE TABLE events (
    -- Server-allocated opaque id (UUID string, like every other id here).
    -- Consumers de-duplicate on it: the same event arriving over the project
    -- subscription and the run subscription must be shown once (§15 T1.02).
    id             TEXT    PRIMARY KEY,
    project_id     TEXT    NOT NULL REFERENCES project_refs(project_id),
    -- Per-project sequence, allocated in the caller's transaction.
    project_seq    INTEGER NOT NULL CHECK (project_seq >= 1),
    -- NULL for an event that belongs to no run. When present the run must
    -- exist in this database: an event that names a run nobody can read is an
    -- audit hole, not a historical curiosity.
    run_id         TEXT    NULL REFERENCES runs(id),
    run_seq        INTEGER NULL CHECK (run_seq IS NULL OR run_seq >= 1),
    -- NULL unless the event happened while a specific attempt was live (§27.1).
    -- Deliberately no foreign key to attempts: the legacy Flow projection
    -- (T1.05.c) carries an attempt id that lives in the old database until
    -- T12.01 migrates that domain into this file, and an FK would reject those
    -- rows for a reason the projection cannot fix. The store validates that an
    -- attempt id it is given is non-empty and that the identity agrees about the
    -- run; it does not look the attempt up.
    attempt_id     TEXT    NULL,
    -- Closed enum, exactly the 15 types of schemas/execution-event.schema.json.
    -- A new event type extends that schema, this CHECK and runstore's
    -- eventTypes list in one change; the schema_version then decides whether
    -- consumers can still read the stream.
    type           TEXT    NOT NULL CHECK (type IN (
        'approval.approved',
        'approval.decided',
        'approval.required',
        'budget.soft_exceeded',
        'budget.warning',
        'checkpoint.acknowledged',
        'merge.completed',
        'process.exited',
        'process.started',
        'process.terminated',
        'run.completed',
        'run.failed',
        'scheduler.claimed',
        'server.restart',
        'tool.requested'
    )),
    -- Frozen at 1 (§20.2). An incompatible envelope change bumps it; a row
    -- claiming any other version would be unreadable by this binary.
    schema_version INTEGER NOT NULL CHECK (schema_version = 1),
    -- When the fact happened, not when it was written. Server-filled (§27.4).
    occurred_at    INTEGER NOT NULL,
    -- The ExecutionIdentity envelope, validated by the store against
    -- schemas/execution-identity.schema.json before it is written.
    identity_json  TEXT    NOT NULL,
    -- Canonical JSON: object keys sorted, no insignificant whitespace, numbers
    -- preserved exactly (the same rule input_snapshots follows). Canonical so
    -- that payload_hash is recomputable from the stored bytes alone.
    payload_json   TEXT    NOT NULL,
    -- "sha256:" + 64 lowercase hex characters of payload_json's bytes.
    payload_hash   TEXT    NOT NULL CHECK (payload_hash LIKE 'sha256:%'),
    -- A project timeline may not contain the same position twice: a duplicate
    -- would make "after=N" ambiguous for every client that reconnects.
    UNIQUE (project_id, project_seq),
    -- Same rule for the run timeline. Several NULLs are allowed, which is
    -- exactly right: an event without a run has no run position.
    UNIQUE (run_id, run_seq),
    -- run_id and run_seq are both present or both absent. Half a pair would
    -- either index a run-scoped event under no run, or leave a run position
    -- that no run owns.
    CHECK ((run_id IS NULL) = (run_seq IS NULL))
);

-- An event is a fact. §19.3/§27.4: the stream is what happened, so nothing may
-- rewrite it in place — not the store (which exposes no update path at all) and
-- not a statement issued by hand. The message is stable and asserted by
-- runstore's tests; the store maps it to ErrEventImmutable.
CREATE TRIGGER trg_events_immutable_update
BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'event_immutable');
END;

CREATE TRIGGER trg_events_immutable_delete
BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'event_immutable');
END;

-- One pending delivery of one event to one destination. §19.1 fixes the column
-- list; this table is that list verbatim.
--
-- Why an outbox at all (§19.3 last paragraph): the side effect of telling
-- somebody about an event — a WebSocket frame, a desktop notification, an audit
-- export — happens outside the transaction, and the plan forbids broadcasting
-- success before the commit and writing the fact only after a successful
-- broadcast. So the transaction records "this must be delivered" and a
-- dispatcher (T1.05.b) delivers it later, at least once, retrying from this
-- row. Consumers are idempotent on event_id, which is what makes at-least-once
-- safe.
--
-- Writer rule (not a CHECK, because the dispatcher of T1.05.b may legitimately
-- re-arm a row): a pending row must always carry a next_attempt_at. The
-- dispatcher's scan is "pending and due", so a pending row with no due time
-- would be skipped forever — the silent gap §27.4 forbids. AppendEventTx sets
-- it to the event's occurred_at.
CREATE TABLE outbox (
    id              TEXT    PRIMARY KEY,
    -- The event this delivery carries. Deleting the event is impossible
    -- (trigger above), so a delivery can never point at nothing.
    event_id        TEXT    NOT NULL REFERENCES events(id),
    -- Where to deliver. The destination vocabulary belongs to the dispatcher
    -- (T1.05.b); this table only requires that a delivery names one.
    destination     TEXT    NOT NULL,
    state           TEXT    NOT NULL DEFAULT 'pending'
                            CHECK (state IN ('pending', 'delivered', 'dead_letter')),
    -- How many delivery attempts have been made. Only grows.
    attempt_count   INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    -- When the next attempt may run. NULL means "no further attempt is
    -- scheduled", which is the normal state of a delivered row.
    next_attempt_at INTEGER NULL,
    -- The last failure, kept for the operator. NULL means "never failed".
    last_error      TEXT    NULL,
    created_at      INTEGER NOT NULL,
    -- One delivery per (event, destination): a retry re-uses the row instead of
    -- creating a second one, so "delivered twice" stays a transport-level
    -- duplicate that event_id de-duplication absorbs rather than a second
    -- delivery record.
    UNIQUE (event_id, destination)
);

-- The dispatcher's scan is "the pending rows that are due, oldest first". A
-- partial index keeps that scan proportional to the backlog instead of to the
-- delivered history.
CREATE INDEX idx_outbox_pending ON outbox (next_attempt_at) WHERE state = 'pending';

-- A dead-lettered delivery is retained, not deleted (§19.1 "dead-letter 不删
-- 除"; §15 T1.05 "dead-letter 只针对通知/外部投递，不能把运行终态从事实流删
-- 除"). Deleting the row would erase the fact that a notification was abandoned,
-- and with it the operator's only way to find and replay it. Delivered rows stay
-- deletable: the event they carried is still the fact, and retention/cleanup
-- (T12.02) needs a way to trim them.
CREATE TRIGGER trg_outbox_dead_letter_retained
BEFORE DELETE ON outbox
WHEN OLD.state = 'dead_letter'
BEGIN
    SELECT RAISE(ABORT, 'outbox_dead_letter_retained');
END;
