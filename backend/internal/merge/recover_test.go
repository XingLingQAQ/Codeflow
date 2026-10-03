package merge

// The crash recovery tests (T1.09.b group 4). Every construct here is a
// "what the journal and the directory look like after the process died at this
// step" — a real file database, a real target directory, real blobs — and every
// assertion is about the three things a recovery is judged by: the outcome, the
// tree byte for byte (structure included), and the root lock.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/codeflow/backend/internal/workspace"
)

// recoverFileSpec is one path of the crash fixture: the kind of operation, the
// content the target holds before it, and the content the operation writes.
type recoverFileSpec struct {
	rel  string
	kind string
	old  string
	neu  string
}

// recoverFixture is an operation of the journal, at a chosen step, with a real
// target directory and a real blob store behind it. The fixture never runs
// Publish: it builds the state a crash would leave, because that is what a
// recovery must read.
type recoverFixture struct {
	t      *testing.T
	store  *runstore.Store
	blobs  *countingBlobs
	fs     *faultFS
	target string
	root   workspace.RootIdentity
	specs  []recoverFileSpec
	// before is the target exactly as it was before the operation started.
	before map[string]string
	now    time.Time
}

// newRecoverFixture builds the fixture. An empty spec list is a prepared
// operation (no file rows); otherwise the file list is committed with
// WriteFilesTx and the operation is applying, with the target still holding the
// pre-publish state — the crash constructs mutate it from there.
func newRecoverFixture(t *testing.T, specs []recoverFileSpec) *recoverFixture {
	t.Helper()
	st, _ := mergeDB(t)
	target := t.TempDir()
	f := &recoverFixture{
		t:      t,
		store:  st,
		blobs:  newCountingBlobs(),
		fs:     newFaultFS(),
		target: target,
		specs:  specs,
		now:    time.UnixMilli(1700002000000).UTC(),
	}

	for _, s := range specs {
		if s.kind != KindCreate {
			writeTestFile(t, target, s.rel, s.old)
		}
	}
	root, err := workspace.CaptureRootIdentity(target)
	if err != nil {
		t.Fatalf("CaptureRootIdentity: %v", err)
	}
	f.root = root
	f.before = snapshotTree(t, target)

	op := createOperation(t, st, journalOpInput("op-rec", root.String()))
	if len(specs) == 0 {
		return f
	}

	files := make([]MergeFile, len(specs))
	for i, s := range specs {
		mf := MergeFile{Seq: int64(i + 1), Path: s.rel, Kind: s.kind, State: FileStatePending}
		switch s.kind {
		case KindCreate:
			newHash := hashOf([]byte(s.neu))
			mf.NewHash = &newHash
			mf.NewMode = u32ptr(0o644)
			mf.CreatedDirs = f.missingDirs(s.rel)
		case KindModify:
			oldHash, newHash := hashOf([]byte(s.old)), hashOf([]byte(s.neu))
			mf.OldHash, mf.BackupHash, mf.NewHash = &oldHash, &oldHash, &newHash
			mf.OldMode, mf.NewMode = u32ptr(0o644), u32ptr(0o644)
		case KindDelete:
			oldHash := hashOf([]byte(s.old))
			mf.OldHash, mf.BackupHash = &oldHash, &oldHash
			mf.OldMode = u32ptr(0o644)
		default:
			t.Fatalf("fixture: unknown kind %q", s.kind)
		}
		files[i] = mf
	}
	// Blobs first, rows second: the journal may never name a blob that is not
	// in the store, so the fixture stores the same way the publisher does.
	for _, s := range specs {
		if s.old != "" {
			f.putBlob(s.old)
		}
		if s.neu != "" {
			f.putBlob(s.neu)
		}
	}
	writeFiles(t, st, op, files)
	return f
}

func (f *recoverFixture) spec(rel string) recoverFileSpec {
	f.t.Helper()
	for _, s := range f.specs {
		if s.rel == rel {
			return s
		}
	}
	f.t.Fatalf("no fixture spec for %s", rel)
	return recoverFileSpec{}
}

func (f *recoverFixture) putBlob(content string) {
	f.t.Helper()
	hash, _, err := f.blobs.Put(context.Background(), strings.NewReader(content))
	if err != nil {
		f.t.Fatalf("put blob: %v", err)
	}
	if hash != hashOf([]byte(content)) {
		f.t.Fatalf("blob store returned %s for content hashing to %s", hash, hashOf([]byte(content)))
	}
}

