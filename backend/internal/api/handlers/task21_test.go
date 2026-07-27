package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
)

// --- A: guard exemption-requests ---

func exemptionRequestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	prev := guard.GetService()
	guard.SetService(guard.NewEngine(nil, nil))
	t.Cleanup(func() { guard.SetService(prev) })

	r := gin.New()
	r.POST("/api/v1/guard/exemption-requests", CreateExemptionRequest)
	r.GET("/api/v1/guard/exemption-requests", ListExemptionRequests)
	r.POST("/api/v1/guard/exemption-requests/:id/decide", DecideExemptionRequest)
	return r
}

func TestExemptionRequestCreateAndList(t *testing.T) {
	r := exemptionRequestRouter(t)

	body := rexMustJSON(t, map[string]interface{}{
		"path":      "src/hack.go",
		"reason":    "temporary migration fix",
		"requester": "agent-1",
	})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/guard/exemption-requests", body, nil)
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	rexData(t, w, http.StatusCreated, true, &created)
	if created.ID == "" {
		t.Fatalf("expected non-empty id")
	}
	if created.Status != "pending" {
		t.Fatalf("status=%q want=pending", created.Status)
	}

	w = rexRequest(t, r, http.MethodGet, "/api/v1/guard/exemption-requests?status=pending", nil, nil)
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total < 1 {
		t.Fatalf("expected >=1 pending request, got %d", list.Total)
	}
}

func TestExemptionRequestDecide(t *testing.T) {
	r := exemptionRequestRouter(t)

	body := rexMustJSON(t, map[string]interface{}{
		"path": "src/tmp.go", "reason": "reason", "requester": "agent-2",
	})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/guard/exemption-requests", body, nil)
	var created struct {
		ID string `json:"id"`
	}
	rexData(t, w, http.StatusCreated, true, &created)

	decideBody := rexMustJSON(t, map[string]interface{}{
		"approve": true, "decided_by": "admin", "reason": "approved",
	})
	w = rexRequest(t, r, http.MethodPost,
		"/api/v1/guard/exemption-requests/"+created.ID+"/decide", decideBody, nil)
	var decided struct {
		Status string `json:"status"`
	}
	rexData(t, w, http.StatusOK, true, &decided)
	if decided.Status != "approved" {
		t.Fatalf("status=%q want=approved", decided.Status)
	}

	w = rexRequest(t, r, http.MethodPost,
		"/api/v1/guard/exemption-requests/"+created.ID+"/decide", decideBody, nil)
	rexData(t, w, http.StatusConflict, false, nil)
}

func TestExemptionRequestDecideNotFound(t *testing.T) {
	r := exemptionRequestRouter(t)

	body := rexMustJSON(t, map[string]interface{}{
		"approve": false, "decided_by": "admin",
	})
	w := rexRequest(t, r, http.MethodPost,
		"/api/v1/guard/exemption-requests/nonexistent/decide", body, nil)
	rexData(t, w, http.StatusNotFound, false, nil)
}

func TestExemptionRequestValidation(t *testing.T) {
	r := exemptionRequestRouter(t)

	body := rexMustJSON(t, map[string]interface{}{
		"path": "x.go",
	})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/guard/exemption-requests", body, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing required fields: status=%d want=400", w.Code)
	}
}

// --- C: floweng template import/export/delete ---

func flowTemplateIORouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/api/v1/flows/templates/import", ImportFlowTemplate)
	r.GET("/api/v1/flows/templates/:tid/export", ExportFlowTemplate)
	r.DELETE("/api/v1/flows/templates/:tid", DeleteFlowTemplate)
	return r
}

func TestFlowTemplateImportExportRoundTrip(t *testing.T) {
	r := flowTemplateIORouter(t)

	tmpl := map[string]interface{}{
		"id": "test-roundtrip",
		"stages": []map[string]interface{}{
			{"type": "research", "name": "Research"},
			{"type": "review", "name": "Review"},
		},
	}
	body := rexMustJSON(t, tmpl)
	t.Cleanup(func() { _ = floweng.UnregisterTemplate("test-roundtrip") })

	w := rexRequest(t, r, http.MethodPost, "/api/v1/flows/templates/import", body, nil)
	var imp struct {
		ID string `json:"id"`
	}
	rexData(t, w, http.StatusCreated, true, &imp)
	if imp.ID != "test-roundtrip" {
		t.Fatalf("import id=%q want=test-roundtrip", imp.ID)
	}

	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows/templates/test-roundtrip/export", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export status=%d want=200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("content-type=%q want=application/json", ct)
	}
	var exported struct {
		ID     string        `json:"id"`
		Stages []interface{} `json:"stages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &exported); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if exported.ID != "test-roundtrip" || len(exported.Stages) != 2 {
		t.Fatalf("export mismatch: id=%q stages=%d", exported.ID, len(exported.Stages))
	}

	w = rexRequest(t, r, http.MethodDelete, "/api/v1/flows/templates/test-roundtrip", nil, nil)
	rexData(t, w, http.StatusOK, true, nil)

	w = rexRequest(t, r, http.MethodDelete, "/api/v1/flows/templates/test-roundtrip", nil, nil)
	rexData(t, w, http.StatusNotFound, false, nil)
}

func TestFlowTemplateDeleteBuiltin(t *testing.T) {
	r := flowTemplateIORouter(t)

	w := rexRequest(t, r, http.MethodDelete, "/api/v1/flows/templates/standard", nil, nil)
	rexData(t, w, http.StatusConflict, false, nil)
}

func TestFlowTemplateImportInvalid(t *testing.T) {
	r := flowTemplateIORouter(t)

	w := rexRequest(t, r, http.MethodPost, "/api/v1/flows/templates/import",
		[]byte(`not json`), nil)
	rexData(t, w, http.StatusBadRequest, false, nil)
}

func TestFlowTemplateExportNotFound(t *testing.T) {
	r := flowTemplateIORouter(t)

	w := rexRequest(t, r, http.MethodGet, "/api/v1/flows/templates/nonexistent/export", nil, nil)
	rexData(t, w, http.StatusNotFound, false, nil)
}
