package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"

	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/samg"
)

type memoryAgentHandlerDeps struct {
	agent   *memory.MemoryAgent
	cleanup func()
}

type failingRawArchive struct{}

func (f failingRawArchive) Store(ctx context.Context, entry memory.RawEntry) (string, error) {
	return "", errors.New("store failed")
}

func (f failingRawArchive) Get(ctx context.Context, id string) (*memory.RawEntry, error) {
	return nil, errors.New("get failed")
}

func (f failingRawArchive) Search(ctx context.Context, query string, limit int) ([]memory.RawEntry, error) {
	return nil, errors.New("search failed")
}

func (f failingRawArchive) List(ctx context.Context, opts *memory.RawArchiveSearchOptions) ([]memory.RawEntry, error) {
	return nil, errors.New("list failed")
}

func (f failingRawArchive) Count(ctx context.Context) (int, error) {
	return 0, errors.New("count failed")
}

func (f failingRawArchive) Close() error {
	return nil
}

func setupMemoryAgentHandlerRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	memoryGroup := v1.Group("/memory")
	{
		memoryGroup.POST("/agent/ingest", MemoryAgentIngest)
		memoryGroup.POST("/agent/retrieve", MemoryAgentRetrieve)
		memoryGroup.POST("/agent/context", MemoryAgentContext)
	}

	return router
}

func setupMemoryAgentHandlerDeps(t *testing.T) memoryAgentHandlerDeps {
	t.Helper()

	tmpDir := t.TempDir()
	archive := memory.NewSQLiteRawArchive(filepath.Join(tmpDir, "raw_archive.db"))

	db, err := sql.Open("sqlite3", filepath.Join(tmpDir, "atomic_memory.db"))
	if err != nil {
		t.Fatalf("open atomic memory db failed: %v", err)
	}

	embeddingProvider := memory.NewSimpleEmbeddingProvider(32)
	vectorStore, err := memory.CreateSQLiteVectorStore(&memory.VectorStoreConfig{
		CollectionName: "memory_agent_handler_test",
		DBPath:         filepath.Join(tmpDir, "atomic_vectors.db"),
		WALMode:        true,
	}, embeddingProvider)
	if err != nil {
		_ = db.Close()
		t.Fatalf("create vector store failed: %v", err)
	}

	atomicSvc, err := memory.NewAtomicMemoryService(context.Background(), db, vectorStore, embeddingProvider)
	if err != nil {
		_ = vectorStore.Close()
		_ = db.Close()
		t.Fatalf("create atomic memory service failed: %v", err)
	}

	samgSvc := samg.NewSAMGService(nil)
	cleanup := func() {
		memory.SetMemoryAgent(nil)
		_ = vectorStore.Close()
		_ = db.Close()
		_ = archive.Close()
	}

	return memoryAgentHandlerDeps{
		agent: memory.NewMemoryAgent(archive, atomicSvc, samgSvc),
		cleanup: cleanup,
	}
}