// missingDirs is the journal's created_dirs for a path: the ancestors that do
// not exist yet, parents first. It mirrors the publisher's plan for the same
// target state.
func (f *recoverFixture) missingDirs(rel string) []string {
	f.t.Helper()
	var out []string
	for _, dir := range ancestorDirs(rel) {
		if _, err := os.Stat(filepath.Join(f.target, filepath.FromSlash(dir))); err == nil {
			continue
		}
		out = append(out, dir)
	}
	return out
}

// --- simulating the crashed publish's disk state ---

// writeNew simulates the content step of one file: the new content lands, or,
// for a delete, the path is removed. It is the "the disk changed, the journal
// did not" half of every crash construct.
func (f *recoverFixture) writeNew(rel string) {
	f.t.Helper()
	s := f.spec(rel)
	if s.kind == KindDelete {
		f.removeRel(rel)
		return
	}
	writeTestFile(f.t, f.target, rel, s.neu)
}

func (f *recoverFixture) writeRaw(rel, content string) {
	f.t.Helper()
	writeTestFile(f.t, f.target, rel, content)
}

func (f *recoverFixture) removeRel(rel string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.target, filepath.FromSlash(rel))); err != nil {
		f.t.Fatalf("remove %s: %v", rel, err)
	}
}

// mkdirs creates the directories a create planned — the state a crash between
// "the directories were made" and "the file was written" leaves behind.
func (f *recoverFixture) mkdirs(rel string) {
	f.t.Helper()
	for _, dir := range ancestorDirs(rel) {
		if err := os.MkdirAll(filepath.Join(f.target, filepath.FromSlash(dir)), 0o755); err != nil {
			f.t.Fatalf("mkdirs %s: %v", dir, err)
		}
	}
}

// mark performs one file-state CAS the way the publisher would have.
func (f *recoverFixture) mark(seq int64, from, to string) {
	f.t.Helper()
	if err := f.store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		return MarkFileTx(ctx, tx, "op-rec", seq, from, to, time.UnixMilli(1700001005000).UTC())
	}); err != nil {
		f.t.Fatalf("mark %d %s -> %s: %v", seq, from, to, err)
	}
}

// status moves the operation the way the publisher would have.
func (f *recoverFixture) status(from, to string) {
	f.t.Helper()
	casStatus(f.t, f.store, f.op(), from, to)
}

// --- running the recovery ---

func (f *recoverFixture) input() RecoverInput {
	return RecoverInput{
		Store:        f.store,
		Blobs:        f.blobs,
		TargetRoot:   f.target,
		ExpectedRoot: f.root,
		Recoverer:    "test-recoverer",
		Now:          func() time.Time { return f.now },
		FS:           f.fs,
		Destinations: []string{"ws:project:p-1", "ws:run:r-1", "audit"},
		// OperationID stays empty on purpose: this is the start-up path's form
		// ("whatever holds this root's lock"), not a pinned row.
	}
}

func (f *recoverFixture) recover() (RecoverReport, error) {
	f.t.Helper()
	return Recover(context.Background(), f.input())
}

func (f *recoverFixture) retry() (RecoverReport, error) {
	f.t.Helper()
	return RetryRecovery(context.Background(), f.input())
}

func (f *recoverFixture) op() MergeOperation {
	f.t.Helper()
	return getOperation(f.t, f.store, "op-rec")
}

func (f *recoverFixture) rows() []MergeFile {
	f.t.Helper()
	files, err := ListFiles(context.Background(), f.store.DB(), "op-rec")
	if err != nil {
		f.t.Fatalf("ListFiles: %v", err)
	}
	return files
}

func (f *recoverFixture) assertTree(what string) {
	f.t.Helper()
	assertTreeEquals(f.t, what, f.before, snapshotTree(f.t, f.target))
}

func (f *recoverFixture) claimError(id string) error {
	return f.store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		_, err := CreateOperationTx(ctx, tx, journalOpInput(id, f.root.String()))
		return err
	})
}

func (f *recoverFixture) assertLocked() {
	f.t.Helper()
	if err := f.claimError("op-after"); !errors.Is(err, ErrRootBusy) {
		f.t.Fatalf("a new operation on the recovered root = %v, want ErrRootBusy", err)
	}
}

