package merge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/dbx"
	"github.com/codeflow/backend/internal/runstore"
)

// The journal tests (T1.09.b group 3). They run against a real file database
// and the real migration set, because everything this file checks — the root
// lock, the fence, the triggers — is a property of the schema, and an
// in-memory fake would be a test of the fake.

const (
	journalTestHashA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	journalTestHashB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	journalTestHashC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// mergeDB opens a migrated file database in the production transaction mode
// (BEGIN IMMEDIATE, which the root lock's read-then-insert depends on) and
// seeds the project/task/run set an operation needs.
func mergeDB(t *testing.T) (*runstore.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codeflow.db")
	db, err := dbx.Open(path, dbx.WithTxLock("immediate"))
	if err != nil {
		t.Fatalf("dbx.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := runstore.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	seedMergeFixture(t, db)
	return runstore.NewStore(db), path
}

// seedMergeFixture inserts the rows a merge operation references. It is the
// same shape the runstore fixtures use: one project, one task, one run.
func seedMergeFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO project_refs (project_id, source_revision, snapshot_hash, state, captured_at, verified_at)
		 VALUES ('p-1', NULL, 'sha256:proj', 'active', 1700000000000, 1700000000001)`,
		`INSERT INTO tasks (id, project_id, title, kind, status, priority,
		                    input_json, input_hash, lease_epoch, revision, created_at, updated_at)
		 VALUES ('t-1', 'p-1', 'merge', 'code', 'ready', 3, '{}', 'sha256:in', 0, 1, 1700000000002, 1700000000003)`,
		`INSERT INTO input_snapshots (id, project_id, content_json, content_hash, created_at)
		 VALUES ('snap-1', 'p-1', '{}', 'sha256:snap', 1700000000006)`,
		`INSERT INTO runs (id, task_id, project_id, binding_id, binding_revision,
		                   base_manifest_hash, agent_revision_id, input_snapshot_id,
		                   input_snapshot_hash, status, revision, created_at, updated_at)
		 VALUES ('r-1', 't-1', 'p-1', 'b-1', 7, 'sha256:manifest', 'ar-1', 'snap-1',
		         'sha256:snap', 'running', 1, 1700000000004, 1700000000005)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
}

func journalOpInput(id, rootKey string) CreateOperationInput {
	return CreateOperationInput{
		ID:                 id,
		ProjectID:          "p-1",
		RunID:              "r-1",
		CandidateHash:      journalTestHashA,
		BaseManifestHash:   journalTestHashA,
		TargetManifestHash: journalTestHashB,
		ResultManifestHash: journalTestHashC,
		RootKey:            rootKey,
		TargetRoot:         `C:\work\repo`,
		Now:                time.UnixMilli(1700000100000).UTC(),
	}
}

func createOperation(t *testing.T, st *runstore.Store, in CreateOperationInput) MergeOperation {
	t.Helper()
	var op MergeOperation
	if err := st.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		op, err = CreateOperationTx(ctx, tx, in)
		return err
	}); err != nil {
		t.Fatalf("CreateOperationTx(%s): %v", in.ID, err)
	}
	return op
}

func getOperation(t *testing.T, st *runstore.Store, id string) MergeOperation {
	t.Helper()
	op, err := GetOperation(context.Background(), st.DB(), id)
	if err != nil {
		t.Fatalf("GetOperation(%s): %v", id, err)
	}
	return op
}

// journalFiles is a three-file list covering all three kinds in publish order.
func journalFiles() []MergeFile {
	oldA, oldB, newA := journalTestHashA, journalTestHashB, journalTestHashB
	backupA, backupB := journalTestHashA, journalTestHashB
	oldMode, newMode := uint32(0o644), uint32(0o755)
	return []MergeFile{
		{Seq: 1, Path: "dir/new.txt", Kind: KindCreate,
			NewHash: &newA, NewMode: &newMode, State: FileStatePending,
			CreatedDirs: []string{"dir"}},
		{Seq: 2, Path: "old.txt", Kind: KindModify,
			OldHash: &oldA, OldMode: &oldMode, NewHash: &newA, NewMode: &newMode,
			BackupHash: &backupA, State: FileStatePending},
		{Seq: 3, Path: "gone.txt", Kind: KindDelete,
			OldHash: &oldB, OldMode: &oldMode, BackupHash: &backupB, State: FileStatePending},
	}
}

