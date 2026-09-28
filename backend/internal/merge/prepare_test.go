package merge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
	"github.com/codeflow/backend/internal/runworkspace"
)

// These tests drive Prepare over real temp directories and real captures: the
// manifests come from runworkspace.Capture, so the paths, types, sizes, and
// hashes are the ones production produces.

// TestPrepareEmptyChangeSet covers the quiet outcome: the run copied the
// baseline and changed nothing, while the user kept editing. Nothing may be
// published, and the user's parallel work must not be reported as a merge.
func TestPrepareEmptyChangeSet(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "A\n")
	writeTestFile(t, root, "dir/b.txt", "B\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)

	// The working copy must carry the runworkspace ownership marker, which is
	// CodeFlow's own bookkeeping and never project content.
	if _, err := os.Stat(filepath.Join(wc.Path, runworkspace.OwnerMarkerName())); err != nil {
		t.Fatalf("fixture: the working copy has no ownership marker: %v", err)
	}

	writeTestFile(t, root, "a.txt", "A plus a user edit\n")
	writeTestFile(t, root, "untracked.txt", "U\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	blobs := newRecordingBlobs()
	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: blobs,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(c.Operations) != 0 {
		t.Fatalf("a run that changed nothing produced operations: %+v", c.Operations)
	}
	if len(c.Conflicts) != 0 {
		t.Fatalf("a run that changed nothing produced conflicts: %+v", c.Conflicts)
	}
	if len(c.AlreadyApplied) != 0 {
		t.Fatalf("a run that changed nothing produced already-applied marks: %+v", c.AlreadyApplied)
	}
	if c.Publishable() {
		t.Fatal("a candidate with nothing to do must not be publishable")
	}
	if blobs.count() != 0 {
		t.Fatalf("no operation was planned but %d blobs were stored", blobs.count())
	}
	// The user's parallel edit and untracked file are not changes of ours, and
	// neither is the ownership marker.
	noOperationFor(t, c, "a.txt")
	noOperationFor(t, c, "untracked.txt")
	noOperationFor(t, c, runworkspace.OwnerMarkerName())
}

// TestPrepareCreateModifyDelete is the core change set: a file the run added,
// a file it changed, and a file it removed, applied onto a target that still
// holds the base content.
func TestPrepareCreateModifyDelete(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "modify.txt", "before\n")
	writeTestFile(t, root, "delete.txt", "gone\n")
	writeTestFile(t, root, "keep.txt", "keep\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "create.txt", "created by the run\n")
	writeTestFile(t, wc.Path, "modify.txt", "after\n")
	removeTestFile(t, wc.Path, "delete.txt")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	targetBefore := treeFingerprint(t, root)
	resultBefore := treeFingerprint(t, wc.Path)

	blobs := newRecordingBlobs()
	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: blobs,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(c.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", c.Conflicts)
	}
	if !c.Publishable() {
		t.Fatal("a clean change set must be publishable")
	}

	create := findOperation(t, c, "create.txt")
	if create.Kind != KindCreate || create.OldType != "" || create.OldHash != "" {
		t.Fatalf("create operation = %+v", create)
	}
	if create.NewType != runworkspace.TypeFile || create.NewHash != entryHash(t, result, "create.txt") {
		t.Fatalf("create operation does not carry the result hash: %+v", create)
	}
	if got, ok := blobs.content(create.NewHash); !ok || got != "created by the run\n" {
		t.Fatalf("stored blob = %q (present %v), want the run's new file", got, ok)
	}

	modify := findOperation(t, c, "modify.txt")
	if modify.Kind != KindModify {
		t.Fatalf("modify operation = %+v", modify)
	}
	if modify.OldHash != entryHash(t, base, "modify.txt") || modify.OldType != runworkspace.TypeFile {
		t.Fatalf("modify old side must be the base value: %+v", modify)
	}
	if modify.NewHash != entryHash(t, result, "modify.txt") {
		t.Fatalf("modify new side must be the result value: %+v", modify)
	}
	if got, _ := blobs.content(modify.NewHash); got != "after\n" {
		t.Fatalf("stored blob = %q, want %q", got, "after\n")
	}
	if modify.Size != int64(len("after\n")) {
		t.Fatalf("modify size = %d, want %d", modify.Size, len("after\n"))
	}

	del := findOperation(t, c, "delete.txt")
	if del.Kind != KindDelete || del.NewType != "" || del.NewHash != "" || del.Size != 0 {
		t.Fatalf("delete operation = %+v", del)
	}
	if del.OldHash != entryHash(t, base, "delete.txt") {
		t.Fatalf("delete old side must be the base value: %+v", del)
	}

	noOperationFor(t, c, "keep.txt")
	noOperationFor(t, c, runworkspace.OwnerMarkerName())

	// Prepare is read-only in both directions.
	assertSameFingerprint(t, "target", targetBefore, treeFingerprint(t, root))
	assertSameFingerprint(t, "working copy", resultBefore, treeFingerprint(t, wc.Path))

	// Every operation is sorted by path, which is part of the canonical form.
	for i := 1; i < len(c.Operations); i++ {
		if c.Operations[i-1].Path > c.Operations[i].Path {
			t.Fatalf("operations are not sorted by path: %+v", c.Operations)
		}
	}
}

// TestPrepareRenamePair proves that a delete and a create of the same content
// are reported as one rename, with both operations kept: the publisher still
// has to remove one path and add the other.
func TestPrepareRenamePair(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "old/name.txt", "the same content\n")
	writeTestFile(t, root, "stays.txt", "stays\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	removeTestFile(t, wc.Path, "old/name.txt")
	writeTestFile(t, wc.Path, "new/name.txt", "the same content\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	blobs := newRecordingBlobs()
	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: blobs,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(c.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", c.Conflicts)
	}

	create := findOperation(t, c, "new/name.txt")
	del := findOperation(t, c, "old/name.txt")
	if create.Kind != KindCreate || del.Kind != KindDelete {
		t.Fatalf("rename produced %+v and %+v", create, del)
	}
	if create.RenamedFrom != "old/name.txt" {
		t.Fatalf("create.RenamedFrom = %q, want old/name.txt", create.RenamedFrom)
	}
	if del.RenamedTo != "new/name.txt" {
		t.Fatalf("delete.RenamedTo = %q, want new/name.txt", del.RenamedTo)
	}
	if create.NewHash != del.OldHash {
		t.Fatalf("both halves of a rename must refer to one hash: %s vs %s", create.NewHash, del.OldHash)
	}
	if got, ok := blobs.content(create.NewHash); !ok || got != "the same content\n" {
		t.Fatalf("the renamed content was not stored: %q (present %v)", got, ok)
	}
	noOperationFor(t, c, "stays.txt")
}

// TestPrepareBinaryFlag proves the binary marker is decided from the new
// content itself, not from a file extension, and only within the sniff window.
func TestPrepareBinaryFlag(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "plain.txt", "text\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	// A NUL in the first bytes of a file whose name says nothing binary.
	writeTestFile(t, wc.Path, "plain.txt", "PNG\x00\x01\x02binary")
	writeTestFile(t, wc.Path, "large.bin.txt", strings.Repeat("x", 9000)+"\x00tail")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if op := findOperation(t, c, "plain.txt"); !op.Binary {
		t.Fatalf("a NUL in the new content must set Binary: %+v", op)
	}
	// The NUL sits past the sniff window, so this file is not flagged.
	if op := findOperation(t, c, "large.bin.txt"); op.Binary {
		t.Fatalf("a NUL after the sniff window must not set Binary: %+v", op)
	}
}

// TestPrepareDirtyBaselineSurvives: the base was captured with a dirty edit,
// and the run did not touch that path. The user's dirty content must stay
// exactly as it is, with no operation and no conflict.
func TestPrepareDirtyBaselineSurvives(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "dirty.txt", "committed\n")
	writeTestFile(t, root, "clean.txt", "clean\n")

	// The user edits the file before the run starts: the baseline captures the
	// dirty state, so the working copy starts from the dirty content.
	writeTestFile(t, root, "dirty.txt", "dirty at capture time\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	if got, err := os.ReadFile(filepath.Join(wc.Path, "dirty.txt")); err != nil || string(got) != "dirty at capture time\n" {
		t.Fatalf("the working copy must start from the dirty baseline: %q, %v", got, err)
	}

	// The user keeps editing the same file while the run works.
	writeTestFile(t, root, "dirty.txt", "dirty again, later\n")
	target := capturePlain(t, root)
	result := capturePlain(t, wc.Path)

	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	noOperationFor(t, c, "dirty.txt")
	noOperationFor(t, c, "clean.txt")
	if len(c.Conflicts) != 0 {
		t.Fatalf("a dirty baseline the run did not touch must produce no conflict: %+v", c.Conflicts)
	}
}

// TestPrepareUserEditedAnotherFile: the user edited a file the run never
// touched. The run's own change publishes and the user's file is left alone.
func TestPrepareUserEditedAnotherFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "user.txt", "user v1\n")
	writeTestFile(t, root, "run.txt", "run v1\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "run.txt", "run v2\n")

	writeTestFile(t, root, "user.txt", "user v2, edited while the run worked\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	blobs := newRecordingBlobs()
	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: blobs,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(c.Conflicts) != 0 {
		t.Fatalf("a parallel edit at another path is not a conflict: %+v", c.Conflicts)
	}
	if op := findOperation(t, c, "run.txt"); op.Kind != KindModify {
		t.Fatalf("run.txt operation = %+v", op)
	}
	noOperationFor(t, c, "user.txt")
	if _, ok := blobs.content(entryHash(t, result, "run.txt")); !ok {
		t.Fatal("the run's new content was not stored")
	}
}

// TestPrepareConflictWhenTargetChanged: the user edited the very file the run
// changed. The run's content must not be published over it, and the base is
// never silently replaced by the fresher content.
func TestPrepareConflictWhenTargetChanged(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "shared.txt", "v1\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "shared.txt", "run v2\n")

	writeTestFile(t, root, "shared.txt", "user v2\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	conflict := findConflict(t, c, "shared.txt")
	if conflict.Reason != ReasonTargetChanged {
		t.Fatalf("Reason = %q, want %q", conflict.Reason, ReasonTargetChanged)
	}
	if conflict.Base == nil || conflict.Base.ContentHash != entryHash(t, base, "shared.txt") || !conflict.Base.Exists {
		t.Fatalf("conflict base = %+v", conflict.Base)
	}
	if conflict.Result == nil || conflict.Result.ContentHash != entryHash(t, result, "shared.txt") {
		t.Fatalf("conflict result = %+v", conflict.Result)
	}
	if conflict.Target == nil || conflict.Target.ContentHash != entryHash(t, target, "shared.txt") {
		t.Fatalf("conflict target = %+v", conflict.Target)
	}
	if c.Publishable() {
		t.Fatal("a candidate with a conflict must not be publishable")
	}
}

// TestPrepareConflictWhenTargetDeletedTheFile: the user removed a file the run
// changed. Recreating it would undo the user's deletion.
func TestPrepareConflictWhenTargetDeletedTheFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "shared.txt", "v1\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "shared.txt", "run v2\n")

	removeTestFile(t, root, "shared.txt")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	conflict := findConflict(t, c, "shared.txt")
	if conflict.Reason != ReasonTargetChanged {
		t.Fatalf("Reason = %q, want %q", conflict.Reason, ReasonTargetChanged)
	}
	// A plain capture records a removal by not listing the path, so the target
	// side of the conflict is absent rather than a deleted entry.
	if conflict.Target != nil {
		t.Fatalf("a path the target no longer has must summarize as absent: %+v", conflict.Target)
	}
	if conflict.Base == nil || !conflict.Base.Exists {
		t.Fatalf("the base side must still be described: %+v", conflict.Base)
	}
	if conflict.Result == nil || conflict.Result.ContentHash != entryHash(t, result, "shared.txt") {
		t.Fatalf("the result side must still be described: %+v", conflict.Result)
	}
}

// TestPrepareUserCreatedDifferentContent: the user created a file at a path
// the run also created, with different content. Neither side may win silently.
func TestPrepareUserCreatedDifferentContent(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "keep.txt", "keep\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "new.txt", "the run's version\n")

	writeTestFile(t, root, "new.txt", "the user's version\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	conflict := findConflict(t, c, "new.txt")
	if conflict.Reason != ReasonTargetChanged {
		t.Fatalf("Reason = %q, want %q", conflict.Reason, ReasonTargetChanged)
	}
	if conflict.Base != nil {
		t.Fatalf("the path was in neither base nor... base must be absent here: %+v", conflict.Base)
	}
}

// TestPrepareAlreadyApplied: the user's tree already holds exactly what the
// run produced. There is nothing to publish, and the path is reported so a
// reader can see why the change set is empty.
func TestPrepareAlreadyApplied(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "shared.txt", "v1\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "shared.txt", "run v2\n")
	writeTestFile(t, root, "shared.txt", "run v2\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	blobs := newRecordingBlobs()
	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: blobs,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(c.Conflicts) != 0 {
		t.Fatalf("an already applied change is not a conflict: %+v", c.Conflicts)
	}
	if len(c.Operations) != 0 {
		t.Fatalf("an already applied change needs no operation: %+v", c.Operations)
	}
	if len(c.AlreadyApplied) != 1 || c.AlreadyApplied[0] != "shared.txt" {
		t.Fatalf("AlreadyApplied = %+v, want [shared.txt]", c.AlreadyApplied)
	}
	if c.Publishable() {
		t.Fatal("nothing left to publish")
	}
	if blobs.count() != 0 {
		t.Fatalf("an already applied change stored %d blobs", blobs.count())
	}
}

// TestPrepareResultExcludedSecret: the run created a secret file. The result
// capture excludes it, so there is no content hash to publish under; the
// candidate is blocked instead of publishing a file it cannot describe.
func TestPrepareResultExcludedSecret(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "code.txt", "v1\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "server.key", "PRIVATE KEY\n")

	result := capturePlain(t, wc.Path)
	if reason := exclusionReason(t, result, "server.key"); reason != runworkspace.ReasonSecret {
		t.Fatalf("fixture is wrong: server.key was excluded as %q", reason)
	}
	target := capturePlain(t, root)

	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	conflict := findConflict(t, c, "server.key")
	want := ReasonResultExcluded + ":" + runworkspace.ReasonSecret
	if conflict.Reason != want {
		t.Fatalf("Reason = %q, want %q", conflict.Reason, want)
	}
	if conflict.Result != nil {
		t.Fatalf("an excluded path has no result entry to summarize: %+v", conflict.Result)
	}
	if c.Publishable() {
		t.Fatal("a run that created a secret must not be publishable")
	}
}

// TestPrepareTargetExcludedOversize: an external process replaced a file the
// run changed with one the target capture refuses to describe. Its current
// content is unknown, so it cannot be compared and cannot be overwritten.
func TestPrepareTargetExcludedOversize(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "big.txt", "small\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "big.txt", "run v2\n")

	// An external editor writes a file past the capture limit.
	writeTestFile(t, root, "big.txt", strings.Repeat("x", int(runworkspace.DefaultMaxFileBytes)+1024))

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)
	if reason := exclusionReason(t, target, "big.txt"); reason != runworkspace.ReasonOversize {
		t.Fatalf("fixture is wrong: big.txt was excluded as %q", reason)
	}

	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	conflict := findConflict(t, c, "big.txt")
	want := ReasonTargetExcluded + ":" + runworkspace.ReasonOversize
	if conflict.Reason != want {
		t.Fatalf("Reason = %q, want %q", conflict.Reason, want)
	}
	if len(c.Operations) != 0 {
		t.Fatalf("an excluded target path produced an operation: %+v", c.Operations)
	}
}

// TestPrepareBaseExcludedResultExcluded: a path that both sides exclude is
// left alone. The run and the user both wrote to a secret; nothing about it
// may enter the candidate.
func TestPrepareBaseExcludedResultExcluded(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "code.txt", "v1\n")
	writeTestFile(t, root, ".env", "TOKEN=user\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	// The run writes its own secret, and the user changes theirs.
	writeTestFile(t, wc.Path, ".env", "TOKEN=run\n")
	writeTestFile(t, root, ".env", "TOKEN=user, changed\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)
	if reason := exclusionReason(t, result, ".env"); reason != runworkspace.ReasonSecret {
		t.Fatalf("fixture is wrong: the result capture excluded .env as %q", reason)
	}

	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	noOperationFor(t, c, ".env")
	if len(c.Conflicts) != 0 {
		t.Fatalf("a path both sides exclude is not a conflict: %+v", c.Conflicts)
	}
}

// TestPrepareResultChangedAfterCapture: an external process rewrites the
// working copy after the result was captured. The manifest no longer
// describes the tree, so no candidate may be produced from it.
func TestPrepareResultChangedAfterCapture(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "v1\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "file.txt", "run v2\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	// Same length, different bytes: only a content check catches this.
	writeTestFile(t, wc.Path, "file.txt", "run v3\n")

	blobs := newRecordingBlobs()
	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: blobs,
	})
	if !errors.Is(err, ErrResultChangedAfterCapture) {
		t.Fatalf("err = %v, want ErrResultChangedAfterCapture", err)
	}
	if c != nil {
		t.Fatalf("a failed prepare must not return a candidate: %+v", c)
	}

	// A removal after the capture is caught the same way.
	removeTestFile(t, wc.Path, "file.txt")
	if _, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: blobs,
	}); !errors.Is(err, ErrResultChangedAfterCapture) {
		t.Fatalf("err = %v, want ErrResultChangedAfterCapture for a removed file", err)
	}
}

