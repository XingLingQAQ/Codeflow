// Package handlers - Isolation API tests
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codeflow/backend/internal/isolation"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func decodeIsolationResponseData[T any](t *testing.T, body []byte) T {
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

func decodeIsolationErrorResponse(t *testing.T, body []byte) Response {
	t.Helper()

	var envelope Response
	err := json.Unmarshal(body, &envelope)
	assert.NoError(t, err)
	assert.False(t, envelope.Success)
	return envelope
}

func mustCreateIsolationContainer(t *testing.T, router *gin.Engine, role string) isolation.ContextContainer {
	t.Helper()

	body, err := json.Marshal(CreateContainerRequest{Role: role})
	assert.NoError(t, err)

	req, err := http.NewRequest("POST", "/api/v1/isolation/containers", bytes.NewBuffer(body))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	return decodeIsolationResponseData[isolation.ContextContainer](t, w.Body.Bytes())
}

func setupIsolationTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Initialize isolation service
	rbac := isolation.NewRBACManager()
	svc := isolation.NewIsolationService(rbac)
	isolation.SetIsolationService(svc)

	// Setup routes
	v1 := router.Group("/api/v1")
	isolationGroup := v1.Group("/isolation")
	{
		isolationGroup.GET("/containers", GetContainers)
		isolationGroup.POST("/containers", CreateContainer)
		isolationGroup.GET("/containers/:id", GetContainer)
		isolationGroup.DELETE("/containers/:id", DeleteContainer)
		isolationGroup.PUT("/containers/:id/quota", SetContainerQuota)
		isolationGroup.POST("/access/check", CheckAccess)
		isolationGroup.POST("/io/validate", ValidateIO)
		isolationGroup.GET("/roles", GetRoles)
		isolationGroup.POST("/roles", RegisterRole)
		isolationGroup.GET("/roles/:name", GetRole)
		isolationGroup.GET("/roles/:name/permissions", GetRolePermissions)
		isolationGroup.POST("/roles/:name/check", CheckRolePermission)
	}

	return router
}

func TestGetContainers_Empty(t *testing.T) {
	router := setupIsolationTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/isolation/containers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "containers")
	assert.Contains(t, response, "count")
}

