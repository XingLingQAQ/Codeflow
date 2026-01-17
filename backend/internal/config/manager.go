package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ConfigManager 配置管理器实现
type ConfigManager struct {
	globalConfig    GlobalConfig
	sessionConfigs  map[string]*SessionConfig
	roleConfigs     map[RoleType]*RoleConfig
	changeCallbacks []ConfigChangeCallback
	mu              sync.RWMutex
}

// NewConfigManager 创建配置管理器
func NewConfigManager(initial *GlobalConfig) *ConfigManager {
	cfg := DefaultGlobalConfig
	if initial != nil {
		cfg = *initial
	}

	return &ConfigManager{
		globalConfig:    cfg,
		sessionConfigs:  make(map[string]*SessionConfig),
		roleConfigs:     make(map[RoleType]*RoleConfig),
		changeCallbacks: make([]ConfigChangeCallback, 0),
	}
}

// LoadGlobalConfig 加载全局配置
func (m *ConfigManager) LoadGlobalConfig() *GlobalConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg := m.globalConfig
	return &cfg
}

// LoadSessionConfig 加载会话配置
func (m *ConfigManager) LoadSessionConfig(sessionID string) *SessionConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if cfg, ok := m.sessionConfigs[sessionID]; ok {
		copy := *cfg
		return &copy
	}
	return nil
}

// LoadRoleConfig 加载角色配置
func (m *ConfigManager) LoadRoleConfig(role RoleType) *RoleConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if cfg, ok := m.roleConfigs[role]; ok {
		copy := *cfg
		return &copy
	}

	// 返回默认配置
	if defaultCfg, ok := DefaultRoleConfigs[role]; ok {
		copy := *defaultCfg
		return &copy
	}
	return nil
}

// SaveGlobalConfig 保存全局配置
func (m *ConfigManager) SaveGlobalConfig(config *GlobalConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	m.mu.Lock()
	m.globalConfig = *config
	m.mu.Unlock()

	m.notifyChange()
	return nil
}

// SaveSessionConfig 保存会话配置
func (m *ConfigManager) SaveSessionConfig(config *SessionConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	if config.SessionID == "" {
		return fmt.Errorf("session ID cannot be empty")
	}

	m.mu.Lock()
	m.sessionConfigs[config.SessionID] = config
	m.mu.Unlock()

	m.notifyChange()
	return nil
}

// SaveRoleConfig 保存角色配置
func (m *ConfigManager) SaveRoleConfig(role RoleType, config *RoleConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	m.mu.Lock()
	m.roleConfigs[role] = config
	m.mu.Unlock()

	m.notifyChange()
	return nil
}

// ResolveConfig 解析配置（三级继承）
func (m *ConfigManager) ResolveConfig(sessionID string, role RoleType) *ResolvedConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 1. 从Global开始
	model := m.globalConfig.DefaultModel
	temperature := 1.0
	var topP *float64
	var maxTokens *int
	mcpTools := make([]string, len(m.globalConfig.PublicMCP))
	copy(mcpTools, m.globalConfig.PublicMCP)
	var systemPrompt string
	apiChannelID := "default"

	// 2. 应用Session配置
	if sessionID != "" {
		if sessionCfg, ok := m.sessionConfigs[sessionID]; ok {
			if sessionCfg.OverrideModel != "" {
				model = sessionCfg.OverrideModel
			}
			if sessionCfg.Temperature != nil {
				temperature = *sessionCfg.Temperature
			}
			if sessionCfg.MaxTokens != nil {
				maxTokens = sessionCfg.MaxTokens
			}
		}
	}

	// 3. 应用Role配置（最高优先级）
	if role != "" {
		roleCfg := m.roleConfigs[role]
		if roleCfg == nil {
			roleCfg = DefaultRoleConfigs[role]
		}
		if roleCfg != nil {
			model = roleCfg.Model
			temperature = roleCfg.Temperature
			topP = roleCfg.TopP
			apiChannelID = roleCfg.APIChannel
			mcpTools = append(mcpTools, roleCfg.MCPTools...)
			systemPrompt = roleCfg.SystemPrompt
		}
	}

	// 4. 解析API Channel
	apiChannel := m.resolveAPIChannel(apiChannelID)

	// 5. 去重MCP Tools
	mcpTools = uniqueStrings(mcpTools)

	return &ResolvedConfig{
		Model:        model,
		Temperature:  temperature,
		TopP:         topP,
		MaxTokens:    maxTokens,
		APIChannel:   apiChannel,
		MCPTools:     mcpTools,
		SystemPrompt: systemPrompt,
		Timeout:      m.globalConfig.Timeout,
		MaxRetries:   m.globalConfig.MaxRetries,
	}
}

// OnConfigChange 注册配置变更回调
func (m *ConfigManager) OnConfigChange(callback ConfigChangeCallback) func() {
	m.mu.Lock()
	m.changeCallbacks = append(m.changeCallbacks, callback)
	index := len(m.changeCallbacks) - 1
	m.mu.Unlock()

	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if index < len(m.changeCallbacks) {
			m.changeCallbacks = append(m.changeCallbacks[:index], m.changeCallbacks[index+1:]...)
		}
	}
}