// TestPrepareBlobWriterContract: the hash the store returns is checked against
// the manifest. A store that answers with a different hash cannot produce a
// candidate that points at content nobody can find, and a store that fails
// fails the whole prepare.
func TestPrepareBlobWriterContract(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "v1\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "file.txt", "run v2\n")
	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	t.Run("store returns a different hash", func(t *testing.T) {
		blobs := newRecordingBlobs()
		blobs.wrongHash = true
		c, err := Prepare(context.Background(), PrepareInput{
			Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: blobs,
		})
		if !errors.Is(err, ErrResultChangedAfterCapture) {
			t.Fatalf("err = %v, want ErrResultChangedAfterCapture", err)
		}
		if c != nil {
			t.Fatalf("a mismatched blob hash must not produce a candidate: %+v", c)
		}
	})

	t.Run("store fails", func(t *testing.T) {
		blobs := newRecordingBlobs()
		blobs.failOn = func(string) error { return errors.New("store is full") }
		c, err := Prepare(context.Background(), PrepareInput{
			Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: blobs,
		})
		if err == nil {
			t.Fatal("a failing store must fail the prepare")
		}
		if c != nil {
			t.Fatalf("a failing store produced a candidate: %+v", c)
		}
	})
}

// TestPrepareStoreRefusesUndescribedSources covers the read path's guards
// without needing a link privilege: each guard is the reason a link can never
// be followed into file content. Readlink on a regular file is refused by the
// OS, so the symlink branch has no fallback to file bytes; a directory, a
// missing file, and a shrunk file are all refused by the file branch.
func TestPrepareStoreRefusesUndescribedSources(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "content\n")
	writeTestFile(t, root, "dir/nested.txt", "nested\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	blobs := newRecordingBlobs()
	b := &builder{ctx: context.Background(), resultRoot: wc.Path, blobs: blobs}

	t.Run("symlink branch never reads file bytes", func(t *testing.T) {
		entry := runworkspace.Entry{
			Path: "file.txt", Type: runworkspace.TypeSymlink, Mode: 0o777,
			Size: 5, ContentHash: hashA,
		}
		if _, err := b.storeNewContent("file.txt", entry); !errors.Is(err, ErrResultChangedAfterCapture) {
			t.Fatalf("err = %v, want ErrResultChangedAfterCapture", err)
		}
		if blobs.count() != 0 {
			t.Fatalf("the file content was stored as if it were a link target: %d puts", blobs.count())
		}
	})

	t.Run("file branch refuses a directory", func(t *testing.T) {
		entry := runworkspace.Entry{Path: "dir", Type: runworkspace.TypeFile, Size: 0, ContentHash: hashA}
		if _, err := b.storeNewContent("dir", entry); !errors.Is(err, ErrResultChangedAfterCapture) {
			t.Fatalf("err = %v, want ErrResultChangedAfterCapture", err)
		}
	})

	t.Run("file branch refuses a missing file", func(t *testing.T) {
		entry := runworkspace.Entry{Path: "gone.txt", Type: runworkspace.TypeFile, Size: 1, ContentHash: hashA}
		if _, err := b.storeNewContent("gone.txt", entry); !errors.Is(err, ErrResultChangedAfterCapture) {
			t.Fatalf("err = %v, want ErrResultChangedAfterCapture", err)
		}
	})

	t.Run("file branch refuses a size that does not match", func(t *testing.T) {
		entry := runworkspace.Entry{Path: "file.txt", Type: runworkspace.TypeFile, Size: 3, ContentHash: hashA}
		if _, err := b.storeNewContent("file.txt", entry); !errors.Is(err, ErrResultChangedAfterCapture) {
			t.Fatalf("err = %v, want ErrResultChangedAfterCapture", err)
		}
	})

	t.Run("file branch refuses a path outside the result root", func(t *testing.T) {
		entry := runworkspace.Entry{Path: "../escape.txt", Type: runworkspace.TypeFile, Size: 1, ContentHash: hashA}
		if _, err := b.storeNewContent("../escape.txt", entry); !errors.Is(err, ErrUnsafeManifestPath) {
			t.Fatalf("err = %v, want ErrUnsafeManifestPath", err)
		}
	})
}

