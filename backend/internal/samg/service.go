// Package samg - SAMG Service for unified graph operations
package samg

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ISAMGService SAMG服务接口
type ISAMGService interface {
	// 三元组操作
	AddTriples(ctx context.Context, triples []Triple) error
	GetTriple(ctx context.Context, id string) (*Triple, error)
	QueryTriples(ctx context.Context, query TripleQuery) ([]Triple, error)
	DeleteTriples(ctx context.Context, ids []string) error

	// 增量提取
	ExtractFromMessage(ctx context.Context, message string, source TripleSource) ([]Triple, error)
	ExtractWithPointers(ctx context.Context, message string, source TripleSource, rawArchiveID string) ([]Triple, error)
	TriggerIncrementalExtraction(ctx context.Context, sessionID string, messageIndex int, content string) error

	// 查询
	QueryMemory(ctx context.Context, req QueryMemoryRequest) (*QueryMemoryResponse, error)

	// 扩展激活
	Activate(ctx context.Context, sourceIDs []string) (*ActivationResult, error)
	FindPaths(ctx context.Context, sourceID, targetID string, maxHops int) ([][]string, error)

	// 衰减管理
	RecordAccess(ctx context.Context, nodeID string) error
	ApplyDecay(ctx context.Context) (int, int, error)
	GetVisibleNodes(ctx context.Context) []NodeActivation
	GetHiddenNodes(ctx context.Context) []NodeActivation
	GetTopNodes(ctx context.Context, n int) []NodeActivation

	// 图谱操作
	ExportGraph(ctx context.Context) (*JsonLdGraph, error)
	ImportGraph(ctx context.Context, graph *JsonLdGraph) (*ImportGraphResult, error)
	// ReplaceGraph clears the current graph and imports the provided snapshot payload (true restore).
	ReplaceGraph(ctx context.Context, graph *JsonLdGraph) (*ImportGraphResult, error)
	GetStats(ctx context.Context) (*SAMGStats, error)

	// 指针操作
	GetNodePointers(ctx context.Context, nodeID string) ([]Pointer, error)
	AddNodePointer(ctx context.Context, nodeID string, ptr Pointer) error

	// 配置
	GetActivationConfig() ActivationConfig
	UpdateActivationConfig(config ActivationConfig)
	GetDecayConfig() DecayConfig
	UpdateDecayConfig(config DecayConfig)
}

// SAMGStats SAMG统计信息
type SAMGStats struct {
	GraphStats    *GraphMetadata         `json:"graph_stats"`
	DecayStats    map[string]interface{} `json:"decay_stats"`
	ExtractorInfo map[string]interface{} `json:"extractor_info"`
}

// SAMGService SAMG服务实现
type SAMGService struct {
	store      ITripleStore
	extractor  *TripleExtractor
	activation *SpreadingActivation
	decay      *DecayManager
	mu         sync.RWMutex

	// Hook回调
	onExtractComplete func(ctx context.Context, triples []Triple)
}

var (
	globalSAMGService *SAMGService
	globalSAMGMu      sync.RWMutex
	samgOnce          sync.Once
)

// SAMGServiceConfig 服务配置
type SAMGServiceConfig struct {
	StoreConfig      *TripleStoreConfig
	ExtractorConfig  *TripleExtractorConfig
	ActivationConfig *ActivationConfig
	DecayConfig      *DecayConfig
}

// NewSAMGService 创建SAMG服务
func NewSAMGService(config *SAMGServiceConfig) *SAMGService {
	var storeConfig *TripleStoreConfig
	var extractorConfig *TripleExtractorConfig
	var activationConfig *ActivationConfig
	var decayConfig *DecayConfig

	if config != nil {
		storeConfig = config.StoreConfig
		extractorConfig = config.ExtractorConfig
		activationConfig = config.ActivationConfig
		decayConfig = config.DecayConfig
	}

	store := NewInMemoryTripleStore(storeConfig)
	extractor := NewTripleExtractor(extractorConfig)
	activation := NewSpreadingActivation(store, activationConfig)
	decay := NewDecayManager(store, decayConfig)

	return &SAMGService{
		store:      store,
		extractor:  extractor,
		activation: activation,
		decay:      decay,
	}
}

// SetSAMGService 设置全局SAMG服务
func SetSAMGService(svc *SAMGService) {
	globalSAMGMu.Lock()
	defer globalSAMGMu.Unlock()
	globalSAMGService = svc
}

