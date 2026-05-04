package isolation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/audit"
)

var (
	ErrContainerNotFound = errors.New("container not found")
	ErrRoleNotFound      = errors.New("role not found")
	ErrAccessDenied      = errors.New("access denied")
	ErrInvalidInput      = errors.New("invalid input")
	ErrQuotaExceeded     = errors.New("quota exceeded")
	ErrMaxConcurrent     = errors.New("max concurrent containers exceeded")
)

// RBACManager RBAC权限管理器
type RBACManager struct {
	roles map[IsolationAgentRole]RoleDefinition
	mu    sync.RWMutex
}

// NewRBACManager 创建RBAC管理器
func NewRBACManager() *RBACManager {
	mgr := &RBACManager{
		roles: make(map[IsolationAgentRole]RoleDefinition),
	}
	// 加载默认角色
	for role, def := range DefaultRoles {
		mgr.roles[role] = def
	}
	return mgr
}

// GetRole 获取角色定义
func (m *RBACManager) GetRole(role IsolationAgentRole) (*RoleDefinition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	def, ok := m.roles[role]
	if !ok {
		return nil, false
	}
	return &def, true
}

// HasPermission 检查权限
func (m *RBACManager) HasPermission(role IsolationAgentRole, resource ResourceType, level PermissionLevel) bool {
	perms := m.GetEffectivePermissions(role)
	requiredPriority := PermissionPriority[level]

	for _, perm := range perms {
		if perm.Resource == resource {
			actualPriority := PermissionPriority[perm.Level]
			if actualPriority >= requiredPriority {
				return true
			}
		}
	}
	return false
}

// GetEffectivePermissions 获取有效权限（含继承）
func (m *RBACManager) GetEffectivePermissions(role IsolationAgentRole) []Permission {
	m.mu.RLock()
	defer m.mu.RUnlock()

	visited := make(map[IsolationAgentRole]bool)
	return m.collectPermissions(role, visited)
}

func (m *RBACManager) collectPermissions(role IsolationAgentRole, visited map[IsolationAgentRole]bool) []Permission {
	if visited[role] {
		return nil
	}
	visited[role] = true

	def, ok := m.roles[role]
	if !ok {
		return nil
	}

	perms := make([]Permission, len(def.Permissions))
	copy(perms, def.Permissions)

	// 收集继承的权限
	for _, inheritedRole := range def.Inherits {
		inheritedPerms := m.collectPermissions(inheritedRole, visited)
		perms = append(perms, inheritedPerms...)
	}

	return perms
}

// RegisterRole 注册自定义角色
func (m *RBACManager) RegisterRole(definition RoleDefinition) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roles[definition.Name] = definition
	return nil
}

