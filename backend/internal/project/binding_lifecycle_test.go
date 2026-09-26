package project

// Binding lifecycle acceptance tests (T1.10.c).
//
// T1.10.a built the snapshot/identity model, T1.10.b made the validator and the
// binding table real. What was still missing is the *end* of a binding's life:
// archiving a project left its bindings active, and the validator could not see
// a binding's state at all, because a retired row still carries the snapshot it
// was retired at - exactly the snapshot an old Run pinned. So an archived
// project's old Run passed the merge check.
//
// The four tests in this file are the ones the plan names for this step
// (§28 T1.10.c, §15 T1.10). They run against real temporary directories and a
// real file-backed project database; nothing here is a mock of the filesystem,
// and no case is skipped: junctions are created with mklink /J on Windows and
// with a directory symlink elsewhere.
//
//   - TestRootReplacedAfterPrepare: the workspace root is prepared into a
//     working copy, then replaced at its own path. The stored binding still
//     names the old directory, so the check must report root_replaced rather
//     than let the Run write into the replacement.
//   - TestJunctionEscapeRemainsDenied: a junction inside an allowed root that
//     points outside it must not become a binding, through either writer.
//     (The workspace package has its own test with this name for the Resolve /
//     read path; this one covers the binding layer.)
//   - TestDerivedBindingsCoexist: one primary, a flow and runs under it; a run
//     validates on its own derived binding, retiring one derived binding leaves
//     the others alone, and a primary rebind is seen only by Runs pinned to the
//     primary.
//   - TestArchivedProjectCannotMerge: archiving retires every binding of the
//     project in one commit, every purpose is refused with binding_retired, the
//     revision history stays readable, and a restore hands out a *new* primary
//     binding so nothing pinned before the archive is revived.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/runworkspace"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// bindingLifecycleCount runs one COUNT(*) query against the project database.
// Lifecycle assertions about "no binding row exists" must not go through the
// store's own reads, or a broken store could agree with itself.
func bindingLifecycleCount(t *testing.T, svc *SQLiteProjectService, query string, args ...any) int {
	t.Helper()
	var n int
	if err := svc.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return n
}

// writeLifecycleFile writes one file, creating its parent directories.
func writeLifecycleFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readLifecycleFile reads one file and fails the test when it is missing or
// changed, which is how "the copy was not touched" is phrased.
func readLifecycleFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}

// sameBindingSnapshot reports whether two snapshots describe the same binding
// state. It compares the fields that carry meaning instead of using reflect so a
// failure names what changed.
func sameBindingSnapshot(a, b WorkspaceBindingSnapshot) bool {
	return a.BindingID == b.BindingID &&
		a.ProjectID == b.ProjectID &&
		a.Kind == b.Kind &&
		a.ParentBindingID == b.ParentBindingID &&
		a.Revision == b.Revision &&
		a.CanonicalRoot == b.CanonicalRoot &&
		a.BaseRef == b.BaseRef &&
		a.LinkPolicy == b.LinkPolicy &&
		a.RootIdentity.Equal(b.RootIdentity)
}

// ---------------------------------------------------------------------------
// TestRootReplacedAfterPrepare
// ---------------------------------------------------------------------------

