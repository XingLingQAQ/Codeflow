// Package handlers - Snapshot API tests
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/codeflow/backend/internal/snapshot"
)

// experimentalFields represents experimental metadata appended to snapshot responses.
type experimentalFields struct {
	Warning string `json:"warning"`
	Status  string `json:"status"`
}

func setupSnapshotRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	{
		snapshots := v1.Group("/snapshots")
		snapshots.Use(middleware.Experimental("snapshot"))
		{
			snapshots.POST("", CreateSnapshot)
			snapshots.GET("", GetSnapshots)
			snapshots.GET("/:id", GetSnapshot)
			snapshots.POST("/:id/restore", RestoreSnapshot)
			snapshots.DELETE("/:id", DeleteSnapshot)
		}
	}

	return router
}

// mustCreateSnapshot creates one snapshot and stops the test immediately when
// the service cannot capture state (the default state provider needs process
// globals these tests do not install).
//
// It must stay a require, not an assert: the discarded error used to let a nil
// snapshot and an empty item list reach the assertions below, where `created.ID`
// and `resp.Items[0]` panicked. A panic kills the whole test binary, so every
// test declared after this file never ran — 45 of the package's 215 test
// functions, including the summarize/compress handler tests. Failing here keeps
// these tests red (the underlying global-service gap is owned by S1/S3) while
// leaving the rest of the package executable.
func mustCreateSnapshot(t *testing.T, svc *snapshot.InMemorySnapshotService, req *snapshot.SnapshotCreateRequest) *snapshot.Snapshot {
	t.Helper()
	created, err := svc.Create(nil, req)
	require.NoError(t, err, "snapshot create failed")
	require.NotNil(t, created, "snapshot create returned no snapshot")
	return created
}

func assertExperimentalResponse(t *testing.T, w *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	assert.Equal(t, "experimental", w.Header().Get("X-Feature-Status"))
	assert.Contains(t, w.Header().Get("X-Feature-Warning"), "snapshot functionality is experimental")

	var fields experimentalFields
	err := json.Unmarshal(w.Body.Bytes(), &fields)
	assert.NoError(t, err)
	assert.Equal(t, "experimental", fields.Status)
	assert.Contains(t, fields.Warning, "experimental")
	if target != nil {
		err = json.Unmarshal(w.Body.Bytes(), target)
		assert.NoError(t, err)
	}
}

func TestCreateSnapshotAPI(t *testing.T) {
	router := setupSnapshotRouter()
	snapshot.SetSnapshotService(snapshot.NewInMemorySnapshotService())

	reqBody := map[string]interface{}{
		"description": "Test snapshot",
		"session_id":  "session-123",
		"tags":        []string{"test"},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/snapshots", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp snapshot.Snapshot
	assertExperimentalResponse(t, w, &resp)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "Test snapshot", resp.Description)
	assert.Equal(t, "session-123", resp.SessionID)
}

