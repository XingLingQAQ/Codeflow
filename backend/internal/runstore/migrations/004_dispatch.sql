-- 004_dispatch.sql — the dispatcher's lease and fence columns, the claim index
-- and consumer_offsets (T1.05.b).
--
-- Scope (plan §28 T1.05.b, §15 T1.05, §19.2 "scope_counters / consumer_offsets"
-- row, §19.3, §27.4): what a delivery needs in order to be claimed by one
-- dispatcher at a time, retried with backoff, fenced against a stale writer,
-- and recorded as consumed once by a downstream consumer. It deliberately does
-- NOT touch events (003), does not add the approvals / artifact_versions /
-- bookmarks tables (T1.02/T1.03/T1.11) and does not add a command record
-- (T1.11).
--
-- The three facts this file exists to make true at the database level:
--
--   1. A delivery can be leased. §19.3 makes the delivery a side effect that
--      happens outside the transaction which recorded the event, so the
--      dispatcher must be able to say "this row is mine until T" and to recover
--      the row when its owner dies. lease_owner / lease_until are that claim.
--   2. A delivery can be fenced. A dispatcher declared dead by lease expiry may
--      still be alive (a long pause, a hung write) and may still try to write
--      its result. lease_epoch is the fence: every claim increments it, and a
--      write-back names the epoch it was granted, so a stale writer's UPDATE
--      matches no row and cannot overwrite the result of the dispatcher that
--      took over (§19.1's lease rule for tasks, applied to outbox rows; §19.3
--      "至少一次投递" is what makes the duplicate harmless).
--   3. A consumer is idempotent. §19.2 requires consumer + event_id to be
--      unique, with the receipt committed in the same transaction as the
--      consumer's derived state. consumer_offsets is that receipt: the
--      dispatcher-side record of "this consumer has already applied this
--      event", so a redelivery (which at-least-once delivery guarantees will
--      happen) is not applied twice.
--
-- SQLite's ALTER TABLE rules decide the shape of the new columns (verified
-- against the 3.53.3 build this schema is pinned to):
--   - ADD COLUMN accepts a NOT NULL column only together with a non-NULL
--     constant default; lease_epoch therefore carries DEFAULT 0 and every
--     pre-existing row (there are none in production yet, but a test fixture
--     may hold some) is epoch 0 — "never claimed", which is exactly true.
--   - ADD COLUMN may not add a PRIMARY KEY, a UNIQUE constraint or a
--     REFERENCES clause to an existing table. Nothing here needs one.
--   - The added columns are read and written by name, never by position, so
--     appending them at the end of the existing column list is safe for 003's
--     INSERT statements (which name their columns).
--
-- Time is Unix milliseconds (UTC) in INTEGER columns, as in 001-003.

-- Who holds the row, and until when. Both NULL means "not leased", which is
-- the state of a row that was just queued (003), that finished its write-back,
-- or whose lease was released after a failure.
ALTER TABLE outbox ADD COLUMN lease_owner TEXT NULL;
ALTER TABLE outbox ADD COLUMN lease_until INTEGER NULL;

-- The fence, and the delivery timestamp.
--
-- lease_epoch only ever grows: a claim sets it to lease_epoch + 1. The
-- dispatcher's write-back is a CAS on (state='pending', lease_owner,
-- lease_epoch), so a writer whose lease has been taken over by someone else
-- (or by a later round of the same process after a lease expiry) matches no
-- row. Without the epoch, a stale writer that happens to name the same owner
-- string would still match and could mark a row delivered while a newer claim
-- was mid-delivery, or overwrite a delivered row with a retry schedule.
--
-- delivered_at is when the delivery succeeded. It is NULL on pending and
-- dead_letter rows: a dead-lettered row records that a delivery was abandoned,
-- not that it happened.
ALTER TABLE outbox ADD COLUMN lease_epoch INTEGER NOT NULL DEFAULT 0;
ALTER TABLE outbox ADD COLUMN delivered_at INTEGER NULL;

-- The claim scan is "the pending rows of the destinations this dispatcher
-- serves that are due and unleased, oldest first". 003's idx_outbox_pending
-- covers the whole backlog; this one narrows it per destination, so a
-- dispatcher is not woken up by rows it will never claim.
CREATE INDEX idx_outbox_claim ON outbox (destination, next_attempt_at) WHERE state = 'pending';

-- One receipt: "consumer C has applied event E".
--
-- PRIMARY KEY (consumer, event_id) is the whole contract (§19.2 "consumer +
-- event_id 唯一"): the second insert of the same pair conflicts, and the
-- dispatcher/consumer that does the insert learns it is a redelivery and skips
-- the side effect. The primary key's index also answers "has this consumer
-- already applied this event" without a second lookup.
--
-- applied_at is when the consumer applied the event (server clock, Unix
-- milliseconds). The row is written in the same transaction as the consumer's
-- derived state (§19.3), so a rolled-back transaction leaves no receipt and
-- the redelivery is processed again — a receipt without the state it
-- describes would make an event silently unapplied, which is the one failure
-- at-least-once delivery must not produce.
--
-- The foreign key is what keeps a receipt from naming an event nobody can
-- read. It is also why deleting an event is impossible (003's trigger), so a
-- consumer's offset can never dangle.
CREATE TABLE consumer_offsets (
    consumer   TEXT    NOT NULL,
    event_id   TEXT    NOT NULL REFERENCES events(id),
    applied_at INTEGER NOT NULL,
    PRIMARY KEY (consumer, event_id)
);
