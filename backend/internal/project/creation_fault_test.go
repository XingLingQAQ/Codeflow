package project

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/storage"
)

// resetIdempotentCreationCache clears the process-global idempotent creation
// success cache. Cache entries are keyed by the service pointer address (%p)
// and outlive the services they describe: a garbage-collected service's
// address can be reused by a fresh service, which would then be answered from
// a stale entry. Tests reset the cache before and after they run so the
// package stays hermetic under -count>1.
func resetIdempotentCreationCache() {
	idempotencyMu.Lock()
	defer idempotencyMu.Unlock()
	clear(idempotentCreations)
}

// faultOnceSessionProvisioner wraps a real SessionProvisioner and injects one
// error into the next CreateDefaultSession call. The underlying session
// storage stays real; only the commit boundary is faulted.
type faultOnceSessionProvisioner struct {
	inner      SessionProvisioner
	failCreate bool
}

func (p *faultOnceSessionProvisioner) CreateDefaultSession(ctx context.Context, projectID, title string) (*storage.Session, error) {
	if p.failCreate {
		p.failCreate = false
		return nil, errors.New("injected session commit fault")
	}
	return p.inner.CreateDefaultSession(ctx, projectID, title)
}

func (p *faultOnceSessionProvisioner) GetSession(ctx context.Context, sessionID string) (*storage.Session, error) {
	return p.inner.GetSession(ctx, sessionID)
}

func (p *faultOnceSessionProvisioner) DeleteSession(ctx context.Context, sessionID string) error {
	return p.inner.DeleteSession(ctx, sessionID)
}

// faultOnceFlowEngine wraps a real flow engine and injects one error into the
// next Create call, before the real engine commits anything.
type faultOnceFlowEngine struct {
	floweng.Engine
	failCreate bool
}

func (e *faultOnceFlowEngine) Create(ctx context.Context, req *floweng.CreateFlowRequest) (*floweng.Flow, error) {
	if e.failCreate {
		e.failCreate = false
		return nil, errors.New("injected flow commit fault")
	}
	return e.Engine.Create(ctx, req)
}

// faultOnceProjectService wraps a real *SQLiteProjectService and injects one
// error into the next CreateProject commit, or into the next
// SaveCreateOperation journal commit carrying failSaveState. The journal and
// project tables stay real SQLite; only the named commit point is faulted.
type faultOnceProjectService struct {
	*SQLiteProjectService
	failCreateProject bool
	failSaveState     string
}

func (s *faultOnceProjectService) CreateProject(ctx context.Context, req *ProjectCreateRequest) (*Project, error) {
	if s.failCreateProject {
		s.failCreateProject = false
		return nil, errors.New("injected project commit fault")
	}
	return s.SQLiteProjectService.CreateProject(ctx, req)
}

func (s *faultOnceProjectService) SaveCreateOperation(ctx context.Context, key string, record *CreateOperationRecord) error {
	if s.failSaveState != "" && record != nil && record.State == s.failSaveState {
		s.failSaveState = ""
		return errors.New("injected journal commit fault")
	}
	return s.SQLiteProjectService.SaveCreateOperation(ctx, key, record)
}

// creationFaultEnv wires the three durable stores a create saga touches —
// SQLite project journal, SQLite flow engine, SQLite session storage — behind
// one-shot fault injectors, so every commit point can be crashed exactly once
// while all persistence stays real.
type creationFaultEnv struct {
	dir         string
	projects    *faultOnceProjectService
	rawProjects *SQLiteProjectService
	flows       *faultOnceFlowEngine
	rawFlows    *floweng.InMemoryEngine
	sessions    *storage.SessionStorage
	provisioner *faultOnceSessionProvisioner
}

func newCreationFaultEnv(t *testing.T) *creationFaultEnv {
	t.Helper()
	resetIdempotentCreationCache()
	env := &creationFaultEnv{dir: t.TempDir()}
	env.open(t)
	t.Cleanup(func() {
		env.close()
		resetIdempotentCreationCache()
	})
	return env
}

func (env *creationFaultEnv) open(t *testing.T) {
	t.Helper()
	projects, err := NewSQLiteProjectService(filepath.Join(env.dir, "project.db"))
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	flows, err := floweng.NewSQLiteEngine(filepath.Join(env.dir, "floweng.db"), nil)
	if err != nil {
		_ = projects.Close()
		t.Fatalf("NewSQLiteEngine failed: %v", err)
	}
	sessions, err := storage.NewSessionStorage(filepath.Join(env.dir, "sessions.db"))
	if err != nil {
		_ = projects.Close()
		_ = flows.Close()
		t.Fatalf("NewSessionStorage failed: %v", err)
	}
	env.rawProjects = projects
	env.projects = &faultOnceProjectService{SQLiteProjectService: projects}
	env.rawFlows = flows
	env.flows = &faultOnceFlowEngine{Engine: flows}
	env.sessions = sessions
	env.provisioner = &faultOnceSessionProvisioner{inner: storageSessionProvisioner{store: sessions}}
	SetSessionProvisioner(env.provisioner)
}

