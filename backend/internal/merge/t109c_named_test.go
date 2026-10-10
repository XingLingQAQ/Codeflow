package merge

// The T1.09.c named tests: the user's dirty git baseline survives a publish, a
// failure at any file and any step restores the target byte for byte, a
// recovery never writes over a user's edit, two spellings of one directory
// share one root lock, and a crash at every publish point recovers to exactly
// the state the journal describes.
//
// Everything here is built on the accepted fixtures of publish_test.go and
// recover_test.go (newPublishFixture, snapshotTree, assertTreeEquals, the fault
// seams) rather than new doubles, so the tests exercise the same publisher and
// recoverer production runs.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runworkspace"
	"github.com/codeflow/backend/internal/workspace"
)

// --- crash injection ---

// crashPanic is the sentinel a crash hook panics with. Recovering it in the
// goroutine that called Publish is what "the process died here" means for the
// publisher: publish.go performs its rollback with explicit calls, not with a
// deferred handler, so a panic skips the rollback and the journal stops at the
// exact statement the hook interrupted (WithTx rolls a panic inside a
// transaction back and re-raises it).
type crashPanic struct {
	Stage string
	Seq   int64
}

// t109cCrashAt returns a hook that panics when the publisher reaches the given
// step. StageBeforeEvent has no per-file seq (the publisher calls it once, with
// Seq 0), so for that stage only the stage is matched.
func t109cCrashAt(stage string, seq int64) Hook {
	return func(s Step) error {
		if s.Stage != stage {
			return nil
		}
		if stage != StageBeforeEvent && s.Seq != seq {
			return nil
		}
		panic(crashPanic{Stage: s.Stage, Seq: s.Seq})
	}
}

// publishCrash runs Publish with a hook that will panic, recovers the sentinel
// on the same goroutine, and fails the test if the publish returned normally
// instead (the injection did not fire) or panicked for an unrelated reason.
func publishCrash(t *testing.T, in PublishInput) {
	t.Helper()
	returned := false
	func() {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			if _, ok := r.(crashPanic); !ok {
				panic(r) // an unrelated panic is a bug; do not swallow it
			}
		}()
		res, err := Publish(context.Background(), in)
		returned = true
		_ = res
		_ = err
	}()
	if returned {
		t.Fatal("Publish returned normally; the crash hook should have panicked inside it")
	}
}

// --- recovery plumbing over the publish fixture ---

// t109cRecoverInput is the start-up form (opID == "") or the pinned form of the
// recovery over a publish fixture's root and seams.
func t109cRecoverInput(f *publishFixture, opID string) RecoverInput {
	return RecoverInput{
		Store:        f.store,
		Blobs:        f.blobs,
		TargetRoot:   f.target,
		ExpectedRoot: f.root,
		Recoverer:    "t109c-recoverer",
		Now:          func() time.Time { return time.UnixMilli(1700007000000).UTC() },
		FS:           f.fs,
		Destinations: []string{"ws:project:p-1", "ws:run:r-1", "audit"},
		OperationID:  opID,
	}
}

// t109cAssertBlobsPresent checks every journal row against the blob store: no
// line may name a backup or a new-content blob the store does not have
// (the §30.4 "可见版本无缺失 blob" condition).
func t109cAssertBlobsPresent(t *testing.T, rows []MergeFile, blobs *countingBlobs, what string) {
	t.Helper()
	for _, row := range rows {
		if row.BackupHash != nil && !blobs.has(*row.BackupHash) {
			t.Errorf("%s: journal row %s (seq %d) names backup blob %s, which the store does not have",
				what, row.Path, row.Seq, *row.BackupHash)
		}
		if row.NewHash != nil && !blobs.has(*row.NewHash) {
			t.Errorf("%s: journal row %s (seq %d) names new-content blob %s, which the store does not have",
				what, row.Path, row.Seq, *row.NewHash)
		}
	}
}

// t109cAssertRecoverIsIdempotent runs the recovery a second time over a settled
// root, in both of its forms, and requires: nothing to do, no takeover, no
// write of any kind, no event, and the journal row untouched. It must run
// BEFORE a test claims the root again: a fresh prepared operation on the root
// would be picked up by the start-up form and rolled back by it.
func t109cAssertRecoverIsIdempotent(t *testing.T, f *publishFixture, opID, what string) {
	t.Helper()
	opBefore := f.operation(t, opID)
	replaces, removes, puts := f.fs.replaces(), f.fs.removes(), f.blobs.putCount()
	events := countEvents(t, f.store, string(run.EventMergeCompleted))
	outbox := countOutbox(t, f.store)
	tree := snapshotTree(t, f.target)

	// The start-up form: this root has no unfinished operation any more.
	first, err := Recover(context.Background(), t109cRecoverInput(f, ""))
	if err != nil {
		t.Fatalf("%s: the second Recover (start-up form): %v (report %+v)", what, err, first)
	}
	if first.Outcome != RecoverOutcomeNone {
		t.Fatalf("%s: the second Recover on a settled root = %s, want none", what, first.Outcome)
	}
	if first.TakenOver {
		t.Fatalf("%s: the second Recover took over an operation on a settled root", what)
	}

	// The pinned form: the row is terminal and nothing may move it.
	second, err := Recover(context.Background(), t109cRecoverInput(f, opID))
	if err != nil {
		t.Fatalf("%s: the third Recover (pinned): %v (report %+v)", what, err, second)
	}
	if second.Outcome != RecoverOutcomeUnchanged {
		t.Fatalf("%s: recovering the terminal row %s = %s, want unchanged", what, opID, second.Outcome)
	}
	if second.TakenOver {
		t.Fatalf("%s: the terminal row %s was taken over", what, opID)
	}

	if n := f.fs.replaces(); n != replaces {
		t.Fatalf("%s: the idempotent Recover replaced %d files, want 0", what, n-replaces)
	}
	if n := f.fs.removes(); n != removes {
		t.Fatalf("%s: the idempotent Recover removed %d files, want 0", what, n-removes)
	}
	if n := f.blobs.putCount(); n != puts {
		t.Fatalf("%s: the idempotent Recover stored %d blobs, want 0", what, n-puts)
	}
	if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != events {
		t.Fatalf("%s: the idempotent Recover changed the merge.completed count from %d to %d", what, events, n)
	}
	if n := countOutbox(t, f.store); n != outbox {
		t.Fatalf("%s: the idempotent Recover changed the outbox count from %d to %d", what, outbox, n)
	}
	assertTreeEquals(t, what+": the target after the idempotent Recover", tree, snapshotTree(t, f.target))
	opAfter := f.operation(t, opID)
	if opAfter.Status != opBefore.Status || opAfter.Fence != opBefore.Fence || opAfter.Revision != opBefore.Revision {
		t.Fatalf("%s: the idempotent Recover moved the row: %s fence %d rev %d -> %s fence %d rev %d",
			what, opBefore.Status, opBefore.Fence, opBefore.Revision, opAfter.Status, opAfter.Fence, opAfter.Revision)
	}
}

