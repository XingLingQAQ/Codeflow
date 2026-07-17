// Package handlers provides HTTP request handlers for the API.
package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/codeflow/backend/internal/audit"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
	"github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/isolation"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/privacy"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/skill"
	"github.com/codeflow/backend/internal/workspace"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const backendVersion = "0.1.0"

type readinessComponent struct {
	Ready    bool `json:"ready"`
	Required bool `json:"required"`
}

// Response represents a standard API response.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// HealthCheck handles GET /health
func HealthCheck(c *gin.Context) {
	respondOK(c, gin.H{
		"status":  "healthy",
		"version": backendVersion,
	})
}

// ReadinessCheck handles GET /ready.
func ReadinessCheck(c *gin.Context) {
	components := gin.H{
		"planner":   readinessComponent{Ready: planner.HasPlanner(), Required: true},
		"project":   readinessComponent{Ready: project.HasProjectService(), Required: true},
		"context":   readinessComponent{Ready: ctxsvc.HasContextService(), Required: true},
		"audit":     readinessComponent{Ready: audit.HasAuditService(), Required: false},
		"hooks":     readinessComponent{Ready: hooks.HasHookManager(), Required: false},
		"privacy":   readinessComponent{Ready: privacy.HasPrivacyService(), Required: false},
		"isolation": readinessComponent{Ready: isolation.HasIsolationService(), Required: false},
		// Experimental modules: Has* does not lazy-construct, so this shows bootstrap wiring.
		"floweng":   readinessComponent{Ready: floweng.HasEngine(), Required: false},
		"workspace": readinessComponent{Ready: workspace.HasService(), Required: false},
		"guard":     readinessComponent{Ready: guard.HasService(), Required: false},
		"skill":     readinessComponent{Ready: skill.HasRegistry(), Required: false},
	}

	ready := true
	for _, name := range []string{"planner", "project", "context"} {
		component := components[name].(readinessComponent)
		if component.Required && !component.Ready {
			ready = false
			break
		}
	}

	status := http.StatusOK
	state := "ready"
	if !ready {
		status = http.StatusServiceUnavailable
		state = "not_ready"
	}

	c.JSON(status, Response{
		Success: ready,
		Data: gin.H{
			"status":     state,
			"version":    backendVersion,
			"components": components,
		},
	})
}

// Metrics handles GET /metrics.
func Metrics(c *gin.Context) {
	trace := middleware.GetTrace(c)
	respondOK(c, gin.H{
		"trace":   trace,
		"metrics": middleware.GetMetricsSnapshot(),
	})
}

// respondOK sends a successful response.
func respondOK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Data:    data,
	})
}

// respondCreated sends a 201 Created response.
func respondCreated(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, Response{
		Success: true,
		Data:    data,
	})
}

// respondError sends an error response.
func respondError(c *gin.Context, status int, message string) {
	c.JSON(status, Response{
		Success: false,
		Error:   message,
	})
}

// respondNotImplemented sends a 501 Not Implemented response.
func respondNotImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, Response{
		Success: false,
		Error:   "Not implemented yet",
	})
}

func respondInternalError(c *gin.Context, context string, err error) {
	trace := middleware.GetTrace(c)
	requestID := ""
	if trace != nil {
		requestID = trace.RequestID
	}
	log.Printf("[ERROR] [%s] %s: %v", requestID, context, err)
	c.JSON(http.StatusInternalServerError, Response{
		Success: false,
		Error:   "Internal server error",
	})
}

func requireUUIDParam(c *gin.Context, name, label string) (string, bool) {
	value := strings.TrimSpace(c.Param(name))
	if value == "" {
		respondError(c, http.StatusBadRequest, fmt.Sprintf("Missing %s", label))
		return "", false
	}
	if _, err := uuid.Parse(value); err != nil {
		respondError(c, http.StatusBadRequest, fmt.Sprintf("Invalid %s", label))
		return "", false
	}
	return value, true
}

func parsePositiveQueryInt(c *gin.Context, name string, defaultValue, maxValue int) (int, bool) {
	value := defaultValue
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return value, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 {
		respondError(c, http.StatusBadRequest, fmt.Sprintf("%s must be a positive integer", name))
		return 0, false
	}
	if maxValue > 0 && parsed > maxValue {
		parsed = maxValue
	}
	return parsed, true
}

func parseNonNegativeQueryInt(c *gin.Context, name string, defaultValue int) (int, bool) {
	value := defaultValue
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return value, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		respondError(c, http.StatusBadRequest, fmt.Sprintf("%s must be a non-negative integer", name))
		return 0, false
	}
	return parsed, true
}
