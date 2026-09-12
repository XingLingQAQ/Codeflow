package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/workflow"
)

func setupWorkflowRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	{
		workflows := v1.Group("/workflows")
		{
			workflows.GET("/:projectId/overview", GetWorkflowOverview)
			workflows.GET("/:projectId/timeline", GetWorkflowTimeline)
			workflows.GET("/:projectId/replay", GetWorkflowReplay)
		}
	}

	return router
}

func TestWorkflowEndpoints(t *testing.T) {
	router := setupWorkflowRouter()
	projectSvc, err := project.NewSQLiteProjectService(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	plannerSvc, err := planner.NewSQLitePlanner(":memory:")
	if err != nil {
		t.Fatalf("NewSQLitePlanner failed: %v", err)
	}
	agentSvc := agent.NewInMemoryAgentService()
	auditStorage := audit.NewMemoryStorage()
	auditSvc := audit.NewAuditService(auditStorage)

	project.SetProjectService(projectSvc)
	planner.SetPlanner(plannerSvc)
	agent.SetAgentService(agentSvc)
	audit.SetAuditService(auditSvc)
	workflow.SetService(nil)
	defer func() {
		workflow.SetService(nil)
		project.SetProjectService(nil)
		planner.SetPlanner(nil)
		agent.SetAgentService(nil)
		audit.SetAuditService(nil)
		_ = projectSvc.Close()
		_ = plannerSvc.Close()
		_ = auditSvc.Close()
	}()

	projectBody := map[string]any{
		"title":       "Workflow Alpha",
		"description": "Minimal workflow project",
		"metadata":    map[string]any{"session_id": "sess-1"},
	}
	body, _ := json.Marshal(projectBody)
	createProjectReq, _ := http.NewRequest("POST", "/api/v1/projects", bytes.NewBuffer(body))
	createProjectReq.Header.Set("Content-Type", "application/json")
	createProjectResp := httptest.NewRecorder()
	setupProjectsRouter().ServeHTTP(createProjectResp, createProjectReq)
	assert.Equal(t, http.StatusCreated, createProjectResp.Code)
	createdProject := decodeContextResponseData[project.Project](t, createProjectResp.Body.Bytes())

	plan, err := plannerSvc.CreatePlan(context.Background(), &planner.PlanCreateRequest{
		Title: "Plan A",
		Metadata: map[string]any{
			"session_id": "sess-1",
		},
	})
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}
	assert.NoError(t, projectSvc.AddPlanToProject(context.Background(), createdProject.ID, plan.ID))

	task, err := plannerSvc.CreateTask(context.Background(), plan.ID, &planner.TaskCreateRequest{
		Title:       "Task A",
		Description: "Deliver minimal replay",
		Metadata: map[string]any{
			"session_id": "sess-1",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	_, err = plannerSvc.UpdateTask(context.Background(), plan.ID, task.ID, &planner.TaskUpdateRequest{Status: planner.TaskStatusInProgress})
	assert.NoError(t, err)

	agentSvc.RegisterAgent(&agent.Agent{
		ID:        "agent-1",
		Name:      "Workflow Agent",
		Role:      agent.RolePlanner,
		Status:    agent.AgentStatusRunning,
		Model:     "claude-opus-4-6",
		SessionID: "sess-1",
	})
	traceID := agentSvc.StartTrace("sess-1", "agent-1", "create_workflow", map[string]interface{}{
		"project_id": createdProject.ID,
		"plan_id":    plan.ID,
		"task_id":    task.ID,
	})
	agentSvc.EndTrace(traceID, "workflow trace completed", "completed")

	assert.NoError(t, auditSvc.Log(audit.ContextWithTrace(context.Background(), &audit.AuditTrace{
		SessionID: "sess-1",
		ProjectID: createdProject.ID,
		PlanID:    plan.ID,
		TaskID:    task.ID,
		AgentID:   "agent-1",
	}), &audit.AuditLogEntry{
		EventType: audit.EventApproval,
		Severity:  audit.SeverityInfo,
		Actor:     audit.AuditActor{ID: "agent-1", Type: "agent", Name: "Workflow Agent", SessionID: "sess-1"},
		Resource:  audit.AuditResource{Type: "project", ID: createdProject.ID, Name: createdProject.Title},
		Action:    "approve_workflow",
		Outcome:   audit.OutcomeSuccess,
		Details: map[string]interface{}{
			"project_id": createdProject.ID,
			"plan_id":    plan.ID,
			"task_id":    task.ID,
			"session_id": "sess-1",
			"agent_id":   "agent-1",
		},
	}))

	overviewReq, _ := http.NewRequest("GET", "/api/v1/workflows/"+createdProject.ID+"/overview", nil)
	overviewResp := httptest.NewRecorder()
	router.ServeHTTP(overviewResp, overviewReq)
	assert.Equal(t, http.StatusOK, overviewResp.Code)
	overview := decodeContextResponseData[workflow.WorkflowOverview](t, overviewResp.Body.Bytes())
	assert.Equal(t, createdProject.ID, overview.Project.ID)
	assert.Len(t, overview.Plans, 1)
	assert.Len(t, overview.Tasks, 1)
	assert.Len(t, overview.Agents, 1)
	assert.Equal(t, 1, overview.Summary.AuditCount)
	assert.Equal(t, 1, overview.Summary.InProgressTasks)

	timelineReq, _ := http.NewRequest("GET", "/api/v1/workflows/"+createdProject.ID+"/timeline", nil)
	timelineResp := httptest.NewRecorder()
	router.ServeHTTP(timelineResp, timelineReq)
	assert.Equal(t, http.StatusOK, timelineResp.Code)
	timeline := decodeContextResponseData[workflow.WorkflowTimeline](t, timelineResp.Body.Bytes())
	assert.NotEmpty(t, timeline.Events)
	// T13.01.c: the scope also authorizes the server-assigned default session,
	// so element order is not contractual — membership is. Exactly the default
	// session and the plan/task-bound "sess-1" may appear (no audit/URL widening).
	assert.Contains(t, timeline.SessionIDs, "sess-1")
	assert.Contains(t, timeline.SessionIDs, createdProject.DefaultSessionID)
	assert.Len(t, timeline.SessionIDs, 2)

	replayReq, _ := http.NewRequest("GET", "/api/v1/workflows/"+createdProject.ID+"/replay?session_id=sess-1", nil)
	replayResp := httptest.NewRecorder()
	router.ServeHTTP(replayResp, replayReq)
	assert.Equal(t, http.StatusOK, replayResp.Code)
	replay := decodeContextResponseData[workflow.WorkflowReplay](t, replayResp.Body.Bytes())
	assert.Equal(t, "sess-1", replay.SessionID)
	assert.NotEmpty(t, replay.Events)
	assert.Equal(t, 1, replay.Summary.AuditCount)
	assert.Equal(t, 1, replay.Summary.TraceCount)
	assert.NotNil(t, replay.Trace)
}

// T13.01.c (E-01) HTTP-level scope tests below. They exercise the real wiring —
// SQLite project/planner stores, in-memory agent/audit services, an isolated
// floweng engine, and the workflow routes over the {success,data,error}
// envelope — and assert at the response-body level that no foreign-project ID
// or content crosses the boundary. Each fixture owns every process global it
// touches and restores them on cleanup, so the tests pass both under -run and
// in a full-package execution.

// workflowScopeFixture is the isolated per-test wiring for the scope tests.
type workflowScopeFixture struct {
	router   *gin.Engine
	projects *project.SQLiteProjectService
	planner  *planner.SQLitePlanner
	agents   *agent.InMemoryAgentService
	audit    *audit.AuditService
	flows    *floweng.InMemoryEngine
}

func newWorkflowScopeFixture(t *testing.T) *workflowScopeFixture {
	t.Helper()
	projectSvc, err := project.NewSQLiteProjectService(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	plannerSvc, err := planner.NewSQLitePlanner(":memory:")
	if err != nil {
		t.Fatalf("NewSQLitePlanner failed: %v", err)
	}
	agentSvc := agent.NewInMemoryAgentService()
	auditSvc := audit.NewAuditService(audit.NewMemoryStorage())

	prevEngine := floweng.GetEngine()
	engine := floweng.NewInMemoryEngine(nil)
	floweng.SetEngine(engine)

	project.SetProjectService(projectSvc)
	planner.SetPlanner(plannerSvc)
	agent.SetAgentService(agentSvc)
	audit.SetAuditService(auditSvc)
	workflow.SetService(nil)
	t.Cleanup(func() {
		workflow.SetService(nil)
		project.SetProjectService(nil)
		planner.SetPlanner(nil)
		agent.SetAgentService(nil)
		audit.SetAuditService(nil)
		floweng.SetEngine(prevEngine)
		_ = projectSvc.Close()
		_ = plannerSvc.Close()
		_ = auditSvc.Close()
	})

	return &workflowScopeFixture{
		router:   setupWorkflowRouter(),
		projects: projectSvc,
		planner:  plannerSvc,
		agents:   agentSvc,
		audit:    auditSvc,
		flows:    engine,
	}
}

// createScopeProject creates one real project through the projects HTTP route;
// the server assigns its default session and default flow.
func (f *workflowScopeFixture) createScopeProject(t *testing.T, title string) project.Project {
	t.Helper()
	body := rexMustJSON(t, map[string]any{"title": title, "description": title + " description"})
	resp := rexRequest(t, setupProjectsRouter(), http.MethodPost, "/api/v1/projects", body, nil)
	var created project.Project
	rexData(t, resp, http.StatusCreated, true, &created)
	if created.ID == "" || created.DefaultSessionID == "" || created.DefaultFlowID == "" {
		t.Fatalf("created project missing server bindings: %+v", created)
	}
	return created
}

// bindSessionPlan adds a plan with one task to the project, bound to sessionID
// through the metadata channel the scope resolver treats as authoritative.
func (f *workflowScopeFixture) bindSessionPlan(t *testing.T, projectID, sessionID string) (*planner.Plan, *planner.Task) {
	t.Helper()
	ctx := context.Background()
	plan, err := f.planner.CreatePlan(ctx, &planner.PlanCreateRequest{
		Title:    "Plan " + sessionID,
		Metadata: map[string]any{"session_id": sessionID},
	})
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}
	if err := f.projects.AddPlanToProject(ctx, projectID, plan.ID); err != nil {
		t.Fatalf("AddPlanToProject failed: %v", err)
	}
	task, err := f.planner.CreateTask(ctx, plan.ID, &planner.TaskCreateRequest{
		Title:    "Task " + sessionID,
		Metadata: map[string]any{"session_id": sessionID},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	return plan, task
}

// logScopeAudit records one audit entry carrying the given project/session
// binding and a caller-chosen action marker.
func (f *workflowScopeFixture) logScopeAudit(t *testing.T, projectID, sessionID, resourceType, resourceID, action string) {
	t.Helper()
	ctx := context.Background()
	if projectID != "" || sessionID != "" {
		ctx = audit.ContextWithTrace(ctx, &audit.AuditTrace{ProjectID: projectID, SessionID: sessionID})
	}
	if err := f.audit.Log(ctx, &audit.AuditLogEntry{
		EventType: audit.EventApproval,
		Severity:  audit.SeverityInfo,
		Actor:     audit.AuditActor{ID: "agent-scope", Type: "agent", Name: "Scope Agent", SessionID: sessionID},
		Resource:  audit.AuditResource{Type: resourceType, ID: resourceID},
		Action:    action,
		Outcome:   audit.OutcomeSuccess,
	}); err != nil {
		t.Fatalf("audit Log failed: %v", err)
	}
}

func (f *workflowScopeFixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return rexRequest(t, f.router, http.MethodGet, path, nil, nil)
}

// assertBodyLacks fails when the response body contains any foreign marker:
// scope decisions must be visible in the payload, not only in the status code.
func assertBodyLacks(t *testing.T, body string, markers []string) {
	t.Helper()
	for _, marker := range markers {
		assert.NotContains(t, body, marker, "response body must not contain foreign marker %q", marker)
	}
}

func TestWorkflowReplayRejectsForeignSession(t *testing.T) {
	f := newWorkflowScopeFixture(t)
	projA := f.createScopeProject(t, "Scope Alpha")
	projB := f.createScopeProject(t, "Scope Beta")
	_, taskB := f.bindSessionPlan(t, projB.ID, "sess-b-scope")

	f.agents.RegisterAgent(&agent.Agent{
		ID:        "agent-b-scope",
		Name:      "B Scope Agent",
		Role:      agent.RolePlanner,
		Status:    agent.AgentStatusRunning,
		Model:     "test-model",
		SessionID: "sess-b-scope",
	})
	traceID := f.agents.StartTrace("sess-b-scope", "agent-b-scope", "b_tool", map[string]interface{}{
		"project_id": projB.ID,
		"task_id":    taskB.ID,
	})
	f.agents.EndTrace(traceID, "b-foreign-secret-output", "completed")
	f.logScopeAudit(t, projB.ID, "sess-b-scope", "project", projB.ID, "b_foreign_action")

	foreignMarkers := []string{"b-foreign-secret-output", "b_foreign_action", taskB.ID, "agent-b-scope", projB.DefaultFlowID}

	// Sanity: B's own replay really serves the markers, so their absence from
	// A's rejection bodies is evidence of filtering, not an empty fixture.
	bReplayResp := f.get(t, "/api/v1/workflows/"+projB.ID+"/replay?session_id=sess-b-scope")
	rexData(t, bReplayResp, http.StatusOK, true, nil)
	assert.Contains(t, bReplayResp.Body.String(), "b-foreign-secret-output")
	assert.Contains(t, bReplayResp.Body.String(), "b_foreign_action")

	for _, foreign := range []string{projB.DefaultSessionID, "sess-b-scope"} {
		resp := f.get(t, "/api/v1/workflows/"+projA.ID+"/replay?session_id="+foreign)
		env := rexData(t, resp, http.StatusForbidden, false, nil)
		assert.Equal(t, "Session is not authorized for this project", env.Error)
		var data map[string]string
		if err := json.Unmarshal(env.Data, &data); err != nil {
			t.Fatalf("decode 403 data: %v body=%s", err, resp.Body.String())
		}
		// The envelope names exactly the requesting project and the rejected
		// session — and nothing from project B's content.
		assert.Len(t, data, 2)
		assert.Equal(t, projA.ID, data["project_id"])
		assert.Equal(t, foreign, data["session_id"])
		assertBodyLacks(t, resp.Body.String(), foreignMarkers)
	}

	// The same project still replays normally without a foreign session.
	okResp := f.get(t, "/api/v1/workflows/"+projA.ID+"/replay")
	replay := workflow.WorkflowReplay{}
	rexData(t, okResp, http.StatusOK, true, &replay)
	assert.Equal(t, projA.DefaultSessionID, replay.SessionID)
}

func TestWorkflowUnknownProjectReturns404(t *testing.T) {
	f := newWorkflowScopeFixture(t)
	for _, suffix := range []string{"/overview", "/timeline", "/replay", "/replay?session_id=sess-x"} {
		resp := f.get(t, "/api/v1/workflows/proj-does-not-exist"+suffix)
		env := rexData(t, resp, http.StatusNotFound, false, nil)
		assert.Equal(t, "Project not found", env.Error)
	}
}

func TestWorkflowEmptyProjectHasNoGlobalAgents(t *testing.T) {
	f := newWorkflowScopeFixture(t)
	// Agents and content that belong to no project must never surface.
	f.agents.RegisterAgent(&agent.Agent{
		ID:        "agent-global-1",
		Name:      "Global Agent One",
		Role:      agent.RolePlanner,
		Status:    agent.AgentStatusRunning,
		Model:     "test-model",
		SessionID: "sess-elsewhere",
	})
	f.agents.RegisterAgent(&agent.Agent{
		ID:     "agent-global-2",
		Name:   "Global Agent Two",
		Role:   agent.RoleCoder,
		Status: agent.AgentStatusIdle,
		Model:  "test-model",
	})
	traceID := f.agents.StartTrace("sess-elsewhere", "agent-global-1", "elsewhere_tool", nil)
	f.agents.EndTrace(traceID, "elsewhere-secret-output", "completed")
	f.logScopeAudit(t, "", "sess-elsewhere", "tool", "tool-elsewhere", "elsewhere_action")

	proj := f.createScopeProject(t, "Empty Project")
	globalMarkers := []string{"agent-global-1", "agent-global-2", "Global Agent", "sess-elsewhere", "elsewhere-secret-output", "elsewhere_action"}

	overviewResp := f.get(t, "/api/v1/workflows/"+proj.ID+"/overview")
	overview := workflow.WorkflowOverview{}
	rexData(t, overviewResp, http.StatusOK, true, &overview)
	assert.Empty(t, overview.Plans)
	assert.Empty(t, overview.Tasks)
	assert.Empty(t, overview.Agents)
	assert.Equal(t, 0, overview.Summary.AgentCount)
	assert.Equal(t, 0, overview.Summary.AuditCount)
	assert.Equal(t, []string{proj.DefaultSessionID}, overview.SessionIDs)
	assertBodyLacks(t, overviewResp.Body.String(), globalMarkers)

	timelineResp := f.get(t, "/api/v1/workflows/"+proj.ID+"/timeline")
	timeline := workflow.WorkflowTimeline{}
	rexData(t, timelineResp, http.StatusOK, true, &timeline)
	assert.Equal(t, []string{proj.DefaultSessionID}, timeline.SessionIDs)
	for _, ev := range timeline.Events {
		assert.NotEqual(t, "agent", ev.Type)
		assert.NotEqual(t, "audit", ev.Type)
		assert.Empty(t, ev.AgentID)
	}
	assertBodyLacks(t, timelineResp.Body.String(), globalMarkers)

	replayResp := f.get(t, "/api/v1/workflows/"+proj.ID+"/replay")
	replay := workflow.WorkflowReplay{}
	rexData(t, replayResp, http.StatusOK, true, &replay)
	assert.Equal(t, proj.DefaultSessionID, replay.SessionID)
	assert.Nil(t, replay.Trace)
	assert.Equal(t, 0, replay.Summary.TraceCount)
	assert.Equal(t, 0, replay.Summary.AuditCount)
	assert.Empty(t, replay.Agents)
	for _, ev := range replay.Events {
		assert.Equal(t, "floweng", ev.Lane)
		assert.Equal(t, proj.ID, ev.ProjectID)
	}
	assertBodyLacks(t, replayResp.Body.String(), globalMarkers)
}

func TestWorkflowScopeFiltersAuditAndTraceTogether(t *testing.T) {
	f := newWorkflowScopeFixture(t)
	projA := f.createScopeProject(t, "Scope A")
	projB := f.createScopeProject(t, "Scope B")
	planA, taskA := f.bindSessionPlan(t, projA.ID, "sess-a-scope")
	planB, taskB := f.bindSessionPlan(t, projB.ID, "sess-b-scope")

	f.agents.RegisterAgent(&agent.Agent{
		ID:        "agent-a-scope",
		Name:      "A Scope Agent",
		Role:      agent.RolePlanner,
		Status:    agent.AgentStatusRunning,
		Model:     "test-model",
		SessionID: "sess-a-scope",
	})
	f.agents.RegisterAgent(&agent.Agent{
		ID:        "agent-b-scope",
		Name:      "B Scope Agent",
		Role:      agent.RoleCoder,
		Status:    agent.AgentStatusRunning,
		Model:     "test-model",
		SessionID: "sess-b-scope",
	})
	traceA := f.agents.StartTrace("sess-a-scope", "agent-a-scope", "a_tool", map[string]interface{}{
		"project_id": projA.ID,
		"plan_id":    planA.ID,
		"task_id":    taskA.ID,
	})
	f.agents.EndTrace(traceA, "a-trace-visible-marker", "completed")
	traceB := f.agents.StartTrace("sess-b-scope", "agent-b-scope", "b_tool", map[string]interface{}{
		"project_id": projB.ID,
		"plan_id":    planB.ID,
		"task_id":    taskB.ID,
	})
	f.agents.EndTrace(traceB, "b-trace-secret-marker", "completed")
	f.logScopeAudit(t, projA.ID, "sess-a-scope", "project", projA.ID, "a_visible_action")
	f.logScopeAudit(t, projB.ID, "sess-b-scope", "project", projB.ID, "b_secret_action")

	bMarkers := []string{
		"b-trace-secret-marker", "b_secret_action", "sess-b-scope",
		projB.ID, planB.ID, taskB.ID, "agent-b-scope",
		projB.DefaultSessionID, projB.DefaultFlowID,
	}

	// Sanity: B's own replay really serves the B markers, so their absence from
	// A's responses is evidence of filtering, not an empty fixture.
	bReplayResp := f.get(t, "/api/v1/workflows/"+projB.ID+"/replay?session_id=sess-b-scope")
	rexData(t, bReplayResp, http.StatusOK, true, nil)
	assert.Contains(t, bReplayResp.Body.String(), "b-trace-secret-marker")
	assert.Contains(t, bReplayResp.Body.String(), "b_secret_action")

	replayResp := f.get(t, "/api/v1/workflows/"+projA.ID+"/replay?session_id=sess-a-scope")
	replay := workflow.WorkflowReplay{}
	rexData(t, replayResp, http.StatusOK, true, &replay)
	assert.Equal(t, "sess-a-scope", replay.SessionID)
	assert.NotNil(t, replay.Trace)
	assert.Equal(t, 1, replay.Summary.TraceCount)
	assert.Equal(t, 1, replay.Summary.AuditCount)
	if assert.Len(t, replay.AuditEntries, 1) {
		assert.Equal(t, "a_visible_action", replay.AuditEntries[0].Action)
	}
	assert.Contains(t, replayResp.Body.String(), "a-trace-visible-marker")
	assert.Contains(t, replayResp.Body.String(), "a_visible_action")
	assertBodyLacks(t, replayResp.Body.String(), bMarkers)

	overviewResp := f.get(t, "/api/v1/workflows/"+projA.ID+"/overview")
	overview := workflow.WorkflowOverview{}
	rexData(t, overviewResp, http.StatusOK, true, &overview)
	assert.Equal(t, 1, overview.Summary.AuditCount)
	if assert.Len(t, overview.Agents, 1) {
		assert.Equal(t, "agent-a-scope", overview.Agents[0].ID)
	}
	assertBodyLacks(t, overviewResp.Body.String(), bMarkers)

	timelineResp := f.get(t, "/api/v1/workflows/"+projA.ID+"/timeline")
	timeline := workflow.WorkflowTimeline{}
	rexData(t, timelineResp, http.StatusOK, true, &timeline)
	assert.Contains(t, timeline.SessionIDs, "sess-a-scope")
	assert.Contains(t, timeline.SessionIDs, projA.DefaultSessionID)
	assertBodyLacks(t, timelineResp.Body.String(), bMarkers)
}

func TestWorkflowValidSessionKeepsFlowEvents(t *testing.T) {
	f := newWorkflowScopeFixture(t)
	projA := f.createScopeProject(t, "Flow A")
	projB := f.createScopeProject(t, "Flow B")
	f.bindSessionPlan(t, projA.ID, "sess-a-flow")
	f.agents.RegisterAgent(&agent.Agent{
		ID:        "agent-a-flow",
		Name:      "A Flow Agent",
		Role:      agent.RolePlanner,
		Status:    agent.AgentStatusRunning,
		Model:     "test-model",
		SessionID: "sess-a-flow",
	})
	// Both projects advance their default flows so each has runtime events; a
	// valid session replay must keep its own project's flow events.
	if _, err := f.flows.Advance(context.Background(), projA.DefaultFlowID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatalf("Advance projA flow failed: %v", err)
	}
	if _, err := f.flows.Advance(context.Background(), projB.DefaultFlowID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatalf("Advance projB flow failed: %v", err)
	}

	replayResp := f.get(t, "/api/v1/workflows/"+projA.ID+"/replay?session_id=sess-a-flow")
	replay := workflow.WorkflowReplay{}
	rexData(t, replayResp, http.StatusOK, true, &replay)
	assert.Equal(t, "sess-a-flow", replay.SessionID)
	flowengReplay := 0
	for _, ev := range replay.Events {
		if ev.Lane != "floweng" {
			continue
		}
		flowengReplay++
		assert.Equal(t, projA.ID, ev.ProjectID)
		assert.Equal(t, "flow:"+projA.DefaultFlowID, ev.Evidence)
	}
	assert.Positive(t, flowengReplay, "valid session replay must keep its project's flow events")
	assert.NotContains(t, replayResp.Body.String(), projB.DefaultFlowID)

	timelineResp := f.get(t, "/api/v1/workflows/"+projA.ID+"/timeline")
	timeline := workflow.WorkflowTimeline{}
	rexData(t, timelineResp, http.StatusOK, true, &timeline)
	flowengTimeline := 0
	for _, ev := range timeline.Events {
		if ev.Lane != "floweng" {
			continue
		}
		flowengTimeline++
		assert.Equal(t, projA.ID, ev.ProjectID)
	}
	assert.Positive(t, flowengTimeline, "timeline must keep its project's flow events")
	assert.NotContains(t, timelineResp.Body.String(), projB.DefaultFlowID)
}
