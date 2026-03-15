package isolation

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/audit"
)

func TestRBACManagerBasic(t *testing.T) {
	rbac := NewRBACManager()

	// 测试获取默认角色
	role, ok := rbac.GetRole(RoleMain)
	if !ok {
		t.Fatal("expected main role to exist")
	}
	if role.Name != RoleMain {
		t.Errorf("expected role name main, got %s", role.Name)
	}

	// 测试不存在的角色
	_, ok = rbac.GetRole("nonexistent")
	if ok {
		t.Error("expected nonexistent role to not exist")
	}
}

func TestRBACManagerPermissions(t *testing.T) {
	rbac := NewRBACManager()

	// Main角色应该有admin权限
	if !rbac.HasPermission(RoleMain, ResourceFile, PermissionAdmin) {
		t.Error("main role should have admin file permission")
	}

	// Coder角色应该有write文件权限
	if !rbac.HasPermission(RoleCoder, ResourceFile, PermissionWrite) {
		t.Error("coder role should have write file permission")
	}

	// Reviewer角色不应该有write文件权限
	if rbac.HasPermission(RoleReviewer, ResourceFile, PermissionWrite) {
		t.Error("reviewer role should not have write file permission")
	}

	// Reviewer角色应该有read文件权限
	if !rbac.HasPermission(RoleReviewer, ResourceFile, PermissionRead) {
		t.Error("reviewer role should have read file permission")
	}
}

func TestRBACManagerEffectivePermissions(t *testing.T) {
	rbac := NewRBACManager()

	// 注册一个继承角色
	rbac.RegisterRole(RoleDefinition{
		Name:        "senior_coder",
		Description: "Senior coder with extra permissions",
		Permissions: []Permission{
			{Resource: ResourceConfig, Level: PermissionWrite},
		},
		Inherits: []IsolationAgentRole{RoleCoder},
	})

	perms := rbac.GetEffectivePermissions("senior_coder")
	if len(perms) == 0 {
		t.Fatal("expected permissions for senior_coder")
	}

	// 应该有自己的config权限
	hasConfig := false
	for _, p := range perms {
		if p.Resource == ResourceConfig && p.Level == PermissionWrite {
			hasConfig = true
			break
		}
	}
	if !hasConfig {
		t.Error("senior_coder should have config write permission")
	}

	// 应该继承coder的file权限
	hasFile := false
	for _, p := range perms {
		if p.Resource == ResourceFile {
			hasFile = true
			break
		}
	}
	if !hasFile {
		t.Error("senior_coder should inherit file permission from coder")
	}
}

func TestRBACManagerResourceAccess(t *testing.T) {
	rbac := NewRBACManager()

	// Coder可以访问ts/js文件
	if !rbac.CheckResourceAccess(RoleCoder, ResourceFile, "src/app.ts", "write") {
		t.Error("coder should have write access to .ts files")
	}

	// Main可以访问任何文件
	if !rbac.CheckResourceAccess(RoleMain, ResourceFile, "any/path/file.txt", "write") {
		t.Error("main should have write access to any file")
	}

	// Reviewer只能读取
	if rbac.CheckResourceAccess(RoleReviewer, ResourceFile, "src/app.ts", "write") {
		t.Error("reviewer should not have write access")
	}
	if !rbac.CheckResourceAccess(RoleReviewer, ResourceFile, "src/app.ts", "read") {
		t.Error("reviewer should have read access")
	}
}

func TestIsolationManagerCreateContainer(t *testing.T) {
	mgr := NewIsolationManager(nil)
	ctx := context.Background()

	// 创建容器
	container, err := mgr.CreateContainer(ctx, RoleCoder, "")
	if err != nil {
		t.Fatalf("create container: %v", err)
	}
	if container.Role != RoleCoder {
		t.Errorf("expected role coder, got %s", container.Role)
	}
	if container.ID == "" {
		t.Error("expected container ID")
	}

	// 获取容器
	got, err := mgr.GetContainer(ctx, container.ID)
	if err != nil {
		t.Fatalf("get container: %v", err)
	}
	if got.ID != container.ID {
		t.Errorf("expected ID %s, got %s", container.ID, got.ID)
	}
}

