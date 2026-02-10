package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// FolderMemory 文件夹记忆（folderId 为必填）。
type FolderMemory struct {
	AtomicMemory
}

// FolderInfo 文件夹信息摘要。
type FolderInfo struct {
	FolderID        string `json:"folder_id"`
	MemoryCount     int    `json:"memory_count"`
	LatestTimestamp int64  `json:"latest_timestamp"`
}

// FolderMemoryService 文件夹级记忆隔离服务。
// 在 AtomicMemoryService 之上提供文件夹维度的记忆管理。
type FolderMemoryService struct {
	atomicService *AtomicMemoryService
}

// NewFolderMemoryService 创建文件夹记忆服务。
func NewFolderMemoryService(atomicService *AtomicMemoryService) (*FolderMemoryService, error) {
	if atomicService == nil {
		return nil, errors.New("folder memory service: atomic service is nil")
	}
	return &FolderMemoryService{atomicService: atomicService}, nil
}

// AddToFolder 向指定文件夹添加记忆。
func (s *FolderMemoryService) AddToFolder(ctx context.Context, folderId string, mem *AtomicMemory) error {
	folderId = strings.TrimSpace(folderId)
	if folderId == "" {
		return errors.New("folder_id is required")
	}
	if mem == nil {
		return errors.New("memory is nil")
	}

	mem.FolderID = &folderId
	return s.atomicService.Add(ctx, mem)
}

// SearchInFolder 在指定文件夹内搜索记忆。
func (s *FolderMemoryService) SearchInFolder(ctx context.Context, folderId string, query string, opts *AtomicMemorySearchOptions) ([]AtomicMemory, error) {
	folderId = strings.TrimSpace(folderId)
	if folderId == "" {
		return nil, errors.New("folder_id is required")
	}

	if opts == nil {
		opts = &AtomicMemorySearchOptions{}
	}
	opts.FolderID = folderId

	results, err := s.atomicService.Search(ctx, query, opts)
	if err != nil {
		return nil, fmt.Errorf("search in folder %s: %w", folderId, err)
	}

	// 二次过滤确保 folderId 匹配
	filtered := make([]AtomicMemory, 0, len(results))
	for _, mem := range results {
		if mem.FolderID != nil && *mem.FolderID == folderId {
			filtered = append(filtered, mem)
		}
	}

	return filtered, nil
}

// SearchAcrossFolders 跨文件夹搜索记忆（不限定 folderId）。
func (s *FolderMemoryService) SearchAcrossFolders(ctx context.Context, query string, opts *AtomicMemorySearchOptions) ([]AtomicMemory, error) {
	if opts == nil {
		opts = &AtomicMemorySearchOptions{}
	}
	// 不设置 FolderID，搜索所有文件夹
	opts.FolderID = ""

	return s.atomicService.Search(ctx, query, opts)
}

// ListFolders 列出指定会话中的所有文件夹。
func (s *FolderMemoryService) ListFolders(ctx context.Context, sessionID string, limit, offset int) ([]FolderInfo, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}

	memories, err := s.atomicService.GetBySession(ctx, sessionID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list folders for session %s: %w", sessionID, err)
	}

	folderMap := make(map[string]*FolderInfo)
	for _, mem := range memories {
		if mem.FolderID == nil {
			continue
		}
		fid := *mem.FolderID
		info, ok := folderMap[fid]
		if !ok {
			info = &FolderInfo{
				FolderID:        fid,
				MemoryCount:     0,
				LatestTimestamp: 0,
			}
			folderMap[fid] = info
		}
		info.MemoryCount++
		if mem.Timestamp > info.LatestTimestamp {
			info.LatestTimestamp = mem.Timestamp
		}
	}

	folders := make([]FolderInfo, 0, len(folderMap))
	for _, info := range folderMap {
		folders = append(folders, *info)
	}

	// 按最新时间戳降序排列
	for i := 0; i < len(folders); i++ {
		for j := i + 1; j < len(folders); j++ {
			if folders[j].LatestTimestamp > folders[i].LatestTimestamp {
				folders[i], folders[j] = folders[j], folders[i]
			}
		}
	}

	return folders, nil
}

// DeleteFolder 删除指定文件夹内的所有记忆。
func (s *FolderMemoryService) DeleteFolder(ctx context.Context, folderId string, sessionID string) (int, error) {
	folderId = strings.TrimSpace(folderId)
	if folderId == "" {
		return 0, errors.New("folder_id is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, errors.New("session_id is required")
	}

	memories, err := s.atomicService.GetBySession(ctx, sessionID, 1000, 0)
	if err != nil {
		return 0, fmt.Errorf("get folder memories: %w", err)
	}

	deleted := 0
	for _, mem := range memories {
		if mem.FolderID != nil && *mem.FolderID == folderId {
			if err := s.atomicService.Delete(ctx, mem.ID); err != nil {
				return deleted, fmt.Errorf("delete memory %s: %w", mem.ID, err)
			}
			deleted++
		}
	}

	return deleted, nil
}
