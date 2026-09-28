package merge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/git"
	"github.com/codeflow/backend/internal/runworkspace"
)

// recordingBlobs is a BlobWriter that stores content in memory and remembers
// every object it was handed, so a test can prove that the bytes which reached
// the store are the bytes on disk, and that the hash the store returned is the
// hash the candidate publishes.
type recordingBlobs struct {
	mu       sync.Mutex
	contents map[string]string
	puts     int
	// failOn makes the store reject a specific content hash.
	failOn func(hash string) error
	// wrongHash makes the store answer with a hash of content it did not
	// receive, which is the failure mode the manifest check must catch.
	wrongHash bool
}

func newRecordingBlobs() *recordingBlobs {
	return &recordingBlobs{contents: map[string]string{}}
}

func (b *recordingBlobs) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	// The store must hold exactly the bytes it was handed: no trimming, no
	// newline normalization. A reader that came with extra bytes would show up
	// here and in the manifest comparison.
	data, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failOn != nil {
		if err := b.failOn(hash); err != nil {
			return "", 0, err
		}
	}
	b.puts++
	b.contents[hash] = string(data)
	if b.wrongHash {
		// Only the hash is wrong; the size is right, so a check that compares
		// sizes alone would not notice.
		return "sha256:" + strings.Repeat("f", 64), int64(len(data)), nil
	}
	return hash, int64(len(data)), nil
}

func (b *recordingBlobs) content(hash string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.contents[hash]
	return c, ok
}

func (b *recordingBlobs) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.puts
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func removeTestFile(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("remove %s: %v", rel, err)
	}
}

func capturePlain(t *testing.T, root string) *runworkspace.Manifest {
	t.Helper()
	m, err := runworkspace.Capture(context.Background(), nil, root, runworkspace.CaptureOptions{})
	if err != nil {
		t.Fatalf("capture %s: %v", root, err)
	}
	if err := m.Verify(); err != nil {
		t.Fatalf("capture %s produced a manifest that does not verify: %v", root, err)
	}
	return m
}

// captureGitManifest captures a git work tree. The caller installs the process
// policy, exactly as the runworkspace tests do.
func captureGitManifest(t *testing.T, gm *git.GitManager, root string) *runworkspace.Manifest {
	t.Helper()
	m, err := runworkspace.Capture(context.Background(), gm, root, runworkspace.CaptureOptions{})
	if err != nil {
		t.Fatalf("git capture %s: %v", root, err)
	}
	if err := m.Verify(); err != nil {
		t.Fatalf("git capture %s produced a manifest that does not verify: %v", root, err)
	}
	return m
}

// materialize makes the working copy the run would edit.
func materialize(t *testing.T, base *runworkspace.Manifest) *runworkspace.WorkingCopy {
	t.Helper()
	wc, err := runworkspace.Materialize(context.Background(), base, t.TempDir(), runworkspace.Ownership{
		RunID:         "run-1",
		AttemptID:     "attempt-1",
		OwnerInstance: "instance-1",
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	return wc
}

// findOperation returns the operation for path.
func findOperation(t *testing.T, c *Candidate, path string) Operation {
	t.Helper()
	for _, op := range c.Operations {
		if op.Path == path {
			return op
		}
	}
	t.Fatalf("no operation for %s in %+v", path, c.Operations)
	return Operation{}
}

// noOperationFor fails when the candidate mentions path at all: no operation,
// no conflict, and no already-applied mark.
func noOperationFor(t *testing.T, c *Candidate, path string) {
	t.Helper()
	for _, op := range c.Operations {
		if op.Path == path {
			t.Fatalf("unexpected operation for %s: %+v", path, op)
		}
	}
	for _, cf := range c.Conflicts {
		if cf.Path == path {
			t.Fatalf("unexpected conflict for %s: %+v", path, cf)
		}
	}
	for _, p := range c.AlreadyApplied {
		if p == path {
			t.Fatalf("unexpected already-applied mark for %s", path)
		}
	}
}

func findConflict(t *testing.T, c *Candidate, path string) Conflict {
	t.Helper()
	for _, cf := range c.Conflicts {
		if cf.Path == path {
			return cf
		}
	}
	t.Fatalf("no conflict for %s in %+v", path, c.Conflicts)
	return Conflict{}
}

// hashOf is the manifest content-hash form of a byte slice, used by tests that
// check a candidate's expectations against bytes on disk.
func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func entryHash(t *testing.T, m *runworkspace.Manifest, path string) string {
	t.Helper()
	for _, e := range m.Entries {
		if e.Path == path {
			return e.ContentHash
		}
	}
	t.Fatalf("no entry for %s in manifest", path)
	return ""
}

func exclusionReason(t *testing.T, m *runworkspace.Manifest, path string) string {
	t.Helper()
	for _, x := range m.Excluded {
		if x.Path == path {
			return x.Reason
		}
	}
	t.Fatalf("no exclusion for %s in manifest", path)
	return ""
}

// treeFingerprint reads every file under root and records its bytes and
// modification time. Prepare must leave both trees byte-identical and
// untouched: it is a computation about a publish, not a publish.
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
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = fmt.Sprintf("sha256=%s size=%d mtime=%s mode=%s",
			hex.EncodeToString(sum[:]), len(data), info.ModTime().UTC().Format(time.RFC3339Nano), info.Mode())
		return nil
	})
	if err != nil {
		t.Fatalf("fingerprint %s: %v", root, err)
	}
	return out
}

func assertSameFingerprint(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		if before[name] != after[name] {
			t.Fatalf("%s changed at %s:\n before %s\n after  %s", what, name, before[name], after[name])
		}
	}
}

// --- git fixture helpers, mirroring the runworkspace ones ---

func requireGitCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git CLI is required for this test but was not found on PATH: %v", err)
	}
}

func newGitFixtureRepo(t *testing.T) (string, *git.GitManager) {
	t.Helper()
	requireGitCLI(t)
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@test.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "config", "core.autocrlf", "false")
	return root, git.NewGitManager(root)
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v (output: %s)", strings.Join(args, " "), err, out)
	}
	return string(out)
}
