// Package samg - Spreading Activation for multi-hop reasoning
package samg

import (
	"context"
	"math"
	"sort"
	"sync"
)

// ActivationConfig 扩展激活配置
type ActivationConfig struct {
	InitialActivation float64 `json:"initial_activation"`
	DecayFactor       float64 `json:"decay_factor"`
	FiringThreshold   float64 `json:"firing_threshold"`
	MaxHops           int     `json:"max_hops"`
	MaxActivatedNodes int     `json:"max_activated_nodes"`
	SpreadingFactor   float64 `json:"spreading_factor"`
}

// DefaultActivationConfig 默认配置
var DefaultActivationConfig = ActivationConfig{
	InitialActivation: 1.0,
	DecayFactor:       0.5,
	FiringThreshold:   0.1,
	MaxHops:           3,
	MaxActivatedNodes: 100,
	SpreadingFactor:   0.7,
}

// ActivatedNode 激活节点
type ActivatedNode struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	Type       string  `json:"type"`
	Activation float64 `json:"activation"`
	Hop        int     `json:"hop"`
	Path       []string `json:"path"`
}

// ActivationResult 激活结果
type ActivationResult struct {
	SourceNodes    []string         `json:"source_nodes"`
	ActivatedNodes []ActivatedNode  `json:"activated_nodes"`
	TotalHops      int              `json:"total_hops"`
	PathsFound     int              `json:"paths_found"`
}

// SpreadingActivation 扩展激活引擎
type SpreadingActivation struct {
	config ActivationConfig
	store  ITripleStore
	mu     sync.RWMutex
}

// NewSpreadingActivation 创建扩展激活引擎
func NewSpreadingActivation(store ITripleStore, config *ActivationConfig) *SpreadingActivation {
	cfg := DefaultActivationConfig
	if config != nil {
		cfg = *config
	}
	return &SpreadingActivation{
		config: cfg,
		store:  store,
	}
}

// Activate 从源节点开始扩展激活
func (sa *SpreadingActivation) Activate(ctx context.Context, sourceIDs []string) (*ActivationResult, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	activations := make(map[string]*ActivatedNode)
	visited := make(map[string]bool)

	// 初始化源节点
	for _, id := range sourceIDs {
		entity, err := sa.store.GetEntity(ctx, id)
		if err != nil {
			continue
		}

		nodeType := ""
		if entity != nil && len(entity.Type) > 0 {
			nodeType = entity.Type[0]
		}
		label := ""
		if entity != nil {
			label = entity.Label
		}

		activations[id] = &ActivatedNode{
			ID:         id,
			Label:      label,
			Type:       nodeType,
			Activation: sa.config.InitialActivation,
			Hop:        0,
			Path:       []string{id},
		}
		visited[id] = true
	}

	// 多跳扩展
	currentHop := 0
	for currentHop < sa.config.MaxHops && len(activations) < sa.config.MaxActivatedNodes {
		newActivations := make(map[string]*ActivatedNode)

		for nodeID, node := range activations {
			if node.Hop != currentHop {
				continue
			}

			// 获取相邻节点
			neighbors, err := sa.getNeighbors(ctx, nodeID)
			if err != nil {
				continue
			}

			for _, neighbor := range neighbors {
				if visited[neighbor.ID] {
					// 已访问节点：累加激活值
					if existing, ok := activations[neighbor.ID]; ok {
						existing.Activation += node.Activation * sa.config.SpreadingFactor * sa.config.DecayFactor
					}
					continue
				}

				// 计算传播激活值
				spreadActivation := node.Activation * sa.config.SpreadingFactor * math.Pow(sa.config.DecayFactor, float64(currentHop+1))

				if spreadActivation < sa.config.FiringThreshold {
					continue
				}

				newPath := make([]string, len(node.Path)+1)
				copy(newPath, node.Path)
				newPath[len(node.Path)] = neighbor.ID

				newActivations[neighbor.ID] = &ActivatedNode{
					ID:         neighbor.ID,
					Label:      neighbor.Label,
					Type:       neighbor.Type,
					Activation: spreadActivation,
					Hop:        currentHop + 1,
					Path:       newPath,
				}
				visited[neighbor.ID] = true
			}
		}

		// 合并新激活节点
		for id, node := range newActivations {
			activations[id] = node
		}

		if len(newActivations) == 0 {
			break
		}

		currentHop++
	}

	// 转换为结果
	result := &ActivationResult{
		SourceNodes:    sourceIDs,
		ActivatedNodes: make([]ActivatedNode, 0, len(activations)),
		TotalHops:      currentHop,
	}

	for _, node := range activations {
		result.ActivatedNodes = append(result.ActivatedNodes, *node)
		if len(node.Path) > 1 {
			result.PathsFound++
		}
	}

	// 按激活值排序
	sort.Slice(result.ActivatedNodes, func(i, j int) bool {
		return result.ActivatedNodes[i].Activation > result.ActivatedNodes[j].Activation
	})

	// 限制返回数量
	if len(result.ActivatedNodes) > sa.config.MaxActivatedNodes {
		result.ActivatedNodes = result.ActivatedNodes[:sa.config.MaxActivatedNodes]
	}

	return result, nil
}