func TestMemoryAgentHandlersLifecycle(t *testing.T) {
	router := setupMemoryAgentHandlerRouter()
	deps := setupMemoryAgentHandlerDeps(t)
	defer deps.cleanup()
	memory.SetMemoryAgent(deps.agent)

	ingestBody := map[string]any{
		"content":    "class UserService extends BaseService implements IUserService",
		"type":       "conversation",
		"session_id": "handler-session",
		"source":     "assistant",
		"tags":       []string{"handler", "memory-agent"},
	}
	body, _ := json.Marshal(ingestBody)
	createReq, _ := http.NewRequest("POST", "/api/v1/memory/agent/ingest", bytes.NewBuffer(body))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)

	assert.Equal(t, http.StatusCreated, createResp.Code)
	ingested := decodeContextResponseData[memory.IngestResult](t, createResp.Body.Bytes())
	assert.NotEmpty(t, ingested.RawArchiveID)
	assert.NotEmpty(t, ingested.AtomicMemoryID)
	assert.Greater(t, ingested.SAMGTripleCount, 0)

	retrieveBody := map[string]any{
		"query":       "UserService",
		"session_id":  "handler-session",
		"max_results": 10,
	}
	body, _ = json.Marshal(retrieveBody)
	retrieveReq, _ := http.NewRequest("POST", "/api/v1/memory/agent/retrieve", bytes.NewBuffer(body))
	retrieveReq.Header.Set("Content-Type", "application/json")
	retrieveResp := httptest.NewRecorder()
	router.ServeHTTP(retrieveResp, retrieveReq)

	assert.Equal(t, http.StatusOK, retrieveResp.Code)
	retrieved := decodeContextResponseData[memory.RetrieveResult](t, retrieveResp.Body.Bytes())
	assert.NotEmpty(t, retrieved.AtomicMemories)
	assert.NotEmpty(t, retrieved.SAMGNodes)
	assert.NotEmpty(t, retrieved.Sources)
	assert.Greater(t, retrieved.TotalFound, 0)

	hasResolvedSource := false
	for _, source := range retrieved.Sources {
		if source.Kind == memory.MemorySourceKindSAMGPointer && source.SourceID == ingested.RawArchiveID {
			assert.Equal(t, "handler-session", source.SessionID)
			assert.Contains(t, source.Content, "UserService")
			hasResolvedSource = true
		}
	}
	assert.True(t, hasResolvedSource)

	contextBody := map[string]any{
		"query":      "UserService",
		"session_id": "handler-session",
		"max_tokens": 200,
	}
	body, _ = json.Marshal(contextBody)
	contextReq, _ := http.NewRequest("POST", "/api/v1/memory/agent/context", bytes.NewBuffer(body))
	contextReq.Header.Set("Content-Type", "application/json")
	contextResp := httptest.NewRecorder()
	router.ServeHTTP(contextResp, contextReq)

	assert.Equal(t, http.StatusOK, contextResp.Code)
	contextResult := decodeContextResponseData[memory.ContextResult](t, contextResp.Body.Bytes())
	assert.NotEmpty(t, contextResult.AtomicMemories)
	assert.NotEmpty(t, contextResult.SAMGNodes)
	assert.NotEmpty(t, contextResult.Sources)
	assert.Equal(t, len(contextResult.Sources), contextResult.SourceCount)
	assert.Contains(t, contextResult.ContextBlock, "<memory_context>")
	assert.Contains(t, contextResult.ContextBlock, "UserService")
}

func TestMemoryAgentIngestBadRequest(t *testing.T) {
	router := setupMemoryAgentHandlerRouter()
	deps := setupMemoryAgentHandlerDeps(t)
	defer deps.cleanup()
	memory.SetMemoryAgent(deps.agent)

	req, _ := http.NewRequest("POST", "/api/v1/memory/agent/ingest", bytes.NewBufferString("{"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
	var envelope Response
	err := json.Unmarshal(resp.Body.Bytes(), &envelope)
	assert.NoError(t, err)
	assert.False(t, envelope.Success)
	assert.Contains(t, envelope.Error, "Invalid request")
}

func TestMemoryAgentRetrieveServiceError(t *testing.T) {
	router := setupMemoryAgentHandlerRouter()
	memory.SetMemoryAgent(memory.NewMemoryAgent(failingRawArchive{}, nil, nil))
	defer memory.SetMemoryAgent(nil)

	body, _ := json.Marshal(map[string]any{
		"query":       "boom",
		"session_id":  "failure-session",
		"max_results": 5,
	})
	req, _ := http.NewRequest("POST", "/api/v1/memory/agent/retrieve", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusInternalServerError, resp.Code)
	var envelope Response
	err := json.Unmarshal(resp.Body.Bytes(), &envelope)
	assert.NoError(t, err)
	assert.False(t, envelope.Success)
	assert.True(t, strings.Contains(envelope.Error, "Retrieve failed") || strings.Contains(envelope.Error, "retrieve raw archive fallback"))
}
