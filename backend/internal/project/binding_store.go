package project

// Workspace binding persistence (T1.10.b group 2/2).
//
// Group 1 built the pure model: WorkspaceBindingSnapshot, CaptureBindingSnapshot
// and ValidateBinding. That model could not answer "which binding is in force for
// this project right now", because a project only had projects.workspace_root and
// projects.binding_state. Rebinding a project overwrote one column, so a Run
// created against root A could not tell that the project had been moved to root
// B - and would have silently followed the new root.
//
// This file stores bindings in the *project database* (the same SQLite file
// SQLiteProjectService already owns), so the three writers of a workspace root
// (CreateProject with a root, UpdateProject changing the root, BindWorkspaceRoot)
// commit the project rows and the binding rows in one transaction.
//
// Three facts this file exists to make true at the database level:
//
//  1. A project has at most one active primary binding. Enforced by a partial
//     unique index, not only by the read-then-write in this file, so an
//     accidental second primary is refused by SQLite itself.
//  2. Every revision a binding ever had stays readable. workspace_binding_revisions
//     is append-only (BEFORE UPDATE/DELETE triggers refuse both), which is what
//     lets a Run ask "what did binding X look like at revision 1" long after the
//     project was rebound. That question is the whole reason a pinned snapshot
//     can be validated against "the binding in force" (ValidateBinding).
//  3. The bindings survive the projects table being rewritten. SQLiteProjectService
//     persists projects by DELETE FROM projects followed by a full re-INSERT
//     (saveSnapshot), so a FOREIGN KEY from workspace_bindings to projects would
//     either fail every ordinary save (foreign_keys is on) or cascade-delete every
//     binding. There is deliberately *no* foreign key to projects; project
//     existence is checked by the application against the in-memory project map,
//     and TestBindingStoreOrdinarySaveDoesNotTouchBindings pins that decision.
//
// Archiving a project (T1.10.c) retires every binding the project owns in the
// same transaction that writes the archived project row. An archived project
// with an active binding would let an old Run keep passing the merge check, so
// the two facts commit together. Retired bindings stay readable - the revision
// history is what a pinned Run is compared against - and a restored project
// gets a brand-new primary binding instead of reviving the retired one, which
// keeps every snapshot pinned before the archive refused.
//
// Time: this is a new table in a library whose older columns (projects.created_at
// and friends) hold Unix *seconds*. Bindings use Unix *milliseconds* UTC, matching
// runstore; the older columns are left alone.
//
// Exclusivity is deliberately *not* per project: two projects may bind the same
// real directory, and §27.1 requires the merge lock key to be the real target
// root / repository identity rather than project_id. The root_key index and
// ActiveBindingsByRootKey exist so the merge path (T1.09) can find every binding
// that shares a root and serialize them on one lock.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codeflow/backend/internal/workspace"
)

// ---------------------------------------------------------------------------
// Records and errors
// ---------------------------------------------------------------------------

// BindingStateValue is the lifecycle state of a persisted binding row. It is a
// distinct type from BindingKind so a kind can never be assigned a state.
type BindingStateValue string

const (
	// BindingStateActive is the binding currently in force for its project.
	BindingStateActive BindingStateValue = "active"
	// BindingStateRetired is a binding that may no longer be used, but whose
	// revisions stay readable forever.
	BindingStateRetired BindingStateValue = "retired"
)

// Valid reports whether the state is one of the supported states.
func (s BindingStateValue) Valid() bool {
	switch s {
	case BindingStateActive, BindingStateRetired:
		return true
	default:
		return false
	}
}

// ErrBindingNotFound reports that no binding row matched the lookup.
var ErrBindingNotFound = errors.New("workspace binding not found")

// ErrBindingConflict is the class of every binding write that lost against the
// current state. Use errors.As to reach the *BindingConflictError and read the
// machine-readable Code, the expected/current revisions and Next().
var ErrBindingConflict = errors.New("workspace binding conflict")

// Stable codes reported by BindingConflictError.
const (
	// BindingConflictAlreadyExists: an active primary binding already exists for
	// the project. The caller should read it (ActivePrimaryBinding) and decide
	// whether a rebind is intended.
	BindingConflictAlreadyExists = "binding_primary_already_exists"
	// BindingConflictRevision: the revision in force is not the expected one.
	// Expected and Current are filled in.
	BindingConflictRevision = "binding_revision_conflict"
	// BindingConflictParentNotFound: a derived binding named a parent binding
	// that does not exist.
	BindingConflictParentNotFound = "binding_parent_not_found"
	// BindingConflictParentState: the parent binding exists but is not active.
	BindingConflictParentState = "binding_parent_not_active"
	// BindingConflictParentKind: the parent binding has a kind that may not have
	// this child (flow requires a primary parent; run accepts primary or flow).
	BindingConflictParentKind = "binding_parent_kind"
	// BindingConflictKind: a derived binding was requested with a kind that may
	// not be derived (primary), or with an unknown kind.
	BindingConflictKind = "binding_kind_not_derivable"
	// BindingConflictNotBound: a binding was requested for a project whose
	// binding_state forbids one (unbound, archived, deleting).
	BindingConflictNotBound = "binding_state_not_bindable"
)

// BindingConflictError reports a binding write that lost against the current
// binding state. It is wrapped with ErrBindingConflict, so errors.Is works and
// errors.As exposes the structured fields.
type BindingConflictError struct {
	// Code is one of the BindingConflict* constants.
	Code string
	// ProjectID names the project the write was about, when there is one.
	ProjectID string
	// BindingID names the binding involved, when there is one.
	BindingID string
	// Expected is the revision the caller assumed (CAS), when relevant.
	Expected int64
	// Current is the revision actually in force, when relevant.
	Current int64
	// Detail explains the conflict in prose.
	Detail string
}

func (e *BindingConflictError) Error() string {
	if e == nil {
		return "workspace binding conflict"
	}
	msg := fmt.Sprintf("workspace binding conflict (%s): %s", e.Code, e.Detail)
	if e.Expected != 0 || e.Current != 0 {
		msg += fmt.Sprintf(" (expected revision %d, current %d)", e.Expected, e.Current)
	}
	return msg
}