// FindPaths 查找两个节点之间的路径
func (sa *SpreadingActivation) FindPaths(ctx context.Context, sourceID, targetID string, maxHops int) ([][]string, error) {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if maxHops <= 0 {
		maxHops = sa.config.MaxHops
	}

	var paths [][]string
	visited := make(map[string]bool)

	var dfs func(current string, path []string, depth int)
	dfs = func(current string, path []string, depth int) {
		if depth > maxHops {
			return
		}

		if current == targetID {
			pathCopy := make([]string, len(path))
			copy(pathCopy, path)
			paths = append(paths, pathCopy)
			return
		}

		if visited[current] {
			return
		}
		visited[current] = true
		defer func() { visited[current] = false }()

		neighbors, err := sa.getNeighbors(ctx, current)
		if err != nil {
			return
		}

		for _, neighbor := range neighbors {
			newPath := append(path, neighbor.ID)
			dfs(neighbor.ID, newPath, depth+1)
		}
	}

	dfs(sourceID, []string{sourceID}, 0)

	return paths, nil
}

// getNeighbors 获取节点的相邻节点
func (sa *SpreadingActivation) getNeighbors(ctx context.Context, nodeID string) ([]ActivatedNode, error) {
	var neighbors []ActivatedNode
	seen := make(map[string]bool)

	// 作为主语的三元组
	subjectTriples, err := sa.store.FindBySubject(ctx, nodeID)
	if err == nil {
		for _, triple := range subjectTriples {
			if triple.Object.Node != nil && !seen[triple.Object.Node.ID] {
				seen[triple.Object.Node.ID] = true
				nodeType := ""
				if len(triple.Object.Node.Type) > 0 {
					nodeType = triple.Object.Node.Type[0]
				}
				neighbors = append(neighbors, ActivatedNode{
					ID:    triple.Object.Node.ID,
					Label: triple.Object.Node.Label,
					Type:  nodeType,
				})
			}
		}
	}

	// 作为宾语的三元组
	objectTriples, err := sa.store.FindByObject(ctx, nodeID)
	if err == nil {
		for _, triple := range objectTriples {
			if !seen[triple.Subject.ID] {
				seen[triple.Subject.ID] = true
				nodeType := ""
				if len(triple.Subject.Type) > 0 {
					nodeType = triple.Subject.Type[0]
				}
				neighbors = append(neighbors, ActivatedNode{
					ID:    triple.Subject.ID,
					Label: triple.Subject.Label,
					Type:  nodeType,
				})
			}
		}
	}

	return neighbors, nil
}

// GetConfig 获取配置
func (sa *SpreadingActivation) GetConfig() ActivationConfig {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.config
}

// UpdateConfig 更新配置
func (sa *SpreadingActivation) UpdateConfig(config ActivationConfig) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.config = config
}
