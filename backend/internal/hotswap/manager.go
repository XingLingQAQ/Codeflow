package hotswap

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/adapters"
)

// HotSwapManager 热切换管理器实现
type HotSwapManager struct {
	config         HotSwapConfig
	models         map[string]*ModelInfo
	adaptersMap    map[string]adapters.ICliAdapter
	currentModelID string
	currentAdapter adapters.ICliAdapter
	failureCount   map[string]int
	mu             sync.RWMutex
}

// NewHotSwapManager 创建热切换管理器
func NewHotSwapManager(config *HotSwapConfig) *HotSwapManager {
	cfg := DefaultHotSwapConfig
	if config != nil {
		cfg = *config
	}

	manager := &HotSwapManager{
		config:       cfg,
		models:       make(map[string]*ModelInfo),
		adaptersMap:  make(map[string]adapters.ICliAdapter),
		failureCount: make(map[string]int),
	}

	// 初始化预定义模型
	for _, model := range PredefinedModels {
		m := model
		manager.models[model.ID] = &m
	}

	return manager
}

// RegisterModel 注册模型
func (m *HotSwapManager) RegisterModel(model *ModelInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models[model.ID] = model
}

// RegisterAdapter 注册适配器
func (m *HotSwapManager) RegisterAdapter(modelID string, adapter adapters.ICliAdapter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.adaptersMap[modelID] = adapter
	if m.currentAdapter == nil {
		m.currentAdapter = adapter
		m.currentModelID = modelID
	}
}

// GetAvailableModels 获取可用模型列表
func (m *HotSwapManager) GetAvailableModels() []ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var available []ModelInfo
	for _, model := range m.models {
		if model.Available {
			available = append(available, *model)
		}
	}
	return available
}

// GetCurrentModel 获取当前模型
func (m *HotSwapManager) GetCurrentModel() *ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentModelID == "" {
		return nil
	}
	if model, ok := m.models[m.currentModelID]; ok {
		return model
	}
	return nil
}

// GetModelInfo 获取模型信息
func (m *HotSwapManager) GetModelInfo(modelID string) *ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.models[modelID]
}

// GetCurrentAdapter 获取当前适配器
func (m *HotSwapManager) GetCurrentAdapter() adapters.ICliAdapter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentAdapter
}

// CanSwitch 检查是否可以切换
func (m *HotSwapManager) CanSwitch(modelID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	model, ok := m.models[modelID]
	if !ok {
		return false
	}
	if !model.Available {
		return false
	}
	if model.Status == StatusOffline {
		return false
	}
	_, hasAdapter := m.adaptersMap[modelID]
	return hasAdapter
}

