package bootstrap

import (
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/agent"
	cfgsvc "github.com/codeflow/backend/internal/config"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
)

func TestServicesValidateReportsMissingDependencies(t *testing.T) {
	err := Services{}.Validate()
	if err == nil {
		t.Fatalf("expected missing services error")
	}
	for _, name := range []string{"config", "agent", "planner", "project", "context"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("expected missing dependency %q in error %q", name, err.Error())
		}
	}
}

func TestServicesApplyAndResetCompatibilityLayer(t *testing.T) {
	services := Services{
		Config:  cfgsvc.NewConfigManager(nil),
		Agent:   agent.NewInMemoryAgentService(),
		Planner: planner.NewInMemoryPlanner(),
		Project: project.NewInMemoryProjectService(),
		Context: ctxsvc.NewInMemoryContextService(),
	}
	services.Reset()
	t.Cleanup(services.Reset)

	if err := services.Apply(); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if cfgsvc.GetConfigService() != services.Config {
		t.Fatalf("config service was not applied")
	}
	if agent.GetAgentService() != services.Agent {
		t.Fatalf("agent service was not applied")
	}
	if planner.GetPlanner() != services.Planner {
		t.Fatalf("planner service was not applied")
	}
	if project.GetProjectService() != services.Project {
		t.Fatalf("project service was not applied")
	}
	if ctxsvc.GetContextService() != services.Context {
		t.Fatalf("context service was not applied")
	}

	services.Reset()
	if cfgsvc.GetConfigService() == services.Config {
		t.Fatalf("config service was not reset")
	}
	if agent.GetAgentService() == services.Agent {
		t.Fatalf("agent service was not reset")
	}
	if planner.GetPlanner() == services.Planner {
		t.Fatalf("planner service was not reset")
	}
	if project.GetProjectService() == services.Project {
		t.Fatalf("project service was not reset")
	}
	if ctxsvc.GetContextService() == services.Context {
		t.Fatalf("context service was not reset")
	}
}
