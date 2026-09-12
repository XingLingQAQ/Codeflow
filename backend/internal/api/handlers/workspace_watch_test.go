package handlers

// HTTP handler-level coverage for the experimental workspace file-watch API.
// A fresh in-memory FSService is installed and the package watch registry is
// drained around every test so watchers never leak across cases. Assertions go
// through the shared rex* envelope helpers (routes_extra_common_test.go).

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/workspace"
)

// resetWatchRegistry stops every active watcher and clears the registry maps so
// one test cannot see (or leak goroutines into) another.
func resetWatchRegistry(t *testing.T) {
	t.Helper()
	watchRegistry.mu.Lock()
	entries := make([]*watchEntry, 0, len(watchRegistry.byID))
	for _, e := range watchRegistry.byID {
		entries = append(entries, e)
	}
	watchRegistry.byID = make(map[string]*watchEntry)
	watchRegistry.byRoot = make(map[string]*watchEntry)
	watchRegistry.mu.Unlock()
	for _, e := range entries {
		e.cancel()
		e.watcher.Stop()
	}
}

// watchTestRouter installs a fresh workspace service, resets the registry, and
// returns a minimal router with the three watch routes. Cleanup stops watchers
// and restores the prior service (or the unset state).
func watchTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	hadSvc := workspace.HasService()
	var prev workspace.Service
	if hadSvc {
		prev = workspace.GetService()
	}
	workspace.SetService(workspace.NewFSService(nil))
	resetWatchRegistry(t)
	t.Cleanup(func() {
		resetWatchRegistry(t)
		if hadSvc {
			workspace.SetService(prev)
		} else {
			workspace.SetService(nil)
		}
	})

	r := gin.New()
	r.POST("/api/v1/workspace/watch", CreateWorkspaceWatch)
	r.GET("/api/v1/workspace/watches", ListWorkspaceWatches)
	r.DELETE("/api/v1/workspace/watch", DeleteWorkspaceWatch)
	return r
}

func createWatch(t *testing.T, r *gin.Engine, root string, intervalMs int) (int, watchView) {
	t.Helper()
	payload := map[string]interface{}{}
	if intervalMs != 0 {
		payload["interval_ms"] = intervalMs
	}
	w := rexRequest(t, r, http.MethodPost, "/api/v1/workspace/watch", rexMustJSON(t, payload), map[string]string{"X-Codeflow-Workspace-Root": root})
	var view watchView
	env := rexDecode(t, w)
	if env.Success && len(env.Data) > 0 {
		rexData(t, w, w.Code, true, &view)
	}
	return w.Code, view
}

