package project

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/dbx"
	"github.com/codeflow/backend/internal/workspace"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// bindingEnv is a real file-backed project database plus the two workspace
// directories most cases need. Everything is under t.TempDir() so the binding
// store is exercised against real SQLite and real directory identities, never a
// fake.
type bindingEnv struct {
	t         *testing.T
	dbPath    string
	rootA     string
	rootB     string
	svc       *SQLiteProjectService
	allowed   []string
	projectID string
}

func newBindingEnv(t *testing.T) *bindingEnv {
	t.Helper()
	dir := t.TempDir()
	env := &bindingEnv{
		t:       t,
		dbPath:  filepath.Join(dir, "project.db"),
		rootA:   mkdirAll(t, filepath.Join(dir, "root-a")),
		rootB:   mkdirAll(t, filepath.Join(dir, "root-b")),
		allowed: []string{dir},
	}
	env.open()
	t.Cleanup(func() { env.close() })
	return env
}

func (env *bindingEnv) open() {
	env.t.Helper()
	svc, err := NewSQLiteProjectService(env.dbPath)
	if err != nil {
		env.t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	svc.SetAllowedWorkspaceRoots(env.allowed)
	env.svc = svc
}

func (env *bindingEnv) close() {
	env.t.Helper()
	if env.svc != nil {
		if err := env.svc.Close(); err != nil {
			env.t.Fatalf("Close failed: %v", err)
		}
		env.svc = nil
	}
}

// reopen closes and reopens the database, which is how every durability
// assertion is phrased.
func (env *bindingEnv) reopen() {
	env.t.Helper()
	env.close()
	env.open()
}

func (env *bindingEnv) createBoundProject(title, root string) *Project {
	env.t.Helper()
	project, err := env.svc.CreateProject(context.Background(), &ProjectCreateRequest{
		Title:         title,
		WorkspaceRoot: root,
	})
	if err != nil {
		env.t.Fatalf("CreateProject(%s) failed: %v", title, err)
	}
	if env.projectID == "" {
		env.projectID = project.ID
	}
	return project
}

// seedLegacyBoundProject writes a bound project straight into the projects
// table, with no binding rows at all, and reloads it into memory. That is
// exactly what a database written by the previous release looks like: the row
// says bound and names a root, but workspace_bindings has never heard of it.
//
// It has to go through SQL rather than the API because the API now always
// writes a binding, and the history triggers (correctly) refuse to let a test
// delete its way back to the legacy state.
func (env *bindingEnv) seedLegacyBoundProject(id, title, root string) *Project {
	env.t.Helper()
	canonical, err := CanonicalizeWorkspaceRoot(root, env.allowed)
	if err != nil {
		env.t.Fatalf("CanonicalizeWorkspaceRoot(%s): %v", root, err)
	}
	now := time.Now().Unix()
	if _, err := env.svc.db.Exec(`
		INSERT INTO projects (id, title, description, status, progress, tags_json, git_branch, created_at, updated_at, last_active, metadata_json, workspace_root, default_flow_id, default_session_id, binding_state)
		VALUES (?, ?, '', 'planning', 0, '[]', NULL, ?, ?, ?, NULL, ?, NULL, NULL, 'bound')
	`, id, title, now, now, now, canonical); err != nil {
		env.t.Fatalf("seed legacy project %s: %v", id, err)
	}
	// loadFromDBLocked replaces the whole in-memory map, so it must not run
	// while other goroutines are using the service; callers seed before they
	// start any.
	if err := env.svc.loadFromDBLocked(); err != nil {
		env.t.Fatalf("reload after seeding legacy project %s: %v", id, err)
	}
	project, err := env.svc.GetProject(context.Background(), id)
	if err != nil || project == nil {
		env.t.Fatalf("GetProject(%s) after seeding = %+v, %v", id, project, err)
	}
	return project
}

// bindingCounts reads the two binding tables directly. The assertions about
// "the history has exactly N rows" must not go through the store's own reads,
// or a broken store could agree with itself.
func (env *bindingEnv) bindingCounts(bindingID string) (bindings int, revisions int) {
	env.t.Helper()
	if err := env.svc.db.QueryRow(`SELECT COUNT(*) FROM workspace_bindings WHERE id = ?`, bindingID).Scan(&bindings); err != nil {
		env.t.Fatalf("count workspace_bindings: %v", err)
	}
	if err := env.svc.db.QueryRow(`SELECT COUNT(*) FROM workspace_binding_revisions WHERE binding_id = ?`, bindingID).Scan(&revisions); err != nil {
		env.t.Fatalf("count workspace_binding_revisions: %v", err)
	}
	return bindings, revisions
}

func (env *bindingEnv) totalRevisions() int {
	env.t.Helper()
	var n int
	if err := env.svc.db.QueryRow(`SELECT COUNT(*) FROM workspace_binding_revisions`).Scan(&n); err != nil {
		env.t.Fatalf("count all revisions: %v", err)
	}
	return n
}

func (env *bindingEnv) mustPrimary(projectID string) WorkspaceBindingRecord {
	env.t.Helper()
	record, err := env.svc.ActivePrimaryBinding(context.Background(), projectID)
	if err != nil {
		env.t.Fatalf("ActivePrimaryBinding(%s) failed: %v", projectID, err)
	}
	return record
}

// bindingConflict extracts the typed conflict from an error, failing the test
// when the error is not one.
func bindingConflict(t *testing.T, err error) *BindingConflictError {
	t.Helper()
	if err == nil {
		t.Fatal("expected a binding conflict, got nil")
	}
	var conflict *BindingConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected *BindingConflictError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("conflict must be reachable through errors.Is(err, ErrBindingConflict): %v", err)
	}
	return conflict
}

// ---------------------------------------------------------------------------
// CreateProject / persistence / reopen
// ---------------------------------------------------------------------------

