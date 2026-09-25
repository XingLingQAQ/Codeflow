package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/workspace"
)

// CanonicalizeWorkspaceRoot resolves a workspace root to the path the server
// will use for all subsequent operations. It fails closed for missing roots,
// files, and roots outside the configured allow-list.
func CanonicalizeWorkspaceRoot(root string, allowedRoots []string) (string, error) {
	return canonicalizeWorkspaceRoot(root, allowedRoots, false)
}

func canonicalizeWorkspaceRoot(root string, allowedRoots []string, allowUnrestricted bool) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory: %s", root)
	}
	abs = filepath.Clean(abs)
	for _, allowed := range allowedRoots {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(allowedAbs); err == nil {
			allowedAbs = resolved
		}
		allowedAbs = filepath.Clean(allowedAbs)
		if abs == allowedAbs || strings.HasPrefix(abs, allowedAbs+string(filepath.Separator)) {
			return abs, nil
		}
	}
	if allowUnrestricted {
		return abs, nil
	}
	if len(allowedRoots) == 0 {
		return "", fmt.Errorf("workspace root binding is disabled until CODEFLOW_WORKSPACE_ROOTS is configured")
	}
	return "", fmt.Errorf("workspace root is outside allowed roots: %s", root)
}

// BindWorkspaceRoot canonicalizes and persists the root on a project.
func (s *InMemoryProjectService) bindWorkspaceRoot(id, root string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, fmt.Errorf("project not found")
	}
	canonical, err := canonicalizeWorkspaceRoot(root, s.allowedRoots, s.allowUnrestrictedRoots)
	if err != nil {
		return nil, err
	}
	p.WorkspaceRoot = canonical
	p.BindingState = BindingStateBound
	p.UpdatedAt = nowUnix()
	p.LastActive = p.UpdatedAt
	return cloneProject(p), nil
}

func (s *InMemoryProjectService) BindWorkspaceRoot(ctx context.Context, id, root string) (*Project, error) {
	_ = ctx
	return s.bindWorkspaceRoot(id, root)
}

func nowUnix() int64 { return time.Now().Unix() }

// ---------------------------------------------------------------------------
// Workspace binding snapshots (T1.10.a)
//
// A binding snapshot records *what a workspace root was* at capture time:
// the canonical path plus the operating-system identity of the directory the
// OS actually writes into. A path alone is not an identity - the same string
// can be re-pointed at a different directory by deleting and recreating it,
// renaming it away, or re-targeting a junction - so high-risk access re-checks
// the snapshot before trusting the root again.
//
// This file only provides the model and the OS identity primitives. Persisting
// snapshots, exclusivity across projects and the validator wiring are separate
// steps.
// ---------------------------------------------------------------------------

// BindingKind classifies why a workspace binding exists.
type BindingKind string

const (
	// BindingKindPrimary is the single authoritative binding of a project.
	BindingKindPrimary BindingKind = "primary"
	// BindingKindFlow is a branch workspace created for a flow.
	BindingKindFlow BindingKind = "flow"
	// BindingKindRun is a workspace created for one run.
	BindingKindRun BindingKind = "run"
)

// Valid reports whether the kind is one of the supported binding kinds.
func (k BindingKind) Valid() bool {
	switch k {
	case BindingKindPrimary, BindingKindFlow, BindingKindRun:
		return true
	default:
		return false
	}
}

// LinkPolicyDenyEscape is the only link policy this step defines. It matches
// workspace.FSService.Resolve: a symlink that leaves the root is rejected, and
// non-symlink reparse points (Windows junctions / mount points) fail closed.
const LinkPolicyDenyEscape = "deny_escape"

// WorkspaceBindingSnapshot is an immutable record of a workspace binding at a
// point in time. Paths are stored canonicalized; the OS identity is stored so
// that a re-pointed or replaced directory can be detected later.
type WorkspaceBindingSnapshot struct {
	BindingID string      `json:"binding_id"`
	ProjectID string      `json:"project_id"`
	Kind      BindingKind `json:"kind"`
	// ParentBindingID is empty for primary and required for flow/run, so that
	// branch workspaces remain traceable to their parent binding.
	ParentBindingID string `json:"parent_binding_id,omitempty"`
	// Revision is monotonic per binding and starts at 1.
	Revision int64 `json:"revision"`
	// CanonicalRoot is the canonicalized absolute root (see
	// CanonicalizeWorkspaceRoot). It is what Resolve receives.
	CanonicalRoot string `json:"canonical_root"`
	// RootIdentity is the OS identity of the directory the OS writes into.
	RootIdentity workspace.RootIdentity `json:"root_identity"`
	// BaseRef is the base git ref the binding was created from; may be empty.
	BaseRef string `json:"base_ref,omitempty"`
	// LinkPolicy is the escape policy in force when the snapshot was captured.
	LinkPolicy string    `json:"link_policy"`
	CapturedAt time.Time `json:"captured_at"`
}