func (f *recoverFixture) assertUnlocked() {
	f.t.Helper()
	if err := f.claimError("op-after"); err != nil {
		f.t.Fatalf("the root lock was not released: %v", err)
	}
}

// forget removes one blob from the store, simulating a store that lost (or
// never committed) a backup the journal names.
func (b *countingBlobs) forget(hash string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.contents, hash)
}

// --- the per-state rules ---

// TestRecoverPreparedTouchesNothingAndReleasesTheRoot: a prepared operation has
// no file list (WriteFilesTx inserts the rows and moves to applying in one
// transaction), so no target byte was written and the recovery's only job is to
// release the root lock with a reason recorded.
func TestRecoverPreparedTouchesNothingAndReleasesTheRoot(t *testing.T) {
	f := newRecoverFixture(t, nil)
	replacesBefore := f.fs.replaces()

	res, err := f.recover()
	if err != nil {
		t.Fatalf("Recover of a prepared operation: %v (report %+v)", err, res)
	}
	if res.Outcome != RecoverOutcomeRolledBack {
		t.Fatalf("outcome = %s, want rolled_back", res.Outcome)
	}
	op := f.op()
	if op.Status != StatusRolledBack {
		t.Fatalf("operation status = %s, want rolled_back", op.Status)
	}
	if op.FailureCode == nil || *op.FailureCode != recoveryPreparedCode {
		t.Fatalf("failure code = %v, want %s", op.FailureCode, recoveryPreparedCode)
	}
	if n := f.fs.replaces(); n != replacesBefore {
		t.Fatalf("a prepared recovery replaced %d files, want 0", n-replacesBefore)
	}
	if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 0 {
		t.Fatalf("merge.completed events = %d, want 0", n)
	}
	f.assertTree("the target after a prepared recovery")
	f.assertUnlocked()
}

