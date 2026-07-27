package handlers

// HTTP handler-level coverage for flow routes that lacked it: delete, artifact
// status PATCH, single stage/artifact GET, gate flattening, active-stage,
// list filtering, event filter+limit composition, template describe, and a
// walkable import_project flow. Uses floweng.NewInMemoryEngine swapped via the
// process-wide singleton (restored on cleanup), mirroring flows_test.go.

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/floweng"
)

// rexFlowRouter swaps in a fresh in-memory engine and returns a router wired to
// the flow handlers under test. Template routes are registered on their own
// router per-test to keep this shared router free of static/param siblings.
func rexFlowRouter(t *testing.T) (*gin.Engine, *floweng.InMemoryEngine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	eng := floweng.NewInMemoryEngine(nil)
	prev := floweng.GetEngine()
	floweng.SetEngine(eng)
	t.Cleanup(func() { floweng.SetEngine(prev) })

	r := gin.New()
	r.POST("/api/v1/flows", CreateFlow)
	r.GET("/api/v1/flows", ListFlows)
	r.GET("/api/v1/flows/:id", GetFlow)
	r.DELETE("/api/v1/flows/:id", DeleteFlow)
	r.GET("/api/v1/flows/:id/events", ListFlowEvents)
	r.GET("/api/v1/flows/:id/active-stage", GetActiveFlowStage)
	r.GET("/api/v1/flows/:id/gates", ListFlowGates)
	r.GET("/api/v1/flows/:id/stages/:sid", GetFlowStage)
	r.GET("/api/v1/flows/:id/artifacts/:aid", GetFlowArtifact)
	r.PATCH("/api/v1/flows/:id/artifacts/:aid", UpdateFlowArtifactStatus)
	r.POST("/api/v1/flows/:id/stages/:sid/advance", AdvanceFlowStage)
	r.POST("/api/v1/flows/:id/abort", AbortFlow)
	return r, eng
}

func rexNewFlow(t *testing.T, eng *floweng.InMemoryEngine, projectID string, tmpl floweng.TemplateID) *floweng.Flow {
	t.Helper()
	flow, err := eng.Create(nil, &floweng.CreateFlowRequest{ProjectID: projectID, TemplateID: tmpl})
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	return flow
}

// 1. DELETE /api/v1/flows/:id
func TestDeleteFlowRoutesExtra(t *testing.T) {
	r, eng := rexFlowRouter(t)
	flow := rexNewFlow(t, eng, "proj-del", floweng.TemplateNewProject)

	w := rexRequest(t, r, http.MethodDelete, "/api/v1/flows/"+flow.ID, nil, nil)
	var data struct {
		Deleted bool `json:"deleted"`
	}
	rexData(t, w, http.StatusOK, true, &data)
	if !data.Deleted {
		t.Fatalf("expected deleted=true, body=%s", w.Body.String())
	}

	// Deleting again (now missing) maps to 404.
	w = rexRequest(t, r, http.MethodDelete, "/api/v1/flows/"+flow.ID, nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing status=%d want=404 body=%s", w.Code, w.Body.String())
	}
}

