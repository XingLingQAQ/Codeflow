// Package handlers - Skill registry API (experimental).
package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/skill"
)

// CreateSkill handles POST /api/v1/skills
func CreateSkill(c *gin.Context) {
	var req skill.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	s, err := skill.GetRegistry().Create(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondCreated(c, s)
}

// ListSkills handles GET /api/v1/skills?stage=&enabled=
func ListSkills(c *gin.Context) {
	stage := c.Query("stage")
	includeDisabled := true
	if c.Query("enabled") == "true" {
		// enabled=true → only enabled skills
		includeDisabled = false
	}
	items, err := skill.GetRegistry().ListFiltered(c.Request.Context(), stage, includeDisabled)
	if err != nil {
		respondInternalError(c, "list skills", err)
		return
	}
	respondOK(c, gin.H{"items": items, "total": len(items)})
}

// GetSkill handles GET /api/v1/skills/:id
func GetSkill(c *gin.Context) {
	s, err := skill.GetRegistry().Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}
	respondOK(c, s)
}

// UpdateSkill handles PATCH /api/v1/skills/:id
func UpdateSkill(c *gin.Context) {
	var req skill.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	s, err := skill.GetRegistry().Update(c.Request.Context(), c.Param("id"), &req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, s)
}

// DeleteSkill handles DELETE /api/v1/skills/:id
func DeleteSkill(c *gin.Context) {
	if err := skill.GetRegistry().Delete(c.Request.Context(), c.Param("id")); err != nil {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}
	respondOK(c, gin.H{"deleted": true})
}

// MatchSkills handles POST /api/v1/skills/match
func MatchSkills(c *gin.Context) {
	var req skill.MatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	items, err := skill.GetRegistry().Match(c.Request.Context(), &req)
	if err != nil {
		respondInternalError(c, "match skills", err)
		return
	}
	respondOK(c, gin.H{"items": items, "total": len(items)})
}

// InjectSkills handles POST /api/v1/skills/inject — returns prompt markdown block.
func InjectSkills(c *gin.Context) {
	var req skill.MatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	text, err := skill.GetRegistry().RenderInjection(c.Request.Context(), &req)
	if err != nil {
		respondInternalError(c, "inject skills", err)
		return
	}
	respondOK(c, gin.H{"injection": text})
}
