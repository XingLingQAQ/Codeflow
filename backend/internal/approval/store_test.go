package approval_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/approval"
	"github.com/codeflow/backend/internal/execbackend"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// The tests are external (package approval_test) on purpose: they can only use
// what the package exports, so a test cannot accidentally lean on an internal
// helper and make the public contract weaker than it looks.

// fixture owns what every test in this file needs: a migrated file database
// with two projects, one task, one snapshot and two runs — one per project — so
// "the run belongs to another project" is expressible without inventing rows.
type fixture struct {
	store   *runstore.Store
	ctx     context.Context
	now     time.Time
	projID  string
	otherID string
	runID   string
	attempt string
	other   string // a run of otherID
	otherAt string // an attempt of that run
}

const (
	fxProject   = "p-1"
	fxOther     = "p-2"
	fxRun       = "r-1"
	fxAttempt   = "a-1"
	fxOtherRun  = "r-2"
	fxOtherAtte = "a-2"
)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	store, _, err := runstore.OpenStore(ctx, filepath.Join(t.TempDir(), "codeflow.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	f := &fixture{
		store: store, ctx: ctx,
		now:    time.UnixMilli(1700000000000).UTC(),
		projID: fxProject, otherID: fxOther,
		runID: fxRun, attempt: fxAttempt, other: fxOtherRun, otherAt: fxOtherAtte,
	}

	err = store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		for _, projectID := range []string{f.projID, f.otherID} {
			if err := runstore.UpsertProjectRef(ctx, tx, run.ProjectRef{
				ProjectID:    projectID,
				SnapshotHash: "sha256:" + projectID,
				State:        run.ProjectRefStateActive,
				CapturedAt:   f.now,
				VerifiedAt:   f.now,
			}); err != nil {
				return err
			}
		}
		// One task per project: 001's idx_runs_one_active_per_task allows only
		// one non-terminal run per task, so the other project's run needs a
		// task of its own.
		tasks := []run.Task{
			{ID: "t-1", ProjectID: f.projID, Title: "add tests", Kind: run.TaskKindCode,
				Status: run.TaskStatusReady, InputJSON: `{"prompt":"add tests"}`,
				CreatedAt: f.now, UpdatedAt: f.now},
			{ID: "t-2", ProjectID: f.otherID, Title: "other", Kind: run.TaskKindCode,
				Status: run.TaskStatusReady, InputJSON: `{"prompt":"other"}`,
				CreatedAt: f.now, UpdatedAt: f.now},
		}
		for i := range tasks {
			if err := runstore.InsertTask(ctx, tx, &tasks[i]); err != nil {
				return err
			}
		}
		snapshotHash, err := runstore.InsertInputSnapshot(ctx, tx, f.projID, "snap-1",
			json.RawMessage(`{"prompt":"add tests"}`), f.now)
		if err != nil {
			return err
		}
		snapshotHash2, err := runstore.InsertInputSnapshot(ctx, tx, f.otherID, "snap-2",
			json.RawMessage(`{"prompt":"other"}`), f.now)
		if err != nil {
			return err
		}
		runs := []run.Run{
			{ID: fxRun, TaskID: "t-1", ProjectID: f.projID, BindingID: "b-1", BindingRevision: 1,
				BaseManifestHash: "sha256:manifest", AgentRevisionID: "ar-1",
				InputSnapshotID: "snap-1", InputSnapshotHash: snapshotHash,
				Status: run.RunStatusQueued, CreatedAt: f.now, UpdatedAt: f.now},
			{ID: fxOtherRun, TaskID: "t-2", ProjectID: f.otherID, BindingID: "b-1", BindingRevision: 1,
				BaseManifestHash: "sha256:manifest", AgentRevisionID: "ar-1",
				InputSnapshotID: "snap-2", InputSnapshotHash: snapshotHash2,
				Status: run.RunStatusQueued, CreatedAt: f.now, UpdatedAt: f.now},
		}
		for i := range runs {
			if err := runstore.InsertRun(ctx, tx, &runs[i]); err != nil {
				return err
			}
		}
		attempts := []run.Attempt{
			{ID: fxAttempt, RunID: fxRun, AttemptNo: 1, Backend: "claude_code",
				Status: run.AttemptStatusRunning, CreatedAt: f.now},
			{ID: fxOtherAtte, RunID: fxOtherRun, AttemptNo: 1, Backend: "claude_code",
				Status: run.AttemptStatusRunning, CreatedAt: f.now},
		}
		for i := range attempts {
			if err := runstore.InsertAttempt(ctx, tx, &attempts[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	return f
}

// argumentsHash computes the tool-request fingerprint exactly the way the
// production caller will, so the test binds the same hash T1.07.b froze.
func argumentsHash(t *testing.T, tool, arguments string) string {
	t.Helper()
	hash, err := execbackend.ToolRequestFingerprint(tool, json.RawMessage(arguments))
	if err != nil {
		t.Fatalf("ToolRequestFingerprint(%s, %s): %v", tool, arguments, err)
	}
	return hash
}

// baseInput is the tool fingerprint input every tool case starts from.
func (f *fixture) baseInput(t *testing.T) approval.FingerprintInput {
	t.Helper()
	return approval.FingerprintInput{
		ProjectID:        f.projID,
		SubjectType:      approval.SubjectTool,
		SubjectID:        "tc_1",
		AgentRevisionID:  "ar-1",
		ArgumentsHash:    argumentsHash(t, "run_shell", `{"command":"ls -la"}`),
		Command:          []string{"ls", "-la"},
		Cwd:              "/workspace/p-1",
		TargetPaths:      []string{"src/a.go"},
		BaseManifestHash: "sha256:manifest",
		PolicyVersion:    "b4-2026-08-19",
	}
}

// newApproval is the tool NewApproval every case starts from.
func (f *fixture) newApproval(t *testing.T) approval.NewApproval {
	t.Helper()
	return approval.NewApproval{
		ID:               "ap-1",
		ProjectID:        f.projID,
		RunID:            f.runID,
		AttemptID:        f.attempt,
		SubjectType:      approval.SubjectTool,
		SubjectID:        "tc_1",
		Risk:             approval.RiskMedium,
		Scope:            []byte(`{"paths":["src/a.go"]}`),
		FingerprintInput: f.baseInput(t),
		RequestedAt:      f.now,
		ExpiresAt:        f.now.Add(time.Hour),
		PolicyVersion:    "b4-2026-08-19",
	}
}

// create runs CreateApprovalTx in its own transaction.
func (f *fixture) create(t *testing.T, in approval.NewApproval) (approval.Approval, error) {
	t.Helper()
	var out approval.Approval
	err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var createErr error
		out, createErr = approval.CreateApprovalTx(ctx, tx, in)
		return createErr
	})
	return out, err
}

// mustCreate is create for the cases where success is the precondition.
func (f *fixture) mustCreate(t *testing.T, in approval.NewApproval) approval.Approval {
	t.Helper()
	out, err := f.create(t, in)
	if err != nil {
		t.Fatalf("CreateApprovalTx(%s): %v", in.ID, err)
	}
	return out
}

// approve flips a stored row to approved with SQL. T2.02.b owns the decision
// path; this test needs decided rows, not a second implementation of decisions.
func (f *fixture) approve(t *testing.T, id, decidedBy string, at time.Time) {
	t.Helper()
	err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, execErr := tx.ExecContext(ctx,
			`UPDATE approvals SET status='approved', decided_by=?, decided_at=?, revision=revision+1
			 WHERE id=?`, decidedBy, at.UnixMilli(), id)
		return execErr
	})
	if err != nil {
		t.Fatalf("approve %s: %v", id, err)
	}
}

