// Crash recovery for merges (T1.09.b group 4): what an interrupted publish
// becomes when the process that started it is gone.
//
// The merge journal (migration 010) commits what a publish intends — every
// path, its old and new content hashes, its backup blob, its per-file state —
// before the first target byte changes (§19.3 item 4, §27.5 item 3). This file
// is the other half of that design: it decides, from the journal and the bytes
// actually on disk, what the operation has to become:
//
//   - applied, when every path already matches the candidate exactly — the
//     write half of §27.5 item 5's "完整目标匹配后才能记 applied + event". The
//     missing per-file marks are filled in, the status and the merge.completed
//     event go into one transaction;
//   - rolled_back, when only some paths match: the operation's own writes are
//     restored in reverse publish order, with the same per-file rules and the
//     same targetFS functions the publisher's rollback uses, and the empty
//     directories this operation created are removed;
//   - needs_recovery, when a path holds content that is neither this
//     operation's old nor its new content (an external edit), when a link was
//     swapped into the target, or when a backup blob is missing. The row is
//     left external_edit where the journal allows it, not one byte of the path
//     is touched, and the root lock stays held.
//
// What a recovery must never do, and has no code path for: reset or clean the
// tree, restore "the whole old backup", delete a directory this operation did
// not create, follow a link inside the root to write outside it, or write
// anything at all for a path whose backup blob it could not read (§27.5 item 5,
// §28's "不能制造指向缺失 blob 的可见版本").
//
// Takeover protocol (§27.5 item 6: a stale lease must not let a new worker
// publish while the old one might still run): before any state is decided, one
// transaction bumps the operation's fence and revision and records the
// recoverer's name (journal.TakeOverOperationTx). The publisher re-reads
// (status, fence) before every single file, so a still-running old publisher
// stops at its next checkOwnership and writes not one more byte. Recording the
// taker in failure_code is a deliberate use of the frozen 010 schema: there is
// no owner column, and amending the migration is not this card's to do.
package merge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/codeflow/backend/internal/workspace"
)

// RecoverOutcome* name what Recover did, so a start-up caller can log a summary
// and a test can assert the decision without re-reading the journal.
const (
	// RecoverOutcomeNone: the root has no unfinished operation. Nothing was
	// read and nothing was written.
	RecoverOutcomeNone = "none"
	// RecoverOutcomeUnchanged: the operation is terminal (nothing may move it)
	// or it is in needs_recovery and the caller did not ask for a retry. The
	// journal and the tree were left exactly as they were (a retryless
	// needs_recovery read still records the taker; see Recover).
	RecoverOutcomeUnchanged = "unchanged"
	// RecoverOutcomeApplied: the target matched the candidate completely; the
	// operation is applied and merge.completed was written.
	RecoverOutcomeApplied = "applied"
	// RecoverOutcomeRolledBack: the target was brought back to its state before
	// the publish and the root lock was released.
	RecoverOutcomeRolledBack = "rolled_back"
	// RecoverOutcomeNeedsRecovery: the recovery stopped without touching the
	// path in question; the operation holds the root lock until a human
	// decides (or RetryRecovery runs after the human did).
	RecoverOutcomeNeedsRecovery = "needs_recovery"
)

// The failure_code values a recovery writes. The taker's own marker is
// takeoverCodePrefix + name (journal.go).
const (
	recoveryPreparedCode     = "recovery_prepared"
	recoveryRolledBackCode   = "recovery_rolled_back"
	recoveryNeedsRecoveryMsg = "recovery_needs_recovery"
)

// RecoverInput is everything Recover needs. Like PublishInput it takes the
// values the caller bound instead of re-deriving them: which directory, which
// identity, which store — and who is doing the recovering.
type RecoverInput struct {
	// Store is the run-store handle. Recover opens its own transactions.
	Store *runstore.Store
	// Blobs is the store the backups live in. A backup that cannot be read
	// parks the operation instead of writing a guess.
	Blobs BlobStore
	// TargetRoot is the directory the operation wrote into.
	TargetRoot string
	// ExpectedRoot is the root identity the caller bound to TargetRoot. It is
	// checked against the directory before anything is decided, and re-checked
	// per file: recovering "into" a directory that was replaced would restore
	// into a tree the operation never bound.
	ExpectedRoot workspace.RootIdentity
	// Recoverer names who is taking over (a server instance id, a test). It is
	// recorded in the journal.
	Recoverer string
	// Now is the clock. Required, for the same reason PublishInput.Now is.
	Now func() time.Time
	// FS is the file-system seam; nil means the operating system.
	FS fsOps
	// Destinations names the outbox deliveries for merge.completed, exactly as
	// PublishInput does.
	Destinations []string
	// OperationID selects one operation. Empty means "whatever operation holds
	// this root's lock", which is the form a start-up scan uses: for every
	// unfinished operation ListNonTerminalOperations returns, call Recover with
	// its RootKey bound to a TargetRoot.
	OperationID string
	// Retry is the explicit human entry point: reconcile a needs_recovery
	// operation a second time, after a human resolved what stopped the first
	// attempt. RetryRecovery sets it and calls Recover.
	Retry bool
}