// TestRootReplacedAfterPrepare covers the prepare-then-replace sequence: a Run
// prepares (materializes) a working copy from the project's bound root, and the
// root is afterwards replaced at its own path - the directory was renamed away
// and a new one took its name.
//
// The stored binding was never rebound, so the row still holds the *old*
// directory's identity and still claims to be active. The path exists, resolves
// to itself, and is still inside the allow-list, so only the identity check can
// tell that the directory behind the path is a different one. Both merge and
// dispatch must be refused with root_replaced, and the already materialized
// working copy must be exactly as it was prepared: the refusal is about the
// root, and nothing may have been written, moved or cleaned up on the way.
func TestRootReplacedAfterPrepare(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("prepare-then-replace", env.rootA)
	pinned := env.mustPrimary(project.ID).Snapshot

	// The content the Run will see, and the baseline it prepares from it.
	writeLifecycleFile(t, filepath.Join(env.rootA, "src.txt"), "PREPARED-BASELINE")
	manifest, err := runworkspace.Capture(ctx, nil, env.rootA, runworkspace.CaptureOptions{})
	if err != nil {
		t.Fatalf("runworkspace.Capture(%s): %v", env.rootA, err)
	}
	if manifest.Mode != runworkspace.ModePlain {
		t.Fatalf("manifest mode = %q, want %q", manifest.Mode, runworkspace.ModePlain)
	}
	// A separate temp dir: a destination inside the source root (or containing
	// it) is refused by Materialize, and a sibling of the root is still inside
	// its parent.
	prepared, err := runworkspace.Materialize(ctx, manifest, t.TempDir(), runworkspace.Ownership{
		RunID:         "run-prepare",
		AttemptID:     "attempt-1",
		OwnerInstance: "project-test",
	})
	if err != nil {
		t.Fatalf("runworkspace.Materialize: %v", err)
	}
	preparedFile := filepath.Join(prepared.Path, "src.txt")
	readLifecycleFile(t, preparedFile, "PREPARED-BASELINE")

	// Replace the root at its own path: the original directory keeps its
	// identity but moves away, and a brand-new directory takes its name.
	moved := env.rootA + "-moved-away"
	if err := os.Rename(env.rootA, moved); err != nil {
		t.Fatalf("rename %s -> %s: %v", env.rootA, moved, err)
	}
	mkdirAll(t, env.rootA)

	// The store was not rebound, so the record in force is the pinned one: same
	// binding, same revision, same path, same identity, still active.
	current := env.mustPrimary(project.ID)
	if !sameBindingSnapshot(current.Snapshot, pinned) {
		t.Fatalf("binding in force = %+v, want the pinned snapshot %+v", current.Snapshot, pinned)
	}
	if current.State != BindingStateActive {
		t.Fatalf("binding state = %q, want %q", current.State, BindingStateActive)
	}
	// The stored root is the *canonical* spelling of the path, which on Windows
	// is the OS-normalized long name; it names the directory that was replaced,
	// not the replacement.
	canonicalRoot, err := CanonicalizeWorkspaceRoot(env.rootA, env.allowed)
	if err != nil {
		t.Fatalf("CanonicalizeWorkspaceRoot(%s): %v", env.rootA, err)
	}
	if current.Snapshot.CanonicalRoot != canonicalRoot {
		t.Fatalf("binding root = %q, want the replaced path %q", current.Snapshot.CanonicalRoot, canonicalRoot)
	}

	for _, purpose := range []BindingPurpose{BindingPurposeMerge, BindingPurposeDispatch} {
		err := ValidateBindingRecord(pinned, current, purpose, project.ID, env.allowed)
		if code := validationCode(t, err); code != DriftRootReplaced {
			t.Fatalf("purpose %s: code = %q, want %q (the path is unchanged, so only the identity can tell)",
				purpose, code, DriftRootReplaced)
		}
	}

	// The working copy prepared before the replacement is untouched...
	readLifecycleFile(t, preparedFile, "PREPARED-BASELINE")
	if len(prepared.Files) != 1 || prepared.Files[0] != "src.txt" {
		t.Fatalf("prepared files = %v, want [src.txt]", prepared.Files)
	}
	// ...the moved-away directory still holds the original content...
	readLifecycleFile(t, filepath.Join(moved, "src.txt"), "PREPARED-BASELINE")
	// ...and the directory that took the path is still empty: nothing was
	// copied into it, which is what "the Run was not allowed to follow the
	// replacement" means on disk.
	entries, err := os.ReadDir(env.rootA)
	if err != nil {
		t.Fatalf("read %s: %v", env.rootA, err)
	}
	if len(entries) != 0 {
		t.Fatalf("the replacement directory is not empty: %v", entries)
	}
}

// ---------------------------------------------------------------------------
// TestJunctionEscapeRemainsDenied
// ---------------------------------------------------------------------------

