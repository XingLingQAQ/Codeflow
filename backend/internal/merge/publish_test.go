package merge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/codeflow/backend/internal/runworkspace"
	"github.com/codeflow/backend/internal/workspace"
)

// The publish tests (T1.09.b group 3). The target root, the working copy and
// the database are all real: a publish is a sequence of writes to a directory,
// and the only way to test its ordering is to make it happen.

// publishFixture is one prepared publish: the database, the target directory,
// the working copy, the candidate between them, and the seams.
type publishFixture struct {
	t         *testing.T
	store     *runstore.Store
	blobs     *countingBlobs
	fs        *faultFS
	target    string
	wc        string
	root      workspace.RootIdentity
	candidate *Candidate
	// baseFiles is the tree the candidate's base was captured from; rebuild
	// re-captures it, so the fixture is deterministic.
	baseFiles map[string]string

	// beforeTree is the target tree as it was before Publish, for the byte
	// comparisons every failure path makes.
	beforeTree map[string]string
}

// newPublishFixture builds a target from baseFiles, a working copy from
// resultFiles, and computes the candidate with the real Prepare. A path in
// resultFiles that baseFiles lacks is a create; a changed path is a modify; a
// baseFiles path missing from resultFiles is a delete.
//
// dirtyFiles are written to the target *after* the candidate is computed: they
// are the user's own edits, which the candidate does not know about and a
// publish must never touch.
//
// The working copy carries runworkspace's ownership marker (Materialize writes
// it in production), so the fixtures exercise the real "internal path" rule: a
// publish must not carry the marker into the user's tree.
func newPublishFixture(t *testing.T, baseFiles, resultFiles, dirtyFiles map[string]string) *publishFixture {
	t.Helper()
	st, _ := mergeDB(t)
	target := t.TempDir()
	wc := t.TempDir()
	writeTestFile(t, wc, runworkspace.OwnerMarkerName(), `{"run":"r-1"}`)
	for rel, content := range resultFiles {
		writeTestFile(t, wc, rel, content)
	}
	// The target holds the base state; the operations are what the run's result
	// changes about it. (Dirty files are added after the candidate is computed.)
	for rel, content := range baseFiles {
		writeTestFile(t, target, rel, content)
	}

	f := &publishFixture{
		t:         t,
		store:     st,
		blobs:     newCountingBlobs(),
		fs:        newFaultFS(),
		target:    target,
		wc:        wc,
		baseFiles: copyStrings(baseFiles),
	}
	root, err := workspace.CaptureRootIdentity(target)
	if err != nil {
		t.Fatalf("CaptureRootIdentity: %v", err)
	}
	f.root = root
	f.rebuild(t)

	for rel, content := range dirtyFiles {
		writeTestFile(t, target, rel, content)
	}
	f.beforeTree = snapshotTree(t, target)
	return f
}

