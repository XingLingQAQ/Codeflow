package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// requireGitCLI reports a missing git binary as an environment failure rather
// than skipping: every assertion in this file is meaningless without a real
// git, and a silent skip would hide that.
func requireGitCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git CLI is required for this test but was not found on PATH: %v", err)
	}
}

// TestReadOnlyRejectsWriteSubcommands verifies the closed whitelist: mutating
// subcommands are refused before any process is started, so a read-only caller
// cannot be tricked into committing, staging, or checking out.
func TestReadOnlyRejectsWriteSubcommands(t *testing.T) {
	requireGitCLI(t)
	policytesting.AllowForTest(t, policy.OperationProcessStart)

	manager, dir, ctx := initTestRepo(t)
	writeTestFile(t, dir, "a.txt", "hello\n")
	if _, err := manager.Commit(ctx, "init"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	writeSubcommands := [][]string{
		{"commit", "-m", "nope"},
		{"add", "a.txt"},
		{"checkout", "HEAD"},
		{"reset", "--hard"},
		{"stash", "push"},
		{"update-index", "--refresh"},
	}
	for _, args := range writeSubcommands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := manager.ReadOnly(ctx, args...)
			if !errors.Is(err, ErrReadOnlySubcommand) {
				t.Fatalf("expected ErrReadOnlySubcommand for %v, got %v (stdout %q)", args, err, out)
			}
			if len(out) != 0 {
				t.Errorf("expected no stdout for rejected subcommand, got %q", out)
			}
		})
	}

	// Repository state is untouched by the rejected calls.
	status, err := manager.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status) != 0 {
		t.Errorf("rejected write subcommands changed repository state: %+v", status)
	}
}

// TestReadOnlyRejectsRepositoryRedirectingGlobals verifies that global options
// able to point git at another repository or inject configuration are refused,
// in both separated and --option=value form.
func TestReadOnlyRejectsRepositoryRedirectingGlobals(t *testing.T) {
	requireGitCLI(t)
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	manager, _, ctx := initTestRepo(t)

	for _, args := range [][]string{
		{"--git-dir=/tmp/elsewhere", "status"},
		{"--work-tree=/tmp/elsewhere", "status"},
		{"-c", "core.fsmonitor=true", "status"},
		{"--config-env=core.fsmonitor=FSMON", "status"},
		{"-C", "/tmp/elsewhere", "status"},
		{"--exec-path=/tmp/evil", "rev-parse", "HEAD"},
	} {
		if _, err := manager.ReadOnly(ctx, args...); !errors.Is(err, ErrReadOnlySubcommand) {
			t.Errorf("expected ErrReadOnlySubcommand for %v, got %v", args, err)
		}
	}
}

// TestReadOnlyRejectsSymbolicRefWriteForms pins the argument-level rule for
// symbolic-ref: the subcommand is only read-only in its single-positional form,
// while -d/--delete/-m rewrite or delete the ref it reports.
func TestReadOnlyRejectsSymbolicRefWriteForms(t *testing.T) {
	requireGitCLI(t)
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	manager, dir, ctx := initTestRepo(t)
	writeTestFile(t, dir, "a.txt", "hello\n")
	if _, err := exec.Command("git", "-C", dir, "add", "a.txt").Output(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := exec.Command("git", "-C", dir, "commit", "-q", "-m", "init").Output(); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	headPath := filepath.Join(dir, ".git", "HEAD")
	before, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	branch := strings.TrimSpace(string(before))
	branch = strings.TrimPrefix(branch, "ref: ")

	writeForms := [][]string{
		{"symbolic-ref", "HEAD", "refs/heads/evil"},
		{"symbolic-ref", "-d", "HEAD"},
		{"symbolic-ref", "--delete", "HEAD"},
		{"symbolic-ref", "-m", "x", "HEAD", "refs/heads/evil"},
		{"symbolic-ref", "--quiet", "HEAD", "refs/heads/evil"},
		{"symbolic-ref", "--short", "HEAD", "refs/heads/evil"},
		{"symbolic-ref"},
	}
	for _, args := range writeForms {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := manager.ReadOnly(ctx, args...)
			if !errors.Is(err, ErrReadOnlySubcommand) {
				t.Fatalf("expected ErrReadOnlySubcommand for %v, got %v (stdout %q)", args, err, out)
			}
			if len(out) != 0 {
				t.Errorf("expected no stdout for a rejected symbolic-ref form, got %q", out)
			}
		})
	}

	after, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatalf("re-read HEAD: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf(".git/HEAD changed during rejected symbolic-ref calls: %q -> %q", before, after)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "refs", "heads", "evil")); !os.IsNotExist(err) {
		t.Errorf("a rejected symbolic-ref call created refs/heads/evil (stat err=%v)", err)
	}

	// The read forms must keep working.
	out, err := manager.ReadOnly(ctx, "symbolic-ref", "HEAD")
	if err != nil {
		t.Fatalf("symbolic-ref HEAD: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != branch {
		t.Errorf("symbolic-ref HEAD = %q, want %q", got, branch)
	}
	short, err := manager.ReadOnly(ctx, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatalf("symbolic-ref --short HEAD: %v", err)
	}
	if got := strings.TrimSpace(string(short)); got != strings.TrimPrefix(branch, "refs/heads/") {
		t.Errorf("symbolic-ref --short HEAD = %q, want %q", got, strings.TrimPrefix(branch, "refs/heads/"))
	}
}

