package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// --- guard stubs (distinct from service_test.go's guardFunc) ---

// toggleGuard denies BeforeWrite once armed; used to prove Promote re-runs guard.
type toggleGuard struct{ deny bool }

func (g *toggleGuard) BeforeWrite(ctx context.Context, abs string, content []byte) error {
	if g.deny {
		return errors.New("guard denied")
	}
	return nil
}

// pathDenyGuard denies BeforeWrite for any absolute path containing deny.
type pathDenyGuard struct{ deny string }

func (g *pathDenyGuard) BeforeWrite(ctx context.Context, abs string, content []byte) error {
	if g.deny != "" && strings.Contains(filepath.ToSlash(abs), g.deny) {
		return errors.New("guard denied " + g.deny)
	}
	return nil
}

// countingReserver is a race-safe guard exercising the WriteReserver path.
type countingReserver struct {
	reserves int64
	releases int64
}

func (g *countingReserver) BeforeWrite(ctx context.Context, abs string, content []byte) error {
	return nil
}
func (g *countingReserver) ReserveWrite(ctx context.Context, abs string, content []byte) error {
	atomic.AddInt64(&g.reserves, 1)
	return nil
}
func (g *countingReserver) ReleaseWrite(abs string) { atomic.AddInt64(&g.releases, 1) }

// 1. Symlink escape: a symlink inside root pointing outside must be rejected by
// Resolve/Read/Write. On Windows this needs symlink privilege / developer mode.
func TestResolveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Directory symlink inside root pointing at the outside tree.
	linkDir := filepath.Join(root, "escape")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Skipf("symlink unsupported (needs privilege / developer mode): %v", err)
	}

	svc := NewFSService(nil)
	ctx := context.Background()

	if _, err := svc.Resolve(root, "escape/secret.txt"); err == nil {
		t.Fatal("Resolve should reject traversal through a symlink out of root")
	}
	if _, err := svc.Read(ctx, &ReadRequest{Root: root, Path: "escape/secret.txt"}); err == nil {
		t.Fatal("Read should reject a symlinked escape")
	}
	if _, err := svc.Write(ctx, &WriteRequest{Root: root, Path: "escape/pwn.txt", Content: []byte("x"), CreateParents: true}); err == nil {
		t.Fatal("Write should reject a symlinked escape")
	}

	// A direct file symlink out of the tree is rejected too.
	if err := os.Symlink(secret, filepath.Join(root, "leak.txt")); err == nil {
		if _, err := svc.Read(ctx, &ReadRequest{Root: root, Path: "leak.txt"}); err == nil {
			t.Fatal("Read should reject a file symlink escape")
		}
	}

	// The outside secret is untouched and nothing was written outside the root.
	if data, err := os.ReadFile(secret); err != nil || string(data) != "top-secret" {
		t.Fatalf("outside secret altered: %q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "pwn.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escaped into outside dir: err=%v", err)
	}
}

// 1b. Junction escape (Windows): an NTFS junction inside root pointing outside
// must be rejected. Junctions need no special privilege (unlike symlinks), so
// this exercises the reparse-point rejection on ordinary Windows CI. Skips on
// non-Windows and if mklink is unavailable.
func TestResolveRejectsJunctionEscape(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction test is Windows-only")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("mklink /J unavailable: %v (%s)", err, out)
	}

	svc := NewFSService(nil)
	ctx := context.Background()

	if _, err := svc.Resolve(root, "escape/secret.txt"); err == nil {
		t.Fatal("Resolve must reject traversal through a junction out of root")
	}
	if _, err := svc.Read(ctx, &ReadRequest{Root: root, Path: "escape/secret.txt"}); err == nil {
		t.Fatal("Read must reject a junction escape")
	}
	if _, err := svc.Write(ctx, &WriteRequest{Root: root, Path: "escape/pwn.txt", Content: []byte("x")}); err == nil {
		t.Fatal("Write must reject a junction escape")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwn.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through junction into outside dir: err=%v", err)
	}
}

