package runworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/git"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// TestCaptureDirtyUntrackedBaseline is the core acceptance case: the baseline
// is the workspace as it is on disk, dirty edits and untracked files included,
// ignored and secret files excluded with a stated reason.
func TestCaptureDirtyUntrackedBaseline(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	root, gm := newGitFixtureRepo(t)

	writeFile(t, root, "a.txt", "A v1\n")
	writeFile(t, root, "b.txt", "B v1\n")
	writeFile(t, root, ".gitignore", "d.txt\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "init")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	writeFile(t, root, "a.txt", "A v2 dirty\n")            // unstaged edit
	writeFile(t, root, "b.txt", "B v2 staged\n")           // staged edit
	runGit(t, root, "add", "b.txt")                        //
	writeFile(t, root, "c.txt", "C untracked\n")           // untracked
	writeFile(t, root, "d.txt", "D ignored\n")             // ignored
	writeFile(t, root, ".env", "TOKEN=super-secret\n")     // secret
	writeFile(t, root, ".env.example", "TOKEN=changeme\n") // template, kept

	m, err := Capture(context.Background(), gm, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	if m.FormatVersion != 1 {
		t.Errorf("FormatVersion = %d, want 1", m.FormatVersion)
	}
	if m.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", m.Mode, ModeGit)
	}
	if m.Root != filepath.Clean(root) {
		t.Errorf("Root = %q, want %q", m.Root, filepath.Clean(root))
	}
	if m.HeadCommit == nil || *m.HeadCommit != head {
		t.Errorf("HeadCommit = %v, want %s", m.HeadCommit, head)
	}
	if !strings.HasPrefix(m.Hash, "sha256:") || len(m.Hash) != len("sha256:")+64 {
		t.Errorf("Hash = %q, want sha256:<64 hex>", m.Hash)
	}
	if len(m.Rules) == 0 || !strings.Contains(strings.Join(m.Rules, "\n"), "exclude-standard") {
		t.Errorf("Rules should document the gitignore rule, got %v", m.Rules)
	}

	a, ok := lookupEntry(m, "a.txt")
	if !ok {
		t.Fatalf("a.txt missing from entries: %+v", m.Entries)
	}
	if a.Type != TypeFile {
		t.Errorf("a.txt Type = %q, want %q", a.Type, TypeFile)
	}
	if a.ContentHash != hashBytes([]byte("A v2 dirty\n")) {
		t.Errorf("a.txt ContentHash = %s, want the dirty on-disk content", a.ContentHash)
	}
	if a.Size != int64(len("A v2 dirty\n")) {
		t.Errorf("a.txt Size = %d, want %d", a.Size, len("A v2 dirty\n"))
	}
	if a.GitStatus != " M" {
		t.Errorf("a.txt GitStatus = %q, want %q", a.GitStatus, " M")
	}
	if a.Mode == 0 {
		t.Error("a.txt Mode should keep the permission bits")
	}

	b, ok := lookupEntry(m, "b.txt")
	if !ok {
		t.Fatalf("b.txt missing from entries")
	}
	if b.ContentHash != hashBytes([]byte("B v2 staged\n")) || b.GitStatus != "M " {
		t.Errorf("b.txt = %+v, want staged content with status \"M \"", b)
	}

	c, ok := lookupEntry(m, "c.txt")
	if !ok {
		t.Fatalf("untracked c.txt missing from entries")
	}
	if c.GitStatus != "??" || c.ContentHash != hashBytes([]byte("C untracked\n")) {
		t.Errorf("c.txt = %+v, want untracked with status \"??\"", c)
	}

	if _, ok := lookupEntry(m, "d.txt"); ok {
		t.Error("git-ignored d.txt must not be captured")
	}
	if _, ok := lookupReason(m, "d.txt"); ok {
		t.Error("ignored paths are not enumerated by git at all, so they carry no exclusion record")
	}

	if _, ok := lookupEntry(m, ".env"); ok {
		t.Error(".env must not be captured")
	}
	if reason, ok := lookupReason(m, ".env"); !ok || reason != ReasonSecret {
		t.Errorf(".env exclusion = %q (present=%v), want %q", reason, ok, ReasonSecret)
	}

	envExample, ok := lookupEntry(m, ".env.example")
	if !ok {
		t.Fatalf(".env.example must stay in the baseline: %+v", m.Excluded)
	}
	if envExample.ContentHash != hashBytes([]byte("TOKEN=changeme\n")) {
		t.Errorf(".env.example ContentHash = %s", envExample.ContentHash)
	}

	if !sort.SliceIsSorted(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path }) {
		t.Error("entries must be sorted by path")
	}

	t.Logf("EVIDENCE head=%s manifest_hash=%s entries=%d excluded=%+v", head, m.Hash, len(m.Entries), m.Excluded)
}