// Unwrap makes errors.Is(err, ErrBindingConflict) true for every conflict.
func (e *BindingConflictError) Unwrap() error { return ErrBindingConflict }

// Next suggests the caller's next move so a retry loop does not have to switch
// on Code:
//
//   - "retry"  - a concurrent writer won; re-read and re-apply.
//   - "rebind" - an active primary already exists; the workspace-root writers
//     are the intended path.
//   - "manual" - the request cannot succeed as written (bad parent, bad kind,
//     project not bindable).
func (e *BindingConflictError) Next() string {
	switch e.Code {
	case BindingConflictRevision:
		return "retry"
	case BindingConflictAlreadyExists:
		return "rebind"
	default:
		return "manual"
	}
}

// WorkspaceBindingRecord is a persisted binding row: the captured snapshot of
// its current revision plus the row's lifecycle metadata.
//
// Snapshot can be handed straight to ValidateBinding as the "current" argument,
// which is what makes the validator usable from T1.04 (run creation/dispatch)
// and T1.09 (merge) without either of them reading the table themselves.
type WorkspaceBindingRecord struct {
	Snapshot  WorkspaceBindingSnapshot
	State     BindingStateValue
	CreatedAt time.Time
	UpdatedAt time.Time
	RetiredAt *time.Time
}

// Active reports whether the binding may still be used.
func (r WorkspaceBindingRecord) Active() bool { return r.State == BindingStateActive }

// WorkspaceBindingStore is the persistence contract the workspace-root writers
// and the later consumers (T1.04/T1.09) depend on. *SQLiteProjectService
// implements it; InMemoryProjectService deliberately does not, because bindings
// only became a durable fact with this step.
type WorkspaceBindingStore interface {
	ActivePrimaryBinding(ctx context.Context, projectID string) (WorkspaceBindingRecord, error)
	EnsurePrimaryBinding(ctx context.Context, projectID string) (WorkspaceBindingRecord, error)
	GetBinding(ctx context.Context, bindingID string) (WorkspaceBindingRecord, error)
	GetBindingRevision(ctx context.Context, bindingID string, revision int64) (WorkspaceBindingSnapshot, error)
	CreateDerivedBinding(ctx context.Context, parentBindingID string, kind BindingKind, root string, baseRef string) (WorkspaceBindingRecord, error)
	RetireBinding(ctx context.Context, bindingID string, expectedRevision int64) error
	ActiveBindingsByRootKey(ctx context.Context, rootKey string) ([]WorkspaceBindingRecord, error)
}

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

// createBindingTablesSQL creates the binding tables and their indexes/triggers.
//
// Every statement is IF NOT EXISTS, so an existing project database (which has
// only projects/project_plans/project_operations) gains the tables on the next
// open without a migration table.
//
// workspace_bindings has no FOREIGN KEY to projects on purpose: see the file
// header. The one foreign key it does have is internal to the binding tables
// (revisions -> bindings), which is safe because neither table is ever bulk
// deleted.
const createBindingTablesSQL = `
CREATE TABLE IF NOT EXISTS workspace_bindings (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	kind TEXT NOT NULL CHECK (kind IN ('primary', 'flow', 'run')),
	parent_binding_id TEXT NULL REFERENCES workspace_bindings(id),
	binding_revision INTEGER NOT NULL CHECK (binding_revision >= 1),
	canonical_root TEXT NOT NULL,
	root_identity_json TEXT NOT NULL,
	root_key TEXT NOT NULL,
	base_ref TEXT NULL,
	link_policy TEXT NOT NULL,
	state TEXT NOT NULL CHECK (state IN ('active', 'retired')),
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	retired_at INTEGER NULL,
	CHECK ((kind = 'primary') = (parent_binding_id IS NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_workspace_bindings_active_primary
	ON workspace_bindings(project_id) WHERE kind = 'primary' AND state = 'active';

CREATE INDEX IF NOT EXISTS idx_workspace_bindings_root_key
	ON workspace_bindings(root_key) WHERE state = 'active';

CREATE INDEX IF NOT EXISTS idx_workspace_bindings_project
	ON workspace_bindings(project_id, kind, state);

CREATE TABLE IF NOT EXISTS workspace_binding_revisions (
	binding_id TEXT NOT NULL REFERENCES workspace_bindings(id),
	binding_revision INTEGER NOT NULL CHECK (binding_revision >= 1),
	canonical_root TEXT NOT NULL,
	root_identity_json TEXT NOT NULL,
	root_key TEXT NOT NULL,
	base_ref TEXT NULL,
	link_policy TEXT NOT NULL,
	captured_at INTEGER NOT NULL,
	PRIMARY KEY (binding_id, binding_revision)
);

CREATE TRIGGER IF NOT EXISTS trg_workspace_binding_revisions_immutable_update
BEFORE UPDATE ON workspace_binding_revisions
BEGIN
	SELECT RAISE(ABORT, 'binding_revision_immutable');
END;

CREATE TRIGGER IF NOT EXISTS trg_workspace_binding_revisions_immutable_delete
BEFORE DELETE ON workspace_binding_revisions
BEGIN
	SELECT RAISE(ABORT, 'binding_revision_immutable');
END;
`

// bindingQueryTimeout bounds every binding read/write so a caller that passes a
// background context cannot hang the write path indefinitely.
const bindingQueryTimeout = 5 * time.Second

// ---------------------------------------------------------------------------
// Row conversion
// ---------------------------------------------------------------------------

// bindingColumns is the column list of every workspace_bindings SELECT, so the
// scan order in scanBinding can never drift from a query.
const bindingColumns = `id, project_id, kind, parent_binding_id, binding_revision, canonical_root, root_identity_json, root_key, base_ref, link_policy, state, created_at, updated_at, retired_at`

// bindingRow mirrors one workspace_bindings row.
type bindingRow struct {
	id              string
	projectID       string
	kind            BindingKind
	parentBindingID sql.NullString
	revision        int64
	canonicalRoot   string
	identityJSON    string
	rootKey         string
	baseRef         sql.NullString
	linkPolicy      string
	state           BindingStateValue
	createdAt       int64
	updatedAt       int64
	retiredAt       sql.NullInt64
}

