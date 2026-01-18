// Package handlers - Blackboard API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/blackboard"
)

// GetBlackboardEntries handles GET /api/v1/blackboard/entries
func GetBlackboardEntries(c *gin.Context) {
	var req blackboard.EntryListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid query parameters: "+err.Error())
		return
	}

	svc := blackboard.GetBlackboard()
	result, err := svc.ListEntries(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to list entries: "+err.Error())
		return
	}

	respondOK(c, result)
}

// CreateBlackboardEntry handles POST /api/v1/blackboard/entries
func CreateBlackboardEntry(c *gin.Context) {
	var req blackboard.EntryCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := blackboard.GetBlackboard()
	result, err := svc.CreateEntry(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create entry: "+err.Error())
		return
	}

	respondCreated(c, result)
}

// UpdateBlackboardEntry handles PATCH /api/v1/blackboard/entries/:id
func UpdateBlackboardEntry(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing entry ID")
		return
	}

	var req blackboard.EntryUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := blackboard.GetBlackboard()
	result, err := svc.UpdateEntry(c.Request.Context(), id, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to update entry: "+err.Error())
		return
	}

	respondOK(c, result)
}

// DeleteBlackboardEntry handles DELETE /api/v1/blackboard/entries/:id
func DeleteBlackboardEntry(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing entry ID")
		return
	}

	svc := blackboard.GetBlackboard()
	if err := svc.DeleteEntry(c.Request.Context(), id); err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to delete entry: "+err.Error())
		return
	}

	respondOK(c, gin.H{"deleted": true, "id": id})
}

// CreateVote handles POST /api/v1/votes
func CreateVote(c *gin.Context) {
	var req blackboard.VoteCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := blackboard.GetBlackboard()
	result, err := svc.CreateVote(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create vote: "+err.Error())
		return
	}

	respondCreated(c, result)
}

// GetVote handles GET /api/v1/votes/:id
func GetVote(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing vote ID")
		return
	}

	svc := blackboard.GetBlackboard()
	result, err := svc.GetVote(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get vote: "+err.Error())
		return
	}
	if result == nil {
		respondError(c, http.StatusNotFound, "Vote not found")
		return
	}

	respondOK(c, result)
}

// CastVote handles POST /api/v1/votes/:id/cast
func CastVote(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing vote ID")
		return
	}

	var req blackboard.VoteCastRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := blackboard.GetBlackboard()
	result, err := svc.CastVote(c.Request.Context(), id, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to cast vote: "+err.Error())
		return
	}

	respondOK(c, result)
}
