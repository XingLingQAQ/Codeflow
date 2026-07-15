// Package handlers - Guard policy API (experimental).
package handlers

import (
	"encoding/base64"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/guard"
)

type guardCheckBody struct {
	Path          string `json:"path" binding:"required"`
	ContentText   string `json:"content_text"`
	ContentBase64 string `json:"content_base64"`
	// AbsPath optional override; if empty path is treated as absolute-ish for checks.
	AbsPath string `json:"abs_path"`
	Root    string `json:"root"`
}

// GuardCheck handles POST /api/v1/guard/check — dry-run Evaluate without writing.
func GuardCheck(c *gin.Context) {
	var body guardCheckBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	var content []byte
	if body.ContentBase64 != "" {
		b, err := base64.StdEncoding.DecodeString(body.ContentBase64)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid content_base64")
			return
		}
		content = b
	} else {
		content = []byte(body.ContentText)
	}
	abs := strings.TrimSpace(body.AbsPath)
	if abs == "" {
		if body.Root != "" {
			abs = filepath.Join(body.Root, filepath.FromSlash(body.Path))
		} else {
			abs = body.Path
		}
	}
	svc, ok := guard.GetService().(*guard.Engine)
	if !ok || svc == nil {
		// fall back via Evaluate if interface extended — only Engine has Evaluate
		respondError(c, http.StatusServiceUnavailable, "guard engine not available for dry-run")
		return
	}
	dec := svc.Evaluate(c.Request.Context(), abs, content)
	respondOK(c, dec)
}

type guardIndexBody struct {
	Root string `json:"root" binding:"required"`
}

// GuardIndexTree handles POST /api/v1/guard/index — seed AST symbol index from a tree.
func GuardIndexTree(c *gin.Context) {
	var body guardIndexBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	svc, ok := guard.GetService().(*guard.Engine)
	if !ok || svc == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	n, err := svc.IndexTree(c.Request.Context(), body.Root)
	if err != nil {
		respondInternalError(c, "index tree", err)
		return
	}
	respondOK(c, gin.H{"indexed_files": n, "root": body.Root})
}