func scanBinding(scanner interface{ Scan(dest ...any) error }) (bindingRow, error) {
	var row bindingRow
	var kind, state string
	if err := scanner.Scan(
		&row.id,
		&row.projectID,
		&kind,
		&row.parentBindingID,
		&row.revision,
		&row.canonicalRoot,
		&row.identityJSON,
		&row.rootKey,
		&row.baseRef,
		&row.linkPolicy,
		&state,
		&row.createdAt,
		&row.updatedAt,
		&row.retiredAt,
	); err != nil {
		return bindingRow{}, err
	}
	row.kind = BindingKind(kind)
	row.state = BindingStateValue(state)
	return row, nil
}

// record rebuilds the snapshot of the binding's *current* revision. The
// revision columns live on the binding row itself (the history table is the
// append-only record of the same values), so no second query is needed.
func (row bindingRow) record() (WorkspaceBindingRecord, error) {
	snapshot, err := snapshotFromBindingRow(row)
	if err != nil {
		return WorkspaceBindingRecord{}, err
	}
	record := WorkspaceBindingRecord{
		Snapshot:  snapshot,
		State:     row.state,
		CreatedAt: time.UnixMilli(row.createdAt).UTC(),
		UpdatedAt: time.UnixMilli(row.updatedAt).UTC(),
	}
	if row.retiredAt.Valid {
		retired := time.UnixMilli(row.retiredAt.Int64).UTC()
		record.RetiredAt = &retired
	}
	return record, nil
}

// snapshotFromBindingRow rebuilds a snapshot from a current-revision row.
func snapshotFromBindingRow(row bindingRow) (WorkspaceBindingSnapshot, error) {
	identity, err := identityFromJSON(row.identityJSON)
	if err != nil {
		return WorkspaceBindingSnapshot{}, err
	}
	snapshot := WorkspaceBindingSnapshot{
		BindingID:       row.id,
		ProjectID:       row.projectID,
		Kind:            row.kind,
		ParentBindingID: row.parentBindingID.String,
		Revision:        row.revision,
		CanonicalRoot:   row.canonicalRoot,
		RootIdentity:    identity,
		BaseRef:         row.baseRef.String,
		LinkPolicy:      row.linkPolicy,
		CapturedAt:      time.UnixMilli(row.updatedAt).UTC(),
	}
	if err := snapshot.Validate(); err != nil {
		return WorkspaceBindingSnapshot{}, err
	}
	return snapshot, nil
}

// identityFromJSON decodes a stored root identity. An unreadable value is an
// error rather than a zero identity: a binding whose identity cannot be read
// would compare as "a different directory" and refuse every run on it, hiding
// the real cause.
func identityFromJSON(raw string) (workspace.RootIdentity, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return workspace.RootIdentity{}, fmt.Errorf("workspace binding: stored root identity is empty")
	}
	var identity workspace.RootIdentity
	if err := json.Unmarshal([]byte(raw), &identity); err != nil {
		return workspace.RootIdentity{}, fmt.Errorf("workspace binding: decode root identity: %w", err)
	}
	if identity.IsZero() {
		return workspace.RootIdentity{}, fmt.Errorf("workspace binding: stored root identity is empty")
	}
	return identity, nil
}

func identityToJSON(identity workspace.RootIdentity) (string, error) {
	data, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("workspace binding: encode root identity: %w", err)
	}
	return string(data), nil
}

// rootKeyOf renders the identity that keys the root index. It is
// RootIdentity.String(), which contains no path, so the key stays stable
// however the directory was spelled when it was reached.
func rootKeyOf(snapshot WorkspaceBindingSnapshot) string {
	return snapshot.RootIdentity.String()
}

