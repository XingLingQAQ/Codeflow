// Package api - Debate route tests.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codeflow/backend/internal/debate"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDebateRouteTest(t *testing.T) (*gin.Engine, *debate.Debate) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	manager := debate.NewInMemoryDebateManager()
	_ = debate.GetDebateManager()
	debate.SetDebateManager(manager)

	created, err := manager.CreateDebate(context.Background(), &debate.DebateCreateRequest{
		Title:        "Route coverage debate",
		Description:  "Verify solution route registration",
		GeneratorID:  "generator-1",
		CriticID:     "critic-1",
		InitialInput: "Find the best implementation.",
	})
	require.NoError(t, err)

	server := NewServer(&Config{EnableDebugMode: true})
	return server.Router(), created
}

func TestProposeSolutionRoute(t *testing.T) {
	router, created := setupDebateRouteTest(t)

	body, err := json.Marshal(debate.ProposeSolutionRequest{
		ProposedBy:  "agent-1",
		Role:        debate.RoleGenerator,
		Title:       "Use the registered route",
		Description: "Submit a solution through POST /api/v1/debates/:id/solutions.",
		Pros:        []string{"route is reachable"},
		Cons:        []string{"none"},
	})
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/debates/"+created.ID+"/solutions", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp struct {
		Success bool            `json:"success"`
		Data    debate.Solution `json:"data"`
		Error   string          `json:"error,omitempty"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Empty(t, resp.Error)
	assert.Equal(t, created.ID, resp.Data.DebateID)
	assert.Equal(t, "Use the registered route", resp.Data.Title)
	assert.Equal(t, debate.RoleGenerator, resp.Data.Role)
}

func TestProposeSolutionRouteRejectsInvalidRequest(t *testing.T) {
	router, created := setupDebateRouteTest(t)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/debates/"+created.ID+"/solutions", bytes.NewReader([]byte(`{"proposed_by":"agent-1"}`)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "Invalid request body")
}