func TestBindingStoreCreateProjectWritesPrimaryRevisionOne(t *testing.T) {
	env := newBindingEnv(t)
	project := env.createBoundProject("bound", env.rootA)

	record := env.mustPrimary(project.ID)
	if record.State != BindingStateActive {
		t.Fatalf("state = %q, want active", record.State)
	}
	if record.Snapshot.Kind != BindingKindPrimary {
		t.Fatalf("kind = %q, want primary", record.Snapshot.Kind)
	}
	if record.Snapshot.Revision != 1 {
		t.Fatalf("revision = %d, want 1", record.Snapshot.Revision)
	}
	if record.Snapshot.ProjectID != project.ID {
		t.Fatalf("snapshot project = %q, want %q", record.Snapshot.ProjectID, project.ID)
	}
	if record.Snapshot.ParentBindingID != "" {
		t.Fatalf("primary must have no parent, got %q", record.Snapshot.ParentBindingID)
	}
	if record.Snapshot.BindingID == "" {
		t.Fatal("binding id must be assigned")
	}
	canonical, err := CanonicalizeWorkspaceRoot(env.rootA, env.allowed)
	if err != nil {
		t.Fatalf("CanonicalizeWorkspaceRoot: %v", err)
	}
	if record.Snapshot.CanonicalRoot != canonical {
		t.Fatalf("canonical root = %q, want %q", record.Snapshot.CanonicalRoot, canonical)
	}
	if record.Snapshot.RootIdentity.IsZero() {
		t.Fatal("root identity must be captured")
	}
	if record.Snapshot.LinkPolicy != LinkPolicyDenyEscape {
		t.Fatalf("link policy = %q", record.Snapshot.LinkPolicy)
	}
	if record.Snapshot.CapturedAt.IsZero() {
		t.Fatal("captured_at must be set")
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		t.Fatal("created_at/updated_at must be set")
	}
	if record.RetiredAt != nil {
		t.Fatalf("a fresh binding must not be retired, got %v", record.RetiredAt)
	}

	bindings, revisions := env.bindingCounts(record.Snapshot.BindingID)
	if bindings != 1 || revisions != 1 {
		t.Fatalf("bindings=%d revisions=%d, want 1/1", bindings, revisions)
	}

	// The history row of revision 1 must be readable and equal to the current
	// snapshot, because a Run pinned to revision 1 reads it back later.
	revisionOne, err := env.svc.GetBindingRevision(context.Background(), record.Snapshot.BindingID, 1)
	if err != nil {
		t.Fatalf("GetBindingRevision(1) failed: %v", err)
	}
	if revisionOne.CanonicalRoot != record.Snapshot.CanonicalRoot ||
		!revisionOne.RootIdentity.Equal(record.Snapshot.RootIdentity) ||
		revisionOne.Revision != 1 {
		t.Fatalf("revision 1 = %+v, current = %+v", revisionOne, record.Snapshot)
	}

	// Durable across a real close/reopen of the file database.
	env.reopen()
	after := env.mustPrimary(project.ID)
	if after.Snapshot.BindingID != record.Snapshot.BindingID || after.Snapshot.Revision != 1 {
		t.Fatalf("after reopen: %+v, want binding %s revision 1", after.Snapshot, record.Snapshot.BindingID)
	}
	if after.Snapshot.CanonicalRoot != record.Snapshot.CanonicalRoot ||
		!after.Snapshot.RootIdentity.Equal(record.Snapshot.RootIdentity) {
		t.Fatalf("after reopen the root changed: %+v", after.Snapshot)
	}
	if !after.Snapshot.CapturedAt.Equal(record.Snapshot.CapturedAt) {
		t.Fatalf("captured_at changed across reopen: %v -> %v", record.Snapshot.CapturedAt, after.Snapshot.CapturedAt)
	}
}

func TestBindingStoreProjectWithoutRootHasNoBinding(t *testing.T) {
	env := newBindingEnv(t)
	project, err := env.svc.CreateProject(context.Background(), &ProjectCreateRequest{Title: "unbound"})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	if _, err := env.svc.ActivePrimaryBinding(context.Background(), project.ID); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("an unbound project must have no primary binding, got %v", err)
	}
	if n := env.totalRevisions(); n != 0 {
		t.Fatalf("no revisions expected, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Rebind: revision history and binding_rebound
// ---------------------------------------------------------------------------

func TestBindingStoreRebindAdvancesRevisionAndKeepsHistory(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("rebind", env.rootA)

	first := env.mustPrimary(project.ID)
	pinned := first.Snapshot // what a Run created now would freeze

	rebound, err := env.svc.BindWorkspaceRoot(ctx, project.ID, env.rootB)
	if err != nil {
		t.Fatalf("BindWorkspaceRoot(root-b) failed: %v", err)
	}
	canonicalB, err := CanonicalizeWorkspaceRoot(env.rootB, env.allowed)
	if err != nil {
		t.Fatalf("CanonicalizeWorkspaceRoot: %v", err)
	}
	if rebound.WorkspaceRoot != canonicalB {
		t.Fatalf("project root = %q, want %q", rebound.WorkspaceRoot, canonicalB)
	}

	current := env.mustPrimary(project.ID)
	if current.Snapshot.BindingID != first.Snapshot.BindingID {
		t.Fatalf("rebind must keep the binding id: %s -> %s",
			first.Snapshot.BindingID, current.Snapshot.BindingID)
	}
	if current.Snapshot.Revision != 2 {
		t.Fatalf("revision = %d, want 2", current.Snapshot.Revision)
	}
	if current.Snapshot.CanonicalRoot != canonicalB {
		t.Fatalf("current root = %q, want %q", current.Snapshot.CanonicalRoot, canonicalB)
	}
	bindings, revisions := env.bindingCounts(first.Snapshot.BindingID)
	if bindings != 1 || revisions != 2 {
		t.Fatalf("bindings=%d revisions=%d, want 1/2", bindings, revisions)
	}

	// The old revision still describes the old directory: this is what lets a
	// Run recover the snapshot it pinned after the project moved on.
	revisionOne, err := env.svc.GetBindingRevision(ctx, first.Snapshot.BindingID, 1)
	if err != nil {
		t.Fatalf("GetBindingRevision(1) failed: %v", err)
	}
	if revisionOne.CanonicalRoot != pinned.CanonicalRoot ||
		!revisionOne.RootIdentity.Equal(pinned.RootIdentity) {
		t.Fatalf("revision 1 was rewritten: %+v, want %+v", revisionOne, pinned)
	}
	if revisionOne.RootIdentity.Equal(current.Snapshot.RootIdentity) {
		t.Fatal("revision 1 and revision 2 must have different directory identities")
	}

	// The old Run's pinned snapshot no longer matches the binding in force.
	err = ValidateBinding(pinned, current.Snapshot, BindingPurposeDispatch, project.ID, env.allowed)
	var validation *BindingValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected *BindingValidationError, got %T: %v", err, err)
	}
	if validation.Code != BindingCodeRebound {
		t.Fatalf("code = %q, want %q", validation.Code, BindingCodeRebound)
	}

	// A run that re-reads its own revision and pins it against the current
	// binding is still valid, which is the other half of the same rule.
	if err := ValidateBinding(revisionOne, current.Snapshot, BindingPurposeDispatch, project.ID, env.allowed); err == nil {
		t.Fatal("revision 1 must not validate against revision 2")
	}
	if err := ValidateBinding(current.Snapshot, current.Snapshot, BindingPurposeDispatch, project.ID, env.allowed); err != nil {
		t.Fatalf("the binding in force must validate against itself: %v", err)
	}

	// Durable: the same revisions survive a reopen.
	env.reopen()
	after := env.mustPrimary(project.ID)
	if after.Snapshot.Revision != 2 || after.Snapshot.CanonicalRoot != canonicalB {
		t.Fatalf("after reopen: revision %d root %q", after.Snapshot.Revision, after.Snapshot.CanonicalRoot)
	}
	if _, err := env.svc.GetBindingRevision(ctx, first.Snapshot.BindingID, 1); err != nil {
		t.Fatalf("revision 1 must survive reopen: %v", err)
	}
}

func TestBindingStoreRebindToSameDirectoryDoesNotAdvanceRevision(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("same-root", env.rootA)
	first := env.mustPrimary(project.ID)

	// Same path, spelled the same way.
	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, env.rootA); err != nil {
		t.Fatalf("rebind to the same root failed: %v", err)
	}
	current := env.mustPrimary(project.ID)
	if current.Snapshot.Revision != 1 {
		t.Fatalf("revision = %d, want 1 (same directory is not a rebind)", current.Snapshot.Revision)
	}
	if _, revisions := env.bindingCounts(first.Snapshot.BindingID); revisions != 1 {
		t.Fatalf("revisions = %d, want 1", revisions)
	}

	// Same directory through a junction: a different path string that resolves
	// to the same directory must also be recognised as "no change".
	junction := filepath.Join(filepath.Dir(env.rootA), "junction-a")
	makeJunction(t, junction, env.rootA)
	t.Cleanup(func() { removeJunction(t, junction) })

	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, junction); err != nil {
		t.Fatalf("rebind through a junction failed: %v", err)
	}
	viaJunction := env.mustPrimary(project.ID)
	if viaJunction.Snapshot.Revision != 1 {
		t.Fatalf("revision = %d after binding through a junction, want 1", viaJunction.Snapshot.Revision)
	}
	if viaJunction.Snapshot.CanonicalRoot != first.Snapshot.CanonicalRoot {
		t.Fatalf("canonical root = %q, want %q", viaJunction.Snapshot.CanonicalRoot, first.Snapshot.CanonicalRoot)
	}
	if !viaJunction.Snapshot.RootIdentity.Equal(first.Snapshot.RootIdentity) {
		t.Fatalf("junction must keep the target identity: %s -> %s",
			first.Snapshot.RootIdentity.String(), viaJunction.Snapshot.RootIdentity.String())
	}
	if _, revisions := env.bindingCounts(first.Snapshot.BindingID); revisions != 1 {
		t.Fatalf("revisions = %d after the junction rebind, want 1", revisions)
	}
	// A junction is not just "no error": the binding must still name a path the
	// OS resolves to the same directory it recorded, so Recheck passes.
	if err := viaJunction.Snapshot.Recheck(env.allowed); err != nil {
		t.Fatalf("the junction rebind left a binding that fails Recheck: %v", err)
	}

	// A genuinely different directory still moves the revision, so the test
	// above is not passing because nothing ever moves.
	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, env.rootB); err != nil {
		t.Fatalf("rebind to root-b failed: %v", err)
	}
	if got := env.mustPrimary(project.ID).Snapshot.Revision; got != 2 {
		t.Fatalf("revision = %d after a real rebind, want 2", got)
	}
}

