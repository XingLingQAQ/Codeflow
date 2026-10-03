package handlers

// T1.10.d handler-level coverage: with no allow-list configured the workspace
// file routes answer 403 (before this step the text-matching fallthrough
// produced 500), the body carries the repair hint, an allow-listed root inside
// the list still succeeds, and a root outside a configured list is 403 rather
// than the old 500.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
	"github.com/codeflow/backend/internal/workspace"
)

// fcWorkspaceRouter installs svc as the process workspace service and returns a
// router with the representative file routes. Cleanup restores the previous
// service (or the unset state).
func fcWorkspaceRouter(t *testing.T, svc workspace.Service) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hadSvc := workspace.HasService()
	var prev workspace.Service
	if hadSvc {
		prev = workspace.GetService()
	}
	workspace.SetService(svc)
	t.Cleanup(func() {
		if hadSvc {
			workspace.SetService(prev)
		} else {
			workspace.SetService(nil)
		}
	})

	r := gin.New()
	r.GET("/api/v1/workspace/list", ListWorkspace)
	r.GET("/api/v1/workspace/read", ReadWorkspaceFile)
	r.POST("/api/v1/workspace/write", WriteWorkspaceFile)
	r.GET("/api/v1/workspace/stat", StatWorkspaceFile)
	r.GET("/api/v1/workspace/staged", ListWorkspaceStaged)
	r.POST("/api/v1/workspace/promote", PromoteWorkspaceFile)
	r.POST("/api/v1/workspace/promote-all", PromoteAllWorkspace)
	r.POST("/api/v1/workspace/discard", DiscardWorkspaceStaged)
	r.POST("/api/v1/workspace/discard-all", DiscardAllWorkspaceStaged)
	r.GET("/api/v1/workspace/scripts", DetectWorkspaceScripts)
	r.POST("/api/v1/workspace/dev-servers", StartWorkspaceDevServer)
	return r
}

// fcWorkspaceWatchRouter is watchTestRouter with a caller-supplied workspace
// service, so the watch route can be exercised behind a fail-closed service.
func fcWorkspaceWatchRouter(t *testing.T, svc workspace.Service) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hadSvc := workspace.HasService()
	var prev workspace.Service
	if hadSvc {
		prev = workspace.GetService()
	}
	workspace.SetService(svc)
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
	return r
}

func TestWorkspaceRoutesUnconfiguredReturn403(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	root := t.TempDir()
	// A fully unconfigured service: no allow-list, switch off.
	r := fcWorkspaceRouter(t, workspace.NewFSService(nil))
	hdr := map[string]string{"X-Codeflow-Workspace-Root": root}
	writeBody := rexMustJSON(t, map[string]interface{}{"path": "planted.txt", "content_text": "x"})

	cases := []struct {
		name   string
		method string
		target string
		body   []byte
	}{
		{"list", http.MethodGet, "/api/v1/workspace/list", nil},
		{"read", http.MethodGet, "/api/v1/workspace/read?path=x.txt", nil},
		{"write", http.MethodPost, "/api/v1/workspace/write", writeBody},
		{"stat", http.MethodGet, "/api/v1/workspace/stat?path=x.txt", nil},
		{"staged", http.MethodGet, "/api/v1/workspace/staged", nil},
		{"promote", http.MethodPost, "/api/v1/workspace/promote", rexMustJSON(t, map[string]string{"path": "x.txt"})},
		{"promote-all", http.MethodPost, "/api/v1/workspace/promote-all", nil},
		{"discard", http.MethodPost, "/api/v1/workspace/discard", rexMustJSON(t, map[string]string{"path": "x.txt"})},
		{"discard-all", http.MethodPost, "/api/v1/workspace/discard-all", nil},
		{"scripts", http.MethodGet, "/api/v1/workspace/scripts", nil},
		{"dev-servers", http.MethodPost, "/api/v1/workspace/dev-servers", rexMustJSON(t, map[string]string{"script": "dev"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := rexRequest(t, r, tc.method, tc.target, tc.body, hdr)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s status=%d want=403 body=%s", tc.name, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "CODEFLOW_WORKSPACE_ROOTS") {
				t.Fatalf("%s body lacks the repair hint: %s", tc.name, w.Body.String())
			}
		})
	}

	// Nothing was written to the (refused) root.
	w := rexRequest(t, r, http.MethodGet, "/api/v1/workspace/stat?path=planted.txt", nil, hdr)
	if w.Code != http.StatusForbidden {
		t.Fatalf("stat after refused write status=%d want=403 body=%s", w.Code, w.Body.String())
	}
}

