// Package isolation - Isolation Service wrapper
package isolation

import (
	"context"
	"sync"
)

// IsolationService wraps IsolationManager with global instance management
type IsolationService struct {
	manager *IsolationManager
}

var (
	globalIsolationService *IsolationService
	globalServiceMu        sync.RWMutex
)

// NewIsolationService creates a new isolation service
func NewIsolationService(rbac *RBACManager) *IsolationService {
	return &IsolationService{
		manager: NewIsolationManager(rbac),
	}
}

// SetIsolationService sets the global isolation service instance
func SetIsolationService(svc *IsolationService) {
	globalServiceMu.Lock()
	defer globalServiceMu.Unlock()
	globalIsolationService = svc
}

// GetIsolationService returns the global isolation service instance
func GetIsolationService() *IsolationService {
	globalServiceMu.RLock()
	defer globalServiceMu.RUnlock()
	return globalIsolationService
}

// HasIsolationService reports whether the global isolation service has been configured.
func HasIsolationService() bool {
	globalServiceMu.RLock()
	defer globalServiceMu.RUnlock()
	return globalIsolationService != nil
}

// CreateContainer creates a new isolation container
func (s *IsolationService) CreateContainer(ctx context.Context, role IsolationAgentRole, parentID string) (*ContextContainer, error) {
	return s.manager.CreateContainer(ctx, role, parentID)
}

// GetContainer retrieves a container by ID
func (s *IsolationService) GetContainer(ctx context.Context, containerID string) (*ContextContainer, error) {
	return s.manager.GetContainer(ctx, containerID)
}

// DestroyContainer destroys a container
func (s *IsolationService) DestroyContainer(ctx context.Context, containerID string) error {
	return s.manager.DestroyContainer(ctx, containerID)
}

// CheckAccess checks access permissions
func (s *IsolationService) CheckAccess(ctx context.Context, request AccessRequest) (*AccessDecision, error) {
	return s.manager.CheckAccess(ctx, request)
}

// ValidateIO validates I/O for security
func (s *IsolationService) ValidateIO(ctx context.Context, containerID string, input string, direction string) (*IOValidationResult, error) {
	return s.manager.ValidateIO(ctx, containerID, input, direction)
}

// GetContainersByRole retrieves containers by role
func (s *IsolationService) GetContainersByRole(ctx context.Context, role IsolationAgentRole) ([]*ContextContainer, error) {
	return s.manager.GetContainersByRole(ctx, role)
}

// GetAllContainers retrieves all containers
func (s *IsolationService) GetAllContainers(ctx context.Context) ([]*ContextContainer, error) {
	return s.manager.GetAllContainers(ctx)
}

// SetResourceQuota sets resource quota for a container
func (s *IsolationService) SetResourceQuota(ctx context.Context, containerID string, quota ResourceQuota) error {
	return s.manager.SetResourceQuota(ctx, containerID, quota)
}

// GetRBACManager returns the RBAC manager
func (s *IsolationService) GetRBACManager() *RBACManager {
	return s.manager.GetRBACManager()
}

// GetRole retrieves a role definition
func (s *IsolationService) GetRole(role IsolationAgentRole) (*RoleDefinition, bool) {
	return s.manager.rbac.GetRole(role)
}

// GetAllRoles retrieves all role definitions
func (s *IsolationService) GetAllRoles() map[IsolationAgentRole]RoleDefinition {
	rbac := s.manager.GetRBACManager()
	rbac.mu.RLock()
	defer rbac.mu.RUnlock()

	roles := make(map[IsolationAgentRole]RoleDefinition)
	for k, v := range rbac.roles {
		roles[k] = v
	}
	return roles
}

// RegisterRole registers a custom role
func (s *IsolationService) RegisterRole(definition RoleDefinition) error {
	return s.manager.rbac.RegisterRole(definition)
}

// HasPermission checks if a role has a specific permission
func (s *IsolationService) HasPermission(role IsolationAgentRole, resource ResourceType, level PermissionLevel) bool {
	return s.manager.rbac.HasPermission(role, resource, level)
}

// GetEffectivePermissions retrieves effective permissions for a role
func (s *IsolationService) GetEffectivePermissions(role IsolationAgentRole) []Permission {
	return s.manager.rbac.GetEffectivePermissions(role)
}

// CheckResourceAccess checks resource access for a role
func (s *IsolationService) CheckResourceAccess(role IsolationAgentRole, resource ResourceType, path string, action string) bool {
	return s.manager.rbac.CheckResourceAccess(role, resource, path, action)
}
