-- 009_approvals.sql — approval facts and their one-shot consumption receipts
-- (T2.02.a, plan §28 T2.02.a, §19.1 "approvals", §27.2.3, §27.2.5, §15 T2.02).
--
-- Scope: exactly two tables. `approvals` records what a human or a policy was
-- asked to authorize — which subject, against which canonical fingerprint,
-- under which policy version, until when — and `approval_consumptions` records
-- the one time that authorization was actually spent on a tool call. It
-- deliberately does NOT add:
--   - a decision path (request/decide/expire/invalidate), an event or outbox
--     write, or the CAS contract around them. Those are T2.02.b, in package
--     approval, over this schema. Nothing here writes status other than the
--     initial pending, and no trigger fires on a legal pending -> decided
--     transition;
--   - a grant, a batch or a standing authorization. Batch decision is T3.04.a,
--     scoped grants with limits on path/operation/count are T3.04.b. This
--     table stores one pending-or-decided approval for one subject, which is
--     the base those cards build on, not a replacement for them;
--   - any HTTP surface. §27.3's POST /approvals/:aid/decide belongs to T3.04.a;
--   - a snapshot or a copy of the subject's content. subject_id names the
--     subject (a tool call, a stage gate, a merge candidate) and the
--     fingerprint pins the parameters; the approval never becomes a second
--     store of the thing it authorizes.
--
-- Time is Unix milliseconds (UTC) in INTEGER columns, as in 001-008.
--
-- Amendment policy (plan §26.31, as recorded in 003, 006, 007 and 008): the
-- in-place amendment window closed at T1.04. Migrations are frozen; the runner
-- fails closed on a checksum mismatch (migrate.go verifyRecorded), so any
-- further change is a new, appended migration whose version is the next
-- integer.