// Validate checks the structural invariants of a snapshot. It is a pure model
// check: it never touches the filesystem, so a snapshot that passes Validate
// is still subject to Recheck before the root may be trusted.
func (s WorkspaceBindingSnapshot) Validate() error {
	if strings.TrimSpace(s.BindingID) == "" {
		return fmt.Errorf("workspace binding: binding id is required")
	}
	if strings.TrimSpace(s.ProjectID) == "" {
		return fmt.Errorf("workspace binding: project id is required")
	}
	if !s.Kind.Valid() {
		return fmt.Errorf("workspace binding: invalid kind %q", string(s.Kind))
	}
	parent := strings.TrimSpace(s.ParentBindingID)
	switch s.Kind {
	case BindingKindPrimary:
		if parent != "" {
			return fmt.Errorf("workspace binding: primary binding must not have a parent binding")
		}
	default:
		if parent == "" {
			return fmt.Errorf("workspace binding: %s binding requires a parent binding", string(s.Kind))
		}
	}
	if s.Revision < 1 {
		return fmt.Errorf("workspace binding: revision must be >= 1, got %d", s.Revision)
	}
	if strings.TrimSpace(s.CanonicalRoot) == "" {
		return fmt.Errorf("workspace binding: canonical root is required")
	}
	if s.RootIdentity.IsZero() {
		return fmt.Errorf("workspace binding: root identity is required")
	}
	if s.LinkPolicy != LinkPolicyDenyEscape {
		return fmt.Errorf("workspace binding: unsupported link policy %q", s.LinkPolicy)
	}
	if s.CapturedAt.IsZero() {
		return fmt.Errorf("workspace binding: captured at is required")
	}
	return nil
}

// BindingSnapshotInput carries the caller-supplied fields of a new snapshot.
type BindingSnapshotInput struct {
	BindingID       string
	ProjectID       string
	Kind            BindingKind
	ParentBindingID string
	Revision        int64
	// Root is the workspace root as supplied by the caller; it is canonicalized
	// before capture.
	Root    string
	BaseRef string
}

// CaptureBindingSnapshot canonicalizes a workspace root and records its OS
// identity.
//
// Canonicalization reuses canonicalizeWorkspaceRoot with allowUnrestricted
// false, so a snapshot can only be captured for an existing directory inside
// allowedRoots and the caller sees exactly the same errors as
// CanonicalizeWorkspaceRoot. The identity is then read from the canonical path
// (following links/junctions, i.e. the directory the OS writes into).
func CaptureBindingSnapshot(in BindingSnapshotInput, allowedRoots []string) (WorkspaceBindingSnapshot, error) {
	canonical, err := canonicalizeWorkspaceRoot(in.Root, allowedRoots, false)
	if err != nil {
		return WorkspaceBindingSnapshot{}, err
	}
	if canonical == "" {
		return WorkspaceBindingSnapshot{}, fmt.Errorf("workspace binding: workspace root is required")
	}
	identity, err := workspace.CaptureRootIdentity(canonical)
	if err != nil {
		return WorkspaceBindingSnapshot{}, fmt.Errorf("workspace binding: %w", err)
	}
	snapshot := WorkspaceBindingSnapshot{
		BindingID:       strings.TrimSpace(in.BindingID),
		ProjectID:       strings.TrimSpace(in.ProjectID),
		Kind:            in.Kind,
		ParentBindingID: strings.TrimSpace(in.ParentBindingID),
		Revision:        in.Revision,
		CanonicalRoot:   canonical,
		RootIdentity:    identity,
		BaseRef:         strings.TrimSpace(in.BaseRef),
		LinkPolicy:      LinkPolicyDenyEscape,
		CapturedAt:      time.Now().UTC(),
	}
	if err := snapshot.Validate(); err != nil {
		return WorkspaceBindingSnapshot{}, err
	}
	return snapshot, nil
}

// Drift codes reported by BindingDriftError. They are fixed strings so callers
// (and audit records) can match on them.
const (
	// DriftRootMissing: the root no longer exists, or is no longer a directory.
	DriftRootMissing = "root_missing"
	// DriftRootRedirected: re-resolving the canonical path no longer yields
	// that same path, because the path itself or one of its ancestors became a
	// link/junction (or the chain can no longer be resolved).
	DriftRootRedirected = "root_redirected"
	// DriftRootReplaced: the path still resolves to itself, but the directory
	// behind it is a different one (deleted and recreated, renamed away and
	// recreated, or a junction re-pointed at another target).
	DriftRootReplaced = "root_replaced"
	// DriftRootNotAllowed: the root is no longer inside allowedRoots.
	DriftRootNotAllowed = "root_not_allowed"
)

