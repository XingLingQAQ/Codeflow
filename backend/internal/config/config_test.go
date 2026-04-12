package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/adapters"
)

func TestConfigManagerBasic(t *testing.T) {
	manager := NewConfigManager(nil)

	// 测试默认全局配置
	global := manager.LoadGlobalConfig()
	if global.DefaultModel != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected default model, got %s", global.DefaultModel)
	}
	if global.MaxRetries != 3 {
		t.Errorf("expected 3 retries, got %d", global.MaxRetries)
	}
}

func TestConfigManagerSessionConfig(t *testing.T) {
	manager := NewConfigManager(nil)

	// 初始应该没有会话配置
	session := manager.LoadSessionConfig("session-001")
	if session != nil {
		t.Error("expected nil session config")
	}

	// 保存会话配置
	temp := 0.8
	sessionCfg := &SessionConfig{
		SessionID:     "session-001",
		Mode:          ModeDevelopment,
		OverrideModel: "claude-3-opus",
		Temperature:   &temp,
	}
	if err := manager.SaveSessionConfig(sessionCfg); err != nil {
		t.Fatalf("save session config: %v", err)
	}

	// 加载会话配置
	loaded := manager.LoadSessionConfig("session-001")
	if loaded == nil {
		t.Fatal("expected session config")
	}
	if loaded.Mode != ModeDevelopment {
		t.Errorf("expected development mode, got %s", loaded.Mode)
	}
	if loaded.OverrideModel != "claude-3-opus" {
		t.Errorf("expected claude-3-opus, got %s", loaded.OverrideModel)
	}
}

func TestConfigManagerSaveSessionConfigPreservesExistingFieldsOnPartialUpdate(t *testing.T) {
	manager := NewConfigManager(nil)

	temp := 0.8
	maxTokens := 2048
	if err := manager.SaveSessionConfig(&SessionConfig{
		SessionID:     "session-001",
		Mode:          ModeDevelopment,
		OverrideModel: "claude-3-opus",
		Temperature:   &temp,
		MaxTokens:     &maxTokens,
	}); err != nil {
		t.Fatalf("seed session config: %v", err)
	}

	updatedTemp := 0.3
	if err := manager.SaveSessionConfig(&SessionConfig{
		SessionID:   "session-001",
		Temperature: &updatedTemp,
	}); err != nil {
		t.Fatalf("partial update session config: %v", err)
	}

	loaded := manager.LoadSessionConfig("session-001")
	if loaded == nil {
		t.Fatal("expected session config")
	}
	if loaded.Mode != ModeDevelopment {
		t.Errorf("expected development mode, got %s", loaded.Mode)
	}
	if loaded.OverrideModel != "claude-3-opus" {
		t.Errorf("expected override model preserved, got %s", loaded.OverrideModel)
	}
	if loaded.Temperature == nil || *loaded.Temperature != 0.3 {
		t.Fatalf("expected updated temperature 0.3, got %v", loaded.Temperature)
	}
	if loaded.MaxTokens == nil || *loaded.MaxTokens != 2048 {
		t.Fatalf("expected max tokens preserved as 2048, got %v", loaded.MaxTokens)
	}
}

func TestConfigManagerRoleConfig(t *testing.T) {
	manager := NewConfigManager(nil)

	// 加载默认角色配置
	mainCfg := manager.LoadRoleConfig(RoleMain)
	if mainCfg == nil {
		t.Fatal("expected default main config")
	}
	if mainCfg.Temperature != 1.0 {
		t.Errorf("expected temperature 1.0, got %f", mainCfg.Temperature)
	}

	// 保存自定义角色配置
	customCfg := &RoleConfig{
		Model:        "custom-model",
		Temperature:  0.5,
		APIChannel:   "custom-channel",
		MCPTools:     []string{"tool1", "tool2"},
		SystemPrompt: "Custom prompt",
	}
	if err := manager.SaveRoleConfig(RoleCoder, customCfg); err != nil {
		t.Fatalf("save role config: %v", err)
	}

	// 加载自定义配置
	loaded := manager.LoadRoleConfig(RoleCoder)
	if loaded.Model != "custom-model" {
		t.Errorf("expected custom-model, got %s", loaded.Model)
	}
	if loaded.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %f", loaded.Temperature)
	}
}