// 2. PATCH /api/v1/flows/:id/artifacts/:aid (UpdateFlowArtifactStatus)
func TestUpdateFlowArtifactStatusRoutesExtra(t *testing.T) {
	r, eng := rexFlowRouter(t)
	flow := rexNewFlow(t, eng, "proj-art", floweng.TemplateNewProject)
	art, err := eng.AttachArtifact(nil, flow.ID, flow.Stages[0].ID, "design_doc", "ref://x")
	if err != nil {
		t.Fatalf("attach artifact: %v", err)
	}

	// approve → 200 with updated status
	body := rexMustJSON(t, map[string]string{"status": "approved"})
	w := rexRequest(t, r, http.MethodPatch, "/api/v1/flows/"+flow.ID+"/artifacts/"+art.ID, body, nil)
	var out floweng.Artifact
	rexData(t, w, http.StatusOK, true, &out)
	if out.Status != floweng.ArtifactStatusApproved {
		t.Fatalf("status=%q want=approved body=%s", out.Status, w.Body.String())
	}

	// invalid status value → 400 (bound ok, engine rejects)
	body = rexMustJSON(t, map[string]string{"status": "bogus"})
	w = rexRequest(t, r, http.MethodPatch, "/api/v1/flows/"+flow.ID+"/artifacts/"+art.ID, body, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status status=%d want=400 body=%s", w.Code, w.Body.String())
	}

	// missing status field → 400 (binding:"required")
	w = rexRequest(t, r, http.MethodPatch, "/api/v1/flows/"+flow.ID+"/artifacts/"+art.ID, []byte(`{}`), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing status status=%d want=400 body=%s", w.Code, w.Body.String())
	}

	// unknown artifact → 404
	body = rexMustJSON(t, map[string]string{"status": "approved"})
	w = rexRequest(t, r, http.MethodPatch, "/api/v1/flows/"+flow.ID+"/artifacts/00000000-0000-0000-0000-000000000000", body, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing artifact status=%d want=404 body=%s", w.Code, w.Body.String())
	}
}

// 3. GET /api/v1/flows/:id/stages/:sid
func TestGetFlowStageRoutesExtra(t *testing.T) {
	r, eng := rexFlowRouter(t)
	flow := rexNewFlow(t, eng, "proj-stage", floweng.TemplateNewProject)
	sid := flow.Stages[0].ID

	w := rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/stages/"+sid, nil, nil)
	var stage floweng.Stage
	rexData(t, w, http.StatusOK, true, &stage)
	if stage.ID != sid {
		t.Fatalf("stage id=%q want=%q", stage.ID, sid)
	}
	if stage.Status != floweng.StageStatusActive {
		t.Fatalf("first stage status=%q want=active", stage.Status)
	}

	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/stages/nope", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing stage status=%d want=404 body=%s", w.Code, w.Body.String())
	}
}

// 4. GET /api/v1/flows/:id/artifacts/:aid
func TestGetFlowArtifactRoutesExtra(t *testing.T) {
	r, eng := rexFlowRouter(t)
	flow := rexNewFlow(t, eng, "proj-getart", floweng.TemplateNewProject)
	art, err := eng.AttachArtifact(nil, flow.ID, flow.Stages[0].ID, "plan", "ref://p")
	if err != nil {
		t.Fatalf("attach artifact: %v", err)
	}

	w := rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/artifacts/"+art.ID, nil, nil)
	var out floweng.Artifact
	rexData(t, w, http.StatusOK, true, &out)
	if out.ID != art.ID {
		t.Fatalf("artifact id=%q want=%q", out.ID, art.ID)
	}

	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/artifacts/nope", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing artifact status=%d want=404 body=%s", w.Code, w.Body.String())
	}
}

