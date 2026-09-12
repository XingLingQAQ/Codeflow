package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CanonicalizeWorkspaceRoot resolves a workspace root to the path the server
// will use for all subsequent operations. It fails closed for missing roots,
// files, and roots outside the configured allow-list.
func CanonicalizeWorkspaceRoot(root string, allowedRoots []string) (string, error) {
	return canonicalizeWorkspaceRoot(root, allowedRoots, false)
}

func canonicalizeWorkspaceRoot(root string, allowedRoots []string, allowUnrestricted bool) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory: %s", root)
	}
	abs = filepath.Clean(abs)
	for _, allowed := range allowedRoots {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(allowedAbs); err == nil {
			allowedAbs = resolved
		}
		allowedAbs = filepath.Clean(allowedAbs)
		if abs == allowedAbs || strings.HasPrefix(abs, allowedAbs+string(filepath.Separator)) {
			return abs, nil
		}
	}
	if allowUnrestricted {
		return abs, nil
	}
	if len(allowedRoots) == 0 {
		return "", fmt.Errorf("workspace root binding is disabled until CODEFLOW_WORKSPACE_ROOTS is configured")
	}
	return "", fmt.Errorf("workspace root is outside allowed roots: %s", root)
}

// BindWorkspaceRoot canonicalizes and persists the root on a project.
func (s *InMemoryProjectService) bindWorkspaceRoot(id, root string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, fmt.Errorf("project not found")
	}
	canonical, err := canonicalizeWorkspaceRoot(root, s.allowedRoots, s.allowUnrestrictedRoots)
	if err != nil {
		return nil, err
	}
	p.WorkspaceRoot = canonical
	p.BindingState = BindingStateBound
	p.UpdatedAt = nowUnix()
	p.LastActive = p.UpdatedAt
	return cloneProject(p), nil
}

func (s *InMemoryProjectService) BindWorkspaceRoot(ctx context.Context, id, root string) (*Project, error) {
	_ = ctx
	return s.bindWorkspaceRoot(id, root)
}

func nowUnix() int64 { return time.Now().Unix() }
