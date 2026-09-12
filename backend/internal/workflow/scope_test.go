package workflow

// Tests for ResolveProjectScope (T13.01.a / E-01): the resolver must return
// exactly the authoritative plan/task/session/agent sets of a project on an
// A/B/empty fixture, reject explicit sessions that belong to another project
// (or to no project), never widen via audit content, URL session_id, or the
// global agent list, and treat a nil audit service as a no-op.
//
// Fixtures use fakes with embedded interfaces (only the methods the resolver
// calls are implemented; any unexpected call panics on the nil embedded
// interface). The real InMemoryProjectService is unsuitable here because its
// GetProjectPlans reads the process-global planner singleton, which would leak
// state between tests.

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
)

type fakeScopeProjects struct {
	project.IProjectService
	projects map[string]*project.Project
	plans    map[string][]planner.Plan
}

func (f *fakeScopeProjects) GetProject(ctx context.Context, id string) (*project.Project, error) {
	return f.projects[id], nil
}

func (f *fakeScopeProjects) GetProjectPlans(ctx context.Context, projectID string) ([]planner.Plan, error) {
	return f.plans[projectID], nil
}

type fakeScopePlanner struct {
	planner.IPlanner
	tasks map[string][]planner.Task
}

func (f *fakeScopePlanner) ListTasks(ctx context.Context, planID string, req *planner.TaskListRequest) (*planner.TaskListResponse, error) {
	tasks := f.tasks[planID]
	return &planner.TaskListResponse{Tasks: tasks, Total: len(tasks)}, nil
}

type fakeScopeAgents struct {
	agent.IAgentService
	agents []agent.Agent
}

func (f *fakeScopeAgents) ListAgents(ctx context.Context) (*agent.AgentListResponse, error) {
	return &agent.AgentListResponse{Agents: f.agents, Total: len(f.agents)}, nil
}

// fakeScopeAudit returns caller-controlled entries and counts invocations so
// tests can prove the resolver never consults audit content.
type fakeScopeAudit struct {
	queries int
	entries []audit.AuditLogEntry
}

func (f *fakeScopeAudit) Query(ctx context.Context, query *audit.AuditQuery) (*audit.AuditQueryResult, error) {
	f.queries++
	return &audit.AuditQueryResult{Entries: f.entries, Total: len(f.entries)}, nil
}

type scopeFixture struct {
	deps  ScopeDependencies
	audit *fakeScopeAudit
}

// newScopeFixture builds the A/B/empty matrix:
//   - proj-a: default session sess-a-default; plan-a1 (session_id sess-a-plan)
//     with task-a1 (session_id sess-a-task) and task-a2 (no session); plan-a2
//     with task-a3 (workflow_session_id sess-a-task2). Agents agent-a1/agent-a2
//     are bound to A sessions.
//   - proj-b: default session sess-b-default; plan-b1 (session_id sess-b-plan)
//     with task-b1 (sessionId sess-b-task). Agent agent-b1 bound to B.
//   - proj-empty: no sessions, plans, tasks, or agents.
//   - Decoys: agent-shared/agent-nosession belong to no project; audit entries
//     reference proj-b, sess-shared, and proj-a with a session (sess-audit-only)
//     no authority binds to A.
func newScopeFixture() *scopeFixture {
	projects := &fakeScopeProjects{
		projects: map[string]*project.Project{
			"proj-a":     {ID: "proj-a", Title: "A", DefaultSessionID: "sess-a-default"},
			"proj-b":     {ID: "proj-b", Title: "B", DefaultSessionID: "sess-b-default"},
			"proj-empty": {ID: "proj-empty", Title: "Empty"},
		},
		plans: map[string][]planner.Plan{
			"proj-a": {
				{ID: "plan-a1", Metadata: map[string]interface{}{"session_id": "sess-a-plan"}},
				{ID: "plan-a2"},
			},
			"proj-b": {
				{ID: "plan-b1", Metadata: map[string]interface{}{"session_id": "sess-b-plan"}},
			},
		},
	}
	tasks := &fakeScopePlanner{tasks: map[string][]planner.Task{
		"plan-a1": {
			{ID: "task-a1", PlanID: "plan-a1", Metadata: map[string]interface{}{"session_id": "sess-a-task"}},
			{ID: "task-a2", PlanID: "plan-a1"},
		},
		"plan-a2": {
			{ID: "task-a3", PlanID: "plan-a2", Metadata: map[string]interface{}{"workflow_session_id": "sess-a-task2"}},
		},
		"plan-b1": {
			{ID: "task-b1", PlanID: "plan-b1", Metadata: map[string]interface{}{"sessionId": "sess-b-task"}},
		},
	}}
	agents := &fakeScopeAgents{agents: []agent.Agent{
		{ID: "agent-a1", SessionID: "sess-a-default"},
		{ID: "agent-a2", SessionID: "sess-a-task"},
		{ID: "agent-b1", SessionID: "sess-b-default"},
		{ID: "agent-shared", SessionID: "sess-shared"},
		{ID: "agent-nosession"},
	}}
	auditFake := &fakeScopeAudit{entries: []audit.AuditLogEntry{
		{
			ID:       "audit-b",
			Resource: audit.AuditResource{Type: "project", ID: "proj-b"},
			Trace:    &audit.AuditTrace{ProjectID: "proj-b", SessionID: "sess-b-default"},
		},
		{
			ID:       "audit-a-foreign-session",
			Resource: audit.AuditResource{Type: "project", ID: "proj-a"},
			Trace:    &audit.AuditTrace{ProjectID: "proj-a", SessionID: "sess-audit-only"},
		},
		{
			ID:      "audit-shared",
			Details: map[string]interface{}{"session_id": "sess-shared"},
		},
	}}
	return &scopeFixture{
		deps: ScopeDependencies{
			Projects: projects,
			Planner:  tasks,
			Agents:   agents,
			Audit:    auditFake,
		},
		audit: auditFake,
	}
}

