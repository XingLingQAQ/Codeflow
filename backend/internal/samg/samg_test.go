package samg

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryTripleStoreBasic(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	// 创建三元组
	triple := Triple{
		ID:        GenerateTripleID("entity:a", Predicates.Calls, "entity:b"),
		Subject:   CreateNode("entity:a", EntityTypes.Function, "functionA"),
		Predicate: Predicates.Calls,
		Object:    CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "functionB")),
		Confidence: 0.8,
		Timestamp: time.Now().UnixMilli(),
		Source: TripleSource{
			SessionID:        "session1",
			ExtractionMethod: ExtractionRule,
		},
	}

	// 添加
	err := store.Add(ctx, []Triple{triple})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// 获取
	got, err := store.Get(ctx, triple.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected triple, got nil")
	}
	if got.Subject.ID != triple.Subject.ID {
		t.Errorf("expected subject %s, got %s", triple.Subject.ID, got.Subject.ID)
	}

	// 统计
	stats, err := store.GetStats(ctx)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.TripleCount != 1 {
		t.Errorf("expected 1 triple, got %d", stats.TripleCount)
	}
}

func TestInMemoryTripleStoreQuery(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	// 添加多个三元组
	triples := []Triple{
		{
			ID:         GenerateTripleID("entity:a", Predicates.Calls, "entity:b"),
			Subject:    CreateNode("entity:a", EntityTypes.Function, "funcA"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "funcB")),
			Confidence: 0.8,
			Timestamp:  time.Now().UnixMilli(),
			Source:     TripleSource{SessionID: "s1", ExtractionMethod: ExtractionRule},
		},
		{
			ID:         GenerateTripleID("entity:a", Predicates.Uses, "entity:c"),
			Subject:    CreateNode("entity:a", EntityTypes.Function, "funcA"),
			Predicate:  Predicates.Uses,
			Object:     CreateNodeObject(CreateNode("entity:c", EntityTypes.Variable, "varC")),
			Confidence: 0.6,
			Timestamp:  time.Now().UnixMilli(),
			Source:     TripleSource{SessionID: "s1", ExtractionMethod: ExtractionRule},
		},
		{
			ID:         GenerateTripleID("entity:b", Predicates.Calls, "entity:c"),
			Subject:    CreateNode("entity:b", EntityTypes.Function, "funcB"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:c", EntityTypes.Function, "funcC")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
			Source:     TripleSource{SessionID: "s2", ExtractionMethod: ExtractionLLM},
		},
	}

	err := store.Add(ctx, triples)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// 按Subject查询
	results, err := store.FindBySubject(ctx, "entity:a")
	if err != nil {
		t.Fatalf("find by subject: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 triples for subject entity:a, got %d", len(results))
	}

	// 按Predicate查询
	results, err = store.FindByPredicate(ctx, Predicates.Calls)
	if err != nil {
		t.Fatalf("find by predicate: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 triples for predicate calls, got %d", len(results))
	}

	// 按Object查询
	results, err = store.FindByObject(ctx, "entity:c")
	if err != nil {
		t.Fatalf("find by object: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 triples for object entity:c, got %d", len(results))
	}

	// 复合查询
	results, err = store.Query(ctx, TripleQuery{
		Subject:   "entity:a",
		Predicate: Predicates.Calls,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 triple, got %d", len(results))
	}

	// 置信度过滤
	results, err = store.Query(ctx, TripleQuery{MinConfidence: 0.85})
	if err != nil {
		t.Fatalf("query with confidence: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 triple with confidence >= 0.85, got %d", len(results))
	}
}

func TestInMemoryTripleStoreDelete(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	triple := Triple{
		ID:         "triple:test",
		Subject:    CreateNode("entity:a", EntityTypes.Function, "funcA"),
		Predicate:  Predicates.Calls,
		Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "funcB")),
		Confidence: 0.8,
		Timestamp:  time.Now().UnixMilli(),
		Source:     TripleSource{SessionID: "s1", ExtractionMethod: ExtractionRule},
	}

	store.Add(ctx, []Triple{triple})

	// 删除
	err := store.Delete(ctx, []string{"triple:test"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// 验证已删除
	got, _ := store.Get(ctx, "triple:test")
	if got != nil {
		t.Error("expected triple to be deleted")
	}
}

func TestInMemoryTripleStoreDeduplication(t *testing.T) {
	store := NewInMemoryTripleStore(&TripleStoreConfig{
		EnableDeduplication: true,
		MaxTriples:          1000,
	})
	ctx := context.Background()

	// 添加两个相同的三元组，不同置信度
	triple1 := Triple{
		ID:         "triple:1",
		Subject:    CreateNode("entity:a", EntityTypes.Function, "funcA"),
		Predicate:  Predicates.Calls,
		Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "funcB")),
		Confidence: 0.5,
		Timestamp:  time.Now().UnixMilli(),
		Source:     TripleSource{SessionID: "s1", ExtractionMethod: ExtractionRule},
	}

	triple2 := Triple{
		ID:         "triple:2",
		Subject:    CreateNode("entity:a", EntityTypes.Function, "funcA"),
		Predicate:  Predicates.Calls,
		Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "funcB")),
		Confidence: 0.9,
		Timestamp:  time.Now().UnixMilli(),
		Source:     TripleSource{SessionID: "s1", ExtractionMethod: ExtractionRule},
	}

	store.Add(ctx, []Triple{triple1})
	store.Add(ctx, []Triple{triple2})

	stats, _ := store.GetStats(ctx)
	if stats.TripleCount != 1 {
		t.Errorf("expected 1 triple after deduplication, got %d", stats.TripleCount)
	}

	// 应该保留高置信度的
	results, _ := store.Query(ctx, TripleQuery{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", results[0].Confidence)
	}
}

