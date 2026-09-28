// Package merge builds the candidate tree of a run attempt: the result of
// applying a run's working copy onto the user's current workspace, described
// as a set of per-path operations plus the conflicts that block publishing.
//
// Prepare is a pure computation over manifests. It reads the working copy to
// stream new content into the blob store, and it never writes to the target
// root or to the working copy: publishing is a later step that re-verifies
// every expectation recorded here before it touches the user's files. Because
// external editors are not bound by any lock the run may hold, the target
// manifest and the content of the working copy are both re-checked here, and a
// candidate is only usable while its hashes still describe both trees.
//
// This step deliberately does not decide how a delete is performed, does not
// touch a journal, and does not recover an interrupted publish: those belong
// to the publisher.
package merge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/codeflow/backend/internal/runworkspace"
)

const (
	// FormatVersion is the candidate schema version emitted by this package.
	FormatVersion = 1

	// candidateHashDomain separates candidate hashes from manifest hashes.
	candidateHashDomain = "codeflow-merge-candidate\n"

	// binarySniffBytes is how much of a new file is inspected for a NUL byte
	// when deciding whether the operation carries binary content.
	binarySniffBytes = 8000
)

// Operation kinds, spelled out as constants so the publisher and its tests
// never match a literal.
const (
	// KindCreate adds a path that did not exist in the target.
	KindCreate = "create"
	// KindModify replaces the content at a path that exists in the target.
	KindModify = "modify"
	// KindDelete removes a path from the target.
	KindDelete = "delete"
)

// Conflict reasons. The two exclusion reasons are returned with the capture's
// exclusion reason appended after a colon, so a reader can tell a new secret
// from an oversize file without re-deriving it.
const (
	// ReasonTargetChanged means the target holds a third state: neither the
	// frozen base nor the run result. Publishing over it would destroy an edit
	// made after the run started.
	ReasonTargetChanged = "target_changed"
	// ReasonResultExcluded means the run changed a path that the result capture
	// excluded (a new secret, an oversize file, a link out of the tree), so the
	// new content has no manifest description and no blob hash.
	ReasonResultExcluded = "result_excluded"
	// ReasonTargetExcluded means the run changed a path that the target capture
	// excluded, so the current content at that path is unknown.
	ReasonTargetExcluded = "target_excluded"
)

// Sentinel errors returned by Prepare.
var (
	// ErrManifestVersion means one of the three manifests is not format 1.
	ErrManifestVersion = errors.New("merge: unsupported manifest format version")

	// ErrManifestHashMismatch means a manifest does not describe its own
	// entries (see runworkspace.Manifest.Verify).
	ErrManifestHashMismatch = errors.New("merge: manifest hash does not match its entries")

	// ErrManifestOrder means a manifest's entries or exclusions are not sorted
	// by path or contain duplicates.
	ErrManifestOrder = errors.New("merge: manifest entries are not sorted by path or contain duplicates")

	// ErrRootMismatch means base and target do not describe the same root, or
	// ResultRoot is not the root of the result manifest.
	ErrRootMismatch = errors.New("merge: base, target, and result roots do not match")

	// ErrModeMismatch means base and target were captured in different modes,
	// so their entries are not comparable (git mode does not enumerate paths
	// that plain mode would).
	ErrModeMismatch = errors.New("merge: base and target capture modes differ")

	// ErrResultChangedAfterCapture means a path no longer holds the content the
	// result manifest recorded, so the manifest is no longer a truthful
	// description of the working copy. No candidate is returned.
	ErrResultChangedAfterCapture = errors.New("merge: result content changed after capture")

	// ErrUnsafeManifestPath means a manifest path is absolute or contains a
	// parent segment, so joining it to a root could escape that root.
	ErrUnsafeManifestPath = errors.New("merge: manifest path is not a safe relative path")
)

