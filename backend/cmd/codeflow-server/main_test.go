package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/config"
	backendhooks "github.com/codeflow/backend/internal/hooks"
)

func TestConfigureHookRuntimeControls(t *testing.T) {
	previous := backendhooks.GetHookManager()
	mgr := backendhooks.NewHookManager()
	backendhooks.SetHookManager(mgr)
	t.Cleanup(func() {
		backendhooks.SetHookManager(previous)
	})

	configureHookRuntimeControls()
	controls := mgr.GetControls()
	if controls.Enabled == nil || !*controls.Enabled {
		t.Fatalf("expected hooks to be enabled by default, got %#v", controls.Enabled)
	}
	allowed := map[backendhooks.HookType]bool{}
	for _, hookType := range controls.AllowedHooks {
		allowed[hookType] = true
	}
	for _, hookType := range []backendhooks.HookType{
		backendhooks.HookBeforeSend,
		backendhooks.HookPostResponse,
		backendhooks.HookOnStream,
		backendhooks.HookBeforeCompress,
		backendhooks.HookOnMessageComplete,
		backendhooks.HookAfterExec,
		backendhooks.HookRestoreState,
		backendhooks.HookOnUserInputSubmitted,
		backendhooks.HookBeforeTaskExecute,
		backendhooks.HookAfterTaskExecute,
		backendhooks.HookOnTaskFailure,
		backendhooks.HookOnTaskComplete,
	} {
		if !allowed[hookType] {
			t.Fatalf("expected hook %s to be allowed by default, got %v", hookType, controls.AllowedHooks)
		}
	}
}

func TestRegisterConfiguredAgents(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "config.db")
	cfgSvc, err := config.NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteConfigService() error = %v", err)
	}
	defer cfgSvc.Close()

	global := cfgSvc.LoadGlobalConfig()
	global.APIPool = []config.APIChannel{{
		ID:       "default-channel",
		Name:     "Default",
		Provider: config.ProviderAnthropic,
		APIKey:   "test-key",
		Enabled:  true,
	}}
	if err := cfgSvc.SaveGlobalConfig(global); err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	agentSvc := agent.NewInMemoryAgentService()
	if err := registerConfiguredAgents(cfgSvc, agentSvc); err != nil {
		t.Fatalf("registerConfiguredAgents() error = %v", err)
	}

	result, err := agentSvc.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("expected 3 agents, got %d", result.Total)
	}

	roles := map[agent.AgentRole]bool{}
	for _, registered := range result.Agents {
		roles[registered.Role] = true
		if registered.Status != agent.AgentStatusIdle {
			t.Fatalf("expected idle status, got %s", registered.Status)
		}
		if registered.Model == "" {
			t.Fatal("expected model to be populated")
		}
	}

	for _, role := range []agent.AgentRole{agent.RoleMain, agent.RoleCoder, agent.RoleSubExpert} {
		if !roles[role] {
			t.Fatalf("expected role %s to be registered", role)
		}
	}
}

func TestRegisterConfiguredAgentsSkipsMissingAPIChannel(t *testing.T) {
	cfgSvc := config.NewConfigManager(nil)
	agentSvc := agent.NewInMemoryAgentService()

	if err := registerConfiguredAgents(cfgSvc, agentSvc); err != nil {
		t.Fatalf("registerConfiguredAgents() error = %v", err)
	}

	result, err := agentSvc.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if result.Total != 0 {
		t.Fatalf("expected 0 agents without configured API channel, got %d", result.Total)
	}
}

func TestRegisterConfiguredAgentsFailsOnUnsupportedProvider(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "config.db")
	cfgSvc, err := config.NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteConfigService() error = %v", err)
	}
	defer cfgSvc.Close()

	global := cfgSvc.LoadGlobalConfig()
	global.APIPool = []config.APIChannel{{
		ID:       "default-channel",
		Name:     "Broken",
		Provider: config.Provider("azure"),
		APIKey:   "test-key",
		Enabled:  true,
	}}
	if err := cfgSvc.SaveGlobalConfig(global); err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	agentSvc := agent.NewInMemoryAgentService()
	if err := registerConfiguredAgents(cfgSvc, agentSvc); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestInitConfigService(t *testing.T) {
	dataDir := t.TempDir()
	old := os.Getenv("CODEFLOW_DATA_DIR")
	if err := os.Setenv("CODEFLOW_DATA_DIR", dataDir); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	defer func() {
		if old == "" {
			_ = os.Unsetenv("CODEFLOW_DATA_DIR")
		} else {
			_ = os.Setenv("CODEFLOW_DATA_DIR", old)
		}
	}()

	svc, closeFn, err := initConfigService()
	if err != nil {
		t.Fatalf("initConfigService() error = %v", err)
	}
	defer closeFn()

	if svc == nil {
		t.Fatal("expected config service")
	}
}
