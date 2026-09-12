// Package handlers - Project API handlers
package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/websocket"
)

// GetProjects handles GET /api/v1/projects
func GetProjects(c *gin.Context) {
	var req project.ProjectListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid query parameters: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	result, err := svc.ListProjects(c.Request.Context(), &req)
	if err != nil {
		respondInternalError(c, "list projects", err)
		return
	}

	respondOK(c, result)
}

// CreateProject handles POST /api/v1/projects
func CreateProject(c *gin.Context) {
	var req project.ProjectCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		key = uuid.NewString()
		c.Header("Idempotency-Key", key)
	}
	result, err := project.CreateProjectWithDefaultFlowIdempotent(
		c.Request.Context(),
		project.GetProjectService(),
		floweng.GetEngine(),
		&req,
		key,
	)
	if err != nil {
		if errors.Is(err, project.ErrInvalidProjectCreate) {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		var conflictErr *project.ProjectCreateConflictError
		if errors.As(err, &conflictErr) {
			respondProjectCreateConflict(c, conflictErr)
			return
		}

		log.Printf("[ERROR] create project with default flow: %v", err)
		var creationErr *project.ProjectCreationError
		if errors.As(err, &creationErr) && creationErr.CleanupErr != nil {
			respondError(c, http.StatusInternalServerError, "工作流初始化失败，且未能自动清理临时数据；请刷新项目列表确认后再重试")
			return
		}
		respondError(c, http.StatusInternalServerError, "工作流初始化失败，项目未创建；请重试")
		return
	}

	respondCreated(c, result)
}

// respondProjectCreateConflict answers an idempotency-key replay that carried
// a different payload with 409: the envelope names the conflicting operation
// (idempotency key and reserved project id) plus the journaled intent fields
// known to differ, so the client can decide whether to reuse the original
// operation or choose a new key.
func respondProjectCreateConflict(c *gin.Context, conflict *project.ProjectCreateConflictError) {
	fields := conflict.DifferingFields
	if fields == nil {
		fields = []string{}
	}
	c.JSON(http.StatusConflict, Response{
		Success: false,
		Error:   conflict.Error(),
		Data: gin.H{
			"idempotency_key":  conflict.IdempotencyKey,
			"project_id":       conflict.ProjectID,
			"differing_fields": fields,
		},
	})
}

// GetProject handles GET /api/v1/projects/:id
func GetProject(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	svc := project.GetProjectService()
	result, err := svc.GetProject(c.Request.Context(), id)
	if err != nil {
		respondInternalError(c, "get project", err)
		return
	}
	if result == nil {
		respondError(c, http.StatusNotFound, "Project not found")
		return
	}

	respondOK(c, result)
}

// StreamProjectEvents handles WebSocket /api/v1/projects/:id/stream.
func StreamProjectEvents(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}
	result, err := project.GetProjectService().GetProject(c.Request.Context(), id)
	if err != nil {
		respondInternalError(c, "authorize project stream", err)
		return
	}
	if result == nil {
		respondError(c, http.StatusNotFound, "Project not found")
		return
	}
	topic := "flow:project:" + id
	websocket.HandleScopedWebSocket(websocket.GetHub(), c, "project:"+id, topic)
}

// UpdateProject handles PUT /api/v1/projects/:id
func UpdateProject(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	var req project.ProjectUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	result, err := svc.UpdateProject(c.Request.Context(), id, &req)
	if err != nil {
		respondInternalError(c, "update project", err)
		return
	}

	respondOK(c, result)
}

// DeleteProject handles DELETE /api/v1/projects/:id
func DeleteProject(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	svc := project.GetProjectService()
	cleanup := func(_ context.Context, p *project.Project) error {
		StopWorkspaceWatchesForRoot(p.WorkspaceRoot)
		StopWorkspaceDevServersForRoot(p.WorkspaceRoot)
		return nil
	}
	if err := project.ArchiveProjectAndAbortFlowsWithCleanup(c.Request.Context(), svc, floweng.GetEngine(), id, cleanup); err != nil {
		respondInternalError(c, "delete project", err)
		return
	}

	respondOK(c, gin.H{"deleted": true, "id": id, "status": string(project.StatusArchived)})
}

// RestoreProject handles POST /api/v1/projects/:id/restore.
func RestoreProject(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}
	result, err := project.RestoreProjectWithDefaultFlow(c.Request.Context(), project.GetProjectService(), floweng.GetEngine(), id)
	if err != nil {
		if err.Error() == "project not found" {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondInternalError(c, "restore project", err)
		return
	}
	respondOK(c, result)
}