func (env *creationFaultEnv) close() {
	SetSessionProvisioner(nil)
	if env.rawProjects != nil {
		_ = env.rawProjects.Close()
	}
	if env.rawFlows != nil {
		_ = env.rawFlows.Close()
	}
	if env.sessions != nil {
		_ = env.sessions.Close()
	}
}

// reopen simulates a process restart: every store is closed and reopened over
// the same database files, so only durable state survives.
func (env *creationFaultEnv) reopen(t *testing.T) {
	t.Helper()
	env.close()
	env.open(t)
}

func (env *creationFaultEnv) assertFaultConsumed(t *testing.T) {
	t.Helper()
	if env.projects.failCreateProject || env.projects.failSaveState != "" || env.flows.failCreate || env.provisioner.failCreate {
		t.Fatal("injected fault was never consumed: the first attempt did not hit the intended commit point")
	}
}

// assertExactlyOneCreation proves the saga left exactly one project, one flow
// and one session — no duplicates, no orphans — with mutually consistent
// bindings, a completed journal record, and a ready reconcile projection.
func (env *creationFaultEnv) assertExactlyOneCreation(t *testing.T, key string) *CreateOperationRecord {
	t.Helper()
	ctx := context.Background()
	record, err := env.projects.GetCreateOperation(ctx, key)
	if err != nil || record == nil {
		t.Fatalf("journaled operation %q missing: record=%+v err=%v", key, record, err)
	}
	if record.State != createOperationCompleted {
		t.Fatalf("operation state=%q, want completed", record.State)
	}
	projectID := record.ProjectID
	project, err := env.projects.GetProject(ctx, projectID)
	if err != nil || project == nil {
		t.Fatalf("project %s not persisted: %+v err=%v", projectID, project, err)
	}
	listed, err := env.projects.ListProjects(ctx, &ProjectListRequest{})
	if err != nil || listed == nil || listed.Total != 1 {
		t.Fatalf("projects=%+v err=%v, want exactly one", listed, err)
	}
	projectFlows, err := env.flows.List(ctx, projectID)
	if err != nil || len(projectFlows) != 1 {
		t.Fatalf("project flows=%d err=%v, want exactly one", len(projectFlows), err)
	}
	allFlows, err := env.flows.ListByStatus(ctx, "", "")
	if err != nil || len(allFlows) != 1 {
		t.Fatalf("engine flows=%d err=%v, want exactly one (no orphans)", len(allFlows), err)
	}
	if projectFlows[0].ID != record.FlowID || allFlows[0].ID != record.FlowID {
		t.Fatalf("flow ids diverge: project flow=%s engine flow=%s journaled=%s", projectFlows[0].ID, allFlows[0].ID, record.FlowID)
	}
	allSessions, err := env.sessions.GetAllSessions(nil)
	if err != nil || len(allSessions) != 1 {
		t.Fatalf("sessions=%d err=%v, want exactly one", len(allSessions), err)
	}
	if allSessions[0].ID != record.SessionID || allSessions[0].ID != "project-"+projectID {
		t.Fatalf("session id=%s journaled=%s, want project-%s exactly once", allSessions[0].ID, record.SessionID, projectID)
	}
	if project.DefaultFlowID != record.FlowID || project.DefaultSessionID != record.SessionID {
		t.Fatalf("project bindings flow=%s session=%s, journaled flow=%s session=%s", project.DefaultFlowID, project.DefaultSessionID, record.FlowID, record.SessionID)
	}
	projection, err := ReconcileCreateOperation(ctx, env.projects, env.flows, record)
	if err != nil {
		t.Fatal(err)
	}
	assertProjection(t, projection, CreateOperationProjectionReady, nil)
	return record
}