// TestPrepareVerifiesManifests: a manifest that does not describe its own
// entries, or that is out of order, cannot be the basis of a merge.
func TestPrepareVerifiesManifests(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "A\n")
	writeTestFile(t, root, "b.txt", "B\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "c.txt", "C\n")
	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	t.Run("tampered entry content hash", func(t *testing.T) {
		tampered := cloneManifest(t, target)
		tampered.Entries[0].ContentHash = hashC
		assertPrepareRejects(t, base, tampered, result, wc.Path, ErrManifestHashMismatch)
	})

	t.Run("tampered manifest hash", func(t *testing.T) {
		tampered := cloneManifest(t, base)
		tampered.Hash = hashC
		assertPrepareRejects(t, tampered, target, result, wc.Path, ErrManifestHashMismatch)
	})

	// The order checks run before the hash check, so a reordered or duplicated
	// entry list is reported as malformed even though its recorded hash is now
	// stale as well.
	t.Run("entries out of order", func(t *testing.T) {
		tampered := cloneManifest(t, result)
		tampered.Entries[0], tampered.Entries[1] = tampered.Entries[1], tampered.Entries[0]
		assertPrepareRejects(t, base, target, tampered, wc.Path, ErrManifestOrder)
	})

	t.Run("duplicate path", func(t *testing.T) {
		tampered := cloneManifest(t, result)
		tampered.Entries = append(tampered.Entries, tampered.Entries[len(tampered.Entries)-1])
		assertPrepareRejects(t, base, target, tampered, wc.Path, ErrManifestOrder)
	})

	t.Run("wrong format version", func(t *testing.T) {
		tampered := cloneManifest(t, base)
		tampered.FormatVersion = 2
		assertPrepareRejects(t, tampered, target, result, wc.Path, ErrManifestVersion)
	})
}

