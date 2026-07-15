package handlers

import (
	"net/http"

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
	reg, ok := skill.GetRegistry().(*skill.InMemoryRegistry)
	if !ok || reg == nil {
		respondError(c, http.StatusServiceUnavailable, "skill registry does not support import")
		return
	}
	n, err := reg.ImportMarkdownDir(c.Request.Context(), body.Dir)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, gin.H{"imported": n, "dir": body.Dir})
}