// BindingDriftError reports that a binding snapshot no longer matches the
// filesystem. Retrieve it with errors.As.
type BindingDriftError struct {
	// Code is one of DriftRootMissing, DriftRootRedirected, DriftRootReplaced,
	// DriftRootNotAllowed.
	Code string
	// Detail explains the drift. It may contain filesystem paths but never
	// tokens or environment information.
	Detail string
}

func (e *BindingDriftError) Error() string {
	if e == nil {
		return "workspace binding drift"
	}
	return fmt.Sprintf("workspace binding drift (%s): %s", e.Code, e.Detail)
}

// Recheck re-resolves the binding's ancestry and compares it with the snapshot.
// It returns nil when the root still is the same directory at the same path
// inside allowedRoots, otherwise a *BindingDriftError.
//
// Checks run in this order, first failure wins:
//
//  1. root_missing     - os.Stat(CanonicalRoot) fails, or the result is not a
//     directory. Stat follows links, so a root that is still reachable through
//     a live junction is *present* here; this check is about
//     existence, not about where the path leads.
//  2. root_redirected  - filepath.Abs + filepath.EvalSymlinks of CanonicalRoot
//     either fails or returns a different path. CanonicalRoot was itself
//     produced by EvalSymlinks (see CaptureBindingSnapshot), so re-resolving it
//     is byte-stable for an untouched directory; any difference means the path
//     or one of its ancestors was replaced by a link/junction. Note that
//     EvalSymlinks fails outright for a path whose ancestor is a Windows
//     junction, so that case also lands here.
//  3. root_replaced    - the path resolves to itself but the OS identity of the
//     directory behind it differs from RootIdentity. This catches a directory
//     that was deleted and recreated at the same path, renamed away with a new
//     directory taking its name, or a junction re-pointed at another target.
//  4. root_not_allowed - the root is no longer inside allowedRoots. An empty
//     allowedRoots list allows nothing, matching the fail-closed default of
//     canonicalizeWorkspaceRoot.
//
// The order is deliberate: identity can only be compared once the path is known
// to still resolve to itself, and allow-list membership is a policy check that
// should not mask the more specific filesystem drift.
func (s WorkspaceBindingSnapshot) Recheck(allowedRoots []string) error {
	root := strings.TrimSpace(s.CanonicalRoot)
	if root == "" {
		return &BindingDriftError{Code: DriftRootMissing, Detail: "snapshot has no canonical root"}
	}

	info, err := os.Stat(root)
	if err != nil {
		return &BindingDriftError{
			Code:   DriftRootMissing,
			Detail: fmt.Sprintf("workspace root %s is missing: %v", root, err),
		}
	}
	if !info.IsDir() {
		return &BindingDriftError{
			Code:   DriftRootMissing,
			Detail: fmt.Sprintf("workspace root %s is not a directory", root),
		}
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return &BindingDriftError{
			Code:   DriftRootRedirected,
			Detail: fmt.Sprintf("workspace root %s could not be resolved: %v", root, err),
		}
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return &BindingDriftError{
			Code:   DriftRootRedirected,
			Detail: fmt.Sprintf("workspace root %s ancestry could not be resolved: %v", root, err),
		}
	}
	resolved = filepath.Clean(resolved)
	if resolved != root {
		return &BindingDriftError{
			Code:   DriftRootRedirected,
			Detail: fmt.Sprintf("workspace root %s now resolves to %s", root, resolved),
		}
	}

	identity, err := workspace.CaptureRootIdentity(root)
	if err != nil {
		return &BindingDriftError{
			Code:   DriftRootReplaced,
			Detail: fmt.Sprintf("workspace root %s identity could not be read: %v", root, err),
		}
	}
	if !identity.Equal(s.RootIdentity) {
		return &BindingDriftError{
			Code: DriftRootReplaced,
			Detail: fmt.Sprintf("workspace root %s identity changed from %s to %s",
				root, s.RootIdentity.String(), identity.String()),
		}
	}

	if !rootWithinAllowedRoots(root, allowedRoots) {
		return &BindingDriftError{
			Code:   DriftRootNotAllowed,
			Detail: fmt.Sprintf("workspace root %s is outside allowed roots", root),
		}
	}
	return nil
}

// rootWithinAllowedRoots mirrors the allow-list comparison of
// canonicalizeWorkspaceRoot (Abs, then EvalSymlinks when it succeeds, then Clean,
// then equal-or-descendant) without its error reporting, so Recheck can classify
// an out-of-list root as drift instead of a capture error.
func rootWithinAllowedRoots(root string, allowedRoots []string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}
	rootAbs = filepath.Clean(rootAbs)
	for _, allowed := range allowedRoots {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(allowedAbs); err == nil {
			allowedAbs = resolved
		}
		allowedAbs = filepath.Clean(allowedAbs)
		if rootAbs == allowedAbs || strings.HasPrefix(rootAbs, allowedAbs+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
