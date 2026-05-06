package summarizer

import (
	stdcontext "context"
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
		want int
	}{
		{"empty", "", 0},
		{"english", "hello world", 3},
		{"chinese", "你好世界", 3},
		{"mixed", "hello 你好", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tc.Count(tt.text)
			if got < 1 && tt.text != "" {
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

func TestCompressor_ExtractSkeleton(t *testing.T) {
	c := NewCompressor(nil, nil)

	messages := []adapters.Message{
		{Role: adapters.RoleUser, Content: "We should implement the AuthService using OAuth", Timestamp: time.Now()},
		{Role: adapters.RoleAssistant, Content: "I decide to use the TokenManager for handling JWT tokens", Timestamp: time.Now()},
		{Role: adapters.RoleUser, Content: "The UserController will call AuthService", Timestamp: time.Now()},
	}

	skeleton, err := c.ExtractSkeleton(messages)
	if err != nil {
		t.Fatalf("ExtractSkeleton error: %v", err)
	}

	if len(skeleton.Entities) == 0 {
		t.Error("ExtractSkeleton entities should not be empty")
	}

	if len(skeleton.Decisions) == 0 {
		t.Error("ExtractSkeleton decisions should not be empty")
	}

	// Check for expected entities
	foundAuthService := false
	for _, e := range skeleton.Entities {
		if e == "AuthService" {
			foundAuthService = true
			break
		}
	}
	if !foundAuthService {
		t.Error("Expected to find 'AuthService' in entities")
	}
}

func TestCompressorTriggersBeforeCompressHook(t *testing.T) {
	mgr := backendhooks.NewHookManager()
	previous := backendhooks.GetHookManager()
	backendhooks.SetHookManager(mgr)
	t.Cleanup(func() {
		backendhooks.SetHookManager(previous)
	})

	var received Context
	err := mgr.Register(backendhooks.HookConfig{Name: "before-compress", Type: backendhooks.HookBeforeCompress, Enabled: true}, func(ctx stdcontext.Context, value interface{}) (interface{}, error) {
		payload, ok := value.(Context)
		if !ok {
			t.Fatalf("expected summarizer Context payload, got %#v", value)
		}
		received = payload
		payload.Messages = append(payload.Messages, adapters.Message{Role: adapters.RoleSystem, Content: "hook-added", Timestamp: time.Now()})
		return payload, nil
	})
	if err != nil {
		t.Fatalf("register before-compress hook: %v", err)
	}

	c := NewCompressor(nil, nil)
	input := Context{Messages: []adapters.Message{{Role: adapters.RoleUser, Content: "compress me", Timestamp: time.Now()}}}
	result, err := c.CompressContext(stdcontext.Background(), input, nil)
	if err != nil {
		t.Fatalf("CompressContext error: %v", err)
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
		t.Fatalf("expected compressed result to include hook-modified context, got %#v", result.PreservedMessages)
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

	ctx := Context{
		Messages:   messages,
		TokenCount: 0,
	}

	result, err := c.Compress(ctx, nil)
	if err != nil {
		t.Fatalf("Compress error: %v", err)
	}

	if result.OriginalTokens <= 0 {
		t.Error("OriginalTokens should be > 0")
	}

	if len(result.PreservedMessages) == 0 {
		t.Error("PreservedMessages should not be empty")
	}

	// System message should be preserved
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

	if !containsAny(summary, "messages", "Assistant", "responses") {
		t.Errorf("Summary should contain expected keywords, got: %s", summary)
	}
}

func TestCompressor_CalculateImportance(t *testing.T) {
	c := NewCompressor(nil, nil)

	tests := []struct {
		name    string
		msg     adapters.Message
		wantMin float64
	}{
		{
			name:    "assistant message",
			msg:     adapters.Message{Role: adapters.RoleAssistant, Content: "Some content"},
			wantMin: 5.0, // At least role bonus
		},
		{
			name:    "important keyword",
			msg:     adapters.Message{Role: adapters.RoleUser, Content: "This is critical bug fix"},
			wantMin: 2.0, // At least keyword bonus
		},
		{
			name:    "long message",
			msg:     adapters.Message{Role: adapters.RoleUser, Content: string(make([]byte, 500))},
			wantMin: 1.0,
		},
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

	// Save
	err := h.Save(history)
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Load
	loaded, err := h.Load("test-1")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded == nil || loaded.ID != "test-1" {
		t.Error("Load should return saved history")
	}

	// LoadBySession
	bySession, err := h.LoadBySession("session-1")
	if err != nil {
		t.Fatalf("LoadBySession error: %v", err)
	}
	if len(bySession) != 1 {
		t.Errorf("LoadBySession should return 1 item, got %d", len(bySession))
	}

	// List
	list, err := h.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List should return 1 item, got %d", len(list))
	}

	// Delete
	err = h.Delete("test-1")
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// Verify deletion
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

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if containsIgnoreCase(s, sub) {
			return true
		}
	}
	return false
}

func containsIgnoreCase(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && containsLower(toLower(s), toLower(sub))))
}

func containsLower(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
