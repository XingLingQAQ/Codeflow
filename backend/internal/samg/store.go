package samg

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// InMemoryTripleStore 内存三元组存储实现
type InMemoryTripleStore struct {
	config         TripleStoreConfig
	triples        map[string]Triple
	entities       map[string]Entity
	subjectIndex   map[string]map[string]struct{}
	predicateIndex map[string]map[string]struct{}
	objectIndex    map[string]map[string]struct{}
	mu             sync.RWMutex
}

// NewInMemoryTripleStore 创建内存三元组存储
func NewInMemoryTripleStore(config *TripleStoreConfig) *InMemoryTripleStore {
	cfg := DefaultTripleStoreConfig
	if config != nil {
		cfg = *config
	}

	return &InMemoryTripleStore{
		config:         cfg,
		triples:        make(map[string]Triple),
		entities:       make(map[string]Entity),
		subjectIndex:   make(map[string]map[string]struct{}),
		predicateIndex: make(map[string]map[string]struct{}),
		objectIndex:    make(map[string]map[string]struct{}),
	}
}

// Add 添加三元组
func (s *InMemoryTripleStore) Add(ctx context.Context, triples []Triple) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, triple := range triples {
		if len(s.triples) >= s.config.MaxTriples {
			return errors.New("max triples limit reached")
		}

		if s.config.EnableDeduplication {
			existingID := s.findDuplicate(triple)
			if existingID != "" {
				existing := s.triples[existingID]
				if triple.Confidence > existing.Confidence {
					s.removeFromIndices(existingID, existing)
					delete(s.triples, existingID)
				} else {
					continue
				}
			}
		}

		s.triples[triple.ID] = triple
		s.addToIndices(triple)
		s.updateEntitiesFromTriple(triple)
	}

	return nil
}

// Get 获取三元组
func (s *InMemoryTripleStore) Get(ctx context.Context, id string) (*Triple, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if triple, ok := s.triples[id]; ok {
		return &triple, nil
	}
	return nil, nil
}

// Update 更新三元组
func (s *InMemoryTripleStore) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.triples[id]
	if !ok {
		return errors.New("triple not found")
	}

	s.removeFromIndices(id, existing)

	// 应用更新
	if confidence, ok := updates["confidence"].(float64); ok {
		existing.Confidence = confidence
	}
	if metadata, ok := updates["metadata"].(map[string]interface{}); ok {
		existing.Metadata = metadata
	}

	s.triples[id] = existing
	s.addToIndices(existing)

	return nil
}

// Delete 删除三元组
func (s *InMemoryTripleStore) Delete(ctx context.Context, ids []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		if triple, ok := s.triples[id]; ok {
			s.removeFromIndices(id, triple)
			delete(s.triples, id)
		}
	}

	return nil
}

// Clear 清空存储
func (s *InMemoryTripleStore) Clear(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.triples = make(map[string]Triple)
	s.entities = make(map[string]Entity)
	s.subjectIndex = make(map[string]map[string]struct{})
	s.predicateIndex = make(map[string]map[string]struct{})
	s.objectIndex = make(map[string]map[string]struct{})

	return nil
}

// Query 查询三元组
func (s *InMemoryTripleStore) Query(ctx context.Context, query TripleQuery) ([]Triple, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var candidateIDs map[string]struct{}

	// 使用索引缩小候选集
	if query.Subject != "" {
		if idx, ok := s.subjectIndex[query.Subject]; ok {
			candidateIDs = copySet(idx)
		} else {
			return []Triple{}, nil
		}
	}

	if query.Predicate != "" {
		predicateIDs := s.predicateIndex[query.Predicate]
		if predicateIDs == nil {
			return []Triple{}, nil
		}
		if candidateIDs == nil {
			candidateIDs = copySet(predicateIDs)
		} else {
			candidateIDs = intersect(candidateIDs, predicateIDs)
		}
	}

	if query.Object != "" {
		objectIDs := s.objectIndex[query.Object]
		if objectIDs == nil {
			return []Triple{}, nil
		}
		if candidateIDs == nil {
			candidateIDs = copySet(objectIDs)
		} else {
			candidateIDs = intersect(candidateIDs, objectIDs)
		}
	}

	// 如果没有索引约束，使用所有三元组
	if candidateIDs == nil {
		candidateIDs = make(map[string]struct{})
		for id := range s.triples {
			candidateIDs[id] = struct{}{}
		}
	}

	// 过滤并收集结果
	var results []Triple
	for id := range candidateIDs {
		triple, ok := s.triples[id]
		if !ok {
			continue
		}

		// 置信度过滤
		if query.MinConfidence > 0 && triple.Confidence < query.MinConfidence {
			continue
		}

		// 来源过滤
		if query.Source != nil {
			if query.Source.SessionID != "" && triple.Source.SessionID != query.Source.SessionID {
				continue
			}
			if query.Source.AgentRole != "" && triple.Source.AgentRole != query.Source.AgentRole {
				continue
			}
			if query.Source.ExtractionMethod != "" && triple.Source.ExtractionMethod != query.Source.ExtractionMethod {
				continue
			}
		}

		results = append(results, triple)
	}

	// 按置信度排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Confidence > results[j].Confidence
	})

	// 分页
	offset := query.Offset
	if offset > len(results) {
		return []Triple{}, nil
	}

	limit := query.Limit
	if limit <= 0 {
		limit = len(results)
	}

	end := offset + limit
	if end > len(results) {
		end = len(results)
	}

	return results[offset:end], nil
}

