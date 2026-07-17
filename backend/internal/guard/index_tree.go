package guard

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IndexTree walks root and commits parseable source files into the symbol index.
// Non-code files are skipped. Errors on individual files are ignored.
func (e *Engine) IndexTree(ctx context.Context, root string) (int, error) {
	if e == nil {
		return 0, nil
	}
	e.mu.RLock()
	idx := e.symbols
	e.mu.RUnlock()
	if idx == nil {
		return 0, nil
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, nil
	}

	count := 0
	seen := make(map[string]struct{})
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "node_modules" || name == ".git" || name == "dist" || name == "vendor" || name == ".archive" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSourceExt(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		clean := filepath.Clean(path)
		idx.Commit(ctx, clean, data)
		seen[clean] = struct{}{}
		count++
		return nil
	})
	if err != nil {
		return count, err
	}
	// Drop symbols for source files under root that no longer exist on disk.
	rootPrefix := root
	if !strings.HasSuffix(rootPrefix, string(filepath.Separator)) {
		rootPrefix += string(filepath.Separator)
	}
	for _, p := range idx.Paths() {
		if p != root && !strings.HasPrefix(p, rootPrefix) {
			continue
		}
		if !isSourceExt(p) {
			continue
		}
		if _, ok := seen[filepath.Clean(p)]; ok {
			continue
		}
		idx.Remove(p)
	}
	return count, nil
}

func isSourceExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java":
		return true
	default:
		return false
	}
}
