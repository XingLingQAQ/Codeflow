// Package runworkspace builds the immutable baseline manifest of a project
// workspace and materializes a working copy of it for a run attempt.
//
// The baseline is a content-addressed inventory of the workspace exactly as it
// is on disk — dirty edits and untracked files included — plus the HEAD commit
// it was derived from. Capturing a baseline must never mutate the user's
// repository: no git object is written, no index stat cache is refreshed, and
// no file in the workspace is touched. The materialized working copy is what
// later steps operate on, so the user's own index, HEAD, and worktree stay
// untouched for the whole run.
//
// Format version 1 of the manifest is defined by Manifest and Entry below; the
// hash in Manifest.Hash covers the sorted entry tuples only, so the same tree
// captured twice — or captured in a different traversal order — hashes
// identically. HeadCommit is deliberately excluded and stored separately.
package runworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codeflow/backend/internal/git"
)

const (
	// FormatVersion is the manifest schema version emitted by this package.
	FormatVersion = 1

	// ModeGit marks a manifest captured from a git work tree.
	ModeGit = "git"
	// ModePlain marks a manifest captured from a directory with no git.
	ModePlain = "plain"

	// DefaultMaxFileBytes is the per-file capture limit (5 MiB). Files above it
	// are listed in Excluded with reason "oversize" instead of being hashed.
	DefaultMaxFileBytes int64 = 5 << 20
)

// Entry type values. A deleted entry exists in the git index but not on disk;
// it carries no content hash.
const (
	TypeFile    = "file"
	TypeSymlink = "symlink"
	TypeDeleted = "deleted"
)

// Exclusion reasons. The set is closed: every path left out of Entries is
// reported with exactly one of these reasons, so a baseline can always explain
// what it does not contain.
const (
	ReasonSecret          = "secret"
	ReasonOversize        = "oversize"
	ReasonOutsideRootLink = "outside_root_link"
	ReasonReparsePoint    = "reparse_point"
	ReasonSpecialFile     = "special_file"
)

// Sentinel errors returned by Capture and Materialize.
var (
	// ErrNotWorkTreeRoot means the git manager's work directory is not the top
	// level of the repository containing root (a subdirectory binding is a
	// later decision, not silently accepted here).
	ErrNotWorkTreeRoot = errors.New("root is not the top level of the git work tree")

	// ErrDestinationInsideSource means the working-copy destination would be
	// placed inside the captured tree (or the captured tree inside the
	// destination parent), which would let the copy recurse into itself.
	ErrDestinationInsideSource = errors.New("working copy destination overlaps the captured root")

	// ErrSourceChangedDuringCopy means a file's bytes no longer match the
	// manifest, so the baseline is no longer a truthful description of the
	// source and the copy was discarded.
	ErrSourceChangedDuringCopy = errors.New("source changed after the baseline was captured")

	// ErrNotOwner means the working copy's ownership marker does not match the
	// caller, so nothing was deleted.
	ErrNotOwner = errors.New("working copy is not owned by this run attempt")
)

// Entry is one captured path. Path is relative to Manifest.Root with forward
// slashes so manifests are comparable across platforms.
type Entry struct {
	Path string `json:"path"`
	// Type is TypeFile, TypeSymlink, or TypeDeleted.
	Type string `json:"type"`
	// Mode keeps the permission bits, including the executable bit. On Windows
	// Go reports 0666/0777 for everything, so executability is not encoded
	// there (see the receipt for T1.09.b).
	Mode uint32 `json:"mode"`
	// Size is the number of bytes read for TypeFile, the link target length for
	// TypeSymlink, and 0 for TypeDeleted.
	Size int64 `json:"size"`
	// ContentHash is "sha256:<hex>" over the file bytes, over the link target
	// string for symlinks, and empty for deleted entries.
	ContentHash string `json:"content_hash"`
	// GitStatus is the porcelain v1 XY pair (for example " M", "M ", "??")
	// when git reported this path; empty for clean paths and in plain mode.
	GitStatus string `json:"git_status,omitempty"`
}

