package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
	"github.com/codeflow/backend/internal/skill"
)

func validTemplateJSON(t *testing.T, id string) json.RawMessage {
	t.Helper()
	ct := floweng.CustomTemplate{
		ID: floweng.TemplateID(id),
		Stages: []floweng.CustomStage{
			{Type: floweng.StageTypeCoding, Name: "Code"},
			{Type: floweng.StageTypeReview, Name: "Review"},
		},
	}
	data, err := json.Marshal(ct)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func setupContribDeps(t *testing.T) {
	t.Helper()
	prevGuard := guard.GetService()
	prevSkill := skill.GetRegistry()
	eng := guard.NewEngine(nil, nil)
	guard.SetService(eng)
	skill.SetRegistry(skill.NewInMemoryRegistry())
	t.Cleanup(func() {
		guard.SetService(prevGuard)
		skill.SetRegistry(prevSkill)
	})
}

func TestContributionRoundTrip(t *testing.T) {
	setupContribDeps(t)
	reg := NewContributionRegistry()
	m := ContributionManifest{
		Skills: []SkillContrib{
			{Name: "Plugin Skill", Body: "body text", Triggers: []string{"plugtest"}},
		},
		GuardRules: []GuardRuleContrib{
			{RuleID: guard.RuleBinaryExecWrite, Severity: guard.SeverityError},
			{DeniedPathGlobs: []string{"**/*.bak"}},
		},
		FlowTemplates: []FlowTemplateContrib{
			{TemplateJSON: validTemplateJSON(t, "plugin-tpl-1")},
		},
	}
	if err := reg.RegisterContributions("com.test.plugin", m); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reg.UnregisterContributions("com.test.plugin") })

	applied := reg.ListApplied("com.test.plugin")
	if len(applied) != 3 {
		t.Fatalf("expected 3 applied records, got %d", len(applied))
	}

	matches, _ := skill.GetRegistry().Match(context.Background(), &skill.MatchRequest{Text: "plugtest"})
	found := false
	for _, m := range matches {
		if m.Skill.Name == "Plugin Skill" {
			found = true
		}
	}
	if !found {
		t.Fatal("skill contribution not visible via Match")
	}

	cfg := guard.GetService().(*guard.Engine).Config()
	if cfg.Rules[guard.RuleBinaryExecWrite].Severity != guard.SeverityError {
		t.Fatalf("guard severity not applied: %s", cfg.Rules[guard.RuleBinaryExecWrite].Severity)
	}
	globFound := false
	for _, g := range cfg.DeniedPathGlobs {
		if g == "**/*.bak" {
			globFound = true
		}
	}
	if !globFound {
		t.Fatal("denied glob not applied")
	}

	if _, err := floweng.ExportTemplateJSON("plugin-tpl-1"); err != nil {
		t.Fatalf("template not registered: %v", err)
	}
}

func TestContributionUnregisterRemovesAll(t *testing.T) {
	setupContribDeps(t)
	reg := NewContributionRegistry()
	m := ContributionManifest{
		Skills: []SkillContrib{
			{Name: "Removable Skill", Body: "body", Triggers: []string{"removeme"}},
		},
		FlowTemplates: []FlowTemplateContrib{
			{TemplateJSON: validTemplateJSON(t, "plugin-tpl-remove")},
		},
	}
	if err := reg.RegisterContributions("com.test.remove", m); err != nil {
		t.Fatal(err)
	}
	if err := reg.UnregisterContributions("com.test.remove"); err != nil {
		t.Fatal(err)
	}

	matches, _ := skill.GetRegistry().Match(context.Background(), &skill.MatchRequest{Text: "removeme"})
	for _, m := range matches {
		if m.Skill.Name == "Removable Skill" {
			t.Fatal("skill should have been removed after unregister")
		}
	}
	if _, err := floweng.ExportTemplateJSON("plugin-tpl-remove"); err == nil {
		t.Fatal("template should have been unregistered")
	}

	if applied := reg.ListApplied("com.test.remove"); len(applied) != 0 {
		t.Fatalf("expected empty applied list, got %d", len(applied))
	}
}

func TestContributionInvalidTemplateRollsBackSkill(t *testing.T) {
	setupContribDeps(t)
	reg := NewContributionRegistry()
	m := ContributionManifest{
		Skills: []SkillContrib{
			{Name: "Should Rollback", Body: "body", Triggers: []string{"rollback"}},
		},
		FlowTemplates: []FlowTemplateContrib{
			{TemplateJSON: json.RawMessage(`{"invalid json`)},
		},
	}
	err := reg.RegisterContributions("com.test.rollback", m)
	if err == nil {
		t.Fatal("expected registration to fail on invalid template")
	}

	matches, _ := skill.GetRegistry().Match(context.Background(), &skill.MatchRequest{Text: "rollback"})
	for _, m := range matches {
		if m.Skill.Name == "Should Rollback" {
			t.Fatal("skill should have been rolled back after template failure")
		}
	}

	if applied := reg.ListApplied("com.test.rollback"); len(applied) != 0 {
		t.Fatalf("no records should remain after failed registration, got %d", len(applied))
	}
}

func TestContributionDoubleRegisterRejected(t *testing.T) {
	setupContribDeps(t)
	reg := NewContributionRegistry()
	m := ContributionManifest{
		Skills: []SkillContrib{
			{Name: "Double Skill", Body: "body", Triggers: []string{"double"}},
		},
	}
	if err := reg.RegisterContributions("com.test.double", m); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reg.UnregisterContributions("com.test.double") })

	if err := reg.RegisterContributions("com.test.double", m); err == nil {
		t.Fatal("expected double-register to be rejected")
	}
}

func TestContributionUnknownPluginUnregister(t *testing.T) {
	reg := NewContributionRegistry()
	if err := reg.UnregisterContributions("com.test.unknown"); err == nil {
		t.Fatal("expected error for unknown plugin unregister")
	}
}

func TestContributionEmptyPluginIDRejected(t *testing.T) {
	reg := NewContributionRegistry()
	if err := reg.RegisterContributions("", ContributionManifest{}); err == nil {
		t.Fatal("expected error for empty plugin id")
	}
}

func TestContributionEmptyManifestAllowed(t *testing.T) {
	setupContribDeps(t)
	reg := NewContributionRegistry()
	if err := reg.RegisterContributions("com.test.empty", ContributionManifest{}); err != nil {
		t.Fatalf("empty manifest should register: %v", err)
	}
	t.Cleanup(func() { _ = reg.UnregisterContributions("com.test.empty") })

	if applied := reg.ListApplied("com.test.empty"); len(applied) != 0 {
		t.Fatalf("empty manifest should have 0 records, got %d", len(applied))
	}
}
