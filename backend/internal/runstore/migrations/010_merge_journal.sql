-- 010_merge_journal.sql — the persistent file-system publish journal of a merge
-- (T1.09.b group 3, plan §19.2 "merge_operations / merge_files", §19.3 item 4,
-- §27.2 item 4, §27.5 items 3-5).
--
-- Scope: exactly two tables, and they are a journal, not a second artifact
-- store. A merge is the one operation in CodeFlow that writes a user's own
-- directory, and SQLite and a directory cannot be committed together. The
-- journal is what makes that gap survivable: every expectation the publisher
-- has about the target is committed *before* the first byte of the target
-- changes, so a process that dies mid-publish — or a publisher that has to give
-- up — can tell from SQL alone what was written, what was restored, and what to
-- do next (§19.3 item 4: "先提交 merge journal；文件发布在事务外逐项校验").
--
--   merge_operations  one publish attempt over one target root: its identity,
--                     its fencing epoch, and its seven-state lifecycle;
--   merge_files       one path of that attempt: the old/new content hashes, the
--                     backup blob, the permission bits, and the per-file
--                     publish/restore state.
--
-- What this migration deliberately does NOT do:
--   - it does not create or reference the blob store. Backups and new content
--     live in the content-addressed store of 008 (artifact.BlobStore) and are
--     written *before* the row that names them, so the order is always "blob
--     first, row second". A crash can therefore leave an unreferenced blob, but
--     never a journal row pointing at a missing blob — which is what §28 means
--     by "不能制造指向缺失 blob 的可见版本". Hash format is the same
--     ("sha256:" + 64 lowercase hex) that runworkspace manifests and 008's
--     artifact_versions already use, so manifest, diff and backup name one
--     family of hashes;
--   - it does not touch the merge candidate. merge.Prepare (T1.09.b group 2)
--     is a pure computation over three manifests and writes nothing; this
--     journal records what the publisher decided to do with one candidate, and
--     candidate_hash is the anchor that ties the two together (§27.5 item 2:
--     "candidate hash 是 Guard/check/Approval 的共同锚点");
--   - it does not implement crash recovery. Deciding, on start-up, what an
--     interrupted publish should become is T1.09.b group 4 (recover.go); this
--     migration only guarantees that everything group 4 needs is on disk —
--     which "still ours" means (old_hash/new_hash/state) and which directories
--     this operation created;
--   - it does not consult a Guard report or consume an Approval. T2.03 inserts
--     those checks before the first row here is written; the columns are
--     already sufficient (candidate_hash is the report's binding);
--   - it does not create a third table, does not add a Run state, and does not
--     touch runs.status. A merge that reaches applied writes merge.completed
--     through the event store (run.EventMergeCompleted), which requires a Run,
--     hence the NOT NULL run_id below.
--
-- Time is Unix milliseconds (UTC) in INTEGER columns, as in 001-009.
--
-- Amendment policy (plan §26.31, as recorded in 003, 006, 007, 008 and 009):
-- until T1.04 wires runstore into production start-up, the main agent may amend
-- a committed migration in place and record it in the plan; no database outside
-- of tests exists yet. From T1.04 on migrations are frozen: the runner fails
-- closed on a checksum mismatch (migrate.go verifyRecorded), so any further
-- change is a new, appended migration whose version is the next integer.

-- One publish attempt over one target root.
--
-- The row is created *before* any target file is touched and is the root lock:
-- the partial unique index below refuses a second non-terminal operation for a
-- root_key that already has one, which is both the cross-process mutual
-- exclusion §27.5 item 3 asks for and the durable "已有 needs_recovery 时拒绝新
-- merge" of §27.2 item 4 (needs_recovery is non-terminal, so its lock is never
-- released). Together with fence it gives the same shape the task claim already
-- uses (lease_epoch): a lock is a row, and the epoch is how a later holder can
-- tell "my write" from "the previous holder's".
CREATE TABLE merge_operations (
    id                   TEXT    PRIMARY KEY,
    project_id           TEXT    NOT NULL REFERENCES project_refs(project_id),
    -- A merge belongs to the Run whose result it publishes. NOT NULL on
    -- purpose, and exactly like 009's tool approvals: the operation ends by
    -- writing run.EventMergeCompleted, and run.RequiredIdentity("merge.completed")
    -- requires run_id, so an operation without a Run could never reach applied
    -- (§27.1). A manual merge outside any Run is not a thing this table can
    -- hold; T3.02's review flow, when it arrives, decides whether it needs one.
    run_id               TEXT    NOT NULL REFERENCES runs(id),
    -- "sha256:" + exactly 64 lowercase hex, the family of 008. The first CHECK
    -- below pins prefix and length for all four hash columns at once; lowercase
    -- is a Go-level check (artifact.ValidHash), because SQLite's LIKE is
    -- case-insensitive and a CHECK pretending otherwise would be a lie.
    --
    -- candidate_hash is the hash of the merge.Candidate this operation
    -- publishes. It is what a Guard report and an Approval are bound to, and
    -- the reason the identity trigger below freezes it: a candidate that
    -- changed after a human approved it must not be publishable under the old
    -- approval.
    candidate_hash       TEXT    NOT NULL,
    -- The three manifest hashes the candidate was computed from. base and
    -- target are what a later recovery or a reversing operation re-checks;
    -- result is kept so a reader can name the working copy the content came
    -- from without re-deriving it from the candidate.
    base_manifest_hash   TEXT    NOT NULL,
    target_manifest_hash TEXT    NOT NULL,
    result_manifest_hash TEXT    NOT NULL,
    -- The root lock key: workspace.RootIdentity.String() of the target root.
    -- It is an OS identity (volume + file id), not a path, so it is stable
    -- against a rename, distinct after a delete-and-recreate, and safe to log.
    -- Two different bindings that point at the same directory share one key,
    -- which is precisely the exclusion an external editor cannot see.
    root_key             TEXT    NOT NULL,
    -- The target directory path itself, as the publisher resolved it. The
    -- identity proves *which* directory a publish is about; a recovery with
    -- only the identity could not open it again, so the path is stored too,
    -- and the two are always checked against each other before a write
    -- (CaptureRootIdentity(target_root) must still equal root_key).
    target_root          TEXT    NOT NULL,
    -- Fencing epoch. Allocated as max(fence)+1 over this root_key inside the
    -- same transaction that inserts the row, so it is strictly increasing per
    -- root and never reused. The publisher re-reads (status, fence) before
    -- every single file write and stops the moment either moved: a holder that
    -- was taken over must not write one more byte (§27.5 item 4). The trigger
    -- below keeps it monotone: entries may move forward, never back.
    fence                INTEGER NOT NULL CHECK (fence >= 1),
    -- The seven states of §27.2 item 4. They are a closed set, and the two
    -- partial indexes below plus the terminal trigger depend on exactly this
    -- list: prepared/applying/rolling_back/needs_recovery hold the lock,
    -- applied/conflict/rolled_back release it.
    status               TEXT    NOT NULL CHECK (
        status IN ('prepared', 'applying', 'applied', 'conflict', 'rolling_back', 'rolled_back', 'needs_recovery')
    ),
    -- Why a terminal state was reached, when there is something to say
    -- (a conflict's reason, a rollback failure's reason). Free text like
    -- failure_code in 006: the closed vocabulary belongs to the Go sentinel
    -- errors, and pinning it here would mean migrating the CHECK every time a
    -- card adds a cause.
    failure_code         TEXT    NULL,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL,
    -- Monotonic revision, the CAS token of every status change. §27.2 item 5
    -- in its journal form: a publisher that lost the race reads the row again
    -- instead of overwriting the winner.
    revision             INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),

    -- An operation is created at a real instant and only ever moves later.
    CHECK (updated_at >= created_at),
    -- Every hash column is "sha256:%-71" (see the long note above). Written as
    -- one CHECK so a column added later cannot quietly skip the rule.
    CHECK (
        candidate_hash       LIKE 'sha256:%' AND length(candidate_hash)       = 71 AND
        base_manifest_hash   LIKE 'sha256:%' AND length(base_manifest_hash)   = 71 AND
        target_manifest_hash LIKE 'sha256:%' AND length(target_manifest_hash) = 71 AND
        result_manifest_hash LIKE 'sha256:%' AND length(result_manifest_hash) = 71
    ),
    -- The lock key and the path are both required: an empty root_key would make
    -- every operation of every root collide on one index entry, and an empty
    -- target_root would leave a recovery with nothing to open.
    CHECK (length(root_key) > 0 AND length(target_root) > 0)
);

