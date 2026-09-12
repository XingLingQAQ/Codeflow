package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/memory"
)

type atomicMemoryServiceMock struct {
	addFn               func(ctx context.Context, mem *memory.AtomicMemory) error
	addWithReceiptFn    func(ctx context.Context, mem *memory.AtomicMemory) (memory.AtomicMutationReceipt, error)
	searchFn            func(ctx context.Context, query string, opts *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, error)
	searchWithReportFn  func(ctx context.Context, query string, opts *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, memory.AtomicSearchReport, error)
	getBySessionFn      func(ctx context.Context, sessionID string, limit, offset int) ([]memory.AtomicMemory, error)
	getByIDFn           func(ctx context.Context, id string) (*memory.AtomicMemory, error)
	updateFn            func(ctx context.Context, id string, updates *memory.AtomicMemoryUpdate) error
	updateWithReceiptFn func(ctx context.Context, id string, updates *memory.AtomicMemoryUpdate) (memory.AtomicMutationReceipt, error)
	deleteFn            func(ctx context.Context, id string) error
	deleteWithReceiptFn func(ctx context.Context, id string) (memory.AtomicMutationReceipt, error)
	applyHeatDecayFn    func(ctx context.Context) (int, error)
	recomputeTiersFn    func(ctx context.Context) (int, error)
	boostHeatFn         func(ctx context.Context, id string, boost float64) error
	searchByTierFn      func(ctx context.Context, tier memory.MemoryTier, limit int) ([]memory.AtomicMemory, error)
}

func (m *atomicMemoryServiceMock) AddWithReceipt(ctx context.Context, mem *memory.AtomicMemory) (memory.AtomicMutationReceipt, error) {
	if m.addWithReceiptFn != nil {
		return m.addWithReceiptFn(ctx, mem)
	}
	if m.addFn != nil {
		if err := m.addFn(ctx, mem); err != nil {
			return memory.AtomicMutationReceipt{}, err
		}
	}
	return memory.AtomicMutationReceipt{ID: mem.ID, Revision: 1, IndexSync: memory.AtomicIndexSyncSynced}, nil
}

func (m *atomicMemoryServiceMock) SearchWithReport(ctx context.Context, query string, opts *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, memory.AtomicSearchReport, error) {
	if m.searchWithReportFn != nil {
		return m.searchWithReportFn(ctx, query, opts)
	}
	if m.searchFn != nil {
		items, err := m.searchFn(ctx, query, opts)
		return items, memory.AtomicSearchReport{Exhausted: true}, err
	}
	return nil, memory.AtomicSearchReport{Exhausted: true}, nil
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

func (m *atomicMemoryServiceMock) UpdateWithReceipt(ctx context.Context, id string, updates *memory.AtomicMemoryUpdate) (memory.AtomicMutationReceipt, error) {
	if m.updateWithReceiptFn != nil {
		return m.updateWithReceiptFn(ctx, id, updates)
	}
	if m.updateFn != nil {
		if err := m.updateFn(ctx, id, updates); err != nil {
			return memory.AtomicMutationReceipt{}, err
		}
	}
	return memory.AtomicMutationReceipt{ID: id, Revision: 2, IndexSync: memory.AtomicIndexSyncSynced}, nil
}

func (m *atomicMemoryServiceMock) DeleteWithReceipt(ctx context.Context, id string) (memory.AtomicMutationReceipt, error) {
	if m.deleteWithReceiptFn != nil {
		return m.deleteWithReceiptFn(ctx, id)
	}
	if m.deleteFn != nil {
		if err := m.deleteFn(ctx, id); err != nil {
			return memory.AtomicMutationReceipt{}, err
		}
	}
	return memory.AtomicMutationReceipt{ID: id, Revision: 2, IndexSync: memory.AtomicIndexSyncSynced}, nil
}

func (m *atomicMemoryServiceMock) ApplyHeatDecay(ctx context.Context) (int, error) {
	if m.applyHeatDecayFn != nil {
		return m.applyHeatDecayFn(ctx)
	}
	return 0, nil
}

func (m *atomicMemoryServiceMock) RecomputeTiers(ctx context.Context) (int, error) {
	if m.recomputeTiersFn != nil {
		return m.recomputeTiersFn(ctx)
	}
	return 0, nil
}

func (m *atomicMemoryServiceMock) BoostHeat(ctx context.Context, id string, boost float64) error {
	if m.boostHeatFn != nil {
		return m.boostHeatFn(ctx, id, boost)
	}
	return nil
}

func (m *atomicMemoryServiceMock) SearchByTier(ctx context.Context, tier memory.MemoryTier, limit int) ([]memory.AtomicMemory, error) {
	if m.searchByTierFn != nil {
		return m.searchByTierFn(ctx, tier, limit)
	}
	return nil, nil
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
	router.POST("/api/v1/memory/atomic/:id/boost", BoostAtomicHeat)
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

func TestBoostAtomicHeatRejectsInvalidJSON(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	called := false
	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		boostHeatFn: func(_ context.Context, _ string, _ float64) error {
			called = true
			return nil
		},
	})

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/memory/atomic/m1/boost", bytes.NewBufferString(`{`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if called {
		t.Fatalf("BoostHeat should not be called for invalid JSON")
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

// ---------------------------------------------------------------------------
// T13.02.c：mutation 响应 envelope 的索引同步语义
// ---------------------------------------------------------------------------

// decodeAtomicResponseData 解析统一 envelope 并返回 data 对象。
func decodeAtomicResponseData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body failed: %v (body=%s)", err, w.Body.String())
	}
	if !body.Success {
		t.Fatalf("expected success response, body=%s", w.Body.String())
	}
	return body.Data
}