// GetSAMGService 获取全局SAMG服务
func GetSAMGService() *SAMGService {
	samgOnce.Do(func() {
		globalSAMGMu.Lock()
		defer globalSAMGMu.Unlock()
		if globalSAMGService == nil {
			globalSAMGService = NewSAMGService(nil)
		}
	})
	globalSAMGMu.RLock()
	defer globalSAMGMu.RUnlock()
	return globalSAMGService
}

// SetOnExtractComplete 设置提取完成回调
func (s *SAMGService) SetOnExtractComplete(callback func(ctx context.Context, triples []Triple)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onExtractComplete = callback
}

// AddTriples 添加三元组
func (s *SAMGService) AddTriples(ctx context.Context, triples []Triple) error {
	return s.store.Add(ctx, triples)
}

// GetTriple 获取三元组
func (s *SAMGService) GetTriple(ctx context.Context, id string) (*Triple, error) {
	return s.store.Get(ctx, id)
}

// QueryTriples 查询三元组
func (s *SAMGService) QueryTriples(ctx context.Context, query TripleQuery) ([]Triple, error) {
	return s.store.Query(ctx, query)
}

// DeleteTriples 删除三元组
func (s *SAMGService) DeleteTriples(ctx context.Context, ids []string) error {
	return s.store.Delete(ctx, ids)
}

// ExtractFromMessage 从消息提取三元组
func (s *SAMGService) ExtractFromMessage(ctx context.Context, message string, source TripleSource) ([]Triple, error) {
	triples, err := s.extractor.Extract(ctx, message, source)
	if err != nil {
		return nil, err
	}

	// 添加到存储
	if len(triples) > 0 {
		if err := s.store.Add(ctx, triples); err != nil {
			return triples, err
		}

		// 记录访问以更新衰减
		for _, triple := range triples {
			s.decay.RecordAccess(ctx, triple.Subject.ID)
			if triple.Object.Node != nil {
				s.decay.RecordAccess(ctx, triple.Object.Node.ID)
			}
		}

		// 触发回调
		s.mu.RLock()
		callback := s.onExtractComplete
		s.mu.RUnlock()

		if callback != nil {
			callback(ctx, triples)
		}
	}

	return triples, nil
}

// TriggerIncrementalExtraction 触发增量提取（用于hook_on_message_complete）
func (s *SAMGService) TriggerIncrementalExtraction(ctx context.Context, sessionID string, messageIndex int, content string) error {
	source := TripleSource{
		SessionID:        sessionID,
		MessageIndex:     messageIndex,
		ExtractionMethod: ExtractionRule,
	}

	_, err := s.ExtractFromMessage(ctx, content, source)
	return err
}

// Activate 扩展激活
func (s *SAMGService) Activate(ctx context.Context, sourceIDs []string) (*ActivationResult, error) {
	// 记录访问
	for _, id := range sourceIDs {
		s.decay.RecordAccess(ctx, id)
	}

	return s.activation.Activate(ctx, sourceIDs)
}

// FindPaths 查找路径
func (s *SAMGService) FindPaths(ctx context.Context, sourceID, targetID string, maxHops int) ([][]string, error) {
	return s.activation.FindPaths(ctx, sourceID, targetID, maxHops)
}

// RecordAccess 记录访问
func (s *SAMGService) RecordAccess(ctx context.Context, nodeID string) error {
	return s.decay.RecordAccess(ctx, nodeID)
}

// ApplyDecay 应用衰减
func (s *SAMGService) ApplyDecay(ctx context.Context) (int, int, error) {
	return s.decay.ApplyDecay(ctx)
}

// GetVisibleNodes 获取可见节点
func (s *SAMGService) GetVisibleNodes(ctx context.Context) []NodeActivation {
	return s.decay.GetVisibleNodes(ctx)
}

// GetHiddenNodes 获取隐藏节点
func (s *SAMGService) GetHiddenNodes(ctx context.Context) []NodeActivation {
	return s.decay.GetHiddenNodes(ctx)
}

// GetTopNodes 获取Top N节点
func (s *SAMGService) GetTopNodes(ctx context.Context, n int) []NodeActivation {
	return s.decay.GetTopNodes(ctx, n)
}

// ExportGraph 导出图谱
func (s *SAMGService) ExportGraph(ctx context.Context) (*JsonLdGraph, error) {
	return s.store.ExportGraph(ctx)
}

// ImportGraph 导入图谱
func (s *SAMGService) ImportGraph(ctx context.Context, graph *JsonLdGraph) (*ImportGraphResult, error) {
	return s.store.ImportGraph(ctx, graph)
}

