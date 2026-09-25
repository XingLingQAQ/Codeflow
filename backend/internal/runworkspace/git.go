package runworkspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codeflow/backend/internal/git"
	"github.com/codeflow/backend/internal/policy"
)

// captureGit builds a baseline from a git work tree. Every git call goes
// through GitManager.ReadOnly, so the policy gate is enforced and no optional
// lock is taken: capturing a baseline never writes an object, never refreshes
// the index stat cache, and never touches HEAD or refs.
func captureGit(ctx context.Context, gm *git.GitManager, root string, maxBytes int64) ([]Entry, []Exclusion, *string, []string, error) {
	if err := verifyWorkTreeRoot(ctx, gm, root); err != nil {
		return nil, nil, nil, nil, err
	}

	head, err := readHead(ctx, gm)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	statuses, err := readStatuses(ctx, gm)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	candidates, err := listCandidates(ctx, gm)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	var (
		entries  []Entry
		excluded []Exclusion
	)
	for _, rel := range candidates {
		entry, exclusion, skip, err := classifyGitPath(root, rel, maxBytes)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if skip {
			if exclusion != nil {
				excluded = append(excluded, *exclusion)
			}
			continue
		}
		entry.GitStatus = statuses[rel]
		entries = append(entries, entry)
	}

	return entries, excluded, head, gitRules(maxBytes), nil
}

// verifyWorkTreeRoot rejects a root that is not the top level of its work tree.
// Binding a subdirectory is a later decision; silently capturing a subtree
// while labelling it with the whole repository's HEAD would produce a baseline
// that cannot be applied.
func verifyWorkTreeRoot(ctx context.Context, gm *git.GitManager, root string) error {
	out, err := gm.ReadOnly(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("runworkspace: rev-parse --show-toplevel: %w", err)
	}
	toplevel := strings.TrimSpace(string(out))
	if toplevel == "" {
		return fmt.Errorf("%w: git reported no work tree top level for %s", ErrNotWorkTreeRoot, root)
	}
	resolved, err := filepath.EvalSymlinks(toplevel)
	if err != nil {
		resolved = filepath.Clean(toplevel)
	}
	if !samePath(resolved, root) {
		return fmt.Errorf("%w: work tree top level is %s, root is %s", ErrNotWorkTreeRoot, resolved, root)
	}
	return nil
}

// readHead returns the HEAD commit hash, or nil for an unborn branch.
func readHead(ctx context.Context, gm *git.GitManager) (*string, error) {
	out, err := gm.ReadOnly(ctx, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		// --verify -q on an unborn HEAD exits non-zero without a message; that
		// is a normal state, not a capture failure. A policy denial, however,
		// must never be silently reinterpreted as "no HEAD".
		var denied *policy.DeniedError
		if errors.As(err, &denied) {
			return nil, fmt.Errorf("runworkspace: rev-parse HEAD: %w", err)
		}
		return nil, nil
	}
	hash := strings.TrimSpace(string(out))
	if hash == "" {
		return nil, nil
	}
	return &hash, nil
}

// readStatuses maps a work-tree-relative path to its porcelain v1 XY pair.
// The status call is a pure read: ReadOnly suppresses the optional index
// refresh that plain `git status` performs.
func readStatuses(ctx context.Context, gm *git.GitManager) (map[string]string, error) {
	out, err := gm.ReadOnly(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("runworkspace: status: %w", err)
	}
	if len(out) == 0 {
		return map[string]string{}, nil
	}
	parsed, err := git.ParseStatusPorcelainZ(out)
	if err != nil {
		return nil, fmt.Errorf("runworkspace: parse status: %w", err)
	}
	statuses := make(map[string]string, len(parsed))
	for _, p := range parsed {
		statuses[filepath.ToSlash(p.Path)] = string([]byte{p.IndexStatus, p.WorktreeStatus})
	}
	return statuses, nil
}

// listCandidates enumerates the paths that make up the baseline: everything in
// the index plus untracked files, minus what git ignores. Ignored paths are
// never enumerated, so they cannot leak into the manifest.
func listCandidates(ctx context.Context, gm *git.GitManager) ([]string, error) {
	out, err := gm.ReadOnly(ctx, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("runworkspace: ls-files: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	parts := strings.Split(string(out), "\x00")
	paths := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		paths = append(paths, filepath.ToSlash(p))
	}
	return paths, nil
}

// classifyGitPath inspects one candidate path from git's own output. Paths
// outside the root are skipped without an exclusion record: they are not part
// of this baseline by definition, and only a caller that asked for a
// subdirectory binding could even see them.
func classifyGitPath(root, rel string, maxBytes int64) (Entry, *Exclusion, bool, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	if !pathWithinRoot(root, full) {
		return Entry{}, nil, true, nil
	}

	info, err := os.Lstat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Tracked in the index, gone from disk: the baseline records the
			// deletion rather than inventing content.
			return Entry{Path: rel, Type: TypeDeleted}, nil, false, nil
		}
		return Entry{}, nil, false, fmt.Errorf("runworkspace: lstat %s: %w", rel, err)
	}

	return classifyPath(root, full, rel, info, maxBytes)
}
