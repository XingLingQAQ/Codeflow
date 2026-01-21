// Package samg - BLA (Base-Level Activation) decay algorithm
package samg

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"
)

// DecayConfig BLA衰减配置
type DecayConfig struct {
	DecayRate       float64 `json:"decay_rate"`        // 衰减率 (d)
	BaseActivation  float64 `json:"base_activation"`   // 基础激活值
	MinActivation   float64 `json:"min_activation"`    // 最小激活阈值
	HideThreshold   float64 `json:"hide_threshold"`    // 隐藏阈值
	BoostOnAccess   float64 `json:"boost_on_access"`   // 访问时的激活提升
	TimeUnit        int64   `json:"time_unit"`         // 时间单位(毫秒)
	EnableAutoDecay bool    `json:"enable_auto_decay"` // 启用自动衰减
}

// DefaultDecayConfig 默认衰减配置
var DefaultDecayConfig = DecayConfig{
	DecayRate:       0.5,
	BaseActivation:  1.0,
	MinActivation:   0.01,
	HideThreshold:   0.1,
	BoostOnAccess:   0.3,
	TimeUnit:        3600000, // 1小时
	EnableAutoDecay: true,
}

// NodeActivation 节点激活状态
type NodeActivation struct {
	NodeID         string    `json:"node_id"`
	Label          string    `json:"label"`
	Type           string    `json:"type"`
	Activation     float64   `json:"activation"`
	AccessCount    int       `json:"access_count"`
	LastAccessTime int64     `json:"last_access_time"`
	CreatedTime    int64     `json:"created_time"`
	AccessHistory  []int64   `json:"access_history"`
	Hidden         bool      `json:"hidden"`
}

// DecayManager BLA衰减管理器
type DecayManager struct {
	config      DecayConfig
	store       ITripleStore
	activations map[string]*NodeActivation
	mu          sync.RWMutex
}

// NewDecayManager 创建衰减管理器
func NewDecayManager(store ITripleStore, config *DecayConfig) *DecayManager {
	cfg := DefaultDecayConfig
	if config != nil {
		cfg = *config
	}
	return &DecayManager{
		config:      cfg,
		store:       store,
		activations: make(map[string]*NodeActivation),
	}
}

// RecordAccess 记录节点访问
func (dm *DecayManager) RecordAccess(ctx context.Context, nodeID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	now := time.Now().UnixMilli()

	activation, exists := dm.activations[nodeID]
	if !exists {
		// 从存储获取实体信息
		entity, _ := dm.store.GetEntity(ctx, nodeID)
		label := ""
		nodeType := ""
		if entity != nil {
			label = entity.Label
			if len(entity.Type) > 0 {
				nodeType = entity.Type[0]
			}
		}

		activation = &NodeActivation{
			NodeID:         nodeID,
			Label:          label,
			Type:           nodeType,
			Activation:     dm.config.BaseActivation,
			AccessCount:    0,
			CreatedTime:    now,
			LastAccessTime: now,
			AccessHistory:  make([]int64, 0),
			Hidden:         false,
		}
		dm.activations[nodeID] = activation
	}

	// 更新访问记录
	activation.AccessCount++
	activation.LastAccessTime = now
	activation.AccessHistory = append(activation.AccessHistory, now)

	// 限制历史记录长度
	if len(activation.AccessHistory) > 100 {
		activation.AccessHistory = activation.AccessHistory[len(activation.AccessHistory)-100:]
	}

	// 提升激活值
	activation.Activation += dm.config.BoostOnAccess

	// 取消隐藏
	if activation.Hidden {
		activation.Hidden = false
	}

	return nil
}

// CalculateBLA 计算BLA激活值
// BLA公式: B_i = ln(sum(t_j^(-d))) + β
// 其中 t_j 是第j次访问到现在的时间，d是衰减率
func (dm *DecayManager) CalculateBLA(nodeID string) float64 {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	activation, exists := dm.activations[nodeID]
	if !exists {
		return 0
	}

	now := time.Now().UnixMilli()
	var sum float64

	for _, accessTime := range activation.AccessHistory {
		// 计算时间差（以时间单位为单位）
		timeDiff := float64(now-accessTime) / float64(dm.config.TimeUnit)
		if timeDiff < 0.001 {
			timeDiff = 0.001 // 避免除零
		}

		// t^(-d)
		sum += math.Pow(timeDiff, -dm.config.DecayRate)
	}

	if sum <= 0 {
		return dm.config.MinActivation
	}

	// ln(sum) + base
	bla := math.Log(sum) + dm.config.BaseActivation

	if bla < dm.config.MinActivation {
		return dm.config.MinActivation
	}

	return bla
}