// consume runs RecordConsumptionTx in its own transaction.
func (f *fixture) consume(t *testing.T, in approval.ConsumptionInput) (approval.Consumption, error) {
	t.Helper()
	var out approval.Consumption
	err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var consumeErr error
		out, consumeErr = approval.RecordConsumptionTx(ctx, tx, in)
		return consumeErr
	})
	return out, err
}

// countRows counts rows in a table; used to prove a refused call wrote nothing.
func (f *fixture) countRows(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.store.DB().QueryRowContext(f.ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Missing fields
// ---------------------------------------------------------------------------

// TestCreateRejectsMissingFields is §28 T2.02.a's "缺字段拒绝": every required
// field of NewApproval and of FingerprintInput, blanked one at a time, must be
// refused with ErrInvalidApproval *and named*, and nothing may be written.
func TestCreateRejectsMissingFields(t *testing.T) {
	cases := []struct {
		name  string
		field string
		mut   func(*approval.NewApproval)
	}{
		{"approval id", "ID", func(in *approval.NewApproval) { in.ID = "" }},
		{"approval project", "ProjectID", func(in *approval.NewApproval) { in.ProjectID = "" }},
		{"subject type", "SubjectType", func(in *approval.NewApproval) { in.SubjectType = "" }},
		{"subject id", "SubjectID", func(in *approval.NewApproval) { in.SubjectID = "" }},
		{"risk", "Risk", func(in *approval.NewApproval) { in.Risk = "" }},
		{"policy version", "PolicyVersion", func(in *approval.NewApproval) { in.PolicyVersion = "" }},
		{"requested at", "RequestedAt", func(in *approval.NewApproval) { in.RequestedAt = time.Time{} }},
		{"expires at", "ExpiresAt", func(in *approval.NewApproval) { in.ExpiresAt = time.Time{} }},
		{"fingerprint project", "FingerprintInput.ProjectID",
			func(in *approval.NewApproval) { in.FingerprintInput.ProjectID = "" }},
		{"fingerprint subject type", "FingerprintInput.SubjectType",
			func(in *approval.NewApproval) { in.FingerprintInput.SubjectType = "" }},
		{"fingerprint subject id", "FingerprintInput.SubjectID",
			func(in *approval.NewApproval) { in.FingerprintInput.SubjectID = "" }},
		{"fingerprint policy version", "FingerprintInput.PolicyVersion",
			func(in *approval.NewApproval) { in.FingerprintInput.PolicyVersion = "" }},
		{"tool agent revision", "FingerprintInput.AgentRevisionID",
			func(in *approval.NewApproval) { in.FingerprintInput.AgentRevisionID = "" }},
		{"tool arguments hash", "FingerprintInput.ArgumentsHash",
			func(in *approval.NewApproval) { in.FingerprintInput.ArgumentsHash = "" }},
		{"tool base manifest", "FingerprintInput.BaseManifestHash",
			func(in *approval.NewApproval) { in.FingerprintInput.BaseManifestHash = "" }},
		{"tool run", "RunID", func(in *approval.NewApproval) { in.RunID = "" }},
		{"tool attempt", "AttemptID", func(in *approval.NewApproval) { in.AttemptID = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			in := f.newApproval(t)
			tc.mut(&in)
			_, err := f.create(t, in)
			if !errors.Is(err, approval.ErrInvalidApproval) {
				t.Fatalf("CreateApprovalTx error = %v, want ErrInvalidApproval", err)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error %q does not name %s", err, tc.field)
			}
			if n := f.countRows(t, "approvals"); n != 0 {
				t.Errorf("approvals rows = %d after a refused create, want 0", n)
			}
		})
	}
}

// TestFingerprintErrorsNameTheField is the same rule one layer down: the
// Fingerprint function itself refuses each missing field by name, so a caller
// that only computes hashes (a policy check, say) learns the same thing.
func TestFingerprintErrorsNameTheField(t *testing.T) {
	base := func() approval.FingerprintInput {
		return approval.FingerprintInput{
			ProjectID:        "p-1",
			SubjectType:      approval.SubjectTool,
			SubjectID:        "tc_1",
			AgentRevisionID:  "ar-1",
			ArgumentsHash:    "sha256:" + strings.Repeat("a", 64),
			BaseManifestHash: "sha256:manifest",
			PolicyVersion:    "b4-2026-08-19",
		}
	}
	cases := []struct {
		name  string
		field string
		mut   func(*approval.FingerprintInput)
	}{
		{"project", "ProjectID", func(in *approval.FingerprintInput) { in.ProjectID = "  " }},
		{"subject type", "SubjectType", func(in *approval.FingerprintInput) { in.SubjectType = "" }},
		{"subject type invalid", "SubjectType", func(in *approval.FingerprintInput) { in.SubjectType = "sandbox" }},
		{"subject id", "SubjectID", func(in *approval.FingerprintInput) { in.SubjectID = "" }},
		{"policy version", "PolicyVersion", func(in *approval.FingerprintInput) { in.PolicyVersion = "" }},
		{"agent revision", "AgentRevisionID", func(in *approval.FingerprintInput) { in.AgentRevisionID = "" }},
		{"arguments hash", "ArgumentsHash", func(in *approval.FingerprintInput) { in.ArgumentsHash = "" }},
		{"arguments hash malformed", "ArgumentsHash",
			func(in *approval.FingerprintInput) { in.ArgumentsHash = "sha256:xyz" }},
		{"base manifest", "BaseManifestHash", func(in *approval.FingerprintInput) { in.BaseManifestHash = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mut(&in)
			hash, err := approval.Fingerprint(in)
			if !errors.Is(err, approval.ErrInvalidApproval) {
				t.Fatalf("Fingerprint error = %v, want ErrInvalidApproval", err)
			}
			if hash != "" {
				t.Errorf("Fingerprint returned %q alongside an error", hash)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error %q does not name %s", err, tc.field)
			}
		})
	}

	t.Run("merge needs a base manifest", func(t *testing.T) {
		in := base()
		in.SubjectType = approval.SubjectMerge
		in.AgentRevisionID = ""
		in.ArgumentsHash = ""
		in.BaseManifestHash = ""
		if _, err := approval.Fingerprint(in); !errors.Is(err, approval.ErrInvalidApproval) ||
			!strings.Contains(err.Error(), "BaseManifestHash") {
			t.Fatalf("merge without base manifest = %v, want ErrInvalidApproval naming BaseManifestHash", err)
		}
	})

	t.Run("gate needs no tool fields", func(t *testing.T) {
		in := base()
		in.SubjectType = approval.SubjectGate
		in.SubjectID = "stage-1"
		in.AgentRevisionID = ""
		in.ArgumentsHash = ""
		in.BaseManifestHash = ""
		if _, err := approval.Fingerprint(in); err != nil {
			t.Fatalf("gate fingerprint: %v", err)
		}
	})
}

// TestGateAndMergeNeedNoAttempt is §28 T2.02.a's "gate/人工合入可无 attempt":
// both subject types are stored without a run, and the fingerprint does not
// demand the fields a tool approval needs.
func TestGateAndMergeNeedNoAttempt(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		id   string
		kind approval.SubjectType
	}{
		{"ap-gate", approval.SubjectGate},
		{"ap-merge", approval.SubjectMerge},
	} {
		in := f.newApproval(t)
		in.ID = tc.id
		in.SubjectType = tc.kind
		in.SubjectID = "subject-" + string(tc.kind)
		in.RunID = ""
		in.AttemptID = ""
		in.FingerprintInput.SubjectType = tc.kind
		in.FingerprintInput.SubjectID = in.SubjectID
		in.FingerprintInput.AgentRevisionID = ""
		in.FingerprintInput.ArgumentsHash = ""
		if tc.kind == approval.SubjectGate {
			in.FingerprintInput.BaseManifestHash = ""
		}
		stored := f.mustCreate(t, in)
		if stored.RunID != "" || stored.AttemptID != "" {
			t.Errorf("%s stored run/attempt %q/%q, want both empty", tc.kind, stored.RunID, stored.AttemptID)
		}
		if stored.Status != approval.StatusPending || stored.Revision != 1 {
			t.Errorf("%s stored status/revision %s/%d, want pending/1", tc.kind, stored.Status, stored.Revision)
		}
	}
}

// ---------------------------------------------------------------------------
// Cross-project refusal
// ---------------------------------------------------------------------------

// TestCreateRejectsCrossProject pins the three cross-project rules: a run of
// another project, an attempt of another run, and a fingerprint input naming
// another project. None of them may write a row.
func TestCreateRejectsCrossProject(t *testing.T) {
	t.Run("run of another project", func(t *testing.T) {
		f := newFixture(t)
		in := f.newApproval(t)
		in.RunID = f.other
		_, err := f.create(t, in)
		if !errors.Is(err, approval.ErrApprovalProjectMismatch) {
			t.Fatalf("error = %v, want ErrApprovalProjectMismatch", err)
		}
		if n := f.countRows(t, "approvals"); n != 0 {
			t.Errorf("approvals rows = %d, want 0", n)
		}
	})

	t.Run("attempt of another run", func(t *testing.T) {
		f := newFixture(t)
		in := f.newApproval(t)
		in.AttemptID = f.otherAt
		_, err := f.create(t, in)
		if !errors.Is(err, approval.ErrRunProjectMismatch) {
			t.Fatalf("error = %v, want ErrRunProjectMismatch", err)
		}
		if n := f.countRows(t, "approvals"); n != 0 {
			t.Errorf("approvals rows = %d, want 0", n)
		}
	})

	t.Run("unknown run", func(t *testing.T) {
		f := newFixture(t)
		in := f.newApproval(t)
		in.RunID = "r-does-not-exist"
		_, err := f.create(t, in)
		if !errors.Is(err, approval.ErrRunProjectMismatch) {
			t.Fatalf("error = %v, want ErrRunProjectMismatch", err)
		}
	})

	t.Run("fingerprint project differs", func(t *testing.T) {
		f := newFixture(t)
		in := f.newApproval(t)
		in.FingerprintInput.ProjectID = f.otherID
		_, err := f.create(t, in)
		if !errors.Is(err, approval.ErrInvalidApproval) {
			t.Fatalf("error = %v, want ErrInvalidApproval", err)
		}
		if !strings.Contains(err.Error(), "FingerprintInput.ProjectID") {
			t.Errorf("error %q does not name FingerprintInput.ProjectID", err)
		}
		if n := f.countRows(t, "approvals"); n != 0 {
			t.Errorf("approvals rows = %d, want 0", n)
		}
	})

	t.Run("gate with a foreign run", func(t *testing.T) {
		f := newFixture(t)
		in := f.newApproval(t)
		in.SubjectType = approval.SubjectGate
		in.SubjectID = "stage-1"
		in.FingerprintInput.SubjectType = approval.SubjectGate
		in.FingerprintInput.SubjectID = in.SubjectID
		in.FingerprintInput.AgentRevisionID = ""
		in.FingerprintInput.ArgumentsHash = ""
		in.FingerprintInput.BaseManifestHash = ""
		in.RunID = f.other // a gate may carry a run, but not someone else's
		in.AttemptID = ""
		_, err := f.create(t, in)
		if !errors.Is(err, approval.ErrApprovalProjectMismatch) {
			t.Fatalf("error = %v, want ErrApprovalProjectMismatch", err)
		}
	})
}

// TestCreateStoresComputedFingerprint proves the store owns the hash: what it
// writes equals Fingerprint(in) and the stored canonical input hashes to it.
func TestCreateStoresComputedFingerprint(t *testing.T) {
	f := newFixture(t)
	in := f.newApproval(t)
	want, err := approval.Fingerprint(in.FingerprintInput)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	stored := f.mustCreate(t, in)
	if stored.Fingerprint != want {
		t.Errorf("stored fingerprint = %s, want %s", stored.Fingerprint, want)
	}
	if !approval.ValidHash(stored.Fingerprint) {
		t.Errorf("stored fingerprint %q is not sha256:<64 lowercase hex>", stored.Fingerprint)
	}
	canonical, err := approval.CanonicalFingerprintJSON(in.FingerprintInput)
	if err != nil {
		t.Fatalf("CanonicalFingerprintJSON: %v", err)
	}
	if stored.FingerprintInputJSON != canonical {
		t.Errorf("stored input = %s, want %s", stored.FingerprintInputJSON, canonical)
	}
	// Re-reading the row must produce the same two values: the audit half of
	// T2.02.b's consumption check reads them back, not recomputes them.
	read, err := approval.GetApproval(f.ctx, f.store.DB(), stored.ID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if read.Fingerprint != want || read.FingerprintInputJSON != canonical {
		t.Errorf("read fingerprint/input = %s/%s, want %s/%s",
			read.Fingerprint, read.FingerprintInputJSON, want, canonical)
	}
	if read.Status != approval.StatusPending || read.Revision != 1 ||
		read.DecidedBy != "" || !read.DecidedAt.IsZero() {
		t.Errorf("new approval = status %s revision %d decided %q/%v, want pending/1/empty",
			read.Status, read.Revision, read.DecidedBy, read.DecidedAt)
	}
	if !read.RequestedAt.Equal(in.RequestedAt) || !read.ExpiresAt.Equal(in.ExpiresAt) {
		t.Errorf("times = %s/%s, want %s/%s", read.RequestedAt, read.ExpiresAt, in.RequestedAt, in.ExpiresAt)
	}
}

// TestCreateCannotBeHandedAFingerprint pins the half of the contract that is
// about the API's *shape*: there is nowhere for a caller to put a fingerprint,
// so the store cannot be tricked into recording a hash for an action other than
// the one it is being asked to authorize.
//
// A test that only compares the stored hash with Fingerprint(input) would pass
// just as well against a store that accepts a caller's fingerprint whenever one
// is supplied — the caller in the test simply never supplies one. These
// assertions are what make "参数变化必须新建审批、指纹由 store 计算" a property of
// the package rather than a habit of its callers.
func TestCreateCannotBeHandedAFingerprint(t *testing.T) {
	t.Run("NewApproval has no field carrying a fingerprint", func(t *testing.T) {
		// A *string* field named after a fingerprint (or a digest/hash of one)
		// would be somewhere for a caller's value to go. FingerprintInput does
		// contain the word in its name and is not one: it is the struct the
		// store hashes. The check therefore looks at the kind, not the name
		// alone, so the legitimate field cannot hide the illegitimate one.
		typ := reflect.TypeOf(approval.NewApproval{})
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Type.Kind() != reflect.String {
				continue
			}
			name := strings.ToLower(field.Name)
			if strings.Contains(name, "fingerprint") || strings.Contains(name, "digest") {
				t.Errorf("NewApproval.%s is a string a caller could put a fingerprint in; "+
					"the store must compute it", field.Name)
			}
		}
	})

	t.Run("CreateApprovalTx takes nothing a fingerprint could hide in", func(t *testing.T) {
		fn := reflect.TypeOf(approval.CreateApprovalTx)
		if fn.NumIn() != 3 {
			t.Fatalf("CreateApprovalTx takes %d parameters, want 3 (ctx, tx, NewApproval)", fn.NumIn())
		}
		if got := fn.In(2); got != reflect.TypeOf(approval.NewApproval{}) {
			t.Errorf("CreateApprovalTx's third parameter is %s, want approval.NewApproval", got)
		}
	})
}

// TestStoredFingerprintHashesTheStoredInput is the behaviour half of the same
// contract, computed independently of the package: re-hash the canonical input
// that was persisted and require it to equal the persisted fingerprint. A row
// whose hash does not describe its own input is an authorization for something
// other than what the row says.
func TestStoredFingerprintHashesTheStoredInput(t *testing.T) {
	f := newFixture(t)
	stored := f.mustCreate(t, f.newApproval(t))
	sum := sha256.Sum256([]byte(stored.FingerprintInputJSON))
	if want := approval.FingerprintPrefix + hex.EncodeToString(sum[:]); stored.Fingerprint != want {
		t.Errorf("stored fingerprint %s does not hash the stored input %s (want %s)",
			stored.Fingerprint, stored.FingerprintInputJSON, want)
	}
}

// TestCreateRejectsExpiryBeforeRequest: a deadline that is not after the
// request is refused in Go (the schema's CHECK is the backstop).
func TestCreateRejectsExpiryBeforeRequest(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name    string
		expires time.Time
	}{
		{"before", f.now.Add(-time.Second)},
		{"equal", f.now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := f.newApproval(t)
			in.ExpiresAt = tc.expires
			_, err := f.create(t, in)
			if !errors.Is(err, approval.ErrInvalidApproval) {
				t.Fatalf("error = %v, want ErrInvalidApproval", err)
			}
			if !strings.Contains(err.Error(), "ExpiresAt") {
				t.Errorf("error %q does not name ExpiresAt", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// TestGetAndListApprovals covers the three readers: GetApproval by id (and
// ErrNotFound), ListApprovalsByRun in (requested_at, id) order, and
// ListPendingApprovalsByProject which must exclude decided rows.
func TestGetAndListApprovals(t *testing.T) {
	f := newFixture(t)

	// Two approvals of the same run requested in the same millisecond, plus one
	// of the other project, plus a gate with no run at all.
	first := f.mustCreate(t, func() approval.NewApproval { in := f.newApproval(t); in.ID = "ap-b"; return in }())
	second := f.mustCreate(t, func() approval.NewApproval { in := f.newApproval(t); in.ID = "ap-a"; return in }())
	gate := f.newApproval(t)
	gate.ID = "ap-gate"
	gate.SubjectType = approval.SubjectGate
	gate.SubjectID = "stage-1"
	gate.RunID = ""
	gate.AttemptID = ""
	gate.FingerprintInput.SubjectType = approval.SubjectGate
	gate.FingerprintInput.SubjectID = "stage-1"
	gate.FingerprintInput.AgentRevisionID = ""
	gate.FingerprintInput.ArgumentsHash = ""
	gate.FingerprintInput.BaseManifestHash = ""
	f.mustCreate(t, gate)

	if _, err := approval.GetApproval(f.ctx, f.store.DB(), "ap-missing"); !errors.Is(err, runstore.ErrNotFound) {
		t.Errorf("GetApproval(missing) = %v, want runstore.ErrNotFound", err)
	}
	if _, err := approval.GetApproval(f.ctx, f.store.DB(), "  "); !errors.Is(err, approval.ErrInvalidApproval) {
		t.Errorf("GetApproval(blank) = %v, want ErrInvalidApproval", err)
	}

	byRun, err := approval.ListApprovalsByRun(f.ctx, f.store.DB(), f.runID)
	if err != nil {
		t.Fatalf("ListApprovalsByRun: %v", err)
	}
	if len(byRun) != 2 {
		t.Fatalf("ListApprovalsByRun = %d rows, want 2", len(byRun))
	}
	// Same requested_at, so the id decides: ap-a before ap-b.
	if byRun[0].ID != second.ID || byRun[1].ID != first.ID {
		t.Errorf("ListApprovalsByRun order = %s,%s, want ap-a,ap-b", byRun[0].ID, byRun[1].ID)
	}

	pending, err := approval.ListPendingApprovalsByProject(f.ctx, f.store.DB(), f.projID)
	if err != nil {
		t.Fatalf("ListPendingApprovalsByProject: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("ListPendingApprovalsByProject = %d rows, want 3", len(pending))
	}
	f.approve(t, second.ID, "user-1", f.now.Add(time.Minute))
	afterDecision, err := approval.ListPendingApprovalsByProject(f.ctx, f.store.DB(), f.projID)
	if err != nil {
		t.Fatalf("ListPendingApprovalsByProject after a decision: %v", err)
	}
	for _, a := range afterDecision {
		if a.ID == second.ID {
			t.Errorf("decided approval %s is still listed as pending", a.ID)
		}
	}

	if _, err := approval.ListApprovalsByRun(f.ctx, f.store.DB(), " "); !errors.Is(err, approval.ErrInvalidApproval) {
		t.Errorf("ListApprovalsByRun(blank) = %v, want ErrInvalidApproval", err)
	}
	if _, err := approval.ListPendingApprovalsByProject(f.ctx, f.store.DB(), " "); !errors.Is(err, approval.ErrInvalidApproval) {
		t.Errorf("ListPendingApprovalsByProject(blank) = %v, want ErrInvalidApproval", err)
	}
}

// ---------------------------------------------------------------------------
// Scope normalization
// ---------------------------------------------------------------------------

// TestScopeIsCanonicalized: two spellings of one scope must be stored as one
// string, and the stored string must be canonical JSON.
func TestScopeIsCanonicalized(t *testing.T) {
	f := newFixture(t)
	left := f.mustCreate(t, func() approval.NewApproval {
		in := f.newApproval(t)
		in.Scope = []byte(`{"b":2,"a":{"z":[1,2],"y":1}}`)
		return in
	}())
	right := f.mustCreate(t, func() approval.NewApproval {
		in := f.newApproval(t)
		in.ID = "ap-2"
		in.Scope = []byte("{\n  \"a\": {\"y\": 1, \"z\": [1, 2]},\n  \"b\": 2\n}")
		return in
	}())
	if left.ScopeJSON != right.ScopeJSON {
		t.Errorf("equivalent scopes stored as %s and %s", left.ScopeJSON, right.ScopeJSON)
	}
	const want = `{"a":{"y":1,"z":[1,2]},"b":2}`
	if left.ScopeJSON != want {
		t.Errorf("stored scope = %s, want %s", left.ScopeJSON, want)
	}
	if !json.Valid([]byte(left.ScopeJSON)) {
		t.Errorf("stored scope %q is not valid JSON", left.ScopeJSON)
	}

	t.Run("no scope is the empty object", func(t *testing.T) {
		g := newFixture(t)
		in := g.newApproval(t)
		in.Scope = nil
		if stored := g.mustCreate(t, in); stored.ScopeJSON != "{}" {
			t.Errorf("stored scope = %s, want {}", stored.ScopeJSON)
		}
	})
}

// TestScopeRejectsBadJSON: malformed JSON, trailing content and duplicate keys
// are all refused with nothing written. Duplicate keys are the one encoding/json
// would silently resolve (last one wins), which is exactly how an unreviewed
// command would get approved.
func TestScopeRejectsBadJSON(t *testing.T) {
	cases := []struct {
		name  string
		scope string
		want  string
	}{
		{"not json", `{"a":}`, "not valid JSON"},
		{"trailing content", `{"a":1}{"b":2}`, "trailing content"},
		{"trailing garbage", `{"a":1} x`, "trailing content"},
		{"duplicate key", `{"cmd":"rm -rf /","cmd":"ls"}`, "repeats the key"},
		{"duplicate key nested", `{"a":{"b":1,"b":2}}`, "repeats the key"},
		{"duplicate key in array", `[{"a":1,"a":2}]`, "repeats the key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			in := f.newApproval(t)
			in.Scope = []byte(tc.scope)
			_, err := f.create(t, in)
			if !errors.Is(err, approval.ErrInvalidApproval) {
				t.Fatalf("error = %v, want ErrInvalidApproval", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
			if n := f.countRows(t, "approvals"); n != 0 {
				t.Errorf("approvals rows = %d after a refused scope, want 0", n)
			}
		})
	}

	t.Run("repeated values are not repeated keys", func(t *testing.T) {
		f := newFixture(t)
		in := f.newApproval(t)
		in.Scope = []byte(`{"a":["x","x"],"b":{"c":"c"}}`)
		if _, err := f.create(t, in); err != nil {
			t.Fatalf("a scope with repeated values must be accepted: %v", err)
		}
	})

	t.Run("numbers survive normalization", func(t *testing.T) {
		f := newFixture(t)
		in := f.newApproval(t)
		// Nothing here goes through a float64: 1.0 stays 1.0, 10e-1 stays a
		// tenth, and the 30-digit integer keeps every digit (json.Number is
		// marshalled as its own literal; probing confirmed this on the pinned
		// toolchain, it is not assumed).
		in.Scope = []byte(`{"big":100000000000000000000000000000,"z":1.0,"y":10e-1,"x":-3,"w":1e6,"v":0.5}`)
		stored := f.mustCreate(t, in)
		for _, want := range []string{
			`"big":100000000000000000000000000000`, `"z":1.0`, `"y":10e-1`,
			`"x":-3`, `"w":1e6`, `"v":0.5`,
		} {
			if !strings.Contains(stored.ScopeJSON, want) {
				t.Errorf("stored scope %s lost %s", stored.ScopeJSON, want)
			}
		}
		// Key order is the only thing canonicalization changes: the same scope
		// written in a different order stores as the same string.
		other := f.newApproval(t)
		other.ID = "ap-2"
		other.Scope = []byte(`{"v":0.5,"x":-3,"w":1e6,"z":1.0,"y":10e-1,"big":100000000000000000000000000000}`)
		if got := f.mustCreate(t, other); got.ScopeJSON != stored.ScopeJSON {
			t.Errorf("reordered scope = %s, want %s", got.ScopeJSON, stored.ScopeJSON)
		}

		// Two spellings of one numeric value are two canonical strings. This is
		// the safe direction (nothing is rounded, so two different scopes never
		// collapse), and it is asserted so a later "improvement" that starts
		// normalizing numbers has to change this test on purpose.
		numeric := f.newApproval(t)
		numeric.ID = "ap-3"
		numeric.Scope = []byte(`{"big":1e29}`)
		if got := f.mustCreate(t, numeric); got.ScopeJSON == stored.ScopeJSON {
			t.Error("expected 1e29 to stay a different string from its long form; it did not")
		}
	})
}

// ---------------------------------------------------------------------------
// Consumption
// ---------------------------------------------------------------------------

// TestConsumeOncePerApprovalAndCall is the one-shot rule: one approval, one
// tool call, one receipt, and every later attempt at the same pair is
// ErrAlreadyConsumed.
func TestConsumeOncePerApprovalAndCall(t *testing.T) {
	f := newFixture(t)
	stored := f.mustCreate(t, f.newApproval(t))
	f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))

	first, err := f.consume(t, approval.ConsumptionInput{
		ApprovalID: stored.ID, ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: f.now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("first consumption: %v", err)
	}
	if first.ApprovalID != stored.ID || first.ToolCallID != "tc_1" || first.AttemptID != f.attempt {
		t.Errorf("receipt = %+v, want the consumed identity", first)
	}
	if first.IsZero() {
		t.Error("a written receipt reported IsZero")
	}

	_, err = f.consume(t, approval.ConsumptionInput{
		ApprovalID: stored.ID, ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: f.now.Add(3 * time.Minute),
	})
	if !errors.Is(err, approval.ErrAlreadyConsumed) {
		t.Fatalf("second consumption = %v, want ErrAlreadyConsumed", err)
	}
	if n := f.countRows(t, "approval_consumptions"); n != 1 {
		t.Errorf("receipts = %d, want exactly 1", n)
	}

	// The (attempt_id, tool_call_id) constraint's case: a second approval of
	// the *same* attempt cannot authorize a second receipt for a call that
	// attempt already made. (Measured: when a statement violates both
	// constraints the driver names only this one, so this is also the message a
	// retry of the same approval+call produces — see migration 009.)
	second := f.mustCreate(t, func() approval.NewApproval {
		in := f.newApproval(t)
		in.ID = "ap-2"
		in.SubjectID = "tc_1"
		in.FingerprintInput.SubjectID = "tc_1"
		return in
	}())
	f.approve(t, second.ID, "user-1", f.now.Add(time.Minute))
	_, err = f.consume(t, approval.ConsumptionInput{
		ApprovalID: second.ID, ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: f.now.Add(2 * time.Minute),
	})
	if !errors.Is(err, approval.ErrAlreadyConsumed) {
		t.Fatalf("a second authorization for a spent call = %v, want ErrAlreadyConsumed", err)
	}
	if n := f.countRows(t, "approval_consumptions"); n != 1 {
		t.Errorf("receipts = %d, want exactly 1", n)
	}

	// A different call of the same attempt, under its own approval, is one
	// more receipt: both rules are per call, not per attempt.
	third := f.mustCreate(t, func() approval.NewApproval {
		in := f.newApproval(t)
		in.ID = "ap-3"
		in.SubjectID = "tc_2"
		in.FingerprintInput.SubjectID = "tc_2"
		return in
	}())
	f.approve(t, third.ID, "user-1", f.now.Add(time.Minute))
	if _, err := f.consume(t, approval.ConsumptionInput{
		ApprovalID: third.ID, ToolCallID: "tc_2", AttemptID: f.attempt, ConsumedAt: f.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("a second, distinct call was refused: %v", err)
	}
	if n := f.countRows(t, "approval_consumptions"); n != 2 {
		t.Errorf("receipts = %d, want 2", n)
	}
}

// TestConsumeRejectsUnapprovedAndNonTool: pending, rejected, expired and
// invalidated approvals authorize nothing, and a gate or merge approval is
// never consumable by a tool call.
func TestConsumeRejectsUnapprovedAndNonTool(t *testing.T) {
	for _, status := range []struct {
		status approval.Status
		setSQL string
	}{
		{approval.StatusPending, ""},
		{approval.StatusRejected, `status='rejected', decided_by='user-1', decided_at=1700000060000`},
		{approval.StatusExpired, `status='expired', decided_at=1700000060000`},
		{approval.StatusInvalidated, `status='invalidated', decided_at=1700000060000`},
	} {
		t.Run(string(status.status), func(t *testing.T) {
			f := newFixture(t)
			stored := f.mustCreate(t, f.newApproval(t))
			if status.setSQL != "" {
				err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
					_, execErr := tx.ExecContext(ctx,
						`UPDATE approvals SET `+status.setSQL+` WHERE id=?`, stored.ID)
					return execErr
				})
				if err != nil {
					t.Fatalf("set status %s: %v", status.status, err)
				}
			}
			_, err := f.consume(t, approval.ConsumptionInput{
				ApprovalID: stored.ID, ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: f.now,
			})
			if !errors.Is(err, approval.ErrApprovalNotConsumable) {
				t.Fatalf("consuming a %s approval = %v, want ErrApprovalNotConsumable", status.status, err)
			}
			if n := f.countRows(t, "approval_consumptions"); n != 0 {
				t.Errorf("receipts = %d, want 0", n)
			}
		})
	}

	t.Run("gate approval", func(t *testing.T) {
		f := newFixture(t)
		in := f.newApproval(t)
		in.SubjectType = approval.SubjectGate
		in.SubjectID = "stage-1"
		in.RunID = ""
		in.AttemptID = ""
		in.FingerprintInput.SubjectType = approval.SubjectGate
		in.FingerprintInput.SubjectID = "stage-1"
		in.FingerprintInput.AgentRevisionID = ""
		in.FingerprintInput.ArgumentsHash = ""
		stored := f.mustCreate(t, in)
		f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))
		_, err := f.consume(t, approval.ConsumptionInput{
			ApprovalID: stored.ID, ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: f.now,
		})
		if !errors.Is(err, approval.ErrApprovalNotConsumable) {
			t.Fatalf("consuming a gate approval = %v, want ErrApprovalNotConsumable", err)
		}
	})

	t.Run("another attempt", func(t *testing.T) {
		f := newFixture(t)
		stored := f.mustCreate(t, f.newApproval(t))
		f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))
		_, err := f.consume(t, approval.ConsumptionInput{
			ApprovalID: stored.ID, ToolCallID: "tc_1", AttemptID: f.otherAt, ConsumedAt: f.now,
		})
		if !errors.Is(err, approval.ErrApprovalNotConsumable) {
			t.Fatalf("consuming from another attempt = %v, want ErrApprovalNotConsumable", err)
		}
	})

	t.Run("missing approval", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.consume(t, approval.ConsumptionInput{
			ApprovalID: "ap-missing", ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: f.now,
		})
		if !errors.Is(err, runstore.ErrNotFound) {
			t.Fatalf("consuming a missing approval = %v, want runstore.ErrNotFound", err)
		}
	})

	for _, tc := range []struct {
		name string
		mut  func(*approval.ConsumptionInput)
	}{
		{"no approval id", func(in *approval.ConsumptionInput) { in.ApprovalID = "" }},
		{"no tool call id", func(in *approval.ConsumptionInput) { in.ToolCallID = " " }},
		{"no attempt", func(in *approval.ConsumptionInput) { in.AttemptID = "" }},
		{"no time", func(in *approval.ConsumptionInput) { in.ConsumedAt = time.Time{} }},
		{"tool call id too long", func(in *approval.ConsumptionInput) {
			in.ToolCallID = strings.Repeat("t", 257)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			in := approval.ConsumptionInput{
				ApprovalID: "ap-1", ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: f.now,
			}
			tc.mut(&in)
			if _, err := f.consume(t, in); !errors.Is(err, approval.ErrInvalidApproval) {
				t.Fatalf("error = %v, want ErrInvalidApproval", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Schema guards
// ---------------------------------------------------------------------------

// TestDecidedRowIsFinal: once a row is decided, no UPDATE may touch it. This is
// §19.1's "已批准历史不改" as a database fact, not a convention.
func TestDecidedRowIsFinal(t *testing.T) {
	f := newFixture(t)
	stored := f.mustCreate(t, f.newApproval(t))
	f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"back to pending", `UPDATE approvals SET status='pending', decided_by=NULL, decided_at=NULL WHERE id='ap-1'`},
		{"to rejected", `UPDATE approvals SET status='rejected' WHERE id='ap-1'`},
		{"move the revision", `UPDATE approvals SET revision=revision+1 WHERE id='ap-1'`},
		{"rewrite the decider", `UPDATE approvals SET decided_by='user-2' WHERE id='ap-1'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, execErr := tx.ExecContext(ctx, tc.sql)
				return execErr
			})
			if err == nil {
				t.Fatalf("%s was accepted; a decided approval must be final", tc.name)
			}
			// The trigger's message is stable, and MapRefusal is how T2.02.b
			// turns it into a sentinel without knowing the text.
			if !errors.Is(approval.MapRefusal(err), approval.ErrApprovalNotPending) {
				t.Errorf("MapRefusal(%v) is not ErrApprovalNotPending", err)
			}
		})
	}

	// The row itself is intact.
	read, err := approval.GetApproval(f.ctx, f.store.DB(), stored.ID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if read.Status != approval.StatusApproved || read.DecidedBy != "user-1" {
		t.Errorf("approval = %s by %q, want approved by user-1", read.Status, read.DecidedBy)
	}
}

// TestPendingBindingIsFrozen: even while pending, what the approval authorizes
// cannot be rewritten — a changed parameter set is a new approval (§27.2.3).
func TestPendingBindingIsFrozen(t *testing.T) {
	f := newFixture(t)
	stored := f.mustCreate(t, f.newApproval(t))

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"fingerprint", `UPDATE approvals SET fingerprint='sha256:` + strings.Repeat("b", 64) + `' WHERE id='ap-1'`},
		{"fingerprint input", `UPDATE approvals SET fingerprint_input_json='{}' WHERE id='ap-1'`},
		{"subject id", `UPDATE approvals SET subject_id='tc-9' WHERE id='ap-1'`},
		{"subject type", `UPDATE approvals SET subject_type='gate' WHERE id='ap-1'`},
		{"project", `UPDATE approvals SET project_id='p-2' WHERE id='ap-1'`},
		{"run", `UPDATE approvals SET run_id='r-2' WHERE id='ap-1'`},
		{"attempt", `UPDATE approvals SET attempt_id='a-2' WHERE id='ap-1'`},
		{"id", `UPDATE approvals SET id='ap-9' WHERE id='ap-1'`},
		// §27.2.3 binds the scope and the policy version too. The policy
		// version is also inside fingerprint_input_json, which is frozen, so an
		// editable column would let the two copies disagree.
		{"scope", `UPDATE approvals SET scope_json='{"paths":["src/b.go"]}' WHERE id='ap-1'`},
		{"policy version", `UPDATE approvals SET policy_version='b4-2026-09-01' WHERE id='ap-1'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, execErr := tx.ExecContext(ctx, tc.sql)
				return execErr
			})
			if err == nil {
				t.Fatalf("rewriting the %s of a pending approval was accepted", tc.name)
			}
			if !errors.Is(approval.MapRefusal(err), approval.ErrApprovalBindingFrozen) {
				t.Errorf("MapRefusal(%v) is not ErrApprovalBindingFrozen", err)
			}
		})
	}

	// What stays editable while pending: the risk classification and the
	// deadline (a deliberate choice of migration 009, pinned here so that
	// freezing them later is a visible decision), then the decision surface —
	// the status, the decider and its timestamp.
	err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, execErr := tx.ExecContext(ctx,
			`UPDATE approvals SET risk='high', expires_at=expires_at+60000 WHERE id=?`, stored.ID)
		return execErr
	})
	if err != nil {
		t.Fatalf("re-classifying the risk or extending the deadline of a pending approval was refused: %v", err)
	}
	err = f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, execErr := tx.ExecContext(ctx,
			`UPDATE approvals SET status='approved', decided_by='user-1', decided_at=?, revision=revision+1
			 WHERE id=?`, f.now.Add(time.Minute).UnixMilli(), stored.ID)
		return execErr
	})
	if err != nil {
		t.Fatalf("a legal pending -> approved transition was refused: %v", err)
	}
}

