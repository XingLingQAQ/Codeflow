// Package samg - SQLite triple store implementation
package samg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteTripleStore SQLite三元组存储实现
type SQLiteTripleStore struct {
	config TripleStoreConfig
	db     *sql.DB
	mu     sync.RWMutex
}

// NewSQLiteTripleStore 创建SQLite三元组存储
func NewSQLiteTripleStore(dbPath string, config *TripleStoreConfig) (*SQLiteTripleStore, error) {
	cfg := DefaultTripleStoreConfig
	if config != nil {
		cfg = *config
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteTripleStore{
		config: cfg,
		db:     db,
	}

	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema 初始化数据库表结构
func (s *SQLiteTripleStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS triples (
		id TEXT PRIMARY KEY,
		subject_id TEXT NOT NULL,
		subject_type TEXT,
		subject_label TEXT,
		predicate TEXT NOT NULL,
		object_type TEXT NOT NULL,
		object_node_id TEXT,
		object_node_type TEXT,
		object_node_label TEXT,
		object_literal_value TEXT,
		object_literal_type TEXT,
		object_literal_lang TEXT,
		confidence REAL DEFAULT 1.0,
		timestamp INTEGER NOT NULL,
		source_session_id TEXT,
		source_message_index INTEGER,
		source_agent_role TEXT,
		source_git_hash TEXT,
		source_extraction_method TEXT,
		metadata TEXT,
		created_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_triples_subject ON triples(subject_id);
	CREATE INDEX IF NOT EXISTS idx_triples_predicate ON triples(predicate);
	CREATE INDEX IF NOT EXISTS idx_triples_object_node ON triples(object_node_id);
	CREATE INDEX IF NOT EXISTS idx_triples_confidence ON triples(confidence);
	CREATE INDEX IF NOT EXISTS idx_triples_timestamp ON triples(timestamp);

	CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		label TEXT,
		description TEXT,
		properties TEXT,
		aliases TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);
	CREATE INDEX IF NOT EXISTS idx_entities_label ON entities(label);

	CREATE TABLE IF NOT EXISTS graph_metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Close 关闭数据库连接
func (s *SQLiteTripleStore) Close() error {
	return s.db.Close()
}

// Add 添加三元组
func (s *SQLiteTripleStore) Add(ctx context.Context, triples []Triple) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO triples (
			id, subject_id, subject_type, subject_label,
			predicate, object_type, object_node_id, object_node_type, object_node_label,
			object_literal_value, object_literal_type, object_literal_lang,
			confidence, timestamp, source_session_id, source_message_index,
			source_agent_role, source_git_hash, source_extraction_method,
			metadata, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UnixMilli()

	for _, triple := range triples {
		subjectType := ""
		if len(triple.Subject.Type) > 0 {
			subjectType = triple.Subject.Type[0]
		}

		objectType := "node"
		var objectNodeID, objectNodeType, objectNodeLabel sql.NullString
		var objectLiteralValue, objectLiteralType, objectLiteralLang sql.NullString

		if triple.Object.IsLiteral() {
			objectType = "literal"
			if triple.Object.Literal != nil {
				valueBytes, _ := json.Marshal(triple.Object.Literal.Value)
				objectLiteralValue = sql.NullString{String: string(valueBytes), Valid: true}
				objectLiteralType = sql.NullString{String: triple.Object.Literal.Type, Valid: triple.Object.Literal.Type != ""}
				objectLiteralLang = sql.NullString{String: triple.Object.Literal.Language, Valid: triple.Object.Literal.Language != ""}
			}
		} else if triple.Object.Node != nil {
			objectNodeID = sql.NullString{String: triple.Object.Node.ID, Valid: true}
			if len(triple.Object.Node.Type) > 0 {
				objectNodeType = sql.NullString{String: triple.Object.Node.Type[0], Valid: true}
			}
			objectNodeLabel = sql.NullString{String: triple.Object.Node.Label, Valid: triple.Object.Node.Label != ""}
		}

		var metadataJSON sql.NullString
		if triple.Metadata != nil {
			metaBytes, _ := json.Marshal(triple.Metadata)
			metadataJSON = sql.NullString{String: string(metaBytes), Valid: true}
		}

		_, err := stmt.ExecContext(ctx,
			triple.ID, triple.Subject.ID, subjectType, triple.Subject.Label,
			triple.Predicate, objectType, objectNodeID, objectNodeType, objectNodeLabel,
			objectLiteralValue, objectLiteralType, objectLiteralLang,
			triple.Confidence, triple.Timestamp, triple.Source.SessionID, triple.Source.MessageIndex,
			triple.Source.AgentRole, triple.Source.GitCommitHash, string(triple.Source.ExtractionMethod),
			metadataJSON, now,
		)
		if err != nil {
			return err
		}

		// 更新实体
		if err := s.upsertEntityFromTripleTx(ctx, tx, triple); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// upsertEntityFromTripleTx 从三元组更新实体（事务内）
func (s *SQLiteTripleStore) upsertEntityFromTripleTx(ctx context.Context, tx *sql.Tx, triple Triple) error {
	now := time.Now().UnixMilli()

	// Subject实体
	subjectType := "codeflow:Entity"
	if len(triple.Subject.Type) > 0 {
		subjectType = triple.Subject.Type[0]
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO entities (id, type, label, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET updated_at = ?
	`, triple.Subject.ID, subjectType, triple.Subject.Label, now, now, now)
	if err != nil {
		return err
	}

	// Object实体（如果是节点）
	if !triple.Object.IsLiteral() && triple.Object.Node != nil {
		objectType := "codeflow:Entity"
		if len(triple.Object.Node.Type) > 0 {
			objectType = triple.Object.Node.Type[0]
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO entities (id, type, label, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET updated_at = ?
		`, triple.Object.Node.ID, objectType, triple.Object.Node.Label, now, now, now)
		if err != nil {
			return err
		}
	}

	return nil
}

// Get 获取三元组
func (s *SQLiteTripleStore) Get(ctx context.Context, id string) (*Triple, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRowContext(ctx, `
		SELECT id, subject_id, subject_type, subject_label,
			predicate, object_type, object_node_id, object_node_type, object_node_label,
			object_literal_value, object_literal_type, object_literal_lang,
			confidence, timestamp, source_session_id, source_message_index,
			source_agent_role, source_git_hash, source_extraction_method, metadata
		FROM triples WHERE id = ?
	`, id)

	triple, err := s.scanTriple(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return triple, nil
}

// scanTriple 扫描单行为Triple
func (s *SQLiteTripleStore) scanTriple(row *sql.Row) (*Triple, error) {
	var triple Triple
	var subjectType, objectType sql.NullString
	var objectNodeID, objectNodeType, objectNodeLabel sql.NullString
	var objectLiteralValue, objectLiteralType, objectLiteralLang sql.NullString
	var sourceSessionID, sourceAgentRole, sourceGitHash, sourceExtractionMethod sql.NullString
	var sourceMessageIndex sql.NullInt64
	var metadataJSON sql.NullString

	err := row.Scan(
		&triple.ID, &triple.Subject.ID, &subjectType, &triple.Subject.Label,
		&triple.Predicate, &objectType, &objectNodeID, &objectNodeType, &objectNodeLabel,
		&objectLiteralValue, &objectLiteralType, &objectLiteralLang,
		&triple.Confidence, &triple.Timestamp, &sourceSessionID, &sourceMessageIndex,
		&sourceAgentRole, &sourceGitHash, &sourceExtractionMethod, &metadataJSON,
	)
	if err != nil {
		return nil, err
	}

	if subjectType.Valid {
		triple.Subject.Type = []string{subjectType.String}
	}

	if objectType.String == "literal" {
		triple.Object.Literal = &LiteralValue{}
		if objectLiteralValue.Valid {
			json.Unmarshal([]byte(objectLiteralValue.String), &triple.Object.Literal.Value)
		}
		if objectLiteralType.Valid {
			triple.Object.Literal.Type = objectLiteralType.String
		}
		if objectLiteralLang.Valid {
			triple.Object.Literal.Language = objectLiteralLang.String
		}
	} else {
		triple.Object.Node = &TripleNode{}
		if objectNodeID.Valid {
			triple.Object.Node.ID = objectNodeID.String
		}
		if objectNodeType.Valid {
			triple.Object.Node.Type = []string{objectNodeType.String}
		}
		if objectNodeLabel.Valid {
			triple.Object.Node.Label = objectNodeLabel.String
		}
	}

	if sourceSessionID.Valid {
		triple.Source.SessionID = sourceSessionID.String
	}
	if sourceMessageIndex.Valid {
		triple.Source.MessageIndex = int(sourceMessageIndex.Int64)
	}
	if sourceAgentRole.Valid {
		triple.Source.AgentRole = sourceAgentRole.String
	}
	if sourceGitHash.Valid {
		triple.Source.GitCommitHash = sourceGitHash.String
	}
	if sourceExtractionMethod.Valid {
		triple.Source.ExtractionMethod = ExtractionMethod(sourceExtractionMethod.String)
	}

	if metadataJSON.Valid {
		json.Unmarshal([]byte(metadataJSON.String), &triple.Metadata)
	}

	return &triple, nil
}

// Update 更新三元组
func (s *SQLiteTripleStore) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if confidence, ok := updates["confidence"].(float64); ok {
		_, err := s.db.ExecContext(ctx, "UPDATE triples SET confidence = ? WHERE id = ?", confidence, id)
		if err != nil {
			return err
		}
	}

	if metadata, ok := updates["metadata"].(map[string]interface{}); ok {
		metaBytes, _ := json.Marshal(metadata)
		_, err := s.db.ExecContext(ctx, "UPDATE triples SET metadata = ? WHERE id = ?", string(metaBytes), id)
		if err != nil {
			return err
		}
	}

	return nil
}

// Delete 删除三元组
func (s *SQLiteTripleStore) Delete(ctx context.Context, ids []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "DELETE FROM triples WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		_, err := stmt.ExecContext(ctx, id)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Clear 清空存储
func (s *SQLiteTripleStore) Clear(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, "DELETE FROM triples")
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, "DELETE FROM entities")
	return err
}

// Query 查询三元组
func (s *SQLiteTripleStore) Query(ctx context.Context, query TripleQuery) ([]Triple, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	sqlQuery := `
		SELECT id, subject_id, subject_type, subject_label,
			predicate, object_type, object_node_id, object_node_type, object_node_label,
			object_literal_value, object_literal_type, object_literal_lang,
			confidence, timestamp, source_session_id, source_message_index,
			source_agent_role, source_git_hash, source_extraction_method, metadata
		FROM triples WHERE 1=1
	`
	args := []interface{}{}

	if query.Subject != "" {
		sqlQuery += " AND subject_id = ?"
		args = append(args, query.Subject)
	}
	if query.Predicate != "" {
		sqlQuery += " AND predicate = ?"
		args = append(args, query.Predicate)
	}
	if query.Object != "" {
		sqlQuery += " AND object_node_id = ?"
		args = append(args, query.Object)
	}
	if query.MinConfidence > 0 {
		sqlQuery += " AND confidence >= ?"
		args = append(args, query.MinConfidence)
	}
	if query.Source != nil {
		if query.Source.SessionID != "" {
			sqlQuery += " AND source_session_id = ?"
			args = append(args, query.Source.SessionID)
		}
		if query.Source.AgentRole != "" {
			sqlQuery += " AND source_agent_role = ?"
			args = append(args, query.Source.AgentRole)
		}
		if query.Source.ExtractionMethod != "" {
			sqlQuery += " AND source_extraction_method = ?"
			args = append(args, string(query.Source.ExtractionMethod))
		}
	}

	sqlQuery += " ORDER BY confidence DESC"

	if query.Limit > 0 {
		sqlQuery += " LIMIT ?"
		args = append(args, query.Limit)
		if query.Offset > 0 {
			sqlQuery += " OFFSET ?"
			args = append(args, query.Offset)
		}
	} else if query.Offset > 0 {
		// SQLite requires LIMIT with OFFSET, use -1 for unlimited
		sqlQuery += " LIMIT -1 OFFSET ?"
		args = append(args, query.Offset)
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Triple
	for rows.Next() {
		triple, err := s.scanTripleRows(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, *triple)
	}

	return results, rows.Err()
}

// scanTripleRows 扫描多行为Triple
func (s *SQLiteTripleStore) scanTripleRows(rows *sql.Rows) (*Triple, error) {
	var triple Triple
	var subjectType, objectType sql.NullString
	var objectNodeID, objectNodeType, objectNodeLabel sql.NullString
	var objectLiteralValue, objectLiteralType, objectLiteralLang sql.NullString
	var sourceSessionID, sourceAgentRole, sourceGitHash, sourceExtractionMethod sql.NullString
	var sourceMessageIndex sql.NullInt64
	var metadataJSON sql.NullString

	err := rows.Scan(
		&triple.ID, &triple.Subject.ID, &subjectType, &triple.Subject.Label,
		&triple.Predicate, &objectType, &objectNodeID, &objectNodeType, &objectNodeLabel,
		&objectLiteralValue, &objectLiteralType, &objectLiteralLang,
		&triple.Confidence, &triple.Timestamp, &sourceSessionID, &sourceMessageIndex,
		&sourceAgentRole, &sourceGitHash, &sourceExtractionMethod, &metadataJSON,
	)
	if err != nil {
		return nil, err
	}

	if subjectType.Valid {
		triple.Subject.Type = []string{subjectType.String}
	}

	if objectType.String == "literal" {
		triple.Object.Literal = &LiteralValue{}
		if objectLiteralValue.Valid {
			json.Unmarshal([]byte(objectLiteralValue.String), &triple.Object.Literal.Value)
		}
		if objectLiteralType.Valid {
			triple.Object.Literal.Type = objectLiteralType.String
		}
		if objectLiteralLang.Valid {
			triple.Object.Literal.Language = objectLiteralLang.String
		}
	} else {
		triple.Object.Node = &TripleNode{}
		if objectNodeID.Valid {
			triple.Object.Node.ID = objectNodeID.String
		}
		if objectNodeType.Valid {
			triple.Object.Node.Type = []string{objectNodeType.String}
		}
		if objectNodeLabel.Valid {
			triple.Object.Node.Label = objectNodeLabel.String
		}
	}

	if sourceSessionID.Valid {
		triple.Source.SessionID = sourceSessionID.String
	}
	if sourceMessageIndex.Valid {
		triple.Source.MessageIndex = int(sourceMessageIndex.Int64)
	}
	if sourceAgentRole.Valid {
		triple.Source.AgentRole = sourceAgentRole.String
	}
	if sourceGitHash.Valid {
		triple.Source.GitCommitHash = sourceGitHash.String
	}
	if sourceExtractionMethod.Valid {
		triple.Source.ExtractionMethod = ExtractionMethod(sourceExtractionMethod.String)
	}

	if metadataJSON.Valid {
		json.Unmarshal([]byte(metadataJSON.String), &triple.Metadata)
	}

	return &triple, nil
}

// FindBySubject 按主语查询
func (s *SQLiteTripleStore) FindBySubject(ctx context.Context, subjectID string) ([]Triple, error) {
	return s.Query(ctx, TripleQuery{Subject: subjectID})
}

// FindByPredicate 按谓语查询
func (s *SQLiteTripleStore) FindByPredicate(ctx context.Context, predicate string) ([]Triple, error) {
	return s.Query(ctx, TripleQuery{Predicate: predicate})
}

// FindByObject 按宾语查询
func (s *SQLiteTripleStore) FindByObject(ctx context.Context, objectID string) ([]Triple, error) {
	return s.Query(ctx, TripleQuery{Object: objectID})
}

// GetEntity 获取实体
func (s *SQLiteTripleStore) GetEntity(ctx context.Context, id string) (*Entity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, label, description, properties, aliases, created_at, updated_at
		FROM entities WHERE id = ?
	`, id)

	var entity Entity
	var entityType, description, properties, aliases sql.NullString

	err := row.Scan(&entity.ID, &entityType, &entity.Label, &description, &properties, &aliases, &entity.CreatedAt, &entity.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if entityType.Valid {
		entity.Type = []string{entityType.String}
	}
	if description.Valid {
		entity.Description = description.String
	}
	if properties.Valid {
		json.Unmarshal([]byte(properties.String), &entity.Properties)
	}
	if aliases.Valid {
		json.Unmarshal([]byte(aliases.String), &entity.Aliases)
	}

	return &entity, nil
}

// GetEntities 获取所有实体
func (s *SQLiteTripleStore) GetEntities(ctx context.Context) ([]Entity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, label, description, properties, aliases, created_at, updated_at
		FROM entities
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var entity Entity
		var entityType, description, properties, aliases sql.NullString

		err := rows.Scan(&entity.ID, &entityType, &entity.Label, &description, &properties, &aliases, &entity.CreatedAt, &entity.UpdatedAt)
		if err != nil {
			return nil, err
		}

		if entityType.Valid {
			entity.Type = []string{entityType.String}
		}
		if description.Valid {
			entity.Description = description.String
		}
		if properties.Valid {
			json.Unmarshal([]byte(properties.String), &entity.Properties)
		}
		if aliases.Valid {
			json.Unmarshal([]byte(aliases.String), &entity.Aliases)
		}

		entities = append(entities, entity)
	}

	return entities, rows.Err()
}

// UpsertEntity 更新或插入实体
func (s *SQLiteTripleStore) UpsertEntity(ctx context.Context, entity Entity) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()

	entityType := ""
	if len(entity.Type) > 0 {
		entityType = entity.Type[0]
	}

	var propertiesJSON, aliasesJSON sql.NullString
	if entity.Properties != nil {
		propBytes, _ := json.Marshal(entity.Properties)
		propertiesJSON = sql.NullString{String: string(propBytes), Valid: true}
	}
	if entity.Aliases != nil {
		aliasBytes, _ := json.Marshal(entity.Aliases)
		aliasesJSON = sql.NullString{String: string(aliasBytes), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO entities (id, type, label, description, properties, aliases, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			label = excluded.label,
			description = excluded.description,
			properties = excluded.properties,
			aliases = excluded.aliases,
			updated_at = excluded.updated_at
	`, entity.ID, entityType, entity.Label, entity.Description, propertiesJSON, aliasesJSON, now, now)

	return err
}

// ExportGraph 导出图谱
func (s *SQLiteTripleStore) ExportGraph(ctx context.Context) (*JsonLdGraph, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	triples, err := s.Query(ctx, TripleQuery{})
	if err != nil {
		return nil, err
	}

	stats, err := s.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	return &JsonLdGraph{
		Context: JsonLdContext{
			Vocab: s.config.VocabURI,
			Base:  s.config.BaseURI,
		},
		ID:       s.config.GraphID,
		Type:     "Graph",
		Graph:    triples,
		Metadata: *stats,
	}, nil
}

// ImportGraph 导入图谱
func (s *SQLiteTripleStore) ImportGraph(ctx context.Context, graph *JsonLdGraph) error {
	if err := s.Clear(ctx); err != nil {
		return err
	}
	return s.Add(ctx, graph.Graph)
}

// GetStats 获取统计信息
func (s *SQLiteTripleStore) GetStats(ctx context.Context) (*GraphMetadata, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var tripleCount, entityCount, predicateCount int

	row := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM triples")
	row.Scan(&tripleCount)

	row = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entities")
	row.Scan(&entityCount)

	row = s.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT predicate) FROM triples")
	row.Scan(&predicateCount)

	now := time.Now().UnixMilli()
	return &GraphMetadata{
		CreatedAt:      now,
		UpdatedAt:      now,
		TripleCount:    tripleCount,
		EntityCount:    entityCount,
		PredicateCount: predicateCount,
		Version:        "1.0.0",
	}, nil
}

// Deduplicate 去重
func (s *SQLiteTripleStore) Deduplicate(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 直接查询数据库，不通过 Query 方法（避免死锁）
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, subject_id, subject_type, subject_label,
			predicate, object_type, object_node_id, object_node_type, object_node_label,
			object_literal_value, object_literal_type, object_literal_lang,
			confidence, timestamp, source_session_id, source_message_index,
			source_agent_role, source_git_hash, source_extraction_method, metadata
		FROM triples
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var triples []Triple
	for rows.Next() {
		triple, err := s.scanTripleRows(rows)
		if err != nil {
			return 0, err
		}
		triples = append(triples, *triple)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	seen := make(map[string]Triple)
	toDelete := make([]string, 0)

	for _, triple := range triples {
		key := triple.Subject.ID + "|" + triple.Predicate + "|" + triple.Object.GetID()
		existing, exists := seen[key]

		if exists {
			if triple.Confidence > existing.Confidence {
				toDelete = append(toDelete, existing.ID)
				seen[key] = triple
			} else {
				toDelete = append(toDelete, triple.ID)
			}
		} else {
			seen[key] = triple
		}
	}

	// 直接删除，不通过 Delete 方法（避免死锁）
	if len(toDelete) > 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, err
		}
		defer tx.Rollback()

		stmt, err := tx.PrepareContext(ctx, "DELETE FROM triples WHERE id = ?")
		if err != nil {
			return 0, err
		}
		defer stmt.Close()

		for _, id := range toDelete {
			_, err := stmt.ExecContext(ctx, id)
			if err != nil {
				return 0, err
			}
		}

		if err := tx.Commit(); err != nil {
			return 0, err
		}
	}

	return len(toDelete), nil
}

// Ensure SQLiteTripleStore implements ITripleStore
var _ ITripleStore = (*SQLiteTripleStore)(nil)