// FindBySubject 按主语查询
func (s *InMemoryTripleStore) FindBySubject(ctx context.Context, subjectID string) ([]Triple, error) {
	return s.Query(ctx, TripleQuery{Subject: subjectID})
}

// FindByPredicate 按谓语查询
func (s *InMemoryTripleStore) FindByPredicate(ctx context.Context, predicate string) ([]Triple, error) {
	return s.Query(ctx, TripleQuery{Predicate: predicate})
}

// FindByObject 按宾语查询
func (s *InMemoryTripleStore) FindByObject(ctx context.Context, objectID string) ([]Triple, error) {
	return s.Query(ctx, TripleQuery{Object: objectID})
}

// GetEntity 获取实体
func (s *InMemoryTripleStore) GetEntity(ctx context.Context, id string) (*Entity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if entity, ok := s.entities[id]; ok {
		return &entity, nil
	}
	return nil, nil
}

// GetEntities 获取所有实体
func (s *InMemoryTripleStore) GetEntities(ctx context.Context) ([]Entity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entities := make([]Entity, 0, len(s.entities))
	for _, entity := range s.entities {
		entities = append(entities, entity)
	}
	return entities, nil
}

// UpsertEntity 更新或插入实体
func (s *InMemoryTripleStore) UpsertEntity(ctx context.Context, entity Entity) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.entities[entity.ID]; ok {
		entity.CreatedAt = existing.CreatedAt
		entity.UpdatedAt = time.Now().UnixMilli()
	} else {
		now := time.Now().UnixMilli()
		entity.CreatedAt = now
		entity.UpdatedAt = now
	}

	s.entities[entity.ID] = entity
	return nil
}

// ExportGraph 导出图谱
func (s *InMemoryTripleStore) ExportGraph(ctx context.Context) (*JsonLdGraph, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	stats, _ := s.getStatsLocked()

	triples := make([]Triple, 0, len(s.triples))
	for _, triple := range s.triples {
		triples = append(triples, triple)
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
func (s *InMemoryTripleStore) ImportGraph(ctx context.Context, graph *JsonLdGraph) error {
	if err := s.Clear(ctx); err != nil {
		return err
	}
	return s.Add(ctx, graph.Graph)
}

// GetStats 获取统计信息
func (s *InMemoryTripleStore) GetStats(ctx context.Context) (*GraphMetadata, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getStatsLocked()
}

func (s *InMemoryTripleStore) getStatsLocked() (*GraphMetadata, error) {
	predicates := make(map[string]struct{})
	for _, triple := range s.triples {
		predicates[triple.Predicate] = struct{}{}
	}

	now := time.Now().UnixMilli()
	return &GraphMetadata{
		CreatedAt:      now,
		UpdatedAt:      now,
		TripleCount:    len(s.triples),
		EntityCount:    len(s.entities),
		PredicateCount: len(predicates),
		Version:        "1.0.0",
	}, nil
}

// Deduplicate 去重
func (s *InMemoryTripleStore) Deduplicate(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]string) // key -> id
	toDelete := make([]string, 0)

	for id, triple := range s.triples {
		key := s.getTripleKey(triple)
		existingID, exists := seen[key]

		if exists {
			existing := s.triples[existingID]
			if triple.Confidence > existing.Confidence {
				toDelete = append(toDelete, existingID)
				seen[key] = id
			} else {
				toDelete = append(toDelete, id)
			}
		} else {
			seen[key] = id
		}
	}

	for _, id := range toDelete {
		if triple, ok := s.triples[id]; ok {
			s.removeFromIndices(id, triple)
			delete(s.triples, id)
		}
	}

	return len(toDelete), nil
}

// 私有辅助方法

func (s *InMemoryTripleStore) findDuplicate(triple Triple) string {
	key := s.getTripleKey(triple)
	for id, existing := range s.triples {
		if s.getTripleKey(existing) == key {
			return id
		}
	}
	return ""
}