// TestReceiptIsImmutable: an UPDATE or DELETE against a receipt is refused, so
// the record of spending an authorization cannot be moved or removed.
func TestReceiptIsImmutable(t *testing.T) {
	f := newFixture(t)
	stored := f.mustCreate(t, f.newApproval(t))
	f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))
	if _, err := f.consume(t, approval.ConsumptionInput{
		ApprovalID: stored.ID, ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: f.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("consume: %v", err)
	}

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"update call id", `UPDATE approval_consumptions SET tool_call_id='tc-9'`},
		{"update attempt", `UPDATE approval_consumptions SET attempt_id='a-2'`},
		{"update time", `UPDATE approval_consumptions SET consumed_at=1`},
		{"delete", `DELETE FROM approval_consumptions`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, execErr := tx.ExecContext(ctx, tc.sql)
				return execErr
			})
			if err == nil {
				t.Fatalf("%s was accepted; a receipt is immutable", tc.name)
			}
			if !errors.Is(approval.MapRefusal(err), approval.ErrConsumptionImmutable) {
				t.Errorf("MapRefusal(%v) is not ErrConsumptionImmutable", err)
			}
		})
	}
	if n := f.countRows(t, "approval_consumptions"); n != 1 {
		t.Errorf("receipts = %d, want the original 1", n)
	}
}

