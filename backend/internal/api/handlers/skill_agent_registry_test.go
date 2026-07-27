package handlers

// HTTP handler-level coverage for skill version history and agent registry
// endpoints. Uses rex* envelope helpers (routes_extra_common_test.go).

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/skill"
)

// --- skill version helpers ---

func skillVersionRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	prev := skill.GetRegistry()
	skill.SetRegistry(skill.NewInMemoryRegistry())
	t.Cleanup(func() { skill.SetRegistry(prev) })

	r := gin.New()
	r.GET("/api/v1/skills/:id/versions", ListSkillVersions)
	r.POST("/api/v1/skills/:id/rollback", RollbackSkillVersion)
	r.POST("/api/v1/skills", CreateSkill)
	r.PATCH("/api/v1/skills/:id", UpdateSkill)
	return r
}

func TestSkillVersionListEmpty(t *testing.T) {
	r := skillVersionRouter(t)

	ctx := t
	_ = ctx
	s := createTestSkill(t, r)

	w := rexRequest(t, r, http.MethodGet, "/api/v1/skills/"+s.ID+"/versions", nil, nil)
	var list struct {
		Items []interface{} `json:"items"`
		Total int           `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total != 0 {
		t.Fatalf("expected 0 versions before any update, got %d", list.Total)
	}
}

func TestSkillVersionAfterUpdate(t *testing.T) {
	r := skillVersionRouter(t)

	s := createTestSkill(t, r)
	newName := "updated-skill"
	body := rexMustJSON(t, map[string]interface{}{"name": newName})
	w := rexRequest(t, r, http.MethodPatch, "/api/v1/skills/"+s.ID, body, nil)
	rexData(t, w, http.StatusOK, true, nil)

	w = rexRequest(t, r, http.MethodGet, "/api/v1/skills/"+s.ID+"/versions", nil, nil)
	var list struct {
		Items []struct {
			RowID   int64  `json:"row_id"`
			SkillID string `json:"skill_id"`
			Version string `json:"version"`
		} `json:"items"`
		Total int `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total != 1 {
		t.Fatalf("expected 1 version after update, got %d", list.Total)
	}
	if list.Items[0].SkillID != s.ID {
		t.Fatalf("version skill_id=%q want=%q", list.Items[0].SkillID, s.ID)
	}
}

func TestSkillVersionRollback(t *testing.T) {
	r := skillVersionRouter(t)

	s := createTestSkill(t, r)
	origName := s.Name

	newName := "v2-name"
	body := rexMustJSON(t, map[string]interface{}{"name": newName})
	w := rexRequest(t, r, http.MethodPatch, "/api/v1/skills/"+s.ID, body, nil)
	rexData(t, w, http.StatusOK, true, nil)

	w = rexRequest(t, r, http.MethodGet, "/api/v1/skills/"+s.ID+"/versions", nil, nil)
	var list struct {
		Items []struct {
			RowID int64 `json:"row_id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total < 1 {
		t.Fatalf("expected >=1 version, got %d", list.Total)
	}
	rowID := list.Items[0].RowID

	rbBody := rexMustJSON(t, map[string]interface{}{"version_row_id": rowID})
	w = rexRequest(t, r, http.MethodPost, "/api/v1/skills/"+s.ID+"/rollback", rbBody, nil)
	var rolled struct {
		Name string `json:"name"`
	}
	rexData(t, w, http.StatusOK, true, &rolled)
	if rolled.Name != origName {
		t.Fatalf("after rollback name=%q want=%q", rolled.Name, origName)
	}
}

func TestSkillVersionRollbackNotFound(t *testing.T) {
	r := skillVersionRouter(t)

	s := createTestSkill(t, r)
	body := rexMustJSON(t, map[string]interface{}{"version_row_id": 99999})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/skills/"+s.ID+"/rollback", body, nil)
	rexData(t, w, http.StatusNotFound, false, nil)
}

type testSkill struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func createTestSkill(t *testing.T, r *gin.Engine) testSkill {
	t.Helper()
	body := rexMustJSON(t, map[string]interface{}{
		"name": "test-skill",
		"body": "test body content",
	})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/skills", body, nil)
	var s testSkill
	rexData(t, w, http.StatusCreated, true, &s)
	if s.ID == "" {
		t.Fatalf("create skill returned empty id, body=%s", w.Body.String())
	}
	return s
}

// --- agent registry helpers ---

func agentRegistryRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hadReg := agent.HasAgentRegistry()
	var prev agent.AgentRegistry
	if hadReg {
		prev = agent.GetAgentRegistry()
	}
	agent.SetAgentRegistry(agent.NewInMemoryAgentRegistry())
	t.Cleanup(func() {
		if hadReg {
			agent.SetAgentRegistry(prev)
		} else {
			agent.SetAgentRegistry(nil)
		}
	})

	r := gin.New()
	r.GET("/api/v1/agents/registry", ListRegistryAgents)
	r.POST("/api/v1/agents/registry", CreateRegistryAgent)
	r.GET("/api/v1/agents/registry/:id", GetRegistryAgent)
	r.PATCH("/api/v1/agents/registry/:id", UpdateRegistryAgent)
	r.DELETE("/api/v1/agents/registry/:id", DeleteRegistryAgent)
	r.POST("/api/v1/agents/registry/:id/usage", IncrementRegistryAgentUsage)
	r.POST("/api/v1/agents/registry/:id/score", SetRegistryAgentScore)
	return r
}

func TestAgentRegistryCRUD(t *testing.T) {
	r := agentRegistryRouter(t)

	body := rexMustJSON(t, map[string]interface{}{
		"name":      "test-agent",
		"role_base": "coder",
	})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/agents/registry", body, nil)
	var created struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		RoleBase string `json:"role_base"`
	}
	rexData(t, w, http.StatusCreated, true, &created)
	if created.ID == "" || created.Name != "test-agent" {
		t.Fatalf("unexpected create result: %s", w.Body.String())
	}

	w = rexRequest(t, r, http.MethodGet, "/api/v1/agents/registry/"+created.ID, nil, nil)
	rexData(t, w, http.StatusOK, true, nil)

	upBody := rexMustJSON(t, map[string]interface{}{"name": "renamed"})
	w = rexRequest(t, r, http.MethodPatch, "/api/v1/agents/registry/"+created.ID, upBody, nil)
	var updated struct {
		Name string `json:"name"`
	}
	rexData(t, w, http.StatusOK, true, &updated)
	if updated.Name != "renamed" {
		t.Fatalf("update name=%q want=renamed", updated.Name)
	}

	w = rexRequest(t, r, http.MethodDelete, "/api/v1/agents/registry/"+created.ID, nil, nil)
	rexData(t, w, http.StatusOK, true, nil)

	w = rexRequest(t, r, http.MethodGet, "/api/v1/agents/registry/"+created.ID, nil, nil)
	rexData(t, w, http.StatusNotFound, false, nil)
}

func TestAgentRegistryListFiltered(t *testing.T) {
	r := agentRegistryRouter(t)

	body := rexMustJSON(t, map[string]interface{}{
		"name":      "coder-agent",
		"role_base": "coder",
	})
	rexRequest(t, r, http.MethodPost, "/api/v1/agents/registry", body, nil)

	w := rexRequest(t, r, http.MethodGet, "/api/v1/agents/registry?role=coder", nil, nil)
	var list struct {
		Items []interface{} `json:"items"`
		Total int           `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total < 1 {
		t.Fatalf("expected >=1 coder agent, got %d", list.Total)
	}
}

func TestAgentRegistryBuiltinProtected(t *testing.T) {
	r := agentRegistryRouter(t)

	w := rexRequest(t, r, http.MethodGet, "/api/v1/agents/registry?source=builtin", nil, nil)
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total == 0 {
		t.Skip("no builtins seeded in test registry")
	}
	builtinID := list.Items[0].ID

	w = rexRequest(t, r, http.MethodDelete, "/api/v1/agents/registry/"+builtinID, nil, nil)
	rexData(t, w, http.StatusConflict, false, nil)
}

func TestAgentRegistryNotFound(t *testing.T) {
	r := agentRegistryRouter(t)

	w := rexRequest(t, r, http.MethodGet, "/api/v1/agents/registry/nonexistent-id", nil, nil)
	rexData(t, w, http.StatusNotFound, false, nil)
}

func TestAgentRegistryUsageAndScore(t *testing.T) {
	r := agentRegistryRouter(t)

	body := rexMustJSON(t, map[string]interface{}{
		"name":      "usage-agent",
		"role_base": "sub",
	})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/agents/registry", body, nil)
	var created struct {
		ID string `json:"id"`
	}
	rexData(t, w, http.StatusCreated, true, &created)

	w = rexRequest(t, r, http.MethodPost, "/api/v1/agents/registry/"+created.ID+"/usage", nil, nil)
	rexData(t, w, http.StatusOK, true, nil)

	scoreBody := rexMustJSON(t, map[string]interface{}{"score": 0.95})
	w = rexRequest(t, r, http.MethodPost, "/api/v1/agents/registry/"+created.ID+"/score", scoreBody, nil)
	var scoreResp struct {
		Score float64 `json:"score"`
	}
	rexData(t, w, http.StatusOK, true, &scoreResp)
	if scoreResp.Score != 0.95 {
		t.Fatalf("score=%v want=0.95", scoreResp.Score)
	}

	w = rexRequest(t, r, http.MethodPost, "/api/v1/agents/registry/nonexistent/usage", nil, nil)
	rexData(t, w, http.StatusNotFound, false, nil)

	w = rexRequest(t, r, http.MethodPost, "/api/v1/agents/registry/nonexistent/score",
		rexMustJSON(t, map[string]interface{}{"score": 1.0}), nil)
	rexData(t, w, http.StatusNotFound, false, nil)
}
