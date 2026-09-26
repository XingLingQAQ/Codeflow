package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/workspace"
)

// --- helpers ---------------------------------------------------------------

func capturePrimary(t *testing.T, root string, allowedRoots []string) WorkspaceBindingSnapshot {
	t.Helper()
	snapshot, err := CaptureBindingSnapshot(BindingSnapshotInput{
		BindingID: "bind-1",
		ProjectID: "proj-1",
		Kind:      BindingKindPrimary,
		Revision:  1,
		Root:      root,
	}, allowedRoots)
	if err != nil {
		t.Fatalf("CaptureBindingSnapshot(%s): %v", root, err)
	}
	return snapshot
}

func driftCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a drift error, got nil")
	}
	var drift *BindingDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("expected *BindingDriftError, got %T: %v", err, err)
	}
	return drift.Code
}

func mkdirAll(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

// makeJunction creates an NTFS junction (Windows) or a directory symlink
// (elsewhere). Junctions need no privilege, so this must not skip on Windows.
func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J %s %s: %v (%s)", link, target, err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported (needs privilege / developer mode): %v", err)
	}
}

func removeJunction(t *testing.T, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "rmdir", link).CombinedOutput()
		if err != nil {
			t.Fatalf("rmdir %s: %v (%s)", link, err, out)
		}
		return
	}
	if err := os.Remove(link); err != nil {
		t.Fatalf("remove link %s: %v", link, err)
	}
}

// --- capture / Validate ----------------------------------------------------

func TestCaptureBindingSnapshotPrimarySucceeds(t *testing.T) {
	root := t.TempDir()
	snapshot := capturePrimary(t, root, []string{root})

	if snapshot.Revision != 1 {
		t.Fatalf("revision = %d, want 1", snapshot.Revision)
	}
	if snapshot.ParentBindingID != "" {
		t.Fatalf("primary parent binding = %q, want empty", snapshot.ParentBindingID)
	}
	if snapshot.LinkPolicy != LinkPolicyDenyEscape {
		t.Fatalf("link policy = %q, want %q", snapshot.LinkPolicy, LinkPolicyDenyEscape)
	}
	if snapshot.RootIdentity.IsZero() {
		t.Fatal("root identity must not be zero")
	}
	if snapshot.CanonicalRoot == "" {
		t.Fatal("canonical root must not be empty")
	}
	if snapshot.CapturedAt.IsZero() {
		t.Fatal("captured at must be set")
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate on a captured snapshot: %v", err)
	}
	if snapshot.RootIdentity.String() == "" {
		t.Fatal("identity String must not be empty")
	}
	if strings.Contains(snapshot.RootIdentity.String(), root) {
		t.Fatalf("identity String must not contain a path: %s", snapshot.RootIdentity.String())
	}
}

func TestCaptureBindingSnapshotRejectsInvalidInput(t *testing.T) {
	root := t.TempDir()
	allowed := []string{root}

	base := BindingSnapshotInput{
		BindingID: "bind-1",
		ProjectID: "proj-1",
		Kind:      BindingKindPrimary,
		Revision:  1,
		Root:      root,
	}

	cases := []struct {
		name   string
		mutate func(in *BindingSnapshotInput)
	}{
		{"flow without parent", func(in *BindingSnapshotInput) { in.Kind = BindingKindFlow }},
		{"run without parent", func(in *BindingSnapshotInput) { in.Kind = BindingKindRun }},
		{"primary with parent", func(in *BindingSnapshotInput) { in.ParentBindingID = "bind-0" }},
		{"invalid kind", func(in *BindingSnapshotInput) { in.Kind = BindingKind("detached") }},
		{"empty kind", func(in *BindingSnapshotInput) { in.Kind = BindingKind("") }},
		{"revision zero", func(in *BindingSnapshotInput) { in.Revision = 0 }},
		{"revision negative", func(in *BindingSnapshotInput) { in.Revision = -3 }},
		{"missing binding id", func(in *BindingSnapshotInput) { in.BindingID = "  " }},
		{"missing project id", func(in *BindingSnapshotInput) { in.ProjectID = "" }},
		{"empty root", func(in *BindingSnapshotInput) { in.Root = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			if _, err := CaptureBindingSnapshot(in, allowed); err == nil {
				t.Fatalf("CaptureBindingSnapshot must reject %s", tc.name)
			}
		})
	}
}

