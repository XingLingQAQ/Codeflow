package workspace

// T1.10.d: the file service is fail-closed. With no CODEFLOW_WORKSPACE_ROOTS
// configured every operation on a root is refused with a sentinel error, the
// temporary migration switch (SetAllowUnrestrictedRoots) is the only way to
// allow arbitrary existing directories, and the switch never widens a
// configured allow-list. The lazy process default (GetService) and the watcher
// fallback service inherit the same rule.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// rootAllowed returns a service whose allow-list is exactly the given roots.
// Tests that operate on t.TempDir() must name their root explicitly: the zero
// value is fail-closed since T1.10.d, so "NewFSService(nil)" alone no longer
// permits a temporary directory.
func rootAllowed(roots ...string) *FSService {
	svc := NewFSService(nil)
	svc.SetAllowedRoots(roots)
	return svc
}

// rootAllowedWithGuard is rootAllowed with a write guard attached.
func rootAllowedWithGuard(g WriteGuard, root string) *FSService {
	svc := NewFSService(g)
	svc.SetAllowedRoots([]string{root})
	return svc
}

// assertRootNotAllowed fails the test unless err is (or wraps) the
// "root not allowed" sentinel.
func assertRootNotAllowed(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected the root-not-allowed sentinel, got nil", what)
	}
	if !errors.Is(err, ErrRootNotAllowed) {
		t.Fatalf("%s error = %v, want errors.Is(err, ErrRootNotAllowed)", what, err)
	}
}

func TestFSServiceUnconfiguredDeniesAllOperations(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	ctx := context.Background()
	root := t.TempDir()
	svc := NewFSService(nil)

	if _, err := svc.Resolve(root, "f.txt"); err != nil {
		assertRootNotAllowed(t, err, "Resolve")
	} else {
		t.Fatal("Resolve on an unconfigured service must be refused")
	}
	if _, err := svc.List(ctx, &ListRequest{Root: root}); err != nil {
		assertRootNotAllowed(t, err, "List")
	} else {
		t.Fatal("List on an unconfigured service must be refused")
	}
	if _, err := svc.Read(ctx, &ReadRequest{Root: root, Path: "f.txt"}); err != nil {
		assertRootNotAllowed(t, err, "Read")
	} else {
		t.Fatal("Read on an unconfigured service must be refused")
	}
	if _, err := svc.Stat(ctx, root, "f.txt"); err != nil {
		assertRootNotAllowed(t, err, "Stat")
	} else {
		t.Fatal("Stat on an unconfigured service must be refused")
	}
	if _, err := svc.Write(ctx, &WriteRequest{Root: root, Path: "planted.txt", Content: []byte("x")}); err != nil {
		assertRootNotAllowed(t, err, "Write")
	} else {
		t.Fatal("Write on an unconfigured service must be refused")
	}
	// The denied write must not have reached disk.
	if _, err := os.Stat(filepath.Join(root, "planted.txt")); !os.IsNotExist(err) {
		t.Fatalf("denied write reached disk: stat err=%v", err)
	}

	// Lazy staging endpoints must fail closed too: ListStaged must not walk a
	// staging tree the allow-list would refuse, and PromoteAll/DiscardAllStaged
	// (which go through it) must not report success.
	if _, err := svc.ListStaged(ctx, root); err != nil {
		assertRootNotAllowed(t, err, "ListStaged")
	} else {
		t.Fatal("ListStaged on an unconfigured service must be refused")
	}
	if _, err := svc.PromoteAll(ctx, root); err != nil {
		assertRootNotAllowed(t, err, "PromoteAll")
	} else {
		t.Fatal("PromoteAll on an unconfigured service must be refused")
	}
	if _, err := svc.DiscardAllStaged(ctx, root); err != nil {
		assertRootNotAllowed(t, err, "DiscardAllStaged")
	} else {
		t.Fatal("DiscardAllStaged on an unconfigured service must be refused")
	}
	if _, err := svc.Promote(ctx, root, "f.txt"); err != nil {
		assertRootNotAllowed(t, err, "Promote")
	} else {
		t.Fatal("Promote on an unconfigured service must be refused")
	}
	if err := svc.DiscardStaged(ctx, root, "f.txt"); err != nil {
		assertRootNotAllowed(t, err, "DiscardStaged")
	} else {
		t.Fatal("DiscardStaged on an unconfigured service must be refused")
	}
}

// The not-configured refusal and the outside-allow-list refusal are distinct
// (different errors, and the not-configured one is ErrRootsUnconfigured) yet
// both satisfy errors.Is(err, ErrRootNotAllowed), which is what handlers map.
func TestFSRootRefusalsShareSentinelAndKeepReason(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()

	unconfigured := NewFSService(nil)
	_, errUnconfigured := unconfigured.Resolve(t.TempDir(), "")
	assertRootNotAllowed(t, errUnconfigured, "unconfigured Resolve")
	if !errors.Is(errUnconfigured, ErrRootsUnconfigured) {
		t.Fatalf("unconfigured error = %v, want errors.Is(err, ErrRootsUnconfigured)", errUnconfigured)
	}

	configured := NewFSService(nil)
	configured.SetAllowedRoots([]string{allowed})
	_, errOutside := configured.Resolve(outside, "")
	assertRootNotAllowed(t, errOutside, "outside Resolve")
	if errors.Is(errOutside, ErrRootsUnconfigured) {
		t.Fatalf("outside-allow-list error = %v must not be the not-configured error", errOutside)
	}
	if errUnconfigured.Error() == errOutside.Error() {
		t.Fatalf("the two refusals must be distinguishable, both read %q", errUnconfigured.Error())
	}
}

