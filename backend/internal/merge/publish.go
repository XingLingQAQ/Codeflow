// The merge publisher (T1.09.b group 3): it turns a prepared merge.Candidate
// into changes in the user's directory, with a durable journal in codeflow.db
// and an in-process rollback when anything goes wrong.
//
// The shape of the algorithm is fixed by §27.5 items 3 and 4 and §19.3 item 4,
// and every step below names the one it implements:
//
//	(a) refuse before writing anything — an unpublishable candidate, a candidate
//	    whose hash does not describe it, an operation this step does not support
//	    (a symlink), a target root that is not the one the caller bound;
//	(b) transaction 1: insert the operation in prepared = take the root lock.
//	    A root that already holds a non-terminal operation (including
//	    needs_recovery) refuses the new one with ErrRootBusy and nothing is
//	    written;
//	(c) stream every backup into the blob store and re-check the target against
//	    the candidate's old hashes. A mismatch means somebody changed the target
//	    after Prepare: the operation becomes conflict, no file row and no target
//	    byte is written;
//	(d) transaction 2: write the whole file list and move the operation to
//	    applying. This is §19.3 item 4's "先提交 merge journal": after it
//	    commits, every path, every expectation and every backup blob is durable,
//	    and only then does the first target byte change. Nothing here can name a
//	    blob that does not exist, because the order is always "blob first, row
//	    second";
//	(e) per file, in seq order: re-read the operation (fence and state) — a
//	    takeover stops the loop before the next write — re-capture the root
//	    identity, re-check the precondition against old_hash, then stage the new
//	    content in the target's own directory, sync it, replace the target with
//	    it in one step, and record `written` for that file in its own small
//	    transaction;
//	(f) re-verify every path against the candidate, and only then write
//	    applied + merge.completed (+ outbox rows) in one transaction.
//
// SQLite and a directory cannot be committed together, and this file does not
// pretend otherwise: every window between a database write and a file-system
// write is either closed by the journal (a crash leaves a row that group 4's
// recovery can read) or by the per-file precondition (an external editor is
// never locked out, so it is detected). What the publisher does guarantee is
// that it never claims more than it did: `written` is recorded only after the
// file on disk is the new content, and `applied` only after every path has been
// read back.
//
// What this file is not: it is not crash recovery (a process that dies
// mid-publish is T1.09.b group 4's job), it is not the Guard/Approval gate
// (T2.03 inserts that before the first row is written), and it is not a
// transactional file system.
package merge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/codeflow/backend/internal/runworkspace"
	"github.com/codeflow/backend/internal/workspace"
)

// BlobStore is the content-addressed store the publisher writes backups and new
// content into and restores from.
//
// The signatures are the ones of (*artifact.BlobStore); this package does not
// import artifact, so a test can hand in an in-memory implementation, and the
// production wiring hands in the real store (artifact_blobs_test.go proves the
// two meet at compile time). Open returns bytes that hash to the requested
// value: the production store verifies that at EOF, and this package re-hashes
// what it read before it restores from a backup, so a corrupt blob fails the
// rollback instead of silently writing wrong content into a user's file.
type BlobStore interface {
	Put(ctx context.Context, r io.Reader) (hash string, size int64, err error)
	Open(hash string) (io.ReadCloser, error)
}

// Sentinels of the publisher. Each one means "no further write happened", and
// each one is returned together with the journal rows as they stand, so a
// caller can report what the operation became.
var (
	// ErrCandidateNotPublishable means the candidate has conflicts or nothing to
	// do (§27.5 item 2: a conflicting candidate never touches the target).
	ErrCandidateNotPublishable = errors.New("merge: the candidate is not publishable")

	// ErrCandidateHashMismatch means the candidate's own Hash does not describe
	// its content, so the value a Guard report or an Approval was bound to
	// cannot be trusted.
	ErrCandidateHashMismatch = errors.New("merge: the candidate hash does not describe the candidate")

	// ErrUnsupportedOperation means the candidate carries an operation this step
	// does not publish: a symlink, or a kind outside create/modify/delete. It is
	// refused rather than approximated — a symlink's "content" is a link target
	// string and writing it as a file would silently change what the path means.
	ErrUnsupportedOperation = errors.New("merge: this publisher does not support the operation")

	// ErrTargetChanged means the target no longer holds what the candidate
	// required: at backup time the operation becomes conflict and nothing is
	// written; during the publish loop it triggers a rollback.
	ErrTargetChanged = errors.New("merge: the target changed after the candidate was prepared")

	// ErrRootIdentityChanged means the target root is not the directory the
	// caller bound (or it was replaced while publishing). Publishing into it
	// would write into a directory the caller never authorized.
	ErrRootIdentityChanged = errors.New("merge: the target root identity changed")

	// ErrNeedsRecovery means a rollback could not finish: a path a previous
	// write of this operation had touched now holds something else, or the
	// restore itself failed. The operation is left in needs_recovery, which
	// holds the root lock, and the path in question is not touched.
	ErrNeedsRecovery = errors.New("merge: the rollback could not finish; the operation needs recovery")
)

// PublishInput is everything Publish needs. It is deliberately explicit: the
// publisher takes the values the caller bound rather than re-deriving them, so
// that "which directory, which identity, which run" is a decision made outside
// this package and visible in one place.
type PublishInput struct {
	// Store is the run-store handle. Publish opens its own transactions through
	// it (the journal functions never do).
	Store *runstore.Store
	// Blobs stores the backup of every modify/delete and the new content of
	// every create/modify.
	Blobs BlobStore
	// Candidate is the change set to publish. It must be publishable and
	// self-consistent.
	Candidate *Candidate
	// ResultRoot is the working copy the candidate's new content is read from:
	// the same directory Prepare was handed.
	ResultRoot string
	// TargetRoot is the user's directory the operations apply to.
	TargetRoot string
	// ExpectedRoot is the root identity the caller bound to TargetRoot (from the
	// workspace binding). Captured once at the start and re-captured before
	// every file.
	ExpectedRoot workspace.RootIdentity
	// ProjectID and RunID scope the operation and its completion event.
	ProjectID string
	RunID     string
	// OperationID is the journal row's id. The caller supplies it so a retry of
	// the same logical merge reuses one id (and a different one cannot silently
	// claim the same row).
	OperationID string
	// Now is the clock. Required; there is no default, because a journal whose
	// timestamps come from an implicit clock cannot be tested.
	Now func() time.Time
	// Destinations names the outbox deliveries for merge.completed, exactly as
	// runstore.TransitionInput does. Empty is legal.
	Destinations []string
	// FS is the file-system seam; nil means the operating system. Tests hand in
	// an implementation that fails at a chosen step.
	FS fsOps
	// Hook is called before and after each file's content step and before the
	// journal mark; returning an error fails the publish at that point.
	Hook Hook
}