// TestPrepareRejectsMismatchedRootsAndModes: the three manifests must describe
// the same tree and compare in the same mode.
func TestPrepareRejectsMismatchedRootsAndModes(t *testing.T) {
	rootA := t.TempDir()
	writeTestFile(t, rootA, "a.txt", "A\n")
	base := capturePlain(t, rootA)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "b.txt", "B\n")
	result := capturePlain(t, wc.Path)

	other := t.TempDir()
	writeTestFile(t, other, "a.txt", "A\n")
	otherTarget := capturePlain(t, other)

	policytesting.AllowForTest(t, policy.OperationProcessStart)
	gitRoot, gm := newGitFixtureRepo(t)
	writeTestFile(t, gitRoot, "a.txt", "A\n")
	runGit(t, gitRoot, "add", ".")
	runGit(t, gitRoot, "commit", "-qm", "init")
	gitTarget := captureGitManifest(t, gm, gitRoot)

	cases := []struct {
		name string
		in   PrepareInput
		want error
	}{
		{
			name: "target root differs from base root",
			in:   PrepareInput{Base: base, Target: otherTarget, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs()},
			want: ErrRootMismatch,
		},
		{
			name: "result root differs from the result manifest root",
			in:   PrepareInput{Base: base, Target: base, Result: result, ResultRoot: other, Blobs: newRecordingBlobs()},
			want: ErrRootMismatch,
		},
		{
			name: "target mode differs from base mode",
			in:   PrepareInput{Base: base, Target: gitTarget, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs()},
			want: ErrModeMismatch,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Prepare(context.Background(), tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if c != nil {
				t.Fatalf("rejected input produced a candidate: %+v", c)
			}
		})
	}
}