// TestBindingStoreRebindSameIdentityDifferentPathKeepsRevision covers the second
// half of "binding the same directory twice is not a rebind": the stored
// canonical path no longer matches the project's root, but the OS identity of
// the directory behind both is the same. That is what a renamed directory looks
// like, and it must not burn a revision - a Run pinned to revision 1 would be
// refused for a change that never happened.
func TestBindingStoreRebindSameIdentityDifferentPathKeepsRevision(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("renamed", env.rootA)
	first := env.mustPrimary(project.ID)

	// Rewrite the stored canonical root to a path that is not the project's
	// root, keeping the identity. The directory "moved" without changing.
	stale := filepath.Join(filepath.Dir(env.rootA), "root-a-old-name")
	if _, err := env.svc.db.Exec(`UPDATE workspace_bindings SET canonical_root = ? WHERE id = ?`, stale, first.Snapshot.BindingID); err != nil {
		t.Fatalf("simulate a renamed directory: %v", err)
	}

	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, env.rootA); err != nil {
		t.Fatalf("BindWorkspaceRoot failed: %v", err)
	}
	current := env.mustPrimary(project.ID)
	if current.Snapshot.Revision != 1 {
		t.Fatalf("revision = %d, want 1: the same directory identity is not a rebind", current.Snapshot.Revision)
	}
	// The stored path follows the project's root (the old path is dead), and
	// Recheck - which is what every dispatch runs - accepts it again.
	if current.Snapshot.CanonicalRoot != first.Snapshot.CanonicalRoot {
		t.Fatalf("canonical root = %q, want %q", current.Snapshot.CanonicalRoot, first.Snapshot.CanonicalRoot)
	}
	if err := current.Snapshot.Recheck(env.allowed); err != nil {
		t.Fatalf("the refreshed binding must still pass Recheck: %v", err)
	}
	if _, revisions := env.bindingCounts(first.Snapshot.BindingID); revisions != 1 {
		t.Fatalf("revisions = %d, want 1 (a path refresh is not a revision)", revisions)
	}

	// A *different* directory at a different path still moves the revision, so
	// the check above is not passing because nothing ever moves.
	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, env.rootB); err != nil {
		t.Fatalf("rebind to root-b failed: %v", err)
	}
	if got := env.mustPrimary(project.ID).Snapshot.Revision; got != 2 {
		t.Fatalf("revision = %d after a real rebind, want 2", got)
	}
}

func TestBindingStoreUpdateProjectRootRebindsOnce(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("update-root", env.rootA)
	first := env.mustPrimary(project.ID)

	// An update that does not mention the root must not touch the binding.
	if _, err := env.svc.UpdateProject(ctx, project.ID, &ProjectUpdateRequest{
		Title: func(v string) *string { return &v }("renamed"),
	}); err != nil {
		t.Fatalf("UpdateProject(title) failed: %v", err)
	}
	if got := env.mustPrimary(project.ID); got.Snapshot.Revision != 1 {
		t.Fatalf("a title update moved the binding revision to %d", got.Snapshot.Revision)
	}
	if _, revisions := env.bindingCounts(first.Snapshot.BindingID); revisions != 1 {
		t.Fatalf("a title update added a history row: revisions=%d", revisions)
	}

	// An update that names the same root must not touch it either.
	if _, err := env.svc.UpdateProject(ctx, project.ID, &ProjectUpdateRequest{
		WorkspaceRoot: func(v string) *string { return &v }(env.rootA),
	}); err != nil {
		t.Fatalf("UpdateProject(same root) failed: %v", err)
	}
	if _, revisions := env.bindingCounts(first.Snapshot.BindingID); revisions != 1 {
		t.Fatalf("re-binding the same root added a history row: revisions=%d", revisions)
	}

	// A different root rebinds once.
	if _, err := env.svc.UpdateProject(ctx, project.ID, &ProjectUpdateRequest{
		WorkspaceRoot: func(v string) *string { return &v }(env.rootB),
	}); err != nil {
		t.Fatalf("UpdateProject(new root) failed: %v", err)
	}
	current := env.mustPrimary(project.ID)
	if current.Snapshot.Revision != 2 {
		t.Fatalf("revision = %d, want 2", current.Snapshot.Revision)
	}
	if _, revisions := env.bindingCounts(first.Snapshot.BindingID); revisions != 2 {
		t.Fatalf("revisions = %d, want 2", revisions)
	}
}