// TestRecoverCompletesAFullyWrittenTarget: every path already holds the new
// content while every row is still pending (the marks were swallowed). That is
// §27.5 item 5's "完整目标匹配": the recovery fills the marks, moves to applied
// and writes merge.completed, all in one transaction.
func TestRecoverCompletesAFullyWrittenTarget(t *testing.T) {
	f := newRecoverFixture(t, []recoverFileSpec{
		{rel: "mod.txt", kind: KindModify, old: "old mod", neu: "new mod"},
		{rel: "sub/new.txt", kind: KindCreate, neu: "created"},
		{rel: "gone.txt", kind: KindDelete, old: "bye"},
	})
	f.writeNew("mod.txt")
	f.writeNew("sub/new.txt")
	f.writeNew("gone.txt")

	res, err := f.recover()
	if err != nil {
		t.Fatalf("Recover: %v (report %+v)", err, res)
	}
	if res.Outcome != RecoverOutcomeApplied {
		t.Fatalf("outcome = %s, want applied", res.Outcome)
	}
	op := f.op()
	if op.Status != StatusApplied {
		t.Fatalf("operation status = %s, want applied", op.Status)
	}
	if res.Event == nil || res.Event.Type != string(run.EventMergeCompleted) {
		t.Fatalf("no merge.completed event in the report: %+v", res.Event)
	}
	if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 1 {
		t.Fatalf("merge.completed events = %d, want exactly 1", n)
	}
	if n := countOutbox(t, f.store); n != len(f.input().Destinations) {
		t.Fatalf("outbox rows = %d, want %d", n, len(f.input().Destinations))
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Event.Payload, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload["operation_id"] != "op-rec" || payload["file_count"] != float64(3) {
		t.Fatalf("payload = %v, want operation_id op-rec and file_count 3", payload)
	}
	for _, row := range f.rows() {
		if row.State != FileStateWritten {
			t.Errorf("file %s is in state %s, want written (the recovery must fill the marks)", row.Path, row.State)
		}
	}
	want := map[string]string{
		"mod.txt":     "file:new mod",
		"sub/":        "dir",
		"sub/new.txt": "file:created",
	}
	assertTreeEquals(t, "the target after the recovered apply", want, snapshotTree(t, f.target))
	f.assertUnlocked()
}

// TestRecoverRollsBackFromEveryCrashPoint walks the constructs a crash can
// leave in an applying (or half-rolled-back) operation and requires the same
// three facts of each: the outcome, the tree byte for byte, and the released
// lock. No merge.completed may exist for any of them.
func TestRecoverRollsBackFromEveryCrashPoint(t *testing.T) {
	specs := []recoverFileSpec{
		{rel: "mod.txt", kind: KindModify, old: "old mod", neu: "new mod"},
		{rel: "sub/new.txt", kind: KindCreate, neu: "created"},
		{rel: "gone.txt", kind: KindDelete, old: "bye"},
	}
	cases := []struct {
		name  string
		setup func(*recoverFixture)
	}{
		{"a write on disk whose mark was swallowed", func(f *recoverFixture) {
			f.writeNew("mod.txt")
		}},
		{"a write on disk, marked written", func(f *recoverFixture) {
			f.writeNew("mod.txt")
			f.mark(1, FileStatePending, FileStateWritten)
		}},
		{"a delete executed, mark swallowed", func(f *recoverFixture) {
			f.removeRel("gone.txt")
		}},
		{"a create's directories made, file never written", func(f *recoverFixture) {
			f.mkdirs("sub/new.txt")
		}},
		{"a rollback done half way", func(f *recoverFixture) {
			f.writeNew("mod.txt")
			f.mark(1, FileStatePending, FileStateWritten)
			f.status(StatusApplying, StatusRollingBack)
			f.writeRaw("mod.txt", "old mod")
			f.mark(1, FileStateWritten, FileStateRestored)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRecoverFixture(t, specs)
			tc.setup(f)

			res, err := f.recover()
			if err != nil {
				t.Fatalf("Recover: %v (report %+v)", err, res)
			}
			if res.Outcome != RecoverOutcomeRolledBack {
				t.Fatalf("outcome = %s, want rolled_back", res.Outcome)
			}
			op := f.op()
			if op.Status != StatusRolledBack {
				t.Fatalf("operation status = %s, want rolled_back", op.Status)
			}
			if op.FailureCode == nil || *op.FailureCode != recoveryRolledBackCode {
				t.Fatalf("failure code = %v, want %s", op.FailureCode, recoveryRolledBackCode)
			}
			if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 0 {
				t.Fatalf("merge.completed events = %d, want 0", n)
			}
			f.assertTree("the target after the recovery")
			f.assertUnlocked()
		})
	}
}

// TestRecoverLeavesAnExternalEditAndParks: the one path of the operation that
// was written was edited afterwards. The user's version is newer than both the
// operation's and its backup, so not one byte of it is touched; the row records
// external_edit and the operation parks in needs_recovery, which holds the root
// lock.
func TestRecoverLeavesAnExternalEditAndParks(t *testing.T) {
	f := newRecoverFixture(t, []recoverFileSpec{
		{rel: "mod.txt", kind: KindModify, old: "old mod", neu: "new mod"},
	})
	f.writeNew("mod.txt")
	f.mark(1, FileStatePending, FileStateWritten)
	edited := "the user's newest version"
	f.writeRaw("mod.txt", edited)
	replacesBefore := f.fs.replaces()

	res, err := f.recover()
	if err != nil {
		t.Fatalf("Recover: %v (report %+v)", err, res)
	}
	if res.Outcome != RecoverOutcomeNeedsRecovery {
		t.Fatalf("outcome = %s, want needs_recovery", res.Outcome)
	}
	if got, ok := targetFile(f.target, "mod.txt"); !ok || got != edited {
		t.Fatalf("mod.txt = %q (present %v), want the user's edit untouched", got, ok)
	}
	if n := f.fs.replaces(); n != replacesBefore {
		t.Fatalf("the recovery replaced %d files, want 0 (the path must not be touched)", n-replacesBefore)
	}
	if row := f.rows()[0]; row.State != FileStateExternalEdit {
		t.Fatalf("file row state = %s, want external_edit", row.State)
	}
	op := f.op()
	if op.Status != StatusNeedsRecovery {
		t.Fatalf("operation status = %s, want needs_recovery", op.Status)
	}
	if len(res.Reasons) == 0 {
		t.Fatal("the report gives no reason for needs_recovery")
	}
	f.assertLocked()
}

// TestRecoverRefusesALinkSwappedIntoTheTarget replaces a directory of the
// target with a link to a directory outside the root that holds exactly the new
// content — a recovery that followed the link would classify the path as
// written and "restore" the old content into the outside directory. The link is
// refused, nothing outside changes, and the operation parks.
func TestRecoverRefusesALinkSwappedIntoTheTarget(t *testing.T) {
	f := newRecoverFixture(t, []recoverFileSpec{
		{rel: "sub/file.txt", kind: KindModify, old: "old", neu: "new"},
	})
	f.writeNew("sub/file.txt")
	f.mark(1, FileStatePending, FileStateWritten)

	outside := t.TempDir()
	writeTestFile(t, outside, "file.txt", "new")
	outsideBefore := snapshotTree(t, outside)
	realBefore := snapshotTree(t, filepath.Join(f.target, "sub"))
	if err := swapDirForLink(t, f.target, "sub", outside); err != nil {
		t.Fatalf("swap sub for a link: %v", err)
	}
	replacesBefore := f.fs.replaces()

	res, err := f.recover()
	if err != nil {
		t.Fatalf("Recover: %v (report %+v)", err, res)
	}
	if res.Outcome != RecoverOutcomeNeedsRecovery {
		t.Fatalf("outcome = %s, want needs_recovery", res.Outcome)
	}
	assertTreeEquals(t, "the directory outside the root", outsideBefore, snapshotTree(t, outside))
	assertTreeEquals(t, "the user's real sub/ (moved aside)", realBefore, snapshotTree(t, filepath.Join(f.target, "sub-moved")))
	if n := f.fs.replaces(); n != replacesBefore {
		t.Fatalf("the recovery replaced %d files through a link, want 0", n-replacesBefore)
	}
	if row := f.rows()[0]; row.State != FileStateExternalEdit {
		t.Fatalf("file row state = %s, want external_edit (the path was left for a human)", row.State)
	}
	if op := f.op(); op.Status != StatusNeedsRecovery {
		t.Fatalf("operation status = %s, want needs_recovery", op.Status)
	}
	f.assertLocked()
}

// TestRecoverWithAMissingBackupBlobWritesNothing: the journal names a backup
// the store does not have. Writing anything for that path would put an unknown
// version of the user's file in place of the known one (or delete it without
// the content to put back), so the recovery writes nothing at all and parks.
func TestRecoverWithAMissingBackupBlobWritesNothing(t *testing.T) {
	// The second file is never written, so the operation must roll back — and
	// the first file's backup is the one the store no longer has.
	f := newRecoverFixture(t, []recoverFileSpec{
		{rel: "mod.txt", kind: KindModify, old: "old mod", neu: "new mod"},
		{rel: "other.txt", kind: KindModify, old: "old other", neu: "new other"},
	})
	f.writeNew("mod.txt")
	f.mark(1, FileStatePending, FileStateWritten)
	before := snapshotTree(t, f.target)
	f.blobs.forget(hashOf([]byte("old mod")))
	replacesBefore := f.fs.replaces()

	res, err := f.recover()
	if err != nil {
		t.Fatalf("Recover: %v (report %+v)", err, res)
	}
	if res.Outcome != RecoverOutcomeNeedsRecovery {
		t.Fatalf("outcome = %s, want needs_recovery", res.Outcome)
	}
	if n := f.fs.replaces(); n != replacesBefore {
		t.Fatalf("the recovery replaced %d files without their backup, want 0", n-replacesBefore)
	}
	assertTreeEquals(t, "the target after the missing-blob recovery", before, snapshotTree(t, f.target))
	if op := f.op(); op.Status != StatusNeedsRecovery {
		t.Fatalf("operation status = %s, want needs_recovery", op.Status)
	}
	f.assertLocked()
}

// TestRecoverStopsALivePublisher holds a real publisher inside its file loop —
// file 1's content is on disk, its journal mark has not happened — and recovers
// the operation underneath it. The takeover (fence + 1) must make the released
// publisher stop without writing one more target byte, and the recovery must
// leave the tree exactly as it was before the publish.
func TestRecoverStopsALivePublisher(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"a.txt": "old a", "b.txt": "old b"},
		map[string]string{"a.txt": "new a", "b.txt": "new b"},
		nil)

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releasePublisher := func() { releaseOnce.Do(func() { close(release) }) }
	pubErr := make(chan error, 1)
	pubDone := make(chan struct{})
	var pubRes PublishResult

	in := f.input("op-winner")
	var blockOnce sync.Once
	in.Hook = func(s Step) error {
		if s.Seq == 1 && s.Stage == StageBeforeMark {
			blockOnce.Do(func() {
				close(started)
				<-release
			})
		}
		return nil
	}
	go func() {
		defer close(pubDone)
		res, err := Publish(context.Background(), in)
		pubRes = res
		pubErr <- err
	}()
	t.Cleanup(func() { releasePublisher(); <-pubDone })

	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the publisher never reached the first file's before_mark step")
	}
	if got, _ := targetFile(f.target, "a.txt"); got != "new a" {
		t.Fatalf("a.txt = %q, want the publisher's write on disk before the takeover", got)
	}

	report, err := Recover(context.Background(), RecoverInput{
		Store:        f.store,
		Blobs:        f.blobs,
		TargetRoot:   f.target,
		ExpectedRoot: f.root,
		Recoverer:    "test-recoverer",
		Now:          func() time.Time { return time.UnixMilli(1700003000000).UTC() },
		FS:           f.fs,
		Destinations: []string{"ws:project:p-1"},
	})
	if err != nil {
		t.Fatalf("Recover while a publisher was live: %v (report %+v)", err, report)
	}
	if !report.TakenOver {
		t.Fatal("the recovery did not report a takeover")
	}
	if report.Outcome != RecoverOutcomeRolledBack {
		t.Fatalf("outcome = %s, want rolled_back (file 1 was written, file 2 was not)", report.Outcome)
	}
	if report.Operation.Fence != 2 {
		t.Fatalf("fence = %d, want 2 (the takeover must bump it)", report.Operation.Fence)
	}
	assertTreeEquals(t, "the target after the recovery", f.beforeTree, snapshotTree(t, f.target))
	replacesAtRelease := f.fs.replaces()

	// Release the old publisher: it must stop. Its mark of file 1 hits the
	// terminal trigger (the operation is rolled_back), and its rollback cannot
	// begin either; either way, no target byte may change.
	releasePublisher()
	<-pubDone
	if err := <-pubErr; err == nil {
		t.Fatalf("the superseded publisher reported success: %+v", pubRes)
	}
	if got := f.fs.replaces(); got != replacesAtRelease {
		t.Fatalf("the superseded publisher replaced %d more files after the takeover", got-replacesAtRelease)
	}
	assertTreeEquals(t, "the target after the superseded publisher stopped", f.beforeTree, snapshotTree(t, f.target))
	if err := f.mustCreateClaimedOp(t, "op-next"); err != nil {
		t.Fatalf("the root lock was not released by the recovery: %v", err)
	}
}

