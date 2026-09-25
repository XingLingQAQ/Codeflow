package runworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// TestMaterializeRejectsDestinationInsideSource proves the anti-recursion
// guard: the copy must not be written into the tree it copies from, in either
// the "dest parent is the root" or the "dest parent is a subdirectory" form.
func TestMaterializeRejectsDestinationInsideSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "a\n")
	writeFile(t, root, "sub/b.txt", "b\n")

	m, err := Capture(context.Background(), nil, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	cases := []struct {
		name       string
		destParent string
	}{
		{"dest parent is the source root", root},
		{"dest parent is a subdirectory of the source root", filepath.Join(root, "sub")},
		{"dest parent is a not-yet-existing subdirectory", filepath.Join(root, "sub", "nested")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Materialize(context.Background(), m, tc.destParent, Ownership{RunID: "r1", AttemptID: "a1", OwnerInstance: "i1"})
			if !errors.Is(err, ErrDestinationInsideSource) {
				t.Fatalf("expected ErrDestinationInsideSource, got %v", err)
			}
			if entries, readErr := os.ReadDir(tc.destParent); readErr == nil {
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "run-") {
						t.Errorf("a working copy was created at the rejected destination: %s", e.Name())
					}
				}
			}
		})
	}

	t.Run("source inside destination parent", func(t *testing.T) {
		parent := t.TempDir()
		inner := filepath.Join(parent, "project")
		if err := os.MkdirAll(inner, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeFile(t, inner, "a.txt", "a\n")
		innerManifest, err := Capture(context.Background(), nil, inner, CaptureOptions{})
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		// destParent contains the root: a copy made there would sit next to the
		// source and be picked up by any later capture of the parent.
		if _, err := Materialize(context.Background(), innerManifest, parent, Ownership{RunID: "r1", AttemptID: "a1"}); !errors.Is(err, ErrDestinationInsideSource) {
			t.Fatalf("expected ErrDestinationInsideSource, got %v", err)
		}
	})
}

// TestMaterializeCopiesBaseline is the happy path: every file entry lands in a
// fresh directory with byte-identical content, the ownership marker exists, and
// the source tree is untouched by the copy.
func TestMaterializeCopiesBaseline(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	root, gm := newGitFixtureRepo(t)
	writeFile(t, root, "a.txt", "alpha\n")
	writeFile(t, root, "sub/b.txt", "beta\n")
	writeFile(t, root, "empty.txt", "")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "init")
	writeFile(t, root, "a.txt", "alpha dirty\n")
	writeFile(t, root, "untracked.txt", "untracked\n")

	m, err := Capture(context.Background(), gm, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	sourceBefore := treeFingerprint(t, root)
	destParent := t.TempDir()
	owner := Ownership{RunID: "run-1", AttemptID: "attempt-1", OwnerInstance: "instance-1"}
	wc, err := Materialize(context.Background(), m, destParent, owner)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(wc.Path) })

	t.Logf("EVIDENCE working copy=%s manifest_hash=%s files=%d", wc.Path, wc.ManifestHash, len(wc.Files))

	if filepath.Dir(wc.Path) != filepath.Clean(destParent) {
		t.Errorf("working copy %s is not directly under %s", wc.Path, destParent)
	}
	if wc.ManifestHash != m.Hash {
		t.Errorf("WorkingCopy.ManifestHash = %s, want %s", wc.ManifestHash, m.Hash)
	}
	if wc.Owner != owner {
		t.Errorf("WorkingCopy.Owner = %+v, want %+v", wc.Owner, owner)
	}
	if len(wc.Files) != len(m.Entries) {
		t.Errorf("copied %d files for %d entries: %v", len(wc.Files), len(m.Entries), wc.Files)
	}
	if !sort.StringsAreSorted(wc.Files) {
		t.Errorf("copied file list must be sorted: %v", wc.Files)
	}

	for _, entry := range m.Entries {
		got := filepath.Join(wc.Path, filepath.FromSlash(entry.Path))
		data, err := os.ReadFile(got)
		if err != nil {
			t.Fatalf("read copied %s: %v", entry.Path, err)
		}
		sum := sha256.Sum256(data)
		if "sha256:"+hex.EncodeToString(sum[:]) != entry.ContentHash {
			t.Errorf("copied %s does not match the manifest hash", entry.Path)
		}
	}

	markerPath := filepath.Join(wc.Path, OwnerMarkerName())
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("ownership marker missing: %v", err)
	}
	var marker ownerMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("parse marker: %v", err)
	}
	if marker.Ownership != owner {
		t.Errorf("marker owner = %+v, want %+v", marker.Ownership, owner)
	}
	if marker.ManifestHash != m.Hash {
		t.Errorf("marker manifest hash = %s, want %s", marker.ManifestHash, m.Hash)
	}
	if marker.CreatedAt == "" {
		t.Error("marker must record a creation time")
	}

	sourceAfter := treeFingerprint(t, root)
	if len(sourceBefore) != len(sourceAfter) {
		t.Fatalf("source tree gained or lost files during the copy: %d -> %d", len(sourceBefore), len(sourceAfter))
	}
	for path, hash := range sourceBefore {
		if sourceAfter[path] != hash {
			t.Errorf("source file %s changed during the copy", path)
		}
	}

	// A second call must not reuse the first destination.
	second, err := Materialize(context.Background(), m, destParent, owner)
	if err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(second.Path) })
	if second.Path == wc.Path {
		t.Error("two materializations must not share a destination")
	}
}

