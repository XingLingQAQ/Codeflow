package project

// T1.10.d: the binding side and the file service share one root rule. A
// configured allow-list is always honored in full — the temporary migration
// switch cannot widen it — while an empty allow-list allows arbitrary existing
// directories only when the switch is explicitly on. Unconfigured stay
// refused without the switch.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/workspace"
)

// The migration switch must not let a root outside a configured allow-list
// through canonicalization. Before T1.10.d the switch branch ran after the
// allow-list loop and accepted anything.
func TestCanonicalizeWorkspaceRootSwitchNeverBypassesAllowList(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()

	if _, err := canonicalizeWorkspaceRoot(outside, []string{allowed}, true); err == nil {
		t.Fatal("the migration switch must not accept a root outside a configured allow-list")
	} else if !strings.Contains(err.Error(), "outside allowed roots") {
		t.Fatalf("outside-root error = %v, want the existing outside-allowed-roots message", err)
	}

	// The same root without the switch still refuses, and an allow-listed root
	// is accepted whether or not the switch is on.
	if _, err := canonicalizeWorkspaceRoot(outside, []string{allowed}, false); err == nil {
		t.Fatal("a root outside the allow-list must refuse")
	}
	if _, err := canonicalizeWorkspaceRoot(allowed, []string{allowed}, true); err != nil {
		t.Fatalf("an allow-listed root must be accepted with the switch on: %v", err)
	}
}

func TestCanonicalizeWorkspaceRootUnconfigured(t *testing.T) {
	root := t.TempDir()

	// No allow-list, switch off: refused, and classed as root-not-allowed.
	_, err := canonicalizeWorkspaceRoot(root, nil, false)
	if err == nil {
		t.Fatal("unconfigured without the migration switch must refuse")
	}
	if !errors.Is(err, workspace.ErrRootNotAllowed) {
		t.Fatalf("unconfigured error = %v, want errors.Is(err, workspace.ErrRootNotAllowed)", err)
	}
	if !strings.Contains(err.Error(), "CODEFLOW_WORKSPACE_ROOTS") {
		t.Fatalf("unconfigured error = %v, want the CODEFLOW_WORKSPACE_ROOTS repair hint", err)
	}

	// No allow-list, switch on: any existing directory is accepted.
	got, err := canonicalizeWorkspaceRoot(root, nil, true)
	if err != nil {
		t.Fatalf("unconfigured with the migration switch on must accept an existing directory: %v", err)
	}
	if got == "" {
		t.Fatal("canonical root must not be empty")
	}
}

func TestInMemoryProjectServiceSwitchNeverBypassesAllowList(t *testing.T) {
	ctx := context.Background()
	allowed := t.TempDir()
	outside := t.TempDir()

	svc := NewInMemoryProjectService()
	svc.SetAllowedWorkspaceRoots([]string{allowed})
	svc.SetAllowUnrestrictedWorkspaceRoots(true)

	if _, err := svc.CreateProject(ctx, &ProjectCreateRequest{Title: "outside", WorkspaceRoot: outside}); err == nil {
		t.Fatal("CreateProject outside the allow-list with the switch on must be refused")
	} else if !strings.Contains(err.Error(), "outside allowed roots") {
		t.Fatalf("CreateProject error = %v, want the outside-allowed-roots rejection", err)
	}

	project, err := svc.CreateProject(ctx, &ProjectCreateRequest{Title: "unbound"})
	if err != nil {
		t.Fatalf("CreateProject without a root: %v", err)
	}
	if _, err := svc.BindWorkspaceRoot(ctx, project.ID, outside); err == nil {
		t.Fatal("BindWorkspaceRoot outside the allow-list with the switch on must be refused")
	} else if !strings.Contains(err.Error(), "outside allowed roots") {
		t.Fatalf("BindWorkspaceRoot error = %v, want the outside-allowed-roots rejection", err)
	}

	// The allow-listed root still binds.
	if _, err := svc.CreateProject(ctx, &ProjectCreateRequest{Title: "inside", WorkspaceRoot: allowed}); err != nil {
		t.Fatalf("CreateProject inside the allow-list must succeed: %v", err)
	}
}

// The SQLite service captures a binding snapshot for every accepted root. Its
// unrestricted branch must obey the same rule: a configured allow-list wins
// over the switch, so the capture itself is refused.
func TestSQLiteCaptureBindingSnapshotSwitchNeverBypassesAllowList(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := NewSQLiteProjectService(filepath.Join(dir, "project.db"))
	if err != nil {
		t.Fatalf("NewSQLiteProjectService: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	allowed := mkdirAll(t, filepath.Join(dir, "allowed"))
	outside := mkdirAll(t, filepath.Join(dir, "outside"))
	svc.SetAllowedWorkspaceRoots([]string{allowed})
	svc.SetAllowUnrestrictedWorkspaceRoots(true)

	if _, err := svc.CreateProject(ctx, &ProjectCreateRequest{Title: "outside", WorkspaceRoot: outside}); err == nil {
		t.Fatal("SQLite snapshot capture outside the allow-list with the switch on must be refused")
	}
	if _, err := svc.CreateProject(ctx, &ProjectCreateRequest{Title: "inside", WorkspaceRoot: allowed}); err != nil {
		t.Fatalf("SQLite snapshot capture inside the allow-list must succeed: %v", err)
	}
}

// Without an allow-list the switch is what makes binding possible at all: off
// refuses, on accepts an existing directory (both through the SQLite service,
// so the snapshot path is covered).
func TestSQLiteCaptureBindingSnapshotUnconfiguredSwitch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	root := mkdirAll(t, filepath.Join(dir, "root"))
	dbPath := filepath.Join(dir, "project.db")

	off, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteProjectService: %v", err)
	}
	if _, err := off.CreateProject(ctx, &ProjectCreateRequest{Title: "off", WorkspaceRoot: root}); err == nil {
		t.Fatal("unconfigured without the switch must refuse the binding")
	}
	_ = off.Close()

	on, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteProjectService: %v", err)
	}
	t.Cleanup(func() { _ = on.Close() })
	on.SetAllowUnrestrictedWorkspaceRoots(true)
	if _, err := on.CreateProject(ctx, &ProjectCreateRequest{Title: "on", WorkspaceRoot: root}); err != nil {
		t.Fatalf("unconfigured with the switch on must accept an existing directory: %v", err)
	}
}