// TestRecoverIsIdempotent: a second recovery of a settled root changes nothing
// and does not even take the operation over; a root with no unfinished
// operation is reported as such.
func TestRecoverIsIdempotent(t *testing.T) {
	// The second file is left pending, so the first recovery rolls back rather
	// than completing a target that only half matches.
	f := newRecoverFixture(t, []recoverFileSpec{
		{rel: "mod.txt", kind: KindModify, old: "old mod", neu: "new mod"},
		{rel: "other.txt", kind: KindModify, old: "old other", neu: "new other"},
	})
	f.writeNew("mod.txt")

	first, err := f.recover()
	if err != nil || first.Outcome != RecoverOutcomeRolledBack {
		t.Fatalf("first recovery: outcome %s err %v, want rolled_back", first.Outcome, err)
	}
	replaces := f.fs.replaces()
	events := countEvents(t, f.store, string(run.EventMergeCompleted))
	tree := snapshotTree(t, f.target)

	second, err := f.recover()
	if err != nil {
		t.Fatalf("second recovery: %v", err)
	}
	if second.Outcome != RecoverOutcomeNone {
		t.Fatalf("second outcome = %s, want none (a settled root has nothing to recover)", second.Outcome)
	}
	if second.TakenOver {
		t.Fatal("a settled root was reported as taken over")
	}
	if got := f.fs.replaces(); got != replaces {
		t.Fatalf("the second recovery replaced %d files, want 0", got-replaces)
	}
	if got := countEvents(t, f.store, string(run.EventMergeCompleted)); got != events {
		t.Fatalf("the second recovery changed the event count from %d to %d", events, got)
	}
	assertTreeEquals(t, "the target after the second recovery", tree, snapshotTree(t, f.target))
	f.assertUnlocked()

	// The explicit form of the same call: a terminal operation is reported and
	// left alone — not taken over, not moved, not written to.
	in := f.input()
	in.OperationID = "op-rec"
	third, err := Recover(context.Background(), in)
	if err != nil {
		t.Fatalf("Recover of a terminal operation: %v", err)
	}
	if third.Outcome != RecoverOutcomeUnchanged {
		t.Fatalf("terminal outcome = %s, want unchanged", third.Outcome)
	}
	if third.TakenOver {
		t.Fatal("a terminal operation was taken over")
	}
	if got := f.fs.replaces(); got != replaces {
		t.Fatalf("recovering a terminal operation replaced %d files, want 0", got-replaces)
	}
	if err := f.claimError("op-after-terminal"); !errors.Is(err, ErrRootBusy) {
		// op-after was created by assertUnlocked above; the root must now read
		// as busy because of *that* operation, proving neither terminal read
		// wrote anything.
		t.Fatalf("a claim after the terminal reads = %v, want ErrRootBusy", err)
	}

	// The start-up scan's simplest case: a root with no operation at all.
	st, _ := mergeDB(t)
	clean := t.TempDir()
	root, err := workspace.CaptureRootIdentity(clean)
	if err != nil {
		t.Fatalf("CaptureRootIdentity: %v", err)
	}
	res, err := Recover(context.Background(), RecoverInput{
		Store:        st,
		Blobs:        newCountingBlobs(),
		TargetRoot:   clean,
		ExpectedRoot: root,
		Recoverer:    "test-recoverer",
		Now:          func() time.Time { return time.UnixMilli(1700004000000).UTC() },
	})
	if err != nil {
		t.Fatalf("Recover of a clean root: %v", err)
	}
	if res.Outcome != RecoverOutcomeNone {
		t.Fatalf("outcome = %s, want none", res.Outcome)
	}
	if res.TakenOver {
		t.Fatal("a root with no operation was reported as taken over")
	}
}