// rebuild recomputes the fixture's candidate from the two trees as they are
// now. Base and target are the same directory — the target holds the base
// state, which is what makes every candidate operation applicable — so both
// manifests describe f.target, captured with the same contents.
func (f *publishFixture) rebuild(t *testing.T) {
	t.Helper()
	targetManifest := capturePlain(t, f.target)
	baseManifest := capturePlain(t, f.target)
	resultManifest := capturePlain(t, f.wc)
	// The result manifest keeps its own root (the working copy): Prepare
	// requires ResultRoot to be the directory the result manifest describes, and
	// the publisher reads new content from exactly that directory. Only base and
	// target, which describe the one user directory, must be the same root.

	f.blobs = newCountingBlobs()
	c, err := Prepare(context.Background(), PrepareInput{
		Base:       baseManifest,
		Target:     targetManifest,
		Result:     resultManifest,
		ResultRoot: f.wc,
		Blobs:      f.blobs,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !c.Publishable() {
		t.Fatalf("fixture candidate is not publishable: conflicts %+v applied %v", c.Conflicts, c.AlreadyApplied)
	}
	f.candidate = c
}

func copyStrings(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (f *publishFixture) input(opID string) PublishInput {
	return PublishInput{
		Store:        f.store,
		Blobs:        f.blobs,
		Candidate:    f.candidate,
		ResultRoot:   f.wc,
		TargetRoot:   f.target,
		ExpectedRoot: f.root,
		ProjectID:    "p-1",
		RunID:        "r-1",
		OperationID:  opID,
		Now:          func() time.Time { return time.UnixMilli(1700001000000).UTC() },
		Destinations: []string{"ws:project:p-1", "ws:run:r-1", "audit"},
		FS:           f.fs,
	}
}

// snapshotTree reads every path under root, files and directories alike, so a
// rollback can be compared byte for byte *including the structure*: a
// directory the publish created and a rollback failed to remove shows up as an
// extra key.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			out[rel+"/"] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = "file:" + string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return out
}

func assertTreeEquals(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if reflect.DeepEqual(before, after) {
		return
	}
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	var diffs []string
	for k := range keys {
		b, inBefore := before[k]
		a, inAfter := after[k]
		switch {
		case !inAfter:
			diffs = append(diffs, fmt.Sprintf("  missing after: %s (%q)", k, b))
		case !inBefore:
			diffs = append(diffs, fmt.Sprintf("  appeared: %s (%q)", k, a))
		case a != b:
			diffs = append(diffs, fmt.Sprintf("  changed: %s\n    before %q\n    after  %q", k, b, a))
		}
	}
	sort.Strings(diffs)
	t.Fatalf("%s: the trees differ:\n%s", what, strings.Join(diffs, "\n"))
}

func (f *publishFixture) operation(t *testing.T, id string) MergeOperation {
	t.Helper()
	op, err := GetOperation(context.Background(), f.store.DB(), id)
	if err != nil {
		t.Fatalf("GetOperation(%s): %v", id, err)
	}
	return op
}

func (f *publishFixture) files(t *testing.T, id string) []MergeFile {
	t.Helper()
	files, err := ListFiles(context.Background(), f.store.DB(), id)
	if err != nil {
		t.Fatalf("ListFiles(%s): %v", id, err)
	}
	return files
}

// publish runs Publish with the fixture's seams and returns the result.
func (f *publishFixture) publish(t *testing.T, in PublishInput) (PublishResult, error) {
	t.Helper()
	return Publish(context.Background(), in)
}

// mustCreateClaimedOp attempts to create an operation on the fixture's root and
// returns the error, so a test can assert whether the root lock is free.
func (f *publishFixture) mustCreateClaimedOp(t *testing.T, id string) error {
	t.Helper()
	return f.store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		_, err := CreateOperationTx(ctx, tx, journalOpInput(id, f.root.String()))
		return err
	})
}

// forceNeedsRecovery moves the operation to needs_recovery the way a recoverer
// taking over would: a higher fence and a state that is not applying.
func (f *publishFixture) forceNeedsRecovery(t *testing.T, id string) {
	t.Helper()
	if _, err := f.store.DB().Exec(`
		UPDATE merge_operations
		SET status = 'needs_recovery', fence = fence + 1, revision = revision + 1, updated_at = updated_at + 1
		WHERE id = ?`, id); err != nil {
		t.Fatalf("force needs_recovery(%s): %v", id, err)
	}
}

// fileOf returns the journal row for one path.
func fileOf(t *testing.T, files []MergeFile, path string) MergeFile {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("no file row for %s in %+v", path, files)
	return MergeFile{}
}

// opFor returns the candidate's operation for one path (the seq a test wants to
// fail at is read from the candidate, not assumed).
func opFor(t *testing.T, c *Candidate, path string) Operation {
	t.Helper()
	for _, op := range c.Operations {
		if op.Path == path {
			return op
		}
	}
	t.Fatalf("no candidate operation for %s", path)
	return Operation{}
}

// --- the success path ---

