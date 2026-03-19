// Package middleware provides HTTP middleware for the API server.
package middleware

import (
	"sync"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	HeaderRequestID = "X-Request-ID"
	HeaderSessionID = "X-Session-ID"
	HeaderTaskID    = "X-Task-ID"
	HeaderAgentID   = "X-Agent-ID"
)

type traceContextKey string

const (
	traceKey   traceContextKey = "api_trace"
	metricsKey traceContextKey = "api_metrics"
)

// RequestTrace describes trace metadata attached to one HTTP request.
type RequestTrace struct {
	RequestID  string  `json:"request_id,omitempty"`
	SessionID  string  `json:"session_id,omitempty"`
	TaskID     string  `json:"task_id,omitempty"`
	AgentID    string  `json:"agent_id,omitempty"`
	Method     string  `json:"method,omitempty"`
	Path       string  `json:"path,omitempty"`
	Route      string  `json:"route,omitempty"`
	StatusCode int     `json:"status_code,omitempty"`
	LatencyMs  float64 `json:"latency_ms,omitempty"`
}

// MetricsSnapshot describes coarse HTTP middleware metrics.
type MetricsSnapshot struct {
	RequestCount     int64   `json:"request_count"`
	ErrorCount       int64   `json:"error_count"`
	LastStatusCode   int     `json:"last_status_code,omitempty"`
	LastLatencyMs    float64 `json:"last_latency_ms,omitempty"`
	LastRequestAt    int64   `json:"last_request_at,omitempty"`
	LastRequestPath  string  `json:"last_request_path,omitempty"`
	LastRequestRoute string  `json:"last_request_route,omitempty"`
}

var (
	metricsMu       sync.RWMutex
	metricsSnapshot MetricsSnapshot
)

// Trace attaches request/audit trace metadata to gin context and response headers.
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		trace := &RequestTrace{
			RequestID: resolveHeader(c.GetHeader(HeaderRequestID)),
			SessionID: resolveHeader(c.GetHeader(HeaderSessionID)),
			TaskID:    resolveHeader(c.GetHeader(HeaderTaskID)),
			AgentID:   resolveHeader(c.GetHeader(HeaderAgentID)),
			Method:    c.Request.Method,
			Path:      c.Request.URL.Path,
		}
		if trace.RequestID == "" {
			trace.RequestID = uuid.NewString()
		}

		c.Header(HeaderRequestID, trace.RequestID)
		if trace.SessionID != "" {
			c.Header(HeaderSessionID, trace.SessionID)
		}
		if trace.TaskID != "" {
			c.Header(HeaderTaskID, trace.TaskID)
		}
		if trace.AgentID != "" {
			c.Header(HeaderAgentID, trace.AgentID)
		}

		c.Set(string(traceKey), trace)
		ctx := audit.ContextWithTrace(c.Request.Context(), &audit.AuditTrace{
			RequestID: trace.RequestID,
			SessionID: trace.SessionID,
			TaskID:    trace.TaskID,
			AgentID:   trace.AgentID,
			Method:    trace.Method,
			Path:      trace.Path,
		})
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		trace.StatusCode = c.Writer.Status()
		trace.Route = c.FullPath()
		trace.LatencyMs = float64(time.Since(start).Microseconds()) / 1000.0
		if trace.Route == "" {
			trace.Route = trace.Path
		}

		c.Set(string(traceKey), trace)
		c.Request = c.Request.WithContext(audit.ContextWithTrace(c.Request.Context(), &audit.AuditTrace{
			RequestID:  trace.RequestID,
			SessionID:  trace.SessionID,
			TaskID:     trace.TaskID,
			AgentID:    trace.AgentID,
			Method:     trace.Method,
			Path:       trace.Path,
			Route:      trace.Route,
			StatusCode: trace.StatusCode,
			LatencyMs:  trace.LatencyMs,
		}))

		updateMetrics(trace, len(c.Errors) > 0)
	}
}

// GetTrace returns the current request trace snapshot.
func GetTrace(c *gin.Context) *RequestTrace {
	if c == nil {
		return nil
	}

	if value, ok := c.Get(string(traceKey)); ok {
		if trace, ok := value.(*RequestTrace); ok && trace != nil {
			copy := *trace
			return &copy
		}
	}
	if trace := audit.TraceFromContext(c.Request.Context()); trace != nil {
		return &RequestTrace{
			RequestID:  trace.RequestID,
			SessionID:  trace.SessionID,
			TaskID:     trace.TaskID,
			AgentID:    trace.AgentID,
			Method:     trace.Method,
			Path:       trace.Path,
			Route:      trace.Route,
			StatusCode: trace.StatusCode,
			LatencyMs:  trace.LatencyMs,
		}
	}
	return nil
}

// GetMetricsSnapshot returns a copy of coarse middleware metrics.
func GetMetricsSnapshot() MetricsSnapshot {
	metricsMu.RLock()
	defer metricsMu.RUnlock()
	return metricsSnapshot
}

func resolveHeader(value string) string {
	if value != "" {
		return value
	}
	return ""
}

func updateMetrics(trace *RequestTrace, hasErrors bool) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	metricsSnapshot.RequestCount++
	if hasErrors || (trace != nil && trace.StatusCode >= 500) {
		metricsSnapshot.ErrorCount++
	}
	if trace == nil {
		return
	}
	metricsSnapshot.LastStatusCode = trace.StatusCode
	metricsSnapshot.LastLatencyMs = trace.LatencyMs
	metricsSnapshot.LastRequestAt = time.Now().UnixMilli()
	metricsSnapshot.LastRequestPath = trace.Path
	metricsSnapshot.LastRequestRoute = trace.Route
}
