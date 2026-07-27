package guard

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
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
	loadedReqs, err := store.loadAllRequests()
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
	if e.exRequests == nil {
		e.exRequests = make(map[string]ExemptionRequest)
	}
	for _, r := range loadedReqs {
		e.exRequests[r.ID] = r
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
// duration via ExpiresAt; zero defaults to 1 hour. When a durable store is open, the grant is persisted
// before the in-memory map is updated so API callers can surface DB failures.
func (e *Engine) GrantExemption(ex Exemption) error {
	if ex.Path == "" {
		return fmt.Errorf("exemption path required")
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
	if e.exStore != nil {
		if err := e.exStore.put(ex); err != nil {
			return err
		}
	}
	if e.exemptions == nil {
		e.exemptions = make(map[string]Exemption)
	}
	e.exemptions[ex.Path] = ex
	return nil
}

// ClearExemption removes a path exemption (memory + durable store).
func (e *Engine) ClearExemption(path string) error {
	path = normalizeExemptionPath(path)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exStore != nil {
		if err := e.exStore.delete(path); err != nil {
			return err
		}
	}
	if e.exemptions != nil {
		delete(e.exemptions, path)
	}
	return nil
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

// ---------------------------------------------------------------------------
// Exemption approval workflow
// ---------------------------------------------------------------------------

// RequestExemption creates a pending exemption request. The request must be
// decided via DecideExemptionRequest before the exemption becomes active.
func (e *Engine) RequestExemption(ctx context.Context, req ExemptionRequest) (*ExemptionRequest, error) {
	if strings.TrimSpace(req.Path) == "" {
		return nil, fmt.Errorf("exemption request path required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, fmt.Errorf("exemption request reason required")
	}
	if strings.TrimSpace(req.Requester) == "" {
		return nil, fmt.Errorf("exemption request requester required")
	}
	req.ID = uuid.New().String()
	req.Status = RequestPending
	req.CreatedAt = time.Now().UTC()
	if req.TTL <= 0 {
		req.TTL = time.Hour
	}

	e.mu.Lock()
	if e.exRequests == nil {
		e.exRequests = make(map[string]ExemptionRequest)
	}
	if e.exStore != nil {
		if err := e.exStore.putRequest(req); err != nil {
			e.mu.Unlock()
			return nil, err
		}
	}
	e.exRequests[req.ID] = req
	e.mu.Unlock()

	e.mu.RLock()
	auditor := e.auditor
	e.mu.RUnlock()
	if auditor != nil {
		_ = auditor.RecordGuardEvent(ctx, "guard.exemption_requested", map[string]interface{}{
			"request_id": req.ID,
			"path":       req.Path,
			"rule_id":    string(req.RuleID),
			"requester":  req.Requester,
			"reason":     req.Reason,
		})
	}
	cp := req
	return &cp, nil
}

// DecideExemptionRequest approves or rejects a pending request. On approval
// the existing GrantExemption path is called so exemption normalization,
// persistence, and in-memory activation happen exactly as for direct grants.
func (e *Engine) DecideExemptionRequest(ctx context.Context, id string, approve bool, decidedBy, reason string) (*ExemptionRequest, error) {
	e.mu.Lock()
	req, ok := e.exRequests[id]
	if !ok {
		e.mu.Unlock()
		return nil, fmt.Errorf("exemption request not found: %s", id)
	}
	if req.Status != RequestPending {
		e.mu.Unlock()
		return nil, fmt.Errorf("exemption request already decided: %s (status %s)", id, req.Status)
	}
	now := time.Now().UTC()
	if approve {
		req.Status = RequestApproved
	} else {
		req.Status = RequestRejected
	}
	req.DecidedBy = decidedBy
	req.DecideReason = reason
	req.DecidedAt = now
	if e.exStore != nil {
		if err := e.exStore.putRequest(req); err != nil {
			e.mu.Unlock()
			return nil, err
		}
	}
	e.exRequests[id] = req
	e.mu.Unlock()

	if approve {
		rules := []RuleID(nil)
		if req.RuleID != "" {
			rules = []RuleID{req.RuleID}
		}
		if err := e.GrantExemption(Exemption{
			Path:      req.Path,
			Rules:     rules,
			Reason:    fmt.Sprintf("approved request %s: %s", id, req.Reason),
			ExpiresAt: now.Add(req.TTL),
		}); err != nil {
			return nil, err
		}
	}

	e.mu.RLock()
	auditor := e.auditor
	e.mu.RUnlock()
	if auditor != nil {
		_ = auditor.RecordGuardEvent(ctx, "guard.exemption_decided", map[string]interface{}{
			"request_id": req.ID,
			"path":       req.Path,
			"status":     string(req.Status),
			"decided_by": decidedBy,
			"reason":     reason,
		})
	}
	cp := req
	return &cp, nil
}

// ListExemptionRequests returns requests filtered by status (empty = all),
// ordered newest-first.
func (e *Engine) ListExemptionRequests(status RequestStatus) []ExemptionRequest {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]ExemptionRequest, 0, len(e.exRequests))
	for _, r := range e.exRequests {
		if status != "" && r.Status != status {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}
