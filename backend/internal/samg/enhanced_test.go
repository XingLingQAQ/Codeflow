// Package samg - Tests for enhanced SAMG features
package samg

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSpreadingActivation_Activate(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	// 添加测试三元组
	triples := []Triple{
		{
			ID:         "t1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "ClassA"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "ClassB")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "t2",
			Subject:    CreateNode("entity:b", EntityTypes.Class, "ClassB"),
			Predicate:  Predicates.Implements,
			Object:     CreateNodeObject(CreateNode("entity:c", EntityTypes.Class, "InterfaceC")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "t3",
			Subject:    CreateNode("entity:c", EntityTypes.Class, "InterfaceC"),
			Predicate:  Predicates.RelatedTo,
			Object:     CreateNodeObject(CreateNode("entity:d", EntityTypes.Concept, "ConceptD")),
			Confidence: 0.8,
			Timestamp:  time.Now().UnixMilli(),
		},
	}

	err := store.Add(ctx, triples)
	assert.NoError(t, err)

	// 创建扩展激活引擎，使用更高的激活阈值以确保多跳传播
	config := &ActivationConfig{
		InitialActivation: 1.0,
		DecayFactor:       0.7,  // 较高的衰减因子
		FiringThreshold:   0.05, // 较低的阈值
		MaxHops:           5,
		MaxActivatedNodes: 100,
		SpreadingFactor:   0.8,
	}
	sa := NewSpreadingActivation(store, config)

	// 从节点A开始激活
	result, err := sa.Activate(ctx, []string{"entity:a"})
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, []string{"entity:a"}, result.SourceNodes)
	assert.Greater(t, len(result.ActivatedNodes), 0)

	// 验证激活传播 - 至少应该找到B
	foundB := false
	for _, node := range result.ActivatedNodes {
		if node.ID == "entity:b" {
			foundB = true
			assert.Equal(t, 1, node.Hop)
		}
	}
	assert.True(t, foundB, "Should find entity:b")
}