func (in PublishInput) fs() fsOps {
	if in.FS != nil {
		return in.FS
	}
	return osFS{}
}

func (in PublishInput) clock() time.Time {
	if in.Now == nil {
		return time.Time{}
	}
	return in.Now().UTC()
}

// PublishResult describes what the journal holds after Publish returns.
//
// It is returned even when the error is non-nil: a failed publish is a fact
// about the journal (which state the operation reached, which files were
// written and restored, which directories were left behind), and a caller that
// wanted only the error would have to re-read the database to learn it.
type PublishResult struct {
	// Operation is the journal row in its final state.
	Operation MergeOperation
	// Files is the file list in publish order, with each row's final state.
	Files []MergeFile
	// LeftoverDirs lists the directories this operation created that a rollback
	// could not remove because they are no longer empty. They are reported, not
	// forced: the content somebody else put there is not this operation's to
	// delete.
	LeftoverDirs []string
	// Event is the merge.completed event, written in the same transaction as
	// applied. Nil for every other outcome.
	Event *runstore.Event
}

// Publish publishes the candidate into the target root.
//
// The order of the steps, and what each one guarantees, is documented in the
// file comment; the per-file rules of §27.5 item 4 are in writeFiles.
func Publish(ctx context.Context, in PublishInput) (PublishResult, error) {
	p, err := newPublisher(in)
	if err != nil {
		return PublishResult{}, err
	}
	return p.run(ctx)
}

// publisher carries the state of one publish. It owns the file list before it
// is committed, the operation row as the database last answered it, and the
// clock, so that every step of the loop uses the same values the caller bound.
type publisher struct {
	in    PublishInput
	fs    fsOps
	files []MergeFile
	op    MergeOperation
	// target is the file-system half of a rollback: the root, the blob store
	// and the directories a rollback could not remove. It is a separate value
	// because the crash recovery (T1.09.b group 4) performs the same per-file
	// restores without a publisher, and the rules must be the same object, not
	// the same prose.
	target *targetFS
	// takenOver is set when the journal says this process no longer holds the
	// operation. It stops the loop *and* suppresses the rollback: the writes on
	// disk belong to whoever holds the operation now.
	takenOver bool
	// parked is set when the failure has already been recorded as
	// needs_recovery (a changed root identity or a rollback that could not
	// finish), so the caller does not try to roll back a second time.
	parked bool
}

// clock is the input's clock, in UTC. Every journal write of one publish uses
// the same call site, so a test's scripted clock advances one step per write
// and the ordering of updated_at is deterministic.
func (p *publisher) clock() time.Time { return p.in.clock() }

func newPublisher(in PublishInput) (*publisher, error) {
	if err := validatePublishInput(in); err != nil {
		return nil, err
	}
	return &publisher{
		in:     in,
		fs:     in.fs(),
		target: &targetFS{fs: in.fs(), root: in.TargetRoot, blobs: in.Blobs},
	}, nil
}

func (p *publisher) run(ctx context.Context) (PublishResult, error) {
	// (a) Everything that can be refused is refused before the first write of
	// any kind — no journal row, no blob, no target byte.
	if err := p.checkCandidate(); err != nil {
		return PublishResult{}, err
	}
	if err := p.checkRootIdentity(); err != nil {
		return PublishResult{}, err
	}

	files, err := p.planFiles()
	if err != nil {
		return PublishResult{}, err
	}
	p.files = files

	// (b) Transaction 1: the operation row is the root lock.
	if err := p.lockRoot(ctx); err != nil {
		return PublishResult{}, err
	}

	// (c) Backups, then the new content, all before transaction 2. A target
	// that moved on since Prepare ends the publish here, as a conflict with
	// nothing written.
	if err := p.storeContent(ctx); err != nil {
		return p.abandon(ctx, err)
	}

	// (d) Transaction 2: the file list and `applying`, still before any target
	// write.
	if err := p.commitFileList(ctx); err != nil {
		return p.abandon(ctx, err)
	}

	// (e) The file loop.
	if err := p.writeFiles(ctx); err != nil {
		// Two failures are not this process's to undo. A takeover means another
		// holder owns the tree now and only it may decide what to roll back; a
		// changed root means the directory the writes went into is gone, and
		// writing backups back into the directory that replaced it would be
		// restoring into a tree this operation never bound. Both are recorded
		// (needs_recovery holds the root lock) and reported as they stand.
		if p.takenOver || p.parked || errors.Is(err, ErrNeedsRecovery) {
			return p.result(ctx, err)
		}
		return p.rollback(ctx, err)
	}

	// (f) Verify, then applied + merge.completed in one transaction.
	if err := p.verifyTarget(ctx); err != nil {
		return p.rollback(ctx, err)
	}
	return p.commitApplied(ctx)
}

// --- (a) refusals ---

