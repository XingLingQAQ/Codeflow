// Package handlers - Isolation API handlers
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/codeflow/backend/internal/isolation"
	"github.com/gin-gonic/gin"
)

// CreateContainerRequest represents a request to create a container
type CreateContainerRequest struct {
	Role     string `json:"role" binding:"required"`
	ParentID string `json:"parent_id,omitempty"`
}

// SetQuotaRequest represents a request to set resource quota
type SetQuotaRequest struct {
	MaxMemoryMB     int `json:"max_memory_mb,omitempty"`
	MaxFileSizeMB   int `json:"max_file_size_mb,omitempty"`
	MaxFileCount    int `json:"max_file_count,omitempty"`
	MaxNetworkConns int `json:"max_network_conns,omitempty"`
	MaxCPUPercent   int `json:"max_cpu_percent,omitempty"`
}

// CheckAccessRequest represents a request to check access
type CheckAccessRequest struct {
	ContainerID  string `json:"container_id" binding:"required"`
	Resource     string `json:"resource" binding:"required"`
	ResourcePath string `json:"resource_path,omitempty"`
	Action       string `json:"action" binding:"required"`
}

// ValidateIORequest represents a request to validate I/O
type ValidateIORequest struct {
	ContainerID string `json:"container_id" binding:"required"`
	Input       string `json:"input" binding:"required"`
	Direction   string `json:"direction"` // "input" or "output"
}

// RegisterRoleRequest represents a request to register a custom role
type RegisterRoleRequest struct {
	Name          string                 `json:"name" binding:"required"`
	Description   string                 `json:"description"`
	Permissions   []isolation.Permission `json:"permissions" binding:"required"`
	Inherits      []string               `json:"inherits,omitempty"`
	MaxConcurrent int                    `json:"max_concurrent,omitempty"`
}

// GetContainers retrieves all containers or filters by role.
// GET /api/v1/isolation/containers
func GetContainers(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	ctx := context.Background()
	role := c.Query("role")

	var containers []*isolation.ContextContainer
	var err error

	if role != "" {
		containers, err = svc.GetContainersByRole(ctx, isolation.IsolationAgentRole(role))
	} else {
		containers, err = svc.GetAllContainers(ctx)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"containers": containers,
		"count":      len(containers),
	})
}

// CreateContainer creates a new isolation container.
// POST /api/v1/isolation/containers
func CreateContainer(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	var req CreateContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	container, err := svc.CreateContainer(ctx, isolation.IsolationAgentRole(req.Role), req.ParentID)
	if err != nil {
		if err == isolation.ErrRoleNotFound {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
			return
		}
		if err == isolation.ErrMaxConcurrent {
			c.JSON(http.StatusConflict, gin.H{"error": "max concurrent containers exceeded for this role"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, container)
}

// GetContainer retrieves a specific container.
// GET /api/v1/isolation/containers/:id
func GetContainer(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	containerID := c.Param("id")
	ctx := context.Background()

	container, err := svc.GetContainer(ctx, containerID)
	if err != nil {
		if err == isolation.ErrContainerNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, container)
}

// DeleteContainer destroys a container.
// DELETE /api/v1/isolation/containers/:id
func DeleteContainer(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	containerID := c.Param("id")
	ctx := context.Background()

	err := svc.DestroyContainer(ctx, containerID)
	if err != nil {
		if err == isolation.ErrContainerNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "container destroyed"})
}

// SetContainerQuota sets resource quota for a container.
// PUT /api/v1/isolation/containers/:id/quota
func SetContainerQuota(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	containerID := c.Param("id")
	var req SetQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	quota := isolation.ResourceQuota{
		MaxMemoryMB:     req.MaxMemoryMB,
		MaxFileSizeMB:   req.MaxFileSizeMB,
		MaxFileCount:    req.MaxFileCount,
		MaxNetworkConns: req.MaxNetworkConns,
		MaxCPUPercent:   req.MaxCPUPercent,
	}

	err := svc.SetResourceQuota(ctx, containerID, quota)
	if err != nil {
		if err == isolation.ErrContainerNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "quota updated", "quota": quota})
}

// CheckAccess checks access permissions.
// POST /api/v1/isolation/access/check
func CheckAccess(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	var req CheckAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	startTime := time.Now()

	decision, err := svc.CheckAccess(ctx, isolation.AccessRequest{
		ContainerID:  req.ContainerID,
		Resource:     isolation.ResourceType(req.Resource),
		ResourcePath: req.ResourcePath,
		Action:       req.Action,
		Timestamp:    time.Now().UnixMilli(),
	})

	elapsed := time.Since(startTime)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"decision":    decision,
		"latency_ms":  float64(elapsed.Microseconds()) / 1000.0,
	})
}