func (s *InMemoryTripleStore) getTripleKey(triple Triple) string {
	subjectID := triple.Subject.ID
	var objectID string
	if triple.Object.IsLiteral() {
		objectID = "literal:" + toString(triple.Object.Literal.Value)
	} else if triple.Object.Node != nil {
		objectID = triple.Object.Node.ID
	}
	return subjectID + "|" + triple.Predicate + "|" + objectID
}

func (s *InMemoryTripleStore) addToIndices(triple Triple) {
	// Subject索引
	subjectID := triple.Subject.ID
	if s.subjectIndex[subjectID] == nil {
		s.subjectIndex[subjectID] = make(map[string]struct{})
	}
	s.subjectIndex[subjectID][triple.ID] = struct{}{}

	// Predicate索引
	if s.predicateIndex[triple.Predicate] == nil {
		s.predicateIndex[triple.Predicate] = make(map[string]struct{})
	}
	s.predicateIndex[triple.Predicate][triple.ID] = struct{}{}

	// Object索引（仅节点类型）
	if !triple.Object.IsLiteral() && triple.Object.Node != nil {
		objectID := triple.Object.Node.ID
		if s.objectIndex[objectID] == nil {
			s.objectIndex[objectID] = make(map[string]struct{})
		}
		s.objectIndex[objectID][triple.ID] = struct{}{}
	}
}

func (s *InMemoryTripleStore) removeFromIndices(id string, triple Triple) {
	subjectID := triple.Subject.ID
	if idx, ok := s.subjectIndex[subjectID]; ok {
		delete(idx, id)
	}

	if idx, ok := s.predicateIndex[triple.Predicate]; ok {
		delete(idx, id)
	}

	if !triple.Object.IsLiteral() && triple.Object.Node != nil {
		objectID := triple.Object.Node.ID
		if idx, ok := s.objectIndex[objectID]; ok {
			delete(idx, id)
		}
	}
}

func (s *InMemoryTripleStore) updateEntitiesFromTriple(triple Triple) {
	now := time.Now().UnixMilli()

	// 创建Subject实体
	if _, ok := s.entities[triple.Subject.ID]; !ok {
		entityType := "codeflow:Entity"
		if len(triple.Subject.Type) > 0 {
			entityType = triple.Subject.Type[0]
		}
		s.entities[triple.Subject.ID] = Entity{
			ID:        triple.Subject.ID,
			Type:      []string{entityType},
			Label:     triple.Subject.Label,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	// 创建Object实体（如果是节点）
	if !triple.Object.IsLiteral() && triple.Object.Node != nil {
		if _, ok := s.entities[triple.Object.Node.ID]; !ok {
			entityType := "codeflow:Entity"
			if len(triple.Object.Node.Type) > 0 {
				entityType = triple.Object.Node.Type[0]
			}
			s.entities[triple.Object.Node.ID] = Entity{
				ID:        triple.Object.Node.ID,
				Type:      []string{entityType},
				Label:     triple.Object.Node.Label,
				CreatedAt: now,
				UpdatedAt: now,
			}
		}
	}
}

// 辅助函数

func copySet(src map[string]struct{}) map[string]struct{} {
	dst := make(map[string]struct{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func intersect(a, b map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{})
	for k := range a {
		if _, ok := b[k]; ok {
			result[k] = struct{}{}
		}
	}
	return result
}

func toString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return string(rune(val))
	case float64:
		return string(rune(int(val)))
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

// AppendPointer 向实体追加指针，超过上限时按 Relevance × Recency 淘汰。
func (s *InMemoryTripleStore) AppendPointer(ctx context.Context, entityID string, ptr Pointer) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entity, ok := s.entities[entityID]
	if !ok {
		return nil // 实体不存在，静默跳过
	}

	entity.Pointers = append(entity.Pointers, ptr)

	// 超过上限时淘汰
	if len(entity.Pointers) > MaxPointersPerEntity {
		sort.Slice(entity.Pointers, func(i, j int) bool {
			scoreI := entity.Pointers[i].Relevance * float64(entity.Pointers[i].Timestamp)
			scoreJ := entity.Pointers[j].Relevance * float64(entity.Pointers[j].Timestamp)
			return scoreI > scoreJ
		})
		entity.Pointers = entity.Pointers[:MaxPointersPerEntity]
	}

	entity.UpdatedAt = time.Now().UnixMilli()
	s.entities[entityID] = entity
	return nil
}

// GetPointers 获取实体的所有指针。
func (s *InMemoryTripleStore) GetPointers(ctx context.Context, entityID string) ([]Pointer, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entity, ok := s.entities[entityID]
	if !ok {
		return nil, nil
	}
	return entity.Pointers, nil
}