func validatePublishInput(in PublishInput) error {
	switch {
	case in.Store == nil:
		return fmt.Errorf("%w: Store is required", ErrInvalidJournalInput)
	case in.Blobs == nil:
		return fmt.Errorf("%w: Blobs is required", ErrInvalidJournalInput)
	case in.Candidate == nil:
		return fmt.Errorf("%w: Candidate is required", ErrInvalidJournalInput)
	case strings.TrimSpace(in.ResultRoot) == "":
		return fmt.Errorf("%w: ResultRoot is required", ErrInvalidJournalInput)
	case strings.TrimSpace(in.TargetRoot) == "":
		return fmt.Errorf("%w: TargetRoot is required", ErrInvalidJournalInput)
	case in.ExpectedRoot.IsZero():
		return fmt.Errorf("%w: ExpectedRoot is required", ErrInvalidJournalInput)
	case strings.TrimSpace(in.ProjectID) == "":
		return fmt.Errorf("%w: ProjectID is required", ErrInvalidJournalInput)
	case strings.TrimSpace(in.RunID) == "":
		return fmt.Errorf("%w: RunID is required (merge.completed belongs to a run)", ErrInvalidJournalInput)
	case strings.TrimSpace(in.OperationID) == "":
		return fmt.Errorf("%w: OperationID is required", ErrInvalidJournalInput)
	case in.Now == nil:
		return fmt.Errorf("%w: Now is required", ErrInvalidJournalInput)
	case in.clock().IsZero():
		return fmt.Errorf("%w: Now returned a zero instant", ErrInvalidJournalInput)
	}
	return nil
}

func (p *publisher) checkCandidate() error {
	c := p.in.Candidate
	if !c.Publishable() {
		if len(c.Conflicts) > 0 {
			return fmt.Errorf("%w: %d conflict(s), first at %s (%s)",
				ErrCandidateNotPublishable, len(c.Conflicts), c.Conflicts[0].Path, c.Conflicts[0].Reason)
		}
		return fmt.Errorf("%w: the candidate has no operations", ErrCandidateNotPublishable)
	}
	if c.Hash != candidateHash(c) {
		return fmt.Errorf("%w: candidate hash is %s, its content hashes to %s",
			ErrCandidateHashMismatch, c.Hash, candidateHash(c))
	}
	if !sameRoot(p.in.TargetRoot, c.Root) {
		return fmt.Errorf("%w: target root %s, candidate root %s",
			ErrRootMismatch, p.in.TargetRoot, c.Root)
	}
	for _, op := range c.Operations {
		if err := supportedOperation(op); err != nil {
			return err
		}
	}
	return nil
}

// supportedOperation refuses the operations this step cannot publish.
//
// A symlink is the case that matters: its manifest "content" is the link target
// string, so the hash comparison that protects every other path would be
// comparing a string, not bytes, and a reader that followed the link would read
// the wrong file. Publishing links is a later decision (§27.5 item 1 records
// exclusions for links out of the tree; T1.09.c owns the remaining cases), so
// this step registers them as unsupported instead of guessing.
func supportedOperation(op Operation) error {
	switch op.Kind {
	case KindCreate, KindModify, KindDelete:
	default:
		return fmt.Errorf("%w: %s has kind %q", ErrUnsupportedOperation, op.Path, op.Kind)
	}
	if op.OldType != "" && op.OldType != runworkspace.TypeFile {
		return fmt.Errorf("%w: %s currently holds a %s", ErrUnsupportedOperation, op.Path, op.OldType)
	}
	if op.NewType != "" && op.NewType != runworkspace.TypeFile {
		return fmt.Errorf("%w: %s would become a %s", ErrUnsupportedOperation, op.Path, op.NewType)
	}
	if op.Kind != KindDelete && !validHash(op.NewHash) {
		return fmt.Errorf("%w: %s has no new content hash", ErrUnsupportedOperation, op.Path)
	}
	if op.Kind != KindCreate && !validHash(op.OldHash) {
		return fmt.Errorf("%w: %s has no old content hash", ErrUnsupportedOperation, op.Path)
	}
	return nil
}

// checkRootIdentity confirms the target root is the bound directory. A
// directory that was deleted and recreated, or a binding that was re-pointed,
// produces a different identity — and publishing into it would write into a
// tree the caller never looked at.
func (p *publisher) checkRootIdentity() error {
	got, err := p.fs.identity(p.in.TargetRoot)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRootIdentityChanged, err)
	}
	if !got.Equal(p.in.ExpectedRoot) {
		return fmt.Errorf("%w: %s is %s, the binding says %s",
			ErrRootIdentityChanged, p.in.TargetRoot, got, p.in.ExpectedRoot)
	}
	return nil
}

// --- the plan ---

// planFiles turns the candidate's operations into journal rows, with seq the
// publish order (the candidate's own path order, 1-based).
func (p *publisher) planFiles() ([]MergeFile, error) {
	ops := p.in.Candidate.Operations
	out := make([]MergeFile, 0, len(ops))
	for i, op := range ops {
		f := MergeFile{
			OperationID: p.in.OperationID,
			Seq:         int64(i + 1),
			Path:        op.Path,
			Kind:        op.Kind,
			State:       FileStatePending,
		}
		if op.Kind != KindCreate {
			oldHash := op.OldHash
			oldMode := op.OldMode
			f.OldHash = &oldHash
			f.OldMode = &oldMode
		}
		if op.Kind != KindDelete {
			newHash := op.NewHash
			newMode := op.NewMode
			f.NewHash = &newHash
			f.NewMode = &newMode
		}
		out = append(out, f)
	}

	// The directories this publish will have to create, computed before
	// anything is created and recorded in the journal: a rollback and group 4's
	// recovery can only delete what the journal says this operation made.
	if err := p.planCreatedDirs(out); err != nil {
		return nil, err
	}
	return out, nil
}