// ---------------------------------------------------------------------------
// One active primary per project
// ---------------------------------------------------------------------------

func TestBindingStoreActivePrimaryIsUniquePerProject(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("unique", env.rootA)
	record := env.mustPrimary(project.ID)

	identity := workspace.RootIdentity{Platform: "test", Volume: "1", FileID: "2"}
	identityJSON, err := identityToJSON(identity)
	if err != nil {
		t.Fatalf("identityToJSON: %v", err)
	}

	// Bypass the API entirely: the database itself must refuse a second active
	// primary for the same project.
	_, err = env.svc.db.Exec(`
		INSERT INTO workspace_bindings (id, project_id, kind, parent_binding_id, binding_revision, canonical_root, root_identity_json, root_key, base_ref, link_policy, state, created_at, updated_at, retired_at)
		VALUES ('rogue', ?, 'primary', NULL, 1, 'C:\rogue', ?, 'root-identity:test:1:2', NULL, 'deny_escape', 'active', 1, 1, NULL)
	`, project.ID, identityJSON)
	if err == nil {
		t.Fatal("a second active primary was accepted by the database")
	}
	if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("unexpected error from the partial unique index: %v", err)
	}

	// A retired primary does not occupy the slot, which is how a project gets a
	// brand new primary binding after its old one was retired.
	if err := env.svc.RetireBinding(ctx, record.Snapshot.BindingID, 1); err != nil {
		t.Fatalf("RetireBinding failed: %v", err)
	}
	if _, err := env.svc.ActivePrimaryBinding(ctx, project.ID); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("a retired primary must not be active, got %v", err)
	}
	recreated := env.createBoundProjectFromWriter(t, project.ID, env.rootB)
	if recreated.Snapshot.BindingID == record.Snapshot.BindingID {
		t.Fatal("the new primary must have a new binding id")
	}
	if recreated.Snapshot.Revision != 1 {
		t.Fatalf("a new primary starts at revision 1, got %d", recreated.Snapshot.Revision)
	}
	if got := env.mustPrimary(project.ID).Snapshot.BindingID; got != recreated.Snapshot.BindingID {
		t.Fatalf("active primary = %s, want %s", got, recreated.Snapshot.BindingID)
	}
}

// createBoundProjectFromWriter drives a new primary through the public writer
// (BindWorkspaceRoot) rather than the database, so the test never depends on
// the internals of the store to reach a legal state.
func (env *bindingEnv) createBoundProjectFromWriter(t *testing.T, projectID, root string) WorkspaceBindingRecord {
	t.Helper()
	if _, err := env.svc.BindWorkspaceRoot(context.Background(), projectID, root); err != nil {
		t.Fatalf("BindWorkspaceRoot(%s) failed: %v", root, err)
	}
	return env.mustPrimary(projectID)
}

// ---------------------------------------------------------------------------
// Derived bindings
// ---------------------------------------------------------------------------