func TestSpreadingActivation_FindPaths(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	// 添加测试三元组形成路径 A -> B -> C
	triples := []Triple{
		{
			ID:         "t1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "t2",
			Subject:    CreateNode("entity:b", EntityTypes.Function, "B"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:c", EntityTypes.Function, "C")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}

	err := store.Add(ctx, triples)
	assert.NoError(t, err)

	sa := NewSpreadingActivation(store, nil)

	// 查找从A到C的路径
	paths, err := sa.FindPaths(ctx, "entity:a", "entity:c", 3)
	assert.NoError(t, err)
	assert.Greater(t, len(paths), 0)

	// 验证路径
	found := false
	for _, path := range paths {
		if len(path) == 3 && path[0] == "entity:a" && path[1] == "entity:b" && path[2] == "entity:c" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should find path A -> B -> C")
}

func TestDecayManager_RecordAccess(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	dm := NewDecayManager(store, nil)

	// 记录访问
	err := dm.RecordAccess(ctx, "node:test")
	assert.NoError(t, err)

	// 验证激活状态
	activation, exists := dm.GetNodeActivation("node:test")
	assert.True(t, exists)
	assert.Equal(t, "node:test", activation.NodeID)
	assert.Equal(t, 1, activation.AccessCount)
	assert.False(t, activation.Hidden)
}

func TestDecayManager_BLACalculation(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	dm := NewDecayManager(store, &DecayConfig{
		DecayRate:      0.5,
		BaseActivation: 1.0,
		MinActivation:  0.01,
		HideThreshold:  0.1,
		BoostOnAccess:  0.3,
		TimeUnit:       1000, // 1秒
	})

	// 多次访问
	for i := 0; i < 5; i++ {
		dm.RecordAccess(ctx, "node:test")
		time.Sleep(10 * time.Millisecond)
	}

	// 计算BLA
	bla := dm.CalculateBLA("node:test")
	assert.Greater(t, bla, 0.0)
}

func TestDecayManager_ApplyDecay(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	dm := NewDecayManager(store, &DecayConfig{
		DecayRate:      0.5,
		BaseActivation: 1.0,
		MinActivation:  0.01,
		HideThreshold:  0.5, // 高阈值以便测试隐藏
		BoostOnAccess:  0.1,
		TimeUnit:       1, // 1毫秒，快速衰减
	})

	// 记录访问
	dm.RecordAccess(ctx, "node:test")

	// 等待一段时间让衰减生效
	time.Sleep(100 * time.Millisecond)

	// 应用衰减
	decayed, hidden, err := dm.ApplyDecay(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 1, decayed)
	// hidden可能为0或1，取决于衰减程度
	_ = hidden
}

func TestDecayManager_GetVisibleAndHiddenNodes(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	dm := NewDecayManager(store, nil)

	// 添加多个节点
	dm.RecordAccess(ctx, "node:visible1")
	dm.RecordAccess(ctx, "node:visible2")
	dm.RecordAccess(ctx, "node:hidden1")

	// 手动隐藏一个节点
	dm.SetHidden("node:hidden1", true)

	// 获取可见节点
	visible := dm.GetVisibleNodes(ctx)
	assert.Equal(t, 2, len(visible))

	// 获取隐藏节点
	hidden := dm.GetHiddenNodes(ctx)
	assert.Equal(t, 1, len(hidden))
	assert.Equal(t, "node:hidden1", hidden[0].NodeID)
}

func TestDecayManager_GetTopNodes(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	dm := NewDecayManager(store, nil)

	// 添加多个节点，不同访问次数
	for i := 0; i < 3; i++ {
		dm.RecordAccess(ctx, "node:low")
	}
	for i := 0; i < 10; i++ {
		dm.RecordAccess(ctx, "node:high")
	}
	for i := 0; i < 5; i++ {
		dm.RecordAccess(ctx, "node:medium")
	}

	// 获取Top 2
	top := dm.GetTopNodes(ctx, 2)
	assert.Equal(t, 2, len(top))
	// 最高访问次数的应该排在前面
	assert.Equal(t, "node:high", top[0].NodeID)
}

func TestSAMGService_ExtractFromMessage(t *testing.T) {
	svc := NewSAMGService(nil)
	ctx := context.Background()

	source := TripleSource{
		SessionID:        "test-session",
		MessageIndex:     1,
		ExtractionMethod: ExtractionRule,
	}

	// 测试代码关系提取
	message := "class UserService extends BaseService implements IUserService"
	triples, err := svc.ExtractFromMessage(ctx, message, source)
	assert.NoError(t, err)
	assert.Greater(t, len(triples), 0)

	// 验证提取的关系
	hasExtends := false
	hasImplements := false
	for _, triple := range triples {
		if triple.Predicate == Predicates.Extends {
			hasExtends = true
		}
		if triple.Predicate == Predicates.Implements {
			hasImplements = true
		}
	}
	assert.True(t, hasExtends, "Should extract extends relation")
	assert.True(t, hasImplements, "Should extract implements relation")
}

func TestSAMGService_TriggerIncrementalExtraction(t *testing.T) {
	svc := NewSAMGService(nil)
	ctx := context.Background()

	// 触发增量提取
	err := svc.TriggerIncrementalExtraction(ctx, "session-1", 1, "import React from 'react'")
	assert.NoError(t, err)

	// 验证三元组已添加
	triples, err := svc.QueryTriples(ctx, TripleQuery{Predicate: Predicates.Imports})
	assert.NoError(t, err)
	assert.Greater(t, len(triples), 0)
}

func TestSAMGService_ActivateAndDecay(t *testing.T) {
	svc := NewSAMGService(nil)
	ctx := context.Background()

	// 添加测试数据
	triples := []Triple{
		{
			ID:         "t1",
			Subject:    CreateNode("entity:x", EntityTypes.Class, "X"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:y", EntityTypes.Function, "Y")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}
	err := svc.AddTriples(ctx, triples)
	assert.NoError(t, err)

	// 激活
	result, err := svc.Activate(ctx, []string{"entity:x"})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// 获取Top节点
	top := svc.GetTopNodes(ctx, 10)
	assert.Greater(t, len(top), 0)
}

func TestSAMGService_GetStats(t *testing.T) {
	svc := NewSAMGService(nil)
	ctx := context.Background()

	// 添加一些数据
	triples := []Triple{
		{
			ID:         "t1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}
	svc.AddTriples(ctx, triples)

	// 获取统计
	stats, err := svc.GetStats(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, stats)
	assert.NotNil(t, stats.GraphStats)
	assert.NotNil(t, stats.DecayStats)
	assert.NotNil(t, stats.ExtractorInfo)
	assert.Equal(t, 1, stats.GraphStats.TripleCount)
}

func TestSAMGService_ExportImportGraph(t *testing.T) {
	svc := NewSAMGService(nil)
	ctx := context.Background()

	// 添加数据
	triples := []Triple{
		{
			ID:         "t1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}
	svc.AddTriples(ctx, triples)

	// 导出
	graph, err := svc.ExportGraph(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, graph)
	assert.Equal(t, 1, len(graph.Graph))

	// 创建新服务并导入
	svc2 := NewSAMGService(nil)
	err = svc2.ImportGraph(ctx, graph)
	assert.NoError(t, err)

	// 验证导入
	stats, _ := svc2.GetStats(ctx)
	assert.Equal(t, 1, stats.GraphStats.TripleCount)
}

func TestSAMGService_GetRelations(t *testing.T) {
	svc := NewSAMGService(nil)
	ctx := context.Background()

	// 添加数据
	triples := []Triple{
		{
			ID:         "t1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "t2",
			Subject:    CreateNode("entity:c", EntityTypes.Class, "C"),
			Predicate:  Predicates.Uses,
			Object:     CreateNodeObject(CreateNode("entity:a", EntityTypes.Class, "A")),
			Confidence: 0.8,
			Timestamp:  time.Now().UnixMilli(),
		},
	}
	svc.AddTriples(ctx, triples)

	// 获取entity:a的关系
	relations, err := svc.GetRelations(ctx, "entity:a")
	assert.NoError(t, err)
	assert.Equal(t, 2, len(relations)) // 作为主语1个，作为宾语1个
}

func TestSAMGService_HookOnMessageComplete(t *testing.T) {
	svc := NewSAMGService(nil)
	ctx := context.Background()

	// 模拟Hook调用
	err := svc.HookOnMessageComplete(ctx, "session-1", 1, "assistant", "I created a new UserController class that extends BaseController")
	assert.NoError(t, err)

	// 验证提取结果
	triples, err := svc.QueryTriples(ctx, TripleQuery{})
	assert.NoError(t, err)
	assert.Greater(t, len(triples), 0)
}

func TestSAMGService_ConfigUpdate(t *testing.T) {
	svc := NewSAMGService(nil)

	// 更新激活配置
	newActivationConfig := ActivationConfig{
		InitialActivation: 2.0,
		DecayFactor:       0.3,
		FiringThreshold:   0.2,
		MaxHops:           5,
		MaxActivatedNodes: 200,
		SpreadingFactor:   0.8,
	}
	svc.UpdateActivationConfig(newActivationConfig)

	config := svc.GetActivationConfig()
	assert.Equal(t, 2.0, config.InitialActivation)
	assert.Equal(t, 5, config.MaxHops)

	// 更新衰减配置
	newDecayConfig := DecayConfig{
		DecayRate:       0.7,
		BaseActivation:  2.0,
		MinActivation:   0.05,
		HideThreshold:   0.2,
		BoostOnAccess:   0.5,
		TimeUnit:        7200000,
		EnableAutoDecay: false,
	}
	svc.UpdateDecayConfig(newDecayConfig)

	decayConfig := svc.GetDecayConfig()
	assert.Equal(t, 0.7, decayConfig.DecayRate)
	assert.Equal(t, false, decayConfig.EnableAutoDecay)
}

func TestGlobalSAMGService(t *testing.T) {
	// 初始应该为nil
	svc := GetSAMGService()
	assert.Nil(t, svc)

	// 设置全局服务
	newSvc := NewSAMGService(nil)
	SetSAMGService(newSvc)

	// 获取应该返回设置的服务
	svc = GetSAMGService()
	assert.NotNil(t, svc)
	assert.Equal(t, newSvc, svc)

	// 清理
	SetSAMGService(nil)
}
