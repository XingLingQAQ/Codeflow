package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/workspace"
)

// --- helpers ---------------------------------------------------------------

// validationCode returns the stable code of a failed ValidateBinding call.
func validationCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a binding validation error, got nil")
	}
	var invalid *BindingValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected *BindingValidationError via errors.As, got %T: %v", err, err)
	}
	if invalid.Code == "" {
		t.Fatal("validation error must carry a code")
	}
	if invalid.Detail == "" {
		t.Fatal("validation error must carry a detail")
	}
	if !strings.Contains(invalid.Error(), invalid.Code) {
		t.Fatalf("error string %q must mention the code %q", invalid.Error(), invalid.Code)
	}
	return invalid.Code
}

func captureBinding(t *testing.T, id, projectID string, kind BindingKind, parent string, revision int64, root string, allowedRoots []string) WorkspaceBindingSnapshot {
	t.Helper()
	snapshot, err := CaptureBindingSnapshot(BindingSnapshotInput{
		BindingID:       id,
		ProjectID:       projectID,
		Kind:            kind,
		ParentBindingID: parent,
		Revision:        revision,
		Root:            root,
	}, allowedRoots)
	if err != nil {
		t.Fatalf("CaptureBindingSnapshot(%s/%s rev %d root %s): %v", id, kind, revision, root, err)
	}
	return snapshot
}

// --- happy path ------------------------------------------------------------

func TestValidateBindingSameSnapshotPasses(t *testing.T) {
	root := t.TempDir()
	allowed := []string{root}
	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)

	for _, purpose := range []BindingPurpose{BindingPurposeCreateRun, BindingPurposeDispatch, BindingPurposeMerge} {
		t.Run(string(purpose), func(t *testing.T) {
			if err := ValidateBinding(pinned, pinned, purpose, "proj-1", allowed); err != nil {
				t.Fatalf("an unchanged binding must validate for %s, got %v", purpose, err)
			}
		})
	}
	// Repeated calls stay clean (no state is kept).
	for i := 0; i < 3; i++ {
		if err := ValidateBinding(pinned, pinned, BindingPurposeDispatch, "proj-1", allowed); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}

func TestValidateBindingDerivedKindsPassForCreateRunAndDispatch(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "run-root"))
	allowed := []string{base}
	for _, kind := range []BindingKind{BindingKindFlow, BindingKindRun} {
		snapshot := captureBinding(t, "bind-"+string(kind), "proj-1", kind, "bind-primary", 1, root, allowed)
		for _, purpose := range []BindingPurpose{BindingPurposeCreateRun, BindingPurposeDispatch} {
			if err := ValidateBinding(snapshot, snapshot, purpose, "proj-1", allowed); err != nil {
				t.Fatalf("%s binding must validate for %s: %v", kind, purpose, err)
			}
		}
	}
}

// --- project binding -------------------------------------------------------

func TestValidateBindingProjectMismatch(t *testing.T) {
	root := t.TempDir()
	allowed := []string{root}
	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)

	if code := validationCode(t, ValidateBinding(pinned, pinned, BindingPurposeDispatch, "proj-2", allowed)); code != BindingCodeProjectMismatch {
		t.Fatalf("code = %q, want %q", code, BindingCodeProjectMismatch)
	}
	// An empty project id fails closed too: no project named, no access.
	if code := validationCode(t, ValidateBinding(pinned, pinned, BindingPurposeDispatch, "", allowed)); code != BindingCodeProjectMismatch {
		t.Fatalf("empty project code = %q, want %q", code, BindingCodeProjectMismatch)
	}
	// A pinned snapshot without a project cannot be attributed to anyone.
	orphan := pinned
	orphan.ProjectID = ""
	if code := validationCode(t, ValidateBinding(orphan, orphan, BindingPurposeDispatch, "proj-1", allowed)); code != BindingCodeProjectMismatch {
		t.Fatalf("orphan binding code = %q, want %q", code, BindingCodeProjectMismatch)
	}
}

// --- rebound: the old run must not follow the new root ----------------------

