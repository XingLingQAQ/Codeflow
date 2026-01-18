package commander

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/adapters"
)

// MockAdapter 测试用模拟适配器
type MockAdapter struct {
	history    []adapters.Message
	response   string
	shouldFail bool
	mu         sync.Mutex
}

func NewMockAdapter(response string) *MockAdapter {
	return &MockAdapter{
		history:  make([]adapters.Message, 0),
		response: response,
	}
}

func (m *MockAdapter) Send(ctx context.Context, prompt string, opts *adapters.SendOptions) (*adapters.AIResponse, error) {
	if m.shouldFail {
		return nil, context.DeadlineExceeded
	}
	return &adapters.AIResponse{
		Content: m.response,
		Usage: adapters.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *MockAdapter) Stream(ctx context.Context, prompt string, opts *adapters.SendOptions) (<-chan adapters.StreamChunk, error) {
	return nil, nil
}

func (m *MockAdapter) GetHistory() []adapters.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.history
}

func (m *MockAdapter) SetHistory(messages []adapters.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = messages
}

func (m *MockAdapter) Configure(config *adapters.AdapterConfig) {}
func (m *MockAdapter) Rewind(count int) error                   { return nil }
func (m *MockAdapter) ClearHistory()                            { m.history = nil }
func (m *MockAdapter) Compact(ctx context.Context) error        { return nil }
func (m *MockAdapter) GetConfig() adapters.AdapterConfig {
	return adapters.AdapterConfig{}
}
func (m *MockAdapter) Close() error { return nil }

func TestCommander_RegisterAgent(t *testing.T) {
	cmd := NewCommander(5)

	adapter := NewMockAdapter("test response")
	cmd.RegisterAgent(AgentConfig{
		Role:         RoleMain,
		Adapter:      adapter,
		SystemPrompt: "You are a helpful assistant",
	})

	agent := cmd.GetAgent(RoleMain)
	if agent == nil {
		t.Fatal("Expected agent to be registered")
	}
	if agent.Role != RoleMain {
		t.Errorf("Expected role %s, got %s", RoleMain, agent.Role)
	}
	if agent.SystemPrompt != "You are a helpful assistant" {
		t.Errorf("Expected system prompt to be set")
	}
}

