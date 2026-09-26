-- 005_legacy_sources.sql — the source-event identity of a projected legacy fact.
--
-- Scope (plan §28 T1.05.c, §27.1 item 4, §19.3): the mapping table that makes
-- "the same legacy source event becomes exactly one runtime event" a database
-- fact rather than a property of one projector's memory. It deliberately does
-- NOT touch events, outbox or consumer_offsets (003/004), does not add the
-- approvals / artifact_versions / bookmarks tables (T1.02/T1.03/T1.11) and does
-- not migrate the legacy Flow domain itself (T3.01/T12.01).
--
-- Why this table exists. §19.3 and §27.1 put the legacy Flow store and
-- codeflow.db in two files until T3.01, so a Flow write and its projection
-- cannot share one transaction. The bridge is a local outbox in the source
-- database plus an at-least-once projector: the projector re-reads the same
-- source event after every crash and retry, so the projection target must be
-- idempotent on the source event's identity. "source_store + source_event_id"
-- is that identity — not the runtime event id, which the runtime allocates, and
-- not the local outbox row, which the projector advances.
--
-- The identity is per source store on purpose: two different legacy databases
-- may both have an event called "ev-1", and merging them under one key would
-- silently drop the second database's timeline. source_store names the store
-- (the flow store's own name, e.g. "floweng"), so the pair is what is unique.
--
-- This table is the reason a duplicate projection consumes no sequence number
-- (§19.1 "序号事务分配"): ProjectLegacyEventTx checks the mapping inside the
-- caller's transaction and, when the pair is already present, returns the event
-- that was stored the first time without touching scope_counters or outbox at
-- all. A retry therefore cannot produce a second project_seq for one fact, which
-- is what makes "every event has a position and the position is gap-free" hold
-- for a stream fed by an at-least-once producer.
--
-- Immutability: like events (003), a mapping row is a fact and is written once.
-- A projection that could be re-pointed at a different event would let one
-- source event's identity be reused for a second runtime event, and the
-- de-duplication the whole design rests on would stop being true. The BEFORE
-- UPDATE / BEFORE DELETE triggers below refuse both, so not even a hand-written
-- statement can rewrite the mapping.
--
-- The event_id foreign key keeps a mapping from naming an event nobody can read,
-- and it is also why deleting an event is impossible (003's trigger): a mapping
-- can never dangle, and a source event that was projected stays projected for as
-- long as the runtime database holds its history.
--
-- Amended in place by contract amendment CA-2 (2026-09-26, plan section 26.31):
-- the projected event type is legacy.flow_event, project-scoped with no run.
-- That amendment changed 003's type CHECK only; this table is type-agnostic
-- (ProjectLegacyEventTx projects any legal event type) because the projection
-- key is the source identity, not the vocabulary.
--
-- Amendment policy, stated here because this is the last migration of the T1.05
-- card: migrations 001-004 were amended in place by CA-1/CA-2 only because
-- runstore was not yet wired into production start-up (T1.04). This file is the
-- first one written after that window closes; from T1.04 on, migrations are
-- frozen and any change is a new, appended migration. Amending a recorded
-- migration in place would change its checksum, and the runner fails closed on a
-- checksum mismatch (migrate.go verifyRecorded) rather than re-applying it.
--
-- Time is Unix milliseconds (UTC) in INTEGER columns, as in 001-004.

-- One source event that has been projected into this database.
--
-- The composite primary key is the de-duplication key of the projection: the
-- second INSERT of the same (source_store, source_event_id) conflicts, which is
-- the database-level half of ProjectLegacyEventTx's check — under BEGIN
-- IMMEDIATE the check cannot actually race, but correctness must not depend on
-- the caller's transaction mode, exactly as nextScopeSeq's upsert does not.
CREATE TABLE legacy_event_sources (
    -- Which legacy store the event came from. Bounded because it is a store
    -- name, not a free-form label: an empty or unbounded value would make the
    -- identity ambiguous (or the row arbitrarily large) for no benefit.
    source_store    TEXT    NOT NULL CHECK (length(source_store) BETWEEN 1 AND 64),
    -- The source event's own id, as the legacy store recorded it. It is the
    -- FlowEvent id in the legacy vocabulary; the runtime never generates it and
    -- never reinterprets it.
    source_event_id TEXT    NOT NULL CHECK (length(source_event_id) BETWEEN 1 AND 128),
    -- The runtime event this source event became. UNIQUE, not merely a foreign
    -- key: one runtime event must not be the projection of two different source
    -- events, or replaying one of them would be ambiguous. The unique index also
    -- answers "is this id a projected event" without a second lookup.
    event_id        TEXT    NOT NULL UNIQUE REFERENCES events(id),
    -- When the projection was recorded (server clock, Unix milliseconds). It is
    -- the runtime side's own timestamp: the source event's occurred_at travels
    -- in the event row, and the two are deliberately different facts.
    projected_at    INTEGER NOT NULL,
    PRIMARY KEY (source_store, source_event_id)
);

-- A mapping is a fact: the source event was projected, once. Rewriting it would
-- let one source event be re-pointed at a second runtime event (or at none), and
-- the de-duplication the projector relies on would stop being true. The message
-- is stable and asserted by runstore's tests, which check it on the raw
-- constraint error: the store exposes no update/delete path for this table, so
-- there is no typed error to map it to, and T1.05.c's sentinel list is
-- deliberately limited to the two projection errors (ErrInvalidLegacySource,
-- ErrLegacySourceConflict).
CREATE TRIGGER trg_legacy_event_sources_immutable_update
BEFORE UPDATE ON legacy_event_sources
BEGIN
    SELECT RAISE(ABORT, 'legacy_source_immutable');
END;

CREATE TRIGGER trg_legacy_event_sources_immutable_delete
BEFORE DELETE ON legacy_event_sources
BEGIN
    SELECT RAISE(ABORT, 'legacy_source_immutable');
END;