// A primary binding is rebound to a new root (revision bump, new identity). A
// run that pinned revision 1 must be refused for every purpose instead of
// silently writing into the new directory.
func TestValidateBindingRejectsReboundPrimary(t *testing.T) {
	baseA := t.TempDir()
	baseB := t.TempDir()
	rootA := mkdirAll(t, filepath.Join(baseA, "work"))
	rootB := mkdirAll(t, filepath.Join(baseB, "work"))
	allowed := []string{baseA, baseB}

	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, rootA, allowed)
	current := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 2, rootB, allowed)

	for _, purpose := range []BindingPurpose{BindingPurposeCreateRun, BindingPurposeDispatch, BindingPurposeMerge} {
		t.Run(string(purpose), func(t *testing.T) {
			err := ValidateBinding(pinned, current, purpose, "proj-1", allowed)
			if code := validationCode(t, err); code != BindingCodeRebound {
				t.Fatalf("code = %q, want %q", code, BindingCodeRebound)
			}
			var invalid *BindingValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected *BindingValidationError, got %T", err)
			}
			if invalid.Purpose != purpose {
				t.Fatalf("purpose = %q, want %q", invalid.Purpose, purpose)
			}
			// The refusal must win over any filesystem drift: both roots exist
			// and are allow-listed, so nothing else could explain it.
			if invalid.DriftErr != nil {
				t.Fatalf("a rebind refusal must not report drift: %v", invalid.DriftErr)
			}
		})
	}
}

func TestValidateBindingRejectsDifferentRevisionOnly(t *testing.T) {
	root := t.TempDir()
	allowed := []string{root}
	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)
	current := pinned
	current.Revision = 2
	if code := validationCode(t, ValidateBinding(pinned, current, BindingPurposeDispatch, "proj-1", allowed)); code != BindingCodeRebound {
		t.Fatalf("code = %q, want %q", code, BindingCodeRebound)
	}
}

func TestValidateBindingRejectsDifferentBindingID(t *testing.T) {
	root := t.TempDir()
	allowed := []string{root}
	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)
	current := pinned
	current.BindingID = "bind-2"
	if code := validationCode(t, ValidateBinding(pinned, current, BindingPurposeDispatch, "proj-1", allowed)); code != BindingCodeRebound {
		t.Fatalf("code = %q, want %q", code, BindingCodeRebound)
	}
}

// Same id, same revision, same path string - but the directory behind the path
// is a different one. The identity check must catch it.
func TestValidateBindingRejectsIdentitySwapAtSameRevision(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "work"))
	allowed := []string{base}
	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)

	// Delete and recreate: same path, new identity.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	mkdirAll(t, root)
	current := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)

	if current.RootIdentity.Equal(pinned.RootIdentity) {
		t.Fatal("fixture error: recreate must yield a new identity")
	}
	if code := validationCode(t, ValidateBinding(pinned, current, BindingPurposeDispatch, "proj-1", allowed)); code != BindingCodeRebound {
		t.Fatalf("code = %q, want %q", code, BindingCodeRebound)
	}
}

func TestValidateBindingNoBindingInForce(t *testing.T) {
	root := t.TempDir()
	allowed := []string{root}
	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)

	if code := validationCode(t, ValidateBinding(pinned, WorkspaceBindingSnapshot{}, BindingPurposeDispatch, "proj-1", allowed)); code != BindingCodeRebound {
		t.Fatalf("empty current code = %q, want %q", code, BindingCodeRebound)
	}
}

func TestValidateBindingStructurallyInvalidCandidate(t *testing.T) {
	root := t.TempDir()
	allowed := []string{root}
	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)

	broken := pinned
	broken.LinkPolicy = "follow_anything"
	broken.Revision = 0
	if code := validationCode(t, ValidateBinding(pinned, broken, BindingPurposeDispatch, "proj-1", allowed)); code == "" {
		t.Fatal("a structurally invalid candidate must be refused")
	} else if code != BindingCodeCandidateInvalid {
		t.Fatalf("code = %q, want %q (structural problems are reported before a rebind)", code, BindingCodeCandidateInvalid)
	}
}

// --- purpose rules ---------------------------------------------------------

