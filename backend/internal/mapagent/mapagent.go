package mapagent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/adapters"
)

// MapNodeType 节点类型
type MapNodeType string

const (
	NodeEntity    MapNodeType = "entity"
	NodeDecision  MapNodeType = "decision"
	NodeAction    MapNodeType = "action"
	NodeConcept   MapNodeType = "concept"
	NodeReference MapNodeType = "reference"
)

// MapNode 导图节点
type MapNode struct {
	ID         string                 `json:"id"`
	Type       MapNodeType            `json:"type"`
	Label      string                 `json:"label"`
	Content    string                 `json:"content"`
	Importance float64                `json:"importance"`
	Timestamp  int64                  `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// MapEdge 导图边
type MapEdge struct {
	ID     string  `json:"id"`
	Source string  `json:"source"`
	Target string  `json:"target"`
	Type   string  `json:"type"`
	Weight float64 `json:"weight"`
	Label  string  `json:"label,omitempty"`
}

// DecisionSkeleton 决策骨架
type DecisionSkeleton struct {
	Entities  []string   `json:"entities"`
	Decisions []string   `json:"decisions"`
	Relations []Relation `json:"relations"`
}

// Relation 关系
type Relation struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// MessageRange 消息范围
type MessageRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// CompressionMap 压缩前导图
type CompressionMap struct {
	ID           string                 `json:"id"`
	SessionID    string                 `json:"session_id"`
	Nodes        []MapNode              `json:"nodes"`
	Edges        []MapEdge              `json:"edges"`
	Skeleton     DecisionSkeleton       `json:"skeleton"`
	CreatedAt    int64                  `json:"created_at"`
	MessageRange MessageRange           `json:"message_range"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// MapAgentConfig 配置
type MapAgentConfig struct {
	Adapter          adapters.ICliAdapter `json:"-"`
	ExtractEntities  bool                 `json:"extract_entities"`
	ExtractDecisions bool                 `json:"extract_decisions"`
	ExtractRelations bool                 `json:"extract_relations"`
	MaxNodes         int                  `json:"max_nodes"`
	MinImportance    float64              `json:"min_importance"`
}

// EntityInfo 实体信息
type EntityInfo struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Importance float64 `json:"importance"`
}

// DecisionInfo 决策信息
type DecisionInfo struct {
	Content    string  `json:"content"`
	Importance float64 `json:"importance"`
	Context    string  `json:"context,omitempty"`
}

// RelationInfo 关系信息
type RelationInfo struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Type   string  `json:"type"`
	Weight float64 `json:"weight"`
}

// ConceptInfo 概念信息
type ConceptInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ExtractionResult 提取结果
type ExtractionResult struct {
	Entities  []EntityInfo   `json:"entities"`
	Decisions []DecisionInfo `json:"decisions"`
	Relations []RelationInfo `json:"relations"`
	Concepts  []ConceptInfo  `json:"concepts"`
}

// 实体类型常量
const (
	EntityPerson       = "person"
	EntityOrganization = "organization"
	EntityLocation     = "location"
	EntityTechnology   = "technology"
	EntityConcept      = "concept"
	EntityFile         = "file"
	EntityFunction     = "function"
	EntityClass        = "class"
	EntityVariable     = "variable"
)

// 关系类型常量
const (
	RelationUses       = "uses"
	RelationDependsOn  = "depends_on"
	RelationImplements = "implements"
	RelationExtends    = "extends"
	RelationCalls      = "calls"
	RelationReferences = "references"
	RelationDecides    = "decides"
	RelationCreates    = "creates"
	RelationModifies   = "modifies"
)

// MapSummary 导图摘要
type MapSummary struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	CreatedAt int64  `json:"created_at"`
}

// DefaultMapAgentConfig 默认配置
var DefaultMapAgentConfig = MapAgentConfig{
	ExtractEntities:  true,
	ExtractDecisions: true,
	ExtractRelations: true,
	MaxNodes:         50,
	MinImportance:    0.3,
}

// MapAgent 实现
type MapAgent struct {
	adapter adapters.ICliAdapter
	config  MapAgentConfig
}

