// Package handlers - Snapshot API handlers
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/snapshot"
)

const snapshotWarning = "Snapshot module is experimental: capture functions return placeholder data, restore functions are no-op. See P0-001 for implementation status."

// experimentalSnapshotResponse adds experimental metadata without wrapping the original payload.
func experimentalSnapshotResponse(c *gin.Context, status int, data interface{}) {
	body := gin.H{}
	raw, err := json.Marshal(data)
	if err == nil {
		_ = json.Unmarshal(raw, &body)
	}
	if len(body) == 0 {
		body["payload"] = data
	}

	body["warning"] = snapshotWarning
	body["status"] = "experimental"
	c.JSON(status, body)
}

// CreateSnapshot creates a new snapshot.
// POST /api/v1/snapshots
func CreateSnapshot(c *gin.Context) {
	var req snapshot.SnapshotCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	svc := snapshot.GetSnapshotService()
	snap, err := svc.Create(c.Request.Context(), &req)
	if err != nil {
		respondInternalError(c, "create snapshot", err)
		return
	}

	experimentalSnapshotResponse(c, http.StatusCreated, snap)
}

// GetSnapshots returns a paginated list of snapshots.
// GET /api/v1/snapshots
func GetSnapshots(c *gin.Context) {
	opts := &snapshot.SnapshotListOptions{}

	// Parse query parameters
	if sessionID := c.Query("session_id"); sessionID != "" {
		opts.SessionID = sessionID
	}

	if startTimeStr := c.Query("start_time"); startTimeStr != "" {
		if startTime, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			opts.StartTime = &startTime
		}
	}

	if endTimeStr := c.Query("end_time"); endTimeStr != "" {
		if endTime, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			opts.EndTime = &endTime
		}
	}

	if tags := c.QueryArray("tags"); len(tags) > 0 {
		opts.Tags = tags
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			opts.Limit = limit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			opts.Offset = offset
		}
	}

	svc := snapshot.GetSnapshotService()
	resp, err := svc.List(c.Request.Context(), opts)
	if err != nil {
		respondInternalError(c, "list snapshots", err)
		return
	}

	experimentalSnapshotResponse(c, http.StatusOK, resp)
}

// GetSnapshot retrieves a snapshot by ID.
// GET /api/v1/snapshots/:id
func GetSnapshot(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "snapshot ID")
	if !ok {
		return
	}

	svc := snapshot.GetSnapshotService()
	snap, err := svc.Get(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusNotFound, "Snapshot not found")
		return
	}

	experimentalSnapshotResponse(c, http.StatusOK, snap)
}

// RestoreSnapshot restores the system state from a snapshot.
// POST /api/v1/snapshots/:id/restore
func RestoreSnapshot(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "snapshot ID")
	if !ok {
		return
	}

	startTime := time.Now()
	svc := snapshot.GetSnapshotService()
	result, err := svc.Restore(c.Request.Context(), id)
	elapsed := time.Since(startTime)

	if err != nil {
		respondInternalError(c, "restore snapshot", err)
		return
	}

	// Verify restore time < 2 seconds
	if elapsed > 2*time.Second {
		result.Errors = append(result.Errors, "restore operation exceeded 2 second threshold")
	}

	experimentalSnapshotResponse(c, http.StatusOK, result)
}

// DeleteSnapshot deletes a snapshot by ID.
// DELETE /api/v1/snapshots/:id
func DeleteSnapshot(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "snapshot ID")
	if !ok {
		return
	}

	svc := snapshot.GetSnapshotService()
	if err := svc.Delete(c.Request.Context(), id); err != nil {
		respondError(c, http.StatusNotFound, "Snapshot not found")
		return
	}

	experimentalSnapshotResponse(c, http.StatusOK, gin.H{"message": "snapshot deleted successfully"})
}