// Exclusion records a path that was deliberately left out of the baseline.
type Exclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Manifest is the baseline of a workspace at one instant.
type Manifest struct {
	FormatVersion int `json:"format_version"`
	// Mode is ModeGit or ModePlain.
	Mode string `json:"mode"`
	// Root is the cleaned absolute path the entries are relative to.
	Root string `json:"root"`
	// HeadCommit is the HEAD commit hash, or nil for a plain directory or an
	// unborn branch. It is not part of Hash.
	HeadCommit *string `json:"head_commit"`
	// Entries are sorted by Path.
	Entries []Entry `json:"entries"`
	// Excluded are the paths deliberately not captured, sorted by Path.
	Excluded []Exclusion `json:"excluded"`
	// Rules describes the exclusion rules that were actually applied.
	Rules []string `json:"rules"`
	// Hash is "sha256:<hex>" over the canonical entry tuples.
	Hash string `json:"hash"`
}

// CaptureOptions tunes a capture. The zero value uses the defaults.
type CaptureOptions struct {
	// MaxFileBytes caps a single file's size; 0 or negative means
	// DefaultMaxFileBytes.
	MaxFileBytes int64
}

func (o CaptureOptions) maxFileBytes() int64 {
	if o.MaxFileBytes <= 0 {
		return DefaultMaxFileBytes
	}
	return o.MaxFileBytes
}

// Capture inventories root and returns its baseline manifest.
//
// When gm is non-nil the capture runs in git mode: root must be the top level
// of the work tree, HEAD is recorded, and the candidate paths come from
// `git ls-files` (ignored paths are not enumerated at all) annotated with
// `git status --porcelain=v1 -z`. When gm is nil the capture walks the
// directory tree itself and no git process is started.
//
// Both modes read content from disk as it is now, so dirty and untracked state
// is part of the baseline. Neither mode writes to the repository.
func Capture(ctx context.Context, gm *git.GitManager, root string, opts CaptureOptions) (*Manifest, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("runworkspace: root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("runworkspace: resolve root: %w", err)
	}
	absRoot = filepath.Clean(absRoot)
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("runworkspace: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("runworkspace: root is not a directory: %s", absRoot)
	}

	maxBytes := opts.maxFileBytes()
	var (
		entries  []Entry
		excluded []Exclusion
		mode     string
		head     *string
		rules    []string
	)
	if gm != nil {
		mode = ModeGit
		entries, excluded, head, rules, err = captureGit(ctx, gm, absRoot, maxBytes)
	} else {
		mode = ModePlain
		entries, excluded, rules, err = capturePlain(ctx, absRoot, maxBytes)
	}
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	sort.Slice(excluded, func(i, j int) bool {
		if excluded[i].Path != excluded[j].Path {
			return excluded[i].Path < excluded[j].Path
		}
		return excluded[i].Reason < excluded[j].Reason
	})

	m := &Manifest{
		FormatVersion: FormatVersion,
		Mode:          mode,
		Root:          absRoot,
		HeadCommit:    head,
		Entries:       entries,
		Excluded:      excluded,
		Rules:         rules,
	}
	m.Hash = computeManifestHash(m)
	return m, nil
}

// computeManifestHash hashes the canonical form of the manifest. The canonical
// form is a fixed header followed by one NUL-separated tuple per entry, in
// ascending path order:
//
//	codeflow-runworkspace-manifest\n
//	format_version=<n>\nmode=<mode>\n
//	<path>\x00<type>\x00<mode>\x00<size>\x00<content_hash>\x00   (per entry)
//
// Root, HeadCommit, Excluded, and Rules are excluded on purpose: the hash
// identifies the captured content, so identical trees hash identically
// regardless of where they live or in what order they were traversed.
func computeManifestHash(m *Manifest) string {
	// Sort a copy rather than trusting the caller's ordering: the hash is
	// defined to be traversal-independent, so order independence must hold for
	// every caller, not only for the one inside Capture.
	sorted := make([]Entry, len(m.Entries))
	copy(sorted, m.Entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	h := sha256.New()
	io.WriteString(h, "codeflow-runworkspace-manifest\n")
	fmt.Fprintf(h, "format_version=%d\nmode=%s\n", m.FormatVersion, m.Mode)
	for _, e := range sorted {
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00%s\x00", e.Path, e.Type, e.Mode, e.Size, e.ContentHash)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// hashBytes returns the manifest content-hash form for a byte slice.
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// hashReaderLimited streams r into a hash and reports how many bytes it read.
// Reading stops at limit+1 bytes so a caller can detect that the source grew
// past the capture limit without buffering an unbounded file.
func hashReaderLimited(r io.Reader, limit int64) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, limit+1))
	if err != nil {
		return "", n, err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), n, nil
}

