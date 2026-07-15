package guard

import (
	"path/filepath"
	"time"
)

// GrantExemption registers a temporary path exemption (in-memory).
// duration <= 0 defaults to 1 hour.
func (e *Engine) GrantExemption(ex Exemption) {
	if ex.Path == "" {
		return
	}
	if ex.ExpiresAt.IsZero() {
		ex.ExpiresAt = time.Now().UTC().Add(time.Hour)
	}
	ex.Path = filepath.Clean(ex.Path)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exemptions == nil {
		e.exemptions = make(map[string]Exemption)
	}
	e.exemptions[ex.Path] = ex
}

// ClearExemption removes a path exemption.
func (e *Engine) ClearExemption(path string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exemptions != nil {
		delete(e.exemptions, filepath.Clean(path))
	}
}

func (e *Engine) isExempt(absPath string, rule RuleID) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.exemptions == nil {
		return false
	}
	ex, ok := e.exemptions[filepath.Clean(absPath)]
	if !ok {
		return false
	}
	if time.Now().UTC().After(ex.ExpiresAt) {
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