func (in RecoverInput) fs() fsOps {
	if in.FS != nil {
		return in.FS
	}
	return osFS{}
}

func (in RecoverInput) clock() time.Time {
	if in.Now == nil {
		return time.Time{}
	}
	return in.Now().UTC()
}

// RecoverReport describes what the journal and the tree are after Recover.
//
// It is returned with a nil error for every outcome Recover reached, including
// needs_recovery: "a human must look at this" is a result, not a failure of the
// recoverer. The error is for the cases where even the journal could not be
// brought to a definite state.
type RecoverReport struct {
	// Operation is the journal row as it stands.
	Operation MergeOperation
	// Files is the operation's file list in publish order, as it stands.
	Files []MergeFile
	// Outcome is one of the RecoverOutcome* values.
	Outcome string
	// Reasons names, in order, why the outcome is needs_recovery (or why an
	// unchanged operation was left alone). Empty for applied and rolled_back.
	Reasons []string
	// Event is the merge.completed event written in the same transaction as
	// applied. Nil for every other outcome.
	Event *runstore.Event
	// LeftoverDirs lists directories this operation created that a rollback
	// could not remove because they are no longer empty. Reported, never
	// forced.
	LeftoverDirs []string
	// TakenOver reports whether the takeover transaction ran (false for a
	// missing or terminal operation — Recover never writes the journal for
	// those).
	TakenOver bool
}

// Recover decides what an interrupted merge operation becomes, and performs it.
//
// The caller is the process's start-up path (T1.04 wires it): for every root
// with an unfinished operation — ListNonTerminalOperations — it calls Recover
// once. Per-state handling is documented on recoverer.run.
func Recover(ctx context.Context, in RecoverInput) (RecoverReport, error) {
	r, err := newRecoverer(in)
	if err != nil {
		return RecoverReport{}, err
	}
	return r.run(ctx)
}

// RetryRecovery is the explicit second attempt for a needs_recovery operation:
// a human resolved the external edit (or the link, or the missing blob), and
// this call re-runs the same reconciliation once. If the obstacle is still
// there, the operation stays needs_recovery.
func RetryRecovery(ctx context.Context, in RecoverInput) (RecoverReport, error) {
	in.Retry = true
	return Recover(ctx, in)
}

// targetFS is the file-system half of a rollback: the root, the blob store and
// the directories a rollback could not remove. It was extracted from the
// publisher when the crash recovery needed exactly the same per-file rules —
// restoring from a backup, removing created directories, refusing to write
// through a link — without a publisher around them. Both callers must behave
// identically, and they do because they call these functions.
type targetFS struct {
	fs   fsOps
	root string
	// blobs is where the backups live; a restore streams from here.
	blobs BlobStore
	// leftover accumulates the directories a rollback could not remove because
	// they are no longer empty (see removeCreatedDirs).
	leftover []string
}

// checkRealAncestors refuses a path whose parent chain below the target root
// holds anything but real directories.
//
// Every write, delete and restore of a publisher and a recovery addresses a
// path by joining it to the target root, and the OS resolves every
// intermediate component of that join. A directory that was replaced by a
// symbolic link or a Windows junction after the journal was planned would
// therefore redirect the write — or the delete — to wherever the link points,
// outside the root: the root's own identity check and the per-file lstat
// (which does not follow only the *last* component) cannot see it. Measured on
// Go 1.26 / Windows: os.Lstat reports a junction as ModeSymlink, and a file
// written through it lands in the junction's target.
//
// With requireExist every ancestor must exist (the parent of a file about to
// be written, deleted or restored always does). Without it a missing ancestor
// ends the walk — nothing below a missing directory can be followed — which is
// what a rollback needs for a create whose directories were never made.
//
// The check runs immediately before each file-system step, so what remains is
// the window between this lstat walk and the step itself; closing that would
// need handle-relative file operations Go does not offer portably.
func (t *targetFS) checkRealAncestors(rel string, requireExist bool) error {
	for _, dir := range ancestorDirs(rel) {
		local, err := targetPath(t.root, dir)
		if err != nil {
			return err
		}
		entry, err := t.fs.lstat(local)
		if err != nil {
			return err
		}
		if !entry.Exists {
			if requireExist {
				return fmt.Errorf("%w: %s is missing; %s cannot be addressed under the target root",
					ErrTargetChanged, dir, rel)
			}
			return nil
		}
		if entry.IsSymlink || !entry.IsDir {
			return fmt.Errorf("%w: %s is %s, not a directory of the target root; writing %s through it could land outside the root",
				ErrTargetChanged, dir, entryWord(entry), rel)
		}
	}
	return nil
}