func writeFiles(t *testing.T, st *runstore.Store, op MergeOperation, files []MergeFile) MergeOperation {
	t.Helper()
	var out MergeOperation
	if err := st.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		out, err = WriteFilesTx(ctx, tx, op, files, time.UnixMilli(1700000101000).UTC())
		return err
	}); err != nil {
		t.Fatalf("WriteFilesTx: %v", err)
	}
	return out
}

func casStatus(t *testing.T, st *runstore.Store, op MergeOperation, expected, next string) MergeOperation {
	t.Helper()
	var out MergeOperation
	if err := st.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		out, err = UpdateOperationStatusCAS(ctx, tx, op, expected, next, nil, time.UnixMilli(1700000102000).UTC())
		return err
	}); err != nil {
		t.Fatalf("UpdateOperationStatusCAS(%s -> %s): %v", expected, next, err)
	}
	return out
}

func TestJournalCreateOperationTakesTheRootLock(t *testing.T) {
	st, _ := mergeDB(t)
	ctx := context.Background()

	op := createOperation(t, st, journalOpInput("op-1", "root-1"))
	if op.Status != StatusPrepared || op.Fence != 1 || op.Revision != 1 {
		t.Fatalf("new operation = %+v, want prepared/fence 1/revision 1", op)
	}
	if op.TargetRoot != `C:\work\repo` || op.RootKey != "root-1" {
		t.Fatalf("operation records root %q key %q", op.TargetRoot, op.RootKey)
	}

	// The same root is busy, in both directions: a second operation is refused
	// with ErrRootBusy and the occupant's id, and a different root is not.
	err := st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, err := CreateOperationTx(ctx, tx, journalOpInput("op-2", "root-1"))
		return err
	})
	if !errors.Is(err, ErrRootBusy) {
		t.Fatalf("second create on the same root = %v, want ErrRootBusy", err)
	}
	if !strings.Contains(err.Error(), "op-1") {
		t.Fatalf("ErrRootBusy does not name the occupant: %v", err)
	}
	if other := createOperation(t, st, journalOpInput("op-3", "root-2")); other.Fence != 1 {
		t.Fatalf("operation on a different root got fence %d, want 1", other.Fence)
	}

	// A terminal operation releases the lock, and the fence keeps counting.
	casStatus(t, st, op, StatusPrepared, StatusRolledBack)
	next := createOperation(t, st, journalOpInput("op-4", "root-1"))
	if next.Fence != 2 {
		t.Fatalf("fence after a finished operation = %d, want 2", next.Fence)
	}
}

func TestJournalFenceIsMonotoneAcrossOperations(t *testing.T) {
	st, _ := mergeDB(t)
	seen := map[int64]bool{}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("op-%d", i+1)
		op := createOperation(t, st, journalOpInput(id, "root-f"))
		if seen[op.Fence] {
			t.Fatalf("fence %d was reused (operation %s)", op.Fence, id)
		}
		seen[op.Fence] = true
		casStatus(t, st, op, StatusPrepared, StatusRolledBack)
	}
	if len(seen) != 3 {
		t.Fatalf("fences = %v, want three distinct values", seen)
	}
}

func TestJournalCreateOperationRefusesAnotherProjectsRun(t *testing.T) {
	st, _ := mergeDB(t)
	in := journalOpInput("op-x", "root-x")
	in.ProjectID = "p-other"
	err := st.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		_, err := CreateOperationTx(ctx, tx, in)
		return err
	})
	if !errors.Is(err, ErrRunProjectMismatch) {
		t.Fatalf("create with a foreign run = %v, want ErrRunProjectMismatch", err)
	}
	if _, err := GetOperation(context.Background(), st.DB(), "op-x"); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("a refused operation left a row behind: %v", err)
	}
}

func TestJournalWriteFilesMovesToApplying(t *testing.T) {
	st, _ := mergeDB(t)
	ctx := context.Background()

	op := createOperation(t, st, journalOpInput("op-1", "root-1"))
	applying := writeFiles(t, st, op, journalFiles())
	if applying.Status != StatusApplying || applying.Revision != op.Revision+1 {
		t.Fatalf("after WriteFilesTx = %+v, want applying/revision %d", applying, op.Revision+1)
	}

	files, err := ListFiles(ctx, st.DB(), "op-1")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("ListFiles returned %d rows, want 3", len(files))
	}
	if files[0].Path != "dir/new.txt" || files[2].Path != "gone.txt" {
		t.Fatalf("files are not in seq order: %+v", files)
	}
	if got := files[0].CreatedDirs; len(got) != 1 || got[0] != "dir" {
		t.Fatalf("created dirs = %v, want [dir]", got)
	}
	if files[0].OldHash != nil || files[0].BackupHash != nil {
		t.Fatalf("a create carries an old side: %+v", files[0])
	}
	if files[2].NewHash != nil || files[2].NewMode != nil {
		t.Fatalf("a delete carries a new side: %+v", files[2])
	}

	// A write to a terminal operation is refused by the Go-side CAS and names
	// the sentinel, so a caller can act on it rather than on a string.
	casStatus(t, st, applying, StatusApplying, StatusRolledBack)
	err = st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		return MarkFileTx(ctx, tx, "op-1", 1, FileStatePending, FileStateWritten,
			time.UnixMilli(1700000103000).UTC())
	})
	if !errors.Is(err, ErrOperationTerminal) {
		t.Fatalf("marking a file of a terminal operation = %v, want ErrOperationTerminal", err)
	}
}