// queryRower is the smallest handle shape the binding readers need, so they
// accept both *sql.DB and *sql.Tx.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// queryBindingRow runs a single-row binding query and maps "no rows" to
// ErrBindingNotFound.
func queryBindingRow(ctx context.Context, q queryRower, query string, args ...any) (bindingRow, error) {
	row, err := scanBinding(q.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return bindingRow{}, ErrBindingNotFound
	}
	if err != nil {
		return bindingRow{}, fmt.Errorf("query workspace binding: %w", err)
	}
	return row, nil
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// ActivePrimaryBinding returns the project's one active primary binding.
//
// A project that is bound but predates this table has no row: that is
// ErrBindingNotFound, and EnsurePrimaryBinding is the backfill for it.
func (s *SQLiteProjectService) ActivePrimaryBinding(ctx context.Context, projectID string) (WorkspaceBindingRecord, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return WorkspaceBindingRecord{}, fmt.Errorf("active primary binding: project id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, bindingQueryTimeout)
	defer cancel()

	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	row, err := queryBindingRow(ctx, s.db, `SELECT `+bindingColumns+` FROM workspace_bindings
		WHERE project_id = ? AND kind = 'primary' AND state = 'active'`, projectID)
	if err != nil {
		return WorkspaceBindingRecord{}, err
	}
	return row.record()
}

// GetBinding returns the current revision of one binding, whatever its state.
func (s *SQLiteProjectService) GetBinding(ctx context.Context, bindingID string) (WorkspaceBindingRecord, error) {
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return WorkspaceBindingRecord{}, fmt.Errorf("get binding: binding id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, bindingQueryTimeout)
	defer cancel()

	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	row, err := queryBindingRow(ctx, s.db, `SELECT `+bindingColumns+` FROM workspace_bindings WHERE id = ?`, bindingID)
	if err != nil {
		return WorkspaceBindingRecord{}, err
	}
	return row.record()
}

// GetBindingRevision returns what a binding looked like at one revision, read
// from the append-only history. This is the query a Run uses with its pinned
// (binding_id, revision) to recover the snapshot it froze.
func (s *SQLiteProjectService) GetBindingRevision(ctx context.Context, bindingID string, revision int64) (WorkspaceBindingSnapshot, error) {
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return WorkspaceBindingSnapshot{}, fmt.Errorf("get binding revision: binding id is required")
	}
	if revision < 1 {
		return WorkspaceBindingSnapshot{}, fmt.Errorf("get binding revision: revision must be >= 1, got %d", revision)
	}
	ctx, cancel := context.WithTimeout(ctx, bindingQueryTimeout)
	defer cancel()

	s.dbMu.RLock()
	defer s.dbMu.RUnlock()

	// kind/project/parent come from the binding row: they are invariant over a
	// binding's life (a new revision changes the root, never the owner, the kind
	// or the parent).
	row, err := queryBindingRow(ctx, s.db, `SELECT `+bindingColumns+` FROM workspace_bindings WHERE id = ?`, bindingID)
	if err != nil {
		return WorkspaceBindingSnapshot{}, err
	}

	var identityJSON, canonicalRoot, linkPolicy string
	var baseRef sql.NullString
	var capturedAt int64
	err = s.db.QueryRowContext(ctx, `SELECT canonical_root, root_identity_json, base_ref, link_policy, captured_at
		FROM workspace_binding_revisions WHERE binding_id = ? AND binding_revision = ?`, bindingID, revision).Scan(
		&canonicalRoot, &identityJSON, &baseRef, &linkPolicy, &capturedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceBindingSnapshot{}, fmt.Errorf("%w: binding %s revision %d", ErrBindingNotFound, bindingID, revision)
	}
	if err != nil {
		return WorkspaceBindingSnapshot{}, fmt.Errorf("get binding revision: %w", err)
	}

	identity, err := identityFromJSON(identityJSON)
	if err != nil {
		return WorkspaceBindingSnapshot{}, err
	}
	snapshot := WorkspaceBindingSnapshot{
		BindingID:       bindingID,
		ProjectID:       row.projectID,
		Kind:            row.kind,
		ParentBindingID: row.parentBindingID.String,
		Revision:        revision,
		CanonicalRoot:   canonicalRoot,
		RootIdentity:    identity,
		BaseRef:         baseRef.String,
		LinkPolicy:      linkPolicy,
		CapturedAt:      time.UnixMilli(capturedAt).UTC(),
	}
	if err := snapshot.Validate(); err != nil {
		return WorkspaceBindingSnapshot{}, err
	}
	return snapshot, nil
}

// ActiveBindingsByRootKey returns every active binding (any kind, any project)
// whose directory has this OS identity.
//
// It exists for the merge lock: §27.1 requires the lock key to be the real
// target root / repository identity, so two projects writing into the same
// directory must find each other here instead of serializing per project.
func (s *SQLiteProjectService) ActiveBindingsByRootKey(ctx context.Context, rootKey string) ([]WorkspaceBindingRecord, error) {
	rootKey = strings.TrimSpace(rootKey)
	if rootKey == "" {
		return nil, fmt.Errorf("active bindings by root key: root key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, bindingQueryTimeout)
	defer cancel()

	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+bindingColumns+` FROM workspace_bindings
		WHERE root_key = ? AND state = 'active'
		ORDER BY project_id ASC, id ASC`, rootKey)
	if err != nil {
		return nil, fmt.Errorf("query active bindings by root key: %w", err)
	}
	defer rows.Close()

	var records []WorkspaceBindingRecord
	for rows.Next() {
		row, err := scanBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan active binding: %w", err)
		}
		record, err := row.record()
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active bindings: %w", err)
	}
	return records, nil
}

// ---------------------------------------------------------------------------
// Pending writes
// ---------------------------------------------------------------------------

// bindingWrite is one pending binding change, fully computed (ids assigned,
// snapshot captured) before any SQL runs. It is what the workspace-root writers
// hand to saveSnapshotWith, so the filesystem I/O that reads the directory
// identity happens outside the SQL transaction while the rows are written inside
// it - one commit covers the projects rewrite and the binding rows together.
type bindingWrite struct {
	// create inserts a brand-new binding at revision 1 plus its history row.
	create *bindingCreate
	// appendRevision advances an existing binding by one revision.
	appendRevision *bindingRevisionBump
	// refreshPath rewrites the current row's path without moving the revision.
	refreshPath *bindingRefreshPath
	// retire marks an existing binding retired.
	retire *bindingRetire
	// retireProject retires every active binding of one project at once; it is
	// the archive path (T1.10.c).
	retireProject *bindingRetireProject
}

// bindingCreate inserts a new binding and the first row of its history.
type bindingCreate struct {
	BindingID       string
	ProjectID       string
	ParentBindingID string
	Snapshot        WorkspaceBindingSnapshot
	Now             int64
}

// bindingRevisionBump advances a binding by one revision. ExpectedRevision is
// the revision observed before the write and makes the UPDATE a CAS, so two
// concurrent rebinds cannot both win and silently skip a revision number.
type bindingRevisionBump struct {
	BindingID        string
	ExpectedRevision int64
	Snapshot         WorkspaceBindingSnapshot
	Now              int64
}

// bindingRefreshPath rewrites the canonical path of a binding whose directory
// did not change but whose path did (the root was renamed, or the project was
// re-bound through another spelling of the same directory).
//
// It deliberately does not touch binding_revision and does not append a history
// row: no revision of this binding ever described a different directory, so
// there is nothing new to record. Leaving the stale path in place would be
// worse than a missing feature, though: the stored path is what Recheck re-stats
// on every dispatch, so a binding that no longer names its own directory would
// fail closed forever.
type bindingRefreshPath struct {
	BindingID        string
	ExpectedRevision int64
	CanonicalRoot    string
	Now              int64
}

// bindingRetire marks a binding retired at the revision the caller expected.
type bindingRetire struct {
	BindingID        string
	ExpectedRevision int64
	Now              int64
}

// bindingRetireProject retires every active binding of one project: the primary
// and all of its flow/run descendants.
//
// It exists for archiving. Retiring one binding at a time cannot express the
// archive invariant ("an archived project has no active binding at all"), and a
// per-binding loop outside the transaction could leave half a project retired if
// one row lost a race. This is one statement, inside the same transaction that
// writes the archived project row.
//
// There is deliberately no CAS on BindingID/Revision: the caller is not retiring
// "the binding it read" but "whatever is active when the project is archived",
// and a binding created concurrently must not survive the archive. The retires
// are idempotent, so a repeated archive writes nothing.
type bindingRetireProject struct {
	ProjectID string
	Now       int64
}

// bindingWriteFaultFunc is a test-only interposition on a pending binding
// change. It runs inside the caller's transaction, before the change, and can
// either fail it or let it proceed.
type bindingWriteFaultFunc func(ctx context.Context, tx *sql.Tx) error

// Apply runs the pending change inside the caller's transaction.
//
// fault, when non-nil, runs *in place of* the change (see
// SQLiteProjectService.failNextBindingWrite). It is applied here rather than by
// the caller so it lands inside whatever transaction the write was given: if a
// caller ever moved the binding write into its own transaction, the fault would
// surface after the project rows had already committed, which is exactly what
// TestBindingStoreBindingWriteFailureRollsBackProjectAndBinding is there to
// detect.
func (w *bindingWrite) Apply(ctx context.Context, tx *sql.Tx, fault bindingWriteFaultFunc) error {
	if w == nil {
		return nil
	}
	if fault != nil {
		return fault(ctx, tx)
	}
	switch {
	case w.create != nil:
		return w.create.apply(ctx, tx)
	case w.appendRevision != nil:
		return w.appendRevision.apply(ctx, tx)
	case w.refreshPath != nil:
		return w.refreshPath.apply(ctx, tx)
	case w.retire != nil:
		return w.retire.apply(ctx, tx)
	case w.retireProject != nil:
		return w.retireProject.apply(ctx, tx)
	default:
		return nil
	}
}

func (c *bindingCreate) apply(ctx context.Context, tx *sql.Tx) error {
	snapshot := c.Snapshot
	if err := snapshot.Validate(); err != nil {
		return err
	}
	identityJSON, err := identityToJSON(snapshot.RootIdentity)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspace_bindings (id, project_id, kind, parent_binding_id, binding_revision, canonical_root, root_identity_json, root_key, base_ref, link_policy, state, created_at, updated_at, retired_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
	`, c.BindingID, c.ProjectID, string(snapshot.Kind), nullProjectString(snapshot.ParentBindingID), snapshot.Revision, snapshot.CanonicalRoot, identityJSON, rootKeyOf(snapshot), nullProjectString(snapshot.BaseRef), snapshot.LinkPolicy, string(BindingStateActive), c.Now, c.Now); err != nil {
		return classifyBindingWriteError(err, c.ProjectID, c.BindingID)
	}
	return insertBindingRevision(ctx, tx, snapshot, identityJSON)
}

func (b *bindingRevisionBump) apply(ctx context.Context, tx *sql.Tx) error {
	snapshot := b.Snapshot
	if err := snapshot.Validate(); err != nil {
		return err
	}
	identityJSON, err := identityToJSON(snapshot.RootIdentity)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE workspace_bindings
		SET binding_revision = ?, canonical_root = ?, root_identity_json = ?, root_key = ?, base_ref = ?, updated_at = ?
		WHERE id = ? AND binding_revision = ? AND state = 'active'
	`, snapshot.Revision, snapshot.CanonicalRoot, identityJSON, rootKeyOf(snapshot), nullProjectString(snapshot.BaseRef), b.Now, b.BindingID, b.ExpectedRevision)
	if err != nil {
		return classifyBindingWriteError(err, "", b.BindingID)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update workspace binding %s: %w", b.BindingID, err)
	}
	if affected != 1 {
		current, readErr := currentBindingRevision(ctx, tx, b.BindingID)
		if readErr != nil {
			return readErr
		}
		return &BindingConflictError{
			Code:      BindingConflictRevision,
			BindingID: b.BindingID,
			Expected:  b.ExpectedRevision,
			Current:   current,
			Detail: fmt.Sprintf("workspace binding %s is not at the expected revision while a new revision was written",
				b.BindingID),
		}
	}
	return insertBindingRevision(ctx, tx, snapshot, identityJSON)
}

func (p *bindingRefreshPath) apply(ctx context.Context, tx *sql.Tx) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE workspace_bindings
		SET canonical_root = ?, updated_at = ?
		WHERE id = ? AND state = 'active' AND binding_revision = ?
	`, p.CanonicalRoot, p.Now, p.BindingID, p.ExpectedRevision)
	if err != nil {
		return classifyBindingWriteError(err, "", p.BindingID)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("refresh workspace binding %s: %w", p.BindingID, err)
	}
	if affected != 1 {
		current, readErr := currentBindingRevision(ctx, tx, p.BindingID)
		if readErr != nil {
			return readErr
		}
		if current < 0 {
			return fmt.Errorf("%w: binding %s", ErrBindingNotFound, p.BindingID)
		}
		return &BindingConflictError{
			Code:      BindingConflictRevision,
			BindingID: p.BindingID,
			Expected:  p.ExpectedRevision,
			Current:   current,
			Detail:    fmt.Sprintf("workspace binding %s moved while its path was refreshed", p.BindingID),
		}
	}
	return nil
}

func (r *bindingRetire) apply(ctx context.Context, tx *sql.Tx) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE workspace_bindings
		SET state = ?, retired_at = ?, updated_at = ?
		WHERE id = ? AND state = 'active' AND binding_revision = ?
	`, string(BindingStateRetired), r.Now, r.Now, r.BindingID, r.ExpectedRevision)
	if err != nil {
		return classifyBindingWriteError(err, "", r.BindingID)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("retire workspace binding %s: %w", r.BindingID, err)
	}
	if affected == 1 {
		return nil
	}
	current, readErr := currentBindingRevision(ctx, tx, r.BindingID)
	if readErr != nil {
		return readErr
	}
	if current < 0 {
		return fmt.Errorf("%w: binding %s", ErrBindingNotFound, r.BindingID)
	}
	return &BindingConflictError{
		Code:      BindingConflictRevision,
		BindingID: r.BindingID,
		Expected:  r.ExpectedRevision,
		Current:   current,
		Detail: fmt.Sprintf("workspace binding %s could not be retired at revision %d",
			r.BindingID, r.ExpectedRevision),
	}
}

