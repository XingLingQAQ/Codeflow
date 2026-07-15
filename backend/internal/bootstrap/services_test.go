package bootstrap

import (
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/agent"
	cfgsvc "github.com/codeflow/backend/internal/config"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/debate"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/snapshot"
	"github.com/codeflow/backend/internal/summarize"
)

func TestServicesValidateReportsMissingDependencies(t *testing.T) {
	err := Services{}.Validate()
	if err == nil {
		t.Fatalf("expected missing services error")
	}
	for _, name := range []string{"config", "agent", "planner", "project", "context", "snapshot", "debate", "summarize"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("expected missing dependency %q in error %q", name, err.Error())
		}
	}
}

func TestServicesApplyAndResetCompatibilityLayer(t *testing.T) {
	services := Services{
		Config:    cfgsvc.NewConfigManager(nil),
		Agent:     agent.NewInMemoryAgentService(),
		Planner:   planner.NewInMemoryPlanner(),
		Project:   project.NewInMemoryProjectService(),
		Context:   ctxsvc.NewInMemoryContextService(),
		Snapshot:  snapshot.NewInMemorySnapshotService(),
		Debate:    debate.NewInMemoryDebateManager(),
		Summarize: summarize.NewSummarizerService(),
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
	if snapshot.GetSnapshotService() != services.Snapshot {
		t.Fatalf("snapshot service was not applied")
	}
	if debate.GetDebateManager() != services.Debate {
		t.Fatalf("debate manager was not applied")
	}
	if summarize.GetSummarizer() != services.Summarize {
		t.Fatalf("summarize service was not applied")
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
	// Snapshot/Summarize Get* auto-construct when nil; ensure they are not the same instance we applied.
	if snapshot.GetSnapshotService() == services.Snapshot {
		t.Fatalf("snapshot service was not reset (still previous instance)")
	}
	if debate.GetDebateManager() == services.Debate {
		t.Fatalf("debate manager was not reset")
	}
	if summarize.GetSummarizer() == services.Summarize {
		t.Fatalf("summarize service was not reset (still previous instance)")
	}
}
