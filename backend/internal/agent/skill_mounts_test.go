package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/skill"
)

func newTestSkillRegistry(t *testing.T) skill.Registry {
	t.Helper()
	r := skill.NewInMemoryRegistry()
	ctx := context.Background()
	r.Create(ctx, &skill.CreateRequest{
		Name:      "API Errors",
		Body:      "Return structured {error, code} JSON.",
		Triggers:  []string{"api", "error"},
		StageTags: []string{"coding"},
	})
	r.Create(ctx, &skill.CreateRequest{
		Name:      "Logging Rules",
		Body:      "Use structured logging with level and correlation-id.",
		Triggers:  []string{"log", "logging"},
		StageTags: []string{"coding"},
	})
	return r
}

func TestInjectionEmptyMountsFullResult(t *testing.T) {
	agentReg := NewInMemoryAgentRegistry()
	skillReg := newTestSkillRegistry(t)
	ctx := context.Background()

	a, err := agentReg.Create(ctx, &CreateAgentRequest{
		Name: "Unrestricted", RoleBase: RoleBaseCoder,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := &skill.MatchRequest{Text: "api error logging", StageType: "coding"}
	direct, err := skillReg.RenderInjection(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	mounted, err := RenderInjectionForAgent(ctx, agentReg, skillReg, a.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if mounted != direct {
		t.Fatalf("empty mounts should equal direct injection\nmounted=%q\ndirect=%q", mounted, direct)
	}
}

func TestInjectionMountedByID(t *testing.T) {
	agentReg := NewInMemoryAgentRegistry()
	skillReg := newTestSkillRegistry(t)
	ctx := context.Background()

	allSkills, _ := skillReg.List(ctx)
	var apiErrorID string
	for _, s := range allSkills {
		if s.Name == "API Errors" {
			apiErrorID = s.ID
			break
		}
	}
	if apiErrorID == "" {
		t.Fatal("API Errors skill not found")
	}

	a, _ := agentReg.Create(ctx, &CreateAgentRequest{
		Name:     "Scoped",
		RoleBase: RoleBaseCoder,
		Mounts:   &Mounts{Skills: []string{apiErrorID}},
	})

	req := &skill.MatchRequest{Text: "api error logging", StageType: "coding"}
	result, err := RenderInjectionForAgent(ctx, agentReg, skillReg, a.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "API Errors") {
		t.Fatal("mounted skill should appear in injection")
	}
	if strings.Contains(result, "Logging Rules") {
		t.Fatal("non-mounted skill should be excluded")
	}
}

func TestInjectionMountedByName(t *testing.T) {
	agentReg := NewInMemoryAgentRegistry()
	skillReg := newTestSkillRegistry(t)
	ctx := context.Background()

	a, _ := agentReg.Create(ctx, &CreateAgentRequest{
		Name:     "Name Mount",
		RoleBase: RoleBaseCoder,
		Mounts:   &Mounts{Skills: []string{"Logging Rules"}},
	})

	req := &skill.MatchRequest{Text: "logging api error", StageType: "coding"}
	result, err := RenderInjectionForAgent(ctx, agentReg, skillReg, a.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Logging Rules") {
		t.Fatal("name-mounted skill should appear")
	}
	if strings.Contains(result, "API Errors") {
		t.Fatal("non-mounted skill should be excluded when mounting by name")
	}
}

func TestInjectionMountedButNonMatching(t *testing.T) {
	agentReg := NewInMemoryAgentRegistry()
	skillReg := newTestSkillRegistry(t)
	ctx := context.Background()

	a, _ := agentReg.Create(ctx, &CreateAgentRequest{
		Name:     "Mismatch",
		RoleBase: RoleBaseCoder,
		Mounts:   &Mounts{Skills: []string{"API Errors"}},
	})

	req := &skill.MatchRequest{Text: "nothing matches", StageType: "coding"}
	result, err := RenderInjectionForAgent(ctx, agentReg, skillReg, a.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatalf("mounted but non-matching should produce empty, got %q", result)
	}
}

func TestInjectionUnknownAgent(t *testing.T) {
	agentReg := NewInMemoryAgentRegistry()
	skillReg := newTestSkillRegistry(t)
	ctx := context.Background()

	_, err := RenderInjectionForAgent(ctx, agentReg, skillReg, "nonexistent", &skill.MatchRequest{Text: "api"})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestInjectionDisabledAgent(t *testing.T) {
	agentReg := NewInMemoryAgentRegistry()
	skillReg := newTestSkillRegistry(t)
	ctx := context.Background()

	a, _ := agentReg.Create(ctx, &CreateAgentRequest{
		Name: "Disabled", RoleBase: RoleBaseCoder,
	})
	off := false
	agentReg.Update(ctx, a.ID, &UpdateAgentRequest{Enabled: &off})

	result, err := RenderInjectionForAgent(ctx, agentReg, skillReg, a.ID, &skill.MatchRequest{Text: "api error"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatalf("disabled agent should produce empty injection, got %q", result)
	}
}

func TestInjectionNameMatchCaseInsensitive(t *testing.T) {
	agentReg := NewInMemoryAgentRegistry()
	skillReg := newTestSkillRegistry(t)
	ctx := context.Background()

	a, _ := agentReg.Create(ctx, &CreateAgentRequest{
		Name:     "Case Test",
		RoleBase: RoleBaseCoder,
		Mounts:   &Mounts{Skills: []string{"api errors"}},
	})

	req := &skill.MatchRequest{Text: "api error", StageType: "coding"}
	result, err := RenderInjectionForAgent(ctx, agentReg, skillReg, a.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "API Errors") {
		t.Fatal("case-insensitive name mount should match")
	}
}

func TestInjectionBuiltinsUnrestricted(t *testing.T) {
	agentReg := NewInMemoryAgentRegistry()
	ctx := context.Background()

	for _, id := range []string{
		"builtin-flow-conductor", "builtin-code-artisan",
		"builtin-scout", "builtin-red-critic", "builtin-deep-researcher",
	} {
		a, err := agentReg.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Mounts.Skills) != 0 {
			t.Fatalf("builtin %s should have empty Mounts.Skills (unrestricted), got %v", id, a.Mounts.Skills)
		}
	}
}