func TestValidateBindingUnknownPurposeFailsClosed(t *testing.T) {
	root := t.TempDir()
	allowed := []string{root}
	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)

	for _, purpose := range []BindingPurpose{"", "delete", "MERGE", "merge "} {
		if code := validationCode(t, ValidateBinding(pinned, pinned, purpose, "proj-1", allowed)); code != BindingCodeInvalidPurpose {
			t.Fatalf("purpose %q code = %q, want %q", purpose, code, BindingCodeInvalidPurpose)
		}
	}
	// The supported purposes are exactly the three the plan names.
	for _, purpose := range []BindingPurpose{BindingPurposeCreateRun, BindingPurposeDispatch, BindingPurposeMerge} {
		if !purpose.Valid() {
			t.Fatalf("purpose %q must be supported", purpose)
		}
	}
}

func TestValidateBindingKindRules(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "root"))
	allowed := []string{base}

	cases := []struct {
		kind       BindingKind
		purpose    BindingPurpose
		wantOK     bool
		wantCodeIs string
	}{
		{BindingKindPrimary, BindingPurposeMerge, true, ""},
		{BindingKindFlow, BindingPurposeMerge, false, BindingCodeKindNotMergeable},
		{BindingKindRun, BindingPurposeMerge, false, BindingCodeKindNotMergeable},
		{BindingKindFlow, BindingPurposeCreateRun, true, ""},
		{BindingKindRun, BindingPurposeDispatch, true, ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind)+"/"+string(tc.purpose), func(t *testing.T) {
			parent := ""
			if tc.kind != BindingKindPrimary {
				parent = "bind-primary"
			}
			snapshot := captureBinding(t, "bind-"+string(tc.kind), "proj-1", tc.kind, parent, 1, root, allowed)
			err := ValidateBinding(snapshot, snapshot, tc.purpose, "proj-1", allowed)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("%s/%s must be allowed: %v", tc.kind, tc.purpose, err)
				}
				return
			}
			if code := validationCode(t, err); code != tc.wantCodeIs {
				t.Fatalf("code = %q, want %q", code, tc.wantCodeIs)
			}
		})
	}
}

// --- drift codes through the validator -------------------------------------

func TestValidateBindingReportsDriftCodes(t *testing.T) {
	base := t.TempDir()
	allowed := []string{base}

	t.Run("root_missing", func(t *testing.T) {
		root := mkdirAll(t, filepath.Join(base, "gone"))
		snapshot := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		if code := validationCode(t, ValidateBinding(snapshot, snapshot, BindingPurposeDispatch, "proj-1", allowed)); code != DriftRootMissing {
			t.Fatalf("code = %q, want %q", code, DriftRootMissing)
		}
	})

	t.Run("root_replaced", func(t *testing.T) {
		root := mkdirAll(t, filepath.Join(base, "recreate"))
		snapshot := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		mkdirAll(t, root)
		if code := validationCode(t, ValidateBinding(snapshot, snapshot, BindingPurposeDispatch, "proj-1", allowed)); code != DriftRootReplaced {
			t.Fatalf("code = %q, want %q", code, DriftRootReplaced)
		}
	})

	t.Run("root_redirected", func(t *testing.T) {
		parent := mkdirAll(t, filepath.Join(base, "A"))
		root := mkdirAll(t, filepath.Join(parent, "sub"))
		snapshot := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)
		if err := os.Rename(parent, filepath.Join(base, "A-renamed")); err != nil {
			t.Fatal(err)
		}
		other := mkdirAll(t, filepath.Join(base, "other"))
		mkdirAll(t, filepath.Join(other, "sub"))
		makeJunction(t, parent, other)

		err := ValidateBinding(snapshot, snapshot, BindingPurposeDispatch, "proj-1", allowed)
		code := validationCode(t, err)
		if code != DriftRootRedirected && code != DriftRootReplaced {
			t.Fatalf("code = %q, want %q or %q", code, DriftRootRedirected, DriftRootReplaced)
		}
		t.Logf("ancestor junction through the validator: %q: %v", code, err)
	})

	t.Run("root_not_allowed", func(t *testing.T) {
		root := mkdirAll(t, filepath.Join(base, "inside"))
		snapshot := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)
		// Same root, but the allow-list no longer covers it.
		if code := validationCode(t, ValidateBinding(snapshot, snapshot, BindingPurposeDispatch, "proj-1", []string{t.TempDir()})); code != DriftRootNotAllowed {
			t.Fatalf("code = %q, want %q", code, DriftRootNotAllowed)
		}
		// An empty allow-list allows nothing.
		if code := validationCode(t, ValidateBinding(snapshot, snapshot, BindingPurposeDispatch, "proj-1", nil)); code != DriftRootNotAllowed {
			t.Fatalf("empty allow-list code = %q, want %q", code, DriftRootNotAllowed)
		}
	})
}