// planCreatedDirs decides which ancestor directories of which files do not
// exist yet and must be created.
//
// The simulation walks the files in publish order and keeps one set of
// directories that will exist by the time each file is written — the ones
// already on disk plus the ones an earlier file will create. A directory that
// only one file needs is recorded on that file; a directory two files need is
// recorded once, on the earlier one, so a rollback that removes directories
// deepest-first cannot delete something a later file's write depends on.
//
// This is a plan, not a guarantee: the directories are created as the loop
// reaches them, and mkdir is allowed to fail with "already exists" — somebody
// else may have created one in the meantime, which is not an error.
func (p *publisher) planCreatedDirs(files []MergeFile) error {
	known := map[string]bool{}
	willExist := func(rel string) (bool, error) {
		if known[rel] {
			return true, nil
		}
		local, err := targetPath(p.in.TargetRoot, rel)
		if err != nil {
			return false, err
		}
		entry, err := p.fs.lstat(local)
		if err != nil {
			return false, err
		}
		if entry.Exists && entry.IsDir {
			known[rel] = true
			return true, nil
		}
		return false, nil
	}

	for i := range files {
		var created []string
		for _, dir := range ancestorDirs(files[i].Path) {
			exists, err := willExist(dir)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			created = append(created, dir)
			known[dir] = true
		}
		files[i].CreatedDirs = created
	}
	return nil
}

// --- (b) the root lock ---

func (p *publisher) lockRoot(ctx context.Context) error {
	in := CreateOperationInput{
		ID:                 p.in.OperationID,
		ProjectID:          p.in.ProjectID,
		RunID:              p.in.RunID,
		CandidateHash:      p.in.Candidate.Hash,
		BaseManifestHash:   p.in.Candidate.BaseManifestHash,
		TargetManifestHash: p.in.Candidate.TargetManifestHash,
		ResultManifestHash: p.in.Candidate.ResultManifestHash,
		RootKey:            p.in.ExpectedRoot.String(),
		TargetRoot:         p.in.TargetRoot,
		Now:                p.clock(),
	}
	var op MergeOperation
	err := p.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		var err error
		op, err = CreateOperationTx(ctx, tx, in)
		return err
	})
	if err != nil {
		return err
	}
	p.op = op
	return nil
}

// --- (c) backups and new content, before the file list is committed ---

func (p *publisher) storeContent(ctx context.Context) error {
	if err := p.storeBackups(ctx); err != nil {
		return err
	}
	return p.storeNewContent(ctx)
}

// storeBackups streams the current content of every modify/delete into the blob
// store, after checking that it is still the content the candidate expected.
//
// The check and the backup are the same read: the bytes that were hashed are
// the bytes that were stored, so a file that changed between the two cannot be
// backed up as if it had not. This is the last moment the target is examined
// before the journal commits, and it is the moment §27.5 item 4's "目标内容已变
// 化则冲突" is decided for the whole operation.
func (p *publisher) storeBackups(ctx context.Context) error {
	for i := range p.files {
		f := &p.files[i]
		if f.Kind == KindCreate {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		local, err := targetPath(p.in.TargetRoot, f.Path)
		if err != nil {
			return err
		}
		entry, err := p.fs.lstat(local)
		if err != nil {
			return err
		}
		if !entry.Exists || !entry.IsRegular {
			return fmt.Errorf("%w: %s is not a regular file any more", ErrTargetChanged, f.Path)
		}
		reader, err := p.fs.open(local)
		if err != nil {
			return err
		}
		hash, size, putErr := p.in.Blobs.Put(ctx, reader)
		closeErr := reader.Close()
		if putErr != nil {
			return fmt.Errorf("merge: back up %s: %w", f.Path, putErr)
		}
		if closeErr != nil {
			return fmt.Errorf("merge: back up %s: %w", f.Path, closeErr)
		}
		if hash != *f.OldHash || size != entry.Size {
			return fmt.Errorf("%w: %s holds %s (%d bytes), the candidate expected %s",
				ErrTargetChanged, f.Path, hash, size, *f.OldHash)
		}
		backup := hash
		f.BackupHash = &backup
	}
	return nil
}

// storeNewContent reads every create/modify's new content out of the working
// copy and stores it, checking the hash the candidate recorded.
//
// Prepare already stored these bytes; storing them again is not wasted work,
// it is the guarantee this function is here for: after it returns, every row in
// the file list names a blob that exists, whatever happened to the working copy
// in between. A working copy that changed is ErrResultChangedAfterCapture, the
// same sentinel Prepare uses, because it is the same fact.
func (p *publisher) storeNewContent(ctx context.Context) error {
	for i := range p.files {
		f := &p.files[i]
		if f.Kind == KindDelete {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		local, err := targetPath(p.in.ResultRoot, f.Path)
		if err != nil {
			return err
		}
		entry, err := p.fs.lstat(local)
		if err != nil {
			return err
		}
		if !entry.Exists || !entry.IsRegular {
			return fmt.Errorf("%w: %s is not a regular file in the working copy",
				ErrResultChangedAfterCapture, f.Path)
		}
		reader, err := p.fs.open(local)
		if err != nil {
			return err
		}
		hash, size, putErr := p.in.Blobs.Put(ctx, reader)
		closeErr := reader.Close()
		if putErr != nil {
			return fmt.Errorf("merge: store new content of %s: %w", f.Path, putErr)
		}
		if closeErr != nil {
			return fmt.Errorf("merge: store new content of %s: %w", f.Path, closeErr)
		}
		if hash != *f.NewHash || size != entry.Size {
			return fmt.Errorf("%w: %s holds %s (%d bytes), the candidate expected %s",
				ErrResultChangedAfterCapture, f.Path, hash, size, *f.NewHash)
		}
	}
	return nil
}

// abandon ends a published-nowhere operation and releases the root lock.
//
// Two outcomes, and the difference is the caller's:
//
//   - a target that moved on since Prepare is a conflict: the merge cannot be
//     applied to a tree that no longer holds its baseline, and §27.5 item 2
//     forbids overwriting what is there instead;
//   - anything else (a blob store that refused, a canceled context, a read
//     error) is rolled_back: nothing was written to the target, so "the target
//     is as it was" is exactly true, and the row records that the attempt
//     happened without pretending it succeeded.
func (p *publisher) abandon(ctx context.Context, cause error) (PublishResult, error) {
	status := StatusRolledBack
	code := "publish_abandoned"
	if errors.Is(cause, ErrTargetChanged) {
		status = StatusConflict
		code = "base_changed"
	}
	err := p.in.Store.WithTx(context.WithoutCancel(ctx), func(ctx context.Context, tx runstore.Tx) error {
		_, err := UpdateOperationStatusCAS(ctx, tx, p.op, StatusPrepared, status, &code, p.clock())
		return err
	})
	if err != nil {
		// The lock is still held and the reason is on the floor; reporting the
		// original cause is more useful than reporting the cleanup failure, so
		// both are returned and the caller can see the operation did not settle.
		return p.result(ctx, fmt.Errorf("%w (and the journal could not record it: %v)", cause, err))
	}
	p.op.Status = status
	p.op.FailureCode = &code
	p.op.Revision++
	return p.result(ctx, cause)
}

// --- (d) the file list ---

func (p *publisher) commitFileList(ctx context.Context) error {
	var op MergeOperation
	err := p.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		var err error
		op, err = WriteFilesTx(ctx, tx, p.op, p.files, p.clock())
		return err
	})
	if err != nil {
		return err
	}
	p.op = op
	return nil
}

