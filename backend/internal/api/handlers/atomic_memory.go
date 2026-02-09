package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"

	"github.com/codeflow/backend/internal/memory"
)

type atomicMemoryService interface {
	Add(ctx context.Context, mem *memory.AtomicMemory) error
	Search(ctx context.Context, query string, opts *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, error)
	GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]memory.AtomicMemory, error)
	GetByID(ctx context.Context, id string) (*memory.AtomicMemory, error)
	Update(ctx context.Context, id string, updates *memory.AtomicMemoryUpdate) error
	Delete(ctx context.Context, id string) error
}

var (
	defaultAtomicMemoryService atomicMemoryService
	atomicMemoryServiceMu      sync.Mutex
)

func getAtomicMemoryService() (atomicMemoryService, error) {
	atomicMemoryServiceMu.Lock()
	defer atomicMemoryServiceMu.Unlock()

	if defaultAtomicMemoryService != nil {
		return defaultAtomicMemoryService, nil
	}

	svc, err := initAtomicMemoryService()
	if err != nil {
		return nil, err
	}
	defaultAtomicMemoryService = svc
	return defaultAtomicMemoryService, nil
}

func initAtomicMemoryService() (atomicMemoryService, error) {
	dataDir := filepath.Join(".", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "atomic_memory.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open atomic memory db: %w", err)
	}

	vectorDBPath := filepath.Join(dataDir, "atomic_vectors.db")
	vectorStore, err := memory.CreateSQLiteVectorStore(&memory.VectorStoreConfig{
		CollectionName: "atomic_memory",
		DBPath:         vectorDBPath,
		WALMode:        true,
	}, memory.NewSimpleEmbeddingProvider(384))
	if err != nil {
		return nil, fmt.Errorf("create atomic vector store: %w", err)
	}

	svc, err := memory.NewAtomicMemoryService(context.Background(), db, vectorStore, memory.NewSimpleEmbeddingProvider(384))
	if err != nil {
		return nil, fmt.Errorf("create atomic memory service: %w", err)
	}
	return svc, nil
}

func setAtomicMemoryServiceForTest(svc atomicMemoryService) {
	atomicMemoryServiceMu.Lock()
	defer atomicMemoryServiceMu.Unlock()
	defaultAtomicMemoryService = svc
}

type createAtomicMemoryRequest struct {
	Content    string    `json:"content"`
	Tags       []string  `json:"tags"`
	SessionID  string    `json:"session_id"`
	FolderID   *string   `json:"folder_id,omitempty"`
	Source     string    `json:"source"`
	Importance float64   `json:"importance"`
	Timestamp  *int64    `json:"timestamp,omitempty"`
	Embedding  []float64 `json:"embedding,omitempty"`
}

type updateAtomicMemoryRequest struct {
	Timestamp     *int64     `json:"timestamp,omitempty"`
	Content       *string    `json:"content,omitempty"`
	Tags          *[]string  `json:"tags,omitempty"`
	SessionID     *string    `json:"session_id,omitempty"`
	FolderID      *string    `json:"folder_id,omitempty"`
	ClearFolderID bool       `json:"clear_folder_id,omitempty"`
	Source        *string    `json:"source,omitempty"`
	Importance    *float64   `json:"importance,omitempty"`
	Embedding     *[]float64 `json:"embedding,omitempty"`
}

// CreateAtomicMemory handles POST /api/v1/memory/atomic.
func CreateAtomicMemory(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	var req createAtomicMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	req.Content = strings.TrimSpace(req.Content)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Source = strings.TrimSpace(req.Source)

	if req.Content == "" {
		respondError(c, http.StatusBadRequest, "content is required")
		return
	}
	if req.SessionID == "" {
		respondError(c, http.StatusBadRequest, "session_id is required")
		return
	}

	source := memory.AtomicMemorySource(req.Source)
	if !isValidAtomicMemorySource(source) {
		respondError(c, http.StatusBadRequest, "source must be one of: user, assistant, system")
		return
	}
	if req.Importance < 0 || req.Importance > 1 {
		respondError(c, http.StatusBadRequest, "importance must be in range [0,1]")
		return
	}

	timestamp := time.Now().Unix()
	if req.Timestamp != nil {
		if *req.Timestamp <= 0 {
			respondError(c, http.StatusBadRequest, "timestamp must be positive")
			return
		}
		timestamp = *req.Timestamp
	}

	mem := &memory.AtomicMemory{
		ID:         uuid.NewString(),
		Timestamp:  timestamp,
		Content:    req.Content,
		Tags:       req.Tags,
		SessionID:  req.SessionID,
		FolderID:   req.FolderID,
		Source:     source,
		Importance: req.Importance,
		Embedding:  req.Embedding,
	}
	if err := mem.Validate(); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid atomic memory payload: "+err.Error())
		return
	}

	if err := svc.Add(c.Request.Context(), mem); err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create atomic memory: "+err.Error())
		return
	}

	respondCreated(c, mem)
}

// SearchAtomicMemory handles GET /api/v1/memory/atomic/search.
func SearchAtomicMemory(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		respondError(c, http.StatusBadRequest, "query is required")
		return
	}

	opts, err := buildAtomicSearchOptions(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	items, err := svc.Search(c.Request.Context(), query, opts)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to search atomic memories: "+err.Error())
		return
	}

	respondOK(c, items)
}

