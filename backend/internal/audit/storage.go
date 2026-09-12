package audit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileAuditStorage 文件审计存储实现
type FileAuditStorage struct {
	config      FileStorageConfig
	currentFile string
	writeBuffer []*AuditLogEntry
	entryIndex  map[string]entryLocation
	lastEntry   *AuditLogEntry
	anchorHash  string
	initialized bool
	mu          sync.RWMutex
	flushTicker *time.Ticker
	done        chan struct{}
}

type entryLocation struct {
	File string
	Line int
}

type chainManifest struct {
	Version      int    `json:"version"`
	AnchorHash   string `json:"anchor_hash"`
	RotatedAt    int64  `json:"rotated_at,omitempty"`
	RemovedFiles int    `json:"removed_files,omitempty"`
}

// NewFileAuditStorage 创建文件审计存储
func NewFileAuditStorage(config *FileStorageConfig) *FileAuditStorage {
	cfg := DefaultFileStorageConfig
	if config != nil {
		if config.LogDir != "" {
			cfg.LogDir = config.LogDir
		}
		if config.FilePrefix != "" {
			cfg.FilePrefix = config.FilePrefix
		}
		if config.MaxFileSize > 0 {
			cfg.MaxFileSize = config.MaxFileSize
		}
		if config.MaxFiles > 0 {
			cfg.MaxFiles = config.MaxFiles
		}
		cfg.VerifyOnStartup = config.VerifyOnStartup
		if config.FlushInterval > 0 {
			cfg.FlushInterval = config.FlushInterval
		}
	}

	return &FileAuditStorage{
		config:      cfg,
		writeBuffer: make([]*AuditLogEntry, 0),
		entryIndex:  make(map[string]entryLocation),
	}
}

// Initialize 初始化存储
func (s *FileAuditStorage) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return nil
	}

	// 确保目录存在
	if err := os.MkdirAll(s.config.LogDir, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	if err := s.loadChainManifest(); err != nil {
		return fmt.Errorf("load chain manifest: %w", err)
	}

	// 获取或创建当前日志文件
	currentFile, err := s.getCurrentLogFile()
	if err != nil {
		return fmt.Errorf("get current log file: %w", err)
	}
	s.currentFile = currentFile

	// 构建索引
	if err := s.buildIndex(); err != nil {
		return fmt.Errorf("build index: %w", err)
	}

	// 验证哈希链完整性
	if s.config.VerifyOnStartup {
		result, err := s.verifyHashChainInternal()
		if err != nil {
			return fmt.Errorf("verify hash chain: %w", err)
		}
		if !result.Valid {
			return fmt.Errorf("hash chain integrity verification failed at %s", result.BrokenChainAt)
		}
	}

	// 启动定时刷新
	s.startFlushTimer()

	s.initialized = true
	return nil
}

// Append 追加日志条目
func (s *FileAuditStorage) Append(_ context.Context, entry *AuditLogEntry) error {
	if err := s.ensureInitialized(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.writeBuffer = append(s.writeBuffer, entry)
	s.lastEntry = entry

	// 如果缓冲区较大，立即刷新
	if len(s.writeBuffer) >= 100 {
		return s.flushLocked()
	}

	return nil
}

// Get 获取单条日志
func (s *FileAuditStorage) Get(_ context.Context, id string) (*AuditLogEntry, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// 先检查写缓冲区
	for _, e := range s.writeBuffer {
		if e.ID == id {
			return e, nil
		}
	}

	// 从索引查找
	location, ok := s.entryIndex[id]
	if !ok {
		return nil, nil
	}

	return s.readEntryFromFile(location.File, location.Line)
}

// Query 查询日志
func (s *FileAuditStorage) Query(ctx context.Context, query *AuditQuery) ([]AuditLogEntry, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}
	if query == nil {
		query = &AuditQuery{}
	}

	// 刷新缓冲区
	s.mu.Lock()
	if err := s.flushLocked(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []AuditLogEntry
	files, err := s.getLogFiles()
	if err != nil {
		return nil, err
	}

	// 从最新文件开始读取
	for i := len(files) - 1; i >= 0; i-- {
		entries, err := s.readEntriesFromFile(files[i])
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if s.matchesQuery(&entry, query) {
				results = append(results, entry)
			}

			// 检查是否达到限制
			if query.Limit > 0 && len(results) >= query.Limit+query.Offset {
				break
			}
		}

		if query.Limit > 0 && len(results) >= query.Limit+query.Offset {
			break
		}
	}

	// 按时间排序（最新在前）
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp > results[j].Timestamp
	})

	// 应用分页
	offset := query.Offset
	if offset > len(results) {
		offset = len(results)
	}

	limit := query.Limit
	if limit <= 0 || offset+limit > len(results) {
		limit = len(results) - offset
	}

	return results[offset : offset+limit], nil
}