// --- (e) the file loop ---

// writeFiles publishes every file in seq order (§27.5 item 4).
//
// Three rules are checked before each write, and each has its own reason:
//
//   - the operation is still `applying` and still at the fence this holder
//     started with. A recoverer that took the root over bumps the fence, and a
//     superseded publisher must not write one more byte — so the loop stops
//     with ErrOperationMoved and *without* rolling back, because undoing files
//     is the new holder's decision now;
//   - the target root is still the same directory. A root that was replaced
//     mid-publish means the files written so far no longer exist where they
//     were written; continuing would write into a foreign tree, and rolling
//     back into it would be worse. The operation is left in needs_recovery with
//     the lock held;
//   - the path still holds the old content (or, for a create, still does not
//     exist). This is the precondition that makes the write safe against an
//     external editor.
//
// A failed precondition or write is not special-cased at file 1: it rolls back
// like any other failure, and a rollback of an operation with nothing written
// is simply the move to rolled_back. One code path, one meaning.
func (p *publisher) writeFiles(ctx context.Context) error {
	for i := range p.files {
		f := &p.files[i]
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.checkOwnership(ctx); err != nil {
			p.takenOver = true
			return err
		}
		if err := p.checkRootIdentity(); err != nil {
			p.parked = true
			_, err := p.needsRecovery(context.WithoutCancel(ctx), err)
			return err
		}
		if err := p.checkPrecondition(f); err != nil {
			return err
		}
		if err := p.writeFile(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

// checkOwnership re-reads the operation row and stops a superseded publisher.
func (p *publisher) checkOwnership(ctx context.Context) error {
	current, err := GetOperation(ctx, p.in.Store.DB(), p.op.ID)
	if err != nil {
		return err
	}
	if current.Fence != p.op.Fence {
		return fmt.Errorf("%w: fence moved from %d to %d (operation %s)",
			ErrOperationMoved, p.op.Fence, current.Fence, p.op.ID)
	}
	if current.Status != StatusApplying {
		return fmt.Errorf("%w: operation %s is %s, not %s",
			ErrOperationMoved, p.op.ID, current.Status, StatusApplying)
	}
	p.op = current
	return nil
}

// checkPrecondition verifies the target still holds what the candidate
// expected before a single byte is written for this path.
func (p *publisher) checkPrecondition(f *MergeFile) error {
	local, err := targetPath(p.in.TargetRoot, f.Path)
	if err != nil {
		return err
	}
	entry, err := p.fs.lstat(local)
	if err != nil {
		return err
	}

	if f.Kind == KindCreate {
		if entry.Exists {
			return fmt.Errorf("%w: %s already exists (%s)", ErrTargetChanged, f.Path, entryWord(entry))
		}
		return nil
	}
	if !entry.Exists || !entry.IsRegular {
		return fmt.Errorf("%w: %s is %s, the candidate expected the file %s",
			ErrTargetChanged, f.Path, entryWord(entry), *f.OldHash)
	}
	hash, size, err := p.fs.hashFile(local)
	if err != nil {
		return err
	}
	if hash != *f.OldHash {
		return fmt.Errorf("%w: %s holds %s (%d bytes), the candidate expected %s",
			ErrTargetChanged, f.Path, hash, size, *f.OldHash)
	}
	return nil
}

// writeFile performs one file's content step and records it.
//
// The write itself is the pair §27.5 item 4 asks for: the new content is staged
// in the target's *own* directory (so the final step is a same-filesystem
// operation, not a copy across devices), synced, and then moved onto the target
// in one step (see osFS.replaceFile for the platform primitives). The target is
// therefore never truncated in place: a reader sees the old content or the new
// one.
//
// The journal mark comes after the disk write and in its own transaction. That
// ordering is deliberate: a row that says `written` is always true, while a row
// that says `pending` may or may not have been written — which is exactly the
// uncertainty group 4 resolves by comparing the content on disk with old_hash
// and new_hash.
//
// `pending` is therefore not a synonym for "untouched": a failure between the
// content step and the mark leaves a row in pending whose content this
// operation already wrote, and the same is true for every file after it that
// the loop never reached. Rollback handles that by deciding from the content on
// disk (see holdsOurWrite) rather than from the row alone.
func (p *publisher) writeFile(ctx context.Context, f *MergeFile) error {
	if err := p.contentStep(ctx, f); err != nil {
		return err
	}
	return p.markWritten(ctx, f)
}

// contentStep is the write itself, without the journal mark: for a create the
// directories are made first, then the staged content replaces the path; for a
// modify only the replace; for a delete the path is removed after its old
// content was confirmed.
func (p *publisher) contentStep(ctx context.Context, f *MergeFile) error {
	local, err := targetPath(p.in.TargetRoot, f.Path)
	if err != nil {
		return err
	}
	switch f.Kind {
	case KindCreate:
		if err := p.createDirs(f); err != nil {
			return err
		}
	case KindModify, KindDelete:
		// The parent directory exists: the path itself does (checked just
		// above by checkPrecondition).
	default:
		return fmt.Errorf("%w: %s has kind %q", ErrUnsupportedOperation, f.Path, f.Kind)
	}

	if err := p.hook(stepFor(p.op.ID, f, StageBeforeReplace)); err != nil {
		return err
	}
	if err := p.target.checkRealAncestors(f.Path, true); err != nil {
		return err
	}
	switch f.Kind {
	case KindCreate, KindModify:
		if err := p.replaceFromBlob(ctx, f, local); err != nil {
			return err
		}
	case KindDelete:
		if err := p.fs.removeFile(local); err != nil {
			return err
		}
	}
	if err := p.hook(stepFor(p.op.ID, f, StageAfterReplace)); err != nil {
		return err
	}
	return p.hook(stepFor(p.op.ID, f, StageBeforeMark))
}

// markWritten is the journal half of a content step. It is separate from
// contentStep so that the moment between "the disk changed" and "the journal
// knows" has a name a test can hold on to (StageBeforeMark).
func (p *publisher) markWritten(ctx context.Context, f *MergeFile) error {
	return p.markFile(ctx, f, FileStatePending, FileStateWritten)
}

// createDirs creates the directories this file needs, deepest last (the list is
// parents first). A directory that appeared in the meantime is not an error:
// the plan is a plan, and "it is already there" is the outcome it wanted.
func (p *publisher) createDirs(f *MergeFile) error {
	for _, rel := range f.CreatedDirs {
		local, err := targetPath(p.in.TargetRoot, rel)
		if err != nil {
			return err
		}
		if err := p.target.checkRealAncestors(rel, true); err != nil {
			return err
		}
		if err := p.fs.mkdir(local, 0o755); err != nil {
			entry, statErr := p.fs.lstat(local)
			if statErr != nil {
				return err
			}
			if !entry.Exists || !entry.IsDir {
				return err
			}
		}
	}
	return nil
}

// replaceFromBlob streams a blob to a staging file in the target's directory
// and replaces the target with it.
//
// The bytes are verified while they stream: BlobStore.Open is required to
// return content that hashes to the requested value, and the hash is recomputed
// here before the staging file is published, so a corrupted blob fails the
// write instead of putting wrong bytes in a user's file. The staging file is
// removed on every failure path — a leftover `.codeflow-merge-*` file in a
// user's directory would be litter this operation has no right to leave.
func (p *publisher) replaceFromBlob(ctx context.Context, f *MergeFile, local string) error {
	reader, err := p.in.Blobs.Open(*f.NewHash)
	if err != nil {
		return fmt.Errorf("merge: open new content of %s: %w", f.Path, err)
	}
	defer reader.Close()

	dir := filepath.Dir(local)
	tmp, err := p.fs.createTemp(dir, ".codeflow-merge-*.tmp")
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

	sum, size, err := copyAndHash(tmp, reader, ctx)
	if err != nil {
		return fmt.Errorf("merge: write %s: %w", f.Path, err)
	}
	if sum != *f.NewHash || size != contentSize(p.in.Candidate, f.Path) {
		return fmt.Errorf("%w: the stored content of %s is %s (%d bytes), the candidate says %s (%d bytes)",
			ErrResultChangedAfterCapture, f.Path, sum, size, *f.NewHash, contentSize(p.in.Candidate, f.Path))
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("merge: sync %s: %w", f.Path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("merge: close staging file for %s: %w", f.Path, err)
	}

	perm := os.FileMode(0o644)
	if f.NewMode != nil {
		perm = os.FileMode(*f.NewMode)
	}
	if err := p.fs.replaceFile(tmpPath, local, perm); err != nil {
		return err
	}
	published = true
	syncDir(dir)
	return nil
}

func (p *publisher) markFile(ctx context.Context, f *MergeFile, expected, next string) error {
	err := p.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		return MarkFileTx(ctx, tx, p.op.ID, f.Seq, expected, next, p.clock())
	})
	if err != nil {
		return err
	}
	f.State = next
	return nil
}

// --- (f) verify and complete ---

// verifyTarget re-reads every path and compares it with the candidate.
//
// It is the second half of "全部发布并核对目标" (§19.3 item 4): the writes
// happened outside any transaction, so nothing but this pass can say the tree
// is what merge.completed is about to claim. A path that does not match — an
// external editor that got there first, a delete that was recreated — fails the
// publish, and the failure takes the rollback path.
func (p *publisher) verifyTarget(ctx context.Context) error {
	for i := range p.files {
		f := &p.files[i]
		if err := ctx.Err(); err != nil {
			return err
		}
		local, err := targetPath(p.in.TargetRoot, f.Path)
		if err != nil {
			return err
		}
		entry, err := p.fs.lstat(local)
		if err != nil {
			return err
		}
		if f.Kind == KindDelete {
			if entry.Exists {
				return fmt.Errorf("%w: %s should be deleted but is %s",
					ErrTargetChanged, f.Path, entryWord(entry))
			}
			continue
		}
		if !entry.Exists || !entry.IsRegular {
			return fmt.Errorf("%w: %s should hold %s but is %s",
				ErrTargetChanged, f.Path, *f.NewHash, entryWord(entry))
		}
		hash, size, err := p.fs.hashFile(local)
		if err != nil {
			return err
		}
		if hash != *f.NewHash || size != contentSize(p.in.Candidate, f.Path) {
			return fmt.Errorf("%w: %s holds %s (%d bytes), the candidate says %s (%d bytes)",
				ErrTargetChanged, f.Path, hash, size, *f.NewHash, contentSize(p.in.Candidate, f.Path))
		}
	}
	return nil
}

// commitApplied writes the completion in one transaction: the operation moves
// to applied, merge.completed is appended, and the event's outbox rows are
// queued by the same AppendEventTx call (§19.3 item 4). A failure rolls the
// whole thing back — there is no state in which the tree is merged and the
// event is missing, or the reverse.
func (p *publisher) commitApplied(ctx context.Context) (PublishResult, error) {
	at := p.clock()
	payload, err := json.Marshal(map[string]any{
		"operation_id":   p.op.ID,
		"candidate_hash": p.op.CandidateHash,
		"file_count":     len(p.files),
		"fence":          p.op.Fence,
	})
	if err != nil {
		return p.rollback(ctx, fmt.Errorf("merge: encode merge.completed payload: %w", err))
	}
	runID := p.in.RunID
	identity, err := json.Marshal(run.ExecutionIdentity{
		ProjectID: p.in.ProjectID,
		RunID:     &runID,
		Actor:     run.Actor{Type: run.ActorTypeSystem, ID: mergeActorID},
	})
	if err != nil {
		return p.rollback(ctx, fmt.Errorf("merge: encode merge.completed identity: %w", err))
	}

	var (
		op    MergeOperation
		event runstore.Event
	)
	err = p.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		var err error
		op, err = UpdateOperationStatusCAS(ctx, tx, p.op, StatusApplying, StatusApplied, nil, at)
		if err != nil {
			return err
		}
		// The event is appended after the status move and inside the same
		// transaction, so the injected failure below (StageBeforeEvent) aborts
		// both: the row stays applying and the caller rolls back. That is the
		// observable form of "applied and merge.completed are one transaction".
		if err := p.hook(Step{OperationID: p.op.ID, Stage: StageBeforeEvent}); err != nil {
			return err
		}
		event, err = runstore.AppendEventTx(ctx, tx, runstore.EventInput{
			ProjectID:    p.in.ProjectID,
			RunID:        &runID,
			Type:         string(run.EventMergeCompleted),
			OccurredAt:   at,
			Identity:     identity,
			Payload:      payload,
			Destinations: p.in.Destinations,
		})
		return err
	})
	if err != nil {
		return p.rollback(ctx, fmt.Errorf("merge: complete operation %s: %w", p.op.ID, err))
	}
	p.op = op
	return PublishResult{Operation: op, Files: p.files, LeftoverDirs: p.target.leftover, Event: &event}, nil
}

