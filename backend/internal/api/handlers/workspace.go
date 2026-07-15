// Package handlers - Workspace filesystem API (experimental).
package handlers

import (
	"encoding/base64"
	"net/http"
	"os"
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

// ReadWorkspaceFile handles GET /api/v1/workspace/read?root=&path=
func ReadWorkspaceFile(c *gin.Context) {
	root := workspaceRootFromRequest(c, "")
	path := c.Query("path")
	if root == "" || path == "" {
		respondError(c, http.StatusBadRequest, "root and path are required")
		return
	}
	fc, err := workspace.GetService().Read(c.Request.Context(), &workspace.ReadRequest{Root: root, Path: path})
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
	ent, err := workspace.GetService().Write(c.Request.Context(), &workspace.WriteRequest{
		Root:          root,
		Path:          body.Path,
		Content:       content,
		CreateParents: body.CreateParents,
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