func TestPrepareRejectsMissingInputs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "A\n")
	m := capturePlain(t, root)

	cases := []struct {
		name string
		in   PrepareInput
	}{
		{"no base", PrepareInput{Target: m, Result: m, ResultRoot: root, Blobs: newRecordingBlobs()}},
		{"no target", PrepareInput{Base: m, Result: m, ResultRoot: root, Blobs: newRecordingBlobs()}},
		{"no result", PrepareInput{Base: m, Target: m, ResultRoot: root, Blobs: newRecordingBlobs()}},
		{"no blob writer", PrepareInput{Base: m, Target: m, Result: m, ResultRoot: root}},
		{"no result root", PrepareInput{Base: m, Target: m, Result: m, Blobs: newRecordingBlobs()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Prepare(context.Background(), tc.in); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// TestPrepareGitModeRunsTheSameRules is the same change set in git mode. The
// mode only changes how paths are enumerated; the three-way comparison is
// identical.
//
// A git capture requires the root to be the work tree top level, so here the
// repository itself plays both roles: the committed state is what stands in
// for the frozen baseline, and the edited work tree is what stands in for the
// working copy, which is exactly the state Materialize builds by copying the
// baseline bytes. Each scenario captures its target and its result from the
// tree as it is at that moment.
func TestPrepareGitModeRunsTheSameRules(t *testing.T) {
	requireGitCLI(t)
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	root, gm := newGitFixtureRepo(t)

	baseState := map[string]string{"modify.txt": "before\n", "delete.txt": "gone\n"}
	runState := map[string]string{"modify.txt": "after\n", "create.txt": "created\n"}

	setGitWorkTree(t, root, baseState)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "init")
	base := captureGitManifest(t, gm, root)

	// Scenario 1: the run changed three paths while the user's tree stayed at
	// the baseline.
	setGitWorkTree(t, root, baseState)
	target := captureGitManifest(t, gm, root)
	setGitWorkTree(t, root, runState)
	result := captureGitManifest(t, gm, root)

	blobs := newRecordingBlobs()
	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: root, Blobs: blobs,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(c.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", c.Conflicts)
	}
	if got := findOperation(t, c, "create.txt").Kind; got != KindCreate {
		t.Fatalf("create.txt kind = %q", got)
	}
	if got := findOperation(t, c, "modify.txt").Kind; got != KindModify {
		t.Fatalf("modify.txt kind = %q", got)
	}
	if got := findOperation(t, c, "delete.txt").Kind; got != KindDelete {
		t.Fatalf("delete.txt kind = %q", got)
	}
	if result.Mode != runworkspace.ModeGit || target.Mode != runworkspace.ModeGit || c.Root != root {
		t.Fatalf("git-mode candidate = %+v (target mode %q)", c, target.Mode)
	}
	if c.BaseManifestHash != base.Hash || c.TargetManifestHash != target.Hash || c.ResultManifestHash != result.Hash {
		t.Fatalf("the candidate must carry all three manifest hashes: %+v", c)
	}
	if got, ok := blobs.content(entryHash(t, result, "create.txt")); !ok || got != "created\n" {
		t.Fatalf("stored blob = %q (present %v)", got, ok)
	}

	// Scenario 2: the user's tree already holds exactly the run's result.
	setGitWorkTree(t, root, runState)
	appliedTarget := captureGitManifest(t, gm, root)
	appliedResult := captureGitManifest(t, gm, root)
	c2, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: appliedTarget, Result: appliedResult, ResultRoot: root, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare against the applied target: %v", err)
	}
	if len(c2.Operations) != 0 {
		t.Fatalf("an already applied change needs no operation: %+v", c2.Operations)
	}
	wantApplied := []string{"create.txt", "delete.txt", "modify.txt"}
	if len(c2.AlreadyApplied) != len(wantApplied) {
		t.Fatalf("AlreadyApplied = %+v, want %+v", c2.AlreadyApplied, wantApplied)
	}
	for i, path := range wantApplied {
		if c2.AlreadyApplied[i] != path {
			t.Fatalf("AlreadyApplied = %+v, want %+v", c2.AlreadyApplied, wantApplied)
		}
	}
	if len(c2.Conflicts) != 0 {
		t.Fatalf("an already applied change is not a conflict: %+v", c2.Conflicts)
	}

	// Scenario 3: the user edited the same path in their own direction. A third
	// state is a conflict in git mode exactly as in plain mode.
	editedState := map[string]string{"modify.txt": "the user's own\n", "create.txt": "created\n"}
	setGitWorkTree(t, root, editedState)
	editedTarget := captureGitManifest(t, gm, root)
	setGitWorkTree(t, root, runState)
	editedResult := captureGitManifest(t, gm, root)
	c3, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: editedTarget, Result: editedResult, ResultRoot: root, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare against the edited target: %v", err)
	}
	if reason := findConflict(t, c3, "modify.txt").Reason; reason != ReasonTargetChanged {
		t.Fatalf("Reason = %q, want %q", reason, ReasonTargetChanged)
	}
}

