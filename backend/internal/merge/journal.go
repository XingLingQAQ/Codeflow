// Merge journal (T1.09.b group 3): the persistent record of a publish, in
// codeflow.db, written through runstore's transaction machinery.
//
// This file is the SQL half of the publisher. It owns the types, the closed
// state sets and the statements; it never opens a transaction, never touches
// the file system, and never decides anything on its own — publish.go drives it
// and owns the ordering. That split is the same one runstore itself follows
// (every write takes a runstore.Tx), and it is what lets a test commit or roll
// back a publish step without a file system.
//
// Schema: migrations/010_merge_journal.sql. Read that file for the reasoning
// behind each column, CHECK and trigger; this file only states the Go-side
// contract they support:
//
//   - the root lock is the partial unique index idx_merge_operations_root_lock
//     (at most one non-terminal operation per root_key). A loser's INSERT fails
//     and its transaction is rolled back, so it writes nothing at all;
//   - the fence is allocated as max(fence)+1 for the root inside that same
//     transaction, and only ever grows;
//   - every status change is a CAS on (id, revision) and, where it matters,
//     on the state the caller expected — a change by somebody else is
//     ErrOperationMoved, never an overwrite (§27.2 item 5);
//   - a terminal operation is frozen by trigger, and its file rows with it.
//
// Dependency direction: this package may import runstore, run and workspace; it
// must not import artifact or approval (the blob store is an interface here,
// and the Guard/Approval binding is T2.03).
package merge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/runstore"
)

// MergeOperation states (§27.2 item 4, spelled out as constants so the publisher,
// the recovery and their tests never match a literal).
const (
	// StatusPrepared is the state a freshly created operation is in: the row
	// exists, the root lock is held, and nothing has been written yet. It is
	// the state a crash between "the lock is taken" and "the file list is
	// committed" leaves behind, which is why it counts as blocking.
	StatusPrepared = "prepared"
	// StatusApplying means the file list is committed and the publisher is
	// writing the target.
	StatusApplying = "applying"
	// StatusApplied is terminal: every path matches the candidate and
	// merge.completed was written in the same transaction.
	StatusApplied = "applied"
	// StatusConflict is terminal: the candidate could not be published and
	// nothing was written (the target had moved on since Prepare).
	StatusConflict = "conflict"
	// StatusRollingBack means the publisher is undoing the writes it made.
	StatusRollingBack = "rolling_back"
	// StatusRolledBack is terminal: the target is back to what it was, and the
	// root lock is released.
	StatusRolledBack = "rolled_back"
	// StatusNeedsRecovery means the rollback could not finish (a path was
	// edited by somebody else, or the process died) and a human or group 4's
	// recoverer must decide. It is NOT terminal and it does NOT release the
	// root lock: publishing over a half-rolled-back tree is precisely what
	// §27.5 item 5 forbids.
	StatusNeedsRecovery = "needs_recovery"
)

// OperationStatuses lists every valid state, in declaration order. It mirrors
// the migration's CHECK, and a test asserts the two sets are equal.
var OperationStatuses = []string{
	StatusPrepared, StatusApplying, StatusApplied, StatusConflict,
	StatusRollingBack, StatusRolledBack, StatusNeedsRecovery,
}

