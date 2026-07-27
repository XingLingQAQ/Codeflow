// Package handlers - Guard exemption-request approval flow (experimental).
package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/guard"
)

type createExemptionRequestBody struct {
	Path       string `json:"path" binding:"required"`
	RuleID     string `json:"rule_id"`
	Reason     string `json:"reason" binding:"required"`
	Requester  string `json:"requester" binding:"required"`
	TTLSeconds int    `json:"ttl_seconds"`
}

type decideExemptionRequestBody struct {
	Approve   bool   `json:"approve" binding:"required"`
	DecidedBy string `json:"decided_by"`
	Reason    string `json:"reason"`
}

// CreateExemptionRequest handles POST /api/v1/guard/exemption-requests
func CreateExemptionRequest(c *gin.Context) {
	eng, ok := guard.GetService().(*guard.Engine)
	if !ok || eng == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	var body createExemptionRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	var ttl time.Duration
	if body.TTLSeconds > 0 {
		ttl = time.Duration(body.TTLSeconds) * time.Second
	}
	req := guard.ExemptionRequest{
		Path:      body.Path,
		RuleID:    guard.RuleID(body.RuleID),
		Reason:    body.Reason,
		Requester: body.Requester,
		TTL:       ttl,
	}
	result, err := eng.RequestExemption(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondCreated(c, result)
}

// ListExemptionRequests handles GET /api/v1/guard/exemption-requests?status=
func ListExemptionRequests(c *gin.Context) {
	eng, ok := guard.GetService().(*guard.Engine)
	if !ok || eng == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	status := guard.RequestStatus(strings.TrimSpace(c.Query("status")))
	items := eng.ListExemptionRequests(status)
	respondOK(c, gin.H{"items": items, "total": len(items)})
}

// DecideExemptionRequest handles POST /api/v1/guard/exemption-requests/:id/decide
func DecideExemptionRequest(c *gin.Context) {
	eng, ok := guard.GetService().(*guard.Engine)
	if !ok || eng == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	id := c.Param("id")
	var body decideExemptionRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	result, err := eng.DecideExemptionRequest(c.Request.Context(), id, body.Approve, body.DecidedBy, body.Reason)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "already decided") {
			respondError(c, http.StatusConflict, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, result)
}