// TestCaptureDoesNotTouchIndexOrHead proves the capture is an observation:
// .git/index (bytes and mtime), .git/HEAD, and every ref file are identical
// afterwards, even with stale stat information that makes plain `git status`
// want to refresh the index.
func TestCaptureDoesNotTouchIndexOrHead(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	root, gm := newGitFixtureRepo(t)
	writeFile(t, root, "a.txt", "A\n")
	writeFile(t, root, "sub/b.txt", "B\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "init")

	// Content unchanged, mtime altered: the exact condition that makes
	// `git status` rewrite .git/index.
	staleStat(t, filepath.Join(root, "a.txt"))

	before := repoState(t, root)
	if _, err := Capture(context.Background(), gm, root, CaptureOptions{}); err != nil {
		t.Fatalf("capture: %v", err)
	}
	after := repoState(t, root)

	if len(before) == 0 {
		t.Fatal("repo state snapshot is empty; the test would prove nothing")
	}
	keys := make([]string, 0, len(before))
	for key := range before {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Logf("EVIDENCE unchanged %s = %s", key, before[key])
	}
	for key, want := range before {
		if got, ok := after[key]; !ok {
			t.Errorf("%s disappeared during capture", key)
		} else if got != want {
			t.Errorf("%s changed during capture: %s -> %s", key, want, got)
		}
	}
	for key := range after {
		if _, ok := before[key]; !ok {
			t.Errorf("%s appeared during capture", key)
		}
	}
}

// TestCaptureRequiresPolicy is the fail-closed evidence at the runworkspace
// boundary: with no evaluator installed the capture cannot start a git process.
func TestCaptureRequiresPolicy(t *testing.T) {
	requireGitCLI(t)
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })

	root := t.TempDir()
	gm := git.NewGitManager(root)
	if err := exec.Command("git", "-C", root, "init", "-q").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	_, err := Capture(context.Background(), gm, root, CaptureOptions{})
	var denied *policy.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected *policy.DeniedError, got %v", err)
	}
	if denied.Decision.Allowed {
		t.Fatalf("unexpected allowed decision: %+v", denied.Decision)
	}
}

// TestCaptureUnbornHead covers a repository with no commits: there is no HEAD
// to record, but the working tree is still a valid baseline.
func TestCaptureUnbornHead(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	root, gm := newGitFixtureRepo(t)
	writeFile(t, root, "u.txt", "unborn content\n")

	m, err := Capture(context.Background(), gm, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if m.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", m.Mode, ModeGit)
	}
	if m.HeadCommit != nil {
		t.Errorf("HeadCommit = %v, want nil for an unborn branch", *m.HeadCommit)
	}
	entry, ok := lookupEntry(m, "u.txt")
	if !ok {
		t.Fatalf("untracked u.txt missing from an unborn repository: %+v", m.Entries)
	}
	if entry.GitStatus != "??" || entry.ContentHash != hashBytes([]byte("unborn content\n")) {
		t.Errorf("u.txt = %+v", entry)
	}
}

