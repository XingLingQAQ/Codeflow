package merge

// Test added by the main agent's acceptance review of T1.09.b group 4: no test
// pinned the recovery's first guard, the root identity check. A target
// directory that was replaced (moved away, a new directory created at the same
// path) is not the directory the journal describes; a recovery that went ahead
// would restore backups into, or delete files from, a directory this operation
// never touched.

import (
	"errors"
	"os"
	"testing"

	"github.com/codeflow/backend/internal/run"
)

func TestRecoverRefusesAReplacedRoot(t *testing.T) {
	f := newRecoverFixture(t, []recoverFileSpec{
		{rel: "a.txt", kind: KindModify, old: "old a", neu: "new a"},
	})
	// The crash: a.txt was written and marked, the operation is still applying,
	// so a recovery of the real root would restore the old content.
	f.writeNew("a.txt")
	f.mark(1, FileStatePending, FileStateWritten)

	// The user (or a tool) replaced the directory: same path, new directory,
	// holding the same bytes a.txt had after the write.
	moved := f.target + "-replaced-away"
	if err := os.Rename(f.target, moved); err != nil {
		t.Fatalf("move the target away: %v", err)
	}
	if err := os.MkdirAll(f.target, 0o755); err != nil {
		t.Fatalf("recreate the target path: %v", err)
	}
	writeTestFile(t, f.target, "a.txt", "new a")
	newDirBefore := snapshotTree(t, f.target)
	movedBefore := snapshotTree(t, moved)
	opBefore := f.op()

	_, err := f.recover()
	if !errors.Is(err, ErrRootIdentityChanged) {
		t.Fatalf("Recover on a replaced root: error = %v, want ErrRootIdentityChanged", err)
	}
	assertTreeEquals(t, "the new directory at the target path", newDirBefore, snapshotTree(t, f.target))
	assertTreeEquals(t, "the original directory, moved away", movedBefore, snapshotTree(t, moved))
	opAfter := f.op()
	if opAfter.Fence != opBefore.Fence || opAfter.Revision != opBefore.Revision || opAfter.Status != opBefore.Status {
		t.Fatalf("operation after the refusal = %s fence %d rev %d, want it untouched (%s fence %d rev %d)",
			opAfter.Status, opAfter.Fence, opAfter.Revision, opBefore.Status, opBefore.Fence, opBefore.Revision)
	}
}

// TestRecoverRollbackMarksARestoreWhoseMarkWasSwallowed: a rollback put a
// written file's old bytes back and crashed before recording it, so the row
// still says written over the pre-publish content. Finishing the rollback must
// record the restore — a rolled_back operation whose journal still claims a
// written file describes a tree that does not exist. Added by the main agent's
// acceptance review of T1.09.c: dropping that mark survived the whole package.
func TestRecoverRollbackMarksARestoreWhoseMarkWasSwallowed(t *testing.T) {
	specs := []recoverFileSpec{
		{rel: "mod.txt", kind: KindModify, old: "old mod", neu: "new mod"},
		{rel: "sub/new.txt", kind: KindCreate, neu: "created"},
		{rel: "gone.txt", kind: KindDelete, old: "bye"},
	}
	for _, status := range []string{StatusApplying, StatusRollingBack} {
		t.Run(status, func(t *testing.T) {
			f := newRecoverFixture(t, specs)
			f.writeNew("mod.txt")
			f.mark(1, FileStatePending, FileStateWritten)
			if status == StatusRollingBack {
				f.status(StatusApplying, StatusRollingBack)
			}
			// The restore reached the disk; its mark did not.
			f.writeRaw("mod.txt", "old mod")

			res, err := f.recover()
			if err != nil {
				t.Fatalf("Recover: %v (report %+v)", err, res)
			}
			if res.Outcome != RecoverOutcomeRolledBack {
				t.Fatalf("outcome = %s, want rolled_back", res.Outcome)
			}
			if op := f.op(); op.Status != StatusRolledBack {
				t.Fatalf("operation status = %s, want rolled_back", op.Status)
			}
			if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 0 {
				t.Fatalf("merge.completed events = %d, want 0", n)
			}
			f.assertTree("the target after the recovery")
			f.assertUnlocked()

			for _, row := range f.rows() {
				if row.State == FileStateWritten {
					t.Errorf("%s (seq %d) is still marked written after the rollback finished", row.Path, row.Seq)
				}
				if row.Path == "mod.txt" && row.State != FileStateRestored {
					t.Errorf("mod.txt state = %s, want %s", row.State, FileStateRestored)
				}
			}
		})
	}
}