-- One approval: the record of what was asked, and (after T2.02.b decides it)
-- what was answered.
--
-- The fingerprint is computed by the store, never supplied by a caller: the
-- approval package hashes the canonical FingerprintInput itself and writes both
-- the hash and the exact canonical input it hashed. Storing the input is what
-- lets T2.02.b re-hash at consumption time and compare against a value that
-- cannot have drifted — the comparison reads a stored string, it does not
-- re-derive one from a live request (§27.2.3: parameters, target content or
-- policy change and the old decision becomes unconsumable history).
CREATE TABLE approvals (
    id             TEXT    PRIMARY KEY,
    project_id     TEXT    NOT NULL REFERENCES project_refs(project_id),
    -- Nullable because a stage gate or a manual merge does not belong to a Run
    -- ("stage gate 可以无 Run", §19.1). A tool approval is the opposite case and
    -- the CHECK at the end of this table enforces it: an authorization to run
    -- one tool call is meaningless outside the attempt that would run it.
    run_id         TEXT    NULL REFERENCES runs(id),
    attempt_id     TEXT    NULL REFERENCES attempts(id),
    subject_type   TEXT    NOT NULL CHECK (subject_type IN ('tool', 'gate', 'merge')),
    -- The subject's identity: a tool_call_id, a stage id, a merge candidate id.
    -- Opaque to this table; the approval package only requires it non-empty.
    subject_id     TEXT    NOT NULL,
    risk           TEXT    NOT NULL CHECK (risk IN ('low', 'medium', 'high')),
    -- Canonical JSON of the requested scope (paths, operations, limits). The
    -- store normalizes it (json.Unmarshal to any then Marshal) so two spellings
    -- of one scope cannot become two rows, and refuses malformed JSON or
    -- duplicate keys before any SQL runs.
    scope_json     TEXT    NOT NULL,
    -- "sha256:" + exactly 64 lowercase hex, the form the store computes and the
    -- only form it accepts. The CHECK pins prefix and length; lowercase is a
    -- store check (like artifact.ValidHash), because SQLite's LIKE is
    -- case-insensitive and a CHECK pretending otherwise would be a lie.
    fingerprint    TEXT    NOT NULL CHECK (
        fingerprint LIKE 'sha256:%' AND length(fingerprint) = 71
    ),
    -- The exact canonical JSON that was hashed to produce `fingerprint`. Kept
    -- so an auditor — and T2.02.b's consumption re-check — can re-hash the
    -- stored input instead of trusting the hash beside it.
    fingerprint_input_json TEXT NOT NULL,
    status         TEXT    NOT NULL CHECK (
        status IN ('pending', 'approved', 'rejected', 'expired', 'invalidated')
    ),
    -- The policy version in force when the approval was requested (§27.2.3).
    -- A later evaluation under a different version cannot consume this row.
    policy_version TEXT    NOT NULL,
    requested_at   INTEGER NOT NULL,
    expires_at     INTEGER NOT NULL,
    -- Set exactly when the status leaves pending. Who decided is free text
    -- (user id, system component): the actor identity contract lives in the
    -- service layer, not in a CHECK a later card would have to migrate.
    decided_by     TEXT    NULL,
    decided_at     INTEGER NULL,
    -- Monotonic decision counter. T2.02.b decides with an expected revision
    -- (CAS); a loser of a concurrent cancel/approve reads the row again rather
    -- than overwriting the winner (§27.2.5).
    revision       INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),

    -- Every deadline is strictly positive: a zero or negative expiry would make
    -- an approval that is already dead on arrival, which the store refuses in
    -- Go before any SQL runs as well.
    CHECK (expires_at > 0),
    -- Time runs forward: an approval may not expire before it was requested.
    CHECK (expires_at > requested_at),
    -- A tool approval must name the run *and* the attempt it authorizes. This is
    -- §28 T2.02.a's "tool 必须关联有效 attempt" as a row invariant: the FK
    -- proves the attempt exists, this CHECK proves one was named at all. The
    -- project half — the run belongs to project_id and the attempt to that run
    -- — is the store's job; see the note below the tables.
    CHECK (subject_type <> 'tool' OR (run_id IS NOT NULL AND attempt_id IS NOT NULL)),
    -- decided_by and decided_at are a pair, and the pair is exactly the
    -- difference between pending and decided: a pending row carries neither, an
    -- approved/rejected row both (who said yes, and when), and an
    -- expired/invalidated row at least the time it stopped being actionable.
    CHECK (
        (status = 'pending' AND decided_by IS NULL AND decided_at IS NULL)
        OR (status IN ('approved', 'rejected')
            AND decided_by IS NOT NULL AND decided_at IS NOT NULL)
        OR (status IN ('expired', 'invalidated')
            AND decided_at IS NOT NULL)
    )
);

-- "The approvals of one run", the read a cancel or a run-finish path makes to
-- find the pending rows it must invalidate (§27.2.5).
CREATE INDEX idx_approvals_run ON approvals (run_id);

-- "The pending approvals of one project", the list behind T3.04.a's review
-- screen. Composite, so the project filter and the status filter are one seek
-- rather than a scan of every approval the project ever made.
CREATE INDEX idx_approvals_project_status ON approvals (project_id, status);