func TestFSServiceUnconfiguredSwitchAllowsExistingDirectory(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	ctx := context.Background()
	root := t.TempDir()

	svc := NewFSService(nil)
	if _, err := svc.Resolve(root, ""); err == nil {
		t.Fatal("fixture must be refused before the switch is set")
	}
	svc.SetAllowUnrestrictedRoots(true)

	if _, err := svc.Resolve(root, "f.txt"); err != nil {
		t.Fatalf("switch on + no allow-list must accept an existing directory: %v", err)
	}
	if _, err := svc.Write(ctx, &WriteRequest{Root: root, Path: "f.txt", Content: []byte("ok")}); err != nil {
		t.Fatalf("Write with the migration switch on: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "f.txt")); err != nil || string(data) != "ok" {
		t.Fatalf("written file = %q, %v", data, err)
	}
}

func TestFSServiceSwitchNeverBypassesConfiguredAllowList(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	ctx := context.Background()
	allowed := t.TempDir()
	outside := t.TempDir()

	svc := NewFSService(nil)
	svc.SetAllowedRoots([]string{allowed})
	svc.SetAllowUnrestrictedRoots(true)

	if _, err := svc.Resolve(outside, ""); err != nil {
		assertRootNotAllowed(t, err, "Resolve outside the allow-list with the switch on")
	} else {
		t.Fatal("the switch must not widen a configured allow-list")
	}
	if _, err := svc.Write(ctx, &WriteRequest{Root: outside, Path: "planted.txt", Content: []byte("x")}); err != nil {
		assertRootNotAllowed(t, err, "Write outside the allow-list with the switch on")
	} else {
		t.Fatal("the switch must not widen a configured allow-list for writes")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.txt")); !os.IsNotExist(err) {
		t.Fatalf("denied write reached disk: stat err=%v", err)
	}
	// The allow-listed root is unaffected by the switch.
	if _, err := svc.Write(ctx, &WriteRequest{Root: allowed, Path: "ok.txt", Content: []byte("ok")}); err != nil {
		t.Fatalf("allow-listed write must succeed: %v", err)
	}
}

func TestFSServiceAllowedRootPermitsOperations(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewFSService(nil)
	svc.SetAllowedRoots([]string{root})

	if _, err := svc.Resolve(root, "seed.txt"); err != nil {
		t.Fatalf("Resolve inside the allow-list: %v", err)
	}
	entries, err := svc.List(ctx, &ListRequest{Root: root})
	if err != nil || len(entries) != 1 {
		t.Fatalf("List inside the allow-list = %+v, %v", entries, err)
	}
	fc, err := svc.Read(ctx, &ReadRequest{Root: root, Path: "seed.txt"})
	if err != nil || string(fc.Content) != "seed" {
		t.Fatalf("Read inside the allow-list = %+v, %v", fc, err)
	}
	if _, err := svc.Stat(ctx, root, "seed.txt"); err != nil {
		t.Fatalf("Stat inside the allow-list: %v", err)
	}
	if _, err := svc.Write(ctx, &WriteRequest{Root: root, Path: "new.txt", Content: []byte("new")}); err != nil {
		t.Fatalf("Write inside the allow-list: %v", err)
	}
}

// The lazy process default service (workspace.GetService without bootstrap)
// must refuse everything just like a zero-value FSService: "no bootstrap, no
// allow".
func TestGetServiceLazyDefaultIsFailClosed(t *testing.T) {
	prev := GetService()
	t.Cleanup(func() { SetService(prev) })
	SetService(nil)
	svc := GetService()
	if svc == nil {
		t.Fatal("GetService must return the lazy default")
	}
	if _, err := svc.Resolve(t.TempDir(), ""); err != nil {
		assertRootNotAllowed(t, err, "lazy default Resolve")
	} else {
		t.Fatal("the lazy default service must refuse an unconfigured root")
	}
	if _, err := svc.List(context.Background(), &ListRequest{Root: t.TempDir()}); err != nil {
		assertRootNotAllowed(t, err, "lazy default List")
	} else {
		t.Fatal("the lazy default service must refuse List on an unconfigured root")
	}
}

// NewWatcher(nil, root) falls back to a fail-closed FSService, so starting a
// watch on a root the service does not allow must fail with the sentinel
// instead of silently polling an unscanned (empty) tree.
func TestWatcherNilServiceUnconfiguredRootFailsToStart(t *testing.T) {
	root := t.TempDir()
	w := NewWatcher(nil, root)
	err := w.Start(context.Background())
	if err == nil {
		w.Stop()
		t.Fatal("Start with a nil service (fail-closed default) must fail")
	}
	assertRootNotAllowed(t, err, "watcher Start")
	// A failed start does not consume the watcher's single-use budget; Stop is
	// still a no-op rather than a panic.
	w.Stop()
}

func TestWatcherConfiguredRootStarts(t *testing.T) {
	root := t.TempDir()
	svc := NewFSService(nil)
	svc.SetAllowedRoots([]string{root})
	w := NewWatcher(svc, root)
	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("Start with an allow-listed root: %v", err)
	}
	w.Stop()
}
