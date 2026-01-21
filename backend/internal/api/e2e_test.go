// Package api - End-to-end tests for advanced API endpoints
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/config"
	"github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/isolation"
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/privacy"
	"github.com/codeflow/backend/internal/samg"
	"github.com/codeflow/backend/internal/snapshot"
	"github.com/codeflow/backend/internal/summarize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupE2EServer creates a test server with all services initialized
func setupE2EServer(t *testing.T) *httptest.Server {
	// Initialize all services
	snapshotSvc := snapshot.NewInMemorySnapshotService()
	snapshot.SetSnapshotService(snapshotSvc)

	// Config service - use in-memory for testing
	configSvc, err := config.NewSQLiteConfigService(":memory:")
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}
	config.SetConfigService(configSvc)

	hookMgr := hooks.NewHookManager()
	hooks.SetHookManager(hookMgr)

	preflightSvc := memory.NewMemoryPreflightService()
	memory.SetPreflightService(preflightSvc)

	summarizeSvc := summarize.NewSummarizerService()
	summarize.SetSummarizer(summarizeSvc)

	// Audit service with in-memory storage
	auditStorage := audit.NewMemoryStorage()
	auditSvc := audit.NewAuditService(auditStorage)
	audit.SetAuditService(auditSvc)

	privacySvc, err := privacy.NewPrivacyService("test-master-password", nil)
	if err != nil {
		t.Fatalf("Failed to create privacy service: %v", err)
	}
	privacy.SetPrivacyService(privacySvc)

	rbacMgr := isolation.NewRBACManager()
	isolationSvc := isolation.NewIsolationService(rbacMgr)
	isolation.SetIsolationService(isolationSvc)

	samgSvc := samg.NewSAMGService(nil)
	samg.SetSAMGService(samgSvc)

	// Create server
	cfg := &Config{
		Port:            "0",
		AllowedOrigins:  []string{"*"},
		EnableDebugMode: true,
	}
	server := NewServer(cfg)

	return httptest.NewServer(server.Router())
}