// TestSchemaRefusesHandWrittenRows: the CHECKs are the last line of defence, so
// a hand-written INSERT that bypasses the Go rules must still be refused.
func TestSchemaRefusesHandWrittenRows(t *testing.T) {
	const insert = `INSERT INTO approvals (id, project_id, run_id, attempt_id, subject_type,
	    subject_id, risk, scope_json, fingerprint, fingerprint_input_json, status,
	    policy_version, requested_at, expires_at, decided_by, decided_at, revision)
	    VALUES (?, 'p-1', ?, ?, ?, 'tc_1', ?, '{}', ?, '{}', ?, 'v1', ?, ?, ?, ?, 1)`

	hash := "sha256:" + strings.Repeat("a", 64)
	cases := []struct {
		name string
		args []any
	}{
		{"unknown subject type", []any{"ap-1", "r-1", "a-1", "sandbox", "low", hash, "pending", 1, 2, nil, nil}},
		{"unknown risk", []any{"ap-1", "r-1", "a-1", "tool", "extreme", hash, "pending", 1, 2, nil, nil}},
		{"unknown status", []any{"ap-1", "r-1", "a-1", "tool", "low", hash, "granted", 1, 2, nil, nil}},
		{"bad fingerprint", []any{"ap-1", "r-1", "a-1", "tool", "low", "sha256:short", "pending", 1, 2, nil, nil}},
		{"tool without attempt", []any{"ap-1", "r-1", nil, "tool", "low", hash, "pending", 1, 2, nil, nil}},
		{"tool without run", []any{"ap-1", nil, nil, "tool", "low", hash, "pending", 1, 2, nil, nil}},
		{"pending with a decider", []any{"ap-1", "r-1", "a-1", "tool", "low", hash, "pending", 1, 2, "user-1", 3}},
		{"approved without a decider", []any{"ap-1", "r-1", "a-1", "tool", "low", hash, "approved", 1, 2, nil, nil}},
		{"rejected without a decider", []any{"ap-1", "r-1", "a-1", "tool", "low", hash, "rejected", 1, 2, "user-1", nil}},
		{"expired without a decision time", []any{"ap-1", "r-1", "a-1", "tool", "low", hash, "expired", 1, 2, nil, nil}},
		{"expiry before the request", []any{"ap-1", "r-1", "a-1", "tool", "low", hash, "pending", 5, 2, nil, nil}},
		{"expiry equal to the request", []any{"ap-1", "r-1", "a-1", "tool", "low", hash, "pending", 2, 2, nil, nil}},
		{"zero expiry", []any{"ap-1", "r-1", "a-1", "tool", "low", hash, "pending", 1, 0, nil, nil}},
		{"unknown run", []any{"ap-1", "r-nope", "a-1", "tool", "low", hash, "pending", 1, 2, nil, nil}},
		{"unknown attempt", []any{"ap-1", "r-1", "a-nope", "tool", "low", hash, "pending", 1, 2, nil, nil}},
		{"unknown project", []any{"ap-1", "r-1", "a-1", "tool", "low", hash, "pending", 1, 2, nil, nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.name == "unknown project" {
				tc.args[0] = "ap-1"
				err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
					_, execErr := tx.ExecContext(ctx,
						`INSERT INTO approvals (id, project_id, run_id, attempt_id, subject_type, subject_id,
						    risk, scope_json, fingerprint, fingerprint_input_json, status, policy_version,
						    requested_at, expires_at, revision)
						 VALUES ('ap-1', 'p-nope', 'r-1', 'a-1', 'tool', 'tc_1', 'low', '{}', ?, '{}',
						         'pending', 'v1', 1, 2, 1)`, hash)
					return execErr
				})
				if err == nil {
					t.Fatal("an approval naming a project this database has never seen was accepted")
				}
				return
			}
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, execErr := tx.ExecContext(ctx, insert, tc.args...)
				return execErr
			})
			if err == nil {
				t.Fatalf("a hand-written row with %s was accepted", tc.name)
			}
			if n := f.countRows(t, "approvals"); n != 0 {
				t.Errorf("approvals rows = %d, want 0", n)
			}
		})
	}

	t.Run("a legal hand-written row is accepted", func(t *testing.T) {
		// The control: the same statement with nothing wrong must succeed, so
		// the cases above fail for their reason and not because the statement
		// or the fixture is broken.
		f := newFixture(t)
		err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
			_, execErr := tx.ExecContext(ctx, insert,
				"ap-1", "r-1", "a-1", "tool", "low", hash, "pending", 1, 2, nil, nil)
			return execErr
		})
		if err != nil {
			t.Fatalf("the control row was refused: %v", err)
		}
	})
}