func TestJournalWriteFilesRejectsTheWrongShape(t *testing.T) {
	st, _ := mergeDB(t)
	op := createOperation(t, st, journalOpInput("op-1", "root-1"))
	ctx := context.Background()

	cases := []struct {
		name  string
		files []MergeFile
		want  error
	}{
		{"a create with an old hash", []MergeFile{{
			Seq: 1, Path: "a.txt", Kind: KindCreate, State: FileStatePending,
			OldHash: strptr(journalTestHashA), NewHash: strptr(journalTestHashA), NewMode: u32ptr(0o644),
		}}, ErrInvalidJournalInput},
		{"a delete with a new hash", []MergeFile{{
			Seq: 1, Path: "a.txt", Kind: KindDelete, State: FileStatePending,
			OldHash: strptr(journalTestHashA), OldMode: u32ptr(0o644),
			NewHash: strptr(journalTestHashA), BackupHash: strptr(journalTestHashA),
		}}, ErrInvalidJournalInput},
		{"a path that escapes the root", []MergeFile{{
			Seq: 1, Path: "../a.txt", Kind: KindCreate, State: FileStatePending,
			NewHash: strptr(journalTestHashA), NewMode: u32ptr(0o644),
		}}, ErrInvalidJournalInput},
		{"a seq that is not the publish order", []MergeFile{{
			Seq: 2, Path: "a.txt", Kind: KindCreate, State: FileStatePending,
			NewHash: strptr(journalTestHashA), NewMode: u32ptr(0o644),
		}}, ErrInvalidJournalInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, err := WriteFilesTx(ctx, tx, op, tc.files, time.UnixMilli(1700000101000).UTC())
				return err
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("WriteFilesTx = %v, want %v", err, tc.want)
			}
			if _, err := ListFiles(ctx, st.DB(), "op-1"); err != nil {
				t.Fatal(err)
			}
			rows, err := ListFiles(ctx, st.DB(), "op-1")
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 0 {
				t.Fatalf("a refused file list left %d rows behind", len(rows))
			}
			got, err := GetOperation(ctx, st.DB(), "op-1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != StatusPrepared {
				t.Fatalf("a refused file list moved the operation to %s", got.Status)
			}
		})
	}
}

func TestJournalStatusCASStopsAStaleHolder(t *testing.T) {
	st, _ := mergeDB(t)
	ctx := context.Background()

	op := createOperation(t, st, journalOpInput("op-1", "root-1"))
	stale := op // the revision before somebody else moved it
	moved := casStatus(t, st, op, StatusPrepared, StatusConflict)

	err := st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, err := UpdateOperationStatusCAS(ctx, tx, stale, StatusPrepared, StatusApplying, nil,
			time.UnixMilli(1700000102000).UTC())
		return err
	})
	if !errors.Is(err, ErrOperationMoved) {
		t.Fatalf("stale CAS = %v, want ErrOperationMoved", err)
	}
	if got := getOperation(t, st, "op-1"); got.Status != moved.Status || got.Revision != moved.Revision {
		t.Fatalf("the stale CAS overwrote the winner: %+v", got)
	}
}