func TestBindingStoreDerivedBindingsCoexist(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("derived", env.rootA)
	primary := env.mustPrimary(project.ID)

	flowRoot := mkdirAll(t, filepath.Join(filepath.Dir(env.rootA), "flow-root"))
	runRootA := mkdirAll(t, filepath.Join(filepath.Dir(env.rootA), "run-a"))
	runRootB := mkdirAll(t, filepath.Join(filepath.Dir(env.rootA), "run-b"))

	flow, err := env.svc.CreateDerivedBinding(ctx, primary.Snapshot.BindingID, BindingKindFlow, flowRoot, "refs/heads/flow")
	if err != nil {
		t.Fatalf("CreateDerivedBinding(flow) failed: %v", err)
	}
	if flow.Snapshot.Kind != BindingKindFlow || flow.Snapshot.ParentBindingID != primary.Snapshot.BindingID {
		t.Fatalf("flow binding = %+v", flow.Snapshot)
	}
	if flow.Snapshot.ProjectID != project.ID {
		t.Fatalf("derived binding project = %q, want %q", flow.Snapshot.ProjectID, project.ID)
	}
	if flow.Snapshot.Revision != 1 || flow.Snapshot.BaseRef != "refs/heads/flow" {
		t.Fatalf("flow binding = %+v", flow.Snapshot)
	}

	runA, err := env.svc.CreateDerivedBinding(ctx, primary.Snapshot.BindingID, BindingKindRun, runRootA, "")
	if err != nil {
		t.Fatalf("CreateDerivedBinding(run A) failed: %v", err)
	}
	runB, err := env.svc.CreateDerivedBinding(ctx, flow.Snapshot.BindingID, BindingKindRun, runRootB, "")
	if err != nil {
		t.Fatalf("CreateDerivedBinding(run B under the flow) failed: %v", err)
	}
	if runB.Snapshot.ParentBindingID != flow.Snapshot.BindingID {
		t.Fatalf("run under flow parent = %q", runB.Snapshot.ParentBindingID)
	}
	ids := map[string]bool{primary.Snapshot.BindingID: true, flow.Snapshot.BindingID: true, runA.Snapshot.BindingID: true, runB.Snapshot.BindingID: true}
	if len(ids) != 4 {
		t.Fatal("every binding must have its own id")
	}

	// The primary is still the only active primary, and it is still the one
	// ActivePrimaryBinding returns.
	stillPrimary := env.mustPrimary(project.ID)
	if stillPrimary.Snapshot.BindingID != primary.Snapshot.BindingID {
		t.Fatalf("active primary changed to %s", stillPrimary.Snapshot.BindingID)
	}
	var activePrimaries int
	if err := env.svc.db.QueryRow(`SELECT COUNT(*) FROM workspace_bindings WHERE project_id = ? AND kind = 'primary' AND state = 'active'`, project.ID).Scan(&activePrimaries); err != nil {
		t.Fatalf("count active primaries: %v", err)
	}
	if activePrimaries != 1 {
		t.Fatalf("active primaries = %d, want 1", activePrimaries)
	}
	for _, record := range []WorkspaceBindingRecord{flow, runA, runB} {
		if !record.Active() {
			t.Fatalf("derived binding %s is %s, want active", record.Snapshot.BindingID, record.State)
		}
	}

	// Rejections: a retired parent, a primary parent for nothing, and a parent
	// that is not a primary when a flow is asked for.
	if err := env.svc.RetireBinding(ctx, flow.Snapshot.BindingID, 1); err != nil {
		t.Fatalf("RetireBinding(flow) failed: %v", err)
	}
	_, err = env.svc.CreateDerivedBinding(ctx, flow.Snapshot.BindingID, BindingKindRun, runRootB, "")
	if conflict := bindingConflict(t, err); conflict.Code != BindingConflictParentState {
		t.Fatalf("code = %q, want %q", conflict.Code, BindingConflictParentState)
	}
	_, err = env.svc.CreateDerivedBinding(ctx, primary.Snapshot.BindingID, BindingKindPrimary, runRootB, "")
	if conflict := bindingConflict(t, err); conflict.Code != BindingConflictKind {
		t.Fatalf("code = %q, want %q", conflict.Code, BindingConflictKind)
	}
	_, err = env.svc.CreateDerivedBinding(ctx, "does-not-exist", BindingKindRun, runRootB, "")
	if conflict := bindingConflict(t, err); conflict.Code != BindingConflictParentNotFound {
		t.Fatalf("code = %q, want %q", conflict.Code, BindingConflictParentNotFound)
	}
	if _, err := env.svc.CreateDerivedBinding(ctx, primary.Snapshot.BindingID, BindingKindFlow, runRootB, ""); err != nil {
		t.Fatalf("a flow under a primary parent must be accepted: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Two projects, one directory
// ---------------------------------------------------------------------------

func TestBindingStoreTwoProjectsShareOneRootKey(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	first := env.createBoundProject("first", env.rootA)
	second := env.createBoundProject("second", env.rootA)

	firstBinding := env.mustPrimary(first.ID)
	secondBinding := env.mustPrimary(second.ID)
	if firstBinding.Snapshot.BindingID == secondBinding.Snapshot.BindingID {
		t.Fatal("two projects must not share a binding id")
	}
	firstKey := rootKeyOf(firstBinding.Snapshot)
	if firstKey != rootKeyOf(secondBinding.Snapshot) {
		t.Fatalf("root keys differ: %s vs %s", firstKey, rootKeyOf(secondBinding.Snapshot))
	}

	byRoot, err := env.svc.ActiveBindingsByRootKey(ctx, firstKey)
	if err != nil {
		t.Fatalf("ActiveBindingsByRootKey failed: %v", err)
	}
	if len(byRoot) != 2 {
		t.Fatalf("bindings by root key = %d, want 2", len(byRoot))
	}
	seen := map[string]bool{}
	for _, record := range byRoot {
		seen[record.Snapshot.ProjectID] = true
	}
	if !seen[first.ID] || !seen[second.ID] {
		t.Fatalf("root key lookup returned projects %v", seen)
	}

	// Retiring one project's binding removes it from the shared-root set without
	// touching the other project's binding.
	if err := env.svc.RetireBinding(ctx, firstBinding.Snapshot.BindingID, 1); err != nil {
		t.Fatalf("RetireBinding failed: %v", err)
	}
	byRoot, err = env.svc.ActiveBindingsByRootKey(ctx, firstKey)
	if err != nil {
		t.Fatalf("ActiveBindingsByRootKey failed: %v", err)
	}
	if len(byRoot) != 1 || byRoot[0].Snapshot.ProjectID != second.ID {
		t.Fatalf("after retire: %+v", byRoot)
	}
	if got := env.mustPrimary(second.ID); got.Snapshot.Revision != 1 {
		t.Fatalf("the other project's binding moved to revision %d", got.Snapshot.Revision)
	}
}

// ---------------------------------------------------------------------------
// The whole-table rewrite must not touch bindings
// ---------------------------------------------------------------------------

func TestBindingStoreOrdinarySaveDoesNotTouchBindings(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("survivor", env.rootA)

	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, env.rootB); err != nil {
		t.Fatalf("BindWorkspaceRoot failed: %v", err)
	}
	before := env.mustPrimary(project.ID)
	if before.Snapshot.Revision != 2 {
		t.Fatalf("revision = %d, want 2", before.Snapshot.Revision)
	}

	// A batch of ordinary saves, each of which rewrites the whole projects
	// table (DELETE FROM projects + re-INSERT). If workspace_bindings had a
	// foreign key to projects these would either fail outright or cascade the
	// bindings away.
	for i := 0; i < 3; i++ {
		title := "title-" + string(rune('a'+i))
		if _, err := env.svc.UpdateProject(ctx, project.ID, &ProjectUpdateRequest{
			Title: &title,
		}); err != nil {
			t.Fatalf("UpdateProject failed: %v", err)
		}
	}
	if _, err := env.svc.UpdateRuntimeBindings(ctx, project.ID, "flow-1", "session-1"); err != nil {
		t.Fatalf("UpdateRuntimeBindings failed: %v", err)
	}
	if err := env.svc.AddPlanToProject(ctx, project.ID, "plan-1"); err != nil {
		t.Fatalf("AddPlanToProject failed: %v", err)
	}
	if err := env.svc.RemovePlanFromProject(ctx, project.ID, "plan-1"); err != nil {
		t.Fatalf("RemovePlanFromProject failed: %v", err)
	}
	// A second, unrelated project forces another full rewrite of both tables.
	env.createBoundProject("other", env.rootA)

	after := env.mustPrimary(project.ID)
	if after.Snapshot.BindingID != before.Snapshot.BindingID ||
		after.Snapshot.Revision != before.Snapshot.Revision ||
		after.Snapshot.CanonicalRoot != before.Snapshot.CanonicalRoot {
		t.Fatalf("an ordinary save changed the binding: %+v -> %+v", before.Snapshot, after.Snapshot)
	}
	bindings, revisions := env.bindingCounts(before.Snapshot.BindingID)
	if bindings != 1 || revisions != 2 {
		t.Fatalf("bindings=%d revisions=%d, want 1/2", bindings, revisions)
	}

	// And the rows are still there after the database is reopened, so this is
	// persistence and not just a surviving in-memory map.
	env.reopen()
	if _, revisions := env.bindingCounts(before.Snapshot.BindingID); revisions != 2 {
		t.Fatalf("revisions after reopen = %d, want 2", revisions)
	}
}

// ---------------------------------------------------------------------------
// Atomicity
// ---------------------------------------------------------------------------

// TestBindingStoreBindingWriteFailureRollsBackProjectAndBinding drives the
// public writer with a binding write that cannot succeed inside the transaction,
// and checks both halves of atomicity: the in-memory project is restored and the
// database - reopened from disk - has neither the new root nor a new binding.
//
// The failure is injected inside the transaction (a duplicate binding id) rather
// than before it, because a failure *before* BeginTx would never prove that the
// projects rewrite and the binding write share one commit. A binding write that
// failed on its own would still leave the projects row written if the two used
// separate transactions.
func TestBindingStoreBindingWriteFailureRollsBackProjectAndBinding(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("atomic", env.rootA)
	record := env.mustPrimary(project.ID)

	before, err := env.svc.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	beforeRoot := before.WorkspaceRoot
	beforeTitle := before.Title

	// The rebind captures the new root and then hits the duplicate id inside the
	// transaction, so the whole thing must roll back.
	env.svc.failNextBindingWrite = func(context.Context, *sql.Tx) error {
		return &BindingConflictError{
			Code:      BindingConflictAlreadyExists,
			ProjectID: project.ID,
			BindingID: record.Snapshot.BindingID,
			Detail:    "injected binding write fault",
		}
	}
	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, env.rootB); err == nil {
		t.Fatal("the injected binding write fault must fail the rebind")
	}
	env.svc.failNextBindingWrite = nil

	// Memory is restored...
	after, err := env.svc.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if after.WorkspaceRoot != beforeRoot || after.Title != beforeTitle {
		t.Fatalf("in-memory project = %+v, want root %q title %q (restoreState must have run)",
			after, beforeRoot, beforeTitle)
	}
	// ...the binding is untouched...
	current := env.mustPrimary(project.ID)
	if current.Snapshot.BindingID != record.Snapshot.BindingID || current.Snapshot.Revision != 1 {
		t.Fatalf("binding changed after a failed rebind: %+v", current.Snapshot)
	}
	if _, revisions := env.bindingCounts(record.Snapshot.BindingID); revisions != 1 {
		t.Fatalf("revisions = %d, want 1", revisions)
	}
	// ...and disk agrees after a reopen, which is what proves the projects row
	// was not written either.
	env.reopen()
	reloaded, err := env.svc.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProject after reopen failed: %v", err)
	}
	if reloaded == nil || reloaded.WorkspaceRoot != beforeRoot || reloaded.Title != beforeTitle {
		t.Fatalf("persisted project = %+v, want root %q title %q", reloaded, beforeRoot, beforeTitle)
	}
	if got := env.mustPrimary(project.ID); got.Snapshot.Revision != 1 {
		t.Fatalf("persisted binding revision = %d, want 1", got.Snapshot.Revision)
	}
}