// TestSchemaRefusesBadReceipts covers the receipt's own constraints.
func TestSchemaRefusesBadReceipts(t *testing.T) {
	f := newFixture(t)
	stored := f.mustCreate(t, f.newApproval(t))
	f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))

	const insert = `INSERT INTO approval_consumptions (approval_id, tool_call_id, attempt_id, consumed_at)
	                VALUES (?, ?, ?, 1)`

	for _, tc := range []struct {
		name string
		args []any
	}{
		{"unknown approval", []any{"ap-nope", "tc_1", fxAttempt}},
		{"unknown attempt", []any{"ap-1", "tc_1", "a-nope"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, execErr := tx.ExecContext(ctx, insert, tc.args...)
				return execErr
			})
			if err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
		})
	}

	t.Run("duplicate pair in one statement batch", func(t *testing.T) {
		err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
			if _, execErr := tx.ExecContext(ctx, insert, "ap-1", "tc_1", fxAttempt); execErr != nil {
				return execErr
			}
			_, execErr := tx.ExecContext(ctx, insert, "ap-1", "tc_1", fxAttempt)
			return execErr
		})
		if err == nil {
			t.Fatal("the same (approval_id, tool_call_id) was accepted twice")
		}
		if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			t.Errorf("error = %v, want a UNIQUE constraint failure", err)
		}
	})
}