func TestJournalTerminalStatesAreFinal(t *testing.T) {
	for _, terminal := range []string{StatusApplied, StatusConflict, StatusRolledBack} {
		t.Run(terminal, func(t *testing.T) {
			st, _ := mergeDB(t)
			ctx := context.Background()
			op := createOperation(t, st, journalOpInput("op-1", "root-1"))
			done := casStatus(t, st, op, StatusPrepared, terminal)

			// The trigger fires before the CAS can even be considered, so any
			// update of the row is refused, whatever it says.
			_, err := st.DB().ExecContext(ctx,
				`UPDATE merge_operations SET status = 'prepared', revision = revision + 1 WHERE id = 'op-1'`)
			if !errors.Is(mapJournalError(err), ErrOperationTerminal) {
				t.Fatalf("rewriting a %s operation = %v, want the terminal sentinel", terminal, err)
			}
			// The fence cannot decrease either.
			_, err = st.DB().ExecContext(ctx,
				`UPDATE merge_operations SET fence = fence - 1 WHERE id = 'op-1'`)
			if err == nil {
				t.Fatal("lowering the fence of a terminal operation was accepted")
			}
			// And it cannot be deleted.
			_, err = st.DB().ExecContext(ctx, `DELETE FROM merge_operations WHERE id = 'op-1'`)
			if !errors.Is(mapJournalError(err), ErrOperationIsHistory) {
				t.Fatalf("deleting a %s operation = %v, want the history sentinel", terminal, err)
			}
			if got := getOperation(t, st, "op-1"); got.Status != terminal || got.Revision != done.Revision {
				t.Fatalf("operation changed after terminal: %+v", got)
			}
		})
	}
}

func TestJournalIdentityIsFrozen(t *testing.T) {
	// The row is left non-terminal on purpose: the terminal trigger would
	// otherwise fire first and mask the identity trigger this test is about.
	st, _ := mergeDB(t)
	ctx := context.Background()
	op := createOperation(t, st, journalOpInput("op-1", "root-1"))

	columns := map[string]string{
		"id":                   "'op-rewritten'",
		"project_id":           "'p-other'",
		"run_id":               "'r-other'",
		"root_key":             "'root-other'",
		"target_root":          "'/elsewhere'",
		"candidate_hash":       "'" + journalTestHashC + "'",
		"base_manifest_hash":   "'" + journalTestHashB + "'",
		"target_manifest_hash": "'" + journalTestHashA + "'",
		"result_manifest_hash": "'" + journalTestHashB + "'",
		"created_at":           "1",
	}
	for column, value := range columns {
		_, err := st.DB().ExecContext(ctx,
			fmt.Sprintf(`UPDATE merge_operations SET %s = %s WHERE id = 'op-1'`, column, value))
		if !errors.Is(mapJournalError(err), ErrInvalidJournalInput) {
			t.Errorf("rewriting %s = %v, want the frozen-identity refusal", column, err)
		}
	}
	// Assigning the same value is not a change, so the CAS path still works.
	if err := st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, err := UpdateOperationStatusCAS(ctx, tx, op, StatusPrepared, StatusConflict, nil,
			time.UnixMilli(1700000102000).UTC())
		return err
	}); err != nil {
		t.Fatalf("CAS after same-value identity assignments: %v", err)
	}
	if got := getOperation(t, st, "op-1"); got.Status != StatusConflict || got.Revision != op.Revision+1 {
		t.Fatalf("operation after CAS: %+v", got)
	}
}

func TestJournalFileRowsAreClosedAfterTerminal(t *testing.T) {
	st, _ := mergeDB(t)
	ctx := context.Background()
	op := createOperation(t, st, journalOpInput("op-1", "root-1"))
	applying := writeFiles(t, st, op, journalFiles())
	casStatus(t, st, applying, StatusApplying, StatusRolledBack)

	_, err := st.DB().ExecContext(ctx, `
		INSERT INTO merge_files (operation_id, seq, path, kind, new_hash, new_mode, state, created_dirs_json, updated_at)
		VALUES ('op-1', 4, 'late.txt', 'create', ?, 420, 'pending', '[]', 1)`, journalTestHashA)
	if !errors.Is(mapJournalError(err), ErrOperationTerminal) {
		t.Fatalf("insert into a terminal operation = %v, want ErrOperationTerminal", err)
	}
	_, err = st.DB().ExecContext(ctx, `UPDATE merge_files SET state = 'written' WHERE operation_id='op-1' AND seq=1`)
	if !errors.Is(mapJournalError(err), ErrOperationTerminal) {
		t.Fatalf("update of a terminal operation's files = %v, want ErrOperationTerminal", err)
	}
	_, err = st.DB().ExecContext(ctx, `DELETE FROM merge_files WHERE operation_id='op-1' AND seq=1`)
	if !errors.Is(mapJournalError(err), ErrOperationIsHistory) {
		t.Fatalf("delete of a merge file = %v, want ErrOperationIsHistory", err)
	}
}

