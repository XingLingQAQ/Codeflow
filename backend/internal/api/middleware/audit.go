package middleware

import (
	"context"
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
		actor, actorResolution := resolveMutationActor(c.Request.Context())
		details := map[string]interface{}{"status_code":status, "route":route}
		if actorResolution != "" { details["actor_resolution"] = actorResolution }
		if _, err := audit.Record(c.Request.Context(), &audit.AuditLogEntry{
			EventType: eventType,
			Severity: severity,
			Actor: actor.AuditActor(),
			Resource: audit.AuditResource{Type:resourceType, ID:resourceID},
			Action: fmt.Sprintf("api.%s %s", strings.ToLower(c.Request.Method), route),
			Outcome: outcome,
			Trace: trace,
			Details: details,
		}); err != nil {
			log.Printf("[audit] mutation record failed: method=%s route=%s err=%v", c.Request.Method, route, err)
		}
	}
}

// resolveMutationActor 解析基线条目的操作者身份（T0.10.c）：
//  1. 认证中间件注入的合法身份：如实采用（sidecar token → user/sidecar-user）；
//  2. 注入但形状非法：绝不采用，降级 DefaultSystemActor 并留痕
//     actor_resolution=invalid_context_actor（与 policy.resolveDecisionActor 同口径）；
//  3. 未注入（未挂认证的路由或测试直连）：降级 DefaultSystemActor(auto-default)，
//     不伪造用户身份。
func resolveMutationActor(ctx context.Context) (audit.Actor, string) {
	actor, err := audit.ResolveActor(ctx, audit.MissingActorDefaultSystem)
	if err != nil {
		return audit.DefaultSystemActor(), "invalid_context_actor"
	}
	return actor, ""
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