func (p *bindingRetireProject) apply(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE workspace_bindings
		SET state = ?, retired_at = ?, updated_at = ?
		WHERE project_id = ? AND state = 'active'
	`, string(BindingStateRetired), p.Now, p.Now, p.ProjectID); err != nil {
		return classifyBindingWriteError(err, p.ProjectID, "")
	}
	return nil
}

// currentBindingRevision reads a binding's revision in force inside a
// transaction. A missing or retired binding has no revision in force, which is
// reported as -1 so callers can tell "gone" from "revision 0".
func currentBindingRevision(ctx context.Context, tx *sql.Tx, bindingID string) (int64, error) {
	var revision int64
	var state string
	err := tx.QueryRowContext(ctx, `SELECT binding_revision, state FROM workspace_bindings WHERE id = ?`, bindingID).Scan(&revision, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, nil
	}
	if err != nil {
		return -1, fmt.Errorf("read workspace binding %s: %w", bindingID, err)
	}
	if BindingStateValue(state) != BindingStateActive {
		return -1, nil
	}
	return revision, nil
}

// insertBindingRevision appends one history row. The triggers on the table make
// the row unchangeable afterwards, which is what keeps GetBindingRevision(id, 1)
// returning root A after the project was rebound to root B.
func insertBindingRevision(ctx context.Context, tx *sql.Tx, snapshot WorkspaceBindingSnapshot, identityJSON string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspace_binding_revisions (binding_id, binding_revision, canonical_root, root_identity_json, root_key, base_ref, link_policy, captured_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, snapshot.BindingID, snapshot.Revision, snapshot.CanonicalRoot, identityJSON, rootKeyOf(snapshot), nullProjectString(snapshot.BaseRef), snapshot.LinkPolicy, snapshot.CapturedAt.UTC().UnixMilli()); err != nil {
		return classifyBindingWriteError(err, snapshot.ProjectID, snapshot.BindingID)
	}
	return nil
}

// classifyBindingWriteError turns the SQLite constraint messages the binding
// tables raise into the typed conflicts callers branch on. Anything else is
// returned wrapped but untyped: a real storage failure must stay a 5xx rather
// than become a 409.
func classifyBindingWriteError(err error, projectID, bindingID string) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "ux_workspace_bindings_active_primary") ||
		(strings.Contains(message, "unique constraint failed") && strings.Contains(message, "workspace_bindings")) {
		return &BindingConflictError{
			Code:      BindingConflictAlreadyExists,
			ProjectID: projectID,
			BindingID: bindingID,
			Detail:    "the project already has an active primary workspace binding",
		}
	}
	return fmt.Errorf("write workspace binding %s: %w", bindingID, err)
}

// ---------------------------------------------------------------------------
// Planning a binding change (filesystem I/O happens here, outside any tx)
// ---------------------------------------------------------------------------

// planPrimaryBinding decides what the binding table needs for a project whose
// workspace root may have changed, and captures the snapshot the transaction
// will write.
//
// rootChanged is the caller's answer to "did this write change the project's
// canonical workspace root?" (a new project with a root counts as changed). When
// it is false nothing is planned at all, so an ordinary save - a title change,
// adding a plan, a status update - never reads a directory and can never fail
// because the workspace is offline. It also means such a save leaves every
// binding row byte-identical.
//
// When the root did change, the decision is:
//
//   - no active primary yet: capture the root and create revision 1 (this is
//     also the lazy backfill for a project that predates this table);
//   - an active primary whose canonical root already is the new root: no write;
//   - an active primary for a different path whose OS identity equals the
//     identity in force: the two spellings name the same directory (for example
//     a junction path and its target), so nothing is written and the revision
//     does not move - binding the same directory twice is not a rebind;
//   - otherwise: revision + 1 on the same binding id.
//
// A project with no workspace root, or whose binding_state is not bound
// (deleting/archived), gets no binding write: archiving retires bindings through
// planArchiveBindings, and nothing else may write one for a project that is no
// longer bound.
func (s *SQLiteProjectService) planPrimaryBinding(ctx context.Context, project *Project, rootChanged bool) (*bindingWrite, error) {
	if project == nil || !rootChanged || project.BindingState != BindingStateBound {
		return nil, nil
	}
	root := strings.TrimSpace(project.WorkspaceRoot)
	if root == "" {
		return nil, nil
	}

	existing, err := s.ActivePrimaryBinding(ctx, project.ID)
	if errors.Is(err, ErrBindingNotFound) {
		snapshot, err := s.capturePrimarySnapshot(project.ID, "", 1, root, "")
		if err != nil {
			return nil, err
		}
		return &bindingWrite{create: &bindingCreate{
			BindingID: snapshot.BindingID,
			ProjectID: project.ID,
			Snapshot:  snapshot,
			Now:       snapshot.CapturedAt.UTC().UnixMilli(),
		}}, nil
	}
	if err != nil {
		return nil, err
	}
	if existing.Snapshot.CanonicalRoot == root {
		return nil, nil
	}

	snapshot, err := s.capturePrimarySnapshot(project.ID, existing.Snapshot.BindingID, existing.Snapshot.Revision+1, root, existing.Snapshot.BaseRef)
	if err != nil {
		return nil, err
	}
	if snapshot.RootIdentity.Equal(existing.Snapshot.RootIdentity) {
		// The same directory reached by another spelling. No revision moves,
		// but the stored path must follow the project's root: it is the path
		// Recheck re-stats on every dispatch, and a binding that no longer names
		// its own directory would fail closed forever.
		now := snapshot.CapturedAt.UTC().UnixMilli()
		return &bindingWrite{
			refreshPath: &bindingRefreshPath{
				BindingID:        existing.Snapshot.BindingID,
				ExpectedRevision: existing.Snapshot.Revision,
				CanonicalRoot:    snapshot.CanonicalRoot,
				Now:              now,
			},
		}, nil
	}
	return &bindingWrite{appendRevision: &bindingRevisionBump{
		BindingID:        existing.Snapshot.BindingID,
		ExpectedRevision: existing.Snapshot.Revision,
		Snapshot:         snapshot,
		Now:              snapshot.CapturedAt.UTC().UnixMilli(),
	}}, nil
}

// planArchiveBindings decides what the binding table needs when a project is
// archived.
//
// Archiving is the one project write that retires bindings instead of creating
// or advancing them: an archived project must not have a binding a Run can
// still merge on. The plan is deliberately unconditional - it does not read the
// binding table and cannot fail - because the invariant is "no active binding
// remains", and the SQL states it directly: UPDATE ... WHERE state = 'active'
// retires every row that is active now, including one written by a concurrent
// writer, and touches nothing when there is none. A read-then-write plan would
// have to reason about which rows it saw, and a concurrent bind between the read
// and the transaction would survive the archive.
//
// The write is a no-op when the project has no active binding, so archiving an
// unbound or already-archived project leaves the binding tables byte-identical.
func (s *SQLiteProjectService) planArchiveBindings(ctx context.Context, projectID string) (*bindingWrite, error) {
	_ = ctx
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, nil
	}
	return &bindingWrite{retireProject: &bindingRetireProject{
		ProjectID: projectID,
		Now:       time.Now().UTC().UnixMilli(),
	}}, nil
}

// capturePrimarySnapshot canonicalizes the root under the service's root policy
// and captures the OS identity of the directory it resolves to. bindingID is
// empty for a new binding, in which case a fresh UUID is assigned.
func (s *SQLiteProjectService) capturePrimarySnapshot(projectID, bindingID string, revision int64, root, baseRef string) (WorkspaceBindingSnapshot, error) {
	if strings.TrimSpace(bindingID) == "" {
		bindingID = uuid.New().String()
	}
	return s.captureBindingSnapshot(BindingSnapshotInput{
		BindingID: bindingID,
		ProjectID: projectID,
		Kind:      BindingKindPrimary,
		Revision:  revision,
		Root:      root,
		BaseRef:   baseRef,
	})
}

// captureBindingSnapshot is CaptureBindingSnapshot under the service's own root
// policy, including the temporary migration switch
// (SetAllowUnrestrictedWorkspaceRoots): a root the InMemory layer accepted must
// still be capturable, or every write to it would fail after this step. Group
// 1's CaptureBindingSnapshot takes only allowedRoots, so the unrestricted case
// is expressed here by capturing with the canonicalized root as its own
// allow-list entry, which reproduces "any existing directory is allowed" while
// keeping the final-path resolution and the identity read identical.
func (s *SQLiteProjectService) captureBindingSnapshot(in BindingSnapshotInput) (WorkspaceBindingSnapshot, error) {
	s.mu.RLock()
	allowedRoots := append([]string(nil), s.allowedRoots...)
	allowUnrestricted := s.allowUnrestrictedRoots
	s.mu.RUnlock()

	if !allowUnrestricted {
		return CaptureBindingSnapshot(in, allowedRoots)
	}
	canonical, err := canonicalizeWorkspaceRoot(in.Root, allowedRoots, true)
	if err != nil {
		return WorkspaceBindingSnapshot{}, err
	}
	if canonical == "" {
		return WorkspaceBindingSnapshot{}, fmt.Errorf("workspace binding: workspace root is required")
	}
	// The canonical root is inside its own allow-list, so the capture below
	// applies exactly the same path/identity rules as the restricted case.
	unrestricted := BindingSnapshotInput{
		BindingID:       in.BindingID,
		ProjectID:       in.ProjectID,
		Kind:            in.Kind,
		ParentBindingID: in.ParentBindingID,
		Revision:        in.Revision,
		Root:            canonical,
		BaseRef:         in.BaseRef,
	}
	return CaptureBindingSnapshot(unrestricted, []string{canonical})
}

// ---------------------------------------------------------------------------
// Writes driven by callers
// ---------------------------------------------------------------------------

// EnsurePrimaryBinding returns the project's active primary binding, creating it
// (revision 1) for a project that is bound but has no binding row yet - a project
// that predates this table, or one whose creation could not write the binding.
//
// The backfill is lazy and on demand rather than a bulk pass at startup: at
// startup the workspace directory may be offline, and a capture failure would
// have to be recorded as "this project has no binding", which is a wrong answer
// that outlives the network share being down.
//
// A project that is not bound (unbound/archived/deleting, or with no workspace
// root) is refused with a *BindingConflictError: it never had a root, so there is
// nothing to backfill. Concurrent calls are safe - the partial unique index lets
// exactly one INSERT win and the loser re-reads the winner's row.
func (s *SQLiteProjectService) EnsurePrimaryBinding(ctx context.Context, projectID string) (WorkspaceBindingRecord, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return WorkspaceBindingRecord{}, fmt.Errorf("ensure primary binding: project id is required")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Read the project through snapshotState (a clone under the InMemory lock)
	// rather than the live pointer, so the root read here cannot race a
	// concurrent project write.
	state := s.snapshotState()
	project, ok := state.projects[projectID]
	if !ok {
		return WorkspaceBindingRecord{}, fmt.Errorf("project not found")
	}
	if project.BindingState != BindingStateBound || strings.TrimSpace(project.WorkspaceRoot) == "" {
		return WorkspaceBindingRecord{}, &BindingConflictError{
			Code:      BindingConflictNotBound,
			ProjectID: projectID,
			Detail: fmt.Sprintf("project %s is not bound (binding_state=%s)",
				projectID, string(project.BindingState)),
		}
	}

	if existing, err := s.ActivePrimaryBinding(ctx, projectID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrBindingNotFound) {
		return WorkspaceBindingRecord{}, err
	}

	// Capture before the transaction: the identity read is filesystem I/O.
	snapshot, err := s.capturePrimarySnapshot(projectID, "", 1, project.WorkspaceRoot, "")
	if err != nil {
		return WorkspaceBindingRecord{}, err
	}

	err = s.saveSnapshotWith(ctx, state, &bindingWrite{create: &bindingCreate{
		BindingID: snapshot.BindingID,
		ProjectID: projectID,
		Snapshot:  snapshot,
		Now:       snapshot.CapturedAt.UTC().UnixMilli(),
	}})
	if err != nil {
		var conflict *BindingConflictError
		if errors.As(err, &conflict) && conflict.Code == BindingConflictAlreadyExists {
			return s.ActivePrimaryBinding(ctx, projectID)
		}
		return WorkspaceBindingRecord{}, err
	}
	return s.ActivePrimaryBinding(ctx, projectID)
}

// CreateDerivedBinding creates a flow or run binding under an active parent.
//
// Rules (§19.1, §27.1): the parent must exist, be active and belong to the same
// project; a flow requires a primary parent and a run accepts a primary or a
// flow. The derived binding starts at revision 1 with its own id and never takes
// part in the one-active-primary constraint, so a primary can have many flow and
// run bindings at once.
//
// The project is taken from the parent, never from the caller, so a derived
// binding cannot be attached to another project's history.
func (s *SQLiteProjectService) CreateDerivedBinding(ctx context.Context, parentBindingID string, kind BindingKind, root string, baseRef string) (WorkspaceBindingRecord, error) {
	parentBindingID = strings.TrimSpace(parentBindingID)
	if parentBindingID == "" {
		return WorkspaceBindingRecord{}, &BindingConflictError{
			Code:   BindingConflictParentNotFound,
			Detail: "a derived workspace binding requires a parent binding",
		}
	}
	switch kind {
	case BindingKindFlow, BindingKindRun:
	case BindingKindPrimary:
		return WorkspaceBindingRecord{}, &BindingConflictError{
			Code:   BindingConflictKind,
			Detail: "a primary binding cannot be derived; primary bindings are written by the workspace-root writers",
		}
	default:
		return WorkspaceBindingRecord{}, &BindingConflictError{
			Code:   BindingConflictKind,
			Detail: fmt.Sprintf("unknown workspace binding kind %q", string(kind)),
		}
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	parent, err := s.GetBinding(ctx, parentBindingID)
	if err != nil {
		if errors.Is(err, ErrBindingNotFound) {
			return WorkspaceBindingRecord{}, &BindingConflictError{
				Code:      BindingConflictParentNotFound,
				BindingID: parentBindingID,
				Detail:    fmt.Sprintf("parent workspace binding %s does not exist", parentBindingID),
			}
		}
		return WorkspaceBindingRecord{}, err
	}
	if parent.State != BindingStateActive {
		return WorkspaceBindingRecord{}, &BindingConflictError{
			Code:      BindingConflictParentState,
			ProjectID: parent.Snapshot.ProjectID,
			BindingID: parentBindingID,
			Detail:    fmt.Sprintf("parent workspace binding %s is %s", parentBindingID, string(parent.State)),
		}
	}
	if kind == BindingKindFlow && parent.Snapshot.Kind != BindingKindPrimary {
		return WorkspaceBindingRecord{}, &BindingConflictError{
			Code:      BindingConflictParentKind,
			ProjectID: parent.Snapshot.ProjectID,
			BindingID: parentBindingID,
			Detail: fmt.Sprintf("a flow binding requires a primary parent, got %s",
				string(parent.Snapshot.Kind)),
		}
	}

	// Capture before the transaction: the identity read is filesystem I/O.
	snapshot, err := s.captureBindingSnapshot(BindingSnapshotInput{
		BindingID:       uuid.New().String(),
		ProjectID:       parent.Snapshot.ProjectID,
		Kind:            kind,
		ParentBindingID: parentBindingID,
		Revision:        1,
		Root:            root,
		BaseRef:         baseRef,
	})
	if err != nil {
		return WorkspaceBindingRecord{}, err
	}

	err = s.saveSnapshotWith(ctx, s.snapshotState(), &bindingWrite{create: &bindingCreate{
		BindingID:       snapshot.BindingID,
		ProjectID:       snapshot.ProjectID,
		ParentBindingID: parentBindingID,
		Snapshot:        snapshot,
		Now:             snapshot.CapturedAt.UTC().UnixMilli(),
	}})
	if err != nil {
		return WorkspaceBindingRecord{}, err
	}
	return s.GetBinding(ctx, snapshot.BindingID)
}

// RetireBinding retires a binding at the revision the caller expected.
//
// Retiring is idempotent: a binding already retired at the expected revision
// returns nil, so a retried retire does not have to tell "I did it" from
// "someone did it". Any other mismatch is a *BindingConflictError with Code
// BindingConflictRevision and Expected/Current filled in.
//
// Retiring the primary is allowed and is how a project gets a *new* primary
// binding: the partial unique index only constrains active rows, so once the old
// primary is retired the workspace-root writers may create another one (with a
// new binding id, which makes runs pinned to the retired binding fail closed
// with binding_rebound).
func (s *SQLiteProjectService) RetireBinding(ctx context.Context, bindingID string, expectedRevision int64) error {
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return fmt.Errorf("retire binding: binding id is required")
	}
	if expectedRevision < 1 {
		return fmt.Errorf("retire binding: expected revision must be >= 1, got %d", expectedRevision)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	current, err := s.GetBinding(ctx, bindingID)
	if err != nil {
		return err
	}
	if current.State == BindingStateRetired {
		if current.Snapshot.Revision == expectedRevision {
			return nil
		}
		return &BindingConflictError{
			Code:      BindingConflictRevision,
			BindingID: bindingID,
			Expected:  expectedRevision,
			Current:   current.Snapshot.Revision,
			Detail:    fmt.Sprintf("workspace binding %s is already retired", bindingID),
		}
	}
	if current.Snapshot.Revision != expectedRevision {
		return &BindingConflictError{
			Code:      BindingConflictRevision,
			BindingID: bindingID,
			Expected:  expectedRevision,
			Current:   current.Snapshot.Revision,
			Detail:    fmt.Sprintf("workspace binding %s is not at revision %d", bindingID, expectedRevision),
		}
	}

	return s.saveSnapshotWith(ctx, s.snapshotState(), &bindingWrite{retire: &bindingRetire{
		BindingID:        bindingID,
		ExpectedRevision: expectedRevision,
		Now:              time.Now().UTC().UnixMilli(),
	}})
}