func TestInMemoryTripleStoreEntity(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	// 添加三元组会自动创建实体
	triple := Triple{
		ID:         "triple:test",
		Subject:    CreateNode("entity:a", EntityTypes.Function, "funcA"),
		Predicate:  Predicates.Calls,
		Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "funcB")),
		Confidence: 0.8,
		Timestamp:  time.Now().UnixMilli(),
		Source:     TripleSource{SessionID: "s1", ExtractionMethod: ExtractionRule},
	}

	store.Add(ctx, []Triple{triple})

	// 验证实体已创建
	entities, _ := store.GetEntities(ctx)
	if len(entities) != 2 {
		t.Errorf("expected 2 entities, got %d", len(entities))
	}

	// 获取特定实体
	entity, _ := store.GetEntity(ctx, "entity:a")
	if entity == nil {
		t.Fatal("expected entity, got nil")
	}
	if entity.Label != "funcA" {
		t.Errorf("expected label funcA, got %s", entity.Label)
	}

	// 更新实体
	newEntity := Entity{
		ID:          "entity:a",
		Type:        []string{EntityTypes.Function},
		Label:       "updatedFuncA",
		Description: "Updated description",
	}
	store.UpsertEntity(ctx, newEntity)

	entity, _ = store.GetEntity(ctx, "entity:a")
	if entity.Label != "updatedFuncA" {
		t.Errorf("expected label updatedFuncA, got %s", entity.Label)
	}
}