func assertIDs(t *testing.T, label string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s=%v want %v", label, got, want)
	}
}

func TestResolveProjectScopeProjectA(t *testing.T) {
	f := newScopeFixture()
	scope, err := ResolveProjectScope(context.Background(), f.deps, "proj-a", ScopeOptions{})
	if err != nil {
		t.Fatalf("ResolveProjectScope: %v", err)
	}
	if scope.ProjectID != "proj-a" {
		t.Fatalf("ProjectID=%q want proj-a", scope.ProjectID)
	}
	assertIDs(t, "PlanIDs", scope.PlanIDs, []string{"plan-a1", "plan-a2"})
	assertIDs(t, "TaskIDs", scope.TaskIDs, []string{"task-a1", "task-a2", "task-a3"})
	assertIDs(t, "SessionIDs", scope.SessionIDs,
		[]string{"sess-a-default", "sess-a-plan", "sess-a-task", "sess-a-task2"})
	assertIDs(t, "AgentIDs", scope.AgentIDs, []string{"agent-a1", "agent-a2"})
	if scope.Empty {
		t.Fatal("proj-a scope must not be Empty")
	}
	if !scope.HasSession("sess-a-task") || scope.HasSession("sess-b-task") {
		t.Fatal("HasSession must follow the authorized set exactly")
	}
}

func TestResolveProjectScopeProjectBIsolatedFromA(t *testing.T) {
	f := newScopeFixture()
	scope, err := ResolveProjectScope(context.Background(), f.deps, "proj-b", ScopeOptions{})
	if err != nil {
		t.Fatalf("ResolveProjectScope: %v", err)
	}
	assertIDs(t, "PlanIDs", scope.PlanIDs, []string{"plan-b1"})
	assertIDs(t, "TaskIDs", scope.TaskIDs, []string{"task-b1"})
	assertIDs(t, "SessionIDs", scope.SessionIDs,
		[]string{"sess-b-default", "sess-b-plan", "sess-b-task"})
	assertIDs(t, "AgentIDs", scope.AgentIDs, []string{"agent-b1"})
}

func TestResolveProjectScopeEmptyProject(t *testing.T) {
	f := newScopeFixture()
	scope, err := ResolveProjectScope(context.Background(), f.deps, "proj-empty", ScopeOptions{})
	if err != nil {
		t.Fatalf("ResolveProjectScope: %v", err)
	}
	if !scope.Empty {
		t.Fatalf("proj-empty must resolve Empty=true, got %+v", scope)
	}
	assertIDs(t, "PlanIDs", scope.PlanIDs, []string{})
	assertIDs(t, "TaskIDs", scope.TaskIDs, []string{})
	assertIDs(t, "SessionIDs", scope.SessionIDs, []string{})
	// Explicitly forbidden: falling back to the global agent list when the
	// project has no sessions.
	assertIDs(t, "AgentIDs", scope.AgentIDs, []string{})
}

func TestResolveProjectScopeProjectNotFound(t *testing.T) {
	f := newScopeFixture()
	scope, err := ResolveProjectScope(context.Background(), f.deps, "proj-missing", ScopeOptions{})
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("err=%v must match ErrProjectNotFound", err)
	}
	if errors.Is(err, ErrSessionNotInProject) {
		t.Fatalf("err=%v must not match ErrSessionNotInProject", err)
	}
	if scope != nil {
		t.Fatalf("scope must be nil on error, got %+v", scope)
	}
}