// sweepCreatedDirs removes the directories this operation created for a path
// whose content it never (visibly) wrote: the path's own empty parents, deepest
// first, and only when they are empty. A directory somebody else filled is
// reported, never deleted (see removeCreatedDirs).
func (t *targetFS) sweepCreatedDirs(ctx context.Context, f *MergeFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return t.removeCreatedDirs(f)
}

// restoreFile puts the old content back for one path.
//
// For a create that means removing the file (when it is there at all — a create
// whose content step failed before its replace has nothing to remove, and its
// undo is still "the path must not exist") and then the directories this
// operation created for it; for a modify or a delete it means streaming the
// backup blob back over the path, with the old mode. The blob is verified while
// it is read (see replaceFromBlob), so a backup that cannot be trusted cannot
// be written back. A blob store that cannot open the backup fails here, before
// the path is touched: no byte is written without its backup read.
func (t *targetFS) restoreFile(ctx context.Context, f *MergeFile) error {
	local, err := targetPath(t.root, f.Path)
	if err != nil {
		return err
	}
	if err := t.checkRealAncestors(f.Path, f.Kind != KindCreate); err != nil {
		return err
	}
	if f.Kind == KindCreate {
		entry, err := t.fs.lstat(local)
		if err != nil {
			return err
		}
		if entry.Exists {
			if err := t.fs.removeFile(local); err != nil {
				return err
			}
		}
		return t.removeCreatedDirs(f)
	}

	reader, err := t.blobs.Open(*f.BackupHash)
	if err != nil {
		return fmt.Errorf("merge: open backup of %s: %w", f.Path, err)
	}
	defer reader.Close()

	dir := filepath.Dir(local)
	tmp, err := t.fs.createTemp(dir, ".codeflow-merge-restore-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	published := false
	defer func() {
		if !published {
			_ = tmp.Close()
			_ = discardStaging(tmpPath)
		}
	}()

	sum, _, err := copyAndHash(tmp, reader, ctx)
	if err != nil {
		return fmt.Errorf("merge: restore %s: %w", f.Path, err)
	}
	if sum != *f.OldHash {
		// The backup blob does not hold the content the journal says it does.
		// Writing it would put an unknown version of the user's file in place of
		// the known one, which is worse than leaving the wrong (but known) new
		// content there for a human to look at.
		return fmt.Errorf("merge: the backup of %s hashes to %s, the journal says %s", f.Path, sum, *f.OldHash)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("merge: sync restore of %s: %w", f.Path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("merge: close restore staging file for %s: %w", f.Path, err)
	}
	perm := os.FileMode(0o644)
	if f.OldMode != nil {
		perm = os.FileMode(*f.OldMode)
	}
	if err := t.fs.replaceFile(tmpPath, local, perm); err != nil {
		return err
	}
	published = true
	syncDir(dir)
	return nil
}

// removeCreatedDirs deletes the directories this operation created for a file
// that is being un-created, deepest first, and only when they are empty.
//
// A directory that is no longer empty is left alone and reported: something
// else is in it, and deleting it would destroy content this operation never
// wrote. That is the difference between "undo my own work" and "restore a
// backup over the user's tree", which §27.5 item 5 forbids.
func (t *targetFS) removeCreatedDirs(f *MergeFile) error {
	dirs := append([]string(nil), f.CreatedDirs...)
	for i := len(dirs) - 1; i >= 0; i-- {
		local, err := targetPath(t.root, dirs[i])
		if err != nil {
			return err
		}
		// A directory this operation created that is now a link, or that sits
		// below one, is not the directory it created: report it, touch nothing.
		if err := t.checkRealAncestors(dirs[i], false); err != nil {
			t.leftover = append(t.leftover, dirs[i])
			continue
		}
		if entry, err := t.fs.lstat(local); err == nil && entry.Exists && (entry.IsSymlink || !entry.IsDir) {
			t.leftover = append(t.leftover, dirs[i])
			continue
		}
		names, err := t.fs.readDir(local)
		if err != nil {
			// Already gone (somebody removed it, or an earlier file of this
			// operation created it and a later rollback step removed it): the
			// goal is met.
			entry, statErr := t.fs.lstat(local)
			if statErr == nil && !entry.Exists {
				continue
			}
			return err
		}
		if len(names) > 0 {
			t.leftover = append(t.leftover, dirs[i])
			continue
		}
		if err := t.fs.removeDir(local); err != nil {
			entry, statErr := t.fs.lstat(local)
			if statErr == nil && !entry.Exists {
				continue
			}
			return err
		}
	}
	return nil
}

// recoverer carries the state of one recovery.
type recoverer struct {
	in     RecoverInput
	target *targetFS
	op     MergeOperation
	files  []MergeFile
	// reasons collects why the outcome is needs_recovery, in the order the
	// obstacles were found.
	reasons   []string
	takenOver bool
}

func (r *recoverer) clock() time.Time { return r.in.clock() }

func newRecoverer(in RecoverInput) (*recoverer, error) {
	if err := validateRecoverInput(in); err != nil {
		return nil, err
	}
	return &recoverer{
		in:     in,
		target: &targetFS{fs: in.fs(), root: in.TargetRoot, blobs: in.Blobs},
	}, nil
}

func validateRecoverInput(in RecoverInput) error {
	switch {
	case in.Store == nil:
		return fmt.Errorf("%w: Store is required", ErrInvalidJournalInput)
	case in.Blobs == nil:
		return fmt.Errorf("%w: Blobs is required", ErrInvalidJournalInput)
	case strings.TrimSpace(in.TargetRoot) == "":
		return fmt.Errorf("%w: TargetRoot is required", ErrInvalidJournalInput)
	case in.ExpectedRoot.IsZero():
		return fmt.Errorf("%w: ExpectedRoot is required", ErrInvalidJournalInput)
	case strings.TrimSpace(in.Recoverer) == "":
		return fmt.Errorf("%w: Recoverer is required (the taker is recorded in the journal)", ErrInvalidJournalInput)
	case in.Now == nil:
		return fmt.Errorf("%w: Now is required", ErrInvalidJournalInput)
	case in.clock().IsZero():
		return fmt.Errorf("%w: Now returned a zero instant", ErrInvalidJournalInput)
	}
	return nil
}

// run is the whole decision, in the order the rules require:
//
//  1. the target root is still the directory the caller bound. A changed root
//     means a recovery would restore into a tree the operation never bound:
//     refuse before anything — including the journal — is written;
//  2. select the operation (explicitly, or whatever holds the root lock) and
//     refuse an operation that belongs to another root;
//  3. terminal operations are history: report them and change nothing;
//  4. take over: fence + 1, revision + 1, the taker recorded. From here on a
//     superseded publisher stops at its next per-file checkOwnership;
//  5. per state:
//     - prepared: no file list can exist, so no target byte was written.
//     Roll back (zero disk touches);
//     - applying: classify every path from disk. All written → applied +
//     merge.completed in one transaction; otherwise roll back;
//     - rolling_back: continue the rollback;
//     - needs_recovery: without Retry, leave it (the human's move) and report
//     why it is there; with Retry, re-classify exactly like applying.
func (r *recoverer) run(ctx context.Context) (RecoverReport, error) {
	if err := r.checkRootIdentity(); err != nil {
		return RecoverReport{}, err
	}

	op, err := r.selectOperation(ctx)
	if err != nil {
		return RecoverReport{}, err
	}
	if op.ID == "" {
		return RecoverReport{Outcome: RecoverOutcomeNone}, nil
	}
	r.op = op

	if Terminal(op.Status) {
		r.reasons = append(r.reasons, fmt.Sprintf("operation %s is %s (terminal)", op.ID, op.Status))
		return r.report(ctx, RecoverOutcomeUnchanged, nil)
	}

	previousCode := op.FailureCode
	if err := r.takeOver(ctx); err != nil {
		return r.report(ctx, RecoverOutcomeUnchanged, err)
	}

	switch r.op.Status {
	case StatusPrepared:
		// No file list can exist for a prepared operation (WriteFilesTx moves
		// to applying in the same transaction that inserts the rows), so "no
		// target byte was written" is a fact of the schema, not a guess.
		return r.finishRollback(ctx, recoveryPreparedCode)
	case StatusApplying:
		return r.reconcile(ctx)
	case StatusRollingBack:
		return r.rollback(ctx)
	case StatusNeedsRecovery:
		if !r.in.Retry {
			if previousCode != nil {
				// The takeover's marker in failure_code is the only place the
				// taker can be recorded (010 has no owner column), but the
				// reason the operation parked is what a human needs to see.
				// Put it back; the report names the taker.
				if err := r.restoreFailureCode(ctx, *previousCode); err != nil {
					return r.report(ctx, RecoverOutcomeNeedsRecovery, err)
				}
			}
			r.reasons = append(r.reasons, fmt.Sprintf("operation %s is needs_recovery", r.op.ID))
			if previousCode != nil {
				r.reasons = append(r.reasons, "recorded reason: "+*previousCode)
			}
			for _, f := range r.readFilesQuiet(ctx) {
				if f.State == FileStateExternalEdit {
					r.reasons = append(r.reasons, fmt.Sprintf("%s was left for a human after an external edit", f.Path))
				}
			}
			return r.report(ctx, RecoverOutcomeUnchanged, nil)
		}
		return r.reconcile(ctx)
	default:
		return r.report(ctx, RecoverOutcomeUnchanged,
			fmt.Errorf("%w: operation %s is in unknown state %q", ErrInvalidJournalInput, r.op.ID, r.op.Status))
	}
}

// selectOperation returns the operation to recover, or a zero value when the
// root has none. An explicit OperationID that does not belong to the bound
// root is refused: recovering one root's operation against another root's
// directory would restore the wrong tree.
func (r *recoverer) selectOperation(ctx context.Context) (MergeOperation, error) {
	rootKey := r.in.ExpectedRoot.String()
	if id := strings.TrimSpace(r.in.OperationID); id != "" {
		op, err := GetOperation(ctx, r.in.Store.DB(), id)
		if err != nil {
			return MergeOperation{}, err
		}
		if op.RootKey != rootKey {
			return MergeOperation{}, fmt.Errorf("%w: operation %s belongs to root %s, not %s",
				ErrInvalidJournalInput, id, op.RootKey, rootKey)
		}
		return op, nil
	}
	blocking, id, err := BlockingOperation(ctx, r.in.Store.DB(), rootKey)
	if err != nil {
		return MergeOperation{}, err
	}
	if !blocking {
		return MergeOperation{}, nil
	}
	op, err := GetOperation(ctx, r.in.Store.DB(), id)
	if err != nil {
		return MergeOperation{}, err
	}
	if op.RootKey != rootKey {
		return MergeOperation{}, fmt.Errorf("%w: operation %s holds root %s, not %s",
			ErrInvalidJournalInput, id, op.RootKey, rootKey)
	}
	return op, nil
}

// takeOver is the fencing step of §27.5 item 6, in one transaction.
func (r *recoverer) takeOver(ctx context.Context) error {
	var op MergeOperation
	err := r.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		var err error
		op, err = TakeOverOperationTx(ctx, tx, r.op, r.in.Recoverer, r.clock())
		return err
	})
	if err != nil {
		return err
	}
	r.op = op
	r.takenOver = true
	return nil
}

