package project

import (
	"errors"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Binding validator (T1.10.b group 1/2)
//
// A Run freezes a binding snapshot when it is created (pinned). Every later
// high-risk access - creating another run, dispatching work into the workspace,
// merging a result back - re-validates that frozen snapshot against the binding
// that is currently in force before the workspace root may be touched.
//
// The validator is pure with respect to state: it takes both snapshots and the
// current allow-list as arguments and it never reads a database. The only I/O it
// performs is the filesystem re-check that Recheck is built from, so it can be
// called on every dispatch and merge without a cached answer going stale.
//
// Persistence of the current binding, revision bumps and cross-project
// exclusivity are the second half of this step; this half takes the current
// snapshot from the caller so it can be tested against real temporary
// directories today and wired by T1.04 (run creation/dispatch) and T1.09
// (merge) later.
// ---------------------------------------------------------------------------

// BindingPurpose says what the caller is about to do with the workspace. The
// purpose decides how strict the check is: a merge writes outside the run's
// workspace and therefore needs the extra kind rule.
type BindingPurpose string

const (
	// BindingPurposeCreateRun validates a root before a Run is created on it.
	BindingPurposeCreateRun BindingPurpose = "create_run"
	// BindingPurposeDispatch validates a root before work is dispatched into it.
	BindingPurposeDispatch BindingPurpose = "dispatch"
	// BindingPurposeMerge validates a root before a result is merged back.
	BindingPurposeMerge BindingPurpose = "merge"
)

// Valid reports whether the purpose is one of the supported purposes.
func (p BindingPurpose) Valid() bool {
	switch p {
	case BindingPurposeCreateRun, BindingPurposeDispatch, BindingPurposeMerge:
		return true
	default:
		return false
	}
}

// Stable codes reported by BindingValidationError. They mirror the drift codes
// of BindingDriftError where the cause is the same, and add the codes that are
// about the binding relationship rather than the filesystem.
const (
	// BindingCodeProjectMismatch: the run/project asking is not the project the
	// binding was captured for. A binding never crosses projects.
	BindingCodeProjectMismatch = "binding_project_mismatch"
	// BindingCodeRebound: the binding in force is not the pinned one any more -
	// a different binding ID, the same ID at a different revision, or the same
	// revision pointing at a different directory. The pinned run would silently
	// start writing into a different directory, so it is refused.
	BindingCodeRebound = "binding_rebound"
	// BindingCodeKindNotMergeable: a merge was requested on a binding kind that
	// cannot be merged into.
	BindingCodeKindNotMergeable = "binding_kind_not_mergeable"
	// BindingCodeInvalidPurpose: the caller passed a purpose this validator does
	// not know. Unknown purposes fail closed.
	BindingCodeInvalidPurpose = "binding_invalid_purpose"
	// BindingCodeCandidateInvalid: the current binding is not a structurally
	// valid snapshot, so it cannot be trusted as "what is in force". The Detail
	// names the field that failed.
	BindingCodeCandidateInvalid = "binding_candidate_invalid"
	// BindingCodeRetired: the binding in force has been retired, so nothing may
	// be created, dispatched or merged on it. A project is retired when it is
	// archived (T1.10.c), and retiring a binding individually is how a project
	// gets a new primary binding, so this is a permanent refusal for anything
	// pinned before the retire - the replacement binding has a different id and
	// would report binding_rebound instead.
	BindingCodeRetired = "binding_retired"
)

// BindingValidationError reports that a binding may not be used for the
// requested purpose. Retrieve it with errors.As.
//
// DriftErr is set when the refusal came from the filesystem re-check, so a
// caller that wants to branch on drift can read it (and match a
// *BindingDriftError) without re-running Recheck.
type BindingValidationError struct {
	// Code is one of the BindingCode* constants, or a DriftRoot* constant when
	// DriftErr is set.
	Code string
	// Purpose is the purpose the check was made for.
	Purpose BindingPurpose
	// Detail explains the refusal. It may contain filesystem paths but never
	// tokens or environment information.
	Detail string
	// DriftErr is the underlying drift, when there is one.
	DriftErr *BindingDriftError
}

func (e *BindingValidationError) Error() string {
	if e == nil {
		return "workspace binding is not usable"
	}
	return fmt.Sprintf("workspace binding invalid (%s, purpose=%s): %s", e.Code, string(e.Purpose), e.Detail)
}

// Unwrap exposes the drift error so errors.As can also reach
// *BindingDriftError through a BindingValidationError.
func (e *BindingValidationError) Unwrap() error {
	if e == nil || e.DriftErr == nil {
		return nil
	}
	return e.DriftErr
}

// ValidateBinding checks that the binding frozen by a run (pinned) is still the
// binding in force (current) and that its workspace root is still the same
// directory at the same path inside allowedRoots.
//
// Checks run in this order, first failure wins:
//
//  1. project - projectID must be non-empty and equal to pinned.ProjectID,
//     otherwise binding_project_mismatch. The binding of one project is never
//     used by another, whatever the path says.
//  2. rebound - current must be the same binding as pinned: same BindingID, same
//     Revision and same root identity. Any difference is binding_rebound. This
//     is the check that stops an old Run from silently writing into the new root
//     after the primary binding was rebound: the run pinned (bind, rev 1,
//     root A); after a re-bind the binding in force is (bind, rev 2, root B);
//     the pinned snapshot no longer matches, so the run is refused instead of
//     quietly following the new root.
//  3. purpose - the purpose must be known (binding_invalid_purpose); a merge
//     additionally requires a mergeable kind (binding_kind_not_mergeable). See
//     mergeableKind.
//  4. drift - Recheck on current, reusing root_missing, root_redirected,
//     root_replaced, root_not_allowed and root_unverifiable. The code of the
//     refusal is the drift code.
//
// pinned and current are both taken by value; the function mutates neither and
// holds no state, so it is safe to call concurrently.
func ValidateBinding(pinned, current WorkspaceBindingSnapshot, purpose BindingPurpose, projectID string, allowedRoots []string) error {
	if err := checkBindingProject(pinned, projectID); err != nil {
		return withPurpose(err, purpose)
	}
	if err := checkBindingRebound(pinned, current); err != nil {
		return withPurpose(err, purpose)
	}
	if err := checkBindingPurpose(current, purpose); err != nil {
		return withPurpose(err, purpose)
	}
	if err := current.Recheck(allowedRoots); err != nil {
		var drift *BindingDriftError
		if !errors.As(err, &drift) {
			// Recheck only ever returns *BindingDriftError; guard anyway so a
			// future change cannot turn this into an untyped pass-through.
			return &BindingValidationError{
				Code:    DriftRootUnverifiable,
				Purpose: purpose,
				Detail:  fmt.Sprintf("workspace binding check failed: %v", err),
			}
		}
		return &BindingValidationError{
			Code:     drift.Code,
			Purpose:  purpose,
			Detail:   drift.Detail,
			DriftErr: drift,
		}
	}
	return nil
}

// ValidateBindingRecord checks a pinned snapshot against the binding record the
// store says is in force *right now*, including the record's lifecycle state.
//
// ValidateBinding answers "is the binding in force the one the run pinned, at a
// root that is still the same directory?" but it only ever sees a snapshot, and a
// snapshot does not carry the state: a retired binding row still holds the
// snapshot it was retired at, which is exactly the snapshot a run pinned before
// the archive. Passing that snapshot to ValidateBinding therefore succeeds, and
// an archived project's old Run would be allowed to merge. This entry point is
// what closes that gap:
//
//  1. state - a record that is not active (retired) is refused with
//     binding_retired, for every purpose, before any other check. The refusal is
//     about the row, not about the filesystem, so it does not depend on the
//     directory still existing.
//  2. everything ValidateBinding already checks - project, rebound, purpose,
//     drift - is delegated to it unchanged, so the two entry points can never
//     disagree about the snapshot half.
//
// Callers that hold a *WorkspaceBindingRecord (T1.04 run creation/dispatch,
// T1.09 merge) use this; callers that only have a snapshot (a run replaying its
// own frozen record) keep using ValidateBinding.
func ValidateBindingRecord(pinned WorkspaceBindingSnapshot, current WorkspaceBindingRecord, purpose BindingPurpose, projectID string, allowedRoots []string) error {
	if !current.Active() {
		return &BindingValidationError{
			Code:    BindingCodeRetired,
			Purpose: purpose,
			Detail: fmt.Sprintf("workspace binding %s of project %s is %s and may not be used",
				current.Snapshot.BindingID, current.Snapshot.ProjectID, string(current.State)),
		}
	}
	return ValidateBinding(pinned, current.Snapshot, purpose, projectID, allowedRoots)
}

// withPurpose stamps the purpose onto a validation error built by one of the
// per-check helpers, so every refusal carries the purpose it was made for. The
// helper that raised it may not know the purpose (for example the project check
// runs before the purpose is inspected), and an unknown purpose is still
// reported as such by the caller.
func withPurpose(err error, purpose BindingPurpose) error {
	var invalid *BindingValidationError
	if errors.As(err, &invalid) && invalid.Purpose == "" {
		stamped := *invalid
		stamped.Purpose = purpose
		return &stamped
	}
	return err
}

// checkBindingProject rejects a binding that belongs to another project, and a
// caller that does not name a project at all.
func checkBindingProject(pinned WorkspaceBindingSnapshot, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	pinnedProject := strings.TrimSpace(pinned.ProjectID)
	if projectID == "" {
		return &BindingValidationError{
			Code:   BindingCodeProjectMismatch,
			Detail: "no project was named for the workspace binding check",
		}
	}
	if pinnedProject == "" {
		return &BindingValidationError{
			Code:   BindingCodeProjectMismatch,
			Detail: "pinned workspace binding does not name a project",
		}
	}
	if projectID != pinnedProject {
		return &BindingValidationError{
			Code: BindingCodeProjectMismatch,
			Detail: fmt.Sprintf("workspace binding belongs to project %s, not %s",
				pinnedProject, projectID),
		}
	}
	return nil
}

// checkBindingRebound rejects a current binding that is not the pinned one.
func checkBindingRebound(pinned, current WorkspaceBindingSnapshot) error {
	if strings.TrimSpace(current.BindingID) == "" {
		return &BindingValidationError{
			Code:   BindingCodeRebound,
			Detail: "no workspace binding is in force",
		}
	}
	if err := current.Validate(); err != nil {
		return &BindingValidationError{
			Code:   BindingCodeCandidateInvalid,
			Detail: fmt.Sprintf("the workspace binding in force is not usable: %v", err),
		}
	}
	if current.BindingID != pinned.BindingID {
		return &BindingValidationError{
			Code: BindingCodeRebound,
			Detail: fmt.Sprintf("workspace binding in force is %s, the run pinned %s",
				current.BindingID, pinned.BindingID),
		}
	}
	if current.Revision != pinned.Revision {
		return &BindingValidationError{
			Code: BindingCodeRebound,
			Detail: fmt.Sprintf("workspace binding %s is at revision %d, the run pinned revision %d",
				current.BindingID, current.Revision, pinned.Revision),
		}
	}
	if !current.RootIdentity.Equal(pinned.RootIdentity) {
		return &BindingValidationError{
			Code: BindingCodeRebound,
			Detail: fmt.Sprintf("workspace binding %s revision %d now points at a different directory (%s, pinned %s)",
				current.BindingID, current.Revision, current.RootIdentity.String(), pinned.RootIdentity.String()),
		}
	}
	return nil
}

// checkBindingPurpose rejects unknown purposes and, for a merge, kinds that
// cannot be merged into.
func checkBindingPurpose(current WorkspaceBindingSnapshot, purpose BindingPurpose) error {
	if !purpose.Valid() {
		return &BindingValidationError{
			Code:    BindingCodeInvalidPurpose,
			Purpose: purpose,
			Detail:  fmt.Sprintf("unknown workspace binding purpose %q", string(purpose)),
		}
	}
	if purpose != BindingPurposeMerge {
		return nil
	}
	if !mergeableKind(current.Kind) {
		return &BindingValidationError{
			Code:    BindingCodeKindNotMergeable,
			Purpose: purpose,
			Detail:  fmt.Sprintf("a %s binding cannot be merged into", string(current.Kind)),
		}
	}
	return nil
}

// mergeableKind reports whether a result may be merged on a binding of this
// kind.
//
// Only a primary binding is mergeable. A primary binding is the project's one
// authoritative root: it is what a run is merged back into, and the whole point
// of the rebind check is that exactly one directory has that role at a time.
//
// A flow binding is a branch workspace: its result is merged by moving the
// branch (or by the merge of the flow's own run), not by writing files into the
// branch workspace through this path, so it is not a merge target. The same goes
// for a run binding, whose workspace is a scratch copy that is explicitly
// discarded or materialized elsewhere - never merged into.
//
// The rule is deliberately conservative: a kind that is not proven mergeable is
// refused, so widening it later (for example to a flow binding that is first
// promoted to primary) is a visible decision rather than a silent default.
func mergeableKind(kind BindingKind) bool {
	return kind == BindingKindPrimary
}