// validOperationStatus reports whether s is one of the seven states.
func validOperationStatus(s string) bool {
	for _, v := range OperationStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// blockingStatuses are the states that hold the root lock, mirroring the WHERE
// clause of idx_merge_operations_root_lock. They are also what ListBlocking
// reports, so a caller can name the operation that is in its way.
var blockingStatuses = []string{StatusPrepared, StatusApplying, StatusRollingBack, StatusNeedsRecovery}

// Terminal reports whether a state can never change again. A terminal operation
// is frozen by trigger and does not hold the root lock.
func Terminal(status string) bool {
	switch status {
	case StatusApplied, StatusConflict, StatusRolledBack:
		return true
	default:
		return false
	}
}

// Per-file publish states.
const (
	// FileStatePending: nothing has been written for this path.
	FileStatePending = "pending"
	// FileStateWritten: the target holds the new content (or, for a delete, holds
	// nothing at that path).
	FileStateWritten = "written"
	// FileStateRestored: a rollback put the old content back (or, for a create,
	// removed the file this operation added).
	FileStateRestored = "restored"
	// FileStateExternalEdit: a rollback found content that is neither the new nor
	// the old content and stopped without touching the path.
	FileStateExternalEdit = "external_edit"
)

// FileStateValues lists every valid per-file state, in declaration order.
var FileStateValues = []string{FileStatePending, FileStateWritten, FileStateRestored, FileStateExternalEdit}

// Sentinels of the journal. They are returned by this file and by Publish, and
// every one of them means "no further write happened": the caller can treat
// them as the reason a publish stopped, not as a partial success.
var (
	// ErrRootBusy means another non-terminal operation already holds the root
	// lock — including a needs_recovery operation, which never releases it.
	// The error names the occupying operation.
	ErrRootBusy = errors.New("merge: the target root already has an unfinished merge")

	// ErrOperationMoved means a CAS failed: the operation's revision, status or
	// fence is no longer what the caller read. Somebody else — a recoverer, a
	// second publisher — has taken it over, and the caller must stop.
	ErrOperationMoved = errors.New("merge: the merge operation moved under the caller")

	// ErrOperationNotFound means no operation row has the requested id.
	ErrOperationNotFound = errors.New("merge: no such merge operation")

	// ErrOperationTerminal means a write was attempted on an applied, conflict
	// or rolled_back operation. The database refuses it with a trigger as well;
	// this sentinel is the Go side of the same rule.
	ErrOperationTerminal = errors.New("merge: the merge operation is in a terminal state")

	// ErrOperationIsHistory means a DELETE was attempted. The journal is never
	// deleted, in any state.
	ErrOperationIsHistory = errors.New("merge: the merge journal is history")

	// ErrRunProjectMismatch means the run the operation belongs to is not the
	// run of the project the caller named. The journal checks it in Go because
	// runs has no composite key a FK could use (see the migration's note).
	ErrRunProjectMismatch = errors.New("merge: the run does not belong to the project")

	// ErrInvalidJournalInput means the caller handed the journal something it
	// cannot store: an empty id, a path that is not a safe relative path, a
	// kind/hash combination the CHECK would refuse, a malformed directory list.
	// It is refused before any SQL runs.
	ErrInvalidJournalInput = errors.New("merge: invalid merge journal input")
)

// Operations are rows of merge_operations. Time is stored as Unix milliseconds
// (UTC) in INTEGER columns, as everywhere in runstore, and converted here.
type MergeOperation struct {
	ID        string
	ProjectID string
	RunID     string
	// CandidateHash is the hash of the merge.Candidate this operation
	// publishes; it is the Guard/Approval anchor and is frozen after insert.
	CandidateHash string
	// BaseManifestHash, TargetManifestHash and ResultManifestHash are the three
	// captures the candidate was computed from.
	BaseManifestHash   string
	TargetManifestHash string
	ResultManifestHash string
	// RootKey is workspace.RootIdentity.String() of the target root: the root
	// lock key, and stable across a rename.
	RootKey string
	// TargetRoot is the directory the operations apply to.
	TargetRoot string
	// Fence is the monotone epoch of this operation within its root.
	Fence int64
	// Status is one of the Status* constants.
	Status string
	// FailureCode explains a terminal or needs_recovery state when there is
	// something to say; nil otherwise.
	FailureCode *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Revision    int64
}

// MergeFile is one row of merge_files: everything the publisher and the recovery
// need to know about one path without the candidate in hand.
//
// OldHash/OldMode are nil for a create and NewHash/NewMode nil for a delete, so
// the struct is a faithful image of the row; the CHECK in the migration is what
// guarantees the combination is one of the three legal ones.
type MergeFile struct {
	OperationID string
	// Seq is the 1-based publish order.
	Seq  int64
	Path string
	// Kind is KindCreate, KindModify or KindDelete (candidate.go).
	Kind string
	// OldHash is the content the target must still hold (nil for create).
	OldHash *string
	// NewHash is the content the target gets (nil for delete).
	NewHash *string
	// BackupHash is the old content, stored as a blob before this row was
	// committed (nil for create).
	BackupHash *string
	// OldMode and NewMode are the permission bits of the two sides, as the
	// manifests recorded them (nil where the corresponding hash is nil).
	OldMode *uint32
	NewMode *uint32
	// State is one of the MergeFile* constants.
	State string
	// CreatedDirs are the directories, relative to the target root, that the
	// publisher had to create before it could write this path — parents first,
	// and only those that did not exist when the file list was built. They are
	// recorded before they are created (see the migration).
	CreatedDirs []string
	UpdatedAt   time.Time
}

// CreateOperationInput is what a caller must supply to take the root lock.
//
// There is deliberately no fence field: the fence is allocated by the database
// from the root's history, so a caller cannot claim an epoch.
type CreateOperationInput struct {
	ID                 string
	ProjectID          string
	RunID              string
	CandidateHash      string
	BaseManifestHash   string
	TargetManifestHash string
	ResultManifestHash string
	RootKey            string
	TargetRoot         string
	// Now is the instant the operation is created. Required.
	Now time.Time
}

// CreateOperationTx inserts one merge operation in prepared state, which is how
// the root lock is taken (§27.2 item 4, §27.5 item 3).
//
// The insert, the fence allocation and the root-lock check are one statement
// plus one read, all inside the caller's transaction:
//
//  1. the run is read (runstore.GetRun) and its project compared with
//     ProjectID — the journal will not attribute a merge to another project's
//     run;
//  2. fence is max(fence)+1 over root_key (1 when the root has no history).
//     Two operations of the same root cannot both compute it: the partial
//     unique index refuses the second INSERT, and the loser's transaction is
//     rolled back by its caller, so no fence gap is left behind either;
//  3. the row is inserted. A unique-index violation on the root lock is
//     reported as ErrRootBusy, naming the occupying operation, and nothing is
//     written — which is what §27.2 item 4 means by "已有 needs_recovery 时拒绝
//     新 merge" (that state is in the index and never leaves it).
//
// The returned struct is the stored row, so the caller's fence is the one the
// database allocated rather than one it hoped for.
func CreateOperationTx(ctx context.Context, tx runstore.Tx, in CreateOperationInput) (MergeOperation, error) {
	if err := validateCreateOperation(in); err != nil {
		return MergeOperation{}, err
	}

	r, err := runstore.GetRun(ctx, tx, in.RunID)
	if err != nil {
		return MergeOperation{}, fmt.Errorf("merge: read run %s: %w", in.RunID, err)
	}
	if r.ProjectID != in.ProjectID {
		return MergeOperation{}, fmt.Errorf("%w: run %s belongs to project %s, not %s",
			ErrRunProjectMismatch, in.RunID, r.ProjectID, in.ProjectID)
	}

	fence, err := nextFence(ctx, tx, in.RootKey)
	if err != nil {
		return MergeOperation{}, err
	}

	now := unixMilli(in.Now)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO merge_operations (
			id, project_id, run_id, candidate_hash, base_manifest_hash,
			target_manifest_hash, result_manifest_hash, root_key, target_root,
			fence, status, failure_code, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'prepared', NULL, ?, ?, 1)`,
		in.ID, in.ProjectID, in.RunID, in.CandidateHash, in.BaseManifestHash,
		in.TargetManifestHash, in.ResultManifestHash, in.RootKey, in.TargetRoot,
		fence, now, now,
	); err != nil {
		if busy, occupant, mapped := mapRootLockError(ctx, tx, in.RootKey, err); mapped {
			if busy {
				return MergeOperation{}, fmt.Errorf("%w: operation %s holds root %s",
					ErrRootBusy, occupant, in.RootKey)
			}
		}
		return MergeOperation{}, fmt.Errorf("merge: create operation %s: %w", in.ID, err)
	}

	return MergeOperation{
		ID:                 in.ID,
		ProjectID:          in.ProjectID,
		RunID:              in.RunID,
		CandidateHash:      in.CandidateHash,
		BaseManifestHash:   in.BaseManifestHash,
		TargetManifestHash: in.TargetManifestHash,
		ResultManifestHash: in.ResultManifestHash,
		RootKey:            in.RootKey,
		TargetRoot:         in.TargetRoot,
		Fence:              fence,
		Status:             StatusPrepared,
		CreatedAt:          fromUnixMilli(now),
		UpdatedAt:          fromUnixMilli(now),
		Revision:           1,
	}, nil
}

// nextFence reads the root's current high-water mark and returns the next
// epoch. A root with no history starts at 1.
//
// The read is not "the same statement" as the insert on purpose: under the
// BEGIN IMMEDIATE transactions runstore pins, the caller's transaction holds
// the write lock from before this read, so nothing can insert between the two.
// A root that had a finished operation yesterday continues from that number
// rather than restarting, which is what makes the epoch monotone per root
// across operations.
func nextFence(ctx context.Context, tx runstore.Tx, rootKey string) (int64, error) {
	var maxFence sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT max(fence) FROM merge_operations WHERE root_key = ?`, rootKey,
	).Scan(&maxFence); err != nil {
		return 0, fmt.Errorf("merge: read fence for root %s: %w", rootKey, err)
	}
	if !maxFence.Valid {
		return 1, nil
	}
	return maxFence.Int64 + 1, nil
}