// Count 计数
func (s *FileAuditStorage) Count(_ context.Context, query *AuditQuery) (int, error) {
	if err := s.ensureInitialized(); err != nil {
		return 0, err
	}

	s.mu.Lock()
	if err := s.flushLocked(); err != nil {
		s.mu.Unlock()
		return 0, err
	}
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	if query == nil {
		return len(s.entryIndex), nil
	}

	// 需要遍历所有条目进行计数
	count := 0
	files, err := s.getLogFiles()
	if err != nil {
		return 0, err
	}

	for _, file := range files {
		entries, err := s.readEntriesFromFile(file)
		if err != nil {
			return 0, err
		}
		for _, entry := range entries {
			if s.matchesQuery(&entry, query) {
				count++
			}
		}
	}

	return count, nil
}

// GetLastEntry 获取最后一条日志
func (s *FileAuditStorage) GetLastEntry(_ context.Context) (*AuditLogEntry, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// 优先返回缓冲区中的最后一条
	if len(s.writeBuffer) > 0 {
		return s.writeBuffer[len(s.writeBuffer)-1], nil
	}

	return s.lastEntry, nil
}

// Delete 删除日志（不支持物理删除）
func (s *FileAuditStorage) Delete(_ context.Context, ids []string) (int, error) {
	// 文件存储不支持物理删除（会破坏哈希链）
	return 0, fmt.Errorf("delete not supported: would break hash chain")
}

// Clear 清空存储
func (s *FileAuditStorage) Clear(_ context.Context) error {
	if err := s.ensureInitialized(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 停止刷新定时器
	s.stopFlushTimer()

	// 清空缓冲区
	s.writeBuffer = s.writeBuffer[:0]
	s.entryIndex = make(map[string]entryLocation)
	s.lastEntry = nil
	s.anchorHash = GenesisHash

	// 删除所有日志文件
	files, _ := s.getLogFiles()
	for _, file := range files {
		os.Remove(file)
	}

	// 重新创建当前文件
	var err error
	s.currentFile, err = s.createNewLogFile()
	if err != nil {
		return err
	}

	// 重启刷新定时器
	s.startFlushTimer()

	return nil
}

// VerifyHashChain 验证哈希链完整性
func (s *FileAuditStorage) VerifyHashChain(_ context.Context) (*IntegrityVerificationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.flushLocked(); err != nil {
		return nil, err
	}

	return s.verifyHashChainInternal()
}

// Close 关闭存储
func (s *FileAuditStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stopFlushTimer()
	if err := s.flushLocked(); err != nil {
		return err
	}
	s.initialized = false
	return nil
}

// GetStorageStats 获取存储统计
func (s *FileAuditStorage) GetStorageStats() (*StorageStats, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.getLogFiles()
	if err != nil {
		return nil, err
	}

	var totalSize int64
	for _, file := range files {
		if stat, err := os.Stat(file); err == nil {
			totalSize += stat.Size()
		}
	}

	var currentFileSize int64
	if stat, err := os.Stat(s.currentFile); err == nil {
		currentFileSize = stat.Size()
	}

	return &StorageStats{
		TotalFiles:      len(files),
		TotalSize:       totalSize,
		TotalEntries:    len(s.entryIndex) + len(s.writeBuffer),
		CurrentFileSize: currentFileSize,
	}, nil
}

// StorageStats 存储统计
type StorageStats struct {
	TotalFiles      int
	TotalSize       int64
	TotalEntries    int
	CurrentFileSize int64
}

// ensureInitialized 确保已初始化
func (s *FileAuditStorage) ensureInitialized() error {
	if !s.initialized {
		return s.Initialize()
	}
	return nil
}

func (s *FileAuditStorage) manifestPath() string {
	return filepath.Join(s.config.LogDir, s.config.FilePrefix+"_chain.json")
}

func (s *FileAuditStorage) loadChainManifest() error {
	s.anchorHash = GenesisHash
	raw, err := os.ReadFile(s.manifestPath())
	if os.IsNotExist(err) {
		return s.persistChainManifest(0)
	}
	if err != nil {
		return err
	}
	var manifest chainManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("decode chain manifest: %w", err)
	}
	if manifest.Version != 1 || len(manifest.AnchorHash) != 64 {
		return fmt.Errorf("invalid chain manifest")
	}
	s.anchorHash = manifest.AnchorHash
	return nil
}

