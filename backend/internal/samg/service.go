// Package samg - SAMG Service for unified graph operations
package samg

import (
	"context"
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
	TriggerIncrementalExtraction(ctx context.Context, sessionID string, messageIndex int, content string) error

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
	ImportGraph(ctx context.Context, graph *JsonLdGraph) error
	GetStats(ctx context.Context) (*SAMGStats, error)

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
func (s *SAMGService) ImportGraph(ctx context.Context, graph *JsonLdGraph) error {
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
			"min_confidence":   s.extractor.config.MinConfidence,
			"max_triples":      s.extractor.config.MaxTriplesPerExtraction,
			"rule_based":       s.extractor.config.EnableRuleBasedExtraction,
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