// NewMapAgent 创建MapAgent
func NewMapAgent(adapter adapters.ICliAdapter, config *MapAgentConfig) *MapAgent {
	cfg := DefaultMapAgentConfig
	if config != nil {
		cfg = *config
	}
	return &MapAgent{
		adapter: adapter,
		config:  cfg,
	}
}

// Extract 提取信息
func (m *MapAgent) Extract(messages []adapters.Message) (*ExtractionResult, error) {
	if m.adapter != nil {
		return m.extractWithLLM(messages)
	}
	return m.extractLocally(messages), nil
}

// BuildMap 构建导图
func (m *MapAgent) BuildMap(messages []adapters.Message, sessionID string) (*CompressionMap, error) {
	extraction, err := m.Extract(messages)
	if err != nil {
		extraction = m.extractLocally(messages)
	}

	var nodes []MapNode
	var edges []MapEdge
	nodeIDMap := make(map[string]string)

	nodeCounter := 0
	generateNodeID := func() string {
		nodeCounter++
		return fmt.Sprintf("node_%d", nodeCounter)
	}

	// 创建实体节点
	if m.config.ExtractEntities {
		for _, entity := range extraction.Entities {
			if entity.Importance < m.config.MinImportance {
				continue
			}
			nodeID := generateNodeID()
			nodeIDMap[entity.Name] = nodeID

			nodes = append(nodes, MapNode{
				ID:         nodeID,
				Type:       NodeEntity,
				Label:      entity.Name,
				Content:    entity.Name,
				Importance: entity.Importance,
				Timestamp:  time.Now().UnixMilli(),
				Metadata:   map[string]interface{}{"entityType": entity.Type},
			})
		}
	}

	// 创建决策节点
	if m.config.ExtractDecisions {
		for _, decision := range extraction.Decisions {
			if decision.Importance < m.config.MinImportance {
				continue
			}
			nodeID := generateNodeID()
			label := decision.Content
			if len(label) > 50 {
				label = label[:50]
			}
			nodeIDMap[label] = nodeID

			nodes = append(nodes, MapNode{
				ID:         nodeID,
				Type:       NodeDecision,
				Label:      label,
				Content:    decision.Content,
				Importance: decision.Importance,
				Timestamp:  time.Now().UnixMilli(),
				Metadata:   map[string]interface{}{"context": decision.Context},
			})
		}
	}

	// 创建概念节点
	for _, concept := range extraction.Concepts {
		nodeID := generateNodeID()
		nodeIDMap[concept.Name] = nodeID

		nodes = append(nodes, MapNode{
			ID:         nodeID,
			Type:       NodeConcept,
			Label:      concept.Name,
			Content:    concept.Description,
			Importance: 0.5,
			Timestamp:  time.Now().UnixMilli(),
		})
	}

	// 创建边
	if m.config.ExtractRelations {
		edgeCounter := 0
		for _, relation := range extraction.Relations {
			sourceID, ok1 := nodeIDMap[relation.From]
			targetID, ok2 := nodeIDMap[relation.To]

			if ok1 && ok2 {
				edgeCounter++
				edges = append(edges, MapEdge{
					ID:     fmt.Sprintf("edge_%d", edgeCounter),
					Source: sourceID,
					Target: targetID,
					Type:   relation.Type,
					Weight: relation.Weight,
					Label:  relation.Type,
				})
			}
		}
	}

	// 按重要性排序并限制节点数量
	sortNodesByImportance(nodes)
	if len(nodes) > m.config.MaxNodes {
		nodes = nodes[:m.config.MaxNodes]
	}

	// 过滤边（只保留存在节点的边）
	limitedNodeIDs := make(map[string]bool)
	for _, n := range nodes {
		limitedNodeIDs[n.ID] = true
	}
	var limitedEdges []MapEdge
	for _, e := range edges {
		if limitedNodeIDs[e.Source] && limitedNodeIDs[e.Target] {
			limitedEdges = append(limitedEdges, e)
		}
	}

	// 构建决策骨架
	skeleton := DecisionSkeleton{
		Entities:  make([]string, 0, len(extraction.Entities)),
		Decisions: make([]string, 0, len(extraction.Decisions)),
		Relations: make([]Relation, 0, len(extraction.Relations)),
	}
	for _, e := range extraction.Entities {
		skeleton.Entities = append(skeleton.Entities, e.Name)
	}
	for _, d := range extraction.Decisions {
		skeleton.Decisions = append(skeleton.Decisions, d.Content)
	}
	for _, r := range extraction.Relations {
		skeleton.Relations = append(skeleton.Relations, Relation{
			From: r.From,
			To:   r.To,
			Type: r.Type,
		})
	}

	return &CompressionMap{
		ID:        fmt.Sprintf("map_%s_%d", sessionID, time.Now().UnixMilli()),
		SessionID: sessionID,
		Nodes:     nodes,
		Edges:     limitedEdges,
		Skeleton:  skeleton,
		CreatedAt: time.Now().UnixMilli(),
		MessageRange: MessageRange{
			Start: 0,
			End:   len(messages) - 1,
		},
	}, nil
}

