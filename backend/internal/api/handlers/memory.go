// Package handlers - Memory API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/memory"
)

// GetMemoryItems handles GET /api/v1/memory/items
func GetMemoryItems(c *gin.Context) {
	var opts memory.MemoryListOptions
	if err := c.ShouldBindQuery(&opts); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid query parameters: "+err.Error())
		return
	}

	svc := memory.GetMemoryService()
	result, err := svc.List(c.Request.Context(), &opts)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to list memory items: "+err.Error())
		return
	}

	respondOK(c, result)
}

// CreateMemoryItem handles POST /api/v1/memory/items
func CreateMemoryItem(c *gin.Context) {
	var req memory.MemoryItemCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := memory.GetMemoryService()
	item, err := svc.Create(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create memory item: "+err.Error())
		return
	}

	respondCreated(c, item)
}

// UpdateMemoryItem handles PATCH /api/v1/memory/items/:id
func UpdateMemoryItem(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing item ID")
		return
	}

	var req memory.MemoryItemUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := memory.GetMemoryService()
	item, err := svc.Update(c.Request.Context(), id, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to update memory item: "+err.Error())
		return
	}
	if item == nil {
		respondError(c, http.StatusNotFound, "Memory item not found")
		return
	}

	respondOK(c, item)
}

// DeleteMemoryItem handles DELETE /api/v1/memory/items/:id
func DeleteMemoryItem(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing item ID")
		return
	}

	svc := memory.GetMemoryService()

	// Check if item exists
	item, err := svc.Get(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get memory item: "+err.Error())
		return
	}
	if item == nil {
		respondError(c, http.StatusNotFound, "Memory item not found")
		return
	}

	if err := svc.Delete(c.Request.Context(), id); err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to delete memory item: "+err.Error())
		return
	}

	respondOK(c, gin.H{"deleted": true, "id": id})
}

// ArchiveMemoryItem handles POST /api/v1/memory/items/:id/archive
func ArchiveMemoryItem(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing item ID")
		return
	}

	svc := memory.GetMemoryService()
	item, err := svc.Archive(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to archive memory item: "+err.Error())
		return
	}
	if item == nil {
		respondError(c, http.StatusNotFound, "Memory item not found")
		return
	}

	respondOK(c, item)
}

// RestoreMemoryItem handles POST /api/v1/memory/items/:id/restore
func RestoreMemoryItem(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing item ID")
		return
	}

	svc := memory.GetMemoryService()
	item, err := svc.Restore(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to restore memory item: "+err.Error())
		return
	}
	if item == nil {
		respondError(c, http.StatusNotFound, "Memory item not found")
		return
	}

	respondOK(c, item)
}
