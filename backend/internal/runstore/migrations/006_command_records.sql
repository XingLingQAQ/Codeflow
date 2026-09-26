-- 006_command_records.sql — the idempotency ledger of every client-issued
-- command (T1.11.a).
--
-- Scope (plan §28 T1.11.a, §15 T1.11, §19.2 "idempotency_records" row, §27.3,
-- §20.4): one table that makes "(principal, project, operation, command_id) has
-- been used for exactly this request" a database fact. It deliberately does NOT
-- add approvals / artifact_versions / bookmarks (T1.02/T1.03), does not touch
-- events/outbox/consumer_offsets (003/004), does not touch the legacy mapping
-- (005) and does not build the HTTP handler above it (T1.11.b).
--
-- The four facts this table exists to make true at the database level:
--
--   1. A client-supplied command id is the de-duplication key, and the CLIENT
--      generates it, before sending (§20.4: the key is the only identifier a
--      client still holds when a response is lost — the server's request_id
--      belongs to one HTTP trace and the client may never have seen it). The
--      server therefore never invents a command_id; a row only exists because a
--      caller claimed the key the client chose. An empty or whitespace-padded
--      key is refused by the CHECKs below, and the store refuses it in Go before
--      any SQL runs, because " rq_1" and "rq_1" must not become two keys for one
--      client retry.
--   2. The key is scoped: (principal_id, project_id, operation, command_id) is
--      the primary key (§27.3 "幂等作用域为 (principal_id,project_id,operation,
--      command_id)"). Two principals, two projects or two different operations
--      may each use the same client-chosen id without colliding. project_id has
--      NO foreign key to project_refs on purpose: a command may legitimately
--      arrive before the project reference snapshot exists (the reference is
--      captured from the legacy library, and a command that creates the very
--      first runtime resource is exactly the case T1.04 has to support). The
--      authority for "does this project exist and may this principal act on it"
--      is the service layer, not this row — §19 states the same for every
--      cross-file reference.
--   3. The request hash decides "same command" from "reused key". request_hash
--      covers the method, the canonical path and the de-identified request
--      structure plus any secret-reference versions (§27.3) — never an access
--      token. Same key + same hash replays the recorded result; same key +
--      different hash is a 409 idempotency_key_reused and must never execute.
--      The hash is stored, never recomputed from a body, so the comparison
--      cannot drift with a serialization change.
--   4. A command is a state machine, and the database holds the state to the
--      same rules the store applies: in_flight/reconciling are the two active
--      states and may not carry a status code, a response or an expiry;
--      succeeded/failed are terminal and MUST carry status_code, response_json,
--      completed_at and expires_at; expired is a tombstone that keeps the key,
--      the hash and the outcome but drops the response body, so an old key is
--      never executed a second time and never answers with a body nobody is
--      allowed to keep (§27.3 "活动命令不按 TTL 删除，终态默认保留至少 30 天；
--      过期保留 tombstone").
--
-- Why a CHECK and not a trigger: these are invariants of a row, not of a
-- transition, so the statement that writes the row enforces them and any writer
-- (the store, a migration, a hand-written statement) is held to the same rule.
-- The one transition that a row-level CHECK cannot express — "an expired
-- tombstone never goes back to being an active command" — is the trigger at the
-- end of this file.
--
-- Times are Unix milliseconds (UTC) in INTEGER columns, as in 001-005.
--
-- Amendment policy: migrations 001-005 were amended in place by contract
-- amendments CA-1/CA-2 (§26.31) only because runstore was not yet wired into a
-- production start-up (T1.04). That window is closed: this file is written after
-- it, so from here on migrations are frozen and any change is a new, appended
-- migration whose version is the next integer. Amending a recorded migration in
-- place would change its checksum, and the runner fails closed on a checksum
-- mismatch (migrate.go verifyRecorded) rather than re-applying it.

-- One command a client issued: claimed once, executed by exactly one owner, and
-- recorded with the answer the client was (or would have been) given.
CREATE TABLE command_records (
    -- Who issued the command. The principal is half of the identity because two
    -- users may pick the same human-friendly command id. The bound is a length
    -- bound as SQLite counts it (characters); the store's own check counts bytes,
    -- which is the stricter of the two and therefore the safe direction.
    principal_id  TEXT    NOT NULL CHECK (length(principal_id) BETWEEN 1 AND 128),
    -- Which project the command acts on. Deliberately NOT a foreign key to
    -- project_refs: see the header — a command that creates the first runtime
    -- resource of a project arrives before the reference snapshot is captured.
    project_id    TEXT    NOT NULL CHECK (length(project_id) BETWEEN 1 AND 128),
    -- The operation the command performs ("runs.create", "runs.cancel", ...).
    -- Bounded and lower-case by convention in the store; the CHECK only fixes
    -- the length, because the vocabulary of operations belongs to the API layer
    -- (T1.11.b/T1.04) and not to this table.
    operation     TEXT    NOT NULL CHECK (length(operation) BETWEEN 1 AND 64),
    -- The client's own Idempotency-Key, sent before the request was made
    -- (§20.4). Never generated by the server.
    command_id    TEXT    NOT NULL CHECK (length(command_id) BETWEEN 1 AND 128),
    -- sha256 of method + canonical path + de-identified body (see
    -- CommandRequestHash). Stored, never recomputed: the row must be comparable
    -- byte-for-byte years later.
    request_hash  TEXT    NOT NULL CHECK (length(request_hash) BETWEEN 1 AND 256),
    -- The command's state. The closed set is the contract the API layer answers
    -- from (§20.4 maps it to accepted/applied/rejected/reconciling).
    state         TEXT    NOT NULL CHECK (state IN ('in_flight', 'reconciling', 'succeeded', 'failed', 'expired')),
    -- The HTTP status the client was given. NULL while the command is active.
    status_code   INTEGER NULL CHECK (status_code IS NULL OR (status_code BETWEEN 100 AND 599)),
    -- The recorded response body, canonical JSON of one object (the store
    -- refuses anything else before any SQL runs). Dropped when the record
    -- becomes a tombstone.
    response_json TEXT    NULL CHECK (response_json IS NULL OR length(response_json) <= 65536),
    -- The resource the command created, when it created one (T1.04's Run). The
    -- pair is written together or not at all, so a caller can never resolve half
    -- a resource reference.
    resource_type TEXT    NULL,
    resource_id   TEXT    NULL,
    -- Why an unfinished command is being reconciled (§27.3, §20.4). Truncated by
    -- the store to 512 bytes on a rune boundary; the CHECK is a backstop, and it
    -- is a character count, which is never larger than the byte count Go
    -- truncates at.
    last_error    TEXT    NULL CHECK (last_error IS NULL OR length(last_error) <= 512),
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    -- Set exactly when the command reached a terminal state.
    completed_at  INTEGER NULL,
    -- When the recorded response may be replaced by a tombstone. NULL while the
    -- command is active: an active command is never expired, however long it has
    -- been running (§27.3 "活动命令不按 TTL 删除").
    expires_at    INTEGER NULL,

    -- The idempotency scope itself. This is the arbiter of "the key is taken":
    -- two concurrent claims cannot both insert, and a claim that loses the race
    -- reads the winner's row instead of executing a second side effect.
    PRIMARY KEY (principal_id, project_id, operation, command_id),

    -- State and fields move together. A row that says in_flight while carrying
    -- a status code would let a lost-response replay answer with a result that
    -- was never recorded; a terminal row without expires_at would live forever.
    CHECK (
        (state IN ('in_flight', 'reconciling')
             AND status_code IS NULL AND response_json IS NULL
             AND completed_at IS NULL AND expires_at IS NULL)
        OR (state IN ('succeeded', 'failed')
             AND status_code IS NOT NULL AND response_json IS NOT NULL
             AND completed_at IS NOT NULL AND expires_at IS NOT NULL)
        OR (state = 'expired'
             AND response_json IS NULL
             AND completed_at IS NOT NULL AND expires_at IS NOT NULL)
    ),

    -- A resource reference is a pair.
    CHECK ((resource_type IS NULL) = (resource_id IS NULL))
);

-- The retention sweep of ExpireCommandsTx: the terminal rows whose response may
-- become a tombstone, oldest deadline first. Partial, so the index holds only the
-- rows the sweep can touch: active commands and existing tombstones are not in
-- it, and the sweep stays cheap as the ledger grows without bound.
CREATE INDEX idx_command_records_expiry
    ON command_records (expires_at)
    WHERE state IN ('succeeded', 'failed');

-- An expired record is a tombstone and stays one. §27.3 keeps it so an old key is
-- never executed again; if a statement could move the row back to in_flight,
-- the key would silently become claimable a second time, which is exactly the
-- double execution the whole table exists to prevent. Only the state is pinned:
-- the row may still be trimmed by retention (T12.02), and updated_at may move
-- while it exists.
--
-- The message body is stable and asserted by runstore's tests; the store maps it
-- to ErrCommandAlreadyCompleted (errors.go).
CREATE TRIGGER trg_command_records_expired_is_final
BEFORE UPDATE ON command_records
WHEN OLD.state = 'expired' AND NEW.state <> 'expired'
BEGIN
    SELECT RAISE(ABORT, 'command_expired_is_final');
END;