// TestPublishAppliesCreateModifyDeleteAndRenamePair publishes a candidate that
// contains all three kinds plus the delete+create pair of a rename, with a
// nested directory created along the way, and then checks the three sides of
// the same fact: the directory, the journal and the event.
func TestPublishAppliesCreateModifyDeleteAndRenamePair(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{
			"keep.txt":      "unchanged",
			"mod.txt":       "old content",
			"gone.txt":      "to be deleted",
			"old-name.txt":  "renamed content",
			"deep/kept.txt": "deep",
		},
		map[string]string{
			"keep.txt":         "unchanged",
			"mod.txt":          "new content",
			"new-name.txt":     "renamed content", // create paired with the delete of old-name.txt
			"added.txt":        "brand new",
			"deep/kept.txt":    "deep",
			"deep/sub/new.txt": "nested create",
		},
		map[string]string{"dirty.txt": "user's own file"})

	// The rename pair really is a pair: Prepare marks both halves.
	if op := opFor(t, f.candidate, "new-name.txt"); op.RenamedFrom != "old-name.txt" {
		t.Fatalf("new-name.txt RenamedFrom = %q, want old-name.txt", op.RenamedFrom)
	}
	if op := opFor(t, f.candidate, "old-name.txt"); op.RenamedTo != "new-name.txt" {
		t.Fatalf("old-name.txt RenamedTo = %q, want new-name.txt", op.RenamedTo)
	}

	in := f.input("op-success")
	res, err := f.publish(t, in)
	if err != nil {
		t.Fatalf("Publish: %v (result %+v)", err, res)
	}
	if res.Operation.Status != StatusApplied {
		t.Fatalf("operation status = %s, want applied", res.Operation.Status)
	}
	if res.Event == nil || res.Event.Type != string(run.EventMergeCompleted) {
		t.Fatalf("no merge.completed event in the result: %+v", res.Event)
	}

	// The target is exactly the result tree plus the user's dirty file, and the
	// working copy's ownership marker did not leak into it.
	for rel, want := range map[string]string{
		"keep.txt":         "unchanged",
		"mod.txt":          "new content",
		"added.txt":        "brand new",
		"new-name.txt":     "renamed content",
		"deep/kept.txt":    "deep",
		"deep/sub/new.txt": "nested create",
		"dirty.txt":        "user's own file",
	} {
		got, ok := targetFile(f.target, rel)
		if !ok || got != want {
			t.Errorf("%s = %q (present %v), want %q", rel, got, ok, want)
		}
	}
	for _, gone := range []string{"gone.txt", "old-name.txt", runworkspace.OwnerMarkerName()} {
		if _, ok := targetFile(f.target, gone); ok {
			t.Errorf("%s exists after the publish, want it gone or never published", gone)
		}
	}
	// Whole-tree equality: the user's directory is byte-for-byte the result
	// working copy (minus the ownership marker, which is internal) plus the
	// dirty file the run never touched.
	want := snapshotTree(t, f.wc)
	delete(want, runworkspace.OwnerMarkerName())
	want["dirty.txt"] = "file:user's own file"
	assertTreeEquals(t, "the target after the publish", want, snapshotTree(t, f.target))

	// Every file row is written; the state of each is what the path holds.
	for _, row := range f.files(t, "op-success") {
		if row.State != FileStateWritten {
			t.Errorf("file %s (%s) is in state %s, want written", row.Path, row.Kind, row.State)
		}
	}
	if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 1 {
		t.Errorf("merge.completed events = %d, want 1", n)
	}
	if n := countOutbox(t, f.store); n != len(in.Destinations) {
		t.Errorf("outbox rows = %d, want %d", n, len(in.Destinations))
	}
}

func TestPublishLeavesTheWorkingCopyAlone(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old"},
		map[string]string{"a.txt": "new"},
		nil)
	wcBefore := snapshotTree(t, f.wc)

	if _, err := f.publish(t, f.input("op-1")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	assertTreeEquals(t, "the working copy", wcBefore, snapshotTree(t, f.wc))
}

// TestPublishCompletionIsOneTransaction injects a failure between the applied
// move and the event append. If the two were separate transactions, the
// operation would be left applied with no merge.completed; because they are
// one, the operation is still applying when the rollback reads it back, the
// target is back to its old content, and no event exists.
func TestPublishCompletionIsOneTransaction(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old"},
		map[string]string{"a.txt": "new"},
		nil)

	in := f.input("op-1")
	in.Hook = func(s Step) error {
		if s.Stage == StageBeforeEvent {
			return errors.New("scripted failure before merge.completed")
		}
		return nil
	}
	res, err := f.publish(t, in)
	if err == nil {
		t.Fatalf("Publish with a failing event write returned no error: %+v", res)
	}
	if got := res.Operation.Status; got != StatusRolledBack {
		t.Fatalf("operation status = %s, want rolled_back", got)
	}
	if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 0 {
		t.Fatalf("merge.completed events = %d, want 0", n)
	}
	if n := countOutbox(t, f.store); n != 0 {
		t.Fatalf("outbox rows = %d, want 0", n)
	}
	if got, ok := targetFile(f.target, "a.txt"); !ok || got != "old" {
		t.Fatalf("a.txt = %q (present %v), want the old content", got, ok)
	}
	// The root lock was released by the rollback.
	if err := f.mustCreateClaimedOp(t, "op-next"); err != nil {
		t.Fatalf("a new operation on the same root was refused after the rollback: %v", err)
	}
}

// --- failure injection ---

