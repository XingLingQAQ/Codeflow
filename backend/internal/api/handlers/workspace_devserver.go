// Package handlers - Workspace dev-server management API (experimental).
package handlers

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/workspace"
)

// devServerMgr is the process-wide dev-server manager, lazy-initialized on
// first use. Mirrors the watchRegistry singleton pattern.
var (
	devServerMgr     *workspace.DevServerManager
	devServerMgrOnce sync.Once
)

func getDevServerMgr() *workspace.DevServerManager {
	devServerMgrOnce.Do(func() {
		devServerMgr = workspace.NewDevServerManager(workspace.GetService())
	})
	return devServerMgr
}

// ShutdownWorkspaceDevServers stops all running dev-servers. Wire as defer in
// main.go next to ShutdownWorkspaceWatches.
func ShutdownWorkspaceDevServers() {
	if devServerMgr != nil {
		devServerMgr.Shutdown()
	}
}

type startDevServerBody struct {
	Root   string `json:"root"`
	Script string `json:"script" binding:"required"`
}

// DetectWorkspaceScripts handles GET /api/v1/workspace/scripts
func DetectWorkspaceScripts(c *gin.Context) {
	if !workspace.HasService() {
		respondError(c, http.StatusServiceUnavailable, "workspace service not available")
		return
	}
	root := workspaceRootFromRequest(c, "")
	if root == "" {
		respondError(c, http.StatusBadRequest, "root is required (query root= or header X-Codeflow-Workspace-Root)")
		return
	}
	scripts, err := workspace.DetectScripts(workspace.GetService(), root)
	if err != nil {
		if strings.Contains(err.Error(), "escapes") || strings.Contains(err.Error(), "not a directory") || strings.Contains(err.Error(), "not exist") {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondInternalError(c, "detect scripts", err)
		return
	}
	if scripts == nil {
		scripts = []workspace.ScriptInfo{}
	}
	respondOK(c, gin.H{"items": scripts, "total": len(scripts)})
}

// StartWorkspaceDevServer handles POST /api/v1/workspace/dev-servers
func StartWorkspaceDevServer(c *gin.Context) {
	if !workspace.HasService() {
		respondError(c, http.StatusServiceUnavailable, "workspace service not available")
		return
	}
	var body startDevServerBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	root := workspaceRootFromRequest(c, body.Root)
	if root == "" {
		respondError(c, http.StatusBadRequest, "root is required")
		return
	}
	mgr := getDevServerMgr()
	handle, err := mgr.Start(root, body.Script)
	if err != nil {
		if strings.Contains(err.Error(), "limit reached") {
			respondError(c, http.StatusConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "escapes") || strings.Contains(err.Error(), "not a directory") {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondInternalError(c, "start dev-server", err)
		return
	}
	respondCreated(c, handle)
}

// ListWorkspaceDevServers handles GET /api/v1/workspace/dev-servers
func ListWorkspaceDevServers(c *gin.Context) {
	if !workspace.HasService() {
		respondError(c, http.StatusServiceUnavailable, "workspace service not available")
		return
	}
	items := getDevServerMgr().List()
	respondOK(c, gin.H{"items": items, "total": len(items)})
}

type deleteDevServerBody struct {
	ID string `json:"id"`
}

// DeleteWorkspaceDevServer handles DELETE /api/v1/workspace/dev-servers?id=
func DeleteWorkspaceDevServer(c *gin.Context) {
	if !workspace.HasService() {
		respondError(c, http.StatusServiceUnavailable, "workspace service not available")
		return
	}
	id := strings.TrimSpace(c.Query("id"))
	if id == "" && c.Request.ContentLength > 0 {
		var body deleteDevServerBody
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		id = strings.TrimSpace(body.ID)
	}
	if id == "" {
		respondError(c, http.StatusBadRequest, "id is required (query id= or JSON id)")
		return
	}
	if err := getDevServerMgr().Stop(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondInternalError(c, "stop dev-server", err)
		return
	}
	respondOK(c, gin.H{"id": id, "stopped": true})
}

// GetWorkspaceDevServerLogs handles GET /api/v1/workspace/dev-servers/:id/logs?tail=
func GetWorkspaceDevServerLogs(c *gin.Context) {
	if !workspace.HasService() {
		respondError(c, http.StatusServiceUnavailable, "workspace service not available")
		return
	}
	id := c.Param("id")
	tail, ok := parseNonNegativeQueryInt(c, "tail", 0)
	if !ok {
		return
	}
	lines, err := getDevServerMgr().Logs(id, tail)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondInternalError(c, "dev-server logs", err)
		return
	}
	respondOK(c, gin.H{"id": id, "lines": lines, "count": len(lines)})
}