-- A decision is final: no statement may move a row out of approved, rejected,
-- expired or invalidated. This is the database half of §19.1's "已批准历史不改"
-- (and §27.2.5's "输方读取当前状态，不反向复活终态"): a bug in T2.02.b's CAS
-- cannot resurrect a decision that was already answered, and an expired
-- approval cannot be edited back into something consumable. The store maps this
-- message to approval.ErrApprovalNotPending.
--
-- Only the status is pinned. A decided row may still be read, and T2.02.b may
-- touch decided_at/decided_by/revision inside the one transaction that performs
-- the decision itself (at that moment OLD.status is still pending, so this
-- trigger does not fire). The trigger below covers what happens after.
CREATE TRIGGER trg_approvals_terminal_is_final
BEFORE UPDATE ON approvals
WHEN OLD.status <> 'pending'
BEGIN
    SELECT RAISE(ABORT, 'approval_terminal_is_final');
END;

-- A pending row's *binding* is immutable too: the fingerprint, the canonical
-- input behind it, the subject, the project, the run/attempt pins, the scope and
-- the policy version may not change, even while the row is still pending.
-- §27.2.3 is explicit — "Approval 绑定 subject + 规范化参数 hash +
-- base_manifest_hash + policy version + agent revision + scope" and "参数、目标
-- 内容或策略改变则原决定成为不可消费历史，新建 approval" — so a caller whose
-- parameters changed must create a new approval; editing this one in place would
-- let a decision that was shown to a human ("approve `ls -la`") silently become
-- an authorization for something else. The policy version is also inside
-- fingerprint_input_json; freezing the column keeps the two copies equal.
--
-- What stays editable while pending: risk, the deadlines, decided_* and
-- revision. That is deliberate — T2.02.b's decision writes decided_*/revision,
-- and nothing here forbids a future card from re-classifying risk or extending a
-- deadline through an explicit write.
CREATE TRIGGER trg_approvals_binding_is_frozen
BEFORE UPDATE ON approvals
WHEN NEW.id <> OLD.id
  OR NEW.project_id <> OLD.project_id
  OR NEW.run_id IS NOT OLD.run_id
  OR NEW.attempt_id IS NOT OLD.attempt_id
  OR NEW.subject_type <> OLD.subject_type
  OR NEW.subject_id <> OLD.subject_id
  OR NEW.fingerprint <> OLD.fingerprint
  OR NEW.fingerprint_input_json <> OLD.fingerprint_input_json
  OR NEW.scope_json <> OLD.scope_json
  OR NEW.policy_version <> OLD.policy_version
BEGIN
    SELECT RAISE(ABORT, 'approval_binding_is_frozen');
END;

-- An approval is history and is never deleted, pending or decided: a retry keeps
-- the old run's approvals (§27.2.1), a decision is never rewritten (§19.1), and
-- a pending approval that stops mattering becomes expired or invalidated rather
-- than disappearing. runstore refuses DELETE on every other history table
-- (runs, attempts, input_snapshots, events, outbox, legacy_event_sources) for
-- the same reason. The store maps this message to approval.ErrApprovalIsHistory.
CREATE TRIGGER trg_approvals_no_delete
BEFORE DELETE ON approvals
BEGIN
    SELECT RAISE(ABORT, 'approval_is_history');
END;

-- One consumption receipt: the proof that an authorization was spent exactly
-- once, on exactly one tool call. The row is the mutex — the primary key is the
-- arbiter, so two racing consumers cannot both believe they were granted
-- (§27.2.5 "同一 tool_call_id 最多一次授权消费", §28 T2.02.a "唯一
-- (approval_id,tool_call_id) 消费回执").
CREATE TABLE approval_consumptions (
    approval_id TEXT    NOT NULL REFERENCES approvals(id),
    -- The tool call that was authorized. This is the id the backend itself gave
    -- the call (execbackend calls it ProviderRef), and it is part of the
    -- approval's identity: the fingerprint pins the request's parameters, this
    -- pins which concrete call they belong to, and §15 T1.07.b recorded that the
    -- second half must be carried alongside the first. It equals the approval's
    -- subject_id: a tool approval is requested for one call.
    tool_call_id TEXT   NOT NULL,
    -- The attempt that spent the authorization. A foreign key, so a receipt
    -- cannot name an attempt this database has never seen; the rule "the
    -- attempt is the approval's own" is checked by the store and, as a
    -- backstop, by trg_approval_consumptions_require_grant below.
    attempt_id  TEXT    NOT NULL REFERENCES attempts(id),
    consumed_at INTEGER NOT NULL,
    -- A comparison and a write inside one statement, so the second row for one
    -- approval+call cannot be written even by a consumer that forgot to check
    -- first. The two constraints answer two different questions:
    --   PRIMARY KEY (approval_id, tool_call_id)   this authorization is spent;
    --                                             a retry of the same call is
    --                                             the same consumption;
    --   UNIQUE (attempt_id, tool_call_id)         this tool call is over; an
    --                                             approval cannot authorize a
    --                                             call that already happened.
    -- Measured on the pinned engine (SQLite 3.53.3 via modernc.org/sqlite):
    -- when a statement violates both, the reported message names only the
    -- UNIQUE constraint, so a retry of the same pair is refused as
    -- "attempt_id, tool_call_id" rather than as the primary key. Both are one
    -- fact to the caller — the authorization is gone — and
    -- approval.RecordConsumptionTx maps either message to
    -- approval.ErrAlreadyConsumed.
    --
    -- A tool approval can only be spent on its own subject (the trigger
    -- trg_approval_consumptions_require_grant below), so today one approval has
    -- at most one receipt. The key stays (approval_id, tool_call_id) as §28
    -- T2.02.a specifies: T3.04's standing grants, which authorize several calls
    -- under one approval, will relax the subject rule in a migration of their
    -- own, not rewrite this table.
    PRIMARY KEY (approval_id, tool_call_id),
    UNIQUE (attempt_id, tool_call_id)
);

-- The other direction of the same question: "which calls has this attempt
-- already consumed". The UNIQUE above already indexes (attempt_id,
-- tool_call_id); this index is the one a recovery or a reconciliation walk
-- wants when it starts from an attempt and lists what it spent.
CREATE INDEX idx_approval_consumptions_attempt ON approval_consumptions (attempt_id);

-- A receipt is evidence, and evidence is not rewritten. §27.2.5's one-shot
-- consumption is only meaningful if the record of it cannot be edited to point
-- at another call or deleted to make room for a second one, so both statements
-- are refused outright. The store exposes no UPDATE or DELETE path; reaching
-- this trigger means a statement was issued by hand. The message is stable and
-- mapped to approval.ErrConsumptionImmutable.
CREATE TRIGGER trg_approval_consumptions_no_update
BEFORE UPDATE ON approval_consumptions
BEGIN
    SELECT RAISE(ABORT, 'approval_consumption_is_immutable');
END;

CREATE TRIGGER trg_approval_consumptions_no_delete
BEFORE DELETE ON approval_consumptions
BEGIN
    SELECT RAISE(ABORT, 'approval_consumption_is_immutable');
END;

-- A receipt needs a grant: the approval must be an approved tool approval whose
-- subject is this tool call, made for this attempt, and the consumption must
-- fall before its deadline. An approved row is terminal and never becomes
-- expired (trg_approvals_terminal_is_final), so the deadline is checked here, at
-- the moment the grant is spent (T2.02 "过期授权拒绝"). consumed_at is the
-- service's clock, not the database's: the check keeps a logic error from
-- spending a dead grant, and tests drive it with a fake clock (T2.02.c).
--
-- approval.RecordConsumptionTx checks every condition first and reports which
-- one failed; this trigger is the backstop for a statement that skipped it, and
-- its message is mapped to approval.ErrApprovalNotConsumable. It runs before
-- the key constraints, so a retry of a receipt that was legitimately written
-- still reaches the UNIQUE check and is reported as already consumed.
CREATE TRIGGER trg_approval_consumptions_require_grant
BEFORE INSERT ON approval_consumptions
WHEN NOT EXISTS (
    SELECT 1 FROM approvals a
    WHERE a.id = NEW.approval_id
      AND a.status = 'approved'
      AND a.subject_type = 'tool'
      AND a.subject_id = NEW.tool_call_id
      AND a.attempt_id = NEW.attempt_id
      AND NEW.consumed_at < a.expires_at
)
BEGIN
    SELECT RAISE(ABORT, 'approval_consumption_without_grant');
END;

-- "The run is in project_id and the attempt belongs to that run" is NOT
-- expressible as a composite foreign key here, for the same reason 008 records
-- for artifact_versions: runs has no UNIQUE (id, project_id) that a composite
-- key could target, and adding one would mean altering 001's runs table (and
-- its checksum) for a constraint this card does not own. The store therefore
-- checks it in Go: approval.CreateApprovalTx reads runs.project_id and
-- attempts.run_id and refuses a mismatch with ErrRunProjectMismatch before any
-- row is written. A hand-written INSERT naming a foreign project's run is not
-- caught by the database; that gap is recorded here so the next reader does not
-- assume the FK covers it.
--
-- The same reasoning applies to project_id itself: it is a plain FK, so it
-- proves the project was captured, but "the FK target's row is the one the
-- caller meant" is the store's check (and, for tool approvals, the run/attempt
-- pair the store verifies is what closes the loop).
