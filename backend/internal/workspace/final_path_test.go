package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- FinalPath -------------------------------------------------------------

func TestFinalPathPlainDirectoryIsItself(t *testing.T) {
	dir := mustMkdir(t, filepath.Join(t.TempDir(), "plain"))
	got, err := FinalPath(dir)
	if err != nil {
		t.Fatalf("FinalPath(%s): %v", dir, err)
	}
	// A directory with no links in its ancestry must resolve to itself: same
	// directory (OS identity), same absolute path, still a directory. The
	// comparison is on identity rather than on the spelling so the test holds on
	// a temp path that contains 8.3 components.
	if !filepath.IsAbs(got) {
		t.Fatalf("FinalPath(%s) = %q, want an absolute path", dir, got)
	}
	info, err := os.Stat(got)
	if err != nil || !info.IsDir() {
		t.Fatalf("FinalPath(%s) = %q is not a directory: %v", dir, got, err)
	}
	if !mustIdentity(t, got).Equal(mustIdentity(t, dir)) {
		t.Fatalf("FinalPath(%s) = %q is a different directory", dir, got)
	}
	if strings.Contains(got, "~") {
		t.Fatalf("final path must use long names, got %q", got)
	}
	// Stable across repeated calls (read-only primitive, no state).
	again, err := FinalPath(dir)
	if err != nil || again != got {
		t.Fatalf("FinalPath is not stable: %q then %q (err %v)", got, again, err)
	}
	// Idempotent: resolving an already final path changes nothing.
	twice, err := FinalPath(got)
	if err != nil || twice != got {
		t.Fatalf("FinalPath(FinalPath(%s)) = %q, want %q (err %v)", dir, twice, got, err)
	}
}

func TestFinalPathJunctionResolvesToTarget(t *testing.T) {
	base := t.TempDir()
	target := mustMkdir(t, filepath.Join(base, "target"))
	link := filepath.Join(base, "link")
	mustLinkDir(t, link, target)

	got, err := FinalPath(link)
	if err != nil {
		t.Fatalf("FinalPath(%s): %v", link, err)
	}
	wantTarget, err := FinalPath(target)
	if err != nil {
		t.Fatalf("FinalPath(%s): %v", target, err)
	}
	if got != wantTarget {
		t.Fatalf("FinalPath(%s) = %q, want the target %q (junction must not resolve to itself)", link, got, wantTarget)
	}
	if filepath.Clean(got) == filepath.Clean(link) {
		t.Fatalf("FinalPath(%s) returned the link path itself", link)
	}
}

func TestFinalPathJunctionRePointChangesResult(t *testing.T) {
	base := t.TempDir()
	first := mustMkdir(t, filepath.Join(base, "a"))
	second := mustMkdir(t, filepath.Join(base, "b"))
	link := filepath.Join(base, "link")
	mustLinkDir(t, link, first)

	before, err := FinalPath(link)
	if err != nil {
		t.Fatalf("FinalPath(%s): %v", link, err)
	}
	mustRemoveLinkDir(t, link)
	mustLinkDir(t, link, second)
	after, err := FinalPath(link)
	if err != nil {
		t.Fatalf("FinalPath(%s) after re-point: %v", link, err)
	}
	if before == after {
		t.Fatalf("FinalPath did not change after re-pointing the junction: %q", after)
	}
	wantSecond, err := FinalPath(second)
	if err != nil {
		t.Fatal(err)
	}
	if after != wantSecond {
		t.Fatalf("FinalPath(%s) after re-point = %q, want %q", link, after, wantSecond)
	}
}

// A short (8.3) path and its long spelling must yield the same final path: the
// OS normalizes the name, so a caller cannot smuggle in a different spelling.
func TestFinalPathShortAndLongNamesAgree(t *testing.T) {
	base := t.TempDir()
	deep := mustMkdir(t, filepath.Join(base, "a-rather-long-directory-name", "child"))

	long, err := FinalPath(deep)
	if err != nil {
		t.Fatalf("FinalPath(%s): %v", deep, err)
	}

	shortBase := shortPath(t, base)
	short := filepath.Join(shortBase, "a-rather-long-directory-name", "child")
	// When 8.3 generation is disabled the short spelling is identical to the
	// long one; the comparison below still has to hold.
	got, err := FinalPath(short)
	if err != nil {
		t.Fatalf("FinalPath(%s): %v", short, err)
	}
	if got != long {
		t.Fatalf("short spelling %q -> %q, long spelling -> %q (must agree)", short, got, long)
	}
	if strings.Contains(got, "~") {
		t.Fatalf("final path must use long names, got %q", got)
	}
}

// shortPath returns the 8.3 spelling of path when the volume has one, else path.
func shortPath(t *testing.T, path string) string {
	t.Helper()
	short, err := shortPathName(path)
	if err != nil {
		t.Logf("no 8.3 short name for %s (%v); comparing the long spelling with itself", path, err)
		return path
	}
	return short
}