func TestConfigManagerResolveConfig(t *testing.T) {
	manager := NewConfigManager(&GlobalConfig{
		DefaultModel: "global-model",
		PublicMCP:    []string{"public-tool"},
		Timeout:      30000,
		MaxRetries:   5,
	})

	// 仅全局配置
	resolved := manager.ResolveConfig("", "")
	if resolved.Model != "global-model" {
		t.Errorf("expected global-model, got %s", resolved.Model)
	}
	if len(resolved.MCPTools) != 1 || resolved.MCPTools[0] != "public-tool" {
		t.Errorf("expected public-tool, got %v", resolved.MCPTools)
	}

	// 添加会话配置
	temp := 0.7
	maxTokens := 2000
	manager.SaveSessionConfig(&SessionConfig{
		SessionID:     "session-001",
		Mode:          ModeResearch,
		OverrideModel: "session-model",
		Temperature:   &temp,
		MaxTokens:     &maxTokens,
	})

	resolved = manager.ResolveConfig("session-001", "")
	if resolved.Model != "session-model" {
		t.Errorf("expected session-model, got %s", resolved.Model)
	}
	if resolved.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %f", resolved.Temperature)
	}
	if resolved.MaxTokens == nil || *resolved.MaxTokens != 2000 {
		t.Error("expected max tokens 2000")
	}

	// 添加角色配置（应该覆盖会话配置）
	resolved = manager.ResolveConfig("session-001", RoleMain)
	if resolved.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected role model to override, got %s", resolved.Model)
	}
	if resolved.Temperature != 1.0 {
		t.Errorf("expected role temperature 1.0, got %f", resolved.Temperature)
	}
	// MCP tools应该合并
	if len(resolved.MCPTools) < 2 {
		t.Errorf("expected at least 2 mcp tools, got %d", len(resolved.MCPTools))
	}
}

func TestConfigManagerAPIChannel(t *testing.T) {
	manager := NewConfigManager(nil)

	// 添加API通道
	channel := &APIChannel{
		ID:       "channel-001",
		Name:     "Test Channel",
		Provider: ProviderAnthropic,
		APIKey:   "test-key",
		Enabled:  true,
	}
	if err := manager.AddAPIChannel(channel); err != nil {
		t.Fatalf("add api channel: %v", err)
	}

	// 验证添加
	global := manager.LoadGlobalConfig()
	if len(global.APIPool) != 1 {
		t.Errorf("expected 1 channel, got %d", len(global.APIPool))
	}
	if global.APIPool[0].ID != "channel-001" {
		t.Errorf("expected channel-001, got %s", global.APIPool[0].ID)
	}

	// 更新通道
	channel.Name = "Updated Channel"
	manager.AddAPIChannel(channel)
	global = manager.LoadGlobalConfig()
	if global.APIPool[0].Name != "Updated Channel" {
		t.Errorf("expected Updated Channel, got %s", global.APIPool[0].Name)
	}

	// 移除通道
	if !manager.RemoveAPIChannel("channel-001") {
		t.Error("expected to remove channel")
	}
	global = manager.LoadGlobalConfig()
	if len(global.APIPool) != 0 {
		t.Errorf("expected 0 channels, got %d", len(global.APIPool))
	}
}

func TestConfigManagerConflictDetection(t *testing.T) {
	manager := NewConfigManager(nil)

	// 默认配置应该没有冲突
	conflicts := manager.DetectConflicts()
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got %v", conflicts)
	}

	// 添加引用不存在通道的角色配置
	manager.SaveRoleConfig(RoleMain, &RoleConfig{
		Model:        "model",
		Temperature:  1.0,
		APIChannel:   "nonexistent-channel",
		MCPTools:     []string{},
		SystemPrompt: "prompt",
	})

	conflicts = manager.DetectConflicts()
	if len(conflicts) != 1 {
		t.Errorf("expected 1 conflict, got %d", len(conflicts))
	}

	// 添加禁用的通道
	manager.AddAPIChannel(&APIChannel{
		ID:      "disabled-channel",
		Name:    "Disabled",
		Enabled: false,
	})
	manager.SaveRoleConfig(RoleCoder, &RoleConfig{
		Model:        "model",
		Temperature:  1.0,
		APIChannel:   "disabled-channel",
		MCPTools:     []string{},
		SystemPrompt: "prompt",
	})

	conflicts = manager.DetectConflicts()
	if len(conflicts) != 2 {
		t.Errorf("expected 2 conflicts, got %d", len(conflicts))
	}
}

