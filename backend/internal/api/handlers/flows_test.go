package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/floweng"
)

func TestFlowAPICreateAdvanceSkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	eng := floweng.NewInMemoryEngine(nil)
	prev := floweng.GetEngine()
	floweng.SetEngine(eng)
	t.Cleanup(func() { floweng.SetEngine(prev) })

	r := gin.New()
	r.POST("/api/v1/flows", CreateFlow)
	r.GET("/api/v1/flows/:id", GetFlow)
	r.POST("/api/v1/flows/:id/stages/:sid/advance", AdvanceFlowStage)
	r.POST("/api/v1/flows/:id/stages/:sid/skip", SkipFlowStage)
	r.GET("/api/v1/flows/templates", ListFlowTemplates)

	// templates
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/flows/templates", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("templates status=%d body=%s", w.Code, w.Body.String())
	}

	// create
	body, _ := json.Marshal(map[string]string{
		"project_id":  "proj-api",
		"template_id": "new_project",
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/v1/flows", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		// respondCreated may be 201
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}

	var created struct {
		Success bool         `json:"success"`
		Data    floweng.Flow `json:"data"`
	}
	// handlers may wrap in Response
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	flow := created.Data
	if flow.ID == "" {
		// maybe unwrapped
		var raw floweng.Flow
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err == nil && raw.ID != "" {
			flow = raw
		}
	}
	if flow.ID == "" {
		t.Fatalf("no flow id in response: %s", w.Body.String())
	}

	activeID := ""
	for _, s := range flow.Stages {
		if s.Status == floweng.StageStatusActive {
			activeID = s.ID
			break
		}
	}
	if activeID == "" {
		t.Fatal("no active stage")
	}

	// advance idea
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/v1/flows/"+flow.ID+"/stages/"+activeID+"/advance", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("advance status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDecideFlowGateRequiresApproved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	eng := floweng.NewInMemoryEngine(nil)
	prev := floweng.GetEngine()
	floweng.SetEngine(eng)
	t.Cleanup(func() { floweng.SetEngine(prev) })

	flow, err := eng.Create(nil, &floweng.CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.POST("/api/v1/flows/:id/gates/:gid/decide", DecideFlowGate)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/flows/"+flow.ID+"/gates/"+flow.Stages[0].Gates[0].ID+"/decide", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdvanceFlowStageRejectsStalePathStage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	eng := floweng.NewInMemoryEngine(nil)
	prev := floweng.GetEngine()
	floweng.SetEngine(eng)
	t.Cleanup(func() { floweng.SetEngine(prev) })

	flow, err := eng.Create(nil, &floweng.CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.POST("/api/v1/flows/:id/stages/:sid/advance", AdvanceFlowStage)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/flows/"+flow.ID+"/stages/"+flow.Stages[1].ID+"/advance", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestFlowAPISkipNonOptional(t *testing.T) {
	gin.SetMode(gin.TestMode)
	eng := floweng.NewInMemoryEngine(nil)
	prev := floweng.GetEngine()
	floweng.SetEngine(eng)
	t.Cleanup(func() { floweng.SetEngine(prev) })

	flow, err := eng.Create(nil, &floweng.CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.POST("/api/v1/flows/:id/stages/:sid/skip", SkipFlowStage)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/flows/"+flow.ID+"/stages/"+flow.Stages[0].ID+"/skip", nil)
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("expected conflict/error, got 200")
	}
}