// TestCreationDifferentPayloadConflicts is the service-layer half of the §28
// acceptance pair: a replay under the same idempotency key with a different
// payload is a deterministic typed conflict — in-process and after a reopen —
// while the original payload still replays the original operation and no
// duplicate resources are ever provisioned. The HTTP-layer half lives in
// backend/internal/api/handlers (409 mapping).
func TestCreationDifferentPayloadConflicts(t *testing.T) {
	ctx := context.Background()
	env := newCreationFaultEnv(t)

	const key = "conflict-service-key"
	original := &ProjectCreateRequest{Title: "Alpha", Tags: []string{"one", "two"}}
	created, err := CreateProjectWithDefaultFlowIdempotent(ctx, env.projects, env.flows, original, key)
	if err != nil {
		t.Fatal(err)
	}

	changed := &ProjectCreateRequest{Title: "Beta", Tags: []string{"one", "two"}}
	assertConflict := func(t *testing.T, err error) {
		t.Helper()
		var conflictErr *ProjectCreateConflictError
		if !errors.As(err, &conflictErr) {
			t.Fatalf("different payload replay error=%v, want *ProjectCreateConflictError", err)
		}
		if !errors.Is(err, ErrProjectCreateConflict) {
			t.Fatalf("error %v does not unwrap to ErrProjectCreateConflict", err)
		}
		if conflictErr.IdempotencyKey != key || conflictErr.ProjectID != created.ID {
			t.Fatalf("conflict identity=%q/%q, want %s/%s", conflictErr.IdempotencyKey, conflictErr.ProjectID, key, created.ID)
		}
		if len(conflictErr.DifferingFields) != 1 || conflictErr.DifferingFields[0] != "title" {
			t.Fatalf("differing fields=%v, want [title]", conflictErr.DifferingFields)
		}
		if conflictErr.AcceptedHash == "" || conflictErr.RejectedHash == "" || conflictErr.AcceptedHash == conflictErr.RejectedHash {
			t.Fatalf("conflict hashes accepted=%q rejected=%q, want two distinct non-empty hashes", conflictErr.AcceptedHash, conflictErr.RejectedHash)
		}
	}

	_, err = CreateProjectWithDefaultFlowIdempotent(ctx, env.projects, env.flows, changed, key)
	assertConflict(t, err)

	// The durable evidence: after a full reopen the in-process success cache is
	// bypassed, the journaled operation still rejects the different payload and
	// still replays the original one.
	env.reopen(t)
	_, err = CreateProjectWithDefaultFlowIdempotent(ctx, env.projects, env.flows, changed, key)
	assertConflict(t, err)
	replayed, err := CreateProjectWithDefaultFlowIdempotent(ctx, env.projects, env.flows, original, key)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != created.ID || replayed.Flow.ID != created.Flow.ID || replayed.Session.ID != created.Session.ID {
		t.Fatalf("same-payload replay returned different resources: %+v vs %+v", replayed, created)
	}

	env.assertExactlyOneCreation(t, key)
}

// TestReconcileNeverDuplicatesSession injects a one-shot fault at each create
// saga commit point — session, flow, project and the journal binding commit —
// over real SQLite stores, then completes the operation either by a same-key
// client retry or by startup recovery after a simulated restart. In every
// case the reconcile-driven continuation must converge to exactly one
// project, one flow and one session with consistent bindings: no duplicate
// resources, no orphans.
func TestReconcileNeverDuplicatesSession(t *testing.T) {
	ctx := context.Background()
	points := []struct {
		name string
		arm  func(env *creationFaultEnv)
	}{
		{name: "session commit", arm: func(env *creationFaultEnv) { env.provisioner.failCreate = true }},
		{name: "flow commit", arm: func(env *creationFaultEnv) { env.flows.failCreate = true }},
		{name: "project commit", arm: func(env *creationFaultEnv) { env.projects.failCreateProject = true }},
		{name: "binding journal commit", arm: func(env *creationFaultEnv) { env.projects.failSaveState = createOperationFlowCreated }},
	}
	const key = "fault-injection-key"

	attempt := func(t *testing.T, env *creationFaultEnv) *ProjectCreateRequest {
		t.Helper()
		return &ProjectCreateRequest{Title: "Fault Tolerant", Tags: []string{"t0-11c"}}
	}

	for _, point := range points {
		t.Run(point.name+"/same-key retry", func(t *testing.T) {
			env := newCreationFaultEnv(t)
			point.arm(env)
			req := attempt(t, env)
			_, err := CreateProjectWithDefaultFlowIdempotent(ctx, env.projects, env.flows, req, key)
			if err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("first attempt err=%v, want the injected %s fault", err, point.name)
			}
			env.assertFaultConsumed(t)

			created, err := CreateProjectWithDefaultFlowIdempotent(ctx, env.projects, env.flows, req, key)
			if err != nil {
				t.Fatalf("same-key retry after injected %s fault failed: %v", point.name, err)
			}
			record := env.assertExactlyOneCreation(t, key)
			if created.ID != record.ProjectID || created.Flow.ID != record.FlowID || created.Session.ID != record.SessionID {
				t.Fatalf("retry returned %+v, journaled resources are %+v", created, record)
			}
		})

		t.Run(point.name+"/restart recovery", func(t *testing.T) {
			env := newCreationFaultEnv(t)
			point.arm(env)
			req := attempt(t, env)
			_, err := CreateProjectWithDefaultFlowIdempotent(ctx, env.projects, env.flows, req, key)
			if err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("first attempt err=%v, want the injected %s fault", err, point.name)
			}
			env.assertFaultConsumed(t)

			env.reopen(t)
			diagnostics, err := RecoverIncompleteProjectCreations(ctx, env.projects, env.flows)
			if err != nil {
				t.Fatalf("startup recovery after injected %s fault returned fatal error: %v", point.name, err)
			}
			if len(diagnostics) != 0 {
				t.Fatalf("recovery diagnostics=%v, want none: the operation is resumable", diagnostics)
			}
			record := env.assertExactlyOneCreation(t, key)

			// A client replay after recovery reuses the recovered resources;
			// it must never provision a second session.
			replayed, err := CreateProjectWithDefaultFlowIdempotent(ctx, env.projects, env.flows, req, key)
			if err != nil {
				t.Fatal(err)
			}
			if replayed.ID != record.ProjectID || replayed.Flow.ID != record.FlowID || replayed.Session.ID != record.SessionID {
				t.Fatalf("post-recovery replay returned %+v, recovered resources are %+v", replayed, record)
			}
			env.assertExactlyOneCreation(t, key)
		})
	}
}