func TestCaptureBindingSnapshotFlowAndRunRequireParent(t *testing.T) {
	root := t.TempDir()
	for _, kind := range []BindingKind{BindingKindFlow, BindingKindRun} {
		snapshot, err := CaptureBindingSnapshot(BindingSnapshotInput{
			BindingID:       "bind-child",
			ProjectID:       "proj-1",
			Kind:            kind,
			ParentBindingID: "bind-primary",
			Revision:        2,
			Root:            root,
			BaseRef:         "refs/heads/main",
		}, []string{root})
		if err != nil {
			t.Fatalf("CaptureBindingSnapshot(%s): %v", kind, err)
		}
		if snapshot.ParentBindingID != "bind-primary" {
			t.Fatalf("%s parent binding = %q", kind, snapshot.ParentBindingID)
		}
		if snapshot.BaseRef != "refs/heads/main" {
			t.Fatalf("%s base ref = %q", kind, snapshot.BaseRef)
		}
		if err := snapshot.Validate(); err != nil {
			t.Fatalf("Validate(%s): %v", kind, err)
		}
	}
}

func TestCaptureBindingSnapshotAllowedRoots(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()

	// Empty allow-list keeps the existing fail-closed message.
	_, err := CaptureBindingSnapshot(BindingSnapshotInput{
		BindingID: "bind-1", ProjectID: "proj-1", Kind: BindingKindPrimary,
		Revision: 1, Root: root,
	}, nil)
	if err == nil {
		t.Fatal("an empty allow-list must reject capture")
	}
	if !strings.Contains(err.Error(), "CODEFLOW_WORKSPACE_ROOTS") {
		t.Fatalf("empty allow-list error = %v, want the existing disabled-until-configured message", err)
	}

	// A root outside the allow-list is rejected with the existing message.
	_, err = CaptureBindingSnapshot(BindingSnapshotInput{
		BindingID: "bind-1", ProjectID: "proj-1", Kind: BindingKindPrimary,
		Revision: 1, Root: root,
	}, []string{other})
	if err == nil {
		t.Fatal("a root outside allowed roots must reject capture")
	}
	if !strings.Contains(err.Error(), "outside allowed roots") {
		t.Fatalf("outside-root error = %v, want the existing outside-allowed-roots message", err)
	}

	// A subdirectory of an allowed root is accepted.
	sub := mkdirAll(t, filepath.Join(root, "nested", "deep"))
	if _, err := CaptureBindingSnapshot(BindingSnapshotInput{
		BindingID: "bind-2", ProjectID: "proj-1", Kind: BindingKindPrimary,
		Revision: 1, Root: sub,
	}, []string{root}); err != nil {
		t.Fatalf("a nested root inside an allowed root must be accepted: %v", err)
	}
}

func TestCaptureBindingSnapshotMatchesCanonicalize(t *testing.T) {
	root := t.TempDir()
	sub := mkdirAll(t, filepath.Join(root, "sub"))
	canonical, err := CanonicalizeWorkspaceRoot(sub, []string{root})
	if err != nil {
		t.Fatalf("CanonicalizeWorkspaceRoot: %v", err)
	}
	snapshot := capturePrimary(t, sub, []string{root})
	if snapshot.CanonicalRoot != canonical {
		t.Fatalf("snapshot root = %q, canonicalize = %q (capture must reuse the same rule)", snapshot.CanonicalRoot, canonical)
	}
}

// --- Recheck: no drift -----------------------------------------------------

func TestRecheckUnchangedRootPasses(t *testing.T) {
	root := t.TempDir()
	sub := mkdirAll(t, filepath.Join(root, "work"))
	snapshot := capturePrimary(t, sub, []string{root})

	if err := snapshot.Recheck([]string{root}); err != nil {
		t.Fatalf("Recheck on an unchanged root must pass, got %v", err)
	}
	// Repeated checks stay clean (no state is mutated).
	for i := 0; i < 3; i++ {
		if err := snapshot.Recheck([]string{root}); err != nil {
			t.Fatalf("Recheck iteration %d: %v", i, err)
		}
	}
}

