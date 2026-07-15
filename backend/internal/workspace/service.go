package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	s.mu.RLock()
	guard := s.guard
	s.mu.RUnlock()
	if guard != nil {
		if err := guard.BeforeWrite(ctx, finalAbs, req.Content); err != nil {
			return nil, fmt.Errorf("write blocked by guard: %w", err)
		}
	}

	if req.CreateParents || mode == WriteModeStage {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir: %w", err)
		}
	}
	if err := os.WriteFile(abs, req.Content, 0o644); err != nil {
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
	return s.Write(ctx, &WriteRequest{
		Root:          root,
		Path:          rel,
		Content:       data,
		CreateParents: true,
		Mode:          WriteModeDirect,
	})
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