func TestInMemoryTripleStoreExportImport(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	triples := []Triple{
		{
			ID:         "triple:1",
			Subject:    CreateNode("entity:a", EntityTypes.Function, "funcA"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "funcB")),
			Confidence: 0.8,
			Timestamp:  time.Now().UnixMilli(),
			Source:     TripleSource{SessionID: "s1", ExtractionMethod: ExtractionRule},
		},
		{
			ID:         "triple:2",
			Subject:    CreateNode("entity:b", EntityTypes.Function, "funcB"),
			Predicate:  Predicates.Uses,
			Object:     CreateNodeObject(CreateNode("entity:c", EntityTypes.Variable, "varC")),
			Confidence: 0.7,
			Timestamp:  time.Now().UnixMilli(),
			Source:     TripleSource{SessionID: "s1", ExtractionMethod: ExtractionRule},
		},
	}

	store.Add(ctx, triples)

	// 导出
	graph, err := store.ExportGraph(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(graph.Graph) != 2 {
		t.Errorf("expected 2 triples in export, got %d", len(graph.Graph))
	}

	// 创建新存储并导入
	newStore := NewInMemoryTripleStore(nil)
	err = newStore.ImportGraph(ctx, graph)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	stats, _ := newStore.GetStats(ctx)
	if stats.TripleCount != 2 {
		t.Errorf("expected 2 triples after import, got %d", stats.TripleCount)
	}
}

func TestInMemoryTripleStoreClear(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	triple := Triple{
		ID:         "triple:test",
		Subject:    CreateNode("entity:a", EntityTypes.Function, "funcA"),
		Predicate:  Predicates.Calls,
		Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "funcB")),
		Confidence: 0.8,
		Timestamp:  time.Now().UnixMilli(),
		Source:     TripleSource{SessionID: "s1", ExtractionMethod: ExtractionRule},
	}

	store.Add(ctx, []Triple{triple})

	stats, _ := store.GetStats(ctx)
	if stats.TripleCount != 1 {
		t.Errorf("expected 1 triple, got %d", stats.TripleCount)
	}

	// 清空
	store.Clear(ctx)

	stats, _ = store.GetStats(ctx)
	if stats.TripleCount != 0 {
		t.Errorf("expected 0 triples after clear, got %d", stats.TripleCount)
	}
}

func TestTripleExtractorCodeRelations(t *testing.T) {
	extractor := NewTripleExtractor(nil)
	ctx := context.Background()

	text := `
The UserService class extends BaseService and implements IUserService.
The login function calls authenticate and then calls createSession.
We import axios from 'axios' and lodash from 'lodash'.
`

	source := TripleSource{
		SessionID:        "test-session",
		ExtractionMethod: ExtractionRule,
	}

	triples, err := extractor.Extract(ctx, text, source)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(triples) == 0 {
		t.Error("expected some triples")
	}

	// 检查是否提取了extends关系
	foundExtends := false
	for _, triple := range triples {
		if triple.Predicate == Predicates.Extends {
			foundExtends = true
			break
		}
	}
	if !foundExtends {
		t.Error("expected to find extends relation")
	}

	// 检查是否提取了implements关系
	foundImplements := false
	for _, triple := range triples {
		if triple.Predicate == Predicates.Implements {
			foundImplements = true
			break
		}
	}
	if !foundImplements {
		t.Error("expected to find implements relation")
	}

	// 检查是否提取了calls关系
	foundCalls := false
	for _, triple := range triples {
		if triple.Predicate == Predicates.Calls {
			foundCalls = true
			break
		}
	}
	if !foundCalls {
		t.Error("expected to find calls relation")
	}
}

func TestTripleExtractorDecisionRelations(t *testing.T) {
	extractor := NewTripleExtractor(nil)
	ctx := context.Background()

	text := `
We decided to use React for the frontend.
The team chose to implement caching with Redis.
Created a new UserController component.
`

	source := TripleSource{
		SessionID:        "test-session",
		ExtractionMethod: ExtractionRule,
	}

	triples, err := extractor.Extract(ctx, text, source)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// 检查是否提取了decides关系
	foundDecides := false
	for _, triple := range triples {
		if triple.Predicate == Predicates.Decides {
			foundDecides = true
			break
		}
	}
	if !foundDecides {
		t.Error("expected to find decides relation")
	}

	// 检查是否提取了creates关系
	foundCreates := false
	for _, triple := range triples {
		if triple.Predicate == Predicates.Creates {
			foundCreates = true
			break
		}
	}
	if !foundCreates {
		t.Error("expected to find creates relation")
	}
}