func (s *FileAuditStorage) persistChainManifest(removed int) error {
	manifest := chainManifest{Version: 1, AnchorHash: s.anchorHash, RotatedAt: time.Now().UnixMilli(), RemovedFiles: removed}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.manifestPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// getCurrentLogFile 获取当前日志文件
func (s *FileAuditStorage) getCurrentLogFile() (string, error) {
	files, err := s.getLogFiles()
	if err != nil {
		return "", err
	}

	if len(files) == 0 {
		return s.createNewLogFile()
	}

	latestFile := files[len(files)-1]
	stat, err := os.Stat(latestFile)
	if err != nil {
		return s.createNewLogFile()
	}

	if stat.Size() >= s.config.MaxFileSize {
		return s.createNewLogFile()
	}

	return latestFile, nil
}

// createNewLogFile 创建新日志文件
func (s *FileAuditStorage) createNewLogFile() (string, error) {
	timestamp := time.Now().Format("2006-01-02T15-04-05.000000000")
	filename := fmt.Sprintf("%s_%s.jsonl", s.config.FilePrefix, timestamp)
	filepath := filepath.Join(s.config.LogDir, filename)

	if err := os.WriteFile(filepath, []byte{}, 0644); err != nil {
		return "", err
	}

	// 检查是否需要轮转
	if err := s.rotateIfNeeded(); err != nil {
		return "", err
	}

	return filepath, nil
}

// getLogFiles 获取所有日志文件
func (s *FileAuditStorage) getLogFiles() ([]string, error) {
	entries, err := os.ReadDir(s.config.LogDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() &&
			strings.HasPrefix(entry.Name(), s.config.FilePrefix) &&
			strings.HasSuffix(entry.Name(), ".jsonl") {
			files = append(files, filepath.Join(s.config.LogDir, entry.Name()))
		}
	}

	sort.Strings(files)
	return files, nil
}

// rotateIfNeeded 按需轮转日志
func (s *FileAuditStorage) rotateIfNeeded() error {
	files, err := s.getLogFiles()
	if err != nil {
		return err
	}

	if len(files) > s.config.MaxFiles {
		toDelete := files[:len(files)-s.config.MaxFiles]

		for _, file := range toDelete {
			entries, err := s.readEntriesFromFile(file)
			if err != nil {
				return fmt.Errorf("read rotated audit file: %w", err)
			}
			for i := range entries {
				s.anchorHash = entries[i].Hash
			}
			if err := os.Remove(file); err != nil {
				return fmt.Errorf("remove rotated audit file: %w", err)
			}

			// 从索引中移除
			for id, location := range s.entryIndex {
				if location.File == file {
					delete(s.entryIndex, id)
				}
			}
		}
		if err := s.persistChainManifest(len(toDelete)); err != nil {
			return err
		}
	}
	if err := s.persistChainManifest(0); err != nil {
		return err
	}
	return nil
}

// buildIndex 构建索引
func (s *FileAuditStorage) buildIndex() error {
	files, err := s.getLogFiles()
	if err != nil {
		return err
	}

	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			return err
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
		lineNumber := 0

		for scanner.Scan() {
			line := scanner.Text()
			if line = strings.TrimSpace(line); line != "" {
				var entry AuditLogEntry
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					f.Close()
					return fmt.Errorf("decode %s line %d: %w", filepath.Base(file), lineNumber+1, err)
				}
				if entry.ID == "" || entry.Hash == "" {
					f.Close()
					return fmt.Errorf("invalid audit entry in %s line %d", filepath.Base(file), lineNumber+1)
				}
				s.entryIndex[entry.ID] = entryLocation{File: file, Line: lineNumber}
				s.lastEntry = &entry
			}
			lineNumber++
		}

		if err := scanner.Err(); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}

	return nil
}

// readEntryFromFile 从文件读取单条记录
func (s *FileAuditStorage) readEntryFromFile(file string, lineNumber int) (*AuditLogEntry, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	currentLine := 0

	for scanner.Scan() {
		if currentLine == lineNumber {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				var entry AuditLogEntry
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					return nil, err
				}
				return &entry, nil
			}
			return nil, nil
		}
		currentLine++
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

// readEntriesFromFile 从文件读取所有记录
func (s *FileAuditStorage) readEntriesFromFile(file string) ([]AuditLogEntry, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []AuditLogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			var entry AuditLogEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}

	return entries, scanner.Err()
}

// matchesQuery 检查条目是否匹配查询
func (s *FileAuditStorage) matchesQuery(entry *AuditLogEntry, query *AuditQuery) bool {
	if query == nil {
		return true
	}

	if query.StartTime > 0 && entry.Timestamp < query.StartTime {
		return false
	}
	if query.EndTime > 0 && entry.Timestamp > query.EndTime {
		return false
	}
	if len(query.EventTypes) > 0 && !containsEventType(query.EventTypes, entry.EventType) {
		return false
	}
	if len(query.Severities) > 0 && !containsSeverity(query.Severities, entry.Severity) {
		return false
	}
	if query.ActorID != "" && entry.Actor.ID != query.ActorID {
		return false
	}
	if query.ResourceID != "" && entry.Resource.ID != query.ResourceID {
		return false
	}
	if query.ResourceType != "" && entry.Resource.Type != query.ResourceType {
		return false
	}
	if query.Outcome != "" && entry.Outcome != query.Outcome {
		return false
	}

	return true
}

