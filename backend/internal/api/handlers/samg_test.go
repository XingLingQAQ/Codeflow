// Package handlers - SAMG API tests
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/samg"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupSAMGTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Initialize SAMG service
	svc := samg.NewSAMGService(nil)
	samg.SetSAMGService(svc)

	// Setup routes
	v1 := router.Group("/api/v1")
	samgGroup := v1.Group("/samg")
	{
		samgGroup.GET("/triples", GetTriples)
		samgGroup.POST("/triples", AddTriples)
		samgGroup.GET("/triples/:id", GetTriple)
		samgGroup.DELETE("/triples", DeleteTriples)
		samgGroup.GET("/triples/:id/relations", GetRelations)
		samgGroup.POST("/extract", ExtractTriples)
		samgGroup.POST("/activate", Activate)
		samgGroup.POST("/paths", FindPaths)
		samgGroup.GET("/activation", GetActivationConfig)
		samgGroup.PUT("/activation", UpdateActivationConfig)
		samgGroup.GET("/decay", GetDecayConfig)
		samgGroup.PUT("/decay", UpdateDecayConfig)
		samgGroup.POST("/decay/apply", ApplyDecay)
		samgGroup.GET("/nodes/visible", GetVisibleNodes)
		samgGroup.GET("/nodes/hidden", GetHiddenNodes)
		samgGroup.GET("/nodes/top", GetTopNodes)
		samgGroup.POST("/nodes/:id/access", RecordAccess)
		samgGroup.GET("/graph/export", ExportGraph)
		samgGroup.POST("/graph/import", ImportGraph)
		samgGroup.GET("/stats", GetSAMGStats)
	}

	return router
}

func decodeSAMGResponseData[T any](t *testing.T, body []byte) T {
	t.Helper()

	var envelope Response
	err := json.Unmarshal(body, &envelope)
	assert.NoError(t, err)
	assert.True(t, envelope.Success)

	raw, err := json.Marshal(envelope.Data)
	assert.NoError(t, err)

	var data T
	err = json.Unmarshal(raw, &data)
	assert.NoError(t, err)
	return data
}

func TestGetTriples_Empty(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/samg/triples", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "triples")
	assert.Contains(t, response, "count")
}