// TestReadOnlyRejectsCatFileExternalDrivers pins the argument-level rule for
// cat-file: --textconv/--filters/--path make git execute repository-configured
// external programs, which is arbitrary command execution, not a read. The
// marker file proves the rejection happens before any process is started.
func TestReadOnlyRejectsCatFileExternalDrivers(t *testing.T) {
	requireGitCLI(t)
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	manager, dir, ctx := initTestRepo(t)
	writeTestFile(t, dir, "a.txt", "hello\n")
	if _, err := exec.Command("git", "-C", dir, "add", "a.txt").Output(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := exec.Command("git", "-C", dir, "commit", "-q", "-m", "init").Output(); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// A textconv driver that drops a marker file where we can see it. git runs
	// the command through its bundled sh, so a plain redirect works. It is
	// configured only after the commit so the setup itself cannot run it.
	probeDir := t.TempDir()
	marker := filepath.Join(probeDir, "pwned.txt")
	if strings.ContainsAny(marker, " \t\"'$&;") {
		t.Fatalf("marker path must not need quoting for the driver: %s", marker)
	}
	driver := "echo pwned > " + filepath.ToSlash(marker)
	for _, key := range []string{"diff.x.textconv", "filter.x.clean", "filter.x.smudge"} {
		if out, err := exec.Command("git", "-C", dir, "config", key, driver).CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %v (output: %s)", key, err, string(out))
		}
	}
	writeTestFile(t, dir, ".gitattributes", "a.txt diff=x filter=x\n")

	// Control: the driver is live, so an absent marker later means "no process
	// was started" rather than "the driver was misconfigured".
	if out, err := exec.Command("git", "-C", dir, "cat-file", "--textconv", "HEAD:a.txt").CombinedOutput(); err != nil {
		t.Fatalf("control cat-file --textconv failed, the driver is not usable: %v (output: %s)", err, string(out))
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("control run did not create the marker, the driver never executed: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove control marker: %v", err)
	}

	blob := strings.TrimSpace(runGitCLI(t, dir, "rev-parse", "HEAD:a.txt"))
	for _, args := range [][]string{
		{"cat-file", "--textconv", "HEAD:a.txt"},
		{"cat-file", "--filters", "HEAD:a.txt"},
		{"cat-file", "--path=a.txt", "--filters", blob},
		{"cat-file", "--textconv", blob},
		// git accepts unambiguous abbreviations, so these are the same risk.
		{"cat-file", "--textc", "HEAD:a.txt"},
		{"cat-file", "--filt", "HEAD:a.txt"},
		{"cat-file", "--pat", "a.txt", blob},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := manager.ReadOnly(ctx, args...)
			if !errors.Is(err, ErrReadOnlySubcommand) {
				t.Fatalf("expected ErrReadOnlySubcommand for %v, got %v (stdout %q)", args, err, out)
			}
		})
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("rejected %v executed the configured external driver (marker exists)", args)
		}
	}

	// Plain object reads keep working and still do not run the driver.
	out, err := manager.ReadOnly(ctx, "cat-file", "-p", "HEAD:a.txt")
	if err != nil {
		t.Fatalf("cat-file -p: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("cat-file -p HEAD:a.txt = %q, want %q", out, "hello\n")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("cat-file -p executed the configured external driver")
	}
}

