package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/memory"
)

type atomicMemoryServiceMock struct {
	addFn          func(ctx context.Context, mem *memory.AtomicMemory) error
	searchFn       func(ctx context.Context, query string, opts *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, error)
	getBySessionFn func(ctx context.Context, sessionID string, limit, offset int) ([]memory.AtomicMemory, error)
	getByIDFn      func(ctx context.Context, id string) (*memory.AtomicMemory, error)
	updateFn       func(ctx context.Context, id string, updates *memory.AtomicMemoryUpdate) error
	deleteFn       func(ctx context.Context, id string) error
}

func (m *atomicMemoryServiceMock) Add(ctx context.Context, mem *memory.AtomicMemory) error {
	if m.addFn != nil {
		return m.addFn(ctx, mem)
	}
	return nil
}

func (m *atomicMemoryServiceMock) Search(ctx context.Context, query string, opts *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, query, opts)
	}
	return nil, nil
}

func (m *atomicMemoryServiceMock) GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]memory.AtomicMemory, error) {
	if m.getBySessionFn != nil {
		return m.getBySessionFn(ctx, sessionID, limit, offset)
	}
	return nil, nil
}

func (m *atomicMemoryServiceMock) GetByID(ctx context.Context, id string) (*memory.AtomicMemory, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, memory.ErrAtomicMemoryNotFound
}

func (m *atomicMemoryServiceMock) Update(ctx context.Context, id string, updates *memory.AtomicMemoryUpdate) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, updates)
	}
	return nil
}

func (m *atomicMemoryServiceMock) Delete(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func setupAtomicMemoryHandlerTest(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	setAtomicMemoryServiceForTest(nil)

	router := gin.New()
	router.POST("/api/v1/memory/atomic", CreateAtomicMemory)
	router.GET("/api/v1/memory/atomic/search", SearchAtomicMemory)
	router.GET("/api/v1/memory/atomic/session/:id", GetAtomicMemoriesBySession)
	router.PUT("/api/v1/memory/atomic/:id", UpdateAtomicMemory)
	router.DELETE("/api/v1/memory/atomic/:id", DeleteAtomicMemory)
	return router
}

func TestCreateAtomicMemory(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	var created *memory.AtomicMemory
	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		addFn: func(_ context.Context, mem *memory.AtomicMemory) error {
			created = mem
			return nil
		},
	})

	body := map[string]any{
		"content":    "记住用户偏好中文输出",
		"tags":       []string{"preference", "language"},
		"session_id": "session-1",
		"source":     "user",
		"importance": 0.9,
	}
	payload, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/memory/atomic", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusCreated, w.Code, w.Body.String())
	}
	if created == nil {
		t.Fatalf("expected service Add to receive memory")
	}
	if created.ID == "" {
		t.Fatalf("expected generated id")
	}
	if created.Timestamp <= 0 {
		t.Fatalf("expected generated timestamp")
	}
}

func TestCreateAtomicMemoryValidation(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{})

	body := map[string]any{
		"content":    "",
		"session_id": "session-1",
		"source":     "user",
		"importance": 0.3,
	}
	payload, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/memory/atomic", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestSearchAtomicMemory(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		searchFn: func(_ context.Context, query string, opts *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, error) {
			if query != "偏好" {
				t.Fatalf("unexpected query: %s", query)
			}
			if opts == nil || opts.Limit != 5 || opts.Offset != 1 || opts.SessionID != "s1" {
				t.Fatalf("unexpected options: %+v", opts)
			}
			return []memory.AtomicMemory{{ID: "m1", Content: "偏好中文", SessionID: "s1", Source: memory.AtomicMemorySourceUser, Importance: 0.7, Timestamp: 1}}, nil
		},
	})

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/memory/atomic/search?query=%E5%81%8F%E5%A5%BD&limit=5&offset=1&session_id=s1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestSearchAtomicMemoryQueryRequired(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{})

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/memory/atomic/search", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestGetAtomicMemoriesBySession(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		getBySessionFn: func(_ context.Context, sessionID string, limit, offset int) ([]memory.AtomicMemory, error) {
			if sessionID != "session-1" || limit != 20 || offset != 2 {
				t.Fatalf("unexpected params: session=%s limit=%d offset=%d", sessionID, limit, offset)
			}
			return []memory.AtomicMemory{{ID: "m1", SessionID: sessionID, Content: "c", Source: memory.AtomicMemorySourceUser, Importance: 0.4, Timestamp: 1}}, nil
		},
	})

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/memory/atomic/session/session-1?limit=20&offset=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestUpdateAtomicMemory(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		updateFn: func(_ context.Context, id string, updates *memory.AtomicMemoryUpdate) error {
			if id != "m1" {
				t.Fatalf("unexpected id: %s", id)
			}
			if updates.Content == nil || *updates.Content != "新内容" {
				t.Fatalf("unexpected update content: %+v", updates.Content)
			}
			return nil
		},
		getByIDFn: func(_ context.Context, id string) (*memory.AtomicMemory, error) {
			content := "新内容"
			return &memory.AtomicMemory{
				ID:         id,
				Timestamp:  100,
				Content:    content,
				SessionID:  "session-1",
				Source:     memory.AtomicMemorySourceAssistant,
				Importance: 0.6,
			}, nil
		},
	})

	body := map[string]any{"content": "新内容"}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/memory/atomic/m1", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestUpdateAtomicMemoryNotFound(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		updateFn: func(_ context.Context, _ string, _ *memory.AtomicMemoryUpdate) error {
			return memory.ErrAtomicMemoryNotFound
		},
	})

	body := map[string]any{"content": "新内容"}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/memory/atomic/m404", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestDeleteAtomicMemory(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		deleteFn: func(_ context.Context, id string) error {
			if id != "m1" {
				t.Fatalf("unexpected id: %s", id)
			}
			return nil
		},
	})

	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/memory/atomic/m1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestDeleteAtomicMemoryNotFound(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		deleteFn: func(_ context.Context, _ string) error {
			return memory.ErrAtomicMemoryNotFound
		},
	})

	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/memory/atomic/m404", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestUpdateAtomicMemoryValidation(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{})

	body := map[string]any{"importance": 1.8}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/memory/atomic/m1", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestSearchAtomicMemoryInvalidTimeRange(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{})

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/memory/atomic/search?query=q&start_at=200&end_at=100", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateAtomicMemoryServiceInitFailure(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		addFn: func(_ context.Context, _ *memory.AtomicMemory) error {
			return errors.New("unexpected")
		},
	})

	body := map[string]any{
		"content":    "abc",
		"session_id": "s1",
		"source":     "user",
		"importance": 0.5,
	}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/memory/atomic", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}