// capturePlain walks root without following any link and returns the captured
// entries, the exclusions, and the applied rule descriptions.
func capturePlain(ctx context.Context, root string, maxBytes int64) ([]Entry, []Exclusion, []string, error) {
	var (
		entries  []Entry
		excluded []Exclusion
	)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			// .git is never part of a baseline: it is repository metadata, not
			// project content, and it is enormous.
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			// A reparse point (junction, mount point) or a symlinked directory
			// must not be descended into: the walk stays inside root.
			if info.Mode()&os.ModeIrregular != 0 {
				excluded = append(excluded, Exclusion{Path: rel, Reason: ReasonReparsePoint})
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		entry, exclusion, skip, err := classifyPath(root, path, rel, info, maxBytes)
		if err != nil {
			return err
		}
		switch {
		case exclusion != nil:
			excluded = append(excluded, *exclusion)
		case !skip:
			entries = append(entries, entry)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("runworkspace: walk %s: %w", root, err)
	}

	return entries, excluded, plainRules(maxBytes), nil
}

// classifyPath turns one on-disk path into either an entry or an exclusion.
// It never follows a link: symlinks are recorded as links and junctions are
// recorded as reparse points.
func classifyPath(root, path, rel string, info fs.FileInfo, maxBytes int64) (Entry, *Exclusion, bool, error) {
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		entry, exclusion, err := captureSymlink(root, path, rel, mode)
		return entry, exclusion, exclusion != nil, err
	case mode&os.ModeIrregular != 0:
		return Entry{}, &Exclusion{Path: rel, Reason: ReasonReparsePoint}, true, nil
	case !mode.IsRegular():
		// Devices, pipes, sockets: not content, not copyable.
		return Entry{}, &Exclusion{Path: rel, Reason: ReasonSpecialFile}, true, nil
	}

	if isSecretPath(rel) {
		return Entry{}, &Exclusion{Path: rel, Reason: ReasonSecret}, true, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return Entry{}, nil, false, fmt.Errorf("runworkspace: open %s: %w", rel, err)
	}
	defer f.Close()

	hash, size, err := hashReaderLimited(f, maxBytes)
	if err != nil {
		return Entry{}, nil, false, fmt.Errorf("runworkspace: hash %s: %w", rel, err)
	}
	if size > maxBytes {
		return Entry{}, &Exclusion{Path: rel, Reason: ReasonOversize}, true, nil
	}

	return Entry{
		Path:        rel,
		Type:        TypeFile,
		Mode:        uint32(mode.Perm()),
		Size:        size,
		ContentHash: hash,
	}, nil, false, nil
}

// captureSymlink records a link without following it. The content hash is the
// hash of the link target string, so a retargeted link changes the baseline.
func captureSymlink(root, path, rel string, mode fs.FileMode) (Entry, *Exclusion, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return Entry{}, nil, fmt.Errorf("runworkspace: readlink %s: %w", rel, err)
	}
	if linkTargetEscapesRoot(root, filepath.Dir(path), target) {
		return Entry{}, &Exclusion{Path: rel, Reason: ReasonOutsideRootLink}, nil
	}
	return Entry{
		Path:        rel,
		Type:        TypeSymlink,
		Mode:        uint32(mode.Perm()),
		Size:        int64(len(target)),
		ContentHash: hashBytes([]byte(target)),
	}, nil, nil
}

