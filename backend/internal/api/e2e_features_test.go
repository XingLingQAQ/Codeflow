// Package api — E2E tests for session features: workspace watch, flow gate
// escalation, debate solutions, guard exemption, and skill inject. Each test
// starts its own httptest.Server via setupE2EServer and swaps the missing
// experimental-module globals (floweng/workspace/guard/skill/debate) with
// in-memory implementations + t.Cleanup restore, mirroring the handler-level
// convention.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codeflow/backend/internal/debate"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
	"github.com/codeflow/backend/internal/skill"
	"github.com/codeflow/backend/internal/workspace"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// e2fJSON marshals v to JSON bytes for request bodies.
func e2fJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// e2fDecode decodes a Response-envelope body into out (the "data" value).
// Returns the raw top-level map for callers that need success/error fields.
func e2fDecode(t *testing.T, resp *http.Response, out interface{}) map[string]interface{} {
	t.Helper()
	var raw map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
	resp.Body.Close()
	if out != nil && raw["data"] != nil {
		b, _ := json.Marshal(raw["data"])
		require.NoError(t, json.Unmarshal(b, out))
	}
	return raw
}

// e2fDo issues a request and returns the response. method/url/body are
// straightforward; content-type is set for non-nil bodies.
func e2fDo(t *testing.T, client *http.Client, method, url string, body []byte) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

// ---------------------------------------------------------------------------
// 1. Workspace watch lifecycle
// ---------------------------------------------------------------------------

