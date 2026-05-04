package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	// ErrAtomicMemoryNotFound 原子记忆不存在。
	ErrAtomicMemoryNotFound = errors.New("atomic memory not found")
)

// AtomicMemoryUpdate 原子记忆更新参数。
type AtomicMemoryUpdate struct {
	Timestamp     *int64
	Content       *string
	Tags          *[]string
	SessionID     *string
	FolderID      *string
	ClearFolderID bool
	Source        *AtomicMemorySource
	Importance    *float64
	Embedding     *[]float64
	Tier          *MemoryTier
	Heat          *float64
	Surprise      *float64
}

// AtomicMemoryService 原子记忆服务。
type AtomicMemoryService struct {
	db                *sql.DB
	vectorStore       IVectorStore
	embeddingProvider IEmbeddingProvider
	agentRole         string
}

// NewAtomicMemoryService 创建原子记忆服务。
func NewAtomicMemoryService(ctx context.Context, db *sql.DB, vectorStore IVectorStore, embeddingProvider IEmbeddingProvider) (*AtomicMemoryService, error) {
	if db == nil {
		return nil, errors.New("atomic memory service init failed: db is nil")
	}
	if vectorStore == nil {
		return nil, errors.New("atomic memory service init failed: vector store is nil")
	}
	if embeddingProvider == nil {
		embeddingProvider = NewSimpleEmbeddingProvider(384)
	}

	ctx = ensureContext(ctx)
	if err := EnsureAtomicMemorySchema(ctx, db); err != nil {
		return nil, err
	}

	return &AtomicMemoryService{
		db:                db,
		vectorStore:       vectorStore,
		embeddingProvider: embeddingProvider,
		agentRole:         "atomic_memory",
	}, nil
}