-- The root lock, and the whole reason this table can replace a lock file.
--
-- A partial unique index over root_key, restricted to the four non-terminal
-- states, says exactly one thing: for any root there is at most one merge that
-- has been started and not finished. It is enforced by SQLite for every writer
-- in every process, so two goroutines, two connections or two server instances
-- that race for the same root cannot both proceed — the loser's INSERT fails
-- inside its own transaction and writes nothing. §27.5 item 3 ("取得…真实根锁")
-- and §27.2 item 4's needs_recovery case fall out of the same index:
-- needs_recovery is one of the four states, so a root whose last publish could
-- not be rolled back refuses new merges until a human resolves it.
--
-- A terminal state (applied/conflict/rolled_back) is not in the index, so the
-- next merge of the same root is allowed the moment the previous one finished;
-- the history rows stay and the fence keeps counting from the last one.
CREATE UNIQUE INDEX idx_merge_operations_root_lock
    ON merge_operations (root_key)
    WHERE status IN ('prepared', 'applying', 'rolling_back', 'needs_recovery');

-- "The history of one project's merges", newest first. The project filter comes
-- first because every read in the API layer is project-scoped (§27.3); id is
-- the tie-break so the order is total even for operations created in the same
-- millisecond.
CREATE INDEX idx_merge_operations_project_created
    ON merge_operations (project_id, created_at DESC, id);