// TestPublishRollsBackFromEveryFailurePoint runs the same publish with the
// first, a middle and the last file failing, at each of the three content-step
// stages (before the replace, after the replace, between the replace and the
// journal mark). Every combination must leave the tree byte-identical to what
// it was — structure included — the operation rolled_back, and the lock
// released.
func TestPublishRollsBackFromEveryFailurePoint(t *testing.T) {
	base := map[string]string{
		"a.txt":    "old a",
		"b.txt":    "old b",
		"c.txt":    "old c",
		"old1.txt": "rename one",
		"old2.txt": "rename two",
	}
	result := map[string]string{
		"a.txt":      "new a",
		"b.txt":      "changed b",
		"new1.txt":   "rename one",
		"new2.txt":   "rename two",
		"deep/x.txt": "nested create",
		"c2.txt":     "created",
	}

	for _, stage := range []string{StageBeforeReplace, StageAfterReplace, StageBeforeMark} {
		for _, which := range []string{"first", "middle", "last"} {
			t.Run(stage+"/"+which, func(t *testing.T) {
				f := newPublishFixture(t, base, result, nil)
				n := int64(len(f.candidate.Operations))
				seq := int64(1)
				switch which {
				case "middle":
					seq = n/2 + 1
				case "last":
					seq = n
				}
				path := f.candidate.Operations[seq-1].Path

				in := f.input("op-" + stage + "-" + which)
				in.Hook = failAtContentStep(seq, stage)

				res, err := f.publish(t, in)
				if err == nil {
					t.Fatalf("Publish with a failing %s at seq %d (%s) returned no error", stage, seq, path)
				}
				if !strings.Contains(err.Error(), fmt.Sprintf("seq %d stage %s", seq, stage)) {
					t.Fatalf("error does not name the failure point: %v", err)
				}
				if res.Operation.Status != StatusRolledBack {
					t.Fatalf("operation status = %s, want rolled_back", res.Operation.Status)
				}
				assertTreeEquals(t, "the target after the rollback", f.beforeTree, snapshotTree(t, f.target))
				if err := f.mustCreateClaimedOp(t, "op-next"); err != nil {
					t.Fatalf("the root lock was not released: %v", err)
				}
			})
		}
	}
}

// TestPublishRollbackRemovesTheDirectoriesItCreated pins the directory part of
// the rollback: deep/, which this operation planned to create and did create,
// must be removed again when it is empty; deep/kept.txt, which the plan did not
// create, must survive untouched.
func TestPublishRollbackRemovesTheDirectoriesItCreated(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"keep.txt": "keep"},
		map[string]string{
			"keep.txt":         "keep",
			"deep/sub/new.txt": "nested",
			"deep/x.txt":       "shallow",
		},
		nil)
	if _, ok := targetFile(f.target, "deep/sub/new.txt"); ok {
		t.Fatal("the fixture built deep/ into the target; the test needs it absent before the publish")
	}

	in := f.input("op-1")
	in.Hook = failAtContentStep(2, StageBeforeReplace) // deep/x.txt, after deep/sub/new.txt (path order)
	res, err := f.publish(t, in)
	if err == nil {
		t.Fatalf("Publish returned no error: %+v", res)
	}
	if res.Operation.Status != StatusRolledBack {
		t.Fatalf("operation status = %s, want rolled_back", res.Operation.Status)
	}
	assertTreeEquals(t, "the target after the rollback", f.beforeTree, snapshotTree(t, f.target))
}

// --- the conflict and refusal paths ---

func TestPublishBackupFindsAChangedTarget(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old", "b.txt": "keep"},
		map[string]string{"a.txt": "new", "b.txt": "keep"},
		nil)
	// Somebody edits the target after Prepare. The backup phase reads the file,
	// hashes it, and must refuse: conflict, zero writes, lock released.
	writeTestFile(t, f.target, "a.txt", "edited after prepare")

	before := snapshotTree(t, f.target)
	in := f.input("op-1")
	res, err := f.publish(t, in)
	if !errors.Is(err, ErrTargetChanged) {
		t.Fatalf("Publish with a changed target = %v, want ErrTargetChanged", err)
	}
	if res.Operation.Status != StatusConflict {
		t.Fatalf("operation status = %s, want conflict", res.Operation.Status)
	}
	assertTreeEquals(t, "the target after the conflict", before, snapshotTree(t, f.target))
	if n := f.fs.replaces(); n != 0 {
		t.Fatalf("a conflicting publish replaced %d files, want 0", n)
	}
	if files := f.files(t, "op-1"); len(files) != 0 {
		t.Fatalf("a conflicting operation has %d file rows, want 0", len(files))
	}
	if err := f.mustCreateClaimedOp(t, "op-next"); err != nil {
		t.Fatalf("the root lock was not released: %v", err)
	}
}