// SwitchModel 切换模型
func (m *HotSwapManager) SwitchModel(modelID string, options *SwitchOptions) (*SwitchResult, error) {
	opts := DefaultSwitchOptions
	if options != nil {
		opts = *options
	}

	m.mu.Lock()
	previousModel := m.currentModelID
	if previousModel == "" {
		previousModel = "none"
	}
	m.mu.Unlock()

	// 检查目标模型
	model := m.GetModelInfo(modelID)
	if model == nil {
		return &SwitchResult{
			Success:       false,
			PreviousModel: previousModel,
			CurrentModel:  previousModel,
			Error:         fmt.Sprintf("model %s not found", modelID),
		}, nil
	}

	if !m.CanSwitch(modelID) {
		if opts.FallbackOnError {
			return m.Relay(nil)
		}
		return &SwitchResult{
			Success:       false,
			PreviousModel: previousModel,
			CurrentModel:  previousModel,
			Error:         fmt.Sprintf("cannot switch to model %s", modelID),
		}, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取当前历史
	var history []adapters.Message
	var tokensMigrated int

	if opts.PreserveHistory && m.currentAdapter != nil {
		history = m.currentAdapter.GetHistory()
	}

	// 切换到新适配器
	newAdapter, ok := m.adaptersMap[modelID]
	if !ok {
		return &SwitchResult{
			Success:       false,
			PreviousModel: previousModel,
			CurrentModel:  previousModel,
			Error:         fmt.Sprintf("adapter for %s not registered", modelID),
		}, nil
	}

	// 迁移上下文
	if opts.MigrateContext && len(history) > 0 {
		migratedHistory := m.truncateHistory(history, model.ContextWindow)
		newAdapter.SetHistory(migratedHistory)
		tokensMigrated = m.estimateTokens(migratedHistory)
	} else if opts.PreserveHistory && len(history) > 0 {
		newAdapter.SetHistory(history)
		tokensMigrated = m.estimateTokens(history)
	}

	m.currentAdapter = newAdapter
	m.currentModelID = modelID
	m.failureCount[modelID] = 0

	return &SwitchResult{
		Success:         true,
		PreviousModel:   previousModel,
		CurrentModel:    modelID,
		ContextMigrated: opts.MigrateContext,
		TokensMigrated:  tokensMigrated,
	}, nil
}

// Retry 重试当前模型
func (m *HotSwapManager) Retry(strategy *RetryStrategy) (*SwitchResult, error) {
	s := m.config.RetryStrategy
	if strategy != nil {
		s = *strategy
	}

	m.mu.RLock()
	currentModel := m.currentModelID
	failures := m.failureCount[currentModel]
	m.mu.RUnlock()

	if currentModel == "" {
		return &SwitchResult{
			Success:       false,
			PreviousModel: "none",
			CurrentModel:  "none",
			Error:         "no current model to retry",
		}, nil
	}

	if failures >= s.MaxRetries {
		return m.Relay(nil)
	}

	// 计算退避延迟
	delay := time.Duration(float64(s.BaseDelay) * math.Pow(s.BackoffMultiplier, float64(failures)))
	if delay > s.MaxDelay {
		delay = s.MaxDelay
	}

	time.Sleep(delay)

	// 增加失败计数
	m.mu.Lock()
	m.failureCount[currentModel]++
	m.mu.Unlock()

	return &SwitchResult{
		Success:       true,
		PreviousModel: currentModel,
		CurrentModel:  currentModel,
	}, nil
}

// Relay 接力到备用模型
func (m *HotSwapManager) Relay(fallbackChain []string) (*SwitchResult, error) {
	chain := m.config.RelayConfig.FallbackChain
	if fallbackChain != nil {
		chain = fallbackChain
	}

	m.mu.RLock()
	currentModel := m.currentModelID
	m.mu.RUnlock()

	// 找到当前模型在链中的位置
	startIndex := 0
	for i, modelID := range chain {
		if modelID == currentModel {
			startIndex = i + 1
			break
		}
	}

	// 尝试链中的下一个模型
	for i := startIndex; i < len(chain); i++ {
		modelID := chain[i]
		if m.CanSwitch(modelID) {
			return m.SwitchModel(modelID, &SwitchOptions{
				PreserveHistory: true,
				MigrateContext:  true,
				FallbackOnError: false,
			})
		}
	}

	// 没有可用的备用模型
	return &SwitchResult{
		Success:       false,
		PreviousModel: currentModel,
		CurrentModel:  currentModel,
		Error:         "no available fallback model",
	}, nil
}

// MigrateContext 迁移上下文
func (m *HotSwapManager) MigrateContext(targetModel string) (*ContextMigrationResult, error) {
	m.mu.RLock()
	currentAdapter := m.currentAdapter
	m.mu.RUnlock()

	if currentAdapter == nil {
		return &ContextMigrationResult{
			Success:  false,
			Messages: []adapters.Message{},
		}, nil
	}

	history := currentAdapter.GetHistory()
	originalTokens := m.estimateTokens(history)

	// 获取目标模型的上下文窗口
	targetModelInfo := m.GetModelInfo(targetModel)
	if targetModelInfo == nil {
		return &ContextMigrationResult{
			Success:        false,
			OriginalTokens: originalTokens,
			Messages:       history,
		}, nil
	}

	// 截断历史以适应目标模型
	migratedHistory := m.truncateHistory(history, targetModelInfo.ContextWindow)
	migratedTokens := m.estimateTokens(migratedHistory)

	return &ContextMigrationResult{
		Success:        true,
		OriginalTokens: originalTokens,
		MigratedTokens: migratedTokens,
		Truncated:      len(migratedHistory) < len(history),
		Messages:       migratedHistory,
	}, nil
}

// Configure 配置管理器
func (m *HotSwapManager) Configure(config *HotSwapConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if config != nil {
		m.config = *config
	}
}

// SetRelayConfig 设置接力配置
func (m *HotSwapManager) SetRelayConfig(config *RelayConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if config != nil {
		m.config.RelayConfig = *config
	}
}

// RecordFailure 记录失败
func (m *HotSwapManager) RecordFailure(modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failureCount[modelID]++
}

// GetFailureCount 获取失败次数
func (m *HotSwapManager) GetFailureCount(modelID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.failureCount[modelID]
}

// ResetFailureCount 重置失败计数
func (m *HotSwapManager) ResetFailureCount(modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failureCount[modelID] = 0
}

// 辅助方法

func (m *HotSwapManager) estimateTokens(messages []adapters.Message) int {
	total := 0
	for _, msg := range messages {
		// 继续沿用粗估口径，但字符数统一复用 adapters 真相源。
		total += adapters.ApproxMessageChars(msg) / 4
	}
	return total
}

func (m *HotSwapManager) truncateHistory(history []adapters.Message, maxTokens int) []adapters.Message {
	if maxTokens <= 0 {
		return history
	}

	// 从最新消息开始，保留到token限制
	result := make([]adapters.Message, 0, len(history))
	currentTokens := 0

	for i := len(history) - 1; i >= 0; i-- {
		msgTokens := adapters.ApproxMessageChars(history[i]) / 4
		if currentTokens+msgTokens > maxTokens/2 { // 保留一半给输出
			break
		}
		result = append([]adapters.Message{history[i]}, result...)
		currentTokens += msgTokens
	}

	return result
}