func TestE2EFeatures_WorkspaceWatchLifecycle(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	prev := workspace.GetService()
	workspace.SetService(workspace.NewFSService(nil))
	t.Cleanup(func() { workspace.SetService(prev) })

	client := ts.Client()
	root := t.TempDir()
	hdr := func(req *http.Request) { req.Header.Set("X-Codeflow-Workspace-Root", root) }

	// POST /workspace/watch → 201 + topic starts with "workspace:root:".
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workspace/watch", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	hdr(req)
	resp, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var watch struct {
		WatchID string `json:"watch_id"`
		Root    string `json:"root"`
		Topic   string `json:"topic"`
	}
	e2fDecode(t, resp, &watch)
	assert.NotEmpty(t, watch.WatchID)
	assert.True(t, strings.HasPrefix(watch.Topic, "workspace:root:"), "topic=%s", watch.Topic)
	watchID := watch.WatchID

	// GET /workspace/watches → list includes the watch.
	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/workspace/watches", nil)
	hdr(req)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var list struct {
		Items []struct {
			WatchID string `json:"watch_id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	e2fDecode(t, resp, &list)
	assert.GreaterOrEqual(t, list.Total, 1)
	found := false
	for _, it := range list.Items {
		if it.WatchID == watchID {
			found = true
		}
	}
	assert.True(t, found, "watch %s not in list", watchID)

	// POST again same root → 200 same id (idempotent).
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/workspace/watch", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	hdr(req)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var watch2 struct {
		WatchID string `json:"watch_id"`
	}
	e2fDecode(t, resp, &watch2)
	assert.Equal(t, watchID, watch2.WatchID)

	// DELETE → 200 stopped; DELETE again → 404.
	resp = e2fDo(t, client, "DELETE", ts.URL+"/api/v1/workspace/watch?id="+watchID, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp = e2fDo(t, client, "DELETE", ts.URL+"/api/v1/workspace/watch?id="+watchID, nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// 2. Flow gate-escalation journey
// ---------------------------------------------------------------------------

func TestE2EFeatures_FlowGateEscalation(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	prevEng := floweng.GetEngine()
	eng := floweng.NewInMemoryEngine(nil)
	floweng.SetEngine(eng)
	t.Cleanup(func() { floweng.SetEngine(prevEng) })

	tmplID := floweng.TemplateID("e2e_escalate_gate")
	require.NoError(t, floweng.RegisterTemplate(floweng.CustomTemplate{
		ID: tmplID,
		Stages: []floweng.CustomStage{
			{Type: floweng.StageTypeDesign, Name: "gate-check", Canvas: "design",
				Gates: []floweng.CustomGate{{
					Phase:  floweng.GatePhaseExit,
					Kind:   floweng.GateKindHumanApproval,
					OnFail: floweng.GateOnFailEscalateDebate,
				}}},
			{Type: floweng.StageTypeCoding, Name: "code", Canvas: "coding"},
		},
	}))
	t.Cleanup(func() { _ = floweng.UnregisterTemplate(tmplID) })

	client := ts.Client()

	// Create flow from the custom template.
	resp, err := client.Post(ts.URL+"/api/v1/flows",
		"application/json",
		bytes.NewReader(e2fJSON(t, map[string]string{
			"project_id":  "esc-proj",
			"template_id": string(tmplID),
		})))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	var flow floweng.Flow
	e2fDecode(t, resp, &flow)
	require.NotEmpty(t, flow.ID)
	require.Len(t, flow.Stages, 2)
	stageID := flow.Stages[0].ID
	require.Len(t, flow.Stages[0].Gates, 1)
	gateID := flow.Stages[0].Gates[0].ID

	// Advance → 409 (blocked on human_approval exit gate).
	resp = e2fDo(t, client, "POST",
		fmt.Sprintf("%s/api/v1/flows/%s/stages/%s/advance", ts.URL, flow.ID, stageID),
		[]byte(`{}`))
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	resp.Body.Close()

	// GET gates → gate visible with correct phase.
	resp = e2fDo(t, client, "GET",
		fmt.Sprintf("%s/api/v1/flows/%s/gates", ts.URL, flow.ID), nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var gates struct {
		Items []struct {
			ID    string `json:"id"`
			Phase string `json:"phase"`
		} `json:"items"`
	}
	e2fDecode(t, resp, &gates)
	require.NotEmpty(t, gates.Items)
	assert.Equal(t, gateID, gates.Items[0].ID)

	// Decide REJECT → gate.escalate_debate event emitted.
	f := false
	resp = e2fDo(t, client, "POST",
		fmt.Sprintf("%s/api/v1/flows/%s/gates/%s/decide", ts.URL, flow.ID, gateID),
		e2fJSON(t, map[string]interface{}{"approved": &f, "reason": "needs debate"}))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp = e2fDo(t, client, "GET",
		fmt.Sprintf("%s/api/v1/flows/%s/events?type=gate.escalate_debate", ts.URL, flow.ID), nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var evs struct {
		Items []floweng.FlowEvent `json:"items"`
		Total int                 `json:"total"`
	}
	e2fDecode(t, resp, &evs)
	assert.GreaterOrEqual(t, evs.Total, 1, "expected gate.escalate_debate event")

	// Decide APPROVE → gate passes; advance succeeds.
	tr := true
	resp = e2fDo(t, client, "POST",
		fmt.Sprintf("%s/api/v1/flows/%s/gates/%s/decide", ts.URL, flow.ID, gateID),
		e2fJSON(t, map[string]interface{}{"approved": &tr}))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp = e2fDo(t, client, "POST",
		fmt.Sprintf("%s/api/v1/flows/%s/stages/%s/advance", ts.URL, flow.ID, stageID),
		[]byte(`{}`))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var advanced floweng.Flow
	e2fDecode(t, resp, &advanced)
	assert.Equal(t, floweng.StageStatusDone, advanced.Stages[0].Status)
}

// ---------------------------------------------------------------------------
// 3. Debate solutions round-trip
// ---------------------------------------------------------------------------

func TestE2EFeatures_DebateSolutionsRoundTrip(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	prevDM := debate.GetDebateManager()
	debate.SetDebateManager(debate.NewInMemoryDebateManager())
	t.Cleanup(func() { debate.SetDebateManager(prevDM) })

	client := ts.Client()

	// Create debate.
	resp, err := client.Post(ts.URL+"/api/v1/debates",
		"application/json",
		bytes.NewReader(e2fJSON(t, map[string]interface{}{
			"title":         "E2E Solutions",
			"generator_id":  "gen-1",
			"critic_id":     "crit-1",
			"initial_input": "implement caching layer",
		})))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	var created debate.Debate
	e2fDecode(t, resp, &created)
	debateID := created.ID
	require.NotEmpty(t, debateID)
	assert.Equal(t, debate.DebateStatusInProgress, created.Status)

	// Next round with outputs.
	resp = e2fDo(t, client, "POST",
		fmt.Sprintf("%s/api/v1/debates/%s/next-round", ts.URL, debateID),
		e2fJSON(t, map[string]interface{}{
			"generator_output": "Use Redis for hot keys",
			"critic_feedback":  "Consider TTL policy",
		}))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Propose a solution.
	resp = e2fDo(t, client, "POST",
		fmt.Sprintf("%s/api/v1/debates/%s/solutions", ts.URL, debateID),
		e2fJSON(t, map[string]interface{}{
			"proposed_by": "gen-1",
			"role":        "generator",
			"title":       "Redis with TTL",
			"description": "Use Redis caching with configurable TTL per key family",
		}))
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	var sol debate.Solution
	e2fDecode(t, resp, &sol)
	require.NotEmpty(t, sol.ID)
	assert.Equal(t, "Redis with TTL", sol.Title)

	// Select the solution → status becomes resolved.
	resp = e2fDo(t, client, "POST",
		fmt.Sprintf("%s/api/v1/debates/%s/select-solution", ts.URL, debateID),
		e2fJSON(t, map[string]string{"solution_id": sol.ID}))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var selected debate.Debate
	e2fDecode(t, resp, &selected)
	assert.Equal(t, debate.DebateStatusResolved, selected.Status)
	assert.Equal(t, sol.ID, selected.SelectedSolution)

	// Export report shows solution + resolved.
	resp = e2fDo(t, client, "GET",
		fmt.Sprintf("%s/api/v1/debates/%s/export", ts.URL, debateID), nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var report debate.AuditReport
	e2fDecode(t, resp, &report)
	assert.Equal(t, debate.DebateStatusResolved, report.Status)
	assert.Equal(t, sol.ID, report.SelectedSolution)
	require.NotEmpty(t, report.Solutions)
	assert.Equal(t, "Redis with TTL", report.Solutions[0].Title)
}

// ---------------------------------------------------------------------------
// 4. Guard exemption over HTTP
// ---------------------------------------------------------------------------

func TestE2EFeatures_GuardExemption(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	guardEng := guard.NewEngine(nil, nil)
	prevGuard := guard.GetService()
	guard.SetService(guardEng)
	t.Cleanup(func() { guard.SetService(prevGuard) })

	prevWS := workspace.GetService()
	workspace.SetService(workspace.NewFSService(guardEng))
	t.Cleanup(func() { workspace.SetService(prevWS) })

	client := ts.Client()
	root := t.TempDir()

	// Build the absolute path the way both the exemption normalizer and the
	// workspace Resolve will: abs + EvalSymlinks on the existing root, then
	// join the leaf that does not exist yet.
	absRoot, _ := filepath.Abs(root)
	if r, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = r
	}
	exemptPath := filepath.Join(absRoot, "service_v2.go")

	// Grant an exemption for the stacked-naming path.
	resp := e2fDo(t, client, "POST", ts.URL+"/api/v1/guard/exempt",
		e2fJSON(t, map[string]interface{}{
			"path":        exemptPath,
			"ttl_seconds": 3600,
			"reason":      "e2e test",
		}))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Write to that stacked-naming path → succeeds (guard exempted).
	writeBody := e2fJSON(t, map[string]interface{}{
		"path":           "service_v2.go",
		"content_text":   "package svc\n",
		"create_parents": true,
	})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workspace/write", bytes.NewReader(writeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codeflow-Workspace-Root", root)
	resp, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "write should succeed while exempted")
	resp.Body.Close()

	// Clear the exemption.
	resp = e2fDo(t, client, "DELETE",
		ts.URL+"/api/v1/guard/exempt?path="+exemptPath, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Same write now blocked → 403 (handler maps "blocked by guard").
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/workspace/write",
		bytes.NewReader(writeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codeflow-Workspace-Root", root)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "write should be blocked after exemption cleared")
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// 5. Skill inject E2E
// ---------------------------------------------------------------------------

func TestE2EFeatures_SkillInject(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	prevReg := skill.GetRegistry()
	skill.SetRegistry(skill.NewInMemoryRegistry())
	t.Cleanup(func() { skill.SetRegistry(prevReg) })

	client := ts.Client()

	// Create a user skill with a trigger.
	resp, err := client.Post(ts.URL+"/api/v1/skills",
		"application/json",
		bytes.NewReader(e2fJSON(t, map[string]interface{}{
			"name":        "cache-first",
			"body":        "Always check the cache before hitting the database.",
			"triggers":    []string{"cache", "performance"},
			"stage_tags":  []string{"coding"},
		})))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	// POST /skills/inject with matching text → injection block returned.
	resp = e2fDo(t, client, "POST", ts.URL+"/api/v1/skills/inject",
		e2fJSON(t, map[string]interface{}{
			"text":       "how to improve cache performance",
			"stage_type": "coding",
		}))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var injectOut struct {
		Injection string `json:"injection"`
	}
	e2fDecode(t, resp, &injectOut)
	assert.Contains(t, injectOut.Injection, "## Active Skills")
	assert.Contains(t, injectOut.Injection, "Always check the cache before hitting the database.")
}

// ---------------------------------------------------------------------------
// convenience: run a targeted subset via -run
// ---------------------------------------------------------------------------
// All tests above start with TestE2EFeatures_ so they match
//   go test -run 'E2EFeatures' ./internal/api/
// and also match the full-suite pattern
//   go test -run 'E2EFeatures|TestE2E' ./internal/api/
