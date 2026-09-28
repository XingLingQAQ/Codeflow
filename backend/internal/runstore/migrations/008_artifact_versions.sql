-- 008_artifact_versions.sql — immutable, content-addressed versions of an
-- artifact (T1.09.b, plan §19.1 and §27.8 "成果内容").
--
-- Scope: exactly one table, artifact_versions. It records which immutable blob
-- an artifact had at which version number, so a diff, a merge journal entry or
-- a backup can all name the same bytes by the same hash instead of copying a
-- path around. The hash format is the one runworkspace already emits
-- ("sha256:" + 64 lowercase hex over the file bytes), which is what makes
-- "manifest, diff and backup reference one hash family" true.
--
-- What this migration deliberately does NOT do:
--   - it does not create the blob store itself. Blobs are files under the
--     artifact root (internal/artifact.BlobStore), written before this row is
--     inserted: the visible version is the SQL row, and the order is always
--     "blob first, row second" so a crash can only ever leave an unreferenced
--     blob, never a version whose blob is missing (§27.8 "不能制造指向缺失 blob
--     的可见版本"). There is no foreign key to a file, and SQLite could not
--     express one anyway;
--   - it does not make content_hash globally unique. Two artifacts may hold the
--     same bytes (a copied file, a shared fixture), and §19.1 says so in as many
--     words: uniqueness is (artifact_id, version_no), which is the version
--     identity, not the content identity. A global UNIQUE would refuse the copy
--     that a user is entitled to make and would tempt a writer into sharing one
--     row between two artifacts, which would then delete both at once when one
--     retention policy fires;
--   - it does not add artifacts or artifact metadata. An artifact is still the
--     caller's identity; T3.02 adds review state and the Contract, and §27.8
--     forbids building a second artifact store. This table is the minimum a
--     merge journal and a diff need today;
--   - it does not add a delete/GC path. Retention and pinning are T12.02; a
--     version row is immutable history like events, so no UPDATE or DELETE
--     trigger is needed here beyond what the FK from a later table would use.
--     Deliberately no "delete_forbidden" trigger either: T12.02 must be able to
--     retire rows through an explicit migration without first dropping a guard,
--     and this table is not an append-only fact stream in the §27.4 sense;
--   - it does not constrain size beyond >= 0, and does not store the bytes.
--     The blob is the bytes; this row only pins the hash and the size so a
--     reader can detect a truncated or replaced blob without opening it.
--
-- Time is Unix milliseconds (UTC) in INTEGER columns, as in 001-007.
--
-- Amendment policy (plan §26.31, as recorded in 003 and 007): until T1.04
-- wires the run store into production start-up, the main agent may amend a
-- committed migration in place and records the amendment in plan §26. From
-- T1.04 on, migrations are frozen: the runner fails closed on a checksum
-- mismatch (migrate.go verifyRecorded), so any further change here is a new,
-- appended migration whose version is the next integer.

-- One immutable version of one artifact. The row is the *visible* fact: the
-- blob it names must already exist on disk when it is inserted, because the
-- store checks before it writes (artifact.CreateVersionTx refuses with
-- ErrBlobMissing and inserts nothing).
CREATE TABLE artifact_versions (
    id           TEXT    PRIMARY KEY,
    artifact_id  TEXT    NOT NULL,
    project_id   TEXT    NOT NULL REFERENCES project_refs(project_id),
    -- Per-artifact sequence, not a global one: version numbers are the
    -- artifact's own history and must not have holes left by other artifacts'
    -- writes. Allocated inside the writing transaction as max(version_no)+1 for
    -- this artifact_id, and protected by the UNIQUE constraint below, so two
    -- concurrent writers cannot both take the same number.
    version_no   INTEGER NOT NULL CHECK (version_no >= 1),
    -- "sha256:" + exactly 64 lowercase hex characters, the same form
    -- runworkspace.Entry.ContentHash uses. The CHECK pins the prefix and the
    -- length; lowercase is a store-level check (ValidHash), because SQLite's
    -- LIKE is case-insensitive and a CHECK that pretended otherwise would be a
    -- lie in the schema.
    content_hash TEXT    NOT NULL CHECK (
        content_hash LIKE 'sha256:%' AND length(content_hash) = 71
    ),
    -- Size of the blob in bytes: the same number BlobStore.Stat returns. It is
    -- stored so a reader can compare it against the file without reading the
    -- bytes, and so a truncated blob is detectable from SQL alone.
    size         INTEGER NOT NULL CHECK (size >= 0),
    -- Where the content lives. Always "blob:" + content_hash today, but it is
    -- a separate column because T12.01 migrates other domains into this same
    -- database and an existing artifact store may have to be addressed
    -- differently; a reader must treat it as the address, not re-derive it.
    content_ref  TEXT    NOT NULL,
    -- Actor identity (user, agent revision, system). Free text on purpose: the
    -- backlog has several actor kinds and the identity contract lives in the
    -- service layer, not in a CHECK this table would then have to be migrated
    -- to widen.
    created_by   TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    -- Optional context. stage_id is a legacy reference with no cross-file
    -- foreign key, exactly as in tasks; run_id/attempt_id point into this
    -- database but are nullable because a manual artifact is legitimate
    -- ("人工成果允许无 Run", §19.1) — it must not have to fake a Run to exist.
    stage_id     TEXT    NULL,
    -- A plain FK to runs(id)/attempts(id) gives the existence half of the
    -- check: a version cannot name a run this database has never seen. The
    -- project half is the store's job (see the note below the table).
    run_id       TEXT    NULL REFERENCES runs(id),
    attempt_id   TEXT    NULL REFERENCES attempts(id),
    -- A version may name the version it supersedes. The parent must be a
    -- version of the same artifact; the constraint here can only pin the
    -- identity (the column order makes (id, artifact_id) unusable as a target,
    -- so this is a plain FK to id) and the store checks the artifact match.
    parent_version_id TEXT NULL REFERENCES artifact_versions(id),
    -- The version identity. Two artifacts may hold the same content_hash, so
    -- this is deliberately not a unique index on content_hash (§19.1).
    UNIQUE (artifact_id, version_no)
);

-- The read behind a diff or a merge journal entry is always "the versions of
-- one artifact, ordered by version number". UNIQUE (artifact_id, version_no)
-- already provides that prefix; this composite index additionally serves the
-- project-scoped sweep T12.02's retention will need and makes the project
-- ownership check (project_id, artifact_id) a point lookup rather than a scan.
CREATE INDEX idx_artifact_versions_project_artifact
    ON artifact_versions (project_id, artifact_id);

-- "A run_id, when present, belongs to the same project" is NOT expressible as a
-- composite foreign key here. The pattern 001 used for runs -> tasks needs a
-- UNIQUE (id, project_id) on the parent, and runs does not have one; adding it
-- would mean altering 001's runs table (and its checksum) for a constraint this
-- card does not own. The store therefore checks it in Go:
-- artifact.CreateVersionTx queries the run and refuses a version whose run
-- belongs to another project with ErrArtifactProjectMismatch, before any row is
-- written. A hand-written INSERT naming a foreign project's run is not caught
-- by the database; that gap is recorded here so the next reader does not assume
-- the FK covers it.
-- attempt_id is checked the same way when run_id is given: the attempt must
-- exist and belong to that run. An attempt without a run_id is refused by the
-- store, because an attempt is meaningless without the run it belongs to.
--
-- (No trigger table copy needed: nothing here can be rewritten, and the store
-- exposes no UPDATE path. A later card that wants to freeze these rows adds its
-- own trigger in its own migration.)
