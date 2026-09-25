package runworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ownerMarkerName is the ownership file written at the root of every working
// copy before any content is copied. It is the only thing that authorizes
// Remove to delete the directory, so a stray path can never be removed by
// mistake.
const ownerMarkerName = ".codeflow-owner.json"

// Ownership identifies the run attempt that created (and therefore owns) a
// working copy.
type Ownership struct {
	RunID         string `json:"run_id"`
	AttemptID     string `json:"attempt_id"`
	OwnerInstance string `json:"owner_instance"`
}

// ownerMarker is the on-disk form of the ownership claim.
type ownerMarker struct {
	Ownership
	ManifestHash string `json:"manifest_hash"`
	CreatedAt    string `json:"created_at"`
}

// WorkingCopy is a materialized baseline on disk.
type WorkingCopy struct {
	// Path is the working copy root (a freshly created temp directory).
	Path string `json:"path"`
	// ManifestHash is the hash of the baseline this copy was made from.
	ManifestHash string `json:"manifest_hash"`
	// Owner is the run attempt that may remove this copy.
	Owner Ownership `json:"owner"`
	// Files are the relative paths copied, sorted.
	Files []string `json:"files"`
	// Skipped are the manifest entries that were not materialized, sorted.
	// Symlink entries land here in this step.
	Skipped []string `json:"skipped"`
}

// Materialize writes a working copy of m under destParent and returns it.
//
// The destination is a fresh temp directory, so two attempts never share one.
// It refuses destinations that overlap the source tree (either direction):
// a copy placed inside the tree it copies from would recurse into itself as it
// grows. Each file is hashed while it is copied and compared with the
// manifest; on mismatch the partial copy is removed and
// ErrSourceChangedDuringCopy is returned, because a baseline that no longer
// matches its source cannot be trusted for rollback or comparison.
func Materialize(ctx context.Context, m *Manifest, destParent string, owner Ownership) (*WorkingCopy, error) {
	if m == nil {
		return nil, errors.New("runworkspace: manifest is required")
	}
	if err := checkDestination(m.Root, destParent); err != nil {
		return nil, err
	}
	if strings.TrimSpace(destParent) == "" {
		return nil, errors.New("runworkspace: destination parent is required")
	}

	info, err := os.Stat(destParent)
	if err != nil {
		return nil, fmt.Errorf("runworkspace: stat destination parent: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("runworkspace: destination parent is not a directory: %s", destParent)
	}

	dest, err := os.MkdirTemp(destParent, "run-*")
	if err != nil {
		return nil, fmt.Errorf("runworkspace: create working copy: %w", err)
	}

	marker := ownerMarker{
		Ownership:    owner,
		ManifestHash: m.Hash,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	markerData, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		os.RemoveAll(dest)
		return nil, fmt.Errorf("runworkspace: encode ownership marker: %w", err)
	}
	// The marker goes down first: from here on the directory is
	// self-describing, so a crash mid-copy still leaves a removable copy.
	if err := os.WriteFile(filepath.Join(dest, ownerMarkerName), markerData, 0o600); err != nil {
		os.RemoveAll(dest)
		return nil, fmt.Errorf("runworkspace: write ownership marker: %w", err)
	}

	wc := &WorkingCopy{
		Path:         dest,
		ManifestHash: m.Hash,
		Owner:        owner,
		Files:        []string{},
		Skipped:      []string{},
	}

	for _, entry := range m.Entries {
		if err := ctx.Err(); err != nil {
			os.RemoveAll(dest)
			return nil, err
		}
		switch entry.Type {
		case TypeDeleted:
			// Nothing on disk to copy; the manifest records the deletion and a
			// later step decides how to represent it.
			wc.Skipped = append(wc.Skipped, entry.Path)
			continue
		case TypeSymlink:
			// Symlink materialization is a later decision (T1.09.b); the entry
			// is reported instead of silently dropped.
			wc.Skipped = append(wc.Skipped, entry.Path)
			continue
		case TypeFile:
		default:
			os.RemoveAll(dest)
			return nil, fmt.Errorf("runworkspace: unknown entry type %q for %s", entry.Type, entry.Path)
		}

		if err := copyFile(ctx, m.Root, dest, entry); err != nil {
			os.RemoveAll(dest)
			return nil, err
		}
		wc.Files = append(wc.Files, entry.Path)
	}

	return wc, nil
}

// copyFile copies one manifest entry and verifies the bytes against the
// manifest while streaming them.
func copyFile(ctx context.Context, root, dest string, entry Entry) error {
	src := filepath.Join(root, filepath.FromSlash(entry.Path))
	// Re-check containment against the root the copy reads from: a manifest is
	// data and may be replayed later, so its paths are not trusted blindly.
	if !pathWithinRoot(root, src) {
		return fmt.Errorf("runworkspace: manifest path escapes root: %s", entry.Path)
	}

	in, err := os.Open(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s is missing", ErrSourceChangedDuringCopy, entry.Path)
		}
		return fmt.Errorf("runworkspace: open %s: %w", entry.Path, err)
	}
	defer in.Close()

	target := filepath.Join(dest, filepath.FromSlash(entry.Path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("runworkspace: create directory for %s: %w", entry.Path, err)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(entry.Mode))
	if err != nil {
		return fmt.Errorf("runworkspace: create %s: %w", entry.Path, err)
	}

	h := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, h), in)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("runworkspace: copy %s: %w", entry.Path, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("runworkspace: close %s: %w", entry.Path, closeErr)
	}

	got := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if got != entry.ContentHash || written != entry.Size {
		return fmt.Errorf("%w: %s (expected %s/%d bytes, read %s/%d bytes)",
			ErrSourceChangedDuringCopy, entry.Path, entry.ContentHash, entry.Size, got, written)
	}
	return nil
}

