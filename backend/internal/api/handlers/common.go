// Package handlers provides HTTP request handlers for the API.
package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/codeflow/backend/internal/audit"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
	"github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/isolation"
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/privacy"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/readiness"
	"github.com/codeflow/backend/internal/samg"
	"github.com/codeflow/backend/internal/skill"
	"github.com/codeflow/backend/internal/workspace"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const backendVersion = "0.1.0"

type readinessComponent struct {
	Ready    bool `json:"ready"`
	Required bool `json:"required"`
	// Probe-backed components additionally report the dependency state, the
	// probe's read-only declaration, the machine-readable code of the most
	// recent failure, and the last probe wall time. All probe fields are
	// omitted from legacy Has* entries, so the previous response shape is
	// unchanged when no probes are registered (I-54, T0.12.a).
	Status    string `json:"status,omitempty"`
	Readonly  bool   `json:"readonly,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	LatencyMS *int64 `json:"latency_ms,omitempty"`
	Detail    string `json:"detail,omitempty"`
	// CheckedAt is the RFC3339 UTC wall time of the probe run (or of the
	// moment the run was judged timed out) and is only set on
	// probe-backed components.
	CheckedAt string `json:"checked_at,omitempty"`
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
		"audit":     readinessComponent{Ready: audit.HasAuditService(), Required: true},
		"agent":     readinessComponent{Ready: agent.HasAgentService(), Required: true},
		"memory":    readinessComponent{Ready: memory.HasMemoryService(), Required: true},
		"samg":      readinessComponent{Ready: samg.HasSAMGService(), Required: true},
		"hooks":     readinessComponent{Ready: hooks.HasHookManager(), Required: false},
		"privacy":   readinessComponent{Ready: privacy.HasPrivacyService(), Required: false},
		"isolation": readinessComponent{Ready: isolation.HasIsolationService(), Required: false},
		// Experimental modules: Has* does not lazy-construct, so this shows bootstrap wiring.
		"floweng":   readinessComponent{Ready: floweng.HasEngine(), Required: false},
		"workspace": readinessComponent{Ready: workspace.HasService(), Required: false},
		"guard":     readinessComponent{Ready: guard.HasService(), Required: false},
		"skill":     readinessComponent{Ready: skill.HasRegistry(), Required: false},
	}

	// Probe-backed dependency checks (I-54, T0.12): run registered probes
	// concurrently under per-probe deadlines and merge them into components.
	// A probe entry overrides a same-named legacy Has* entry with the richer
	// payload while keeping the ready/required fields consumers already read.
	probed := readiness.Run(c.Request.Context())
	for name, probe := range probed {
		latencyMS := probe.Latency.Milliseconds()
		component := readinessComponent{
			Ready:     probe.Result.State == readiness.StateReady,
			Required:  probe.Required,
			Status:    string(probe.Result.State),
			Readonly:  probe.Readonly,
			ErrorCode: probe.Result.ErrCode,
			LatencyMS: &latencyMS,
			Detail:    probe.Result.Detail,
		}
		if !probe.CheckedAt.IsZero() {
			component.CheckedAt = probe.CheckedAt.UTC().Format(time.RFC3339)
		}
		components[name] = component
	}

	ready := true
	for _, name := range []string{"planner", "project", "context", "audit", "agent", "memory", "samg"} {
		component := components[name].(readinessComponent)
		if component.Required && !component.Ready {
			ready = false
			break
		}
	}
	// Required probes gate the overall verdict the same way required Has*
	// services do: a required probe not in state ready makes /ready 503.
	if ready {
		for _, probe := range probed {
			if probe.Required && probe.Result.State != readiness.StateReady {
				ready = false
				break
			}
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
