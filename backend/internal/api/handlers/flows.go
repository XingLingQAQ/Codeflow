// Package handlers - Flow engine API handlers (experimental).
package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/floweng"
)

// CreateFlow handles POST /api/v1/flows
func CreateFlow(c *gin.Context) {
	var req floweng.CreateFlowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	flow, err := floweng.GetEngine().Create(c.Request.Context(), &req)
	if err != nil {
		if strings.Contains(err.Error(), "unknown template") || strings.Contains(err.Error(), "required") {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondInternalError(c, "create flow", err)
		return
	}
	respondCreated(c, flow)
}

// ListFlows handles GET /api/v1/flows?project_id=
func ListFlows(c *gin.Context) {
	projectID := c.Query("project_id")
	flows, err := floweng.GetEngine().List(c.Request.Context(), projectID)
	if err != nil {
		respondInternalError(c, "list flows", err)
		return
	}
	respondOK(c, gin.H{"items": flows, "total": len(flows)})
}

// GetFlow handles GET /api/v1/flows/:id
func GetFlow(c *gin.Context) {
	id := c.Param("id")
	flow, err := floweng.GetEngine().Get(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}
	respondOK(c, flow)
}

// AdvanceFlowStage handles POST /api/v1/flows/:id/stages/:sid/advance
// Stage id in path is validated against the current active stage (or ignored if matches any — advance always acts on active).
func AdvanceFlowStage(c *gin.Context) {
	flowID := c.Param("id")
	stageID := c.Param("sid")
	var req floweng.AdvanceRequest
	_ = c.ShouldBindJSON(&req)

	// Ensure the path stage is the active one when provided
	flow, err := floweng.GetEngine().Get(c.Request.Context(), flowID)
	if err != nil {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}
	activeID := ""
	for _, s := range flow.Stages {
		if s.Status == floweng.StageStatusActive {
			activeID = s.ID
			break
		}
	}
	if stageID != "" && activeID != "" && stageID != activeID {
		respondError(c, http.StatusConflict, "stage is not active; advance only applies to the active stage")
		return
	}

	flow, err = floweng.GetEngine().Advance(c.Request.Context(), flowID, &req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "blocked") || strings.Contains(err.Error(), "not active") {
			respondError(c, http.StatusConflict, err.Error())
			return
		}
		respondInternalError(c, "advance flow", err)
		return
	}
	respondOK(c, flow)
}

// SkipFlowStage handles POST /api/v1/flows/:id/stages/:sid/skip
func SkipFlowStage(c *gin.Context) {
	flowID := c.Param("id")
	stageID := c.Param("sid")
	flow, err := floweng.GetEngine().Skip(c.Request.Context(), flowID, &floweng.SkipRequest{StageID: stageID})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "not optional") || strings.Contains(err.Error(), "cannot be skipped") {
			respondError(c, http.StatusConflict, err.Error())
			return
		}
		respondInternalError(c, "skip stage", err)
		return
	}
	respondOK(c, flow)
}

// LoopFlow handles POST /api/v1/flows/:id/loop
func LoopFlow(c *gin.Context) {
	flowID := c.Param("id")
	var req floweng.LoopRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	flow, err := floweng.GetEngine().Loop(c.Request.Context(), flowID, &req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "not allowed") || strings.Contains(err.Error(), "must be") {
			respondError(c, http.StatusConflict, err.Error())
			return
		}
		respondInternalError(c, "loop flow", err)
		return
	}
	respondOK(c, flow)
}

// ListFlowEvents handles GET /api/v1/flows/:id/events
func ListFlowEvents(c *gin.Context) {
	flowID := c.Param("id")
	events, err := floweng.GetEngine().ListEvents(c.Request.Context(), flowID)
	if err != nil {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}
	respondOK(c, gin.H{"items": events, "total": len(events)})
}

// ListFlowTemplates handles GET /api/v1/flows/templates
func ListFlowTemplates(c *gin.Context) {
	// Prefer rich descriptions; keep ids for older clients under "ids".
	infos := floweng.ListTemplateInfos()
	ids := floweng.ListTemplates()
	respondOK(c, gin.H{"items": infos, "ids": ids, "total": len(infos)})
}

// AttachFlowArtifact handles POST /api/v1/flows/:id/stages/:sid/artifacts
func AttachFlowArtifact(c *gin.Context) {
	flowID := c.Param("id")
	stageID := c.Param("sid")
	var body struct {
		Type       string `json:"type"`
		ContentRef string `json:"content_ref"`
	}
	_ = c.ShouldBindJSON(&body)
	art, err := floweng.GetEngine().AttachArtifact(c.Request.Context(), flowID, stageID, body.Type, body.ContentRef)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondCreated(c, art)
}

// ListFlowStages handles GET /api/v1/flows/:id/stages
func ListFlowStages(c *gin.Context) {
	flow, err := floweng.GetEngine().Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}
	respondOK(c, gin.H{"items": flow.Stages, "total": len(flow.Stages)})
}

// ListFlowArtifacts handles GET /api/v1/flows/:id/artifacts
func ListFlowArtifacts(c *gin.Context) {
	flow, err := floweng.GetEngine().Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}
	respondOK(c, gin.H{"items": flow.Artifacts, "total": len(flow.Artifacts)})
}

// UpdateFlowArtifactStatus handles PATCH /api/v1/flows/:id/artifacts/:aid
func UpdateFlowArtifactStatus(c *gin.Context) {
	var body struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	art, err := floweng.GetEngine().SetArtifactStatus(c.Request.Context(), c.Param("id"), c.Param("aid"), floweng.ArtifactStatus(body.Status))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, art)
}

// AbortFlow handles POST /api/v1/flows/:id/abort
func AbortFlow(c *gin.Context) {
	flowID := c.Param("id")
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	flow, err := floweng.GetEngine().Abort(c.Request.Context(), flowID, body.Reason)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "not active") {
			respondError(c, http.StatusConflict, err.Error())
			return
		}
		respondInternalError(c, "abort flow", err)
		return
	}
	respondOK(c, flow)
}

// DecideFlowGate handles POST /api/v1/flows/:id/gates/:gid/decide
func DecideFlowGate(c *gin.Context) {
	flowID := c.Param("id")
	gateID := c.Param("gid")
	var req floweng.GateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// allow empty body for approve via query? require JSON
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	flow, err := floweng.GetEngine().DecideGate(c.Request.Context(), flowID, gateID, &req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, flow)
}
