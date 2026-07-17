package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/skill"
)

type importSkillsBody struct {
	Dir string `json:"dir" binding:"required"`
}

// ImportSkills handles POST /api/v1/skills/import
func ImportSkills(c *gin.Context) {
	var body importSkillsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	dir, err := sanitizeSkillsImportDir(body.Dir)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	n, err := skill.GetRegistry().ImportMarkdownDir(c.Request.Context(), dir)
	if err != nil {
		if strings.Contains(err.Error(), "builtin") {
			respondError(c, http.StatusConflict, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, gin.H{"imported": n, "dir": dir})
}

// sanitizeSkillsImportDir restricts import to directories under the process working
// directory or an explicit CODEFLOW_SKILLS_DIR allow-root.
func sanitizeSkillsImportDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("dir is required")
	}
	abs, err := filepath.Abs(dir)
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
		return "", fmt.Errorf("import path is not a directory")
	}
	allowRoots := make([]string, 0, 2)
	if env := strings.TrimSpace(os.Getenv("CODEFLOW_SKILLS_DIR")); env != "" {
		if a, err := filepath.Abs(env); err == nil {
			if r, err := filepath.EvalSymlinks(a); err == nil {
				a = r
			}
			allowRoots = append(allowRoots, filepath.Clean(a))
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if r, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = r
		}
		allowRoots = append(allowRoots, filepath.Clean(cwd))
	}
	for _, root := range allowRoots {
		if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("import dir must be under process cwd or CODEFLOW_SKILLS_DIR")
}
