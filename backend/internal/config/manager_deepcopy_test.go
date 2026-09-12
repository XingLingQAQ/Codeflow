package config

import "testing"

// ConfigManager getters must hand out deep copies: mutating a returned slice
// or pointer never changes the live snapshot (the ConfigManager side of the
// E-09 consistency contract). The getters already deep-copy; this pins the
// invariant.
func TestConfigManagerGettersReturnDeepCopies(t *testing.T) {
	manager := NewConfigManager(&GlobalConfig{
		DefaultModel: "m",
		APIPool:      []APIChannel{{ID: "ch-1", Name: "one", Enabled: true}},
		PublicMCP:    []string{"tool1"},
	})
	if err := manager.SaveRoleConfig(RoleCoder, &RoleConfig{
		Model:    "m",
		MCPTools: []string{"role-tool"},
	}); err != nil {
		t.Fatal(err)
	}
	temp := 0.5
	if err := manager.SaveSessionConfig(&SessionConfig{SessionID: "s1", Temperature: &temp}); err != nil {
		t.Fatal(err)
	}

	global := manager.LoadGlobalConfig()
	global.APIPool[0].Name = "mutated"
	global.PublicMCP[0] = "mutated"

	role := manager.LoadRoleConfig(RoleCoder)
	role.MCPTools[0] = "mutated"

	session := manager.LoadSessionConfig("s1")
	*session.Temperature = 9.9

	freshGlobal := manager.LoadGlobalConfig()
	if freshGlobal.APIPool[0].Name != "one" || freshGlobal.PublicMCP[0] != "tool1" {
		t.Fatalf("global getter shares live state: %+v", freshGlobal)
	}
	freshRole := manager.LoadRoleConfig(RoleCoder)
	if len(freshRole.MCPTools) != 1 || freshRole.MCPTools[0] != "role-tool" {
		t.Fatalf("role getter shares live state: %+v", freshRole.MCPTools)
	}
	freshSession := manager.LoadSessionConfig("s1")
	if freshSession.Temperature == nil || *freshSession.Temperature != 0.5 {
		t.Fatalf("session getter shares live state: %+v", freshSession.Temperature)
	}
}