func TestFinalPathMissingPathFailsClosed(t *testing.T) {
	base := t.TempDir()
	for _, p := range []string{
		filepath.Join(base, "does-not-exist"),
		filepath.Join(base, "does-not-exist", "deeper"),
		"",
		"   ",
	} {
		if got, err := FinalPath(p); err == nil {
			t.Fatalf("FinalPath(%q) = %q, want an error for a non-existent path", p, got)
		}
	}
}

func TestFinalPathPlainFileResolves(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FinalPath(file)
	if err != nil {
		t.Fatalf("FinalPath(%s): %v", file, err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("FinalPath(%s) = %q, want an absolute path", file, got)
	}
	info, err := os.Stat(got)
	if err != nil || info.IsDir() {
		t.Fatalf("FinalPath(%s) = %q is not the same regular file: %v", file, got, err)
	}
	if filepath.Base(got) != "file.txt" {
		t.Fatalf("FinalPath(%s) = %q, want it to end in file.txt", file, got)
	}
}

// --- allow-list escape via a junction (I-48) --------------------------------

// TestJunctionEscapeRemainsDenied is the named regression test for I-48.
//
// Measured before the fix: Go 1.26 on Windows leaves a live junction path
// unchanged in filepath.EvalSymlinks, so `allowed\j -> outside` passed the
// allow-list check as if it were `allowed\j`, and both the canonical root and
// every read/write behind it landed outside the allow-list. The test asserts the
// whole chain: canonicalization refuses the link, the service refuses to resolve
// or read through it, and the real target stays outside.
func TestJunctionEscapeRemainsDenied(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "j")
	mustLinkDir(t, link, outside)

	svc := NewFSService(nil)
	svc.SetAllowedRoots([]string{allowed})

	if _, err := svc.Resolve(link, ""); err == nil {
		t.Fatalf("Resolve on the escaping junction root %s must be denied", link)
	} else if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Resolve error = %v, want the not-allowed rejection", err)
	}
	if _, err := svc.Read(context.Background(), &ReadRequest{Root: link, Path: "secret.txt"}); err == nil {
		t.Fatalf("Read through the escaping junction root %s must be denied", link)
	}
	if _, err := svc.List(context.Background(), &ListRequest{Root: link}); err == nil {
		t.Fatalf("List through the escaping junction root %s must be denied", link)
	}
	// An allowed root is resolved to its final path too, so configuring the
	// junction as an allowed root is the operator explicitly naming the target
	// directory: the target becomes allowed, while a root candidate that is
	// *not* under the configured entry is still refused. (This is the same rule
	// as TestEnsureRootAllowedAllowedRootSpelledAsJunction; it is not a hole,
	// because the operator supplied the path.)
	svc.SetAllowedRoots([]string{link})
	if _, err := svc.Resolve(outside, ""); err != nil {
		t.Fatalf("an explicitly configured junction root must allow its target: %v", err)
	}
	if _, err := svc.Resolve(t.TempDir(), ""); err == nil {
		t.Fatal("an unrelated root must still be refused")
	}
	// The secret is still there - nothing was read, moved or deleted by the
	// denied calls.
	data, err := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if err != nil || string(data) != "SECRET" {
		t.Fatalf("the outside file must be untouched: %q, %v", data, err)
	}
}

// A junction inside an allowed root that points outside it must not be accepted
// as a workspace root: the OS would route every read/write to the outside
// directory while the path still looks allow-listed.
func TestEnsureRootAllowedRejectsJunctionEscape(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "j")
	mustLinkDir(t, link, outside)

	svc := NewFSService(nil)
	svc.SetAllowedRoots([]string{allowed})

	if _, err := svc.Read(context.Background(), &ReadRequest{Root: link, Path: "secret.txt"}); err == nil {
		t.Fatalf("FSService.Read through junction root %s (-> %s) must be rejected", link, outside)
	} else if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("read error = %v, want the not-allowed rejection", err)
	}
	if _, err := svc.Resolve(link, "secret.txt"); err == nil {
		t.Fatalf("FSService.Resolve through junction root %s must be rejected", link)
	}
	if _, err := svc.Write(context.Background(), &WriteRequest{Root: link, Path: "planted.txt", Content: []byte("x")}); err == nil {
		t.Fatalf("FSService.Write through junction root %s must be rejected", link)
	}
}