// TestBindingStoreSnapshotCaptureFailureLeavesEverythingUntouched covers the
// other failure position: the capture (filesystem I/O, before the transaction)
// fails because the directory is gone. Nothing may be written - including the
// in-memory project, which the InMemory layer already changed by the time the
// capture runs.
func TestBindingStoreSnapshotCaptureFailureLeavesEverythingUntouched(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("capture-fault", env.rootA)
	record := env.mustPrimary(project.ID)

	before, err := env.svc.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}

	missing := filepath.Join(filepath.Dir(env.rootA), "gone")
	mkdirAll(t, missing)
	if err := os.RemoveAll(missing); err != nil {
		t.Fatalf("remove %s: %v", missing, err)
	}
	if _, err := env.svc.BindWorkspaceRoot(ctx, project.ID, missing); err == nil {
		t.Fatal("binding a deleted directory must fail")
	}

	after, err := env.svc.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if after.WorkspaceRoot != before.WorkspaceRoot {
		t.Fatalf("in-memory root = %q, want %q", after.WorkspaceRoot, before.WorkspaceRoot)
	}
	if after.BindingState != BindingStateBound {
		t.Fatalf("in-memory binding state = %q", after.BindingState)
	}
	if got := env.mustPrimary(project.ID); got.Snapshot.Revision != 1 ||
		got.Snapshot.BindingID != record.Snapshot.BindingID {
		t.Fatalf("binding changed after a failed capture: %+v", got.Snapshot)
	}
	if _, revisions := env.bindingCounts(record.Snapshot.BindingID); revisions != 1 {
		t.Fatalf("revisions = %d, want 1", revisions)
	}

	env.reopen()
	reloaded, err := env.svc.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProject after reopen failed: %v", err)
	}
	if reloaded == nil || reloaded.WorkspaceRoot != before.WorkspaceRoot {
		t.Fatalf("persisted root = %+v, want %q", reloaded, before.WorkspaceRoot)
	}
}

// ---------------------------------------------------------------------------
// EnsurePrimaryBinding: lazy backfill, concurrency, refusal
// ---------------------------------------------------------------------------

func TestBindingStoreEnsurePrimaryBindingBackfillsLegacyProject(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.seedLegacyBoundProject("legacy-project", "legacy", env.rootA)

	if _, err := env.svc.ActivePrimaryBinding(ctx, project.ID); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("expected ErrBindingNotFound before backfill, got %v", err)
	}

	backfilled, err := env.svc.EnsurePrimaryBinding(ctx, project.ID)
	if err != nil {
		t.Fatalf("EnsurePrimaryBinding failed: %v", err)
	}
	if backfilled.Snapshot.Revision != 1 {
		t.Fatalf("backfill revision = %d, want 1", backfilled.Snapshot.Revision)
	}
	if backfilled.Snapshot.Kind != BindingKindPrimary {
		t.Fatalf("backfill kind = %q", backfilled.Snapshot.Kind)
	}
	canonical, err := CanonicalizeWorkspaceRoot(env.rootA, env.allowed)
	if err != nil {
		t.Fatalf("CanonicalizeWorkspaceRoot: %v", err)
	}
	if backfilled.Snapshot.CanonicalRoot != canonical {
		t.Fatalf("backfill root = %q, want %q", backfilled.Snapshot.CanonicalRoot, canonical)
	}
	if _, revisions := env.bindingCounts(backfilled.Snapshot.BindingID); revisions != 1 {
		t.Fatalf("backfill revisions = %d, want 1", revisions)
	}

	// Idempotent: a second call returns the same row and adds nothing.
	again, err := env.svc.EnsurePrimaryBinding(ctx, project.ID)
	if err != nil {
		t.Fatalf("second EnsurePrimaryBinding failed: %v", err)
	}
	if again.Snapshot.BindingID != backfilled.Snapshot.BindingID {
		t.Fatalf("second call returned binding %s, want %s", again.Snapshot.BindingID, backfilled.Snapshot.BindingID)
	}
	if n := env.totalRevisions(); n != 1 {
		t.Fatalf("total revisions = %d, want 1", n)
	}
}