// mapRootLockError turns a failed CREATE into "this root is busy" when — and
// only when — a blocking operation really is present. It never guesses from the
// error text alone: an insert can fail for a dozen reasons, and reporting one of
// them as "busy" would tell a caller to wait for an operation that does not
// exist.
func mapRootLockError(ctx context.Context, tx runstore.Tx, rootKey string, err error) (busy bool, occupant string, mapped bool) {
	if err == nil {
		return false, "", false
	}
	blocking, blocker, lookupErr := BlockingOperation(ctx, tx, rootKey)
	if lookupErr != nil || !blocking {
		return false, "", false
	}
	return true, blocker, true
}

// BlockingOperation reports whether rootKey currently has an operation that
// holds the root lock, and which one. It is the read behind ErrRootBusy's
// message and behind the same check made before a publish starts.
func BlockingOperation(ctx context.Context, q runstore.Querier, rootKey string) (bool, string, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id FROM merge_operations
		WHERE root_key = ?
		  AND status IN ('prepared', 'applying', 'rolling_back', 'needs_recovery')
		ORDER BY created_at, id
		LIMIT 1`, rootKey)
	var id string
	err := row.Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, "", nil
	case err != nil:
		return false, "", fmt.Errorf("merge: read root lock for %s: %w", rootKey, err)
	default:
		return true, id, nil
	}
}

// ListNonTerminalOperations lists every operation that still holds a root lock,
// oldest first ("created_at, id", the same total order BlockingOperation uses).
//
// It is the read a process makes on start-up to find every interrupted publish
// (T1.09.b group 4 hands one of these to Recover per root; T1.04 wires that
// call). Only the four blocking states are returned — a terminal operation is
// history and holds nothing — and the set is taken from blockingStatuses
// rather than spelled again here, so it cannot drift from the partial unique
// index the lock actually is.
func ListNonTerminalOperations(ctx context.Context, q runstore.Querier) ([]MergeOperation, error) {
	args := make([]any, 0, len(blockingStatuses))
	for _, s := range blockingStatuses {
		args = append(args, s)
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id, project_id, run_id, candidate_hash, base_manifest_hash,
		       target_manifest_hash, result_manifest_hash, root_key, target_root,
		       fence, status, failure_code, created_at, updated_at, revision
		FROM merge_operations
		WHERE status IN (?, ?, ?, ?)
		ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("merge: list unfinished operations: %w", err)
	}
	defer rows.Close()

	var out []MergeOperation
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			return nil, fmt.Errorf("merge: scan unfinished operation: %w", err)
		}
		out = append(out, op)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("merge: list unfinished operations: %w", err)
	}
	return out, nil
}

// TakeOverOperationTx moves an operation into the recoverer's hands: fence + 1,
// revision + 1, and the taker's name in failure_code, all as one CAS on the
// row the caller read.
//
// This is the fencing half of §27.5 item 6 ("先 fencing 禁止状态发布"): the
// previous holder — a publisher still looping, a crashed process's successor —
// re-reads (status, fence) before every single file and stops the moment either
// moved, so after this statement commits the old holder cannot write one more
// byte. The fence is the epoch that makes "my write" and "the previous holder's
// write" distinguishable even though both rows have the same id.
//
// The taker is recorded in failure_code because the frozen 010 schema has no
// owner column and adding one would mean amending a migration group 4 does not
// own. The value is "recovery_takeover:<taker>"; the real failure code replaces
// it when the recovery concludes, so it survives only if the recoverer itself
// dies — which is exactly when a reader wants to know who held the row.
//
// A terminal operation is refused before any SQL runs (the trigger would refuse
// it too, but the sentinel is the useful answer). Any other state may be taken
// over, including needs_recovery: that state is a deliberate "a human or a
// recoverer must decide", not a frozen one.
func TakeOverOperationTx(ctx context.Context, tx runstore.Tx, op MergeOperation, taker string, now time.Time) (MergeOperation, error) {
	if strings.TrimSpace(taker) == "" {
		return MergeOperation{}, fmt.Errorf("%w: a recoverer identity is required to take over %s", ErrInvalidJournalInput, op.ID)
	}
	if Terminal(op.Status) {
		return MergeOperation{}, fmt.Errorf("%w: operation %s is %s", ErrOperationTerminal, op.ID, op.Status)
	}
	code := takeoverCodePrefix + taker
	res, err := tx.ExecContext(ctx, `
		UPDATE merge_operations
		SET fence = fence + 1, revision = revision + 1, failure_code = ?, updated_at = ?
		WHERE id = ? AND revision = ? AND status = ? AND fence = ?`,
		code, unixMilli(now), op.ID, op.Revision, op.Status, op.Fence,
	)
	if err != nil {
		return MergeOperation{}, mapOperationWriteError("take over operation "+op.ID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return MergeOperation{}, fmt.Errorf("merge: take over operation %s: %w", op.ID, err)
	}
	if affected == 0 {
		return MergeOperation{}, fmt.Errorf("%w: operation %s was expected at revision %d in state %s with fence %d",
			ErrOperationMoved, op.ID, op.Revision, op.Status, op.Fence)
	}

	updated := op
	updated.Fence++
	updated.Revision++
	updated.FailureCode = &code
	updated.UpdatedAt = fromUnixMilli(unixMilli(now))
	return updated, nil
}

// takeoverCodePrefix marks the failure_code TakeOverOperationTx writes. It is a
// prefix, not an enum value: the whole point is to name the taker.
const takeoverCodePrefix = "recovery_takeover:"

// SetOperationFailureCodeCAS changes only failure_code, as the same CAS tuple
// the status transitions use (id, revision, status, fence).
//
// It exists for the one move a status CAS cannot express: keeping a
// needs_recovery operation in needs_recovery while recording why a retry could
// not finish (UpdateOperationStatusCAS refuses expected == next on purpose —
// "no change" is not a transition). The revision still bumps, so a racing
// writer loses exactly as it would to any other write.
func SetOperationFailureCodeCAS(ctx context.Context, tx runstore.Tx, op MergeOperation, failureCode string, now time.Time) (MergeOperation, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE merge_operations
		SET failure_code = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ? AND status = ? AND fence = ?`,
		failureCode, unixMilli(now), op.ID, op.Revision, op.Status, op.Fence,
	)
	if err != nil {
		return MergeOperation{}, mapOperationWriteError("record failure code of "+op.ID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return MergeOperation{}, fmt.Errorf("merge: record failure code of %s: %w", op.ID, err)
	}
	if affected == 0 {
		return MergeOperation{}, fmt.Errorf("%w: operation %s was expected at revision %d in state %s with fence %d",
			ErrOperationMoved, op.ID, op.Revision, op.Status, op.Fence)
	}
	updated := op
	updated.FailureCode = &failureCode
	updated.Revision++
	updated.UpdatedAt = fromUnixMilli(unixMilli(now))
	return updated, nil
}