// --- small assertions shared by the constructs ---

// t109cMultiKindBase is the base tree of the multi-kind candidate: a modify, a
// delete, half of a rename pair, and a directory the result adds files under.
func t109cMultiKindBase() map[string]string {
	return map[string]string{
		"keep.txt":        "keep me",
		"mod.txt":         "old mod",
		"gone.txt":        "delete me",
		"renamed-old.txt": "rename body",
		"deep/kept.txt":   "deep kept",
	}
}

// t109cMultiKindResult is the result tree of the multi-kind candidate: the
// modify, the delete's other half removed, a rename pair, a nested create with
// two new directory levels, a binary file (0x00 and non-UTF-8 bytes), and an
// empty file. Sorted by path the operations are seq 1..7:
//
//	1 blob.bin  2 deep/sub/new.txt  3 empty.txt  4 gone.txt
//	5 mod.txt   6 renamed-new.txt   7 renamed-old.txt
func t109cMultiKindResult() map[string]string {
	return map[string]string{
		"keep.txt":         "keep me",
		"mod.txt":          "new mod",
		"renamed-new.txt":  "rename body",
		"deep/kept.txt":    "deep kept",
		"deep/sub/new.txt": "nested create",
		"blob.bin":         "bin\x00\x01\xfe\xff bytes",
		"empty.txt":        "",
	}
}

// t109cMultiKindResultTree is what the target must hold after the multi-kind
// candidate is fully applied: the result files plus the directories in the
// path, and nothing else.
func t109cMultiKindResultTree() map[string]string {
	return map[string]string{
		"blob.bin":         "file:bin\x00\x01\xfe\xff bytes",
		"deep/":            "dir",
		"deep/kept.txt":    "file:deep kept",
		"deep/sub/":        "dir",
		"deep/sub/new.txt": "file:nested create",
		"empty.txt":        "file:",
		"keep.txt":         "file:keep me",
		"mod.txt":          "file:new mod",
		"renamed-new.txt":  "file:rename body",
	}
}

// t109cOpForSeq returns the candidate operation at publish position seq: the
// operations slice is in publish order, so seq is the 1-based index (Operation
// itself carries no seq; the journal row does).
func t109cOpForSeq(t *testing.T, c *Candidate, seq int64) Operation {
	t.Helper()
	if seq < 1 || int(seq) > len(c.Operations) {
		t.Fatalf("no candidate operation at seq %d (the candidate has %d)", seq, len(c.Operations))
	}
	return c.Operations[seq-1]
}

// t109cAssertPathAt fails unless the path in the target holds the side of the
// operation named: "new" (for a delete: absent), or "old" (for a create:
// absent). The comparison is by content hash, so it is byte-exact.
func t109cAssertPathAt(t *testing.T, root string, op Operation, side string) {
	t.Helper()
	data, ok := targetFile(root, op.Path)
	switch {
	case op.Kind == KindCreate && side == "old", op.Kind == KindDelete && side == "new":
		if ok {
			t.Fatalf("%s: %s should not exist (%s side) but holds %q", root, op.Path, side, data)
		}
	case side == "new":
		if !ok {
			t.Fatalf("%s: %s should hold the new content but does not exist", root, op.Path)
		}
		if h := hashOf([]byte(data)); h != op.NewHash {
			t.Fatalf("%s: %s holds %s, the new side is %s", root, op.Path, h, op.NewHash)
		}
	default:
		if !ok {
			t.Fatalf("%s: %s should hold the old content but does not exist", root, op.Path)
		}
		if h := hashOf([]byte(data)); h != op.OldHash {
			t.Fatalf("%s: %s holds %s, the old side is %s", root, op.Path, h, op.OldHash)
		}
	}
}

// t109cAssertNoMergeEvent pins the count of merge.completed for one store.
func t109cAssertNoMergeEvent(t *testing.T, f *publishFixture, what string) {
	t.Helper()
	if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 0 {
		t.Fatalf("%s: merge.completed events = %d, want 0", what, n)
	}
}

// t109cAssertLockFree requires the root lock to be released, and uses a fresh
// operation id so repeated calls in one test do not collide.
func t109cAssertLockFree(t *testing.T, f *publishFixture, id, what string) {
	t.Helper()
	if err := f.mustCreateClaimedOp(t, id); err != nil {
		t.Fatalf("%s: the root lock was not released: %v", what, err)
	}
}

// t109cAssertLockHeld requires the root to refuse a new operation, and names
// the occupant of the operation id passed.
func t109cAssertLockHeld(t *testing.T, f *publishFixture, occupant, id, what string) {
	t.Helper()
	err := f.mustCreateClaimedOp(t, id)
	if !errors.Is(err, ErrRootBusy) {
		t.Fatalf("%s: a new operation on the root = %v, want ErrRootBusy", what, err)
	}
	if !strings.Contains(err.Error(), occupant) {
		t.Fatalf("%s: ErrRootBusy does not name %s: %v", what, occupant, err)
	}
}

// t109cDrain releases a blocked hook and waits for its goroutine, whatever the
// test did before. It keeps a failing test from hanging the package.
func t109cDrain(release chan struct{}, done chan struct{}) {
	var once sync.Once
	once.Do(func() { close(release) })
	<-done
}

// --- TestDirtyBaselinePreserved: the git side ---

// t109cRequireGit skips with a named reason when no git can be run; on a
// machine with git the test must run (the receipt records a zero SKIP count).
func t109cRequireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not on PATH; this test needs a real repository: %v", err)
	}
}

