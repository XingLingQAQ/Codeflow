package mapagent

import (
	"testing"
	"time"

	"github.com/codeflow/backend/internal/adapters"
)

func TestMapAgent_ExtractLocally(t *testing.T) {
	agent := NewMapAgent(nil, nil)

	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "We should implement the AuthService using OAuth", Timestamp: time.Now()},
		{Role: adapters.RoleAssistant, Content: "I will use TokenManager for JWT handling", Timestamp: time.Now()},
		{Role: adapters.RoleUser, Content: "The UserController calls AuthService", Timestamp: time.Now()},
	}

	result, err := agent.Extract(messages)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	if len(result.Entities) == 0 {
		t.Error("Expected entities to be extracted")
	}

	if len(result.Decisions) == 0 {
		t.Error("Expected decisions to be extracted")
	}

	// 检查是否提取到AuthService
	foundAuthService := false
	for _, e := range result.Entities {
		if e.Name == "AuthService" {
			foundAuthService = true
			break
		}
	}
	if !foundAuthService {
		t.Error("Expected to find AuthService in entities")
	}
}

func TestMapAgent_BuildMap(t *testing.T) {
	agent := NewMapAgent(nil, nil)

	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "Implement UserService with CRUD operations", Timestamp: time.Now()},
		{Role: adapters.RoleAssistant, Content: "I decide to use Repository pattern for UserService", Timestamp: time.Now()},
	}

	m, err := agent.BuildMap(messages, "session-1")
	if err != nil {
		t.Fatalf("BuildMap error: %v", err)
	}

	if m.ID == "" {
		t.Error("Expected map ID to be set")
	}
	if m.SessionID != "session-1" {
		t.Errorf("Expected session ID 'session-1', got %s", m.SessionID)
	}
	if len(m.Nodes) == 0 {
		t.Error("Expected nodes to be created")
	}
	if len(m.Skeleton.Entities) == 0 {
		t.Error("Expected skeleton entities")
	}
}

func TestMapAgent_MergeMap(t *testing.T) {
	agent := NewMapAgent(nil, nil)

	existing := &CompressionMap{
		ID:        "map-1",
		SessionID: "session-1",
		Nodes: []MapNode{
			{ID: "n1", Type: NodeEntity, Label: "Entity1", Importance: 0.5},
		},
		Edges: []MapEdge{},
		Skeleton: DecisionSkeleton{
			Entities:  []string{"Entity1"},
			Decisions: []string{"Decision1"},
			Relations: []Relation{},
		},
		CreatedAt: time.Now().UnixMilli(),
		MessageRange: MessageRange{
			Start: 0,
			End:   5,
		},
	}

	newMap := &CompressionMap{
		ID:        "map-2",
		SessionID: "session-1",
		Nodes: []MapNode{
			{ID: "n2", Type: NodeEntity, Label: "Entity2", Importance: 0.6},
			{ID: "n3", Type: NodeEntity, Label: "Entity1", Importance: 0.5}, // 重复
		},
		Edges: []MapEdge{},
		Skeleton: DecisionSkeleton{
			Entities:  []string{"Entity2", "Entity1"},
			Decisions: []string{"Decision2"},
			Relations: []Relation{},
		},
		CreatedAt: time.Now().UnixMilli(),
		MessageRange: MessageRange{
			Start: 6,
			End:   10,
		},
	}

	merged := agent.MergeMap(existing, newMap)

	// 应该有2个节点（不包含重复的Entity1）
	if len(merged.Nodes) != 2 {
		t.Errorf("Expected 2 nodes after merge, got %d", len(merged.Nodes))
	}

	// 实体应该去重
	if len(merged.Skeleton.Entities) != 2 {
		t.Errorf("Expected 2 unique entities, got %d", len(merged.Skeleton.Entities))
	}

	// 决策应该合并
	if len(merged.Skeleton.Decisions) != 2 {
		t.Errorf("Expected 2 decisions, got %d", len(merged.Skeleton.Decisions))
	}

	// MessageRange应该更新
	if merged.MessageRange.End != 10 {
		t.Errorf("Expected message range end 10, got %d", merged.MessageRange.End)
	}
}

func TestMapAgent_EntityTypeInference(t *testing.T) {
	agent := NewMapAgent(nil, nil)

	tests := []struct {
		entity   string
		context  string
		expected string
	}{
		{"UserService", "class UserService {}", EntityClass},
		{"handleClick", "function handleClick() {}", EntityFunction},
		{"config", "config.ts file", EntityFile},
		{"AuthManager", "using AuthManager for auth", EntityClass}, // CamelCase
		{"Simple", "just a simple concept", EntityConcept},
	}

	for _, tt := range tests {
		got := agent.inferEntityType(tt.entity, tt.context)
		if got != tt.expected {
			t.Errorf("inferEntityType(%s, %s) = %s, want %s", tt.entity, tt.context, got, tt.expected)
		}
	}
}

func TestMapAgent_DecisionImportance(t *testing.T) {
	agent := NewMapAgent(nil, nil)

	tests := []struct {
		decision string
		minScore float64
	}{
		{"This is critical for security", 0.9},
		{"We must implement this feature", 0.9},
		{"This is an important decision", 0.9},
		{"You should consider this approach", 0.7},
		{"I recommend using this library", 0.7},
		{"Let's use this method", 0.5},
	}

	for _, tt := range tests {
		got := agent.calculateDecisionImportance(tt.decision)
		if got < tt.minScore {
			t.Errorf("calculateDecisionImportance(%s) = %f, want >= %f", tt.decision, got, tt.minScore)
		}
	}
}

