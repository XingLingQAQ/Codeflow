package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/codeflow/backend/internal/policy"
)

// ErrReadOnlySubcommand signals that a caller asked the read-only entry point
// for a git subcommand, or an argument form of one, that is outside the closed
// read-only whitelist. No git process is started for such a request: the error
// is returned before policy evaluation and before exec.
var ErrReadOnlySubcommand = errors.New("git subcommand not permitted by the read-only entry point")

// readOnlySubcommands is the closed set of git subcommands ReadOnly may start.
//
// The set is restricted to commands that observe repository state without
// writing to the object database, the index, or refs. Note that this is a
// whitelist of *subcommands*, not a claim that every argument of these
// subcommands is harmless: arguments are additionally validated by
// validateReadOnlyArgs (global options) and readOnlyArgValidators
// (per-subcommand forms), and the hardening config below (core.fsmonitor=false)
// is prepended to the caller's arguments so that it cannot be overridden.
//
//   - rev-parse:    resolves revisions/paths (--show-toplevel, --verify HEAD)
//   - status:       reports worktree state (may refresh the index stat cache;
//     the optional-lock suppression below keeps it from doing so)
//   - ls-files:     enumerates index/untracked paths
//   - symbolic-ref: reports the current branch ref (read form only, see below)
//   - cat-file:     reads objects already in the repository (without the
//     external-driver forms, see below)
var readOnlySubcommands = map[string]bool{
	"rev-parse":    true,
	"status":       true,
	"ls-files":     true,
	"symbolic-ref": true,
	"cat-file":     true,
}

// readOnlyForbiddenGlobals are git global options that would let a caller
// redirect the repository, inject configuration, or replace the exec path.
// They are rejected outright instead of being forwarded: ReadOnly must observe
// exactly the repository the manager was constructed for, under exactly the
// configuration the caller cannot influence.
var readOnlyForbiddenGlobals = map[string]bool{
	"-c":             true,
	"--config-env":   true,
	"-C":             true,
	"--git-dir":      true,
	"--work-tree":    true,
	"--namespace":    true,
	"--exec-path":    true,
	"--bare":         true,
	"--no-index":     true,
	"--repository":   true,
	"--super-prefix": true,
}

// symbolicRefReadOptions are the only options the read form of symbolic-ref
// accepts. Everything else — notably -d/--delete (delete the ref) and
// -m/--message (write the ref with a reflog message) — is rejected, and more
// than one positional argument is a write as well:
//
//	git symbolic-ref HEAD refs/heads/evil   # rewrites HEAD
//	git symbolic-ref -d HEAD                # deletes HEAD
var symbolicRefReadOptions = map[string]bool{
	"-q":           true,
	"--quiet":      true,
	"--short":      true,
	"--no-recurse": true,
}

// catFileExternalDriverOptions are cat-file options that make git execute a
// program configured in the repository (textconv drivers, clean/smudge
// filters) — arbitrary command execution on behalf of a "read". --path
// supplies the attributes path those drivers are looked up with.
var catFileExternalDriverOptions = []string{"--textconv", "--filters", "--path"}

// readOnlyArgValidators holds the per-subcommand argument rules, keyed by
// subcommand. It receives the arguments that follow the subcommand. A
// subcommand without an entry here has no extra argument rule beyond the
// global blacklist.
var readOnlyArgValidators = map[string]func([]string) error{
	"symbolic-ref": validateSymbolicRefArgs,
	"cat-file":     validateCatFileArgs,
}

