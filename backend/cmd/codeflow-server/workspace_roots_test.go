package main

// T1.10.d: the two workspace-root environment variables are read once and the
// same values go to the project binding service and the workspace file service.
// The four combinations are pinned here, including the two warnings: the
// temporary-migration warning when the switch alone decides, and the
// "switch ignored" warning when an allow-list is configured.

import (
	"context"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/workspace"
)

func TestLoadWorkspaceRootConfigCombinations(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	cases := []struct {
		name          string
		roots         string
		switchOn      string
		wantRoots     []string
		wantUnrestr   bool
		wantWarnings  int
		warningSubstr []string
	}{
		{
			name:        "unconfigured switch off",
			roots:       "",
			switchOn:    "",
			wantRoots:   nil,
			wantUnrestr: false,
			// No roots and no switch is the plain fail-closed default; it is
			// the documented state and stays quiet (readiness reports it).
			wantWarnings: 0,
		},
		{
			name:          "unconfigured switch on",
			roots:         "",
			switchOn:      "1",
			wantRoots:     nil,
			wantUnrestr:   true,
			wantWarnings:  1,
			warningSubstr: []string{"temporary desktop migration switch"},
		},
		{
			name:         "configured switch off",
			roots:        rootA + "," + rootB,
			switchOn:     "",
			wantRoots:    []string{rootA, rootB},
			wantUnrestr:  false,
			wantWarnings: 0,
		},
		{
			name:          "configured switch on",
			roots:         rootA,
			switchOn:      "1",
			wantRoots:     []string{rootA},
			wantUnrestr:   false, // the switch must not widen a configured allow-list
			wantWarnings:  1,
			warningSubstr: []string{"ignored"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEFLOW_WORKSPACE_ROOTS", tc.roots)
			t.Setenv("CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING", tc.switchOn)

			cfg := loadWorkspaceRootConfig()
			if len(cfg.AllowedRoots) != len(tc.wantRoots) {
				t.Fatalf("AllowedRoots = %v, want %v", cfg.AllowedRoots, tc.wantRoots)
			}
			for i := range tc.wantRoots {
				if cfg.AllowedRoots[i] != tc.wantRoots[i] {
					t.Fatalf("AllowedRoots[%d] = %q, want %q", i, cfg.AllowedRoots[i], tc.wantRoots[i])
				}
			}
			if cfg.AllowUnrestricted != tc.wantUnrestr {
				t.Fatalf("AllowUnrestricted = %v, want %v", cfg.AllowUnrestricted, tc.wantUnrestr)
			}
			if len(cfg.Warnings) != tc.wantWarnings {
				t.Fatalf("Warnings = %v, want %d entries", cfg.Warnings, tc.wantWarnings)
			}
			for _, sub := range tc.warningSubstr {
				found := false
				for _, w := range cfg.Warnings {
					if strings.Contains(w, sub) {
						found = true
					}
				}
				if !found {
					t.Fatalf("warning %v does not contain %q", cfg.Warnings, sub)
				}
			}
		})
	}
}

// The same configuration reaches both services through
// applyWorkspaceRootPolicy, and the switch never widens a configured list.
func TestApplyWorkspaceRootPolicyToBothServices(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	t.Run("configured list with switch on is not widened", func(t *testing.T) {
		cfg := workspaceRootConfig{AllowedRoots: []string{rootA}, AllowUnrestricted: false}
		projectSvc := project.NewInMemoryProjectService()
		fsSvc := workspace.NewFSService(nil)

		applyWorkspaceRootPolicy(cfg, projectSvc, fsSvc)

		// The service stores final paths, so compare against the resolved one.
		finalA, err := workspace.FinalPath(rootA)
		if err != nil {
			t.Fatalf("FinalPath(%s): %v", rootA, err)
		}
		if got := fsSvc.AllowedRoots(); len(got) != 1 || got[0] != finalA {
			t.Fatalf("file service roots = %v, want [%s]", got, finalA)
		}
		// Both services refuse rootB, so neither switch nor list leaked it.
		if _, err := fsSvc.Resolve(rootB, ""); err == nil {
			t.Fatal("file service accepted a root outside the configured list")
		}
		if _, err := projectSvc.CreateProject(context.Background(), &project.ProjectCreateRequest{Title: "x", WorkspaceRoot: rootB}); err == nil {
			t.Fatal("project service accepted a root outside the configured list")
		}
	})

	t.Run("unconfigured switch on reaches both services", func(t *testing.T) {
		cfg := workspaceRootConfig{AllowUnrestricted: true}
		projectSvc := project.NewInMemoryProjectService()
		fsSvc := workspace.NewFSService(nil)

		applyWorkspaceRootPolicy(cfg, projectSvc, fsSvc)

		if _, err := fsSvc.Resolve(rootB, ""); err != nil {
			t.Fatalf("file service with the migration switch on: %v", err)
		}
		if _, err := projectSvc.CreateProject(context.Background(), &project.ProjectCreateRequest{Title: "x", WorkspaceRoot: rootB}); err != nil {
			t.Fatalf("project service with the migration switch on: %v", err)
		}
	})
}