// TestPrepareNeverFollowsAResultLink pins the read path's containment rule: a
// manifest path is resolved strictly inside the result root, so no manifest
// can point the reader at content outside it. The symlink shapes themselves
// are covered by the decision table above, which needs no link privilege; this
// test covers the guard that makes "never follow" hold on every platform.
func TestPrepareNeverFollowsAResultLink(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "target.txt", "content\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	blobs := newRecordingBlobs()
	b := &builder{ctx: context.Background(), resultRoot: filepath.Clean(wc.Path), blobs: blobs}

	link := runworkspace.Entry{
		Path: "link.txt", Type: runworkspace.TypeSymlink, Mode: 0o777,
		Size: 5, ContentHash: hashA,
	}

	// The link's name is looked up inside the result root and nowhere else.
	local, err := b.localPath(link.Path)
	if err != nil {
		t.Fatalf("localPath(%s) = %v", link.Path, err)
	}
	if want := filepath.Join(wc.Path, "link.txt"); local != want {
		t.Fatalf("localPath(%s) = %s, want %s", link.Path, local, want)
	}

	// A traversing or absolute path is refused before any read, so a manifest
	// cannot redirect the reader at a file outside the working copy.
	for _, bad := range []string{
		"../target.txt",
		"..\\target.txt",
		"nested/../../target.txt",
		"/target.txt",
		"c:/windows/system32/config/sam",
		"",
		".",
		"nested/./target.txt",
	} {
		if _, err := b.localPath(bad); !errors.Is(err, ErrUnsafeManifestPath) {
			t.Fatalf("localPath(%q) err = %v, want ErrUnsafeManifestPath", bad, err)
		}
	}
	if blobs.count() != 0 {
		t.Fatalf("a refused path still stored content: %d puts", blobs.count())
	}

	// The symlink read path has no file-bytes fallback: a path that is not a
	// link is refused rather than read as content.
	if err := b.storeSymlink("target.txt", filepath.Join(wc.Path, "target.txt"), link); !errors.Is(err, ErrResultChangedAfterCapture) {
		t.Fatalf("storeSymlink on a regular file err = %v, want ErrResultChangedAfterCapture", err)
	}
	if _, ok := blobs.content(hashOf([]byte("content\n"))); ok {
		t.Fatal("the file's bytes were stored as a link target")
	}
}

// TestPrepareVerifiesManifestsExclusionsAndOrder covers the exclusion list and
// the ordering edge cases the manifest hash alone does not describe.
func TestPrepareVerifiesManifestsExclusionsAndOrder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "A\n")
	writeTestFile(t, root, ".env", "TOKEN=1\n")
	writeTestFile(t, root, "b.pem", "KEY\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "c.txt", "C\n")
	result := capturePlain(t, wc.Path)
	target := base

	if len(target.Excluded) < 2 {
		t.Fatalf("fixture is wrong: expected at least two exclusions, got %+v", target.Excluded)
	}

	t.Run("unsorted exclusion list", func(t *testing.T) {
		tampered := cloneManifest(t, target)
		tampered.Excluded[0], tampered.Excluded[1] = tampered.Excluded[1], tampered.Excluded[0]
		assertPrepareRejects(t, base, tampered, result, wc.Path, ErrManifestOrder)
	})

	t.Run("duplicate exclusion path and reason", func(t *testing.T) {
		tampered := cloneManifest(t, target)
		tampered.Excluded = append(tampered.Excluded, tampered.Excluded[0])
		assertPrepareRejects(t, base, tampered, result, wc.Path, ErrManifestOrder)
	})

	t.Run("exclusion with an empty path", func(t *testing.T) {
		tampered := cloneManifest(t, target)
		tampered.Excluded = append([]runworkspace.Exclusion{{Path: "", Reason: "secret"}}, tampered.Excluded...)
		assertPrepareRejects(t, base, tampered, result, wc.Path, ErrManifestOrder)
	})

	t.Run("entry removed but hash left alone", func(t *testing.T) {
		tampered := cloneManifest(t, target)
		tampered.Entries = tampered.Entries[1:]
		assertPrepareRejects(t, base, tampered, result, wc.Path, ErrManifestHashMismatch)
	})
}