// 5. GET /api/v1/flows/:id/gates
func TestListFlowGatesRoutesExtra(t *testing.T) {
	r, eng := rexFlowRouter(t)
	flow := rexNewFlow(t, eng, "proj-gates", floweng.TemplateNewProject)

	w := rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/gates", nil, nil)
	var out struct {
		Items []struct {
			StageID   string `json:"stage_id"`
			StageType string `json:"stage_type"`
			ID        string `json:"id"`
			Phase     string `json:"phase"`
		} `json:"items"`
		Total int `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &out)
	// new_project seeds one auto exit gate per stage (7 stages).
	if out.Total != len(flow.Stages) {
		t.Fatalf("gate total=%d want=%d", out.Total, len(flow.Stages))
	}
	if len(out.Items) == 0 {
		t.Fatal("expected at least one gate row")
	}
	for i, row := range out.Items {
		if row.StageID == "" || row.StageType == "" || row.ID == "" {
			t.Fatalf("row %d missing flattened fields: %+v", i, row)
		}
	}
	if out.Items[0].StageType != string(floweng.StageTypeIdea) {
		t.Fatalf("first gate stage_type=%q want=%q", out.Items[0].StageType, floweng.StageTypeIdea)
	}
}

// 6. GET /api/v1/flows/:id/active-stage
func TestGetActiveFlowStageRoutesExtra(t *testing.T) {
	r, eng := rexFlowRouter(t)
	flow := rexNewFlow(t, eng, "proj-active", floweng.TemplateNewProject)

	w := rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/active-stage", nil, nil)
	var stage floweng.Stage
	rexData(t, w, http.StatusOK, true, &stage)
	if stage.ID != flow.Stages[0].ID || stage.Status != floweng.StageStatusActive {
		t.Fatalf("active stage=%+v want id=%s active", stage, flow.Stages[0].ID)
	}

	// Abort clears the active stage; active-stage then 404s.
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows/"+flow.ID+"/abort", []byte(`{"reason":"stop"}`), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("abort status=%d want=200 body=%s", w.Code, w.Body.String())
	}
	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/active-stage", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("active-stage after abort status=%d want=404 body=%s", w.Code, w.Body.String())
	}
}

// 7. GET /api/v1/flows?project_id=&status=
func TestListFlowsFilterRoutesExtra(t *testing.T) {
	r, eng := rexFlowRouter(t)
	flowA := rexNewFlow(t, eng, "proj-a", floweng.TemplateNewProject)
	flowB := rexNewFlow(t, eng, "proj-b", floweng.TemplateNewProject)

	// Abort B so it leaves the active set.
	w := rexRequest(t, r, http.MethodPost, "/api/v1/flows/"+flowB.ID+"/abort", []byte(`{}`), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("abort B status=%d body=%s", w.Code, w.Body.String())
	}

	type listOut struct {
		Items []floweng.Flow `json:"items"`
		Total int            `json:"total"`
	}

	// project_id + status=active → only A
	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows?project_id=proj-a&status=active", nil, nil)
	var got listOut
	rexData(t, w, http.StatusOK, true, &got)
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != flowA.ID {
		t.Fatalf("proj-a active = %+v want single A(%s)", got, flowA.ID)
	}

	// proj-b + active → none (B aborted)
	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows?project_id=proj-b&status=active", nil, nil)
	got = listOut{}
	rexData(t, w, http.StatusOK, true, &got)
	if got.Total != 0 {
		t.Fatalf("proj-b active total=%d want=0", got.Total)
	}

	// proj-b + aborted → only B
	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows?project_id=proj-b&status=aborted", nil, nil)
	got = listOut{}
	rexData(t, w, http.StatusOK, true, &got)
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != flowB.ID {
		t.Fatalf("proj-b aborted = %+v want single B(%s)", got, flowB.ID)
	}

	// status=active only (no project) → only A across the engine
	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows?status=active", nil, nil)
	got = listOut{}
	rexData(t, w, http.StatusOK, true, &got)
	if got.Total != 1 || got.Items[0].ID != flowA.ID {
		t.Fatalf("global active = %+v want single A(%s)", got, flowA.ID)
	}
}

// 8. GET /api/v1/flows/:id/events?type=&limit=
func TestListFlowEventsFilterRoutesExtra(t *testing.T) {
	r, eng := rexFlowRouter(t)
	flow := rexNewFlow(t, eng, "proj-events", floweng.TemplateNewProject)
	s0, s1 := flow.Stages[0].ID, flow.Stages[1].ID

	// Two advances → two stage.done events (for s0 then s1).
	for _, sid := range []string{s0, s1} {
		w := rexRequest(t, r, http.MethodPost, "/api/v1/flows/"+flow.ID+"/stages/"+sid+"/advance", []byte(`{}`), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("advance %s status=%d body=%s", sid, w.Code, w.Body.String())
		}
	}

	type evOut struct {
		Items []floweng.FlowEvent `json:"items"`
		Total int                 `json:"total"`
	}

	// type filter alone → both stage.done events
	w := rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/events?type=stage.done", nil, nil)
	var got evOut
	rexData(t, w, http.StatusOK, true, &got)
	if got.Total != 2 {
		t.Fatalf("stage.done total=%d want=2 body=%s", got.Total, w.Body.String())
	}

	// type + limit=1 → most recent stage.done (s1) only
	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/events?type=stage.done&limit=1", nil, nil)
	got = evOut{}
	rexData(t, w, http.StatusOK, true, &got)
	if got.Total != 1 || len(got.Items) != 1 {
		t.Fatalf("limited total=%d want=1 body=%s", got.Total, w.Body.String())
	}
	if got.Items[0].Type != "stage.done" || got.Items[0].StageID != s1 {
		t.Fatalf("limited event=%+v want stage.done for s1(%s)", got.Items[0], s1)
	}
}

// 9. GET /api/v1/flows/templates/:tid
func TestGetFlowTemplateRoutesExtra(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/flows/templates/:tid", GetFlowTemplate)

	// known: new_project → 7 stages, id echoed
	w := rexRequest(t, r, http.MethodGet, "/api/v1/flows/templates/new_project", nil, nil)
	var info floweng.TemplateInfo
	rexData(t, w, http.StatusOK, true, &info)
	if info.ID != floweng.TemplateNewProject {
		t.Fatalf("template id=%q want=new_project", info.ID)
	}
	if len(info.Stages) != 7 {
		t.Fatalf("new_project stages=%d want=7", len(info.Stages))
	}

	// known: import_project → first stage is import
	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows/templates/import_project", nil, nil)
	info = floweng.TemplateInfo{}
	rexData(t, w, http.StatusOK, true, &info)
	if len(info.Stages) == 0 || info.Stages[0].Type != floweng.StageTypeImport {
		t.Fatalf("import_project first stage=%+v want import", info.Stages)
	}

	// unknown → 404
	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows/templates/does_not_exist", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown template status=%d want=404 body=%s", w.Code, w.Body.String())
	}
}

// 10. POST /api/v1/flows with import_project, then walk the first two stages.
func TestImportTemplateWalkableRoutesExtra(t *testing.T) {
	r, _ := rexFlowRouter(t)

	body := rexMustJSON(t, map[string]string{"project_id": "imp", "template_id": "import_project"})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/flows", body, nil)
	var flow floweng.Flow
	rexData(t, w, http.StatusCreated, true, &flow)
	if flow.ID == "" || len(flow.Stages) != 7 {
		t.Fatalf("created flow=%+v want 7 stages", flow)
	}
	if flow.Stages[0].Type != floweng.StageTypeImport || flow.Stages[1].Type != floweng.StageTypeComprehend {
		t.Fatalf("import template stage order unexpected: %s,%s", flow.Stages[0].Type, flow.Stages[1].Type)
	}

	// Advance 导入 then 理解 (both auto exit gates) over HTTP.
	for _, sid := range []string{flow.Stages[0].ID, flow.Stages[1].ID} {
		w = rexRequest(t, r, http.MethodPost, "/api/v1/flows/"+flow.ID+"/stages/"+sid+"/advance", []byte(`{}`), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("advance %s status=%d body=%s", sid, w.Code, w.Body.String())
		}
	}

	// Planning (stage index 2) is now the active stage.
	w = rexRequest(t, r, http.MethodGet, "/api/v1/flows/"+flow.ID+"/active-stage", nil, nil)
	var active floweng.Stage
	rexData(t, w, http.StatusOK, true, &active)
	if active.Type != floweng.StageTypePlanning {
		t.Fatalf("active after two advances=%q want=planning", active.Type)
	}
}