// AddAPIChannel 添加API通道
func (m *ConfigManager) AddAPIChannel(channel *APIChannel) error {
	if channel == nil {
		return fmt.Errorf("channel cannot be nil")
	}
	if channel.ID == "" {
		return fmt.Errorf("channel ID cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 查找是否存在
	for i, ch := range m.globalConfig.APIPool {
		if ch.ID == channel.ID {
			m.globalConfig.APIPool[i] = *channel
			m.notifyChangeLocked()
			return nil
		}
	}

	// 添加新通道
	m.globalConfig.APIPool = append(m.globalConfig.APIPool, *channel)
	m.notifyChangeLocked()
	return nil
}

// RemoveAPIChannel 移除API通道
func (m *ConfigManager) RemoveAPIChannel(channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, ch := range m.globalConfig.APIPool {
		if ch.ID == channelID {
			m.globalConfig.APIPool = append(m.globalConfig.APIPool[:i], m.globalConfig.APIPool[i+1:]...)
			m.notifyChangeLocked()
			return true
		}
	}
	return false
}

// DetectConflicts 检测配置冲突
func (m *ConfigManager) DetectConflicts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var conflicts []string

	for _, role := range []RoleType{RoleMain, RoleCoder, RoleSub} {
		roleCfg := m.roleConfigs[role]
		if roleCfg == nil {
			roleCfg = DefaultRoleConfigs[role]
		}

		if roleCfg.APIChannel != "default" {
			found := false
			enabled := false
			for _, ch := range m.globalConfig.APIPool {
				if ch.ID == roleCfg.APIChannel {
					found = true
					enabled = ch.Enabled
					break
				}
			}

			if !found {
				conflicts = append(conflicts, fmt.Sprintf(
					"Role '%s' references non-existent API channel '%s'",
					role, roleCfg.APIChannel))
			} else if !enabled {
				conflicts = append(conflicts, fmt.Sprintf(
					"Role '%s' references disabled API channel '%s'",
					role, roleCfg.APIChannel))
			}
		}
	}

	return conflicts
}

// LoadFromFile 从文件加载配置
func (m *ConfigManager) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// 环境变量替换
	content := expandEnvVars(string(data))
	data = []byte(content)

	var hierarchy ConfigHierarchy
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &hierarchy); err != nil {
			return fmt.Errorf("parse yaml: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &hierarchy); err != nil {
			return fmt.Errorf("parse json: %w", err)
		}
	default:
		return fmt.Errorf("unsupported file format: %s", ext)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.globalConfig = hierarchy.Global
	if hierarchy.Session != nil {
		m.sessionConfigs[hierarchy.Session.SessionID] = hierarchy.Session
	}
	if hierarchy.Role != nil {
		for role, cfg := range hierarchy.Role {
			if cfg != nil {
				m.roleConfigs[role] = cfg
			}
		}
	}

	m.notifyChangeLocked()
	return nil
}

// SaveToFile 保存配置到文件
func (m *ConfigManager) SaveToFile(path string) error {
	m.mu.RLock()
	hierarchy := m.getConfigHierarchy()
	m.mu.RUnlock()

	ext := strings.ToLower(filepath.Ext(path))
	var data []byte
	var err error

	switch ext {
	case ".yaml", ".yml":
		data, err = yaml.Marshal(hierarchy)
	case ".json":
		data, err = json.MarshalIndent(hierarchy, "", "  ")
	default:
		return fmt.Errorf("unsupported file format: %s", ext)
	}

	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// DeleteSessionConfig 删除会话配置
func (m *ConfigManager) DeleteSessionConfig(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sessionConfigs[sessionID]; ok {
		delete(m.sessionConfigs, sessionID)
		m.notifyChangeLocked()
		return true
	}
	return false
}

// ListSessions 列出所有会话
func (m *ConfigManager) ListSessions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]string, 0, len(m.sessionConfigs))
	for id := range m.sessionConfigs {
		sessions = append(sessions, id)
	}
	return sessions
}

// 辅助方法

func (m *ConfigManager) resolveAPIChannel(channelID string) *APIChannel {
	if channelID == "default" {
		if len(m.globalConfig.APIPool) > 0 {
			ch := m.globalConfig.APIPool[0]
			return &ch
		}
		return nil
	}

	for _, ch := range m.globalConfig.APIPool {
		if ch.ID == channelID {
			return &ch
		}
	}
	return nil
}

func (m *ConfigManager) notifyChange() {
	m.mu.RLock()
	hierarchy := m.getConfigHierarchy()
	callbacks := make([]ConfigChangeCallback, len(m.changeCallbacks))
	copy(callbacks, m.changeCallbacks)
	m.mu.RUnlock()

	for _, cb := range callbacks {
		cb(&hierarchy)
	}
}

func (m *ConfigManager) notifyChangeLocked() {
	hierarchy := m.getConfigHierarchy()
	callbacks := make([]ConfigChangeCallback, len(m.changeCallbacks))
	copy(callbacks, m.changeCallbacks)

	// 异步通知，避免死锁
	go func() {
		for _, cb := range callbacks {
			cb(&hierarchy)
		}
	}()
}

func (m *ConfigManager) getConfigHierarchy() ConfigHierarchy {
	roleConfigs := make(map[RoleType]*RoleConfig)
	for role, cfg := range m.roleConfigs {
		copy := *cfg
		roleConfigs[role] = &copy
	}

	hierarchy := ConfigHierarchy{
		Global: m.globalConfig,
	}

	if len(roleConfigs) > 0 {
		hierarchy.Role = roleConfigs
	}

	return hierarchy
}

func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

func expandEnvVars(content string) string {
	return os.ExpandEnv(content)
}