// TestReadOnlyRequiresPolicy is the fail-closed evidence for the read-only
// entry point: without an installed evaluator no git process may start.
func TestReadOnlyRequiresPolicy(t *testing.T) {
	requireGitCLI(t)
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })

	dir := t.TempDir()
	manager := NewGitManager(dir)
	_, err := manager.ReadOnly(context.Background(), "rev-parse", "--show-toplevel")
	var denied *policy.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected *policy.DeniedError, got %v", err)
	}
	if denied.Decision.Allowed || denied.Decision.Reason != "policy evaluator is not configured" {
		t.Fatalf("unexpected decision: %+v", denied.Decision)
	}
}

// TestReadOnlyStatusDoesNotRewriteIndex is the regression this entry point
// exists for. `git status` refreshes stale stat information in .git/index; the
// optional-lock suppression must keep the index byte-identical (and its mtime
// unchanged) so that baseline capture never mutates the user's repository.
//
// The first half of the test records the plain (unhardened) behaviour as
// evidence; if this git version happens to leave the index alone, that half
// logs the version and the assertion that must hold regardless (ReadOnly) is
// still enforced below.
func TestReadOnlyStatusDoesNotRewriteIndex(t *testing.T) {
	requireGitCLI(t)
	policytesting.AllowForTest(t, policy.OperationProcessStart)

	version := gitVersion(t)

	t.Run("plain Status may rewrite the index", func(t *testing.T) {
		manager, dir, ctx := initTestRepo(t)
		writeTestFile(t, dir, "a.txt", "hello\n")
		if _, err := exec.Command("git", "-C", dir, "add", "a.txt").Output(); err != nil {
			t.Fatalf("git add: %v", err)
		}
		if _, err := exec.Command("git", "-C", dir, "commit", "-q", "-m", "init").Output(); err != nil {
			t.Fatalf("git commit: %v", err)
		}
		staleStat(t, filepath.Join(dir, "a.txt"))

		indexPath := filepath.Join(dir, ".git", "index")
		before := fileFingerprint(t, indexPath)
		// The pre-existing entry point, unchanged by this step: plain
		// `git status --porcelain=v1 -z` with no optional-lock suppression.
		if _, err := manager.Status(ctx); err != nil {
			t.Fatalf("manager.Status: %v", err)
		}
		after := fileFingerprint(t, indexPath)

		if before == after {
			t.Logf("git %s left .git/index unchanged after a plain Status; the hardening is still required for versions that do refresh (this machine's probe: 2.55.0.windows.4 rewrote it)", version)
		} else {
			t.Logf("EVIDENCE git %s: GitManager.Status (plain status) rewrote .git/index: %s -> %s", version, before, after)
		}
	})

	t.Run("ReadOnly leaves the index untouched", func(t *testing.T) {
		manager, dir, ctx := initTestRepo(t)
		writeTestFile(t, dir, "a.txt", "hello\n")
		if _, err := exec.Command("git", "-C", dir, "add", "a.txt").Output(); err != nil {
			t.Fatalf("git add: %v", err)
		}
		if _, err := exec.Command("git", "-C", dir, "commit", "-q", "-m", "init").Output(); err != nil {
			t.Fatalf("git commit: %v", err)
		}
		staleStat(t, filepath.Join(dir, "a.txt"))

		indexPath := filepath.Join(dir, ".git", "index")
		before := fileFingerprint(t, indexPath)
		out, err := manager.ReadOnly(ctx, "status", "--porcelain=v1", "-z")
		if err != nil {
			t.Fatalf("ReadOnly status: %v", err)
		}
		after := fileFingerprint(t, indexPath)

		if before.hash != after.hash {
			t.Errorf("ReadOnly status rewrote .git/index: hash %s -> %s", before.hash, after.hash)
		}
		if !before.mtime.Equal(after.mtime) {
			t.Errorf("ReadOnly status touched .git/index mtime: %s -> %s", before.mtime, after.mtime)
		}
		t.Logf("EVIDENCE git %s: ReadOnly status left .git/index identical: %s -> %s", version, before, after)

		// The command still reports correct state despite the stale stat data.
		entries, err := ParseStatusPorcelainZ(out)
		if err != nil {
			t.Fatalf("parse status: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected clean status for a file whose content is unchanged, got %+v", entries)
		}
	})
}