// --- Recheck: the four drift codes ----------------------------------------

func TestRecheckRootMissing(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "gone"))
	snapshot := capturePrimary(t, root, []string{base})

	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove %s: %v", root, err)
	}
	if code := driftCode(t, snapshot.Recheck([]string{base})); code != DriftRootMissing {
		t.Fatalf("code = %q, want %q", code, DriftRootMissing)
	}
}

func TestRecheckRootReplacedAfterRecreate(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "recreate"))
	snapshot := capturePrimary(t, root, []string{base})

	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove %s: %v", root, err)
	}
	mkdirAll(t, root)

	if code := driftCode(t, snapshot.Recheck([]string{base})); code != DriftRootReplaced {
		t.Fatalf("code = %q, want %q", code, DriftRootReplaced)
	}
}

func TestRecheckRootReplacedAfterMoveAndRecreate(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "movable"))
	snapshot := capturePrimary(t, root, []string{base})

	movedTo := filepath.Join(base, "movable-renamed")
	if err := os.Rename(root, movedTo); err != nil {
		t.Fatalf("rename %s -> %s: %v", root, movedTo, err)
	}
	mkdirAll(t, root)

	if code := driftCode(t, snapshot.Recheck([]string{base})); code != DriftRootReplaced {
		t.Fatalf("code = %q, want %q", code, DriftRootReplaced)
	}
}

func TestRecheckRootNotAllowed(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "inside"))
	snapshot := capturePrimary(t, root, []string{base})

	// Same root, but the allow-list no longer covers it.
	if code := driftCode(t, snapshot.Recheck([]string{t.TempDir()})); code != DriftRootNotAllowed {
		t.Fatalf("code = %q, want %q", code, DriftRootNotAllowed)
	}
	// An empty allow-list allows nothing.
	if code := driftCode(t, snapshot.Recheck(nil)); code != DriftRootNotAllowed {
		t.Fatalf("empty allow-list code = %q, want %q", code, DriftRootNotAllowed)
	}
}

func TestRecheckRootMissingBeatsNotAllowed(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "gone"))
	snapshot := capturePrimary(t, root, []string{base})
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove %s: %v", root, err)
	}
	// Missing is checked first, so it wins over an out-of-list root.
	if code := driftCode(t, snapshot.Recheck(nil)); code != DriftRootMissing {
		t.Fatalf("code = %q, want %q (missing is checked before the allow-list)", code, DriftRootMissing)
	}
}

// --- Recheck: junction (Windows) ------------------------------------------

// A junction used as the workspace root: canonicalization resolves the link
// when EvalSymlinks can follow it, and the captured identity is the target's.
// Re-pointing the junction must be detected, either as a replaced directory or
// as a redirected path.
func TestRecheckJunctionRootRePointed(t *testing.T) {
	base := t.TempDir()
	first := mkdirAll(t, filepath.Join(base, "target-a"))
	second := mkdirAll(t, filepath.Join(base, "target-b"))
	link := filepath.Join(base, "link")
	makeJunction(t, link, first)

	snapshot := capturePrimary(t, link, []string{base})
	t.Logf("junction root: CanonicalRoot=%q identity=%s", snapshot.CanonicalRoot, snapshot.RootIdentity)

	// Since T1.10.b the capture resolves the junction to its final path, so the
	// snapshot pins the real target (target-a), not the junction spelling.
	firstFinal, err := workspace.FinalPath(first)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CanonicalRoot != firstFinal {
		t.Fatalf("CanonicalRoot = %q, want the junction target %q", snapshot.CanonicalRoot, firstFinal)
	}

	// Unchanged junction root re-checks clean.
	if err := snapshot.Recheck([]string{base}); err != nil {
		t.Fatalf("Recheck on an untouched junction root must pass, got %v", err)
	}

	removeJunction(t, link)
	makeJunction(t, link, second)

	// Re-pointing the junction does not move the pinned snapshot: target-a is
	// untouched, so the pinned binding keeps writing into target-a and never
	// silently follows the link to target-b.
	if err := snapshot.Recheck([]string{base}); err != nil {
		t.Fatalf("the pinned target is untouched, Recheck must still pass, got %v", err)
	}
	// The re-point is visible to a fresh capture through the link, which now
	// yields a different root; ValidateBinding reports that as a rebind
	// (TestValidateBindingJunctionRePointDoesNotMoveCapturedRoot).
	recaptured := capturePrimary(t, link, []string{base})
	if recaptured.RootIdentity.Equal(snapshot.RootIdentity) {
		t.Fatalf("a capture through the re-pointed junction must see the new target, got the pinned identity %s", snapshot.RootIdentity)
	}
}

