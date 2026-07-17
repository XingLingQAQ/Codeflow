package guard

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/codeflow/backend/internal/ast"
)

// SymbolLoc is one definition site in the project index.
type SymbolLoc struct {
	Path   string
	Line   int
	Kind   string
	Name   string
	SigKey string // kind:name (signature-lite)
}

// SymbolIndex tracks top-level symbols for duplicate detection across files.
type SymbolIndex struct {
	mu     sync.RWMutex
	parser *ast.SimpleASTParser
	// path -> symbols defined in that file
	byPath map[string][]SymbolLoc
	// sigKey -> locations (may include multiple files)
	byKey map[string][]SymbolLoc
}

// NewSymbolIndex creates an empty index.
func NewSymbolIndex() *SymbolIndex {
	return &SymbolIndex{
		parser: ast.NewSimpleASTParser(nil),
		byPath: make(map[string][]SymbolLoc),
		byKey:  make(map[string][]SymbolLoc),
	}
}

// CheckDuplicates parses content for absPath and returns conflicts with other files.
// Does not mutate the index.
func (idx *SymbolIndex) CheckDuplicates(ctx context.Context, absPath string, content []byte) []SymbolLoc {
	if idx == nil {
		return nil
	}
	incoming := idx.extract(ctx, absPath, content)
	if len(incoming) == 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	absPath = filepath.Clean(absPath)
	conflicts := make([]SymbolLoc, 0)
	seen := make(map[string]bool)
	for _, sym := range incoming {
		for _, existing := range idx.byKey[sym.SigKey] {
			if filepath.Clean(existing.Path) == absPath {
				continue // same file re-write is ok
			}
			key := existing.Path + "|" + existing.SigKey
			if seen[key] {
				continue
			}
			seen[key] = true
			conflicts = append(conflicts, existing)
		}
	}
	return conflicts
}

// Commit replaces indexed symbols for absPath with those extracted from content.
// Call after a write is accepted so the next write sees the new truth.
func (idx *SymbolIndex) Commit(ctx context.Context, absPath string, content []byte) {
	if idx == nil {
		return
	}
	incoming := idx.extract(ctx, absPath, content)
	absPath = filepath.Clean(absPath)

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.commitLocked(absPath, incoming)
}

// CheckAndCommit atomically detects cross-file duplicates and, when clear,
// commits the incoming symbols. Used for direct writes to close TOCTOU
// between CheckDuplicates and Commit under concurrent writers.
func (idx *SymbolIndex) CheckAndCommit(ctx context.Context, absPath string, content []byte) []SymbolLoc {
	if idx == nil {
		return nil
	}
	incoming := idx.extract(ctx, absPath, content)
	if len(incoming) == 0 {
		// Still clear prior symbols for this path (rewrite to non-code / empty).
		idx.mu.Lock()
		idx.commitLocked(filepath.Clean(absPath), nil)
		idx.mu.Unlock()
		return nil
	}
	absPath = filepath.Clean(absPath)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	conflicts := make([]SymbolLoc, 0)
	seen := make(map[string]bool)
	for _, sym := range incoming {
		for _, existing := range idx.byKey[sym.SigKey] {
			if filepath.Clean(existing.Path) == absPath {
				continue
			}
			key := existing.Path + "|" + existing.SigKey
			if seen[key] {
				continue
			}
			seen[key] = true
			conflicts = append(conflicts, existing)
		}
	}
	if len(conflicts) > 0 {
		return conflicts
	}
	idx.commitLocked(absPath, incoming)
	return nil
}

func (idx *SymbolIndex) commitLocked(absPath string, incoming []SymbolLoc) {
	if old, ok := idx.byPath[absPath]; ok {
		for _, sym := range old {
			idx.byKey[sym.SigKey] = removeLoc(idx.byKey[sym.SigKey], absPath)
			if len(idx.byKey[sym.SigKey]) == 0 {
				delete(idx.byKey, sym.SigKey)
			}
		}
	}
	if len(incoming) == 0 {
		delete(idx.byPath, absPath)
		return
	}
	idx.byPath[absPath] = incoming
	for _, sym := range incoming {
		idx.byKey[sym.SigKey] = append(idx.byKey[sym.SigKey], sym)
	}
}

