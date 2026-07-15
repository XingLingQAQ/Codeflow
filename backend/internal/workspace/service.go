package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	backendhooks "github.com/codeflow/backend/internal/hooks"
)

// FSService is a real filesystem workspace rooted per-call.
type FSService struct {
	mu    sync.RWMutex
	guard WriteGuard
}

// NewFSService creates a workspace service. guard may be nil.
func NewFSService(guard WriteGuard) *FSService {
	return &FSService{guard: guard}
}

// SetGuard replaces the write guard (bootstrap / tests).
func (s *FSService) SetGuard(g WriteGuard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guard = g
}

// Resolve joins root+rel and ensures the result is inside root.
func (s *FSService) Resolve(root, rel string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return "", fmt.Errorf("workspace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory: %s", absRoot)
	}

	rel = strings.TrimSpace(rel)
	rel = strings.ReplaceAll(rel, "\\", "/")
	if rel == "" || rel == "." {
		return absRoot, nil
	}
	// Reject absolute and parent escapes early
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("path must be relative to project root")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project root: %s", rel)
	}

	joined := filepath.Join(absRoot, clean)
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	// Ensure absJoined is under absRoot (with separator boundary)
	rootPrefix := absRoot
	if !strings.HasSuffix(rootPrefix, string(filepath.Separator)) {
		rootPrefix += string(filepath.Separator)
	}
	if absJoined != absRoot && !strings.HasPrefix(absJoined, rootPrefix) {
		return "", fmt.Errorf("path escapes project root: %s", rel)
	}
	return absJoined, nil
}

// List lists a directory under root.
func (s *FSService) List(ctx context.Context, req *ListRequest) ([]Entry, error) {
	if req == nil {
		return nil, fmt.Errorf("list request is required")
	}
	abs, err := s.Resolve(req.Root, req.Path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	baseRel := normalizeRel(req.Path)
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		rel := name
		if baseRel != "" {
			rel = baseRel + "/" + name
		}
		out = append(out, Entry{
			Name:    name,
			Path:    rel,
			IsDir:   e.IsDir(),
			Size:    sizeOf(info),
			ModTime: info.ModTime().UTC(),
		})
	}
	return out, nil
}

// Read reads a file under root.
func (s *FSService) Read(ctx context.Context, req *ReadRequest) (*FileContent, error) {
	if req == nil || strings.TrimSpace(req.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	abs, err := s.Resolve(req.Root, req.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory: %s", req.Path)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return &FileContent{
		Path:    normalizeRel(req.Path),
		Content: data,
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
	}, nil
}

// Write writes a file under root after optional guard approval.
// Mode stage writes under .codeflow/staging/<rel> (still path-sandboxed via Resolve on staging root).
func (s *FSService) Write(ctx context.Context, req *WriteRequest) (*Entry, error) {
	if req == nil || strings.TrimSpace(req.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Always validate rel path against project root first.
	if _, err := s.Resolve(req.Root, req.Path); err != nil {
		return nil, err
	}

	targetRoot := req.Root
	rel := normalizeRel(req.Path)
	mode := req.Mode
	if mode == "" {
		mode = WriteModeDirect
	}
	if mode == WriteModeStage {
		targetRoot = filepath.Join(req.Root, ".codeflow", "staging")
		if err := os.MkdirAll(targetRoot, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir staging: %w", err)
		}
	}
	abs, err := s.Resolve(targetRoot, rel)
	if err != nil {
		return nil, err
	}

	// Guard always evaluates against the *intended* final path (project tree), not staging path.
	finalAbs, err := s.Resolve(req.Root, rel)
	if err != nil {
		return nil, err
	}
	content := req.Content
	if backendhooks.HasHookManager() {
		payload := map[string]interface{}{
			"path":    finalAbs,
			"rel":     rel,
			"content": content,
			"mode":    string(mode),
		}
		result, herr := backendhooks.GetHookManager().Trigger(ctx, backendhooks.HookBeforeWrite, payload)
		if herr != nil {
			log.Printf("[WARN] workspace before-write hook failed: %v", herr)
		} else if m, ok := result.(map[string]interface{}); ok {
			if c, ok := m["content"].([]byte); ok {
				content = c
			} else if c, ok := m["content"].(string); ok {
				content = []byte(c)
			}
		}
	}
	s.mu.RLock()
	guard := s.guard
	s.mu.RUnlock()
	if guard != nil {
		if err := guard.BeforeWrite(ctx, finalAbs, content); err != nil {
			return nil, fmt.Errorf("write blocked by guard: %w", err)
		}
	}

	if req.CreateParents || mode == WriteModeStage {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir: %w", err)
		}
	}
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	return &Entry{
		Name:    filepath.Base(abs),
		Path:    rel,
		IsDir:   false,
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
	}, nil
}

// Promote copies a staged file into the real project tree (guard re-checked).
func (s *FSService) Promote(ctx context.Context, root, rel string) (*Entry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rel = normalizeRel(rel)
	stagingRoot := filepath.Join(root, ".codeflow", "staging")
	stagedAbs, err := s.Resolve(stagingRoot, rel)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(stagedAbs)
	if err != nil {
		return nil, fmt.Errorf("read staged: %w", err)
	}
	ent, err := s.Write(ctx, &WriteRequest{
		Root:          root,
		Path:          rel,
		Content:       data,
		CreateParents: true,
		Mode:          WriteModeDirect,
	})
	if err != nil {
		return nil, err
	}
	// Best-effort cleanup of the shadow copy after successful promote.
	_ = os.Remove(stagedAbs)
	return ent, nil
}

// PromoteAll promotes every staged file (best-effort continues after individual failures).
func (s *FSService) PromoteAll(ctx context.Context, root string) ([]Entry, error) {
	staged, err := s.ListStaged(ctx, root)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(staged))
	var firstErr error
	for _, e := range staged {
		ent, err := s.Promote(ctx, root, e.Path)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("promote %s: %w", e.Path, err)
			}
			continue
		}
		out = append(out, *ent)
	}
	return out, firstErr
}