// ReplaceGraph clears the current graph and imports the provided snapshot payload.
// Empty/nil graph is treated as a successful clear (true restore to empty).
func (s *SAMGService) ReplaceGraph(ctx context.Context, graph *JsonLdGraph) (*ImportGraphResult, error) {
	if err := s.store.Clear(ctx); err != nil {
		return nil, fmt.Errorf("clear graph before restore: %w", err)
	}
	if graph == nil {
		return &ImportGraphResult{}, nil
	}
	return s.store.ImportGraph(ctx, graph)
}

// GetStats 获取统计信息
func (s *SAMGService) GetStats(ctx context.Context) (*SAMGStats, error) {
	graphStats, err := s.store.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	return &SAMGStats{
		GraphStats: graphStats,
		DecayStats: s.decay.GetStats(),
		ExtractorInfo: map[string]interface{}{
			"min_confidence": s.extractor.config.MinConfidence,
			"max_triples":    s.extractor.config.MaxTriplesPerExtraction,
			"rule_based":     s.extractor.config.EnableRuleBasedExtraction,
		},
	}, nil
}

// GetActivationConfig 获取激活配置
func (s *SAMGService) GetActivationConfig() ActivationConfig {
	return s.activation.GetConfig()
}

// UpdateActivationConfig 更新激活配置
func (s *SAMGService) UpdateActivationConfig(config ActivationConfig) {
	s.activation.UpdateConfig(config)
}

// GetDecayConfig 获取衰减配置
func (s *SAMGService) GetDecayConfig() DecayConfig {
	return s.decay.GetConfig()
}

// UpdateDecayConfig 更新衰减配置
func (s *SAMGService) UpdateDecayConfig(config DecayConfig) {
	s.decay.UpdateConfig(config)
}

// GetStore 获取底层存储（用于高级操作）
func (s *SAMGService) GetStore() ITripleStore {
	return s.store
}

// GetDecayManager 获取衰减管理器
func (s *SAMGService) GetDecayManager() *DecayManager {
	return s.decay
}

// GetActivationEngine 获取激活引擎
func (s *SAMGService) GetActivationEngine() *SpreadingActivation {
	return s.activation
}

// GetRelations 获取节点的关联关系
func (s *SAMGService) GetRelations(ctx context.Context, nodeID string) ([]Triple, error) {
	// 作为主语
	subjectTriples, err := s.store.FindBySubject(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	// 作为宾语
	objectTriples, err := s.store.FindByObject(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	// 合并结果
	result := make([]Triple, 0, len(subjectTriples)+len(objectTriples))
	result = append(result, subjectTriples...)
	result = append(result, objectTriples...)

	// 记录访问
	s.decay.RecordAccess(ctx, nodeID)

	return result, nil
}

// HookOnMessageComplete Hook: 消息完成时触发
// 这个方法应该被Hook系统调用
func (s *SAMGService) HookOnMessageComplete(ctx context.Context, sessionID string, messageIndex int, role string, content string) error {
	source := TripleSource{
		SessionID:        sessionID,
		MessageIndex:     messageIndex,
		AgentRole:        role,
		ExtractionMethod: ExtractionRule,
	}

	_, err := s.ExtractFromMessage(ctx, content, source)
	return err
}

// StartAutoDecay 启动自动衰减（后台goroutine）
func (s *SAMGService) StartAutoDecay(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.decay.ApplyDecay(ctx)
			}
		}
	}()
}

// ExtractWithPointers 提取三元组并绑定 Raw Archive 指针。
func (s *SAMGService) ExtractWithPointers(ctx context.Context, message string, source TripleSource, rawArchiveID string) ([]Triple, error) {
	triples, err := s.extractor.Extract(ctx, message, source)
	if err != nil {
		return nil, err
	}

	if len(triples) == 0 {
		return triples, nil
	}

	// 添加到存储
	if err := s.store.Add(ctx, triples); err != nil {
		return triples, err
	}

	// 为每个三元组的 Subject/Object 节点挂载指针
	summary := message
	if len(summary) > 100 {
		summary = summary[:100]
	}

	for _, triple := range triples {
		ptr := Pointer{
			SourceID:   rawArchiveID,
			SourceType: string(source.ExtractionMethod),
			Summary:    summary,
			Timestamp:  time.Now().UnixMilli(),
			Relevance:  triple.Confidence,
		}

		s.store.AppendPointer(ctx, triple.Subject.ID, ptr)
		if triple.Object.Node != nil {
			s.store.AppendPointer(ctx, triple.Object.Node.ID, ptr)
		}

		// 记录访问以更新 BLA
		s.decay.RecordAccess(ctx, triple.Subject.ID)
		if triple.Object.Node != nil {
			s.decay.RecordAccess(ctx, triple.Object.Node.ID)
		}
	}

	// 触发回调
	s.mu.RLock()
	callback := s.onExtractComplete
	s.mu.RUnlock()
	if callback != nil {
		callback(ctx, triples)
	}

	return triples, nil
}