// t109cGitRepo runs git against one directory with its environment pinned:
// no system config (GIT_CONFIG_NOSYSTEM), no user-global config (HOME and
// USERPROFILE point at a fresh temporary directory), and identity and line
// endings supplied per command with -c. Nothing about the machine's git
// configuration can leak in, and nothing the test runs can change it.
type t109cGitRepo struct {
	t    *testing.T
	dir  string
	home string
}

func (g *t109cGitRepo) git(args ...string) string {
	g.t.Helper()
	full := append([]string{
		"-c", "user.name=T1.09.c",
		"-c", "user.email=t109c@test.invalid",
		"-c", "core.autocrlf=false",
		"-c", "core.safecrlf=false",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = g.dir
	cmd.Env = []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"HOME=" + g.home,
		"USERPROFILE=" + g.home,
		"XDG_CONFIG_HOME=" + g.home,
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"windir=" + os.Getenv("windir"),
		"TEMP=" + os.Getenv("TEMP"),
		"TMP=" + os.Getenv("TMP"),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		g.t.Fatalf("git %s (in %s) with an isolated environment: %v (output: %s)",
			strings.Join(args, " "), g.dir, err, out)
	}
	return string(out)
}

// t109cCopyTreeMinusGit copies the working tree (not the repository metadata)
// into dst: the result capture of the candidate starts from the user's current
// files, so the fixture's working copy must hold them too.
func t109cCopyTreeMinusGit(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy the working tree %s -> %s: %v", src, dst, err)
	}
}

// t109cGitPublishFixture builds a publish fixture whose target is the real
// repository: the working copy is a copy of its current tree with mutate
// applied, and Prepare has run. The repository itself is never written by the
// fixture.
func t109cGitPublishFixture(t *testing.T, g *t109cGitRepo, mutate func(wc string)) *publishFixture {
	t.Helper()
	st, _ := mergeDB(t)
	f := &publishFixture{
		t:      t,
		store:  st,
		blobs:  newCountingBlobs(),
		fs:     newFaultFS(),
		target: g.dir,
	}
	root, err := workspace.CaptureRootIdentity(g.dir)
	if err != nil {
		t.Fatalf("CaptureRootIdentity(%s): %v", g.dir, err)
	}
	f.root = root
	t109cRebuildGitFixture(t, g, f, mutate)
	return f
}

// t109cRebuildGitFixture recomputes the candidate from the repository as it is
// now: the new working copy copies the tree, applies mutate, and rebuild
// re-captures everything.
func t109cRebuildGitFixture(t *testing.T, g *t109cGitRepo, f *publishFixture, mutate func(wc string)) {
	t.Helper()
	wc := t.TempDir()
	t109cCopyTreeMinusGit(t, g.dir, wc)
	writeTestFile(t, wc, runworkspace.OwnerMarkerName(), `{"run":"r-1"}`)
	mutate(wc)
	f.wc = wc
	f.rebuild(t)
	f.beforeTree = snapshotTree(t, f.target)
}

// t109cDirtyBaseline is the byte-level fingerprint of everything a publish
// must not touch: the four dirty paths' contents, the index, HEAD, the whole
// .git directory, and the per-path porcelain lines that say the dirty shape is
// still the same shape.
type t109cDirtyBaseline struct {
	paths     []string
	porcelain string
	contents  map[string]string
	index     string
	head      string
	gitTree   map[string]string
}

func t109cCaptureDirtyBaseline(t *testing.T, g *t109cGitRepo, paths []string) *t109cDirtyBaseline {
	t.Helper()
	d := &t109cDirtyBaseline{paths: paths}
	// status first: it is the one command allowed to refresh the index, and
	// the fingerprint must be taken after it.
	d.porcelain = g.git("status", "--porcelain")
	d.contents = map[string]string{}
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(g.dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read dirty file %s: %v", rel, err)
		}
		d.contents[rel] = string(data)
	}
	data, err := os.ReadFile(filepath.Join(g.dir, ".git", "index"))
	if err != nil {
		t.Fatalf("read .git/index: %v", err)
	}
	d.index = string(data)
	d.head = g.git("rev-parse", "HEAD")
	d.gitTree = snapshotTree(t, filepath.Join(g.dir, ".git"))
	return d
}