// WriteFilesTx writes the operation's file list and moves the operation from
// prepared to applying, in one transaction.
//
// This is the "先提交 merge journal" step of §19.3 item 4: after it commits,
// every path the publisher may touch, every old/new hash it will check against,
// and every backup it may need are already durable — and every backup blob is
// already in the blob store, because the caller streams them before it calls
// this function. Nothing here touches the target.
//
// The state change is a CAS on (id, revision, status = prepared): a caller that
// lost the operation to a recoverer gets ErrOperationMoved and writes no file
// rows, rather than adding a file list to an operation somebody else is
// driving.
func WriteFilesTx(ctx context.Context, tx runstore.Tx, op MergeOperation, files []MergeFile, now time.Time) (MergeOperation, error) {
	if op.ID == "" {
		return MergeOperation{}, fmt.Errorf("%w: operation id is required", ErrInvalidJournalInput)
	}
	if len(files) == 0 {
		return MergeOperation{}, fmt.Errorf("%w: an operation with no files is not writable", ErrInvalidJournalInput)
	}
	for i := range files {
		if err := validateFile(op.ID, files[i], int64(i+1)); err != nil {
			return MergeOperation{}, err
		}
	}

	nowMS := unixMilli(now)
	for _, f := range files {
		dirs, err := encodeCreatedDirs(f.CreatedDirs)
		if err != nil {
			return MergeOperation{}, fmt.Errorf("%w: %s: %v", ErrInvalidJournalInput, f.Path, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO merge_files (
				operation_id, seq, path, kind, old_hash, new_hash, backup_hash,
				old_mode, new_mode, state, created_dirs_json, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
			op.ID, f.Seq, f.Path, f.Kind,
			nullableString(f.OldHash), nullableString(f.NewHash), nullableString(f.BackupHash),
			nullableUint32(f.OldMode), nullableUint32(f.NewMode), dirs, nowMS,
		); err != nil {
			return MergeOperation{}, mapFileWriteError("insert merge file "+f.Path, err)
		}
	}

	return UpdateOperationStatusCAS(ctx, tx, op, StatusPrepared, StatusApplying, nil, now)
}

// UpdateOperationStatusCAS moves an operation to next, provided the stored row
// is still exactly what the caller holds: same revision, same status (expected)
// and same fence.
//
// The fence is part of the CAS because it is what a superseded holder must be
// stopped by: a recoverer that takes over a root bumps the fence, and every
// later write by the previous holder must fail here rather than land on a row
// it no longer owns. The revision is bumped by this statement itself, so two
// racing callers cannot both succeed.
func UpdateOperationStatusCAS(ctx context.Context, tx runstore.Tx, op MergeOperation, expected, next string, failureCode *string, now time.Time) (MergeOperation, error) {
	if !validOperationStatus(next) {
		return MergeOperation{}, fmt.Errorf("%w: %q is not a merge operation state", ErrInvalidJournalInput, next)
	}
	if expected == next {
		return MergeOperation{}, fmt.Errorf("%w: %q to %q is not a change", ErrInvalidJournalInput, expected, next)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE merge_operations
		SET status = ?, failure_code = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ? AND status = ? AND fence = ?`,
		next, nullableString(failureCode), unixMilli(now),
		op.ID, op.Revision, expected, op.Fence,
	)
	if err != nil {
		return MergeOperation{}, mapOperationWriteError("update operation "+op.ID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return MergeOperation{}, fmt.Errorf("merge: update operation %s: %w", op.ID, err)
	}
	if affected == 0 {
		return MergeOperation{}, fmt.Errorf("%w: operation %s was expected at revision %d in state %s with fence %d",
			ErrOperationMoved, op.ID, op.Revision, expected, op.Fence)
	}

	updated := op
	updated.Status = next
	updated.FailureCode = failureCode
	updated.Revision++
	updated.UpdatedAt = fromUnixMilli(unixMilli(now))
	return updated, nil
}

// MarkFileTx records what happened to one path, as a CAS on the file row's own
// state.
//
// expectedState is the state the caller read (pending before the write,
// written before a restore), so a double write cannot be recorded twice and a
// rollback cannot claim to have restored a file that was never written. The
// operation is not touched: its state is moved once, by the caller, around the
// whole loop.
func MarkFileTx(ctx context.Context, tx runstore.Tx, operationID string, seq int64, expectedState, nextState string, now time.Time) error {
	if !validFileState(nextState) {
		return fmt.Errorf("%w: %q is not a merge file state", ErrInvalidJournalInput, nextState)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE merge_files
		SET state = ?, updated_at = ?
		WHERE operation_id = ? AND seq = ? AND state = ?`,
		nextState, unixMilli(now), operationID, seq, expectedState,
	)
	if err != nil {
		return mapFileWriteError(fmt.Sprintf("mark file %s#%d", operationID, seq), err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("merge: mark file %s#%d: %w", operationID, seq, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: file %s#%d was expected in state %s",
			ErrOperationMoved, operationID, seq, expectedState)
	}
	return nil
}

// GetOperation reads one operation row.
func GetOperation(ctx context.Context, q runstore.Querier, id string) (MergeOperation, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, project_id, run_id, candidate_hash, base_manifest_hash,
		       target_manifest_hash, result_manifest_hash, root_key, target_root,
		       fence, status, failure_code, created_at, updated_at, revision
		FROM merge_operations WHERE id = ?`, id)
	op, err := scanOperation(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return MergeOperation{}, fmt.Errorf("%w: %s", ErrOperationNotFound, id)
	case err != nil:
		return MergeOperation{}, fmt.Errorf("merge: read operation %s: %w", id, err)
	default:
		return op, nil
	}
}

// ListFiles reads the file list of one operation in publish order.
func ListFiles(ctx context.Context, q runstore.Querier, operationID string) ([]MergeFile, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT operation_id, seq, path, kind, old_hash, new_hash, backup_hash,
		       old_mode, new_mode, state, created_dirs_json, updated_at
		FROM merge_files WHERE operation_id = ? ORDER BY seq`, operationID)
	if err != nil {
		return nil, fmt.Errorf("merge: list files of %s: %w", operationID, err)
	}
	defer rows.Close()

	var out []MergeFile
	for rows.Next() {
		var (
			f          MergeFile
			oldHash    sql.NullString
			newHash    sql.NullString
			backupHash sql.NullString
			oldMode    sql.NullInt64
			newMode    sql.NullInt64
			dirsJSON   string
			updatedAt  int64
		)
		if err := rows.Scan(&f.OperationID, &f.Seq, &f.Path, &f.Kind,
			&oldHash, &newHash, &backupHash, &oldMode, &newMode, &f.State,
			&dirsJSON, &updatedAt); err != nil {
			return nil, fmt.Errorf("merge: scan file of %s: %w", operationID, err)
		}
		f.OldHash = fromNullString(oldHash)
		f.NewHash = fromNullString(newHash)
		f.BackupHash = fromNullString(backupHash)
		f.OldMode = fromNullUint32(oldMode)
		f.NewMode = fromNullUint32(newMode)
		f.UpdatedAt = fromUnixMilli(updatedAt)
		if err := decodeCreatedDirs(dirsJSON, &f); err != nil {
			return nil, fmt.Errorf("merge: file %s#%d: %w", operationID, f.Seq, err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("merge: list files of %s: %w", operationID, err)
	}
	return out, nil
}

// --- validation ---

func validateCreateOperation(in CreateOperationInput) error {
	switch {
	case strings.TrimSpace(in.ID) == "":
		return fmt.Errorf("%w: operation id is required", ErrInvalidJournalInput)
	case strings.TrimSpace(in.ProjectID) == "":
		return fmt.Errorf("%w: project id is required", ErrInvalidJournalInput)
	case strings.TrimSpace(in.RunID) == "":
		return fmt.Errorf("%w: run id is required (merge.completed is a Run event)", ErrInvalidJournalInput)
	case strings.TrimSpace(in.RootKey) == "":
		return fmt.Errorf("%w: root key is required", ErrInvalidJournalInput)
	case strings.TrimSpace(in.TargetRoot) == "":
		return fmt.Errorf("%w: target root is required", ErrInvalidJournalInput)
	case in.Now.IsZero():
		return fmt.Errorf("%w: Now is required", ErrInvalidJournalInput)
	}
	for _, h := range []struct{ name, value string }{
		{"candidate_hash", in.CandidateHash},
		{"base_manifest_hash", in.BaseManifestHash},
		{"target_manifest_hash", in.TargetManifestHash},
		{"result_manifest_hash", in.ResultManifestHash},
	} {
		if !validHash(h.value) {
			return fmt.Errorf("%w: %s %q is not a sha256 content hash", ErrInvalidJournalInput, h.name, h.value)
		}
	}
	return nil
}

// validateFile re-checks the kind/hash/mode combination the migration's CHECK
// expresses, so a caller gets a named field and no SQL runs, and checks that
// seq is the position the caller is writing.
func validateFile(operationID string, f MergeFile, wantSeq int64) error {
	if f.Seq != wantSeq {
		return fmt.Errorf("%w: file %s has seq %d where %d was expected (seq is the publish order, from 1)",
			ErrInvalidJournalInput, f.Path, f.Seq, wantSeq)
	}
	if !safeRelativePath(f.Path) {
		return fmt.Errorf("%w: %q is not a safe relative path", ErrInvalidJournalInput, f.Path)
	}
	if err := validateFileHashes(operationID, f); err != nil {
		return err
	}
	if err := validateFileModes(f); err != nil {
		return err
	}
	for _, dir := range f.CreatedDirs {
		if !safeRelativePath(dir) {
			return fmt.Errorf("%w: created directory %q is not a safe relative path", ErrInvalidJournalInput, dir)
		}
	}
	return nil
}

func validateFileHashes(operationID string, f MergeFile) error {
	requireHash := func(name string, v *string) error {
		if v == nil || !validHash(*v) {
			return fmt.Errorf("%w: file %s of %s needs %s", ErrInvalidJournalInput, f.Path, operationID, name)
		}
		return nil
	}
	forbidHash := func(name string, v *string) error {
		if v != nil {
			return fmt.Errorf("%w: file %s of %s must not carry %s", ErrInvalidJournalInput, f.Path, operationID, name)
		}
		return nil
	}
	switch f.Kind {
	case KindCreate:
		if err := forbidHash("old_hash", f.OldHash); err != nil {
			return err
		}
		if err := forbidHash("backup_hash", f.BackupHash); err != nil {
			return err
		}
		return requireHash("new_hash", f.NewHash)
	case KindModify:
		for _, h := range []struct {
			name string
			v    *string
		}{{"old_hash", f.OldHash}, {"backup_hash", f.BackupHash}, {"new_hash", f.NewHash}} {
			if err := requireHash(h.name, h.v); err != nil {
				return err
			}
		}
		return nil
	case KindDelete:
		if err := forbidHash("new_hash", f.NewHash); err != nil {
			return err
		}
		for _, h := range []struct {
			name string
			v    *string
		}{{"old_hash", f.OldHash}, {"backup_hash", f.BackupHash}} {
			if err := requireHash(h.name, h.v); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: file %s has kind %q", ErrInvalidJournalInput, f.Path, f.Kind)
	}
}

func validateFileModes(f MergeFile) error {
	present := func(v *uint32) bool { return v != nil }
	switch f.Kind {
	case KindCreate:
		if present(f.OldMode) || !present(f.NewMode) {
			return fmt.Errorf("%w: create %s must carry only new_mode", ErrInvalidJournalInput, f.Path)
		}
	case KindModify:
		if !present(f.OldMode) || !present(f.NewMode) {
			return fmt.Errorf("%w: modify %s must carry both modes", ErrInvalidJournalInput, f.Path)
		}
	case KindDelete:
		if !present(f.OldMode) || present(f.NewMode) {
			return fmt.Errorf("%w: delete %s must carry only old_mode", ErrInvalidJournalInput, f.Path)
		}
	}
	return nil
}

func validFileState(s string) bool {
	for _, v := range FileStateValues {
		if v == s {
			return true
		}
	}
	return false
}

// safeRelativePath reports whether a journal path can be joined to a root
// without leaving it. Same rule as Prepare's manifest paths, deliberately: the
// journal is replayed by a recovery that has no candidate to compare against.
func safeRelativePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validHash(hash string) bool {
	rest, ok := strings.CutPrefix(hash, "sha256:")
	if !ok || len(rest) != 64 {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// --- created directories ---

func encodeCreatedDirs(dirs []string) (string, error) {
	if len(dirs) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(dirs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeCreatedDirs(raw string, f *MergeFile) error {
	if raw == "" || raw == "[]" {
		f.CreatedDirs = nil
		return nil
	}
	var dirs []string
	if err := json.Unmarshal([]byte(raw), &dirs); err != nil {
		return fmt.Errorf("created directories are not a JSON array: %v", err)
	}
	f.CreatedDirs = dirs
	return nil
}

// --- scanning and error mapping ---

type scanner interface{ Scan(dest ...any) error }

func scanOperation(row scanner) (MergeOperation, error) {
	var (
		op          MergeOperation
		failureCode sql.NullString
		createdAt   int64
		updatedAt   int64
	)
	if err := row.Scan(&op.ID, &op.ProjectID, &op.RunID, &op.CandidateHash,
		&op.BaseManifestHash, &op.TargetManifestHash, &op.ResultManifestHash,
		&op.RootKey, &op.TargetRoot, &op.Fence, &op.Status, &failureCode,
		&createdAt, &updatedAt, &op.Revision); err != nil {
		return MergeOperation{}, err
	}
	op.FailureCode = fromNullString(failureCode)
	op.CreatedAt = fromUnixMilli(createdAt)
	op.UpdatedAt = fromUnixMilli(updatedAt)
	return op, nil
}

// mapOperationWriteError maps the triggers of migration 010 to their sentinels.
// A trigger message is stable (it is a literal in the migration) and the
// mapping is by name, not by fuzzy text: an unrecognized error is returned
// unchanged, because reporting the wrong sentinel is worse than reporting none.
func mapOperationWriteError(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, errNameOperationTerminalIsFinal):
		return fmt.Errorf("%s: %w", op, ErrOperationTerminal)
	case strings.Contains(msg, errNameOperationIsHistory):
		return fmt.Errorf("%s: %w", op, ErrOperationIsHistory)
	case strings.Contains(msg, errNameOperationIdentityFrozen),
		strings.Contains(msg, errNameOperationFenceDecreased):
		// The journal helpers never rewrite an identity column and never lower a
		// fence, so reaching either trigger means a statement was written by
		// hand. It is reported as the general "invalid input" for the same
		// reason runstore maps its identity triggers to ErrInvalidRecord: the
		// caller built an update it must not build.
		return fmt.Errorf("%s: %w", op, ErrInvalidJournalInput)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

// mapFileWriteError does the same for merge_files' triggers.
func mapFileWriteError(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, errNameOperationTerminalIsFinal):
		return fmt.Errorf("%s: %w", op, ErrOperationTerminal)
	case strings.Contains(msg, errNameFileIsHistory):
		return fmt.Errorf("%s: %w", op, ErrOperationIsHistory)
	case strings.Contains(msg, errNameFileRestoreWithoutWrite):
		return fmt.Errorf("%s: %w", op, ErrOperationMoved)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

// The trigger messages of migration 010, spelled once. They are the literal
// RAISE(ABORT, ...) strings; a migration that renames one must rename it here
// too, which is why they are constants rather than inline literals scattered
// through the mappers.
const (
	errNameOperationTerminalIsFinal = "merge_operation_terminal_is_final"
	errNameOperationIsHistory       = "merge_operation_is_history"
	errNameOperationIdentityFrozen  = "merge_operation_identity_is_frozen"
	errNameOperationFenceDecreased  = "merge_operation_fence_decreased"
	errNameFileIsHistory            = "merge_file_is_history"
	errNameFileRestoreWithoutWrite  = "merge_file_restore_without_write"
)

// --- conversions, mirroring runstore's ---

func unixMilli(t time.Time) int64 { return t.UTC().UnixMilli() }

func fromUnixMilli(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullableUint32(v *uint32) any {
	if v == nil {
		return nil
	}
	return int64(*v)
}

func fromNullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func fromNullUint32(v sql.NullInt64) *uint32 {
	if !v.Valid {
		return nil
	}
	u := uint32(v.Int64)
	return &u
}