// checkRootIdentity confirms the target directory is still the bound root.
func (r *recoverer) checkRootIdentity() error {
	got, err := r.target.fs.identity(r.in.TargetRoot)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRootIdentityChanged, err)
	}
	if !got.Equal(r.in.ExpectedRoot) {
		return fmt.Errorf("%w: %s is %s, the binding says %s",
			ErrRootIdentityChanged, r.in.TargetRoot, got, r.in.ExpectedRoot)
	}
	return nil
}

// --- the apply / rollback decision (applying, and the retry of needs_recovery) ---

// reconcile classifies every path and either completes the apply or rolls back.
func (r *recoverer) reconcile(ctx context.Context) (RecoverReport, error) {
	files, err := ListFiles(ctx, r.in.Store.DB(), r.op.ID)
	if err != nil {
		return r.report(ctx, RecoverOutcomeNeedsRecovery, err)
	}
	r.files = files
	if len(files) == 0 {
		// An applying operation with no file list cannot be produced by the
		// journal API (WriteFilesTx inserts the rows and the status move in
		// one transaction). "" is not "everything is applied" — fail closed
		// and let a human look.
		return r.park(ctx, "the operation is applying but has no file rows")
	}

	classes := make([]targetClass, len(files))
	for i := range files {
		class, err := r.classifyAt(ctx, &files[i])
		if err != nil {
			return r.parkErr(ctx, err)
		}
		classes[i] = class
	}

	allWritten := true
	for _, class := range classes {
		if class != targetWritten {
			allWritten = false
			break
		}
	}
	if allWritten {
		return r.finishApplied(ctx, files)
	}

	// Not every path matches the candidate: the operation must go back. The
	// classes already computed are re-derived per file inside rollback (the
	// disk is read again there, next to the write it justifies).
	return r.rollback(ctx)
}