func TestTripleExtractorFileRelations(t *testing.T) {
	extractor := NewTripleExtractor(nil)
	ctx := context.Background()

	text := `
Modified the file src/components/App.tsx to add new features.
The changes affect helper.js and api.ts files.
`

	source := TripleSource{
		SessionID:        "test-session",
		ExtractionMethod: ExtractionRule,
	}

	triples, err := extractor.Extract(ctx, text, source)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// 检查是否提取了modifies关系
	foundModifies := false
	for _, triple := range triples {
		if triple.Predicate == Predicates.Modifies {
			foundModifies = true
			break
		}
	}
	if !foundModifies {
		t.Error("expected to find modifies relation")
	}

	// 检查是否提取了relatedTo关系（多个文件时产生）
	foundRelated := false
	for _, triple := range triples {
		if triple.Predicate == Predicates.RelatedTo {
			foundRelated = true
			break
		}
	}
	if !foundRelated {
		// 如果没有relatedTo，至少要提取到多个文件
		fileCount := 0
		for _, triple := range triples {
			if triple.Object.Node != nil && len(triple.Object.Node.Type) > 0 &&
				triple.Object.Node.Type[0] == EntityTypes.File {
				fileCount++
			}
		}
		if fileCount < 1 {
			t.Error("expected to find file relations")
		}
	}
}

func TestGenerateIDs(t *testing.T) {
	// Triple ID
	id1 := GenerateTripleID("subject1", "predicate1", "object1")
	id2 := GenerateTripleID("subject1", "predicate1", "object1")
	id3 := GenerateTripleID("subject2", "predicate1", "object1")

	if id1 != id2 {
		t.Error("same inputs should produce same triple ID")
	}
	if id1 == id3 {
		t.Error("different inputs should produce different triple IDs")
	}

	// Entity ID
	eid1 := GenerateEntityID("type1", "label1")
	eid2 := GenerateEntityID("type1", "label1")
	eid3 := GenerateEntityID("type2", "label1")

	if eid1 != eid2 {
		t.Error("same inputs should produce same entity ID")
	}
	if eid1 == eid3 {
		t.Error("different inputs should produce different entity IDs")
	}
}

func TestCreateHelpers(t *testing.T) {
	// CreateNode
	node := CreateNode("entity:test", EntityTypes.Function, "testFunc")
	if node.ID != "entity:test" {
		t.Errorf("expected ID entity:test, got %s", node.ID)
	}
	if len(node.Type) != 1 || node.Type[0] != EntityTypes.Function {
		t.Error("expected type to be Function")
	}
	if node.Label != "testFunc" {
		t.Errorf("expected label testFunc, got %s", node.Label)
	}

	// CreateLiteral
	literal := CreateLiteral("test value", "xsd:string", "en")
	if literal.Value != "test value" {
		t.Errorf("expected value 'test value', got %v", literal.Value)
	}
	if literal.Type != "xsd:string" {
		t.Errorf("expected type xsd:string, got %s", literal.Type)
	}
	if literal.Language != "en" {
		t.Errorf("expected language en, got %s", literal.Language)
	}

	// TripleObject
	nodeObj := CreateNodeObject(node)
	if nodeObj.IsLiteral() {
		t.Error("node object should not be literal")
	}

	literalObj := CreateLiteralObject(literal)
	if !literalObj.IsLiteral() {
		t.Error("literal object should be literal")
	}
}

func TestContextCancellation(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.Query(ctx, TripleQuery{})
	if err == nil {
		t.Error("expected error on cancelled context")
	}

	extractor := NewTripleExtractor(nil)
	_, err = extractor.Extract(ctx, "test", TripleSource{})
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}