// mergeActorID is the actor of the merge.completed event. The publisher runs
// inside the server, not inside the agent backend whose result it publishes, so
// the actor is the system — the same choice runstore.TransitionRunTx makes for
// its own server-decided events.
const mergeActorID = "merge-publisher"

// --- rollback ---

// rollback undoes the writes this operation made, in reverse publish order.
//
// The rule (§27.5 item 4, last sentence) is narrow on purpose: only a file this
// operation *wrote* is a candidate for restoring, and only while the path still
// holds exactly what this operation put there.
//
// Which files were written is decided from the content on disk, not from the
// journal alone. A row in `written` was certainly written; a row in `pending`
// may be either "not reached" or "the content step happened and its mark did
// not" (StageBeforeMark), and in the second case the path holds exactly the new
// content. So a pending row whose path already holds the new content is treated
// as written — and, symmetrically, a pending create whose directories were made
// but whose file was never replaced is still this operation's to clean up,
// because it may have created the directories.
//
//   - the path holds this operation's new content (or, for a delete, is still
//     absent) → the old content is restored from the backup blob (a create is
//     removed). The directories this operation created are removed afterwards,
//     deepest first, empty ones only;
//   - the path holds anything else, and the row was written → somebody edited it
//     after the write. The file is marked external_edit, the rollback stops, and
//     the operation becomes needs_recovery, which keeps the root lock. Not one
//     byte of that file is touched: the user's version is newer than both this
//     operation's and its backup, and choosing between them is a human's
//     decision;
//   - the path holds anything else, and the row was still pending → the content
//     step never happened (or the path moved on under its own power in the
//     window); the row is left as it is, but not before the directories this
//     operation recorded for that path are swept, because it may have made them
//     just before failing.
//
// A rollback never touches the content of a path the operation did not write,
// never removes a directory that is not empty, and never restores "the whole
// backup": there is no such thing here, only per-file content that was read from
// the path it is being written back to.
func (p *publisher) rollback(ctx context.Context, cause error) (PublishResult, error) {
	rollbackCtx := context.WithoutCancel(ctx)
	if err := p.beginRollback(rollbackCtx); err != nil {
		return p.result(ctx, fmt.Errorf("%w (and the rollback could not be recorded: %v)", cause, err))
	}

	for i := len(p.files) - 1; i >= 0; i-- {
		f := &p.files[i]
		// A link above the path means neither "is it still ours" nor "put it
		// back" can be answered without following it out of the root: park.
		if err := p.target.checkRealAncestors(f.Path, false); err != nil {
			return p.needsRecovery(rollbackCtx, fmt.Errorf("%w: %v (cause: %v)", ErrNeedsRecovery, err, cause))
		}
		ours, err := p.holdsOurWrite(f)
		if err != nil {
			return p.needsRecovery(rollbackCtx, fmt.Errorf("%w: %v", cause, err))
		}
		if !ours {
			if f.State != FileStateWritten {
				// Never marked written and the content is not ours: the content
				// step either never happened or wrote nothing durable. Leave every
				// byte of the path alone; only sweep directories this operation
				// might have created for it.
				if err := p.target.sweepCreatedDirs(rollbackCtx, f); err != nil {
					return p.needsRecovery(rollbackCtx, fmt.Errorf("%w: %v (cause: %v)", ErrNeedsRecovery, err, cause))
				}
				continue
			}
			if err := p.markFile(rollbackCtx, f, FileStateWritten, FileStateExternalEdit); err != nil {
				return p.needsRecovery(rollbackCtx, fmt.Errorf("%w: %v", cause, err))
			}
			return p.needsRecovery(rollbackCtx,
				fmt.Errorf("%w: %s was edited after it was written (cause: %v)", ErrNeedsRecovery, f.Path, cause))
		}
		if err := p.target.restoreFile(rollbackCtx, f); err != nil {
			return p.needsRecovery(rollbackCtx, fmt.Errorf("%w: %v (cause: %v)", ErrNeedsRecovery, err, cause))
		}
		if f.State == FileStateWritten {
			if err := p.markFile(rollbackCtx, f, FileStateWritten, FileStateRestored); err != nil {
				return p.needsRecovery(rollbackCtx, fmt.Errorf("%w: %v (cause: %v)", ErrNeedsRecovery, err, cause))
			}
		}
	}

	code := failureCodeFor(cause)
	if err := p.finishRollback(rollbackCtx, StatusRolledBack, code); err != nil {
		return p.result(ctx, fmt.Errorf("%w (and the rollback could not be recorded: %v)", cause, err))
	}
	return p.result(ctx, cause)
}