// CheckResourceAccess 检查资源访问
func (m *RBACManager) CheckResourceAccess(role IsolationAgentRole, resource ResourceType, path string, action string) bool {
	perms := m.GetEffectivePermissions(role)

	for _, perm := range perms {
		if perm.Resource != resource {
			continue
		}

		// 检查路径模式
		if perm.Pattern != "" && path != "" {
			if !matchPattern(perm.Pattern, path) {
				continue
			}
		}

		// 检查动作权限
		if checkActionPermission(perm.Level, action) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, path string) bool {
	// 简化的glob匹配
	path = filepath.ToSlash(path)

	// 先处理{a,b,c}模式
	pattern = strings.ReplaceAll(pattern, "{", "(")
	pattern = strings.ReplaceAll(pattern, "}", ")")
	pattern = strings.ReplaceAll(pattern, ",", "|")

	// 转义点号（在处理*之前）
	pattern = strings.ReplaceAll(pattern, ".", "\\.")

	// 用占位符保护 **/
	pattern = strings.ReplaceAll(pattern, "**/", "\x00GLOB_DOUBLESTAR\x00")

	// 处理单个*（不匹配/）
	pattern = strings.ReplaceAll(pattern, "*", "[^/]*")

	// 恢复 **/ - 匹配任意路径前缀（包括空），贪婪
	pattern = strings.ReplaceAll(pattern, "\x00GLOB_DOUBLESTAR\x00", "(.*/)?")

	pattern = "^" + pattern + "$"

	matched, _ := regexp.MatchString(pattern, path)
	return matched
}

func checkActionPermission(level PermissionLevel, action string) bool {
	switch action {
	case "read":
		return PermissionPriority[level] >= PermissionPriority[PermissionRead]
	case "write":
		return PermissionPriority[level] >= PermissionPriority[PermissionWrite]
	case "execute":
		return PermissionPriority[level] >= PermissionPriority[PermissionExecute]
	case "admin":
		return level == PermissionAdmin
	default:
		return false
	}
}

func sortedContextKeys(values map[string]interface{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (m *IsolationManager) recordIsolationAudit(ctx context.Context, container *ContextContainer, action string, outcome audit.AuditOutcome, severity audit.AuditSeverity, resourceType string, resourceID string, resourcePath string, details map[string]interface{}) string {
	actor := audit.AuditActor{Type: "service"}
	resource := audit.AuditResource{
		Type: resourceType,
		ID:   resourceID,
		Name: resourcePath,
		Path: resourcePath,
	}
	if resource.Name == "" {
		resource.Name = resourceID
	}
	if container != nil {
		actor = audit.AuditActor{
			ID:   container.ID,
			Type: "agent",
			Name: string(container.Role),
		}
		if resource.ID == "" {
			resource.ID = container.ID
		}
		if resource.Name == "" {
			resource.Name = string(container.Role)
		}
	}
	if resource.ID == "" {
		resource.ID = action
	}
	if resource.Name == "" {
		resource.Name = action
	}

	entryID, err := audit.Record(ctx, &audit.AuditLogEntry{
		EventType: audit.EventIsolation,
		Severity:  severity,
		Actor:     actor,
		Resource:  resource,
		Action:    action,
		Outcome:   outcome,
		Details:   details,
	})
	if err != nil {
		log.Printf("[WARN] isolation audit record failed: action=%s resource_type=%s resource_id=%s err=%v", action, resource.Type, resource.ID, err)
		return ""
	}
	return entryID
}

func boolToOutcome(allowed bool) audit.AuditOutcome {
	if allowed {
		return audit.OutcomeSuccess
	}
	return audit.OutcomeFailure
}

func boolToSeverity(allowed bool) audit.AuditSeverity {
	if allowed {
		return audit.SeverityInfo
	}
	return audit.SeverityWarning
}

func validationSeverity(result *IOValidationResult) audit.AuditSeverity {
	if result == nil {
		return audit.SeverityWarning
	}
	if !result.Valid {
		return audit.SeverityWarning
	}
	if len(result.Warnings) > 0 {
		return audit.SeverityInfo
	}
	return audit.SeverityInfo
}

func validationOutcome(result *IOValidationResult) audit.AuditOutcome {
	if result == nil || !result.Valid {
		return audit.OutcomeFailure
	}
	return audit.OutcomeSuccess
}

func previewInput(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	const limit = 120
	if len(trimmed) > limit {
		return trimmed[:limit] + "..."
	}
	return trimmed
}

func containsSensitiveWarning(warnings []string) bool {
	for _, warning := range warnings {
		if strings.Contains(strings.ToLower(warning), "sensitive") {
			return true
		}
	}
	return false
}

func sanitizedChanged(original string, sanitized string) bool {
	return original != sanitized
}

func redactedMarkerCount(sanitized string) int {
	if sanitized == "" {
		return 0
	}
	return strings.Count(strings.ToUpper(sanitized), "[REDACTED]")
}

func blockedPatternCount(blocked []string) int {
	return len(blocked)
}

func warningCount(warnings []string) int {
	return len(warnings)
}

func resolveContainerRole(container *ContextContainer) string {
	if container == nil {
		return ""
	}
	return string(container.Role)
}

func resolveContainerID(container *ContextContainer, fallback string) string {
	if container != nil && container.ID != "" {
		return container.ID
	}
	return fallback
}

func resolveResourceID(request AccessRequest) string {
	if request.ResourcePath != "" {
		return request.ResourcePath
	}
	return string(request.Resource)
}

func resolveResourceName(request AccessRequest) string {
	if request.ResourcePath != "" {
		return request.ResourcePath
	}
	return request.Action
}

// IsolationManager 隔离管理器
type IsolationManager struct {
	containers map[string]*ContextContainer
	rbac       *RBACManager
	mu         sync.RWMutex

	// 敏感模式检测
	sensitivePatterns []*regexp.Regexp
	injectionPatterns []*regexp.Regexp
}

// NewIsolationManager 创建隔离管理器
func NewIsolationManager(rbac *RBACManager) *IsolationManager {
	if rbac == nil {
		rbac = NewRBACManager()
	}

	mgr := &IsolationManager{
		containers: make(map[string]*ContextContainer),
		rbac:       rbac,
	}

	// 初始化敏感数据模式
	mgr.sensitivePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)(secret|token)\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)(private[_-]?key)\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)(access[_-]?token)\s*[:=]\s*\S+`),
		regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}\b`), // Base64-like strings
	}

	// 初始化注入检测模式
	mgr.injectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(;\s*rm\s+-rf|;\s*del\s+/|;\s*format\s+)`),
		regexp.MustCompile(`(?i)(\|\s*sh|\|\s*bash|\|\s*cmd)`),
		regexp.MustCompile(`(?i)(eval\s*\(|exec\s*\()`),
		regexp.MustCompile(`(?i)(__import__|os\.system|subprocess)`),
		regexp.MustCompile(`(?i)(drop\s+table|truncate\s+table|delete\s+from)`),
	}

	return mgr
}

// CreateContainer 创建隔离容器
func (m *IsolationManager) CreateContainer(ctx context.Context, role IsolationAgentRole, parentID string) (*ContextContainer, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// 验证角色
	roleDef, ok := m.rbac.GetRole(role)
	if !ok {
		return nil, ErrRoleNotFound
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查并发限制
	count := 0
	for _, c := range m.containers {
		if c.Role == role {
			count++
		}
	}
	if roleDef.MaxConcurrent > 0 && count >= roleDef.MaxConcurrent {
		return nil, ErrMaxConcurrent
	}

	// 生成容器ID
	id := generateID("container")

	container := &ContextContainer{
		ID:             id,
		Role:           role,
		ParentID:       parentID,
		CreatedAt:      time.Now().UnixMilli(),
		Metadata:       make(map[string]interface{}),
		IsolationLevel: string(IsolationProcess),
	}

	m.containers[id] = container
	return container, nil
}

// GetContainer 获取容器
func (m *IsolationManager) GetContainer(ctx context.Context, containerID string) (*ContextContainer, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	container, ok := m.containers[containerID]
	if !ok {
		return nil, ErrContainerNotFound
	}
	return container, nil
}

// DestroyContainer 销毁容器
func (m *IsolationManager) DestroyContainer(ctx context.Context, containerID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.containers[containerID]; !ok {
		return ErrContainerNotFound
	}

	delete(m.containers, containerID)
	return nil
}

// CheckAccess 检查访问权限
func (m *IsolationManager) CheckAccess(ctx context.Context, request AccessRequest) (*AccessDecision, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.mu.RLock()
	container, ok := m.containers[request.ContainerID]
	m.mu.RUnlock()

	if !ok {
		entryID := m.recordIsolationAudit(ctx, nil, "check_access", audit.OutcomeFailure, audit.SeverityWarning, string(request.Resource), resolveResourceID(request), request.ResourcePath, map[string]interface{}{
			"reason":        "container not found",
			"container_id":  request.ContainerID,
			"resource":      string(request.Resource),
			"resource_name": resolveResourceName(request),
			"action":        request.Action,
			"context_keys":  sortedContextKeys(request.Context),
		})
		return &AccessDecision{
			Allowed: false,
			Reason:  "container not found",
			AuditID: entryID,
		}, nil
	}

	allowed := m.rbac.CheckResourceAccess(
		container.Role,
		request.Resource,
		request.ResourcePath,
		request.Action,
	)

	decision := &AccessDecision{Allowed: allowed}
	if allowed {
		decision.Reason = "access granted by RBAC policy"
	} else {
		decision.Reason = "access denied: insufficient permissions"
	}

	decision.AuditID = m.recordIsolationAudit(ctx, container, "check_access", boolToOutcome(allowed), boolToSeverity(allowed), string(request.Resource), resolveResourceID(request), request.ResourcePath, map[string]interface{}{
		"container_id":   resolveContainerID(container, request.ContainerID),
		"container_role": resolveContainerRole(container),
		"resource":       string(request.Resource),
		"resource_name":  resolveResourceName(request),
		"action":         request.Action,
		"allowed":        allowed,
		"reason":         decision.Reason,
		"context_keys":   sortedContextKeys(request.Context),
	})

	return decision, nil
}

// ValidateIO 验证I/O
func (m *IsolationManager) ValidateIO(ctx context.Context, containerID string, input string, direction string) (*IOValidationResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.mu.RLock()
	container, ok := m.containers[containerID]
	m.mu.RUnlock()

	result := &IOValidationResult{
		Valid:           true,
		Warnings:        make([]string, 0),
		BlockedPatterns: make([]string, 0),
	}

	sanitized := input

	for _, pattern := range m.injectionPatterns {
		if pattern.MatchString(input) {
			result.Valid = false
			result.BlockedPatterns = append(result.BlockedPatterns, pattern.String())
		}
	}

	for _, pattern := range m.sensitivePatterns {
		if pattern.MatchString(input) {
			result.Warnings = append(result.Warnings, "sensitive data detected")
			sanitized = pattern.ReplaceAllString(sanitized, "[REDACTED]")
		}
	}

	result.SanitizedInput = sanitized

	details := map[string]interface{}{
		"container_id":               resolveContainerID(container, containerID),
		"container_role":             resolveContainerRole(container),
		"direction":                  direction,
		"valid":                      result.Valid,
		"warning_count":              warningCount(result.Warnings),
		"blocked_pattern_count":      blockedPatternCount(result.BlockedPatterns),
		"contains_sensitive_warning": containsSensitiveWarning(result.Warnings),
		"sanitized_changed":          sanitizedChanged(input, sanitized),
		"redacted_marker_count":      redactedMarkerCount(sanitized),
	}
	if !ok {
		details["reason"] = "container not found"
	}

	m.recordIsolationAudit(ctx, container, "validate_io", validationOutcome(result), validationSeverity(result), "io", resolveContainerID(container, containerID), direction, details)
	return result, nil
}

// GetContainersByRole 按角色获取容器
func (m *IsolationManager) GetContainersByRole(ctx context.Context, role IsolationAgentRole) ([]*ContextContainer, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var containers []*ContextContainer
	for _, c := range m.containers {
		if c.Role == role {
			containers = append(containers, c)
		}
	}
	return containers, nil
}

// SetResourceQuota 设置资源配额
func (m *IsolationManager) SetResourceQuota(ctx context.Context, containerID string, quota ResourceQuota) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	container, ok := m.containers[containerID]
	if !ok {
		return ErrContainerNotFound
	}

	container.ResourceQuota = &quota
	return nil
}

// GetRBACManager 获取RBAC管理器
func (m *IsolationManager) GetRBACManager() *RBACManager {
	return m.rbac
}

// GetAllContainers 获取所有容器
func (m *IsolationManager) GetAllContainers(ctx context.Context) ([]*ContextContainer, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	containers := make([]*ContextContainer, 0, len(m.containers))
	for _, c := range m.containers {
		containers = append(containers, c)
	}
	return containers, nil
}

func generateID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