func TestResolveProjectScopeExplicitSessionNotInProject(t *testing.T) {
	f := newScopeFixture()
	cases := map[string][]string{
		// B's default/plan/task sessions, a session only some agent claims, a
		// session only present in audit content, and one that exists nowhere.
		"proj-a":     {"sess-b-default", "sess-b-plan", "sess-b-task", "sess-shared", "sess-audit-only", "sess-never-existed"},
		"proj-empty": {"sess-shared", "sess-a-default"},
	}
	for projectID, sessions := range cases {
		for _, sessionID := range sessions {
			scope, err := ResolveProjectScope(context.Background(), f.deps, projectID, ScopeOptions{SessionID: sessionID})
			if err == nil {
				t.Fatalf("%s + %s: expected rejection", projectID, sessionID)
			}
			if !errors.Is(err, ErrSessionNotInProject) {
				t.Fatalf("%s + %s: err=%v must match ErrSessionNotInProject", projectID, sessionID, err)
			}
			if errors.Is(err, ErrProjectNotFound) {
				t.Fatalf("%s + %s: err=%v must not match ErrProjectNotFound", projectID, sessionID, err)
			}
			var mismatch *SessionNotInProjectError
			if !errors.As(err, &mismatch) {
				t.Fatalf("%s + %s: err=%v must be *SessionNotInProjectError", projectID, sessionID, err)
			}
			if mismatch.ProjectID != projectID || mismatch.SessionID != sessionID {
				t.Fatalf("mismatch carries (%q,%q) want (%q,%q)",
					mismatch.ProjectID, mismatch.SessionID, projectID, sessionID)
			}
			if scope != nil {
				t.Fatalf("%s + %s: scope must be nil on rejection, got %+v", projectID, sessionID, scope)
			}
		}
	}
}

func TestResolveProjectScopeExplicitSessionInProject(t *testing.T) {
	f := newScopeFixture()
	baseline, err := ResolveProjectScope(context.Background(), f.deps, "proj-a", ScopeOptions{})
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	for _, sessionID := range []string{"sess-a-default", "sess-a-plan", "sess-a-task", "sess-a-task2"} {
		scope, err := ResolveProjectScope(context.Background(), f.deps, "proj-a", ScopeOptions{SessionID: sessionID})
		if err != nil {
			t.Fatalf("session %s: %v", sessionID, err)
		}
		// A URL session_id that is already authoritative validates but must not
		// be added again or otherwise change the authorized sets.
		if !reflect.DeepEqual(scope, baseline) {
			t.Fatalf("session %s: scope %+v differs from baseline %+v", sessionID, scope, baseline)
		}
	}
}

func TestResolveProjectScopeAuditCannotWidenAndNilIsNoop(t *testing.T) {
	f := newScopeFixture()
	withAudit, err := ResolveProjectScope(context.Background(), f.deps, "proj-a", ScopeOptions{})
	if err != nil {
		t.Fatalf("with audit: %v", err)
	}
	if f.audit.queries != 0 {
		t.Fatalf("resolver consulted audit %d times; membership must be audit-independent", f.audit.queries)
	}

	nilAuditDeps := f.deps
	nilAuditDeps.Audit = nil
	withoutAudit, err := ResolveProjectScope(context.Background(), nilAuditDeps, "proj-a", ScopeOptions{})
	if err != nil {
		t.Fatalf("nil audit: %v", err)
	}
	if !reflect.DeepEqual(withAudit, withoutAudit) {
		t.Fatalf("nil audit changed scope: %+v vs %+v", withoutAudit, withAudit)
	}
}

func TestResolveProjectScopeNilAgentsService(t *testing.T) {
	f := newScopeFixture()
	deps := f.deps
	deps.Agents = nil
	scope, err := ResolveProjectScope(context.Background(), deps, "proj-a", ScopeOptions{})
	if err != nil {
		t.Fatalf("ResolveProjectScope: %v", err)
	}
	assertIDs(t, "AgentIDs", scope.AgentIDs, []string{})
	assertIDs(t, "SessionIDs", scope.SessionIDs,
		[]string{"sess-a-default", "sess-a-plan", "sess-a-task", "sess-a-task2"})
	if scope.Empty {
		t.Fatal("sessions/plans/tasks still authorize a non-empty scope without agents")
	}
}

func TestResolveProjectScopeUnavailableDeps(t *testing.T) {
	f := newScopeFixture()
	if _, err := ResolveProjectScope(context.Background(), ScopeDependencies{
		Planner: f.deps.Planner,
	}, "proj-a", ScopeOptions{}); err == nil {
		t.Fatal("nil Projects must fail")
	}
	if _, err := ResolveProjectScope(context.Background(), ScopeDependencies{
		Projects: f.deps.Projects,
	}, "proj-a", ScopeOptions{}); err == nil {
		t.Fatal("nil Planner must fail")
	}
}