// flushLocked 刷新缓冲区（必须持有锁）
func (s *FileAuditStorage) flushLocked() error {
	if len(s.writeBuffer) == 0 {
		return nil
	}

	// 检查是否需要轮转
	if stat, err := os.Stat(s.currentFile); err == nil {
		if stat.Size() >= s.config.MaxFileSize {
			newFile, err := s.createNewLogFile()
			if err != nil {
				return err
			}
			s.currentFile = newFile
		}
	}

	// 写入缓冲区内容
	f, err := os.OpenFile(s.currentFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// 获取当前行数
	lineCount, err := s.countLines(s.currentFile)
	if err != nil {
		return err
	}

	for i, entry := range s.writeBuffer {
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err := f.WriteString(string(data) + "\n"); err != nil {
			return err
		}

		// 更新索引
		s.entryIndex[entry.ID] = entryLocation{File: s.currentFile, Line: lineCount + i}
	}
	if err := f.Sync(); err != nil {
		return err
	}

	// 清空缓冲区
	s.writeBuffer = s.writeBuffer[:0]

	return nil
}

// countLines 计算文件行数
func (s *FileAuditStorage) countLines(file string) (int, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count, nil
}

// startFlushTimer 启动刷新定时器
func (s *FileAuditStorage) startFlushTimer() {
	if s.flushTicker != nil {
		return
	}

	s.done = make(chan struct{})
	s.flushTicker = time.NewTicker(time.Duration(s.config.FlushInterval) * time.Millisecond)
	ticker := s.flushTicker
	done := s.done

	go func() {
		for {
			select {
			case <-ticker.C:
				s.mu.Lock()
				s.flushLocked()
				s.mu.Unlock()
			case <-done:
				return
			}
		}
	}()
}

// stopFlushTimer 停止刷新定时器
func (s *FileAuditStorage) stopFlushTimer() {
	if s.flushTicker != nil {
		s.flushTicker.Stop()
		s.flushTicker = nil
	}
	if s.done != nil {
		close(s.done)
		s.done = nil
	}
}

// verifyHashChainInternal 内部哈希链验证
func (s *FileAuditStorage) verifyHashChainInternal() (*IntegrityVerificationResult, error) {
	files, err := s.getLogFiles()
	if err != nil {
		return nil, err
	}

	result := &IntegrityVerificationResult{
		Valid:          true,
		InvalidEntries: []string{},
		VerifiedAt:     time.Now().UnixMilli(),
	}

	expectedPreviousHash := s.anchorHash

	for _, file := range files {
		entries, err := s.readEntriesFromFile(file)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			result.CheckedEntries++

			// 验证链接
			if entry.PreviousHash != expectedPreviousHash {
				result.Valid = false
				result.BrokenChainAt = entry.ID
				result.InvalidEntries = append(result.InvalidEntries, entry.ID)
			}

			// 验证哈希
			calculatedHash := CalculateEntryHash(&entry)
			if calculatedHash != entry.Hash {
				result.Valid = false
				result.InvalidEntries = append(result.InvalidEntries, entry.ID)
			}

			expectedPreviousHash = entry.Hash
		}
	}

	return result, nil
}

// CalculateEntryHash 计算条目哈希
func CalculateEntryHash(entry *AuditLogEntry) string {
	data := map[string]interface{}{
		"id":            entry.ID,
		"timestamp":     entry.Timestamp,
		"event_type":    entry.EventType,
		"severity":      entry.Severity,
		"actor":         entry.Actor,
		"resource":      entry.Resource,
		"action":        entry.Action,
		"outcome":       entry.Outcome,
		"trace":         entry.Trace,
		"details":       entry.Details,
		"previous_hash": entry.PreviousHash,
	}

	jsonData, _ := json.Marshal(data)
	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:])
}

// containsEventType 检查事件类型是否在列表中
func containsEventType(types []AuditEventType, t AuditEventType) bool {
	for _, typ := range types {
		if typ == t {
			return true
		}
	}
	return false
}

// containsSeverity 检查严重级别是否在列表中
func containsSeverity(severities []AuditSeverity, s AuditSeverity) bool {
	for _, sev := range severities {
		if sev == s {
			return true
		}
	}
	return false
}

// CreateFileAuditStorage 创建并初始化文件审计存储
func CreateFileAuditStorage(config *FileStorageConfig) (*FileAuditStorage, error) {
	storage := NewFileAuditStorage(config)
	if err := storage.Initialize(); err != nil {
		return nil, err
	}
	return storage, nil
}