// linkTargetEscapesRoot reports whether a link target resolves outside root.
// The check is lexical first; when the target exists it is additionally
// resolved so a chain of in-root links ending outside is still caught.
func linkTargetEscapesRoot(root, linkDir, target string) bool {
	var resolved string
	if filepath.IsAbs(target) {
		resolved = filepath.Clean(target)
	} else {
		resolved = filepath.Clean(filepath.Join(linkDir, target))
	}
	if !pathWithinRoot(root, resolved) {
		return true
	}
	if evaluated, err := filepath.EvalSymlinks(resolved); err == nil {
		return !pathWithinRoot(root, evaluated)
	}
	return false
}

// pathWithinRoot reports whether path is root or sits inside it.
func pathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// secretSuffixes and secretPrefixes are matched case-insensitively against the
// base name at any depth: a key is a key wherever it sits in the tree.
var (
	secretSuffixes = []string{".pem", ".key", ".p12", ".pfx", ".keystore"}
	secretPrefixes = []string{"id_rsa", "id_ecdsa", "id_ed25519"}
)

// isSecretPath applies the default secret-file rule. `.env` and `.env.*` are
// secrets except the documented templates, which exist precisely to be
// committed. Matching is case-insensitive so `.ENV` cannot slip through on a
// case-insensitive filesystem.
func isSecretPath(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	switch base {
	case ".env":
		return true
	case ".env.example", ".env.sample", ".env.template":
		return false
	}
	if strings.HasPrefix(base, ".env.") {
		return true
	}
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	for _, prefix := range secretPrefixes {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}
	return false
}

// plainRules and gitRules describe the applied rules. They are part of the
// manifest so a reader can tell what a baseline is missing without reading
// this source.
func plainRules(maxBytes int64) []string {
	return []string{
		"traversal: directory walk, links are never followed",
		"skip: directories named .git are not traversed",
		"gitignore: not applied (plain mode has no repository)",
		oversizeRule(maxBytes),
		secretRule(),
		"links: symlinks are recorded as links; targets outside the root are excluded",
		"reparse: Windows junctions/mount points are recorded and not traversed",
	}
}

func gitRules(maxBytes int64) []string {
	return []string{
		"candidate paths: git ls-files -z --cached --others --exclude-standard",
		"gitignore: ignored paths are not enumerated (exclude-standard covers .gitignore, info/exclude, core.excludesFile)",
		"content: read from disk as-is; dirty and untracked state is part of the baseline",
		"index: read-only, no refresh; HEAD recorded separately and not part of the hash",
		oversizeRule(maxBytes),
		secretRule(),
		"links: symlinks are recorded as links; targets outside the root are excluded",
		"reparse: Windows junctions/mount points are recorded and not traversed",
	}
}

func oversizeRule(maxBytes int64) string {
	return fmt.Sprintf("max-file-bytes: %d (larger files are excluded as oversize)", maxBytes)
}

func secretRule() string {
	return "secrets: .env and .env.* (except .env.example/.env.sample/.env.template), *.pem, *.key, *.p12, *.pfx, *.keystore, id_rsa*, id_ecdsa*, id_ed25519*"
}

// ContentHashHex returns the hex digest of a manifest content hash, which is
// the form the journey fixture manifest publishes.
func ContentHashHex(contentHash string) string {
	return strings.TrimPrefix(contentHash, "sha256:")
}

// samePath reports whether two paths name the same location. Beyond case and
// separator differences it also normalizes the 8.3 short form Windows reports
// for some temp directories (C:\Users\XINGLI~1\... versus
// C:\Users\XingLingQAQ\...): comparing raw strings would reject a root that is
// genuinely the work tree top level.
func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	cleanA, cleanB := filepath.Clean(absA), filepath.Clean(absB)
	if strings.EqualFold(cleanA, cleanB) {
		return true
	}
	return strings.EqualFold(canonicalPath(cleanA), canonicalPath(cleanB))
}

// canonicalPath resolves the longest existing prefix of path, which expands
// Windows short names via the OS. A path that does not exist yet is returned
// unchanged.
func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	dir := path
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		dir = parent
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return path
		}
		return filepath.Clean(filepath.Join(resolved, rel))
	}
}