func TestPublishBlobFailureWritesNothing(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old"},
		map[string]string{"a.txt": "new"},
		nil)
	// The first Put *Publish* makes is the backup of a.txt (backups come before
	// new content); the fixture's own Prepare already stored the new content,
	// so the failure point is counted from where the store is now.
	putsBefore := f.blobs.putCount()
	f.blobs.failPutAt = putsBefore + 1

	before := snapshotTree(t, f.target)
	in := f.input("op-1")
	res, err := f.publish(t, in)
	if err == nil {
		t.Fatalf("Publish with a failing blob store returned no error: %+v", res)
	}
	if res.Operation.Status != StatusRolledBack {
		t.Fatalf("operation status = %s, want rolled_back", res.Operation.Status)
	}
	assertTreeEquals(t, "the target after the blob failure", before, snapshotTree(t, f.target))
	// No file row may name a blob that is not in the store: the journal is only
	// ever written after its blobs are.
	for _, row := range f.files(t, "op-1") {
		if row.BackupHash != nil && !f.blobs.has(*row.BackupHash) {
			t.Errorf("file row %s references missing blob %s", row.Path, *row.BackupHash)
		}
		if row.NewHash != nil && !f.blobs.has(*row.NewHash) {
			t.Errorf("file row %s references missing blob %s", row.Path, *row.NewHash)
		}
	}
}

func TestPublishRefusesAChangedRootIdentity(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old"},
		map[string]string{"a.txt": "new"},
		nil)
	// The bound root is some other directory now (deleted and recreated, or the
	// binding was re-pointed). The very first check must refuse, before any
	// journal row, blob or target byte.
	other := t.TempDir()
	otherRoot, err := workspace.CaptureRootIdentity(other)
	if err != nil {
		t.Fatalf("CaptureRootIdentity: %v", err)
	}
	in := f.input("op-1")
	in.ExpectedRoot = otherRoot

	putsBefore := f.blobs.putCount()
	before := snapshotTree(t, f.target)
	_, err = f.publish(t, in)
	if !errors.Is(err, ErrRootIdentityChanged) {
		t.Fatalf("Publish with a foreign root = %v, want ErrRootIdentityChanged", err)
	}
	assertTreeEquals(t, "the target after the refusal", before, snapshotTree(t, f.target))
	if _, err := GetOperation(context.Background(), f.store.DB(), "op-1"); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("the refused publish left a journal row: %v", err)
	}
	if got := f.blobs.putCount(); got != putsBefore {
		t.Fatalf("the refused publish stored %d blobs, want 0 beyond the fixture's own", got-putsBefore)
	}
}

func TestPublishRefusesARootThatMovedDuringTheLoop(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old", "b.txt": "old"},
		map[string]string{"a.txt": "new", "b.txt": "new"},
		nil)
	other := t.TempDir()
	otherRoot, err := workspace.CaptureRootIdentity(other)
	if err != nil {
		t.Fatalf("CaptureRootIdentity: %v", err)
	}
	// Capture 1 is the pre-publish check; the loop then captures once per file.
	// From capture 2 on the directory has "moved": the first file's re-check
	// must see the wrong root and park the operation, before that file's
	// precondition or write.
	f.fs.identityOverride = &otherRoot
	f.fs.identityOverrideFrom = 2

	before := snapshotTree(t, f.target)
	_, err = f.publish(t, f.input("op-1"))
	if !errors.Is(err, ErrRootIdentityChanged) {
		t.Fatalf("Publish whose root was replaced mid-loop = %v, want ErrRootIdentityChanged", err)
	}
	op := f.operation(t, "op-1")
	if op.Status != StatusNeedsRecovery {
		t.Fatalf("operation status = %s, want needs_recovery", op.Status)
	}
	// Nothing was written: the check is before the first file's content step.
	assertTreeEquals(t, "the target after the parked publish", before, snapshotTree(t, f.target))
	if n := f.fs.replaces(); n != 0 {
		t.Fatalf("replace calls = %d, want 0", n)
	}
	// The lock is held (needs_recovery does not release it).
	if err := f.mustCreateClaimedOp(t, "op-next"); !errors.Is(err, ErrRootBusy) {
		t.Fatalf("a new operation on a needs_recovery root = %v, want ErrRootBusy", err)
	}
}

// --- takeover and the fence ---

