// Package bootstrap wires core backend services for the server runtime.
package bootstrap

import (
	"fmt"

	"github.com/codeflow/backend/internal/agent"
	cfgsvc "github.com/codeflow/backend/internal/config"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/debate"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/snapshot"
	"github.com/codeflow/backend/internal/summarize"
	"github.com/codeflow/backend/internal/workspace"
)

// Services contains the explicit service dependencies required by the server runtime.
// It is a compatibility-oriented DI boundary: Apply wires existing global accessors,
// while handlers and legacy packages can keep using Get*/Set* during migration.
//
// B0: Config/Agent/Planner/Project/Context
// B1: Snapshot/Debate/Summarize
// B1+: Floweng (PR-8 minimal engine)
type Services struct {
	Config    cfgsvc.IConfigService
	Agent     agent.IAgentService
	Planner   planner.IPlanner
	Project   project.IProjectService
	Context   ctxsvc.IContextService
	Snapshot  snapshot.ISnapshotService
	Debate    debate.IDebateManager
	Summarize  summarize.ISummarizer
	Floweng    floweng.Engine
	Workspace  workspace.Service
	Guard      guard.Service
}

// Validate checks that every required service dependency has been provided.
func (s Services) Validate() error {
	missing := make([]string, 0)
	if s.Config == nil {
		missing = append(missing, "config")
	}
	if s.Agent == nil {
		missing = append(missing, "agent")
	}
	if s.Planner == nil {
		missing = append(missing, "planner")
	}
	if s.Project == nil {
		missing = append(missing, "project")
	}
	if s.Context == nil {
		missing = append(missing, "context")
	}
	if s.Snapshot == nil {
		missing = append(missing, "snapshot")
	}
	if s.Debate == nil {
		missing = append(missing, "debate")
	}
	if s.Summarize == nil {
		missing = append(missing, "summarize")
	}
	if s.Floweng == nil {
		missing = append(missing, "floweng")
	}
	if s.Workspace == nil {
		missing = append(missing, "workspace")
	}
	if s.Guard == nil {
		missing = append(missing, "guard")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing bootstrap services: %v", missing)
	}
	return nil
}

// Apply wires the services into the existing global compatibility layer.
func (s Services) Apply() error {
	if err := s.Validate(); err != nil {
		return err
	}
	cfgsvc.SetConfigService(s.Config)
	agent.SetAgentService(s.Agent)
	planner.SetPlanner(s.Planner)
	project.SetProjectService(s.Project)
	ctxsvc.SetContextService(s.Context)
	snapshot.SetSnapshotService(s.Snapshot)
	debate.SetDebateManager(s.Debate)
	summarize.SetSummarizer(s.Summarize)
	floweng.SetEngine(s.Floweng)
	workspace.SetService(s.Workspace)
	guard.SetService(s.Guard)
	// Keep workspace write path forced through the same guard instance when possible.
	if fs, ok := s.Workspace.(*workspace.FSService); ok {
		if ge, ok := s.Guard.(*guard.Engine); ok {
			fs.SetGuard(ge)
		} else {
			fs.SetGuard(s.Guard)
		}
	}
	return nil
}

// Reset clears the global compatibility layer for services owned by this container.
func (s Services) Reset() {
	cfgsvc.SetConfigService(nil)
	agent.SetAgentService(nil)
	planner.SetPlanner(nil)
	project.SetProjectService(nil)
	ctxsvc.SetContextService(nil)
	snapshot.SetSnapshotService(nil)
	debate.SetDebateManager(nil)
	summarize.SetSummarizer(nil)
	floweng.SetEngine(nil)
	workspace.SetService(nil)
	guard.SetService(nil)
}