// ApplyDecay 应用衰减到所有节点
func (dm *DecayManager) ApplyDecay(ctx context.Context) (int, int, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	decayed := 0
	hidden := 0

	for nodeID, activation := range dm.activations {
		// 计算新的BLA值
		newActivation := dm.calculateBLALocked(nodeID)
		activation.Activation = newActivation

		decayed++

		// 检查是否需要隐藏
		if newActivation < dm.config.HideThreshold && !activation.Hidden {
			activation.Hidden = true
			hidden++
		}
	}

	return decayed, hidden, nil
}

// calculateBLALocked 内部BLA计算（需要持有锁）
func (dm *DecayManager) calculateBLALocked(nodeID string) float64 {
	activation, exists := dm.activations[nodeID]
	if !exists {
		return 0
	}

	now := time.Now().UnixMilli()
	var sum float64

	for _, accessTime := range activation.AccessHistory {
		timeDiff := float64(now-accessTime) / float64(dm.config.TimeUnit)
		if timeDiff < 0.001 {
			timeDiff = 0.001
		}
		sum += math.Pow(timeDiff, -dm.config.DecayRate)
	}

	if sum <= 0 {
		return dm.config.MinActivation
	}

	bla := math.Log(sum) + dm.config.BaseActivation

	if bla < dm.config.MinActivation {
		return dm.config.MinActivation
	}

	return bla
}

// GetVisibleNodes 获取可见节点（未隐藏）
func (dm *DecayManager) GetVisibleNodes(ctx context.Context) []NodeActivation {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var visible []NodeActivation
	for _, activation := range dm.activations {
		if !activation.Hidden {
			visible = append(visible, *activation)
		}
	}

	// 按激活值排序
	sort.Slice(visible, func(i, j int) bool {
		return visible[i].Activation > visible[j].Activation
	})

	return visible
}

// GetHiddenNodes 获取隐藏节点
func (dm *DecayManager) GetHiddenNodes(ctx context.Context) []NodeActivation {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var hidden []NodeActivation
	for _, activation := range dm.activations {
		if activation.Hidden {
			hidden = append(hidden, *activation)
		}
	}

	return hidden
}

// GetTopNodes 获取激活值最高的N个节点
func (dm *DecayManager) GetTopNodes(ctx context.Context, n int) []NodeActivation {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	nodes := make([]NodeActivation, 0, len(dm.activations))
	for _, activation := range dm.activations {
		nodes = append(nodes, *activation)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Activation > nodes[j].Activation
	})

	if n > 0 && n < len(nodes) {
		return nodes[:n]
	}
	return nodes
}

// GetNodeActivation 获取节点激活状态
func (dm *DecayManager) GetNodeActivation(nodeID string) (*NodeActivation, bool) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	activation, exists := dm.activations[nodeID]
	if !exists {
		return nil, false
	}

	// 返回副本
	copy := *activation
	return &copy, true
}

// SetHidden 设置节点隐藏状态
func (dm *DecayManager) SetHidden(nodeID string, hidden bool) bool {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	activation, exists := dm.activations[nodeID]
	if !exists {
		return false
	}

	activation.Hidden = hidden
	return true
}

// GetConfig 获取配置
func (dm *DecayManager) GetConfig() DecayConfig {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.config
}

// UpdateConfig 更新配置
func (dm *DecayManager) UpdateConfig(config DecayConfig) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.config = config
}

// GetStats 获取统计信息
func (dm *DecayManager) GetStats() map[string]interface{} {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	totalNodes := len(dm.activations)
	hiddenCount := 0
	totalAccess := 0
	var avgActivation float64

	for _, activation := range dm.activations {
		if activation.Hidden {
			hiddenCount++
		}
		totalAccess += activation.AccessCount
		avgActivation += activation.Activation
	}

	if totalNodes > 0 {
		avgActivation /= float64(totalNodes)
	}

	return map[string]interface{}{
		"total_nodes":       totalNodes,
		"hidden_nodes":      hiddenCount,
		"visible_nodes":     totalNodes - hiddenCount,
		"total_accesses":    totalAccess,
		"average_activation": avgActivation,
		"config":            dm.config,
	}
}

// Clear 清空所有激活状态
func (dm *DecayManager) Clear() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.activations = make(map[string]*NodeActivation)
}