func TestPublishStopsWhenTheOperationIsTakenOver(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old", "b.txt": "old", "c.txt": "old"},
		map[string]string{"a.txt": "A", "b.txt": "B", "c.txt": "C"},
		nil)

	in := f.input("op-1")
	// After the first file's replace, a recoverer takes the operation over:
	// status needs_recovery and a higher fence. The loop must stop before the
	// second file's content step — the ownership check comes first — and, since
	// a takeover is not this process's to undo, it must not roll back.
	var tookOver bool
	in.Hook = func(s Step) error {
		if s.Seq == 1 && s.Stage == StageAfterReplace && !tookOver {
			tookOver = true
			f.forceNeedsRecovery(t, "op-1")
		}
		return nil
	}
	res, err := f.publish(t, in)
	if !errors.Is(err, ErrOperationMoved) {
		t.Fatalf("Publish after a takeover = %v, want ErrOperationMoved", err)
	}
	if res.Operation.Status != StatusNeedsRecovery {
		t.Fatalf("operation status = %s, want needs_recovery", res.Operation.Status)
	}
	if n := f.fs.replaces(); n != 1 {
		t.Fatalf("replace calls = %d, want exactly 1 (the loop must stop at the takeover)", n)
	}
	if got, _ := targetFile(f.target, "b.txt"); got != "old" {
		t.Fatalf("b.txt = %q, want the old content (untouched)", got)
	}
	// The lock is still held: another publish of the same root is refused and
	// names the occupant.
	res2, err2 := f.publish(t, f.input("op-2"))
	if !errors.Is(err2, ErrRootBusy) {
		t.Fatalf("second publish on a needs_recovery root = %v, want ErrRootBusy", err2)
	}
	if !strings.Contains(err2.Error(), "op-1") {
		t.Fatalf("ErrRootBusy does not name the occupant: %v", err2)
	}
	_ = res2
}

// TestPublishStopsWhenTheFenceMoves isolates the fence from the status: the
// operation stays applying but its epoch moves, which is how a recoverer that
// wants to keep the operation alive would announce that a previous holder is
// superseded. The loop must stop before the next file, and (as with a full
// takeover) must not roll back.
func TestPublishStopsWhenTheFenceMoves(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old", "b.txt": "old"},
		map[string]string{"a.txt": "A", "b.txt": "B"},
		nil)

	in := f.input("op-1")
	var bumped bool
	in.Hook = func(s Step) error {
		if s.Seq == 1 && s.Stage == StageAfterReplace && !bumped {
			bumped = true
			if _, err := f.store.DB().Exec(`
				UPDATE merge_operations SET fence = fence + 1, revision = revision + 1 WHERE id = 'op-1'`); err != nil {
				t.Errorf("bump fence: %v", err)
			}
		}
		return nil
	}
	res, err := f.publish(t, in)
	if !errors.Is(err, ErrOperationMoved) {
		t.Fatalf("Publish after a fence move = %v, want ErrOperationMoved", err)
	}
	if res.Operation.Status != StatusApplying || res.Operation.Fence != 2 {
		t.Fatalf("operation row = %s at fence %d, want applying at fence 2", res.Operation.Status, res.Operation.Fence)
	}
	if n := f.fs.replaces(); n != 1 {
		t.Fatalf("replace calls = %d, want exactly 1 (the loop must stop at the fence move)", n)
	}
	if got, _ := targetFile(f.target, "b.txt"); got != "old" {
		t.Fatalf("b.txt = %q, want the old content (untouched)", got)
	}
}