// GetAtomicMemoriesBySession handles GET /api/v1/memory/atomic/session/:id.
func GetAtomicMemoriesBySession(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		respondError(c, http.StatusBadRequest, "session id is required")
		return
	}

	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value <= 0 {
			respondError(c, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = value
	}

	offset := 0
	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 0 {
			respondError(c, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		offset = value
	}

	items, err := svc.GetBySession(c.Request.Context(), sessionID, limit, offset)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get session atomic memories: "+err.Error())
		return
	}

	respondOK(c, items)
}

// UpdateAtomicMemory handles PUT /api/v1/memory/atomic/:id.
func UpdateAtomicMemory(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		respondError(c, http.StatusBadRequest, "id is required")
		return
	}

	var req updateAtomicMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	updates, err := buildAtomicMemoryUpdate(&req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if !hasAtomicUpdates(updates) {
		respondError(c, http.StatusBadRequest, "no updatable fields provided")
		return
	}

	if err := svc.Update(c.Request.Context(), id, updates); err != nil {
		if errors.Is(err, memory.ErrAtomicMemoryNotFound) {
			respondError(c, http.StatusNotFound, "Atomic memory not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to update atomic memory: "+err.Error())
		return
	}

	item, err := svc.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, memory.ErrAtomicMemoryNotFound) {
			respondError(c, http.StatusNotFound, "Atomic memory not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to load updated atomic memory: "+err.Error())
		return
	}

	respondOK(c, item)
}

// DeleteAtomicMemory handles DELETE /api/v1/memory/atomic/:id.
func DeleteAtomicMemory(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		respondError(c, http.StatusBadRequest, "id is required")
		return
	}

	if err := svc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, memory.ErrAtomicMemoryNotFound) {
			respondError(c, http.StatusNotFound, "Atomic memory not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to delete atomic memory: "+err.Error())
		return
	}

	respondOK(c, gin.H{"deleted": true, "id": id})
}

func buildAtomicSearchOptions(c *gin.Context) (*memory.AtomicMemorySearchOptions, error) {
	opts := &memory.AtomicMemorySearchOptions{}

	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return nil, errors.New("limit must be a positive integer")
		}
		opts.Limit = limit
	}

	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return nil, errors.New("offset must be a non-negative integer")
		}
		opts.Offset = offset
	}

	opts.SessionID = strings.TrimSpace(c.Query("session_id"))
	opts.FolderID = strings.TrimSpace(c.Query("folder_id"))
	opts.Tags = splitAndTrim(c.Query("tags"))

	if raw := strings.TrimSpace(c.Query("start_at")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return nil, errors.New("start_at must be a positive unix timestamp")
		}
		opts.StartAt = &value
	}

	if raw := strings.TrimSpace(c.Query("end_at")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return nil, errors.New("end_at must be a positive unix timestamp")
		}
		opts.EndAt = &value
	}

	if opts.StartAt != nil && opts.EndAt != nil && *opts.StartAt > *opts.EndAt {
		return nil, errors.New("start_at cannot be greater than end_at")
	}

	return opts, nil
}

func buildAtomicMemoryUpdate(req *updateAtomicMemoryRequest) (*memory.AtomicMemoryUpdate, error) {
	updates := &memory.AtomicMemoryUpdate{
		Timestamp:     req.Timestamp,
		Tags:          req.Tags,
		ClearFolderID: req.ClearFolderID,
		Importance:    req.Importance,
		Embedding:     req.Embedding,
	}

	if req.Timestamp != nil && *req.Timestamp <= 0 {
		return nil, errors.New("timestamp must be positive")
	}
	if req.Importance != nil && (*req.Importance < 0 || *req.Importance > 1) {
		return nil, errors.New("importance must be in range [0,1]")
	}

	if req.Content != nil {
		content := strings.TrimSpace(*req.Content)
		if content == "" {
			return nil, errors.New("content cannot be empty")
		}
		updates.Content = &content
	}
	if req.SessionID != nil {
		sessionID := strings.TrimSpace(*req.SessionID)
		if sessionID == "" {
			return nil, errors.New("session_id cannot be empty")
		}
		updates.SessionID = &sessionID
	}
	if req.FolderID != nil {
		folderID := strings.TrimSpace(*req.FolderID)
		updates.FolderID = &folderID
	}
	if req.Source != nil {
		source := memory.AtomicMemorySource(strings.TrimSpace(*req.Source))
		if !isValidAtomicMemorySource(source) {
			return nil, errors.New("source must be one of: user, assistant, system")
		}
		updates.Source = &source
	}

	return updates, nil
}

func hasAtomicUpdates(updates *memory.AtomicMemoryUpdate) bool {
	return updates.Timestamp != nil ||
		updates.Content != nil ||
		updates.Tags != nil ||
		updates.SessionID != nil ||
		updates.FolderID != nil ||
		updates.ClearFolderID ||
		updates.Source != nil ||
		updates.Importance != nil ||
		updates.Embedding != nil
}

func isValidAtomicMemorySource(source memory.AtomicMemorySource) bool {
	switch source {
	case memory.AtomicMemorySourceUser, memory.AtomicMemorySourceAssistant, memory.AtomicMemorySourceSystem:
		return true
	default:
		return false
	}
}

func splitAndTrim(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