func TestProviderToAdapterProvider(t *testing.T) {
	cases := []struct {
		name     string
		provider Provider
		want     adapters.Provider
	}{
		{name: "anthropic maps to claude", provider: ProviderAnthropic, want: adapters.ProviderClaude},
		{name: "google maps to gemini", provider: ProviderGoogle, want: adapters.ProviderGemini},
		{name: "openai stays openai", provider: ProviderOpenAI, want: adapters.ProviderOpenAI},
		{name: "custom stays claude-compatible", provider: ProviderCustom, want: adapters.ProviderClaude},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.provider.ToAdapterProvider()
			if err != nil {
				t.Fatalf("ToAdapterProvider() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, got)
			}
		})
	}
}

func TestConfigManagerDetectConflictsRejectsUnsupportedProvider(t *testing.T) {
	manager := NewConfigManager(nil)

	if err := manager.AddAPIChannel(&APIChannel{
		ID:       "channel-invalid-provider",
		Name:     "Invalid Provider",
		Provider: Provider("azure"),
		Enabled:  true,
	}); err != nil {
		t.Fatalf("add api channel: %v", err)
	}

	conflicts := manager.DetectConflicts()
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d (%v)", len(conflicts), conflicts)
	}
	if !strings.Contains(conflicts[0], "unsupported provider 'azure'") {
		t.Fatalf("expected unsupported provider conflict, got %q", conflicts[0])
	}
}

func TestConfigManagerChangeCallback(t *testing.T) {
	manager := NewConfigManager(nil)

	callCount := 0
	unsubscribe := manager.OnConfigChange(func(config *ConfigHierarchy) {
		callCount++
	})
	defer unsubscribe()

	// 触发变更
	manager.SaveGlobalConfig(&GlobalConfig{
		DefaultModel: "new-model",
	})

	// 等待异步回调
	time.Sleep(50 * time.Millisecond)

	if callCount < 1 {
		t.Error("expected callback to be called")
	}
}

func TestConfigManagerFilePersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewConfigManager(&GlobalConfig{
		DefaultModel: "test-model",
		PublicMCP:    []string{"tool1", "tool2"},
		Timeout:      10000,
		MaxRetries:   2,
	})

	// 保存YAML
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	if err := manager.SaveToFile(yamlPath); err != nil {
		t.Fatalf("save yaml: %v", err)
	}

	// 加载YAML
	manager2 := NewConfigManager(nil)
	if err := manager2.LoadFromFile(yamlPath); err != nil {
		t.Fatalf("load yaml: %v", err)
	}

	global := manager2.LoadGlobalConfig()
	if global.DefaultModel != "test-model" {
		t.Errorf("expected test-model, got %s", global.DefaultModel)
	}
	if len(global.PublicMCP) != 2 {
		t.Errorf("expected 2 mcp tools, got %d", len(global.PublicMCP))
	}

	// 保存JSON
	jsonPath := filepath.Join(tmpDir, "config.json")
	if err := manager.SaveToFile(jsonPath); err != nil {
		t.Fatalf("save json: %v", err)
	}

	// 加载JSON
	manager3 := NewConfigManager(nil)
	if err := manager3.LoadFromFile(jsonPath); err != nil {
		t.Fatalf("load json: %v", err)
	}

	global = manager3.LoadGlobalConfig()
	if global.DefaultModel != "test-model" {
		t.Errorf("expected test-model from json, got %s", global.DefaultModel)
	}
}

func TestConfigManagerEnvVarExpansion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 设置环境变量
	os.Setenv("TEST_MODEL", "env-model")
	defer os.Unsetenv("TEST_MODEL")

	// 创建带环境变量的配置文件
	yamlContent := `
global:
  default_model: $TEST_MODEL
  timeout: 30000
  max_retries: 3
`
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(yamlPath, []byte(yamlContent), 0644)

	manager := NewConfigManager(nil)
	if err := manager.LoadFromFile(yamlPath); err != nil {
		t.Fatalf("load yaml: %v", err)
	}

	global := manager.LoadGlobalConfig()
	if global.DefaultModel != "env-model" {
		t.Errorf("expected env-model from env var, got %s", global.DefaultModel)
	}
}

func TestConfigManagerListSessions(t *testing.T) {
	manager := NewConfigManager(nil)

	// 添加多个会话
	for _, id := range []string{"session-001", "session-002", "session-003"} {
		manager.SaveSessionConfig(&SessionConfig{
			SessionID: id,
			Mode:      ModeDevelopment,
		})
	}

	sessions := manager.ListSessions()
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}

	// 删除会话
	if !manager.DeleteSessionConfig("session-002") {
		t.Error("expected to delete session")
	}

	sessions = manager.ListSessions()
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions after delete, got %d", len(sessions))
	}
}
