package summarize

import (
	stdcontext "context"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/adapters"
	backendhooks "github.com/codeflow/backend/internal/hooks"
)

func TestTokenCounter_Count(t *testing.T) {
	tc := NewTokenCounter(nil)

	tests := []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"english", "hello world"},
		{"chinese", "你好世界"},
		{"mixed", "hello 你好"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tc.Count(tt.text)
			if tt.text == "" {
				if got != 0 {
					t.Errorf("Count(%q) = %d, want 0", tt.text, got)
				}
				return
			}
			if got < 1 {
				t.Errorf("Count(%q) = %d, want > 0", tt.text, got)
			}
		})
	}
}

func TestTokenCounter_CountMessages(t *testing.T) {
	tc := NewTokenCounter(nil)
	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "Hello", Timestamp: time.Now()},
		{Role: adapters.RoleAssistant, Content: "Hi there", Timestamp: time.Now()},
		{Role: adapters.RoleSystem, Content: "You are helpful", Timestamp: time.Now()},
	}

	count := tc.CountMessages(messages)
	if count.Total <= 0 {
		t.Errorf("CountMessages total = %d, want > 0", count.Total)
	}
	if len(count.ByMessage) != len(messages) {
		t.Errorf("CountMessages byMessage length = %d, want %d", len(count.ByMessage), len(messages))
	}
	if count.ByRole["user"] <= 0 {
		t.Errorf("CountMessages byRole[user] = %d, want > 0", count.ByRole["user"])
	}
}

func TestCompressor_ExtractEntitySkeleton(t *testing.T) {
	c := NewCompressor(nil, nil)
	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "We should implement the AuthService using OAuth", Timestamp: time.Now()},
		{Role: adapters.RoleAssistant, Content: "I decide to use the TokenManager for handling JWT tokens", Timestamp: time.Now()},
		{Role: adapters.RoleUser, Content: "The UserController will call AuthService", Timestamp: time.Now()},
	}

	skeleton, err := c.ExtractEntitySkeleton(messages)
	if err != nil {
		t.Fatalf("ExtractEntitySkeleton error: %v", err)
	}
	if len(skeleton.Entities) == 0 {
		t.Error("entities should not be empty")
	}
	if len(skeleton.Decisions) == 0 {
		t.Error("decisions should not be empty")
	}
	foundAuthService := false
	for _, e := range skeleton.Entities {
		if e == "AuthService" {
			foundAuthService = true
			break
		}
	}
	if !foundAuthService {
		t.Error("expected AuthService in entities")
	}
}

func TestCompressorTriggersBeforeCompressHook(t *testing.T) {
	mgr := backendhooks.NewHookManager()
	previous := backendhooks.GetHookManager()
	backendhooks.SetHookManager(mgr)
	t.Cleanup(func() {
		backendhooks.SetHookManager(previous)
	})

	var received EngineContext
	err := mgr.Register(backendhooks.HookConfig{Name: "before-compress", Type: backendhooks.HookBeforeCompress, Enabled: true}, func(ctx stdcontext.Context, value interface{}) (interface{}, error) {
		payload, ok := value.(EngineContext)
		if !ok {
			t.Fatalf("expected EngineContext payload, got %#v", value)
		}
		received = payload
		payload.Messages = append(payload.Messages, adapters.Message{Role: adapters.RoleSystem, Content: "hook-added", Timestamp: time.Now()})
		return payload, nil
	})
	if err != nil {
		t.Fatalf("register before-compress hook: %v", err)
	}

	c := NewCompressor(nil, nil)
	input := EngineContext{Messages: []adapters.Message{{Role: adapters.RoleUser, Content: "compress me", Timestamp: time.Now()}}}
	result, err := c.CompressMessages(stdcontext.Background(), input, nil)
	if err != nil {
		t.Fatalf("CompressMessages error: %v", err)
	}
	if len(received.Messages) != 1 || received.Messages[0].Content != "compress me" {
		t.Fatalf("unexpected hook payload: %#v", received)
	}
	found := false
	for _, msg := range result.PreservedMessages {
		if msg.Content == "hook-added" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected hook-modified context, got %#v", result.PreservedMessages)
	}
}