// TestRecoverRetryAfterAHumanFix is the explicit human entry point: the first
// pass parks in needs_recovery; a human resolves the obstacle; RetryRecovery
// re-runs the same reconciliation. Both endings are exercised — rolled back
// when the old content is back, applied when the candidate's content is.
func TestRecoverRetryAfterAHumanFix(t *testing.T) {
	build := func(t *testing.T) *recoverFixture {
		t.Helper()
		f := newRecoverFixture(t, []recoverFileSpec{
			{rel: "mod.txt", kind: KindModify, old: "old mod", neu: "new mod"},
		})
		f.writeNew("mod.txt")
		f.mark(1, FileStatePending, FileStateWritten)
		f.writeRaw("mod.txt", "the user's newest version")
		first, err := f.recover()
		if err != nil || first.Outcome != RecoverOutcomeNeedsRecovery {
			t.Fatalf("first recovery: outcome %s err %v, want needs_recovery", first.Outcome, err)
		}
		return f
	}

	t.Run("the human restores the old content", func(t *testing.T) {
		f := build(t)
		f.writeRaw("mod.txt", "old mod")
		res, err := f.retry()
		if err != nil {
			t.Fatalf("RetryRecovery: %v (report %+v)", err, res)
		}
		if res.Outcome != RecoverOutcomeRolledBack {
			t.Fatalf("outcome = %s, want rolled_back", res.Outcome)
		}
		if op := f.op(); op.Status != StatusRolledBack {
			t.Fatalf("operation status = %s, want rolled_back", op.Status)
		}
		f.assertTree("the target after the retry")
		f.assertUnlocked()
	})

	t.Run("the human restores the candidate's content", func(t *testing.T) {
		f := build(t)
		f.writeNew("mod.txt")
		res, err := f.retry()
		if err != nil {
			t.Fatalf("RetryRecovery: %v (report %+v)", err, res)
		}
		if res.Outcome != RecoverOutcomeApplied {
			t.Fatalf("outcome = %s, want applied", res.Outcome)
		}
		if n := countEvents(t, f.store, string(run.EventMergeCompleted)); n != 1 {
			t.Fatalf("merge.completed events = %d, want 1", n)
		}
		if row := f.rows()[0]; row.State != FileStateWritten {
			t.Fatalf("file row state = %s, want written", row.State)
		}
		f.assertUnlocked()
	})

	t.Run("the human did nothing", func(t *testing.T) {
		f := build(t)
		res, err := f.retry()
		if err != nil {
			t.Fatalf("RetryRecovery: %v", err)
		}
		if res.Outcome != RecoverOutcomeNeedsRecovery {
			t.Fatalf("outcome = %s, want needs_recovery again", res.Outcome)
		}
		if got, _ := targetFile(f.target, "mod.txt"); got != "the user's newest version" {
			t.Fatalf("mod.txt = %q, want the user's edit untouched", got)
		}
		f.assertLocked()
	})
}