// DiscardStaged removes a file from .codeflow/staging (does not touch the real tree).
func (s *FSService) DiscardStaged(ctx context.Context, root, rel string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_ = ctx
	rel = normalizeRel(rel)
	if rel == "" {
		return fmt.Errorf("path is required")
	}
	stagingRoot := filepath.Join(root, ".codeflow", "staging")
	stagedAbs, err := s.Resolve(stagingRoot, rel)
	if err != nil {
		return err
	}
	if err := os.Remove(stagedAbs); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("staged file not found: %s", rel)
		}
		return fmt.Errorf("discard staged: %w", err)
	}
	return nil
}

// ListStaged walks .codeflow/staging and returns file entries with project-relative paths.
// Missing staging dir yields an empty list (not an error).
func (s *FSService) ListStaged(ctx context.Context, root string) ([]Entry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	_ = ctx
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("workspace root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	stagingRoot := filepath.Join(absRoot, ".codeflow", "staging")
	info, err := os.Stat(stagingRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("staging path is not a directory")
	}
	out := make([]Entry, 0)
	err = filepath.WalkDir(stagingRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(stagingRoot, path)
		if err != nil {
			return err
		}
		rel = normalizeRel(rel)
		fi, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, Entry{
			Name:    d.Name(),
			Path:    rel,
			IsDir:   false,
			Size:    fi.Size(),
			ModTime: fi.ModTime().UTC(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Stat returns metadata for a path under root.
func (s *FSService) Stat(ctx context.Context, root, rel string) (*Entry, error) {
	abs, err := s.Resolve(root, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	return &Entry{
		Name:    info.Name(),
		Path:    normalizeRel(rel),
		IsDir:   info.IsDir(),
		Size:    sizeOf(info),
		ModTime: info.ModTime().UTC(),
	}, nil
}

func normalizeRel(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.Trim(p, "/")
	if p == "." {
		return ""
	}
	return p
}

func sizeOf(info fs.FileInfo) int64 {
	if info.IsDir() {
		return 0
	}
	return info.Size()
}

// --- process-wide default ---

var (
	defaultSvc Service
	svcMu      sync.RWMutex
)

// GetService returns the global workspace service.
func GetService() Service {
	svcMu.RLock()
	s := defaultSvc
	svcMu.RUnlock()
	if s != nil {
		return s
	}
	svcMu.Lock()
	defer svcMu.Unlock()
	if defaultSvc == nil {
		defaultSvc = NewFSService(nil)
	}
	return defaultSvc
}

// SetService sets the global workspace service (bootstrap / tests).
func SetService(s Service) {
	svcMu.Lock()
	defer svcMu.Unlock()
	defaultSvc = s
}