// A drift refusal carries the underlying drift, so a caller can inspect both the
// stable code and the drift detail.
func TestValidateBindingExposesDriftError(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "gone"))
	snapshot := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, []string{base})
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}

	err := ValidateBinding(snapshot, snapshot, BindingPurposeMerge, "proj-1", []string{base})
	var invalid *BindingValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected *BindingValidationError, got %T", err)
	}
	if invalid.Code != DriftRootMissing {
		t.Fatalf("code = %q, want %q", invalid.Code, DriftRootMissing)
	}
	if invalid.DriftErr == nil {
		t.Fatal("a drift refusal must expose the drift error")
	}
	var drift *BindingDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("*BindingValidationError must unwrap to *BindingDriftError, got %T", err)
	}
	if drift.Code != DriftRootMissing {
		t.Fatalf("drift code = %q, want %q", drift.Code, DriftRootMissing)
	}
}

// --- junction escape through the validator ---------------------------------

// A run pinned on a junction that points outside the allow-list must not be
// usable: neither the capture nor the validation may treat the link as an
// allow-listed root. This is the I-48 fix end to end through the validator.
func TestValidateBindingRejectsJunctionEscape(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "j")
	makeJunction(t, link, outside)

	// Capture must refuse the escaping junction outright.
	if _, err := CaptureBindingSnapshot(BindingSnapshotInput{
		BindingID: "bind-1", ProjectID: "proj-1", Kind: BindingKindPrimary, Revision: 1, Root: link,
	}, []string{allowed}); err == nil {
		t.Fatalf("capturing a snapshot on the escaping junction %s must fail", link)
	}

	// A snapshot forged on the outside target cannot be used as an allowed
	// binding for the project either: the path is outside the allow-list.
	forged := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, outside, []string{outside})
	if code := validationCode(t, ValidateBinding(forged, forged, BindingPurposeDispatch, "proj-1", []string{allowed})); code != DriftRootNotAllowed {
		t.Fatalf("code = %q, want %q", code, DriftRootNotAllowed)
	}
}

// A junction inside the allow-list pointing at another directory inside the
// allow-list is legitimate: the snapshot is captured on the target's real path
// and validates.
func TestValidateBindingAcceptsInternalJunction(t *testing.T) {
	allowed := t.TempDir()
	target := mkdirAll(t, filepath.Join(allowed, "real"))
	if err := os.WriteFile(filepath.Join(target, "f.txt"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "link")
	makeJunction(t, link, target)

	snapshot := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, link, []string{allowed})
	wantFinal, err := workspace.FinalPath(target)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CanonicalRoot != wantFinal {
		t.Fatalf("CanonicalRoot = %q, want the real target %q", snapshot.CanonicalRoot, wantFinal)
	}
	if err := ValidateBinding(snapshot, snapshot, BindingPurposeMerge, "proj-1", []string{allowed}); err != nil {
		t.Fatalf("an internal junction must validate: %v", err)
	}

	// Re-pointing the junction at a directory outside the allow-list must not
	// produce a usable binding: capture refuses it.
	outside := t.TempDir()
	if err := os.Remove(link); err != nil {
		// Junctions are removed with rmdir on Windows; os.Remove delegates to
		// that, but fall back to the helper for safety.
		removeJunction(t, link)
	}
	makeJunction(t, link, outside)
	if _, err := CaptureBindingSnapshot(BindingSnapshotInput{
		BindingID: "bind-2", ProjectID: "proj-1", Kind: BindingKindPrimary, Revision: 1, Root: link,
	}, []string{allowed}); err == nil {
		t.Fatalf("a junction re-pointed outside the allow-list must not be captured")
	}
}