// checkDestination rejects a destParent that contains the source root or is
// contained by it. Either direction means a copy could observe its own output.
func checkDestination(root, destParent string) error {
	if strings.TrimSpace(destParent) == "" {
		return errors.New("runworkspace: destination parent is required")
	}
	absParent, err := filepath.Abs(destParent)
	if err != nil {
		return fmt.Errorf("runworkspace: resolve destination parent: %w", err)
	}
	absParent = filepath.Clean(absParent)
	absRoot := filepath.Clean(root)

	if pathWithinRoot(absRoot, absParent) {
		return fmt.Errorf("%w: destination parent %s is inside source root %s", ErrDestinationInsideSource, absParent, absRoot)
	}
	if pathWithinRoot(absParent, absRoot) {
		return fmt.Errorf("%w: source root %s is inside destination parent %s", ErrDestinationInsideSource, absRoot, absParent)
	}
	return nil
}

// Remove deletes the working copy, but only for the owner that created it.
// The check reads the ownership marker and requires an exact match on all
// three identity fields; on any mismatch nothing is deleted.
func (wc *WorkingCopy) Remove(owner Ownership) error {
	if wc == nil || wc.Path == "" {
		return errors.New("runworkspace: working copy is empty")
	}
	markerPath := filepath.Join(wc.Path, ownerMarkerName)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return fmt.Errorf("runworkspace: read ownership marker: %w", err)
	}
	var marker ownerMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return fmt.Errorf("runworkspace: parse ownership marker: %w", err)
	}
	if marker.Ownership != owner {
		return fmt.Errorf("%w: marker owner %+v, caller %+v", ErrNotOwner, marker.Ownership, owner)
	}
	if err := os.RemoveAll(wc.Path); err != nil {
		return fmt.Errorf("runworkspace: remove working copy: %w", err)
	}
	return nil
}

// OwnerMarkerName exposes the marker file name for callers that need to
// recognize (or avoid copying) it.
func OwnerMarkerName() string { return ownerMarkerName }