func (d *t109cDirtyBaseline) assertIntact(t *testing.T, g *t109cGitRepo, what string) {
	t.Helper()
	for _, rel := range d.paths {
		data, err := os.ReadFile(filepath.Join(g.dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: dirty file %s is gone: %v", what, rel, err)
		}
		if string(data) != d.contents[rel] {
			t.Fatalf("%s: dirty file %s changed\n before %q\n after  %q", what, rel, d.contents[rel], string(data))
		}
	}
	data, err := os.ReadFile(filepath.Join(g.dir, ".git", "index"))
	if err != nil {
		t.Fatalf("%s: .git/index is gone: %v", what, err)
	}
	if string(data) != d.index {
		t.Fatalf("%s: .git/index changed byte for byte", what)
	}
	// The whole .git directory, not just the index: objects, refs, logs. A
	// publish may not move the repository at all.
	assertTreeEquals(t, what+": .git as a whole", d.gitTree, snapshotTree(t, filepath.Join(g.dir, ".git")))
	if got := g.git("rev-parse", "HEAD"); got != d.head {
		t.Fatalf("%s: HEAD moved from %s to %s", what, d.head, got)
	}
	// And the dirty paths are still dirty in exactly the same way.
	porcelain := g.git("status", "--porcelain")
	for _, rel := range d.paths {
		before, after := t109cPorcelainLine(t, d.porcelain, rel), t109cPorcelainLine(t, porcelain, rel)
		if before != after {
			t.Fatalf("%s: the git status of the dirty path %s changed from %q to %q", what, rel, before, after)
		}
	}
}

func t109cPorcelainLine(t *testing.T, porcelain, path string) string {
	t.Helper()
	for _, line := range strings.Split(porcelain, "\n") {
		if strings.HasSuffix(line, " "+path) {
			return line
		}
	}
	t.Fatalf("no git status --porcelain line for %s in:\n%s", path, porcelain)
	return ""
}

// TestDirtyBaselinePreserved publishes a candidate that does not touch the
// user's dirty paths into a repository whose index, worktree and HEAD all
// differ from one another, then fails a publish in the middle and takes the
// rollback. After both, the dirty files, the untracked file, .git/index and
// HEAD are byte-identical to what they were: a merge edits the paths of its
// candidate and never "cleans" the user's repository.
func TestDirtyBaselinePreserved(t *testing.T) {
	t109cRequireGit(t)
	repoDir := t.TempDir()
	g := &t109cGitRepo{t: t, dir: repoDir, home: t.TempDir()}
	g.git("init", "-q")

	for rel, content := range map[string]string{
		"tracked.txt":  "v1 tracked",
		"both.txt":     "v1 both",
		"sub/keep.txt": "v1 sub keep",
		"cf-gone.txt":  "gone body",
		"cf-mod.txt":   "mod v1",
	} {
		writeTestFile(t, repoDir, rel, content)
	}
	g.git("add", "-A")
	g.git("commit", "-q", "-m", "base")

	// The dirty baseline: a worktree-only edit, an untracked file, a file
	// staged and then edited again (index != HEAD, worktree != index), and the
	// same shape on a committed path.
	writeTestFile(t, repoDir, "tracked.txt", "dirty worktree tracked")
	writeTestFile(t, repoDir, "untracked.txt", "untracked body")
	writeTestFile(t, repoDir, "staged.txt", "staged v1")
	g.git("add", "staged.txt")
	writeTestFile(t, repoDir, "staged.txt", "staged v2 worktree")
	writeTestFile(t, repoDir, "both.txt", "both staged v2")
	g.git("add", "both.txt")
	writeTestFile(t, repoDir, "both.txt", "both worktree v3")

	dirtyPaths := []string{"tracked.txt", "untracked.txt", "staged.txt", "both.txt"}
	baseline := t109cCaptureDirtyBaseline(t, g, dirtyPaths)

	// --- round 1: a successful publish that does not touch the dirty paths ---

	f := t109cGitPublishFixture(t, g, func(wc string) {
		removeTestFile(t, wc, "cf-gone.txt")
		writeTestFile(t, wc, "cf-mod.txt", "mod v2")
		writeTestFile(t, wc, "cf-new.txt", "created at root")
		writeTestFile(t, wc, "cf-deep/a.txt", "created deep")
	})
	res, err := f.publish(t, f.input("op-git-1"))
	if err != nil {
		t.Fatalf("Publish into the dirty repository: %v (result %+v)", err, res)
	}
	if res.Operation.Status != StatusApplied {
		t.Fatalf("operation status = %s, want applied", res.Operation.Status)
	}
	// The candidate is in: the touched paths changed, the untouched did not.
	if got, _ := targetFile(repoDir, "cf-mod.txt"); got != "mod v2" {
		t.Fatalf("cf-mod.txt = %q, want the candidate's content", got)
	}
	if _, ok := targetFile(repoDir, "cf-gone.txt"); ok {
		t.Fatal("cf-gone.txt still exists; the delete did not happen")
	}
	want := copyStrings(f.beforeTree)
	want["cf-mod.txt"] = "file:mod v2"
	want["cf-new.txt"] = "file:created at root"
	want["cf-deep/"] = "dir"
	want["cf-deep/a.txt"] = "file:created deep"
	delete(want, "cf-gone.txt")
	assertTreeEquals(t, "the repository after the successful publish", want, snapshotTree(t, repoDir))
	baseline.assertIntact(t, g, "after a successful publish")

	// --- round 2: a failing publish (the middle file) and its rollback ---

	// A fresh fingerprint: round 1's assertions ended with a git status call,
	// and status is allowed to rewrite the index's stat cache. The bytes to
	// hold a publish to are the ones immediately before it.
	baseline = t109cCaptureDirtyBaseline(t, g, dirtyPaths)
	t109cRebuildGitFixture(t, g, f, func(wc string) {
		writeTestFile(t, wc, "cf-mod.txt", "mod v3")
		writeTestFile(t, wc, "cf-new2.txt", "second round")
		writeTestFile(t, wc, "cf-deep/b.txt", "second round deep")
	})
	midSeq := int64(len(f.candidate.Operations)/2 + 1)
	mid := t109cOpForSeq(t, f.candidate, midSeq)
	failIn := f.input("op-git-2")
	failIn.Hook = failAtContentStep(midSeq, StageBeforeReplace)
	res2, err2 := f.publish(t, failIn)
	if err2 == nil {
		t.Fatalf("the second publish was supposed to fail at seq %d (%s): %+v", midSeq, mid.Path, res2)
	}
	if res2.Operation.Status != StatusRolledBack {
		t.Fatalf("operation status = %s, want rolled_back", res2.Operation.Status)
	}
	assertTreeEquals(t, "the repository after the failed publish", f.beforeTree, snapshotTree(t, repoDir))
	baseline.assertIntact(t, g, "after a failed publish and its rollback")
	if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 1 {
		t.Fatalf("merge.completed events = %d, want exactly the first round's 1", n)
	}
	t109cAssertLockFree(t, f, "op-git-after", "after the failed publish")
}

// TestMergeNthFileFailureRestores fails the first, the middle and the last file
// of the multi-kind candidate at each of the three per-file failure points, and
// requires the in-process rollback to leave the target tree byte-equal to the
// moment before the publish — structure included, the created directories
// removed — with the operation rolled back, the root lock released, no
// merge.completed, and every backup the journal names still in the blob store.
func TestMergeNthFileFailureRestores(t *testing.T) {
	// The candidate is not two text files: pin the kinds this construct
	// exists to exercise before failing anything (a rename pair, a binary
	// file with a NUL, an empty file, a nested create).
	probe := newPublishFixture(t, t109cMultiKindBase(), t109cMultiKindResult(), nil)
	if n := len(probe.candidate.Operations); n != 7 {
		t.Fatalf("the multi-kind candidate has %d operations, want 7: %+v", n, probe.candidate.Operations)
	}
	if op := opFor(t, probe.candidate, "renamed-new.txt"); op.RenamedFrom != "renamed-old.txt" {
		t.Fatalf("renamed-new.txt RenamedFrom = %q, want renamed-old.txt (the rename pair)", op.RenamedFrom)
	}
	if op := opFor(t, probe.candidate, "renamed-old.txt"); op.RenamedTo != "renamed-new.txt" {
		t.Fatalf("renamed-old.txt RenamedTo = %q, want renamed-new.txt (the rename pair)", op.RenamedTo)
	}
	if op := opFor(t, probe.candidate, "blob.bin"); !op.Binary {
		t.Fatal("blob.bin is not marked binary; the candidate must carry 0x00 and non-UTF-8 bytes")
	}
	if op := opFor(t, probe.candidate, "empty.txt"); op.Kind != KindCreate || op.Size != 0 {
		t.Fatalf("empty.txt = kind %s size %d, want a zero-byte create", op.Kind, op.Size)
	}
	if op := opFor(t, probe.candidate, "deep/sub/new.txt"); len(op.NewHash) == 0 {
		t.Fatal("deep/sub/new.txt has no new content; the nested create is missing")
	}

	for _, stage := range []string{StageBeforeReplace, StageAfterReplace, StageBeforeMark} {
		for _, which := range []string{"first", "middle", "last"} {
			t.Run(stage+"/"+which, func(t *testing.T) {
				f := newPublishFixture(t, t109cMultiKindBase(), t109cMultiKindResult(), nil)
				ops := f.candidate.Operations
				idx := 0
				switch which {
				case "middle":
					idx = len(ops) / 2
				case "last":
					idx = len(ops) - 1
				}
				op := ops[idx]
				seq := int64(idx + 1) // the operations slice is in publish order
				opID := fmt.Sprintf("op-nth-%s-%s", stage, which)

				in := f.input(opID)
				in.Hook = failAtContentStep(seq, stage)
				res, err := f.publish(t, in)
				if err == nil {
					t.Fatalf("Publish with a failing %s at seq %d (%s) returned no error: %+v",
						stage, seq, op.Path, res)
				}
				if !strings.Contains(err.Error(), fmt.Sprintf("seq %d stage %s", seq, stage)) {
					t.Fatalf("the error does not name the failure point: %v", err)
				}
				if res.Operation.Status != StatusRolledBack {
					t.Fatalf("operation status = %s, want rolled_back", res.Operation.Status)
				}
				assertTreeEquals(t, "the target after the rollback", f.beforeTree, snapshotTree(t, f.target))
				t109cAssertNoMergeEvent(t, f, "a failed publish")
				t109cAssertBlobsPresent(t, f.files(t, opID), f.blobs,
					"after the rollback of "+opID)
				t109cAssertLockFree(t, f, "op-nth-next-"+stage+"-"+which, "after the rollback")
			})
		}
	}
}

// --- TestRecoveryPreservesExternalEdit ---

// t109cRecoveryEditCandidate is the candidate of the external-edit constructs:
// a-mod/b-mod/c-mod are written in that order, deep/sub/created.txt is a
// nested create, and zz-new.txt is a create at the end of the publish order.
// Sorted by path: a-mod(1), b-mod(2), c-mod(3), deep/sub/created(4),
// zz-new(5).
func t109cRecoveryEditBase() map[string]string {
	return map[string]string{
		"a-mod.txt": "old a",
		"b-mod.txt": "old b",
		"c-mod.txt": "old c",
	}
}

func t109cRecoveryEditResult() map[string]string {
	return map[string]string{
		"a-mod.txt":            "new a",
		"b-mod.txt":            "new b",
		"c-mod.txt":            "new c",
		"deep/sub/created.txt": "nested",
		"zz-new.txt":           "zz new",
	}
}

// t109cCrashFixture builds the fixture, crashes a real publish at the given
// step (the hook panics; the publisher never rolls back), and returns the
// fixture plus the operation id.
func t109cCrashFixture(t *testing.T, stage string, seq int64, opID string) *publishFixture {
	t.Helper()
	f := newPublishFixture(t, t109cRecoveryEditBase(), t109cRecoveryEditResult(), nil)
	in := f.input(opID)
	in.Hook = t109cCrashAt(stage, seq)
	publishCrash(t, in)
	if op := f.operation(t, opID); op.Status != StatusApplying {
		t.Fatalf("after the crash the operation is %s, want applying (a crash may not roll back)", op.Status)
	}
	return f
}

// TestRecoveryPreservesExternalEdit drives the external-edit rule through a
// real crashed publish. Two constructs are needed, because the two required
// endings are mutually exclusive from one recovery: a rollback that restores
// the other written files parks at the edited one, and a retry of that state
// can only finish the rollback; to reach applied, the recovery must stop at the
// edit before it restores anything, which happens when the edited file is last
// in the reverse walk. Construct A pins "the edit is untouched, the rest is
// restored, the operation parks holding the lock, a retry with the old content
// rolls back"; construct B pins both retry endings (applied when the human
// restores the candidate's content, rolled_back when they restore the old
// state).
func TestRecoveryPreservesExternalEdit(t *testing.T) {
	t.Run("A: crash mid-publish, edit the first written file", func(t *testing.T) {
		// The crash point is seq 4 (deep/sub/created.txt, before its replace):
		// seq 1..3 are written and marked, the rest never reached.
		f := t109cCrashFixture(t, StageBeforeReplace, 4, "op-edit-a")
		edited := "the user's newest version of a"
		writeTestFile(t, f.target, "a-mod.txt", edited)

		report, err := Recover(context.Background(), t109cRecoverInput(f, ""))
		if err != nil {
			t.Fatalf("Recover: %v (report %+v)", err, report)
		}
		if report.Outcome != RecoverOutcomeNeedsRecovery {
			t.Fatalf("outcome = %s, want needs_recovery", report.Outcome)
		}
		if got, _ := targetFile(f.target, "a-mod.txt"); got != edited {
			t.Fatalf("a-mod.txt = %q, want the user's edit untouched", got)
		}
		// Everything else this operation had written is back at its old
		// content, and the directories its create had made are gone.
		for rel, want := range map[string]string{"b-mod.txt": "old b", "c-mod.txt": "old c"} {
			if got, _ := targetFile(f.target, rel); got != want {
				t.Fatalf("%s = %q, want %q", rel, got, want)
			}
		}
		if _, ok := targetFile(f.target, "deep/sub/created.txt"); ok {
			t.Fatal("deep/sub/created.txt still exists; the create was not undone")
		}
		if _, err := os.Stat(filepath.Join(f.target, "deep")); !os.IsNotExist(err) {
			t.Fatalf("deep/ still exists, want the created directories removed: %v", err)
		}
		want := t109cRecoveryEditBase()
		want["a-mod.txt"] = edited
		got := snapshotTree(t, f.target)
		for rel, content := range want {
			if got[rel] != "file:"+content {
				t.Fatalf("tree[%s] = %q, want %q (the edit is the only difference)", rel, got[rel], "file:"+content)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("the tree has %d entries, want %d: %v", len(got), len(want), got)
		}
		t109cAssertNoMergeEvent(t, f, "after the parked recovery")
		t109cAssertLockHeld(t, f, "op-edit-a", "op-edit-a-next", "after the parked recovery")
		// The lock is real: a fresh publish on the root is refused by the
		// candidate checks before anything is written.
		res, err := f.publish(t, f.input("op-edit-a-second"))
		if !errors.Is(err, ErrRootBusy) {
			t.Fatalf("a new Publish on the needs_recovery root = %v (result %+v), want ErrRootBusy", err, res)
		}
		if !strings.Contains(err.Error(), "op-edit-a") {
			t.Fatalf("ErrRootBusy does not name the occupant: %v", err)
		}

		// The human puts the old content back: the retry can only roll back.
		writeTestFile(t, f.target, "a-mod.txt", "old a")
		retry, err := RetryRecovery(context.Background(), t109cRecoverInput(f, ""))
		if err != nil {
			t.Fatalf("RetryRecovery: %v (report %+v)", err, retry)
		}
		if retry.Outcome != RecoverOutcomeRolledBack {
			t.Fatalf("retry outcome = %s, want rolled_back", retry.Outcome)
		}
		assertTreeEquals(t, "the target after the retry", f.beforeTree, snapshotTree(t, f.target))
		t109cAssertNoMergeEvent(t, f, "after the retried rollback")
		t109cAssertRecoverIsIdempotent(t, f, "op-edit-a", "construct A")
		t109cAssertLockFree(t, f, "op-edit-a-done", "after the retried rollback")
		t109cAssertBlobsPresent(t, f.files(t, "op-edit-a"), f.blobs, "construct A")
	})

	t.Run("B: crash before the event, edit the last file", func(t *testing.T) {
		// The crash is in transaction 3 (before_event): every file is written
		// and marked, the status move and the event rolled back together, so
		// the journal says applying and the target is fully new.
		f := t109cCrashFixture(t, StageBeforeEvent, 0, "op-edit-b")
		edited := "the user's newest version of zz"
		writeTestFile(t, f.target, "zz-new.txt", edited)

		report, err := Recover(context.Background(), t109cRecoverInput(f, ""))
		if err != nil {
			t.Fatalf("Recover: %v (report %+v)", err, report)
		}
		if report.Outcome != RecoverOutcomeNeedsRecovery {
			t.Fatalf("outcome = %s, want needs_recovery", report.Outcome)
		}
		if got, _ := targetFile(f.target, "zz-new.txt"); got != edited {
			t.Fatalf("zz-new.txt = %q, want the user's edit untouched", got)
		}
		// zz-new.txt is first in the reverse walk, so the rollback parked
		// before restoring anything: the other files are still new.
		for rel, want := range map[string]string{
			"a-mod.txt": "new a", "b-mod.txt": "new b", "c-mod.txt": "new c",
		} {
			if got, _ := targetFile(f.target, rel); got != want {
				t.Fatalf("%s = %q, want %q (the park must come before any restore)", rel, got, want)
			}
		}
		if got, _ := targetFile(f.target, "deep/sub/created.txt"); got != "nested" {
			t.Fatalf("deep/sub/created.txt = %q, want the published content", got)
		}
		t109cAssertNoMergeEvent(t, f, "after the parked recovery")
		t109cAssertLockHeld(t, f, "op-edit-b", "op-edit-b-next", "after the parked recovery")

		// B1: the human restores the candidate's content — the recovery may
		// finish the apply. A fresh crash fixture, because B2 needs the parked
		// state too and this Recover is terminal.
		writeTestFile(t, f.target, "zz-new.txt", "zz new")
		retry, err := RetryRecovery(context.Background(), t109cRecoverInput(f, ""))
		if err != nil {
			t.Fatalf("RetryRecovery: %v (report %+v)", err, retry)
		}
		if retry.Outcome != RecoverOutcomeApplied {
			t.Fatalf("retry outcome = %s, want applied", retry.Outcome)
		}
		if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 1 {
			t.Fatalf("merge.completed events = %d, want exactly 1", n)
		}
		wantTree := map[string]string{
			"a-mod.txt":            "file:new a",
			"b-mod.txt":            "file:new b",
			"c-mod.txt":            "file:new c",
			"deep/":                "dir",
			"deep/sub/":            "dir",
			"deep/sub/created.txt": "file:nested",
			"zz-new.txt":           "file:zz new",
		}
		assertTreeEquals(t, "the target after the applied retry", wantTree, snapshotTree(t, f.target))
		t109cAssertRecoverIsIdempotent(t, f, "op-edit-b", "construct B1")
		t109cAssertLockFree(t, f, "op-edit-b-done", "after the applied retry")
		t109cAssertBlobsPresent(t, f.files(t, "op-edit-b"), f.blobs, "construct B1")
	})

	t.Run("B2: the human restores the old state (absent), the retry rolls back", func(t *testing.T) {
		f := t109cCrashFixture(t, StageBeforeEvent, 0, "op-edit-b2")
		writeTestFile(t, f.target, "zz-new.txt", "the user's newest version of zz")
		report, err := Recover(context.Background(), t109cRecoverInput(f, ""))
		if err != nil || report.Outcome != RecoverOutcomeNeedsRecovery {
			t.Fatalf("first Recover: outcome %s err %v, want needs_recovery", report.Outcome, err)
		}
		// For a create, "the old content" is absence: the human deletes the
		// file the operation created.
		removeTestFile(t, f.target, "zz-new.txt")
		retry, err := RetryRecovery(context.Background(), t109cRecoverInput(f, ""))
		if err != nil {
			t.Fatalf("RetryRecovery: %v (report %+v)", err, retry)
		}
		if retry.Outcome != RecoverOutcomeRolledBack {
			t.Fatalf("retry outcome = %s, want rolled_back", retry.Outcome)
		}
		assertTreeEquals(t, "the target after the retried rollback", f.beforeTree, snapshotTree(t, f.target))
		t109cAssertNoMergeEvent(t, f, "after the retried rollback")
		t109cAssertRecoverIsIdempotent(t, f, "op-edit-b2", "construct B2")
		t109cAssertLockFree(t, f, "op-edit-b2-done", "after the retried rollback")
		t109cAssertBlobsPresent(t, f.files(t, "op-edit-b2"), f.blobs, "construct B2")
	})
}

// --- TestConcurrentRootsShareLock ---

// t109cPubOutcome carries one goroutine's Publish result back to the test.
type t109cPubOutcome struct {
	res PublishResult
	err error
}

func t109cPublishInBackground(f *publishFixture, in PublishInput) (chan t109cPubOutcome, chan struct{}) {
	out := make(chan t109cPubOutcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := Publish(context.Background(), in)
		out <- t109cPubOutcome{res: res, err: err}
	}()
	return out, done
}

// TestConcurrentRootsShareLock gives the same directory two spellings — the
// real path and a junction (symlink elsewhere) pointing at it — and requires
// that the two spellings' captured root identities are equal, that a publish
// through one spelling while the other holds the root lock is refused with
// ErrRootBusy and writes nothing, and that a genuinely different directory is
// not blocked by either.
func TestConcurrentRootsShareLock(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old", "b.txt": "old"},
		map[string]string{"a.txt": "new", "b.txt": "new"},
		nil)
	before := snapshotTree(t, f.target)

	// The second spelling: a junction (Windows) or symlink to the target.
	via := filepath.Join(t.TempDir(), "via-link")
	makeDirLink(t, via, f.target)
	viaRoot, err := workspace.CaptureRootIdentity(via)
	if err != nil {
		t.Fatalf("CaptureRootIdentity(%s): %v", via, err)
	}
	if !viaRoot.Equal(f.root) {
		t.Fatalf("the identity captured through %s is %s, the real path's is %s; the shared lock rests on this equality",
			via, viaRoot, f.root)
	}
	if viaRoot.String() != f.root.String() {
		t.Fatalf("the two spellings produce different root keys: %s vs %s", viaRoot.String(), f.root.String())
	}

	// The winner holds the root from inside its file loop; the loop has
	// committed the operation row (the lock) and written nothing yet.
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce, releaseOnce sync.Once
	winnerIn := f.input("op-winner-4")
	winnerIn.Hook = func(s Step) error {
		if s.Seq == 1 && s.Stage == StageBeforeReplace {
			startedOnce.Do(func() { close(started) })
			<-release
		}
		return nil
	}
	winnerOut, winnerDone := t109cPublishInBackground(f, winnerIn)
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		<-winnerDone
	})
	<-started

	// The loser publishes the same directory through the other spelling. The
	// lock key comes from the root identity, so this must be the same root.
	loserFs := newFaultFS()
	loserIn := f.input("op-loser-4")
	loserIn.TargetRoot = via
	loserIn.ExpectedRoot = viaRoot
	loserIn.FS = loserFs
	loserIn.Candidate = retargetCandidate(t, f.candidate, via)
	loserOut, loserDone := t109cPublishInBackground(f, loserIn)
	loser := <-loserOut
	<-loserDone
	if !errors.Is(loser.err, ErrRootBusy) {
		t.Fatalf("the publish through %s while %s held the root = %v (result %+v), want ErrRootBusy",
			via, f.target, loser.err, loser.res)
	}
	if !strings.Contains(loser.err.Error(), "op-winner-4") {
		t.Fatalf("ErrRootBusy does not name the occupant: %v", loser.err)
	}
	if n := loserFs.replaces(); n != 0 {
		t.Fatalf("the refused publish through the other spelling replaced %d files, want 0", n)
	}
	if _, err := GetOperation(context.Background(), f.store.DB(), "op-loser-4"); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("the refused publish left a journal row: %v", err)
	}
	assertTreeEquals(t, "the target while the winner is held", before, snapshotTree(t, f.target))

	// A genuinely different directory must not be blocked by the winner: its
	// publish completes to applied while the winner is still inside its loop.
	second := t.TempDir()
	writeTestFile(t, second, "a.txt", "old")
	writeTestFile(t, second, "b.txt", "old")
	secondRoot, err := workspace.CaptureRootIdentity(second)
	if err != nil {
		t.Fatalf("CaptureRootIdentity(%s): %v", second, err)
	}
	secondIn := f.input("op-second-4")
	secondIn.TargetRoot = second
	secondIn.ExpectedRoot = secondRoot
	secondIn.FS = newFaultFS()
	secondIn.Candidate = retargetCandidate(t, f.candidate, second)
	secondRes, err := f.publish(t, secondIn)
	if err != nil {
		t.Fatalf("a publish on a different directory while the first is held: %v (result %+v)", err, secondRes)
	}
	if secondRes.Operation.Status != StatusApplied {
		t.Fatalf("the different directory's operation = %s, want applied", secondRes.Operation.Status)
	}
	if got, _ := targetFile(second, "a.txt"); got != "new" {
		t.Fatalf("the different directory's a.txt = %q, want the new content", got)
	}

	// Release the winner: it finishes its own publish.
	releaseOnce.Do(func() { close(release) })
	<-winnerDone
	winner := <-winnerOut
	if winner.err != nil {
		t.Fatalf("the winner publish: %v (result %+v)", winner.err, winner.res)
	}
	if winner.res.Operation.Status != StatusApplied {
		t.Fatalf("the winner's status = %s, want applied", winner.res.Operation.Status)
	}
	if got, _ := targetFile(f.target, "a.txt"); got != "new" {
		t.Fatalf("the winner's a.txt = %q, want the new content", got)
	}
	t109cAssertLockFree(t, f, "op-after-4", "after the winner finished")
}

