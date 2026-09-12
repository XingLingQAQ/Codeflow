package middleware

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/codeflow/backend/internal/audit"
	"github.com/gin-gonic/gin"
)

// AuditMutations establishes the baseline audit record for every authenticated
// state-changing API call, including handler failures. Domain services may add
// richer records, but no mutation route is completely invisible.
func AuditMutations() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		c.Next()

		status := c.Writer.Status()
		outcome := audit.OutcomeSuccess
		severity := audit.SeverityInfo
		if status >= http.StatusBadRequest {
			outcome = audit.OutcomeFailure
			severity = audit.SeverityWarning
		}
		eventType := audit.EventModify
		switch c.Request.Method {
		case http.MethodPost:
			eventType = audit.EventCreate
		case http.MethodDelete:
			eventType = audit.EventDelete
		}
		route := c.FullPath()
		if route == "" { route = c.Request.URL.Path }
		resourceType, resourceID := mutationResource(c)
		trace := audit.TraceFromContext(c.Request.Context())
		if trace == nil { trace = &audit.AuditTrace{} } else { copied := *trace; trace = &copied }
		trace.Route = route
		trace.StatusCode = status
		if !audit.HasAuditService() { return }
		if _, err := audit.Record(c.Request.Context(), &audit.AuditLogEntry{
			EventType: eventType,
			Severity: severity,
			Actor: audit.AuditActor{ID:"sidecar-user", Type:"user"},
			Resource: audit.AuditResource{Type:resourceType, ID:resourceID},
			Action: fmt.Sprintf("api.%s %s", strings.ToLower(c.Request.Method), route),
			Outcome: outcome,
			Trace: trace,
			Details: map[string]interface{}{"status_code":status, "route":route},
		}); err != nil {
			log.Printf("[audit] mutation record failed: method=%s route=%s err=%v", c.Request.Method, route, err)
		}
	}
}

func mutationResource(c *gin.Context) (string, string) {
	path := strings.TrimPrefix(c.Request.URL.Path, "/api/v1/")
	parts := strings.Split(path, "/")
	resourceType := "api"
	if len(parts) > 0 && parts[0] != "" { resourceType = parts[0] }
	for _, name := range []string{"id","projectId","sessionId","tid","sid","aid","gid"} {
		if value := strings.TrimSpace(c.Param(name)); value != "" { return resourceType, value }
	}
	if trace := audit.TraceFromContext(c.Request.Context()); trace != nil {
		if trace.ProjectID != "" { return resourceType, trace.ProjectID }
		if trace.SessionID != "" { return resourceType, trace.SessionID }
	}
	return resourceType, resourceType
}
