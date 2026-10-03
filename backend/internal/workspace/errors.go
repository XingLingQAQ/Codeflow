package workspace

// Root refusal errors (T1.10.d).
//
// The file service and the project binding service share one rule: a root is
// usable only when it is inside the configured CODEFLOW_WORKSPACE_ROOTS
// allow-list, or when no allow-list is configured and the explicit temporary
// migration switch CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING=1 is set. The
// switch never widens a configured allow-list.
//
// Every refusal is classed by ErrRootNotAllowed so callers (HTTP handlers) can
// map "this root may not be used" to 403 with errors.Is, without matching
// error text. The two reasons stay distinguishable: ErrRootsUnconfigured
// reports "nothing was configured" and the plain sentinel reports "outside the
// configured allow-list".

import (
	"errors"
	"fmt"
)

// ErrRootNotAllowed is the sentinel class of every workspace-root refusal:
// not configured, outside the allow-list, or not resolvable. errors.Is(err,
// ErrRootNotAllowed) must be true for all of them.
var ErrRootNotAllowed = errors.New("workspace root not allowed")

// ErrRootsUnconfigured is the specific refusal used when CODEFLOW_WORKSPACE_ROOTS
// is empty and the temporary migration switch is off. It wraps
// ErrRootNotAllowed, so errors.Is(err, ErrRootNotAllowed) is true for it while
// errors.Is(err, ErrRootsUnconfigured) distinguishes "nothing configured" from
// "configured, but this root is not in the list".
var ErrRootsUnconfigured = fmt.Errorf("no workspace roots are configured; set CODEFLOW_WORKSPACE_ROOTS to the allowed directories, or for the temporary desktop migration set CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING=1: %w", ErrRootNotAllowed)

// errRootNotConfigured is the refusal of a root while no allow-list is
// configured and the migration switch is off.
func errRootNotConfigured(root string) error {
	return fmt.Errorf("%w (root %s)", ErrRootsUnconfigured, root)
}

// errRootOutsideAllowed is the refusal of a root that is not inside the
// configured allow-list. The migration switch cannot relax this: with a
// configured allow-list only the list is honored.
func errRootOutsideAllowed(root string) error {
	return fmt.Errorf("%w: %s is outside the configured CODEFLOW_WORKSPACE_ROOTS allow-list; add it to CODEFLOW_WORKSPACE_ROOTS or use a directory inside it", ErrRootNotAllowed, root)
}

// errRootUnresolvable is the refusal of a candidate root that cannot be
// resolved to the directory the OS routes to. There is no way to tell where it
// leads, so it fails closed.
func errRootUnresolvable(root string, cause error) error {
	return fmt.Errorf("%w: %s could not be resolved to its final path: %v", ErrRootNotAllowed, root, cause)
}