// --- the root itself becomes a junction ------------------------------------

// The canonical root is a real directory when it is captured. Replacing that
// directory with a junction that points somewhere else must be detected: the
// path now leads to another tree, so the pinned run may not use it.
func TestValidateBindingDetectsRootReplacedByJunction(t *testing.T) {
	base := t.TempDir()
	root := mkdirAll(t, filepath.Join(base, "work"))
	elsewhere := mkdirAll(t, filepath.Join(base, "elsewhere"))
	allowed := []string{base}

	snapshot := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)
	if err := ValidateBinding(snapshot, snapshot, BindingPurposeDispatch, "proj-1", allowed); err != nil {
		t.Fatalf("before the swap the binding must validate: %v", err)
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove %s: %v", root, err)
	}
	makeJunction(t, root, elsewhere)

	err := ValidateBinding(snapshot, snapshot, BindingPurposeDispatch, "proj-1", allowed)
	code := validationCode(t, err)
	if code != DriftRootRedirected && code != DriftRootReplaced {
		t.Fatalf("code = %q, want %q or %q", code, DriftRootRedirected, DriftRootReplaced)
	}
	t.Logf("root replaced by a junction detected as %q: %v", code, err)
}

// A junction that was re-pointed at another target inside the allow-list: the
// snapshot captured the *target* directory, so the pinned run keeps writing
// into the original target and the binding is not affected. The rebind check is
// what refuses an old run after the binding itself moves.
func TestValidateBindingJunctionRePointDoesNotMoveCapturedRoot(t *testing.T) {
	base := t.TempDir()
	first := mkdirAll(t, filepath.Join(base, "target-a"))
	second := mkdirAll(t, filepath.Join(base, "target-b"))
	link := filepath.Join(base, "link")
	makeJunction(t, link, first)
	allowed := []string{base}

	snapshot := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, link, allowed)
	if snapshot.CanonicalRoot == filepath.Clean(link) {
		t.Fatalf("canonical root must be the junction target, not the link: %q", snapshot.CanonicalRoot)
	}
	firstFinal, err := workspace.FinalPath(first)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CanonicalRoot != firstFinal {
		t.Fatalf("CanonicalRoot = %q, want %q", snapshot.CanonicalRoot, firstFinal)
	}

	removeJunction(t, link)
	makeJunction(t, link, second)

	// The captured root is untouched, so the pinned run is still usable and
	// still writes into target-a - it did not silently follow the link.
	if err := ValidateBinding(snapshot, snapshot, BindingPurposeDispatch, "proj-1", allowed); err != nil {
		t.Fatalf("the captured target must stay usable: %v", err)
	}
	if err := snapshot.Recheck(allowed); err != nil {
		t.Fatalf("Recheck on the captured target: %v", err)
	}
	// A new capture through the same link now yields the new target: that is a
	// different binding revision, which is what the rebind check refuses.
	recaptured := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 2, link, allowed)
	secondFinal, err := workspace.FinalPath(second)
	if err != nil {
		t.Fatal(err)
	}
	if recaptured.CanonicalRoot != secondFinal {
		t.Fatalf("re-captured root = %q, want %q", recaptured.CanonicalRoot, secondFinal)
	}
	if code := validationCode(t, ValidateBinding(snapshot, recaptured, BindingPurposeDispatch, "proj-1", allowed)); code != BindingCodeRebound {
		t.Fatalf("code = %q, want %q", code, BindingCodeRebound)
	}
}

// --- pinned snapshot versus the binding in force ---------------------------