// TestE2E_SnapshotWorkflow tests the complete snapshot workflow
func TestE2E_SnapshotWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. Create snapshot
	createReq := map[string]interface{}{
		"name": "test-snapshot",
		"tags": []string{"test", "e2e"},
	}
	body, _ := json.Marshal(createReq)
	resp, err := client.Post(ts.URL+"/api/v1/snapshots", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var createResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createResp)
	resp.Body.Close()
	snapshotID := createResp["id"].(string)
	assert.NotEmpty(t, snapshotID)

	// 2. List snapshots
	resp, err = client.Get(ts.URL + "/api/v1/snapshots")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var listResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()
	assert.GreaterOrEqual(t, int(listResp["total"].(float64)), 1)

	// 3. Get snapshot details
	resp, err = client.Get(ts.URL + "/api/v1/snapshots/" + snapshotID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 4. Restore snapshot
	resp, err = client.Post(ts.URL+"/api/v1/snapshots/"+snapshotID+"/restore", "application/json", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 5. Delete snapshot
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/snapshots/"+snapshotID, nil)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_ConfigWorkflow tests the complete config workflow
func TestE2E_ConfigWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. Get global config
	resp, err := client.Get(ts.URL + "/api/v1/config/global")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 2. Update global config
	updateReq := map[string]interface{}{
		"model":       "claude-3-opus",
		"temperature": 0.7,
	}
	body, _ := json.Marshal(updateReq)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config/global", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 3. Get role config
	resp, err = client.Get(ts.URL + "/api/v1/config/roles/main")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 4. Resolve config
	resp, err = client.Get(ts.URL + "/api/v1/config/resolve?role=main")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_PAPIWorkflow tests the complete PAPI workflow
func TestE2E_PAPIWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. List PAPI variables (should work even if empty)
	resp, err := client.Get(ts.URL + "/api/v1/config/papi")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 2. Check conflicts endpoint
	resp, err = client.Get(ts.URL + "/api/v1/config/papi/conflicts")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_HooksWorkflow tests the complete hooks workflow
func TestE2E_HooksWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. List hooks
	resp, err := client.Get(ts.URL + "/api/v1/hooks")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 2. Get hook events
	resp, err = client.Get(ts.URL + "/api/v1/hooks/events")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_MemoryPreflightWorkflow tests the memory preflight workflow
func TestE2E_MemoryPreflightWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. Preflight check
	preflightReq := map[string]interface{}{
		"query":       "How to use React hooks?",
		"max_results": 10,
		"min_score":   0.3,
	}
	body, _ := json.Marshal(preflightReq)
	resp, err := client.Post(ts.URL+"/api/v1/memory/preflight", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 2. Get suggestions
	resp, err = client.Get(ts.URL + "/api/v1/memory/suggestions?context_id=test-ctx&limit=10")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_SummarizeWorkflow tests the summarize workflow
func TestE2E_SummarizeWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. Summarize conversation
	summarizeReq := map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "user", "content": "How do I implement authentication?"},
			{"role": "assistant", "content": "You can use JWT tokens for authentication..."},
		},
	}
	body, _ := json.Marshal(summarizeReq)
	resp, err := client.Post(ts.URL+"/api/v1/summarize/conversation", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_AuditWorkflow tests the audit workflow
func TestE2E_AuditWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. Get audit logs
	resp, err := client.Get(ts.URL + "/api/v1/audit/logs?limit=10")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 2. Verify audit chain
	verifyReq := map[string]interface{}{}
	body, _ := json.Marshal(verifyReq)
	resp, err = client.Post(ts.URL+"/api/v1/audit/verify", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 3. Get statistics
	resp, err = client.Get(ts.URL + "/api/v1/audit/statistics")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 4. Export logs
	resp, err = client.Get(ts.URL + "/api/v1/audit/export?format=json")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_PrivacyWorkflow tests the privacy workflow
func TestE2E_PrivacyWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. Detect PII
	detectReq := map[string]interface{}{
		"text": "Contact me at test@example.com or call 123-456-7890",
	}
	body, _ := json.Marshal(detectReq)
	resp, err := client.Post(ts.URL+"/api/v1/privacy/detect", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 2. Redact PII
	redactReq := map[string]interface{}{
		"text": "Contact me at test@example.com or call 123-456-7890",
	}
	body, _ = json.Marshal(redactReq)
	resp, err = client.Post(ts.URL+"/api/v1/privacy/redact", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 3. Get privacy metrics
	resp, err = client.Get(ts.URL + "/api/v1/privacy/metrics")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_IsolationWorkflow tests the isolation workflow
func TestE2E_IsolationWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. List containers
	resp, err := client.Get(ts.URL + "/api/v1/isolation/containers")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 2. List roles
	resp, err = client.Get(ts.URL + "/api/v1/isolation/roles")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 3. Create a container first (needed for access check and IO validate)
	createContainerReq := map[string]interface{}{
		"role": "main",
	}
	body, _ := json.Marshal(createContainerReq)
	resp, err = client.Post(ts.URL+"/api/v1/isolation/containers", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var containerResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&containerResp)
	resp.Body.Close()
	containerID := containerResp["id"].(string)
	assert.NotEmpty(t, containerID)

	// 4. Check access
	checkReq := map[string]interface{}{
		"container_id":  containerID,
		"resource":      "file",
		"resource_path": "/src/main.go",
		"action":        "read",
	}
	body, _ = json.Marshal(checkReq)
	resp, err = client.Post(ts.URL+"/api/v1/isolation/access/check", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 5. Validate I/O
	validateReq := map[string]interface{}{
		"container_id": containerID,
		"input":        "Hello world",
		"direction":    "output",
	}
	body, _ = json.Marshal(validateReq)
	resp, err = client.Post(ts.URL+"/api/v1/isolation/io/validate", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 6. Delete container
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/isolation/containers/"+containerID, nil)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_SAMGWorkflow tests the SAMG workflow
func TestE2E_SAMGWorkflow(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. Add triples
	addReq := map[string]interface{}{
		"triples": []map[string]interface{}{
			{
				"@id":        "triple-e2e-1",
				"subject":    map[string]interface{}{"@id": "entity:classA", "@type": []string{"codeflow:Class"}, "label": "ClassA"},
				"predicate":  "codeflow:extends",
				"object":     map[string]interface{}{"node": map[string]interface{}{"@id": "entity:classB", "@type": []string{"codeflow:Class"}, "label": "ClassB"}},
				"confidence": 0.9,
				"timestamp":  time.Now().UnixMilli(),
			},
		},
	}
	body, _ := json.Marshal(addReq)
	resp, err := client.Post(ts.URL+"/api/v1/samg/triples", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	// 2. Query triples
	resp, err = client.Get(ts.URL + "/api/v1/samg/triples")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 3. Get triple
	resp, err = client.Get(ts.URL + "/api/v1/samg/triples/triple-e2e-1")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 4. Extract triples
	extractReq := map[string]interface{}{
		"content":    "class UserService extends BaseService implements IUserService",
		"session_id": "e2e-session",
	}
	body, _ = json.Marshal(extractReq)
	resp, err = client.Post(ts.URL+"/api/v1/samg/extract", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 5. Activate
	activateReq := map[string]interface{}{
		"source_ids": []string{"entity:classA"},
	}
	body, _ = json.Marshal(activateReq)
	resp, err = client.Post(ts.URL+"/api/v1/samg/activate", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 6. Get decay config
	resp, err = client.Get(ts.URL + "/api/v1/samg/decay")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 7. Apply decay
	resp, err = client.Post(ts.URL+"/api/v1/samg/decay/apply", "application/json", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 8. Get visible nodes
	resp, err = client.Get(ts.URL + "/api/v1/samg/nodes/visible")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 9. Export graph
	resp, err = client.Get(ts.URL + "/api/v1/samg/graph/export")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 10. Get stats
	resp, err = client.Get(ts.URL + "/api/v1/samg/stats")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 11. Delete triples
	deleteReq := map[string]interface{}{
		"ids": []string{"triple-e2e-1"},
	}
	body, _ = json.Marshal(deleteReq)
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/samg/triples", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// TestE2E_CrossModuleIntegration tests cross-module integration
func TestE2E_CrossModuleIntegration(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	// 1. Create a snapshot before making changes
	createSnapshotReq := map[string]interface{}{
		"name": "before-changes",
		"tags": []string{"integration-test"},
	}
	body, _ := json.Marshal(createSnapshotReq)
	resp, err := client.Post(ts.URL+"/api/v1/snapshots", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var snapshotResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&snapshotResp)
	resp.Body.Close()
	snapshotID := snapshotResp["id"].(string)

	// 2. Update config
	configReq := map[string]interface{}{
		"model": "claude-3-opus",
	}
	body, _ = json.Marshal(configReq)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config/global", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 3. Add SAMG triples
	addTriplesReq := map[string]interface{}{
		"triples": []map[string]interface{}{
			{
				"@id":        "triple-integration-1",
				"subject":    map[string]interface{}{"@id": "entity:test", "@type": []string{"codeflow:Class"}, "label": "Test"},
				"predicate":  "codeflow:extends",
				"object":     map[string]interface{}{"node": map[string]interface{}{"@id": "entity:base", "@type": []string{"codeflow:Class"}, "label": "Base"}},
				"confidence": 0.9,
				"timestamp":  time.Now().UnixMilli(),
			},
		},
	}
	body, _ = json.Marshal(addTriplesReq)
	resp, err = client.Post(ts.URL+"/api/v1/samg/triples", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	// 4. Verify audit logs captured the changes
	resp, err = client.Get(ts.URL + "/api/v1/audit/logs?limit=10")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 5. Restore snapshot
	resp, err = client.Post(ts.URL+"/api/v1/snapshots/"+snapshotID+"/restore", "application/json", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 6. Cleanup
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/v1/snapshots/"+snapshotID, nil)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}