// BlobWriter stores content by value and answers with the hash of what it
// stored.
//
// The signature is the one of (*artifact.BlobStore).Put in the package that
// owns the blob repository; this package never imports that one, so the
// publisher hands the store in and a test can hand in a counter.
type BlobWriter interface {
	Put(ctx context.Context, r io.Reader) (hash string, size int64, err error)
}

// Operation is one path change from the current target to the run result.
//
// The Old fields are the expectations the target must still satisfy when the
// publisher runs: they are the base values, which the target was required to
// hold while this candidate was built. A create has no Old values; a delete
// has no New values.
type Operation struct {
	// Path is relative to Root with forward slashes, as in a manifest.
	Path string `json:"path"`
	// Kind is KindCreate, KindModify, or KindDelete.
	Kind string `json:"kind"`
	// OldType, OldHash, OldMode are what the target must still hold.
	OldType string `json:"old_type,omitempty"`
	OldHash string `json:"old_hash,omitempty"`
	OldMode uint32 `json:"old_mode,omitempty"`
	// NewType, NewHash, NewMode are what the target gets. NewHash is the blob
	// hash returned by BlobWriter.Put: the same hash the result manifest, the
	// diff, the backup, and the guard all refer to. They are empty for delete.
	NewType string `json:"new_type,omitempty"`
	NewHash string `json:"new_hash,omitempty"`
	NewMode uint32 `json:"new_mode,omitempty"`
	// Size is the length of the new content in bytes: file length, or link
	// target length for a symlink. It is 0 for delete.
	Size int64 `json:"size"`
	// Binary marks new content that contains a NUL byte in its first
	// binarySniffBytes bytes. It is only a marker in this step; no
	// content-level merge is attempted.
	Binary bool `json:"binary,omitempty"`
	// RenamedFrom is set on a create whose content previously lived at another
	// path that this same candidate deletes.
	RenamedFrom string `json:"renamed_from,omitempty"`
	// RenamedTo is set on a delete whose content reappears at another path that
	// this same candidate creates.
	RenamedTo string `json:"renamed_to,omitempty"`
}

// EntrySummary is one side of a conflict: what that manifest held at the path.
type EntrySummary struct {
	// Exists is false when the path was not part of that manifest at all.
	Exists bool `json:"exists"`
	// Type is file, symlink, or deleted.
	Type string `json:"type,omitempty"`
	// ContentHash is the manifest content hash, empty for a deleted entry.
	ContentHash string `json:"content_hash,omitempty"`
	// Mode is the permission bits the manifest recorded.
	Mode uint32 `json:"mode,omitempty"`
	// GitStatus is the manifest's git status when the path came from a git
	// capture; it explains why a path the run never touched is different here.
	GitStatus string `json:"git_status,omitempty"`
}

// Conflict is one path the candidate cannot publish. A conflict never deletes
// or overwrites anything: it blocks the whole candidate until a human resolves
// it.
type Conflict struct {
	Path string `json:"path"`
	// Reason is ReasonTargetChanged, ReasonResultExcluded, or
	// ReasonTargetExcluded, the latter two with ":<reason>" appended when the
	// capture stated one.
	Reason string        `json:"reason"`
	Base   *EntrySummary `json:"base,omitempty"`
	Result *EntrySummary `json:"result,omitempty"`
	Target *EntrySummary `json:"target,omitempty"`
}

// Candidate is the prepared change set.
type Candidate struct {
	FormatVersion int `json:"format_version"`
	// Root is the target root the operations apply to.
	Root string `json:"root"`
	// BaseManifestHash is the frozen baseline the run started from.
	BaseManifestHash string `json:"base_manifest_hash"`
	// TargetManifestHash is the target manifest the old-content expectations
	// were checked against; the publisher must re-verify it before publishing.
	TargetManifestHash string `json:"target_manifest_hash"`
	// ResultManifestHash is the working-copy capture the new content and the
	// blob hashes were verified against.
	ResultManifestHash string `json:"result_manifest_hash"`
	// Operations are the changes to publish, sorted by path.
	Operations []Operation `json:"operations"`
	// AlreadyApplied lists paths whose result content the target already holds:
	// the run changed them and the user's tree already matches, so there is
	// nothing left to publish. Sorted by path.
	AlreadyApplied []string `json:"already_applied"`
	// Conflicts are the paths that block publishing, sorted by path.
	Conflicts []Conflict `json:"conflicts"`
	// Hash is "sha256:<hex>" over the canonical encoding of every other field.
	Hash string `json:"hash"`
}