// MergeMap 合并导图
func (m *MapAgent) MergeMap(existing, newMap *CompressionMap) *CompressionMap {
	existingNodeLabels := make(map[string]bool)
	for _, n := range existing.Nodes {
		existingNodeLabels[n.Label] = true
	}

	var newNodes []MapNode
	for _, n := range newMap.Nodes {
		if !existingNodeLabels[n.Label] {
			newNodes = append(newNodes, n)
		}
	}

	existingEdgeKeys := make(map[string]bool)
	for _, e := range existing.Edges {
		key := fmt.Sprintf("%s-%s-%s", e.Source, e.Target, e.Type)
		existingEdgeKeys[key] = true
	}

	var newEdges []MapEdge
	for _, e := range newMap.Edges {
		key := fmt.Sprintf("%s-%s-%s", e.Source, e.Target, e.Type)
		if !existingEdgeKeys[key] {
			newEdges = append(newEdges, e)
		}
	}

	// 合并实体（去重）
	entitySet := make(map[string]bool)
	var mergedEntities []string
	for _, e := range existing.Skeleton.Entities {
		if !entitySet[e] {
			entitySet[e] = true
			mergedEntities = append(mergedEntities, e)
		}
	}
	for _, e := range newMap.Skeleton.Entities {
		if !entitySet[e] {
			entitySet[e] = true
			mergedEntities = append(mergedEntities, e)
		}
	}

	return &CompressionMap{
		ID:        existing.ID,
		SessionID: existing.SessionID,
		Nodes:     append(existing.Nodes, newNodes...),
		Edges:     append(existing.Edges, newEdges...),
		Skeleton: DecisionSkeleton{
			Entities:  mergedEntities,
			Decisions: append(existing.Skeleton.Decisions, newMap.Skeleton.Decisions...),
			Relations: append(existing.Skeleton.Relations, newMap.Skeleton.Relations...),
		},
		CreatedAt: existing.CreatedAt,
		MessageRange: MessageRange{
			Start: existing.MessageRange.Start,
			End:   newMap.MessageRange.End,
		},
	}
}

// ==================== 私有方法 ====================

func (m *MapAgent) extractWithLLM(messages []adapters.Message) (*ExtractionResult, error) {
	if m.adapter == nil {
		return m.extractLocally(messages), nil
	}

	prompt := m.buildExtractionPrompt(messages)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := m.adapter.Send(ctx, prompt, &adapters.SendOptions{MaxTokens: 2000})
	if err != nil {
		return m.extractLocally(messages), nil
	}

	return m.parseExtractionResponse(response.Content), nil
}