// TestPublishTwoGoroutinesSameRootExactlyOneWins starts two publishes on one
// root with different operation ids and makes the race deterministic: the first
// goroutine is held inside its file loop, after its lock transaction committed
// (the operation row exists, applying, fence 1), while the second tries to take
// the same root. The second must be refused with ErrRootBusy naming the first —
// the partial unique index decides, not timing — and must write nothing. When
// the winner is released and finishes, the same root accepts the next publish
// again.
func TestPublishTwoGoroutinesSameRootExactlyOneWins(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old", "b.txt": "old"},
		map[string]string{"a.txt": "new", "b.txt": "new"},
		nil)

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWinner := func() { releaseOnce.Do(func() { close(release) }) }
	winnerErr := make(chan error, 1)
	winnerDone := make(chan struct{})
	var winnerRes PublishResult

	winnerIn := f.input("op-winner")
	winnerIn.Hook = func(s Step) error {
		if s.Seq == 1 && s.Stage == StageBeforeReplace {
			close(started)
			<-release
		}
		return nil
	}
	go func() {
		defer close(winnerDone)
		res, err := Publish(context.Background(), winnerIn)
		winnerRes = res
		winnerErr <- err
	}()
	// Whatever happens to the test, the winner must not stay blocked on release:
	// its goroutine holds no transaction once the hook returns, so the store can
	// be closed cleanly.
	t.Cleanup(func() { releaseWinner(); <-winnerDone })

	<-started

	// While the winner holds the root (applying), the second publish must be
	// refused, name the occupant, and write nothing.
	loserIn := f.input("op-loser")
	loserIn.FS = newFaultFS()
	loserRes, loserErr := f.publish(t, loserIn)
	if !errors.Is(loserErr, ErrRootBusy) {
		t.Fatalf("concurrent publish on a locked root = %v, want ErrRootBusy", loserErr)
	}
	if !strings.Contains(loserErr.Error(), "op-winner") {
		t.Fatalf("ErrRootBusy does not name the occupant: %v", loserErr)
	}
	if loserRes.Operation.ID != "" {
		t.Fatalf("the refused publish reported an operation: %+v", loserRes.Operation)
	}
	if n := f.fs.replaces(); n != 0 {
		t.Fatalf("the loser wrote %d files while the winner held the root, want 0", n)
	}
	if op := f.operation(t, "op-winner"); op.Status != StatusApplying || op.Fence != 1 {
		t.Fatalf("winner row = %+v, want applying at fence 1", op)
	}

	// Release the winner: it finishes normally, with the file loop, the verify
	// pass and the completion transaction.
	releaseWinner()
	<-winnerDone
	if err := <-winnerErr; err != nil {
		t.Fatalf("winner publish: %v (result %+v)", err, winnerRes)
	}
	if winnerRes.Operation.Status != StatusApplied {
		t.Fatalf("winner status = %s, want applied", winnerRes.Operation.Status)
	}
	for _, rel := range []string{"a.txt", "b.txt"} {
		if got, _ := targetFile(f.target, rel); got != "new" {
			t.Fatalf("%s = %q, want the new content", rel, got)
		}
	}
	// The winner released the lock: a fresh claim of the same root is accepted.
	if err := f.mustCreateClaimedOp(t, "op-next"); err != nil {
		t.Fatalf("the root was not released after the winner finished: %v", err)
	}
}

// TestPublishDifferentRootsDoNotBlockEachOther holds one publish inside its
// file loop (its root lock committed) and runs a second publish of a different
// root to completion while the first is stuck: the second must finish with a
// nil error, proving the two roots' locks are independent. The first is then
// released and must also finish.
func TestPublishDifferentRootsDoNotBlockEachOther(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old"},
		map[string]string{"a.txt": "new"},
		nil)

	second := t.TempDir()
	writeTestFile(t, second, "a.txt", "old")
	secondRoot, err := workspace.CaptureRootIdentity(second)
	if err != nil {
		t.Fatalf("CaptureRootIdentity: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
	firstErr := make(chan error, 1)
	firstDone := make(chan struct{})
	var firstRes PublishResult

	firstIn := f.input("op-first")
	firstIn.Hook = func(s Step) error {
		if s.Seq == 1 && s.Stage == StageBeforeReplace {
			close(started)
			<-release
		}
		return nil
	}
	go func() {
		defer close(firstDone)
		res, err := Publish(context.Background(), firstIn)
		firstRes = res
		firstErr <- err
	}()
	t.Cleanup(func() { releaseFirst(); <-firstDone })
	<-started

	// The first root's lock is committed (the operator row exists, applying).
	if op := f.operation(t, "op-first"); op.Status != StatusApplying {
		t.Fatalf("first operation status = %s, want applying", op.Status)
	}

	// The second publish, on its own root, completes while the first is stuck.
	secondIn := f.input("op-second")
	secondIn.TargetRoot = second
	secondIn.ExpectedRoot = secondRoot
	secondIn.FS = newFaultFS()
	secondIn.Candidate = retargetCandidate(t, f.candidate, second)
	res, err := f.publish(t, secondIn)
	if err != nil {
		t.Fatalf("publish on a different root while the first was held: %v", err)
	}
	if res.Operation.Status != StatusApplied {
		t.Fatalf("second operation status = %s, want applied", res.Operation.Status)
	}
	if got, _ := targetFile(second, "a.txt"); got != "new" {
		t.Fatalf("second root: a.txt = %q, want the new content", got)
	}

	// Release the first; it must finish its own publish.
	releaseFirst()
	<-firstDone
	if err := <-firstErr; err != nil {
		t.Fatalf("first publish: %v (result %+v)", err, firstRes)
	}
	if firstRes.Operation.Status != StatusApplied {
		t.Fatalf("first operation status = %s, want applied", firstRes.Operation.Status)
	}
	if got, _ := targetFile(f.target, "a.txt"); got != "new" {
		t.Fatalf("first root: a.txt = %q, want the new content", got)
	}
}

// retargetCandidate returns a copy of c whose Root is root. The operations are
// shared (read-only); only the Hash is recomputed, because Root is part of it.
func retargetCandidate(t *testing.T, c *Candidate, root string) *Candidate {
	t.Helper()
	out := *c
	out.Root = root
	out.Hash = candidateHash(&out)
	return &out
}

// --- rollback meets an external edit ---

func TestPublishRollbackMeetsAnExternalEditAndParks(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old a", "b.txt": "old b", "c.txt": "old c"},
		map[string]string{"a.txt": "A", "b.txt": "B", "c.txt": "C"},
		nil)

	in := f.input("op-1")
	// File 1 is written and marked. Then, before the failure at file 3, the
	// user edits file 1 again: the rollback will find content that is neither
	// this operation's new content nor its backup, and must not touch it.
	edited := "the user's newest version"
	in.Hook = func(s Step) error {
		switch {
		case s.Stage == StageAfterReplace && s.Seq == 1:
			writeTestFile(t, f.target, "a.txt", edited)
		case s.Seq == 3 && s.Stage == StageBeforeReplace:
			return errors.New("scripted failure at the third file")
		}
		return nil
	}
	res, err := f.publish(t, in)
	if !errors.Is(err, ErrNeedsRecovery) {
		t.Fatalf("Publish whose rollback met an external edit = %v, want ErrNeedsRecovery", err)
	}
	if res.Operation.Status != StatusNeedsRecovery {
		t.Fatalf("operation status = %s, want needs_recovery", res.Operation.Status)
	}
	if got, _ := targetFile(f.target, "a.txt"); got != edited {
		t.Fatalf("a.txt = %q, want the user's edit untouched", got)
	}
	if row := fileOf(t, res.Files, "a.txt"); row.State != FileStateExternalEdit {
		t.Fatalf("a.txt row state = %s, want external_edit", row.State)
	}
	// The lock is held: a second publish of the same root is refused.
	if _, err := f.publish(t, f.input("op-2")); !errors.Is(err, ErrRootBusy) {
		t.Fatalf("second publish = %v, want ErrRootBusy", err)
	}
}

