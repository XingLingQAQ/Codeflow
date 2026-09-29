package handlers

// HTTP-level coverage of the T3.01.a group 2 rule: one active project flow per
// project. CreateFlow must answer 409 and name the flow holding the slot, a
// second project flow must not be written, task flows stay unlimited, a bad
// parent_project_flow_id is a 400, and the resume route (POST /flows/:id/resume)
// answers 200/404/409. The suspended flow is produced the way the store
// migration produces one — a direct store write — because the migration itself
// is covered in floweng; the route only needs the state.

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/codeflow/backend/internal/floweng"
)

// fsaRouter wires the flow routes this test needs, including the new resume
// route, and installs eng as the process-wide engine for the test's duration.
func fsaRouter(t *testing.T, eng *floweng.InMemoryEngine) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	prev := floweng.GetEngine()
	floweng.SetEngine(eng)
	t.Cleanup(func() { floweng.SetEngine(prev) })

	r := gin.New()
	r.POST("/api/v1/flows", CreateFlow)
	r.GET("/api/v1/flows/:id", GetFlow)
	r.POST("/api/v1/flows/:id/abort", AbortFlow)
	r.POST("/api/v1/flows/:id/resume", ResumeFlow)
	return r
}

// TestCreateFlowRefusesASecondActiveProjectFlow pins the 409 mapping and that
// the refusal carries the id of the flow that holds the project's slot.
func TestCreateFlowRefusesASecondActiveProjectFlow(t *testing.T) {
	eng := floweng.NewInMemoryEngine(nil)
	r := fsaRouter(t, eng)

	body := rexMustJSON(t, map[string]any{"project_id": "proj-api-one", "template_id": "new_project"})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/flows", body, nil)
	var first floweng.Flow
	rexData(t, w, http.StatusCreated, true, &first)

	// The same project asks for a second project flow: 409, message names the
	// first flow, and the engine still holds exactly one flow for the project.
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows", body, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("second project flow status=%d want=409 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), first.ID) {
		t.Fatalf("409 body does not name the flow holding the slot: %s", w.Body.String())
	}
	flows, err := eng.List(nil, "proj-api-one")
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].ID != first.ID {
		t.Fatalf("after the 409 the project holds %d flows", len(flows))
	}

	// A task flow of the same project is not limited by the rule.
	taskBody := rexMustJSON(t, map[string]any{
		"project_id": "proj-api-one", "template_id": "new_project", "kind": "task",
	})
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows", taskBody, nil)
	var task floweng.Flow
	rexData(t, w, http.StatusCreated, true, &task)
	if task.Kind != floweng.FlowKindTask {
		t.Fatalf("task flow kind=%q want=task", task.Kind)
	}

	// A different project is unaffected.
	otherBody := rexMustJSON(t, map[string]any{"project_id": "proj-api-two", "template_id": "new_project"})
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows", otherBody, nil)
	rexData(t, w, http.StatusCreated, true, nil)

	// An unknown kind is a bad request, not a 500.
	badKind := rexMustJSON(t, map[string]any{"project_id": "proj-api-two", "template_id": "new_project", "kind": "epic"})
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows", badKind, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind status=%d want=400 body=%s", w.Code, w.Body.String())
	}
}

// TestCreateFlowValidatesParentProjectFlowID pins the 400 mapping for every
// misuse of parent_project_flow_id.
func TestCreateFlowValidatesParentProjectFlowID(t *testing.T) {
	eng := floweng.NewInMemoryEngine(nil)
	r := fsaRouter(t, eng)

	parentBody := rexMustJSON(t, map[string]any{"project_id": "proj-api-parent", "template_id": "new_project"})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/flows", parentBody, nil)
	var parent floweng.Flow
	rexData(t, w, http.StatusCreated, true, &parent)

	task, err := eng.Create(nil, &floweng.CreateFlowRequest{ProjectID: "proj-api-parent", Kind: floweng.FlowKindTask})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]map[string]any{
		"unknown parent":  {"project_id": "proj-api-parent", "template_id": "new_project", "kind": "task", "parent_project_flow_id": uuid.NewString()},
		"task as parent":  {"project_id": "proj-api-parent", "template_id": "new_project", "kind": "task", "parent_project_flow_id": task.ID},
		"another project": {"project_id": "proj-api-elsewhere", "template_id": "new_project", "kind": "task", "parent_project_flow_id": parent.ID},
	}
	for name, raw := range cases {
		body := map[string]any{}
		for k, v := range raw {
			body[k] = v
		}
		w := rexRequest(t, r, http.MethodPost, "/api/v1/flows", rexMustJSON(t, body), nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d want=400 body=%s", name, w.Code, w.Body.String())
		}
	}

	// A valid parent passes and is carried into the created flow.
	taskBody := rexMustJSON(t, map[string]any{
		"project_id": "proj-api-parent", "template_id": "new_project", "kind": "task", "parent_project_flow_id": parent.ID,
	})
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows", taskBody, nil)
	var child floweng.Flow
	rexData(t, w, http.StatusCreated, true, &child)
	if child.ParentProjectFlowID != parent.ID {
		t.Fatalf("created task parent=%q want=%q", child.ParentProjectFlowID, parent.ID)
	}
}