func (m *MapAgent) extractLocally(messages []adapters.Message) *ExtractionResult {
	var entities []EntityInfo
	var decisions []DecisionInfo
	var relations []RelationInfo

	entitySet := make(map[string]bool)
	decisionSet := make(map[string]bool)

	entityRegex := regexp.MustCompile(`\b[A-Z][a-zA-Z]+\b`)
	codeRegex := regexp.MustCompile("`([^`]+)`")

	for _, msg := range messages {
		content := msg.Content

		// 提取实体
		entityMatches := entityRegex.FindAllString(content, -1)
		for _, entity := range entityMatches {
			if !entitySet[entity] && len(entity) > 2 {
				entitySet[entity] = true
				entities = append(entities, EntityInfo{
					Name:       entity,
					Type:       m.inferEntityType(entity, content),
					Importance: m.calculateImportance(entity, messages),
				})
			}
		}

		// 提取代码标识符
		codeMatches := codeRegex.FindAllStringSubmatch(content, -1)
		for _, match := range codeMatches {
			if len(match) > 1 {
				cleaned := match[1]
				if !entitySet[cleaned] && len(cleaned) > 1 {
					entitySet[cleaned] = true
					entities = append(entities, EntityInfo{
						Name:       cleaned,
						Type:       EntityFunction,
						Importance: 0.6,
					})
				}
			}
		}

		// 提取决策
		decisionKeywords := []string{
			"decide", "choose", "select", "implement", "use", "adopt", "will", "should",
			"决定", "选择", "采用", "实现", "使用",
		}

		sentences := splitSentences(content)
		for _, sentence := range sentences {
			lowerSentence := strings.ToLower(sentence)
			for _, kw := range decisionKeywords {
				if strings.Contains(lowerSentence, kw) {
					trimmed := strings.TrimSpace(sentence)
					if trimmed != "" && !decisionSet[trimmed] && len(trimmed) > 10 {
						decisionSet[trimmed] = true
						decisions = append(decisions, DecisionInfo{
							Content:    trimmed,
							Importance: m.calculateDecisionImportance(trimmed),
							Context:    string(msg.Role),
						})
					}
					break
				}
			}
		}
	}

	// 提取关系（基于共现）
	entityList := make([]string, 0, len(entitySet))
	for e := range entitySet {
		entityList = append(entityList, e)
	}

	for i := 0; i < len(entityList)-1 && i < 20; i++ {
		for j := i + 1; j < len(entityList) && j < 20; j++ {
			e1, e2 := entityList[i], entityList[j]
			for _, msg := range messages {
				if strings.Contains(msg.Content, e1) && strings.Contains(msg.Content, e2) {
					relationType := m.inferRelationType(msg.Content)
					relations = append(relations, RelationInfo{
						From:   e1,
						To:     e2,
						Type:   relationType,
						Weight: 0.5,
					})
					break
				}
			}
		}
	}

	// 限制数量
	if len(entities) > 30 {
		entities = entities[:30]
	}
	if len(decisions) > 15 {
		decisions = decisions[:15]
	}
	if len(relations) > 20 {
		relations = relations[:20]
	}

	return &ExtractionResult{
		Entities:  entities,
		Decisions: decisions,
		Relations: relations,
		Concepts:  nil,
	}
}

func (m *MapAgent) buildExtractionPrompt(messages []adapters.Message) string {
	var content strings.Builder
	count := len(messages)
	if count > 10 {
		count = 10
	}
	for _, msg := range messages[len(messages)-count:] {
		c := msg.Content
		if len(c) > 300 {
			c = c[:300]
		}
		content.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, c))
	}

	return fmt.Sprintf(`Analyze the following conversation and extract:
1. Key entities (people, technologies, concepts, files, functions)
2. Important decisions made
3. Relationships between entities

Conversation:
%s

Respond in JSON format:
{
  "entities": [{"name": "...", "type": "...", "importance": 0.0-1.0}],
  "decisions": [{"content": "...", "importance": 0.0-1.0}],
  "relations": [{"from": "...", "to": "...", "type": "...", "weight": 0.0-1.0}]
}`, content.String())
}

func (m *MapAgent) parseExtractionResponse(response string) *ExtractionResult {
	result := &ExtractionResult{
		Entities:  []EntityInfo{},
		Decisions: []DecisionInfo{},
		Relations: []RelationInfo{},
		Concepts:  []ConceptInfo{},
	}

	jsonRegex := regexp.MustCompile(`\{[\s\S]*\}`)
	jsonMatch := jsonRegex.FindString(response)
	if jsonMatch == "" {
		return result
	}

	var parsed struct {
		Entities  []EntityInfo   `json:"entities"`
		Decisions []DecisionInfo `json:"decisions"`
		Relations []RelationInfo `json:"relations"`
		Concepts  []ConceptInfo  `json:"concepts"`
	}

	if err := json.Unmarshal([]byte(jsonMatch), &parsed); err == nil {
		if parsed.Entities != nil {
			result.Entities = parsed.Entities
		}
		if parsed.Decisions != nil {
			result.Decisions = parsed.Decisions
		}
		if parsed.Relations != nil {
			result.Relations = parsed.Relations
		}
		if parsed.Concepts != nil {
			result.Concepts = parsed.Concepts
		}
	}

	return result
}

