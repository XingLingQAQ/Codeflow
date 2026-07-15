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
		ex.Path = filepath.Clean(ex.Path)
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
	ex.Path = filepath.Clean(ex.Path)
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
	path = filepath.Clean(path)
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
	key := filepath.Clean(absPath)
	ex, ok := e.exemptions[key]
	if !ok {
		// Allow relative exemption paths to match absolute write targets by suffix.
		slashAbs := filepath.ToSlash(key)
		for k, candidate := range e.exemptions {
			slashKey := filepath.ToSlash(k)
			if slashKey == "" {
				continue
			}
			if slashAbs == slashKey || strings.HasSuffix(slashAbs, "/"+slashKey) || strings.HasSuffix(slashAbs, slashKey) {
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
