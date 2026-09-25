package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
	"github.com/gin-gonic/gin"
)

// installSpoofTestAudit 安装内存审计存储为全局服务并登记清理。
func installSpoofTestAudit(t *testing.T) (*audit.AuditService, *audit.MemoryStorage) {
	t.Helper()
	storage := audit.NewMemoryStorage()
	svc := audit.NewAuditService(storage)
	audit.SetAuditService(svc)
	t.Cleanup(func() { audit.SetAuditService(nil) })
	return svc, storage
}

func spoofTestEntries(t *testing.T, storage *audit.MemoryStorage) []audit.AuditLogEntry {
	t.Helper()
	entries, err := storage.Query(context.Background(), &audit.AuditQuery{Limit: 100})
	if err != nil {
		t.Fatalf("query audit entries: %v", err)
	}
	return entries
}

// TestAuditRejectsSpoofedActor 是 T0.10.c 的 HTTP 级负向证据：客户端经请求头
// 自报的 agent 身份（X-Agent-ID）没有任何服务端凭据，一律不得被采信为审计
// actor——有凭据时按注入身份（user）记录，无凭据时直接 401 拒绝且零落盘；
// 未挂认证的链上降级为 system(auto-default) 留痕，同样绝不采信自报身份。
func TestAuditRejectsSpoofedActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const spoofedAgent = "spoofed-agent-1"

	newAuthedRouter := func(hitPolicy bool) *gin.Engine {
		r := gin.New()
		r.Use(Trace(), RequireAccessToken(authTestToken), AuditMutations())
		r.POST("/api/v1/projects/:id", func(c *gin.Context) {
			if hitPolicy {
				// 与生产 handler 同型：执行边界评估直接消费请求上下文。
				d := policy.EvaluateBoundary(c.Request.Context(), policy.Request{
					Operation: policy.OperationWorkspaceWrite,
					Resource:  "proj/file.txt",
					ProjectID: "p1",
				})
				c.JSON(http.StatusCreated, gin.H{"allowed": d.Allowed, "actor_type": d.ActorType, "audit_id": d.AuditID})
				return
			}
			c.Status(http.StatusCreated)
		})
		return r
	}

	t.Run("valid token: agent claim not adopted, mutation entry uses injected user", func(t *testing.T) {
		_, storage := installSpoofTestAudit(t)
		r := newAuthedRouter(false)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1", nil)
		req.Header.Set("Authorization", "Bearer "+authTestToken)
		req.Header.Set(HeaderAgentID, spoofedAgent)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("authed request must proceed: got %d", w.Code)
		}

		entries := spoofTestEntries(t, storage)
		if len(entries) != 1 {
			t.Fatalf("expected exactly 1 mutation entry, got %d", len(entries))
		}
		entry := entries[0]
		if entry.Actor.Type != string(audit.ActorTypeUser) || entry.Actor.ID != SidecarUserActorID || entry.Actor.Source != ActorSourceSidecarToken {
			t.Fatalf("actor must come from server-side injection, got %+v", entry.Actor)
		}
		if entry.Actor.Type == string(audit.ActorTypeAgent) || entry.Actor.ID == spoofedAgent {
			t.Fatalf("spoofed agent identity was adopted: %+v", entry.Actor)
		}
		if _, marked := entry.Details["actor_resolution"]; marked {
			t.Fatalf("valid injected identity must not leave fallback marker: %+v", entry.Details)
		}
		// 自报值只作为关联证据保留在 trace 上，不作为身份。
		if entry.Trace == nil || entry.Trace.AgentID != spoofedAgent {
			t.Fatalf("client claim should be retained as trace evidence, not identity: %+v", entry.Trace)
		}
	})

	t.Run("valid token: injected user reaches policy decision despite agent claim", func(t *testing.T) {
		_, storage := installSpoofTestAudit(t)
		policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
		r := newAuthedRouter(true)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1", nil)
		req.Header.Set("Authorization", "Bearer "+authTestToken)
		req.Header.Set(HeaderAgentID, spoofedAgent)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("authed request must proceed: got %d", w.Code)
		}
		var body struct {
			Allowed   bool   `json:"allowed"`
			ActorType string `json:"actor_type"`
			AuditID   string `json:"audit_id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !body.Allowed || body.ActorType != string(audit.ActorTypeUser) || body.AuditID == "" {
			t.Fatalf("decision must be allowed and attributed to injected user: %+v", body)
		}

		entries := spoofTestEntries(t, storage)
		var decision *audit.AuditLogEntry
		for i := range entries {
			if entries[i].ID == body.AuditID {
				decision = &entries[i]
			}
		}
		if decision == nil {
			t.Fatalf("decision audit entry %s not found among %d entries", body.AuditID, len(entries))
		}
		if decision.Actor.Type != string(audit.ActorTypeUser) || decision.Actor.ID != SidecarUserActorID || decision.Actor.Source != ActorSourceSidecarToken {
			t.Fatalf("policy decision actor must be the injected user, got %+v", decision.Actor)
		}
		// 客户端声称的 agent 作为证据留痕（details.agent_id），但不升级为 actor 身份。
		if decision.Details["agent_id"] != spoofedAgent {
			t.Fatalf("agent claim must be kept as evidence only: %+v", decision.Details)
		}
		if _, marked := decision.Details["actor_resolution"]; marked {
			t.Fatalf("no fallback marker expected for valid injected identity: %+v", decision.Details)
		}
		raw, err := json.Marshal(decision)
		if err != nil {
			t.Fatalf("marshal decision entry: %v", err)
		}
		if strings.Contains(string(raw), `"type":"agent"`) {
			t.Fatalf("no stored field may attribute the request to an agent: %s", raw)
		}
	})

	t.Run("no credential: spoofed request rejected with 401 and zero audit growth", func(t *testing.T) {
		_, storage := installSpoofTestAudit(t)
		r := newAuthedRouter(false)
		before, err := storage.Count(context.Background(), nil)
		if err != nil {
			t.Fatalf("count: %v", err)
		}

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1", nil)
		req.Header.Set(HeaderAgentID, spoofedAgent)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("credential-less request must be rejected: got %d", w.Code)
		}
		after, err := storage.Count(context.Background(), nil)
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		if after != before {
			t.Fatalf("rejected request must not grow the audit log: before=%d after=%d", before, after)
		}
	})

	t.Run("no auth middleware: degrades to system with trace, never adopts the claim", func(t *testing.T) {
		_, storage := installSpoofTestAudit(t)
		r := gin.New()
		r.Use(Trace(), AuditMutations())
		r.POST("/api/v1/projects/:id", func(c *gin.Context) { c.Status(http.StatusCreated) })

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1", nil)
		req.Header.Set(HeaderAgentID, spoofedAgent)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("request must proceed: got %d", w.Code)
		}

		entries := spoofTestEntries(t, storage)
		if len(entries) != 1 {
			t.Fatalf("expected exactly 1 entry, got %d", len(entries))
		}
		entry := entries[0]
		if entry.Actor.Type != string(audit.ActorTypeSystem) || entry.Actor.ID != audit.DefaultSystemActorID || entry.Actor.Source != audit.ActorSourceAutoDefault {
			t.Fatalf("unauthenticated chain must degrade to system auto-default, got %+v", entry.Actor)
		}
		if entry.Actor.ID == spoofedAgent || entry.Actor.Type == string(audit.ActorTypeAgent) {
			t.Fatalf("spoofed identity adopted on unauthenticated chain: %+v", entry.Actor)
		}
	})
}