// BindProjectWorkspace handles POST /api/v1/projects/:id/bind-workspace.
func BindProjectWorkspace(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}
	var body struct {
		WorkspaceRoot string `json:"workspace_root" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	result, err := project.GetProjectService().BindWorkspaceRoot(c.Request.Context(), id, body.WorkspaceRoot)
	if err != nil {
		if err.Error() == "project not found" {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, result)
}

// GetProjectPlans handles GET /api/v1/projects/:id/plans
func GetProjectPlans(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	svc := project.GetProjectService()
	plans, err := svc.GetProjectPlans(c.Request.Context(), id)
	if err != nil {
		respondInternalError(c, "get project plans", err)
		return
	}

	respondOK(c, gin.H{"plans": plans, "total": len(plans)})
}

// AddPlanToProject handles POST /api/v1/projects/:id/plans
func AddPlanToProject(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	var req project.AddPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	if err := svc.AddPlanToProject(c.Request.Context(), id, req.PlanID); err != nil {
		respondInternalError(c, "add plan to project", err)
		return
	}

	respondOK(c, gin.H{"project_id": id, "plan_id": req.PlanID, "associated": true})
}

// RemovePlanFromProject handles DELETE /api/v1/projects/:id/plans/:planId

// RemovePlanFromProject handles DELETE /api/v1/projects/:id/plans/:planId
func RemovePlanFromProject(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}
	planID, ok := requireUUIDParam(c, "planId", "plan ID")
	if !ok {
		return
	}

	svc := project.GetProjectService()
	if err := svc.RemovePlanFromProject(c.Request.Context(), id, planID); err != nil {
		respondInternalError(c, "remove plan from project", err)
		return
	}

	respondOK(c, gin.H{"project_id": id, "plan_id": planID, "removed": true})
}

// GenerateProjectPlan handles POST /api/v1/projects/:id/plan
func GenerateProjectPlan(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	var req project.PlanGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	result, err := svc.GeneratePlanDocument(c.Request.Context(), id, &req)
	if err != nil {
		respondProjectPlanError(c, "generate project plan", err)
		return
	}
	recordProjectPlanAudit(c, result, "plan_generated")
	respondCreated(c, result)
}

// GetProjectPlan handles GET /api/v1/projects/:id/plan
func GetProjectPlan(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	svc := project.GetProjectService()
	result, err := svc.GetPlanDocument(c.Request.Context(), id)
	if err != nil {
		respondProjectPlanError(c, "get project plan", err)
		return
	}
	if result == nil {
		respondError(c, http.StatusNotFound, "Project plan not found")
		return
	}
	respondOK(c, result)
}

// ReviseProjectPlan handles POST /api/v1/projects/:id/plan/revise
func ReviseProjectPlan(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	var req project.PlanReviseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	result, err := svc.RevisePlanDocument(c.Request.Context(), id, &req)
	if err != nil {
		respondProjectPlanError(c, "revise project plan", err)
		return
	}
	recordProjectPlanAudit(c, result, "plan_revised")
	respondOK(c, result)
}

// ApproveProjectPlan handles POST /api/v1/projects/:id/plan/approve
func ApproveProjectPlan(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	var req project.PlanApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	result, err := svc.ApprovePlanDocument(c.Request.Context(), id, &req)
	if err != nil {
		respondProjectPlanError(c, "approve project plan", err)
		return
	}
	recordProjectPlanAudit(c, result, "plan_approved")
	respondOK(c, result)
}

// ExecuteProjectPlan handles POST /api/v1/projects/:id/plan/execute
func ExecuteProjectPlan(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "project ID")
	if !ok {
		return
	}

	svc := project.GetProjectService()
	plan, err := svc.GetPlanDocument(c.Request.Context(), id)
	if err != nil {
		respondProjectPlanError(c, "execute project plan", err)
		return
	}
	if plan == nil {
		respondError(c, http.StatusNotFound, "Project plan not found")
		return
	}
	if plan.Status != project.PlanDocumentStatusApproved {
		respondError(c, http.StatusBadRequest, "Project plan must be approved before execution")
		return
	}

	sessionID := c.GetHeader("X-Session-ID")
	if sessionID == "" {
		sessionID = c.GetHeader("X-Session-Id")
	}
	recordProjectPlanAudit(c, plan, "plan_execute")
	respondOK(c, gin.H{
		"started":    true,
		"status":     string(plan.Status),
		"session_id": sessionID,
	})
}

func respondProjectPlanError(c *gin.Context, context string, err error) {
	switch err.Error() {
	case "project not found", "plan document not found":
		respondError(c, http.StatusNotFound, err.Error())
	default:
		respondError(c, http.StatusBadRequest, err.Error())
	}
}

func recordProjectPlanAudit(c *gin.Context, plan *project.PlanDocument, action string) {
	if plan == nil {
		return
	}
	if _, err := audit.Record(c.Request.Context(), &audit.AuditLogEntry{
		EventType: audit.EventApproval,
		Severity:  audit.SeverityInfo,
		Actor: audit.AuditActor{
			ID:   plan.ApprovedBy,
			Type: "user",
			Name: plan.ApprovedBy,
		},
		Resource: audit.AuditResource{
			Type: "project_plan",
			ID:   plan.ID,
			Name: plan.Title,
		},
		Action:  action,
		Outcome: audit.OutcomeSuccess,
		Details: map[string]interface{}{
			"project_id": plan.ProjectID,
			"revision":   plan.Revision,
			"status":     string(plan.Status),
		},
	}); err != nil {
		log.Printf("[WARN] project plan audit record failed: project_id=%s plan_id=%s action=%s err=%v", plan.ProjectID, plan.ID, action, err)
	}
}