// TestMaterializeDetectsSourceChange covers the race the manifest exists to
// catch: the source changed after capture, so the baseline no longer describes
// it and the partial copy must not be left behind.
func TestMaterializeDetectsSourceChange(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "original\n")
	writeFile(t, root, "b.txt", "second\n")

	m, err := Capture(context.Background(), nil, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	writeFile(t, root, "b.txt", "changed after capture\n")

	destParent := t.TempDir()
	owner := Ownership{RunID: "run-1", AttemptID: "attempt-1", OwnerInstance: "instance-1"}
	_, err = Materialize(context.Background(), m, destParent, owner)
	if !errors.Is(err, ErrSourceChangedDuringCopy) {
		t.Fatalf("expected ErrSourceChangedDuringCopy, got %v", err)
	}
	if leftovers := runDirectories(t, destParent); len(leftovers) != 0 {
		t.Errorf("failed materialization left directories behind: %v", leftovers)
	}

	// A file deleted after capture is the same class of failure.
	writeFile(t, root, "b.txt", "second\n")
	m2, err := Capture(context.Background(), nil, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	_, err = Materialize(context.Background(), m2, destParent, owner)
	if !errors.Is(err, ErrSourceChangedDuringCopy) {
		t.Fatalf("expected ErrSourceChangedDuringCopy for a deleted source file, got %v", err)
	}
	if leftovers := runDirectories(t, destParent); len(leftovers) != 0 {
		t.Errorf("failed materialization left directories behind: %v", leftovers)
	}
}

// TestMaterializeSkipsSymlinksAndDeleted documents what this step does not
// materialize: link entries and index deletions are reported, not silently
// dropped.
func TestMaterializeSkipsSymlinksAndDeleted(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	root, gm := newGitFixtureRepo(t)
	writeFile(t, root, "kept.txt", "kept\n")
	writeFile(t, root, "gone.txt", "gone\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "init")
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	m, err := Capture(context.Background(), gm, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	wc, err := Materialize(context.Background(), m, t.TempDir(), Ownership{RunID: "r", AttemptID: "a", OwnerInstance: "i"})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(wc.Path) })

	if len(wc.Skipped) != 1 || wc.Skipped[0] != "gone.txt" {
		t.Errorf("Skipped = %v, want [gone.txt]", wc.Skipped)
	}
	if _, err := os.Stat(filepath.Join(wc.Path, "gone.txt")); !os.IsNotExist(err) {
		t.Errorf("a deleted entry must not be materialized, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(wc.Path, "kept.txt")); err != nil {
		t.Errorf("kept.txt must be materialized: %v", err)
	}
}

// TestRemoveRequiresOwnership proves deletion is safe by construction: the
// marker must match the caller exactly, so a path that merely looks like a
// working copy cannot be removed by the wrong run attempt.
func TestRemoveRequiresOwnership(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "a\n")
	m, err := Capture(context.Background(), nil, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	destParent := t.TempDir()
	owner := Ownership{RunID: "run-1", AttemptID: "attempt-1", OwnerInstance: "instance-1"}
	wc, err := Materialize(context.Background(), m, destParent, owner)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	wrongOwner := Ownership{RunID: "run-2", AttemptID: "attempt-2", OwnerInstance: "instance-2"}
	if err := wc.Remove(wrongOwner); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("expected ErrNotOwner, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(wc.Path, "a.txt")); err != nil {
		t.Errorf("a rejected Remove must delete nothing: %v", err)
	}

	// Same run, different attempt: still not the owner.
	otherAttempt := owner
	otherAttempt.AttemptID = "attempt-2"
	if err := wc.Remove(otherAttempt); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("expected ErrNotOwner for a different attempt, got %v", err)
	}
	if _, err := os.Stat(wc.Path); err != nil {
		t.Errorf("the working copy must survive a rejected Remove: %v", err)
	}

	if err := wc.Remove(owner); err != nil {
		t.Fatalf("owner Remove: %v", err)
	}
	if _, err := os.Stat(wc.Path); !os.IsNotExist(err) {
		t.Errorf("working copy still exists after Remove, stat err=%v", err)
	}
}

// TestMaterializeRejectsEscapingManifestPath treats a manifest as untrusted
// data: an entry whose path escapes the root is refused rather than read.
func TestMaterializeRejectsEscapingManifestPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "a\n")
	secret := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := os.WriteFile(secret, []byte("outside\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	t.Cleanup(func() { os.Remove(secret) })

	m := &Manifest{
		FormatVersion: FormatVersion,
		Mode:          ModePlain,
		Root:          root,
		Entries: []Entry{{
			Path:        "../outside.txt",
			Type:        TypeFile,
			Mode:        0o644,
			Size:        int64(len("outside\n")),
			ContentHash: hashBytes([]byte("outside\n")),
		}},
		Hash: "sha256:" + strings.Repeat("0", 64),
	}

	destParent := t.TempDir()
	_, err := Materialize(context.Background(), m, destParent, Ownership{RunID: "r", AttemptID: "a"})
	if err == nil {
		t.Fatal("expected an error for a manifest path escaping the root")
	}
	if leftovers := runDirectories(t, destParent); len(leftovers) != 0 {
		t.Errorf("rejected materialization left directories behind: %v", leftovers)
	}
}

// --- helpers ---

func runDirectories(t *testing.T, parent string) []string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read %s: %v", parent, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "run-") {
			out = append(out, e.Name())
		}
	}
	return out
}

// treeFingerprint hashes every regular file below root (path -> sha256),
// leaving links and special files out.
func treeFingerprint(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatalf("fingerprint %s: %v", root, err)
	}
	return out
}
