package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func mustMkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

func mustIdentity(t *testing.T, path string) RootIdentity {
	t.Helper()
	id, err := CaptureRootIdentity(path)
	if err != nil {
		t.Fatalf("CaptureRootIdentity(%s): %v", path, err)
	}
	return id
}

// A junction (or symlink on Unix) is created at link pointing at target.
func mustLinkDir(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Skipf("mklink /J unavailable: %v (%s)", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported (needs privilege / developer mode): %v", err)
	}
}

func mustRemoveLinkDir(t *testing.T, link string) {
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

func TestCaptureRootIdentitySameAndDifferentDirs(t *testing.T) {
	first := mustMkdir(t, filepath.Join(t.TempDir(), "one"))
	second := mustMkdir(t, filepath.Join(t.TempDir(), "two"))

	a := mustIdentity(t, first)
	b := mustIdentity(t, first)
	if !a.Equal(b) {
		t.Fatalf("same directory captured twice must be equal: %s vs %s", a, b)
	}
	if a.IsZero() {
		t.Fatal("captured identity must not be zero")
	}
	if a.String() == "" {
		t.Fatal("identity String must not be empty")
	}
	if a.Platform != "windows" && a.Platform != "unix" {
		t.Fatalf("unexpected platform %q", a.Platform)
	}

	c := mustIdentity(t, second)
	if a.Equal(c) {
		t.Fatalf("different directories must not be equal: %s vs %s", a, c)
	}
}

func TestCaptureRootIdentityRecreateAndMove(t *testing.T) {
	base := t.TempDir()

	// (a) delete + recreate at the same path -> new identity.
	recreated := mustMkdir(t, filepath.Join(base, "recreate"))
	before := mustIdentity(t, recreated)
	if err := os.RemoveAll(recreated); err != nil {
		t.Fatalf("remove %s: %v", recreated, err)
	}
	mustMkdir(t, recreated)
	after := mustIdentity(t, recreated)
	if before.Equal(after) {
		t.Fatalf("directory deleted and recreated at the same path must get a new identity, got %s", before)
	}

	// (b) rename away + recreate the same name -> new identity; the moved
	// directory keeps its identity at the new path.
	moved := mustMkdir(t, filepath.Join(base, "moved"))
	original := mustIdentity(t, moved)
	movedTo := filepath.Join(base, "moved-renamed")
	if err := os.Rename(moved, movedTo); err != nil {
		t.Fatalf("rename %s -> %s: %v", moved, movedTo, err)
	}
	if atNewPath := mustIdentity(t, movedTo); !original.Equal(atNewPath) {
		t.Fatalf("identity must follow the directory across a rename: %s vs %s", original, atNewPath)
	}
	mustMkdir(t, moved)
	if fresh := mustIdentity(t, moved); fresh.Equal(original) {
		t.Fatalf("a new directory at the old name must not reuse the old identity: %s", fresh)
	}
}

func TestCaptureRootIdentityRejectsFileAndMissingPath(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
	if _, err := CaptureRootIdentity(file); err == nil {
		t.Fatal("a regular file must not yield a root identity")
	}
	if _, err := CaptureRootIdentity(filepath.Join(base, "does-not-exist")); err == nil {
		t.Fatal("a missing path must not yield a root identity")
	}
	if _, err := CaptureRootIdentity("   "); err == nil {
		t.Fatal("an empty path must not yield a root identity")
	}
}

// The identity of a directory reached through a link is the identity of the
// link target: that is the directory the OS actually writes into. Windows uses
// a junction (no privilege needed), Unix a symlink.
func TestCaptureRootIdentityFollowsLink(t *testing.T) {
	base := t.TempDir()
	first := mustMkdir(t, filepath.Join(base, "A"))
	second := mustMkdir(t, filepath.Join(base, "B"))
	link := filepath.Join(base, "link")

	mustLinkDir(t, link, first)
	directA := mustIdentity(t, first)
	viaLink := mustIdentity(t, link)
	if !directA.Equal(viaLink) {
		t.Fatalf("identity through link must equal the target identity: target=%s link=%s", directA, viaLink)
	}

	// Re-point the link at B; the same path now yields B's identity.
	mustRemoveLinkDir(t, link)
	mustLinkDir(t, link, second)
	directB := mustIdentity(t, second)
	viaLinkB := mustIdentity(t, link)
	if !directB.Equal(viaLinkB) {
		t.Fatalf("identity through re-pointed link must equal the new target: target=%s link=%s", directB, viaLinkB)
	}
	if viaLinkB.Equal(directA) {
		t.Fatalf("re-pointed link must not keep the old target identity: %s", viaLinkB)
	}

	// On Windows the junction path itself is a reparse point that EvalSymlinks
	// leaves untouched (its resolution fails), so the hardlink through it is
	// exactly why identity capture - not path comparison - is needed.
	if runtime.GOOS == "windows" {
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatalf("EvalSymlinks(%s) on a live junction: %v", link, err)
		}
		if filepath.Clean(resolved) == filepath.Clean(link) {
			// Documented behaviour: the junction path survives re-resolution, so
			// path-only checks cannot detect a re-point. Identity checks can.
			t.Logf("junction path %s survives EvalSymlinks unchanged (identity checks are load-bearing)", link)
		}
	}
}
