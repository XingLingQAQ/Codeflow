// Package handlers - Isolation API tests
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/codeflow/backend/internal/audit"
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

func setupIsolationTestRouter(t *testing.T) (*gin.Engine, *audit.MemoryStorage) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Trace())

	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))

	rbac := isolation.NewRBACManager()
	svc := isolation.NewIsolationService(rbac)
	isolation.SetIsolationService(svc)

	t.Cleanup(func() {
		cleanupIsolationTestState()
	})

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

	return router, storage
}

func cleanupIsolationTestState() {
	audit.SetAuditService(nil)
	isolation.SetIsolationService(nil)
}

func lastIsolationAuditEntry(t *testing.T, storage *audit.MemoryStorage) *audit.AuditLogEntry {
	t.Helper()

	entry, err := storage.GetLastEntry(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, audit.EventIsolation, entry.EventType)
	return entry
}

func assertIsolationTraceHeaders(t *testing.T, w *httptest.ResponseRecorder, requestID, sessionID, taskID, agentID string) {
	t.Helper()
	assert.Equal(t, requestID, w.Header().Get(middleware.HeaderRequestID))
	assert.Equal(t, sessionID, w.Header().Get(middleware.HeaderSessionID))
	assert.Equal(t, taskID, w.Header().Get(middleware.HeaderTaskID))
	assert.Equal(t, agentID, w.Header().Get(middleware.HeaderAgentID))
}

func setIsolationTraceHeaders(req *http.Request, requestID, sessionID, taskID, agentID string) {
	req.Header.Set(middleware.HeaderRequestID, requestID)
	req.Header.Set(middleware.HeaderSessionID, sessionID)
	req.Header.Set(middleware.HeaderTaskID, taskID)
	req.Header.Set(middleware.HeaderAgentID, agentID)
}

func assertIsolationAuditTrace(t *testing.T, entry *audit.AuditLogEntry, requestID, sessionID, taskID, agentID, method, path string) {
	t.Helper()
	if assert.NotNil(t, entry.Trace) {
		assert.Equal(t, requestID, entry.Trace.RequestID)
		assert.Equal(t, sessionID, entry.Trace.SessionID)
		assert.Equal(t, taskID, entry.Trace.TaskID)
		assert.Equal(t, agentID, entry.Trace.AgentID)
		assert.Equal(t, method, entry.Trace.Method)
		assert.Equal(t, path, entry.Trace.Path)
	}
}

func newIsolationTraceValues() (string, string, string, string) {
	return "req-isolation-001", "session-isolation-001", "task-isolation-001", "agent-isolation-001"
}

func extractIsolationDecision(t *testing.T, response map[string]interface{}) map[string]interface{} {
	t.Helper()

	decision, ok := response["decision"].(map[string]interface{})
	assert.True(t, ok)
	return decision
}