func TestBindingStoreEnsurePrimaryBindingRefusesUnboundProject(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)

	unbound, err := env.svc.CreateProject(ctx, &ProjectCreateRequest{Title: "unbound"})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	_, err = env.svc.EnsurePrimaryBinding(ctx, unbound.ID)
	if conflict := bindingConflict(t, err); conflict.Code != BindingConflictNotBound {
		t.Fatalf("code = %q, want %q", conflict.Code, BindingConflictNotBound)
	}
	if _, err := env.svc.EnsurePrimaryBinding(ctx, "missing-project"); err == nil {
		t.Fatal("an unknown project must be refused")
	}

	// An archived project keeps its binding but must not gain a new one: the
	// project below is archived *and* has no binding row (a legacy archive).
	legacyArchived := env.seedLegacyBoundProject("legacy-archived", "legacy archived", env.rootB)
	if err := env.svc.DeleteProject(ctx, legacyArchived.ID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}
	if _, err := env.svc.ActivePrimaryBinding(ctx, legacyArchived.ID); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("archived legacy project should have no binding, got %v", err)
	}
	_, err = env.svc.EnsurePrimaryBinding(ctx, legacyArchived.ID)
	if conflict := bindingConflict(t, err); conflict.Code != BindingConflictNotBound {
		t.Fatalf("archived project: code = %q, want %q", conflict.Code, BindingConflictNotBound)
	}
	if n := env.totalRevisions(); n != 0 {
		t.Fatalf("an archived project must not gain a binding, revisions = %d", n)
	}
}

func TestBindingStoreEnsurePrimaryBindingConcurrentCallersCreateOneRow(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.seedLegacyBoundProject("concurrent-project", "concurrent", env.rootA)

	const callers = 8
	var wg sync.WaitGroup
	ids := make([]string, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			got, err := env.svc.EnsurePrimaryBinding(ctx, project.ID)
			if err != nil {
				errs[idx] = err
				return
			}
			ids[idx] = got.Snapshot.BindingID
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d failed: %v", i, err)
		}
	}
	for i, id := range ids {
		if id != ids[0] {
			t.Fatalf("caller %d saw binding %s, caller 0 saw %s", i, id, ids[0])
		}
	}
	var rows int
	if err := env.svc.db.QueryRow(`SELECT COUNT(*) FROM workspace_bindings WHERE project_id = ? AND kind = 'primary'`, project.ID).Scan(&rows); err != nil {
		t.Fatalf("count primaries: %v", err)
	}
	if rows != 1 {
		t.Fatalf("primary binding rows = %d, want 1", rows)
	}
	if n := env.totalRevisions(); n != 1 {
		t.Fatalf("revision rows = %d, want 1", n)
	}
}