// targetClass is what the bytes at one path say about the operation.
type targetClass int

const (
	// targetUnapplied: the path still holds exactly what the operation
	// expected to find (old content; absent, for a create). The rollback goal
	// for the path is already true.
	targetUnapplied targetClass = iota
	// targetWritten: the path holds exactly the new content (absent, for a
	// delete). This is the operation's own write, or the completed delete.
	targetWritten
	// targetForeign: neither — somebody else's content. Touch nothing.
	targetForeign
)

// classifyAt reads one path and decides which of the three states it is in,
// with the same rules the publisher's backup phase and rollback use:
//
//	create  absent → unapplied; == new_hash → written; anything else → foreign
//	modify  == old_hash → unapplied; == new_hash → written; missing/other → foreign
//	delete  absent → written;   == old_hash → unapplied; anything else → foreign
//
// The link walk comes first: a path whose ancestors hold a link cannot be
// classified without following it out of the root, so it parks untouched.
// classifyAt is the classification for the reconcile phase (applying, and the
// retry of needs_recovery). It is classifyTarget with the two guards that make
// the classification safe to act on:
//
//   - a link above the path is refused before anything is read. A path whose
//     ancestors are not real directories cannot be judged without following it
//     out of the root — except when the row was already written: the path was
//     this operation's, so the rollback's foreign branch is the right answer
//     and it records external_edit and parks;
//   - the root identity is re-checked per path, like the publisher does.
func (r *recoverer) classifyAt(ctx context.Context, f *MergeFile) (targetClass, error) {
	if err := ctx.Err(); err != nil {
		return targetForeign, err
	}
	if err := r.target.checkRealAncestors(f.Path, false); err != nil {
		if f.State == FileStateWritten {
			return targetForeign, nil
		}
		return targetForeign, fmt.Errorf("%s: %v", f.Path, err)
	}
	if err := r.checkRootIdentity(); err != nil {
		return targetForeign, err
	}
	return r.classifyTarget(f)
}

