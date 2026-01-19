// Package config - SQLite service tests
package config

import (
	"os"
	"testing"
)

func TestSQLiteConfigService(t *testing.T) {
	// Create temporary database
	dbPath := "test_config.db"
	defer os.Remove(dbPath)

	svc, err := NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteConfigService failed: %v", err)
	}
	defer svc.Close()

	// Test global config persistence
	globalCfg := &GlobalConfig{
		DefaultModel:     "test-model",
		SummaryThreshold: 10000,
		MaxRetries:       5,
		Timeout:          30000,
	}

	err = svc.SaveGlobalConfig(globalCfg)
	if err != nil {
		t.Fatalf("SaveGlobalConfig failed: %v", err)
	}

	// Reload from database
	svc2, err := NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	defer svc2.Close()

	loaded := svc2.LoadGlobalConfig()
	if loaded.DefaultModel != "test-model" {
		t.Errorf("DefaultModel mismatch: got %s, want test-model", loaded.DefaultModel)
	}
	if loaded.SummaryThreshold != 10000 {
		t.Errorf("SummaryThreshold mismatch: got %d, want 10000", loaded.SummaryThreshold)
	}
}

func TestSQLiteConfigServiceSessionPersistence(t *testing.T) {
	dbPath := "test_session_config.db"
	defer os.Remove(dbPath)

	svc, err := NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteConfigService failed: %v", err)
	}
	defer svc.Close()

	// Save session config
	temp := 0.8
	sessionCfg := &SessionConfig{
		SessionID:     "test-session",
		Mode:          ModeDevelopment,
		OverrideModel: "test-model",
		Temperature:   &temp,
	}

	err = svc.SaveSessionConfig(sessionCfg)
	if err != nil {
		t.Fatalf("SaveSessionConfig failed: %v", err)
	}

	// Reload
	svc2, err := NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	defer svc2.Close()

	loaded := svc2.LoadSessionConfig("test-session")
	if loaded == nil {
		t.Fatal("Session config not found")
	}
	if loaded.SessionID != "test-session" {
		t.Errorf("SessionID mismatch: got %s, want test-session", loaded.SessionID)
	}
	if loaded.OverrideModel != "test-model" {
		t.Errorf("OverrideModel mismatch: got %s, want test-model", loaded.OverrideModel)
	}
}

func TestSQLiteConfigServiceRolePersistence(t *testing.T) {
	dbPath := "test_role_config.db"
	defer os.Remove(dbPath)

	svc, err := NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteConfigService failed: %v", err)
	}
	defer svc.Close()

	// Save role config
	roleCfg := &RoleConfig{
		Model:        "test-model",
		Temperature:  0.9,
		APIChannel:   "test-channel",
		MCPTools:     []string{"tool1", "tool2"},
		SystemPrompt: "Test prompt",
	}

	err = svc.SaveRoleConfig(RoleMain, roleCfg)
	if err != nil {
		t.Fatalf("SaveRoleConfig failed: %v", err)
	}

	// Reload
	svc2, err := NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	defer svc2.Close()

	loaded := svc2.LoadRoleConfig(RoleMain)
	if loaded == nil {
		t.Fatal("Role config not found")
	}
	if loaded.Model != "test-model" {
		t.Errorf("Model mismatch: got %s, want test-model", loaded.Model)
	}
	if loaded.Temperature != 0.9 {
		t.Errorf("Temperature mismatch: got %f, want 0.9", loaded.Temperature)
	}
}

func TestConfigServiceInterface(t *testing.T) {
	// Test that ConfigManager implements IConfigService
	var _ IConfigService = NewConfigManager(nil)

	// Test that SQLiteConfigService implements IConfigService
	dbPath := "test_interface.db"
	defer os.Remove(dbPath)

	svc, err := NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteConfigService failed: %v", err)
	}
	defer svc.Close()

	var _ IConfigService = svc
}

func TestConfigHierarchyPriority(t *testing.T) {
	mgr := NewConfigManager(nil)

	// Set global default model
	globalCfg := &GlobalConfig{
		DefaultModel: "global-model",
	}
	mgr.SaveGlobalConfig(globalCfg)

	// Set session override
	temp := 0.7
	sessionCfg := &SessionConfig{
		SessionID:     "test-session",
		OverrideModel: "session-model",
		Temperature:   &temp,
	}
	mgr.SaveSessionConfig(sessionCfg)

	// Set role config (highest priority)
	roleCfg := &RoleConfig{
		Model:       "role-model",
		Temperature: 0.9,
		APIChannel:  "default",
	}
	mgr.SaveRoleConfig(RoleMain, roleCfg)

	// Resolve config - role should override session and global
	resolved := mgr.ResolveConfig("test-session", RoleMain)

	if resolved.Model != "role-model" {
		t.Errorf("Model should be from role config, got %s", resolved.Model)
	}

	if resolved.Temperature != 0.9 {
		t.Errorf("Temperature should be from role config, got %f", resolved.Temperature)
	}
}

func TestConfigChangeNotification(t *testing.T) {
	mgr := NewConfigManager(nil)

	notified := false
	unsubscribe := mgr.OnConfigChange(func(config *ConfigHierarchy) {
		notified = true
	})
	defer unsubscribe()

	// Trigger change
	globalCfg := &GlobalConfig{
		DefaultModel: "new-model",
	}
	mgr.SaveGlobalConfig(globalCfg)

	if !notified {
		t.Error("Config change callback was not triggered")
	}
}
