// Package handlers - Skill version history API (experimental).
package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/skill"
)

type rollbackSkillBody struct {
	VersionRowID int64 `json:"version_row_id" binding:"required"`
}

// ListSkillVersions handles GET /api/v1/skills/:id/versions
func ListSkillVersions(c *gin.Context) {
	id := c.Param("id")
	versions, err := skill.GetRegistry().ListVersions(c.Request.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondInternalError(c, "list skill versions", err)
		return
	}
	respondOK(c, gin.H{"items": versions, "total": len(versions)})
}

// RollbackSkillVersion handles POST /api/v1/skills/:id/rollback
func RollbackSkillVersion(c *gin.Context) {
	id := c.Param("id")
	var body rollbackSkillBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	s, err := skill.GetRegistry().RollbackVersion(c.Request.Context(), id, body.VersionRowID)
	if err != nil {
		respondSkillWriteError(c, "rollback skill version", err)
		return
	}
	respondOK(c, s)
}