// TestRecoverPreparedDoesNotReadAHandWrittenFileList is the fail-closed shape
// of the prepared rule. WriteFilesTx cannot produce a prepared operation with
// file rows, but the journal is data, and a prepared row whose file list was
// inserted by hand must still not cause a single byte to be touched: prepared
// means "the journal was committed before any write", so the recovery releases
// the lock and reads nothing on disk.
func TestRecoverPreparedDoesNotReadAHandWrittenFileList(t *testing.T) {
	f := newRecoverFixture(t, []recoverFileSpec{
		{rel: "mod.txt", kind: KindModify, old: "old mod", neu: "new mod"},
	})
	// The fixture committed the list and moved to applying; force the pair
	// back into the state a hand-written journal would present, with one row
	// already written and its content on disk — the rows a recovery that
	// "helpfully" walked would restore.
	f.writeNew("mod.txt")
	f.mark(1, FileStatePending, FileStateWritten)
	if _, err := f.store.DB().Exec(`
		UPDATE merge_operations SET status = 'prepared' WHERE id = 'op-rec'`); err != nil {
		t.Fatalf("force prepared: %v", err)
	}
	before := snapshotTree(t, f.target)
	replacesBefore := f.fs.replaces()

	res, err := f.recover()
	if err != nil {
		t.Fatalf("Recover: %v (report %+v)", err, res)
	}
	if res.Outcome != RecoverOutcomeRolledBack {
		t.Fatalf("outcome = %s, want rolled_back", res.Outcome)
	}
	if op := f.op(); op.Status != StatusRolledBack || op.FailureCode == nil || *op.FailureCode != recoveryPreparedCode {
		t.Fatalf("operation = %+v, want rolled_back with %s", op, recoveryPreparedCode)
	}
	if n := f.fs.replaces(); n != replacesBefore {
		t.Fatalf("a prepared recovery replaced %d files, want 0", n-replacesBefore)
	}
	assertTreeEquals(t, "the target after the prepared recovery", before, snapshotTree(t, f.target))
	f.assertUnlocked()
}

