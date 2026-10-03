package main

import (
	"os"
	"strings"
)

// workspaceRootConfig is the single read of the two workspace-root environment
// variables (T1.10.d). The same value is handed to the project binding service
// and to the workspace file service, so both sides apply one rule: a configured
// CODEFLOW_WORKSPACE_ROOTS is honored in full (the migration switch cannot
// widen it), and while nothing is configured every root is refused unless the
// explicit temporary desktop migration switch is set.
type workspaceRootConfig struct {
	// AllowedRoots is the parsed CODEFLOW_WORKSPACE_ROOTS list (nil when empty).
	AllowedRoots []string
	// AllowUnrestricted is CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING=1. It
	// is only passed on to the services when AllowedRoots is empty.
	AllowUnrestricted bool
	// Warnings are the startup messages for the operator, one per line.
	Warnings []string
}

// loadWorkspaceRootConfig reads the two environment variables once. An empty
// CODEFLOW_WORKSPACE_ROOTS leaves AllowedRoots nil and the services fail closed
// unless the switch is set. The switch is ignored — with a warning — whenever
// an allow-list is configured, because a configured allow-list must never be
// widened by the temporary desktop migration switch.
func loadWorkspaceRootConfig() workspaceRootConfig {
	roots := parseAllowedWorkspaceRoots()
	switchOn := strings.TrimSpace(os.Getenv("CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING")) == "1"

	cfg := workspaceRootConfig{AllowedRoots: roots}
	if len(roots) > 0 {
		if switchOn {
			cfg.Warnings = append(cfg.Warnings,
				"CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING=1 is ignored while CODEFLOW_WORKSPACE_ROOTS is configured; only the configured roots are allowed")
		}
		return cfg
	}
	if switchOn {
		cfg.AllowUnrestricted = true
		cfg.Warnings = append(cfg.Warnings,
			"CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING=1 is a temporary desktop migration switch: no workspace roots are configured, so any existing directory may be bound and used until it is removed")
	}
	return cfg
}

// projectRootPolicy is implemented by the project binding services (SQLite and
// in-memory); it is what applyWorkspaceRootPolicy configures.
type projectRootPolicy interface {
	SetAllowedWorkspaceRoots([]string)
	SetAllowUnrestrictedWorkspaceRoots(bool)
}

// fileRootPolicy is implemented by workspace.FSService.
type fileRootPolicy interface {
	SetAllowedRoots([]string)
	SetAllowUnrestrictedRoots(bool)
}

// applyWorkspaceRootPolicy pushes the same configuration to the project binding
// service and the workspace file service; main.go uses it so the wiring here is
// exactly what the unit test covers. It never widens a configured allow-list
// with the switch.
func applyWorkspaceRootPolicy(cfg workspaceRootConfig, projectSvc projectRootPolicy, wsSvc fileRootPolicy) {
	if projectSvc != nil {
		projectSvc.SetAllowedWorkspaceRoots(cfg.AllowedRoots)
		projectSvc.SetAllowUnrestrictedWorkspaceRoots(cfg.AllowUnrestricted)
	}
	if wsSvc != nil {
		wsSvc.SetAllowedRoots(cfg.AllowedRoots)
		wsSvc.SetAllowUnrestrictedRoots(cfg.AllowUnrestricted)
	}
}