func TestWorkspaceRoutesConfiguredOutsideReturn403(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	allowed := t.TempDir()
	outside := t.TempDir()
	svc := workspace.NewFSService(nil)
	svc.SetAllowedRoots([]string{allowed})
	r := fcWorkspaceRouter(t, svc)
	hdr := map[string]string{"X-Codeflow-Workspace-Root": outside}

	w := rexRequest(t, r, http.MethodGet, "/api/v1/workspace/list", nil, hdr)
	if w.Code != http.StatusForbidden {
		t.Fatalf("list outside allow-list status=%d want=403 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "CODEFLOW_WORKSPACE_ROOTS") {
		t.Fatalf("body lacks the repair hint: %s", w.Body.String())
	}
	w = rexRequest(t, r, http.MethodPost, "/api/v1/workspace/write", rexMustJSON(t, map[string]interface{}{"path": "x.txt", "content_text": "x"}), hdr)
	if w.Code != http.StatusForbidden {
		t.Fatalf("write outside allow-list status=%d want=403 body=%s", w.Code, w.Body.String())
	}

	// The allow-listed root still serves 200 for the same routes.
	insideHdr := map[string]string{"X-Codeflow-Workspace-Root": allowed}
	w = rexRequest(t, r, http.MethodGet, "/api/v1/workspace/list", nil, insideHdr)
	if w.Code != http.StatusOK {
		t.Fatalf("list inside allow-list status=%d want=200 body=%s", w.Code, w.Body.String())
	}
	w = rexRequest(t, r, http.MethodPost, "/api/v1/workspace/write", rexMustJSON(t, map[string]interface{}{"path": "ok.txt", "content_text": "ok"}), insideHdr)
	if w.Code != http.StatusOK {
		t.Fatalf("write inside allow-list status=%d want=200 body=%s", w.Code, w.Body.String())
	}
}

func TestWorkspaceRoutesSwitchAllowsUnconfiguredRoot(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)
	root := t.TempDir()
	svc := workspace.NewFSService(nil)
	svc.SetAllowUnrestrictedRoots(true)
	r := fcWorkspaceRouter(t, svc)
	hdr := map[string]string{"X-Codeflow-Workspace-Root": root}

	w := rexRequest(t, r, http.MethodGet, "/api/v1/workspace/list", nil, hdr)
	if w.Code != http.StatusOK {
		t.Fatalf("list with the migration switch on status=%d want=200 body=%s", w.Code, w.Body.String())
	}
	w = rexRequest(t, r, http.MethodPost, "/api/v1/workspace/write", rexMustJSON(t, map[string]interface{}{"path": "ok.txt", "content_text": "ok"}), hdr)
	if w.Code != http.StatusOK {
		t.Fatalf("write with the migration switch on status=%d want=200 body=%s", w.Code, w.Body.String())
	}
}

// The watch route resolves through the same service, so an unconfigured root is
// refused (4xx, with the repair hint) rather than 500.
func TestWorkspaceWatchUnconfiguredRootRefused(t *testing.T) {
	r := fcWorkspaceWatchRouter(t, workspace.NewFSService(nil))
	root := t.TempDir()
	w := rexRequest(t, r, http.MethodPost, "/api/v1/workspace/watch",
		rexMustJSON(t, map[string]interface{}{}), map[string]string{"X-Codeflow-Workspace-Root": root})
	if w.Code != http.StatusForbidden {
		t.Fatalf("watch on an unconfigured root status=%d want=403 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "CODEFLOW_WORKSPACE_ROOTS") {
		t.Fatalf("watch body lacks the repair hint: %s", w.Body.String())
	}
}