func TestCommander_CallCoderAgent(t *testing.T) {
	cmd := NewCommander(5)

	adapter := NewMockAdapter("Generated code: function hello() { return 'world'; }")
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleCoder,
		Adapter: adapter,
	})

	result, err := cmd.CallCoderAgent(CallCoderAgentParams{
		Task:     "Write a hello function",
		Language: "javascript",
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
	if result.AgentRole != RoleCoder {
		t.Errorf("Expected agent role %s, got %s", RoleCoder, result.AgentRole)
	}
	if result.Output == "" {
		t.Error("Expected non-empty output")
	}
}

func TestCommander_CallCoderAgent_NotRegistered(t *testing.T) {
	cmd := NewCommander(5)

	result, err := cmd.CallCoderAgent(CallCoderAgentParams{
		Task: "Write code",
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Expected failure when coder not registered")
	}
	if result.Error == "" {
		t.Error("Expected error message")
	}
}

func TestCommander_ConsultSubExpert(t *testing.T) {
	cmd := NewCommander(5)

	adapter := NewMockAdapter("Security recommendation: Use HTTPS and validate all inputs.")
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleSubExpert,
		Adapter: adapter,
	})

	result, err := cmd.ConsultSubExpert(ConsultSubExpertParams{
		Domain:   "security",
		Question: "How to secure API endpoints?",
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
	if result.AgentRole != RoleSubExpert {
		t.Errorf("Expected agent role %s, got %s", RoleSubExpert, result.AgentRole)
	}
}

func TestCommander_GraftContext(t *testing.T) {
	cmd := NewCommander(5)

	mainAdapter := NewMockAdapter("main response")
	mainAdapter.SetHistory([]adapters.Message{
		{Role: adapters.RoleUser, Content: "Hello", Timestamp: time.Now()},
		{Role: adapters.RoleAssistant, Content: "Hi there!", Timestamp: time.Now()},
		{Role: adapters.RoleUser, Content: "How are you?", Timestamp: time.Now()},
	})

	coderAdapter := NewMockAdapter("coder response")

	cmd.RegisterAgent(AgentConfig{
		Role:         RoleMain,
		Adapter:      mainAdapter,
		SystemPrompt: "Main system prompt",
	})
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleCoder,
		Adapter: coderAdapter,
	})

	graftedCtx, err := cmd.GraftContext(RoleMain, RoleCoder, &ContextGraftConfig{
		InheritMessages:     true,
		InheritSystemPrompt: true,
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(graftedCtx.Messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(graftedCtx.Messages))
	}
	if graftedCtx.SystemPrompt != "Main system prompt" {
		t.Errorf("Expected system prompt to be inherited")
	}
	if graftedCtx.Metadata.SourceAgent != RoleMain {
		t.Errorf("Expected source agent %s, got %s", RoleMain, graftedCtx.Metadata.SourceAgent)
	}
}

func TestCommander_GraftContext_FilterRoles(t *testing.T) {
	cmd := NewCommander(5)

	mainAdapter := NewMockAdapter("main response")
	mainAdapter.SetHistory([]adapters.Message{
		{Role: adapters.RoleUser, Content: "User message 1", Timestamp: time.Now()},
		{Role: adapters.RoleAssistant, Content: "Assistant response", Timestamp: time.Now()},
		{Role: adapters.RoleUser, Content: "User message 2", Timestamp: time.Now()},
	})

	cmd.RegisterAgent(AgentConfig{
		Role:    RoleMain,
		Adapter: mainAdapter,
	})
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleCoder,
		Adapter: NewMockAdapter(""),
	})

	graftedCtx, err := cmd.GraftContext(RoleMain, RoleCoder, &ContextGraftConfig{
		InheritMessages: true,
		FilterRoles:     []string{"user"},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(graftedCtx.Messages) != 2 {
		t.Errorf("Expected 2 user messages, got %d", len(graftedCtx.Messages))
	}
	for _, m := range graftedCtx.Messages {
		if m.Role != adapters.RoleUser {
			t.Errorf("Expected only user messages, got %s", m.Role)
		}
	}
}

func TestCommander_GraftContext_MaxTokens(t *testing.T) {
	cmd := NewCommander(5)

	mainAdapter := NewMockAdapter("main response")
	// 每条消息约100字符，总共300字符（约75 tokens）
	mainAdapter.SetHistory([]adapters.Message{
		{Role: adapters.RoleUser, Content: string(make([]byte, 100)), Timestamp: time.Now()},
		{Role: adapters.RoleUser, Content: string(make([]byte, 100)), Timestamp: time.Now()},
		{Role: adapters.RoleUser, Content: string(make([]byte, 100)), Timestamp: time.Now()},
	})

	cmd.RegisterAgent(AgentConfig{
		Role:    RoleMain,
		Adapter: mainAdapter,
	})
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleCoder,
		Adapter: NewMockAdapter(""),
	})

	// 限制50 tokens（约200字符），应只保留最近2条
	graftedCtx, err := cmd.GraftContext(RoleMain, RoleCoder, &ContextGraftConfig{
		InheritMessages:  true,
		MaxContextTokens: 50,
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(graftedCtx.Messages) > 2 {
		t.Errorf("Expected at most 2 messages due to token limit, got %d", len(graftedCtx.Messages))
	}
}

func TestCommander_GetToolDefinitions(t *testing.T) {
	cmd := NewCommander(5)
	defs := cmd.GetToolDefinitions()

	if len(defs) != 2 {
		t.Errorf("Expected 2 tool definitions, got %d", len(defs))
	}

	coderTool := defs[0]
	if coderTool.Name != "call_coder_agent" {
		t.Errorf("Expected call_coder_agent, got %s", coderTool.Name)
	}
	if coderTool.Parameters.Type != "object" {
		t.Errorf("Expected object type parameters")
	}
	if _, ok := coderTool.Parameters.Properties["task"]; !ok {
		t.Error("Expected task property")
	}

	expertTool := defs[1]
	if expertTool.Name != "consult_sub_expert" {
		t.Errorf("Expected consult_sub_expert, got %s", expertTool.Name)
	}
}

func TestCommander_EventHandlers(t *testing.T) {
	cmd := NewCommander(5)

	var eventCount int32
	handler := func(data interface{}) {
		atomic.AddInt32(&eventCount, 1)
	}

	cmd.On(EventAgentRegistered, handler)

	cmd.RegisterAgent(AgentConfig{
		Role:    RoleMain,
		Adapter: NewMockAdapter(""),
	})
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleCoder,
		Adapter: NewMockAdapter(""),
	})

	// 等待事件处理
	time.Sleep(10 * time.Millisecond)

	if atomic.LoadInt32(&eventCount) != 2 {
		t.Errorf("Expected 2 events, got %d", eventCount)
	}
}