func (m *MapAgent) inferEntityType(entity, context string) string {
	lowerContext := strings.ToLower(context)
	lowerEntity := strings.ToLower(entity)

	if strings.Contains(lowerContext, "class "+lowerEntity) {
		return EntityClass
	}
	if strings.Contains(lowerContext, "function "+lowerEntity) {
		return EntityFunction
	}
	if strings.Contains(lowerContext, lowerEntity+".ts") || strings.Contains(lowerContext, lowerEntity+".js") {
		return EntityFile
	}
	if regexp.MustCompile(`^[A-Z][a-z]+[A-Z]`).MatchString(entity) {
		return EntityClass
	}

	return EntityConcept
}

func (m *MapAgent) calculateImportance(entity string, messages []adapters.Message) float64 {
	count := 0
	re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(entity))
	for _, msg := range messages {
		matches := re.FindAllString(msg.Content, -1)
		count += len(matches)
	}
	importance := float64(count) / float64(len(messages))
	if importance > 1 {
		importance = 1
	}
	return importance
}

func (m *MapAgent) calculateDecisionImportance(decision string) float64 {
	strongKeywords := []string{"must", "critical", "important", "必须", "关键", "重要"}
	mediumKeywords := []string{"should", "recommend", "应该", "建议"}

	lower := strings.ToLower(decision)
	for _, kw := range strongKeywords {
		if strings.Contains(lower, kw) {
			return 0.9
		}
	}
	for _, kw := range mediumKeywords {
		if strings.Contains(lower, kw) {
			return 0.7
		}
	}
	return 0.5
}

func (m *MapAgent) inferRelationType(context string) string {
	lower := strings.ToLower(context)

	if strings.Contains(lower, "uses") || strings.Contains(lower, "使用") {
		return RelationUses
	}
	if strings.Contains(lower, "depends") || strings.Contains(lower, "依赖") {
		return RelationDependsOn
	}
	if strings.Contains(lower, "implements") || strings.Contains(lower, "实现") {
		return RelationImplements
	}
	if strings.Contains(lower, "extends") || strings.Contains(lower, "继承") {
		return RelationExtends
	}
	if strings.Contains(lower, "calls") || strings.Contains(lower, "调用") {
		return RelationCalls
	}

	return RelationReferences
}

func splitSentences(text string) []string {
	re := regexp.MustCompile(`[.。!！?？]+`)
	parts := re.Split(text, -1)
	var sentences []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			sentences = append(sentences, trimmed)
		}
	}
	return sentences
}

func sortNodesByImportance(nodes []MapNode) {
	for i := 0; i < len(nodes)-1; i++ {
		for j := i + 1; j < len(nodes); j++ {
			if nodes[j].Importance > nodes[i].Importance {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
	}
}

// InMemoryMapStorage 内存存储实现
type InMemoryMapStorage struct {
	maps map[string]*CompressionMap
	mu   sync.RWMutex
}

// NewInMemoryMapStorage 创建内存存储
func NewInMemoryMapStorage() *InMemoryMapStorage {
	return &InMemoryMapStorage{
		maps: make(map[string]*CompressionMap),
	}
}

// Save 保存导图
func (s *InMemoryMapStorage) Save(m *CompressionMap) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maps[m.ID] = m
	return nil
}

// Load 加载导图
func (s *InMemoryMapStorage) Load(id string) (*CompressionMap, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m, ok := s.maps[id]; ok {
		return m, nil
	}
	return nil, nil
}

// LoadBySession 按会话加载
func (s *InMemoryMapStorage) LoadBySession(sessionID string) ([]*CompressionMap, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*CompressionMap
	for _, m := range s.maps {
		if m.SessionID == sessionID {
			result = append(result, m)
		}
	}
	return result, nil
}

// Delete 删除导图
func (s *InMemoryMapStorage) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.maps, id)
	return nil
}

// List 列出所有导图
func (s *InMemoryMapStorage) List() ([]MapSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []MapSummary
	for _, m := range s.maps {
		result = append(result, MapSummary{
			ID:        m.ID,
			SessionID: m.SessionID,
			CreatedAt: m.CreatedAt,
		})
	}
	return result, nil
}
