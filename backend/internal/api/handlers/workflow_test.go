package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/audit"
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

	plan, err := plannerSvc.CreatePlan(t.Context(), &planner.PlanCreateRequest{
		Title: "Plan A",
		Metadata: map[string]any{
			"session_id": "sess-1",
		},
	})
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}
	assert.NoError(t, projectSvc.AddPlanToProject(t.Context(), createdProject.ID, plan.ID))

	task, err := plannerSvc.CreateTask(t.Context(), plan.ID, &planner.TaskCreateRequest{
		Title:       "Task A",
		Description: "Deliver minimal replay",
		Metadata: map[string]any{
			"session_id": "sess-1",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	_, err = plannerSvc.UpdateTask(t.Context(), plan.ID, task.ID, &planner.TaskUpdateRequest{Status: planner.TaskStatusInProgress})
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

	assert.NoError(t, auditSvc.Log(audit.ContextWithTrace(t.Context(), &audit.AuditTrace{
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
	assert.Equal(t, "sess-1", timeline.SessionIDs[0])

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