// --- refusals before anything happens ---

func TestPublishRefusesUnpublishableCandidates(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old"},
		map[string]string{"a.txt": "new"},
		nil)
	before := snapshotTree(t, f.target)

	t.Run("conflicts", func(t *testing.T) {
		in := f.input("op-conflict")
		bad := *f.candidate
		bad.Conflicts = []Conflict{{Path: "a.txt", Reason: ReasonTargetChanged}}
		bad.Hash = candidateHash(&bad)
		in.Candidate = &bad
		if _, err := f.publish(t, in); !errors.Is(err, ErrCandidateNotPublishable) {
			t.Fatalf("conflicting candidate = %v, want ErrCandidateNotPublishable", err)
		}
	})
	t.Run("hash does not describe the candidate", func(t *testing.T) {
		in := f.input("op-hash")
		bad := *f.candidate
		bad.Hash = journalTestHashA // not the hash of Bad's content
		in.Candidate = &bad
		if _, err := f.publish(t, in); !errors.Is(err, ErrCandidateHashMismatch) {
			t.Fatalf("candidate with a forged hash = %v, want ErrCandidateHashMismatch", err)
		}
	})
	t.Run("unsupported operation", func(t *testing.T) {
		in := f.input("op-symlink")
		bad := *f.candidate
		bad.Operations = append([]Operation(nil), f.candidate.Operations...)
		link := opFor(t, f.candidate, "a.txt")
		link.Path = "link.txt"
		link.Kind = KindCreate
		link.NewType = runworkspace.TypeSymlink
		bad.Operations = append(bad.Operations, link)
		bad.Hash = candidateHash(&bad)
		in.Candidate = &bad
		if _, err := f.publish(t, in); !errors.Is(err, ErrUnsupportedOperation) {
			t.Fatalf("candidate with a symlink = %v, want ErrUnsupportedOperation", err)
		}
	})

	// None of the three touched anything.
	assertTreeEquals(t, "the target after the refusals", before, snapshotTree(t, f.target))
	for _, id := range []string{"op-conflict", "op-hash", "op-symlink"} {
		if _, err := GetOperation(context.Background(), f.store.DB(), id); !errors.Is(err, ErrOperationNotFound) {
			t.Errorf("refused publish %s left a journal row: %v", id, err)
		}
	}
	if err := f.mustCreateClaimedOp(t, "op-next"); err != nil {
		t.Fatalf("a refused publish left the root locked: %v", err)
	}
}

// --- helpers ---

func countEvents(t *testing.T, st *runstore.Store, eventType string) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM events WHERE type = ?`, eventType).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

func countOutbox(t *testing.T, st *runstore.Store) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return n
}