// ValidateIO validates I/O for security.
// POST /api/v1/isolation/io/validate
func ValidateIO(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	var req ValidateIORequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	direction := req.Direction
	if direction == "" {
		direction = "input"
	}

	ctx := context.Background()
	result, err := svc.ValidateIO(ctx, req.ContainerID, req.Input, direction)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetRoles retrieves all role definitions.
// GET /api/v1/isolation/roles
func GetRoles(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	roles := svc.GetAllRoles()

	// Convert to slice for JSON
	roleList := make([]isolation.RoleDefinition, 0, len(roles))
	for _, role := range roles {
		roleList = append(roleList, role)
	}

	c.JSON(http.StatusOK, gin.H{
		"roles": roleList,
		"count": len(roleList),
	})
}

// GetRole retrieves a specific role definition.
// GET /api/v1/isolation/roles/:name
func GetRole(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	roleName := c.Param("name")
	role, ok := svc.GetRole(isolation.IsolationAgentRole(roleName))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "role not found"})
		return
	}

	c.JSON(http.StatusOK, role)
}

// RegisterRole registers a custom role.
// POST /api/v1/isolation/roles
func RegisterRole(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	var req RegisterRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Convert inherits to IsolationAgentRole
	inherits := make([]isolation.IsolationAgentRole, len(req.Inherits))
	for i, r := range req.Inherits {
		inherits[i] = isolation.IsolationAgentRole(r)
	}

	definition := isolation.RoleDefinition{
		Name:          isolation.IsolationAgentRole(req.Name),
		Description:   req.Description,
		Permissions:   req.Permissions,
		Inherits:      inherits,
		MaxConcurrent: req.MaxConcurrent,
	}

	err := svc.RegisterRole(definition)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "role registered",
		"role":    definition,
	})
}

// GetRolePermissions retrieves effective permissions for a role.
// GET /api/v1/isolation/roles/:name/permissions
func GetRolePermissions(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	roleName := c.Param("name")
	_, ok := svc.GetRole(isolation.IsolationAgentRole(roleName))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "role not found"})
		return
	}

	permissions := svc.GetEffectivePermissions(isolation.IsolationAgentRole(roleName))

	c.JSON(http.StatusOK, gin.H{
		"role":        roleName,
		"permissions": permissions,
		"count":       len(permissions),
	})
}

// CheckRolePermission checks if a role has a specific permission.
// POST /api/v1/isolation/roles/:name/check
func CheckRolePermission(c *gin.Context) {
	svc := isolation.GetIsolationService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "isolation service not available"})
		return
	}

	roleName := c.Param("name")
	var req struct {
		Resource string `json:"resource" binding:"required"`
		Level    string `json:"level" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hasPermission := svc.HasPermission(
		isolation.IsolationAgentRole(roleName),
		isolation.ResourceType(req.Resource),
		isolation.PermissionLevel(req.Level),
	)

	c.JSON(http.StatusOK, gin.H{
		"role":           roleName,
		"resource":       req.Resource,
		"level":          req.Level,
		"has_permission": hasPermission,
	})
}