// Publishable reports whether this candidate may be published at all: a
// candidate with conflicts is never published, and a candidate with nothing to
// do is not published either.
func (c *Candidate) Publishable() bool {
	if c == nil {
		return false
	}
	return len(c.Conflicts) == 0 && len(c.Operations) > 0
}

// candidateHashInput is the canonical encoding of a candidate. It mirrors
// Candidate without its Hash field: every other field, including all three
// manifest hashes, every operation field (Old* and New* alike), and every
// conflict is part of the hash. The encoding is a struct, so field order is
// fixed by this declaration rather than by map iteration or slice layout: Go
// marshals struct fields in declaration order, and every slice here is already
// sorted by path before the hash is computed.
type candidateHashInput struct {
	FormatVersion      int
	Root               string
	BaseManifestHash   string
	TargetManifestHash string
	ResultManifestHash string
	Operations         []Operation
	AlreadyApplied     []string
	Conflicts          []Conflict
}

// candidateHash returns the canonical hash of a candidate. Operations,
// AlreadyApplied, and Conflicts must already be sorted by path.
func candidateHash(c *Candidate) string {
	input := candidateHashInput{
		FormatVersion:      c.FormatVersion,
		Root:               c.Root,
		BaseManifestHash:   c.BaseManifestHash,
		TargetManifestHash: c.TargetManifestHash,
		ResultManifestHash: c.ResultManifestHash,
		Operations:         c.Operations,
		AlreadyApplied:     c.AlreadyApplied,
		Conflicts:          c.Conflicts,
	}
	h := sha256.New()
	io.WriteString(h, candidateHashDomain)
	enc := json.NewEncoder(h)
	// HTML escaping off so a path containing & or < encodes the same
	// everywhere; Encode appends one newline, which keeps the canonical form a
	// single, unambiguous line.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(input); err != nil {
		// Encode only fails when the writer fails, and a hash.Hash never does;
		// there is no error to return from a hash function, and returning a
		// made-up hash would be worse than stopping.
		panic(fmt.Sprintf("merge: encode candidate hash input: %v", err))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// entryExists reports whether an entry describes content on disk. A deleted
// entry exists in the manifest but not in the tree, so it counts as absent
// everywhere an operation is generated, and a nil entry is absent too.
func entryExists(e *runworkspace.Entry) bool {
	return e != nil && e.Type != runworkspace.TypeDeleted
}

// sameState reports whether two manifest sides describe the same state at a
// path: both absent (no entry, or a deleted entry), or both present with the
// same content. This is the identity the three-way decision table compares.
func sameState(a, b *runworkspace.Entry) bool {
	if !entryExists(a) && !entryExists(b) {
		return true
	}
	return sameContent(a, b)
}

// sameContent reports whether the content of a path is unchanged between two
// manifests. Type and ContentHash are the content identity: a path that
// changed from a file to a link, or from a link to a file, is never the same
// even if a hash of one form happened to equal the hash of the other.
//
// The permission bits are part of the identity only where they carry
// information. Go reports 0666/0777 for every path on Windows, so comparing
// them there would report every path as changed; on other systems the
// executable bit is a real change.
func sameContent(a, b *runworkspace.Entry) bool {
	if !entryExists(a) || !entryExists(b) {
		return false
	}
	if a.Type != b.Type || a.ContentHash != b.ContentHash {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return a.Mode == b.Mode
}