-- "The merges of one Run", the read a Run snapshot or a cancel path makes.
CREATE INDEX idx_merge_operations_run ON merge_operations (run_id);

-- One path of one operation.
--
-- Every row is written in one transaction, before any target file is touched,
-- so the row set is a complete description of what the operation intends to do.
-- The publisher (group 3) walks it in seq order and the recovery (group 4)
-- reads it to decide, per path, what the target should hold now.
CREATE TABLE merge_files (
    operation_id        TEXT    NOT NULL REFERENCES merge_operations(id),
    -- Publish order, 1-based. It is the path order of the candidate and is
    -- deliberately not re-derived on read: renames (a delete plus a create) and
    -- nested creates must be applied in one specific order, and the order the
    -- publisher used is part of the record, not a function of the paths.
    seq                 INTEGER NOT NULL CHECK (seq >= 1),
    -- Relative to the target root, forward slashes, as in a manifest.
    path                TEXT    NOT NULL CHECK (length(path) > 0),
    kind                TEXT    NOT NULL CHECK (kind IN ('create', 'modify', 'delete')),
    -- The content the target must still hold when this row is applied, and the
    -- content it must hold afterwards. NULL where the kind has no such side
    -- (see the CHECK below). Both are manifest-family hashes.
    old_hash            TEXT    NULL,
    new_hash            TEXT    NULL,
    -- The content that was at `path` before the write, stored as a blob before
    -- this row was committed. It is the old content for modify and delete and
    -- NULL for create: a create has nothing to back up, and pretending it had
    -- one would make a rollback of a create try to restore content that never
    -- existed. This is what the rollback restores from, and it is always
    -- already in the blob store when the row exists (see the file header).
    backup_hash         TEXT    NULL,
    -- Permission bits of the two sides, as the manifests recorded them (the
    -- low bits of os.FileMode.Perm()). They are needed because a recovery can
    -- only read the journal: the candidate is long gone, and "restore the old
    -- permission" and "is this file still the one we wrote" are both questions
    -- about a number that has to be somewhere. NULL follows the kind rule of
    -- old_hash/new_hash. On Windows Go reports 0666/0777 for everything, so the
    -- value is recorded and applied on a best-effort basis there; see the
    -- publisher's platform note.
    old_mode            INTEGER NULL,
    new_mode            INTEGER NULL,
    -- Per-file publish state. It is written in the same transaction as the file
    -- system step it describes, so a crash leaves the row one state behind the
    -- disk at worst — which is what group 4 has to reconcile, and why the
    -- states are named after what happened to the *content* rather than after
    -- what the publisher was doing:
    --   pending        nothing written yet
    --   written        the target holds the new content (deleted, for a delete)
    --   restored       a rollback put the old content back
    --   external_edit  a rollback found the path holding something that is
    --                  neither the new nor the old content, and stopped
    state               TEXT    NOT NULL CHECK (state IN ('pending', 'written', 'restored', 'external_edit')),
    -- The directories this operation had to create before it could write this
    -- path, as a JSON array of relative paths, parents before children, and
    -- only those that did not exist when the journal was built. It is a
    -- write-ahead record: the directories are listed here *before* the
    -- publisher creates any of them, for the same reason the hashes are — a
    -- rollback (and group 4's recovery) must be able to remove exactly the
    -- directories this operation created and none that already existed.
    --
    -- Directories another row of the same operation also needs are recorded
    -- once, on the earliest row that needs them, so removing them deepest-first
    -- on rollback cannot hit a directory a later row still depends on. The
    -- default '[]' is the common case (a modify or delete of an existing path
    -- creates nothing).
    created_dirs_json   TEXT    NOT NULL DEFAULT '[]',
    updated_at          INTEGER NOT NULL,

    -- One row per path. The publisher does not need this to be true (the
    -- candidate has one operation per path), but a hand-written or replayed
    -- INSERT could otherwise give one path two expectations, and a recovery
    -- would have no reason to prefer either.
    PRIMARY KEY (operation_id, seq),
    UNIQUE (operation_id, path),

    -- The kind decides which columns exist, in one expression rather than three
    -- overlapping ones:
    --   create  nothing before (old_hash/old_mode/backup_hash NULL), new
    --           content and its mode required;
    --   modify  everything required — a modify with no old content could not
    --           check its precondition, and one with no backup could not be
    --           rolled back;
    --   delete  no new side (new_hash/new_mode NULL), old content and its
    --           backup required so the path can be put back.
    -- The mode columns follow the hash columns exactly, which is why they are
    -- in the same CHECK: a row with a hash but no mode (or the reverse) is not
    -- a state any publisher produces.
    CHECK (
        (kind = 'create'
             AND old_hash IS NULL AND old_mode IS NULL AND backup_hash IS NULL
             AND new_hash IS NOT NULL AND new_mode IS NOT NULL)
        OR (kind = 'modify'
             AND old_hash IS NOT NULL AND old_mode IS NOT NULL AND backup_hash IS NOT NULL
             AND new_hash IS NOT NULL AND new_mode IS NOT NULL)
        OR (kind = 'delete'
             AND old_hash IS NOT NULL AND old_mode IS NOT NULL AND backup_hash IS NOT NULL
             AND new_hash IS NULL AND new_mode IS NULL)
    ),
    -- Every hash column, when present, is in the family of 008 (prefix and
    -- length; lowercase is the Go check, as above).
    CHECK (
        (old_hash    IS NULL OR (old_hash    LIKE 'sha256:%' AND length(old_hash)    = 71)) AND
        (new_hash    IS NULL OR (new_hash    LIKE 'sha256:%' AND length(new_hash)    = 71)) AND
        (backup_hash IS NULL OR (backup_hash LIKE 'sha256:%' AND length(backup_hash) = 71))
    ),
    -- Modes are permission bits, so they fit in a byte on every platform Go
    -- supports; anything larger is not a mode. 511 is 0o777 — SQLite has no
    -- octal literal, so the bound is written in decimal.
    CHECK ((old_mode IS NULL OR (old_mode >= 0 AND old_mode <= 511))
       AND (new_mode IS NULL OR (new_mode >= 0 AND new_mode <= 511))),
    -- created_dirs_json must be a JSON array: the recovery parses it, and a
    -- malformed value would only be discovered by the process least able to
    -- cope with a surprise. json_type is available in the pinned engine
    -- (probed on modernc.org/sqlite 1.57.0: json_type('[]') = 'array',
    -- json_type('{"a":1}') = 'object', json_type('not json') raises), so the
    -- empty default and every real value can be checked exactly.
    CHECK (json_type(created_dirs_json) = 'array'),
    -- A path is relative: an absolute one or one with a parent segment could
    -- address a file outside target_root when joined to it, and this journal is
    -- data (a recovery replays it without the candidate to compare against).
    CHECK (path NOT LIKE '/%' AND path NOT LIKE '\%' AND path NOT LIKE '%/../%'
       AND path NOT LIKE '../%' AND path NOT LIKE '%/..' AND path <> '..' AND path <> '.')
);

-- "Every file of this operation, in publish order" is the primary key's own
-- order, so no extra index is needed for the publisher's walk. This one serves
-- the question a recovery asks first: which paths of this operation are in a
-- non-pending state, i.e. what might already be on disk.
CREATE INDEX idx_merge_files_state ON merge_files (operation_id, state);

-- A finished operation is finished. No statement may move a row out of applied,
-- conflict or rolled_back: §27.5 item 5 says "完整目标匹配后才能记 applied", and
-- applied is the fact that merge.completed was written; re-opening it would let
-- a second publish claim the same completion, and a rolled_back operation that
-- could be edited back into applying would contradict the directories that were
-- already restored. The store maps this message to merge.ErrOperationTerminal.
--
-- Only the terminal three are pinned. A needs_recovery row may still be moved
-- (by group 4's explicit reconciliation), and that is deliberate: needs_recovery
-- means "a human or a recoverer must decide", not "nothing may ever change".
CREATE TRIGGER trg_merge_operations_terminal_is_final
BEFORE UPDATE ON merge_operations
WHEN OLD.status IN ('applied', 'conflict', 'rolled_back')
BEGIN
    SELECT RAISE(ABORT, 'merge_operation_terminal_is_final');
END;

-- A journal is history: an operation row is never deleted, in any state. It is
-- the only record of what was done to a user's directory — which blobs were
-- written, which paths were touched, which directories were created — and
-- deleting it would erase the audit trail §27.5 item 5's recovery depends on.
-- The store maps this message to merge.ErrOperationIsHistory.
CREATE TRIGGER trg_merge_operations_no_delete
BEFORE DELETE ON merge_operations
BEGIN
    SELECT RAISE(ABORT, 'merge_operation_is_history');
END;

-- The identity of an operation is frozen from the moment it is created:
-- id, project, run, the root lock key, the target path, the four hashes and
-- created_at may never be rewritten. Each one is frozen for its own reason and
-- they are all the same rule — this row must keep naming the same publish:
--   * candidate_hash is the Guard/Approval anchor (§27.5 item 2): editing it
--     after a human approved the candidate would silently re-point an approval
--     at different content;
--   * root_key decides which root this operation locks and target_root decides
--     which directory it writes into — editing either would let an operation
--     holding one root's lock publish into another root;
--   * the manifest hashes and run_id are what group 4 re-checks and what
--     merge.completed is attributed to.
-- Assigning the same value is not a change, so the CAS updates below are
-- unaffected: they touch status/failure_code/revision/updated_at only, and
-- fence is covered by its own monotone trigger.
CREATE TRIGGER trg_merge_operations_identity_is_frozen
BEFORE UPDATE ON merge_operations
WHEN NEW.id <> OLD.id
  OR NEW.project_id <> OLD.project_id
  OR NEW.run_id <> OLD.run_id
  OR NEW.root_key <> OLD.root_key
  OR NEW.target_root <> OLD.target_root
  OR NEW.candidate_hash <> OLD.candidate_hash
  OR NEW.base_manifest_hash <> OLD.base_manifest_hash
  OR NEW.target_manifest_hash <> OLD.target_manifest_hash
  OR NEW.result_manifest_hash <> OLD.result_manifest_hash
  OR NEW.created_at <> OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'merge_operation_identity_is_frozen');
END;

-- The fence is a monotone epoch, never a settable field: a holder may be
-- replaced by one with a higher fence (that is what fencing means), but no
-- statement may lower it. A decreasing fence would let a superseded publisher
-- "prove" it is still the current holder with a value it read before it was
-- taken over, which is exactly the write §27.5 item 4 forbids.
CREATE TRIGGER trg_merge_operations_fence_monotone
BEFORE UPDATE ON merge_operations
WHEN NEW.fence < OLD.fence
BEGIN
    SELECT RAISE(ABORT, 'merge_operation_fence_decreased');
END;

-- The file list of a finished operation is closed: once the operation is
-- applied, conflict or rolled_back, no row may be inserted for it. A file row
-- created after the fact would describe a write that the completion event never
-- covered — for an applied operation, a path merge.completed did not count; for
-- a conflict, a file the operation promised it never touched (§27.5 item 4:
-- a conflict writes nothing).
CREATE TRIGGER trg_merge_files_no_insert_after_terminal
BEFORE INSERT ON merge_files
WHEN EXISTS (
    SELECT 1 FROM merge_operations o
    WHERE o.id = NEW.operation_id
      AND o.status IN ('applied', 'conflict', 'rolled_back')
)
BEGIN
    SELECT RAISE(ABORT, 'merge_operation_terminal_is_final');
END;

-- The same rule for an update: a finished operation's file rows are the record
-- of what it did, and rewriting one (say a written file back to pending) would
-- make the journal disagree with both the directory and the completion event.
CREATE TRIGGER trg_merge_files_no_update_after_terminal
BEFORE UPDATE ON merge_files
WHEN EXISTS (
    SELECT 1 FROM merge_operations o
    WHERE o.id = OLD.operation_id
      AND o.status IN ('applied', 'conflict', 'rolled_back')
)
BEGIN
    SELECT RAISE(ABORT, 'merge_operation_terminal_is_final');
END;

-- A file row is never deleted while its operation exists. The DELETE rule is
-- the mirror of the operation's own rule and it is stated separately because a
-- rolled-back operation is *still history*: the rows are how a reader learns
-- which paths were restored, and removing one would silently turn "this path
-- was written and put back" into "this path was never touched".
CREATE TRIGGER trg_merge_files_no_delete
BEFORE DELETE ON merge_files
BEGIN
    SELECT RAISE(ABORT, 'merge_file_is_history');
END;

-- `restored` means "the old content is back", which can only be true of a file
-- that was written first: the rollback walks the files it wrote, and a pending
-- file was never touched, so marking it restored would report a restore that
-- never happened (and, in group 4, would authorise the deletion of a directory
-- this operation does not own). The other three transitions are not pinned —
-- pending->written is the publish, pending->external_edit and
-- written->external_edit are the two ways a rollback gives up, and group 4 may
-- move a written row to restored while reconciling.
CREATE TRIGGER trg_merge_files_restored_requires_written
BEFORE UPDATE ON merge_files
WHEN NEW.state = 'restored' AND OLD.state <> 'written'
BEGIN
    SELECT RAISE(ABORT, 'merge_file_restore_without_write');
END;

-- "The run is in project_id" is NOT expressible as a composite foreign key
-- here, for the same reason 008 and 009 record: runs has no UNIQUE
-- (id, project_id) a composite key could target, and adding one would mean
-- altering 001's runs table (and its checksum) for a constraint this card does
-- not own. The journal helpers therefore check it in Go before writing:
-- merge.CreateOperationTx reads runs.project_id (runstore.GetRun) and refuses a
-- mismatch with merge.ErrRunProjectMismatch before any row is written.
--
-- The same reasoning applies to project_id itself: it is a plain FK, so it
-- proves the project_refs row exists, and "that row is the project the caller
-- meant" is not something a FK can state.
--
-- One further gap is recorded here so the next reader does not assume the
-- schema covers it: nothing verifies that the *content* hashes in
-- merge_files.old_hash/backup_hash describe the target at the moment the
-- journal is written. They cannot be — the target is a directory and SQL holds
-- no lock over it. That verification is the publisher's per-file precondition
-- (§27.5 item 4) and the only window it cannot close is between the check and
-- the write, which is why the file is replaced atomically rather than in place.