// TestBindingStoreEnsurePrimaryBindingLosesRaceAndReReads pins the documented
// behaviour of the losing writer. writeMu serializes the eight goroutines in the
// test above, so they never actually collide on the unique index; this test
// injects the collision the index would raise and asserts that the loser returns
// the winner's row instead of an error. Without the re-read, a lost race would
// surface as a 409 to a caller who only asked "make sure this project is bound".
// TestBindingStoreEnsurePrimaryBindingExistingRowWritesNothing is the reachable
// half of the concurrency contract. EnsurePrimaryBinding serializes its writers
// with writeMu, so in this process a second caller always takes the "already
// bound" fast path rather than colliding on the unique index; what has to be
// true is that the fast path returns the row in force and writes nothing at all.
// (The conflict-then-re-read branch behind it is a defensive path for a future
// out-of-process writer and is not reachable while the mutex is held, so it is
// deliberately not claimed as covered here.)
func TestBindingStoreEnsurePrimaryBindingExistingRowWritesNothing(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("existing", env.rootA)
	winner := env.mustPrimary(project.ID)

	// Any write attempt would now fail, so a fast path that wrote would be
	// caught even though the call is expected to succeed.
	env.svc.failNextBindingWrite = func(context.Context, *sql.Tx) error {
		t.Error("EnsurePrimaryBinding wrote a binding for a project that already had one")
		return errors.New("unexpected binding write")
	}
	got, err := env.svc.EnsurePrimaryBinding(ctx, project.ID)
	if err != nil {
		t.Fatalf("EnsurePrimaryBinding on a bound project failed: %v", err)
	}
	if got.Snapshot.BindingID != winner.Snapshot.BindingID {
		t.Fatalf("returned binding %s, want %s", got.Snapshot.BindingID, winner.Snapshot.BindingID)
	}
	if got.Snapshot.Revision != 1 {
		t.Fatalf("revision = %d, want 1", got.Snapshot.Revision)
	}
	if n := env.totalRevisions(); n != 1 {
		t.Fatalf("revision rows = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// History is append-only; RetireBinding CAS
// ---------------------------------------------------------------------------

func TestBindingStoreRevisionHistoryIsImmutable(t *testing.T) {
	env := newBindingEnv(t)
	project := env.createBoundProject("immutable", env.rootA)
	record := env.mustPrimary(project.ID)

	if _, err := env.svc.db.Exec(`UPDATE workspace_binding_revisions SET canonical_root = 'C:\rewritten' WHERE binding_id = ?`, record.Snapshot.BindingID); err == nil {
		t.Fatal("updating a history row must be refused by the trigger")
	} else if !strings.Contains(err.Error(), "binding_revision_immutable") {
		t.Fatalf("unexpected update error: %v", err)
	}

	if _, err := env.svc.db.Exec(`DELETE FROM workspace_binding_revisions WHERE binding_id = ?`, record.Snapshot.BindingID); err == nil {
		t.Fatal("deleting a history row must be refused by the trigger")
	} else if !strings.Contains(err.Error(), "binding_revision_immutable") {
		t.Fatalf("unexpected delete error: %v", err)
	}

	// The row is still exactly what it was.
	revision, err := env.svc.GetBindingRevision(context.Background(), record.Snapshot.BindingID, 1)
	if err != nil {
		t.Fatalf("GetBindingRevision failed: %v", err)
	}
	if revision.CanonicalRoot != record.Snapshot.CanonicalRoot {
		t.Fatalf("revision 1 root = %q, want %q", revision.CanonicalRoot, record.Snapshot.CanonicalRoot)
	}
}

func TestBindingStoreRetireBindingCAS(t *testing.T) {
	ctx := context.Background()
	env := newBindingEnv(t)
	project := env.createBoundProject("retire", env.rootA)
	record := env.mustPrimary(project.ID)

	// Wrong expected revision: refused, with expected/current filled in.
	err := env.svc.RetireBinding(ctx, record.Snapshot.BindingID, 7)
	conflict := bindingConflict(t, err)
	if conflict.Code != BindingConflictRevision {
		t.Fatalf("code = %q, want %q", conflict.Code, BindingConflictRevision)
	}
	if conflict.Expected != 7 || conflict.Current != 1 {
		t.Fatalf("expected/current = %d/%d, want 7/1", conflict.Expected, conflict.Current)
	}
	if conflict.Next() != "retry" {
		t.Fatalf("Next() = %q, want retry", conflict.Next())
	}
	if !strings.Contains(err.Error(), "expected revision 7, current 1") {
		t.Fatalf("error must carry both revisions: %v", err)
	}
	if env.mustPrimary(project.ID).State != BindingStateActive {
		t.Fatal("a refused retire must not change the state")
	}

	// Correct revision: retired.
	if err := env.svc.RetireBinding(ctx, record.Snapshot.BindingID, 1); err != nil {
		t.Fatalf("RetireBinding failed: %v", err)
	}
	retired, err := env.svc.GetBinding(ctx, record.Snapshot.BindingID)
	if err != nil {
		t.Fatalf("GetBinding failed: %v", err)
	}
	if retired.State != BindingStateRetired || retired.RetiredAt == nil {
		t.Fatalf("retired binding = %+v", retired)
	}

	// Idempotent at the same revision...
	if err := env.svc.RetireBinding(ctx, record.Snapshot.BindingID, 1); err != nil {
		t.Fatalf("a repeated retire must be idempotent: %v", err)
	}
	// ...and a conflict at a different one.
	err = env.svc.RetireBinding(ctx, record.Snapshot.BindingID, 2)
	if conflict := bindingConflict(t, err); conflict.Code != BindingConflictRevision {
		t.Fatalf("code = %q, want %q", conflict.Code, BindingConflictRevision)
	}

	// The history is untouched by retiring.
	if _, revisions := env.bindingCounts(record.Snapshot.BindingID); revisions != 1 {
		t.Fatalf("revisions = %d, want 1", revisions)
	}
	if err := env.svc.RetireBinding(ctx, "no-such-binding", 1); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("retiring an unknown binding = %v, want ErrBindingNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Legacy database compatibility
// ---------------------------------------------------------------------------

func TestBindingStoreOpensLegacyDatabaseAndBackfills(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")
	root := mkdirAll(t, filepath.Join(dir, "legacy-root"))

	// Build a database with the *old* schema only, exactly as the previous
	// release would have left it, and write one bound project into it.
	legacy, err := dbx.Open(dbPath)
	if err != nil {
		t.Fatalf("dbx.Open failed: %v", err)
	}
	canonical, err := CanonicalizeWorkspaceRoot(root, []string{dir})
	if err != nil {
		t.Fatalf("CanonicalizeWorkspaceRoot: %v", err)
	}
	if _, err := legacy.Exec(`
		CREATE TABLE projects (
			id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT, status TEXT NOT NULL,
			progress INTEGER NOT NULL, tags_json TEXT NOT NULL DEFAULT '[]', git_branch TEXT,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, last_active INTEGER NOT NULL,
			metadata_json TEXT, workspace_root TEXT, default_flow_id TEXT, default_session_id TEXT,
			binding_state TEXT NOT NULL DEFAULT 'unbound'
		);
		CREATE TABLE project_plans (
			project_id TEXT NOT NULL, plan_id TEXT NOT NULL, position INTEGER NOT NULL,
			PRIMARY KEY (project_id, plan_id),
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		);
		CREATE TABLE project_operations (
			idempotency_key TEXT PRIMARY KEY, project_id TEXT NOT NULL, flow_id TEXT, session_id TEXT,
			title TEXT NOT NULL DEFAULT '', workspace_root TEXT, normalized_request_hash TEXT,
			operation TEXT NOT NULL, state TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		);
		INSERT INTO projects (id, title, description, status, progress, tags_json, git_branch, created_at, updated_at, last_active, metadata_json, workspace_root, default_flow_id, default_session_id, binding_state)
		VALUES ('legacy-project', 'legacy', '', 'planning', 0, '[]', NULL, 100, 100, 100, NULL, ?, NULL, NULL, 'bound');
	`, canonical); err != nil {
		_ = legacy.Close()
		t.Fatalf("seed legacy database failed: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy database failed: %v", err)
	}

	// Opening with the new code must create the binding tables and leave the old
	// data readable.
	svc, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteProjectService on a legacy database failed: %v", err)
	}
	defer svc.Close()
	svc.SetAllowedWorkspaceRoots([]string{dir})

	for _, table := range []string{"workspace_bindings", "workspace_binding_revisions"} {
		var name string
		if err := svc.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("table %s was not created: %v", table, err)
		}
	}

	project, err := svc.GetProject(ctx, "legacy-project")
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if project == nil || project.WorkspaceRoot != canonical || project.BindingState != BindingStateBound {
		t.Fatalf("legacy project = %+v", project)
	}

	// The legacy project has no binding row yet: EnsurePrimaryBinding backfills
	// revision 1 from the root the project already had.
	if _, err := svc.ActivePrimaryBinding(ctx, "legacy-project"); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("legacy project should have no binding before the backfill, got %v", err)
	}
	backfilled, err := svc.EnsurePrimaryBinding(ctx, "legacy-project")
	if err != nil {
		t.Fatalf("EnsurePrimaryBinding failed: %v", err)
	}
	if backfilled.Snapshot.CanonicalRoot != canonical || backfilled.Snapshot.Revision != 1 {
		t.Fatalf("backfilled binding = %+v", backfilled.Snapshot)
	}

	// Ordinary saves still work on the upgraded database.
	if _, err := svc.UpdateProject(ctx, "legacy-project", &ProjectUpdateRequest{
		Title: func(v string) *string { return &v }("legacy renamed"),
	}); err != nil {
		t.Fatalf("UpdateProject on the upgraded database failed: %v", err)
	}
	if got := svc.mustPrimaryForTest(t, "legacy-project"); got.Snapshot.BindingID != backfilled.Snapshot.BindingID {
		t.Fatalf("binding changed: %s -> %s", backfilled.Snapshot.BindingID, got.Snapshot.BindingID)
	}
}

// mustPrimaryForTest is mustPrimary on a bare *SQLiteProjectService, so the
// legacy test does not need the full env fixture.
func (s *SQLiteProjectService) mustPrimaryForTest(t *testing.T, projectID string) WorkspaceBindingRecord {
	t.Helper()
	record, err := s.ActivePrimaryBinding(context.Background(), projectID)
	if err != nil {
		t.Fatalf("ActivePrimaryBinding(%s) failed: %v", projectID, err)
	}
	return record
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// ensure the sql import is used even if a future edit drops the direct uses.
var _ = sql.ErrNoRows