// classifyTarget is classifyAt without the link and root checks, for callers
// that performed them already (the rollback loop, per file, before each step).
func (r *recoverer) classifyTarget(f *MergeFile) (targetClass, error) {
	local, err := targetPath(r.target.root, f.Path)
	if err != nil {
		return targetForeign, err
	}
	entry, err := r.target.fs.lstat(local)
	if err != nil {
		return targetForeign, err
	}
	switch f.Kind {
	case KindCreate:
		if !entry.Exists {
			return targetUnapplied, nil
		}
		if entry.IsRegular {
			hash, _, err := r.target.fs.hashFile(local)
			if err != nil {
				return targetForeign, err
			}
			if hash == *f.NewHash {
				return targetWritten, nil
			}
		}
		return targetForeign, nil
	case KindModify:
		if !entry.Exists || !entry.IsRegular {
			// The file the operation promised to modify is gone or is no
			// longer a file: neither old nor new content, so not ours.
			return targetForeign, nil
		}
		hash, _, err := r.target.fs.hashFile(local)
		if err != nil {
			return targetForeign, err
		}
		switch {
		case hash == *f.NewHash:
			return targetWritten, nil
		case hash == *f.OldHash:
			return targetUnapplied, nil
		default:
			return targetForeign, nil
		}
	case KindDelete:
		if !entry.Exists {
			// The delete happened (or is still true): this is the operation's
			// written state for a delete.
			return targetWritten, nil
		}
		if entry.IsRegular {
			hash, _, err := r.target.fs.hashFile(local)
			if err != nil {
				return targetForeign, err
			}
			if hash == *f.OldHash {
				return targetUnapplied, nil
			}
		}
		return targetForeign, nil
	default:
		return targetForeign, fmt.Errorf("%w: %s has kind %q", ErrInvalidJournalInput, f.Path, f.Kind)
	}
}

// finishApplied records the completion §27.5 item 5 asks for: the target
// matched completely, so every unmarked write is marked, the operation moves to
// applied, and merge.completed is appended — all in one transaction, exactly
// like publish's commitApplied. A failure aborts the whole transaction; there
// is no state in which the operation is applied and the event is missing.
func (r *recoverer) finishApplied(ctx context.Context, files []MergeFile) (RecoverReport, error) {
	at := r.clock()
	payload, err := json.Marshal(map[string]any{
		"operation_id":   r.op.ID,
		"candidate_hash": r.op.CandidateHash,
		"file_count":     len(files),
		"fence":          r.op.Fence,
	})
	if err != nil {
		return r.report(ctx, RecoverOutcomeApplied, fmt.Errorf("merge: encode merge.completed payload: %w", err))
	}
	runID := r.op.RunID
	identity, err := json.Marshal(run.ExecutionIdentity{
		ProjectID: r.op.ProjectID,
		RunID:     &runID,
		Actor:     run.Actor{Type: run.ActorTypeSystem, ID: mergeActorID},
	})
	if err != nil {
		return r.report(ctx, RecoverOutcomeApplied, fmt.Errorf("merge: encode merge.completed identity: %w", err))
	}

	var (
		op    MergeOperation
		event runstore.Event
	)
	err = r.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		// Fill the marks a crash swallowed: a row is written when the disk
		// says so, and the classification above said so for every row.
		for i := range files {
			f := &files[i]
			if f.State == FileStateWritten {
				continue
			}
			if err := MarkFileTx(ctx, tx, r.op.ID, f.Seq, f.State, FileStateWritten, at); err != nil {
				return err
			}
		}
		var err error
		op, err = UpdateOperationStatusCAS(ctx, tx, r.op, r.op.Status, StatusApplied, nil, at)
		if err != nil {
			return err
		}
		event, err = runstore.AppendEventTx(ctx, tx, runstore.EventInput{
			ProjectID:    r.op.ProjectID,
			RunID:        &runID,
			Type:         string(run.EventMergeCompleted),
			OccurredAt:   at,
			Identity:     identity,
			Payload:      payload,
			Destinations: r.in.Destinations,
		})
		return err
	})
	if err != nil {
		// The transaction left nothing behind: the operation is still applying
		// (at the takeover's fence) and holds the root lock. Report the failure
		// as it is — a second Recover will read the journal again.
		return r.report(ctx, RecoverOutcomeApplied, fmt.Errorf("merge: complete operation %s: %w", r.op.ID, err))
	}
	r.op = op
	out, cause := r.report(ctx, RecoverOutcomeApplied, nil)
	out.Event = &event
	return out, cause
}