// QueryMemory 通过 SAMG 神经索引查询相关记忆。
func (s *SAMGService) QueryMemory(ctx context.Context, req QueryMemoryRequest) (*QueryMemoryResponse, error) {
	if req.MaxResults <= 0 {
		req.MaxResults = 10
	}

	// Step 1: 找到种子节点
	seedIDs := s.findSeedNodes(ctx, req.Topic)
	if len(seedIDs) == 0 {
		return &QueryMemoryResponse{
			ActivatedNodes: []QueryMemoryNode{},
			ContextBlock:   "",
		}, nil
	}

	// Step 2: 扩散激活
	result, err := s.activation.Activate(ctx, seedIDs)
	if err != nil {
		return nil, err
	}

	// Step 3: BLA 过滤 + 构建 QueryMemoryNode
	var nodes []QueryMemoryNode
	for _, na := range result.ActivatedNodes {
		if req.MinBLA > 0 {
			decayNode, exists := s.decay.GetNodeActivation(na.ID)
			if exists && decayNode.Activation < req.MinBLA {
				continue
			}
		}

		node := QueryMemoryNode{
			ID:         na.ID,
			Label:      na.Label,
			Activation: na.Activation,
			Hop:        na.Hop,
		}

		// 获取 label
		entity, _ := s.store.GetEntity(ctx, na.ID)
		if entity != nil {
			node.Type = append([]string(nil), entity.Type...)
			node.Label = entity.Label
			node.Description = entity.Description
			node.Properties = entity.Properties
			node.Aliases = append([]string(nil), entity.Aliases...)
		}

		// Step 4: 指针解析
		if req.ResolvePointers && entity != nil {
			for _, ptr := range entity.Pointers {
				rp := ResolvedPointer{Pointer: ptr}
				// 注意：实际的 ResolvedContent 需要调用 Raw Archive，
				// 这里只返回指针信息，由上层 MemoryAgent 负责解析
				node.Pointers = append(node.Pointers, rp)
			}
		}

		nodes = append(nodes, node)
		if len(nodes) >= req.MaxResults {
			break
		}
	}

	// Step 5: 构造上下文块
	contextBlock := s.buildContextBlock(nodes)

	return &QueryMemoryResponse{
		ActivatedNodes: nodes,
		ContextBlock:   contextBlock,
	}, nil
}

// GetNodePointers 获取节点的所有指针。
func (s *SAMGService) GetNodePointers(ctx context.Context, nodeID string) ([]Pointer, error) {
	return s.store.GetPointers(ctx, nodeID)
}

// AddNodePointer 手动添加指针到节点。
func (s *SAMGService) AddNodePointer(ctx context.Context, nodeID string, ptr Pointer) error {
	return s.store.AppendPointer(ctx, nodeID, ptr)
}

// findSeedNodes 从 topic 找到种子节点 ID。
func (s *SAMGService) findSeedNodes(ctx context.Context, topic string) []string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil
	}

	entities, _ := s.store.GetEntities(ctx)
	var seeds []string

	topicLower := strings.ToLower(topic)
	words := strings.Fields(topicLower)

	for _, entity := range entities {
		labelLower := strings.ToLower(entity.Label)
		// 精确匹配
		if labelLower == topicLower {
			seeds = append(seeds, entity.ID)
			continue
		}
		// 关键词匹配
		for _, word := range words {
			if strings.Contains(labelLower, word) {
				seeds = append(seeds, entity.ID)
				break
			}
		}
		// 别名匹配
		for _, alias := range entity.Aliases {
			if strings.Contains(strings.ToLower(alias), topicLower) {
				seeds = append(seeds, entity.ID)
				break
			}
		}
	}

	return seeds
}

// buildContextBlock 从激活节点构建上下文文本块。
func (s *SAMGService) buildContextBlock(nodes []QueryMemoryNode) string {
	if len(nodes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 相关记忆\n\n")

	for _, node := range nodes {
		sb.WriteString(fmt.Sprintf("### %s (关联度: %.2f)\n", node.Label, node.Activation))
		if len(node.Pointers) > 0 {
			for _, ptr := range node.Pointers {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", ptr.SourceType, ptr.Summary))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
