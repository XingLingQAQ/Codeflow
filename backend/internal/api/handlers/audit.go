// Package handlers - Audit API handlers
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/gin-gonic/gin"
)

// GetAuditLogs retrieves audit logs with filtering and pagination.
// GET /api/v1/audit/logs
func GetAuditLogs(c *gin.Context) {
	svc := audit.GetAuditService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit service not available"})
		return
	}

	// Parse query parameters
	query := &audit.AuditQuery{}

	if startTime := c.Query("start_time"); startTime != "" {
		if val, err := strconv.ParseInt(startTime, 10, 64); err == nil {
			query.StartTime = val
		}
	}

	if endTime := c.Query("end_time"); endTime != "" {
		if val, err := strconv.ParseInt(endTime, 10, 64); err == nil {
			query.EndTime = val
		}
	}

	if actorID := c.Query("actor_id"); actorID != "" {
		query.ActorID = actorID
	}

	if resourceID := c.Query("resource_id"); resourceID != "" {
		query.ResourceID = resourceID
	}

	if resourceType := c.Query("resource_type"); resourceType != "" {
		query.ResourceType = resourceType
	}

	if outcome := c.Query("outcome"); outcome != "" {
		query.Outcome = audit.AuditOutcome(outcome)
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit > 1000 {
		limit = 1000
	}
	query.Limit = limit

	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	query.Offset = offset

	result, err := svc.Query(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// VerifyAuditChain verifies the integrity of the audit log chain.
// POST /api/v1/audit/verify
func VerifyAuditChain(c *gin.Context) {
	svc := audit.GetAuditService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit service not available"})
		return
	}

	result, err := svc.VerifyChain(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetAuditStatistics returns audit statistics.
// GET /api/v1/audit/statistics
func GetAuditStatistics(c *gin.Context) {
	svc := audit.GetAuditService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit service not available"})
		return
	}

	stats, err := svc.GetStatistics(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// ExportAuditLogs exports audit logs in JSON format.
// GET /api/v1/audit/export
func ExportAuditLogs(c *gin.Context) {
	svc := audit.GetAuditService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit service not available"})
		return
	}

	// Query all logs (with reasonable limit)
	query := &audit.AuditQuery{
		Limit: 10000,
	}

	result, err := svc.Query(c.Request.Context(), query)
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=audit-logs-%d.json", getCurrentTimestamp()))

	// Export as JSON
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", data)
}

func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}
