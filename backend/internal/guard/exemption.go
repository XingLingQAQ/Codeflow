package guard

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// OpenExemptionStore attaches a durable SQLite store and reloads active exemptions.
// Safe to call once after NewEngine; subsequent calls replace the previous store.
func (e *Engine) OpenExemptionStore(dbPath string) error {
	if e == nil {
		return fmt.Errorf("guard engine is nil")
	}
	store, err := openSQLiteExemptionStore(dbPath)
	if err != nil {
		return err
	}
	loaded, err := store.loadActive(time.Now().UTC())
	if err != nil {
		_ = store.Close()
		return err
	}

	e.mu.Lock()
	if e.exStore != nil {
		_ = e.exStore.Close()
	}
	e.exStore = store
	if e.exemptions == nil {
		e.exemptions = make(map[string]Exemption)
	}
	for _, ex := range loaded {
		if ex.Path == "" {
			continue
		}
		ex.Path = normalizeExemptionPath(ex.Path)
		e.exemptions[ex.Path] = ex
	}
	e.mu.Unlock()
	return nil
}

// CloseExemptionStore closes the optional durable store (in-memory map is kept).
func (e *Engine) CloseExemptionStore() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exStore == nil {
		return nil
	}
	err := e.exStore.Close()
	e.exStore = nil
	return err
}

// GrantExemption registers a temporary path exemption.
// duration via ExpiresAt; zero defaults to 1 hour. When a durable store is open, the grant is persisted.
func (e *Engine) GrantExemption(ex Exemption) {
	if ex.Path == "" {
		return
	}
	if ex.ExpiresAt.IsZero() {
		ex.ExpiresAt = time.Now().UTC().Add(time.Hour)
	}
	ex.Path = normalizeExemptionPath(ex.Path)
	// Copy rules slice so callers cannot mutate after grant.
	if len(ex.Rules) > 0 {
		ex.Rules = append([]RuleID(nil), ex.Rules...)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exemptions == nil {
		e.exemptions = make(map[string]Exemption)
	}
	e.exemptions[ex.Path] = ex
	if e.exStore != nil {
		_ = e.exStore.put(ex)
	}
}

// ClearExemption removes a path exemption (memory + durable store).
func (e *Engine) ClearExemption(path string) {
	path = normalizeExemptionPath(path)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exemptions != nil {
		delete(e.exemptions, path)
	}
	if e.exStore != nil {
		_ = e.exStore.delete(path)
	}
}

// ListExemptions returns a snapshot of non-expired exemptions.
func (e *Engine) ListExemptions() []Exemption {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exemptions == nil {
		return nil
	}
	now := time.Now().UTC()
	out := make([]Exemption, 0, len(e.exemptions))
	for key, ex := range e.exemptions {
		if now.After(ex.ExpiresAt) {
			delete(e.exemptions, key)
			if e.exStore != nil {
				_ = e.exStore.delete(key)
			}
			continue
		}
		cp := ex
		if len(ex.Rules) > 0 {
			cp.Rules = append([]RuleID(nil), ex.Rules...)
		}
		out = append(out, cp)
	}
	return out
}

func (e *Engine) isExempt(absPath string, rule RuleID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exemptions == nil {
		return false
	}
	key := normalizeExemptionPath(absPath)
	ex, ok := e.exemptions[key]
	if !ok {
		// Allow relative exemption paths to match absolute write targets by
		// exact path or path-segment boundary only (never bare suffix).
		slashAbs := filepath.ToSlash(key)
		for k, candidate := range e.exemptions {
			slashKey := filepath.ToSlash(filepath.Clean(k))
			if slashKey == "" || slashKey == "." {
				continue
			}
			if pathMatchesExemption(slashAbs, slashKey) {
				ex = candidate
				ok = true
				key = k
				break
			}
		}
	}
	if !ok {
		return false
	}
	if time.Now().UTC().After(ex.ExpiresAt) {
		delete(e.exemptions, key)
		if e.exStore != nil {
			_ = e.exStore.delete(key)
		}
		return false
	}
	if len(ex.Rules) == 0 {
		return true
	}
	for _, r := range ex.Rules {
		if r == rule {
			return true
		}
	}
	return false
}

// normalizeExemptionPath cleans exemption keys for stable map lookup.
// Relative paths stay relative (so they can match absolute writes by segment).
// Absolute paths are Abs'd and symlink-resolved on the longest existing prefix
// so they compare equal to workspace.Resolve results.
func normalizeExemptionPath(p string) string {
	p = filepath.Clean(p)
	if p == "" || p == "." {
		return p
	}
	if !filepath.IsAbs(p) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return resolveExistingPrefix(abs)
}

// resolveExistingPrefix EvalSymlinks the longest existing ancestor and rejoins
// any missing trailing segments (write targets that do not exist yet).
func resolveExistingPrefix(abs string) string {
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	rest := make([]string, 0, 4)
	cur := abs
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...)
		}
		cur = parent
	}
}

// pathMatchesExemption returns true when absPath equals key or ends with "/"+key.
// Bare suffix matching is intentionally rejected to avoid "a.go" exempting "ba.go".
func pathMatchesExemption(slashAbs, slashKey string) bool {
	if slashAbs == slashKey {
		return true
	}
	return strings.HasSuffix(slashAbs, "/"+slashKey)
}