// TestResumeFlowRoute covers the resume route on a real SQLite store, the store
// the migration runs on: slot taken → 409 naming the holder, unknown → 404,
// active → 409, and after the holder is aborted a suspended flow resumes with
// 200 and holds the slot afterwards.
func TestResumeFlowRoute(t *testing.T) {
	store, err := floweng.NewSQLiteFlowStore(filepath.Join(t.TempDir(), "floweng.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	eng := floweng.NewEngineWithStore(store, nil)
	r := fsaRouter(t, eng)

	body := rexMustJSON(t, map[string]any{"project_id": "proj-api-resume", "template_id": "new_project"})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/flows", body, nil)
	var primary floweng.Flow
	rexData(t, w, http.StatusCreated, true, &primary)

	parked := fsaSuspendedFlow(t, store, "proj-api-resume")

	// Slot taken → 409 naming the holder.
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows/"+parked.ID+"/resume", nil, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("resume while the slot is taken status=%d want=409 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), primary.ID) {
		t.Fatalf("the 409 does not name the holder: %s", w.Body.String())
	}

	// Unknown flow → 404.
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows/"+uuid.NewString()+"/resume", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("resume unknown flow status=%d want=404 body=%s", w.Code, w.Body.String())
	}

	// Active (not suspended) → 409.
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows/"+primary.ID+"/resume", nil, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("resume an active flow status=%d want=409 body=%s", w.Code, w.Body.String())
	}

	// Free the slot, then resume → 200, active, event recorded.
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows/"+primary.ID+"/abort", []byte(`{"reason":"switch"}`), nil)
	rexData(t, w, http.StatusOK, true, nil)

	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows/"+parked.ID+"/resume", nil, nil)
	var resumed floweng.Flow
	rexData(t, w, http.StatusOK, true, &resumed)
	if resumed.ID != parked.ID || resumed.Status != floweng.FlowStatusActive {
		t.Fatalf("resumed flow id=%s status=%s want %s/active", resumed.ID, resumed.Status, parked.ID)
	}
	found := false
	for _, ev := range resumed.Events {
		if ev.Type == "flow.resumed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("resumed flow carries no flow.resumed event: %+v", resumed.Events)
	}

	// And the resumed flow now holds the slot: a new project flow is refused.
	w = rexRequest(t, r, http.MethodPost, "/api/v1/flows", body, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("create while the resumed flow holds the slot status=%d want=409 body=%s", w.Code, w.Body.String())
	}
}

// fsaSuspendedFlow writes a suspended project flow straight into the store, the
// shape the migration leaves behind.
func fsaSuspendedFlow(t *testing.T, store floweng.FlowStore, projectID string) *floweng.Flow {
	t.Helper()
	now := time.Now().UTC()
	flow := &floweng.Flow{
		ID:         uuid.NewString(),
		ProjectID:  projectID,
		TemplateID: floweng.TemplateNewProject,
		Status:     floweng.FlowStatusSuspended,
		Kind:       floweng.FlowKindProject,
		Stages: []floweng.Stage{{ID: uuid.NewString(), Type: floweng.StageTypeIdea, Name: "想法",
			Canvas: "intent", Status: floweng.StageStatusActive, Order: 0}},
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	if err := store.Put(flow); err != nil {
		t.Fatalf("store suspended flow: %v", err)
	}
	return flow
}