// --- rollback ---

// rollback undoes the writes this operation made, in reverse publish order.
//
// It is the publisher's rollback (publish.go) against the same targetFS, with
// one reconciliation the publisher cannot make: the recovery classifies each
// path against *both* hashes, so a path that already holds the old content
// again is recognized as "the rollback goal of this path is met" instead of
// being treated as a failed write. The cases, per path:
//
//   - the path holds the operation's new content (for a delete: is absent) →
//     restore from the backup blob (a create is removed), then remove the
//     directories this operation created, deepest first, empty ones only. A
//     row that says `written` is marked `restored`;
//   - the path already holds the old content (for a create: is absent) → the
//     goal is met; nothing on disk is touched. A row that says `written` is
//     marked `restored` without a write — migration 010's comment anticipates
//     exactly this ("group 4 may move a written row to restored while
//     reconciling"), and `restored` is true: the old content is back;
//   - anything else, and the row was written → somebody edited the path after
//     this operation wrote it. The row is marked `external_edit`, the rollback
//     stops, and the operation parks in needs_recovery, which holds the root
//     lock. Not one byte of the path is touched;
//   - anything else, and the row is still pending → the content step never
//     happened (or wrote nothing durable). The bytes are somebody else's and
//     are left alone; only the directories this operation recorded for the
//     path are swept, because it may have created them.
//
// A link anywhere above a path parks the operation before classification:
// neither "is it still ours" nor "put it back" can be answered without
// following it out of the root.
func (r *recoverer) rollback(ctx context.Context) (RecoverReport, error) {
	files, err := ListFiles(ctx, r.in.Store.DB(), r.op.ID)
	if err != nil {
		return r.report(ctx, RecoverOutcomeNeedsRecovery, err)
	}
	r.files = files
	rollbackCtx := context.WithoutCancel(ctx)

	if r.op.Status != StatusRollingBack {
		op, err := r.casStatus(rollbackCtx, StatusRollingBack, nil)
		if err != nil {
			return r.report(ctx, RecoverOutcomeNeedsRecovery, err)
		}
		r.op = op
	}

	for i := len(files) - 1; i >= 0; i-- {
		f := &files[i]
		if err := r.target.checkRealAncestors(f.Path, false); err != nil {
			return r.parkFile(rollbackCtx, f, fmt.Sprintf("%s: %v", f.Path, err))
		}
		if err := r.checkRootIdentity(); err != nil {
			return r.parkErr(rollbackCtx, err)
		}
		class, err := r.classifyTarget(f)
		if err != nil {
			return r.parkErr(rollbackCtx, fmt.Errorf("%s: %v", f.Path, err))
		}
		switch class {
		case targetWritten:
			if err := r.target.restoreFile(rollbackCtx, f); err != nil {
				return r.parkErr(rollbackCtx, fmt.Errorf("%s: %v", f.Path, err))
			}
			if f.State == FileStateWritten {
				if err := r.markFile(rollbackCtx, f, FileStateWritten, FileStateRestored); err != nil {
					return r.parkErr(rollbackCtx, err)
				}
			}
		case targetUnapplied:
			// The path is already back at its pre-publish state. A create may
			// still own empty directories here (it made them, never wrote the
			// file, and a crash swallowed the rest); remove exactly those.
			if f.Kind == KindCreate {
				if err := r.target.removeCreatedDirs(f); err != nil {
					return r.parkErr(rollbackCtx, fmt.Errorf("%s: %v", f.Path, err))
				}
			}
			if f.State == FileStateWritten {
				if err := r.markFile(rollbackCtx, f, FileStateWritten, FileStateRestored); err != nil {
					return r.parkErr(rollbackCtx, err)
				}
			}
		case targetForeign:
			switch f.State {
			case FileStateWritten:
				return r.parkFile(rollbackCtx, f, fmt.Sprintf("%s was edited after it was written; the edit was left untouched", f.Path))
			case FileStateExternalEdit, FileStateRestored:
				// A previous attempt (or rollback) already stopped at this
				// path and a human has not resolved it: the content is still
				// neither this operation's nor the old one. Deciding now would
				// be choosing between the user's versions for them.
				return r.park(rollbackCtx, fmt.Sprintf("%s still holds neither the old nor the new content; a human has not resolved it", f.Path))
			default:
				// Never marked written and the content is not ours: the
				// content step either never happened or wrote nothing durable.
				// Leave every byte of the path alone; only sweep directories
				// this operation might have created for it.
				if err := r.target.sweepCreatedDirs(rollbackCtx, f); err != nil {
					return r.parkErr(rollbackCtx, fmt.Errorf("%s: %v", f.Path, err))
				}
			}
		}
	}

	return r.finishRollback(ctx, recoveryRolledBackCode)
}