// TestCaptureDeletedTrackedFile covers a path that is in the index but gone
// from disk: the baseline records the deletion instead of inventing content.
func TestCaptureDeletedTrackedFile(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	root, gm := newGitFixtureRepo(t)
	writeFile(t, root, "gone.txt", "to be deleted\n")
	writeFile(t, root, "kept.txt", "kept\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "init")
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	m, err := Capture(context.Background(), gm, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	entry, ok := lookupEntry(m, "gone.txt")
	if !ok {
		t.Fatalf("deleted tracked file missing from entries: %+v", m.Entries)
	}
	if entry.Type != TypeDeleted {
		t.Errorf("gone.txt Type = %q, want %q", entry.Type, TypeDeleted)
	}
	if entry.ContentHash != "" || entry.Size != 0 {
		t.Errorf("deleted entry must carry no content: %+v", entry)
	}
	if entry.GitStatus != " D" {
		t.Errorf("gone.txt GitStatus = %q, want %q", entry.GitStatus, " D")
	}
	if _, ok := lookupEntry(m, "kept.txt"); !ok {
		t.Error("kept.txt must still be captured")
	}
}

// TestCaptureSubdirectoryRootRejected pins the current contract: a root that
// is not the work tree top level is an error, not a silently mislabelled
// subtree baseline. Subdirectory binding is a later decision.
func TestCaptureSubdirectoryRootRejected(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	root, _ := newGitFixtureRepo(t)
	writeFile(t, root, "sub/inner.txt", "inner\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "init")

	sub := filepath.Join(root, "sub")
	_, err := Capture(context.Background(), git.NewGitManager(sub), sub, CaptureOptions{})
	if !errors.Is(err, ErrNotWorkTreeRoot) {
		t.Fatalf("expected ErrNotWorkTreeRoot, got %v", err)
	}
}

// TestCapturePlainMatchesJourneyManifest checks the plain (no git) mode against
// the checked-in journey fixture: the hashes must be the ones the fixture
// manifest publishes, byte for byte.
func TestCapturePlainMatchesJourneyManifest(t *testing.T) {
	fixture := filepath.Join("..", "api", "testdata", "journeys")
	copy := t.TempDir()
	copyTree(t, filepath.Join(fixture, "project-no-git"), copy)

	m, err := Capture(context.Background(), nil, copy, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture plain: %v", err)
	}
	if m.Mode != ModePlain {
		t.Errorf("Mode = %q, want %q", m.Mode, ModePlain)
	}
	if m.HeadCommit != nil {
		t.Errorf("HeadCommit = %v, want nil in plain mode", *m.HeadCommit)
	}
	if len(m.Excluded) != 0 {
		t.Errorf("project-no-git has nothing to exclude, got %+v", m.Excluded)
	}

	want := journeyFixtureHashes(t, fixture, "project-no-git")
	if len(want) != len(m.Entries) {
		t.Fatalf("captured %d entries, fixture manifest lists %d: %+v", len(m.Entries), len(want), m.Entries)
	}
	for _, entry := range m.Entries {
		sha, ok := want[entry.Path]
		if !ok {
			t.Errorf("captured %s which the fixture manifest does not list", entry.Path)
			continue
		}
		if got := ContentHashHex(entry.ContentHash); got != sha {
			t.Errorf("%s ContentHash = %s, fixture sha256 = %s", entry.Path, got, sha)
		}
		t.Logf("EVIDENCE fixture %s captured=%s manifest.json=%s MATCH", entry.Path, ContentHashHex(entry.ContentHash), sha)
	}
	t.Logf("EVIDENCE plain manifest_hash=%s", m.Hash)
}

// TestCaptureExclusions covers every exclusion reason the capture can emit.
func TestCaptureExclusions(t *testing.T) {
	if DefaultMaxFileBytes != 5<<20 {
		t.Errorf("DefaultMaxFileBytes = %d, want 5 MiB", DefaultMaxFileBytes)
	}

	t.Run("oversize", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "big.bin", strings.Repeat("x", 2048))
		writeFile(t, root, "small.txt", strings.Repeat("y", 1024))
		writeFile(t, root, "at-limit.txt", strings.Repeat("z", 1024))

		m, err := Capture(context.Background(), nil, root, CaptureOptions{MaxFileBytes: 1024})
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if _, ok := lookupEntry(m, "big.bin"); ok {
			t.Error("oversize file must not be captured")
		}
		if reason, ok := lookupReason(m, "big.bin"); !ok || reason != ReasonOversize {
			t.Errorf("big.bin exclusion = %q (present=%v), want %q", reason, ok, ReasonOversize)
		}
		if _, ok := lookupEntry(m, "small.txt"); !ok {
			t.Error("file under the limit must be captured")
		}
		atLimit, ok := lookupEntry(m, "at-limit.txt")
		if !ok || atLimit.Size != 1024 {
			t.Errorf("a file exactly at the limit is allowed: %+v (present=%v)", atLimit, ok)
		}
	})

	t.Run("secret names", func(t *testing.T) {
		root := t.TempDir()
		secrets := []string{".env", ".env.local", ".env.production", "cert.pem", "server.key", "bundle.p12", "app.pfx", "app.keystore", "id_rsa", "id_rsa.pub", "id_ecdsa", "id_ed25519", "nested/deep/.env"}
		keep := []string{".env.example", ".env.sample", ".env.template", "environment.txt", "keyboard.txt"}
		for _, name := range secrets {
			writeFile(t, root, name, "secret\n")
		}
		for _, name := range keep {
			writeFile(t, root, name, "not a secret\n")
		}

		m, err := Capture(context.Background(), nil, root, CaptureOptions{})
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		for _, name := range secrets {
			if _, ok := lookupEntry(m, name); ok {
				t.Errorf("%s must not be captured", name)
			}
			if reason, ok := lookupReason(m, name); !ok || reason != ReasonSecret {
				t.Errorf("%s exclusion = %q (present=%v), want %q", name, reason, ok, ReasonSecret)
			}
		}
		for _, name := range keep {
			if _, ok := lookupEntry(m, name); !ok {
				t.Errorf("%s must be captured", name)
			}
		}
		if !strings.Contains(strings.Join(m.Rules, "\n"), "id_ed25519*") {
			t.Errorf("Rules must state the secret rule, got %v", m.Rules)
		}
	})

	t.Run("dot env case insensitive", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".ENV", "secret\n")
		m, err := Capture(context.Background(), nil, root, CaptureOptions{})
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if reason, ok := lookupReason(m, ".ENV"); !ok || reason != ReasonSecret {
			t.Errorf(".ENV exclusion = %q (present=%v), want %q", reason, ok, ReasonSecret)
		}
	})

	t.Run("symlink outside root", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.txt")
		writeFile(t, t.TempDir(), "outside.txt", "outside\n")
		if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
			t.Skipf("symlink creation is not permitted in this environment (%v); the outside-root-link exclusion is unverified here", err)
		}
		m, err := Capture(context.Background(), nil, root, CaptureOptions{})
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if _, ok := lookupEntry(m, "escape"); ok {
			t.Error("link to a target outside the root must not be captured")
		}
		if reason, ok := lookupReason(m, "escape"); !ok || reason != ReasonOutsideRootLink {
			t.Errorf("escape exclusion = %q (present=%v), want %q", reason, ok, ReasonOutsideRootLink)
		}
	})

	t.Run("symlink inside root is recorded as a link", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "target.txt", "target\n")
		if err := os.Symlink(filepath.Join(root, "target.txt"), filepath.Join(root, "link.txt")); err != nil {
			t.Skipf("symlink creation is not permitted in this environment (%v); the symlink entry type is unverified here", err)
		}
		m, err := Capture(context.Background(), nil, root, CaptureOptions{})
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		entry, ok := lookupEntry(m, "link.txt")
		if !ok {
			t.Fatalf("in-root link must be captured: %+v", m.Excluded)
		}
		if entry.Type != TypeSymlink {
			t.Errorf("link.txt Type = %q, want %q", entry.Type, TypeSymlink)
		}
		if entry.ContentHash != hashBytes([]byte(filepath.Join(root, "target.txt"))) {
			t.Errorf("symlink ContentHash must hash the link target string, got %s", entry.ContentHash)
		}
	})

	t.Run("junction is a reparse point", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skipf("junction creation requires Windows (GOOS=%s)", runtime.GOOS)
		}
		root := t.TempDir()
		outsideDir := t.TempDir()
		writeFile(t, outsideDir, "outside.txt", "outside\n")
		junc := filepath.Join(root, "junc")
		out, err := exec.Command("cmd", "/c", "mklink", "/J", junc, outsideDir).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J: %v (output: %q)", err, string(out))
		}
		writeFile(t, root, "normal.txt", "normal\n")

		m, err := Capture(context.Background(), nil, root, CaptureOptions{})
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if _, ok := lookupEntry(m, "junc"); ok {
			t.Error("junction must not be captured as content")
		}
		if reason, ok := lookupReason(m, "junc"); !ok || reason != ReasonReparsePoint {
			t.Errorf("junc exclusion = %q (present=%v), want %q", reason, ok, ReasonReparsePoint)
		}
		for _, entry := range m.Entries {
			if strings.HasPrefix(entry.Path, "junc/") {
				t.Errorf("walk must not descend into the junction, found %s", entry.Path)
			}
		}
		if _, ok := lookupEntry(m, "normal.txt"); !ok {
			t.Error("normal.txt must still be captured alongside the junction")
		}
		if !strings.Contains(strings.Join(m.Rules, "\n"), "junction") {
			t.Errorf("Rules must state the reparse-point rule, got %v", m.Rules)
		}
	})
}

