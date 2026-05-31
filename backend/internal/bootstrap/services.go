// Package bootstrap wires core backend services for the server runtime.
package bootstrap

import (
	"fmt"

	"github.com/codeflow/backend/internal/agent"
	cfgsvc "github.com/codeflow/backend/internal/config"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
)

// Services contains the explicit service dependencies required by the server runtime.
// It is a compatibility-oriented DI boundary: Apply wires existing global accessors,
// while handlers and legacy packages can keep using Get*/Set* during migration.
type Services struct {
	Config  cfgsvc.IConfigService
	Agent   agent.IAgentService
	Planner planner.IPlanner
	Project project.IProjectService
	Context ctxsvc.IContextService
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
	return nil
}

// Reset clears the global compatibility layer for services owned by this container.
func (s Services) Reset() {
	cfgsvc.SetConfigService(nil)
	agent.SetAgentService(nil)
	planner.SetPlanner(nil)
	project.SetProjectService(nil)
	ctxsvc.SetContextService(nil)
}
