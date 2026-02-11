package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/memory"
)

// StoreRawArchive handles POST /api/v1/memory/archive
func StoreRawArchive(c *gin.Context) {
	var req struct {
		Type      string                 `json:"type"`
		Content   string                 `json:"content" binding:"required"`
		Metadata  map[string]interface{} `json:"metadata,omitempty"`
		SessionID string                 `json:"session_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	archive, err := memory.GetRawArchive()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to init raw archive: "+err.Error())
		return
	}

	entry := memory.RawEntry{
		Type:      memory.RawEntryType(req.Type),
		Content:   req.Content,
		Metadata:  req.Metadata,
		SessionID: req.SessionID,
	}

	id, err := archive.Store(c.Request.Context(), entry)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to store: "+err.Error())
		return
	}

	respondCreated(c, gin.H{"id": id})
}

// GetRawArchiveEntry handles GET /api/v1/memory/archive/:id
func GetRawArchiveEntry(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing entry ID")
		return
	}

	archive, err := memory.GetRawArchive()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to init raw archive: "+err.Error())
		return
	}

	entry, err := archive.Get(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get entry: "+err.Error())
		return
	}
	if entry == nil {
		respondError(c, http.StatusNotFound, "Entry not found")
		return
	}

	respondOK(c, entry)
}

// SearchRawArchive handles GET /api/v1/memory/archive/search?q=xxx&limit=20
func SearchRawArchive(c *gin.Context) {
	q := c.Query("q")
	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 20
	}

	archive, err := memory.GetRawArchive()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to init raw archive: "+err.Error())
		return
	}

	entries, err := archive.Search(c.Request.Context(), q, limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to search: "+err.Error())
		return
	}

	respondOK(c, gin.H{"entries": entries, "count": len(entries)})
}

// ListRawArchive handles GET /api/v1/memory/archive
func ListRawArchive(c *gin.Context) {
	opts := &memory.RawArchiveSearchOptions{}

	if t := c.Query("type"); t != "" {
		opts.Type = memory.RawEntryType(t)
	}
	if sid := c.Query("session_id"); sid != "" {
		opts.SessionID = sid
	}
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			opts.Limit = v
		}
	}
	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			opts.Offset = v
		}
	}

	archive, err := memory.GetRawArchive()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to init raw archive: "+err.Error())
		return
	}

	entries, err := archive.List(c.Request.Context(), opts)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to list: "+err.Error())
		return
	}

	count, _ := archive.Count(c.Request.Context())

	respondOK(c, gin.H{"entries": entries, "count": len(entries), "total": count})
}

// GetRawArchiveStats handles GET /api/v1/memory/archive/stats
func GetRawArchiveStats(c *gin.Context) {
	archive, err := memory.GetRawArchive()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to init raw archive: "+err.Error())
		return
	}

	count, err := archive.Count(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to count: "+err.Error())
		return
	}

	respondOK(c, gin.H{"total_entries": count})
}