// finishRollback records the end of a rollback (rolled_back, or rolled_back
// for a prepared operation, which never touched disk).
func (r *recoverer) finishRollback(ctx context.Context, code string) (RecoverReport, error) {
	op, err := r.casStatus(context.WithoutCancel(ctx), StatusRolledBack, &code)
	if err != nil {
		return r.report(ctx, RecoverOutcomeRolledBack, err)
	}
	r.op = op
	return r.report(ctx, RecoverOutcomeRolledBack, nil)
}

// park stops the recovery without touching anything else, and leaves the
// operation in needs_recovery — which holds the root lock, so no new publish
// can start over a tree that is half this operation's and half the user's.
func (r *recoverer) park(ctx context.Context, reason string) (RecoverReport, error) {
	r.reasons = append(r.reasons, reason)
	if r.op.Status == StatusNeedsRecovery {
		// Already there (a retry that met the obstacle again): only record the
		// new reason. UpdateOperationStatusCAS refuses expected == next on
		// purpose, so the same-state write is the failure-code CAS.
		if err := r.restoreFailureCode(ctx, recoveryNeedsRecoveryMsg); err != nil {
			return r.report(ctx, RecoverOutcomeNeedsRecovery, err)
		}
		return r.report(ctx, RecoverOutcomeNeedsRecovery, nil)
	}
	code := recoveryNeedsRecoveryMsg
	op, err := r.casStatus(context.WithoutCancel(ctx), StatusNeedsRecovery, &code)
	if err != nil {
		return r.report(ctx, RecoverOutcomeNeedsRecovery, err)
	}
	r.op = op
	return r.report(ctx, RecoverOutcomeNeedsRecovery, nil)
}

// parkErr parks with the error's message as the reason.
func (r *recoverer) parkErr(ctx context.Context, err error) (RecoverReport, error) {
	return r.park(ctx, err.Error())
}

// parkFile does parkErr for one path, and first records the migration's "this
// rollback gave up" state where the journal allows it: a row that was written
// becomes external_edit. The CAS uses the row's *on-disk* state — the recovery
// holds rows as it listed them, and a previous attempt may have moved one
// already — so a double honor is a no-op rather than a failure.
func (r *recoverer) parkFile(ctx context.Context, f *MergeFile, reason string) (RecoverReport, error) {
	if f.State != FileStateExternalEdit {
		rows := r.readFilesQuiet(ctx)
		stored := f
		for i := range rows {
			if rows[i].Seq == f.Seq {
				stored = &rows[i]
				break
			}
		}
		if stored.State != FileStateExternalEdit {
			if err := r.markFile(ctx, stored, stored.State, FileStateExternalEdit); err != nil {
				return r.parkErr(ctx, err)
			}
		}
	}
	return r.park(ctx, reason)
}

// --- journal helpers ---

func (r *recoverer) casStatus(ctx context.Context, next string, code *string) (MergeOperation, error) {
	var op MergeOperation
	err := r.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		var err error
		op, err = UpdateOperationStatusCAS(ctx, tx, r.op, r.op.Status, next, code, r.clock())
		return err
	})
	return op, err
}

func (r *recoverer) markFile(ctx context.Context, f *MergeFile, expected, next string) error {
	err := r.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		return MarkFileTx(ctx, tx, r.op.ID, f.Seq, expected, next, r.clock())
	})
	if err != nil {
		return err
	}
	f.State = next
	return nil
}

// restoreFailureCode rewrites failure_code with the same CAS tuple the status
// moves use.
func (r *recoverer) restoreFailureCode(ctx context.Context, code string) error {
	var op MergeOperation
	err := r.in.Store.WithTx(context.WithoutCancel(ctx), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		op, err = SetOperationFailureCodeCAS(ctx, tx, r.op, code, r.clock())
		return err
	})
	if err != nil {
		return err
	}
	r.op = op
	return nil
}

// readFilesQuiet lists the file rows for a report and ignores a read failure:
// the caller is already reporting an outcome, and a report helper must not turn
// that into an error.
func (r *recoverer) readFilesQuiet(ctx context.Context) []MergeFile {
	files, err := ListFiles(context.WithoutCancel(ctx), r.in.Store.DB(), r.op.ID)
	if err != nil {
		return r.files
	}
	r.files = files
	return files
}

// report reads the journal back and returns it with the outcome and cause. It
// is how every path of Recover reports: the caller gets the rows as they stand.
func (r *recoverer) report(ctx context.Context, outcome string, cause error) (RecoverReport, error) {
	readCtx := context.WithoutCancel(ctx)
	out := RecoverReport{
		Outcome:      outcome,
		Reasons:      r.reasons,
		LeftoverDirs: r.target.leftover,
		TakenOver:    r.takenOver,
	}
	if op, err := GetOperation(readCtx, r.in.Store.DB(), r.op.ID); err == nil {
		out.Operation = op
	} else {
		out.Operation = r.op
	}
	if files, err := ListFiles(readCtx, r.in.Store.DB(), r.op.ID); err == nil {
		out.Files = files
	} else {
		out.Files = r.files
	}
	return out, cause
}
