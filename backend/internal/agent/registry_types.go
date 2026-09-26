package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ErrAgentDisabled rejects starting a new run with a disabled asset
// (T1.03.b). The run entry point (S1) calls AssertRunnable before wiring a
// run; disabling never erases history, so revision queries stay valid.
var ErrAgentDisabled = fmt.Errorf("agent asset is disabled")

// AgentSource identifies where an agent asset came from.
type AgentSource string

const (
	SourceBuiltin AgentSource = "builtin"
	SourceUser    AgentSource = "user"
	SourcePlugin  AgentSource = "plugin"
)

// Valid reports whether s is a recognized agent source.
func (s AgentSource) Valid() bool {
	switch s {
	case SourceBuiltin, SourceUser, SourcePlugin:
		return true
	default:
		return false
	}
}

// RoleBase is the base role archetype an agent inherits.
type RoleBase string

const (
	RoleBaseMain       RoleBase = "main"
	RoleBaseCoder      RoleBase = "coder"
	RoleBaseSub        RoleBase = "sub"
	RoleBaseCritic     RoleBase = "critic"
	RoleBaseResearcher RoleBase = "researcher"
)

// Valid reports whether r is a recognized role base.
func (r RoleBase) Valid() bool {
	switch r {
	case RoleBaseMain, RoleBaseCoder, RoleBaseSub, RoleBaseCritic, RoleBaseResearcher:
		return true
	default:
		return false
	}
}

// Binding is the runtime model/channel configuration for an agent.
type Binding struct {
	Model       string   `json:"model,omitempty"`
	Channel     string   `json:"channel,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
}

// Mounts lists the MCP tools and skills attached to an agent.
type Mounts struct {
	MCPTools []string `json:"mcp_tools,omitempty"`
	Skills   []string `json:"skills,omitempty"`
}

// Stats holds usage and quality telemetry for an agent asset.
type Stats struct {
	UsageCount int64   `json:"usage_count"`
	Score      float64 `json:"score"`
}

// AgentAsset is a versioned, source-tagged agent definition in the registry.
//
// Purpose/Backend/ModelPolicy/Fit are configuration: they belong to the
// immutable revision payload (T4.04.a), so changing any of them is a new
// revision. Stats is the opposite - it is cumulative telemetry, not
// configuration. The field stays on the struct because the legacy read model
// exposes it, but it is never authoritative: the counters live in
// agent_revision_stats keyed by (agent_id, revision) and the store projects
// them onto the asset on read (see sqlite_registry.go). Nothing may edit Stats
// as if it were a configuration field.
type AgentAsset struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Avatar       string       `json:"avatar,omitempty"`
	Description  string       `json:"description,omitempty"`
	Version      string       `json:"version"`
	Source       AgentSource  `json:"source"`
	RoleBase     RoleBase     `json:"role_base"`
	SystemPrompt string       `json:"system_prompt,omitempty"`
	Binding      Binding      `json:"binding"`
	Mounts       Mounts       `json:"mounts"`
	StageTags    []string     `json:"stage_tags,omitempty"`
	Purpose      string       `json:"purpose,omitempty"`
	Backend      string       `json:"backend,omitempty"`
	ModelPolicy  *ModelPolicy `json:"model_policy,omitempty"`
	Fit          *Fit         `json:"fit,omitempty"`
	Stats        Stats        `json:"stats"`
	Enabled      bool         `json:"enabled"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// CreateAgentRequest creates an agent asset in the registry.
type CreateAgentRequest struct {
	Name         string      `json:"name" binding:"required"`
	Avatar       string      `json:"avatar,omitempty"`
	Description  string      `json:"description,omitempty"`
	Version      string      `json:"version,omitempty"`
	Source       AgentSource `json:"source,omitempty"`
	RoleBase     RoleBase    `json:"role_base" binding:"required"`
	SystemPrompt string      `json:"system_prompt,omitempty"`
	Binding      *Binding    `json:"binding,omitempty"`
	Mounts       *Mounts     `json:"mounts,omitempty"`
	StageTags    []string    `json:"stage_tags,omitempty"`
}

// UpdateAgentRequest patches an agent asset.
type UpdateAgentRequest struct {
	Name         *string   `json:"name,omitempty"`
	Avatar       *string   `json:"avatar,omitempty"`
	Description  *string   `json:"description,omitempty"`
	Version      *string   `json:"version,omitempty"`
	RoleBase     *RoleBase `json:"role_base,omitempty"`
	SystemPrompt *string   `json:"system_prompt,omitempty"`
	Binding      *Binding  `json:"binding,omitempty"`
	Mounts       *Mounts   `json:"mounts,omitempty"`
	StageTags    []string  `json:"stage_tags,omitempty"`
	Enabled      *bool     `json:"enabled,omitempty"`
}

// AssertRunnable reports whether the agent may start a new run: nil for an
// enabled asset, ErrAgentDisabled for a disabled one, ErrAgentAssetNotFound
// for an unknown id. It gates new runs only — a disabled asset keeps its
// revisions readable through the store's headRevision/revisionAsset queries.
func (r *InMemoryAgentRegistry) AssertRunnable(ctx context.Context, id string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrAgentAssetNotFound, id)
	}
	if !a.Enabled {
		return fmt.Errorf("%w: %s", ErrAgentDisabled, id)
	}
	return nil
}

// cloneAgent returns a deep copy of a, safe to return across lock boundaries.
func cloneAgent(a *AgentAsset) *AgentAsset {
	if a == nil {
		return nil
	}
	cp := *a
	cp.StageTags = append([]string(nil), a.StageTags...)
	cp.Mounts.MCPTools = append([]string(nil), a.Mounts.MCPTools...)
	cp.Mounts.Skills = append([]string(nil), a.Mounts.Skills...)
	if a.ModelPolicy != nil {
		mp := *a.ModelPolicy
		mp.AllowedModels = append([]string(nil), a.ModelPolicy.AllowedModels...)
		mp.Temperature = copyFloat(a.ModelPolicy.Temperature)
		mp.MaxTokens = copyInt(a.ModelPolicy.MaxTokens)
		cp.ModelPolicy = &mp
	}
	if a.Fit != nil {
		f := *a.Fit
		f.Stages = append([]string(nil), a.Fit.Stages...)
		f.TaskTypes = append([]string(nil), a.Fit.TaskTypes...)
		cp.Fit = &f
	}
	if a.Binding.Temperature != nil {
		t := *a.Binding.Temperature
		cp.Binding.Temperature = &t
	}
	if a.Binding.MaxTokens != nil {
		m := *a.Binding.MaxTokens
		cp.Binding.MaxTokens = &m
	}
	return &cp
}

// validRoleBases returns the recognized role_base values for error messages.
func validRoleBases() string {
	return strings.Join([]string{
		string(RoleBaseMain), string(RoleBaseCoder),
		string(RoleBaseSub), string(RoleBaseCritic),
		string(RoleBaseResearcher),
	}, ", ")
}

// validSources returns the recognized source values for error messages.
func validSources() string {
	return strings.Join([]string{
		string(SourceBuiltin), string(SourceUser), string(SourcePlugin),
	}, ", ")
}