// 2. Promote re-runs guard: staging succeeds, then the guard denies the promote
// (a direct write); the real file is not created and the staged copy remains.
func TestPromoteRerunsGuard(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	root := t.TempDir()
	g := &toggleGuard{}
	svc := NewFSService(g)
	ctx := context.Background()

	if _, err := svc.Write(ctx, &WriteRequest{Root: root, Path: "src/x.go", Content: []byte("pkg"), CreateParents: true, Mode: WriteModeStage}); err != nil {
		t.Fatalf("stage write: %v", err)
	}
	g.deny = true // arm the guard for the promote's re-check

	if _, err := svc.Promote(ctx, root, "src/x.go"); err == nil {
		t.Fatal("Promote should fail when the guard denies the final write")
	}
	if _, err := os.Stat(filepath.Join(root, "src", "x.go")); !os.IsNotExist(err) {
		t.Fatalf("promoted file should not exist: err=%v", err)
	}
	staged, err := svc.ListStaged(ctx, root)
	if err != nil || len(staged) != 1 || staged[0].Path != "src/x.go" {
		t.Fatalf("staged copy should remain for retry: %+v err=%v", staged, err)
	}
}

// 3. PromoteAll partial failure: the guard denies one of two staged files; the
// other promotes, the error names the failure, and the failed file stays staged.
func TestPromoteAllPartialGuardFailure(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	root := t.TempDir()
	g := &pathDenyGuard{}
	svc := NewFSService(g)
	ctx := context.Background()

	for _, p := range []string{"keep.txt", "block.txt"} {
		if _, err := svc.Write(ctx, &WriteRequest{Root: root, Path: p, Content: []byte(p), CreateParents: true, Mode: WriteModeStage}); err != nil {
			t.Fatalf("stage %s: %v", p, err)
		}
	}
	g.deny = "block.txt" // deny only block.txt at promote time

	items, err := svc.PromoteAll(ctx, root)
	if err == nil {
		t.Fatal("PromoteAll should report the blocked file")
	}
	if !strings.Contains(err.Error(), "block.txt") {
		t.Fatalf("error should name the failed file: %v", err)
	}
	if len(items) != 1 || items[0].Path != "keep.txt" {
		t.Fatalf("only keep.txt should promote: %+v", items)
	}
	if data, err := os.ReadFile(filepath.Join(root, "keep.txt")); err != nil || string(data) != "keep.txt" {
		t.Fatalf("keep.txt not promoted: %q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "block.txt")); !os.IsNotExist(err) {
		t.Fatalf("block.txt should not be in the real tree: err=%v", err)
	}
	staged, err := svc.ListStaged(ctx, root)
	if err != nil || len(staged) != 1 || staged[0].Path != "block.txt" {
		t.Fatalf("block.txt should remain staged: %+v err=%v", staged, err)
	}
}

// 4. Concurrent direct writes to the same path: no data race (run with -race)
// and the final content is exactly one of the payloads.
func TestConcurrentWriteSamePath(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	root := t.TempDir()
	svc := NewFSService(nil)
	ctx := context.Background()

	const n = 12
	payloads := make([]string, n)
	for i := range payloads {
		payloads[i] = fmt.Sprintf("payload-%02d", i) // equal length
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Write(ctx, &WriteRequest{Root: root, Path: "race.txt", Content: []byte(payloads[i])})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("write %d failed: %v", i, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(root, "race.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, p := range payloads {
		if string(got) == p {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("final content is not a whole payload: %q", got)
	}
}

// 4b. Concurrent writes through a WriteReserver guard: every direct write
// reserves once and, since all succeed, none roll back (reservation is kept).
func TestConcurrentWriteReservationBalance(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	root := t.TempDir()
	g := &countingReserver{}
	svc := NewFSService(g)
	ctx := context.Background()

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Write(ctx, &WriteRequest{Root: root, Path: fmt.Sprintf("f%02d.txt", i), Content: []byte("x"), CreateParents: true})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("write %d failed: %v", i, err)
		}
	}
	if got := atomic.LoadInt64(&g.reserves); got != n {
		t.Fatalf("reserves=%d want %d", got, n)
	}
	if got := atomic.LoadInt64(&g.releases); got != 0 {
		t.Fatalf("releases=%d want 0 (no failures, reservation kept)", got)
	}
}

// 5. Before-write hook: content transformation lands on disk.
func TestBeforeWriteHookTransformsContent(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite, policy.OperationHookExecute)
	restoreHookManager(t)
	mgr := backendhooks.NewHookManager()
	backendhooks.SetHookManager(mgr)
	err := mgr.Register(backendhooks.HookConfig{Name: "xform", Type: backendhooks.HookBeforeWrite, Enabled: true},
		func(ctx context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
			m, ok := payload.(map[string]interface{})
			if !ok {
				return payload, nil
			}
			m["content"] = []byte("TRANSFORMED")
			return m, nil
		})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}

	root := t.TempDir()
	svc := NewFSService(nil)
	if _, err := svc.Write(context.Background(), &WriteRequest{Root: root, Path: "a.txt", Content: []byte("original")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(data) != "TRANSFORMED" {
		t.Fatalf("hook content not applied: %q err=%v", data, err)
	}
}

// 5b. Before-write hook error fails closed: no file is written.
func TestBeforeWriteHookFailsClosed(t *testing.T) {
	restoreHookManager(t)
	mgr := backendhooks.NewHookManager()
	backendhooks.SetHookManager(mgr)
	err := mgr.Register(backendhooks.HookConfig{Name: "veto", Type: backendhooks.HookBeforeWrite, Enabled: true},
		func(ctx context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
			return payload, errors.New("veto")
		})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}

	root := t.TempDir()
	svc := NewFSService(nil)
	if _, err := svc.Write(context.Background(), &WriteRequest{Root: root, Path: "b.txt", Content: []byte("x")}); err == nil {
		t.Fatal("write should fail when the before-write hook errors")
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("file must not exist after hook veto: err=%v", err)
	}
}

// 6. Staged read at the service level, mirroring handler ReadWorkspaceFile
// ?staged=true (Read with Root pointed at .codeflow/staging). Non-staged read of
// the real tree is absent until promote.
func TestStagedReadViaService(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	root := t.TempDir()
	svc := NewFSService(nil)
	ctx := context.Background()

	if _, err := svc.Write(ctx, &WriteRequest{Root: root, Path: "notes/x.md", Content: []byte("draft"), CreateParents: true, Mode: WriteModeStage}); err != nil {
		t.Fatalf("stage write: %v", err)
	}

	stagingRoot := filepath.Join(root, ".codeflow", "staging")
	fc, err := svc.Read(ctx, &ReadRequest{Root: stagingRoot, Path: "notes/x.md"})
	if err != nil {
		t.Fatalf("staged read: %v", err)
	}
	if string(fc.Content) != "draft" {
		t.Fatalf("staged content=%q", fc.Content)
	}

	if _, err := svc.Read(ctx, &ReadRequest{Root: root, Path: "notes/x.md"}); err == nil {
		t.Fatal("non-staged read should fail before promote")
	}
}

// restoreHookManager snapshots the global hook manager and restores it after the
// test, without triggering GetHookManager's lazy-create side effect.
func restoreHookManager(t *testing.T) {
	t.Helper()
	prevHad := backendhooks.HasHookManager()
	var prev backendhooks.IHookManager
	if prevHad {
		prev = backendhooks.GetHookManager()
	}
	t.Cleanup(func() {
		if prevHad {
			backendhooks.SetHookManager(prev)
		} else {
			backendhooks.SetHookManager(nil)
		}
	})
}