// The intended lifecycle: a Run pins the primary binding when it is created; a
// later rebind changes the binding in force; the run's dispatch is refused and
// the same run may not be merged into the new root either.
func TestValidateBindingRebindLifecycle(t *testing.T) {
	baseOld := t.TempDir()
	baseNew := t.TempDir()
	rootOld := mkdirAll(t, filepath.Join(baseOld, "project"))
	rootNew := mkdirAll(t, filepath.Join(baseNew, "project"))
	allowed := []string{baseOld, baseNew}

	pinned := captureBinding(t, "bind-primary", "proj-1", BindingKindPrimary, "", 1, rootOld, allowed)
	if err := ValidateBinding(pinned, pinned, BindingPurposeDispatch, "proj-1", allowed); err != nil {
		t.Fatalf("before the rebind the run must be usable: %v", err)
	}

	rebound := captureBinding(t, "bind-primary", "proj-1", BindingKindPrimary, "", 2, rootNew, allowed)
	if rebound.RootIdentity.Equal(pinned.RootIdentity) {
		t.Fatal("fixture error: the rebind must point at a different directory")
	}
	if code := validationCode(t, ValidateBinding(pinned, rebound, BindingPurposeDispatch, "proj-1", allowed)); code != BindingCodeRebound {
		t.Fatalf("dispatch after rebind code = %q, want %q", code, BindingCodeRebound)
	}
	if code := validationCode(t, ValidateBinding(pinned, rebound, BindingPurposeMerge, "proj-1", allowed)); code != BindingCodeRebound {
		t.Fatalf("merge after rebind code = %q, want %q", code, BindingCodeRebound)
	}
	// The rebound run itself validates against the new binding in force.
	if err := ValidateBinding(rebound, rebound, BindingPurposeMerge, "proj-1", allowed); err != nil {
		t.Fatalf("the new run must be usable: %v", err)
	}
}

// A derived (flow/run) binding has its own id and parent. It never claims the
// primary's id, so pinning it does not disturb a primary binding that is also in
// force - the validator only compares like with like.
func TestValidateBindingDerivedBindingKeepsParentTraceable(t *testing.T) {
	base := t.TempDir()
	primaryRoot := mkdirAll(t, filepath.Join(base, "primary"))
	runRoot := mkdirAll(t, filepath.Join(base, "runs", "r1"))
	allowed := []string{base}

	primary := captureBinding(t, "bind-primary", "proj-1", BindingKindPrimary, "", 1, primaryRoot, allowed)
	runBinding := captureBinding(t, "bind-run-1", "proj-1", BindingKindRun, "bind-primary", 1, runRoot, allowed)

	if runBinding.ParentBindingID != primary.BindingID {
		t.Fatalf("derived binding parent = %q, want %q", runBinding.ParentBindingID, primary.BindingID)
	}
	if runBinding.BindingID == primary.BindingID {
		t.Fatal("a derived binding must not reuse the primary binding id")
	}
	if err := ValidateBinding(runBinding, runBinding, BindingPurposeDispatch, "proj-1", allowed); err != nil {
		t.Fatalf("a derived run binding must validate for dispatch: %v", err)
	}
	// The primary binding stays usable while the derived one is in force: the
	// derived binding does not steal the primary's identity.
	if err := ValidateBinding(primary, primary, BindingPurposeMerge, "proj-1", allowed); err != nil {
		t.Fatalf("the primary binding must stay usable: %v", err)
	}
	// A run pinning the primary is not satisfied by the derived binding.
	if code := validationCode(t, ValidateBinding(primary, runBinding, BindingPurposeDispatch, "proj-1", allowed)); code != BindingCodeRebound {
		t.Fatalf("code = %q, want %q", code, BindingCodeRebound)
	}
}

// --- secret hygiene --------------------------------------------------------

func TestValidateBindingErrorsDoNotLeakSecrets(t *testing.T) {
	root := t.TempDir()
	allowed := []string{root}
	pinned := captureBinding(t, "bind-1", "proj-1", BindingKindPrimary, "", 1, root, allowed)
	current := pinned
	current.Revision = 7

	err := ValidateBinding(pinned, current, BindingPurposeDispatch, "proj-1", allowed)
	lower := strings.ToLower(err.Error())
	for _, banned := range []string{"token", "secret", "password", "authorization", "bearer", "cookie", "env="} {
		if strings.Contains(lower, banned) {
			t.Fatalf("error must not contain %q: %s", banned, err)
		}
	}
}