func TestAddTriples(t *testing.T) {
	router := setupSAMGTestRouter()

	triples := []samg.Triple{
		{
			ID:         "test-triple-1",
			Subject:    samg.CreateNode("entity:a", samg.EntityTypes.Class, "ClassA"),
			Predicate:  samg.Predicates.Extends,
			Object:     samg.CreateNodeObject(samg.CreateNode("entity:b", samg.EntityTypes.Class, "ClassB")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}

	reqBody := AddTriplesRequest{Triples: triples}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/samg/triples", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "triples added", response["message"])
	assert.Equal(t, float64(1), response["count"])
}

func TestGetTriple(t *testing.T) {
	router := setupSAMGTestRouter()

	// First add a triple
	triples := []samg.Triple{
		{
			ID:         "test-get-triple",
			Subject:    samg.CreateNode("entity:x", samg.EntityTypes.Function, "FuncX"),
			Predicate:  samg.Predicates.Calls,
			Object:     samg.CreateNodeObject(samg.CreateNode("entity:y", samg.EntityTypes.Function, "FuncY")),
			Confidence: 0.8,
			Timestamp:  time.Now().UnixMilli(),
		},
	}
	reqBody := AddTriplesRequest{Triples: triples}
	body, _ := json.Marshal(reqBody)
	addReq, _ := http.NewRequest("POST", "/api/v1/samg/triples", bytes.NewBuffer(body))
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)

	// Then get it
	req, _ := http.NewRequest("GET", "/api/v1/samg/triples/test-get-triple", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[samg.Triple](t, w.Body.Bytes())
	assert.Equal(t, "test-get-triple", response.ID)
}

func TestGetTriple_NotFound(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/samg/triples/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestExtractTriples(t *testing.T) {
	router := setupSAMGTestRouter()

	reqBody := ExtractRequest{
		Content:   "class UserService extends BaseService implements IUserService",
		SessionID: "test-session",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/samg/extract", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "triples")
	assert.Greater(t, int(response["count"].(float64)), 0)
}

func TestActivate(t *testing.T) {
	router := setupSAMGTestRouter()

	// First add some triples
	triples := []samg.Triple{
		{
			ID:         "activate-t1",
			Subject:    samg.CreateNode("entity:act-a", samg.EntityTypes.Class, "A"),
			Predicate:  samg.Predicates.Extends,
			Object:     samg.CreateNodeObject(samg.CreateNode("entity:act-b", samg.EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}
	addBody, _ := json.Marshal(AddTriplesRequest{Triples: triples})
	addReq, _ := http.NewRequest("POST", "/api/v1/samg/triples", bytes.NewBuffer(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)

	// Then activate
	reqBody := ActivateRequest{SourceIDs: []string{"entity:act-a"}}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/samg/activate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[samg.ActivationResult](t, w.Body.Bytes())
	assert.Equal(t, []string{"entity:act-a"}, response.SourceNodes)
}

func TestFindPaths(t *testing.T) {
	router := setupSAMGTestRouter()

	// Add triples forming a path
	triples := []samg.Triple{
		{
			ID:         "path-t1",
			Subject:    samg.CreateNode("entity:path-a", samg.EntityTypes.Class, "A"),
			Predicate:  samg.Predicates.Calls,
			Object:     samg.CreateNodeObject(samg.CreateNode("entity:path-b", samg.EntityTypes.Function, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "path-t2",
			Subject:    samg.CreateNode("entity:path-b", samg.EntityTypes.Function, "B"),
			Predicate:  samg.Predicates.Calls,
			Object:     samg.CreateNodeObject(samg.CreateNode("entity:path-c", samg.EntityTypes.Function, "C")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}
	addBody, _ := json.Marshal(AddTriplesRequest{Triples: triples})
	addReq, _ := http.NewRequest("POST", "/api/v1/samg/triples", bytes.NewBuffer(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)

	// Find paths
	reqBody := FindPathsRequest{
		SourceID: "entity:path-a",
		TargetID: "entity:path-c",
		MaxHops:  3,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/samg/paths", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "paths")
}

func TestGetDecayConfig(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/samg/decay", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[samg.DecayConfig](t, w.Body.Bytes())
	assert.Greater(t, response.DecayRate, 0.0)
}

func TestUpdateDecayConfig(t *testing.T) {
	router := setupSAMGTestRouter()

	reqBody := DecayConfigRequest{
		DecayRate:       0.7,
		BaseActivation:  2.0,
		MinActivation:   0.05,
		HideThreshold:   0.2,
		BoostOnAccess:   0.5,
		TimeUnit:        7200000,
		EnableAutoDecay: false,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("PUT", "/api/v1/samg/decay", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "decay config updated", response["message"])
}

func TestApplyDecay(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("POST", "/api/v1/samg/decay/apply", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "decayed_nodes")
	assert.Contains(t, response, "hidden_nodes")
}

func TestGetVisibleNodes(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/samg/nodes/visible", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "nodes")
	assert.Contains(t, response, "count")
}

func TestGetHiddenNodes(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/samg/nodes/hidden", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "nodes")
}

func TestGetTopNodes(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/samg/nodes/top?n=5", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "nodes")
}

func TestRecordAccess(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("POST", "/api/v1/samg/nodes/test-node/access", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "access recorded", response["message"])
	assert.Equal(t, "test-node", response["node_id"])
}

func TestGetActivationConfig(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/samg/activation", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[samg.ActivationConfig](t, w.Body.Bytes())
	assert.Greater(t, response.MaxHops, 0)
}

func TestUpdateActivationConfig(t *testing.T) {
	router := setupSAMGTestRouter()

	reqBody := ActivationConfigRequest{
		InitialActivation: 2.0,
		DecayFactor:       0.6,
		FiringThreshold:   0.15,
		MaxHops:           5,
		MaxActivatedNodes: 200,
		SpreadingFactor:   0.75,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("PUT", "/api/v1/samg/activation", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "activation config updated", response["message"])
}

func TestExportGraph(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/samg/graph/export", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[samg.JsonLdGraph](t, w.Body.Bytes())
	assert.NotEmpty(t, response.ID)
}

func TestImportGraph(t *testing.T) {
	router := setupSAMGTestRouter()

	graph := samg.JsonLdGraph{
		Context: samg.JsonLdContext{
			Vocab: "https://codeflow.ai/vocab/",
			Base:  "https://codeflow.ai/graph/",
		},
		ID:   "test-graph",
		Type: "Graph",
		Graph: []samg.Triple{
			{
				ID:         "import-t1",
				Subject:    samg.CreateNode("entity:import-a", samg.EntityTypes.Class, "A"),
				Predicate:  samg.Predicates.Extends,
				Object:     samg.CreateNodeObject(samg.CreateNode("entity:import-b", samg.EntityTypes.Class, "B")),
				Confidence: 0.9,
				Timestamp:  time.Now().UnixMilli(),
			},
		},
	}
	body, _ := json.Marshal(graph)

	req, _ := http.NewRequest("POST", "/api/v1/samg/graph/import", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "graph imported", response["message"])
	assert.Equal(t, float64(1), response["triple_count"])
}

func TestGetSAMGStats(t *testing.T) {
	router := setupSAMGTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/samg/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[samg.SAMGStats](t, w.Body.Bytes())
	assert.NotNil(t, response.GraphStats)
	assert.NotNil(t, response.DecayStats)
}

func TestGetRelations(t *testing.T) {
	router := setupSAMGTestRouter()

	// Add triples
	triples := []samg.Triple{
		{
			ID:         "rel-t1",
			Subject:    samg.CreateNode("entity:rel-a", samg.EntityTypes.Class, "A"),
			Predicate:  samg.Predicates.Calls,
			Object:     samg.CreateNodeObject(samg.CreateNode("entity:rel-b", samg.EntityTypes.Function, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}
	addBody, _ := json.Marshal(AddTriplesRequest{Triples: triples})
	addReq, _ := http.NewRequest("POST", "/api/v1/samg/triples", bytes.NewBuffer(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)

	// Get relations
	req, _ := http.NewRequest("GET", "/api/v1/samg/triples/entity:rel-a/relations", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "relations")
	assert.Contains(t, response, "node_id")
}

func TestDeleteTriples(t *testing.T) {
	router := setupSAMGTestRouter()

	// Add triples
	triples := []samg.Triple{
		{
			ID:         "delete-t1",
			Subject:    samg.CreateNode("entity:del-a", samg.EntityTypes.Class, "A"),
			Predicate:  samg.Predicates.Extends,
			Object:     samg.CreateNodeObject(samg.CreateNode("entity:del-b", samg.EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}
	addBody, _ := json.Marshal(AddTriplesRequest{Triples: triples})
	addReq, _ := http.NewRequest("POST", "/api/v1/samg/triples", bytes.NewBuffer(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)

	// Delete
	deleteBody, _ := json.Marshal(map[string][]string{"ids": {"delete-t1"}})
	req, _ := http.NewRequest("DELETE", "/api/v1/samg/triples", bytes.NewBuffer(deleteBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeSAMGResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "triples deleted", response["message"])
}
