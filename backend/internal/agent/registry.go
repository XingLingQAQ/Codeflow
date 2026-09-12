package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for registry operations.
var (
	ErrBuiltinProtected   = fmt.Errorf("cannot modify builtin agent")
	ErrAgentAssetNotFound = fmt.Errorf("agent asset not found")
)

// AgentRegistry is the agent asset service.
type AgentRegistry interface {
	Create(ctx context.Context, req *CreateAgentRequest) (*AgentAsset, error)
	Get(ctx context.Context, id string) (*AgentAsset, error)
	List(ctx context.Context) ([]*AgentAsset, error)
	ListFiltered(ctx context.Context, stage string, role RoleBase, source AgentSource) ([]*AgentAsset, error)
	Update(ctx context.Context, id string, req *UpdateAgentRequest) (*AgentAsset, error)
	Delete(ctx context.Context, id string) error
	IncrementUsage(ctx context.Context, id string) error
	SetScore(ctx context.Context, id string, score float64) error
}

// InMemoryAgentRegistry is the agent registry (optional SQLite durability via store).
type InMemoryAgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*AgentAsset
	store  *sqliteAgentStore // optional
}

// NewInMemoryAgentRegistry creates a registry with built-in agents (memory only).
func NewInMemoryAgentRegistry() *InMemoryAgentRegistry {
	r := &InMemoryAgentRegistry{agents: make(map[string]*AgentAsset)}
	r.seedBuiltins()
	return r
}

func (r *InMemoryAgentRegistry) seedBuiltins() {
	for _, a := range builtinAgents() {
		r.agents[a.ID] = a
	}
}

// Create adds an agent asset.
func (r *InMemoryAgentRegistry) Create(ctx context.Context, req *CreateAgentRequest) (*AgentAsset, error) {
	if req == nil {
		return nil, fmt.Errorf("create request is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if !req.RoleBase.Valid() {
		return nil, fmt.Errorf("role_base must be one of: %s", validRoleBases())
	}
	src := req.Source
	if src == "" || src == SourceBuiltin {
		src = SourceUser
	}
	if !src.Valid() {
		return nil, fmt.Errorf("source must be one of: %s", validSources())
	}
	ver := req.Version
	if ver == "" {
		ver = "0.1.0"
	}
	now := time.Now().UTC()
	a := &AgentAsset{
		ID:           uuid.New().String(),
		Name:         name,
		Avatar:       req.Avatar,
		Description:  req.Description,
		Version:      ver,
		Source:       src,
		RoleBase:     req.RoleBase,
		SystemPrompt: req.SystemPrompt,
		StageTags:    append([]string(nil), req.StageTags...),
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if req.Binding != nil {
		a.Binding = *req.Binding
	}
	if req.Mounts != nil {
		a.Mounts.MCPTools = append([]string(nil), req.Mounts.MCPTools...)
		a.Mounts.Skills = append([]string(nil), req.Mounts.Skills...)
	}

	r.mu.Lock()
	r.agents[a.ID] = cloneAgent(a)
	if r.store != nil {
		if err := r.store.put(a); err != nil {
			delete(r.agents, a.ID)
			r.mu.Unlock()
			return nil, err
		}
	}
	r.mu.Unlock()
	return cloneAgent(a), nil
}

// Get returns an agent asset by id.
func (r *InMemoryAgentRegistry) Get(ctx context.Context, id string) (*AgentAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAgentAssetNotFound, id)
	}
	return cloneAgent(a), nil
}

// List returns all agent assets.
func (r *InMemoryAgentRegistry) List(ctx context.Context) ([]*AgentAsset, error) {
	return r.ListFiltered(ctx, "", "", "")
}