// 创建：正文已接受 + 索引同步 pending（向量未就位）分开表达；既有字段不变。
func TestCreateAtomicMemoryReportsIndexSyncPending(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		addWithReceiptFn: func(_ context.Context, mem *memory.AtomicMemory) (memory.AtomicMutationReceipt, error) {
			return memory.AtomicMutationReceipt{
				ID:         mem.ID,
				Revision:   1,
				IndexSync:  memory.AtomicIndexSyncPending,
				IndexError: "injected vector store fault",
			}, nil
		},
	})

	body := map[string]any{
		"content":    "待同步的正文",
		"session_id": "session-1",
		"source":     "user",
		"importance": 0.5,
	}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/memory/atomic", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusCreated, w.Code, w.Body.String())
	}
	data := decodeAtomicResponseData(t, w)
	if data["index_sync"] != "pending" {
		t.Fatalf("expected index_sync=pending, data=%v", data)
	}
	if data["index_error"] != "injected vector store fault" {
		t.Fatalf("expected index_error carried, data=%v", data)
	}
	if data["revision"] != float64(1) {
		t.Fatalf("expected revision=1, data=%v", data)
	}
	// 向后兼容：既有正文字段原样保留。
	if data["content"] != "待同步的正文" || data["session_id"] != "session-1" {
		t.Fatalf("existing fields must be preserved, data=%v", data)
	}
	if data["id"] == nil || data["id"] == "" {
		t.Fatalf("expected generated id preserved, data=%v", data)
	}
}

// 创建：索引同步完成时 index_sync=synced，且无 index_error 键。
func TestCreateAtomicMemoryReportsIndexSynced(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		addWithReceiptFn: func(_ context.Context, mem *memory.AtomicMemory) (memory.AtomicMutationReceipt, error) {
			return memory.AtomicMutationReceipt{ID: mem.ID, Revision: 1, IndexSync: memory.AtomicIndexSyncSynced}, nil
		},
	})

	body := map[string]any{
		"content":    "已同步的正文",
		"session_id": "session-1",
		"source":     "user",
		"importance": 0.5,
	}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/memory/atomic", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusCreated, w.Code, w.Body.String())
	}
	data := decodeAtomicResponseData(t, w)
	if data["index_sync"] != "synced" {
		t.Fatalf("expected index_sync=synced, data=%v", data)
	}
	if _, hasErr := data["index_error"]; hasErr {
		t.Fatalf("synced receipt must omit index_error, data=%v", data)
	}
}