// TestRecoverJournalHelpers pins the two additions to journal.go that the
// recovery is built on: the start-up scan, and the refusal to take over a
// terminal operation.
func TestRecoverJournalHelpers(t *testing.T) {
	st, _ := mergeDB(t)
	ctx := context.Background()

	op := createOperation(t, st, journalOpInput("op-open", "root-open"))
	open, err := ListNonTerminalOperations(ctx, st.DB())
	if err != nil {
		t.Fatalf("ListNonTerminalOperations: %v", err)
	}
	if len(open) != 1 || open[0].ID != "op-open" || open[0].Status != StatusPrepared {
		t.Fatalf("unfinished operations = %+v, want just op-open", open)
	}

	casStatus(t, st, op, StatusPrepared, StatusRolledBack)
	open, err = ListNonTerminalOperations(ctx, st.DB())
	if err != nil {
		t.Fatalf("ListNonTerminalOperations after terminal: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("a terminal operation is still listed: %+v", open)
	}

	done := getOperation(t, st, "op-open")
	err = st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, err := TakeOverOperationTx(ctx, tx, done, "taker", time.UnixMilli(1700005000000).UTC())
		return err
	})
	if !errors.Is(err, ErrOperationTerminal) {
		t.Fatalf("taking over a terminal operation = %v, want ErrOperationTerminal", err)
	}
	if got := getOperation(t, st, "op-open"); got.Revision != done.Revision || got.Fence != done.Fence {
		t.Fatalf("the refused takeover moved the row: %+v", got)
	}
}
