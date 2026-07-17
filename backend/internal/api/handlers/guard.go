// Package handlers - Guard policy API (experimental).
package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

// GuardExempt handles POST /api/v1/guard/exempt — temporary path exemption.
func GuardExempt(c *gin.Context) {
	var body struct {
		Path       string   `json:"path" binding:"required"`
		Rules      []string `json:"rules"`
		Reason     string   `json:"reason"`
		TTLSeconds int      `json:"ttl_seconds"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	svc, ok := guard.GetService().(*guard.Engine)
	if !ok || svc == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	ttl := body.TTLSeconds
	if ttl <= 0 {
		ttl = 3600
	}
	rules := make([]guard.RuleID, 0, len(body.Rules))
	for _, r := range body.Rules {
		rules = append(rules, guard.RuleID(r))
	}
	if err := svc.GrantExemption(guard.Exemption{
		Path:      body.Path,
		Rules:     rules,
		Reason:    body.Reason,
		ExpiresAt: time.Now().UTC().Add(time.Duration(ttl) * time.Second),
	}); err != nil {
		respondInternalError(c, "grant exemption", err)
		return
	}
	respondOK(c, gin.H{"path": body.Path, "ttl_seconds": ttl, "rules": body.Rules})
}

// GuardListExemptions handles GET /api/v1/guard/exemptions
func GuardListExemptions(c *gin.Context) {
	svc, ok := guard.GetService().(*guard.Engine)
	if !ok || svc == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	items := svc.ListExemptions()
	if items == nil {
		items = []guard.Exemption{}
	}
	respondOK(c, gin.H{"items": items, "total": len(items)})
}

// GuardClearExemption handles DELETE /api/v1/guard/exempt?path=
func GuardClearExemption(c *gin.Context) {
	path := strings.TrimSpace(c.Query("path"))
	if path == "" {
		respondError(c, http.StatusBadRequest, "path is required (query path=)")
		return
	}
	svc, ok := guard.GetService().(*guard.Engine)
	if !ok || svc == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	if err := svc.ClearExemption(path); err != nil {
		respondInternalError(c, "clear exemption", err)
		return
	}
	respondOK(c, gin.H{"cleared": true, "path": path})
}

// GuardIndexTree handles POST /api/v1/guard/index — seed AST symbol index from a tree.
func GuardIndexTree(c *gin.Context) {
	var body guardIndexBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	root, err := sanitizeIndexRoot(body.Root)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	svc, ok := guard.GetService().(*guard.Engine)
	if !ok || svc == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	n, err := svc.IndexTree(c.Request.Context(), root)
	if err != nil {
		respondInternalError(c, "index tree", err)
		return
	}
	respondOK(c, gin.H{"indexed_files": n, "root": root})
}

// GuardConfig handles GET /api/v1/guard/config — active policy snapshot.
func GuardConfig(c *gin.Context) {
	svc, ok := guard.GetService().(*guard.Engine)
	if !ok || svc == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	respondOK(c, svc.Config())
}

// GuardRules handles GET /api/v1/guard/rules — list known rule ids with active severity.
func GuardRules(c *gin.Context) {
	svc, ok := guard.GetService().(*guard.Engine)
	if !ok || svc == nil {
		respondError(c, http.StatusServiceUnavailable, "guard engine not available")
		return
	}
	cfg := svc.Config()
	type row struct {
		ID       guard.RuleID   `json:"id"`
		Severity guard.Severity `json:"severity"`
	}
	out := make([]row, 0, len(cfg.Rules))
	for id, rc := range cfg.Rules {
		out = append(out, row{ID: id, Severity: rc.Severity})
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i].ID) < string(out[j].ID) })
	respondOK(c, gin.H{"items": out, "total": len(out), "denied_path_globs": cfg.DeniedPathGlobs, "max_file_bytes": cfg.MaxFileBytes})
}

// sanitizeIndexRoot restricts IndexTree to cwd or CODEFLOW_INDEX_ROOT.
func sanitizeIndexRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root is not a directory")
	}
	allow := make([]string, 0, 2)
	if env := strings.TrimSpace(os.Getenv("CODEFLOW_INDEX_ROOT")); env != "" {
		if a, err := filepath.Abs(env); err == nil {
			if r, err := filepath.EvalSymlinks(a); err == nil {
				a = r
			}
			allow = append(allow, filepath.Clean(a))
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if r, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = r
		}
		allow = append(allow, filepath.Clean(cwd))
	}
	for _, a := range allow {
		if abs == a || strings.HasPrefix(abs, a+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("index root must be under process cwd or CODEFLOW_INDEX_ROOT")
}