func TestIsolationManagerMaxConcurrent(t *testing.T) {
	mgr := NewIsolationManager(nil)
	ctx := context.Background()

	// Main角色只允许1个并发
	_, err := mgr.CreateContainer(ctx, RoleMain, "")
	if err != nil {
		t.Fatalf("create first main container: %v", err)
	}

	// 第二个应该失败
	_, err = mgr.CreateContainer(ctx, RoleMain, "")
	if err != ErrMaxConcurrent {
		t.Errorf("expected ErrMaxConcurrent, got %v", err)
	}
}

func TestIsolationManagerDestroyContainer(t *testing.T) {
	mgr := NewIsolationManager(nil)
	ctx := context.Background()

	container, _ := mgr.CreateContainer(ctx, RoleCoder, "")

	// 销毁容器
	err := mgr.DestroyContainer(ctx, container.ID)
	if err != nil {
		t.Fatalf("destroy container: %v", err)
	}

	// 验证已销毁
	_, err = mgr.GetContainer(ctx, container.ID)
	if err != ErrContainerNotFound {
		t.Errorf("expected ErrContainerNotFound, got %v", err)
	}
}

func TestIsolationManagerCheckAccess(t *testing.T) {
	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))
	t.Cleanup(func() {
		audit.SetAuditService(nil)
	})

	mgr := NewIsolationManager(nil)
	ctx := context.Background()

	container, _ := mgr.CreateContainer(ctx, RoleCoder, "")

	decision, err := mgr.CheckAccess(ctx, AccessRequest{
		ContainerID:  container.ID,
		Resource:     ResourceFile,
		ResourcePath: "src/app.ts",
		Action:       "write",
		Context: map[string]interface{}{
			"source": "unit-test",
		},
	})
	if err != nil {
		t.Fatalf("check access: %v", err)
	}
	if !decision.Allowed {
		t.Error("coder should have write access to .ts files")
	}
	if decision.AuditID == "" {
		t.Fatal("expected audit id for allowed decision")
	}

	allowedEntry, err := storage.Get(ctx, decision.AuditID)
	if err != nil {
		t.Fatalf("get allowed audit entry: %v", err)
	}
	if allowedEntry.EventType != audit.EventIsolation {
		t.Fatalf("expected isolation event, got %s", allowedEntry.EventType)
	}
	if allowedEntry.Outcome != audit.OutcomeSuccess {
		t.Fatalf("expected success outcome, got %s", allowedEntry.Outcome)
	}
	if allowedEntry.Actor.ID != container.ID {
		t.Fatalf("expected actor id %s, got %s", container.ID, allowedEntry.Actor.ID)
	}
	if allowedEntry.Resource.Path != "src/app.ts" {
		t.Fatalf("expected resource path src/app.ts, got %s", allowedEntry.Resource.Path)
	}

	decision, err = mgr.CheckAccess(ctx, AccessRequest{
		ContainerID: container.ID,
		Resource:    ResourceNetwork,
		Action:      "read",
	})
	if err != nil {
		t.Fatalf("check access: %v", err)
	}
	if decision.Allowed {
		t.Error("coder should not have network access")
	}
	if decision.AuditID == "" {
		t.Fatal("expected audit id for denied decision")
	}

	deniedEntry, err := storage.Get(ctx, decision.AuditID)
	if err != nil {
		t.Fatalf("get denied audit entry: %v", err)
	}
	if deniedEntry.Outcome != audit.OutcomeFailure {
		t.Fatalf("expected failure outcome, got %s", deniedEntry.Outcome)
	}
	if deniedEntry.Details["allowed"] != false {
		t.Fatalf("expected allowed=false in audit details, got %#v", deniedEntry.Details["allowed"])
	}
}