func TestJournalFileRestoreRequiresAWrite(t *testing.T) {
	st, _ := mergeDB(t)
	ctx := context.Background()
	op := createOperation(t, st, journalOpInput("op-1", "root-1"))
	writeFiles(t, st, op, journalFiles())

	// pending -> restored is refused by the trigger and mapped to
	// ErrOperationMoved (a state the caller's CAS cannot have observed).
	err := st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		return MarkFileTx(ctx, tx, "op-1", 1, FileStatePending, FileStateRestored,
			time.UnixMilli(1700000103000).UTC())
	})
	if !errors.Is(err, ErrOperationMoved) {
		t.Fatalf("pending -> restored = %v, want ErrOperationMoved", err)
	}
	// pending -> written -> restored is the legal path.
	if err := st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		return MarkFileTx(ctx, tx, "op-1", 1, FileStatePending, FileStateWritten,
			time.UnixMilli(1700000103000).UTC())
	}); err != nil {
		t.Fatalf("pending -> written: %v", err)
	}
	if err := st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		return MarkFileTx(ctx, tx, "op-1", 1, FileStateWritten, FileStateRestored,
			time.UnixMilli(1700000104000).UTC())
	}); err != nil {
		t.Fatalf("written -> restored: %v", err)
	}
	// The CAS read is still enforced: the second mark with the old expectation
	// fails rather than restoring twice.
	err = st.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		return MarkFileTx(ctx, tx, "op-1", 1, FileStateWritten, FileStateRestored,
			time.UnixMilli(1700000105000).UTC())
	})
	if !errors.Is(err, ErrOperationMoved) {
		t.Fatalf("double restore = %v, want ErrOperationMoved", err)
	}
}

func TestJournalRootLockIsCrossConnection(t *testing.T) {
	st, path := mergeDB(t)
	ctx := context.Background()
	createOperation(t, st, journalOpInput("op-1", "root-1"))

	// A second handle to the same file, as another server instance would have.
	second, err := dbx.Open(path, dbx.WithTxLock("immediate"))
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer second.Close()
	st2 := runstore.NewStore(second)

	err = st2.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, err := CreateOperationTx(ctx, tx, journalOpInput("op-2", "root-1"))
		return err
	})
	if !errors.Is(err, ErrRootBusy) {
		t.Fatalf("second connection = %v, want ErrRootBusy", err)
	}
}

func TestJournalConcurrentSameRootExactlyOneWins(t *testing.T) {
	st, _ := mergeDB(t)

	const attempts = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ok      []string
		busy    int
		otherEr []error
	)
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			id := fmt.Sprintf("op-%d", i)
			err := st.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
				_, err := CreateOperationTx(ctx, tx, journalOpInput(id, "root-shared"))
				return err
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok = append(ok, id)
			case errors.Is(err, ErrRootBusy):
				busy++
			default:
				otherEr = append(otherEr, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(otherEr) > 0 {
		t.Fatalf("unexpected errors: %v", otherEr)
	}
	if len(ok) != 1 || busy != attempts-1 {
		t.Fatalf("winners=%v busy=%d, want exactly one winner and %d busy", ok, busy, attempts-1)
	}
}

func TestJournalBlockingOperation(t *testing.T) {
	st, _ := mergeDB(t)
	ctx := context.Background()

	blocked, who, err := BlockingOperation(ctx, st.DB(), "root-1")
	if err != nil || blocked || who != "" {
		t.Fatalf("fresh root: blocked=%v who=%q err=%v", blocked, who, err)
	}
	op := createOperation(t, st, journalOpInput("op-1", "root-1"))
	blocked, who, err = BlockingOperation(ctx, st.DB(), "root-1")
	if err != nil || !blocked || who != op.ID {
		t.Fatalf("after create: blocked=%v who=%q err=%v", blocked, who, err)
	}
	casStatus(t, st, op, StatusPrepared, StatusRolledBack)
	blocked, who, err = BlockingOperation(ctx, st.DB(), "root-1")
	if err != nil || blocked || who != "" {
		t.Fatalf("after rolled_back: blocked=%v who=%q err=%v", blocked, who, err)
	}
}

// mapJournalError is a test-only convenience: the mappers are unexported and
// cover both tables, and a test that asserts a sentinel should not have to know
// which mapper an error came from.
func mapJournalError(err error) error {
	if err == nil {
		return nil
	}
	return mapFileWriteError("", mapOperationWriteError("", err))
}

func strptr(s string) *string { return &s }
func u32ptr(v uint32) *uint32 { return &v }