// TestPrepareHashDeterminism recomputes one candidate many times over the same
// on-disk fixture, with a fresh blob writer each time, and requires byte-equal
// candidates. Run with -count=20 for the strongest form; the loop covers the
// case in a single run too (ordering that came from map iteration would show
// up immediately).
func TestPrepareHashDeterminism(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "modify.txt", "before\n")
	writeTestFile(t, root, "delete.txt", "gone\n")
	writeTestFile(t, root, "rename-old.txt", "moved content\n")
	writeTestFile(t, root, "untouched.txt", "untouched\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "modify.txt", "after\n")
	writeTestFile(t, wc.Path, "create.txt", "created\n")
	writeTestFile(t, wc.Path, "create2.txt", "created\n") // same content as create.txt
	removeTestFile(t, wc.Path, "delete.txt")
	removeTestFile(t, wc.Path, "rename-old.txt")
	writeTestFile(t, wc.Path, "rename-new.txt", "moved content\n")

	// The rename candidates are deliberately given several equal-content paths
	// inside one rename group, and the user edits a path the run also changed,
	// so conflicts and operations are present in the same candidate.
	writeTestFile(t, root, "modify.txt", "the user's own\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	var first *Candidate
	for i := 0; i < 5; i++ {
		c, err := Prepare(context.Background(), PrepareInput{
			Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
		})
		if err != nil {
			t.Fatalf("run %d: prepare: %v", i, err)
		}
		if i == 0 {
			first = c
			continue
		}
		if c.Hash != first.Hash {
			t.Fatalf("run %d: candidate hash changed: %s vs %s", i, c.Hash, first.Hash)
		}
		if len(c.Operations) != len(first.Operations) {
			t.Fatalf("run %d: operation count changed: %d vs %d", i, len(c.Operations), len(first.Operations))
		}
		for j := range c.Operations {
			if c.Operations[j] != first.Operations[j] {
				t.Fatalf("run %d: operation %d changed:\n %+v\n %+v", i, j, c.Operations[j], first.Operations[j])
			}
		}
		if len(c.Conflicts) != len(first.Conflicts) {
			t.Fatalf("run %d: conflict count changed", i)
		}
	}

	// The fixture holds both a conflict (modify.txt) and a clean rename, so the
	// hash covers a candidate that has everything a real one can have.
	if len(first.Conflicts) != 1 || first.Conflicts[0].Path != "modify.txt" {
		t.Fatalf("fixture is wrong: conflicts = %+v", first.Conflicts)
	}
	for _, want := range []string{"rename-old.txt", "rename-new.txt"} {
		findOperation(t, first, want)
	}
	if first.Operations[0].RenamedFrom == "" && first.Operations[len(first.Operations)-1].RenamedTo == "" {
		t.Fatalf("the rename pair was not marked: %+v", first.Operations)
	}
}

// TestPrepareBlobContentFollowsTheResultManifest covers what happens when the
// working copy and the user's tree are the same directory — the arrangement a
// git-mode run with no working copy uses, where an edit made after the run is
// visible in both. The new content is read through the size the manifest
// recorded and verified against the hash it recorded, so live bytes that the
// manifest does not describe are refused rather than published.
func TestPrepareBlobContentFollowsTheResultManifest(t *testing.T) {
	runContent := "run content v2\n"
	externalEdit := "sneaky edit!!!\n"
	if len(externalEdit) != len(runContent) {
		t.Fatalf("fixture is wrong: the edit must keep the length so only a content check catches it")
	}

	t.Run("stale working copy is refused", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "file.txt", "v1\n")
		writeTestFile(t, root, "other.txt", "other\n")

		base := capturePlain(t, root)
		// The user's tree still holds the base content, so the change is
		// plannable.
		target := capturePlain(t, root)

		// The result manifest describes the run's edit ...
		writeTestFile(t, root, "file.txt", runContent)
		result := capturePlain(t, root)
		// ... and then the file is rewritten by something outside the run. The
		// manifest is now a false description of the working copy, so no
		// candidate may be built from it.
		writeTestFile(t, root, "file.txt", externalEdit)

		blobs := newRecordingBlobs()
		c, err := Prepare(context.Background(), PrepareInput{
			Base: base, Target: target, Result: result, ResultRoot: root, Blobs: blobs,
		})
		if !errors.Is(err, ErrResultChangedAfterCapture) {
			t.Fatalf("err = %v, want ErrResultChangedAfterCapture", err)
		}
		if c != nil {
			t.Fatalf("a stale working copy produced a candidate: %+v", c)
		}
		// The store hashes what it is handed, so the rejected bytes may pass
		// through it; what must never happen is a candidate that refers to
		// content nobody wrote. The run's own hash was never produced, so no
		// operation can name it.
		if _, ok := blobs.content(entryHash(t, result, "file.txt")); ok {
			t.Fatal("a blob was stored under the manifest hash without matching content")
		}
	})

	t.Run("a matching working copy stores exactly the recorded content", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "file.txt", "v1\n")
		writeTestFile(t, root, "other.txt", "other\n")

		base := capturePlain(t, root)
		target := capturePlain(t, root)
		writeTestFile(t, root, "file.txt", runContent)
		result := capturePlain(t, root)

		blobs := newRecordingBlobs()
		c, err := Prepare(context.Background(), PrepareInput{
			Base: base, Target: target, Result: result, ResultRoot: root, Blobs: blobs,
		})
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		op := findOperation(t, c, "file.txt")
		if op.NewHash != entryHash(t, result, "file.txt") {
			t.Fatalf("the operation does not name the manifest's hash: %+v", op)
		}
		if got, ok := blobs.content(op.NewHash); !ok || got != runContent {
			t.Fatalf("stored blob = %q (present %v), want %q", got, ok, runContent)
		}
		if op.Size != int64(len(runContent)) {
			t.Fatalf("operation size = %d, want %d", op.Size, len(runContent))
		}
	})

	t.Run("a path with a conflict is never read", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "file.txt", "v1\n")
		writeTestFile(t, root, "other.txt", "other\n")

		base := capturePlain(t, root)
		writeTestFile(t, root, "file.txt", runContent)
		result := capturePlain(t, root)
		// The user wrote their own content at the same path.
		writeTestFile(t, root, "file.txt", externalEdit)
		target := capturePlain(t, root)
		// The working copy is the same directory, so it now holds the user's
		// bytes as well.

		blobs := newRecordingBlobs()
		c, err := Prepare(context.Background(), PrepareInput{
			Base: base, Target: target, Result: result, ResultRoot: root, Blobs: blobs,
		})
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		if reason := findConflict(t, c, "file.txt").Reason; reason != ReasonTargetChanged {
			t.Fatalf("Reason = %q, want %q", reason, ReasonTargetChanged)
		}
		// The path decides against publishing before its content is ever
		// considered: a conflict means nothing about the path will be written,
		// so its stale working-copy bytes are never read into a blob.
		noOperationForBlob(t, blobs, hashOf([]byte(runContent)), hashOf([]byte(externalEdit)))
		if c.Publishable() {
			t.Fatal("a candidate with a conflict must not be publishable")
		}
	})
}