func TestIsolationManagerValidateIO(t *testing.T) {
	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))
	t.Cleanup(func() {
		audit.SetAuditService(nil)
	})

	mgr := NewIsolationManager(nil)
	ctx := context.Background()

	container, _ := mgr.CreateContainer(ctx, RoleCoder, "")

	result, err := mgr.ValidateIO(ctx, container.ID, "Hello, world!", "input")
	if err != nil {
		t.Fatalf("validate IO: %v", err)
	}
	if !result.Valid {
		t.Error("expected valid input")
	}

	validEntry, err := storage.GetLastEntry(ctx)
	if err != nil {
		t.Fatalf("get valid audit entry: %v", err)
	}
	if validEntry.EventType != audit.EventIsolation {
		t.Fatalf("expected isolation event, got %s", validEntry.EventType)
	}
	if validEntry.Action != "validate_io" {
		t.Fatalf("expected validate_io action, got %s", validEntry.Action)
	}
	if validEntry.Details["direction"] != "input" {
		t.Fatalf("expected input direction, got %#v", validEntry.Details["direction"])
	}

	result, _ = mgr.ValidateIO(ctx, container.ID, "test; rm -rf /", "input")
	if result.Valid {
		t.Error("expected invalid input for injection attack")
	}
	if len(result.BlockedPatterns) == 0 {
		t.Error("expected blocked patterns")
	}

	blockedEntry, err := storage.GetLastEntry(ctx)
	if err != nil {
		t.Fatalf("get blocked audit entry: %v", err)
	}
	if blockedEntry.Outcome != audit.OutcomeFailure {
		t.Fatalf("expected failure outcome, got %s", blockedEntry.Outcome)
	}
	if blockedEntry.Details["blocked_pattern_count"] != len(result.BlockedPatterns) {
		t.Fatalf("expected blocked_pattern_count %d, got %#v", len(result.BlockedPatterns), blockedEntry.Details["blocked_pattern_count"])
	}

	result, _ = mgr.ValidateIO(ctx, container.ID, "api_key: sk-12345secret", "input")
	if len(result.Warnings) == 0 {
		t.Error("expected warnings for sensitive data")
	}
	if result.SanitizedInput == "api_key: sk-12345secret" {
		t.Error("expected sensitive data to be redacted")
	}

	redactedEntry, err := storage.GetLastEntry(ctx)
	if err != nil {
		t.Fatalf("get redacted audit entry: %v", err)
	}
	if redactedEntry.Details["contains_sensitive_warning"] != true {
		t.Fatalf("expected contains_sensitive_warning=true, got %#v", redactedEntry.Details["contains_sensitive_warning"])
	}
	if redactedEntry.Details["sanitized_changed"] != true {
		t.Fatalf("expected sanitized_changed=true, got %#v", redactedEntry.Details["sanitized_changed"])
	}
	if _, ok := redactedEntry.Details["input_preview"]; ok {
		t.Fatal("audit details should not contain raw input preview")
	}
}

func TestIsolationManagerGetContainersByRole(t *testing.T) {
	mgr := NewIsolationManager(nil)
	ctx := context.Background()

	// 创建多个容器
	mgr.CreateContainer(ctx, RoleCoder, "")
	mgr.CreateContainer(ctx, RoleCoder, "")
	mgr.CreateContainer(ctx, RoleReviewer, "")

	// 按角色获取
	coders, err := mgr.GetContainersByRole(ctx, RoleCoder)
	if err != nil {
		t.Fatalf("get containers by role: %v", err)
	}
	if len(coders) != 2 {
		t.Errorf("expected 2 coder containers, got %d", len(coders))
	}

	reviewers, _ := mgr.GetContainersByRole(ctx, RoleReviewer)
	if len(reviewers) != 1 {
		t.Errorf("expected 1 reviewer container, got %d", len(reviewers))
	}
}

