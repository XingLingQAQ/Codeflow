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
		if strings.Contains(err.Error(), "required") {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondInternalError(c, "create skill", err)
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
		respondSkillWriteError(c, "update skill", err)
		return
	}
	respondOK(c, s)
}

// DeleteSkill handles DELETE /api/v1/skills/:id
func DeleteSkill(c *gin.Context) {
	if err := skill.GetRegistry().Delete(c.Request.Context(), c.Param("id")); err != nil {
		respondSkillWriteError(c, "delete skill", err)
		return
	}
	respondOK(c, gin.H{"deleted": true})
}

// respondSkillWriteError maps a registry write failure to an HTTP status:
// builtin protection is a 409 conflict and a missing skill/version is a 404;
// anything else is a durable-store/transaction failure surfaced by
// UpdateWithHistory or the store delete/put path — a 5xx server error, never
// a client 4xx (input validation happens at binding time, and the registry's
// only client-side validation message carries "required").
func respondSkillWriteError(c *gin.Context, context string, err error) {
	if strings.Contains(err.Error(), "builtin") {
		respondError(c, http.StatusConflict, err.Error())
		return
	}
	if strings.Contains(err.Error(), "not found") {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}
	respondInternalError(c, context, err)
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

// ExportSkills handles GET /api/v1/skills/export — markdown dump of all skills.
func ExportSkills(c *gin.Context) {
	items, err := skill.GetRegistry().List(c.Request.Context())
	if err != nil {
		respondInternalError(c, "export skills", err)
		return
	}
	var b strings.Builder
	for _, s := range items {
		b.WriteString("---\n")
		b.WriteString("name: " + s.Name + "\n")
		b.WriteString("version: " + s.Version + "\n")
		if s.Description != "" {
			b.WriteString("description: " + s.Description + "\n")
		}
		if len(s.Triggers) > 0 {
			b.WriteString("triggers: [" + strings.Join(s.Triggers, ", ") + "]\n")
		}
		if len(s.StageTags) > 0 {
			b.WriteString("stage_tags: [" + strings.Join(s.StageTags, ", ") + "]\n")
		}
		b.WriteString("---\n")
		b.WriteString(s.Body)
		b.WriteString("\n\n")
	}
	respondOK(c, gin.H{"markdown": b.String(), "total": len(items)})
}
