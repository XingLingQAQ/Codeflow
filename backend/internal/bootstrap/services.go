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
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/readiness"
	"github.com/codeflow/backend/internal/samg"
	"github.com/codeflow/backend/internal/skill"
	"github.com/codeflow/backend/internal/snapshot"
	"github.com/codeflow/backend/internal/summarize"
	"github.com/codeflow/backend/internal/storage"
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
	Skill          skill.Registry
	AgentRegistry  agent.AgentRegistry
	// PolicyEvaluator, when set, is installed by Apply as the process-wide
	// policy evaluator with enforcement required. When nil, Apply installs the
	// production evaluator (see NewProductionEvaluator), so every bootstrapped
	// chain carries a non-nil evaluator instead of relying on
	// cmd/codeflow-server/main.go to install one (I-49). Call ExecutionPolicy
	// to obtain the fail-closed handle reserved for future Run interfaces.
	PolicyEvaluator policy.Evaluator
	// B5 durable boundaries. These are optional for focused unit-test
	// containers, but production bootstrap must provide all of them.
	Session        storage.ISessionStorage
	Memory         memory.IMemoryService
	RawArchive     memory.IRawArchive
	AtomicMemory   *memory.AtomicMemoryService
	MemoryAgent    *memory.MemoryAgent
	SAMG           *samg.SAMGService
	Preflight      memory.IMemoryPreflight
	RequireDurable bool
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
	if s.Skill == nil {
		missing = append(missing, "skill")
	}
	if s.AgentRegistry == nil {
		missing = append(missing, "agent_registry")
	}
	if s.RequireDurable {
		if s.Session == nil { missing = append(missing, "session") }
		if s.Memory == nil { missing = append(missing, "memory") }
		if s.RawArchive == nil { missing = append(missing, "raw_archive") }
		if s.AtomicMemory == nil { missing = append(missing, "atomic_memory") }
		if s.MemoryAgent == nil { missing = append(missing, "memory_agent") }
		if s.SAMG == nil { missing = append(missing, "samg") }
		if s.Preflight == nil { missing = append(missing, "preflight") }
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
	skill.SetRegistry(s.Skill)
	agent.SetAgentRegistry(s.AgentRegistry)
	// Install the single production evaluator with enforcement required so no
	// execution boundary in a bootstrapped process sees the pre-bootstrap
	// "policy not installed" compatibility branch (I-49).
	policy.RequireEnforcement(true)
	policy.SetEvaluator(s.policyEvaluator())
	if s.Session != nil { project.SetSessionStorage(s.Session) }
	memory.SetMemoryService(s.Memory)
	memory.SetRawArchive(s.RawArchive)
	memory.SetAtomicMemoryService(s.AtomicMemory)
	memory.SetMemoryAgent(s.MemoryAgent)
	samg.SetSAMGService(s.SAMG)
	memory.SetPreflightService(s.Preflight)
	// Keep workspace write path forced through the same guard instance when possible.
	if fs, ok := s.Workspace.(*workspace.FSService); ok {
		if ge, ok := s.Guard.(*guard.Engine); ok {
			fs.SetGuard(ge)
		} else {
			fs.SetGuard(s.Guard)
		}
	}
	// Install the production readiness probes last, so every probe observes the
	// services this Apply just wired (policy evaluator, workspace roots). The
	// batch is registered all-or-nothing and re-registering replaces the
	// previous specs, so repeated Apply calls are idempotent (T0.12.b).
	if err := readiness.Register(s.readinessSpecs()...); err != nil {
		return fmt.Errorf("register readiness probes: %w", err)
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
	skill.SetRegistry(nil)
	agent.SetAgentRegistry(nil)
	project.SetSessionStorage(nil)
	memory.SetMemoryService(nil)
	memory.SetRawArchive(nil)
	memory.SetAtomicMemoryService(nil)
	memory.SetMemoryAgent(nil)
	samg.SetSAMGService(nil)
	memory.SetPreflightService(nil)
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	// Symmetric with Apply: drop the production probes so a torn-down container
	// leaves no probe reading globals it no longer owns.
	readiness.Clear()
}
