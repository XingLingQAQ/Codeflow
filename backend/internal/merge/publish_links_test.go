package merge

// Tests added by the main agent's acceptance review of T1.09.b group 3: a
// directory of the target that is replaced by a link (a Windows junction, a
// symbolic link elsewhere) after the journal was planned must never redirect a
// write, a delete or a restore outside the target root. The publisher checked
// the root's identity and lstat'ed the last path component only, so every
// intermediate component was followed: measured on Go 1.26 / Windows, a file
// written through a junction lands in the junction's target.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeDirLink creates link as a directory link to target: a junction on
// Windows (no privilege needed), a symbolic link elsewhere. The link is removed
// before the temporary directories are, so cleanup never walks through it.
func makeDirLink(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J %s %s: %v: %s", link, target, err, out)
		}
	} else if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

// swapDirForLink moves target/dir aside (the user's real directory stays, under
// another name) and puts a link to outside in its place.
func swapDirForLink(t *testing.T, target, dir, outside string) error {
	t.Helper()
	real := filepath.Join(target, dir)
	if err := os.Rename(real, real+"-moved"); err != nil {
		return err
	}
	makeDirLink(t, real, outside)
	return nil
}

// TestPublishRefusesALinkSwappedIntoTheTarget: sub/ is replaced by a link to a
// directory outside the root right before the file below it is written. The
// outside directory holds exactly what the target's sub/ held, so a write, a
// delete or a precondition that followed the link would find what it expects
// there. Nothing outside may change, and the operation parks in needs_recovery
// (a link is now inside the user's tree; a human decides).
func TestPublishRefusesALinkSwappedIntoTheTarget(t *testing.T) {
	cases := []struct {
		name         string
		base, result map[string]string
		path         string
	}{
		{"create below a swapped directory",
			map[string]string{"sub/keep.txt": "keep"},
			map[string]string{"sub/keep.txt": "keep", "sub/new.txt": "brand new"},
			"sub/new.txt"},
		{"modify below a swapped directory",
			map[string]string{"sub/file.txt": "old"},
			map[string]string{"sub/file.txt": "new"},
			"sub/file.txt"},
		{"delete below a swapped directory",
			map[string]string{"sub/gone.txt": "bye", "keep.txt": "k"},
			map[string]string{"keep.txt": "k"},
			"sub/gone.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublishFixture(t, tc.base, tc.result, nil)
			outside := t.TempDir()
			for rel, content := range tc.base {
				if rest, ok := strings.CutPrefix(rel, "sub/"); ok {
					writeTestFile(t, outside, rest, content)
				}
			}
			outsideBefore := snapshotTree(t, outside)
			realBefore := snapshotTree(t, filepath.Join(f.target, "sub"))

			in := f.input("op-link")
			swapped := false
			in.Hook = func(s Step) error {
				if s.Stage == StageBeforeReplace && s.Path == tc.path && !swapped {
					swapped = true
					return swapDirForLink(t, f.target, "sub", outside)
				}
				return nil
			}
			res, err := f.publish(t, in)
			if !swapped {
				t.Fatalf("the hook never reached %s", tc.path)
			}
			if err == nil {
				t.Fatalf("Publish wrote through a link and reported success (result %+v)", res)
			}
			if !errors.Is(err, ErrNeedsRecovery) {
				t.Fatalf("error = %v, want ErrNeedsRecovery", err)
			}
			assertTreeEquals(t, "the directory outside the root", outsideBefore, snapshotTree(t, outside))
			assertTreeEquals(t, "the user's real sub/ (moved aside)", realBefore, snapshotTree(t, filepath.Join(f.target, "sub-moved")))
			if res.Operation.Status != StatusNeedsRecovery {
				t.Fatalf("operation status = %s, want needs_recovery", res.Operation.Status)
			}
		})
	}
}

// TestPublishRollbackDoesNotRestoreThroughALink: sub/a.txt was written, then
// sub/ is replaced by a link to a directory that holds exactly the new content,
// and the next file fails. A rollback that followed the link would find "its
// own write" there and restore the old content into the outside directory.
func TestPublishRollbackDoesNotRestoreThroughALink(t *testing.T) {
	f := newPublishFixture(t,
		map[string]string{"sub/a.txt": "old a", "z.txt": "old z"},
		map[string]string{"sub/a.txt": "new a", "z.txt": "new z"},
		nil)
	outside := t.TempDir()
	writeTestFile(t, outside, "a.txt", "new a")
	outsideBefore := snapshotTree(t, outside)

	in := f.input("op-link-rollback")
	swapped := false
	in.Hook = func(s Step) error {
		if s.Stage == StageBeforeReplace && s.Path == "z.txt" && !swapped {
			swapped = true
			if err := swapDirForLink(t, f.target, "sub", outside); err != nil {
				return err
			}
			return errors.New("injected failure after sub/ became a link")
		}
		return nil
	}
	res, err := f.publish(t, in)
	if !swapped {
		t.Fatal("the hook never reached z.txt")
	}
	if !errors.Is(err, ErrNeedsRecovery) {
		t.Fatalf("error = %v, want ErrNeedsRecovery", err)
	}
	assertTreeEquals(t, "the directory outside the root", outsideBefore, snapshotTree(t, outside))
	if got, ok := targetFile(filepath.Join(f.target, "sub-moved"), "a.txt"); !ok || got != "new a" {
		t.Fatalf("the user's real sub/a.txt = %q (present %v), want the written new content left for a human", got, ok)
	}
	if res.Operation.Status != StatusNeedsRecovery {
		t.Fatalf("operation status = %s, want needs_recovery", res.Operation.Status)
	}
}