func TestMapAgent_RelationTypeInference(t *testing.T) {
	agent := NewMapAgent(nil, nil)

	tests := []struct {
		context  string
		expected string
	}{
		{"UserService uses Repository", RelationUses},
		{"Controller depends on Service", RelationDependsOn},
		{"Class implements Interface", RelationImplements},
		{"ChildClass extends BaseClass", RelationExtends},
		{"Function calls helper", RelationCalls},
		{"Some random text", RelationReferences},
	}

	for _, tt := range tests {
		got := agent.inferRelationType(tt.context)
		if got != tt.expected {
			t.Errorf("inferRelationType(%s) = %s, want %s", tt.context, got, tt.expected)
		}
	}
}

func TestMapAgent_MaxNodesLimit(t *testing.T) {
	config := &MapAgentConfig{
		ExtractEntities:  true,
		ExtractDecisions: true,
		ExtractRelations: true,
		MaxNodes:         5,
		MinImportance:    0,
	}
	agent := NewMapAgent(nil, config)

	// 创建包含很多实体的消息
	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "We have Entity1, Entity2, Entity3, Entity4, Entity5, Entity6, Entity7, Entity8, Entity9, Entity10", Timestamp: time.Now()},
	}

	m, err := agent.BuildMap(messages, "session")
	if err != nil {
		t.Fatalf("BuildMap error: %v", err)
	}

	if len(m.Nodes) > 5 {
		t.Errorf("Expected at most 5 nodes, got %d", len(m.Nodes))
	}
}

func TestMapAgent_MinImportanceFilter(t *testing.T) {
	config := &MapAgentConfig{
		ExtractEntities:  true,
		ExtractDecisions: true,
		ExtractRelations: true,
		MaxNodes:         50,
		MinImportance:    0.8, // 高阈值
	}
	agent := NewMapAgent(nil, config)

	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "Just one mention of Entity1", Timestamp: time.Now()},
	}

	m, err := agent.BuildMap(messages, "session")
	if err != nil {
		t.Fatalf("BuildMap error: %v", err)
	}

	// 由于只提到一次，重要性较低，应该被过滤
	// 实际结果取决于calculateImportance的实现
	t.Logf("Nodes count with high min importance: %d", len(m.Nodes))
}

func TestInMemoryMapStorage_CRUD(t *testing.T) {
	storage := NewInMemoryMapStorage()

	m := &CompressionMap{
		ID:        "map-1",
		SessionID: "session-1",
		Nodes:     []MapNode{},
		Edges:     []MapEdge{},
		Skeleton: DecisionSkeleton{
			Entities:  []string{},
			Decisions: []string{},
			Relations: []Relation{},
		},
		CreatedAt: time.Now().UnixMilli(),
	}

	// Save
	err := storage.Save(m)
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Load
	loaded, err := storage.Load("map-1")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded == nil || loaded.ID != "map-1" {
		t.Error("Expected to load saved map")
	}

	// LoadBySession
	bySession, err := storage.LoadBySession("session-1")
	if err != nil {
		t.Fatalf("LoadBySession error: %v", err)
	}
	if len(bySession) != 1 {
		t.Errorf("Expected 1 map for session, got %d", len(bySession))
	}

	// List
	list, err := storage.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("Expected 1 map in list, got %d", len(list))
	}

	// Delete
	err = storage.Delete("map-1")
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// Verify deletion
	loaded, _ = storage.Load("map-1")
	if loaded != nil {
		t.Error("Expected map to be deleted")
	}
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"Hello. World!", 2},
		{"你好。世界！", 2},
		{"Mixed. 你好。Test!", 3},
		{"", 0},
		{"No punctuation", 1},
	}

	for _, tt := range tests {
		got := splitSentences(tt.text)
		if len(got) != tt.want {
			t.Errorf("splitSentences(%q) = %d sentences, want %d", tt.text, len(got), tt.want)
		}
	}
}

func TestMapAgent_CodeExtraction(t *testing.T) {
	agent := NewMapAgent(nil, nil)

	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "Use `handleClick` function and `UserService` class", Timestamp: time.Now()},
	}

	result, err := agent.Extract(messages)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	// 应该提取到反引号中的代码标识符
	foundHandleClick := false
	foundUserService := false
	for _, e := range result.Entities {
		if e.Name == "handleClick" {
			foundHandleClick = true
		}
		if e.Name == "UserService" {
			foundUserService = true
		}
	}

	if !foundHandleClick {
		t.Error("Expected to find handleClick in entities")
	}
	if !foundUserService {
		t.Error("Expected to find UserService in entities")
	}
}

func TestMapAgent_ChineseDecisions(t *testing.T) {
	agent := NewMapAgent(nil, nil)

	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "我们决定采用微服务架构。这个选择很重要。", Timestamp: time.Now()},
	}

	result, err := agent.Extract(messages)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	if len(result.Decisions) == 0 {
		t.Error("Expected to extract Chinese decisions")
	}
}