// IndexContent is an alias for Commit used when seeding from disk.
func (idx *SymbolIndex) IndexContent(ctx context.Context, absPath string, content []byte) {
	idx.Commit(ctx, absPath, content)
}

// Remove drops all symbols previously indexed for absPath.
func (idx *SymbolIndex) Remove(absPath string) {
	if idx == nil {
		return
	}
	absPath = filepath.Clean(absPath)
	idx.mu.Lock()
	defer idx.mu.Unlock()
	old, ok := idx.byPath[absPath]
	if !ok {
		return
	}
	for _, sym := range old {
		idx.byKey[sym.SigKey] = removeLoc(idx.byKey[sym.SigKey], absPath)
		if len(idx.byKey[sym.SigKey]) == 0 {
			delete(idx.byKey, sym.SigKey)
		}
	}
	delete(idx.byPath, absPath)
}

// Paths returns a snapshot of currently indexed absolute paths.
func (idx *SymbolIndex) Paths() []string {
	if idx == nil {
		return nil
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]string, 0, len(idx.byPath))
	for p := range idx.byPath {
		out = append(out, p)
	}
	return out
}

func (idx *SymbolIndex) extract(ctx context.Context, absPath string, content []byte) []SymbolLoc {
	lang := languageFromPath(absPath)
	if lang == ast.LangUnknown {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := idx.parser.Parse(ctx, string(content), lang)
	if err != nil || result == nil {
		return nil
	}
	out := make([]SymbolLoc, 0)
	for _, sym := range result.Symbols {
		if !isTopLevelKind(sym.Kind) {
			continue
		}
		if sym.Name == "" || isCommonNoiseName(sym.Name) {
			continue
		}
		sig := string(sym.Kind) + ":" + sym.Name
		line := 0
		if sym.Range.Start.Line > 0 {
			line = sym.Range.Start.Line
		}
		out = append(out, SymbolLoc{
			Path:   filepath.Clean(absPath),
			Line:   line,
			Kind:   string(sym.Kind),
			Name:   sym.Name,
			SigKey: sig,
		})
	}
	return out
}

func isTopLevelKind(k ast.SymbolKind) bool {
	switch k {
	case ast.SymbolFunction, ast.SymbolClass, ast.SymbolInterface, ast.SymbolType, ast.SymbolEnum:
		return true
	default:
		return false
	}
}

func isCommonNoiseName(name string) bool {
	switch name {
	case "init", "main", "String", "Error", "New", "Get", "Set", "Id", "ID":
		return true
	default:
		return false
	}
}

func languageFromPath(path string) ast.SupportedLanguage {
	ext := strings.ToLower(filepath.Ext(path))
	if lang, ok := ast.LanguageExtensions[ext]; ok {
		return lang
	}
	return ast.LangUnknown
}

func removeLoc(list []SymbolLoc, path string) []SymbolLoc {
	path = filepath.Clean(path)
	out := list[:0]
	for _, loc := range list {
		if filepath.Clean(loc.Path) != path {
			out = append(out, loc)
		}
	}
	// if we shrunk capacity issues - rebuild if empty and original had items from only this path
	if len(out) == 0 {
		return nil
	}
	// copy to new slice to avoid retaining large underlying array incorrectly when all removed...
	// actually when out reuses list[:0] and all filtered, len 0 return nil is fine
	cp := make([]SymbolLoc, len(out))
	copy(cp, out)
	return cp
}

// formatConflict builds a human message for a duplicate.
func formatConflict(name, kind string, loc SymbolLoc) string {
	return fmt.Sprintf("duplicate %s %q already defined at %s:%d", kind, name, loc.Path, loc.Line)
}