// TestMapRefusalLeavesOtherErrorsAlone: the classifier recognizes the three
// trigger messages and touches nothing else, so a caller can apply it to any
// error without losing information.
func TestMapRefusalLeavesOtherErrorsAlone(t *testing.T) {
	if got := approval.MapRefusal(nil); got != nil {
		t.Errorf("MapRefusal(nil) = %v, want nil", got)
	}
	plain := errors.New("approval: something else entirely")
	if got := approval.MapRefusal(plain); got != plain {
		t.Errorf("MapRefusal(plain) = %v, want the same error back", got)
	}
	if errors.Is(approval.MapRefusal(plain), approval.ErrApprovalNotPending) ||
		errors.Is(approval.MapRefusal(plain), approval.ErrApprovalBindingFrozen) ||
		errors.Is(approval.MapRefusal(plain), approval.ErrConsumptionImmutable) ||
		errors.Is(approval.MapRefusal(plain), approval.ErrApprovalIsHistory) ||
		errors.Is(approval.MapRefusal(plain), approval.ErrApprovalNotConsumable) {
		t.Error("a plain error was classified as a trigger refusal")
	}
}

// TestConcurrentConsumersLeaveOneReceipt is §27.2.5's race: two consumers, one
// authorization, exactly one receipt. The database decides; the loser is told
// so and does not proceed.
func TestConcurrentConsumersLeaveOneReceipt(t *testing.T) {
	f := newFixture(t)
	stored := f.mustCreate(t, f.newApproval(t))
	f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))

	const consumers = 8
	type result struct {
		consumed approval.Consumption
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, consumers)
	for i := 0; i < consumers; i++ {
		go func(i int) {
			<-start
			consumed, err := f.consume(t, approval.ConsumptionInput{
				ApprovalID: stored.ID,
				// The approval's own subject: a tool approval authorizes
				// exactly the call it was requested for.
				ToolCallID: "tc_1",
				AttemptID:  f.attempt,
				ConsumedAt: f.now.Add(time.Duration(i) * time.Second),
			})
			results <- result{consumed: consumed, err: err}
		}(i)
	}
	close(start)

	granted, refused := 0, 0
	for i := 0; i < consumers; i++ {
		r := <-results
		switch {
		case r.err == nil:
			granted++
			if r.consumed.ToolCallID != "tc_1" {
				t.Errorf("winner consumed %q, want tc_1", r.consumed.ToolCallID)
			}
		case errors.Is(r.err, approval.ErrAlreadyConsumed):
			refused++
		default:
			t.Errorf("loser error = %v, want ErrAlreadyConsumed", r.err)
		}
	}
	if granted != 1 || refused != consumers-1 {
		t.Errorf("granted/refused = %d/%d, want 1/%d", granted, refused, consumers-1)
	}
	if n := f.countRows(t, "approval_consumptions"); n != 1 {
		t.Errorf("receipts = %d, want exactly 1", n)
	}
}