func TestCommander_CallTrace(t *testing.T) {
	cmd := NewCommander(5)

	adapter := NewMockAdapter("response")
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleCoder,
		Adapter: adapter,
	})

	cmd.CallCoderAgent(CallCoderAgentParams{
		Task: "Test task",
	})

	// 调用完成后callStack应该为空
	trace := cmd.GetCallTrace()
	if len(trace) != 0 {
		t.Errorf("Expected empty call stack after completion, got %d", len(trace))
	}
}

func TestCommander_MaxNestingDepth(t *testing.T) {
	cmd := NewCommander(1) // 最大深度1

	adapter := NewMockAdapter("response")
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleCoder,
		Adapter: adapter,
	})

	// 手动模拟已有一个调用在栈中
	cmd.pushCall(&CallTrace{
		ID:        "test",
		AgentRole: RoleMain,
		ToolName:  "test",
		StartTime: time.Now().UnixMilli(),
		Children:  make([]*CallTrace, 0),
	})

	result, _ := cmd.CallCoderAgent(CallCoderAgentParams{
		Task: "Test task",
	})

	if result.Success {
		t.Error("Expected failure due to max nesting depth")
	}
	if result.Error == "" || result.Error != "max nesting depth (1) exceeded" {
		t.Errorf("Expected max nesting depth error, got: %s", result.Error)
	}
}

func TestCommander_BuildPrompts(t *testing.T) {
	cmd := NewCommander(5)

	coderPrompt := cmd.buildCoderPrompt(CallCoderAgentParams{
		Task:        "Write a function",
		Context:     "For a web app",
		Files:       []string{"app.js", "utils.js"},
		Language:    "javascript",
		Constraints: []string{"Use ES6", "No dependencies"},
	})

	if coderPrompt == "" {
		t.Error("Expected non-empty prompt")
	}
	if !contains(coderPrompt, "Write a function") {
		t.Error("Expected task in prompt")
	}
	if !contains(coderPrompt, "For a web app") {
		t.Error("Expected context in prompt")
	}
	if !contains(coderPrompt, "app.js") {
		t.Error("Expected files in prompt")
	}
	if !contains(coderPrompt, "javascript") {
		t.Error("Expected language in prompt")
	}
	if !contains(coderPrompt, "Use ES6") {
		t.Error("Expected constraints in prompt")
	}

	expertPrompt := cmd.buildSubExpertPrompt(ConsultSubExpertParams{
		Domain:   "security",
		Question: "How to prevent XSS?",
		Context:  "React application",
	})

	if expertPrompt == "" {
		t.Error("Expected non-empty prompt")
	}
	if !contains(expertPrompt, "security") {
		t.Error("Expected domain in prompt")
	}
	if !contains(expertPrompt, "How to prevent XSS?") {
		t.Error("Expected question in prompt")
	}
	if !contains(expertPrompt, "React application") {
		t.Error("Expected context in prompt")
	}
}

func TestCommander_Concurrent(t *testing.T) {
	cmd := NewCommander(10)

	adapter := NewMockAdapter("response")
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleCoder,
		Adapter: adapter,
	})
	cmd.RegisterAgent(AgentConfig{
		Role:    RoleSubExpert,
		Adapter: adapter,
	})

	var wg sync.WaitGroup
	errors := make(chan error, 20)

	for i := 0; i < 10; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			_, err := cmd.CallCoderAgent(CallCoderAgentParams{
				Task: "Concurrent task",
			})
			if err != nil {
				errors <- err
			}
		}()

		go func() {
			defer wg.Done()
			_, err := cmd.ConsultSubExpert(ConsultSubExpertParams{
				Domain:   "test",
				Question: "Concurrent question",
			})
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent error: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && containsSubstr(s, substr)))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
