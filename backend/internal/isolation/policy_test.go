package isolation

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/policy"
)

func TestAccessDecisionIncludesSharedPolicyIdentity(t *testing.T) {
	policy.SetEvaluator(policy.NewLocalEvaluator())
	t.Cleanup(func() { policy.SetEvaluator(nil) })
	mgr := NewIsolationManager(nil)
	container, err := mgr.CreateContainer(context.Background(), RoleMain, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := audit.ContextWithTrace(context.Background(), &audit.AuditTrace{ProjectID: "project-1", AgentID: "agent-1"})
	decision, err := mgr.CheckAccess(ctx, AccessRequest{
		ContainerID: container.ID, Resource: ResourceFile, ResourcePath: "src/main.go", Action: "write",
		Context: map[string]interface{}{"plugin_id": "plugin-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.RuleVersion != policy.RuleVersion || decision.ProjectID != "project-1" ||
		decision.AgentID != "agent-1" || decision.PluginID != "plugin-1" || decision.Operation != policy.OperationWorkspaceWrite {
		t.Fatalf("decision lacks policy identity: %+v", decision)
	}
}
