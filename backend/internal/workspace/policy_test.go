package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/policy"
)

func setupDenyPolicy(t *testing.T) *audit.AuditService {
	t.Helper()
	auditSvc := audit.NewAuditService(audit.NewMemoryStorage())
	audit.SetAuditService(auditSvc)
	policy.SetEvaluator(policy.NewFailClosedEvaluator())
	t.Cleanup(func() {
		policy.SetEvaluator(nil)
		audit.SetAuditService(nil)
	})
	return auditSvc
}

func TestDirectWorkspaceWriteDeniedAndAudited(t *testing.T) {
	auditSvc := setupDenyPolicy(t)
	root := t.TempDir()
	target := filepath.Join(root, "denied.txt")

	_, err := NewFSService(nil).Write(context.Background(), &WriteRequest{
		Root: root, Path: "denied.txt", Content: []byte("blocked"), ProjectID: "project-1", AgentID: "agent-1",
	})
	if err == nil {
		t.Fatal("direct service write bypassed policy")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("denied write mutated filesystem: %v", statErr)
	}
	result, queryErr := auditSvc.Query(context.Background(), &audit.AuditQuery{ResourceType: policy.OperationWorkspaceWrite})
	if queryErr != nil || result.Total != 1 || result.Entries[0].Details["project_id"] != "project-1" {
		t.Fatalf("missing workspace denial audit: result=%+v err=%v", result, queryErr)
	}
}

func TestDirectDevServerStartDeniedBeforeProcessSpawn(t *testing.T) {
	setupDenyPolicy(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"dev":"echo should-not-run"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := audit.ContextWithTrace(context.Background(), &audit.AuditTrace{ProjectID: "project-1", AgentID: "agent-1"})
	mgr := NewDevServerManager(NewFSService(nil))
	if _, err := mgr.StartContext(ctx, root, "dev"); err == nil {
		t.Fatal("direct process start bypassed policy")
	}
	if len(mgr.List()) != 0 {
		t.Fatal("denied process was registered")
	}
}