// TestReadOnlyReadCommandsWork exercises the whitelisted subcommands end to
// end so the whitelist is not merely restrictive but usable.
func TestReadOnlyReadCommandsWork(t *testing.T) {
	requireGitCLI(t)
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	manager, dir, ctx := initTestRepo(t)
	writeTestFile(t, dir, "a.txt", "hello\n")
	if _, err := exec.Command("git", "-C", dir, "add", "a.txt").Output(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := exec.Command("git", "-C", dir, "commit", "-q", "-m", "init").Output(); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	toplevel, err := manager.ReadOnly(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatalf("rev-parse --show-toplevel: %v", err)
	}
	got := strings.TrimSpace(string(toplevel))
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		want = dir
	}
	if !samePath(got, want) {
		t.Errorf("rev-parse --show-toplevel = %q, want %q", got, want)
	}

	head, err := manager.ReadOnly(ctx, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse --verify HEAD: %v", err)
	}
	if len(strings.TrimSpace(string(head))) != 40 {
		t.Errorf("expected a 40-char commit hash, got %q", head)
	}

	files, err := manager.ReadOnly(ctx, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		t.Fatalf("ls-files: %v", err)
	}
	if !strings.Contains(string(files), "a.txt") {
		t.Errorf("ls-files did not list a.txt: %q", files)
	}

	// stdout is returned verbatim: the raw NUL-separated stream keeps its
	// trailing separator, which a TrimSpace-style helper would destroy.
	if !strings.HasSuffix(string(files), "\x00") {
		t.Errorf("expected ls-files -z output to end with NUL, got %q", files)
	}
}

// TestReadOnlyFailureCarriesStderr verifies that a failing read command still
// returns an error carrying git's stderr, so callers can distinguish "not a
// repository" from "policy denied".
func TestReadOnlyFailureCarriesStderr(t *testing.T) {
	requireGitCLI(t)
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	manager := NewGitManager(t.TempDir())

	_, err := manager.ReadOnly(context.Background(), "rev-parse", "--verify", "-q", "HEAD")
	if err == nil {
		t.Fatal("expected an error outside a repository")
	}
	var denied *policy.DeniedError
	if errors.As(err, &denied) {
		t.Fatalf("expected a git failure, got a policy denial: %v", err)
	}
	if !strings.Contains(err.Error(), "stderr:") {
		t.Errorf("expected the error to carry stderr, got %v", err)
	}
}

// --- helpers ---

type fileFingerprintValue struct {
	hash  string
	mtime time.Time
}

func (v fileFingerprintValue) String() string {
	return v.hash + "@" + v.mtime.UTC().Format(time.RFC3339Nano)
}

func fileFingerprint(t *testing.T, path string) fileFingerprintValue {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fileFingerprintValue{hash: hex.EncodeToString(sum[:]), mtime: info.ModTime()}
}

// staleStat rewrites a file's mtime without changing its content, which is
// exactly the condition that makes `git status` want to refresh the index.
func staleStat(t *testing.T, path string) {
	t.Helper()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func gitVersion(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		t.Fatalf("git --version: %v", err)
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "git version "))
}

// runGitCLI runs a git CLI command inside dir, for test setup only.
func runGitCLI(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v (output: %s)", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func samePath(a, b string) bool {
	na, errA := filepath.Abs(a)
	nb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return strings.EqualFold(filepath.Clean(na), filepath.Clean(nb))
}