// ReadOnly runs a read-only git subcommand against the manager's work
// directory, having first obtained a process_start policy decision. It is the
// hardened counterpart of execGitRaw for baseline capture: plain `git status`
// refreshes stale stat information in .git/index (measured on git
// 2.55.0.windows.4: the index file was rewritten even though no content
// changed), which would mutate the user's repository as a side effect of
// observation.
//
//   - `--no-optional-locks` and GIT_OPTIONAL_LOCKS=0 stop optional index
//     refreshes; `-c core.fsmonitor=false` stops an external fsmonitor helper
//     from running. They precede the caller's arguments and are therefore in
//     effect for the whole invocation.
//
// Known boundary, not handled here: `status` may still execute a
// repository-local clean filter when it has to recompute the hash of a
// stat-dirty file, and reading repository state reads repository
// configuration. ReadOnly therefore removes the *incidental* writes of a
// trusted repository (index refresh, fsmonitor); it does not turn an untrusted
// repository into a safe one. Executing untrusted repositories is the T2.04
// sandbox's job, not this entry point's.
//
// stdout is returned verbatim (no trimming): callers parsing NUL-separated
// (-z) formats need the exact byte stream. stderr is folded into the error.
func (m *GitManager) ReadOnly(ctx context.Context, args ...string) ([]byte, error) {
	if err := validateReadOnlyArgs(args); err != nil {
		return nil, err
	}
	sub, rest, err := readOnlySubcommand(args)
	if err != nil {
		return nil, err
	}
	if !readOnlySubcommands[sub] {
		return nil, fmt.Errorf("%w: %q", ErrReadOnlySubcommand, sub)
	}
	if validate, ok := readOnlyArgValidators[sub]; ok {
		if err := validate(rest); err != nil {
			return nil, err
		}
	}

	full := make([]string, 0, len(args)+4)
	full = append(full, "--no-optional-locks", "-c", "core.fsmonitor=false")
	full = append(full, args...)

	// Policy gate: identical contract to execGit/execGitRaw — with no evaluator
	// installed the request is denied and no process is started.
	decision := policy.EnforceBoundary(ctx, policy.Request{
		Operation: policy.OperationProcessStart,
		Resource:  "git " + strings.Join(full, " "),
	})
	if err := policy.DenialError(decision); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = m.workDir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return outBuf.Bytes(), fmt.Errorf("git %s: %w (stderr: %s)",
			strings.Join(full, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return outBuf.Bytes(), nil
}

// readOnlySubcommand returns the first non-option token, i.e. the subcommand,
// together with the arguments that follow it. Options before the subcommand
// are skipped; "--" is skipped as a separator so a caller can pass pathspecs
// that begin with a dash.
func readOnlySubcommand(args []string) (string, []string, error) {
	for i, arg := range args {
		if arg == "--" || strings.HasPrefix(arg, "-") {
			continue
		}
		return arg, args[i+1:], nil
	}
	return "", nil, fmt.Errorf("%w: no subcommand in %v", ErrReadOnlySubcommand, args)
}

// validateSymbolicRefArgs accepts only the read form of symbolic-ref: exactly
// one positional argument (the ref to report) and none of the write options.
func validateSymbolicRefArgs(args []string) error {
	positional := 0
	for _, arg := range args {
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if !symbolicRefReadOptions[arg] {
				return fmt.Errorf("%w: symbolic-ref option %q is not accepted by the read-only entry point (symbolic-ref is only read-only as `symbolic-ref <ref>`)", ErrReadOnlySubcommand, arg)
			}
			continue
		}
		positional++
	}
	if positional != 1 {
		return fmt.Errorf("%w: symbolic-ref requires exactly one ref name, got %d positional arguments (a second argument writes the ref)", ErrReadOnlySubcommand, positional)
	}
	return nil
}

// validateCatFileArgs rejects the cat-file forms that execute repository
// configured external programs. Object reads (-t, -s, -e, -p, <type>
// <object>, --batch-check, ...) stay available.
func validateCatFileArgs(args []string) error {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name := arg
		if i := strings.IndexByte(arg, '='); i >= 0 {
			name = arg[:i]
		}
		// A bare "--" and one-letter options cannot abbreviate a long option.
		if len(name) < 3 {
			continue
		}
		for _, forbidden := range catFileExternalDriverOptions {
			// git accepts unambiguous abbreviations, so --textc and --filt
			// must be rejected exactly like --textconv and --filters.
			if strings.HasPrefix(forbidden, name) {
				return fmt.Errorf("%w: cat-file option %q runs repository-configured external drivers and is not accepted by the read-only entry point", ErrReadOnlySubcommand, arg)
			}
		}
	}
	return nil
}

// validateReadOnlyArgs rejects argument shapes that would let a caller point
// git at another repository, inject configuration, or replace the git exec
// path. Only the forbidden-global blacklist is enforced here; subcommand
// semantics stay git's responsibility.
func validateReadOnlyArgs(args []string) error {
	for _, arg := range args {
		if readOnlyForbiddenGlobals[arg] {
			return fmt.Errorf("%w: global option %q is not accepted by the read-only entry point", ErrReadOnlySubcommand, arg)
		}
		// Attached forms such as --git-dir=... must not slip past the
		// exact-match check above.
		if strings.HasPrefix(arg, "--") {
			if name, _, found := strings.Cut(arg, "="); found && readOnlyForbiddenGlobals[name] {
				return fmt.Errorf("%w: global option %q is not accepted by the read-only entry point", ErrReadOnlySubcommand, name)
			}
		}
	}
	return nil
}