// TestPrepareSecondPassIsInert is the strongest form of the two-hash rule: a
// candidate is computed, then applied by hand exactly as its operations say —
// every file still matching its recorded OldHash — and Prepare is run again
// against the same base and result. The second pass must find nothing to do
// and must not propose re-applying the first change set, which is what would
// happen if "the target has changed since capture" were enough of a reason to
// publish.
func TestPrepareSecondPassIsInert(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "modify.txt", "before\n")
	writeTestFile(t, root, "delete.txt", "gone\n")
	writeTestFile(t, root, "rename-old.txt", "moved\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)
	writeTestFile(t, wc.Path, "modify.txt", "after\n")
	writeTestFile(t, wc.Path, "create.txt", "created\n")
	removeTestFile(t, wc.Path, "delete.txt")
	removeTestFile(t, wc.Path, "rename-old.txt")
	writeTestFile(t, wc.Path, "rename-new.txt", "moved\n")

	result := capturePlain(t, wc.Path)
	firstTarget := capturePlain(t, root)

	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: firstTarget, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !c.Publishable() {
		t.Fatalf("fixture is wrong: %+v", c)
	}

	// Apply the candidate by the recorded expectations only: every operation's
	// old side is checked against the bytes on disk before anything is touched.
	for _, op := range c.Operations {
		local := filepath.Join(root, filepath.FromSlash(op.Path))
		switch op.Kind {
		case KindDelete:
			data, err := os.ReadFile(local)
			if err != nil {
				t.Fatalf("delete precondition for %s: %v", op.Path, err)
			}
			if hashOf(data) != op.OldHash {
				t.Fatalf("delete precondition for %s: content is %s, want %s", op.Path, hashOf(data), op.OldHash)
			}
			removeTestFile(t, root, op.Path)
		case KindCreate, KindModify:
			if op.Kind == KindModify {
				data, err := os.ReadFile(local)
				if err != nil {
					t.Fatalf("modify precondition for %s: %v", op.Path, err)
				}
				if hashOf(data) != op.OldHash {
					t.Fatalf("modify precondition for %s: content is %s, want %s", op.Path, hashOf(data), op.OldHash)
				}
			}
			source, err := os.ReadFile(filepath.Join(wc.Path, filepath.FromSlash(op.Path)))
			if err != nil {
				t.Fatalf("read new content for %s: %v", op.Path, err)
			}
			if hashOf(source) != op.NewHash {
				t.Fatalf("new content for %s is %s, want %s", op.Path, hashOf(source), op.NewHash)
			}
			writeTestFile(t, root, op.Path, string(source))
		}
	}

	// The published tree must now hold everything the working copy holds. The
	// working copy also still carries the ownership marker, which is not
	// project content and is deliberately not compared through the hash.
	published := capturePlain(t, root)
	for _, want := range result.Entries {
		if want.Path == runworkspace.OwnerMarkerName() {
			continue
		}
		if got := entryHash(t, published, want.Path); got != want.ContentHash {
			t.Fatalf("after applying, %s is %s, want the result hash %s", want.Path, got, want.ContentHash)
		}
	}
	if len(published.Entries) != len(result.Entries)-1 {
		t.Fatalf("published tree has %d entries, want the result's %d minus the ownership marker",
			len(published.Entries), len(result.Entries))
	}

	second, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: published, Result: result, ResultRoot: wc.Path, Blobs: newRecordingBlobs(),
	})
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if len(second.Operations) != 0 {
		t.Fatalf("the second pass proposed operations: %+v", second.Operations)
	}
	if len(second.Conflicts) != 0 {
		t.Fatalf("the second pass found conflicts: %+v", second.Conflicts)
	}
	if len(second.AlreadyApplied) != len(c.Operations) {
		t.Fatalf("AlreadyApplied = %+v, want one per applied operation (%d)", second.AlreadyApplied, len(c.Operations))
	}
	if second.Publishable() {
		t.Fatal("a second pass over an applied tree must have nothing to publish")
	}
	// The second pass describes the same three trees, so it hashes the same.
	if second.BaseManifestHash != c.BaseManifestHash || second.ResultManifestHash != c.ResultManifestHash {
		t.Fatalf("the second pass used different inputs: %+v vs %+v", second, c)
	}
	if second.TargetManifestHash == c.TargetManifestHash {
		t.Fatal("fixture is wrong: applying the candidate left the target manifest identical")
	}
}

// noOperationForBlob asserts that none of the given contents reached the store
// for a path the candidate refused.
func noOperationForBlob(t *testing.T, blobs *recordingBlobs, hashes ...string) {
	t.Helper()
	for _, hash := range hashes {
		if _, ok := blobs.content(hash); ok {
			t.Fatalf("content %s reached the blob store for a path with no operation", hash)
		}
	}
}

// --- helpers for this file ---

// setGitWorkTree makes the repository work tree hold exactly the given files,
// removing whatever else is tracked at the top level. It is the git-mode
// equivalent of editing a materialized working copy.
func setGitWorkTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	out := runGit(t, root, "ls-files")
	for _, path := range strings.Split(strings.TrimSpace(out), "\n") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, keep := files[path]; keep {
			continue
		}
		// The path may already be gone: the run's own state has it removed.
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", path, err)
		}
	}
	for path, content := range files {
		writeTestFile(t, root, path, content)
	}
}

func cloneManifest(t *testing.T, m *runworkspace.Manifest) *runworkspace.Manifest {
	t.Helper()
	out := *m
	out.Entries = append([]runworkspace.Entry(nil), m.Entries...)
	out.Excluded = append([]runworkspace.Exclusion(nil), m.Excluded...)
	out.Rules = append([]string(nil), m.Rules...)
	return &out
}

func assertPrepareRejects(t *testing.T, base, target, result *runworkspace.Manifest, resultRoot string, want error) {
	t.Helper()
	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: resultRoot, Blobs: newRecordingBlobs(),
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if c != nil {
		t.Fatalf("rejected manifest produced a candidate: %+v", c)
	}
}