// TestRecoverIncompleteProjectCreationsContinuesAfterFailedRecord pins the
// startup semantics of §26.17 P2: one journal record whose reconcile
// projection is failed must not block recovery of the remaining records —
// it is aggregated into the diagnostics channel and startup continues.
func TestRecoverIncompleteProjectCreationsContinuesAfterFailedRecord(t *testing.T) {
	ctx := context.Background()
	projects, flows, provisioner := newReconcileFixture(t)

	// failed-key: the persisted project is bound to a flow that no longer
	// exists, so its reconcile projection is failed and no retry can complete it.
	provisioner.sessions["s-fail"] = &storage.Session{ID: "s-fail", Title: "failed session"}
	failedFlow, err := flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "p-fail", TemplateID: floweng.TemplateNewProject, SessionID: "s-fail"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.CreateProject(ctx, &ProjectCreateRequest{ID: "p-fail", Title: "Failed", DefaultFlowID: failedFlow.ID, DefaultSessionID: "s-fail"}); err != nil {
		t.Fatal(err)
	}
	if err := flows.Delete(ctx, failedFlow.ID); err != nil {
		t.Fatal(err)
	}
	projects.records["failed-key"] = &CreateOperationRecord{IdempotencyKey: "failed-key", ProjectID: "p-fail", SessionID: "s-fail", FlowID: failedFlow.ID, State: createOperationFlowCreated, Title: "Failed"}

	// ok-key: an ordinary resumable operation interrupted after flow_created.
	provisioner.sessions["s-ok"] = &storage.Session{ID: "s-ok", Title: "ok session"}
	okFlow, err := flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "p-ok", TemplateID: floweng.TemplateNewProject, SessionID: "s-ok"})
	if err != nil {
		t.Fatal(err)
	}
	projects.records["ok-key"] = &CreateOperationRecord{IdempotencyKey: "ok-key", ProjectID: "p-ok", SessionID: "s-ok", FlowID: okFlow.ID, State: createOperationFlowCreated, Title: "OK"}

	diagnostics, err := RecoverIncompleteProjectCreations(ctx, projects, flows)
	if err != nil {
		t.Fatalf("a single failed record must not make recovery fatal: %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics=%v, want exactly the failed record", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.IdempotencyKey != "failed-key" || diagnostic.ProjectID != "p-fail" || diagnostic.State != createOperationFlowCreated {
		t.Fatalf("diagnostic=%+v, want failed-key/p-fail/flow_created", diagnostic)
	}
	if !strings.Contains(diagnostic.Error, failedFlow.ID) {
		t.Fatalf("diagnostic error %q does not explain the failure (want the missing flow id)", diagnostic.Error)
	}

	// The resumable record recovered fully; the failed one is untouched.
	if projects.records["ok-key"].State != createOperationCompleted {
		t.Fatalf("ok-key state=%q, want completed", projects.records["ok-key"].State)
	}
	okProject, err := projects.GetProject(ctx, "p-ok")
	if err != nil || okProject == nil || okProject.DefaultFlowID != okFlow.ID || okProject.DefaultSessionID != "s-ok" {
		t.Fatalf("recovered project=%+v err=%v", okProject, err)
	}
	if projects.records["failed-key"].State != createOperationFlowCreated {
		t.Fatalf("failed-key state=%q, want untouched flow_created", projects.records["failed-key"].State)
	}
	failedProject, err := projects.GetProject(ctx, "p-fail")
	if err != nil || failedProject == nil || failedProject.DefaultFlowID != failedFlow.ID {
		t.Fatalf("recovery rebound the failed project: %+v err=%v", failedProject, err)
	}

	// The diagnostics are queryable after the run, identical to the returned list.
	visible := CreateOperationRecoveryDiagnostics()
	if len(visible) != 1 || visible[0] != diagnostic {
		t.Fatalf("CreateOperationRecoveryDiagnostics=%v, want %+v", visible, diagnostic)
	}
}

// TestRecoverIncompleteProjectCreationsSkipsRolledBack pins the whitelist:
// terminal or unknown journal states — rolled_back included — are never
// listed as incomplete and never resurrected by recovery, while resumable
// states still recover. The rolled_back row is left durably untouched.
func TestRecoverIncompleteProjectCreationsSkipsRolledBack(t *testing.T) {
	ctx := context.Background()
	resetIdempotentCreationCache()
	t.Cleanup(resetIdempotentCreationCache)
	dbPath := filepath.Join(t.TempDir(), "project.db")
	svc, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	t.Cleanup(func() { svc.Close() })
	flows := floweng.NewInMemoryEngine(nil)
	provisioner := newRecoverySessionProvisioner()
	SetSessionProvisioner(provisioner)
	t.Cleanup(func() { SetSessionProvisioner(nil) })

	seed := func(key, projectID, state string) {
		t.Helper()
		if _, err := svc.db.Exec(`
			INSERT INTO project_operations (idempotency_key, project_id, flow_id, session_id, title, workspace_root, normalized_request_hash, operation, state, created_at, updated_at)
			VALUES (?, ?, NULL, NULL, ?, NULL, NULL, 'create_project', ?, 1000, 1000)
		`, key, projectID, "Seeded", state); err != nil {
			t.Fatalf("seed %s failed: %v", key, err)
		}
	}
	seed("rolled-key", "rolled-project", createOperationRolledBack)
	seed("unknown-key", "unknown-project", "archived_v0")
	seed("pending-key", "pending-project", createOperationPending)

	listed, err := svc.ListIncompleteCreateOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].IdempotencyKey != "pending-key" {
		t.Fatalf("incomplete operations=%v, want only pending-key", listed)
	}

	diagnostics, err := RecoverIncompleteProjectCreations(ctx, svc, flows)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%v, want none: excluded states are not errors", diagnostics)
	}

	pending, err := svc.GetProject(ctx, "pending-project")
	if err != nil || pending == nil {
		t.Fatalf("resumable operation was not recovered: %+v err=%v", pending, err)
	}
	for _, projectID := range []string{"rolled-project", "unknown-project"} {
		project, err := svc.GetProject(ctx, projectID)
		if err != nil {
			t.Fatal(err)
		}
		if project != nil {
			t.Fatalf("recovery resurrected %s: %+v", projectID, project)
		}
	}
	for key, wantState := range map[string]string{"rolled-key": createOperationRolledBack, "unknown-key": "archived_v0"} {
		record, err := svc.GetCreateOperation(ctx, key)
		if err != nil || record == nil {
			t.Fatalf("record %s missing after recovery: %+v err=%v", key, record, err)
		}
		if record.State != wantState {
			t.Fatalf("record %s state=%q, want untouched %q", key, record.State, wantState)
		}
	}
}

// TestRecoverIncompleteProjectCreationsFailsWhenJournalUnreadable keeps the
// one fatal case: when the journal cannot even list its rows there is nothing
// to recover or report, so startup must still abort.
func TestRecoverIncompleteProjectCreationsFailsWhenJournalUnreadable(t *testing.T) {
	ctx := context.Background()
	svc, err := NewSQLiteProjectService(filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverIncompleteProjectCreations(ctx, svc, floweng.NewInMemoryEngine(nil)); err == nil {
		t.Fatal("an unreadable journal must remain a fatal startup error")
	}
}