func TestIsolationManagerSetResourceQuota(t *testing.T) {
	mgr := NewIsolationManager(nil)
	ctx := context.Background()

	container, _ := mgr.CreateContainer(ctx, RoleCoder, "")

	quota := ResourceQuota{
		MaxMemoryMB:   512,
		MaxFileSizeMB: 10,
		MaxFileCount:  100,
	}

	err := mgr.SetResourceQuota(ctx, container.ID, quota)
	if err != nil {
		t.Fatalf("set quota: %v", err)
	}

	got, _ := mgr.GetContainer(ctx, container.ID)
	if got.ResourceQuota == nil {
		t.Fatal("expected resource quota")
	}
	if got.ResourceQuota.MaxMemoryMB != 512 {
		t.Errorf("expected max memory 512, got %d", got.ResourceQuota.MaxMemoryMB)
	}
}

func TestIsolationManagerInvalidRole(t *testing.T) {
	mgr := NewIsolationManager(nil)
	ctx := context.Background()

	_, err := mgr.CreateContainer(ctx, "invalid_role", "")
	if err != ErrRoleNotFound {
		t.Errorf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	mgr := NewIsolationManager(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.CreateContainer(ctx, RoleCoder, "")
	if err == nil {
		t.Error("expected error on cancelled context")
	}

	_, err = mgr.CheckAccess(ctx, AccessRequest{})
	if err == nil {
		t.Error("expected error on cancelled context")
	}

	_, err = mgr.ValidateIO(ctx, "test", "input", "input")
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}

func TestPermissionPriority(t *testing.T) {
	if PermissionPriority[PermissionNone] != 0 {
		t.Error("none should have priority 0")
	}
	if PermissionPriority[PermissionRead] != 1 {
		t.Error("read should have priority 1")
	}
	if PermissionPriority[PermissionWrite] != 2 {
		t.Error("write should have priority 2")
	}
	if PermissionPriority[PermissionExecute] != 3 {
		t.Error("execute should have priority 3")
	}
	if PermissionPriority[PermissionAdmin] != 4 {
		t.Error("admin should have priority 4")
	}
}

func TestDefaultRoles(t *testing.T) {
	roles := []IsolationAgentRole{RoleMain, RoleCoder, RoleReviewer, RolePlanner, RoleResearcher, RoleExecutor}

	for _, role := range roles {
		def, ok := DefaultRoles[role]
		if !ok {
			t.Errorf("expected default role %s to exist", role)
			continue
		}
		if len(def.Permissions) == 0 {
			t.Errorf("role %s should have at least one permission", role)
		}
	}
}

func TestPatternMatching(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"**/*.ts", "src/app.ts", true},
		{"**/*.ts", "src/deep/nested/file.ts", true},
		{"**/*.ts", "src/app.js", false},
		{"**/*.{ts,js,go}", "src/main.go", true},
		{"**/*.{ts,js,go}", "src/main.py", false},
		{"src/**/*.ts", "src/app.ts", true},
		{"src/**/*.ts", "other/app.ts", false},
		{"*.ts", "app.ts", true},
		{"*.ts", "src/app.ts", false},
	}

	for _, tt := range tests {
		got := matchPattern(tt.pattern, tt.path)
		if got != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestRegisterCustomRole(t *testing.T) {
	rbac := NewRBACManager()

	customRole := RoleDefinition{
		Name:        "custom_role",
		Description: "Custom test role",
		Permissions: []Permission{
			{Resource: ResourceFile, Level: PermissionRead},
			{Resource: ResourceMemory, Level: PermissionWrite},
		},
		MaxConcurrent: 10,
	}

	err := rbac.RegisterRole(customRole)
	if err != nil {
		t.Fatalf("register role: %v", err)
	}

	got, ok := rbac.GetRole("custom_role")
	if !ok {
		t.Fatal("expected custom role to exist")
	}
	if got.Description != "Custom test role" {
		t.Errorf("expected description 'Custom test role', got %s", got.Description)
	}
	if len(got.Permissions) != 2 {
		t.Errorf("expected 2 permissions, got %d", len(got.Permissions))
	}
}
