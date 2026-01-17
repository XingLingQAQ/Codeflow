package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteVectorStore SQLite 向量存储实现
type SQLiteVectorStore struct {
	db                *sql.DB
	config            VectorStoreConfig
	embeddingProvider IEmbeddingProvider
	initialized       bool
	mu                sync.RWMutex
}

// NewSQLiteVectorStore 创建 SQLite 向量存储
func NewSQLiteVectorStore(config *VectorStoreConfig, embeddingProvider IEmbeddingProvider) *SQLiteVectorStore {
	cfg := DefaultConfig
	if config != nil {
		if config.CollectionName != "" {
			cfg.CollectionName = config.CollectionName
		}
		if config.DBPath != "" {
			cfg.DBPath = config.DBPath
		}
		if config.ChunkSize > 0 {
			cfg.ChunkSize = config.ChunkSize
		}
		if config.ChunkOverlap >= 0 {
			cfg.ChunkOverlap = config.ChunkOverlap
		}
		cfg.WALMode = config.WALMode
	}

	if embeddingProvider == nil {
		embeddingProvider = NewSimpleEmbeddingProvider(384)
	}

	return &SQLiteVectorStore{
		config:            cfg,
		embeddingProvider: embeddingProvider,
	}
}

// Initialize 初始化数据库
func (s *SQLiteVectorStore) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return nil
	}

	// 确保目录存在
	dbDir := filepath.Dir(s.config.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	// 打开数据库
	db, err := sql.Open("sqlite3", s.config.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	s.db = db

	// 启用 WAL 模式
	if s.config.WALMode {
		if _, err := s.db.Exec("PRAGMA journal_mode = WAL"); err != nil {
			return fmt.Errorf("enable WAL: %w", err)
		}
	}

	// 创建表结构
	if err := s.createTables(); err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	s.initialized = true
	return nil
}

// Add 添加文档块
func (s *SQLiteVectorStore) Add(ctx context.Context, chunks []DocumentChunk) error {
	if err := s.ensureInitialized(); err != nil {
		return err
	}

	if len(chunks) == 0 {
		return nil
	}

	// 获取文本
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	// 批量生成 embedding
	embeddings, err := s.embeddingProvider.EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed batch: %w", err)
	}

	// 事务插入
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO vectors (
			id, content, embedding, session_id, agent_role, git_commit_hash,
			message_index, chunk_index, timestamp, source, collection_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()

	for i, chunk := range chunks {
		embeddingJSON, err := json.Marshal(embeddings[i])
		if err != nil {
			return fmt.Errorf("marshal embedding: %w", err)
		}

		gitCommitHash := sql.NullString{
			String: chunk.Metadata.GitCommitHash,
			Valid:  chunk.Metadata.GitCommitHash != "",
		}

		_, err = stmt.ExecContext(ctx,
			chunk.ID,
			chunk.Content,
			string(embeddingJSON),
			chunk.Metadata.SessionID,
			chunk.Metadata.AgentRole,
			gitCommitHash,
			chunk.Metadata.MessageIndex,
			chunk.Metadata.ChunkIndex,
			chunk.Metadata.Timestamp,
			string(chunk.Metadata.Source),
			s.config.CollectionName,
		)
		if err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
	}

	return tx.Commit()
}

// Search 搜索相似文档
func (s *SQLiteVectorStore) Search(ctx context.Context, query string, opts *VectorSearchOptions) ([]VectorSearchResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	topK := 10
	minScore := 0.0
	if opts != nil {
		if opts.TopK > 0 {
			topK = opts.TopK
		}
		if opts.MinScore > 0 {
			minScore = opts.MinScore
		}
	}

	// 生成查询向量
	queryEmbedding, err := s.embeddingProvider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// 构建查询
	whereClause := "WHERE collection_name = ?"
	params := []interface{}{s.config.CollectionName}

	if opts != nil {
		if opts.FilterSessionID != "" {
			whereClause += " AND session_id = ?"
			params = append(params, opts.FilterSessionID)
		}
		if opts.FilterAgentRole != "" {
			whereClause += " AND agent_role = ?"
			params = append(params, opts.FilterAgentRole)
		}
		if opts.FilterGitCommit != "" {
			whereClause += " AND git_commit_hash = ?"
			params = append(params, opts.FilterGitCommit)
		}
		if opts.FilterSource != "" {
			whereClause += " AND source = ?"
			params = append(params, string(opts.FilterSource))
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, embedding, session_id, agent_role, git_commit_hash,
		       message_index, chunk_index, timestamp, source
		FROM vectors
		`+whereClause, params...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var (
			id            string
			content       string
			embeddingJSON string
			sessionID     string
			agentRole     string
			gitCommitHash sql.NullString
			messageIndex  int
			chunkIndex    int
			timestamp     int64
			source        string
		)

		if err := rows.Scan(&id, &content, &embeddingJSON, &sessionID, &agentRole,
			&gitCommitHash, &messageIndex, &chunkIndex, &timestamp, &source); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		var embedding []float64
		if err := json.Unmarshal([]byte(embeddingJSON), &embedding); err != nil {
			return nil, fmt.Errorf("unmarshal embedding: %w", err)
		}

		// 计算余弦相似度
		distance := cosineSimilarity(queryEmbedding, embedding)
		score := (distance + 1) / 2 // 转换为 0-1 范围

		if score >= minScore {
			chunk := DocumentChunk{
				ID:      id,
				Content: content,
				Metadata: ChunkMetadata{
					SessionID:     sessionID,
					AgentRole:     agentRole,
					GitCommitHash: gitCommitHash.String,
					MessageIndex:  messageIndex,
					ChunkIndex:    chunkIndex,
					Timestamp:     timestamp,
					Source:        SourceType(source),
				},
			}

			if opts != nil && opts.IncludeEmbeddings {
				chunk.Embedding = embedding
			}

			results = append(results, VectorSearchResult{
				Chunk:    chunk,
				Score:    score,
				Distance: distance,
			})
		}
	}

	// 按分数排序
	sortByScore(results)

	// 返回 topK
	if len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// Delete 删除文档
func (s *SQLiteVectorStore) Delete(ctx context.Context, ids []string) error {
	if err := s.ensureInitialized(); err != nil {
		return err
	}

	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	params := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		params[i] = id
	}

	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM vectors WHERE id IN (%s)", strings.Join(placeholders, ",")),
		params...)
	return err
}

// Clear 清空集合
func (s *SQLiteVectorStore) Clear(ctx context.Context) error {
	if err := s.ensureInitialized(); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, "DELETE FROM vectors WHERE collection_name = ?", s.config.CollectionName)
	return err
}

// GetBySessionID 按会话 ID 获取文档
func (s *SQLiteVectorStore) GetBySessionID(ctx context.Context, sessionID string) ([]DocumentChunk, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, session_id, agent_role, git_commit_hash,
		       message_index, chunk_index, timestamp, source
		FROM vectors
		WHERE collection_name = ? AND session_id = ?
		ORDER BY timestamp ASC
	`, s.config.CollectionName, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanChunks(rows)
}

// GetByGitCommit 按 Git 提交获取文档
func (s *SQLiteVectorStore) GetByGitCommit(ctx context.Context, commitHash string) ([]DocumentChunk, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, session_id, agent_role, git_commit_hash,
		       message_index, chunk_index, timestamp, source
		FROM vectors
		WHERE collection_name = ? AND git_commit_hash = ?
		ORDER BY timestamp ASC
	`, s.config.CollectionName, commitHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanChunks(rows)
}

// Count 获取文档数量
func (s *SQLiteVectorStore) Count(ctx context.Context) (int, error) {
	if err := s.ensureInitialized(); err != nil {
		return 0, err
	}

	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM vectors WHERE collection_name = ?",
		s.config.CollectionName).Scan(&count)
	return count, err
}

// GetCollectionInfo 获取集合信息
func (s *SQLiteVectorStore) GetCollectionInfo(ctx context.Context) (*CollectionInfo, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	count, err := s.Count(ctx)
	if err != nil {
		return nil, err
	}

	return &CollectionInfo{
		Name:      s.config.CollectionName,
		Count:     count,
		Dimension: s.embeddingProvider.GetDimension(),
		Metadata: map[string]interface{}{
			"dbPath":  s.config.DBPath,
			"walMode": s.config.WALMode,
		},
	}, nil
}

// Close 关闭数据库连接
func (s *SQLiteVectorStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		s.initialized = false
		return err
	}
	return nil
}

// GetStats 获取数据库统计信息
func (s *SQLiteVectorStore) GetStats(ctx context.Context) (*StoreStats, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	var totalVectors int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vectors").Scan(&totalVectors); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT collection_name FROM vectors")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var collections []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		collections = append(collections, name)
	}

	var dbSizeBytes int64
	if info, err := os.Stat(s.config.DBPath); err == nil {
		dbSizeBytes = info.Size()
	}

	return &StoreStats{
		TotalVectors: totalVectors,
		Collections:  collections,
		DBSizeBytes:  dbSizeBytes,
	}, nil
}

// StoreStats 存储统计信息
type StoreStats struct {
	TotalVectors int
	Collections  []string
	DBSizeBytes  int64
}

// ensureInitialized 确保已初始化
func (s *SQLiteVectorStore) ensureInitialized() error {
	if !s.initialized {
		return s.Initialize()
	}
	return nil
}

// createTables 创建表结构
func (s *SQLiteVectorStore) createTables() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS vectors (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			embedding TEXT NOT NULL,
			session_id TEXT NOT NULL,
			agent_role TEXT NOT NULL,
			git_commit_hash TEXT,
			message_index INTEGER NOT NULL,
			chunk_index INTEGER NOT NULL,
			timestamp INTEGER NOT NULL,
			source TEXT NOT NULL,
			collection_name TEXT NOT NULL,
			created_at INTEGER DEFAULT (strftime('%s', 'now'))
		);

		CREATE INDEX IF NOT EXISTS idx_vectors_collection ON vectors(collection_name);
		CREATE INDEX IF NOT EXISTS idx_vectors_session ON vectors(session_id);
		CREATE INDEX IF NOT EXISTS idx_vectors_git_commit ON vectors(git_commit_hash);
		CREATE INDEX IF NOT EXISTS idx_vectors_timestamp ON vectors(timestamp);
	`)
	return err
}

// scanChunks 扫描行为文档块
func (s *SQLiteVectorStore) scanChunks(rows *sql.Rows) ([]DocumentChunk, error) {
	var chunks []DocumentChunk
	for rows.Next() {
		var (
			id            string
			content       string
			sessionID     string
			agentRole     string
			gitCommitHash sql.NullString
			messageIndex  int
			chunkIndex    int
			timestamp     int64
			source        string
		)

		if err := rows.Scan(&id, &content, &sessionID, &agentRole,
			&gitCommitHash, &messageIndex, &chunkIndex, &timestamp, &source); err != nil {
			return nil, err
		}

		chunks = append(chunks, DocumentChunk{
			ID:      id,
			Content: content,
			Metadata: ChunkMetadata{
				SessionID:     sessionID,
				AgentRole:     agentRole,
				GitCommitHash: gitCommitHash.String,
				MessageIndex:  messageIndex,
				ChunkIndex:    chunkIndex,
				Timestamp:     timestamp,
				Source:        SourceType(source),
			},
		})
	}
	return chunks, rows.Err()
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	magnitude := math.Sqrt(normA) * math.Sqrt(normB)
	if magnitude == 0 {
		return 0
	}
	return dotProduct / magnitude
}

// sortByScore 按分数排序（降序）
func sortByScore(results []VectorSearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

// CreateSQLiteVectorStore 创建并初始化 SQLite 向量存储
func CreateSQLiteVectorStore(config *VectorStoreConfig, embeddingProvider IEmbeddingProvider) (*SQLiteVectorStore, error) {
	store := NewSQLiteVectorStore(config, embeddingProvider)
	if err := store.Initialize(); err != nil {
		return nil, err
	}
	return store, nil
}
