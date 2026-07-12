// Package memory - Memory service for STM/LTM management
package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// IMemoryService 记忆服务接口
type IMemoryService interface {
	// CRUD
	List(ctx context.Context, opts *MemoryListOptions) (*MemoryListResponse, error)
	Get(ctx context.Context, id string) (*MemoryItem, error)
	Create(ctx context.Context, req *MemoryItemCreateRequest) (*MemoryItem, error)
	Update(ctx context.Context, id string, req *MemoryItemUpdateRequest) (*MemoryItem, error)
	Delete(ctx context.Context, id string) error

	// Archive/Restore
	Archive(ctx context.Context, id string) (*MemoryItem, error)
	Restore(ctx context.Context, id string) (*MemoryItem, error)

	// Batch operations
	RefreshHeatScores(ctx context.Context) error

	// ReplaceItems replaces session-scoped (or all, if sessionID empty) memory items with the provided set.
	ReplaceItems(ctx context.Context, sessionID string, items []MemoryItem) error
}

// InMemoryService 内存实现的记忆服务 (用于开发/测试)
type InMemoryService struct {
	mu    sync.RWMutex
	items map[string]*MemoryItem
}

// NewInMemoryService 创建内存记忆服务
func NewInMemoryService() *InMemoryService {
	return &InMemoryService{
		items: make(map[string]*MemoryItem),
	}
}

// List 列出记忆项
func (s *InMemoryService) List(ctx context.Context, opts *MemoryListOptions) (*MemoryListResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 过滤
	var filtered []*MemoryItem
	for _, item := range s.items {
		if opts.Type != "" && item.Type != opts.Type {
			continue
		}
		if opts.SessionID != "" && item.SessionID != opts.SessionID {
			continue
		}
		if opts.Status != "" && string(item.Status) != opts.Status {
			continue
		}
		filtered = append(filtered, item)
	}

	// 排序
	sortBy := opts.SortBy
	if sortBy == "" {
		sortBy = "timestamp"
	}
	sortOrder := opts.SortOrder
	if sortOrder == "" {
		sortOrder = "desc"
	}

	sort.Slice(filtered, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "heat":
			less = filtered[i].Heat < filtered[j].Heat
		case "surprise":
			less = filtered[i].Surprise < filtered[j].Surprise
		default: // timestamp
			less = filtered[i].Timestamp < filtered[j].Timestamp
		}
		if sortOrder == "desc" {
			return !less
		}
		return less
	})

	// 分页
	total := len(filtered)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	// 转换为值切片
	items := make([]MemoryItem, 0, end-start)
	for _, item := range filtered[start:end] {
		items = append(items, *item)
	}

	hasMore := end < total
	var nextOffset int
	if hasMore {
		nextOffset = end
	}

	return &MemoryListResponse{
		Items:      items,
		Total:      total,
		HasMore:    hasMore,
		NextOffset: nextOffset,
	}, nil
}

// Get 获取单个记忆项
func (s *InMemoryService) Get(ctx context.Context, id string) (*MemoryItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	item, ok := s.items[id]
	if !ok {
		return nil, nil
	}
	return item, nil
}

// Create 创建记忆项
func (s *InMemoryService) Create(ctx context.Context, req *MemoryItemCreateRequest) (*MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	memType := req.Type
	if memType == "" {
		memType = MemoryTypeSTM
	}

	item := &MemoryItem{
		ID:           uuid.New().String(),
		Content:      req.Content,
		Type:         memType,
		Status:       MemoryStatusActive,
		SessionID:    req.SessionID,
		MessageIndex: len(s.items), // 简化实现
		Timestamp:    now,
		Heat:         1.0, // 新创建的记忆热度最高
		Surprise:     0.5, // 默认中等惊喜度
		Tags:         req.Tags,
		Source:       req.Source,
		IsPermanent:  req.IsPermanent,
	}

	s.items[item.ID] = item
	return item, nil
}

// Update 更新记忆项
func (s *InMemoryService) Update(ctx context.Context, id string, req *MemoryItemUpdateRequest) (*MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[id]
	if !ok {
		return nil, nil
	}

	if req.Tags != nil {
		item.Tags = *req.Tags
	}
	if req.IsPermanent != nil {
		item.IsPermanent = *req.IsPermanent
	}

	return item, nil
}

// Delete 删除记忆项
func (s *InMemoryService) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.items, id)
	return nil
}

// Archive 归档记忆项 (STM -> LTM)
func (s *InMemoryService) Archive(ctx context.Context, id string) (*MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[id]
	if !ok {
		return nil, nil
	}

	now := time.Now().Unix()
	item.Type = MemoryTypeLTM
	item.Status = MemoryStatusArchived
	item.ArchivedAt = &now

	return item, nil
}

// Restore 恢复记忆项 (LTM -> STM)
func (s *InMemoryService) Restore(ctx context.Context, id string) (*MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[id]
	if !ok {
		return nil, nil
	}

	item.Type = MemoryTypeSTM
	item.Status = MemoryStatusActive
	item.ArchivedAt = nil
	item.Heat = 1.0 // 恢复后热度重置

	return item, nil
}

// RefreshHeatScores 刷新所有记忆项的热度分数
func (s *InMemoryService) RefreshHeatScores(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, item := range s.items {
		// 简化实现: 只基于时间衰减
		item.Heat = CalculateHeat(item.Timestamp, 0, 0.5)
	}

	return nil
}

// 全局服务实例

// ReplaceItems replaces memory items for a session (or all items if sessionID is empty).
// Used by snapshot true restore for vector/memory state.
func (s *InMemoryService) ReplaceItems(ctx context.Context, sessionID string, items []MemoryItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		s.items = make(map[string]*MemoryItem)
	} else {
		for id, item := range s.items {
			if item != nil && item.SessionID == sessionID {
				delete(s.items, id)
			}
		}
	}

	if items == nil {
		return nil
	}
	for i := range items {
		item := items[i]
		if strings.TrimSpace(item.ID) == "" {
			item.ID = uuid.New().String()
		}
		if sessionID != "" {
			item.SessionID = sessionID
		}
		if item.Status == "" {
			item.Status = MemoryStatusActive
		}
		if item.Type == "" {
			item.Type = MemoryTypeSTM
		}
		copied := item
		s.items[copied.ID] = &copied
	}
	return nil
}

var defaultMemoryService IMemoryService

// GetMemoryService 获取记忆服务实例
func GetMemoryService() IMemoryService {
	if defaultMemoryService == nil {
		defaultMemoryService = NewInMemoryService()
	}
	return defaultMemoryService
}

// SetMemoryService 设置记忆服务实例 (用于测试)
func SetMemoryService(svc IMemoryService) {
	defaultMemoryService = svc
}