// TestManifestHashDeterministic pins the hash contract: content-addressed and
// order-independent, unaffected by timestamps, HEAD, or where the tree lives.
func TestManifestHashDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "alpha\n")
	writeFile(t, root, "sub/b.txt", "beta\n")

	first, err := Capture(context.Background(), nil, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	second, err := Capture(context.Background(), nil, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("two captures of an unchanged tree differ: %s vs %s", first.Hash, second.Hash)
	}
	t.Logf("EVIDENCE repeat capture hash=%s", first.Hash)

	// Only the mtime changes: content addressing must ignore it.
	staleStat(t, filepath.Join(root, "a.txt"))
	third, err := Capture(context.Background(), nil, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if third.Hash != first.Hash {
		t.Errorf("mtime change altered the manifest hash: %s vs %s", third.Hash, first.Hash)
	}
	t.Logf("EVIDENCE mtime-only change kept hash=%s", third.Hash)

	// One byte of content changes: the hash must follow.
	writeFile(t, root, "a.txt", "alphb\n")
	fourth, err := Capture(context.Background(), nil, root, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if fourth.Hash == first.Hash {
		t.Error("a one-byte content change must change the manifest hash")
	}
	t.Logf("EVIDENCE one-byte change: %s -> %s", first.Hash, fourth.Hash)

	// Traversal order must not matter: the canonical form is sorted.
	shuffled := *fourth
	shuffled.Entries = []Entry{fourth.Entries[1], fourth.Entries[0]}
	if got := computeManifestHash(&shuffled); got != fourth.Hash {
		t.Errorf("hash depends on entry order: %s vs %s", got, fourth.Hash)
	}

	// HEAD and the root path are recorded, but they are not part of the hash.
	head := "0123456789abcdef0123456789abcdef01234567"
	withHead := *fourth
	withHead.HeadCommit = &head
	withHead.Root = filepath.Join(t.TempDir(), "somewhere-else")
	if got := computeManifestHash(&withHead); got != fourth.Hash {
		t.Errorf("HeadCommit/Root must not affect the hash: %s vs %s", got, fourth.Hash)
	}

	// The mode is part of the canonical header, so a plain and a git capture of
	// the same content cannot collide.
	asGit := *fourth
	asGit.Mode = ModeGit
	if got := computeManifestHash(&asGit); got == fourth.Hash {
		t.Error("mode must be part of the manifest hash")
	}
}

// TestCaptureDifferentRootsSameContentHaveSameHash documents the consequence of
// content addressing: moving an identical tree does not change its hash.
func TestCaptureDifferentRootsSameContentHaveSameHash(t *testing.T) {
	first := t.TempDir()
	writeFile(t, first, "a.txt", "same\n")
	second := t.TempDir()
	writeFile(t, second, "a.txt", "same\n")

	m1, err := Capture(context.Background(), nil, first, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	m2, err := Capture(context.Background(), nil, second, CaptureOptions{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if m1.Hash != m2.Hash {
		t.Errorf("identical content in different roots must hash identically: %s vs %s", m1.Hash, m2.Hash)
	}
	if m1.Root == m2.Root {
		t.Fatal("test setup is broken: both captures used the same root")
	}
}

// --- helpers ---

func requireGitCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git CLI is required for this test but was not found on PATH: %v", err)
	}
}

// newGitFixtureRepo creates a real repository in t.TempDir() using the git CLI.
// The CLI is used only for setup; the capture itself must go through the
// policy-gated read-only entry point.
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
		t.Fatalf("git %s: %v (output: %s)", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func lookupEntry(m *Manifest, path string) (Entry, bool) {
	for _, e := range m.Entries {
		if e.Path == path {
			return e, true
		}
	}
	return Entry{}, false
}

func lookupReason(m *Manifest, path string) (string, bool) {
	for _, e := range m.Excluded {
		if e.Path == path {
			return e.Reason, true
		}
	}
	return "", false
}

// staleStat changes only a file's mtime, which is what makes git want to
// refresh the index stat cache.
func staleStat(t *testing.T, path string) {
	t.Helper()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// repoState fingerprints the repository state the capture must not touch:
// index bytes and mtime, HEAD, and every ref file.
func repoState(t *testing.T, root string) map[string]string {
	t.Helper()
	state := map[string]string{}
	paths := []string{
		filepath.Join(root, ".git", "index"),
		filepath.Join(root, ".git", "HEAD"),
	}
	refsDir := filepath.Join(root, ".git", "refs")
	err := filepath.WalkDir(refsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk refs: %v", err)
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(root, p)
		state[filepath.ToSlash(rel)] = "sha256=" + hex.EncodeToString(sum[:]) + " mtime=" + info.ModTime().UTC().Format(time.RFC3339Nano)
	}
	return state
}

// copyTree copies a directory tree without following links, for the fixture
// copy the journey manifest requires before any capture.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("fixture contains a non-regular file: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy fixture %s: %v", src, err)
	}
}

// journeyFixtureHashes reads the published sha256 for every file of one fixture
// from testdata/journeys/manifest.json.
func journeyFixtureHashes(t *testing.T, fixtureDir, name string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read journey manifest: %v", err)
	}
	var doc struct {
		Fixtures []struct {
			Name  string `json:"name"`
			Files []struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
			} `json:"files"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse journey manifest: %v", err)
	}
	for _, fx := range doc.Fixtures {
		if fx.Name != name {
			continue
		}
		out := make(map[string]string, len(fx.Files))
		for _, f := range fx.Files {
			out[f.Path] = f.SHA256
		}
		return out
	}
	t.Fatalf("fixture %q not found in the journey manifest", name)
	return nil
}