// --- Recheck: ancestor chain ----------------------------------------------

// The root's own path is untouched, but one of its ancestors is replaced by a
// junction pointing at a different tree. This must be detected.
func TestRecheckAncestorJunctionDetected(t *testing.T) {
	base := t.TempDir()
	parent := mkdirAll(t, filepath.Join(base, "A"))
	root := mkdirAll(t, filepath.Join(parent, "sub"))
	snapshot := capturePrimary(t, root, []string{base})

	if err := snapshot.Recheck([]string{base}); err != nil {
		t.Fatalf("Recheck before the ancestor changes must pass, got %v", err)
	}

	// Move A aside and put a junction named A in its place, pointing at another
	// tree that also contains a "sub" directory.
	if err := os.Rename(parent, filepath.Join(base, "A-renamed")); err != nil {
		t.Fatalf("rename %s: %v", parent, err)
	}
	other := mkdirAll(t, filepath.Join(base, "other"))
	mkdirAll(t, filepath.Join(other, "sub"))
	makeJunction(t, parent, other)

	err := snapshot.Recheck([]string{base})
	code := driftCode(t, err)
	if code != DriftRootRedirected && code != DriftRootReplaced {
		t.Fatalf("ancestor junction code = %q, want %q or %q", code, DriftRootRedirected, DriftRootReplaced)
	}
	// Measured on this machine: Stat still reaches the directory through the
	// junction, but EvalSymlinks fails to resolve a path whose ancestor is a
	// junction, so the ancestry check reports root_redirected.
	if code != DriftRootRedirected {
		t.Fatalf("measured behaviour is %q; if the platform changed, update this assertion and the receipt", code)
	}
	t.Logf("ancestor junction detected as %q: %v", code, err)
}

// --- drift error shape -----------------------------------------------------

func TestBindingDriftErrorCarriesCodeAndDetail(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "gone"))
	snapshot := capturePrimary(t, root, []string{base})
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove %s: %v", root, err)
	}

	err := snapshot.Recheck([]string{base})
	var drift *BindingDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("expected *BindingDriftError via errors.As, got %T", err)
	}
	if drift.Code != DriftRootMissing {
		t.Fatalf("code = %q, want %q", drift.Code, DriftRootMissing)
	}
	if drift.Detail == "" {
		t.Fatal("detail must not be empty")
	}
	if !strings.Contains(drift.Error(), DriftRootMissing) {
		t.Fatalf("error string %q must mention the code", drift.Error())
	}
	// Detail may name the path but must never leak tokens or environment info.
	lower := strings.ToLower(drift.Detail)
	for _, banned := range []string{"token", "secret", "password", "authorization", "bearer", "cookie", "env="} {
		if strings.Contains(lower, banned) {
			t.Fatalf("detail must not contain %q: %s", banned, drift.Detail)
		}
	}
}

// A snapshot with no canonical root cannot be re-checked; it must fail closed
// rather than silently pass.
func TestRecheckEmptyCanonicalRootFailsClosed(t *testing.T) {
	empty := WorkspaceBindingSnapshot{}
	if code := driftCode(t, empty.Recheck(nil)); code != DriftRootMissing {
		t.Fatalf("code = %q, want %q", code, DriftRootMissing)
	}
}