func TestCreateContainer(t *testing.T) {
	router := setupIsolationTestRouter()

	reqBody := CreateContainerRequest{
		Role: "main",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/isolation/containers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	response := decodeIsolationResponseData[isolation.ContextContainer](t, w.Body.Bytes())
	assert.NotEmpty(t, response.ID)
	assert.Equal(t, isolation.RoleMain, response.Role)
}

func TestCreateContainer_InvalidRole(t *testing.T) {
	router := setupIsolationTestRouter()

	reqBody := CreateContainerRequest{
		Role: "invalid_role",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/isolation/containers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	response := decodeIsolationErrorResponse(t, w.Body.Bytes())
	assert.Equal(t, "invalid role", response.Error)
}

func TestGetContainer(t *testing.T) {
	router := setupIsolationTestRouter()

	created := mustCreateIsolationContainer(t, router, "coder")

	req, _ := http.NewRequest("GET", "/api/v1/isolation/containers/"+created.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[isolation.ContextContainer](t, w.Body.Bytes())
	assert.Equal(t, created.ID, response.ID)
}

func TestGetContainer_NotFound(t *testing.T) {
	router := setupIsolationTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/isolation/containers/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	response := decodeIsolationErrorResponse(t, w.Body.Bytes())
	assert.Equal(t, "container not found", response.Error)
}

func TestDeleteContainer(t *testing.T) {
	router := setupIsolationTestRouter()

	created := mustCreateIsolationContainer(t, router, "reviewer")

	req, _ := http.NewRequest("DELETE", "/api/v1/isolation/containers/"+created.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "container destroyed", response["message"])

	getReq, _ := http.NewRequest("GET", "/api/v1/isolation/containers/"+created.ID, nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	assert.Equal(t, http.StatusNotFound, getW.Code)
}

func TestSetContainerQuota(t *testing.T) {
	router := setupIsolationTestRouter()

	created := mustCreateIsolationContainer(t, router, "main")

	quotaBody, _ := json.Marshal(SetQuotaRequest{
		MaxMemoryMB:   512,
		MaxFileSizeMB: 10,
		MaxFileCount:  100,
	})
	req, _ := http.NewRequest("PUT", "/api/v1/isolation/containers/"+created.ID+"/quota", bytes.NewBuffer(quotaBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "quota updated", response["message"])
}

func TestCheckAccess(t *testing.T) {
	router := setupIsolationTestRouter()

	created := mustCreateIsolationContainer(t, router, "main")

	checkBody, _ := json.Marshal(CheckAccessRequest{
		ContainerID:  created.ID,
		Resource:     "file",
		ResourcePath: "/src/main.go",
		Action:       "read",
	})
	req, _ := http.NewRequest("POST", "/api/v1/isolation/access/check", bytes.NewBuffer(checkBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "decision")
	assert.Contains(t, response, "latency_ms")
}

func TestValidateIO(t *testing.T) {
	router := setupIsolationTestRouter()

	created := mustCreateIsolationContainer(t, router, "coder")

	validateBody, _ := json.Marshal(ValidateIORequest{
		ContainerID: created.ID,
		Input:       "SELECT * FROM users",
		Direction:   "input",
	})
	req, _ := http.NewRequest("POST", "/api/v1/isolation/io/validate", bytes.NewBuffer(validateBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	_ = decodeIsolationResponseData[isolation.IOValidationResult](t, w.Body.Bytes())
}

func TestGetRoles(t *testing.T) {
	router := setupIsolationTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/isolation/roles", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "roles")
	assert.Contains(t, response, "count")
	assert.Greater(t, int(response["count"].(float64)), 0)
}

func TestGetRole(t *testing.T) {
	router := setupIsolationTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/isolation/roles/main", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[isolation.RoleDefinition](t, w.Body.Bytes())
	assert.Equal(t, isolation.RoleMain, response.Name)
}

func TestGetRole_NotFound(t *testing.T) {
	router := setupIsolationTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/isolation/roles/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	response := decodeIsolationErrorResponse(t, w.Body.Bytes())
	assert.Equal(t, "role not found", response.Error)
}

func TestRegisterRole(t *testing.T) {
	router := setupIsolationTestRouter()

	reqBody := RegisterRoleRequest{
		Name:        "custom_role",
		Description: "A custom test role",
		Permissions: []isolation.Permission{
			{Resource: isolation.ResourceFile, Level: isolation.PermissionRead},
		},
		MaxConcurrent: 5,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/isolation/roles", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "role registered", response["message"])
}

func TestGetRolePermissions(t *testing.T) {
	router := setupIsolationTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/isolation/roles/main/permissions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "main", response["role"])
	assert.Contains(t, response, "permissions")
	assert.Contains(t, response, "count")
}

func TestCheckRolePermission(t *testing.T) {
	router := setupIsolationTestRouter()

	reqBody := map[string]string{
		"resource": "file",
		"level":    "read",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/isolation/roles/main/check", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Equal(t, "main", response["role"])
	assert.Equal(t, "file", response["resource"])
	assert.Equal(t, "read", response["level"])
	assert.Contains(t, response, "has_permission")
}

func TestGetContainers_FilterByRole(t *testing.T) {
	router := setupIsolationTestRouter()

	for _, role := range []string{"main", "coder", "reviewer"} {
		_, _ = json.Marshal(CreateContainerRequest{Role: role})
		_ = mustCreateIsolationContainer(t, router, role)
	}

	req, _ := http.NewRequest("GET", "/api/v1/isolation/containers?role=main", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.GreaterOrEqual(t, int(response["count"].(float64)), 1)
}

func TestCheckAccess_Performance(t *testing.T) {
	router := setupIsolationTestRouter()

	created := mustCreateIsolationContainer(t, router, "main")

	checkBody, _ := json.Marshal(CheckAccessRequest{
		ContainerID:  created.ID,
		Resource:     "file",
		ResourcePath: "/src/main.go",
		Action:       "read",
	})
	req, _ := http.NewRequest("POST", "/api/v1/isolation/access/check", bytes.NewBuffer(checkBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	latency := response["latency_ms"].(float64)
	assert.Less(t, latency, 50.0, "Access check should complete in <50ms")
}