// ListFiltered returns agent assets optionally filtered by stage tag, role base, and source.
// Empty filter values match all.
func (r *InMemoryAgentRegistry) ListFiltered(ctx context.Context, stage string, role RoleBase, source AgentSource) ([]*AgentAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stage = strings.ToLower(strings.TrimSpace(stage))
	out := make([]*AgentAsset, 0, len(r.agents))
	for _, a := range r.agents {
		if role != "" && a.RoleBase != role {
			continue
		}
		if source != "" && a.Source != source {
			continue
		}
		if stage != "" && len(a.StageTags) > 0 {
			match := false
			for _, t := range a.StageTags {
				if strings.ToLower(t) == stage {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, cloneAgent(a))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Update patches an agent asset.
func (r *InMemoryAgentRegistry) Update(ctx context.Context, id string, req *UpdateAgentRequest) (*AgentAsset, error) {
	if req == nil {
		return nil, fmt.Errorf("update request is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.agents[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAgentAssetNotFound, id)
	}
	if a.Source == SourceBuiltin {
		return nil, fmt.Errorf("%w: %s", ErrBuiltinProtected, id)
	}
	prev := cloneAgent(a)
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			return nil, fmt.Errorf("name cannot be empty")
		}
		a.Name = n
	}
	if req.Avatar != nil {
		a.Avatar = *req.Avatar
	}
	if req.Description != nil {
		a.Description = *req.Description
	}
	if req.Version != nil {
		a.Version = *req.Version
	}
	if req.RoleBase != nil {
		if !req.RoleBase.Valid() {
			return nil, fmt.Errorf("role_base must be one of: %s", validRoleBases())
		}
		a.RoleBase = *req.RoleBase
	}
	if req.SystemPrompt != nil {
		a.SystemPrompt = *req.SystemPrompt
	}
	if req.Binding != nil {
		a.Binding = *req.Binding
	}
	if req.Mounts != nil {
		a.Mounts.MCPTools = append([]string(nil), req.Mounts.MCPTools...)
		a.Mounts.Skills = append([]string(nil), req.Mounts.Skills...)
	}
	if req.StageTags != nil {
		a.StageTags = append([]string(nil), req.StageTags...)
	}
	if req.Enabled != nil {
		a.Enabled = *req.Enabled
	}
	a.UpdatedAt = time.Now().UTC()
	if r.store != nil {
		if err := r.store.put(a); err != nil {
			r.agents[id] = prev
			return nil, err
		}
	}
	return cloneAgent(a), nil
}

// Delete removes an agent asset.
func (r *InMemoryAgentRegistry) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.agents[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrAgentAssetNotFound, id)
	}
	if a.Source == SourceBuiltin {
		return fmt.Errorf("%w: %s", ErrBuiltinProtected, id)
	}
	if r.store != nil {
		if err := r.store.delete(id); err != nil {
			return err
		}
	}
	delete(r.agents, id)
	return nil
}

// IncrementUsage atomically bumps the usage counter for an agent.
func (r *InMemoryAgentRegistry) IncrementUsage(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.agents[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrAgentAssetNotFound, id)
	}
	a.Stats.UsageCount++
	a.UpdatedAt = time.Now().UTC()
	if r.store != nil {
		if err := r.store.putStats(a); err != nil {
			return err
		}
	}
	return nil
}

// SetScore sets the quality score for an agent.
func (r *InMemoryAgentRegistry) SetScore(ctx context.Context, id string, score float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.agents[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrAgentAssetNotFound, id)
	}
	a.Stats.Score = score
	a.UpdatedAt = time.Now().UTC()
	if r.store != nil {
		if err := r.store.putStats(a); err != nil {
			return err
		}
	}
	return nil
}

// Close releases the optional durable store.
func (r *InMemoryAgentRegistry) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store == nil {
		return nil
	}
	err := r.store.Close()
	r.store = nil
	return err
}

// --- global registry ---

var (
	agentRegMu      sync.RWMutex
	defaultAgentReg AgentRegistry
)

// HasAgentRegistry reports whether an agent registry was explicitly set.
func HasAgentRegistry() bool {
	agentRegMu.RLock()
	defer agentRegMu.RUnlock()
	return defaultAgentReg != nil
}

// GetAgentRegistry returns the process-wide agent registry.
func GetAgentRegistry() AgentRegistry {
	agentRegMu.RLock()
	r := defaultAgentReg
	agentRegMu.RUnlock()
	if r != nil {
		return r
	}
	agentRegMu.Lock()
	defer agentRegMu.Unlock()
	if defaultAgentReg == nil {
		defaultAgentReg = NewInMemoryAgentRegistry()
	}
	return defaultAgentReg
}

// SetAgentRegistry sets the process-wide agent registry.
func SetAgentRegistry(r AgentRegistry) {
	agentRegMu.Lock()
	defer agentRegMu.Unlock()
	defaultAgentReg = r
}