func TestCompressor_Compress(t *testing.T) {
	c := NewCompressor(nil, nil)
	messages := make([]adapters.Message, 0)
	for i := 0; i < 20; i++ {
		role := adapters.RoleUser
		if i%2 == 1 {
			role = adapters.RoleAssistant
		}
		messages = append(messages, adapters.Message{
			Role:      role,
			Content:   "This is a test message with some content that should be processed correctly.",
			Timestamp: time.Now(),
		})
	}
	messages = append([]adapters.Message{{Role: adapters.RoleSystem, Content: "System prompt", Timestamp: time.Now()}}, messages...)

	result, err := c.Compress(EngineContext{Messages: messages}, nil)
	if err != nil {
		t.Fatalf("Compress error: %v", err)
	}
	if result.OriginalTokens <= 0 {
		t.Error("OriginalTokens should be > 0")
	}
	if len(result.PreservedMessages) == 0 {
		t.Error("PreservedMessages should not be empty")
	}
	hasSystem := false
	for _, m := range result.PreservedMessages {
		if m.Role == adapters.RoleSystem {
			hasSystem = true
			break
		}
	}
	if !hasSystem {
		t.Error("System message should be preserved")
	}
}

func TestCompressor_GenerateLocalSummary(t *testing.T) {
	c := NewCompressor(nil, nil)
	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "First topic about coding", Timestamp: time.Now()},
		{Role: adapters.RoleAssistant, Content: "Response about coding", Timestamp: time.Now()},
		{Role: adapters.RoleUser, Content: "Second topic about testing", Timestamp: time.Now()},
		{Role: adapters.RoleAssistant, Content: "Response about testing", Timestamp: time.Now()},
	}

	summary, err := c.GenerateSummary(messages, nil)
	if err != nil {
		t.Fatalf("GenerateSummary error: %v", err)
	}
	if summary == "" {
		t.Error("Summary should not be empty")
	}
	if !strings.Contains(strings.ToLower(summary), "messages") {
		t.Errorf("Summary should mention messages, got: %s", summary)
	}
}

func TestCompressor_CalculateImportance(t *testing.T) {
	c := NewCompressor(nil, nil)
	tests := []struct {
		name    string
		msg     adapters.Message
		wantMin float64
	}{
		{"assistant message", adapters.Message{Role: adapters.RoleAssistant, Content: "Some content"}, 5.0},
		{"important keyword", adapters.Message{Role: adapters.RoleUser, Content: "This is critical bug fix"}, 2.0},
		{"long message", adapters.Message{Role: adapters.RoleUser, Content: string(make([]byte, 500))}, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.calculateImportance(tt.msg, 5, 10)
			if got < tt.wantMin {
				t.Errorf("calculateImportance() = %f, want >= %f", got, tt.wantMin)
			}
		})
	}
}

func TestInMemorySummaryHistory(t *testing.T) {
	h := NewInMemorySummaryHistory()
	history := &SummaryHistory{
		ID:        "test-1",
		SessionID: "session-1",
		Summary:   "Test summary",
		CreatedAt: time.Now().Unix(),
	}
	if err := h.Save(history); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	loaded, err := h.Load("test-1")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded == nil || loaded.ID != "test-1" {
		t.Error("Load should return saved history")
	}
	bySession, err := h.LoadBySession("session-1")
	if err != nil {
		t.Fatalf("LoadBySession error: %v", err)
	}
	if len(bySession) != 1 {
		t.Errorf("LoadBySession should return 1 item, got %d", len(bySession))
	}
	list, err := h.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List should return 1 item, got %d", len(list))
	}
	if err := h.Delete("test-1"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	loaded, _ = h.Load("test-1")
	if loaded != nil {
		t.Error("Load should return nil after deletion")
	}
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"english", "Hello. World!", 2},
		{"chinese", "你好。世界！", 2},
		{"mixed", "Hello. 你好。World!", 3},
		{"empty", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitSentences(tt.text)
			if len(got) != tt.want {
				t.Errorf("splitSentences(%q) = %d sentences, want %d", tt.text, len(got), tt.want)
			}
		})
	}
}
