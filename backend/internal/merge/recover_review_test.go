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