func TestWorkspaceWatchCreateListDelete(t *testing.T) {
	r := watchTestRouter(t)
	dir := t.TempDir()

	// Create: 201 with a per-root topic and the default interval.
	code, view := createWatch(t, r, dir, 0)
	if code != http.StatusCreated {
		t.Fatalf("create status=%d want=201", code)
	}
	if view.WatchID == "" {
		t.Fatalf("expected non-empty watch_id")
	}
	if !strings.HasPrefix(view.Topic, "workspace:root:") {
		t.Fatalf("topic=%q want prefix workspace:root:", view.Topic)
	}
	if view.IntervalMs != watchIntervalDefaultMs {
		t.Fatalf("interval_ms=%d want=%d (default)", view.IntervalMs, watchIntervalDefaultMs)
	}

	// List shows exactly the one watch.
	w := rexRequest(t, r, http.MethodGet, "/api/v1/workspace/watches", nil, nil)
	var list struct {
		Items []watchView `json:"items"`
		Total int         `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("list total=%d items=%d want 1/1", list.Total, len(list.Items))
	}
	if list.Items[0].WatchID != view.WatchID {
		t.Fatalf("listed id=%q want=%q", list.Items[0].WatchID, view.WatchID)
	}

	// Delete by query id: gone afterwards.
	w = rexRequest(t, r, http.MethodDelete, "/api/v1/workspace/watch?id="+view.WatchID, nil, nil)
	rexData(t, w, http.StatusOK, true, nil)

	w = rexRequest(t, r, http.MethodGet, "/api/v1/workspace/watches", nil, nil)
	list.Items = nil
	list.Total = -1
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("after delete total=%d items=%d want 0/0", list.Total, len(list.Items))
	}

	// Second delete of the same id: 404.
	w = rexRequest(t, r, http.MethodDelete, "/api/v1/workspace/watch?id="+view.WatchID, nil, nil)
	rexData(t, w, http.StatusNotFound, false, nil)
}

func TestWorkspaceWatchIdempotentSameRoot(t *testing.T) {
	r := watchTestRouter(t)
	dir := t.TempDir()

	code1, v1 := createWatch(t, r, dir, 0)
	if code1 != http.StatusCreated {
		t.Fatalf("first create status=%d want=201", code1)
	}
	// Second watch on the same resolved root returns the existing entry with 200.
	code2, v2 := createWatch(t, r, dir, 0)
	if code2 != http.StatusOK {
		t.Fatalf("duplicate create status=%d want=200", code2)
	}
	if v2.WatchID != v1.WatchID {
		t.Fatalf("duplicate watch_id=%q want=%q", v2.WatchID, v1.WatchID)
	}
	if v2.Topic != v1.Topic {
		t.Fatalf("duplicate topic=%q want=%q", v2.Topic, v1.Topic)
	}
	// Only one watch should exist.
	w := rexRequest(t, r, http.MethodGet, "/api/v1/workspace/watches", nil, nil)
	var list struct {
		Total int `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total != 1 {
		t.Fatalf("total=%d want 1 after idempotent create", list.Total)
	}
}

func TestWorkspaceWatchInvalidRoot(t *testing.T) {
	r := watchTestRouter(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	code, _ := createWatch(t, r, missing, 0)
	if code < 400 || code >= 500 {
		t.Fatalf("invalid root status=%d want 4xx", code)
	}
}

func TestWorkspaceWatchIntervalClamped(t *testing.T) {
	r := watchTestRouter(t)
	dir := t.TempDir()

	// 100ms is below the floor and must clamp up to the minimum.
	code, view := createWatch(t, r, dir, 100)
	if code != http.StatusCreated {
		t.Fatalf("create status=%d want=201", code)
	}
	if view.IntervalMs != watchIntervalMinMs {
		t.Fatalf("interval_ms=%d want=%d (clamped min)", view.IntervalMs, watchIntervalMinMs)
	}
}

func TestWorkspaceWatchCapEnforced(t *testing.T) {
	r := watchTestRouter(t)

	// Fill to the cap with distinct roots.
	for i := 0; i < maxActiveWatches; i++ {
		code, _ := createWatch(t, r, t.TempDir(), 0)
		if code != http.StatusCreated {
			t.Fatalf("create #%d status=%d want=201", i, code)
		}
	}
	// One more distinct root exceeds the cap.
	w := rexRequest(t, r, http.MethodPost, "/api/v1/workspace/watch",
		rexMustJSON(t, map[string]interface{}{}), map[string]string{"X-Codeflow-Workspace-Root": t.TempDir()})
	rexData(t, w, http.StatusConflict, false, nil)
}

func TestWorkspaceWatchDeleteViaJSONBody(t *testing.T) {
	r := watchTestRouter(t)
	dir := t.TempDir()

	code, view := createWatch(t, r, dir, 0)
	if code != http.StatusCreated {
		t.Fatalf("create status=%d want=201", code)
	}
	// Delete accepts the id in a JSON body as watch_id.
	body := rexMustJSON(t, map[string]string{"watch_id": view.WatchID})
	w := rexRequest(t, r, http.MethodDelete, "/api/v1/workspace/watch", body, nil)
	rexData(t, w, http.StatusOK, true, nil)

	w = rexRequest(t, r, http.MethodDelete, "/api/v1/workspace/watch", body, nil)
	rexData(t, w, http.StatusNotFound, false, nil)
}

func TestShutdownWorkspaceWatches(t *testing.T) {
	r := watchTestRouter(t)

	code1, _ := createWatch(t, r, t.TempDir(), 0)
	if code1 != http.StatusCreated {
		t.Fatalf("create 1 status=%d want=201", code1)
	}
	code2, _ := createWatch(t, r, t.TempDir(), 0)
	if code2 != http.StatusCreated {
		t.Fatalf("create 2 status=%d want=201", code2)
	}

	ShutdownWorkspaceWatches()

	// Registry must be empty after shutdown.
	w := rexRequest(t, r, http.MethodGet, "/api/v1/workspace/watches", nil, nil)
	var list struct {
		Total int `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &list)
	if list.Total != 0 {
		t.Fatalf("registry should be empty after shutdown, total=%d", list.Total)
	}

	// Double-shutdown is safe (idempotent).
	ShutdownWorkspaceWatches()
}