func TestGetContainers_Empty(t *testing.T) {
	router, _ := setupIsolationTestRouter(t)

	req, _ := http.NewRequest("GET", "/api/v1/isolation/containers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "containers")
	assert.Contains(t, response, "count")
}

func TestCreateContainer(t *testing.T) {
	router, _ := setupIsolationTestRouter(t)

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
	router, _ := setupIsolationTestRouter(t)

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
	router, _ := setupIsolationTestRouter(t)

	created := mustCreateIsolationContainer(t, router, "coder")

	req, _ := http.NewRequest("GET", "/api/v1/isolation/containers/"+created.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[isolation.ContextContainer](t, w.Body.Bytes())
	assert.Equal(t, created.ID, response.ID)
}

func TestGetContainer_NotFound(t *testing.T) {
	router, _ := setupIsolationTestRouter(t)

	req, _ := http.NewRequest("GET", "/api/v1/isolation/containers/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	response := decodeIsolationErrorResponse(t, w.Body.Bytes())
	assert.Equal(t, "container not found", response.Error)
}

func TestDeleteContainer(t *testing.T) {
	router, _ := setupIsolationTestRouter(t)

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
	router, _ := setupIsolationTestRouter(t)

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
	router, storage := setupIsolationTestRouter(t)
	requestID, sessionID, taskID, agentID := newIsolationTraceValues()

	created := mustCreateIsolationContainer(t, router, "main")

	checkBody, _ := json.Marshal(CheckAccessRequest{
		ContainerID:  created.ID,
		Resource:     "file",
		ResourcePath: "/src/main.go",
		Action:       "read",
	})
	req, _ := http.NewRequest("POST", "/api/v1/isolation/access/check", bytes.NewBuffer(checkBody))
	req.Header.Set("Content-Type", "application/json")
	setIsolationTraceHeaders(req, requestID, sessionID, taskID, agentID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assertIsolationTraceHeaders(t, w, requestID, sessionID, taskID, agentID)

	response := decodeIsolationResponseData[map[string]interface{}](t, w.Body.Bytes())
	assert.Contains(t, response, "decision")
	assert.Contains(t, response, "latency_ms")

	decision := extractIsolationDecision(t, response)
	auditID, ok := decision["audit_id"].(string)
	if assert.True(t, ok) {
		assert.NotEmpty(t, auditID)
	}
	assert.Equal(t, true, decision["allowed"])
	assert.Equal(t, "access granted by RBAC policy", decision["reason"])

	entry := lastIsolationAuditEntry(t, storage)
	assert.Equal(t, auditID, entry.ID)
	assert.Equal(t, audit.OutcomeSuccess, entry.Outcome)
	assert.Equal(t, "check_access", entry.Action)
	assert.Equal(t, created.ID, entry.Actor.ID)
	assert.Equal(t, "/src/main.go", entry.Resource.Path)
	assert.Equal(t, true, entry.Details["allowed"])
	assert.Equal(t, string(isolation.RoleMain), entry.Details["container_role"])
	assertIsolationAuditTrace(t, entry, requestID, sessionID, taskID, agentID, http.MethodPost, "/api/v1/isolation/access/check")
}

func TestValidateIO(t *testing.T) {
	router, storage := setupIsolationTestRouter(t)
	requestID, sessionID, taskID, agentID := newIsolationTraceValues()

	created := mustCreateIsolationContainer(t, router, "coder")

	validateBody, _ := json.Marshal(ValidateIORequest{
		ContainerID: created.ID,
		Input:       "api_key: sk-12345secret",
		Direction:   "input",
	})
	req, _ := http.NewRequest("POST", "/api/v1/isolation/io/validate", bytes.NewBuffer(validateBody))
	req.Header.Set("Content-Type", "application/json")
	setIsolationTraceHeaders(req, requestID, sessionID, taskID, agentID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assertIsolationTraceHeaders(t, w, requestID, sessionID, taskID, agentID)

	result := decodeIsolationResponseData[isolation.IOValidationResult](t, w.Body.Bytes())
	assert.True(t, result.Valid)
	assert.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.SanitizedInput, "[REDACTED]")

	entry := lastIsolationAuditEntry(t, storage)
	assert.Equal(t, audit.OutcomeSuccess, entry.Outcome)
	assert.Equal(t, "validate_io", entry.Action)
	assert.Equal(t, created.ID, entry.Actor.ID)
	assert.Equal(t, "input", entry.Resource.Path)
	assert.Equal(t, "input", entry.Details["direction"])
	assert.Equal(t, true, entry.Details["contains_sensitive_warning"])
	assert.Equal(t, true, entry.Details["sanitized_changed"])
	assertIsolationAuditTrace(t, entry, requestID, sessionID, taskID, agentID, http.MethodPost, "/api/v1/isolation/io/validate")
}

func TestGetRoles(t *testing.T) {
	router, _ := setupIsolationTestRouter(t)

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
	router, _ := setupIsolationTestRouter(t)

	req, _ := http.NewRequest("GET", "/api/v1/isolation/roles/main", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	response := decodeIsolationResponseData[isolation.RoleDefinition](t, w.Body.Bytes())
	assert.Equal(t, isolation.RoleMain, response.Name)
}

func TestGetRole_NotFound(t *testing.T) {
	router, _ := setupIsolationTestRouter(t)

	req, _ := http.NewRequest("GET", "/api/v1/isolation/roles/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	response := decodeIsolationErrorResponse(t, w.Body.Bytes())
	assert.Equal(t, "role not found", response.Error)
}

func TestRegisterRole(t *testing.T) {
	router, _ := setupIsolationTestRouter(t)

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
	router, _ := setupIsolationTestRouter(t)

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
	router, _ := setupIsolationTestRouter(t)

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
	router, _ := setupIsolationTestRouter(t)

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
	router, _ := setupIsolationTestRouter(t)

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