// --- the crash matrix ---

// TestMergeCrashAtEveryPublishPoint crashes a real publish at every injection
// point the publisher offers — before_replace, after_replace, before_mark for
// every file position, and before_event — and then recovers. The crash stops
// the publisher where the hook fired: no rollback, no further journal write,
// the operation left applying. The recovery's ending is decided by what the
// target matches, not by which stage the hook fired at, and the matrix pins
// both:
//
//   - applied (exactly one merge.completed, target equal to the result tree)
//     when every path already holds the new side: before_event always, and a
//     crash after the content step of the last file when that step was a
//     delete (an applied delete is also its own "written" state);
//   - rolled_back (target byte-equal to before the publish, root lock
//     released, no event) for every other point.
//
// Every case also checks, row by row, that no journal line names a blob the
// store does not have, and that a second recovery is a no-op.
func TestMergeCrashAtEveryPublishPoint(t *testing.T) {
	// One fixture to read the publish order from; the bodies build their own.
	probe := newPublishFixture(t, t109cMultiKindBase(), t109cMultiKindResult(), nil)
	ops := probe.candidate.Operations
	if len(ops) != 7 {
		t.Fatalf("the multi-kind candidate has %d operations, the matrix is written for 7: %+v", len(ops), ops)
	}
	// The outcome of one case, from the crash point alone: a crash after the
	// content step of the last file matches the result tree exactly when that
	// file's "written" state is its absence (a delete). The last file is
	// renamed-old.txt (seq 7) in this candidate; the check derives it instead
	// of assuming it.
	applied := func(stage string, seq int64) bool {
		if seq == 0 {
			return true
		}
		if seq != int64(len(ops)) {
			return false
		}
		if stage != StageAfterReplace && stage != StageBeforeMark {
			return false
		}
		return ops[len(ops)-1].Kind == KindDelete
	}
	if !applied(StageBeforeEvent, 0) {
		t.Fatal("the before_event case must recover to applied")
	}
	if got := applied(StageAfterReplace, 7); !got {
		t.Fatalf("the candidate's last file is %s (%s); the matrix expects a delete after whose content step the target matches",
			ops[len(ops)-1].Path, ops[len(ops)-1].Kind)
	}

	body := func(t *testing.T, stage string, seq int64, opID string) {
		f := newPublishFixture(t, t109cMultiKindBase(), t109cMultiKindResult(), nil)
		in := f.input(opID)
		in.Hook = t109cCrashAt(stage, seq)
		publishCrash(t, in)

		// The crash really stopped the publisher: applying, no rollback, no
		// completion, and the row states exactly where the loop was.
		if op := f.operation(t, opID); op.Status != StatusApplying {
			t.Fatalf("after the crash the operation is %s, want applying", op.Status)
		}
		t109cAssertNoMergeEvent(t, f, "after the crash")
		for _, row := range f.files(t, opID) {
			cop := t109cOpForSeq(t, f.candidate, row.Seq)
			switch {
			case seq == 0 || row.Seq < seq:
				if row.State != FileStateWritten {
					t.Fatalf("%s seq %d: row state after the crash = %s, want written", opID, row.Seq, row.State)
				}
				t109cAssertPathAt(t, f.target, cop, "new")
			case row.Seq == seq && stage == StageBeforeReplace:
				if row.State != FileStatePending {
					t.Fatalf("%s seq %d: row state = %s, want pending (the content step never ran)", opID, row.Seq, row.State)
				}
				t109cAssertPathAt(t, f.target, cop, "old")
			case row.Seq == seq:
				// The content step ran (the disk changed), the journal mark
				// did not: the row is pending and the disk holds the new side.
				if row.State != FileStatePending {
					t.Fatalf("%s seq %d: row state = %s, want pending (the mark was never written)", opID, row.Seq, row.State)
				}
				t109cAssertPathAt(t, f.target, cop, "new")
			default:
				if row.State != FileStatePending {
					t.Fatalf("%s seq %d: row state = %s, want pending (the loop never reached it)", opID, row.Seq, row.State)
				}
				t109cAssertPathAt(t, f.target, cop, "old")
			}
		}
		wantApplied := applied(stage, seq)
		if wantApplied {
			// The crash left the target matching the candidate completely (for
			// before_event with every mark too, for the last-file delete with
			// its row still pending — the recovery fills the mark).
			assertTreeEquals(t, "the target before the recovery (fully new)",
				t109cMultiKindResultTree(), snapshotTree(t, f.target))
		}

		report, err := Recover(context.Background(), t109cRecoverInput(f, ""))
		if err != nil {
			t.Fatalf("Recover after the crash at %s seq %d: %v (report %+v)", stage, seq, err, report)
		}
		if wantApplied {
			if report.Outcome != RecoverOutcomeApplied {
				t.Fatalf("outcome = %s, want applied (every file matched)", report.Outcome)
			}
			if report.Event == nil || report.Event.Type != string(run.EventMergeCompleted) {
				t.Fatalf("no merge.completed event in the report: %+v", report.Event)
			}
			if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 1 {
				t.Fatalf("merge.completed events = %d, want exactly 1", n)
			}
			assertTreeEquals(t, "the target after the recovered apply",
				t109cMultiKindResultTree(), snapshotTree(t, f.target))
		} else {
			if report.Outcome != RecoverOutcomeRolledBack {
				t.Fatalf("outcome = %s, want rolled_back", report.Outcome)
			}
			assertTreeEquals(t, "the target after the recovered rollback", f.beforeTree, snapshotTree(t, f.target))
			t109cAssertNoMergeEvent(t, f, "after the recovered rollback")
		}
		t109cAssertBlobsPresent(t, f.files(t, opID), f.blobs,
			fmt.Sprintf("after the recovery at %s seq %d", stage, seq))

		// Requirement 6: the recovery is idempotent. This runs before the lock
		// is claimed again, or the start-up form would pick up the new claim.
		t109cAssertRecoverIsIdempotent(t, f, opID, fmt.Sprintf("%s seq %d", stage, seq))
		t109cAssertLockFree(t, f, fmt.Sprintf("op-next-%s-%d", stage, seq), "after the recovery")
	}

	for _, stage := range []string{StageBeforeReplace, StageAfterReplace, StageBeforeMark} {
		for i := range ops {
			seq, path := int64(i+1), ops[i].Path
			t.Run(fmt.Sprintf("%s/seq%d-%s", stage, seq, path), func(t *testing.T) {
				body(t, stage, seq, fmt.Sprintf("op-crash-%s-%d", stage, seq))
			})
		}
	}
	t.Run(StageBeforeEvent, func(t *testing.T) {
		body(t, StageBeforeEvent, 0, "op-crash-before-event")
	})
}
