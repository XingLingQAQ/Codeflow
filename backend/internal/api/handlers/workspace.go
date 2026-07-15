// Package handlers - Workspace filesystem API (experimental).
package handlers

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/workspace"
)

// workspaceRootFromRequest resolves the absolute root.
// Prefer header X-Codeflow-Workspace-Root; fallback JSON/query "root".
func workspaceRootFromRequest(c *gin.Context, bodyRoot string) string {
	if h := strings.TrimSpace(c.GetHeader("X-Codeflow-Workspace-Root")); h != "" {
		return h
	}
	if bodyRoot != "" {
		return bodyRoot
	}
	return strings.TrimSpace(c.Query("root"))
}

// ListWorkspace handles GET /api/v1/workspace/list?root=&path=
func ListWorkspace(c *gin.Context) {
	root := workspaceRootFromRequest(c, "")
	path := c.Query("path")
	if root == "" {
		respondError(c, http.StatusBadRequest, "root is required (query root= or header X-Codeflow-Workspace-Root)")
		return
	}
	entries, err := workspace.GetService().List(c.Request.Context(), &workspace.ListRequest{Root: root, Path: path})
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not exist") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "escapes") || strings.Contains(err.Error(), "relative") {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondInternalError(c, "list workspace", err)
		return
	}
	respondOK(c, gin.H{"items": entries, "total": len(entries)})
}

// ReadWorkspaceFile handles GET /api/v1/workspace/read?root=&path=&staged=
func ReadWorkspaceFile(c *gin.Context) {
	root := workspaceRootFromRequest(c, "")
	path := c.Query("path")
	if root == "" || path == "" {
		respondError(c, http.StatusBadRequest, "root and path are required")
		return
	}
	readRoot := root
	if c.Query("staged") == "true" || c.Query("staged") == "1" {
		readRoot = filepath.Join(root, ".codeflow", "staging")
	}
	fc, err := workspace.GetService().Read(c.Request.Context(), &workspace.ReadRequest{Root: readRoot, Path: path})
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not exist") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "escapes") || strings.Contains(err.Error(), "directory") {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondInternalError(c, "read workspace", err)
		return
	}
	// Return content as base64 to stay JSON-safe for binary files.
	respondOK(c, gin.H{
		"path":     fc.Path,
		"size":     fc.Size,
		"mod_time": fc.ModTime,
		"content_base64": base64.StdEncoding.EncodeToString(fc.Content),
		"content_text":   string(fc.Content), // convenience for text
	})
}

type writeWorkspaceBody struct {
	Root          string `json:"root"`
	Path          string `json:"path" binding:"required"`
	ContentText   string `json:"content_text"`
	ContentBase64 string `json:"content_base64"`
	CreateParents bool   `json:"create_parents"`
	Mode          string `json:"mode"` // direct | stage
}

type promoteWorkspaceBody struct {
	Root string `json:"root"`
	Path string `json:"path" binding:"required"`
}

// WriteWorkspaceFile handles POST /api/v1/workspace/write
func WriteWorkspaceFile(c *gin.Context) {
	var body writeWorkspaceBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	root := workspaceRootFromRequest(c, body.Root)
	if root == "" {
		respondError(c, http.StatusBadRequest, "root is required")
		return
	}
	var content []byte
	switch {
	case body.ContentBase64 != "":
		b, err := base64.StdEncoding.DecodeString(body.ContentBase64)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid content_base64")
			return
		}
		content = b
	default:
		content = []byte(body.ContentText)
	}
	mode := workspace.WriteMode(body.Mode)
	if mode == "" {
		mode = workspace.WriteModeDirect
	}
	ent, err := workspace.GetService().Write(c.Request.Context(), &workspace.WriteRequest{
		Root:          root,
		Path:          body.Path,
		Content:       content,
		CreateParents: body.CreateParents,
		Mode:          mode,
	})
	if err != nil {
		if strings.Contains(err.Error(), "blocked by guard") {
			respondError(c, http.StatusForbidden, err.Error())
			return
		}
		if strings.Contains(err.Error(), "escapes") || strings.Contains(err.Error(), "relative") {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondInternalError(c, "write workspace", err)
		return
	}
	respondOK(c, ent)
}


// StatWorkspaceFile handles GET /api/v1/workspace/stat?root=&path=
func StatWorkspaceFile(c *gin.Context) {
	root := workspaceRootFromRequest(c, "")
	path := c.Query("path")
	if root == "" || path == "" {
		respondError(c, http.StatusBadRequest, "root and path are required")
		return
	}
	ent, err := workspace.GetService().Stat(c.Request.Context(), root, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) || strings.Contains(err.Error(), "not exist") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "escapes") || strings.Contains(err.Error(), "relative") {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondInternalError(c, "stat workspace", err)
		return
	}
	respondOK(c, ent)
}

// ListWorkspaceStaged handles GET /api/v1/workspace/staged?root=
func ListWorkspaceStaged(c *gin.Context) {
	root := workspaceRootFromRequest(c, "")
	if root == "" {
		respondError(c, http.StatusBadRequest, "root is required (query root= or header X-Codeflow-Workspace-Root)")
		return
	}
	entries, err := workspace.GetService().ListStaged(c.Request.Context(), root)
	if err != nil {
		respondInternalError(c, "list staged workspace", err)
		return
	}
	respondOK(c, gin.H{"items": entries, "total": len(entries)})
}

// PromoteWorkspaceFile handles POST /api/v1/workspace/promote
func PromoteWorkspaceFile(c *gin.Context) {
	var body promoteWorkspaceBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	root := workspaceRootFromRequest(c, body.Root)
	if root == "" {
		respondError(c, http.StatusBadRequest, "root is required")
		return
	}
	ent, err := workspace.GetService().Promote(c.Request.Context(), root, body.Path)
	if err != nil {
		if strings.Contains(err.Error(), "blocked by guard") {
			respondError(c, http.StatusForbidden, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, ent)
}

// DiscardWorkspaceStaged handles POST /api/v1/workspace/discard
func DiscardWorkspaceStaged(c *gin.Context) {
	var body promoteWorkspaceBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	root := workspaceRootFromRequest(c, body.Root)
	if root == "" {
		respondError(c, http.StatusBadRequest, "root is required")
		return
	}
	if err := workspace.GetService().DiscardStaged(c.Request.Context(), root, body.Path); err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not exist") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, gin.H{"discarded": true, "path": body.Path})
}