// A junction inside the allow-list that points at another directory inside the
// allow-list is legitimate: it must be accepted, and the resolved root must be
// the real target path, not the link.
func TestEnsureRootAllowedAcceptsInternalJunction(t *testing.T) {
	allowed := t.TempDir()
	target := mustMkdir(t, filepath.Join(allowed, "real"))
	if err := os.WriteFile(filepath.Join(target, "file.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "link")
	mustLinkDir(t, link, target)

	svc := NewFSService(nil)
	svc.SetAllowedRoots([]string{allowed})

	resolved, err := svc.Resolve(link, "file.txt")
	if err != nil {
		t.Fatalf("an internal junction must be allowed: %v", err)
	}
	wantFinal, err := FinalPath(target)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != filepath.Join(wantFinal, "file.txt") {
		t.Fatalf("Resolve through an internal junction = %q, want %q", resolved, filepath.Join(wantFinal, "file.txt"))
	}
	fc, err := svc.Read(context.Background(), &ReadRequest{Root: link, Path: "file.txt"})
	if err != nil {
		t.Fatalf("Read through an internal junction: %v", err)
	}
	if string(fc.Content) != "ok" {
		t.Fatalf("content = %q, want %q", fc.Content, "ok")
	}
}

// A junction that escapes is rejected even when it is not the root itself: the
// allow-list is decided on the final path of the candidate root.
func TestEnsureRootAllowedRejectsJunctionEscapeViaAncestor(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	mustMkdir(t, filepath.Join(outside, "work"))
	link := filepath.Join(allowed, "hop")
	mustLinkDir(t, link, outside)

	svc := NewFSService(nil)
	svc.SetAllowedRoots([]string{allowed})

	if _, err := svc.Resolve(filepath.Join(link, "work"), ""); err == nil {
		t.Fatalf("a root reached through an escaping junction must be rejected")
	}
}

// Allowed roots themselves may be spelled through a junction: the allow-list is
// compared on final paths, so a configured root that is a link to the real tree
// still allows the real tree.
func TestEnsureRootAllowedAllowedRootSpelledAsJunction(t *testing.T) {
	base := t.TempDir()
	real := mustMkdir(t, filepath.Join(base, "real"))
	link := filepath.Join(base, "link")
	mustLinkDir(t, link, real)
	if err := os.WriteFile(filepath.Join(real, "f.txt"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewFSService(nil)
	svc.SetAllowedRoots([]string{link})

	// Reached through the real path, which the configured link points at.
	if _, err := svc.Read(context.Background(), &ReadRequest{Root: real, Path: "f.txt"}); err != nil {
		t.Fatalf("the real path behind an allowed junction root must be allowed: %v", err)
	}
	// Reached through the link itself.
	if _, err := svc.Read(context.Background(), &ReadRequest{Root: link, Path: "f.txt"}); err != nil {
		t.Fatalf("the configured junction root must be allowed: %v", err)
	}
	// An unrelated directory is still rejected.
	if _, err := svc.Read(context.Background(), &ReadRequest{Root: t.TempDir(), Path: "f.txt"}); err == nil {
		t.Fatal("an unrelated root must still be rejected")
	}
}

// A junction *inside* a workspace root (pnpm fills node_modules with them on
// Windows) is followed only when its final path stays inside that same root.
// Resolve returns the final path, so the caller's later read/write does not go
// through the junction again. A junction to a sibling directory is still an
// escape even when that sibling is itself inside the allow-list: containment is
// decided against this root, not against the allow-list.
func TestResolveInternalJunctionStaysInsideRoot(t *testing.T) {
	allowed := t.TempDir()
	root := mustMkdir(t, filepath.Join(allowed, "project"))
	store := mustMkdir(t, filepath.Join(root, "node_modules", ".pnpm", "pkg"))
	if err := os.WriteFile(filepath.Join(store, "index.js"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustLinkDir(t, filepath.Join(root, "node_modules", "pkg"), store)
	sibling := mustMkdir(t, filepath.Join(allowed, "other"))
	if err := os.WriteFile(filepath.Join(sibling, "secret.txt"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustLinkDir(t, filepath.Join(root, "side"), sibling)

	svc := NewFSService(nil)
	svc.SetAllowedRoots([]string{allowed})

	got, err := svc.Resolve(root, "node_modules/pkg/index.js")
	if err != nil {
		t.Fatalf("a junction that stays inside the root must resolve: %v", err)
	}
	storeFinal, err := FinalPath(store)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(storeFinal, "index.js"); got != want {
		t.Fatalf("Resolve = %q, want the final path %q (not a path through the junction)", got, want)
	}
	fc, err := svc.Read(context.Background(), &ReadRequest{Root: root, Path: "node_modules/pkg/index.js"})
	if err != nil || string(fc.Content) != "ok" {
		t.Fatalf("Read through an internal junction = (%v, %v), want \"ok\"", fc, err)
	}

	// The junction as the last component is the case only the reparse-point
	// containment check guards: there is no later component to re-check.
	if got, err := svc.Resolve(root, "side"); err == nil {
		t.Fatalf("Resolve of a sibling junction itself = %q, must be rejected", got)
	}
	if _, err := svc.Resolve(root, "side/secret.txt"); err == nil {
		t.Fatal("a junction to an allow-listed sibling outside this root must be rejected")
	} else if !strings.Contains(err.Error(), "escapes project root") {
		t.Fatalf("sibling junction error = %v, want the escapes-project-root rejection", err)
	}
	if _, err := svc.Read(context.Background(), &ReadRequest{Root: root, Path: "side/secret.txt"}); err == nil {
		t.Fatal("Read through a sibling junction must be rejected")
	}
}