// Add 添加原子记忆（SQLite + 向量库）。
func (s *AtomicMemoryService) Add(ctx context.Context, mem *AtomicMemory) error {
	if err := s.validateDependencies(); err != nil {
		return err
	}
	if mem == nil {
		return errors.New("add atomic memory: memory is nil")
	}

	ctx = ensureContext(ctx)
	if mem.Timestamp <= 0 {
		mem.Timestamp = time.Now().Unix()
	}
	if mem.Tier == "" {
		mem.Tier = MemoryTierHot
	}
	if mem.Heat <= 0 {
		mem.Heat = 1.0
	}
	if mem.Surprise <= 0 {
		mem.Surprise = 0.5
	}
	if len(mem.Embedding) == 0 {
		embedding, err := s.embeddingProvider.Embed(ctx, mem.Content)
		if err != nil {
			return fmt.Errorf("generate embedding: %w", err)
		}
		mem.Embedding = embedding
	}
	if err := mem.Validate(); err != nil {
		return fmt.Errorf("validate atomic memory: %w", err)
	}

	tagsJSON, err := mem.TagsJSON()
	if err != nil {
		return fmt.Errorf("encode tags json: %w", err)
	}
	embeddingJSON, err := mem.EmbeddingJSON()
	if err != nil {
		return fmt.Errorf("encode embedding json: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO atomic_memories (
			id, timestamp, content, tags_json, session_id, folder_id,
			source, importance, embedding_json, vector_dim, tier, heat, surprise, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'))
	`,
		mem.ID,
		mem.Timestamp,
		mem.Content,
		tagsJSON,
		mem.SessionID,
		nullableString(mem.FolderID),
		string(mem.Source),
		mem.Importance,
		embeddingJSON,
		len(mem.Embedding),
		string(mem.Tier),
		mem.Heat,
		mem.Surprise,
	)
	if err != nil {
		return fmt.Errorf("insert atomic memory: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit atomic memory: %w", err)
	}

	if err := s.indexVector(ctx, mem); err != nil {
		if cleanupErr := s.deleteAtomicMemoryRow(ctx, mem.ID); cleanupErr != nil {
			return fmt.Errorf("store vector index: %w; rollback atomic memory: %v", err, cleanupErr)
		}
		return fmt.Errorf("store vector index: %w", err)
	}

	return nil
}

// Search 语义检索原子记忆。
func (s *AtomicMemoryService) Search(ctx context.Context, query string, opts *AtomicMemorySearchOptions) ([]AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	ctx = ensureContext(ctx)

	query = strings.TrimSpace(query)
	if query == "" {
		return []AtomicMemory{}, nil
	}

	vectorOpts := &VectorSearchOptions{
		TopK:     10,
		MinScore: 0,
	}
	if opts != nil {
		if opts.Limit > 0 {
			vectorOpts.TopK = opts.Limit + opts.Offset
		}
		if opts.SessionID != "" {
			vectorOpts.FilterSessionID = opts.SessionID
		}
	}

	vectorResults, err := s.vectorStore.Search(ctx, query, vectorOpts)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	if len(vectorResults) == 0 {
		return []AtomicMemory{}, nil
	}

	orderedIDs := make([]string, 0, len(vectorResults))
	for _, result := range vectorResults {
		if result.Chunk.ID != "" {
			orderedIDs = append(orderedIDs, result.Chunk.ID)
		}
	}
	if len(orderedIDs) == 0 {
		return []AtomicMemory{}, nil
	}

	memoryByID, err := s.getByIDs(ctx, orderedIDs)
	if err != nil {
		return nil, err
	}

	filtered := make([]AtomicMemory, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		mem, ok := memoryByID[id]
		if !ok {
			continue
		}
		if !matchAtomicFilters(mem, opts) {
			continue
		}
		filtered = append(filtered, mem)
	}

	if opts != nil && opts.Offset > 0 {
		if opts.Offset >= len(filtered) {
			return []AtomicMemory{}, nil
		}
		filtered = filtered[opts.Offset:]
	}
	if opts != nil && opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}

	return filtered, nil
}

// SearchByTimeRange 按时间范围检索。
func (s *AtomicMemoryService) SearchByTimeRange(ctx context.Context, start, end int64) ([]AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	if start > end {
		return nil, errors.New("invalid time range: start is greater than end")
	}

	ctx = ensureContext(ctx)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query by time range: %w", err)
	}
	defer rows.Close()

	return scanAtomicMemories(rows)
}

// SearchByTags 按标签检索。
func (s *AtomicMemoryService) SearchByTags(ctx context.Context, tags []string) ([]AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	ctx = ensureContext(ctx)

	if len(tags) == 0 {
		return []AtomicMemory{}, nil
	}

	clauses := make([]string, 0, len(tags))
	params := make([]interface{}, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		clauses = append(clauses, `tags_json LIKE ?`)
		params = append(params, `%"`+tag+`"%`)
	}
	if len(clauses) == 0 {
		return []AtomicMemory{}, nil
	}

	query := `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE ` + strings.Join(clauses, " OR ") + `
		ORDER BY timestamp DESC
	`

	rows, err := s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("query by tags: %w", err)
	}
	defer rows.Close()

	return scanAtomicMemories(rows)
}

// GetBySession 获取会话下的原子记忆。
func (s *AtomicMemoryService) GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	ctx = ensureContext(ctx)

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE session_id = ?
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`, sessionID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query by session: %w", err)
	}
	defer rows.Close()

	return scanAtomicMemories(rows)
}

// GetByID 获取单条原子记忆。
func (s *AtomicMemoryService) GetByID(ctx context.Context, id string) (*AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	ctx = ensureContext(ctx)

	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE id = ?
	`, id)

	memory, err := scanAtomicMemoryRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAtomicMemoryNotFound
		}
		return nil, err
	}
	return &memory, nil
}

// Update 更新原子记忆并同步向量索引。
func (s *AtomicMemoryService) Update(ctx context.Context, id string, updates *AtomicMemoryUpdate) error {
	if err := s.validateDependencies(); err != nil {
		return err
	}
	if updates == nil {
		return errors.New("update atomic memory: updates is nil")
	}

	ctx = ensureContext(ctx)
	current, err := s.GetByID(ctx, id)
	if err != nil {
		return err
	}

	applyAtomicUpdates(current, updates)

	if updates.Content != nil && updates.Embedding == nil {
		embedding, embErr := s.embeddingProvider.Embed(ctx, current.Content)
		if embErr != nil {
			return fmt.Errorf("regenerate embedding: %w", embErr)
		}
		current.Embedding = embedding
	}

	if err := current.Validate(); err != nil {
		return fmt.Errorf("validate updated atomic memory: %w", err)
	}

	tagsJSON, err := current.TagsJSON()
	if err != nil {
		return fmt.Errorf("encode tags json: %w", err)
	}
	embeddingJSON, err := current.EmbeddingJSON()
	if err != nil {
		return fmt.Errorf("encode embedding json: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE atomic_memories
		SET timestamp = ?,
			content = ?,
			tags_json = ?,
			session_id = ?,
			folder_id = ?,
			source = ?,
			importance = ?,
			embedding_json = ?,
			vector_dim = ?,
			tier = ?,
			heat = ?,
			surprise = ?,
			updated_at = strftime('%s', 'now')
		WHERE id = ?
	`,
		current.Timestamp,
		current.Content,
		tagsJSON,
		current.SessionID,
		nullableString(current.FolderID),
		string(current.Source),
		current.Importance,
		embeddingJSON,
		len(current.Embedding),
		string(current.Tier),
		current.Heat,
		current.Surprise,
		current.ID,
	)
	if err != nil {
		return fmt.Errorf("update atomic memory: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if affected == 0 {
		return ErrAtomicMemoryNotFound
	}

	if err := s.replaceVector(ctx, current); err != nil {
		return fmt.Errorf("sync vector index: %w", err)
	}
	return nil
}

// Delete 删除原子记忆并移除向量索引。
func (s *AtomicMemoryService) Delete(ctx context.Context, id string) error {
	if err := s.validateDependencies(); err != nil {
		return err
	}
	ctx = ensureContext(ctx)

	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("id is required")
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM atomic_memories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete atomic memory: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if affected == 0 {
		return ErrAtomicMemoryNotFound
	}

	if err := s.vectorStore.Delete(ctx, []string{id}); err != nil {
		return fmt.Errorf("delete vector index: %w", err)
	}
	return nil
}

// ApplyHeatDecay 对所有原子记忆执行 Heat 衰减。
// Heat(t) = Heat(t-1) × 0.5^(Δt / HalfLife)
func (s *AtomicMemoryService) ApplyHeatDecay(ctx context.Context) (int, error) {
	if err := s.validateDependencies(); err != nil {
		return 0, err
	}
	ctx = ensureContext(ctx)

	halfLifeSeconds := float64(HeatHalfLifeDays * 24 * 3600)
	now := time.Now().Unix()

	// 读取所有 heat > 0.001 的记忆
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, heat, updated_at FROM atomic_memories WHERE heat > 0.001
	`)
	if err != nil {
		return 0, fmt.Errorf("query for decay: %w", err)
	}
	defer rows.Close()

	type decayItem struct {
		id      string
		newHeat float64
	}
	var items []decayItem

	for rows.Next() {
		var id string
		var heat float64
		var updatedAt int64
		if err := rows.Scan(&id, &heat, &updatedAt); err != nil {
			return 0, err
		}
		dt := float64(now - updatedAt)
		if dt <= 0 {
			continue
		}
		newHeat := heat * math.Pow(0.5, dt/halfLifeSeconds)
		if newHeat < 0.001 {
			newHeat = 0
		}
		items = append(items, decayItem{id: id, newHeat: newHeat})
	}

	if len(items) == 0 {
		return 0, nil
	}

	// 批量更新
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `UPDATE atomic_memories SET heat = ?, updated_at = ? WHERE id = ?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, item := range items {
		stmt.ExecContext(ctx, item.newHeat, now, item.id)
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return len(items), nil
}

// RecomputeTiers 根据 Heat 值重新分配层级。
// hot: heat >= 0.5, warm: 0.1 <= heat < 0.5, cold: heat < 0.1
func (s *AtomicMemoryService) RecomputeTiers(ctx context.Context) (int, error) {
	if err := s.validateDependencies(); err != nil {
		return 0, err
	}
	ctx = ensureContext(ctx)

	result, err := s.db.ExecContext(ctx, `
		UPDATE atomic_memories
		SET tier = CASE
			WHEN heat >= 0.5 THEN 'hot'
			WHEN heat >= 0.1 THEN 'warm'
			ELSE 'cold'
		END
		WHERE tier != CASE
			WHEN heat >= 0.5 THEN 'hot'
			WHEN heat >= 0.1 THEN 'warm'
			ELSE 'cold'
		END
	`)
	if err != nil {
		return 0, fmt.Errorf("recompute tiers: %w", err)
	}

	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// BoostHeat 提升指定记忆的 Heat（被访问时调用）。
func (s *AtomicMemoryService) BoostHeat(ctx context.Context, id string, boost float64) error {
	if err := s.validateDependencies(); err != nil {
		return err
	}
	if boost <= 0 {
		boost = 0.2
	}
	ctx = ensureContext(ctx)

	_, err := s.db.ExecContext(ctx, `
		UPDATE atomic_memories
		SET heat = MIN(1.0, heat + ?),
			tier = CASE
				WHEN MIN(1.0, heat + ?) >= 0.5 THEN 'hot'
				WHEN MIN(1.0, heat + ?) >= 0.1 THEN 'warm'
				ELSE 'cold'
			END,
			updated_at = strftime('%s', 'now')
		WHERE id = ?
	`, boost, boost, boost, id)
	return err
}

// SearchByTier 按层级检索。
func (s *AtomicMemoryService) SearchByTier(ctx context.Context, tier MemoryTier, limit int) ([]AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	ctx = ensureContext(ctx)
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE tier = ?
		ORDER BY heat DESC
		LIMIT ?
	`, string(tier), limit)
	if err != nil {
		return nil, fmt.Errorf("query by tier: %w", err)
	}
	defer rows.Close()

	return scanAtomicMemories(rows)
}

func (s *AtomicMemoryService) deleteAtomicMemoryRow(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM atomic_memories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return nil
}

func (s *AtomicMemoryService) validateDependencies() error {
	if s == nil {
		return errors.New("atomic memory service is nil")
	}
	if s.db == nil {
		return errors.New("atomic memory service db is nil")
	}
	if s.vectorStore == nil {
		return errors.New("atomic memory service vector store is nil")
	}
	if s.embeddingProvider == nil {
		return errors.New("atomic memory service embedding provider is nil")
	}
	return nil
}

func (s *AtomicMemoryService) indexVector(ctx context.Context, mem *AtomicMemory) error {
	chunk := buildAtomicMemoryChunk(mem, s.agentRole)
	return s.vectorStore.Add(ctx, []DocumentChunk{chunk})
}

func (s *AtomicMemoryService) replaceVector(ctx context.Context, mem *AtomicMemory) error {
	if err := s.vectorStore.Delete(ctx, []string{mem.ID}); err != nil {
		return err
	}
	return s.indexVector(ctx, mem)
}

func (s *AtomicMemoryService) getByIDs(ctx context.Context, orderedIDs []string) (map[string]AtomicMemory, error) {
	uniqueIDs := dedupeOrderedIDs(orderedIDs)
	if len(uniqueIDs) == 0 {
		return map[string]AtomicMemory{}, nil
	}

	placeholders := make([]string, len(uniqueIDs))
	params := make([]interface{}, len(uniqueIDs))
	for i, id := range uniqueIDs {
		placeholders[i] = "?"
		params[i] = id
	}

	query := `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE id IN (` + strings.Join(placeholders, ",") + `)
	`
	rows, err := s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("query atomic memories by ids: %w", err)
	}
	defer rows.Close()

	items, err := scanAtomicMemories(rows)
	if err != nil {
		return nil, err
	}

	memoryByID := make(map[string]AtomicMemory, len(items))
	for _, item := range items {
		memoryByID[item.ID] = item
	}
	return memoryByID, nil
}

func applyAtomicUpdates(current *AtomicMemory, updates *AtomicMemoryUpdate) {
	if updates.Timestamp != nil {
		current.Timestamp = *updates.Timestamp
	}
	if updates.Content != nil {
		current.Content = strings.TrimSpace(*updates.Content)
	}
	if updates.Tags != nil {
		current.Tags = *updates.Tags
	}
	if updates.SessionID != nil {
		current.SessionID = strings.TrimSpace(*updates.SessionID)
	}
	if updates.ClearFolderID {
		current.FolderID = nil
	} else if updates.FolderID != nil {
		folderID := strings.TrimSpace(*updates.FolderID)
		current.FolderID = &folderID
	}
	if updates.Source != nil {
		current.Source = *updates.Source
	}
	if updates.Importance != nil {
		current.Importance = *updates.Importance
	}
	if updates.Embedding != nil {
		current.Embedding = *updates.Embedding
	}
	if updates.Tier != nil {
		current.Tier = *updates.Tier
	}
	if updates.Heat != nil {
		current.Heat = *updates.Heat
	}
	if updates.Surprise != nil {
		current.Surprise = *updates.Surprise
	}
}

func matchAtomicFilters(mem AtomicMemory, opts *AtomicMemorySearchOptions) bool {
	if opts == nil {
		return true
	}
	if opts.FolderID != "" {
		if mem.FolderID == nil || *mem.FolderID != opts.FolderID {
			return false
		}
	}
	if len(opts.Tags) > 0 && !containsAnyTag(mem.Tags, opts.Tags) {
		return false
	}
	if opts.StartAt != nil && mem.Timestamp < *opts.StartAt {
		return false
	}
	if opts.EndAt != nil && mem.Timestamp > *opts.EndAt {
		return false
	}
	return true
}

func containsAnyTag(memoryTags []string, filterTags []string) bool {
	if len(filterTags) == 0 {
		return true
	}
	tagSet := make(map[string]struct{}, len(memoryTags))
	for _, tag := range memoryTags {
		t := strings.TrimSpace(tag)
		if t == "" {
			continue
		}
		tagSet[t] = struct{}{}
	}
	for _, tag := range filterTags {
		t := strings.TrimSpace(tag)
		if t == "" {
			continue
		}
		if _, ok := tagSet[t]; ok {
			return true
		}
	}
	return false
}

func scanAtomicMemories(rows *sql.Rows) ([]AtomicMemory, error) {
	items := make([]AtomicMemory, 0)
	for rows.Next() {
		item, err := scanAtomicMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanAtomicMemoryRow(scanner interface {
	Scan(dest ...interface{}) error
}) (AtomicMemory, error) {
	var (
		item          AtomicMemory
		tagsJSON      string
		folderID      sql.NullString
		source        string
		embeddingJSON sql.NullString
		tier          string
	)

	err := scanner.Scan(
		&item.ID,
		&item.Timestamp,
		&item.Content,
		&tagsJSON,
		&item.SessionID,
		&folderID,
		&source,
		&item.Importance,
		&embeddingJSON,
		&tier,
		&item.Heat,
		&item.Surprise,
	)
	if err != nil {
		return AtomicMemory{}, err
	}

	item.Source = AtomicMemorySource(source)
	item.Tier = MemoryTier(tier)
	if item.Tier == "" {
		item.Tier = MemoryTierHot
	}
	if folderID.Valid {
		v := folderID.String
		item.FolderID = &v
	}

	if err := json.Unmarshal([]byte(tagsJSON), &item.Tags); err != nil {
		return AtomicMemory{}, fmt.Errorf("unmarshal tags_json: %w", err)
	}
	if embeddingJSON.Valid && strings.TrimSpace(embeddingJSON.String) != "" {
		if err := json.Unmarshal([]byte(embeddingJSON.String), &item.Embedding); err != nil {
			return AtomicMemory{}, fmt.Errorf("unmarshal embedding_json: %w", err)
		}
	}

	return item, nil
}

func buildAtomicMemoryChunk(mem *AtomicMemory, agentRole string) DocumentChunk {
	return DocumentChunk{
		ID:      mem.ID,
		Content: mem.Content,
		Metadata: ChunkMetadata{
			SessionID:    mem.SessionID,
			AgentRole:    agentRole,
			MessageIndex: 0,
			ChunkIndex:   0,
			Timestamp:    mem.Timestamp,
			Source:       SourceType(mem.Source),
		},
	}
}

func dedupeOrderedIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	ordered := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	return ordered
}

func nullableString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	value := strings.TrimSpace(*v)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