// 更新：回执 revision/index_sync 原样透出，正文为更新后内容。
func TestUpdateAtomicMemoryReportsIndexSync(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		updateWithReceiptFn: func(_ context.Context, id string, _ *memory.AtomicMemoryUpdate) (memory.AtomicMutationReceipt, error) {
			return memory.AtomicMutationReceipt{ID: id, Revision: 2, IndexSync: memory.AtomicIndexSyncSynced}, nil
		},
		getByIDFn: func(_ context.Context, id string) (*memory.AtomicMemory, error) {
			return &memory.AtomicMemory{
				ID:         id,
				Timestamp:  100,
				Content:    "新内容",
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
	data := decodeAtomicResponseData(t, w)
	if data["index_sync"] != "synced" || data["revision"] != float64(2) {
		t.Fatalf("expected index_sync=synced revision=2, data=%v", data)
	}
	if data["content"] != "新内容" {
		t.Fatalf("expected updated content, data=%v", data)
	}
}

// 删除：保留既有 deleted/id 字段，新增 revision/index_sync。
func TestDeleteAtomicMemoryReportsIndexSync(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		deleteWithReceiptFn: func(_ context.Context, id string) (memory.AtomicMutationReceipt, error) {
			return memory.AtomicMutationReceipt{ID: id, Revision: 2, IndexSync: memory.AtomicIndexSyncPending, IndexError: "injected vector store fault"}, nil
		},
	})

	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/memory/atomic/m1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, w.Code, w.Body.String())
	}
	data := decodeAtomicResponseData(t, w)
	if data["deleted"] != true || data["id"] != "m1" {
		t.Fatalf("existing deleted/id fields must be preserved, data=%v", data)
	}
	if data["index_sync"] != "pending" || data["revision"] != float64(2) {
		t.Fatalf("expected index_sync=pending revision=2, data=%v", data)
	}
	if data["index_error"] != "injected vector store fault" {
		t.Fatalf("expected index_error carried, data=%v", data)
	}
}

// ---------------------------------------------------------------------------
// T13.02.c：Search 不完整原因（负向核心：向量未同步时不得返回"完整空结果"）
// ---------------------------------------------------------------------------

// 向量未同步（pending/failed job）时：响应含 incomplete_reason 与
// search_report 明细，空 memories 不再伪装成完整空结果。
func TestSearchAtomicMemoryReportsIncompleteReason(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		searchWithReportFn: func(_ context.Context, _ string, _ *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, memory.AtomicSearchReport, error) {
			return []memory.AtomicMemory{}, memory.AtomicSearchReport{
				CandidatesScanned: 0,
				Batches:           1,
				Exhausted:         true,
				IndexPendingJobs:  2,
				IndexFailedJobs:   1,
			}, nil
		},
	})

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/memory/atomic/search?query=%E5%81%8F%E5%A5%BD", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, w.Code, w.Body.String())
	}
	data := decodeAtomicResponseData(t, w)
	reason, _ := data["incomplete_reason"].(string)
	if reason == "" {
		t.Fatalf("expected non-empty incomplete_reason, data=%v", data)
	}
	if !strings.Contains(reason, "index_sync_pending=2") || !strings.Contains(reason, "index_sync_failed=1") {
		t.Fatalf("incomplete_reason must carry pending/failed counts, got %q", reason)
	}
	if data["count"] != float64(0) {
		t.Fatalf("expected count=0, data=%v", data)
	}
	report, ok := data["search_report"].(map[string]any)
	if !ok {
		t.Fatalf("expected search_report object, data=%v", data)
	}
	if report["index_pending_jobs"] != float64(2) || report["index_failed_jobs"] != float64(1) || report["exhausted"] != true {
		t.Fatalf("unexpected search_report: %v", report)
	}
}

// 完整结果：索引已同步且候选耗尽时，不含 incomplete_reason 键。
func TestSearchAtomicMemoryCompleteHasNoIncompleteReason(t *testing.T) {
	router := setupAtomicMemoryHandlerTest(t)
	defer setAtomicMemoryServiceForTest(nil)

	setAtomicMemoryServiceForTest(&atomicMemoryServiceMock{
		searchWithReportFn: func(_ context.Context, _ string, _ *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, memory.AtomicSearchReport, error) {
			return []memory.AtomicMemory{{ID: "m1", Content: "偏好中文", SessionID: "s1", Source: memory.AtomicMemorySourceUser, Importance: 0.7, Timestamp: 1}},
				memory.AtomicSearchReport{CandidatesScanned: 1, Batches: 1, Exhausted: true}, nil
		},
	})

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/memory/atomic/search?query=%E5%81%8F%E5%A5%BD", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, w.Code, w.Body.String())
	}
	data := decodeAtomicResponseData(t, w)
	if _, hasReason := data["incomplete_reason"]; hasReason {
		t.Fatalf("complete result must omit incomplete_reason, data=%v", data)
	}
	if data["count"] != float64(1) {
		t.Fatalf("expected count=1, data=%v", data)
	}
	report, ok := data["search_report"].(map[string]any)
	if !ok || report["exhausted"] != true || report["index_pending_jobs"] != float64(0) {
		t.Fatalf("unexpected search_report: %v", report)
	}
}