// beginRollback moves the operation to rolling_back, which is also how a second
// publisher or a recoverer sees that a rollback is in progress rather than a
// half-finished apply.
func (p *publisher) beginRollback(ctx context.Context) error {
	err := p.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		op, err := UpdateOperationStatusCAS(ctx, tx, p.op, StatusApplying, StatusRollingBack, nil, p.clock())
		if err != nil {
			return err
		}
		p.op = op
		return nil
	})
	return err
}

// holdsOurWrite reports whether the target still holds exactly what this
// operation wrote for f.
func (p *publisher) holdsOurWrite(f *MergeFile) (bool, error) {
	local, err := targetPath(p.in.TargetRoot, f.Path)
	if err != nil {
		return false, err
	}
	entry, err := p.fs.lstat(local)
	if err != nil {
		return false, err
	}
	if f.Kind == KindDelete {
		// This operation deleted the path; "still ours" means it is still gone.
		return !entry.Exists, nil
	}
	if !entry.Exists || !entry.IsRegular {
		return false, nil
	}
	hash, size, err := p.fs.hashFile(local)
	if err != nil {
		return false, err
	}
	return hash == *f.NewHash && size == contentSize(p.in.Candidate, f.Path), nil
}

// needsRecovery parks the operation where only an explicit decision can move
// it. The root lock stays held (needs_recovery is one of the blocking states),
// which is what stops a second publish from writing over a tree that is half
// this operation's and half the user's.
func (p *publisher) needsRecovery(ctx context.Context, cause error) (PublishResult, error) {
	code := "needs_recovery"
	if err := p.finishRollback(ctx, StatusNeedsRecovery, code); err != nil {
		return p.result(ctx, fmt.Errorf("%w (and the journal could not record it: %v)", cause, err))
	}
	return p.result(ctx, cause)
}