// TestJunctionEscapeRemainsDenied is the binding-layer half of the I-48
// regression test; internal/workspace/final_path_test.go has the Resolve/read
// half under the same name.
//
// A junction inside an allowed root that points outside the allow-list must not
// be usable as a workspace root: the OS would route every read and write to the
// outside directory while the path still looks allow-listed. The binding layer
// must refuse it at *both* writers - CreateProject with that root, and
// BindWorkspaceRoot on an existing project - and must not leave a binding row
// behind. The outside directory must be untouched.
func TestJunctionEscapeRemainsDenied(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	writeLifecycleFile(t, outsideFile, "SECRET")

	link := filepath.Join(env.rootA, "j")
	makeJunction(t, link, outside)
	// The fixture must actually be an escape: the link exists and its target is
	// outside the configured allow-list.
	if _, err := os.Stat(link); err != nil {
		t.Fatalf("junction %s -> %s is not reachable: %v", link, outside, err)
	}

	// 1. Creating a project on the escaping junction is refused before any row
	//    is written.
	_, err := env.svc.CreateProject(ctx, &ProjectCreateRequest{Title: "junction", WorkspaceRoot: link})
	if err == nil {
		t.Fatalf("CreateProject on the escaping junction %s must be refused", link)
	}
	if !strings.Contains(err.Error(), "outside allowed roots") {
		t.Fatalf("CreateProject error = %v, want the outside-allowed-roots rejection", err)
	}
	if n := bindingLifecycleCount(t, env.svc, `SELECT COUNT(*) FROM workspace_bindings`); n != 0 {
		t.Fatalf("bindings after a refused create = %d, want 0", n)
	}
	if n := bindingLifecycleCount(t, env.svc, `SELECT COUNT(*) FROM projects`); n != 0 {
		t.Fatalf("projects after a refused create = %d, want 0", n)
	}

	// 2. Binding the junction on an existing project is refused too, and leaves
	//    the project unbound with no binding row.
	project, err := env.svc.CreateProject(ctx, &ProjectCreateRequest{Title: "unbound"})
	if err != nil {
		t.Fatalf("CreateProject without a root failed: %v", err)
	}
	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, link); err == nil {
		t.Fatalf("BindWorkspaceRoot(%s) must be refused", link)
	} else if !strings.Contains(err.Error(), "outside allowed roots") {
		t.Fatalf("BindWorkspaceRoot error = %v, want the outside-allowed-roots rejection", err)
	}
	unbound, err := env.svc.GetProject(ctx, project.ID)
	if err != nil || unbound == nil {
		t.Fatalf("GetProject(%s) = %+v, %v", project.ID, unbound, err)
	}
	if unbound.WorkspaceRoot != "" || unbound.BindingState != BindingStateUnbound {
		t.Fatalf("project after a refused bind = root %q state %q, want empty/unbound",
			unbound.WorkspaceRoot, unbound.BindingState)
	}
	if n := bindingLifecycleCount(t, env.svc, `SELECT COUNT(*) FROM workspace_bindings`); n != 0 {
		t.Fatalf("bindings after a refused bind = %d, want 0", n)
	}

	// 3. A project already bound to a legitimate root keeps that binding
	//    unchanged when the junction is offered as a rebind.
	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, env.rootA); err != nil {
		t.Fatalf("binding the legitimate root %s failed: %v", env.rootA, err)
	}
	legitimate := env.mustPrimary(project.ID)
	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, link); err == nil {
		t.Fatalf("rebinding to the escaping junction %s must be refused", link)
	}
	after := env.mustPrimary(project.ID)
	if !sameBindingSnapshot(after.Snapshot, legitimate.Snapshot) || after.State != BindingStateActive {
		t.Fatalf("binding changed after a refused rebind: %+v -> %+v", legitimate.Snapshot, after.Snapshot)
	}
	if n := bindingLifecycleCount(t, env.svc, `SELECT COUNT(*) FROM workspace_bindings`); n != 1 {
		t.Fatalf("bindings after a refused rebind = %d, want 1", n)
	}

	// The escape target was never read, moved or deleted.
	readLifecycleFile(t, outsideFile, "SECRET")
	if _, err := os.Stat(link); err != nil {
		t.Fatalf("the junction itself must still exist: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestDerivedBindingsCoexist
// ---------------------------------------------------------------------------

// TestDerivedBindingsCoexist covers the binding relationships a run depends on:
// one primary binding, a flow and runs derived from it, all active at once.
//
// A Run pinned to a derived run binding validates on that binding alone; a
// rebind of the primary must not be visible to it. Retiring one derived binding
// must not disturb the primary or the other derived bindings. And a rebind of
// the primary is seen by exactly the Runs pinned to the primary: their snapshot
// is no longer the binding in force, so they get binding_rebound instead of
// silently following the project into the new directory.
func TestDerivedBindingsCoexist(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("derived-coexist", env.rootA)
	primary := env.mustPrimary(project.ID)

	base := filepath.Dir(env.rootA)
	flow, err := env.svc.CreateDerivedBinding(ctx, primary.Snapshot.BindingID, BindingKindFlow,
		mkdirAll(t, filepath.Join(base, "flow-root")), "refs/heads/flow")
	if err != nil {
		t.Fatalf("CreateDerivedBinding(flow) failed: %v", err)
	}
	runA, err := env.svc.CreateDerivedBinding(ctx, primary.Snapshot.BindingID, BindingKindRun,
		mkdirAll(t, filepath.Join(base, "run-a")), "")
	if err != nil {
		t.Fatalf("CreateDerivedBinding(run A) failed: %v", err)
	}
	runB, err := env.svc.CreateDerivedBinding(ctx, primary.Snapshot.BindingID, BindingKindRun,
		mkdirAll(t, filepath.Join(base, "run-b")), "")
	if err != nil {
		t.Fatalf("CreateDerivedBinding(run B) failed: %v", err)
	}

	// 1. One flow and two runs coexist with the primary, all active, and the
	//    primary is still the only active primary.
	if n := bindingLifecycleCount(t, env.svc,
		`SELECT COUNT(*) FROM workspace_bindings WHERE project_id = ? AND state = 'active'`,
		project.ID); n != 4 {
		t.Fatalf("active bindings = %d, want 4 (primary + flow + 2 runs)", n)
	}
	for _, derived := range []WorkspaceBindingRecord{flow, runA, runB} {
		if derived.Snapshot.ProjectID != project.ID {
			t.Fatalf("derived binding %s belongs to %q, want %q",
				derived.Snapshot.BindingID, derived.Snapshot.ProjectID, project.ID)
		}
		if derived.Snapshot.ParentBindingID != primary.Snapshot.BindingID {
			t.Fatalf("derived binding %s parent = %q, want the primary %s",
				derived.Snapshot.BindingID, derived.Snapshot.ParentBindingID, primary.Snapshot.BindingID)
		}
		if derived.Snapshot.Revision != 1 || !derived.Active() {
			t.Fatalf("derived binding %+v, want revision 1 and active", derived)
		}
	}
	if n := bindingLifecycleCount(t, env.svc,
		`SELECT COUNT(*) FROM workspace_bindings WHERE project_id = ? AND kind = 'primary' AND state = 'active'`,
		project.ID); n != 1 {
		t.Fatalf("active primaries = %d, want 1", n)
	}

	// 2. A Run pinned to a derived run binding validates on that binding.
	if err := ValidateBindingRecord(runA.Snapshot, runA, BindingPurposeCreateRun, project.ID, env.allowed); err != nil {
		t.Fatalf("a run pinned to its own derived binding must validate: %v", err)
	}
	if err := ValidateBindingRecord(runA.Snapshot, runA, BindingPurposeDispatch, project.ID, env.allowed); err != nil {
		t.Fatalf("dispatch on a derived run binding must validate: %v", err)
	}
	// The delegation is not weakened: a run binding is still not a merge target.
	if code := validationCode(t, ValidateBindingRecord(runA.Snapshot, runA, BindingPurposeMerge, project.ID, env.allowed)); code != BindingCodeKindNotMergeable {
		t.Fatalf("merge on a run binding = %q, want %q", code, BindingCodeKindNotMergeable)
	}
	// The primary is the merge target.
	if err := ValidateBindingRecord(primary.Snapshot, primary, BindingPurposeMerge, project.ID, env.allowed); err != nil {
		t.Fatalf("merge on the primary must validate: %v", err)
	}

	// 3. Retiring one derived binding leaves the primary and the other derived
	//    bindings alone.
	if err := env.svc.RetireBinding(ctx, runA.Snapshot.BindingID, 1); err != nil {
		t.Fatalf("RetireBinding(run A) failed: %v", err)
	}
	retiredA, err := env.svc.GetBinding(ctx, runA.Snapshot.BindingID)
	if err != nil {
		t.Fatalf("GetBinding(run A) failed: %v", err)
	}
	if retiredA.State != BindingStateRetired || retiredA.RetiredAt == nil {
		t.Fatalf("run A after retiring = %+v, want retired with a timestamp", retiredA)
	}
	if code := validationCode(t, ValidateBindingRecord(runA.Snapshot, retiredA, BindingPurposeDispatch, project.ID, env.allowed)); code != BindingCodeRetired {
		t.Fatalf("a run pinned to a retired derived binding = %q, want %q", code, BindingCodeRetired)
	}
	if err := ValidateBindingRecord(primary.Snapshot, env.mustPrimary(project.ID), BindingPurposeMerge, project.ID, env.allowed); err != nil {
		t.Fatalf("retiring a derived binding must not disturb the primary: %v", err)
	}
	for _, survivor := range []WorkspaceBindingRecord{flow, runB} {
		record, err := env.svc.GetBinding(ctx, survivor.Snapshot.BindingID)
		if err != nil {
			t.Fatalf("GetBinding(%s) failed: %v", survivor.Snapshot.BindingID, err)
		}
		if !record.Active() || !sameBindingSnapshot(record.Snapshot, survivor.Snapshot) {
			t.Fatalf("derived binding %s changed: %+v -> %+v", survivor.Snapshot.BindingID, survivor.Snapshot, record)
		}
		if err := ValidateBindingRecord(survivor.Snapshot, record, BindingPurposeDispatch, project.ID, env.allowed); err != nil {
			t.Fatalf("surviving derived binding %s must still validate: %v", survivor.Snapshot.BindingID, err)
		}
	}

	// 4. Rebinding the primary to another directory moves only the primary, and
	//    only Runs pinned to the primary are refused.
	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, env.rootB); err != nil {
		t.Fatalf("BindWorkspaceRoot(%s) failed: %v", env.rootB, err)
	}
	rebound := env.mustPrimary(project.ID)
	if rebound.Snapshot.BindingID != primary.Snapshot.BindingID {
		t.Fatalf("rebinding must advance the same binding, got a new id %s", rebound.Snapshot.BindingID)
	}
	if rebound.Snapshot.Revision != 2 {
		t.Fatalf("primary revision after a rebind = %d, want 2", rebound.Snapshot.Revision)
	}
	if code := validationCode(t, ValidateBindingRecord(primary.Snapshot, rebound, BindingPurposeMerge, project.ID, env.allowed)); code != BindingCodeRebound {
		t.Fatalf("a run pinned to primary revision 1 = %q, want %q", code, BindingCodeRebound)
	}
	for _, derived := range []WorkspaceBindingRecord{flow, runB} {
		record, err := env.svc.GetBinding(ctx, derived.Snapshot.BindingID)
		if err != nil {
			t.Fatalf("GetBinding(%s) failed: %v", derived.Snapshot.BindingID, err)
		}
		if !record.Active() || !sameBindingSnapshot(record.Snapshot, derived.Snapshot) {
			t.Fatalf("a primary rebind changed the derived binding %s: %+v -> %+v",
				derived.Snapshot.BindingID, derived.Snapshot, record)
		}
		if err := ValidateBindingRecord(derived.Snapshot, record, BindingPurposeDispatch, project.ID, env.allowed); err != nil {
			t.Fatalf("a primary rebind must not invalidate the derived binding %s: %v",
				derived.Snapshot.BindingID, err)
		}
	}
}

// ---------------------------------------------------------------------------
// TestArchivedProjectCannotMerge
// ---------------------------------------------------------------------------

// TestArchivedProjectCannotMerge covers the archive path end to end.
//
// Archiving is the project-level version of retiring a binding: every binding
// the project owns - the primary and all its derived bindings - must be retired
// in the same transaction that writes the archived project row, or a Run created
// before the archive keeps a usable binding. The test drives the real archive
// workflow (ArchiveProjectAndAbortFlows), not DeleteProject directly, so the
// order MarkProjectDeleting -> DeleteProject is exercised as callers use it.
//
// It then asserts the four facts the step is about: every binding is retired
// with a timestamp, every purpose is refused with binding_retired for both the
// primary and a derived binding, the append-only revision history is still
// readable (that is what a pinned Run compares against), and restoring the
// project hands out a brand-new primary binding - so the pinned snapshot is
// refused as binding_rebound rather than being revived by the restore.
func TestArchivedProjectCannotMerge(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("archived", env.rootA)
	primary := env.mustPrimary(project.ID)

	base := filepath.Dir(env.rootA)
	flow, err := env.svc.CreateDerivedBinding(ctx, primary.Snapshot.BindingID, BindingKindFlow,
		mkdirAll(t, filepath.Join(base, "flow-root")), "")
	if err != nil {
		t.Fatalf("CreateDerivedBinding(flow) failed: %v", err)
	}
	run, err := env.svc.CreateDerivedBinding(ctx, primary.Snapshot.BindingID, BindingKindRun,
		mkdirAll(t, filepath.Join(base, "run-root")), "")
	if err != nil {
		t.Fatalf("CreateDerivedBinding(run) failed: %v", err)
	}
	// Everything is usable before the archive, which is what makes the refusals
	// after it meaningful.
	if err := ValidateBindingRecord(primary.Snapshot, primary, BindingPurposeMerge, project.ID, env.allowed); err != nil {
		t.Fatalf("merge on the primary before archiving must validate: %v", err)
	}
	if err := ValidateBindingRecord(run.Snapshot, run, BindingPurposeDispatch, project.ID, env.allowed); err != nil {
		t.Fatalf("dispatch on the derived run before archiving must validate: %v", err)
	}

	// Archive through the real workflow. The engine has no flows for this
	// project, so nothing is aborted; the archive journal is written to the same
	// database.
	if err := ArchiveProjectAndAbortFlows(ctx, env.svc, floweng.NewInMemoryEngine(nil), project.ID); err != nil {
		t.Fatalf("ArchiveProjectAndAbortFlows failed: %v", err)
	}
	archived, err := env.svc.GetProject(ctx, project.ID)
	if err != nil || archived == nil {
		t.Fatalf("GetProject(%s) = %+v, %v", project.ID, archived, err)
	}
	if archived.Status != StatusArchived || archived.BindingState != BindingStateArchived {
		t.Fatalf("archived project = status %q state %q, want %q/%q",
			archived.Status, archived.BindingState, StatusArchived, BindingStateArchived)
	}

	// 1. Every binding of the project is retired, with a timestamp, and none is
	//    left active - asserted against the database, not the store's reads.
	if n := bindingLifecycleCount(t, env.svc,
		`SELECT COUNT(*) FROM workspace_bindings WHERE project_id = ? AND state = 'active'`,
		project.ID); n != 0 {
		t.Fatalf("active bindings after archiving = %d, want 0", n)
	}
	if n := bindingLifecycleCount(t, env.svc,
		`SELECT COUNT(*) FROM workspace_bindings WHERE project_id = ? AND state = 'retired' AND retired_at IS NOT NULL AND retired_at > 0`,
		project.ID); n != 3 {
		t.Fatalf("retired bindings with a timestamp = %d, want 3 (primary + flow + run)", n)
	}
	if _, err := env.svc.ActivePrimaryBinding(ctx, project.ID); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("ActivePrimaryBinding on an archived project = %v, want ErrBindingNotFound", err)
	}
	retiredRecords := make(map[string]WorkspaceBindingRecord, 3)
	for _, record := range []WorkspaceBindingRecord{primary, flow, run} {
		got, err := env.svc.GetBinding(ctx, record.Snapshot.BindingID)
		if err != nil {
			t.Fatalf("GetBinding(%s) failed: %v", record.Snapshot.BindingID, err)
		}
		if got.State != BindingStateRetired || got.RetiredAt == nil || got.RetiredAt.IsZero() {
			t.Fatalf("binding %s after archiving = %+v, want retired with a timestamp", record.Snapshot.BindingID, got)
		}
		if !sameBindingSnapshot(got.Snapshot, record.Snapshot) {
			t.Fatalf("archiving changed the snapshot of %s: %+v -> %+v", record.Snapshot.BindingID, record.Snapshot, got.Snapshot)
		}
		retiredRecords[record.Snapshot.BindingID] = got
	}

	// 2. Every purpose is refused with binding_retired, for the primary and for
	//    the derived binding. Nothing about the filesystem matters here: the
	//    directories still exist and still have their identities.
	purposes := []BindingPurpose{BindingPurposeCreateRun, BindingPurposeDispatch, BindingPurposeMerge}
	for _, pinned := range []WorkspaceBindingRecord{primary, flow, run} {
		current := retiredRecords[pinned.Snapshot.BindingID]
		for _, purpose := range purposes {
			code := validationCode(t, ValidateBindingRecord(pinned.Snapshot, current, purpose, project.ID, env.allowed))
			if code != BindingCodeRetired {
				t.Fatalf("binding %s purpose %s after archiving = %q, want %q",
					pinned.Snapshot.BindingID, purpose, code, BindingCodeRetired)
			}
		}
	}

	// 3. The append-only history is untouched: a Run can still read back what it
	//    pinned, which is the evidence a refusal is based on.
	revision, err := env.svc.GetBindingRevision(ctx, primary.Snapshot.BindingID, 1)
	if err != nil {
		t.Fatalf("GetBindingRevision after archiving failed: %v", err)
	}
	if !sameBindingSnapshot(revision, primary.Snapshot) {
		t.Fatalf("revision 1 after archiving = %+v, want the pinned snapshot %+v", revision, primary.Snapshot)
	}

	// 4. An archived project cannot be backfilled, and restoring it does not
	//    revive the retired binding: the first EnsurePrimaryBinding after the
	//    restore creates a *new* binding, so the pinned snapshot is refused as
	//    binding_rebound (a different binding id is in force) rather than
	//    passing on the strength of its old revision.
	if _, err := env.svc.EnsurePrimaryBinding(ctx, project.ID); err == nil {
		t.Fatal("EnsurePrimaryBinding on an archived project must be refused")
	} else if conflict := bindingConflict(t, err); conflict.Code != BindingConflictNotBound {
		t.Fatalf("EnsurePrimaryBinding on an archived project = %q, want %q", conflict.Code, BindingConflictNotBound)
	}

	restored, err := env.svc.RestoreProject(ctx, project.ID)
	if err != nil || restored == nil {
		t.Fatalf("RestoreProject(%s) = %+v, %v", project.ID, restored, err)
	}
	if restored.Status != StatusPlanning || restored.BindingState != BindingStateBound {
		t.Fatalf("restored project = status %q state %q, want %q/%q",
			restored.Status, restored.BindingState, StatusPlanning, BindingStateBound)
	}
	newPrimary, err := env.svc.EnsurePrimaryBinding(ctx, project.ID)
	if err != nil {
		t.Fatalf("EnsurePrimaryBinding after a restore failed: %v", err)
	}
	if newPrimary.Snapshot.BindingID == primary.Snapshot.BindingID {
		t.Fatal("a restore must not revive the retired primary binding")
	}
	if newPrimary.Snapshot.Revision != 1 || !newPrimary.Active() {
		t.Fatalf("new primary after a restore = %+v, want revision 1 and active", newPrimary)
	}
	if newPrimary.Snapshot.CanonicalRoot != primary.Snapshot.CanonicalRoot {
		t.Fatalf("new primary root = %q, want the project root %q",
			newPrimary.Snapshot.CanonicalRoot, primary.Snapshot.CanonicalRoot)
	}
	code := validationCode(t, ValidateBindingRecord(primary.Snapshot, newPrimary, BindingPurposeMerge, project.ID, env.allowed))
	if code != BindingCodeRebound {
		t.Fatalf("a run pinned before the archive against the restored primary = %q, want %q", code, BindingCodeRebound)
	}
	// The retired rows are still retired - the restore wrote no state back.
	for id, record := range retiredRecords {
		got, err := env.svc.GetBinding(ctx, id)
		if err != nil {
			t.Fatalf("GetBinding(%s) after a restore failed: %v", id, err)
		}
		if got.State != BindingStateRetired {
			t.Fatalf("binding %s after a restore = %q, want %q (was %q)", id, got.State, BindingStateRetired, record.State)
		}
	}

	// 5. The retirement shares the archive's transaction. A binding write that
	//    cannot succeed must leave *both* halves unwritten: the project must not
	//    end up archived, and its bindings must not end up retired. If the
	//    retirement ran in its own transaction after the projects write, the
	//    archive would already have committed by the time the retire failed, and
	//    this section would observe an archived project with an active binding -
	//    the exact state the archive is supposed to make impossible.
	//
	//    The failure is injected inside the transaction (the test-only
	//    failNextBindingWrite hook) rather than before it, because a failure
	//    before BeginTx would not prove the two writes share one commit.
	atomicProject := env.createBoundProject("archive-atomic", env.rootB)
	atomicPrimary := env.mustPrimary(atomicProject.ID)
	if _, err := env.svc.CreateDerivedBinding(ctx, atomicPrimary.Snapshot.BindingID, BindingKindRun,
		mkdirAll(t, filepath.Join(base, "atomic-run")), ""); err != nil {
		t.Fatalf("CreateDerivedBinding for the atomicity project failed: %v", err)
	}
	env.svc.failNextBindingWrite = func(context.Context, *sql.Tx) error {
		return errors.New("injected archive binding write fault")
	}
	if err := env.svc.DeleteProject(ctx, atomicProject.ID); err == nil {
		t.Fatal("the injected binding write fault must fail the archive")
	}
	env.svc.failNextBindingWrite = nil

	// Memory was restored...
	atomicAfter, err := env.svc.GetProject(ctx, atomicProject.ID)
	if err != nil || atomicAfter == nil {
		t.Fatalf("GetProject(%s) = %+v, %v", atomicProject.ID, atomicAfter, err)
	}
	if atomicAfter.Status != StatusPlanning || atomicAfter.BindingState != BindingStateBound {
		t.Fatalf("project after a failed archive = status %q state %q, want %q/%q (restoreState must have run)",
			atomicAfter.Status, atomicAfter.BindingState, StatusPlanning, BindingStateBound)
	}
	// ...and disk agrees after a reopen, which is what proves the projects row
	// was not written either.
	env.reopen()
	persisted, err := env.svc.GetProject(ctx, atomicProject.ID)
	if err != nil || persisted == nil {
		t.Fatalf("GetProject(%s) after reopen = %+v, %v", atomicProject.ID, persisted, err)
	}
	if persisted.Status != StatusPlanning || persisted.BindingState != BindingStateBound {
		t.Fatalf("persisted project after a failed archive = status %q state %q, want %q/%q",
			persisted.Status, persisted.BindingState, StatusPlanning, BindingStateBound)
	}
	if n := bindingLifecycleCount(t, env.svc,
		`SELECT COUNT(*) FROM workspace_bindings WHERE project_id = ? AND state = 'active' AND retired_at IS NULL`,
		atomicProject.ID); n != 2 {
		t.Fatalf("active bindings after a failed archive = %d, want 2 (primary + run, still untouched)", n)
	}
	if _, err := env.svc.ActivePrimaryBinding(ctx, atomicProject.ID); err != nil {
		t.Fatalf("the primary binding must still be active after a failed archive: %v", err)
	}
}
