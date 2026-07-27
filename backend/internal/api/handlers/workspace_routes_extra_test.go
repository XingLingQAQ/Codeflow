package handlers

// HTTP handler-level coverage for the workspace staged-write lifecycle:
// a stage-mode write is visible via read?staged=true and via /staged, a plain
// read 404s until promote, and after promote the plain read returns the content
// and staging is emptied. Uses a fresh FSService over a temp root (no guard).

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/workspace"
)

func rexWorkspaceRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	prev := workspace.GetService()
	workspace.SetService(workspace.NewFSService(nil))
	t.Cleanup(func() { workspace.SetService(prev) })

	r := gin.New()
	r.GET("/api/v1/workspace/read", ReadWorkspaceFile)
	r.POST("/api/v1/workspace/write", WriteWorkspaceFile)
	r.GET("/api/v1/workspace/staged", ListWorkspaceStaged)
	r.POST("/api/v1/workspace/promote", PromoteWorkspaceFile)
	return r, t.TempDir()
}

func TestWorkspaceStagedReadThenPromoteRoutesExtra(t *testing.T) {
	r, root := rexWorkspaceRouter(t)
	hdr := map[string]string{"X-Codeflow-Workspace-Root": root}
	const rel = "src/app.go"
	const contents = "package app\n"

	// Stage-mode write lands under .codeflow/staging, not the real tree.
	writeBody := rexMustJSON(t, map[string]interface{}{
		"path":         rel,
		"content_text": contents,
		"mode":         "stage",
	})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/workspace/write", writeBody, hdr)
	rexData(t, w, http.StatusOK, true, nil)

	// staged=true read returns the staged content.
	w = rexRequest(t, r, http.MethodGet, "/api/v1/workspace/read?path="+rel+"&staged=true", nil, hdr)
	var readOut struct {
		Path        string `json:"path"`
		ContentText string `json:"content_text"`
	}
	rexData(t, w, http.StatusOK, true, &readOut)
	if readOut.ContentText != contents {
		t.Fatalf("staged content=%q want=%q", readOut.ContentText, contents)
	}

	// Plain read of the same rel 404s: nothing promoted to the real tree yet.
	w = rexRequest(t, r, http.MethodGet, "/api/v1/workspace/read?path="+rel, nil, hdr)
	if w.Code != http.StatusNotFound {
		t.Fatalf("plain read before promote status=%d want=404 body=%s", w.Code, w.Body.String())
	}

	// /staged lists the pending file by its project-relative path.
	w = rexRequest(t, r, http.MethodGet, "/api/v1/workspace/staged", nil, hdr)
	var stagedList struct {
		Items []workspace.Entry `json:"items"`
		Total int               `json:"total"`
	}
	rexData(t, w, http.StatusOK, true, &stagedList)
	if stagedList.Total != 1 || len(stagedList.Items) != 1 || stagedList.Items[0].Path != rel {
		t.Fatalf("staged list=%+v want single %q", stagedList, rel)
	}

	// Promote moves it into the real tree.
	promoteBody := rexMustJSON(t, map[string]string{"path": rel})
	w = rexRequest(t, r, http.MethodPost, "/api/v1/workspace/promote", promoteBody, hdr)
	rexData(t, w, http.StatusOK, true, nil)

	// Plain read now succeeds with the promoted content.
	w = rexRequest(t, r, http.MethodGet, "/api/v1/workspace/read?path="+rel, nil, hdr)
	readOut = struct {
		Path        string `json:"path"`
		ContentText string `json:"content_text"`
	}{}
	rexData(t, w, http.StatusOK, true, &readOut)
	if readOut.ContentText != contents {
		t.Fatalf("promoted content=%q want=%q", readOut.ContentText, contents)
	}

	// Staging is empty after promote.
	w = rexRequest(t, r, http.MethodGet, "/api/v1/workspace/staged", nil, hdr)
	stagedList.Items, stagedList.Total = nil, 0
	rexData(t, w, http.StatusOK, true, &stagedList)
	if stagedList.Total != 0 {
		t.Fatalf("staged after promote total=%d want=0", stagedList.Total)
	}
}