// finishRollback records the end of a rollback. The expected state is whatever
// the row is in now — rolling_back after beginRollback — and the CAS keeps a
// racing recoverer from having its own conclusion overwritten.
func (p *publisher) finishRollback(ctx context.Context, status, code string) error {
	err := p.in.Store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		op, err := UpdateOperationStatusCAS(ctx, tx, p.op, p.op.Status, status, &code, p.clock())
		if err != nil {
			return err
		}
		p.op = op
		return nil
	})
	return err
}

// failureCodeFor puts the cause's classification in the journal. It is free
// text (the column is), and it is short because its reader is a human looking
// at a failed merge in a UI.
func failureCodeFor(cause error) string {
	switch {
	case errors.Is(cause, ErrTargetChanged):
		return "base_changed"
	case errors.Is(cause, ErrResultChangedAfterCapture):
		return "result_changed"
	case errors.Is(cause, ErrOperationMoved):
		return "operation_taken_over"
	case errors.Is(cause, context.Canceled), errors.Is(cause, context.DeadlineExceeded):
		return "cancelled"
	default:
		return "publish_failed"
	}
}

// result reads the journal back and returns it with the supplied error. It is
// how every failure path reports: the caller gets the rows as they stand, not a
// guess about them.
func (p *publisher) result(ctx context.Context, cause error) (PublishResult, error) {
	readCtx := context.WithoutCancel(ctx)
	out := PublishResult{}
	if op, err := GetOperation(readCtx, p.in.Store.DB(), p.in.OperationID); err == nil {
		out.Operation = op
	} else {
		out.Operation = p.op
	}
	if files, err := ListFiles(readCtx, p.in.Store.DB(), p.in.OperationID); err == nil {
		out.Files = files
	} else {
		out.Files = p.files
	}
	out.LeftoverDirs = p.target.leftover
	return out, cause
}

// --- small helpers ---

func (p *publisher) hook(step Step) error { return p.in.Hook.call(step) }

func stepFor(operationID string, f *MergeFile, stage string) Step {
	return Step{OperationID: operationID, Seq: f.Seq, Path: f.Path, Kind: f.Kind, Stage: stage}
}

// copyAndHash copies source to dest while hashing what was copied, and stops at
// the first canceled context check the copy makes.
func copyAndHash(dst io.Writer, src io.Reader, ctx context.Context) (string, int64, error) {
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, sum), &contextReader{ctx: ctx, r: src})
	if err != nil {
		return "", n, err
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), n, nil
}

// discardStaging removes a staging file this operation created and could not
// publish. Production passes a path inside the target root, so this is the one
// place the publisher deletes something in the user's directory that it made
// itself; the caller has already closed the handle. A failure is not reported:
// the staging file is this operation's own litter and there is nothing a caller
// could do about it that the publish's own error does not already say.
func discardStaging(path string) error {
	return os.Remove(path)
}

// contextReader stops a long copy when the caller's context is done, so a
// canceled publish does not finish streaming a large blob before it notices.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// contentSize returns the size the candidate recorded for a path's new content,
// so a verify pass can compare sizes as well as hashes. A path the candidate
// does not describe has size 0, which cannot match a real file's hash anyway.
func contentSize(c *Candidate, path string) int64 {
	for _, op := range c.Operations {
		if op.Path == path {
			return op.Size
		}
	}
	return 0
}

// entryWord names an fsEntry for an error message without leaking a symbolic
// mode string that differs between platforms.
func entryWord(e fsEntry) string {
	switch {
	case !e.Exists:
		return "missing"
	case e.IsDir:
		return "a directory"
	case e.IsSymlink:
		return "a symlink"
	case e.IsRegular:
		return "a file"
	default:
		return "not a regular file"
	}
}