func TestGetSnapshotsAPI(t *testing.T) {
	router := setupSnapshotRouter()
	svc := snapshot.NewInMemorySnapshotService()
	snapshot.SetSnapshotService(svc)

	// Create test snapshots
	for i := 0; i < 3; i++ {
		mustCreateSnapshot(t, svc, &snapshot.SnapshotCreateRequest{
			Description: "Test snapshot",
			SessionID:   "session-123",
		})
	}

	req, _ := http.NewRequest("GET", "/api/v1/snapshots?limit=10", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp snapshot.SnapshotListResponse
	assertExperimentalResponse(t, w, &resp)
	assert.Equal(t, 3, resp.Total)
	assert.Len(t, resp.Items, 3)
}

func TestGetSnapshotsWithFiltersAPI(t *testing.T) {
	router := setupSnapshotRouter()
	svc := snapshot.NewInMemorySnapshotService()
	snapshot.SetSnapshotService(svc)

	// Create test snapshots with different session IDs
	mustCreateSnapshot(t, svc, &snapshot.SnapshotCreateRequest{
		Description: "Session 1 snapshot",
		SessionID:   "session-1",
	})
	mustCreateSnapshot(t, svc, &snapshot.SnapshotCreateRequest{
		Description: "Session 2 snapshot",
		SessionID:   "session-2",
	})

	// Filter by session ID
	req, _ := http.NewRequest("GET", "/api/v1/snapshots?session_id=session-1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp snapshot.SnapshotListResponse
	assertExperimentalResponse(t, w, &resp)
	assert.Equal(t, 1, resp.Total)
	require.Len(t, resp.Items, 1)
	assert.Equal(t, "session-1", resp.Items[0].SessionID)
}

func TestGetSnapshotAPI(t *testing.T) {
	router := setupSnapshotRouter()
	svc := snapshot.NewInMemorySnapshotService()
	snapshot.SetSnapshotService(svc)

	// Create a test snapshot
	created := mustCreateSnapshot(t, svc, &snapshot.SnapshotCreateRequest{
		Description: "Test snapshot",
	})

	// Get the snapshot
	httpReq, _ := http.NewRequest("GET", "/api/v1/snapshots/"+created.ID, nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp snapshot.Snapshot
	assertExperimentalResponse(t, w, &resp)
	assert.Equal(t, created.ID, resp.ID)
}

func TestRestoreSnapshotAPI(t *testing.T) {
	router := setupSnapshotRouter()
	svc := snapshot.NewInMemorySnapshotService()
	snapshot.SetSnapshotService(svc)

	// Create a test snapshot
	created := mustCreateSnapshot(t, svc, &snapshot.SnapshotCreateRequest{
		Description: "Test snapshot",
	})

	// Restore the snapshot
	startTime := time.Now()
	httpReq, _ := http.NewRequest("POST", "/api/v1/snapshots/"+created.ID+"/restore", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)
	elapsed := time.Since(startTime)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp snapshot.RestoreResult
	assertExperimentalResponse(t, w, &resp)
	assert.Equal(t, created.ID, resp.SnapshotID)
	assert.True(t, resp.GitRestored)
	assert.True(t, resp.ConversationRestored)
	assert.True(t, resp.VectorRestored)
	assert.True(t, resp.MemoryGraphRestored)

	// Verify restore time < 2 seconds
	assert.Less(t, elapsed, 2*time.Second, "Restore operation should complete in less than 2 seconds")
}

func TestDeleteSnapshotAPI(t *testing.T) {
	router := setupSnapshotRouter()
	svc := snapshot.NewInMemorySnapshotService()
	snapshot.SetSnapshotService(svc)

	// Create a test snapshot
	created := mustCreateSnapshot(t, svc, &snapshot.SnapshotCreateRequest{
		Description: "Test snapshot",
	})

	// Delete the snapshot
	httpReq, _ := http.NewRequest("DELETE", "/api/v1/snapshots/"+created.ID, nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify deletion
	httpReq, _ = http.NewRequest("GET", "/api/v1/snapshots/"+created.ID, nil)
	w = httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetSnapshotsPaginationAPI(t *testing.T) {
	router := setupSnapshotRouter()
	svc := snapshot.NewInMemorySnapshotService()
	snapshot.SetSnapshotService(svc)

	// Create 5 test snapshots
	for i := 0; i < 5; i++ {
		mustCreateSnapshot(t, svc, &snapshot.SnapshotCreateRequest{
			Description: "Test snapshot",
		})
	}

	// Get first page
	req, _ := http.NewRequest("GET", "/api/v1/snapshots?limit=2&offset=0", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp snapshot.SnapshotListResponse
	assertExperimentalResponse(t, w, &resp)
	assert.Equal(t, 5, resp.Total)
	assert.Len(t, resp.Items, 2)
	assert.True(t, resp.HasMore)
	assert.Equal(t, 2, resp.NextOffset)
}
