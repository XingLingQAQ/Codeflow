package handlers

// HTTP handler-level coverage for skill registry routes: POST /match honours the
// stage_type filter (a submit-stage prompt matches the commit-hygiene builtin but
// not the coding/review-only builtin), and GET /skills?enabled=true lists only
// enabled skills. A fresh in-memory registry (two seeded builtins) is swapped in.

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/skill"
)

func rexSkillRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	prev := skill.GetRegistry()
	skill.SetRegistry(skill.NewInMemoryRegistry())
	t.Cleanup(func() { skill.SetRegistry(prev) })

	r := gin.New()
	r.POST("/api/v1/skills/match", MatchSkills)
	r.GET("/api/v1/skills", ListSkills)
	return r
}

func TestMatchSkillsStageFilterRoutesExtra(t *testing.T) {
	r := rexSkillRouter(t)

	// "commit" trigger + stage submit → only builtin-commit-hygiene (stage tags
	// submit,coding). builtin-test-first (coding,review) is excluded by stage.
	body := rexMustJSON(t, map[string]interface{}{
		"text":       "time to commit changes",
		"stage_type": "submit",
	})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/skills/match", body, nil)
	var out struct {
		Items []struct {
			Skill struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"skill"`
			Score float64  `json:"score"`
			Hits  []string `json:"hits"`
		} `json:"items"`
		Total int `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &out)
	if out.Total != 1 || len(out.Items) != 1 {
		t.Fatalf("match total=%d want=1 body=%s", out.Total, w.Body.String())
	}
	if out.Items[0].Skill.ID != "builtin-commit-hygiene" {
		t.Fatalf("matched skill=%q want=builtin-commit-hygiene", out.Items[0].Skill.ID)
	}
	if out.Items[0].Score <= 0 {
		t.Fatalf("expected positive score, got %v", out.Items[0].Score)
	}
}

func TestListSkillsEnabledRoutesExtra(t *testing.T) {
	r := rexSkillRouter(t)

	// enabled=true → both seeded builtins (both enabled).
	w := rexRequest(t, r, http.MethodGet, "/api/v1/skills?enabled=true", nil, nil)
	var out struct {
		Items []skill.Skill `json:"items"`
		Total int           `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &out)
	if out.Total != 2 {
		t.Fatalf("enabled skills total=%d want=2 body=%s", out.Total, w.Body.String())
	}
	for _, s := range out.Items {
		if !s.Enabled {
			t.Fatalf("disabled skill leaked into enabled=true list: %s", s.ID)
		}
	}

	// stage filter narrows to the review-tagged builtin only.
	w = rexRequest(t, r, http.MethodGet, "/api/v1/skills?enabled=true&stage=review", nil, nil)
	out.Items, out.Total = nil, 0
	rexData(t, w, http.StatusOK, true, &out)
	if out.Total != 1 || len(out.Items) != 1 {
		t.Fatalf("stage=review total=%d want=1 body=%s", out.Total, w.Body.String())
	}
	if out.Items[0].ID != "builtin-test-first" {
		t.Fatalf("stage=review skill=%q want=builtin-test-first", out.Items[0].ID)
	}
}
